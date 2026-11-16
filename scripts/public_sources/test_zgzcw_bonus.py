import copy
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
from zgzcw_bonus import SCORE_LABELS, normalize_cp_bonus, normalize_cp_schedule


SCHEDULE = '''<textarea id="responseJson">{issue:"2026-11-15",systemTime:"2026-11-15 20:07:15"}</textarea>
<select id="selectissue"><option selected value="2026-11-15"></option></select>
<table><tr id="tr_2042775" mn="周日010" rq="-1" t="2026-11-15 22:40" m="欧国联">
<td><span title="截止时间:2026-11-15 22:40"></span><span title="比赛时间:2026-11-16 03:45"></span></td>
<td class="wh-4"><a title="西班牙">西班牙</a><em>[1]</em></td>
<td class="wh-6"><em>[4]</em><a>英格兰</a></td>
<td><div id="ch_2042775_22"><em class="rq">-1</em></div></td>
<td newplayid="4518112" oldplayid="0"></td></tr>
<tr id="tr_2042775_bf"><td>expanded scores</td></tr></table>'''
SPF = [["1.61", "3.83", "3.88", "2026-11-15 09:19:52"],
       ["1.81", "3.45", "3.37", "2026-11-15 19:58:51"],
       ["1.83", "3.45", "3.30", "2026-11-15 17:55:51"],
       ["1.61", "3.83", "3.88", "2026-11-15 09:19:51"]]
RQSPF = [["2.86", "3.45", "2", "2026-11-15 09:19:52"],
         ["3.85", "3.35", "1.72", "2026-11-15 19:58:51"]]
SCORES = "10.50 11.00 7.00 18.00 11.50 16.50 36.00 25.00 36.00 90.00 65.00 80.00 28.00 20.00 7.25 9.50 33.00 125.0 16.00 21.00 11.00 45.00 25.00 24.00 90.00 65.00 65.00 350.0 175.0 200.0 65.00"
TOTALS = "20.00 6.75 4.30 3.40 4.40 6.75 11.50 15.00"


class ZgzcwBonusTests(unittest.TestCase):
    def identity(self):
        return normalize_cp_schedule(SCHEDULE, "2026-11-15")[0]

    def bonus(self, **changes):
        args = dict(identity=self.identity(), spf_history=copy.deepcopy(SPF),
                    rqspf_history=copy.deepcopy(RQSPF), score_sp=SCORES, total_sp=TOTALS)
        args.update(changes)
        return normalize_cp_bonus(**args)

    def test_schedule_reads_explicit_cross_source_link_and_actual_kickoff(self):
        row = self.identity()
        self.assertEqual((row["sourceMatchId"], row["zgzcwMatchId"]), ("2042775", "4518112"))
        self.assertEqual(row["kickoff"], "2026-11-16T03:45:00+08:00")
        self.assertEqual(row["cutoffAt"], "2026-11-15T22:40:00+08:00")
        self.assertEqual((row["home"], row["away"], row["goal"]), ("西班牙", "英格兰", "-1"))

    def test_schedule_rejects_wrong_date_missing_link_and_ambiguous_ids(self):
        for html in (SCHEDULE.replace('issue:"2026-11-15"', 'issue:"2026-11-14"'),
                     SCHEDULE.replace('newplayid="4518112"', ''),
                     SCHEDULE.replace('newplayid="4518112"', 'newplayid="0"'),
                     SCHEDULE.replace('</tr>\n<tr id="tr_2042775_bf">', '<td newplayid="999"></td></tr>\n<tr id="tr_2042775_bf">'),
                     SCHEDULE + SCHEDULE):
            with self.subTest(html=html):
                with self.assertRaises(ValueError):
                    normalize_cp_schedule(html, "2026-11-15")

    def test_schedule_does_not_replace_missing_kickoff_with_cutoff(self):
        with self.assertRaises(ValueError):
            normalize_cp_schedule(SCHEDULE.replace('比赛时间:', '其他时间:'), "2026-11-15")
        with self.assertRaises(ValueError):
            normalize_cp_schedule(SCHEDULE.replace('rq="-1"', 'rq="+1"'), "2026-11-15")

    def test_real_response_shapes_preserve_opening_recent_history_and_all_scores(self):
        result = self.bonus()
        self.assertEqual(result["spf"]["opening"], [1.61, 3.83, 3.88])
        self.assertEqual(result["spf"]["latest"], [1.81, 3.45, 3.37])
        self.assertEqual(result["spf"]["openingAt"], "2026-11-15 09:19:52")
        self.assertEqual(result["history"]["hadList"][0]["updateTime"], "09:19:51")
        self.assertEqual(result["history"]["hadList"][-1]["updateTime"], "19:58:51")
        self.assertEqual(result["rqspf"]["goal"], "-1")
        self.assertEqual(result["drawScores"], {"0:0": 20, "1:1": 7.25, "2:2": 9.5, "3:3": 33})
        self.assertEqual(list(result["scoreOdds"]), list(SCORE_LABELS))
        self.assertEqual(len(result["scoreOdds"]), 31)
        self.assertEqual(result["totalGoals"], [20, 6.75, 4.3, 3.4, 4.4, 6.75, 11.5, 15])
        self.assertEqual(result["history"]["crsList"][0]["s00s00"], 20)
        self.assertNotIn("updateDate", result["history"]["crsList"][0])
        self.assertEqual(result["coverage"]["scores"], "latest_only")

    def test_incomplete_or_bad_endpoints_do_not_create_partial_bundle(self):
        for changes in ({"spf_history": None}, {"rqspf_history": []}, {"spf_history": {}},
                        {"spf_history": [[1.8, 3.4, 3.3]]}, {"score_sp": "{}"},
                        {"score_sp": " ".join(SCORES.split()[:-1])}, {"total_sp": None},
                        {"total_sp": TOTALS + " 1.0"}, {"total_sp": TOTALS.replace("20.00", "NaN")},
                        {"total_sp": TOTALS.replace("20.00", "0")},
                        {"spf_history": [[1.8, 3.4, 3.3, "2026-99-15 12:00:00"]]}):
            with self.subTest(changes=changes):
                with self.assertRaises(ValueError):
                    self.bonus(**changes)

    def test_bonus_rejects_mismatched_cp_identity_and_preserves_source_ids(self):
        row = self.identity()
        row["cpMatchId"] = "4518112"
        with self.assertRaises(ValueError):
            self.bonus(identity=row)
        result = self.bonus()
        self.assertEqual(result["sourceMatchId"], "2042775")
        self.assertEqual(result["zgzcwMatchId"], "4518112")

    def test_single_source_opening_is_valid_but_current_vectors_get_no_opening(self):
        result = self.bonus(spf_history=[SPF[0]])
        self.assertTrue(result["history"]["hadList"][0]["isSourceOpening"])
        self.assertEqual(result["spf"]["opening"], result["spf"]["latest"])
        self.assertNotIn("opening", result["history"]["crsList"][0])
        self.assertNotIn("opening", result["history"]["ttgList"][0])


if __name__ == "__main__":
    unittest.main()
