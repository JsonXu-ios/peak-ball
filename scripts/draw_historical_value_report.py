#!/usr/bin/env python3
"""Render the offline historical-value study; do not select or fit rules."""
from __future__ import annotations

import argparse
import html
import json
import math
from datetime import datetime, timedelta, timezone
from pathlib import Path


def esc(x):
    return html.escape(str(x if x is not None else "暂无"), quote=True)


def num(x):
    return f"{int(x):,}" if isinstance(x, (int, float)) and math.isfinite(x) else "暂无"


def pct(x):
    return f"{100*x:.2f}%" if isinstance(x, (int, float)) and math.isfinite(x) else "暂无"


def pp(x):
    return f"{x:+.2f} 个百分点" if isinstance(x, (int, float)) and math.isfinite(x) else "暂无"


TOKENS = {"home": "主", "away": "客", "guest": "客", "draw": "平", "sporttery": "竞彩", "william": "威廉",
          "bet365": "Bet365", "average": "平均欧赔", "selected": "页面取样", "simulated": "非让球模拟", "local": "本地模拟",
          "opening": "初盘", "latest": "即时盘", "change": "初盘至即时变化", "odds": "赔率", "prob": "去水概率",
          "roi": "庄家测算ROI", "share": "测算资金比例", "hotCold": "冷热指数", "drawMinimum": "平项最低",
          "spread": "极差", "psychologyError": "心理误差", "minimum": "最小项", "sideGap": "主客去水概率差绝对值",
          "entropy": "三项概率熵", "overround": "三项倒数和", "drawVsStrongSide": "平与强方概率比",
          "oddsChange": "赔率变化", "relativeChange": "赔率相对变化", "probChange": "去水概率变化", "drawAbsRank": "平差值绝对值名次",
          "asianHandicap": "亚盘", "goalsLine": "大小球", "line": "盘口", "water1": "主/大水位", "water2": "客/小水位",
          "waterSpread": "水位差", "goalsFor": "场均进球", "goalsAgainst": "场均失球", "totalGoals": "场均总球",
          "goalDifference": "场均净胜球", "drawRate": "平率", "winRate": "胜率", "lossRate": "负率",
          "wins": "胜场", "draws": "平场", "losses": "负场", "n": "样本数", "under25Rate": "总球不超过2球比例",
          "zeroZeroRate": "0比0比例", "oneOneRate": "1比1比例", "cleanSheetRate": "零封比例", "scorelessRate": "不进球比例",
          "drawStreak": "最近连平数", "winStreak": "最近连胜数", "unbeatenStreak": "最近连续不败数",
          "pair": "双方", "sum": "合计", "absGap": "差绝对值", "over": "大球", "under": "小球",
          "asianHome": "亚盘主队计算热度", "goals": "大小球", "rank": "可比格式排名", "form": "近期战绩",
          "pointsPerGame": "场均赛果积分", "schedule": "已记录赛程", "restDays": "距上一场已记录比赛天数",
          "known7Days": "近7天已记录比赛数", "known14Days": "近14天已记录比赛数", "attackDefenseIndex": "历史攻守算术指数",
          "absLargestTwo": "差值绝对值最大两项", "absSmallestTwo": "差值绝对值最小两项", "home_draw": "胜平",
          "draw_away": "平负", "home_away": "胜负", "ambiguous": "边界并列不明确", "hsupportRate": "主源站支持比例",
          "dsupportRate": "平源站支持比例", "asupportRate": "客源站支持比例", "hprobability": "主源站概率",
          "dprobability": "平源站概率", "aprobability": "客源站概率", "herror": "主源站误差", "derror": "平源站误差",
          "aerror": "客源站误差", "psyError": "心理误差", "hy": "主盈利率", "dy": "平盈利率", "ay": "客盈利率",
          "hlr": "主利润", "dlr": "平利润", "alr": "客利润", "professionalConsensus": "专业合成方向",
          "sportteryComfort": "竞彩舒适方向", "outcome": "方向", "bookmaker": "庄家模型", "platform": "平台模型",
          "main": "主模型", "drawRisk": "平风险合成", "score": "得分", "evilCult": "另类模型", "prediction": "预测",
          "scores": "得分", "overPercent": "大球占比", "underPercent": "小球占比", "warningAdjusted": "警示修正",
          "historyGoalAverage": "旧交锋场均球", "recentGoalAverage": "旧近期场均球", "combinedGoalAverage": "旧合成场均球",
          "combinedHandicapAverage": "旧合成净胜球", "recentTotalGoals": "旧近期场均总球", "recentHomeMatches": "旧主近期样本数",
          "recentGuestMatches": "旧客近期样本数", "goalsInput": "大小球旧历史输入", "tzbl": "源站投注比例",
          "jyykSpf": "源站非让球交易", "h": "主", "d": "平", "a": "客"}


def feature_label(name):
    if name == "context.goals.home.attackDefenseIndex":
        return "（主队近10场均进球＋客队近10场均失球）÷2"
    if name == "context.goals.away.attackDefenseIndex":
        return "（客队近10场均进球＋主队近10场均失球）÷2"
    parts = str(name).split(".")
    if parts[0] == "safeRecent":
        if parts[1] == "pair":
            return "严格赛前双方近10场·" + "·".join(TOKENS.get(p, p) for p in parts[2:])
        return "严格赛前" + TOKENS.get(parts[1], parts[1]) + "队近" + parts[2] + "场·" + "·".join(TOKENS.get(p, p) for p in parts[3:])
    if parts[0] == "safeH2H":
        return "严格赛前交锋" + ("全部" if parts[1] == "all" else "近" + parts[1] + "场") + "·" + "·".join(TOKENS.get(p, p) for p in parts[2:])
    heads = {"euro": "欧赔", "difference": "威廉减", "kelly": "凯利", "line": "公司盘口", "profit": "页面模拟盈亏",
             "marketProfit": "源站支持率口径测算盈亏", "trade": "源站字段", "heat": "计算热度", "retail": "本地散户心理模拟",
             "snapshot": "前台快照派生", "professional": "前台合成信号", "context": "客观情境", "conflict": "差值组合",
             "recent": "严格近期", "availability": "输入可用性"}
    return "·".join([heads.get(parts[0], parts[0])] + [TOKENS.get(p, p) for p in parts[1:]])


def condition_label(atom):
    name = feature_label(atom.get("feature", "未知字段"))
    if atom.get("transform") == "abs":
        name = "绝对值（" + name + "）"
    operators = {"ge": "≥", "le": "≤", "eq": "＝", "lt": "<", "gt": ">"}
    v = atom.get("value")
    value = f"{v:.6g}" if isinstance(v, (int, float)) else str(v)
    field = atom.get("feature", "")
    if isinstance(v, (int, float)):
        if field.endswith("sideGap") or ".probChange." in field:
            value = f"{v * 100:.6g} 个百分点"
        elif (".prob." in field or field.endswith(("Rate", ".probability", ".relativeChange"))) and not field.startswith("trade."):
            value = f"{v * 100:.6g}%"
        elif field.startswith(("heat.", "retail.")) or ".roi." in field or field.endswith(("supportRate", "probability")):
            value += "%"
    return name + " " + operators.get(atom.get("op"), str(atom.get("op"))) + " " + value


STYLE = """
:root{color-scheme:dark;--bg:#0c1220;--panel:#141f30;--line:#30425b;--muted:#a7b8ce;--blue:#82c4ff;--good:#82d7b1;--warn:#f1d287}*{box-sizing:border-box}body{margin:0;background:var(--bg);color:#eaf1fb;font:15px/1.65 system-ui,'Microsoft YaHei',sans-serif}main{max-width:1600px;margin:auto;padding:30px 24px 70px}h1{font-size:30px;margin:0 0 6px}h2{font-size:23px;margin:28px 0 12px}h3{margin:12px 0 8px;font-size:17px}p{margin:8px 0 14px}.muted,small{color:var(--muted)}small{display:block;font-size:12px}.cards{display:grid;grid-template-columns:repeat(auto-fit,minmax(190px,1fr));gap:12px}.card,.panel,details{background:var(--panel);border:1px solid var(--line);border-radius:9px;padding:16px;margin:12px 0}.card b{display:block;font-size:27px}.card{margin:0}.note{padding:13px 16px;border-left:4px solid var(--blue);background:#142b44;margin:15px 0}.warn{background:#302a1b;border-color:var(--warn)}table{border-collapse:collapse;width:100%;font-size:13px}th,td{padding:10px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}th{background:#20314a;white-space:nowrap}tbody tr:hover{background:#1c2b40}.scroll{overflow:auto}.nowrap{white-space:nowrap}.wrap{min-width:270px;max-width:550px}.positive{color:var(--good)}.controls{display:flex;gap:12px;flex-wrap:wrap;align-items:end}.controls label{display:flex;flex-direction:column;gap:4px;font-size:13px}.controls input{min-width:250px}input,select,button{font:inherit;background:#0e1828;color:#eaf1fb;border:1px solid #526b8a;border-radius:5px;padding:8px}button{cursor:pointer}button:disabled{opacity:.4}.controls select{max-width:700px}a{color:var(--blue)}summary{cursor:pointer;font-weight:600}code{font:12px/1.4 Consolas,monospace;word-break:break-all}.pager{display:flex;align-items:center;justify-content:space-between;gap:12px;margin:12px 0}.tag{display:inline-block;border:1px solid #48617f;border-radius:4px;padding:1px 6px;font-size:11px;margin:2px}.grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}ul{padding-left:22px}.compact p{margin:4px 0}@media(max-width:800px){main{padding:20px 12px}.grid{grid-template-columns:1fr}h1{font-size:25px}.controls select{max-width:92vw}}
"""


APP_JS = r"""
const D=JSON.parse(document.getElementById('studyData').textContent);
const el=id=>document.getElementById(id), E=x=>String(x??'暂无').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const N=x=>Number.isFinite(x)?x.toLocaleString('zh-CN'):'暂无', P=x=>Number.isFinite(x)?(x*100).toFixed(2)+'%':'暂无', PP=x=>Number.isFinite(x)?(x>=0?'+':'')+x.toFixed(2)+' 个百分点':'暂无';
const rules=new Map(D.evidence.map(r=>[r.id,r])), atoms=new Map(D.atoms.map(a=>[a.id,a])), matches=new Map(D.matches.map(m=>[m.matchId,m]));
const chainLabel=m=>({purity:'纯度优先',balanced:'平衡覆盖与误选',middle:'平衡覆盖与误选',moderate:'较重视误选代价',coverage:'覆盖优先'}[m]||m);
const options=[...D.evidence.map(r=>({...r,kind:'rule'})),{id:'all',label:'全部4161场历史比赛',matchIds:D.matches.map(m=>m.matchId),stats:D.counts}];
for(let i=0;i<(D.themeUnions||[]).length;i++){const t=D.themeUnions[i];options.push({...t,id:'theme:'+i,kind:'theme',views:[...new Set(t.ruleIds.flatMap(id=>rules.get(id)?.views||[]))],marketComparison:t.stats.marketComparison});}
for(const chain of D.chains){const ids=new Set(),views=new Set();for(let i=0;i<chain.steps.length;i++){const s=chain.steps[i],r=rules.get(s.ruleId);if(r){r.matchIds.forEach(id=>ids.add(id));r.views.forEach(v=>views.add(v));}options.push({id:'chain:'+chain.mode+':'+i,label:chainLabel(chain.mode)+'并集·第'+(i+1)+'步',matchIds:[...ids],views:[...views],stats:s.cumulative,marketComparison:s.marketComparison,kind:'chain',chainMode:chain.mode,chainStep:i});}}
const byOption=new Map(options.map(o=>[o.id,o]));let page=0,visible=[];
const metrics=s=>`选中 <b>${N(s.selected)}</b> 场；平局 <b>${N(s.draw)}</b>；非平误选 <b>${N(s.falseSelections)}</b>（主胜 ${N(s.home)}／客胜 ${N(s.away)}）；历史平率 <b>${P(s.precision)}</b>；覆盖全部实际平局 <b>${P(s.recall)}</b>`;
const table=(heads,rows)=>'<div class="scroll"><table><thead><tr>'+heads.map(h=>'<th>'+E(h)+'</th>').join('')+'</tr></thead><tbody>'+rows.map(r=>'<tr>'+r.map(c=>'<td>'+c+'</td>').join('')+'</tr>').join('')+'</tbody></table></div>';
function marketHTML(m){if(!m)return '<p class="muted">暂无共同市场对照。</p>';const rows=[['同可用且均赔存在的全部比赛',m.available],['本规则在共同样本内',m.ruleInCommonCohort],['均赔排序：同入选数量（边界并列全留）',m.marketSameSelectedCountWithAllTies],['均赔排序：同实际平局数目标（历史事后）',m.marketHistoricalSameDrawTargetWithAllTies]].map(([t,s])=>[E(t),N(s?.selected),N(s?.draw),N(s?.falseSelections),P(s?.precision),P(s?.recall)]);return table(['共同市场口径','入选','平局','误选非平','历史平率','覆盖全部平局'],rows)+'<p class="muted">规则中有 '+N(m.excludedRuleRowsMissingMarket)+' 场因缺少均赔未进入共同对照。同入选数量按概率排序、全留并列，实际数量可能略多；同平局目标用到了已知赛果，是事后效率参照。</p>';}
function rebuildOptions(){const q=el('ruleSearch').value.trim().toLowerCase(),v=el('viewFilter').value, old=el('ruleSelect').value;const list=options.filter(o=>(!q||[o.label,o.id,...(o.allOf||[]).flatMap(a=>[atoms.get(a)?.displayDefinition||'',atoms.get(a)?.feature||''])].join(' ').toLowerCase().includes(q))&&(v==='all'||!o.views||(v==='core'?!o.views.includes('snapshotDerived'):o.views.includes('snapshotDerived'))));el('ruleSelect').innerHTML=list.map(o=>'<option value="'+E(o.id)+'">'+E(o.label)+' ['+N(o.stats.selected)+'场/'+N(o.stats.draw)+'平]</option>').join('');if(list.some(o=>o.id===old))el('ruleSelect').value=old;else el('ruleSelect').value=list[0]?.id||'';page=0;render();}
function renderDetail(o){if(!o){el('ruleDetail').innerHTML='<p>没有符合条件的规则。</p>';return;}let h='<h3>'+E(o.label)+'</h3><p>'+metrics(o.stats)+'</p>';
if(o.kind==='rule'){h+='<p>'+o.views.map(v=>'<span class="tag">'+E(v==='safeHistory'?'严格赛前历史':v==='snapshotDerived'?'前台快照派生':'市场来源字段')+'</span>').join('')+'</p>';for(const id of o.allOf){const a=atoms.get(id);h+='<div class="panel compact"><b>主条件：'+E(a.displayDefinition)+'</b><small><code>'+E(a.feature)+'</code></small><p>样本守卫：'+(a.guardLabels.length?a.guardLabels.map(E).join('；'):'本字段存在且为有限数值')+'</p><small>来源：'+E(a.source)+'；阈值来源：'+E(a.thresholdOrigin||a.origin||'预设条件')+'</small></div>';}
const s=o.stats;h+='<p>同输入可用样本 '+N(s.eligible?.selected)+' 场，平 '+N(s.eligible?.draw)+' 场，平率 '+P(s.eligible?.precision)+'；本条件相对该基率 '+PP(s.eligibleLiftPP)+'。同可用未命中组 '+N(s.notMatchedEligible?.selected)+' 场、平率 '+P(s.notMatchedEligible?.precision)+'。</p><p>历史平率Wilson95%描述区间：'+P(s.wilson95Descriptive?.[0])+'～'+P(s.wilson95Descriptive?.[1])+'。筛选使用全部历史，该区间未计入挑规则造成的不确定性。</p>';if(s.drawScoreCounts)h+='<p>实际平局比分：'+Object.entries(s.drawScoreCounts).map(([score,n])=>E(score)+' '+N(n)+'场').join('；')+'。</p>';
h+='<h3>较早／中间／较晚：同一规则的历史分布</h3>'+table(['日期段','入选','主胜','平','客胜','历史平率','该段同可用基率','提升'],Object.entries(s.periods||{}).map(([k,v])=>[E({early:'较早',middle:'中间',late:'较晚'}[k]||k),N(v.selected),N(v.home),N(v.draw),N(v.away),P(v.precision),P(v.eligible?.precision),PP(v.liftPP)]));
h+='<h3>共同市场样本对照</h3>'+marketHTML(s.marketComparison);const st=s.marketStratified;h+='<details><summary>粗略均赔分层描述与增加条件的增量</summary><p>按固定均赔平局概率档分层后，对照预期平局 '+N(st?.expectedDrawsFromSameEligibleStrata)+' 场，实际多出 '+N(st?.excessDraws)+' 场（'+PP(st?.excessPP)+'）。这是5个百分点粗分档的历史残差，同一档内仍有价格差异；即使只取较高均赔平局概率也可能出现正残差，不能据此判断超越市场或有独立增量。优先看上方同入选量对照。</p>'+table(['均赔平局概率档','可用总场','可用组平率','规则入选','规则平局','规则平率'],(st?.strata||[]).map(v=>[E(Array.isArray(v.range)?v.range.map(x=>(100*x).toFixed(0)+'%').join('～'):'均赔缺失'),N(v.eligible.selected),P(v.eligible.precision),N(v.selected.selected),N(v.selected.draw),P(v.selected.precision)]));if(o.parents?.length)h+=table(['增加的条件','父条件入选/平','父条件排除子组后入选/平','子组相对互斥父对照提升'],o.parents.map(v=>[E(atoms.get(v.removed)?.displayDefinition||v.removed),N(v.parent.selected)+' / '+N(v.parent.draw),N(v.parentNotChild.selected)+' / '+N(v.parentNotChild.draw),PP(v.childVersusDisjointParentPP)]));h+='</details>';
}else if(o.kind==='chain'||o.kind==='theme'){h+='<p>已按比赛ID去重；下方逐场列表为'+(o.kind==='theme'?'各主题条件':'该步及之前规则')+'的并集。</p>';if(o.ruleIds)h+='<ul>'+o.ruleIds.map(id=>'<li>'+E(rules.get(id)?.label||id)+'</li>').join('')+'</ul>';if(o.stats.periods)h+='<h3>较早／中间／较晚</h3>'+table(['日期段','入选','平局','误选非平','历史平率','同可用基率'],Object.entries(o.stats.periods).map(([k,v])=>[E({early:'较早',middle:'中间',late:'较晚'}[k]||k),N(v.selected),N(v.draw),N(v.falseSelections),P(v.precision),P(v.eligible?.precision)]));h+='<h3>共同市场样本对照</h3>'+marketHTML(o.marketComparison);}else h+='<p class="muted">全部比赛用于核对总体分母；请选择具体规则查看条件和共同市场对照。</p>';el('ruleDetail').innerHTML=h;}
function render(){const o=byOption.get(el('ruleSelect').value);renderDetail(o);const q=el('matchSearch').value.trim().toLowerCase(),f=el('outcomeFilter').value;visible=(o?.matchIds||[]).map(id=>matches.get(id)).filter(Boolean).filter(m=>(f==='all'||String(m.result)===f||(f==='non'&&m.result!==1))&&(!q||[m.matchId,m.home,m.guest,m.league].join(' ').toLowerCase().includes(q)));const max=Math.max(0,Math.ceil(visible.length/100)-1);page=Math.max(0,Math.min(page,max));el('matchCount').textContent='当前逐场筛选 '+N(visible.length)+' 场；搜索及赛果筛选不改变上方规则总体统计。';el('matchBody').innerHTML=visible.slice(page*100,page*100+100).map(m=>'<tr><td class="nowrap">'+E(m.beijingTime)+'</td><td>'+E(m.league)+'</td><td>'+E(m.home)+'<br>'+E(m.guest)+'</td><td>'+E(m.homeScore)+' : '+E(m.awayScore)+'</td><td>'+E(['主胜','平局','客胜'][m.result]||'未知')+'</td><td><code>'+E(m.matchId)+'</code></td></tr>').join('')||'<tr><td colspan="6">没有符合条件的比赛</td></tr>';el('pageLabel').textContent=(visible.length?page+1:0)+' / '+Math.ceil(visible.length/100)+' 页，每页100场';el('prev').disabled=page<=0;el('next').disabled=page>=max;}
el('ruleSearch').addEventListener('input',rebuildOptions);el('viewFilter').addEventListener('change',rebuildOptions);el('ruleSelect').addEventListener('change',()=>{page=0;render();});for(const id of ['matchSearch','outcomeFilter'])el(id).addEventListener(id==='matchSearch'?'input':'change',()=>{page=0;render();});el('prev').addEventListener('click',()=>{page--;render();});el('next').addEventListener('click',()=>{page++;render();});el('reset').addEventListener('click',()=>{el('ruleSearch').value='';el('viewFilter').value='all';el('matchSearch').value='';el('outcomeFilter').value='all';rebuildOptions();});
document.querySelectorAll('[data-rule]').forEach(b=>b.addEventListener('click',()=>{el('ruleSearch').value='';el('viewFilter').value='all';rebuildOptions();el('ruleSelect').value=b.dataset.rule;page=0;render();el('explorer').scrollIntoView({behavior:'smooth'});}));rebuildOptions();
"""


def stat_cells(s):
    return "".join("<td>" + esc(v) + "</td>" for v in (num(s.get("selected")), num(s.get("draw")), num(s.get("falseSelections")), pct(s.get("precision")), pct(s.get("recall"))))


def render(directory):
    directory = Path(directory)
    def read(name):
        return json.loads((directory / name).read_text(encoding="utf-8-sig"))
    analysis, catalog, protocol, matches, univariate, audit = [read(n) for n in ("analysis.json", "catalog.json", "protocol.json", "matches.json", "univariate.json", "context-audit.json")]
    evidence = analysis["evidence"]
    all_atoms = {a["id"]: a for a in catalog["atoms"]}
    used = {i for r in evidence for i in r["allOf"]}
    atoms = []
    for i in sorted(used):
        a = dict(all_atoms[i])
        a["displayDefinition"] = condition_label(a)
        a["guardLabels"] = [condition_label(g) for g in a.get("guards", [])]
        atoms.append(a)
    atom_lookup = {a["id"]: a for a in atoms}
    for r in evidence:
        r["label"] = " 且 ".join(atom_lookup[a]["displayDefinition"] for a in r["allOf"])
    for m in matches:
        dt = datetime.fromisoformat(m["matchTime"].replace("Z", "+00:00")).astimezone(timezone(timedelta(hours=8)))
        m["beijingTime"] = dt.strftime("%Y-%m-%d %H:%M") + " 北京时间"
    evidence_by = {r["id"]: r for r in evidence}
    ranked = [evidence_by[r] for r in analysis.get("rankedRuleIds", []) if r in evidence_by]
    counts = analysis["counts"]
    stable_order = [evidence_by[r] for r in analysis.get("stableRuleIds", []) if r in evidence_by]
    if not stable_order:
        stable_order = [r for r in ranked if r["stats"].get("allThreePositiveWith20Rows")]
    # Prefer an already-selected stable representative that also has more
    # actual draws than same-count mean-odds ranking in the common cohort.
    def common_market_more_draws(r):
        m = r["stats"].get("marketComparison", {})
        a, b = m.get("ruleInCommonCohort", {}), m.get("marketSameSelectedCountWithAllTies", {})
        return (a.get("draw", 0) > b.get("draw", 0)) and a.get("selected", 0) <= b.get("selected", 0)
    stable_order = [r for r in stable_order if common_market_more_draws(r)] + [r for r in stable_order if not common_market_more_draws(r)]
    priority = stable_order + [r for r in ranked if r["id"] not in {x["id"] for x in stable_order}]
    thematic = []
    for theme in analysis.get("themes", []):
        for rid in theme.get("ruleIds", []):
            if rid in evidence_by and rid not in {r["id"] for r in thematic}:
                thematic.append(evidence_by[rid])
    priority = thematic + [r for r in priority if r["id"] not in {x["id"] for x in thematic}]
    payload = {"counts": counts, "evidence": priority + [r for r in evidence if r["id"] not in {x["id"] for x in priority}],
               "atoms": atoms, "matches": matches, "chains": analysis["chains"], "themeUnions": analysis.get("themeUnions", [])}
    text = ["<!doctype html><html lang='zh-CN'><head><meta charset='utf-8'><meta name='viewport' content='width=device-width,initial-scale=1'><title>历史平局 · 最有价值特征</title><style>", STYLE, "</style></head><body><main>",
            "<h1>历史平局：哪些特征最有价值</h1><p class='muted'>自有冻结数据 · 先还原历史，再比较纯度、覆盖和误选代价</p>",
            "<div class='note warn'><b>这份报告使用全部历史赛果寻找并挑选规则。</b>下列百分比是这些历史比赛中的实际平率，不能直接当成下一场平局概率。早／中／晚只是描述一致性；所有时段都参与过选规则。盘口采集时点尚未全部确认。</div>",
            "<div class='cards'>"]
    for label, value in (("确认完赛", num(counts["selected"])), ("实际平局", num(counts["draw"])), ("总体平率", pct(counts["precision"])), ("自有数值维度", num(analysis.get("features"))), ("筛选后独特规则", num(analysis["search"].get("uniqueMasks")))):
        text.append("<div class='card'>" + esc(label) + "<b>" + esc(value) + "</b></div>")
    text.append("</div>")
    if analysis.get("themes"):
        text.append("<h2>本轮优先保留的平局主线</h2><p>这些主题按条件是否清楚、分段方向和共同市场对照，从全历史搜索结果中归纳。它们是历史发现，不能解释成已证实的动机或未来胜率。</p><div class='grid'>")
        for i, theme in enumerate(analysis["themes"]):
            text.append("<div class='panel'><small>" + ("补充观察" if i >= 3 else "主要历史线索 " + str(i + 1)) + "</small><h3>" + esc(theme["label"]) + "</h3><p>" + esc(theme["explanation"]) + "</p>")
            for j, rid in enumerate(theme["ruleIds"]):
                r, s = evidence_by[rid], evidence_by[rid]["stats"]
                text.append("<p><b>" + ("严格档" if len(theme["ruleIds"]) > 1 and j == 0 else "较宽档" if len(theme["ruleIds"]) > 1 else "历史表现") + "：平 " + num(s["draw"]) + " / " + num(s["selected"]) + " 场（" + pct(s["precision"]) + "），误选 " + num(s["falseSelections"]) + "，覆盖全部平局 " + pct(s["recall"]) + "。</b></p>")
                text.append("<small>早／中／晚：" + esc(" / ".join(pct(p["precision"]) + "（" + num(p["draw"]) + "/" + num(p["selected"]) + "）" for p in s["periods"].values())) + "</small>")
                m = s["marketComparison"]; a, b = m["ruleInCommonCohort"], m["marketSameSelectedCountWithAllTies"]
                text.append("<p>共同样本：规则选" + num(a["selected"]) + "／平" + num(a["draw"]) + "／误选" + num(a["falseSelections"]) + "；同量均赔选" + num(b["selected"]) + "／平" + num(b["draw"]) + "／误选" + num(b["falseSelections"]) + "。</p><button data-rule='" + esc(rid) + "'>查看条件及全部正反例</button>")
            text.append("</div>")
        text.append("</div><h3>三条主要线索合并后，覆盖多少平局</h3><div class='grid'>")
        for i, union in enumerate(analysis.get("themeUnions", [])):
            s = union["stats"]; m = s["marketComparison"]; a, b = m["ruleInCommonCohort"], m["marketSameSelectedCountWithAllTies"]
            text.append("<div class='panel'><h3>" + esc(union["label"]) + "</h3><p>按比赛ID去重：平 " + num(s["draw"]) + " / " + num(s["selected"]) + " 场，历史平率 " + pct(s["precision"]) + "；误选 " + num(s["falseSelections"]) + "；覆盖全部平局 " + pct(s["recall"]) + "。</p><p>共同样本规则：" + num(a["draw"]) + "平／" + num(a["selected"]) + "场／" + num(a["falseSelections"]) + "误选；同量均赔：" + num(b["draw"]) + "平／" + num(b["selected"]) + "场／" + num(b["falseSelections"]) + "误选。</p><button data-rule='theme:" + str(i) + "'>查看去重后的全部比赛</button></div>")
        text.append("</div>")
    text.append("<h2>候选代表与分段波动观察</h2><p>平率高但覆盖很小，和覆盖广但误选很多，是两种不同价值。以下是本轮候选池内的代表，完整条件和反例在下方可查。</p><div class='grid'>")
    reps = []
    if stable_order:
        reps.append(("三个日期段均有样本、平率均有提升的代表", stable_order[0]))
    elif ranked:
        reps.append(("按全期平率下界排序的候选（须看分段波动）", ranked[0]))
    if evidence:
        reps.append(("单条规则覆盖实际平局最多", max(evidence, key=lambda r: (r["stats"]["draw"], -r["stats"]["falseSelections"]))))
    if ranked and (not stable_order or ranked[0]["id"] != stable_order[0]["id"]):
        reps.append(("全期高平率但需核对分段波动", ranked[0]))
    for title, r in reps:
        s = r["stats"]
        period_text = " / ".join(pct(v.get("precision")) + "（" + num(v.get("draw")) + "/" + num(v.get("selected")) + "）" for v in s.get("periods", {}).values())
        m = s.get("marketComparison", {})
        a, b = m.get("ruleInCommonCohort", {}), m.get("marketSameSelectedCountWithAllTies", {})
        market_text = "共同样本：规则选" + num(a.get("selected")) + "／平" + num(a.get("draw")) + "／误选" + num(a.get("falseSelections")) + "；同量均赔选" + num(b.get("selected")) + "／平" + num(b.get("draw")) + "／误选" + num(b.get("falseSelections")) + "。"
        text.append("<div class='panel'><small>" + esc(title) + "</small><h3>" + esc(r["label"]) + "</h3><p><b>" + pct(s["precision"]) + "</b> 历史平率；平 " + num(s["draw"]) + " / 入选 " + num(s["selected"]) + "；误选 " + num(s["falseSelections"]) + "；覆盖 " + pct(s["recall"]) + "。</p><p>早／中／晚：" + esc(period_text) + "。</p><p>同输入可用基率 " + pct(s["eligible"]["precision"]) + "；提升 " + pp(s.get("eligibleLiftPP")) + "。</p><p>" + esc(market_text) + "</p><button data-rule='" + esc(r["id"]) + "'>查看条件和逐场反例</button></div>")
    text.append("</div><h2>逐步叠加：还能新增多少平局，代价是多少</h2><p>每一步按比赛ID取并集，新增平局和新增误选均排除前面已选中的比赛。最多16步，候选规则须高于总体平率至少1个百分点，每步至少新增5场平局。两条链是搜索启发式，不代表所有可能组合的全局最优。</p>")
    for chain in analysis["chains"]:
        chain_label = {"purity": "纯度优先", "balanced": "平衡覆盖与误选", "middle": "平衡覆盖与误选", "moderate": "较重视误选代价", "coverage": "覆盖优先"}.get(chain["mode"], chain["mode"])
        text.append("<details open><summary>" + esc(chain_label) + "叠加链</summary><div class='scroll'><table><thead><tr><th>步</th><th>加入的规则</th><th>新增平局</th><th>新增误选</th><th>累计入选</th><th>累计平局</th><th>累计误选</th><th>累计平率</th><th>覆盖全部平局</th></tr></thead><tbody>")
        for i, s in enumerate(chain["steps"]):
            r = evidence_by[s["ruleId"]]
            text.append("<tr><td>" + str(i + 1) + "</td><td class='wrap'>" + esc(r["label"]) + "<br><button data-rule='chain:" + chain["mode"] + ":" + str(i) + "'>查看累计并集</button></td><td>" + num(s["addedDraws"]) + "</td><td>" + num(s["addedFalseSelections"]) + "</td>" + stat_cells(s["cumulative"]) + "</tr>")
        text.append("</tbody></table></div>")
        if chain["steps"]:
            last = chain["steps"][-1]["cumulative"]
            text.append("<p>本轮累计覆盖 " + num(last["draw"]) + " / " + num(counts["draw"]) + " 场平局。" + ("达到900场历史覆盖目标。" if last["draw"] >= 900 else "尚未达到900场；不通过扩大到任意全选来补足。") + "</p>")
        text.append("</details>")
    text.append("<h2 id='explorer'>条件、对照与逐场比赛</h2><div class='controls'><label>查找规则或字段<input id='ruleSearch' type='search' placeholder='如：凯利、平差值、goalsLine'></label><label>来源范围<select id='viewFilter'><option value='all'>全部来源</option><option value='core'>市场字段／严格历史</option><option value='snapshot'>含前台快照派生</option></select></label><label>规则或累计并集<select id='ruleSelect'></select></label></div><div id='ruleDetail' class='panel'></div><div class='controls'><label>逐场赛果<select id='outcomeFilter'><option value='all'>全部赛果</option><option value='1'>实际平局</option><option value='non'>误选非平</option><option value='0'>实际主胜</option><option value='2'>实际客胜</option></select></label><label>比赛搜索<input id='matchSearch' type='search' placeholder='球队／比赛ID／联赛'></label><button id='reset'>重置筛选</button></div><p id='matchCount' class='muted'></p><div class='scroll'><table><thead><tr><th>开赛时间</th><th>联赛</th><th>主队／客队</th><th>比分</th><th>赛果</th><th>比赛ID</th></tr></thead><tbody id='matchBody'></tbody></table></div><div class='pager'><button id='prev'>上一页</button><span id='pageLabel'></span><button id='next'>下一页</button></div>")
    text.append("<h2>单维度线索与数据边界</h2><p>单维度筛选独立列出，以免组合提升掩盖原始信号。这里是每字段代表中的前20项；完整排名可查看同目录 univariate.csv。</p><div class='scroll'><table><thead><tr><th>单维度条件</th><th>入选</th><th>平局</th><th>历史平率</th><th>同可用基率提升</th><th>覆盖全部平局</th><th>BH q</th></tr></thead><tbody>")
    for r in univariate.get("rankedBestPerFeature", [])[:20]:
        s = r["stats"]
        text.append("<tr><td class='wrap'>" + esc(condition_label(r)) + "</td><td>" + num(s.get("games")) + "</td><td>" + num(s.get("draws")) + "</td><td>" + pct(s.get("drawRate")) + "</td><td>" + pp(100 * s["lift"] if isinstance(s.get("lift"), (int, float)) else None) + "</td><td>" + pct(s.get("drawCoverage")) + "</td><td>" + esc(f"{s['fisherQ']:.4g}" if isinstance(s.get("fisherQ"), (int, float)) else "暂无") + "</td></tr>")
    text.append("</tbody></table></div><p class='muted'>BH q只调整本次单维度候选族；不能修正过去全部探索，也不覆盖本轮两条件组合挑选。多个条件同源，不能当成相互独立的证据。</p><details><summary>本地有哪些客观信息，哪些动机目前无法核实</summary><ul><li>严格赛前历史：双方至少各5场的比赛 " + num(audit["historyAndTimingChecks"]["bothAtLeastFiveSafePrevious"]) + " 场；交锋至少3场 " + num(audit["historyAndTimingChecks"]["headToHeadAtLeastThree"]) + " 场。</li><li>可比格式排名 " + num(audit["historyAndTimingChecks"]["rankSameTextualPrefixOrBothBare"]) + " 场，属于快照；未确认同一赛季、排名体系和赛前时点。</li><li>没有积分表、晋级／保级条件、完整赛程、确认阵容或轮换信息。“双方拿一分都有利”“保存体力”“送分”不能从现有字段客观确认。</li><li>已记录比赛间隔与近7／14天比赛数来自最多10场安全历史，是观测密度，不能当作完整赛程或真实疲劳。</li><li>亚盘／大小球热度和散户心理为计算值；庄家盈亏是赔率乘假设支持比例的测算，不是观测到的真实资金账本。</li><li>全部日期分段及列表使用matchTime换算北京时间；原始date与真实开赛日不一致的 " + num(audit["historyAndTimingChecks"]["sourceDateDiffersFromActualBeijingKickoffDate"]) + " 场未用于日期分段。</li></ul></details>")
    text.append("<details><summary>研究口径与完整产物</summary><p>每条规则最多两个主条件；样本守卫不算主条件。单条入选至少80场、至少20场实际平局，同可用未命中至少80场。阈值包含预设业务值和全历史无标签分位值；随后用全部赛果挑选，属于历史发现。</p><p>三段边界：" + esc(" / ".join(protocol.get("periodBoundariesBJT", []))) + "（北京时间开赛日）。市场同量对照只在相同规则输入可用、且均赔存在的共同集合比较，缺失不会补成0。</p><p>搜索候选：" + num(analysis["search"].get("atomicCandidates")) + " 个原子条件；" + num(analysis["search"].get("crossFamilyPairsAttempted")) + " 个跨字段族条件对；去重后 " + num(analysis["search"].get("uniqueMasks")) + " 条支持足够的规则。字段族不同不意味着来源独立。</p><p>同目录文件：analysis.json、catalog.json、protocol.json、matches.json、rule-summary.csv、univariate.csv、context-audit.json。仅离线读取冻结文件，没有启动服务或写入数据库。</p></details>")
    payload_json = json.dumps(payload, ensure_ascii=False, separators=(",", ":"), allow_nan=False).replace("</", "<\\/")
    text.extend(["</main><script id='studyData' type='application/json'>", payload_json, "</script><script>", APP_JS, "</script></body></html>"])
    target = directory / "report.html"
    target.write_text("".join(text), encoding="utf-8")
    return {"report": str(target), "evidenceRules": len(evidence), "matches": len(matches), "bytes": target.stat().st_size}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--directory", default="reports/draw-historical-value-study")
    args = parser.parse_args()
    print(json.dumps(render(args.directory), ensure_ascii=False))


if __name__ == "__main__":
    main()
