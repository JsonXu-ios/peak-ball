#!/usr/bin/env python3
"""Descriptive, offline historical draw associations; no forecast or model fit.

Each rule uses one measured field plus minimum-history-denominator guards. All
outcomes are used for historical discovery, including threshold choice and
ranking. Chronological thirds are stability descriptions, not held-out tests.
"""
from __future__ import annotations

import csv
import hashlib
import json
import math
import re
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

sys.dont_write_bytecode = True
import numpy as np
from scipy.stats import fisher_exact

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "reports/draw-full-feature-study"
EXTRA = ROOT / "reports/draw-conflict-study"
OUT = ROOT / "reports/draw-historical-value-study"
MIN_ARM = 80
PREFERRED_N = 150
EPS = 1e-10


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def write(path, obj):
    temporary = path.with_name(path.name + ".tmp")
    temporary.write_text(json.dumps(obj, ensure_ascii=False, allow_nan=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def wilson(k, n):
    if not n:
        return [None, None]
    z = 1.959963984540054
    p, den = k / n, 1 + z * z / n
    mid = (p + z * z / (2 * n)) / den
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / den
    return [max(0., mid - half), min(1., mid + half)]


def bh(pvalues):
    p = np.asarray(pvalues, float)
    order = np.argsort(p, kind="stable")
    result = np.empty(len(p))
    result[order] = np.minimum(1., np.minimum.accumulate((p[order] * len(p) / np.arange(1, len(p) + 1))[::-1])[::-1])
    return result


def guards_for(meta, names):
    """Avoid presenting one or two prior matches as robust recent form."""
    name = meta["name"]
    # Context provenance uses explanatory strings. They are not executable
    # predicates; the adapter has already made failed guards missing (NaN).
    guards = [g for g in meta.get("guards", []) if isinstance(g, dict) and {"feature", "op", "value"} <= set(g)]
    m = re.match(r"safeRecent\.(home|away)\.(5|10)\.", name)
    if m:
        guards.append({"feature": f"safeRecent.{m[1]}.{m[2]}.n", "op": "ge", "value": 5})
    if name.startswith("safeRecent.pair."):
        guards.extend({"feature": f"safeRecent.{side}.10.n", "op": "ge", "value": 5} for side in ("home", "away"))
    m = re.match(r"safeH2H\.(10|all)\.", name)
    if m:
        guards.append({"feature": f"safeH2H.{m[1]}.n", "op": "ge", "value": 3})
    if name.startswith("conflict.recent."):
        side = name.split(".")[2]
        guards.append({"feature": f"safeRecent.{side}.10.n", "op": "ge", "value": 5})
    # The adapter's exact pair only applies to non-tied largest/smallest sets.
    if name.startswith("conflict.difference.") and not name.endswith(".ambiguous"):
        guards.append({"feature": name.rsplit(".", 1)[0] + ".ambiguous", "op": "eq", "value": 0})
    unique = {(g["feature"], g["op"], g["value"]): g for g in guards}
    return [unique[k] for k in sorted(unique) if k[0] in names]


def excluded(meta):
    name = meta["name"]
    if meta.get("denominatorOnly") or name.endswith(".n"):
        return "sample_denominator_only"
    if name.startswith("availability.") or meta.get("family") == "inputAvailability":
        return "availability_not_a_sporting_feature"
    if name in {"snapshot.goalsInput.recentHomeMatches", "snapshot.goalsInput.recentGuestMatches"}:
        return "sample_denominator_only"
    if name.endswith(".ambiguous"):
        return "pair_tie_quality_flag_not_a_sporting_feature"
    return None


def fixed_thresholds(meta, transform):
    name, unit = meta["name"], meta["unit"]
    if transform == "abs":
        return [0., .025, .05, .1, .15, .2, .25, .3, .4, .5, .75, 1., 1.5, 2.]
    if unit == "indicator":
        return [0., 1.]
    if unit == "count":
        return [0., 1., 2., 3., 4., 5., 6., 8., 10.]
    if unit == "rank":
        return [1., 2., 3., 4., 5., 6., 8., 10., 12., 16., 20.]
    if unit == "days":
        return [1., 2., 3., 4., 5., 6., 7., 8., 10., 14., 21., 28.]
    if unit == "pointsPerMatch":
        return [0., .5, .75, 1., 1.25, 1.5, 1.75, 2., 2.25, 2.5, 2.75, 3., 3.5, 4., 4.5, 5.]
    if unit == "goalsPerMatch":
        return [0., .25, .5, .75, 1., 1.25, 1.5, 1.75, 2., 2.25, 2.5, 2.75, 3., 3.25, 3.5, 4., 4.5, 5.]
    if unit == "decimal_odds":
        return [1.5, 1.75, 2., 2.25, 2.5, 2.75, 3., 3.25, 3.5, 3.75, 4., 4.5, 5., 6., 8., 10.]
    if unit == "probability":
        return [0., .05, .1, .15, .2, .225, .25, .275, .3, .325, .35, .4, .45, .5, .6, .7, .8, .9, 1.]
    if unit == "percent" or name.startswith(("profit.", "marketProfit.", "heat.", "retail.", "trade.")):
        return [-50., -30., -20., -10., -5., 0., 5., 10., 20., 25., 30., 35., 40., 50., 60., 65., 70., 75., 80., 90.]
    if unit in ("odds_difference", "index_difference") or ".change." in name:
        return [-1., -.75, -.5, -.3, -.25, -.2, -.15, -.1, -.05, -.025, 0., .025, .05, .1, .15, .2, .25, .3, .5, .75, 1.]
    if unit == "water":
        return [-.3, -.2, -.1, 0., .1, .2, .3, .7, .8, .85, .9, .95, 1., 1.05, 1.1, 1.2]
    if unit == "goals":
        return [-3., -2., -1.5, -1., -.75, -.5, -.25, 0., .25, .5, .75, 1., 1.25, 1.5, 1.75, 2., 2.25, 2.5, 2.75, 3., 3.25, 3.5, 4., 4.5, 5., 6., 7., 8.]
    if unit == "ratio":
        return [0., .1, .2, .25, .5, .75, 1., 1.25, 1.5, 2., 3.]
    return [-.5, -.25, -.1, 0., .025, .05, .1, .15, .2, .25, .5, .75, .8, .85, .9, .925, .95, .975, 1., 1.025, 1.05, 1.1, 1.25, 1.5, 2., 3., 4., 5., 10., 20., 40., 60.]


def transformations(meta, values):
    yield "identity", values
    name, unit = meta["name"], meta["unit"]
    if (unit in ("odds_difference", "index_difference") or ".change." in name
            or ("asianHandicap" in name and name.endswith(".line"))
            or name.endswith(".goalDifference") or name.endswith(".waterSpread")):
        yield "abs", np.abs(values)


def compare(values, op, value):
    if op == "ge":
        return values >= value
    if op == "le":
        return values <= value
    if op == "eq":
        return values == value
    raise ValueError(op)


def outcome(mask, labels):
    n = int(mask.sum())
    counts = [int((mask & (labels == k)).sum()) for k in (0, 1, 2)]
    return {"games": n, "homeWins": counts[0], "draws": counts[1], "awayWins": counts[2],
            "drawRate": counts[1] / n if n else None, "drawWilson95": wilson(counts[1], n)}


def stats(mask, eligible, labels, period_masks, all_draws):
    selected, complement, same = (outcome(m, labels) for m in (mask, eligible & ~mask, eligible))
    a, b = selected["draws"], selected["games"] - selected["draws"]
    c, d = complement["draws"], complement["games"] - complement["draws"]
    fisher = fisher_exact([[a, b], [c, d]], alternative="two-sided")
    ratio = float(fisher.statistic)
    periods = []
    for pm in period_masks:
        s, e, other = (outcome(m & pm, labels) for m in (mask, eligible, eligible & ~mask))
        lift = s["drawRate"] - e["drawRate"] if s["games"] and e["games"] else None
        periods.append({"selected": s, "eligible": e, "complement": other, "lift": lift,
                        "supported": s["games"] >= 20 and other["games"] >= 20})
    positive = [p["lift"] is not None and p["lift"] > EPS for p in periods]
    return {**selected, "falseSelections": b, "eligible": same, "complement": complement,
            "drawCoverage": a / all_draws, "eligibleDrawCoverage": a / same["draws"] if same["draws"] else None,
            "lift": selected["drawRate"] - same["drawRate"],
            "versusComplementLift": selected["drawRate"] - complement["drawRate"],
            "relativeLift": selected["drawRate"] / same["drawRate"] if same["drawRate"] else None,
            "oddsRatio": ratio if math.isfinite(ratio) else None,
            "oddsRatioInfinite": math.isinf(ratio),
            "oddsRatioHaldane": ((a + .5) * (d + .5)) / ((b + .5) * (c + .5)),
            "fisherP": float(fisher.pvalue), "periods": periods,
            "threePeriodPositive": all(positive),
            "threePeriodSupportedPositive": all(positive) and all(p["supported"] for p in periods),
            "minPeriodLift": min((p["lift"] for p in periods if p["lift"] is not None), default=None),
            "wilsonLowerExcess": selected["drawWilson95"][0] - same["drawRate"]}


def rule_key(rule):
    s = rule["stats"]
    # Historical evidence tiers precede precision: prevents tiny lucky groups
    # from crowding out supported, three-period-consistent fields.
    tier = (s["games"] >= PREFERRED_N and s["lift"] > 0
            and s["threePeriodSupportedPositive"] and s["fisherQ"] <= .05)
    stable = s["games"] >= PREFERRED_N and s["threePeriodSupportedPositive"] and s["lift"] > 0
    supported = s["games"] >= PREFERRED_N and s["lift"] > 0
    positive = s["lift"] > 0
    return (int(tier), int(stable), int(supported), int(positive),
            s["wilsonLowerExcess"], s["lift"], s["drawCoverage"], s["games"], rule["id"])


def csv_rows(path, rules):
    fields = ["id", "feature", "label", "family", "view", "unit", "source", "transform", "op", "value", "thresholdOrigin", "definition", "guards",
              "games", "homeWins", "draws", "awayWins", "falseSelections", "drawRate", "drawCoverage", "eligibleGames", "eligibleDraws", "eligibleDrawRate", "eligibleDrawCoverage",
              "complementGames", "complementDraws", "complementDrawRate", "lift", "versusComplementLift", "relativeLift", "oddsRatio", "oddsRatioHaldane", "fisherP", "fisherQ",
              "threePeriodPositive", "threePeriodSupportedPositive", "earlyGames", "earlyDraws", "earlyDrawRate", "earlyEligibleDrawRate", "earlyLift", "middleGames", "middleDraws", "middleDrawRate", "middleEligibleDrawRate", "middleLift", "lateGames", "lateDraws", "lateDrawRate", "lateEligibleDrawRate", "lateLift"]
    with path.open("w", encoding="utf-8-sig", newline="") as f:
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        for rule in rules:
            s = rule["stats"]
            row = {k: rule.get(k) for k in fields if k in rule}
            row["guards"] = json.dumps(rule["guards"], ensure_ascii=False, separators=(",", ":"))
            row.update({k: s[k] for k in fields if k in s})
            for outer, prefix in (("eligible", "eligible"), ("complement", "complement")):
                for k in ("games", "draws", "drawRate"):
                    row[prefix + k[0].upper() + k[1:]] = s[outer][k]
            for name, p in zip(("early", "middle", "late"), s["periods"]):
                row.update({name + "Games": p["selected"]["games"], name + "Draws": p["selected"]["draws"], name + "DrawRate": p["selected"]["drawRate"], name + "EligibleDrawRate": p["eligible"]["drawRate"], name + "Lift": p["lift"]})
            w.writerow(row)


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    files = {"matrix": SOURCE / "features.csv", "manifest": SOURCE / "feature-manifest.json", "dataset": SOURCE / "dataset.json",
             "extraMatrix": EXTRA / "extra-features.npz", "extraManifest": EXTRA / "extra-feature-manifest.json", "script": Path(__file__)}
    if (OUT / "context-features.npz").exists() and (OUT / "context-feature-manifest.json").exists():
        files.update(contextMatrix=OUT / "context-features.npz", contextManifest=OUT / "context-feature-manifest.json")
    hashes = {k: sha(p) for k, p in files.items()}
    with files["matrix"].open(encoding="utf-8-sig", newline="") as f:
        r = csv.reader(f)
        header, rows = next(r), list(r)
    assert header[:2] == ["matchId", "resultLabel"]
    ids = [r[0] for r in rows]
    labels = np.array([int(r[1]) for r in rows])
    X = np.array([[float(v) if v else np.nan for v in r[2:]] for r in rows])
    metadata = read(files["manifest"])
    assert header[2:] == [m["name"] for m in metadata]
    hashes["canonicalFeatureMatrix"] = hashlib.sha256(np.nan_to_num(X, nan=-9.87654321e99).astype("<f8").tobytes()).hexdigest()
    assert hashes["canonicalFeatureMatrix"] == read(SOURCE / "analysis.json")["featureMatrixSHA256"]
    extra = np.load(files["extraMatrix"])
    extra_meta = read(files["extraManifest"])
    assert extra["matchIds"].tolist() == ids and extra["values"].shape == (len(ids), len(extra_meta))
    X = np.concatenate([X, extra["values"]], axis=1)
    metadata += extra_meta
    if "contextMatrix" in files:
        context_matrix, context_meta = np.load(files["contextMatrix"]), read(files["contextManifest"])
        context_values = context_matrix["values"] if "values" in context_matrix.files else context_matrix["X"]
        assert context_matrix["matchIds"].tolist() == ids and context_values.shape == (len(ids), len(context_meta))
        X = np.concatenate([X, context_values], axis=1)
        metadata += context_meta
    names = {m["name"]: i for i, m in enumerate(metadata)}
    assert len(names) == X.shape[1] and X.shape[1] >= 512 and len(ids) == 4161 and int((labels == 1).sum()) == 970
    by_id = {str(r["matchId"]): r for r in read(files["dataset"])["rows"]}
    contexts = [by_id[i] for i in ids]
    beijing = timezone(timedelta(hours=8))
    def bjt_day(r):
        value = datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00"))
        return value.replace(tzinfo=beijing).date().isoformat() if value.tzinfo is None else value.astimezone(beijing).date().isoformat()
    dates = np.array([bjt_day(r) for r in contexts])
    assert all(int(r["result"]) == labels[i] for i, r in enumerate(contexts))
    sorted_dates = np.sort(dates)
    boundaries = [str(sorted_dates[len(ids) // 3]), str(sorted_dates[2 * len(ids) // 3])]
    period_masks = [dates < boundaries[0], (dates >= boundaries[0]) & (dates < boundaries[1]), dates >= boundaries[1]]
    periods = [{"name": name, "minDate": str(min(dates[m])), "maxDate": str(max(dates[m])), **outcome(m, labels)} for name, m in zip(("early", "middle", "late"), period_masks)]
    total_draws = int((labels == 1).sum())
    candidates, masks, excluded_fields, field_counts = [], {}, [], []
    for j, meta in enumerate(metadata):
        reason = excluded(meta)
        if reason:
            excluded_fields.append({"feature": meta["name"], "reason": reason})
            continue
        v = X[:, j]
        guards = guards_for(meta, names)
        eligible = np.isfinite(v)
        for g in guards:
            gv = X[:, names[g["feature"]]]
            eligible &= np.isfinite(gv) & compare(gv, g["op"], g["value"])
        n = int(eligible.sum())
        if n < 2 * MIN_ARM or len(np.unique(v[eligible])) < 2:
            excluded_fields.append({"feature": meta["name"], "reason": "insufficient_eligible_or_constant", "eligibleGames": n})
            continue
        seen, n_field = set(), 0
        for transform, tv in transformations(meta, v):
            values = tv[eligible]
            options = []
            unique = np.unique(values)
            if len(unique) <= 12:
                options.extend(("eq", float(x), "observed_category") for x in unique)
            if meta["unit"] != "indicator":
                options.extend((op, float(x), "fixed_readable") for x in fixed_thresholds(meta, transform) for op in ("le", "ge"))
                options.extend((op, float(x), "full_history_unlabelled_quantile_" + str(int(q * 100))) for q, x in zip((.2, .4, .6, .8), np.quantile(values, [.2, .4, .6, .8])) for op in ("le", "ge"))
            for op, value, origin in options:
                mask = eligible & compare(tv, op, value)
                selected = int(mask.sum())
                if min(selected, n - selected) < MIN_ARM:
                    continue
                packed = np.packbits(mask).tobytes()
                # Exact thresholds/transform/category masks are one statistical
                # test per field, regardless of the number of aliases.
                if packed in seen:
                    continue
                seen.add(packed)
                definition = f'{"abs(" if transform == "abs" else ""}{meta["name"]}{")" if transform == "abs" else ""} {dict(le="<=", ge=">=", eq="=")[op]} {value:.12g}'
                identity = json.dumps([meta["name"], transform, op, value, guards], separators=(",", ":"))
                ident = "u_" + hashlib.sha256(identity.encode()).hexdigest()[:16]
                rule = {"id": ident, **{k: meta[k] for k in ("name", "label", "family", "view", "unit", "source")}, "feature": meta["name"],
                        "transform": transform, "op": op, "value": value, "thresholdOrigin": origin, "definition": definition, "guards": guards,
                        "sourceLabel": meta["label"], "provenanceGuards": meta.get("guards", []),
                        "allOf": [{"feature": meta["name"], "transform": transform, "op": op, "value": value}] + [{**g, "transform": "identity"} for g in guards],
                        "stats": stats(mask, eligible, labels, period_masks, total_draws)}
                candidates.append(rule)
                masks[ident] = mask
                n_field += 1
        field_counts.append({"feature": meta["name"], "eligibleGames": n, "candidateCount": n_field})
    q = bh([r["stats"]["fisherP"] for r in candidates])
    for r, value in zip(candidates, q):
        r["stats"]["fisherQ"] = float(value)
    by_feature = {}
    for rule in candidates:
        by_feature.setdefault(rule["feature"], []).append(rule)
    best, reduced = [], []
    near_removed = 0
    for feature, values in by_feature.items():
        kept = []
        for rule in sorted(values, key=rule_key, reverse=True):
            mask = masks[rule["id"]]
            # Rank first, then collapse same-field candidates that differ on
            # <=max(3,1% eligible) rows or have selected Jaccard >=.98.
            n = rule["stats"]["eligible"]["games"]
            near = False
            for other in kept:
                om = masks[other["id"]]
                union = int((mask | om).sum())
                if int((mask ^ om).sum()) <= max(3, int(.01 * n)) or (union and int((mask & om).sum()) / union >= .98):
                    near = True
                    break
            if near:
                near_removed += 1
            else:
                kept.append(rule)
        if kept:
            best.append(kept[0])
            reduced.extend(kept)
    best.sort(key=rule_key, reverse=True)
    reduced.sort(key=rule_key, reverse=True)
    for rank, rule in enumerate(best, 1):
        rule["rank"] = rank
    result = {
        "version": "historical-single-field-v1", "generatedAt": datetime.now(timezone.utc).isoformat(), "sourceHashes": hashes,
        "dataset": outcome(np.ones(len(labels), bool), labels), "featureCount": X.shape[1], "candidateCount": len(candidates),
        "fieldCountWithCandidates": len(best), "afterNearDuplicateCount": len(reduced), "nearDuplicateCandidatesRemoved": near_removed,
        "periods": periods, "periodBoundaries": boundaries, "excludedFields": excluded_fields, "fieldCoverage": field_counts,
        "topByFeature": {r["feature"]: r["id"] for r in best},
        "protocol": {"scope": "historical_descriptive_discovery_only", "outcome": "resultLabel==1", "labelColumnsExcluded": header[:2],
                     "minimumSelected": MIN_ARM, "minimumComplement": MIN_ARM, "preferredSelected": PREFERRED_N,
                     "thresholds": "fixed unit-dependent readable values plus full-history feature-only quantiles 20/40/60/80%; categories for <=12 unique values",
                     "missing": "finite target field and all guards; never imputed to 0; selected and complement partition exactly the same eligibility set",
                     "historyGuards": "recent5 require5; recent10 require>=5; h2h require>=3; recent3 require3; pair both recent10>=5; streak known prior recent10>=5",
                     "ranking": "tier supported n>=150 & all3 periods positive with >=20 selected/complement each & BHq<=.05, then same stability without q, then n>=150 positive; within tier Wilson95 lower minus eligible base rate, lift, coverage, n",
                     "multipleTesting": "two-sided Fisher selected vs disjoint eligible-not-selected; Benjamini-Hochberg over ALL exactly deduplicated supported candidates before near-duplicate collapse and field-best selection",
                     "nearDuplicates": "within same field after evidence ranking; symmetric mask difference <=max(3,1%eligible) or Jaccard>=.98; no cross-field collapse",
                     "periods": "three full Beijing matchTime date blocks near equal row thirds (never crawler schedule date), used only descriptively; not independent or untouched tests"},
        "summary": {"supportedStablePositiveQ05Fields": sum(r["stats"]["games"] >= PREFERRED_N and r["stats"]["threePeriodSupportedPositive"] and r["stats"]["fisherQ"] <= .05 for r in best),
                    "supportedStablePositiveFields": sum(r["stats"]["games"] >= PREFERRED_N and r["stats"]["threePeriodSupportedPositive"] for r in best),
                    "allCandidatePositiveQ05": sum(r["stats"]["lift"] > 0 and r["stats"]["fisherQ"] <= .05 for r in candidates)},
        "warnings": ["All labels were used in historical ranking; three-period consistency is descriptive and not independent validation.",
                     "Feature-only full-history quantiles are data-derived thresholds. Fixed thresholds are separately labelled.",
                     "Market/source snapshot timestamps are not proven pre-kickoff. Safe history guards remove target and later games, but snapshotDerived columns have weaker provenance.",
                     "Heat and simulated profits are calculated fields sharing odds/history inputs; not verified funds, bookmaker intent, or independent information.",
                     "BH adjusts this candidate family only, not all previous exploratory analyses. Correlated fields/rules are not independent evidence.",
                     "Coverage is selected actual draws / all970draws; eligible coverage uses only actual draws with this field and guards available."],
        "rankedBestPerFeature": best,
    }
    write(OUT / "univariate.json", result)
    write(OUT / "univariate-candidates.json", candidates)
    csv_rows(OUT / "univariate.csv", best)
    csv_rows(OUT / "univariate-candidates.csv", sorted(candidates, key=rule_key, reverse=True))
    write(OUT / "univariate-reduced-rules.json", reduced)
    print(json.dumps({"candidateCount": len(candidates), "fields": len(best), "summary": result["summary"], "top": [{"label": r["label"], "definition": r["definition"], "n": r["stats"]["games"], "draws": r["stats"]["draws"], "drawRate": r["stats"]["drawRate"], "lift": r["stats"]["lift"], "q": r["stats"]["fisherQ"]} for r in best[:12]]}, ensure_ascii=False), flush=True)


if __name__ == "__main__":
    main()
