"""Batch API and merge regressions; all transport calls are local mocks."""
import unittest
from copy import deepcopy
from unittest.mock import Mock, patch

from cp_extra_collection import CATALOG_ENDPOINT, ENDPOINT, FIELDS, MODEL_COMPANIES, MODEL_KINDS, fetch_extras, merge_extras, owned_fields
from zgzcw_extras import CP_COMPANIES, KINDS, normalize_cp_extras


IDENTITIES = [{"sourceMatchId": "9000133", "cpMatchId": "9000133", "zgzcwMatchId": "4650133"}]
PAYLOADS = {
    "europe": [{"MATCHID": "9000133", "SP": ["3.0", "3.60", "2.0", "3.40", "3.70", "1.80"],
                "SP_CHANGE": [["3.4", "3.7", "1.8", "2026-11-14 17:00:00"]]}],
    "kelly": [{"MATCHID": "9000133", "SP": ["0.91", "0.92", "0.93"]}],
    "probability": [{"MATCHID": "9000133", "SP": ["31", "25", "44"]}],
    "return": [{"MATCHID": "9000133", "SP": ["", "0.94", ""]}],
}


def quote(label="36*"):
    return {"source": "zgzcw_fenxi", "sourceLabel": label, "sourceCompanyId": "8",
            "sourceMatchId": "4650133", "opening": ["2.9", "3.5", "2.1"], "latest": ["3.4", "3.7", "1.8"],
            "kelly": None, "probabilities": None, "returnRatio": None, "openingKelly": None}


def extra(cid="2"):
    return normalize_cp_extras(deepcopy(PAYLOADS), IDENTITIES, cid)["9000133"]


class CpExtraCollectionTests(unittest.TestCase):
    def client(self, failures=None, bodies=None):
        client = Mock()
        client.last_api_transport = "http"
        failures = failures or {}
        bodies = bodies or {}

        def api(url, form=None):
            if url == CATALOG_ENDPOINT:
                self.assertEqual(form, {})
                if "catalog" in failures:
                    raise failures["catalog"]
                return deepcopy(bodies.get("catalog", {kind: CP_COMPANIES for kind in KINDS}))
            self.assertEqual(url, ENDPOINT)
            self.assertEqual(set(form), {"cid", "sptype", "date"})
            self.assertEqual(form["date"], "2026-11-14")
            pair = (form["cid"], form["sptype"])
            if pair in failures:
                raise failures[pair]
            return deepcopy(bodies.get(pair, PAYLOADS[form["sptype"]]))
        client.api.side_effect = api
        return client

    def test_fetch_discovers_once_then_six_required_date_batches_and_retains_receipts(self):
        client, on_error, heartbeat = self.client(), Mock(), Mock()
        result, receipts, catalog = fetch_extras(client, IDENTITIES, "2026-11-14", on_error, heartbeat)
        self.assertEqual(client.api.call_count, 7)
        self.assertEqual(heartbeat.call_count, 7)
        self.assertEqual(catalog["source"], "current_api")
        self.assertEqual({(call.kwargs["form"]["cid"], call.kwargs["form"]["sptype"]) for call in client.api.call_args_list[1:]},
                         {(cid, kind) for cid in MODEL_COMPANIES for kind in MODEL_KINDS})
        self.assertEqual(len(result["9000133"]), 3)
        self.assertTrue(all(not row["missingKinds"] for row in result["9000133"]))
        self.assertEqual(len(receipts), 7)
        self.assertEqual(receipts[0]["url"], CATALOG_ENDPOINT)
        self.assertEqual(receipts[1]["response"], PAYLOADS["europe"])
        self.assertEqual(receipts[0]["method"], "POST")
        self.assertTrue(receipts[0]["receivedAt"])
        on_error.assert_not_called()
        client.document.assert_not_called()
        client.render.assert_not_called()

    def test_europe_failure_excludes_only_its_company_and_never_uses_html(self):
        client, on_error = self.client({("9", "europe"): ValueError("API unavailable")}), Mock()
        result, receipts, _ = fetch_extras(client, IDENTITIES, "2026-11-14", on_error, Mock())
        self.assertEqual({row["sourceCompanyId"] for row in result["9000133"]}, set(MODEL_COMPANIES) - {"9"})
        self.assertEqual(len(receipts), 6)
        self.assertEqual(client.api.call_count, 7)
        self.assertIn("威*:europe", on_error.call_args.args[0])
        client.document.assert_not_called()

    def test_unrequested_invalid_fields_do_not_block_required_euro_and_kelly(self):
        client = self.client({("2", "kelly"): ValueError("unavailable")},
                             {("2", "probability"): [{"MATCHID": "9000133", "SP": ["40", "40", "40"]}]})
        on_error = Mock()
        result, receipts, _ = fetch_extras(client, IDENTITIES, "2026-11-14", on_error, Mock())
        row = next(row for row in result["9000133"] if row["sourceCompanyId"] == "2")
        self.assertIsNone(row["kelly"])
        self.assertIsNone(row["probabilities"])
        self.assertIsNone(row["returnRatio"])
        self.assertEqual(row["missingKinds"], ["kelly"])
        self.assertEqual(len(result["9000133"]), 3)
        self.assertEqual(on_error.call_count, 1)
        self.assertEqual(len(receipts), 6)
        self.assertFalse(any(call.kwargs["form"].get("sptype") in ("probability", "return") for call in client.api.call_args_list))

    def test_invalid_required_kelly_is_missing_without_losing_paired_prices(self):
        client = self.client(bodies={("2", "kelly"): [{"MATCHID": "9000133", "SP": ["bad", "0.9", "0.9"]}]})
        errors = Mock()
        result, _, _ = fetch_extras(client, IDENTITIES, "2026-11-14", errors, Mock())
        row = next(row for row in result["9000133"] if row["sourceCompanyId"] == "2")
        self.assertEqual(row["missingKinds"], ["kelly"])
        self.assertEqual(row["latest"], ["3.40", "3.70", "1.80"])
        errors.assert_called_once()

    def test_bundle_capture_time_is_the_later_quote_or_kelly_response(self):
        stamps = [f"2026-11-14T09:00:0{i}+00:00" for i in range(7)]
        with patch("pipeline.now", side_effect=stamps):
            result, _, _ = fetch_extras(self.client(), IDENTITIES, "2026-11-14", Mock(), Mock())
        row = next(row for row in result["9000133"] if row["sourceCompanyId"] == "2")
        self.assertEqual(row["quoteReceivedAt"], stamps[5])
        self.assertEqual(row["kellyReceivedAt"], stamps[6])
        self.assertEqual(row["receivedAt"], stamps[6])
        main = quote()
        main["latest"][0] = "3.41"
        result = merge_extras([main], [row])[0]
        self.assertEqual(result["cpExtras"]["receivedAt"], stamps[6])
        self.assertEqual(result["cpExtras"]["latest"], row["latest"])
        self.assertEqual(result["cpExtras"]["kelly"], row["kelly"])
        self.assertEqual(result["cpExtras"]["status"], "price_mismatch")
        self.assertEqual(result["latest"], main["latest"])
        self.assertIsNone(result["kelly"])

    def test_missing_company_rows_stay_missing_and_empty_identity_does_not_fetch(self):
        client, errors = self.client(bodies={("2", "europe"): []}), Mock()
        result, _, _ = fetch_extras(client, IDENTITIES, "2026-11-14", errors, Mock())
        self.assertNotIn("2", [row["sourceCompanyId"] for row in result["9000133"]])
        self.assertIn("europe缺项", errors.call_args.args[0])
        empty = self.client()
        self.assertEqual(fetch_extras(empty, [], "2026-11-14", Mock(), Mock()), ({}, [], None))
        empty.api.assert_not_called()

    def test_catalog_does_not_expand_requests_beyond_required_companies_and_types(self):
        body = {kind: dict(CP_COMPANIES) for kind in KINDS}
        body["europe"]["88"] = "New Exact"
        body["return"]["88"] = "New Exact"
        body["betting_ratio"] = {"wsl": "胜平负"}
        client, errors = self.client(bodies={"catalog": body}), Mock()
        result, receipts, catalog = fetch_extras(client, IDENTITIES, "2026-11-14", errors, Mock())
        requested = {(call.kwargs["form"]["cid"], call.kwargs["form"]["sptype"]) for call in client.api.call_args_list[1:]}
        self.assertEqual(requested, {(cid,kind) for cid in MODEL_COMPANIES for kind in MODEL_KINDS})
        self.assertEqual(len(receipts), 7)
        self.assertEqual(owned_fields("36*", catalog), ("kelly", "opening"))
        self.assertEqual(owned_fields("New Exact", catalog), ())
        self.assertEqual(owned_fields("澳*", catalog), ())
        self.assertIn("88", catalog["advertisedCompanies"])
        self.assertNotIn("88", catalog["companies"])
        self.assertEqual({row["sourceCompanyId"] for row in result["9000133"]}, set(MODEL_COMPANIES))
        errors.assert_not_called()

    def test_unadvertised_required_kind_is_reported_and_never_requested(self):
        body = {kind: dict(CP_COMPANIES) for kind in KINDS}
        del body["kelly"]["2"]
        errors = Mock()
        client = self.client(bodies={"catalog":body})
        result, _, catalog = fetch_extras(client, IDENTITIES, "2026-11-14", errors, Mock())
        self.assertNotIn(("2","kelly"), {(call.kwargs["form"].get("cid"),call.kwargs["form"].get("sptype")) for call in client.api.call_args_list})
        self.assertEqual(owned_fields("36*",catalog),("opening",))
        errors.assert_called_once()
        self.assertIn("模型所需类型", str(errors.call_args.args[1]))

    def test_catalog_failure_keeps_verified_fallback_explicit_and_visible(self):
        for client in (self.client({"catalog": ValueError("unavailable")}), self.client(bodies={"catalog": {}})):
            errors = Mock()
            result, _, catalog = fetch_extras(client, IDENTITIES, "2026-11-14", errors, Mock())
            self.assertEqual(catalog["source"], "verified_static_fallback")
            self.assertIn("不代表本次目录确认", catalog["warning"])
            self.assertEqual(len(result["9000133"]), 3)
            errors.assert_called_once()
            self.assertIn("静态目录", str(errors.call_args.args[1]))
            matched = merge_extras([quote()], result["9000133"], catalog)[0]
            self.assertEqual(matched["cpExtras"]["catalogSource"], "verified_static_fallback")
            self.assertEqual(matched["cpExtras"]["catalogWarning"], catalog["warning"])
            missing = merge_extras([quote()], [], catalog)[0]
            self.assertEqual(missing["cpExtras"]["catalogWarning"], catalog["warning"])

    def test_alias_ambiguity_in_live_catalog_is_not_a_verified_identity(self):
        client = self.client(bodies={"catalog": {"europe": {"0": "平均值", "99": "平均*"}}})
        errors = Mock()
        _, _, catalog = fetch_extras(client, IDENTITIES, "2026-11-14", errors, Mock())
        self.assertEqual(catalog["source"], "verified_static_fallback")
        self.assertNotIn("99", catalog["companies"])
        errors.assert_called_once()

    def test_matching_prices_fill_only_missing_fields_and_scope_source_ids(self):
        original = quote()
        original["returnRatio"] = "0.95"
        cp = extra()
        result = merge_extras([original], [cp])[0]
        self.assertEqual(result["opening"], original["opening"])
        self.assertEqual(result["latest"], original["latest"])
        self.assertEqual(result["sourceCompanyId"], "8")
        self.assertEqual(result["sourceMatchId"], "4650133")
        self.assertEqual(result["returnRatio"], "0.95")
        self.assertEqual(result["kelly"], ["0.91", "0.92", "0.93"])
        self.assertIsNone(result["probabilities"])
        self.assertIsNone(result["openingKelly"])
        self.assertEqual(result["cpExtras"]["sourceCompanyId"], "2")
        self.assertEqual(result["cpExtras"]["sourceMatchId"], "9000133")
        self.assertEqual(result["cpExtras"]["status"], "matched")
        self.assertEqual(result["cpExtras"]["filledFields"], ["kelly"])
        self.assertEqual(result["apiOwnedFields"], ["kelly", "opening"])
        result["cpExtras"]["rawFields"]["kelly"]["SP"] = []
        self.assertIsNone(original["kelly"])
        self.assertEqual(cp, extra())

    def test_different_prices_do_not_mix_current_extras_but_missing_opening_can_fill(self):
        original = quote()
        original["opening"] = []
        original["latest"][0] = "3.41"
        original["fieldWarnings"] = ["earlier warning"]
        result = merge_extras([original], [extra()])[0]
        self.assertEqual(result["latest"], ["3.41", "3.7", "1.8"])
        self.assertEqual(result["opening"], ["3.0", "3.60", "2.0"])
        self.assertTrue(all(result[field] is None for field in FIELDS))
        self.assertEqual(result["cpExtras"]["status"], "price_mismatch")
        self.assertEqual(result["cpExtras"]["filledFields"], ["opening"])
        self.assertFalse(result["cpExtras"]["matchedLatest"])
        self.assertIn("earlier warning", result["fieldWarnings"])
        self.assertEqual(len(result["fieldWarnings"]), 2)

    def test_required_companies_keep_field_ownership_when_no_response_arrives(self):
        for label in ("平均值", "平均欧赔", "平均*", "威*", "36*"):
            with self.subTest(label=label):
                self.assertEqual(owned_fields(label), ("kelly", "opening"))
                original = quote(label)
                original["opening"] = []
                result = merge_extras([original], [])[0]
                self.assertEqual(result["apiOwnedFields"], ["kelly", "opening"])
                self.assertEqual(result["opening"], [])
                self.assertEqual(original["opening"], [])
                self.assertEqual(result["cpExtras"]["status"], "missing")
                self.assertEqual(result["cpExtras"]["missingKinds"], list(MODEL_KINDS))
        for label in ("澳*", "立*", "bwi*", "平*", "36"):
            self.assertEqual(owned_fields(label), ())
            self.assertNotIn("apiOwnedFields", merge_extras([quote(label)], [])[0])

    def test_official_bonus_api_owns_only_the_opening_price(self):
        self.assertEqual(owned_fields("官方(胜平负)"), ("opening",))
        self.assertFalse(set(owned_fields("官方(胜平负)")) & set(FIELDS))

    def test_exact_aliases_join_but_similar_names_and_numeric_ids_do_not(self):
        average = merge_extras([quote("平均欧赔")], [extra("0")])[0]
        self.assertEqual(average["kelly"], ["0.91", "0.92", "0.93"])
        unrelated = quote("平*")
        unrelated["sourceCompanyId"] = "2"
        self.assertEqual(merge_extras([unrelated], [extra()]), [unrelated])
        self.assertEqual(merge_extras([], [extra()]), [])
        wrong_cp_id = dict(extra(), sourceCompanyId="9")
        result = merge_extras([quote()], [wrong_cp_id])[0]
        self.assertIsNone(result["kelly"])
        self.assertEqual(result["cpExtras"]["status"], "missing")

    def test_nonrequired_bwin_metrics_are_not_newly_merged_or_erased(self):
        original = quote("Bw*n")
        original["kelly"] = ["0.88", "0.89", "0.90"]
        original["probabilities"] = ["30", "25", "45"]
        self.assertEqual(merge_extras([original], [extra("114")]), [original])
        self.assertEqual(owned_fields("BWI*"), ())

    def test_ambiguous_company_or_wrong_match_cannot_join(self):
        for targets, sources, expected in (([quote(), quote()], [extra()], "ambiguous"),
                                          ([quote()], [extra(), extra()], "ambiguous"),
                                          ([quote()], [dict(extra(), zgzcwMatchId="999")], "identity_mismatch")):
            with self.subTest(expected=expected):
                result = merge_extras(targets, sources)
                self.assertTrue(all(row["kelly"] is None for row in result))
                self.assertTrue(all(row["cpExtras"]["status"] == expected for row in result))
                self.assertTrue(all(row["apiOwnedFields"] == ["kelly", "opening"] for row in result))


if __name__ == "__main__":
    unittest.main()
