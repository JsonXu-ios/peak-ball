"""API transport regressions; no real network, browser, services, or database."""
import json
import sys
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

import api_transport
from api_transport import fetch_json


URL = "https://fenxi.zgzcw.com/json/4518112/bjop"


def response(url=URL, value=None, text=None, status=200):
    if text is None:
        text = json.dumps(value if value is not None else {"company5List": []})
    return SimpleNamespace(url=url, status=status, text=Mock(return_value=text), dispose=Mock())


class Page:
    """Event boundary double: only HTTP responses may contain source data."""
    def __init__(self, responses=(), later=(), navigation_error=None):
        self.responses = list(responses)
        self.later = list(later)
        self.navigation_error = navigation_error
        self.listener = None
        self.removed = False
        self.destinations = []
        self.waits = []

    def on(self, event, callback):
        assert event == "response"
        self.listener = callback

    def remove_listener(self, event, callback):
        assert event == "response" and callback == self.listener
        self.removed = True
        self.listener = None

    def goto(self, url, **kwargs):
        self.destinations.append(url)
        for item in self.responses:
            self.listener(item)
        if self.navigation_error:
            raise self.navigation_error
        return self.responses[-1] if self.responses else None

    def wait_for_timeout(self, timeout):
        self.waits.append(timeout)
        if self.later:
            self.listener(self.later.pop(0))

    def content(self):
        raise AssertionError("API data cannot be read from DOM")

    def inner_text(self, *_):
        raise AssertionError("API data cannot be read from DOM")


def client(page=None, direct=None):
    page = page or Page()
    context = SimpleNamespace(request=Mock())
    return SimpleNamespace(
        http=Mock(return_value=direct) if direct is not None else Mock(side_effect=ValueError("需要正常浏览器加载")),
        browser_enabled=True, blocked_hosts=set(), proxies=[], index=0,
        _session=Mock(return_value=(Mock(), context, page)), throttle=Mock(),
        _page_forwarded_for=None, _navigation_requests=0,
    )


class ApiTransportTests(unittest.TestCase):
    def setUp(self):
        self.ip = patch("collector.random_forwarded_for", return_value="8.8.8.8")
        self.ip.start()
        self.addCleanup(self.ip.stop)

    def test_direct_json_never_opens_browser(self):
        source = client(direct='{"matches":[{"id":12}]}')
        self.assertEqual(fetch_json(source, URL), {"matches": [{"id": 12}]})
        source.http.assert_called_once_with(URL, form=None)
        source._session.assert_not_called()
        self.assertEqual(source.last_api_transport, "http")

    def test_form_keeps_same_api_and_parameters(self):
        source = client(direct='[]')
        form = {"type": "jc", "issue": "2026-11-15", "companys": "2,9,22"}
        self.assertEqual(fetch_json(source, URL, form=form), [])
        source.http.assert_called_once_with(URL, form=form)
        source._session.assert_not_called()

    def test_browser_reads_original_response_and_retains_one_header_for_navigation(self):
        page = Page([
            response(text="<html><script>ordinary navigation</script></html>"),
            response(url=URL + "?security_antibot_code=fixture", value={"company5List": [{"attribute": {"cid": 9}}]}),
        ])
        source = client(page)
        self.assertEqual(fetch_json(source, URL), {"company5List": [{"attribute": {"cid": 9}}]})
        self.assertEqual(page.destinations, [URL])
        self.assertEqual(source._page_forwarded_for, "8.8.8.8")
        self.assertEqual(source._navigation_requests, 0)
        self.assertTrue(page.removed)
        self.assertEqual(source.last_api_transport, "browser")

    def test_wrong_match_and_unrelated_json_cannot_satisfy_this_api(self):
        page = Page(
            responses=[response(url=URL.replace("4518112", "123"), value={"wrong": True}),
                       response(url="https://unrelated.example/json/4518112/bjop", value={"wrong": True})],
            later=[response(value={"correct": True})],
        )
        self.assertEqual(fetch_json(client(page), URL), {"correct": True})
        self.assertEqual(len(page.waits), 1)
        self.assertTrue(page.removed)

    def test_redirect_without_readable_body_does_not_hide_final_json(self):
        redirect = response(status=302)
        redirect.text.side_effect = RuntimeError("Redirect response has no body")
        page = Page([redirect], later=[response(value={"loaded": True})])
        self.assertEqual(fetch_json(client(page), URL), {"loaded": True})
        redirect.text.assert_not_called()
        self.assertTrue(page.removed)

    def test_query_identity_is_preserved(self):
        requested = URL + "?matchId=1&poolCode=HAD"
        self.assertTrue(api_transport._same_endpoint(requested, requested + "&security_antibot_code=x"))
        self.assertFalse(api_transport._same_endpoint(requested, URL + "?matchId=2&poolCode=HAD"))
        self.assertFalse(api_transport._same_endpoint(URL, URL + "?matchId=2"))

    def test_html_json_fragment_is_not_api_data(self):
        page = Page([response(text='<pre>{"forged":"DOM only"}</pre>')])
        source = client(page)
        with patch("api_transport.time.monotonic", side_effect=[0, 31]), \
                self.assertRaisesRegex(ValueError, "超时"):
            fetch_json(source, URL)
        self.assertTrue(page.removed)

    def test_verification_response_stops_source_without_reading_dom(self):
        page = Page([response(text="<h1>Access Verification 请求过于频繁</h1>")])
        source = client(page)
        with self.assertRaisesRegex(ValueError, "验证"):
            fetch_json(source, URL)
        self.assertIn("fenxi.zgzcw.com", source.blocked_hosts)
        self.assertTrue(page.removed)
        source.http.reset_mock()
        with self.assertRaisesRegex(ValueError, "验证"):
            fetch_json(source, URL)
        source.http.assert_not_called()

    def test_direct_verification_never_opens_browser(self):
        source = client(direct="Access Verification 请求过于频繁")
        with self.assertRaisesRegex(ValueError, "验证"):
            fetch_json(source, URL)
        source._session.assert_not_called()

    def test_browser_post_uses_same_form_and_cleans_response(self):
        page = Page()
        source = client(page)
        context = source._session.return_value[1]
        reply = response(value=[{"ID": 4518112}])
        context.request.post.return_value = reply
        form = {"type": "jc", "issue": "2026-11-15"}
        self.assertEqual(fetch_json(source, URL, form), [{"ID": 4518112}])
        args, kwargs = context.request.post.call_args
        self.assertEqual(args, (URL,))
        self.assertEqual(kwargs["form"], form)
        self.assertEqual(kwargs["headers"]["X-Forwarded-For"], "8.8.8.8")
        self.assertEqual(kwargs["max_redirects"], 0)
        source.throttle.assert_called_once()
        reply.dispose.assert_called_once()
        self.assertEqual(page.destinations, [])

    def test_browser_post_cannot_return_html_or_follow_redirect(self):
        for reply in (response(text="<table>fake</table>"), response(status=302)):
            with self.subTest(status=reply.status):
                source = client()
                source._session.return_value[1].request.post.return_value = reply
                with self.assertRaises(ValueError):
                    fetch_json(source, URL, {"type": "jc"})
                reply.dispose.assert_called_once()

    def test_listener_is_removed_when_navigation_fails(self):
        page = Page(navigation_error=RuntimeError("net::ERR_CONNECTION_RESET"))
        with self.assertRaisesRegex(ValueError, "ERR_CONNECTION_RESET"):
            fetch_json(client(page), URL)
        self.assertTrue(page.removed)

    def test_disabled_browser_keeps_clear_api_error(self):
        source = client(direct="<html>not JSON</html>")
        source.browser_enabled = False
        with self.assertRaisesRegex(ValueError, "JSON"):
            fetch_json(source, URL)
        source._session.assert_not_called()

    def test_untrusted_url_and_invalid_form_stop_before_transport(self):
        source = client(direct="{}")
        for target, form in (("https://example.com/data", None), (URL, "a=1")):
            with self.subTest(target=target), self.assertRaises(ValueError):
                fetch_json(source, target, form)
        source.http.assert_not_called()

    def test_json_scalars_nonfinite_and_oversized_data_are_not_accepted(self):
        for raw in ("null", "12", '{"bad": NaN}', '"text"'):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                api_transport._decode_json(raw)
        with patch("api_transport.MAX_RESPONSE_BYTES", 10), self.assertRaisesRegex(ValueError, "8MB"):
            api_transport._decode_json('{"value":"long"}')


if __name__ == "__main__":
    unittest.main()
