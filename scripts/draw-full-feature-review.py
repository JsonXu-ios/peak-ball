#!/usr/bin/env python3
"""Independent audit of exported features, chronological folds and saved scores."""
import copy
import csv
import hashlib
import importlib.util
import json
import math
import sys
from collections import Counter, defaultdict
from datetime import datetime, timedelta
from pathlib import Path

sys.dont_write_bytecode = True
import numpy as np

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "scripts"))
from draw_full_features import build_features, self_test

OUT = ROOT / "reports/draw-full-feature-study"


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def average_precision(y, p):
    order = np.argsort(-p, kind="stable")
    y, p = np.asarray(y)[order], np.asarray(p)[order]
    if not sum(y):
        return None
    ends = np.r_[np.flatnonzero(p[1:] != p[:-1]), len(y)-1]
    positive = np.cumsum(y)[ends]
    increments = np.diff(np.r_[0, positive])
    return float(sum(increments * positive / (ends+1)) / sum(y))


def metrics(y, p):
    y, p = np.asarray(y, int), np.clip(np.asarray(p, float), 1e-7, 1-1e-7)
    # Pair-count AUC, including half credit for exact ties.
    positives, negatives = p[y == 1], p[y == 0]
    auc = float(sum(np.sum(v > negatives) + .5*np.sum(v == negatives) for v in positives) / (len(positives)*len(negatives))) if len(positives) and len(negatives) else None
    return {"n": len(y), "draws": int(sum(y)), "rate": float(np.mean(y)), "AP": average_precision(y, p), "ROC_AUC": auc,
            "Brier": float(np.mean((y-p)**2)), "logLoss": float(-np.mean(y*np.log(p)+(1-y)*np.log1p(-p)))}


def confusion(y, flags):
    y, flags = np.asarray(y, bool), np.asarray(flags, bool)
    tp, fp, fn, tn = int(sum(y & flags)), int(sum(~y & flags)), int(sum(y & ~flags)), int(sum(~y & ~flags))
    div = lambda a, b: a/b if b else None
    return {"n": len(y), "draws": int(sum(y)), "selected": tp+fp, "TP": tp, "FP": fp, "FN": fn, "TN": tn,
            "precision": div(tp,tp+fp), "recall": div(tp,tp+fn), "specificity": div(tn,tn+fp), "selectionRate": div(tp+fp,len(y))}


def same_fields(left, right):
    for key, value in left.items():
        other = right[key]
        assert other is None if value is None else abs(value-other) < 1e-11, (key, value, other)


def main():
    spec = importlib.util.spec_from_file_location("full_study_reviewed", ROOT / "scripts/draw-full-feature-study.py")
    study = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = study
    spec.loader.exec_module(study)
    data, report, protocol = (read(OUT / name) for name in ("dataset.json", "analysis.json", "protocol.json"))
    rows = sorted(data["rows"], key=lambda r: (datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00")), str(r["matchId"])))
    for row in rows:
        row["_kickoff"] = datetime.fromisoformat(row["matchTime"].replace("Z", "+00:00"))
        row["_day"] = row["_kickoff"].date().isoformat()
    x, manifest = build_features(rows)
    assert read(OUT / "feature-manifest.json") == manifest
    with (OUT / "features.csv").open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.reader(stream)
        header, saved = next(reader), list(reader)
    assert header[2:] == [m["name"] for m in manifest]
    assert [r[0] for r in saved] == [str(r["matchId"]) for r in rows]
    assert [int(r[1]) for r in saved] == [r["result"] for r in rows]
    saved_x = np.array([[float(v) if v else np.nan for v in r[2:]] for r in saved])
    assert np.array_equal(x, saved_x, equal_nan=True)
    assert report["featureMatrixSHA256"] == hashlib.sha256(np.nan_to_num(x,nan=-9.87654321e99).astype('<f8').tobytes()).hexdigest()
    checks = Counter(featureMatrixCells=x.size, featureManifestColumns=len(manifest))
    issues = []
    assert report["datasetSHA256"] == digest(OUT / "dataset.json")
    assert report["extractorSHA256"] == digest(ROOT / "scripts/draw_full_features.py")
    assert report["scriptSHA256"] == digest(ROOT / "scripts/draw-full-feature-study.py")
    assert report["protocolSHA256"] == digest(OUT / "protocol.json")
    folds = read(OUT / "folds.json")
    expected_folds = study.make_folds(rows, protocol)
    seen, ids = set(), {str(r["matchId"]): i for i,r in enumerate(rows)}
    for fold, expected in zip(folds, expected_folds):
        train, cal, test = (fold[key] for key in ("train", "calibration", "test"))
        assert train == expected["train"] and cal == expected["calibration"] and test == expected["test"]
        assert set(train).isdisjoint(cal) and set(train).isdisjoint(test) and set(cal).isdisjoint(test)
        for key in ("train", "calibration", "test"):
            assert fold["matchIds"][key] == [rows[i]["matchId"] for i in fold[key]]
        assert max(rows[i]["_day"] for i in train) < min(rows[i]["_day"] for i in cal)
        assert max(rows[i]["_day"] for i in cal) < min(rows[i]["_day"] for i in test)
        assert max(rows[i]["_kickoff"]+timedelta(hours=2) for i in train) < min(rows[i]["_kickoff"] for i in cal)
        assert max(rows[i]["_kickoff"]+timedelta(hours=2) for i in cal) < min(rows[i]["_kickoff"] for i in test)
        assert not seen.intersection(test)
        seen.update(test)
        checks["wholeDateFoldsAndStrictTwoHourBoundaries"] += 1
    with (OUT / "predictions.csv").open(encoding="utf-8-sig", newline="") as stream:
        predictions = list(csv.DictReader(stream))
    grouped = defaultdict(list)
    for item in predictions:
        r = {**item, "fold": int(item["fold"]), "y": int(item["y"]), "probability": float(item["probability"])}
        for target in (.5,.7,.8):
            r[f"selected_{target}"] = bool(int(item[f"selected_{target}"]))
        assert r["y"] == int(rows[ids[r["matchId"]]]["result"] == 1)
        grouped[r["model"]].append(r)
    reports = {r["id"]: r for r in report["models"]}
    specifications = {s.id: s for s in study.model_specs()}
    assert set(grouped) == set(reports)
    independent_fold_metrics = {}
    for model_id, items in grouped.items():
        assert len(items) == len(seen) and len({r["matchId"] for r in items}) == len(seen)
        for fold in folds:
            block = [r for r in items if r["fold"] == fold["fold"]]
            assert [r["matchId"] for r in block] == fold["matchIds"]["test"]
            scores, y = np.array([r["probability"] for r in block]), np.array([r["y"] for r in block])
            assert np.isfinite(scores).all() and np.all((scores>=0)&(scores<=1))
            m = metrics(y, scores)
            independent_fold_metrics[model_id, fold["fold"]] = m
            stored = reports[model_id]["folds"][fold["fold"]]
            same_fields(m, stored["metrics"])
            specification = specifications[model_id]
            expected_columns = []
            for j, meta in enumerate(manifest):
                if meta["view"] not in specification.views or any(token in (meta["family"]+"."+meta["name"]).lower() for token in specification.excluded):
                    continue
                training = x[fold["train"],j]
                valid_training = training[np.isfinite(training)]
                if len(valid_training) >= max(20,math.ceil(len(fold["train"])*.02)) and max(valid_training)>min(valid_training):
                    expected_columns.append(meta["name"])
            assert expected_columns == stored["columnNames"], (model_id,fold["fold"])
            checks["trainingOnlyFeatureEligibilityAndAblations"] += 1
            for target in (.5,.7,.8):
                threshold = stored["thresholds"][str(target)]
                assert [r[f"selected_{target}"] for r in block] == list(scores >= threshold)
                cal = stored["calibrationTargets"][str(target)]
                if cal["draws"]:
                    assert cal["recall"] >= target-1e-12
                checks["calibrationThresholdAndLaterFrozenFlags"] += 1
            checks["independentFoldMetrics"] += 1
        for part, role in (("allTemporal",None),("development","development"),("audit","lockedInternalAudit")):
            block = [r for r in items if role is None or r["role"] == role]
            y, scores = np.array([r["y"] for r in block]), np.array([r["probability"] for r in block])
            stored = reports[model_id]["pooled"][part]
            same_fields(metrics(y,scores), stored)
            for target in (.5,.7,.8):
                same_fields(confusion(y,[r[f"selected_{target}"] for r in block]), stored["recallTargets"][str(target)])
                checks["independentPooledConfusionTables"] += 1
        # Market comparator selects the same requested count per time block,
        # with ties preserved, without using the actual outcome to rank rows.
        market_items = grouped["market_raw"]
        assert [r["matchId"] for r in items] == [r["matchId"] for r in market_items]
        equal_count_flags = {str(target):np.zeros(len(items),bool) for target in (.5,.7,.8)}
        for fold in folds:
            block_indices = [i for i,r in enumerate(items) if r["fold"]==fold["fold"]]
            market_scores = np.array([market_items[i]["probability"] for i in block_indices])
            for target in (.5,.7,.8):
                requested = sum(items[i][f"selected_{target}"] for i in block_indices)
                cutoff = sorted(market_scores,reverse=True)[requested-1] if requested else np.inf
                equal_count_flags[str(target)][block_indices] = market_scores>=cutoff
        for part,role in (("allTemporal",None),("development","development"),("audit","lockedInternalAudit")):
            indices = [i for i,r in enumerate(items) if role is None or r["role"]==role]
            for target in (.5,.7,.8):
                result = confusion([items[i]["y"] for i in indices], equal_count_flags[str(target)][indices])
                requested = sum(items[i][f"selected_{target}"] for i in indices)
                result["requestedSelectionCount"] = requested
                result["extraFromMarketBoundaryTies"] = result["selected"]-requested
                same_fields(result,reports[model_id]["pooled"][part]["marketAtSameSelectionCount"][str(target)])
                checks["independentSameSelectionCountMarketComparisons"] += 1
    # Recompute the raw-market fallback using only the fold's earlier training labels.
    for item in grouped["market_raw"]:
        row = rows[ids[item["matchId"]]]
        avg = row["latest"].get("average")
        expected = (1/avg[1])/sum(1/v for v in avg) if avg and all(v>0 for v in avg) else np.mean([rows[i]["result"] == 1 for i in folds[item["fold"]]["train"]])
        assert abs(item["probability"]-expected) < 1e-14
        checks["sameRowMarketAndTrainingOnlyFallback"] += 1
    # Use independently reconstructed first-three-block metrics for the winner.
    eligible = [m for m in report["models"] if m["primaryEligible"]]
    ranking = sorted(eligible,key=lambda m:(-np.mean([independent_fold_metrics[m["id"],i]["AP"] for i in range(3)]),np.mean([independent_fold_metrics[m["id"],i]["Brier"] for i in range(3)]),m["id"]))
    assert report["primaryModelId"] == ranking[0]["id"]
    assert report["selection"]["developmentRanking"] == [m["id"] for m in ranking]
    altered = copy.deepcopy(report["models"])
    for m in altered:
        for f in m["folds"][3:]:
            f["metrics"] = {"AP": 1e50,"Brier": -1e50}
        m["pooled"] = {"audit": {"AP":1e100}}
    assert study.choose_primary(altered) == report["primaryModelId"]
    checks["auditOutcomeMutationCannotChangePrimary"] = 1
    coverage = report["evaluationCoverage"]
    assert coverage["allCompleted"] == len(rows) == 4161
    assert coverage["allDraws"] == sum(r["result"]==1 for r in rows) == 970
    assert coverage["temporallyEvaluated"] == len(seen)
    assert coverage["evaluatedDraws"] == sum(rows[i]["result"]==1 for i in seen)
    assert coverage["warmupNotScored"] == len(rows)-len(seen)
    assert coverage["warmupDraws"] == 970-coverage["evaluatedDraws"]
    assert coverage["independentProspectiveMatches"] == 0
    for i, description in enumerate(report["featureDescriptions"]):
        valid = np.isfinite(x[:,i]); y = np.array([r["result"]==1 for r in rows])
        assert description["validDraws"] == int(sum(valid&y))
        assert description["validNonDraws"] == int(sum(valid&~y))
        assert description["validDraws"]+description["missingDraws"] == 970
        assert description["validNonDraws"]+description["missingNonDraws"] == 3191
        checks["allDrawNonDrawFeatureDenominators"] += 1
    out = {"purpose":"Independent feature/fold/metric/selection audit; all dates are repeated internal validation.",
           "datasetSHA256":digest(OUT/"dataset.json"),"featureScriptSHA256":digest(ROOT/"scripts/draw_full_features.py"),"studyScriptSHA256":digest(ROOT/"scripts/draw-full-feature-study.py"),
           "checks":dict(checks),"issues":issues,"featureSelfTest":self_test(),"primaryModelIdFromDevelopmentOnly":ranking[0]["id"],
           "evaluationCoverage":coverage,"featureMatrixFiniteValues":int(np.isfinite(x).sum()),"allSavedFeaturesEqualFreshExtractor":True}
    (OUT/"review.json").write_text(json.dumps(out,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
    print(json.dumps(out,ensure_ascii=False))


if __name__ == "__main__":
    main()
