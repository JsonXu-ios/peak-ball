"""Independent public football sources. Never imports the VIPC collectors."""
from __future__ import annotations

import json
import math
import re
import sqlite3
from datetime import datetime, timedelta, timezone
from pathlib import Path

CHINA = timezone(timedelta(hours=8))
VERSION = "public-sources-v1"


def now():
    return datetime.now(timezone.utc).isoformat()


def number(value):
    try:
        result = float(re.sub(r"[%↑↓,]", "", str(value)).strip())
        return result if math.isfinite(result) else None
    except (ValueError, TypeError):
        return None


def timestamp(row):
    return (str(row.get("updateDate", "")) + " " + str(row.get("updateTime", ""))).strip()


def quote_history(history, key):
    return sorted(history.get(key) or [], key=timestamp)


def analyze(odds, support, probabilities=None):
    """Percentages are vote shares; derived values are explicitly not sales data."""
    values = [number(x) for x in odds]
    shares = [number(x) for x in support]
    if len(values) != 3 or len(shares) != 3:
        return []
    inverse = sum(1 / x for x in values) if all(x and x > 1 for x in values) else None
    implied = [100 / x / inverse for x in values] if inverse else [None] * 3
    given = [number(x) for x in probabilities] if probabilities else implied
    if len(given) != 3:
        given = implied
    given = [value if value is not None else implied[i] for i, value in enumerate(given)]
    # Missing values and zero votes are not a valid distribution.
    valid_share = all(x is not None and 0 <= x <= 100 for x in shares) and abs(sum(shares) - 100) <= 1.1
    return [{"outcome": label, "odds": price, "support": share,
             "probability": probability,
             "profitRate": round((1 - share / 100 * price) * 100, 4) if valid_share and price and price > 1 else None,
             "heatDifference": round(share - probability, 4) if valid_share and probability is not None else None}
            for label, price, share, probability in zip(("胜", "平", "负"), values, shares, given)]


def normalize_bonus(body):
    if str(body.get("errorCode")) != "0" or not isinstance(body.get("value"), dict):
        raise ValueError("体彩奖金响应无有效数据")
    value = body["value"]
    raw = value.get("oddsHistory") or {}
    lists = {k: quote_history(raw, k) for k in ("hadList", "hhadList", "crsList", "ttgList", "hafuList")}
    if not any(lists.values()):
        raise ValueError("体彩奖金记录为空，不覆盖已有报价")
    result = {"history": lists, "sourceMatchId": str(raw.get("matchId", "")), "isCancel": value.get("isCancel"),
              "home": raw.get("homeTeamAllName"), "away": raw.get("awayTeamAllName")}
    for key, name in (("hadList", "spf"), ("hhadList", "rqspf")):
        if lists[key]:
            result[name] = {"opening": [number(lists[key][0].get(x)) for x in "hda"],
                            "latest": [number(lists[key][-1].get(x)) for x in "hda"],
                            "goal": lists[key][-1].get("goalLine", ""), "updatedAt": timestamp(lists[key][-1])}
    if lists["crsList"]:
        row = lists["crsList"][-1]
        result["drawScores"] = {k: number(row.get(v)) for k, v in {"0:0": "s00s00", "1:1": "s01s01", "2:2": "s02s02", "3:3": "s03s03"}.items()}
    if lists["ttgList"]:
        result["totalGoals"] = [number(lists["ttgList"][-1].get("s" + str(i))) for i in range(8)]
    return result


def vote_record(row):
    support = [row.get(k) for k in ("hsupportRate", "dsupportRate", "asupportRate")]
    if not any(number(x) is not None for x in support):
        raise ValueError("无有效支持比例")
    return {"support": [number(x) for x in support], "odds": [number(row.get(k)) for k in "hda"],
            "probabilities": [number(row.get(k)) for k in ("hprobability", "dprobability", "aprobability")],
            "errors": [number(row.get(k)) for k in ("herror", "derror", "aerror")], "psychology": row.get("psyError"),
            "counts": [number(row.get(k)) for k in ("win", "draw", "lose")], "goal": row.get("goalLine", ""),
            "source": "sporttery_user_vote", "receivedAt": now(), "sourceUpdatedAt": None}


def vote_dom(text):
    """Fallback for previously captured rendered support pages."""
    pattern = r"(?:让胜|让平|让负|胜|平|负)\s+(\d+)\s+(\d+(?:\.\d+)?%)\s+([\d.]+)\s+(\d+(?:\.\d+)?%)\s+(-?\d+(?:\.\d+)?%)"
    found = re.findall(pattern, text)
    if len(found) != 3:
        raise ValueError("支持比例页面结构不匹配")
    return {"counts": [number(x[0]) for x in found], "support": [number(x[1]) for x in found],
            "odds": [number(x[2]) for x in found], "probabilities": [number(x[3]) for x in found],
            "errors": [number(x[4]) for x in found], "psychology": "有误差" if "有误差" in text else "正常" if "正常" in text else None,
            "source": "sporttery_user_vote", "receivedAt": now(), "sourceUpdatedAt": None}


def bookmaker_rows(tables, market):
    # Ignore advertising and max/min rows. Preserve published labels and IDs;
    # never assume their numeric IDs mean the same company as VIPC.
    result = []
    for table in tables:
        for row in table:
            if len(row) < 15 or not str(row[0]).isdigit() or number(row[2]) is None:
                continue
            result.append({"sourceLabel": row[1], "opening": row[2:5], "latest": [re.sub("[↑↓]", "", x) for x in row[5:8]],
                           "probabilities": row[9:12] if market == "euro" else row[9:11],
                           "kelly": row[12:15] if market == "euro" else row[11:13],
                           "returnRatio": row[15] if market == "euro" and len(row) > 15 else row[13],
                           "source": "zgzcw", "openingKelly": None})
    if not result:
        raise ValueError("盘口表格未取得有效记录")
    return result


def connect(path):
    path = Path(path).resolve()
    path.parent.mkdir(parents=True, exist_ok=True)
    db = sqlite3.connect(path, timeout=20)
    db.row_factory = sqlite3.Row
    db.execute("PRAGMA journal_mode=WAL")
    db.execute("PRAGMA foreign_keys=ON")
    db.executescript("""
      CREATE TABLE IF NOT EXISTS ps_matches (
        id TEXT PRIMARY KEY, business_date TEXT NOT NULL, official_id TEXT NOT NULL,
        home TEXT NOT NULL, away TEXT NOT NULL, kickoff TEXT, lottery_number TEXT,
        zgzcw_id TEXT, updated_at TEXT NOT NULL, UNIQUE(business_date, official_id));
      CREATE TABLE IF NOT EXISTS ps_snapshots (
        id INTEGER PRIMARY KEY, match_id TEXT NOT NULL REFERENCES ps_matches(id), kind TEXT NOT NULL,
        source TEXT NOT NULL, url TEXT NOT NULL, received_at TEXT NOT NULL,
        source_updated_at TEXT, payload TEXT NOT NULL, normalized TEXT NOT NULL, sha256 TEXT NOT NULL);
      CREATE INDEX IF NOT EXISTS ps_snapshot_lookup ON ps_snapshots(match_id,kind,id DESC);
      CREATE TABLE IF NOT EXISTS ps_jobs (
        id INTEGER PRIMARY KEY, status TEXT NOT NULL, config TEXT NOT NULL, created_at TEXT NOT NULL,
        started_at TEXT, finished_at TEXT, heartbeat TEXT, processed INTEGER NOT NULL DEFAULT 0,
        errors TEXT NOT NULL DEFAULT '[]', message TEXT NOT NULL DEFAULT '');
      CREATE TABLE IF NOT EXISTS ps_company_mappings (
        source TEXT NOT NULL, source_key TEXT NOT NULL, canonical_name TEXT NOT NULL,
        updated_at TEXT NOT NULL, PRIMARY KEY(source,source_key));
      CREATE TABLE IF NOT EXISTS ps_model_results (
        match_id TEXT PRIMARY KEY REFERENCES ps_matches(id), fingerprint TEXT NOT NULL,
        result TEXT NOT NULL, calculated_at TEXT NOT NULL);
    """)
    return db


def save_match(db, data):
    key = "sporttery:" + data["date"] + ":" + str(data["officialId"])
    db.execute("""INSERT INTO ps_matches(id,business_date,official_id,home,away,kickoff,lottery_number,zgzcw_id,updated_at)
      VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET home=excluded.home,away=excluded.away,
      kickoff=COALESCE(excluded.kickoff,ps_matches.kickoff),lottery_number=COALESCE(excluded.lottery_number,ps_matches.lottery_number),
      zgzcw_id=COALESCE(excluded.zgzcw_id,ps_matches.zgzcw_id),updated_at=excluded.updated_at""",
      (key, data["date"], str(data["officialId"]), data["home"], data["away"], data.get("kickoff"), data.get("lotteryNumber"), data.get("zgzcwId"), now()))
    db.commit()
    return key


def snapshot(db, key, kind, source, url, raw, normalized, received=None):
    import hashlib
    text = raw if isinstance(raw, str) else json.dumps(raw, ensure_ascii=False)
    db.execute("INSERT INTO ps_snapshots(match_id,kind,source,url,received_at,source_updated_at,payload,normalized,sha256) VALUES(?,?,?,?,?,?,?,?,?)",
               (key, kind, source, url, received or now(), normalized.get("updatedAt") if isinstance(normalized, dict) else None,
                text, json.dumps(normalized, ensure_ascii=False), hashlib.sha256(text.encode()).hexdigest()))
    db.execute("UPDATE ps_matches SET updated_at=? WHERE id=?", (now(), key))
    db.commit()


def detail(db, key, at=None):
    match = db.execute("SELECT * FROM ps_matches WHERE id=?", (key,)).fetchone()
    if match is None:
        raise ValueError("比赛不存在")
    parts, provenance = {}, {}
    for row in db.execute("SELECT id,kind,source,url,received_at,source_updated_at,normalized,sha256 FROM ps_snapshots WHERE match_id=? ORDER BY id DESC", (key,)):
        if row["kind"] in parts:
            continue
        parts[row["kind"]] = json.loads(row["normalized"])
        provenance[row["kind"]] = {k: row[k] for k in ("id", "source", "url", "received_at", "source_updated_at", "sha256")}
    bonus = parts.get("bonus", {})
    analysis = {}
    for kind, market in (("support_had", "spf"), ("support_hhad", "rqspf")):
        vote = parts.get(kind)
        if vote:
            # Calculate using the paired quote published alongside these votes.
            # Do not silently mix its share with a newer unrelated quote.
            analysis[market] = {"basis": "paired_vote_odds", "rows": analyze(vote["odds"], vote["support"], vote.get("probabilities")),
                                "latestQuote": bonus.get(market), "source": "derived_from_user_vote", "receivedAt": provenance[kind]["received_at"]}
    cutoff = None
    if match["kickoff"]:
        try:
            cutoff = datetime.fromisoformat(match["kickoff"]).replace(tzinfo=CHINA)
        except ValueError:
            pass
    clock = at or datetime.now(timezone.utc)
    stopped = clock >= cutoff if cutoff else None
    mappings = {r["source_key"]: r["canonical_name"] for r in db.execute("SELECT * FROM ps_company_mappings WHERE source='zgzcw'")}
    for market in ("euro", "asian", "totals"):
        for item in parts.get(market, []):
            item["company"] = mappings.get(item["sourceLabel"], item["sourceLabel"])
    return {"match": dict(match), "parts": parts, "provenance": provenance, "analysis": analysis,
            "cutoff": {"rule": "kickoff", "time": match["kickoff"], "stopped": stopped},
            "missing": [k for k in ("bonus", "support_had", "support_hhad", "euro", "asian", "totals", "history") if k not in parts], "version": VERSION}


def overview(db, date=""):
    query, params = (" WHERE business_date=?", (date,)) if date else ("", ())
    matches = [dict(r) for r in db.execute("SELECT * FROM ps_matches" + query + " ORDER BY kickoff,lottery_number LIMIT 500", params)]
    for match in matches:
        match["parts"] = [r[0] for r in db.execute("SELECT DISTINCT kind FROM ps_snapshots WHERE match_id=?", (match["id"],))]
    jobs = []
    for row in db.execute("SELECT * FROM ps_jobs ORDER BY id DESC LIMIT 30"):
        job = dict(row)
        job["config"], job["errors"] = json.loads(job["config"]), json.loads(job["errors"])
        if job["status"] == "running" and job["heartbeat"] and datetime.now(timezone.utc) - datetime.fromisoformat(job["heartbeat"]) > timedelta(minutes=35):
            job["status"] = "interrupted"
            job["message"] = "任务已中断，可重新采集"
        jobs.append(job)
    return {"matches": matches, "jobs": jobs, "storage": "独立 SQLite / ps_*", "version": VERSION}
