#!/usr/bin/env python3
"""Offline audit of captured public pages against the existing VIPC contract.

Does not call production services, connect to databases, or modify collectors.
Run after saving DOM captures and the official fixed-bonus JSON in the probe dir.
"""
import argparse
import csv
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def rows(doc):
    return [r for r in max(doc["tables"], key=len) if len(r) >= 15 and r[0].isdigit()]


def clean(value):
    return re.sub("[↑↓]", "", value)


def structs(path):
    result = {}
    text = path.read_text(encoding="utf-8-sig")
    for name, body in re.findall(r"type (\w+) struct \{(.*?)\n\}", text, re.S):
        result[name] = re.findall(r'^\s*\w+\s+(\S+)\s+`json:"([^",]+)', body, re.M)
    return result


def flatten(defs, name, prefix=""):
    for kind, field in defs[name]:
        if field == "-":
            continue
        path = prefix + field
        child = kind.removeprefix("[]")
        if child in defs:
            yield from flatten(defs, child, path + ("[]." if kind.startswith("[]") else "."))
        else:
            yield path, kind


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("capture_dir", type=Path)
    args = parser.parse_args()
    p = args.capture_dir.resolve()
    captures = {k: read_json(p / (k + ".dom.json")) for k in ("euro", "asian", "totals", "history", "support-had", "support-hhad", "official-euro-history")}
    bonus = read_json(p / "fixed-bonus.browser.txt")
    if str(bonus.get("errorCode")) != "0":
        raise ValueError("Fixed-bonus response was not successful")
    history = bonus["value"]["oddsHistory"]
    if history["homeTeamAllName"] != "日本" or history["awayTeamAllName"] != "巴西":
        raise ValueError("Unexpected match identity")
    normalized = {
        "identity": {"home": "日本", "away": "巴西", "kickoffChina": "2026-11-14 18:15:00", "zgzcwMatchId": "4650133", "sportteryRequestMatchId": "2042753", "officialResponseMatchId": history.get("matchId"), "vipcMatchId": None, "lotteryNumber": "周六003"},
        "scope": "One match; VIPC field-contract comparison, not same-time VIPC numeric comparison",
        "officialQuoteHistory": {k: history[k] for k in ("hadList", "hhadList", "crsList", "ttgList", "hafuList", "singleList")},
        "scoreDrawLatest": {"sd00": history["crsList"][-1]["s00s00"], "sd11": history["crsList"][-1]["s01s01"], "sd22": history["crsList"][-1]["s02s02"], "sd33": history["crsList"][-1]["s03s03"], "stop": None},
        "totalGoalsLatest": {**{"t" + str(i): history["ttgList"][-1]["s" + str(i)] for i in range(8)}, "stop": None},
        "supportPages": {k: next(t for t in captures[k]["tables"] if "巴西" in t) for k in ("support-had", "support-hhad")},
        "bookmakerRows": {},
        "warnings": ["Missing fields stay null; no synthetic zero or invented profit", "isCancel=0 is not proof that bf.stop or jq.stop is false", "Bookmaker labels remain exactly as published; IDs require a separate mapping", "Source pages were captured sequentially and are not synchronized snapshots"],
    }
    for k in ("euro", "asian", "totals"):
        normalized["bookmakerRows"][k] = [
            {"sourceLabel": r[1], "opening": r[2:5], "latest": [clean(x) for x in r[5:8]], "latestProbability": r[9:12] if k == "euro" else r[9:11], "latestKelly": r[12:15] if k == "euro" else r[11:13], "latestReturnRatio": r[15] if k == "euro" else r[13]}
            for r in rows(captures[k])
        ]
    # Retain raw published history tables. Some table rows are hidden by the
    # site's 10/20/50 selector; visible text is the evidence for the default 10.
    normalized["historyEvidence"] = {"visibleText": captures["history"]["text"], "rawTables": captures["history"]["tables"], "visibleRecentHome": 10, "visibleRecentAway": 10, "visibleHeadToHead": 10, "futureHome": 4, "futureAway": 1}
    (p / "normalized-match.json").write_text(json.dumps(normalized, ensure_ascii=False, indent=2), encoding="utf-8")

    defs = structs(ROOT / "go_crawler/models.go")
    defs.update(structs(ROOT / "go_server/handlers/analysis_weight.go"))
    inventory = []

    def add(endpoint, field, kind, status, evidence, difference):
        inventory.append(dict(endpoint=endpoint, vipc_field=field, type=kind, status=status, evidence=evidence, difference=difference))

    # Enumerate every declared leaf in the 1-6 endpoint contracts, rather than
    # declaring entire endpoints covered just because a page title exists.
    configs = [(1, "MatchListResponse"), (2, "HistoryResponse"), (3, "RankData"), (4, "OddsEuroResponse"), (5, "OddsPankouResponse"), (6, "sportteryTradeData")]
    for endpoint, struct in configs:
        for field, kind in flatten(defs, struct):
            status, evidence, difference = "未取得/未验收", "", "本次单场样本无可直接映射的证据"
            leaf = field.rsplit(".", 1)[-1]
            if endpoint == 1:
                if leaf in {"home", "guest", "league", "matchTime", "season", "round", "homeRank", "guestRank"}:
                    status, evidence, difference = "页面有值/需转换", "history.dom.json", "联赛别名、时间格式、FIFA排名与联赛排名须区分"
                elif leaf in {"matchId", "homeTeamId", "guestTeamId", "leagueId", "jingcaiId"}:
                    status, evidence, difference = "异源标识/需映射", "fixed-bonus.browser.txt; history.dom.json", "不能把来源ID当VIPC ID；周六003也不是VIPC jingcaiId原值"
                elif leaf in {"homeScore", "guestScore", "homeHalfScore", "guestHalfScore", "homeOtScore", "guestOtScore", "homeOtPenalty", "guestOtPenalty", "homeCorner", "guestCorner"}:
                    difference = "本场未开赛，不能验证比分/角球；未知不能填0冒充实值"
            elif endpoint == 2:
                if field.startswith(("against.list[].", "recent.home.list[].", "recent.guest.list[].", "future.home[].", "future.guest[].")):
                    if leaf in {"matchTime", "home", "guest", "goal", "halfGoal", "league", "letGoal"}:
                        status, evidence, difference = "页面有值/需转换", "history.dom.json", "历史有半全场、让球；未来部分只有日期且未赛比分应留空"
                    elif leaf in {"homeId", "guestId", "matchId", "scheduleId"}:
                        difference = "需从来源链接提取并建立身份映射，本次尚未完成"
                elif ".summary." in field and leaf in {"win", "lose", "draw", "homeWin", "homeLose", "homeDraw", "all", "homeAll"}:
                    status, evidence, difference = "页面汇总/需校验", "history.dom.json", "默认近期10场；汇总样本与VIPC需一致，部分计数需计算"
                elif field.startswith(("leagueStat.", "leagueSummary.", "rank.")):
                    difference = "国际友谊样本不能验收联赛积分、主客场联赛汇总及升降级颜色"
            elif endpoint == 3:
                difference = "国际友谊没有可验收的联赛积分榜；FIFA17/6不能替代rank.list"
            elif endpoint in {4, 5}:
                evidence = "euro.dom.json" if endpoint == 4 else "asian.dom.json; totals.dom.json"
                if leaf in {"odds", "firstOdds", "ratio", "kelly", "pankou", "firstPankou", "returnRatio"}:
                    status, difference = "已取得/需格式转换", "赔率字符串、百分比小数、盘口文本须统一；公司集合与时点不同"
                elif leaf in {"companyId", "companyName"}:
                    status, difference = "来源标签/需身份映射", "当前展示威*/36*/平*等，不能沿用VIPC公司ID"
                elif leaf in {"firstReturnRatio", "firstRatio"}:
                    status, difference = "可计算/非直接原值", "可从初赔计算；精度与四舍五入不能保证等于VIPC"
                elif leaf == "firstKelly":
                    difference = "本次表格只取得最新凯利，不能拿最新凯利当初始凯利"
                elif leaf in {"oddsTrend", "ratioTrend", "riseAndFall"}:
                    status, difference = "部分/口径待定", "赔率箭头有值，但参照上次更新或初赔的口径未验证；概率趋势和汇总未取得"
            elif endpoint == 6:
                if field.startswith("tzbl."):
                    status, evidence, difference = "页面同名语义/需映射", "support-had.dom.json", "投票支持率与概率为带%字符串；心理误差为文本分类；尚无同步VIPC样本确认同口径"
                elif field.startswith("jyykSpf.") and leaf in {"h", "d", "a"}:
                    status, evidence, difference = "奖金已取得", "fixed-bonus.browser.txt:hadList", "支持比例页与最新奖金列表可能不一致"
                elif field.startswith("jyykRqspf.") and leaf in {"h", "d", "a", "goal"}:
                    status, evidence, difference = "奖金/让球已取得", "fixed-bonus.browser.txt:hhadList", "goalLine=+1；需转换为项目goal字段"
                elif leaf in {"hsupportRate", "dsupportRate", "asupportRate"}:
                    evidence, difference = "support-had.dom.json; support-hhad.dom.json", "投票比例已取得，但未证明是VIPC jyyk这组的同口径比例，不直接替换"
                elif leaf in {"hy", "dy", "ay", "hlr", "dlr", "alr"}:
                    difference = "所取公开数据无同口径盈亏/冷热值；不能用投票率计算后冒充唯彩原值"
            add(endpoint, field, kind, status, evidence, difference)

    for field, source in [("spf.odds[].spf3", "hadList[].h"), ("spf.odds[].spf1", "hadList[].d"), ("spf.odds[].spf0", "hadList[].a"), ("spf.odds[].updateTime", "hadList[].updateDate+updateTime")]:
        add(7, field, "quote", "已取得/需重命名", "fixed-bonus.browser.txt:" + source, "官方7次变更；原VIPC同场响应未取得，不做数值等同性断言")
    for old, new in [("sd00", "s00s00"), ("sd11", "s01s01"), ("sd22", "s02s02"), ("sd33", "s03s03")]:
        add(7, "bf." + old, "quote", "已取得/需重命名", "crsList[-1]." + new, "最新值=" + history["crsList"][-1][new])
    for i in range(8):
        add(7, "jq.t" + str(i), "quote", "已取得/需重命名", "ttgList[-1].s" + str(i), "最新值=" + history["ttgList"][-1]["s" + str(i)])
    for field in ("bf.stop", "jq.stop"):
        add(7, field, "bool", "缺少等价状态", "isCancel=0; sectionsNo999=''; singleList", "取消标记和单关标记均不能证明玩法未停售；不能默认false")
    add(7, "data.matchId", "ID", "异源标识/需映射", "oddsHistory.matchId", "请求matchId、返回matchId、足彩网ID与VIPC ID可能是不同体系")
    add(7, "其他未被显式消费的原始报价包字段", "raw JSON", "未逐字段验收", "fixed-bonus.browser.txt", "VIPC原始包整体保存；本表只保证列出代码明确消费的报价字段，不声称原始包完全一致")
    with (p / "field-differences.csv").open("w", encoding="utf-8-sig", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=list(inventory[0]))
        writer.writeheader()
        writer.writerows(inventory)

    counts = {k: len(v) for k, v in normalized["bookmakerRows"].items()}
    report = f"""# 日本—巴西：免费备选源单场实拉核对

本次读取网站实际数据并保存，未修改VIPC、数据库或服务。比赛：2026-11-14 18:15（北京时间），周六003，足彩网4650133，体彩请求ID2042753。

比较基准是项目现有VIPC字段契约，没有拿到VIPC同场同步响应，因此不能声称已完成两个来源的逐值对账。

## 已取得

- 欧赔 {counts['euro']} 条表格记录，含平均及官方；亚盘 {counts['asian']} 条，大小球 {counts['totals']} 条。数量包含来源平均行，不能当实际独立公司数。
- 双方默认可见近期各10场、交锋10场，含日期、球队、全场、半场和盘口资料；未来日本4场、巴西1场。原始DOM表格含隐藏行，不能直接按表格总行数算比赛数。
- 官方JSON：胜平负7条、让球胜平负8条、比分6条、总进球7条、半全场5条变化记录，全部保留在fixed-bonus.browser.txt及normalized-match.json。
- 最新胜平负3.36/3.65/1.76（17:42:16），让球+1，1.77/3.65/3.33（17:42:23）。
- 最新平局比分0:0=24、1:1=8.25、2:2=9.25、3:3=29（17:31:41）。
- 最新0至7+球：24/7.80/4.25/3.55/4.15/6.60/10.50/12.50（17:31:32）。
- 胜平负投票支持31%/25%/44%，人数85543/69212/125480；让球+1投票45%/35%/20%，人数105927/82208/48066。

## 具体差异与剩余缺口

1. 盈亏hy/dy/ay、胜平负冷热hlr/dlr/alr未取得。投票支持率不能冒充真实资金占比，也不能未经核对直接填入jyykSpf/jyykRqspf。
2. 比分及总进球奖金已经取得，上一轮未确认的缺口已缩小。但bf.stop/jq.stop没有等价证据；isCancel=0是比赛取消标记，singleList是单关标记，不能据此默认stop=false。现有总进球代码要求明确stop=false，否则无法使用。
3. 支持页还显示3.20/3.65/1.81、让球1.72/3.70/3.47，奖金JSON已更新到上述新值；支持比例与奖金不能不带时点混用。顺序采集而非原子快照，不把差异全归因为缓存。
4. 同一次胜平负调整，体彩17:42:16、足彩网17:43:51，观测时间差95秒；初次开盘体彩09:40:59、足彩网09:46:51，差352秒。这只是本场样本，不是固定延迟承诺。
5. 只直接取得最新凯利和返还率；初始凯利缺少，初始概率/返还率可由赔率计算但不保证与VIPC原始精度一致。趋势箭头的参照时点及涨跌汇总待核验。平均值的公司集合也不同。
6. 三个比赛ID体系不能混用；来源公司威*/36*/平*等需要独立映射，不能沿用VIPC公司编号。盘口受半/一、2.5/3球需转换并验证正负方向。
7. 友谊赛没有可验收的联赛积分榜。联赛积分、主客场联赛汇总、赛程分页、全量非竞彩赛事均未验证。FIFA17/6不等于联赛积分名次。
8. 当前比赛尚未开赛，比分/半场/加时/点球/角球/进行时间及延期腰斩状态未验收，不能用0补缺。
9. 普通Python HTTP请求4个官方端点均返回567；正常浏览器访问固定奖金JSON成功，支持页正常渲染，直接支持率及比赛头接口仍被拦截。此次是真实获取成功，但尚不代表无人值守HTTP采集稳定可用；没有绕过验证码或安全挑战。

## 文件

- normalized-match.json：本场完整奖金历史、盘口核心值、支持率页面和战绩原始表格。
- field-differences.csv：{len(inventory)}条字段/字段组，按7个VIPC接口标明获得情况和差异。1—6按Go声明逐叶枚举，7按代码显式读取的报价字段枚举；不把未使用的原始包未知字段算作完成。
- *.dom.json：浏览器DOM证据，含URL与采集UTC时间。
- fixed-bonus.browser.txt：官方成功JSON原文。
- http-manifest.json：普通HTTP失败的实测记录；evidence-manifest.json：证据文件SHA256。

本场足以证明多数核心报价有免费公开来源；仍不能给出全站或全字段覆盖百分比。没有接入生产，不需重启程序。
"""
    (p / "comparison.md").write_text(report, encoding="utf-8")
    evidence = [{"file": f.name, "bytes": f.stat().st_size, "sha256": hashlib.sha256(f.read_bytes()).hexdigest()} for f in sorted(p.iterdir()) if f.is_file() and f.name != "evidence-manifest.json"]
    (p / "evidence-manifest.json").write_text(json.dumps(evidence, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"report": str(p / "comparison.md"), "field_rows": len(inventory), "records": counts, "evidence_files": len(evidence)}, ensure_ascii=True))


if __name__ == "__main__":
    main()
