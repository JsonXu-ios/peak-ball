#!/usr/bin/env python3
"""Independent, read-only rule/parity review for the fixed reassembly protocol.

Does not select rules using held-out outcomes. Writes only review.json.
"""
import copy
import hashlib
import importlib.util
import json
import math
import random
import sys
from collections import Counter
from datetime import datetime
from pathlib import Path

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-feature-reassembly"
EPS = 1e-9


def numeric(v):
    return type(v) in (int, float) and math.isfinite(v)


def triple(v, price=False):
    return tuple(v) if isinstance(v, list) and len(v) == 3 and all(numeric(x) and (not price or x > 0) for x in v) else None


def kind(v, phase):
    if v is None:
        return "missing"
    absolute = [abs(x) for x in v]
    maximum, second = sorted(absolute, reverse=True)[:2]
    if absolute[1] + EPS >= maximum:
        return "largest"
    if phase == "latest" and absolute[1] + EPS >= second:
        return "negative" if v[1] < -EPS else "positive" if v[1] > EPS else "none"
    return "none"


def amount_roi(market):
    if not isinstance(market, dict) or market.get("oddsAvailable") is not True:
        return None
    result = []
    for key in ("home", "draw", "away"):
        matches = [x for x in market.get("bookmakerByOutcome", []) if x.get("outcome") == key]
        if len(matches) != 1:
            return None
        item = matches[0]
        if item.get("available") is not True or not numeric(item.get("odds")) or item["odds"] <= 0:
            return None
        amount, total = item.get("bookmakerProfit"), item.get("totalStake")
        if not numeric(amount) or not numeric(total) or total <= 0:
            return None
        result.append(amount * 100 / total)
    return tuple(result)


def features(raw):
    f = {}
    for phase in ("opening", "latest"):
        stage = raw.get(phase) or {}
        w = triple(stage.get("william"), True)
        for key, source in (("ds", "sporttery"), ("da", "average")):
            c = triple(stage.get(source), True)
            diff = tuple(w[i] - c[i] for i in range(3)) if w is not None and c is not None else None
            f[f"{phase}.{key}"] = diff
            f[f"{phase}.{key}.class"] = kind(diff, phase)
    heat = raw.get("homeHeat")
    f["heat"] = heat if numeric(heat) and 0 <= heat <= 100 else None
    markets = (raw.get("roiSimulation") or {}).get("markets", [])
    groups = {
        "sporttery": [m for m in markets if m.get("key") == "sporttery" and m.get("simulated") is not True],
        "simulated": [m for m in markets if m.get("key") in ("sporttery", "sportterySim") and m.get("simulated") is True],
    }
    for source, market in groups.items():
        assert len(market) <= 1
        f[f"profit.{source}"] = amount_roi(market[0]) if market else None
    return f


def specs():
    """Build the 12 bases and seven gates without using production Rule objects."""
    bases = {}
    for source in ("ds", "da"):
        key = f"opening.{source}"
        bases[f"opening_{source}_largest"] = ((key,), lambda f, k=key: f[k + ".class"] == "largest")
    for source in ("ds", "da"):
        key = f"latest.{source}"
        for c in ("largest", "negative", "positive"):
            bases[f"latest_{source}_{c}"] = ((key,), lambda f, k=key, c=c: f[k + ".class"] == c)
    matched = lambda f, k: f[k + ".class"] in ("largest", "negative", "positive")
    bases["opening_both"] = (("opening.ds", "opening.da"), lambda f: f["opening.ds.class"] == f["opening.da.class"] == "largest")
    bases["latest_both"] = (("latest.ds", "latest.da"), lambda f: matched(f, "latest.ds") and matched(f, "latest.da"))
    for source in ("ds", "da"):
        keys = (f"opening.{source}", f"latest.{source}")
        bases[f"consistent_{source}"] = (keys, lambda f, keys=keys: f[keys[0] + ".class"] == "largest" and matched(f, keys[1]))
    gates = {}
    for source in ("sporttery", "simulated"):
        key = "profit." + source
        gates[source + "_negative"] = ((key,), lambda f, k=key: f[k][1] < -EPS)
        gates[source + "_minimum"] = ((key,), lambda f, k=key: all(f[k][1] <= v + EPS for v in f[k]))
    gates["heat_balanced"] = (("heat",), lambda f: 40 <= f["heat"] <= 60)
    gates["heat_one_sided"] = (("heat",), lambda f: f["heat"] >= 70 or f["heat"] <= 30)
    gates["both_profit_minimum"] = (("profit.sporttery", "profit.simulated"), lambda f: all(all(f[k][1] <= v + EPS for v in f[k]) for k in ("profit.sporttery", "profit.simulated")))
    profit_ids, heat_ids = list(gates)[:4], list(gates)[4:6]
    variants = [()] + [(g,) for g in gates] + [(p, h) for p in profit_ids for h in heat_ids] + [("both_profit_minimum", h) for h in heat_ids]
    output = {}
    for base_id, (required, predicate) in bases.items():
        for gate_ids in variants:
            needed = tuple(dict.fromkeys(required + tuple(k for g in gate_ids for k in gates[g][0])))
            output[base_id + "".join("__" + g for g in gate_ids)] = (needed, lambda f, p=predicate, gs=gate_ids: p(f) and all(gates[g][1](f) for g in gs), base_id if gate_ids else None)
    for gate_id, (required, predicate) in gates.items():
        output["gate_" + gate_id] = (required, predicate, None)
    return bases, output


def exact_fisher(a, b, c, d):
    total, draw_total, chosen = a + b + c + d, a + c, a + b
    if not total or not chosen or chosen == total:
        return 1.0
    denominator = math.comb(total, chosen)
    return sum(math.comb(draw_total, x) * math.comb(total - draw_total, chosen - x) for x in range(a, min(draw_total, chosen) + 1)) / denominator


def main():
    path = ROOT / "scripts/draw-feature-reassembly.py"
    spec = importlib.util.spec_from_file_location("reassembly_under_review", path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    raw = json.loads((OUT / "dataset.json").read_text(encoding="utf-8-sig"))
    protocol = json.loads((OUT / "protocol.json").read_text(encoding="utf-8-sig"))
    old = json.loads((ROOT / "reports/draw-feature-study/dataset.json").read_text(encoding="utf-8-sig"))
    old_ids = {str(r["matchId"]) for r in old["rows"]}
    old_max_kickoff = max(datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00")) for r in old["rows"])
    new_date_raw = [r for r in raw["rows"] if str(r["date"])[:10] > protocol["oldLastDate"]]
    new_date_actual_time_overlap = [str(r["matchId"]) for r in new_date_raw if datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00")) <= old_max_kickoff]
    assert not new_date_actual_time_overlap
    rows = [module.enrich(r, protocol, old_ids) for r in raw["rows"]]
    independent = [features(r) for r in raw["rows"]]
    bases, reference_specs = specs()
    actual_rules, actual_bases, _ = module.rules()
    assert len(actual_rules) == len(reference_specs) == 223
    assert set(r.id for r in actual_rules) == set(reference_specs)
    checks, errors = Counter(), []
    for row, ref, source in zip(rows, independent, raw["rows"]):
        for key, value in ref.items():
            actual = row.get(key)
            if isinstance(value, tuple):
                same = actual is not None and len(actual) == len(value) and all(abs(a - b) <= 1e-10 for a, b in zip(actual, value))
            else:
                same = actual == value
            assert same, (source["matchId"], key, actual, value)
            checks["featureComparisons"] += 1
        date = str(source["date"])[:10]
        expected_split = "train" if date <= protocol["partitions"]["trainEnd"] else "calibration" if date <= protocol["partitions"]["calibrationEnd"] else "internalTest" if date <= protocol["oldLastDate"] else "newDates"
        assert row["split"] == expected_split
        checks["splitComparisons"] += 1
    results = []
    fingerprint = {}
    for rule in actual_rules:
        required, predicate, parent_id = reference_specs[rule.id]
        assert set(rule.required) == set(required), rule.id
        result, _, _ = module.evaluate(rule, rows, actual_bases)
        results.append(result)
        fingerprints = {}
        for split in module.PARTS:
            pool = [i for i, r in enumerate(rows) if (split == "all" or r["split"] == split) and all(independent[i][k] is not None for k in required)]
            ids = [i for i in pool if predicate(independent[i])]
            actual_ids = [i for i in pool if rule.predicate(rows[i])]
            assert ids == actual_ids, rule.id
            n, d = len(ids), sum(raw["rows"][i]["result"] == 1 for i in ids)
            pn, pd = len(pool), sum(raw["rows"][i]["result"] == 1 for i in pool)
            metric = result["partitions"][split]
            assert (metric["n"], metric["draws"], metric["eligible"]["n"], metric["eligible"]["draws"]) == (n, d, pn, pd), (rule.id, split)
            if parent_id:
                parent_hits = [i for i in pool if bases[parent_id][1](independent[i])]
                bn, bd = len(parent_hits), sum(raw["rows"][i]["result"] == 1 for i in parent_hits)
                assert (metric["parentSameInputs"]["n"], metric["parentSameInputs"]["draws"]) == (bn, bd)
                expected_increment = (d / n - bd / bn) * 100 if n and bn else None
                assert metric["incrementalLiftPP"] == expected_increment
                checks["sameEligibilityParentComparisons"] += 1
            fingerprints[split] = hashlib.sha256("\n".join(sorted(str(raw["rows"][i]["matchId"]) for i in ids)).encode()).hexdigest()
            checks["rulePartitionComparisons"] += 1
            checks["predicateRowComparisons"] += len(pool)
        fingerprint[rule.id] = fingerprints
    module.adjust_bh(results)
    # Independent BH on all 223 training p-values, with no eligibility pruning.
    ps = sorted((r["partitions"]["train"]["fisherEnrichmentP"], r["id"]) for r in results)
    q = 1.0
    expected_q = {}
    for index in reversed(range(len(ps))):
        p, rule_id = ps[index]
        q = min(q, p * len(ps) / (index + 1))
        expected_q[rule_id] = q
    assert all(abs(r["trainBHq"] - expected_q[r["id"]]) < 1e-12 for r in results)
    checks["BHComparisons"] = len(results)
    rng = random.Random(20261024)
    tables = [(0, 1, 0, 1), (1, 0, 0, 1), (0, 0, 4, 7), (9, 5, 3, 13)] + [tuple(rng.randrange(18) for _ in range(4)) for _ in range(100)]
    for a, b, c, d in tables:
        actual = module.fisher_enrichment({"n": a+b, "draws": a}, {"n": c+d, "draws": c})
        assert abs(actual - exact_fisher(a, b, c, d)) < 1e-10
    checks["exactFisherTables"] = len(tables)
    selected_before = [r["id"] for r in module.select(results)]
    expected_selected = []
    for result in results:
        if result["id"].startswith("gate_"):
            continue
        train, calibration = (result["partitions"][s] for s in ("train", "calibration"))
        if train["n"] >= 60 and calibration["n"] >= 15 and train["liftPP"] > 0 and calibration["liftPP"] > 0:
            if result["baseId"] and not (train["incrementalLiftPP"] > 0 and calibration["incrementalLiftPP"] > 0):
                continue
            expected_selected.append(result)
    expected_selected.sort(key=lambda r: (
        -min(r["partitions"]["train"]["liftPP"], r["partitions"]["calibration"]["liftPP"]),
        -(r["partitions"]["calibration"]["wilson95"][0] - r["partitions"]["calibration"]["eligible"]["rate"]),
        -r["partitions"]["calibration"]["n"], r["id"]))
    assert selected_before == [r["id"] for r in expected_selected]
    checks["independentDevelopmentRanking"] = len(expected_selected)
    tampered = copy.deepcopy(results)
    for r in tampered:
        for split in ("internalTest", "newDates", "all"):
            r["partitions"][split] = {"n": -999999, "draws": -999999, "rate": 1e99, "liftPP": 1e99, "incrementalLiftPP": 1e99}
    assert [r["id"] for r in module.select(tampered)] == selected_before
    checks["heldOutMutationSelectionInvariance"] = 1
    # These edge cases use literal expected classes, not the reference helper.
    edges = [
        (None, "latest", "missing"), ((0, 0, 0), "opening", "largest"),
        ((0, 0, 0), "latest", "largest"), ((-1, -3, 2), "latest", "largest"),
        ((5, -2, 1), "opening", "none"), ((5, -2, 1), "latest", "negative"),
        ((5, 2, 1), "latest", "positive"), ((3, 1, 2), "latest", "none"),
        ((1, 0, 0), "latest", "none"), ((1, -1, .2), "latest", "largest"),
        ((2, -1, 1), "latest", "negative"), ((2, 1, -1), "latest", "positive"),
    ]
    for values, phase, expected in edges:
        assert module.category(values, phase) == expected
    checks["literalCategoryEdges"] = len(edges)
    market = {"oddsAvailable": True, "bookmakerByOutcome": [
        {"outcome": key, "available": True, "odds": 3.0, "bookmakerProfit": amount, "totalStake": 1000, "bookmakerRoi": 999, "officialProfitRate": -999}
        for key, amount in zip(("home", "draw", "away"), (0, -250, 800))]}
    assert module.profit_vector(market) == (0.0, -25.0, 80.0)
    for mutate in (
        lambda m: m.update(oddsAvailable=False),
        lambda m: m["bookmakerByOutcome"][1].update(available=False),
        lambda m: m["bookmakerByOutcome"][1].update(totalStake=0),
        lambda m: m["bookmakerByOutcome"][1].update(bookmakerProfit=None),
        lambda m: m["bookmakerByOutcome"][1].update(odds=0),
        lambda m: m["bookmakerByOutcome"].append(m["bookmakerByOutcome"][0]),
    ):
        changed = copy.deepcopy(market)
        mutate(changed)
        assert module.profit_vector(changed) is None
    checks["profitAvailabilityEdges"] = 7
    # Verify completed output artifacts against the independently reconstructed IDs.
    analysis_path = OUT / "analysis.json"
    if analysis_path.exists():
        stored = json.loads(analysis_path.read_text(encoding="utf-8-sig"))
        assert stored["datasetSHA256"] == hashlib.sha256((OUT / "dataset.json").read_bytes()).hexdigest()
        assert stored["candidateCount"] == 223
        stored_candidates = {r["id"]: r for r in stored["candidates"]}
        assert set(stored_candidates) == set(reference_specs)
        for result in results:
            candidate = stored_candidates[result["id"]]
            assert abs(candidate["trainBHq"] - result["trainBHq"]) < 1e-12
            for split in module.PARTS:
                assert candidate["partitions"][split] == result["partitions"][split]
                checks["storedReportPartitionComparisons"] += 1
        primary_id = selected_before[0] if selected_before else None
        assert stored["primaryRuleId"] == primary_id
        assert stored["selection"]["developmentRanking"] == selected_before
        if primary_id:
            ids = [str(r["matchId"]) for r in stored["primaryMatches"]]
            assert len(ids) == len(set(ids))
            assert hashlib.sha256("\n".join(sorted(ids)).encode()).hexdigest() == fingerprint[primary_id]["all"]
            for split in module.PARTS[:-1]:
                chosen = [str(r["matchId"]) for r in stored["primaryMatches"] if r["split"] == split]
                assert hashlib.sha256("\n".join(sorted(chosen)).encode()).hexdigest() == fingerprint[primary_id][split]
        hazards = ("usedRecentAtOrAfterKickoff", "usedRecentTargetId", "usedRecentUnknownTime", "againstListedAtOrAfterKickoff", "againstListedTargetId", "againstListedUnknownTime")
        clean = sum(bool((r.get("history") or {}).get("present")) and all(type((r.get("history") or {}).get(k)) in (int, float) and (r.get("history") or {})[k] == 0 for k in hazards) for r in raw["rows"])
        assert stored["coverage"]["historyListedClean"] == clean
        checks["independentHistoryCleanCount"] = clean
        report_html = (OUT / "report.html").read_text(encoding="utf-8")
        assert all(r["id"] in report_html for r in results)
        assert "主胜" in report_html and "平局" in report_html and "客胜" in report_html
        checks["htmlAllCandidateIdsAndOutcomes"] = 1
    # Exercise previously fragile missing-value sensitivity on a full-input rule.
    sample = next(r for r in actual_rules if r.id == "consistent_ds__both_profit_minimum__heat_balanced")
    try:
        module.sensitivity(sample, rows, actual_bases)
        checks["missingInputSensitivity"] = 1
    except Exception as exc:
        errors.append({"check": "missingInputSensitivity", "error": repr(exc)})
    output = {
        "purpose": "Independent implementation and fixed-protocol review; no held-out-driven rule selection.",
        "datasetSHA256": hashlib.sha256((OUT / "dataset.json").read_bytes()).hexdigest(),
        "reviewedScriptSHA256": hashlib.sha256(path.read_bytes()).hexdigest(),
        "rows": len(rows), "candidateCount": len(actual_rules), "checks": dict(checks), "errors": errors,
        "primaryRuleIdFromDevelopmentOnly": selected_before[0] if selected_before else None,
        "dataAudit": {
            "outcomes": dict(Counter(str(r["result"]) for r in raw["rows"])),
            "oldMaximumActualKickoff": str(old_max_kickoff),
            "newDateRows": len(new_date_raw),
            "newDateActualKickoffOverlapsOldData": new_date_actual_time_overlap,
            "bookmakerStakeBases": dict(Counter(str(x.get("totalStake")) for r in raw["rows"] for m in ((r.get("roiSimulation") or {}).get("markets") or []) for x in m.get("bookmakerByOutcome", []))),
        },
        "findings": [
            "Same-stage raw differences and exclusive priority labels match independent definitions, including missing and tied values.",
            "Profit ratios come only from available outcome amount / positive same-row stake base; official rates are kept separate.",
            "Every combination parent comparison uses the child eligibility pool.",
            "Fisher selected/unselected cells are disjoint; BH includes all 223 training comparisons.",
            "Changing all non-development performance fields leaves the training/calibration ranking unchanged.",
            "The dataset preserves the frontend history calculation, which is not cutoff at kickoff; clean-list sensitivity cannot prove immutable pre-match availability.",
        ],
        "partitionMatchedIdSHA256": fingerprint,
    }
    (OUT / "review.json").write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({k: v for k, v in output.items() if k != "partitionMatchedIdSHA256"}, ensure_ascii=False))


if __name__ == "__main__":
    main()
