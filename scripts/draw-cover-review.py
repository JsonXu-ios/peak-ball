#!/usr/bin/env python3
"""Independent finite-rule OR coverage reference; uses frozen local CSV only."""
import csv
import hashlib
import json
import math
import sys
from pathlib import Path

sys.dont_write_bytecode = True
import numpy as np

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-cover-study"
SOURCE = ROOT / "reports/draw-full-feature-study"


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def compare(value, op, threshold):
    if not math.isfinite(value):
        return False
    return value >= threshold if op == "ge" else value <= threshold if op == "le" else value == threshold


def independent_masks(x, names, design):
    """Scalar reference evaluator, intentionally independent of main vector code."""
    columns = {name: i for i,name in enumerate(names)}
    atom_match, atom_eligible = {}, {}
    for atom in design["atoms"]:
        assert atom["feature"] in columns
        assert atom["op"] in ("ge", "le", "eq") and atom["transform"] in ("identity", "abs")
        matched, eligible = [], []
        for row in x:
            value = row[columns[atom["feature"]]]
            valid = math.isfinite(value)
            for guard in atom.get("guards", []):
                valid = valid and compare(row[columns[guard["feature"]]],guard["op"],guard["value"])
            eligible.append(valid)
            matched.append(valid and compare(abs(value) if atom["transform"]=="abs" else value,atom["op"],atom["value"]))
        atom_match[atom["id"]] = np.array(matched,bool)
        atom_eligible[atom["id"]] = np.array(eligible,bool)
    matched, eligible = [], []
    for rule in design["rules"]:
        assert len(rule["allOf"]) in (1,2)
        matched.append(np.logical_and.reduce([atom_match[key] for key in rule["allOf"]]))
        eligible.append(np.logical_and.reduce([atom_eligible[key] for key in rule["allOf"]]))
    return np.asarray(matched),np.asarray(eligible),atom_match,atom_eligible


def summary(y, selected):
    y,selected = np.asarray(y,bool),np.asarray(selected,bool)
    tp,fp,fn,tn = int(sum(y&selected)),int(sum(~y&selected)),int(sum(y&~selected)),int(sum(~y&~selected))
    return {"TP":tp,"FP":fp,"FN":fn,"TN":tn,"selected":tp+fp,
            "precision":tp/(tp+fp) if tp+fp else None,"recall":tp/(tp+fn) if tp+fn else None,
            "specificity":tn/(tn+fp) if tn+fp else None}


def greedy_reference(masks,y,rules,pool,penalty=0,budget=None,target=None):
    selected = np.zeros(len(y),bool)
    available = set(pool)
    steps = []
    while available:
        choices=[]
        already_fp=int(np.count_nonzero(selected&~y))
        for j in available:
            new = masks[j]&~selected
            tp,fp=int(np.count_nonzero(new&y)),int(np.count_nonzero(new&~y))
            if tp==0 or budget is not None and already_fp+fp>budget:
                continue
            score=tp-penalty*fp
            if score<=0:
                continue
            choices.append((-score,-tp,fp,len(rules[j]["allOf"]),rules[j]["id"],j))
        if not choices:
            break
        j=min(choices)[-1]
        previous=selected.copy()
        selected |= masks[j]
        steps.append({"ruleId":rules[j]["id"],"newTP":int(np.count_nonzero((selected&~previous)&y)),"newFP":int(np.count_nonzero((selected&~previous)&~y)),**summary(y,selected)})
        available.remove(j)
        if target is not None and steps[-1]["TP"]>=target:
            break
    return {"steps":steps,"metrics":summary(y,selected),"selected":selected}


def self_test():
    masks=np.array([[1,1,0,1,1],[0,1,1,0,1],[1,0,0,0,0],[0,0,0,1,1]],bool)
    y=np.array([1,1,1,0,0],bool)
    rules=[{"id":"r1","allOf":["a"]},{"id":"r2","allOf":["b"]},{"id":"r3","allOf":["c"]},{"id":"r4","allOf":["d"]}]
    result=greedy_reference(masks,y,rules,range(4))
    assert [s["ruleId"] for s in result["steps"]]==["r2","r3"]
    assert result["metrics"]["TP"]==3 and result["metrics"]["FP"]==1
    assert sum(s["newTP"] for s in result["steps"])==3 and sum(s["newFP"] for s in result["steps"])==1
    zero_budget=greedy_reference(masks,y,rules,range(4),budget=0)
    assert zero_budget["metrics"]["TP"]==1 and zero_budget["metrics"]["FP"]==0
    assert compare(float('nan'),"le",100) is False and compare(float('inf'),"ge",0) is False
    assert compare(0,"ge",0) and compare(0,"le",0) and compare(0,"eq",0)
    return {"deduplicatedIncrements":True,"tieBreakNewFalsePositives":True,"zeroBudgetStops":True,"missingDoesNotMatch":True,"realZeroRetained":True}


def load_inputs():
    design=read(OUT/"candidate-design.json")
    with (SOURCE/"features.csv").open(encoding="utf-8-sig",newline="") as stream:
        reader=csv.reader(stream); header=next(reader); rows=list(reader)
    names=header[2:]
    assert header[:2]==["matchId","resultLabel"]
    x=np.array([[float(v) if v else np.nan for v in row[2:]] for row in rows])
    y=np.array([row[1]=="1" for row in rows])
    return design,rows,names,x,y


def check_metrics(expected, actual):
    for key,value in expected.items():
        translated={"TP":"tp","FP":"fp","FN":"fn","TN":"tn"}.get(key,key)
        other=actual[translated]
        assert other is None if value is None else abs(value-other)<1e-12, (translated,value,other)


def pool_reference(masks,eligible,atoms,atom_eligible,design,y,train,cal):
    broad,suspected,details=[],[],[]
    atom_variable={key:len(set(bool(v) for v in values[train][atom_eligible[key][train]]))==2 for key,values in atoms.items()}
    for j,rule in enumerate(design['rules']):
        ti,ci=train[masks[j,train]],cal[masks[j,cal]]
        te,ce=train[eligible[j,train]],cal[eligible[j,cal]]
        nt,nc,at,ac=len(ti),len(ci),len(te),len(ce)
        dt,dc=int(sum(y[ti])),int(sum(y[ci]))
        pt,pc=dt/nt if nt else None,dc/nc if nc else None
        bt,bc=float(np.mean(y[te])) if at else None,float(np.mean(y[ce])) if ac else None
        reasons=[]
        if nt<40:reasons.append('train_selected_below_40')
        if nc<10:reasons.append('cal_selected_below_10')
        if not 0<nt<at:reasons.append('rule_constant_within_train_eligible')
        if not all(atom_variable[key] for key in rule['allOf']):reasons.append('atom_constant_within_train_eligible')
        good=not reasons
        suspect=good and pt>bt+1e-9 and pc>bc+1e-9
        if good:broad.append(j)
        if suspect:suspected.append(j)
        details.append({'ruleId':rule['id'],'broad':good,'suspected':suspect,
                        'train':{'selected':nt,'draws':dt,'eligible':at,'precision':pt,'baseline':bt},
                        'calibration':{'selected':nc,'draws':dc,'eligible':ac,'precision':pc,'baseline':bc},'broadExclusions':reasons})
    return {'broad':broad,'suspected':suspected},details


def review_saved(design,rows,names,x,y,masks,eligible,atoms,atom_eligible):
    import importlib.util
    sys.path.insert(0,str(ROOT/'scripts'))
    spec=importlib.util.spec_from_file_location('or_study_reviewed',ROOT/'scripts/draw-cover-study.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    report=read(OUT/'analysis.json');protocol=read(OUT/'protocol.json')
    source_files={'dataset':SOURCE/'dataset.json','matrix':SOURCE/'features.csv','manifest':SOURCE/'feature-manifest.json',
                  'folds':SOURCE/'folds.json','protocol':OUT/'protocol.json','candidateScript':ROOT/'scripts/draw_cover_candidates.py',
                  'script':ROOT/'scripts/draw-cover-study.py','candidateDesign':OUT/'candidate-design.json',
                  'reportScript':ROOT/'scripts/draw_cover_report.py',
                  'inputAudit':SOURCE/'union-input-availability-audit.json'}
    for key,path in source_files.items():
        assert report['sourceHashes'][key]==hashlib.sha256(path.read_bytes()).hexdigest(),key
    assert report['sourceHashes']['canonicalFeatureMatrix']==hashlib.sha256(np.nan_to_num(x,nan=-9.87654321e99).astype('<f8').tobytes()).hexdigest()
    actual_m,actual_e,actual_a,actual_ae=module.evaluate_candidates(x,names,design)
    assert np.array_equal(actual_m,masks) and np.array_equal(actual_e,eligible)
    assert all(np.array_equal(actual_a[key],atoms[key]) and np.array_equal(actual_ae[key],atom_eligible[key]) for key in atoms)
    source={str(r['matchId']):r for r in read(SOURCE/'dataset.json')['rows']}
    train=np.array([i for i,r in enumerate(rows) if source[r[0]]['date']<=protocol['developmentDates']['trainEnd']],int)
    cal=np.array([i for i,r in enumerate(rows) if protocol['developmentDates']['trainEnd']<source[r[0]]['date']<=protocol['developmentDates']['calibrationEnd']],int)
    pools,details=pool_reference(masks,eligible,atoms,atom_eligible,design,y,train,cal)
    assert report['candidates']['qualification']==details
    assert report['candidates']['qualified']=={key:len(value) for key,value in pools.items()}
    assert report['development']['train']==train.tolist() and report['development']['calibration']==cal.tolist()
    checks={'scalarAtomRows':len(atoms)*len(y),'ruleMaskRows':len(masks)*len(y),'globalRuleQualifications':len(masks)}
    path_masks={}
    for path in report['paths']:
        result=greedy_reference(masks,y,design['rules'],pools[path['pool']],penalty=path['penalty'],budget=path['fpBudget'])
        assert len(result['steps'])==len(path['steps']),path['id']
        for expected,actual in zip(result['steps'],path['steps']):
            assert expected['ruleId']==actual['ruleId'] and expected['newTP']==actual['newTP'] and expected['newFP']==actual['newFP'],path['id']
            check_metrics({k:v for k,v in expected.items() if k not in('ruleId','newTP','newFP')},actual)
        check_metrics(result['metrics'],path['final'])
        pool_upper=np.logical_or.reduce(masks[pools[path['pool']]]) if pools[path['pool']] else np.zeros(len(y),bool)
        check_metrics(summary(y,pool_upper),path['upperBound'])
        for target in (900,920,950,970):
            expected=next((s for s in result['steps'] if s['TP']>=target),None)
            actual=path['milestones'][str(target)]
            assert (expected is None)==(actual is None)
            if expected:
                assert expected['ruleId']==actual['ruleId']
                check_metrics({k:v for k,v in expected.items() if k not in('ruleId','newTP','newFP')},actual)
        assert path['nearAllSelection']==bool(np.mean(result['selected'])>=.95)
        path_masks[path['id']]=result['selected']
    checks['completeHistoricalGreedyPaths']=len(report['paths'])
    checks['historicalGreedySteps']=sum(len(p['steps']) for p in report['paths'])
    check_metrics(summary(y,masks.any(axis=0)),report['candidateUnionUpperBound'])
    check_metrics(summary(y,np.ones(len(y),bool)),report['allPositiveReference'])
    missed=read(OUT/'missed-draws.json')
    root_counts={}
    for pool in pools:
        pure=next(p for p in report['paths'] if p['pool']==pool and p['method']=='pure')
        covered=path_masks[pure['id']]
        expected_indices=np.flatnonzero(y&~covered)
        recorded=[r for r in missed if r['pool']==pool]
        assert [r['matchId'] for r in recorded]==[rows[i][0] for i in expected_indices]
        upper=masks[pools[pool]].any(axis=0)
        for i,item in zip(expected_indices,recorded):
            reason='no_eligible_candidate' if not eligible[pools[pool],i].any() else 'no_candidate_match' if not upper[i] else 'matched_not_selected'
            assert item['reason']==reason and item['rawCandidateMatched']==bool(masks[:,i].any())
            root_reason='no_raw_candidate_input_qualification' if not eligible[:,i].any() else 'no_fixed_business_rule_match' if not masks[:,i].any() else 'matches_only_early_excluded_rules' if not upper[i] else 'matched_not_selected'
            assert item['rootReason']==root_reason
            root_counts[pool,root_reason]=root_counts.get((pool,root_reason),0)+1
        assert report['missedBreakdown'][pool]=={reason:count for (name,reason),count in root_counts.items() if name==pool}
    checks['missedDrawExplanations']=len(missed)
    atoms_by_id={a['id']:a for a in design['atoms']}
    rules_by_id={r['id']:r for r in design['rules']}
    qualified_by_id={r['ruleId']:r for r in details}
    selected=set(step['ruleId'] for path in report['paths'] for step in path['steps'])
    selected_details=read(OUT/'selected-rule-details.json')
    assert {item['rule']['id'] for item in selected_details}==selected
    for item in selected_details:
        rule_id=item['rule']['id']
        assert item['rule']==rules_by_id[rule_id]
        assert item['atoms']==[atoms_by_id[key] for key in rules_by_id[rule_id]['allOf']]
        assert item['qualification']==qualified_by_id[rule_id]
    checks['selectedBusinessRuleDetails']=len(selected_details)
    folds=read(SOURCE/'folds.json');assert len(folds)==len(report['temporal']['folds'])==5
    pooled={}
    for fold,stored in zip(folds,report['temporal']['folds']):
        ti,ci,vi=(np.array(fold[k],int) for k in ('train','calibration','test'))
        assert stored['fitMatchIds']=={'train':[rows[i][0] for i in ti],'calibration':[rows[i][0] for i in ci]}
        fold_pools,fold_details=pool_reference(masks,eligible,atoms,atom_eligible,design,y,ti,ci)
        altered=y.copy();altered[vi]=~altered[vi]
        observed_pool,_=module.pool_qualification(masks,eligible,atoms,atom_eligible,design,altered,ti,ci)
        assert observed_pool==fold_pools
        for pool,candidates in fold_pools.items():
            block=stored['pools'][pool]
            assert block['qualification']==fold_details and block['qualified']==len(candidates)
            fitted=greedy_reference(masks[:,ti],y[ti],design['rules'],candidates)
            assert [s['ruleId'] for s in fitted['steps']]==[s['ruleId'] for s in block['steps']]
            mutated_steps,_=module.greedy(masks,design['rules'],candidates,altered,ti)
            assert [s['ruleId'] for s in mutated_steps]==[s['ruleId'] for s in fitted['steps']]
            for expected,actual in zip(fitted['steps'],block['steps']):
                assert expected['newTP']==actual['newTP'] and expected['newFP']==actual['newFP']
                check_metrics({k:v for k,v in expected.items() if k not in('ruleId','newTP','newFP')},actual)
            indices={r['id']:i for i,r in enumerate(design['rules'])}
            for case in block['cases']:
                fraction=1. if case['target']=='maximum' else int(case['target'])/970
                target=int(math.ceil(fraction*int(sum(y[ti]))-1e-9))
                prefix=next((i+1 for i,s in enumerate(fitted['steps']) if s['TP']>=target),len(fitted['steps']))
                rule_ids=[s['ruleId'] for s in fitted['steps'][:prefix]]
                assert case['prefix']==prefix and case['ruleIds']==rule_ids and case['trainingDrawTarget']==target
                mask=np.logical_or.reduce([masks[indices[key]] for key in rule_ids]) if rule_ids else np.zeros(len(y),bool)
                assert case['trainingTargetReached']==bool(sum(mask[ti]&y[ti])>=target)
                for key,ii in (('train',ti),('calibration',ci),('test',vi)):
                    check_metrics(summary(y[ii],mask[ii]),case[key])
                for role in ('all',fold['role']):
                    key=(pool,case['target'],role)
                    if key not in pooled:pooled[key]=([],np.zeros(len(y),bool))
                    pooled[key][0].extend(vi);pooled[key][1][vi]=mask[vi]
                checks['temporalLockedCases']=checks.get('temporalLockedCases',0)+1
    for item in report['temporal']['summaries']:
        ii,mask=pooled[item['pool'],item['target'],item['role']]
        assert len(ii)==len(set(ii))==item['games']
        ii=np.array(ii,int)
        check_metrics(summary(y[ii],mask[ii]),item)
    checks['temporalPooledCases']=len(pooled)
    checks['testLabelMutationCannotChangeOrders']=10
    return {'checks':checks,'issues':[], 'qualified':report['candidates']['qualified'],'rawUpperBound':report['candidateUnionUpperBound'],
            'pureFinals':{p['pool']:p['final'] for p in report['paths'] if p['method']=='pure'},
            'missedBreakdown':report['missedBreakdown'],
            'reportHTML_SHA256':hashlib.sha256((OUT/'report.html').read_bytes()).hexdigest(),
            'studyScriptSHA256':hashlib.sha256((ROOT/'scripts/draw-cover-study.py').read_bytes()).hexdigest()}


if __name__=="__main__":
    design,rows,names,x,y=load_inputs()
    masks,eligible,am,ae=independent_masks(x,names,design)
    info={"purpose":"Independent fixed-rule scalar mask reference; no threshold tuning.","featuresCSV_SHA256":hashlib.sha256((SOURCE/"features.csv").read_bytes()).hexdigest(),
          "candidateDesignSHA256":hashlib.sha256((OUT/"candidate-design.json").read_bytes()).hexdigest(),"rows":len(rows),"draws":int(sum(y)),"atoms":len(am),"rules":len(masks),
          "selfTest":self_test(),"rawCandidateUnion":summary(y,np.logical_or.reduce(masks)),
          "ruleMaskSHA256":{rule["id"]:hashlib.sha256(masks[i].tobytes()).hexdigest() for i,rule in enumerate(design["rules"])}}
    (OUT/"independent-mask-reference.json").write_text(json.dumps(info,ensure_ascii=False,indent=2)+"\n",encoding="utf-8")
    print(json.dumps({k:v for k,v in info.items() if k!="ruleMaskSHA256"},ensure_ascii=False))
    if (OUT/'analysis.json').exists():
        review=review_saved(design,rows,names,x,y,masks,eligible,am,ae)
        review.update({'purpose':'Independent mask, qualification, incremental set-coverage and chronological frozen-rule audit.',
                       'featuresCSV_SHA256':info['featuresCSV_SHA256'],'candidateDesignSHA256':info['candidateDesignSHA256'],'selfTest':self_test()})
        (OUT/'review.json').write_text(json.dumps(review,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
        print(json.dumps(review,ensure_ascii=False))
