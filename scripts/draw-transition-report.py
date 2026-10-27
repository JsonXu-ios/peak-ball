#!/usr/bin/env python3
"""Human-readable standalone report, reading already-computed research only."""
from __future__ import annotations

import hashlib
import html
import json
from html.parser import HTMLParser
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
OUT=ROOT/"reports/draw-transition-study"
esc=lambda value:html.escape(str(value))
pct=lambda value:"—" if value is None else f"{100*value:.2f}%"
COHORTS=(("all","全部确认完赛"),("sporttery","竞彩编号范围"),("nonSporttery","非竞彩编号范围"))


def metric_row(rule):
    a=rule["metrics"]["all"];d=rule["metrics"]["development"];t=rule["metrics"]["period3"]
    return f'<tr><td>{esc(rule["label"])}</td><td>{a["n"]}</td><td>{a["draws"]}</td><td>{a["falseSelections"]}</td><td>{pct(a["precision"])}</td><td>{pct(a["drawCoverage"])}</td><td>{d["draws"]}/{d["n"]}={pct(d["precision"])}</td><td>{t["draws"]}/{t["n"]}={pct(t["precision"])}</td><td>{pct(t["baseline"])}</td><td>{d["q"]:.4g}</td></tr>'


def rule_table(rules):
    head='<table><thead><tr><th>固定条件</th><th>入选</th><th>平局</th><th>误选</th><th>历史命中率</th><th>总平覆盖率</th><th>前2/3</th><th>后1/3内部复核</th><th>后段同输入基准</th><th>开发区BH q</th></tr></thead><tbody>'
    return head+''.join(metric_row(r) for r in rules)+'</tbody></table>'


def cells(rule):
    if "fourCells" not in rule:return ''
    parts=[f'<details><summary>{esc(rule["label"])}：四格交叉与误选代价</summary><p>A={esc(rule["parents"][0]["label"])}；B={esc(rule["parents"][1]["label"])}。四格全部使用共同完整输入，不把缺失当否。</p><table><tr><th>区间</th><th>A且B</th><th>A且非B</th><th>非A且B</th><th>都不符合</th><th>加法交互差</th></tr>']
    for key,label in (("all","全部快照"),("development","前2/3"),("period3","后1/3内部")):
        data=rule["fourCells"][key]
        values=''.join(f'<td>{data[c]["draws"]}/{data[c]["n"]}={pct(data[c]["precision"])}</td>' for c in ("both","aOnly","bOnly","neither"))
        diff=data['additiveInteractionPP'];parts.append(f'<tr><td>{label}</td>{values}<td>{diff:+.2f}个百分点</td></tr>' if diff is not None else f'<tr><td>{label}</td>{values}<td>暂无</td></tr>')
    all_cells=rule['fourCells']['all']
    constant_a=all_cells['bOnly']['n']+all_cells['neither']['n']==0
    constant_b=all_cells['aOnly']['n']+all_cells['neither']['n']==0
    note='<p class="note">至少一个条件在共同有效人群中恒真；这是人群或输入可用性限制，不能称为额外独立叠加信号。</p>' if constant_a or constant_b else ''
    parts.append('</table>'+note+'<p>交互差＝P(A且B)−P(A且非B)−P(非A且B)＋P(都不符合)，仅为描述值，未证明独立叠加或因果。</p><table><tr><th>母条件（同输入）</th><th>母条件场/平</th><th>加另一条件后提升</th><th>减少误选</th><th>少覆盖平局</th></tr>')
    for parent in rule["parents"]:
        inc=parent['childIncrementPP'];parts.append(f'<tr><td>{esc(parent["label"])}</td><td>{parent["sameInputsMatches"]}/{parent["sameInputsDraws"]}</td><td>{inc:+.2f}个百分点</td><td>{parent["removedFalseSelections"]}</td><td>{parent["removedDraws"]}</td></tr>' if inc is not None else '')
    parts.append('</table></details>');return ''.join(parts)


def asian_table(audit,cohort):
    mapping={"all":"all","sporttery":"sporttery","nonSporttery":"non_sporttery"};data=audit["cohorts"][mapping[cohort]]
    keys={"asian.move","asian.depth","heat.band","william.favoriteTransition","average.favoriteTransition","william.drawOddsMove","average.drawOddsMove"}
    parts=['<details><summary>亚盘／热度／热门方向变化完整单向分组</summary><table><tr><th>维度</th><th>类别</th><th>场</th><th>平</th><th>平率</th><th>同输入基准</th><th>总平覆盖率</th></tr>']
    for group in data["groups"]:
        if group['key'] not in keys:continue
        for row in group['categories']:
            parts.append(f'<tr><td>{esc(group["description"])}</td><td>{esc(row["category"])}</td><td>{row["matches"]}</td><td>{row["draws"]}</td><td>{pct(row["precision"])}</td><td>{pct(row["baselineDrawRate"])}</td><td>{pct(row["drawCoverageOfCohort"])}</td></tr>')
    parts.append('</table></details>');return ''.join(parts)


def sign_count_table(rows,cohort):
    pool=[r for r in rows if cohort=='all' or bool(str(r['jingcaiId'] or '').strip())==(cohort=='sporttery')]
    parts=['<details><summary>初盘与即时：负数／正数数量原始分组</summary><p>负数数量与正数数量分别列出；0是真实零值，不能把“没有负数”一概叫“三个正数”。各阶段各自完整输入分母，初即不是默认同一批。</p><table><tr><th>阶段与差值源</th><th>负／正计数</th><th>数量</th><th>场数</th><th>平局</th><th>平率</th><th>该源完整场数</th></tr>']
    for key,name in (('opening.ds','初盘体彩差'),('latest.ds','即时体彩差'),('opening.da','初盘均赔差'),('latest.da','即时均赔差')):
        valid=[r for r in pool if r['inputs'][key] is not None]
        for label,fn in (('负数',lambda x:x < -1e-9),('正数',lambda x:x > 1e-9)):
            for count in range(4):
                selected=[r for r in valid if sum(fn(x) for x in r['inputs'][key])==count];n=len(selected);d=sum(r['result']==1 for r in selected)
                parts.append(f'<tr><td>{name}</td><td>{label}</td><td>{count}</td><td>{n}</td><td>{d}</td><td>{pct(d/n if n else None)}</td><td>{len(valid)}</td></tr>')
    parts.append('</table></details>');return ''.join(parts)


def loss_focus_table(section):
    parts=[f'<p><strong>用户确认主口径：亏损超线＝盈亏率&lt;−28%；正常＝盈亏率≥−28%，包括所有正盈利。</strong>原目录中该范围有{section["qualifiedRules"]}条开发样本达标规则，通过原完整家族BH的正向规则{section["devBHSupported"]}条。下表只继承原开发排序，不重新搜索。</p><div class="scroll"><table><tr><th>条件（风险组）</th><th>全部风险组 场/平/平率</th><th>同背景正常组 场/平/平率</th><th>后段风险组</th><th>后段正常组</th><th>开发区完整家族q</th></tr>']
    for r in section['topDevelopment'][:5]:
        a=r['riskNormalSameContext']['all'];t=r['riskNormalSameContext']['period3']
        fmt=lambda v:f'{v["n"]}/{v["draws"]}/{pct(v["precision"])}'
        parts.append(f'<tr><td>{esc(r["label"])}</td><td>{fmt(a["risk"])}</td><td>{fmt(a["normal"])}</td><td>{fmt(t["risk"])}</td><td>{fmt(t["normal"])}</td><td>{r["metrics"]["development"]["q"]:.4g}</td></tr>')
    parts.append('</table></div><p>风险与正常在同一背景、同一真实输入可用人群中互补；缺失不进入正常组。此处“最低价方向”是均赔唯一最低主／客方向，各市场自身最低价的审计另列。</p>');return ''.join(parts)


def overlay_table(overlay):
    parts=['<details><summary>与原冻结模型的有限AND／OR对照</summary><p class="note">原模型训练见过这批赛果；交叉后的较高命中率不是独立验证。高命中分支覆盖明显下降；各分支与原模型取OR没有新增平局，只增加误选。</p>']
    for cohort,title in (('all','全部'),('sporttery','竞彩')):
        section=overlay['cohorts'][cohort];old=section['oldFrozen']
        parts.append(f'<p>{title}原模型：{old["TP"]}/{old["n"]}={pct(old["precision"])}，覆盖{pct(old["recall"])}。</p><table><tr><th>开发区既定前5条件</th><th>新条件独立 场/平/命中率</th><th>与原模型AND</th><th>AND总平覆盖率</th><th>OR场/平</th></tr>')
        for rule in section['topFive']:
            stats=rule['periods']['all']
            # Exact operation keys are part of the exported overlay contract.
            new=stats['newCondition'];both=stats['AND'];either=stats['OR']
            fmt=lambda m:f'{m["n"]}/{m["TP"]}/{pct(m["precision"])}'
            parts.append(f'<tr><td>{esc(rule["label"])}</td><td>{fmt(new)}</td><td>{fmt(both)}</td><td>{pct(both["recall"])}</td><td>{either["n"]}/{either["TP"]}</td></tr>')
        parts.append('</table>')
    parts.append('<p><a href="frozen-overlay.json">含逐场ID的完整叠加数据</a></p></details>');return ''.join(parts)


def confirmed_source_table(audit,cohort):
    mapping={'all':'allFinished','sporttery':'sportteryMarked','nonSporttery':'nonSportteryMarked'}
    axes=audit['cohorts'][mapping[cohort]]['sources']['official_sporttery_rate']['axes']
    parts=['<details open><summary>真实竞彩源站率：亏损超线与正常方向核对</summary><table><tr><th>方向口径</th><th>盈亏组</th><th>场数</th><th>平局</th><th>平率</th><th>该方向赢（非平）</th><th>相反方向赢</th></tr>']
    for axis,label in (('own_current_minimum_odds','该源最低赔率方向'),('asian_heat_above_65','亚盘热度>65%方向')):
        for key,name in (('risk','风险：<−28%'),('normal','正常：≥−28%')):
            row=axes[axis]['groups'][key]
            parts.append(f'<tr><td>{label}</td><td>{name}</td><td>{row["n"]}</td><td>{row["draw"]}</td><td>{pct(row["drawRate"])}</td><td>{row["selectedSideNonDrawWin"]}</td><td>{row["oppositeSideNonDrawWin"]}</td></tr>')
    parts.append('</table><p>两个方向口径分别比较，不是把“最低价风险”和“热度风险”强行看成同一筛选集合。非竞彩真实官方率仅1场，不能一般化；本地ROI的其他来源见完整审计。风险超线不等于该方向不能赢，更不等于一定平。</p></details>')
    return ''.join(parts)


def main():
    data=json.loads((OUT/"analysis.json").read_text(encoding='utf8'))
    audit=json.loads((OUT/"asian-audit.json").read_text(encoding='utf8'))
    raw_inputs=json.loads((OUT/'inputs.json').read_text(encoding='utf8'))
    focus=json.loads((OUT/'focused-loss28.json').read_text(encoding='utf8'))
    confirmed=json.loads((OUT/'confirmed-loss28-audit.json').read_text(encoding='utf8'))
    text=['<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>平局：符号、变动与盈亏交叉复核</title><style>body{font:15px/1.65 system-ui;max-width:1550px;margin:26px auto;padding:0 18px;background:#141a21;color:#e0e8f0}h1,h2{line-height:1.4}a{color:#79c5ff}table{width:100%;border-collapse:collapse;margin:15px 0;font-size:13px}th,td{border:1px solid #3c4b5c;text-align:left;padding:7px;vertical-align:top}th{background:#273748}td:first-child{max-width:520px}details{margin:14px 0;padding:8px;background:#1a2531}.note{background:#423921;padding:12px}.numbers{color:#87d0bb}code{color:#8bc9f2}.scroll{overflow:auto}</style><h1>平局特征：差值符号、初即变化、热门与盈亏</h1>']
    text.append(f'<p>固定{data["rootCount"]}项差值条件、{data["modifierCount"]}项盘口/热度/盈亏条件；共{data["candidateCount"]}项去重单条件或双条件交叉。没有加总评分、没有改生产模型、没有重启程序。</p>')
    text.append('<p>差值＝威廉−竞彩，或威廉−平均欧赔，按胜／平／负保留负、零、正。初盘与即时分别看完整形态、负数个数、平项符号、绝对值最小两项；再看转正／转负、分歧扩大／收窄、进入／退出两项。亚盘正线表示主队让球，负线表示主队受让；数字变化不直接等于真实实力变化。</p>')
    text.append('<p class="note">用户已确认：亏损超过28%＝盈亏率&lt;−28%；正常可出范围＝盈亏率≥−28%，包含0和所有正盈利。+28%不是主阈值，只保留为原探索分档对照。亏损界限是待验证参考，尚不能认定为真实庄家赔付底线。源站官方率、本地公司报价ROI、非让球模拟ROI各自保留来源。</p>')
    text.append(f'<p>按完整北京时间开球日分三段，切点{esc(data["timeCuts"])}。前两段筛选、后段仅作内部稳定性复核；这些历史已多次研究，后段也不是全新盲测。采集时间未证明在开赛前。所有覆盖率保留整个范围的平局分母。</p>')
    text.append('<p>三个源问题：亚盘热度由价格／盘口推算，与最低赔率方向并非独立；竞彩462场官方率中142场源赔率与当前选用赔率不同，不能假定同一时点；现数据没有真实初盘盈亏序列，所以只验证“初即盘口变化＋当前盈亏快照”，没有制造初盘盈亏。</p>')
    for cohort,label in COHORTS:
        section=data['cohorts'][cohort]
        text.append(f'<h2>{label}：{section["matches"]}场／{section["draws"]}平，基准{pct(section["drawRate"])}</h2><p>开发区通过BH q≤0.05且样本达标的规则{section["devBHSupportedRules"]}条。下表按开发区排序，不根据后段结果重排；同源字段高度相关，通过多条不表示多份独立证据。</p>')
        text.append(loss_focus_table(focus['cohorts'][cohort]))
        text.append(confirmed_source_table(confirmed,cohort))
        text.append(sign_count_table(raw_inputs,cohort))
        text.append('<h3>其余分档与初即变化的探索对照</h3><p>下表含[0%,28%]等原始探索分档，不能与已确认的“亏损超28%”主口径混为一谈。</p>')
        text.append('<div class="scroll">'+rule_table(section['topDevelopment'][:10])+'</div>')
        for rule in section['topDevelopment'][:5]:text.append(cells(rule))
        text.append('<details><summary>开发区排序的单条件对照</summary><div class="scroll">'+rule_table(section['topSinglesDevelopment'])+'</div></details>')
        text.append('<details><summary>全历史命中率最高的描述表（不是选定模型）</summary><p>该表按全部历史赛果排序，有事后挑选偏差；不能用来宣称未见比赛命中率。</p><div class="scroll">'+rule_table(section['fullHistoryHighestPrecisionDescriptive'])+'</div></details>')
        text.append(asian_table(audit,cohort))
        control=section['shuffle'];text.append(f'<p>全历史最大关联随机对照：{control["rules"]}条样本达标规则，{control["repetitions"]}轮时段内洗牌，每轮重取全目录最大值；观察最大Z={control["observedMaxZ"]:.3f}，随机最大值95%分位={control["randomMaxZ95"]:.3f}，经验P={control["familywiseEmpiricalP"]:.4f}。此项检验全历史最大关联，不是对“开发→后段”选择流程的检验。</p>')
    overlay=OUT/'frozen-overlay.json'
    if overlay.exists():text.append(overlay_table(json.loads(overlay.read_text(encoding='utf8'))))
    text.append('<p><a href="focused-loss28.json">已确认亏损28%主口径</a> · <a href="confirmed-loss28-audit.json">各自最低赔／热度方向风险与正常审计</a> · <a href="rules.csv">全部规则与各时段指标CSV</a> · <a href="sign-transitions.json">所有实际符号迁移格</a> · <a href="profit-audit.json">盈亏来源及原四分档</a> · <a href="asian-audit.json">亚盘原始方向审计</a> · <a href="protocol.json">原探索协议（主口径以后续确认为准）</a> · <a href="review.json">独立复核</a></p></html>')
    rendered=''.join(text);(OUT/'report.html').write_text(rendered,encoding='utf8')
    class Check(HTMLParser):
        def __init__(self):super().__init__();self.links=[];self.script=False
        def handle_starttag(self,tag,attrs):
            aa=dict(attrs)
            if tag in ('img','script','iframe') and aa.get('src','').startswith(('http:','https:')):self.script=True
            if tag=='a':self.links.append(aa['href'])
    check=Check();check.feed(rendered)
    missing=[name for name in check.links if not (OUT/name).exists()]
    # Review may still be running; the report link is intentionally stable.
    assert not check.script and not [name for name in missing if name!='review.json']
    write={"status":"passed","externalAssets":False,"links":len(check.links),"pendingReviewLink":'review.json' in missing,"sha256":hashlib.sha256(rendered.encode()).hexdigest(),"characters":len(rendered)}
    (OUT/'report-review.json').write_text(json.dumps(write,indent=2),encoding='utf8')
    print(json.dumps(write))


if __name__=='__main__':main()
