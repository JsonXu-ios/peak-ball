#!/usr/bin/env python3
"""Independent masks, three-way outcomes and chronological conflict audit."""
import csv,hashlib,importlib.util,json,math,sys
from collections import Counter,defaultdict
from datetime import datetime,timedelta
from functools import lru_cache
from pathlib import Path
from statistics import NormalDist
import numpy as np
from scipy.stats import hypergeom

sys.dont_write_bytecode=True
ROOT=Path(__file__).resolve().parents[1];OUT=ROOT/'reports/draw-conflict-study';SOURCE=ROOT/'reports/draw-full-feature-study'
sys.path.insert(0,str(ROOT/'scripts'))
from draw_conflict_features import build_conflict_features
from draw_conflict_rules import build_rules
STRATEGIES=('clean_best1','clean_or3','all_best1','all_or3','all_enriched_or3','smallest_sensitivity_best1')

def read(p):return json.loads(p.read_text(encoding='utf-8-sig'))
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
def cmp(v,op,t):
    if op=='ge':return v>=t
    if op=='le':return v<=t
    if op=='gt':return v>t
    if op=='lt':return v<t
    if op=='eq':return v==t
    if op=='finite':return np.isfinite(v)
    raise AssertionError(op)
def equal(a,b,path=''):
    if isinstance(a,dict):
        assert set(a)==set(b),(path,set(a)^set(b))
        for k in a:equal(a[k],b[k],path+'.'+k)
    elif isinstance(a,list):
        assert len(a)==len(b),path
        for i,(v,w) in enumerate(zip(a,b)):equal(v,w,path+str(i))
    elif isinstance(a,float):assert abs(a-b)<=1e-9,(path,a,b)
    else:assert a==b,(path,a,b)
def wilson(k,n):
    if not n:return [None,None]
    z=NormalDist().inv_cdf(.975);p=k/n;den=1+z*z/n;half=z*math.sqrt(p*(1-p)/n+z*z/(4*n*n))/den;center=(p+z*z/(2*n))/den
    return [max(0.,center-half),min(1.,center+half)]
@lru_cache(None)
def fisher(a,b,c,d):
    if not a+b or not c+d:return 1.
    n=a+b+c+d;k=a+c;sample=a+b;support=np.arange(max(0,sample-(n-k)),min(sample,k)+1)
    probabilities=hypergeom.pmf(support,n,k,sample);observed=hypergeom.pmf(a,n,k,sample)
    return min(1.,float(probabilities[probabilities<=observed*(1+1e-12)].sum()))
def bh(p):
    ordered=sorted(enumerate(p),key=lambda v:(v[1],v[0]));out=[0.]*len(p);smallest=1.
    for rank in range(len(p),0,-1):
        i,value=ordered[rank-1];smallest=min(smallest,value*len(p)/rank);out[i]=smallest
    return out
def raw_stats(mask,labels,ix,favorite,market):
    selected=ix[mask[ix]];n=len(selected);counts=Counter(labels[selected]);h,d,a=(counts[i] for i in (0,1,2));known=np.isfinite(market[selected]);fw,fl=(h,a) if favorite=='home' else (a,h);draws=int((labels[ix]==1).sum())
    return {'games':n,'homeWins':h,'draws':d,'awayWins':a,'homeWinRate':h/n if n else None,'drawRate':d/n if n else None,'awayWinRate':a/n if n else None,
            'coverage':d/draws if draws else None,'favoriteWins':fw,'favoriteLosses':fl,'favoriteNotWins':d+fl,'favoriteNotWinRate':(d+fl)/n if n else None,
            'drawAmongFavoriteNotWins':d/(d+fl) if d+fl else None,'meanMarketDrawProb':float(np.mean(market[selected][known])) if known.any() else None,'marketAvailable':int(known.sum()),'drawWilson95':wilson(d,n)}
def rule_stats(m,e,p,i,j,rule,labels,ix,market):
    child=raw_stats(m[j],labels,ix,rule['favorite'],market);parent=raw_stats(p[j],labels,ix,rule['favorite'],market);nc=raw_stats(p[j]&~m[j],labels,ix,rule['favorite'],market);eligible=raw_stats(e[j],labels,ix,rule['favorite'],market);image=raw_stats(i[j],labels,ix,rule['favorite'],market)
    lift=lambda other:child['drawRate']-other['drawRate'] if child['drawRate'] is not None and other['drawRate'] is not None else None
    return {**child,'parent':parent,'noConflict':nc,'eligible':eligible,'imageParent':image,'parentLift':lift(parent),'eligibleLift':lift(eligible),'imageLift':lift(image),'fisherP':fisher(child['draws'],child['games']-child['draws'],nc['draws'],nc['games']-nc['draws'])}
def qualification(t,c):
    enriched=t['games']>=40 and c['games']>=10 and all(s['eligibleLift'] is not None and s['eligibleLift']>1e-9 for s in (t,c))
    incremental=enriched and t['noConflict']['games']>=20 and c['noConflict']['games']>=5 and all(s['parentLift'] is not None and s['parentLift']>1e-9 for s in (t,c));reasons=[]
    if t['games']<40:reasons.append('train_selected_below40')
    if c['games']<10:reasons.append('calibration_selected_below10')
    if not enriched:reasons.append('not_draw_enriched_in_both_development_intervals')
    if not incremental:reasons.append('parent_increment_or_disjoint_control_support_not_met')
    return {'enriched':bool(enriched),'incremental':bool(incremental),'reasons':reasons}
def choose(rules,ts,cs,qs,strategy):
    options=[];sensitivity=strategy=='smallest_sensitivity_best1';enriched=strategy=='all_enriched_or3';metric='eligibleLift' if enriched else 'parentLift'
    for j,r in enumerate(rules):
        if (r['tier']=='sensitivity')!=sensitivity or strategy.startswith('clean_') and r['view']!='clean' or not qs[j]['enriched' if enriched else 'incremental']:continue
        options.append((-min(ts[j][metric],cs[j][metric]),-min(ts[j]['drawWilson95'][0],cs[j]['drawWilson95'][0]),-ts[j]['games'],r['id'],j))
    return [v[-1] for v in sorted(options)[:1 if strategy.endswith('best1') else 3]]
def union(m,chosen):return np.logical_or.reduce(m[chosen],axis=0) if chosen else np.zeros(m.shape[1],bool)
def metrics(mask,labels,ix):
    positive=labels[ix]==1;selected=mask[ix];tp=int((selected&positive).sum());fp=int((selected&~positive).sum());fn=int((~selected&positive).sum());tn=int((~selected&~positive).sum());n=tp+fp
    return {'games':len(ix),'draws':tp+fn,'tp':tp,'fp':fp,'fn':fn,'tn':tn,'selected':n,'precision':tp/n if n else None,'recall':tp/(tp+fn) if tp+fn else None,'specificity':tn/(tn+fp) if tn+fp else None,'precisionWilson95':wilson(tp,n)}
def winner(summaries):
    eligible=[s for s in summaries if s['role']=='development' and s['method'] in STRATEGIES[:4] and s['selected']>=60]
    return min(eligible,key=lambda s:(-s['precisionWilson95'][0],-s['tp'],s['fp'],s['method']))['method'] if eligible else None

def main():
    report=read(OUT/'analysis.json');folds=read(SOURCE/'folds.json');manifest=read(SOURCE/'feature-manifest.json');raw=read(SOURCE/'dataset.json');byid={str(r['matchId']):r for r in raw['rows']};checks=defaultdict(int)
    with (SOURCE/'features.csv').open(encoding='utf-8-sig',newline='') as stream:reader=csv.reader(stream);header=next(reader);values=list(reader)
    ids=[r[0] for r in values];rows=[byid[i] for i in ids];labels=np.asarray([int(r[1]) for r in values]);names=header[2:];x=np.asarray([[float(v) if v else np.nan for v in r[2:]] for r in values]);idindex={v:j for j,v in enumerate(ids)}
    assert len(ids)==4161 and Counter(labels)=={0:1856,1:970,2:1335}
    assert all((0 if r['homeScore']>r['awayScore'] else 2 if r['homeScore']<r['awayScore'] else 1)==labels[j] for j,r in enumerate(rows))
    z,extra=build_conflict_features(rows,x,names,manifest);archive=np.load(OUT/'extra-features.npz',allow_pickle=False);assert list(archive['matchIds'])==ids and np.array_equal(archive['values'],z,equal_nan=True);assert extra==read(OUT/'extra-feature-manifest.json')
    assert read(OUT/'feature-adapter-review.json')['adapterSHA256']==sha(ROOT/'scripts/draw_conflict_features.py')
    x=np.column_stack([x,z]);names+= [v['name'] for v in extra];manifest+=extra;catalog=read(OUT/'catalog.json');assert catalog==build_rules(manifest)==report['catalog'];rules=catalog['rules'];col={v:j for j,v in enumerate(names)};am={};ae={}
    for atom in catalog['atoms']:
        v=x[:,col[atom['feature']]];valid=np.isfinite(v)
        for guard in atom['guards']:
            gv=x[:,col[guard['feature']]];valid&=np.isfinite(gv)&cmp(gv,guard['op'],guard['value'])
        ae[atom['id']]=valid;am[atom['id']]=valid&cmp(np.abs(v) if atom['transform']=='abs' else v,atom['op'],atom['value'])
    e=np.array([np.logical_and.reduce([ae[a] for a in r['allOf']]) for r in rules]);m=np.array([np.logical_and.reduce([am[a] for a in r['allOf']]) for r in rules]);p=np.array([np.logical_and.reduce([am[a] for a in r['parentAllOf']])&e[j] for j,r in enumerate(rules)]);image=np.array([np.logical_and.reduce([am[a] for a in r['imageAllOf']])&e[j] for j,r in enumerate(rules)])
    assert not (m&~p).any() and not (m&~image).any();checks['allRuleMaskRows']=m.size
    savedmatches=read(OUT/'rule-matches.json');assert savedmatches=={r['id']:[ids[i] for i in np.flatnonzero(m[j])] for j,r in enumerate(rules)}
    market=x[:,col['euro.average.latest.prob.draw']];old=read(ROOT/'reports/draw-cover-study/analysis.json');intervals={'full':np.arange(len(ids)),'train':np.array(old['development']['train'],int),'calibration':np.array(old['development']['calibration'],int),'development':np.array([i for f in folds if f['role']=='development' for i in f['test']],int),'audit':np.array([i for f in folds if f['role']=='lockedInternalAudit' for i in f['test']],int)}
    assert report['phaseMatchIds']=={k:[ids[i] for i in ix] for k,ix in intervals.items()};globalstats={}
    for role,ix in intervals.items():
        globalstats[role]=[rule_stats(m,e,p,image,j,r,labels,ix,market) for j,r in enumerate(rules)]
        for j,r in enumerate(report['rules']):equal(globalstats[role][j],r['stats'][role],r['id']+role)
        checks['independentThreeClassParentFisherStats']+=len(rules)
    for role,key in [('train','qTrain'),('full','qFull')]:
        adjusted=bh([s['fisherP'] for s in globalstats[role]])
        equal(adjusted,[r[key] for r in report['rules']]);checks['allCandidateBHComparisons']+=len(rules)
    qualification0=[qualification(t,c) for t,c in zip(globalstats['train'],globalstats['calibration'])];equal(qualification0,[r['qualification'] for r in report['rules']]);checks['earlierQualificationScreens']+=len(rules)
    for historical in report['historicalUnions']:
        chosen=choose(rules,globalstats['train'],globalstats['calibration'],qualification0,historical['method']);assert historical['ruleIds']==[rules[i]['id'] for i in chosen];equal(metrics(union(m,chosen),labels,intervals['full']),historical['metrics']);checks['historicalUnionsNoFullLabelRanking']+=1
    savedfold=read(OUT/'fold-rule-stats.json');predictions={};market_masks={};statfold=[]
    with (OUT/'predictions.csv').open(encoding='utf-8-sig',newline='') as stream:
        for r in csv.DictReader(stream):
            key=(int(r['fold']),r['method']);i=idindex[r['matchId']];predictions.setdefault(key,np.zeros(len(ids),bool))[i]=bool(int(r['selected']));assert labels[i]==int(r['resultLabel'])
    times=[datetime.fromisoformat(r['matchTime']) for r in rows]
    for fold,saved,rulefold in zip(folds,report['temporal']['folds'],savedfold):
        number=fold['fold'];ti,ci,vi=(np.array(fold[k],int) for k in ('train','calibration','test'));assert not (set(ti)&set(ci) or set(ci)&set(vi) or set(ti)&set(vi));assert max(times[i]+timedelta(hours=2) for i in ti)<min(times[i] for i in ci);assert max(times[i]+timedelta(hours=2) for i in ci)<min(times[i] for i in vi);checks['strictTwoHourBoundaries']+=2
        ts,cs,es=([rule_stats(m,e,p,image,j,r,labels,ix,market) for j,r in enumerate(rules)] for ix in (ti,ci,vi));qs=[qualification(t,c) for t,c in zip(ts,cs)]
        for j,r in enumerate(rules):equal({'ruleId':r['id'],'train':ts[j],'calibration':cs[j],'test':es[j],'qualification':qs[j]},rulefold['rules'][j]);checks['independentThreeClassParentFisherStats']+=3
        assert saved['qualifications']=={'enriched':sum(q['enriched'] for q in qs),'incremental':sum(q['incremental'] for q in qs)};checks['earlierQualificationScreens']+=len(rules)
        market_masks[number]=np.where(np.isfinite(market),market,float((labels[ti]==1).mean()))
        for strategy in STRATEGIES:
            chosen=choose(rules,ts,cs,qs,strategy);selected=union(m,chosen);assert saved['selected'][strategy]==[rules[i]['id'] for i in chosen];equal(metrics(selected,labels,vi),saved['metrics'][strategy]);assert np.array_equal(selected[vi],predictions[number,strategy][vi]);checks['frozenTimeSelectedUnions']+=1
        statfold.append({'fold':number,'ts':ts,'cs':cs,'qs':qs});print('reviewed fold',number,flush=True)
    for summary in report['temporal']['summaries']:
        chosen=np.zeros(len(ids),bool);benchmark=np.zeros(len(ids),bool);ix=[]
        included=[f for f in folds if summary['role']=='all' or f['role']==summary['role']]
        for f in included:
            vi=np.array(f['test'],int);ix.extend(vi);selected=predictions[f['fold'],summary['method']];chosen[vi]=selected[vi];n=int(selected[vi].sum())
            if n:benchmark[vi]=market_masks[f['fold']][vi]>=sorted(market_masks[f['fold']][vi],reverse=True)[n-1]
        ix=np.array(ix,int);assert len(ix)==len(set(ix));equal(metrics(chosen,labels,ix),{k:summary[k] for k in metrics(chosen,labels,ix)});equal(metrics(benchmark,labels,ix),summary['marketSameSize']);assert summary['marketTiesMayIncreaseCount']==(chosen[ix].sum()!=benchmark[ix].sum());checks['pooledUnionsAndSameSizeMarket']=checks['pooledUnionsAndSameSizeMarket']+1
    selected=winner(report['temporal']['summaries']);assert selected==report['temporal']['selection']['method'];mutated=json.loads(json.dumps(report['temporal']['summaries']))
    for r in mutated:
        if r['role']=='lockedInternalAudit':r.update(selected=999999,tp=999999,fp=0,precisionWilson95=[1.,1.])
    assert winner(mutated)==selected;checks['auditOutcomeMutationCannotChangeWinner']=1
    files={'dataset':SOURCE/'dataset.json','matrix':SOURCE/'features.csv','manifest':SOURCE/'feature-manifest.json','folds':SOURCE/'folds.json','coverAnalysis':ROOT/'reports/draw-cover-study/analysis.json','protocol':OUT/'protocol.json','adapter':ROOT/'scripts/draw_conflict_features.py','ruleGenerator':ROOT/'scripts/draw_conflict_rules.py','script':ROOT/'scripts/draw-conflict-study.py','reportScript':ROOT/'scripts/draw_conflict_report.py'}
    for key,path in files.items():assert sha(path)==report['sourceHashes'][key],key
    checks['sourceHashes']=len(files)
    result={'purpose':'Independent arithmetic/protocol audit; all study outcomes are repeated internal data.','issues':[],'checks':dict(checks),'selectedMethod':selected,'studyScriptSHA256':sha(files['script']),'analysisSHA256':sha(OUT/'analysis.json'),'htmlSHA256':sha(OUT/'report.html'),'reviewScriptSHA256':sha(Path(__file__))}
    (OUT/'review.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n',encoding='utf-8');print(json.dumps(result,ensure_ascii=False))

if __name__=='__main__':main()
