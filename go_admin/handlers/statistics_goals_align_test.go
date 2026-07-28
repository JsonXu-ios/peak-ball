package handlers

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// 完赛基础统计 #15~#18：期望球数(0.7×历史+0.3×近期)与球数热度(等权均值)的方向
// 组合，按 同向/反向 × 判大/判小 拆成四行，推荐一律跟热度。

func findStatSignal(report gin.H, key string) gin.H {
	signals, ok := report["signals"].([]gin.H)
	if !ok {
		return nil
	}
	for _, sig := range signals {
		if sig["key"] == key {
			return sig
		}
	}
	return nil
}

func statSignal(t *testing.T, report gin.H, key string) gin.H {
	t.Helper()
	sig := findStatSignal(report, key)
	if sig == nil {
		t.Fatalf("signal %q not found", key)
	}
	return sig
}

// goalsHistory 造交锋与近期战绩：交锋 1 场共 againstHome+againstGuest 球，主客近期
// 各 1 场共 recentHome+recentGuest 球，于是历史场均/近期场均就是这两个和。
func goalsHistory(home, guest string, againstHome, againstGuest, recentHome, recentGuest int) map[string]interface{} {
	num := func(v int) string { return string(rune('0' + v)) }
	return map[string]interface{}{
		"against_list":      `[{"matchTime":"2025-06-01","home":"` + home + `","guest":"` + guest + `","goal":[` + num(againstHome) + `,` + num(againstGuest) + `]}]`,
		"recent_home_list":  `[{"matchTime":"2026-02-01","home":"` + home + `","guest":"X","goal":[` + num(recentHome) + `,` + num(recentGuest) + `]}]`,
		"recent_guest_list": `[{"matchTime":"2026-02-02","home":"` + guest + `","guest":"Y","goal":[` + num(recentHome) + `,` + num(recentGuest) + `]}]`,
	}
}

func TestGoalsAlignSplitsByHeatDirection(t *testing.T) {
	// 同向判小球（#15）：历史 2 球、近期 2 球 → 期望/热度 2.0，盘口 2.5 都判小球；
	//   实际 1-0 共 1 球打出小球 → 命中。
	// 同向判大球（#16）：历史 4 球、近期 4 球 → 期望/热度 4.0，盘口 2.5 都判大球；
	//   实际 3-1 共 4 球打出大球 → 命中。
	// 两场必须各归各行，不能挤进同一行。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 1, GuestScore: 0},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 3, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{
		"1": goalsHistory("A", "B", 1, 1, 1, 1),
		"2": goalsHistory("C", "D", 3, 1, 2, 2),
	}
	line := map[string]interface{}{"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`}
	pankous := map[string]map[string]interface{}{"1": line, "2": line}
	report := buildSignalStatistics(matches, histories, pankous, nil)

	under := statSignal(t, report, "goals_align_under")
	if under["matched"] != 1 || under["hit"] != 1 {
		t.Fatalf("#15 matched/hit = %v/%v, want 1/1", under["matched"], under["hit"])
	}
	underDetails, _ := under["matches"].([]statisticsDetail)
	if len(underDetails) != 1 || underDetails[0].MatchID != "1" || underDetails[0].Pick != "小球" {
		t.Fatalf("#15 明细 = %+v, want 比赛1/判小球", underDetails)
	}
	if underDetails[0].ExpGoals != "判小球 2.00" || underDetails[0].HeatGoals != "判小球 2.00" {
		t.Fatalf("#15 明细方向列 = %q / %q, want 判小球 2.00 / 判小球 2.00", underDetails[0].ExpGoals, underDetails[0].HeatGoals)
	}

	over := statSignal(t, report, "goals_align_over")
	if over["matched"] != 1 || over["hit"] != 1 {
		t.Fatalf("#16 matched/hit = %v/%v, want 1/1", over["matched"], over["hit"])
	}
	overDetails, _ := over["matches"].([]statisticsDetail)
	if len(overDetails) != 1 || overDetails[0].MatchID != "2" || overDetails[0].Pick != "大球" {
		t.Fatalf("#16 明细 = %+v, want 比赛2/判大球", overDetails)
	}
}

func TestGoalsDirRowsPartitionEveryMatch(t *testing.T) {
	// #15~#18 必须互斥且穷尽：四种组合各造一场，四行各收一场，场次加起来正好是
	// 全部样本。任何一场被漏掉或被重复计入，命中率就都不可信了。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 1, GuestScore: 0},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 3, GuestScore: 1},
		{ID: "3", Date: "2026-03-01", Home: "E", Guest: "F", HomeScore: 3, GuestScore: 1},
		{ID: "4", Date: "2026-03-01", Home: "G", Guest: "H", HomeScore: 3, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{
		"1": goalsHistory("A", "B", 1, 1, 1, 1), // 同向判小：期望/热度 2.0，盘口 2.5
		"2": goalsHistory("C", "D", 3, 1, 2, 2), // 同向判大：期望/热度 4.0，盘口 2.5
		"3": goalsHistory("E", "F", 1, 0, 3, 3), // 反向判大：期望 2.5 / 热度 3.5，盘口 3.25
		"4": goalsHistory("G", "H", 5, 1, 1, 0), // 反向判小：期望 4.5 / 热度 3.5，盘口 3.75
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
		"2": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
		"3": {"dxq_data": `[{"companyId":8,"pankou":"3.25"}]`},
		"4": {"dxq_data": `[{"companyId":8,"pankou":"3.75"}]`},
	}
	report := buildSignalStatistics(matches, histories, pankous, nil)

	seen := map[string]string{}
	for _, key := range []string{"goals_align_under", "goals_align_over", "goals_split_over", "goals_split_under"} {
		sig := statSignal(t, report, key)
		if sig["matched"] != 1 {
			t.Fatalf("%s matched = %v, want 1", key, sig["matched"])
		}
		details, _ := sig["matches"].([]statisticsDetail)
		for _, d := range details {
			if prev, dup := seen[d.MatchID]; dup {
				t.Fatalf("比赛 %s 同时进了 %s 和 %s，四行必须互斥", d.MatchID, prev, key)
			}
			seen[d.MatchID] = key
		}
	}
	if len(seen) != len(matches) {
		t.Fatalf("四行共覆盖 %d 场，want %d（不能漏场）", len(seen), len(matches))
	}
}

func TestGoalsSplitFollowsHeatOnBothSides(t *testing.T) {
	// 反向且热度判大球（#17）：历史 1 球、近期 6 球 → 期望=0.7×1+0.3×6=2.5（判小），
	// 等权热度=3.5（判大），盘口 3.25 夹在中间。推荐跟热度=大球；实际 3-1 共 4 球
	// 打出大球 → 命中。
	// 反向且热度判小球（#18）：历史 6 球、近期 1 球 → 期望 4.5（判大）、热度 3.5
	//（判小），盘口 3.75。推荐跟热度=小球；实际 4 球打出大球 → 未命中。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 3, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{
		"1": goalsHistory("A", "B", 1, 0, 3, 3),
		"2": goalsHistory("C", "D", 5, 1, 1, 0),
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"3.25"}]`},
		"2": {"dxq_data": `[{"companyId":8,"pankou":"3.75"}]`},
	}
	report := buildSignalStatistics(matches, histories, pankous, nil)

	over := statSignal(t, report, "goals_split_over")
	if over["matched"] != 1 || over["hit"] != 1 {
		t.Fatalf("#17 matched/hit = %v/%v, want 1/1", over["matched"], over["hit"])
	}
	overDetails, _ := over["matches"].([]statisticsDetail)
	if len(overDetails) != 1 || overDetails[0].MatchID != "1" || overDetails[0].Pick != "大球" {
		t.Fatalf("#17 明细 = %+v, want 比赛1/判大球", overDetails)
	}
	// 数值列跟推荐走（热度值 3.5），不是期望球数的 3.0。
	if overDetails[0].Value != 3.5 {
		t.Fatalf("#17 数值 = %v, want 3.5（热度值）", overDetails[0].Value)
	}
	// 期望球数按 0.7/0.3 加权 = 2.50；这个数字直接体现用的是哪套权重。
	if overDetails[0].ExpGoals != "判小球 2.50" || overDetails[0].HeatGoals != "判大球 3.50" {
		t.Fatalf("#17 明细方向列 = %q / %q, want 判小球 2.50 / 判大球 3.50", overDetails[0].ExpGoals, overDetails[0].HeatGoals)
	}

	under := statSignal(t, report, "goals_split_under")
	if under["matched"] != 1 || under["hit"] != 0 {
		t.Fatalf("#18 matched/hit = %v/%v, want 1/0", under["matched"], under["hit"])
	}
	underDetails, _ := under["matches"].([]statisticsDetail)
	if len(underDetails) != 1 || underDetails[0].MatchID != "2" || underDetails[0].Pick != "小球" {
		t.Fatalf("#18 明细 = %+v, want 比赛2/判小球", underDetails)
	}
	// 同向两行不能把反向的场次收进去。
	for _, key := range []string{"goals_align_under", "goals_align_over"} {
		if sig := statSignal(t, report, key); sig["matched"] != 0 {
			t.Fatalf("%s matched = %v, want 0", key, sig["matched"])
		}
	}
}

func TestRetiredGoalsSignalsAreGone(t *testing.T) {
	// 13b~13k 那一整套（含 0.45/0.55 旧口径、主推分档、按命中率筛盘口）已全部撤掉，
	// 只保留 #13 热度分档；留着会让人以为多套口径还并存。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{"1": goalsHistory("A", "B", 4, 0, 2, 1)}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"3.25"}]`},
	}
	report := buildSignalStatistics(matches, histories, pankous, nil)
	for _, key := range []string{
		"front_goals_avg",
		"goals_align", "goals_align_base65", "goals_align_base70",
		"goals_split", "goals_split_base65", "goals_split_base70",
		"goals_align_w64", "goals_align_w64_base65", "goals_align_w64_base70",
		"goals_split_w64", "goals_split_w64_base65", "goals_split_w64_base70",
		"goals_align_w64_hot_over", "goals_align_w64_hot_under",
		"goals_split_w64_hot_over", "goals_split_w64_hot_under",
	} {
		if sig := findStatSignal(report, key); sig != nil {
			t.Fatalf("已下线的信号 %q 仍在输出里", key)
		}
	}
	// #13 热度分档必须留着。
	if sig := findStatSignal(report, "goals_heat"); sig == nil {
		t.Fatal("#13 大小球投注热度分档不应被删掉")
	}
}
