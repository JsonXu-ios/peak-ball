"""Current source allocation never silently borrows historical source data."""
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
from core import connect, detail, overview, save_match, snapshot
from models import MODEL_KINDS, enrich, projection
from source_policy import SOURCE_POLICY, allows_snapshot, effective_match
from worker import dispatch, import_capture


POLICY = {"sourcePolicy": SOURCE_POLICY, "mode": "api"}
BEFORE = "2026-11-15T04:59:00Z"


class SourcePolicyTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.db = connect(Path(self.folder.name) / "policy.sqlite3")
        self.addCleanup(self.db.close)
        self.key = save_match(self.db, {"date": "2026-11-15", "officialId": "2042767",
                                      "home": "町田泽维亚", "away": "神户胜利船",
                                      "kickoff": "2026-11-15 12:30:00", "lotteryNumber": "周日001"})

    def add(self, kind, *, source=None, metadata=POLICY, value=None, received=BEFORE):
        source = source or ("sporttery" if kind.startswith("support_") else "zgzcw")
        if value is None:
            value = {"kickoff": "2026-11-15 13:00:00"} if kind == "match_head" else {} if kind in {"bonus", "history"} else []
        snapshot(self.db, self.key, kind, source, "fixture", {"raw": kind}, value, received, metadata)

    def stored_match(self):
        return dict(self.db.execute("SELECT * FROM ps_matches WHERE id=?", (self.key,)).fetchone())

    def test_policy_is_explicit_for_every_kind_and_rejects_wrong_source(self):
        for kind, expected in (("support_had", "sporttery"), ("support_hhad", "sporttery"),
                               ("match_head", "zgzcw"), ("bonus", "zgzcw"), ("history", "zgzcw"), ("euro", "zgzcw")):
            self.assertTrue(allows_snapshot(kind, expected, POLICY))
            for missing in ({}, None, "[]", "not-json", {"sourcePolicy": "previous"}):
                self.assertFalse(allows_snapshot(kind, expected, missing))
            self.assertFalse(allows_snapshot(kind, "zgzcw" if expected == "sporttery" else "sporttery", POLICY))

    def test_cross_provider_interfaces_and_pages_cannot_claim_current_policy(self):
        for kind, source, forbidden in (("support_had", "sporttery", "https://fenxi.zgzcw.com/fixture"),
                                         ("support_hhad", "sporttery", "https://plzx.zgzcw.com/fixture"),
                                         ("euro", "zgzcw", "https://webapi.sporttery.cn/bonus.qry"),
                                         ("bonus", "zgzcw", "https://www.sporttery.cn/fixture"),
                                         ("history", "zgzcw", "https://sporttery.cn/fixture")):
            for evidence_key in ("interfaces", "pageFields"):
                with self.subTest(kind=kind, evidence_key=evidence_key):
                    metadata = dict(POLICY, **{evidence_key: [{"url": forbidden}]})
                    self.assertFalse(allows_snapshot(kind, source, metadata))
                    self.assertFalse(allows_snapshot(kind, source, json.dumps(metadata)))
            self.assertTrue(allows_snapshot(kind, source, dict(POLICY, interfaces=[], pageFields=[])))
            self.assertTrue(allows_snapshot(kind, source, dict(POLICY, interfaces=[{"url": "https://example.test/fixture"}])))

    def test_relabelled_mixed_euro_receipt_stays_out_of_all_current_views(self):
        self.add("match_head")
        mixed = dict(POLICY, interfaces=[
            {"url": "https://fenxi.zgzcw.com/json/4665767/bjop"},
            {"url": "https://webapi.sporttery.cn/getFixedBonusV1.qry?matchId=2042767"},
        ], pageFields=[])
        self.add("euro", metadata=mixed,
                 value=[{"sourceLabel": "官方(胜平负)", "openingSource": {"source": "sporttery"}}])
        self.assertNotIn("euro", detail(self.db, self.key)["parts"])
        self.assertIn("euro", detail(self.db, self.key)["missing"])
        self.assertNotIn("euro", overview(self.db)["matches"][0]["parts"])
        self.assertNotIn("euro", projection(self.db, self.stored_match())["parts"])
        self.assertEqual(json.loads(self.db.execute("SELECT acquisition FROM ps_snapshots WHERE kind='euro'").fetchone()[0]), mixed)

    def test_legacy_official_and_mixed_snapshots_remain_raw_but_do_not_fill_parts(self):
        self.add("match_head", source="sporttery", metadata={})
        self.add("bonus", source="sporttery", metadata={})
        self.add("history", source="sporttery", metadata={})
        self.add("support_had", metadata={})
        self.add("euro", metadata={"mode": "api_with_page_fields"},
                 value=[{"sourceLabel": "官方(胜平负)", "openingSource": {"source": "sporttery"}}])
        current = detail(self.db, self.key)
        self.assertEqual(current["parts"], {})
        self.assertEqual(current["provenance"], {})
        self.assertIsNone(current["match"]["kickoff"])
        self.assertIsNone(current["cutoff"]["stopped"])
        self.assertTrue({"bonus", "history", "euro", "support_had", "kickoff"}.issubset(current["missing"]))
        listed = overview(self.db)["matches"][0]
        self.assertEqual(listed["parts"], [])
        self.assertEqual(listed["missing"], current["missing"])
        self.assertEqual(projection(self.db, self.stored_match())["parts"], {})
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0], 5)
        self.assertEqual(json.loads(dispatch(self.db, "raw", {"id": self.key, "snapshotId": 2})["payload"]), {"raw": "bonus"})

    def test_new_allocation_supplies_current_parts_and_source_kickoff(self):
        self.add("match_head")
        for kind in MODEL_KINDS:
            self.add(kind)
        for kind in ("support_had", "support_hhad"):
            self.add(kind, value={"odds": [2, 3, 4], "support": [50, 30, 20]})
        current = detail(self.db, self.key)
        self.assertEqual(current["missing"], [])
        self.assertEqual(current["match"]["kickoff"], "2026-11-15 13:00:00")
        self.assertEqual(self.stored_match()["kickoff"], "2026-11-15 12:30:00")
        model = projection(self.db, self.stored_match())
        self.assertEqual(model["match"]["kickoff"], current["match"]["kickoff"])
        self.assertEqual(set(model["parts"]), MODEL_KINDS)
        self.assertTrue(all(x["source"] == "zgzcw" for x in model["provenance"].values()))
        listed = overview(self.db)["matches"][0]
        self.assertEqual(set(listed["parts"]), set(current["parts"]))
        self.assertEqual(listed["kickoff"], current["match"]["kickoff"])

    def test_failed_new_collection_cannot_reuse_old_wrong_source_or_cache(self):
        self.add("match_head")
        self.add("bonus", source="sporttery", metadata={})
        self.add("history", source="sporttery", metadata={})
        for kind in ("euro", "asian", "totals"):
            self.add(kind)
        executable = Path(self.folder.name) / "model.exe"
        executable.write_bytes(b"never executed")
        self.db.execute("INSERT INTO ps_model_results VALUES(?,?,?,?)", (self.key, "old-fingerprint", '{"state":"ready"}', BEFORE))
        self.db.commit()
        match = self.stored_match()
        match["models"] = {"state": "ready", "observation": {"score": 1}}
        with patch.dict("os.environ", {"PUBLIC_SOURCE_MODEL": str(executable)}), patch("models.subprocess.run") as run:
            enrich(self.db, [match])
        run.assert_not_called()
        self.assertEqual(match["models"]["state"], "unavailable")
        self.assertEqual(set(match["models"]["missingParts"]), {"bonus", "history"})
        self.assertNotIn("observation", match["models"])

    def test_newer_disallowed_snapshot_cannot_override_an_allowed_capture(self):
        self.add("match_head")
        self.add("history", value={"tables": ["accepted"]})
        self.add("history", source="sporttery", value={"tables": ["wrong-source"]})
        self.add("history", metadata={}, value={"tables": ["legacy-import"]})
        self.add("match_head", source="sporttery", value={"kickoff": "2026-11-15 17:00:00"})
        self.assertEqual(detail(self.db, self.key)["parts"]["history"], {"tables": ["accepted"]})
        model = projection(self.db, self.stored_match())
        self.assertEqual(model["parts"]["history"], {"tables": ["accepted"]})
        self.assertEqual(model["match"]["kickoff"], "2026-11-15 13:00:00")

    def test_bad_head_never_restores_legacy_kickoff(self):
        for head in (None, {}, {"kickoff": None}, {"kickoff": "not-a-date"}, {"kickoff": "2026-11-15"}):
            self.assertIsNone(effective_match(self.stored_match(), head)["kickoff"])
        self.assertEqual(effective_match(self.stored_match(), {"kickoff": "2026-11-15T05:00:00Z"})["kickoff"], "2026-11-15 13:00:00")

    def test_legacy_capture_import_keeps_evidence_without_claiming_new_policy(self):
        folder = Path(self.folder.name) / "old-capture"
        folder.mkdir()
        identity = {"kickoffChina": "2026-11-15 13:00:00", "sportteryRequestMatchId": "2042767",
                    "home": "町田泽维亚", "away": "神户胜利船", "lotteryNumber": "周日001", "zgzcwMatchId": "4665767"}
        bonus = {"errorCode": "0", "value": {"oddsHistory": {
            "homeTeamAllName": identity["home"], "awayTeamAllName": identity["away"], "matchId": "2042767",
            "hadList": [{"h": "2", "d": "3", "a": "4", "updateDate": "2026-11-15", "updateTime": "12:00:00"}]}}}
        files = {"normalized-match.json": {"identity": identity}, "fixed-bonus.browser.txt": bonus,
                 "fixed-bonus.dom.json": {"url": "https://webapi.sporttery.cn/fixture", "capturedAt": BEFORE}}
        vote = "町田泽维亚 神户胜利船 胜 50 50% 2.00 40% 10% 平 30 30% 3.00 30% 0% 负 20 20% 4.00 30% -10%"
        for kind in ("support-had", "support-hhad"):
            files[kind + ".dom.json"] = {"tables": [vote], "url": "https://www.sporttery.cn/fixture", "capturedAt": BEFORE}
        row = ["1", "36*", "2", "3", "4", "2", "3", "4", "", "40", "30", "30", ".9", ".9", ".9", ".9"]
        for kind in ("euro", "asian", "totals", "history"):
            files[kind + ".dom.json"] = {"tables": [[row]], "text": "legacy", "url": "https://fenxi.zgzcw.com/fixture", "capturedAt": BEFORE}
        for name, value in files.items():
            (folder / name).write_text(json.dumps(value, ensure_ascii=False), encoding="utf-8")
        result = import_capture(self.db, folder)
        self.assertTrue(result["imported"])
        stored = self.db.execute("SELECT acquisition,payload FROM ps_snapshots").fetchall()
        self.assertEqual(len(stored), 7)
        self.assertTrue(all(row["acquisition"] == "{}" and row["payload"] for row in stored))
        self.assertEqual(detail(self.db, self.key)["parts"], {})
        self.assertEqual(projection(self.db, self.stored_match())["parts"], {})


if __name__ == "__main__":
    unittest.main()
