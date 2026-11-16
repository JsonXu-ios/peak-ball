"""Offline regression tests for Sporttery JSON history normalization."""
import copy
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from official_history import normalize_official_history


def row(home, away, score="0:0", stamp="2026-11-13"):
    return {"matchDate": stamp, "tournamentShortName": "测试联赛",
            "homeTeamShortName": home, "awayTeamShortName": away,
            "fullCourtGoal": score, "halfTimeGoal": "0:0"}


def section(name, rows):
    return {"statistics": {"teamShortName": name}, "matchList": rows}


def response(value):
    return {"errorCode": "0", "value": value}


def fixtures():
    recent = response({"home": section("阿尔梅勒", [row("对手甲", "阿尔梅勒", "0:2")]),
                       "away": section("海尔蒙特", [row("海尔蒙特", "对手乙", "1:0")])})
    h2h = response(section("阿尔梅勒", [row("海尔蒙特", "阿尔梅勒", "0:0", "2026-09-10")]))
    return recent, h2h


class OfficialHistoryTests(unittest.TestCase):
    def normalize(self, recent, h2h):
        return normalize_official_history(recent, h2h, "阿尔梅勒城", "海尔蒙特", home_alias="阿尔梅勒")

    def test_json_records_keep_orientation_zero_scores_and_verified_aliases(self):
        recent, h2h = fixtures()
        unchanged = copy.deepcopy((recent, h2h))
        normalized = self.normalize(recent, h2h)
        self.assertEqual(normalized["source"], "sporttery_api")
        self.assertEqual(normalized["tables"], [
            [["测试联赛", "", "2026-11-13", "对手甲", "0:2", "阿尔梅勒城", "0:0"]],
            [["测试联赛", "", "2026-11-13", "海尔蒙特", "1:0", "对手乙", "0:0"]],
            [["测试联赛", "", "2026-09-10", "海尔蒙特", "0:0", "阿尔梅勒城", "0:0"]],
        ])
        self.assertEqual((recent, h2h), unchanged)

    def test_recent_goal_counters_accept_numeric_zero_without_inventing_result(self):
        recent, h2h = fixtures()
        sample = recent["value"]["home"]["matchList"][0]
        del sample["fullCourtGoal"]
        sample.update(homeTeamFullCourtGoalCnt=0, awayTeamFullCourtGoalCnt="2")
        self.assertEqual(self.normalize(recent, h2h)["tables"][0][0][4], "0:2")
        sample["fullCourtGoal"] = ""
        with self.assertRaisesRegex(ValueError, "没有有效"):
            self.normalize(recent, h2h)

    def test_api_errors_and_schema_errors_never_become_success(self):
        recent, h2h = fixtures()
        for bad in ({"errorCode": "567", "value": {}}, {"errorCode": "0", "value": []}, None):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                self.normalize(bad, h2h)
            with self.subTest(h2h=bad), self.assertRaises(ValueError):
                self.normalize(recent, bad)
        for bad in ({}, {"matchList": "[]"}, {"statistics": {"teamShortName": "阿尔梅勒"}}):
            damaged = copy.deepcopy(recent)
            damaged["value"]["home"] = bad
            with self.subTest(section=bad), self.assertRaises(ValueError):
                self.normalize(damaged, h2h)

    def test_statistics_and_row_team_identity_must_match(self):
        for path in ("home", "away", "h2h"):
            recent, h2h = fixtures()
            target = h2h["value"] if path == "h2h" else recent["value"][path]
            target["statistics"]["teamShortName"] = "另一个球队"
            with self.subTest(path=path), self.assertRaisesRegex(ValueError, "球队"):
                self.normalize(recent, h2h)
        recent, h2h = fixtures()
        recent["value"]["home"]["matchList"][0]["awayTeamShortName"] = "第三队"
        with self.assertRaisesRegex(ValueError, "目标球队"):
            self.normalize(recent, h2h)
        recent, h2h = fixtures()
        h2h["value"]["matchList"][0]["homeTeamShortName"] = "第三队"
        with self.assertRaisesRegex(ValueError, "交锋记录"):
            self.normalize(recent, h2h)

    def test_invalid_dates_and_empty_scores_are_filtered_but_not_all_invalid(self):
        recent, h2h = fixtures()
        records = recent["value"]["home"]["matchList"]
        records.extend([row("阿尔梅勒", "对手", "", "2026-11-13"),
                        row("阿尔梅勒", "对手", "0:0", "2026-02-30"),
                        row("阿尔梅勒", "对手", "0:0", "11/13/2026"),
                        row("阿尔梅勒", "对手", "99:0", "2026-11-13")])
        self.assertEqual(len(self.normalize(recent, h2h)["tables"][0]), 1)
        records.pop(0)
        with self.assertRaisesRegex(ValueError, "没有有效"):
            self.normalize(recent, h2h)

    def test_empty_head_to_head_is_allowed_only_when_explicit(self):
        recent, _ = fixtures()
        for value in ({"matchList": [], "statistics": {}}, {"matchList": []}, section("阿尔梅勒", [])):
            with self.subTest(value=value):
                self.assertEqual(self.normalize(recent, response(value))["tables"][2], [])
        empty = response({})
        with self.assertRaises(ValueError):
            self.normalize(recent, empty)
        empty["emptyFlag"] = True
        self.assertEqual(self.normalize(recent, empty)["tables"][2], [])
        invalid = response(section("阿尔梅勒", [row("阿尔梅勒", "海尔蒙特", "")]))
        with self.assertRaisesRegex(ValueError, "没有有效"):
            self.normalize(recent, invalid)

    def test_at_least_one_valid_recent_table_is_required(self):
        recent, h2h = fixtures()
        recent["value"]["home"]["matchList"] = []
        self.assertEqual(self.normalize(recent, h2h)["tables"][0], [])
        recent["value"]["away"]["matchList"] = []
        with self.assertRaisesRegex(ValueError, "近期战绩均为空"):
            self.normalize(recent, h2h)

    def test_conflicting_scores_and_ambiguous_aliases_are_rejected(self):
        recent, h2h = fixtures()
        recent["value"]["home"]["matchList"][0].update(homeTeamFullCourtGoalCnt="2", awayTeamFullCourtGoalCnt="0")
        with self.assertRaisesRegex(ValueError, "不一致"):
            self.normalize(recent, h2h)
        recent, h2h = fixtures()
        with self.assertRaisesRegex(ValueError, "别名冲突"):
            normalize_official_history(recent, h2h, "主队", "客队", home_alias="相同", away_alias="相同")


if __name__ == "__main__":
    unittest.main()
