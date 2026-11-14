#!/usr/bin/env python3
"""Hard serial backup confirmations using previously audited frozen inputs.

The first valid layer locks home/away. Later layers can only confirm or stop;
there is no weighted rescue, direction replacement, or fallback after failure.
"""
from __future__ import annotations

import argparse
import importlib.util
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

loader=importlib.util.spec_from_file_location("difference_audit",Path(__file__).with_name("audit-draw-alternative-difference.py"))
c=importlib.util.module_from_spec(loader)
loader.loader.exec_module(c)


def pipeline_specs():
    return [dict(id=f"{basis}.{phase}.{kind}",basis=basis,phase=phase,kind=kind)
        for basis in ("amount","source_rate") for phase in ("latest","opening")
        for kind in ("absolute_side_min","signed_side_min","absolute_two_small_with_draw")]


def steps(spec):
    a,b=("official_amount.side","sim_amount.side") if spec["basis"]=="amount" else ("official_rate.side","sim_roi.side")
    return [("L1_official_profit", "profit", a), ("L2_simulated_profit", "profit", b),
        ("L3_sporttery_difference", "difference", spec["phase"]+".sporttery."+spec["kind"]),
        ("L4_average_difference", "difference", spec["phase"]+".average."+spec["kind"])]


def serial(pool, spec, profit_rows, difference_rows):
    alive=list(pool);locked={};history=[];traces={e["key"]:[] for e in pool}
    baseline={e["key"]:(e["lowPrice"],"baseline") for e in pool}
    for index,(name,source,rule) in enumerate(steps(spec)):
        old_keys={e["key"] for e in alive};kept=[];removed=[];failures=Counter()
        for e in pool:
            key=e["key"]
            if key not in old_keys:
                traces[key].append(dict(layer=name,state="not_reached"))
                continue
            item=(profit_rows if source=="profit" else difference_rows)[key]["rules"][rule]
            observed=item["direction"]
            if index==0 and observed is not None:
                locked[key]=observed
            passed=observed is not None and observed==locked.get(key)
            if passed:
                kept.append(e);reason="confirmed"
            else:
                removed.append(e)
                reason=item["reason"] if observed is None else "conflicts_with_locked_direction"
                failures[reason]+=1
            traces[key].append(dict(layer=name,rule=rule,state="passed" if passed else "stopped",
                observedDirection=observed,lockedDirection=locked.get(key),reason=reason))
        kept_keys={e["key"] for e in kept}
        assert kept_keys<=old_keys
        forecast={e["key"]:(locked[e["key"]],"serial_confirmed") if e["key"] in kept_keys else (None,"serial_stopped") for e in pool}
        audit=c.evaluate(pool,forecast,baseline)
        known_removed=[e for e in removed if e["key"] in locked]
        removed_hits=sum(locked[e["key"]]==e["result"] for e in known_removed)
        history.append(dict(layer=name,rule=rule,entered=len(alive),remaining=len(kept),retainedShare=len(kept)/len(pool) if pool else None,
            actualDrawsRemaining=sum(e["result"]==1 for e in kept),removed=len(removed),
            removedWouldBeHits=removed_hits,removedWouldBeMisses=len(known_removed)-removed_hits,
            removedWithoutDirection=len(removed)-len(known_removed),removedActualDraws=sum(e["result"]==1 for e in removed),
            failureReasons=dict(failures),metrics=audit))
        alive=kept
    for key,trace in traces.items():
        stopped=False
        for item in trace:
            if stopped:
                assert item["state"]=="not_reached" and "observedDirection" not in item
            if item["state"]=="stopped":
                stopped=True
    return dict(inputMatches=len(pool),baseline=c.metrics(pool,baseline),layers=history,traces=traces,
        finalRows=[dict(key=e["key"],matchId=e["matchId"],home=e["home"],guest=e["guest"],result=e["result"],direction=locked[e["key"]]) for e in alive])


def latest_scopes(ledger, rows):
    latest,cutoff={},{}
    for r in sorted(ledger["allValidEdges"],key=lambda r:(c.moment(r["predictedAt"]),r["predictionId"])):
        latest[str(r["matchId"])]=r
        if r["purchaseDay"] and c.moment(r["predictedAt"])<=datetime.fromisoformat(r["purchaseDay"]+"T22:00:00+08:00"):
            cutoff[str(r["matchId"])]=r
    def candidates(records):
        return [rows["live:"+str(r["predictionId"])] for r in records.values() if r["original"] and r["finished"] and r["jingcaiId"].strip()]
    return candidates(latest),candidates(cutoff)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for key in ("profit-report","profit-predictions","difference-report","difference-predictions","replay","ledger","protocol","out"):
        parser.add_argument("--"+key,type=Path,required=True)
    args=parser.parse_args()
    pr,pp,dr,dp,replay,ledger,protocol=map(c.read,(args.profit_report,args.profit_predictions,args.difference_report,args.difference_predictions,args.replay,args.ledger,args.protocol))
    assert pr["sourceSHA256"]==dr["sourceSHA256"]==replay["studySHA256"]==protocol["sourceSHA256"]
    profit_rows={r["key"]:r for r in pp["entries"]};difference_rows={r["key"]:r for r in dp["entries"]}
    assert len(profit_rows)==len(pp["entries"]) and profit_rows.keys()==difference_rows.keys()
    for key,p in profit_rows.items():
        d=difference_rows[key]
        assert all(p[x]==d[x] for x in ("matchId","result","lowPrice"))
    models={str(r["matchId"]):r for r in replay["items"]}
    current=[r for r in profit_rows.values() if r["key"].startswith("current:")]
    jc=[r for r in current if models[str(r["matchId"])]["productionOriginalSelected"]]
    global_research=[r for r in current if models[str(r["matchId"])]["researchOnlyUnscopedInputAvailable"] and models[str(r["matchId"])]["researchOnlyUnscopedPrediction"]["selected"]]
    live,before22=latest_scopes(ledger,profit_rows)
    scopes=dict(jc_full=jc,jc_recent=[r for r in jc if c.day(r["matchTime"])>"2026-10-24"],
        global_research_full=global_research,jc_sealed_live=live,jc_sealed_before22=before22)
    assert (len(jc),len(global_research),len(live),len(before22))==(416,3360,41,30)
    specs=pipeline_specs();primary="amount.latest.absolute_side_min"
    report=dict(version=protocol["version"],generatedAt=datetime.now(timezone.utc).isoformat(),protocol=protocol,protocolSHA256=c.digest(args.protocol),
        sourceSHA256=protocol["sourceSHA256"],modelArtifactHash=replay["modelArtifactHash"],sourceGeneratedAt=replay["sourceGeneratedAt"],
        dependencies={str(path):c.digest(path) for path in (args.profit_report,args.profit_predictions,args.difference_report,args.difference_predictions)},
        primaryPipeline=primary,pipelineDefinitions=specs,scopes={},targets={},limitations=[
            "The user's clarification specifies serial filtering, not an exact business order; this audit explicitly assumes official profit then simulation then both difference confirmations.",
            "Only the backup is filtered; the original draw selector and its recorded selection are unchanged.",
            "A stopped fixture has no backup, not a newly correct opposite prediction. Remaining and discarded hits are reported at every layer.",
            "Current historical snapshots have updated inputs; sealed LIVE and purchase cutoff subsets are separately reported.",
            "All stages require strict agreement; missing or tied values stop. No model fitting, weighted scores or later rescue.",
            "This is retrospective exploration on previously examined data, not new prospective validation. No production change."])
    for name,pool in scopes.items():
        report["scopes"][name]={}
        for spec in specs:
            result=serial(pool,spec,profit_rows,difference_rows)
            for e in pool:
                if str(e["matchId"]) in ("498161722","498168369"):
                    target=report["targets"].setdefault(e["key"],dict(matchId=e["matchId"],home=e["home"],guest=e["guest"],result=e["result"],pipelines={}))
                    target["pipelines"][spec["id"]]=result["traces"][e["key"]]
            # Store full traces separately for the primary audit, avoid copying all rows into each summary.
            if spec["id"]==primary:
                result["primaryTraces"]=result["traces"]
            del result["traces"]
            report["scopes"][name][spec["id"]]=result
    args.out.mkdir(parents=True,exist_ok=True)
    c.write(args.out/"report.json",report)
    summary={k:v for k,v in report.items() if k!="scopes"}
    summary["scopes"]={name:{pid:{k:v for k,v in value.items() if k not in ("primaryTraces","finalRows")} for pid,value in pipelines.items()} for name,pipelines in report["scopes"].items()}
    c.write(args.out/"summary.json",summary)
    for name,pipelines in report["scopes"].items():
        result=pipelines[primary]
        print("SCOPE",name,"input",result["inputMatches"])
        for layer in result["layers"]:
            v=layer["metrics"]["rule"];b=layer["metrics"]["pairedLowPrice"]
            print(layer["layer"],"remain",layer["remaining"],"hit",v["hits"],"nonDraw",v["nonDrawPredicted"],"single",v["singleAccuracy"],"conditional",v["conditionalNonDrawAccuracy"],"sameBase",b["hits"],b["nonDrawPredicted"],"removedHits",layer["removedWouldBeHits"],"removedMisses",layer["removedWouldBeMisses"])
    for key,target in report["targets"].items():
        if key.startswith("live:"):
            print("TARGET",key,target["matchId"],target["pipelines"][primary])


if __name__=="__main__":
    main()
