"""Pure normalization of verified CP batch euro/Kelly/probability/return APIs.

Each endpoint is selected by a request-level company ID and sptype. Its rows
carry CP lottery match IDs, which must join an explicitly verified CP schedule.
There are no source timestamps or opening Kelly values in the extras responses.
"""
from __future__ import annotations

from copy import deepcopy
from datetime import datetime
from decimal import Decimal, InvalidOperation
import json
import re

from quote_identity import same_prices


CP_COMPANIES = {"0": "平均值", "7": "澳*", "5": "立*", "9": "威*", "114": "bwi*", "2": "36*"}
KINDS = ("europe", "kelly", "probability", "return")
_NUMBER = re.compile(r"[+]?(?:\d+(?:\.\d*)?|\.\d+)\Z")
_TIME = re.compile(r"\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?\Z")


def verified_cp_catalog():
    """Previously verified contracts, used only when live discovery fails."""
    return {cid: {"label": label, "kinds": list(KINDS)} for cid, label in CP_COMPANIES.items()}


def normalize_cp_catalog(payload):
    """Discover only the four verified euro contracts, never betting_ratio."""
    if not isinstance(payload, dict) or not isinstance(payload.get("europe"), dict) or not payload["europe"]:
        raise ValueError("CP 公司目录缺少有效欧赔公司清单")
    catalog = {}
    for kind in KINDS:
        companies = payload.get(kind, {})
        if not isinstance(companies, dict):
            raise ValueError("CP 公司目录类型结构无效：" + kind)
        labels = set()
        for raw_id, raw_label in companies.items():
            cid = _id(raw_id, company=True)
            if not isinstance(raw_label, str) or not raw_label.strip():
                raise ValueError("CP 公司目录缺少明确公司名称")
            label = raw_label.strip()
            if label.casefold() in labels:
                raise ValueError("CP 公司目录同一类型存在重名公司")
            labels.add(label.casefold())
            if kind == "europe":
                catalog[cid] = {"label": label, "kinds": [kind]}
            elif cid in catalog:
                if catalog[cid]["label"].casefold() != label.casefold():
                    raise ValueError("CP 公司目录同一公司 ID 的名称不一致")
                catalog[cid]["kinds"].append(kind)
    return catalog


def _id(value, company=False):
    pattern = r"(?:0|[1-9]\d{0,19})" if company else r"[1-9]\d{0,19}"
    if isinstance(value, bool) or not re.fullmatch(pattern, str(value or ("0" if value == 0 else ""))):
        raise ValueError("足彩网 CP 附加数据 ID 无效")
    return str(value)


def _number(value, allow_zero=False):
    text = str(value).strip()
    if isinstance(value, bool) or not _NUMBER.fullmatch(text):
        raise ValueError("足彩网 CP 附加数据包含非法数字")
    try:
        number = Decimal(text)
    except InvalidOperation:
        raise ValueError("足彩网 CP 附加数据包含非法数字") from None
    if not number.is_finite() or number < 0 or (not allow_zero and number == 0):
        raise ValueError("足彩网 CP 附加数据数字范围无效")
    return text


def _array(value, label):
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except (ValueError, TypeError):
            raise ValueError(f"足彩网 CP {label}不是合法 JSON 数组") from None
    if not isinstance(value, list):
        raise ValueError(f"足彩网 CP {label}不是数组")
    return value


def _index(rows):
    if not isinstance(rows, list):
        raise ValueError("足彩网 CP 批量附加接口须返回数组")
    result = {}
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError("足彩网 CP 批量附加记录不是对象")
        cp_id = _id(row.get("MATCHID"))
        if cp_id in result:
            raise ValueError("足彩网 CP 批量附加记录存在重复比赛 ID")
        result[cp_id] = row
    return result


def _vector(row, count, kind):
    values = _array(row.get("SP"), kind + ".SP")
    if len(values) != count:
        raise ValueError(f"足彩网 CP {kind}.SP 必须有 {count} 项")
    if kind == "return":
        if values[0] != "" or values[2] != "":
            raise ValueError("足彩网 CP 返还率响应占位结构不一致")
        return _number(values[1])
    normalized = [_number(value, allow_zero=kind == "probability") for value in values]
    if kind == "probability":
        probabilities = [Decimal(value) for value in normalized]
        if any(value > 100 for value in probabilities) or abs(sum(probabilities) - 100) > Decimal("0.05"):
            raise ValueError("足彩网 CP 概率百分比范围或合计无效")
    return normalized


def _history(row):
    if row.get("SP_CHANGE") in (None, ""):
        return []
    records = _array(row["SP_CHANGE"], "europe.SP_CHANGE")
    history = []
    for item in records:
        if not isinstance(item, list) or len(item) != 4:
            raise ValueError("足彩网 CP 欧赔变化记录结构不一致")
        prices = [_number(value) for value in item[:3]]
        stamp = item[3]
        if not isinstance(stamp, str) or not _TIME.fullmatch(stamp):
            raise ValueError("足彩网 CP 欧赔变化时间无效")
        try:
            datetime.fromisoformat(stamp)
        except ValueError:
            raise ValueError("足彩网 CP 欧赔变化时间无效") from None
        history.append({"win": prices[0], "same": prices[1], "lost": prices[2], "wdate": stamp,
                        "rawArray": deepcopy(item)})
    history.sort(key=lambda item: datetime.fromisoformat(item["wdate"]))
    return history


def normalize_cp_extras(payloads, identities, company_id, company_label=None, available_kinds=None):
    """Return CP-ID keyed euro records for one requested company.

    ``payloads`` contains decoded batch JSON arrays keyed by KINDS. The euro
    response is required; missing optional response/row fields remain None and
    appear in missingKinds. Extra match IDs from other dates are ignored.
    An invalid supplied response fails, and no source timestamp is invented.
    """
    cid = _id(company_id, company=True)
    label = company_label if company_label is not None else CP_COMPANIES.get(cid)
    if not isinstance(label, str) or not label.strip():
        raise ValueError("足彩网 CP 附加数据缺少来源公司名称")
    if not isinstance(payloads, dict) or "europe" not in payloads:
        raise ValueError("足彩网 CP 附加数据缺少配对欧赔接口")
    if not isinstance(identities, list):
        raise ValueError("足彩网 CP 附加数据缺少已验证身份列表")
    kinds = tuple(available_kinds) if available_kinds is not None else KINDS
    if not kinds or kinds[0] != "europe" or len(set(kinds)) != len(kinds) or any(kind not in KINDS for kind in kinds):
        raise ValueError("CP 公司目录包含未核验的附加类型")
    indices = {kind: _index(payloads[kind]) if kind in payloads else {} for kind in kinds}
    result, seen_cp, seen_fenxi = {}, set(), set()
    for identity in identities:
        if not isinstance(identity, dict):
            raise ValueError("足彩网 CP 附加身份不是对象")
        cp_id = _id(identity.get("sourceMatchId"))
        fenxi_id = _id(identity.get("zgzcwMatchId"))
        if identity.get("cpMatchId") is not None and _id(identity["cpMatchId"]) != cp_id:
            raise ValueError("足彩网 CP 附加身份 ID 不一致")
        if cp_id in seen_cp or fenxi_id in seen_fenxi:
            raise ValueError("足彩网 CP 附加身份存在重复关联")
        seen_cp.add(cp_id)
        seen_fenxi.add(fenxi_id)
        euro = indices["europe"].get(cp_id)
        if euro is None:
            continue  # The caller can report the missing expected CP ID.
        prices = _vector(euro, 6, "europe")
        history = _history(euro)
        missing = [kind for kind in kinds[1:] if cp_id not in indices[kind]]
        entry = {"source": "zgzcw_cp", "sourceMatchId": cp_id, "cpMatchId": cp_id,
                 "zgzcwMatchId": fenxi_id, "sourceCompanyId": cid, "sourceLabel": label.strip(),
                 "opening": prices[:3], "latest": prices[3:], "kelly": None,
                 "probabilities": None, "returnRatio": None, "updatedAt": None,
                 "quoteHistory": history, "historyScope": "recent", "missingKinds": missing,
                 "availableKinds": list(kinds),
                 "rawFields": {kind: deepcopy(indices[kind][cp_id]) for kind in kinds if cp_id in indices[kind]}}
        for kind, field in (("kelly", "kelly"), ("probability", "probabilities"), ("return", "returnRatio")):
            if kind in indices and cp_id in indices[kind]:
                entry[field] = _vector(indices[kind][cp_id], 3, kind)
        if history:
            last = history[-1]
            if same_prices(entry["latest"], [last[key] for key in ("win", "same", "lost")], "euro"):
                entry["updatedAt"] = last["wdate"]
            else:
                entry["fieldWarnings"] = ["CP 即时欧赔与最新变化记录不同，未把变化时间标为即时价时间"]
        result[cp_id] = entry
    return result
