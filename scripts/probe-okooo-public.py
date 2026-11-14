#!/usr/bin/env python3
"""One-shot public-page audit. No DB, cookies, login, scheduler or VIPC access."""
from __future__ import annotations

import argparse
import hashlib
import html
import json
import re
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timezone
from pathlib import Path

from bs4 import BeautifulSoup

ROOT = Path(__file__).resolve().parents[1]
ORIGIN = 'https://www.okooo.com'
MARKETS = {
    'history': '交锋与近期战绩', 'odds': '欧赔', 'ah': '亚盘',
    'overunder': '大小球', 'cs': '比分报价', 'goals': '总进球报价',
    'exchanges': '必发与竞彩保存盈亏',
}


def utc_now():
    return datetime.now(timezone.utc).isoformat()


class PublicOnlyRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        target = urllib.parse.urljoin(req.full_url, newurl)
        parsed = urllib.parse.urlparse(target)
        if parsed.scheme != 'https' or parsed.netloc != 'www.okooo.com':
            raise urllib.error.HTTPError(target, code, 'external redirect stopped', headers, fp)
        if re.search(r'/user/|login|regform', parsed.path, re.I):
            raise urllib.error.HTTPError(target, code, 'login redirect stopped', headers, fp)
        return super().redirect_request(req, fp, code, msg, headers, target)


def decode_body(body):
    declared = re.search(br'charset\s*=\s*["\']?([\w-]+)', body[:4096], re.I)
    encoding = declared.group(1).decode('ascii') if declared else 'utf-8'
    if encoding.lower() in ('gb2312', 'gbk'):
        encoding = 'gb18030'
    try:
        return body.decode(encoding), encoding
    except (UnicodeError, LookupError):
        return body.decode('utf-8', errors='replace'), 'utf-8-replacement'


def price(text):
    if not re.fullmatch(r'\d+(?:\.\d+)?', text.strip()):
        return False
    return 1 < float(text) < 10000


def evidence(document, market, match_id):
    """Keep structural evidence; never infer availability from navigation labels."""
    soup = BeautifulSoup(document, 'html.parser')
    title = soup.title.get_text(' ', strip=True) if soup.title else ''
    tables = []
    for table in soup.select('table'):
        # Nested tables are captured separately, not counted twice.
        rows = []
        for tr in table.select('tr'):
            if tr.find_parent('table') is not table:
                continue
            cells = [c.get_text(' ', strip=True) for c in tr.find_all(['td', 'th'], recursive=False)]
            if cells:
                rows.append(cells)
        if rows:
            tables.append({'id': table.get('id'), 'classes': table.get('class', []), 'rows': rows})
    history = []
    priced = []
    for table_index, table in enumerate(tables):
        headers = ' '.join(' '.join(row) for row in table['rows'][:3])
        market_table = any(term in headers for term in ('公司名', '公司名称', '价位', '成交量', '成交比例', '盈亏指数'))
        for row in table['rows']:
            joined = ' '.join(row)
            # Require a dated result; a score in a menu or news headline is insufficient.
            if re.search(r'\b(?:20\d{2}|\d{2})-\d{2}-\d{2}\b', joined) and re.search(r'\b\d{1,2}\s*[-:]\s*\d{1,2}\b', joined):
                history.append({'table': table_index, 'cells': row})
            if market != 'history' and market_table and sum(price(c) for c in row) >= 3:
                priced.append({'table': table_index, 'cells': row})
    text = soup.get_text(' ', strip=True)
    notices = [s for s in ('开通欧亚数据权限', '付费可见', '下载APP使用相关功能', '请登录') if s in text]
    return {
        'title': title, 'source_match_id': match_id,
        'history_result_rows': history if market == 'history' else [],
        'numeric_table_candidates': priced,
        'tables': tables, 'access_notices': notices,
        # Numeric cells alone do not identify company, market, stage or provenance.
        'semantic_mapping': 'not_verified',
        'vipc_equivalence': 'not_verified',
    }


def fixtures(document, date, limit):
    soup = BeautifulSoup(document, 'html.parser')
    found = []
    seen = set()
    for row in soup.select('.touzhu_1[data-mid]'):
        mid = row.get('data-mid', '')
        when = row.select_one('.shijian')
        when_text = when.get('title', '') if when else ''
        group = row.find_parent('div', class_='cont')
        heading = group.select_one('.riqi .time > a') if group else None
        day_match = re.search(r'\d{4}-\d{2}-\d{2}', heading.get_text(' ', strip=True)) if heading else None
        schedule_date = day_match.group(0) if day_match else None
        if not re.fullmatch(r'\d{1,20}', mid) or mid in seen or schedule_date != date:
            continue
        seen.add(mid)
        teams = row.select('.shenpf .zhum')
        home, away = (teams + [None, None])[:2]
        league = row.select_one('.saiming')
        found.append({
            'source': 'okooo', 'source_match_id': mid,
            'jingcai_label': row.get('data-ordercn'), 'schedule_date': schedule_date,
            'kickoff_source_text': when_text,
            'home': home.get('title') or home.get_text(strip=True) if home else row.get('data-hname'),
            'away': away.get('title') or away.get_text(strip=True) if away else row.get('data-aname'),
            'league': league.get_text(strip=True) if league else None,
            'schedule_row_html': str(row),
            'vipc_match_id': None,
        })
        if len(found) >= limit:
            break
    return found


class Fetcher:
    def __init__(self, directory, interval, timeout):
        self.directory, self.interval, self.timeout = directory, interval, timeout
        self.last = 0.0
        # No cookie processor, no user session, no retries or challenge bypass.
        self.opener = urllib.request.build_opener(PublicOnlyRedirect())

    def get(self, url, name):
        time.sleep(max(0, self.interval - (time.monotonic() - self.last)))
        self.last = time.monotonic()
        record = {'url': url, 'observed_at': utc_now(), 'status': None, 'error': None}
        body = b''
        try:
            request = urllib.request.Request(url, headers={'User-Agent': 'Mozilla/5.0', 'Accept': 'text/html'})
            with self.opener.open(request, timeout=self.timeout) as response:
                record.update(status=response.status, final_url=response.url,
                              content_type=response.headers.get('Content-Type'))
                body = response.read(4 * 1024 * 1024 + 1)
                if len(body) > 4 * 1024 * 1024:
                    raise ValueError('response exceeds 4 MiB; not parsed')
        except urllib.error.HTTPError as exc:
            record.update(status=exc.code, error=str(exc))
            body = exc.read(65536)
        except Exception as exc:
            record['error'] = str(exc)
        if body:
            raw = self.directory / 'raw' / (name + '.bin')
            raw.write_bytes(body)
            record.update(raw_file=str(raw.relative_to(self.directory)), sha256=hashlib.sha256(body).hexdigest())
        document, encoding = decode_body(body)
        record['encoding'] = encoding
        print(f"{name}: HTTP {record['status']} {record['error'] or ''}", flush=True)
        return record, document


def report_html(report):
    esc = lambda x: html.escape(str(x))
    rows = []
    for match in report['matches']:
        for market, page in match['pages'].items():
            ev = page.get('evidence', {})
            count = len(ev.get('history_result_rows', [])) if market == 'history' else len(ev.get('numeric_table_candidates', []))
            rows.append('<tr>' + ''.join('<td>' + esc(v) + '</td>' for v in (
                match['fixture']['source_match_id'], match['fixture']['jingcai_label'], MARKETS[market],
                page['status'], count, page.get('error') or '', '、'.join(ev.get('access_notices', [])))) + '</tr>')
    return '''<!doctype html><meta charset="utf-8"><title>澳客独立公开数据验证</title>
<style>body{font:15px system-ui;margin:32px;color:#182233}table{border-collapse:collapse;width:100%}td,th{border:1px solid #ddd;padding:8px;text-align:left}th{background:#edf2f8}</style>
<h1>澳客独立公开数据验证</h1><p>''' + esc(report['started_at']) + '''</p>
<p>仅访问免费公开页面；未访问数据库、VIPC、登录会话、定时任务或业务接口。</p>
<p>表格数值候选仅为待核验原始证据，不代表赔率、公司、初即时盘或盈亏口径已映射成功。HTTP 200 也不等于字段可用。</p>
<p>计划采样 ''' + esc(report['requested_count']) + ' 场，列表发现 ' + esc(len(report['matches'])) + ''' 场。失败及空结果保留，不以旧场次补足。</p>
<p>原始响应见 raw；完整表格、采集时间、错误及哈希见 report.json。澳客保存盈亏与必发盈亏均不得写入原 VIPC 口径字段。</p>
<table><tr><th>澳客ID</th><th>竞彩编号</th><th>页面</th><th>HTTP</th><th>数据行候选数</th><th>错误</th><th>权限提示</th></tr>''' + ''.join(rows) + '</table>'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--date', required=True, help='Source schedule date YYYY-MM-DD; explicit to avoid timezone ambiguity')
    parser.add_argument('--limit', type=int, default=10)
    parser.add_argument('--interval', type=float, default=1.5)
    parser.add_argument('--timeout', type=float, default=12)
    args = parser.parse_args()
    datetime.strptime(args.date, '%Y-%m-%d')
    if not 1 <= args.limit <= 10 or args.interval < 1 or args.timeout <= 0:
        parser.error('limit must be 1..10, interval >=1 second, timeout >0')
    directory = ROOT / 'reports' / 'source-probes' / 'okooo' / datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    (directory / 'raw').mkdir(parents=True, exist_ok=False)
    fetcher = Fetcher(directory, args.interval, args.timeout)
    report = {'source': 'okooo', 'mode': 'isolated_public_probe', 'started_at': utc_now(),
              'schedule_date': args.date, 'requested_count': args.limit, 'matches': [],
              'limitations': ['No business writes or VIPC access', 'No source ID mapping yet',
                              'Numeric candidates require semantic verification', 'No equivalent VIPC trade fields established']}

    def save():
        (directory / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
        (directory / 'report.html').write_text(report_html(report), encoding='utf-8')

    index, document = fetcher.get(ORIGIN + '/jingcai/', 'schedule')
    report['schedule_request'] = index
    selected = fixtures(document, args.date, args.limit) if index['status'] == 200 and not index['error'] else []
    save()
    for fixture in selected:
        match = {'fixture': fixture, 'pages': {}}
        report['matches'].append(match)
        for market in MARKETS:
            mid = fixture['source_match_id']
            record, document = fetcher.get(f'{ORIGIN}/soccer/match/{mid}/{market}/', mid + '-' + market)
            if record['status'] == 200 and not record['error']:
                record['evidence'] = evidence(document, market, mid)
            match['pages'][market] = record
            save()
    report['finished_at'] = utc_now()
    report['request_counts'] = {str(code): sum(p['status'] == code for m in report['matches'] for p in m['pages'].values())
                                for code in {p['status'] for m in report['matches'] for p in m['pages'].values()}}
    save()
    print(f'Report: {directory / "report.html"}', flush=True)
    if not selected:
        raise SystemExit(2)


if __name__ == '__main__':
    main()
