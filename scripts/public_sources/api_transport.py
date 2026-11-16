"""Read public API response bodies without substituting scraped web pages.

The browser fallback changes transport only: it requests the same API URL and
decodes the original HTTP response. It never extracts data from a rendered DOM.
"""
from __future__ import annotations

import json
import re
import time
from collections import Counter
from urllib.parse import parse_qsl, urlsplit

from collection_report import safe_error


MAX_RESPONSE_BYTES = 8 * 1024 * 1024
BROWSER_TIMEOUT_MS = 30_000
_VERIFICATION = re.compile(
    r"安全策略拦截|疑似攻击|Access Verification|人机验证|请求过于频繁|"
    r"请输入正确的验证码|验证您是真人", re.I,
)


def _check_blocked(client, url):
    if urlsplit(url).hostname in client.blocked_hosts:
        raise ValueError("该接口来源已触发验证，本次任务停止继续请求")


def _check_verification(client, url, text):
    if _VERIFICATION.search(text):
        client.blocked_hosts.add(urlsplit(url).hostname)
        raise ValueError("接口来源触发安全验证，本次任务停止该来源请求")


def _decode_json(text):
    if not isinstance(text, str):
        raise ValueError("接口响应不是文本JSON")
    if len(text.encode("utf-8")) > MAX_RESPONSE_BYTES:
        raise ValueError("接口响应超过8MB")
    try:
        def reject_constant(value):
            raise ValueError("接口JSON含无效数值")
        value = json.loads(text.lstrip("\ufeff"), parse_constant=reject_constant)
    except (ValueError, TypeError):
        raise ValueError("接口未返回有效JSON；未使用网页内容替代") from None
    if not isinstance(value, (dict, list)):
        raise ValueError("接口JSON必须为对象或数组")
    return value


def _same_endpoint(expected, actual):
    """Ignore only the site's ordinary navigation verification query suffix.

    Match IDs and all other original parameters must remain present; a JSON
    response from another page, host, or match cannot satisfy this request.
    """
    target, source = urlsplit(expected), urlsplit(actual)
    if (source.scheme, source.hostname, source.port, source.path) != (
        target.scheme, target.hostname, target.port, target.path
    ):
        return False
    requested = Counter(parse_qsl(target.query, keep_blank_values=True))
    returned = Counter(parse_qsl(source.query, keep_blank_values=True))
    if requested - returned:
        return False
    return all(key.startswith("security_") for key, _ in (returned - requested))


def _browser_context(client):
    from collector import random_forwarded_for

    proxy = client.proxies[client.index % len(client.proxies)] if client.proxies else None
    client.index += 1
    _, context, page = client._session(proxy)
    # Redirects belonging to this one request use the same advertised address.
    client._page_forwarded_for = random_forwarded_for()
    client._navigation_requests = 0
    return context, page


def _browser_get(client, url):
    _, page = _browser_context(client)
    state = {"value": None, "error": None}
    deadline = time.monotonic() + BROWSER_TIMEOUT_MS / 1000

    def capture(response):
        if not _same_endpoint(url, response.url):
            return
        if 300 <= response.status < 400:
            return  # Redirect responses may have no readable body.
        # Capture response bodies, not JSON-looking text in the page's DOM.
        try:
            text = response.text()
            _check_verification(client, url, text)
            if response.status >= 400:
                state["error"] = ValueError(f"接口来源HTTP {response.status}，未取得数据")
                return
            if len(text.encode("utf-8")) > MAX_RESPONSE_BYTES:
                state["error"] = ValueError("接口响应超过8MB")
                return
            try:
                state["value"] = _decode_json(text)
            except ValueError:
                # A normal browser navigation can first return a non-data
                # document. Only a later JSON HTTP response may satisfy it.
                return
        except Exception as exc:
            state["error"] = exc

    page.on("response", capture)
    navigation_error = None
    try:
        try:
            response = page.goto(url, wait_until="domcontentloaded", timeout=BROWSER_TIMEOUT_MS)
            if response is not None and state["value"] is None:
                capture(response)
        except Exception as exc:
            navigation_error = exc

        while state["value"] is None:
            _check_blocked(client, url)
            if state["error"] is not None:
                raise ValueError("接口请求失败：" + safe_error(state["error"])) from None
            if navigation_error is not None:
                raise ValueError("接口浏览器请求失败：" + safe_error(navigation_error)) from None
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise ValueError("等待接口JSON响应超时；未使用网页内容替代")
            page.wait_for_timeout(min(200, remaining * 1000))
        _check_blocked(client, url)
        return state["value"]
    finally:
        page.remove_listener("response", capture)


def _browser_post(client, url, form):
    context, _ = _browser_context(client)
    # APIRequestContext calls do not pass through the page routing handler.
    client.throttle()
    headers = {
        "Accept": "application/json,text/plain,*/*",
        "X-Forwarded-For": client._page_forwarded_for,
        "Referer": urlsplit(url).scheme + "://" + urlsplit(url).netloc + "/",
    }
    response = context.request.post(
        url, form=form, headers=headers, timeout=BROWSER_TIMEOUT_MS,
        max_redirects=0,
    )
    try:
        text = response.text()
        _check_verification(client, url, text)
        if response.status >= 300:
            raise ValueError(f"接口来源HTTP {response.status}，未取得JSON数据")
        if not _same_endpoint(url, response.url):
            raise ValueError("接口响应地址不匹配，未关联其他来源数据")
        return _decode_json(text)
    finally:
        response.dispose()


def fetch_json(client, url, form=None):
    """Return JSON from this API; transport failure never authorizes HTML data."""
    from collector import check_url

    check_url(url)
    _check_blocked(client, url)
    if form is not None and not isinstance(form, dict):
        raise ValueError("接口表单参数必须为对象")
    try:
        raw = client.http(url, form=form)
        _check_verification(client, url, raw)
        value = _decode_json(raw)
        client.last_api_transport = "http"
        return value
    except (ValueError, OSError) as exc:
        direct_error = exc
    _check_blocked(client, url)
    if not client.browser_enabled:
        raise ValueError("接口请求失败：" + safe_error(direct_error)) from None
    try:
        value = _browser_get(client, url) if form is None else _browser_post(client, url, form)
    except Exception as exc:
        _check_blocked(client, url)
        raise ValueError("接口请求失败：" + safe_error(exc)) from None
    client.last_api_transport = "browser"
    return value
