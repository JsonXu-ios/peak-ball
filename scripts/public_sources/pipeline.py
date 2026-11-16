"""API-first collection; HTML can fill only explicitly audited API gaps."""
from __future__ import annotations

from copy import deepcopy
from datetime import datetime
import os
import re
import urllib.parse

from core import now, bookmaker_rows
from zgzcw_api import normalize_market_index
from quote_identity import same_prices
from source_policy import SOURCE_POLICY

INDEX = "https://plzx.zgzcw.com/odds/oyzs_ajax.action"
# OYZS selector plus company 12 from the published BJZS asian selector.
# Some companies return identity only; normalization must not invent prices.
# No batch exceeds the source's ten-company UI limit.
COMPANY_BATCHES = ("2,9,22,7,3,10,5,11,6,15", "14,4,13,16,8,17,136,12")
PAGE_GAPS = {
    "euro": "仅补模型所需公司的缺失报价、非CP公司即时凯利及指定公司的完整历史；平均、威廉、Bet365的CP接口字段失败不改用页面",
    "asian": "仅在所需公司的初盘或即时盘口水位缺失时补充报价，不为未使用的凯利、概率或返还率请求页面",
    "totals": "仅在所需公司的初盘或即时盘口水位缺失时补充报价，不为未使用的凯利、概率或返还率请求页面",
}
MODEL_QUOTE_LABELS = {
    "euro": frozenset({"平均欧赔", "威*", "36*", "平*", "官方(胜平负)"}),
    "asian": frozenset({"平均欧赔", "威*", "36*", "澳*"}),
    "totals": frozenset({"平均欧赔", "威*", "36*", "澳*"}),
}
REQUIRED_QUOTE_COMPANIES = {
    "euro": frozenset({"平均欧赔", "威*", "36*", "平*"}),
    "asian": frozenset({"威*", "36*", "澳*"}),
    "totals": frozenset({"威*", "36*", "澳*"}),
}


def request_api(client, url, form=None):
    response = client.api(url, form=form) if form is not None else client.api(url)
    return {"url": url, "method": "POST" if form is not None else "GET", "form": form,
            "transport": getattr(client, "last_api_transport", "http"), "receivedAt": now(), "response": response}


def acquisition(receipts):
    return {"mode": "api", "sourcePolicy": SOURCE_POLICY,
            "interfaces": [{k: deepcopy(r.get(k)) for k in ("url", "method", "transport", "form", "receivedAt")} for r in receipts],
            "pageFields": []}


def market_index(client, date):
    matches, receipts = {}, []
    for companies in COMPANY_BATCHES:
        receipt = request_api(client, INDEX, {"type": "jc", "issue": date, "date": "", "companys": companies})
        rows = normalize_market_index(receipt["response"])
        receipts.append(receipt)
        for row in rows:
            key = str(row["ID"])
            if key not in matches:
                matches[key] = deepcopy(row)
                continue
            previous = matches[key]
            if any(previous[k] != row[k] for k in ("ID", "HOST_NAME", "GUEST_NAME", "CC_ID", "MATCH_TIME")):
                raise ValueError("欧亚指数分批返回的比赛身份不一致")
            ids = {str(q["SOURCE_COMPANY_ID"]) for q in previous["listOdds"]}
            previous["listOdds"].extend(deepcopy(q) for q in row["listOdds"] if str(q["SOURCE_COMPANY_ID"]) not in ids)
    return list(matches.values()), receipts


def _company_key(label):
    # Published spelling variants. Company IDs remain scoped to their API.
    return {"平均*": "平均欧赔", "平均值": "平均欧赔", "12b*": "12b*t", "18b*": "18b*t", "明*": "*陞", "bwi*": "bw*n"}.get(label.lower(), label.lower())


def merge_euro_api(changes, index_quotes):
    index = {}
    for quote in index_quotes:
        index.setdefault(_company_key(quote["sourceLabel"]), []).append(quote)
    result = deepcopy(changes)
    for quote in result:
        choices = index.get(_company_key(quote["sourceLabel"]), [])
        if len(choices) == 1 and choices[0]["opening"]:
            quote["opening"] = choices[0]["opening"][:]
            quote["openingSource"] = {"source": "zgzcw_plzx", "sourceCompanyId": choices[0]["sourceCompanyId"]}
    known = {_company_key(q["sourceLabel"]) for q in result}
    for quote in index_quotes:
        if _company_key(quote["sourceLabel"]) not in known:
            result.append(deepcopy(quote))
    if not result:
        raise ValueError("欧赔接口没有有效报价")
    return result


def _verify_page(soup, identity, kickoff):
    text = soup.get_text(" ", strip=True)
    names = ((identity["HOST_NAME"], identity.get("_officialHome", "")), (identity["GUEST_NAME"], identity.get("_officialAway", "")))
    if not all(any(name and name in text for name in side) for side in names):
        raise ValueError("补充字段页面球队身份不匹配")
    stamp = re.search(r"比赛时间[：:]\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})", text)
    if not stamp or abs((datetime.fromisoformat(stamp[1]) - datetime.fromisoformat(kickoff)).total_seconds()) > 900:
        raise ValueError("补充字段页面开赛时间不匹配")


def supplement_quotes(client, identity, kind, api_quotes, meta, on_error, heartbeat, cp_catalog=None):
    """Fill audited absent fields; never overwrite any available API prices."""
    from collector import doc_tables
    from cp_extra_collection import owned_fields
    if kind not in PAGE_GAPS or not api_quotes:
        raise ValueError("没有取得有效接口数据，禁止使用页面替代")
    result, evidence = deepcopy(api_quotes), []
    labels = [label.strip() for label in os.environ.get("PUBLIC_SOURCE_KELLY_LABELS", "威*,36*,平*,官方(胜平负)").split(",") if label.strip()]

    def needs_history(quote):
        return (kind == "euro" and ("*" in labels or quote["sourceLabel"] in labels)
                and not all(quote.get(key) for key in ("openingKelly", "openingKellyTime", "openingKellyPrices", "fullQuoteHistory")))

    def required_fields(quote):
        key = _company_key(quote["sourceLabel"])
        if key not in MODEL_QUOTE_LABELS[kind]:
            return ()
        return ("opening", "latest", "kelly") if kind == "euro" else ("opening", "latest")

    def missing_companies():
        present = {_company_key(quote["sourceLabel"]) for quote in result}
        return sorted(set(REQUIRED_QUOTE_COMPANIES.get(kind, ())) - present)

    for quote in result:
        owned = owned_fields(quote["sourceLabel"], cp_catalog) if kind == "euro" else ()
        if owned:
            # Ownership survives a failed API attempt; HTML cannot fill it.
            quote["apiOwnedFields"] = list(dict.fromkeys(list(quote.get("apiOwnedFields", [])) + list(owned)))
    if not missing_companies() and not any(needs_history(quote) or any(
        name not in quote.get("apiOwnedFields", []) and not quote.get(name)
        for name in required_fields(quote)
    ) for quote in result):
        return result, evidence

    url = f"https://fenxi.zgzcw.com/{identity['ID']}/" + {"euro": "bjop", "asian": "ypdb", "totals": "dxdb"}[kind]
    raw = client.document(url)
    soup, tables = doc_tables(raw)
    _verify_page(soup, identity, identity["MATCH_TIME"])
    page_quotes = bookmaker_rows(tables, kind)
    by_label = {}
    for quote in result:
        by_label.setdefault(_company_key(quote["sourceLabel"]), []).append(quote)
    fields, warnings = [], []
    for page_quote in page_quotes:
        if not required_fields(page_quote) and not needs_history(page_quote):
            continue
        choices = by_label.get(_company_key(page_quote["sourceLabel"]), [])
        if len(choices) > 1:
            on_error(kind + ":公司字段", ValueError("公司标签有歧义，未合并页面补充字段"))
            continue
        if choices:
            quote = choices[0]
            for name in ("opening", "latest"):
                if name not in quote.get("apiOwnedFields", []) and not quote.get(name) and page_quote.get(name) is not None:
                    quote[name] = deepcopy(page_quote[name])
                    fields.append(quote["sourceLabel"] + ":" + name)
            extras = [name for name in ("kelly",) if name in required_fields(quote)
                      and name not in quote.get("apiOwnedFields", []) and not quote.get(name) and page_quote.get(name) is not None]
            if extras and same_prices(quote["latest"], page_quote["latest"], kind):
                for name in extras:
                    quote[name] = deepcopy(page_quote[name])
                    fields.append(quote["sourceLabel"] + ":" + name)
            elif extras:
                warning = "接口即时价与页面即时价不同，未合并页面凯利"
                quote.setdefault("fieldWarnings", []).append(warning)
                warnings.append({"company": quote["sourceLabel"], "message": warning, "apiPrices": quote["latest"], "pagePrices": page_quote["latest"], "fields": extras})
        else:
            quote = deepcopy(page_quote)
            quote["source"] = "zgzcw_page_fields"
            # An already-open page may provide a missing core company, but
            # fields outside the required input set remain outside this merge.
            for field in ("probabilities", "kelly", "returnRatio"):
                if field not in required_fields(quote):
                    quote[field] = None
            if kind == "euro":
                quote["apiOwnedFields"] = list(owned_fields(quote["sourceLabel"], cp_catalog))
                for field in quote["apiOwnedFields"]:
                    quote[field] = [] if field in ("opening", "latest") else None
            result.append(quote)
            by_label[_company_key(quote["sourceLabel"])] = [quote]
            fields.append(quote["sourceLabel"] + ":接口公司列表未提供的报价")
    if fields or warnings:
        evidence.append({"url": url, "receivedAt": now(), "fields": fields, "warnings": warnings, "reason": PAGE_GAPS[kind], "response": raw})
    if fields:
        meta["pageFields"].append({"url": url, "fields": fields, "reason": PAGE_GAPS[kind]})
    if warnings:
        meta.setdefault("fieldWarnings", []).extend(warnings)
    if kind == "euro":
        for quote in result:
            if not needs_history(quote):
                continue
            row = next((r for r in soup.select("tr") if len(r.find_all("td", recursive=False)) >= 15 and r.find_all("td", recursive=False)[1].get_text(strip=True) == quote["sourceLabel"]), None)
            link = row.select_one('a[href*="/bjop/zhishu?"]') if row else None
            if not link:
                on_error("初始凯利:" + quote["sourceLabel"], ValueError("来源未提供完整历史入口"))
                continue
            history_url = urllib.parse.urljoin(url, link["href"])
            heartbeat()
            try:
                history_raw = client.document(history_url)
                _, history_tables = doc_tables(history_raw)
                records = [r for table in history_tables for r in table if len(r) >= 13 and re.fullmatch(r"\d{4}-\d{2}-\d{2} \d{2}:\d{2}(?::\d{2})?", r[1])]
                if not records:
                    raise ValueError("完整历史未返回有效凯利，不使用最近10条冒充初始记录")
                first = min(records, key=lambda r: r[1])
                quote["openingKelly"], quote["openingKellyTime"] = first[9:12], first[1]
                quote["openingKellyPrices"] = first[3:6]
                if not same_prices(quote["opening"], first[3:6], "euro"):
                    quote["openingKellyNote"] = "最早历史记录的赔率与接口初赔不同；此凯利对应下列历史赔率"
                quote["fullQuoteHistory"], quote["historyUrl"] = records, history_url
                names = [quote["sourceLabel"] + ":openingKelly", quote["sourceLabel"] + ":openingKellyPrices", quote["sourceLabel"] + ":fullQuoteHistory"]
                why = "欧赔JSON仅提供近期变化且无凯利字段；已核查完整历史页无独立数据请求，仅从完整历史原文补齐最早凯利"
                meta["pageFields"].append({"url": history_url, "fields": names, "reason": why})
                evidence.append({"url": history_url, "receivedAt": now(), "fields": names, "reason": why, "response": history_raw})
            except Exception as exc:
                on_error("初始凯利:" + quote["sourceLabel"], exc)
    if meta["pageFields"]:
        meta["mode"] = "api_with_page_fields"
    if missing_companies():
        on_error(kind + ":所需公司", ValueError("接口及补充页均未提供所需公司：" + "、".join(missing_companies())))
    return result, evidence


def run_collection(db, job_id, client_factory):
    from split_pipeline import collect_sources
    return collect_sources(db, job_id, client_factory)
