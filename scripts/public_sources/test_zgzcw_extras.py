import copy
import json
from pathlib import Path
import sys
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parent))
from zgzcw_extras import normalize_cp_catalog, normalize_cp_extras


IDENTITY = {"sourceMatchId": "2042775", "cpMatchId": "2042775", "zgzcwMatchId": "4518112"}
PAYLOADS = {
    "europe": [{"MATCHID": "2042775", "SP": '["1.57","4.00","4.60","1.95","3.60","3.50"]',
                "SP_CHANGE": '[["1.95","3.60","3.50","2026-11-15 20:25:05"],["1.91","3.75","3.60","2026-11-15 19:49:06"]]'}],
    "kelly": [{"MATCHID": "2042775", "SP": '["0.95","0.89","0.93"]'}],
    "probability": [{"MATCHID": "2042775", "SP": '["47.65","25.81","26.55"]'}],
    "return": [{"MATCHID": "2042775", "SP": '["","0.93",""]'}],
}


class CpExtrasTests(unittest.TestCase):
    def test_catalog_keeps_only_published_verified_types_and_rejects_ambiguity(self):
        value = normalize_cp_catalog({"europe": {"9": "威*", "12": "New"}, "kelly": {"9": "威*"},
                                      "return": {"12": "New"}, "betting_ratio": {"wsl": "胜平负"}})
        self.assertEqual(value, {"9": {"label": "威*", "kinds": ["europe", "kelly"]},
                                 "12": {"label": "New", "kinds": ["europe", "return"]}})
        for bad in ({}, {"europe": []}, {"europe": {"9": ""}},
                    {"europe": {"9": "Same", "12": "same"}},
                    {"europe": {"9": "威*"}, "kelly": {"9": "Other"}}):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                normalize_cp_catalog(bad)

    def test_unpublished_kinds_are_not_missing_or_silently_read(self):
        row = normalize_cp_extras(copy.deepcopy(PAYLOADS), [IDENTITY], "9", "威*", ["europe", "kelly"])["2042775"]
        self.assertEqual(row["missingKinds"], [])
        self.assertEqual(row["availableKinds"], ["europe", "kelly"])
        self.assertIsNone(row["probabilities"])
        self.assertIsNone(row["returnRatio"])
        self.assertEqual(set(row["rawFields"]), {"europe", "kelly"})
        with self.assertRaises(ValueError):
            normalize_cp_extras(PAYLOADS, [IDENTITY], "9", "威*", ["europe", "betting_ratio"])

    def normalize(self, payloads=None, identities=None):
        return normalize_cp_extras(copy.deepcopy(PAYLOADS) if payloads is None else payloads,
                                   [dict(IDENTITY)] if identities is None else identities, "9")

    def test_real_batch_contract_reads_latest_and_source_time(self):
        result = self.normalize()["2042775"]
        self.assertEqual(result["zgzcwMatchId"], "4518112")
        self.assertEqual(result["opening"], ["1.57", "4.00", "4.60"])
        self.assertEqual(result["latest"], ["1.95", "3.60", "3.50"])
        self.assertEqual(result["kelly"], ["0.95", "0.89", "0.93"])
        self.assertEqual(result["probabilities"], ["47.65", "25.81", "26.55"])
        self.assertEqual(result["returnRatio"], "0.93")
        self.assertEqual(result["updatedAt"], "2026-11-15 20:25:05")
        self.assertEqual(result["quoteHistory"][0]["wdate"], "2026-11-15 19:49:06")
        self.assertEqual(result["rawFields"]["europe"], PAYLOADS["europe"][0])
        self.assertNotIn("openingKelly", result)
        self.assertNotIn("openingAt", result)

    def test_old_ids_do_not_join_current_matches_or_shift_positions(self):
        payloads = copy.deepcopy(PAYLOADS)
        for kind in payloads:
            payloads[kind].insert(0, {"MATCHID": "2042752", "SP": "old unneeded data"})
        result = self.normalize(payloads)
        self.assertEqual(list(result), ["2042775"])
        self.assertEqual(result["2042775"]["kelly"], ["0.95", "0.89", "0.93"])

    def test_changed_euro_snapshot_gets_no_unrelated_history_timestamp(self):
        payloads = copy.deepcopy(PAYLOADS)
        payloads["europe"][0]["SP"] = '["1.57","4","4.6","1.96","3.6","3.5"]'
        result = self.normalize(payloads)["2042775"]
        self.assertIsNone(result["updatedAt"])
        self.assertTrue(result["fieldWarnings"])

    def test_no_history_does_not_invent_time(self):
        payloads = copy.deepcopy(PAYLOADS)
        del payloads["europe"][0]["SP_CHANGE"]
        self.assertIsNone(self.normalize(payloads)["2042775"]["updatedAt"])

    def test_missing_rows_stay_missing_and_price_pairing_is_required(self):
        payloads = copy.deepcopy(PAYLOADS)
        payloads["kelly"] = []
        del payloads["return"]
        result = self.normalize(payloads)["2042775"]
        self.assertIsNone(result["kelly"])
        self.assertIsNone(result["returnRatio"])
        self.assertEqual(result["missingKinds"], ["kelly", "return"])
        payloads["europe"] = []
        self.assertEqual(self.normalize(payloads), {})
        del payloads["europe"]
        with self.assertRaises(ValueError):
            self.normalize(payloads)

    def test_duplicate_or_mismatched_identifiers_fail(self):
        payloads = copy.deepcopy(PAYLOADS)
        payloads["kelly"] *= 2
        with self.assertRaises(ValueError):
            self.normalize(payloads)
        with self.assertRaises(ValueError):
            self.normalize(identities=[IDENTITY, IDENTITY])
        with self.assertRaises(ValueError):
            self.normalize(identities=[dict(IDENTITY, cpMatchId="4518112")])

    def test_invalid_shapes_numbers_and_times_fail_without_eval(self):
        cases = [("europe", '["1","2","3","2026-11-15 10:00:00"]'),
                 ("kelly", '["NaN","0.9","0.9"]'),
                 ("probability", '["0.4","0.3","0.3"]'),
                 ("return", '["0.9","",""]'),
                 ("return", '["","0",""]'),
                 ("kelly", '__import__("os")')]
        for kind, bad in cases:
            with self.subTest(kind=kind, bad=bad):
                payloads = copy.deepcopy(PAYLOADS)
                payloads[kind][0]["SP"] = bad
                with self.assertRaises(ValueError):
                    self.normalize(payloads)
        payloads = copy.deepcopy(PAYLOADS)
        payloads["europe"][0]["SP_CHANGE"] = json.dumps([["1.95", "3.6", "3.5", "2026-99-15 12:00:00"]])
        with self.assertRaises(ValueError):
            self.normalize(payloads)

    def test_company_zero_and_raw_fields_are_independent_copies(self):
        payloads = copy.deepcopy(PAYLOADS)
        result = normalize_cp_extras(payloads, [IDENTITY], "0", "平均值")["2042775"]
        self.assertEqual(result["sourceCompanyId"], "0")
        result["rawFields"]["kelly"]["SP"] = "changed"
        self.assertEqual(payloads, PAYLOADS)


if __name__ == "__main__":
    unittest.main()
