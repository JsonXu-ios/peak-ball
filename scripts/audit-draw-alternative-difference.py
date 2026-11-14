#!/usr/bin/env python3
"""Result-blind minimum-difference backup rules, offline paired audit only.

No model fitting, data collection, database writes or production rule changes.
Missing/tied inputs abstain. Coverage and same-ID accuracy are kept separate.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import math
from collections import Counter
from datetime import datetime, timedelta, timezone
from pathlib import Path

EPS = 1e-9
BJT = timezone(timedelta(hours=8))
KINDS = ("signed_side_min", "absolute_side_min", "absolute_all_min_decisive", "absolute_two_small_with_draw")


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def write(path, value):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False), encoding="utf-8")


def moment(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def day(value):
    return moment(value).astimezone(BJT).date().isoformat()


def valid(value):
    return type(value) in (int, float) and math.isfinite(value)


def prices(value):
    return value if isinstance(value, list) and len(value) == 3 and all(valid(v) and v > 1 for v in value) else None


def difference(raw, phase, comparison):
    block = raw.get(phase) or {}
    w, other = prices(block.get("william")), prices(block.get(comparison))
    return [a - b for a, b in zip(w, other)] if w is not None and other is not None else None


def minimum_direction(values, kind):
    if values is None:
        return None, "missing_same_phase_complete_prices"
    if kind in ("signed_side_min", "absolute_side_min"):
        home, away = values[0], values[2]
        if kind == "absolute_side_min":
            home, away = abs(home), abs(away)
        if abs(home - away) <= EPS:
            return None, "home_away_minimum_tie"
        return (0 if home < away else 2), "available"
    absolute = [abs(v) for v in values]
    if kind == "absolute_all_min_decisive":
        members = [i for i, v in enumerate(absolute) if v <= min(absolute) + EPS]
        if len(members) != 1:
            return None, "three_outcome_minimum_tie"
        return (None, "draw_is_minimum") if members[0] == 1 else (members[0], "available")
    assert kind == "absolute_two_small_with_draw"
    boundary = sorted(absolute)[1]
    members = [i for i, v in enumerate(absolute) if v <= boundary + EPS]
    if len(members) != 2:
        return None, "two_small_boundary_tie"
    if 1 not in members:
        return None, "two_small_pair_excludes_draw"
    return next(i for i in members if i != 1), "available"


def baseline(raw):
    front = raw.get("frontend") or {}
    if "averageEuroOdds" in front:
        values = front["averageEuroOdds"]
    else:
        market = front.get("localMarket") or {}
        odds = market.get("odds") or {}
        values = [odds.get(k) for k in ("home", "draw", "away")] if market.get("key") == "localAverageOdds" and market.get("oddsAvailable") else None
    if not isinstance(values, list) or len(values) != 3 or not all(valid(values[i]) and values[i] > 1 for i in (0, 2)):
        return None
    return None if abs(values[0] - values[2]) <= EPS else 0 if values[0] < values[2] else 2


def rule_specs():
    result = []
    for phase in ("opening", "latest"):
        for comparison in ("sporttery", "average"):
            for kind in KINDS:
                result.append(dict(id=f"{phase}.{comparison}.{kind}", mode="single", phase=phase, comparison=comparison, kind=kind))
    for kind in ("signed_side_min", "absolute_side_min", "absolute_two_small_with_draw"):
        for phase in ("opening", "latest"):
            result.append(dict(id=f"{phase}.comparisons_agree.{kind}", mode="comparisons_agree", phase=phase, kind=kind))
        for comparison in ("sporttery", "average"):
            result.append(dict(id=f"both_phases.{comparison}.{kind}", mode="phases_agree", comparison=comparison, kind=kind))
    return result


def recommend(raw, spec):
    kind = spec["kind"]
    if spec["mode"] == "single":
        return minimum_direction(difference(raw, spec["phase"], spec["comparison"]), kind)
    inputs = [(spec["phase"], c) for c in ("sporttery", "average")] if spec["mode"] == "comparisons_agree" else [(p, spec["comparison"]) for p in ("opening", "latest")]
    choices = [minimum_direction(difference(raw, p, c), kind) for p, c in inputs]
    if any(v is None for v, _ in choices):
        return None, "agreement_input_unavailable"
    return (choices[0][0], "available") if choices[0][0] == choices[1][0] else (None, "directions_disagree")


def wilson(h, n):
    if not n:
        return [None, None]
    z = 1.959963984540054
    p, d = h / n, 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    w = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / d
    return [c - w, c + w]


def metrics(entries, choices):
    available = [e for e in entries if choices[e["key"]][0] is not None]
    non_draw = [e for e in available if e["result"] != 1]
    hits = sum(choices[e["key"]][0] == e["result"] for e in available)
    draws = sum(e["result"] == 1 for e in entries)
    return dict(candidates=len(entries), predicted=len(available), availability=len(available) / len(entries) if entries else None,
        actualDraws=draws, nonDrawPredicted=len(non_draw), hits=hits,
        singleAccuracy=hits / len(available) if available else None,
        conditionalNonDrawAccuracy=hits / len(non_draw) if non_draw else None,
        conditionalWilson95=wilson(hits, len(non_draw)),
        hitsOverAllCandidates=hits / len(entries) if entries else None,
        drawOrBackupHits=draws + hits, drawOrBackupCoverage=(draws + hits) / len(entries) if entries else None,
        abstentionReasons=dict(Counter(choices[e["key"]][1] for e in entries if choices[e["key"]][0] is None)))


def evaluate(entries, choices, base_choices):
    paired = [e for e in entries if choices[e["key"]][0] is not None and base_choices[e["key"]][0] is not None]
    wins = sum(e["result"] != 1 and choices[e["key"]][0] == e["result"] and base_choices[e["key"]][0] != e["result"] for e in paired)
    losses = sum(e["result"] != 1 and choices[e["key"]][0] != e["result"] and base_choices[e["key"]][0] == e["result"] for e in paired)
    return dict(rule=metrics(entries, choices), pairedRule=metrics(paired, choices), pairedLowPrice=metrics(paired, base_choices),
        changedDirection=sum(choices[e["key"]][0] != base_choices[e["key"]][0] for e in paired),
        ruleOnlyCorrect=wins, lowPriceOnlyCorrect=losses, netExtraHits=wins - losses)


def live_entries(source, ledger, evidence):
    raw_by_id = {int(e["prediction_id"]):e for e in evidence["live_edges"]}
    all_entries = []
    for record in ledger["allValidEdges"]:
        edge = raw_by_id[int(record["predictionId"])]
        assert edge["provenance"] == edge["input_provenance"] == "live"
        assert moment(edge["known_by"]) <= moment(edge["as_of"]) < moment(edge["kickoff"])
        assert moment(edge["as_of"]) <= moment(edge["predicted_at"]) < moment(edge["kickoff"])
        assert moment(edge["input_observed_at"]) <= moment(edge["as_of"])
        raw = edge["raw_input"]
        assert str(raw["matchId"]) == str(record["matchId"])
        assert baseline(raw) == record["direction"]
        target = source.get(str(record["matchId"]))
        all_entries.append(dict(key="live:" + str(record["predictionId"]), raw=raw,
            matchId=str(record["matchId"]), matchTime=record["kickoff"], jingcaiId=record["jingcaiId"],
            home=raw["home"], guest=raw["guest"], result=target["result"] if target else None,
            original=record["original"], predictionId=record["predictionId"], predictedAt=record["predictedAt"],
            purchaseDay=record["purchaseDay"], knownBy=record["knownBy"], asOf=record["asOf"]))
    latest, before22 = {}, {}
    for e in sorted(all_entries, key=lambda r:(moment(r["predictedAt"]), r["predictionId"])):
        latest[e["matchId"]] = e
        if e["purchaseDay"] and moment(e["predictedAt"]) <= datetime.fromisoformat(e["purchaseDay"] + "T22:00:00+08:00"):
            before22[e["matchId"]] = e
    chosen = lambda a:[e for e in a.values() if e["original"] and e["jingcaiId"].strip() and e["result"] is not None]
    return all_entries, chosen(latest), chosen(before22)


def self_audit(specs):
    assert minimum_direction([-.3, .2, .05], "signed_side_min")[0] == 0
    assert minimum_direction([-.3, .2, .05], "absolute_side_min")[0] == 2
    assert minimum_direction([.1, 0., -.2], "absolute_all_min_decisive")[1] == "draw_is_minimum"
    assert minimum_direction([.1, 0., -.2], "absolute_two_small_with_draw")[0] == 0
    assert minimum_direction([0., .4, .1], "absolute_two_small_with_draw")[0] is None
    assert minimum_direction([-.1, 0., .1], "absolute_two_small_with_draw")[0] is None
    assert minimum_direction([0., .3, .1], "absolute_side_min")[0] == 0
    assert minimum_direction([0., .3, 5e-10], "absolute_side_min")[0] is None
    class InputOnly(dict):
        forbidden = {"result", "homeScore", "awayScore", "status", "score", "predictionHit"}
        def get(self, key, default=None):
            assert key not in self.forbidden, "target field read: " + key
            return super().get(key, default)
        def __getitem__(self, key):
            assert key not in self.forbidden, "target field read: " + key
            return super().__getitem__(key)
    raw = {"opening":{"william":[2.2,3.1,3.4],"sporttery":[2.5,2.9,3.35],"average":[2.1,3.,3.5]},
           "latest":{"william":[2.3,3.2,3.5],"sporttery":[2.6,3.,3.45],"average":[2.2,3.1,3.6]}}
    for spec in specs:
        assert recommend(InputOnly(raw), spec) == recommend({**raw,"result":2,"homeScore":0,"awayScore":9}, spec)
    assert difference({"latest":{"william":[2,3,4],"average":[2,3,1]}}, "latest", "average") is None
    return dict(resultBlindRules=len(specs), samePhaseOnly=True, zeroRetained=True, tieAbstains=True, missingNeverFilled=True)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for arg in ("study", "replay", "ledger", "evidence", "protocol", "out"):
        p.add_argument("--" + arg, required=True, type=Path)
    args = p.parse_args()
    source, replay, ledger, evidence, protocol = map(read, (args.study,args.replay,args.ledger,args.evidence,args.protocol))
    assert digest(args.study) == replay["studySHA256"] == protocol["sourceSHA256"]
    specs = rule_specs()
    audit = self_audit(specs)
    by_id = {str(r["matchId"]):r for r in source["rows"]}
    models = {str(r["matchId"]):r for r in replay["items"]}
    assert len(by_id) == len(source["rows"]) == len(models)
    entries = []
    stored_diff_disagreements = []
    for raw in source["rows"]:
        assert raw["result"] == (0 if raw["homeScore"] > raw["awayScore"] else 1 if raw["homeScore"] == raw["awayScore"] else 2)
        entries.append(dict(key="current:" + str(raw["matchId"]), raw=raw, **{k:raw[k] for k in ("matchId","matchTime","jingcaiId","home","guest","result")}))
        for phase in ("opening", "latest"):
            for comparison, stored in (("sporttery","sportteryDiff"),("average","averageDiff")):
                calculated, old = difference(raw, phase, comparison), (raw.get(phase) or {}).get(stored)
                if calculated is not None and isinstance(old,list) and len(old)==3:
                    if any(abs(a-b)>EPS for a,b in zip(calculated,old)):
                        stored_diff_disagreements.append(dict(matchId=raw["matchId"], phase=phase, comparison=comparison, calculated=calculated, stored=old))
    all_live, latest_live, cutoff_live = live_entries(by_id, ledger, evidence)
    all_entries = entries + all_live
    base_choices = {e["key"]:(baseline(e["raw"]), "low_price_unavailable" if baseline(e["raw"]) is None else "available") for e in all_entries}
    choices = {s["id"]:{e["key"]:recommend(e["raw"],s) for e in all_entries} for s in specs}
    global_gate = lambda e:models[str(e["matchId"])]["researchOnlyUnscopedInputAvailable"] and models[str(e["matchId"])]["researchOnlyUnscopedPrediction"]["selected"]
    jc_gate = lambda e:models[str(e["matchId"])]["productionOriginalSelected"]
    scopes = {}
    for name, gate in (("global",global_gate),("jc",jc_gate)):
        pool = [e for e in entries if gate(e)]
        scopes[name + "_full"] = pool
        scopes[name + "_calibration"] = [e for e in pool if "2026-10-16" < day(e["matchTime"]) <= "2026-10-24"]
        scopes[name + "_test"] = [e for e in pool if day(e["matchTime"]) > "2026-10-24"]
    scopes["jc_sealed_live"] = latest_live
    scopes["jc_sealed_before22"] = cutoff_live
    scopes["jc_sealed_purchase20261102"] = [e for e in latest_live if e["purchaseDay"]=="20261102"]
    report = dict(version=protocol["version"], generatedAt=datetime.now(timezone.utc).isoformat(), sourceGeneratedAt=source["generatedAt"],
        sourceSHA256=digest(args.study), replaySHA256=digest(args.replay), protocolSHA256=digest(args.protocol),
        modelArtifactHash=replay["modelArtifactHash"], protocol=protocol, rules=specs,
        audit={**audit,"storedDifferenceDisagreements":stored_diff_disagreements}, sourceMatches=len(entries), sourceJc=sum(bool(e["jingcaiId"].strip()) for e in entries),
        scopes={name:dict(baseline=metrics(pool,base_choices),rules={s["id"]:evaluate(pool,choices[s["id"]],base_choices) for s in specs}) for name,pool in scopes.items()},
        targets={}, limitations=["Current snapshot full/calibration/test results use updated historical inputs whose capture timing is often unknown.",
            "Only sealed LIVE scopes use observed pre-match raw inputs; no final outcome enters direction rules.",
            "Global scope is research extrapolation of the fixed selector, not a deployed global prediction ledger.",
            "Rules which abstain must be compared with low price on exactly the same eligible fixtures. High precision on very few calls is not broad coverage.",
            "All 28 fixed rule variants are reported; no result-based winner was deployed or claimed prospectively validated."])
    for e in all_entries:
        if str(e["matchId"]) in ("498161722","498168369","498184399") and (e in entries or e in latest_live):
            report["targets"][e["key"]] = {k:v for k,v in e.items() if k!="raw"}
            report["targets"][e["key"]].update(phaseInputs={phase:{comp:difference(e["raw"],phase,comp) for comp in ("sporttery","average")} for phase in ("opening","latest")},
                lowPrice=base_choices[e["key"]][0], directions={s["id"]:dict(direction=choices[s["id"]][e["key"]][0],reason=choices[s["id"]][e["key"]][1]) for s in specs})
    assert (len(scopes["global_full"]),len(scopes["jc_full"]),len(latest_live),len(cutoff_live)) == (3360,416,41,30)
    assert report["scopes"]["jc_sealed_live"]["baseline"]["hits"] == 26
    assert report["scopes"]["jc_sealed_before22"]["baseline"]["hits"] == 16
    args.out.mkdir(parents=True,exist_ok=True)
    write(args.out/"report.json",report)
    write(args.out/"predictions.json",dict(entries=[{**{k:v for k,v in e.items() if k!="raw"},"lowPrice":base_choices[e["key"]][0],
        "rules":{s["id"]:dict(direction=choices[s["id"]][e["key"]][0],reason=choices[s["id"]][e["key"]][1]) for s in specs}} for e in all_entries]))
    print(json.dumps({"status":"passed","sourceMatches":len(entries),"rules":len(specs),"scopes":{k:len(v) for k,v in scopes.items()},"storedDifferenceDisagreements":len(stored_diff_disagreements),"out":str(args.out)},ensure_ascii=True))
    for scope in ("jc_full","jc_test","jc_sealed_live","jc_sealed_before22"):
        for s in specs:
            if s.get("phase") != "latest":
                continue
            if s["mode"]!="single":
                continue
            v=report["scopes"][scope]["rules"][s["id"]]
            print(json.dumps(dict(scope=scope,rule=s["id"],predicted=v["rule"]["predicted"],hits=v["rule"]["hits"],nonDraw=v["rule"]["nonDrawPredicted"],single=v["rule"]["singleAccuracy"],conditional=v["rule"]["conditionalNonDrawAccuracy"],pairedBaseline=v["pairedLowPrice"]["conditionalNonDrawAccuracy"],net=v["netExtraHits"]),ensure_ascii=True))


if __name__ == "__main__":
    main()
