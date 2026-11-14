import json
import sys
import tempfile
import unittest
from unittest.mock import patch
from datetime import datetime, timezone
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from core import analyze, connect, detail, normalize_bonus, save_match, snapshot, vote_dom, bookmaker_rows
from worker import dispatch, enqueue, validate_config
from collector import check_url, collect
from models import projection, enrich


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

    def tearDown(self):
        self.db.close()
        self.folder.cleanup()

    def test_screenshot_profit_and_heat(self):
        rows = analyze([4.10, 3.95, 1.56], [15, 28, 57], [21, 22, 57])
        self.assertEqual([r["profitRate"] for r in rows], [38.5, -10.6, 11.08])
        self.assertEqual([r["heatDifference"] for r in rows], [-6, 6, 0])

    def test_missing_votes_never_turn_into_profit(self):
        for shares in ([None, 28, 57], [0, 0, 0], [150, -28, -22]):
            self.assertTrue(all(r["profitRate"] is None for r in analyze([4.1, 3.95, 1.56], shares)))
        self.assertIsNone(analyze([0, 3.95, 1.56], [15, 28, 57])[0]["profitRate"])

    def test_quote_chronology_and_provider_identity(self):
        bonus = normalize_bonus(bonus_fixture())
        self.assertEqual(bonus["spf"]["opening"], [3.85, 3.6, 1.66])
        self.assertEqual(bonus["spf"]["latest"], [3.36, 3.65, 1.76])
        self.assertEqual(bonus["sourceMatchId"], "other-provider-7")
        self.assertEqual(bonus["drawScores"]["2:2"], 9.25)
        self.assertEqual(len(bonus["totalGoals"]), 8)

    def test_cutoff_is_beijing_time_not_machine_timezone(self):
        before = detail(self.db, self.key, datetime(2026, 11, 14, 10, 14, 59, tzinfo=timezone.utc))
        at = detail(self.db, self.key, datetime(2026, 11, 14, 10, 15, tzinfo=timezone.utc))
        self.assertFalse(before["cutoff"]["stopped"])
        self.assertTrue(at["cutoff"]["stopped"])

    def test_analysis_uses_paired_votes_not_unrelated_new_quote(self):
        snapshot(self.db, self.key, "bonus", "sporttery", "source", {}, normalize_bonus(bonus_fixture()))
        snapshot(self.db, self.key, "support_had", "sporttery", "source", {}, {"odds": [4.1, 3.95, 1.56], "support": [15, 28, 57], "probabilities": [21, 22, 57]})
        result = detail(self.db, self.key)
        self.assertEqual(result["analysis"]["spf"]["rows"][0]["profitRate"], 38.5)
        self.assertEqual(result["analysis"]["spf"]["latestQuote"]["latest"], [3.36, 3.65, 1.76])

    def test_snapshot_append_and_empty_source_does_not_erase_success(self):
        snapshot(self.db, self.key, "bonus", "sporttery", "source", bonus_fixture(), normalize_bonus(bonus_fixture()))
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
        snapshot(self.db, self.key, "bonus", "sporttery", "source", {}, normalize_bonus(bonus_fixture()))
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
        snapshot(self.db, self.key, "bonus", "sporttery", "source", bonus_fixture(), normalize_bonus(bonus_fixture()))
        job = enqueue(self.db, {"date": "2026-11-14", "officialId": "2042753", "limit": 1})
        with patch("collector.Client") as client:
            client.return_value.votes.side_effect = ValueError("来源HTTP 567，未取得数据")
            client.return_value.document.side_effect = ValueError("来源HTTP 567，未取得数据")
            client.return_value.api.side_effect = ValueError("来源HTTP 567，未取得数据")
            result = collect(self.db, job["id"])
        self.assertEqual(result["status"], "partial")
        self.assertEqual(result["processed"], 1)
        self.assertGreater(len(result["errors"]), 0)
        self.assertEqual(detail(self.db, self.key)["parts"]["bonus"]["spf"]["latest"], [3.36, 3.65, 1.76])

    def test_model_projection_freezes_last_pre_match_snapshot(self):
        snapshot(self.db, self.key, "bonus", "sporttery", "source", {}, {"value": "before"}, "2026-11-14T10:14:59Z")
        snapshot(self.db, self.key, "bonus", "sporttery", "source", {}, {"value": "at-kickoff"}, "2026-11-14T10:15:00Z")
        snapshot(self.db, self.key, "bonus", "sporttery", "source", {}, {"value": "after"}, "2026-11-14T11:00:00+00:00")
        match = detail(self.db, self.key)["match"]
        self.assertEqual(detail(self.db, self.key)["parts"]["bonus"]["value"], "after")
        source = projection(self.db, match)
        self.assertEqual(source["parts"]["bonus"]["value"], "before")
        self.assertEqual(source["provenance"]["bonus"]["id"], 1)

    def test_model_cache_is_invalidated_by_mapping_but_not_post_match_refresh(self):
        fake = Path(self.folder.name) / "model-test"
        fake.write_bytes(b"test-model")
        snapshot(self.db, self.key, "euro", "zgzcw", "source", {}, [{"sourceLabel": "36*"}], "2026-11-14T09:00:00Z")
        match = detail(self.db, self.key)["match"]
        with patch.dict("os.environ", {"PUBLIC_SOURCE_MODEL": str(fake)}), patch("models.subprocess.run") as run:
            run.return_value.stdout = '[{"state":"ready","version":"test"}]'
            enrich(self.db, [match])
            enrich(self.db, [match])
            self.assertEqual(run.call_count, 1)
            snapshot(self.db, self.key, "euro", "zgzcw", "source", {}, [{"sourceLabel": "36*", "latest": [1, 2, 3]}], "2026-11-14T11:00:00Z")
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


if __name__ == "__main__":
    unittest.main()
