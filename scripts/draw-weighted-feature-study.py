#!/usr/bin/env python3
"""Fixed, interpretable weighted draw rules. Offline; no training or service IO.

Protocol is written before outcomes are evaluated. Scores use inputs only.
Outcomes are used exclusively by metrics/reporting. This is historical snapshot
association, not an independently validated pre-match win probability.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import html
import itertools
import json
import math
from collections import Counter
from pathlib import Path

EPS = 1e-9
PHASES = ("opening", "latest")
SOURCES = ("kelly", "sporttery", "average")
OUTCOMES = ("home", "draw", "away")
CN = ("胜", "平", "负")
WEIGHTS = {"main_3_2_1": (3., 2., 1.), "mild_2_1_1": (2., 1., 1.), "strong_5_3_1": (5., 3., 1.)}


def number(v):
    return type(v) in (int, float) and math.isfinite(v)


def vector(v, positive=False):
    return tuple(float(x) for x in v) if isinstance(v, (list, tuple)) and len(v) == 3 and all(number(x) and (not positive or x > 0) for x in v) else None


def smallest_pairs(values, absolute=False):
    ranked = tuple(abs(v) for v in values) if absolute else values
    cutoff = sorted(ranked)[1]
    must = {i for i, v in enumerate(ranked) if v < cutoff - EPS}
    allowed = {i for i, v in enumerate(ranked) if v <= cutoff + EPS}
    return [p for p in itertools.combinations(range(3), 2) if must.issubset(p) and set(p).issubset(allowed)]


def source_support(values, source, weights=WEIGHTS["main_3_2_1"], fallback=True, signed=True):
    if values is None:
        return None
    pairs = smallest_pairs(values, source != "kelly")
    out = [0., 0., 0.]
    # Observed but indistinguishable: do not inflate a score threshold.
    informative = max(values) - min(values) > EPS
    if informative:
        negative, positive_pair, positive_mixed = weights
        for pair in pairs:
            has_negative = any(values[i] < -EPS for i in pair)
            for i in pair:
                if source == "kelly" or not signed:
                    value = 1.
                elif values[i] < -EPS:
                    value = 1.
                elif values[i] > EPS:
                    value = (positive_mixed if has_negative else positive_pair) / negative
                else:
                    value = positive_mixed / negative
                out[i] += value / len(pairs)
        if source != "kelly" and fallback and signed and not any(1 in p for p in pairs) and abs(values[1]) > max(abs(values[0]), abs(values[2])) + EPS:
            out[1] = .5 / negative
    return {"values": list(values), "pairs": [list(p) for p in pairs], "support": out, "informative": informative}


def raw_inputs(row):
    result = {}
    stages = {s.get("key"): s for s in (row.get("frontend") or {}).get("comparisonStages", [])}
    for phase in PHASES:
        raw = row.get(phase) or {}
        metric = (stages.get(phase) or {}).get("kelly")
        kelly = vector(metric.get("values"), True) if metric is not None and metric.get("available") is True else None
        if metric is None:
            kelly = vector(raw.get("kelly"), True)
        william = vector(raw.get("william"), True)
        result[phase] = {"kelly": kelly}
        for source in ("sporttery", "average"):
            comparison = vector(raw.get(source), True)
            result[phase][source] = tuple(a-b for a, b in zip(william, comparison)) if william and comparison else None
    return result


def strict_rates(row):
    markets = [m for m in (row.get("roiSimulation") or {}).get("markets", []) if m.get("key") == "sporttery" and m.get("simulated") is not True]
    presence = (row.get("profitInputs") or {}).get("officialProfitRatePresence", [])
    out = [None, None, None]
    if len(markets) == 1 and markets[0].get("oddsAvailable") is True:
        for i, key in enumerate(OUTCOMES):
            entries = [v for v in markets[0].get("bookmakerByOutcome", []) if v.get("outcome") == key]
            if len(entries) != 1 or len(presence) != 3 or presence[i] is not True:
                continue
            item = entries[0]
            if number(item.get("odds")) and item["odds"] > 0 and number(item.get("officialProfitRate")):
                out[i] = float(item["officialProfitRate"])
    return out


def hot_point(row):
    heat = row.get("homeHeat")
    rates = strict_rates(row)
    if not number(heat) or not 0 <= heat <= 100:
        return {"state": "unknown", "direction": None, "heat": None, "rate": None, "rates": rates}
    direction = 0 if heat > 65 else 2 if 100-heat > 65 else None
    if direction is None:
        return {"state": "no_hot", "direction": None, "heat": heat, "rate": None, "rates": rates}
    rate = rates[direction]
    return {"state": "unknown" if rate is None else "supported" if rate < 28 else "not_supported", "direction": direction,
            "heat": heat if direction == 0 else 100-heat, "rate": rate, "rates": rates}


def signal(row, weights=WEIGHTS["main_3_2_1"], phase_weights=(3., 7.), fallback=True, signed=True, sources=SOURCES, bonus=1.):
    raw = raw_inputs(row)
    phase_details, phase_scores = {}, {}
    for phase in PHASES:
        phase_details[phase] = {s: source_support(raw[phase][s], s, weights, fallback, signed) for s in SOURCES}
        used = [phase_details[phase][s] for s in sources]
        phase_scores[phase] = [sum(v["support"][i] for v in used) / len(used) for i in range(3)] if all(v is not None for v in used) else None
    required = [phase for phase, weight in zip(PHASES, phase_weights) if weight > 0]
    base = [sum(phase_weights[PHASES.index(p)] * phase_scores[p][i] for p in required) for i in range(3)] if all(phase_scores[p] is not None for p in required) else None
    point = hot_point(row)
    adjusted = list(base) if base is not None and point["state"] != "unknown" else None
    if adjusted is not None and point["state"] == "supported":
        adjusted[point["direction"]] += bonus
    return {"phases": phase_details, "phaseScores": phase_scores, "base": base, "adjusted": adjusted, "hot": point}


def classification(scores):
    if scores is None:
        return "missing"
    leaders = [i for i, v in enumerate(scores) if v >= max(scores) - EPS]
    return "candidate" if leaders == [1] else "suspected" if 1 in leaders else "not_selected"


def draw_in_top_two(scores):
    return scores is not None and max(scores)-min(scores) > EPS and scores[1] >= sorted(scores, reverse=True)[1]-EPS


def stats(rows):
    counts = Counter(r["result"] for r in rows)
    return {"matches": len(rows), "home": counts[0], "draws": counts[1], "away": counts[2], "drawRate": counts[1] / len(rows) if rows else None}


def hot_diagnostics(rows, signals):
    output = []
    predicates = [("热门且源盈亏率已知", lambda p: True), ("源盈亏率<0", lambda p: p["rate"] < 0),
                  ("源盈亏率=0", lambda p: p["rate"] == 0), ("0<源盈亏率<28", lambda p: 0 < p["rate"] < 28),
                  ("源盈亏率<28（正式点位）", lambda p: p["rate"] < 28), ("源盈亏率>=28", lambda p: p["rate"] >= 28)]
    for label, predicate in predicates:
        chosen = [(r, s["hot"]) for r, s in zip(rows, signals) if s["hot"]["direction"] is not None and s["hot"]["rate"] is not None and predicate(s["hot"])]
        totals = stats([r for r, _ in chosen])
        favorite = sum(r["result"] == p["direction"] for r, p in chosen)
        output.append({"label": label, **totals, "favoriteWins": favorite, "favoriteWinRate": favorite/len(chosen) if chosen else None,
                       "oppositeWins": totals["matches"]-totals["draws"]-favorite})
    return output


def evaluate(rows, signals, score_key, minimum=None, include_tied=False, top_two=False):
    eligible = [r for r, s in zip(rows, signals) if s[score_key] is not None]
    chosen = [r for r, s in zip(rows, signals) if s[score_key] is not None and (draw_in_top_two(s[score_key]) if top_two else classification(s[score_key]) in (("candidate", "suspected") if include_tied else ("candidate",))) and (minimum is None or s[score_key][1] >= minimum-EPS)]
    full, valid, selected = stats(rows), stats(eligible), stats(chosen)
    selected.update({"precision": selected["drawRate"], "drawCoverage": selected["draws"] / full["draws"] if full["draws"] else None,
                     "eligibleDrawCoverage": selected["draws"] / valid["draws"] if valid["draws"] else None,
                     "falseSelections": selected["matches"] - selected["draws"], "missedDraws": full["draws"] - selected["draws"]})
    return {"cohort": full, "eligible": valid, "selected": selected, "missingMatches": full["matches"] - valid["matches"], "missingDraws": full["draws"] - valid["draws"]}


def protocol():
    return {"version": "weighted-draw-v2", "status": "fixed before evaluating labels; no fitting or threshold optimization",
            "scope": "primary nonempty jingcaiId; all confirmed finished matches secondary", "outcomes": OUTCOMES,
            "difference": "William minus sporttery / William minus average, unrounded; negative is user's directional-support hypothesis",
            "pairSelection": "Kelly smallest two; signed differences smallest two absolute values. Boundary ties average all legal two-slot pairs symmetrically.",
            "weights": WEIGHTS, "primary": {"weights": "main_3_2_1", "phase": [3,7], "hotBonus": 1, "fallbackRawPoints": .5,
                                              "decision": "draw strictly highest adjusted score; ties labelled suspected, not counted as strict selections"},
            "signs": "negative=3, positive in nonnegative pair=2, positive paired with negative=1, zero=1; divide by3. Kelly selected=1. Mixed negative takes priority.",
            "fallback": "draw only, uniquely maximum absolute difference and in none of the smallest pairs:0.5/3. Other omitted outcomes0. Zero/all-identical vector contributes0 and remains observed.",
            "score": "phase = mean of 3 source supports; base=3*opening+7*latest (0..10). Hot rule adds1 only to hot home/away (adjusted0..11), not draw.",
            "hotPoint": "strict homeHeat>65 or 100-homeHeat>65 AND that direction's real sporttery source officialProfitRate<28%; includes negative, zero and positive<28. No simulation ROI or amount-derived rate.",
            "missing": "all 6 source triples required. Hot unknown: retain base but adjusted unavailable. No-hot needs heat but not an unused official rate. No filling or phase borrowing.",
            "broadSelection": "Additional literal two-option check: draw is in aggregate top two scores, cutoff ties retained, all-equal scores excluded. Declared after first strict-rank run; descriptive coverage only, not fitted.",
            "sensitivity": {"weights": list(WEIGHTS), "hotBonus": [0,.5,1,2], "thresholds": [None,6,7,8]},
            "limitations": ["Source heat is calculated price/handicap pressure, not verified real wagering volume.", "Price snapshots have no proven pre-kickoff capture; all results repeatedly inspected. Historical association only.", "Scores are rule support points, not percentages/probabilities. No claim about bookmaker intent.", "Frozen HGB model and its selection marks are unchanged."]}


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False), encoding="utf-8")


def report(out, analysis, records):
    esc = html.escape
    pct = lambda x: "—" if x is None else f"{x*100:.2f}%"
    parts = ['<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>平局加权特征复核</title><style>body{font:15px/1.6 system-ui;max-width:1500px;margin:28px auto;padding:0 20px;background:#14191e;color:#e6edf3}table{border-collapse:collapse;width:100%;margin:16px 0}th,td{padding:7px 9px;text-align:left;border:1px solid #394553}th{background:#263341}code{color:#84c9ff}details{margin:16px 0}.warn{background:#423421;padding:12px}h1,h2{line-height:1.4}.good{color:#8ad8b3}</style><h1>平局加权特征 v2：历史快照复核</h1>']
    parts.append('<p>体彩差值＝威廉－竞彩；均赔差值＝威廉－平均欧赔。凯利取数值最小两项，差值取绝对值最小两项。负负各3，正正各2，正负为负3/正1，零1；平唯一绝对值最大且不在两项中，给末位0.5。各组除3（凯利直接1），三源等权；初盘3分、即时7分。三项相同无辨别力给0。</p>')
    parts.append('<p>亚盘一侧热度&gt;65%，且该方向真实竞彩源站盈亏率&lt;28%，给热门方向加1分。正数0～28%同样有效。基础分0～10，修正分0～11；分数不是命中概率。平严格第一为候选，并列第一为疑似。并列取样均分所有合法两项组合。</p>')
    parts.append('<p class="warn">使用已反复研究的历史快照，不是未见比赛验证。赔率采集未证明发生在赛前；热度为现有盘口计算值。缺失不当0，未完整的比赛保留在总平局覆盖率分母。冻结“模型选平”不变。</p>')
    for cohort, title in (("sporttery", "竞彩范围"), ("all", "全部比赛范围")):
        section = analysis[cohort]
        base = section["baseline"]
        parts.append(f'<h2>{title}：{base["matches"]}场，{base["draws"]}平，基准{pct(base["drawRate"])}</h2>')
        parts.append('<table><thead><tr><th>预先固定规则</th><th>可计算场/平</th><th>入选场</th><th>命中平</th><th>误选</th><th>命中率</th><th>总平覆盖率</th><th>有效平覆盖率</th></tr></thead><tbody>')
        for row in section["main"]:
            s, v = row["selected"], row["eligible"]
            parts.append(f'<tr><td>{esc(row["label"])}</td><td>{v["matches"]}/{v["draws"]}</td><td>{s["matches"]}</td><td>{s["draws"]}</td><td>{s["falseSelections"]}</td><td>{pct(s["precision"])}</td><td>{pct(s["drawCoverage"])}</td><td>{pct(s["eligibleDrawCoverage"])}</td></tr>')
        parts.append('</tbody></table><details><summary>固定权重/阈值敏感性（完整保留，不按赛果选最优）</summary><table><tr><th>权重</th><th>热方加分</th><th>平分阈值</th><th>入选/平/误选</th><th>命中率</th><th>总平覆盖率</th></tr>')
        for row in section["sensitivity"]:
            s = row["selected"]
            parts.append(f'<tr><td>{row["weight"]}</td><td>{row["bonus"]}</td><td>{row["minimum"] if row["minimum"] is not None else "严格第一"}</td><td>{s["matches"]}/{s["draws"]}/{s["falseSelections"]}</td><td>{pct(s["precision"])}</td><td>{pct(s["drawCoverage"])}</td></tr>')
        parts.append('</table></details>')
        parts.append('<details><summary>热门方向与真实源站盈亏率点位核对</summary><table><tr><th>条件（热度严格&gt;65%）</th><th>场数</th><th>平局</th><th>平率</th><th>热门方向赢</th><th>热门胜率</th><th>冷门赢</th></tr>')
        for row in section["hotDiagnostics"]:
            parts.append(f'<tr><td>{esc(row["label"])}</td><td>{row["matches"]}</td><td>{row["draws"]}</td><td>{pct(row["drawRate"])}</td><td>{row["favoriteWins"]}</td><td>{pct(row["favoriteWinRate"])}</td><td>{row["oppositeWins"]}</td></tr>')
        parts.append('</table></details>')
    parts.append('<h2>逐场审阅：竞彩全部511场</h2><p>可在CSV/JSON查看原始三项、所有取样对、分值、热度点位、缺失原因和最终结果。</p><table><tr><th>ID / 日期</th><th>比赛</th><th>赛果</th><th>基础 胜/平/负</th><th>修正 胜/平/负</th><th>状态</th><th>热方 / 盈亏率</th></tr>')
    for r in records:
        if not r["jingcaiId"]:
            continue
        fmt = lambda values: "/".join(f"{v:.3f}" for v in values) if values is not None else "暂无"
        p = r["signal"]["hot"]
        parts.append(f'<tr><td>{r["matchId"]}<br>{esc(r["date"])}</td><td>{esc(r["home"])} / {esc(r["guest"])}</td><td>{r["homeScore"]}:{r["awayScore"]}</td><td>{fmt(r["signal"]["base"])}</td><td>{fmt(r["signal"]["adjusted"])}</td><td>{r["baseState"]} → {r["adjustedState"]}</td><td>{p["state"]} {CN[p["direction"]] if p["direction"] is not None else ""} / {p["rate"] if p["rate"] is not None else "暂无"}</td></tr>')
    parts.append('</table></html>')
    (out / "report.html").write_text("".join(parts), encoding="utf-8")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--dataset", type=Path, default=Path("reports/draw-full-feature-study/dataset.json"))
    parser.add_argument("--out", type=Path, default=Path("reports/draw-weighted-feature-study"))
    args = parser.parse_args()
    args.out.mkdir(parents=True, exist_ok=True)
    # Declare immutable primary mapping before reading any target label.
    write_json(args.out / "protocol.json", protocol())
    raw_bytes = args.dataset.read_bytes()
    dataset = json.loads(raw_bytes)
    rows = dataset["rows"]
    signals = [signal(r) for r in rows]
    analysis = {"datasetSha256": hashlib.sha256(raw_bytes).hexdigest(), "version": "weighted-draw-v2"}
    for cohort in ("sporttery", "all"):
        indexes = [i for i, r in enumerate(rows) if cohort == "all" or str(r.get("jingcaiId") or "").strip()]
        pool = [rows[i] for i in indexes]
        ss = [signals[i] for i in indexes]
        main_rows = []
        for key, label in (("base", "基础：平严格第一"), ("adjusted", "基础＋热方1分：平严格第一")):
            for tied in (False, True):
                main_rows.append({"label": label + ("＋并列疑似" if tied else ""), "scoreKey": key, "includeTied": tied, **evaluate(pool, ss, key, include_tied=tied)})
        for key in ("base", "adjusted"):
            main_rows.append({"label": ("基础" if key == "base" else "热方修正") + "宽选：综合前两项包含平", "scoreKey": key, "topTwo": True, **evaluate(pool, ss, key, top_two=True)})
        for threshold in (6, 7, 8):
            main_rows.append({"label": f"热方修正：平严格第一且≥{threshold}分", **evaluate(pool, ss, "adjusted", threshold)})
        # Same eligible rows for paired gate and phase/sign comparisons.
        common_indexes = [i for i, s in zip(indexes, ss) if s["adjusted"] is not None]
        common_pool = [rows[i] for i in common_indexes]
        for key in ("base", "adjusted"):
            ev = evaluate(common_pool, [signals[i] for i in common_indexes], key)
            ev["selected"]["drawCoverage"] = ev["selected"]["draws"] / stats(pool)["draws"] if stats(pool)["draws"] else None
            main_rows.append({"label": "共同有效样本：" + ("不加热方分" if key == "base" else "热方＋1"), **ev})
        ablations = [("仅凯利，两阶段3:7", {"sources": ("kelly",)}), ("仅体彩差，两阶段3:7", {"sources": ("sporttery",)}),
                     ("仅均赔差，两阶段3:7", {"sources": ("average",)}), ("仅初盘，统一10分", {"phase_weights": (10.,0.)}),
                     ("仅即时，统一10分", {"phase_weights": (0.,10.)}), ("初即5:5对照", {"phase_weights": (5.,5.)}),
                     ("不看差值符号对照", {"signed": False}), ("去掉平最大末位分对照", {"fallback": False})]
        for label, options in ablations:
            variant = [signal(r, **options) for r in common_pool]
            ev = evaluate(common_pool, variant, "adjusted")
            ev["selected"]["drawCoverage"] = ev["selected"]["draws"] / stats(pool)["draws"] if stats(pool)["draws"] else None
            main_rows.append({"label": "共同样本消融：" + label, **ev})
        sensitivity = []
        for name, weights in WEIGHTS.items():
            for bonus in (0., .5, 1., 2.):
                variants = [signal(r, weights=weights, bonus=bonus) for r in pool]
                for minimum in (None,6,7,8):
                    sensitivity.append({"weight": name, "bonus": bonus, "minimum": minimum, **evaluate(pool, variants, "adjusted", minimum)})
        analysis[cohort] = {"baseline": stats(pool), "main": main_rows, "sensitivity": sensitivity, "hotDiagnostics": hot_diagnostics(pool, ss)}
    records = []
    for row, s in zip(rows, signals):
        records.append({**{k: row.get(k) for k in ("matchId", "date", "home", "guest", "jingcaiId", "homeScore", "awayScore", "result")},
                        "signal": s, "baseState": classification(s["base"]), "adjustedState": classification(s["adjusted"]),
                        "baseTopTwo": draw_in_top_two(s["base"]), "adjustedTopTwo": draw_in_top_two(s["adjusted"])})
    write_json(args.out / "analysis.json", analysis)
    write_json(args.out / "matches.json", records)
    with (args.out / "matches.csv").open("w", newline="", encoding="utf-8-sig") as f:
        writer = csv.writer(f)
        writer.writerow(["matchId", "date", "home", "guest", "jingcaiId", "result", "baseHome", "baseDraw", "baseAway", "adjustedHome", "adjustedDraw", "adjustedAway", "baseState", "adjustedState", "hotState", "hotDirection", "hotHeat", "officialProfitRate", "missingInputs"])
        for row in records:
            s = row["signal"]
            missing = [f"{phase}.{source}" for phase in PHASES for source in SOURCES if s["phases"][phase][source] is None]
            writer.writerow([row[k] for k in ("matchId", "date", "home", "guest", "jingcaiId", "result")] + (s["base"] or [None]*3) + (s["adjusted"] or [None]*3) + [row["baseState"], row["adjustedState"], s["hot"]["state"], s["hot"]["direction"], s["hot"]["heat"], s["hot"]["rate"], ";".join(missing)])
    # Full parity fixture uses only exported UI inputs; no study labels in inputs.
    fixtures = []
    for row, s in zip(rows, signals):
        raw = raw_inputs(row)
        fixtures.append({"matchId": row["matchId"], "inputs": raw, "homeHeat": row.get("homeHeat"), "officialProfitRates": strict_rates(row),
                         "expected": {"phases": s["phases"], "base": s["base"], "adjusted": s["adjusted"], "baseState": classification(s["base"]), "adjustedState": classification(s["adjusted"]), "hot": s["hot"]}})
    write_json(args.out / "ui-reference.json", fixtures)
    report(args.out, analysis, records)
    print(json.dumps({"status": "completed", "datasetSha256": analysis["datasetSha256"], "sporttery": analysis["sporttery"]["main"][:4], "all": analysis["all"]["main"][:2]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
