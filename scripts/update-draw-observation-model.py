#!/usr/bin/env python3
"""Refit the original draw family on the latest confirmed local training rows.

No database/network/service access. Output is a candidate, never auto-published.
The Go startup coordinator validates, saves atomically and activates it.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
from datetime import datetime, timezone
import warnings

for key in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
    os.environ.setdefault(key, "4")

import numpy as np
from sklearn.ensemble import HistGradientBoostingClassifier
import sklearn
from threadpoolctl import threadpool_limits

ROOT = Path(__file__).resolve().parents[1]
THRESHOLD = 0.2016984853563883
PARAMETERS = dict(max_iter=240, max_depth=3, max_leaf_nodes=8,
                  min_samples_leaf=40, l2_regularization=10,
                  learning_rate=.05, max_bins=32, early_stopping=False,
                  random_state=20261025)

def recipe_hash():
    h = hashlib.sha256()
    for name in ("update-draw-observation-model.py", "draw-runtime-requirements.txt"):
        h.update(name.encode()); h.update(b"\0"); h.update((ROOT / "scripts" / name).read_bytes())
    return h.hexdigest()

def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False) + "\n", encoding="utf-8")

def guarded_matrix(rows, features):
    values = np.empty((len(rows), len(features)))
    for i, row in enumerate(rows):
        for j, feature in enumerate(features):
            data = row["features"]
            value = data.get(feature["name"], np.nan)
            if not all(data.get(g["feature"], -np.inf) >= g["minimum"] for g in feature.get("guards") or []):
                value = np.nan
            values[i, j] = value
    values[~np.isfinite(values)] = np.nan
    return values

def impute(values, medians):
    return np.where(np.isfinite(values), values, medians)

def portable_prediction(values, artifact):
    x = impute(values, np.array([f["median"] for f in artifact["features"]]))
    logits = np.full(len(x), artifact["baseline"])
    for tree in artifact["trees"]:
        positions = np.zeros(len(x), dtype=int)
        done = np.zeros(len(x), dtype=bool)
        for _ in range(32):
            if done.all(): break
            for index in np.unique(positions[~done]):
                ids = np.where((positions == index) & ~done)[0]
                node = tree[index]
                if node["leaf"]:
                    logits[ids] += node["value"]; done[ids] = True
                else:
                    positions[ids] = np.where(x[ids, node["feature"]] <= node["threshold"], node["left"], node["right"])
        if not done.all(): raise ValueError("tree traversal failed")
    return 1 / (1 + np.exp(-logits))

def fit(values, labels, features):
    with warnings.catch_warnings():
        warnings.simplefilter("ignore", RuntimeWarning)
        medians = np.nanmedian(values, axis=0)
    medians[~np.isfinite(medians)] = 0
    model = HistGradientBoostingClassifier(**PARAMETERS)
    with threadpool_limits(limits=4):
        model.fit(impute(values, medians), labels)
    trees, used = [], set()
    for predictor in model._predictors:
        tree = []
        for node in predictor[0].nodes:
            if node["is_categorical"]: raise ValueError("unexpected categorical split")
            if node["is_leaf"]: tree.append(dict(leaf=True, value=float(node["value"])))
            else:
                j = int(node["feature_idx"]); used.add(j)
                tree.append(dict(leaf=False, feature=j, threshold=float(node["num_threshold"]), left=int(node["left"]), right=int(node["right"])))
        trees.append(tree)
    exported = dict(schema=1, modelId="clean_d3_l40_i240", threshold=THRESHOLD,
        baseline=float(model._baseline_prediction[0, 0]), trees=trees,
        features=[dict(f, median=float(medians[j]), usedInTrees=j in used) for j, f in enumerate(features)])
    return model, exported

def observed(values, artifact):
    used = [i for i, f in enumerate(artifact["features"]) if f["usedInTrees"]]
    return np.any(np.isfinite(values[:, used]), axis=1)

def metrics(rows, scores, available):
    labels = np.array([r["result"] for r in rows])
    jc = np.array([bool(r["jingcaiId"].strip()) for r in rows])
    eligible = np.array([r["eligible"] for r in rows])
    alternatives = np.array([r["alternative"] for r in rows])
    selected = jc & eligible & available & (scores >= THRESHOLD)
    n, total = int(selected.sum()), int(jc.sum())
    hits = int(np.sum(selected & (labels == 1)))
    alt = selected & (alternatives >= 0)
    alt_hits = int(np.sum(alt & (labels == alternatives)))
    return dict(matches=total, selected=n, drawHits=hits,
        drawHitRate=hits / n if n else None, selectionCoverage=n / total if total else None,
        drawRecall=hits / int(np.sum(jc & (labels == 1))) if np.any(jc & (labels == 1)) else None,
        alternativeAvailable=int(alt.sum()), alternativeHits=alt_hits,
        alternativeHitRate=alt_hits / int(alt.sum()) if alt.any() else None,
        eitherHits=hits + alt_hits, eitherHitRate=(hits + alt_hits) / n if n else None)

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--input", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)
    p.add_argument("--report", type=Path)
    args = p.parse_args()
    source = json.loads(args.input.read_text(encoding="utf-8-sig"))
    rows, features = source["rows"], source["features"]
    if len(rows) < 100 or len(features) != 391 or len({r["matchId"] for r in rows}) != len(rows):
        raise ValueError("invalid training sample/feature contract")
    if set(r["result"] for r in rows) - {0, 1, 2}: raise ValueError("unconfirmed label")
    labels = np.array([r["result"] == 1 for r in rows])
    if min(labels.sum(), (~labels).sum()) < 20: raise ValueError("insufficient class support")
    values = guarded_matrix(rows, features)
    baseline = json.loads((ROOT / "go_server/internal/drawmodeldata/observation_v1.json").read_text(encoding="utf-8"))
    old_scores = portable_prediction(values, baseline)
    if np.max(np.abs(old_scores - np.array([r["baselineScore"] for r in rows]))) > 1e-11:
        raise ValueError("baseline feature/guard parity mismatch")
    model, artifact = fit(values, labels, features)
    medians = np.array([f["median"] for f in artifact["features"]])
    with threadpool_limits(limits=4):
        expected = model.predict_proba(impute(values, medians))[:, 1]
    actual = portable_prediction(values, artifact)
    maximum_error = float(np.max(np.abs(expected - actual)))
    if maximum_error > 1e-11: raise ValueError("portable numeric parity failure")
    recipe = recipe_hash()
    last_date = max(r["date"] for r in rows)
    artifact["version"] = f"sporttery-draw-observation-v2-{last_date.replace('-', '')}-{source['dataSHA256'][:12]}-{recipe[:8]}"
    artifact["training"] = dict(dataSHA256=source["dataSHA256"], recipeSHA256=recipe,
        featureCodeSHA256=source["featureCodeSHA256"], rows=len(rows),
        sportteryRows=sum(bool(r["jingcaiId"].strip()) for r in rows), latestDate=last_date,
        generatedAt=datetime.now(timezone.utc).isoformat(), sourceGeneratedAt=source["generatedAt"],
        hyperparameters=PARAMETERS, sklearnVersion=sklearn.__version__)
    artifact["contract"] = dict(fitType="all_confirmed_labels_final_refit", prospectiveTestRows=0,
        captureTimeKnownPrematch=False, scoreMeaning="uncalibrated ranking score, not hit-rate probability",
        selectionThreshold="unchanged original .2016984853563883; no November tuning",
        history="same serving extraction; strict prior +2h recent/H2H; legacy aggregate snapshots retained",
        neverUseAsInputs=["target result", "target score", "target status", "match ID", "team ID", "kickoff"])
    indices = sorted(set(np.linspace(0, len(rows)-1, 20, dtype=int).tolist() + np.argsort(actual)[:3].tolist() + np.argsort(actual)[-3:].tolist()))
    artifact["checks"] = [dict(features=rows[i]["features"], score=float(actual[i]), selected=bool(actual[i]>=THRESHOLD)) for i in indices]
    empty_score = float(portable_prediction(np.full((1, len(features)), np.nan), artifact)[0])
    artifact["checks"].append(dict(features={}, score=empty_score, selected=empty_score>=THRESHOLD))
    artifact["audit"] = dict(allRowsPortableParityMaxError=maximum_error,
        inSample=metrics(rows, actual, observed(values, artifact)),
        baselineSameLatestData=metrics(rows, old_scores, observed(values, baseline)))
    # Optional locked time splits compare newly refitted weights on LATER dates.
    # These are latest-data retrospective checks, never claims of blind history.
    if args.report:
        replay = []
        for start, end in (("2026-11-01", "2026-11-07"), ("2026-11-08", "2026-11-14"), ("2026-11-15", "2026-11-21")):
            training = np.array([r["date"] < start for r in rows])
            test = np.array([start <= r["date"] <= end for r in rows])
            if training.sum() < 100 or not test.any(): continue
            _, check_model = fit(values[training], labels[training], features)
            check_scores = portable_prediction(values[test], check_model)
            test_rows = [r for r, use in zip(rows, test) if use]
            replay.append(dict(trainThrough=max(r["date"] for r,use in zip(rows,training) if use),
                testStart=start, testEnd=end, trainingRows=int(training.sum()),
                original=metrics(test_rows,old_scores[test],observed(values[test],baseline)),
                updated=metrics(test_rows,check_scores,observed(values[test],check_model))))
        write(args.report, dict(version=artifact["version"], training=artifact["training"],
            audit=artifact["audit"], temporalReplay=replay,
            caveat="Latest-data retrospective replay. Final release fitted all labels; its historical scores are in-sample. No prospective profitability claim.",
            thresholdTuned=False, modelFamilyChanged=False, fullRefitAfterIncrementalDataChange=True))
    write(args.output, artifact)
    print(json.dumps(dict(version=artifact["version"], rows=len(rows), latestDate=last_date,
        checks=len(artifact["checks"]), parityMaxError=maximum_error, output=str(args.output))))

if __name__ == "__main__": main()
