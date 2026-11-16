"""Offline regressions for the audited bsls historical-result fields."""
from copy import deepcopy
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
from zgzcw_history import normalize_zgzcw_history


def team(name, identity):
    return f'<a href="https://saishi.zgzcw.com/soccer/team/2906/{identity}">{name}</a>'


def row(group, identity, day, home, away, score="1:0", half="0:0", hidden=False):
    cells = ["友谊赛", "小组赛", day, team(*home), score, team(*away), half]
    return f'<tr id="{group}_{identity}" style="display:{"none" if hidden else ""}">' + "".join("<td>" + value + "</td>" for value in cells) + "</tr>"


def fixture(home=None, away=None, h2h=None):
    header = '<div class="logoVs"><div class="host-name">' + team("西班牙", 11) + '</div><div class="visit-name">' + team("英格兰", 22) + '</div></div><div class="date">比赛时间：2026-11-16 03:45:00</div>'
    body = header + '<input id="b_Home" value="#home_4518112"><input id="b_Guest" value="#guest_4518112">'
    groups = {
        "Home": home if home is not None else [row("home", 100, "26-11-13", ("捷克", 33), ("西班牙", 11), "0:2", "0:1")],
        "Guest": away if away is not None else [row("guest", 101, "26-11-13", ("英格兰", 22), ("克罗地亚", 44), "2:1", "1:0")],
        "His": h2h if h2h is not None else [row("his", 102, "26-9-27", ("英格兰", 22), ("西班牙", 11), "2:3", "2:1")],
    }
    for group, entries in groups.items():
        body += f'<table id="match_{group}"><thead>' + "".join("<th>" + name + "</th>" for name in ["联赛", "轮次", "时间", "主队", "比分", "客队", "半场"]) + "</thead><tbody>" + "".join(entries) + "</tbody></table>"
    return body


def normalize(raw, **kwargs):
    values = dict(source_match_id="4518112", home="西班牙", away="英格兰", kickoff="2026-11-16 03:45:00")
    values.update(kwargs)
    return normalize_zgzcw_history(raw, **values)


class ZgzcwHistoryTests(unittest.TestCase):
    def test_real_columns_aliases_and_zero_scores(self):
        got = normalize(fixture(), home="西班牙全称", away="英格兰全称", home_alias="西班牙", away_alias="英格兰")
        self.assertEqual(got["tables"][0][0], ["友谊赛", "", "2026-11-13", "捷克", "0:2", "西班牙全称", "0:1"])
        self.assertEqual(got["tables"][2][0][3:6], ["英格兰全称", "2:3", "西班牙全称"])
        self.assertEqual(got["source"], "zgzcw_page_fields")

    def test_verified_source_and_official_short_names_both_identify_real_teams(self):
        cases = [
            ("町田泽维亚", "神户胜利船", ["町田泽维亚", "町田泽维"], ["神户胜利船", "神户胜利"], "町田泽维", "神户胜利"),
            ("马斯特里赫特", "维迪斯", ["马斯特里赫特", "马斯特里"], ["维特斯", "维迪斯"], "马斯特里赫特", "维特斯"),
        ]
        for home, away, home_alias, away_alias, page_home, page_away in cases:
            raw = fixture().replace("西班牙", page_home).replace("英格兰", page_away)
            aliases_before = deepcopy((home_alias, away_alias))
            with self.subTest(home=home, away=away):
                got = normalize(raw, home=home, away=away, home_alias=home_alias, away_alias=away_alias)
                self.assertEqual((got["sourceHome"], got["sourceAway"]), (page_home, page_away))
                self.assertEqual(got["tables"][0][0][5], home)
                self.assertEqual(got["tables"][1][0][3], away)
                self.assertEqual(got["tables"][2][0][3:6], [away, "2:3", home])
                self.assertEqual((home_alias, away_alias), aliases_before)

    def test_aliases_reject_invalid_entries_and_cross_team_names(self):
        invalid = ["", "  ", 42, {"name": "西班牙"}, ("西班牙",), [None], ["西班牙", ""], ["西班牙", 42]]
        for alias in invalid:
            for key in ("home_alias", "away_alias"):
                with self.subTest(key=key, alias=alias), self.assertRaisesRegex(ValueError, "别名"):
                    normalize(fixture(), **{key: alias})
        with self.assertRaisesRegex(ValueError, "明确且不同"):
            normalize(fixture(), home_alias=["西班牙", "英格兰"])

    def test_hidden_history_is_data_but_target_and_future_are_not(self):
        entries = [row("home", 4518112, "26-11-16", ("西班牙", 11), ("英格兰", 22), "1:0"),
                   row("home", 110, "26-11-16", ("西班牙", 11), ("捷克", 33)),
                   row("home", 111, "26-11-17", ("西班牙", 11), ("捷克", 33)),
                   row("home", 112, "26-10-7", ("西班牙", 11), ("捷克", 33), "0:0", hidden=True)]
        got = normalize(fixture(home=entries))
        self.assertEqual(len(got["tables"][0]), 1)
        self.assertEqual(got["tables"][0][0][4], "0:0")
        self.assertEqual(got["discarded"]["Home"]["same_day_or_future"], 2)
        self.assertEqual(got["discarded"]["Home"]["target"], 1)

    def test_invalid_dates_and_scores_cannot_fabricate_results(self):
        entries = [row("home", 100, "26-11-13", ("西班牙", 11), ("捷克", 33)),
                   row("home", 102, "26-2-30", ("西班牙", 11), ("捷克", 33)),
                   row("home", 103, "26-11-12", ("西班牙", 11), ("捷克", 33), "VS"),
                   row("home", 104, "26-11-11", ("西班牙", 11), ("捷克", 33), "")]
        got = normalize(fixture(home=entries))
        self.assertEqual(len(got["tables"][0]), 1)
        with self.assertRaisesRegex(ValueError, "解析错误"):
            normalize(fixture(home=entries[1:]))

    def test_empty_h2h_allowed_but_missing_table_or_all_recent_empty_fail(self):
        self.assertEqual(normalize(fixture(h2h=[]))["tables"][2], [])
        with self.assertRaisesRegex(ValueError, "唯一有效结构"):
            normalize(fixture().replace('id="match_His"', 'id="other"'))
        with self.assertRaisesRegex(ValueError, "双方均无有效"):
            normalize(fixture(home=[], away=[]))

    def test_match_id_names_and_kickoff_are_verified(self):
        for raw in [fixture().replace('value="#home_4518112"', 'value="#home_999"'),
                    fixture().replace('03:45:00', '05:45:00'),
                    fixture().replace('<div class="host-name">' + team("西班牙", 11), '<div class="host-name">' + team("其他队", 11))]:
            with self.subTest(raw=raw[:80]), self.assertRaises(ValueError):
                normalize(raw)

    def test_wrong_group_team_and_name_id_conflicts_fail(self):
        for entry in [row("home", 100, "26-11-13", ("其他", 33), ("英格兰", 22)),
                      row("home", 100, "26-11-13", ("西班牙", 33), ("西班牙", 11)),
                      row("guest", 100, "26-11-13", ("西班牙", 11), ("捷克", 33))]:
            with self.subTest(entry=entry), self.assertRaises(ValueError):
                normalize(fixture(home=[entry]))
        with self.assertRaisesRegex(ValueError, "交锋记录双方"):
            normalize(fixture(h2h=[row("his", 100, "26-11-13", ("西班牙", 11), ("捷克", 33))]))

    def test_duplicates_are_deduplicated_and_conflicts_rejected(self):
        entry = row("home", 100, "26-11-13", ("西班牙", 11), ("捷克", 33))
        self.assertEqual(len(normalize(fixture(home=[entry, entry]))["tables"][0]), 1)
        with self.assertRaisesRegex(ValueError, "冲突"):
            normalize(fixture(home=[entry, entry.replace("1:0", "2:0")]))

    def test_distinct_same_day_friendlies_are_not_conflicting_duplicates(self):
        one = row("home", 100, "26-11-13", ("西班牙", 11), ("捷克", 33), "2:0")
        two = row("home", 101, "26-11-13", ("西班牙", 11), ("捷克", 33), "0:1")
        got = normalize(fixture(home=[one, two]))
        self.assertEqual([entry[4] for entry in got["tables"][0]], ["2:0", "0:1"])

    def test_structure_errors_and_verification_are_not_empty_successes(self):
        for raw in ["<html>Access Verification</html>", fixture().replace("<th>比分</th>", "<th>新增列</th>"), fixture().replace('id="home_100"', 'id="oops_100"')]:
            with self.subTest(raw=raw[:50]), self.assertRaises(ValueError):
                normalize(raw)

    def test_no_input_mutation_and_bad_half_remains_unknown(self):
        raw = fixture(home=[row("home", 100, "2026-11-13", ("西班牙", 11), ("捷克", 33), "0:0", "未知")])
        original = deepcopy(raw)
        self.assertEqual(normalize(raw)["tables"][0][0][6], "")
        self.assertEqual(raw, original)


if __name__ == "__main__":
    unittest.main()
