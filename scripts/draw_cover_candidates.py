#!/usr/bin/env python3
"""Fixed business hypotheses for a high-recall OR rule study.

Only the frozen feature manifest is consumed; no match rows or labels are read.
Numeric thresholds and cross-family templates are declared below rather than
estimated from outcomes. Missing/non-finite inputs must never match a predicate.
The API supplies hypotheses, not claims that any selected rule predicts draws.
"""
from __future__ import annotations

import argparse
import hashlib
import json
from collections import Counter, defaultdict
from pathlib import Path


def _identifier(prefix, value):
    canonical = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
    return prefix + hashlib.sha256(canonical.encode("utf-8")).hexdigest()[:16]


def build_candidates(manifest):
    """Return stable ``atoms``, 1/2-atom ``rules`` and documented exclusions.

    Evaluator contract: check finite scalar input, apply abs only when specified,
    then ge/le/eq; AND every finite guard; AND the one or two referenced atoms.
    Indicators are matched against 0/1 only when their source scalar is finite.
    Guards express minimum source-history sample sizes, never rule endpoints.
    """
    if not isinstance(manifest, list) or any(not isinstance(item, dict) or not isinstance(item.get("name"), str) for item in manifest):
        raise ValueError("manifest must be a list of named feature metadata")
    columns = {item["name"]: item for item in manifest}
    if len(columns) != len(manifest):
        raise ValueError("duplicate manifest feature names")
    atoms, rules, excluded = [], [], []
    atom_by_id, rule_by_id, buckets = {}, {}, defaultdict(list)
    singles = []

    def add(bucket, feature, op, value, label, *, transform="identity", guards=(), single=False):
        required = [feature] + [guard["feature"] for guard in guards]
        missing = [name for name in required if name not in columns]
        if missing:
            excluded.append({"feature": feature, "reason": "missing frozen manifest columns", "missing": missing})
            return
        guard_values = [dict(guard) for guard in guards]
        core = {"feature": feature, "op": op, "value": value, "transform": transform, "guards": guard_values}
        atom_id = _identifier("a_", core)
        metadata = columns[feature]
        atom = {"id": atom_id, **core, "label": label, "family": metadata.get("family"),
                "view": metadata.get("view"), "source": metadata.get("source")}
        if atom_id not in atom_by_id:
            atom_by_id[atom_id] = atom
            atoms.append(atom)
        if atom_id not in buckets[bucket]:
            buckets[bucket].append(atom_id)
        if single and atom_id not in singles:
            singles.append(atom_id)

    def guard(feature, value):
        return {"feature": feature, "op": "ge", "value": value}

    def rule(ids, mechanism):
        ids = sorted(set(ids))
        if len(ids) not in (1, 2):
            return
        if len(ids) == 2:
            a, b = (atom_by_id[ident] for ident in ids)
            # Two thresholds on one underlying scalar are not an independent
            # two-concept hypothesis and can conceal tautological intervals.
            if (a["feature"], a["transform"]) == (b["feature"], b["transform"]):
                return
        rule_id = _identifier("r_", ids)
        if rule_id in rule_by_id:
            return
        item = {"id": rule_id, "allOf": ids,
                "label": " 且 ".join(atom_by_id[ident]["label"] for ident in ids),
                "mechanism": mechanism}
        rule_by_id[rule_id] = item
        rules.append(item)

    def phase(atom_id):
        text = atom_by_id[atom_id]["feature"]
        return "opening" if ".opening." in text else "latest" if ".latest." in text else None

    def pairs(left, right, mechanism, *, same_phase=False, predicate=None):
        for a in buckets[left]:
            for b in buckets[right]:
                if a == b or same_phase and phase(a) != phase(b):
                    continue
                if predicate is not None and not predicate(a, b):
                    continue
                rule([a, b], mechanism)

    # User-defined initial maximum, latest maximum/negative-top-two/positive-
    # top-two states. Ties and zero semantics come from the frozen extractor.
    for source, title in (("sporttery", "威廉−竞彩"), ("average", "威廉−均赔")):
        add("differenceSignal", f"difference.opening.{source}.drawAbsMaximum", "eq", 1,
            f"初盘{title}平差绝对值最大（含并列）", single=True)
        for key, text in (("drawAbsMaximum", "平差绝对值最大（含并列）"),
                          ("negativeTopTwo", "非最大、负数且绝对值前二含平"),
                          ("positiveTopTwo", "非最大、正数且绝对值前二含平")):
            add("differenceSignal", f"difference.latest.{source}.{key}", "eq", 1,
                f"即时{title}{text}", single=True)
        add("differenceTopTwo", f"difference.latest.{source}.drawAbsRank", "le", 2,
            f"即时{title}平差绝对值排前二", single=True)
        for current, label in (("opening", "初盘"), ("latest", "即时")):
            name = f"difference.{current}.{source}.draw"
            for sign, op, threshold in (("正", "ge", 1e-9), ("负", "le", -1e-9)):
                add("differenceSign", name, op, threshold, f"{label}{title}平差为{sign}")
            for threshold in (.10, .30):
                add("differenceMagnitude", name, "ge", threshold, f"{label}{title}平差绝对值≥{threshold:g}", transform="abs")

    for current, label in (("opening", "初盘"), ("latest", "即时")):
        for threshold in (.20, .24, .27, .30):
            add("drawProbability", f"euro.average.{current}.prob.draw", "ge", threshold,
                f"均赔{label}平去水概率≥{threshold:.0%}", single=True)
        for threshold in (.10, .20, .35, .50):
            add("balanceGap", f"euro.average.{current}.sideGap", "le", threshold,
                f"均赔{label}主客概率差≤{threshold:.0%}", single=threshold <= .20)
        for threshold in (.95, 1.0):
            add("entropy", f"euro.average.{current}.entropy", "ge", threshold,
                f"均赔{label}三项概率熵≥{threshold:g}", single=True)
        for side, title in (("home", "主胜"), ("away", "客胜")):
            for threshold in (.55, .65):
                add("favorite", f"euro.average.{current}.prob.{side}", "ge", threshold,
                    f"均赔{label}{title}去水概率≥{threshold:.0%}")
        for threshold in (3.0, 3.5, 4.0):
            add("drawOdds", f"euro.average.{current}.odds.draw", "le", threshold,
                f"均赔{label}平赔≤{threshold:g}", single=threshold <= 3.5)
    for op, values in (("ge", (.01, .03)), ("le", (-.01, -.03))):
        for threshold in values:
            add("drawMovement", "euro.average.change.probChange.draw", op, threshold,
                f"均赔初即平概率变化{'≥' if op=='ge' else '≤'}{threshold:+.0%}")

    for current, label in (("opening", "初盘"), ("latest", "即时")):
        for threshold in (.25, .5, .75):
            add("shallowAsian", f"line.selected.asianHandicap.{current}.line", "le", threshold,
                f"实际亚盘{label}绝对值≤{threshold:g}", transform="abs", single=threshold <= .5)
        for threshold in (1.0, 1.5):
            add("deepAsian", f"line.selected.asianHandicap.{current}.line", "ge", threshold,
                f"实际亚盘{label}绝对值≥{threshold:g}", transform="abs")
        for threshold in (2.25, 2.5, 2.75):
            add("lowGoals", f"line.selected.goalsLine.{current}.line", "le", threshold,
                f"实际大小球{label}≤{threshold:g}", single=threshold <= 2.5)
        for threshold in (2.75, 3.0, 3.25):
            add("highGoals", f"line.selected.goalsLine.{current}.line", "ge", threshold,
                f"实际大小球{label}≥{threshold:g}")
    for op, values in (("ge", (.25, .5)), ("le", (-.25, -.5))):
        for threshold in values:
            add("goalsMovement", "line.selected.goalsLine.change.line", op, threshold,
                f"大小球初即变化{'≥' if op=='ge' else '≤'}{threshold:+g}")
    for threshold in (.25, .5):
        add("asianMovement", "line.selected.asianHandicap.change.line", "ge", threshold,
            f"亚盘初即变动绝对值≥{threshold:g}", transform="abs")

    for side, label in (("home", "主队"), ("away", "客队")):
        required = [guard(f"safeRecent.{side}.10.n", 5)]
        for threshold in (.2, .3, .4):
            add("recentDraw", f"safeRecent.{side}.10.drawRate", "ge", threshold,
                f"{label}赛前近10平率≥{threshold:.0%}（样本≥5）", guards=required, single=True)
        for threshold in (.5, .7):
            add("recentLowGoals", f"safeRecent.{side}.10.under25Rate", "ge", threshold,
                f"{label}近10总球≤2球比例≥{threshold:.0%}（样本≥5）", guards=required)
        add("recentLowGoals", f"safeRecent.{side}.10.totalGoals", "le", 2.5,
            f"{label}近10场均总球≤2.5（样本≥5）", guards=required)
        for threshold in (1.0, 1.5, 2.0):
            add("recentScoring", f"safeRecent.{side}.10.goalsFor", "ge", threshold,
                f"{label}近10场均进球≥{threshold:g}（样本≥5）", guards=required)
            add("recentConceding", f"safeRecent.{side}.10.goalsAgainst", "ge", threshold,
                f"{label}近10场均失球≥{threshold:g}（样本≥5）", guards=required)
        for threshold in (3.0, 3.5):
            add("recentHighGoals", f"safeRecent.{side}.10.totalGoals", "ge", threshold,
                f"{label}近10场均总球≥{threshold:g}（样本≥5）", guards=required)
        for threshold in (.1, .2):
            add("recentZero", f"safeRecent.{side}.10.zeroZeroRate", "ge", threshold,
                f"{label}近10零比零比例≥{threshold:.0%}（样本≥5）", guards=required)
        for threshold in (1, 2):
            add("recentStreak", f"safeRecent.{side}.10.drawStreak", "ge", threshold,
                f"{label}最近连续平局≥{threshold}场（样本≥5）", guards=required, single=True)
    h2h_guard = [guard("safeH2H.all.n", 3)]
    for threshold in (.2, .3, .4):
        add("h2hDraw", "safeH2H.all.drawRate", "ge", threshold,
            f"赛前交锋平率≥{threshold:.0%}（样本≥3）", guards=h2h_guard, single=True)
    add("h2hLowGoals", "safeH2H.all.totalGoals", "le", 2.5, "赛前交锋场均总球≤2.5（样本≥3）", guards=h2h_guard)
    for threshold in (3.0, 3.5):
        add("h2hHighGoals", "safeH2H.all.totalGoals", "ge", threshold,
            f"赛前交锋场均总球≥{threshold:g}（样本≥3）", guards=h2h_guard)
    for threshold in (.1, .2):
        add("h2hZero", "safeH2H.all.zeroZeroRate", "ge", threshold,
            f"赛前交锋零比零比例≥{threshold:.0%}（样本≥3）", guards=h2h_guard)
    both_guards = [guard("safeRecent.home.10.n", 5), guard("safeRecent.away.10.n", 5)]
    for bucket, name, op, thresholds, label in (
        ("pairDraw", "drawRate.sum", "ge", (.4, .6, .8), "双方近10平率之和"),
        ("pairTotalHigh", "totalGoals.sum", "ge", (5.0, 6.0, 7.0), "双方近10场均总球之和"),
        ("pairTotalGap", "totalGoals.absGap", "le", (.5, 1.0), "双方近10场均总球绝对差"),
        ("pairScoring", "goalsFor.sum", "ge", (2.0, 3.0, 4.0), "双方近10场均进球之和"),
        ("pairConceding", "goalsAgainst.sum", "ge", (2.0, 3.0, 4.0), "双方近10场均失球之和"),
        ("pairBalance", "goalDifference.absGap", "le", (.5, 1.0), "双方近10场均净胜球绝对差"),
    ):
        for threshold in thresholds:
            add(bucket, "safeRecent.pair." + name, op, threshold,
                f"{label}{'≥' if op=='ge' else '≤'}{threshold:g}（双方样本≥5）", guards=both_guards)

    for current, label in (("opening", "初盘"), ("latest", "即时")):
        for threshold in (.9, .95, 1.0):
            add("kellyDraw", f"kelly.{current}.draw", "le", threshold, f"{label}凯利平值≤{threshold:g}")
        add("kellyMinimum", f"kelly.{current}.minimum.draw", "eq", 1, f"{label}凯利最小项含平", single=True)
        add("kellyAvoidHome", f"kelly.{current}.minimum.home", "eq", 0, f"{label}凯利最小项不含胜")
        add("kellyAvoidAway", f"kelly.{current}.minimum.away", "eq", 0, f"{label}凯利最小项不含负")
        for threshold in (.02, .05):
            add("kellyFlat", f"kelly.{current}.spread", "le", threshold, f"{label}凯利三项极差≤{threshold:g}")
    for op, value in (("le", -.02), ("ge", .02)):
        add("kellyMovement", "kelly.change.draw", op, value, f"同公司初即凯利平值变化{'≤' if op=='le' else '≥'}{value:+g}")

    for market, label in (("sporttery", "竞彩测算"), ("simulated", "胜平负模拟")):
        add("profitMinimum", f"profit.{market}.drawMinimum", "eq", 1, f"{label}平项庄家ROI最低（含并列）", single=True)
        for op, thresholds in (("le", (0.0, -10.0, -25.0)), ("ge", (0.0, 10.0, 25.0, 50.0))):
            for threshold in thresholds:
                add("profitRoi", f"profit.{market}.roi.draw", op, threshold,
                    f"{label}平项庄家ROI{'≤' if op=='le' else '≥'}{threshold:+g}%")
        for threshold in (20.0, 25.0, 30.0):
            add("profitShare", f"profit.{market}.share.draw", "ge", threshold, f"{label}平项支持比例≥{threshold:g}%")
        add("profitShare", f"profit.{market}.share.draw", "le", 15.0, f"{label}平项支持比例≤15%")
    for op, thresholds in (("le", (0.0, -10.0)), ("ge", (0.0, 10.0))):
        for threshold in thresholds:
            add("officialProfitRate", "trade.jyykSpf.dy", op, threshold,
                f"源站真实平盈亏率{'≤' if op=='le' else '≥'}{threshold:+g}%")
    for market, label in (("william", "威廉赔率×共享分布"), ("local", "本地均赔测算")):
        add("profitMinimum", f"profit.{market}.drawMinimum", "eq", 1, f"{label}平项庄家ROI最低", single=True)
        for op, threshold in (("le", 0.0), ("ge", 25.0), ("ge", 50.0)):
            add("secondaryProfitRoi", f"profit.{market}.roi.draw", op, threshold, f"{label}平项庄家ROI{'≤' if op=='le' else '≥'}{threshold:g}%")

    for threshold in (60.0, 70.0, 80.0):
        add("asianHomeHot", "heat.asianHome", "ge", threshold, f"亚盘主队计算热度≥{threshold:g}%")
    for threshold in (40.0, 30.0, 20.0):
        add("asianAwayHot", "heat.asianHome", "le", threshold, f"亚盘客队计算热度≥{100-threshold:g}%")
    buckets["asianHeat"] = buckets["asianHomeHot"] + buckets["asianAwayHot"]
    for side, label in (("over", "大"), ("under", "小")):
        for threshold in (55.0, 65.0, 75.0):
            add("goalsHeat", "heat.goals." + side, "ge", threshold, f"大小球计算{label}热度≥{threshold:g}%")
    for op, thresholds in (("ge", (20.0, 25.0, 30.0)), ("le", (10.0, 15.0))):
        for threshold in thresholds:
            add("retailDraw", "retail.draw", op, threshold, f"本地散户心理平比例{'≥' if op=='ge' else '≤'}{threshold:g}%")
    for threshold in (2.0, 4.0, 6.0):
        add("professionalRisk", "professional.drawRisk.score", "ge", threshold, f"前台专业平风险分数≥{threshold:g}", single=True)
    for path, label in (("bookmaker.outcome", "庄家专业方向"), ("platform.outcome", "平台专业方向"),
                        ("warningAdjusted.outcome", "警示后专业方向"), ("professionalConsensus", "凯体彩专业共识")):
        add("professionalDraw", "professional." + path + ".draw", "eq", 1, label + "为平", single=True)

    for atom_id in singles:
        rule([atom_id], "单项疑似信号；弱条件仍须报告非平误选")
    pairs("lowGoals", "balanceGap", "低比分环境：实际低总球盘口与胜负均衡", same_phase=True)
    pairs("shallowAsian", "drawProbability", "低比分均势：浅亚盘与市场平概率", same_phase=True)
    pairs("shallowAsian", "lowGoals", "低比分均势：浅亚盘与低总球盘口", same_phase=True)
    pairs("balanceGap", "drawProbability", "赔率均衡：主客差较小且平概率达到固定档", same_phase=True)
    pairs("highGoals", "pairTotalHigh", "高比分互攻：高总球盘与双方近期高总球")
    pairs("highGoals", "pairScoring", "高比分互攻：高总球盘与双方进球能力")
    pairs("highGoals", "pairConceding", "高比分互攻：高总球盘与双方失球暴露")
    pairs("highGoals", "balanceGap", "高比分均势：高总球盘与赔率均衡", same_phase=True)
    pairs("deepAsian", "recentDraw", "深盘冷门拉平：强弱盘口与近期偏平")
    pairs("deepAsian", "kellyMinimum", "深盘反差：深亚盘与凯利平项最低")
    pairs("deepAsian", "professionalDraw", "深盘反差：深亚盘与专业平向")
    pairs("differenceSignal", "drawProbability", "用户源差值形态与市场平概率", same_phase=True)
    pairs("differenceSignal", "profitMinimum", "用户源差值形态与庄家平项最低ROI")
    pairs("differenceSignal", "profitRoi", "用户源差值形态与庄家平项盈亏符号/固定档")

    def compatible_differences(a, b):
        left, right = atom_by_id[a]["feature"].split("."), atom_by_id[b]["feature"].split(".")
        # Latest maximum/negative/positive classes on one source are mutually
        # exclusive. Cross-source or initial/latest agreement is meaningful.
        return left[1:3] != right[1:3]

    pairs("differenceSignal", "differenceSignal", "源差值多源或跨阶段共同信号", predicate=compatible_differences)
    pairs("asianHomeHot", "kellyAvoidHome", "热门主队与凯利避胜反差")
    pairs("asianAwayHot", "kellyAvoidAway", "热门客队与凯利避负反差")
    pairs("kellyAvoidHome", "kellyAvoidHome", "初盘和即时凯利共同避胜")
    pairs("kellyAvoidAway", "kellyAvoidAway", "初盘和即时凯利共同避负")
    pairs("goalsHeat", "kellyMinimum", "大小球热度与凯利平向共同信号")
    pairs("asianHeat", "differenceSignal", "亚盘热度与用户源差值反差信号")
    pairs("profitMinimum", "drawProbability", "庄家平项最低ROI与市场平概率")
    pairs("profitRoi", "balanceGap", "庄家平项盈亏档与较强赔率均衡（概率差≤20%）",
          predicate=lambda a, b: atom_by_id[b]["value"] <= .20)
    pairs("profitRoi", "lowGoals", "低总球环境下的庄家平项盈亏")
    pairs("profitRoi", "highGoals", "高总球环境下的庄家平项盈亏")
    pairs("recentDraw", "pairBalance", "历史偏平与双方近期净胜球接近")
    pairs("recentDraw", "balanceGap", "历史偏平与市场主客均衡")
    pairs("recentDraw", "recentDraw", "主客双方近期同时偏平",
          predicate=lambda a, b: atom_by_id[a]["feature"].split(".")[1] != atom_by_id[b]["feature"].split(".")[1])
    pairs("h2hDraw", "drawProbability", "有交锋样本的历史偏平与市场平概率")
    pairs("h2hDraw", "shallowAsian", "有交锋样本的历史偏平与浅盘")
    pairs("h2hDraw", "recentDraw", "交锋偏平与单队近期偏平")
    pairs("recentLowGoals", "lowGoals", "历史低总球与实际低盘口共同环境")
    pairs("recentHighGoals", "highGoals", "历史高总球与实际高盘口共同环境")
    pairs("professionalRisk", "balanceGap", "专业平风险与市场均势")
    pairs("professionalRisk", "highGoals", "高总球比赛中的专业平风险")
    pairs("professionalDraw", "differenceSignal", "专业平向与用户源差值共同信号")
    pairs("drawMovement", "differenceSignal", "市场平概率变动与即时源差值",
          predicate=lambda a, b: phase(b) == "latest")
    pairs("goalsMovement", "pairTotalGap", "总球盘口变动与双方近期总球接近")
    pairs("officialProfitRate", "differenceSignal", "源站真实平盈亏率与用户源差值")
    pairs("pairDraw", "lowGoals", "双方近期平率合计与低总球盘口")
    # These extra, fixed cross-family templates use atoms that would otherwise
    # remain unused; no data-dependent ordering or coverage-based pruning occurs.
    pairs("differenceMagnitude", "kellyMinimum", "较大平赔率差与凯利平向")
    pairs("differenceSign", "shallowAsian", "平差正负方向在浅盘环境中的对照")
    pairs("favorite", "h2hDraw", "明显热门比赛中的交锋偏平反差")
    pairs("profitShare", "balanceGap", "平项支持比例与较强赔率均势（概率差≤20%）",
          predicate=lambda a, b: atom_by_id[b]["value"] <= .20)
    pairs("recentZero", "shallowAsian", "近期零比零倾向与浅盘")
    pairs("h2hLowGoals", "lowGoals", "交锋低总球与实际低盘口")
    pairs("h2hHighGoals", "highGoals", "交锋高总球与实际高盘口")
    pairs("h2hZero", "shallowAsian", "交锋零比零倾向与浅盘")
    pairs("secondaryProfitRoi", "drawProbability", "其他页面测算ROI与市场平概率")
    pairs("retailDraw", "differenceTopTwo", "散户平比例与用户平差前二")
    pairs("kellyDraw", "drawProbability", "凯利平值固定档与市场平概率", same_phase=True)
    pairs("kellyFlat", "lowGoals", "凯利分布接近与低总球环境", same_phase=True)
    pairs("kellyMovement", "differenceTopTwo", "同公司凯利平变化与平差前二")
    pairs("asianMovement", "recentDraw", "亚盘变动与近期偏平")
    pairs("recentScoring", "recentConceding", "一方近期进球能力与另一方失球暴露",
          predicate=lambda a, b: atom_by_id[a]["feature"].split(".")[1] != atom_by_id[b]["feature"].split(".")[1])
    pairs("drawOdds", "pairTotalGap", "平赔固定档与双方近期总球接近")
    pairs("entropy", "pairDraw", "市场三项概率接近与双方近期偏平")

    excluded.extend([
        {"pattern": "availability.* or finite-input-only", "reason": "No match merely because input data exist; missing never matches and completeness is not a terminal draw hypothesis."},
        {"pattern": "history sample count as standalone", "reason": "Sample minimums are guards only; no n>0 endpoint or drawRate>=0 tautology."},
        {"pattern": "target actual score/result/subtype/status; ID/team/date", "reason": "Never accepted as rule inputs; high/low scoring mechanisms use only the frozen source variables, not actual score subtypes."},
        {"pattern": "profit drawMaximum", "reason": "No frozen standalone maximum-rank feature; do not silently invent a cross-column predicate. Existing drawMinimum and signed ROI comparisons remain explicit."},
        {"pattern": "same-source same-phase mutually exclusive difference categories", "reason": "No impossible conjunction of maximum/negativeTopTwo/positiveTopTwo."},
        {"pattern": "post-outcome quantile thresholds or best-correlated columns", "reason": "All thresholds and pair templates fixed in this module; no rows/labels are read."},
        {"pattern": "all candidate rules ORed as a claimed result", "reason": "This is only a hypothesis pool. The selector must report final non-draw false positives, overlap, coverage/precision tradeoff, source coverage, OR saturation and temporal internal validation, including failures to reach 900/970."},
    ])
    if len(atoms) > 300 or len(rules) > 1500:
        raise RuntimeError(f"Fixed candidate budget exceeded: {len(atoms)} atoms, {len(rules)} rules")
    assert len(atom_by_id) == len(atoms) and len(rule_by_id) == len(rules)
    assert all(1 <= len(item["allOf"]) <= 2 for item in rules)
    return {"atoms": atoms, "rules": rules, "excluded": excluded}


def main():
    root = Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, default=root / "reports/draw-full-feature-study/feature-manifest.json")
    parser.add_argument("--out", type=Path, default=root / "reports/draw-cover-study/candidate-design.json")
    args = parser.parse_args()
    manifest = json.loads(args.manifest.read_text(encoding="utf-8-sig"))
    candidates = build_candidates(manifest)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(candidates, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"atoms": len(candidates["atoms"]), "rules": len(candidates["rules"]),
                      "ruleLengths": dict(Counter(len(item["allOf"]) for item in candidates["rules"])),
                      "views": dict(Counter(item["view"] for item in candidates["atoms"])),
                      "excluded": len(candidates["excluded"]), "out": str(args.out)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
