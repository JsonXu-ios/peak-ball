"""Read actual HTTP responses from the replica's explicitly enabled WebView debugger.

Only observes Network events. Does not replay or modify requests, cookies or TLS.
Account/payment endpoints retain field structure only; sports routes may retain
sanitized response bodies. Raw headers and raw POST bodies are never persisted.
"""
import argparse
import asyncio
import base64
import collections
import datetime
import json
import re
import time
import urllib.parse
import urllib.request
import zipfile
from pathlib import Path

import websockets
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.primitives.padding import PKCS7

BUSINESS_HOSTS = {
    "apic.jindianle.com", "match-api.18aitou.com", "monetize-apic.jindianle.com",
    "apic-sport-new.jdddata.com", "data2c.jdddata.com", "apiws-sport-new.jdddata.com",
    "qms.jindianle.com", "stat-api.51aitou.com", "upload-apic.jindianle.com",
}
SPORTS_PREFIXES = ("/qkdata/odds/", "/qkdata/soccer/", "/itou/soccer/", "/sportdata/odds/", "/itou/sportdata/soccer/")
SPORTS_PATHS = {
    "/api/match/selectlist", "/api/match/selectmore", "/api/match/finishresult",
    "/api/match/prizelist", "/api/match/leaguequerylist",
}
SAFE_PARAMETERS = {"matchId", "matchid", "match_id", "lotteryId", "lottery_id", "date", "date_list", "jc_type", "partner", "type", "oddsType"}
SENSITIVE_KEY = re.compile(r"token|password|passwd|secret|cookie|authorization|phone|mobile|openid|unionid|user[_-]?id|username|station[_-]?uuid|idcard|bankcard|payment|pay[_-]?sign", re.I)
PHONE = re.compile(r"(?<!\d)1\d{10}(?!\d)")


def schema(value, path="$", result=None, depth=0):
    result = {} if result is None else result
    kind = "null" if value is None else "boolean" if isinstance(value, bool) else "number" if isinstance(value, (int, float)) else "string" if isinstance(value, str) else "array" if isinstance(value, list) else "object"
    result.setdefault(path, set()).add(kind)
    if depth >= 9 or len(result) > 3000:
        return result
    if isinstance(value, dict):
        for key, child in list(value.items())[:300]:
            key = "*" if str(key).isdigit() else str(key)
            schema(child, path + "." + key, result, depth + 1)
    elif isinstance(value, list):
        for child in value[:32]:
            schema(child, path + "[]", result, depth + 1)
    return result


def schema_json(value):
    return {k: sorted(v) for k, v in sorted(schema(value).items())}


def redact(value, depth=0):
    if depth > 20:
        return "[depth limit]"
    if isinstance(value, dict):
        return {str(k): "[redacted]" if SENSITIVE_KEY.search(str(k)) else redact(v, depth + 1) for k, v in value.items()}
    if isinstance(value, list):
        return [redact(v, depth + 1) for v in value]
    if isinstance(value, str):
        value = PHONE.sub("[phone hidden]", value)
        if value.startswith(("http://", "https://")):
            url = urllib.parse.urlsplit(value)
            pairs = [(k, "[redacted]" if SENSITIVE_KEY.search(k) else v) for k, v in urllib.parse.parse_qsl(url.query)]
            return urllib.parse.urlunsplit((url.scheme, url.netloc, url.path, urllib.parse.urlencode(pairs), ""))
    return value


def transport_keys(apk):
    with zipfile.ZipFile(apk) as z:
        script = z.read("assets/www/static/js/public/service-6a3410de12.js").decode()
    module = script.split("/* 109 */", 1)[1].split("/* 110 */", 1)[0]
    key = re.search(r"key = encUtf8\.parse\('([^']+)'\)", module).group(1).encode()
    iv = re.search(r"iv = encUtf8\.parse\('([^']+)'\)", module).group(1).encode()
    return key, iv


def request_details(request, keys):
    url = urllib.parse.urlsplit(request["url"])
    query = urllib.parse.parse_qs(url.query)
    params = {k: v[0] if len(v) == 1 else v for k, v in query.items() if k in SAFE_PARAMETERS}
    detail = {"query_fields": sorted(query), "safe_parameters": params}
    raw = request.get("postData")
    if not raw:
        return detail
    try:
        body = json.loads(raw)
    except (ValueError, TypeError):
        body = urllib.parse.parse_qs(raw)
    detail["post_fields"] = sorted(body) if isinstance(body, dict) else []
    if isinstance(body, dict) and "request" in body and url.hostname in BUSINESS_HOSTS:
        try:
            encoded = body["request"][0] if isinstance(body["request"], list) else body["request"]
            key, iv = keys
            decryptor = Cipher(algorithms.AES(key), modes.CBC(iv)).decryptor()
            padded = decryptor.update(base64.b64decode(encoded)) + decryptor.finalize()
            unpadder = PKCS7(128).unpadder()
            decoded = json.loads((unpadder.update(padded) + unpadder.finalize()).decode())
            body = decoded.get("body", {})
            detail["transport_envelope"] = "decoded using the supplied APK's own anonymous match transport format"
        except Exception:
            detail["transport_envelope"] = "encrypted or unrecognized; not persisted"
            return detail
    detail["request_schema"] = schema_json(body)
    if isinstance(body, dict):
        detail["safe_parameters"].update({k: redact(v) for k, v in body.items() if k in SAFE_PARAMETERS})
    return detail


def parse_response(text):
    try:
        return json.loads(text)
    except ValueError:
        match = re.fullmatch(r"\s*[A-Za-z_$][\w.$]*\((.*)\);?\s*", text, re.S)
        if match:
            return json.loads(match.group(1))
        raise


def response_record(request, response, body, keys):
    url = urllib.parse.urlsplit(request["url"])
    item = {
        "observed_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "source": "WebView CDP Network.getResponseBody",
        "host": url.hostname, "path": url.path, "method": request.get("method"),
        "status": response.get("status"), "mime_type": response.get("mimeType"),
        "from_disk_cache": response.get("fromDiskCache", False),
        "request": request_details(request, keys),
    }
    try:
        obj = parse_response(body)
    except (ValueError, TypeError):
        item["response_format"] = "non-JSON or encrypted; content not retained"
        item["response_characters"] = len(body)
        return item
    item["response_schema"] = schema_json(obj)
    sports = url.path.startswith(SPORTS_PREFIXES) or url.path.rstrip("/") in SPORTS_PATHS
    item["retention"] = "sanitized sports response" if sports else "schema only"
    if sports:
        item["response"] = redact(obj)
    return item


class Capture:
    def __init__(self, ws, output, keys):
        self.ws, self.output, self.keys = ws, output, keys
        self.serial = 0
        self.pending = {}
        self.requests = {}
        self.responses = {}
        self.tasks = set()
        self.records = []

    async def command(self, method, params=None):
        self.serial += 1
        number = self.serial
        waiter = asyncio.get_running_loop().create_future()
        self.pending[number] = waiter
        await self.ws.send(json.dumps({"id": number, "method": method, "params": params or {}}))
        try:
            return await asyncio.wait_for(waiter, 12)
        finally:
            self.pending.pop(number, None)

    async def read(self):
        async for raw in self.ws:
            message = json.loads(raw)
            if "id" in message:
                waiter = self.pending.get(message["id"])
                if waiter and not waiter.done():
                    if "error" in message:
                        waiter.set_exception(RuntimeError(message["error"].get("message", "CDP error")))
                    else:
                        waiter.set_result(message.get("result", {}))
                continue
            method, params = message.get("method"), message.get("params", {})
            request_id = params.get("requestId")
            if method == "Network.requestWillBeSent":
                request = params.get("request", {})
                url = urllib.parse.urlsplit(request.get("url", ""))
                if url.hostname in BUSINESS_HOSTS and request.get("method") != "OPTIONS":
                    # Keep no headers even in the per-request in-memory record.
                    self.requests[request_id] = {k: request[k] for k in ("url", "method", "postData") if k in request}
            elif method == "Network.responseReceived" and request_id in self.requests:
                response = params["response"]
                self.responses[request_id] = {k: response[k] for k in ("status", "mimeType", "fromDiskCache") if k in response}
            elif method == "Network.loadingFinished" and request_id in self.requests:
                task = asyncio.create_task(self.collect(request_id))
                self.tasks.add(task)
                task.add_done_callback(self.tasks.discard)
            elif method == "Network.loadingFailed" and request_id in self.requests:
                request = self.requests.pop(request_id)
                url = urllib.parse.urlsplit(request["url"])
                self.append({"host": url.hostname, "path": url.path, "method": request.get("method"), "failed": True, "error": params.get("errorText", "request failed")})

    async def collect(self, request_id):
        request = self.requests.pop(request_id)
        response = self.responses.pop(request_id, {})
        try:
            result = await self.command("Network.getResponseBody", {"requestId": request_id})
            body = result.get("body", "")
            if result.get("base64Encoded"):
                body = base64.b64decode(body).decode("utf-8", "replace")
            item = response_record(request, response, body, self.keys)
        except Exception as exc:
            url = urllib.parse.urlsplit(request["url"])
            item = {"host": url.hostname, "path": url.path, "method": request.get("method"), "status": response.get("status"), "body_unavailable": str(exc)[:160]}
        self.append(item)

    def append(self, item):
        self.records.append(item)
        with (self.output / "responses.jsonl").open("a", encoding="utf-8") as stream:
            stream.write(json.dumps(item, ensure_ascii=False) + "\n")
        print(json.dumps({k: item[k] for k in ("host", "path", "method", "status", "failed", "retention") if k in item}), flush=True)

    def save_summary(self):
        counts = collections.Counter((r.get("host"), r.get("path"), r.get("method")) for r in self.records)
        result = {
            "transport": "read-only WebView CDP Network observation",
            "native_sdk_traffic_included": False,
            "business_hosts": sorted(BUSINESS_HOSTS),
            "response_events": len(self.records), "distinct_interfaces": len(counts),
            "interfaces": [{"host": h, "path": p, "method": m, "response_events": count} for (h, p, m), count in sorted(counts.items())],
            "scope_note": "Only observed activity during this run; not proof that every packaged interface was called.",
        }
        (self.output / "summary.json").write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding="utf-8")
        return result


async def capture(args):
    if args.output.exists() and any(args.output.iterdir()):
        raise RuntimeError("Use a new empty capture directory to avoid mixing runs")
    args.output.mkdir(parents=True, exist_ok=True)
    with urllib.request.urlopen(f"http://127.0.0.1:{args.port}/json", timeout=10) as response:
        targets = json.load(response)
    pages = [p for p in targets if p.get("type") == "page" and p.get("webSocketDebuggerUrl")]
    if args.target_id:
        pages = [p for p in pages if p.get("id") == args.target_id]
    elif len(pages) > 1:
        bundled = [p for p in pages if p.get("url", "").startswith("file:") and "/index" in p.get("url", "")]
        if len(bundled) == 1:
            pages = bundled
    if len(pages) != 1:
        raise RuntimeError(f"Expected one replica WebView, found {len(pages)}; select the intended target explicitly")
    keys = transport_keys(args.apk)
    async with websockets.connect(pages[0]["webSocketDebuggerUrl"], open_timeout=10, max_size=16 * 1024 * 1024) as ws:
        observer = Capture(ws, args.output, keys)
        reader = asyncio.create_task(observer.read())
        await observer.command("Network.enable", {"maxTotalBufferSize": 16000000, "maxResourceBufferSize": 4000000, "maxPostDataSize": 16384})
        print(json.dumps({"capture_ready": True, "output": str(args.output), "seconds": args.seconds}), flush=True)
        deadline = time.monotonic() + args.seconds
        try:
            while time.monotonic() < deadline and not (args.output / "STOP").exists() and not reader.done():
                await asyncio.sleep(0.5)
            if observer.tasks:
                await asyncio.wait(observer.tasks, timeout=12)
        finally:
            summary = observer.save_summary()
            print(json.dumps({"capture_complete": True, "distinct_interfaces": summary["distinct_interfaces"], "response_events": summary["response_events"]}), flush=True)
            reader.cancel()
            await asyncio.gather(reader, return_exceptions=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=int, default=19229)
    parser.add_argument("--seconds", type=int, default=180)
    parser.add_argument("--target-id", help="Explicit WebView target when more than one page is exposed")
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--apk", type=Path, default=Path(__file__).resolve().parents[2] / "reports/source-probes/koudai-mobile-20261119/base.apk")
    asyncio.run(capture(parser.parse_args()))
