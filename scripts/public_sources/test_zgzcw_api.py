"""Pure source-schema and identity regressions, based on captured API fields."""
import unittest
from copy import deepcopy
from datetime import datetime, timezone, timedelta

from zgzcw_api import (
    asian_line, match_index, normalize_euro_changes, normalize_market_index,
    normalize_markets,
)


def index_fixture():
    # Actual field layout and values from the 2026-11-15 PLZX response.
    return [{
        "MATCH_TIME_INTEGER": 1794729600, "SOURCE_GUEST_ID": 10908,
        "CC_ID": "周日004", "SOURCE_LEAGUE_ID": 575, "RANK_FLAG": 0,
        "MATCH_STATUS": 2, "GUEST_NAME": "川崎前锋", "HOST_NAME": "柏太阳神",
        "COLOR": "#00ba5d", "SOURCE_MATCH_ID": 4665765,
        "LEAGUE_NAME_SIMPLY": "日联杯", "SOURCE_HOST_ID": 10380,
        "GUEST_GOAL": 3, "ID": 4665765, "MATCH_TIME": "2026-11-15 16:00:00",
        "TYPE": 2, "HOST_GOAL": 1,
        "listOdds": [{
            "FIRST_LOST": 3.4, "SAME": 3.9, "BIG": 0.82, "FIRST_SAME": 3.8,
            "FIRST_HOST": 0.95, "LOST": 3.25, "SOURCE_COMPANY_ID": 2,
            "GUEST": 0.85, "FIRST_BIG": 0.77, "HOST": 0.95,
            "COMPANY_NAME": "36*", "SMALL": 0.97, "FIRST_WIN": 1.9,
            "FIRST_HANDICAP": -0.5, "FIRST_GUEST": 0.85,
            "SOURCE_MATCH_ID": 4665765, "DW_HANDICAP": 3.25,
            "HANDICAP": -0.5, "DW_FIRST_HANDICAP": 3,
            "FIRST_SMALL": 1.02, "WIN": 1.9,
        }],
    }]


def vote_fixture():
    return {"matchNum": "周日004", "businessDate": "2026-11-15",
            "homeTeamAllName": "柏太阳神", "awayTeamAllName": "川崎前锋"}


def set_time(row, stamp):
    row["MATCH_TIME"] = stamp
    row["MATCH_TIME_INTEGER"] = int(datetime.fromisoformat(stamp).replace(
        tzinfo=timezone(timedelta(hours=8))).timestamp())


def changes_fixture():
    newer = {"arq": "半球", "alost": "0.78", "samech": "0", "arq_num": "-0.50",
             "returnPer": "0.94", "same": "3.80", "wdate1": "1794729358000",
             "lostch": "2", "lost": "3.72", "awin": "1.07", "win": "1.90",
             "wdate": "2026-11-15 15:55:58", "cid": "0", "winch": "0"}
    older = dict(newer, win="1.89", wdate="2026-11-15 15:00:00")
    return {"company5List": [{"attribute": {"name": "平均欧赔", "cid": "0"},
                              "c": [{"attribute": newer, "value": ""},
                                    {"attribute": older, "value": ""}]}]}


class ZgzcwApiTests(unittest.TestCase):
    def test_index_preserves_original_fields_without_mutating_input(self):
        raw = index_fixture()
        result = normalize_market_index(raw)
        self.assertEqual(result, raw)
        result[0]["listOdds"][0]["WIN"] = 99
        self.assertEqual(raw[0]["listOdds"][0]["WIN"], 1.9)
        self.assertEqual(normalize_market_index([]), [])

    def test_bad_id_date_number_and_list_are_rejected(self):
        for key, bad in (("ID", "not-an-id"), ("CC_ID", ""),
                         ("MATCH_TIME", "2026-02-30 16:00:00"),
                         ("HOST_NAME", ""), ("listOdds", {}),
                         ("MATCH_TIME_INTEGER", 1)):
            raw = index_fixture()
            raw[0][key] = bad
            with self.subTest(key=key), self.assertRaises(ValueError):
                normalize_market_index(raw)
        with self.assertRaises(ValueError):
            normalize_market_index({"matches": []})

    def test_cross_match_quote_and_duplicate_company_do_not_mix(self):
        raw = index_fixture()
        raw[0]["listOdds"][0]["SOURCE_MATCH_ID"] = 99
        with self.assertRaisesRegex(ValueError, "其他比赛"):
            normalize_market_index(raw)
        raw = index_fixture()
        raw[0]["listOdds"].append(deepcopy(raw[0]["listOdds"][0]))
        with self.assertRaisesRegex(ValueError, "重复公司"):
            normalize_market_index(raw)
        raw = index_fixture() * 2
        with self.assertRaisesRegex(ValueError, "重复比赛"):
            normalize_market_index(raw)

    def test_mapping_requires_oriented_names_number_and_time(self):
        rows, vote = normalize_market_index(index_fixture()), vote_fixture()
        match = match_index(rows, vote, "2026-11-15 16:00:00")
        self.assertEqual(match["ID"], 4665765)
        for changes in ({"homeTeamAllName": "川崎前锋", "awayTeamAllName": "柏太阳神"},
                        {"matchNum": "周日005"}, {"homeTeamAllName": "柏太阳"}):
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                match_index(rows, dict(vote, **changes), "2026-11-15 16:00:00")
        with self.assertRaises(ValueError):
            match_index(rows, vote, "2026-11-15 16:15:01")
        self.assertEqual(match_index(rows, vote, "2026-11-15 16:15:00")["ID"], 4665765)

    def test_only_explicit_verified_aliases_are_accepted(self):
        rows, vote = index_fixture(), vote_fixture()
        vote["awayTeamAllName"] = "川崎"
        with self.assertRaises(ValueError):
            match_index(rows, vote, "2026-11-15 16:00:00")
        for extra in ({"_sourceAway": "川崎前锋"}, {"_awayAliases": ["川崎前锋"]}):
            self.assertEqual(match_index(rows, dict(vote, **extra), "2026-11-15 16:00:00")["ID"], 4665765)
        with self.assertRaises(ValueError):
            match_index(rows, dict(vote, _awayAliases="川崎前锋"), "2026-11-15 16:00:00")

    def test_source_schedule_provides_kickoff_without_sporttery_head(self):
        vote = vote_fixture()
        vote["awayTeamAllName"] = "川崎前锋足球俱乐部"
        vote["awayTeamAbbName"] = "川崎前锋"
        self.assertEqual(match_index(index_fixture(), vote)["MATCH_TIME"], "2026-11-15 16:00:00")
        with self.assertRaises(ValueError):
            match_index(index_fixture(), dict(vote, matchNum="周日099"))
        with self.assertRaises(ValueError):
            match_index(index_fixture(), dict(vote, businessDate="2026-11-17"))

    def test_overnight_fixtures_still_belong_to_business_day(self):
        rows = index_fixture()
        set_time(rows[0], "2026-11-16 03:45:00")
        self.assertEqual(match_index(rows, vote_fixture(), "2026-11-16 03:45:00")["ID"], 4665765)
        with self.assertRaisesRegex(ValueError, "竞彩日"):
            match_index(rows, vote_fixture(), "2026-11-17 03:45:00")

    def test_manual_mapping_cannot_bypass_participants_or_timestamp(self):
        rows, vote = index_fixture(), vote_fixture()
        self.assertEqual(match_index(rows, vote, "2026-11-15 16:00:00", "4665765")["ID"], 4665765)
        for supplied in ("99", "4665765"):
            with self.subTest(supplied=supplied), self.assertRaises(ValueError):
                match_index(rows, dict(vote, awayTeamAllName="别队"), "2026-11-15 16:00:00", supplied)

    def test_ambiguous_schedule_requires_explicit_valid_id(self):
        rows = index_fixture()
        alternate = deepcopy(rows[0])
        alternate["ID"] = alternate["SOURCE_MATCH_ID"] = 12345
        alternate["listOdds"][0]["SOURCE_MATCH_ID"] = 12345
        rows.append(alternate)
        with self.assertRaisesRegex(ValueError, "唯一"):
            match_index(rows, vote_fixture(), "2026-11-15 16:00:00")
        self.assertEqual(match_index(rows, vote_fixture(), "2026-11-15 16:00:00", "4665765")["ID"], 4665765)

    def test_all_markets_use_exact_source_columns_and_keep_unknowns_empty(self):
        result = normalize_markets(index_fixture()[0])
        self.assertEqual(result["euro"][0]["opening"], ["1.9", "3.8", "3.4"])
        self.assertEqual(result["euro"][0]["latest"], ["1.9", "3.9", "3.25"])
        self.assertEqual(result["asian"][0]["opening"], ["0.95", "半球", "0.85"])
        self.assertEqual(result["totals"][0]["opening"], ["0.77", "3", "1.02"])
        self.assertEqual(result["totals"][0]["latest"], ["0.82", "3.25", "0.97"])
        for rows in result.values():
            self.assertEqual(rows[0]["sourceCompanyId"], "2")
            for key in ("kelly", "openingKelly", "probabilities", "returnRatio"):
                self.assertIsNone(rows[0][key])

    def test_handicap_sign_quarters_and_zero_are_preserved(self):
        expected = {"-0.75": "半球/一球", "0.75": "受半球/一球", "-0.5": "半球",
                    "0.5": "受半球", "0": "平手", "-1.25": "一球/球半",
                    "-3.25": "三球/三球半", "0.25": "受平手/半球"}
        for source, text in expected.items():
            self.assertEqual(asian_line(source), text)
        for value in (None, "NaN", "Infinity", "0.3", True):
            self.assertIsNone(asian_line(value))

    def test_missing_or_invalid_prices_do_not_become_zero_or_other_phase(self):
        row = index_fixture()[0]
        quote = row["listOdds"][0]
        for key in ("FIRST_HOST", "FIRST_HANDICAP", "FIRST_GUEST", "HOST", "HANDICAP", "GUEST"):
            quote.pop(key)
        quote["FIRST_WIN"] = None
        self.assertEqual(normalize_markets(row)["asian"], [])
        self.assertEqual(normalize_markets(row)["euro"][0]["opening"], [])
        self.assertEqual(normalize_markets(row)["euro"][0]["latest"], ["1.9", "3.9", "3.25"])
        quote["WIN"] = float("nan")
        self.assertEqual(normalize_markets(row)["euro"], [])

    def test_published_one_point_zero_price_is_preserved(self):
        row = index_fixture()[0]
        row["listOdds"][0].update(FIRST_WIN=1, WIN=1, FIRST_SAME=16, SAME=14, FIRST_LOST=85, LOST=75)
        market = normalize_markets(row)["euro"][0]
        self.assertEqual(market["opening"], ["1", "16", "85"])
        self.assertEqual(market["latest"], ["1", "14", "75"])

    def test_change_feed_is_chronological_but_never_fabricates_opening(self):
        raw = changes_fixture()
        result = normalize_euro_changes(raw)
        row = result[0]
        self.assertEqual(row["latest"], ["1.9", "3.8", "3.72"])
        self.assertEqual(row["historyScope"], "recent")
        self.assertEqual(row["opening"], [])
        self.assertIsNone(row["openingKelly"])
        self.assertEqual(row["returnRatio"], "0.94")
        self.assertEqual(row["quoteHistory"][0]["wdate"], "2026-11-15 15:00:00")
        self.assertEqual(row["quoteHistory"][-1], raw["company5List"][0]["c"][0]["attribute"])
        self.assertEqual(row["quoteHistory"][-1]["arq_num"], "-0.50")
        row["quoteHistory"][-1]["win"] = "99"
        self.assertEqual(raw["company5List"][0]["c"][0]["attribute"]["win"], "1.90")

    def test_changes_skip_empty_or_invalid_quotes_and_reject_company_mixing(self):
        raw = changes_fixture()
        company = raw["company5List"][0]
        company["c"][0]["attribute"]["win"] = "0"
        self.assertEqual(normalize_euro_changes(raw)[0]["latest"][0], "1.89")
        company["c"][1]["attribute"]["wdate"] = "invalid"
        self.assertEqual(normalize_euro_changes(raw), [])
        company["c"][0]["attribute"]["cid"] = "9"
        with self.assertRaisesRegex(ValueError, "公司ID"):
            normalize_euro_changes(raw)

    def test_different_company_id_namespaces_are_retained(self):
        source = index_fixture()[0]
        source["listOdds"][0].update(SOURCE_COMPANY_ID=22, COMPANY_NAME="平*")
        changes = changes_fixture()
        changes["company5List"][0]["attribute"] = {"cid": "106", "name": "平*"}
        for item in changes["company5List"][0]["c"]:
            item["attribute"]["cid"] = "106"
        first = normalize_markets(source)["euro"][0]
        second = normalize_euro_changes(changes)[0]
        self.assertEqual(first["sourceCompanyId"], "22")
        self.assertEqual(second["sourceCompanyId"], "106")
        self.assertNotEqual(first["source"], second["source"])
        self.assertEqual(first["sourceLabel"], second["sourceLabel"])


if __name__ == "__main__":
    unittest.main()
