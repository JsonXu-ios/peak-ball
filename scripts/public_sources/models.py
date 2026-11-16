"""Run the original Go model functions against isolated pre-match snapshots."""
import hashlib
import json
import os
import subprocess
from datetime import datetime
from pathlib import Path

from core import CHINA, now
from source_policy import SOURCE_POLICY, allows_snapshot, effective_match

ROOT = Path(__file__).resolve().parents[2]
MODEL_KINDS = {"bonus", "euro", "asian", "totals", "history"}
MODEL_KIND_NAMES = {"bonus": "竞彩奖金", "euro": "欧赔", "asian": "亚盘", "totals": "大小球", "history": "战绩"}


def model_path():
    return Path(os.environ.get("PUBLIC_SOURCE_MODEL", str(ROOT / "data" / "public-sources" / ("public-source-model.exe" if os.name == "nt" else "public-source-model"))))


def projection(db, match):
    parts, provenance = {}, {}
    rows = [row for row in db.execute("SELECT id,kind,source,received_at,normalized,acquisition FROM ps_snapshots WHERE match_id=? ORDER BY id DESC", (match["id"],))
            if allows_snapshot(row["kind"], row["source"], row["acquisition"])]
    head = next((json.loads(row["normalized"]) for row in rows if row["kind"] == "match_head"), None)
    match = effective_match(match, head)
    # Exclude caller-provided model outputs even when no valid head exists.
    identity = {k: match.get(k) for k in ("id", "business_date", "home", "away", "kickoff", "lottery_number")}
    source_id = head.get("sourceMatchId") if isinstance(head, dict) else None
    identity["zgzcw_id"] = (str(source_id) if not isinstance(source_id, bool) and isinstance(source_id, (str, int))
                             and len(str(source_id)) <= 20 and str(source_id).isascii()
                             and str(source_id).isdigit() and int(source_id) > 0 else None)
    for target, field in (("source_home", "home"), ("source_away", "away")):
        name = head.get(field) if isinstance(head, dict) else None
        identity[target] = name.strip() if isinstance(name, str) and name.strip() else None
    try:
        kickoff = datetime.fromisoformat(match["kickoff"]).replace(tzinfo=CHINA)
    except (TypeError, ValueError):
        return {"match": identity, "parts": {}, "provenance": {}}
    # The overview passes a compact match object; discard cached model outputs
    # before serialization to prevent recursive/bloated model input.
    for row in rows:
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
        provenance[kind] = {"id": row["id"], "received_at": row["received_at"], "source": row["source"], "sourcePolicy": SOURCE_POLICY}
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
        match["kickoff"] = source["match"]["kickoff"]
        missing = [kind for kind in MODEL_KIND_NAMES if kind not in source["parts"]]
        if missing:
            if not match.get("kickoff"):
                reason = "缺少开赛时间，无法确认赛前数据"
            elif not source["provenance"]:
                reason = "本场没有赛前采集快照；开赛后首次补采的数据不能冒充赛前预测"
            else:
                reason = "缺少赛前" + "、".join(MODEL_KIND_NAMES[k] for k in missing) + "，暂不计算；赛后补采不参与赛前预测"
            match["models"] = {"state": "unavailable", "reason": reason, "missingParts": missing, "snapshotIds": {k:v["id"] for k,v in source["provenance"].items()}}
            continue
        digest = hashlib.sha256(("complete-input-v4:" + SOURCE_POLICY + ":" + engine + json.dumps(source, ensure_ascii=False, sort_keys=True)).encode()).hexdigest()
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
        for match, digest, result, source in zip(pending_matches, hashes, results, pending):
            match["models"] = result
            if result.get("state") == "unavailable" and not source["provenance"] and match.get("kickoff"):
                result["reason"] = "本场没有赛前采集快照；开赛后首次补采的数据不能冒充赛前预测"
            db.execute("INSERT INTO ps_model_results(match_id,fingerprint,result,calculated_at) VALUES(?,?,?,?) ON CONFLICT(match_id) DO UPDATE SET fingerprint=excluded.fingerprint,result=excluded.result,calculated_at=excluded.calculated_at", (match["id"], digest, json.dumps(result, ensure_ascii=False), now()))
        db.commit()
    except (OSError, ValueError, subprocess.SubprocessError):
        for match in pending_matches:
            match["models"] = {"state": "unavailable", "reason": "独立模型执行失败，未用其他数据源替代；请检查模型程序"}
