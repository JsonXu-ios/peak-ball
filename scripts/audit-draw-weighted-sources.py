#!/usr/bin/env python3
"""Read-only coverage audit for the local weighted draw feature experiment.

This script does not fit models, access services, or modify business data.
Official rate is the explicit source jyykSpf field, never simulated ROI.
"""
from __future__ import annotations

import hashlib
import json
import math
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "reports/draw-full-feature-study/dataset.json"
OUTPUT = ROOT / "reports/draw-weighted-feature-study/source-audit.json"
OUTCOMES = ("home", "draw", "away")
EPS = 1e-9


def obj(value):
    return value if isinstance(value, dict) else {}


def num(value):
    return float(value) if type(value) in (int, float) and math.isfinite(value) else None


def triple(value):
    if not isinstance(value, list) or len(value) != 3:
        return None
    values = [num(v) for v in value]
    return values if all(v is not None for v in values) else None


def stats(rows):
    results = Counter(row["result"] for row in rows)
    n = len(rows)
    return {"n": n, "home": results[0], "draw": results[1], "away": results[2],
            "drawRate": results[1] / n if n else None}


def smallest_two(values, absolute=False):
    if values is None:
        return None
    ordered = [abs(v) if absolute else v for v in values]
    boundary = sorted(ordered)[1]
    return [OUTCOMES[i] for i, value in enumerate(ordered) if value <= boundary + EPS]


def extract(row):
    profit = obj(row.get("profitInputs"))
    flags = profit.get("officialProfitRatePresence")
    flags = flags if isinstance(flags, list) and len(flags) == 3 else [False] * 3
    # The strict export keeps true non-simulated source sporttery and the
    # sportterySim contrast separate. Explicitly ignore all simulated markets.
    simulation = obj(row.get("roiSimulation"))
    markets = simulation.get("markets")
    candidates = [obj(m) for m in markets if obj(m).get("key") == "sporttery" and obj(m).get("simulated") is not True] if isinstance(markets, list) else []
    market = candidates[0] if len(candidates) == 1 else {}
    details = market.get("bookmakerByOutcome")
    details = details if isinstance(details, list) else []
    raw_fields = obj(obj(row.get("analysisInputs")).get("tradeFields"))
    info = {"triples": {}, "rates": [], "presence": flags,
            "ratePointers": [], "rawRates": [], "priced": [], "amountAvailable": [], "fakeZero": [],
            "heat": num(row.get("homeHeat"))}
    for phase in ("opening", "latest"):
        values = obj(row.get(phase))
        kelly = triple(values.get("kelly"))
        info["triples"][phase + ".kelly"] = kelly if kelly is not None and all(v > 0 for v in kelly) else None
        for comparison in ("sporttery", "average"):
            w, c = triple(values.get("william")), triple(values.get(comparison))
            complete = w is not None and c is not None and all(v > 0 for v in w + c)
            diff = [a - b for a, b in zip(w, c)] if complete else None
            info["triples"][phase + "." + comparison] = diff
    for i, outcome in enumerate(OUTCOMES):
        found = [obj(d) for d in details if obj(d).get("outcome") == outcome]
        detail = found[0] if len(found) == 1 else {}
        odd = num(detail.get("odds"))
        priced = market.get("oddsAvailable") is True and odd is not None and odd > 0
        pointer = num(detail.get("officialProfitRate"))
        raw = num(raw_fields.get("jyykSpf." + ("hy", "dy", "ay")[i]))
        rate = pointer if priced and flags[i] is True and pointer is not None else None
        info["priced"].append(priced)
        info["amountAvailable"].append(detail.get("available") is True)
        info["ratePointers"].append(pointer)
        info["rawRates"].append(raw)
        info["rates"].append(rate)
        info["fakeZero"].append(priced and pointer == 0 and flags[i] is not True)
    return info


def audit_scope(rows):
    data = [(r, extract(r)) for r in rows]
    result = {"baseline": stats(rows), "triples": {}, "officialRate": {}, "hotDirection": {}, "joint": {}}
    keys = [phase + "." + source for phase in ("opening", "latest") for source in ("kelly", "sporttery", "average")]
    for key in keys:
        complete = [(r, a) for r, a in data if a["triples"][key] is not None]
        result["triples"][key] = {"complete": stats([r for r, _ in complete]), "smallestTwo": {}}
        for absolute in (False, True):
            mode = "absolute" if absolute else "signed"
            counts = Counter("_".join(smallest_two(a["triples"][key], absolute)) for _, a in complete)
            ambiguous = [r for r, a in complete if len(smallest_two(a["triples"][key], absolute)) > 2]
            result["triples"][key]["smallestTwo"][mode] = {"pairs": dict(counts), "boundaryThreeWayTie": stats(ambiguous)}
    heat_rows = [r for r, a in data if a["heat"] is not None and 0 <= a["heat"] <= 100]
    result["heat"] = stats(heat_rows)
    complete_rates = [r for r, a in data if all(v is not None for v in a["rates"])]
    result["officialRate"]["allThreeComplete"] = stats(complete_rates)
    result["officialRate"]["anyDirection"] = stats([r for r, a in data if any(v is not None for v in a["rates"])])
    result["officialRate"]["perDirection"] = {}
    for i, outcome in enumerate(OUTCOMES):
        valid = [(r, a) for r, a in data if a["rates"][i] is not None]
        values = [a["rates"][i] for _, a in valid]
        result["officialRate"]["perDirection"][outcome] = {
            "valid": stats([r for r, _ in valid]),
            "validOfficialRateDespiteLocalAmountUnavailable": stats([r for r, a in valid if not a["amountAvailable"][i]]),
            "presenceTrue": stats([r for r, a in data if a["presence"][i] is True]),
            "pricedPointer": stats([r for r, a in data if a["priced"][i] and a["ratePointers"][i] is not None]),
            "pointerZeroWithoutPresenceExcluded": stats([r for r, a in data if a["fakeZero"][i]]),
            "realZero": stats([r for r, a in valid if a["rates"][i] == 0]),
            "negative": stats([r for r, a in valid if a["rates"][i] < 0]),
            "strictlyPositive": stats([r for r, a in valid if a["rates"][i] > 0]),
            "rateBelow28": stats([r for r, a in valid if a["rates"][i] < 28]),
            "positiveRateBelow28": stats([r for r, a in valid if 0 < a["rates"][i] < 28]),
            "range": [min(values), max(values)] if values else [None, None],
            "presenceRawMismatchIds": [r["matchId"] for r, a in data if (a["presence"][i] is True) != (a["rawRates"][i] is not None)],
            "validPointerRawMismatchIds": [r["matchId"] for r, a in valid if a["rawRates"][i] is None or abs(a["rates"][i] - a["rawRates"][i]) > .0050000001],
        }
    for phase in ("opening", "latest"):
        phase_keys = [phase + "." + source for source in ("kelly", "sporttery", "average")]
        result["joint"][phase + ".allThreeTriples"] = stats([r for r, a in data if all(a["triples"][key] is not None for key in phase_keys)])
    result["joint"]["allSixTriples"] = stats([r for r, a in data if all(a["triples"][key] is not None for key in keys)])
    result["joint"]["allSixPlusHeat"] = stats([r for r, a in data if all(a["triples"][key] is not None for key in keys) and a["heat"] is not None and 0 <= a["heat"] <= 100])
    result["joint"]["allSixPlusAllOfficialRates"] = stats([r for r, a in data if all(a["triples"][key] is not None for key in keys) and all(v is not None for v in a["rates"])])
    result["joint"]["allSixPlusHeatPlusAllOfficialRates"] = stats([r for r, a in data if all(a["triples"][key] is not None for key in keys) and all(v is not None for v in a["rates"]) and a["heat"] is not None and 0 <= a["heat"] <= 100])
    hot = {"home": [], "away": []}
    for row, a in data:
        heat = a["heat"]
        if heat is None or not 0 <= heat <= 100:
            continue
        if heat > 65:
            hot["home"].append((row, a))
        elif 100 - heat > 65:
            hot["away"].append((row, a))
    for direction, records in hot.items():
        i = OUTCOMES.index(direction)
        result["hotDirection"][direction] = {
            "heatAbove65": stats([r for r, _ in records]),
            "heatAbove65AndOfficialRateKnown": stats([r for r, a in records if a["rates"][i] is not None]),
            "heatAbove65AndOfficialRateBelow28": stats([r for r, a in records if a["rates"][i] is not None and a["rates"][i] < 28]),
            "heatAbove65AndPositiveOfficialRateBelow28": stats([r for r, a in records if a["rates"][i] is not None and 0 < a["rates"][i] < 28]),
            "matchIdsOfficialRateBelow28": [r["matchId"] for r, a in records if a["rates"][i] is not None and a["rates"][i] < 28],
            "matchIdsPositiveOfficialRateBelow28": [r["matchId"] for r, a in records if a["rates"][i] is not None and 0 < a["rates"][i] < 28],
        }
    for positive in (False, True):
        name = "eitherHotSidePositiveRateBelow28" if positive else "eitherHotSideRateBelow28"
        records = []
        for side, group in hot.items():
            i = OUTCOMES.index(side)
            records += [r for r, a in group if a["rates"][i] is not None and a["rates"][i] < 28 and (not positive or a["rates"][i] > 0)]
        result["hotDirection"][name] = stats(records)
        result["joint"][name + ".allSixTriples"] = stats([r for r in records if all(extract(r)["triples"][key] is not None for key in keys)])
    return result


def main():
    dataset = json.loads(SOURCE.read_text(encoding="utf-8-sig"))
    rows = dataset["rows"]
    marked = [r for r in rows if isinstance(r.get("jingcaiId"), str) and r["jingcaiId"].strip()]
    assert len(rows) == 4161 and len(marked) == 511
    assert sum(r["result"] == 1 for r in rows) == 970 and sum(r["result"] == 1 for r in marked) == 124
    paths = [SOURCE, ROOT / "go_server/handlers/draw_feature_study.go", ROOT / "go_server/handlers/analysis_weight.go", ROOT / "go_server/handlers/draw_feature_study_test.go",
             ROOT / "go_server/handlers/draw_observation_display.go", ROOT / "go_server/handlers/draw_weighted_inputs_test.go", Path(__file__)]
    result = {
        "purpose": "read-only local weighted-draw source coverage; no model fitting",
        "sourceHashes": {str(p.relative_to(ROOT)).replace("\\", "/"): hashlib.sha256(p.read_bytes()).hexdigest() for p in paths},
        "definitions": {
            "scope": "Nonblank jingcaiId defines sporttery-marked cohort, not inferred from presence of odds.",
            "officialRate": "Unique key=sporttery, simulated!=true market; oddsAvailable=true; unique matching outcome with positive actual odds; officialProfitRate finite and profitInputs.officialProfitRatePresence[outcome] is true. Outcome available=true is NOT required: it describes local amount/distribution calculation, independent of a recorded official source rate.",
            "officialRawCrossCheck": "analysisInputs.tradeFields[jyykSpf.hy/dy/ay] is independently checked against presence and rounded displayed officialProfitRate. Official rate uses source percentage units: 28 means 28%, not 0.28.",
            "notOfficial": "bookmakerRoi and bookmakerProfit/totalStake*100 are locally calculated amounts and never substitute for officialProfitRate; simulated market excluded.",
            "zero": "Real recorded zero retained only when corresponding source presence=true. A populated zero pointer with source presence=false remains missing.",
            "heat": "homeHeat is computed page heat; awayHeat=100-homeHeat. Strict >65 only. This does not independently prove actual traded capital.",
            "twoSmallest": "Both signed raw-value and absolute-value rankings are audited separately. Include all cutoff ties within 1e-9; a 3-member boundary tie is flagged, never arbitrarily split.",
            "triples": "Original strict exporter phase fields: Kelly triple, William minus sporttery triple, William minus average triple. All three raw numbers required; price triples must be positive.",
            "capture": "Snapshot capture times unconfirmed before kickoff; retrospective association only.",
        },
        "sourceCodeAudit": {
            "drawStudyProfitInputs": "Reads raw jyykSpf hy/dy/ay via json.RawMessage and marks presence only when key exists, non-null, numeric float JSON and finite. A genuine numeric zero is present.",
            "decorateSportterySpfMarket": "Copies legacy struct HY/DY/AY into pointer; absent raw fields can have default numeric zero. This is why the strict presence flag is mandatory.",
            "existingRegression": "TestDrawFeatureStudyProfitPresenceKeepsRealZeroAndDistributionFallbacks checks raw hy=0,dy=-12,ay=null -> true,true,false and rejects absent numeric defaults.",
            "observationInputAdapter": "frontendAnalysis.weightedFeatureInputs preserves strict drawStudyStrictAsianHeat and officialProfitRates source observations. Missing heat/rates are explicit null, real zero retained. Adapter rejects simulated/unpriced outcomes and requires the same raw presence flags; local amount availability is not an additional gate for official source rates. It does not alter shared legacy decorator or frozen HGB.",
        },
        "allFinished": audit_scope(rows), "sportteryMarked": audit_scope(marked),
    }
    issues = []
    for scope in ("allFinished", "sportteryMarked"):
        for side, audit in result[scope]["officialRate"]["perDirection"].items():
            if audit["presenceRawMismatchIds"] or audit["validPointerRawMismatchIds"]:
                issues.append(scope + "." + side + ": source/pointer presence mismatch")
    result["issues"] = issues
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    scope = result["sportteryMarked"]
    print(json.dumps({"issues": issues, "sporttery": scope["baseline"],
                      "tripleCoverage": {key: value["complete"] for key, value in scope["triples"].items()},
                      "officialRates": scope["officialRate"]["allThreeComplete"],
                      "joint": scope["joint"],
                      "hotRateBelow28": scope["hotDirection"]["eitherHotSideRateBelow28"],
                      "hotPositiveRateBelow28": scope["hotDirection"]["eitherHotSidePositiveRateBelow28"]}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
