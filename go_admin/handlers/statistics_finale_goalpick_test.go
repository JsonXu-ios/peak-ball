package handlers

import "testing"

// 终章胜平负页的两列大小球推荐，口径同「完赛基础统计」#32/#33，且只推小球：
//
//	差值 = 大小球盘口 − 球数综合均值（历史场均与近期场均等权）
//	超出带（|差值| ≥ 0.75）：差值 ≤ −0.75 → 买小球；差值 ≥ +0.75 判大球，不推荐。
//	带内（|差值| < 0.75）：差值 > 0（均值低于盘口）→ 买小球；差值 < 0 判大球，不推荐。

// goalPicksOf 造一场比赛跑 buildFinaleRow，取出两列推荐文本。
func goalPicksOf(t *testing.T, history map[string]interface{}, line string) (outer, mid string) {
	t.Helper()
	match := statisticsMatch{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B"}
	pankou := map[string]interface{}{"dxq_data": `[{"companyId":8,"pankou":"` + line + `"}]`}
	row := buildFinaleRow(match, history, pankou, nil)
	return row.goalPick, row.goalPickMid
}

func TestFinaleGoalPickOnlyRecommendsUnder(t *testing.T) {
	// 交锋 2 球、近期 2 球 → 综合均值 2.0。
	average2 := goalsHistory("A", "B", 1, 1, 1, 1)

	cases := []struct {
		name      string
		line      string
		wantOuter string
		wantMid   string
	}{
		{"盘口3 差+1.00 超出带判大球 → 都不推荐", "3", "", ""},
		{"盘口2.75 差+0.75 超出带判大球 → 都不推荐", "2.75", "", ""},
		{"盘口2.74 差+0.74 带内均值低于盘口 → 带内买小球", "2.74", "", "买小球"},
		{"盘口2.5 差+0.50 带内 → 带内买小球", "2.5", "", "买小球"},
		{"盘口2 差 0 均值压线 → 都不推荐", "2", "", ""},
		{"盘口1.5 差-0.50 带内判大球 → 都不推荐", "1.5", "", ""},
		{"盘口1.26 差-0.74 带内判大球 → 都不推荐", "1.26", "", ""},
		{"盘口1.25 差-0.75 超出带 → 超出带买小球", "1.25", "买小球", ""},
		{"盘口1 差-1.00 超出带 → 超出带买小球", "1", "买小球", ""},
	}
	for _, item := range cases {
		outer, mid := goalPicksOf(t, average2, item.line)
		if outer != item.wantOuter || mid != item.wantMid {
			t.Errorf("%s: 超出带=%q 带内=%q, want 超出带=%q 带内=%q",
				item.name, outer, mid, item.wantOuter, item.wantMid)
		}
	}

	// 两列永远不会同时有值。
	for _, line := range []string{"1", "1.25", "1.5", "2", "2.5", "2.74", "2.75", "3"} {
		if outer, mid := goalPicksOf(t, average2, line); outer != "" && mid != "" {
			t.Errorf("盘口%s 两列都有值（%q / %q），应互斥", line, outer, mid)
		}
	}
}

// #32/#33 用等权综合均值，只要历史与近期有一侧有样本就能算——不要求有交锋记录。
func TestFinaleGoalPickWorksWithoutHeadToHead(t *testing.T) {
	noHistory := map[string]interface{}{
		"against_list":      `[]`,
		"recent_home_list":  `[{"matchTime":"2026-02-01","home":"A","guest":"X","goal":[3,3]}]`,
		"recent_guest_list": `[{"matchTime":"2026-02-02","home":"B","guest":"Y","goal":[3,3]}]`,
	}
	// 只有近期：均值 6.0，盘口 2.5 → 差 −3.5 → 超出带买小球。
	if outer, mid := goalPicksOf(t, noHistory, "2.5"); outer != "买小球" || mid != "" {
		t.Errorf("无交锋记录时 超出带=%q 带内=%q, want 买小球 / 空", outer, mid)
	}
	// 两侧都没有样本就算不出均值，两列都空。
	empty := map[string]interface{}{"against_list": `[]`, "recent_home_list": `[]`, "recent_guest_list": `[]`}
	if outer, mid := goalPicksOf(t, empty, "2.5"); outer != "" || mid != "" {
		t.Errorf("无任何样本时 超出带=%q 带内=%q, want 都空", outer, mid)
	}
}
