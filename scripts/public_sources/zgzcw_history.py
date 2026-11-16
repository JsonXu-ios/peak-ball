"""Validate the historical-result fields embedded in 足彩网's bsls response.

The audited matchHis.rip.js filters existing table rows. Its /bsls/oy request
only replaces historical odds; it does not return dates, teams or scores.
This parser is an explicit no-history-API field source, never an API-error
fallback. It performs no HTTP requests or database writes.
"""
from __future__ import annotations

from datetime import datetime, timedelta, timezone
import re
from urllib.parse import urlsplit

from bs4 import BeautifulSoup

CHINA = timezone(timedelta(hours=8))
HISTORY_PAGE_REASON = "已核查比赛历史原始响应及 matchHis.rip.js：日期、主客队和比分直接包含于三张历史表；/bsls/oy 仅补充赔率和盘口，未发现对应战绩 JSON 接口"
_SCORE = re.compile(r"\s*(\d{1,2})\s*[:：]\s*(\d{1,2})\s*")
_DATE = re.compile(r"(\d{2}|\d{4})-(\d{1,2})-(\d{1,2})")
_TEAM_PATH = re.compile(r"/soccer/team/\d+/([1-9]\d*)/?")
_BLOCKED = re.compile(r"安全策略拦截|Access Verification|请求过于频繁|请输入正确的验证码|验证您是真人", re.I)


def _name(value):
    return value.strip() if isinstance(value, str) else ""


def _aliases(value):
    if value is None:
        return set()
    values = [value] if isinstance(value, str) else value
    if not isinstance(values, list) or any(not isinstance(name, str) or not name.strip() for name in values):
        raise ValueError("已验证的战绩球队别名必须是非空字符串或非空字符串列表")
    return {name.strip() for name in values}


def _one(soup, selector, label):
    nodes = soup.select(selector)
    if len(nodes) != 1:
        raise ValueError(label + "缺少唯一有效结构")
    return nodes[0]


def _team(node):
    anchors = node.find_all("a", href=True)
    choices = [(anchor, _TEAM_PATH.fullmatch(urlsplit(anchor["href"]).path)) for anchor in anchors]
    choices = [(anchor, found) for anchor, found in choices if found]
    if len(choices) != 1:
        raise ValueError("战绩球队缺少唯一来源球队ID")
    anchor, found = choices[0]
    name = anchor.get_text(" ", strip=True)
    if not name:
        raise ValueError("战绩球队名称为空")
    return name, found[1]


def _kickoff(value):
    try:
        parsed = value if isinstance(value, datetime) else datetime.fromisoformat(value)
    except (TypeError, ValueError):
        raise ValueError("战绩关联缺少有效开赛时间") from None
    return parsed.replace(tzinfo=CHINA) if parsed.tzinfo is None else parsed.astimezone(CHINA)


def _date(value):
    found = _DATE.fullmatch(value.strip())
    if not found:
        return None
    year, month, day = found.groups()
    try:
        # Match Go's existing two-digit-year convention, without guessing the
        # century from whether an invalid/future result would otherwise fit.
        if len(year) == 2:
            year = str((2000 if int(year) < 69 else 1900) + int(year))
        return datetime(int(year), int(month), int(day), tzinfo=CHINA).date()
    except ValueError:
        return None


def _score(value):
    found = _SCORE.fullmatch(value)
    if not found:
        return None
    home, away = int(found[1]), int(found[2])
    return f"{home}:{away}" if home <= 50 and away <= 50 else None


def _rows(table, group, match_id, cutoff, target_id, other_id, names_by_id, verified_names):
    headers = [node.get_text(" ", strip=True) for node in table.find_all("th")]
    if headers[:7] != ["联赛", "轮次", "时间", "主队", "比分", "客队", "半场"]:
        raise ValueError("战绩表列定义发生变化，未按旧位置猜测数据")
    prefix = {"Home": "home", "Guest": "guest", "His": "his"}[group]
    result, by_id = [], {}
    discarded = {"target": 0, "same_day_or_future": 0, "invalid_date": 0, "invalid_score": 0, "duplicate": 0}
    malformed = 0
    for row in table.find_all("tr"):
        if row.find_parent("table") is not table:
            continue
        cells = row.find_all("td", recursive=False)
        row_id = str(row.get("id") or "")
        found = re.fullmatch(prefix + r"_([1-9]\d*)", row_id)
        if not found:
            # Header/explicit-empty rows have no data ID. A substantive data
            # row with another layout is an error, not an empty-history signal.
            if len(cells) >= 7 or row_id:
                raise ValueError("战绩行来源ID或分组不匹配")
            continue
        historical_id = found[1]
        if historical_id == match_id:
            discarded["target"] += 1
            continue
        if len(cells) < 7:
            raise ValueError("战绩行缺少日期、球队或比分列")
        day = _date(cells[2].get_text(" ", strip=True))
        if day is None:
            discarded["invalid_date"] += 1
            malformed += 1
            continue
        # Date-only history cannot establish completion time on kickoff day.
        if day >= cutoff.date():
            discarded["same_day_or_future"] += 1
            continue
        score = _score(cells[4].get_text(" ", strip=True))
        if score is None:
            discarded["invalid_score"] += 1
            malformed += 1
            continue
        home, home_id = _team(cells[3])
        away, away_id = _team(cells[5])
        if home_id == away_id or target_id not in (home_id, away_id):
            raise ValueError("战绩记录不属于本组已验证球队")
        if group == "His" and {home_id, away_id} != {target_id, other_id}:
            raise ValueError("交锋记录双方球队与目标比赛不一致")
        for name, team_id in ((home, home_id), (away, away_id)):
            if name in verified_names and verified_names[name] != team_id:
                raise ValueError("战绩球队名称与已验证球队ID矛盾")
        home, away = names_by_id.get(home_id, home), names_by_id.get(away_id, away)
        normalized = [cells[0].get_text(" ", strip=True), "", day.isoformat(), home, score, away,
                      _score(cells[6].get_text(" ", strip=True)) or ""]
        # Teams can play two separate friendlies on the same calendar day.
        # Source match IDs establish duplicates; date/team names alone cannot.
        previous = by_id.get(historical_id)
        if previous:
            if previous != normalized:
                raise ValueError("同场历史记录的日期、球队或比分冲突")
            discarded["duplicate"] += 1
            continue
        by_id[historical_id] = normalized
        result.append(normalized)
    if malformed and not result:
        raise ValueError("战绩表没有有效历史记录，未将解析错误当作空战绩")
    result.sort(key=lambda row: row[2], reverse=True)
    return result, discarded


def normalize_zgzcw_history(raw, source_match_id, home, away, kickoff, home_alias=None, away_alias=None):
    """Return three Go-compatible tables using verified 足彩网 page fields.

    ``home``/``away`` are the existing model's names. Optional aliases are a
    string or list of nonempty strings, already verified against this source
    match by the caller. Only these names may identify the header; row team
    IDs then establish membership.
    The target, all same-day/future rows, and invalid dates/scores are excluded.
    Explicit empty head-to-head is allowed; both recent groups empty is not.
    """
    match_id = str(source_match_id)
    if not re.fullmatch(r"[1-9]\d{0,19}", match_id):
        raise ValueError("足彩网战绩比赛ID无效")
    home, away = _name(home), _name(away)
    home_names, away_names = {home} | _aliases(home_alias), {away} | _aliases(away_alias)
    if not home or not away or home_names & away_names:
        raise ValueError("战绩关联需要明确且不同的双方球队")
    cutoff = _kickoff(kickoff)
    if not isinstance(raw, str) or not raw.strip():
        raise ValueError("足彩网战绩原始响应为空")
    soup = BeautifulSoup(raw, "html.parser")
    if _BLOCKED.search(soup.get_text(" ", strip=True)):
        raise ValueError("足彩网战绩来源触发验证，未取得有效历史数据")
    source_home, home_id = _team(_one(soup, ".host-name", "战绩主队"))
    source_away, away_id = _team(_one(soup, ".visit-name", "战绩客队"))
    if source_home not in home_names or source_away not in away_names or home_id == away_id:
        raise ValueError("战绩页面主客队身份与目标比赛不匹配")
    stamp = _one(soup, ".date", "战绩开赛时间").get_text(" ", strip=True)
    found = re.fullmatch(r"比赛时间[：:]\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})", stamp)
    if not found or abs((_kickoff(found[1]) - cutoff).total_seconds()) > 900:
        raise ValueError("战绩页面开赛时间与目标比赛不匹配")
    for group, prefix in (("Home", "home"), ("Guest", "guest")):
        marker = _one(soup, "#b_" + group, "战绩目标比赛")
        if marker.get("value") != f"#{prefix}_{match_id}":
            raise ValueError("战绩页面来源比赛ID不匹配")
    names_by_id = {home_id: home, away_id: away}
    verified_names = {name: home_id for name in home_names}
    verified_names.update({name: away_id for name in away_names})
    tables, discarded = [], {}
    for group, target, other in (("Home", home_id, away_id), ("Guest", away_id, home_id), ("His", home_id, away_id)):
        table = _one(soup, "#match_" + group, "战绩" + group + "表")
        rows, counts = _rows(table, group, match_id, cutoff, target, other, names_by_id, verified_names)
        tables.append(rows)
        discarded[group] = counts
    if not tables[0] and not tables[1]:
        raise ValueError("双方均无有效赛前近期战绩，不能作为成功采集")
    return {"tables": tables, "source": "zgzcw_page_fields", "sourceMatchId": match_id,
            "sourceHome": source_home, "sourceAway": source_away, "sourceKickoff": found[1],
            "discarded": discarded}
