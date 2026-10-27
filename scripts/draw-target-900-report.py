#!/usr/bin/env python3
"""Render audited retrospective 900-draw target and label-shuffle controls."""
import csv
import hashlib
import html
import json
from pathlib import Path

import numpy as np

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "reports/draw-target-900-study"


def read(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def esc(s):
    return html.escape(str(s), quote=True)


def main():
    models = read(OUT / "models.json")
    rules = read(OUT / "rules.json")
    control = read(OUT / "permutation-control.json")
    audit = read(OUT / "review.json")
    assert audit.get("issues") == []
    rows = read(ROOT / "reports/draw-full-feature-study/dataset.json")["rows"]
    scores = np.load(OUT / "model-scores.npz", allow_pickle=False)
    ids = [r["matchId"] for r in rows]
    assert list(scores["matchIds"].astype(str)) == ids
    with (OUT / "rule-selections.csv").open(encoding="utf-8-sig", newline="") as f:
        selected_rows = list(csv.DictReader(f))
    assert [r["matchId"] for r in selected_rows] == ids
    labels = np.array([r["result"] for r in rows])
    methods = []
    def add(ident, label, mask, declared):
        mask = np.asarray(mask, bool)
        n, d = int(mask.sum()), int(np.sum(mask & (labels == 1)))
        expected_d = declared.get("TP", declared.get("draw"))
        assert (n, d) == (declared["selected"], expected_d)
        methods.append({"id": ident, "label": label, "n": n, "d": d, "fp": n - d,
                        "precision": d / n if n else 0, "recall": d / 970,
                        "selection": "".join("1" if v else "0" for v in mask)})
    for m in models["models"]:
        add(m["id"], "模型全历史拟合 · " + m["id"], scores[m["id"] + "_selected"], m["at900"])
    for r in rules["configurations"]:
        add(r["id"], "规则全历史挑选 · " + r["id"], [v[r["id"]] == "1" for v in selected_rows], r["stats"])
    def row_table(m):
        reached = m["d"] >= 900 and m["precision"] >= .3
        return f'<tr><td><button data-method="{esc(m["id"])}">{esc(m["label"])}</button></td><td>{m["n"]}</td><td>{m["d"]}</td><td>{m["fp"]}</td><td>{m["precision"]:.2%}</td><td>{"数值达标" if reached else "未达标"}</td></tr>'
    model_table = "".join(row_table(m) for m in methods[:len(models["models"])])
    rule_table = "".join(row_table(m) for m in sorted(methods[len(models["models"]):], key=lambda m: (-m["d"] >= -900, m["fp"])))
    controls = "".join(f'<tr><td>{c["permutationSeed"]}</td><td>{c["at900AgainstPermutedOwnFitLabels"]["selected"]}</td><td>900</td><td>{c["at900AgainstPermutedOwnFitLabels"]["FP"]}</td><td>{c["at900AgainstPermutedOwnFitLabels"]["precision"]:.2%}</td></tr>' for c in control["controls"])
    best = next(m for m in methods if m["id"] == rules["bestConfigurationId"])
    config = next(c for c in rules["configurations"] if c["id"] == rules["bestConfigurationId"])
    by_rule = {r["id"]: r for r in rules["rules"]}
    step_table = "".join(f'<tr><td>{i + 1}</td><td>{esc(by_rule[s["ruleId"]].get("label", s["ruleId"]))}</td><td>{s.get("removedDraws", 0)}</td><td>{s.get("removedNonDraws", 0)}</td><td>{s["remaining"]["draw"]}</td><td>{s["remaining"]["falseSelections"]}</td></tr>' for i, s in enumerate(config["steps"]))
    payload = {"methods": methods, "default": best["id"], "matches": [{k: r.get(k) for k in ("matchId", "matchTime", "league", "home", "guest", "homeScore", "awayScore", "result")} for r in rows]}
    encoded = json.dumps(payload, ensure_ascii=False).replace("<", "\\u003c")
    head = '<tr><th>方法（点击查看比赛）</th><th>入选</th><th>平局</th><th>误选</th><th>命中率</th><th>900＋30%目标</th></tr>'
    document = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>900场平局、30%命中目标核对</title>
<style>body{margin:0;background:#111820;color:#e5edf5;font:15px/1.7 system-ui}main{max-width:1450px;margin:auto;padding:28px}h1,h2{line-height:1.4}section{margin:22px 0;background:#1b2633;padding:20px;border-radius:10px}.alert{border-left:5px solid #fbbf24}.good{color:#67e8b0}.muted{color:#a8b7c8}table{border-collapse:collapse;width:100%;font-size:14px}th,td{border-bottom:1px solid #3b4c60;text-align:left;padding:9px;vertical-align:top}th{color:#a9cbed}button,select,input{font:inherit;color:inherit;background:#26384b;border:1px solid #58718b;border-radius:5px;padding:5px 9px}button{cursor:pointer}a{color:#83caff}.scroll{overflow:auto;max-height:650px}.toolbar{display:flex;gap:12px;flex-wrap:wrap;margin:12px 0}.number{font-size:24px;font-weight:bold}</style><main>
<h1>900场平局、30%命中：历史目标核对</h1>
<section><p>本地冻结样本：4161场，970场平局，实际平率23.31%。目标：找回至少900场平局且入选平率≥30%；恰好900场时，最多入选3000场、误选2100场。</p><p>反向等价目标：排除1161场，其中至多70场平局、至少1091场非平局。大数定律不保证记录字段能识别这些比赛，也不规定每场有三分之一概率平局。</p></section>
<section class="alert"><h2>随机赛果也能被模型拟合达标</h2><p>主配置固定后，将970个平局标签随机打乱5次；每次用自己的打乱标签拟合并评分，五次都达到900场、30%。因此，历史训练内达标不能单独证明提取到了有效平局特征。这是容量诊断，不是正式置换显著性检验。</p><table><tr><th>打乱种子</th><th>入选</th><th>打乱标签中的正例</th><th>打乱标签中的误选</th><th>训练内命中率</th></tr>__CONTROLS__</table></section>
<section><h2>可解释规则：目标尚未达到</h2><p class="number">900平局 / 3172入选＝28.37%，误选2272场</p><p>最多32条排除规则，每条最多两个主要条件，样本量作为明确护栏。比旧规则900平局＋2603误选减少331场误选；还需在保留900场时再减少172场误选。有限搜索不是全局最优结论；规则同样经过全部历史标签挑选。</p><p><a href="rules.json">完整规则与全部步骤</a> · <a href="rule-selections.csv">逐场选择CSV</a> · <a href="review.json">独立复算</a></p></section>
<section><h2>9个模型配置：均为全历史拟合</h2><p>historyOnly仅使用123个严格历史字段，去掉赔率、排名、热度与盈亏。clean使用市场＋严格历史；all另含页面派生。240棵浅树可以组合大量条件，不等于一条简短有效特征。</p><table>__HEAD____MODELS__</table><p class="muted">均赔基准同900目标：3601场、误选2701。缺均赔统一填全历史平率，并列全部保留；同选量比较可能因并列扩大场数，详见models.json实际N，不假称完全同场数。</p></section>
<section><h2>40个规则配置</h2><div class="scroll"><table>__HEAD____RULES__</table></div></section>
<section><h2>最佳规则的逐步排除</h2><div class="scroll"><table><tr><th>步骤</th><th>排除条件（取并集）</th><th>新排除平局</th><th>新排除非平局</th><th>剩余平局</th><th>剩余误选</th></tr>__STEPS__</table></div></section>
<section><h2>逐场核查</h2><div class="toolbar"><select id="method"></select><select id="outcome"><option value="selected">全部入选</option><option value="draw">命中的平局</option><option value="false">误选非平局</option><option value="miss">漏掉的平局</option><option value="removed">排除的非平局</option></select><input id="search" placeholder="球队 / 比赛ID"><button id="prev">上一页</button><button id="next">下一页</button></div><p id="totals"></p><p id="page"></p><div class="scroll"><table><tr><th>比赛ID / 时间</th><th>联赛</th><th>主队 / 客队</th><th>比分</th></tr><tbody id="rows"></tbody></table></div></section>
<p><a href="models.json">模型参数与字段</a> · <a href="permutation-control.json">随机标签诊断</a> · <a href="prior-audit.json">旧结果核查</a></p>
<script id="payload" type="application/json">__DATA__</script><script>
const data=JSON.parse(document.getElementById('payload').textContent);const el=id=>document.getElementById(id);let page=0;const escape=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
el('method').innerHTML=data.methods.map(m=>`<option value="${escape(m.id)}">${escape(m.label)}</option>`).join('');el('method').value=data.default;
function render(){const m=data.methods.find(m=>m.id===el('method').value);const mode=el('outcome').value,q=el('search').value.trim().toLowerCase();const list=data.matches.filter((r,i)=>{const yes=m.selection[i]==='1',draw=r.result===1;const match=mode==='draw'?yes&&draw:mode==='false'?yes&&!draw:mode==='miss'?!yes&&draw:mode==='removed'?!yes&&!draw:yes;return match&&(!q||[r.matchId,r.home,r.guest].some(v=>String(v??'').toLowerCase().includes(q)))});const pages=Math.max(1,Math.ceil(list.length/100));page=Math.max(0,Math.min(page,pages-1));el('totals').textContent=`本方法总体：入选${m.n}场，平${m.d}场，误选${m.fp}场，命中率${(100*m.precision).toFixed(2)}%；覆盖${(100*m.recall).toFixed(2)}%。搜索不会改变总体统计。`;el('page').textContent=`筛选${list.length}场，第${page+1}/${pages}页，每页最多100场`;el('rows').innerHTML=list.slice(page*100,page*100+100).map(r=>`<tr><td>${escape(r.matchId)}<br>${escape(r.matchTime)}</td><td>${escape(r.league)}</td><td>${escape(r.home)} / ${escape(r.guest)}</td><td>${escape(r.homeScore)}:${escape(r.awayScore)}</td></tr>`).join('');}
for(const id of ['method','outcome','search'])el(id).addEventListener(id==='search'?'input':'change',()=>{page=0;render()});el('prev').addEventListener('click',()=>{page--;render()});el('next').addEventListener('click',()=>{page++;render()});document.querySelectorAll('[data-method]').forEach(b=>b.addEventListener('click',()=>{el('method').value=b.dataset.method;page=0;render();el('method').scrollIntoView({block:'center'})}));render();
</script></main></html>'''
    for token, value in [("__CONTROLS__", controls), ("__HEAD__", head), ("__MODELS__", model_table), ("__RULES__", rule_table), ("__STEPS__", step_table), ("__DATA__", encoded)]:
        document = document.replace(token, value)
    assert "__DATA__" not in document
    target = OUT / "report.html"
    target.write_text(document, encoding="utf-8")
    summary = {"modelConfigurations": len(models["models"]), "ruleConfigurations": len(rules["configurations"]),
               "negativeControls": len(control["controls"]), "matchRows": len(rows), "bestRules": rules["best"],
               "allOutcomeCountsRecomputed": True, "reportSHA256": hashlib.sha256(target.read_bytes()).hexdigest(),
               "sources": {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in [OUT / "models.json", OUT / "rules.json", OUT / "permutation-control.json", OUT / "review.json", Path(__file__)]}}
    (OUT / "report-review.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False))


if __name__ == "__main__":
    main()
