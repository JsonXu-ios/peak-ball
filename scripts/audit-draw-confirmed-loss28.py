#!/usr/bin/env python3
"""Confirmed loss boundary audit, preserving the existing transition artifacts.

Risk: source/ROI < -28 percent. Normal: source/ROI >= -28 percent, including
zero and every positive profit value. No fitting, rule optimization, or live
service/database/cache action is performed.
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATASET = ROOT / "reports/draw-full-feature-study/dataset.json"
BASE_SCRIPT = ROOT / "scripts/audit-draw-transition-profit.py"
BASE_REPORT = ROOT / "reports/draw-transition-study/profit-audit.json"
OUTPUT = ROOT / "reports/draw-transition-study/confirmed-loss28-audit.json"
OUTCOMES = ("home", "draw", "away")
EPS = 1e-9


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def loss_group(value):
    return "risk" if value < -28 else "normal"


def module():
    specification = importlib.util.spec_from_file_location("confirmed_loss28_source_adapter", BASE_SCRIPT)
    result = importlib.util.module_from_spec(specification)
    specification.loader.exec_module(result)
    return result


def counts(records, active_by_id):
    records = list({record["row"]["matchId"]: record for record in records}.values())
    hda = Counter(record["row"]["result"] for record in records)
    n = len(records)
    chosen_win = sum(record["row"]["result"] != 1 and record["row"]["result"] in active_by_id[record["row"]["matchId"]] for record in records)
    other_win = sum(record["row"]["result"] != 1 and record["row"]["result"] not in active_by_id[record["row"]["matchId"]] for record in records)
    market_win = sum(record["row"]["result"] != 1 and record["row"]["result"] in record["favorite"] for record in records)
    result = {"n": n, "home": hda[0], "draw": hda[1], "away": hda[2],
              "drawRate": hda[1] / n if n else None,
              "selectedSideNonDrawWin": chosen_win, "oppositeSideNonDrawWin": other_win,
              "selectedSideWinRate": chosen_win / n if n else None,
              "selectedDirectionLiteralHit": sum(record["row"]["result"] in active_by_id[record["row"]["matchId"]] for record in records),
              "marketFavoriteNonDrawWin": market_win,
              "marketUnderdogNonDrawWin": n - hda[1] - market_win,
              "marketMinimumOddsTies": sum(len(record["favorite"]) > 1 for record in records),
              "activeDirectionTies": sum(len(active_by_id[record["row"]["matchId"]]) > 1 for record in records)}
    assert chosen_win + hda[1] + other_win == n
    assert result["marketFavoriteNonDrawWin"] + hda[1] + result["marketUnderdogNonDrawWin"] == n
    return result


def heat_side(row):
    value = row.get("homeHeat")
    if type(value) not in (float, int) or not 0 <= value <= 100:
        return []
    return [0] if value > 65 else [2] if 100 - value > 65 else []


def axis(record, name):
    if name == "own_current_minimum_odds":
        return list(record["favorite"])
    if name == "original_official_source_minimum_odds":
        prices = record["officialSourceOdds"]
        if any(value is None or value <= 0 for value in prices):
            return []
        return [i for i, value in enumerate(prices) if value <= min(prices) + EPS]
    if name == "asian_heat_above_65":
        return heat_side(record["row"])
    raise ValueError(name)


def evaluate(records, name):
    axes = {record["row"]["matchId"]: axis(record, name) for record in records}
    observed_axes = {record["row"]["matchId"]: [i for i in axes[record["row"]["matchId"]] if record["rates"][i] is not None] for record in records}
    available = [record for record in records if observed_axes[record["row"]["matchId"]]]
    result = {
        "axisAvailableBeforeRate": sum(bool(axes[record["row"]["matchId"]]) for record in records),
        "eligible": counts(available, observed_axes),
        "unknownRateMatches": sum(bool(axes[record["row"]["matchId"]]) and not observed_axes[record["row"]["matchId"]] for record in records),
        "groups": {}, "crossGroupTieOverlap": [],
    }
    grouped = {}
    for group in ("risk", "normal"):
        active = {record["row"]["matchId"]: [i for i in observed_axes[record["row"]["matchId"]] if loss_group(record["rates"][i]) == group] for record in available}
        chosen = [record for record in available if active[record["row"]["matchId"]]]
        grouped[group] = {record["row"]["matchId"] for record in chosen}
        observations = [(record, i) for record in chosen for i in active[record["row"]["matchId"]]]
        report = counts(chosen, active)
        report.update({
            "directionObservations": len(observations),
            "directions": dict(Counter(OUTCOMES[i] for _, i in observations)),
            "rateRangePercent": [min(record["rates"][i] for record, i in observations), max(record["rates"][i] for record, i in observations)] if observations else [None, None],
            "distributionSources": dict(Counter(record["distribution"] for record in chosen)),
            "matchIds": [record["row"]["matchId"] for record in chosen],
            "uniqueAxisDirectionOnly": counts([record for record in chosen if len(axes[record["row"]["matchId"]]) == 1], active),
        })
        if name == "asian_heat_above_65":
            report["heatSideAgreesWithCurrentMarketFavorite"] = sum(any(i in record["favorite"] for i in active[record["row"]["matchId"]]) for record in chosen)
        result["groups"][group] = report
    for mid in sorted(grouped["risk"] & grouped["normal"]):
        record = next(record for record in available if record["row"]["matchId"] == mid)
        result["crossGroupTieOverlap"].append({"matchId": mid, "directions": [OUTCOMES[i] for i in axes[mid]], "ratesPercent": {OUTCOMES[i]: record["rates"][i] for i in observed_axes[mid]}, "result": OUTCOMES[record["row"]["result"]]})
    assert grouped["risk"] | grouped["normal"] == {record["row"]["matchId"] for record in available}
    assert result["groups"]["risk"]["n"] + result["groups"]["normal"]["n"] - len(result["crossGroupTieOverlap"]) == result["eligible"]["n"]
    # Explicit counterexamples distinguish a risk flag from a deterministic
    # outcome. Examples use the active ROI-bearing direction, not another tied
    # market favorite with an unrelated ROI value.
    risk = result["groups"]["risk"]
    result["riskOutcomeExamples"] = {}
    for label in ("selectedSideWin", "draw", "oppositeSideWin"):
        candidates = []
        for record in available:
            mid, actual = record["row"]["matchId"], record["row"]["result"]
            active = [i for i in observed_axes[mid] if record["rates"][i] < -28]
            if not active:
                continue
            matches = actual == 1 if label == "draw" else actual != 1 and (actual in active) == (label == "selectedSideWin")
            if matches:
                candidates.append({"matchId": mid, "home": record["row"].get("home"), "away": record["row"].get("guest"),
                                   "result": OUTCOMES[actual], "directions": [OUTCOMES[i] for i in active],
                                   "ratesPercent": {OUTCOMES[i]: record["rates"][i] for i in active}})
        result["riskOutcomeExamples"][label] = candidates[:3]
    result["cannotInferDeterministicOutcome"] = {"riskSideStillWonObserved": risk["selectedSideNonDrawWin"] > 0,
                                                 "riskNotAlwaysDrawObserved": risk["n"] > risk["draw"],
                                                 "normalCanAlsoDrawObserved": result["groups"]["normal"]["draw"] > 0}
    return result


def main():
    base = module()
    protected = {str(path.relative_to(ROOT)).replace("\\", "/"): sha(path) for path in (DATASET, BASE_SCRIPT, BASE_REPORT)}
    rows = json.loads(DATASET.read_text(encoding="utf-8-sig"))["rows"]
    scopes = {"allFinished": rows,
              "sportteryMarked": [row for row in rows if str(row.get("jingcaiId") or "").strip()],
              "nonSportteryMarked": [row for row in rows if not str(row.get("jingcaiId") or "").strip()]}
    result = {
        "purpose": "Confirmed loss threshold descriptive audit; no score/model fitting or threshold optimization",
        "definitions": {
            "risk": "rate_or_roi < -28, in percent units",
            "normal": "rate_or_roi >= -28, including -28 itself, zero, and ALL positive profits; not limited to <=28",
            "percentageUnits": "28 means 28 percent, not 0.28; negative values are loss. For local ROI, -28 means loss 28 per 100 assumed total stake, not observed company funds.",
            "sourceSeparation": "Source official jyykSpf rates, local actual-quote ROI, non-handicap synthetic-quote ROI and local average market remain separate; no actual opening/latest profit sequence is available.",
            "currentMinimum": "Complete own-market selected current odds, all minima within absolute EPS=1e-9 retained.",
            "originalSourceMinimum": "For official source rates only, use source jyykSpf h/d/a prices. This is a second snapshot price reference, not a proven opening quote or timestamp-aligned sequence.",
            "asianAxis": "Strict stored homeHeat >65 chooses home; 100-homeHeat >65 chooses away; exactly 65 is excluded. Computed heat is not actual traded capital. It may disagree with lowest odds.",
            "directionResultPartition": "selectedSideNonDrawWin + draw + oppositeSideNonDrawWin = unique matches. Within a risk/normal group, selectedSide refers only to ROI-bearing active directions in that group, not every tied minimum. selectedDirectionLiteralHit separately includes draw when draw is a selected minimum.",
            "marketFavoriteResult": "marketFavoriteNonDrawWin uses the entire current own-market minimum set even if one tied direction is in the other ROI group; this is reported separately from active-direction performance.",
            "ties": "Each group deduplicates matches. A tied minimum with one risk and one normal direction appears in both groups; explicit overlaps and uniqueAxisDirectionOnly are provided. Unknown rates never become normal.",
            "interpretation": "Risk is a source/assumed payout exposure condition, not a statement that the team cannot win or that the match must draw. Normal means the chosen loss threshold was not exceeded, not a guaranteed outcome.",
        },
        "sourceHashes": {**protected, str(Path(__file__).relative_to(ROOT)).replace("\\", "/"): sha(__file__)},
        "cohorts": {},
    }
    for scope, selected in scopes.items():
        result["cohorts"][scope] = {"baseline": base.counts(selected), "sources": {}}
        for source, specification in base.SOURCES.items():
            records = [base.source_row(row, specification) for row in selected]
            axes = ["own_current_minimum_odds", "asian_heat_above_65"]
            if specification["kind"] == "source_rate":
                axes.append("original_official_source_minimum_odds")
            result["cohorts"][scope]["sources"][source] = {"definition": specification, "axes": {name: evaluate(records, name) for name in axes}}
    checks = 0
    for source, source_result in result["cohorts"]["allFinished"]["sources"].items():
        for name in source_result["axes"]:
            for group in ("risk", "normal"):
                for field in ("n", "home", "draw", "away", "selectedSideNonDrawWin", "oppositeSideNonDrawWin", "marketFavoriteNonDrawWin", "marketUnderdogNonDrawWin"):
                    assert source_result["axes"][name]["groups"][group][field] == sum(result["cohorts"][cohort]["sources"][source]["axes"][name]["groups"][group][field] for cohort in ("sportteryMarked", "nonSportteryMarked"))
                    checks += 1
    for relative, digest in protected.items():
        assert sha(ROOT / relative) == digest, "Protected existing artifact changed: " + relative
    expected_boundary = {"-28.01": "risk", "-28": "normal", "-1": "normal", "0": "normal", "28": "normal", "100": "normal"}
    computed_boundary = {text: loss_group(float(text)) for text in expected_boundary}
    assert computed_boundary == expected_boundary
    result["checks"] = {"cohortAdditivity": checks, "riskNormalUnionAndTies": "asserted for every source/axis/cohort", "existingAuditArtifactsUnchanged": True,
                        "boundaryCases": computed_boundary}
    result["issues"] = []
    OUTPUT.write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    compact = {}
    for scope, group in result["cohorts"].items():
        compact[scope] = {}
        for source, source_result in group["sources"].items():
            compact[scope][source] = {name: {group_name: {field: values[field] for field in ("n", "home", "draw", "away", "selectedSideNonDrawWin", "oppositeSideNonDrawWin")} for group_name, values in audit["groups"].items()} for name, audit in source_result["axes"].items()}
    print(json.dumps({"checks": result["checks"], "issues": result["issues"], "cohorts": compact}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
