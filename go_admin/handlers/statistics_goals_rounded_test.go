package handlers

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// #23 期望球数取整对盘口：期望球数先四舍五入成 0~8 的整数球数，再与盘口比大小，
// 相等也判大球。

func roundedDetails(t *testing.T, report gin.H) []statisticsDetail {
	t.Helper()
	details, ok := statSignal(t, report, "goals_rounded")["matches"].([]statisticsDetail)
	if !ok {
		t.Fatal("goals_rounded 明细类型不对")
	}
	return details
}

func TestGoalsRoundedComparesIntegerGoalsToLine(t *testing.T) {
	// 期望球数=0.3×历史+0.7×近期。三场分别落在取整的三种关系上：
	// 差值一律取【期望球数原值 − 盘口】，不是取整值减盘口。
	//   1: 历史2 近期3 → 期望 2.7 → 取整 3；盘口 2.5 → 3 > 2.5 判大球，
	//      实际 3-1 共 4 球打出大球 → 命中。差值 = 2.7-2.5 = +0.20（不是 +0.50）。
	//   2: 历史2 近期2 → 期望 2.0 → 取整 2；盘口 2.5 → 2 < 2.5 判小球，
	//      实际 3-1 共 4 球打出大球 → 未命中。差值 -0.50。
	//   3: 历史3 近期3 → 期望 3.0 → 取整 3；盘口 3（整数盘）→ 相等按大球算，
	//      实际 4-0 共 4 球 > 3 打出大球 → 命中。差值 +0.00。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 3, GuestScore: 1},
		{ID: "3", Date: "2026-03-01", Home: "E", Guest: "F", HomeScore: 4, GuestScore: 0},
	}
	histories := map[string]map[string]interface{}{
		"1": goalsHistory("A", "B", 2, 0, 2, 1),
		"2": goalsHistory("C", "D", 1, 1, 1, 1),
		"3": goalsHistory("E", "F", 2, 1, 2, 1),
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
		"2": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
		"3": {"dxq_data": `[{"companyId":8,"pankou":"3"}]`},
	}
	report := buildSignalStatistics(matches, histories, pankous, nil)

	sig := statSignal(t, report, "goals_rounded")
	if sig["matched"] != 3 || sig["hit"] != 2 {
		t.Fatalf("matched/hit = %v/%v, want 3/2", sig["matched"], sig["hit"])
	}

	want := map[string]struct {
		value float64
		pick  string
		hit   bool
		exp   string
		diff  string
	}{
		"1": {3, "大球", true, "期望 2.70 → 3 球", "差值 +0.20"},
		"2": {2, "小球", false, "期望 2.00 → 2 球", "差值 -0.50"},
		"3": {3, "大球", true, "期望 3.00 → 3 球", "差值 +0.00"},
	}
	for _, d := range roundedDetails(t, report) {
		expect, ok := want[d.MatchID]
		if !ok {
			t.Fatalf("多出一场比赛 %s", d.MatchID)
		}
		if d.Value != expect.value || d.Pick != expect.pick || d.Hit != expect.hit {
			t.Errorf("比赛%s = %v/%s/%v, want %v/%s/%v", d.MatchID, d.Value, d.Pick, d.Hit, expect.value, expect.pick, expect.hit)
		}
		if d.ExpGoals != expect.exp || d.HeatGoals != expect.diff {
			t.Errorf("比赛%s 明细列 = %q / %q, want %q / %q", d.MatchID, d.ExpGoals, d.HeatGoals, expect.exp, expect.diff)
		}
	}
}

func TestDiffStepBucketsByTenths(t *testing.T) {
	// 差值分档的下界必须落在 0.1 的整数倍上，负数要向下取整（更负的一侧）。
	// 0.3 这类值在浮点下是 0.29999…，直接 ×10 取整会掉进上一档，所以走整数分。
	cases := []struct {
		diff float64
		want int
	}{
		{0.00, 0},   // +0.0 ~ +0.1
		{0.05, 0},   //
		{0.10, 1},   // +0.1 ~ +0.2
		{0.20, 2},   //
		{0.30, 3},   // 浮点陷阱：0.3×10 = 2.9999…
		{0.70, 7},   //
		{-0.05, -1}, // -0.1 ~ -0.0
		{-0.10, -1}, // 下界落在 -0.1 这一档
		{-0.25, -3}, // -0.3 ~ -0.2
		{-0.30, -3}, //
		{-1.00, -10},
	}
	for _, item := range cases {
		if got := statisticsDiffStep(item.diff); got != item.want {
			t.Errorf("statisticsDiffStep(%.2f) = %d, want %d", item.diff, got, item.want)
		}
	}
}

func TestGoalsDiffBandGroupsMatchesByTenth(t *testing.T) {
	// 三场比赛的差值分别是 +0.20、-0.50、+0.00（口径同上一条测试的三场），
	// 应当落进 +0.2~+0.3、-0.5~-0.4、+0.0~+0.1 三个不同的档，各 1 场。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 3, GuestScore: 1},
		{ID: "3", Date: "2026-03-01", Home: "E", Guest: "F", HomeScore: 4, GuestScore: 0},
	}
	histories := map[string]map[string]interface{}{
		"1": goalsHistory("A", "B", 2, 0, 2, 1),
		"2": goalsHistory("C", "D", 1, 1, 1, 1),
		"3": goalsHistory("E", "F", 2, 1, 2, 1),
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
		"2": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
		"3": {"dxq_data": `[{"companyId":8,"pankou":"3"}]`},
	}
	sig := statSignal(t, buildSignalStatistics(matches, histories, pankous, nil), "goals_diff_band")
	if sig["matched"] != 3 || sig["hit"] != 2 {
		t.Fatalf("合计 matched/hit = %v/%v, want 3/2", sig["matched"], sig["hit"])
	}
	buckets, ok := sig["buckets"].([]gin.H)
	if !ok {
		t.Fatalf("buckets 类型 = %T", sig["buckets"])
	}
	// 只输出有场次的档，且按差值升序排列。
	if len(buckets) != 3 {
		t.Fatalf("档位数 = %d, want 3（空档不输出）", len(buckets))
	}
	wantTitles := []string{"-0.5 ~ -0.4 球", "+0.0 ~ +0.1 球", "+0.2 ~ +0.3 球"}
	for i, bucket := range buckets {
		if title, _ := bucket["title"].(string); title != wantTitles[i] {
			t.Fatalf("第%d档 = %q, want %q（必须按差值升序）", i+1, title, wantTitles[i])
		}
		if matched, _ := bucket["matched"].(int); matched != 1 {
			t.Fatalf("第%d档场次 = %d, want 1", i+1, matched)
		}
	}
}

func TestGoalsRoundedTreatsEqualAsOver(t *testing.T) {
	// 取整球数正好等于整数盘口时必须判大球，且实际走盘（总进球=盘口）不计入分母。
	// 历史3 近期3 → 期望 3.0 → 取整 3，盘口 3；实际 2-1 共 3 球正好走盘。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 2, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{"1": goalsHistory("A", "B", 2, 1, 2, 1)}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"3"}]`},
	}
	sig := statSignal(t, buildSignalStatistics(matches, histories, pankous, nil), "goals_rounded")
	if sig["matched"] != 0 {
		t.Fatalf("走盘场次 matched = %v, want 0（走盘不进分母）", sig["matched"])
	}
}

func TestGoalsRoundedClampsToEightGoals(t *testing.T) {
	// 期望值再高也只换算到 8 球：历史9 近期9 → 期望 9.0 → 取整夹到 8。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 5, GuestScore: 4},
	}
	histories := map[string]map[string]interface{}{"1": goalsHistory("A", "B", 5, 4, 5, 4)}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"3.5"}]`},
	}
	details := roundedDetails(t, buildSignalStatistics(matches, histories, pankous, nil))
	if len(details) != 1 || details[0].Value != 8 {
		t.Fatalf("取整球数 = %+v, want 8（上限夹住）", details)
	}
	if details[0].ExpGoals != "期望 9.00 → 8 球" {
		t.Fatalf("明细 = %q, want 期望 9.00 → 8 球", details[0].ExpGoals)
	}
}
