#!/usr/bin/env python3
"""Offline fixed-weight win/loss backup study, never a production selector.

Weights are explicit points, not probabilities. Calibration selects among a
small declared set; later periods and sealed inputs are compared on equal IDs.
"""
from __future__ import annotations

import argparse
import importlib.util
import math
from datetime import datetime, timedelta, timezone
from pathlib import Path

loader = importlib.util.spec_from_file_location("profit_audit", Path(__file__).with_name("audit-draw-alternative-profit.py"))
profit = importlib.util.module_from_spec(loader)
loader.loader.exec_module(profit)
c = profit.core
EPS = 1e-9


def clip(value):
    return max(-1., min(1., value))


def observed(value):
    return (0., 0.) if value is None else (float(value), 1.)


def blend(parts):
    return (sum(w * value for w, (value, available) in parts),
            sum(w * available for w, (value, available) in parts))


def choose_smaller(a, b):
    return 0. if abs(a - b) <= EPS else 1. if a < b else -1.


def phase_vote(raw, key, phase):
    block = raw.get(phase) or {}
    values = block.get("average" if key == "odds" else "kelly")
    positive = isinstance(values, list) and len(values) == 3 and all(c.valid(v) and v > (1 if key == "odds" else 0) for v in values)
    return observed(choose_smaller(values[0], values[2]) if positive else None)


def safe_history(raw, kind):
    target = c.moment(raw["matchTime"])
    rows, seen = [], set()
    block = ((raw.get("preMatchHistory") or {}).get(kind) or {})
    for row in block.get("matches", []):
        rid, timestamp = str(row.get("matchId") or ""), row.get("matchTime")
        h, a, venue = row.get("homeScore"), row.get("awayScore"), row.get("venue")
        if not timestamp or rid == str(raw["matchId"]) or venue not in ("home", "away"):
            continue
        if not all(c.valid(v) and 0 <= v <= 30 and int(v) == v for v in (h,a)):
            continue
        kickoff = c.moment(timestamp)
        if kickoff + timedelta(hours=2) >= target:
            continue
        identity = rid or (timestamp, row.get("home"), row.get("guest"))
        if identity in seen:
            continue
        seen.add(identity)
        gf, ga = (h,a) if venue == "home" else (a,h)
        rows.append((kickoff, gf, ga))
    rows.sort(key=lambda r:r[0], reverse=True)
    return rows


def common_components(raw, protocol):
    phases = protocol["phaseWeights"]
    result = {key:blend([(w,phase_vote(raw,key,phase)) for phase,w in phases.items()]) for key in ("odds","kelly")}
    packs = profit.extract(raw)
    for key, source in (("official_profit","official_amount"),("simulated_profit","sim_amount")):
        direction, reason = profit.maximum(packs[source], source, "side", True)
        value = 1. if direction == 0 else -1. if direction == 2 else 0. if reason in ("maximum_profit_tie","no_profitable_direction") else None
        result[key] = observed(value)
    heat = raw.get("homeHeat")
    heat = clip((heat-50)/15) if c.valid(heat) and 0 <= heat <= 100 else None
    a,b = raw.get("asianOpening"), raw.get("asianLine")
    movement = clip((b-a)/.25) if c.valid(a) and c.valid(b) else None
    result["asian"] = blend([(.5,observed(heat)),(.5,observed(movement))])
    home, away = safe_history(raw,"homeRecent")[:5], safe_history(raw,"awayRecent")[:5]
    if len(home) == len(away) == 5:
        win_gap = (sum(gf>ga for _,gf,ga in home)-sum(gf>ga for _,gf,ga in away))/5
        goal_gap = (sum(gf-ga for _,gf,ga in home)-sum(gf-ga for _,gf,ga in away))/5
        recent = .5*win_gap + .5*clip(goal_gap/2)
    else:
        recent = None
    result["recent"] = observed(recent)
    h2h = safe_history(raw,"headToHead")
    if h2h:
        wins, losses = sum(gf>ga for _,gf,ga in h2h), sum(gf<ga for _,gf,ga in h2h)
        h2h_vote = (wins-losses)/len(h2h) * min(len(h2h),5)/5
    else:
        h2h_vote = None
    result["h2h"] = observed(h2h_vote)
    platform_block = ((raw.get("analysisInputs") or {}).get("platform") or {})
    platform = (platform_block.get("platform") or {}).get("outcome")
    result["platform"] = observed({"home":1.,"away":-1.,"draw":0.}.get(platform))
    return result


def difference_vote(values, style):
    if values is None:
        return observed(None)
    if style == "negative_first_abs":
        indices = [i for i in (0,2) if values[i] < -EPS] or [0,2]
        if len(indices) == 1:
            direction = indices[0]
        elif abs(abs(values[0])-abs(values[2])) <= EPS:
            direction = None
        else:
            direction = min(indices,key=lambda i:abs(values[i]))
    else:
        direction, _ = c.minimum_direction(values, style)
    return observed(1. if direction == 0 else -1. if direction == 2 else 0.)


def difference_component(raw, style, protocol):
    return blend([(.5, blend([(w,difference_vote(c.difference(raw,phase,comparison),style))
        for phase,w in protocol["phaseWeights"].items()])) for comparison in ("sporttery","average")])


def score(common, difference, weights, min_available, minimum_margin):
    components = {**common,"differences":difference}
    home = sum(weight*max(components[key][0],0) for key,weight in weights.items())
    away = sum(weight*max(-components[key][0],0) for key,weight in weights.items())
    known = sum(weight*components[key][1] for key,weight in weights.items())
    assert -EPS <= home+away <= known+EPS <= 100+EPS
    if known+EPS < min_available:
        direction, reason = None,"insufficient_available_weight"
    elif abs(home-away) <= EPS:
        direction, reason = None,"weighted_score_tie"
    elif abs(home-away)+EPS < minimum_margin:
        direction, reason = None,"weighted_margin_too_small"
    else:
        direction, reason = (0 if home>away else 2),"available"
    return dict(homePoints=home,awayPoints=away,availableWeight=known,margin=abs(home-away),direction=direction,reason=reason)


def self_audit(protocol, raw_rows):
    assert blend([(.3,observed(1)),(.7,observed(-1))]) == (-.39999999999999997,1.)
    assert blend([(.3,observed(None)),(.7,observed(1))]) == (.7,.7)
    assert difference_vote([-.2,.3,-.05],"negative_first_abs")[0] == -1
    assert difference_vote([-.2,.3,-.05],"signed_side_min")[0] == 1
    assert difference_vote([-.2,.3,.01],"negative_first_abs")[0] == 1
    assert all(sum(weights.values()) == 100 for weights in protocol["profiles"].values())
    class InputOnly(dict):
        allowed={"opening","latest","frontend","profitInputs","homeHeat","asianOpening","asianLine","matchId","matchTime","preMatchHistory","analysisInputs"}
        def get(self,key,default=None):
            assert key in self.allowed,"target field read: "+key
            return super().get(key,default)
        def __getitem__(self,key):
            assert key in self.allowed,"target field read: "+key
            return super().__getitem__(key)
    checked = 0
    for raw in raw_rows:
        safe=InputOnly(raw)
        assert common_components(raw,protocol)==common_components(safe,protocol)
        for style in protocol["differenceStyles"]:
            assert difference_component(raw,style,protocol)==difference_component(safe,style,protocol)
        checked+=1
    return dict(resultBlindCheckedRows=checked,missingWeightsNeverReassigned=True,initialLatestFixed=True,
        normalizedScoresBounded=True,zeroAndTiesHandled=True,h2hIsOptionalNotAGate=True)


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    for arg in ("study","replay","ledger","evidence","protocol","out"):
        parser.add_argument("--"+arg,type=Path,required=True)
    args=parser.parse_args()
    source,replay,ledger,evidence,protocol=map(c.read,(args.study,args.replay,args.ledger,args.evidence,args.protocol))
    assert c.digest(args.study)==protocol["sourceSHA256"]==replay["studySHA256"]
    models={str(r["matchId"]):r for r in replay["items"]}
    labels={str(r["matchId"]):r for r in source["rows"]}
    current=[dict(key="current:"+str(raw["matchId"]),raw=raw,**{k:raw[k] for k in ("matchId","matchTime","jingcaiId","home","guest","result")}) for raw in source["rows"]]
    live,latest_live,before22=c.live_entries(labels,ledger,evidence)
    entries=current+live
    sampled=source["rows"][::max(1,len(current)//17)][:17]+[e["raw"] for e in latest_live[:17]]
    audit=self_audit(protocol,sampled)
    common={e["key"]:common_components(e["raw"],protocol) for e in entries}
    differences={style:{e["key"]:difference_component(e["raw"],style,protocol) for e in entries} for style in protocol["differenceStyles"]}
    base={e["key"]:(c.baseline(e["raw"]),"baseline") for e in entries}
    specs=[];choices={};scores={}
    for profile,weights in protocol["profiles"].items():
        for style in protocol["differenceStyles"]:
            name=profile+"."+style
            scores[name]={e["key"]:score(common[e["key"]],differences[style][e["key"]],weights,protocol["minAvailableWeight"],0) for e in entries}
            for margin in protocol["minimumMarginVariants"]:
                rule=dict(id=name+".gap"+str(margin),profile=profile,style=style,minimumMargin=margin)
                specs.append(rule)
                choices[rule["id"]]={}
                for e in entries:
                    v=scores[name][e["key"]]
                    v=score(common[e["key"]],differences[style][e["key"]],weights,protocol["minAvailableWeight"],margin) if margin else v
                    choices[rule["id"]][e["key"]]=(v["direction"],v["reason"])
    scopes={}
    for name,gate in (("global",lambda mid:models[mid]["researchOnlyUnscopedInputAvailable"] and models[mid]["researchOnlyUnscopedPrediction"]["selected"]),
                      ("jc",lambda mid:models[mid]["productionOriginalSelected"])):
        pool=[e for e in current if gate(str(e["matchId"]))]
        scopes[name+"_full"]=pool
        scopes[name+"_calibration"]=[e for e in pool if protocol["calibrationFromBjt"]<=c.day(e["matchTime"])<=protocol["calibrationThroughBjt"]]
        scopes[name+"_test"]=[e for e in pool if c.day(e["matchTime"])>=protocol["testFromBjt"]]
    scopes["jc_sealed_live"],scopes["jc_sealed_before22"]=latest_live,before22
    scopes["jc_sealed_purchase20261102"]=[e for e in latest_live if e["purchaseDay"]=="20261102"]
    # Choose weights using calibration only, and persist those choices before test metrics.
    calibration={name:{s["id"]:c.evaluate(scopes[name+"_calibration"],choices[s["id"]],base) for s in specs} for name in ("global","jc")}
    selected={}
    for name in ("global","jc"):
        denominator=sum(base[e["key"]][0] is not None for e in scopes[name+"_calibration"])
        eligible=[]
        for i,s in enumerate(specs):
            value=calibration[name][s["id"]];metric=value["pairedRule"]
            if s["minimumMargin"]==0 and metric["predicted"]>=.8*denominator and metric["nonDrawPredicted"]>=(30 if name=="jc" else 100):
                eligible.append((i,s,value))
        eligible.sort(key=lambda x:(-x[2]["pairedRule"]["conditionalNonDrawAccuracy"],x[2]["changedDirection"],-x[2]["pairedRule"]["predicted"],x[0]))
        selected[name]=dict(spec=eligible[0][1],calibration=eligible[0][2]) if eligible else dict(spec=None,reason="insufficient calibration coverage")
    args.out.mkdir(parents=True,exist_ok=True)
    c.write(args.out/"selected-on-calibration.json",dict(protocolSHA256=c.digest(args.protocol),choices=selected))
    report=dict(version=protocol["version"],generatedAt=datetime.now(timezone.utc).isoformat(),sourceGeneratedAt=source["generatedAt"],
        sourceSHA256=c.digest(args.study),protocolSHA256=c.digest(args.protocol),scriptSHA256=c.digest(Path(__file__)),modelArtifactHash=replay["modelArtifactHash"],
        protocol=protocol,rules=specs,audit=audit,selectedOnCalibration=selected,
        scopes={name:dict(baseline=c.metrics(pool,base),rules={s["id"]:c.evaluate(pool,choices[s["id"]],base) for s in specs}) for name,pool in scopes.items()},
        targets={},limitations=[protocol["historyWarning"],"Global is research extrapolation of the fixed JC selector, not a global production ledger.",
            "Source historical inputs often lack capture times; sealed LIVE audit is separately reported and uses pre-kickoff inputs only.",
            "Scores are heuristic weighted points, never calibrated win probabilities. Fixed weights do not establish independent information among correlated components.",
            "Only >=60 points of observed input weight can issue a backup. Missing groups are not reassigned, ties and low margins abstain.",
            "No product selector, database or running service was changed."])
    latest_keys={e["key"] for e in latest_live}
    for e in entries:
        if str(e["matchId"]) in ("498161722","498168369","498184399") and (e["key"].startswith("current:") or e["key"] in latest_keys):
            key=e["key"];report["targets"][key]=dict(metadata={k:v for k,v in e.items() if k!="raw"},baseline=base[key][0],common=common[key],
                differences={style:differences[style][key] for style in protocol["differenceStyles"]},
                scores={s["id"]:{**scores[s["profile"]+"."+s["style"]][key],"direction":choices[s["id"]][key][0],"reason":choices[s["id"]][key][1]} for s in specs})
    assert (len(scopes["global_full"]),len(scopes["jc_full"]),len(latest_live),len(before22))==(3360,416,41,30)
    assert report["scopes"]["jc_sealed_live"]["baseline"]["hits"]==26
    c.write(args.out/"report.json",report)
    c.write(args.out/"components.json",dict(rows=[{**{k:v for k,v in e.items() if k!="raw"},"baseline":base[e["key"]][0],"common":common[e["key"]],
        "differences":{style:differences[style][e["key"]] for style in protocol["differenceStyles"]}} for e in entries]))
    c.write(args.out/"predictions.json",dict(keys=[e["key"] for e in entries],predictions={name:[values[e["key"]][0] for e in entries] for name,values in choices.items()}))
    print("PASSED",len(entries),"rows",len(specs),"fixed weighted variants")
    for name in ("global","jc"):
        print("SELECTED",name,selected[name]["spec"])
        if selected[name]["spec"]:
            rule=selected[name]["spec"]["id"]
            for scope in (name+"_full",name+"_calibration",name+"_test")+( ("jc_sealed_live","jc_sealed_before22") if name=="jc" else () ):
                v=report["scopes"][scope]["rules"][rule];a,b=v["rule"],v["pairedLowPrice"]
                print(scope,rule,"calls",a["predicted"],"hit",a["hits"],"nd",a["nonDrawPredicted"],"conditional",a["conditionalNonDrawAccuracy"],"single",a["singleAccuracy"],"base",b["hits"],b["nonDrawPredicted"],"net",v["netExtraHits"])


if __name__=="__main__":
    main()
