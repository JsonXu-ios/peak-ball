#!/usr/bin/env python3
"""Replay-only audit: original draw, original backup, and low-water totals.

Never fits a model or uses experimental tree predictions. All combinations
are retrospective coverage definitions, not portfolio profit claims.
"""
from pathlib import Path
import argparse
from collections import Counter
import hashlib
import html
import importlib.util
import json
import math

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('existing_goal_settlement', ROOT / 'scripts/audit-draw-score-goals.py')
legacy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(legacy)

def read(path): return json.loads(path.read_text(encoding='utf-8-sig'))
def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()
def ratio(a, b): return a / b if b else None
def percent(value): return '暂无' if value is None else f'{100 * value:.2f}%'

def quote(row, phase, safe=True):
    value = legacy.real_line(row, phase)
    if value is None: return None
    over, under = value['overWater'], value['underWater']
    if over is None or under is None or safe and max(over, under) > 2: return None
    # Existing predeclared tie policy, independent of any score/result.
    direction = 'over' if over < under else 'under'
    return {**value, 'direction': direction, 'tiedWater': abs(over-under) <= 1e-9,
            'chosenWater': over if direction == 'over' else under}

def phase_result(row, phase, safe=True):
    value = quote(row, phase, safe)
    if value is None: return None
    total = row['homeScore'] + row['awayScore']
    label, net = legacy.settle(total, value['line'], value['direction'], value['chosenWater'])
    positive_comparison = total > value['line'] if value['direction'] == 'over' else total < value['line']
    assert positive_comparison == (label in ['full_win', 'half_win'])
    return {**value, 'settlement': label, 'netUnits': net, 'hit': positive_comparison, 'fullWinOnlyHit': label == 'full_win'}

def summarize(rows, phase):
    n = len(rows)
    draw = sum(r['drawHit'] for r in rows)
    backup = sum(r['backupHit'] for r in rows)
    base = draw + backup
    assert base == sum(r['drawOrBackup'] for r in rows)
    ready = [r for r in rows if r['markets'][phase] is not None]
    labels = Counter(r['markets'][phase]['settlement'] for r in ready)
    hits = sum(r['markets'][phase]['hit'] for r in ready)
    full = labels['full_win']
    rescue = sum(not r['drawOrBackup'] and r['markets'][phase]['hit'] for r in ready)
    full_rescue = sum(not r['drawOrBackup'] and r['markets'][phase]['fullWinOnlyHit'] for r in ready)
    joint = sum(r['drawOrBackup'] and r['markets'][phase]['hit'] for r in ready)
    assert hits == rescue + joint
    assert hits == labels['full_win'] + labels['half_win']
    assert sum(labels.values()) == len(ready)
    return dict(selected=n, drawHits=draw, drawHitRate=ratio(draw,n), backupHits=backup,
        backupAvailable=sum(r['backup'] is not None for r in rows),
        drawOrBackupHits=base, drawOrBackupRate=ratio(base,n),
        totalsAvailable=len(ready), totalsMissingOrAnomalous=n-len(ready), totalsOutputCoverage=ratio(len(ready),n),
        totalsSettlements={k:labels[k] for k in legacy.SETTLEMENTS},
        totalsProfitableHits=hits, totalsProfitableRate=ratio(hits,len(ready)),
        totalsFullWinOnlyHits=full, totalsFullWinOnlyRate=ratio(full,len(ready)),
        ties=sum(r['markets'][phase]['tiedWater'] for r in ready),
        rescuedByTotals=rescue, originalThreeMarketUnionHits=base+rescue,
        originalThreeMarketUnionRate=ratio(base+rescue,n),
        fullWinOnlyRescue=full_rescue, fullWinOnlyThreeMarketUnionHits=base+full_rescue,
        fullWinOnlyThreeMarketUnionRate=ratio(base+full_rescue,n),
        bothResultAndTotalsHits=joint, bothResultAndTotalsRateAllSelected=ratio(joint,n),
        bothResultAndTotalsRateSameTotalsPool=ratio(joint,len(ready)),
        baseHitsOnSameTotalsPool=sum(r['drawOrBackup'] for r in ready),
        threeMarketUnionSameTotalsPool=ratio(sum(r['drawOrBackup'] or r['markets'][phase]['hit'] for r in ready),len(ready)),
        actualGroups={group:dict(matches=len(group_rows),totalsAvailable=sum(r['markets'][phase] is not None for r in group_rows),
            totalsHits=sum(r['markets'][phase] is not None and r['markets'][phase]['hit'] for r in group_rows))
            for group in ['draw_hit','alternative_hit','both_miss','alternative_unavailable_non_draw']
            for group_rows in [[r for r in rows if r['group']==group]]},
        lowWaterNetUnits=sum(r['markets'][phase]['netUnits'] for r in ready),
        lowWaterUnitROI=ratio(sum(r['markets'][phase]['netUnits'] for r in ready),len(ready)))

def make_html(report, path):
    def table(headers, rows):
        return '<div class="scroll"><table><thead><tr>'+''.join('<th>'+html.escape(str(x))+'</th>' for x in headers)+'</tr></thead><tbody>'+''.join('<tr>'+''.join('<td>'+html.escape(str(x))+'</td>' for x in row)+'</tr>' for row in rows)+'</tbody></table></div>'
    doc='''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>仅原模型：平局、次选与低水位大小球</title><style>body{font:15px/1.75 system-ui,"Microsoft YaHei",sans-serif;margin:0;color:#193247;background:#f4f7fa}main{max-width:1250px;margin:auto;padding:28px 24px 65px}section{background:white;border:1px solid #d9e4ec;border-radius:8px;padding:12px 20px 20px;margin:22px 0}.scroll{overflow:auto}table{width:100%;border-collapse:collapse;font-size:13px}th,td{padding:9px;border-bottom:1px solid #d9e4ec;text-align:left;white-space:nowrap}th{background:#eef5fa}.notice{background:#fff2dc;padding:15px;border-left:4px solid #bc831a}a{color:#195c9a}code{overflow-wrap:anywhere}</style><main><h1>仅原模型：平局、胜负次选与低水位大小球</h1>'''
    doc += '<p>本轮从MySQL重新只读导出全部确认完赛，再使用原冻结模型Go运行时重算。原模型参数、阈值和次选规则不变；没有训练或读取任何新树预测。</p>'
    doc += '<p>源快照北京时间 '+html.escape(report['sourceGeneratedAtBeijing'])+'；完赛 '+str(report['sourceMatches'])+' 场；原入选 '+str(report['selected'])+' 场。</p>'
    doc += '<div class="notice"><b>必须区分两种组合：</b>“平／胜负次选／大小球至少一项命中”是跨市场备选覆盖；“胜平负组合与大小球同时命中”要求两个市场都对。前者不能当作单注命中率、串关命中率或盈利率。</div>'
    doc += '<section><h2>固定规则</h2><p>原模型选平；原次选取真实即时平均欧赔主客低赔率，缺失或同价不猜。大小球取真实Bet365同阶段盘口两侧低水位（同水买小）；主结果要求两侧港赔水位均在(0,2]。无法判定的比赛保留在原入选总分母，不强行填值。</p><p>按总球高于盘口选大、低于盘口选小判命中。例如2.25盘口：3球买大命中，1球买小命中；2球买小赢半也属正收益命中。整数盘相等为走盘，不算命中。下表同时单列五档结算及仅全赢的更严格口径。</p></section>'
    names={'all':'全球（含竞彩）','sporttery':'竞彩','nonSporttery':'非竞彩'}
    for period,label in [('whole','全部历史'),('test','10月25日起时间区间')]:
        for phase,phase_label in [('latest','即时盘主结果'),('opening','初盘对照')]:
            doc += '<section><h2>'+label+' · '+phase_label+'</h2>'
            lines=[];settlements=[];joint=[]
            for scope,scope_label in names.items():
                b=report['periods'][period][scope][phase]
                lines.append([scope_label,b['selected'],f"{b['drawHits']} / {percent(b['drawHitRate'])}",f"{b['drawOrBackupHits']} / {percent(b['drawOrBackupRate'])}",f"{b['totalsAvailable']} / {b['selected']}",f"{b['totalsProfitableHits']} / {percent(b['totalsProfitableRate'])}",b['rescuedByTotals'],f"{b['originalThreeMarketUnionHits']} / {percent(b['originalThreeMarketUnionRate'])}"])
                s=b['totalsSettlements'];settlements.append([scope_label,b['totalsAvailable'],s['full_win'],s['half_win'],s['push'],s['half_loss'],s['full_loss'],b['ties'],percent(b['lowWaterUnitROI'])])
                joint.append([scope_label,f"{b['bothResultAndTotalsHits']}/{b['totalsAvailable']}",percent(b['bothResultAndTotalsRateSameTotalsPool']),percent(b['bothResultAndTotalsRateAllSelected']),f"{b['fullWinOnlyThreeMarketUnionHits']}/{b['selected']}",percent(b['fullWinOnlyThreeMarketUnionRate'])])
            doc += table(['范围','原入选','主选平命中／率','主或次覆盖／率','大小球有输出','大小球赢／有输出命中率','新增补覆盖','三项至少一中／率'],lines)
            doc += '<details><summary>五档结算、水位并列与大小球单独ROI</summary>'+table(['范围','大小球可评','全赢','赢半','走盘','输半','全输','同水买小','大小球单独ROI'],settlements)+'</details>'
            doc += '<details open><summary>要求两个市场同时命中，以及只算全赢的对照</summary>'+table(['范围','胜平负组合且大小球都中／大小球可评','同可评池联合命中率','占全部原入选','大小球只认全赢时三项覆盖','覆盖率'],joint)+'</details></section>'
    doc += '<section><h2>解释与边界</h2><p>全部历史包含原模型训练样本，只能核账；后续时间区间也已被多次研究。全部源盘口采集时间未知，不能将本报告视为真实赛前可下单收益。不同市场至少一项正确并不意味着组合下注盈利；这里只给大小球单独ROI，不假设投注本金分配。</p><p><a href="report.json">完整统计与来源哈希</a> · <a href="settled-rows.json">原入选逐场预测和结果</a> · <a href="independent.json">独立复核</a></p></section></main></html>'
    path.write_text(doc,encoding='utf-8')

def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--study',type=Path,default=ROOT/'reports/original-model-low-water-20261104/current-study.json')
    p.add_argument('--replay',type=Path,default=ROOT/'reports/original-model-low-water-20261104/original-replay.json')
    p.add_argument('--out',type=Path,default=ROOT/'reports/original-model-low-water-20261104')
    a=p.parse_args();a.out.mkdir(parents=True,exist_ok=True)
    source,replay=read(a.study),read(a.replay)
    assert sha(a.study)==replay['inputSHA256']
    predictions={r['matchId']:r for r in replay['rows']}
    assert len(predictions)==len(source['rows'])==len(replay['rows'])
    checks=legacy.checks()
    assert legacy.settle(3,2.25,'over',.9)==('full_win',.9)
    assert legacy.settle(1,2.25,'under',.9)==('full_win',.9)
    selected=[]
    for raw in source['rows']:
        pred=predictions[raw['matchId']]
        if not pred['selected']: continue
        assert pred['observedFeatures']>0
        h,g=raw['homeScore'],raw['awayScore'];result=1 if h==g else 0 if h>g else 2
        assert result==raw['result']
        backup=legacy.backup(raw);draw_hit=result==1;backup_hit=result!=1 and result==backup
        group='draw_hit' if draw_hit else 'alternative_hit' if backup_hit else 'alternative_unavailable_non_draw' if backup is None else 'both_miss'
        selected.append(dict(matchId=raw['matchId'],dateBjt=legacy.day(raw['matchTime']),scope='sporttery' if raw['jingcaiId'].strip() else 'nonSporttery',home=raw['home'],guest=raw['guest'],homeScore=h,awayScore=g,totalGoals=h+g,result=result,backup=backup,drawHit=draw_hit,backupHit=backup_hit,drawOrBackup=draw_hit or backup_hit,group=group,markets={phase:phase_result(raw,phase) for phase in ['opening','latest']}))
    periods={}
    for period,cut in [('whole',''),('test','2026-10-25'),('november','2026-11-01')]:
        periods[period]={}
        for scope in ['all','sporttery','nonSporttery']:
            rows=[r for r in selected if (not cut or r['dateBjt']>=cut) and (scope=='all' or r['scope']==scope)]
            periods[period][scope]={phase:summarize(rows,phase) for phase in ['opening','latest']}
    report=dict(version='original-draw-backup-low-water-v1',sourceGeneratedAt=source['generatedAt'],sourceMatches=len(source['rows']),selected=len(selected),sourceSHA256=sha(a.study),replaySHA256=sha(a.replay),modelSHA256=sha(ROOT/'go_server/internal/drawmodeldata/observation_v1.json'),modelVersion=replay['modelVersion'],periods=periods,checks=checks,semantics=dict(selection='original frozen draw model only',backup='same real latest average 1X2 home/away lower quote; no side on ties or unavailable source',totals='same Bet365 phase line and two HK water quotes; lower water, ties under; primary both in(0,2]',hit='over total>line, under total<line; full_win+half_win; push is not a hit',threeMarketUnion='draw OR original backup OR profitable low-water totals; not a single-market accuracy, parlay or portfolio ROI',bothMarkets='(draw OR backup) AND low-water totals; both all-candidate and same-totals-pool denominators shown'),servicesStarted=False,modelsTrained=False,originalSchemeChanged=False)
    report['sourceGeneratedAtBeijing']=legacy.datetime.fromisoformat(source['generatedAt'].replace('Z','+00:00')).astimezone(legacy.BJT).strftime('%Y-%m-%d %H:%M:%S')
    (a.out/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2,allow_nan=False),encoding='utf-8')
    (a.out/'settled-rows.json').write_text(json.dumps(selected,ensure_ascii=False,separators=(',',':'),allow_nan=False),encoding='utf-8')
    make_html(report,a.out/'report.html')
    print(json.dumps({'status':'passed','report':str(a.out/'report.html'),'periods':{p:{s:blocks['latest'] for s,blocks in scoped.items()} for p,scoped in periods.items()}},ensure_ascii=False,indent=2))

if __name__=='__main__': main()
