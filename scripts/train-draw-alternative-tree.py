#!/usr/bin/env python3
"""Offline chronological win/loss tree study after the fixed draw selector.

No database, network, service operation or production artifact replacement.
Models are selected on calibration only, refitted on earlier labels only, and
compared with the current low-price alternative on exactly the same test IDs.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import re
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

for variable in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
    os.environ.setdefault(variable, "4")

import joblib
import numpy as np
import sklearn
from scipy.stats import binomtest
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.impute import SimpleImputer
from sklearn.tree import DecisionTreeClassifier, export_text
from threadpoolctl import threadpool_limits

BEIJING = timezone(timedelta(hours=8))


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def clean(value):
    if isinstance(value, dict):
        return {str(k): clean(v) for k, v in value.items()}
    if isinstance(value, (tuple, list)):
        return [clean(v) for v in value]
    if isinstance(value, np.ndarray):
        return clean(value.tolist())
    if isinstance(value, np.generic):
        return clean(value.item())
    if isinstance(value, float) and not math.isfinite(value):
        return None
    return value


def write(path, value):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    Path(path).write_text(json.dumps(clean(value), ensure_ascii=False, indent=2, allow_nan=False), encoding="utf-8")


def bjt_day(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(BEIJING).date().isoformat()


def minimum_guards(name):
    if name.startswith(("safeRecent.home.", "safeRecent.away.")):
        return [(".".join(name.split(".")[:3]) + ".n", 5)]
    if name.startswith("safeH2H."):
        return [(".".join(name.split(".")[:2]) + ".n", 1)]
    if name.startswith("safeRecent.pair."):
        return [(f"safeRecent.{side}.10.n", 5) for side in ("home", "away")]
    if name.startswith("conflict.recent."):
        p = name.split(".")
        return [(f"conflict.recent.{p[2]}.3.n" if p[3] == "3" else f"safeRecent.{p[2]}.10.n", 3 if p[3] == "3" else 5)]
    return []


def exclusion(meta):
    name, lower = meta["name"], meta["name"].lower()
    if name.startswith("availability.") or "ambiguous" in lower or "missing" in lower:
        return "availability flag"
    if meta.get("denominatorOnly") or name.endswith(".n") or re.search(r"\.(recentHomeMatches|recentGuestMatches|homeSamples|awaySamples|matches|sampleCount|samples)$", name, re.I):
        return "denominator only"
    if any(v in lower for v in ("matchid", "teamid", "kickoff", "timestamp", "resultlabel", "predictionhit", "accuracyfit", "myangle")):
        return "identity/time/target"
    return None


def matrix(source, raw, columns=None):
    names, metadata, rows = source["featureNames"], source["metadata"], source["rows"]
    assert names == [m["name"] for m in metadata] and len(set(names)) == len(names)
    assert [r["matchId"] for r in rows] == [r["matchId"] for r in raw["rows"]]
    assert source["sourceSHA256"] == raw["_sha256"]
    x = np.asarray([[np.nan if v is None else v for v in r["values"]] for r in rows], dtype=float)
    x[~np.isfinite(x)] = np.nan
    lookup = {name: j for j, name in enumerate(names)}
    risk_keys = ("usedRecentAtOrAfterKickoff", "usedRecentTargetId", "usedRecentUnknownTime",
                 "againstListedAtOrAfterKickoff", "againstListedTargetId", "againstListedUnknownTime")
    risk = np.asarray([any((r.get("history") or {}).get(k, 0) > 0 for k in risk_keys) for r in raw["rows"]])
    derived = [j for j, m in enumerate(metadata) if m.get("view") not in ("market", "safeHistory")]
    masked = int(np.isfinite(x[np.ix_(risk, derived)]).sum())
    x[np.ix_(risk, derived)] = np.nan
    guarded = 0
    for j, name in enumerate(names):
        valid = np.ones(len(rows), dtype=bool)
        for guard, minimum in minimum_guards(name):
            observed = x[:, lookup[guard]]
            valid &= np.isfinite(observed) & (observed >= minimum)
        guarded += int(np.sum(np.isfinite(x[:, j]) & ~valid))
        x[~valid, j] = np.nan
    if columns is None:
        columns = [j for j, m in enumerate(metadata) if exclusion(m) is None]
    return x[:, columns], columns, dict(unsafeHistoryRows=int(risk.sum()), maskedDerivedCells=masked,
        guardedCells=guarded, columns=len(columns), excludedColumns=[m["name"] for m in metadata if exclusion(m)],
        nonEmptyRows=int(np.isfinite(x[:, columns]).any(axis=1).sum()))


def low_price(raw_rows):
    result = []
    for row in raw_rows:
        a = (row.get("frontend") or {}).get("averageEuroOdds")
        valid = lambda v: type(v) in (float, int) and math.isfinite(v) and v > 1
        if isinstance(a, list) and len(a) == 3 and valid(a[0]) and valid(a[2]) and abs(a[0] - a[2]) > 1e-9:
            result.append(0 if a[0] < a[2] else 2)
        else:
            result.append(-1)
    return np.asarray(result, dtype=np.int8)


def wilson(hits, n):
    if n == 0:
        return [None, None]
    z = 1.959963984540054
    p, d = hits / n, 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    w = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return [c - w, c + w]


def outcome_metrics(y, prediction, candidate):
    direction = candidate & (prediction >= 0)
    nd = direction & (y != 1)
    hits = direction & (prediction == y)
    dr = candidate & (y == 1)
    n, available, nh, non_draws = int(candidate.sum()), int(direction.sum()), int(hits.sum()), int(nd.sum())
    return dict(candidates=n, available=available, unavailable=n - available, actualDraws=int(dr.sum()),
                actualNonDraws=int(np.sum(candidate & (y != 1))), nonDrawAvailable=non_draws,
                backupHits=nh, backupMissesOnNonDraw=non_draws - nh,
                backupSingleAccuracy=nh / available if available else None,
                backupHitsOverAllCandidates=nh / n if n else None,
                backupConditionalAccuracy=nh / non_draws if non_draws else None,
                conditionalWilson95=wilson(nh, non_draws),
                drawOrBackupHits=int(dr.sum()) + nh,
                drawOrBackupCoverage=(int(dr.sum()) + nh) / n if n else None)


def paired_metrics(y, prediction, baseline, candidate):
    common = candidate & (prediction >= 0) & (baseline >= 0)
    nd = common & (y != 1)
    tree_only = int(np.sum(nd & (prediction == y) & (baseline != y)))
    price_only = int(np.sum(nd & (baseline == y) & (prediction != y)))
    discordant = tree_only + price_only
    return dict(tree=outcome_metrics(y, prediction, common), lowPrice=outcome_metrics(y, baseline, common),
                changedDirection=int(np.sum(common & (prediction != baseline))),
                treeOnlyCorrect=tree_only, lowPriceOnlyCorrect=price_only,
                netExtraHits=tree_only - price_only,
                mcnemarExactTwoSidedP=float(binomtest(tree_only, discordant, .5).pvalue) if discordant else 1.)


def make_model(config, protocol):
    kind, pool = config["algorithm"], config["pool"]
    if kind == "CART":
        p = protocol["models"]["CART"]
        return DecisionTreeClassifier(max_depth=config["depth"], min_samples_leaf=config["leaf"],
            criterion=p["criterion"], random_state=p["random_state"])
    p = protocol["models"]["HistGradientBoosting"]
    return HistGradientBoostingClassifier(max_depth=config["depth"], min_samples_leaf=config["leaf"],
        max_iter=p["max_iter"], learning_rate=p["learning_rate"], l2_regularization=p["l2_regularization"],
        early_stopping=p["early_stopping"], random_state=p["random_state"])


def fit(config, protocol, x, y, selected):
    training = selected & (y != 1) & np.isfinite(x).any(axis=1)
    assert training.sum() >= 20 and set(y[training]) == {0, 2}
    imputer = SimpleImputer(strategy="median", keep_empty_features=True)
    data = imputer.fit_transform(x[training])
    model = make_model(config, protocol)
    model.fit(data, y[training])
    return model, imputer, training


def predict(model, imputer, x):
    available = np.isfinite(x).any(axis=1)
    direction = np.full(len(x), -1, dtype=np.int8)
    if available.any():
        direction[available] = model.predict(imputer.transform(x[available]))
    return direction


def describe(model, metadata):
    if isinstance(model, DecisionTreeClassifier):
        weights = model.feature_importances_
        nodes = int(model.tree_.node_count)
    else:
        weights = np.zeros(len(metadata))
        nodes = 0
        for stage in model._predictors:
            for tree in stage:
                nodes += len(tree.nodes)
                for node in tree.nodes:
                    if not node["is_leaf"]:
                        weights[int(node["feature_idx"])] += 1
        if weights.sum():
            weights /= weights.sum()
    return dict(treeNodes=nodes, usedFeatures=[dict(name=metadata[j]["name"], label=metadata[j].get("label"),
        view=metadata[j].get("view"), weight=float(weights[j])) for j in np.argsort(weights)[::-1] if weights[j] > 0])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--matrix", type=Path, required=True)
    parser.add_argument("--raw", type=Path, required=True)
    parser.add_argument("--replay", type=Path, required=True)
    parser.add_argument("--protocol", type=Path, required=True)
    parser.add_argument("--live-matrix", type=Path, required=True)
    parser.add_argument("--live-raw", type=Path, required=True)
    parser.add_argument("--live-ledger", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    start = time.perf_counter()
    protocol, source, raw, replay = read(args.protocol), read(args.matrix), read(args.raw), read(args.replay)
    assert sha(args.raw) == protocol["sourceSHA256"] == source["sourceSHA256"] == replay["studySHA256"]
    raw["_sha256"] = sha(args.raw)
    rows = source["rows"]
    ids = [str(r["matchId"]) for r in rows]
    assert len(ids) == len(set(ids)) and ids == [str(r["matchId"]) for r in replay["items"]]
    x, columns, audit = matrix(source, raw)
    metadata = [source["metadata"][j] for j in columns]
    names = [m["name"] for m in metadata]
    y = np.asarray([r["result"] for r in rows], dtype=np.int8)
    assert set(y) == {0, 1, 2}
    for row in rows:
        assert row["result"] == (0 if row["homeScore"] > row["awayScore"] else 1 if row["homeScore"] == row["awayScore"] else 2)
    days = np.asarray([bjt_day(r["matchTime"]) for r in rows])
    cut = protocol["cutoffsBjt"]
    periods = dict(train=days <= cut["trainThrough"], calibration=(days > cut["trainThrough"]) & (days <= cut["calibrationThrough"]),
                   test=days > cut["calibrationThrough"])
    assert np.all(sum(periods.values()) == 1)
    global_selected = np.asarray([r["researchOnlyUnscopedInputAvailable"] and r["researchOnlyUnscopedPrediction"]["selected"] for r in replay["items"]])
    jc_selected = np.asarray([r["productionOriginalSelected"] for r in replay["items"]])
    marked = np.asarray([bool(str(r.get("jingcaiId") or "").strip()) for r in rows])
    assert np.all(~jc_selected | (global_selected & marked))
    scopes = dict(global_selected=global_selected, jc_selected=jc_selected)
    pools = dict(global_selected=global_selected, jc_selected=jc_selected, global_all=np.ones(len(rows), dtype=bool), jc_all=marked)
    baseline = low_price(raw["rows"])
    args.out.mkdir(parents=True, exist_ok=True)
    configs = []
    for pool in protocol["trainingPools"]:
        for depth in protocol["models"]["CART"]["max_depth"]:
            for leaf in protocol["models"]["CART"]["min_samples_leaf_by_pool"][pool]:
                configs.append(dict(id=f"CART-{pool}-d{depth}-l{leaf}", algorithm="CART", pool=pool, depth=depth, leaf=leaf))
        for depth in protocol["models"]["HistGradientBoosting"]["max_depth"]:
            leaf = protocol["models"]["HistGradientBoosting"]["min_samples_leaf_by_pool"][pool]
            configs.append(dict(id=f"HGB-{pool}-d{depth}-l{leaf}", algorithm="HGB", pool=pool, depth=depth, leaf=leaf))
    print(json.dumps(dict(stage="training", rows=len(rows), features=len(names), configs=len(configs), periods={k:int(v.sum()) for k,v in periods.items()})), flush=True)
    calibration = []
    with threadpool_limits(limits=4):
        for config in configs:
            model, imputer, training = fit(config, protocol, x, y, pools[config["pool"]] & periods["train"])
            direction = predict(model, imputer, x)
            record = dict(config=config, trainingRows=int(training.sum()), scopes={})
            for scope, selected in scopes.items():
                if scope == "global_selected" and config["pool"].startswith("jc_"):
                    continue
                record["scopes"][scope] = paired_metrics(y, direction, baseline, selected & periods["calibration"])
            calibration.append(record)
    write(args.out / "calibration-models.json", calibration)
    chosen = {}
    for scope in scopes:
        chosen[scope] = {}
        for algorithm in ("CART", "HGB"):
            candidates = [r for r in calibration if r["config"]["algorithm"] == algorithm and scope in r["scopes"]]
            candidates.sort(key=lambda r: (-r["scopes"][scope]["tree"]["backupConditionalAccuracy"], r["config"]["depth"], -r["config"]["leaf"], r["config"]["id"]))
            chosen[scope][algorithm] = candidates[0]
    # Seal all model/config choices before test evaluation or live labels are read.
    write(args.out / "selected-on-calibration.json", dict(protocolSHA256=sha(args.protocol), choices=chosen))
    live_source, live_raw, ledger = read(args.live_matrix), read(args.live_raw), read(args.live_ledger)
    live_raw["_sha256"] = sha(args.live_raw)
    lx, _, live_audit = matrix(live_source, live_raw, columns)
    assert live_source["featureNames"] == source["featureNames"]
    latest = {str(r["matchId"]):r for r in ledger["latest"]}
    live_rows = live_source["rows"]
    live_labels = np.asarray([latest[str(r["matchId"])]["result"] if latest[str(r["matchId"])]["finished"] else -1 for r in live_rows], dtype=np.int8)
    live_original = np.asarray([latest[str(r["matchId"])]["original"] and latest[str(r["matchId"])]["finished"] for r in live_rows])
    live_after_cut = np.asarray([bjt_day(r["matchTime"]) > cut["calibrationThrough"] for r in live_rows])
    live_original &= live_after_cut
    live_base = low_price(live_raw["rows"])
    assert all(live_base[i] == latest[str(r["matchId"])]["direction"] for i,r in enumerate(live_rows))
    all_results, predictions = {}, {}
    with threadpool_limits(limits=4):
        for scope, algorithms in chosen.items():
            all_results[scope] = {}
            for algorithm, selected in algorithms.items():
                config = selected["config"]
                model, imputer, training = fit(config, protocol, x, y, pools[config["pool"]] & ~periods["test"])
                assert np.all(days[training] <= cut["calibrationThrough"])
                direction, live_direction = predict(model, imputer, x), predict(model, imputer, lx)
                candidate = scopes[scope] & periods["test"]
                result = dict(config=config, trainingRows=int(training.sum()), trainingLatestBjtDay=max(days[training].tolist()),
                    calibration=selected["scopes"][scope], test=paired_metrics(y, direction, baseline, candidate),
                    testAllCandidates=outcome_metrics(y, direction, candidate),
                    trainingResubstitution=outcome_metrics(y, direction, pools[config["pool"]] & ~periods["test"]),
                    featureUsage=describe(model, metadata), testByDate={})
                for day in np.unique(days[periods["test"]]):
                    result["testByDate"][str(day)] = paired_metrics(y, direction, baseline, scopes[scope] & (days == day))
                old_ids_path = Path(__file__).resolve().parents[1] / "reports/draw-observation-model/frozen-scores.json"
                if old_ids_path.exists():
                    old_ids = {str(r["matchId"]) for r in read(old_ids_path)["scores"]}
                    unseen = np.asarray([mid not in old_ids for mid in ids])
                    result["testUnseenByOriginalSelector"] = paired_metrics(y, direction, baseline, candidate & unseen)
                if scope == "jc_selected":
                    result["sealedLive"] = paired_metrics(live_labels, live_direction, live_base, live_original)
                    before22 = []
                    for row in live_rows:
                        record = latest[str(row["matchId"])]
                        purchase_day = str(record.get("purchaseDay") or "")
                        cutoff = datetime.fromisoformat(purchase_day + "T22:00:00+08:00") if purchase_day else None
                        predicted_at = datetime.fromisoformat(record["predictedAt"].replace("Z", "+00:00"))
                        before22.append(cutoff is not None and predicted_at <= cutoff)
                    result["sealedLiveLatestRecordBefore22"] = paired_metrics(live_labels, live_direction, live_base, live_original & np.asarray(before22))
                    result["liveTargets"] = []
                    for i,row in enumerate(live_rows):
                        if str(row["matchId"]) in ("498161722", "498168369", "498184399"):
                            result["liveTargets"].append(dict(matchId=row["matchId"], home=row["home"], guest=row["guest"],
                                actualResult=int(live_labels[i]), treeDirection=int(live_direction[i]), lowPriceDirection=int(live_base[i]),
                                frozenOriginalSelected=latest[str(row["matchId"])]["original"],
                                sourceAverage=live_raw["rows"][i]["latest"]["average"], sourceAsOf=latest[str(row["matchId"])]["asOf"]))
                all_results[scope][algorithm] = result
                key = f"{scope}-{algorithm}"
                predictions[key] = dict(current=direction.tolist(), live=live_direction.tolist())
                joblib.dump(dict(model=model, imputer=imputer, columns=columns, names=names, config=config,
                    protocolSHA256=sha(args.protocol), trainedThroughBjt=cut["calibrationThrough"],
                    classMeaning="home/away conditional on non-draw; never a full win-draw-loss probability"), args.out / f"{key}.joblib")
                if algorithm == "CART":
                    (args.out / f"{key}-rules.txt").write_text(export_text(model, feature_names=names, decimals=5), encoding="utf-8")
    report = dict(version="draw-alternative-tree-study-v1", generatedAt=datetime.now(timezone.utc).isoformat(),
        protocol=protocol, protocolSHA256=sha(args.protocol), sourceSHA256=sha(args.raw), matrixSHA256=sha(args.matrix),
        modelArtifactHash=source["modelArtifactHash"], sklearnVersion=sklearn.__version__,
        featureAudit=audit, liveFeatureAudit=live_audit, selectedFeatures=metadata,
        partitions={k:dict(rows=int(v.sum()), globalCandidates=int(np.sum(v & global_selected)), jcCandidates=int(np.sum(v & jc_selected)),
            earliest=min(days[v].tolist()), latest=max(days[v].tolist())) for k,v in periods.items()}, results=all_results,
        limitations=["Current-snapshot historical inputs often lack source capture timestamps; chronological testing is not proof of immutable pre-match availability.",
            "Sealed LIVE audit uses actual pre-match inputs. Its labels were excluded from fitting and calibration, but these historical outcomes were already discussed in earlier research.",
            "The original draw selector is fixed and may have seen older retrospective test labels during its own development; unseen-ID subset is reported separately.",
            "Tree binary class probabilities are uncalibrated conditional home/away scores, not full win-draw-loss probabilities.",
            "Calibration cohorts are small, especially JC. Single temporal holdout is not a guarantee of future accuracy.",
            "No production rules, database records or running services were changed."], seconds=time.perf_counter()-start)
    write(args.out / "report.json", report)
    write(args.out / "predictions.json", dict(currentRows=[{k:r[k] for k in ("matchId","matchTime","jingcaiId","home","guest","result")} for r in rows],
        liveRows=[{**latest[str(r["matchId"])], "featureSource":"sealed live raw_input"} for r in live_rows], predictions=predictions))
    print(json.dumps(clean(dict(stage="complete", partitions=report["partitions"],
        results={scope:{kind:{k:v[k] for k in ("config","test","sealedLive") if k in v} for kind,v in kinds.items()} for scope,kinds in all_results.items()},
        seconds=report["seconds"])), ensure_ascii=True), flush=True)


if __name__ == "__main__":
    main()
