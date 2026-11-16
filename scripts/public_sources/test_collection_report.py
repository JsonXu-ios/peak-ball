import copy
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from collection_report import aggregate_errors, safe_error, summarize_collection


class CollectionReportTests(unittest.TestCase):
    def test_real_failed_batch_is_not_partial_success(self):
        result = summarize_collection(processed=10, complete=0, updated=10,
                                      errors=[{"stage": "2042767:比赛信息", "message": "比赛头尚未加载"}])
        self.assertEqual(result["status"], "failed")
        self.assertIn("本次取得数据10场", result["message"])
        self.assertIn("完整0场", result["message"])

    def test_failed_refresh_cannot_reuse_old_success(self):
        result = summarize_collection(processed=10, complete=10, updated=0,
                                      errors=[{"stage": "任务", "message": "来源HTTP 567"}])
        self.assertEqual(result["status"], "failed")
        self.assertIn("已有数据未算作采集成功", result["message"])

    def test_partial_update_and_partial_data_are_reported(self):
        for complete, updated in ((10, 5), (5, 10), (10, 10)):
            result = summarize_collection(processed=10, complete=complete, updated=updated,
                                          errors=[{"stage": "204:euro", "message": "来源HTTP 418"}])
            self.assertEqual(result["status"], "partial")

    def test_completed_and_empty_date_have_distinct_messages(self):
        result = summarize_collection(processed=10, complete=10, updated=10, errors=[])
        self.assertEqual(result["status"], "completed")
        empty = summarize_collection(processed=0, complete=0, updated=0, errors=[])
        self.assertEqual(empty["status"], "completed")
        self.assertIn("没有可采集比赛", empty["message"])
        failed = summarize_collection(processed=0, complete=0, updated=0,
                                      errors=[{"stage": "任务", "message": "来源HTTP 567"}])
        self.assertEqual(failed["status"], "failed")

    def test_invalid_counts_are_rejected(self):
        for processed, complete, updated in ((1, 2, 1), (1, 1, 2), (-1, 0, 0), (True, 0, 0), (1, 0.5, 1)):
            with self.assertRaises(ValueError):
                summarize_collection(processed=processed, complete=complete, updated=updated, errors=[])

    def test_browser_error_preserves_real_cause_and_removes_verbose_call_log(self):
        error = RuntimeError('Page.goto: net::ERR_CONNECTION_RESET at https://user:password@host.test/path?token=secret\nCall log:\n  - navigating to "https://host.test/secret"')
        result = safe_error(error)
        self.assertIn("net::ERR_CONNECTION_RESET", result)
        self.assertIn("[来源地址]", result)
        for secret in ("user", "password", "host.test", "secret", "Call log"):
            self.assertNotIn(secret, result)

    def test_credentials_are_removed_before_whitespace_collapse(self):
        error = "failed\nAuthorization: Bearer hidden-token\nCookie: session=private; user=alice\nproxy_password=p@ss username=alice api_key=123\n{'client_secret': 'my secret', 'access_token': 'private-token'}\nsocks5://alice:p@ss@localhost:1234"
        result = safe_error(error)
        for secret in ("hidden-token", "session=private", "p@ss", "alice", "my secret", "private-token", "123", "localhost"):
            self.assertNotIn(secret, result)
        self.assertIn("failed", result)

    def test_empty_errors_are_useful_and_long_errors_are_bounded(self):
        for error in (RuntimeError(), "Error", "Exception", None):
            self.assertIn("未提供具体原因", safe_error(error))
        self.assertIn("超时", safe_error(TimeoutError("TimeoutError")))
        self.assertEqual(len(safe_error("x" * 1000)), 350)

    def test_legacy_errors_group_by_source_without_mutation(self):
        errors = [
            {"stage": "204:euro", "message": "Error"},
            {"stage": "204:asian", "message": "浏览器收到安全拦截，保留失败"},
            {"stage": "205:totals", "message": "该来源已触发验证，本次任务停止继续请求"},
            {"stage": "胜平负支持比例", "message": "安全验证"},
            {"stage": "任务", "message": "验证码"},
        ]
        previous = copy.deepcopy(errors)
        result = aggregate_errors(errors)
        self.assertEqual(errors, previous)
        self.assertEqual(len(result), 4)
        self.assertIn("未提供具体原因", result[0]["message"])
        self.assertEqual(result[1]["source"], "zgzcw")
        self.assertEqual(result[1]["count"], 2)
        self.assertIn("共影响2项", result[1]["message"])
        self.assertEqual(result[2]["source"], "sporttery")
        self.assertEqual(result[3]["stage"], "任务")
        self.assertEqual(aggregate_errors(result), result)


if __name__ == "__main__":
    unittest.main()
