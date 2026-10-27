#!/usr/bin/env python3
"""Standalone sporttery historical-scope report; local inputs only."""
from __future__ import annotations

import csv
import hashlib
import html
import json
from pathlib import Path

import numpy as np

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-sporttery-study"


def read(name):
    return json.loads((OUT / name).read_text(encoding="utf-8-sig"))


def esc(value):
    return html.escape(str(value))


def pct(value):
    return "暂无" if value is None else f"{100*value:.2f}%"


def main():
    cohort, rules, control, analysis = [read(n) for n in ("cohort-audit.json", "rules.json", "rule-control.json", "analysis.json")]
    rows = json.loads((ROOT / "reports/draw-full-feature-study/dataset.json").read_text(encoding="utf-8"))["rows"]
    arrays = np.load(OUT / "scores.npz", allow_pickle=False)
    ids = arrays["matchIds"].astype(str).tolist()
    assert ids == [str(r["matchId"]) for r in rows]
    assert hashlib.sha256((OUT / "scores.npz").read_bytes()).hexdigest() == analysis["scoresSHA256"]
    labels = arrays["labels"]
    rule_masks = {}
    with (OUT / "rule-selections.csv").open(encoding="utf-8-sig", newline="") as f:
        records = list(csv.DictReader(f))
    assert [r["matchId"] for r in records] == ids
    rule_configs = {c["id"]: c for c in rules["configurations"]}
    scope_labels = {"marked": "竞彩标记范围：511场／124平", "markedBothQuotes": "竞彩内初即盘完整：471场／119平", "latestQuoteEvidence": "真实体彩即时盘完整：472场／119平（含1场缺竞彩标记）"}
    view_labels = {"jcOnly": "仅体彩胜平负报价及确定变换", "jcHistory": "体彩报价＋严格赛前历史", "historyOnly": "仅严格赛前历史", "clean": "多公司市场＋严格赛前历史", "all": "全部现有输入（含快照派生）"}
    methods = []
    model_control = []
    def model_name(result):
        kind = result["kind"]
        if kind == "sporttery_quote_baseline":
            return "体彩即时去水平局概率排序；本范围重新选阈值"
        phase = {"global_fit_threshold_frozen": "旧全局模型·原阈值冻结", "global_fit_subset_threshold": "旧全局模型·按本范围重选阈值", "subset_full_history_fit": "竞彩子集·重新拟合并选阈值"}.get(kind, kind)
        return phase + "｜" + view_labels.get(result.get("view"), result.get("view", "")) + "｜" + result.get("configId", result.get("oldModelId", ""))
    for result in analysis["results"]:
        if result["kind"] == "shuffled_training_labels_capacity_diagnostic":
            model_control.append(result)
            continue
        s = result["stats"]
        methods.append({"id": result["id"], "scope": result["scopeId"], "name": model_name(result),
                        "stats": s, "selected": result["selectedMatchIds"], "type": "model",
                        "threshold": result["threshold"], "thresholdMode": result["thresholdMode"],
                        "inputFields": result.get("inputFields", []), "kind": result["kind"],
                        "note": result.get("missingPolicy", "")})
    for cfg in rules["configurations"]:
        ident = cfg["id"]
        mask = np.array([r[ident] == "1" for r in records])
        scope = "marked" if cfg["scope"] == "jingcaiId" else "latestQuoteEvidence"
        selected_ids = [ident for ident, yes in zip(ids, mask) if yes]
        stats = cfg["stats"]
        assert len(selected_ids) == stats["selected"]
        assert int(np.sum(mask & (labels == 1))) == stats["draw"]
        rule_masks[ident] = selected_ids
    winners = [rule_configs[w["id"]] for w in rules["bestByScopeSourceLimit"]]
    frozen = [c for c in rules["configurations"] if not c["fittedOnSubset"]]
    rule_lookup = {r["id"]: r for r in rules["rules"]}
    for cfg in frozen + winners:
        scope = "marked" if cfg["scope"] == "jingcaiId" else "latestQuoteEvidence"
        source = "多公司及全部现有输入" if cfg["source"] == "allRecorded" else "体彩报价＋严格赛前历史"
        name = "旧全局32条排除规则原样应用" if not cfg["fittedOnSubset"] else f"本范围重选≤{cfg['maximumRules']}条排除规则｜{source}"
        methods.append({"id": cfg["id"], "scope": scope, "name": name, "type": "rule", "stats": cfg["stats"],
                        "selected": rule_masks[cfg["id"]], "rules": [rule_lookup[rid] for rid in cfg["ruleIds"]],
                        "algorithm": cfg.get("algorithm"), "coefficient": cfg.get("coefficient"),
                        "kind": "historical_rule_refit" if cfg["fittedOnSubset"] else "old_global_rule_frozen"})
    union_ids = set(cohort["cohorts"]["quoteOrIdEvidence"]["matchIds"])
    data_rows = [{"id": str(r["matchId"]), "date": r["date"], "time": r["matchTime"],
                  "league": r["league"], "home": r["home"], "away": r["guest"],
                  "jingcaiId": r.get("jingcaiId", ""), "h": r["homeScore"], "a": r["awayScore"],
                  "label": r["result"]} for r in rows if str(r["matchId"]) in union_ids]
    scopes = {k: v["matchIds"] for k, v in analysis["scopes"].items()}
    payload = {"methods": methods, "rows": data_rows, "scopes": scopes, "scopeLabels": scope_labels}
    def table(headers, body):
        return "<div class=scroll><table><thead><tr>"+"".join("<th>"+esc(c)+"</th>" for c in headers)+"</tr></thead><tbody>"+"".join("<tr>"+"".join("<td>"+esc(c)+"</td>" for c in row)+"</tr>" for row in body)+"</tbody></table></div>"
    scope_table = table(["范围", "完赛", "平局", "非平局", "基础平率", "同覆盖目标"], [
        [scope_labels[k], analysis["scopes"][k]["scopeGames"], analysis["scopes"][k]["scopeDraws"], analysis["scopes"][k]["scopeGames"]-analysis["scopes"][k]["scopeDraws"], pct(analysis["scopes"][k]["precision"]), analysis["scopes"][k]["targetDraws"]] for k in scope_labels])
    def summary_table(mm):
        return table(["方法", "范围", "入选", "命中平", "误选", "漏平", "命中率", "覆盖率"], [[m["name"], m["scope"], m["stats"]["selected"], m["stats"]["draw"], m["stats"]["falseSelections"], analysis["scopes"][m["scope"]]["scopeDraws"]-m["stats"]["draw"], pct(m["stats"]["precision"]), pct(m["stats"]["recall"])] for m in mm])
    main_rule_table = summary_table([m for m in methods if m["type"] == "rule" and m["scope"] == "marked"])
    new_model_table = summary_table([m for m in methods if m["kind"] in ("subset_full_history_fit", "sporttery_quote_baseline") and m["scope"] == "marked"])
    old_table = summary_table([m for m in methods if m["kind"] in ("global_fit_threshold_frozen", "global_fit_subset_threshold") and m["scope"] == "marked"])
    sensitivity = summary_table([m for m in methods if m["scope"] != "marked"])
    controls = table(["容量诊断", "种子", "保留随机正例", "选中", "误选随机负例", "训练内命中率"], [["16条规则，固定penalty=5", r["seed"], r["stats"]["draw"], r["stats"]["selected"], r["stats"]["falseSelections"], pct(r["stats"]["precision"])] for r in control["results"]] + [["固定体彩＋历史模型", r["permutationSeed"], r["stats"]["draw"], r["stats"]["selected"], r["stats"]["falseSelections"], pct(r["stats"]["precision"])] for r in model_control])
    links = " · ".join(f'<a href="{esc(n)}">{esc(n)}</a>' for n in ("analysis.json", "rules.json", "cohort-audit.json", "rule-control.json", "scores.npz", "rule-selections.csv"))
    escaped_data = json.dumps(payload, ensure_ascii=False, separators=(",", ":")).replace("<", "\\u003c").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
    document = TEMPLATE.replace("__SCOPE__", scope_table).replace("__RULES__", main_rule_table).replace("__MODELS__", new_model_table).replace("__OLD__", old_table).replace("__SENSITIVITY__", sensitivity).replace("__CONTROLS__", controls).replace("__LINKS__", links).replace("__DATA__", escaped_data)
    (OUT / "report.html").write_text(document, encoding="utf-8")
    print(json.dumps({"output": str((OUT / "report.html").relative_to(ROOT)), "methods": len(methods), "rows": len(data_rows), "sha256": hashlib.sha256((OUT / "report.html").read_bytes()).hexdigest()}, ensure_ascii=False))


TEMPLATE = r'''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>体彩范围历史平局核实</title>
<style>body{font:15px/1.7 system-ui,sans-serif;margin:auto;max-width:1550px;padding:24px;color:#dfe6ef;background:#10151c}h1,h2{color:#fff;line-height:1.4}h2{font-size:20px;margin-top:30px}p{max-width:1250px}.warn{padding:18px;border:1px solid #bd814a;background:#392b1c;border-radius:8px}.note{color:#aab9cb}.scroll{overflow:auto}table{border-collapse:collapse;width:100%;white-space:nowrap;background:#18212c}th,td{border:1px solid #344153;padding:8px;text-align:left}th{background:#243245}input,select,button{font:inherit;background:#1b2735;color:#fff;border:1px solid #66788c;border-radius:4px;padding:7px;margin:5px}select{max-width:95%}#method{min-width:420px}button:disabled{opacity:.4}a{color:#72bdff}pre{white-space:pre-wrap;word-break:break-word;background:#192330;padding:12px}details{margin:16px 0}summary{cursor:pointer;color:#9bcaff}.counts{font-weight:bold;color:#8acbff}small{color:#aab9cb}</style>
<h1>体彩范围：历史平局特征核实</h1>
<div class="warn"><strong>先看容量对照：</strong>体彩报价＋历史的16条规则，真实赛果拟合为116/235＝49.36%；同样方法拟合随机标签也达到47.74%—51.79%。体彩＋历史模型训练内91.34%，随机标签也达85.29%—90.63%；多公司模型训练内甚至100%。这些都是同批数据拟合，不能当作未来准确率。<br>本页所有新阈值、子集模型和规则重选均使用已知历史赛果，没有独立验证；旧全局模型也曾使用这批比赛训练。“旧阈值冻结”不代表新数据验证。</div>
<h2>比赛范围与输入数据分开看</h2>
__SCOPE__
<p>主范围按非空 <code>jingcaiId</code>：511场中124场平局。40场虽有竞彩标记却缺真实体彩即时赔率，其中5场平局，仍留在主范围。另有1场无竞彩标记但有真实体彩即时赔率（498169395，1:2），仅列入472场赔率证据敏感性，不把缺标记直接认定为非竞彩。</p>
<p>原目标“找回900/970平局且命中率≥30%”按相同比例换算：主范围至少找回116/124；471、472场范围至少111/119。基础平率24%左右，不能沿用绝对900场目标。</p>
<p><strong>输入含义：</strong>“仅体彩”只含体彩普通胜平负的真实初盘、即时盘和确定性变换，不含让球胜平负、威廉、Bet365、均赔、跨公司差值、热度、模拟赔率。“体彩＋历史”另加严格按目标开赛前重建的历史记录；“多公司市场＋历史”仍使用其他公司数据。缺盘口不以模拟盘冒充；价格排序基准缺报价时用该范围历史平率，所有边界并列完整保留。</p>
<p class="note">历史记录剔除目标场、去重，并要求历史开赛＋2小时早于目标开赛；近况通常要求至少5场，交锋至少1场。模型中不足样本字段置为缺失，子集内中位数填补，不额外使用缺失指示。规则的每个条件及样本护栏在下方逐项公开。所有盘口的真实采集时点尚未证明在赛前。</p>
<h2>主511场：可解释排除规则</h2>__RULES__
<p class="note">旧32条原样应用；新8/16条从固定候选目录和有限贪心配置中按本范围赛果重选。每条最多2个主条件，缺失不匹配。主表仅列每种输入及复杂度的历史最优配置，不声称全局最优。</p>
<h2>主511场：子集重新拟合与体彩报价排序</h2>__MODELS__
<details><summary>旧全局模型：冻结原阈值 与 只在子集重新选阈值</summary><p>两者价格打分相同，只有入选阈值是否重新使用本范围赛果选择不同；不等于在竞彩子集重新训练。模型ID与输入视图均保留。</p>__OLD__</details>
<details><summary>471场主内完整报价与472场赔率证据敏感性</summary>__SENSITIVITY__</details>
<h2>随机标签容量诊断</h2>__CONTROLS__
<p class="note">每个随机诊断仍有124个随机正例，并按自身随机标签保留至少116例，不把随机正例称作实际平局。固定同一模型参数或同一16规则penalty=5，不为乱标签另加复杂度；规则种子/候选筛选在每份乱标签下重做。原目录阈值和模板曾探索过真实历史，因此这只是容量诊断，不计算正式置换检验p值。</p>
<h2>逐场核对：命中、误选与漏平</h2>
<label>范围<select id="scope"><option value="marked">主范围511场</option><option value="markedBothQuotes">主范围内双盘完整471场</option><option value="latestQuoteEvidence">即时盘证据472场</option></select></label><br>
<label>方法<select id="method"></select></label><br>
<label>结果<select id="outcome"><option value="all">全部</option><option value="hit">命中平局</option><option value="false">误选非平局</option><option value="miss">漏掉平局</option><option value="rejected">未选非平局</option></select></label>
<label>球队／比赛ID／竞彩ID<input id="search" placeholder="输入筛选；不改变整体指标"></label>
<p id="aggregate" class="counts"></p><p id="detail" class="note"></p><div class="scroll"><table><thead><tr><th>日期／比赛ID／竞彩ID</th><th>联赛</th><th>主队</th><th>客队</th><th>比分</th><th>状态</th></tr></thead><tbody id="matches"></tbody></table></div>
<button id="prev">上一页</button><span id="page"></span><button id="next">下一页</button>
<details><summary>当前方法的原始条件、样本护栏与字段来源</summary><pre id="formula"></pre></details>
<h2>本地原始产物</h2><p>__LINKS__</p><p class="note">离线报告；未启动或重启程序，未读写业务数据库或Redis。</p>
<script id="sporttery-data" type="application/json">__DATA__</script>
<script>
const DATA=JSON.parse(document.getElementById('sporttery-data').textContent);
const el=id=>document.getElementById(id);let currentPage=0;let filteredRows=[];
const escapeHtml=value=>String(value??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const percent=x=>x==null?'暂无':(x*100).toFixed(2)+'%';
const status=(r,set)=>set.has(r.id)?(r.label===1?'hit':'false'):(r.label===1?'miss':'rejected');
const statusText={hit:'命中平局',false:'误选非平局',miss:'漏掉平局',rejected:'未选非平局'};
function refreshMethods(){const options=DATA.methods.filter(m=>m.scope===el('scope').value);el('method').innerHTML=options.map(m=>'<option value="'+escapeHtml(m.id)+'">'+escapeHtml(m.name)+'</option>').join('');el('method').value=options.find(m=>m.type==='rule'&&m.id.includes('sportteryMarketAndSafeHistory_16'))?.id||options[0].id;currentPage=0;render();}
function render(){const method=DATA.methods.find(m=>m.id===el('method').value);const scopeSet=new Set(DATA.scopes[method.scope]);const chosen=new Set(method.selected);const all=DATA.rows.filter(r=>scopeSet.has(r.id));const s=method.stats;const totalDraw=all.filter(r=>r.label===1).length;
 el('aggregate').textContent='整体入选 '+s.selected+' 场｜命中平 '+s.draw+'｜误选 '+s.falseSelections+'｜漏平 '+(totalDraw-s.draw)+'｜命中率 '+percent(s.precision)+'｜覆盖率 '+percent(s.recall);
 el('detail').textContent=method.name+'；范围 '+DATA.scopeLabels[method.scope]+(method.threshold!=null?'；阈值 '+method.threshold:'')+(method.note?'；'+method.note:'');
 el('formula').textContent=JSON.stringify(method.type==='rule'?{algorithm:method.algorithm,coefficient:method.coefficient,rules:method.rules}:{kind:method.kind,thresholdMode:method.thresholdMode,threshold:method.threshold,inputFields:method.inputFields},null,2);
 const query=el('search').value.trim().toLowerCase();filteredRows=all.filter(r=>(el('outcome').value==='all'||status(r,chosen)===el('outcome').value)&&(!query||[r.id,r.jingcaiId,r.home,r.away].some(v=>String(v).toLowerCase().includes(query))));
 const pages=Math.max(1,Math.ceil(filteredRows.length/100));currentPage=Math.min(currentPage,pages-1);el('matches').innerHTML=filteredRows.slice(currentPage*100,currentPage*100+100).map(r=>'<tr><td>'+escapeHtml(r.date)+'<br>'+escapeHtml(r.id)+'<br>'+escapeHtml(r.jingcaiId)+'</td><td>'+escapeHtml(r.league)+'</td><td>'+escapeHtml(r.home)+'</td><td>'+escapeHtml(r.away)+'</td><td>'+r.h+':'+r.a+'</td><td>'+statusText[status(r,chosen)]+'</td></tr>').join('');
 el('page').textContent='筛选 '+filteredRows.length+' 场，第 '+(currentPage+1)+' / '+pages+' 页，每页最多100场';el('prev').disabled=currentPage===0;el('next').disabled=currentPage>=pages-1;
}
el('scope').addEventListener('change',refreshMethods);for(const id of ['method','outcome'])el(id).addEventListener('change',()=>{currentPage=0;render();});el('search').addEventListener('input',()=>{currentPage=0;render();});el('prev').addEventListener('click',()=>{currentPage--;render();});el('next').addEventListener('click',()=>{currentPage++;render();});
globalThis.sportteryReport={data:DATA,render,refreshMethods,getFiltered:()=>filteredRows,getPage:()=>currentPage,status};refreshMethods();
</script></html>'''


if __name__ == "__main__":
    main()
