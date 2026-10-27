#!/usr/bin/env python3
"""Label-free, training-only business predicates for a draw-pool reduction study.

The caller supplies X_train explicitly. No dataset, outcomes, folds, database,
network or previous study result is read here. The returned predicates describe
candidate rejection signals; the caller must qualify/select them using only its
development data and evaluate a locked later period separately.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
from collections import Counter, defaultdict, deque

import numpy as np

from draw_cover_candidates import build_candidates

QUANTILES = (.10, .25, .50, .75, .90)
MIN_FINITE = 40
MAX_RULES = 5000
ALLOWED_FAMILIES = {
    "euroOdds", "euroMovement", "priceDifference", "kelly", "companyLines",
    "safeRecent", "safeH2H", "snapshotProfit", "marketProfit", "sourceTrade",
    "heat", "snapshotRetail", "snapshotHistory", "professional",
}
FORBIDDEN_NAMES = {"result", "homeScore", "guestScore", "awayScore", "matchId",
                   "matchTime", "date", "status", "displayState", "isDraw", "subtype"}


def _id(prefix, value):
    text = json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"), allow_nan=False)
    return prefix + hashlib.sha256(text.encode("utf-8")).hexdigest()[:16]


def _sample_column(name):
    return name.endswith(".n") or name.endswith((".recentHomeMatches", ".recentGuestMatches"))


def _history_guards(name):
    """Return actual corresponding source counts, never a substitute history."""
    if name.startswith("safeRecent.pair."):
        return [{"feature": f"safeRecent.{side}.10.n", "op": "ge", "value": 5} for side in ("home", "away")]
    if name.startswith("safeRecent."):
        pieces = name.split(".")
        return [{"feature": ".".join(pieces[:3]) + ".n", "op": "ge", "value": 5}]
    if name.startswith("safeH2H."):
        pieces = name.split(".")
        return [{"feature": ".".join(pieces[:2]) + ".n", "op": "ge", "value": 3}]
    if name in ("snapshot.recentGoalAverage", "snapshot.goalsInput.recentTotalGoals"):
        return [{"feature": "snapshot.goalsInput.recent" + side + "Matches", "op": "ge", "value": 5}
                for side in ("Home", "Guest")]
    if name in ("snapshot.historyGoalAverage", "snapshot.combinedGoalAverage", "snapshot.combinedHandicapAverage"):
        # The frozen 470 columns do not expose the original snapshot's
        # againstSummary denominator. A reconstructed H2H count is not it.
        return None
    return []


def _compare(values, op, threshold):
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
    raise ValueError("unsupported comparison: " + str(op))


def build_rejection_candidates(X_train, names, manifest):
    """Return ``atoms/rules/excluded/thresholdSource`` using training X only.

    Atom finite contract: the feature and every guard must be finite. Apply the
    atom's identity/abs transform, compare exactly with ge/le/lt/gt/eq, then AND
    all guards. A strict complement is only complementary *inside the same
    eligible input set*: missing data never become a rejection match.
    """
    matrix = np.asarray(X_train, dtype=np.float64)
    names = [str(name) for name in names]
    if matrix.ndim != 2 or matrix.shape[1] != len(names):
        raise ValueError("X_train must be a two-dimensional matrix aligned with names")
    if len(set(names)) != len(names):
        raise ValueError("duplicate input feature names")
    if not isinstance(manifest, list) or any(not isinstance(item, dict) or not isinstance(item.get("name"), str) for item in manifest):
        raise ValueError("manifest must be named frozen feature metadata")
    metadata = {item["name"]: item for item in manifest}
    if len(metadata) != len(manifest):
        raise ValueError("duplicate manifest names")
    column = {name: index for index, name in enumerate(names)}
    atoms, rules, excluded = [], [], []
    atom_map, rule_map, predicate_map = {}, {}, {}
    quantile_records = []
    fixed_map, inverse_map, atom_origin = {}, {}, {}

    def exclusion(feature, reason, **details):
        excluded.append({"feature": feature, "reason": reason, **details})

    def allowed(name):
        item = metadata.get(name)
        if name in FORBIDDEN_NAMES or name.startswith(("availability.", "target.", "label.")):
            return False, "target/identifier/availability column is not a business signal"
        if _sample_column(name):
            return False, "pure history sample count is a guard, never a rejection signal"
        if item is None:
            return False, "not in the frozen feature manifest"
        if item.get("family") not in ALLOWED_FAMILIES or item.get("view") not in ("market", "safeHistory", "snapshotDerived"):
            return False, "unsupported business family/view"
        if name not in column:
            return False, "not in the caller-supplied training feature matrix"
        return True, None

    def add_atom(feature, op, value, transform, label, guards, origin):
        ok, reason = allowed(feature)
        if not ok:
            exclusion(feature, reason, origin=origin)
            return None
        if op not in ("ge", "le", "lt", "gt", "eq") or transform not in ("identity", "abs"):
            raise ValueError("invalid predicate schema")
        if not isinstance(value, (int, float)) or not math.isfinite(value):
            raise ValueError("predicate threshold must be finite")
        guards = [dict(guard) for guard in guards]
        absent = [guard["feature"] for guard in guards if guard["feature"] not in column or guard["feature"] not in metadata]
        if absent:
            exclusion(feature, "corresponding source sample guards missing", guards=absent, origin=origin)
            return None
        core = {"feature": feature, "op": op, "value": value, "transform": transform, "guards": guards}
        # A fixed integer threshold 1 and a learned float threshold 1.0 are
        # the same predicate. Preserve the first stable ID instead of creating
        # duplicate candidates merely because JSON numbers spell differently.
        semantic_key = (feature, op, float(value), transform,
                        tuple((guard["feature"], guard["op"], float(guard["value"])) for guard in guards))
        if semantic_key in predicate_map:
            return predicate_map[semantic_key]
        ident = _id("a_", core)
        if ident not in atom_map:
            info = metadata[feature]
            atom = {"id": ident, **core, "label": label, "family": info["family"],
                    "view": info["view"], "source": info.get("source")}
            atom_map[ident] = atom
            atom_origin[ident] = origin
            atoms.append(atom)
            predicate_map[semantic_key] = ident
        return ident

    fixed = build_candidates(manifest)
    for original in fixed["atoms"]:
        ident = add_atom(original["feature"], original["op"], original["value"], original["transform"],
                         original["label"], original["guards"], "fixed_business")
        if ident:
            fixed_map[original["id"]] = ident
        inverse_op = {"ge": "lt", "le": "gt"}.get(original["op"])
        inverse_value = original["value"]
        if original["op"] == "eq" and metadata.get(original["feature"], {}).get("unit") == "indicator" and original["value"] in (0, 1):
            inverse_op, inverse_value = "eq", 1-original["value"]
        if inverse_op is None:
            exclusion(original["feature"], "only ge/le and verified boolean eq have a single-atom strict complement", origin="fixed_complement")
            continue
        inverse = add_atom(original["feature"], inverse_op, inverse_value, original["transform"],
                           "有效输入内未满足（" + original["label"] + "）", original["guards"], "fixed_complement")
        if inverse:
            inverse_map[original["id"]] = inverse

    for name in sorted(names):
        ok, reason = allowed(name)
        if not ok:
            exclusion(name, reason, origin="training_quantile")
            continue
        info = metadata[name]
        if info.get("unit") == "indicator":
            exclusion(name, "indicator: no continuous quantile thresholds", origin="training_quantile")
            continue
        guards = _history_guards(name)
        if guards is None:
            exclusion(name, "original snapshot historical denominator absent from frozen columns; do not borrow reconstructed history counts", origin="training_quantile")
            continue
        if any(guard["feature"] not in column or guard["feature"] not in metadata for guard in guards):
            exclusion(name, "corresponding actual history sample-count column missing", origin="training_quantile")
            continue
        values = matrix[:, column[name]]
        eligible = np.isfinite(values)
        for guard in guards:
            guard_values = matrix[:, column[guard["feature"]]]
            eligible &= np.isfinite(guard_values) & _compare(guard_values, guard["op"], guard["value"])
        finite = values[eligible]
        if len(finite) < MIN_FINITE:
            exclusion(name, "fewer than 40 finite eligible training values", finiteEligible=int(len(finite)), origin="training_quantile")
            continue
        quantiles = np.quantile(finite, QUANTILES, method="linear")
        record = {"feature": name, "finiteEligible": int(len(finite)), "guards": guards,
                  "values": [{"quantile": q, "value": float(value)} for q, value in zip(QUANTILES, quantiles)]}
        quantile_records.append(record)
        for q, raw_threshold in zip(QUANTILES, quantiles):
            threshold = float(raw_threshold)  # No display-rounding of learned thresholds.
            for op, symbol in (("ge", "≥"), ("le", "≤")):
                matched = int(_compare(finite, op, threshold).sum())
                if not 0 < matched < len(finite):
                    exclusion(name, "constant quantile predicate on finite eligible training inputs", quantile=q,
                              op=op, value=threshold, matched=matched, finiteEligible=len(finite), origin="training_quantile")
                    continue
                label = str(info.get("label", name)) + symbol + repr(threshold) + f"（训练{q:.0%}分位）"
                add_atom(name, op, threshold, "identity", label, guards, "training_quantile")

    def add_rule(atom_ids, mechanism):
        atom_ids = sorted(set(atom_ids))
        if len(atom_ids) not in (1, 2):
            return False
        if len(atom_ids) == 2:
            a, b = (atom_map[ident] for ident in atom_ids)
            if (a["feature"], a["transform"]) == (b["feature"], b["transform"]):
                return False
        ident = _id("r_", atom_ids)
        if ident in rule_map:
            return False
        rule = {"id": ident, "allOf": atom_ids,
                "label": " 且 ".join(atom_map[atom_id]["label"] for atom_id in atom_ids), "mechanism": mechanism}
        rule_map[ident] = rule
        rules.append(rule)
        return True

    # All candidate atoms become actual single-condition rules. Quantile
    # columns are never silently calculated then omitted as unused atoms.
    for atom in atoms:
        add_rule([atom["id"]], "单条件排除候选；" + atom_origin[atom["id"]])
    if len(rules) > MAX_RULES:
        raise RuntimeError("complete fixed/quantile singles exceed the predeclared 5000-rule budget")
    single_count = len(rules)
    compound_added = Counter()
    # Bounded transformations of existing two-concept business templates only.
    # (not A AND not B) is NOT advertised as NOT(A AND B).
    variants = (
        ("both_complement", True, True, "双原子反向（不是整条AND取反）"),
        ("original", False, False, "原双条件"),
        ("left_complement", True, False, "左原子反向"),
        ("right_complement", False, True, "右原子反向"),
    )
    for variant, invert_left, invert_right, title in variants:
        groups = defaultdict(list)
        for source_rule in fixed["rules"]:
            if len(source_rule["allOf"]) != 2:
                continue
            first, second = source_rule["allOf"]
            first = (inverse_map if invert_left else fixed_map).get(first)
            second = (inverse_map if invert_right else fixed_map).get(second)
            if not first or not second:
                continue
            groups[source_rule["mechanism"]].append((source_rule["id"], first, second, source_rule["mechanism"]))
        queue = deque((mechanism, deque(sorted(entries))) for mechanism, entries in sorted(groups.items()))
        while queue:
            mechanism, entries = queue.popleft()
            source_id, first, second, source_mechanism = entries.popleft()
            if len(rules) < MAX_RULES:
                if add_rule([first, second], title + "：" + source_mechanism):
                    compound_added[variant] += 1
            else:
                excluded.append({"sourceRuleId": source_id, "variant": variant,
                                 "reason": "fixed 5000-rule budget; singles first then variant-priority/mechanism round-robin, no outcomes consulted"})
            if entries:
                queue.append((mechanism, entries))

    threshold_source = {
        "method": "caller-supplied training X only; no y/outcome/fold file/held-later-period/prior-result input",
        "trainingRows": int(matrix.shape[0]), "trainingColumns": len(names),
        "quantiles": list(QUANTILES), "quantileMethod": "numpy.quantile(method='linear')",
        "minimumFiniteEligibleTrainingValues": MIN_FINITE,
        "thresholdsNotRounded": True, "quantilePredicatesNonconstantWithinEligibleTraining": True,
        "missingNeverMatchesStrictComplements": True,
        "fixedBusinessAtomCount": len(fixed["atoms"]),
        "atomOrigins": dict(Counter(atom_origin.values())),
        "singleRules": single_count, "compoundRulesByVariant": dict(compound_added),
        "maximumRules": MAX_RULES,
        "budgetPolicy": "retain every fixed/strict-complement/eligible-quantile single; remaining slots: both-complement, original, left-complement, right-complement; stable mechanism round-robin; no label-based pruning",
        "snapshotDerived": "Original manifest view is preserved. Legacy simulation/psychology/professional aggregates share history/market inputs and must be evaluated separately from market+safeHistory.",
        "featureQuantiles": quantile_records,
    }
    assert len(atom_map) == len(atoms) and len(rule_map) == len(rules) <= MAX_RULES
    assert all(len(rule["allOf"]) in (1, 2) for rule in rules)
    return {"atoms": atoms, "rules": rules, "excluded": excluded, "thresholdSource": threshold_source}


def self_test():
    """Synthetic boundary tests only: no repository dataset or labels loaded."""
    tiny = [
        {"name": "euro.average.latest.prob.draw", "label": "平概率", "unit": "probability", "family": "euroOdds", "view": "market", "source": "synthetic source"},
        {"name": "safeRecent.home.10.drawRate", "label": "近期平率", "unit": "probability", "family": "safeRecent", "view": "safeHistory", "source": "synthetic history"},
        {"name": "safeRecent.home.10.n", "label": "样本", "unit": "count", "family": "safeRecent", "view": "safeHistory", "source": "synthetic history"},
        {"name": "kelly.latest.minimum.draw", "label": "最小凯利含平", "unit": "indicator", "family": "kelly", "view": "market", "source": "synthetic Kelly"},
        {"name": "availability.averageLatest", "label": "可用性", "unit": "indicator", "family": "inputAvailability", "view": "market", "source": "synthetic flag"},
    ]
    names = [item["name"] for item in tiny]
    matrix = np.column_stack([np.linspace(.05, .50, 100), np.linspace(0, .8, 100),
                              np.r_[np.full(60, 10), np.full(40, 4)], np.arange(100)%2, np.ones(100)])
    before = matrix.copy()
    result = build_rejection_candidates(matrix, names, tiny)
    assert np.array_equal(matrix, before)
    assert result == build_rejection_candidates(matrix[::-1], names, list(reversed(tiny)))
    assert all(atom["feature"] not in (names[2], names[4]) for atom in result["atoms"])
    records = {item["feature"]: item for item in result["thresholdSource"]["featureQuantiles"]}
    assert records[names[1]]["finiteEligible"] == 60
    for item in records[names[1]]["values"]:
        assert item["value"] == float(np.quantile(matrix[:60, 1], item["quantile"], method="linear"))
    boolean = [atom for atom in result["atoms"] if atom["feature"] == names[3]]
    assert {atom["value"] for atom in boolean} == {0, 1} and all(atom["op"] == "eq" for atom in boolean)
    fixed_ge = next(atom for atom in result["atoms"] if atom["feature"] == names[0] and atom["op"] == "ge" and atom["value"] == .20)
    strict_lt = next(atom for atom in result["atoms"] if atom["feature"] == names[0] and atom["op"] == "lt" and atom["value"] == .20)
    check = np.array([np.nan, np.inf, -.1, .20, .3])
    valid = np.isfinite(check)
    first, second = valid & _compare(check, fixed_ge["op"], .20), valid & _compare(check, strict_lt["op"], .20)
    assert not (first & second).any() and np.array_equal(first | second, valid)
    small = build_rejection_candidates(matrix[:39], names, tiny)
    assert not small["thresholdSource"]["featureQuantiles"]
    constant = matrix.copy(); constant[:, 0] = .25
    constant_result = build_rejection_candidates(constant, names, tiny)
    assert all(item["feature"] != names[0] or "训练" not in item["label"] for item in constant_result["atoms"])
    missing = matrix.copy(); missing[:, 0] = np.nan
    absent = build_rejection_candidates(missing, names, tiny)
    assert all(item["feature"] != names[0] for item in absent["thresholdSource"]["featureQuantiles"])
    return {"noOutcomesLoaded": True, "inputNotMutated": True, "rowAndManifestOrderStable": True,
            "guardsAppliedBeforeQuantiles": True, "strictBoundaryAndMissingFiniteChecked": True,
            "booleanComplementsChecked": True, "minimum40AndConstantsChecked": True,
            "syntheticAtoms": len(result["atoms"]), "syntheticRules": len(result["rules"])}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--self-test", action="store_true", required=True)
    parser.parse_args()
    print(json.dumps(self_test(), ensure_ascii=False))
