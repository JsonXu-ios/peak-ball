#!/usr/bin/env python3
"""Independent scalar-mask and historical-value report audit.

No study implementation is imported. This recount intentionally uses the full
historical labels and describes fits; it does not add an independent test set.
"""
from __future__ import annotations

import csv
import hashlib
import json
import math
from collections import Counter
from datetime import datetime, timedelta, timezone
from pathlib import Path

import numpy as np

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-historical-value-study"
BASE = ROOT / "reports/draw-full-feature-study"
EXTRA = ROOT / "reports/draw-conflict-study"


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def interval(k, n):
    if not n:
        return [None, None]
    z2 = 3.841458820694124
    center = (k + z2 / 2) / (n + z2)
    half = math.sqrt(z2 * (k * (n - k) / n + z2 / 4)) / (n + z2)
    return [center - half, center + half]


def main():
    report = read(OUT / "analysis.json")
    catalog = read(OUT / "catalog.json")
    protocol = read(OUT / "protocol.json")
    data = read(BASE / "dataset.json")["rows"]
    with (BASE / "features.csv").open(encoding="utf-8-sig", newline="") as f:
        csv_rows = list(csv.reader(f))
    header, records = csv_rows[0], csv_rows[1:]
    ids = [r[0] for r in records]
    y = np.asarray([int(r[1]) for r in records])
    X = np.asarray([[float(v) if v else np.nan for v in r[2:]] for r in records])
    names = header[2:]
    metadata = read(BASE / "feature-manifest.json")
    assert names == [m["name"] for m in metadata]
    for npz, manifest in ((EXTRA / "extra-features.npz", EXTRA / "extra-feature-manifest.json"),
                          (OUT / "context-features.npz", OUT / "context-feature-manifest.json")):
        if not npz.exists():
            continue
        with np.load(npz, allow_pickle=False) as p:
            assert p["matchIds"].astype(str).tolist() == ids
            values = p["values"] if "values" in p else p["X"]
            X = np.column_stack((X, values))
        more = read(manifest)
        metadata += more
        names += [m["name"] for m in more]
    cols = {name: j for j, name in enumerate(names)}
    p = X[:, cols["euro.average.latest.prob.draw"]]
    assert len(ids) == len(set(ids)) == 4161
    assert len(names) == len(set(names)) == X.shape[1] == report["features"]
    assert [str(r["matchId"]) for r in data] == ids
    assert np.array_equal(y, [r["result"] for r in data])
    assert int((y == 1).sum()) == 970
    issues, checked = [], 0

    def check(actual, expected, path):
        nonlocal checked
        checked += 1
        if isinstance(expected, dict):
            for key, value in expected.items():
                if key not in actual:
                    issues.append(path + "." + key + " missing")
                else:
                    check(actual[key], value, path + "." + key)
        elif isinstance(expected, list):
            if not isinstance(actual, list) or len(actual) != len(expected):
                issues.append(path + " length")
            else:
                for j, value in enumerate(expected):
                    check(actual[j], value, path + f"[{j}]")
        elif isinstance(expected, float):
            if actual is None or not math.isclose(actual, expected, rel_tol=3e-10, abs_tol=3e-10):
                issues.append(f"{path}: {actual!r} != {expected!r}")
        elif actual != expected:
            issues.append(f"{path}: {actual!r} != {expected!r}")

    def count(mask):
        h, d, a = np.bincount(y[mask], minlength=3).tolist()
        n = int(mask.sum())
        return {"selected": n, "home": h, "draw": d, "away": a,
                "falseSelections": h + a, "precision": d / n if n else None,
                "recall": d / 970, "wilson95Descriptive": interval(d, n)}

    def predicate(atom):
        v = X[:, cols[atom["feature"]]]
        if atom.get("transform", "identity") == "abs":
            v = np.abs(v)
        e = np.isfinite(v)
        op, value = atom["op"], atom["value"]
        if op == "le":
            m = v <= value
        elif op == "lt":
            m = v < value
        elif op == "ge":
            m = v >= value
        elif op == "gt":
            m = v > value
        elif op == "eq":
            m = v == value
        else:
            raise ValueError(op)
        for g in atom.get("guards", []):
            gm, ge = predicate(g)
            e = e & gm & ge
        return m & e, e

    atoms = {a["id"]: a for a in catalog["atoms"]}
    atomic_masks, atomic_e = {}, {}
    for aid, atom in atoms.items():
        atomic_masks[aid], atomic_e[aid] = predicate(atom)
        if atom["feature"].endswith(".n") or atom["feature"].startswith("availability."):
            issues.append(aid + " denominator/availability used as principal predicate")
    rules = {r["id"]: r for r in catalog["rules"]}
    masks, eligibility, seen, rule_counts = {}, {}, set(), {}
    alias_checks = 0
    for rid, rule in rules.items():
        all_of = rule["allOf"]
        if not 1 <= len(all_of) <= 2:
            issues.append(rid + " principal condition complexity")
        m = np.logical_and.reduce([atomic_masks[aid] for aid in all_of])
        e = np.logical_and.reduce([atomic_e[aid] for aid in all_of])
        masks[rid], eligibility[rid] = m, e
        rule_counts[rid] = count(m)
        check(rule["stats"], rule_counts[rid], rid + ".stats")
        key = np.packbits(m).tobytes()
        if key in seen:
            issues.append(rid + " duplicate selected mask")
        seen.add(key)
        for alias in [all_of] + rule["aliases"]:
            am = np.logical_and.reduce([atomic_masks[aid] for aid in alias])
            ae = np.logical_and.reduce([atomic_e[aid] for aid in alias])
            alias_checks += 1
            if not np.array_equal(am, m):
                issues.append(rid + " alias mask differs")
            if am.sum() < 80 or (am & (y == 1)).sum() < 20 or (ae & ~am).sum() < 80:
                issues.append(rid + " unsupported alias")
            if len(alias) == 2:
                if atoms[alias[0]]["family"] == atoms[alias[1]]["family"]:
                    issues.append(rid + " same-family interaction")
                if atoms[alias[0]]["feature"] == atoms[alias[1]]["feature"]:
                    issues.append(rid + " same-feature interaction")
    check(report["counts"], count(np.ones(len(y), bool)), "counts")
    check(catalog["search"]["uniqueMasks"], len(rules), "uniqueMasks")
    check(catalog["search"]["supportedCandidatesBeforeMaskDedup"], alias_checks, "supportedCandidatesBeforeMaskDedup")

    # Actual Beijing kickoff dates, not the scraper's schedule-date label.
    days = np.asarray([datetime.fromisoformat(r["matchTime"].replace("Z", "+00:00")).astimezone(timezone(timedelta(hours=8))).date().isoformat() for r in data])
    cuts = protocol["periodBoundariesBJT"]
    day_counts = Counter(days.tolist())
    unique_days = sorted(day_counts)
    cumulative, running = [], 0
    for day in unique_days:
        running += day_counts[day]
        cumulative.append(running)
    first = min(range(len(unique_days)), key=lambda j: abs(cumulative[j] - len(y) / 3))
    second = max(first + 1, min(range(len(unique_days)), key=lambda j: abs(cumulative[j] - 2 * len(y) / 3)))
    check(cuts, [unique_days[first], unique_days[second]], "periodBoundariesBJT")
    pm = {"early": days <= cuts[0], "middle": (days > cuts[0]) & (days <= cuts[1]), "late": days > cuts[1]}
    check(int(sum(int(m.sum()) for m in pm.values())), len(y), "periodPartition")
    for name, m in pm.items():
        check(report["periods"][name], count(m), "periods." + name)

    def market(m, e):
        available = e & np.isfinite(p)
        selected = m & available
        actual = count(selected)
        n, d = actual["selected"], actual["draw"]
        sn = np.zeros(len(y), bool)
        sd = np.zeros(len(y), bool)
        if n:
            threshold = sorted(p[available], reverse=True)[n - 1]
            sn = available & (p >= threshold)
        if d:
            threshold = sorted(p[available & (y == 1)], reverse=True)[d - 1]
            sd = available & (p >= threshold)
        return {"available": count(available), "ruleInCommonCohort": actual,
                "marketSameSelectedCountWithAllTies": count(sn),
                "marketHistoricalSameDrawTargetWithAllTies": count(sd),
                "sameDrawTarget": d, "excludedRuleRowsMissingMarket": int((m & ~np.isfinite(p)).sum()),
                "sameDrawUsesOutcomes": True}

    def detail(m, e):
        c, pop, rest = count(m), count(e), count(e & ~m)
        expected = 0.0
        strata = []
        bounds = [0, .15, .20, .25, .30, .35, 1.0000001]
        for j in range(7):
            sm = ~np.isfinite(p) if j == 6 else (np.isfinite(p) & (p >= bounds[j]) & (p < bounds[j + 1]))
            ps, cs = count(e & sm), count(m & sm)
            ex = cs["selected"] * (ps["precision"] or 0)
            expected += ex
            strata.append({"range": "missing" if j == 6 else [bounds[j], bounds[j + 1]], "eligible": ps, "selected": cs, "expectedDraws": ex})
        per = {}
        for name, t in pm.items():
            s, b = count(m & t), count(e & t)
            s.update(eligible=b, liftPP=100 * (s["precision"] - b["precision"]) if s["selected"] and b["selected"] else None)
            s["coverageOfAllPeriodDraws"] = s["draw"] / int((t & (y == 1)).sum()) if (t & (y == 1)).any() else None
            per[name] = s
        return {**c, "eligible": pop, "eligibleLiftPP": 100 * (c["precision"] - pop["precision"]) if c["selected"] else None,
                "notMatchedEligible": rest,
                "versusNotMatchedPP": 100 * (c["precision"] - rest["precision"]) if c["selected"] and rest["selected"] else None,
                "marketStratified": {"expectedDrawsFromSameEligibleStrata": expected, "excessDraws": c["draw"] - expected,
                                      "excessPP": 100 * (c["draw"] - expected) / c["selected"] if c["selected"] else None, "strata": strata},
                "periods": per, "allThreePositiveWith20Rows": all(v["selected"] >= 20 and (v["liftPP"] or -1) > 0 for v in per.values()),
                "marketComparison": market(m, e)}

    evidence_ids = set()
    for item in report["evidence"]:
        rid = item["id"]
        evidence_ids.add(rid)
        m, e = masks[rid], eligibility[rid]
        check(item["stats"], detail(m, e), "evidence." + rid)
        check(item["matchIds"], [ids[i] for i in np.flatnonzero(m)], "evidence." + rid + ".matchIds")
        check(item["aliasCount"], len(rules[rid]["aliases"]), "evidence." + rid + ".aliasCount")
        scores = Counter(f"{data[i]['homeScore']}:{data[i]['awayScore']}" for i in np.flatnonzero(m & (y == 1)))
        check(item["stats"]["drawScoreCounts"], dict(sorted(scores.items())), "evidence." + rid + ".drawScoreCounts")
        common = market(m, e)
        actual, reference = common["ruleInCommonCohort"], common["marketSameSelectedCountWithAllTies"]
        check(item["stats"]["commonMarketPrecisionDifferencePP"], 100 * (actual["precision"] - reference["precision"]) if actual["selected"] and reference["selected"] else None, "evidence." + rid + ".commonMarketPrecisionDifferencePP")
        for parent in item["parents"]:
            remain = [a for a in rules[rid]["allOf"] if a != parent["removed"]]
            parent_mask = e & np.logical_and.reduce([atomic_masks[a] for a in remain])
            rest = count(parent_mask & ~m)
            check(parent["parent"], count(parent_mask), "parent." + rid)
            check(parent["parentNotChild"], rest, "parentNotChild." + rid)
            check(parent["childVersusDisjointParentPP"], 100 * (count(m)["precision"] - rest["precision"]) if rest["selected"] else None, "parentLift." + rid)
    front, best_d = [], -1
    for r in sorted(rules.values(), key=lambda r: (rule_counts[r["id"]]["falseSelections"], -rule_counts[r["id"]]["draw"], len(r["allOf"]), r["id"])):
        d = rule_counts[r["id"]]["draw"]
        if d > best_d:
            front.append(r["id"])
            best_d = d
    check(report["paretoRuleIds"], front, "paretoRuleIds")
    short = sorted(rules.values(), key=lambda r: (rule_counts[r["id"]]["wilson95Descriptive"][0], rule_counts[r["id"]]["draw"], -len(r["allOf"]), r["id"]), reverse=True)
    check(report["rankedRuleIds"], [r["id"] for r in short[:100]], "rankedRuleIds")
    stable = []
    for r in short:
        rid = r["id"]
        if rule_counts[rid]["draw"] < 30:
            continue
        supported = True
        for t in pm.values():
            mm, ee = masks[rid] & t, eligibility[rid] & t
            n, en = int(mm.sum()), int(ee.sum())
            d, ed = int((mm & (y == 1)).sum()), int((ee & (y == 1)).sum())
            minimum_lift = protocol.get("minimumPeriodLiftPPForStableDisplay", 0) / 100
            if n < 20 or en - n < 20 or d * en <= ed * n or d / n - ed / en < minimum_lift:
                supported = False
                break
        if supported:
            stable.append(rid)
    if "stableRuleIds" in report:
        check(report["stableRuleIds"], stable[:40], "stableRuleIds")
        check(report["allThreePeriodPositiveRuleCount"], len(stable), "allThreePeriodPositiveRuleCount")
    family_best = {}
    for r in short:
        family = "+".join(sorted({atoms[aid]["family"] for aid in r["allOf"]}))
        family_best.setdefault(family, r["id"])
    check(report["familyBestRuleIds"], family_best, "familyBestRuleIds")
    theme_members = set()
    for theme in report.get("themes", []):
        for rid in theme["ruleIds"]:
            if rid not in evidence_ids:
                issues.append("theme " + theme["id"] + " missing evidence " + rid)
            theme_members.add(rid)
    for union in report.get("themeUnions", []):
        m = np.logical_or.reduce([masks[rid] for rid in union["ruleIds"]])
        check(union["stats"], detail(m, np.ones(len(y), bool)), "themeUnion." + union["label"])
        check(union["matchIds"], [ids[i] for i in np.flatnonzero(m)], "themeUnion." + union["label"] + ".matchIds")
    chain_steps = 0
    for c in report["chains"]:
        mode, union, used = c["mode"], np.zeros(len(y), bool), set()
        pool = [r for r in rules.values() if rule_counts[r["id"]]["precision"] >= np.mean(y == 1) + .01]
        for step in c["steps"]:
            choices = []
            for r in pool:
                if r["id"] in used:
                    continue
                delta = masks[r["id"]] & ~union
                d, f = int((delta & (y == 1)).sum()), int((delta & (y != 1)).sum())
                if d < 5:
                    continue
                exponent = protocol.get("chainCostExponents", {"purity": 1, "coverage": .5})[mode]
                value = d / ((f + 1) ** exponent)
                choices.append((value, d, -f, -len(r["allOf"]), r["id"]))
            win = max(choices)
            check(step["ruleId"], win[4], "chain." + mode + ".ruleId")
            rid = step["ruleId"]
            delta = masks[rid] & ~union
            d, f = int((delta & (y == 1)).sum()), int((delta & (y != 1)).sum())
            union |= masks[rid]
            used.add(rid)
            check(step, {"addedDraws": d, "addedFalseSelections": f, "addedPrecision": d / (d + f), "cumulative": count(union), "marketComparison": market(union, np.ones(len(y), bool))}, "chain." + mode)
            chain_steps += 1
        if len(c["steps"]) > 16:
            issues.append(mode + " too many chain steps")
        if len(c["steps"]) < 16 and count(union)["draw"] < 900:
            remaining = [r for r in pool if r["id"] not in used and ((masks[r["id"]] & ~union) & (y == 1)).sum() >= 5]
            if remaining:
                issues.append(mode + " stopped before fixed greedy stopping condition")
    for path, expected in report["sourceHashes"].items():
        check(sha(ROOT / path), expected, "sourceHash." + path)
    artifact = {"version": "historical-value-independent-review-v1", "generatedAt": datetime.now(timezone.utc).isoformat(),
                "sourceHashes": {p.name: sha(p) for p in (OUT / "analysis.json", OUT / "catalog.json", OUT / "protocol.json", Path(__file__))},
                "games": len(y), "draws": int((y == 1).sum()), "features": len(names),
                "atomicMasks": len(atoms), "uniqueRuleMasks": len(rules), "aliasPredicateSets": alias_checks,
                "evidenceRules": len(evidence_ids), "parentComparisons": sum(len(i["parents"]) for i in report["evidence"]),
                "allCandidateThreePeriodPositiveCount": len(stable),
                "themeRules": len(theme_members), "themeUnions": len(report.get("themeUnions", [])),
                "chainSteps": chain_steps, "checkedValues": checked,
                "issues": issues[:100], "issueCount": len(issues),
                "interpretation": "Recounts full-history fitted associations, same-E cohorts, fixed market strata and fitted OR chains. It is not a new predictive validation."}
    (OUT / "review.json").write_text(json.dumps(artifact, ensure_ascii=False, allow_nan=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"uniqueRuleMasks": len(rules), "checkedValues": checked, "issueCount": len(issues), "firstIssues": issues[:10]}, ensure_ascii=False), flush=True)
    if issues:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
