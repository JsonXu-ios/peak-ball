#!/usr/bin/env python3
"""Re-evaluate saved rules with independent NumPy masks, without searching."""
import csv
import hashlib
import importlib.util
import json
from pathlib import Path

import numpy as np

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/"reports/draw-target-900-study"
spec=importlib.util.spec_from_file_location("historical_load",ROOT/"scripts/draw-historical-value-study.py")
loader=importlib.util.module_from_spec(spec)
spec.loader.exec_module(loader)
rows,X,y,metadata,_=loader.load_inputs(ROOT/"reports/draw-historical-value-study")
saved=json.loads((OUT/"rules.json").read_text(encoding="utf8"))
cols={m["name"]:i for i,m in enumerate(metadata)}


def predicate(p):
    v=X[:,cols[p["feature"]]]
    if p.get("transform")=="abs":
        v=np.abs(v)
    t=p["value"]
    compare={"le":np.less_equal,"ge":np.greater_equal,"lt":np.less,"gt":np.greater,"eq":np.equal}[p["op"]]
    result=np.isfinite(v)&compare(v,t)
    for guard in p.get("guards",[]):
        result &= predicate(guard)
    return result


def check_stats(m,expected):
    n,d=int(m.sum()),int(np.sum(m&(y==1)))
    assert expected["selected"]==n
    assert expected["draw"]==d
    assert expected["falseSelections"]==n-d
    if n:
        assert abs(expected["precision"]-d/n)<1e-12
    assert abs(expected["recall"]-d/int(np.sum(y==1)))<1e-12
    assert expected["targetReached"]==(d>=900 and n>0 and d/n>=.30)


rule_masks={}
for rule in saved["rules"]:
    assert 1<=len(rule["allOf"])<=2
    value=np.ones(len(y),bool)
    for p in rule["allOf"]:
        assert p["feature"] in cols
        assert not p["feature"].endswith(".n")
        value &= predicate(p)
    check_stats(value,rule["rawMatchStats"])
    rule_masks[rule["id"]]=value

expected_columns={}
step_count=0
for config in saved["configurations"]:
    positive=config["operation"].startswith("select")
    selected=np.zeros(len(y),bool) if positive else np.ones(len(y),bool)
    assert len(config["steps"])<=config["maximumRules"]
    for step in config["steps"]:
        value=rule_masks[step["ruleId"]]
        if positive:
            delta=value&~selected
            selected |= value
            assert step["addedDraws"]==int(np.sum(delta&(y==1)))
            assert step["addedNonDraws"]==int(np.sum(delta&(y!=1)))
        else:
            delta=value&selected
            selected &= ~value
            assert step["removedDraws"]==int(np.sum(delta&(y==1)))
            assert step["removedNonDraws"]==int(np.sum(delta&(y!=1)))
        check_stats(selected,step["remaining"])
        step_count+=1
    check_stats(selected,config["stats"])
    expected_columns[config["id"]]=selected

with (OUT/"rule-selections.csv").open(encoding="utf-8-sig",newline="") as f:
    reader=csv.DictReader(f)
    n=0
    for i,row in enumerate(reader):
        assert row["matchId"]==rows[i]["matchId"] and int(row["result"])==int(y[i])
        for name,value in expected_columns.items():
            assert int(row[name])==int(value[i])
        n+=1
    assert n==len(y)

for name,expected in saved["sources"].items():
    assert hashlib.sha256((ROOT/name).read_bytes()).hexdigest()==expected,name

eligible=[c for c in saved["configurations"] if c["stats"]["draw"]>=900]
best=min(eligible,key=lambda c:(c["stats"]["falseSelections"],-c["stats"]["draw"],len(c["steps"])))
assert saved["bestConfigurationId"]==best["id"]
assert saved["best"]==best["stats"]
result={"issues":[],"rules":len(rule_masks),"configurations":len(expected_columns),"steps":step_count,
        "rowConfigurationDecisions":len(y)*len(expected_columns),"allSavedSourcesHashMatch":True,
        "best":best["stats"],"scope":"Independent NumPy predicate, cumulative TP/FP and CSV checks; no search optimality or predictive validation claim.",
        "sourceHashes":{p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in
                        [Path(__file__),OUT/"rules.json",OUT/"rule-selections.csv"]}}
(OUT/"rules-review.json").write_text(json.dumps(result,ensure_ascii=False,indent=2)+"\n",encoding="utf8")
print(json.dumps(result,ensure_ascii=False))
