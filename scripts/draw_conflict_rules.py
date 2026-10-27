"""Finite, result-blind hypotheses for popularity/price-direction conflicts."""
from __future__ import annotations
import hashlib
import json


def ident(prefix, value):
    return prefix + hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()[:16]


def build_rules(metadata):
    meta = {m["name"]: m for m in metadata}
    atoms, rules, excluded = {}, {}, []

    def atom(feature, op, value, label, guards=(), transform="identity"):
        needed = [feature] + [g["feature"] for g in guards]
        if any(name not in meta for name in needed):
            raise ValueError("missing declared business input: " + repr(needed))
        core = {"feature": feature, "op": op, "value": value, "guards": list(guards), "transform": transform}
        aid = ident("a_", core)
        atoms.setdefault(aid, {"id": aid, **core, "label": label, "family": meta[feature]["family"],
                               "source": meta[feature]["source"], "view": meta[feature]["view"]})
        return aid

    def ge_guard(feature, value):
        return {"feature": feature, "op": "ge", "value": value}

    def anchor(phase, source, pair, smallest=False):
        mode = "absSmallestTwo" if smallest else "absLargestTwo"
        prefix = f"conflict.difference.{phase}.{source}.{mode}"
        phase_cn = "初盘" if phase == "opening" else "即时"
        source_cn = "威廉−竞彩" if source == "sporttery" else "威廉−均赔"
        pair_cn = {"home_draw": "胜平", "draw_away": "平负"}[pair]
        return atom(prefix + "." + pair, "eq", 1, f"{phase_cn}{source_cn}差值{pair_cn}" + ("（最小绝对值两项，次要对照）" if smallest else ""),
                    [{"feature": prefix + ".ambiguous", "op": "eq", "value": 0}])

    def popularity(side):
        title = "主队" if side == "home" else "客队"
        heat = lambda threshold: atom("heat.asianHome", "ge" if side == "home" else "le", threshold if side == "home" else 100 - threshold,
                                      f"{title}亚盘计算热度≥{threshold}%")
        history_guard = [ge_guard(f"safeRecent.{side}.10.n", 5)]
        streak = atom(f"conflict.recent.{side}.winStreak", "ge", 2, f"{title}赛前最近至少2连胜（历史≥5场）", history_guard)
        good = atom(f"safeRecent.{side}.10.winRate", "ge", .6, f"{title}赛前近10胜率≥60%（历史≥5场）", history_guard)
        over = atom("heat.goals.over", "ge", 65, "大球计算热度≥65%")
        under = atom("heat.goals.under", "ge", 65, "小球计算热度≥65%")
        # Kelly change is finite only when opening/latest come from the same known source.
        same_source = [{"feature": f"kelly.change.{side}", "op": "finite", "value": 0}]
        not_initial = atom(f"kelly.opening.minimum.{side}", "eq", 0, f"初盘凯利最小项不含{'胜' if side == 'home' else '负'}", same_source)
        not_latest = atom(f"kelly.latest.minimum.{side}", "eq", 0, f"即时凯利最小项不含{'胜' if side == 'home' else '负'}", same_source)
        draw_kelly = atom("kelly.latest.minimum.draw", "eq", 1, "即时凯利最小项含平")
        sim_hot_loss = atom(f"profit.simulated.roi.{side}", "lt", 0, f"{title}赢球时模拟庄家ROI为负")
        sim_draw_gain = atom("profit.simulated.roi.draw", "gt", 0, "平局时模拟庄家ROI为正")
        support = atom(f"trade.tzbl.{'h' if side == 'home' else 'a'}supportRate", "ge", 50, f"源站{title}支持比例≥50%")
        official_hot_loss = atom(f"marketProfit.sporttery.roi.{side}", "lt", 0, f"{title}赢球时源站支持口径竞彩ROI为负")
        official_draw_gain = atom("marketProfit.sporttery.roi.draw", "gt", 0, "平局时源站支持口径竞彩ROI为正")
        rise = atom(f"euro.william.change.oddsChange.{side}", "ge", .1, f"威廉{title}胜赔初即上升≥0.10")
        shallow = atom("line.selected.asianHandicap.latest.line", "le", .5, "即时亚盘绝对值≤0.5", transform="abs")
        retail = atom(f"retail.{side}", "ge", 50, f"本地心理模拟{title}支持≥50%")
        professional = atom("professional.professionalConsensus.draw", "eq", 1, "页面专业共识含平")
        # Each tuple: stable key, pre-existing conditions, added conditions, narrative source.
        return [
            ("hot60", [], [heat(60)], "计算亚盘热"),
            ("hot70", [], [heat(70)], "计算亚盘热"),
            ("hot80", [], [heat(80)], "计算亚盘热"),
            ("win_streak_over", [streak], [over], "近期连胜形象"),
            ("good_form_over", [good], [over], "近期强战绩形象"),
            ("hot_streak", [heat(70)], [streak], "计算亚盘热"),
            ("hot_good_form", [heat(70)], [good], "计算亚盘热"),
            ("hot_over", [heat(70)], [over], "计算亚盘热"),
            ("hot_kelly_avoids", [heat(70)], [not_initial, not_latest], "计算亚盘热"),
            ("hot_kelly_draw", [heat(70)], [draw_kelly], "计算亚盘热"),
            ("hot_simulated_profit", [heat(70)], [sim_hot_loss, sim_draw_gain], "计算亚盘热＋本地心理模拟盈亏"),
            ("hot_source_support", [heat(70)], [support], "计算亚盘热＋源站支持比例"),
            ("hot_price_rise", [heat(70)], [rise], "计算亚盘热"),
            ("good_form_over_shallow", [good, over], [shallow], "近期强战绩形象"),
            ("hot_under", [heat(70)], [under], "计算亚盘热（小球对照）"),
            ("hot_source_profit", [heat(70)], [official_hot_loss, official_draw_gain], "计算亚盘热＋源站支持口径测算盈亏"),
            ("hot_retail", [heat(70)], [retail], "计算亚盘热＋本地心理模拟"),
            ("hot_professional_draw", [heat(70)], [professional], "计算亚盘热＋页面派生共识"),
        ]

    def add_rule(anchor_ids, variant, side, scenario, tier="primary"):
        key, before, extra, favorite_source = variant
        all_ids = sorted(set(anchor_ids + before + extra))
        parent_ids = sorted(set(anchor_ids + before))
        image_ids = sorted(set(before + extra))
        core = {"allOf": all_ids, "parentAllOf": parent_ids, "anchorAllOf": sorted(anchor_ids),
                "imageAllOf": image_ids, "favorite": side, "tier": tier}
        rid = ident("r_", core)
        label = " 且 ".join(atoms[a]["label"] for a in anchor_ids + before + extra)
        rules.setdefault(rid, {"id": rid, **core, "label": label,
                         "parentLabel": " 且 ".join(atoms[a]["label"] for a in anchor_ids + before),
                         "scenario": scenario, "variant": key, "favoriteSource": favorite_source,
                         "view": "snapshot" if any(atoms[a]["view"] == "snapshotDerived" for a in all_ids) else "clean"})

    for phase in ("opening", "latest"):
        for source in ("sporttery", "average"):
            for pair in ("draw_away", "home_draw"):
                a = anchor(phase, source, pair)
                for side in ("home", "away"):
                    excluded_side = "home" if pair == "draw_away" else "away"
                    scenario = "差值排除关注方但包含平" if side == excluded_side else "差值保留关注方与平"
                    for variant in popularity(side):
                        add_rule([a], variant, side, scenario)
                    # Secondary interpretation never replaces a failed largest-two result.
                    s = anchor(phase, source, pair, True)
                    for variant in popularity(side):
                        if variant[0] in ("hot70", "win_streak_over"):
                            add_rule([s], variant, side, "最小绝对值两项灵敏度", "sensitivity")
    # Two companies' candidate pairs intersect only at draw (orientation kept explicit).
    for phase in ("opening", "latest"):
        for jc, av in (("draw_away", "home_draw"), ("home_draw", "draw_away")):
            anchors = [anchor(phase, "sporttery", jc), anchor(phase, "average", av)]
            for side in ("home", "away"):
                for variant in popularity(side):
                    if variant[0] in ("hot70", "win_streak_over", "hot_kelly_avoids", "hot_simulated_profit"):
                        add_rule(anchors, variant, side, "两组公司差值共同选项仅平")
    # Initial/current candidate directions reverse but share draw.
    for source in ("sporttery", "average"):
        for opening, latest in (("draw_away", "home_draw"), ("home_draw", "draw_away")):
            anchors = [anchor("opening", source, opening), anchor("latest", source, latest)]
            for side in ("home", "away"):
                for variant in popularity(side):
                    if variant[0] in ("hot70", "good_form_over"):
                        add_rule(anchors, variant, side, "初即差值方向切换但共同含平")
    return {"atoms": sorted(atoms.values(), key=lambda a: a["id"]),
            "rules": sorted(rules.values(), key=lambda r: r["id"]), "excluded": excluded,
            "design": "Fixed user-directed business hypotheses; no labels/data or threshold fitting consumed."}
