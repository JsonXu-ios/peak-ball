"""Normalize the public Sporttery recent-results and head-to-head APIs.

The caller provides the two team names and aliases verified by the same
Sporttery match ID. No fuzzy matching or page fallback takes place here.
"""
from __future__ import annotations

import re
from datetime import date


def _name(value):
    return value.strip() if isinstance(value, str) else ""


def _response(body, label):
    if not isinstance(body, dict) or str(body.get("errorCode")) != "0":
        raise ValueError(label + "接口返回错误，未使用页面替代")
    value = body.get("value")
    if not isinstance(value, dict):
        raise ValueError(label + "接口数据结构不匹配")
    return value


def _score(value):
    if not isinstance(value, str):
        return None
    found = re.fullmatch(r"\s*(\d{1,2})\s*[:：]\s*(\d{1,2})\s*", value)
    if not found:
        return None
    home, away = int(found[1]), int(found[2])
    return f"{home}:{away}" if home <= 50 and away <= 50 else None


def _goal(value):
    if isinstance(value, bool) or not isinstance(value, (str, int)):
        return None
    text = str(value).strip()
    if not re.fullmatch(r"\d{1,2}", text):
        return None
    result = int(text)
    return result if result <= 50 else None


def _full_score(row):
    # An explicitly empty published full-time score must not turn into 0:0
    # through default-valued goal counters on an unplayed match.
    full = _score(row.get("fullCourtGoal"))
    if "fullCourtGoal" in row and full is None:
        return None
    home = _goal(row.get("homeTeamFullCourtGoalCnt"))
    away = _goal(row.get("awayTeamFullCourtGoalCnt"))
    counters = f"{home}:{away}" if home is not None and away is not None else None
    if full is not None and counters is not None and full != counters:
        raise ValueError("战绩接口全场比分与进球数字段不一致")
    return full or counters


def _rows(section, expected, aliases, label, opponent=None, allow_empty_statistics=False):
    if not isinstance(section, dict) or not isinstance(section.get("matchList"), list):
        raise ValueError(label + "缺少明确的比赛列表")
    rows = section["matchList"]
    statistics = section.get("statistics")
    if allow_empty_statistics and not rows and statistics in (None, {}):
        pass
    elif not isinstance(statistics, dict) or aliases.get(_name(statistics.get("teamShortName")), _name(statistics.get("teamShortName"))) != expected:
        raise ValueError(label + "统计球队与同场已验证球队不匹配")
    result = []
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError(label + "比赛记录结构不匹配")
        stamp = row.get("matchDate")
        if not isinstance(stamp, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}", stamp):
            continue
        try:
            date.fromisoformat(stamp)
        except ValueError:
            continue
        score = _full_score(row)
        home_raw, away_raw = _name(row.get("homeTeamShortName")), _name(row.get("awayTeamShortName"))
        if score is None or not home_raw or not away_raw:
            continue
        home, away = aliases.get(home_raw, home_raw), aliases.get(away_raw, away_raw)
        if home == away or expected not in (home, away):
            raise ValueError(label + "比赛记录不属于目标球队")
        if opponent is not None and {home, away} != {expected, opponent}:
            raise ValueError("交锋记录双方球队与同场已验证球队不匹配")
        result.append([_name(row.get("tournamentShortName")), "", stamp,
                       home, score, away, _score(row.get("halfTimeGoal")) or ""])
    if rows and not result:
        raise ValueError(label + "没有有效日期和全场比分，未将解析失败当作空战绩")
    return result


def normalize_official_history(recent, h2h, home, away, home_alias=None, away_alias=None):
    """Return the existing model's three ordered history tables from JSON.

    Explicitly empty lists remain empty. At least one recent-results table
    must contain valid records, and a nonempty list cannot normalize to zero
    records unnoticed. The existing model applies kickoff/date exclusions.
    """
    home, away = _name(home), _name(away)
    if not home or not away or home == away:
        raise ValueError("战绩关联需要明确且不同的双方球队")
    home_names = {home, _name(home_alias)} - {""}
    away_names = {away, _name(away_alias)} - {""}
    if home_names & away_names:
        raise ValueError("同场已验证球队别名冲突")
    aliases = {name: home for name in home_names}
    aliases.update({name: away for name in away_names})
    recent_value = _response(recent, "近期战绩")
    h2h_value = _response(h2h, "历史交锋")
    home_rows = _rows(recent_value.get("home"), home, aliases, "主队近期战绩")
    away_rows = _rows(recent_value.get("away"), away, aliases, "客队近期战绩")
    # An API can explicitly announce no head-to-head records through its
    # emptyFlag. An unexplained {} response is a schema/data error instead.
    if h2h_value == {} and h2h.get("emptyFlag") is True:
        h2h_value = {"matchList": [], "statistics": {}}
    h2h_rows = _rows(h2h_value, home, aliases, "历史交锋", opponent=away, allow_empty_statistics=True)
    if not home_rows and not away_rows:
        raise ValueError("双方近期战绩均为空，未取得模型所需近期记录")
    return {"tables": [home_rows, away_rows, h2h_rows], "source": "sporttery_api"}
