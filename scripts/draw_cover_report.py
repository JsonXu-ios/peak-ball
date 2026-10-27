#!/usr/bin/env python3
"""Offline, self-contained HTML for retrospective draw-rule OR coverage.

render_report(analysis, contexts, rule_matches, out_path) never runs a model,
reads source databases, changes candidates or starts a service.
"""
from __future__ import annotations

import html
import json
import math
from pathlib import Path


def _clean(value):
    if isinstance(value, dict):
        return {str(key): _clean(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_clean(item) for item in value]
    if hasattr(value, "tolist"):
        return _clean(value.tolist())
    if hasattr(value, "item"):
        return _clean(value.item())
    if isinstance(value, float) and not math.isfinite(value):
        return None
    if isinstance(value, Path):
        return str(value)
    return value


def _text(value):
    return html.escape(str(value if value is not None else "暂无"), quote=True)


def _percent(value):
    return f"{float(value)*100:.2f}%" if isinstance(value, (int, float)) and math.isfinite(value) else "暂无"


def _count(value):
    return f"{int(value):,}" if isinstance(value, (int, float)) and math.isfinite(value) else "暂无"


def _pool(value):
    return {"broad": "宽口径池", "suspected": "疑似池"}.get(str(value), str(value))


def _path_title(path):
    method = {"pure": "纯覆盖", "penalized": "误选成本", "fpbudget": "误选预算"}.get(path.get("method"), str(path.get("method", "路线")))
    suffix = ""
    if path.get("method") == "penalized":
        suffix = f" λ={path.get('penalty', 0):g}"
    elif path.get("method") == "fpbudget" and path.get("fpBudget") is not None:
        suffix = " · 最多误选 " + _count(path["fpBudget"]) + " 场非平"
    return _pool(path.get("pool")) + " · " + method + suffix


def _metrics_cells(item):
    return "".join("<td>" + value + "</td>" for value in (
        _count(item.get("tp")), _count(item.get("fp")), _count(item.get("fn")),
        _count(item.get("selected")), _percent(item.get("recall")),
        _percent(item.get("precision")), _percent(item.get("specificity"))))


def _steps_table(path):
    rows = []
    for step in path.get("steps") or []:
        rows.append("<tr><td>" + _count(step.get("step")) + "</td><th class='rule-name'>" + _text(step.get("label", step.get("ruleId"))) +
                    "</th><td>" + _count(step.get("newTP")) + "</td><td>" + _count(step.get("newFP")) + "</td>" + _metrics_cells(step) + "</tr>")
    if not rows:
        rows.append("<tr><td colspan='11'>此成本条件没有选入规则；覆盖为零，保留该结果。</td></tr>")
    return "<div class='scroll'><table><thead><tr><th>步</th><th>新增规则</th><th>新覆盖平局</th><th>新误选非平</th><th>累计覆盖平局</th><th>累计误选非平</th><th>漏平</th><th>累计选择</th><th>平局覆盖率</th><th>入选中平局占比</th><th>非平排除率</th></tr></thead><tbody>" + "".join(rows) + "</tbody></table></div>"


def _route_tables(analysis):
    paths = analysis.get("paths") or []
    counts = analysis.get("counts") or {}
    games = counts.get("games") or 0
    milestones = sorted({str(key) for path in paths for key in (path.get("milestones") or {})}, key=lambda value: (not value.isdigit(), int(value) if value.isdigit() else value))
    headings = "<th>路线</th><th>规则数</th><th>覆盖平局</th><th>误选非平</th><th>漏平</th><th>选择场数</th><th>平局覆盖率</th><th>入选中平局占比</th><th>非平排除率</th>"
    headings += "".join("<th>覆盖 " + _text(value) + " 平局</th>" for value in milestones)
    rows, details = [], []
    for path in paths:
        final = path.get("final") or {}
        selection = final.get("selected") or 0
        saturated = bool(path.get("nearAllSelection")) or bool(games and selection/games >= .90)
        label = _text(_path_title(path)) + ("<small class='danger-text'>近全选</small>" if saturated else "")
        cells = "<th>" + label + "</th><td>" + str(len(path.get("steps") or [])) + "</td>" + _metrics_cells(final)
        for target in milestones:
            reached = (path.get("milestones") or {}).get(target)
            if reached:
                cells += "<td>第 " + _count(reached.get("step")) + " 步<small>选 " + _count(reached.get("selected")) + "，误选 " + _count(reached.get("fp")) + "</small></td>"
            else:
                cells += "<td class='danger-text'>未达到</td>"
        rows.append("<tr>" + cells + "</tr>")
        bound = path.get("upperBound") or {}
        note = "本池候选在当前数据上的 OR 可覆盖 " + _count(bound.get("tp")) + " 场平局；这是本次有限候选池的覆盖范围，不能称为所有算法的理论上限。"
        alert = ""
        if saturated:
            alert += "<p class='alert danger'>近全选：选入 " + _count(selection) + " / " + _count(games) + " 场，误选非平 " + _count(final.get("fp")) + " 场。高覆盖不能单独视为识别能力。</p>"
        flags = path.get("complementFlags") or []
        if flags:
            alert += "<div class='alert danger'><b>互补分支警报</b><pre>" + _text(json.dumps(flags, ensure_ascii=False, indent=2)) + "</pre></div>"
        body = "<p>" + _text(note) + "</p>" + alert + _steps_table(path)
        if path.get("method") == "pure":
            details.append("<section><h3>" + _text(_path_title(path)) + "：每步去重增量</h3>" + body + "</section>")
        else:
            details.append("<details><summary>" + _text(_path_title(path)) + " · 覆盖 " + _count(final.get("tp")) + " 平局 / 误选 " + _count(final.get("fp")) + " 非平</summary>" + body + "</details>")
    overview = "<div class='scroll'><table><thead><tr>" + headings + "</tr></thead><tbody>" + "".join(rows) + "</tbody></table></div>"
    return overview, "".join(details)


def _temporal_table(analysis):
    temporal = analysis.get("temporal") or {}
    rows = temporal.get("summaries") or []
    preferred = ("pool", "target", "role", "games", "draws", "tp", "fp", "fn", "selected", "precision", "recall", "specificity")
    labels = {"pool": "候选池", "target": "训练前缀覆盖目标", "role": "时段角色", "games": "比赛", "draws": "实际平局", "tp": "覆盖平局", "fp": "误选非平", "fn": "漏平", "selected": "选入比赛", "precision": "入选中平局占比", "recall": "平局覆盖率", "specificity": "非平排除率"}
    if not rows:
        return "<p>当前数据未提供时间复核汇总。</p>"
    keys = [key for key in preferred if any(key in row for row in rows if isinstance(row, dict))]
    keys += sorted({key for row in rows if isinstance(row, dict) for key in row if key not in keys and not isinstance(row[key], (dict, list))})
    body = []
    for row in rows:
        cells = []
        for key in keys:
            value = row.get(key)
            if key in ("precision", "recall", "specificity"):
                text = _percent(value)
            elif key == "pool":
                text = _pool(value)
            elif key == "role":
                text = {"all": "全部时间复核", "development": "开发时段", "audit": "内部审计时段", "lockedInternalAudit": "锁定内部审计时段"}.get(value, value)
            elif key == "target" and str(value).isdigit() and (analysis.get("counts") or {}).get("draws"):
                total = analysis["counts"]["draws"]
                text = str(value) + "/" + str(total) + "（" + _percent(int(value)/total) + "）"
            else:
                text = value
            cells.append("<td>" + _text(text) + "</td>")
        body.append("<tr>" + "".join(cells) + "</tr>")
    warning = "<p class='alert'>" + _text(temporal.get("warning", "时间复核依然使用已有历史快照，是内部复核，不是新的赛前独立验证。")) + "</p><p>目标栏表示训练时按相应平局覆盖比例确定规则前缀，例如900/970表示92.78%；它不是要求后续复核时段覆盖900场，也不保证训练池达到该比例。某折训练目标未达时锁定该折全部可达前缀，后续复核区不补规则。复核时段的实际场数、平局数和覆盖数单独列示。</p>"
    return warning + "<div class='scroll'><table><thead><tr>" + "".join("<th>" + _text(labels.get(key, key)) + "</th>" for key in keys) + "</tr></thead><tbody>" + "".join(body) + "</tbody></table></div>"


def _missed_breakdown(analysis):
    breakdown = analysis.get("missedBreakdown") or {}
    if not breakdown:
        return ""
    labels = {
        "no_raw_candidate_input_qualification": "未达本次候选输入资格（含历史最低样本不足）",
        "no_fixed_business_rule_match": "具备候选资格，但不符合任何固定业务条件",
        "matches_only_early_excluded_rules": "匹配宽口径规则，但未通过早期训练/校准正提升资格",
        "matched_not_selected": "匹配候选，但未被本条路线选入",
    }
    rows = []
    for pool, reasons in breakdown.items():
        for key, value in (reasons or {}).items():
            rows.append("<tr><th>" + _text(_pool(pool)) + "</th><td>" + _text(labels.get(key, key)) + "</td><td>" + _count(value) + "</td></tr>")
    return "<h2>漏掉的平局为什么未被选入</h2><p>候选资格不足不等于完全没有数据。历史样本未达到本次候选的最低场数，和已有输入却不匹配固定业务条件，是两类不同原因。</p><div class='scroll'><table><thead><tr><th>候选池</th><th>漏平原因</th><th>场数</th></tr></thead><tbody>" + "".join(rows) + "</tbody></table></div>"


APP_JS = r"""
(function () {
  'use strict';
  const isDraw = row => row && (row.isDraw === true || row.isDraw === 1);
  function pathRuleIds(path) {
    return [...new Set((path && Array.isArray(path.steps) ? path.steps : []).map(s => s.ruleId).filter(id => typeof id === 'string' && id.length))];
  }
  function compileMatches(contexts, matches) {
    const result = Object.create(null);
    for (const [id, entries] of Object.entries(matches || {})) {
      const mask = new Uint8Array(contexts.length);
      if (Array.isArray(entries)) for (const index of entries) if (Number.isInteger(index) && index >= 0 && index < mask.length) mask[index] = 1;
      result[id] = mask;
    }
    return result;
  }
  function computeUnion(contexts, compiled, ruleIds) {
    const mask = new Uint8Array(contexts.length);
    for (const id of new Set(ruleIds)) {
      const source = compiled[id];
      if (!source) continue;
      for (let index = 0; index < mask.length; index++) if (source[index]) mask[index] = 1;
    }
    let tp=0, fp=0, fn=0, tn=0;
    for (let index=0; index<contexts.length; index++) {
      if (isDraw(contexts[index])) { if (mask[index]) tp++; else fn++; }
      else { if (mask[index]) fp++; else tn++; }
    }
    return {mask, tp, fp, fn, tn, selected:tp+fp,
      precision:tp+fp ? tp/(tp+fp) : null,
      recall:tp+fn ? tp/(tp+fn) : null,
      specificity:fp+tn ? tn/(fp+tn) : null};
  }
  function category(row, selected) {
    return isDraw(row) ? (selected ? 'tp' : 'fn') : (selected ? 'fp' : 'tn');
  }
  function filterRows(contexts, stats, mode, query) {
    const needle=String(query || '').trim().toLowerCase(), result=[];
    for (let index=0; index<contexts.length; index++) {
      const row=contexts[index], selected=!!stats.mask[index], kind=category(row, selected);
      if (mode==='selected' && !selected || ['tp','fp','fn','tn'].includes(mode) && kind!==mode) continue;
      const text=[row.matchId,row.date,row.matchTime,row.league,row.home,row.away,row.score].map(v=>String(v == null ? '' : v)).join(' ').toLowerCase();
      if (needle && !text.includes(needle)) continue;
      result.push(index);
    }
    return result;
  }
  function pageIndices(indices, page, size) {
    size=Math.max(1,Math.min(100,Number.isFinite(+size)?Math.floor(+size):25));
    const pages=Math.max(1,Math.ceil(indices.length/size));
    page=Math.max(0,Math.min(pages-1,Number.isFinite(+page)?Math.floor(+page):0));
    return {page,pages,size,indices:indices.slice(page*size,(page+1)*size),total:indices.length};
  }
  const api={isDraw,pathRuleIds,compileMatches,computeUnion,category,filterRows,pageIndices};
  if (typeof module !== 'undefined' && module.exports) module.exports=api;
  if (typeof document === 'undefined') return;
  const payload=JSON.parse(document.getElementById('cover-payload').textContent);
  const data=payload.analysis, contexts=payload.contexts || [], paths=data.paths || [];
  const compiled=compileMatches(contexts,payload.ruleMatches), rules=new Map((data.rules || []).map(r=>[r.id,r]));
  const byId=id=>document.getElementById(id), pct=v=>v==null?'暂无':(v*100).toFixed(2)+'%', count=v=>Number(v).toLocaleString('zh-CN');
  const pool=p=>p==='broad'?'宽口径池':p==='suspected'?'疑似池':p;
  const method=p=>p.method==='pure'?'纯覆盖':p.method==='penalized'?'误选成本 λ='+p.penalty:p.method==='fpbudget'?'最多误选 '+count(p.fpBudget)+' 场非平':p.method;
  const state={pathIndex:0,selected:new Set(),stats:null,page:0};
  function element(tag,text,className) {const e=document.createElement(tag);if(text!==undefined)e.textContent=text;if(className)e.className=className;return e;}
  function currentPath(){return paths[state.pathIndex] || {steps:[]};}
  function currentIds(){return pathRuleIds(currentPath());}
  function renderMetrics(){
    const s=state.stats, metrics=[['覆盖平局 TP',count(s.tp)],['误选非平 FP',count(s.fp)],['漏选平局 FN',count(s.fn)],['正确排除非平 TN',count(s.tn)],['选择比赛',count(s.selected)+' / '+count(contexts.length)],['平局覆盖率',pct(s.recall)],['入选中平局占比',pct(s.precision)],['非平排除率',pct(s.specificity)]];
    byId('live-metrics').replaceChildren(...metrics.map(([label,value])=>{const card=element('div',undefined,'metric');card.append(element('span',label),element('strong',value));return card;}));
    const alerts=[], share=contexts.length?s.selected/contexts.length:0;
    if(share>=.90) alerts.push('近全选警报：当前组合选入 '+pct(share)+' 的比赛，误选 '+count(s.fp)+' 场非平局；高召回主要来自扩大选择范围。');
    if(s.selected===contexts.length && contexts.length) alerts.push('当前组合等于全选：没有排除任何非平局。');
    const path=currentPath(), missing=currentIds().filter(id=>!compiled[id]);
    if(missing.length) alerts.push('缺少 '+missing.length+' 条规则的匹配明细，当前实时计算只包含有明细的规则。');
    if((path.complementFlags || []).length) alerts.push('此原始路线存在互补分支警报；取消部分勾选不代表已重新完成互补性检验。');
    if(!state.selected.size) alerts.push('当前未勾选任何规则，选择为零；精度分母为空。');
    if(state.selected.size===currentIds().length && !missing.length && path.final && ['tp','fp','fn','tn'].some(k=>path.final[k]!==undefined && path.final[k]!==s[k])) alerts.push('数据核对警报：实时OR与文件中的路线终值不同，请核对匹配明细。');
    byId('live-alert').replaceChildren(...alerts.map(text=>element('p',text,'alert danger')));
    byId('live-summary').textContent='当前 '+state.selected.size+' / '+currentIds().length+' 条规则；所有比赛按OR去重。勾选调整是已知历史结果上的人工探索，不是新验证。';
  }
  function renderMatches(){
    const indices=filterRows(contexts,state.stats,byId('match-mode').value,byId('match-search').value);
    const page=pageIndices(indices,state.page,byId('page-size').value);state.page=page.page;
    const body=byId('match-rows');body.replaceChildren();
    const names={tp:'命中平局',fn:'漏选平局',fp:'误选非平',tn:'正确排除非平'};
    for(const index of page.indices){
      const row=contexts[index], kind=category(row,!!state.stats.mask[index]), tr=element('tr');
      const time=element('td',String(row.matchTime || row.date || ''));time.append(element('small',String(row.league || '')));
      const teams=element('td',String(row.home || '')+' / '+String(row.away || ''));teams.append(element('small','比赛ID '+String(row.matchId || '')));
      const active=currentIds().filter(id=>state.selected.has(id)&&compiled[id]&&compiled[id][index]);
      const label=active.map(id=>'#'+(currentIds().indexOf(id)+1)).join('、') || '—';
      const hits=element('td',label);hits.title=active.map(id=>(rules.get(id)||{}).label || id).join('\n');
      tr.append(time,teams,element('td',String(row.score || '')),element('td',names[kind],kind==='fp'?'danger-text':kind==='tp'?'success-text':''),hits);body.append(tr);
    }
    if(!page.indices.length){const tr=element('tr'),td=element('td','没有符合当前类别与搜索条件的比赛');td.colSpan=5;tr.append(td);body.append(tr);}
    byId('page-info').textContent='共 '+count(page.total)+' 场 · 第 '+(page.page+1)+' / '+page.pages+' 页 · 每页最多 '+page.size+' 场';
    byId('page-prev').disabled=page.page===0;byId('page-next').disabled=page.page>=page.pages-1;
  }
  function refresh(){state.stats=computeUnion(contexts,compiled,[...state.selected]);renderMetrics();renderMatches();}
  function choosePath(index){
    state.pathIndex=index;state.page=0;state.selected=new Set(currentIds().filter(id=>compiled[id]));
    const list=byId('rule-options');list.replaceChildren();
    currentIds().forEach((id,index)=>{
      const step=(currentPath().steps || []).find(s=>s.ruleId===id) || {}, record=rules.get(id) || {}, label=element('label',undefined,'rule-option'),input=element('input');
      input.type='checkbox';input.checked=state.selected.has(id);input.disabled=!compiled[id];input.dataset.ruleId=id;
      input.addEventListener('change',()=>{if(input.checked)state.selected.add(id);else state.selected.delete(id);state.page=0;refresh();});
      const text=element('span','#'+(index+1)+' '+String(step.label || record.label || id));
      text.append(element('small','原路径此步新增：平局 '+(step.newTP ?? '暂无')+'，非平 '+(step.newFP ?? '暂无')+'。'+(!compiled[id]?' 匹配明细缺失。':'')));
      label.append(input,text);list.append(label);
    });
    if(!currentIds().length)list.append(element('p','此路线未选入规则，保留零覆盖结果。'));
    refresh();
  }
  paths.forEach((path,index)=>{const option=element('option',pool(path.pool)+' · '+method(path)+' · 原路径覆盖 '+((path.final||{}).tp ?? '暂无')+' 平局');option.value=String(index);byId('path-choice').append(option);});
  byId('path-choice').addEventListener('change',event=>choosePath(Number(event.target.value)));
  byId('rules-all').addEventListener('click',()=>{state.selected=new Set(currentIds().filter(id=>compiled[id]));for(const input of byId('rule-options').querySelectorAll('input'))input.checked=state.selected.has(input.dataset.ruleId);state.page=0;refresh();});
  byId('rules-none').addEventListener('click',()=>{state.selected.clear();for(const input of byId('rule-options').querySelectorAll('input'))input.checked=false;state.page=0;refresh();});
  for(const id of ['match-mode','page-size'])byId(id).addEventListener('change',()=>{state.page=0;renderMatches();});
  byId('match-search').addEventListener('input',()=>{state.page=0;renderMatches();});
  byId('page-prev').addEventListener('click',()=>{state.page--;renderMatches();});
  byId('page-next').addEventListener('click',()=>{state.page++;renderMatches();});
  choosePath(0);
})();
"""


CSS = """
body{margin:0;background:#f4f6f9;color:#172a3d;font-family:system-ui,'Microsoft Yahei',sans-serif;line-height:1.55}main{max-width:1500px;margin:auto;padding:28px}h1{font-size:28px;margin:0 0 12px}h2{font-size:21px;margin:28px 0 12px}h3{font-size:17px;margin:22px 0 10px}p{max-width:1250px}.alert{background:#e8f1fb;border-left:4px solid #337db7;padding:12px 16px;margin:12px 0}.danger{background:#fff0e9;border-color:#d66a30}.danger-text{color:#af3921;font-weight:650}.success-text{color:#17734b;font-weight:650}.metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:10px;margin:16px 0}.metric{background:#fff;border:1px solid #dbe3ec;border-radius:8px;padding:13px 16px}.metric span{display:block;font-size:12px;color:#63798f}.metric strong{font-size:23px}.scroll{overflow:auto;background:#fff;border:1px solid #dce3ed;margin:14px 0}table{border-collapse:collapse;min-width:1080px;width:100%;font-size:13px}th,td{padding:10px 12px;border-bottom:1px solid #e3e9f0;text-align:right;white-space:nowrap}thead th{background:#eaf0f6}tbody th{text-align:left}tbody tr:nth-child(even){background:#fafbfd}small{display:block;color:#697e92;font-size:11px;font-weight:400}.rule-name{white-space:normal;min-width:310px;max-width:450px}details{margin:14px 0;background:#fff;border:1px solid #dce3ed;padding:12px 16px}summary{cursor:pointer;font-weight:650}pre{white-space:pre-wrap;word-break:break-all;font-size:12px}.controls{display:flex;flex-wrap:wrap;align-items:center;gap:10px;margin:16px 0}select,input[type=search],button{font:inherit;padding:9px 12px;border:1px solid #bdcbd9;border-radius:5px;background:white;color:inherit}select{max-width:100%}button{cursor:pointer}button:disabled{cursor:default;opacity:.4}input[type=search]{min-width:250px;flex:1}.interactive{padding:18px;border:1px solid #c9d8e8;background:#edf3fa;border-radius:8px}.rule-options{display:grid;grid-template-columns:repeat(auto-fit,minmax(330px,1fr));gap:8px;max-height:390px;overflow:auto}.rule-option{display:flex;align-items:flex-start;gap:8px;background:white;padding:10px 12px;border:1px solid #dae3ee;border-radius:5px;font-size:13px}.rule-option input{margin-top:4px}.match-table td{text-align:left}.match-table{min-width:850px}.muted{color:#61788e;font-size:13px}footer{font-size:12px;color:#667b90;margin-top:24px}a{color:#196a9d}@media(max-width:650px){main{padding:14px}.interactive{padding:12px}.rule-options{grid-template-columns:1fr}input[type=search]{min-width:160px}}
"""


def render_report(analysis, contexts, rule_matches, out_path):
    """Write a standalone Chinese HTML report and return its Path."""
    analysis, contexts, rule_matches = _clean(analysis), _clean(contexts), _clean(rule_matches)
    paths = analysis.get("paths") or []
    counts = analysis.get("counts") or {}
    candidates = analysis.get("candidates") or {}
    qualified = candidates.get("qualified") or {}
    overview, route_details = _route_tables(analysis)
    warnings = list(analysis.get("warnings") or [])
    warnings.insert(0, "这是使用已知历史平局标签组装的 OR 覆盖复盘，不是赛前有效预测或未来命中率。所有采集时间尚未证实早于开球。")
    intro = "<h1>多组条件覆盖历史平局</h1><p>逐步加入疑似条件，记录新覆盖多少平局、同时新误选多少非平局。规则之间按 OR 合并并去重；平局覆盖率必须和误选比赛一起看。</p>"
    cards = "<div class='metrics'>" + "".join("<div class='metric'><span>" + label + "</span><strong>" + _count(value) + "</strong></div>" for label, value in (
        ("实际完赛比赛", counts.get("games")), ("实际平局", counts.get("draws")), ("实际非平局", counts.get("nonDraws")),
        ("固定候选规则", candidates.get("rules")), ("宽口径合格规则", qualified.get("broad")), ("疑似合格规则", qualified.get("suspected")))) + "</div>"
    pure_paths = [path for path in paths if path.get("method") == "pure"]
    findings = []
    for path in pure_paths:
        final = path.get("final") or {}
        findings.append(_pool(path.get("pool")) + "纯覆盖：覆盖 " + _count(final.get("tp")) + " / " + _count(counts.get("draws")) + " 平局（覆盖率 " + _percent(final.get("recall")) + "），选入 " + _count(final.get("selected")) + " 场，误选非平 " + _count(final.get("fp")) + " 场；入选中平局占比仅 " + _percent(final.get("precision")) + "。")
        if path.get("nearAllSelection") or counts.get("games") and (final.get("selected") or 0)/counts["games"]>=.9:
            findings.append("近全选警报：" + _pool(path.get("pool")) + "选择了全部比赛的 " + _percent((final.get("selected") or 0)/counts["games"]) + "；这条路线适合作为宽泛粗筛复盘，不能把高覆盖率当作高命中率。")
        if not (path.get("milestones") or {}).get("900"):
            findings.append(_pool(path.get("pool")) + "未达到覆盖 900 场平局；不能写成目标已达成。")
    highlights = "<div class='alert danger'>" + "".join("<p>" + _text(line) + "</p>" for line in findings) + "</div>"
    warning_html = "<details open><summary>复盘口径与限制</summary><ul>" + "".join("<li>" + _text(warning if isinstance(warning, str) else json.dumps(warning, ensure_ascii=False)) + "</li>" for warning in warnings) + "</ul></details>"
    formula_html = "<details><summary>20%门槛和‘近10场’怎么算</summary><p>均赔初盘平去水比例＝(1÷初盘均赔平) ÷ [(1÷初盘均赔胜)＋(1÷初盘均赔平)＋(1÷初盘均赔负)]。20%只是报价归一化后的筛选门槛，不是已经校准的模型单场概率，也不是预测命中率。</p><p>近期平率＝赛前最近最多10场有效样本中的平局数 ÷ 实际样本数；本批候选要求至少5场。5场中1平也等于20%，只有完整10场时，≥20%才等价于至少2场平局。</p></details>"
    interaction = """
<h2>交互选择规则，直接核对 OR 结果</h2><div class='interactive'>
<div class='controls'><label for='path-choice'>路线</label><select id='path-choice'></select><button id='rules-all'>全部勾选</button><button id='rules-none'>全部清除</button></div>
<div id='rule-options' class='rule-options'></div><p id='live-summary' class='muted'></p><div id='live-metrics' class='metrics'></div><div id='live-alert' aria-live='polite'></div>
<div class='controls'><select id='match-mode' aria-label='复盘类别'><option value='fn'>漏选平局</option><option value='tp'>命中平局</option><option value='fp'>误选非平</option><option value='selected'>全部已选比赛</option><option value='tn'>正确排除非平</option><option value='all'>全部比赛</option></select><input id='match-search' type='search' placeholder='搜索球队、联赛、日期、比赛ID' aria-label='搜索比赛'><select id='page-size' aria-label='每页场数'><option>25</option><option>50</option><option>100</option></select></div>
<div class='scroll'><table class='match-table'><thead><tr><th>比赛时间 / 联赛</th><th>主队 / 客队</th><th>实际比分</th><th>复盘类别</th><th>命中所勾选规则</th></tr></thead><tbody id='match-rows'></tbody></table></div>
<div class='controls'><button id='page-prev'>上一页</button><span id='page-info'></span><button id='page-next'>下一页</button></div><p class='muted'>悬停规则编号可查看实际命中条件。搜索仅筛选比赛列表，不改变上方全样本统计。每页最多展示100场。</p></div>
"""
    payload = {"analysis": {"counts": counts, "paths": paths, "rules": analysis.get("rules") or []}, "contexts": contexts, "ruleMatches": rule_matches}
    safe_payload = json.dumps(payload, ensure_ascii=False, separators=(",", ":"), allow_nan=False).replace("<", r"\u003c").replace(">", r"\u003e").replace("&", r"\u0026").replace(chr(0x2028), r"\u2028").replace(chr(0x2029), r"\u2029")
    source_details = "<details><summary>来源与复核记录</summary><pre>" + _text(json.dumps(analysis.get("sourceHashes") or {}, ensure_ascii=False, indent=2)) + "</pre><p>候选范围是预先固定的有限业务规则。它的全集覆盖范围不是足球平局预测的理论上限，也不能说明其他算法不能改进。</p></details>"
    page = "<!doctype html><html lang='zh-CN'><head><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>多组条件覆盖历史平局</title><style>" + CSS + "</style></head><body><main>"
    page += intro + cards + highlights + warning_html + formula_html + "<h2>覆盖里程碑与成本路线</h2>" + overview + interaction + "<h2>纯覆盖路径与每步新增误选</h2>" + route_details
    page += _missed_breakdown(analysis) + "<h2>时间顺序内部复核</h2>" + _temporal_table(analysis) + source_details
    page += "<footer>TP=覆盖的实际平局，FP=误选的实际非平局，FN=漏掉的平局，TN=正确排除的非平局。平局覆盖率=TP/全部平局；入选中平局占比=TP/全部已选。未覆盖不等于完全没有数据，也可能是历史样本未达到候选资格门槛，或不符合这批固定条件。页面为离线复盘，勾选不重新拟合模型、不改变原始候选。</footer></main>"
    page += "<script id='cover-payload' type='application/json'>" + safe_payload + "</script><script>" + APP_JS + "</script></body></html>"
    out_path = Path(out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(page, encoding="utf-8")
    return out_path
