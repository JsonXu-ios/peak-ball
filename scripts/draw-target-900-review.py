#!/usr/bin/env python3
"""Independent arithmetic, source, rule-mask and winner-refit audit.

Reads frozen local artifacts only. Does not call the production study's
predicate, metrics, feature-selection or threshold functions.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import json
import math
import os
import re
from datetime import datetime, timezone
from pathlib import Path

for variable in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
    os.environ.setdefault(variable, "4")

import numpy as np
import joblib
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.impute import SimpleImputer
from sklearn.metrics import average_precision_score, brier_score_loss
from threadpoolctl import threadpool_limits

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-target-900-study"


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def array_sha(values):
    return hashlib.sha256(np.asarray(values, dtype="<f8").tobytes()).hexdigest()


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
    for folder, data_name, meta_name in [
        ("draw-conflict-study", "extra-features.npz", "extra-feature-manifest.json"),
        ("draw-historical-value-study", "context-features.npz", "context-feature-manifest.json"),
    ]:
        data = np.load(ROOT / "reports" / folder / data_name, allow_pickle=False)
        extra = read(ROOT / "reports" / folder / meta_name)
        assert np.array_equal(data["matchIds"].astype(str), ids)
        values = data["values"] if "values" in data else data["X"]
        assert values.shape == (len(rows), len(extra))
        matrices.append(values)
        metadata += extra
    matrix = np.column_stack(matrices)
    assert len(set(ids)) == len(ids) == 4161 and matrix.shape == (4161, 530)
    assert (labels == 1).sum() == 970
    assert np.isin(labels, [0, 1, 2]).all()
    return rows, ids, labels, matrix, metadata


def result_counts(labels, selected):
    home, draw, away = [int(np.count_nonzero(selected & (labels == k))) for k in range(3)]
    total = home + draw + away
    return {"selected": total, "home": home, "draw": draw, "away": away,
            "TP": draw, "FP": home + away, "FN": int(np.count_nonzero(labels == 1)) - draw,
            "TN": int(np.count_nonzero(labels != 1)) - home - away,
            "precision": draw / total if total else None,
            "recall": draw / int(np.count_nonzero(labels == 1)),
            "targetReached": draw >= 900 and total > 0 and draw / total >= .3}


def audit_counts(reported, actual):
    aliases = {"tp": "TP", "fp": "FP", "fn": "FN", "tn": "TN", "falseSelections": "FP"}
    for name, value in actual.items():
        if name not in reported:
            continue
        observed = reported[name]
        if isinstance(value, float):
            assert observed is not None and math.isclose(observed, value, abs_tol=1e-12, rel_tol=1e-12), (name, observed, value)
        else:
            assert observed == value, (name, observed, value)
    for original, normal in aliases.items():
        if original in reported:
            assert reported[original] == actual[normal], (original, reported[original], actual[normal])


def cumulative_threshold(scores, labels, target=900):
    # Walk the complete descending score sequence; then include the WHOLE
    # boundary score group. This is independent of sorting positive scores.
    order = np.argsort(-scores, kind="stable")
    hit = np.cumsum(labels[order] == 1)
    position = int(np.flatnonzero(hit >= target)[0])
    threshold = float(scores[order[position]])
    mask = scores >= threshold
    assert np.count_nonzero((scores > threshold) & (labels == 1)) < target
    assert np.count_nonzero(mask & (labels == 1)) >= target
    return threshold, mask


def independently_guard(matrix, metadata):
    guarded = matrix.copy()
    columns = {item["name"]: index for index, item in enumerate(metadata)}
    changes = {}
    for index, item in enumerate(metadata):
        name = item["name"]
        valid = np.ones(len(matrix), dtype=bool)
        recent = re.fullmatch(r"safeRecent\.(home|away)\.(5|10)\..+", name)
        h2h = re.fullmatch(r"safeH2H\.([^.]*)\..+", name)
        conflict = re.fullmatch(r"conflict\.recent\.(home|away)\..+", name)
        if recent:
            valid = matrix[:, columns[f"safeRecent.{recent[1]}.{recent[2]}.n"]] >= 5
        elif h2h:
            valid = matrix[:, columns[f"safeH2H.{h2h[1]}.n"]] >= 1
        elif name.startswith("safeRecent.pair."):
            valid = ((matrix[:, columns["safeRecent.home.10.n"]] >= 5)
                     & (matrix[:, columns["safeRecent.away.10.n"]] >= 5))
        elif conflict:
            three_match_window = re.match(r"conflict\.recent\.(home|away)\.3\.", name) is not None
            key = (f"conflict.recent.{conflict[1]}.3.n" if three_match_window
                   else f"safeRecent.{conflict[1]}.10.n")
            valid = matrix[:, columns[key]] >= (3 if three_match_window else 5)
        removed = int(np.count_nonzero(np.isfinite(matrix[:, index]) & ~valid))
        if removed:
            changes[name] = removed
        guarded[~valid, index] = np.nan
    return guarded, changes


def independently_select(guarded, metadata, view):
    selected = []
    for index, item in enumerate(metadata):
        name = item["name"]
        lower = name.lower()
        rejected = (name.startswith("availability.") or "ambiguous" in lower or "missing" in lower
                    or item.get("denominatorOnly") or name.endswith(".n")
                    or re.search(r"\.(recentHomeMatches|recentGuestMatches|homeSamples|awaySamples|matches|sampleCount|samples)$", name, re.I)
                    or any(part in lower for part in ("matchid", "kickoff", "timestamp", "teamid", "resultlabel")))
        if rejected or (view == "clean" and item["view"] not in ("market", "safeHistory")):
            continue
        if view == "historyOnly" and item["view"] != "safeHistory":
            continue
        values = guarded[np.isfinite(guarded[:, index]), index]
        if len(values) >= 40 and values.max() > values.min():
            selected.append(index)
    return selected


def review_models(ids, labels, matrix, metadata, old_review, force_refit=False):
    report_path = OUT / "models.json"
    report = read(report_path)
    scores_path = OUT / "model-scores.npz"
    saved = np.load(scores_path, allow_pickle=False)
    assert report["scoresSHA256"] == sha(scores_path)
    assert np.array_equal(saved["matchIds"].astype(str), ids)
    assert np.array_equal(saved["labels"], labels)
    sources = {}
    for name, expected in report["sourceSHA256"].items():
        path = ROOT / name.replace("\\", "/")
        observed = sha(path)
        assert observed == expected, (name, observed, expected)
        sources[name] = observed
    assert report["sourceFeatureMatrixCanonicalSHA256"] == array_sha(np.nan_to_num(matrix, nan=-9.87654321e99))
    guarded, changes = independently_guard(matrix, metadata)
    assert report["guardedFeatureMatrixCanonicalSHA256"] == array_sha(np.nan_to_num(guarded, nan=-9.87654321e99))
    expected_changes = {entry["feature"]: entry["valuesRemovedForInsufficientHistory"] for entry in report["historyQualificationChanges"]}
    assert changes == expected_changes
    names = [item["name"] for item in metadata]
    col = {name: index for index, name in enumerate(names)}
    market_raw = matrix[:, col["euro.average.latest.prob.draw"]]
    market = np.where(np.isfinite(market_raw), market_raw, np.mean(labels == 1))
    assert np.array_equal(saved["market"], market)
    assert np.isfinite(market_raw).sum() == report["market"]["knownQuoteRows"]
    results = []
    for item in report["models"]:
        scores = saved[item["id"]]
        assert np.isfinite(scores).all() and ((scores >= 0) & (scores <= 1)).all()
        indices = independently_select(guarded, metadata, item["view"])
        assert [names[index] for index in indices] == item["inputFeatureNames"]
        assert len(indices) == item["inputFeatureCount"]
        threshold, selected = cumulative_threshold(scores, labels)
        actual = result_counts(labels, selected)
        audit_counts(item["at900"], actual)
        assert item["at900"]["threshold"] == threshold
        assert item["at900"]["tiesAtThreshold"] == int(np.count_nonzero(scores == threshold))
        assert np.array_equal(selected, saved[item["id"] + "_selected"])
        assert ids[selected].tolist() == item["selectedMatchIds"]
        assert math.isclose(item["inSampleBrier"], brier_score_loss(labels == 1, scores), abs_tol=1e-12)
        assert math.isclose(item["inSampleAP"], average_precision_score(labels == 1, scores), abs_tol=1e-12)
        n = actual["selected"]
        market_threshold = np.partition(market, len(market) - n)[len(market) - n]
        market_mask = market >= market_threshold
        audit_counts(item["marketSameSelectedCount"], result_counts(labels, market_mask))
        assert item["marketSameSelectedCount"]["threshold"] == market_threshold
        assert item["marketSameSelectedCount"]["tiesAtThreshold"] == int(np.count_nonzero(market == market_threshold))
        results.append({"id": item["id"], "view": item["view"], **actual,
                        "threshold": threshold, "tiesAtThreshold": int(np.count_nonzero(scores == threshold)),
                        "scoreArraySHA256": array_sha(scores), "inputFeatureCount": len(indices)})
    threshold, market_mask = cumulative_threshold(market, labels)
    audit_counts(report["market"]["at900"], result_counts(labels, market_mask))
    assert np.array_equal(market_mask, saved["market_selected"])
    assert threshold == report["market"]["at900"]["threshold"]
    global_best = min(results, key=lambda item: (item["FP"], item["selected"], item["id"]))
    winner = min((item for item in results if item["view"] == "clean"),
                 key=lambda item: (item["FP"], item["id"]))
    winner_item = next(item for item in report["models"] if item["id"] == winner["id"])
    previous = old_review.get("models", {}).get("winnerRefit", {})
    signature = {"refitAuditVersion": 1, "winnerId": winner["id"],
                 "guardedMatrixSHA256": report["guardedFeatureMatrixCanonicalSHA256"],
                 "featureNames": winner_item["inputFeatureNames"], "parameters": winner_item["parameters"],
                 "savedWinnerScoresSHA256": array_sha(saved[winner["id"]]),
                 "labelsSHA256": hashlib.sha256(labels.astype("<i8").tobytes()).hexdigest(),
                 "numpy": report["numpy"], "sklearn": report["sklearn"]}
    if not force_refit and previous.get("signature") == signature and previous.get("exactScoreMatch"):
        refit = previous
    else:
        indices = independently_select(guarded, metadata, winner_item["view"])
        data = SimpleImputer(strategy="median", add_indicator=False).fit_transform(guarded[:, indices])
        with threadpool_limits(limits=4):
            estimator = HistGradientBoostingClassifier(**winner_item["parameters"])
            estimator.fit(data, labels == 1)
            refit_scores = estimator.predict_proba(data)[:, 1]
        assert np.array_equal(refit_scores, saved[winner["id"]]), float(np.max(np.abs(refit_scores - saved[winner["id"]])))
        refit = {"id": winner["id"], "signature": signature, "exactScoreMatch": True,
                 "maximumAbsoluteDifference": float(np.max(np.abs(refit_scores - saved[winner["id"]]))),
                 "refitScoreSHA256": array_sha(refit_scores), "trainedTrees": int(estimator.n_iter_),
                 "parameters": winner_item["parameters"], "onlyOneModelRefitted": True}
    for item in report["models"]:
        if "modelArtifact" in item:
            assert sha(ROOT / item["modelArtifact"].replace("\\", "/")) == item["modelArtifactSHA256"]
    fields = report.get("fieldStatistics", [])
    assert len(fields) == len(metadata)
    selections = {view: independently_select(guarded, metadata, view) for view in ("clean", "all", "historyOnly")}
    for index, (field, meta) in enumerate(zip(fields, metadata)):
        assert field["name"] == meta["name"]
        for label, data in (("raw", matrix), ("qualified", guarded)):
            finite = np.isfinite(data[:, index])
            assert field[label + "FiniteRows"] == int(finite.sum())
            assert math.isclose(field[label + "FiniteRate"], float(finite.mean()), abs_tol=1e-12)
        assert field["usedInViews"] == [view for view in selections if index in selections[view]]
    for family in report.get("familyStatistics", []):
        js = [index for index, meta in enumerate(metadata) if meta.get("family", "unknown") == family["family"]]
        assert len(js) == family["fields"]
        finite = np.isfinite(guarded[:, js])
        assert family["qualifiedFiniteCells"] == int(finite.sum())
        assert math.isclose(family["qualifiedFiniteRate"], float(finite.mean()), abs_tol=1e-12)
    # Reproduce saved-model training-only group shuffles. This checks the
    # descriptive numbers without fitting any additional estimator.
    group_statistics_checked = 0
    field_by_name = {field["name"]: field for field in fields}
    for diagnostic in report.get("groupPermutationTrainingOnly", []):
        model_item = next(item for item in report["models"] if item["id"] == diagnostic["modelId"])
        artifact = joblib.load(ROOT / model_item["modelArtifact"].replace("\\", "/"))
        indices = independently_select(guarded, metadata, model_item["view"])
        assert artifact["inputColumns"] == indices
        assert artifact["inputFeatureNames"] == model_item["inputFeatureNames"]
        data = artifact["imputer"].transform(guarded[:, indices])
        rng = np.random.default_rng(20261025)
        groups = {}
        for local, index in enumerate(indices):
            groups.setdefault(field_by_name[names[index]]["recordGroup"], []).append(local)
        expected = {item["group"]: item for item in diagnostic["groups"]}
        assert set(groups) == set(expected)
        assert diagnostic["baselineInSampleAP"] == model_item["inSampleAP"]
        with threadpool_limits(limits=4):
            assert np.array_equal(artifact["model"].predict_proba(data)[:, 1], saved[diagnostic["modelId"]])
            for group, js in sorted(groups.items()):
                observed = expected[group]
                assert observed["featureCount"] == len(js)
                drops = []
                for repeat in range(3):
                    perturbed = data.copy()
                    order = rng.permutation(len(labels))
                    perturbed[:, js] = data[order][:, js]
                    drop = (model_item["inSampleAP"] - average_precision_score(
                        labels == 1, artifact["model"].predict_proba(perturbed)[:, 1]))
                    drops.append(float(drop))
                    assert math.isclose(observed["all3Drops"][repeat], drop, abs_tol=1e-12)
                assert math.isclose(observed["meanInSampleAPDrop"], np.mean(drops), abs_tol=1e-12)
                group_statistics_checked += 1
    return {"checkedModelCount": len(results), "counts": results, "market": result_counts(labels, market_mask),
            "winner": winner, "auxiliaryGlobalLowestFP": global_best,
            "winnerRefit": refit, "sourcesMatched": len(sources),
            "fieldAvailabilityRowsChecked": len(fields), "familiesChecked": len(report.get("familyStatistics", [])),
            "trainingOnlyGroupPermutationStatisticsChecked": group_statistics_checked,
            "guardedValuesRemoved": int(sum(changes.values())),
            "claimBoundary": "All labels used for training and threshold; this confirms historical fitting only, independent test rows = 0."}


def evaluate_atom(atom, matrix, columns):
    values = matrix[:, columns[atom["feature"]]]
    transform = atom.get("transform", "identity")
    assert transform in ("identity", "abs")
    if transform == "abs":
        values = np.abs(values)
    operators = {"le": np.less_equal, "ge": np.greater_equal, "eq": np.equal,
                 "lt": np.less, "gt": np.greater}
    result = np.isfinite(values) & operators[atom["op"]](values, atom["value"])
    for guard in atom.get("guards", []):
        result &= evaluate_atom(guard, matrix, columns)
    return result


def review_rules(ids, labels, matrix, metadata):
    report = read(OUT / "rules.json")
    columns = {item["name"]: index for index, item in enumerate(metadata)}
    for name, expected in report["sources"].items():
        assert sha(ROOT / name.replace("\\", "/")) == expected, name
    masks, rules = {}, {}
    terms = guards = 0
    for rule in report["rules"]:
        assert rule["id"] not in masks
        result = np.ones(len(ids), dtype=bool)
        assert 1 <= len(rule["allOf"]) <= 2
        for atom in rule["allOf"]:
            result &= evaluate_atom(atom, matrix, columns)
            terms += 1
            guards += len(atom.get("guards", []))
            assert not any(token in atom["feature"].lower() for token in
                           ("matchid", "matchtime", "leagueid", "teamid", "target.", "label.", "availability."))
            assert not atom["feature"].endswith((".n", "recentHomeMatches", "recentGuestMatches"))
        audit_counts(rule["rawMatchStats"], result_counts(labels, result))
        masks[rule["id"]] = result
        rules[rule["id"]] = rule
    audit_counts(report["baseline"], result_counts(labels, np.ones(len(ids), dtype=bool)))
    checked_steps = 0
    final_masks, summaries = {}, []
    for config in report["configurations"]:
        union = np.zeros(len(ids), dtype=bool)
        positive = config["operation"] == "select if any positive rule matches"
        for step in config["steps"]:
            rule = rules[step["ruleId"]]
            current = masks[step["ruleId"]]
            if config["source"] == "marketAndSafeHistory":
                assert all(atom["view"] != "snapshotDerived" for atom in rule["allOf"])
            delta = current & ~union
            draw = int(np.count_nonzero(delta & (labels == 1)))
            non = int(np.count_nonzero(delta & (labels != 1)))
            if positive:
                assert step["addedDraws"] == draw and step["addedNonDraws"] == non
            else:
                assert step["removedDraws"] == draw and step["removedNonDraws"] == non
            union |= current
            selected = union if positive else ~union
            audit_counts(step["remaining"], result_counts(labels, selected))
            checked_steps += 1
        selected = union if positive else ~union
        actual = result_counts(labels, selected)
        audit_counts(config["stats"], actual)
        assert len(config["steps"]) <= config["maximumRules"]
        if not positive:
            assert int(np.count_nonzero(union & (labels == 1))) <= 70
        final_masks[config["id"]] = selected
        summaries.append({"id": config["id"], "rules": len(config["steps"]), **actual})
    with (OUT / "rule-selections.csv").open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.reader(stream)
        header = next(reader)
        assert header == ["matchId", "result"] + [config["id"] for config in report["configurations"]]
        entries = list(reader)
    assert len(entries) == len(ids)
    assert [entry[0] for entry in entries] == ids.tolist()
    assert np.array_equal([int(entry[1]) for entry in entries], labels)
    for j, config in enumerate(report["configurations"]):
        assert np.array_equal([bool(int(entry[j + 2])) for entry in entries], final_masks[config["id"]])
    by_id = {entry["id"]: entry for entry in summaries}
    eligible = [config for config in report["configurations"] if config["stats"]["draw"] >= 900]
    best = min(eligible, key=lambda config: (config["stats"]["falseSelections"], -config["stats"]["draw"], len(config["steps"])))
    assert best["id"] == report["bestConfigurationId"]
    audit_counts(report["best"], by_id[best["id"]])
    return {"rulesChecked": len(masks), "principalPredicatesChecked": terms, "directGuardsChecked": guards,
            "configurationCount": len(summaries), "cumulativeStepsChecked": checked_steps,
            "selectionCSVMasksChecked": len(summaries) * len(ids),
            "best": by_id[best["id"]], "counts": summaries,
            "rulesSHA256": sha(OUT / "rules.json"), "csvSHA256": sha(OUT / "rule-selections.csv"),
            "claimBoundary": "Exact predicates, finite-value guards and every union step independently reevaluated from frozen input matrix. Search is finite and historical, no global optimality claim."}


def review_permutations(ids, labels, model_review):
    path = OUT / "permutation-control.json"
    report = read(path)
    score_path = OUT / "permutation-control-scores.npz"
    assert sha(score_path) == report["scoresSHA256"]
    saved = np.load(score_path, allow_pickle=False)
    assert np.array_equal(saved["matchIds"].astype(str), ids)
    assert report["modelId"] == model_review["winner"]["id"]
    model = next(model for model in read(OUT / "models.json")["models"] if model["id"] == report["modelId"])
    assert report["parameters"] == model["parameters"]
    assert report["inputFeatureNames"] == model["inputFeatureNames"]
    audit_counts(report["actualLabelsPrimaryWinner"], model_review["winner"])
    for name, expected in report["sourceSHA256"].items():
        assert sha(ROOT / name.replace("\\", "/")) == expected
    summaries = []
    for control in report["controls"]:
        seed = control["permutationSeed"]
        expected = np.random.default_rng(seed).permutation(labels == 1)
        own_labels = saved[f"seed{seed}_labels"]
        scores = saved[f"seed{seed}_scores"]
        assert np.array_equal(expected, own_labels)
        assert int(own_labels.sum()) == control["positiveLabels"] == 970
        assert np.isfinite(scores).all() and ((scores >= 0) & (scores <= 1)).all()
        binary = own_labels.astype(int)
        threshold, mask = cumulative_threshold(scores, binary)
        actual = result_counts(binary, mask)
        audit_counts(control["at900AgainstPermutedOwnFitLabels"], actual)
        assert threshold == control["at900AgainstPermutedOwnFitLabels"]["threshold"]
        assert int(np.count_nonzero(scores == threshold)) == control["at900AgainstPermutedOwnFitLabels"]["tiesAtThreshold"]
        assert np.array_equal(saved[f"seed{seed}_selected"], mask)
        assert math.isclose(brier_score_loss(own_labels, scores), control["inSampleBrierAgainstPermutedOwnFitLabels"], abs_tol=1e-12)
        assert math.isclose(average_precision_score(own_labels, scores), control["inSampleAPAgainstPermutedOwnFitLabels"], abs_tol=1e-12)
        summaries.append({"seed": seed, **{name: actual[name] for name in ("selected", "TP", "FP", "FN", "TN", "precision", "recall", "targetReached")},
                          "threshold": threshold, "tiesAtThreshold": int(np.count_nonzero(scores == threshold))})
    assert report["seedsMeetingTarget"] == sum(item["targetReached"] for item in summaries)
    assert report["controlFPRange"] == [min(item["FP"] for item in summaries), max(item["FP"] for item in summaries)]
    assert report["controlPrecisionRange"] == [min(item["precision"] for item in summaries), max(item["precision"] for item in summaries)]
    return {"controlsChecked": len(summaries), "countsAgainstEachOwnShuffledFitLabels": summaries,
            "seedsMeetingTarget": report["seedsMeetingTarget"], "controlPrecisionRange": report["controlPrecisionRange"],
            "sourceSHA256": sha(path), "scoresSHA256": sha(score_path),
            "claimBoundary": "No true-label validation and no formal permutation significance test. Same model can reach the historical objective on all five randomized label sets, showing capacity fit alone is insufficient evidence of useful draw features."}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--models-only", action="store_true")
    parser.add_argument("--force-refit", action="store_true")
    args = parser.parse_args()
    path = OUT / "review.json"
    old = read(path) if path.exists() else {}
    rows, ids, labels, matrix, metadata = load_raw()
    output = {"purpose": __doc__, "generatedAt": datetime.now(timezone.utc).isoformat(),
              "reviewScriptSHA256": sha(__file__), "issues": [],
              "datasetCounts": {"games": len(labels), "home": int(sum(labels == 0)),
                                "draw": int(sum(labels == 1)), "away": int(sum(labels == 2))}}
    output["models"] = review_models(ids, labels, matrix, metadata, old, args.force_refit)
    output["rulesPending"] = not (OUT / "rules.json").exists()
    if not args.models_only:
        assert not output["rulesPending"]
        output["rules"] = review_rules(ids, labels, matrix, metadata)
        output["permutationControls"] = review_permutations(ids, labels, output["models"])
    path.write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"issues": output["issues"], "winner": output["models"]["winner"], "rulesPending": output["rulesPending"]}))


if __name__ == "__main__":
    main()
