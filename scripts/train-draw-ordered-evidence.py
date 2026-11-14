#!/usr/bin/env python3
"""Offline, business-ordered conditional outcome study. Never operates services.

Each stage narrows a historical reference group, rather than rejecting the
fixture. Candidate disagreement is recorded and later stages still run. Labels
only fit the tree or settle predictions; they never route a predicted fixture.
"""
from __future__ import annotations

import argparse
import importlib.util
import time
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

import numpy as np

loader = importlib.util.spec_from_file_location(
    "alternative_tree", Path(__file__).with_name("train-draw-alternative-tree.py"))
common = importlib.util.module_from_spec(loader)
loader.loader.exec_module(common)

STAGES = ("profit", "difference", "recent", "h2h", "market", "platform")


def group(meta):
    family, name = meta["family"], meta["name"]
    if (family == "snapshotProfit" and name.startswith(("profit.sporttery.", "profit.simulated."))
            or family == "marketProfit" and name.startswith("marketProfit.sporttery.")
            or family == "sourceTrade" and name.startswith("trade.jyykSpf.")):
        return "profit"
    if family in ("priceDifference", "conflictDifferencePair"):
        return "difference"
    if family in ("safeRecent", "conflictSafeRecent") or (
            family == "historicalContext" and name.startswith(("context.form.", "context.goals."))):
        return "recent"
    if family == "safeH2H":
        return "h2h"
    if family in ("euroOdds", "euroMovement", "companyLines", "kelly", "heat") or (
            family == "sourceTrade" and name.startswith("trade.tzbl.")):
        return "market"
    if family in ("professional", "snapshotRetail", "snapshotProfit", "marketProfit"):
        return "platform"
    return None


def ranked_set(probabilities, mass):
    """Smallest top-frequency set reaching mass, retaining boundary ties."""
    order = sorted(range(3), key=lambda j: (-probabilities[j], j))
    chosen, accumulated = [], 0.
    for position, index in enumerate(order):
        chosen.append(index)
        accumulated += probabilities[index]
        if accumulated + 1e-12 >= mass:
            boundary = probabilities[index]
            chosen += [j for j in order[position + 1:]
                       if abs(probabilities[j] - boundary) <= 1e-12]
            break
    return sorted(chosen)


def unique_rank(probabilities, indices, maximum=True):
    values = [probabilities[j] for j in indices]
    extreme = max(values) if maximum else min(values)
    winners = [j for j in indices if abs(probabilities[j] - extreme) <= 1e-12]
    return winners[0] if len(winners) == 1 else -1


class OrderedTree:
    def __init__(self, metadata, min_leaf, splits_per_stage):
        self.metadata = metadata
        self.min_leaf, self.splits_per_stage = min_leaf, splits_per_stage
        self.features = [[j for j, m in enumerate(metadata) if group(m) == stage]
                         for stage in STAGES]
        self.nodes = []

    def best_split(self, ids, stage):
        if len(ids) < 2 * self.min_leaf:
            return None
        best, best_gain = None, 1e-9
        for feature in self.features[stage]:
            values = self.x[ids, feature]
            known = np.isfinite(values)
            observed_ids = ids[known]
            count = len(observed_ids)
            if count < 2 * self.min_leaf:
                continue
            order = np.argsort(values[known], kind="stable")
            observed_ids, ordered = observed_ids[order], values[known][order]
            positions = np.flatnonzero(ordered[:-1] < ordered[1:]) + 1
            positions = positions[(positions >= self.min_leaf) & (count - positions >= self.min_leaf)]
            if not len(positions):
                continue
            cumulative = self.encoded[self.y[observed_ids]].cumsum(axis=0)
            total = cumulative[-1]
            left, right = cumulative[positions - 1], total - cumulative[positions - 1]
            # Equivalent to parent Gini minus the size-weighted child Gini.
            gain = ((left * left).sum(axis=1) / positions
                    + (right * right).sum(axis=1) / (count - positions)
                    - (total * total).sum() / count) / len(ids)
            choice = int(np.argmax(gain))
            if gain[choice] > best_gain + 1e-12:
                position = int(positions[choice])
                threshold = float(ordered[position - 1] / 2 + ordered[position] / 2)
                best_gain = float(gain[choice])
                best = dict(feature=feature, threshold=threshold, gain=best_gain,
                            observed=count, left_ids=observed_ids[:position],
                            right_ids=observed_ids[position:])
        return best

    def build(self, ids, stage, depth):
        key = (stage, depth, ids.tobytes())
        if key in self.memo:
            return self.memo[key]
        index = len(self.nodes)
        self.memo[key] = index
        counts = np.bincount(self.y[ids], minlength=3)
        node = dict(id=index, stage=stage, withinStageDepth=depth, n=len(ids),
                    counts=counts.tolist(), distribution=((counts + 1) / (len(ids) + 3)).tolist())
        self.nodes.append(node)
        if stage == len(STAGES):
            node["kind"] = "terminal"
            return index
        split = self.best_split(ids, stage)
        if split is None:
            node.update(kind="skip", next=self.build(ids, stage + 1, 0),
                        reason="insufficient_reference_rows_or_no_informative_split")
            return index
        node.update(kind="split", feature=split["feature"],
                    name=self.metadata[split["feature"]]["name"], threshold=split["threshold"],
                    gain=split["gain"], observed=split["observed"])
        next_stage, next_depth = (stage, depth + 1) if depth + 1 < self.splits_per_stage else (stage + 1, 0)
        node["left"] = self.build(np.sort(split["left_ids"]), next_stage, next_depth)
        node["right"] = self.build(np.sort(split["right_ids"]), next_stage, next_depth)
        # Missing feature skips the stage using the SAME parent reference group.
        # Even if all training values exist, future missing inputs have a route.
        node["fallback"] = self.build(ids, stage + 1, 0)
        return index

    def fit(self, x, y, mask):
        self.x, self.y = x, y
        self.encoded = np.eye(3, dtype=np.int64)
        self.memo = {}
        ids = np.flatnonzero(mask & np.isfinite(x).any(axis=1))
        if len(ids) < self.min_leaf:
            raise ValueError("not enough training rows")
        self.root = self.build(ids, 0, 0)
        self.training_rows = len(ids)
        del self.x, self.y, self.encoded, self.memo
        return self

    def predict_one(self, values, mass=.8):
        if not np.isfinite(values).any():
            return dict(available=False, most=-1, least=-1, backup=-1, trace=[])
        node_id, history, prior, cumulative = self.root, [], None, None
        for stage_index, stage in enumerate(STAGES):
            tests, skips = [], []
            node = self.nodes[node_id]
            while node["stage"] == stage_index:
                if node["kind"] == "skip":
                    skips.append(node["reason"])
                    node_id = node["next"]
                else:
                    value = values[node["feature"]]
                    test = dict(feature=node["name"], threshold=node["threshold"],
                                value=float(value) if np.isfinite(value) else None,
                                referenceRowsBefore=node["n"])
                    if not np.isfinite(value):
                        test["branch"] = "missing_skip_stage"
                        node_id = node["fallback"]
                    else:
                        test["branch"] = "left" if value <= node["threshold"] else "right"
                        node_id = node[test["branch"]]
                    tests.append(test)
                node = self.nodes[node_id]
            probabilities = node["distribution"]
            candidates = ranked_set(probabilities, mass)
            intersection = sorted(set(candidates) & set(prior)) if prior is not None else candidates
            cumulative = set(candidates) if cumulative is None else cumulative & set(candidates)
            history.append(dict(stage=stage, tests=tests, skips=skips, node=node_id,
                                referenceRows=node["n"], historicalCounts=node["counts"],
                                distribution=probabilities, candidates=candidates,
                                withPriorIntersection=intersection, withAllEarlierIntersection=sorted(cumulative),
                                conflictWithPrior=prior is not None and not intersection,
                                conflictWithAllEarlier=not cumulative,
                                most=unique_rank(probabilities, (0, 1, 2)),
                                least=unique_rank(probabilities, (0, 1, 2), False),
                                backup=unique_rank(probabilities, (0, 2))))
            prior = candidates
        final = history[-1]
        return dict(available=True, most=final["most"], least=final["least"], backup=final["backup"],
                    distribution=final["distribution"], trace=history)

    def predict(self, x, mass=.8):
        return [self.predict_one(row, mass) for row in x]

    def export(self):
        return dict(root=self.root, minLeaf=self.min_leaf, splitsPerStage=self.splits_per_stage,
                    trainingRows=self.training_rows, stages=list(STAGES), nodes=self.nodes)


def market_predictions(raw_rows):
    predictions = []
    for row in raw_rows:
        odds = (row.get("frontend") or {}).get("averageEuroOdds")
        valid = (isinstance(odds, list) and len(odds) == 3
                 and all(type(v) in (int, float) and np.isfinite(v) and v > 1 for v in odds))
        if valid:
            inverse = [1 / v for v in odds]
            predictions.append(dict(most=unique_rank(inverse, (0, 1, 2)),
                                    least=unique_rank(inverse, (0, 1, 2), False),
                                    backup=unique_rank(inverse, (0, 2))))
        else:
            predictions.append(dict(most=-1, least=-1, backup=-1))
    return predictions


def metrics(labels, predictions, mask):
    result = dict(candidates=int(mask.sum()))
    for kind in ("most", "least", "backup"):
        choice = np.asarray([r[kind] for r in predictions])
        available = mask & (choice >= 0)
        hits = available & ((choice != labels) if kind == "least" else (choice == labels))
        n, correct = int(available.sum()), int(hits.sum())
        result[kind] = dict(calls=n, hits=correct, unavailable=int(mask.sum()) - n,
                            accuracy=correct / n if n else None, wilson95=common.wilson(correct, n))
        if kind == "backup":
            non_draw = available & (labels != 1)
            result[kind].update(nonDrawCalls=int(non_draw.sum()), actualDraws=int(np.sum(available & (labels == 1))),
                                conditionalAccuracy=correct / int(non_draw.sum()) if non_draw.any() else None)
    return result


def evaluate(labels, predictions, baseline, mask):
    paired = {}
    for kind in ("most", "least", "backup"):
        a = np.asarray([p[kind] for p in predictions])
        b = np.asarray([p[kind] for p in baseline])
        common_mask = mask & (a >= 0) & (b >= 0)
        paired[kind] = dict(model=metrics(labels, predictions, common_mask)[kind],
                            market=metrics(labels, baseline, common_mask)[kind])
    stage_metrics = []
    for index, stage in enumerate(STAGES):
        calls = [i for i in np.flatnonzero(mask) if predictions[i]["available"]]
        hits = sum(int(labels[i]) in predictions[i]["trace"][index]["candidates"] for i in calls)
        size = sum(len(predictions[i]["trace"][index]["candidates"]) for i in calls)
        stage_predictions = [p["trace"][index] if p["available"] else dict(most=-1, least=-1, backup=-1)
                             for p in predictions]
        stage_metrics.append(dict(stage=stage, calls=len(calls), actualIncluded=hits,
                                  candidateCoverage=hits / len(calls) if calls else None,
                                  meanCandidateSize=size / len(calls) if calls else None,
                                  candidateSets=dict(Counter(
                                      ",".join(map(str, predictions[i]["trace"][index]["candidates"])) for i in calls)),
                                  conflicts=sum(predictions[i]["trace"][index]["conflictWithPrior"] for i in calls),
                                  conflictingEarlierStages=sum(predictions[i]["trace"][index]["conflictWithAllEarlier"] for i in calls),
                                  applied=sum(bool(predictions[i]["trace"][index]["tests"]) for i in calls),
                                  metrics=metrics(labels, stage_predictions, mask)))
    return dict(model=metrics(labels, predictions, mask), market=metrics(labels, baseline, mask),
                paired=paired, stages=stage_metrics)


def candidate_baseline(model, labels, mask, mass):
    """Does the first candidate set beat simply retaining frequent train outcomes?"""
    root = model.nodes[model.root]
    candidates = ranked_set(root["distribution"], mass)
    n = int(mask.sum())
    hits = sum(int(v) in candidates for v in labels[mask])
    return dict(trainingCounts=root["counts"], fixedCandidates=candidates, calls=n,
                included=hits, inclusion=hits / n if n else None)


def compact_prediction(row):
    keys = ("available", "most", "least", "backup", "distribution")
    return {k: row[k] for k in keys if k in row}


def choose_mass(model, x, labels, mask):
    options = []
    for mass in (.6, .7, .8, .9, 1.):
        predictions = model.predict(x[mask], mass)
        known = [(int(y), p["trace"][0]["candidates"]) for y, p in zip(labels[mask], predictions) if p["available"]]
        n = len(known)
        options.append(dict(mass=mass, calls=n, inclusion=sum(y in c for y, c in known) / n if n else 0.,
                            meanSize=sum(len(c) for _, c in known) / n if n else 3.))
    acceptable = [r for r in options if r["inclusion"] >= .8]
    return min(acceptable, key=lambda r: (r["meanSize"], r["mass"]))["mass"], options


def audit_paths(model, x):
    checked = Counter()
    for row in x:
        pred = model.predict_one(row)
        if not pred["available"]:
            continue
        assert [r["stage"] for r in pred["trace"]] == list(STAGES)
        checked["completeStageTraces"] += 1
        for index, trace in enumerate(pred["trace"]):
            for test in trace["tests"]:
                assert group(next(m for m in model.metadata if m["name"] == test["feature"])) == STAGES[index]
                checked["stageOrderedTests"] += 1
            if trace["conflictWithPrior"]:
                assert len(pred["trace"]) == len(STAGES)
                checked["conflictsContinue"] += 1
    missing = model.predict_one(np.full(x.shape[1], np.nan))
    assert not missing["available"]
    # Keep one unrelated finite feature: missing chosen split must still reach later stages.
    probe = np.full(x.shape[1], np.nan)
    probe[-1] = 0.
    routed = model.predict_one(probe)
    assert len(routed["trace"]) == len(STAGES)
    checked["missingInputsContinue"] += 1
    return dict(checked)


def self_test():
    """A later conditional outcome must be able to overturn the profit candidate."""
    metadata = [dict(family=family, name=name) for family, name in (
        ("marketProfit", "marketProfit.sporttery.roi.home"),
        ("priceDifference", "difference.latest.average.home"),
        ("safeRecent", "safeRecent.home.10.winRate"),
        ("safeH2H", "safeH2H.all.homeWinRate"),
        ("euroOdds", "odds.latest.average.home"),
        ("professional", "professional.platform.outcome.away"))]
    x = np.zeros((120, 6))
    x[60:, 0] = 1
    x[50:60, 1] = x[110:120, 1] = 1
    y = np.asarray([0] * 50 + [2] * 10 + [2] * 50 + [0] * 10)
    model = OrderedTree(metadata, 5, 1).fit(x, y, np.ones(120, dtype=bool))
    p = model.predict_one(np.asarray([0., 1., 0., 0., 0., 0.]), .6)
    assert p["trace"][0]["candidates"] == [0]
    assert p["trace"][1]["candidates"] == [2] and p["trace"][1]["conflictWithPrior"]
    assert p["most"] == p["backup"] == 2 and len(p["trace"]) == 6
    assert not model.predict_one(np.full(6, np.nan))["available"]
    assert len(model.predict_one(np.asarray([np.nan, 1., 0., 0., 0., 0.]))["trace"]) == 6
    assert ranked_set([.4, .3, .3], .6) == [0, 1, 2]
    assert unique_rank([.4, .2, .4], (0, 2)) == -1
    return dict(conflictingCandidatesContinue=True, laterEvidenceCanOverturn=True,
                missingStageContinues=True, allMissingAbstains=True, rankingTiesRetained=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("matrix", "raw", "replay", "protocol", "live-matrix", "live-raw", "live-ledger", "out"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    start = time.perf_counter()
    logic_audit = self_test()
    read, write = common.read, common.write
    protocol, source, raw, replay = map(read, (args.protocol, args.matrix, args.raw, args.replay))
    source_sha = common.sha(args.raw)
    assert source_sha == protocol["sourceSHA256"] == source["sourceSHA256"] == replay["studySHA256"]
    raw["_sha256"] = source_sha
    x, columns, feature_audit = common.matrix(source, raw)
    metadata = [source["metadata"][j] for j in columns]
    rows = source["rows"]
    ids = [str(r["matchId"]) for r in rows]
    assert ids == [str(r["matchId"]) for r in replay["items"]]
    assert len(ids) == len(set(ids))
    labels = np.asarray([r["result"] for r in rows], dtype=np.int8)
    assert set(labels) == {0, 1, 2}
    assert all(r["result"] == (0 if r["homeScore"] > r["awayScore"] else 1 if r["homeScore"] == r["awayScore"] else 2) for r in rows)
    days = np.asarray([common.bjt_day(r["matchTime"]) for r in rows])
    cut = protocol["cutoffsBjt"]
    periods = dict(train=days <= cut["trainThrough"],
                   calibration=(days > cut["trainThrough"]) & (days <= cut["calibrationThrough"]),
                   test=days > cut["calibrationThrough"])
    marked = np.asarray([bool(str(r.get("jingcaiId") or "").strip()) for r in rows])
    global_selected = np.asarray([r["researchOnlyUnscopedInputAvailable"] and r["researchOnlyUnscopedPrediction"]["selected"] for r in replay["items"]])
    jc_selected = np.asarray([r["productionOriginalSelected"] for r in replay["items"]])
    assert np.all(~jc_selected | (global_selected & marked))
    scopes = dict(global_selected=global_selected, jc_selected=jc_selected)
    pools = dict(global_all=np.ones(len(rows), dtype=bool), jc_all=marked, **scopes)
    baseline = market_predictions(raw["rows"])
    assert all(p["backup"] == b for p, b in zip(baseline, common.low_price(raw["rows"])))
    args.out.mkdir(parents=True, exist_ok=True)
    calibration = []
    for pool in protocol["configs"]["trainingPools"]:
        for leaf in protocol["configs"]["minLeaf"]:
            for splits in protocol["configs"]["splitsPerStage"]:
                config = dict(id=f"{pool}-l{leaf}-s{splits}", pool=pool, minLeaf=leaf, splitsPerStage=splits)
                tick = time.perf_counter()
                model = OrderedTree(metadata, leaf, splits).fit(x, labels, pools[pool] & periods["train"])
                prediction = model.predict(x)
                record = dict(config=config, nodes=len(model.nodes), trainingRows=model.training_rows, scopes={})
                for scope, selected in scopes.items():
                    if scope == "global_selected" and pool.startswith("jc_"):
                        continue
                    mask = selected & periods["calibration"]
                    record["scopes"][scope] = evaluate(labels, prediction, baseline, mask)
                calibration.append(record)
                print("CAL", config["id"], "nodes", len(model.nodes), "sec", round(time.perf_counter() - tick, 2), flush=True)
    write(args.out / "calibration.json", calibration)
    choices = {}
    for scope, selected in scopes.items():
        candidates = [r for r in calibration if scope in r["scopes"]]
        def key(record):
            metrics_ = record["scopes"][scope]
            n = metrics_["paired"]["most"]["model"]["calls"]
            base_n = metrics_["market"]["most"]["calls"]
            return (n >= .8 * base_n, metrics_["paired"]["most"]["model"]["accuracy"] or 0.,
                    metrics_["paired"]["least"]["model"]["accuracy"] or 0., -record["nodes"], record["config"]["id"])
        chosen = max(candidates, key=key)
        config = chosen["config"]
        calibration_model = OrderedTree(metadata, config["minLeaf"], config["splitsPerStage"]).fit(
            x, labels, pools[config["pool"]] & periods["train"])
        mass, options = choose_mass(calibration_model, x, labels, selected & periods["calibration"])
        choices[scope] = dict(config=config, candidateMass=mass, candidateMassCalibration=options,
                              calibration=chosen["scopes"][scope])
    # Seal configuration and candidate-set definitions before test/LIVE settlement.
    write(args.out / "selected-on-calibration.json", dict(protocolSHA256=common.sha(args.protocol), choices=choices))
    live_source, live_raw, ledger = map(read, (args.live_matrix, args.live_raw, args.live_ledger))
    live_raw["_sha256"] = common.sha(args.live_raw)
    lx, live_columns, live_feature_audit = common.matrix(live_source, live_raw, columns)
    assert columns == live_columns and source["featureNames"] == live_source["featureNames"]
    latest = {str(r["matchId"]): r for r in ledger["latest"]}
    live_rows = live_source["rows"]
    live_labels = np.asarray([latest[str(r["matchId"])]["result"] if latest[str(r["matchId"])]["finished"] else -1 for r in live_rows], dtype=np.int8)
    live_selected = np.asarray([latest[str(r["matchId"])]["original"] and latest[str(r["matchId"])]["finished"] for r in live_rows])
    live_selected &= np.asarray([common.bjt_day(r["matchTime"]) > cut["calibrationThrough"] for r in live_rows])
    live_base = market_predictions(live_raw["rows"])
    assert all(p["backup"] == latest[str(r["matchId"])]["direction"] for p, r in zip(live_base, live_rows))
    before22 = []
    for row in live_rows:
        record = latest[str(row["matchId"])]
        purchase_day = str(record.get("purchaseDay") or "")
        cutoff = datetime.fromisoformat(purchase_day + "T22:00:00+08:00") if purchase_day else None
        predicted_at = datetime.fromisoformat(record["predictedAt"].replace("Z", "+00:00"))
        before22.append(cutoff is not None and predicted_at <= cutoff)
    results = {}
    for scope, choice in choices.items():
        config = choice["config"]
        model = OrderedTree(metadata, config["minLeaf"], config["splitsPerStage"]).fit(
            x, labels, pools[config["pool"]] & ~periods["test"])
        prediction = model.predict(x, choice["candidateMass"])
        live_prediction = model.predict(lx, choice["candidateMass"])
        result = dict(config=config, candidateMass=choice["candidateMass"], nodes=len(model.nodes),
                      trainingRows=model.training_rows, trainedThroughBjt=cut["calibrationThrough"],
                      test=evaluate(labels, prediction, baseline, scopes[scope] & periods["test"]),
                      resubstitution=metrics(labels, prediction, pools[config["pool"]] & ~periods["test"]),
                      pathAudit=audit_paths(model, np.concatenate((x[scopes[scope] & periods["test"]], lx))),
                      featureUsage=dict(Counter(n["name"] for n in model.nodes if n["kind"] == "split")))
        result["testCandidateFrequencyBaseline"] = candidate_baseline(
            model, labels, scopes[scope] & periods["test"], choice["candidateMass"])
        if scope == "jc_selected":
            result["sealedLive"] = evaluate(live_labels, live_prediction, live_base, live_selected)
            result["sealedLiveLatestBefore22"] = evaluate(live_labels, live_prediction, live_base, live_selected & np.asarray(before22))
            result["liveCandidateFrequencyBaseline"] = candidate_baseline(model, live_labels, live_selected, choice["candidateMass"])
            result["targets"] = []
            for i, row in enumerate(live_rows):
                if str(row["matchId"]) in ("498161722", "498168369"):
                    result["targets"].append(dict(matchId=row["matchId"], home=row["home"], guest=row["guest"],
                        actualResult=int(live_labels[i]), sourceAsOf=latest[str(row["matchId"])]["asOf"],
                        market=live_base[i], prediction=live_prediction[i]))
        results[scope] = result
        write(args.out / f"{scope}-model.json", dict(config=config, candidateMass=choice["candidateMass"],
             trainedThroughBjt=cut["calibrationThrough"], sourceSHA256=source_sha, metadata=metadata, **model.export()))
        write(args.out / f"{scope}-predictions.json", dict(current=[dict(matchId=r["matchId"], result=int(labels[i]),
             period=next(k for k, mask in periods.items() if mask[i]), selected=bool(scopes[scope][i]), **compact_prediction(prediction[i]))
             for i, r in enumerate(rows)], live=[dict(matchId=r["matchId"], result=int(live_labels[i]),
             selected=bool(live_selected[i]), before22=bool(before22[i]), **compact_prediction(live_prediction[i])) for i, r in enumerate(live_rows)]))
        write(args.out / f"{scope}-traces.json", dict(
            test=[dict(matchId=r["matchId"], trace=prediction[i]["trace"]) for i, r in enumerate(rows)
                  if scopes[scope][i] and periods["test"][i]],
            live=[dict(matchId=r["matchId"], trace=live_prediction[i]["trace"]) for i, r in enumerate(live_rows)]))
        print("RESULT", scope, "config", config["id"], "test", result["test"]["model"], flush=True)
    report = dict(version=protocol["version"], generatedAt=datetime.now(timezone.utc).isoformat(),
        protocol=protocol, protocolSHA256=common.sha(args.protocol), sourceSHA256=source_sha,
        productionModelArtifactHash=source["modelArtifactHash"], featureAudit=feature_audit, liveFeatureAudit=live_feature_audit,
        logicAudit=logic_audit, scriptSHA256=common.sha(Path(__file__)),
        stageFeatures={stage: [m["name"] for m in metadata if group(m) == stage] for stage in STAGES},
        partitions={k: dict(rows=int(mask.sum()), globalSelected=int(np.sum(mask & global_selected)),
            jcSelected=int(np.sum(mask & jc_selected)), earliestBjt=min(days[mask].tolist()), latestBjt=max(days[mask].tolist()))
            for k, mask in periods.items()}, results=results, seconds=time.perf_counter() - start,
        limitations=["Fixed historical snapshot inputs may have been updated after kickoff; time splitting does not certify original pre-match availability.",
            "Sealed LIVE predictions replay the new model on genuine pre-match inputs, not deployed new-model predictions. Outcomes were discussed in prior research.",
            "The original draw-selection gate is fixed; this experiment does not retrain or replace it.",
            "Candidate intersections explain agreement; routing uses learned conditional feature splits, never elimination of current fixtures.",
            "Smoothed historical proportions and the candidate inclusion threshold are not calibrated future probabilities or a guarantee.",
            "Missing selected inputs skip a stage through the same parent reference group, rather than learning a missingness outcome class.",
            "Node statistics used for forecasting contain only train/calibration labels through 2026-10-24 Beijing time.",
            "No database, running service, production model or UI changed."])
    write(args.out / "report.json", report)
    print("COMPLETE", round(report["seconds"], 2), "seconds", flush=True)


if __name__ == "__main__":
    main()
