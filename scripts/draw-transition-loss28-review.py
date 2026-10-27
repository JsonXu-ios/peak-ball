#!/usr/bin/env python3
"""Audit the confirmed-loss focus without recomputing the full rule family."""
from __future__ import annotations

import csv
import hashlib
import importlib.util
import json
import math
from collections import Counter
from pathlib import Path

import numpy as np


def main():
    folder = Path("reports/draw-transition-study")
    load = lambda name: json.loads((folder / name).read_text(encoding="utf-8-sig"))
    focus, all_focus, original, catalog, input_rows, reviewed = (load(name) for name in ("focused-loss28.json", "focused-loss28-rules.json", "analysis.json", "catalog.json", "inputs.json", "review.json"))
    spec = importlib.util.spec_from_file_location("independent_transition_reviewer", "scripts/draw-transition-review.py")
    audit = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(audit)
    issues, checks = [], Counter()

    def equal(actual, expected, path):
        checks["fields"] += 1
        if isinstance(expected, dict):
            if not isinstance(actual, dict):
                issues.append(path + ": expected mapping")
                return
            for key, value in expected.items():
                if key not in actual:
                    issues.append(path + ": missing " + key)
                else:
                    equal(actual[key], value, path + "." + key)
        elif isinstance(expected, (list, tuple)):
            if not isinstance(actual, (list, tuple)) or len(actual) != len(expected):
                issues.append(path + ": array length mismatch")
                return
            for i, value in enumerate(expected):
                equal(actual[i], value, path + f"[{i}]")
        elif type(expected) in (int, float):
            if type(actual) not in (int, float) or not math.isclose(actual, expected, rel_tol=1e-10, abs_tol=1e-12):
                issues.append(f"{path}: {actual!r} != {expected!r}")
        elif actual != expected:
            issues.append(f"{path}: {actual!r} != {expected!r}")

    equal(reviewed["status"], "passed", "fullFamilyPriorReview")
    sealed = ("analysis.json", "catalog.json", "inputs.json", "rules.csv")
    for name in sealed:
        path = folder / name
        key = str(path).replace("\\", "/")
        equal(hashlib.sha256(path.read_bytes()).hexdigest(), reviewed["sourceHashes"][key], "unchangedMain." + name)
    for raw_path, value in focus["sourceHashes"].items():
        equal(hashlib.sha256(Path(raw_path).read_bytes()).hexdigest(), value, "focusSourceHash." + raw_path)
    risk = lambda identity: any(part.startswith("profit.") and part.endswith(".loss28") for part in identity.split(" & "))
    candidates = {c["id"]: c for c in catalog["candidates"] if risk(c["id"])}
    equal(len(candidates), 596, "fixedSubset")
    equal(focus["count"], len(candidates), "focus.count")
    equal(focus["candidateFamily"], 6737, "focus.familyCount")
    hidden_aliases = [identity for identity, aliases in catalog["aliases"].items() if not risk(identity) and any(risk(alias) for alias in aliases)]
    equal(hidden_aliases, [], "noRiskAliasesLost")
    inherited = {}
    counts = {"eligible", "eligibleDraws", "n", "draws", "falseSelections"}
    with (folder / "rules.csv").open(encoding="utf-8-sig", newline="") as stream:
        for row in csv.DictReader(stream):
            if row["rule"] not in candidates:
                continue
            numbers = {key: None if value == "" else int(value) if key in counts else float(value) for key, value in row.items() if key not in ("cohort", "rule", "label", "split")}
            inherited.setdefault((row["cohort"], row["rule"]), {})[row["split"]] = numbers
    xs = [row["inputs"] for row in input_rows]
    y = np.array([row["result"] == 1 for row in input_rows])
    marked = np.array([bool(str(row.get("jingcaiId") or "").strip()) for row in input_rows])
    dates = np.array([row["kickoffDate"] for row in input_rows])
    periods = np.where(dates <= original["timeCuts"][0], 0, np.where(dates <= original["timeCuts"][1], 1, 2))
    cohorts = {"all": np.ones(len(xs), bool), "sporttery": marked, "nonSporttery": ~marked}
    all_expected, summaries = [], {}
    for name, scope in cohorts.items():
        minimum = 30 if name == "sporttery" else 60
        qualified = []
        for identity, candidate in candidates.items():
            values = inherited[(name, identity)]
            dev = values["development"]
            if dev["n"] >= minimum and dev["eligible"] - dev["n"] >= minimum:
                qualified.append({key: candidate[key] for key in ("id", "label", "parts")} | {"metrics": values})
        qualified.sort(key=lambda row: (row["metrics"]["development"]["q"], -row["metrics"]["development"]["complementLiftPP"], -row["metrics"]["development"]["n"], row["id"]))
        supported = sum(row["metrics"]["development"]["q"] <= .05 and row["metrics"]["development"]["complementLiftPP"] > 0 for row in qualified)
        equal(focus["cohorts"][name], {"qualifiedRules": len(qualified), "devBHSupported": supported, "topDevelopment": qualified[:10]}, name + ".inheritedSelection")
        total_draws = int((scope & y).sum())
        for record in focus["cohorts"][name]["topDevelopment"]:
            valid = np.ones(len(xs), bool)
            context = np.ones(len(xs), bool)
            risk_masks = []
            for part in record["parts"]:
                values = [audit.atom(part, x) for x in xs]
                known = np.array([value is not None for value in values])
                selected = np.array([value is True for value in values])
                valid &= known
                if part.startswith("profit.") and part.endswith(".loss28"):
                    risk_masks.append(selected)
                else:
                    context &= selected
            equal(len(risk_masks), 1, name + ".singleProfitPredicate")
            same_context = valid & context & scope
            risk_rows = same_context & risk_masks[0]
            normal_rows = same_context & ~risk_masks[0]
            equal(bool(np.any(risk_rows & normal_rows)), False, name + ".disjointRiskNormal")
            equal(bool(np.array_equal(risk_rows | normal_rows, same_context)), True, name + ".completeRiskNormal")
            expected = {}
            for split, subset in (("all", np.ones(len(xs), bool)), ("development", periods < 2), ("period3", periods == 2)):
                expected[split] = {}
                for label, mask in (("risk", risk_rows), ("normal", normal_rows)):
                    selected = mask & subset
                    n, draws = int(selected.sum()), int((selected & y).sum())
                    expected[split][label] = {"n": n, "draws": draws, "falseSelections": n - draws, "precision": draws / n if n else None, "drawCoverageOfAllCohort": draws / total_draws if total_draws else None}
                equal(expected[split]["risk"]["n"], record["metrics"][split]["n"], name + ".inheritedRiskN")
                equal(expected[split]["risk"]["draws"], record["metrics"][split]["draws"], name + ".inheritedRiskDraws")
            equal(record["riskNormalSameContext"], expected, name + ".riskNormal." + record["id"])
            checks["riskNormalSplitTables"] += 3
        all_expected.extend({"cohort": name, **record} for record in qualified)
        summaries[name] = {"qualifiedRules": len(qualified), "devBHSupported": int(supported)}
    equal(all_focus, all_expected, "allQualifiedRulesAndMetrics")
    checks["qualifiedRules"] = len(all_expected)
    # Literal boundaries: -28 itself and every positive rate are normal;
    # missing is neither normal nor risk, and no 0..100 cap is appropriate.
    for value in (None, -1000, -28.000000001, -28, -27.999999999, -1, 0, 28, 28.000001, 1000):
        sample = {"profit.official": [value, value, value], "favorite": 0, "hot": 2}
        for target in ("favorite", "draw", "hot"):
            answer = audit.atom("profit.official." + target + ".loss28", sample)
            equal(answer, None if value is None else value < -28, "synthetic.risk")
            normal = None if answer is None else not answer
            equal(normal, None if value is None else value >= -28, "synthetic.normal")
            checks["boundaryCases"] += 1
    paths = [Path(__file__), Path("scripts/draw-transition-loss28-focus.py")] + [folder / name for name in ("focused-loss28.json", "focused-loss28-rules.json", "review.json", *sealed)]
    result = {"status": "passed" if not issues else "failed", "scope": "Focused subset and inherited full-family statistics only; full 6737-rule study was not rerun. Uses independently audited condition interpreter, never imports focus or study code.",
              "comparisons": dict(checks), "cohorts": summaries, "sourceHashes": {str(path).replace("\\", "/"): hashlib.sha256(path.read_bytes()).hexdigest() for path in paths}, "issues": issues}
    (folder / "focused-loss28-review.json").write_text(json.dumps(result, ensure_ascii=False, indent=2, allow_nan=False), encoding="utf-8")
    print(json.dumps({"status": result["status"], "comparisons": result["comparisons"], "cohorts": summaries, "issueCount": len(issues), "issues": issues[:10]}, ensure_ascii=False))
    raise SystemExit(bool(issues))


if __name__ == "__main__":
    main()
