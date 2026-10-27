#!/usr/bin/env python3
"""Read-only source/ROI-bin audit for the draw-transition research.

No training, live database, Redis, service or network operations. All source
rates, local quoted-odds amounts and non-handicap simulations remain separate.
"""
from __future__ import annotations

import hashlib
import json
import math
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATASET = ROOT / "reports/draw-full-feature-study/dataset.json"
OUTPUT = ROOT / "reports/draw-transition-study/profit-audit.json"
OUTCOMES = ("home", "draw", "away")
EPS = 1e-9
BUCKETS = ("below_negative_28", "negative_28_to_below_zero", "zero_through_28", "above_28")
SOURCES = {
    "official_sporttery_rate": {"key": "sporttery", "simulated": False, "kind": "source_rate", "label": "竞彩源站胜平负官方盈亏率（jyykSpf hy/dy/ay）"},
    "quoted_sporttery_roi": {"key": "sporttery", "simulated": False, "kind": "computed_roi", "label": "竞彩真实报价下本地测算ROI"},
    "quoted_william_roi": {"key": "william", "simulated": False, "kind": "computed_roi", "label": "威廉真实报价下本地测算ROI"},
    "quoted_bet365_roi": {"key": "bet365", "simulated": False, "kind": "computed_roi", "label": "Bet365真实报价下本地测算ROI"},
    "simulated_sporttery_fallback_roi": {"key": "sporttery", "simulated": True, "kind": "computed_roi", "label": "无真实竞彩报价时的非让球模拟ROI"},
    "simulated_sporttery_comparison_roi": {"key": "sportterySim", "simulated": True, "kind": "computed_roi", "label": "真实竞彩旁的非让球模拟对比ROI"},
    "local_average_roi": {"key": "localAverageOdds", "simulated": False, "kind": "computed_roi", "local": True, "label": "本地均赔市场测算ROI（单列，非公司实际盈亏）"},
}


def obj(value):
    return value if isinstance(value, dict) else {}


def number(value):
    return float(value) if type(value) in (int, float) and math.isfinite(value) else None


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def counts(rows):
    outcomes = Counter(row["result"] for row in rows)
    n = len(rows)
    return {"n": n, "home": outcomes[0], "draw": outcomes[1], "away": outcomes[2], "drawRate": outcomes[1] / n if n else None}


def interval(value):
    if value < -28:
        return BUCKETS[0]
    if value < 0:
        return BUCKETS[1]
    if value <= 28:
        return BUCKETS[2]
    return BUCKETS[3]


def markets(row):
    frontend = obj(row.get("frontend"))
    simulation = obj(frontend.get("roiSimulation"))
    # Root roiSimulation intentionally projects out William/Bet365. Prefer
    # full frontend markets and use the root only if no frontend block exists.
    if not simulation:
        simulation = obj(row.get("roiSimulation"))
    values = simulation.get("markets")
    return [obj(value) for value in values] if isinstance(values, list) else []


def source_row(row, specification):
    if specification.get("local"):
        market = obj(obj(row.get("frontend")).get("localMarket"))
        candidates = [market] if market else []
    else:
        candidates = [market for market in markets(row) if market.get("key") == specification["key"] and (market.get("simulated") is True) == specification["simulated"]]
    market = candidates[0] if len(candidates) == 1 else {}
    details = market.get("bookmakerByOutcome")
    details = [obj(d) for d in details] if isinstance(details, list) else []
    flags = obj(row.get("profitInputs")).get("officialProfitRatePresence")
    flags = flags if isinstance(flags, list) and len(flags) == 3 else [False] * 3
    raw_fields = obj(obj(row.get("analysisInputs")).get("tradeFields"))
    distribution_sources = obj(obj(row.get("profitInputs")).get("distributionSources"))
    if specification["key"] not in distribution_sources:
        distribution_sources = obj(obj(row.get("analysisInputs")).get("distributionSources"))
    distribution = "localSanhu" if specification.get("local") else distribution_sources.get(specification["key"], "unknown")
    rates, prices, issues, local_rois = [], [], [], []
    finite_amount, support_valid = [], False
    if distribution in ("tzbl", "jyykSpf"):
        support = [number(raw_fields.get(distribution + "." + key)) for key in ("hsupportRate", "dsupportRate", "asupportRate")]
        support_valid = all(v is not None and 0 <= v <= 100 for v in support) and sum(v for v in support if v is not None) > 0
    for index, outcome in enumerate(OUTCOMES):
        matching = [detail for detail in details if detail.get("outcome") == outcome]
        detail = matching[0] if len(matching) == 1 else {}
        price = number(detail.get("odds"))
        priced = market.get("oddsAvailable") is True and price is not None and price > 0
        price = price if priced else None
        prices.append(price)
        amount, stake = number(detail.get("bookmakerProfit")), number(detail.get("totalStake"))
        amount_available = priced and detail.get("available") is True and amount is not None and stake is not None and stake > 0
        finite_amount.append(amount_available)
        displayed_roi = number(detail.get("bookmakerRoi"))
        local_roi = displayed_roi if amount_available else None
        local_rois.append(local_roi)
        if amount_available and displayed_roi is not None and abs(displayed_roi - amount / stake * 100) > .005000001:
            issues.append(outcome + ": displayed local ROI inconsistent with amount/base")
        if specification["kind"] == "source_rate":
            value = number(detail.get("officialProfitRate"))
            raw = number(raw_fields.get("jyykSpf." + ("hy", "dy", "ay")[index]))
            value = value if priced and flags[index] is True else None
            if value is not None and (raw is None or abs(value - raw) > .005000001):
                issues.append(outcome + ": official pointer/presence differs from raw source")
        else:
            value = local_roi
        rates.append(value)
    complete_prices = all(value is not None for value in prices)
    favorite = [i for i, price in enumerate(prices) if price <= min(prices) + EPS] if complete_prices else []
    return {"row": row, "market": market, "present": len(candidates) == 1, "prices": prices, "rates": rates,
            "favorite": favorite, "completePrices": complete_prices,
            "distribution": distribution, "rawSupportVerified": support_valid,
            "officialSourceOdds": [number(raw_fields.get("jyykSpf." + key)) for key in ("h", "d", "a")],
            "amountAvailable": finite_amount, "localRoi": local_rois, "issues": issues}


def favorite_result(records):
    unique = {record["row"]["matchId"]: record for record in records}
    values = list(unique.values())
    result = counts([record["row"] for record in values])
    favorite_win = sum(record["row"]["result"] != 1 and record["row"]["result"] in record["favorite"] for record in values)
    underdog_win = sum(record["row"]["result"] != 1 and record["row"]["result"] not in record["favorite"] for record in values)
    result.update(favoriteNonDrawWin=favorite_win, draw=result["draw"], underdogNonDrawWin=underdog_win,
                  favoriteWinRate=favorite_win / len(values) if values else None,
                  minimumOddsOutcomeHit=sum(record["row"]["result"] in record["favorite"] for record in values),
                  tieMatches=sum(len(record["favorite"]) > 1 for record in values),
                  minimumSetIncludesDraw=sum(1 in record["favorite"] for record in values))
    assert result["favoriteNonDrawWin"] + result["draw"] + result["underdogNonDrawWin"] == result["n"]
    return result


def audit_source(rows, specification):
    records = [source_row(row, specification) for row in rows]
    present = [record for record in records if record["present"]]
    any_rate = [record for record in records if any(value is not None for value in record["rates"])]
    complete_rate = [record for record in records if all(value is not None for value in record["rates"])]
    complete_price = [record for record in records if record["completePrices"]]
    favorites_usable = [record for record in complete_price if any(record["rates"][i] is not None for i in record["favorite"])]
    official_vs_local = [record for record in any_rate if all(record["rates"][i] is not None and record["localRoi"][i] is not None for i in range(3))]
    report = {
        "definition": specification,
        "marketPresent": counts([record["row"] for record in present]),
        "anyRateAvailable": counts([record["row"] for record in any_rate]),
        "allThreeRatesAvailable": counts([record["row"] for record in complete_rate]),
        "allThreeActualOwnOdds": counts([record["row"] for record in complete_price]),
        "favoriteRateAvailable": favorite_result(favorites_usable),
        "allFavoriteRatesKnown": favorite_result([record for record in complete_price if all(record["rates"][i] is not None for i in record["favorite"])]),
        "ownMinimumOddsTies": favorite_result([record for record in complete_price if len(record["favorite"]) > 1]),
        "directionAvailability": {name: counts([record["row"] for record in records if record["rates"][i] is not None]) for i, name in enumerate(OUTCOMES)},
        "distributionSourceAmongAvailable": dict(Counter(record["distribution"] for record in any_rate)),
        "verifiedSourceSupportAmongAvailable": counts([record["row"] for record in any_rate if record["rawSupportVerified"]]),
        "displayLabelsAmongPresent": dict(Counter(str(record["market"].get("name", "")) for record in present)),
        "buckets": {}, "directionBuckets": {},
        "issues": [{"matchId": record["row"]["matchId"], "problems": record["issues"]} for record in records if record["issues"]],
    }
    if specification["kind"] == "source_rate":
        report["sourceRateAndLocalRoiBothAvailable"] = counts([record["row"] for record in official_vs_local])
        report["sourceRateEqualsLocalRoiAllThree"] = counts([record["row"] for record in official_vs_local if all(abs(record["rates"][i] - record["localRoi"][i]) <= .005000001 for i in range(3))])
        source_odds = [record for record in any_rate if record["completePrices"] and all(value is not None and value > 0 for value in record["officialSourceOdds"])]
        source_equal = [record for record in source_odds if all(abs(record["prices"][i] - record["officialSourceOdds"][i]) <= EPS for i in range(3))]
        source_different = [record for record in source_odds if not all(abs(record["prices"][i] - record["officialSourceOdds"][i]) <= EPS for i in range(3))]
        report["officialSourceOriginalOddsComplete"] = counts([record["row"] for record in source_odds])
        report["officialSourceOriginalOddsEqualSelectedCurrentOdds"] = counts([record["row"] for record in source_equal])
        report["officialSourceOriginalOddsDifferentFromSelectedCurrentOdds"] = counts([record["row"] for record in source_different])
        report["sourceOddsDifferenceMatchIds"] = [record["row"]["matchId"] for record in source_different]
        aligned = [{**record, "favorite": [i for i, value in enumerate(record["officialSourceOdds"]) if value <= min(record["officialSourceOdds"]) + EPS]} for record in source_odds]
        report["currentVsOriginalSourceMinimumSetsDiffer"] = counts([record["row"] for record in aligned if record["favorite"] != next(current["favorite"] for current in source_odds if current["row"]["matchId"] == record["row"]["matchId"])])
        report["originalSourceOddsMinimumBandsSensitivity"] = {}
        for bucket in BUCKETS:
            selected = [record for record in aligned if any(record["rates"][i] is not None and interval(record["rates"][i]) == bucket for i in record["favorite"])]
            report["originalSourceOddsMinimumBandsSensitivity"][bucket] = favorite_result(selected)
    memberships = {}
    for bucket in BUCKETS:
        entries = [(record, i) for record in favorites_usable for i in record["favorite"] if record["rates"][i] is not None and interval(record["rates"][i]) == bucket]
        unique = {record["row"]["matchId"]: record for record, _ in entries}
        report["buckets"][bucket] = {
            **favorite_result(list(unique.values())), "favoriteDirectionObservations": len(entries),
            "favoriteDirections": dict(Counter(OUTCOMES[i] for _, i in entries)),
            "favoriteRateRange": [min(record["rates"][i] for record, i in entries), max(record["rates"][i] for record, i in entries)] if entries else [None, None],
            "distributionSources": dict(Counter(record["distribution"] for record in unique.values())),
            "matchIds": list(unique),
        }
        memberships[bucket] = set(unique)
    overlap = []
    for record in favorites_usable:
        active = [bucket for bucket in BUCKETS if record["row"]["matchId"] in memberships[bucket]]
        if len(active) > 1:
            overlap.append({"matchId": record["row"]["matchId"], "buckets": active, "favorite": [OUTCOMES[i] for i in record["favorite"]]})
    report["crossBucketTieOverlap"] = overlap
    report["sumBucketUniqueMatches"] = sum(value["n"] for value in report["buckets"].values())
    assert set().union(*memberships.values()) == {record["row"]["matchId"] for record in favorites_usable}
    for i, direction in enumerate(OUTCOMES):
        report["directionBuckets"][direction] = {
            bucket: counts([record["row"] for record in favorites_usable if i in record["favorite"] and record["rates"][i] is not None and interval(record["rates"][i]) == bucket]) for bucket in BUCKETS
        }
    # No minimum-price selection: useful as an explicit control showing
    # whether a favorite-only observation differs from a generic rate band.
    report["allOutcomeRateBuckets"] = {direction: {bucket: counts([record["row"] for record in records if record["rates"][i] is not None and interval(record["rates"][i]) == bucket]) for bucket in BUCKETS} for i, direction in enumerate(OUTCOMES)}
    return report


def main():
    data = json.loads(DATASET.read_text(encoding="utf-8-sig"))
    rows = data["rows"]
    marked = [row for row in rows if str(row.get("jingcaiId") or "").strip()]
    unmarked = [row for row in rows if not str(row.get("jingcaiId") or "").strip()]
    assert len(rows) == 4161 and len(marked) == 511 and sum(row["result"] == 1 for row in marked) == 124
    paths = [DATASET, Path(__file__), ROOT / "go_server/handlers/draw_feature_study.go", ROOT / "go_server/handlers/analysis_weight.go", ROOT / "go_server/handlers/analysis_simulation.go"]
    report = {
        "purpose": "Local source-specific profit audit and own-market minimum-price favorite ROI bands, retrospective only",
        "datasetVersion": data.get("version"),
        "sourceHashes": {str(path.relative_to(ROOT)).replace("\\", "/"): sha(path) for path in paths},
        "definitions": {
            "cohorts": "All finished rows; sportteryMarked=nonblank jingcaiId; nonSportteryMarked=its exact complement. Presence of source rate does not itself change match scope.",
            "sourceRate": "Unique non-simulated sporttery market, oddsAvailable, positive priced outcome, finite officialProfitRate and true corresponding profitInputs.officialProfitRatePresence; raw jyykSpf hy/dy/ay cross-checked. Local amount available is independent and not required.",
            "computedRoi": "Stored bookmakerRoi percent, requiring real priced outcome, local amount available, finite bookmakerProfit and positive totalStake. Checked against 100*profit/stake. This is calculated ROI under a supplied/model support distribution, not actual observed company funds.",
            "simulation": "Only non-handicap simulated key=sporttery or sportterySim; sportteryRqspf and sportteryRqspfSim excluded. A synthetic quote is not an actual quoted company price.",
            "favorite": "Each source market's OWN complete current odds triple: include every outcome <= min(odds)+1e-9. Never borrow another company's odds or choose one arbitrarily in a tie.",
            "favoriteResultPartition": "favoriteNonDrawWin + draw + underdogNonDrawWin = unique matches. Any actual draw is in draw even when draw itself has the minimum odds. minimumOddsOutcomeHit separately reports literal membership.",
            "ties": "In each ROI bucket matches are deduplicated. A tied favorite with two ROI values in different buckets belongs to both; favoriteDirectionObservations and crossBucketTieOverlap disclose this. Bucket counts can exceed unique eligible rows only for these ties.",
            "intervalsPercent": {"below_negative_28": "ROI < -28", "negative_28_to_below_zero": "-28 <= ROI < 0", "zero_through_28": "0 <= ROI <= 28", "above_28": "ROI > 28"},
            "profitTiming": "Only latest snapshot rates/distributions/profit amounts are exported. The snapshot does not provide observed opening-vs-latest profit histories. Recomputing opening odds with latest support is a counterfactual and cannot be called actual opening profit.",
            "sourcePriceSynchronization": "The source jyykSpf h/d/a prices may differ from the selected current sporttery quote (which can prefer the recorded SPF odds endpoint). Source-rate and price synchronization is unproven without timestamps; price-equality counts are supplied explicitly.",
            "capture": "Actual capture time is not established before kickoff for these exports; all associations are historical snapshot associations.",
        },
        "sequenceAudit": {
            "openingPhaseFields": sorted(set().union(*(set(obj(row.get("opening"))) for row in rows))),
            "latestPhaseFields": sorted(set().union(*(set(obj(row.get("latest"))) for row in rows))),
            "frontendComparisonStageFields": sorted(set().union(*(set(obj(stage)) for row in rows for stage in obj(row.get("frontend")).get("comparisonStages", [])))),
            "actualOpeningProfitRateSequences": 0,
            "actualOpeningProfitAmountSequences": 0,
            "actualTimeStampedProfitChanges": 0,
            "ownOddsOpeningLatestAvailableSeparately": True,
            "sourceCodeEvidence": ["drawStudyPhase exports Kelly/company odds/differences only, no profit fields", "decorateSportterySpfMarket copies one current jyykSpf HY/DY/AY triple", "buildBookmakerMarkets computes one ROI table from current quote and one distribution", "drawStudyProfitMarkets is a filtered current-market projection, not a chronological series"],
            "rootRoiMarketObjects": dict(Counter(market.get("key", "unknown") + (".simulated" if market.get("simulated") is True else ".quoted") for row in rows for market in obj(row.get("roiSimulation")).get("markets", []))),
            "fullFrontendMarketObjects": dict(Counter(market.get("key", "unknown") + (".simulated" if market.get("simulated") is True else ".quoted") for row in rows for market in markets(row))),
        },
        "cohorts": {},
    }
    for cohort, chosen in (("allFinished", rows), ("sportteryMarked", marked), ("nonSportteryMarked", unmarked)):
        report["cohorts"][cohort] = {"baseline": counts(chosen), "sources": {key: audit_source(chosen, specification) for key, specification in SOURCES.items()}}
    report["unmarkedMatchesWithOfficialRates"] = [row["matchId"] for row in unmarked if any(value is not None for value in source_row(row, SOURCES["official_sporttery_rate"])["rates"])]
    additive_checks = 0
    for source in SOURCES:
        for group in ("marketPresent", "anyRateAvailable", "allThreeRatesAvailable", "allThreeActualOwnOdds", "favoriteRateAvailable"):
            for field in ("n", "home", "draw", "away"):
                assert report["cohorts"]["allFinished"]["sources"][source][group][field] == sum(report["cohorts"][scope]["sources"][source][group][field] for scope in ("sportteryMarked", "nonSportteryMarked"))
                additive_checks += 1
        for bucket in BUCKETS:
            for field in ("n", "home", "draw", "away", "favoriteNonDrawWin", "underdogNonDrawWin"):
                assert report["cohorts"]["allFinished"]["sources"][source]["buckets"][bucket][field] == sum(report["cohorts"][scope]["sources"][source]["buckets"][bucket][field] for scope in ("sportteryMarked", "nonSportteryMarked"))
                additive_checks += 1
    assert [interval(value) for value in (-28.01, -28., -.01, 0., 28., 28.01)] == [BUCKETS[0], BUCKETS[1], BUCKETS[1], BUCKETS[2], BUCKETS[2], BUCKETS[3]]
    report["arithmeticChecks"] = {"cohortAdditivityChecks": additive_checks, "bucketBoundaryChecks": 6,
                                  "favoriteResultPartitions": "asserted for every source/cohort/bucket", "tiesDisclosed": True}
    report["issues"] = [{"cohort": cohort, "source": source, "issues": value["issues"]} for cohort, group in report["cohorts"].items() for source, value in group["sources"].items() if value["issues"]]
    OUTPUT.parent.mkdir(parents=True, exist_ok=True)
    OUTPUT.write_text(json.dumps(report, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    compact = {cohort: {source: {"available": value["allThreeRatesAvailable"], "buckets": {bucket: {key: score[key] for key in ("n", "home", "draw", "away", "favoriteNonDrawWin", "underdogNonDrawWin")} for bucket, score in value["buckets"].items()}, "distributionSources": value["distributionSourceAmongAvailable"]} for source, value in group["sources"].items()} for cohort, group in report["cohorts"].items()}
    print(json.dumps({"issues": report["issues"], "unmarkedOfficialRateMatchIds": report["unmarkedMatchesWithOfficialRates"], "cohorts": compact}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
