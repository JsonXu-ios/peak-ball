import json
from copy import deepcopy
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parent))
from core import connect, detail, overview, save_match, snapshot
from models import projection
from source_policy import SOURCE_POLICY
from worker import dispatch, enqueue
from results import (ENDPOINT, attach_result, collect_results, confirmed, current_result,
                     load_identity, normalize_result, raw_result, save_result, unknown)

CAPTURE = "2026-11-16T01:00:00+00:00"
POLICY = {"sourcePolicy": SOURCE_POLICY}


class FakeClient:
    def __init__(self, rows=None, error=None):
        self.rows, self.error, self.requests, self.closed = rows, error, [], False

    def api(self, url, form=None):
        self.requests.append((url, form))
        if self.error:
            raise ValueError(self.error)
        return deepcopy(self.rows)

    def votes(self, *_):
        raise AssertionError("result updates must never recollect votes")

    def close(self):
        self.closed = True


class ResultTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.db = connect(Path(self.folder.name) / "test.sqlite3")
        self.key = save_match(self.db, {"date":"2026-11-15", "officialId":"2042774", "home":"官方主队", "away":"官方客队", "lotteryNumber":"周日009", "zgzcwId":"bad-manual-id"})
        self.head = {"sourceMatchId":"4518112", "home":"克罗地亚", "away":"捷克", "kickoff":"2026-11-16 03:45:00", "lotteryNumber":"周日009"}
        snapshot(self.db, self.key, "match_head", "zgzcw", "verified-heading", {}, self.head, "2026-11-15T14:00:00Z", POLICY)
        self.match = dict(self.db.execute("SELECT * FROM ps_matches WHERE id=?", (self.key,)).fetchone())
        self.identity = load_identity(self.db, self.match)
        self.row = {"ID":4518112,"HOST_NAME":"克罗地亚","GUEST_NAME":"捷克","MATCH_TIME":"2026-11-16 03:45:00","CC_ID":"周日009","MATCH_STATUS":-1,"HOST_GOAL":2,"GUEST_GOAL":1}

    def tearDown(self):
        self.db.close()
        self.folder.cleanup()

    def value(self, **changes):
        return normalize_result(dict(self.row, **changes), self.identity, CAPTURE)

    def store(self, value):
        return save_result(self.db, self.key, None, value, {"fixture":True}, self.identity, CAPTURE)

    def test_contract_final_scope_and_empty_scores(self):
        self.assertTrue(confirmed(self.value()))
        self.assertEqual(self.value(HOST_GOAL=0,GUEST_GOAL="0")["homeScore"], 0)
        for raw in (None, "", " ", True, False, 2.0, "2.0", "-1", "2:1", "NaN"):
            with self.subTest(raw=raw):
                result = self.value(HOST_GOAL=raw)
                self.assertFalse(result["regulationSettled"])
                self.assertIsNone(result["homeScore"])
                self.assertEqual(result["state"], "unverified")

    def test_verified_extra_time_penalty_and_ordinary_fixtures(self):
        # Audited at reports/source-probes/public-results-20261116/result-contract-evidence.json.
        # J League final was 3:2 after ET; FA shootout was 3:2. Both API rows
        # publish 2:2 at 90 minutes, not the extra-time/shootout result.
        cases = [
            ("2025-09-07",4345436,"川崎前锋","浦和红钻","周日003","2025-09-07 18:00:00",2,2),
            ("2025-08-10",4335455,"水晶宫","利物浦","周日011","2025-08-10 22:00:00",2,2),
            ("2026-11-15",4551914,"马斯特里赫特","维特斯","周日005","2026-11-15 21:30:00",2,1),
        ]
        for day,mid,home,away,number,kickoff,h,a in cases:
            identity = dict(self.identity,businessDate=day,sourceMatchId=str(mid),home=home,away=away,lotteryNumber=number,kickoff=kickoff)
            row = dict(ID=mid,HOST_NAME=home,GUEST_NAME=away,CC_ID=number,MATCH_TIME=kickoff,MATCH_STATUS=-1,HOST_GOAL=h,GUEST_GOAL=a)
            result = normalize_result(row,identity,CAPTURE)
            self.assertTrue(confirmed(result))
            self.assertEqual((result["homeScore"],result["awayScore"]),(h,a))

    def test_nonfinal_cancelled_postponed_unknown_are_not_settled(self):
        for status,state in [(0,"pending"),(1,"pending"),(2,"pending"),(3,"pending"),(4,"pending"),(-10,"cancelled"),(-14,"postponed"),(-11,"unverified"),(-12,"unverified"),(-13,"unverified"),(77,"unverified"),(None,"unverified")]:
            result = self.value(MATCH_STATUS=status)
            self.assertEqual(result["state"],state)
            self.assertFalse(confirmed(result))
            self.assertIsNone(result["homeScore"])

    def test_strict_id_teams_number_kickoff_and_received_time(self):
        for changes in ({"ID":12},{"SOURCE_MATCH_ID":12},{"HOST_NAME":"官方主队"},{"GUEST_NAME":"克罗地亚"},{"CC_ID":"周一009"},{"MATCH_TIME":"2026-11-15 03:45:00"},{"MATCH_TIME":"2026-11-16 03:46:00"},{"MATCH_TIME_INTEGER":0}):
            with self.subTest(changes=changes), self.assertRaises(ValueError):
                self.value(**changes)
        for stamp in ("invalid","2026-11-16T01:00:00","2026-11-15T19:44:59Z"):
            with self.subTest(stamp=stamp), self.assertRaises(ValueError):
                normalize_result(self.row,self.identity,stamp)
        self.assertTrue(confirmed(normalize_result(self.row,self.identity,"2026-11-15T19:45:00Z")))

    def test_heading_policy_and_cross_midnight_identity(self):
        self.assertEqual(self.identity["sourceMatchId"],"4518112")
        snapshot(self.db,self.key,"match_head","sporttery","wrong-provider",{},dict(self.head,sourceMatchId="12"),CAPTURE,POLICY)
        self.assertEqual(load_identity(self.db,self.match),self.identity)
        for changes in ({"kickoff":"2026-11-17 03:45:00"},{"lotteryNumber":"周一009"},{"sourceMatchId":None},{"home":""}):
            snapshot(self.db,self.key,"match_head","zgzcw","bad-head",{},dict(self.head,**changes),CAPTURE,POLICY)
            with self.assertRaises(ValueError): load_identity(self.db,self.match)

    def test_preserve_confirmed_on_failure_and_accept_confirmed_revision(self):
        first=self.store(self.value())
        self.store(unknown("接口失败"))
        result=current_result(self.db,self.key)
        self.assertTrue(confirmed(result));self.assertEqual(result["snapshotId"],first)
        self.assertEqual(result["lastAttemptError"],"接口失败")
        self.store(self.value(HOST_GOAL=3))
        result=current_result(self.db,self.key)
        self.assertTrue(confirmed(result));self.assertEqual(result["homeScore"],3)
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_result_snapshots").fetchone()[0],3)

    def test_cancellation_conflict_stays_unverified_until_new_final(self):
        self.store(self.value())
        self.store(self.value(MATCH_STATUS=-10))
        self.store(unknown("本次请求失败"))
        result=current_result(self.db,self.key)
        self.assertEqual(result["state"],"unverified");self.assertFalse(result["regulationSettled"])
        self.assertEqual(result["homeScore"],2)
        self.store(self.value(HOST_GOAL=1,GUEST_GOAL=1))
        self.assertTrue(confirmed(current_result(self.db,self.key)))

    def test_result_read_checks_current_verified_identity(self):
        self.store(self.value())
        snapshot(self.db,self.key,"match_head","zgzcw","new-heading",{},dict(self.head,sourceMatchId="99"),CAPTURE,POLICY)
        self.assertFalse(current_result(self.db,self.key)["regulationSettled"])

    def test_response_overlay_does_not_mutate_model_or_cache(self):
        models={"version":"frozen", "observation":{"homeScore":-1,"awayScore":-1,"settled":False,"regulationSettled":False,"frozenDrawSignal":{"state":"selected","score":.301},"scoreRecommendation":{"scores":["1:1","2:2"]}}}
        text=json.dumps(models)
        self.db.execute("INSERT INTO ps_model_results VALUES(?,?,?,?)",(self.key,"fingerprint",text,CAPTURE));self.db.commit()
        before=projection(self.db,self.match)
        snapshots=self.db.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0]
        self.store(self.value())
        result=current_result(self.db,self.key)
        enriched=attach_result(models,result)
        self.assertTrue(enriched["observation"]["settled"])
        self.assertEqual(enriched["observation"]["frozenDrawSignal"],models["observation"]["frozenDrawSignal"])
        self.assertEqual(json.dumps(models),text)
        self.assertEqual(projection(self.db,self.match),before)
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0],snapshots)
        self.assertEqual(tuple(self.db.execute("SELECT fingerprint,result FROM ps_model_results").fetchone()),("fingerprint",text))
        self.assertIn("result",detail(self.db,self.key))
        self.assertTrue(overview(self.db,"2026-11-15")["matches"][0]["result"]["regulationSettled"])
        def enrich_fixture(_db,matches):
            for match in matches:match["models"]=models
        with patch("models.enrich",side_effect=enrich_fixture):
            response=dispatch(self.db,"overview",{"date":"2026-11-15"})
            self.assertTrue(response["matches"][0]["models"]["observation"]["settled"])
            response=dispatch(self.db,"detail",{"id":self.key})
            self.assertTrue(response["models"]["observation"]["settled"])
        self.assertEqual(json.dumps(models),text)

    def test_result_raw_is_separate_and_match_scoped(self):
        rid=self.store(self.value())
        self.assertEqual(json.loads(raw_result(self.db,{"id":self.key,"snapshotId":str(rid)})["payload"]),{"fixture":True})
        for payload in ({"id":"other","snapshotId":rid},{"id":self.key,"snapshotId":True},{"id":self.key,"snapshotId":"1.0"}):
            with self.assertRaises(ValueError):raw_result(self.db,payload)

    def test_result_and_collection_jobs_share_exclusion(self):
        job=dispatch(self.db,"enqueue-results",{"date":"2026-11-15","officialId":"2042774"})
        config=json.loads(self.db.execute("SELECT config FROM ps_jobs WHERE id=?",(job["id"],)).fetchone()[0])
        self.assertEqual(config,{"date":"2026-11-15","officialId":"2042774","kind":"results"})
        with self.assertRaises(ValueError):enqueue(self.db,{"date":"2026-11-15"})
        with self.assertRaises(ValueError):dispatch(self.db,"enqueue-results",{"date":"2026-11-15"})
        with patch("results.collect_results",return_value={"kind":"results"}) as collect, patch("collector.collect",side_effect=AssertionError("full recollect")):
            self.assertEqual(dispatch(self.db,"collect",{"id":job["id"]}),{"kind":"results"});collect.assert_called_once()

    def test_update_only_results_with_real_collector_contract(self):
        job=dispatch(self.db,"enqueue-results",{"date":"2026-11-15"})
        client=FakeClient([self.row]);before=projection(self.db,self.match)
        with patch("pipeline.now",return_value=CAPTURE):report=collect_results(self.db,job["id"],lambda:client)
        self.assertEqual(report["status"],"completed");self.assertEqual(report["processed"],1)
        self.assertEqual(client.requests,[(ENDPOINT,{"type":"jc","issue":"2026-11-15","date":"","companys":"2"})])
        self.assertTrue(client.closed);self.assertTrue(confirmed(current_result(self.db,self.key)))
        self.assertEqual(projection(self.db,self.match),before)
        self.assertEqual(self.db.execute("SELECT COUNT(*) FROM ps_snapshots").fetchone()[0],1)
        with self.assertRaises(ValueError):collect_results(self.db,job["id"],lambda:client)

    def test_failed_empty_duplicate_and_unknown_responses_not_completed(self):
        for rows,error in [([],None),([self.row,self.row],None),({},None),([dict(self.row,MATCH_STATUS=77)],None),(None,"接口HTTP404")]:
            with self.subTest(rows=rows,error=error):
                job=dispatch(self.db,"enqueue-results",{"date":"2026-11-15"})
                with patch("pipeline.now",return_value=CAPTURE):report=collect_results(self.db,job["id"],lambda:FakeClient(rows,error))
                self.assertEqual(report["status"],"failed");self.assertTrue(report["errors"])
        self.store(self.value())
        job=dispatch(self.db,"enqueue-results",{"date":"2026-11-15"})
        with patch("pipeline.now",return_value=CAPTURE):collect_results(self.db,job["id"],lambda:FakeClient(error="失败"))
        self.assertTrue(confirmed(current_result(self.db,self.key)))
        self.assertEqual(current_result(self.db,self.key)["lastAttemptError"],"失败")

    def test_status_conflict_is_reported_as_failed_update(self):
        self.store(self.value())
        job=dispatch(self.db,"enqueue-results",{"date":"2026-11-15"})
        with patch("pipeline.now",return_value=CAPTURE):
            report=collect_results(self.db,job["id"],lambda:FakeClient([dict(self.row,MATCH_STATUS=-14)]))
        self.assertEqual(report["status"],"partial")
        self.assertIn("冲突",report["errors"][0]["message"])
        self.assertIn("已确认比分0场，未完赛0场，取消/延期0场，待核实或失败1项",report["message"])
        self.assertEqual(current_result(self.db,self.key)["state"],"unverified")
        self.assertFalse(current_result(self.db,self.key)["regulationSettled"])


if __name__ == "__main__":
    unittest.main()
