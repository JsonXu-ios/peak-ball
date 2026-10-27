#!/usr/bin/env python3
"""Independent review of the locked goals-heat supplement, using local JSON only."""
import copy
import hashlib
import importlib.util
import json
import math
import sys
from collections import Counter
from datetime import datetime
from pathlib import Path

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-goals-heat-study"


def load_module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def stamp(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def identifier_hash(ids):
    return hashlib.sha256("\n".join(sorted(map(str, ids))).encode()).hexdigest()


def main():
    study = load_module("goals_study_under_review", ROOT / "scripts/draw-goals-heat-study.py")
    independent = load_module("independent_prior_rules", ROOT / "scripts/draw-feature-reassembly-review.py")
    protocol = read(OUT / "protocol.json")
    raw = read(OUT / "dataset.json")
    prior_raw = read(ROOT / "reports/draw-feature-reassembly/dataset.json")
    prior = {str(r["matchId"]): r for r in prior_raw["rows"]}
    assert hashlib.sha256((ROOT / "reports/draw-feature-reassembly/dataset.json").read_bytes()).hexdigest() == protocol["priorDatasetSHA256"]
    original, added, parents, bases = study.rules()
    independent_bases, independent_old = independent.specs()
    actual = [study.enrich(r, protocol, prior) for r in raw["rows"]]
    reference = [independent.features(r) for r in raw["rows"]]
    checks = Counter()
    issues = []
    goal_gate_specs = {
        "goals_over70": (("goalsHeat.over",), lambda f: f["goalsHeat.over"] >= 70),
        "goals_under70": (("goalsHeat.under",), lambda f: f["goalsHeat.under"] >= 70),
        "goals_balanced": (("goalsHeat.over",), lambda f: 40 <= f["goalsHeat.over"] <= 60),
    }
    reference_specs = {}
    for old_id, (old_required, old_predicate, _) in independent_old.items():
        for gate_id, (required, predicate) in goal_gate_specs.items():
            reference_specs[old_id + "__" + gate_id] = (
                tuple(dict.fromkeys(old_required + required)),
                lambda f, p=old_predicate, g=predicate: p(f) and g(f), old_id)
    for gate_id, (required, predicate) in goal_gate_specs.items():
        reference_specs["gate_" + gate_id] = (required, predicate, None)
    assert len(added) == len(reference_specs) == 672
    assert {r.id for r in added} == set(reference_specs)
    unchanged_reference = []
    fingerprint_keys = ("date", "matchTime", "result", "homeScore", "awayScore", "opening", "latest", "homeHeat", "asianOpening", "asianLine", "roiSimulation", "profitInputs", "history")
    for source, row, f in zip(raw["rows"], actual, reference):
        g = source.get("goalsHeat") or {}
        inputs = g.get("inputs") or {}
        home, away = inputs.get("recentHomeMatches"), inputs.get("recentGuestMatches")
        hg, ag = inputs.get("recentHomeTotalGoals"), inputs.get("recentGuestTotalGoals")
        assert all(independent.numeric(v) for v in (home, away, hg, ag, inputs.get("calculationLine")))
        recent = (hg + ag) / max(home + away, 1)
        assert abs(recent - inputs["recentTotalGoals"]) < 1e-10
        line = inputs["calculationLine"]
        unclipped = 50 + (recent - line) * 18
        over = min(100, max(0, unclipped))
        round_go = lambda v: math.floor(v * 100 + .5) / 100
        if g.get("over") is not None and g.get("under") is not None:
            assert abs(g["over"] - round_go(over)) <= 1e-9, source["matchId"]
            assert abs(g["under"] - round_go(100 - over)) <= 1e-9, source["matchId"]
            checks["independentFrontendFormulaRows"] += 1
        history = source.get("history") or {}
        hazards = ("usedRecentAtOrAfterKickoff", "usedRecentTargetId", "usedRecentUnknownTime")
        valid = (independent.numeric(g.get("latestLine")) and g["latestLine"] > 0 and home > 0 and away > 0 and
                 inputs.get("recentScoresValid") is True and all(history.get(k) == 0 for k in hazards) and
                 independent.numeric(g.get("over")) and independent.numeric(g.get("under")) and
                 g.get("lineSource") == "latest" and abs(line - g["latestLine"]) <= 1e-9)
        assert g.get("available") is bool(valid), (source["matchId"], g.get("reason"), valid)
        checks["independentAvailabilityRows"] += 1
        valid_pair = valid and 0 <= g["over"] <= 100 and 0 <= g["under"] <= 100 and abs(g["over"] + g["under"] - 100) < .011
        f["goalsHeat.over"] = g["over"] if valid_pair else None
        f["goalsHeat.under"] = g["under"] if valid_pair else None
        for key, value in f.items():
            actual_value = row.get(key)
            assert (actual_value == value if not isinstance(value, tuple) else actual_value is not None and all(abs(a-b) < 1e-10 for a,b in zip(actual_value, value))), (source["matchId"], key)
            checks["derivedFeatureComparisons"] += 1
        date = str(source["date"])[:10]
        unseen = str(source["matchId"]) not in prior and stamp(source["matchTime"]) > stamp(protocol["priorLastActualKickoff"])
        expected = "unseenTimes" if unseen else "train" if date <= protocol["partitions"]["trainEnd"] else "calibration" if date <= protocol["partitions"]["calibrationEnd"] else "internalTest" if date <= "2026-10-22" else "recentInternal"
        assert row["split"] == expected
        checks["timePartitionComparisons"] += 1
        previous = prior.get(str(source["matchId"]))
        unchanged = previous is not None and all(source.get(k) == previous.get(k) for k in fingerprint_keys)
        assert row["unchangedSincePrior"] == unchanged
        if unchanged:
            unchanged_reference.append(str(source["matchId"]))
        checks["sharedFeatureComparisonRows"] += 1
    records = []
    fingerprints = {}
    for rule in added:
        required, predicate, parent_id = reference_specs[rule.id]
        assert set(required) == set(rule.required)
        result, _, _ = study.evaluate(rule, actual, parents, bases)
        records.append(result)
        fingerprints[rule.id] = {}
        for split in study.PARTS:
            pool = [i for i, row in enumerate(actual) if (split == "all" or row["split"] == split) and all(reference[i][k] is not None for k in required)]
            hits = [i for i in pool if predicate(reference[i])]
            assert hits == [i for i in pool if rule.predicate(actual[i])]
            n, d = len(hits), sum(raw["rows"][i]["result"] == 1 for i in hits)
            en, ed = len(pool), sum(raw["rows"][i]["result"] == 1 for i in pool)
            metrics = result["partitions"][split]
            assert (metrics["n"], metrics["draws"], metrics["eligible"]["n"], metrics["eligible"]["draws"]) == (n, d, en, ed)
            if parent_id:
                parent_hits = [i for i in pool if independent_old[parent_id][1](reference[i])]
                pn, pd = len(parent_hits), sum(raw["rows"][i]["result"] == 1 for i in parent_hits)
                assert (metrics["parentSameInputs"]["n"], metrics["parentSameInputs"]["draws"]) == (pn, pd)
                assert metrics["incrementalLiftPP"] == ((d / n - pd / pn) * 100 if n and pn else None)
                checks["exactOldParentSameInputComparisons"] += 1
            base_id = rule.id.split("__")[0]
            if base_id in independent_bases:
                base_hits = [i for i in pool if independent_bases[base_id][1](reference[i])]
                bn, bd = len(base_hits), sum(raw["rows"][i]["result"] == 1 for i in base_hits)
                assert (metrics["originalBaseSameInputs"]["n"], metrics["originalBaseSameInputs"]["draws"]) == (bn, bd)
                assert metrics["incrementOverOriginalBasePP"] == ((d / n - bd / bn) * 100 if n and bn else None)
                checks["originalTwelveBaseSameInputComparisons"] += 1
            fingerprints[rule.id][split] = identifier_hash(raw["rows"][i]["matchId"] for i in hits)
            checks["candidatePartitionComparisons"] += 1
            checks["eligiblePredicateRowComparisons"] += len(pool)
    old_results = [study.core.evaluate(rule, actual, bases)[0] for rule in original]
    study.core.adjust_bh(old_results + records)
    all_p = sorted((r["partitions"]["train"]["fisherEnrichmentP"], r["id"]) for r in old_results + records)
    assert len(all_p) == 895
    q, independent_q = 1, {}
    for index in reversed(range(len(all_p))):
        p, key = all_p[index]
        q = min(q, p * len(all_p) / (index+1))
        independent_q[key] = q
    assert all(abs(r["trainBHq"] - independent_q[r["id"]]) < 1e-12 for r in old_results + records)
    checks["BHTrainingFamilyComparisons"] = len(all_p)
    expected_selection = []
    for result in records:
        if result["id"].startswith("gate_"):
            continue
        a, b = (result["partitions"][split] for split in ("train", "calibration"))
        if a["n"] >= 60 and b["n"] >= 15 and a["liftPP"] > 0 and b["liftPP"] > 0 and a["incrementalLiftPP"] > 0 and b["incrementalLiftPP"] > 0:
            expected_selection.append(result)
    expected_selection.sort(key=lambda r: (-min(r["partitions"][s]["liftPP"] for s in ("train", "calibration")), -(r["partitions"]["calibration"]["wilson95"][0] - r["partitions"]["calibration"]["eligible"]["rate"]), -r["partitions"]["calibration"]["n"], r["id"]))
    actual_ranking = [r["id"] for r in study.choose(records)]
    assert actual_ranking == [r["id"] for r in expected_selection]
    altered = copy.deepcopy(records)
    for result in altered:
        for split in ("internalTest", "recentInternal", "unseenTimes", "all"):
            result["partitions"][split] = {"n": -1, "draws": -1, "rate": 1e100, "liftPP": 1e100, "incrementalLiftPP": 1e100}
    assert [r["id"] for r in study.choose(altered)] == actual_ranking
    checks["nonDevelopmentMutationSelectionInvariance"] = 1
    checks["independentDevelopmentRanking"] = len(actual_ranking)
    controls = {r.id: r for r in added if r.id.startswith("gate_goals_")}
    for over, expected in ((0, (False, True, False)), (30, (False, True, False)), (40, (False, False, True)),
                           (50, (False, False, True)), (60, (False, False, True)), (70, (True, False, False)), (100, (True, False, False))):
        f = {"goalsHeat.over": over, "goalsHeat.under": 100-over}
        flags = tuple(controls[key].predicate(f) for key in ("gate_goals_over70", "gate_goals_under70", "gate_goals_balanced"))
        assert flags == expected
        checks["literalHeatThresholdEdges"] += 1
    template = copy.deepcopy(raw["rows"][0])
    for goals in (None, {"available": False, "over": 70, "under": 30}, {"available": True, "over": None, "under": 30},
                  {"available": True, "over": 70, "under": 31}, {"available": True, "over": 101, "under": -1}):
        sample = {**template, "goalsHeat": goals}
        derived = study.enrich(sample, protocol, prior)
        assert derived["goalsHeat.over"] is None and derived["goalsHeat.under"] is None
        checks["heatAvailabilityEdges"] += 1
    for match_id, match_time, date, expected in (
        ("unseen-review-synthetic", "2026-10-24T15:31:00+08:00", "2026-01-01", "unseenTimes"),
        ("unseen-review-synthetic", protocol["priorLastActualKickoff"], "2026-10-24", "recentInternal"),
        ("unseen-review-synthetic", "2026-10-23T15:31:00+08:00", "2026-10-24", "recentInternal"),
        (template["matchId"], "2026-10-24T15:31:00+08:00", template["date"], "train" if template["date"] <= protocol["partitions"]["trainEnd"] else "calibration" if template["date"] <= protocol["partitions"]["calibrationEnd"] else "internalTest" if template["date"] <= "2026-10-22" else "recentInternal"),
    ):
        sample = {**template, "matchId": match_id, "matchTime": match_time, "date": date}
        assert study.enrich(sample, protocol, prior)["split"] == expected
        checks["literalUnseenTimeEdges"] += 1
    # Verify complete saved report, including primary match IDs and failed rows.
    stored = read(OUT / "analysis.json")
    assert stored["datasetSHA256"] == hashlib.sha256((OUT / "dataset.json").read_bytes()).hexdigest()
    assert stored["candidateCount"] == 672 and stored["trainingBHFamilySize"] == 895
    saved = {r["id"]: r for r in stored["newCandidates"]}
    for result in records:
        assert saved[result["id"]] == result
        checks["savedCandidateComparisons"] += 1
    assert stored["oldParentCandidates"] == old_results
    primary_id = actual_ranking[0] if actual_ranking else None
    assert stored["primaryRuleId"] == primary_id
    assert stored["selection"]["developmentRanking"] == actual_ranking
    assert stored["snapshotComparison"]["unchangedSharedFeatures"] == len(unchanged_reference)
    assert stored["snapshotComparison"]["changedSharedIds"] == [r["matchId"] for r in raw["rows"] if str(r["matchId"]) in prior and str(r["matchId"]) not in set(unchanged_reference)]
    if primary_id:
        for split in study.PARTS:
            ids = [r["matchId"] for r in stored["primaryMatches"] if split == "all" or r["split"] == split]
            assert identifier_hash(ids) == fingerprints[primary_id][split]
        checks["savedPrimaryPartitionIdComparisons"] = len(study.PARTS)
    doc = (OUT / "report.html").read_text(encoding="utf-8")
    assert all(r["id"] in doc for r in records)
    assert all(text in doc for text in ("主胜", "平局", "客胜", "未见", "重复复核"))
    checks["htmlCompleteRulesAndOutcomeFilters"] = 1
    summary = {
        "purpose": "Independent fixed-rule goals-heat supplement review. No rule selected from internal or unseen outcomes.",
        "datasetSHA256": stored["datasetSHA256"], "reviewedScriptSHA256": hashlib.sha256((ROOT / "scripts/draw-goals-heat-study.py").read_bytes()).hexdigest(),
        "rows": len(actual), "newCandidateCount": len(added), "BHTrainingFamilySize": 895,
        "checks": dict(checks), "issues": issues,
        "primaryRuleIdFromDevelopmentOnly": primary_id,
        "dataAudit": {"goalsHeatValid": sum(r["goalsHeat.over"] is not None for r in actual), "unchangedSharedIds": len(unchanged_reference), "unseenTimes": sum(r["split"] == "unseenTimes" for r in actual)},
        "matchedIdsSHA256": fingerprints,
        "limitations": ["All previously viewed dates remain repeated internal checks.", "Listed history timing checks do not establish immutable pre-match capture.", "Goals heat is a calculation from recent goals and a goals line, not observed betting money, and shares inputs with simulated profit."],
    }
    (OUT / "review.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({k: v for k, v in summary.items() if k != "matchedIdsSHA256"}, ensure_ascii=False))


if __name__ == "__main__":
    main()
