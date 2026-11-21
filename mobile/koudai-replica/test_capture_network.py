"""Privacy and real CDP message-sequencing tests, with no device or service access."""
import asyncio
import base64
import copy
import io
import json
import tempfile
import unittest
import urllib.parse
from contextlib import redirect_stdout
from pathlib import Path

import websockets
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
from cryptography.hazmat.primitives.padding import PKCS7

from capture_network import Capture, request_details, response_record


class PrivacyTests(unittest.TestCase):
    def test_account_response_keeps_structure_without_values_or_headers(self):
        request = {"url": "https://apic.jindianle.com/api/user/info?token=PRIVATE_QUERY", "method": "POST", "postData": "password=PRIVATE_PASSWORD", "headers": {"Authorization": "PRIVATE_HEADER"}}
        original = copy.deepcopy(request)
        item = response_record(request, {"status": 200}, json.dumps({"data": {"token": "PRIVATE_TOKEN", "nickname": "PRIVATE_NAME", "phone": "19999999999"}}), (b"a" * 24, b"0" * 16))
        serialized = json.dumps(item)
        self.assertNotIn("PRIVATE_", serialized)
        self.assertNotIn("19999999999", serialized)
        self.assertEqual(item["retention"], "schema only")
        self.assertIn("$.data.token", item["response_schema"])
        self.assertNotIn("response", item)
        self.assertEqual(request, original)

    def test_sports_values_survive_but_auth_values_do_not(self):
        request = {"url": "https://match-api.18aitou.com/qkdata/odds/betfair/trade/info?matchId=123&token=PRIVATE_QUERY", "method": "POST"}
        item = response_record(request, {"status": 200}, json.dumps({"body": {"matchId": 123, "leftLoss": 6065, "token": "PRIVATE_TOKEN", "ownerMobile": "19999999999"}}), (b"a" * 24, b"0" * 16))
        self.assertEqual(item["response"]["body"]["leftLoss"], 6065)
        self.assertEqual(item["response"]["body"]["token"], "[redacted]")
        self.assertNotIn("PRIVATE_", json.dumps(item))
        self.assertNotIn("19999999999", json.dumps(item))

    def test_encrypted_transport_exposes_match_parameters_only(self):
        key, iv = b"a" * 24, b"0" * 16
        raw = json.dumps({"header": {"token": "PRIVATE_HEADER_TOKEN"}, "body": {"matchId": 456, "lotteryId": 90, "token": "PRIVATE_BODY_TOKEN"}}).encode()
        pad = PKCS7(128).padder()
        padded = pad.update(raw) + pad.finalize()
        encryptor = Cipher(algorithms.AES(key), modes.CBC(iv)).encryptor()
        encrypted = encryptor.update(padded) + encryptor.finalize()
        request = {"url": "https://match-api.18aitou.com/qkdata/odds/betfair/trade/info", "postData": urllib.parse.urlencode({"request": base64.b64encode(encrypted).decode()})}
        result = request_details(request, (key, iv))
        self.assertEqual(result["safe_parameters"], {"matchId": 456, "lotteryId": 90})
        self.assertNotIn("PRIVATE_", json.dumps(result))


class CdpTests(unittest.IsolatedAsyncioTestCase):
    async def test_actual_websocket_response_body_is_collected_without_deadlock(self):
        observed_commands = []

        async def fake_webview(ws):
            async for wire in ws:
                command = json.loads(wire)
                observed_commands.append(command["method"])
                if command["method"] == "Network.enable":
                    await ws.send(json.dumps({"id": command["id"], "result": {}}))
                    for method, params in [
                        ("Network.requestWillBeSent", {"requestId": "r1", "request": {"url": "https://match-api.18aitou.com/qkdata/odds/betfair/trade/info", "method": "POST", "headers": {"Cookie": "PRIVATE_COOKIE"}}}),
                        ("Network.responseReceived", {"requestId": "r1", "response": {"status": 200, "mimeType": "application/json", "headers": {"Set-Cookie": "PRIVATE_COOKIE"}}}),
                        ("Network.loadingFinished", {"requestId": "r1"}),
                    ]:
                        await ws.send(json.dumps({"method": method, "params": params}))
                elif command["method"] == "Network.getResponseBody":
                    await ws.send(json.dumps({"id": command["id"], "result": {"body": '{"body":{"leftLoss":6065}}', "base64Encoded": False}}))

        with tempfile.TemporaryDirectory() as directory, redirect_stdout(io.StringIO()):
            async with websockets.serve(fake_webview, "127.0.0.1", 0) as server:
                port = server.sockets[0].getsockname()[1]
                async with websockets.connect(f"ws://127.0.0.1:{port}") as ws:
                    observer = Capture(ws, Path(directory), (b"a" * 24, b"0" * 16))
                    reader = asyncio.create_task(observer.read())
                    await observer.command("Network.enable")
                    for _ in range(100):
                        if observer.records:
                            break
                        await asyncio.sleep(0.01)
                    self.assertEqual(len(observer.records), 1)
                    self.assertEqual(observer.records[0]["response"]["body"]["leftLoss"], 6065)
                    self.assertNotIn("PRIVATE_COOKIE", (Path(directory) / "responses.jsonl").read_text())
                    summary = observer.save_summary()
                    self.assertEqual(summary["distinct_interfaces"], 1)
                    self.assertEqual(observed_commands, ["Network.enable", "Network.getResponseBody"])
                    reader.cancel()
                    await asyncio.gather(reader, return_exceptions=True)


if __name__ == "__main__":
    unittest.main()
