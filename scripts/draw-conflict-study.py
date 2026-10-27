#!/usr/bin/env python3
"""Offline tests of fixed popularity/price-conflict hypotheses, own data only."""
from __future__ import annotations
import csv
import hashlib
import importlib.util
import json
import math
import sys
from collections import Counter, defaultdict
from datetime import datetime, timezone
from pathlib import Path

sys.dont_write_bytecode = True
import numpy as np
from scipy.stats import fisher_exact
from draw_conflict_features import build_conflict_features
from draw_conflict_rules import build_rules

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "reports/draw-full-feature-study"
OUT = ROOT / "reports/draw-conflict-study"
EPS = 1e-9
STRATEGIES = ("clean_best1", "clean_or3", "all_best1", "all_or3", "all_enriched_or3", "smallest_sensitivity_best1")


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False) + "\n", encoding="utf-8")


def load_cover():
    spec = importlib.util.spec_from_file_location("conflict_cover", Path(__file__).with_name("draw-cover-study.py"))
    value = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = value
    spec.loader.exec_module(value)
    return value


cover = load_cover()


def compare(values, op, value):
    if op == "ge":
        return values >= value
    if op == "le":
        return values <= value
    if op == "gt":
        return values > value
    if op == "lt":
        return values < value
    if op == "eq":
        return values == value
    if op == "finite":
        return np.isfinite(values)
    raise ValueError(op)


def masks(X, names, catalog):
    columns = {name: j for j, name in enumerate(names)}
    am, ae = {}, {}
    for atom in catalog["atoms"]:
        v = X[:, columns[atom["feature"]]]
        eligible = np.isfinite(v)
        for g in atom["guards"]:
            gv = X[:, columns[g["feature"]]]
            eligible &= np.isfinite(gv) & compare(gv, g["op"], g["value"])
        ae[atom["id"]] = eligible
        transformed = np.abs(v) if atom["transform"] == "abs" else v
        am[atom["id"]] = eligible & compare(transformed, atom["op"], atom["value"])
    M, E, P, I = [], [], [], []
    for rule in catalog["rules"]:
        eligible = np.logical_and.reduce([ae[a] for a in rule["allOf"]])
        match = np.logical_and.reduce([am[a] for a in rule["allOf"]])
        parent = np.logical_and.reduce([am[a] for a in rule["parentAllOf"]]) & eligible
        image = np.logical_and.reduce([am[a] for a in rule["imageAllOf"]]) & eligible
        assert not (match & ~parent).any() and not (match & ~image).any()
        M.append(match)
        E.append(eligible)
        P.append(parent)
        I.append(image)
    return (np.array(m) for m in (M, E, P, I))


def wilson(k, n):
    if not n:
        return [None, None]
    z = 1.959963984540054
    p = k / n
    den = 1 + z * z / n
    center = (p + z * z / (2 * n)) / den
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return [max(0., center - half), min(1., center + half)]


def outcome_stats(mask, labels, indices, favorite, market):
    selected = np.asarray(indices)[mask[indices]]
    actual = labels[selected]
    home, draws, away = (int((actual == k).sum()) for k in (0, 1, 2))
    n = len(selected)
    favorite_wins = home if favorite == "home" else away
    favorite_losses = away if favorite == "home" else home
    known = np.isfinite(market[selected])
    return {"games": n, "homeWins": home, "draws": draws, "awayWins": away,
            "homeWinRate": home / n if n else None, "drawRate": draws / n if n else None, "awayWinRate": away / n if n else None,
            "coverage": draws / int((labels[indices] == 1).sum()) if (labels[indices] == 1).any() else None,
            "favoriteWins": favorite_wins, "favoriteLosses": favorite_losses, "favoriteNotWins": draws + favorite_losses,
            "favoriteNotWinRate": (draws + favorite_losses) / n if n else None,
            "drawAmongFavoriteNotWins": draws / (draws + favorite_losses) if draws + favorite_losses else None,
            "meanMarketDrawProb": float(market[selected][known].mean()) if known.any() else None,
            "marketAvailable": int(known.sum()), "drawWilson95": wilson(draws, n)}


def rule_stats(M, E, P, I, ri, rule, labels, indices, market):
    child = outcome_stats(M[ri], labels, indices, rule["favorite"], market)
    parent = outcome_stats(P[ri], labels, indices, rule["favorite"], market)
    no_conflict = outcome_stats(P[ri] & ~M[ri], labels, indices, rule["favorite"], market)
    eligible = outcome_stats(E[ri], labels, indices, rule["favorite"], market)
    image = outcome_stats(I[ri], labels, indices, rule["favorite"], market)
    p = 1.
    if child["games"] and no_conflict["games"]:
        p = float(fisher_exact([[child["draws"], child["games"] - child["draws"]],
                               [no_conflict["draws"], no_conflict["games"] - no_conflict["draws"]]], alternative="two-sided").pvalue)
    lift = lambda other: child["drawRate"] - other["drawRate"] if child["drawRate"] is not None and other["drawRate"] is not None else None
    return {**child, "parent": parent, "noConflict": no_conflict, "eligible": eligible, "imageParent": image,
            "parentLift": lift(parent), "eligibleLift": lift(eligible), "imageLift": lift(image), "fisherP": p}


def bh(values):
    p = np.asarray(values, float)
    order = np.argsort(p, kind="stable")
    adjusted = np.minimum.accumulate((p[order] * len(p) / np.arange(1, len(p) + 1))[::-1])[::-1]
    out = np.empty(len(p))
    out[order] = np.minimum(adjusted, 1.)
    return out.tolist()


def qualification(train, calibration):
    enriched = train["games"] >= 40 and calibration["games"] >= 10 and all(s["eligibleLift"] is not None and s["eligibleLift"] > EPS for s in (train, calibration))
    incremental = enriched and train["noConflict"]["games"] >= 20 and calibration["noConflict"]["games"] >= 5 and all(s["parentLift"] is not None and s["parentLift"] > EPS for s in (train, calibration))
    reasons = []
    if train["games"] < 40:
        reasons.append("train_selected_below40")
    if calibration["games"] < 10:
        reasons.append("calibration_selected_below10")
    if not enriched:
        reasons.append("not_draw_enriched_in_both_development_intervals")
    if not incremental:
        reasons.append("parent_increment_or_disjoint_control_support_not_met")
    return {"enriched": bool(enriched), "incremental": bool(incremental), "reasons": reasons}


def choose_rules(rules, train_stats, cal_stats, qualifications, strategy):
    enriched_only = strategy == "all_enriched_or3"
    sensitivity = strategy == "smallest_sensitivity_best1"
    candidates = []
    for i, rule in enumerate(rules):
        if (rule["tier"] == "sensitivity") != sensitivity or strategy.startswith("clean_") and rule["view"] != "clean":
            continue
        if not qualifications[i]["enriched" if enriched_only else "incremental"]:
            continue
        a, b = train_stats[i], cal_stats[i]
        key = "eligibleLift" if enriched_only else "parentLift"
        candidates.append(((-min(a[key], b[key]), -min(a["drawWilson95"][0], b["drawWilson95"][0]), -a["games"], rule["id"]), i))
    take = 1 if strategy.endswith("best1") else 3
    return [i for _, i in sorted(candidates)[:take]]


def union_mask(M, indices):
    return M[indices].any(axis=0) if indices else np.zeros(M.shape[1], bool)


def union_stats(selected, labels, indices):
    y = labels == 1
    result = {"games": len(indices), "draws": int(y[indices].sum()), **cover.metrics(selected, y, indices)}
    result["precisionWilson95"] = wilson(result["tp"], result["selected"])
    return result


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    files = {"dataset": SOURCE / "dataset.json", "matrix": SOURCE / "features.csv", "manifest": SOURCE / "feature-manifest.json",
             "folds": SOURCE / "folds.json", "coverAnalysis": ROOT / "reports/draw-cover-study/analysis.json",
             "protocol": OUT / "protocol.json", "adapter": Path(__file__).with_name("draw_conflict_features.py"),
             "ruleGenerator": Path(__file__).with_name("draw_conflict_rules.py"), "script": Path(__file__),
             "reportScript": Path(__file__).with_name("draw_conflict_report.py")}
    source_hashes = {k: digest(p) for k, p in files.items()}
    protocol, metadata = read(files["protocol"]), read(files["manifest"])
    assert source_hashes["dataset"] == protocol["datasetSHA256"]
    with files["matrix"].open(encoding="utf-8-sig", newline="") as stream:
        reader = csv.reader(stream)
        header, data = next(reader), list(reader)
    ids = [r[0] for r in data]
    labels = np.array([int(r[1]) for r in data])
    X = np.array([[float(v) if v else np.nan for v in r[2:]] for r in data])
    names = header[2:]
    assert names == [m["name"] for m in metadata] and header[:2] == ["matchId", "resultLabel"]
    canonical = hashlib.sha256(np.nan_to_num(X, nan=-9.87654321e99).astype("<f8").tobytes()).hexdigest()
    assert canonical == read(SOURCE / "analysis.json")["featureMatrixSHA256"]
    source_hashes["canonicalFeatureMatrix"] = canonical
    by_id = {str(r["matchId"]): r for r in read(files["dataset"])["rows"]}
    rows = [by_id[ident] for ident in ids]
    assert len(ids) == len(by_id) == 4161 and int((labels == 1).sum()) == 970
    assert all(int(row["result"]) == labels[i] and (row["homeScore"] == row["awayScore"]) == (labels[i] == 1) for i, row in enumerate(rows))
    Z, extra_meta = build_conflict_features(rows, X, names, metadata)
    assert Z.shape == (len(ids), 42)
    contexts = [{"matchId": ids[i], "date": r["date"], "matchTime": r["matchTime"], "league": r["league"], "home": r["home"],
                 "away": r["guest"], "score": f'{r["homeScore"]}:{r["awayScore"]}', "resultLabel": int(labels[i]), "isDraw": bool(labels[i] == 1)} for i, r in enumerate(rows)]
    X = np.concatenate([X, Z], axis=1)
    metadata = metadata + extra_meta
    names += [m["name"] for m in extra_meta]
    catalog = build_rules(metadata)
    write(OUT / "catalog.json", catalog)
    write(OUT / "extra-feature-manifest.json", extra_meta)
    np.savez_compressed(OUT / "extra-features.npz", matchIds=np.asarray(ids), values=Z)
    print("fixed catalog", len(catalog["atoms"]), len(catalog["rules"]), dict(Counter(r["tier"] for r in catalog["rules"])), flush=True)
    M, E, P, I = masks(X, names, catalog)
    rules = catalog["rules"]
    market = X[:, names.index("euro.average.latest.prob.draw")]
    old_cover, folds = read(files["coverAnalysis"]), read(files["folds"])
    intervals = {"full": np.arange(len(labels)), "train": np.array(old_cover["development"]["train"], int),
                 "calibration": np.array(old_cover["development"]["calibration"], int),
                 "development": np.array([i for f in folds if f["role"] == "development" for i in f["test"]], int),
                 "audit": np.array([i for f in folds if f["role"] == "lockedInternalAudit" for i in f["test"]], int)}
    global_stats = {role: [rule_stats(M, E, P, I, i, r, labels, ix, market) for i, r in enumerate(rules)] for role, ix in intervals.items()}
    q_train = bh([s["fisherP"] for s in global_stats["train"]])
    q_full = bh([s["fisherP"] for s in global_stats["full"]])
    qualified = [qualification(global_stats["train"][i], global_stats["calibration"][i]) for i in range(len(rules))]
    detailed_rules = [{**r, "stats": {role: values[i] for role, values in global_stats.items()},
                       "qualification": qualified[i], "qTrain": q_train[i], "qFull": q_full[i]} for i, r in enumerate(rules)]
    historical_unions = []
    for strategy in STRATEGIES:
        chosen = choose_rules(rules, global_stats["train"], global_stats["calibration"], qualified, strategy)
        selected = union_mask(M, chosen)
        historical_unions.append({"method": strategy, "ruleIds": [rules[i]["id"] for i in chosen],
                                  "metrics": union_stats(selected, labels, intervals["full"]),
                                  "label": "较早训练/校准资格与排序应用于全历史，含用于选规则的样本，属描述性复盘"})
    print("early qualifications", Counter((q["enriched"], q["incremental"]) for q in qualified), flush=True)
    fold_results, prediction_rows = [], []
    predictions, markets = {}, {}
    all_fold_stats = []
    for fold in folds:
        train, calibration, test = (np.array(fold[k], int) for k in ("train", "calibration", "test"))
        ts = [rule_stats(M, E, P, I, i, r, labels, train, market) for i, r in enumerate(rules)]
        cs = [rule_stats(M, E, P, I, i, r, labels, calibration, market) for i, r in enumerate(rules)]
        es = [rule_stats(M, E, P, I, i, r, labels, test, market) for i, r in enumerate(rules)]
        qs = [qualification(ts[i], cs[i]) for i in range(len(rules))]
        selected_rules, evaluated = {}, {}
        training_prior = float((labels[train] == 1).mean())
        market_scores = np.where(np.isfinite(market), market, training_prior)
        markets[fold["fold"]] = market_scores
        for strategy in STRATEGIES:
            chosen = choose_rules(rules, ts, cs, qs, strategy)
            selected = union_mask(M, chosen)
            selected_rules[strategy] = [rules[i]["id"] for i in chosen]
            predictions[(fold["fold"], strategy)] = selected
            evaluated[strategy] = union_stats(selected, labels, test)
            for i in test:
                prediction_rows.append({"matchId": ids[i], "fold": fold["fold"], "role": fold["role"], "method": strategy,
                                        "resultLabel": int(labels[i]), "selected": int(selected[i]), "marketScore": float(market_scores[i])})
        fold_results.append({k: v for k, v in fold.items() if k != "matchIds"} | {
            "selected": selected_rules, "metrics": evaluated,
            "qualifications": {"enriched": sum(q["enriched"] for q in qs), "incremental": sum(q["incremental"] for q in qs)}})
        all_fold_stats.append({"fold": fold["fold"], "rules": [{"ruleId": r["id"], "train": ts[i], "calibration": cs[i], "test": es[i],
                                                                 "qualification": qs[i]} for i, r in enumerate(rules)]})
        print("fold", fold["fold"], "qualified", fold_results[-1]["qualifications"], "selected", {k: len(v) for k, v in selected_rules.items()}, flush=True)
    summaries = []
    for role in ("all", "development", "lockedInternalAudit"):
        included = [f for f in folds if role == "all" or f["role"] == role]
        ix = np.array([i for f in included for i in f["test"]], int)
        for strategy in STRATEGIES:
            selected, market_selected = np.zeros(len(labels), bool), np.zeros(len(labels), bool)
            for fold in included:
                test = np.array(fold["test"], int)
                selected[test] = predictions[(fold["fold"], strategy)][test]
                count = int(selected[test].sum())
                if count:
                    scores = markets[fold["fold"]][test]
                    threshold = np.sort(scores)[::-1][count - 1]
                    market_selected[test] = scores >= threshold
            metric = union_stats(selected, labels, ix)
            summaries.append({"method": strategy, "role": role, **metric,
                              "marketSameSize": union_stats(market_selected, labels, ix),
                              "marketTiesMayIncreaseCount": bool(selected[ix].sum() != market_selected[ix].sum()),
                              "ruleIdsByFold": {str(f["fold"]): fold_results[f["fold"]]["selected"][strategy] for f in included}})
    eligible_methods = [s for s in summaries if s["role"] == "development" and s["method"] in STRATEGIES[:4] and s["selected"] >= 60]
    if eligible_methods:
        winner = min(eligible_methods, key=lambda s: (-s["precisionWilson95"][0], -s["tp"], s["fp"], s["method"]))
        selection = {"method": winner["method"], "development": winner,
                     "audit": next(s for s in summaries if s["role"] == "lockedInternalAudit" and s["method"] == winner["method"]),
                     "reason": "Only first3 development blocks, minimum60selected, highest Wilson lower bound; all audit outcomes excluded from strategy selection."}
    else:
        selection = {"method": None, "reason": "No primary strategy had minimum60 development selections; no winner promoted."}
    result = {"version": "draw-conflict-v1", "generatedAt": datetime.now(timezone.utc).isoformat(), "sourceHashes": source_hashes,
              "counts": {"games": len(labels), "draws": int((labels == 1).sum()), "nonDraws": int((labels != 1).sum()),
                         "homeWins": int((labels == 0).sum()), "awayWins": int((labels == 2).sum()), "newFeatures": Z.shape[1],
                         "rules": len(rules), "primaryRules": sum(r["tier"] == "primary" for r in rules)},
              "protocol": protocol, "catalog": catalog, "rules": detailed_rules,
              "phaseMatchIds": {role: [ids[i] for i in ix] for role, ix in intervals.items()},
              "historicalUnions": historical_unions, "temporal": {"folds": fold_results, "summaries": summaries, "selection": selection},
              "warnings": protocol["limitations"] + ["热门不胜包括平局和热门输球；本报告只把实际平局计为平局命中。",
                   "关注方由每个场景的计算热度或战绩形象定义，不一定是市场赔率上的第一热门。",
                   "Fisher/BH为双侧差异检验，q较小也可能是平率下降；必须结合增量方向，不能自动解释成提高平局概率。",
                   "全量场景平率属于固定规则的历史描述，不能当成未来单场概率。最小绝对差两项为次要对照，不替换失败主口径。"]}
    write(OUT / "analysis.json", result)
    write(OUT / "fold-rule-stats.json", all_fold_stats)
    write(OUT / "contexts.json", contexts)
    write(OUT / "summary.json", {"counts": result["counts"], "sourceHashes": source_hashes,
          "qualified": {"enriched": sum(q["enriched"] for q in qualified), "incremental": sum(q["incremental"] for q in qualified)},
          "bh": {"train_q_le005": sum(q <= .05 for q in q_train), "full_q_le005": sum(q <= .05 for q in q_full)},
          "historicalUnions": historical_unions, "temporal": {"summaries": summaries, "selection": selection}, "warnings": result["warnings"]})
    rule_matches = {r["id"]: [ids[i] for i in np.flatnonzero(M[j])] for j, r in enumerate(rules)}
    write(OUT / "rule-matches.json", rule_matches)
    with (OUT / "predictions.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        writer = csv.DictWriter(stream, fieldnames=list(prediction_rows[0]))
        writer.writeheader()
        writer.writerows(prediction_rows)
    with (OUT / "scenario-comparison.csv").open("w", encoding="utf-8-sig", newline="") as stream:
        fields = ["id", "label", "tier", "view", "favorite", "favoriteSource", "phase", "games", "homeWins", "draws", "awayWins", "drawRate", "coverage", "favoriteWins", "favoriteLosses", "favoriteNotWinRate", "drawAmongFavoriteNotWins", "parentLift", "eligibleLift", "imageLift", "fisherP", "qTrain", "qFull"]
        writer = csv.DictWriter(stream, fieldnames=fields, extrasaction="ignore")
        writer.writeheader()
        for rule in detailed_rules:
            for phase, stat in rule["stats"].items():
                writer.writerow({**rule, "phase": phase, **stat})
    from draw_conflict_report import render_report
    render_report(result, contexts, rule_matches, OUT / "report.html")
    assert source_hashes == {**{k: digest(p) for k, p in files.items()}, "canonicalFeatureMatrix": canonical}, "source changed during study"
    print("strategy", selection["method"], "BH", sum(q <= .05 for q in q_train), sum(q <= .05 for q in q_full), flush=True)
    print("completed", OUT, flush=True)


if __name__ == "__main__":
    main()
