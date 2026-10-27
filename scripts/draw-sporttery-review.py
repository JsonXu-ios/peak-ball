#!/usr/bin/env python3
"""Independent arithmetic/source review of the local Sporttery scope study.

Reads frozen local files only. Never accesses services, DB, Redis or network.
The review reimplements scope definitions, counts and tie-preserving threshold
selection; it never calls the production study's metric or scope functions.
"""
from __future__ import annotations

import csv
import hashlib
import json
import math
import os
import re
from pathlib import Path

for variable in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
    os.environ.setdefault(variable, "4")

import numpy as np
import joblib

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-sporttery-study"


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def load_raw():
    base = ROOT / "reports/draw-full-feature-study"
    rows = read(base / "dataset.json")["rows"]
    ids = np.asarray([str(row["matchId"]) for row in rows])
    labels = np.asarray([row["result"] for row in rows], dtype=int)
    metadata = read(base / "feature-manifest.json")
    with (base / "features.csv").open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.reader(stream)
        header = next(reader)
        entries = list(reader)
    assert header[2:] == [column["name"] for column in metadata]
    assert [entry[0] for entry in entries] == ids.tolist()
    assert np.array_equal([int(entry[1]) for entry in entries], labels)
    matrices = [np.asarray([[float(value) if value else np.nan for value in entry[2:]] for entry in entries])]
    for folder, filename, metaname in [
        ("draw-conflict-study", "extra-features.npz", "extra-feature-manifest.json"),
        ("draw-historical-value-study", "context-features.npz", "context-feature-manifest.json"),
    ]:
        saved = np.load(ROOT / "reports" / folder / filename, allow_pickle=False)
        extra = read(ROOT / "reports" / folder / metaname)
        assert np.array_equal(saved["matchIds"].astype(str), ids)
        values = saved["values"] if "values" in saved else saved["X"]
        assert values.shape == (len(rows), len(extra))
        matrices.append(values)
        metadata += extra
    matrix = np.column_stack(matrices)
    assert len(set(ids)) == len(ids) == 4161 and matrix.shape == (4161, 530)
    assert np.count_nonzero(labels == 1) == 970
    return rows, ids, labels, matrix, metadata


def valid_odds(values):
    return (isinstance(values, list) and len(values) == 3
            and all(isinstance(value, (float, int)) and not isinstance(value, bool)
                    and math.isfinite(value) and value > 1 for value in values))


def raw_scopes(rows):
    identified = np.asarray([bool(str(row.get("jingcaiId") or "").strip()) for row in rows])
    opening = np.asarray([valid_odds(row.get("opening", {}).get("sporttery")) for row in rows])
    latest = np.asarray([valid_odds(row.get("latest", {}).get("sporttery")) for row in rows])
    return {"jingcaiId": identified, "jingcaiIdAndBothOdds": identified & opening & latest,
            "realSportteryLatestComplete": latest}, opening, latest


def stats(labels, selected, target):
    selected = np.asarray(selected, dtype=bool)
    positive = np.asarray(labels) == 1
    tp = int(np.count_nonzero(selected & positive))
    fp = int(np.count_nonzero(selected & ~positive))
    n = tp + fp
    return {"selected": n, "TP": tp, "FP": fp, "FN": int(positive.sum()) - tp,
            "TN": int((~positive).sum()) - fp, "precision": tp/n if n else None,
            "recall": tp/int(positive.sum()) if positive.any() else None,
            "home": int(np.count_nonzero(selected & (np.asarray(labels) == 0))),
            "away": int(np.count_nonzero(selected & (np.asarray(labels) == 2))),
            "targetReached": tp >= target and bool(n) and tp/n >= .30}


def audit_metrics(reported, actual):
    aliases = {"draw": "TP", "draws": "TP", "falseSelections": "FP", "tp": "TP", "fp": "FP"}
    for original, value in reported.items():
        name = aliases.get(original, original)
        if name not in actual:
            continue
        expected = actual[name]
        if isinstance(expected, float):
            assert value is not None and math.isclose(value, expected, abs_tol=1e-12, rel_tol=1e-12), (name, value, expected)
        else:
            assert value == expected, (name, value, expected)


def threshold_mask(scores, labels, target):
    """Walk every score, then retain the entire boundary score group."""
    scores = np.asarray(scores)
    assert np.isfinite(scores).all()
    order = np.argsort(-scores, kind="stable")
    positives = np.cumsum(np.asarray(labels)[order] == 1)
    boundary = int(np.flatnonzero(positives >= target)[0])
    threshold = float(scores[order[boundary]])
    result = scores >= threshold
    assert np.count_nonzero((scores > threshold) & (np.asarray(labels) == 1)) < target
    return threshold, result


def same_count_mask(scores, count):
    order = np.argsort(-np.asarray(scores), kind="stable")
    boundary = float(np.asarray(scores)[order[count - 1]])
    return boundary, np.asarray(scores) >= boundary


def source_audit(sources):
    checked = {}
    for relative, expected in sources.items():
        observed = sha(ROOT / relative.replace("\\", "/"))
        assert observed == expected, (relative, observed, expected)
        checked[relative] = observed
    return checked


def qualified_matrix(matrix, metadata):
    """Independently apply minimum safe-history sample guards."""
    guarded = matrix.copy()
    cols = {entry["name"]: j for j, entry in enumerate(metadata)}
    for j, entry in enumerate(metadata):
        name = entry["name"]
        recent = re.fullmatch(r"safeRecent\.(home|away)\.(5|10)\..+", name)
        h2h = re.fullmatch(r"safeH2H\.([^.]*)\..+", name)
        conflict = re.fullmatch(r"conflict\.recent\.(home|away)\..+", name)
        valid = np.ones(len(matrix), dtype=bool)
        if recent:
            valid = matrix[:, cols[f"safeRecent.{recent[1]}.{recent[2]}.n"]] >= 5
        elif h2h:
            valid = matrix[:, cols[f"safeH2H.{h2h[1]}.n"]] >= 1
        elif name.startswith("safeRecent.pair."):
            valid = ((matrix[:, cols["safeRecent.home.10.n"]] >= 5)
                     & (matrix[:, cols["safeRecent.away.10.n"]] >= 5))
        elif conflict:
            three = re.match(r"conflict\.recent\.(home|away)\.3\.", name) is not None
            count = f"conflict.recent.{conflict[1]}.3.n" if three else f"safeRecent.{conflict[1]}.10.n"
            valid = matrix[:, cols[count]] >= (3 if three else 5)
        guarded[~valid, j] = np.nan
    return guarded


def assert_allowed_names(names, metadata):
    by_name = {entry["name"]: entry for entry in metadata}
    for name in names:
        item = by_name[name]
        lower = name.lower()
        assert not (name.startswith("availability.") or "ambiguous" in lower or "missing" in lower
                    or item.get("denominatorOnly") or name.endswith(".n")
                    or re.search(r"\.(recentHomeMatches|recentGuestMatches|homeSamples|awaySamples|matches|sampleCount|samples)$", name, re.I)
                    or any(part in lower for part in ("matchid", "kickoff", "timestamp", "teamid", "resultlabel"))), name


def independently_selected_columns(guarded, metadata, marked, view):
    selected = []
    for j, item in enumerate(metadata):
        name = item["name"]
        try:
            assert_allowed_names([name], metadata)
        except AssertionError:
            continue
        allowed = name.startswith("euro.sporttery.")
        if view == "jcHistory":
            allowed = allowed or item["view"] == "safeHistory"
        elif view == "historyOnly":
            allowed = item["view"] == "safeHistory"
        elif view == "clean":
            allowed = item["view"] in ("market", "safeHistory")
        finite = guarded[marked, j]
        finite = finite[np.isfinite(finite)]
        if allowed and len(finite) >= 20 and finite.min() < finite.max():
            selected.append(j)
    return selected


def audit_models(rows, ids, labels, matrix, metadata, scopes):
    report = read(OUT / "analysis.json")
    hashes = source_audit(report["sourceHashes"])
    assert sha(OUT / "scores.npz") == report["scoresSHA256"]
    saved = np.load(OUT / "scores.npz", allow_pickle=False)
    assert np.array_equal(saved["matchIds"].astype(str), ids)
    assert np.array_equal(saved["labels"], labels)
    actual_scopes = {"marked": scopes["jingcaiId"], "markedBothQuotes": scopes["jingcaiIdAndBothOdds"],
                     "latestQuoteEvidence": scopes["realSportteryLatestComplete"]}
    for name, mask in actual_scopes.items():
        entry = report["scopes"][name]
        target = math.ceil(np.count_nonzero(mask & (labels == 1)) * 900 / 970)
        assert np.array_equal(saved[entry["scopeKey"]], mask)
        assert entry["matchIds"] == ids[mask].tolist()
        audit_metrics(entry, stats(labels[mask], np.ones(mask.sum(), dtype=bool), target))
        assert entry["scopeGames"] == int(mask.sum()) and entry["scopeDraws"] == int(np.count_nonzero(mask & (labels == 1)))
        assert entry["targetDraws"] == target
    old_folder = ROOT / "reports/draw-target-900-study"
    old = read(old_folder / "models.json")
    oldmodels = {item["id"]: item for item in old["models"]}
    olds = np.load(old_folder / "model-scores.npz", allow_pickle=False)
    guarded = qualified_matrix(matrix, metadata)
    known = np.asarray([valid_odds(row.get("latest", {}).get("sporttery")) for row in rows])
    raw_probability = np.full(len(rows), np.nan)
    for i in np.flatnonzero(known):
        inverse = 1 / np.asarray(rows[i]["latest"]["sporttery"], dtype=float)
        raw_probability[i] = inverse[1] / inverse.sum()
    resultids, artifact_predictions, own_labels = set(), 0, 0
    model_artifacts = {}
    controls = []
    for item in report["results"]:
        assert item["id"] not in resultids
        resultids.add(item["id"])
        scope = actual_scopes[item["scopeId"]]
        lab = saved[item["labelsKey"]]
        score = saved[item["scoresKey"]]
        chosen = saved[item["selectedKey"]]
        assert len(score) == len(labels) == len(lab) and chosen.dtype == np.bool_
        assert not np.any(chosen & ~scope) and np.isfinite(score[scope]).all()
        assert item["positiveLabel"] == 1
        target = math.ceil(np.count_nonzero(scope & (lab == 1)) * 900 / 970)
        if item["thresholdMode"] == "old_global_threshold_frozen":
            threshold = oldmodels[item["oldModelId"]]["at900"]["threshold"]
            assert item["threshold"] == threshold
        else:
            threshold, _ = threshold_mask(score[scope], lab[scope], target)
            assert item["threshold"] == threshold
        expected = scope & (score >= threshold)
        assert np.array_equal(chosen, expected), item["id"]
        assert item["selectedMatchIds"] == ids[expected].tolist()
        assert item["tiesAtThresholdInScope"] == int(np.count_nonzero(scope & (score == threshold)))
        audit_metrics(item["stats"], stats(lab[scope], expected[scope], target))
        assert item["stats"]["targetDraws"] == target
        assert item["stats"]["scopeGames"] == int(scope.sum())
        assert item["stats"]["scopeDraws"] == int(np.count_nonzero(scope & (lab == 1)))
        if item["kind"] in ("global_fit_threshold_frozen", "global_fit_subset_threshold"):
            assert np.array_equal(score, olds[item["oldModelId"]])
        elif item["kind"] == "sporttery_quote_baseline":
            expected_score = np.where(known, raw_probability, np.mean(labels[scope] == 1))
            assert np.allclose(score, expected_score, atol=1e-14, rtol=0)
            assert item["missingQuoteRows"] == int(np.count_nonzero(scope & ~known))
        elif item["kind"] == "subset_full_history_fit":
            assert item["scopeId"] == "marked"
            artifact = ROOT / item["modelArtifact"].replace("\\", "/")
            assert sha(artifact) == item["modelArtifactSHA256"]
            value = joblib.load(artifact)
            cols = value["inputColumns"]
            feature_names = [metadata[j]["name"] for j in cols]
            assert feature_names == value["inputNames"] == [field["name"] for field in item["inputFields"]]
            assert len(cols) == item["inputFeatureCount"]
            assert cols == independently_selected_columns(guarded, metadata, scope, item["view"])
            assert_allowed_names(feature_names, metadata)
            assert value["model"].get_params() == item["parameters"]
            assert not value["imputer"].add_indicator
            data = guarded[scope][:, cols]
            assert np.allclose(value["imputer"].statistics_, np.nanmedian(data, axis=0), atol=0, rtol=0)
            expected_score = value["model"].predict_proba(value["imputer"].transform(data))[:, 1]
            assert np.allclose(score[scope], expected_score, atol=1e-14, rtol=0)
            assert np.isnan(score[~scope]).all()
            artifact_predictions += len(expected_score)
            model_artifacts[item["configId"]] = value
        elif item["kind"] == "shuffled_training_labels_capacity_diagnostic":
            assert item["scopeId"] == "marked"
            expected_labels = np.zeros(len(labels), dtype=int)
            expected_labels[scope] = np.random.default_rng(item["permutationSeed"]).permutation((labels[scope] == 1).astype(int))
            assert np.array_equal(lab, expected_labels)
            assert np.isnan(score[~scope]).all()
            assert item["fixedConfig"] == "jcHistory_d3_l40_i240"
            controls.append(item)
            own_labels += int(scope.sum())
        else:
            raise AssertionError(("unknown result kind", item["kind"]))
    # Independently refit the fixed negative controls. This verifies that the
    # saved arbitrary labels were actually used for their own fits.
    from sklearn.ensemble import HistGradientBoostingClassifier
    from threadpoolctl import threadpool_limits
    fixed = model_artifacts["jcHistory_d3_l40_i240"]
    scope = actual_scopes["marked"]
    data = fixed["imputer"].transform(guarded[scope][:, fixed["inputColumns"]])
    with threadpool_limits(limits=4):
        for item in controls:
            model = HistGradientBoostingClassifier(**fixed["model"].get_params())
            model.fit(data, saved[item["labelsKey"]][scope])
            prediction = model.predict_proba(data)[:, 1]
            assert np.allclose(prediction, saved[item["scoresKey"]][scope], atol=1e-14, rtol=0)
    assert len(report["results"]) == 66 and len(model_artifacts) == 6 and len(controls) == 3
    return {"issues": [], "resultConfigurations": len(resultids), "savedRowsPerResult": len(ids),
            "scopeMasks": len(actual_scopes), "modelArtifactsIndependentlyPredicted": len(model_artifacts),
            "artifactRowsPredicted": artifact_predictions, "independentlyRefittedNegativeControls": len(controls),
            "ownRandomLabelRows": own_labels, "sourceHashes": hashes,
            "outputs": {name: sha(OUT / name) for name in ("analysis.json", "scores.npz")}}


def predicate(atom, matrix, columns):
    values = matrix[:, columns[atom["feature"]]]
    if atom.get("transform") == "abs":
        values = np.abs(values)
    compare = {"le": np.less_equal, "ge": np.greater_equal, "lt": np.less,
               "gt": np.greater, "eq": np.equal}[atom["op"]]
    mask = np.isfinite(values) & compare(values, atom["value"])
    for guard in atom.get("guards", []):
        mask &= predicate(guard, matrix, columns)
    return mask


def audit_rules(ids, labels, matrix, metadata, scopes):
    saved = read(OUT / "rules.json")
    hashes = source_audit(saved["sources"])
    columns = {entry["name"]: j for j, entry in enumerate(metadata)}
    views = {entry["name"]: entry["view"] for entry in metadata}
    rules = {rule["id"]: rule for rule in saved["rules"]}
    assert len(rules) == len(saved["rules"])
    masks = {}
    for name, rule in rules.items():
        assert 1 <= len(rule["allOf"]) <= 2
        result = np.ones(len(labels), dtype=bool)
        for atom in rule["allOf"]:
            assert atom["feature"] in columns and not atom["feature"].endswith(".n")
            result &= predicate(atom, matrix, columns)
        masks[name] = result
    old = read(ROOT / "reports/draw-target-900-study/rules.json")
    oldbest = next(item for item in old["configurations"] if item["id"] == old["bestConfigurationId"])
    frozen_ruleids = [step["ruleId"] for step in oldbest["steps"]]
    results, steps = {}, 0
    for item in saved["configurations"]:
        scope = scopes[item["scope"]]
        target = math.ceil(int(np.count_nonzero(scope & (labels == 1))) * 900 / 970)
        selected = scope.copy()
        if item["fittedOnSubset"]:
            assert len(item["ruleIds"]) <= item["maximumRules"]
            assert item["ruleIds"] == [step["ruleId"] for step in item["steps"]]
            for step in item["steps"]:
                rule = rules[step["ruleId"]]
                if item["source"] == "sportteryMarketAndSafeHistory":
                    for atom in rule["allOf"]:
                        assert views[atom["feature"]] == "safeHistory" or atom["feature"].startswith("euro.sporttery."), atom
                removed = selected & masks[step["ruleId"]]
                assert step["removedDraws"] == int(np.count_nonzero(removed & (labels == 1)))
                assert step["removedNonDraws"] == int(np.count_nonzero(removed & (labels != 1)))
                selected &= ~masks[step["ruleId"]]
                audit_metrics(step["remaining"], stats(labels[scope], selected[scope], target))
                steps += 1
        else:
            assert item["ruleIds"] == frozen_ruleids
            for name in item["ruleIds"]:
                selected &= ~masks[name]
        audit_metrics(item["stats"], stats(labels[scope], selected[scope], target))
        assert item["stats"]["minimumDraws"] == target
        results[item["id"]] = selected
    csvfile = OUT / "rule-selections.csv"
    with csvfile.open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.DictReader(stream)
        n = 0
        for i, row in enumerate(reader):
            assert row["matchId"] == ids[i] and int(row["result"]) == labels[i]
            assert int(row["hasJingcaiId"]) == int(scopes["jingcaiId"][i])
            assert int(row["realSportteryLatestComplete"]) == int(scopes["realSportteryLatestComplete"][i])
            for name, selected in results.items():
                assert int(row[name]) == int(selected[i]), (name, i)
            n += 1
        assert n == len(ids)
    for best in saved["bestByScopeSourceLimit"]:
        eligible = [item for item in saved["configurations"] if item["scope"] == best["scope"]
                    and item["source"] == best["source"] and item.get("maximumRules") == best["maximumRules"]]
        winner = min(eligible, key=lambda item: (item["stats"]["falseSelections"], -item["stats"]["draw"], len(item["ruleIds"]), item["id"]))
        assert best["id"] == winner["id"] and best["stats"] == winner["stats"]
    return {"issues": [], "rules": len(rules), "configurations": len(results), "steps": steps,
            "csvRowDecisions": len(results) * len(ids), "sourceHashes": hashes,
            "outputs": {name: sha(OUT / name) for name in ("rules.json", "rule-selections.csv")},
            "bestByScopeSourceLimit": saved["bestByScopeSourceLimit"]}


def audit_rule_controls(ids, labels, matrix, metadata, scopes):
    report = read(OUT / "rule-control.json")
    hashes = source_audit(report["sources"])
    assert report["formalPermutationPValue"] is False
    actual = read(OUT / "rules.json")
    fixed = report["fixedConfiguration"]
    actual_config = next(item for item in actual["configurations"] if item["id"] == fixed["actualWinnerId"])
    assert actual_config["stats"] == report["originalHistoricalFit"]
    assert fixed["algorithm"] == actual_config["algorithm"] == "penalty"
    assert fixed["coefficient"] == actual_config["coefficient"] == 5
    assert fixed["maximumRules"] == actual_config["maximumRules"] == 16
    marked = scopes["jingcaiId"]
    subset_ids, subset_labels, subset_matrix = ids[marked], labels[marked], matrix[marked]
    cols = {item["name"]: j for j, item in enumerate(metadata)}
    views = {item["name"]: item["view"] for item in metadata}
    catalogue = read(ROOT / "reports/draw-historical-value-study/catalog.json")
    atoms = {item["id"]: item for item in catalogue["atoms"]}
    totalsteps, counts = 0, []
    for item in report["results"]:
        own_labels = np.random.default_rng(item["seed"]).permutation(subset_labels)
        target = math.ceil(np.count_nonzero(own_labels == 1) * 900 / 970)
        assert target == fixed["minimumPositivesRetained"] == 116
        used = {rule["id"]: rule for rule in item["usedRules"]}
        assert len(used) == len(item["steps"]) == item["actualSelectedRules"] <= 16
        assert set(used) == {step["ruleId"] for step in item["steps"]}
        masks = {}
        for name, rule in used.items():
            assert 1 <= len(rule["allOf"]) <= 2
            selected = np.ones(len(subset_ids), dtype=bool)
            atomids = []
            for atom in rule["allOf"]:
                assert atom == atoms[atom["id"]]
                assert views[atom["feature"]] == "safeHistory" or atom["feature"].startswith("euro.sporttery.")
                assert not atom["feature"].endswith(".n")
                selected &= predicate(atom, subset_matrix, cols)
                atomids.append(atom["id"])
            expected_id = "r_" + hashlib.sha256(json.dumps(sorted(atomids), ensure_ascii=False, sort_keys=True).encode()).hexdigest()[:16]
            assert name == expected_id
            assert np.count_nonzero(selected & (own_labels == 1)) <= 8
            assert np.count_nonzero(selected & (own_labels != 1)) >= 3
            masks[name] = selected
        kept = np.ones(len(subset_ids), dtype=bool)
        for step in item["steps"]:
            removed = kept & masks[step["ruleId"]]
            assert step["removedDraws"] == int(np.count_nonzero(removed & (own_labels == 1)))
            assert step["removedNonDraws"] == int(np.count_nonzero(removed & (own_labels != 1)))
            kept &= ~masks[step["ruleId"]]
            audit_metrics(step["remaining"], stats(own_labels, kept, target))
            assert step["remaining"]["minimumDraws"] == target
            totalsteps += 1
        audit_metrics(item["stats"], stats(own_labels, kept, target))
        assert item["selectedMatchIds"] == subset_ids[kept].tolist()
        counts.append({"seed": item["seed"], **stats(own_labels, kept, target)})
    assert len(report["results"]) == 3
    assert report["randomFitsReachingTarget"] == sum(item["targetReached"] for item in counts)
    return {"issues": [], "permutations": len(counts), "steps": totalsteps,
            "labelsGeneratedIndependentlyFromSeeds": True, "allPredicatesAndSelectedIdsVerified": True,
            "searchOptimalityOrFormalSignificanceTested": False, "results": counts,
            "sourceHashes": hashes, "outputs": {"rule-control.json": sha(OUT / "rule-control.json")}}


def main():
    # Study-specific outputs are audited after the producing agent seals them.
    rows, ids, labels, matrix, metadata = load_raw()
    scopes, opening, latest = raw_scopes(rows)
    diagnostics = {}
    for name, mask in scopes.items():
        target = math.ceil(int(np.count_nonzero(mask & (labels == 1))) * 900 / 970)
        diagnostics[name] = {"rows": int(mask.sum()), "draws": int(np.count_nonzero(mask & (labels == 1))),
                             "targetDraws": target, "home": int(np.count_nonzero(mask & (labels == 0))),
                             "away": int(np.count_nonzero(mask & (labels == 2)))}
    assert diagnostics["jingcaiId"] == {"rows": 511, "draws": 124, "targetDraws": 116, "home": 221, "away": 166}
    assert diagnostics["jingcaiIdAndBothOdds"] == {"rows": 471, "draws": 119, "targetDraws": 111, "home": 199, "away": 153}
    review = {"issues": [], "independentlyVerifiedScopes": diagnostics,
              "rules": audit_rules(ids, labels, matrix, metadata, scopes),
              "models": audit_models(rows, ids, labels, matrix, metadata, scopes),
              "ruleCapacityDiagnostic": audit_rule_controls(ids, labels, matrix, metadata, scopes),
              "scopeOfReview": "Independent local arithmetic, masks, source hashes, model predictions and fixed model-control refits; no predictive validation or rule-search optimum claim.",
              "reviewScriptSHA256": sha(Path(__file__))}
    (OUT / "review.json").write_text(json.dumps(review, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"issues": review["issues"], "rules": review["rules"]["configurations"],
                      "modelResults": review["models"]["resultConfigurations"],
                      "modelArtifacts": review["models"]["modelArtifactsIndependentlyPredicted"],
                      "refittedModelControls": review["models"]["independentlyRefittedNegativeControls"],
                      "ruleControlSteps": review["ruleCapacityDiagnostic"]["steps"],
                      "review": str(OUT / "review.json")}, ensure_ascii=False))


if __name__ == "__main__":
    main()
