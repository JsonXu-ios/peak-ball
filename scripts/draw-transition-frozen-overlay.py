#!/usr/bin/env python3
"""Observe fixed development top-five rules AND/OR the exact frozen old model.

No fitting, threshold adjustment, production updates, or service access. IDs are
explicitly aligned; any missing exact model evidence aborts rather than guesses.
"""
from __future__ import annotations

import hashlib
import importlib.util
import json
from pathlib import Path

import numpy as np

ROOT = Path(__file__).resolve().parents[1]
FOLDER = ROOT / "reports/draw-transition-study"
DATASET = ROOT / "reports/draw-full-feature-study/dataset.json"
OLD_SCORES = ROOT / "reports/draw-target-900-study/model-scores.npz"
OLD_META = ROOT / "reports/draw-target-900-study/models.json"
SCOPE_SCORES = ROOT / "reports/draw-sporttery-study/scores.npz"
MODEL = "clean_d3_l40_i240"
THRESHOLD = 0.2016984853563883


def load(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def aligned(array, ids, reference):
    names = [str(value) for value in ids]
    if len(set(names)) != len(names) or set(names) != set(reference):
        raise ValueError("Score IDs are not a unique exact match to the current frozen dataset")
    positions = {identity: i for i, identity in enumerate(names)}
    return np.asarray(array)[[positions[identity] for identity in reference]]


def metric(selection, universe, labels, ids, new_valid):
    selected = selection & universe
    tp = int(np.sum(selected & labels))
    n = int(selected.sum())
    total_draws = int(np.sum(labels & universe))
    return {"cohortMatches": int(universe.sum()), "cohortDraws": total_draws,
            "n": n, "TP": tp, "FP": n - tp, "precision": tp / n if n else None,
            "recall": tp / total_draws if total_draws else None,
            "selectedWithoutNewConditionInputs": int(np.sum(selected & ~new_valid)),
            "selectedDrawsWithoutNewConditionInputs": int(np.sum(selected & ~new_valid & labels)),
            "selectedMatchIds": ids[selected].tolist()}


def main():
    script = ROOT / "scripts/draw-transition-study.py"
    used_study_sha = sha(script)
    spec = importlib.util.spec_from_file_location("fixed_transition_overlay_source", script)
    study = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(study)
    rows = load(DATASET)["rows"]
    ids = np.asarray([str(row["matchId"]) for row in rows])
    labels = np.asarray([row["result"] == 1 for row in rows], dtype=bool)
    results = np.asarray([row["result"] for row in rows])
    marked = np.asarray([bool(str(row.get("jingcaiId") or "").strip()) for row in rows])
    assert len(rows) == 4161 and len(set(ids)) == 4161 and int(labels.sum()) == 970
    research_bytes = {name: (FOLDER / name).read_bytes() for name in ("analysis.json", "catalog.json", "inputs.json")}
    analysis, catalog, saved_inputs = (json.loads(research_bytes[name]) for name in ("analysis.json", "catalog.json", "inputs.json"))
    fixed_research_hashes = {name: hashlib.sha256(content).hexdigest() for name, content in research_bytes.items()}
    assert analysis["datasetSha256"] == sha(DATASET)
    inputs_by_id = {str(row["matchId"]): row for row in saved_inputs}
    assert len(inputs_by_id) == len(rows) and set(inputs_by_id) == set(ids)
    xs = [study.features(row) for row in rows]
    for row, features in zip(rows, xs):
        previous = inputs_by_id[row["matchId"]]
        assert previous["result"] == row["result"]
        assert json.loads(json.dumps(features)) == previous["inputs"]
    roots, modifiers = study.make_conditions(xs)
    atoms = {row["id"]: row for row in roots + modifiers}
    assert len(atoms) == len(roots) + len(modifiers)
    stored_atoms = {row["id"]: row for row in catalog["roots"] + catalog["modifiers"]}
    assert set(atoms) == set(stored_atoms)
    candidates = {row["id"]: row for row in catalog["candidates"]}

    metadata = load(OLD_META)
    assert metadata["scoresSHA256"] == sha(OLD_SCORES)
    matching = [row for row in metadata["models"] if row["id"] == MODEL]
    assert len(matching) == 1 and matching[0]["at900"]["threshold"] == THRESHOLD
    old = np.load(OLD_SCORES, allow_pickle=False)
    assert MODEL in old.files and MODEL + "_selected" in old.files
    probabilities = aligned(old[MODEL], old["matchIds"], ids)
    assert probabilities.shape == (4161,) and np.isfinite(probabilities).all()
    assert np.array_equal(aligned(old["labels"], old["matchIds"], ids), results)
    frozen = probabilities >= THRESHOLD
    assert np.array_equal(frozen, aligned(old[MODEL + "_selected"], old["matchIds"], ids))
    assert (int(frozen.sum()), int(np.sum(frozen & labels))) == (2518, 900)
    assert (int(np.sum(frozen & marked)), int(np.sum(frozen & marked & labels))) == (320, 122)
    independent = np.load(SCOPE_SCORES, allow_pickle=False)
    scope_key = "oldFrozen_marked_" + MODEL
    assert np.array_equal(aligned(independent["labels"], independent["matchIds"], ids), results)
    assert np.array_equal(aligned(independent[scope_key + "_scores"], independent["matchIds"], ids), probabilities)
    assert np.array_equal(aligned(independent[scope_key + "_selected"], independent["matchIds"], ids), frozen & marked)
    _, groups, cuts = study.time_groups(rows)
    assert cuts == analysis["timeCuts"]
    periods = {"all": np.ones(len(rows), dtype=bool), "development": groups < 2,
               "period1": groups == 0, "period2": groups == 1, "period3": groups == 2}
    output = {"version": "fixed-top-five-frozen-overlay-v1", "modelId": MODEL, "threshold": THRESHOLD,
              "modelFit": "Old model was trained on all 4161 labels. No period here is independent model validation.",
              "ruleSelection": "Each cohort's first five topDevelopment entries, in their existing development order; never ranked or altered by overlay outcomes.",
              "operations": "AND=both selected; OR=either selected. Missing new-condition inputs are not guessed: new=false; OR may still select from old model. Full cohort draw denominator is retained.",
              "latestThird": "Internal historical description only: old model also used these labels. This is not an independent test of the overlay.",
              "timeCuts": cuts, "cohorts": {}, "independentValidation": False,
              "finalResearchCandidateCount": analysis["candidateCount"],
              "validation": {"idAlignment": "explicit unique ID mapping", "fullFeatureReextractRows": len(rows), "oldSelectedMaskThresholdMatch": True,
                             "sportterySavedScoresAndSelectionsCrosscheck": True, "noRetraining": True, "noThresholdAdjustment": True, "noServiceAccess": True}}
    comparisons = 0
    for name, scope in (("all", np.ones(len(rows), dtype=bool)), ("sporttery", marked)):
        section = {"baseline": metric(scope, scope, labels, ids, np.ones(len(rows), dtype=bool)),
                   "oldFrozen": metric(frozen, scope, labels, ids, np.ones(len(rows), dtype=bool)), "topFive": []}
        for rank, rule in enumerate(analysis["cohorts"][name]["topDevelopment"][:5], start=1):
            assert rule["id"] in candidates and candidates[rule["id"]]["parts"] == rule["parts"]
            parts = [atoms[identity] for identity in rule["parts"]]
            new = np.logical_and.reduce([part["mask"] for part in parts])
            valid = np.logical_and.reduce([part["valid"] for part in parts])
            assert not np.any(new & ~valid)
            assert (int(np.sum(new & scope)), int(np.sum(new & scope & labels))) == (rule["metrics"]["all"]["n"], rule["metrics"]["all"]["draws"])
            masks = {"newCondition": new, "oldFrozen": frozen, "AND": new & frozen, "OR": new | frozen,
                     "newOnlyAddedToOld": new & ~frozen, "oldOnlyRemovedByAND": frozen & ~new,
                     "oldOnNewCompleteInputs": frozen & valid}
            item = {"developmentRank": rank, "id": rule["id"], "label": rule["label"], "parts": rule["parts"],
                    "sourceAvailable": metric(valid, scope, labels, ids, valid), "periods": {}}
            for period, subset in periods.items():
                universe = scope & subset
                item["periods"][period] = {operation: metric(mask, universe, labels, ids, valid) for operation, mask in masks.items()}
                actual = item["periods"][period]
                assert actual["AND"]["n"] + actual["OR"]["n"] == actual["newCondition"]["n"] + actual["oldFrozen"]["n"]
                assert actual["AND"]["TP"] + actual["OR"]["TP"] == actual["newCondition"]["TP"] + actual["oldFrozen"]["TP"]
                assert actual["OR"]["TP"] - actual["oldFrozen"]["TP"] == actual["newOnlyAddedToOld"]["TP"]
                for key, value in actual.items():
                    assert value["n"] == value["TP"] + value["FP"]
                    assert set(value["selectedMatchIds"]) <= set(ids[universe])
                comparisons += len(masks)
            section["topFive"].append(item)
        output["cohorts"][name] = section
    paths = [Path(__file__), script, DATASET, FOLDER / "analysis.json", FOLDER / "catalog.json", FOLDER / "inputs.json", OLD_SCORES, OLD_META, SCOPE_SCORES]
    assert all(sha(FOLDER / name) == expected for name, expected in fixed_research_hashes.items()), "Research changed during overlay; rerun after final snapshot"
    assert sha(script) == used_study_sha, "Research calculation source changed during overlay; rerun"
    output["sourceHashes"] = {str(path.relative_to(ROOT)).replace('\\', '/'): sha(path) for path in paths}
    output["validation"]["metricGroups"] = comparisons
    (FOLDER / "frozen-overlay.json").write_text(json.dumps(output, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")
    compact = {name: {"old": {key: section["oldFrozen"][key] for key in ("n", "TP", "FP", "precision", "recall")},
                     "topFive": [{"rank": item["developmentRank"], "label": item["label"],
                                  "all": {operation: {key: item["periods"]["all"][operation][key] for key in ("n", "TP", "FP", "precision", "recall")} for operation in ("newCondition", "AND", "OR")},
                                  "period3": {operation: {key: item["periods"]["period3"][operation][key] for key in ("n", "TP", "FP", "precision", "recall")} for operation in ("newCondition", "oldFrozen", "AND", "OR")}}
                                 for item in section["topFive"]]} for name, section in output["cohorts"].items()}
    print(json.dumps({"status": "passed", "metricGroups": comparisons, "result": compact}, ensure_ascii=True, indent=2))


if __name__ == "__main__":
    main()
