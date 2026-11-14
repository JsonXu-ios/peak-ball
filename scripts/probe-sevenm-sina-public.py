#!/usr/bin/env python3
"""Isolated public-source reconnaissance; no DB, VIPC, login or service changes."""
import argparse
import ast
import hashlib
import html
import importlib.util
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
spec = importlib.util.spec_from_file_location('public_probe_helpers', Path(__file__).with_name('probe-okooo-public.py'))
helpers = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helpers)


def sevenm_arrays(document):
    """Read literal arrays only; never execute source JavaScript."""
    arrays = {}
    for name, value in re.findall(r'var\s+(\w+)\s*=\s*(\[[^;]*?\])\s*;', document):
        try:
            parsed = ast.literal_eval(value)
            if isinstance(parsed, list):
                arrays[name] = parsed
        except (ValueError, SyntaxError):
            pass
    return arrays


def sevenm_sample(arrays, limit):
    required = ['live_bh_Arr', 'Team_A_Arr', 'Team_B_Arr', 'Match_name_Arr', 'live_a_Arr', 'live_b_Arr']
    sizes = {key: len(arrays.get(key, [])) for key in required}
    if not sizes[required[0]] or len(set(sizes.values())) != 1:
        return [], {'error': 'required source arrays absent or not aligned', 'lengths': sizes}
    rows = []
    for index in range(min(limit, sizes[required[0]])):
        rows.append({key: arrays[key][index] for key in arrays if len(arrays[key]) == sizes[required[0]]})
    return rows, {'lengths': sizes, 'semantic_mapping': 'raw_source_arrays_only',
                  'note': 'Source calendar date is not the Chinese lottery sales date. Scores/stages/odds still require mapping.'}


def sina_detail_links(document, limit):
    soup = BeautifulSoup(document, 'html.parser')
    rows, seen = [], set()
    for a in soup.select('a[href]'):
        url = urllib.parse.urljoin('https://lottery.sina.com.cn/ai/', a['href'])
        parsed = urllib.parse.urlparse(url)
        if parsed.hostname != 'lottery.sina.com.cn' or '/ai/match/football/' not in parsed.path:
            continue
        query = urllib.parse.parse_qs(parsed.query)
        mid = (query.get('matchId') or query.get('lc_id') or [''])[0]
        if not re.fullmatch(r'\d{1,20}', mid) or mid in seen:
            continue
        # Use links exactly as published, including any legacy JS redirect shell.
        seen.add(mid)
        rows.append({'source_match_id': mid, 'url': url, 'anchor_text': a.get_text(' ', strip=True)})
        if len(rows) == limit:
            break
    return rows


class PublicRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        target = urllib.parse.urljoin(req.full_url, newurl)
        p = urllib.parse.urlparse(target)
        if p.scheme != 'https' or not any(p.hostname == d or (p.hostname or '').endswith('.' + d) for d in ('7m.com.cn', 'sina.com.cn')):
            raise urllib.error.HTTPError(target, code, 'external redirect stopped', headers, fp)
        if re.search(r'login|passport|register', target, re.I):
            raise urllib.error.HTTPError(target, code, 'login redirect stopped', headers, fp)
        return super().redirect_request(req, fp, code, msg, headers, target)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--date', required=True, help='7M source calendar date; not lottery sales date')
    parser.add_argument('--limit', type=int, default=10)
    args = parser.parse_args()
    datetime.strptime(args.date, '%Y-%m-%d')
    if not 1 <= args.limit <= 10:
        parser.error('limit must be 1..10')
    directory = ROOT / 'reports/source-probes/sevenm-sina' / datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S%fZ')
    (directory / 'raw').mkdir(parents=True, exist_ok=False)
    opener = urllib.request.build_opener(PublicRedirect())
    report = {'started_at': helpers.utc_now(), 'source_date': args.date, 'mode': 'isolated_public_probe',
              'vipc_untouched': True, 'pages': [], 'sevenm_sample': [], 'sina_sample': [],
              'limitations': ['No database, services, user sessions or VIPC accessed.',
                              'Published source arrays and page tables are evidence, not verified field mappings.',
                              '7M calendar-date sample and Sina upcoming sample are not paired matches.',
                              'No equivalent VIPC trade fields established.']}

    def save():
        (directory / 'report.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
        rows = []
        for p in report['pages']:
            ev = p.get('evidence', {})
            values = (p['name'], p['status'], p.get('error') or '', ev.get('title', ''),
                      len(ev.get('numeric_table_candidates', [])), p.get('javascript_redirect_shell', False))
            rows.append('<tr>' + ''.join('<td>' + html.escape(str(v)) + '</td>' for v in values) + '</tr>')
        (directory / 'report.html').write_text('''<!doctype html><meta charset="utf-8"><title>7M与新浪独立验证</title>
<style>body{font:15px system-ui;margin:32px}table{border-collapse:collapse}td,th{border:1px solid #ddd;padding:8px}</style>
<h1>7M与新浪独立公开页面验证</h1><p>未注册、付费或登录，未访问VIPC、数据库或业务服务。</p>
<p>7M按源站自然日抽取原始数组样本，新浪取页面公开的未来赛事链接；不是同一比赛池覆盖率对比。</p>
<p>HTTP 200 和数值候选不等于真实赔率已采集。菜单、文章和JavaScript跳转壳不能冒充完整字段。</p>
<p>原始数据、时间、SHA-256及样本见 report.json 和 raw。</p>
<table><tr><th>页面</th><th>HTTP</th><th>错误</th><th>标题</th><th>数值候选行</th><th>JS跳转壳</th></tr>'''
            + ''.join(rows) + '</table>', encoding='utf-8')

    def get(name, url):
        time.sleep(1.5)
        record = {'name': name, 'url': url, 'observed_at': helpers.utc_now(), 'status': None, 'error': None}
        body = b''
        try:
            req = urllib.request.Request(url, headers={'User-Agent': 'Mozilla/5.0'})
            with opener.open(req, timeout=12) as response:
                record.update(status=response.status, final_url=response.url)
                body = response.read(4 * 1024 * 1024 + 1)
                if len(body) > 4 * 1024 * 1024:
                    raise ValueError('response exceeds 4 MiB; not parsed')
        except urllib.error.HTTPError as exc:
            record.update(status=exc.code, error=str(exc))
            body = exc.read(65536)
        except Exception as exc:
            record['error'] = str(exc)
        document, encoding = helpers.decode_body(body)
        raw = directory / 'raw' / (name + '.bin')
        raw.write_bytes(body)
        record.update(raw_file=str(raw.relative_to(directory)), encoding=encoding, sha256=hashlib.sha256(body).hexdigest())
        if record['status'] == 200 and not record['error']:
            record['evidence'] = helpers.evidence(document, 'odds', '')
            record['javascript_redirect_shell'] = bool(re.search(r'window\.location\.replace\(', document))
        report['pages'].append(record)
        save()
        print(name, record['status'], record['error'] or '', flush=True)
        return record, document

    get('7m-home', 'https://www.7m.com.cn/')
    record, document = get('7m-results', 'https://data.7m.com.cn/result_data/default_gb.shtml?date=' + args.date)
    # This exact date-file template is present in the public results page.
    if record['status'] == 200 and '/result_data/' in document and '/index_gb.js' in document:
        record, document = get('7m-result-arrays', f'https://data.7m.com.cn/result_data/{args.date}/index_gb.js')
        if record['status'] == 200 and not record['error']:
            arrays = sevenm_arrays(document)
            report['sevenm_arrays'] = arrays
            report['sevenm_sample'], report['sevenm_validation'] = sevenm_sample(arrays, args.limit)
            save()
    get('sina-home', 'https://lottery.sina.com.cn/')
    record, document = get('sina-schedule', 'https://lottery.sina.com.cn/ai/?scrollTo=dc')
    if record['status'] == 200 and not record['error']:
        report['sina_sample'] = sina_detail_links(document, args.limit)
        save()
        for fixture in report['sina_sample']:
            get('sina-match-' + fixture['source_match_id'], fixture['url'])
    report['finished_at'] = helpers.utc_now()
    save()
    print('Report:', directory / 'report.html', flush=True)


if __name__ == '__main__':
    main()
