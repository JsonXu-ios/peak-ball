#!/usr/bin/env python3
"""Find compact historical draw descriptions, using frozen local data only.

All 4161 labels may influence selection. This is explicitly retrospective
discovery, not a new validation experiment or a claim of a global optimum.
Each rule has at most two principal scalar conditions plus visible history
sample guards. No service, database, network, or cache is accessed.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import html
import json
import math
from collections import Counter, defaultdict
from datetime import datetime, timezone, timedelta
from pathlib import Path

import numpy as np

from draw_cover_candidates import build_candidates

ROOT = Path(__file__).resolve().parents[1]


def read(p):
    return json.loads(p.read_text(encoding="utf-8-sig"))


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def tidy(x):
    if isinstance(x, dict):
        return {str(k): tidy(v) for k, v in x.items()}
    if isinstance(x, (list, tuple)):
        return [tidy(v) for v in x]
    if isinstance(x, np.ndarray):
        return tidy(x.tolist())
    if isinstance(x, np.generic):
        return tidy(x.item())
    if isinstance(x, float) and not math.isfinite(x):
        return None
    return x


def write(p, x):
    p.write_text(json.dumps(tidy(x), ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def ident(x):
    return hashlib.sha256(json.dumps(x, ensure_ascii=False, sort_keys=True).encode()).hexdigest()[:16]


def wilson(k, n):
    if not n:
        return [None, None]
    z = 1.959963984540054
    p, den = k / n, 1 + z * z / n
    c = (p + z * z / (2 * n)) / den
    d = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return [c - d, c + d]


def load_inputs(out):
    base = ROOT / "reports/draw-full-feature-study"
    data = read(base / "dataset.json")
    meta = read(base / "feature-manifest.json")
    with (base / "features.csv").open(encoding="utf-8-sig", newline="") as f:
        reader = csv.reader(f)
        header = next(reader)
        a = list(reader)
    assert header[2:] == [m["name"] for m in meta]
    ids = [r["matchId"] for r in data["rows"]]
    assert [r[0] for r in a] == ids
    y = np.array([int(r[1]) for r in a])
    assert np.array_equal(y, [r["result"] for r in data["rows"]])
    X = np.array([[float(v) if v else np.nan for v in r[2:]] for r in a])
    sources = {str(p.relative_to(ROOT)): sha(p) for p in (base / "dataset.json", base / "features.csv", base / "feature-manifest.json")}
    for p, mp in [(ROOT / "reports/draw-conflict-study/extra-features.npz", ROOT / "reports/draw-conflict-study/extra-feature-manifest.json"),
                  (out / "context-features.npz", out / "context-feature-manifest.json")]:
        if not p.exists():
            continue
        z = np.load(p, allow_pickle=False)
        assert list(z["matchIds"].astype(str)) == ids
        extra = read(mp)
        values = z["values"] if "values" in z else z["X"]
        assert values.shape == (len(ids), len(extra))
        X = np.column_stack([X, values])
        meta += extra
        sources[str(p.relative_to(ROOT))], sources[str(mp.relative_to(ROOT))] = sha(p), sha(mp)
    assert len({m["name"] for m in meta}) == len(meta)
    return data["rows"], X, y, meta, sources


def predicate(p, X, cols):
    v = X[:, cols[p["feature"]]]
    if p.get("transform", "identity") == "abs":
        v = np.abs(v)
    e = np.isfinite(v)
    op, value = p["op"], p["value"]
    if op == "le":
        m = v <= value
    elif op == "ge":
        m = v >= value
    elif op == "eq":
        m = v == value
    elif op == "lt":
        m = v < value
    elif op == "gt":
        m = v > value
    else:
        raise ValueError(op)
    for g in p.get("guards", []):
        gm, ge = predicate(g, X, cols)
        e &= ge & gm
    return m & e, e


def count(mask, y):
    n = int(mask.sum())
    h, d, a = [int(np.sum(mask & (y == k))) for k in (0, 1, 2)]
    return {"selected": n, "home": h, "draw": d, "away": a, "falseSelections": h + a,
            "precision": d / n if n else None, "recall": d / int(np.sum(y == 1)),
            "wilson95Descriptive": wilson(d, n)}


def periods(rows):
    days = [datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00")).astimezone(timezone(timedelta(hours=8))).date().isoformat() for r in rows]
    unique, counts = np.unique(days, return_counts=True)
    cumulative = np.cumsum(counts)
    cut1 = int(np.argmin(abs(cumulative - len(days) / 3)))
    cut2 = max(cut1 + 1, int(np.argmin(abs(cumulative - len(days) * 2 / 3))))
    cuts = [unique[cut1], unique[cut2]]
    arr = np.asarray(days)
    return {"early": arr <= cuts[0], "middle": (arr > cuts[0]) & (arr <= cuts[1]), "late": arr > cuts[1]}, cuts


def market_comparison(mask, eligible, y, p):
    available = eligible & np.isfinite(p)
    selected = mask & available
    actual = count(selected, y)
    ix = np.flatnonzero(available)
    scores = np.asarray(p[ix])
    def at_count(n):
        if not n:
            return np.zeros(len(y), bool)
        threshold = np.sort(scores)[::-1][min(n, len(scores)) - 1]
        return available & (p >= threshold)
    matched = at_count(actual["selected"])
    target = actual["draw"]
    positives = p[available & (y == 1)]
    if target > len(positives):
        raise AssertionError("unreachable common-cohort target")
    same_draw = available & (p >= np.sort(positives)[::-1][target - 1]) if target else np.zeros(len(y), bool)
    return {"available": count(available, y), "ruleInCommonCohort": actual,
            "marketSameSelectedCountWithAllTies": count(matched, y),
            "marketHistoricalSameDrawTargetWithAllTies": count(same_draw, y),
            "sameDrawTarget": target, "excludedRuleRowsMissingMarket": int(np.sum(mask & ~np.isfinite(p))),
            "sameDrawUsesOutcomes": True}


def detailed(mask, eligible, y, p, period_masks):
    result = count(mask, y)
    e = count(eligible, y)
    result["eligible"] = e
    result["eligibleLiftPP"] = 100 * (result["precision"] - e["precision"]) if result["selected"] else None
    rest = count(eligible & ~mask, y)
    result["notMatchedEligible"] = rest
    result["versusNotMatchedPP"] = 100 * (result["precision"] - rest["precision"]) if result["selected"] and rest["selected"] else None
    # Fixed predeclared market strata; missing odds remain their own stratum.
    bounds = [0, .15, .20, .25, .30, .35, 1.0000001]
    strata, expected = [], 0.
    for j in range(len(bounds)):
        s = ~np.isfinite(p) if j == len(bounds) - 1 else np.isfinite(p) & (p >= bounds[j]) & (p < bounds[j + 1])
        population = count(eligible & s, y)
        child = count(mask & s, y)
        ex = child["selected"] * (population["precision"] or 0)
        expected += ex
        strata.append({"range": "missing" if j == len(bounds) - 1 else [bounds[j], bounds[j + 1]], "eligible": population, "selected": child, "expectedDraws": ex})
    result["marketStratified"] = {"expectedDrawsFromSameEligibleStrata": expected,
                                  "excessDraws": result["draw"] - expected,
                                  "excessPP": 100 * (result["draw"] - expected) / result["selected"] if result["selected"] else None,
                                  "strata": strata}
    result["periods"] = {}
    for name, pm in period_masks.items():
        c, b = count(mask & pm, y), count(eligible & pm, y)
        c["eligible"] = b
        c["coverageOfAllPeriodDraws"] = c["draw"] / int(np.sum(pm & (y == 1))) if np.any(pm & (y == 1)) else None
        c["liftPP"] = 100 * (c["precision"] - b["precision"]) if c["selected"] and b["selected"] else None
        result["periods"][name] = c
    result["allThreePositiveWith20Rows"] = all(v["selected"] >= 20 and (v["liftPP"] or -1) > 0 for v in result["periods"].values())
    result["marketComparison"] = market_comparison(mask, eligible, y, p)
    return result


def make_atomic(out, meta):
    cols = {m["name"]: m for m in meta}
    raw = read(out / "univariate-candidates.json")
    result = []
    for a in raw:
        if a["feature"] not in cols:
            continue
        m = cols[a["feature"]]
        item = {k: a[k] for k in ("feature", "op", "value", "transform", "guards", "label", "thresholdOrigin") if k in a} | {"family": m["family"], "view": m["view"], "source": m["source"], "origin": "historicalUnivariate"}
        unit = m.get("unit")
        value = float(a["value"])
        text = f"{100 * value:.6g}%" if unit == "probability" else f"{value:.6g}" + ("%" if unit == "percent" else "")
        item["label"] = a["label"] + ("绝对值" if a.get("transform") == "abs" else "") + {"le": "≤", "ge": "≥", "eq": "＝", "lt": "<", "gt": ">"}[a["op"]] + text
        if "quantile" in str(a.get("thresholdOrigin", "")):
            item["label"] += "（历史分位阈值）"
        result.append(item)
    old = build_candidates(meta)
    for a in old["atoms"]:
        result.append({k: a[k] for k in ("feature", "op", "value", "transform", "guards", "label", "family", "view", "source")} | {"origin": "fixedBusiness", "thresholdOrigin": "preexisting_fixed"})
    atoms = {}
    for a in result:
        a.setdefault("transform", "identity")
        a.setdefault("guards", [])
        core = {k: a[k] for k in ("feature", "op", "value", "transform", "guards")}
        a["id"] = "a_" + ident(core)
        atoms.setdefault(a["id"], a)
        if a["origin"] == "fixedBusiness":
            atoms[a["id"]] = a
    return list(atoms.values())


def discover(atoms, X, y, meta):
    cols = {m["name"]: i for i, m in enumerate(meta)}
    n, draws = len(y), y == 1
    masks, eligible, singles = {}, {}, []
    for a in atoms:
        mask, e = predicate(a, X, cols)
        masks[a["id"]], eligible[a["id"]] = mask, e
        c = count(mask, y)
        if c["selected"] < 80 or int(np.sum(e & ~mask)) < 80 or c["draw"] < 20:
            continue
        q = c["precision"]
        baseline = float(np.mean(draws[e]))
        score = (c["wilson95Descriptive"][0] - baseline, q - baseline, c["draw"])
        singles.append({"allOf": [a["id"]], "score": score, "stats": c, "family": a["family"], "feature": a["feature"]})
    singles.sort(key=lambda r: (r["score"], r["allOf"]), reverse=True)
    # Per-field representatives stop 20 nearby cutoffs of one field occupying
    # the interaction budget; fixed business atoms retain their own admission.
    field_best = {}
    for r in singles:
        field_best.setdefault(r["feature"], r)
    chosen, family_n = {}, Counter()
    for r in field_best.values():
        if family_n[r["family"]] < 12:
            chosen[r["allOf"][0]] = r
            family_n[r["family"]] += 1
    for r in list(field_best.values())[:160]:
        chosen[r["allOf"][0]] = r
    admitted = {r["allOf"][0] for r in singles}
    for a in atoms:
        if a["origin"] == "fixedBusiness" and a["id"] in admitted:
            chosen.setdefault(a["id"], next(r for r in singles if r["allOf"][0] == a["id"]))
    by_id = {a["id"]: a for a in atoms}
    candidates = list(singles)
    seeds = sorted(chosen)
    attempted = 0
    for j, a in enumerate(seeds):
        for b in seeds[j + 1:]:
            if by_id[a]["family"] == by_id[b]["family"] or by_id[a]["feature"] == by_id[b]["feature"]:
                continue
            attempted += 1
            m, e = masks[a] & masks[b], eligible[a] & eligible[b]
            selected, d = int(m.sum()), int(np.sum(m & draws))
            if selected < 80 or int(np.sum(e & ~m)) < 80 or d < 20:
                continue
            candidates.append({"allOf": sorted([a, b]), "stats": count(m, y)})
    # Complete selected-mask deduplication; alias eligibility is not discarded:
    # aliases retain their predicates and are independently evaluated in review.
    ordered = sorted(candidates, key=lambda r: (len(r["allOf"]), sum(by_id[a]["view"] == "snapshotDerived" for a in r["allOf"]), sum(by_id[a]["origin"] != "fixedBusiness" for a in r["allOf"]), r["allOf"]))
    unique = {}
    for r in ordered:
        all_of = r["allOf"]
        m = np.logical_and.reduce([masks[a] for a in all_of])
        key = np.packbits(m).tobytes()
        if key not in unique:
            unique[key] = {"id": "r_" + ident(all_of), "allOf": all_of, "stats": r["stats"], "aliases": []}
        else:
            unique[key]["aliases"].append(all_of)
    rules = list(unique.values())
    rules.sort(key=lambda r: r["id"])
    return rules, by_id, masks, eligible, {"atomicCandidates": len(atoms), "qualifiedSingles": len(singles), "interactionSeeds": len(seeds), "crossFamilyPairsAttempted": attempted, "supportedCandidatesBeforeMaskDedup": len(candidates), "uniqueMasks": len(rules)}


def pareto(rules):
    ordered = sorted(rules, key=lambda r: (r["stats"]["falseSelections"], -r["stats"]["draw"], len(r["allOf"]), r["id"]))
    front, best = [], -1
    for r in ordered:
        if r["stats"]["draw"] > best:
            front.append(r["id"])
            best = r["stats"]["draw"]
    return front


def bitset(mask):
    return int.from_bytes(np.packbits(mask).tobytes(), "little")


def chain(rules, rule_masks, rule_bits, y, mode, max_rules=16):
    # Search heuristic only. Every candidate's overall rate must exceed the
    # full-cohort rate; a new step must add at least five actual historical draws.
    pool = [r for r in rules if r["stats"]["precision"] >= float(np.mean(y == 1)) + .01]
    selected, used, steps = np.zeros(len(y), bool), set(), []
    positive = bitset(y == 1)
    selected_bits = 0
    gamma = {"purity": 1., "moderate": .9, "balanced": .75, "coverage": .5}[mode]
    for _ in range(max_rules):
        choices = []
        for r in pool:
            if r["id"] in used:
                continue
            delta = rule_bits[r["id"]] & ~selected_bits
            d = (delta & positive).bit_count()
            f = delta.bit_count() - d
            if d < 5:
                continue
            # Different costs expose precision/coverage tradeoffs without
            # claiming that any one coefficient is the true business cost.
            value = d / ((f + 1) ** gamma)
            choices.append((value, d, -f, -len(r["allOf"]), r["id"]))
        if not choices:
            break
        best = max(choices, key=lambda t: t[:5])
        _, d, nf, _, rid = best
        selected |= rule_masks[rid]
        selected_bits |= rule_bits[rid]
        used.add(rid)
        steps.append({"ruleId": rid, "addedDraws": d, "addedFalseSelections": -nf, "addedPrecision": d / (d - nf), "cumulative": count(selected, y)})
        if np.sum(selected & (y == 1)) >= 900:
            break
    return steps


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, default=ROOT / "reports/draw-historical-value-study")
    args = parser.parse_args()
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=True)
    rows, X, y, meta, sources = load_inputs(out)
    pm, cuts = periods(rows)
    cols = {m["name"]: i for i, m in enumerate(meta)}
    p = X[:, cols["euro.average.latest.prob.draw"]]
    atoms = make_atomic(out, meta)
    rules, by_atom, masks, eligible, search = discover(atoms, X, y, meta)
    print(json.dumps({"stage": "discovery", **search}), flush=True)
    by_rule = {r["id"]: r for r in rules}
    rule_masks = {r["id"]: np.logical_and.reduce([masks[a] for a in r["allOf"]]) for r in rules}
    rule_bits = {rid: bitset(m) for rid, m in rule_masks.items()}
    front = pareto(rules)
    # A short, non-overlapping-condition list chosen by historical lower-bound
    # ranking. Selection uncertainty is not captured by these descriptive CIs.
    short = sorted(rules, key=lambda r: (r["stats"]["wilson95Descriptive"][0], r["stats"]["draw"], -len(r["allOf"]), r["id"]), reverse=True)
    selected_ids = set(front)
    for minimum in (30, 50, 100, 200):
        selected_ids.update(r["id"] for r in [r for r in short if r["stats"]["draw"] >= minimum][:20])
    # Check every discovered rule, not merely the highest global-rate subset.
    # These are three descriptions of the SAME historical discovery cohort.
    positive_bits = bitset(y == 1)
    e_bits = {aid: bitset(e) for aid, e in eligible.items()}
    p_bits = {name: bitset(m) for name, m in pm.items()}
    stable = []
    for r in short:
        if r["stats"]["draw"] < 30:
            continue
        e = e_bits[r["allOf"][0]]
        for aid in r["allOf"][1:]:
            e &= e_bits[aid]
        supported = True
        for pb in p_bits.values():
            mb, eb = rule_bits[r["id"]] & pb, e & pb
            n, en = mb.bit_count(), eb.bit_count()
            d, ed = (mb & positive_bits).bit_count(), (eb & positive_bits).bit_count()
            if n < 20 or en - n < 20 or d * en <= ed * n or d / n - ed / en < .03:
                supported = False
                break
        if supported:
            stable.append(r["id"])
    stable_show = stable[:40]
    selected_ids.update(stable_show)
    # One representative per source family to retain the history and user-
    # specified signals even where the highest-rate list is market dominated.
    family_best = {}
    for r in short:
        key = "+".join(sorted({by_atom[a]["family"] for a in r["allOf"]}))
        if key not in family_best:
            family_best[key] = r["id"]
            selected_ids.add(r["id"])
    chains = []
    for mode in ("purity", "moderate", "balanced", "coverage"):
        steps = chain(rules, rule_masks, rule_bits, y, mode)
        selected_ids.update(s["ruleId"] for s in steps)
        cumulative = np.zeros(len(y), bool)
        for step in steps:
            cumulative |= rule_masks[step["ruleId"]]
            step["marketComparison"] = market_comparison(cumulative, np.ones(len(y), bool), y, p)
        chains.append({"mode": mode, "steps": steps})
    evidence = []
    for rid in sorted(selected_ids):
        r = by_rule[rid]
        ids = r["allOf"]
        e = np.logical_and.reduce([eligible[a] for a in ids])
        m = rule_masks[rid]
        item = {"id": rid, "allOf": ids, "label": " 且 ".join(by_atom[a]["label"] for a in ids),
                "families": sorted({by_atom[a]["family"] for a in ids}),
                "views": sorted({by_atom[a]["view"] for a in ids}), "aliasCount": len(r["aliases"]),
                "stats": detailed(m, e, y, p, pm), "matchIds": [rows[i]["matchId"] for i in np.flatnonzero(m)], "parents": []}
        item["stats"]["drawScoreCounts"] = dict(sorted(Counter(
            f"{rows[i]['homeScore']}:{rows[i]['awayScore']}" for i in np.flatnonzero(m & (y == 1))).items()))
        comparison = item["stats"]["marketComparison"]
        actual = comparison["ruleInCommonCohort"]
        market = comparison["marketSameSelectedCountWithAllTies"]
        item["stats"]["commonMarketPrecisionDifferencePP"] = 100 * (actual["precision"] - market["precision"]) if actual["selected"] and market["selected"] else None
        if len(ids) > 1:
            for removed in ids:
                parent = e & np.logical_and.reduce([masks[a] for a in ids if a != removed])
                ps, rest = count(parent, y), count(parent & ~m, y)
                item["parents"].append({"removed": removed, "parent": ps, "parentNotChild": rest,
                                        "childVersusDisjointParentPP": 100 * (item["stats"]["precision"] - rest["precision"]) if rest["selected"] else None})
        evidence.append(item)
    protocol = {"purpose": "全历史寻找最有价值且可解释的平局描述", "selectionUsesAllHistoricalOutcomes": True,
                "independentValidation": False, "maximumMainConditions": 2, "minimumSelected": 80, "minimumDraws": 20,
                "minimumEligibleNotSelected": 80, "interactionScreen": "每字段按同可用基率的Wilson下界差选代表；每family最多先取12，加全局前160字段和合格固定业务条件，再跨family两项组合",
                "familyDoesNotMeanIndependentSource": True, "chainMaximumRules": 16,
                "chainCostExponents": {"purity": 1., "moderate": .9, "balanced": .75, "coverage": .5},
                "stableScreen": "对全部唯一规则筛选：全期至少30平，各期子组与同E互补组各至少20场，三期平率均至少高于同E基率3个百分点；仅为历史展示筛选，非留出验证",
                "minimumPeriodLiftPPForStableDisplay": 3,
                "periodBoundariesBJT": cuts, "periodsAreDescriptiveOnly": True,
                "marketStrata": [0, .15, .20, .25, .30, .35, 1], "marketMissingIsSeparate": True,
                "globalOptimumClaimed": False, "wilsonIsUnadjustedDescriptiveAfterSelection": True,
                "marketComparator": "限定规则同输入E且均赔即时可用；同选量全留并列，同平局目标为使用赛果的历史事后对照",
                "marketStrataInterpretation": "固定粗分层的残差仅为描述，层内价格和联赛分布仍可不同，不能单独解释成独立增量或超过市场。",
                "humanThemeSelection": "在全历史搜索后按可解释性、三段方向及同选量共同市场对照选出代表，全部属于历史发现，不是预先设定或独立验证。",
                "timing": "盘口采集时点未全部确认；主客近10场和新增间隔按严格历史取数；缺失不作零或无动机。",
                "noServiceOrDatabaseAccess": True}
    for source in [Path(__file__).resolve(), Path(__file__).with_name("draw_cover_candidates.py"),
                   Path(__file__).with_name("draw_historical_context.py"), Path(__file__).with_name("draw-historical-univariate.py")]:
        sources[str(source.relative_to(ROOT))] = sha(source)
    sources[str((out / "univariate-candidates.json").relative_to(ROOT))] = sha(out / "univariate-candidates.json")
    write(out / "protocol.json", protocol)
    write(out / "catalog.json", {"atoms": list(by_atom.values()), "rules": rules, "search": search})
    result = {"counts": count(np.ones(len(y), bool), y), "features": len(meta), "sourceHashes": sources,
              "search": search, "paretoRuleIds": front, "rankedRuleIds": [r["id"] for r in short[:100]],
              "familyBestRuleIds": family_best, "stableRuleIds": stable_show, "allThreePeriodPositiveRuleCount": len(stable),
              "chains": chains, "evidence": evidence,
              "periods": {name: count(m, y) for name, m in pm.items()}}
    themes = [
        {"id": "limited_away_goals_balanced_sides", "label": "客队进球历史支持偏低，同时市场认为双方接近", "ruleIds": ["r_c62a8a4a5337ee12"],
         "explanation": "（客队近10场均进球＋主队近10场均失球）÷2≤1，双方各至少5场；均赔即时去水主胜与客胜概率差绝对值≤15个百分点。算术值不是xG或进球预测。"},
        {"id": "home_quote_moved_draw_quote_low", "label": "主胜报价调整较大，同时平赔保持较低", "ruleIds": ["r_d0a0ab472c3ce9a0", "r_066fed8a7f18104c"],
         "explanation": "均赔主胜即时减初盘的绝对值≥0.15，涨跌均算。严格档另要求均赔即时平赔≤3；较宽档另要求Bet365即时平去水概率≥27.5%。两档共享输入，不算独立证据。"},
        {"id": "draw_quote_fell_balanced_sides", "label": "平赔下调，同时主客胜概率接近", "ruleIds": ["r_123e1b0baa269f3a"],
         "explanation": "Bet365平赔即时/初盘−1≤−2.5%；均赔即时去水主胜与客胜概率差绝对值≤15个百分点。"},
        {"id": "low_recent_goals_similar_observed_interval", "label": "主队近期小球较多，同时双方已记录比赛间隔相近", "ruleIds": ["r_fd578b1e32c2a36b"],
         "explanation": "主队安全近10场小于2.5球比例≥80%，至少5场；双方距上一场已记录比赛的间隔差≤3天。历史记录不是完整赛程，不能据此推断实际轮换。"},
    ]
    for theme in themes:
        assert all(rid in selected_ids for rid in theme["ruleIds"])
    result["themes"] = themes
    result["themeUnions"] = []
    for name, ids in [("三个主题的严格条件并集", ["r_c62a8a4a5337ee12", "r_d0a0ab472c3ce9a0", "r_123e1b0baa269f3a"]),
                      ("报价调整采用较宽条件的三个主题并集", ["r_c62a8a4a5337ee12", "r_066fed8a7f18104c", "r_123e1b0baa269f3a"])]:
        mask = np.logical_or.reduce([rule_masks[rid] for rid in ids])
        result["themeUnions"].append({"label": name, "ruleIds": ids, "stats": detailed(mask, np.ones(len(y), bool), y, p, pm), "matchIds": [rows[i]["matchId"] for i in np.flatnonzero(mask)]})
    write(out / "analysis.json", result)
    chosen = [r for r in short if r["id"] in selected_ids]
    with (out / "rule-summary.csv").open("w", encoding="utf-8-sig", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=["id", "label", "conditions", "selected", "draw", "falseSelections", "precision", "recall"])
        writer.writeheader()
        for r in chosen:
            writer.writerow({"id": r["id"], "label": " 且 ".join(by_atom[a]["label"] for a in r["allOf"]), "conditions": len(r["allOf"]), **{k: r["stats"][k] for k in ("selected", "draw", "falseSelections", "precision", "recall")}})
    # All 4161 IDs/outcomes remain available for reproducing counterexamples.
    match_table = [{k: row.get(k) for k in ("matchId", "matchTime", "league", "home", "guest", "homeScore", "awayScore", "result")} for row in rows]
    write(out / "matches.json", match_table)
    print(json.dumps({"stage": "complete", "evidenceRules": len(evidence), "paretoRules": len(front), "chains": [{"mode": c["mode"], "steps": len(c["steps"]), "last": c["steps"][-1]["cumulative"] if c["steps"] else None} for c in chains]}), flush=True)


if __name__ == "__main__":
    main()
