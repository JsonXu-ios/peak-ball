#!/usr/bin/env python3
"""JSON stdin/stdout bridge for the independent admin module."""
from __future__ import annotations

import argparse
import json
import re
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

from core import connect, detail, now, normalize_bonus, overview, save_match, snapshot, vote_dom, bookmaker_rows


def validate_config(payload):
    date = payload.get("date", "")
    if not isinstance(date, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}", date):
        raise ValueError("日期格式应为YYYY-MM-DD")
    datetime.strptime(date, "%Y-%m-%d")
    limit = payload.get("limit", 10)
    if isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= 50:
        raise ValueError("单次比赛上限为1—50")
    config = {"date": date, "limit": limit}
    for key in ("officialId", "zgzcwId"):
        value = payload.get(key)
        if value:
            if not isinstance(value, str) or not re.fullmatch(r"\d{1,20}", value):
                raise ValueError("来源比赛ID必须为数字")
            config[key] = value
    return config


def enqueue(db, payload, results=False):
    config = validate_config(payload)
    if results:
        if payload.get("zgzcwId"):
            raise ValueError("赛果更新只使用已核实的本地比赛映射")
        config = {k: config[k] for k in ("date", "officialId") if k in config}
        config["kind"] = "results"
        sql, params = "SELECT 1 FROM ps_matches WHERE business_date=?", [config["date"]]
        if config.get("officialId"):
            sql += " AND official_id=?"
            params.append(config["officialId"])
        if db.execute(sql, params).fetchone() is None:
            raise ValueError("此日期或指定比赛没有本地采集记录")
    # Serialize starts across concurrent admin requests and restarts.
    db.execute("BEGIN IMMEDIATE")
    stale = (datetime.now(timezone.utc) - timedelta(minutes=35)).isoformat()
    db.execute("UPDATE ps_jobs SET status='interrupted',finished_at=?,message='执行中断，可重试' WHERE status IN ('queued','running') AND COALESCE(heartbeat,created_at)<?", (now(), stale))
    if db.execute("SELECT 1 FROM ps_jobs WHERE status IN ('queued','running')").fetchone():
        db.rollback()
        raise ValueError("已有采集任务执行中，请等待完成")
    cursor = db.execute("INSERT INTO ps_jobs(status,config,created_at,heartbeat) VALUES('queued',?,?,?)", (json.dumps(config), now(), now()))
    db.commit()
    return {"id": cursor.lastrowid, "status": "queued"}


def import_capture(db, folder):
    """Explicit local CLI import; not exposed as an arbitrary admin file reader."""
    folder = Path(folder).resolve()
    raw = json.loads((folder / "fixed-bonus.browser.txt").read_text(encoding="utf-8"))
    identity = json.loads((folder / "normalized-match.json").read_text(encoding="utf-8"))["identity"]
    key = save_match(db, {"date": identity["kickoffChina"][:10], "officialId": identity["sportteryRequestMatchId"], "home": identity["home"], "away": identity["away"], "kickoff": identity["kickoffChina"], "lotteryNumber": identity["lotteryNumber"], "zgzcwId": identity["zgzcwMatchId"]})
    bonus = normalize_bonus(raw)
    if bonus["home"] != identity["home"] or bonus["away"] != identity["away"]:
        raise ValueError("导入样本身份不匹配")
    reference = json.loads((folder / "fixed-bonus.dom.json").read_text(encoding="utf-8"))
    snapshot(db, key, "bonus", "sporttery", reference["url"], raw, bonus, reference["capturedAt"])
    for name, kind in (("support-had", "support_had"), ("support-hhad", "support_hhad")):
        doc = json.loads((folder / (name + ".dom.json")).read_text(encoding="utf-8"))
        text = next(t for t in doc["tables"] if identity["home"] in t and identity["away"] in t)
        vote = vote_dom(text)
        vote["receivedAt"] = doc["capturedAt"]
        snapshot(db, key, kind, "sporttery", doc["url"], doc, vote, doc["capturedAt"])
    for kind in ("euro", "asian", "totals", "history"):
        doc = json.loads((folder / (kind + ".dom.json")).read_text(encoding="utf-8"))
        normalized = {"tables": doc["tables"], "text": doc["text"]} if kind == "history" else bookmaker_rows(doc["tables"], kind)
        # One published official history was captured during the audit. Select
        # its earliest timestamp, not row zero (the source is newest-first).
        if kind == "euro":
            history_file = folder / "official-euro-history.dom.json"
            if history_file.exists():
                history_doc = json.loads(history_file.read_text(encoding="utf-8"))
                history_rows = [r for t in history_doc["tables"] for r in t if len(r) >= 13 and re.match(r"\d{4}-", r[1])]
                if history_rows:
                    first = min(history_rows, key=lambda r: r[1])
                    for item in normalized:
                        if item["sourceLabel"] == "官方(胜平负)":
                            item["openingKelly"] = first[9:12]
                            item["openingKellyTime"] = first[1]
        snapshot(db, key, kind, "zgzcw", doc["url"], doc, normalized, doc["capturedAt"])
    return {"id": key, "imported": True}


def import_repair(db, folder):
    """Import actual, timestamped browser evidence into this database only."""
    from collector import parse_official_page, parse_mapping_index
    folder = Path(folder).resolve()
    index = json.loads((folder / "fenxi-index.json").read_text(encoding="utf-8"))
    rows, documents = [], {}
    for path in sorted(folder.glob("official-*.json")):
        official_id = path.stem.removeprefix("official-")
        existing = db.execute("SELECT * FROM ps_matches WHERE official_id=?", (official_id,)).fetchone()
        if existing is None:
            continue
        document = json.loads(path.read_text(encoding="utf-8"))
        head, forecast = parse_official_page(document["html"], existing["lottery_number"])
        row = {"matchId": official_id, "homeTeamAllName": existing["home"], "awayTeamAllName": existing["away"], "_sourceHome": head["home"], "_sourceAway": head["away"]}
        rows.append((row, None, ""))
        documents[official_id] = (dict(existing), document, head, forecast)
    date = next(iter(documents.values()))[0]["business_date"] if documents else ""
    mappings = parse_mapping_index(index["html"], rows, date)
    errors, imported = [], 0
    for row, _, _ in rows:
        existing, document, head, forecast = documents[row["matchId"]]
        source_id = mappings.get(row["matchId"])
        key = save_match(db, {"date": existing["business_date"], "officialId": existing["official_id"], "home": existing["home"], "away": existing["away"], "kickoff": head["kickoff"], "lotteryNumber": existing["lottery_number"], "zgzcwId": source_id})
        snapshot(db, key, "match_head", "sporttery", document["url"], document["html"], head, document["capturedAt"])
        aliases = {head["home"]: existing["home"], head["away"]: existing["away"]}
        for table in forecast["tables"]:
            for record in table:
                record[3], record[5] = aliases.get(record[3], record[3]), aliases.get(record[5], record[5])
        if any(forecast["tables"]):
            snapshot(db, key, "history", "sporttery", document["url"], document["html"], forecast, document["capturedAt"])
        for kind in ("euro", "asian", "totals", "history"):
            path = folder / f"{source_id}-{kind}.json"
            try:
                capture = json.loads(path.read_text(encoding="utf-8"))
                if kind == "history":
                    if not any(len(t)>2 for t in capture["tables"]):
                        raise ValueError("来源触发验证码，未返回战绩表格")
                    normalized = {"tables": capture["tables"], "text": capture["text"]}
                    for table in normalized["tables"]:
                        for record in table:
                            if len(record)>=7:
                                record[3], record[5] = aliases.get(record[3], record[3]), aliases.get(record[5], record[5])
                else:
                    normalized = bookmaker_rows(capture["tables"], kind)
                snapshot(db, key, kind, "zgzcw", capture["url"], capture["html"], normalized, capture["capturedAt"])
                imported += 1
            except (OSError, ValueError) as exc:
                errors.append({"stage": existing["official_id"]+":"+kind, "message": str(exc) if isinstance(exc,ValueError) else "未取得来源记录"})
    status = "partial" if errors else "completed"
    message = f"已修复{len(rows)}场比赛头与映射；补齐{imported}项盘口/战绩。" + ("验证码拦截项保留缺失。" if errors else "本批所需数据全部取得。")
    db.execute("INSERT INTO ps_jobs(status,config,created_at,started_at,finished_at,heartbeat,processed,errors,message) VALUES(?,?,?,?,?,?,?,?,?)", (status, json.dumps({"date":date,"limit":len(rows),"repair":True}), now(), now(), now(), now(), len(rows), json.dumps(errors,ensure_ascii=False), message))
    db.commit()
    return {"repairedMatches":len(rows),"mapped":len(mappings),"importedParts":imported,"errors":errors}


def dispatch(db, action, payload):
    if action == "overview":
        date = payload.get("date", "")
        if date:
            validate_config({"date": date})
        result = overview(db, date)
        from models import enrich
        enrich(db, result["matches"])
        from results import attach_results
        attach_results(result["matches"])
        return result
    if action == "detail":
        result = detail(db, str(payload.get("id", "")))
        from models import enrich
        enrich(db, [result["match"]])
        result["models"] = result["match"].pop("models")
        from results import attach_result
        result["models"] = attach_result(result["models"], result["result"])
        return result
    if action == "enqueue":
        return enqueue(db, payload)
    if action == "enqueue-results":
        return enqueue(db, payload, results=True)
    if action == "collect":
        job = db.execute("SELECT config FROM ps_jobs WHERE id=?", (int(payload["id"]),)).fetchone()
        if job and json.loads(job["config"]).get("kind") == "results":
            from results import collect_results
            return collect_results(db, int(payload["id"]))
        from collector import collect
        return collect(db, int(payload["id"]))
    if action == "result-raw":
        from results import raw_result
        return raw_result(db, payload)
    if action == "fail-job":
        db.execute("UPDATE ps_jobs SET status='failed',finished_at=?,message='采集进程未正常结束，请检查独立采集运行环境' WHERE id=? AND status IN ('queued','running')", (now(), int(payload["id"])))
        db.commit()
        return {"ok": True}
    if action == "mapping":
        key, source_id = str(payload.get("id", "")), payload.get("zgzcwId", "")
        if not isinstance(source_id, str) or not re.fullmatch(r"\d{1,20}", source_id):
            raise ValueError("足彩网比赛ID必须为数字")
        if not db.execute("SELECT 1 FROM ps_matches WHERE id=?", (key,)).fetchone():
            raise ValueError("比赛不存在")
        db.execute("UPDATE ps_matches SET zgzcw_id=?,updated_at=? WHERE id=?", (source_id, now(), key))
        db.commit()
        return {"ok": True}
    if action == "company-mapping":
        label, company = payload.get("label", ""), payload.get("company", "")
        if not all(isinstance(x, str) and 0 < len(x.strip()) <= 80 for x in (label, company)):
            raise ValueError("请填写有效来源标签和公司名称")
        db.execute("INSERT INTO ps_company_mappings VALUES('zgzcw',?,?,?) ON CONFLICT(source,source_key) DO UPDATE SET canonical_name=excluded.canonical_name,updated_at=excluded.updated_at", (label.strip(), company.strip(), now()))
        db.commit()
        return {"ok": True}
    if action == "raw":
        row = db.execute("SELECT source,url,received_at,source_updated_at,payload,sha256 FROM ps_snapshots WHERE id=? AND match_id=?", (int(payload["snapshotId"]), str(payload.get("id", "")))).fetchone()
        if row is None:
            raise ValueError("原始快照不存在")
        return dict(row)
    raise ValueError("不支持的独立采集操作")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--db", required=True)
    parser.add_argument("--action", required=True)
    parser.add_argument("--capture-dir")
    args = parser.parse_args()
    try:
        with connect(args.db) as db:
            if args.action in ("import-capture", "import-repair"):
                if not args.capture_dir:
                    raise ValueError("缺少--capture-dir")
                result = import_capture(db, args.capture_dir) if args.action == "import-capture" else import_repair(db, args.capture_dir)
            else:
                payload = json.load(sys.stdin)
                if not isinstance(payload, dict):
                    raise ValueError("请求必须为JSON对象")
                result = dispatch(db, args.action, payload)
        print(json.dumps(result, ensure_ascii=True, allow_nan=False))
    except Exception as exc:
        # Structured errors, no traceback/environment secrets in admin output.
        print(json.dumps({"error": str(exc) if isinstance(exc, ValueError) else type(exc).__name__}, ensure_ascii=True))
        sys.exit(1)


if __name__ == "__main__":
    main()
