#!/usr/bin/env python3
"""Reproducible, finite exploration of the user's draw rules and existing UI values.

Reads exported JSON only. Never connects to a database, starts a service, or
turns snapshot associations into pre-kickoff predictions. Selection sees only
the fixed training and calibration partitions; every failed candidate is kept.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import html
import json
import math
import random
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

EPS = 1e-9
PARTS = ("train", "calibration", "internalTest", "newDates", "all")
LABELS = {"train": "训练", "calibration": "校准", "internalTest": "历史内部复核", "newDates": "新增日期验证", "all": "全部快照"}
OUTCOMES = ("胜", "平", "负")


def number(v):
    return not isinstance(v, bool) and isinstance(v, (int, float)) and math.isfinite(v)


def vector(v, positive=False):
    return tuple(v) if isinstance(v, list) and len(v) == 3 and all(number(x) and (not positive or x > 0) for x in v) else None


def extrema(v, mode="min", absolute=False, count=1):
    vals = tuple(abs(x) for x in v) if absolute else v
    cutoff = sorted(vals, reverse=mode == "max")[count - 1]
    return {i for i, x in enumerate(vals) if x >= cutoff - EPS} if mode == "max" else {i for i, x in enumerate(vals) if x <= cutoff + EPS}


def category(v, phase):
    if v is None:
        return "missing"
    if 1 in extrema(v, "max", True):
        return "largest"
    if phase == "latest" and 1 in extrema(v, "max", True, 2):
        if v[1] < -EPS:
            return "negative"
        if v[1] > EPS:
            return "positive"
    return "none"


def profit_vector(market):
    """Model amount / its own stake base: identical ranking/sign to UI amounts.

    Do not mix in the unrelated officialProfitRate or use missing numeric zero.
    Reconstruct unrounded ROI from amounts rather than comparing rounded ROI.
    """
    if not market or market.get("oddsAvailable") is not True:
        return None
    values = []
    for outcome in ("home", "draw", "away"):
        entries = [r for r in market.get("bookmakerByOutcome", []) if r.get("outcome") == outcome]
        if len(entries) != 1:
            return None
        r = entries[0]
        if r.get("available") is not True or not number(r.get("odds")) or r["odds"] <= 0:
            return None
        amount, base = r.get("bookmakerProfit"), r.get("totalStake")
        if not number(amount) or not number(base) or base <= 0:
            return None
        values.append(amount / base * 100)
    return tuple(values)


def enrich(row, protocol, old_ids):
    r = dict(row)
    r["date"] = str(r["date"])[:10]
    if r["date"] <= protocol["partitions"]["trainEnd"]:
        r["split"] = "train"
    elif r["date"] <= protocol["partitions"]["calibrationEnd"]:
        r["split"] = "calibration"
    elif r["date"] <= protocol["oldLastDate"]:
        r["split"] = "internalTest"
    else:
        r["split"] = "newDates"
    r["backfilledOldDate"] = str(r["matchId"]) not in old_ids and r["date"] <= protocol["oldLastDate"]
    for phase in ("opening", "latest"):
        raw = r.get(phase) or {}
        w = vector(raw.get("william"), True)
        for source, name in (("ds", "sporttery"), ("da", "average")):
            p = vector(raw.get(name), True)
            diff = tuple(a - b for a, b in zip(w, p)) if w and p else None
            r[f"{phase}.{source}"] = diff
            r[f"{phase}.{source}.class"] = category(diff, phase)
        r[f"{phase}.average"] = vector(raw.get("average"), True)
    heat = r.get("homeHeat")
    r["heat"] = heat if number(heat) and 0 <= heat <= 100 else None
    markets = (r.get("roiSimulation") or {}).get("markets") or []
    jc = [m for m in markets if m.get("key") == "sporttery" and m.get("simulated") is not True]
    sim = [m for m in markets if m.get("simulated") is True and m.get("key") in ("sporttery", "sportterySim")]
    if len(jc) > 1 or len(sim) > 1:
        raise ValueError(f"Ambiguous profit market {r['matchId']}")
    r["profit.sporttery"] = profit_vector(jc[0] if jc else None)
    r["profit.simulated"] = profit_vector(sim[0] if sim else None)
    r["simulationSource"] = sim[0]["key"] if sim else "missing"
    r["sportteryDistributionSource"] = (r.get("profitInputs") or {}).get("distributionSources", {}).get("sporttery", "missing")
    official = None
    presence = (r.get("profitInputs") or {}).get("officialProfitRatePresence", [])
    if jc and jc[0].get("oddsAvailable") is True and presence == [True, True, True]:
        by = {v["outcome"]: v for v in jc[0].get("bookmakerByOutcome", [])}
        values = [by.get(key, {}).get("officialProfitRate") for key in ("home", "draw", "away")]
        if all(number(by.get(key, {}).get("odds")) and by[key]["odds"] > 0 for key in ("home", "draw", "away")):
            official = vector(values)
    r["profit.official"] = official
    history = r.get("history") or {}
    hazards = ("usedRecentAtOrAfterKickoff", "usedRecentTargetId", "usedRecentUnknownTime", "againstListedAtOrAfterKickoff", "againstListedTargetId", "againstListedUnknownTime")
    r["historyListedClean"] = bool(history.get("present")) and all(number(history.get(k)) and history[k] == 0 for k in hazards)
    # This does not prove that the aggregate summary or capture preceded kickoff.
    r["historyCapturePreKickoff"] = history.get("capturedPreKickoff") is True
    r["captureConfirmed"] = all((r.get("timing") or {}).get(k) is True for k in ("euroPreKickoff", "linePreKickoff"))
    return r


@dataclass
class Rule:
    id: str
    label: str
    required: tuple[str, ...]
    predicate: object
    base_id: str | None = None
    gate_ids: tuple[str, ...] = ()


def rules():
    bases = []
    names = {"ds": "体彩差值", "da": "均赔差值"}
    for source, label in names.items():
        key = f"opening.{source}"
        bases.append(Rule(f"opening_{source}_largest", f"初盘{label}：平绝对值最大", (key,), lambda r, k=key: r[k + ".class"] == "largest"))
    titles = {"largest": "平绝对值最大", "negative": "负数含平", "positive": "正数含平"}
    for source, label in names.items():
        key = f"latest.{source}"
        for kind, title in titles.items():
            bases.append(Rule(f"latest_{source}_{kind}", f"即时{label}：{title}", (key,), lambda r, k=key, c=kind: r[k + ".class"] == c))
    hit = lambda r, k: r[k + ".class"] in titles
    bases.append(Rule("opening_both", "初盘两组平绝对值都最大", ("opening.ds", "opening.da"), lambda r: r["opening.ds.class"] == r["opening.da.class"] == "largest"))
    bases.append(Rule("latest_both", "即时两组都符合指定三类之一", ("latest.ds", "latest.da"), lambda r: hit(r, "latest.ds") and hit(r, "latest.da")))
    for source, label in names.items():
        op, la = f"opening.{source}", f"latest.{source}"
        bases.append(Rule(f"consistent_{source}", f"{label}初盘最大且即时符合", (op, la), lambda r, o=op, l=la: r[o + ".class"] == "largest" and hit(r, l)))
    gates = []
    for source, label in (("sporttery", "竞彩测算"), ("simulated", "胜平负模拟")):
        key = f"profit.{source}"
        gates.append(Rule(f"{source}_negative", f"{label}平项亏损", (key,), lambda r, k=key: r[k][1] < -EPS))
        gates.append(Rule(f"{source}_minimum", f"{label}平项利润最低", (key,), lambda r, k=key: 1 in extrema(r[k])))
    gates.extend([
        Rule("heat_balanced", "亚盘热度均衡（主40%～60%）", ("heat",), lambda r: 40 <= r["heat"] <= 60),
        Rule("heat_one_sided", "亚盘单侧热度≥70%", ("heat",), lambda r: max(r["heat"], 100 - r["heat"]) >= 70),
        Rule("both_profit_minimum", "竞彩与模拟平项利润都最低", ("profit.sporttery", "profit.simulated"), lambda r: 1 in extrema(r["profit.sporttery"]) and 1 in extrema(r["profit.simulated"])),
    ])
    variants = [()] + [(g,) for g in gates] + [(p, h) for p in gates[:4] for h in gates[4:6]] + [(gates[6], h) for h in gates[4:6]]
    output = []
    for base in bases:
        for added in variants:
            required = tuple(dict.fromkeys(base.required + tuple(k for g in added for k in g.required)))
            output.append(Rule(base.id + "".join("__" + g.id for g in added), base.label + "".join(" ＋ " + g.label for g in added), required,
                               lambda r, b=base, gs=added: b.predicate(r) and all(g.predicate(r) for g in gs), base.id if added else None, tuple(g.id for g in added)))
    output.extend(Rule("gate_" + g.id, g.label, g.required, g.predicate, gate_ids=(g.id,)) for g in gates)
    assert len(bases) == 12 and len(output) == 223 and len({r.id for r in output}) == 223
    return output, {r.id: r for r in bases}, gates


def wilson(d, n):
    if not n:
        return None
    z = 1.959963984540054
    p = d / n
    base = 1 + z * z / n
    center = (p + z * z / (2 * n)) / base
    radius = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / base
    return [max(0, center - radius), min(1, center + radius)]


def stats(rows):
    n, d = len(rows), sum(r["result"] == 1 for r in rows)
    return {"n": n, "draws": d, "rate": d / n if n else None, "wilson95": wilson(d, n)}


def fisher_enrichment(selected, unselected):
    a, b = selected["draws"], selected["n"] - selected["draws"]
    c, d = unselected["draws"], unselected["n"] - unselected["draws"]
    n, k, m = a + b + c + d, a + c, a + b
    if not n or not m or m == n:
        return 1.0
    logchoose = lambda total, count: math.lgamma(total + 1) - math.lgamma(count + 1) - math.lgamma(total - count + 1)
    constant = logchoose(n, m)
    return min(1.0, sum(math.exp(logchoose(k, x) + logchoose(n - k, m - x) - constant) for x in range(a, min(k, m) + 1)))


def market_stats(rows):
    priced = [r for r in rows if r["latest.average"]]
    ps = [(1 / r["latest.average"][1]) / sum(1 / x for x in r["latest.average"]) for r in priced]
    actual = sum(r["result"] == 1 for r in priced)
    return {"n": len(priced), "draws": actual, "rate": actual / len(priced) if priced else None,
            "meanImpliedDraw": sum(ps) / len(ps) if ps else None,
            "observedMinusImpliedPP": (actual - sum(ps)) / len(ps) * 100 if ps else None}


def evaluate(rule, rows, bases):
    eligible = [r for r in rows if all(r.get(k) is not None for k in rule.required)]
    matched = [r for r in eligible if rule.predicate(r)]
    partitions = {}
    for split in PARTS:
        pool = eligible if split == "all" else [r for r in eligible if r["split"] == split]
        hits = matched if split == "all" else [r for r in matched if r["split"] == split]
        chosen, baseline = stats(hits), stats(pool)
        unselected = {"n": baseline["n"] - chosen["n"], "draws": baseline["draws"] - chosen["draws"]}
        unselected["rate"] = unselected["draws"] / unselected["n"] if unselected["n"] else None
        parent = stats([r for r in pool if bases[rule.base_id].predicate(r)]) if rule.base_id else None
        recall = chosen["draws"] / baseline["draws"] if baseline["draws"] else None
        non_draws = baseline["n"] - baseline["draws"]
        false_positive = (chosen["n"] - chosen["draws"]) / non_draws if non_draws else None
        partitions[split] = {**chosen, "eligible": baseline, "unselected": unselected, "parentSameInputs": parent,
                             "liftPP": (chosen["rate"] - baseline["rate"]) * 100 if chosen["n"] else None,
                             "incrementalLiftPP": (chosen["rate"] - parent["rate"]) * 100 if parent and chosen["n"] and parent["n"] else None,
                             "selectionRate": chosen["n"] / baseline["n"] if baseline["n"] else None,
                             "drawRecall": recall, "nonDrawFeatureRate": false_positive,
                             "positiveLikelihoodRatio": recall / false_positive if recall is not None and false_positive else None,
                             "fisherEnrichmentP": fisher_enrichment(chosen, unselected), "market": market_stats(hits)}
    return {"id": rule.id, "label": rule.label, "requirements": list(rule.required), "baseId": rule.base_id,
            "gateIds": list(rule.gate_ids), "partitions": partitions}, eligible, matched


def adjust_bh(results):
    ordered = sorted(results, key=lambda r: r["partitions"]["train"]["fisherEnrichmentP"])
    q = 1.0
    for index in range(len(ordered) - 1, -1, -1):
        q = min(q, ordered[index]["partitions"]["train"]["fisherEnrichmentP"] * len(ordered) / (index + 1))
        ordered[index]["trainBHq"] = min(1.0, q)


def select(results):
    """Selection must not touch internalTest/newDates fields or outcomes."""
    qualified = []
    for result in results:
        if result["id"].startswith("gate_"):
            continue
        a, b = (result["partitions"][s] for s in ("train", "calibration"))
        if a["n"] < 60 or b["n"] < 15 or a["liftPP"] <= 0 or b["liftPP"] <= 0:
            continue
        if result["baseId"] and (a["incrementalLiftPP"] <= 0 or b["incrementalLiftPP"] <= 0):
            continue
        qualified.append(result)
    qualified.sort(key=lambda r: (-min(r["partitions"][s]["liftPP"] for s in ("train", "calibration")),
                                  -(r["partitions"]["calibration"]["wilson95"][0] - r["partitions"]["calibration"]["eligible"]["rate"]),
                                  -r["partitions"]["calibration"]["n"], r["id"]))
    return qualified


def grouped(rule, rows, bases, field):
    groups = defaultdict(list)
    for row in rows:
        if field == "month":
            key = row["date"][:7]
        elif field == "heat":
            h = row["heat"]
            key = "缺失" if h is None else "主热≥70" if h >= 70 else "客热≥70" if h <= 30 else "均衡40～60" if 40 <= h <= 60 else "其余"
        elif field == "marketProbability":
            av = row["latest.average"]
            p = (1 / av[1]) / sum(1 / x for x in av) if av else None
            key = "缺失" if p is None else "<20%" if p < .2 else "20%～25%" if p < .25 else "25%～30%" if p < .3 else "≥30%"
        elif field == "historyListedClean":
            key = "列出历史无明显污染" if row[field] else "缺失/未知/有污染"
        else:
            key = str(row.get(field) or "缺失")
        groups[key].append(row)
    output = []
    for key, items in sorted(groups.items()):
        result, _, _ = evaluate(rule, items, bases)
        output.append({"group": key, "partitions": result["partitions"]})
    return output


def cluster_bootstrap(rule, rows, bases, repetitions=1200):
    """Resample complete schedule dates, retaining paired base/child counts."""
    bydate = defaultdict(list)
    for r in rows:
        if all(r.get(k) is not None for k in rule.required):
            bydate[r["date"]].append(r)
    aggregates = []
    for items in bydate.values():
        hits = [r for r in items if rule.predicate(r)]
        parent = [r for r in items if bases[rule.base_id].predicate(r)] if rule.base_id else []
        aggregates.append((len(items), sum(r["result"] == 1 for r in items), len(hits), sum(r["result"] == 1 for r in hits), len(parent), sum(r["result"] == 1 for r in parent)))
    if len(aggregates) < 2:
        return {"dates": len(aggregates), "validReplicates": 0, "reason": "少于2个赛程日，无法估计按日区间"}
    rng = random.Random(20261024)
    lifts, rates, increments = [], [], []
    for _ in range(repetitions):
        total = [sum(values) for values in zip(*(aggregates[rng.randrange(len(aggregates))] for _ in aggregates))]
        n, d, m, h, pn, pd = total
        if m and n:
            rates.append(h / m)
            lifts.append((h / m - d / n) * 100)
            if pn:
                increments.append((h / m - pd / pn) * 100)
    def interval(values):
        values = sorted(values)
        return [values[int((len(values) - 1) * q)] for q in (.025, .975)] if values else None
    return {"dates": len(aggregates), "validReplicates": len(lifts), "requestedReplicates": repetitions,
            "fewDateWarning": "不足10个独立赛程日，按日重抽区间可能退化，不能据此认定稳定；同时查看比赛比例Wilson区间" if len(aggregates) < 10 else None,
            "rate95": interval(rates), "sameInputsLiftPP95": interval(lifts), "incrementalLiftPP95": interval(increments)}


def sensitivity(rule, rows, bases):
    filters = {
        "allSnapshotRows": lambda r: True,
        "historyListedClean": lambda r: r["historyListedClean"],
        "historyCaptureBeforeKickoff": lambda r: r["historyCapturePreKickoff"],
        "euroAndLineCaptureBeforeKickoff": lambda r: r["captureConfirmed"],
        "sportteryDistributionFromSource": lambda r: r["sportteryDistributionSource"] in ("jyykSpf", "tzbl"),
        "simulatedSportteryCompare": lambda r: r["simulationSource"] == "sportterySim",
        "simulatedNonSportteryFallback": lambda r: r["simulationSource"] == "sporttery",
        "noZeroDifferenceTie": lambda r: all(r[k] is not None and any(abs(v) > EPS for v in r[k]) for k in rule.required if k.endswith((".ds", ".da"))),
    }
    output = {key: evaluate(rule, [r for r in rows if predicate(r)], bases)[0]["partitions"] for key, predicate in filters.items()}
    if "profit.sporttery" in rule.required:
        substituted = []
        for r in rows:
            if r["profit.official"] is not None:
                substituted.append({**r, "profit.sporttery": r["profit.official"]})
        output["replaceSportteryModeledWithActuallyPresentOfficialRates"] = evaluate(rule, substituted, bases)[0]["partitions"]
    return output


def diagnostic_profit(rows):
    results = []
    for key in ("profit.sporttery", "profit.simulated", "profit.official"):
        for name, predicate in (("negative", lambda v: v[1] < -EPS), ("zero", lambda v: abs(v[1]) <= EPS),
                                ("positive", lambda v: v[1] > EPS), ("minimum", lambda v: 1 in extrema(v)),
                                ("maximum", lambda v: 1 in extrema(v, "max"))):
            rule = Rule(key + "_" + name, key + " " + name, (key,), lambda r, k=key, p=predicate: p(r[k]))
            result, _, _ = evaluate(rule, rows, {})
            results.append(result)
    return results


def pct(v):
    return "暂无" if v is None else f"{v * 100:.2f}%"


def metric(v):
    if not v["n"]:
        return "0场"
    baseline = v["eligible"]
    ci = v["wilson95"]
    text = f'{v["draws"]}/{v["n"]}，{pct(v["rate"])}；95%区间 {pct(ci[0])}～{pct(ci[1])}；同输入基准 {baseline["draws"]}/{baseline["n"]}={pct(baseline["rate"])}；提升 {v["liftPP"]:+.2f}个百分点'
    if v["parentSameInputs"]:
        text += f'；父条件 {pct(v["parentSameInputs"]["rate"])}，增量 {v["incrementalLiftPP"]:+.2f}个百分点'
    return text


def write_html(path, report):
    esc = lambda v: html.escape(str(v))
    def table(results):
        head = "<tr><th>固定条件</th>" + "".join(f"<th>{LABELS[s]}</th>" for s in PARTS) + "</tr>"
        body = "".join('<tr data-search="' + esc(r["label"]) + '"><td>' + esc(r["label"]) + '<br><small>' + esc(r["id"]) + '</small></td>' + ''.join('<td>' + esc(metric(r["partitions"][s])) + '</td>' for s in PARTS) + '</tr>' for r in results)
        return '<div class="scroll"><table>' + head + body + '</table></div>'
    doc = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>平局条件、盈亏与热度组合研究</title><style>
body{font:14px/1.65 system-ui,sans-serif;max-width:1900px;margin:auto;padding:24px;background:#111820;color:#e2e9ef}h1,h2{line-height:1.3}h2{margin-top:36px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #3b4753;padding:10px;vertical-align:top;min-width:150px}th{background:#243442;position:sticky;top:0}td:first-child{min-width:260px}small{color:#aab9c8}.scroll{overflow:auto;max-height:720px}.note{padding:16px;background:#213342;border-left:4px solid #73b6ef}.caution{padding:16px;background:#493323}input{padding:10px;min-width:360px;background:#22313e;color:white;border:1px solid #6b8193}pre{white-space:pre-wrap;word-break:break-word}details{margin:12px 0}a{color:#9acaff}</style><h1>平局条件、盈亏与亚盘热度：固定组合研究</h1>'''
    doc += '<p>' + esc(f'本次确认完赛 {report["dataset"]["n"]} 场，平局 {report["dataset"]["draws"]} 场（{pct(report["dataset"]["rate"])}）；固定候选223条。') + '</p>'
    doc += '<p class="note">初盘只看平绝对差最大；即时每组按最大→负数含平→正数含平互斥归类，含平取绝对值最大的两项。体彩差值=威廉−竞彩，均赔差值=威廉−均赔。盈亏主分析统一为页面测算金额除以测算基数所得的庄家ROI；不混用源站官方率。正数庄家赚，负数庄家赔。金额与ROI不算两票。</p>'
    doc += '<p class="caution">本报告是已有快照的历史关联。模拟盈亏、亚盘热度与赔率/历史同源，且历史列表没有统一赛前截断；缺少赛前捕获证据时不能称实际赛前命中率。历史内部复核区已用于此前多次研究，不是全新独立测试。主条件仅由训练、校准选定；新增日期作锁定后的复核，完整固定规则结果公开，不据此重选冠军。</p>'
    doc += '<h2>本轮评估</h2><div class="note">' + ''.join('<p>' + esc(v) + '</p>' for v in report["assessment"]["findings"]) + '</div>'
    doc += '<h2>训练、校准选出的一个主观察条件</h2>'
    primary = next((r for r in report["candidates"] if r["id"] == report["primaryRuleId"]), None)
    if primary:
        doc += '<p><b>' + esc(primary["label"]) + '</b></p>' + table([primary])
        doc += '<p>训练223条完整比较的BH校正q：' + esc(f'{primary["trainBHq"]:.4f}') + '。比例的95% Wilson区间、按日成对自助区间及分层结果见下方。仅为观察候选，不把样本命中率当单场概率。</p>'
    else:
        doc += '<p>没有条件同时达到训练≥60场、校准≥15场、两区正提升，且组合相对同资格父条件也有正增量。不强行选型。</p>'
    doc += '<h2>原有12个条件的直接核对</h2>' + table([r for r in report["candidates"] if r["id"] in report["baseIds"]])
    doc += '<h2>盈亏、热度单独对照</h2>' + table([r for r in report["candidates"] if r["id"].startswith("gate_")])
    doc += '<h2>平项利润最低与最高、正负方向对照</h2>' + table(report["profitDirections"])
    doc += '<h2>全部223个预定组合（包括未通过和失败条件）</h2><p>每格平局数/筛选场数；同输入基准分母仅为本规则数据齐全的比赛。组合增量比较把父子都限制在相同输入集合。表格保持固定规则顺序，不按新日期结果排名。</p><input id="search" placeholder="搜索条件，例如 初盘、模拟、均衡">' + table(report["candidates"])
    if primary:
        doc += '<h2>主条件逐场明细（包含未命中的比赛）</h2><p>可按赛果、时间分区和比赛ID过滤。完整明细CSV与analysis.json位于本报告同目录。</p><input id="matchSearch" placeholder="比赛ID、球队、联赛"><select id="outcome"><option value="all">全部赛果</option><option value="1">平局</option><option value="0">主胜</option><option value="2">客胜</option></select><div class="scroll"><table id="matches"><tr><th>赛程日/分区</th><th>比赛</th><th>比分</th><th>亚盘主热度</th><th>初盘差值（胜/平/负）</th><th>即时差值</th><th>测算ROI（竞彩/模拟）</th></tr>'
        for r in report["primaryMatches"]:
            diffs = lambda p: '<br>'.join(esc(k + '：' + (' / '.join(f'{v:+.3f}' for v in r["derived"][p + '.' + k]) if r["derived"][p + '.' + k] is not None else '暂无')) for k in ('ds', 'da'))
            profits = '<br>'.join(esc(k + '：' + (' / '.join(f'{v:+.2f}%' for v in r["derived"]['profit.' + k]) if r["derived"]['profit.' + k] is not None else '暂无')) for k in ('sporttery', 'simulated'))
            doc += '<tr data-outcome="' + str(r["result"]) + '"><td>' + esc(r['date'] + ' / ' + LABELS[r['split']]) + '</td><td>' + esc(r['matchId'] + ' ' + r['league'] + ' ' + r['home'] + '－' + r['guest']) + '</td><td>' + esc(f'{r["homeScore"]}:{r["awayScore"]} {OUTCOMES[r["result"]]}') + '</td><td>' + esc(r['homeHeat']) + '</td><td>' + diffs('opening') + '</td><td>' + diffs('latest') + '</td><td>' + profits + '</td></tr>'
        doc += '</table></div>'
    for key, title in (("coverage", "数据覆盖和口径"), ("partitions", "固定时间分区"), ("kickoffAudit", "实际开球边界核对"), ("bootstrap", "主条件按赛程日成对自助区间"), ("strata", "主条件的时间、联赛、赔率与热度分层"), ("leaveOneLeagueOut", "主条件逐联赛剔除"), ("sensitivity", "缺失、污染与官方率替代敏感性"), ("selection", "只读取训练和校准的选型记录")):
        doc += '<h2>' + title + '</h2><details><summary>展开完整结果</summary><pre>' + esc(json.dumps(report[key], ensure_ascii=False, indent=2)) + '</pre></details>'
    doc += '<script>document.querySelector("#search").addEventListener("input",e=>document.querySelectorAll("tr[data-search]").forEach(r=>r.hidden=!r.dataset.search.includes(e.target.value.trim())));const ms=document.querySelector("#matchSearch"),out=document.querySelector("#outcome");function filter(){document.querySelectorAll("#matches tr[data-outcome]").forEach(r=>r.hidden=!r.textContent.includes(ms.value.trim())||(out.value!=="all"&&r.dataset.outcome!==out.value));}if(ms){ms.addEventListener("input",filter);out.addEventListener("change",filter);}</script></html>'
    path.write_text(doc, encoding="utf-8")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("dataset", type=Path)
    parser.add_argument("--out", type=Path, default=Path("reports/draw-feature-reassembly"))
    parser.add_argument("--old", type=Path, default=Path("reports/draw-feature-study/dataset.json"))
    args = parser.parse_args()
    protocol_path = args.out / "protocol.json"
    protocol = json.loads(protocol_path.read_text(encoding="utf-8-sig"))
    raw = json.loads(args.dataset.read_text(encoding="utf-8-sig"))
    old = json.loads(args.old.read_text(encoding="utf-8-sig"))
    assert hashlib.sha256(args.old.read_bytes()).hexdigest() == protocol["oldDatasetSHA256"]
    old_ids = {str(r["matchId"]) for r in old["rows"]}
    rows = [enrich(r, protocol, old_ids) for r in raw["rows"]]
    assert len(rows) == len({str(r["matchId"]) for r in rows})
    assert all(r["result"] in (0, 1, 2) and r["result"] == (1 if r["homeScore"] == r["awayScore"] else 0 if r["homeScore"] > r["awayScore"] else 2) for r in rows)
    candidates, bases, gates = rules()
    evaluated = [evaluate(rule, rows, bases) for rule in candidates]
    results = [item[0] for item in evaluated]
    adjust_bh(results)
    qualified = select(results)
    # This list is frozen before any robustness output is examined.
    primary_id = qualified[0]["id"] if qualified else None
    primary = next((r for r in candidates if r.id == primary_id), None)
    primary_matches = next((item[2] for item in evaluated if item[0]["id"] == primary_id), [])
    coverage = {k: sum(r.get(k) is not None for r in rows) for k in ("opening.ds", "opening.da", "latest.ds", "latest.da", "profit.sporttery", "profit.simulated", "profit.official", "heat")}
    coverage.update({"simulationSources": dict(Counter(r["simulationSource"] for r in rows)),
                     "validSportteryDistributionSources": dict(Counter(r["sportteryDistributionSource"] for r in rows if r["profit.sporttery"] is not None)),
                     "historyListedClean": sum(r["historyListedClean"] for r in rows), "historyCapturePreKickoff": sum(r["historyCapturePreKickoff"] for r in rows),
                     "euroAndLineCaptureConfirmed": sum(r["captureConfirmed"] for r in rows), "newIdsOnOldDates": sum(r["backfilledOldDate"] for r in rows), "sourceAudit": raw.get("audit")})
    report = {"version": "draw-feature-reassembly-v1", "datasetSHA256": hashlib.sha256(args.dataset.read_bytes()).hexdigest(),
              "scriptSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "protocolSHA256": hashlib.sha256(protocol_path.read_bytes()).hexdigest(),
              "generatedAt": raw.get("generatedAt"), "dataset": stats(rows), "coverage": coverage, "candidateCount": len(results), "baseIds": list(bases),
              "partitions": {s: {**stats([r for r in rows if r["split"] == s]), "dates": sorted({r["date"] for r in rows if r["split"] == s})} for s in PARTS[:-1]},
              "selection": {"qualifiedDevelopmentCount": len(qualified), "developmentRanking": [r["id"] for r in qualified], "usesTestOutcomes": False, "protocol": protocol["selection"]},
              "primaryRuleId": primary_id, "candidates": results, "profitDirections": diagnostic_profit(rows),
              "bootstrap": {}, "strata": {}, "leaveOneLeagueOut": [], "sensitivity": {}, "primaryMatches": []}
    bounds = {}
    for split in PARTS[:-1]:
        times = sorted(datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00")) for r in rows if r["split"] == split)
        bounds[split] = {"first": times[0].isoformat(), "last": times[-1].isoformat()} if times else None
    report["kickoffAudit"] = {"bounds": bounds, "adjacentPartitionOverlap": [
        {"left": a, "right": b, "overlap": datetime.fromisoformat(bounds[a]["last"]) >= datetime.fromisoformat(bounds[b]["first"])}
        for a, b in zip(PARTS[:-2], PARTS[1:-1]) if bounds[a] and bounds[b]]}
    if primary:
        report["bootstrap"] = {s: cluster_bootstrap(primary, rows if s == "all" else [r for r in rows if r["split"] == s], bases) for s in PARTS}
        report["strata"] = {field: grouped(primary, rows, bases, field) for field in ("date", "month", "league", "heat", "marketProbability", "simulationSource", "sportteryDistributionSource", "historyListedClean")}
        for league in sorted({r["league"] for r in primary_matches}):
            report["leaveOneLeagueOut"].append({"excludedLeague": league, "partitions": evaluate(primary, [r for r in rows if r["league"] != league], bases)[0]["partitions"]})
        report["sensitivity"] = sensitivity(primary, rows, bases)
        for r in primary_matches:
            report["primaryMatches"].append({k: r.get(k) for k in ("matchId", "date", "matchTime", "league", "home", "guest", "homeScore", "awayScore", "result", "split", "homeHeat", "simulationSource", "historyListedClean")} | {"derived": {k: r[k] for k in ("opening.ds", "opening.da", "latest.ds", "latest.da", "profit.sporttery", "profit.simulated", "profit.official")}})
    findings = []
    if primary:
        result = next(r for r in results if r["id"] == primary_id)
        findings.append('训练和校准锁定的主观察条件：' + result["label"] + '。全量快照：' + metric(result["partitions"]["all"]) + '。')
        for split in ("internalTest", "newDates"):
            findings.append(LABELS[split] + '：' + metric(result["partitions"][split]) + '。')
        findings.append('主条件训练BH校正q=' + f'{result["trainBHq"]:.4f}' + '；223个候选中q≤0.05的数量为' + str(sum(r["trainBHq"] <= .05 for r in results)) + '。不能据全量命中率认定稳定提升。')
    else:
        findings.append('没有候选通过预定训练/校准最低样本及提升要求，不强行选择主条件。')
    for key in ("gate_sporttery_minimum", "gate_simulated_minimum"):
        result = next(r for r in results if r["id"] == key)
        findings.append('固定盈亏对照：' + result["label"] + '；' + metric(result["partitions"]["all"]) + '；同一批平均欧赔去水隐含平率为' + pct(result["partitions"]["all"]["market"]["meanImpliedDraw"]) + '。对照不参与主条件选型。')
    findings.append('金额与ROI不重复计票；均赔差、亚盘热度和模拟盈亏存在共同计算来源。全部赔率/盘口/历史的独立捕获时间缺失，本轮结果是快照关联，不能称赛前实测命中率。')
    report["assessment"] = {"status": "exploratory_snapshot_association", "stableHighHitFeatureEstablished": False, "findings": findings}
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "analysis.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    write_html(args.out / "report.html", report)
    fields = ["id", "label", "trainBHq"] + [s + "." + k for s in PARTS for k in ("n", "draws", "rate", "liftPP", "incrementalLiftPP")]
    with (args.out / "candidates.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=fields)
        writer.writeheader()
        for result in results:
            writer.writerow({**{k: result[k] for k in ("id", "label", "trainBHq")}, **{s + "." + k: result["partitions"][s][k] for s in PARTS for k in ("n", "draws", "rate", "liftPP", "incrementalLiftPP")}})
    with (args.out / "primary-matches.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        fields = ["matchId", "date", "split", "league", "home", "guest", "homeScore", "awayScore", "result", "homeHeat", "simulationSource", "historyListedClean", "derived"]
        writer = csv.DictWriter(stream, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        for r in report["primaryMatches"]:
            writer.writerow({**r, "derived": json.dumps(r["derived"], ensure_ascii=False)})
    summary = {"dataset": report["dataset"], "coverage": {k: v for k, v in coverage.items() if k != "sourceAudit"}, "partitions": {s: {k: v for k, v in item.items() if k != "dates"} for s, item in report["partitions"].items()}, "primaryRuleId": primary_id, "qualifiedDevelopmentCount": len(qualified)}
    if primary_id:
        selected = next(r for r in results if r["id"] == primary_id)
        summary["primary"] = {"label": selected["label"], "trainBHq": selected["trainBHq"], "partitions": selected["partitions"], "bootstrap": report["bootstrap"]}
    (args.out / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False))


if __name__ == "__main__":
    main()
