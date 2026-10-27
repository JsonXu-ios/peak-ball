#!/usr/bin/env python3
"""Read-only, reproducible study of draw features from an exported local dataset.

Uses Python's standard library only.  No database/network access.  Missing prices
are never converted to zero.  Rule selection never reads held-out outcomes.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import html
import json
import math
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable

OUTCOMES = ("胜", "平", "负")
SPLITS = ("train", "calibration", "test", "all")
SPLIT_LABELS = {"train": "训练", "calibration": "校准", "test": "时间留出测试", "all": "全部（描述性）"}
EPS = 1e-9


def vector(value):
    if not isinstance(value, list) or len(value) != 3:
        return None
    if any(isinstance(v, bool) or not isinstance(v, (float, int)) or not math.isfinite(v) for v in value):
        return None
    return tuple(float(v) for v in value)


def prices(value):
    result = vector(value)
    return result if result and all(v > 0 for v in result) else None


def difference(left, right):
    return tuple(a - b for a, b in zip(left, right)) if left and right else None


def rounded(value):
    return round(value, 8) if isinstance(value, float) else value


def draw_count(rows):
    return sum(r["result"] == 1 for r in rows)


def wilson(successes, n):
    if not n:
        return None
    p = successes / n
    z = 1.959963984540054
    denominator = 1 + z * z / n
    center = (p + z * z / (2 * n)) / denominator
    radius = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denominator
    return [max(0, center - radius), min(1, center + radius)]


def stats(rows):
    n = len(rows)
    draws = draw_count(rows)
    return {"n": n, "draws": draws, "rate": draws / n if n else None, "wilson95": wilson(draws, n)}


def min_indices(values, absolute=False, count=1):
    """Keep all ties at the boundary; never break ties using outcome order."""
    values = [abs(v) for v in values] if absolute else values
    cutoff = sorted(values)[count - 1]
    return {i for i, value in enumerate(values) if value <= cutoff + EPS}


def max_indices(values, absolute=False):
    values = [abs(v) for v in values] if absolute else values
    cutoff = max(values)
    return {i for i, value in enumerate(values) if value >= cutoff - EPS}


def enrich(row):
    r = dict(row)
    r["date"] = str(r.get("date", ""))[:10]
    r["heat"] = r.get("homeHeat") if isinstance(r.get("homeHeat"), (float, int)) else None
    for stage in ("opening", "latest"):
        section = r.get(stage) or {}
        for name in ("william", "sporttery", "average", "kelly"):
            r[f"{stage}.{name}"] = prices(section.get(name))
        # Derive differences from actual input prices, rather than trusting a
        # displayed rounded difference or substituting another bookmaker.
        r[f"{stage}.ds"] = difference(r[f"{stage}.william"], r[f"{stage}.sporttery"])
        r[f"{stage}.da"] = difference(r[f"{stage}.william"], r[f"{stage}.average"])
        r[f"{stage}.sa"] = difference(r[f"{stage}.sporttery"], r[f"{stage}.average"])
        k = r[f"{stage}.kelly"]
        r[f"{stage}.kmin"] = min_indices(k) if k else None
    return r


def time_splits(rows):
    dates = sorted(Counter(r["date"] for r in rows).items())
    if len(dates) < 3:
        raise ValueError("At least three distinct match dates are required for time holdout.")
    cumulative = []
    total = 0
    for date, count in dates:
        total += count
        cumulative.append(total)
    a = min(range(len(dates) - 2), key=lambda i: abs(cumulative[i] - len(rows) * .70))
    b = min(range(a + 1, len(dates) - 1), key=lambda i: abs(cumulative[i] - len(rows) * .85))
    end_train, end_cal = dates[a][0], dates[b][0]
    for r in rows:
        r["split"] = "train" if r["date"] <= end_train else "calibration" if r["date"] <= end_cal else "test"
    return {"method": "按源站赛程日排序，以累计场数最接近 70% / 85% 的日期为边界；同一源站赛程日不可跨区。源站赛程日可能与自然开球日不同，另审计真实开球边界。", "trainEnd": end_train, "calibrationEnd": end_cal,
            "partitions": {s: {**stats([r for r in rows if r["split"] == s]),
                                "firstDate": min((r["date"] for r in rows if r["split"] == s), default=None),
                                "lastDate": max((r["date"] for r in rows if r["split"] == s), default=None)} for s in SPLITS[:-1]}}


@dataclass
class Rule:
    id: str
    family: str
    label: str
    requirements: tuple[str, ...]
    predicate: Callable


def rules():
    output = []

    def add(rule_id, family, label, required, predicate):
        output.append(Rule(rule_id, family, label, tuple(required), predicate))

    for heat in (70, 75, 80):
        required = ("heat", "opening.kelly", "latest.kelly")
        not_win = lambda r: 0 not in r["opening.kmin"] and 0 not in r["latest.kmin"]
        for kind, title, extra in (
            ("not_win", "初盘及即时凯利最低项均不含胜", lambda r: True),
            ("any_draw", "初即凯利均非胜，至少一个最低项为平", lambda r: 1 in r["opening.kmin"] or 1 in r["latest.kmin"]),
            ("both_draw", "初盘和即时凯利最低项均为唯一平", lambda r: r["opening.kmin"] == r["latest.kmin"] == {1}),
            ("late_draw", "初即凯利均非胜，且即时凯利唯一平", lambda r: r["latest.kmin"] == {1}),
        ):
            add(f"heat{heat}_{kind}", "主队热度与凯利背离", f"主队热度≥{heat}%，{title}", required,
                lambda r, h=heat, ex=extra, nw=not_win: r["heat"] >= h and nw(r) and ex(r))
        for kind, title, extra in (
            ("not_lose", "初盘及即时凯利最低项均不含负", lambda r: True),
            ("both_draw", "初盘和即时凯利最低项均为唯一平", lambda r: r["opening.kmin"] == r["latest.kmin"] == {1}),
        ):
            add(f"away_heat{heat}_{kind}", "客队热度与凯利背离（镜像）", f"客队热度≥{heat}%，{title}", required,
                lambda r, h=heat, ex=extra: 100 - r["heat"] >= h and 2 not in r["opening.kmin"] and 2 not in r["latest.kmin"] and ex(r))

    names = {"ds": "威廉－竞彩", "da": "威廉－均赔", "sa": "竞彩－均赔"}
    stages = {"opening": "初盘", "latest": "即时盘"}
    for stage, stitle in stages.items():
        for name, title in names.items():
            key = f"{stage}.{name}"
            for suffix, explanation, predicate in (
                ("min", "平为绝对差值唯一最小项", lambda v: min_indices(v, True) == {1}),
                ("top2", "平在绝对差值最小两项中", lambda v: 1 in min_indices(v, True, 2)),
                ("positive", "平差值为正", lambda v: v[1] > EPS),
                ("negative", "平差值为负", lambda v: v[1] < -EPS),
                ("lowest", "平为有符号差值唯一最小项", lambda v: min_indices(v) == {1}),
                ("highest", "平为有符号差值唯一最大项", lambda v: max_indices(v) == {1}),
            ):
                add(f"{stage}_{name}_{suffix}", "单一差值方向和排序", f"{stitle}{title}：{explanation}", (key,), lambda r, k=key, p=predicate: p(r[k]))

        ds, da = f"{stage}.ds", f"{stage}.da"
        for suffix, explanation, predicate in (
            ("both_min", "两组差值的绝对值唯一最小项均为平", lambda x, y: min_indices(x, True) == min_indices(y, True) == {1}),
            ("both_top2", "平同时进入两组绝对差值最小两项", lambda x, y: 1 in min_indices(x, True, 2) & min_indices(y, True, 2)),
            ("unique_draw", "两组绝对差值最小两项的交集仅为平", lambda x, y: min_indices(x, True, 2) & min_indices(y, True, 2) == {1}),
            ("both_positive", "两组平差值均为正", lambda x, y: x[1] > EPS and y[1] > EPS),
        ):
            add(f"{stage}_cross_{suffix}", "两组差值交叉", f"{stitle}威廉－竞彩、威廉－均赔：{explanation}", (ds, da), lambda r, x=ds, y=da, p=predicate: p(r[x], r[y]))

    for name, title in names.items():
        op, la = f"opening.{name}", f"latest.{name}"
        for suffix, explanation, predicate in (
            ("both_top2", "初即盘平都在绝对差值最小两项中", lambda x, y: 1 in min_indices(x, True, 2) and 1 in min_indices(y, True, 2)),
            ("both_min", "初即盘绝对差值唯一最小项均为平", lambda x, y: min_indices(x, True) == min_indices(y, True) == {1}),
            ("late_min", "初盘平在最小两项中，即时盘平为唯一最小项", lambda x, y: 1 in min_indices(x, True, 2) and min_indices(y, True) == {1}),
        ):
            add(f"consistent_{name}_{suffix}", "初即盘一致性", f"{title}：{explanation}", (op, la), lambda r, x=op, y=la, p=predicate: p(r[x], r[y]))

    for heat in (70, 75, 80):
        for stage in ("opening", "latest"):
            ds, da = f"{stage}.ds", f"{stage}.da"
            add(f"heat{heat}_kelly_{stage}_unique_draw", "热度、凯利、差值组合",
                f"主热≥{heat}%，初即凯利均非胜，{stages[stage]}两组最小两项交集仅平",
                ("heat", "opening.kelly", "latest.kelly", ds, da),
                lambda r, h=heat, x=ds, y=da: r["heat"] >= h and 0 not in r["opening.kmin"] and 0 not in r["latest.kmin"] and min_indices(r[x], True, 2) & min_indices(r[y], True, 2) == {1})
        add(f"heat{heat}_kelly_latest_both_top2", "热度、凯利、差值组合",
            f"主热≥{heat}%，初即凯利均非胜，平在即时两组最小两项中",
            ("heat", "opening.kelly", "latest.kelly", "latest.ds", "latest.da"),
            lambda r, h=heat: r["heat"] >= h and 0 not in r["opening.kmin"] and 0 not in r["latest.kmin"] and 1 in min_indices(r["latest.ds"], True, 2) & min_indices(r["latest.da"], True, 2))

    for stage, stitle in stages.items():
        for name, title in names.items():
            key = f"{stage}.{name}"

            def outlier(v):
                absolute = sorted(abs(x) for x in v)
                return absolute[-1] >= 2 * absolute[-2] + EPS and 1 not in max_indices(v, True)

            add(f"{stage}_{name}_exclude_outlier", "大差值排除", f"{stitle}{title}：最大绝对差至少为次大的2倍，且最大项非平", (key,), lambda r, k=key: outlier(r[k]))

    # Limited robustness checks: remove the mechanical scale advantage of
    # shorter-priced outcomes, without expanding into an arbitrary search grid.
    for stage, stitle in stages.items():
        ds, da = f"{stage}.ds", f"{stage}.da"
        sp, av = f"{stage}.sporttery", f"{stage}.average"
        for source, key, denominator in (("ds", ds, sp), ("da", da, av)):
            add(f"{stage}_{source}_relative_min", "相对差稳健性", f"{stitle}{names[source]}：相对绝对差唯一最小项为平", (key, denominator),
                lambda r, k=key, d=denominator: min_indices(tuple(v / base for v, base in zip(r[k], r[d])), True) == {1})
        add(f"{stage}_cross_relative_unique_draw", "相对差稳健性", f"{stitle}两组相对绝对差最小两项交集仅为平", (ds, da, sp, av),
            lambda r, x=ds, y=da, a=sp, b=av: min_indices(tuple(v / base for v, base in zip(r[x], r[a])), True, 2) & min_indices(tuple(v / base for v, base in zip(r[y], r[b])), True, 2) == {1})
    assert len(output) <= 100, len(output)
    assert len({r.id for r in output}) == len(output)
    return output


def evaluate(rule, rows):
    eligible = [r for r in rows if all(r.get(key) is not None for key in rule.requirements)]
    matched = [r for r in eligible if rule.predicate(r)]
    partitions = {}
    for split in SPLITS:
        inputs = eligible if split == "all" else [r for r in eligible if r["split"] == split]
        hits = matched if split == "all" else [r for r in matched if r["split"] == split]
        baseline, performance = stats(inputs), stats(hits)
        partitions[split] = {**performance, "eligible": baseline, "selectionRate": len(hits) / len(inputs) if inputs else None,
                             "liftPP": (performance["rate"] - baseline["rate"]) * 100 if inputs and hits else None,
                             "relativeLift": performance["rate"] / baseline["rate"] if hits and baseline["rate"] else None}
    return {"id": rule.id, "family": rule.family, "label": rule.label, "requirements": list(rule.requirements), "partitions": partitions}, matched


def select_candidate(results):
    """Strictly development-only selection; no held-out counts or outcomes."""
    qualified = []
    for result in results:
        train, cal = result["partitions"]["train"], result["partitions"]["calibration"]
        if train["n"] >= 60 and cal["n"] >= 15 and train["liftPP"] > 0 and cal["liftPP"] > 0:
            # Lower confidence bound relative to the same-input baseline,
            # with training consistency as a secondary ordering criterion.
            qualified.append((cal["wilson95"][0] - cal["eligible"]["rate"], min(train["liftPP"], cal["liftPP"]), cal["n"], result["id"]))
    if not qualified:
        return None, {"minimumTrainMatches": 60, "minimumCalibrationMatches": 15, "eligibleCandidates": 0,
                      "reason": "没有候选同时达到训练≥60场、校准≥15场且两区均优于同输入资格基准；不推荐强行选型。"}
    winner = max(qualified)
    return winner[-1], {"minimumTrainMatches": 60, "minimumCalibrationMatches": 15, "eligibleCandidates": len(qualified),
                        "criterion": "仅使用训练/校准：两区提升均为正；按校准 Wilson 下界减同输入基准排序，随后比较两区最小提升和校准样本量。",
                        "calibrationLowerBoundLiftPP": winner[0] * 100,
                        "multipleComparisonWarning": "候选规则共享赔率信息，做过多规则比较；普通 Wilson 区间未校正多重比较，校准结果不能当最终证明。"}


def coverage_tests(rows):
    output = []
    for stage in ("opening", "latest"):
        for source in ("ds", "da", "sa"):
            key = f"{stage}.{source}"
            for normalized in (False, True):
                reference = f"{stage}.sporttery" if source == "ds" else f"{stage}.average"
                eligible = [r for r in rows if r[key] is not None and (not normalized or r[reference] is not None)]
                partitions = {}
                for split in SPLITS:
                    sample = eligible if split == "all" else [r for r in eligible if r["split"] == split]
                    success, kept, excluded, excluded_success, unique_exclusions = 0, 0, Counter(), Counter(), 0
                    comparable, signal_covered, market_covered, market_kept = 0, 0, 0, 0
                    signal_only, market_only = 0, 0
                    for r in sample:
                        v = r[key]
                        if normalized:
                            v = tuple(d / base for d, base in zip(v, r[reference]))
                        choices = min_indices(v, True, 2)
                        success += r["result"] in choices
                        kept += len(choices)
                        removed = set(range(3)) - choices
                        if len(removed) == 1:
                            unique_exclusions += 1
                        for i in removed:
                            excluded[i] += 1
                            excluded_success[i] += r["result"] == i
                        average = r[f"{stage}.average"]
                        if average:
                            # Top two normalized implied probabilities have the
                            # same rank as the two smallest average prices.
                            market_choices = min_indices(average, count=2)
                            signal_hit, market_hit = r["result"] in choices, r["result"] in market_choices
                            comparable += 1
                            signal_covered += signal_hit
                            market_covered += market_hit
                            market_kept += len(market_choices)
                            signal_only += signal_hit and not market_hit
                            market_only += market_hit and not signal_hit
                    n = len(sample)
                    partitions[split] = {"n": n, "covered": success, "coverageRate": success / n if n else None,
                                         "wilson95": wilson(success, n), "meanRetainedOptions": kept / n if n else None,
                                         "hasUniqueExcludedOption": unique_exclusions,
                                         "marketComparable": {"n": comparable, "signalCovered": signal_covered, "marketCovered": market_covered,
                                                              "signalCoverageRate": signal_covered / comparable if comparable else None,
                                                              "marketCoverageRate": market_covered / comparable if comparable else None,
                                                              "marketMeanRetainedOptions": market_kept / comparable if comparable else None,
                                                              "signalOnly": signal_only, "marketOnly": market_only},
                                         "excludedOutcomes": [{"outcome": OUTCOMES[i], "n": excluded[i], "actualOccurred": excluded_success[i], "actualRate": excluded_success[i] / excluded[i] if excluded[i] else None} for i in range(3)]}
                output.append({"stage": stage, "source": source, "normalized": normalized, "partitions": partitions})
    return output


def sign_tests(rows):
    output = []
    for stage in ("opening", "latest"):
        for source in ("ds", "da", "sa"):
            key = f"{stage}.{source}"
            for outcome in range(3):
                for sign, predicate in (("正", lambda v: v > EPS), ("负", lambda v: v < -EPS), ("零", lambda v: abs(v) <= EPS)):
                    for split in SPLITS:
                        base = [r for r in rows if r[key] is not None and (split == "all" or r["split"] == split)]
                        sample = [r for r in base if predicate(r[key][outcome])]
                        n = len(sample)
                        actual = sum(r["result"] == outcome for r in sample)
                        b_rate = sum(r["result"] == outcome for r in base) / len(base) if base else None
                        output.append({"stage": stage, "source": source, "outcome": OUTCOMES[outcome], "sign": sign, "split": split,
                                       "n": n, "occurred": actual, "rate": actual / n if n else None, "wilson95": wilson(actual, n), "eligibleRate": b_rate})
    return output


def summarize_groups(rows, field):
    groups = defaultdict(list)
    for r in rows:
        value = r["date"][:7] if field == "month" else r.get(field) or "未知"
        groups[value].append(r)
    return [{"group": group, **stats(items), "splits": dict(Counter(r["split"] for r in items))} for group, items in sorted(groups.items())]


def market_comparison(rows):
    result = {}
    for split in SPLITS:
        result[split] = {}
        subset = rows if split == "all" else [r for r in rows if r["split"] == split]
        for stage in ("opening", "latest"):
            sample = [r for r in subset if r[f"{stage}.average"]]
            probabilities = [(1 / r[f"{stage}.average"][1]) / sum(1 / x for x in r[f"{stage}.average"]) for r in sample]
            expected = sum(probabilities)
            actual = draw_count(sample)
            result[split][stage] = {"n": len(sample), "meanDeVigDrawProbability": expected / len(sample) if sample else None,
                                    "expectedDraws": expected, "actualDraws": actual, "observedRate": actual / len(sample) if sample else None,
                                    "observedMinusExpectedPP": (actual - expected) / len(sample) * 100 if sample else None}
    return result


def public_row(r):
    result = {k: v for k, v in r.items() if "." not in k and k != "heat"}
    result["drawHit"] = r["result"] == 1
    result["resultLabel"] = OUTCOMES[r["result"]]
    result["derived"] = {k: [rounded(x) for x in v] if v is not None else None for k, v in r.items() if "." in k and not k.endswith("kmin")}
    return result


def p(value):
    return "—" if value is None else f"{value * 100:.1f}%"


def cell_stat(value):
    ci = value["wilson95"]
    interval = f"{p(ci[0])}～{p(ci[1])}" if ci else "—"
    return f'{value["draws"]}/{value["n"]} · <b>{p(value["rate"])}</b><br><small>基准 {p(value["eligible"]["rate"])}；提升 {value["liftPP"]:+.1f} 个百分点；95%区间 {interval}</small>' if value["n"] else "0 场"


def write_html(path, report):
    selected = next((c for c in report["candidates"] if c["id"] == report["selectedRuleId"]), None)
    selected_html = (f'<p><b>{html.escape(selected["label"])}</b></p><div class="metrics">' + "".join(f'<section><h3>{SPLIT_LABELS[s]}</h3>{cell_stat(selected["partitions"][s])}</section>' for s in SPLITS[:-1]) + '</div>') if selected else '<p class="warn">未形成符合预设最低样本标准的候选，不推荐强行部署。</p>'
    prompt_ids = {"heat70_not_win", "heat80_not_win", "opening_cross_unique_draw", "latest_cross_unique_draw", "latest_ds_positive", "latest_da_positive", "opening_da_min", "latest_da_min", "consistent_da_both_min"}
    prompt_rows = ''.join('<tr><td>' + html.escape(c["label"]) + '</td>' + ''.join(f'<td>{cell_stat(c["partitions"][s])}</td>' for s in ("all", "train", "calibration", "test")) + '</tr>' for c in report["candidates"] if c["id"] in prompt_ids)
    candidate_rows = []
    for c in report["candidates"]:
        candidate_rows.append(f'<tr data-search="{html.escape(c["label"] + c["family"])}"><td>{html.escape(c["id"])}<br><b>{html.escape(c["label"])}</b><br><small>{html.escape(c["family"])}</small></td>' + "".join(f'<td>{cell_stat(c["partitions"][s])}</td>' for s in SPLITS) + '</tr>')
    coverage_rows = []
    source_names = {"ds": "威廉－竞彩", "da": "威廉－均赔", "sa": "竞彩－均赔"}
    for c in report["topTwoCoverage"]:
        coverage_rows.append(f'<tr><td>{"初盘" if c["stage"] == "opening" else "即时盘"} {source_names[c["source"]]}<br>{"除以比较基准赔率后的相对差" if c["normalized"] else "原始赔率差"}</td>' + "".join(f'<td>{c["partitions"][s]["covered"]}/{c["partitions"][s]["n"]} · <b>{p(c["partitions"][s]["coverageRate"])}</b><br><small>平均保留 {c["partitions"][s]["meanRetainedOptions"] or 0:.2f} 项；同资格市场覆盖 {p(c["partitions"][s]["marketComparable"]["marketCoverageRate"])}（{c["partitions"][s]["marketComparable"]["n"]}场）</small></td>' for s in SPLITS) + '</tr>')
    month_rows = "".join(f'<tr><td>{g["group"]}</td><td>{g["draws"]}/{g["n"]}</td><td>{p(g["rate"])}</td><td>{html.escape(str(g["splits"]))}</td></tr>' for g in report["selectedByMonth"])
    match_rows = []
    for r in report["selectedMatches"]:
        differences = []
        for stage in ("opening", "latest"):
            values = []
            for k, label in (("ds", "威廉－竞彩"), ("da", "威廉－均赔")):
                v = r["derived"].get(f"{stage}.{k}")
                values.append(label + "：" + (" / ".join(f"{x:+.3f}" for x in v) if v else "暂无"))
            differences.append("<br>".join(values))
        match_rows.append(f'<tr class="{"hit" if r["drawHit"] else "miss"}" data-hit="{str(r["drawHit"]).lower()}"><td>{html.escape(str(r.get("date", "")))}<br>{SPLIT_LABELS[r["split"]]}</td><td>{html.escape(str(r["matchId"]))}<br>{html.escape(str(r.get("league", "")))}<br>{html.escape(str(r.get("home", "")))}－{html.escape(str(r.get("guest", "")))}</td><td><b>{html.escape(str(r.get("homeScore")))}:{html.escape(str(r.get("awayScore")))}（{r["resultLabel"]}）</b></td><td>{r.get("homeHeat") if r.get("homeHeat") is not None else "暂无"}</td><td>{differences[0]}</td><td>{differences[1]}</td></tr>')
    escaped_json = json.dumps(report, ensure_ascii=False).replace("</", "<\\/")
    document = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>本地平局特征研究</title><style>
body{font:15px/1.6 system-ui,sans-serif;background:#101418;color:#e1e8ef;margin:0;padding:28px;max-width:1800px;margin:auto}h1,h2,h3{line-height:1.25}h2{margin-top:38px}a{color:#79bfff}.warn{background:#493222;border-left:5px solid #e9a958;padding:16px}.note{background:#172631;padding:16px}table{border-collapse:collapse;width:100%;font-size:13px}th,td{border:1px solid #35404b;padding:10px;vertical-align:top}th{background:#27313b;position:sticky;top:0}small{color:#a8b8c8}input,select{background:#202a35;color:white;border:1px solid #526274;padding:10px;min-width:280px;margin:8px 0}.metrics{display:flex;gap:12px;flex-wrap:wrap}.metrics section{background:#202a35;padding:18px;min-width:250px;flex:1}.metrics h3{margin-top:0}.scroll{max-height:700px;overflow:auto}.hit td:nth-child(3){color:#8dd3a6}.miss td:nth-child(3){color:#efa1a1}pre{white-space:pre-wrap;word-break:break-word;background:#202a35;padding:16px}
</style><h1>本地平局特征研究</h1>'''
    document += f'<p>数据导出时间：{html.escape(str(report["generatedAt"]))}；完赛比赛 {report["dataset"]["n"]} 场，实际平局 {report["dataset"]["draws"]} 场（{p(report["dataset"]["rate"])}）。候选 {report["candidateCount"]} 个。</p>'
    document += '<p class="warn">这是使用本地留存赔率的历史关联研究。赔率若在比赛开始后才采集，结果不能作为真实赛前命中率；初盘/即时盘只有价格阶段含义，不自动保证采集发生在赛前。测试区按源站赛程日留出，但不能修复采集时间泄漏。没有成本、收益或未来盈利承诺。</p>'
    document += '<h2>口径与预先固定的选择流程</h2><div class="note"><p>比赛按源站赛程日分成训练、校准、测试；同一赛程日不跨区。源站赛程日可能与自然开球日不同，另审计开球边界。每条规则只用自己要求的完整输入，并与同资格比赛的实际平率比较；缺值不当成零。凯利相同最低值时全部保留，排除“胜”须最低集合不含胜。绝对差值最小两项遇到并列时保留并列，可能实际保留3项。</p><p>差值方向：威廉－竞彩、威廉－平均欧赔；另单独研究字面上的竞彩－均赔。正数只表示威廉报价更高，不能仅凭正号认定“庄家怕打出”。价格阶段不同不混算。</p><p>候选最多100个，实际为固定规则生成的 ' + str(report["candidateCount"]) + ' 个。训练匹配至少60场、校准至少15场，且两区均优于自身基准；只按校准Wilson下界相对基准选型，测试结果不参与选型。Wilson区间未进行多重比较校正。</p></div>'
    document += '<pre>' + html.escape(json.dumps(report["split"], ensure_ascii=False, indent=2)) + '</pre>'
    document += '<h2>用户提示的直接核对及原始差值对照</h2><p>以下为固定提示和预先列入的原始差值规则，不按测试结果重新选择主规则。“初盘平绝对差最小”是主选相对差规则的原始差值对照。</p><div class="scroll"><table><tr><th>条件</th><th>全部（描述性）</th><th>训练</th><th>校准</th><th>时间留出测试</th></tr>' + prompt_rows + '</table></div>'
    document += '<h2>按预设方法选中的观察候选（尚未证实可部署）</h2>' + selected_html + '<p>95%区间只描述比例估计的不确定性。筛选集包含在资格基准里，两者不是独立两组；不能仅凭区间重叠或不重叠判断提升是否显著。本研究未据此作显著性结论。</p><pre>' + html.escape(json.dumps(report["selection"], ensure_ascii=False, indent=2)) + '</pre>'
    document += '<p>市场预期复核：所选比赛的平均欧赔先分别取倒数，再除以三项倒数之和，得到去水隐含平率；对每场取平均。其含义是报价隐含概率，不是真实已知概率，也不保证精确校准。</p><table><tr><th>分区</th><th>所选实际平率</th><th>初盘平均隐含平率</th><th>即时平均隐含平率</th></tr>' + ''.join(f'<tr><td>{SPLIT_LABELS[s]}</td><td>{p(report["selectedMarketComparison"][s]["opening"]["observedRate"])}</td><td>{p(report["selectedMarketComparison"][s]["opening"]["meanDeVigDrawProbability"])}</td><td>{p(report["selectedMarketComparison"][s]["latest"]["meanDeVigDrawProbability"])}</td></tr>' for s in SPLITS) + '</table>'
    document += '<h2>全部候选：命中率指平局精确率</h2><p>每格：命中平局数/筛选场数；基准为该规则具有完整输入的全部比赛。测试列为一次留出报告。全量列仅为描述。</p><input id="search" placeholder="筛选规则名称"><div class="scroll"><table id="candidates"><thead><tr><th>特征</th>' + ''.join(f'<th>{SPLIT_LABELS[s]}</th>' for s in SPLITS) + '</tr></thead><tbody>' + ''.join(candidate_rows) + '</tbody></table></div>'
    document += '<h2>“绝对差最小两项覆盖赛果”独立检验</h2><p>这是二选覆盖率，不等于单选平局命中率。基准是同资格比赛中平均欧赔隐含概率最高两项的实际覆盖率，并非假定每个赛果等概率的66.7%。原始绝对差易受赔率大小影响，因此另列相对差；平均保留项数大于2表示存在并列，覆盖率会被抬高。</p><div class="scroll"><table><thead><tr><th>口径</th>' + ''.join(f'<th>{SPLIT_LABELS[s]}</th>' for s in SPLITS) + '</tr></thead><tbody>' + ''.join(coverage_rows) + '</tbody></table></div>'
    document += '<h2>选中候选的月份分布</h2><table><tr><th>月份</th><th>平局/筛选</th><th>平率</th><th>分区</th></tr>' + month_rows + '</table>'
    document += '<h2>选中候选的全部比赛（包含失败）</h2><p>赔率差值顺序为胜／平／负。完整输入、原始数据及全部正负号检验见 analysis.json；同目录另有CSV可复核。</p><select id="hitFilter"><option value="all">全部比赛</option><option value="false">只看未命中</option><option value="true">只看命中平局</option></select><div class="scroll"><table id="matches"><thead><tr><th>日期／分区</th><th>比赛</th><th>实际赛果</th><th>主热%</th><th>初盘差值</th><th>即时盘差值</th></tr></thead><tbody>' + ''.join(match_rows) + '</tbody></table></div>'
    document += '<h2>输入与采集时间审计</h2><pre>' + html.escape(json.dumps({"audit": report["inputAudit"], "coverage": report["inputCoverage"], "timing": report["timingAudit"]}, ensure_ascii=False, indent=2)) + '</pre>'
    document += '<h2>复核与共同输入资格</h2><p>各候选已列自己的同资格基准；另外把全部92条规则所需输入都齐全的比赛定义为共同资格集，完整比较另存 analysis.json 的 commonInputCohort 与候选 commonInputPartitions。该共同集仅为可比性复核，不参与选型。</p><pre>' + html.escape(json.dumps({"reproducibility": report["reproducibility"], "commonInputCohort": report["commonInputCohort"]}, ensure_ascii=False, indent=2)) + '</pre>'
    document += '<script type="application/json" id="report-data">' + escaped_json + '</script><script>document.getElementById("search").addEventListener("input",e=>{for(const r of document.querySelectorAll("#candidates tbody tr"))r.hidden=!r.dataset.search.includes(e.target.value)});document.getElementById("hitFilter").addEventListener("change",e=>{for(const r of document.querySelectorAll("#matches tbody tr"))r.hidden=e.target.value!=="all"&&r.dataset.hit!==e.target.value});</script></html>'
    path.write_text(document, encoding="utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dataset", nargs="?", type=Path, default=Path("reports/draw-feature-study/dataset.json"))
    parser.add_argument("--out", type=Path, default=Path("reports/draw-feature-study"))
    args = parser.parse_args()
    payload = json.loads(args.dataset.read_text(encoding="utf-8-sig"))
    rows = [enrich(r) for r in payload["rows"]]
    if any(r.get("result") not in (0, 1, 2) for r in rows):
        raise ValueError("Dataset contains an unconfirmed result.")
    if len({r["matchId"] for r in rows}) != len(rows):
        raise ValueError("Dataset must contain unique match IDs.")
    if any(not r["date"] for r in rows):
        raise ValueError("Every row needs a match date.")
    for r in rows:
        home, away = r.get("homeScore"), r.get("awayScore")
        if isinstance(home, (int, float)) and isinstance(away, (int, float)):
            if (0 if home > away else 1 if home == away else 2) != r["result"]:
                raise ValueError(f"Score/result mismatch for match {r['matchId']}")
    rows.sort(key=lambda r: (r["date"], str(r["matchId"])))
    splits = time_splits(rows)
    candidates = rules()
    results, matches = [], {}
    for rule in candidates:
        result, matched = evaluate(rule, rows)
        results.append(result)
        matches[rule.id] = matched
    selected_id, selection = select_candidate(results)
    selection["status"] = "research_only"
    selection["conclusion"] = "仅观察候选，未证实可部署；输入采集发生于赛前尚无法确认。"
    selected_matches = matches.get(selected_id, [])
    common_requirements = sorted({key for rule in candidates for key in rule.requirements})
    common_rows = [r for r in rows if all(r.get(key) is not None for key in common_requirements)]
    for rule, result in zip(candidates, results):
        common_result, _ = evaluate(rule, common_rows)
        result["commonInputPartitions"] = common_result["partitions"]
    # Test rows have now been reported exactly once; no adaptive search follows.
    coverage = {k: sum(r.get(k) is not None for r in rows) for k in ("heat", "opening.kelly", "latest.kelly", "opening.ds", "latest.ds", "opening.da", "latest.da", "opening.sa", "latest.sa")}
    timing = Counter()
    kickoff_by_split = defaultdict(list)
    for r in rows:
        t = r.get("timing") or {}
        timing["hasEuroCaptureTime" if t.get("euroCapturedAt") else "missingEuroCaptureTime"] += 1
        # Exporter may provide an audited pre-kickoff flag; never guess timezone
        # or reconstruct a year from an MM-DD string in the analysis script.
        flag = t.get("euroPreKickoff", t.get("euroPreMatch"))
        timing["euroConfirmedPreMatch" if flag is True else "euroConfirmedPostKickoff" if flag is False else "euroBeforeKickoffUnconfirmed"] += 1
        timing["hasLineCaptureTime" if t.get("lineCapturedAt") else "missingLineCaptureTime"] += 1
        timing["sportteryCaptureKnown" if t.get("sportteryCaptureKnown") is True else "sportteryCaptureUnknown"] += 1
        try:
            kickoff = datetime.fromisoformat(str(r.get("matchTime", "")).replace("Z", "+00:00"))
            if kickoff.utcoffset() is None:
                timing["kickoffTimezoneUnknown"] += 1
            else:
                kickoff_by_split[r["split"]].append(kickoff.astimezone(timezone.utc))
                if kickoff.date().isoformat() != r["date"]:
                    timing["sourceScheduleDayDiffersFromKickoffDay"] += 1
        except ValueError:
            timing["kickoffUnparseable"] += 1
    timing_audit = dict(timing)
    timing_audit["kickoffBoundariesUTC"] = {s: {"min": min(v).isoformat(), "max": max(v).isoformat(), "n": len(v)} for s, v in kickoff_by_split.items()}
    timing_audit["actualKickoffCrossesPartitions"] = any(max(kickoff_by_split[a]) >= min(kickoff_by_split[b]) for a, b in (("train", "calibration"), ("calibration", "test")) if kickoff_by_split[a] and kickoff_by_split[b])
    report = {"generatedAt": payload.get("generatedAt"), "inputAudit": payload.get("audit"), "dataset": stats(rows), "split": splits,
              "candidateCount": len(candidates), "inputCoverage": coverage, "timingAudit": timing_audit, "selectedRuleId": selected_id,
              "selection": selection, "candidates": results, "topTwoCoverage": coverage_tests(rows), "signTests": sign_tests(rows),
              "reproducibility": {"datasetSHA256": hashlib.sha256(args.dataset.read_bytes()).hexdigest(), "scriptSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), "command": f"python scripts/draw-feature-study.py {args.dataset.as_posix()} --out {args.out.as_posix()}"},
              "commonInputCohort": {"requirements": common_requirements, "partitions": {s: stats(common_rows if s == "all" else [r for r in common_rows if r["split"] == s]) for s in SPLITS}},
              "allByMonth": summarize_groups(rows, "month"), "selectedByMonth": summarize_groups(selected_matches, "month"),
              "selectedMarketComparison": market_comparison(selected_matches),
              "selectedByLeague": summarize_groups(selected_matches, "league"), "selectedMatches": [public_row(r) for r in selected_matches]}
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "analysis.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")
    write_html(args.out / "report.html", report)
    rule_specification = {"status": "research_only", "ruleId": selected_id, "datasetSHA256": report["reproducibility"]["datasetSHA256"],
                          "candidateCount": len(candidates), "selectionUses": ["train", "calibration"], "testUsedForSelection": False,
                          "warning": "观察条件；未确证赛前数据可用性或可部署效果。缺任一输入不匹配，价格不可拿其他公司替代。",
                          "observedPartitions": next((c["partitions"] for c in results if c["id"] == selected_id), None),
                          "marketComparison": report["selectedMarketComparison"]}
    if selected_id == "opening_da_relative_min":
        rule_specification["definition"] = {"stage": "opening", "outcomeOrder": ["home", "draw", "away"],
                                            "requiredInputs": ["opening.william[0:3]", "opening.average[0:3]"],
                                            "inputValidation": "Each vector must have exactly three finite strictly positive numeric prices.",
                                            "values": "abs(opening.william[i] - opening.average[i]) / opening.average[i], i=0,1,2",
                                            "predicate": "values[1] + tieTolerance < values[0] AND values[1] + tieTolerance < values[2]",
                                            "minimumGap": 0.0, "tieTolerance": EPS,
                                            "tieHandling": "Any tie or near-tie within tieTolerance for the minimum does not match.",
                                            "missingInputs": "exclude", "drawPrediction": True}
    else:
        rule_specification["definition"] = {"label": next((c["label"] for c in results if c["id"] == selected_id), None),
                                            "implementation": "See the rule with the matching ID in scripts/draw-feature-study.py; no fallback formula is substituted."}
    (args.out / "selected-rule.json").write_text(json.dumps(rule_specification, ensure_ascii=False, indent=2), encoding="utf-8")
    with (args.out / "candidates.csv").open("w", encoding="utf-8-sig", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["规则ID", "规则", "分区", "筛选场数", "命中平局", "平率", "同资格基准场数", "同资格平率", "提升百分点", "Wilson下限", "Wilson上限"])
        for c in results:
            for s in SPLITS:
                v = c["partitions"][s]
                writer.writerow([c["id"], c["label"], SPLIT_LABELS[s], v["n"], v["draws"], v["rate"], v["eligible"]["n"], v["eligible"]["rate"], v["liftPP"], *(v["wilson95"] or [None, None])])
    with (args.out / "selected-matches.csv").open("w", encoding="utf-8-sig", newline="") as f:
        writer = csv.writer(f)
        writer.writerow(["比赛ID", "日期", "分区", "联赛", "主队", "客队", "主进球", "客进球", "实际赛果", "命中平局", "主队热度", "初盘威廉-竞彩", "即时威廉-竞彩", "初盘威廉-均赔", "即时威廉-均赔"])
        for r in selected_matches:
            writer.writerow([r["matchId"], r["date"], SPLIT_LABELS[r["split"]], r.get("league"), r.get("home"), r.get("guest"), r.get("homeScore"), r.get("awayScore"), OUTCOMES[r["result"]], r["result"] == 1, r.get("homeHeat"), r["opening.ds"], r["latest.ds"], r["opening.da"], r["latest.da"]])
    print(json.dumps({"dataset": report["dataset"], "split": splits, "candidateCount": len(candidates), "selectedRuleId": selected_id, "selection": selection,
                      "selected": next((c for c in results if c["id"] == selected_id), None), "timingAudit": timing_audit}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
