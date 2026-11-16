"""Public HTTP collector with an optional ordinary browser renderer.

Random X-Forwarded-For headers and paced public requests are enabled by default.
Optional HTTP(S) proxies remain supported; no VIPC calls or CAPTCHA solving.
"""
from __future__ import annotations

import json
import ipaddress
import http.cookiejar
import os
import random
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone, timedelta

from bs4 import BeautifulSoup

from core import bookmaker_rows, now, normalize_bonus, save_match, snapshot, vote_dom, vote_record, detail, proxy_config, request_timing
from collection_report import safe_error, aggregate_errors, summarize_collection

OFFICIAL = "https://webapi.sporttery.cn/gateway/uniform/football/"
ALLOWED = {"webapi.sporttery.cn", "www.sporttery.cn", "static.sporttery.cn", "fenxi.zgzcw.com", "odds.zgzcw.com", "plzx.zgzcw.com", "public.zgzcw.com", "saishi.zgzcw.com", "cp.zgzcw.com"}
BLOCKED = re.compile(r"安全策略拦截|疑似攻击|Access Verification|人机验证|请求过于频繁|请输入正确的验证码", re.I)
RANDOM = random.SystemRandom()


def random_forwarded_for():
    while True:
        address = ipaddress.IPv4Address(RANDOM.getrandbits(32))
        if address.is_global and not address.is_multicast:
            return str(address)


def browser_request_headers(request, forwarded_for=None):
    headers = dict(request.headers)
    if urllib.parse.urlsplit(request.url).hostname in ALLOWED:
        headers = {k:v for k,v in headers.items() if k.lower() != "x-forwarded-for"}
        headers["X-Forwarded-For"] = forwarded_for or random_forwarded_for()
    return headers


def check_url(url):
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.hostname not in ALLOWED or parsed.username or parsed.password:
        raise ValueError("不允许的采集来源")
    return url


class SafeRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        check_url(newurl)
        return super().redirect_request(request, fp, code, msg, headers, newurl)


class Client:
    def __init__(self):
        self.delay, self.jitter = request_timing()
        self.proxies = proxy_config()
        if not isinstance(self.proxies, list) or not all(isinstance(x, str) and urllib.parse.urlsplit(x).scheme in {"http", "https"} for x in self.proxies):
            raise ValueError("PUBLIC_SOURCE_PROXIES必须是HTTP(S)代理URL的JSON数组")
        self.index, self.last = 0, 0.0
        self.browser_enabled = os.environ.get("PUBLIC_SOURCE_BROWSER", "auto") != "off"
        self.blocked_hosts = set()
        self._runtime = None
        self._sessions = {}
        self._openers = {}
        self._page_forwarded_for = None
        self._navigation_requests = 0
        self._render_hosts = set()

    def close(self):
        for browser, _, _ in self._sessions.values():
            try:
                browser.close()
            except Exception:
                pass
        self._sessions.clear()
        if self._runtime is not None:
            try:
                self._runtime.stop()
            except Exception:
                pass
            finally:
                self._runtime = None

    def throttle(self):
        time.sleep(max(0, self.delay + RANDOM.uniform(0, self.jitter) - (time.monotonic() - self.last)))
        self.last = time.monotonic()

    def pace(self):
        self.throttle()
        proxy = self.proxies[self.index % len(self.proxies)] if self.proxies else None
        self.index += 1
        return proxy

    def http(self, url, form=None):
        check_url(url)
        if urllib.parse.urlsplit(url).hostname in self.blocked_hosts:
            raise ValueError("该来源已触发验证，本次任务停止继续请求")
        proxy = self.pace()
        if proxy not in self._openers:
            self._openers[proxy] = urllib.request.build_opener(SafeRedirect(), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()), urllib.request.ProxyHandler({"https": proxy, "http": proxy}) if proxy else urllib.request.ProxyHandler())
        opener = self._openers[proxy]
        headers = {"User-Agent": "Mozilla/5.0", "Accept": "application/json,text/plain,text/html", "X-Forwarded-For": random_forwarded_for(), "Referer": "https://www.sporttery.cn/" if "sporttery.cn" in url else "https://" + urllib.parse.urlsplit(url).hostname + "/"}
        encoded = urllib.parse.urlencode(form).encode("utf-8") if form is not None else None
        request = urllib.request.Request(url, data=encoded, headers=headers)
        try:
            with opener.open(request, timeout=25) as response:
                data = response.read(8 * 1024 * 1024 + 1)
                if len(data) > 8 * 1024 * 1024:
                    raise ValueError("响应超过8MB")
                charset = response.headers.get_content_charset() or "utf-8"
                text = data.decode(charset, errors="replace")
        except urllib.error.HTTPError as exc:
            raise ValueError(f"来源HTTP {exc.code}，未取得数据") from None
        if BLOCKED.search(text):
            self.blocked_hosts.add(urllib.parse.urlsplit(url).hostname)
            raise ValueError("来源触发安全验证，本次任务停止该来源请求")
        # Ordinary source pages contain login forms mentioning verification
        # codes. That word alone is not an access challenge for the data.
        if re.search(r"Please Enable JavaScript|验证您是真人", text, re.I):
            raise ValueError("来源要求浏览器或安全验证，未解析为比赛数据")
        return text

    def _session(self, proxy):
        if proxy in self._sessions:
            return self._sessions[proxy]
        try:
            from playwright.sync_api import sync_playwright
        except ImportError:
            raise ValueError("动态页面需安装独立采集依赖：pip install -r scripts/public_sources/requirements.txt") from None
        if self._runtime is None:
            self._runtime = sync_playwright().start()
        options = {"headless": True, "channel": os.environ.get("PUBLIC_SOURCE_BROWSER_CHANNEL", "msedge")}
        if proxy:
            parsed = urllib.parse.urlsplit(proxy)
            options["proxy"] = {"server": f"{parsed.scheme}://{parsed.hostname}:{parsed.port or 80}"}
            if parsed.username:
                options["proxy"].update(username=urllib.parse.unquote(parsed.username), password=urllib.parse.unquote(parsed.password or ""))
        browser = self._runtime.chromium.launch(**options)
        context = browser.new_context(locale="zh-CN", timezone_id="Asia/Shanghai", service_workers="block")

        def route_request(route):
            request = route.request
            host = urllib.parse.urlsplit(request.url).hostname
            if host in self.blocked_hosts:
                route.abort()
                return
            # A data page may embed several CP login/document iframes. Only
            # repeated navigations of this page's main frame indicate a loop;
            # counting child frames would incorrectly block their API host.
            if request.resource_type == "document" and host in ALLOWED and request.frame == page.main_frame:
                self._navigation_requests += 1
                if self._navigation_requests > 4:
                    self.blocked_hosts.add(host)
                    route.abort()
                    return
            # Static assets do not carry match data. Pace document and API
            # requests as well as top-level calls; keep this task's cookies.
            if host in ALLOWED and request.resource_type in {"document", "xhr", "fetch"}:
                self.throttle()
            if request.resource_type in {"image", "media", "font"}:
                route.abort()
                return
            route.continue_(headers=browser_request_headers(request, self._page_forwarded_for))

        context.route("**/*", route_request)
        page = context.new_page()
        self._sessions[proxy] = (browser, context, page)
        return self._sessions[proxy]

    def _check_block(self, page, url):
        if urllib.parse.urlsplit(url).hostname in self.blocked_hosts:
            raise ValueError("来源反复验证或重定向，本次任务停止该来源请求")
        if not page.locator("body").count():
            return  # A normal navigation may briefly have no body.
        body = page.inner_text("body")
        if BLOCKED.search(body) or re.search(r"Please Enable JavaScript|验证您是真人", body, re.I):
            self.blocked_hosts.add(urllib.parse.urlsplit(url).hostname)
            raise ValueError("浏览器收到安全拦截，本次任务停止该来源请求")

    def _wait_ready(self, page, url, date=None, ready=None):
        self._check_block(page, url)
        # Vue creates these elements before its data arrives. Waiting only
        # for selector existence returns an empty match card within 2 seconds.
        if "webapi.sporttery.cn" in url:
            predicate = "() => { try { return typeof JSON.parse(document.body.innerText) === 'object' } catch { return false } }"
            label = "接口JSON"
        elif "/jc/zqdz/" in url:
            predicate = r"""() => {
                const card = document.querySelector('.m-matchCard-Cen');
                const text = s => card?.querySelector(s)?.textContent.trim() || '';
                return /周.\d+/.test(text('.u-top')) && text('.u-middleLf') && text('.u-middleRt') && /\d{4}-\d{2}-\d{2} \d{2}:\d{2}/.test(text('.u-btm'));
            }"""
            label = "比赛编号、双方球队和开赛时间"
        elif "/jc/zq" in url and "zc/" in url:
            predicate = "arg => [...document.querySelectorAll('.s_match')].some(x => (!arg.date || x.innerText.includes(arg.date)) && x.querySelectorAll('tr').length >= 4)"
            label = "指定日期支持比例"
        elif re.search(r"/\d+/(?:bjop|ypdb|dxdb)$", urllib.parse.urlsplit(url).path):
            predicate = r"""() => [...document.querySelectorAll('table tr')].some(row => {
                const cells = [...row.children].filter(x => x.tagName === 'TD');
                return cells.length >= 15 && /^\d+$/.test(cells[0].textContent.trim()) && /^\d+(\.\d+)?$/.test(cells[2].textContent.trim());
            })"""
            label = "有效盘口记录"
        else:
            predicate = "arg => [...document.querySelectorAll(arg.selector)].filter(x => x.textContent.trim()).length >= arg.count"
            label = "比赛数据表格"
        try:
            page.wait_for_function(predicate, arg={"date": date, "selector": ready or "table tr", "count": 1 if ready else 3}, timeout=90000)
        except Exception:
            self._check_block(page, url)
            raise ValueError("等待" + label + "超时，未把未加载页面当作有效数据") from None
        self._check_block(page, url)

    def render(self, url, date=None, pages=1, ready=None):
        if not self.browser_enabled:
            raise ValueError("浏览器采集已关闭")
        check_url(url)
        if urllib.parse.urlsplit(url).hostname in self.blocked_hosts:
            raise ValueError("该来源已触发验证，本次任务停止继续请求")
        proxy = self.proxies[self.index % len(self.proxies)] if self.proxies else None
        self.index += 1
        _, _, page = self._session(proxy)
        # One page's redirects and API calls are one browser load. Changing
        # its advertised address halfway through makes cookies inconsistent.
        self._page_forwarded_for = random_forwarded_for()
        self._navigation_requests = 0
        response = page.goto(url, wait_until="domcontentloaded", timeout=90000)
        if response and response.status >= 400:
            raise ValueError(f"浏览器来源HTTP {response.status}，未取得数据")
        self._check_block(page, url)
        if date and page.locator("#start_date").count():
            if page.locator("#start_date").input_value() != date:
                page.locator("#start_date").fill(date)
                page.locator("#start_date").press("Escape")
                page.locator("body").click(position={"x": 5, "y": 5})
        self._wait_ready(page, url, date, ready)
        content = [page.content()]
        for page_no in range(2, pages + 1):
            next_page = page.locator(".m-paging .u-pg2 a").get_by_text(str(page_no), exact=True)
            if not next_page.count():
                break
            previous = page.locator(".s_match").first.inner_text()
            self.throttle()
            next_page.click()
            page.wait_for_function("arg => document.querySelector('.m-paging .u-pg3 span')?.textContent.trim() === String(arg.page) && document.querySelector('.s_match')?.innerText !== arg.previous", arg={"page":page_no,"previous":previous}, timeout=90000)
            self._wait_ready(page, url, date, ready)
            content.append(page.content())
        return "\n<!-- published page boundary -->\n".join(content)

    def match_info(self, official_id, lottery_number):
        url = OFFICIAL + "getMatchHeadV1.qry?source=web&sportteryMatchId=" + official_id
        body = self.api(url)
        head = parse_official_head(body, official_id, lottery_number)
        return head, {"tables": [], "text": "", "source": "sporttery_match_head"}, body, url

    def document(self, url, need="table"):
        host = urllib.parse.urlsplit(url).hostname
        if host in self._render_hosts:
            return self.render(url)
        try:
            raw = self.http(url)
            soup = BeautifulSoup(raw, "html.parser")
            if len(soup.select(need)) < 2:
                raise ValueError("静态页面无有效数据")
            return raw
        except (ValueError, OSError):
            self._render_hosts.add(host)
            return self.render(url)

    def api(self, url, form=None):
        from api_transport import fetch_json
        return fetch_json(self, url, form=form)

    def votes(self, date, pool, limit):
        result, seen = [], set()
        for page in range(1, (limit + 9) // 10 + 1):
            url = OFFICIAL + "getVoteV1.qry?" + urllib.parse.urlencode(dict(poolCode=pool, pageSize=10, pageNo=page, businessDate=date))
            try:
                data = self.api(url)
                if str(data.get("errorCode")) != "0":
                    raise ValueError("支持率接口返回错误")
                container = data.get("value", {}).get("matches", {})
                items = container.get("list")
                if not isinstance(items, list):
                    raise ValueError("支持比例接口缺少比赛数组")
                for row in items:
                    if str(row.get("businessDate", date)) != date:
                        raise ValueError("支持比例接口返回其他竞彩日期，未写入")
                    key = str(row.get("matchId", ""))
                    if not key.isdigit() or key in seen:
                        raise ValueError("支持比例分页返回重复或无效比赛ID")
                    seen.add(key)
                    normalized = vote_record(row)
                    normalized["transport"] = getattr(self, "last_api_transport", "http")
                    result.append((row, normalized, url))
                pages = container.get("pages")
                if pages is None and container.get("total") is not None:
                    pages = (int(container["total"]) + 9) // 10
                if not items or (pages is not None and page >= int(pages)) or (pages is None and len(items) < 10):
                    break
            except Exception as exc:
                stage = "支持比例分页接口" if page > 1 else "支持比例接口"
                raise ValueError(stage + "失败：" + safe_error(exc) + "；未改用页面") from None
        return result[:limit]


def doc_tables(raw):
    soup = BeautifulSoup(raw, "html.parser")
    tables = [[[c.get_text(" ", strip=True) for c in row.find_all(["th", "td"], recursive=False)] for row in table.find_all("tr")] for table in soup.select("table")]
    return soup, tables


def validate_history_tables(tables):
    if not any(any(re.fullmatch(r"\d{2,4}[-/]\d{1,2}[-/]\d{1,2}(?:\s+\d{2}:\d{2}.*)?", str(cell).strip()) for cell in row)
               and any(re.fullmatch(r"\d+\s*[:：-]\s*\d+", str(cell).strip()) for cell in row)
               for table in tables for row in table if len(row) >= 6):
        raise ValueError("战绩页面没有有效日期和比分记录，未把空页面计为采集成功")


def parse_official_head(body, official_id, lottery_number):
    value = body.get("value") or {}
    if str(body.get("errorCode")) != "0" or str(value.get("sportteryMatchId")) != str(official_id) or value.get("matchNum") != lottery_number:
        raise ValueError("官方比赛信息ID或竞彩编号不匹配")
    home, away = str(value.get("homeTeamShortName") or "").strip(), str(value.get("awayTeamShortName") or "").strip()
    stamp = str(value.get("matchDateTime") or "")
    if not home or not away or not re.fullmatch(r"\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?", stamp):
        raise ValueError("官方比赛信息缺少球队或开赛时间")
    datetime.fromisoformat(stamp)
    return {"home": home, "away": away, "kickoff": stamp + (":00" if len(stamp) == 16 else ""), "lotteryNumber": lottery_number, "officialId":str(official_id)}


def parse_official_page(raw, lottery_number):
    soup = BeautifulSoup(raw, "html.parser")
    card = soup.select_one(".m-matchCard-Cen")
    if not card or lottery_number not in card.get_text():
        raise ValueError("比赛头尚未加载或竞彩编号不匹配")
    home, away, time_node = card.select_one(".u-middleLf"), card.select_one(".u-middleRt"), card.select_one(".u-btm")
    stamp = re.search(r"\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?", time_node.get_text() if time_node else "")
    if not home or not away or not home.get_text(strip=True) or not away.get_text(strip=True) or not stamp:
        raise ValueError("比赛头缺少球队或开赛时间")
    kickoff = stamp[0] + (":00" if len(stamp[0]) == 16 else "")
    head = {"home": home.get_text(strip=True), "away": away.get_text(strip=True), "kickoff": kickoff, "lotteryNumber": lottery_number}
    def history_table(table):
        result = []
        if table:
            for row in table.select("tr"):
                cells = row.find_all("td", recursive=False)
                group = row.select_one(".m-tableFlex")
                if len(cells) < 3 or not group:
                    continue
                left, right = group.select_one(".left"), group.select_one(".right")
                score = group.select_one(".u-top")
                half = group.select_one(".u-btm")
                score_text = re.sub(r"\s", "", score.get_text() if score else "")
                if left and right and re.fullmatch(r"\d+:\d+", score_text):
                    result.append([cells[1].get_text(strip=True), "", cells[0].get_text(strip=True), left.get_text(strip=True), score_text, right.get_text(strip=True), half.get_text(strip=True) if half else ""])
        return result
    recent = soup.select("#bsjk table")
    tables = [history_table(recent[0] if recent else None), history_table(recent[1] if len(recent)>1 else None), history_table(soup.select_one("#lsM table"))]
    return head, {"tables": tables, "text": soup.get_text(" ", strip=True), "source": "sporttery_forecast"}


def parse_mapping_index(raw, matches, date=None, url="https://fenxi.zgzcw.com/"):
    soup = BeautifulSoup(raw, "html.parser")
    if BLOCKED.search(soup.get_text()):
        raise ValueError("赛程入口被安全验证拦截，未把拦截页当作空赛程")
    if date and not (date in soup.get_text() or datetime.strptime(date, "%Y-%m-%d").strftime("%Y年%m月%d日") in soup.get_text()):
        raise ValueError("赛程入口日期不匹配，未关联其他日期比赛")
    found = {}
    for link in soup.select("a[href]"):
        match = re.search(r"fenxi\.zgzcw\.com/(\d+)(?:/|$)", urllib.parse.urljoin(url, link["href"]))
        if not match:
            continue
        parent = link.find_parent("tr")
        if parent:
            text = parent.get_text(" ", strip=True)
            for row, _, _ in matches:
                # Exact full-name matches only. Ambiguous aliases require an
                # explicit mapping in the admin UI, never fuzzy auto-joins.
                home_names = {row["homeTeamAllName"], row.get("_sourceHome", row["homeTeamAllName"])}
                away_names = {row["awayTeamAllName"], row.get("_sourceAway", row["awayTeamAllName"])}
                teams = parent.select('a[href*="/soccer/team/"]')
                home_name = teams[0].get_text(strip=True) if len(teams)>1 else None
                away_name = teams[1].get_text(strip=True) if len(teams)>1 else None
                title = link.get("title", "")
                if (home_name in home_names and away_name in away_names) or (not teams and any(title.startswith(h+"VS"+a) for h in home_names for a in away_names)):
                    found.setdefault(str(row["matchId"]), set()).add(match[1])
    return {k: next(iter(v)) for k, v in found.items() if len(v) == 1}


def collect(db, job_id):
    from pipeline import run_collection
    return run_collection(db, job_id, Client)
