#!/usr/bin/env python3
"""Bounded retrospective draw coverage search; no service or database access.

All outcome labels are deliberately used for historical discovery. This is a
finite heuristic search, not a forecast, validation experiment or optimality proof.
"""
from __future__ import annotations
import argparse
import csv
import hashlib
import importlib.util
import json
import math
from pathlib import Path

import numpy as np

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("historical_value", ROOT / "scripts/draw-historical-value-study.py")
hist = importlib.util.module_from_spec(spec)
spec.loader.exec_module(hist)


def bits(mask):
    return int.from_bytes(np.packbits(mask, bitorder="little").tobytes(), "little")


def mask(value, n):
    return np.unpackbits(np.frombuffer(value.to_bytes((n + 7)//8, "little"), dtype=np.uint8), bitorder="little")[:n].astype(bool)


def rid(all_of):
    return "r_" + hist.ident(sorted(all_of))


def metrics(value, positive, universe):
    d = (value & positive).bit_count()
    n = value.bit_count()
    return {"selected": n, "draw": d, "falseSelections": n-d,
            "precision": d/n if n else None, "recall": d/positive.bit_count(),
            "targetReached": d >= 900 and n > 0 and d/n >= .30}


def allowed(atom):
    name = atom["feature"]
    # Target identifiers are absent from the frozen feature matrix. Keep this
    # explicit so that future exports cannot silently turn them into predicates.
    bad = ("matchid", "matchtime", "leagueid", "teamid", "target.", "label.", "availability.")
    if any(x in name.lower() for x in bad):
        return False
    if name.endswith(".n") or name.endswith(("recentHomeMatches", "recentGuestMatches")):
        return False  # sample counts can only appear as visible guards
    return atom.get("family") != "inputAvailability"


def make_pool(catalog, X, names, y):
    cols = {name: i for i, name in enumerate(names)}
    atoms = {a["id"]: a for a in catalog["atoms"] if allowed(a)}
    am = {aid: bits(hist.predicate(a, X, cols)[0]) for aid, a in atoms.items()}
    positive = bits(y == 1)
    total = (1 << len(y))-1
    candidates = {}

    def add(all_of, value, origin):
        if value == 0 or value in candidates:
            return
        d = (value & positive).bit_count()
        f = value.bit_count()-d
        # A rejection cannot lose over 70 draws, and must remove at least 10
        # non-draws. No missing value becomes a rejection match.
        if d > 70 or f < 10:
            return
        candidates[value] = {"id": rid(all_of), "allOf": sorted(all_of), "bits": value,
                             "draw": d, "false": f, "origin": origin}

    for a, value in am.items():
        add([a], value, "all_original_single_atoms")
    existing = []
    for r in catalog["rules"]:
        if not all(a in atoms for a in r["allOf"]):
            continue
        value = total
        for a in r["allOf"]:
            value &= am[a]
        existing.append({"id": r["id"], "allOf": r["allOf"], "bits": value})
        add(r["allOf"], value, "existing_historical_catalog")
    # Existing interaction seeds emphasized positive draw enrichment. Add a
    # limited low-draw screen, preserving one representative per field first.
    ranks = []
    for aid, value in am.items():
        d, n = (value & positive).bit_count(), value.bit_count()
        if n >= 30:
            ranks.append(((n-d)/(d+1), n-d, -d, aid))
    ranks.sort(reverse=True)
    field, seeds = set(), []
    for _, _, _, aid in ranks:
        name = atoms[aid]["feature"]
        if name not in field:
            seeds.append(aid)
            field.add(name)
        if len(seeds) >= 240:
            break
    seeds = sorted(set(seeds + [r[-1] for r in ranks[:120]]))
    attempts = 0
    for j, a in enumerate(seeds):
        for b in seeds[j+1:]:
            if atoms[a]["feature"] == atoms[b]["feature"]:
                continue
            attempts += 1
            add([a,b], am[a] & am[b], "bounded_low_draw_pair_screen")
    pool = sorted(candidates.values(), key=lambda r:r["id"])
    return atoms, pool, existing, {"atomicCandidates": len(atoms), "existingRulesRecomputed": len(existing),
                                  "lowDrawSeeds": len(seeds), "lowDrawPairsAttempted": attempts,
                                  "uniqueRejectionMasks": len(pool), "maximumLostDrawsPerRule": 70,
                                  "minimumNonDrawsPerRule": 10}


def greedy(pool, positive, universe, max_rules, kind, coefficient):
    removed, chosen, steps = 0, set(), []
    for _ in range(max_rules):
        left = 70-(removed & positive).bit_count()
        best = None
        for r in pool:
            if r["id"] in chosen:
                continue
            delta = r["bits"] & ~removed
            d = (delta & positive).bit_count()
            f = delta.bit_count()-d
            if f <= 0 or d > left:
                continue
            score = f/((d+.5)**coefficient) if kind == "ratio" else f-coefficient*d
            item = (score, f, -d, -len(r["allOf"]), r["id"], r)
            if best is None or item[:5] > best[:5]:
                best = item
        if best is None or best[0] <= 0:
            break
        r = best[-1]
        delta = r["bits"] & ~removed
        removed |= r["bits"]
        chosen.add(r["id"])
        steps.append({"ruleId": r["id"], "removedDraws": (delta&positive).bit_count(),
                      "removedNonDraws": (delta&~positive).bit_count(),
                      "remaining": metrics(universe & ~removed, positive, universe)})
    return removed, steps


def beam(pool, positive, universe, depth=16, width=8, branch=5):
    states = [(0, (), [])]
    best = states[0]
    for _ in range(depth):
        next_states = {}
        for removed, ids, steps in states:
            left = 70-(removed & positive).bit_count()
            choices = []
            for r in pool:
                if r["id"] in ids:
                    continue
                delta = r["bits"] & ~removed
                d = (delta & positive).bit_count()
                f = delta.bit_count()-d
                if f > 0 and d <= left:
                    choices.append((f/(d+.5), f, -d, r["id"], r))
            choices.sort(key=lambda a:a[:4], reverse=True)
            for choice in choices[:branch]:
                _, f, nd, _, r = choice
                new = removed | r["bits"]
                candidate = (new, ids+(r["id"],), steps+[{"ruleId":r["id"], "removedDraws":-nd,
                     "removedNonDraws":f, "remaining":metrics(universe & ~new,positive,universe)}])
                next_states.setdefault(new,candidate)
        if not next_states:
            break
        # Loss-budget-aware score; retained TP is always >=900. Finite heuristic.
        states = sorted(next_states.values(), key=lambda s: (
            (s[0]&~positive).bit_count()-8*(s[0]&positive).bit_count(),
            (s[0]&~positive).bit_count(), -(s[0]&positive).bit_count()), reverse=True)[:width]
        candidate = max(states, key=lambda s:(s[0]&~positive).bit_count())
        if (candidate[0]&~positive).bit_count() > (best[0]&~positive).bit_count():
            best = candidate
    return best[0], best[2]


def positive_or(pool, positive, universe, max_rules, gamma):
    selected, used, steps = 0,set(),[]
    for _ in range(max_rules):
        best=None
        for r in pool:
            if r["id"] in used:
                continue
            delta=r["bits"] & ~selected
            d=(delta&positive).bit_count()
            f=delta.bit_count()-d
            if d<1:
                continue
            score=d/((f+1)**gamma)
            key=(score,d,-f,r["id"])
            if best is None or key>best[0]:
                best=(key,r,d,f)
        if best is None:
            break
        _,r,d,f=best
        selected |= r["bits"]
        used.add(r["id"])
        steps.append({"ruleId":r["id"],"addedDraws":d,"addedNonDraws":f,
                      "remaining":metrics(selected,positive,universe)})
        if (selected&positive).bit_count()>=900:
            break
    return selected,steps


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out",type=Path,default=ROOT/"reports/draw-target-900-study")
    parser.add_argument("--skip-beam",action="store_true")
    args=parser.parse_args()
    args.out.mkdir(parents=True,exist_ok=True)
    old=ROOT/"reports/draw-historical-value-study"
    rows,X,y,meta,sources=hist.load_inputs(old)
    catalog=hist.read(old/"catalog.json")
    atoms,pool,existing,search=make_pool(catalog,X,[m["name"] for m in meta],y)
    positive=bits(y==1)
    universe=(1<<len(y))-1
    print(json.dumps({"stage":"pool",**search}),flush=True)
    results=[]
    selections={}
    for source in ("allRecorded","marketAndSafeHistory"):
        reject=[r for r in pool if source=="allRecorded" or all(atoms[a]["view"]!="snapshotDerived" for a in r["allOf"])]
        for limit in (16,32):
            for kind,coeffs in (("ratio",(.5,.75,1.,1.25)),("penalty",(5.,8.,12.,20.))):
                for coefficient in coeffs:
                    removed,steps=greedy(reject,positive,universe,limit,kind,coefficient)
                    name=f"reject_{source}_{limit}_{kind}_{coefficient:g}"
                    selected=universe&~removed
                    results.append({"id":name,"source":source,"operation":"keep unless any rejection rule matches",
                        "maximumRules":limit,"algorithm":kind,"coefficient":coefficient,"steps":steps,
                        "stats":metrics(selected,positive,universe)})
                    selections[name]=selected
            print(json.dumps({"stage":"greedy", "source":source,"limit":limit,
                              "best":min([r for r in results if r["stats"]["draw"]>=900],key=lambda r:r["stats"]["falseSelections"])["stats"]}),flush=True)
        if not args.skip_beam:
            removed,steps=beam(reject,positive,universe)
            name=f"reject_{source}_16_beam8_branch5"
            selected=universe&~removed
            results.append({"id":name,"source":source,"operation":"keep unless any rejection rule matches",
                            "maximumRules":16,"algorithm":"beam width8 branch5 depth16","steps":steps,
                            "stats":metrics(selected,positive,universe)})
            selections[name]=selected
    for limit in (16,32):
        for gamma in (.25,.5,.75):
            selected,steps=positive_or(existing,positive,universe,limit,gamma)
            name=f"positiveOR_{limit}_{gamma:g}"
            results.append({"id":name,"source":"allRecorded","operation":"select if any positive rule matches",
                            "maximumRules":limit,"algorithm":"positive union greedy","coefficient":gamma,
                            "steps":steps,"stats":metrics(selected,positive,universe)})
            selections[name]=selected
    qualified=[r for r in results if r["stats"]["draw"]>=900]
    best=min(qualified,key=lambda r:(r["stats"]["falseSelections"],-r["stats"]["draw"],len(r["steps"])))
    rule_map={r["id"]:r for r in pool+existing}
    used={step["ruleId"] for result in results for step in result["steps"]}
    exposed=[]
    for rule_id in sorted(used):
        rule=rule_map[rule_id]
        exposed.append({"id":rule_id,"allOf":[atoms[a] for a in rule["allOf"]],
                        "label":" 且 ".join(atoms[a]["label"] for a in rule["allOf"]),
                        "rawMatchStats":metrics(rule["bits"],positive,universe)})
    sources[str((old/"catalog.json").relative_to(ROOT))]=hist.sha(old/"catalog.json")
    sources[str(Path(__file__).relative_to(ROOT))]=hist.sha(Path(__file__))
    output={"purpose":"历史970平局中保留至少900，最小化误选；有限16/32规则启发式",
        "usesAllHistoricalOutcomes":True,"independentValidation":False,"globalOptimumClaimed":False,
        "target":{"minimumDraws":900,"minimumPrecision":.30,"maximumFalseIfDraw900":2100},
        "timing":"盘口采集时间未知，结果不是赛前预测证明；历史字段沿用严格先于目标场2小时护栏。",
        "missing":"主条件和护栏必须有限；缺数据不匹配排除规则，不能按0排除。",
        "complexity":"每条最多2个主要数值条件，历史样本量只作显式护栏；无ID、球队、日期、联赛标识规则。",
        "sources":sources,"features":len(meta),"baseline":metrics(universe,positive,universe),
        "search":search,"bestConfigurationId":best["id"],"best":best["stats"],"configurations":results,"rules":exposed}
    hist.write(args.out/"rules.json",output)
    with (args.out/"rule-selections.csv").open("w",encoding="utf-8-sig",newline="") as f:
        writer=csv.writer(f)
        writer.writerow(["matchId","result"]+[r["id"] for r in results])
        columns=[mask(selections[r["id"]],len(y)) for r in results]
        for i,row in enumerate(rows):
            writer.writerow([row["matchId"],int(y[i])]+[int(m[i]) for m in columns])
    print(json.dumps({"stage":"complete","best":best["id"],"stats":best["stats"],"configurations":len(results),
                      "output":str(args.out/"rules.json")}),flush=True)


if __name__=="__main__":
    main()
