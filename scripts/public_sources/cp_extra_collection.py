"""Collect CP batch quote fields and join only a verified existing company.

No function falls back to HTML. CP company and match IDs remain inside the
``cpExtras`` evidence record instead of replacing FENXI/PLZX identifiers.
"""
from __future__ import annotations

from copy import deepcopy
from datetime import datetime

from quote_identity import same_prices
from zgzcw_extras import KINDS, normalize_cp_catalog, normalize_cp_extras, verified_cp_catalog


ENDPOINT = "https://cp.zgzcw.com/ssc/lottery/jcsp/getjcspbydate.jsp"
CATALOG_ENDPOINT = "https://cp.zgzcw.com/ssc/lottery/jcsp/getjcspcompany.jsp"
FIELDS = ("kelly", "probabilities", "returnRatio")
_KIND_FIELD = {"kelly": "kelly", "probability": "probabilities", "return": "returnRatio"}
MODEL_COMPANIES = ("0", "9", "2")
MODEL_KINDS = ("europe", "kelly")


def required_cp_catalog():
    """Only the verified CP inputs consumed by the model are requested."""
    verified = verified_cp_catalog()
    return {cid: {"label": verified[cid]["label"], "kinds": list(MODEL_KINDS)}
            for cid in MODEL_COMPANIES}


def _company_key(label):
    # Share the existing, explicit spelling aliases; never infer companies
    # from IDs or fuzzy/partial names across providers.
    from pipeline import _company_key as key
    return key(label.strip()) if isinstance(label, str) else ""


def _companies(catalog=None):
    companies = (catalog or {}).get("companies", required_cp_catalog())
    result = {}
    for cid, entry in companies.items():
        key = _company_key(entry["label"])
        if not key or key in result:
            raise ValueError("CP 公司目录的名称或明确别名不唯一")
        result[key] = (cid, entry["label"], tuple(entry["kinds"]))
    return result


def owned_fields(label, catalog=None):
    """Known API fields stay owned even when their request fails.

    Published CP companies own only their advertised kinds and opening prices;
    the official HAD opening comes from the separate CP bonus-history API.
    """
    key = _company_key(label)
    company = _companies(catalog).get(key)
    if company:
        return tuple(field for kind, field in _KIND_FIELD.items() if kind in company[2]) + ("opening",)
    return ("opening",) if key == "官方(胜平负)" else ()


def fetch_extras(client, identities, date, on_error, heartbeat):
    """Return ``(extras_by_cp_id, raw_receipts, discovered_catalog)``.

    Discover companies once per task, then request the average/William/Bet365
    euro and Kelly contracts only when advertised. Other published fields
    remain in the raw catalog; they are not requested or counted as missing.
    Each transport or invalid-response
    error calls ``on_error(stage, exception)``; an optional field's failure
    does not discard that company's other fields. An absent/invalid europe
    response prevents all joins for that company. No browser/HTML fallback is
    performed here. The client owns request pacing and API transport.
    """
    from pipeline import request_api

    if not isinstance(date, str) or datetime.strptime(date, "%Y-%m-%d").strftime("%Y-%m-%d") != date:
        raise ValueError("CP 附加接口日期无效")
    # Validate one-to-one CP/FENXI identity links before making requests.
    normalize_cp_extras({"europe": []}, identities, "0")
    result = {str(identity["sourceMatchId"]): [] for identity in identities}
    receipts = []
    if not identities:
        return result, receipts, None

    heartbeat()
    try:
        receipt = request_api(client, CATALOG_ENDPOINT, {})
        receipts.append(receipt)
        catalog = {"source": "current_api", "companies": normalize_cp_catalog(receipt["response"])}
        _companies(catalog)  # Known spelling aliases must also remain unique.
    except Exception as exc:
        warning = "公司目录请求或校验失败，沿用已核验静态目录；不代表本次目录确认"
        on_error("足彩网 CP公司目录", ValueError(warning + "：" + str(exc)))
        catalog = {"source": "verified_static_fallback", "warning": warning, "companies": verified_cp_catalog()}

    advertised = catalog["companies"]
    catalog["advertisedCompanies"] = deepcopy(advertised)
    catalog["companies"] = {}
    catalog["collectionScope"] = "model_required"
    for cid, required in required_cp_catalog().items():
        current = advertised.get(cid)
        if current is None or _company_key(current["label"]) != _company_key(required["label"]):
            on_error("足彩网 CP所需公司:" + required["label"], ValueError("当前目录未确认该模型所需公司，未按其他公司替代"))
            continue
        kinds = [kind for kind in MODEL_KINDS if kind in current["kinds"]]
        missing = [kind for kind in MODEL_KINDS if kind not in kinds]
        if missing:
            on_error("足彩网 CP所需字段:" + required["label"], ValueError("当前目录未公布模型所需类型：" + ", ".join(missing)))
        catalog["companies"][cid] = {"label": current["label"], "kinds": kinds}

    for cid, contract in catalog["companies"].items():
        label, kinds = contract["label"], contract["kinds"]
        payloads, received = {}, {}
        for kind in kinds:
            heartbeat()
            try:
                receipt = request_api(client, ENDPOINT, {"cid": cid, "sptype": kind, "date": date})
                receipts.append(receipt)
                payloads[kind] = receipt["response"]
                received[kind] = receipt["receivedAt"]
            except Exception as exc:
                on_error(f"足彩网 CP附加:{label}:{kind}", exc)
        if "europe" not in payloads:
            continue
        try:
            company = normalize_cp_extras({"europe": payloads["europe"]}, identities, cid, label, kinds)
        except Exception as exc:
            on_error(f"足彩网 CP附加:{label}:europe解析", exc)
            continue
        missing = [cp_id for cp_id in result if cp_id not in company]
        if missing:
            on_error(f"足彩网 CP附加:{label}:europe缺项", ValueError(f"配对欧赔接口未返回 {len(missing)} 场已验证比赛"))
        for kind, field in _KIND_FIELD.items():
            if kind not in payloads:
                continue
            try:
                partial = normalize_cp_extras({"europe": payloads["europe"], kind: payloads[kind]}, identities, cid, label, kinds)
            except Exception as exc:
                on_error(f"足彩网 CP附加:{label}:{kind}解析", exc)
                continue
            for cp_id, quote in company.items():
                candidate = partial[cp_id]
                if candidate[field] is not None:
                    quote[field] = deepcopy(candidate[field])
                    quote["rawFields"][kind] = deepcopy(candidate["rawFields"][kind])
                    quote["missingKinds"].remove(kind)
        for cp_id, quote in company.items():
            quote["quoteReceivedAt"] = received["europe"]
            quote["kellyReceivedAt"] = received.get("kelly") if quote.get("kelly") is not None else None
            stamps = [stamp for stamp in (quote["quoteReceivedAt"], quote["kellyReceivedAt"]) if stamp]
            quote["receivedAt"] = max(stamps, key=datetime.fromisoformat)
            quote["catalogSource"] = catalog["source"]
            if catalog.get("warning"):
                quote["catalogWarning"] = catalog["warning"]
            result[cp_id].append(quote)
    return result, receipts, catalog


def _warning(quote, evidence, message):
    for target in (quote, evidence):
        warnings = target.setdefault("fieldWarnings", [])
        if message not in warnings:
            warnings.append(message)


def merge_extras(quotes, extras, catalog=None):
    """Return independent quote copies without adding or renaming companies.

    Existing opening/latest prices and populated fields always win. Missing
    opening can come from CP; current extras require an exact current-price
    triple. For the requested CP companies, ``apiOwnedFields`` is set even if their
    request failed or the response lacked a row. ``cpExtras`` contains CP IDs,
    raw fields, missing kinds, ``status``, ``matchedLatest`` and ``filledFields``.
    No sourceCompanyId, sourceMatchId, latest or history on the main quote is
    overwritten. Ambiguous labels or conflicting match IDs cannot join.
    """
    if not isinstance(quotes, list) or not isinstance(extras, list):
        raise ValueError("CP 附加合并需要报价数组")
    result = deepcopy(quotes)
    companies, targets, candidates = _companies(catalog), {}, {}
    for quote in result:
        if not isinstance(quote, dict):
            raise ValueError("CP 附加目标报价必须为对象")
        targets.setdefault(_company_key(quote.get("sourceLabel")), []).append(quote)
    for extra in extras:
        if not isinstance(extra, dict):
            raise ValueError("CP 附加来源报价必须为对象")
        key = _company_key(extra.get("sourceLabel"))
        company = companies.get(key)
        # Request-scoped CP ID and label must agree; FENXI numeric IDs never
        # participate in this lookup.
        if (company and str(extra.get("sourceCompanyId")) == company[0]
                and extra.get("source") == "zgzcw_cp"):
            candidates.setdefault(key, []).append(extra)

    for key, matching in targets.items():
        if key not in companies:
            continue
        cid, label, kinds = companies[key]
        choices = candidates.get(key, [])
        for quote in matching:
            current_owned = quote.get("apiOwnedFields")
            quote["apiOwnedFields"] = list(dict.fromkeys((current_owned if isinstance(current_owned, list) else []) + list(owned_fields(label, catalog))))
            evidence = {"source": "zgzcw_cp", "sourceCompanyId": cid, "sourceLabel": label,
                        "status": "missing", "matchedLatest": False, "filledFields": [], "missingKinds": list(kinds),
                        "availableKinds": list(kinds), "catalogSource": (catalog or {}).get("source", "verified_static")}
            if (catalog or {}).get("warning"):
                evidence["catalogWarning"] = catalog["warning"]
            quote["cpExtras"] = evidence
            if not choices:
                continue
            if len(matching) != 1 or len(choices) != 1:
                evidence["status"] = "ambiguous"
                _warning(quote, evidence, "CP 公司标签或来源记录不唯一，未合并附加字段")
                continue
            candidate = choices[0]
            evidence.update(deepcopy(candidate))
            evidence.update(status="matched", matchedLatest=False, filledFields=[])
            target_match = quote.get("sourceMatchId")
            if target_match and str(target_match) != str(candidate.get("zgzcwMatchId")):
                evidence["status"] = "identity_mismatch"
                _warning(quote, evidence, "CP 与原报价分析比赛 ID 不一致，未合并附加字段")
                continue
            if not quote.get("opening") and candidate.get("opening"):
                quote["opening"] = deepcopy(candidate["opening"])
                evidence["filledFields"].append("opening")
            evidence["matchedLatest"] = same_prices(quote.get("latest"), candidate.get("latest"), "euro")
            if not evidence["matchedLatest"]:
                evidence["status"] = "price_mismatch"
                _warning(quote, evidence, "CP 即时价与原接口即时价不同，未合并 CP 概率、凯利或返还率")
                continue
            for field in FIELDS:
                if field in owned_fields(label, catalog) and not quote.get(field) and candidate.get(field) is not None:
                    quote[field] = deepcopy(candidate[field])
                    evidence["filledFields"].append(field)
    return result
