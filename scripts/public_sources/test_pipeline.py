"""API-first regressions with captured-schema fixtures and disposable SQLite.

Every source response is local; no browser or source-site request is started.
"""
import json
import sys
import tempfile
import unittest
from copy import deepcopy
from contextlib import contextmanager
from pathlib import Path
from unittest.mock import Mock, patch
from urllib.parse import parse_qs, urlsplit

sys.path.insert(0, str(Path(__file__).resolve().parent))

from core import connect, detail, save_match, snapshot
from pipeline import acquisition, merge_euro_api, run_collection, supplement_quotes, REQUIRED_QUOTE_COMPANIES
from source_policy import SOURCE_POLICY
from split_pipeline import cp_opening
from worker import enqueue
from zgzcw_api import normalize_euro_changes, normalize_markets
from zgzcw_extras import CP_COMPANIES, KINDS
from cp_extra_collection import MODEL_COMPANIES, MODEL_KINDS


DAY = "2026-11-14"
KICKOFF = DAY + " 18:15:00"
KEY = "sporttery:" + DAY + ":2042753"
CP_ID = "9000133"
CP_SCHEDULE_PATH = "/lottery/jchtplayvsForJsp.action"
CP_HISTORY_PATH = "/ssc/lottery/jcsp/getjcspbymatchId.jsp"
CP_VALUES_PATH = "/ssc/lottery/getJCBDBFSP.jsp"
CP_EXTRAS_PATH = "/ssc/lottery/jcsp/getjcspbydate.jsp"
CP_CATALOG_PATH = "/ssc/lottery/jcsp/getjcspcompany.jsp"


def index_fixture():
    return [{"ID": 4650133, "SOURCE_MATCH_ID": 4650133,
             "HOST_NAME": "日本", "GUEST_NAME": "巴西", "CC_ID": "周六003",
             "MATCH_TIME": KICKOFF, "listOdds": [{
                 "SOURCE_COMPANY_ID": 2, "COMPANY_NAME": "36*", "SOURCE_MATCH_ID": 4650133,
                 "FIRST_WIN": 3.0, "FIRST_SAME": 3.6, "FIRST_LOST": 2.0,
                 "WIN": 3.2, "SAME": 3.65, "LOST": 1.81,
                 "FIRST_HOST": 0.95, "FIRST_HANDICAP": 0.5, "FIRST_GUEST": 0.85,
                 "HOST": 0.90, "HANDICAP": 0.5, "GUEST": 0.96,
                 "FIRST_BIG": 0.99, "DW_FIRST_HANDICAP": 2.5, "FIRST_SMALL": 0.91,
                 "BIG": 0.93, "DW_HANDICAP": 2.75, "SMALL": 0.97,
             }]}]


def changes_fixture(count=2, label="36*", company_id="8"):
    # FENXI and PLZX company IDs intentionally differ for the same label.
    records = [{"attribute": {"cid": company_id, "win": str(3.31 + i / 100),
                               "same": "3.70", "lost": "1.80", "returnPer": "0.94",
                               "wdate": f"{DAY} 17:{i:02d}:00", "arq": "受半球"}}
               for i in range(count)]
    records[-1]["attribute"]["win"] = "3.40"
    return {"company5List": [{"attribute": {"cid": company_id, "name": label},
                              "c": list(reversed(records))}]}


def page_fixture(kind="euro", label="36*", opening=None, latest=None, history_url=None):
    # Deliberately conflicting page prices expose accidental API overwrites.
    if kind == "euro":
        cells = ["1", label, "9.1", "9.2", "9.3", "8.1", "8.2", "8.3", "",
                 "31", "25", "44", "0.91", "0.92", "0.93", "0.88"]
    else:
        cells = ["1", label, "8.1", "3.5" if kind == "totals" else "一球", "8.2",
                 "9.1", "4" if kind == "totals" else "球半", "9.2", "",
                 "48", "52", "0.91", "0.92", "0.88", "主客同"]
    if opening is not None:
        cells[2:5] = opening
    if latest is not None:
        cells[5:8] = latest
    if history_url:
        cells[8] = '<a href="' + history_url + '">变化</a>'
    return (f"<p>日本 巴西 比赛时间：{KICKOFF}</p><table><tr>" +
            "".join("<td>" + cell + "</td>" for cell in cells) + "</tr></table>")


class FixtureClient:
    """Strict URL-routed fixtures; unknown endpoints fail instead of faking data."""
    def __init__(self):
        self.last_api_transport = "http"
        self.failures = {}
        self.cp_identities = [{"source": "zgzcw_cp", "sourceMatchId": CP_ID, "cpMatchId": CP_ID,
                               "zgzcwMatchId": "4650133", "home": "日本", "away": "巴西",
                               "kickoff": DAY + "T18:15:00+08:00", "lotteryNumber": "周六003",
                               "businessDate": DAY, "goal": "-1"}]
        self.history = {"source": "zgzcw_page_fields", "sourceMatchId": "4650133", "tables": [
            [["友谊赛", "", "2026-10-06", "日本", "2:1", "韩国", "1:0"]],
            [["友谊赛", "", "2026-10-07", "阿根廷", "0:1", "巴西", "0:0"]], []]}
        self.bodies = {
            "/odds/oyzs_ajax.action": index_fixture(),
            CP_CATALOG_PATH: {kind: dict(CP_COMPANIES) for kind in KINDS},
            CP_EXTRAS_PATH: {
                "europe": [{"MATCHID": CP_ID,
                            "SP": '["3","3.6","2","3.4","3.7","1.8"]',
                            "SP_CHANGE": json.dumps([["3.4", "3.7", "1.8", DAY + " 17:01:00"]])}],
                "kelly": [{"MATCHID": CP_ID, "SP": '["0.91","0.92","0.93"]'}],
                "probability": [{"MATCHID": CP_ID, "SP": '["31","25","44"]'}],
                "return": [{"MATCHID": CP_ID, "SP": '["","0.94",""]'}]},
            CP_HISTORY_PATH: {
                "wsl": [["3.85", "3.60", "1.66", DAY + " 09:00:00"],
                        ["3.2", "3.65", "1.81", DAY + " 17:00:00"],
                        ["3.4", "3.65", "1.75", DAY + " 15:00:00"]],
                "handicap_wsl": [["2.86", "3.45", "2.00", DAY + " 09:00:00"],
                                 ["3.85", "3.35", "1.72", DAY + " 17:00:00"]]},
            "/json/4650133/bjop": changes_fixture(),
        }
        self.api = Mock(side_effect=self._api)
        self.http = Mock(side_effect=self._http)
        self.document = Mock(side_effect=self._document)
        self.render = Mock(side_effect=AssertionError("No rendered-page fallback expected"))
        self.match_info = Mock(side_effect=AssertionError("Head must come from ZGCW's index API"))
        self.close = Mock()
        row = {"matchId": "2042753", "homeTeamAllName": "日本", "awayTeamAllName": "巴西",
               "matchNum": "周六003", "businessDate": DAY}
        vote = {"odds": [3.2, 3.65, 1.81], "support": [31, 25, 44],
                "probabilities": [28, 24, 48], "transport": "http"}
        self.votes = Mock(side_effect=lambda date, pool, limit: [(deepcopy(row), deepcopy(vote),
            "https://webapi.sporttery.cn/gateway/uniform/football/getVoteV1.qry?poolCode=" + pool + "&businessDate=" + date)])

    def _api(self, url, form=None):
        path = urlsplit(url).path
        if urlsplit(url).hostname not in {"plzx.zgzcw.com", "fenxi.zgzcw.com", "cp.zgzcw.com"}:
            raise AssertionError("Only the voting method may request Sporttery")
        if path in self.failures:
            raise self.failures[path]
        if path == "/odds/oyzs_ajax.action":
            if not isinstance(form, dict) or form.get("issue") != DAY or not form.get("companys"):
                raise AssertionError("Market-index API must use its actual POST form")
        elif path == CP_CATALOG_PATH:
            if form != {}:
                raise AssertionError("CP company catalog uses its published empty POST form")
        elif path == CP_EXTRAS_PATH:
            cid, kind = (form or {}).get("cid"), (form or {}).get("sptype")
            if (urlsplit(url).hostname != "cp.zgzcw.com" or cid not in CP_COMPANIES or kind not in KINDS
                    or form != {"cid": cid, "sptype": kind, "date": DAY}):
                raise AssertionError("CP extras must use a published company, kind and dated batch POST")
            return deepcopy(self.bodies[path][kind])
        elif path == CP_HISTORY_PATH:
            cid = (form or {}).get("cid")
            if cid not in {"wsl", "handicap_wsl"} or form != {"cid": cid, "matchId": CP_ID, "sptype": "smg", "date": DAY}:
                raise AssertionError("CP bonus history must use its verified CP ID and POST form")
            return deepcopy(self.bodies[path][cid])
        elif form is not None:
            raise AssertionError("Unexpected POST API")
        if path not in self.bodies:
            raise AssertionError("Unexpected API: " + path)
        return deepcopy(self.bodies[path])

    def _document(self, url):
        path = urlsplit(url).path
        if path in self.failures:
            raise self.failures[path]
        if path == CP_SCHEDULE_PATH:
            if parse_qs(urlsplit(url).query) != {"lotteryId": ["47"], "type": ["jcmini"], "issue": [DAY]}:
                raise AssertionError("CP schedule must request the target business date")
            return "<main>CP schedule fixture: identity only</main>"
        if path == "/4650133/bsls":
            return "<main>ZGCW verified history fixture</main>"
        kinds = {"/4650133/bjop": "euro", "/4650133/ypdb": "asian", "/4650133/dxdb": "totals"}
        if path not in kinds:
            raise AssertionError("Unexpected HTML fallback: " + path)
        return page_fixture(kinds[path])

    def _http(self, url, form=None):
        path, query = urlsplit(url).path, parse_qs(urlsplit(url).query)
        if path in self.failures:
            raise self.failures[path]
        if urlsplit(url).hostname != "cp.zgzcw.com" or path != CP_VALUES_PATH or form is not None or query.get("matchId") != [CP_ID]:
            raise AssertionError("Unexpected numeric endpoint or unverified CP match ID")
        if query.get("lottery") == ["23"]:
            return "10.50 11.00 7.00 18.00 11.50 16.50 36.00 25.00 36.00 90.00 65.00 80.00 28.00 20.00 7.25 9.50 33.00 125.0 16.00 21.00 11.00 45.00 25.00 24.00 90.00 65.00 65.00 350.0 175.0 200.0 65.00"
        if query.get("lottery") == ["24"]:
            return "20.00 6.75 4.30 3.40 4.40 6.75 11.50 15.00"
        raise AssertionError("Unknown CP numeric market")


@contextmanager
def fixture_parsers(client, full_company_coverage=False):
    """Only page-normalization boundaries are stubbed in integration tests.

    CP JSON and numeric responses still use the production bonus normalizer;
    the page parsers have separate regressions with full captured markup.
    """
    # Existing transport/field fixtures intentionally contain one company.
    # Full company-coverage behavior is exercised by the dedicated regressions.
    with patch("pipeline.REQUIRED_QUOTE_COMPANIES", REQUIRED_QUOTE_COMPANIES if full_company_coverage else {}), \
            patch("zgzcw_bonus.normalize_cp_schedule", side_effect=lambda *args, **kwargs: deepcopy(client.cp_identities)), \
            patch("split_pipeline.normalize_zgzcw_history", side_effect=lambda *args, **kwargs: deepcopy(client.history)) as history_parser:
        yield history_parser


class PipelineTests(unittest.TestCase):
    def setUp(self):
        coverage = patch("pipeline.REQUIRED_QUOTE_COMPANIES", {})
        coverage.start()
        self.addCleanup(coverage.stop)
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.db = connect(Path(self.folder.name) / "pipeline.sqlite3")
        self.addCleanup(self.db.close)
        self.env = patch.dict("os.environ", {"PUBLIC_SOURCE_KELLY_LABELS": "none"})
        self.env.start()
        self.addCleanup(self.env.stop)

    def collect(self, client):
        job = enqueue(self.db, {"date": DAY, "limit": 1})
        with fixture_parsers(client):
            return run_collection(self.db, job["id"], lambda: client)

    def assert_source_allocation(self, client):
        self.assertEqual([call.args[1] for call in client.votes.call_args_list], ["HAD", "HHAD"])
        for transport in (client.api, client.http, client.document):
            for call in transport.call_args_list:
                self.assertTrue(urlsplit(call.args[0]).hostname.endswith("zgzcw.com"), call)
        client.match_info.assert_not_called()
        client.render.assert_not_called()

    def test_euro_api_failure_never_uses_euro_page(self):
        client = FixtureClient()
        client.failures["/json/4650133/bjop"] = ValueError("欧赔接口暂不可用")
        result = self.collect(client)
        paths = [urlsplit(call.args[0]).path for call in client.document.call_args_list]
        self.assertNotIn("/4650133/bjop", paths)
        self.assertEqual(set(paths), {CP_SCHEDULE_PATH, "/4650133/bsls"})
        parts = detail(self.db, KEY)["parts"]
        self.assertNotIn("euro", parts)
        self.assertIn("asian", parts)
        self.assertTrue(any("euro:接口" in error["stage"] for error in result["errors"]))
        client.render.assert_not_called()
        client.close.assert_called_once()
        self.assert_source_allocation(client)

    def test_index_api_failure_never_discovers_matches_or_odds_from_html(self):
        save_match(self.db, {"date": DAY, "officialId": "2042753", "home": "日本", "away": "巴西",
                             "kickoff": KICKOFF, "zgzcwId": "4650133"})
        snapshot(self.db, KEY, "match_head", "sporttery", "old-official-head", {},
                 {"kickoff": KICKOFF}, acquisition={"sourcePolicy": SOURCE_POLICY})
        client = FixtureClient()
        client.failures["/odds/oyzs_ajax.action"] = ValueError("指数接口未返回数据")
        result = self.collect(client)
        self.assertEqual([urlsplit(call.args[0]).path for call in client.document.call_args_list], [CP_SCHEDULE_PATH])
        client.render.assert_not_called()
        parts = detail(self.db, KEY)["parts"]
        self.assertNotIn("bonus", parts)
        self.assertNotIn("history", parts)
        self.assertNotIn("match_head", parts)
        self.assertIn("support_had", parts)
        self.assertIsNone(detail(self.db, KEY)["match"]["kickoff"])
        self.assertNotIn("euro", parts)
        self.assertTrue(result["errors"])
        self.assert_source_allocation(client)

    def test_zgzcw_history_failure_never_requests_official_history(self):
        client = FixtureClient()
        client.failures["/4650133/bsls"] = ValueError("足彩网战绩未返回数据")
        self.collect(client)
        self.assertNotIn("history", detail(self.db, KEY)["parts"])
        paths = [urlsplit(call.args[0]).path for call in client.document.call_args_list]
        self.assertEqual(set(paths), {CP_SCHEDULE_PATH, "/4650133/bsls"})
        self.assert_source_allocation(client)

    def test_history_receives_verified_source_names_and_official_abbreviations(self):
        client = FixtureClient()
        home, away, short_home, short_away = "町田泽维亚", "神户胜利船", "町田泽维", "神户胜利"
        client.bodies["/odds/oyzs_ajax.action"][0].update(HOST_NAME=home, GUEST_NAME=away)
        client.cp_identities[0].update(home=home, away=away)
        original_votes = client.votes.side_effect

        def votes(date, pool, limit):
            result = original_votes(date, pool, limit)
            result[0][0].update(homeTeamAllName=home, awayTeamAllName=away,
                                homeTeamAbbName=short_home, awayTeamAbbName=short_away)
            return result

        client.votes.side_effect = votes
        client.document.side_effect = lambda url: client._document(url).replace("日本", home).replace("巴西", away)
        job = enqueue(self.db, {"date": DAY, "limit": 1})
        with fixture_parsers(client) as history_parser:
            result = run_collection(self.db, job["id"], lambda: client)
            history_parser.assert_called_once_with("<main>ZGCW verified history fixture</main>", "4650133", home, away, KICKOFF,
                                                   [home, short_home], [away, short_away])
        self.assertEqual(result["status"], "completed", result["errors"])
        self.assert_source_allocation(client)

    def test_missing_hhad_row_cannot_count_old_same_policy_snapshot_as_success(self):
        self.assertEqual(self.collect(FixtureClient())["status"], "completed")
        old = detail(self.db, KEY)["provenance"]["support_hhad"]
        self.assertEqual(old["acquisition"]["sourcePolicy"], SOURCE_POLICY)
        client = FixtureClient()
        original_votes = client.votes.side_effect
        client.votes.side_effect = lambda date, pool, limit: [] if pool == "HHAD" else original_votes(date, pool, limit)
        result = self.collect(client)
        saved = detail(self.db, KEY)
        self.assertEqual(result["status"], "partial", result["errors"])
        self.assertEqual(saved["missing"], [])  # The valid old record remains inspectable.
        self.assertEqual(saved["provenance"]["support_hhad"]["id"], old["id"])
        self.assertGreater(saved["provenance"]["support_had"]["id"], old["id"])
        self.assertTrue(any("让球支持比例缺项" in error["stage"] for error in result["errors"]))
        self.assert_source_allocation(client)

    def test_conflicting_page_prices_cannot_supply_required_kelly(self):
        identity = index_fixture()[0]
        identity["listOdds"][0]["COMPANY_NAME"] = "平*"
        quotes = merge_euro_api(normalize_euro_changes(changes_fixture(label="平*")), normalize_markets(identity)["euro"])
        original = deepcopy(quotes)
        client, meta, on_error = FixtureClient(), acquisition([]), Mock()
        client.document.side_effect = None
        client.document.return_value = page_fixture("euro", label="平*")
        result, evidence = supplement_quotes(client, identity, "euro", quotes, meta, on_error, Mock())
        self.assertEqual(result[0]["opening"], original[0]["opening"])
        self.assertEqual(result[0]["latest"], original[0]["latest"])
        self.assertIsNone(result[0]["kelly"])
        self.assertIsNone(result[0]["probabilities"])
        self.assertEqual(quotes, original)
        self.assertEqual(meta["mode"], "api")
        self.assertEqual(evidence[0]["fields"], [])
        self.assertEqual(evidence[0]["warnings"], meta["fieldWarnings"])
        warning = meta["fieldWarnings"][0]
        self.assertEqual(warning["company"], "平*")
        self.assertEqual(warning["apiPrices"], original[0]["latest"])
        self.assertNotEqual(warning["apiPrices"], warning["pagePrices"])
        self.assertEqual(warning["fields"], ["kelly"])
        on_error.assert_not_called()

    def test_matching_current_prices_supply_only_required_page_kelly(self):
        identity = index_fixture()[0]
        identity["listOdds"][0]["COMPANY_NAME"] = "平*"
        quotes = merge_euro_api(normalize_euro_changes(changes_fixture(label="平*")), normalize_markets(identity)["euro"])
        original = deepcopy(quotes)
        client, meta, on_error = FixtureClient(), acquisition([]), Mock()
        client.document.side_effect = None
        client.document.return_value = page_fixture("euro", label="平*", latest=["3.40", "3.70", "1.80"])
        result, evidence = supplement_quotes(client, identity, "euro", quotes, meta, on_error, Mock())
        self.assertEqual(result[0]["opening"], original[0]["opening"])
        self.assertEqual(result[0]["latest"], original[0]["latest"])
        self.assertEqual(result[0]["returnRatio"], "0.94")
        self.assertEqual(result[0]["kelly"], ["0.91", "0.92", "0.93"])
        self.assertIsNone(result[0]["probabilities"])
        self.assertEqual(meta["mode"], "api_with_page_fields")
        self.assertEqual(evidence[0]["fields"], ["平*:kelly"])
        self.assertEqual(quotes, original)
        on_error.assert_not_called()

    def test_complete_asian_and_total_prices_never_fetch_unused_metrics(self):
        identity = index_fixture()[0]
        for kind in ("asian", "totals"):
            quotes = normalize_markets(identity)[kind]
            original = deepcopy(quotes)
            client = FixtureClient()
            result, evidence = supplement_quotes(client, identity, kind, quotes, acquisition([]), Mock(), Mock())
            self.assertEqual(result, original)
            self.assertEqual(evidence, [])
            self.assertIsNone(result[0]["kelly"])
            self.assertIsNone(result[0]["probabilities"])
            client.document.assert_not_called()

    def test_missing_core_line_price_can_use_page_without_optional_metrics(self):
        identity = index_fixture()[0]
        for kind in ("asian", "totals"):
            quotes = normalize_markets(identity)[kind]
            quotes[0]["opening"] = []
            original_latest = deepcopy(quotes[0]["latest"])
            client = FixtureClient()
            client.document.side_effect = None
            client.document.return_value = page_fixture(kind)
            result, evidence = supplement_quotes(client, identity, kind, quotes, acquisition([]), Mock(), Mock())
            self.assertTrue(result[0]["opening"])
            self.assertEqual(result[0]["latest"], original_latest)
            self.assertIsNone(result[0]["kelly"])
            self.assertIsNone(result[0]["probabilities"])
            self.assertIsNone(result[0]["returnRatio"])
            self.assertEqual(evidence[0]["fields"], ["36*:opening"])
            client.document.assert_called_once()

    def test_page_never_fills_api_owned_fields_even_when_current_prices_match(self):
        fields = ("kelly", "probabilities", "returnRatio")
        cases = ({"kelly": ["0.81", "0.82", "0.83"], "probabilities": ["30", "26", "44"], "returnRatio": "0.94"},
                 dict.fromkeys(fields),
                 {"kelly": None, "probabilities": ["30", "26", "44"], "returnRatio": None})
        for values in cases:
            quotes = normalize_euro_changes(changes_fixture())
            quotes[0].update(values, opening=["3", "3.6", "2"], apiOwnedFields=list(fields))
            original = deepcopy(quotes)
            client, meta, on_error = FixtureClient(), acquisition([]), Mock()
            client.document.side_effect = None
            client.document.return_value = page_fixture("euro", latest=["3.40", "3.70", "1.80"])
            with self.subTest(values=values):
                result, evidence = supplement_quotes(client, index_fixture()[0], "euro", quotes, meta, on_error, Mock())
                self.assertEqual({field: result[0][field] for field in fields}, values)
                self.assertEqual(result[0]["opening"], original[0]["opening"])
                self.assertEqual(result[0]["latest"], original[0]["latest"])
                self.assertEqual(quotes, original)
                self.assertEqual(meta["pageFields"], [])
                self.assertEqual(meta["mode"], "api")
                self.assertEqual(evidence, [])
                on_error.assert_not_called()

    def test_page_only_cp_company_cannot_supply_api_owned_extras(self):
        quotes = normalize_euro_changes(changes_fixture(label="平*", company_id="22"))
        original = deepcopy(quotes)
        for label in ["平均值", "威*", "36*", "平均*", "平均欧赔", "官方(胜平负)"]:
            client, meta, on_error = FixtureClient(), acquisition([]), Mock()
            client.document.side_effect = None
            client.document.return_value = page_fixture("euro", label=label)
            with self.subTest(label=label):
                result, evidence = supplement_quotes(client, index_fixture()[0], "euro", quotes, meta, on_error, Mock())
                added = next(quote for quote in result if quote["sourceLabel"] == label)
                self.assertEqual(added["source"], "zgzcw_page_fields")
                extras = () if label == "官方(胜平负)" else ("kelly",)
                self.assertEqual(set(added["apiOwnedFields"]), {"opening", *extras})
                for field in extras:
                    self.assertIsNone(added[field])
                self.assertEqual(added["opening"], [])
                self.assertEqual(added["latest"], ["8.1", "8.2", "8.3"])
                self.assertEqual(result[0], original[0])
                self.assertEqual(quotes, original)
                self.assertEqual(evidence[0]["fields"], [label + ":接口公司列表未提供的报价"])
                on_error.assert_not_called()

    def test_owned_opening_and_latest_are_never_filled_from_page(self):
        quotes = normalize_euro_changes(changes_fixture(label="平*", company_id="22"))
        quotes[0].update(opening=[], latest=[], apiOwnedFields=["opening", "latest"],
                         kelly=["0.9", "0.9", "0.9"], probabilities=["31", "25", "44"])
        client, meta, on_error = FixtureClient(), acquisition([]), Mock()
        client.document.side_effect = None
        client.document.return_value = page_fixture("euro", label="平*")
        result, evidence = supplement_quotes(client, index_fixture()[0], "euro", quotes, meta, on_error, Mock())
        self.assertEqual(result[0]["opening"], [])
        self.assertEqual(result[0]["latest"], [])
        self.assertEqual(meta["pageFields"], [])
        self.assertEqual(evidence, [])
        on_error.assert_not_called()

    def test_failed_cp_opening_api_does_not_fall_back_to_matching_company_page(self):
        client = FixtureClient()
        client.failures[CP_EXTRAS_PATH] = ValueError("CP 欧赔接口暂不可用")
        for name in ("FIRST_WIN", "FIRST_SAME", "FIRST_LOST"):
            client.bodies["/odds/oyzs_ajax.action"][0]["listOdds"][0].pop(name)
        client.document.side_effect = lambda url: (
            page_fixture("euro", latest=["3.40", "3.70", "1.80"])
            if urlsplit(url).path == "/4650133/bjop" else client._document(url))
        result = self.collect(client)
        self.assertEqual(result["status"], "partial", result["errors"])
        saved = detail(self.db, KEY)
        quote = next(row for row in saved["parts"]["euro"] if row["sourceLabel"] == "36*")
        self.assertEqual(quote["opening"], [])
        self.assertEqual(quote["latest"], ["3.4", "3.7", "1.8"])
        self.assertIn("opening", quote["apiOwnedFields"])
        self.assertNotIn("openingSource", quote)
        self.assertFalse(any("36*:opening" in item["fields"]
                             for item in saved["provenance"]["euro"]["acquisition"]["pageFields"]))
        self.assert_source_allocation(client)

    def test_cp_extras_use_one_dated_batch_per_company_and_kind(self):
        client = FixtureClient()
        result = self.collect(client)
        self.assertEqual(result["status"], "completed", result["errors"])
        calls = [call for call in client.api.call_args_list if urlsplit(call.args[0]).path == CP_EXTRAS_PATH]
        self.assertEqual(len(calls), len(MODEL_COMPANIES) * len(MODEL_KINDS))
        self.assertEqual({(call.kwargs["form"]["cid"], call.kwargs["form"]["sptype"]) for call in calls},
                         {(cid, kind) for cid in MODEL_COMPANIES for kind in MODEL_KINDS})
        quote = detail(self.db, KEY)["parts"]["euro"][0]
        self.assertEqual(quote["sourceLabel"], "36*")
        self.assertEqual(quote["sourceCompanyId"], "8")
        self.assertEqual(quote["sourceMatchId"], "4650133")
        self.assertTrue(quote["cpExtras"]["quoteReceivedAt"])
        self.assertTrue(quote["cpExtras"]["kellyReceivedAt"])
        self.assertTrue(quote["cpExtras"]["receivedAt"])
        self.assertEqual(quote["kelly"], ["0.91", "0.92", "0.93"])
        self.assertIsNone(quote["probabilities"])
        self.assertEqual(quote["returnRatio"], "0.94")
        self.assertEqual(quote["cpExtras"]["cpMatchId"], CP_ID)
        self.assertEqual(quote["cpExtras"]["sourceCompanyId"], "2")
        self.assertEqual(quote["cpExtras"]["status"], "matched")
        self.assert_source_allocation(client)

    def test_catalog_is_requested_once_and_request_summary_preserves_parameters(self):
        client = FixtureClient()
        result = self.collect(client)
        self.assertEqual(result["status"], "completed", result["errors"])
        catalog_calls = [call for call in client.api.call_args_list if urlsplit(call.args[0]).path == CP_CATALOG_PATH]
        self.assertEqual(len(catalog_calls), 1)
        meta = detail(self.db, KEY)["provenance"]["euro"]["acquisition"]
        records = [item for item in meta["interfaces"] if urlsplit(item["url"]).path == CP_EXTRAS_PATH]
        self.assertEqual(len(records), 6)
        self.assertTrue(all(item["form"]["date"] == DAY and item["receivedAt"] for item in records))
        self.assertEqual(next(item for item in meta["interfaces"] if urlsplit(item["url"]).path == CP_CATALOG_PATH)["form"], {})

    def test_catalog_failure_remains_visible_while_verified_batches_continue(self):
        client = FixtureClient()
        client.failures[CP_CATALOG_PATH] = ValueError("directory unavailable")
        result = self.collect(client)
        self.assertEqual(result["status"], "partial")
        self.assertTrue(any("静态目录" in error["message"] for error in result["errors"]))
        quote = detail(self.db, KEY)["parts"]["euro"][0]
        self.assertEqual(quote["cpExtras"]["catalogSource"], "verified_static_fallback")
        self.assertTrue(quote["cpExtras"]["catalogWarning"])
        self.assertEqual(quote["kelly"], ["0.91", "0.92", "0.93"])

    def test_no_page_request_when_all_remaining_fields_are_api_owned_or_complete(self):
        for missing in (False, True):
            quotes = normalize_euro_changes(changes_fixture())
            quotes[0].update(opening=["3", "3.6", "2"], kelly=None if missing else [".9", ".9", ".9"],
                             probabilities=None if missing else ["31", "25", "44"])
            client = FixtureClient()
            result, evidence = supplement_quotes(client, index_fixture()[0], "euro", quotes, acquisition([]), Mock(), Mock())
            self.assertEqual(result[0]["kelly"], quotes[0]["kelly"])
            self.assertEqual(evidence, [])
            client.document.assert_not_called()
            client.render.assert_not_called()

    def test_noncore_company_gaps_do_not_trigger_page_collection(self):
        catalog = {"source": "current_api", "companies": {"88": {"label": "New Exact", "kinds": ["europe", "kelly"]}}}
        quotes = normalize_euro_changes(changes_fixture(label="New Exact"))
        quotes[0].update(opening=["3", "3.6", "2"])
        client = FixtureClient()
        result, evidence = supplement_quotes(client, index_fixture()[0], "euro", quotes, acquisition([]), Mock(), Mock(), catalog)
        client.document.assert_not_called()
        self.assertIsNone(result[0]["kelly"])
        self.assertIsNone(result[0]["probabilities"])
        self.assertEqual(evidence, [])

    def test_complete_requested_history_does_not_trigger_another_page_fetch(self):
        quotes = normalize_euro_changes(changes_fixture())
        quotes[0].update(openingKelly=[".9", ".9", ".9"], openingKellyTime=DAY + " 09:00:00",
                         openingKellyPrices=["3", "3.6", "2"], fullQuoteHistory=[["captured record"]])
        client = FixtureClient()
        with patch.dict("os.environ", {"PUBLIC_SOURCE_KELLY_LABELS": "36*"}):
            result, evidence = supplement_quotes(client, index_fixture()[0], "euro", quotes, acquisition([]), Mock(), Mock())
        self.assertEqual(result[0]["fullQuoteHistory"], [["captured record"]])
        self.assertEqual(evidence, [])
        client.document.assert_not_called()

    def test_failed_cp_extras_cannot_fall_back_to_matching_page_values(self):
        client = FixtureClient()
        client.failures[CP_EXTRAS_PATH] = ValueError("CP 附加接口暂不可用")
        client.document.side_effect = lambda url: (
            page_fixture("euro", latest=["3.40", "3.70", "1.80"])
            if urlsplit(url).path == "/4650133/bjop" else client._document(url))
        result = self.collect(client)
        self.assertEqual(result["status"], "partial", result["errors"])
        saved = detail(self.db, KEY)
        quote = saved["parts"]["euro"][0]
        self.assertIsNone(quote["kelly"])
        self.assertIsNone(quote["probabilities"])
        self.assertEqual(quote["returnRatio"], "0.94")  # Existing FENXI API value survives.
        self.assertEqual(quote["cpExtras"]["status"], "missing")
        self.assertEqual(set(quote["apiOwnedFields"]), {"kelly", "opening"})
        self.assertEqual([item["fields"] for item in saved["provenance"]["euro"]["acquisition"]["pageFields"]],
                         [["euro:matchMapping"]])
        self.assert_source_allocation(client)

    def test_missing_required_kelly_is_partial_but_unrequested_fields_do_not_block(self):
        for kind, field in (("kelly", "kelly"), ("probability", "probabilities"), ("return", "returnRatio")):
            client = FixtureClient()
            client.bodies[CP_EXTRAS_PATH][kind][0]["MATCHID"] = "9000134"
            client.document.side_effect = lambda url: (
                page_fixture("euro", latest=["3.40", "3.70", "1.80"])
                if urlsplit(url).path == "/4650133/bjop" else client._document(url))
            with self.subTest(kind=kind):
                result = self.collect(client)
                saved = detail(self.db, KEY)
                quote = saved["parts"]["euro"][0]
                if kind != "kelly":
                    self.assertEqual(result["status"], "completed", result["errors"])
                    self.assertEqual(quote["cpExtras"]["missingKinds"], [])
                    self.assertFalse(any(call.kwargs.get("form", {}).get("sptype") == kind for call in client.api.call_args_list))
                    continue
                self.assertEqual(result["status"], "partial", result["errors"])
                self.assertEqual(saved["missing"], [])
                self.assertEqual(quote["cpExtras"]["missingKinds"], [kind])
                self.assertIsNone(quote["cpExtras"][field])
                self.assertEqual(quote[field], "0.94" if field == "returnRatio" else None)
                errors = [error for error in result["errors"] if "足彩网CP附加:36*" in error["stage"]]
                self.assertEqual(len(errors), 1, result["errors"])
                self.assertIn(kind, errors[0]["message"])
                self.assertEqual([item["fields"] for item in saved["provenance"]["euro"]["acquisition"]["pageFields"]],
                                 [["euro:matchMapping"]])
                self.assert_source_allocation(client)

    def test_opening_kelly_retains_earliest_history_prices_time_and_mismatch_note(self):
        identity = index_fixture()[0]
        quotes = merge_euro_api(normalize_euro_changes(changes_fixture()), normalize_markets(identity)["euro"])
        history_url = "https://fenxi.zgzcw.com/4650133/bjop/zhishu?company_id=8"
        for historical_prices, mismatch in ((["3.00", "3.60", "2.00"], False),
                                             (["3.20", "3.70", "1.90"], True)):
            # Full-history response is newest-first; opening Kelly belongs to
            # the earliest timestamp and its original price triple.
            rows = [["2", DAY + " 17:10:00", "", "3.4", "3.7", "1.8", "31", "25", "44", "0.91", "0.92", "0.93", "0.94"],
                    ["1", DAY + " 09:00:00", ""] + historical_prices + ["31", "25", "44", "0.81", "0.82", "0.83", "0.94"]]
            history_raw = "<table>" + "".join("<tr>" + "".join("<td>" + value + "</td>" for value in row) + "</tr>" for row in rows) + "</table>"
            client, meta, on_error, heartbeat = FixtureClient(), acquisition([]), Mock(), Mock()
            current_page = page_fixture("euro", latest=["3.40", "3.70", "1.80"], history_url=history_url)
            client.document.side_effect = lambda url: history_raw if url == history_url else current_page
            with self.subTest(mismatch=mismatch), patch.dict("os.environ", {"PUBLIC_SOURCE_KELLY_LABELS": "36*"}):
                result, evidence = supplement_quotes(client, identity, "euro", quotes, meta, on_error, heartbeat)
                quote = result[0]
                self.assertEqual(quote["opening"], ["3", "3.6", "2"])
                self.assertEqual(quote["latest"], ["3.4", "3.7", "1.8"])
                self.assertEqual(quote["openingKelly"], ["0.81", "0.82", "0.83"])
                self.assertEqual(quote["openingKellyTime"], DAY + " 09:00:00")
                self.assertEqual(quote["openingKellyPrices"], historical_prices)
                if mismatch:
                    self.assertIn("与接口初赔不同", quote["openingKellyNote"])
                else:
                    self.assertNotIn("openingKellyNote", quote)
                self.assertEqual(quote["fullQuoteHistory"], rows)
                self.assertEqual(quote["historyUrl"], history_url)
                historical_evidence = next(item for item in evidence if item["url"] == history_url)
                self.assertEqual(historical_evidence["response"], history_raw)
                self.assertIn("36*:openingKellyPrices", historical_evidence["fields"])
                self.assertTrue(any(item["url"] == history_url and "36*:openingKellyPrices" in item["fields"]
                                    for item in meta["pageFields"]))
                self.assertIsNone(quotes[0]["openingKelly"])
                heartbeat.assert_called_once()
                on_error.assert_not_called()

    def test_recent_ten_changes_cannot_supply_opening_or_opening_kelly(self):
        quotes = normalize_euro_changes(changes_fixture(count=10))
        result = merge_euro_api(quotes, [])
        self.assertEqual(len(result[0]["quoteHistory"]), 10)
        self.assertEqual(result[0]["historyScope"], "recent")
        self.assertEqual(result[0]["opening"], [])
        self.assertIsNone(result[0]["openingKelly"])
        self.assertEqual(result[0]["latest"], ["3.4", "3.7", "1.8"])

    def test_rejected_cp_identity_cannot_supply_official_euro_opening(self):
        for field, wrong in (("zgzcwMatchId", "4650134"), ("home", "韩国"),
                             ("away", "阿根廷"), ("kickoff", DAY + "T20:15:00+08:00")):
            client = FixtureClient()
            client.bodies["/json/4650133/bjop"] = changes_fixture(label="官方(胜平负)", company_id="10000")
            client.cp_identities[0][field] = wrong
            with self.subTest(field=field):
                result = self.collect(client)
                saved = detail(self.db, KEY)
                official = next(quote for quote in saved["parts"]["euro"] if quote["sourceLabel"] == "官方(胜平负)")
                self.assertEqual(official["opening"], [])
                self.assertNotIn("openingSource", official)
                self.assertNotIn("bonus", saved["parts"])
                self.assertTrue(any("奖金接口" in error["stage"] for error in result["errors"]))
                interfaces = saved["provenance"]["euro"]["acquisition"]["interfaces"]
                self.assertFalse(any(CP_HISTORY_PATH in item["url"] for item in interfaces))
                self.assertFalse(any(urlsplit(call.args[0]).path == CP_HISTORY_PATH for call in client.api.call_args_list))
                client.http.assert_not_called()
                self.assert_source_allocation(client)

    def test_cp_bonus_does_not_replace_an_existing_api_opening(self):
        quotes = normalize_euro_changes(changes_fixture(label="官方(胜平负)", company_id="10000"))
        quotes[0]["opening"] = ["3", "3.6", "2"]
        quotes[0]["openingSource"] = {"source": "zgzcw_plzx", "sourceCompanyId": "25"}
        original = deepcopy(quotes)
        bonus = {"sourceMatchId": CP_ID, "spf": {"opening": [3.85, 3.6, 1.66]}}
        self.assertFalse(cp_opening(quotes, bonus))
        self.assertEqual(quotes, original)

    def test_failed_cp_bonus_never_uses_published_official_page_opening(self):
        client = FixtureClient()
        client.bodies["/json/4650133/bjop"] = changes_fixture(label="官方(胜平负)", company_id="10000")
        client.failures[CP_HISTORY_PATH] = ValueError("CP 奖金接口暂不可用")
        # A valid same-company page has a complete opening triple. Its
        # existence cannot turn a known API failure into a page-data fallback.
        client.document.side_effect = lambda url: (
            page_fixture("euro", label="官方(胜平负)", opening=["1.61", "3.83", "3.88"],
                         latest=["3.40", "3.70", "1.80"])
            if urlsplit(url).path == "/4650133/bjop" else client._document(url))
        result = self.collect(client)
        saved = detail(self.db, KEY)
        self.assertEqual(result["status"], "failed", result["errors"])
        self.assertNotIn("bonus", saved["parts"])
        official = next(row for row in saved["parts"]["euro"] if row["sourceLabel"] == "官方(胜平负)")
        self.assertEqual(official["opening"], [])
        self.assertEqual(official["latest"], ["3.4", "3.7", "1.8"])
        self.assertEqual(official["apiOwnedFields"], ["opening"])
        self.assertNotIn("openingSource", official)
        self.assertFalse(any("官方(胜平负):opening" in item["fields"]
                             for item in saved["provenance"]["euro"]["acquisition"]["pageFields"]))
        self.assertTrue(any("奖金接口" in error["stage"] for error in result["errors"]))
        self.assert_source_allocation(client)

    def test_absent_or_invalid_spf_cannot_supply_official_opening(self):
        candidates = [None, {}, {"sourceMatchId": CP_ID, "rqspf": {"opening": [3.85, 3.6, 1.66]}},
                      {"sourceMatchId": CP_ID, "spf": {"opening": []}},
                      {"sourceMatchId": CP_ID, "spf": {"opening": [3.85, None, 1.66]}}]
        for bonus in candidates:
            quotes = normalize_euro_changes(changes_fixture(label="官方(胜平负)", company_id="10000"))
            original = deepcopy(quotes)
            with self.subTest(bonus=bonus):
                self.assertFalse(cp_opening(quotes, bonus))
                self.assertEqual(quotes, original)

    def test_verified_cp_opening_preserves_bonus_receipt_and_euro_metadata(self):
        client = FixtureClient()
        client.bodies["/json/4650133/bjop"] = changes_fixture(label="官方(胜平负)", company_id="10000")
        bonus_body = client.bodies[CP_HISTORY_PATH]["wsl"]
        client.document.side_effect = lambda url: (
            page_fixture("euro", label="官方(胜平负)") if urlsplit(url).path.endswith("/bjop")
            else client._document(url))
        result = self.collect(client)
        self.assertEqual(result["status"], "completed", result["errors"])
        saved = detail(self.db, KEY)
        official = next(quote for quote in saved["parts"]["euro"] if quote["sourceLabel"] == "官方(胜平负)")
        self.assertEqual(official["opening"], ["3.85", "3.6", "1.66"])
        self.assertEqual(official["latest"], ["3.4", "3.7", "1.8"])
        self.assertEqual(official["openingSource"], {"source": "zgzcw_cp", "sourceMatchId": CP_ID})
        meta = saved["provenance"]["euro"]["acquisition"]
        bonus_url = "https://cp.zgzcw.com" + CP_HISTORY_PATH
        summary = next(item for item in meta["interfaces"] if item["url"] == bonus_url)
        self.assertEqual({key: summary[key] for key in ("url", "method", "transport")},
                         {"url": bonus_url, "method": "POST", "transport": "http"})
        self.assertEqual(summary["form"], {"cid": "wsl", "matchId": CP_ID, "sptype": "smg", "date": DAY})
        self.assertTrue(summary["receivedAt"])
        self.assertEqual(meta["sourcePolicy"], SOURCE_POLICY)
        self.assertFalse(any("官方(胜平负):opening" in item["fields"] for item in meta["pageFields"]))
        payload = json.loads(self.db.execute(
            "SELECT payload FROM ps_snapshots WHERE match_id=? AND kind='euro' ORDER BY id DESC LIMIT 1",
            (KEY,)).fetchone()[0])
        receipts = [item for item in payload["interfaces"] if item["url"] == bonus_url]
        self.assertEqual(len(receipts), 1)
        self.assertEqual(receipts[0]["response"], bonus_body)
        self.assertTrue(receipts[0]["receivedAt"])
        self.assertEqual(receipts[0]["method"], "POST")
        self.assertEqual(receipts[0]["form"], {"cid": "wsl", "matchId": CP_ID, "sptype": "smg", "date": DAY})
        self.assertEqual(saved["parts"]["bonus"]["sourceMatchId"], CP_ID)
        self.assertEqual(saved["parts"]["bonus"]["zgzcwMatchId"], "4650133")
        self.assertEqual(saved["parts"]["bonus"]["drawScores"], {"0:0": 20.0, "1:1": 7.25, "2:2": 9.5, "3:3": 33.0})
        self.assertEqual(len(saved["parts"]["bonus"]["scoreOdds"]), 31)
        self.assertEqual(len(saved["parts"]["bonus"]["totalGoals"]), 8)
        self.assert_source_allocation(client)

    def test_cp_bonus_failure_cannot_reuse_old_official_or_unmarked_bonus(self):
        save_match(self.db, {"date": DAY, "officialId": "2042753", "home": "日本", "away": "巴西", "kickoff": KICKOFF})
        stale_bonus = {"sourceMatchId": "2042753", "spf": {"opening": [9, 9, 9], "latest": [8, 8, 8]}}
        snapshot(self.db, KEY, "bonus", "sporttery", "old-official-bonus", {}, stale_bonus,
                 acquisition={"sourcePolicy": SOURCE_POLICY})
        snapshot(self.db, KEY, "bonus", "zgzcw", "old-unmarked-bonus", {}, stale_bonus)
        snapshot(self.db, KEY, "history", "sporttery", "old-official-history", {}, {"tables": [[], [], []]},
                 acquisition={"sourcePolicy": SOURCE_POLICY})
        client = FixtureClient()
        client.bodies["/json/4650133/bjop"] = changes_fixture(label="官方(胜平负)", company_id="10000")
        client.failures[CP_HISTORY_PATH] = ValueError("足彩网奖金接口未取得数据")
        client.failures["/4650133/bsls"] = ValueError("足彩网战绩未取得数据")
        result = self.collect(client)
        saved = detail(self.db, KEY)
        self.assertEqual(result["status"], "failed")
        self.assertNotIn("bonus", saved["parts"])
        self.assertNotIn("history", saved["parts"])
        self.assertIn("bonus", saved["missing"])
        self.assertIn("history", saved["missing"])
        official = next(quote for quote in saved["parts"]["euro"] if quote["sourceLabel"] == "官方(胜平负)")
        self.assertEqual(official["opening"], [])
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_snapshots WHERE id<=3").fetchone()[0], 3)
        self.assertFalse(any(urlsplit(call.args[0]).path not in {CP_SCHEDULE_PATH, "/4650133/bsls", "/4650133/bjop", "/4650133/ypdb", "/4650133/dxdb"}
                             for call in client.document.call_args_list))
        self.assert_source_allocation(client)

    def test_equal_company_numbers_do_not_merge_different_labels(self):
        changes = normalize_euro_changes(changes_fixture(label="威*", company_id="2"))
        index = normalize_markets(index_fixture()[0])["euro"]
        result = merge_euro_api(changes, index)
        self.assertEqual([quote["sourceLabel"] for quote in result], ["威*", "36*"])
        self.assertEqual(result[0]["opening"], [])
        self.assertNotIn("openingSource", result[0])
        self.assertEqual(result[1]["opening"], ["3", "3.6", "2"])

    def test_similar_company_labels_cannot_fill_other_company_fields(self):
        client = FixtureClient()
        client.document.return_value = page_fixture("euro", label="36")
        client.document.side_effect = None
        quotes = normalize_euro_changes(changes_fixture())
        quotes[0]["latest"] = []  # A genuine unowned gap permits the page request.
        result, _ = supplement_quotes(client, index_fixture()[0], "euro", quotes, acquisition([]), Mock(), Mock())
        self.assertEqual(result[0]["sourceLabel"], "36*")
        self.assertEqual(result[0]["opening"], [])
        self.assertIsNone(result[0]["kelly"])
        self.assertEqual(len(result), 1)  # Similar/unneeded company is not added.

    def test_empty_api_market_never_uses_its_page(self):
        client = FixtureClient()
        with self.assertRaisesRegex(ValueError, "禁止使用页面替代"):
            supplement_quotes(client, index_fixture()[0], "asian", [], acquisition([]), Mock(), Mock())
        client.document.assert_not_called()

    def test_failed_supplement_preserves_api_snapshot_and_reports_gap(self):
        client = FixtureClient()
        client.bodies["/json/4650133/bjop"] = changes_fixture(label="平*")
        client.bodies["/odds/oyzs_ajax.action"][0]["listOdds"][0]["COMPANY_NAME"] = "平*"
        for path in ("/4650133/bjop", "/4650133/ypdb", "/4650133/dxdb"):
            client.failures[path] = ValueError("字段补充页面暂不可用")
        result = self.collect(client)
        saved = detail(self.db, KEY)
        self.assertEqual(result["status"], "partial")
        self.assertEqual(saved["parts"]["euro"][0]["latest"], ["3.4", "3.7", "1.8"])
        self.assertIsNone(saved["parts"]["euro"][0]["kelly"])
        meta = saved["provenance"]["euro"]["acquisition"]
        self.assertEqual(meta["mode"], "api_with_page_fields")
        self.assertEqual([item["fields"] for item in meta["pageFields"]], [["euro:matchMapping"]])
        self.assertTrue(any("页面补充字段" in error["stage"] for error in result["errors"]))

    def test_wrong_match_page_is_not_used_as_field_evidence(self):
        client = FixtureClient()
        quotes = normalize_markets(index_fixture()[0])["asian"]
        quotes[0]["opening"] = []  # A required price gap, not optional metrics, permits the request.
        for page in (page_fixture("asian").replace("巴西", "韩国"),
                     page_fixture("asian").replace(KICKOFF, DAY + " 19:00:00")):
            client.document.side_effect = None
            client.document.return_value = page
            meta = acquisition([])
            with self.subTest(page=page), self.assertRaisesRegex(ValueError, "不匹配"):
                supplement_quotes(client, index_fixture()[0], "asian", quotes, meta, Mock(), Mock())
            self.assertEqual(meta["pageFields"], [])
            self.assertIsNone(quotes[0]["kelly"])

    def test_missing_core_company_is_added_from_page_without_extra_companies(self):
        for kind, missing in (("euro", "平*"), ("asian", "澳*"), ("totals", "澳*")):
            base = normalize_markets(index_fixture()[0])[kind][0]
            quotes = []
            for label in sorted(REQUIRED_QUOTE_COMPANIES[kind] - {missing}):
                row = deepcopy(base)
                row.update(sourceLabel=label, kelly=[".9", ".9", ".9"] if kind == "euro" else None)
                quotes.append(row)
            original = deepcopy(quotes)
            client, errors = FixtureClient(), Mock()
            client.document.side_effect = None
            client.document.return_value = page_fixture(kind, label=missing) + page_fixture(kind, label="Unused company")
            with self.subTest(kind=kind), patch("pipeline.REQUIRED_QUOTE_COMPANIES", REQUIRED_QUOTE_COMPANIES):
                result, evidence = supplement_quotes(client, index_fixture()[0], kind, quotes, acquisition([]), errors, Mock())
            self.assertEqual({q["sourceLabel"] for q in result}, set(REQUIRED_QUOTE_COMPANIES[kind]))
            added = next(q for q in result if q["sourceLabel"] == missing)
            self.assertEqual(added["source"], "zgzcw_page_fields")
            self.assertTrue(added["opening"] and added["latest"])
            self.assertEqual(added["kelly"], ["0.91", "0.92", "0.93"] if kind == "euro" else None)
            self.assertIsNone(added["probabilities"])
            self.assertIsNone(added["returnRatio"])
            self.assertTrue(any(missing + ":接口公司列表未提供的报价" in e["fields"] for e in evidence))
            for before in original:
                after = next(q for q in result if q["sourceLabel"] == before["sourceLabel"])
                self.assertEqual((after["opening"], after["latest"]), (before["opening"], before["latest"]))
            errors.assert_not_called()
            client.document.assert_called_once()
            client.api.assert_not_called()
            client.document.return_value = page_fixture(kind, label="Unused company")
            with patch("pipeline.REQUIRED_QUOTE_COMPANIES", REQUIRED_QUOTE_COMPANIES):
                still_missing, _ = supplement_quotes(client, index_fixture()[0], kind, quotes, acquisition([]), errors, Mock())
            self.assertNotIn(missing, [q["sourceLabel"] for q in still_missing])
            errors.assert_called_once()
            self.assertIn(missing, str(errors.call_args.args[1]))

    def test_missing_cp_company_page_row_receives_existing_paired_bundle(self):
        client = FixtureClient()
        def documents(url):
            path = urlsplit(url).path
            if path == "/4650133/bjop":
                return "".join(page_fixture("euro", label=label) for label in ("威*", "平均欧赔", "平*"))
            if path in ("/4650133/ypdb", "/4650133/dxdb"):
                kind = "asian" if path.endswith("ypdb") else "totals"
                return "".join(page_fixture(kind, label=label) for label in ("威*", "澳*"))
            return client._document(url)
        client.document.side_effect = documents
        job = enqueue(self.db, {"date": DAY, "limit": 1})
        with fixture_parsers(client, full_company_coverage=True):
            result = run_collection(self.db, job["id"], lambda: client)
        self.assertEqual(result["status"], "completed", result["errors"])
        saved = detail(self.db, KEY)
        william = next(q for q in saved["parts"]["euro"] if q["sourceLabel"] == "威*")
        self.assertEqual(william["latest"], ["8.1", "8.2", "8.3"])
        self.assertEqual(william["opening"], ["3", "3.6", "2"])
        self.assertIsNone(william["kelly"])
        self.assertEqual(william["sourceMatchId"], "4650133")
        self.assertEqual(william["cpExtras"]["status"], "price_mismatch")
        self.assertEqual(william["cpExtras"]["zgzcwMatchId"], "4650133")
        self.assertEqual(william["cpExtras"]["cpMatchId"], CP_ID)
        self.assertEqual(william["cpExtras"]["latest"], ["3.4", "3.7", "1.8"])
        self.assertEqual(william["cpExtras"]["kelly"], ["0.91", "0.92", "0.93"])
        self.assertTrue(william["cpExtras"]["receivedAt"])
        batches = [call for call in client.api.call_args_list if urlsplit(call.args[0]).path == CP_EXTRAS_PATH]
        self.assertEqual(len(batches), 6)
        for kind in ("euro", "asian", "totals"):
            self.assertTrue(REQUIRED_QUOTE_COMPANIES[kind].issubset({q["sourceLabel"] for q in saved["parts"][kind]}))


if __name__ == "__main__":
    unittest.main()
