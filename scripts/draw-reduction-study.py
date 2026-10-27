#!/usr/bin/env python3
"""Offline adaptation of existing inputs: retain draws while rejecting false picks.

No services, network or source storage are touched. Full-history fitting and
chronological, repeatedly examined internal tests are reported separately.
"""
from __future__ import annotations

import os
os.environ.setdefault("OMP_NUM_THREADS", "4")
os.environ.setdefault("OPENBLAS_NUM_THREADS", "4")
os.environ.setdefault("MKL_NUM_THREADS", "4")
import csv
import hashlib
import importlib.util
import json
import math
import sys
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path

sys.dont_write_bytecode = True
import numpy as np
from threadpoolctl import threadpool_limits

from draw_cover_candidates import build_candidates
from draw_rejection_candidates import build_rejection_candidates

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "reports/draw-full-feature-study"
COVER = ROOT / "reports/draw-cover-study"
OUT = ROOT / "reports/draw-reduction-study"
TARGETS = (900, 850, 800)
EPS = 1e-9


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    value = importlib.util.module_from_spec(spec)
    sys.modules[name] = value
    spec.loader.exec_module(value)
    return value


cover = module("draw_cover_prior", "draw-cover-study.py")
existing = module("draw_full_prior", "draw-full-feature-study.py")


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write(path, value):
    existing.write(path, value)


def stats(mask, y, indices):
    indices = np.asarray(indices, int)
    return {"games": len(indices), "draws": int(y[indices].sum()), **cover.metrics(mask, y, indices)}


def cmp(values, op, threshold):
    if op == "ge":
        return values >= threshold
    if op == "le":
        return values <= threshold
    if op == "gt":
        return values > threshold
    if op == "lt":
        return values < threshold
    if op == "eq":
        return values == threshold
    raise ValueError(op)


def masks_for(X, names, design):
    columns = {name: i for i, name in enumerate(names)}
    am, ae = {}, {}
    for atom in design["atoms"]:
        values = X[:, columns[atom["feature"]]]
        eligible = np.isfinite(values)
        for guard in atom["guards"]:
            gv = X[:, columns[guard["feature"]]]
            eligible &= np.isfinite(gv) & cmp(gv, guard["op"], guard["value"])
        transformed = np.abs(values) if atom["transform"] == "abs" else values
        ae[atom["id"]] = eligible
        am[atom["id"]] = eligible & cmp(transformed, atom["op"], atom["value"])
    R = np.array([np.logical_and.reduce([am[a] for a in r["allOf"]]) for r in design["rules"]])
    E = np.array([np.logical_and.reduce([ae[a] for a in r["allOf"]]) for r in design["rules"]])
    return R, E, am, ae


def qualify_rejections(R, E, am, ae, design, base, y, train, calibration):
    """Directional development screens only, not significance tests."""
    atom_ok = {}
    for ident, mask in am.items():
        eligible = base[train] & ae[ident][train]
        hits = int((mask[train] & base[train]).sum())
        atom_ok[ident] = 0 < hits < int(eligible.sum())
    results, pools = [], {"clean": [], "all": []}
    atoms = {a["id"]: a for a in design["atoms"]}
    for ri, rule in enumerate(design["rules"]):
        intervals = {}
        valid = all(atom_ok[a] for a in rule["allOf"])
        for role, indices, minimum in (("train", train, 30), ("calibration", calibration, 10)):
            selected = base[indices] & R[ri, indices]
            eligible = base[indices] & E[ri, indices]
            n, en = int(selected.sum()), int(eligible.sum())
            fp, efp = int((selected & ~y[indices]).sum()), int((eligible & ~y[indices]).sum())
            rate, baseline = fp / n if n else None, efp / en if en else None
            valid &= n >= minimum and rate is not None and baseline is not None and rate > baseline + EPS
            intervals[role] = {"matched": n, "nonDraws": fp, "eligible": en, "nonDrawRate": rate, "eligibleBaseNonDrawRate": baseline}
        clean = all(atoms[a]["view"] in ("market", "safeHistory") for a in rule["allOf"])
        if valid:
            pools["all"].append(ri)
            if clean:
                pools["clean"].append(ri)
        results.append({"ruleId": rule["id"], "qualified": bool(valid), "view": "clean" if clean else "all", **intervals})
    return pools, results


def requested_draws(y, indices, target):
    return int(math.ceil(target / 970 * int(y[indices].sum()) - EPS))


def reject_greedy(R, E, design, pool, base, y, train, calibration, target, strategy, evaluation=None):
    train, calibration = np.asarray(train, int), np.asarray(calibration, int)
    required = {"train": requested_draws(y, train, target), "calibration": requested_draws(y, calibration, target)}
    initial = {"train": stats(base, y, train), "calibration": stats(base, y, calibration)}
    reachable = {role: initial[role]["tp"] >= required[role] for role in required}
    selected = base.copy()
    result = {"target": target, "targetFraction": target / 970, "trainTargetReachable": reachable["train"],
              "calibrationTargetReachable": reachable["calibration"], "requiredDraws": required,
              "budgets": {role: max(0, initial[role]["tp"] - required[role]) for role in required},
              "qualifiedRules": len(pool), "steps": [], "selectedRuleIds": []}
    if not all(reachable.values()):
        result["stopReason"] = "base_cannot_reach_requested_target_keep_base"
        return result, selected
    remaining = set(pool)
    while remaining and len(result["steps"]) < 12:
        options = []
        candidates = sorted(remaining)
        gains = {}
        for role, indices in (("train", train), ("calibration", calibration)):
            active = indices[selected[indices]]
            pos, neg = active[y[active]], active[~y[active]]
            gains[role] = (R[candidates][:, pos].sum(axis=1), R[candidates][:, neg].sum(axis=1))
        current = {"train": stats(selected, y, train), "calibration": stats(selected, y, calibration)}
        for j, ri in enumerate(candidates):
            okay = True
            for role, indices, minimum in (("train", train, 20), ("calibration", calibration, 8)):
                tp, fp = (int(gains[role][k][j]) for k in (0, 1))
                if tp + fp < minimum or fp == 0 or current[role]["tp"] - tp < required[role]:
                    okay = False
                    break
                eligible = selected[indices] & E[ri, indices]
                en, efp = int(eligible.sum()), int((eligible & ~y[indices]).sum())
                if not en or fp / (tp + fp) <= efp / en + EPS:
                    okay = False
                    break
            if not okay:
                continue
            tp, fp = (int(gains["train"][k][j]) for k in (0, 1))
            score = fp / (tp + 1) if strategy == "ratio" else fp
            options.append(((-score, -fp, tp, len(design["rules"][ri]["allOf"]), design["rules"][ri]["id"]), ri))
        if not options:
            result["stopReason"] = "no_qualified_increment_within_both_draw_budgets"
            break
        _, chosen = min(options)
        before = selected.copy()
        selected &= ~R[chosen]
        remaining.remove(chosen)
        rule = design["rules"][chosen]
        removed = before & ~selected
        entry = {"step": len(result["steps"]) + 1, "ruleId": rule["id"], "label": rule["label"],
                 "removedFP": int((removed[train] & ~y[train]).sum()), "removedTP": int((removed[train] & y[train]).sum()),
                 "train": stats(selected, y, train), "calibration": stats(selected, y, calibration),
                 "calibrationRemovedFP": int((removed[calibration] & ~y[calibration]).sum()),
                 "calibrationRemovedTP": int((removed[calibration] & y[calibration]).sum())}
        if evaluation is not None:
            entry["test"] = stats(selected, y, evaluation)
            entry["testRemovedFP"] = int((removed[evaluation] & ~y[evaluation]).sum())
            entry["testRemovedTP"] = int((removed[evaluation] & y[evaluation]).sum())
        result["steps"].append(entry)
        result["selectedRuleIds"].append(rule["id"])
    result.setdefault("stopReason", "maximum12_exclusions" if len(result["steps"]) >= 12 else "candidate_pool_exhausted")
    return result, selected


def score_filter(scores, base, y, calibration, target):
    required = requested_draws(y, calibration, target)
    positive_scores = np.sort(scores[calibration][base[calibration] & y[calibration]])[::-1]
    reachable = len(positive_scores) >= required
    if not reachable or required == 0:
        # Infeasible calibration target is never repaired using future labels.
        return {"threshold": None, "calibrationTargetReachable": reachable,
                "stopReason": "base_cannot_reach_requested_target_keep_base" if not reachable else "no_calibration_draw_requirement"}, base.copy()
    threshold = float(positive_scores[required - 1])
    return {"threshold": threshold, "calibrationTargetReachable": True,
            "stopReason": "highest_calibration_score_threshold_with_target_and_all_ties"}, base & (scores >= threshold)


METHODS = [
    {"id": "base", "label": "高覆盖候选池", "kind": "base", "view": "clean"},
    {"id": "rule_clean_ratio", "label": "市场与赛前战绩：按删非平/误删平排序", "kind": "rule", "view": "clean", "strategy": "ratio"},
    {"id": "rule_clean_max_fp", "label": "市场与赛前战绩：优先多删非平", "kind": "rule", "view": "clean", "strategy": "max_fp"},
    {"id": "rule_all_ratio", "label": "加前台派生：按删非平/误删平排序", "kind": "rule", "view": "all", "strategy": "ratio"},
    {"id": "rule_all_max_fp", "label": "加前台派生：优先多删非平", "kind": "rule", "view": "all", "strategy": "max_fp"},
    {"id": "score_market", "label": "均赔排序过滤", "kind": "score", "view": "clean", "model": "market_raw"},
    {"id": "score_lr_clean", "label": "现有市场战绩逻辑回归过滤", "kind": "score", "view": "clean", "model": "clean_lr_001"},
    {"id": "score_hgb_clean", "label": "现有市场战绩浅树过滤", "kind": "score", "view": "clean", "model": "clean_hgb2"},
    {"id": "score_hgb_all", "label": "现有全页面派生浅树过滤", "kind": "score", "view": "all", "model": "snapshot_hgb"},
]


def model_scores(X, y, train, metadata):
    market_index = next(i for i, m in enumerate(metadata) if m["name"] == "euro.average.latest.prob.draw")
    market = X[:, market_index]
    chosen = {m["model"] for m in METHODS if m["kind"] == "score"}
    scores, details = {}, {}
    with threadpool_limits(limits=4):
        for spec in existing.model_specs():
            if spec.id not in chosen:
                continue
            fitted = existing.Fitted(spec, X, y.astype(int), train, metadata)
            scores[spec.id] = fitted.raw(X, np.arange(len(y)), market)
            details[spec.id] = {"trainingRows": len(train), "trainingPrior": fitted.prior,
                                "featureColumns": fitted.cols, "featureCount": len(fitted.cols)}
    return scores, details


def evaluated_case(method, result, selected, base, y, intervals, target):
    result = {**result, "method": method["id"], "label": method["label"], "kind": method["kind"],
              "view": method["view"], "target": target, "targetFraction": target / 970,
              "base": {role: stats(base, y, ix) for role, ix in intervals.items()},
              "metrics": {role: stats(selected, y, ix) for role, ix in intervals.items()},
              "targetReached": {role: int((selected[ix] & y[ix]).sum()) >= requested_draws(y, ix, target) for role, ix in intervals.items()}}
    result.setdefault("trainTargetReachable", stats(base, y, intervals.get("train", intervals.get("full")))["tp"] >= requested_draws(y, intervals.get("train", intervals.get("full")), target))
    result.setdefault("calibrationTargetReachable", stats(base, y, intervals.get("calibration", intervals.get("full")))["tp"] >= requested_draws(y, intervals.get("calibration", intervals.get("full")), target))
    return result


def run_cases(design, R, E, pools, scores, base, y, train, calibration, test=None):
    cases, masks = [], {}
    intervals = {"train": train, "calibration": calibration}
    if test is not None:
        intervals["test"] = test
    for target in TARGETS:
        for method in METHODS:
            if method["kind"] == "rule":
                result, selected = reject_greedy(R, E, design, pools[method["view"]], base, y, train, calibration,
                                                 target, method["strategy"], test)
            elif method["kind"] == "score":
                result, selected = score_filter(scores[method["model"]], base, y, calibration, target)
            else:
                result, selected = {"steps": [], "selectedRuleIds": [], "threshold": None}, base.copy()
            case = evaluated_case(method, result, selected, base, y, intervals, target)
            cases.append(case)
            masks[(method["id"], target)] = selected
    return cases, masks


def top_with_ties(scores, base, count):
    values = scores[base]
    if count <= 0:
        return np.zeros(len(base), bool)
    threshold = np.sort(values)[::-1][min(count, len(values)) - 1]
    return base & (scores >= threshold)


def summarize_temporal(folds, prediction_masks, market_scores, y):
    groups = defaultdict(list)
    for fold in folds:
        for case in fold["cases"]:
            for role in ("all", fold["role"]):
                groups[(case["method"], case["target"], role)].append((fold, case))
    summaries = []
    for (method, target, role), items in groups.items():
        selected, base_selected, same_market = (np.zeros(len(y), bool) for _ in range(3))
        indices, uniform_tp, uniform_fp, target_failures = [], 0., 0., 0
        for fold, case in items:
            test = np.array(fold["test"], int)
            current = prediction_masks[(fold["fold"], method, target)]
            base = prediction_masks[(fold["fold"], "base", target)]
            selected[test], base_selected[test] = current[test], base[test]
            n = int(current[test].sum())
            market_test = top_with_ties(market_scores[fold["fold"]][test], base[test], n)
            same_market[test] = market_test
            base_n, base_tp = int(base[test].sum()), int((base[test] & y[test]).sum())
            uniform_tp += n * base_tp / base_n if base_n else 0
            uniform_fp += n * (base_n - base_tp) / base_n if base_n else 0
            target_failures += int(not case.get("calibrationTargetReachable", True) or not case.get("trainTargetReachable", True))
            indices.extend(test.tolist())
        indices = np.array(indices, int)
        assert len(set(indices)) == len(indices)
        current_stats, base_stats = stats(selected, y, indices), stats(base_selected, y, indices)
        removed_fp, lost_tp = base_stats["fp"] - current_stats["fp"], base_stats["tp"] - current_stats["tp"]
        summaries.append({"method": method, "target": target, "role": role, **current_stats,
                          "base": base_stats, "removedFP": removed_fp, "lostTP": lost_tp,
                          "fpReduction": removed_fp / base_stats["fp"] if base_stats["fp"] else None,
                          "recallLoss": lost_tp / int(y[indices].sum()),
                          "marketSameSize": stats(same_market, y, indices),
                          "marketSameSizeTiesMayIncreaseCount": bool(int(same_market[indices].sum()) != current_stats["selected"]),
                          "uniformExpectedTP": uniform_tp, "uniformExpectedFP": uniform_fp,
                          "targetReached": current_stats["tp"] >= requested_draws(y, indices, target),
                          "infeasibleFitFolds": target_failures})
    return summaries


def select_on_development(summaries):
    chosen = []
    allowed = {m["id"] for m in METHODS if m["view"] == "clean" and m["id"] != "base"}
    for target in TARGETS:
        development = [s for s in summaries if s["role"] == "development" and s["target"] == target and s["method"] in allowed]
        desired = math.ceil(target / 970 * development[0]["draws"] - EPS)
        winner = min(development, key=lambda s: (max(0, desired - s["tp"]), s["fp"], -s["tp"], s["method"]))
        audit = next(s for s in summaries if s["role"] == "lockedInternalAudit" and s["target"] == target and s["method"] == winner["method"])
        chosen.append({"target": target, "method": winner["method"], "developmentTargetReached": winner["tp"] >= desired,
                       "reason": "Minimize development FP among methods reaching target; otherwise closest draw deficit, then FP. No last2-fold selection.",
                       "development": winner, "audit": audit})
    return chosen


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    files = {"dataset": SOURCE / "dataset.json", "matrix": SOURCE / "features.csv", "manifest": SOURCE / "feature-manifest.json",
             "folds": SOURCE / "folds.json", "protocol": OUT / "protocol.json", "oldCoverAnalysis": COVER / "analysis.json",
             "coverCandidates": Path(__file__).with_name("draw_cover_candidates.py"),
             "coverAlgorithm": Path(__file__).with_name("draw-cover-study.py"),
             "existingModels": Path(__file__).with_name("draw-full-feature-study.py"),
             "rejectionCandidates": Path(__file__).with_name("draw_rejection_candidates.py"),
             "reportScript": Path(__file__).with_name("draw_reduction_report.py"), "script": Path(__file__)}
    hashes = {name: digest(path) for name, path in files.items()}
    protocol, metadata = read(files["protocol"]), read(files["manifest"])
    assert hashes["dataset"] == protocol["datasetSHA256"]
    with files["matrix"].open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.reader(stream)
        header, data = next(reader), list(reader)
    ids, labels = [r[0] for r in data], np.array([int(r[1]) for r in data])
    assert header[:2] == ["matchId", "resultLabel"] and header[2:] == [m["name"] for m in metadata]
    names = header[2:]
    X = np.array([[float(v) if v else np.nan for v in r[2:]] for r in data])
    y = labels == 1
    matrix_hash = hashlib.sha256(np.nan_to_num(X, nan=-9.87654321e99).astype("<f8").tobytes()).hexdigest()
    assert matrix_hash == read(SOURCE / "analysis.json")["featureMatrixSHA256"]
    hashes["canonicalFeatureMatrix"] = matrix_hash
    by_id = {str(r["matchId"]): r for r in read(files["dataset"])["rows"]}
    rows = [by_id[ident] for ident in ids]
    assert len(rows) == len(by_id) == 4161 and int(y.sum()) == 970
    assert all(int(r["result"]) == labels[i] and (r["homeScore"] == r["awayScore"]) == y[i] for i, r in enumerate(rows))
    contexts = [{"matchId": ids[i], "date": r["date"], "matchTime": r["matchTime"], "league": r["league"],
                 "home": r["home"], "away": r["guest"], "score": f'{r["homeScore"]}:{r["awayScore"]}', "isDraw": bool(y[i])} for i, r in enumerate(rows)]
    all_ix = np.arange(len(y))
    draw_design = build_candidates(metadata)
    M, ME, am, ae = cover.evaluate_candidates(X, names, draw_design)
    original = read(files["oldCoverAnalysis"])
    historical_base_ids = [s["ruleId"] for p in original["paths"] if p["id"] == "broad_pure_0" for s in p["steps"]]
    rid = {r["id"]: i for i, r in enumerate(draw_design["rules"])}
    historical_base = M[[rid[ident] for ident in historical_base_ids]].any(axis=0)
    assert stats(historical_base, y, all_ix)["tp"] == 952 and stats(historical_base, y, all_ix)["fp"] == 3120
    early_train = np.array(original["development"]["train"], int)
    early_cal = np.array(original["development"]["calibration"], int)
    design = build_rejection_candidates(X[early_train], names, metadata)
    write(OUT / "candidates-historical.json", design)
    R, E, ram, rae = masks_for(X, names, design)
    pools, qualification = qualify_rejections(R, E, ram, rae, design, historical_base, y, early_train, early_cal)
    write(OUT / "qualification-historical.json", qualification)
    print("historical rejection candidates", len(design["rules"]), {k: len(v) for k, v in pools.items()}, flush=True)
    scores, fit_details = model_scores(X, y, all_ix, metadata)
    np.savez_compressed(OUT / "scores-full.npz", matchIds=np.asarray(ids), **scores)
    full_cases, full_masks = run_cases(design, R, E, pools, scores, historical_base, y, all_ix, all_ix)
    for case in full_cases:
        selected = full_masks[(case["method"], case["target"])]
        case["metrics"] = {"full": case["metrics"]["train"]}
        case["base"] = {"full": stats(historical_base, y, all_ix)}
        case["targetReached"] = {"full": case["targetReached"]["train"]}
        case["selectedMatchIds"] = [ids[i] for i in np.flatnonzero(selected)]
        for step in case.get("steps", []):
            step["full"] = step.pop("train")
            step.pop("calibration", None)
            step.pop("calibrationRemovedFP", None)
            step.pop("calibrationRemovedTP", None)
        print("full", case["method"], case["target"], case["metrics"]["full"]["tp"], case["metrics"]["full"]["fp"], flush=True)
    folds, predictions, markets, model_details = [], {}, {}, {}
    prediction_rows = []
    for source_fold in read(files["folds"]):
        number = source_fold["fold"]
        train, calibration, test = (np.array(source_fold[k], int) for k in ("train", "calibration", "test"))
        base_pools, _ = cover.pool_qualification(M, ME, am, ae, draw_design, y, train, calibration)
        base_steps, base = cover.greedy(M, draw_design["rules"], base_pools["broad"], y, train)
        fold_design = build_rejection_candidates(X[train], names, metadata)
        write(OUT / f"candidates-fold-{number}.json", fold_design)
        fr, fe, fam, fae = masks_for(X, names, fold_design)
        fpools, fqualification = qualify_rejections(fr, fe, fam, fae, fold_design, base, y, train, calibration)
        write(OUT / f"qualification-fold-{number}.json", fqualification)
        fscores, details = model_scores(X, y, train, metadata)
        np.savez_compressed(OUT / f"scores-fold-{number}.npz", matchIds=np.asarray(ids), **fscores)
        cases, fitted_masks = run_cases(fold_design, fr, fe, fpools, fscores, base, y, train, calibration, test)
        folds.append({k: v for k, v in source_fold.items() if k != "matchIds"} | {
            "baseRuleIds": [s["ruleId"] for s in base_steps], "base": {role: stats(base, y, ix) for role, ix in (("train", train), ("calibration", calibration), ("test", test))},
            "candidateCount": len(fold_design["rules"]), "qualifiedRules": {k: len(v) for k, v in fpools.items()}, "cases": cases})
        model_details[str(number)] = details
        markets[number] = fscores["market_raw"]
        for (method, target), mask in fitted_masks.items():
            predictions[(number, method, target)] = mask
            for i in test:
                prediction_rows.append({"matchId": ids[i], "fold": number, "role": source_fold["role"], "method": method, "target": target,
                                        "isDraw": int(y[i]), "baseSelected": int(base[i]), "selected": int(mask[i]),
                                        "marketScore": float(fscores["market_raw"][i])})
        print("fold", number, "base", stats(base, y, test), "qualified", {k: len(v) for k, v in fpools.items()}, flush=True)
    summaries = summarize_temporal(folds, predictions, markets, y)
    selection = select_on_development(summaries)
    candidate_features = {a["feature"] for a in design["atoms"]}
    adaptation = [{**m, "finiteMatches": int(np.isfinite(X[:, j]).sum()),
                   "historicalRejectionOperand": m["name"] in candidate_features,
                   "historicalScorers": [model for model, fit in fit_details.items() if j in fit["featureColumns"]]}
                  for j, m in enumerate(metadata)]
    write(OUT / "feature-adaptation.json", adaptation)
    evidence_files = sorted(list(OUT.glob("candidates-*.json")) + list(OUT.glob("qualification-*.json")) + list(OUT.glob("scores-*.npz")) + [OUT / "feature-adaptation.json"])
    result = {"version": "draw-reduction-v1", "generatedAt": datetime.now(timezone.utc).isoformat(), "sourceHashes": hashes,
              "evidenceHashes": {p.name: digest(p) for p in evidence_files},
              "featureAdaptation": {"totalColumns": len(metadata), "observedColumns": sum(a["finiteMatches"] > 0 for a in adaptation),
                                    "candidateOperands": len(candidate_features), "views": {view: sum(m["view"] == view for m in metadata) for view in ("market", "safeHistory", "snapshotDerived")}},
              "counts": {"games": len(y), "draws": int(y.sum()), "nonDraws": int((~y).sum())}, "protocol": protocol,
              "methods": METHODS, "fullHistory": {"base": stats(historical_base, y, all_ix), "baseRuleIds": historical_base_ids,
              "baseSelectedMatchIds": [ids[i] for i in np.flatnonzero(historical_base)], "candidateCount": len(design["rules"]),
              "qualifiedRules": {k: len(v) for k, v in pools.items()}, "paths": full_cases, "modelFits": fit_details},
              "temporal": {"folds": folds, "summaries": summaries, "selection": selection, "modelFits": model_details},
              "warnings": protocol["limitations"] + [
                  "全量过滤及评分模型使用已知赛果拟合，不能作为未来命中率；较早数据定规则、较晚数据复核的结果另表列出。",
                  "900/850/800代表占总970平的目标比例，跨日期样本实际只有717平；实际保留可能达不到目标。",
                  "误选减少必须同时报告误删平局；同量均赔比较按后区评分排序、并列全保留，使用后区特征但不使用后区赛果。",
              ]}
    write(OUT / "analysis.json", result)
    write(OUT / "summary.json", {"counts": result["counts"], "sourceHashes": hashes,
          "fullHistory": {"base": result["fullHistory"]["base"], "paths": [{k: c[k] for k in ("method", "target", "metrics", "targetReached")} for c in full_cases]},
          "temporal": {"summaries": summaries, "selection": selection}, "warnings": result["warnings"]})
    write(OUT / "contexts.json", contexts)
    with (OUT / "predictions.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(prediction_rows[0]))
        writer.writeheader()
        writer.writerows(prediction_rows)
    with (OUT / "comparison.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        fields = ["method", "target", "role", "games", "draws", "tp", "fp", "fn", "tn", "selected", "precision", "recall", "removedFP", "lostTP", "fpReduction", "recallLoss", "targetReached", "uniformExpectedTP", "uniformExpectedFP"]
        writer = csv.DictWriter(stream, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(summaries)
    from draw_reduction_report import render_report
    render_report(result, contexts, OUT / "report.html")
    assert hashes == {**{name: digest(path) for name, path in files.items()}, "canonicalFeatureMatrix": matrix_hash}, "source changed while fitting"
    print("selection", [(s["target"], s["method"], s["audit"]["tp"], s["audit"]["fp"]) for s in selection], flush=True)
    print("completed", OUT, flush=True)


if __name__ == "__main__":
    main()
