#!/usr/bin/env python3
"""Independent recount of historical univariate discovery exports.

Reads frozen numeric matrices and serialized predicates. Does not import the
candidate generator, ranking implementation, or production statistics helper.
Only writes this study's review artifact; no services or databases are used.
"""
from __future__ import annotations

import csv
import hashlib
import json
import math
from datetime import datetime, timedelta, timezone
from pathlib import Path

import numpy as np
from scipy.stats import false_discovery_control, fisher_exact

ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / "reports/draw-full-feature-study"
EXTRA = ROOT / "reports/draw-conflict-study"
OUT = ROOT / "reports/draw-historical-value-study"


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def wilson(k, n):
    if n == 0:
        return [None, None]
    z2 = 3.841458820694124
    den = n + z2
    center = (k + z2 / 2) / den
    half = math.sqrt(z2 * (k * (n - k) / n + z2 / 4)) / den
    return [max(0.0, center - half), min(1.0, center + half)]


def outcomes(mask, labels):
    counts = np.bincount(labels[mask], minlength=3).tolist()
    n = int(mask.sum())
    return {"games": n, "homeWins": counts[0], "draws": counts[1],
            "awayWins": counts[2], "drawRate": counts[1] / n if n else None,
            "drawWilson95": wilson(counts[1], n)}


def compare(v, op, threshold):
    if op == "le":
        return v <= threshold
    if op == "ge":
        return v >= threshold
    if op == "eq":
        return v == threshold
    if op == "lt":
        return v < threshold
    if op == "gt":
        return v > threshold
    raise ValueError(op)


def main():
    summary = read(OUT / "univariate.json")
    candidates = read(OUT / "univariate-candidates.json")
    with (BASE / "features.csv").open(encoding="utf-8-sig", newline="") as f:
        records = list(csv.reader(f))
    header, rows = records[0], records[1:]
    ids = [r[0] for r in rows]
    labels = np.asarray([int(r[1]) for r in rows], dtype=int)
    X = np.asarray([[float(v) if v else np.nan for v in r[2:]] for r in rows])
    names = header[2:]
    metadata = read(BASE / "feature-manifest.json")
    assert names == [m["name"] for m in metadata]
    with np.load(EXTRA / "extra-features.npz") as p:
        assert p["matchIds"].tolist() == ids
        X = np.column_stack((X, p["values"]))
    names += [m["name"] for m in read(EXTRA / "extra-feature-manifest.json")]
    source_paths = {"matrix": BASE / "features.csv", "manifest": BASE / "feature-manifest.json",
                    "dataset": BASE / "dataset.json", "extraMatrix": EXTRA / "extra-features.npz",
                    "extraManifest": EXTRA / "extra-feature-manifest.json",
                    "script": ROOT / "scripts/draw-historical-univariate.py"}
    # Optional context adapter has its own aligned matrix, not recomputed by
    # this test. The adapter is independently audited separately.
    for matrix_name, manifest_name in (("context-features.npz", "context-feature-manifest.json"),
                                       ("context-features.npz", "context-manifest.json")):
        matrix, manifest = OUT / matrix_name, OUT / manifest_name
        if matrix.exists() and manifest.exists() and summary["featureCount"] > X.shape[1]:
            with np.load(matrix) as p:
                assert p["matchIds"].tolist() == ids
                X = np.column_stack((X, p["values"] if "values" in p else p["X"]))
            names += [m["name"] for m in read(manifest)]
            source_paths.update(contextMatrix=matrix, contextManifest=manifest)
            break
    assert len(names) == X.shape[1] == summary["featureCount"]
    lookup = {name: j for j, name in enumerate(names)}
    source = {str(r["matchId"]): r for r in read(BASE / "dataset.json")["rows"]}
    dates = []
    for i, mid in enumerate(ids):
        row = source[mid]
        assert int(row["result"]) == labels[i]
        t = datetime.fromisoformat(row["matchTime"].replace("Z", "+00:00"))
        assert t.tzinfo is not None
        dates.append(t.astimezone(timezone(timedelta(hours=8))).date().isoformat())
    dates = np.asarray(dates)
    boundaries = summary["periodBoundaries"]
    periods = [dates < boundaries[0], (dates >= boundaries[0]) & (dates < boundaries[1]), dates >= boundaries[1]]
    issues = []
    checked = 0

    def check(actual, expected, label):
        nonlocal checked
        checked += 1
        if isinstance(expected, dict):
            for key, value in expected.items():
                if key not in actual:
                    issues.append(label + "." + key + " missing")
                else:
                    check(actual[key], value, label + "." + key)
        elif isinstance(expected, list):
            if not isinstance(actual, list) or len(actual) != len(expected):
                issues.append(label + " list length")
            else:
                for j, value in enumerate(expected):
                    check(actual[j], value, label + f"[{j}]")
        elif isinstance(expected, float):
            if actual is None or not math.isclose(actual, expected, rel_tol=2e-10, abs_tol=2e-12):
                issues.append(f"{label}: {actual!r} != {expected!r}")
        elif actual != expected:
            issues.append(f"{label}: {actual!r} != {expected!r}")

    check(summary["dataset"], outcomes(np.ones(len(labels), bool), labels), "dataset")
    assert len(ids) == len(set(ids)) == 4161 and int((labels == 1).sum()) == 970
    assert len(candidates) == summary["candidateCount"]
    for p, mask in zip(summary["periods"], periods):
        check(p, outcomes(mask, labels), "period." + p["name"])
        check(p["minDate"], str(min(dates[mask])), "period.minDate")
        check(p["maxDate"], str(max(dates[mask])), "period.maxDate")
    pvalues = []
    masks, eligibility, seen = {}, {}, set()
    for r in candidates:
        v = X[:, lookup[r["feature"]]]
        e = np.isfinite(v)
        for g in r["guards"]:
            gv = X[:, lookup[g["feature"]]]
            e &= np.isfinite(gv) & compare(gv, g["op"], g["value"])
        tv = abs(v) if r["transform"] == "abs" else v
        m = e & compare(tv, r["op"], r["value"])
        key = (r["feature"], np.packbits(m).tobytes())
        if key in seen:
            issues.append(r["id"] + " exact duplicate within field")
        seen.add(key)
        masks[r["id"]], eligibility[r["id"]] = m, e
        if min(int(m.sum()), int((e & ~m).sum())) < 80:
            issues.append(r["id"] + " below either 80 arm")
        selected, eligible, complement = (outcomes(mm, labels) for mm in (m, e, e & ~m))
        a, b = selected["draws"], selected["games"] - selected["draws"]
        c, d = complement["draws"], complement["games"] - complement["draws"]
        result = fisher_exact([[a, b], [c, d]], alternative="two-sided")
        pvalues.append(float(result.pvalue))
        odds = float(result.statistic)
        pp = []
        for pm in periods:
            ps, pe, pc = (outcomes(mm & pm, labels) for mm in (m, e, e & ~m))
            lift = ps["drawRate"] - pe["drawRate"] if ps["games"] and pe["games"] else None
            pp.append({"selected": ps, "eligible": pe, "complement": pc, "lift": lift,
                       "supported": ps["games"] >= 20 and pc["games"] >= 20})
        positive = all(p["lift"] is not None and p["lift"] > 1e-10 for p in pp)
        expected = {**selected, "falseSelections": b, "eligible": eligible, "complement": complement,
                    "drawCoverage": a / 970, "eligibleDrawCoverage": a / eligible["draws"] if eligible["draws"] else None,
                    "lift": selected["drawRate"] - eligible["drawRate"],
                    "versusComplementLift": selected["drawRate"] - complement["drawRate"],
                    "relativeLift": selected["drawRate"] / eligible["drawRate"] if eligible["drawRate"] else None,
                    "oddsRatio": odds if math.isfinite(odds) else None, "oddsRatioInfinite": math.isinf(odds),
                    "oddsRatioHaldane": (a + .5) * (d + .5) / ((b + .5) * (c + .5)),
                    "fisherP": float(result.pvalue), "periods": pp, "threePeriodPositive": positive,
                    "threePeriodSupportedPositive": positive and all(p["supported"] for p in pp),
                    "wilsonLowerExcess": selected["drawWilson95"][0] - eligible["drawRate"]}
        check(r["stats"], expected, r["id"])
    qvalues = false_discovery_control(np.asarray(pvalues), method="bh")
    for r, q in zip(candidates, qvalues):
        check(r["stats"]["fisherQ"], float(q), r["id"] + ".fisherQ")
    for key, path in source_paths.items():
        if key in summary["sourceHashes"]:
            check(summary["sourceHashes"][key], digest(path), "sourceHash." + key)
    best = summary["rankedBestPerFeature"]
    by_id = {r["id"]: r for r in candidates}
    for r in best:
        check(r["stats"], by_id[r["id"]]["stats"], "best." + r["id"])
    same_mask_groups = {}
    for r in best:
        same_mask_groups.setdefault(np.packbits(masks[r["id"]]).tobytes(), []).append(r["id"])
    duplicates = [ids for ids in same_mask_groups.values() if len(ids) > 1]
    artifact = {"version": "historical-univariate-independent-review-v1",
                "sourceHashes": {p.name: digest(p) for p in (OUT / "univariate.json", OUT / "univariate-candidates.json", Path(__file__))},
                "games": len(ids), "draws": int((labels == 1).sum()), "featureCount": len(names),
                "candidateCount": len(candidates), "checkedValues": checked,
                "recountedPredicateRows": len(candidates) * len(labels), "fisherTests": len(pvalues),
                "bhValues": len(qvalues), "periods": [outcomes(m, labels) for m in periods],
                "crossFieldExactMaskAliasGroupsAmongBest": duplicates,
                "issues": issues[:100], "issueCount": len(issues),
                "interpretation": "Historical association recount only. No correction for all previous searches; no held-out or prospective claim."}
    (OUT / "univariate-independent-review.json").write_text(json.dumps(artifact, ensure_ascii=False, allow_nan=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"candidateCount": len(candidates), "checkedValues": checked, "issueCount": len(issues), "firstIssues": issues[:10]}, ensure_ascii=False))
    if issues:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
