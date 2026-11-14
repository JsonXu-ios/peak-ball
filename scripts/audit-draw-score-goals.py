#!/usr/bin/env python3
"""Offline descriptive score/goal distributions and exact Asian totals settlement.

No model fitting, collection, source writes or serving-rule changes. Result
groups are retrospective labels, never features available before kickoff.
"""
from __future__ import annotations

import argparse
from collections import Counter, defaultdict
from datetime import datetime, timedelta, timezone
import hashlib
import json
import math
from pathlib import Path
import statistics
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
GROUPS = ("draw_hit", "alternative_hit", "both_miss", "alternative_unavailable_non_draw")
LABELS = {"selected_total": "全部原入选", "draw_hit": "平局命中", "alternative_hit": "次选命中", "both_miss": "主次均未命中", "alternative_unavailable_non_draw": "非平且次选不可用"}
SCOPES = {"all": "竞彩与非竞彩合计", "jc": "竞彩", "nonjc": "非竞彩"}
SETTLEMENTS = ("full_win", "half_win", "push", "half_loss", "full_loss")
BJT = timezone(timedelta(hours=8))


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def number(v):
    return type(v) in (int, float) and math.isfinite(v)


def day(raw):
    return datetime.fromisoformat(raw.replace("Z", "+00:00")).astimezone(BJT).date().isoformat()


def backup(raw):
    frontend = raw.get("frontend") or {}
    if "averageEuroOdds" in frontend:
        prices = frontend["averageEuroOdds"]
    else:
        market = frontend.get("localMarket") or {}
        prices = [market.get("odds", {}).get(s) for s in ("home", "draw", "away")] if market.get("key") == "localAverageOdds" and market.get("oddsAvailable") else None
    if not isinstance(prices, list) or len(prices) < 3 or not all(number(prices[i]) and prices[i] > 1 for i in (0, 2)):
        return None
    if abs(prices[0] - prices[2]) <= 1e-9:
        return None
    return 0 if prices[0] < prices[2] else 2


def chinese_integer(s):
    digits = {"零": 0, "〇": 0, "一": 1, "二": 2, "两": 2, "三": 3, "四": 4, "五": 5, "六": 6, "七": 7, "八": 8, "九": 9}
    if s in digits:
        return digits[s]
    if s == "十":
        return 10
    if s.startswith("十") and s[1:] in digits:
        return 10 + digits[s[1:]]
    return None


def parse_goal_line(raw):
    if isinstance(raw, bool) or raw is None:
        return None
    s = str(raw).strip().replace("／", "/").replace(" ", "")
    parts = s.split("/")
    if len(parts) > 2:
        return None
    values = []
    for part in parts:
        try:
            value = float(part)
        except ValueError:
            part = part.replace("球", "")
            if part == "半":
                value = .5
            else:
                half = part.endswith("半")
                integer = chinese_integer(part[:-1] if half else part)
                if integer is None:
                    return None
                value = integer + (.5 if half else 0)
        if not math.isfinite(value):
            return None
        values.append(value)
    if len(values) == 2 and abs(values[0] - values[1]) > .5 + 1e-9:
        return None
    value = sum(values) / len(values)
    return value if 0 < value <= 15 and abs(value * 4 - round(value * 4)) < 1e-8 else None


def water(raw):
    try:
        value = float(raw)
    except (ValueError, TypeError):
        return None
    return value if math.isfinite(value) and value > 0 else None


def real_line(raw, phase):
    # One named company, one phase, one raw line and its two corresponding
    # water quotes. Never use a local calculation/history fallback or fill
    # missing Bet365 data from a different bookmaker.
    quotes = [q for q in raw.get("companyQuotes") or [] if q.get("key") == "bet365"]
    if len(quotes) != 1:
        return None
    line = quotes[0].get("goalsLine") or {}
    if line.get("source") != "pankou_moneys" or line.get("company") != "Bet365":
        return None
    value = parse_goal_line(line.get(phase))
    if value is None:
        return None
    odds = line.get(phase + "Odds") or []
    return {"line": value, "overWater": water(odds[0]) if len(odds) > 0 else None,
            "underWater": water(odds[1]) if len(odds) > 1 else None,
            "raw": line.get(phase), "capturedAt": line.get("capturedAt"), "company": line["company"]}


def settle(total, line, direction, hk):
    quarters = int(round(line * 4))
    legs = [(quarters - 1) / 4, (quarters + 1) / 4] if quarters % 2 else [line]
    results = [(1 if total < leg else -1 if total > leg else 0) * (1 if direction == "under" else -1) for leg in legs]
    # Values are fractions of the entire stake; the other half can push.
    result = sum(results) / len(results)
    label = {1: "full_win", .5: "half_win", 0: "push", -.5: "half_loss", -1: "full_loss"}[result]
    profit = sum(hk if x > 0 else -1 if x < 0 else 0 for x in results) / len(results) if hk is not None else None
    return label, profit


def distribution(counter, key):
    total = sum(counter.values())
    return [{key: k, "n": n, "fraction": n / total if total else None} for k, n in sorted(counter.items(), key=lambda item: (-item[1], str(item[0])))]


def bet_stats(entries, phase, direction):
    counts = Counter()
    profits = []
    safe_profits = []
    water_anomalies = []
    line_counts = Counter()
    known_capture = 0
    for entry in entries:
        quote = entry[phase]
        if quote is None:
            continue
        hk = quote[direction + "Water"]
        label, profit = settle(entry["totalGoals"], quote["line"], direction, hk)
        counts[label] += 1
        line_counts[quote["line"]] += 1
        known_capture += bool(quote["capturedAt"])
        if profit is not None:
            profits.append(profit)
            paired_anomaly = any(v is not None and v > 2 for v in (quote['underWater'], quote['overWater']))
            if not paired_anomaly:
                safe_profits.append(profit)
            else:
                water_anomalies.append({"matchId": entry["matchId"], "line": quote["line"], "water": hk, "bothWater": [quote['overWater'], quote['underWater']], "net": profit})
    n = sum(counts.values())
    winners = counts["full_win"] + counts["half_win"]
    return {"n": n, "missingLine": len(entries) - n, **{k: counts[k] for k in SETTLEMENTS},
            "profitable": winners, "profitRate": winners / n if n else None,
            "nonLossRate": (winners + counts["push"]) / n if n else None,
            "waterAvailable": len(profits), "netUnits": sum(profits), "roi": sum(profits) / len(profits) if profits else None,
            "waterAbove2": water_anomalies, "waterAtMost2N": len(safe_profits),
            "waterAtMost2Roi": sum(safe_profits) / len(safe_profits) if safe_profits else None,
            "captureKnown": known_capture,
            "lineDistribution": [{"line": k, "n": v} for k, v in sorted(line_counts.items())]}


def summarize(entries, details=True):
    n = len(entries)
    scores = Counter(f"{e['homeScore']}:{e['awayScore']}" for e in entries)
    goals = Counter(e["totalGoals"] for e in entries)
    oriented = Counter()
    for e in entries:
        if e["backup"] is not None:
            a, b = e["homeScore"], e["awayScore"]
            oriented[f"{a}:{b}" if e["backup"] == 0 else f"{b}:{a}"] += 1
    result = {"n": n, "backupAvailable": sum(e["backup"] is not None for e in entries),
              "scores": distribution(scores, "score"), "alternativeOrientedScores": distribution(oriented, "score"),
              "goals": [{"goals": k, "n": v, "fraction": v / n if n else None} for k, v in sorted(goals.items())],
              "meanGoals": sum(e["totalGoals"] for e in entries) / n if n else None,
              "medianGoals": statistics.median(e["totalGoals"] for e in entries) if n else None,
              "goalsBands": {"0": goals[0], "1": goals[1], "2": goals[2], "3": goals[3], "4": goals[4], "5+": sum(v for k, v in goals.items() if k >= 5)},
              "scoreTopThreeRetrospectiveCoverage": sum(v for _, v in scores.most_common(3)) / n if n else None,
              "settlement": {phase: {direction: bet_stats(entries, phase, direction) for direction in ("under", "over")} for phase in ("opening", "latest")}}
    if details:
        by_line = defaultdict(list)
        for entry in entries:
            if entry["latest"] is not None:
                by_line[entry["latest"]["line"]].append(entry)
        result["latestLineBuckets"] = [{"line": line, "n": len(pool), "scores": distribution(Counter(f"{e['homeScore']}:{e['awayScore']}" for e in pool), "score"),
                                        "under": bet_stats(pool, "latest", "under"), "over": bet_stats(pool, "latest", "over")} for line, pool in sorted(by_line.items())]
    return result


def cohort_report(entries, details=True):
    result = {"selected_total": summarize(entries, details)}
    result.update({group: summarize([e for e in entries if e["group"] == group], details) for group in GROUPS})
    assert sum(result[g]["n"] for g in GROUPS) == len(entries)
    for phase in ("opening", "latest"):
        for side in ("under", "over"):
            for field in ("n", *SETTLEMENTS):
                assert sum(result[g]["settlement"][phase][side][field] for g in GROUPS) == result["selected_total"]["settlement"][phase][side][field]
    return result


def checks():
    for raw, expected in [("2.25", 2.25), ("二球/二球半", 2.25), ("两球半", 2.5), ("2.5/3", 2.75), ("三球", 3), ("半球/一球", .75), ("七球半/八球", 7.75), ("十球半", 10.5)]:
        assert parse_goal_line(raw) == expected, (raw, parse_goal_line(raw))
    for raw in (None, "", "abc", "受让两球", "-2.5", "0", "2.2", "nan", "2/4"):
        assert parse_goal_line(raw) is None, raw
    assert settle(2, 2.25, "under", .9) == ("half_win", .45)
    assert settle(2, 2.25, "over", .9) == ("half_loss", -.5)
    assert settle(3, 2.75, "under", .9) == ("half_loss", -.5)
    assert settle(3, 2.75, "over", .9) == ("half_win", .45)
    assert settle(2, 2, "under", .9) == ("push", 0)
    assert settle(2, 2.5, "under", .9) == ("full_win", .9)
    assert settle(3, 2.5, "under", .9) == ("full_loss", -1)
    for line in (1, 1.25, 1.5, 1.75, 2, 2.25, 2.5, 2.75, 3, 3.25, 3.5, 4, 7.75):
        for total in range(16):
            u, up = settle(total, line, "under", 1)
            o, op = settle(total, line, "over", 1)
            assert abs(up + op) < 1e-12
            assert SETTLEMENTS.index(u) + SETTLEMENTS.index(o) == 4
    probe = {"frontend": {"averageEuroOdds": [2.1, None, 3.4]}, "result": 1, "homeScore": 0, "awayScore": 0}
    assert backup(probe) == 0
    changed = {**probe, "result": 2, "homeScore": 0, "awayScore": 20}
    assert backup(changed) == backup(probe)
    return {"quarterLineExamples": "passed", "overUnderSymmetry": "passed", "backupLabelBlind": "passed", "sourceOnlyNoCrossFill": True}


def html_report(report, path):
    # Full embedded JSON is escaped as data, never interpolated into HTML labels.
    payload = json.dumps(report, ensure_ascii=False, allow_nan=False).replace("<", "\\u003c").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")
    template = r'''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>入选平局：比分与大小球结算</title>
<style>body{font:15px/1.6 system-ui,'Microsoft YaHei',sans-serif;margin:0;background:#f4f6fa;color:#192435}main{max-width:1280px;margin:auto;padding:28px}h1{font-size:26px;margin:0}h2{font-size:19px;margin-top:24px}p{color:#536174}.panel,.card{background:white;border:1px solid #dce2ec;border-radius:12px;padding:18px;margin:14px 0}.controls{display:flex;gap:18px;flex-wrap:wrap;position:sticky;top:0;z-index:2;box-shadow:0 4px 15px #24355110}label{display:flex;gap:9px;align-items:center}select{font:inherit;padding:7px;border:1px solid #c4cedc;border-radius:6px}table{width:100%;border-collapse:collapse;font-size:14px}td,th{text-align:right;padding:8px 10px;border-bottom:1px solid #e5eaf1;white-space:nowrap}td:first-child,th:first-child{text-align:left}th{background:#f2f5fa}.grid{display:grid;grid-template-columns:repeat(3,1fr);gap:15px}.big{font-size:27px;font-weight:700;color:#1c62d1}.muted{color:#637188;font-size:13px}.scroll{overflow:auto}.barrow{display:grid;grid-template-columns:64px 1fr 88px;gap:9px;margin:6px 0;align-items:center}.bar{background:#dbe9ff;height:20px;min-width:1px;border-radius:4px}.warning{border-left:4px solid #e1a227;padding:10px 15px;background:#fff8e9}.positive{color:#087443}.negative{color:#b33c29}@media(max-width:750px){main{padding:14px}.grid{grid-template-columns:1fr}.controls{position:static}}@media print{.controls{position:static}.panel{break-inside:avoid}body{background:white}}</style>
<main><h1>原模型入选后的比分、进球与大小球结算</h1><p id="source"></p><div class="warning">分组按真实赛果回看，不能作为赛前决策条件。盘口为真实 Bet365 单家公司原始初盘/即时盘；缺失留空。盘口采集时点未确认，收益仅为当前快照的历史结算，包含原模型训练样本。</div>
<div class="panel controls"><label>北京时间窗<select id="window"><option value="all">完整历史快照</option><option value="since_2026_10_25">10月25日起</option><option value="since_2026_11_01">11月1日起</option></select></label><label>范围<select id="scope"><option value="all">全部</option><option value="jc">竞彩</option><option value="nonjc">非竞彩</option></select></label><label>赛果分组<select id="group"><option value="selected_total">全部原入选</option><option value="draw_hit">平局命中</option><option value="alternative_hit">次选命中</option><option value="both_miss">主次均未命中</option><option value="alternative_unavailable_non_draw">非平且次选不可用</option></select></label><label>大小球盘口<select id="phase"><option value="latest">即时盘</option><option value="opening">初盘</option></select></label></div>
<h2>互斥分组与完整分母</h2><div class="panel scroll" id="cohorts"></div><div class="grid" id="cards"></div>
<div class="grid" style="grid-template-columns:1fr 1fr"><section class="panel"><h2>总进球完整分布</h2><div id="goals"></div></section><section class="panel"><h2>最常见主客比分</h2><div id="scores"></div><p class="muted">比分始终主队在前。前三集中度属于事后分组内频率，不是已验证的三个推荐准确率。</p></section></div>
<h2>大小球五档结算</h2><div class="panel scroll" id="settlement"></div><p class="muted">每场独立投注1单位；盈利场率=(全赢+赢半)/盘口可结算场次，走水不计盈利。2.25拆2与2.5各半；小2.25实际2球是赢半，水位0.90时净赚0.45单位。净收益率按同场同相水位计算，未计手续费。</p>
<h2>即时盘逐盘口分布</h2><div class="panel scroll" id="lines"></div><details class="panel"><summary>完整比分分布（主客原视角）</summary><div id="fullscores" class="scroll"></div></details><details class="panel"><summary>按次选方在前的比分分布</summary><p class="muted">选择客队时翻转比分，观察“次选方进球:对手进球”；不同于主客原视角。</p><div id="oriented" class="scroll"></div></details><details class="panel"><summary>盘口水位异常与来源审计</summary><div id="audit"></div></details></main>
<script>const DATA=__DATA__;const $=x=>document.getElementById(x);const pct=x=>x==null?'—':(x*100).toFixed(2)+'%';const esc=x=>String(x).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));const table=(heads,rows)=>'<table><thead><tr>'+heads.map(x=>'<th>'+esc(x)+'</th>').join('')+'</tr></thead><tbody>'+rows.map(r=>'<tr>'+r.map(x=>'<td>'+esc(x)+'</td>').join('')+'</tr>').join('')+'</tbody></table>';function bars(rows,key){let max=Math.max(1,...rows.map(r=>r.n));return rows.map(r=>'<div class="barrow"><span>'+esc(r[key])+'</span><div class="bar" style="width:'+r.n/max*100+'%"></div><span>'+r.n+' / '+pct(r.fraction)+'</span></div>').join('')||'无样本'}function draw(){const s=$('scope').value,g=$('group').value,p=$('phase').value,w=$('window').value,cohorts=w==='all'?DATA.scopes:DATA.windows[w],c=cohorts[s],r=c[g],u=r.settlement[p].under,o=r.settlement[p].over;$('source').textContent='快照北京时间 '+DATA.source.snapshotBeijing+'；确认完赛 '+DATA.source.matches+' 场，原模型入选 '+cohorts.all.selected_total.n+' 场（当前时间窗）。';$('cohorts').innerHTML=table(['范围','原入选','平局命中','次选命中','主次均未命中','非平次选不可用'],Object.entries(cohorts).map(([k,v])=>[DATA.scopeLabels[k],v.selected_total.n,...DATA.groupOrder.map(x=>v[x].n)]));$('cards').innerHTML=[['当前组场次',r.n,'均值 '+(r.meanGoals?.toFixed(2)??'—')+' 球；中位 '+(r.medianGoals??'—')+' 球'],['买小盈利场率',pct(u.profitRate),'盘口可结算 '+u.n+' 场；缺盘口 '+u.missingLine+' 场'],['买大盈利场率',pct(o.profitRate),'盘口可结算 '+o.n+' 场；缺盘口 '+o.missingLine+' 场']].map(a=>'<div class="card"><div>'+esc(a[0])+'</div><div class="big">'+esc(a[1])+'</div><div class="muted">'+esc(a[2])+'</div></div>').join('');$('goals').innerHTML=bars(r.goals,'goals');$('scores').innerHTML=bars(r.scores.slice(0,10),'score');$('settlement').innerHTML=table(['方向','可结算','全赢','赢半','走水','输半','全输','盈利场率','不亏场率','净收益/投注额'],[['买小',u],['买大',o]].map(([name,a])=>[name,a.n,a.full_win,a.half_win,a.push,a.half_loss,a.full_loss,pct(a.profitRate),pct(a.nonLossRate),pct(a.roi)]));$('lines').innerHTML=table(['即时盘口','场次','买小盈利率','买大盈利率','买小净收益率','买大净收益率'],(r.latestLineBuckets||[]).map(b=>[b.line,b.n,pct(b.under.profitRate),pct(b.over.profitRate),pct(b.under.roi),pct(b.over.roi)]));const scoreTable=a=>table(['比分','场次','组内占比'],a.map(x=>[x.score,x.n,pct(x.fraction)]));$('fullscores').innerHTML=scoreTable(r.scores);$('oriented').innerHTML=scoreTable(r.alternativeOrientedScores);$('audit').innerHTML=table(['方向','有效水位','水位>2','排除水位>2后的ROI','盘口采集时点已知'],[['小',u],['大',o]].map(([n,a])=>[n,a.waterAvailable,a.waterAbove2.length,pct(a.waterAtMost2Roi),a.captureKnown]));}for(const id of ['window','scope','group','phase'])$(id).addEventListener('change',draw);draw();</script></html>'''
    path.write_text(template.replace("__DATA__", payload), encoding="utf-8")


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--study", type=Path, default=ROOT / "reports/nonjc-draw-secondary-20261104/current-study.json")
    p.add_argument("--replay", type=Path, default=ROOT / "reports/nonjc-draw-secondary-20261104/model-replay.json")
    p.add_argument("--out", type=Path, default=ROOT / "reports/score-goals-audit-20261104")
    args = p.parse_args()
    check_result = checks()
    source, replay = read(args.study), read(args.replay)
    source_sha = sha(args.study)
    assert source_sha == replay["current"]["inputSHA256"], "replay is not from this exact source"
    assert len(source["rows"]) == source["audit"]["settledRegulationMatches"]
    models = {r["matchId"]: r for r in replay["current"]["rows"]}
    assert len(models) == len(source["rows"]) == len({r["matchId"] for r in source["rows"]})
    entries = []
    no_inputs = 0
    for raw in source["rows"]:
        model = models[raw["matchId"]]
        if model["observedFeatures"] <= 0:
            no_inputs += 1
            continue
        if not model["selected"]:
            continue
        h, a = raw["homeScore"], raw["awayScore"]
        assert type(h) is int and type(a) is int and h >= 0 and a >= 0
        result = 0 if h > a else 2 if h < a else 1
        assert result == raw["result"]
        alternative = backup(raw)  # Picks read only source prices, not labels.
        group = "draw_hit" if result == 1 else "alternative_unavailable_non_draw" if alternative is None else "alternative_hit" if result == alternative else "both_miss"
        entries.append({"matchId": raw["matchId"], "home": raw["home"], "guest": raw["guest"], "dateBeijing": day(raw["matchTime"]),
                        "jingcaiId": raw["jingcaiId"], "scope": "jc" if raw["jingcaiId"].strip() else "nonjc", "homeScore": h, "awayScore": a,
                        "totalGoals": h + a, "backup": alternative, "result": result, "group": group,
                        "opening": real_line(raw, "opening"), "latest": real_line(raw, "latest")})
    scopes = {scope: cohort_report([e for e in entries if scope == "all" or e["scope"] == scope]) for scope in SCOPES}
    # Compare independent audits when available, never silently substitute one.
    independent_path = args.out / "group-independent.json"
    if independent_path.exists():
        independent = read(independent_path)
        assert independent["sourceSHA256"] == source_sha
        for scope in SCOPES:
            other = independent["groups"][scope]
            assert scopes[scope]["selected_total"]["n"] == other["selected"]
            for group in GROUPS:
                assert scopes[scope][group]["n"] == other["counts"].get(group, 0)
                other_scores = other["groupDetails"].get(group, {}).get("scoreDistribution", [])
                assert {r["score"]: r["n"] for r in scopes[scope][group]["scores"]} == {r["score"]: r["n"] for r in other_scores}
        check_result["independentGroupCountsAndScoreFrequencies"] = "passed"
    independent_goals = args.out / "goals-independent.json"
    if independent_goals.exists():
        other_report = read(independent_goals)
        assert other_report["generatedAt"] == source["generatedAt"]
        for scope in SCOPES:
            for group in ("selected_total", *GROUPS):
                other_key = scope + "." + ("selected" if group == "selected_total" else group)
                if other_key not in other_report["groups"]:
                    assert scopes[scope][group]["n"] == 0, other_key
                    continue
                other_group = other_report["groups"][other_key]
                for phase in ("opening", "latest"):
                    for side in ("under", "over"):
                        ours = scopes[scope][group]["settlement"][phase][side]
                        theirs = other_group["phases"][phase][side]
                        assert ours["n"] == other_group["phases"][phase]["available"]
                        for field, key in zip(SETTLEMENTS, ("win", "half_win", "push", "half_loss", "loss")):
                            assert ours[field] == theirs["counts"].get(key, 0), (scope, group, phase, side, field)
                        assert abs(ours["netUnits"] - theirs["netUnitTotal"]) < 1e-7, (scope, group, phase, side)
        check_result["independentBothPhaseFiveWaySettlementAndNetProfit"] = "passed"
    report = {"source": {"path": str(args.study), "sha256": source_sha, "matches": len(source["rows"]), "generatedAt": source["generatedAt"],
                         "snapshotBeijing": datetime.fromisoformat(source["generatedAt"].replace("Z", "+00:00")).astimezone(BJT).strftime("%Y-%m-%d %H:%M:%S"),
                         "modelArtifactSHA256": replay["modelArtifactSHA256"], "modelThreshold": replay["threshold"], "excludedNoInputs": no_inputs},
              "method": {"selection": "Original frozen model selected with observedFeatures>0; no secondary filter, no retraining.",
                         "groups": "Mutually exclusive actual draw, correct backup, wrong backup, no backup non-draw. Draw with no backup remains draw_hit.",
                         "backup": "Same real latest average EURO HOME/AWAY lower price as production, no ties or simulated replacement.",
                         "goalsLine": "Only same-company Bet365 source=pankou_moneys opening/latest original goals line and corresponding [over,under] HK water.",
                         "settlement": "Quarter line split equally across adjacent half-goal lines; profitable=(full_win+half_win)/valid_lines. Push is not profit. ROI=sum(net_units)/valid_water_n.",
                         "dataTiming": "Unknown source capture times; retrospective current-snapshot association including model training data, not verified prospective betting returns.",
                         "tree": "Result groups are diagnostic targets, never features for a pre-match decision tree."},
              "scopeLabels": SCOPES, "groupOrder": list(GROUPS), "groupLabels": LABELS,
              "scopes": scopes, "windows": {}, "checks": check_result, "servicesStarted": False, "modelChanged": False}
    for name, cutoff in [("since_2026_10_25", "2026-10-25"), ("since_2026_11_01", "2026-11-01")]:
        report["windows"][name] = {scope: cohort_report([e for e in entries if e["dateBeijing"] >= cutoff and (scope == "all" or e["scope"] == scope)]) for scope in SCOPES}
    args.out.mkdir(parents=True, exist_ok=True)
    (args.out / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2, allow_nan=False), encoding="utf-8")
    (args.out / "settled-rows.json").write_text(json.dumps(entries, ensure_ascii=False, separators=(",", ":"), allow_nan=False), encoding="utf-8")
    html_report(report, args.out / "report.html")
    print(json.dumps({"status": "passed", "report": str(args.out / "report.html"), "checks": check_result,
                      "selected": {s: scopes[s]["selected_total"]["n"] for s in SCOPES},
                      "latestUnder": {g: {k: scopes["all"][g]["settlement"]["latest"]["under"][k] for k in ("n", "full_win", "half_win", "push", "half_loss", "full_loss", "profitRate", "roi")} for g in ("selected_total", *GROUPS)}}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
