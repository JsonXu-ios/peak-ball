#!/usr/bin/env python3
"""Independent offline audit of draw-transition inputs, masks and statistics."""
from __future__ import annotations

import argparse
import csv
import hashlib
import importlib.util
import itertools
import json
import math
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
from fractions import Fraction
from pathlib import Path

import numpy as np
from scipy.stats import fisher_exact, hypergeom, norm

EPS = 1e-9
OUTCOMES = ("home", "draw", "away")
PHASES = ("opening", "latest")


def finite(value):
    return type(value) in (int, float) and math.isfinite(value)


def prices(value):
    return list(map(float, value)) if isinstance(value, list) and len(value) == 3 and all(finite(v) and v > 0 for v in value) else None


def sign(value):
    return int(value > EPS) - int(value < -EPS)


def least(prices_):
    if prices_ is None:
        return None
    options = [i for i in range(3) if prices_[i] <= min(prices_) + EPS]
    return options[0] if len(options) == 1 else -1


def pair(values):
    boundary = sorted(map(abs, values))[1]
    return [i for i in range(3) if abs(values[i]) <= boundary + EPS]


def profit_vector(market):
    if not isinstance(market, dict) or market.get("oddsAvailable") is not True:
        return None
    result = []
    for outcome in OUTCOMES:
        entries = [x for x in market.get("bookmakerByOutcome", []) if x.get("outcome") == outcome]
        if len(entries) != 1:
            return None
        detail = entries[0]
        if detail.get("available") is not True or not finite(detail.get("odds")) or detail["odds"] <= 0 or not finite(detail.get("bookmakerProfit")) or not finite(detail.get("totalStake")) or detail["totalStake"] <= 0:
            return None
        result.append(float(Fraction(str(detail["bookmakerProfit"])) * 100 / Fraction(str(detail["totalStake"]))))
    return result


def extract(row):
    result = {}
    for phase in PHASES:
        source = row.get(phase) or {}
        for company in ("william", "average", "sporttery"):
            result[f"{phase}.price.{company}"] = prices(source.get(company))
        w = result[f"{phase}.price.william"]
        for family, company in (("ds", "sporttery"), ("da", "average")):
            other = result[f"{phase}.price.{company}"]
            result[f"{phase}.{family}"] = [w[i] - other[i] for i in range(3)] if w is not None and other is not None else None
    heat = row.get("homeHeat")
    result["heat"] = float(heat) if finite(heat) and 0 <= heat <= 100 else None
    result["hot"] = None if result["heat"] is None else 0 if result["heat"] > 65 else 2 if result["heat"] < 35 else -1
    result["asianOpening"] = float(row["asianOpening"]) if finite(row.get("asianOpening")) else None
    result["asianLatest"] = float(row["asianLine"]) if finite(row.get("asianLine")) else None
    result["favorite"] = least(result["latest.price.average"])
    flags = (row.get("profitInputs") or {}).get("officialProfitRatePresence", [])
    official = [m for m in (row.get("roiSimulation") or {}).get("markets", []) if m.get("key") == "sporttery" and m.get("simulated") is not True]
    rates = [None] * 3
    if len(official) == 1 and official[0].get("oddsAvailable") is True and len(flags) == 3:
        for i, outcome in enumerate(OUTCOMES):
            details = [d for d in official[0].get("bookmakerByOutcome", []) if d.get("outcome") == outcome]
            if len(details) == 1 and flags[i] is True:
                d = details[0]
                if finite(d.get("odds")) and d["odds"] > 0 and finite(d.get("officialProfitRate")):
                    rates[i] = float(d["officialProfitRate"])
    result["profit.official"] = rates
    frontend = row.get("frontend") or {}
    markets = (frontend.get("roiSimulation") or {}).get("markets", [])
    for company in ("sporttery", "william", "bet365", "simulation"):
        if company == "simulation":
            candidates = [m for m in markets if m.get("simulated") is True and m.get("key") not in ("sportteryRqspf", "sportteryRqspfSim")]
        else:
            candidates = [m for m in markets if m.get("key") == company and m.get("simulated") is not True]
        result["profit." + company] = profit_vector(candidates[0]) if len(candidates) == 1 else None
    result["profit.localAverageOdds"] = profit_vector(frontend.get("localMarket"))
    return result


def atom(identity, x):
    """Return unknown/false/true by parsing the published condition ID."""
    tokens = identity.split(".")
    if tokens[0] in PHASES:
        values = x[".".join(tokens[:2])]
        if values is None:
            return None
        code = tokens[2]
        if code == "shape":
            return "".join({-1: "-", 0: "0", 1: "+"}[sign(v)] for v in values) == tokens[3]
        if code == "negatives":
            return sum(sign(v) == -1 for v in values) == int(tokens[3])
        if code == "drawSign":
            return sign(values[1]) == int(tokens[3])
        if code == "zeroPresent":
            return any(sign(v) == 0 for v in values)
        if code == "drawInTwo":
            return 1 in pair(values)
        if code == "drawAbsMax":
            return abs(values[1]) >= max(map(abs, values)) - EPS
        if code == "pair":
            selected = pair(values)
            expected = {"两负含平": [-1, -1], "两正含平": [1, 1], "一负一正含平": [-1, 1]}[tokens[3]]
            return len(selected) == 2 and 1 in selected and sorted(sign(values[i]) for i in selected) == expected
    if tokens[0] == "transition":
        old, new = x["opening." + tokens[1]], x["latest." + tokens[1]]
        if old is None or new is None:
            return None
        code = tokens[2]
        if code == "negativeCount":
            return sign(sum(sign(v) < 0 for v in new) - sum(sign(v) < 0 for v in old)) == int(tokens[3])
        if code in ("平持续负", "平负转正", "平正转负", "平持续正"):
            expected = {"平持续负": (-1, -1), "平负转正": (-1, 1), "平正转负": (1, -1), "平持续正": (1, 1)}[code]
            return (sign(old[1]), sign(new[1])) == expected
        if code in ("平差绝对值收窄", "平差绝对值不变", "平差绝对值扩大"):
            return sign(abs(new[1]) - abs(old[1])) == {"平差绝对值收窄": -1, "平差绝对值不变": 0, "平差绝对值扩大": 1}[code]
        expected = {"平始终在两项": (True, True), "平进入两项": (False, True), "平退出两项": (True, False), "平始终不在两项": (False, False)}[code]
        return (1 in pair(old), 1 in pair(new)) == expected
    if tokens[0] == "heat":
        if tokens[1] == "favorite":
            if x["hot"] not in (0, 2) or x["favorite"] not in (0, 2):
                return None
            return (x["hot"] == x["favorite"]) == (tokens[2] == "同向")
        return None if x["hot"] is None else x["hot"] == {"home": 0, "away": 2, "neutral": -1}[tokens[1]]
    if tokens[0] == "asian":
        old, new = x["asianOpening"], x["asianLatest"]
        if old is None or new is None:
            return None
        if tokens[1] == "cross":
            return sign(old) * sign(new) == -1
        delta = abs(new) - abs(old) if tokens[1] == "depth" else new - old
        return sign(delta) == int(tokens[2])
    if tokens[0] in ("average", "william"):
        old, new = x[f"opening.price.{tokens[0]}"], x[f"latest.price.{tokens[0]}"]
        if old is None or new is None:
            return None
        if tokens[1] == "drawOdds":
            return sign(new[1] - old[1]) == int(tokens[2])
        before, after = least(old), least(new)
        return before in (0, 2) and after in (0, 2) and (before == after) == tokens[2].endswith("保持")
    if tokens[0] == "profit":
        rates = x["profit." + tokens[1]]
        target = tokens[2]
        index = 1 if target == "draw" else x["favorite"] if target == "favorite" else x["hot"]
        if rates is None or index not in ((1,) if target == "draw" else (0, 2)) or not finite(rates[index]):
            return None
        value = rates[index]
        return {"loss28": value < -28, "lossSmall": -28 <= value < 0, "positiveNormal": 0 <= value <= 28, "positive28": value > 28}[tokens[3]]
    raise ValueError("unparsed condition " + identity)


def adjusted_p(ps):
    ordered = sorted(enumerate(ps), key=lambda item: item[1])
    result = np.empty(len(ps))
    bound = 1.
    for rank in range(len(ordered), 0, -1):
        index, value = ordered[rank - 1]
        bound = min(bound, value * len(ordered) / rank)
        result[index] = bound
    return result


def metric_arrays(v, m, y):
    e, n = v.sum(1), m.sum(1)
    d, tp = v[:, y].sum(1), m[:, y].sum(1)
    ps = np.ones(len(e))
    active = (n > 0) & (e > n)
    ps[active] = hypergeom.sf(tp[active] - 1, e[active], d[active], n[active])
    return e, n, d, tp, ps, adjusted_p(ps)


def metric(e, n, d, tp, total, p, q):
    e, n, d, tp = map(int, (e, n, d, tp))
    rate = tp / n if n else None
    baseline = d / e if e else None
    rest = (d - tp) / (e - n) if e > n else None
    interval = None
    if n:
        z = norm.ppf(.975)
        center = (rate + z * z / (2 * n)) / (1 + z * z / n)
        half = z * math.sqrt(rate * (1 - rate) / n + z * z / (4 * n * n)) / (1 + z * z / n)
        interval = [max(0, center - half), min(1, center + half)]
    return {"eligible": e, "eligibleDraws": d, "n": n, "draws": tp, "falseSelections": n - tp, "precision": rate,
            "drawCoverage": tp / total if total else None, "eligibleDrawCoverage": tp / d if d else None, "baseline": baseline,
            "complementPrecision": rest, "liftPP": (rate - baseline) * 100 if n and e else None,
            "complementLiftPP": (rate - rest) * 100 if n and rest is not None else None,
            "wilson95": interval, "p": float(p), "q": float(q)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out", type=Path, default=Path("reports/draw-transition-study"))
    parser.add_argument("--dataset", type=Path, default=Path("reports/draw-full-feature-study/dataset.json"))
    args = parser.parse_args()
    read = lambda filename: json.loads((args.out / filename).read_text(encoding="utf-8-sig"))
    raw = args.dataset.read_bytes()
    rows = json.loads(raw)["rows"]
    catalog, reported, serialized = read("catalog.json"), read("analysis.json"), read("inputs.json")
    issues, warnings, checks = [], [], Counter()

    def compare(actual, expected, path, atol=1e-10):
        checks["fields"] += 1
        if isinstance(expected, dict):
            if not isinstance(actual, dict):
                issues.append(path + ": expected object")
                return
            for key, value in expected.items():
                if key not in actual:
                    issues.append(path + ": missing " + key)
                else:
                    compare(actual[key], value, path + "." + key, atol)
        elif isinstance(expected, (list, tuple, np.ndarray)):
            if not isinstance(actual, (list, tuple, np.ndarray)) or len(actual) != len(expected):
                issues.append(path + ": list shape mismatch")
                return
            for i, value in enumerate(expected):
                compare(actual[i], value, path + f"[{i}]", atol)
        elif finite(expected):
            if not finite(actual) or not math.isclose(actual, expected, rel_tol=1e-9, abs_tol=atol):
                issues.append(f"{path}: {actual!r} != {expected!r}")
        elif actual != expected:
            issues.append(f"{path}: {actual!r} != {expected!r}")

    compare(reported["datasetSha256"], hashlib.sha256(raw).hexdigest(), "datasetSHA")
    xs = [extract(row) for row in rows]
    for row, values in zip(rows, xs):
        changed = dict(row, result=2, homeScore=99, awayScore=98, status=-999)
        compare(extract(changed), values, "targetMutation." + row["matchId"])
    checks["targetLabelMutations"] = len(xs)
    beijing = timezone(timedelta(hours=8))
    dates = np.array([datetime.fromisoformat(row["matchTime"].replace("Z", "+00:00")).astimezone(beijing).date().isoformat() for row in rows])
    for i, (row, x) in enumerate(zip(rows, xs)):
        compare(serialized[i], {"matchId": row["matchId"], "jingcaiId": row.get("jingcaiId"), "result": row["result"], "kickoffDate": dates[i], "inputs": x}, f"input[{i}]")
    checks["inputRows"] = len(xs)
    atoms = catalog["roots"] + catalog["modifiers"]
    masks, validity = {}, {}
    for condition in atoms:
        values = [atom(condition["id"], x) for x in xs]
        masks[condition["id"]] = np.array([v is True for v in values])
        validity[condition["id"]] = np.array([v is not None for v in values])
    checks["atomRowMasks"] = len(atoms) * len(xs)
    root_ids, modifier_ids = [c["id"] for c in catalog["roots"]], [c["id"] for c in catalog["modifiers"]]
    atom_by_id = {c["id"]: c for c in atoms}
    generated, aliases, signatures = [], {}, {}
    possible = [(identity,) for identity in root_ids + modifier_ids]
    possible.extend((a, b) for a, b in itertools.combinations(root_ids, 2) if atom_by_id[a]["group"] != atom_by_id[b]["group"])
    possible.extend(itertools.product(root_ids, modifier_ids))
    movements = [identity for identity in modifier_ids if not atom_by_id[identity]["group"].startswith("profit.")]
    profits = [identity for identity in modifier_ids if atom_by_id[identity]["group"].startswith("profit.")]
    possible.extend(itertools.product(movements, profits))
    for parts in possible:
        valid = np.array([True] * len(xs))
        mask = valid.copy()
        for part in parts:
            valid &= validity[part]
            mask &= masks[part]
        if len(parts) > 1 and mask.sum() < 30:
            continue
        identity = " & ".join(parts)
        signature = (np.packbits(valid).tobytes(), np.packbits(mask).tobytes())
        if signature in signatures:
            aliases.setdefault(signatures[signature], []).append(identity)
            continue
        signatures[signature] = identity
        generated.append({"id": identity, "parts": list(parts), "kind": "single" if len(parts) == 1 else "intersection", "label": " ＋ ".join(atom_by_id[p]["label"] for p in parts)})
    compare(catalog["candidates"], generated, "candidateCatalog")
    compare(catalog["aliases"], aliases, "candidateAliases")
    compare([len(root_ids), len(modifier_ids), len(generated)], [112, 94, reported["candidateCount"]], "fixedCatalogCounts")
    v = np.stack([np.logical_and.reduce([validity[p] for p in c["parts"]]) for c in generated])
    m = np.stack([np.logical_and.reduce([masks[p] for p in c["parts"]]) for c in generated])
    compare(bool(np.any(m & ~v)), False, "candidateSelectedWithinValidity")
    y = np.array([row["result"] == 1 for row in rows])
    by_date = Counter(dates)
    ordered = sorted(by_date)
    cuts = []
    for numerator in (1, 2):
        so_far = 0
        for date in ordered:
            so_far += by_date[date]
            if so_far * 3 >= len(xs) * numerator:
                cuts.append(date)
                break
    groups = np.array([0 if date <= cuts[0] else 1 if date <= cuts[1] else 2 for date in dates])
    compare(reported["timeCuts"], cuts, "timeCuts")
    marked = np.array([bool(str(row.get("jingcaiId") or "").strip()) for row in rows])
    cohort_masks = {"all": np.ones(len(xs), bool), "sporttery": marked, "nonSporttery": ~marked}
    metrics_by_cohort, summaries = {}, {}
    for name, cohort in cohort_masks.items():
        summary = reported["cohorts"][name]
        minimum = 30 if name == "sporttery" else 60
        period = [{"period": g + 1, "matches": int(sum(cohort & (groups == g))), "draws": int(sum(y & cohort & (groups == g))), "firstDate": str(min(dates[cohort & (groups == g)])), "lastDate": str(max(dates[cohort & (groups == g)]))} for g in range(3)]
        compare(summary, {"matches": int(cohort.sum()), "draws": int((cohort & y).sum()), "drawRate": float(y[cohort].mean()), "periods": period, "minimumSupport": minimum}, name + ".cohort")
        slices = {"all": cohort, "period1": cohort & (groups == 0), "period2": cohort & (groups == 1), "period3": cohort & (groups == 2), "development": cohort & (groups < 2)}
        arrays = {split: metric_arrays(v[:, selected], m[:, selected], y[selected]) for split, selected in slices.items()}
        metrics_by_cohort[name] = (arrays, slices)
        dev = arrays["development"]
        qualified = [i for i in range(len(generated)) if dev[1][i] >= minimum and dev[0][i] - dev[1][i] >= minimum]
        def dev_key(i):
            e, n, d, tp, p, q = (column[i] for column in dev)
            lift = (tp / n - (d - tp) / (e - n)) * 100
            return q, -lift, -int(n), generated[i]["id"]
        qualified.sort(key=dev_key)
        bh_pass = [i for i in qualified if dev_key(i)[0] <= .05 and dev_key(i)[1] < 0]
        top_ids = [generated[i]["id"] for i in qualified[:20]]
        compare([r["id"] for r in summary["topDevelopment"]], top_ids, name + ".topDevelopmentIDs")
        single_ids = [generated[i]["id"] for i in qualified if generated[i]["kind"] == "single"][:12]
        compare([r["id"] for r in summary["topSinglesDevelopment"]], single_ids, name + ".singleIDs")
        compare(summary["devEligibleRules"], len(qualified), name + ".devEligibleRules")
        compare(summary["devBHSupportedRules"], len(bh_pass), name + ".devBHRules")
        full = arrays["all"]
        full_ids = [i for i in range(len(generated)) if full[1][i] >= minimum and full[0][i] > full[1][i]]
        full_ids.sort(key=lambda i: (-full[3][i] / full[1][i], -int(full[3][i]), generated[i]["id"]))
        compare([r["id"] for r in summary["fullHistoryHighestPrecisionDescriptive"]], [generated[i]["id"] for i in full_ids[:20]], name + ".descriptiveIDs")
        index_by_id = {candidate["id"]: i for i, candidate in enumerate(generated)}
        for collection in ("topDevelopment", "topSinglesDevelopment", "fullHistoryHighestPrecisionDescriptive"):
            for record in summary[collection]:
                i = index_by_id[record["id"]]
                for split, columns in arrays.items():
                    values = [column[i] for column in columns]
                    compare(record["metrics"][split], metric(*values[:4], int(y[slices[split]].sum()), *values[4:]), name + "." + collection + "." + record["id"] + "." + split)
                for parent in record["parents"]:
                    parent_mask = masks[parent["id"]] & v[i] & cohort
                    pn, pt = int(parent_mask.sum()), int((parent_mask & y).sum())
                    child_n, child_tp = int(full[1][i]), int(full[3][i])
                    compare(parent, {"sameInputsMatches": pn, "sameInputsDraws": pt, "sameInputsPrecision": pt / pn if pn else None,
                                     "childIncrementPP": (child_tp / child_n - pt / pn) * 100 if child_n and pn else None,
                                     "removedMatches": pn - child_n, "removedDraws": pt - child_tp,
                                     "removedFalseSelections": pn - pt - child_n + child_tp}, name + ".parent." + parent["id"])
                if len(record["parts"]) == 2:
                    a, b = (masks[p] for p in record["parts"])
                    cells = {}
                    for split in ("all", "development", "period3"):
                        complete = v[i] & slices[split]
                        values = {}
                        for key, cell in (("both", a & b), ("aOnly", a & ~b), ("bOnly", ~a & b), ("neither", ~a & ~b)):
                            selected = complete & cell
                            n_cell, d_cell = int(selected.sum()), int((selected & y).sum())
                            values[key] = {"n": n_cell, "draws": d_cell, "precision": d_cell / n_cell if n_cell else None}
                        rates = [values[key]["precision"] for key in ("both", "aOnly", "bOnly", "neither")]
                        values["additiveInteractionPP"] = (rates[0] - rates[1] - rates[2] + rates[3]) * 100 if all(rate is not None for rate in rates) else None
                        compare(sum(values[key]["n"] for key in ("both", "aOnly", "bOnly", "neither")), int(complete.sum()), name + ".fourCellPartition")
                        cells[split] = values
                    compare(record["fourCells"], cells, name + ".fourCells." + record["id"])
                    checks["fourCellTables"] += len(cells)
        # Recompute the actual declared permutation statistic over all whole
        # cohort support-qualified masks, separately from development ranking.
        local_v, local_m, local_y, local_groups = v[:, cohort], m[:, cohort], y[cohort], groups[cohort]
        e, n = local_v.sum(1), local_m.sum(1)
        keep = (n >= minimum) & (e - n >= minimum)
        repetitions = summary["shuffle"]["repetitions"]
        rng = np.random.default_rng(20261027)
        permutations = np.tile(local_y[:, None], (1, repetitions + 1)).astype(float)
        for repetition in range(1, repetitions + 1):
            for g in sorted(set(local_groups)):
                selected = np.flatnonzero(local_groups == g)
                permutations[selected, repetition] = rng.permutation(local_y[selected])
        d = local_v[keep].astype(float) @ permutations
        tp = local_m[keep].astype(float) @ permutations
        e, n = e[keep, None], n[keep, None]
        probability = d / e
        variance = n * probability * (1 - probability) * (e - n) / np.maximum(1, e - 1)
        z = np.divide(tp - n * probability, np.sqrt(np.maximum(variance, 0)), out=np.zeros_like(tp), where=variance > 0)
        maxima = z.max(0)
        random = maxima[1:]
        shuffle = {"rules": int(keep.sum()), "repetitions": repetitions, "observedMaxZ": float(maxima[0]), "randomMaxZ": random.tolist(), "randomMaxZMedian": float(np.median(random)), "randomMaxZ95": float(np.quantile(random, .95)), "familywiseEmpiricalP": float((1 + sum(random >= maxima[0])) / (repetitions + 1))}
        compare(summary["shuffle"], shuffle, name + ".shuffle", 2e-5)
        summaries[name] = {"matches": int(cohort.sum()), "draws": int((cohort & y).sum()), "qualifiedRules": len(qualified), "developmentBHSupported": len(bh_pass), "firstDevelopmentRule": top_ids[0] if top_ids else None, "shuffleObserved": shuffle["observedMaxZ"], "shuffleFamilywiseP": shuffle["familywiseEmpiricalP"]}
        checks["permutationMaxima"] += repetitions + 1
        checks["qValues"] += len(generated) * len(slices)
        # Independent 2x2 Fisher tails confirm the vectorized enrichment p.
        for split, columns in arrays.items():
            for i in sorted(set(qualified[:20] + list(range(0, len(generated), 101)))):
                e0, n0, d0, tp0 = [int(columns[j][i]) for j in range(4)]
                p0 = float(columns[4][i])
                expected_p = fisher_exact([[tp0, n0 - tp0], [d0 - tp0, e0 - n0 - d0 + tp0]], alternative="greater").pvalue if n0 > 0 and e0 > n0 else 1.
                compare(p0, float(expected_p), name + ".fisher." + split + f"[{i}]")
                checks["fisherExactContrasts"] += 1
        print(json.dumps({"stage": "cohort_verified", "cohort": name, "issues": len(issues), "rules": len(generated)}, ensure_ascii=False), flush=True)
    with (args.out / "rules.csv").open(encoding="utf-8-sig", newline="") as stream:
        row_count = 0
        id_index = {candidate["id"]: i for i, candidate in enumerate(generated)}
        for record in csv.DictReader(stream):
            arrays, slices = metrics_by_cohort[record["cohort"]]
            index = id_index[record["rule"]]
            columns = arrays[record["split"]]
            values = [column[index] for column in columns]
            expected = metric(*values[:4], int(y[slices[record["split"]]].sum()), *values[4:])
            for key, value in expected.items():
                if key == "wilson95":
                    continue
                actual = None if record[key] == "" else float(record[key])
                compare(actual, value, "csv." + str(row_count) + "." + key)
            row_count += 1
        compare(row_count, len(generated) * 3 * 5, "CSVRows")
        checks["ruleSplitCSVRows"] = row_count
    cells = []
    for name, selected in cohort_masks.items():
        for source in ("ds", "da"):
            bins = defaultdict(list)
            for i in np.flatnonzero(selected):
                old, new = xs[i]["opening." + source], xs[i]["latest." + source]
                if old is None or new is None:
                    continue
                shape = lambda v: "".join({-1: "-", 0: "0", 1: "+"}[sign(t)] for t in v)
                bins[(shape(old), shape(new))].append(i)
            for (before, after), positions in sorted(bins.items()):
                hits = int(y[positions].sum())
                cells.append({"cohort": name, "source": source, "opening": before, "latest": after, "matches": len(positions), "draws": hits, "precision": hits / len(positions)})
    compare(read("sign-transitions.json"), cells, "signTransitionCells")
    checks["transitionCells"] = len(cells)
    # Product code is loaded only after independent full-data reconstruction;
    # these tests exercise finite synthetic input/float-boundary contracts.
    spec = importlib.util.spec_from_file_location("transition_subject", "scripts/draw-transition-study.py")
    subject = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(subject)
    synthetic = 0
    for amount in (-14000000.01, -14000000, -13999999.99, -.01, 0, .01, 13999999.99, 14000000, 14000000.01):
        market = {"oddsAvailable": True, "bookmakerByOutcome": [{"outcome": outcome, "odds": 2, "available": True, "bookmakerProfit": amount, "totalStake": 50000000} for outcome in OUTCOMES]}
        exact = float(Fraction(str(amount)) * 100 / 50000000)
        compare(subject.modeled_rates(market), [exact] * 3, "synthetic.ratio." + str(amount))
        extracted = {p + ".price." + company: [2, 3, 4] for p in PHASES for company in ("william", "average", "sporttery")}
        extracted.update(heat=65, hot=-1, asianOpening=0, asianLatest=0, favorite=0)
        for source in ("official", "sporttery", "william", "bet365", "simulation", "localAverageOdds"):
            extracted["profit." + source] = [exact] * 3
        expected_bucket = "loss28" if exact < -28 else "lossSmall" if exact < 0 else "positiveNormal" if exact <= 28 else "positive28"
        for bucket in ("loss28", "lossSmall", "positiveNormal", "positive28"):
            compare(atom("profit.william.draw." + bucket, extracted), bucket == expected_bucket, "synthetic.bucket." + str(amount) + "." + bucket)
        if amount in (-14000000, 14000000):
            compare(exact, -28 if amount < 0 else 28, "synthetic.exactBoundary")
        synthetic += 1
    seed_row = {"profitInputs": {"officialProfitRatePresence": [True, True, True]}, "roiSimulation": {"markets": [{"key": "sporttery", "simulated": False, "oddsAvailable": True, "bookmakerByOutcome": [{"outcome": outcome, "odds": 2, "available": False, "officialProfitRate": rate} for outcome, rate in zip(OUTCOMES, (-120, 0, 120))]}]}}
    compare(subject.official_rates(seed_row), [-120., 0., 120.], "synthetic.officialDoesNotNeedAmountAndHasNoPercentCap")
    for i in range(3):
        seed_row["profitInputs"]["officialProfitRatePresence"][i] = False
        compare(subject.official_rates(seed_row)[i], None, "synthetic.defaultZeroPresence")
        seed_row["profitInputs"]["officialProfitRatePresence"][i] = True
        synthetic += 1
    checks["syntheticBoundaryCases"] = synthetic + 1
    paths = [args.dataset, Path(__file__), Path("scripts/draw-transition-study.py")] + [args.out / name for name in ("analysis.json", "catalog.json", "inputs.json", "sign-transitions.json", "rules.csv", "protocol.json")]
    result = {"status": "passed" if not issues else "failed", "independence": "No study import: separate source extraction, condition-ID interpreter, candidate enumeration/deduplication, chronology, metrics, BH, dev-only ranking and permutation max-search reconstruction.", "comparisons": dict(checks), "cohorts": summaries, "sourceHashes": {str(path).replace("\\", "/"): hashlib.sha256(path.read_bytes()).hexdigest() for path in paths}, "warnings": warnings, "issues": issues}
    (args.out / "review.json").write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False), encoding="utf-8")
    print(json.dumps({"status": result["status"], "comparisons": result["comparisons"], "cohorts": summaries, "issueCount": len(issues), "issues": issues[:15]}, ensure_ascii=False))
    raise SystemExit(bool(issues))


if __name__ == "__main__":
    main()
