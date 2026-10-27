#!/usr/bin/env python3
"""Independent training-threshold, rejection-budget and retained-draw audit."""
import csv, hashlib, importlib.util, json, math, sys
from collections import defaultdict
from datetime import datetime,timedelta
from pathlib import Path
import numpy as np

sys.dont_write_bytecode=True
ROOT=Path(__file__).resolve().parents[1];OUT=ROOT/'reports/draw-reduction-study';SOURCE=ROOT/'reports/draw-full-feature-study'
sys.path.insert(0,str(ROOT/'scripts'))

def read(p):return json.loads(p.read_text(encoding='utf-8-sig'))
def digest(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def import_file(name,p):
    spec=importlib.util.spec_from_file_location(name,p);m=importlib.util.module_from_spec(spec);sys.modules[name]=m;spec.loader.exec_module(m);return m
def compare(v,op,t):
    return v>=t if op=='ge' else v<=t if op=='le' else v>t if op=='gt' else v<t if op=='lt' else v==t
def required(y,ix,t):return math.ceil(t/970*int(np.count_nonzero(y[ix]))-1e-9)
def stats(mask,y,ix):
    m,p=mask[ix],y[ix];tp=int(np.count_nonzero(m&p));fp=int(np.count_nonzero(m&~p));fn=int(np.count_nonzero(~m&p));tn=int(np.count_nonzero(~m&~p))
    return {'games':len(ix),'draws':tp+fn,'tp':tp,'fp':fp,'fn':fn,'tn':tn,'selected':tp+fp,'precision':tp/(tp+fp) if tp+fp else None,'recall':tp/(tp+fn) if tp+fn else None,'specificity':tn/(tn+fp) if tn+fp else None}
def equal(a,b):
    for k,v in a.items():
        assert b[k] is None if v is None else abs(v-b[k])<1e-10,(k,v,b.get(k))

def masks(x,names,design):
    col={n:i for i,n in enumerate(names)};am={};ae={}
    for a in design['atoms']:
        v=x[:,col[a['feature']]];valid=np.isfinite(v)
        for g in a['guards']:
            q=x[:,col[g['feature']]];valid=valid&np.isfinite(q)&compare(q,g['op'],g['value'])
        transformed=np.abs(v) if a['transform']=='abs' else v
        am[a['id']]=valid&compare(transformed,a['op'],a['value']);ae[a['id']]=valid
    return np.asarray([np.logical_and.reduce([am[k] for k in r['allOf']]) for r in design['rules']]),np.asarray([np.logical_and.reduce([ae[k] for k in r['allOf']]) for r in design['rules']]),am,ae

def qualify(r,e,am,ae,design,base,y,ti,ci):
    variable={k:len(set(bool(v) for v in a[ti][base[ti]&ae[k][ti]]))==2 for k,a in am.items()};atoms={a['id']:a for a in design['atoms']};pools={'clean':[],'all':[]};rows=[]
    for j,rule in enumerate(design['rules']):
        okay=all(variable[k] for k in rule['allOf']);d={}
        for role,ix,minimum in [('train',ti,30),('calibration',ci,10)]:
            hit=base[ix]&r[j,ix];valid=base[ix]&e[j,ix];n=int(hit.sum());en=int(valid.sum());fp=int((hit&~y[ix]).sum());efp=int((valid&~y[ix]).sum());rate=fp/n if n else None;b=efp/en if en else None
            okay=okay and n>=minimum and rate is not None and b is not None and rate>b+1e-9
            d[role]={'matched':n,'nonDraws':fp,'eligible':en,'nonDrawRate':rate,'eligibleBaseNonDrawRate':b}
        clean=all(atoms[k]['view'] in ('market','safeHistory') for k in rule['allOf'])
        if okay:pools['all'].append(j)
        if okay and clean:pools['clean'].append(j)
        rows.append({'ruleId':rule['id'],'qualified':bool(okay),'view':'clean' if clean else 'all',**d})
    return pools,rows

def reject_reference(r,e,design,pool,base,y,ti,ci,target,strategy):
    ixes={'train':ti,'calibration':ci};floor={k:required(y,ix,target) for k,ix in ixes.items()};survive=base.copy();remaining=set(pool);order=[];steps=[]
    if any(stats(base,y,ix)['tp']<floor[k] for k,ix in ixes.items()):return order,survive,steps
    while remaining and len(order)<12:
        choices=[]
        for j in remaining:
            gain={};valid=True
            for role,ix,minimum in [('train',ti,20),('calibration',ci,8)]:
                m=survive[ix]&r[j,ix];tp=int(np.count_nonzero(m&y[ix]));fp=int(np.count_nonzero(m&~y[ix]));eligible=survive[ix]&e[j,ix];n=int(eligible.sum());efp=int(np.count_nonzero(eligible&~y[ix]))
                if tp+fp<minimum or fp==0 or stats(survive,y,ix)['tp']-tp<floor[role] or not n or fp/(tp+fp)<=efp/n+1e-9:valid=False;break
                gain[role]=(tp,fp)
            if valid:
                tp,fp=gain['train'];score=fp/(tp+1) if strategy=='ratio' else fp
                choices.append((-score,-fp,tp,len(design['rules'][j]['allOf']),design['rules'][j]['id'],j))
        if not choices:break
        j=min(choices)[-1];before=survive.copy();survive &= ~r[j];removed=before&~survive
        order.append(design['rules'][j]['id']);steps.append((survive.copy(),removed))
        remaining.remove(j)
    return order,survive,steps

def score_reference(scores,base,y,ci,target):
    need=required(y,ci,target);positive=sorted(scores[ci][base[ci]&y[ci]],reverse=True)
    if len(positive)<need or need==0:return None,base.copy(),len(positive)>=need
    t=float(positive[need-1]);return t,base&(scores>=t),True

def main():
    m=import_file('reduction_under_review',ROOT/'scripts/draw-reduction-study.py')
    from draw_rejection_candidates import build_rejection_candidates
    report=read(OUT/'analysis.json');metadata=read(SOURCE/'feature-manifest.json');dataset=read(SOURCE/'dataset.json');byid={str(r['matchId']):r for r in dataset['rows']}
    with (SOURCE/'features.csv').open(encoding='utf-8-sig',newline='') as stream:
        reader=csv.reader(stream);head=next(reader);data=list(reader)
    ids=[r[0] for r in data];names=head[2:];x=np.array([[float(v) if v else np.nan for v in r[2:]] for r in data]);y=np.array([r[1]=='1' for r in data]);allix=np.arange(len(y));checks=defaultdict(int);issues=[]
    assert len(y)==4161 and y.sum()==970 and head[:2]==['matchId','resultLabel'] and names==[v['name'] for v in metadata]
    assert all(byid[ids[i]]['result']==int(data[i][1]) and (byid[ids[i]]['homeScore']==byid[ids[i]]['awayScore'])==y[i] for i in range(len(y)))
    sourcefiles={'dataset':SOURCE/'dataset.json','matrix':SOURCE/'features.csv','manifest':SOURCE/'feature-manifest.json','folds':SOURCE/'folds.json','protocol':OUT/'protocol.json','oldCoverAnalysis':ROOT/'reports/draw-cover-study/analysis.json','coverCandidates':ROOT/'scripts/draw_cover_candidates.py','coverAlgorithm':ROOT/'scripts/draw-cover-study.py','existingModels':ROOT/'scripts/draw-full-feature-study.py','rejectionCandidates':ROOT/'scripts/draw_rejection_candidates.py','reportScript':ROOT/'scripts/draw_reduction_report.py','script':ROOT/'scripts/draw-reduction-study.py'}
    for key,path in sourcefiles.items():assert digest(path)==report['sourceHashes'][key],key
    canonical=hashlib.sha256(np.nan_to_num(x,nan=-9.87654321e99).astype('<f8').tobytes()).hexdigest()
    assert canonical==report['sourceHashes']['canonicalFeatureMatrix']
    for filename,expected in report.get('evidenceHashes',{}).items():assert digest(OUT/filename)==expected,filename
    checks['sourceAndEvidenceHashes']=len(sourcefiles)+len(report.get('evidenceHashes',{}))+1
    cover_design=m.build_candidates(metadata);cm,ce,cam,cae=m.cover.evaluate_candidates(x,names,cover_design);ri={r['id']:i for i,r in enumerate(cover_design['rules'])}
    oldcover=read(ROOT/'reports/draw-cover-study/analysis.json');ti0=np.array(oldcover['development']['train'],int);ci0=np.array(oldcover['development']['calibration'],int)
    fullbase=np.logical_or.reduce([cm[ri[k]] for k in report['fullHistory']['baseRuleIds']]);equal(stats(fullbase,y,allix),report['fullHistory']['base'])
    with (OUT/'predictions.csv').open(encoding='utf-8-sig',newline='') as stream:prediction_rows=list(csv.DictReader(stream))
    saved_masks={};saved_base={};saved_market={};idindex={key:i for i,key in enumerate(ids)}
    for row in prediction_rows:
        key=(int(row['fold']),row['method'],int(row['target']));i=idindex[row['matchId']]
        if key not in saved_masks:saved_masks[key]=np.zeros(len(y),bool)
        saved_masks[key][i]=bool(int(row['selected']));saved_base.setdefault(key[0],np.zeros(len(y),bool))[i]=bool(int(row['baseSelected']));saved_market.setdefault(key[0],np.full(len(y),np.nan))[i]=float(row['marketScore'])
        assert bool(int(row['isDraw']))==y[i]
    temporal_folds=read(SOURCE/'folds.json');contexts=[('historical',ti0,ci0,allix,fullbase,report['fullHistory']['paths'],None)]
    for fold,stored in zip(temporal_folds,report['temporal']['folds']):
        ti,ci,vi=(np.array(fold[k],int) for k in ('train','calibration','test'))
        assert not(set(ti)&set(ci) or set(ti)&set(vi) or set(ci)&set(vi))
        times=[datetime.fromisoformat(byid[i]['matchTime']) for i in ids]
        assert max(times[i]+timedelta(hours=2) for i in ti)<min(times[i] for i in ci)
        assert max(times[i]+timedelta(hours=2) for i in ci)<min(times[i] for i in vi)
        checks['strictResultAvailabilityBoundaries']+=2
        p,_=m.cover.pool_qualification(cm,ce,cam,cae,cover_design,y,ti,ci);steps,base=m.cover.greedy(cm,cover_design['rules'],p['broad'],y,ti)
        assert [s['ruleId'] for s in steps]==stored['baseRuleIds'] and np.array_equal(base[vi],saved_base[fold['fold']][vi])
        contexts.append((str(fold['fold']),ti,ci,vi,base,stored['cases'],fold))
        checks['frozenChronologicalBases']+=1
    for tag,ti,ci,vi,base,cases,fold in contexts:
        design=read(OUT/('candidates-historical.json' if tag=='historical' else f'candidates-fold-{tag}.json'))
        assert design==build_rejection_candidates(x[ti],names,metadata),tag
        assert design['thresholdSource']['trainingRows']==len(ti) and len(design['rules'])<=5000
        for q in design['thresholdSource']['featureQuantiles']:
            col=names.index(q['feature']);valid=np.isfinite(x[ti,col])
            for g in q['guards']:
                gv=x[ti,names.index(g['feature'])];valid &= np.isfinite(gv)&compare(gv,g['op'],g['value'])
            values=sorted(x[ti,col][valid]);assert len(values)==q['finiteEligible']>=40
            for v in q['values']:
                position=(len(values)-1)*v['quantile'];lo=math.floor(position);hi=math.ceil(position);expected=values[lo]+(values[hi]-values[lo])*(position-lo)
                assert abs(v['value']-expected)<1e-10
                checks['independentTrainingQuantileValues']+=1
        r,e,am,ae=masks(x,names,design);ar,er,_,_=m.masks_for(x,names,design);assert np.array_equal(r,ar) and np.array_equal(e,er)
        checks['rejectionRuleMaskRows']+=r.size
        pools,qualification=qualify(r,e,am,ae,design,base,y,ti,ci)
        assert qualification==read(OUT/('qualification-historical.json' if tag=='historical' else f'qualification-fold-{tag}.json'))
        checks['initialRuleQualifications']+=len(qualification)
        scorepath=OUT/('scores-full.npz' if tag=='historical' else f'scores-fold-{tag}.npz')
        archive=np.load(scorepath,allow_pickle=False) if scorepath.exists() else None
        if archive is not None:
            market=x[:,names.index('euro.average.latest.prob.draw')]
            scoretrain=allix if tag=='historical' else ti
            expected_market=np.where(np.isfinite(market),market,float(y[scoretrain].mean()))
            assert np.array_equal(archive['market_raw'],expected_market)
            assert all(np.all(np.isfinite(archive[key])) for key in archive.files if key!='matchIds')
            checks['marketSameInputAndTrainPriorScores']+=len(y)
        for case in cases:
            target=case['target'];fit_ti,fit_ci=(allix,allix) if tag=='historical' else (ti,ci)
            expected_steps=[]
            if case['kind']=='rule':
                strategy=next(a['strategy'] for a in report['methods'] if a['id']==case['method'])
                order,survive,expected_steps=reject_reference(r,e,design,pools[case['view']],base,y,fit_ti,fit_ci,target,strategy)
                assert order==case['selectedRuleIds']
                for (mask,removed),step in zip(expected_steps,case['steps']):
                    for role,ix in ([('full',allix)] if tag=='historical' else [('train',ti),('calibration',ci),('test',vi)]):equal(stats(mask,y,ix),step[role])
                    assert step['removedFP']==int(np.count_nonzero(removed[fit_ti]&~y[fit_ti])) and step['removedTP']==int(np.count_nonzero(removed[fit_ti]&y[fit_ti]))
                checks['completeRejectionGreedyCases']+=1
                checks['rejectionGreedySteps']+=len(order)
                if tag!='historical':
                    mutated=y.copy();mutated[vi]=~mutated[vi]
                    changed_result,changed_mask=m.reject_greedy(r,e,design,pools[case['view']],base,mutated,ti,ci,target,strategy,vi)
                    assert changed_result['selectedRuleIds']==order and np.array_equal(changed_mask,survive)
                    checks['futureOutcomeMutationCannotChangeRuleChains']+=1
            elif case['kind']=='base':survive=base.copy()
            elif archive is not None:
                model=next(a['model'] for a in report['methods'] if a['id']==case['method'])
                scores=archive[model];threshold,survive,reachable=score_reference(scores,base,y,fit_ci,target)
                assert case['threshold']==threshold and case['calibrationTargetReachable']==reachable
                if tag!='historical':
                    mutated=y.copy();mutated[vi]=~mutated[vi]
                    threshold2,survive2,reachable2=score_reference(scores,base,mutated,fit_ci,target)
                    assert threshold==threshold2 and reachable==reachable2 and np.array_equal(survive,survive2)
                    checks['futureOutcomeMutationCannotChangeScoreThresholds']+=1
                checks['highestGlobalDrawDenominatorScoreThresholds']+=1
            else:
                issues.append('pending raw score archive '+tag);continue
            assert not np.any(survive&~base)
            if tag=='historical':
                equal(stats(survive,y,allix),case['metrics']['full']);assert case['selectedMatchIds']==[ids[i] for i in np.flatnonzero(survive)]
                assert case['targetReached']['full']==bool(stats(survive,y,allix)['tp']>=target)
            else:
                for role,ix in [('train',ti),('calibration',ci),('test',vi)]:
                    equal(stats(survive,y,ix),case['metrics'][role]);assert case['targetReached'][role]==bool(stats(survive,y,ix)['tp']>=required(y,ix,target))
                assert np.array_equal(survive[vi],saved_masks[int(tag),case['method'],target][vi])
            checks['caseConfusionAndNoAdditions']+=1
        if archive is not None:
            assert list(archive['matchIds'])==ids
        print('reviewed',tag,'candidates',len(design['rules']),flush=True)
    # Independent pooled deltas, uniform-size expectation and same-size market ranks.
    for s in report['temporal']['summaries']:
        indices=[];selected=np.zeros(len(y),bool);base=np.zeros(len(y),bool);marketmask=np.zeros(len(y),bool);utp=0.;ufp=0.;fail=0
        for fold in report['temporal']['folds']:
            if s['role']!='all' and fold['role']!=s['role']:continue
            vi=np.array(fold['test'],int);indices.extend(vi);key=(fold['fold'],s['method'],s['target']);current=saved_masks[key];b=saved_base[fold['fold']];selected[vi]=current[vi];base[vi]=b[vi]
            n=int(current[vi].sum());scores=saved_market[fold['fold']][vi];eligible=b[vi];cut=sorted(scores[eligible],reverse=True)[n-1] if n else np.inf;marketmask[vi]=eligible&(scores>=cut)
            bn=int(eligible.sum());bt=int(np.count_nonzero(eligible&y[vi]));utp+=n*bt/bn if bn else 0.;ufp+=n*(bn-bt)/bn if bn else 0.
            case=next(c for c in fold['cases'] if c['method']==s['method'] and c['target']==s['target']);fail+=int(not case['calibrationTargetReachable'] or not case['trainTargetReachable'])
        ix=np.array(indices,int);assert len(ix)==len(set(ix));st=stats(selected,y,ix);bs=stats(base,y,ix);equal(st,s);equal(bs,s['base']);equal(stats(marketmask,y,ix),s['marketSameSize'])
        removed=bs['fp']-st['fp'];lost=bs['tp']-st['tp'];assert s['removedFP']==removed and s['lostTP']==lost and abs(s['fpReduction']-removed/bs['fp'])<1e-12 and abs(s['recallLoss']-lost/int(y[ix].sum()))<1e-12
        assert abs(s['uniformExpectedTP']-utp)<1e-9 and abs(s['uniformExpectedFP']-ufp)<1e-9 and s['infeasibleFitFolds']==fail
        assert s['targetReached']==(st['tp']>=required(y,ix,s['target']))
        checks['pooledDeltasAndFairSameSizeReferences']+=1
    selected=[];clean={a['id'] for a in report['methods'] if a['view']=='clean' and a['id']!='base'}
    for target in (900,850,800):
        options=[s for s in report['temporal']['summaries'] if s['role']=='development' and s['target']==target and s['method'] in clean];need=math.ceil(target/970*options[0]['draws']-1e-9)
        winner=min(options,key=lambda s:(max(0,need-s['tp']),s['fp'],-s['tp'],s['method']));selected.append(winner['method'])
    assert selected==[s['method'] for s in report['temporal']['selection']]
    changed=json.loads(json.dumps(report['temporal']['summaries']))
    for s in changed:
        if s['role']=='lockedInternalAudit':s.update(tp=999999,fp=0,precision=1,recall=1)
    assert [s['method'] for s in m.select_on_development(changed)]==selected
    checks['auditOutcomeMutationCannotChangeWinners']=1
    out={'purpose':'Independent fixed-coverage rejection study review; repeated internal validation only.','checks':dict(checks),'issues':sorted(set(issues)),
         'studyScriptSHA256':digest(ROOT/'scripts/draw-reduction-study.py'),'candidateScriptSHA256':digest(ROOT/'scripts/draw_rejection_candidates.py'),'reviewScriptSHA256':digest(Path(__file__)),'analysisSHA256':digest(OUT/'analysis.json'),'htmlSHA256':digest(OUT/'report.html'),'selectedMethods':selected}
    (OUT/'review.json').write_text(json.dumps(out,ensure_ascii=False,indent=2)+'\n',encoding='utf-8');print(json.dumps(out,ensure_ascii=False))

if __name__=='__main__':main()
