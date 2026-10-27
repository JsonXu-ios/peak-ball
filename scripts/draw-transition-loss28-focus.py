#!/usr/bin/env python3
"""User-confirmed loss<-28 focus, inheriting the fixed full-family results.

No new candidates, thresholds, weights, q-values or fitted rules are created.
Normal is the exact risk complement on known source inputs under the SAME
context, not a missing-value/default-zero group. Not a production model.
"""
from __future__ import annotations
import csv
import hashlib
import importlib.util
import json
from pathlib import Path

import numpy as np

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/'reports/draw-transition-study'


def main():
    spec=importlib.util.spec_from_file_location('transition_study_for_focus',ROOT/'scripts/draw-transition-study.py')
    study=importlib.util.module_from_spec(spec);spec.loader.exec_module(study)
    raw=(OUT/'inputs.json').read_bytes();rows=json.loads(raw);xs=[r['inputs'] for r in rows]
    roots,mods=study.make_conditions(xs);atomic={c['id']:c for c in roots+mods}
    catalog=json.loads((OUT/'catalog.json').read_text(encoding='utf8'))
    candidates={c['id']:c for c in catalog['candidates'] if '.loss28' in c['id']}
    csvrows=list(csv.DictReader((OUT/'rules.csv').open(encoding='utf-8-sig')))
    numbers={}
    count_keys={'eligible','eligibleDraws','n','draws','falseSelections'}
    for r in csvrows:
        if r['rule'] not in candidates:continue
        values={k:(None if v=='' else int(v) if k in count_keys else float(v)) for k,v in r.items() if k not in ('cohort','rule','label','split')}
        numbers.setdefault((r['cohort'],r['rule']),{})[r['split']]=values
    main_analysis=json.loads((OUT/'analysis.json').read_text(encoding='utf8'))
    dates=np.array([r['kickoffDate'] for r in rows]);cuts=main_analysis['timeCuts'];groups=np.where(dates<=cuts[0],0,np.where(dates<=cuts[1],1,2))
    y=np.array([r['result']==1 for r in rows]);marked=np.array([bool(str(r['jingcaiId'] or '').strip()) for r in rows])
    output={'version':'confirmed-loss28-focus-v1','userConfirmed':'loss risk iff source/model ROI < -28%; normal iff ROI >= -28%, including every positive value',
            'selection':'Existing canonical loss28 candidates only; same original development q/lift/n ordering, full6737-family q inherited unchanged. No new search. Normal is risk complement with identical context and complete inputs.',
            'favoriteDefinition':'Inherited profit.*.favorite uses AVERAGE European latest unique minimum home/away direction; source audit own-market favorites are separate, not pooled.',
            'count':len(candidates),'candidateFamily':main_analysis['candidateCount'],'cohorts':{},'sourceHashes':{str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in (OUT/'analysis.json',OUT/'catalog.json',OUT/'inputs.json',OUT/'rules.csv',ROOT/'scripts/draw-transition-study.py')}}
    all_focus=[]
    for cohort,cohort_mask in (('all',np.ones(len(rows),dtype=bool)),('sporttery',marked),('nonSporttery',~marked)):
        minimum=30 if cohort=='sporttery' else 60;draws=int(y[cohort_mask].sum())
        candidates_in=[]
        for identity,c in candidates.items():
            values=numbers[cohort,identity];dev=values['development']
            if dev['n']<minimum or dev['eligible']-dev['n']<minimum:continue
            candidates_in.append({'id':identity,'label':c['label'],'parts':c['parts'],'metrics':values})
        candidates_in.sort(key=lambda r:(r['metrics']['development']['q'],-r['metrics']['development']['complementLiftPP'],-r['metrics']['development']['n'],r['id']))
        for item in candidates_in[:10]:
            parts=[atomic[p] for p in item['parts']]
            profit=[p for p in parts if p['id'].endswith('.loss28')]
            assert len(profit)==1
            context=[p for p in parts if not p['id'].endswith('.loss28')]
            valid=np.logical_and.reduce([p['valid'] for p in parts])&cohort_mask
            context_mask=np.logical_and.reduce([p['mask'] for p in context]) if context else np.ones(len(rows),dtype=bool)
            risk=valid&context_mask&profit[0]['mask'];normal=valid&context_mask&~profit[0]['mask']
            assert not np.any(risk&normal) and np.array_equal(risk|normal,valid&context_mask)
            item['riskNormalSameContext']={}
            for split,subset in (('all',np.ones(len(rows),dtype=bool)),('development',groups<2),('period3',groups==2)):
                data={}
                for name,mask in (('risk',risk),('normal',normal)):
                    mm=mask&subset;nn=int(mm.sum());dd=int(y[mm].sum())
                    data[name]={'n':nn,'draws':dd,'falseSelections':nn-dd,'precision':dd/nn if nn else None,'drawCoverageOfAllCohort':dd/draws if draws else None}
                item['riskNormalSameContext'][split]=data
                expected=item['metrics'][split]
                assert data['risk']['n']==expected['n'] and data['risk']['draws']==expected['draws']
        supported=[r for r in candidates_in if r['metrics']['development']['q']<=.05 and r['metrics']['development']['complementLiftPP']>0]
        output['cohorts'][cohort]={'qualifiedRules':len(candidates_in),'devBHSupported':len(supported),'topDevelopment':candidates_in[:10]}
        all_focus.extend({'cohort':cohort,**r} for r in candidates_in)
    (OUT/'focused-loss28.json').write_text(json.dumps(output,ensure_ascii=False,indent=2,allow_nan=False),encoding='utf8')
    (OUT/'focused-loss28-rules.json').write_text(json.dumps(all_focus,ensure_ascii=False,indent=2,allow_nan=False),encoding='utf8')
    print(json.dumps({'status':'completed','candidateSubset':len(candidates),'cohorts':{k:{'qualified':v['qualifiedRules'],'BHSupported':v['devBHSupported']} for k,v in output['cohorts'].items()}}))


if __name__=='__main__':main()
