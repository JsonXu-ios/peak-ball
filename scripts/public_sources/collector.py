"""Public HTTP collector with an optional ordinary browser renderer.

No CAPTCHA solving, challenge scripts, forged source-IP headers, or VIPC calls.
Actual egress rotation uses administrator-configured HTTP(S) proxies only.
"""
from __future__ import annotations

import json
import os
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone, timedelta

from bs4 import BeautifulSoup

from core import bookmaker_rows, now, normalize_bonus, save_match, snapshot, vote_dom, vote_record

OFFICIAL = "https://webapi.sporttery.cn/gateway/uniform/football/"
ALLOWED = {"webapi.sporttery.cn", "www.sporttery.cn", "static.sporttery.cn", "fenxi.zgzcw.com", "odds.zgzcw.com", "public.zgzcw.com", "saishi.zgzcw.com"}


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
        self.delay = max(1.0, float(os.environ.get("PUBLIC_SOURCE_INTERVAL", "1.5")))
        self.proxies = json.loads(os.environ.get("PUBLIC_SOURCE_PROXIES", "[]"))
        if not isinstance(self.proxies, list) or not all(isinstance(x, str) and urllib.parse.urlsplit(x).scheme in {"http", "https"} for x in self.proxies):
            raise ValueError("PUBLIC_SOURCE_PROXIES必须是HTTP(S)代理URL的JSON数组")
        self.index, self.last = 0, 0.0
        self.browser_enabled = os.environ.get("PUBLIC_SOURCE_BROWSER", "auto") != "off"

    def pace(self):
        time.sleep(max(0, self.delay - (time.monotonic() - self.last)))
        self.last = time.monotonic()
        proxy = self.proxies[self.index % len(self.proxies)] if self.proxies else None
        self.index += 1
        return proxy

    def http(self, url):
        check_url(url)
        proxy = self.pace()
        opener = urllib.request.build_opener(SafeRedirect(), urllib.request.ProxyHandler({"https": proxy, "http": proxy}) if proxy else urllib.request.ProxyHandler())
        request = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0", "Accept": "application/json,text/html", "Referer": "https://www.sporttery.cn/" if "sporttery.cn" in url else "https://odds.zgzcw.com/odds/oyzs/"})
        try:
            with opener.open(request, timeout=25) as response:
                data = response.read(8 * 1024 * 1024 + 1)
                if len(data) > 8 * 1024 * 1024:
                    raise ValueError("响应超过8MB")
                charset = response.headers.get_content_charset() or "utf-8"
                text = data.decode(charset, errors="replace")
        except urllib.error.HTTPError as exc:
            raise ValueError(f"来源HTTP {exc.code}，未取得数据") from None
        if re.search(r"Please Enable JavaScript|安全策略拦截|验证码|captcha", text, re.I):
            raise ValueError("来源要求浏览器或安全验证，未解析为比赛数据")
        return text

    def render(self, url, date=None, pages=1):
        if not self.browser_enabled:
            raise ValueError("浏览器采集已关闭")
        check_url(url)
        proxy = self.pace()
        try:
            from playwright.sync_api import sync_playwright
        except ImportError:
            raise ValueError("动态页面需安装独立采集依赖：pip install -r scripts/public_sources/requirements.txt") from None
        with sync_playwright() as runtime:
            options = {"headless": True, "channel": os.environ.get("PUBLIC_SOURCE_BROWSER_CHANNEL", "msedge")}
            if proxy:
                parsed = urllib.parse.urlsplit(proxy)
                options["proxy"] = {"server": f"{parsed.scheme}://{parsed.hostname}:{parsed.port or 80}"}
                if parsed.username:
                    options["proxy"].update(username=urllib.parse.unquote(parsed.username), password=urllib.parse.unquote(parsed.password or ""))
            browser = runtime.chromium.launch(**options)
            try:
                context = browser.new_context(locale="zh-CN", timezone_id="Asia/Shanghai", service_workers="block")
                page = context.new_page()
                page.goto(url, wait_until="domcontentloaded", timeout=35000)
                if date and page.locator("#start_date").count():
                    if page.locator("#start_date").input_value() != date:
                        page.locator("#start_date").fill(date)
                        page.locator("#start_date").press("Escape")
                        page.locator("body").click(position={"x": 5, "y": 5})
                selector = ".s_match" if "/jc/zq" in url and "zc/" in url else "table tr"
                if "webapi.sporttery.cn" not in url:
                    try:
                        page.wait_for_function("selector => document.querySelectorAll(selector).length > 2", arg=selector, timeout=15000)
                    except Exception:
                        pass
                content = [page.content()]
                if re.search(r"安全策略拦截|Please Enable JavaScript|验证您是真人", page.inner_text("body"), re.I):
                    raise ValueError("浏览器收到安全拦截，保留失败，不绕过验证")
                # Published paginator, ordinary UI clicks; never invoke hidden
                # page globals or use its network session to bypass a block.
                for page_no in range(2, pages + 1):
                    next_page = page.locator(".m-paging .u-pg2 a").get_by_text(str(page_no), exact=True)
                    if not next_page.count():
                        break
                    time.sleep(self.delay)
                    next_page.click()
                    page.wait_for_function("n => document.querySelector('.m-paging .u-pg3 span')?.textContent.trim() === String(n)", arg=page_no, timeout=20000)
                    content.append(page.content())
                return "\n<!-- published page boundary -->\n".join(content)
            finally:
                browser.close()

    def document(self, url, need="table"):
        try:
            raw = self.http(url)
            soup = BeautifulSoup(raw, "html.parser")
            if len(soup.select(need)) < 2:
                raise ValueError("静态页面无有效数据")
            return raw
        except (ValueError, OSError):
            return self.render(url)

    def api(self, url):
        try:
            return json.loads(self.http(url))
        except (ValueError, OSError):
            raw = self.render(url)
            return json.loads(BeautifulSoup(raw, "html.parser").get_text())

    def votes(self, date, pool, limit):
        result = []
        for page in range(1, (limit + 9) // 10 + 1):
            url = OFFICIAL + "getVoteV1.qry?" + urllib.parse.urlencode(dict(poolCode=pool, pageSize=10, pageNo=page, businessDate=date))
            try:
                data = self.api(url)
                if str(data.get("errorCode")) != "0":
                    raise ValueError("支持率接口返回错误")
                container = data.get("value", {}).get("matches", {})
                items = container.get("list", [])
                result.extend((row, vote_record(row), url) for row in items)
                if not items or page >= container.get("pages", 1):
                    break
            except (ValueError, OSError):
                # A normal rendered page is another published surface. Do not
                # solve or retry security challenges. Follow its paginator.
                if page != 1:
                    raise ValueError("支持率分页未取得，未将第一页冒充完整日期") from None
                url = "https://www.sporttery.cn/jc/" + ("zqspfzc/" if pool == "HAD" else "zqrqspfzc/")
                soup = BeautifulSoup(self.render(url, date, pages=(limit + 9) // 10), "html.parser")
                seen = set()
                for block in soup.select(".s_match"):
                    link = block.select_one('a[href*="mid="]')
                    if not link or date not in block.get_text():
                        continue
                    mid = urllib.parse.parse_qs(urllib.parse.urlsplit(link["href"]).query).get("mid", [""])[0]
                    title = link.get_text(" ", strip=True)
                    match = re.search(r"(周.\d+)\s+(.+?)\s+" + re.escape(date) + r"\s+(.+?)\s+VS\s+(.+)", title)
                    if not mid.isdigit() or not match or mid in seen:
                        continue
                    seen.add(mid)
                    row = {"matchId": mid, "matchNum": match[1], "leagueAllName": match[2], "homeTeamAllName": re.sub(r"[（(].*?[)）]", "", match[3]), "awayTeamAllName": match[4], "matchDate": date}
                    result.append((row, vote_dom(block.get_text(" ", strip=True)), url))
                if not result:
                    raise ValueError("支持比例页无指定日期比赛，未写入其他日期数据")
                break
        return result[:limit]


def doc_tables(raw):
    soup = BeautifulSoup(raw, "html.parser")
    tables = [[[c.get_text(" ", strip=True) for c in row.find_all(["th", "td"], recursive=False)] for row in table.find_all("tr")] for table in soup.select("table")]
    return soup, tables


def discover_mapping(client, matches):
    raw = client.document("https://odds.zgzcw.com/odds/oyzs/")
    soup = BeautifulSoup(raw, "html.parser")
    found = {}
    for link in soup.select("a[href]"):
        match = re.search(r"fenxi\.zgzcw\.com/(\d+)(?:/|$)", link["href"])
        if not match:
            continue
        parent = link.find_parent("tr")
        if parent:
            text = parent.get_text(" ", strip=True)
            for row, _, _ in matches:
                # Exact full-name matches only. Ambiguous aliases require an
                # explicit mapping in the admin UI, never fuzzy auto-joins.
                if row["homeTeamAllName"] in text and row["awayTeamAllName"] in text:
                    found.setdefault(str(row["matchId"]), set()).add(match[1])
    return {k: next(iter(v)) for k, v in found.items() if len(v) == 1}


def opening_kelly(client, soup, normalized, heartbeat, on_error):
    # Fetch earliest available history for the companies used by the existing
    # analysis. Others can be enabled with PUBLIC_SOURCE_KELLY_LABELS='*'.
    labels = os.environ.get("PUBLIC_SOURCE_KELLY_LABELS", "威*,36*,平*,官方(胜平负)").split(",")
    for item in normalized:
        if "*" not in labels and item["sourceLabel"] not in labels:
            continue
        row = next((r for r in soup.select("tr") if len(r.find_all("td", recursive=False)) >= 15 and r.find_all("td", recursive=False)[1].get_text(strip=True) == item["sourceLabel"]), None)
        link = row.select_one('a[href*="/bjop/zhishu?"]') if row else None
        if not link:
            continue
        heartbeat()
        try:
            url = urllib.parse.urljoin("https://fenxi.zgzcw.com/", link["href"])
            raw = client.document(url)
            _, tables = doc_tables(raw)
            records = [r for t in tables for r in t if len(r) >= 13 and re.match(r"\d{4}-\d{2}-\d{2} ", r[1])]
            if not records:
                raise ValueError("公司变化记录无有效凯利")
            first = min(records, key=lambda r: r[1])
            item["openingKelly"], item["openingKellyTime"] = first[9:12], first[1]
            item["historyUrl"], item["quoteHistory"] = url, records
        except Exception as exc:
            on_error("初始凯利:" + item["sourceLabel"], exc)


def collect(db, job_id):
    job = db.execute("SELECT * FROM ps_jobs WHERE id=? AND status='queued'", (job_id,)).fetchone()
    if job is None:
        raise ValueError("任务不存在或已执行")
    config = json.loads(job["config"])
    claimed = db.execute("UPDATE ps_jobs SET status='running',started_at=?,heartbeat=? WHERE id=? AND status='queued'", (now(), now(), job_id))
    db.commit()
    if claimed.rowcount != 1:
        raise ValueError("任务已经被其他进程领取")
    errors, processed = [], 0
    client = Client()

    def error(stage, exc):
        # Never persist proxy credentials or exception URLs from third parties.
        msg = str(exc) if isinstance(exc, ValueError) else type(exc).__name__
        msg = re.sub(r"https?://\S+", "[来源地址]", msg)
        errors.append({"stage": stage, "message": msg[:350]})

    def heartbeat():
        db.execute("UPDATE ps_jobs SET processed=?,errors=?,heartbeat=? WHERE id=?", (processed, json.dumps(errors, ensure_ascii=False), now(), job_id))
        db.commit()

    try:
        try:
            votes = client.votes(config["date"], "HAD", config["limit"])
        except Exception as exc:
            stored = db.execute("SELECT * FROM ps_matches WHERE business_date=? AND official_id=?", (config["date"], config.get("officialId", ""))).fetchone()
            if not stored:
                raise
            votes = [({"matchId": stored["official_id"], "homeTeamAllName": stored["home"], "awayTeamAllName": stored["away"], "matchNum": stored["lottery_number"]}, None, "")]
            error("胜平负支持比例", exc)
        if config.get("officialId"):
            votes = [x for x in votes if str(x[0]["matchId"]) == config["officialId"]]
            if not votes:
                # Existing independently imported matches can be refreshed even
                # if they are not on the currently visible first vote page.
                stored = db.execute("SELECT * FROM ps_matches WHERE business_date=? AND official_id=?", (config["date"], config["officialId"])).fetchone()
                if stored:
                    votes = [({"matchId": stored["official_id"], "homeTeamAllName": stored["home"], "awayTeamAllName": stored["away"], "matchNum": stored["lottery_number"]}, None, "")]
                else:
                    raise ValueError("支持率列表中未找到指定比赛；先按日期采集或导入并映射")
        try:
            rq_votes = {str(r["matchId"]): (r, n, u) for r, n, u in client.votes(config["date"], "HHAD", config["limit"])}
        except Exception as exc:
            rq_votes = {}
            error("让球支持比例", exc)
        try:
            mappings = discover_mapping(client, votes)
        except Exception as exc:
            mappings = {}
            error("足彩网自动映射", exc)
        for row, normalized, url in votes:
            official_id = str(row["matchId"])
            previous = db.execute("SELECT * FROM ps_matches WHERE business_date=? AND official_id=?", (config["date"], official_id)).fetchone()
            source_id = config.get("zgzcwId") if official_id == config.get("officialId") else None
            source_id = source_id or (previous["zgzcw_id"] if previous else None) or mappings.get(official_id)
            kickoff = previous["kickoff"] if previous else None
            if row.get("matchTime") and re.fullmatch(r"\d{2}:\d{2}(?::\d{2})?", str(row["matchTime"])):
                kickoff = str(row.get("matchDate", config["date"])) + " " + row["matchTime"]
            key = save_match(db, {"date": config["date"], "officialId": official_id, "home": row["homeTeamAllName"], "away": row["awayTeamAllName"], "lotteryNumber": row.get("matchNum"), "kickoff": kickoff, "zgzcwId": source_id})
            if normalized:
                snapshot(db, key, "support_had", "sporttery", url, row, normalized)
            if official_id in rq_votes:
                raw_rq, norm_rq, rq_url = rq_votes[official_id]
                snapshot(db, key, "support_hhad", "sporttery", rq_url, raw_rq, norm_rq)
            try:
                bonus_url = OFFICIAL + "getFixedBonusV1.qry?clientCode=3001&matchId=" + official_id
                raw_bonus = client.api(bonus_url)
                bonus = normalize_bonus(raw_bonus)
                if bonus["home"] != row["homeTeamAllName"] or bonus["away"] != row["awayTeamAllName"]:
                    raise ValueError("体彩奖金球队身份不匹配，停止关联")
                snapshot(db, key, "bonus", "sporttery", bonus_url, raw_bonus, bonus)
            except Exception as exc:
                error(official_id + ":奖金", exc)
            if source_id:
                for kind, suffix in (("euro", "bjop"), ("asian", "ypdb"), ("totals", "dxdb"), ("history", "bsls")):
                    heartbeat()
                    try:
                        source_url = "https://fenxi.zgzcw.com/" + source_id + "/" + suffix
                        raw = client.document(source_url)
                        soup, tables = doc_tables(raw)
                        # Validate both participants before applying a manual ID.
                        text = soup.get_text(" ", strip=True)
                        if row["homeTeamAllName"] not in text or row["awayTeamAllName"] not in text:
                            raise ValueError("足彩网球队身份不匹配，未写入盘口")
                        kickoff_match = re.search(r"比赛时间[：:]\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})", text)
                        if kickoff_match:
                            source_kickoff = datetime.fromisoformat(kickoff_match[1])
                            day = datetime.fromisoformat(config["date"])
                            if not day <= source_kickoff < day + timedelta(days=2):
                                raise ValueError("足彩网比赛日期不匹配，未应用映射")
                            if kickoff and abs((datetime.fromisoformat(kickoff) - source_kickoff).total_seconds()) > 900:
                                raise ValueError("来源开赛时间相差超过15分钟，请核对比赛映射")
                            db.execute("UPDATE ps_matches SET kickoff=? WHERE id=?", (kickoff_match[1], key))
                            db.commit()
                        norm = bookmaker_rows(tables, kind) if kind != "history" else {"tables": tables, "text": text}
                        if kind == "euro":
                            opening_kelly(client, soup, norm, heartbeat, error)
                        snapshot(db, key, kind, "zgzcw", source_url, raw, norm)
                    except Exception as exc:
                        error(official_id + ":" + kind, exc)
            else:
                error(official_id + ":映射", ValueError("未找到唯一足彩网比赛ID，可在比赛详情手动映射后补采"))
            processed += 1
            heartbeat()
        status = "partial" if errors and processed else "failed" if errors else "completed"
    except Exception as exc:
        error("任务", exc)
        status = "failed"
    db.execute("UPDATE ps_jobs SET status=?,processed=?,errors=?,finished_at=?,heartbeat=? WHERE id=?", (status, processed, json.dumps(errors, ensure_ascii=False), now(), now(), job_id))
    db.commit()
    return {"id": job_id, "status": status, "processed": processed, "errors": errors}
