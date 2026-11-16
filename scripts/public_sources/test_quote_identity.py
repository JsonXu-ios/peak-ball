"""Prevent mismatched page metadata from being joined onto an API quote."""
import sys
import unittest
from decimal import Decimal
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from quote_identity import same_prices


class QuoteIdentityTests(unittest.TestCase):
    def test_euro_uses_exact_decimal_values_and_keeps_outcome_order(self):
        self.assertTrue(same_prices([1.9, "3.50", Decimal("4.00")], ["1.90↑", 3.5, 4], "euro"))
        self.assertFalse(same_prices([1.9, 3.5, 4], ["1.9000000000000001", 3.5, 4], "euro"))
        self.assertFalse(same_prices([1.9, 3.5, 4], [1.9, 4, 3.5], "euro"))

    def test_asian_published_abbreviations_are_equivalent(self):
        pairs = [("半球/一球", "半/一"), ("平手/半球", "平/半"),
                 ("一球/球半", "一/球半"), ("球半/两球", "一球半/二球"),
                 ("两球/两球半", "两/两半"), ("两球半/三球", "两半/三"),
                 ("三球/三球半", "三/三半"), ("平手", "平"),
                 ("让半球", "半"), ("受让半球/一球", "受半/一")]
        for first, second in pairs:
            with self.subTest(first=first, second=second):
                self.assertTrue(same_prices(["0.90", first, "1.00"], [0.9, second, 1], "asian"))

    def test_asian_directions_and_side_prices_cannot_change(self):
        self.assertFalse(same_prices([0.9, "受半/一", 1], [0.9, "半球/一球", 1], "asian"))
        self.assertFalse(same_prices([0.9, "受平手", 1], [0.9, "平手", 1], "asian"))
        self.assertFalse(same_prices([0.9, "半球", 1], [1, "半球", 0.9], "asian"))
        self.assertFalse(same_prices([0.9, "半球", 1], [0.91, "半球", 1], "asian"))

    def test_asian_numeric_or_unknown_lines_are_not_guessed(self):
        for line in (0.5, "0.5", "-0.5", "受-0.5", "让步半球", "半一", "一/三", "一/半", "半/一/球半"):
            with self.subTest(line=line):
                self.assertFalse(same_prices([0.9, line, 1], [0.9, line, 1], "asian"))

    def test_totals_split_lines_use_exact_mean(self):
        pairs = [("2.75", "2.5/3球"), (2.25, "2/2.5"),
                 (3.25, "3球/3.5球"), ("２.７５", "2.5／3球"),
                 ("2.50球", 2.5)]
        for first, second in pairs:
            with self.subTest(first=first, second=second):
                self.assertTrue(same_prices([0.9, first, 1], ["0.90", second, "1.0"], "totals"))
        self.assertFalse(same_prices([0.9, 2.75, 1], [0.9, "2.750000001", 1], "totals"))
        self.assertFalse(same_prices([0.9, 2.75, 1], [0.9, "2.7500000000000000000000000001", 1], "totals"))
        self.assertFalse(same_prices([0.9, 2.75, 1], [0.9, "2.5/3.0000000000000000000000000001", 1], "totals"))
        self.assertFalse(same_prices([0.9, "2.5/3", 1], [0.9, "3/3.5", 1], "totals"))

    def test_invalid_totals_are_not_considered_equal(self):
        for line in ("大2.5", "2球.5", "3/2.5", "2.5/2.5", "2/2.5/3", "NaN", "Infinity", -2.5, True):
            with self.subTest(line=line):
                self.assertFalse(same_prices([0.9, line, 1], [0.9, line, 1], "totals"))

    def test_totals_ranges_require_adjacent_half_goal_endpoints(self):
        pairs = [("2/4", 3), ("2/3", 2.5), ("2.25/2.75", 2.5),
                 ("2.6/3.1", 2.85),
                 ("2.5000000000000000000000000001/3.0000000000000000000000000001", 2.75)]
        for line, average in pairs:
            with self.subTest(line=line):
                self.assertFalse(same_prices([0.9, line, 1], [0.9, average, 1], "totals"))
                self.assertFalse(same_prices([0.9, line, 1], [0.9, line, 1], "totals"))

    def test_invalid_prices_or_shapes_never_match(self):
        for value in (None, "", "—", "NaN", float("inf"), True, "1.9%", 0, -1, "1.9junk"):
            with self.subTest(value=value):
                self.assertFalse(same_prices([value, 3, 4], [value, 3, 4], "euro"))
        for row in (None, "123", {0: 1, 1: 2, 2: 3}, [], [1, 2], [1, 2, 3, 4]):
            with self.subTest(row=row):
                self.assertFalse(same_prices(row, row, "euro"))
        self.assertFalse(same_prices([1, 2, 3], [1, 2, 3], "unknown"))


if __name__ == "__main__":
    unittest.main()
