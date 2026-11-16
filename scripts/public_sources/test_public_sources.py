import json
import ipaddress
import sys
import tempfile
import unittest
from unittest.mock import patch
from types import SimpleNamespace
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from core import analyze, connect, detail, normalize_bonus, save_match, snapshot, vote_dom, bookmaker_rows, proxy_config
from worker import dispatch, enqueue, validate_config
from collector import check_url, collect, parse_official_page, parse_mapping_index, BLOCKED, Client, browser_request_headers, random_forwarded_for
from models import projection, enrich
from source_policy import SOURCE_POLICY

POLICY = {"sourcePolicy": SOURCE_POLICY}


def bonus_fixture():
    # Deliberately newest-first: collectors must use timestamps, not array index.
    return {"errorCode": "0", "value": {"isCancel": 0, "oddsHistory": {
        "homeTeamAllName": "日本", "awayTeamAllName": "巴西", "matchId": "other-provider-7",
        "hadList": [{"h": "3.36", "d": "3.65", "a": "1.76", "updateDate": "2026-11-14", "updateTime": "17:42:16"}, {"h": "3.85", "d": "3.60", "a": "1.66", "updateDate": "2026-11-14", "updateTime": "09:40:59"}],
        "hhadList": [], "crsList": [{"s00s00": "24", "s01s01": "8.25", "s02s02": "9.25", "s03s03": "29"}],
        "ttgList": [{"s" + str(i): str(v) for i, v in enumerate([24, 7.8, 4.25, 3.55, 4.15, 6.6, 10.5, 12.5])}], "hafuList": []}}}


class PublicSourceTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.db = connect(Path(self.folder.name) / "public.sqlite3")
        self.key = save_match(self.db, {"date": "2026-11-14", "officialId": "2042753", "home": "日本", "away": "巴西", "kickoff": "2026-11-14 18:15:00", "zgzcwId": "4650133"})

    def add_head(self):
        snapshot(self.db, self.key, "match_head", "zgzcw", "fixture", {},
                 {"kickoff": "2026-11-14 18:15:00"}, "2026-11-14T09:00:00Z", POLICY)

    def tearDown(self):
        self.db.close()
        self.folder.cleanup()

    def test_screenshot_profit_and_heat(self):
        rows = analyze([4.10, 3.95, 1.56], [15, 28, 57], [21, 22, 57])
        self.assertEqual([r["profitRate"] for r in rows], [38.5, -10.6, 11.08])
        self.assertEqual([r["profitAmount"] for r in rows], [19250000, -5300000, 5540000])
        self.assertEqual([r["heatDifference"] for r in rows], [-6, 6, 0])

    def test_missing_votes_never_turn_into_profit(self):
        for shares in ([None, 28, 57], [0, 0, 0], [150, -28, -22]):
            self.assertTrue(all(r["profitRate"] is None for r in analyze([4.1, 3.95, 1.56], shares)))
            self.assertTrue(all(r["profitAmount"] is None for r in analyze([4.1, 3.95, 1.56], shares)))
        self.assertIsNone(analyze([0, 3.95, 1.56], [15, 28, 57])[0]["profitRate"])

    def test_missing_source_probability_is_not_reconstructed_from_odds(self):
        for probabilities in (None, [], [None, None, None], [21, 22]):
            with self.subTest(probabilities=probabilities):
                rows = analyze([4.1, 3.95, 1.56], [15, 28, 57], probabilities)
                self.assertEqual([r["probability"] for r in rows], [None, None, None])
                self.assertEqual([r["heatDifference"] for r in rows], [None, None, None])
                self.assertEqual([r["profitRate"] for r in rows], [38.5, -10.6, 11.08])

    def test_source_probability_preserves_valid_values_and_missing_entries(self):
        rows = analyze([4.1, 3.95, 1.56], [15, 28, 57], [None, 22, 101])
        self.assertEqual([r["probability"] for r in rows], [None, 22, None])
        self.assertEqual([r["heatDifference"] for r in rows], [None, 6, None])
        for probabilities in ([-1, float("nan"), float("inf")], ["invalid", None, ""]):
            with self.subTest(probabilities=probabilities):
                rows = analyze([4.1, 3.95, 1.56], [15, 28, 57], probabilities)
                self.assertTrue(all(row["probability"] is None and row["heatDifference"] is None for row in rows))

    def test_quote_chronology_and_provider_identity(self):
        bonus = normalize_bonus(bonus_fixture())
        self.assertEqual(bonus["spf"]["opening"], [3.85, 3.6, 1.66])
        self.assertEqual(bonus["spf"]["latest"], [3.36, 3.65, 1.76])
        self.assertEqual(bonus["sourceMatchId"], "other-provider-7")
        self.assertEqual(bonus["drawScores"]["2:2"], 9.25)
        self.assertEqual(len(bonus["totalGoals"]), 8)

    def test_cutoff_is_beijing_time_not_machine_timezone(self):
        self.add_head()
        before = detail(self.db, self.key, datetime(2026, 11, 14, 10, 14, 59, tzinfo=timezone.utc))
        at = detail(self.db, self.key, datetime(2026, 11, 14, 10, 15, tzinfo=timezone.utc))
        self.assertFalse(before["cutoff"]["stopped"])
        self.assertTrue(at["cutoff"]["stopped"])

    def test_analysis_uses_paired_votes_not_unrelated_new_quote(self):
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", {}, normalize_bonus(bonus_fixture()), acquisition=POLICY)
        snapshot(self.db, self.key, "support_had", "sporttery", "source", {}, {"odds": [4.1, 3.95, 1.56], "support": [15, 28, 57], "probabilities": [21, 22, 57]}, acquisition=POLICY)
        result = detail(self.db, self.key)
        self.assertEqual(result["analysis"]["spf"]["rows"][0]["profitRate"], 38.5)
        self.assertEqual(result["analysis"]["spf"]["totalStake"], 50000000)
        self.assertEqual(dispatch(self.db, "overview", {"date": "2026-11-14"})["matches"][0]["analysis"], result["analysis"])
        self.assertEqual(result["analysis"]["spf"]["latestQuote"]["latest"], [3.36, 3.65, 1.76])

    def test_analysis_provenance_points_to_each_selected_vote_snapshot(self):
        vote = {"odds": [4.1, 3.95, 1.56], "support": [15, 28, 57], "probabilities": [21, 22, 57], "goal": "-1"}
        snapshot(self.db, self.key, "support_had", "sporttery", "https://webapi.sporttery.cn/old-vote", {}, vote, acquisition=POLICY)
        snapshot(self.db, self.key, "support_hhad", "sporttery", "https://webapi.sporttery.cn/handicap-vote", {}, vote, acquisition=POLICY)
        snapshot(self.db, self.key, "support_had", "sporttery", "https://webapi.sporttery.cn/latest-vote", {}, vote, acquisition=dict(POLICY, mode="api"))
        # A newer quote snapshot must not become the input evidence of an
        # existing vote calculation, just as it must not replace paired odds.
        snapshot(self.db, self.key, "bonus", "zgzcw", "https://cp.zgzcw.com/latest-bonus", {}, normalize_bonus(bonus_fixture()), acquisition=dict(POLICY, mode="api"))
        result = detail(self.db, self.key)
        self.assertEqual(result["analysis"]["spf"]["inputSource"], {
            "snapshotId": 3, "source": "sporttery", "url": "https://webapi.sporttery.cn/latest-vote", "mode": "api"})
        self.assertEqual(result["analysis"]["rqspf"]["inputSource"], {
            "snapshotId": 2, "source": "sporttery", "url": "https://webapi.sporttery.cn/handicap-vote", "mode": "legacy"})
        self.assertEqual(result["analysis"]["spf"]["source"], "derived_from_user_vote")
        self.assertEqual(result["analysis"]["spf"]["basis"], "paired_vote_odds")
        self.assertEqual(result["analysis"]["rqspf"]["goal"], "-1")

    def test_vote_handicap_never_borrows_bonus_handicap(self):
        vote = {"odds": [4.1, 3.95, 1.56], "support": [15, 28, 57]}
        snapshot(self.db, self.key, "support_hhad", "sporttery", "source", {}, vote, acquisition=POLICY)
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", {}, {"rqspf": {"goal": "1", "latest": [2, 3, 4]}}, acquisition=POLICY)
        self.assertIsNone(detail(self.db, self.key)["analysis"]["rqspf"]["goal"])
        for goal in (0, "0", "-1", None):
            with self.subTest(goal=goal):
                snapshot(self.db, self.key, "support_hhad", "sporttery", "source", {}, dict(vote, goal=goal), acquisition=POLICY)
                self.assertEqual(detail(self.db, self.key)["analysis"]["rqspf"]["goal"], goal)

    def test_snapshot_append_and_empty_source_does_not_erase_success(self):
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", bonus_fixture(), normalize_bonus(bonus_fixture()), acquisition=POLICY)
        with self.assertRaises(ValueError):
            normalize_bonus({"errorCode": "0", "value": {"oddsHistory": {}}})
        self.assertEqual(detail(self.db, self.key)["parts"]["bonus"]["spf"]["latest"], [3.36, 3.65, 1.76])
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0], 1)

    def test_mapping_does_not_change_match_identity(self):
        dispatch(self.db, "mapping", {"id": self.key, "zgzcwId": "1234567"})
        self.assertEqual(detail(self.db, self.key)["match"]["official_id"], "2042753")
        self.assertEqual(detail(self.db, self.key)["match"]["zgzcw_id"], "1234567")
        with self.assertRaises(ValueError):
            dispatch(self.db, "mapping", {"id": self.key, "zgzcwId": "../source"})

    def test_raw_requires_matching_match_key(self):
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", {}, normalize_bonus(bonus_fixture()), acquisition=POLICY)
        with self.assertRaises(ValueError):
            dispatch(self.db, "raw", {"id": "another-match", "snapshotId": 1})

    def test_one_active_job_and_explicit_failure(self):
        job = enqueue(self.db, {"date": "2026-11-14", "limit": 1})
        with self.assertRaises(ValueError):
            enqueue(self.db, {"date": "2026-11-14"})
        dispatch(self.db, "fail-job", job)
        self.assertEqual(dispatch(self.db, "overview", {})["jobs"][0]["status"], "failed")
        self.assertEqual(enqueue(self.db, {"date": "2026-11-14"})["id"], 2)

    def test_request_validation_and_source_allowlist(self):
        for payload in ({"date": "2026-02-30"}, {"date": "2026-11-14", "limit": 0}, {"date": "2026-11-14", "limit": True}, {"date": "2026-11-14", "officialId": "1;rm"}):
            with self.assertRaises(ValueError):
                validate_config(payload)
        for url in ("http://fenxi.zgzcw.com/1/bjop", "https://127.0.0.1/", "https://webapi.sporttery.cn.evil.com/", "https://user:secret@fenxi.zgzcw.com/1"):
            with self.assertRaises(ValueError):
                check_url(url)

    def test_only_independent_tables_created(self):
        tables = [r[0] for r in self.db.execute("SELECT name FROM sqlite_master WHERE type='table'")]
        self.assertTrue(tables)
        self.assertTrue(all(name.startswith("ps_") for name in tables))

    def test_dom_support_and_ad_rows(self):
        vote = vote_dom("胜 22591 15% 4.10 21% -6% 正常 平 41916 28% 3.95 22% 6% 负 82704 57% 1.56 57% 0%")
        self.assertEqual(vote["support"], [15, 28, 57])
        row = ['1', '36*', '4.20', '3.75', '1.73', '3.30↓', '3.80', '1.91', '', '27.81', '24.15', '48.04', '0.90', '0.93', '0.92', '0.92', '主客同']
        normalized = bookmaker_rows([[['广告'], row, ['最大值']]], "euro")
        self.assertEqual(len(normalized), 1)
        self.assertEqual(normalized[0]["latest"][0], "3.30")
        self.assertIsNone(normalized[0]["openingKelly"])

    def test_failed_refresh_preserves_existing_quotes_and_finishes_job(self):
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", bonus_fixture(), normalize_bonus(bonus_fixture()), acquisition=POLICY)
        job = enqueue(self.db, {"date": "2026-11-14", "officialId": "2042753", "limit": 1})
        with patch("collector.Client") as client:
            client.return_value.votes.side_effect = ValueError("来源HTTP 567，未取得数据")
            client.return_value.document.side_effect = ValueError("来源HTTP 567，未取得数据")
            client.return_value.api.side_effect = ValueError("来源HTTP 567，未取得数据")
            result = collect(self.db, job["id"])
        self.assertEqual(result["status"], "failed")
        self.assertEqual(result["processed"], 0)
        self.assertGreater(len(result["errors"]), 0)
        client.return_value.document.assert_not_called()
        client.return_value.render.assert_not_called()
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0], 1)
        self.assertEqual(detail(self.db, self.key)["parts"]["bonus"]["spf"]["latest"], [3.36, 3.65, 1.76])

    def test_model_projection_freezes_last_pre_match_snapshot(self):
        self.add_head()
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", {}, {"value": "before"}, "2026-11-14T10:14:59Z", acquisition=POLICY)
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", {}, {"value": "at-kickoff"}, "2026-11-14T10:15:00Z", acquisition=POLICY)
        snapshot(self.db, self.key, "bonus", "zgzcw", "source", {}, {"value": "after"}, "2026-11-14T11:00:00+00:00", acquisition=POLICY)
        match = detail(self.db, self.key)["match"]
        self.assertEqual(detail(self.db, self.key)["parts"]["bonus"]["value"], "after")
        source = projection(self.db, match)
        self.assertEqual(source["parts"]["bonus"]["value"], "before")
        self.assertEqual(source["provenance"]["bonus"]["id"], 2)

    def test_model_cache_is_invalidated_by_mapping_but_not_post_match_refresh(self):
        self.add_head()
        fake = Path(self.folder.name) / "model-test"
        fake.write_bytes(b"test-model")
        snapshot(self.db, self.key, "euro", "zgzcw", "source", {}, [{"sourceLabel": "36*"}], "2026-11-14T09:00:00Z", acquisition=POLICY)
        for kind in ("bonus", "asian", "totals", "history"):
            snapshot(self.db, self.key, kind, "zgzcw", "source", {}, [], "2026-11-14T09:00:00Z", acquisition=POLICY)
        match = detail(self.db, self.key)["match"]
        with patch.dict("os.environ", {"PUBLIC_SOURCE_MODEL": str(fake)}), patch("models.subprocess.run") as run:
            run.return_value.stdout = '[{"state":"ready","version":"test"}]'
            enrich(self.db, [match])
            enrich(self.db, [match])
            self.assertEqual(run.call_count, 1)
            snapshot(self.db, self.key, "euro", "zgzcw", "source", {}, [{"sourceLabel": "36*", "latest": [1, 2, 3]}], "2026-11-14T11:00:00Z", acquisition=POLICY)
            enrich(self.db, [match])
            self.assertEqual(run.call_count, 1)
            dispatch(self.db, "company-mapping", {"label": "36*", "company": "Bet365"})
            enrich(self.db, [match])
            self.assertEqual(run.call_count, 2)

    def test_missing_model_executable_is_visible_without_breaking_data(self):
        match = detail(self.db, self.key)["match"]
        with patch.dict("os.environ", {"PUBLIC_SOURCE_MODEL": str(Path(self.folder.name) / "missing-model")}):
            enrich(self.db, [match])
        self.assertEqual(match["models"]["state"], "unavailable")
        self.assertIn("尚未构建", match["models"]["reason"])
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_model_results").fetchone()[0], 0)

    def test_official_head_supplies_real_overnight_kickoff_and_alias(self):
        raw = '''<div class="m-matchCard-Cen"><div class="u-top">周六003</div><div class="u-middleLf">日本</div><div class="u-middleRt">巴西</div><div class="u-btm">赛季 第5轮 2026-11-15 01:00</div></div>'''
        head, _ = parse_official_page(raw, "周六003")
        self.assertEqual(head["kickoff"], "2026-11-15 01:00:00")
        self.assertEqual(head["home"], "日本")
        with self.assertRaises(ValueError):
            parse_official_page(raw, "周六004")

    def test_mapping_relative_links_verified_aliases_and_orientation(self):
        raw = '''<p>2026年11月14日</p><table><tr><td><a href="http://saishi.zgzcw.com/soccer/team/1">阿尔梅勒</a></td><td><a href="http://saishi.zgzcw.com/soccer/team/2">海尔蒙特</a></td><td><a href="/4551908/bjop">欧赔</a></td></tr></table>'''
        row = {"matchId":"10", "homeTeamAllName":"阿尔梅勒城", "awayTeamAllName":"海尔蒙特", "_sourceHome":"阿尔梅勒"}
        self.assertEqual(parse_mapping_index(raw, [(row,None,"")], "2026-11-14"), {"10":"4551908"})
        row = {"matchId":"11", "homeTeamAllName":"海尔蒙特", "awayTeamAllName":"阿尔梅勒"}
        self.assertEqual(parse_mapping_index(raw, [(row,None,"")], "2026-11-14"), {})
        with self.assertRaises(ValueError):
            parse_mapping_index(raw, [(row,None,"")], "2026-11-13")
        with self.assertRaises(ValueError):
            parse_mapping_index("人机验证 您的请求过于频繁!", [], "2026-11-14")
        self.assertTrue(BLOCKED.search("Access Verification 请输入正确的验证码"))

    def test_successful_collection_uses_sporttery_votes_and_zgzcw_other_sources(self):
        from test_pipeline import CP_ID, FixtureClient, fixture_parsers
        from urllib.parse import urlsplit

        self.db.execute("UPDATE ps_matches SET kickoff=NULL,zgzcw_id=NULL");self.db.commit()
        job=enqueue(self.db,{"date":"2026-11-14","limit":1})
        source = FixtureClient()
        with patch('collector.Client', return_value=source), \
                patch.dict('os.environ', {'PUBLIC_SOURCE_KELLY_LABELS': 'none'}), fixture_parsers(source):
            result=collect(self.db,job['id'])
        self.assertEqual(result['status'],'completed', result['errors'])
        saved = detail(self.db,self.key)
        match=saved['match']
        self.assertEqual(match['kickoff'],'2026-11-14 18:15:00')
        self.assertEqual(match['zgzcw_id'],'4650133')
        self.assertEqual(saved['parts']['euro'][0]['opening'], ['3', '3.6', '2'])
        self.assertEqual(saved['parts']['euro'][0]['latest'], ['3.4', '3.7', '1.8'])
        self.assertEqual(saved['parts']['euro'][0]['openingSource']['sourceCompanyId'], '2')
        self.assertEqual(saved['parts']['euro'][0]['sourceCompanyId'], '8')
        for kind in ('match_head', 'support_had', 'support_hhad'):
            self.assertEqual(saved['provenance'][kind]['acquisition']['mode'], 'api')
        self.assertEqual(saved['provenance']['bonus']['acquisition']['mode'], 'api_with_page_fields')
        self.assertEqual(saved['provenance']['history']['acquisition']['mode'], 'page')
        for kind, provenance in saved['provenance'].items():
            self.assertEqual(provenance['source'], 'sporttery' if kind in ('support_had', 'support_hhad') else 'zgzcw')
            self.assertEqual(provenance['acquisition']['sourcePolicy'], SOURCE_POLICY)
        self.assertEqual(saved['parts']['bonus']['sourceMatchId'], CP_ID)
        self.assertEqual(saved['parts']['history']['tables'][0][0][4], '2:1')
        self.assertEqual(saved['parts']['history']['tables'][2], [])
        # This fixture has complete API euro fields and explicitly disables
        # full Kelly history; no quote page needs to be fetched for it.
        self.assertEqual(set(call.args[0] for call in source.document.call_args_list), {
            'https://cp.zgzcw.com/lottery/jchtplayvsForJsp.action?lotteryId=47&type=jcmini&issue=2026-11-14',
            'https://fenxi.zgzcw.com/4650133/bsls'})
        self.assertTrue(any(urlsplit(call.args[0]).path == '/ssc/lottery/jcsp/getjcspcompany.jsp'
                            for call in source.api.call_args_list))
        self.assertEqual([call.args[1] for call in source.votes.call_args_list], ['HAD', 'HHAD'])
        for transport in (source.api, source.http, source.document):
            self.assertTrue(all(urlsplit(call.args[0]).hostname.endswith('zgzcw.com') for call in transport.call_args_list))
        source.match_info.assert_not_called()
        source.render.assert_not_called()

    def test_real_proxy_file_rotation_and_visible_unconfigured_state(self):
        config=Path(self.folder.name)/'proxies.json'
        config.write_text('["http://127.0.0.1:18001","http://127.0.0.1:18002"]',encoding='utf-8')
        with patch.dict('os.environ',{'PUBLIC_SOURCE_PROXY_FILE':str(config)},clear=True),patch('collector.time.sleep'):
            client=Client()
            self.assertEqual([client.pace(),client.pace(),client.pace()],['http://127.0.0.1:18001','http://127.0.0.1:18002','http://127.0.0.1:18001'])
            self.assertEqual(dispatch(self.db,'overview',{})['network']['proxyCount'],2)
            config.write_text('[]',encoding='utf-8')
            self.assertEqual(proxy_config(),[])
            self.assertFalse(dispatch(self.db,'overview',{})['network']['rotationConfigured'])

    def test_http_requests_send_new_forwarded_header(self):
        with patch.dict('os.environ',{'PUBLIC_SOURCE_PROXIES':'[]'},clear=True),patch('collector.urllib.request.build_opener') as build,patch('collector.random_forwarded_for',side_effect=['8.8.8.8','1.1.1.1']),patch.object(Client,'pace',return_value=None):
            response=build.return_value.open.return_value.__enter__.return_value
            response.read.return_value=b'{}'
            response.headers.get_content_charset.return_value='utf-8'
            client=Client()
            client.http('https://webapi.sporttery.cn/test')
            client.http('https://webapi.sporttery.cn/test')
            sent=[call.args[0].get_header('X-forwarded-for') for call in build.return_value.open.call_args_list]
            self.assertEqual(sent,['8.8.8.8','1.1.1.1'])

    def test_browser_headers_preserve_session_and_skip_unrelated_hosts(self):
        request=SimpleNamespace(url='https://fenxi.zgzcw.com/1/bjop',headers={'cookie':'session=fixture','accept':'text/html'})
        with patch('collector.random_forwarded_for',return_value='8.8.8.8'):
            result=browser_request_headers(request)
            self.assertEqual(result['X-Forwarded-For'],'8.8.8.8')
            self.assertEqual(result['cookie'],'session=fixture')
            request.url='https://unrelated.example/image'
            self.assertNotIn('X-Forwarded-For',browser_request_headers(request))
        self.assertTrue(ipaddress.ip_address(random_forwarded_for()).is_global)

    def test_default_pacing_is_five_to_seven_seconds(self):
        with patch.dict('os.environ',{'PUBLIC_SOURCE_PROXIES':'[]'},clear=True),patch('collector.time.monotonic',return_value=10),patch('collector.time.sleep') as sleep,patch('collector.RANDOM.uniform',return_value=1.25):
            client=Client();client.last=9
            client.pace()
            sleep.assert_called_once_with(5.25)
            state=dispatch(self.db,'overview',{})['network']
            self.assertEqual((state['intervalSeconds'],state['jitterSeconds']),(5,2))
            self.assertTrue(state['forwardedForRandomized'])


if __name__ == "__main__":
    unittest.main()
