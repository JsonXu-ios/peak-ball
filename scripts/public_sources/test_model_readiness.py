"""Only complete, timestamped pre-match inputs may reach the isolated model."""
import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))

from core import connect, save_match, snapshot
from models import MODEL_KINDS, enrich, projection
from source_policy import SOURCE_POLICY


BEFORE = "2026-11-15T04:59:00+00:00"
KICKOFF = "2026-11-15T05:00:00+00:00"
AFTER = "2026-11-15T05:01:00+00:00"


def part_fixture(kind):
    if kind == "bonus":
        return {
            "spf": {"opening": [2.3, 3.0, 2.69], "latest": [2.33, 2.98, 2.67]},
            "drawScores": {"0:0": 10, "1:1": 6, "2:2": 13, "3:3": 50},
            "totalGoals": [10, 4.5, 3.1, 3.2, 5.0, 9.0, 20, 30],
            "history": {"hadList": [{"h": "2.3", "d": "3.0", "a": "2.69",
                                      "updateDate": "2026-11-15", "updateTime": "12:50:00"}]},
        }
    if kind == "history":
        return {"tables": [[["日职", "", "26-11-01", "町田泽维亚", "1:1", "神户胜利船", "0:0"]]]}
    values = {"euro": ["2.33", "3.0", "2.7"],
              "asian": ["0.91", "平手", "0.93"],
              "totals": ["0.95", "2.5球", "0.91"]}[kind]
    return [{"sourceLabel": "36*", "opening": values, "latest": values}]


class ModelReadinessTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.db = connect(Path(self.folder.name) / "readiness.sqlite3")
        self.addCleanup(self.db.close)
        executable = Path(self.folder.name) / "fixture-model.exe"
        executable.write_bytes(b"fixture model; never executed")
        environment = patch.dict("os.environ", {"PUBLIC_SOURCE_MODEL": str(executable)})
        environment.start()
        self.addCleanup(environment.stop)
        self.match = self.make_match("2042767")

    def make_match(self, official_id):
        key = save_match(self.db, {
            "date": "2026-11-15", "officialId": official_id,
            "home": "町田泽维亚", "away": "神户胜利船", "kickoff": "2026-11-15 13:00:00",
            "lotteryNumber": "周日001", "zgzcwId": "4665767",
        })
        snapshot(self.db, key, "match_head", "zgzcw", "fixture", {},
                 {"kickoff": "2026-11-15 13:00:00"}, BEFORE, {"sourcePolicy": SOURCE_POLICY})
        return dict(self.db.execute("SELECT * FROM ps_matches WHERE id=?", (key,)).fetchone())

    def add_parts(self, match, kinds, received):
        for kind in kinds:
            snapshot(self.db, match["id"], kind,
                     "zgzcw", "fixture", {"fixture": kind}, part_fixture(kind), received,
                     {"sourcePolicy": SOURCE_POLICY})

    def test_bonus_before_kickoff_and_other_parts_afterwards_cannot_produce_prediction(self):
        self.add_parts(self.match, ["bonus"], BEFORE)
        self.add_parts(self.match, MODEL_KINDS - {"bonus"}, AFTER)
        self.match["models"] = {"state": "ready", "observation": {"score": 0.5}}
        with patch("models.subprocess.run") as run:
            enrich(self.db, [self.match])
        run.assert_not_called()
        result = self.match["models"]
        self.assertEqual(result["state"], "unavailable")
        self.assertEqual(set(result["missingParts"]), MODEL_KINDS - {"bonus"})
        self.assertIn("赛前", result["reason"])
        self.assertNotIn("observation", result)

    def test_model_runs_only_after_all_five_pre_match_parts_exist(self):
        self.add_parts(self.match, ["bonus"], BEFORE)
        self.add_parts(self.match, MODEL_KINDS - {"bonus"}, AFTER)
        with patch("models.subprocess.run") as run:
            run.return_value.stdout = '[{"state":"ready","version":"fixture"}]'
            enrich(self.db, [self.match])
            run.assert_not_called()
            # This emulates importing genuinely earlier captures with their
            # original timestamps, not re-labelling the post-match captures.
            self.add_parts(self.match, MODEL_KINDS - {"bonus"}, BEFORE)
            enrich(self.db, [self.match])
            run.assert_called_once()
            inputs = json.loads(run.call_args.kwargs["input"])["matches"]
            self.assertEqual(len(inputs), 1)
            self.assertEqual(set(inputs[0]["parts"]), MODEL_KINDS)
            self.assertTrue(all(p["received_at"] == BEFORE for p in inputs[0]["provenance"].values()))
            self.assertEqual(self.match["models"]["state"], "ready")

    def test_each_required_part_must_have_a_capture_strictly_before_kickoff(self):
        with patch("models.subprocess.run") as run:
            for i, missing in enumerate(sorted(MODEL_KINDS)):
                match = self.make_match(str(3000000 + i))
                self.add_parts(match, MODEL_KINDS - {missing}, BEFORE)
                self.add_parts(match, [missing], KICKOFF)
                with self.subTest(missing=missing):
                    enrich(self.db, [match])
                    self.assertEqual(match["models"]["state"], "unavailable")
                    self.assertEqual(set(match["models"]["missingParts"]), {missing})
            run.assert_not_called()

    def test_mixed_batch_passes_only_complete_matches_to_the_model(self):
        complete = self.make_match("2042768")
        self.add_parts(complete, MODEL_KINDS, BEFORE)
        self.add_parts(self.match, ["bonus"], BEFORE)
        with patch("models.subprocess.run") as run:
            run.return_value.stdout = '[{"state":"ready","version":"fixture"}]'
            enrich(self.db, [self.match, complete])
        run.assert_called_once()
        inputs = json.loads(run.call_args.kwargs["input"])["matches"]
        self.assertEqual([item["match"]["id"] for item in inputs], [complete["id"]])
        self.assertEqual(self.match["models"]["state"], "unavailable")
        self.assertEqual(complete["models"]["state"], "ready")

    def test_model_identity_comes_only_from_the_accepted_match_head(self):
        self.assertEqual(self.match["zgzcw_id"], "4665767")
        self.assertIsNone(projection(self.db, self.match)["match"]["zgzcw_id"])
        snapshot(self.db, self.match["id"], "match_head", "zgzcw", "fixture", {},
                 {"kickoff": "2026-11-15 13:00:00", "sourceMatchId": "4665768", "home": "町田泽维", "away": "神户胜利"}, BEFORE,
                 {"sourcePolicy": SOURCE_POLICY})
        self.match["zgzcw_id"] = "9999999"
        self.match["source_home"], self.match["source_away"] = "untrusted home", "untrusted away"
        identity = projection(self.db, self.match)["match"]
        self.assertEqual(identity["zgzcw_id"], "4665768")
        self.assertEqual((identity["source_home"], identity["source_away"]), ("町田泽维", "神户胜利"))
        snapshot(self.db, self.match["id"], "match_head", "sporttery", "wrong-provider", {},
                 {"kickoff": "2026-11-15 13:00:00", "sourceMatchId": "1234567", "home": "wrong home", "away": "wrong away"}, BEFORE,
                 {"sourcePolicy": SOURCE_POLICY})
        identity = projection(self.db, self.match)["match"]
        self.assertEqual(identity["zgzcw_id"], "4665768")
        self.assertEqual((identity["source_home"], identity["source_away"]), ("町田泽维", "神户胜利"))
        for source_id in (None, "", "bad", True, "００１", "0", "1" * 30):
            with self.subTest(source_id=source_id):
                snapshot(self.db, self.match["id"], "match_head", "zgzcw", "fixture", {},
                         {"kickoff": "2026-11-15 13:00:00", "sourceMatchId": source_id}, BEFORE,
                         {"sourcePolicy": SOURCE_POLICY})
                self.assertIsNone(projection(self.db, self.match)["match"]["zgzcw_id"])

    def test_cache_changes_with_verified_source_id_but_not_manual_mapping(self):
        self.add_parts(self.match, MODEL_KINDS, BEFORE)
        with patch("models.subprocess.run") as run:
            run.return_value.stdout = '[{"state":"ready","version":"fixture"}]'
            for source_id in ("4665767", "4665768"):
                snapshot(self.db, self.match["id"], "match_head", "zgzcw", "fixture", {},
                         {"kickoff": "2026-11-15 13:00:00", "sourceMatchId": source_id}, BEFORE,
                         {"sourcePolicy": SOURCE_POLICY})
                enrich(self.db, [self.match])
                sent = json.loads(run.call_args.kwargs["input"])["matches"][0]
                self.assertEqual(sent["match"]["zgzcw_id"], source_id)
                previous_calls = run.call_count
                self.match["zgzcw_id"] = "1234567"
                enrich(self.db, [self.match])
                self.assertEqual(run.call_count, previous_calls)
            self.assertEqual(run.call_count, 2)


if __name__ == "__main__":
    unittest.main()
