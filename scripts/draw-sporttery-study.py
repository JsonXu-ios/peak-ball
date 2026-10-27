#!/usr/bin/env python3
"""Offline sporttery-cohort checks, keeping scope and input source separate.

All rows are the previously studied historical export. Refitting/threshold
selection uses their labels. No new validation or database/service action.
"""
from __future__ import annotations

import os
for variable in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
    os.environ.setdefault(variable, "4")

import hashlib
import importlib.util
import json
import math
from collections import Counter
from pathlib import Path

import joblib
import numpy as np
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.impute import SimpleImputer
from threadpoolctl import threadpool_limits

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-sporttery-study"


def read(p):
    return json.loads(p.read_text(encoding="utf-8-sig"))


def sha(p):
    return hashlib.sha256(p.read_bytes()).hexdigest()


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def counts(labels, selected, scope):
    assert not np.any(selected & ~scope)
    h, d, a = [int(np.sum(selected & (labels == k))) for k in (0, 1, 2)]
    n, total_d = h + d + a, int(np.sum(scope & (labels == 1)))
    target = math.ceil(total_d * 900 / 970)
    return {"selected": n, "home": h, "draw": d, "away": a, "falseSelections": h + a,
            "scopeGames": int(scope.sum()), "scopeDraws": total_d, "targetDraws": target,
            "precision": d / n if n else None, "recall": d / total_d if total_d else None,
            "targetReached": d >= target and n > 0 and d / n >= .3}


def top_recall(labels, scores, scope):
    target = math.ceil(int(np.sum(scope & (labels == 1))) * 900 / 970)
    values = np.sort(scores[scope & (labels == 1)])[::-1]
    assert len(values) >= target and np.isfinite(scores[scope]).all()
    threshold = float(values[target - 1])
    selected = scope & (scores >= threshold)
    return selected, threshold


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    hist_path = ROOT / "scripts/draw-historical-value-study.py"
    model_path = ROOT / "scripts/draw-target-900-model.py"
    hist, previous = module("sporttery_input", hist_path), module("sporttery_previous", model_path)
    rows, X, labels, meta, sources = hist.load_inputs(ROOT / "reports/draw-historical-value-study")
    names = [m["name"] for m in meta]
    columns = {name: i for i, name in enumerate(names)}
    ids = np.asarray([str(r["matchId"]) for r in rows])
    marked = np.array([bool(str(r.get("jingcaiId") or "").strip()) for r in rows])
    # Full triples are required for normalised probabilities in the frozen extractor.
    quote = np.isfinite(X[:, columns["euro.sporttery.latest.prob.draw"]])
    opening = np.isfinite(X[:, columns["euro.sporttery.opening.prob.draw"]])
    scopes = {"marked": marked, "markedBothQuotes": marked & quote & opening, "latestQuoteEvidence": quote}
    expected = {"marked": (511, 124), "markedBothQuotes": (471, 119), "latestQuoteEvidence": (472, 119)}
    for name, s in scopes.items():
        assert (int(s.sum()), int(np.sum(s & (labels == 1)))) == expected[name]
    guarded = X.copy()
    for j, name in enumerate(names):
        guarded[~previous.historical_guard(X, columns, name), j] = np.nan
    arrays = {"matchIds": ids, "labels": labels, **{"scope_" + k: v for k, v in scopes.items()}}
    results = []
    def add(ident, scope_name, scores, *, threshold=None, labels_key="labels", kind, extra=None):
        lab = arrays[labels_key]
        scope = scopes[scope_name]
        if threshold is None:
            selected, threshold = top_recall(lab, scores, scope)
            mode = "historical_threshold_refitted_to_subset_labels"
        else:
            selected, mode = scope & (scores >= threshold), "old_global_threshold_frozen"
        sk, mk = ident + "_scores", ident + "_selected"
        arrays[sk], arrays[mk] = scores, selected
        result = {"id": ident, "scopeId": scope_name, "kind": kind, "thresholdMode": mode,
                  "threshold": threshold, "tiesAtThresholdInScope": int(np.sum(scope & (scores == threshold))),
                  "scoresKey": sk, "selectedKey": mk, "labelsKey": labels_key, "positiveLabel": 1,
                  "stats": counts(lab, selected, scope), "selectedMatchIds": ids[selected].tolist()}
        if extra:
            result.update(extra)
        results.append(result)
        return result
    old_folder = ROOT / "reports/draw-target-900-study"
    old = read(old_folder / "models.json")
    old_scores = np.load(old_folder / "model-scores.npz", allow_pickle=False)
    assert list(old_scores["matchIds"].astype(str)) == list(ids)
    for scope_name in scopes:
        for m in old["models"]:
            score = old_scores[m["id"]]
            add(f"oldFrozen_{scope_name}_{m['id']}", scope_name, score, threshold=m["at900"]["threshold"],
                kind="global_fit_threshold_frozen", extra={"oldModelId": m["id"], "view": m["view"]})
            add(f"oldRetuned_{scope_name}_{m['id']}", scope_name, score,
                kind="global_fit_subset_threshold", extra={"oldModelId": m["id"], "view": m["view"]})
        raw = X[:, columns["euro.sporttery.latest.prob.draw"]]
        score = np.where(np.isfinite(raw), raw, float(np.mean(labels[scopes[scope_name]] == 1)))
        add("jcMarket_" + scope_name, scope_name, score, kind="sporttery_quote_baseline",
            extra={"missingQuoteRows": int(np.sum(scopes[scope_name] & ~np.isfinite(raw))),
                   "missingPolicy": "same-scope full-history draw rate, all ties retained; unavailable quotes stay disclosed"})
    views = {}
    for view in ("jcOnly", "jcHistory", "historyOnly", "clean"):
        use = []
        for j, m in enumerate(meta):
            if previous.exclusion(m):
                continue
            allowed = m["name"].startswith("euro.sporttery.")
            if view == "jcHistory":
                allowed |= m["view"] == "safeHistory"
            elif view == "historyOnly":
                allowed = m["view"] == "safeHistory"
            elif view == "clean":
                allowed = m["view"] in ("market", "safeHistory")
            values = guarded[marked, j]
            finite = values[np.isfinite(values)]
            if allowed and len(finite) >= 20 and np.ptp(finite) > 0:
                use.append(j)
        views[view] = use
    configs = [("jcOnly_d2_l40_i120", "jcOnly", 2, 120), ("jcHistory_d2_l40_i120", "jcHistory", 2, 120),
               ("jcOnly_d3_l40_i240", "jcOnly", 3, 240), ("jcHistory_d3_l40_i240", "jcHistory", 3, 240),
               ("historyOnly_d3_l40_i240", "historyOnly", 3, 240), ("clean_d3_l40_i240", "clean", 3, 240)]
    trained, fit_records = {}, []
    (OUT / "models").mkdir(exist_ok=True)
    with threadpool_limits(limits=4):
        for ident, view, depth, iterations in configs:
            cols = views[view]
            imputer = SimpleImputer(strategy="median", add_indicator=False)
            data = imputer.fit_transform(guarded[np.ix_(marked, cols)])
            model = HistGradientBoostingClassifier(max_iter=iterations, max_depth=depth, max_leaf_nodes=2 ** depth,
                min_samples_leaf=40, l2_regularization=10, learning_rate=.05, max_bins=32,
                early_stopping=False, random_state=20261026)
            model.fit(data, labels[marked] == 1)
            score = np.full(len(labels), np.nan)
            score[marked] = model.predict_proba(data)[:, 1]
            fields = [{"name": names[j], "source": meta[j].get("source"), "view": meta[j]["view"], "family": meta[j]["family"]} for j in cols]
            r = add("subsetFit_" + ident, "marked", score, kind="subset_full_history_fit",
                    extra={"configId": ident, "view": view, "inputFeatureCount": len(cols), "inputFields": fields,
                           "parameters": model.get_params()})
            artifact = OUT / "models" / (ident + ".joblib")
            joblib.dump({"model": model, "imputer": imputer, "inputColumns": cols, "scopeId": "marked",
                         "inputNames": [names[j] for j in cols], "historicalThreshold": r["threshold"]}, artifact)
            r["modelArtifact"], r["modelArtifactSHA256"] = str(artifact.relative_to(ROOT)), sha(artifact)
            trained[ident] = (model, data, cols)
            fit_records.append(r)
            print(json.dumps({"id": ident, **r["stats"]}), flush=True)
        model, data, cols = trained["jcHistory_d3_l40_i240"]
        for seed in (20261026, 20261027, 20261028):
            lab = np.zeros(len(labels), dtype=int)
            lab[marked] = np.random.default_rng(seed).permutation((labels[marked] == 1).astype(int))
            lk = f"control_{seed}_labels"
            arrays[lk] = lab
            sham = HistGradientBoostingClassifier(**model.get_params())
            sham.fit(data, lab[marked])
            score = np.full(len(labels), np.nan)
            score[marked] = sham.predict_proba(data)[:, 1]
            add(f"control_{seed}", "marked", score, labels_key=lk, kind="shuffled_training_labels_capacity_diagnostic",
                extra={"permutationSeed": seed, "fixedConfig": "jcHistory_d3_l40_i240",
                       "interpretation": "Evaluated against its own shuffled training labels, not actual match draws; not a significance test"})
    sources.update({str(p.relative_to(ROOT)): sha(p) for p in [hist_path, model_path, Path(__file__).resolve(),
                   old_folder / "models.json", old_folder / "model-scores.npz"]})
    np.savez_compressed(OUT / "scores.npz", **arrays)
    output = {"protocol": {"status": "retrospective_scope_check_and_in_sample_discovery_only", "snapshotRows": 4161,
        "allDatesPreviouslyStudied": True, "independentValidation": False, "noServiceDatabaseNetwork": True,
        "scopeDefinition": "marked: nonempty jingcaiId; no odds absence reclassification",
        "targetRecallReference": "900/970", "targetPrecision": .3, "includeAllBoundaryTies": True,
        "jcOnlyInputs": "only euro.sporttery.* normal SPF original prices and transforms; no handicap SPF, no other companies or synthetic odds",
        "jcHistoryInputs": "jcOnly plus strictly reconstructed safeHistory; not a claim that sporttery prices alone suffice",
        "imputer": "medians fitted only within marked subset, no availability/missing indicators",
        "fixedConfigsBeforeSubsetFit": [dict(id=i, view=v, depth=d, iterations=t, minLeaf=40) for i, v, d, t in configs],
        "controls": "fixed jcHistory d3leaf40iter240, 3 seeds; capacity diagnostic, no model reselection, no significance claim",
        "oddsCaptureProvenPrekickoff": False},
        "sourceHashes": sources, "scoresSHA256": sha(OUT / "scores.npz"),
        "scopes": {k: {**counts(labels, s, s), "matchIds": ids[s].tolist(), "scopeKey": "scope_" + k} for k, s in scopes.items()},
        "results": results}
    (OUT / "analysis.json").write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"complete": True, "resultCount": len(results), "subsetFits": len(fit_records)}), flush=True)


if __name__ == "__main__":
    main()
