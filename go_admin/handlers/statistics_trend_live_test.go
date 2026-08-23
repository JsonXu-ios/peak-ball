package handlers

import (
	"encoding/json"
	"os"
	"testing"

	"go_admin/database"
)

// TestTrendLive 对本地库跑一次真实计算；默认跳过，TREND_LIVE=1 时执行。
func TestTrendLive(t *testing.T) {
	if os.Getenv("TREND_LIVE") != "1" {
		t.Skip("set TREND_LIVE=1 to run against the local database")
	}
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"upcoming", "history"} {
		report, err := computeTrendStatistics(mode, "", "", 5)
		if err != nil {
			t.Fatal(err)
		}
		summary, _ := json.Marshal(report["summary"])
		matches := report["matches"].([]trendMatchReport)
		t.Logf("%s: pool=%v total=%d summary=%s", mode, report["settled_pool"], len(matches), summary)
		for i, m := range matches {
			if i >= 2 {
				break
			}
			raw, _ := json.Marshal(m)
			t.Logf("%s", raw)
		}
	}
}
