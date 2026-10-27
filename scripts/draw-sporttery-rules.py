#!/usr/bin/env python3
"""Bounded retrospective rule fitting on frozen sporttery match subsets only."""
from __future__ import annotations
import csv
import importlib.util
import math
from pathlib import Path
import numpy as np

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('target_rules', ROOT/'scripts/draw-target-900-rules.py')
old = importlib.util.module_from_spec(spec)
spec.loader.exec_module(old)
h = old.hist


def stat(v, pos, target):
    n, d = v.bit_count(), (v & pos).bit_count()
    return {'selected': n, 'draw': d, 'falseSelections': n-d,
            'precision': d/n if n else None, 'recall': d/pos.bit_count(),
            'minimumDraws': target, 'targetReached': d >= target and n > 0 and d/n >= .3}


def pool_for(catalog, X, meta, y, max_loss, source):
    cols = {m['name']: i for i,m in enumerate(meta)}
    def permitted(a):
        if not old.allowed(a): return False
        if source == 'allRecorded': return True
        return a.get('view') == 'safeHistory' or a['feature'].startswith('euro.sporttery.')
    atoms = {a['id']:a for a in catalog['atoms'] if permitted(a)}
    am = {aid:old.bits(h.predicate(a,X,cols)[0]) for aid,a in atoms.items()}
    p = old.bits(y==1)
    u = (1<<len(y))-1
    candidates = {}
    def add(ids, v, origin):
        if not v or v in candidates: return
        d, n = (v&p).bit_count(), v.bit_count()
        if d > max_loss or n-d < 3: return
        candidates[v] = {'id':old.rid(ids), 'allOf':sorted(ids), 'bits':v, 'origin':origin}
    for aid,v in am.items(): add([aid],v,'fixed_atomic_catalog')
    for r in catalog['rules']:
        if len(r['allOf']) > 2 or not all(a in am for a in r['allOf']): continue
        v=u
        for a in r['allOf']: v &= am[a]
        add(r['allOf'],v,'fixed_historical_catalog')
    ranks=[]
    for aid,v in am.items():
        d,n=(v&p).bit_count(),v.bit_count()
        if n>=8: ranks.append(((n-d)/(d+1),n-d,-d,aid))
    ranks.sort(reverse=True)
    used, seeds=set(),[]
    for *_,aid in ranks:
        f=atoms[aid]['feature']
        if f not in used: used.add(f); seeds.append(aid)
        if len(seeds)>=160: break
    seeds=sorted(set(seeds+[r[-1] for r in ranks[:80]]))
    attempts=0
    for i,a in enumerate(seeds):
        for b in seeds[i+1:]:
            if atoms[a]['feature']==atoms[b]['feature']: continue
            attempts+=1; add([a,b],am[a]&am[b],'bounded_low_draw_pair_screen')
    return atoms,sorted(candidates.values(),key=lambda r:r['id']),{'atoms':len(atoms),'uniqueMasks':len(candidates),'pairAttempts':attempts,'seedCount':len(seeds),'maximumLostDraws':max_loss,'minimumRemovedNonDraws':3}


def fit(pool, p, u, target, limit, kind, coef):
    removed=0; steps=[]; used=set(); budget=p.bit_count()-target
    for _ in range(limit):
        best=None
        for r in pool:
            if r['id'] in used: continue
            delta=r['bits']&~removed
            d,f=(delta&p).bit_count(),(delta&~p).bit_count()
            if f<=0 or (removed&p).bit_count()+d>budget: continue
            score=f/(d+.5)**coef if kind=='ratio' else f-coef*d
            key=(score,f,-d,-len(r['allOf']),r['id'])
            if best is None or key>best[0]: best=(key,r,d,f)
        if best is None or best[0][0]<=0: break
        _,r,d,f=best;removed|=r['bits'];used.add(r['id'])
        steps.append({'ruleId':r['id'],'removedDraws':d,'removedNonDraws':f,'remaining':stat(u&~removed,p,target)})
    return u&~removed,steps


def main():
    out=ROOT/'reports/draw-sporttery-study';out.mkdir(parents=True,exist_ok=True)
    rows,X,y,meta,sources=h.load_inputs(ROOT/'reports/draw-historical-value-study')
    catalog=h.read(ROOT/'reports/draw-historical-value-study/catalog.json')
    previous=h.read(ROOT/'reports/draw-target-900-study/rules.json')
    best=next(r for r in previous['configurations'] if r['id']==previous['bestConfigurationId'])
    frozen={r['id']:r for r in previous['rules']}
    jc=np.array([bool(str(r.get('jingcaiId') or '').strip()) for r in rows])
    cols={m['name']:i for i,m in enumerate(meta)}
    complete=np.isfinite(X[:,[cols['euro.sporttery.latest.odds.'+o] for o in ('home','draw','away')]]).all(1)
    scopes={'jingcaiId':jc,'realSportteryLatestComplete':complete}
    results=[]; csvcols={}; allrules={}
    for scope,sel in scopes.items():
        ix=np.flatnonzero(sel);xx,yy=X[sel],y[sel]
        p=old.bits(yy==1);u=(1<<len(yy))-1;target=math.ceil(p.bit_count()*900/970)
        removed=0
        for step in best['steps']:
            r=frozen[step['ruleId']];v=np.ones(len(yy),bool)
            for a in r['allOf']: v &= h.predicate(a,xx,cols)[0]
            removed |= old.bits(v)
            allrules[r['id']]={**r,'source':'frozen_all4161_best32'}
        fid=scope+'_frozenAll4161Best32'
        results.append({'id':fid,'scope':scope,'source':'allRecorded','fittedOnSubset':False,
                        'ruleIds':[s['ruleId'] for s in best['steps']],'stats':stat(u&~removed,p,target)})
        v=np.zeros(len(y),bool);v[ix]=old.mask(u&~removed,len(yy));csvcols[fid]=v
        print(scope,'baseline',stat(u,p,target),flush=True)
        for source in ('allRecorded','sportteryMarketAndSafeHistory'):
            atoms,pool,search=pool_for(catalog,xx,meta,yy,p.bit_count()-target,source)
            print(scope,source,search,flush=True)
            for limit in (8,16):
                for kind,coefs in (('ratio',(.5,.75,1.,1.25)),('penalty',(5.,8.,12.,20.))):
                    for coef in coefs:
                        selected,steps=fit(pool,p,u,target,limit,kind,coef)
                        cid=f'{scope}_{source}_{limit}_{kind}_{coef:g}'
                        results.append({'id':cid,'scope':scope,'source':source,'fittedOnSubset':True,
                            'maximumRules':limit,'algorithm':kind,'coefficient':coef,'steps':steps,
                            'ruleIds':[s['ruleId'] for s in steps],'stats':stat(selected,p,target),'search':search})
                        v=np.zeros(len(y),bool);v[ix]=old.mask(selected,len(yy));csvcols[cid]=v
                print(scope,source,limit,'done',flush=True)
            rm={r['id']:r for r in pool}
            for r in results:
                if r['scope']!=scope or r['source']!=source or not r['fittedOnSubset']:continue
                for rid in r['ruleIds']:
                    a=[atoms[z] for z in rm[rid]['allOf']]
                    allrules[rid]={'id':rid,'allOf':a,'label':' AND '.join(z['label'] for z in a)}
    sources.update({str(p.relative_to(ROOT)):h.sha(p) for p in (Path(__file__),ROOT/'scripts/draw-target-900-rules.py',ROOT/'reports/draw-target-900-study/rules.json',ROOT/'reports/draw-historical-value-study/catalog.json')})
    winners=[]
    for scope in scopes:
        for source in ('allRecorded','sportteryMarketAndSafeHistory'):
            for limit in (8,16):
                z=[r for r in results if r['scope']==scope and r['source']==source and r.get('maximumRules')==limit]
                b=min(z,key=lambda r:(r['stats']['falseSelections'],-r['stats']['draw'],len(r['ruleIds']),r['id']))
                winners.append({'scope':scope,'source':source,'maximumRules':limit,'id':b['id'],'stats':b['stats']})
    output={'purpose':'体彩比赛范围历史目标核实；并比较仅体彩原始欧赔+严格赛前历史字段',
      'usesAllHistoricalOutcomes':True,'independentValidation':False,'globalOptimumClaimed':False,
      'scopeDefinition':'jingcaiId非空；敏感性为真实sporttery即时胜平负三项赔率完整，不使用模拟赔率冒充',
      'objective':'保留ceil(该范围平局数×900/970)平局；命中率至少30%',
      'restriction':'每排除规则最多2主条件，历史样本量护栏沿用；无ID/日期/联赛/球队谓词；缺失不匹配',
      'scopeCounts':{k:{'matches':int(v.sum()),'draws':int((y[v]==1).sum()),'targetDraws':math.ceil(int((y[v]==1).sum())*900/970)} for k,v in scopes.items()},
      'overlapCounts':{'identifiedAndComplete':int((jc&complete).sum()),'completeWithoutId':int((complete&~jc).sum())},
      'sources':sources,'features':len(meta),'frozenConfiguration':previous['bestConfigurationId'],
      'bestByScopeSourceLimit':winners,'configurations':results,'rules':list(allrules.values())}
    h.write(out/'rules.json',output)
    with (out/'rule-selections.csv').open('w',encoding='utf-8-sig',newline='') as f:
        w=csv.writer(f);w.writerow(['matchId','result','hasJingcaiId','realSportteryLatestComplete']+list(csvcols))
        for i,row in enumerate(rows):w.writerow([row['matchId'],int(y[i]),int(jc[i]),int(complete[i])]+[int(v[i]) for v in csvcols.values()])
    print(h.tidy(winners),flush=True)


if __name__=='__main__':main()
