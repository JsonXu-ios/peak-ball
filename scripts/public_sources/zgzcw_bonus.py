"""Pure parsers for the verified ZGZCW CP schedule and bonus responses.

The CP schedule explicitly links its lottery match ID to the FENXI match ID.
These IDs must never be inferred from a Sporttery identifier. SMG history is
published as opening first, followed by recent changes in reverse time order.
The numeric score/total endpoints contain current prices, with no timestamps.
"""
from __future__ import annotations

from datetime import datetime, timedelta, timezone
from decimal import Decimal, InvalidOperation
import math
import re

from bs4 import BeautifulSoup


CHINA = timezone(timedelta(hours=8))
SOURCE = "zgzcw_cp"
SCORE_LABELS = (
    "1:0", "2:0", "2:1", "3:0", "3:1", "3:2", "4:0", "4:1", "4:2",
    "5:0", "5:1", "5:2", "胜其他", "0:0", "1:1", "2:2", "3:3", "平其他",
    "0:1", "0:2", "1:2", "0:3", "1:3", "2:3", "0:4", "1:4", "2:4",
    "0:5", "1:5", "2:5", "负其他",
)
_NUMBER = re.compile(r"[+]?(?:\d+(?:\.\d*)?|\.\d+)\Z")
_STAMP = re.compile(r"\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?\Z")
_LOTTERY = re.compile(r"周[一二三四五六日天]\d{3}\Z")


def _id(value):
    if isinstance(value, bool) or not re.fullmatch(r"[1-9]\d{0,19}", str(value or "")):
        raise ValueError("足彩网 CP 比赛 ID 无效")
    return str(value)


def _date(value):
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
        raise ValueError("足彩网 CP 业务日期无效")
    try:
        return datetime.strptime(value, "%Y-%m-%d").date().isoformat()
    except ValueError:
        raise ValueError("足彩网 CP 业务日期无效") from None


def _stamp(value):
    if not isinstance(value, str) or not _STAMP.fullmatch(value.strip()):
        raise ValueError("足彩网 CP 时间缺失或无效")
    try:
        return datetime.fromisoformat(value.strip()).replace(tzinfo=CHINA)
    except ValueError:
        raise ValueError("足彩网 CP 时间缺失或无效") from None


def _goal(value):
    if isinstance(value, bool) or not re.fullmatch(r"[+-]?\d{1,2}", str(value).strip()):
        raise ValueError("足彩网 CP 让球值缺失或无效")
    return str(int(str(value).strip()))


def _price(value):
    if isinstance(value, bool) or value is None or not _NUMBER.fullmatch(str(value).strip()):
        raise ValueError("足彩网奖金包含无效数字")
    try:
        decimal = Decimal(str(value).strip())
        result = float(decimal)
    except (ValueError, InvalidOperation, OverflowError):
        raise ValueError("足彩网奖金包含无效数字") from None
    if decimal <= 0 or not math.isfinite(result):
        raise ValueError("足彩网奖金必须为正数")
    return result


def normalize_cp_schedule(html, business_date):
    """Read identity links from one CP mini schedule page for the requested date.

    Prices are deliberately not taken from this HTML. The sole purpose of this
    parser is the explicit CP-ID/FENXI-ID link, teams, kickoff, and handicap.
    """
    business_date = _date(business_date)
    if not isinstance(html, str):
        raise ValueError("足彩网 CP 赛程响应不是文本")
    soup = BeautifulSoup(html, "html.parser")
    metadata = soup.select_one("#responseJson")
    issue = re.search(r'\bissue\s*:\s*[\"\'](\d{4}-\d{2}-\d{2})[\"\']', metadata.get_text() if metadata else "")
    if not issue or issue.group(1) != business_date:
        raise ValueError("足彩网 CP 赛程业务日期不匹配")
    selected = soup.select_one("#selectissue option[selected]")
    if selected and selected.get("value") != business_date:
        raise ValueError("足彩网 CP 日期筛选与响应不一致")
    output, cp_ids, fenxi_ids = [], set(), set()
    for row in soup.select('tr[id^="tr_"]'):
        match = re.fullmatch(r"tr_([1-9]\d*)", row.get("id", ""))
        if not match:
            continue  # Expanded score/ad rows have other IDs.
        cp_id = _id(match.group(1))
        links = {_id(node.get("newplayid")) for node in row.select("[newplayid]")}
        if len(links) != 1:
            raise ValueError("足彩网 CP 赛程缺少唯一 FENXI 比赛关联")
        fenxi_id = links.pop()
        if cp_id in cp_ids or fenxi_id in fenxi_ids:
            raise ValueError("足彩网 CP 赛程出现重复比赛关联")
        cp_ids.add(cp_id)
        fenxi_ids.add(fenxi_id)
        home_node, away_node = row.select_one(".wh-4 a"), row.select_one(".wh-6 a")
        home = home_node.get_text(strip=True) if home_node else ""
        away = away_node.get_text(strip=True) if away_node else ""
        if not home or not away or home == away:
            raise ValueError("足彩网 CP 主客队身份不完整")
        lottery = row.get("mn", "").strip()
        if not _LOTTERY.fullmatch(lottery):
            raise ValueError("足彩网 CP 竞彩编号无效")
        kickoff_values = set()
        for node in row.select("[title]"):
            match_time = re.fullmatch(r"比赛时间[：:]\s*(.+)", node.get("title", "").strip())
            if match_time:
                kickoff_values.add(_stamp(match_time.group(1)).isoformat())
        if len(kickoff_values) != 1:
            raise ValueError("足彩网 CP 缺少唯一开赛时间，不能使用停售时间代替")
        handicap = row.select_one(f"#ch_{cp_id}_22 .rq")
        if handicap is None:
            raise ValueError("足彩网 CP 身份页不是含让球的胜平负赛程页")
        goal = _goal(handicap.get_text(strip=True))
        if goal != _goal(row.get("rq")):
            raise ValueError("足彩网 CP 行属性与让球值不一致")
        cutoff = _stamp(row["t"]).isoformat() if row.get("t") else None
        output.append({"source": SOURCE, "sourceMatchId": cp_id, "cpMatchId": cp_id,
                       "zgzcwMatchId": fenxi_id, "businessDate": business_date,
                       "lotteryNumber": lottery, "home": home, "away": away,
                       "kickoff": kickoff_values.pop(), "goal": goal,
                       "cutoffAt": cutoff, "league": row.get("m", "").strip()})
    if not output:
        raise ValueError("足彩网 CP 赛程未返回有效比赛关联")
    return output


def _history(body, goal):
    if not isinstance(body, list) or not body:
        raise ValueError("足彩网奖金历史接口未返回有效记录")
    rows = []
    for index, item in enumerate(body):
        if not isinstance(item, (list, tuple)) or len(item) != 4:
            raise ValueError("足彩网奖金历史记录须包含三项赔率和真实时间")
        odds = [_price(value) for value in item[:3]]
        stamp = _stamp(item[3])
        rows.append({"h": odds[0], "d": odds[1], "a": odds[2], "goalLine": goal,
                     "updateDate": stamp.date().isoformat(), "updateTime": stamp.strftime("%H:%M:%S"),
                     "sourceIndex": index, "isSourceOpening": index == 0})
    opening = rows[0]
    rows.sort(key=lambda row: (row["updateDate"], row["updateTime"], row["sourceIndex"]))
    latest = rows[-1]
    return rows, {"opening": [opening[key] for key in "hda"],
                  "latest": [latest[key] for key in "hda"], "goal": goal,
                  "openingAt": opening["updateDate"] + " " + opening["updateTime"],
                  "updatedAt": latest["updateDate"] + " " + latest["updateTime"],
                  "historyCoverage": "opening_and_recent_changes"}


def _vector(body, size):
    values = body.split() if isinstance(body, str) else body
    if not isinstance(values, (list, tuple)) or len(values) != size:
        raise ValueError(f"足彩网奖金接口须返回完整 {size} 项")
    return [_price(value) for value in values]


def normalize_cp_bonus(identity, spf_history=None, rqspf_history=None, score_sp=None, total_sp=None):
    """Normalize all four real CP responses into the independent bonus contract.

    Missing or invalid parts fail instead of replacing a complete prior bundle.
    Score and total prices remain current-only; their opening/time is unknown.
    """
    if not isinstance(identity, dict):
        raise ValueError("足彩网 CP 奖金缺少已验证身份")
    cp_id = _id(identity.get("sourceMatchId"))
    if identity.get("cpMatchId") is not None and _id(identity["cpMatchId"]) != cp_id:
        raise ValueError("足彩网 CP 奖金身份 ID 不一致")
    fenxi_id = _id(identity.get("zgzcwMatchId"))
    if not identity.get("home") or not identity.get("away") or not identity.get("kickoff"):
        raise ValueError("足彩网 CP 奖金主客队或开赛时间缺失")
    goal = _goal(identity.get("goal"))
    had, spf = _history(spf_history, "0")
    hhad, rqspf = _history(rqspf_history, goal)
    scores, totals = _vector(score_sp, 31), _vector(total_sp, 8)
    score_odds = dict(zip(SCORE_LABELS, scores))
    crs = {f"s{int(label.split(':')[0]):02d}s{int(label.split(':')[1]):02d}": price
           for label, price in score_odds.items() if ":" in label}
    crs["scoreOdds"] = dict(score_odds)
    result = {"source": SOURCE, "sourceMatchId": cp_id, "cpMatchId": cp_id,
              "zgzcwMatchId": fenxi_id, "home": identity["home"], "away": identity["away"],
              "kickoff": identity["kickoff"], "businessDate": identity.get("businessDate"),
              "lotteryNumber": identity.get("lotteryNumber"), "goal": goal,
              "isCancel": None, "spf": spf, "rqspf": rqspf, "scoreOdds": score_odds,
              "drawScores": {label: score_odds[label] for label in ("0:0", "1:1", "2:2", "3:3")},
              "totalGoals": totals,
              "history": {"hadList": had, "hhadList": hhad, "crsList": [crs],
                          "ttgList": [{f"s{i}": price for i, price in enumerate(totals)}], "hafuList": []},
              "coverage": {"spf": "opening_and_recent_changes", "rqspf": "opening_and_recent_changes",
                           "scores": "latest_only", "totalGoals": "latest_only"}}
    return result
