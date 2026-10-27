#!/usr/bin/env python3
"""Offline Chinese report for false-positive reduction studies.

Only renders supplied results. It never fits a model, changes a threshold,
reads source databases, starts a service, or opens a browser.
"""
from __future__ import annotations

import html
import json
import math
import re
from pathlib import Path


def _clean(value):
    if isinstance(value, dict):
        return {str(k): _clean(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_clean(v) for v in value]
    if hasattr(value, "tolist"):
        return _clean(value.tolist())
    if hasattr(value, "item"):
        return _clean(value.item())
    if isinstance(value, float) and not math.isfinite(value):
        return None
    if isinstance(value, Path):
        return str(value)
    return value


def _e(value):
    return html.escape(str(value if value is not None else "暂无"), quote=True)


def _number(value):
    return isinstance(value, (int, float)) and not isinstance(value, bool) and math.isfinite(value)


def _count(value):
    return f"{int(value):,}" if _number(value) else "暂无"


def _pct(value):
    return f"{value * 100:.2f}%" if _number(value) else "暂无"


def _difference(base, current, key):
    a, b = base.get(key), current.get(key)
    return a - b if _number(a) and _number(b) else None


def _rule_label(value):
    """Round displayed text only; the complete original is kept as its title."""
    text = str(value or "")
    text = re.sub(r"(概率[≥≤<>])([+-]?\d+\.\d{5,})",
                  lambda m: m[1] + f"{float(m[2])*100:.2f}%" if 0 <= float(m[2]) <= 1 else m[0], text)
    return re.sub(r"(?<![\w.])([+-]?\d+\.\d{5,})", lambda m: f"{float(m[1]):.4f}".rstrip("0").rstrip("."), text)


VIEWS = {"clean": "市场原值＋严格赛前历史", "all": "含前台派生（辅助）"}
ROLES = {"all": "全部时间区外复核", "development": "前3个开发时段", "lockedInternalAudit": "后2个锁定内部审计时段"}


def _method(ident, methods):
    return methods.get(str(ident), {}).get("label", str(ident or "暂无方法"))


def _target(case):
    target = case.get("target")
    fraction = case.get("targetFraction")
    if not _number(fraction):
        try:
            fraction = float(target) / 970
        except (TypeError, ValueError):
            fraction = None
    return f"{target}/970（{_pct(fraction)}）" if target is not None else "基础池原样保留"


def _metrics_cells(metric, base=None, removed_fp=None, lost_tp=None):
    metric, base = metric or {}, base or {}
    removed_fp = _difference(base, metric, "fp") if removed_fp is None else removed_fp
    lost_tp = _difference(base, metric, "tp") if lost_tp is None else lost_tp
    return "".join("<td>" + _e(v) + "</td>" for v in (
        _count(metric.get("tp")), _count(metric.get("fp")), _count(metric.get("fn")),
        _count(metric.get("selected")), _pct(metric.get("precision")), _pct(metric.get("recall")),
        _count(removed_fp), _count(lost_tp)))


METRIC_HEAD = "<th>保留平局 TP</th><th>误选非平 FP</th><th>漏平 FN</th><th>保留比赛</th><th>选中平局占比</th><th>实际平局覆盖</th><th>相对基础池减少FP</th><th>相对基础池损失TP</th>"


def _summary_table(rows, methods, caption=""):
    body = []
    for row in rows or []:
        meta = methods.get(str(row.get("method")), {})
        view = row.get("view", meta.get("view", ""))
        market = row.get("marketSameSize") or {}
        expected_tp, expected_fp = row.get("uniformExpectedTP"), row.get("uniformExpectedFP")
        reference = "暂无" if not market else f"TP {_count(market.get('tp'))} / FP {_count(market.get('fp'))}<small>实际选 {_count(market.get('selected'))} 场，边界并列保留</small>"
        uniform = "暂无" if not _number(expected_tp) else f"TP {expected_tp:.2f} / FP {expected_fp:.2f}" if _number(expected_fp) else f"TP {expected_tp:.2f}"
        label = _e(_method(row.get("method"), methods)) + "<small>" + _e(VIEWS.get(view, view)) + "</small>"
        status = "<small class='danger-text'>后期实际覆盖未达目标</small>" if row.get("targetReached") is False else ""
        if row.get("infeasibleFitFolds"):
            status += "<small>早期基础池目标不可达：" + _count(row["infeasibleFitFolds"]) + "折</small>"
        body.append("<tr><th>" + label + "</th><td>" + _e(_target(row)) + status + "</td>" +
                    _metrics_cells(row, row.get("base"), row.get("removedFP"), row.get("lostTP")) +
                    "<td>" + reference + "</td><td>" + _e(uniform) + "</td></tr>")
    if not body:
        return "<p>当前结果尚未提供此分区的比较。</p>"
    return "<p class='muted'>" + _e(caption) + "</p><div class='scroll'><table><thead><tr><th>方法 / 输入范围</th><th>早期训练/校准覆盖目标</th>" + METRIC_HEAD + "<th>同筛选数量的均赔排序</th><th>基础池内均匀缩量期望</th></tr></thead><tbody>" + "".join(body) + "</tbody></table></div>"


def _full_metric(case):
    metrics = case.get("metrics") or {}
    return metrics.get("full") or {}


def _full_table(full, methods):
    base = full.get("base") or {}
    if "full" in base:
        base = base["full"]
    body = ["<tr><th>原高覆盖基础池</th><td>未过滤</td>" + _metrics_cells(base, base) + "</tr>"] if base else []
    for case in full.get("paths") or []:
        metric = _full_metric(case)
        own_base = (case.get("base") or {}).get("full") or base
        label = _e(_method(case.get("method"), methods))
        view = case.get("view", methods.get(str(case.get("method")), {}).get("view", ""))
        label += "<small>" + _e(VIEWS.get(view, view)) + "</small>"
        reached = (case.get("targetReached") or {}).get("full")
        status = "<small class='danger'>未达到目标；保留失败结果</small>" if reached is False else ""
        body.append("<tr><th>" + label + "</th><td>" + _e(_target(case)) + status + "</td>" + _metrics_cells(metric, own_base) + "</tr>")
    return "<div class='scroll'><table><thead><tr><th>历史拟合方法</th><th>全量覆盖目标</th>" + METRIC_HEAD + "</tr></thead><tbody>" + "".join(body) + "</tbody></table></div>" if body else "<p>当前尚无全量拟合结果。</p>"


def _steps_table(case, phase):
    steps = case.get("steps") or []
    if not steps:
        if case.get("kind") == "score":
            return "<p>本路线按固定评分阈值保留比赛，不使用逐条排除规则。阈值：" + _e(case.get("threshold")) + "。</p>"
        return "<p>此路线没有加入排除规则，原样保留基础池；不能据此宣称过滤有效。</p>"
    rows = []
    for step in steps:
        metric = step.get(phase) or {}
        label = step.get("label", step.get("ruleId"))
        rows.append("<tr><td>" + _count(step.get("step")) + "</td><th class='rule' title='" + _e(label) + "'>" + _e(_rule_label(label)) +
                    "</th><td>" + _count(step.get("removedFP")) + "</td><td>" + _count(step.get("removedTP")) + "</td><td>" + _count(metric.get("selected")) +
                    "</td><td>" + _count(metric.get("tp")) + "</td><td>" + _count(metric.get("fp")) + "</td><td>" + _pct(metric.get("precision")) + "</td></tr>")
    return "<div class='scroll'><table><thead><tr><th>步</th><th>排除条件</th><th>本步删除非平</th><th>本步误删平</th><th>本步后保留</th><th>保留平</th><th>仍选非平</th><th>选中平占比</th></tr></thead><tbody>" + "".join(rows) + "</tbody></table></div>"


APP_JS = r"""
(function(){
 'use strict';
 const isDraw=r=>r&&(r.isDraw===true||r.isDraw===1);
 const idSet=ids=>new Set((Array.isArray(ids)?ids:[]).map(String));
 function compute(contexts,selectedIds,baseIds){
  const selected=idSet(selectedIds),base=idSet(baseIds),known=idSet(contexts.map(r=>r.matchId));
  let tp=0,fp=0,fn=0,tn=0,removedFP=0,lostTP=0,baseTP=0,baseFP=0,outsideBase=0;
  for(const r of contexts){const id=String(r.matchId),keep=selected.has(id),was=base.has(id),draw=isDraw(r);
   if(draw){if(keep)tp++;else fn++;if(was)baseTP++;if(was&&!keep)lostTP++;}
   else{if(keep)fp++;else tn++;if(was)baseFP++;if(was&&!keep)removedFP++;}
   if(keep&&!was)outsideBase++;
  }
  return {selected,base,tp,fp,fn,tn,removedFP,lostTP,baseTP,baseFP,outsideBase,
   selectedCount:tp+fp,precision:tp+fp?tp/(tp+fp):null,recall:tp+fn?tp/(tp+fn):null,
   unknownSelectedIds:[...selected].filter(id=>!known.has(id))};
 }
 function category(row,stats){const id=String(row.matchId),keep=stats.selected.has(id),was=stats.base.has(id),draw=isDraw(row);
  return keep?(draw?'retainedDraw':'retainedNonDraw'):was?(draw?'removedDraw':'removedNonDraw'):(draw?'baseMissedDraw':'outsideBaseNonDraw');}
 function filterRows(contexts,stats,mode,query){const needle=String(query||'').trim().toLowerCase(),out=[];
  for(let i=0;i<contexts.length;i++){const r=contexts[i],kind=category(r,stats);
   if(mode!=='all'&&(mode==='selected'?!stats.selected.has(String(r.matchId)):kind!==mode))continue;
   const text=[r.matchId,r.matchTime,r.date,r.league,r.home,r.away,r.score].map(v=>String(v==null?'':v)).join(' ').toLowerCase();
   if(needle&&!text.includes(needle))continue;out.push(i);
  }return out;
 }
 function paginate(indices,page,size){size=Math.max(1,Math.min(100,Number.isFinite(+size)?Math.floor(+size):25));const pages=Math.max(1,Math.ceil(indices.length/size));page=Math.max(0,Math.min(pages-1,Number.isFinite(+page)?Math.floor(+page):0));return {page,pages,size,total:indices.length,indices:indices.slice(page*size,(page+1)*size)};}
 const api={isDraw,idSet,compute,category,filterRows,paginate};
 if(typeof module!=='undefined'&&module.exports)module.exports=api;
 if(typeof document==='undefined')return;
 const payload=JSON.parse(document.getElementById('reduction-payload').textContent),contexts=payload.contexts||[],paths=payload.paths||[],baseIds=payload.baseSelectedMatchIds||[];
 const byId=id=>document.getElementById(id),pct=v=>v==null?'暂无':(v*100).toFixed(2)+'%',count=v=>Number(v).toLocaleString('zh-CN');
 const state={path:0,page:0,stats:null};
 const el=(tag,text,cls)=>{const e=document.createElement(tag);if(text!==undefined)e.textContent=text;if(cls)e.className=cls;return e;};
 function route(){return paths[state.path]||{};}
 function renderMetrics(){const r=route(),s=state.stats,items=[['基础池平局',s.baseTP],['基础池非平',s.baseFP],['保留平局 TP',s.tp],['仍选非平 FP',s.fp],['删除非平',s.removedFP],['误删平局',s.lostTP],['最终选择',s.selectedCount],['选中平占比',pct(s.precision)],['实际平局覆盖',pct(s.recall)]];
  byId('live-metrics').replaceChildren(...items.map(([label,value])=>{const card=el('div',undefined,'metric');card.append(el('span',label),el('strong',typeof value==='number'?count(value):value));return card;}));
  const alerts=[];if(s.outsideBase)alerts.push('结果包含 '+s.outsideBase+' 场基础池以外比赛，请核对路线输入，不能将它们算作原池过滤。');if(s.unknownSelectedIds.length)alerts.push('结果含未知比赛ID，未将其计入指标。');
  const expected=(r.metrics||{}).full||{};if(['tp','fp','fn','tn'].some(k=>expected[k]!==undefined&&expected[k]!==s[k]))alerts.push('复算指标与提供的路线结果不一致，请核对。');
  if(contexts.length&&s.selectedCount/contexts.length>=.9)alerts.push('仍然接近全选：覆盖高可能主要来自保留大部分比赛，须同时看误选非平。');
  byId('live-alert').replaceChildren(...alerts.map(t=>el('p',t,'alert danger')));
  byId('live-summary').textContent='当前仅查看既有全量历史拟合路线，'+(r.title||r.method||'暂无路线')+'。搜索只筛明细，不改变全样本指标，也不重新训练或调阈值。';
 }
 function renderRows(){const filtered=filterRows(contexts,state.stats,byId('match-mode').value,byId('match-search').value),page=paginate(filtered,state.page,byId('page-size').value);state.page=page.page;
  const body=byId('match-rows');body.replaceChildren();const labels={retainedDraw:'保留平局',retainedNonDraw:'仍误选非平',removedDraw:'误删平局',removedNonDraw:'删除非平',baseMissedDraw:'基础池本已漏平',outsideBaseNonDraw:'基础池外非平'};
  for(const i of page.indices){const r=contexts[i],kind=category(r,state.stats),tr=el('tr'),time=el('td',r.matchTime||r.date||'');time.append(el('small',r.league||''));const teams=el('td',(r.home||'')+' / '+(r.away||''));teams.append(el('small','比赛ID '+r.matchId));tr.append(time,teams,el('td',r.score||''),el('td',labels[kind],kind==='removedDraw'?'danger-text':''));body.append(tr);}
  if(!page.indices.length){const tr=el('tr'),td=el('td','没有符合当前类别与搜索条件的比赛');td.colSpan=4;tr.append(td);body.append(tr);}
  byId('page-info').textContent='共 '+count(page.total)+' 场 · 第 '+(page.page+1)+' / '+page.pages+' 页 · 每页最多 '+page.size+' 场';byId('page-prev').disabled=page.page===0;byId('page-next').disabled=page.page+1>=page.pages;
 }
 function switchRoute(){state.path=+byId('path-choice').value||0;state.page=0;state.stats=compute(contexts,route().selectedMatchIds,baseIds);renderMetrics();renderRows();}
 byId('path-choice').replaceChildren(...paths.map((r,i)=>{const o=el('option',r.title||r.method||String(i+1));o.value=String(i);return o;}));
 byId('path-choice').addEventListener('change',switchRoute);for(const name of ['match-mode','page-size'])byId(name).addEventListener('change',()=>{state.page=0;renderRows();});byId('match-search').addEventListener('input',()=>{state.page=0;renderRows();});byId('page-prev').addEventListener('click',()=>{state.page--;renderRows();});byId('page-next').addEventListener('click',()=>{state.page++;renderRows();});
 if(paths.length){const firstRule=paths.findIndex(r=>r.kind==='rule');byId('path-choice').value=String(firstRule>=0?firstRule:0);switchRoute();}else{byId('live-summary').textContent='当前尚未提供逐场保留名单。统计比较和阶段结果仍列在页面中。';byId('path-choice').disabled=true;}
})();
"""


CSS = """
body{font:14px/1.65 system-ui,sans-serif;background:#111a23;color:#e1eaf2;max-width:1850px;margin:auto;padding:24px}h1,h2,h3{line-height:1.3}h2{margin-top:36px}a{color:#9acbff}table{border-collapse:collapse;width:100%}th,td{border:1px solid #3b4c5b;padding:10px;vertical-align:top;text-align:left}th{background:#24394b}thead th{position:sticky;top:0}small{display:block;color:#b0c3d2}td{min-width:90px}.rule{min-width:300px}.scroll{overflow:auto;max-height:650px}.alert{padding:14px 18px;background:#223b50;border-left:4px solid #78b7ec}.danger{background:#493425;border-color:#eba268}.danger-text{color:#ffad94}.muted{color:#b2c2cf}.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(145px,1fr));gap:10px;margin:16px 0}.metric{padding:12px;background:#203345;border:1px solid #385164;border-radius:6px}.metric span{display:block;color:#b4c8d7}.metric strong{font-size:20px}.controls{display:flex;gap:10px;flex-wrap:wrap;margin:12px 0;align-items:center}input,select,button{background:#253b4c;color:#edf5fa;border:1px solid #6c8ba1;padding:9px;border-radius:4px}input{min-width:260px}button:disabled{opacity:.4}pre{white-space:pre-wrap;word-break:break-word;font:12px/1.6 ui-monospace,monospace}summary{cursor:pointer;font-weight:700}details{margin:14px 0;padding:10px;border:1px solid #3b4c5b}footer{margin:32px 0;color:#a9bdcc}.interactive{padding:18px;background:#172a39;border:1px solid #456174}
"""


def render_report(analysis, contexts, out_path):
    """Render a self-contained report from already computed result records."""
    analysis, contexts = _clean(analysis or {}), _clean(contexts or [])
    counts = analysis.get("counts") or {}
    methods = {str(m.get("id")): m for m in (analysis.get("methods") or []) if isinstance(m, dict)}
    full, temporal = analysis.get("fullHistory") or {}, analysis.get("temporal") or {}
    summaries = temporal.get("summaries") or []
    title = "高覆盖平局池：减少非平误选"
    doc = "<!doctype html><html lang='zh-CN'><head><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>" + title + "</title><style>" + CSS + "</style></head><body><main><h1>" + title + "</h1>"
    doc += "<p>从既有高覆盖基础池出发，比较反向排除规则、评分排序与均赔排序：每减少多少非平，同时会误删多少平。</p>"
    doc += "<div class='metrics'>" + "".join("<div class='metric'><span>" + label + "</span><strong>" + _count(value) + "</strong></div>" for label, value in (("完赛比赛", counts.get("games")), ("平局", counts.get("draws")), ("非平局", counts.get("nonDraws")))) + "</div>"
    doc += "<p class='alert danger'>900、850、800 都以全部970场平局为比例基准。例如900/970＝92.78%。时间复核在各自早期校准区使用这个覆盖比例定规则或阈值，再原样用于后面的比赛；后期实际覆盖可能达不到目标，不能把它写成后期找到了900场。</p>"
    doc += "<p class='alert'>过滤器和评分器的输入分为市场原值＋严格赛前历史，与另加前台派生的辅助路线；各路线使用相同基础池作比较。前台派生含模拟、散户心理与旧平台复合信号，不能当作额外独立证据。所有日期此前已查看，盘口采集也未证明早于开球；本页是历史拟合与重复内部复核，不是新赛前成绩。</p>"
    temporal_base = next((r for r in summaries if r.get("method") == "base" and r.get("role") == "all"), None)
    if temporal_base and _number(temporal_base.get("draws")) and _number(counts.get("draws")):
        warmup = counts["draws"] - temporal_base["draws"]
        doc += "<p>全部" + _count(counts["draws"]) + "场平局均用于全量描述；时间区外复核共" + _count(temporal_base.get("games")) + "场比赛、" + _count(temporal_base["draws"]) + "场平局，另有" + _count(warmup) + "场平局只作最早训练，不虚构其区外成绩。</p>"
    all_rule = next((r for r in summaries if r.get("method") == "rule_clean_ratio" and str(r.get("target")) == "900" and r.get("role") == "all"), None)
    audit_rule = next((r for r in summaries if r.get("method") == "rule_clean_ratio" and str(r.get("target")) == "900" and r.get("role") == "lockedInternalAudit"), None)
    if all_rule and audit_rule:
        failure = "，全程实际覆盖未达到目标" if all_rule.get("targetReached") is False else ""
        doc += "<p class='alert danger'>目标900/970的市场＋战绩反向排除路线：全部5块区外保留" + _count(all_rule.get("tp")) + "/" + _count(all_rule.get("draws")) + "平（" + _pct(all_rule.get("recall")) + "）" + failure + "；最后2块保留" + _count(audit_rule.get("tp")) + "/" + _count(audit_rule.get("draws")) + "平（" + _pct(audit_rule.get("recall")) + "）。不能只用最后2块较好的数字替代全程结论，也不能据此更换开发区已选定的方法。</p>"
        audit_cases = [c for f in (temporal.get("folds") or []) if f.get("role") == "lockedInternalAudit" for c in (f.get("cases") or []) if c.get("method") == "rule_clean_ratio" and str(c.get("target")) == "900"]
        inactive = sum(c.get("trainTargetReachable") is False or c.get("calibrationTargetReachable") is False for c in audit_cases)
        if inactive:
            doc += "<p class='alert'>这条路线最后2块中有" + _count(inactive) + "块早期基础池目标不可达，因而没有过滤；不能认为每个时段都减少了误选。该段合计减少" + _count(audit_rule.get("removedFP")) + "场非平、误删" + _count(audit_rule.get("lostTP")) + "场平。</p>"
    doc += "<h2>全部5块时间区外复核：先看全程目标是否达成</h2>"
    doc += _summary_table([r for r in summaries if r.get("role") == "all"], methods, "全部区外结果包括前3块开发与后2块锁定内部审计。目标分母是各区间全部平局；基础池已漏平仍计入漏平，不能重新缩小分母。")
    doc += "<h2>最后2块锁定内部审计：同一早期覆盖目标下的实际代价</h2>"
    doc += _summary_table([r for r in summaries if r.get("role") == "lockedInternalAudit"], methods, "先看后2个时段的实际TP、FP与误删平；目标相同不代表后期实际TP相同。减少FP和损失TP均相对同一方法在该时段的基础池。")
    selection = temporal.get("selection") or []
    if selection:
        doc += "<h3>仅由开发时段选定的路线</h3><p>以下选择只使用前3个开发时段。达不到覆盖要求时照实列失败，不根据后2个时段重选。</p>"
        for item in selection:
            audit = item.get("audit") or {}
            note = "<p class='danger-text'>锁定内部审计的实际覆盖未达到这个目标，不能写成目标已实现。</p>" if audit.get("targetReached") is False else ""
            doc += "<details open><summary>目标 " + _e(item.get("target")) + "/970 · " + _e(_method(item.get("method"), methods)) + "</summary><p>仅由前3个开发时段选择：达到覆盖目标者先比较误选FP；若均未达到，先比较漏平缺口，再比较FP。后2个时段不参与重选。</p>" + note + _summary_table([audit] if audit else [], methods, "开发时段选定方法的锁定内部审计结果。") + "<details><summary>完整选型记录</summary><pre>" + _e(json.dumps(item, ensure_ascii=False, indent=2)) + "</pre></details></details>"
    doc += "<h2>全量历史拟合：说明能删到什么程度</h2><p class='alert danger'>本区允许使用全部已知结局组装排除链，或拟合模型并定阈值。因此它只说明这份历史样本能拟合到什么程度，不能与后面的时间复核混为同一份验证。</p>" + _full_table(full, methods)
    doc += "<h2>逐条排除的成本</h2><p>规则路线统一为：保留＝基础池 AND NOT（排除条件1 OR 排除条件2…）。缺少规则输入时不排除；真实0仍参与比较。本步删除非平与误删平按去重后新增部分计算。条件显示值已四舍五入，实际判定保持原始精度；悬停条件可查看完整值。</p>"
    for case in full.get("paths") or []:
        if case.get("kind") == "base":
            continue
        doc += "<details><summary>历史拟合 · " + _e(_method(case.get("method"), methods)) + " · " + _e(_target(case)) + "</summary>" + _steps_table(case, "full") + "</details>"
    paths = []
    for case in full.get("paths") or []:
        if isinstance(case.get("selectedMatchIds"), list):
            payload_case = {k: case.get(k) for k in ("method", "target", "view", "kind", "metrics", "selectedMatchIds")}
            payload_case["title"] = _method(case.get("method"), methods) + " · " + _target(case)
            paths.append(payload_case)
    doc += """<h2>查看哪些非平被删除、哪些平被误删</h2><div class='interactive'><div class='controls'><label for='path-choice'>既有历史拟合路线</label><select id='path-choice'></select></div><p id='live-summary' class='muted'></p><div id='live-metrics' class='metrics'></div><div id='live-alert'></div><div class='controls'><select id='match-mode' aria-label='比赛类别'><option value='removedNonDraw'>删除的非平</option><option value='removedDraw'>误删的平局</option><option value='retainedNonDraw'>仍误选的非平</option><option value='retainedDraw'>保留的平局</option><option value='baseMissedDraw'>基础池本已漏平</option><option value='selected'>所有保留比赛</option><option value='all'>全部比赛</option></select><input id='match-search' type='search' placeholder='比赛ID、球队、联赛、日期'><select id='page-size' aria-label='每页数量'><option>25</option><option>50</option><option>100</option></select></div><div class='scroll'><table><thead><tr><th>时间 / 联赛</th><th>主队 / 客队</th><th>实际比分</th><th>类别</th></tr></thead><tbody id='match-rows'></tbody></table></div><div class='controls'><button id='page-prev'>上一页</button><span id='page-info'></span><button id='page-next'>下一页</button></div><p class='muted'>每页最多100场。基础池本已漏平与过滤新增误删分开；搜索不改变上方全样本指标。</p></div>"""
    doc += "<h2>五个时间块：开发与区外复核完整结果</h2>"
    for role in ("development",):
        doc += "<details><summary>" + ROLES[role] + "</summary>" + _summary_table([r for r in summaries if r.get("role") == role], methods, "均赔对照在同一基础池内按相同筛选数量排序，保留边界并列并报告实际选量；均匀缩量期望用于识别改善是否只是来自少选比赛。") + "</details>"
    for fold in temporal.get("folds") or []:
        body = "<pre>" + _e(json.dumps({k: fold.get(k) for k in ("fold", "role", "baseRuleIds", "base")}, ensure_ascii=False, indent=2)) + "</pre>"
        for case in fold.get("cases") or []:
            unreachable = "<p class='alert danger'>早期训练或校准基础池本来就不足以达到目标，因此原样保留基础池，不强行排除、补选或改用更小平局分母。</p>" if case.get("trainTargetReachable") is False or case.get("calibrationTargetReachable") is False else ""
            body += "<details><summary>" + _e(_method(case.get("method"), methods)) + " · " + _e(_target(case)) + "</summary>" + unreachable + "<p>规则资格、次序和阈值只使用更早的训练/校准；测试只报告结果，不补条件。下面逐步表列训练区成本，各分区结果见上方明细。</p><pre>" + _e(json.dumps({k: case.get(k) for k in ("targetReached", "trainTargetReachable", "calibrationTargetReachable", "stopReason", "threshold", "metrics", "qualifiedRules")}, ensure_ascii=False, indent=2)) + "</pre>" + _steps_table(case, "train") + "</details>"
        doc += "<details><summary>第 " + _e(fold.get("fold")) + " 块 · " + _e(ROLES.get(fold.get("role"), fold.get("role"))) + "</summary>" + body + "</details>"
    for key, label in (("warnings", "解释边界"), ("protocol", "固定研究口径"), ("sourceHashes", "来源与版本核查")):
        doc += "<details" + (" open" if key == "warnings" else "") + "><summary>" + label + "</summary><pre>" + _e(json.dumps(analysis.get(key), ensure_ascii=False, indent=2)) + "</pre></details>"
    doc += "<footer>TP＝保留平局，FP＝误选非平，FN＝漏平。覆盖率＝保留平局／该评价区全部平局；选中平占比＝保留平局／全部保留比赛。删除非平与误删平是相对基础池的变化。早期方向筛选未经显著性保证，贪心与阈值路线不保证全局最优或未来覆盖。</footer>"
    payload = {"contexts": contexts, "paths": paths, "baseSelectedMatchIds": full.get("baseSelectedMatchIds") or []}
    encoded = json.dumps(payload, ensure_ascii=False, separators=(",", ":"), allow_nan=False).replace("<", r"\u003c").replace(">", r"\u003e").replace("&", r"\u0026").replace(chr(0x2028), r"\u2028").replace(chr(0x2029), r"\u2029")
    doc += "<script id='reduction-payload' type='application/json'>" + encoded + "</script><script>" + APP_JS + "</script></main></body></html>"
    path = Path(out_path)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(doc, encoding="utf-8")
    return path
