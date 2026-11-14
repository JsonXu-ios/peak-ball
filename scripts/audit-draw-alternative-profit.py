#!/usr/bin/env python3
"""Offline maximum-bookmaker-profit backup audit, never a production selector.

Amounts are the page's local calculations; source rates and simulated ROI are
kept distinct. All directions are determined without reading target outcomes.
"""
from __future__ import annotations

import argparse
import importlib.util
import math
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

spec = importlib.util.spec_from_file_location("difference_audit", Path(__file__).with_name("audit-draw-alternative-difference.py"))
core = importlib.util.module_from_spec(spec)
spec.loader.exec_module(core)

SIDES = ("home", "draw", "away")
AMOUNT_EPS, RATE_EPS = 1e-6, 1e-9


def extract(raw):
    frontend = raw.get("frontend") or {}
    markets = (frontend.get("roiSimulation") or {}).get("markets") or []
    official = [m for m in markets if m.get("key") == "sporttery" and m.get("simulated") is not True]
    simulated = [m for m in markets if m.get("simulated") is True and m.get("key") not in ("sportteryRqspf", "sportteryRqspfSim")]
    flags = (raw.get("profitInputs") or {}).get("officialProfitRatePresence")
    sources = (raw.get("profitInputs") or {}).get("distributionSources") or {}
    output = {}
    for group, candidates in (("official", official), ("sim", simulated)):
        market = candidates[0] if len(candidates) == 1 else {}
        outcomes = market.get("bookmakerByOutcome") or []
        amounts, rates, bases = [], [], []
        for i, side in enumerate(SIDES):
            rows = [r for r in outcomes if r.get("outcome") == side]
            row = rows[0] if len(rows) == 1 else {}
            priced = market.get("oddsAvailable") is True and core.valid(row.get("odds")) and row["odds"] > 0
            available = priced and row.get("available") is True
            amount = row.get("bookmakerProfit") if available and core.valid(row.get("bookmakerProfit")) else None
            base = row.get("totalStake") if available and core.valid(row.get("totalStake")) and row["totalStake"] > 0 else None
            if group == "official":
                source_present = isinstance(flags,list) and len(flags) == 3 and flags[i] is True
                rate = row.get("officialProfitRate") if priced and source_present and core.valid(row.get("officialProfitRate")) else None
            else:
                rate = row.get("bookmakerRoi") if available and core.valid(row.get("bookmakerRoi")) else None
            amounts.append(amount)
            rates.append(rate)
            bases.append(base)
        meta = dict(key=market.get("key"), marketCount=len(candidates), distributionSource=sources.get(market.get("key")), bases=bases)
        output[group + "_amount"] = dict(values=amounts, **meta)
        output["official_rate" if group == "official" else "sim_roi"] = dict(values=rates, **meta)
    return output


def comparable_bases(pack, indices):
    b = [pack["bases"][i] for i in indices]
    return bool(all(core.valid(v) and v > 0 for v in b) and max(b)-min(b) <= AMOUNT_EPS)


def maximum(pack, source, mode, positive=True):
    indices = (0, 2) if mode == "side" else (0, 1, 2)
    if pack["marketCount"] != 1:
        return None, "missing_or_ambiguous_market"
    values = pack["values"]
    if not all(core.valid(values[i]) for i in indices):
        return None, "missing_outcome_profit"
    if source.endswith("amount") and not comparable_bases(pack, indices):
        return None, "inconsistent_stake_base"
    eps = AMOUNT_EPS if source.endswith("amount") else RATE_EPS
    biggest = max(values[i] for i in indices)
    if positive and biggest <= eps:
        return None, "no_profitable_direction"
    winners = [i for i in indices if values[i] >= biggest - eps]
    if len(winners) != 1:
        return None, "maximum_profit_tie"
    return (None, "maximum_profit_is_draw") if winners[0] == 1 else (winners[0], "available")


def rules():
    specs = []
    for source in ("official_amount", "official_rate", "sim_amount", "sim_roi"):
        for mode in ("side", "all_three"):
            specs.append(dict(id=source + "." + mode, kind="single", source=source, mode=mode, positive=True))
    for units, inputs in (("amount", ("official_amount", "sim_amount")), ("rate", ("official_rate", "sim_roi"))):
        for kind in ("agreement", "cross_maximum"):
            for mode in ("side", "all_three"):
                specs.append(dict(id=f"{units}.{kind}.{mode}", kind=kind, inputs=inputs, units=units, mode=mode, positive=True))
    for source in ("official_amount", "sim_amount"):
        specs.append(dict(id=source + ".side_even_if_loss", kind="single", source=source, mode="side", positive=False))
    return specs


def recommend(packs, rule):
    if rule["kind"] == "single":
        return maximum(packs[rule["source"]], rule["source"], rule["mode"], rule["positive"])
    if rule["kind"] == "agreement":
        choices = [maximum(packs[source], source, rule["mode"], True) for source in rule["inputs"]]
        if any(v is None for v, _ in choices):
            return None, "agreement_source_unavailable"
        return (choices[0][0], "available") if choices[0][0] == choices[1][0] else (None, "profit_maxima_disagree")
    indices = (0, 2) if rule["mode"] == "side" else (0, 1, 2)
    a, b = [packs[source] for source in rule["inputs"]]
    if any(p["marketCount"] != 1 or not all(core.valid(p["values"][i]) for i in indices) for p in (a, b)):
        return None, "cross_maximum_source_unavailable"
    if rule["units"] == "amount":
        if not comparable_bases(a,indices) or not comparable_bases(b,indices) or abs(a["bases"][0]-b["bases"][0]) > AMOUNT_EPS:
            return None, "different_stake_bases"
    eps = AMOUNT_EPS if rule["units"] == "amount" else RATE_EPS
    biggest = max(p["values"][i] for p in (a,b) for i in indices)
    if biggest <= eps:
        return None, "no_profitable_direction"
    directions = {i for p in (a,b) for i in indices if p["values"][i] >= biggest - eps}
    if len(directions) != 1:
        return None, "cross_maximum_direction_tie"
    direction = next(iter(directions))
    return (None, "maximum_profit_is_draw") if direction == 1 else (direction, "available")


def self_audit():
    pack = dict(values=[-40,10,5], bases=[100,100,100], marketCount=1)
    assert maximum(pack,"official_amount","side")[0] == 2  # Largest signed profit, never absolute loss.
    assert maximum(pack,"official_amount","all_three")[1] == "maximum_profit_is_draw"
    assert maximum({**pack,"values":[0,-1,2]},"official_amount","side")[0] == 2
    assert maximum({**pack,"values":[0,-1,0]},"official_amount","side")[1] == "no_profitable_direction"
    assert maximum({**pack,"values":[-2,10,-5]},"official_amount","side",False)[0] == 0
    assert maximum({**pack,"values":[5,10,5]},"official_amount","side")[1] == "maximum_profit_tie"
    bad = {"key":"sportteryRqspfSim","simulated":True,"oddsAvailable":True,"bookmakerByOutcome":[]}
    assert extract({"frontend":{"roiSimulation":{"markets":[bad]}}})["sim_amount"]["marketCount"] == 0
    class InputOnly(dict):
        def get(self,key,default=None):
            assert key in ("frontend","profitInputs"), "target field read: " + key
            return super().get(key,default)
    assert extract(InputOnly({})) == extract({"result":2,"homeScore":0,"awayScore":9})
    return dict(signedMaximumNotAbsolute=True, resultBlind=True, zeroRetained=True, tiesAbstain=True,
        handicapSimulationsExcluded=True, sourceMissingNeverBecomesZero=True)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    for arg in ("study","replay","ledger","evidence","protocol","out"):
        p.add_argument("--"+arg,type=Path,required=True)
    args=p.parse_args()
    source,replay,ledger,evidence,protocol=map(core.read,(args.study,args.replay,args.ledger,args.evidence,args.protocol))
    assert core.digest(args.study)==protocol["sourceSHA256"]==replay["studySHA256"]
    self_checks=self_audit()
    by_id={str(x["matchId"]):x for x in source["rows"]}
    models={str(x["matchId"]):x for x in replay["items"]}
    current=[dict(key="current:"+str(raw["matchId"]),raw=raw,**{k:raw[k] for k in ("matchId","matchTime","jingcaiId","home","guest","result")}) for raw in source["rows"]]
    live,latest_live,before22=core.live_entries(by_id,ledger,evidence)
    entries=current+live
    packs={e["key"]:extract(e["raw"]) for e in entries}
    specs=rules()
    choices={s["id"]:{e["key"]:recommend(packs[e["key"]],s) for e in entries} for s in specs}
    baseline={e["key"]:(core.baseline(e["raw"]),"baseline_unavailable") for e in entries}
    scopes={}
    for name,gate in (("global",lambda mid:models[mid]["researchOnlyUnscopedInputAvailable"] and models[mid]["researchOnlyUnscopedPrediction"]["selected"]),
                      ("jc",lambda mid:models[mid]["productionOriginalSelected"])):
        pool=[e for e in current if gate(str(e["matchId"]))]
        scopes[name+"_full"]=pool
        scopes[name+"_test"]=[e for e in pool if core.day(e["matchTime"])>"2026-10-24"]
    scopes["jc_sealed_live"],scopes["jc_sealed_before22"]=latest_live,before22
    scopes["jc_sealed_purchase20261102"]=[e for e in latest_live if e["purchaseDay"]=="20261102"]
    report=dict(version=protocol["version"],generatedAt=datetime.now(timezone.utc).isoformat(),protocol=protocol,
        sourceGeneratedAt=source["generatedAt"],sourceSHA256=core.digest(args.study),protocolSHA256=core.digest(args.protocol),
        modelArtifactHash=replay["modelArtifactHash"],rules=specs,audit=self_checks,
        sourceCounts=dict(matches=len(current),jingcai=sum(bool(e["jingcaiId"].strip()) for e in current)),
        scopes={name:dict(baseline=core.metrics(pool,baseline),rules={s["id"]:core.evaluate(pool,choices[s["id"]],baseline) for s in specs},
            simulationSourceCounts=dict(Counter(packs[e["key"]]["sim_amount"]["key"] or "missing" for e in pool))) for name,pool in scopes.items()},
        targets={},limitations=["Amounts are the page's hypothetical bookmaker payoff calculations; not observed bookmaker accounts or proof the bookmaker controls match outcomes.",
            "Source official rate and simulated ROI are different estimates, so cross-rate maximum is only a labelled sensitivity check.",
            "Historical current-snapshot results can use updated inputs; only sealed LIVE scopes fix observed pre-kickoff inputs.",
            "Coverage and same-ID paired baseline are reported; few filtered correct calls do not establish high overall accuracy.",
            "No product rule, service or database was changed."])
    latest_keys={e["key"] for e in latest_live}
    for e in entries:
        if str(e["matchId"]) in ("498161722","498168369","498184399") and (e["key"].startswith("current:") or e["key"] in latest_keys):
            report["targets"][e["key"]]={**{k:v for k,v in e.items() if k!="raw"},"packs":packs[e["key"]],"lowPrice":baseline[e["key"]][0],
                "rules":{s["id"]:dict(direction=choices[s["id"]][e["key"]][0],reason=choices[s["id"]][e["key"]][1]) for s in specs}}
    # Record where using amount versus the source percentage would change a pick.
    report["audit"]["officialAmountRateDisagreements"]=[]
    for e in current+latest_live:
        a=choices["official_amount.side"][e["key"]][0];b=choices["official_rate.side"][e["key"]][0]
        if a is not None and b is not None and a!=b:
            report["audit"]["officialAmountRateDisagreements"].append(dict(key=e["key"],matchId=e["matchId"],amountDirection=a,sourceRateDirection=b))
    assert (len(scopes["global_full"]),len(scopes["jc_full"]),len(latest_live),len(before22))==(3360,416,41,30)
    assert report["scopes"]["jc_sealed_live"]["baseline"]["hits"]==26
    args.out.mkdir(parents=True,exist_ok=True)
    core.write(args.out/"report.json",report)
    core.write(args.out/"predictions.json",dict(entries=[{**{k:v for k,v in e.items() if k!="raw"},"lowPrice":baseline[e["key"]][0],"packs":packs[e["key"]],
        "rules":{s["id"]:dict(direction=choices[s["id"]][e["key"]][0],reason=choices[s["id"]][e["key"]][1]) for s in specs}} for e in entries]))
    # Small, label-free input for independently checking the actual Vue utility.
    fixture=[]
    fields=("outcome","available","odds","bookmakerProfit","bookmakerRoi","officialProfitRate","totalStake")
    for e in entries:
        roi=(e["raw"].get("frontend") or {}).get("roiSimulation") or {}
        markets=[{**{k:m[k] for k in ("key","simulated","oddsAvailable") if k in m},"bookmakerByOutcome":[{k:x[k] for k in fields if k in x} for x in m.get("bookmakerByOutcome",[])]} for m in roi.get("markets",[])]
        fixture.append(dict(key=e["key"],frontendAnalysis=dict(roiSimulation=dict(totalStake=roi.get("totalStake"),markets=markets)),packs=packs[e["key"]]))
    core.write(args.out/"page-parity-fixture.json",fixture)
    print(core.read(args.protocol)["version"],"rules",len(specs),"source",len(current),"sourceRateAmountDirectionDifferences",len(report["audit"]["officialAmountRateDisagreements"]))
    for scope in ("jc_full","jc_test","jc_sealed_live","jc_sealed_before22"):
        for name in ("official_amount.side","sim_amount.side","amount.agreement.side","amount.cross_maximum.side","official_amount.all_three","sim_amount.all_three","amount.agreement.all_three","amount.cross_maximum.all_three","official_rate.side"):
            v=report["scopes"][scope]["rules"][name];a,b=v["rule"],v["pairedLowPrice"]
            print(scope,name,"calls",a["predicted"],"hits",a["hits"],"nd",a["nonDrawPredicted"],"single",round((a["singleAccuracy"] or 0)*100,2),"conditional",round((a["conditionalNonDrawAccuracy"] or 0)*100,2),"sameBase",b["hits"],b["nonDrawPredicted"],"net",v["netExtraHits"])


if __name__=="__main__":
    main()
