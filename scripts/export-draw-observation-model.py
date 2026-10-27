#!/usr/bin/env python3
"""Export the existing 900-target model; never train or touch live services.

The artifact is for historical observation, not an independently validated
probability model.  Numeric feature extraction is a separate, frozen contract.
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
import math
import os
from pathlib import Path

for variable in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
    os.environ.setdefault(variable, "4")

import joblib
import numpy as np
from scipy.special import expit
from threadpoolctl import threadpool_limits

ROOT = Path(__file__).resolve().parents[1]
MODEL_ID = "clean_d3_l40_i240"
VERSION = "sporttery-draw-observation-v1"
OUTPUT = ROOT / "go_server/internal/drawmodeldata"
REPORT = ROOT / "reports/draw-observation-model"


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def write(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False) + "\n", encoding="utf-8")


def guards(name):
    if name.startswith(("safeRecent.home.", "safeRecent.away.")):
        return [{"feature": ".".join(name.split(".")[:3]) + ".n", "minimum": 5}]
    if name.startswith("safeH2H."):
        return [{"feature": ".".join(name.split(".")[:2]) + ".n", "minimum": 1}]
    if name.startswith("safeRecent.pair."):
        return [{"feature": f"safeRecent.{side}.10.n", "minimum": 5} for side in ("home", "away")]
    if name.startswith("conflict.recent."):
        pieces = name.split(".")
        if pieces[3] == "3":
            return [{"feature": f"conflict.recent.{pieces[2]}.3.n", "minimum": 3}]
        return [{"feature": f"safeRecent.{pieces[2]}.10.n", "minimum": 5}]
    return []


def transformed(raw, export):
    result = np.empty((len(raw), len(export["features"])), dtype=np.float64)
    for j, feature in enumerate(export["features"]):
        value = raw[:, feature["originalIndex"]].copy()
        valid = np.isfinite(value)
        for guard in feature["guards"]:
            observed = raw[:, export["originalFeatureNames"].index(guard["feature"])]
            valid &= np.isfinite(observed) & (observed >= guard["minimum"])
        result[:, j] = np.where(valid, value, feature["median"])
    return result


def predict(data, export):
    # Scalar traversal uses exported raw numeric thresholds, NOT histogram bins.
    raw = np.full(len(data), export["baseline"], dtype=np.float64)
    for tree in export["trees"]:
        for row in range(len(data)):
            index = 0
            while not tree[index]["leaf"]:
                node = tree[index]
                index = node["left"] if data[row, node["feature"]] <= node["threshold"] else node["right"]
            raw[row] += tree[index]["value"]
    return expit(raw)


def main():
    OUTPUT.mkdir(parents=True, exist_ok=True)
    REPORT.mkdir(parents=True, exist_ok=True)
    historical = module("observation_historical", ROOT / "scripts/draw-historical-value-study.py")
    previous = module("observation_original", ROOT / "scripts/draw-target-900-model.py")
    rows, matrix, labels, metadata, input_sources = historical.load_inputs(ROOT / "reports/draw-historical-value-study")
    artifact_path = ROOT / f"reports/draw-target-900-study/models/{MODEL_ID}.joblib"
    artifact = joblib.load(artifact_path)
    model, imputer = artifact["model"], artifact["imputer"]
    original_names = [item["name"] for item in metadata]
    names, indices = artifact["inputFeatureNames"], artifact["inputColumns"]
    assert len(names) == len(indices) == len(imputer.statistics_) == 391
    assert artifact["threshold"] == 0.2016984853563883
    assert model.n_iter_ == 240 and model.n_trees_per_iteration_ == 1
    assert list(model.classes_) == [False, True]
    trees, used = [], set()
    for stage in model._predictors:
        nodes = []
        for record in stage[0].nodes:
            assert not record["is_categorical"]
            if record["is_leaf"]:
                nodes.append({"leaf": True, "value": float(record["value"])})
            else:
                index = int(record["feature_idx"])
                used.add(index)
                nodes.append({"leaf": False, "feature": index, "threshold": float(record["num_threshold"]),
                              "left": int(record["left"]), "right": int(record["right"])})
        trees.append(nodes)
    feature_contract_paths = [ROOT / "scripts/draw_full_features.py", ROOT / "scripts/draw_conflict_features.py",
                              ROOT / "scripts/draw_historical_context.py", ROOT / "scripts/draw-target-900-model.py"]
    feature_contract_paths = [p for p in feature_contract_paths if p.exists()]
    export = {
        "schema": 1, "version": VERSION, "modelId": MODEL_ID,
        "threshold": artifact["threshold"], "thresholdComparison": ">=; include exact boundary ties",
        "baseline": float(model._baseline_prediction[0, 0]), "trees": trees,
        "features": [{"name": name, "originalIndex": index, "median": float(imputer.statistics_[j]),
                      "guards": guards(name), "usedInTrees": j in used,
                      "view": metadata[index]["view"], "label": metadata[index]["label"]}
                     for j, (name, index) in enumerate(zip(names, indices))],
        "originalFeatureNames": original_names,
        "sourceModelSHA256": sha(artifact_path),
        "featureContractSources": {str(p.relative_to(ROOT)).replace("\\", "/"): sha(p) for p in feature_contract_paths},
        "contract": {
            "input": "Raw named float64 feature values, before historical sample guards and median imputation. Unknown omitted/nonfinite; actual zero retained.",
            "history": "Safe deduplicated prior matches; exclude target ID; prior kickoff+2h strictly earlier; team-relative scores; recent window <=10.",
            "guard": "Per-feature guards applied before finite checks and frozen full-history median fill. No missing indicators.",
            "trees": "numeric raw threshold x<=threshold left, otherwise right; leaf values already include learning rate .05; add in stage order to baseline then sigmoid",
            "scope": "Apply observation candidate marker only to actual nonblank jingcaiId; model inputs also use other company prices and safe history.",
            "captureTimeKnownPrematch": False, "independentTestRows": 0,
            "fitType": "all_labels_in_sample_only", "scoreMeaning": "frozen model score; not a calibrated per-match probability or guaranteed 35% future precision",
            "neverUseAsInputs": ["target result", "target score", "target status", "match ID", "team ID", "kickoff timestamp"],
        },
        "historicalCohort": {"total": 4161, "draws": 970, "sporttery": 511, "sportteryDraws": 124,
                             "sportterySelected": 320, "sportterySelectedDraws": 122},
    }
    guarded = matrix.copy()
    columns = {name: index for index, name in enumerate(original_names)}
    for j, name in enumerate(original_names):
        guarded[~previous.historical_guard(matrix, columns, name), j] = np.nan
    expected_data = imputer.transform(guarded[:, indices])
    actual_data = transformed(matrix, export)
    assert np.array_equal(expected_data, actual_data)
    with threadpool_limits(limits=4):
        expected = model.predict_proba(expected_data)[:, 1]
    actual = predict(actual_data, export)
    score_file = ROOT / "reports/draw-target-900-study/model-scores.npz"
    saved = np.load(score_file, allow_pickle=False)
    assert np.array_equal(expected, saved[MODEL_ID])
    maximum_error = float(np.max(np.abs(expected - actual)))
    assert maximum_error <= 1e-12
    selected, expected_selected = actual >= export["threshold"], expected >= export["threshold"]
    assert np.array_equal(selected, expected_selected)
    marked = np.array([bool(str(row.get("jingcaiId") or "").strip()) for row in rows])
    assert (int(marked.sum()), int(np.sum(marked & (labels == 1)))) == (511, 124)
    assert (int(np.sum(selected & marked)), int(np.sum(selected & marked & (labels == 1)))) == (320, 122)
    path = OUTPUT / "observation_v1.json"
    write(path, export)
    all_ids = [str(row["matchId"]) for row in rows]
    expected_records = [{"matchId": mid, "score": float(score), "selected": bool(chosen), "sporttery": bool(jc)}
                        for mid, score, chosen, jc in zip(all_ids, actual, selected, marked)]
    write(REPORT / "frozen-scores.json", {"version": VERSION, "scores": expected_records})
    # Small runtime fixtures intentionally contain no outcome label. Include
    # missing input, guard failure, zero, and complete historical feature maps.
    positions = sorted(set([0, len(rows)-1] + np.flatnonzero(marked & selected).tolist()[:4]
                           + np.flatnonzero(marked & ~selected).tolist()[:4]
                           + np.argsort(abs(expected-export["threshold"]))[:4].tolist()))
    fixtures = []
    used_names = {names[j] for j in used}
    guard_names = {g["feature"] for j in used for g in export["features"][j]["guards"]}
    # Offline extractor reference: all 4161 rows, raw values BEFORE guards.
    # This is deliberately outside the embedded production artifact.
    reference_names = sorted(used_names | guard_names)
    reference_indices = [columns[name] for name in reference_names]
    write(REPORT / "feature-reference.json", {
        "version": VERSION, "names": reference_names,
        "rows": [{"matchId": mid,
                  "values": [float(value) if math.isfinite(value) else None for value in matrix[i, reference_indices]],
                  "score": float(actual[i]), "selected": bool(selected[i])}
                 for i, mid in enumerate(all_ids)],
    })
    for i in positions:
        features = {name: float(matrix[i, columns[name]]) for name in sorted(used_names | guard_names)
                    if math.isfinite(matrix[i, columns[name]])}
        fixtures.append({"name": f"historical-{all_ids[i]}", "features": features,
                         "score": float(actual[i]), "selected": bool(selected[i])})
    for title, features in [("all-missing", {}), ("all-observed-zero", {name: 0. for name in used_names | guard_names}),
                            ("history-guard-missing", {name: 100. for name in used_names if name.startswith(("safe", "conflict.recent"))})]:
        raw = np.full((1, len(original_names)), np.nan)
        for name, value in features.items():
            raw[0, columns[name]] = value
        score = float(predict(transformed(raw, export), export)[0])
        fixtures.append({"name": title, "features": features, "score": score, "selected": score >= export["threshold"]})
    write(OUTPUT / "testdata.json", fixtures)
    review = {"version": VERSION, "noTrainingPerformed": True, "rowsCompared": len(rows),
              "inputFeatures": len(names), "usedSplitFeatures": len(used), "trees": len(trees),
              "exactGuardedImputedMatrix": True, "matchesSavedModelScores": True,
              "portableMaximumAbsoluteError": maximum_error, "selectedMasksIdentical": True,
              "sporttery": {"rows": int(marked.sum()), "draws": int(np.sum(marked & (labels == 1))),
                            "selected": int(np.sum(selected & marked)), "selectedDraws": int(np.sum(selected & marked & (labels == 1)))},
              "runtimeFixtureCount": len(fixtures), "artifactSHA256": sha(path), "sourceModelSHA256": sha(artifact_path),
              "usedFeatureNames": sorted(used_names), "requiredGuardFeatureNames": sorted(guard_names),
              "inputSources": input_sources, "issues": []}
    write(REPORT / "export-review.json", review)
    print(json.dumps({key: value for key, value in review.items() if key not in ("usedFeatureNames", "inputSources")}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
