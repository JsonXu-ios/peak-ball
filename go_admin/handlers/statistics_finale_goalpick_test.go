package handlers

import "testing"

// 终章胜平负页的「大小球推荐」列：期望球数原值与截尾取整双双判大球才推荐买大球，
// 盘口正好 4 球时反推买小球。

// goalPickOf 造一场比赛跑 buildFinaleRow，取出大小球推荐列的文本。
func goalPickOf(t *testing.T, history map[string]interface{}, line string) string {
	t.Helper()
	match := statisticsMatch{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B"}
	pankou := map[string]interface{}{"dxq_data": `[{"companyId":8,"pankou":"` + line + `"}]`}
	return buildFinaleRow(match, history, pankou, nil).goalPick
}

func TestFinaleGoalPickNeedsBothJudgementsOver(t *testing.T) {
	// 交锋 4 球、近期 4 球 → 期望 4.0，截尾 4。
	//   盘口 2.5：4.0 > 2.5 且 4 > 2.5 → 双双判大 → 买大球
	//   盘口 3.75：4.0 > 3.75 但截尾 4 > 3.75 也成立 → 买大球
	both := goalsHistory("A", "B", 3, 1, 2, 2)
	if pick := goalPickOf(t, both, "2.5"); pick != "买大球" {
		t.Fatalf("盘口2.5 推荐 = %q, want 买大球", pick)
	}

	// 交锋 4 球、近期 2 球 → 期望 2.6，截尾 2。盘口 2.5：原值判大、截尾判小，
	// 不构成「双双判大」→ 不推荐。这正是加截尾条件要挡掉的压线场次。
	edge := goalsHistory("A", "B", 3, 1, 1, 1)
	if pick := goalPickOf(t, edge, "2.5"); pick != "" {
		t.Fatalf("压线场次推荐 = %q, want 空（截尾后判小球）", pick)
	}

	// 期望低于盘口，两条都不成立 → 不推荐。
	if pick := goalPickOf(t, both, "4.5"); pick != "" {
		t.Fatalf("期望低于盘口时推荐 = %q, want 空", pick)
	}
}

func TestFinaleGoalPickFlipsOnFourBallLine(t *testing.T) {
	// 交锋 6 球、近期 6 球 → 期望 6.0、截尾 6，盘口正好 4 球：
	// 双双判大球成立，但 4 球盘要反过来推荐买小球。
	history := goalsHistory("A", "B", 3, 3, 3, 3)
	if pick := goalPickOf(t, history, "4"); pick != "买小球" {
		t.Fatalf("盘口4 推荐 = %q, want 买小球", pick)
	}
	// 相邻盘口不受影响，仍是买大球——确认反转只针对 4 球这一档。
	for _, line := range []string{"3.75", "4.25"} {
		if pick := goalPickOf(t, history, line); pick != "买大球" {
			t.Fatalf("盘口%s 推荐 = %q, want 买大球", line, pick)
		}
	}
}

func TestFinaleGoalPickRequiresHeadToHead(t *testing.T) {
	// 无交锋记录时期望球数不成立，不推荐——与其它期望球数口径一致。
	noHistory := map[string]interface{}{
		"against_list":      `[]`,
		"recent_home_list":  `[{"matchTime":"2026-02-01","home":"A","guest":"X","goal":[3,3]}]`,
		"recent_guest_list": `[{"matchTime":"2026-02-02","home":"B","guest":"Y","goal":[3,3]}]`,
	}
	if pick := goalPickOf(t, noHistory, "2.5"); pick != "" {
		t.Fatalf("无交锋记录推荐 = %q, want 空", pick)
	}
}
