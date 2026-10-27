#!/usr/bin/env python3
"""Capacity diagnostic with permuted labels, not a formal permutation test."""
from __future__ import annotations

import importlib.util
import math
import time
from pathlib import Path

import numpy as np

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("sporttery_rule_capacity", ROOT / "scripts/draw-sporttery-rules.py")
rule = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rule)


def main():
    started = time.monotonic()
    out = ROOT / "reports/draw-sporttery-study"
    actual = rule.h.read(out / "rules.json")
    winner = next(r for r in actual["bestByScopeSourceLimit"]
                  if r["scope"] == "jingcaiId" and r["source"] == "sportteryMarketAndSafeHistory"
                  and r["maximumRules"] == 16)
    cfg = next(c for c in actual["configurations"] if c["id"] == winner["id"])
    rows, X, labels, meta, sources = rule.h.load_inputs(ROOT / "reports/draw-historical-value-study")
    choose = np.array([bool(str(r.get("jingcaiId") or "").strip()) for r in rows])
    X, labels = X[choose], labels[choose]
    catalog = rule.h.read(ROOT / "reports/draw-historical-value-study/catalog.json")
    assert X.shape[0] == 511 and int(np.sum(labels == 1)) == 124
    target = math.ceil(int(np.sum(labels == 1)) * 900 / 970)
    results = []
    for seed in (20261026, 20261027, 20261028):
        yy = np.random.default_rng(seed).permutation(labels)
        pos = rule.old.bits(yy == 1)
        universe = (1 << len(yy)) - 1
        atoms, pool, search = rule.pool_for(catalog, X, meta, yy,
                                           pos.bit_count() - target,
                                           "sportteryMarketAndSafeHistory")
        selected, steps = rule.fit(pool, pos, universe, target, 16,
                                  cfg["algorithm"], cfg["coefficient"])
        pool_lookup = {r["id"]: r for r in pool}
        selected_mask = rule.old.mask(selected, len(yy))
        subset_ids = [str(r["matchId"]) for r, yes in zip(rows, choose) if yes]
        results.append({"seed": seed, "stats": rule.stat(selected, pos, target),
                        "search": search, "actualSelectedRules": len(steps),
                        "steps": steps,
                        "usedRules": [{"id": step["ruleId"],
                                       "allOf": [atoms[aid] for aid in pool_lookup[step["ruleId"]]["allOf"]]}
                                      for step in steps],
                        "selectedMatchIds": [ident for ident, yes in zip(subset_ids, selected_mask) if yes]})
        print({"seed": seed, "stats": results[-1]["stats"], "pool": len(pool)}, flush=True)
    sources.update({str(p.relative_to(ROOT)): rule.h.sha(p) for p in (
        Path(__file__), ROOT / "scripts/draw-sporttery-rules.py",
        ROOT / "reports/draw-sporttery-study/rules.json",
        ROOT / "reports/draw-historical-value-study/catalog.json")})
    data = {
        "purpose": "Random-label capacity diagnostic of fixed 16-rule historical greedy refitting",
        "formalPermutationPValue": False,
        "scope": "jingcaiId nonblank only; 511 matches, 124 positives per permutation",
        "featureRestriction": "Raw sporttery SPF prices/deterministic transforms plus safeHistory; no other-company features",
        "fixedConfiguration": {"actualWinnerId": cfg["id"], "maximumRules": 16,
                               "algorithm": cfg["algorithm"], "coefficient": cfg["coefficient"],
                               "minimumPositivesRetained": target},
        "catalogueCaveat": "Original field thresholds and candidate templates are reused from historical catalogue, which was previously explored using true labels. For each permutation, low-draw atom ranking, two-condition seeds, candidate eligibility, and greedy fitting are recomputed under that permutation. This is a capacity diagnostic, not an exchangeability-valid significance test.",
        "noRescueSearch": "Same original winning algorithm/coefficient and rule limit for every permutation; no additional threshold, coefficient, seed, or complexity search",
        "originalHistoricalFit": winner["stats"],
        "results": results,
        "randomFitsReachingTarget": sum(r["stats"]["targetReached"] for r in results),
        "elapsedSeconds": time.monotonic() - started,
        "sources": sources,
    }
    rule.h.write(out / "rule-control.json", data)
    print({"output": str((out / "rule-control.json").relative_to(ROOT)),
           "elapsedSeconds": data["elapsedSeconds"],
           "randomFitsReachingTarget": data["randomFitsReachingTarget"]}, flush=True)


if __name__ == "__main__":
    main()
