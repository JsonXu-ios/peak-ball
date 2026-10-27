#!/usr/bin/env python3
"""Independent raw-history/pair audit for the label-blind conflict adapter."""
import csv, hashlib, json, sys
from datetime import datetime, timedelta
from pathlib import Path
import numpy as np

sys.dont_write_bytecode=True
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT/'scripts'))
from draw_conflict_features import build_conflict_features
SOURCE=ROOT/'reports/draw-full-feature-study'
OUT=ROOT/'reports/draw-conflict-study'

def read(path):return json.loads(path.read_text(encoding='utf-8-sig'))
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def aware(value):
    try:
        result=datetime.fromisoformat(value.replace('Z','+00:00'))
        return result if result.tzinfo is not None else None
    except (TypeError,ValueError,AttributeError):return None

def recent(row,key):
    history=row.get('preMatchHistory');target=aware(row.get('matchTime'))
    block=history.get(key) if isinstance(history,dict) else None
    if target is None or not isinstance(block,dict) or not isinstance(block.get('matches'),list):return [np.nan]*5
    seen=set();records=[]
    for match in block['matches']:
        kickoff=aware(match.get('matchTime'));hg,ag=match.get('homeScore'),match.get('awayScore')
        if kickoff is None or kickoff+timedelta(hours=2)>=target or str(match.get('matchId',''))==str(row.get('matchId','')):continue
        if not all(isinstance(v,(int,float)) and not isinstance(v,bool) and np.isfinite(v) and int(v)==v and 0<=v<=30 for v in (hg,ag)):continue
        if match.get('venue') not in ('home','away'):continue
        ident=str(match.get('matchId') or (str(kickoff)+'|'+str(match.get('home'))+'|'+str(match.get('guest'))))
        if ident in seen:continue
        seen.add(ident)
        gf,ga=(hg,ag) if match['venue']=='home' else (ag,hg)
        records.append((kickoff,ident,gf-ga))
    differences=[item[2] for item in sorted(records,reverse=True)[:10]]
    n=min(3,len(differences))
    if not n:return [np.nan,np.nan,0.,np.nan,np.nan]
    wins=0
    while wins<len(differences) and differences[wins]>0:wins+=1
    unbeaten=0
    while unbeaten<len(differences) and differences[unbeaten]>=0:unbeaten+=1
    nwin=sum(v>0 for v in differences[:3])
    return [wins,unbeaten,n,nwin,nwin/n]

def pair(raw,largest):
    if not np.isfinite(raw).all():return [np.nan]*4
    absolute=np.abs(raw);order=np.argsort(-absolute if largest else absolute)
    # A tie straddling rank two/three makes all three members admissible.
    ambiguous=abs(absolute[order[1]]-absolute[order[2]])<=1e-9
    members=frozenset(order[:2])
    return [float(not ambiguous and members==frozenset(indices)) for indices in ((0,1),(1,2),(0,2))]+[float(ambiguous)]

def main():
    data=read(SOURCE/'dataset.json');byid={str(r['matchId']):r for r in data['rows']}
    with (SOURCE/'features.csv').open(encoding='utf-8-sig',newline='') as stream:
        reader=csv.reader(stream);header=next(reader);values=list(reader)
    names=header[2:];x=np.array([[float(v) if v else np.nan for v in r[2:]] for r in values]);rows=[byid[r[0]] for r in values]
    manifest=read(SOURCE/'feature-manifest.json');z,meta=build_conflict_features(rows,x,names,manifest)
    expected=[]
    for row in rows:
        expected.append(recent(row,'homeRecent')+recent(row,'awayRecent'))
    expected=np.array(expected,dtype=float)
    for phase in ('opening','latest'):
        for company in ('sporttery','average'):
            raw=x[:,[names.index(f'difference.{phase}.{company}.{outcome}') for outcome in ('home','draw','away')]]
            for largest in (True,False):expected=np.column_stack([expected,np.asarray([pair(r,largest) for r in raw])])
    assert z.shape==(4161,42) and np.array_equal(z,expected,equal_nan=True)
    mutated=[dict(row,result=-999,isDraw=not (row['homeScore']==row['awayScore']),homeScore=99,awayScore=98,guestScore=98,status=999,score='99:98',predictionHit=True) for row in rows]
    changed,changed_meta=build_conflict_features(mutated,x,names,manifest)
    assert np.array_equal(z,changed,equal_nan=True) and changed_meta==meta
    assert all(v.get('denominatorOnly') for v in meta if v['name'].endswith('.3.n'))
    pair_meta=[v for v in meta if v['family']=='conflictDifferencePair']
    assert len(pair_meta)==32 and sum(v['secondary'] for v in pair_meta)==16
    result={'purpose':'Independent feature-adapter audit, not independent future prediction validation.','issues':[],
            'datasetSHA256':sha(SOURCE/'dataset.json'),'matrixCSV_SHA256':sha(SOURCE/'features.csv'),
            'adapterSHA256':sha(ROOT/'scripts/draw_conflict_features.py'),'reviewSHA256':sha(Path(__file__)),
            'rows':len(rows),'columns':z.shape[1],'independentlyRecomputedCells':z.size,
            'historyCells':4161*10,'pairCells':4161*32,'allTargetLabelsMutatedWithoutChangingFeatures':True,
            'features':[{'name':m['name'],'finite':int(np.isfinite(z[:,i]).sum()),'positive':int((z[:,i]>0).sum())} for i,m in enumerate(meta)]}
    OUT.mkdir(exist_ok=True,parents=True);(OUT/'feature-adapter-review.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
    print(json.dumps({k:v for k,v in result.items() if k!='features'},ensure_ascii=False))

if __name__=='__main__':main()
