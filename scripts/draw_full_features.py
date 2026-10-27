#!/usr/bin/env python3
"""Finite, label-blind feature whitelist for the full draw research export.

No fitting, database, network, or service operations occur here. Numeric missing
values are NaN. `market` describes provenance, not verified pre-kickoff capture.
Legacy frontend history/retail/platform calculations are snapshotDerived and
can be excluded as a complete block. Safe history independently checks kickoff
plus two hours, rejects the target ID, and recomputes outcomes from past goals.
"""
from __future__ import annotations

import argparse
import copy
import json
import math
import re
from datetime import datetime, timedelta
from pathlib import Path

import numpy as np

EPS = 1e-9
OUTCOMES = ("home", "draw", "away")
CN = {"home": "胜", "draw": "平", "away": "负"}
COMPANIES = ("sporttery", "william", "bet365", "average")
SOURCE_CN = {"sporttery": "竞彩", "william": "威廉", "bet365": "Bet365", "average": "平均欧赔", "selected": "页面取样公司", "simulated": "非让球模拟", "local": "本地均赔测算"}


def number(value):
    return type(value) in (int, float) and math.isfinite(value)


def scalar(value, positive=False):
    if isinstance(value, str):
        try:
            value = float(value.strip())
        except (ValueError, TypeError):
            return None
    return float(value) if number(value) and (not positive or value > 0) else None


def percentage(value):
    return scalar(value[:-1]) if isinstance(value, str) and value.endswith("%") else scalar(value)


def triple(value, positive=False):
    if not isinstance(value, (list, tuple)) or len(value) != 3:
        return None
    result = tuple(scalar(v, positive) for v in value)
    return result if all(v is not None for v in result) else None


def mapping(value):
    return value if isinstance(value, dict) else {}


def at(value, path, default=None):
    for part in path.split("."):
        if not isinstance(value, dict) or part not in value:
            return default
        value = value[part]
    return value


def line_value(value, goals=False):
    """Parse the raw company's own line; never borrow another phase/company."""
    if number(value):
        result = float(value)
    elif isinstance(value, str) and value.strip():
        text = value.strip().replace(" ", "").replace("／", "/")
        negative = text.startswith("受让") or text.startswith("受")
        if negative:
            text = text[2:] if text.startswith("受让") else text[1:]
        words = {"平手": 0, "平": 0, "半球": .5, "半": .5, "一球": 1, "一": 1,
                 "球半": 1.5, "一球半": 1.5, "两球": 2, "二球": 2, "两": 2,
                 "两球半": 2.5, "二球半": 2.5, "三球": 3, "三": 3, "三球半": 3.5,
                 "四球": 4, "四": 4, "四球半": 4.5, "五球": 5, "五球半": 5.5,
                 "六球": 6, "六球半": 6.5, "七球": 7}
        parts = text.split("/")
        if len(parts) > 2:
            return None
        parsed = [float(words[p]) if p in words else scalar(p) for p in parts]
        if any(v is None for v in parsed):
            return None
        result = sum(parsed) / len(parsed)
        if negative:
            result = -abs(result)
    else:
        return None
    return result if math.isfinite(result) and abs(result) <= 15 and (not goals or result > 0) else None


def timestamp(value):
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
        return parsed if parsed.tzinfo is not None else None
    except ValueError:
        return None


def kelly_source_id(*descriptions):
    """Only compare initial/current Kelly when an explicit company ID agrees."""
    ids = set()
    for description in descriptions:
        if isinstance(description, str):
            found = re.search(r"(?:欧赔\s*)?ID\s*[:：]?\s*(\d+)", description, re.IGNORECASE)
            if found:
                ids.add(found.group(1))
    return next(iter(ids)) if len(ids) == 1 else None


def safe_records(row, key):
    target = timestamp(row.get("matchTime"))
    block = at(row, "preMatchHistory." + key)
    if target is None or not isinstance(block, dict) or not isinstance(block.get("matches"), list):
        return None
    result, seen = [], set()
    for record in block["matches"]:
        r = mapping(record)
        kickoff = timestamp(r.get("matchTime"))
        hg, ag = r.get("homeScore"), r.get("awayScore")
        if kickoff is None or kickoff + timedelta(hours=2) >= target or str(r.get("matchId", "")) == str(row.get("matchId", "")):
            continue
        if not all(number(v) and 0 <= v <= 30 and int(v) == v for v in (hg, ag)) or r.get("venue") not in ("home", "away"):
            continue
        key_id = str(r.get("matchId") or (str(kickoff) + "|" + str(r.get("home")) + "|" + str(r.get("guest"))))
        if key_id in seen:
            continue
        seen.add(key_id)
        gf, ga = (hg, ag) if r["venue"] == "home" else (ag, hg)
        result.append((kickoff, key_id, float(gf), float(ga)))
    result.sort(key=lambda item: (item[0], item[1]), reverse=True)
    return result


HISTORY_METRICS = (
    ("n", "样本数", "count"), ("draws", "平局数", "count"), ("wins", "获胜数", "count"), ("losses", "落败数", "count"),
    ("drawRate", "平局率", "probability"), ("winRate", "胜率", "probability"), ("lossRate", "负率", "probability"),
    ("goalsFor", "场均进球", "goals"), ("goalsAgainst", "场均失球", "goals"), ("totalGoals", "场均总进球", "goals"),
    ("goalDifference", "场均净胜球", "goals"), ("under25Rate", "总球不超过2球比例", "probability"),
    ("zeroZeroRate", "0比0比例", "probability"), ("oneOneRate", "1比1比例", "probability"),
    ("cleanSheetRate", "零封比例", "probability"), ("scorelessRate", "不进球比例", "probability"),
    ("drawStreak", "最近连续平局数", "count"),
)


def history_stats(records, limit):
    if records is None:
        return {key: None for key, _, _ in HISTORY_METRICS}
    selected = records[:limit] if limit else records
    n = len(selected)
    draws = sum(gf == ga for _, _, gf, ga in selected)
    wins = sum(gf > ga for _, _, gf, ga in selected)
    losses = n - draws - wins
    streak = 0
    for _, _, gf, ga in selected:
        if gf != ga:
            break
        streak += 1
    rate = lambda value: value / n if n else None
    return {"n": n, "draws": draws, "wins": wins, "losses": losses,
            "drawRate": rate(draws), "winRate": rate(wins), "lossRate": rate(losses),
            "goalsFor": rate(sum(x[2] for x in selected)), "goalsAgainst": rate(sum(x[3] for x in selected)),
            "totalGoals": rate(sum(x[2] + x[3] for x in selected)), "goalDifference": rate(sum(x[2] - x[3] for x in selected)),
            "under25Rate": rate(sum(x[2] + x[3] <= 2 for x in selected)), "zeroZeroRate": rate(sum(x[2] == x[3] == 0 for x in selected)),
            "oneOneRate": rate(sum(x[2] == x[3] == 1 for x in selected)), "cleanSheetRate": rate(sum(x[3] == 0 for x in selected)),
            "scorelessRate": rate(sum(x[2] == 0 for x in selected)), "drawStreak": streak}


def market_outcomes(market):
    market = mapping(market)
    rows = market.get("bookmakerByOutcome") or []
    if market.get("oddsAvailable") is not True or not isinstance(rows, list):
        return None
    result = []
    for outcome in OUTCOMES:
        selected = [mapping(r) for r in rows if mapping(r).get("outcome") == outcome]
        if len(selected) != 1 or selected[0].get("available") is not True or scalar(selected[0].get("odds"), True) is None:
            return None
        result.append(selected[0])
    return result


def _extract(row, collect=False):
    values, metadata = [], []

    def emit(name, label, value, family, source, unit="index", view="market", requires=()):
        values.append(float(value) if number(value) else np.nan)
        if collect:
            metadata.append({"name": name, "label": label, "family": family, "source": source, "unit": unit,
                             "requires": list(requires) or [source], "view": view})

    frontend, inputs = mapping(row.get("frontend")), mapping(row.get("analysisInputs"))
    availability = mapping(inputs.get("availability"))
    quotes = {q.get("key"): q for q in row.get("companyQuotes", []) if isinstance(q, dict)} if isinstance(row.get("companyQuotes"), list) else {}
    prices, probabilities, margins = {}, {}, {}
    for company in COMPANIES:
        for phase in ("opening", "latest"):
            source = f"companyQuotes[{company}].euroOdds.{phase}"
            if company == "average":
                source = "analysisInputs.average" + ("Opening" if phase == "opening" else "Latest")
                input_key = "averageOpening" if phase == "opening" else "averageLatest"
                raw = inputs.get(input_key)
                if input_key not in inputs:
                    raw = at(row, phase + ".average")
            else:
                euro = mapping(at(quotes.get(company), "euroOdds"))
                raw = euro.get(phase) if euro.get("source") not in (None, "", "missing") else None
                # Existing strict phase export supplies exactly these companies.
                if not euro and company != "bet365":
                    raw = at(row, phase + "." + company)
            p = triple(raw, True)
            prices[company, phase] = p
            inverse = tuple(1 / v for v in p) if p else None
            total = sum(inverse) if inverse else None
            normalized = tuple(v / total for v in inverse) if inverse else None
            probabilities[company, phase], margins[company, phase] = normalized, total - 1 if total else None
            prefix = f"euro.{company}.{phase}"
            title = SOURCE_CN[company] + ("初盘" if phase == "opening" else "即时")
            for i, outcome in enumerate(OUTCOMES):
                emit(prefix + ".odds." + outcome, title + CN[outcome] + "赔率", p[i] if p else None, "euroOdds", source, "decimal_odds")
                emit(prefix + ".prob." + outcome, title + CN[outcome] + "去水概率", normalized[i] if normalized else None, "euroOdds", source, "probability")
            emit(prefix + ".overround", title + "超额概率", total - 1 if total else None, "euroOdds", source, "probability")
            emit(prefix + ".entropy", title + "三项概率熵", -sum(v * math.log(v) for v in normalized) if normalized else None, "euroOdds", source)
            emit(prefix + ".sideGap", title + "主客概率差绝对值", abs(normalized[0] - normalized[2]) if normalized else None, "euroOdds", source, "probability")
            emit(prefix + ".drawVsStrongSide", title + "平概率减较强胜负概率", normalized[1] - max(normalized[0], normalized[2]) if normalized else None, "euroOdds", source, "probability")
        op, la = prices[company, "opening"], prices[company, "latest"]
        po, pl = probabilities[company, "opening"], probabilities[company, "latest"]
        for i, outcome in enumerate(OUTCOMES):
            src = f"{company} same-company complete opening/latest euro triples"
            for suffix, label, value, unit in (
                ("oddsChange", "赔率变化", la[i] - op[i] if op and la else None, "odds_difference"),
                ("relativeChange", "赔率相对变化", la[i] / op[i] - 1 if op and la else None, "ratio"),
                ("probChange", "去水概率变化", pl[i] - po[i] if po and pl else None, "probability"),
            ):
                emit(f"euro.{company}.change.{suffix}.{outcome}", SOURCE_CN[company] + CN[outcome] + label, value, "euroMovement", src, unit)
        emit(f"euro.{company}.change.overround", SOURCE_CN[company] + "超额概率变化", margins[company, "latest"] - margins[company, "opening"] if op and la else None, "euroMovement", src, "probability")
    for phase in ("opening", "latest"):
        w = prices["william", phase]
        for comparison in ("sporttery", "average"):
            c = prices[comparison, phase]
            diff = tuple(a-b for a,b in zip(w,c)) if w and c else None
            absolute = tuple(abs(v) for v in diff) if diff else None
            source = f"{phase} William - {comparison}; raw complete same-phase prices"
            prefix = f"difference.{phase}.{comparison}"
            for i, outcome in enumerate(OUTCOMES):
                emit(prefix + "." + outcome, phase + "威廉减" + SOURCE_CN[comparison] + CN[outcome], diff[i] if diff else None, "priceDifference", source, "odds_difference")
            largest = bool(absolute and absolute[1] >= max(absolute)-EPS)
            top_two = bool(absolute and absolute[1] >= sorted(absolute)[1]-EPS)
            rank = 1 + sum(v > absolute[1]+EPS for v in absolute) if absolute else None
            emit(prefix + ".drawAbsRank", phase + SOURCE_CN[comparison] + "平差绝对值降序名次", rank, "priceDifference", source, "rank")
            emit(prefix + ".drawAbsMaximum", phase + SOURCE_CN[comparison] + "平差绝对值最大", int(largest) if diff else None, "priceDifference", source, "indicator")
            emit(prefix + ".negativeTopTwo", phase + SOURCE_CN[comparison] + "优先分类负数含平", int(not largest and top_two and diff[1] < -EPS) if diff else None, "priceDifference", source, "indicator")
            emit(prefix + ".positiveTopTwo", phase + SOURCE_CN[comparison] + "优先分类正数含平", int(not largest and top_two and diff[1] > EPS) if diff else None, "priceDifference", source, "indicator")
    kelly_values, kelly_sources = {}, {}
    stages = frontend.get("comparisonStages") or []
    for phase in ("opening", "latest"):
        candidates = [mapping(v) for v in stages if mapping(v).get("key") == phase] if isinstance(stages, list) else []
        metric = mapping(candidates[0].get("kelly")) if len(candidates) == 1 else {}
        raw = metric.get("values") if metric.get("available") is True else at(row, phase + ".kelly") if not metric else None
        k = triple(raw, True)
        kelly_values[phase] = k
        kelly_sources[phase] = kelly_source_id(metric.get("basis"), at(row, phase + ".kellySource"))
        for i, outcome in enumerate(OUTCOMES):
            source = f"frontend.comparisonStages[{phase}].kelly; available=true"
            emit(f"kelly.{phase}.{outcome}", phase + "凯利值" + CN[outcome], k[i] if k else None, "kelly", source)
            emit(f"kelly.{phase}.minimum.{outcome}", phase + "凯利最小项含" + CN[outcome], int(k[i] <= min(k)+EPS) if k else None, "kelly", source, "indicator")
        emit(f"kelly.{phase}.spread", phase + "凯利极差", max(k)-min(k) if k else None, "kelly", source)
    for i, outcome in enumerate(OUTCOMES):
        a, b = kelly_values["opening"], kelly_values["latest"]
        same_source = kelly_sources["opening"] is not None and kelly_sources["opening"] == kelly_sources["latest"]
        emit("kelly.change." + outcome, "凯利初即变化" + CN[outcome], b[i]-a[i] if a and b and same_source else None, "kelly", "same explicit company-ID initial/latest Kelly; unknown or differing sources are missing", "index_difference")
    parsed_lines = {}
    for company in ("william", "bet365", "selected"):
        for market in ("asianHandicap", "goalsLine"):
            quote = mapping(quotes.get(company)) if company != "selected" else {}
            block = mapping(quote.get(market)) if company != "selected" else mapping(row.get(market))
            # The export carries selected lines inside observation when provided.
            if company == "selected" and not block:
                block = mapping(at(row, "observation." + market))
            if company == "selected" and not block:
                block = {"source": "strict_export", "opening": row.get("asianOpening") if market == "asianHandicap" else at(row, "goalsHeat.openingLine"),
                         "latest": row.get("asianLine") if market == "asianHandicap" else at(row, "goalsHeat.latestLine")}
            valid_source = block.get("source") not in (None, "", "missing", "history_moneys")
            components = {}
            for phase in ("opening", "latest"):
                line = line_value(block.get(phase), market == "goalsLine") if valid_source else None
                water = block.get(phase + "Odds")
                water = [scalar(v, True) for v in water] if isinstance(water, list) and len(water) == 2 and valid_source else [None, None]
                components[phase] = (line, *water)
                parsed_lines[company, market, phase] = line
                prefix = f"line.{company}.{market}.{phase}"
                src = f"companyQuotes[{company}].{market}.{phase}" if company != "selected" else "strict selected source: asianOpening/asianLine or goalsHeat.openingLine/latestLine; water only when separately stored"
                for key, label, value, unit in (("line", "盘口", line, "goals"), ("water1", "主/大水位", water[0], "water"), ("water2", "客/小水位", water[1], "water"),
                                                ("waterSpread", "两侧水位差", water[0]-water[1] if all(v is not None for v in water) else None, "water")):
                    emit(prefix + "." + key, SOURCE_CN[company] + market + phase + label, value, "companyLines", src, unit)
            for i, key in enumerate(("line", "water1", "water2")):
                a, b = components["opening"][i], components["latest"][i]
                emit(f"line.{company}.{market}.change.{key}", SOURCE_CN[company] + market + "初即变化" + key, b-a if a is not None and b is not None else None, "companyLines", f"{company}.{market} same-company/side opening and latest", "goals" if i == 0 else "water")
    # Safe local-history rows are the sole source of these statistics.
    recent_stats = {}
    for side, key in (("home", "homeRecent"), ("away", "awayRecent")):
        records = safe_records(row, key)
        for window in (5, 10):
            stats = history_stats(records, window)
            recent_stats[side, window] = stats
            for name, label, unit in HISTORY_METRICS:
                emit(f"safeRecent.{side}.{window}.{name}", ("主队" if side == "home" else "客队") + f"赛前{window}场" + label, stats[name], "safeRecent", "preMatchHistory." + key, unit, "safeHistory", ("preMatchHistory." + key, "past kickoff + 2 hours < target; target ID excluded"))
    h2h = safe_records(row, "headToHead")
    h2h_metrics = [entry for entry in HISTORY_METRICS if entry[0] not in ("wins", "losses", "drawStreak")]
    for window in (10, 0):
        stats = history_stats(h2h, window)
        for name, label, unit in h2h_metrics:
            emit(f"safeH2H.{window or 'all'}.{name}", f"赛前交锋{window or '全部'}" + label, stats[name], "safeH2H", "preMatchHistory.headToHead", unit, "safeHistory")
    for name in ("drawRate", "goalsFor", "goalsAgainst", "totalGoals", "goalDifference"):
        a, b = recent_stats["home", 10][name], recent_stats["away", 10][name]
        for operation in ("sum", "absGap"):
            value = a+b if operation == "sum" and a is not None and b is not None else abs(a-b) if a is not None and b is not None else None
            emit(f"safeRecent.pair.{name}.{operation}", "双方赛前近10场" + name + operation, value, "safeRecent", "preMatchHistory home/away 10", "probability" if "Rate" in name else "goals", "safeHistory")
    simulations = mapping(frontend.get("roiSimulation") or row.get("roiSimulation"))
    raw_markets = simulations.get("markets") or []
    raw_markets = [mapping(m) for m in raw_markets] if isinstance(raw_markets, list) else []
    groups = {
        "sporttery": [m for m in raw_markets if m.get("key") == "sporttery" and m.get("simulated") is not True],
        "simulated": [m for m in raw_markets if m.get("key") in ("sporttery", "sportterySim") and m.get("simulated") is True],
        "william": [m for m in raw_markets if m.get("key") == "william" and m.get("simulated") is not True],
        "bet365": [m for m in raw_markets if m.get("key") == "bet365" and m.get("simulated") is not True],
        "local": [mapping(frontend.get("localMarket"))] if frontend.get("localMarket") else [],
    }
    distribution_sources = mapping(inputs.get("distributionSources"))
    cold_sources = mapping(inputs.get("hotColdSources"))
    trade = mapping(inputs.get("tradeFields"))
    for company, candidates in groups.items():
        market = candidates[0] if len(candidates) == 1 else {}
        outcomes = market_outcomes(market)
        roi = []
        for i, outcome in enumerate(OUTCOMES):
            detail = outcomes[i] if outcomes else {}
            amount, base = scalar(detail.get("bookmakerProfit")), scalar(detail.get("totalStake"), True)
            value = amount / base * 100 if amount is not None and base is not None else None
            roi.append(value)
            source = f"frontend.roiSimulation[{company}] available outcome; local amount/base"
            emit(f"profit.{company}.roi.{outcome}", SOURCE_CN[company] + CN[outcome] + "庄家测算ROI", value, "snapshotProfit", source, "percent", "snapshotDerived")
            emit(f"profit.{company}.share.{outcome}", SOURCE_CN[company] + CN[outcome] + "测算资金比例", scalar(detail.get("retailShare")), "snapshotProfit", source, "percent", "snapshotDerived")
            source_list = cold_sources.get(market.get("key"))
            real_cold = isinstance(source_list, list) and len(source_list) == 3 and source_list[i] in ("official", "local_simulated")
            emit(f"profit.{company}.hotCold.{outcome}", SOURCE_CN[company] + CN[outcome] + "冷热值", scalar(detail.get("hotColdIndex")) if real_cold else None, "snapshotProfit", source, "index", "snapshotDerived")
            if company in ("sporttery", "william", "bet365"):
                distribution = distribution_sources.get(market.get("key"))
                support = triple([trade.get(str(distribution) + "." + k) for k in ("hsupportRate", "dsupportRate", "asupportRate")])
                source_ok = distribution in ("tzbl", "jyykSpf") and support is not None and all(0 <= v <= 100 for v in support) and sum(support) > 0
                emit(f"marketProfit.{company}.roi.{outcome}", SOURCE_CN[company] + CN[outcome] + "真实支持率口径测算ROI", value if source_ok else None, "marketProfit", "same quoted odds x source support distribution; not company-specific observed funds", "percent")
        complete = all(v is not None for v in roi)
        for key, value, label in (("drawMinimum", int(roi[1] <= min(roi)+EPS) if complete else None, "平项测算利润最低"),
                                   ("spread", max(roi)-min(roi) if complete else None, "测算利润极差"),
                                   ("psychologyError", scalar(trade.get("tzbl.psyError")) if company == "sporttery" and outcomes else None, "散户心理误差")):
            emit(f"profit.{company}.{key}", SOURCE_CN[company] + label, value, "snapshotProfit", f"frontend.roiSimulation[{company}]", "indicator" if key == "drawMinimum" else "index", "snapshotDerived")
    trade_paths = [f"tzbl.{key}" for key in ("hsupportRate", "dsupportRate", "asupportRate", "hprobability", "dprobability", "aprobability", "herror", "derror", "aerror", "psyError")]
    trade_paths += [f"jyykSpf.{key}" for key in ("hsupportRate", "dsupportRate", "asupportRate", "hy", "dy", "ay", "hlr", "dlr", "alr")]
    for path in trade_paths:
        # The export uses the exact source field casing; no case-insensitive
        # lookup can accidentally revive a missing parser-default numeric zero.
        emit("trade." + path, "源站原始" + path, scalar(trade.get(path)), "sourceTrade", "analysisInputs.tradeFields[" + path + "] (raw numeric presence verified)", "percent" if "Rate" in path or "probability" in path or path.endswith(("hy", "dy", "ay")) else "index")
    # These are intentionally not the safe-history features above.
    heat = scalar(row.get("homeHeat"))
    emit("heat.asianHome", "亚盘主队计算热度", heat if heat is not None and 0 <= heat <= 100 else None, "heat", "homeHeat (strict raw average/Asian input availability)", "percent")
    goals = mapping(row.get("goalsHeat"))
    for side in ("over", "under"):
        value = scalar(goals.get(side)) if goals.get("available") is True else None
        emit("heat.goals." + side, "大小球计算热度" + side, value, "heat", "goalsHeat.available=true", "percent", "snapshotDerived")
    retail = frontend.get("sanhuxinli")
    retail = triple([percentage(v) for v in retail[:3]]) if isinstance(retail, list) and len(retail) >= 3 else None
    if availability.get("averageLatest") is not True:
        retail = None
    for i, outcome in enumerate(OUTCOMES):
        emit("retail." + outcome, "前台散户心理" + CN[outcome], retail[i] if retail else None, "snapshotRetail", "frontend.sanhuxinli first three", "percent", "snapshotDerived")
    for key in ("historyGoalAverage", "recentGoalAverage", "combinedGoalAverage", "combinedHandicapAverage"):
        has_history = availability.get("historySummary") is True
        has_recent = availability.get("recentHome") is True or availability.get("recentGuest") is True
        valid_inputs = has_history if key == "historyGoalAverage" else has_recent if key == "recentGoalAverage" else has_history or has_recent
        emit("snapshot." + key, "前台" + key, scalar(inputs.get(key)) if valid_inputs else None, "snapshotHistory", "analysisInputs." + key, "goals", "snapshotDerived")
    for key in ("recentTotalGoals", "recentHomeMatches", "recentGuestMatches"):
        emit("snapshot.goalsInput." + key, "原近期大小球输入" + key, scalar(at(goals, "inputs." + key)), "snapshotHistory", "goalsHeat.inputs." + key, "count" if key.endswith("Matches") else "goals", "snapshotDerived")
    platform = mapping(inputs.get("platform"))
    platform_inputs = (availability.get("averageLatest") is True and
                       parsed_lines.get(("selected", "asianHandicap", "latest")) is not None and
                       parsed_lines.get(("selected", "goalsLine", "latest")) is not None)
    for path in ("professionalConsensus", "sportteryComfort", "bookmaker.outcome", "platform.outcome", "warningAdjusted.outcome", "evilCult.prediction.outcome"):
        raw = at(platform, path)
        valid = raw in ("", "home", "draw", "away") and bool(platform) and platform_inputs
        if path == "professionalConsensus":
            valid = valid and availability.get("strictProfessionalInputs") is True
        elif path == "sportteryComfort":
            valid = valid and market_outcomes(groups["sporttery"][0] if len(groups["sporttery"]) == 1 else {}) is not None
        for outcome in OUTCOMES:
            emit("professional." + path + "." + outcome, "前台专业方向" + path + CN[outcome], int(raw == outcome) if valid else None, "professional", "analysisInputs.platform." + path, "indicator", "snapshotDerived")
    for path in ("drawRisk.score", "goals.main.home", "goals.main.guest", "evilCult.scores.overPercent", "evilCult.scores.underPercent"):
        emit("professional." + path, "前台专业数值" + path, scalar(at(platform, path)) if platform_inputs else None, "professional", "analysisInputs.platform." + path, "goals" if path.startswith("goals.") else "index", "snapshotDerived")
    for key in ("averageLatest", "averageOpening", "historySummary", "recentHome", "recentGuest", "strictKelly", "strictTicai", "strictProfessionalInputs", "historyRecentTimingClean"):
        raw = availability.get(key)
        emit("availability." + key, "原输入可用性" + key, int(raw) if isinstance(raw, bool) else None, "inputAvailability", "analysisInputs.availability." + key, "indicator", "snapshotDerived" if key in ("historySummary", "recentHome", "recentGuest", "historyRecentTimingClean", "strictProfessionalInputs") else "market")
    return values, metadata


def build_features(rows):
    """Return rows x fixed numeric whitelist and column-aligned metadata.

    Never inspect row.result/target scores/status/IDs as predictive numbers.
    IDs/time are used only to exclude unsafe *historical input* records.
    """
    _, metadata = _extract({}, True)
    values = [_extract(mapping(row))[0] for row in rows]
    array = np.asarray(values, dtype=np.float64).reshape((len(values), len(metadata)))
    assert len({entry["name"] for entry in metadata}) == len(metadata)
    assert np.isfinite(array[~np.isnan(array)]).all()
    return array, metadata


def self_test():
    base = {"matchId": "target", "matchTime": "2026-10-24T12:00:00+08:00", "result": 1, "homeScore": 0, "awayScore": 0,
            "opening": {"william": [2, 3, 4], "sporttery": [2, 3, 4], "average": [2, 3, 4], "kelly": [0.9, 0.9, 1]},
            "latest": {"william": [2, 3, 4], "sporttery": [3, 1, 3], "average": [2, 3, 4]},
            "preMatchHistory": {key: {"matches": []} for key in ("homeRecent", "awayRecent", "headToHead")}}
    base["preMatchHistory"]["homeRecent"]["matches"] = [
        {"matchId": "past", "matchTime": "2026-10-20T12:00:00+08:00", "homeScore": 1, "awayScore": 1, "venue": "home"},
        {"matchId": "target", "matchTime": "2026-10-20T12:00:00+08:00", "homeScore": 8, "awayScore": 0, "venue": "home"},
        {"matchId": "future", "matchTime": "2026-10-25T12:00:00+08:00", "homeScore": 8, "awayScore": 0, "venue": "home"},
        {"matchId": "boundary", "matchTime": "2026-10-24T10:00:00+08:00", "homeScore": 8, "awayScore": 0, "venue": "home"},
    ]
    changed = copy.deepcopy(base)
    changed.update(result=0, homeScore=9, awayScore=1, status=5, displayState="完场", predictionHit=True,
                   accuracyFit={"score": 999}, myAngle={"accuracy": 1}, goddessWoman={"probabilities": {"draw": 1}})
    a, manifest = build_features([base, changed, {}])
    assert np.array_equal(a[0], a[1], equal_nan=True)
    names = {entry["name"]: index for index, entry in enumerate(manifest)}
    value = lambda name: a[0, names[name]]
    assert value("safeRecent.home.10.n") == 1 and value("safeRecent.home.10.drawRate") == 1
    assert value("safeRecent.away.10.n") == 0 and np.isnan(value("safeRecent.away.10.drawRate"))
    assert value("difference.opening.sporttery.drawAbsMaximum") == 1
    assert value("difference.opening.sporttery.negativeTopTwo") == 0
    assert value("kelly.opening.minimum.home") == value("kelly.opening.minimum.draw") == 1
    assert np.isnan(a[2]).all()
    assert line_value("平手") == 0 and line_value("受让半球/一球") == -.75 and line_value("2.5/3", True) == 2.75
    assert line_value(None) is None and line_value("暂无") is None and line_value(0, True) is None
    market = {"key": "sporttery", "oddsAvailable": True, "simulated": False,
              "bookmakerByOutcome": [{"outcome": outcome, "available": True, "odds": odds, "bookmakerProfit": value, "totalStake": 1000, "retailShare": share}
                                      for outcome, value, share, odds in zip(OUTCOMES, (0, -250, 500), (50, 25, 25), (2, 5, 2))]}
    supported = {"frontend": {"roiSimulation": {"markets": [market]}},
                 "analysisInputs": {"distributionSources": {"sporttery": "jyykSpf"},
                                    "tradeFields": {"jyykSpf.hsupportRate": 50, "jyykSpf.dsupportRate": 25, "jyykSpf.asupportRate": 25}}}
    missing_support = copy.deepcopy(supported)
    missing_support["analysisInputs"]["tradeFields"]["jyykSpf.dsupportRate"] = None
    unavailable = copy.deepcopy(supported)
    unavailable["frontend"]["roiSimulation"]["markets"][0]["bookmakerByOutcome"][1]["available"] = False
    b, _ = build_features([supported, missing_support, unavailable])
    assert b[0, names["marketProfit.sporttery.roi.home"]] == 0
    assert b[0, names["profit.sporttery.roi.draw"]] == -25
    assert np.isnan(b[1, names["marketProfit.sporttery.roi.draw"]])
    assert np.isnan(b[2, names["profit.sporttery.roi.draw"]])
    # Explicit absence cannot be repaired from an older compatible phase field.
    absent = {**base, "analysisInputs": {"averageOpening": None},
              "companyQuotes": [{"key": "william", "euroOdds": {"source": "missing", "opening": [2, 3, 4]}}]}
    c, _ = build_features([absent])
    assert np.isnan(c[0, names["euro.average.opening.odds.draw"]])
    assert np.isnan(c[0, names["euro.william.opening.odds.draw"]])
    kelly_same = {"opening": {"kelly": [0.9, 0.8, 1.0], "kellySource": "Company（欧赔 ID 16）"},
                  "latest": {"kelly": [0.8, 0.9, 1.1], "kellySource": "Company（欧赔 ID 16）"}}
    kelly_different = copy.deepcopy(kelly_same)
    kelly_different["latest"]["kellySource"] = "Bet365（欧赔 ID 281）"
    kelly_unknown = copy.deepcopy(kelly_same)
    kelly_unknown["latest"]["kellySource"] = "未知公司"
    k, _ = build_features([kelly_same, kelly_different, kelly_unknown])
    assert abs(k[0, names["kelly.change.draw"]] - .1) < 1e-12
    assert np.isnan(k[1, names["kelly.change.draw"]]) and np.isnan(k[2, names["kelly.change.draw"]])
    assert all(entry["view"] in ("market", "safeHistory", "snapshotDerived") for entry in manifest)
    assert 300 <= len(manifest) <= 500
    return {"features": len(manifest), "targetMutationInvariant": True, "unsafeHistoryRejected": True, "zeroAndTieEdges": True,
            "missingRemainNaN": True, "profitAvailabilityAndSupportPresence": True, "explicitMissingNotBackfilled": True,
            "kellyMovementRequiresSameKnownCompany": True,
            "views": {view: sum(x["view"] == view for x in manifest) for view in ("market", "safeHistory", "snapshotDerived")}}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--dataset", type=Path)
    args = parser.parse_args()
    print(json.dumps(self_test(), ensure_ascii=False))
    if args.dataset:
        raw = json.loads(args.dataset.read_text(encoding="utf-8-sig"))
        x, manifest = build_features(raw["rows"])
        print(json.dumps({"shape": list(x.shape), "nonMissingColumns": int(np.any(~np.isnan(x), axis=0).sum()), "finiteValues": int(np.isfinite(x).sum())}))
