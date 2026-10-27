#!/usr/bin/env python3
"""Independent offline reconstruction of the fixed weighted draw protocol.

The production study is not imported to reconstruct any historical result.
It is loaded only for adversarial synthetic contract checks after the entirely
separate reconstruction has compared all rows, summaries and UI references.
"""
from __future__ import annotations

import argparse
import copy
import hashlib
import importlib.util
import itertools
import json
import math
from collections import Counter
from fractions import Fraction
from pathlib import Path

EPS = 1e-9
PHASES = ("opening", "latest")
SOURCES = ("kelly", "sporttery", "average")
OUTCOMES = ("home", "draw", "away")
WEIGHTS = {"main_3_2_1": (3, 2, 1), "mild_2_1_1": (2, 1, 1), "strong_5_3_1": (5, 3, 1)}


def finite_number(value):
    return type(value) in (int, float) and math.isfinite(value)


def three(value, positive=False):
    if not isinstance(value, (list, tuple)) or len(value) != 3:
        return None
    if any(not finite_number(v) or (positive and v <= 0) for v in value):
        return None
    return tuple(float(v) for v in value)


def input_vectors(row):
    stages = (row.get("frontend") or {}).get("comparisonStages", [])
    result = {}
    for phase in PHASES:
        raw = row.get(phase) or {}
        matching = [s for s in stages if s.get("key") == phase]
        metric = matching[-1].get("kelly") if matching else None
        if metric is None:
            kelly = three(raw.get("kelly"), True)
        else:
            kelly = three(metric.get("values"), True) if metric.get("available") is True else None
        result[phase] = {"kelly": kelly}
        william = three(raw.get("william"), True)
        for source in SOURCES[1:]:
            comparison = three(raw.get(source), True)
            result[phase][source] = tuple(william[i] - comparison[i] for i in range(3)) if william is not None and comparison is not None else None
    return result


def source_vector(values, source, weights=(3, 2, 1), fallback=True, signed=True):
    if values is None:
        return None
    order = values if source == "kelly" else tuple(map(abs, values))
    median = sorted(order)[1]
    pairs = []
    for omitted in range(3):
        pair = tuple(i for i in range(3) if i != omitted)
        # The tolerance band is around second place. Every definitely smaller
        # outcome is mandatory; a tie cannot be broken by outcome or result.
        if order[omitted] < median - EPS or any(order[i] > median + EPS for i in pair):
            continue
        pairs.append(pair)
    pairs.sort()
    scores = [Fraction(0) for _ in range(3)]
    informative = max(values) - min(values) > EPS
    high, middle, low = map(Fraction, weights)
    if informative:
        for pair in pairs:
            negative_in_pair = any(values[i] < -EPS for i in pair)
            for i in pair:
                if source == "kelly" or not signed or values[i] < -EPS:
                    credit = Fraction(1)
                elif values[i] > EPS and not negative_in_pair:
                    credit = middle / high
                else:
                    credit = low / high
                scores[i] += credit / len(pairs)
        draw_is_unique_farthest = abs(values[1]) - max(abs(values[0]), abs(values[2])) > EPS
        if source != "kelly" and signed and fallback and all(1 not in pair for pair in pairs) and draw_is_unique_farthest:
            scores[1] = Fraction(1, 2) / high
    return {"values": list(values), "pairs": [list(p) for p in pairs], "support": list(map(float, scores)), "informative": informative}


def official_rates(row):
    markets = [m for m in (row.get("roiSimulation") or {}).get("markets", []) if m.get("key") == "sporttery" and m.get("simulated") is not True]
    result = [None] * 3
    flags = (row.get("profitInputs") or {}).get("officialProfitRatePresence", [])
    if len(markets) != 1 or markets[0].get("oddsAvailable") is not True or len(flags) != 3:
        return result
    for index, outcome in enumerate(OUTCOMES):
        entries = [item for item in markets[0].get("bookmakerByOutcome", []) if item.get("outcome") == outcome]
        if len(entries) != 1 or flags[index] is not True:
            continue
        item = entries[0]
        # available refers to local amount/distribution computation, not the
        # independently recorded official rate. Do not require that flag.
        if finite_number(item.get("odds")) and item["odds"] > 0 and finite_number(item.get("officialProfitRate")):
            result[index] = float(item["officialProfitRate"])
    return result


def hot_state(row):
    rates = official_rates(row)
    heat = row.get("homeHeat")
    if not finite_number(heat) or heat < 0 or heat > 100:
        return {"state": "unknown", "direction": None, "heat": None, "rate": None, "rates": rates}
    side = 0 if heat > 65 else 2 if heat < 35 else None
    if side is None:
        return {"state": "no_hot", "direction": None, "heat": heat, "rate": None, "rates": rates}
    rate = rates[side]
    state = "unknown" if rate is None else "supported" if rate < 28 else "not_supported"
    return {"state": state, "direction": side, "heat": heat if side == 0 else 100 - heat, "rate": rate, "rates": rates}


def reconstruct(row, weights=(3, 2, 1), phase_weights=(3, 7), sources=SOURCES, fallback=True, signed=True, bonus=1):
    inputs = input_vectors(row)
    phases = {p: {s: source_vector(inputs[p][s], s, weights, fallback, signed) for s in SOURCES} for p in PHASES}
    phase_scores = {}
    for phase in PHASES:
        components = [phases[phase][source] for source in sources]
        phase_scores[phase] = None if any(c is None for c in components) else [sum(c["support"][i] for c in components) / len(sources) for i in range(3)]
    active = [(p, phase_weights[i]) for i, p in enumerate(PHASES) if phase_weights[i] > 0]
    base = None if any(phase_scores[p] is None for p, _ in active) else [sum(phase_scores[p][i] * weight for p, weight in active) for i in range(3)]
    hot = hot_state(row)
    adjusted = list(base) if base is not None and hot["state"] != "unknown" else None
    if adjusted is not None and hot["state"] == "supported":
        adjusted[hot["direction"]] += bonus
    return {"phases": phases, "phaseScores": phase_scores, "base": base, "adjusted": adjusted, "hot": hot}


def decision(score):
    if score is None:
        return "missing"
    top = max(score)
    leaders = {i for i, value in enumerate(score) if top - value <= EPS}
    return "candidate" if leaders == {1} else "suspected" if 1 in leaders else "not_selected"


def top_two(score):
    return score is not None and max(score) - min(score) > EPS and sum(value > score[1] + EPS for value in score) <= 1


def counts(rows):
    outcomes = Counter(r["result"] for r in rows)
    return {"matches": len(rows), "home": outcomes[0], "draws": outcomes[1], "away": outcomes[2], "drawRate": outcomes[1] / len(rows) if rows else None}


def measure(rows, signals, key, minimum=None, tied=False, wide=False):
    eligible = [r for r, s in zip(rows, signals) if s[key] is not None]
    selected = []
    for row, signal in zip(rows, signals):
        state = decision(signal[key])
        if (top_two(signal[key]) if wide else state == "candidate" or (tied and state == "suspected")):
            if minimum is None or signal[key][1] >= minimum - EPS:
                selected.append(row)
    cohort, valid, chosen = counts(rows), counts(eligible), counts(selected)
    chosen.update(precision=chosen["drawRate"], drawCoverage=chosen["draws"] / cohort["draws"] if cohort["draws"] else None,
                  eligibleDrawCoverage=chosen["draws"] / valid["draws"] if valid["draws"] else None,
                  falseSelections=chosen["matches"] - chosen["draws"], missedDraws=cohort["draws"] - chosen["draws"])
    return {"cohort": cohort, "eligible": valid, "selected": chosen,
            "missingMatches": cohort["matches"] - valid["matches"], "missingDraws": cohort["draws"] - valid["draws"]}


def audit_summary(rows):
    inputs = [input_vectors(row) for row in rows]
    rates = [official_rates(row) for row in rows]
    data = {"n": len(rows), **{k: v for k, v in counts(rows).items() if k != "matches"}}
    data["draw"] = data.pop("draws")
    result = {"baseline": data, "triples": {}, "joint": {}}
    as_audit = lambda selected: {"n": len(selected), "home": counts(selected)["home"], "draw": counts(selected)["draws"], "away": counts(selected)["away"], "drawRate": counts(selected)["drawRate"]}
    for phase in PHASES:
        for source in SOURCES:
            selected = [row for row, inp in zip(rows, inputs) if inp[phase][source] is not None]
            result["triples"][phase + "." + source] = {"complete": as_audit(selected), "smallestTwo": {}}
            for absolute in (False, True):
                pairs, tied = Counter(), []
                for row, inp in zip(rows, inputs):
                    values = inp[phase][source]
                    if values is None:
                        continue
                    values = list(map(abs, values)) if absolute else values
                    boundary = sorted(values)[1]
                    members = [OUTCOMES[i] for i in range(3) if values[i] <= boundary + EPS]
                    pairs["_".join(members)] += 1
                    if len(members) == 3:
                        tied.append(row)
                result["triples"][phase + "." + source]["smallestTwo"]["absolute" if absolute else "signed"] = {"pairs": dict(pairs), "boundaryThreeWayTie": as_audit(tied)}
        result["joint"][phase + ".allThreeTriples"] = as_audit([r for r, inp in zip(rows, inputs) if all(inp[phase][s] is not None for s in SOURCES)])
    result["joint"]["allSixTriples"] = as_audit([r for r, inp in zip(rows, inputs) if all(inp[p][s] is not None for p in PHASES for s in SOURCES)])
    result["officialRate"] = {"allThreeComplete": as_audit([r for r, rate in zip(rows, rates) if all(v is not None for v in rate)]),
                              "anyDirection": as_audit([r for r, rate in zip(rows, rates) if any(v is not None for v in rate)])}
    result["hotDirection"] = {}
    for side, index in (("home", 0), ("away", 2)):
        hot = [(row, point) for row in rows if (point := hot_state(row))["direction"] == index]
        result["hotDirection"][side] = {
            "heatAbove65": as_audit([r for r, p in hot]),
            "heatAbove65AndOfficialRateKnown": as_audit([r for r, p in hot if p["rate"] is not None]),
            "heatAbove65AndOfficialRateBelow28": as_audit([r for r, p in hot if p["state"] == "supported"]),
            "heatAbove65AndPositiveOfficialRateBelow28": as_audit([r for r, p in hot if p["state"] == "supported" and p["rate"] > 0]),
        }
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", type=Path, default=Path("reports/draw-weighted-feature-study"))
    parser.add_argument("--dataset", type=Path, default=Path("reports/draw-full-feature-study/dataset.json"))
    args = parser.parse_args()
    dataset_bytes = args.dataset.read_bytes()
    rows = json.loads(dataset_bytes)["rows"]
    load = lambda name: json.loads((args.out / name).read_text(encoding="utf-8-sig"))
    protocol, analysis, records, ui = (load(n) for n in ("protocol.json", "analysis.json", "matches.json", "ui-reference.json"))
    issues, warnings, comparisons = [], [], Counter()

    def equal(actual, expected, path):
        comparisons["fields"] += 1
        if isinstance(expected, dict):
            if not isinstance(actual, dict):
                issues.append(path + ": expected mapping")
                return
            for key in expected:
                if key not in actual:
                    issues.append(path + ": missing " + key)
                else:
                    equal(actual[key], expected[key], path + "." + key)
        elif isinstance(expected, (list, tuple)):
            if not isinstance(actual, (list, tuple)) or len(actual) != len(expected):
                issues.append(path + ": array shape mismatch")
                return
            for i, value in enumerate(expected):
                equal(actual[i], value, path + f"[{i}]")
        elif finite_number(expected):
            if not finite_number(actual) or not math.isclose(actual, expected, rel_tol=1e-11, abs_tol=1e-11):
                issues.append(f"{path}: {actual!r} != {expected!r}")
        elif actual != expected:
            issues.append(f"{path}: {actual!r} != {expected!r}")

    equal(analysis["datasetSha256"], hashlib.sha256(dataset_bytes).hexdigest(), "datasetSHA")
    equal(protocol["primary"], {"weights": "main_3_2_1", "phase": [3, 7], "hotBonus": 1, "fallbackRawPoints": .5}, "protocol.primary")
    equal(len(rows), 4161, "source.rows")
    equal(len(records), len(rows), "matches.rows")
    equal(len(ui), len(rows), "ui.rows")
    signals = []
    for i, row in enumerate(rows):
        s = reconstruct(row)
        signals.append(s)
        equal(records[i]["matchId"], row["matchId"], f"match[{i}].ID")
        equal(records[i]["signal"], s, f"match[{i}].signal")
        equal(records[i]["baseState"], decision(s["base"]), f"match[{i}].baseState")
        equal(records[i]["adjustedState"], decision(s["adjusted"]), f"match[{i}].adjustedState")
        equal(records[i]["baseTopTwo"], top_two(s["base"]), f"match[{i}].baseTopTwo")
        equal(records[i]["adjustedTopTwo"], top_two(s["adjusted"]), f"match[{i}].adjustedTopTwo")
        expected = {"phases": s["phases"], "base": s["base"], "adjusted": s["adjusted"], "baseState": decision(s["base"]), "adjustedState": decision(s["adjusted"]), "hot": s["hot"]}
        equal(ui[i]["inputs"], input_vectors(row), f"ui[{i}].inputs")
        equal(ui[i]["officialProfitRates"], official_rates(row), f"ui[{i}].officialRate")
        equal(ui[i]["expected"], expected, f"ui[{i}].expected")
        for key, high in (("base", 10), ("adjusted", 11)):
            if s[key] is not None and any(v < -EPS or v > high + EPS for v in s[key]):
                issues.append(f"range violation {row['matchId']} {key}")
    comparisons["reconstructedRows"] = len(rows)
    summaries = {}
    for cohort in ("sporttery", "all"):
        positions = [i for i, row in enumerate(rows) if cohort == "all" or str(row.get("jingcaiId") or "").strip()]
        pool, ss = [rows[i] for i in positions], [signals[i] for i in positions]
        equal(analysis[cohort]["baseline"], counts(pool), cohort + ".baseline")
        expected_main = []
        for key in ("base", "adjusted"):
            expected_main.extend(measure(pool, ss, key, tied=tied) for tied in (False, True))
        expected_main.extend(measure(pool, ss, key, wide=True) for key in ("base", "adjusted"))
        expected_main.extend(measure(pool, ss, "adjusted", threshold) for threshold in (6, 7, 8))
        common = [i for i in positions if signals[i]["adjusted"] is not None]
        common_rows, common_signals = [rows[i] for i in common], [signals[i] for i in common]
        for key in ("base", "adjusted"):
            m = measure(common_rows, common_signals, key)
            m["selected"]["drawCoverage"] = m["selected"]["draws"] / counts(pool)["draws"]
            expected_main.append(m)
        variants = ({"sources": ("kelly",)}, {"sources": ("sporttery",)}, {"sources": ("average",)}, {"phase_weights": (10, 0)}, {"phase_weights": (0, 10)}, {"phase_weights": (5, 5)}, {"signed": False}, {"fallback": False})
        for options in variants:
            m = measure(common_rows, [reconstruct(r, **options) for r in common_rows], "adjusted")
            m["selected"]["drawCoverage"] = m["selected"]["draws"] / counts(pool)["draws"]
            expected_main.append(m)
        equal(len(analysis[cohort]["main"]), len(expected_main), cohort + ".mainLength")
        for i, value in enumerate(expected_main):
            equal(analysis[cohort]["main"][i], value, cohort + f".main[{i}]")
        sensitivity = []
        for name, weights in WEIGHTS.items():
            for bonus in (0, .5, 1, 2):
                varying = [reconstruct(r, weights=weights, bonus=bonus) for r in pool]
                for threshold in (None, 6, 7, 8):
                    sensitivity.append({"weight": name, "bonus": bonus, "minimum": threshold, **measure(pool, varying, "adjusted", threshold)})
        equal(analysis[cohort]["sensitivity"], sensitivity, cohort + ".sensitivity")
        summaries[cohort] = {"base": expected_main[0], "adjusted": expected_main[2], "baseTopTwo": expected_main[4], "adjustedTopTwo": expected_main[5], "commonBase": expected_main[9], "commonAdjusted": expected_main[10],
                             "sensitivityCount": len(sensitivity), "sensitivityMaxPrecision": max(s["selected"]["precision"] for s in sensitivity if s["selected"]["precision"] is not None)}
        hot_diagnostics = []
        for label, predicate in (("热门且源盈亏率已知", lambda rate: True), ("源盈亏率<0", lambda rate: rate < 0), ("源盈亏率=0", lambda rate: rate == 0), ("0<源盈亏率<28", lambda rate: 0 < rate < 28), ("源盈亏率<28（正式点位）", lambda rate: rate < 28), ("源盈亏率>=28", lambda rate: rate >= 28)):
            chosen = [(r, s["hot"]) for r, s in zip(pool, ss) if s["hot"]["direction"] is not None and s["hot"]["rate"] is not None and predicate(s["hot"]["rate"])]
            n = len(chosen)
            wins = sum(r["result"] == p["direction"] for r, p in chosen)
            numbers = counts([r for r, _ in chosen])
            hot_diagnostics.append({"label": label, **numbers, "favoriteWins": wins, "favoriteWinRate": wins / n if n else None, "oppositeWins": n - wins - numbers["draws"]})
        equal(analysis[cohort]["hotDiagnostics"], hot_diagnostics, cohort + ".hotDiagnostics")
        comparisons["mainSummaries"] += len(expected_main)
        comparisons["sensitivitySummaries"] += len(sensitivity)
    sport = summaries["sporttery"]
    equal([sport["base"]["selected"][k] for k in ("matches", "draws", "falseSelections")], [194, 47, 147], "primary.baseCounts")
    equal([sport["adjusted"]["selected"][k] for k in ("matches", "draws", "falseSelections")], [159, 42, 117], "primary.adjustedCounts")
    before, after = sport["commonBase"]["selected"], sport["commonAdjusted"]["selected"]
    paired = {"commonEligible": sport["commonBase"]["eligible"]["matches"], "before": before, "after": after,
              "falseSelectionsRemoved": before["falseSelections"] - after["falseSelections"], "drawsLost": before["draws"] - after["draws"]}
    equal([paired["commonEligible"], paired["falseSelectionsRemoved"], paired["drawsLost"]], [461, 28, 5], "pairedComparison")
    source_audit = load("source-audit.json")
    for name, selected in (("allFinished", rows), ("sportteryMarked", [r for r in rows if str(r.get("jingcaiId") or "").strip()])):
        equal(source_audit[name], audit_summary(selected), "sourceAudit." + name)
    source_hashes = {str(args.dataset).replace("\\", "/"): hashlib.sha256(dataset_bytes).hexdigest()}
    for path, expected_hash in source_audit["sourceHashes"].items():
        actual = hashlib.sha256(Path(path).read_bytes()).hexdigest()
        source_hashes[path] = actual
        if actual != expected_hash:
            warnings.append("source-audit hash changed during concurrent work: " + path)
    # The historical reconstruction above never imports the study code.
    spec = importlib.util.spec_from_file_location("weighted_study_subject", "scripts/draw-weighted-feature-study.py")
    subject = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(subject)
    synthetic_cases = 0
    edge_vectors = [None, (0, 0, 0), (.1, .1, .1), (-.1, -.1, -.1), (-.1, .1, .1), (.1, -.2, .1), (.1, -.1, .1), (0, -.1, .2), (0, .1, .2), (.1, .2, .2 + EPS / 2), (.2 - EPS * .8, .2, .2 + EPS * .8), (.9, 1, 1)]
    for values in edge_vectors:
        for source in SOURCES:
            for weights in WEIGHTS.values():
                for fallback in (False, True):
                    for signed in (False, True):
                        equal(subject.source_support(values, source, weights, fallback, signed), source_vector(values, source, weights, fallback, signed), f"synthetic.source.{synthetic_cases}")
                        synthetic_cases += 1
    sample = {"opening": {"kelly": [.9, 1.0, 1.1], "william": [2, 3, 4], "sporttery": [2.1, 3.2, 4.3], "average": [2.2, 3.1, 4.4]}}
    sample["latest"] = copy.deepcopy(sample["opening"])
    sample["profitInputs"] = {"officialProfitRatePresence": [True] * 3}
    sample["roiSimulation"] = {"markets": [{"key": "sporttery", "oddsAvailable": True, "simulated": False, "bookmakerByOutcome": [{"outcome": o, "available": False, "odds": 2 + i, "officialProfitRate": 1} for i, o in enumerate(OUTCOMES)]}]}
    for heat in (None, -1, 0, 34.999999, 35, 50, 65, 65.000001, 100, 101):
        for rate in (None, -100, 0, 27.999999, 28, 100, 101):
            for present in (False, True):
                row = copy.deepcopy(sample)
                row["homeHeat"] = heat
                row["profitInputs"]["officialProfitRatePresence"] = [present] * 3
                for item in row["roiSimulation"]["markets"][0]["bookmakerByOutcome"]:
                    item["officialProfitRate"] = rate
                equal(subject.signal(row), reconstruct(row), f"synthetic.hot.{synthetic_cases}")
                synthetic_cases += 1
    changed = copy.deepcopy(sample)
    changed.update(result=2, homeScore=99, awayScore=0, settled=True, status=999, matchId="label-mutation")
    equal(subject.signal(changed), reconstruct(sample), "synthetic.targetLabelMutation")
    for score in (None, [0, 0, 0], [1, 1, 1], [0, 1, 0], [1, 1, 0], [1, 0, 1], [1, 1 - EPS / 2, 1 + EPS / 2], [1, 1 - EPS * 2, 1]):
        equal(subject.classification(score), decision(score), "synthetic.classification")
        equal(subject.draw_in_top_two(score), top_two(score), "synthetic.topTwo")
    comparisons["syntheticCases"] = synthetic_cases + 1
    for name in ("protocol.json", "analysis.json", "matches.json", "ui-reference.json", "source-audit.json", "report.html"):
        source_hashes[str(args.out / name).replace("\\", "/")] = hashlib.sha256((args.out / name).read_bytes()).hexdigest()
    source_hashes["scripts/draw-weighted-feature-study.py"] = hashlib.sha256(Path("scripts/draw-weighted-feature-study.py").read_bytes()).hexdigest()
    source_hashes[str(Path(__file__).resolve().relative_to(Path.cwd())).replace("\\", "/")] = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
    output = {"status": "passed" if not issues else "failed", "independence": "Separate implementation; no study import used for any historical row, source aggregation or summary reconstruction. Study functions invoked only for adversarial synthetic comparison.",
              "comparisons": dict(comparisons), "pairedHotEffect": paired, "summaries": summaries, "sourceHashes": source_hashes, "warnings": warnings, "issues": issues}
    (args.out / "review.json").write_text(json.dumps(output, ensure_ascii=False, indent=2, allow_nan=False), encoding="utf-8")
    print(json.dumps({"status": output["status"], "comparisons": output["comparisons"], "pairedHotEffect": paired, "warnings": warnings, "issueCount": len(issues), "issues": issues[:12]}, ensure_ascii=False))
    raise SystemExit(bool(issues))


if __name__ == "__main__":
    main()
