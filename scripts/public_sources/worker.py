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


def enqueue(db, payload):
    config = validate_config(payload)
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


def dispatch(db, action, payload):
    if action == "overview":
        date = payload.get("date", "")
        if date:
            validate_config({"date": date})
        result = overview(db, date)
        from models import enrich
        enrich(db, result["matches"])
        return result
    if action == "detail":
        result = detail(db, str(payload.get("id", "")))
        from models import enrich
        enrich(db, [result["match"]])
        result["models"] = result["match"].pop("models")
        return result
    if action == "enqueue":
        return enqueue(db, payload)
    if action == "collect":
        from collector import collect
        return collect(db, int(payload["id"]))
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
            if args.action == "import-capture":
                if not args.capture_dir:
                    raise ValueError("缺少--capture-dir")
                result = import_capture(db, args.capture_dir)
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
