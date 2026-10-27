#!/usr/bin/env python3
"""Frozen-business-rule OR set coverage, with chronological internal checks.

Offline only: reads the frozen research exports; never connects to a service,
database, or Redis. Full-history paths deliberately use labels and are fitted
descriptions. The separate temporal paths fit only their earlier train/cal rows.
"""
from __future__ import annotations

import csv
import hashlib
import json
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

import numpy as np

from draw_cover_candidates import build_candidates

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "reports/draw-full-feature-study"
OUT = ROOT / "reports/draw-cover-study"
EPS = 1e-9
TARGETS = (900, 920, 950, 970)


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")


def compare(values, op, threshold):
    if op == "ge":
        return values >= threshold
    if op == "le":
        return values <= threshold
    if op == "eq":
        return values == threshold
    raise ValueError(op)


def evaluate_candidates(X, names, design):
    """Each operand finite, guards part of qualification, not matching signals."""
    columns = {name: i for i, name in enumerate(names)}
    atoms, eligibility = {}, {}
    for atom in design["atoms"]:
        values = X[:, columns[atom["feature"]]]
        eligible = np.isfinite(values)
        for guard in atom["guards"]:
            gv = X[:, columns[guard["feature"]]]
            eligible &= np.isfinite(gv) & compare(gv, guard["op"], guard["value"])
        transformed = np.abs(values) if atom["transform"] == "abs" else values
        atoms[atom["id"]] = eligible & compare(transformed, atom["op"], atom["value"])
        eligibility[atom["id"]] = eligible
    matches, qualified = [], []
    for rule in design["rules"]:
        matches.append(np.logical_and.reduce([atoms[ident] for ident in rule["allOf"]]))
        qualified.append(np.logical_and.reduce([eligibility[ident] for ident in rule["allOf"]]))
    return np.array(matches), np.array(qualified), atoms, eligibility


def metrics(mask, y, indices=None):
    if indices is not None:
        mask, y = mask[indices], y[indices]
    tp = int(np.count_nonzero(mask & y))
    fp = int(np.count_nonzero(mask & ~y))
    fn = int(np.count_nonzero(~mask & y))
    tn = int(np.count_nonzero(~mask & ~y))
    return {"tp": tp, "fp": fp, "fn": fn, "tn": tn, "selected": tp + fp,
            "precision": tp / (tp + fp) if tp + fp else None,
            "recall": tp / (tp + fn) if tp + fn else None,
            "specificity": tn / (tn + fp) if tn + fp else None}


def pool_qualification(M, E, atom_masks, atom_eligible, design, y, train, calibration):
    def nonconstant(mask, eligible):
        n = int(eligible[train].sum())
        hits = int(mask[train].sum())
        return 0 < hits < n

    atom_ok = {ident: nonconstant(mask, atom_eligible[ident]) for ident, mask in atom_masks.items()}
    details, broad, suspected = [], [], []
    for ri, rule in enumerate(design["rules"]):
        sm_train, sm_cal = M[ri, train], M[ri, calibration]
        qe_train, qe_cal = E[ri, train], E[ri, calibration]
        nt, nc = int(sm_train.sum()), int(sm_cal.sum())
        at, ac = int(qe_train.sum()), int(qe_cal.sum())
        tp_t, tp_c = int((sm_train & y[train]).sum()), int((sm_cal & y[calibration]).sum())
        bt = float(y[train][qe_train].mean()) if at else None
        bc = float(y[calibration][qe_cal].mean()) if ac else None
        pt, pc = tp_t / nt if nt else None, tp_c / nc if nc else None
        reasons = []
        if nt < 40:
            reasons.append("train_selected_below_40")
        if nc < 10:
            reasons.append("cal_selected_below_10")
        if not nonconstant(M[ri], E[ri]):
            reasons.append("rule_constant_within_train_eligible")
        if not all(atom_ok[ident] for ident in rule["allOf"]):
            reasons.append("atom_constant_within_train_eligible")
        is_broad = not reasons
        is_suspected = is_broad and pt > bt + EPS and pc > bc + EPS
        if is_broad:
            broad.append(ri)
        if is_suspected:
            suspected.append(ri)
        details.append({"ruleId": rule["id"], "broad": is_broad, "suspected": is_suspected,
                        "train": {"selected": nt, "draws": tp_t, "eligible": at, "precision": pt, "baseline": bt},
                        "calibration": {"selected": nc, "draws": tp_c, "eligible": ac, "precision": pc, "baseline": bc},
                        "broadExclusions": reasons})
    return {"broad": broad, "suspected": suspected}, details


def greedy(M, rules, indices, y, fit_indices=None, penalty=0.0, fp_budget=None):
    """No pruning by later rows: deterministic order from the fit subset only."""
    fit = np.arange(len(y)) if fit_indices is None else np.asarray(fit_indices, dtype=int)
    covered = np.zeros(len(y), dtype=bool)
    remaining = set(indices)
    steps = []
    while remaining:
        candidates = sorted(remaining)
        positive = fit[y[fit] & ~covered[fit]]
        negative = fit[~y[fit] & ~covered[fit]]
        if not len(positive):
            break
        gain_tp = M[candidates][:, positive].sum(axis=1)
        gain_fp = M[candidates][:, negative].sum(axis=1)
        current_fp = int((covered[fit] & ~y[fit]).sum())
        options = []
        for j, ri in enumerate(candidates):
            tp, fp = int(gain_tp[j]), int(gain_fp[j])
            score = tp - penalty * fp
            if tp == 0 or score <= EPS or fp_budget is not None and current_fp + fp > fp_budget:
                continue
            options.append(((-score, -tp, fp, len(rules[ri]["allOf"]), rules[ri]["id"]), ri, tp, fp))
        if not options:
            break
        _, chosen, tp, fp = min(options)
        covered |= M[chosen]
        remaining.remove(chosen)
        rule = rules[chosen]
        steps.append({"step": len(steps) + 1, "ruleId": rule["id"], "label": rule["label"],
                      "newTP": tp, "newFP": fp, **metrics(covered, y, fit)})
    return steps, covered


def complement_flags(steps, design):
    """Warn about structural tautologies, including common-condition branches."""
    atoms = {a["id"]: a for a in design["atoms"]}
    rules = {r["id"]: r for r in design["rules"]}
    flags = []

    def complementary(a, b):
        if (a["feature"], a["transform"], a["guards"]) != (b["feature"], b["transform"], b["guards"]):
            return False
        low = a if a["op"] == "le" else b
        high = a if a["op"] == "ge" else b
        if low["op"] == "le" and high["op"] == "ge" and low["value"] + EPS >= high["value"]:
            return True
        return (a["op"] == b["op"] == "eq" and {a["value"], b["value"]} == {0, 1}
                and (".minimum." in a["feature"] or a["feature"].endswith("drawAbsMaximum")))

    for i, first in enumerate(steps):
        aa = set(rules[first["ruleId"]]["allOf"])
        for second in steps[i + 1:]:
            bb = set(rules[second["ruleId"]]["allOf"])
            common = aa & bb
            left, right = aa - common, bb - common
            if len(left) == len(right) == 1 and complementary(atoms[next(iter(left))], atoms[next(iter(right))]):
                flags.append({"first": first["ruleId"], "second": second["ruleId"],
                              "commonAtoms": sorted(common), "reason": "互补分支的或合并退化为共同条件；没有共同条件时近似有输入即选"})
    return flags


def describe_path(path_id, pool, method, penalty, steps, mask, y, upper, design, fp_budget=None):
    out = {"id": path_id, "pool": pool, "method": method, "penalty": penalty,
           "steps": steps, "final": metrics(mask, y), "upperBound": upper,
           "milestones": {str(target): next((s for s in steps if s["tp"] >= target), None) for target in TARGETS},
           "complementFlags": complement_flags(steps, design), "fpBudget": fp_budget}
    out["nearAllSelection"] = bool(mask.mean() >= .95)
    return out


def temporal_study(M, E, atom_masks, atom_eligible, design, y, folds, ids):
    records, pooled = [], {}
    fractions = [(str(t), t / 970) for t in (900, 920, 950)] + [("maximum", 1.0)]
    for fold in folds:
        train = np.array(fold["train"], dtype=int)
        calibration = np.array(fold["calibration"], dtype=int)
        test = np.array(fold["test"], dtype=int)
        pools, details = pool_qualification(M, E, atom_masks, atom_eligible, design, y, train, calibration)
        pools_out = {}
        for name, candidates in pools.items():
            steps, _ = greedy(M, design["rules"], candidates, y, train)
            cases = []
            for target, fraction in fractions:
                target_count = int(np.ceil(fraction * int(y[train].sum()) - EPS))
                prefix = next((s["step"] for s in steps if s["tp"] >= target_count), len(steps))
                selected_ids = [s["ruleId"] for s in steps[:prefix]]
                ri = {r["id"]: i for i, r in enumerate(design["rules"])}
                mask = np.logical_or.reduce([M[ri[rid]] for rid in selected_ids]) if selected_ids else np.zeros(len(y), bool)
                result = {"target": target, "trainingRecallTarget": fraction, "trainingDrawTarget": target_count,
                          "trainingTargetReached": bool(metrics(mask, y, train)["tp"] >= target_count),
                          "prefix": prefix, "ruleIds": selected_ids,
                          "train": metrics(mask, y, train), "calibration": metrics(mask, y, calibration),
                          "test": metrics(mask, y, test)}
                cases.append(result)
                for role in ("all", fold["role"]):
                    key = (name, target, role)
                    if key not in pooled:
                        pooled[key] = {"pool": name, "target": target, "role": role,
                                       "predictions": np.zeros(len(y), bool), "indices": []}
                    pooled[key]["predictions"][test] = mask[test]
                    pooled[key]["indices"].extend(test.tolist())
            pools_out[name] = {"qualified": len(candidates), "qualification": details,
                               "steps": steps, "cases": cases}
        records.append({k: v for k, v in fold.items() if k != "matchIds"} | {
            "fitMatchIds": {"train": [ids[i] for i in train], "calibration": [ids[i] for i in calibration]},
            "pools": pools_out})
    summaries = []
    for (pool, target, role), item in pooled.items():
        indices = np.array(item["indices"], dtype=int)
        assert len(set(item["indices"])) == len(indices)
        summaries.append({"pool": pool, "target": target, "role": role, "games": len(indices),
                          "draws": int(y[indices].sum()), **metrics(item["predictions"], y, indices)})
    return {"folds": records, "summaries": summaries,
            "warning": "规则顺序只由该折早期训练确定，资格另看早期校准；较晚测试不补规则。全部旧日期已反复查看，属于重复内部复核，采集时间仍未证实早于开球。"}


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    files = {"dataset": SOURCE / "dataset.json", "matrix": SOURCE / "features.csv",
             "manifest": SOURCE / "feature-manifest.json", "folds": SOURCE / "folds.json",
             "protocol": OUT / "protocol.json", "candidateScript": Path(__file__).with_name("draw_cover_candidates.py"),
             "reportScript": Path(__file__).with_name("draw_cover_report.py"),
             "inputAudit": SOURCE / "union-input-availability-audit.json",
             "script": Path(__file__)}
    hashes = {k: digest(v) for k, v in files.items()}
    manifest = read_json(files["manifest"])
    design = build_candidates(manifest)
    assert design == read_json(OUT / "candidate-design.json"), "candidate design changed"
    hashes["candidateDesign"] = digest(OUT / "candidate-design.json")
    protocol = read_json(files["protocol"])
    assert hashes["dataset"] == protocol["datasetSHA256"]
    with files["matrix"].open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.reader(stream)
        header = next(reader)
        data = list(reader)
    ids = [row[0] for row in data]
    labels = np.array([int(row[1]) for row in data])
    assert set(labels) == {0, 1, 2} and header[:2] == ["matchId", "resultLabel"]
    y = labels == 1
    X = np.array([[float(value) if value else np.nan for value in row[2:]] for row in data])
    assert header[2:] == [m["name"] for m in manifest]
    matrix_hash = hashlib.sha256(np.nan_to_num(X, nan=-9.87654321e99).astype("<f8").tobytes()).hexdigest()
    assert matrix_hash == read_json(SOURCE / "analysis.json")["featureMatrixSHA256"]
    hashes["canonicalFeatureMatrix"] = matrix_hash
    dataset = read_json(files["dataset"])
    by_id = {str(r["matchId"]): r for r in dataset["rows"]}
    rows = [by_id[ident] for ident in ids]
    assert len(rows) == len(by_id) == len(y) == 4161 and int(y.sum()) == 970
    assert all(int(r["result"]) == labels[i] and (r["homeScore"] == r["awayScore"]) == y[i]
               for i, r in enumerate(rows))
    contexts = [{"matchId": str(r["matchId"]), "date": r["date"], "matchTime": r["matchTime"],
                 "league": r["league"], "home": r["home"], "away": r["guest"],
                 "score": f'{r["homeScore"]}:{r["awayScore"]}', "isDraw": bool(y[i])} for i, r in enumerate(rows)]
    train = np.array([i for i, r in enumerate(rows) if r["date"] <= protocol["developmentDates"]["trainEnd"]])
    calibration = np.array([i for i, r in enumerate(rows) if protocol["developmentDates"]["trainEnd"] < r["date"] <= protocol["developmentDates"]["calibrationEnd"]])
    M, E, atom_masks, atom_eligible = evaluate_candidates(X, header[2:], design)
    pools, qualification = pool_qualification(M, E, atom_masks, atom_eligible, design, y, train, calibration)
    print(f"qualified broad={len(pools['broad'])}, suspected={len(pools['suspected'])}", flush=True)
    paths, uppers, missed = [], {}, []
    raw_union = M.any(axis=0)
    for name, candidates in pools.items():
        upper_mask = M[candidates].any(axis=0) if candidates else np.zeros(len(y), bool)
        upper = metrics(upper_mask, y)
        uppers[name] = upper
        for penalty in (0., .25, .5, 1., 2.):
            steps, mask = greedy(M, design["rules"], candidates, y, penalty=penalty)
            method = "pure" if penalty == 0 else "penalized"
            path = describe_path(f"{name}_{method}_{penalty:g}", name, method, penalty, steps, mask, y, upper, design)
            paths.append(path)
            if penalty == 0:
                # Exact candidate-OR TP ceiling (not minimum rule/false-positive cost).
                assert path["final"]["tp"] == upper["tp"]
                for i in np.flatnonzero(y & ~mask):
                    reason = ("no_eligible_candidate" if not E[candidates, i].any() else
                              "no_candidate_match" if not upper_mask[i] else "matched_not_selected")
                    root_reason = ("no_raw_candidate_input_qualification" if not E[:, i].any() else
                                   "no_fixed_business_rule_match" if not raw_union[i] else
                                   "matches_only_early_excluded_rules" if not upper_mask[i] else "matched_not_selected")
                    missed.append({"pool": name, **contexts[i], "reason": reason,
                                   "rawCandidateMatched": bool(raw_union[i]), "rootReason": root_reason})
            print(name, method, penalty, len(steps), path["final"], flush=True)
        for fraction in (.1, .25, .5, .75):
            budget = int(np.floor(fraction * int((~y).sum())))
            steps, mask = greedy(M, design["rules"], candidates, y, fp_budget=budget)
            paths.append(describe_path(f"{name}_fpbudget_{fraction:g}", name, "fpbudget", 0., steps, mask, y, upper, design, budget))
    temporal = temporal_study(M, E, atom_masks, atom_eligible, design, y, read_json(files["folds"]), ids)
    print("temporal evaluation complete", flush=True)
    # Scope ceilings are all candidates before train/cal pool qualification.
    atom_by_id = {a["id"]: a for a in design["atoms"]}
    scope_ceilings = {}
    for title, views in (("market", {"market"}), ("market_history", {"market", "safeHistory"}),
                         ("all", {"market", "safeHistory", "snapshotDerived"})):
        candidates = [i for i, r in enumerate(design["rules"]) if all(atom_by_id[a]["view"] in views for a in r["allOf"])]
        mask = M[candidates].any(axis=0) if candidates else np.zeros(len(y), bool)
        scope_ceilings[title] = {"rules": len(candidates), **metrics(mask, y)}
    result = {"version": "draw-cover-v1", "generatedAt": datetime.now(timezone.utc).isoformat(),
              "counts": {"games": len(y), "draws": int(y.sum()), "nonDraws": int((~y).sum())},
              "sourceHashes": hashes, "rules": design["rules"],
              "candidates": {"atoms": len(design["atoms"]), "rules": len(design["rules"]),
                             "qualified": {name: len(value) for name, value in pools.items()},
                             "qualification": qualification},
              "development": {"train": train.tolist(), "calibration": calibration.tolist()},
              "allPositiveReference": metrics(np.ones(len(y), bool), y),
              "observedInputCeilings": read_json(files["inputAudit"])["observedInputUpperBounds"],
              "candidateUnionUpperBound": metrics(raw_union, y), "scopeCeilings": scope_ceilings,
              "poolUpperBounds": uppers, "paths": paths, "temporal": temporal,
              "missedBreakdown": {pool: dict(Counter(r["rootReason"] for r in missed if r["pool"] == pool)) for pool in pools},
              "warnings": [
                  "全量叠加顺序使用了970场已知平局标签，属于历史拟合覆盖；不是未来命中率。",
                  "固定业务规则并不穷尽所有可能算法。全集OR上界只针对本次1487条候选，不是足球预测理论上界。",
                  "疑似池仅要求早期训练/校准各自高于同输入基准；未经多重显著性确认，也不保证组合后精度。",
                  "宽泛池只检查样本与非恒真信号，不能因覆盖高而认定具备平局倾向。",
                  "近乎全选或互补分支会标记；缺数据不匹配，真实零保留，样本数只作历史资格保护。",
                  "全量成本路线和误选预算是贪心启发式，不保证最少规则或最少误选。",
                  "所有采集时间尚不能证明早于开球；跨日期复核仍然只是重复的历史快照关联。",
              ]}
    write_json(OUT / "analysis.json", result)
    write_json(OUT / "summary.json", {"counts": result["counts"], "sourceHashes": hashes,
               "candidateCounts": {key: result["candidates"][key] for key in ("atoms", "rules", "qualified")},
               "candidateUnionUpperBound": result["candidateUnionUpperBound"],
               "poolUpperBounds": uppers, "scopeCeilings": scope_ceilings,
               "paths": [{key: p[key] for key in ("id", "pool", "method", "penalty", "fpBudget", "final", "milestones", "nearAllSelection")} for p in paths],
               "missedBreakdown": result["missedBreakdown"], "temporal": temporal["summaries"],
               "warnings": result["warnings"]})
    write_json(OUT / "missed-draws.json", missed)
    with (OUT / "paths.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        fields = ["path", "pool", "method", "penalty", "step", "ruleId", "label", "newTP", "newFP", "tp", "fp", "fn", "tn", "selected", "precision", "recall", "specificity"]
        writer = csv.DictWriter(stream, fieldnames=fields)
        writer.writeheader()
        for path in paths:
            for step in path["steps"]:
                writer.writerow({"path": path["id"], **{k: path[k] for k in ("pool", "method", "penalty")}, **step})
    with (OUT / "missed-draws.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(missed[0]) if missed else ["pool", "matchId"])
        writer.writeheader()
        writer.writerows(missed)
    # The report contains labels for review; labels never enter predicate masks.
    selected_ids = {s["ruleId"] for p in paths for s in p["steps"]}
    rule_matches = {r["id"]: np.flatnonzero(M[i]).tolist() for i, r in enumerate(design["rules"]) if r["id"] in selected_ids}
    rule_by_id = {r["id"]: r for r in design["rules"]}
    qualification_by_id = {r["ruleId"]: r for r in qualification}
    write_json(OUT / "selected-rule-details.json", [{"rule": rule_by_id[ident],
               "atoms": [atom_by_id[a] for a in rule_by_id[ident]["allOf"]],
               "qualification": qualification_by_id[ident]} for ident in sorted(selected_ids)])
    write_json(OUT / "interactive-data.json", {"contexts": contexts, "ruleMatches": rule_matches})
    from draw_cover_report import render_report
    render_report(result, contexts, rule_matches, OUT / "report.html")
    assert hashes == {**{k: digest(v) for k, v in files.items()},
                      "candidateDesign": digest(OUT / "candidate-design.json"), "canonicalFeatureMatrix": matrix_hash}
    print("wrote", OUT, flush=True)


if __name__ == "__main__":
    main()
