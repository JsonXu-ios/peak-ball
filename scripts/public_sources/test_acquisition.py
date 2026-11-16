"""Snapshot provenance and legacy SQLite migration regression tests.

All files are temporary. Existing application databases and services are not
opened by these tests.
"""
import hashlib
import json
import sqlite3
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from core import connect, detail, overview, save_match, snapshot
from source_policy import SOURCE_POLICY


class AcquisitionTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.path = Path(self.folder.name) / "public.sqlite3"

    def database(self):
        db = connect(self.path)
        self.addCleanup(db.close)
        return db

    def match(self, db):
        return save_match(db, {"date": "2026-11-15", "officialId": "2042775", "home": "西班牙", "away": "英格兰", "kickoff": "2026-11-16 03:45:00"})

    def test_legacy_migration_preserves_content_and_does_not_infer_api_from_url(self):
        raw = '{"source": "fixture", "value": 123}'
        digest = hashlib.sha256(raw.encode()).hexdigest()
        with sqlite3.connect(self.path) as old:
            old.executescript("""
                CREATE TABLE ps_matches (
                  id TEXT PRIMARY KEY, business_date TEXT NOT NULL, official_id TEXT NOT NULL,
                  home TEXT NOT NULL, away TEXT NOT NULL, kickoff TEXT, lottery_number TEXT,
                  zgzcw_id TEXT, updated_at TEXT NOT NULL, UNIQUE(business_date, official_id));
                CREATE TABLE ps_snapshots (
                  id INTEGER PRIMARY KEY, match_id TEXT NOT NULL REFERENCES ps_matches(id), kind TEXT NOT NULL,
                  source TEXT NOT NULL, url TEXT NOT NULL, received_at TEXT NOT NULL,
                  source_updated_at TEXT, payload TEXT NOT NULL, normalized TEXT NOT NULL, sha256 TEXT NOT NULL);
            """)
            old.execute("INSERT INTO ps_matches VALUES(?,?,?,?,?,?,?,?,?)", ("legacy-match", "2026-11-15", "2042775", "西班牙", "英格兰", "2026-11-16 03:45:00", "周日010", "4518112", "2026-11-15T05:00:00Z"))
            old.execute("INSERT INTO ps_snapshots VALUES(?,?,?,?,?,?,?,?,?,?)", (17, "legacy-match", "history", "sporttery", "https://webapi.sporttery.cn/history.qry", "2026-11-15T05:00:00Z", None, raw, '{"tables": [[], [], []]}', digest))
        old.close()
        db = self.database()
        columns = {r["name"]: r for r in db.execute("PRAGMA table_info(ps_snapshots)")}
        self.assertEqual(columns["acquisition"]["notnull"], 1)
        saved = db.execute("SELECT * FROM ps_snapshots WHERE id=17").fetchone()
        self.assertEqual((saved["payload"], saved["sha256"], saved["acquisition"]), (raw, digest, "{}"))
        self.assertNotIn("history", detail(db, "legacy-match")["provenance"])
        self.assertEqual(overview(db, "2026-11-15")["matches"][0]["acquisition"], {})
        # Opening the migrated database repeatedly neither duplicates the
        # column nor changes the original capture timestamp/content.
        again = self.database()
        self.assertEqual(again.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0], 1)
        self.assertEqual(again.execute("SELECT received_at FROM ps_snapshots").fetchone()[0], "2026-11-15T05:00:00Z")

    def test_new_api_evidence_round_trips_in_detail_and_overview(self):
        db = self.database()
        key = self.match(db)
        acquisition = {"sourcePolicy": SOURCE_POLICY, "mode": "api", "interfaces": [
            {"url": "https://fenxi.zgzcw.com/fixture/recent", "method": "GET", "transport": "http", "fields": ["homeRecent", "awayRecent"]},
            {"url": "https://fenxi.zgzcw.com/fixture/h2h", "method": "GET", "transport": "browser_response", "fields": ["headToHead"]},
        ], "pageFields": []}
        snapshot(db, key, "history", "zgzcw", acquisition["interfaces"][0]["url"], {"recent": {"value": {}}}, {"tables": [[], [], []]}, "2026-11-15T05:00:00Z", acquisition)
        evidence = detail(db, key)["provenance"]["history"]
        self.assertEqual(evidence["acquisition"], acquisition)
        self.assertEqual(overview(db)["matches"][0]["acquisition"]["history"], acquisition)
        acquisition["interfaces"][0]["transport"] = "changed-by-caller"
        self.assertEqual(detail(db, key)["provenance"]["history"]["acquisition"]["interfaces"][0]["transport"], "http")

    def test_mixed_evidence_keeps_exact_page_fields_and_reason(self):
        db = self.database()
        key = self.match(db)
        acquisition = {"sourcePolicy": SOURCE_POLICY, "mode": "api_with_page_fields", "interfaces": [{"url": "https://fenxi.zgzcw.com/json/4518112/bjop", "method": "GET", "transport": "browser_response", "fields": ["latest"]}],
                       "pageFields": [{"fields": ["openingKelly", "openingKellyTime"], "url": "https://fenxi.zgzcw.com/4518112/bjop/zhishu?company_id=9", "reason": "接口只提供最近变化，不含最早凯利"}]}
        snapshot(db, key, "euro", "zgzcw", acquisition["interfaces"][0]["url"], {"api": [], "page": "fixture"}, [], acquisition=acquisition)
        self.assertEqual(detail(db, key)["provenance"]["euro"]["acquisition"], acquisition)

    def test_latest_snapshot_does_not_borrow_older_acquisition_evidence(self):
        db = self.database()
        key = self.match(db)
        api = {"sourcePolicy": SOURCE_POLICY, "mode": "api", "interfaces": [{"url": "https://example.test/first", "method": "GET", "transport": "http"}], "pageFields": []}
        snapshot(db, key, "history", "zgzcw", "https://example.test/first", "old", {"tables": []}, acquisition=api)
        snapshot(db, key, "history", "zgzcw", "https://example.test/second", "new", {"tables": []}, acquisition={"sourcePolicy": SOURCE_POLICY})
        current = detail(db, key)["provenance"]["history"]
        self.assertEqual(current["id"], 2)
        self.assertEqual(current["acquisition"], {"sourcePolicy": SOURCE_POLICY})
        self.assertEqual(overview(db)["matches"][0]["acquisition"]["history"], {"sourcePolicy": SOURCE_POLICY})
        self.assertEqual(json.loads(db.execute("SELECT acquisition FROM ps_snapshots WHERE id=1").fetchone()[0]), api)

    def test_malformed_metadata_cannot_break_existing_match_details(self):
        db = self.database()
        key = self.match(db)
        snapshot(db, key, "history", "fixture", "source", "raw", {"tables": []})
        for value in ("not-json", "[]", "null"):
            db.execute("UPDATE ps_snapshots SET acquisition=?", (value,))
            db.commit()
            with self.subTest(value=value):
                self.assertNotIn("history", detail(db, key)["provenance"])
        with self.assertRaisesRegex(ValueError, "JSON对象"):
            snapshot(db, key, "history", "fixture", "source", "raw", {}, acquisition=[])
        self.assertEqual(db.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0], 1)


if __name__ == "__main__":
    unittest.main()
