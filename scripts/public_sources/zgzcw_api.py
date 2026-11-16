"""Pure normalization of verified 足彩网 JSON APIs.

The PLZX market index and FENXI change feed use different company ID spaces.
Keep source IDs and published labels; do not map IDs between those endpoints.
No function in this module makes network requests or edits stored data.
"""
from __future__ import annotations

from copy import deepcopy
from datetime import datetime, timedelta, timezone
from decimal import Decimal, InvalidOperation
import re


CHINA = timezone(timedelta(hours=8))
PLZX_SOURCE = "zgzcw_plzx"
FENXI_SOURCE = "zgzcw_fenxi"
_STAMP = re.compile(r"\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?")
_LOTTERY = re.compile(r"周[一二三四五六日天]\d{3}")


def _id(value, *, company=False):
    text = str(value).strip()
    if isinstance(value, bool) or not re.fullmatch(r"-?\d{1,20}" if company else r"\d{1,20}", text):
        raise ValueError("足彩网来源ID无效")
    if not company and int(text) <= 0:
        raise ValueError("足彩网比赛或球队ID必须为正数")
    return str(int(text))


def _name(value):
    if not isinstance(value, str) or not value.strip():
        raise ValueError("足彩网球队或公司名称为空")
    return value.strip()


def _stamp(value):
    if isinstance(value, datetime):
        parsed = value
    elif isinstance(value, str) and _STAMP.fullmatch(value.strip()):
        try:
            parsed = datetime.fromisoformat(value.strip())
        except ValueError:
            raise ValueError("足彩网比赛或报价时间无效") from None
    else:
        raise ValueError("足彩网比赛或报价时间无效")
    return parsed.replace(tzinfo=CHINA) if parsed.tzinfo is None else parsed.astimezone(CHINA)


def _decimal(value):
    if value is None or isinstance(value, bool):
        return None
    try:
        result = Decimal(str(value).strip())
    except (InvalidOperation, ValueError):
        return None
    return result if result.is_finite() else None


def _text_number(value):
    number = _decimal(value)
    if number is None:
        return None
    text = format(number, "f")
    return text.rstrip("0").rstrip(".") if "." in text else text


def _validate_match(row):
    if not isinstance(row, dict):
        raise ValueError("足彩网赛程记录必须为对象")
    mid = _id(row.get("ID"))
    if row.get("SOURCE_MATCH_ID") is not None and _id(row["SOURCE_MATCH_ID"]) != mid:
        raise ValueError("足彩网赛程ID与来源比赛ID不一致")
    for key in ("SOURCE_HOST_ID", "SOURCE_GUEST_ID"):
        if row.get(key) is not None:
            _id(row[key])
    if _name(row.get("HOST_NAME")) == _name(row.get("GUEST_NAME")):
        raise ValueError("足彩网主客队名称相同，无法确认比赛")
    start = _stamp(row.get("MATCH_TIME"))
    if row.get("MATCH_TIME_INTEGER") is not None:
        epoch = _decimal(row["MATCH_TIME_INTEGER"])
        if epoch is None or abs(Decimal(str(start.timestamp())) - epoch) > 1:
            raise ValueError("足彩网开赛日期与时间戳不一致")
    if not isinstance(row.get("CC_ID"), str) or not _LOTTERY.fullmatch(row["CC_ID"].strip()):
        raise ValueError("足彩网竞彩编号无效")
    quotes = row.get("listOdds")
    if not isinstance(quotes, list):
        raise ValueError("足彩网盘口列表无效")
    seen = set()
    for quote in quotes:
        if not isinstance(quote, dict):
            raise ValueError("足彩网盘口记录必须为对象")
        cid = _id(quote.get("SOURCE_COMPANY_ID"), company=True)
        _name(quote.get("COMPANY_NAME"))
        if cid in seen:
            raise ValueError("足彩网同场盘口出现重复公司ID")
        seen.add(cid)
        if quote.get("SOURCE_MATCH_ID") is not None and _id(quote["SOURCE_MATCH_ID"]) != mid:
            raise ValueError("足彩网盘口属于其他比赛，停止关联")
    return mid


def normalize_market_index(payload):
    """Validate and copy index rows, preserving every original source field."""
    if not isinstance(payload, list):
        raise ValueError("足彩网欧亚指数接口必须返回比赛数组")
    seen = set()
    for row in payload:
        mid = _validate_match(row)
        if mid in seen:
            raise ValueError("足彩网赛程接口出现重复比赛ID")
        seen.add(mid)
    return deepcopy(payload)


def _aliases(vote_row, side):
    key, verified, extra = (
        ("homeTeamAllName", "_sourceHome", "_homeAliases") if side == "home"
        else ("awayTeamAllName", "_sourceAway", "_awayAliases")
    )
    names = {_name(vote_row.get(key))}
    # The support endpoint publishes its own abbreviations; obtaining a
    # separate Sporttery match-head response is unnecessary for identity.
    for field in (("homeTeamAbbName", "homeTeamShortName") if side == "home" else ("awayTeamAbbName", "awayTeamShortName")):
        if vote_row.get(field):
            names.add(_name(vote_row[field]))
    if vote_row.get(verified):
        names.add(_name(vote_row[verified]))
    aliases = vote_row.get(extra, [])
    if not isinstance(aliases, (list, tuple)):
        raise ValueError("已验证的球队别名必须为列表")
    names.update(_name(value) for value in aliases)
    return names


def match_index(rows, vote_row, kickoff=None, manual_id=None):
    """Return one identity-checked source row; a manual ID never bypasses checks.

    ``_sourceHome``/``_sourceAway`` and optional ``_homeAliases``/
    ``_awayAliases`` must be aliases verified by the caller for this match.
    Matching is exact and preserves home/away orientation.
    """
    if not isinstance(vote_row, dict) or not isinstance(rows, list):
        raise ValueError("比赛映射输入无效")
    start = _stamp(kickoff) if kickoff is not None else None
    business_date = vote_row.get("businessDate") or vote_row.get("matchDate") or (start.date().isoformat() if start else "")
    try:
        day = datetime.strptime(str(business_date), "%Y-%m-%d").replace(tzinfo=CHINA)
    except ValueError:
        raise ValueError("竞彩日期无效，无法关联赛程") from None
    if start is not None and not day <= start < day + timedelta(days=2):
        raise ValueError("官方开赛时间不属于指定竞彩日或次日")
    number = vote_row.get("matchNum") or vote_row.get("lottery_number")
    if not isinstance(number, str) or not _LOTTERY.fullmatch(number.strip()):
        raise ValueError("官方竞彩编号无效，无法关联赛程")
    home_names, away_names = _aliases(vote_row, "home"), _aliases(vote_row, "away")
    manual = _id(manual_id) if manual_id is not None else None
    candidates = []
    for row in rows:
        mid = _validate_match(row)
        if manual is not None and mid != manual:
            continue
        source_start = _stamp(row["MATCH_TIME"])
        if (
            row["CC_ID"].strip() == number.strip()
            and row["HOST_NAME"].strip() in home_names
            and row["GUEST_NAME"].strip() in away_names
            and day <= source_start < day + timedelta(days=2)
            and (start is None or abs((source_start - start).total_seconds()) <= 15 * 60)
        ):
            candidates.append(row)
    if len(candidates) != 1:
        if manual is not None:
            raise ValueError("手工足彩网ID未通过竞彩编号、双方球队及开赛时间校验")
        raise ValueError("未找到唯一且身份一致的足彩网接口比赛")
    return deepcopy(candidates[0])


def asian_line(value):
    """PLZX negatives mean the home team gives the handicap."""
    number = _decimal(value)
    if number is None or number * 4 != (number * 4).to_integral_value():
        return None
    magnitude = abs(number)
    words = {
        Decimal("0"): "平手", Decimal("0.5"): "半球", Decimal("1"): "一球",
        Decimal("1.5"): "球半", Decimal("2"): "两球", Decimal("2.5"): "两球半",
        Decimal("3"): "三球", Decimal("3.5"): "三球半", Decimal("4"): "四球",
        Decimal("4.5"): "四球半", Decimal("5"): "五球", Decimal("5.5"): "五球半",
        Decimal("6"): "六球", Decimal("6.5"): "六球半",
    }
    if magnitude in words:
        line = words[magnitude]
    else:
        low, high = magnitude - Decimal("0.25"), magnitude + Decimal("0.25")
        if low not in words or high not in words:
            return None
        line = words[low] + "/" + words[high]
    return ("受" if number > 0 else "") + line


def _triple(quote, keys, market):
    values = [_decimal(quote.get(key)) for key in keys]
    if any(value is None for value in values):
        return []
    if market == "euro":
        # The captured Hong Kong entry really publishes 1.00 on a heavy
        # favourite. Preserve that quote; model eligibility is a later rule.
        return [_text_number(value) for value in values] if all(value >= 1 for value in values) else []
    if values[0] <= 0 or values[2] <= 0:
        return []
    if market == "asian":
        line = asian_line(values[1])
    else:
        line = _text_number(values[1]) if values[1] >= 0 and values[1] * 4 == (values[1] * 4).to_integral_value() else None
    return [_text_number(values[0]), line, _text_number(values[2])] if line is not None else []


def normalize_markets(index_match):
    """Convert published opening/current prices; absent extras stay absent.

    A wholly absent market for one company is skipped. A missing phase is an
    empty list, never copied from the other phase or set to artificial zero.
    """
    _validate_match(index_match)
    result = {"euro": [], "asian": [], "totals": []}
    fields = {
        "euro": (("FIRST_WIN", "FIRST_SAME", "FIRST_LOST"), ("WIN", "SAME", "LOST")),
        "asian": (("FIRST_HOST", "FIRST_HANDICAP", "FIRST_GUEST"), ("HOST", "HANDICAP", "GUEST")),
        "totals": (("FIRST_BIG", "DW_FIRST_HANDICAP", "FIRST_SMALL"), ("BIG", "DW_HANDICAP", "SMALL")),
    }
    for quote in index_match["listOdds"]:
        for market, (opening_keys, latest_keys) in fields.items():
            opening = _triple(quote, opening_keys, market)
            latest = _triple(quote, latest_keys, market)
            if not opening and not latest:
                continue
            result[market].append({
                "sourceLabel": quote["COMPANY_NAME"].strip(),
                "sourceCompanyId": _id(quote["SOURCE_COMPANY_ID"], company=True),
                "sourceMatchId": _id(index_match["ID"]),
                "source": PLZX_SOURCE, "opening": opening, "latest": latest,
                "probabilities": None, "kelly": None, "openingKelly": None, "returnRatio": None,
            })
    return result


def normalize_euro_changes(body):
    """Decode recent changes; even the earliest returned row is not an opener.

    ``quoteHistory`` preserves every attribute of each valid price record,
    sorted oldest to newest. Invalid/empty price records cannot become latest.
    """
    if not isinstance(body, dict) or not isinstance(body.get("company5List"), list):
        raise ValueError("足彩网欧赔变化接口结构无效")
    result, seen = [], set()
    for company in body["company5List"]:
        if not isinstance(company, dict) or not isinstance(company.get("attribute"), dict):
            raise ValueError("足彩网欧赔变化公司结构无效")
        info = company["attribute"]
        cid, label = _id(info.get("cid"), company=True), _name(info.get("name"))
        if cid in seen:
            raise ValueError("足彩网欧赔变化接口出现重复公司ID")
        seen.add(cid)
        if not isinstance(company.get("c", []), list):
            raise ValueError("足彩网欧赔变化记录必须为数组")
        history = []
        for item in company.get("c", []):
            if not isinstance(item, dict) or not isinstance(item.get("attribute"), dict):
                continue
            attributes = item["attribute"]
            if attributes.get("cid") is not None and _id(attributes["cid"], company=True) != cid:
                raise ValueError("足彩网变化记录公司ID不匹配")
            prices = _triple(attributes, ("win", "same", "lost"), "euro")
            if not prices:
                continue
            try:
                stamp = _stamp(attributes.get("wdate"))
            except ValueError:
                continue
            history.append((stamp, deepcopy(attributes), prices))
        if not history:
            continue
        history.sort(key=lambda item: item[0])
        _, latest, prices = history[-1]
        ratio = _decimal(latest.get("returnPer"))
        result.append({
            "sourceCompanyId": cid, "sourceLabel": label, "source": FENXI_SOURCE,
            "opening": [], "latest": prices, "updatedAt": latest["wdate"],
            "quoteHistory": [row[1] for row in history], "historyScope": "recent",
            "returnRatio": _text_number(ratio) if ratio is not None and ratio > 0 else None,
            "probabilities": None, "kelly": None, "openingKelly": None,
        })
    return result
