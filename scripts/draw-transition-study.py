#!/usr/bin/env python3
"""Finite sign/movement/profit intersections, offline historical associations.

No weighted score, training, network, database writes or service operations.
All candidates are declared without looking at match results. Earlier 2/3 of
whole kickoff dates select rules; final 1/3 is an internal stability check only.
"""
from __future__ import annotations

import argparse
import csv
import hashlib
import html
import itertools
import json
import math
from collections import Counter
from datetime import datetime, timezone, timedelta
from decimal import Decimal
from pathlib import Path

import numpy as np
from scipy.stats import hypergeom

EPS = 1e-9
PHASES = ("opening", "latest")
OUTCOMES = ("home", "draw", "away")
CN = {"home": "胜", "draw": "平", "away": "负", "opening": "初盘", "latest": "即时", "ds": "体彩差", "da": "均赔差"}
PROFIT_NAMES = {"official": "竞彩源站盈亏率", "sporttery": "竞彩报价本地测算ROI", "william": "威廉报价本地测算ROI", "bet365": "Bet365报价本地测算ROI", "simulation": "非让球模拟ROI", "localAverageOdds": "均赔本地测算ROI"}


def number(v):
    return type(v) in (int, float) and math.isfinite(v)


def triple(v, positive=False):
    return tuple(float(x) for x in v) if isinstance(v, (list, tuple)) and len(v) == 3 and all(number(x) and (not positive or x > 0) for x in v) else None


def direction(v):
    return 1 if v > EPS else -1 if v < -EPS else 0


def unique_min(v):
    if v is None:
        return None
    indexes = [i for i, x in enumerate(v) if abs(x-min(v)) <= EPS]
    return indexes[0] if len(indexes) == 1 else -1


def pair_indexes(v):
    cutoff = sorted(abs(x) for x in v)[1]
    return [i for i, x in enumerate(v) if abs(x) <= cutoff+EPS]


def official_rates(row):
    presence = (row.get("profitInputs") or {}).get("officialProfitRatePresence", [])
    markets = [m for m in (row.get("roiSimulation") or {}).get("markets", []) if m.get("key") == "sporttery" and m.get("simulated") is not True]
    result = [None]*3
    if len(markets) != 1 or markets[0].get("oddsAvailable") is not True:
        return result
    for i, key in enumerate(OUTCOMES):
        found = [x for x in markets[0].get("bookmakerByOutcome", []) if x.get("outcome") == key]
        if len(found) == 1 and len(presence) == 3 and presence[i] is True:
            item = found[0]
            if number(item.get("odds")) and item["odds"] > 0 and number(item.get("officialProfitRate")):
                result[i] = float(item["officialProfitRate"])
    return result


def modeled_rates(market):
    if not isinstance(market, dict) or market.get("oddsAvailable") is not True:
        return None
    result = []
    for key in OUTCOMES:
        found = [x for x in market.get("bookmakerByOutcome", []) if x.get("outcome") == key]
        if len(found) != 1:
            return None
        item = found[0]
        if item.get("available") is not True or not number(item.get("odds")) or item["odds"] <= 0 or not number(item.get("bookmakerProfit")) or not number(item.get("totalStake")) or item["totalStake"] <= 0:
            return None
        # Preserve the source decimal amounts. Dividing binary floats before
        # multiplying can turn exactly 28 into 28.000000000000004 and put a
        # real boundary observation in the wrong strict outside bucket.
        result.append(float(Decimal(str(item["bookmakerProfit"]))*100/Decimal(str(item["totalStake"]))))
    return tuple(result)


def features(row):
    """Inputs only: target result/score/status are deliberately never read."""
    out = {}
    for phase in PHASES:
        raw = row.get(phase) or {}
        for company in ("william", "average", "sporttery"):
            out[f"{phase}.price.{company}"] = triple(raw.get(company), True)
        w = out[f"{phase}.price.william"]
        for source, company in (("ds", "sporttery"), ("da", "average")):
            other = out[f"{phase}.price.{company}"]
            out[f"{phase}.{source}"] = tuple(a-b for a, b in zip(w, other)) if w and other else None
    heat = row.get("homeHeat")
    out["heat"] = float(heat) if number(heat) and 0 <= heat <= 100 else None
    out["hot"] = None if out["heat"] is None else 0 if heat > 65 else 2 if heat < 35 else -1
    for key, value in (("asianOpening", "asianOpening"), ("asianLatest", "asianLine")):
        out[key] = float(row[value]) if number(row.get(value)) else None
    avg = out["latest.price.average"]
    fav = unique_min(avg)
    out["favorite"] = fav
    out["profit.official"] = official_rates(row)
    markets = (row.get("frontend") or {}).get("roiSimulation", {}) or {}
    markets = markets.get("markets", [])
    for source in ("sporttery", "william", "bet365", "simulation"):
        candidates = [m for m in markets if (m.get("simulated") is True and m.get("key") not in ("sportteryRqspf", "sportteryRqspfSim"))] if source == "simulation" else [m for m in markets if m.get("key") == source and m.get("simulated") is not True]
        out[f"profit.{source}"] = modeled_rates(candidates[0]) if len(candidates) == 1 else None
    out["profit.localAverageOdds"] = modeled_rates((row.get("frontend") or {}).get("localMarket"))
    return out


def make_conditions(xs):
    roots, mods = [], []
    n = len(xs)

    def add(target, identity, label, group, getter, predicate):
        values = [getter(x) for x in xs]
        valid = np.array([v is not None for v in values], dtype=bool)
        mask = np.array([v is not None and bool(predicate(v)) for v in values], dtype=bool)
        target.append({"id": identity, "label": label, "group": group, "valid": valid, "mask": mask})

    def signed_shape(v):
        return "".join("-" if x < -EPS else "+" if x > EPS else "0" for x in v)

    for source in ("ds", "da"):
        for phase in PHASES:
            key = f"{phase}.{source}"
            label = CN[phase]+CN[source]
            getter = lambda x, k=key: x[k]
            for shape in map("".join, itertools.product("-+", repeat=3)):
                add(roots, key+".shape."+shape, label+"胜平负符号"+shape, key, getter, lambda v, s=shape: signed_shape(v) == s)
            for count in range(4):
                add(roots, key+f".negatives.{count}", label+f"有{count}个负数", key, getter, lambda v, c=count: sum(x < -EPS for x in v) == c)
            for sign, name in ((-1,"负"),(0,"零"),(1,"正")):
                add(roots, key+f".drawSign.{sign}", label+"平项为"+name, key, getter, lambda v, s=sign: direction(v[1]) == s)
            add(roots, key+".zeroPresent", label+"含零值", key, getter, lambda v: any(abs(x) <= EPS for x in v))
            add(roots, key+".drawInTwo", label+"绝对值最小两项含平（并列保留）", key, getter, lambda v: 1 in pair_indexes(v))
            add(roots, key+".drawAbsMax", label+"平绝对值最大（并列保留）", key, getter, lambda v: abs(v[1]) >= max(abs(x) for x in v)-EPS)
            # Exactly two choices for pair-sign comparisons; boundary ties are separate.
            def pair_sign(v):
                selected = pair_indexes(v)
                return sorted(direction(v[i]) for i in selected) if len(selected) == 2 and 1 in selected else []
            for signs, name in (((-1,-1),"两负含平"),((1,1),"两正含平"),((-1,1),"一负一正含平")):
                add(roots, key+".pair."+name, label+"绝对值最小两项："+name, key, getter, lambda v, s=signs: tuple(pair_sign(v)) == s)
        getter = lambda x, s=source: (x[f"opening.{s}"], x[f"latest.{s}"]) if x[f"opening.{s}"] is not None and x[f"latest.{s}"] is not None else None
        key = "transition."+source
        label = CN[source]+"初→即"
        for move, name in ((-1,"负数减少"),(0,"负数数量不变"),(1,"负数增多")):
            add(roots, key+f".negativeCount.{move}", label+name, key, getter, lambda v, m=move: direction(sum(x < -EPS for x in v[1])-sum(x < -EPS for x in v[0])) == m)
        for before, after, name in ((-1,-1,"平持续负"),(-1,1,"平负转正"),(1,-1,"平正转负"),(1,1,"平持续正")):
            add(roots, key+"."+name, label+name, key, getter, lambda v, a=before, b=after: direction(v[0][1]) == a and direction(v[1][1]) == b)
        for move, name in ((-1,"平差绝对值收窄"),(0,"平差绝对值不变"),(1,"平差绝对值扩大")):
            add(roots, key+"."+name, label+name, key, getter, lambda v, m=move: direction(abs(v[1][1])-abs(v[0][1])) == m)
        for before, after, name in ((True,True,"平始终在两项"),(False,True,"平进入两项"),(True,False,"平退出两项"),(False,False,"平始终不在两项")):
            add(roots, key+"."+name, label+name, key, getter, lambda v, a=before,b=after: (1 in pair_indexes(v[0])) == a and (1 in pair_indexes(v[1])) == b)

    add(mods,"heat.home","亚盘主热>65%","heat",lambda x:x["hot"],lambda v:v==0)
    add(mods,"heat.away","亚盘客热>65%","heat",lambda x:x["hot"],lambda v:v==2)
    add(mods,"heat.neutral","亚盘双方热度均≤65%","heat",lambda x:x["hot"],lambda v:v==-1)
    for aligned, name in ((True,"同向"),(False,"冲突")):
        add(mods,"heat.favorite."+name,"均赔最低价方向与亚盘热方"+name,"heatFavorite",lambda x:(x["hot"],x["favorite"]) if x["hot"] in (0,2) and x["favorite"] in (0,2) else None,lambda v,a=aligned:(v[0]==v[1])==a)
    get_line = lambda x:(x["asianOpening"],x["asianLatest"]) if x["asianOpening"] is not None and x["asianLatest"] is not None else None
    for move,name in ((-1,"主队让球数减少"),(0,"亚盘让球数不变"),(1,"主队让球数增加")):
        add(mods,"asian.delta."+str(move),name,"asianDelta",get_line,lambda v,m=move:direction(v[1]-v[0])==m)
    for move,name in ((-1,"亚盘绝对盘口变浅"),(0,"亚盘绝对盘口不变"),(1,"亚盘绝对盘口变深")):
        add(mods,"asian.depth."+str(move),name,"asianDepth",get_line,lambda v,m=move:direction(abs(v[1])-abs(v[0]))==m)
    add(mods,"asian.cross","主让/主受严格翻转","asianCross",get_line,lambda v:direction(v[0])*direction(v[1])==-1)
    for company in ("average","william"):
        get_prices = lambda x,c=company:(x[f"opening.price.{c}"],x[f"latest.price.{c}"]) if x[f"opening.price.{c}"] is not None and x[f"latest.price.{c}"] is not None else None
        for move,name in ((-1,"平赔下降"),(0,"平赔不变"),(1,"平赔上升")):
            add(mods,company+".drawOdds."+str(move),("均赔" if company=="average" else "威廉")+name,company+"drawOdds",get_prices,lambda v,m=move:direction(v[1][1]-v[0][1])==m)
        for same,name in ((True,"最低赔热门保持"),(False,"最低赔热门换向")):
            add(mods,company+".favorite."+name,("均赔" if company=="average" else "威廉")+"主客"+name,company+"favorite",get_prices,lambda v,a=same: unique_min(v[0]) in (0,2) and unique_min(v[1]) in (0,2) and (unique_min(v[0])==unique_min(v[1]))==a)
    # Profit is a separate current snapshot, never a price difference or a fake opening series.
    for source,label in PROFIT_NAMES.items():
        for target in ("favorite","draw","hot"):
            name={"favorite":"均赔最低价主/客方向","draw":"平项","hot":"亚盘热方"}[target]
            def getter(x,s=source,t=target):
                rates=x[f"profit.{s}"]
                index=1 if t=="draw" else x["favorite"] if t=="favorite" else x["hot"]
                if rates is None or index not in ((1,) if t=="draw" else (0,2)):
                    return None
                return rates[index] if number(rates[index]) else None
            for bucket,bname,predicate in (("loss28","<−28%",lambda v:v < -28), ("lossSmall","[−28%,0)",lambda v:-28 <= v < 0),
                                           ("positiveNormal","[0%,28%]",lambda v:0 <= v <= 28), ("positive28",">+28%",lambda v:v > 28)):
                add(mods,f"profit.{source}.{target}.{bucket}",label+name+bname,"profit."+source+"."+target,getter,predicate)
    assert all(len(c["valid"])==n and not np.any(c["mask"] & ~c["valid"]) for c in roots+mods)
    return roots,mods


def build_candidates(roots,mods):
    candidates,lookup,aliases=[],{},{}
    def add(parts):
        valid=np.logical_and.reduce([p["valid"] for p in parts])
        mask=np.logical_and.reduce([p["mask"] for p in parts])
        if len(parts)>1 and int(mask.sum())<30:
            return
        signature=(np.packbits(valid).tobytes(),np.packbits(mask).tobytes())
        identity=" & ".join(p["id"] for p in parts)
        if signature in lookup:
            aliases.setdefault(lookup[signature],[]).append(identity)
            return
        lookup[signature]=identity
        candidates.append({"id":identity,"label":" ＋ ".join(p["label"] for p in parts),"parts":[p["id"] for p in parts],"kind":"single" if len(parts)==1 else "intersection","valid":valid,"mask":mask})
    for c in roots+mods:
        add([c])
    for a,b in itertools.combinations(roots,2):
        if a["group"] != b["group"]:
            add([a,b])
    for a,b in itertools.product(roots,mods):
        add([a,b])
    # User also requested Asian/price movements with profit, independently of
    # a price-difference condition. This is still a two-condition intersection.
    movements=[m for m in mods if not m["group"].startswith("profit.")]
    profits=[m for m in mods if m["group"].startswith("profit.")]
    for a,b in itertools.product(movements,profits):
        add([a,b])
    return candidates,aliases


def bh(ps):
    order=np.argsort(ps)
    vals=np.minimum.accumulate((np.asarray(ps)[order]*len(ps)/np.arange(1,len(ps)+1))[::-1])[::-1]
    out=np.empty(len(ps));out[order]=np.minimum(vals,1)
    return out


def metric_arrays(valid,mask,y):
    e=valid.sum(axis=1).astype(int);n=mask.sum(axis=1).astype(int)
    d=valid.astype(np.int16)@y.astype(np.int16);tp=mask.astype(np.int16)@y.astype(np.int16)
    p=hypergeom.sf(tp-1,e,d,n)
    p=np.where((n>0)&(e>n),p,1.)
    return e,n,d,tp,p,bh(p)


def metric(e,n,d,tp,total_draws,p=None,q=None):
    rate=tp/n if n else None; base=d/e if e else None
    rest_rate=(d-tp)/(e-n) if e>n else None
    if n:
        z=1.959963984540054;den=1+z*z/n;c=(rate+z*z/(2*n))/den;r=z*math.sqrt(rate*(1-rate)/n+z*z/(4*n*n))/den
        ci=[max(0,c-r),min(1,c+r)]
    else:ci=None
    return {"eligible":int(e),"eligibleDraws":int(d),"n":int(n),"draws":int(tp),"falseSelections":int(n-tp),"precision":rate,"drawCoverage":tp/total_draws if total_draws else None,
            "eligibleDrawCoverage":tp/d if d else None,"baseline":base,"complementPrecision":rest_rate,"liftPP":(rate-base)*100 if n and e else None,
            "complementLiftPP":(rate-rest_rate)*100 if n and rest_rate is not None else None,"wilson95":ci,"p":float(p) if p is not None else None,"q":float(q) if q is not None else None}


def date_key(row):
    value=datetime.fromisoformat(row["matchTime"].replace("Z","+00:00"))
    if value.tzinfo is None:value=value.replace(tzinfo=timezone(timedelta(hours=8)))
    return value.astimezone(timezone(timedelta(hours=8))).date().isoformat()


def time_groups(rows):
    dates=np.array([date_key(r) for r in rows]);unique,counts=np.unique(dates,return_counts=True)
    cumulative=np.cumsum(counts)
    cuts=[unique[min(int(np.searchsorted(cumulative,len(rows)*f)),len(unique)-1)] for f in (1/3,2/3)]
    groups=np.where(dates<=cuts[0],0,np.where(dates<=cuts[1],1,2))
    return dates,groups,cuts


def shuffle_search(valid,mask,y,groups,repetitions,minimum):
    keep=(mask.sum(axis=1)>=minimum)&((valid.sum(axis=1)-mask.sum(axis=1))>=minimum)
    if not np.any(keep):return {"rules":0,"repetitions":repetitions}
    v=valid[keep].astype(np.float32);m=mask[keep].astype(np.float32)
    e=v.sum(axis=1);n=m.sum(axis=1)
    rng=np.random.default_rng(20261027)
    yy=np.empty((len(y),repetitions+1),dtype=np.float32);yy[:,0]=y
    for iteration in range(1,repetitions+1):
        yy[:,iteration]=y
        for group in np.unique(groups):
            indexes=np.flatnonzero(groups==group);yy[indexes,iteration]=rng.permutation(y[indexes])
    d=v@yy;tp=m@yy;prob=d/e[:,None]
    sd=np.sqrt(np.maximum(0,n[:,None]*prob*(1-prob)*(e[:,None]-n[:,None])/np.maximum(1,e[:,None]-1)))
    zz=np.divide(tp-n[:,None]*prob,sd,out=np.zeros_like(tp),where=sd>0)
    maxima=zz.max(axis=0)
    observed=float(maxima[0]);randoms=maxima[1:]
    return {"rules":int(keep.sum()),"repetitions":repetitions,"scope":"repeat max-enrichment search over every support-qualified fixed candidate, labels permuted within whole-time thirds",
            "observedMaxZ":observed,"randomMaxZMedian":float(np.median(randoms)),"randomMaxZ95":float(np.quantile(randoms,.95)),
            "familywiseEmpiricalP":float((1+sum(randoms>=observed))/(repetitions+1)),"randomMaxZ":randoms.tolist()}


def write_json(path,value):path.write_text(json.dumps(value,ensure_ascii=False,indent=2,allow_nan=False),encoding="utf-8")


def protocol():
    return {"version":"draw-transition-v1","scope":"all4161, sportteryID511, nonSportteryID3650; no service/database IO","formula":"ds=William-Sporttery, da=William-average; opening/latest unrounded triples; sign epsilon1e-9; raw zero retained",
            "roots":"8 nonzero full sign shapes, negative count0..3, draw sign3, zero present, smallest-absolute-two contains draw, draw absolute max, exactly-two NN/PP/NP including draw; plus initial/latest negative-count/draw-sign/absolute-draw-gap/two-option changes",
            "modifiers":"Asian heat >65 and <35, favorite/heat alignment, signed and absolute Asian line movement/crossing; average and William draw-price movement/favorite changes; real official and modeled company/simulation/local profit quartiles for average-price favorite, draw and hot direction",
            "profitBoundaries":"Separate strict <−28, [−28,0), [0,28], strict >+28 percent; never treat simulated ROI as source official rate. User 28 sign pending: both sides reported, not optimized.",
            "profitArithmetic":"ROI=Decimal(source amount)*100/Decimal(source stake), converted to float after division; no display rounding and no binary-division false crossing of exact ±28 boundaries.",
            "candidateLimit":"singles, two root conditions from different input groups, root+one context/profit modifier, and Asian/odds/heat movement+profit without requiring a difference. No triples or OR cover optimization. Intersections need >=30 input-only occurrences globally. Identical validity+selection masks deduplicated with aliases retained.",
            "catalogAddendum":"After first pair run, added movement/heat+profit two-condition intersections to cover user's request independently of price differences. No thresholds or atomic definitions changed; all added candidates retained and joint BH/shuffle repeated.",
            "selection":"earliest two whole kickoff-date thirds select, final third internal check. Dev support>=60 all/nonSporttery or>=30 Sporttery AND unselected>=same. Rank by dev BHq then complement lift and n. No audit outcome selection.",
            "reporting":"all rules retained CSV, same-validity baseline/complement, draw coverage versus all cohort draws, Wilson intervals, separate time thirds, and same-sample incremental parent contrast. Price-only favorite-stays/flips validity includes price-complete tie/draw-favorite rows in complement, never mislabels these as the opposite movement. Favorable-looking small cells not promoted.",
            "controls":"BH adjustment across entire deduplicated candidate family per cohort/development;200 within-time-third permutations repeat max-search statistic across full support-qualified family.",
            "limitations":["All this history has been repeatedly inspected; final third is internal stability, not independent future validation.","Source odds capture not proven pre-kickoff. Profit is one current snapshot, no fake opening profit.","Sign and low-price/heat/profit measures share inputs: intersections are not independent evidence.","28% is a user hypothesis; bookmaker loss tolerance/intent is not established by these fields.","No production model/UI changes in this research run."]}


def main():
    parser=argparse.ArgumentParser();parser.add_argument("--dataset",type=Path,default=Path("reports/draw-full-feature-study/dataset.json"));parser.add_argument("--out",type=Path,default=Path("reports/draw-transition-study"));parser.add_argument("--permutations",type=int,default=200)
    args=parser.parse_args();args.out.mkdir(parents=True,exist_ok=True)
    write_json(args.out/"protocol.json",protocol()) # before any labels loaded/evaluated
    raw=args.dataset.read_bytes();rows=json.loads(raw)["rows"]
    xs=[features(r) for r in rows];roots,mods=make_conditions(xs);candidates,aliases=build_candidates(roots,mods)
    write_json(args.out/"catalog.json",{"roots":[{k:v for k,v in c.items() if k not in ("valid","mask")} for c in roots],"modifiers":[{k:v for k,v in c.items() if k not in ("valid","mask")} for c in mods],"candidates":[{k:v for k,v in c.items() if k not in ("valid","mask")} for c in candidates],"aliases":aliases})
    print(json.dumps({"stage":"catalog_frozen","roots":len(roots),"modifiers":len(mods),"candidates":len(candidates)},ensure_ascii=False),flush=True)
    valid=np.stack([c["valid"] for c in candidates]);mask=np.stack([c["mask"] for c in candidates]);y=np.array([r["result"]==1 for r in rows],dtype=np.int16)
    dates,groups,cuts=time_groups(rows);marked=np.array([bool(str(r.get("jingcaiId") or "").strip()) for r in rows])
    result={"version":"draw-transition-v1","datasetSha256":hashlib.sha256(raw).hexdigest(),"candidateCount":len(candidates),"rootCount":len(roots),"modifierCount":len(mods),"timeCuts":cuts,"cohorts":{}}
    all_metrics=[];all_by_id={};catalog_by_id={c["id"]:c for c in roots+mods}
    for cohort,indexes in (("all",np.arange(len(rows))),("sporttery",np.flatnonzero(marked)),("nonSporttery",np.flatnonzero(~marked))):
        yy=y[indexes];gg=groups[indexes];v=valid[:,indexes];m=mask[:,indexes];total_draws=int(yy.sum());minimum=30 if cohort=="sporttery" else 60
        arrays={"all":metric_arrays(v,m,yy)}
        for group in range(3):arrays[f"period{group+1}"]=metric_arrays(v[:,gg==group],m[:,gg==group],yy[gg==group])
        dev=gg<2;arrays["development"]=metric_arrays(v[:,dev],m[:,dev],yy[dev])
        records=[]
        for i,candidate in enumerate(candidates):
            rec={k:candidate[k] for k in ("id","label","parts","kind")};rec["metrics"]={}
            for split,values in arrays.items():
                e,n,d,tp,p,q=(a[i] for a in values);total=total_draws if split=="all" else int(yy[dev].sum()) if split=="development" else int(yy[gg==int(split[-1])-1].sum())
                rec["metrics"][split]=metric(e,n,d,tp,total,p,q)
                all_metrics.append({"cohort":cohort,"rule":rec["id"],"label":rec["label"],"split":split,**{k:value for k,value in rec["metrics"][split].items() if k!="wilson95"}})
            # Parent same-validity comparisons: compute on child's complete inputs.
            rec["parents"]=[]
            if len(candidate["parts"])==2:
                for part in candidate["parts"]:
                    parent_mask=catalog_by_id[part]["mask"][indexes]&v[i]
                    n=int(parent_mask.sum());tp=int((parent_mask&yy.astype(bool)).sum());child=rec["metrics"]["all"]
                    rec["parents"].append({"id":part,"label":catalog_by_id[part]["label"],"sameInputsMatches":n,"sameInputsDraws":tp,"sameInputsPrecision":tp/n if n else None,
                                           "childIncrementPP":(child["precision"]-tp/n)*100 if child["n"] and n else None,
                                           "removedMatches":n-child["n"],"removedDraws":tp-child["draws"],"removedFalseSelections":n-tp-child["falseSelections"]})
            records.append(rec)
        qualify=lambda r:r["metrics"]["development"]["n"]>=minimum and r["metrics"]["development"]["eligible"]-r["metrics"]["development"]["n"]>=minimum
        qualified=[r for r in records if qualify(r)]
        qualified.sort(key=lambda r:(r["metrics"]["development"]["q"],-r["metrics"]["development"]["complementLiftPP"],-r["metrics"]["development"]["n"],r["id"]))
        top=qualified[:20]
        singles=[r for r in qualified if r["kind"]=="single"][:12]
        bh_supported=[r for r in qualified if r["metrics"]["development"]["q"]<=.05 and r["metrics"]["development"]["complementLiftPP"]>0]
        # Descriptive full-history tables are explicitly not the selection list.
        full_supported=[r for r in records if r["metrics"]["all"]["n"]>=minimum and r["metrics"]["all"]["complementPrecision"] is not None]
        full_supported.sort(key=lambda r:(-r["metrics"]["all"]["precision"],-r["metrics"]["all"]["draws"],r["id"]))
        # Four mutually exclusive cells for displayed intersections. This lets
        # readers distinguish a useful second filter from a separate signal,
        # rather than calling every pair a proven interaction.
        for rec in {r["id"]:r for r in top+singles+full_supported[:20]}.values():
            if len(rec["parts"])!=2:continue
            c=candidates[next(i for i,c in enumerate(candidates) if c["id"]==rec["id"])]
            a,b=(catalog_by_id[part]["mask"][indexes] for part in rec["parts"])
            complete=c["valid"][indexes]
            cells={}
            for split,subset in (("all",np.ones(len(indexes),dtype=bool)),("development",gg<2),("period3",gg==2)):
                selected=complete&subset;values={}
                for key,cell in (("both",a&b),("aOnly",a&~b),("bOnly",~a&b),("neither",~a&~b)):
                    mm=selected&cell;nn=int(mm.sum());tt=int(yy[mm].sum())
                    values[key]={"n":nn,"draws":tt,"precision":tt/nn if nn else None}
                ps=[values[key]["precision"] for key in ("both","aOnly","bOnly","neither")]
                values["additiveInteractionPP"]=(ps[0]-ps[1]-ps[2]+ps[3])*100 if all(p is not None for p in ps) else None
                cells[split]=values
            rec["fourCells"]=cells
        summary={"matches":len(indexes),"draws":total_draws,"drawRate":total_draws/len(indexes),"periods":[{"period":i+1,"matches":int(sum(gg==i)),"draws":int(yy[gg==i].sum()),"firstDate":str(min(dates[indexes][gg==i])),"lastDate":str(max(dates[indexes][gg==i]))} for i in range(3)],"minimumSupport":minimum,"devEligibleRules":len(qualified),"devBHSupportedRules":len(bh_supported),"topDevelopment":top,"topSinglesDevelopment":singles,"fullHistoryHighestPrecisionDescriptive":full_supported[:20],"shuffle":shuffle_search(v,m,yy,gg,args.permutations,minimum)}
        result["cohorts"][cohort]=summary;all_by_id[cohort]={r["id"]:r for r in records}
        write_json(args.out/f"{cohort}-shortlist.json",summary)
        print(json.dumps({"stage":cohort,"matches":len(indexes),"draws":total_draws,"devBH":len(bh_supported),"bestDev":top[0] if top else None,"shuffle":{k:value for k,value in summary["shuffle"].items() if k!="randomMaxZ"}},ensure_ascii=False),flush=True)
    write_json(args.out/"analysis.json",result)
    with (args.out/"rules.csv").open("w",encoding="utf-8-sig",newline="") as f:
        writer=csv.DictWriter(f,fieldnames=list(all_metrics[0]));writer.writeheader();writer.writerows(all_metrics)
    audit=[]
    for row,x in zip(rows,xs):
        audit.append({"matchId":row["matchId"],"jingcaiId":row.get("jingcaiId"),"kickoffDate":date_key(row),"result":row["result"],"inputs":x})
    write_json(args.out/"inputs.json",audit)
    # Full sign-shape transitions, including sparse/zero cells, are descriptive only.
    cells=[]
    for cohort,indexes in (("all",range(len(rows))),("sporttery",np.flatnonzero(marked)),("nonSporttery",np.flatnonzero(~marked))):
        for source in ("ds","da"):
            bins={}
            for i in indexes:
                a,b=xs[i][f"opening.{source}"],xs[i][f"latest.{source}"]
                if a is None or b is None:continue
                shape=lambda z:"".join("-" if v < -EPS else "+" if v > EPS else "0" for v in z)
                bins.setdefault((shape(a),shape(b)),[]).append(i)
            for (a,b),ii in sorted(bins.items()):cells.append({"cohort":cohort,"source":source,"opening":a,"latest":b,"matches":len(ii),"draws":int(y[ii].sum()),"precision":float(y[ii].mean())})
    write_json(args.out/"sign-transitions.json",cells)
    # Label independence regression: perturb target fields and require identical extraction.
    mutations=0
    for r,x in zip(rows,xs):
        changed={**r,"result":(r["result"]+1)%3,"homeScore":99,"awayScore":98,"status":-999}
        assert features(changed)==x;mutations+=1
    write_json(args.out/"extraction-review.json",{"status":"passed","targetMutationCases":mutations,"rows":len(rows),"roots":len(roots),"modifiers":len(mods),"candidates":len(candidates),"issues":[]})
    print(json.dumps({"stage":"completed","out":str(args.out),"targetMutationCases":mutations}),flush=True)


if __name__=="__main__":main()
