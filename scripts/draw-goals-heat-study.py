#!/usr/bin/env python3
"""Fixed, supplemental goals-heat exploration on read-only exported snapshots."""
from __future__ import annotations

import argparse
import csv
import hashlib
import html
import importlib.util
import json
import sys
from collections import Counter, defaultdict
from datetime import datetime
from pathlib import Path

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("draw_reassembly_core", ROOT / "scripts/draw-feature-reassembly.py")
core = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = core
spec.loader.exec_module(core)
PARTS = ("train", "calibration", "internalTest", "recentInternal", "unseenTimes", "all")
LABELS = {"train": "训练", "calibration": "校准", "internalTest": "较早内部复核", "recentInternal": "近期重复复核", "unseenTimes": "新时间未见样本", "all": "全部快照"}
core.PARTS, core.LABELS = PARTS, LABELS


def stamp(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def fingerprint(row):
    keys = ("date", "matchTime", "result", "homeScore", "awayScore", "opening", "latest", "homeHeat", "asianOpening", "asianLine", "roiSimulation", "profitInputs", "history")
    value = {k: row.get(k) for k in keys}
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


def enrich(raw, protocol, prior):
    # Preserve the original phase/price/profit rules without moving train/cal.
    shim = {"partitions": protocol["partitions"], "oldLastDate": "2026-10-22"}
    r = core.enrich(raw, shim, set(prior))
    unseen = str(r["matchId"]) not in prior and stamp(r["matchTime"]) > stamp(protocol["priorLastActualKickoff"])
    if unseen:
        r["split"] = "unseenTimes"
    elif r["date"] > protocol["partitions"]["calibrationEnd"]:
        r["split"] = "internalTest" if r["date"] <= "2026-10-22" else "recentInternal"
    goals = r.get("goalsHeat") or {}
    over, under = goals.get("over"), goals.get("under")
    valid = goals.get("available") is True and core.number(over) and core.number(under) and 0 <= over <= 100 and 0 <= under <= 100 and abs(over + under - 100) < .011
    r["goalsHeat.over"] = over if valid else None
    r["goalsHeat.under"] = under if valid else None
    r["unchangedSincePrior"] = str(r["matchId"]) in prior and fingerprint(raw) == fingerprint(prior[str(r["matchId"])])
    return r


def rules():
    original, base_rules, _ = core.rules()
    gates = [
        core.Rule("goals_over70", "大球热度≥70%", ("goalsHeat.over",), lambda r: r["goalsHeat.over"] >= 70),
        core.Rule("goals_under70", "小球热度≥70%", ("goalsHeat.under",), lambda r: r["goalsHeat.under"] >= 70),
        core.Rule("goals_balanced", "大小球热度均衡（大40%～60%）", ("goalsHeat.over",), lambda r: 40 <= r["goalsHeat.over"] <= 60),
    ]
    added = []
    for parent in original:
        for gate in gates:
            added.append(core.Rule(parent.id + "__" + gate.id, parent.label + " ＋ " + gate.label,
                                   tuple(dict.fromkeys(parent.required + gate.required)),
                                   lambda r, p=parent, g=gate: p.predicate(r) and g.predicate(r), parent.id, parent.gate_ids + (gate.id,)))
    added.extend(core.Rule("gate_" + g.id, g.label, g.required, g.predicate, gate_ids=(g.id,)) for g in gates)
    parents = {r.id: r for r in original}
    assert len(added) == 672 and len({r.id for r in added}) == 672
    return original, added, parents, base_rules


def choose(results):
    # Reuse development-only thresholds; every added rule has the exact old
    # parent as its comparator. All gate_ controls remain ineligible.
    return core.select(results)


def evaluate(rule, rows, parents, bases):
    result, eligible, matched = core.evaluate(rule, rows, parents)
    # Also compare against the original one of 12 bases, with identical inputs.
    base_id = rule.id.split("__")[0]
    if base_id in bases:
        for split in PARTS:
            pool = eligible if split == "all" else [r for r in eligible if r["split"] == split]
            base = core.stats([r for r in pool if bases[base_id].predicate(r)])
            metrics = result["partitions"][split]
            metrics["originalBaseSameInputs"] = base
            metrics["incrementOverOriginalBasePP"] = (metrics["rate"] - base["rate"]) * 100 if metrics["n"] and base["n"] else None
    return result, eligible, matched


def strata(rule, rows, parents, field):
    grouped = defaultdict(list)
    for row in rows:
        goals = row.get("goalsHeat") or {}
        inputs = goals.get("inputs") or {}
        if field == "goalsLine":
            value = goals.get("latestLine")
            key = "缺失" if not core.number(value) else "≤2.5" if value <= 2.5 else ">2.5"
        elif field == "recentTotalGoals":
            value = inputs.get("recentTotalGoals")
            key = "缺失" if not core.number(value) else "≤2.5" if value <= 2.5 else ">2.5"
        elif field == "marketProbability":
            av = row["latest.average"]
            value = (1 / av[1]) / sum(1 / x for x in av) if av else None
            key = "缺失" if value is None else "<20%" if value < .2 else "20%～25%" if value < .25 else "25%～30%" if value < .3 else "≥30%"
        elif field == "goalsSource":
            key = str(goals.get("source") or "缺失")
        else:
            key = str(row.get(field) or "缺失")
        grouped[key].append(row)
    return [{"group": key, "partitions": core.evaluate(rule, values, parents)[0]["partitions"]} for key, values in sorted(grouped.items())]


def write_html(path, report):
    esc = lambda x: html.escape(str(x))
    def table(results):
        body = ''.join('<tr data-search="' + esc(r["label"]) + '"><td>' + esc(r["label"]) + '<br><small>' + esc(r["id"]) + '</small></td>' + ''.join('<td>' + esc(core.metric(r["partitions"][s])) + '</td>' for s in PARTS) + '</tr>' for r in results)
        return '<div class="scroll"><table><tr><th>固定条件</th>' + ''.join('<th>' + LABELS[s] + '</th>' for s in PARTS) + '</tr>' + body + '</table></div>'
    doc = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>大小球热度补充组合验证</title><style>body{font:14px/1.65 system-ui,sans-serif;background:#111820;color:#e1e8ee;padding:24px;max-width:1900px;margin:auto}table{border-collapse:collapse;width:100%}td,th{border:1px solid #3a4958;padding:10px;vertical-align:top;min-width:160px}th{background:#243442;position:sticky;top:0}td:first-child{min-width:260px}.scroll{overflow:auto;max-height:720px}.note{padding:16px;background:#23384a}.warn{padding:16px;background:#493523}small{color:#b6c3d0}input,select{padding:10px;background:#213344;color:white;border:1px solid #68859e}input{min-width:350px}pre{white-space:pre-wrap;word-break:break-word}h2{margin-top:36px}</style><h1>大小球热度：补充组合验证</h1>'''
    doc += '<p>' + esc(f'快照确认完赛 {report["dataset"]["n"]} 场，平局 {report["dataset"]["draws"]} 场；新增672个固定条件，连同旧223个共同作895项训练多重比较。') + '</p>'
    doc += '<div class="note"><p>上一轮没有组合大小球热度，本次分别加大热≥70%、小热≥70%、均衡40%～60%。差值/盈亏/亚盘条件及其阈值均沿用上轮。只有真实即时大小球盘、双方有效近期比赛且无列出时序风险时，热度才进入主分析。</p><p>大热度=clamp[50+18×(双方近期场均总进球−盘口),0,100]；小热度=100−大热度。不是实际投注资金；小热也不必然代表比赛总进球低。它与模拟盈亏共享近期战绩和大小球盘口，不按独立票数叠加。</p></div>'
    doc += '<p class="warn">此前所有研究日期都已看过，分别称内部复核和近期重复复核；只有未见ID且开球晚于上轮最大实际开球才另列新时间样本。主选仅用固定训练/校准，不根据复核结果调整阈值或重新选冠军。捕获时间缺失，不能把快照命中率当成真实赛前预测成绩。</p>'
    doc += '<h2>本轮评估</h2><div class="note">' + ''.join('<p>' + esc(v) + '</p>' for v in report["assessment"]) + '</div>'
    doc += '<h2>大小球热度独立对照</h2>' + table(report["goalsControls"])
    if report["primaryRuleId"]:
        selected = next(r for r in report["newCandidates"] if r["id"] == report["primaryRuleId"])
        doc += '<h2>开发区锁定的一个观察条件</h2>' + table([selected]) + '<p>同输入父条件指加大小球门槛前的完整旧组合。训练BH校正q=' + esc(f'{selected["trainBHq"]:.4f}') + '；不能把有限探索的最高率当成稳定结论。</p>'
    else:
        doc += '<h2>开发区选型</h2><p>没有组合同时满足训练≥60场、校准≥15场、两区优于同输入基准，且两区相对旧父条件均有正增量。不强行选型。</p>'
    doc += '<h2>12个基础分别加大小球热度</h2>' + table(report["directBaseCombinations"])
    doc += '<h2>全部672个补充条件（包括失败和不足样本）</h2><input id="search" placeholder="搜索条件，例如 小球、初盘、模拟">' + table(report["newCandidates"])
    doc += '<h2>主条件逐场明细</h2><input id="matchSearch" placeholder="比赛ID、球队、联赛"><select id="outcome"><option value="all">全部赛果</option><option value="1">平局</option><option value="0">主胜</option><option value="2">客胜</option></select><div class="scroll"><table id="matches"><tr><th>赛程日 / 分区</th><th>比赛</th><th>比分</th><th>大/小热度</th><th>大小球盘口</th><th>近期均球</th></tr>'
    for r in report["primaryMatches"]:
        g = r["goalsHeat"]
        doc += '<tr data-outcome="' + str(r['result']) + '"><td>' + esc(r['date'] + ' / ' + LABELS[r['split']]) + '</td><td>' + esc(r['matchId'] + ' ' + r['league'] + ' ' + r['home'] + '－' + r['guest']) + '</td><td>' + esc(f'{r["homeScore"]}:{r["awayScore"]}') + '</td><td>' + esc(f'{g.get("over")}% / {g.get("under")}%') + '</td><td>' + esc(g.get('latestLine')) + '</td><td>' + esc((g.get('inputs') or {}).get('recentTotalGoals')) + '</td></tr>'
    doc += '</table></div>'
    for key, title in (("coverage", "可用数据及回退值"), ("snapshotComparison", "新旧共有字段变化"), ("partitions", "时间分区"), ("bootstrap", "按日成对重抽"), ("strata", "联赛、盘口和近期均球分层"), ("sensitivity", "完整近5场、完整初即盘与旧快照一致子集"), ("selection", "开发区选型记录")):
        doc += '<h2>' + title + '</h2><details><summary>展开完整结果</summary><pre>' + esc(json.dumps(report[key], ensure_ascii=False, indent=2)) + '</pre></details>'
    doc += '<script>document.querySelector("#search").addEventListener("input",e=>document.querySelectorAll("tr[data-search]").forEach(r=>r.hidden=!r.dataset.search.includes(e.target.value.trim())));const m=document.querySelector("#matchSearch"),o=document.querySelector("#outcome");function filter(){document.querySelectorAll("#matches tr[data-outcome]").forEach(r=>r.hidden=!r.textContent.includes(m.value.trim())||(o.value!=="all"&&o.value!==r.dataset.outcome));}m.addEventListener("input",filter);o.addEventListener("change",filter);</script></html>'
    path.write_text(doc, encoding="utf-8")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", type=Path, default=ROOT / "reports/draw-goals-heat-study")
    parser.add_argument("--prior", type=Path, default=ROOT / "reports/draw-feature-reassembly/dataset.json")
    args = parser.parse_args()
    folder = args.out
    protocol = read(folder / "protocol.json")
    assert hashlib.sha256(args.prior.read_bytes()).hexdigest() == protocol["priorDatasetSHA256"]
    old = read(args.prior)
    prior = {str(r["matchId"]): r for r in old["rows"]}
    raw = read(folder / "dataset.json")
    rows = [enrich(r, protocol, prior) for r in raw["rows"]]
    assert len(rows) == len({r["matchId"] for r in rows})
    assert all(r["result"] == (1 if r["homeScore"] == r["awayScore"] else 0 if r["homeScore"] > r["awayScore"] else 2) for r in rows)
    original, added, parents, bases = rules()
    evaluated = [evaluate(r, rows, parents, bases) for r in added]
    old_results = [core.evaluate(r, rows, bases)[0] for r in original]
    results = [item[0] for item in evaluated]
    core.adjust_bh(old_results + results)
    qualified = choose(results)
    primary_id = qualified[0]["id"] if qualified else None
    primary = next((r for r in added if r.id == primary_id), None)
    primary_matches = next((item[2] for item in evaluated if item[0]["id"] == primary_id), [])
    covered = [r for r in rows if r["goalsHeat.over"] is not None]
    common = [r for r in rows if str(r["matchId"]) in prior]
    report = {
        "version": "draw-goals-heat-v1", "datasetSHA256": hashlib.sha256((folder / "dataset.json").read_bytes()).hexdigest(),
        "scriptSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "coreScriptSHA256": hashlib.sha256((ROOT / "scripts/draw-feature-reassembly.py").read_bytes()).hexdigest(),
        "protocolSHA256": hashlib.sha256((folder / "protocol.json").read_bytes()).hexdigest(), "generatedAt": raw.get("generatedAt"), "dataset": core.stats(rows),
        "candidateCount": len(results), "trainingBHFamilySize": len(results) + len(old_results), "primaryRuleId": primary_id,
        "selection": {"usesNonDevelopmentResults": False, "qualifiedDevelopmentCount": len(qualified), "developmentRanking": [r["id"] for r in qualified]},
        "coverage": {"validGoalsHeat": core.stats(covered), "invalidReasons": dict(Counter((r.get("goalsHeat") or {}).get("reason", "missing") for r in rows if r["goalsHeat.over"] is None)),
                     "lineSourcesAll": dict(Counter((r.get("goalsHeat") or {}).get("lineSource", "missing") for r in rows)), "sourcesValid": dict(Counter((r.get("goalsHeat") or {}).get("source", "missing") for r in covered)), "sourceAudit": raw.get("audit")},
        "snapshotComparison": {"sharedIds": len(common), "unchangedSharedFeatures": sum(r["unchangedSincePrior"] for r in common), "changedSharedIds": [r["matchId"] for r in common if not r["unchangedSincePrior"]], "newIds": len(rows) - len(common)},
        "partitions": {s: {**core.stats([r for r in rows if r["split"] == s]), "dates": sorted({r["date"] for r in rows if r["split"] == s}), "firstKickoff": min((r["matchTime"] for r in rows if r["split"] == s), default=None), "lastKickoff": max((r["matchTime"] for r in rows if r["split"] == s), default=None)} for s in PARTS[:-1]},
        "newCandidates": results, "oldParentCandidates": old_results,
        "goalsControls": [r for r in results if r["id"] in ("gate_goals_over70", "gate_goals_under70", "gate_goals_balanced")],
        "directBaseCombinations": [r for r in results if r["baseId"] in bases], "bootstrap": {}, "strata": {}, "sensitivity": {}, "primaryMatches": []}
    if primary:
        report["bootstrap"] = {s: core.cluster_bootstrap(primary, rows if s == "all" else [r for r in rows if r["split"] == s], parents) for s in PARTS}
        report["strata"] = {f: strata(primary, rows, parents, f) for f in ("goalsLine", "recentTotalGoals", "date", "league", "simulationSource", "marketProbability", "goalsSource")}
        filters = {
            "unchangedSincePrior": lambda r: r["unchangedSincePrior"],
            "bothTeamsFiveRecent": lambda r: all((r.get("goalsHeat", {}).get("inputs") or {}).get(k, 0) >= 5 for k in ("recentHomeMatches", "recentGuestMatches")),
            "realOpeningAndLatest": lambda r: all(core.number(r.get("goalsHeat", {}).get(k)) and r["goalsHeat"][k] > 0 for k in ("openingLine", "latestLine")),
            "historyListedClean": lambda r: r["historyListedClean"],
        }
        report["sensitivity"] = {k: core.evaluate(primary, [r for r in rows if p(r)], parents)[0]["partitions"] for k, p in filters.items()}
        for r in primary_matches:
            report["primaryMatches"].append({k: r.get(k) for k in ("matchId", "date", "matchTime", "split", "league", "home", "guest", "homeScore", "awayScore", "result", "goalsHeat", "unchangedSincePrior")})
    report["assessment"] = []
    for control in report["goalsControls"]:
        report["assessment"].append(control["label"] + '：' + core.metric(control["partitions"]["all"]) + '。')
    if primary:
        selected = next(r for r in results if r["id"] == primary_id)
        report["assessment"].append('开发区锁定：' + selected["label"] + '；全量：' + core.metric(selected["partitions"]["all"]) + '。')
        report["assessment"].append('较早内部复核：' + core.metric(selected["partitions"]["internalTest"]) + '。近期重复复核：' + core.metric(selected["partitions"]["recentInternal"]) + '。')
        report["assessment"].append('主条件训练BH校正q=' + f'{selected["trainBHq"]:.4f}' + '，本轮新时间未见样本' + str(report["partitions"]["unseenTimes"]["n"]) + '场。未因全量表面提升而认定有效。')
    report["assessment"].append('共享字段一致只指上一轮已导出字段，上一轮没有保存大小球热度；不能声称大小球输入当时已独立冻结。当前可用热度来自同一次只读快照。')
    (folder / "analysis.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    write_html(folder / "report.html", report)
    fields = ["id", "label", "trainBHq"] + [s + "." + k for s in PARTS for k in ("n", "draws", "rate", "liftPP", "incrementalLiftPP")]
    with (folder / "candidates.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=fields)
        writer.writeheader()
        for r in results:
            writer.writerow({**{k: r[k] for k in ("id", "label", "trainBHq")}, **{s + "." + k: r["partitions"][s][k] for s in PARTS for k in ("n", "draws", "rate", "liftPP", "incrementalLiftPP")}})
    summary = {"dataset": report["dataset"], "validGoalsHeat": report["coverage"]["validGoalsHeat"], "snapshotComparison": report["snapshotComparison"],
               "primaryRuleId": primary_id, "qualifiedDevelopmentCount": len(qualified), "goalsControls": [{"id": r["id"], "partitions": r["partitions"]} for r in report["goalsControls"]]}
    if primary_id:
        summary["primary"] = next(r for r in results if r["id"] == primary_id)
    (folder / "summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False))


if __name__ == "__main__":
    main()
