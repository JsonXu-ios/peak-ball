#!/usr/bin/env python3
"""Finite, input-only Asian/heat/European favourite transition audit.

Reads the existing frozen export only. No fitting, thresholds search, services,
business storage writes, browser, or external sources are used.
"""
from __future__ import annotations

import hashlib
import json
import math
import re
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATASET = ROOT / "reports/draw-full-feature-study/dataset.json"
OUTPUT = ROOT / "reports/draw-transition-study/asian-audit.json"
EPS = 1e-9
SIDES = ("home", "draw", "away")


def numeric(value):
    return type(value) in (int, float) and math.isfinite(value)


def prices(value):
    return value if isinstance(value, list) and len(value) == 3 and all(numeric(x) and x > 0 for x in value) else None


def movement(a, b):
    return "same" if abs(b - a) <= EPS else "up" if b > a else "down"


def line_side(value):
    return "home" if value > EPS else "away" if value < -EPS else "level"


def favorite(value):
    if prices(value) is None:
        return None, []
    minimum = min(value)
    selected = [SIDES[i] for i, raw in enumerate(value) if raw <= minimum + EPS]
    return selected[0] if len(selected) == 1 else "tie", selected


def transition_features(row):
    """Return only input-derived fixed masks; deliberately never reads result."""
    first, latest, heat = row.get("asianOpening"), row.get("asianLine"), row.get("homeHeat")
    valid = numeric(first) and numeric(latest)
    source = str(row.get("heatSource") or "")
    source_id = re.search(r"pankou ID ([^)]*)", source)
    data = {
        "asian.available": valid,
        "asian.sourceKnown": bool(source),
        "asian.sourceID": source_id.group(1) if source_id else None,
        "asian.opening": first if numeric(first) else None,
        "asian.latest": latest if numeric(latest) else None,
        "asian.move": None,
        "asian.depth": None,
        "asian.signTransition": None,
        "asian.zeroTransition": None,
        "asian.latestSide": line_side(latest) if valid else None,
        "heat.available": numeric(heat) and 0 <= heat <= 100,
        "heat.band": None,
        "heat.direction": None,
    }
    if valid:
        a, b = line_side(first), line_side(latest)
        data["asian.move"] = "unchanged" if abs(latest - first) <= EPS else "cross_zero" if a != "level" and b != "level" and a != b else "toward_home" if latest > first else "toward_away"
        depth = movement(abs(first), abs(latest))
        data["asian.depth"] = {"up": "deeper", "down": "shallower", "same": "same_depth"}[depth]
        data["asian.signTransition"] = a + "_to_" + b
        data["asian.zeroTransition"] = "both_zero" if a == b == "level" else "from_zero" if a == "level" else "to_zero" if b == "level" else "neither_zero"
    if data["heat.available"]:
        data["heat.band"] = "home_hot" if heat > 65 else "away_hot" if heat < 35 else "neutral"
        data["heat.direction"] = "home" if heat > 65 else "away" if heat < 35 else None
    for source in ("william", "average"):
        opening = (row.get("opening") or {}).get(source)
        current = (row.get("latest") or {}).get(source)
        a, a_members = favorite(opening)
        b, b_members = favorite(current)
        pair_valid = a is not None and b is not None
        data[source + ".available"] = pair_valid
        for phase, key, members in (("opening", a, a_members), ("latest", b, b_members)):
            data[source + "." + phase + "Favorite"] = key
            data[source + "." + phase + "FavoriteMembers"] = members
        data[source + ".favoriteTransition"] = a + "_to_" + b if pair_valid else None
        data[source + ".favoriteChanged"] = "tie_involved" if pair_valid and "tie" in (a, b) else "unchanged" if pair_valid and a == b else "changed" if pair_valid else None
        data[source + ".drawOddsMove"] = movement(opening[1], current[1]) if pair_valid else None
        data[source + ".openingFavoriteOddsMove"] = movement(opening[SIDES.index(a)], current[SIDES.index(a)]) if pair_valid and a in SIDES else None
        data[source + ".favoriteHeat"] = None
        if b is not None and data["heat.available"]:
            data[source + ".favoriteHeat"] = "favorite_tie" if b == "tie" else "draw_favorite" if b == "draw" else "neutral_heat" if data["heat.direction"] is None else "aligned" if b == data["heat.direction"] else "conflict"
        data[source + ".favoriteAsian"] = None
        if b is not None and valid:
            data[source + ".favoriteAsian"] = "favorite_tie" if b == "tie" else "draw_favorite" if b == "draw" else "level_line" if data["asian.latestSide"] == "level" else "aligned" if b == data["asian.latestSide"] else "conflict"
    return data


def group_definitions():
    """Fixed categorical families, no label-dependent category or threshold search."""
    result = [
        ("asian.move", ["toward_home", "toward_away", "cross_zero", "unchanged"], "实际亚盘初即变化；跨0单列"),
        ("asian.depth", ["deeper", "shallower", "same_depth"], "让受球绝对深度变化，不称真实实力变化"),
        ("asian.signTransition", [a + "_to_" + b for a in ("home", "level", "away") for b in ("home", "level", "away")], "让球方初即变化：home=主让，away=主受，level=平手"),
        ("asian.zeroTransition", ["both_zero", "from_zero", "to_zero", "neither_zero"], "零盘触及，区别于严格跨0"),
        ("heat.band", ["home_hot", "neutral", "away_hot"], "页面计算热度：主>65、35至65、主<35（客>65）"),
    ]
    for source in ("william", "average"):
        result += [
            (source + ".openingFavorite", [*SIDES, "tie"], source + "初盘最低赔率方向"),
            (source + ".latestFavorite", [*SIDES, "tie"], source + "即时最低赔率方向"),
            (source + ".favoriteTransition", [a + "_to_" + b for a in (*SIDES, "tie") for b in (*SIDES, "tie")], source + "初即最低赔率方向变化，任何并列单列"),
            (source + ".favoriteChanged", ["changed", "unchanged", "tie_involved"], source + "最低赔率方向是否变化"),
            (source + ".drawOddsMove", ["down", "same", "up"], source + "平局赔率初即涨跌"),
            (source + ".openingFavoriteOddsMove", ["down", "same", "up"], source + "初盘唯一最低赔率方向的自身赔率涨跌"),
            (source + ".favoriteHeat", ["aligned", "conflict", "neutral_heat", "favorite_tie", "draw_favorite"], source + "即时最低赔率方向与>65%计算热度一致性"),
            (source + ".favoriteAsian", ["aligned", "conflict", "level_line", "favorite_tie", "draw_favorite"], source + "即时最低赔率方向与即时亚盘让球方一致性"),
        ]
    return result


def stats(rows):
    counts = Counter(row["result"] for row, _ in rows)
    n = len(rows)
    return {"matches": n, "home": counts[0], "draws": counts[1], "away": counts[2], "precision": counts[1] / n if n else None}


def direction_stats(rows, key):
    known = [(row, data[key]) for row, data in rows if data.get(key) in SIDES]
    non_draw = [(row, side) for row, side in known if side != "draw"]
    def summarize(items):
        n = len(items)
        win = sum(row["result"] == SIDES.index(side) for row, side in items)
        draw = sum(row["result"] == 1 for row, _ in items)
        loss = sum(row["result"] != 1 and row["result"] != SIDES.index(side) for row, side in items)
        return {"known": n, "directionWins": win, "directionWinRate": win / n if n else None, "draws": draw, "oppositeWins": loss,
                "notDirectionWin": n - win}
    return {"allUniqueDirections": summarize(known), "homeAwayUniqueDirections": summarize(non_draw)}


def analyze(rows):
    whole = stats(rows)
    result = {"baseline": whole, "sourceAvailability": {}, "groups": []}
    for key in ["asian.available", "asian.sourceKnown", "heat.available", "william.available", "average.available"]:
        available = [(r, f) for r, f in rows if f[key]]
        metrics = stats(available)
        metrics["missingMatches"] = whole["matches"] - metrics["matches"]
        metrics["missingDraws"] = whole["draws"] - metrics["draws"]
        result["sourceAvailability"][key] = metrics
    for key, categories, description in group_definitions():
        eligible = [(r, f) for r, f in rows if f.get(key) is not None]
        base = stats(eligible)
        groups = []
        for category in categories:
            selected = [(r, f) for r, f in eligible if f[key] == category]
            metric = stats(selected)
            metric.update({"category": category,
                           "baselineDrawRate": base["precision"],
                           "liftPercentagePoints": (metric["precision"] - base["precision"]) * 100 if metric["precision"] is not None and base["precision"] is not None else None,
                           "drawCoverageOfCohort": metric["draws"] / whole["draws"] if whole["draws"] else None,
                           "drawCoverageOfAvailable": metric["draws"] / base["draws"] if base["draws"] else None,
                           "sourceAvailableMatches": base["matches"], "sourceAvailableDraws": base["draws"],
                           "directionResults": {direction: direction_stats(selected, direction) for direction in ("heat.direction", "william.openingFavorite", "william.latestFavorite", "average.openingFavorite", "average.latestFavorite")},
                           "matchIds": [r["matchId"] for r, _ in selected]})
            groups.append(metric)
        assert sum(group["matches"] for group in groups) == base["matches"], key
        assert sum(group["draws"] for group in groups) == base["draws"], key
        result["groups"].append({"key": key, "description": description, "sourceAvailable": base,
                                 "missingMatches": whole["matches"] - base["matches"], "missingDraws": whole["draws"] - base["draws"], "categories": groups})
    return result


def orientation_audit(rows):
    selected = [(row, f) for row, f in rows if f["asian.available"]]
    source_ids = Counter(f["asian.sourceID"] or "unrecorded" for _, f in selected)
    bet365_matches = 0
    raw_examples = []
    negative_received = 0
    pair_match = 0
    mismatch = []
    words = {"平手": 0, "平": 0, "半球": .5, "半": .5, "一球": 1, "一": 1, "一球半": 1.5, "球半": 1.5,
             "二球": 2, "两球": 2, "二": 2, "二球半": 2.5, "两球半": 2.5, "三球": 3, "三": 3, "三球半": 3.5,
             "四球": 4, "四": 4, "四球半": 4.5, "五球": 5, "五球半": 5.5, "六球": 6, "六球半": 6.5}
    def raw_line(value):
        if not isinstance(value, str) or not value.strip() or value.strip() == "-":
            return None
        text = value.strip()
        try:
            value = float(text)
            return value if math.isfinite(value) else None
        except ValueError:
            pass
        sign = -1 if text.startswith("受") else 1
        text = text.removeprefix("受")
        if text in words:
            return sign * words[text]
        parts = text.split("/")
        if len(parts) != 2:
            return None
        parsed = []
        for part in parts:
            try:
                value = float(part)
            except ValueError:
                value = words.get(part)
            if value is None or not math.isfinite(value):
                return None
            parsed.append(value)
        return sign * sum(parsed) / 2
    for row, f in selected:
        if f["asian.sourceID"] != "8":
            continue
        quote = next((q.get("asianHandicap") or {} for q in row.get("companyQuotes") or [] if q.get("key") == "bet365"), {})
        if quote.get("source") == "pankou_moneys":
            bet365_matches += 1
        raw_a, raw_b = raw_line(quote.get("opening")), raw_line(quote.get("latest"))
        if raw_a is not None and raw_b is not None and abs(raw_a - f["asian.opening"]) <= EPS and abs(raw_b - f["asian.latest"]) <= EPS:
            pair_match += 1
        else:
            mismatch.append({"matchId": row["matchId"], "rawOpening": quote.get("opening"), "rawLatest": quote.get("latest"), "numericOpening": f["asian.opening"], "numericLatest": f["asian.latest"]})
        raw = quote.get("latest")
        if isinstance(raw, str) and raw.startswith("受") and f["asian.latest"] < 0:
            negative_received += 1
        if len(raw_examples) < 12 and quote.get("opening") and raw and row.get("asianOpening") != row.get("asianLine"):
            raw_examples.append({"matchId": row["matchId"], "rawOpening": quote["opening"], "rawLatest": raw,
                                 "numericOpening": f["asian.opening"], "numericLatest": f["asian.latest"], "source": row["heatSource"]})
    return {"completeSameQuotePairs": len(selected), "recordedCompanyIdentity": sum(f["asian.sourceKnown"] for _, f in selected),
            "unrecordedCompanyIdentity": sum(not f["asian.sourceKnown"] for _, f in selected), "selectedSourceIds": dict(source_ids),
            "bet365RecordedSourceWithActualQuote": bet365_matches, "bet365NegativeNumericWithRawReceivedPrefix": negative_received,
            "bet365BothEndpointsIndependentlyParsedMatch": pair_match, "bet365EndpointMismatches": mismatch,
            "rawExamples": raw_examples,
            "codeMeaning": "Project drawLine/parsePankouLine maps ordinary Chinese Asian line to positive and leading 受 to negative. Positive=home gives that many goals; negative=home receives. This is the source's home-giving-amount convention, not an additive score adjustment.",
            "movementMeaning": "toward_home denotes an increase in the source home-giving amount, not proven actual home-team strength or bookmaker intent. Strict negative-to-positive or reverse is cross_zero; touches of zero are separately identified.",
            "sameCompanyProvenance": "drawFeatureStudyBuildRow uses opening/latest from one drawStudyAsianSource quote. Bet365 preferred, then asia ID8, then first actual asia row; it never fills one endpoint from another company. HeatSource identity is only populated when average prices and both endpoints also permit pressurePair; 23 complete pairs therefore lack recorded identity."}


def selfcheck():
    def row(a, b, h=50): return {"asianOpening": a, "asianLine": b, "homeHeat": h}
    assert transition_features(row(.25, .5))["asian.move"] == "toward_home"
    assert transition_features(row(-.5, -.25))["asian.move"] == "toward_home"
    assert transition_features(row(.5, -.25))["asian.move"] == "cross_zero"
    assert transition_features(row(0, -.25))["asian.move"] == "toward_away"
    assert transition_features(row(0, 0))["asian.move"] == "unchanged"
    assert transition_features(row(.5, None))["asian.move"] is None
    assert transition_features(row(.5, .5, 65))["heat.band"] == "neutral"
    assert transition_features(row(.5, .5, 35))["heat.band"] == "neutral"
    assert transition_features(row(.5, .5, 65.01))["heat.direction"] == "home"
    assert transition_features(row(.5, .5, 34.99))["heat.direction"] == "away"
    assert favorite([2, 3, 2]) == ("tie", ["home", "away"])
    assert favorite([None, 3, 2]) == (None, [])


def main():
    selfcheck()
    raw = DATASET.read_bytes()
    rows = json.loads(raw)["rows"]
    assert len(rows) == 4161 and sum(row["result"] == 1 for row in rows) == 970
    data = [(row, transition_features(row)) for row in rows]
    # Deliberate target-outcome mutations must not change any input-derived mask.
    for row, features in data:
        altered = {**row, "result": (row["result"] + 1) % 3, "homeScore": 99, "awayScore": 98}
        assert transition_features(altered) == features
    source_paths = [DATASET, Path(__file__), ROOT / "go_server/handlers/draw_feature_study.go", ROOT / "go_server/handlers/analysis.go", ROOT / "go_server/handlers/draw.go", ROOT / "go_server/handlers/draw_history_test.go"]
    result = {
        "version": "asian-euro-transition-audit-v1",
        "scope": "Local frozen export only; all 4161 confirmed completed; sporttery/non-sporttery by nonblank jingcaiId.",
        "definitions": {"asian": "Real finite strict export asianOpening/asianLine; both from one selected source quote; no filling.",
                        "heat": "Strict export homeHeat; >65 home_hot, <35 away_hot, inclusive35..65 neutral; computed pressure, not verified actual money.",
                        "favorite": "Separate William/average original price vectors; full positive triple; unique lowest among home/draw/away; any tie is separate.",
                        "favoriteHeat": "Latest unique home/away lowest price agrees/disagrees with strict >65 heat side. Neutral heat, favorite tie, draw favorite separate.",
                        "drawOddsMove": "Same source opening-to-latest draw price decreases/unchanged/increases. Uses EPS1e-9, no optimized thresholds.",
                        "metrics": "Precision=draws/matches; baseline uses exactly that group's complete-source pool. Cohort and source-available draw coverage both reported. Direction win excludes ties; home/away result breakdown distinguishes draw from favorite loss.",
                        "limitations": "Retrospective snapshot association. Capture times not proven pre-kickoff; heat shares are calculated and structurally depend on prices and Asian line. Correlation cannot demonstrate bookmaker intent or actual strength."},
        "sourceHashes": {str(p.relative_to(ROOT)).replace('\\', '/'): hashlib.sha256(p.read_bytes()).hexdigest() for p in source_paths},
        "sourceOrientation": orientation_audit(data),
        "cohorts": {name: analyze([(r, f) for r, f in data if name == "all" or bool(str(r.get("jingcaiId") or "").strip()) == (name == "sporttery")]) for name in ("all", "sporttery", "non_sporttery")},
        "featureRows": [{"matchId": row["matchId"], **features} for row, features in data],
        "validation": {"selfcheckPassed": True, "targetMutationRows": len(rows), "groupPartitionsPassed": True, "externalIO": False, "serviceStarted": False},
    }
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    compact = {"output": str(OUTPUT.relative_to(ROOT)), "orientation": {k: v for k, v in result["sourceOrientation"].items() if k not in ("rawExamples", "codeMeaning", "movementMeaning", "sameCompanyProvenance")}}
    compact["cohorts"] = {name: {"baseline": cohort["baseline"], "selectedFamilies": [{"key": family["key"], "available": family["sourceAvailable"], "categories": [{k: group[k] for k in ("category", "matches", "draws", "precision", "liftPercentagePoints", "drawCoverageOfCohort")} for group in family["categories"]]} for family in cohort["groups"] if family["key"] in ("asian.move", "heat.band", "william.favoriteHeat", "average.favoriteHeat", "william.drawOddsMove", "average.drawOddsMove")]} for name, cohort in result["cohorts"].items()}
    print(json.dumps(compact, ensure_ascii=True, indent=2))


if __name__ == "__main__":
    main()
