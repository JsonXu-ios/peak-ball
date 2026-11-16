"""Regression coverage for public-page loading and vote-list completeness.

Only the transport/browser boundaries are mocked. These tests do not contact
source sites, start browsers, or open the application's databases.
"""
import sys
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, MagicMock, patch
from urllib.parse import parse_qs, urlsplit

sys.path.insert(0, str(Path(__file__).resolve().parent))

from collector import Client, parse_official_page
import collector


DAY = "2026-11-15"


def vote_row(match_id):
    return {
        "matchId": match_id,
        "matchNum": "周日" + str(match_id).zfill(3),
        "businessDate": DAY,
        "matchDate": DAY,
        "homeTeamAllName": "主队" + str(match_id),
        "awayTeamAllName": "客队" + str(match_id),
        "hsupportRate": "40%", "dsupportRate": "30%", "asupportRate": "30%",
        "h": "2.1", "d": "3.0", "a": "3.2",
    }


def vote_page(ids, page=1, pages=1, total=None):
    return {"errorCode": "0", "value": {"matches": {
        "list": [vote_row(match_id) for match_id in ids],
        "pageNo": page, "pages": pages, "pageSize": 10,
        "total": total if total is not None else len(ids),
    }}}


def official_card(home="日本", away="巴西", stamp="2026-11-16 01:00"):
    return (
        '<div class="m-matchCard-Cen"><div class="u-top">周日003</div>'
        f'<div class="u-middleLf">{home}</div><div class="u-middleRt">{away}</div>'
        f'<div class="u-btm">{stamp}</div></div>'
    )


def official_head_fixture():
    # Provider's internal matchId is deliberately not the sportteryMatchId.
    return {"errorCode": "0", "value": {
        "sportteryMatchId": 2042767, "matchId": 2644728,
        "matchNum": "周日001", "matchDateTime": "2026-11-15 13:00",
        "homeTeamShortName": "町田泽维", "awayTeamShortName": "神户胜利",
    }}


class CollectorRuntimeTests(unittest.TestCase):
    def setUp(self):
        self.env = patch.dict("os.environ", {"PUBLIC_SOURCE_PROXIES": "[]"}, clear=True)
        self.env.start()
        self.addCleanup(self.env.stop)
        self.client = Client()
        self.addCleanup(lambda: getattr(self.client, "close", lambda: None)())

    def test_empty_vue_card_is_not_a_match(self):
        raw = ('<div class="m-matchCard-Cen"><div class="u-top"></div>'
               '<div class="u-middleLf"></div><div class="u-middleRt"></div>'
               '<div class="u-btm"></div></div>')
        with self.assertRaises(ValueError):
            parse_official_page(raw, "周日003")

    def test_login_verification_label_does_not_block_an_ordinary_data_response(self):
        response = Mock()
        response.headers.get_content_charset.return_value = "utf-8"
        body = '<form>登录 验证码</form><table><tr><td>真实比赛数据</td></tr></table>'
        response.read.return_value = body.encode("utf-8")
        opener = MagicMock()
        opener.open.return_value.__enter__.return_value = response
        self.client._openers[None] = opener
        with patch.object(self.client, "pace", return_value=None):
            self.assertEqual(self.client.http("https://cp.zgzcw.com/lottery/jchtplayvsForJsp.action"), body)
            response.read.return_value = b"<html>Please Enable JavaScript and Cookie.</html>"
            with self.assertRaisesRegex(ValueError, "要求浏览器"):
                self.client.http("https://cp.zgzcw.com/lottery/jchtplayvsForJsp.action")

    def test_official_card_requires_nonempty_participants(self):
        for home, away in (("", "巴西"), ("日本", "  ")):
            with self.subTest(home=home, away=away), self.assertRaises(ValueError):
                parse_official_page(official_card(home=home, away=away), "周日003")

    def test_match_info_api_failure_never_renders_a_page(self):
        with patch.object(self.client, "api", side_effect=ValueError("接口暂不可用")), \
                patch.object(self.client, "http") as http, \
                patch.object(self.client, "render") as render:
            with self.assertRaisesRegex(ValueError, "接口暂不可用"):
                self.client.match_info("2042773", "周日003")
        http.assert_not_called()
        render.assert_not_called()

    def test_official_head_api_uses_the_official_id_and_preserves_kickoff(self):
        head = collector.parse_official_head(official_head_fixture(), "2042767", "周日001")
        self.assertEqual(head["kickoff"], "2026-11-15 13:00:00")
        self.assertEqual((head["home"], head["away"]), ("町田泽维", "神户胜利"))

    def test_official_head_api_rejects_wrong_identity(self):
        for values in ({"sportteryMatchId": 2042768}, {"matchNum": "周日002"},
                       {"sportteryMatchId": 2042768, "matchId": 2042767}):
            response = official_head_fixture()
            response["value"].update(values)
            with self.subTest(values=values), self.assertRaises(ValueError):
                collector.parse_official_head(response, "2042767", "周日001")

    def test_official_head_api_requires_real_time_and_both_teams(self):
        for values in ({"matchDateTime": ""}, {"matchDateTime": "2026-02-30 13:00"},
                       {"homeTeamShortName": " "}, {"awayTeamShortName": ""}):
            response = official_head_fixture()
            response["value"].update(values)
            with self.subTest(values=values), self.assertRaises(ValueError):
                collector.parse_official_head(response, "2042767", "周日001")

    def test_valid_official_api_head_does_not_need_an_empty_browser_page(self):
        with patch.object(self.client, "api", return_value=official_head_fixture()) as api, \
                patch.object(self.client, "http") as http, \
                patch.object(self.client, "render") as render:
            head, _, _, url = self.client.match_info("2042767", "周日001")
        self.assertEqual(head["kickoff"], "2026-11-15 13:00:00")
        self.assertIn("getMatchHeadV1.qry", url)
        self.assertIn("sportteryMatchId=2042767", api.call_args.args[0])
        http.assert_not_called()
        render.assert_not_called()

    def test_history_shell_and_future_fixtures_are_not_completed_history(self):
        header = ["联赛", "轮次", "时间", "主队", "比分", "客队", "半场"]
        future = ["欧国联", "", "26-11-15", "格鲁吉亚", "VS", "匈牙利", ""]
        for tables in ([], [[]], [[header]], [[header, future]], [[["暂无数据"]]]):
            with self.subTest(tables=tables), self.assertRaises(ValueError):
                collector.validate_history_tables(tables)

    def test_history_accepts_actual_zgzcw_and_official_date_formats(self):
        for day in ("26-10-6", "2026-10-06"):
            tables = [[["欧国联", "B联赛", day, "北爱尔兰", "0:0", "格鲁吉亚", "0:0"]]]
            with self.subTest(day=day):
                collector.validate_history_tables(tables)

    def test_votes_reads_the_declared_second_page(self):
        responses = [vote_page(range(1, 11), page=1, pages=2, total=12),
                     vote_page(range(11, 13), page=2, pages=2, total=12)]
        with patch.object(self.client, "api", side_effect=responses) as api:
            result = self.client.votes(DAY, "HAD", 50)
        self.assertEqual([row[0]["matchId"] for row in result], list(range(1, 13)))
        queries = [parse_qs(urlsplit(call.args[0]).query) for call in api.call_args_list]
        self.assertEqual([q["pageNo"] for q in queries], [["1"], ["2"]])
        self.assertTrue(all(q["businessDate"] == [DAY] for q in queries))

    def test_vote_limit_does_not_invent_missing_matches(self):
        with patch.object(self.client, "api", return_value=vote_page(range(1, 11))) as api:
            result = self.client.votes(DAY, "HAD", 50)
        self.assertEqual(len(result), 10)
        api.assert_called_once()

    def test_failed_second_vote_page_is_not_reported_as_complete(self):
        first = vote_page(range(1, 11), page=1, pages=2, total=12)
        with patch.object(self.client, "api", side_effect=[first, ValueError("来源未返回数据")]), \
                patch.object(self.client, "render") as render:
            with self.assertRaisesRegex(ValueError, "分页"):
                self.client.votes(DAY, "HAD", 50)
        render.assert_not_called()

    def test_source_cannot_replace_requested_day_with_a_different_business_day(self):
        response = vote_page([1])
        response["value"]["matches"]["list"][0]["businessDate"] = "2026-11-14"
        with patch.object(self.client, "api", return_value=response), \
                patch.object(self.client, "render", return_value="<main>暂无比赛</main>"):
            with self.assertRaises(ValueError):
                self.client.votes(DAY, "HAD", 10)

    def test_wait_timeout_is_reported_instead_of_returning_empty_page(self):
        page = Mock()
        page.inner_text.return_value = "足球对阵详情 VS 暂无数据"
        page.locator.return_value.inner_text.return_value = page.inner_text.return_value
        page.wait_for_function.side_effect = TimeoutError("fixture: body never populated")
        with self.assertRaisesRegex(ValueError, "超时"):
            self.client._wait_ready(
                page,
                "https://www.sporttery.cn/jc/zqdz/index.html?showType=2&mid=2042773",
                ready=".m-matchCard-Cen .u-btm",
            )

    def test_multiple_pages_share_browser_until_client_is_closed(self):
        with patch("playwright.sync_api.sync_playwright") as playwright:
            runtime = playwright.return_value.start.return_value
            browser = runtime.chromium.launch.return_value
            context = browser.new_context.return_value
            page = context.new_page.return_value
            page.goto.return_value.status = 200
            page.inner_text.return_value = "周日003 日本 巴西 2026-11-16 01:00"
            page.content.return_value = official_card()

            first = self.client.render("https://www.sporttery.cn/jc/zqdz/index.html?mid=1")
            second = self.client.render("https://www.sporttery.cn/jc/zqdz/index.html?mid=2")
            self.assertEqual(first, official_card())
            self.assertEqual(second, official_card())
            runtime.chromium.launch.assert_called_once()
            browser.new_context.assert_called_once()
            context.new_page.assert_called_once()
            browser.close.assert_not_called()

            self.client.close()
            self.client.close()
            browser.close.assert_called_once()
            runtime.stop.assert_called_once()

    def test_fixed_forwarded_header_replaces_case_variants_without_losing_cookie(self):
        request = SimpleNamespace(
            url="https://fenxi.zgzcw.com/4665769/bjop",
            headers={"x-forwarded-for": "9.9.9.9", "X-Forwarded-For": "8.8.4.4",
                     "cookie": "session=fixture", "accept": "text/html"},
        )
        with patch("collector.random_forwarded_for") as random_header:
            headers = collector.browser_request_headers(request, "1.1.1.1")
        self.assertEqual(
            [(key, value) for key, value in headers.items() if key.lower() == "x-forwarded-for"],
            [("X-Forwarded-For", "1.1.1.1")],
        )
        self.assertEqual(headers["cookie"], "session=fixture")
        self.assertEqual(headers["accept"], "text/html")
        self.assertEqual(request.headers["x-forwarded-for"], "9.9.9.9")
        random_header.assert_not_called()

    def test_page_redirects_and_ajax_keep_one_header_then_next_render_changes_it(self):
        sent = []
        with patch("playwright.sync_api.sync_playwright") as playwright, \
                patch("collector.random_forwarded_for", side_effect=["8.8.8.8", "1.1.1.1"]) as random_header, \
                patch.object(self.client, "throttle"):
            runtime = playwright.return_value.start.return_value
            context = runtime.chromium.launch.return_value.new_context.return_value
            page = context.new_page.return_value
            page.inner_text.return_value = "已加载比赛盘口数据"
            page.content.return_value = "<table><tr><td>盘口数据</td></tr></table>"

            def navigate(url, **_):
                route_request = context.route.call_args.args[1]
                batch = []
                for resource_type, suffix in (("document", ""), ("document", "?redirect=1"),
                                              ("xhr", "?data=1"), ("fetch", "?data=2"),
                                              ("script", "?script=1")):
                    route = Mock()
                    route.request = SimpleNamespace(
                        url=url + suffix, resource_type=resource_type,
                        frame=page.main_frame,
                        headers={"cookie": "session=fixture", "x-forwarded-for": "9.9.9.9"},
                    )
                    route_request(route)
                    route.abort.assert_not_called()
                    batch.append(route.continue_.call_args.kwargs["headers"])
                sent.append(batch)
                return SimpleNamespace(status=200)

            page.goto.side_effect = navigate
            self.client.render("https://fenxi.zgzcw.com/4665769/bjop")
            self.client.render("https://fenxi.zgzcw.com/4665769/ypdb")
            self.assertEqual(self.client._navigation_requests, 2)

        self.assertEqual(random_header.call_count, 2)
        self.assertEqual([[headers["X-Forwarded-For"] for headers in batch] for batch in sent],
                         [["8.8.8.8"] * 5, ["1.1.1.1"] * 5])
        for batch in sent:
            for headers in batch:
                self.assertEqual(headers["cookie"], "session=fixture")
                self.assertEqual(sum(key.lower() == "x-forwarded-for" for key in headers), 1)

    def test_cp_iframe_documents_do_not_block_the_cp_api_host(self):
        with patch("playwright.sync_api.sync_playwright") as playwright, \
                patch.object(self.client, "throttle") as throttle:
            context = playwright.return_value.start.return_value.chromium.launch.return_value.new_context.return_value
            page = context.new_page.return_value
            self.client._session(None)
            self.client._page_forwarded_for = "8.8.8.8"
            route_request = context.route.call_args.args[1]
            main = Mock()
            main.request = SimpleNamespace(url="https://fenxi.zgzcw.com/4665769/bjop", resource_type="document",
                                           frame=page.main_frame, headers={})
            route_request(main)
            main.continue_.assert_called_once()
            for number in range(8):
                iframe = Mock()
                iframe.request = SimpleNamespace(
                    url=f"https://cp.zgzcw.com/login/iframe?instance={number}", resource_type="document",
                    frame=SimpleNamespace(parent_frame=page.main_frame), headers={"cookie": "session=fixture"})
                route_request(iframe)
                iframe.abort.assert_not_called()
                self.assertEqual(iframe.continue_.call_args.kwargs["headers"]["X-Forwarded-For"], "8.8.8.8")
                self.assertEqual(iframe.continue_.call_args.kwargs["headers"]["cookie"], "session=fixture")
            self.assertEqual(self.client._navigation_requests, 1)
            self.assertEqual(self.client.blocked_hosts, set())
            self.assertEqual(throttle.call_count, 9)
            # The following match's normal CP API request must still be tried.
            url = "https://cp.zgzcw.com/ssc/lottery/jcsp/getjcspbymatchId.jsp"
            with patch.object(self.client, "http", return_value='{"ok":true}') as http:
                self.assertEqual(self.client.api(url, form={"matchId": "9000133"}), {"ok": True})
            http.assert_called_once_with(url, form={"matchId": "9000133"})

    def test_fifth_main_frame_navigation_still_blocks_a_real_loop(self):
        with patch("playwright.sync_api.sync_playwright") as playwright, \
                patch.object(self.client, "throttle") as throttle:
            context = playwright.return_value.start.return_value.chromium.launch.return_value.new_context.return_value
            page = context.new_page.return_value
            self.client._session(None)
            self.client._page_forwarded_for = "8.8.8.8"
            route_request = context.route.call_args.args[1]
            for number in range(1, 6):
                route = Mock()
                route.request = SimpleNamespace(
                    url=f"https://fenxi.zgzcw.com/4665769/bjop?redirect={number}",
                    resource_type="document", frame=page.main_frame, headers={})
                route_request(route)
                if number <= 4:
                    route.abort.assert_not_called()
                    route.continue_.assert_called_once()
                else:
                    route.abort.assert_called_once()
                    route.continue_.assert_not_called()
            self.assertEqual(self.client._navigation_requests, 5)
            self.assertEqual(self.client.blocked_hosts, {"fenxi.zgzcw.com"})
            self.assertEqual(throttle.call_count, 4)
            with self.assertRaisesRegex(ValueError, "反复验证或重定向"):
                self.client._check_block(page, "https://fenxi.zgzcw.com/4665769/bjop")

    def test_verification_during_loading_blocks_further_requests(self):
        page = Mock()
        page.inner_text.side_effect = ["加载比赛数据", "Access Verification 请求过于频繁"]
        page.wait_for_function.side_effect = TimeoutError("loading was interrupted")
        url = "https://fenxi.zgzcw.com/4665769/bjop"
        with self.assertRaisesRegex(ValueError, "拦截|验证"):
            self.client._wait_ready(page, url)
        with patch("playwright.sync_api.sync_playwright") as playwright:
            with self.assertRaisesRegex(ValueError, "验证"):
                self.client.render(url)
        playwright.assert_not_called()

    def test_blocked_host_never_attempts_another_http_request(self):
        self.client.blocked_hosts.add("fenxi.zgzcw.com")
        with patch.object(self.client, "pace") as pace, \
                patch("collector.urllib.request.build_opener") as opener:
            with self.assertRaisesRegex(ValueError, "验证"):
                self.client.http("https://fenxi.zgzcw.com/4665769/bjop")
        pace.assert_not_called()
        opener.assert_not_called()


if __name__ == "__main__":
    unittest.main()
