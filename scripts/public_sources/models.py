"""Run the original Go model functions against isolated pre-match snapshots."""
import hashlib
import json
import os
import subprocess
from datetime import datetime
from pathlib import Path

from core import CHINA, now

ROOT = Path(__file__).resolve().parents[2]
MODEL_KINDS = {"bonus", "euro", "asian", "totals", "history"}


def model_path():
    return Path(os.environ.get("PUBLIC_SOURCE_MODEL", str(ROOT / "data" / "public-sources" / ("public-source-model.exe" if os.name == "nt" else "public-source-model"))))


def projection(db, match):
    parts, provenance = {}, {}
    try:
        kickoff = datetime.fromisoformat(match["kickoff"]).replace(tzinfo=CHINA)
    except (TypeError, ValueError):
        return {"match": match, "parts": {}, "provenance": {}}
    # The overview passes a compact match object; discard cached model outputs
    # before serialization to prevent recursive/bloated model input.
    identity = {k: match.get(k) for k in ("id", "business_date", "home", "away", "kickoff", "lottery_number")}
    for row in db.execute("SELECT id,kind,received_at,normalized FROM ps_snapshots WHERE match_id=? ORDER BY id DESC", (match["id"],)):
        kind = row["kind"]
        if kind not in MODEL_KINDS or kind in parts:
            continue
        try:
            captured = datetime.fromisoformat(row["received_at"])
            if captured.tzinfo is None or captured >= kickoff:
                continue
            value = json.loads(row["normalized"])
        except (TypeError, ValueError):
            continue
        parts[kind] = value
        provenance[kind] = {"id": row["id"], "received_at": row["received_at"]}
    mappings = {r["source_key"]: r["canonical_name"] for r in db.execute("SELECT * FROM ps_company_mappings WHERE source='zgzcw'")}
    for kind in ("euro", "asian", "totals"):
        for row in parts.get(kind, []):
            row["company"] = mappings.get(row["sourceLabel"], row["sourceLabel"])
    return {"match": identity, "parts": parts, "provenance": provenance}


def enrich(db, matches):
    """Batch one model process; persist results solely in the independent DB."""
    if not matches:
        return
    executable = model_path()
    try:
        stat = executable.stat()
        engine = f"{stat.st_size}:{stat.st_mtime_ns}"
    except OSError:
        for match in matches:
            match["models"] = {"state": "unavailable", "reason": "独立模型程序尚未构建，请按运行说明构建 public-source-model"}
        return
    pending, pending_matches, hashes = [], [], []
    for match in matches:
        source = projection(db, match)
        digest = hashlib.sha256((engine + json.dumps(source, ensure_ascii=False, sort_keys=True)).encode()).hexdigest()
        cached = db.execute("SELECT result FROM ps_model_results WHERE match_id=? AND fingerprint=?", (match["id"], digest)).fetchone()
        if cached:
            match["models"] = json.loads(cached["result"])
        else:
            pending.append(source)
            pending_matches.append(match)
            hashes.append(digest)
    if not pending:
        return
    try:
        options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
        process = subprocess.run([str(executable.resolve())], input=json.dumps({"matches": pending}, ensure_ascii=True), capture_output=True, text=True, encoding="utf-8", timeout=18, check=True, **options)
        results = json.loads(process.stdout)
        if not isinstance(results, list) or len(results) != len(pending):
            raise ValueError("模型返回数量不一致")
        for match, digest, result in zip(pending_matches, hashes, results):
            match["models"] = result
            db.execute("INSERT INTO ps_model_results(match_id,fingerprint,result,calculated_at) VALUES(?,?,?,?) ON CONFLICT(match_id) DO UPDATE SET fingerprint=excluded.fingerprint,result=excluded.result,calculated_at=excluded.calculated_at", (match["id"], digest, json.dumps(result, ensure_ascii=False), now()))
        db.commit()
    except (OSError, ValueError, subprocess.SubprocessError):
        for match in pending_matches:
            match["models"] = {"state": "unavailable", "reason": "独立模型执行失败，未用其他数据源替代；请检查模型程序"}
