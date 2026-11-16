"""Pure reporting helpers for independent collection jobs.

These functions also accept legacy error dictionaries containing only ``stage``
and ``message``. They never read or update the collection database.
"""
from __future__ import annotations

import re
from collections.abc import Iterable, Mapping
from typing import Any


_URL = re.compile(r"\b(?:https?|socks[45]h?|wss?)://[^\s<>\"']+", re.I)
_CREDENTIAL_KEYS = (
    r"authorization|proxy[-_]authorization|cookie|set[-_]cookie|"
    r"(?:access[-_]|refresh[-_])?token|api[-_]?key|client[-_]secret|secret|"
    r"(?:proxy[-_])?(?:password|passwd|pwd|username|user)|credentials?"
)
_QUOTED_SECRET = re.compile(
    r"([\"']?(?:" + _CREDENTIAL_KEYS + r")[\"']?\s*[:=]\s*)([\"'])(.*?)\2",
    re.I,
)
_PLAIN_SECRET = re.compile(
    r"(\b(?:" + _CREDENTIAL_KEYS + r")\s*[:=]\s*)(?!\[已隐藏\])([^\s,;\]}\"']+)",
    re.I,
)
_HEADER_SECRET = re.compile(
    r"(?im)^(\s*(?:authorization|proxy-authorization|cookie|set-cookie)\s*:)\s*[^\r\n]*"
)
_BLOCKED = re.compile(
    r"验证码|安全拦截|安全验证|访问验证|请求过于频繁|触发验证|Access Verification|captcha|"
    r"security policy|该来源.*停止继续请求",
    re.I,
)


def safe_error(error: BaseException | str | Any, max_length: int = 350) -> str:
    """Keep a short useful failure reason without URLs or common credentials.

    Playwright errors contain the useful reason before their verbose ``Call
    log``; retain that reason rather than reducing every failure to ``Error``.
    """
    if not isinstance(max_length, int) or isinstance(max_length, bool) or max_length < 40:
        raise ValueError("错误提示长度至少为40")
    message = str(error).strip()
    message = re.split(r"\bCall log:\s*", message, maxsplit=1, flags=re.I)[0]
    message = _HEADER_SECRET.sub(r"\1 [已隐藏]", message)
    message = _URL.sub("[来源地址]", message)
    message = _QUOTED_SECRET.sub(lambda match: match[1] + match[2] + "[已隐藏]" + match[2], message)
    message = _PLAIN_SECRET.sub(r"\1[已隐藏]", message)
    message = re.sub(r"\b(?:Basic|Bearer)\s+[A-Za-z0-9._~+/=-]+", "[认证信息已隐藏]", message, flags=re.I)
    message = re.sub(r"\b[^\s:@/]+:[^\s@/]+@[\w.-]+(?::\d+)?", "[代理凭据已隐藏]", message)
    message = re.sub(r"[\x00-\x1f\x7f]+", " ", message)
    message = re.sub(r"\s+", " ", message).strip()
    if not message or message.lower() in {"error", "exception", "none"}:
        message = "采集失败，来源或浏览器未提供具体原因"
    elif message.lower() in {"timeouterror", "timeout"}:
        message = "等待来源响应或页面数据超时"
    if len(message) > max_length:
        message = message[:max_length - 1] + "…"
    return message


def _source(error: Mapping[str, Any]) -> str:
    explicit = str(error.get("source") or "").lower()
    stage = str(error.get("stage") or "")
    if explicit in {"zgzcw", "足彩网"} or "zgzcw.com" in explicit:
        return "zgzcw"
    if explicit in {"sporttery", "体彩", "竞彩官方"} or "sporttery.cn" in explicit:
        return "sporttery"
    if "足彩网" in stage or re.search(r"(?:^|:)(?:euro|asian|totals|history|映射)(?:$|:)", stage) or "初始凯利" in stage:
        return "zgzcw"
    if any(word in stage for word in ("支持比例", "支持率", "奖金", "比赛信息", "体彩")):
        return "sporttery"
    return ""


def aggregate_errors(errors: Iterable[Mapping[str, Any]]) -> list[dict[str, Any]]:
    """Group repeated verification stops for the same source, preserving order.

    Unclassified errors remain separate, so unrelated failures cannot be
    accidentally merged merely because their text mentions verification.
    """
    result: list[dict[str, Any]] = []
    groups: dict[str, dict[str, Any]] = {}
    for error in errors:
        stage = safe_error(error.get("stage") or "任务", max_length=120)
        message = safe_error(error.get("message") or "")
        source = _source(error)
        if source and _BLOCKED.search(message):
            count = error.get("count", 1)
            if isinstance(count, bool) or not isinstance(count, int) or count < 1:
                count = 1
            if source in groups:
                group = groups[source]
                group["count"] += count
            else:
                group = {"stage": stage, "message": message, "source": source, "count": count}
                groups[source] = group
                result.append(group)
        else:
            result.append({"stage": stage, "message": message})
    for source, group in groups.items():
        if group["count"] > 1:
            name = "足彩网" if source == "zgzcw" else "竞彩官方"
            group["stage"] = name + ":访问验证"
            group["message"] = f"{name}触发访问验证，本任务已停止该来源后续请求；共影响{group['count']}项。"
    return result


def summarize_collection(*, processed: int, complete: int, updated: int,
                         errors: Iterable[Mapping[str, Any]]) -> dict[str, Any]:
    """Report this job, not just whether older complete snapshots still exist.

    ``processed`` is the number of distinct matches handled in this job.
    ``complete`` counts those matches with all required data after the job.
    ``updated`` counts matches with at least one valid snapshot written by this
    job. An unchanged old complete match is never counted as an update.
    """
    counts = (processed, complete, updated)
    if any(isinstance(value, bool) or not isinstance(value, int) or value < 0 for value in counts):
        raise ValueError("采集统计必须是非负整数")
    if complete > processed or updated > processed:
        raise ValueError("完整或更新场数不能超过处理场数")
    reported = aggregate_errors(errors)
    if processed == 0 and not reported:
        status = "completed"
        message = "指定范围内没有可采集比赛；本次未写入数据。"
    elif processed == 0 or complete == 0 or updated == 0:
        status = "failed"
        message = f"处理{processed}场；本次取得数据{updated}场；现有数据完整{complete}场；缺项{processed-complete}场。"
        if updated == 0 and complete:
            message += "本次未取得有效更新，已有数据未算作采集成功。"
        elif processed:
            message += "本次未完成任何一场所需数据。"
    else:
        status = "completed" if complete == processed and updated == processed and not reported else "partial"
        message = f"处理{processed}场；本次取得数据{updated}场；现有数据完整{complete}场；缺项{processed-complete}场。"
        if reported:
            message += "部分采集未成功，详细原因见来源提示。"
    return {"status": status, "processed": processed, "complete": complete,
            "updated": updated, "errors": reported, "message": message}
