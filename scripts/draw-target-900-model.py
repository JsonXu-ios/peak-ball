#!/usr/bin/env python3
"""Finite, explicitly in-sample 900-draw capture search on frozen local exports.

No DB, Redis, services or network. All 4161 labels are deliberately used for
fitting and threshold selection: the output is a historical fit, not validation.
"""
from __future__ import annotations

import os
os.environ.setdefault("OMP_NUM_THREADS", "4")
os.environ.setdefault("OPENBLAS_NUM_THREADS", "4")
os.environ.setdefault("MKL_NUM_THREADS", "4")

import hashlib
import importlib.util
import json
import math
import re
import sys
import time
from collections import Counter
from pathlib import Path

import joblib
import numpy as np
import sklearn
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.impute import SimpleImputer
from sklearn.metrics import average_precision_score, brier_score_loss
from threadpoolctl import threadpool_limits

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-target-900-study"
HIST = ROOT / "reports/draw-historical-value-study"
sys.dont_write_bytecode = True


def sha(p):
    return hashlib.sha256(Path(p).read_bytes()).hexdigest()


def write(p, x):
    Path(p).write_text(json.dumps(x, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def count(y, chosen):
    tp, fp = int(np.sum(y & chosen)), int(np.sum(~y & chosen))
    return {"selected": tp + fp, "TP": tp, "FP": fp, "FN": int(y.sum()) - tp,
            "TN": int((~y).sum()) - fp, "precision": tp / (tp + fp) if tp + fp else None,
            "recall": tp / int(y.sum()), "targetReached": tp >= 900 and tp / (tp + fp) >= .30}


def threshold_at_900(y, scores):
    threshold = float(np.sort(scores[y])[::-1][899])
    chosen = scores >= threshold
    return {"threshold": threshold, "tiesAtThreshold": int(np.sum(scores == threshold)),
            "thresholdRule": "highest score threshold retaining >=900 actual draws; include ALL ties",
            **count(y, chosen)}, chosen


def same_count(y, scores, target):
    threshold = float(np.sort(scores)[::-1][target - 1])
    chosen = scores >= threshold
    return {"requestedCount": target, "threshold": threshold,
            "tiesAtThreshold": int(np.sum(scores == threshold)), **count(y, chosen)}


def exclusion(meta):
    name = meta["name"]
    lower = name.lower()
    if name.startswith("availability.") or "ambiguous" in lower or "missing" in lower:
        return "availability/missing/ambiguity flag"
    if meta.get("denominatorOnly") or name.endswith(".n") or re.search(r"\.(recentHomeMatches|recentGuestMatches|homeSamples|awaySamples|matches|sampleCount|samples)$", name, re.I):
        return "sample count used only for qualification"
    if any(x in lower for x in ("matchid", "kickoff", "timestamp", "teamid", "resultlabel")):
        return "identifier/time/outcome"
    return None


def historical_guard(X, columns, name):
    if name.startswith("safeRecent.home.") or name.startswith("safeRecent.away."):
        parts = name.split(".")
        key = ".".join(parts[:3]) + ".n"
        return X[:, columns[key]] >= 5
    if name.startswith("safeH2H."):
        key = ".".join(name.split(".")[:2]) + ".n"
        return X[:, columns[key]] >= 1
    if name.startswith("safeRecent.pair."):
        return (X[:, columns["safeRecent.home.10.n"]] >= 5) & (X[:, columns["safeRecent.away.10.n"]] >= 5)
    if name.startswith("conflict.recent."):
        side = name.split(".")[2]
        window = "3" if name.split(".")[3] == "3" else "10"
        key = f"safeRecent.{side}.{window}.n" if window == "10" else f"conflict.recent.{side}.3.n"
        return X[:, columns[key]] >= (3 if window == "3" else 5)
    return np.ones(X.shape[0], dtype=bool)


def record_group(name):
    if name.startswith("euro."):
        return "公司欧赔原值及其去水概率/调整"
    if name.startswith(("difference.", "conflict.difference.")):
        return "公司间报价差值及选项排序"
    if name.startswith("kelly."):
        return "凯利值"
    if name.startswith("line."):
        return "亚盘线及水位" if ".asianHandicap." in name else "大小球线及水位"
    if name.startswith("safeH2H."):
        return "赛前历史交锋结果与进失球"
    if name.startswith("context.schedule."):
        return "历史记录可知休息天数与赛程场次"
    if name.startswith("context.form.") or name.startswith("conflict.recent."):
        return "近况积分与连胜不败"
    if name.startswith("context.goals.") or name.startswith("safeRecent.") and any(s in name for s in ("goals", "Goals", "goalDifference", "under25", "zeroZero", "oneOne", "cleanSheet", "scoreless")):
        return "近期比赛进球失球和比分结构"
    if name.startswith("safeRecent."):
        return "近期比赛胜平负次数及比率"
    if name.startswith("context.rank."):
        return "原始排名快照"
    if name.startswith(("profit.", "marketProfit.", "trade.")):
        return "来源投注比例与盈亏计算"
    if name.startswith(("heat.", "retail.")):
        return "本地计算热度及散户模拟"
    return "其他前台快照派生"


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    helper_path = ROOT / "scripts/draw-historical-value-study.py"
    spec = importlib.util.spec_from_file_location("historical_900_inputs", helper_path)
    helper = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = helper
    spec.loader.exec_module(helper)
    rows, X, labels, meta, sources = helper.load_inputs(HIST)
    y = labels == 1
    ids = np.asarray([str(r["matchId"]) for r in rows])
    assert X.shape == (4161, 530) and int(y.sum()) == 970
    sources[str(helper_path.relative_to(ROOT))] = sha(helper_path)
    sources[str(Path(__file__).relative_to(ROOT))] = sha(__file__)
    names = [m["name"] for m in meta]
    columns = {name: i for i, name in enumerate(names)}
    guarded = X.copy()
    guards = []
    for j, m in enumerate(meta):
        valid = historical_guard(X, columns, m["name"])
        removed = int(np.sum(np.isfinite(guarded[:, j]) & ~valid))
        guarded[~valid, j] = np.nan
        if removed:
            guards.append({"feature": m["name"], "valuesRemovedForInsufficientHistory": removed})
    excluded = [{"feature": m["name"], "reason": exclusion(m)} for m in meta if exclusion(m)]
    selection = {}
    for view in ("clean", "all", "historyOnly"):
        chosen = []
        for j, m in enumerate(meta):
            if exclusion(m) or (view == "clean" and m["view"] not in ("market", "safeHistory")):
                continue
            if view == "historyOnly" and m["view"] != "safeHistory":
                continue
            finite = guarded[np.isfinite(guarded[:, j]), j]
            if len(finite) >= 40 and np.ptp(finite) > 0:
                chosen.append(j)
        selection[view] = chosen
    market_raw = X[:, columns["euro.average.latest.prob.draw"]]
    market = np.where(np.isfinite(market_raw), market_raw, float(y.mean()))
    configs = [
        ("clean_d2_l60_i120", "clean", 2, 60, 120),
        ("clean_d2_l60_i240", "clean", 2, 60, 240),
        ("clean_d3_l100_i120", "clean", 3, 100, 120),
        ("clean_d3_l60_i120", "clean", 3, 60, 120),
        ("clean_d3_l60_i240", "clean", 3, 60, 240),
        ("clean_d3_l40_i240", "clean", 3, 40, 240),
        ("all_d2_l60_i120", "all", 2, 60, 120),
        ("all_d3_l60_i240", "all", 3, 60, 240),
        ("historyOnly_d3_l60_i240", "historyOnly", 3, 60, 240),
    ]
    protocol = {"purpose": "historical finite-complexity fit; not validation or a deployable prediction result",
                "targetDraws": 900, "targetPrecision": .30, "atExactly900MaximumFP": 2100,
                "configsFixedBeforeFit": [dict(id=c[0], view=c[1], depth=c[2], minLeaf=c[3], iterations=c[4]) for c in configs],
                "configHistory": "Original eight fixed configurations all retained; one additional fixed history-only control requested after the first batch; no selective removal of unsuccessful fits",
                "allRowsAndLabelsUsedForFittingAndThresholds": True, "independentTestRows": 0,
                "imputation": "full-history medians; NO missing indicators; counts only history qualification",
                "marketMissingFallback": "full-history draw prior; missing quote is not falsely reported as known odds",
                "captureTimeKnownPrematch": False,
                "missingnessCaveat": "Median imputation removes dedicated missing flags but cannot prove absence of every indirect missingness or source-selection association."}
    write(OUT / "model-protocol.json", protocol)
    scores = {"matchIds": ids, "labels": labels, "market": market}
    results = []
    trained = {}
    (OUT / "models").mkdir(exist_ok=True)
    with threadpool_limits(limits=4):
        for ident, view, depth, leaf, iterations in configs:
            started = time.perf_counter()
            cols = selection[view]
            transform = SimpleImputer(strategy="median", add_indicator=False)
            data = transform.fit_transform(guarded[:, cols])
            model = HistGradientBoostingClassifier(max_iter=iterations, max_depth=depth,
                        max_leaf_nodes=2 ** depth, min_samples_leaf=leaf, l2_regularization=10,
                        learning_rate=.05, max_bins=32, early_stopping=False, random_state=20261025)
            model.fit(data, y)
            p = model.predict_proba(data)[:, 1]
            metrics, mask = threshold_at_900(y, p)
            scores[ident] = p
            scores[ident + "_selected"] = mask
            gains, splits = {}, {}
            leaf_count = 0
            for stage in model._predictors:
                for tree in stage:
                    for node in tree.nodes:
                        if bool(node["is_leaf"]):
                            leaf_count += 1
                        else:
                            original_col = cols[int(node["feature_idx"])]
                            gains[original_col] = gains.get(original_col, 0.) + max(0., float(node["gain"]))
                            splits[original_col] = splits.get(original_col, 0) + 1
            top = sorted(gains, key=lambda j: (-gains[j], names[j]))
            total_gain = sum(gains.values())
            item = {"id": ident, "view": view, "fitType": "all_labels_in_sample_only",
                    "parameters": model.get_params(), "inputFeatureCount": len(cols),
                    "inputFeatureNames": [names[j] for j in cols], "uniqueSplitFeatures": len(gains),
                    "trainedTrees": int(model.n_iter_), "totalLeaves": leaf_count,
                    "maxLeavesPerTree": 2 ** depth, "numberOfMedianIndicators": 0,
                    "seconds": time.perf_counter() - started, "at900": metrics,
                    "inSampleBrier": float(brier_score_loss(y, p)),
                    "inSampleAP": float(average_precision_score(y, p)),
                    "marketSameSelectedCount": same_count(y, market, metrics["selected"]),
                    "selectedMatchIds": ids[mask].tolist(),
                    "topSplitGainFeaturesDescriptiveOnly": [{"name": names[j], "label": meta[j]["label"],
                        "view": meta[j]["view"], "source": meta[j].get("source"), "splitCount": splits[j],
                        "gainShare": gains[j] / total_gain if total_gain else 0.} for j in top[:30]]}
            artifact = OUT / "models" / f"{ident}.joblib"
            joblib.dump({"model": model, "imputer": transform, "inputColumns": cols,
                         "inputFeatureNames": [names[j] for j in cols], "historicalQualification": guards,
                         "threshold": metrics["threshold"], "fitType": "all_labels_in_sample_only"}, artifact)
            item["modelArtifact"] = str(artifact.relative_to(ROOT))
            item["modelArtifactSHA256"] = sha(artifact)
            item["inputRecordGroups"] = dict(Counter(record_group(names[j]) for j in cols))
            trained[ident] = (model, data, cols)
            results.append(item)
            print(ident, metrics, "seconds", round(item["seconds"], 2), flush=True)
    market_metrics, market_mask = threshold_at_900(y, market)
    scores["market_selected"] = market_mask
    np.savez_compressed(OUT / "model-scores.npz", **scores)
    permutations = []
    for view in ("clean", "historyOnly"):
        eligible = [r for r in results if r["view"] == view]
        best = min(eligible, key=lambda r: (r["at900"]["FP"], r["id"]))
        model, data, cols = trained[best["id"]]
        groups = {}
        for local, original in enumerate(cols):
            groups.setdefault(record_group(names[original]), []).append(local)
        rng = np.random.default_rng(20261025)
        base_ap = best["inSampleAP"]
        group_results = []
        with threadpool_limits(limits=4):
            for group, group_cols in sorted(groups.items()):
                drops = []
                for _ in range(3):
                    shuffled = data.copy()
                    shuffled[:, group_cols] = data[rng.permutation(len(y))][:, group_cols]
                    drops.append(base_ap - float(average_precision_score(y, model.predict_proba(shuffled)[:, 1])))
                group_results.append({"group": group, "featureCount": len(group_cols),
                                      "meanInSampleAPDrop": float(np.mean(drops)), "all3Drops": drops})
        permutations.append({"modelId": best["id"], "baselineInSampleAP": base_ap,
                             "interpretation": "Training-only group permutation association; correlated groups can hide or redistribute importance; no causal or future claim",
                             "groups": sorted(group_results, key=lambda g: -g["meanInSampleAPDrop"])})
    primary_winner = min([r for r in results if r["view"] == "clean"], key=lambda r: (r["at900"]["FP"], r["id"]))
    winner_model, winner_data, winner_cols = trained[primary_winner["id"]]
    control_results, control_scores = [], {"matchIds": ids}
    with threadpool_limits(limits=4):
        for seed in [20261025, 20261026, 20261027, 20261028, 20261029]:
            randomized_y = np.random.default_rng(seed).permutation(y)
            sham = HistGradientBoostingClassifier(**winner_model.get_params())
            sham.fit(winner_data, randomized_y)
            sham_scores = sham.predict_proba(winner_data)[:, 1]
            sham_metrics, sham_mask = threshold_at_900(randomized_y, sham_scores)
            control_results.append({"permutationSeed": seed, "positiveLabels": int(randomized_y.sum()),
                "at900AgainstPermutedOwnFitLabels": sham_metrics,
                "inSampleBrierAgainstPermutedOwnFitLabels": float(brier_score_loss(randomized_y, sham_scores)),
                "inSampleAPAgainstPermutedOwnFitLabels": float(average_precision_score(randomized_y, sham_scores))})
            control_scores[f"seed{seed}_labels"] = randomized_y
            control_scores[f"seed{seed}_scores"] = sham_scores
            control_scores[f"seed{seed}_selected"] = sham_mask
            print("random_label_control", seed, sham_metrics, flush=True)
    np.savez_compressed(OUT / "permutation-control-scores.npz", **control_scores)
    control = {"purpose": "Negative capacity diagnostic, NOT a formal permutation significance test or validation",
        "winnerSelectionBias": "Winner was chosen using original full-history results; no reselecting configs within permuted controls",
        "modelId": primary_winner["id"], "parameters": winner_model.get_params(),
        "inputFeatureNames": [names[j] for j in winner_cols],
        "evaluationLabels": "Each fit evaluated against its OWN shuffled training labels, never against the true labels",
        "actualLabelsPrimaryWinner": primary_winner["at900"], "controls": control_results,
        "seedsMeetingTarget": int(sum(c["at900AgainstPermutedOwnFitLabels"]["targetReached"] for c in control_results)),
        "controlPrecisionRange": [min(c["at900AgainstPermutedOwnFitLabels"]["precision"] for c in control_results), max(c["at900AgainstPermutedOwnFitLabels"]["precision"] for c in control_results)],
        "controlFPRange": [min(c["at900AgainstPermutedOwnFitLabels"]["FP"] for c in control_results), max(c["at900AgainstPermutedOwnFitLabels"]["FP"] for c in control_results)],
        "sourceSHA256": sources, "scoresSHA256": sha(OUT / "permutation-control-scores.npz")}
    write(OUT / "permutation-control.json", control)
    output = {"protocol": protocol, "sourceSHA256": sources, "datasetRows": len(y), "draws": int(y.sum()),
              "featureMatrixShape": list(X.shape), "sourceFeatureMatrixCanonicalSHA256":
                  hashlib.sha256(np.nan_to_num(X, nan=-9.87654321e99).astype("<f8").tobytes()).hexdigest(),
              "guardedFeatureMatrixCanonicalSHA256":
                  hashlib.sha256(np.nan_to_num(guarded, nan=-9.87654321e99).astype("<f8").tobytes()).hexdigest(),
              "numpy": np.__version__, "sklearn": sklearn.__version__, "excludedInputFeatures": excluded,
              "historyQualificationChanges": guards, "models": results,
              "fieldStatistics": [{"name": m["name"], "label": m["label"], "family": m.get("family"),
                "view": m["view"], "recordGroup": record_group(m["name"]),
                "rawFiniteRows": int(np.isfinite(X[:, j]).sum()), "rawFiniteRate": float(np.isfinite(X[:, j]).mean()),
                "qualifiedFiniteRows": int(np.isfinite(guarded[:, j]).sum()), "qualifiedFiniteRate": float(np.isfinite(guarded[:, j]).mean()),
                "usedInViews": [v for v in selection if j in selection[v]], "excludedReason": exclusion(m)} for j, m in enumerate(meta)],
              "familyStatistics": [{"family": family, "fields": len(js),
                "qualifiedFiniteCells": int(np.isfinite(guarded[:, js]).sum()),
                "qualifiedFiniteRate": float(np.isfinite(guarded[:, js]).mean())}
                for family, js in sorted({family: [j for j, m in enumerate(meta) if m.get("family", "unknown") == family]
                    for family in {m.get("family", "unknown") for m in meta}}.items())],
              "groupPermutationTrainingOnly": permutations,
              "randomLabelCapacityControl": {"file": "permutation-control.json", "seedsMeetingTarget": control["seedsMeetingTarget"],
                 "controlPrecisionRange": control["controlPrecisionRange"], "controlFPRange": control["controlFPRange"]},
              "market": {"at900": market_metrics, "knownQuoteRows": int(np.isfinite(market_raw).sum()),
                         "inSampleBrier": float(brier_score_loss(y, market)), "inSampleAP": float(average_precision_score(y, market))},
              "scoresSHA256": sha(OUT / "model-scores.npz")}
    write(OUT / "models.json", output)
    print(json.dumps({"finished": len(results), "modelsFile": str(OUT / "models.json"), "market": market_metrics}), flush=True)


if __name__ == "__main__":
    main()
