#!/usr/bin/env python3
"""Describe local finished fixtures by Beijing kickoff calendar and competition.

Offline analysis only. No collection, database writes, model fitting, or service
operation. A group's draw rate is its precision if every member is picked as a
draw; coverage always keeps all actual draws in the corresponding scope.
"""
from __future__ import annotations

import argparse
import calendar
import hashlib
import html
import json
import math
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
from pathlib import Path

BEIJING = timezone(timedelta(hours=8))
WEEKDAYS = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"]
TYPES = {"league": "联赛", "cup": "杯赛（国内＋洲际）", "nationalTournament": "国家队大赛", "qualifier": "预选赛", "friendly": "友谊赛", "unknown": "未知"}
FINE_TYPES = {"domesticLeague": "国内联赛", "domesticCup": "国内杯赛", "continentalCup": "洲际杯赛", "nationalTournament": "国家队大赛", "qualifier": "预选赛", "friendly": "友谊赛", "unknown": "未知"}


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def wilson(h, n):
    if not n:
        return [None, None]
    z = 1.959963984540054
    p, d = h / n, 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    w = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return [c - w, c + w]


def metrics(rows, total_draws, total_matches, base_rate):
    n = len(rows)
    h, d, a = [sum(r["result"] == i for r in rows) for i in (0, 1, 2)]
    days = Counter(r["kickoffDay"] for r in rows)
    selected = [r for r in rows if r.get("currentModelSelected")]
    hits = sum(r["result"] == 1 for r in selected)
    return dict(matches=n, home=h, draw=d, away=a, homeRate=h / n if n else None,
                drawRate=d / n if n else None, awayRate=a / n if n else None,
                drawCoverage=d / total_draws if total_draws else None,
                selectionRate=n / total_matches if total_matches else None,
                drawRateDifference=d / n - base_rate if n else None,
                drawRateWilson95=wilson(d, n), observedDays=len(days),
                maxDayShare=max(days.values()) / n if n else None,
                currentModelSelected=len(selected), currentModelHits=hits,
                currentModelPrecision=hits / len(selected) if selected else None,
                currentModelDrawCoverage=hits / total_draws if total_draws else None)


def group(rows, key, total_draws, total_matches, base_rate):
    buckets = defaultdict(list)
    for row in rows:
        value = key(row)
        if value is not None:
            buckets[value].append(row)
    result = []
    for value, members in sorted(buckets.items(), key=lambda item: str(item[0])):
        record = dict(key=value, **metrics(members, total_draws, total_matches, base_rate))
        if isinstance(value, str) and len(value) == 7 and value[4] == "-":
            year, month = map(int, value.split("-"))
            record["calendarDays"] = calendar.monthrange(year, month)[1]
            record["observedDayFraction"] = record["observedDays"] / record["calendarDays"]
        result.append(record)
    return result


def distributions(rows):
    n, draws = len(rows), sum(r["result"] == 1 for r in rows)
    base = draws / n if n else 0
    dimensions = {
        "month": lambda r: r["month"], "weekday": lambda r: r["weekday"],
        "type": lambda r: r["broadType"], "fineType": lambda r: r["type"],
        "competition": lambda r: r["competition"],
        "monthWeekday": lambda r: r["month"] + " / " + WEEKDAYS[r["weekday"]],
        "monthType": lambda r: r["month"] + " / " + TYPES[r["broadType"]],
        "weekdayType": lambda r: WEEKDAYS[r["weekday"]] + " / " + TYPES[r["broadType"]],
        "weekdayCompetition": lambda r: WEEKDAYS[r["weekday"]] + " / " + r["competition"],
    }
    result = dict(overview=metrics(rows, draws, n, base))
    result.update({name: group(rows, key, draws, n, base) for name, key in dimensions.items()})
    if rows:
        existing = {r["key"] for r in result["month"]}
        year, month = map(int, min(existing).split("-"))
        last = max(existing)
        while f"{year:04d}-{month:02d}" <= last:
            key = f"{year:04d}-{month:02d}"
            if key not in existing:
                result["month"].append(dict(key=key, **metrics([], draws, n, base), calendarDays=calendar.monthrange(year, month)[1], observedDayFraction=0))
            year, month = (year + 1, 1) if month == 12 else (year, month + 1)
        result["month"].sort(key=lambda r: r["key"])
    return result


def pct(value):
    return "—" if value is None else f"{value * 100:.2f}%"


def table(records, label, title):
    lines = [f"<h3>{html.escape(title)}</h3><table><thead><tr><th>分组</th><th>比赛</th><th>主胜</th><th>平</th><th>客胜</th><th>主胜率</th><th>平率/命中率</th><th>客胜率</th><th>平局覆盖</th><th>比赛日数</th><th>当前二选命中率</th></tr></thead><tbody>"]
    for row in records:
        lines.append("<tr>" + "".join(f"<td>{html.escape(str(v))}</td>" for v in [label(row["key"]), row["matches"], row["home"], row["draw"], row["away"], pct(row["homeRate"]), pct(row["drawRate"]), pct(row["awayRate"]), pct(row["drawCoverage"]), row["observedDays"], pct(row["currentModelPrecision"])]) + "</tr>")
    return "\n".join(lines) + "</tbody></table>"


def render(report, out):
    parts = ["<!doctype html><html lang='zh-CN'><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>月份、星期与赛事平局特征</title><style>body{font:15px system-ui,sans-serif;max-width:1450px;margin:30px auto;padding:0 20px;color:#17202a;background:#f7f9fc}table{border-collapse:collapse;background:white;width:100%;margin:12px 0 28px}td,th{padding:8px 10px;border:1px solid #dce2ec;text-align:right}td:first-child,th:first-child{text-align:left}th{background:#e8eef8;position:sticky;top:0}h2{margin-top:40px}p{line-height:1.7}.note{background:#fff3cd;padding:15px;border-radius:8px}summary{cursor:pointer;padding:12px;background:#e8eef8}</style><h1>月份、星期与赛事：完赛分布与平局覆盖</h1>"]
    parts.append("<p>按真实北京时间开赛日期，胜/平/负指主胜/平局/客胜。分组平率相当于选该组全部比赛猜平的命中率；覆盖率以该范围全部实际平局为分母。</p>")
    parts.append("<p class='note'>这是本地采样数据的统计探索，未修改线上规则。月份观测不连续，不能直接视为季节规律；赛事类型按本地名称分类，不代表官方赛制或小组/淘汰阶段。全量分组是探索回看，规则的近期复核需另看开发区/测试区结果。</p>")
    for scope, title in (("global", "全球本地完赛（包含竞彩）"), ("sporttery", "竞彩")):
        data = report["scopes"][scope]
        overview = data["overview"]
        parts.append(f"<h2>{title}</h2><p>{overview['matches']}场；主胜{overview['home']}、平{overview['draw']}、客胜{overview['away']}；整体平率{pct(overview['drawRate'])}；实际开赛日期{overview['observedDays']}天。</p>")
        parts.append(table(data["month"], str, "月份：必须结合观测日期数"))
        parts.append(table(sorted(data["weekday"], key=lambda r: r["key"]), lambda k: WEEKDAYS[k], "星期（实际北京时间开赛日）"))
        parts.append(table(data["type"], lambda k: TYPES[k], "联赛、杯赛及其他类型"))
        parts.append(table(data["fineType"], lambda k: FINE_TYPES[k], "赛事细分类"))
        minimum = 50 if scope == "global" else 20
        top = sorted([r for r in data["competition"] if r["matches"] >= minimum and r["observedDays"] >= 3], key=lambda r: (-r["drawRate"], -r["matches"]))[:20]
        parts.append(table(top, str, f"具体赛事平率排名：至少{minimum}场、3个比赛日（探索）"))
        for name, text in (("competition", "全部赛事"), ("monthWeekday", "月份×星期"), ("monthType", "月份×赛事类型"), ("weekdayType", "星期×赛事类型"), ("weekdayCompetition", "星期×具体赛事")):
            parts.append("<details><summary>" + text + "完整分布</summary>" + table(data[name], str, text) + "</details>")
    parts.append(table(report["purchaseWeekdayJC"], lambda k: WEEKDAYS[k] if isinstance(k, int) else k, "竞彩购买期星期（编号日期，不能与开赛星期混用）"))
    if report.get("rules"):
        parts.append("<h2>开发区选特征，近期区验证</h2><p>规则只在2026-10-24及以前选择，10月25日起只评价，不用测试结果挑规则。规则池最低样本、比赛日数和增量质量门槛见协议；以下并集均按比赛ID去重。</p><table><thead><tr><th>范围</th><th>特征族</th><th>规则数</th><th>开发选中/平</th><th>开发命中率</th><th>开发覆盖</th><th>近期选中/平</th><th>近期命中率</th><th>近期覆盖</th></tr></thead><tbody>")
        for row in report["rules"]["results"]:
            dev, test = row["development"], row["test"]
            values = [row["scope"], row["strategy"], row["selectedRuleCount"], f"{dev['selected']}/{dev['hits']}", pct(dev["precision"]), pct(dev["coverage"]), f"{test['selected']}/{test['hits']}", pct(test["precision"]), pct(test["coverage"])]
            parts.append("<tr>" + "".join(f"<td>{html.escape(str(v))}</td>" for v in values) + "</tr>")
        parts.append("</tbody></table><details><summary>已封存的具体规则及增量</summary><pre>" + html.escape(json.dumps([r for r in report["rules"]["results"] if r["strategy"] in ("all", "competition")], ensure_ascii=False, indent=2)) + "</pre></details>")
    if report.get("modelOverlay"):
        parts.append("<h2>与当前二次入选的叠加观察</h2><pre>" + html.escape(json.dumps(report["modelOverlay"], ensure_ascii=False, indent=2)) + "</pre>")
    parts.append("<h2>时间质量与口径</h2><pre>" + html.escape(json.dumps(report["timeQuality"], ensure_ascii=False, indent=2)) + "</pre><p>来源SHA256：" + report["sourceSHA256"] + "</p></html>")
    (out / "calendar-competition-report.html").write_text("\n".join(parts), encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--competition-map", type=Path, required=True)
    parser.add_argument("--model-rows", type=Path, required=True)
    parser.add_argument("--rules", type=Path)
    parser.add_argument("--model-overlay", type=Path)
    parser.add_argument("--out", type=Path, default=Path("reports/draw-calendar-competition-20261101"))
    args = parser.parse_args()
    source, mapping, model_source = read(args.source), read(args.competition_map), read(args.model_rows)
    models = {r["matchId"]: r for r in model_source["rows"]}
    assert len({r["matchId"] for r in source["rows"]}) == len(source["rows"])
    rows, mismatch, unknown_model, midnight = [], [], [], []
    for raw in source["rows"]:
        row = dict(raw)
        kickoff = datetime.fromisoformat(row["matchTime"].replace("Z", "+00:00")).astimezone(BEIJING)
        row.update(kickoffDay=kickoff.date().isoformat(), month=kickoff.strftime("%Y-%m"), weekday=kickoff.weekday(), sporttery=bool(str(row.get("jingcaiId") or "").strip()))
        category = mapping["mapping"][row["league"]]
        row.update(competition=category["normalizedName"], type=category["category"], broadType=category["broadCategory"])
        model = models.get(row["matchId"])
        row["currentModelSelected"] = bool(model and model.get("oldSelected") and model.get("oldScore", 0) >= .225 and model.get("sportteryQuotesAvailable"))
        if row["sporttery"] and model is None:
            unknown_model.append(row["matchId"])
        if row["date"] != row["kickoffDay"]:
            mismatch.append(row["matchId"])
        if kickoff.hour == 0 and kickoff.minute == 0:
            midnight.append(row["matchId"])
        row["purchaseWeekday"] = None
        if row["sporttery"]:
            try:
                row["purchaseWeekday"] = datetime.strptime(str(row["jingcaiId"])[:8], "%Y%m%d").weekday()
            except ValueError:
                pass
        assert row["result"] == (0 if row["homeScore"] > row["awayScore"] else 1 if row["homeScore"] == row["awayScore"] else 2)
        rows.append(row)
    jc = [r for r in rows if r["sporttery"]]
    scopes = dict(global_=rows, sporttery=jc, nonSporttery=[r for r in rows if not r["sporttery"]])
    report = dict(version="draw-calendar-competition-study-v1", generatedAt=datetime.now(timezone.utc).isoformat(), sourceGeneratedAt=source["generatedAt"],
        sourceSHA256=sha(args.source), competitionMapSHA256=sha(args.competition_map), modelRowsSHA256=sha(args.model_rows),
        definitions=dict(time="actual Asia/Shanghai kickoff, never UTC weekday", outcome="0 home win; 1 draw; 2 away win; confirmed regulation only", coverage="group draws / all actual draws in the given scope", classification="local name rules; domestic and continental cups aggregated; national tournaments, qualifiers and friendlies separate", deployed=False, currentModel="existing frozen model + score>=0.225 + actual opening/latest sporttery quotes; no H2H minimum"),
        scopes={"global" if key == "global_" else key: distributions(value) for key, value in scopes.items()},
        timeQuality=dict(observedDays=len({r["kickoffDay"] for r in rows}), sourceDayDifferentCount=len(mismatch), sourceDayDifferentIDs=mismatch, midnightCount=len(midnight), midnightIsNotAutomaticallyInvalid=True, modelInputsUnavailableIDs=unknown_model, knownCaptureTimePrematch=False),
        daily=group(rows, lambda r: r["kickoffDay"], sum(r["result"] == 1 for r in rows), len(rows), sum(r["result"] == 1 for r in rows) / len(rows)))
    report["purchaseWeekdayJC"] = group(jc, lambda r: r["purchaseWeekday"], sum(r["result"] == 1 for r in jc), len(jc), sum(r["result"] == 1 for r in jc) / len(jc))
    midnight_ids = set(midnight)
    report["sensitivityNoMidnight"] = {"global": distributions([r for r in rows if r["matchId"] not in midnight_ids]), "sporttery": distributions([r for r in jc if r["matchId"] not in midnight_ids])}
    source_day_rows = [dict(r, kickoffDay=r["date"], month=r["date"][:7], weekday=datetime.strptime(r["date"], "%Y-%m-%d").weekday()) for r in rows]
    report["sensitivitySourceScheduleDay"] = {"global": distributions(source_day_rows), "sporttery": distributions([r for r in source_day_rows if r["sporttery"]])}
    report["periods"] = {}
    for name, predicate in (("development", lambda r: r["kickoffDay"] <= "2026-10-24"), ("test", lambda r: r["kickoffDay"] > "2026-10-24")):
        report["periods"][name] = {"global": distributions([r for r in rows if predicate(r)]), "sporttery": distributions([r for r in jc if predicate(r)])}
    if args.rules:
        report["rules"] = read(args.rules)
        assert report["rules"]["protocol"]["sourceSHA256"] == report["sourceSHA256"]
        assert report["rules"]["deployed"] is False
    if args.model_overlay:
        report["modelOverlay"] = read(args.model_overlay)
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "analysis.json").write_text(json.dumps(report, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    render(report, args.out)
    print(json.dumps(dict(output=str(args.out), globalSummary=report["scopes"]["global"]["overview"], sportterySummary=report["scopes"]["sporttery"]["overview"]), ensure_ascii=False))


if __name__ == "__main__":
    main()
