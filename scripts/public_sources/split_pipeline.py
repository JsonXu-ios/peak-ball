"""Sporttery supplies voting only. All other collected parts come from ZGCW."""
from __future__ import annotations
import json
import re
from copy import deepcopy
from datetime import datetime
from urllib.parse import urlencode, urlsplit

from core import CHINA, now, save_match, snapshot, detail
from collection_report import aggregate_errors, safe_error, summarize_collection
from source_policy import SOURCE_POLICY
from zgzcw_api import match_index, normalize_markets, normalize_euro_changes
from zgzcw_history import normalize_zgzcw_history, HISTORY_PAGE_REASON

CP_SCHEDULE = "https://cp.zgzcw.com/lottery/jchtplayvsForJsp.action"
CP_HISTORY = "https://cp.zgzcw.com/ssc/lottery/jcsp/getjcspbymatchId.jsp"
CP_VALUES = "https://cp.zgzcw.com/ssc/lottery/getJCBDBFSP.jsp"
CP_IDENTITY_REASON = "足彩网奖金接口只返回赔率；竞彩页的比赛ID、newplayid及球队时间用于验证奖金与分析比赛的对应关系"


def verified_aliases(db, previous, vote):
    """Read the selected source match from past evidence, never a mutable ID."""
    if not previous or (previous["home"], previous["away"], previous["lottery_number"]) != (
        vote["homeTeamAllName"], vote["awayTeamAllName"], vote.get("matchNum")
    ):
        return
    for stored in db.execute("SELECT url,payload,normalized FROM ps_snapshots WHERE match_id=? AND source='zgzcw' AND kind IN ('euro','asian','totals') ORDER BY id DESC", (previous["id"],)):
        try:
            quotes, payload = json.loads(stored["normalized"]), json.loads(stored["payload"])
            bound = {str(q["sourceMatchId"]) for q in quotes if q.get("sourceMatchId")}
            path = urlsplit(stored["url"]).path
            found = re.fullmatch(r"/(?:json/)?(\d+)/(?:bjop|ypdb|dxdb)", path)
            if found:
                bound.add(found[1])
            if len(bound) != 1:
                continue
            source_id = next(iter(bound))
            for receipt in payload.get("interfaces", []):
                if urlsplit(receipt["url"]).path != "/odds/oyzs_ajax.action":
                    continue
                for row in receipt["response"]:
                    if str(row["ID"]) != source_id or row["CC_ID"] != vote.get("matchNum"):
                        continue
                    if not previous["kickoff"] or abs((datetime.fromisoformat(row["MATCH_TIME"]) - datetime.fromisoformat(previous["kickoff"])).total_seconds()) > 900:
                        continue
                    vote["_homeAliases"], vote["_awayAliases"] = [row["HOST_NAME"]], [row["GUEST_NAME"]]
                    return
        except (ValueError, TypeError, KeyError):
            continue


def cp_match(identities, identity, vote):
    candidates = [r for r in identities if str(r["zgzcwMatchId"]) == str(identity["ID"]) and r["lotteryNumber"] == vote["matchNum"]]
    if len(candidates) != 1:
        raise ValueError("足彩网竞彩页未找到唯一且编号一致的奖金比赛")
    result = deepcopy(candidates[0])
    stamp = datetime.fromisoformat(result["kickoff"])
    stamp = stamp.replace(tzinfo=CHINA) if stamp.tzinfo is None else stamp.astimezone(CHINA)
    start = datetime.fromisoformat(identity["MATCH_TIME"]).replace(tzinfo=CHINA)
    if abs((stamp - start).total_seconds()) > 900:
        raise ValueError("足彩网竞彩页和指数接口的开赛时间不一致")
    for side, source_key, official_key, short_key in (
        ("home", "HOST_NAME", "homeTeamAllName", "homeTeamAbbName"),
        ("away", "GUEST_NAME", "awayTeamAllName", "awayTeamAbbName"),
    ):
        allowed = {identity[source_key], vote[official_key], vote.get(short_key)}
        allowed.update(vote.get("_homeAliases" if side == "home" else "_awayAliases", []))
        if result[side] not in allowed:
            raise ValueError("足彩网奖金比赛球队身份不一致")
    return result


def cp_bonus(client, identity, business_date):
    from pipeline import request_api
    from zgzcw_bonus import normalize_cp_bonus
    receipts = []
    for cid in ("wsl", "handicap_wsl"):
        receipts.append(request_api(client, CP_HISTORY, {"cid": cid, "matchId": str(identity["cpMatchId"]), "sptype": "smg", "date": business_date}))
    for lottery in ("23", "24"):
        url = CP_VALUES + "?" + urlencode({"lottery": lottery, "matchId": identity["cpMatchId"]})
        # This data endpoint returns whitespace-separated numbers, not HTML.
        raw = client.http(url)
        receipts.append({"url": url, "method": "GET", "form": None, "transport": "http", "responseFormat": "numeric_text", "receivedAt": now(), "response": raw})
    normalized = normalize_cp_bonus(identity, spf_history=receipts[0]["response"], rqspf_history=receipts[1]["response"], score_sp=receipts[2]["response"], total_sp=receipts[3]["response"])
    return normalized, receipts


def cp_opening(quotes, bonus):
    opening = (bonus or {}).get("spf", {}).get("opening", [])
    if len(opening) != 3 or any(v is None or v < 1 for v in opening):
        return False
    for quote in quotes:
        if quote["sourceLabel"] == "官方(胜平负)" and not quote.get("opening"):
            quote["opening"] = [str(v) for v in opening]
            quote["openingSource"] = {"source": "zgzcw_cp", "sourceMatchId": bonus["sourceMatchId"]}
            return True
    return False


def collect_sources(db, job_id, client_factory):
    from pipeline import acquisition, market_index, request_api, merge_euro_api, supplement_quotes
    from zgzcw_bonus import normalize_cp_schedule
    from cp_extra_collection import fetch_extras, merge_extras
    job = db.execute("SELECT * FROM ps_jobs WHERE id=? AND status='queued'", (job_id,)).fetchone()
    if job is None:
        raise ValueError("任务不存在或已执行")
    config = json.loads(job["config"])
    claimed = db.execute("UPDATE ps_jobs SET status='running',started_at=?,heartbeat=? WHERE id=? AND status='queued'", (now(), now(), job_id))
    db.commit()
    if claimed.rowcount != 1:
        raise ValueError("任务已经被其他进程领取")
    first_snapshot = db.execute("SELECT COALESCE(MAX(id),0) FROM ps_snapshots").fetchone()[0]
    errors, keys, processed, client = [], [], 0, None

    def error(stage, exc):
        errors.append({"stage": stage, "message": safe_error(exc)})

    def heartbeat():
        db.execute("UPDATE ps_jobs SET processed=?,errors=?,heartbeat=? WHERE id=?", (processed, json.dumps(aggregate_errors(errors), ensure_ascii=False), now(), job_id))
        db.commit()

    def store(key, kind, source, receipts, normalized, pages=None, received=None):
        pages = pages or []
        meta = acquisition(receipts)
        if pages:
            meta["mode"] = "api_with_page_fields" if receipts else "page"
            meta["pageFields"] = [{k:p[k] for k in ("url", "fields", "reason")} for p in pages]
        snapshot(db, key, kind, source, receipts[0]["url"] if receipts else pages[0]["url"],
                 {"interfaces": receipts, "pageFields": pages}, normalized, received or now(), acquisition=meta)

    try:
        client = client_factory()
        votes = client.votes(config["date"], "HAD", config["limit"])
        if config.get("officialId"):
            votes = [v for v in votes if str(v[0]["matchId"]) == config["officialId"]]
            if not votes:
                raise ValueError("指定比赛不在此竞彩日的支持比例接口中")
        rq_votes = {}
        try:
            rq_votes = {str(r["matchId"]):(r,n,u) for r,n,u in client.votes(config["date"], "HHAD", config["limit"])}
        except Exception as exc:
            error("让球支持比例接口", exc)
        index, index_receipts = [], []
        try:
            index, index_receipts = market_index(client, config["date"])
        except Exception as exc:
            error("足彩网赛程及盘口接口", exc)
        cp_identities, identity_page = [], None
        try:
            url = CP_SCHEDULE + "?" + urlencode({"lotteryId":47, "type":"jcmini", "issue":config["date"]})
            raw = client.document(url)
            cp_identities = normalize_cp_schedule(raw, config["date"])
            identity_page = {"url":url,"receivedAt":now(),"response":raw,"fields":["bonus:matchMapping"],"reason":CP_IDENTITY_REASON}
        except Exception as exc:
            error("足彩网奖金比赛映射", exc)
        extras, extras_receipts, cp_catalog = {}, [], None
        if cp_identities and index_receipts:
            extras, extras_receipts, cp_catalog = fetch_extras(client, cp_identities, config["date"], error, heartbeat)
        for source_vote, vote, vote_url in votes:
            row = deepcopy(source_vote)
            row["businessDate"] = config["date"]
            official_id = str(row["matchId"])
            previous = db.execute("SELECT * FROM ps_matches WHERE business_date=? AND official_id=?", (config["date"],official_id)).fetchone()
            key = save_match(db, {"date":config["date"],"officialId":official_id,"home":row["homeTeamAllName"],"away":row["awayTeamAllName"],"lotteryNumber":row.get("matchNum")})
            keys.append(key)
            heartbeat()
            for kind, value in (("support_had",(source_vote,vote,vote_url)),("support_hhad",rq_votes.get(official_id))):
                if value:
                    raw_vote, normalized, url = value
                    receipt = {"url":url,"method":"GET","transport":normalized.get("transport","http"),"response":raw_vote,"receivedAt":normalized.get("receivedAt",now())}
                    store(key,kind,"sporttery",[receipt],normalized,received=receipt["receivedAt"])
                elif kind == "support_hhad":
                    error(official_id+":让球支持比例缺项", ValueError("本次支持比例接口未返回此比赛；旧快照不算作本次取得"))
            try:
                if not index_receipts:
                    raise ValueError("足彩网赛程接口失败，未改用体彩比赛信息")
                verified_aliases(db,previous,row)
                identity = match_index(index,row,manual_id=config.get("zgzcwId") if config.get("officialId")==official_id else None)
                identity["_officialHome"] = row.get("homeTeamAbbName") or row["homeTeamAllName"]
                identity["_officialAway"] = row.get("awayTeamAbbName") or row["awayTeamAllName"]
                kickoff, source_id = identity["MATCH_TIME"], str(identity["ID"])
                save_match(db,{"date":config["date"],"officialId":official_id,"home":row["homeTeamAllName"],"away":row["awayTeamAllName"],"lotteryNumber":row["matchNum"],"kickoff":kickoff,"zgzcwId":source_id})
                head = {"home":identity["HOST_NAME"],"away":identity["GUEST_NAME"],"kickoff":kickoff,"lotteryNumber":identity["CC_ID"],"sourceMatchId":source_id}
                store(key,"match_head","zgzcw",index_receipts,head)
                markets = normalize_markets(identity)
            except Exception as exc:
                error(official_id+":足彩网比赛信息",exc)
                processed += 1
                heartbeat()
                continue
            bonus, bonus_receipts, cp_identity = None, [], None
            try:
                cp_identity = cp_match(cp_identities,identity,row)
                bonus, bonus_receipts = cp_bonus(client,cp_identity,config["date"])
                store(key,"bonus","zgzcw",bonus_receipts,bonus,[identity_page] if identity_page else [])
            except Exception as exc:
                error(official_id+":足彩网奖金接口",exc)
            try:
                url = f"https://fenxi.zgzcw.com/{source_id}/bsls"
                raw = client.document(url)
                captured = now()
                history = normalize_zgzcw_history(raw,source_id,row["homeTeamAllName"],row["awayTeamAllName"],kickoff,
                                                 [identity["HOST_NAME"],identity["_officialHome"]],
                                                 [identity["GUEST_NAME"],identity["_officialAway"]])
                page = {"url":url,"receivedAt":captured,"response":raw,"fields":["homeRecent","awayRecent","h2h"],"reason":HISTORY_PAGE_REASON}
                store(key,"history","zgzcw",[],history,[page],received=captured)
            except Exception as exc:
                error(official_id+":足彩网战绩",exc)
            for kind in ("euro","asian","totals"):
                heartbeat()
                try:
                    receipts, quotes = index_receipts[:], markets[kind]
                    if kind == "euro":
                        changes = request_api(client,f"https://fenxi.zgzcw.com/json/{source_id}/bjop")
                        quotes = merge_euro_api(normalize_euro_changes(changes["response"]),quotes)
                        receipts = [changes]+receipts
                        if cp_opening(quotes,bonus):
                            receipts.extend(bonus_receipts[:1])
                        paired = extras.get(str(cp_identity["cpMatchId"]), []) if cp_identity else []
                        quotes = merge_extras(quotes, paired, cp_catalog)
                        if cp_identity:
                            receipts.extend(extras_receipts)
                            for quote in quotes:
                                missing = quote.get("cpExtras", {}).get("missingKinds", [])
                                if missing:
                                    error(official_id+":足彩网CP附加:"+quote["sourceLabel"], ValueError("本场接口缺少："+", ".join(missing)+"；未改用页面填充"))
                    if not quotes:
                        raise ValueError("盘口接口未返回有效报价，未改用页面替代")
                    meta, pages = acquisition(receipts), []
                    try:
                        quotes,pages = supplement_quotes(client,identity,kind,quotes,meta,lambda stage,exc:error(official_id+":"+stage,exc),heartbeat,cp_catalog)
                    except Exception as exc:
                        error(official_id+":"+kind+":页面补充字段",exc)
                    if kind == "euro":
                        # A needed company may exist only on the supplement
                        # page. Attach this task's already-verified CP bundle
                        # after discovery, with the same identity/price guards.
                        quotes = [merge_extras([quote], paired, cp_catalog)[0]
                                  if "cpExtras" not in quote else quote for quote in quotes]
                    for quote in quotes:
                        if quote.get("sourceMatchId") not in (None, "", source_id):
                            raise ValueError("报价比赛ID与已核验足彩网比赛不一致")
                        quote["sourceMatchId"] = source_id
                    if kind == "euro" and cp_identity and identity_page:
                        mapping = dict(identity_page, fields=["euro:matchMapping"])
                        pages.append(mapping)
                        meta["pageFields"].append({k:mapping[k] for k in ("url","fields","reason")})
                        meta["mode"] = "api_with_page_fields"
                    snapshot(db,key,kind,"zgzcw",receipts[0]["url"],{"interfaces":receipts,"pageFields":pages},quotes,now(),acquisition=meta)
                except Exception as exc:
                    error(official_id+":"+kind+":接口",exc)
            processed += 1
            heartbeat()
        complete = sum(not detail(db,key)["missing"] for key in keys)
        updated = {r[0] for r in db.execute("SELECT DISTINCT match_id FROM ps_snapshots WHERE id>?",(first_snapshot,))}
        report = summarize_collection(processed=processed,complete=complete,updated=len(updated.intersection(keys)),errors=errors)
        status,errors = report["status"],report["errors"]
        db.execute("UPDATE ps_jobs SET message=? WHERE id=?",(report["message"]+" 体彩仅支持比例及盈亏配对数据；其他数据来自足彩网。",job_id))
    except Exception as exc:
        error("任务",exc)
        status = "failed"
    finally:
        if client is not None:
            client.close()
    errors = aggregate_errors(errors)
    db.execute("UPDATE ps_jobs SET status=?,processed=?,errors=?,finished_at=?,heartbeat=? WHERE id=?",(status,processed,json.dumps(errors,ensure_ascii=False),now(),now(),job_id))
    db.commit()
    return {"id":job_id,"status":status,"processed":processed,"errors":errors}
