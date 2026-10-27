#!/usr/bin/env python3
"""Audited wide-feature draw study with chronological INTERNAL validation.

No database or network access. Labels never enter the feature extractor.
All preprocessing, calibration and recall thresholds are fitted strictly on
earlier training/calibration rows; previous research dates are not advertised
as new independent validation. Warmup predictions are never invented.
"""
from __future__ import annotations

import os
os.environ.setdefault("OMP_NUM_THREADS", "4")
os.environ.setdefault("OPENBLAS_NUM_THREADS", "4")
os.environ.setdefault("MKL_NUM_THREADS", "4")
import argparse
import csv
import hashlib
import html
import json
import math
import sys
import warnings
from collections import Counter, defaultdict
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path

sys.dont_write_bytecode = True
import numpy as np
import scipy
import sklearn
from scipy.stats import mannwhitneyu
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.impute import SimpleImputer
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import average_precision_score, roc_auc_score
from sklearn.pipeline import make_pipeline
from sklearn.preprocessing import StandardScaler
from sklearn.tree import DecisionTreeClassifier, export_text
from threadpoolctl import threadpool_limits

from draw_full_features import build_features

ROOT = Path(__file__).resolve().parents[1]
RECALLS = (.5, .7, .8)
TOPS = (.1, .2, .3, .5)
SEED = 20261024


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def clean_json(value):
    if isinstance(value, np.ndarray):
        return clean_json(value.tolist())
    if isinstance(value, np.generic):
        return clean_json(value.item())
    if isinstance(value, dict):
        return {str(k): clean_json(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [clean_json(v) for v in value]
    if isinstance(value, float) and not math.isfinite(value):
        return None
    return value


def write(path, data):
    path.write_text(json.dumps(clean_json(data), ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def rate(a, b):
    return a / b if b else None


def wilson(k, n):
    if not n:
        return None
    p, z = k / n, 1.959963984540054
    den = 1 + z * z / n
    center = (p + z * z / (2 * n)) / den
    radius = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return [max(0, center - radius), min(1, center + radius)]


def confusion(y, selected):
    y, selected = np.asarray(y, bool), np.asarray(selected, bool)
    tp, fp = int(np.sum(y & selected)), int(np.sum(~y & selected))
    fn, tn = int(np.sum(y & ~selected)), int(np.sum(~y & ~selected))
    return {"n": len(y), "draws": int(sum(y)), "selected": tp + fp, "TP": tp, "FP": fp, "FN": fn, "TN": tn,
            "precision": rate(tp, tp + fp), "recall": rate(tp, tp + fn), "specificity": rate(tn, tn + fp),
            "selectionRate": rate(tp + fp, len(y)), "precisionWilson95": wilson(tp, tp + fp)}


def performance(y, p):
    y, p = np.asarray(y, int), np.clip(np.asarray(p, float), 1e-7, 1 - 1e-7)
    if not len(y):
        return {"n": 0, "draws": 0, "rate": None, "AP": None, "ROC_AUC": None, "Brier": None, "logLoss": None}
    return {"n": len(y), "draws": int(sum(y)), "rate": float(np.mean(y)),
            "AP": float(average_precision_score(y, p)) if sum(y) else None,
            "ROC_AUC": float(roc_auc_score(y, p)) if 0 < sum(y) < len(y) else None,
            "Brier": float(np.mean((p - y) ** 2)), "logLoss": float(-np.mean(y * np.log(p) + (1 - y) * np.log1p(-p)))}


def calibration(y, p):
    y, p = np.asarray(y), np.asarray(p)
    result = []
    for i in range(10):
        ix = (p >= i / 10) & (p < (i + 1) / 10 if i < 9 else p <= 1)
        result.append({"lower": i / 10, "upper": (i + 1) / 10, "n": int(sum(ix)),
                       "meanPrediction": float(np.mean(p[ix])) if any(ix) else None, "actualDrawRate": float(np.mean(y[ix])) if any(ix) else None})
    return result


def recall_threshold(y, p, target):
    positives = np.sort(np.asarray(p)[np.asarray(y) == 1])[::-1]
    return float(positives[max(0, math.ceil(target * len(positives)) - 1)]) if len(positives) else 1.0


def top_flags(p, count):
    if count <= 0:
        return np.zeros(len(p), bool)
    cutoff = np.sort(p)[::-1][min(len(p), count) - 1]
    return np.asarray(p) >= cutoff


def date_boundary(rows, fraction, minimum_index=0, max_index=None):
    dates = sorted({r["_day"] for r in rows})
    max_index = len(dates) - 1 if max_index is None else max_index
    counts = Counter(r["_day"] for r in rows)
    cumulative = np.cumsum([counts[d] for d in dates])
    return min(range(minimum_index, max_index + 1), key=lambda i: abs(cumulative[i] - len(rows) * fraction))


def make_folds(rows, protocol):
    dates = sorted({r["_day"] for r in rows})
    cuts, start = [], 0
    targets = protocol["timeValidation"]["cumulativeDateTargets"]
    for j, fraction in enumerate(targets[:-1]):
        idx = date_boundary(rows, fraction, start, len(dates) - (len(targets) - j))
        cuts.append(idx)
        start = idx + 1
    cuts.append(len(dates) - 1)
    result = []
    for fold in range(5):
        test_days = dates[cuts[fold] + 1:cuts[fold + 1] + 1]
        test = [i for i, r in enumerate(rows) if r["_day"] in test_days]
        test_start = min(rows[i]["_kickoff"] for i in test)
        available = [i for i, r in enumerate(rows) if r["_day"] < test_days[0] and r["_kickoff"] + timedelta(hours=2) < test_start]
        pool = [rows[i] for i in available]
        pool_dates = sorted({r["_day"] for r in pool})
        cal_cut = date_boundary(pool, .8, 0, len(pool_dates) - 2)
        cal_days = pool_dates[cal_cut + 1:]
        cal = [i for i in available if rows[i]["_day"] in cal_days]
        cal_start = min(rows[i]["_kickoff"] for i in cal)
        train = [i for i in available if rows[i]["_day"] < cal_days[0] and rows[i]["_kickoff"] + timedelta(hours=2) < cal_start]
        assert set(train).isdisjoint(cal) and set(train).isdisjoint(test) and set(cal).isdisjoint(test)
        assert len(train) >= 200 and len(cal) >= 40 and test
        result.append({"fold": fold, "role": "development" if fold < 3 else "lockedInternalAudit", "train": train, "calibration": cal, "test": test,
                       "firstTestDay": test_days[0], "lastTestDay": test_days[-1], "trainLatestResultAvailable": max(rows[i]["_kickoff"] + timedelta(hours=2) for i in train).isoformat(),
                       "calibrationFirstKickoff": cal_start.isoformat(), "calibrationLatestResultAvailable": max(rows[i]["_kickoff"] + timedelta(hours=2) for i in cal).isoformat(), "testFirstKickoff": test_start.isoformat()})
    return result


@dataclass
class ModelSpec:
    id: str
    label: str
    kind: str
    views: tuple = ()
    c: float = .1
    depth: int = 3
    excluded: tuple = ()
    primary: bool = False


def model_specs():
    clean = ("market", "safeHistory")
    return [
        ModelSpec("constant", "训练平率常数", "constant"),
        ModelSpec("market_raw", "均赔去水基准", "market"),
        ModelSpec("market_calibrated", "均赔校准基准", "market_cal"),
        ModelSpec("market_lr", "只用市场字段", "logistic", ("market",), primary=True),
        ModelSpec("history_lr", "只用严格赛前战绩", "logistic", ("safeHistory",), primary=True),
        *[ModelSpec("clean_lr_" + str(c).replace(".", ""), "市场与赛前战绩逻辑回归 C=" + str(c), "logistic", clean, c=c, primary=True) for c in (.01, .1, 1.0)],
        ModelSpec("clean_hgb2", "市场与赛前战绩浅提升树2层", "hgb", clean, depth=2, primary=True),
        ModelSpec("clean_hgb3", "市场与赛前战绩浅提升树3层", "hgb", clean, depth=3, primary=True),
        ModelSpec("clean_tree3", "市场与赛前战绩可解释3层树", "tree", clean, primary=True),
        ModelSpec("snapshot_hgb", "全部页面派生输入（辅助）", "hgb", clean + ("snapshotDerived",)),
        ModelSpec("snapshot_no_profit", "全部页面输入去掉盈亏支持", "hgb", clean + ("snapshotDerived",), excluded=("profit", "support", "trade", "roi")),
        ModelSpec("snapshot_no_heat", "全部页面输入去掉热度冷热心理", "hgb", clean + ("snapshotDerived",), excluded=("heat", "psychology", "retail", "psyerror", "hotcold")),
        ModelSpec("snapshot_no_platform", "全部页面输入去掉平台专业", "hgb", clean + ("snapshotDerived",), excluded=("platform", "professional")),
    ]


class Fitted:
    def __init__(self, spec, X, y, train, metadata):
        self.spec, self.prior = spec, float(np.mean(y[train]))
        cols = []
        for j, m in enumerate(metadata):
            if m["view"] not in spec.views or any(token in (m["family"] + "." + m["name"]).lower() for token in spec.excluded):
                continue
            values = X[train, j]
            valid = values[np.isfinite(values)]
            if len(valid) >= max(20, math.ceil(len(train) * .02)) and np.max(valid) > np.min(valid):
                cols.append(j)
        self.cols = cols
        self.estimator = None
        if spec.kind in ("constant", "market", "market_cal") or not cols:
            return
        if spec.kind in ("logistic", "tree"):
            estimator = LogisticRegression(C=spec.c, max_iter=2000, solver="lbfgs", random_state=SEED) if spec.kind == "logistic" else DecisionTreeClassifier(max_depth=3, min_samples_leaf=80, random_state=SEED)
            self.estimator = make_pipeline(SimpleImputer(strategy="median", add_indicator=True, keep_empty_features=True), StandardScaler(), estimator)
        else:
            self.estimator = HistGradientBoostingClassifier(max_iter=120, max_depth=spec.depth, max_leaf_nodes=4 if spec.depth == 2 else 7,
                                                           min_samples_leaf=60 if spec.depth == 2 else 80, l2_regularization=10, learning_rate=.05,
                                                           max_bins=32, early_stopping=False, random_state=SEED)
        self.estimator.fit(X[np.ix_(train, cols)], y[train])

    def raw(self, X, indices, market):
        if self.spec.kind in ("market", "market_cal"):
            return np.where(np.isfinite(market[indices]), market[indices], self.prior)
        if self.estimator is None:
            return np.repeat(self.prior, len(indices))
        return self.estimator.predict_proba(X[np.ix_(indices, self.cols)])[:, 1]


class Calibrator:
    def __init__(self, y, p, enabled=True):
        self.model = None
        if enabled and len(set(y)) == 2 and np.ptp(p) > 1e-10:
            self.model = LogisticRegression(C=1.0, solver="lbfgs", max_iter=1000).fit(self.logit(p), y)

    @staticmethod
    def logit(p):
        p = np.clip(p, 1e-6, 1 - 1e-6)
        return np.log(p / (1 - p)).reshape(-1, 1)

    def apply(self, p):
        return self.model.predict_proba(self.logit(p))[:, 1] if self.model is not None else p


def describe_features(X, y, metadata, initial):
    result = []
    for j, meta in enumerate(metadata):
        values = X[:, j]
        valid = np.isfinite(values)
        a, b = values[valid & (y == 1)], values[valid & (y == 0)]
        item = {**meta, "valid": int(sum(valid)), "validDraws": len(a), "validNonDraws": len(b), "missingDraws": int(sum(y)) - len(a),
                "missingNonDraws": int(sum(y == 0)) - len(b), "drawMedian": float(np.median(a)) if len(a) else None,
                "nonDrawMedian": float(np.median(b)) if len(b) else None, "mannWhitneyTwoSidedP": float(mannwhitneyu(a, b, alternative="two-sided").pvalue) if len(a) and len(b) else 1.0, "bins": []}
        training = values[initial]
        training = training[np.isfinite(training)]
        if len(training):
            thresholds = np.unique(np.quantile(training, [.25, .5, .75]))
            for k in range(len(thresholds) + 1):
                chosen = valid & (values > thresholds[k - 1] if k else True) & (values <= thresholds[k] if k < len(thresholds) else True)
                item["bins"].append({"lowerExclusive": float(thresholds[k - 1]) if k else None, "upperInclusive": float(thresholds[k]) if k < len(thresholds) else None,
                                     **confusion(y, chosen), "drawOccurrence": rate(int(sum(chosen & (y == 1))), int(sum(y))),
                                     "nonDrawOccurrence": rate(int(sum(chosen & (y == 0))), int(sum(y == 0)))})
        result.append(item)
    order = sorted(range(len(result)), key=lambda i: result[i]["mannWhitneyTwoSidedP"])
    q = 1.0
    for rank in range(len(order) - 1, -1, -1):
        i = order[rank]
        q = min(q, result[i]["mannWhitneyTwoSidedP"] * len(order) / (rank + 1))
        result[i]["descriptiveBHq"] = q
    return result


def pooled(records):
    y = np.array([r["y"] for r in records])
    p = np.array([r["probability"] for r in records])
    return {**performance(y, p), "recallTargets": {str(target): confusion(y, [r["selected"][str(target)] for r in records]) for target in RECALLS},
            "fixedTopFractions": {str(top): confusion(y, [r["top"][str(top)] for r in records]) for top in TOPS}, "calibration": calibration(y, p)}


def choose_primary(model_reports):
    candidates = [r for r in model_reports if r["primaryEligible"]]
    return min(candidates, key=lambda r: (-np.mean([f["metrics"]["AP"] for f in r["folds"][:3]]),
                                          np.mean([f["metrics"]["Brier"] for f in r["folds"][:3]]), r["id"]))["id"]


def equal_selection_comparison(records, market_records):
    """Use exactly the same evaluation fixtures and a model-selected count.

    The comparator never reads target outcomes to choose rows. Market boundary
    ties are retained, so actual selection count can exceed the requested one.
    """
    assert [r["matchId"] for r in records] == [r["matchId"] for r in market_records]
    flags = {str(t): np.zeros(len(records), bool) for t in RECALLS}
    for fold in range(5):
        ix = [i for i, r in enumerate(records) if r["fold"] == fold]
        prices = np.array([market_records[i]["probability"] for i in ix])
        for target in RECALLS:
            count = sum(records[i]["selected"][str(target)] for i in ix)
            flags[str(target)][ix] = top_flags(prices, count)
    result = {}
    for part in ("development", "audit", "allTemporal"):
        ix = [i for i, r in enumerate(records) if part == "allTemporal" or r["role"] == ("development" if part == "development" else "lockedInternalAudit")]
        result[part] = {}
        for target in RECALLS:
            requested = sum(records[i]["selected"][str(target)] for i in ix)
            comparison = confusion([records[i]["y"] for i in ix], flags[str(target)][ix])
            comparison["requestedSelectionCount"] = requested
            comparison["extraFromMarketBoundaryTies"] = comparison["selected"] - requested
            result[part][str(target)] = comparison
    return result


def readable_tree(model, X, y, train, cal, metadata):
    """Translate standardized tree thresholds back to original field units."""
    if model.estimator is None or model.spec.kind != "tree":
        return []
    imputer, scaler, tree_model = model.estimator.steps[0][1], model.estimator.steps[1][1], model.estimator.steps[2][1]
    names = imputer.get_feature_names_out([metadata[j]["name"] for j in model.cols])
    tree = tree_model.tree_
    cal_values = scaler.transform(imputer.transform(X[np.ix_(cal, model.cols)]))
    cal_leaves = tree_model.apply(cal_values)
    leaves = []
    def visit(node, path):
        if tree.children_left[node] == tree.children_right[node]:
            found = cal_leaves == node
            leaves.append({"conditions": path, "trainingN": int(tree.n_node_samples[node]),
                           "trainingDrawRate": float(tree.value[node, 0, 1]), "calibrationN": int(sum(found)),
                           "calibrationDraws": int(sum(y[np.array(cal)[found]])), "calibrationDrawRate": float(np.mean(y[np.array(cal)[found]])) if any(found) else None})
            return
        j = tree.feature[node]
        threshold = float(tree.threshold[node] * scaler.scale_[j] + scaler.mean_[j])
        visit(tree.children_left[node], path + [{"feature": str(names[j]), "operator": "<=", "value": threshold}])
        visit(tree.children_right[node], path + [{"feature": str(names[j]), "operator": ">", "value": threshold}])
    visit(0, [])
    return leaves


def paired_bootstrap(primary, market, repetitions=600):
    assert [r["matchId"] for r in primary] == [r["matchId"] for r in market]
    groups = defaultdict(list)
    for i, row in enumerate(primary):
        groups[row["day"]].append(i)
    dates = list(groups)
    if len(dates) < 2:
        return {"dates": len(dates), "reason": "不足两个评价日"}
    rng = np.random.default_rng(SEED)
    p = np.array([r["probability"] for r in primary]); m = np.array([r["probability"] for r in market]); y = np.array([r["y"] for r in primary])
    ps = np.array([r["selected"]["0.7"] for r in primary]); ms = np.array([r["selected"]["0.7"] for r in market])
    diffs = defaultdict(list)
    for _ in range(repetitions):
        ix = np.concatenate([groups[dates[i]] for i in rng.integers(0, len(dates), len(dates))])
        if not 0 < sum(y[ix]) < len(ix):
            continue
        diffs["APDifference"].append(float(average_precision_score(y[ix], p[ix]) - average_precision_score(y[ix], m[ix])))
        diffs["BrierDifference"].append(float(np.mean((p[ix] - y[ix]) ** 2) - np.mean((m[ix] - y[ix]) ** 2)))
        a, b = confusion(y[ix], ps[ix]), confusion(y[ix], ms[ix])
        if a["precision"] is not None and b["precision"] is not None:
            diffs["precisionAt70TargetDifference"].append(a["precision"] - b["precision"])
        diffs["recallAt70TargetDifference"].append(a["recall"] - b["recall"])
    return {"dates": len(dates), "repetitions": repetitions, "fewDatesWarning": len(dates) < 10,
            "pairedPercentile95": {k: np.quantile(v, [.025, .975]).tolist() for k, v in diffs.items() if v}}


def render_html(path, report):
    e = lambda value: html.escape(str(value))
    pct = lambda value: "暂无" if value is None else f"{value * 100:.2f}%"
    doc = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>全面平局特征研究</title><style>body{font:14px/1.65 system-ui,sans-serif;background:#101820;color:#e2eaf1;max-width:1850px;margin:auto;padding:24px}h2{margin-top:34px}table{border-collapse:collapse;width:100%}th,td{border:1px solid #3b4b59;padding:10px;text-align:left;vertical-align:top}th{background:#243846;position:sticky;top:0}.scroll{max-height:680px;overflow:auto}.note{background:#20384b;padding:16px}.warn{background:#493525;padding:16px}small{color:#b6c7d4}pre{white-space:pre-wrap;word-break:break-word}input,select{background:#22394a;color:white;border:1px solid #8aa1b3;padding:9px}a{color:#9bcbff}</style><h1>平局特征全面研究：覆盖率与误选率同时评估</h1>'''
    doc += '<p>' + e(f'确认完赛{report["dataset"]["n"]}场，平局{report["dataset"]["draws"]}场；提取{report["featureCount"]}列，结构化市场/严格赛前历史/前台派生分别研究。') + '</p>'
    doc += '<p class="note">命中率=选中平局/选中比赛；覆盖率=选中平局/评价区全部平局。50%、70%、80%是内层校准区的目标覆盖率，阈值原样用于后面的比赛，实际覆盖可能达不到目标。不是拿评价区结果倒算筛选阈值。</p>'
    doc += '<p class="warn">所有已有日期已在此前研究中查看，本轮是滚动内部验证。最早一段只作初始训练，没有伪造预测；全部970平用于描述性差异研究，模型成绩只使用时间向前的区外预测。赔率/分析的赛前捕获尚未获证明。禁止把全量训练拟合当成独立赛前成绩。</p>'
    doc += '<h2>数据与可评价范围</h2><pre>' + e(json.dumps(report["evaluationCoverage"], ensure_ascii=False, indent=2)) + '</pre><p>另外可查看 <a href="draw-subtypes.html">970场平局的0:0、1:1、2:2及高比分分型复盘</a>。实际比分类型仅用于复盘，赛前未知，不进入模型特征。</p>'
    doc += '<h2>全部模型及基准</h2><p>主模型只由前3个开发块选择；最后2个内部审计块单独披露。辅助模型包含前台历史/模拟/平台派生，来源与风险单列。</p><div class="scroll"><table><tr><th>模型</th><th>开发 AP / Brier</th><th>内部审计 AP / Brier</th><th>全部区外 AP / Brier</th></tr>'
    for m in report["models"]:
        doc += '<tr><td>' + e(m["label"]) + (' ★ 主模型' if m['id'] == report['primaryModelId'] else '') + '</td>'
        for part in ('development', 'audit', 'allTemporal'):
            s = m["pooled"][part]
            doc += '<td>' + e(f'AP {s["AP"]:.4f} / Brier {s["Brier"]:.4f}；{s["draws"]}/{s["n"]}') + '</td>'
        doc += '</tr>'
    doc += '</table></div><h2>覆盖50%、70%、80%平局的代价</h2><p>以下显示锁定内部审计区；每格包含被找回的平局、误选非平局与漏掉平局。可与同区均赔基准比较。</p><table><tr><th>模型</th><th>校准目标</th><th>筛出/总场次</th><th>平局/误选非平局/漏平</th><th>命中率</th><th>实际覆盖率</th><th>排除非平率</th></tr>'
    for key in ('market_raw', report['primaryModelId'], 'snapshot_hgb'):
        m = next(m for m in report['models'] if m['id'] == key)
        for target, s in m['pooled']['audit']['recallTargets'].items():
            doc += '<tr><td>' + e(m['label']) + '</td><td>' + pct(float(target)) + '</td><td>' + e(f'{s["selected"]}/{s["n"]}') + '</td><td>' + e(f'{s["TP"]} / {s["FP"]} / {s["FN"]}') + '</td><td>' + pct(s['precision']) + '</td><td>' + pct(s['recall']) + '</td><td>' + pct(s['specificity']) + '</td></tr>'
    doc += '</table><h2>全部970平与非平的特征差异</h2><p>这是快照的描述性差异，不是单场预测概率。分箱边界仅来自最早初始训练数据；缺值与有效0分别保留。搜索每项可查看来源、单位及正负样本的有效分母。</p><input id="featureSearch" placeholder="搜索特征或家族"><div class="scroll"><table><tr><th>特征</th><th>家族 / 视图</th><th>有效平 / 非平</th><th>平 / 非平中位数</th><th>描述性BH q</th><th>分箱</th></tr>'
    for f in report['featureDescriptions']:
        doc += '<tr data-feature="' + e(f['name'] + ' ' + f['label'] + ' ' + f['family']) + '"><td>' + e(f['label']) + '<br><small>' + e(f['name']) + '</small></td><td>' + e(f['family'] + ' / ' + f['view']) + '</td><td>' + e(f'{f["validDraws"]} / {f["validNonDraws"]}') + '</td><td>' + e(f'{f["drawMedian"]} / {f["nonDrawMedian"]}') + '</td><td>' + e(f'{f["descriptiveBHq"]:.4g}') + '</td><td><details><summary>查看分箱及覆盖</summary><pre>' + e(json.dumps(f['bins'], ensure_ascii=False, indent=2)) + '</pre></details></td></tr>'
    doc += '</table></div><h2>逐场区外预测及漏平明细</h2><input id="matchSearch" placeholder="比赛ID、联赛、球队"><select id="result"><option value="all">全部赛果</option><option value="draw">实际平局</option><option value="missed">70%目标下漏掉的平局</option><option value="false">误选的非平局</option></select><div class="scroll"><table id="matches"><tr><th>比赛</th><th>时间块</th><th>实际比分</th><th>模型分数</th><th>均赔分数</th><th>70%目标选择</th></tr>'
    for r in report['primaryPredictions']:
        state = 'hit' if r['y'] and r['selected']['0.7'] else 'missed' if r['y'] else 'false' if r['selected']['0.7'] else 'rejected'
        doc += '<tr data-state="' + state + '" data-draw="' + str(r['y']) + '"><td>' + e(r['matchId'] + ' ' + r['league'] + ' ' + r['home'] + '－' + r['guest']) + '</td><td>' + e(r['day'] + ' / ' + r['role']) + '</td><td>' + e(r['score']) + '</td><td>' + pct(r['probability']) + '</td><td>' + pct(r['marketProbability']) + '</td><td>' + e('选中' if r['selected']['0.7'] else '未选') + '</td></tr>'
    doc += '</table></div>'
    doc += '<h2>相同筛选数量的均赔对照</h2><p>下表在每个时间块按模型选中的场数，让均赔排序也取同样数量；边界并列全保留并另记增加场数。无需使用评价区实际赛果来选择比赛。</p><pre>' + e(json.dumps(next(m for m in report['models'] if m['id'] == report['primaryModelId'])['pooled']['audit']['marketAtSameSelectionCount'], ensure_ascii=False, indent=2)) + '</pre>'
    doc += '<p>可解释树在训练中位数填补后判断路径，缺失行也可能进入某一叶子；不能直接把叶子不等式复制为数据库过滤条件。</p>'
    for key, label in (('folds', '时间分区与结果可用缓冲'), ('marketBaseline', '同源完整均赔基准'), ('familyCoverage', '各家族有效覆盖'), ('permutationImportance', '开发区按家族置换影响'), ('interpretableTrees', '可解释树的原始单位条件（训练/校准观察）'), ('bootstrap', '审计区相对均赔的按日成对区间'), ('strata', '日期、联赛、数据缺失分层'), ('selection', '主选记录'), ('warnings', '计算警告'), ('sourceAudit', '来源与可用性审计')):
        doc += '<h2>' + label + '</h2><details><summary>展开</summary><pre>' + e(json.dumps(report[key], ensure_ascii=False, indent=2)) + '</pre></details>'
    doc += '<p>算法实现参考：<a href="https://scikit-learn.org/stable/modules/generated/sklearn.ensemble.HistGradientBoostingClassifier.html">scikit-learn浅提升树</a>、<a href="https://scikit-learn.org/stable/modules/model_evaluation.html#precision-recall-f-measure-metrics">精确率与召回率</a>。</p><script>document.querySelector("#featureSearch").addEventListener("input",e=>document.querySelectorAll("tr[data-feature]").forEach(r=>r.hidden=!r.dataset.feature.toLowerCase().includes(e.target.value.trim().toLowerCase())));const m=document.querySelector("#matchSearch"),s=document.querySelector("#result");function filter(){document.querySelectorAll("#matches tr[data-state]").forEach(r=>r.hidden=!r.textContent.includes(m.value.trim())||(s.value==="draw"&&r.dataset.draw!=="1")||(["missed","false"].includes(s.value)&&r.dataset.state!==s.value));}m.addEventListener("input",filter);s.addEventListener("change",filter);</script></html>'
    path.write_text(doc, encoding="utf-8")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--out', type=Path, default=ROOT / 'reports/draw-full-feature-study')
    args = parser.parse_args(); folder = args.out
    protocol = read(folder / 'protocol.json'); raw = read(folder / 'dataset.json')
    source_hash = digest(Path(__file__))
    extractor_hash = digest(ROOT / 'scripts/draw_full_features.py')
    protocol_hash = digest(folder / 'protocol.json')
    assert digest(ROOT / 'reports/draw-goals-heat-study/dataset.json') == protocol['priorDatasetSHA256']
    rows = sorted(raw['rows'], key=lambda r: (r['matchTime'], str(r['matchId'])))
    for r in rows:
        r['_kickoff'] = datetime.fromisoformat(r['matchTime'].replace('Z', '+00:00'))
        r['_day'] = r['_kickoff'].date().isoformat()
    y = np.array([int(r['result'] == 1) for r in rows])
    assert all(r['result'] == (1 if r['homeScore'] == r['awayScore'] else 0 if r['homeScore'] > r['awayScore'] else 2) for r in rows)
    assert len({r['matchId'] for r in rows}) == len(rows)
    X, metadata = build_features(rows)
    assert X.shape == (len(rows), len(metadata)) and not np.isinf(X).any()
    # Share the exact complete-average field with the learned models. Average
    # availability is independent of whether a William-minus-average
    # comparison can be computed; missing William is not missing average.
    market_column = next(i for i, m in enumerate(metadata) if m['name'] == 'euro.average.latest.prob.draw')
    market = X[:, market_column].copy()
    assert np.all((market[np.isfinite(market)] > 0) & (market[np.isfinite(market)] < 1))
    folds = make_folds(rows, protocol)
    initial = [i for i, r in enumerate(rows) if r['_day'] < folds[0]['firstTestDay']]
    write(folder / 'feature-manifest.json', metadata)
    write(folder / 'folds.json', [{**f, 'matchIds': {k: [rows[i]['matchId'] for i in f[k]] for k in ('train','calibration','test')}} for f in folds])
    all_records, reports, fitted = {}, [], {}
    warning_log = []
    with threadpool_limits(limits=4):
        for spec in model_specs():
            records, fold_reports = [], []
            for fold in folds:
                ti, ci, vi = (fold[k] for k in ('train', 'calibration', 'test'))
                with warnings.catch_warnings(record=True) as seen:
                    warnings.simplefilter('always')
                    model = Fitted(spec, X, y, ti, metadata)
                    pc, pv = model.raw(X, ci, market), model.raw(X, vi, market)
                    calibrator = Calibrator(y[ci], pc, enabled=spec.kind not in ('constant','market'))
                    pc, pv = calibrator.apply(pc), calibrator.apply(pv)
                warning_log.extend({'model':spec.id,'fold':fold['fold'],'warning':str(w.message)} for w in seen)
                thresholds = {str(t): recall_threshold(y[ci], pc, t) for t in RECALLS}
                top = {str(t): top_flags(pv, math.ceil(len(vi)*t)) for t in TOPS}
                fold_reports.append({'fold':fold['fold'],'role':fold['role'],'trainN':len(ti),'calibrationN':len(ci),'columns':len(model.cols),
                                     'columnNames':[metadata[j]['name'] for j in model.cols], 'metrics':performance(y[vi],pv), 'thresholds':thresholds,
                                     'calibrationTargets':{str(t):confusion(y[ci],pc>=thresholds[str(t)]) for t in RECALLS}})
                for n, i in enumerate(vi):
                    records.append({'matchId':rows[i]['matchId'],'rowIndex':i,'fold':fold['fold'],'role':fold['role'],'day':rows[i]['_day'],
                                    'y':int(y[i]),'probability':float(pv[n]),'selected':{str(t):bool(pv[n]>=thresholds[str(t)]) for t in RECALLS},
                                    'top':{str(t):bool(top[str(t)][n]) for t in TOPS}})
                fitted[(spec.id,fold['fold'])]=(model,calibrator)
            all_records[spec.id]=records
            reports.append({'id':spec.id,'label':spec.label,'primaryEligible':spec.primary,'kind':spec.kind,'views':spec.views,'excludedFamilies':spec.excluded,
                            'folds':fold_reports,'pooled':{k:pooled([r for r in records if k=='allTemporal' or r['role']==('development' if k=='development' else 'lockedInternalAudit')]) for k in ('development','audit','allTemporal')}})
            print('completed',spec.id,'devAP',round(reports[-1]['pooled']['development']['AP'],4),'auditAP',round(reports[-1]['pooled']['audit']['AP'],4),flush=True)
        for model_report in reports:
            comparator = equal_selection_comparison(all_records[model_report['id']], all_records['market_raw'])
            for part in ('development','audit','allTemporal'):
                model_report['pooled'][part]['marketAtSameSelectionCount'] = comparator[part]
        primary_id=choose_primary(reports)
        # Primary is frozen here before computing interpretation/audit intervals.
        primary_records=all_records[primary_id]; market_records=all_records['market_raw']
        importance=defaultdict(list)
        families=sorted({m['family'] for m in metadata if m['view'] in ('market','safeHistory')})
        rng=np.random.default_rng(SEED)
        for fold in folds[:3]:
            model,calibrator=fitted[(primary_id,fold['fold'])]; vi=fold['test']
            baseline=average_precision_score(y[vi],calibrator.apply(model.raw(X,vi,market)))
            for family in families:
                cols=[j for j in model.cols if metadata[j]['family']==family]
                if not cols: continue
                for repeat in range(3):
                    shuffled=X.copy(); order=rng.permutation(vi); shuffled[np.ix_(vi,cols)]=X[np.ix_(order,cols)]
                    value=average_precision_score(y[vi],calibrator.apply(model.raw(shuffled,vi,market)))
                    importance[family].append(float(baseline-value))
    evaluated_ids={r['matchId'] for r in primary_records}; warmup=[r for r in rows if r['matchId'] not in evaluated_ids]
    feature_descriptions=describe_features(X,y,metadata,initial)
    family_coverage=[]
    for family in sorted({m['family'] for m in metadata}):
        cols=[j for j,m in enumerate(metadata) if m['family']==family]
        any_valid=np.isfinite(X[:,cols]).any(axis=1)
        family_coverage.append({'family':family,'columns':len(cols),'rowsWithAnyInput':int(sum(any_valid)),'drawsWithAnyInput':int(sum(any_valid & (y==1)))})
    primary_predictions=[]
    market_by_id={r['matchId']:r for r in market_records}
    for r in primary_records:
        original=rows[r['rowIndex']]
        primary_predictions.append({**r,'league':original['league'],'home':original['home'],'guest':original['guest'],
                                    'score':f'{original["homeScore"]}:{original["awayScore"]}','marketProbability':market_by_id[r['matchId']]['probability']})
    audit_primary=[r for r in primary_records if r['role']=='lockedInternalAudit'];audit_market=[r for r in market_records if r['role']=='lockedInternalAudit']
    strata_report={}
    for field in ('league','day','inputCoverage'):
        groups=defaultdict(list)
        for r in audit_primary:
            original=rows[r['rowIndex']]
            key=original['league'] if field=='league' else r['day'] if field=='day' else '输入较全' if np.mean(np.isfinite(X[r['rowIndex']]))>=.5 else '输入较少'
            groups[key].append(r)
        strata_report[field]=[{"group":k,**pooled(v)} for k,v in sorted(groups.items())]
    primary_report=next(m for m in reports if m['id']==primary_id)
    baseline_report=next(m for m in reports if m['id']=='market_raw')
    trees=[{'fold':f['fold'],'role':f['role'],'leaves':readable_tree(fitted[('clean_tree3',f['fold'])][0],X,y,f['train'],f['calibration'],metadata)} for f in folds]
    assert source_hash == digest(Path(__file__)) and extractor_hash == digest(ROOT/'scripts/draw_full_features.py') and protocol_hash == digest(folder/'protocol.json'), 'Source changed during run; rerun before reporting.'
    report={
        'version':'draw-full-features-v1','generatedAt':raw.get('generatedAt'),'datasetSHA256':digest(folder/'dataset.json'),
        'scriptSHA256':source_hash,'extractorSHA256':extractor_hash,'protocolSHA256':protocol_hash,
        'featureMatrixSHA256':hashlib.sha256(np.nan_to_num(X,nan=-9.87654321e99).astype('<f8').tobytes()).hexdigest(),
        'environment':{'python':sys.version.split()[0],'numpy':np.__version__,'scipy':scipy.__version__,'sklearn':sklearn.__version__},
        'dataset':{'n':len(rows),'draws':int(sum(y)),'nonDraws':int(sum(y==0)),'rate':float(np.mean(y))},
        'featureCount':len(metadata),'featureViews':dict(Counter(m['view'] for m in metadata)),'featureDescriptions':feature_descriptions,'familyCoverage':family_coverage,
        'marketBaseline':{'feature':metadata[market_column],'validRows':int(sum(np.isfinite(market))),'validDraws':int(sum(np.isfinite(market) & (y==1))),
                          'missingFallback':'inner training draw prevalence, never uniform33/34/33 or test outcomes'},
        'models':reports,'primaryModelId':primary_id,
        'selection':{'method':'mean AP of first3 development blocks; then lower mean Brier, stable ID','usesAuditForSelection':False,
                     'developmentRanking':[m['id'] for m in sorted([r for r in reports if r['primaryEligible']],key=lambda r:(-np.mean([f['metrics']['AP'] for f in r['folds'][:3]]),np.mean([f['metrics']['Brier'] for f in r['folds'][:3]]),r['id']))]},
        'evaluationCoverage':{'allCompleted':len(rows),'allDraws':int(sum(y)),'temporallyEvaluated':len(primary_records),'evaluatedDraws':sum(r['y'] for r in primary_records),
                              'warmupNotScored':len(warmup),'warmupDraws':sum(r['result']==1 for r in warmup),'auditMatches':len(audit_primary),'auditDraws':sum(r['y'] for r in audit_primary),'independentProspectiveMatches':0},
        'folds':[{k:v for k,v in f.items() if k not in ('train','calibration','test')}|{k+'N':len(f[k]) for k in ('train','calibration','test')} for f in folds],
        'primaryPredictions':primary_predictions,'permutationImportance':[{'family':k,'meanDevelopmentAPDrop':float(np.mean(v)),'values':v} for k,v in sorted(importance.items(),key=lambda kv:-np.mean(kv[1]))],
        'bootstrap':paired_bootstrap(audit_primary,audit_market),'strata':strata_report,'interpretableTrees':trees,'sourceAudit':raw.get('audit'),'warnings':warning_log,
        'auditComparison':{'primary':primary_report['pooled']['audit'],'market':baseline_report['pooled']['audit']}}
    write(folder/'analysis.json',report)
    render_html(folder/'report.html',report)
    write(folder/'summary.json',{k:report[k] for k in ('dataset','featureCount','featureViews','primaryModelId','selection','evaluationCoverage','auditComparison','bootstrap','environment')})
    with (folder/'predictions.csv').open('w',encoding='utf-8-sig',newline='') as stream:
        fields=['model','matchId','fold','role','day','y','probability']+[f'selected_{t}' for t in RECALLS]
        writer=csv.DictWriter(stream,fieldnames=fields);writer.writeheader()
        for model,records in all_records.items():
            for r in records:writer.writerow({'model':model,**{k:r[k] for k in fields if k in r},**{f'selected_{t}':int(r['selected'][str(t)]) for t in RECALLS}})
    with (folder/'features.csv').open('w',encoding='utf-8-sig',newline='') as stream:
        writer=csv.writer(stream);writer.writerow(['matchId','resultLabel']+[m['name'] for m in metadata])
        for i,r in enumerate(rows):writer.writerow([r['matchId'],r['result']]+[float(v) if np.isfinite(v) else '' for v in X[i]])
    with (folder/'feature-bins.csv').open('w',encoding='utf-8-sig',newline='') as stream:
        fields=['name','label','family','view','validDraws','validNonDraws','drawMedian','nonDrawMedian','descriptiveBHq','bins']
        writer=csv.DictWriter(stream,fieldnames=fields,extrasaction='ignore');writer.writeheader()
        for f in feature_descriptions:writer.writerow({**f,'bins':json.dumps(clean_json(f['bins']),ensure_ascii=False)})
    print('primary',primary_id,'auditAP',report['auditComparison']['primary']['AP'],'marketAuditAP',report['auditComparison']['market']['AP'],flush=True)


if __name__=='__main__':
    main()
