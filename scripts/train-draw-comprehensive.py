#!/usr/bin/env python3
"""Train two isolated CART trees from the Go comprehensive feature contract.

This finite offline tool never starts a service or replaces the original model.
All allowed modules are candidate features; split usage is reported separately.
"""
from __future__ import annotations
import argparse
from collections import Counter
from datetime import datetime, timedelta, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import time

for key in ("OMP_NUM_THREADS", "OPENBLAS_NUM_THREADS", "MKL_NUM_THREADS"):
    os.environ.setdefault(key, "4")
import numpy as np
import sklearn
from sklearn.impute import SimpleImputer
from sklearn.metrics import f1_score, accuracy_score, balanced_accuracy_score
from sklearn.tree import DecisionTreeClassifier, export_text
from threadpoolctl import threadpool_limits

ROOT = Path(__file__).resolve().parents[1]
BJT = timezone(timedelta(hours=8))
DEPTHS = (3, 4, 5, 6)
LEAVES = (40, 80, 120)
THRESHOLDS = (.20, .225, .25, .275, .30, .325, .35, .375, .40, .45, .50)

def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))

def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()

def clean(value):
    if isinstance(value, dict): return {str(k): clean(v) for k, v in value.items()}
    if isinstance(value, (tuple, list)): return [clean(v) for v in value]
    if isinstance(value, np.ndarray): return clean(value.tolist())
    if isinstance(value, np.generic): return clean(value.item())
    if isinstance(value, float) and not math.isfinite(value): return None
    return value

def write(path, value, compact=False):
    Path(path).parent.mkdir(parents=True, exist_ok=True)
    Path(path).write_text(json.dumps(clean(value), ensure_ascii=False, indent=None if compact else 2, allow_nan=False), encoding="utf-8")

def day(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00")).astimezone(BJT).date().isoformat()

def source_evidence(name):
    return name.startswith(("euro.", "difference.", "kelly.", "line.", "marketProfit.", "trade.", "heat.", "safeRecent.", "safeH2H.", "context.form.", "context.goals.")) and not name.endswith((".n", ".draws", ".wins", ".losses"))

def low_price(rows):
    out = []
    for row in rows:
        values = (row.get("frontend") or {}).get("averageEuroOdds")
        valid = isinstance(values, list) and len(values) == 3 and all(type(values[i]) in (int, float) and math.isfinite(values[i]) and values[i] > 1 for i in (0, 2))
        out.append(-1 if not valid or abs(values[0] - values[2]) <= 1e-9 else 0 if values[0] < values[2] else 2)
    return np.asarray(out, dtype=int)

def draw_metrics(y, selected, pool):
    actual = pool & (y == 1)
    pick = pool & selected
    n, hits, draws = int(pick.sum()), int(np.sum(pick & actual)), int(actual.sum())
    return dict(matches=int(pool.sum()), actualDraws=draws, selected=n, hits=hits, misses=n-hits,
                precision=hits/n if n else None, coverage=hits/draws if draws else None,
                f1=2*hits/(n+draws) if n+draws else None)

def side_metrics(y, direction, pool):
    usable = pool & (direction >= 0)
    nd = usable & (y != 1)
    hits = int(np.sum(usable & (direction == y)))
    n, non = int(usable.sum()), int(nd.sum())
    return dict(candidates=int(pool.sum()), available=n, hits=hits, singleAccuracy=hits/n if n else None,
                nonDraw=non, conditionalAccuracy=hits/non if non else None,
                draws=int(np.sum(usable & (y == 1))))

def fit_tree(x, target, mask, depth, leaf, imputer=None):
    if imputer is None:
        imputer = SimpleImputer(strategy="median", keep_empty_features=True)
        imputer.fit(x[mask])
    model = DecisionTreeClassifier(max_depth=depth, min_samples_leaf=leaf, criterion="gini", random_state=20261104)
    model.fit(imputer.transform(x[mask]), target[mask])
    return model, imputer

def positive(model, imputer, x):
    return model.predict_proba(imputer.transform(x))[:, list(model.classes_).index(1)]

def export_tree(model, threshold=None):
    nodes=[]
    tree=model.tree_
    for i in range(tree.node_count):
        if tree.children_left[i] == tree.children_right[i]:
            probability=tree.value[i][0]/tree.value[i][0].sum()
            count=int(tree.n_node_samples[i])
            nodes.append(dict(leaf=True, value=float(probability[list(model.classes_).index(1)]), samples=count,
                              counts=[int(round(float(p)*count)) for p in probability]))
        else:
            nodes.append(dict(leaf=False, feature=int(tree.feature[i]), threshold=float(tree.threshold[i]),
                              left=int(tree.children_left[i]), right=int(tree.children_right[i])))
    result=dict(nodes=nodes)
    if threshold is not None: result["threshold"]=threshold
    return result

def predict_old(raw_rows, x, names):
    artifact=read(ROOT/'go_server/internal/drawmodeldata/observation_v1.json')
    lookup={name:j for j,name in enumerate(names)}
    old=np.asarray([[x[i,lookup[f['name']]] for f in artifact['features']] for i in range(len(x))])
    for j,f in enumerate(artifact['features']): old[~np.isfinite(old[:,j]),j]=f['median']
    score=np.full(len(x),artifact['baseline'])
    for tree in artifact['trees']:
        def route(index, ids):
            if not len(ids): return
            node=tree[index]
            if node['leaf']: score[ids]+=node['value'];return
            left=old[ids,node['feature']]<=node['threshold']
            route(node['left'],ids[left]);route(node['right'],ids[~left])
        route(0,np.arange(len(x)))
    scores=1/(1+np.exp(-score))
    used=[lookup[f['name']] for f in artifact['features'] if f['usedInTrees']]
    observed=np.isfinite(x[:,used]).any(axis=1)
    return (scores>=artifact['threshold'])&observed,scores

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--raw',type=Path,default=ROOT/'reports/full-model-20261104/raw.json')
    parser.add_argument('--matrix',type=Path,default=ROOT/'reports/full-model-20261104/matrix.json')
    parser.add_argument('--out',type=Path,default=ROOT/'reports/full-model-20261104')
    parser.add_argument('--artifact',type=Path,default=ROOT/'go_server/internal/drawfullmodel/trees_v1.json')
    args=parser.parse_args();start=time.perf_counter()
    raw,matrix=read(args.raw),read(args.matrix)
    assert sha(args.raw)==matrix['sourceSHA256']
    rows,meta=raw['rows'],matrix['metadata'];names=matrix['featureNames']
    assert names==[f['name'] for f in meta] and len(set(names))==len(names)
    assert [r['matchId'] for r in rows]==[r['matchId'] for r in matrix['rows']]
    x=np.asarray([[np.nan if v is None else v for v in r['values']] for r in matrix['rows']],dtype=float)
    assert np.isfinite(x[~np.isnan(x)]).all()
    y=np.asarray([r['result'] for r in rows],dtype=int)
    assert all(r['result']==(0 if r['homeScore']>r['awayScore'] else 2 if r['homeScore']<r['awayScore'] else 1) for r in rows)
    days=np.asarray([day(r['matchTime']) for r in rows])
    marked=np.asarray([bool(r['jingcaiId'].strip()) for r in rows])
    eligible=np.isfinite(x[:,[j for j,n in enumerate(names) if source_evidence(n)]]).any(axis=1)
    train=(days<='2026-10-19')&eligible
    cal=(days>'2026-10-19')&(days<='2026-10-24')&eligible
    test=(days>'2026-10-24')&eligible
    final_train=(train|cal)
    target_draw=(y==1).astype(int);target_side=(y==2).astype(int)
    protocol=dict(version='draw-comprehensive-protocol-v1',trainThrough='2026-10-19',calibrationThrough='2026-10-24',testFrom='2026-10-25',
                  depthGrid=DEPTHS,leafGrid=LEAVES,thresholdGrid=THRESHOLDS,drawSelection='calibration F1, minimum 20 selected; ties prefer simpler tree then larger leaf',
                  sideSelection='calibration non-draw accuracy; ties prefer simpler tree then larger leaf',
                  finalFit='train+calibration only; no test labels used for fit, model choice or threshold',
                  allCandidateModules=sorted(set(f['module'] for f in meta)),originalModelChanged=False)
    write(args.out/'protocol.json',protocol)
    records=[];side_records=[]
    print(json.dumps(dict(stage='train',rows=len(rows),columns=len(names),train=int(train.sum()),calibration=int(cal.sum()),test=int(test.sum()))),flush=True)
    with threadpool_limits(limits=4):
        for depth in DEPTHS:
            for leaf in LEAVES:
                model,imputer=fit_tree(x,target_draw,train,depth,leaf)
                probabilities=positive(model,imputer,x)
                for threshold in THRESHOLDS:
                    stats=draw_metrics(y,probabilities>=threshold,cal)
                    if stats['selected']>=20:
                        records.append(dict(depth=depth,leaf=leaf,threshold=threshold,metrics=stats))
                side,side_imputer=fit_tree(x,target_side,train&(y!=1),depth,leaf,imputer)
                pred=np.where(positive(side,side_imputer,x)>.5,2,0)
                side_records.append(dict(depth=depth,leaf=leaf,accuracy=float(accuracy_score(y[cal&(y!=1)],pred[cal&(y!=1)]))))
    chosen=sorted(records,key=lambda r:(-r['metrics']['f1'],r['depth'],-r['leaf'],-r['threshold']))[0]
    chosen_side=sorted(side_records,key=lambda r:(-r['accuracy'],r['depth'],-r['leaf']))[0]
    write(args.out/'selected-on-calibration.json',dict(draw=chosen,alternative=chosen_side,candidates=records,sideCandidates=side_records,protocolSHA256=sha(args.out/'protocol.json')))
    draw,draw_imputer=fit_tree(x,target_draw,final_train,chosen['depth'],chosen['leaf'])
    side,side_imputer=fit_tree(x,target_side,final_train&(y!=1),chosen_side['depth'],chosen_side['leaf'],draw_imputer)
    dp=positive(draw,draw_imputer,x);sp=positive(side,side_imputer,x)
    selected=(dp>=chosen['threshold'])&eligible
    direction=np.where(np.abs(sp-.5)<=1e-12,-1,np.where(sp>.5,2,0));direction[~eligible]=-1
    baseline=low_price(rows)
    old_selected,old_score=predict_old(rows,x,names)
    modules={}
    draw_used=set(int(j) for j in draw.tree_.feature if j>=0);side_used=set(int(j) for j in side.tree_.feature if j>=0)
    for module in sorted(set(f['module'] for f in meta)):
        indices=[j for j,f in enumerate(meta) if f['module']==module]
        modules[module]=dict(features=len(indices),availableRows=int(np.isfinite(x[:,indices]).any(axis=1).sum()),
                             observedCells=int(np.isfinite(x[:,indices]).sum()),drawUsed=sum(j in draw_used for j in indices),
                             alternativeUsed=sum(j in side_used for j in indices))
    evaluations={}
    for part,pool in [('trainCalibration',final_train),('test',test),('recentNovember',test&(days>='2026-11-01')),('wholeSnapshotRetrospective',eligible)]:
        evaluations[part]={}
        for scope,scope_mask in [('all',np.ones(len(rows),dtype=bool)),('sporttery',marked),('nonSporttery',~marked)]:
            mask=pool&scope_mask
            same_old=mask&old_selected&(baseline>=0)&(direction>=0)
            new_pool=mask&selected
            same_new=new_pool&(baseline>=0)&(direction>=0)
            evaluations[part][scope]=dict(newDraw=draw_metrics(y,selected,mask),originalDraw=draw_metrics(y,old_selected,mask),
                newTreeAlternative=side_metrics(y,direction,new_pool),lowPriceOnSameNewPool=side_metrics(y,baseline,new_pool),
                pairedNewTree=side_metrics(y,direction,same_new),pairedNewLowPrice=side_metrics(y,baseline,same_new),
                pairedOnOldCandidates=dict(tree=side_metrics(y,direction,same_old),lowPrice=side_metrics(y,baseline,same_old)))
    # Median imputers fit the same train+calibration pool; side-only medians may
    # differ. Keep a separate alternative median when needed by the runtime.
    # Use ONE shared imputer for serving, refit both trees to this exact matrix.
    shared=draw_imputer
    side=DecisionTreeClassifier(max_depth=chosen_side['depth'],min_samples_leaf=chosen_side['leaf'],criterion='gini',random_state=20261104)
    side.fit(shared.transform(x[final_train&(y!=1)]),target_side[final_train&(y!=1)])
    # Re-evaluate the exported side tree, so reported metrics match the artifact.
    sp=positive(side,shared,x)
    direction=np.where(np.abs(sp-.5)<=1e-12,-1,np.where(sp>.5,2,0));direction[~eligible]=-1
    side_used=set(int(j) for j in side.tree_.feature if j>=0)
    for module in modules:
        modules[module]['alternativeUsed']=sum(j in side_used for j,f in enumerate(meta) if f['module']==module)
    for part,pool in [('trainCalibration',final_train),('test',test),('recentNovember',test&(days>='2026-11-01')),('wholeSnapshotRetrospective',eligible)]:
        for scope,scope_mask in [('all',np.ones(len(rows),dtype=bool)),('sporttery',marked),('nonSporttery',~marked)]:
            mask=pool&scope_mask;new_pool=mask&selected
            same_new=new_pool&(baseline>=0)&(direction>=0);same_old=mask&old_selected&(baseline>=0)&(direction>=0)
            block=evaluations[part][scope]
            block.update(newTreeAlternative=side_metrics(y,direction,new_pool),pairedNewTree=side_metrics(y,direction,same_new),pairedNewLowPrice=side_metrics(y,baseline,same_new),pairedOnOldCandidates=dict(tree=side_metrics(y,direction,same_old),lowPrice=side_metrics(y,baseline,same_old)))
    features=[dict(name=f['name'],median=float(shared.statistics_[j]),label=f['label'],module=f['module'],evidence=source_evidence(f['name'])) for j,f in enumerate(meta)]
    metadata=dict(protocol=protocol,sourceSHA256=sha(args.raw),matrixSHA256=sha(args.matrix),sourceGeneratedAt=raw['generatedAt'],
                  trainedThroughBjt='2026-10-24',sourceRows=len(rows),trainRows=int(final_train.sum()),testRows=int(test.sum()),
                  selectedConfiguration=dict(draw=chosen,alternative=chosen_side),modules=modules,evaluation=evaluations,
                  limitations=['Historical inputs lack immutable capture timestamps; chronological holdout is retrospective, not verified prospective execution.',
                    'All valid modules were candidates. CART chooses split variables; registered, observed and used features are separately reported.',
                    'Goddess page outputs include deterministic synthetic preferences and are labelled synthetic in the feature contract.',
                    'A leaf frequency is not a calibrated probability. New artifacts never alter the old model or low-price alternative.'])
    artifact=dict(schema=1,version='draw-comprehensive-trees-v1',inputVersion=matrix['version'],features=features,
                  draw=export_tree(draw,chosen['threshold']),alternative=export_tree(side),metadata=metadata)
    write(args.artifact,artifact,True)
    report=dict(version=artifact['version'],artifactSHA256=sha(args.artifact),sklearnVersion=sklearn.__version__,**metadata,
                allFeatures=meta,drawUsedFeatures=[meta[j] for j in sorted(draw_used)],alternativeUsedFeatures=[meta[j] for j in sorted(side_used)],
                servicesStarted=False,originalModelSHA256=sha(ROOT/'go_server/internal/drawmodeldata/observation_v1.json'),elapsedSeconds=time.perf_counter()-start)
    write(args.out/'training-report.json',report)
    (args.out/'draw-tree.txt').write_text(export_text(draw,feature_names=names,decimals=5),encoding='utf-8')
    (args.out/'alternative-tree.txt').write_text(export_text(side,feature_names=names,decimals=5),encoding='utf-8')
    predictions=[dict(matchId=r['matchId'],drawScore=float(dp[i]),selected=bool(selected[i]),awayScore=float(sp[i]),alternative=int(direction[i]),eligible=bool(eligible[i]),oldSelected=bool(old_selected[i]),oldScore=float(old_score[i])) for i,r in enumerate(rows)]
    write(args.out/'predictions.json',dict(sourceSHA256=sha(args.raw),artifactSHA256=sha(args.artifact),rows=predictions))
    print(json.dumps(clean(dict(status='passed',artifact=str(args.artifact),features=len(features),drawUsed=len(draw_used),alternativeUsed=len(side_used),chosen=chosen,chosenAlternative=chosen_side,test=evaluations['test'],elapsedSeconds=time.perf_counter()-start)),ensure_ascii=False,indent=2),flush=True)

if __name__=='__main__': main()
