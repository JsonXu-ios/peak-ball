#!/usr/bin/env python3
"""Render the supplied, offline conflict-pattern study in Chinese.

The renderer does not choose rules, fit a model, query a database, start a
service, or open a browser. Historical matches remain a repeated internal
review; a bookmaker's intention and actual fund flows are not observed.
"""
from __future__ import annotations

import html
import json
import math
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


def _n(value):
    return f"{int(value):,}" if _number(value) else "暂无"


def _pct(value):
    return f"{value * 100:.2f}%" if _number(value) else "暂无"


def _ratio(numerator, denominator):
    return numerator / denominator if _number(numerator) and _number(denominator) and denominator > 0 else None


SOURCE_NOTES = [
    "亚盘热度为平均欧赔与亚盘深度、升降盘计算出的指数，不是观测到的真实投注资金。",
    "页面散户心理为均赔概率经盘口、旧历史、近期表现与球数锚点修正，再作幂变换的本地模拟分布。",
    "庄家测算盈亏＝总假设投注额－该赛果投注额×赔率；正数代表测算盈利，负数代表测算亏损。源站支持比例也不等于已核实的真实资金流水。",
    "模拟竞彩指数和模拟盈亏依赖均赔及模拟心理；公司盈亏可能共用同一支持比例，不能视为多家独立资金证据。",
    "专业方向综合凯利、体彩差值、热度、利润或旧历史等输入，与上述字段同源；含前台派生的场景须单列。",
    "热门输球指热门的对手获胜；热门不胜包含平局和对手获胜。两者都不能直接等同于平局。",
    "冲突场景是可检验的数据组合；博彩公司是否故意引导、真实交易者心理和比赛动机并不能从这些字段直接观测。",
]


STYLE = """
:root{color-scheme:dark;--bg:#0c111b;--panel:#151e2d;--line:#304057;--text:#eaf0fa;--muted:#a8b7ca;--blue:#71b7ff;--green:#83d4ad;--red:#ff9e9e;--yellow:#f5d78a}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--text);font:15px/1.6 system-ui,"Microsoft YaHei",sans-serif}main{max-width:1600px;margin:auto;padding:28px 24px 70px}h1{font-size:30px;margin:0 0 6px}h2{font-size:22px;margin:28px 0 12px}h3{font-size:17px;margin:18px 0 8px}p{margin:8px 0 14px}.muted,small{color:var(--muted)}small{display:block;font-size:12px}.panel,details{padding:18px;border:1px solid var(--line);border-radius:10px;background:var(--panel);margin:14px 0}.note{border-left:4px solid var(--blue);padding:13px 16px;background:#142841;border-radius:5px;margin:14px 0}.warning{border-color:var(--yellow);background:#302b19}.danger{color:var(--red)}.positive{color:var(--green)}.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr));gap:12px}.card{background:var(--panel);border:1px solid var(--line);padding:15px;border-radius:8px}.card b{display:block;font-size:26px}.scroll{overflow:auto}table{border-collapse:collapse;width:100%;font-size:13px;margin:8px 0}th,td{text-align:left;vertical-align:top;padding:10px 12px;border-bottom:1px solid var(--line);white-space:nowrap}th{background:#1b2a3e}th.rule,td.wrap{white-space:normal;min-width:230px}tbody tr:hover{background:#1a293b}summary{cursor:pointer;font-weight:600}details pre{overflow:auto;font-size:12px;max-height:430px}select,input,button{background:#0d1625;color:var(--text);border:1px solid #53637b;border-radius:6px;padding:9px;font:inherit}button{cursor:pointer}button:disabled{opacity:.45;cursor:default}input[type=search]{min-width:270px;flex:1}label{display:flex;flex-direction:column;gap:5px;font-size:13px}.controls{display:flex;flex-wrap:wrap;align-items:end;gap:12px}.pager{display:flex;align-items:center;gap:10px;justify-content:space-between;margin:12px 0}a{color:var(--blue)}code{font-size:12px;overflow-wrap:anywhere}ul{padding-left:22px}.tag{display:inline-block;border:1px solid #4a6382;border-radius:4px;padding:1px 5px;font-size:11px;margin-right:4px}.split{display:grid;grid-template-columns:1fr 1fr;gap:14px}@media(max-width:850px){main{padding:18px 12px}.split{grid-template-columns:1fr}h1{font-size:25px}}
"""


PHASES = {"full": "全量历史描述", "train": "固定资格训练段", "calibration": "固定资格校准段",
          "development": "开发时段复核", "audit": "后期锁定内部复核"}
ROLES = {"all": "全部跨日期复核", "development": "开发时段", "lockedInternalAudit": "后期锁定内部复核", "audit": "后期锁定内部复核"}
VIEWS = {"clean": "市场来源字段＋严格赛前历史", "snapshot": "含前台派生（辅助）"}
TIERS = {"primary": "主要口径：绝对值最大两项", "sensitivity": "次要灵敏度：绝对值最小两项"}
METHODS = {"clean_best1": "市场／严格历史：单场景", "clean_or3": "市场／严格历史：最多3场景并集",
           "all_best1": "含前台派生：单场景", "all_or3": "含前台派生：最多3场景并集",
           "all_enriched_or3": "平率增高资格：最多3场景并集", "smallest_sensitivity_best1": "最小两项灵敏度：单场景"}


def _qualification(value):
    value = value or {}
    reasons = {"train_selected_below40": "固定训练段匹配不足40场", "calibration_selected_below10": "固定校准段匹配不足10场",
               "not_draw_enriched_in_both_development_intervals": "两个固定资格阶段均高于资格组的要求未满足",
               "parent_increment_or_disjoint_control_support_not_met": "相对父条件的增量或互斥对照样本要求未满足"}
    flags = "平率增高资格：" + ("通过" if value.get("enriched") is True else "未通过") + "；增量资格：" + ("通过" if value.get("incremental") is True else "未通过")
    return flags + ("；原因：" + "、".join(reasons.get(reason, reason) for reason in value.get("reasons") or []) if value.get("reasons") else "")


def _evidence_overview(analysis):
    selection = (analysis.get("temporal") or {}).get("selection") or analysis.get("selection") or {}
    audit = selection.get("audit") or {}
    market = audit.get("marketSameSize") or {}
    doc = "<h2>本轮结果怎么读</h2>"
    if audit:
        doc += "<div class='note'><b>开发段选定：" + _e(METHODS.get(selection.get("method"), selection.get("method"))) + "。</b> 后期复核选 " + _n(audit.get("selected")) + "场，其中平局 " + _n(audit.get("tp")) + "场（" + _pct(audit.get("precision")) + "），误选非平 " + _n(audit.get("fp")) + "场，覆盖后期实际平局 " + _pct(audit.get("recall")) + "。"
        if market:
            doc += " 同量均赔排序选 " + _n(market.get("selected")) + "场，平局 " + _n(market.get("tp")) + "场（" + _pct(market.get("precision")) + "），误选非平 " + _n(market.get("fp")) + "场。"
            if _number(audit.get("precision")) and _number(market.get("precision")) and audit["precision"] <= market["precision"]:
                doc += " <b>这条已选组合在本轮后期复核没有超过同量均赔，尚未证明能稳定提高平局筛选效果。</b>"
        doc += "</div>"
    significant = [rule for rule in analysis.get("rules") or [] if any(_number(rule.get(q)) and rule[q] <= .05 for q in ("qTrain", "qFull"))]
    if significant:
        negative = sum(_number((rule.get("stats", {}).get("full") or {}).get("parentLift")) and rule["stats"]["full"]["parentLift"] < 0 for rule in significant)
        doc += "<div class='note warning'><b>q小也可能表示平局减少。</b> 本轮训练或全量双侧检验 q≤0.05 共 " + _n(len(significant)) + "条，其中全量相对父条件平率下降 " + _n(negative) + "条。必须一起看方向；不能把小q标为强平局信号，也不能把这些描述性检验当成前瞻预测显著性。</div><div class='scroll'><table><thead><tr><th>达到上述q阈值的场景</th><th>训练q</th><th>全量q</th><th>全量平/场</th><th>全量比父条件高（百分点）</th><th>后期平/场</th><th>后期比父条件高（百分点）</th></tr></thead><tbody>"
        for rule in significant:
            full, later = (rule.get("stats") or {}).get("full") or {}, (rule.get("stats") or {}).get("audit") or {}
            doc += "<tr><th class='rule'>" + _e(rule.get("label")) + "</th>" + "".join("<td>" + _e(v) + "</td>" for v in (
                f"{rule['qTrain']:.4g}" if _number(rule.get("qTrain")) else "暂无", f"{rule['qFull']:.4g}" if _number(rule.get("qFull")) else "暂无",
                f"{_n(full.get('draws'))}/{_n(full.get('games'))}（{_pct(full.get('drawRate'))}）", _pct(full.get("parentLift")),
                f"{_n(later.get('draws'))}/{_n(later.get('games'))}（{_pct(later.get('drawRate'))}）", _pct(later.get("parentLift")))) + "</tr>"
        doc += "</tbody></table></div>"
    return doc


def _stat_cells(stat):
    stat = stat or {}
    n = stat.get("games")
    return "".join("<td>" + _e(value) + "</td>" for value in (
        _n(n), _n(stat.get("homeWins")), _n(stat.get("draws")), _n(stat.get("awayWins")),
        _pct(stat.get("drawRate")), _pct(_ratio(stat.get("homeWins"), n)), _pct(_ratio(stat.get("awayWins"), n)),
        _n(stat.get("favoriteWins")), _n(stat.get("favoriteLosses")), _n(stat.get("favoriteNotWins")),
        _pct(stat.get("favoriteNotWinRate")), _pct(stat.get("drawAmongFavoriteNotWins")), _pct(stat.get("coverage"))))


STAT_HEAD = "<th>匹配场数</th><th>主胜场</th><th>平局场</th><th>客胜场</th><th>平局占比</th><th>主胜占比</th><th>客胜占比</th><th>热门胜</th><th>热门输球</th><th>热门不胜</th><th>热门不胜率</th><th>不胜中平局占比</th><th>该阶段平局覆盖</th>"


def _phase_stats_table(rule):
    body = []
    for phase, label in PHASES.items():
        stat = (rule.get("stats") or {}).get(phase)
        if stat:
            parent, no_conflict, eligible = stat.get("parent") or {}, stat.get("noConflict") or {}, stat.get("eligible") or {}
            image_parent = stat.get("imageParent") or {}
            control = lambda value: f"{_n(value.get('draws'))}/{_n(value.get('games'))}（{_pct(value.get('drawRate'))}）"
            body.append("<tr><th>" + label + "</th>" + _stat_cells(stat) + "<td>" + _e(control(parent)) +
                        "</td><td>" + _e(control(no_conflict)) + "</td><td>" + _e(control(image_parent)) + "</td><td>" + _e(_pct(stat.get("imageLift"))) + "</td><td>" + _e(control(eligible)) +
                        "</td><td>" + _e(_pct(stat.get("parentLift"))) + "</td></tr>")
    return "<div class='scroll'><table><thead><tr><th>阶段</th>" + STAT_HEAD + "<th>同资格父条件：平/场</th><th>父条件内未冲突：平/场</th><th>同热门/战绩但去差值条件：平/场</th><th>比去差值条件高（百分点）</th><th>同输入资格：平/场</th><th>比父条件高（百分点）</th></tr></thead><tbody>" + "".join(body) + "</tbody></table></div>" if body else "<p>暂无该场景分阶段结果。</p>"


def _union_table(rows):
    body = []
    for row in rows or []:
        metric = row.get("metrics") or row
        market = row.get("marketSameSize") or metric.get("marketSameSize") or {}
        ref = f"平 {_n(market.get('tp'))} / 非平 {_n(market.get('fp'))}（选 {_n(market.get('selected'))}）" if market else "暂无"
        ident = row.get("method")
        label = _e(METHODS.get(ident, ident)) + "<small>" + _e(ident) + "</small>"
        if row.get("label"):
            label += "<small>" + _e(row["label"]) + "</small>"
        body.append("<tr><th class='rule'>" + label + "</th><td>" + _e(ROLES.get(row.get("role"), row.get("role", "全量历史拟合"))) +
                    "</td>" + "".join("<td>" + _e(value) + "</td>" for value in (
                        _n(metric.get("games")), _n(metric.get("draws")), _n(metric.get("selected")),
                        _n(metric.get("tp")), _n(metric.get("fp")), _n(metric.get("fn")),
                        _pct(metric.get("precision")), _pct(metric.get("recall")), ref)) + "</tr>")
    return "<div class='scroll'><table><thead><tr><th>去重组合</th><th>评价范围</th><th>该阶段比赛</th><th>该阶段平局</th><th>入选比赛</th><th>命中平 TP</th><th>误选非平 FP</th><th>漏平 FN</th><th>选中平局占比</th><th>平局覆盖</th><th>同选中数量均赔对照</th></tr></thead><tbody>" + "".join(body) + "</tbody></table></div>" if body else "<p>暂无组合统计。不能把各场景的命中数直接相加。</p>"


def _catalog_table(rules):
    body = []
    for rule in rules:
        stats = rule.get("stats") or {}
        full, audit = stats.get("full") or {}, stats.get("audit") or {}
        parent = audit.get("parent") or {}
        qualification = rule.get("qualification") or {}
        badges = [name for key, name in (("enriched", "平率增高资格"), ("incremental", "增量资格")) if qualification.get(key) is True]
        body.append("<tr><th class='rule'>" + _e(rule.get("label", rule.get("id"))) + "<small>" + _e(rule.get("id")) +
                    "</small></th><td>" + _e(TIERS.get(rule.get("tier"), rule.get("tier"))) + "<small>" + _e(VIEWS.get(rule.get("view"), rule.get("view"))) +
                    "</small></td><td>" + _e(rule.get("favoriteSource")) + "<small>" + _e("主队" if rule.get("favorite") == "home" else "客队" if rule.get("favorite") == "away" else "未指定") +
                    "</small></td><td>" + _e(f"{_n(full.get('draws'))}/{_n(full.get('games'))}（{_pct(full.get('drawRate'))}）") +
                    "</td><td>" + _e(f"{_n(audit.get('draws'))}/{_n(audit.get('games'))}（{_pct(audit.get('drawRate'))}）") +
                    "</td><td>" + _e(f"{_n(parent.get('draws'))}/{_n(parent.get('games'))}（{_pct(parent.get('drawRate'))}）") +
                    "</td><td>" + _e(_pct(audit.get("parentLift"))) + "</td><td>" + _e("、".join(badges) or "未取得该资格") + "</td></tr>")
    return "<div class='scroll'><table><thead><tr><th>场景 / 条件</th><th>口径 / 来源组</th><th>热门定义</th><th>全量历史：平/场</th><th>后期复核：平/场</th><th>后期父条件：平/场</th><th>后期平率差（百分点）</th><th>全量描述所用固定资格</th></tr></thead><tbody>" + "".join(body) + "</tbody></table></div>"


APP_JS = r"""
(function(){
 'use strict';
 function outcome(r){
  if(r&&r.isDraw===true)return 'draw';
  const v=r&&r.resultLabel;
  if(v===0||v==='home')return 'home';if(v===1||v==='draw')return 'draw';if(v===2||v==='away')return 'away';
  const m=String(r&&r.score||'').match(/^\s*(\d+)\s*[:：-]\s*(\d+)\s*$/);
  return m?(+m[1]>+m[2]?'home':+m[1]<+m[2]?'away':'draw'):null;
 }
 const idSet=ids=>new Set((Array.isArray(ids)?ids:[]).map(String));
 function compute(contexts,ids,favorite,scopeIds){
  const selected=idSet(ids),scope=scopeIds==null?null:idSet(scopeIds),seen=new Set(),indices=[];
  let homeWins=0,draws=0,awayWins=0,unknown=0;
  for(let i=0;i<contexts.length;i++){
   const r=contexts[i],id=String(r.matchId);if(!selected.has(id)||(scope&&!scope.has(id))||seen.has(id))continue;seen.add(id);indices.push(i);
   const actual=outcome(r);if(actual==='home')homeWins++;else if(actual==='draw')draws++;else if(actual==='away')awayWins++;else unknown++;
  }
  const games=indices.length,favoriteWins=favorite==='home'?homeWins:favorite==='away'?awayWins:null;
  const favoriteLosses=favorite==='home'?awayWins:favorite==='away'?homeWins:null;
  const favoriteNotWins=favoriteLosses==null?null:favoriteLosses+draws;
  return {indices,games,homeWins,draws,awayWins,unknown,favoriteWins,favoriteLosses,favoriteNotWins,
   drawRate:games?draws/games:null,favoriteNotWinRate:games&&favoriteNotWins!=null?favoriteNotWins/games:null,
   drawAmongFavoriteNotWins:favoriteNotWins?draws/favoriteNotWins:null};
 }
 function filterRows(contexts,stats,mode,query,favorite){
  const needle=String(query||'').trim().toLowerCase(),out=[];
  for(const i of stats.indices){const r=contexts[i],actual=outcome(r),loss=favorite==='home'?'away':favorite==='away'?'home':null;
   if(mode==='draw'&&actual!=='draw'||mode==='home'&&actual!=='home'||mode==='away'&&actual!=='away'||mode==='nonDraw'&&actual!=='home'&&actual!=='away'||mode==='favoriteLoss'&&(!loss||actual!==loss)||mode==='favoriteNotWin'&&(!loss||actual!==loss&&actual!=='draw'))continue;
   if(needle&&!Object.values(r).filter(v=>v==null||typeof v!=='object').join(' ').toLowerCase().includes(needle))continue;out.push(i);
  }return out;
 }
 function paginate(indices,page,size){size=Math.min(100,Math.max(1,Math.floor(+size)||25));const pages=Math.max(1,Math.ceil(indices.length/size));page=Math.min(pages-1,Math.max(0,Math.floor(+page)||0));return {indices:indices.slice(page*size,(page+1)*size),page,pages,total:indices.length,size};}
 const api={outcome,idSet,compute,filterRows,paginate};if(typeof module!=='undefined'&&module.exports)module.exports=api;
 if(typeof document==='undefined')return;
 const data=JSON.parse(document.getElementById('conflict-payload').textContent),contexts=data.contexts||[],rules=data.rules||[],matches=data.ruleMatches||{},phases=data.phaseMatchIds||{};
 const byId=id=>document.getElementById(id),el=(tag,text,cls)=>{const n=document.createElement(tag);if(text!=null)n.textContent=String(text);if(cls)n.className=cls;return n;},p=v=>typeof v==='number'&&Number.isFinite(v)?(v*100).toFixed(2)+'%':'暂无',n=v=>typeof v==='number'&&Number.isFinite(v)?v.toLocaleString():'暂无';
 const state={rule:null,stats:{indices:[]},page:0};
 for(const key of ['train','calibration','development','audit'])if(Array.isArray(phases[key]))byId('phase-choice').append(elOption(key,({train:'固定资格训练段',calibration:'固定资格校准段',development:'开发时段',audit:'后期锁定复核'})[key]));
 function elOption(value,text){const x=el('option',text);x.value=value;return x;}
 function rebuildChoices(){const tier=byId('tier-choice').value,view=byId('source-choice').value,old=state.rule&&state.rule.id,box=byId('rule-choice');box.replaceChildren();
  for(const rule of rules)if((tier==='all'||rule.tier===tier)&&(view==='all'||rule.view===view))box.append(elOption(rule.id,rule.label||rule.id));
  if([...box.options].some(o=>o.value===old))box.value=old;switchRule();
 }
 function switchRule(){state.rule=rules.find(r=>String(r.id)===byId('rule-choice').value)||null;state.page=0;const r=state.rule,phase=byId('phase-choice').value;
  state.stats=compute(contexts,r?matches[r.id]:[],r&&r.favorite,phase==='full'?null:phases[phase]);renderStats();renderRows();
 }
 function renderStats(){const box=byId('match-metrics');box.replaceChildren();const s=state.stats,r=state.rule;
  for(const [label,value]of [['匹配比赛',n(s.games)],['命中平局',n(s.draws)+' / '+p(s.drawRate)],['主胜',n(s.homeWins)],['客胜',n(s.awayWins)],['热门输球（对方胜）',n(s.favoriteLosses)],['热门不胜（含平）',n(s.favoriteNotWins)]]){const c=el('div',null,'card');c.append(el('small',label),el('b',value));box.append(c);}
  const desc=byId('scene-description');desc.replaceChildren();if(!r){desc.append(el('p','当前口径没有场景。'));return;}
  desc.append(el('p',r.label||r.id),el('small','热门定义：'+(r.favoriteSource||'未注明')+'；方向：'+(r.favorite==='home'?'主队':r.favorite==='away'?'客队':'未指定')),
   el('small','父条件：'+(r.parentLabel||'未提供')));
  const phase=byId('phase-choice').value,stat=(r.stats||{})[phase];if(stat){const parent=stat.parent||{},no=stat.noConflict||{};
   desc.append(el('p','同资格父条件：平 '+n(parent.draws)+' / '+n(parent.games)+'（'+p(parent.drawRate)+'）；其中未冲突：平 '+n(no.draws)+' / '+n(no.games)+'（'+p(no.drawRate)+'）。'));
   const image=stat.imageParent||{};if(stat.imageParent)desc.append(el('p','同热门/战绩但去差值条件：平 '+n(image.draws)+' / '+n(image.games)+'（'+p(image.drawRate)+'）；平率差 '+p(stat.imageLift)+'（百分点，仅描述）。'));
   if(s.games!==stat.games||s.draws!==stat.draws)desc.append(el('p','明细重算与报告统计不一致，请检查阶段ID或结果口径。','danger'));
  }
  desc.append(el('small','列表搜索和赛果筛选只缩小下面明细；上面的完整场景指标不随搜索改变。热门不胜不能全部计作平局。'));
 }
 function renderRows(){const r=state.rule,s=state.stats,filtered=filterRows(contexts,s,byId('result-mode').value,byId('match-search').value,r&&r.favorite),page=paginate(filtered,state.page,byId('page-size').value);state.page=page.page;
  byId('list-count').textContent='符合明细条件 '+n(page.total)+' 场 · 第 '+(page.page+1)+' / '+page.pages+' 页（每页最多100）';byId('previous').disabled=page.page===0;byId('next').disabled=page.page>=page.pages-1;
  const body=byId('table-body');body.replaceChildren();for(const i of page.indices){const row=contexts[i],actual=outcome(row),tr=el('tr'),time=el('td',row.matchTime||row.date||'');time.append(el('small',row.league||''));const teams=el('td',(row.home||'')+' / '+(row.away||''));teams.append(el('small','比赛ID '+row.matchId));
   const favorite=r&&r.favorite,loss=favorite==='home'?'away':favorite==='away'?'home':null,remark=actual==='draw'?'实际平局；热门不胜':loss&&actual===loss?'热门输球；实际非平':actual===favorite?'热门获胜':'结果未知';
   tr.append(time,teams,el('td',row.score||''),el('td',({home:'主胜',draw:'平局',away:'客胜'})[actual]||'未知',actual==='draw'?'positive':''),el('td',remark));body.append(tr);
  }if(!page.indices.length){const tr=el('tr'),td=el('td','没有符合当前明细条件的比赛。');td.colSpan=5;tr.append(td);body.append(tr);}
 }
 for(const id of ['tier-choice','source-choice'])byId(id).addEventListener('change',rebuildChoices);for(const id of ['rule-choice','phase-choice'])byId(id).addEventListener('change',switchRule);
 byId('result-mode').addEventListener('change',()=>{state.page=0;renderRows();});byId('page-size').addEventListener('change',()=>{state.page=0;renderRows();});byId('match-search').addEventListener('input',()=>{state.page=0;renderRows();});
 byId('previous').addEventListener('click',()=>{state.page--;renderRows();});byId('next').addEventListener('click',()=>{state.page++;renderRows();});
 if(Array.isArray(phases.audit))byId('phase-choice').value='audit';rebuildChoices();
})();
"""


def render_report(analysis, contexts, rule_matches, out_path):
    """Write an offline report from frozen supplied statistics and match IDs."""
    analysis, contexts, rule_matches = _clean(analysis or {}), _clean(contexts or []), _clean(rule_matches or {})
    rules = analysis.get("rules") or []
    counts = analysis.get("counts") or {}
    temporal = analysis.get("temporal") or {}
    summaries = temporal.get("summaries") or []
    source_groups = {view: sum(rule.get("view") == view for rule in rules) for view in VIEWS}
    tier_groups = {tier: sum(rule.get("tier") == tier for rule in rules) for tier in TIERS}
    cards = "".join("<div class='card'><small>" + _e(label) + "</small><b>" + _e(_n(value)) + "</b></div>" for label, value in (
        ("完赛比赛", counts.get("games")), ("实际平局", counts.get("draws")), ("实际非平局", counts.get("nonDraws")), ("固定业务场景", len(rules))))
    doc = "<!doctype html><html lang='zh-CN'><head><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>平局冲突场景研究</title><style>" + STYLE + "</style></head><body><main>"
    doc += "<h1>平局冲突场景研究</h1><p class='muted'>按自有赔率差值、热门反向、近期形象与盈亏组合检查真实赛果。</p><div class='cards'>" + cards + "</div>"
    doc += "<div class='note warning'><b>先看平局，再看热门是否输球。</b> 热门输球是另一方获胜；热门不胜才包括平局。报告分别列出，不把反热门命中都算平局命中。下列结果仍是同一批已有历史的重复内部研究，采集时点未知，后期段不能称为新的前瞻独立样本。</div>"
    doc += "<p>主要口径取三个差值绝对值<b>最大的两项</b>；最小两项仅作单列灵敏度对照。并列包含三项时不硬凑成两项。差值正负只表示报价高低，不能直接解释成庄家怕赔或故意引导。</p>"
    doc += _evidence_overview(analysis)
    doc += "<h2>跨日期去重结果</h2><p>每个后期时段的入选规则只由更早数据确定。命中平 TP 与误选非平 FP 均按比赛去重；不同场景可能重叠，不能直接相加。均赔对照按同样选中数量列出。</p>"
    for role in ("all", "development", "lockedInternalAudit", "audit"):
        rows = [row for row in summaries if row.get("role") == role]
        if rows:
            doc += "<h3>" + _e(ROLES[role]) + "</h3>" + _union_table(rows)
    if not summaries:
        doc += "<p>暂无跨日期汇总，不能仅用全量历史的高平率得出预测结论。</p>"
    selection = temporal.get("selection") or analysis.get("selection") or {}
    if selection:
        reason = selection.get("reason", "未提供选择依据")
        if str(reason).startswith("Only first3 development blocks"):
            reason = "只用前3个开发时段，在至少入选60场的主要策略中按平局占比Wilson区间下界选择；后期赛果不参与策略选择。"
        elif str(reason).startswith("No primary strategy had"):
            reason = "没有主要策略达到开发段至少入选60场的要求，未推选优胜组合。"
        doc += "<div class='panel'><h3>只由开发段确定的组合</h3><p>" + _e(METHODS.get(selection.get("method"), selection.get("method", "未选定"))) + "：" + _e(reason) + "</p>"
        rows = [dict(selection[phase], method=selection.get("method"), role=role) for phase, role in (("development", "development"), ("audit", "lockedInternalAudit")) if isinstance(selection.get(phase), dict)]
        doc += _union_table(rows) + "<small>后期复核用于报告这条已选路线的结果，不根据后期表现另换冠军。</small></div>"
    doc += "<h2>具体场景与比赛核对</h2><p>平局占比＝条件内实际平局／条件内比赛。平局覆盖＝命中的平局／该阶段全部实际平局。主对照使用具备相同输入资格的父条件；父条件包含子条件，另列其中未发生冲突的互斥组。第二个对照保留相同热门、战绩、大小球条件，移除差值条件，用于检查差值是否增加信息；该对照只作描述。</p>"
    doc += "<div class='panel'><div class='controls'><label>差值口径<select id='tier-choice'><option value='primary'>主要：绝对值最大两项</option><option value='sensitivity'>次要：绝对值最小两项</option><option value='all'>两种口径分列查看</option></select></label><label>输入来源<select id='source-choice'><option value='all'>全部来源组</option><option value='clean'>市场来源字段＋严格历史</option><option value='snapshot'>含前台派生（辅助）</option></select></label><label style='flex:1'>冲突场景<select id='rule-choice'></select></label><label>明细阶段<select id='phase-choice'><option value='full'>全量历史描述</option></select></label></div><div id='scene-description'></div><div id='match-metrics' class='cards'></div>"
    doc += "<div class='controls' style='margin-top:18px'><label>实际结果<select id='result-mode'><option value='all'>全部赛果</option><option value='draw'>只看实际平局</option><option value='nonDraw'>只看误选非平局</option><option value='home'>只看主胜</option><option value='away'>只看客胜</option><option value='favoriteLoss'>只看热门输球（对方胜）</option><option value='favoriteNotWin'>只看热门不胜（平＋对方胜）</option></select></label><label style='flex:1'>搜索比赛<input id='match-search' type='search' placeholder='球队、联赛、比赛ID、日期'></label><label>每页<select id='page-size'><option value='25'>25</option><option value='50'>50</option><option value='100'>100</option></select></label></div><div class='pager'><span id='list-count'></span><span><button id='previous'>上一页</button> <button id='next'>下一页</button></span></div><div class='scroll'><table><thead><tr><th>时间 / 联赛</th><th>主队 / 客队</th><th>实际比分</th><th>实际赛果</th><th>平局与热门结果分开核对</th></tr></thead><tbody id='table-body'></tbody></table></div></div>"
    doc += "<h2>各场景分阶段统计</h2><p class='muted'>场景名字描述字段条件，不等于已验证的庄家意图。主胜率、平率、客胜率按实际比分分别统计。样本为0时显示暂无比例。固定资格训练/校准段用于全量描述的规则筛选；它们与开发/后期时段可能重叠，不能相加。跨日期组合表则在每一折重新只用更早训练/校准数据选规则；本页固定资格不能代替逐折资格。</p>"
    for rule in rules:
        qualification = rule.get("qualification") or {}
        doc += "<details><summary>" + _e(rule.get("label", rule.get("id"))) + "</summary><p>" + _e(TIERS.get(rule.get("tier"), rule.get("tier"))) + "；" + _e(VIEWS.get(rule.get("view"), rule.get("view"))) + "</p><p>热门定义：" + _e(rule.get("favoriteSource")) + "；父条件：" + _e(rule.get("parentLabel")) + "。</p>" + _phase_stats_table(rule)
        doc += "<small>全量描述所用固定资格：" + _e(_qualification(qualification)) + "；固定训练段多重比较 q=" + _e(rule.get("qTrain")) + "；全量描述 q=" + _e(rule.get("qFull")) + "。Fisher/BH为双侧差异检验，q小也可能表示平率下降；需结合方向。这些检验不证明前瞻预测有效。</small></details>"
    doc += "<details><summary>完整场景总表（" + _n(len(rules)) + "条）</summary>" + _catalog_table(rules) + "</details>"
    doc += "<h2>全量历史组合：仅作描述</h2><div class='note warning'>这些组合使用已有历史选择／评价，属于历史拟合。高覆盖若同时误选大量非平局，不能称为有用的平局模型。</div>" + _union_table(analysis.get("historicalUnions"))
    doc += "<details><summary>字段来源与同源关系</summary><ul>" + "".join("<li>" + _e(note) + "</li>" for note in SOURCE_NOTES) + "</ul><p>主要场景 " + _n(tier_groups.get("primary")) + "条，次要灵敏度 " + _n(tier_groups.get("sensitivity")) + "条；市场／严格历史 " + _n(source_groups.get("clean")) + "条，含派生 " + _n(source_groups.get("snapshot")) + "条。</p></details>"
    for key, label in (("warnings", "结果解释边界"), ("protocol", "固定研究口径"), ("sourceHashes", "来源与版本核查")):
        doc += "<details><summary>" + label + "</summary><pre>" + _e(json.dumps(analysis.get(key), ensure_ascii=False, indent=2)) + "</pre></details>"
    payload = {"contexts": contexts, "rules": rules, "ruleMatches": rule_matches, "phaseMatchIds": analysis.get("phaseMatchIds") or {}}
    encoded = json.dumps(payload, ensure_ascii=False, allow_nan=False, separators=(",", ":")).replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
    doc += "<script id='conflict-payload' type='application/json'>" + encoded + "</script><script>" + APP_JS + "</script></main></body></html>"
    out_path = Path(out_path)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    out_path.write_text(doc, encoding="utf-8")
    return out_path
