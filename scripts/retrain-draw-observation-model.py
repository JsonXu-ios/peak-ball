#!/usr/bin/env python3
"""Train offline draw candidates with a locked chronological evaluation.

Consumes a label-blind feature export. Never connects to a database, starts a
service, replaces the production artifact, or tunes on the final test period.
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
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.impute import SimpleImputer
from sklearn.metrics import average_precision_score, brier_score_loss
from threadpoolctl import threadpool_limits

ROOT = Path(__file__).resolve().parents[1]
BEIJING = timezone(timedelta(hours=8))
CONFIGS = [
    dict(id="global_clean_d3_l40_i240", scope="global", view="old_clean", depth=3, leaf=40, iterations=240),
    dict(id="global_clean_d2_l60_i120", scope="global", view="old_clean", depth=2, leaf=60, iterations=120),
    dict(id="global_all_d2_l60_i120", scope="global", view="all_recorded", depth=2, leaf=60, iterations=120),
    dict(id="jc_clean_d2_l20_i120", scope="sporttery", view="old_clean", depth=2, leaf=20, iterations=120),
]


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def clean(value):
    if isinstance(value, dict):
        return {str(k): clean(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [clean(v) for v in value]
    if isinstance(value, np.ndarray):
        return clean(value.tolist())
    if isinstance(value, np.generic):
        return clean(value.item())
    if isinstance(value, float) and not math.isfinite(value):
        return None
    return value


def write(path, value):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(clean(value), ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")


def guards(name):
    if name.startswith(("safeRecent.home.", "safeRecent.away.")):
        return [dict(feature=".".join(name.split(".")[:3]) + ".n", minimum=5)]
    if name.startswith("safeH2H."):
        return [dict(feature=".".join(name.split(".")[:2]) + ".n", minimum=1)]
    if name.startswith("safeRecent.pair."):
        return [dict(feature=f"safeRecent.{side}.10.n", minimum=5) for side in ("home", "away")]
    if name.startswith("conflict.recent."):
        parts = name.split(".")
        return [dict(feature=f"conflict.recent.{parts[2]}.3.n" if parts[3] == "3" else f"safeRecent.{parts[2]}.10.n", minimum=3 if parts[3] == "3" else 5)]
    return []


def excluded_feature(meta):
    name = meta["name"]
    lower = name.lower()
    if name.startswith("availability.") or "ambiguous" in lower or "missing" in lower:
        return "availability/missingness flag"
    if meta.get("denominatorOnly") or name.endswith(".n") or re.search(r"\.(recentHomeMatches|recentGuestMatches|homeSamples|awaySamples|matches|sampleCount|samples)$", name, re.I):
        return "sample count used for qualification only"
    if any(v in lower for v in ("matchid", "teamid", "kickoff", "timestamp", "resultlabel")):
        return "identity/time/target information"
    return None


def chronological_split(rows):
    days = np.asarray([datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00")).astimezone(BEIJING).date().isoformat() for r in rows])
    unique, counts = np.unique(days, return_counts=True)
    if len(unique) < 5:
        raise ValueError("at least five separate kickoff dates are required")
    cumulative = np.cumsum(counts)
    first = min(range(len(unique) - 2), key=lambda i: (abs(cumulative[i] - .6 * len(rows)), i))
    second = min(range(first + 1, len(unique) - 1), key=lambda i: (abs(cumulative[i] - .8 * len(rows)), i))
    boundaries = [str(unique[first]), str(unique[second])]
    masks = dict(train=days <= boundaries[0], calibration=(days > boundaries[0]) & (days <= boundaries[1]), test=days > boundaries[1])
    assert np.all(sum(masks.values()) == 1)
    return days, boundaries, masks


def wilson(hits, n):
    if not n:
        return [None, None]
    z = 1.959963984540054
    p, denominator = hits / n, 1 + z * z / n
    center = (p + z * z / (2 * n)) / denominator
    delta = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denominator
    return [center - delta, center + delta]


def counts(y, selected, scope):
    selected = selected & scope
    total, draws, n = int(scope.sum()), int(np.sum(y & scope)), int(selected.sum())
    hits = int(np.sum(y & selected))
    return dict(matches=total, draws=draws, selected=n, hits=hits, misses=n - hits,
                precision=hits / n if n else None, coverage=hits / draws if draws else None,
                precisionWilson95=wilson(hits, n))


def threshold_for_coverage(y, scores, scope, available, target):
    all_draws = int(np.sum(y & scope))
    needed = math.ceil(target * all_draws)
    actual = scores[y & scope & available]
    if not needed or not len(actual):
        raise ValueError("calibration has no observable draws")
    threshold = float(np.sort(actual)[::-1][min(needed, len(actual)) - 1])
    return threshold, dict(target=target, neededDraws=needed, observedDraws=len(actual),
                           targetAttainable=len(actual) >= needed, boundaryTiesIncluded=True)


def evaluation(y, scores, threshold, available, marked, gates, masks):
    chosen = available & (scores >= threshold)
    # Compatibility observation only: 0.225 has not been calibrated for a new
    # score distribution. It cannot select the winning candidate.
    secondary = chosen & (scores >= .225) & gates & marked
    result = {}
    for period, mask in masks.items():
        result[period] = {}
        for name, scope in (("global", mask), ("sporttery", mask & marked)):
            scope_scores = scores[scope & available]
            scope_labels = y[scope & available]
            result[period][name] = dict(
                original=counts(y, chosen, scope),
                currentSecondaryCompatibility=counts(y, secondary, scope),
                unavailable=int(np.sum(scope & ~available)),
                averagePrecision=float(average_precision_score(scope_labels, scope_scores)) if int(scope_labels.sum()) else None,
                brier=float(brier_score_loss(scope_labels, scope_scores)) if len(scope_scores) else None,
            )
    return result


def portable(model, imputer, columns, metadata, names, threshold, config, protocol, version):
    trees, used = [], set()
    for stage in model._predictors:
        nodes = []
        for node in stage[0].nodes:
            if node["is_categorical"]:
                raise ValueError("categorical tree cannot be exported as numeric thresholds")
            if node["is_leaf"]:
                nodes.append(dict(leaf=True, value=float(node["value"])))
            else:
                feature = int(node["feature_idx"])
                used.add(feature)
                nodes.append(dict(leaf=False, feature=feature, threshold=float(node["num_threshold"]), left=int(node["left"]), right=int(node["right"])))
        trees.append(nodes)
    return dict(schema=1, version=version, modelId=config["id"], threshold=threshold,
                thresholdComparison=">=; include exact boundary ties", baseline=float(model._baseline_prediction[0, 0]),
                trees=trees, features=[dict(name=names[col], originalIndex=int(col), median=float(imputer.statistics_[j]),
                    guards=guards(names[col]), usedInTrees=j in used, view=metadata[col].get("view"), label=metadata[col].get("label", names[col]))
                    for j, col in enumerate(columns)], originalFeatureNames=names,
                contract=dict(captureTimeKnownPrematch=False, scoreMeaning="uncalibrated model ranking score", scope=config["scope"],
                    fitType="all_available_labels_final_refit", prospectiveTestRows=0,
                    temporalEvaluationIsForSeparateDevelopmentModel=True,
                    fullRefitTestRows=0, thresholdSource="earlier calibration period; unchanged during final full-data refit",
                    secondaryThresholdMeaning="0.225 compatibility observation, not newly calibrated", neverUseAsInputs=["target result", "target score", "target status", "match ID", "team ID", "kickoff"]),
                trainingProtocolSHA256=protocol)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--matrix", type=Path, required=True)
    parser.add_argument("--dataset", type=Path, required=True, help="same raw snapshot, used only to audit legacy history timing risks")
    parser.add_argument("--out", type=Path, default=ROOT / "reports/draw-retrain-20261101")
    parser.add_argument("--old-artifact", type=Path, default=ROOT / "go_server/internal/drawmodeldata/observation_v1.json")
    args = parser.parse_args()
    started = time.perf_counter()
    source = json.loads(args.matrix.read_text(encoding="utf-8-sig"))
    rows, names, metadata = source["rows"], source["featureNames"], source["metadata"]
    assert names == [m["name"] for m in metadata] and len(names) == len(set(names))
    assert len({r["matchId"] for r in rows}) == len(rows)
    labels = np.asarray([r["result"] for r in rows], dtype=int)
    assert set(labels) <= {0, 1, 2}
    y = labels == 1
    marked = np.asarray([bool(str(r.get("jingcaiId") or "").strip()) for r in rows])
    matrix = np.asarray([[np.nan if v is None else v for v in r["values"]] for r in rows], dtype=np.float64)
    assert matrix.shape == (len(rows), len(names))
    matrix[~np.isfinite(matrix)] = np.nan
    if source.get("sourceSHA256") != digest(args.dataset):
        raise ValueError("raw snapshot does not match the sealed feature export")
    raw = json.loads(args.dataset.read_text(encoding="utf-8-sig"))
    risk_keys = ("usedRecentAtOrAfterKickoff", "usedRecentTargetId", "usedRecentUnknownTime",
                 "againstListedAtOrAfterKickoff", "againstListedTargetId", "againstListedUnknownTime")
    history_risk = {r["matchId"]: any((r.get("history") or {}).get(key, 0) > 0 for key in risk_keys) for r in raw["rows"]}
    risk_mask = np.asarray([history_risk[r["matchId"]] for r in rows])
    # Legacy heat/profit/professional snapshots can depend on untruncated
    # history. Conservatively discard non-market/non-safeHistory inputs when
    # their own source audit reports target, future, or unknown-time entries.
    derived_columns = [j for j, meta in enumerate(metadata) if meta.get("view") not in ("market", "safeHistory")]
    masked_derived = int(np.sum(np.isfinite(matrix[np.ix_(risk_mask, derived_columns)])))
    matrix[np.ix_(risk_mask, derived_columns)] = np.nan
    del raw
    columns_by_name = {name: i for i, name in enumerate(names)}
    guarded = matrix.copy()
    for j, name in enumerate(names):
        valid = np.ones(len(rows), dtype=bool)
        for guard in guards(name):
            observed = matrix[:, columns_by_name[guard["feature"]]]
            valid &= np.isfinite(observed) & (observed >= guard["minimum"])
        guarded[~valid, j] = np.nan
    days, boundaries, masks = chronological_split(rows)
    old_training = json.loads((ROOT / "reports/draw-observation-model/frozen-scores.json").read_text(encoding="utf-8-sig"))
    old_ids = {r["matchId"] for r in old_training["scores"]}
    old_unseen = np.asarray([r["matchId"] not in old_ids for r in rows])
    evaluation_masks = {**masks, "testUnseenByOldModel": masks["test"] & old_unseen}
    gates = np.asarray([bool(r.get("sportteryQuotesAvailable")) and r.get("headToHeadSample", 0) >= 5 for r in rows])
    old = json.loads(args.old_artifact.read_text(encoding="utf-8-sig"))
    old_names = {feature["name"] for feature in old["features"]}
    protocol = dict(version="draw-retrain-20261101-v1", sourceMatrixSHA256=digest(args.matrix),
        sourceDatasetSHA256=source.get("sourceSHA256"), oldModelSHA256=digest(args.old_artifact),
        configs=CONFIGS, split="actual Beijing kickoff dates; nearest whole-day 60/20/20; no date crosses periods",
        boundaries=boundaries, calibrationTargetJCDrawCoverage=.9,
        threshold="highest calibration threshold retaining ceil(0.9 * ALL calibration JC draws), all ties included",
        selection="highest calibration JC precision at the locked target, then calibration AP, then fewer trees, then model ID; never use final test",
        imputation="training-period medians only for development; full eligible fit rows for final refit; no missing flags",
        featureSelection="label blind; fixed old catalogue or all allowlisted source-derived features; finite count and nonconstancy on training rows only",
        zeroInput="no observed selected input excluded from fitting and prediction but retained in evaluation denominators",
        finalRefit="all current qualified finished observations in model scope; frozen chosen columns, hyperparameters and calibration threshold",
        currentSecondaryCompatibility="selected and score>=0.225 and actual SPF opening/latest complete and safe H2H>=5; not used for candidate selection",
        legacyHistoryRisk=dict(rows=int(risk_mask.sum()), jc=int(np.sum(risk_mask & marked)), derivedValuesMasked=masked_derived,
            action="mask non-market and non-safeHistory feature views before training; keep labels and pure market/safe-history inputs"),
        oldTrainingOverlap=dict(test=int(np.sum(masks["test"] & ~old_unseen)), testJC=int(np.sum(masks["test"] & marked & ~old_unseen)),
            testUnseen=int(np.sum(masks["test"] & old_unseen)), testJCUnseen=int(np.sum(masks["test"] & marked & old_unseen))),
        limitations=["latest historical snapshots can contain post-match or post-22:00 prices", "temporal label holdout is not prospective source-timestamp validation", "old baseline may have trained on some test dates", "test period has already been studied; this is internal temporal validation", "final full-fit metrics are in-sample, not independent test results"],
        software=dict(sklearn=sklearn.__version__, numpy=np.__version__),
        periods={name: dict(rows=int(mask.sum()), draws=int(np.sum(y & mask)), jc=int(np.sum(marked & mask)), jcDraws=int(np.sum(y & marked & mask))) for name, mask in masks.items()})
    args.out.mkdir(parents=True, exist_ok=True)
    protocol_path = args.out / "protocol.json"
    write(protocol_path, protocol)
    protocol_hash = digest(protocol_path)
    # Seal split membership before fitting or opening any test metrics.
    write(args.out / "split-membership.json", dict(boundaries=boundaries, rows=[dict(matchId=r["matchId"], kickoffDay=str(days[i]), period=next(name for name, mask in masks.items() if mask[i])) for i, r in enumerate(rows)]))
    np.savez_compressed(args.out / "feature-matrix.npz", X=matrix, names=np.asarray(names), ids=np.asarray([r["matchId"] for r in rows]), labels=labels)
    fits = []
    with threadpool_limits(limits=4):
        for config in CONFIGS:
            scope = marked if config["scope"] == "sporttery" else np.ones(len(rows), dtype=bool)
            training = masks["train"] & scope
            minimum = 20 if config["scope"] == "sporttery" else 40
            columns, exclusions = [], []
            for col, meta in enumerate(metadata):
                reason = None
                if config["view"] == "old_clean" and meta["name"] not in old_names:
                    reason = "not in frozen clean input catalogue"
                elif config["view"] == "all_recorded":
                    reason = excluded_feature(meta)
                if not reason:
                    finite = guarded[training, col]
                    finite = finite[np.isfinite(finite)]
                    if len(finite) < minimum or np.ptp(finite) == 0:
                        reason = "too few or constant observed training values"
                if reason:
                    exclusions.append(dict(feature=meta["name"], reason=reason))
                else:
                    columns.append(col)
            if not columns:
                raise ValueError("no trainable feature columns")
            available = np.isfinite(guarded[:, columns]).any(axis=1) & scope
            fit_mask = training & available
            imputer = SimpleImputer(strategy="median", add_indicator=False)
            x_train = imputer.fit_transform(guarded[np.ix_(fit_mask, columns)])
            transformed = imputer.transform(guarded[:, columns])
            model = HistGradientBoostingClassifier(max_iter=config["iterations"], max_depth=config["depth"],
                max_leaf_nodes=2 ** config["depth"], min_samples_leaf=config["leaf"], l2_regularization=10,
                learning_rate=.05, max_bins=32, early_stopping=False, random_state=20261101)
            model.fit(x_train, y[fit_mask])
            scores = model.predict_proba(transformed)[:, 1]
            threshold, threshold_audit = threshold_for_coverage(y, scores, masks["calibration"] & marked, available, .9)
            cal = evaluation(y, scores, threshold, available, marked, gates, dict(calibration=masks["calibration"]))
            folder = args.out / "models" / config["id"]
            folder.mkdir(parents=True, exist_ok=True)
            joblib.dump(dict(model=model, imputer=imputer, inputColumns=columns, inputFeatureNames=[names[c] for c in columns],
                threshold=threshold, scope=config["scope"], fitType="development_train_period_only", trainingIDs=[rows[i]["matchId"] for i in np.flatnonzero(fit_mask)]), folder / "development.joblib")
            record = dict(id=config["id"], config=config, inputFeatures=len(columns), fitRows=int(fit_mask.sum()),
                trainZeroInputExcluded=int(np.sum(training & ~available)), threshold=threshold, thresholdAudit=threshold_audit,
                calibration=cal["calibration"], selectedColumns=columns, excludedFeatures=exclusions)
            fits.append(dict(record=record, model=model, imputer=imputer, scores=scores, available=available, columns=columns, folder=folder, scope=scope))
            print(json.dumps(dict(stage="calibration", id=config["id"], features=len(columns), fitRows=int(fit_mask.sum()), threshold=threshold, jc=record["calibration"]["sporttery"]["original"])), flush=True)
        def ranking(fit):
            cal = fit["record"]["calibration"]["sporttery"]
            return (-(cal["original"]["precision"] or 0), -(cal["averagePrecision"] or 0), fit["record"]["config"]["iterations"], fit["record"]["id"])
        winner = min(fits, key=ranking)["record"]["id"]
        write(args.out / "selection-before-test.json", dict(winner=winner, selectedUsing="calibration only", protocolSHA256=protocol_hash,
            candidates=[dict(id=f["record"]["id"], calibration=f["record"]["calibration"], threshold=f["record"]["threshold"]) for f in fits]))
        # Test outcomes are evaluated only after the candidate choice is sealed.
        for fit in fits:
            record, columns, folder = fit["record"], fit["columns"], fit["folder"]
            record["developmentEvaluation"] = evaluation(y, fit["scores"], record["threshold"], fit["available"], marked, gates, evaluation_masks)
            full_mask = fit["scope"] & fit["available"]
            imputer = SimpleImputer(strategy="median", add_indicator=False)
            full_x = imputer.fit_transform(guarded[np.ix_(full_mask, columns)])
            model = HistGradientBoostingClassifier(**fit["model"].get_params())
            model.fit(full_x, y[full_mask])
            all_x = imputer.transform(guarded[:, columns])
            scores = model.predict_proba(all_x)[:, 1]
            artifact_path = folder / "full.joblib"
            joblib.dump(dict(model=model, imputer=imputer, inputColumns=columns, inputFeatureNames=[names[c] for c in columns],
                threshold=record["threshold"], scope=record["config"]["scope"], fitType="all_available_labels_final_refit",
                trainingIDs=[rows[i]["matchId"] for i in np.flatnonzero(full_mask)]), artifact_path)
            portable_model = portable(model, imputer, columns, metadata, names, record["threshold"], record["config"], protocol_hash,
                "draw-retrained-20261101-" + record["id"])
            portable_model["sourceModelSHA256"] = digest(artifact_path)
            portable_model["trainingCohort"] = dict(total=len(rows), draws=int(y.sum()), sporttery=int(marked.sum()),
                sportteryDraws=int(np.sum(y & marked)), fitRows=int(full_mask.sum()), fitDraws=int(np.sum(y & full_mask)))
            write(folder / "candidate.json", portable_model)
            record["fullFitEvaluationInSampleOnly"] = evaluation(y, scores, record["threshold"], fit["available"], marked, gates, dict(all=np.ones(len(rows), dtype=bool)))["all"]
            record["fullFitRows"], record["fullFitZeroInputExcluded"] = int(full_mask.sum()), int(np.sum(fit["scope"] & ~fit["available"]))
            record["joblibPath"], record["candidatePath"] = str(artifact_path), str(folder / "candidate.json")
            record["joblibSHA256"], record["candidateSHA256"] = digest(artifact_path), digest(folder / "candidate.json")
            np.savez_compressed(folder / "scores.npz", ids=np.asarray([r["matchId"] for r in rows]), labels=labels,
                development=fit["scores"], full=scores, available=fit["available"], marked=marked)
            write(folder / "metrics.json", record)
            print(json.dumps(dict(stage="full_refit", id=record["id"], fitRows=record["fullFitRows"], test=record["developmentEvaluation"]["test"]["sporttery"]["original"], full=record["fullFitEvaluationInSampleOnly"]["sporttery"]["original"])), flush=True)
    old_scores = np.asarray([np.nan if r.get("oldScore") is None else r["oldScore"] for r in rows])
    old_available = np.isfinite(old_scores) & np.asarray([r.get("oldObservedInputs", 0) > 0 for r in rows])
    old_evaluation = evaluation(y, old_scores, old["threshold"], old_available, marked, gates, evaluation_masks)
    summary = dict(version=protocol["version"], generatedAt=datetime.now(timezone.utc).isoformat(),
        protocolSHA256=protocol_hash, sourceGeneratedAt=source.get("sourceGeneratedAt"), sourceDatasetSHA256=source.get("sourceSHA256"),
        matrixSHA256=digest(args.matrix), productionModelSHA256=digest(args.old_artifact), productionModelUnchanged=True,
        rows=len(rows), draws=int(y.sum()), sporttery=int(marked.sum()), sportteryDraws=int(np.sum(y & marked)),
        winner=winner, winnerSelectedBeforeTest=True, productionSwitched=False, protocol=protocol,
        oldFrozenBaseline=old_evaluation, candidates=[fit["record"] for fit in fits], elapsedSeconds=time.perf_counter() - started)
    write(args.out / "training-summary.json", summary)
    print(json.dumps(dict(complete=True, winner=winner, rows=len(rows), elapsedSeconds=summary["elapsedSeconds"], summary=str(args.out / "training-summary.json"))), flush=True)


if __name__ == "__main__":
    main()
