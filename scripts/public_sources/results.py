"""Isolated result evidence; never changes captured model inputs or caches."""
from __future__ import annotations

from copy import deepcopy
from datetime import datetime, timedelta
import hashlib
import json
import re

from core import CHINA, now
from source_policy import allows_snapshot
from collection_report import safe_error

SOURCE = "zgzcw"
ENDPOINT = "https://plzx.zgzcw.com/odds/oyzs_ajax.action"
VERSION = "public-results-v1"
SCORE_BASIS = "zgzcw-oyzs-jc-regulation-v1: completed -1; verified against extra-time and penalty fixtures"


def unknown(reason="尚未更新赛果", state="unavailable"):
    return {"state": state, "homeScore": None, "awayScore": None,
            "regulationSettled": False, "reason": reason}


def source_id(value):
    if isinstance(value, bool) or not isinstance(value, (str, int)):
        raise ValueError("赛果来源比赛ID无效")
    text = str(value).strip()
    if not re.fullmatch(r"[0-9]{1,20}", text) or int(text) <= 0:
        raise ValueError("赛果来源比赛ID无效")
    return text


def local_time(value):
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}(?::\d{2})?(?:Z|[+-]\d{2}:\d{2})?", value):
        raise ValueError("赛果比赛时间无效")
    parsed = datetime.fromisoformat(value)
    return parsed.replace(tzinfo=CHINA) if parsed.tzinfo is None else parsed.astimezone(CHINA)


def load_identity(db, match):
    """Only the accepted heading binds the external match, never a manual ID."""
    head = None
    for row in db.execute("SELECT * FROM ps_snapshots WHERE match_id=? AND kind='match_head' ORDER BY id DESC", (match["id"],)):
        if allows_snapshot("match_head", row["source"], row["acquisition"]):
            head = json.loads(row["normalized"])
            break
    if not isinstance(head, dict):
        raise ValueError("缺少已核实的本地比赛头，不能更新赛果")
    mid = source_id(head.get("sourceMatchId"))
    home, away = head.get("home"), head.get("away")
    if not all(isinstance(v, str) and v.strip() for v in (home, away)) or home.strip() == away.strip():
        raise ValueError("本地比赛头缺少有效主客队")
    kickoff = local_time(head.get("kickoff"))
    business_day = datetime.strptime(match["business_date"], "%Y-%m-%d").date()
    if kickoff.date() not in (business_day, business_day + timedelta(days=1)):
        raise ValueError("本地比赛头开赛日与竞彩日不匹配")
    number = head.get("lotteryNumber")
    if not isinstance(number, str) or not re.fullmatch(r"周[一二三四五六日]\d{3}", number):
        raise ValueError("本地比赛头缺少竞彩编号")
    if match["lottery_number"] and match["lottery_number"] != number:
        raise ValueError("本地比赛头竞彩编号不一致")
    if not number.startswith("周" + "一二三四五六日"[business_day.weekday()]):
        raise ValueError("本地竞彩编号星期与竞彩日不一致")
    return {"businessDate": match["business_date"], "sourceMatchId": mid,
            "home": home.strip(), "away": away.strip(), "lotteryNumber": number,
            "kickoff": kickoff.isoformat()}


def bind_row(row, identity):
    if not isinstance(row, dict) or source_id(row.get("ID")) != identity["sourceMatchId"]:
        raise ValueError("赛果来源比赛ID不匹配")
    if row.get("SOURCE_MATCH_ID") is not None and source_id(row["SOURCE_MATCH_ID"]) != identity["sourceMatchId"]:
        raise ValueError("赛果行的来源比赛ID冲突")
    if not isinstance(row.get("HOST_NAME"), str) or not isinstance(row.get("GUEST_NAME"), str) or row["HOST_NAME"].strip() != identity["home"] or row["GUEST_NAME"].strip() != identity["away"]:
        raise ValueError("赛果主客队与已核实比赛头不一致")
    if row.get("CC_ID") != identity["lotteryNumber"]:
        raise ValueError("赛果竞彩编号不匹配")
    kickoff = local_time(row.get("MATCH_TIME"))
    if kickoff != local_time(identity["kickoff"]):
        raise ValueError("赛果开赛日期或时间与已核实比赛头不一致")
    if row.get("MATCH_TIME_INTEGER") is not None:
        try:
            stamp = float(row["MATCH_TIME_INTEGER"])
        except (TypeError, ValueError):
            raise ValueError("赛果开赛时间戳无效") from None
        if not abs(stamp - kickoff.timestamp()) <= 1:
            raise ValueError("赛果开赛时间戳与显示时间不一致")


def score(value):
    if isinstance(value, bool) or not isinstance(value, (str, int)):
        return None
    text = str(value).strip()
    return int(text) if re.fullmatch(r"\d{1,2}", text) else None


def normalize_result(row, identity, received):
    bind_row(row, identity)
    result = unknown("赛果来源状态尚未核实", "unverified")
    result.update(source=SOURCE, sourceMatchId=identity["sourceMatchId"], receivedAt=received,
                  rawStatus=row.get("MATCH_STATUS"), scoreScope="unknown")
    # This exact OYZS jc endpoint was verified with ID 4345436 (2:2 at 90
    # minutes, 3:2 after extra time) and 4335455 (2:2, penalties 3:2). Never
    # generalize this contract to a source's arbitrary "final score" field.
    status = str(row.get("MATCH_STATUS"))
    if status == "-1":
        result["homeScore"], result["awayScore"] = score(row.get("HOST_GOAL")), score(row.get("GUEST_GOAL"))
        try:
            capture = datetime.fromisoformat(received)
        except (ValueError, TypeError):
            raise ValueError("赛果采集时间无效") from None
        if capture.tzinfo is None or capture < local_time(identity["kickoff"]):
            raise ValueError("来源完赛状态早于本场开赛时间，暂不核对")
        if result["homeScore"] is None or result["awayScore"] is None:
            result["reason"] = "来源完赛比分不完整，暂不核对"
        else:
            result.update(state="finished", regulationSettled=True, scoreScope="regulation", basis=SCORE_BASIS,
                          reason="已核实来源完赛状态及90分钟比分（含补时，不含加时和点球）")
    elif status in {"0", "1", "2", "3", "4"}:
        result.update(state="pending", reason="比赛尚未开始" if status == "0" else "比赛进行中，尚未取得确认完赛状态")
    elif status in {"-10", "-14"}:
        result.update(state="cancelled" if status == "-10" else "postponed", reason="来源显示比赛取消" if status == "-10" else "来源显示比赛推迟")
    elif status in {"-11", "-12", "-13"}:
        result["reason"] = "来源显示待定、腰斩或中断，赛果待核实"
    return result


def confirmed(value):
    return (value.get("state") == "finished" and value.get("regulationSettled") is True
            and value.get("scoreScope") == "regulation"
            and all(type(value.get(k)) is int and 0 <= value[k] < 100 for k in ("homeScore", "awayScore")))


def save_result(db, match_id, job_id, value, raw, identity=None, received=None, url=ENDPOINT):
    received = received or now()
    normalized = deepcopy(value)
    if (normalized.get("regulationSettled") or normalized.get("state") == "finished") and (not confirmed(normalized) or not identity):
        raise ValueError("赛果缺少已确认90分钟比分证据")
    normalized.update(version=VERSION, source=SOURCE, receivedAt=received, url=url)
    if identity:
        normalized["identity"] = deepcopy(identity)
        normalized["sourceMatchId"] = identity["sourceMatchId"]
    text = json.dumps(raw, ensure_ascii=False, allow_nan=False)
    cursor = db.execute("INSERT INTO ps_result_snapshots(match_id,job_id,source,url,received_at,payload,normalized,sha256) VALUES(?,?,?,?,?,?,?,?)",
                        (match_id, job_id, SOURCE, url, received, text, json.dumps(normalized, ensure_ascii=False, allow_nan=False), hashlib.sha256(text.encode()).hexdigest()))
    db.commit()
    return cursor.lastrowid


def current_result(db, match_id):
    if not db.execute("SELECT 1 FROM sqlite_master WHERE type='table' AND name='ps_result_snapshots'").fetchone():
        return unknown()
    records = []
    for row in db.execute("SELECT id,normalized FROM ps_result_snapshots WHERE match_id=? ORDER BY id DESC", (match_id,)):
        value = json.loads(row["normalized"])
        if value.get("version") == VERSION:
            value["snapshotId"] = row["id"]
            records.append(value)
    if not records:
        return unknown()
    latest = records[0]
    prior = next((r for r in records if confirmed(r)), None)
    if prior and not confirmed(latest):
        conflict = next((r for r in records if r["snapshotId"] > prior["snapshotId"] and r.get("state") in {"cancelled", "postponed"}), None)
        if conflict:
            effective = deepcopy(conflict)
            effective.update(state="unverified", homeScore=prior["homeScore"], awayScore=prior["awayScore"], regulationSettled=False,
                             reason="来源取消或延期状态与已确认赛果冲突，待核实；历史比分仅供查看", previousConfirmedSnapshotId=prior["snapshotId"])
        else:
            effective = deepcopy(prior)
            effective["lastAttemptError"] = latest.get("reason", "本次未取得新的有效赛果")
    else:
        effective = deepcopy(latest)
    identity = effective.pop("identity", None)
    if identity:
        match = db.execute("SELECT * FROM ps_matches WHERE id=?", (match_id,)).fetchone()
        try:
            if load_identity(db, match) != identity:
                raise ValueError("赛果证据绑定的比赛头已变化，需重新核实")
        except ValueError as exc:
            effective.update(state="unverified", regulationSettled=False, reason=str(exc))
    elif effective.get("regulationSettled"):
        effective.update(state="unverified", regulationSettled=False, reason="赛果缺少可核对的来源身份绑定")
    effective.pop("version", None)
    return effective


def attach_result(models, result):
    projected = deepcopy(models)
    observation = projected.get("observation") if isinstance(projected, dict) else None
    if isinstance(observation, dict):
        if confirmed(result):
            observation.update(homeScore=result["homeScore"], awayScore=result["awayScore"], settled=True,
                               regulationSettled=True, displayState="已核对90分钟赛果")
        else:
            observation.update(homeScore=-1, awayScore=-1, settled=False,
                               regulationSettled=False, displayState="赛果待核实")
    return projected


def attach_results(matches):
    for match in matches:
        if "models" in match:
            match["models"] = attach_result(match["models"], match.get("result") or unknown())


def raw_result(db, payload):
    value = payload.get("snapshotId")
    if isinstance(value, bool) or not isinstance(value, (str, int)) or not re.fullmatch(r"[0-9]+", str(value)) or int(value) <= 0:
        raise ValueError("赛果快照ID无效")
    row = db.execute("SELECT source,url,received_at,payload,sha256 FROM ps_result_snapshots WHERE id=? AND match_id=?",
                     (int(value), str(payload.get("id", "")))).fetchone()
    if row is None:
        raise ValueError("赛果原始快照不存在")
    return dict(row)


def collect_results(db, job_id, client_factory=None):
    from pipeline import request_api
    if client_factory is None:
        from collector import Client
        client_factory = Client
    job = db.execute("SELECT * FROM ps_jobs WHERE id=? AND status='queued'", (job_id,)).fetchone()
    if job is None or json.loads(job["config"]).get("kind") != "results":
        raise ValueError("赛果任务不存在或已执行")
    config = json.loads(job["config"])
    claim = db.execute("UPDATE ps_jobs SET status='running',started_at=?,heartbeat=? WHERE id=? AND status='queued'", (now(), now(), job_id))
    db.commit()
    if claim.rowcount != 1:
        raise ValueError("赛果任务已经被其他进程领取")
    errors, processed, verified, conflicts, client = [], 0, 0, 0, None
    counts = {"finished": 0, "pending": 0, "cancelled": 0, "postponed": 0}
    sql, args = "SELECT * FROM ps_matches WHERE business_date=?", [config["date"]]
    if config.get("officialId"):
        sql += " AND official_id=?"
        args.append(config["officialId"])
    matches = db.execute(sql + " ORDER BY lottery_number", args).fetchall()
    def error(stage, exc):
        errors.append({"stage": stage, "message": safe_error(exc)})
    def progress_message():
        return (f"赛果更新：检查{processed}场，已确认比分{counts['finished']}场，未完赛{counts['pending']}场，"
                f"取消/延期{counts['cancelled'] + counts['postponed']}场，待核实或失败{len(errors)}项。")
    try:
        if not matches:
            raise ValueError("此日期没有本地比赛，未发起赛果请求")
        identities = {}
        for match in matches:
            try:
                identities[match["id"]] = load_identity(db, match)
            except ValueError as exc:
                error(match["official_id"], exc)
                save_result(db, match["id"], job_id, unknown(str(exc), "unverified"), {"error": str(exc)})
        receipt, rows, request_error = None, [], None
        if identities:
            try:
                client = client_factory()
                receipt = request_api(client, ENDPOINT, {"type": "jc", "issue": config["date"], "date": "", "companys": "2"})
                if not isinstance(receipt["response"], list):
                    raise ValueError("赛果接口未返回比赛数组")
                rows = receipt["response"]
            except Exception as exc:
                request_error = safe_error(exc)
        for match in matches:
            identity = identities.get(match["id"])
            if identity:
                raw = {"receipt": receipt} if receipt else {"error": request_error}
                try:
                    if request_error:
                        raise ValueError(request_error)
                    found = [r for r in rows if isinstance(r, dict) and str(r.get("ID")) == identity["sourceMatchId"]]
                    if len(found) != 1:
                        raise ValueError("本次赛果接口未唯一返回此场比赛")
                    value = normalize_result(found[0], identity, receipt["receivedAt"])
                    previous = current_result(db, match["id"])
                    conflict = value["state"] in {"cancelled", "postponed"} and (confirmed(previous) or previous.get("previousConfirmedSnapshotId"))
                    if conflict:
                        conflicts += 1
                        error(match["official_id"], ValueError("取消或延期状态与已确认赛果冲突，当前赛果待核实"))
                    elif value["state"] in {"unverified", "unavailable"}:
                        error(match["official_id"], ValueError(value["reason"]))
                    else:
                        verified += 1
                        counts[value["state"]] += 1
                    raw["matchedRow"] = found[0]
                except Exception as exc:
                    error(match["official_id"], exc)
                    value = unknown(safe_error(exc))
                save_result(db, match["id"], job_id, value, raw, identity, received=receipt["receivedAt"] if receipt else now())
            processed += 1
            db.execute("UPDATE ps_jobs SET processed=?,heartbeat=?,errors=?,message=? WHERE id=?", (processed, now(), json.dumps(errors, ensure_ascii=False), progress_message(), job_id))
            db.commit()
    except Exception as exc:
        error("赛果任务", exc)
    finally:
        if client is not None:
            try:
                client.close()
            except Exception as exc:
                error("赛果来源会话关闭", exc)
    status = "completed" if processed and not errors else "partial" if verified or conflicts else "failed"
    message = progress_message()
    db.execute("UPDATE ps_jobs SET status=?,processed=?,errors=?,finished_at=?,heartbeat=?,message=? WHERE id=?",
               (status, processed, json.dumps(errors, ensure_ascii=False), now(), now(), message, job_id))
    db.commit()
    return {"id": job_id, "status": status, "processed": processed, "verified": verified, "errors": errors, "message": message}
