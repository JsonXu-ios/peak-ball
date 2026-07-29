package handlers

import (
	"testing"
	"time"

	"go_admin/models"

	"github.com/gin-gonic/gin"
)

// 终章·大小球：两种「期望球数与球数热度反向」的组合，都买大球，各带盘口区间限制。

// goalMatch 造一场已完赛比赛（用于结算断言）。
func goalMatch(homeScore, guestScore int) statisticsMatch {
	return statisticsMatch{
		ID: "1", Date: "2026-03-01", MatchTime: "2026-03-01 20:00",
		Home: "A", Guest: "B", HomeScore: homeScore, GuestScore: guestScore, Settled: true,
	}
}

// goalPankou 造一行大小球即时盘。
func goalPankou(line string) map[string]interface{} {
	return map[string]interface{}{"dxq_data": `[{"companyId":8,"pankou":"` + line + `"}]`}
}

func TestFinaleGoalComboUnderHeatLineCeiling(t *testing.T) {
	// 反向·热度判小球 = 热度在盘口下方、期望球数在盘口上方，所以盘口必须夹在
	// 等权热度与加权期望之间。
	//
	// 入选样本：历史 0 球、近期 4 球 → 期望=0.3×0+0.7×4=2.8（判大），等权热度=2.0
	//（判小），盘口 2.5 夹在中间且 < 3.75 → 入选。
	match := statisticsMatch{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B"}
	row, ok := buildFinaleGoalRow(match, goalsHistory("A", "B", 0, 0, 2, 2), goalPankou("2.5"))
	if !ok || row.combo != finaleGoalComboUnderHeat {
		t.Fatalf("盘口2.5 = %+v/%v, want 入选 split_under", row, ok)
	}
	if row.expGoals != "判大球 2.80" || row.heatGoals != "判小球 2.00" {
		t.Fatalf("展示列 = %q / %q, want 判大球 2.80 / 判小球 2.00", row.expGoals, row.heatGoals)
	}

	// 上限样本：历史 0 球、近期 6 球 → 期望=4.2（判大）、热度=3.0（判小），
	// 盘口 3.5 与 3.75 都仍构成反向（都夹在 3.0 与 4.2 之间），但 3.75 触到上限
	// 必须剔除、3.5 必须保留——上限是「< 3.75，3.75 本身也剔除」。
	ceiling := goalsHistory("A", "B", 0, 0, 3, 3)
	if _, ok := buildFinaleGoalRow(match, ceiling, goalPankou("3.5")); !ok {
		t.Fatal("盘口3.5 应当入选：反向成立且未触到 3.75 上限")
	}
	if _, ok := buildFinaleGoalRow(match, ceiling, goalPankou("3.75")); ok {
		t.Fatal("盘口3.75 不该入选：口径是剔除 3.75 及以上")
	}
}

func TestFinaleGoalComboOverHeatLineWindow(t *testing.T) {
	// 反向·热度判大球：历史 6 球、近期 1 球 → 期望=0.3×6+0.7×1=2.5（判小），
	// 等权热度=3.5（判大）。盘口落在 2.5~3.5 之间才构成反向。
	history := goalsHistory("A", "B", 5, 1, 1, 0)
	match := statisticsMatch{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B"}

	row, ok := buildFinaleGoalRow(match, history, goalPankou("3"))
	if !ok || row.combo != finaleGoalComboOverHeat {
		t.Fatalf("盘口3 = %+v/%v, want 入选 split_over", row, ok)
	}
	if row.expGoals != "判小球 2.50" || row.heatGoals != "判大球 3.50" {
		t.Fatalf("展示列 = %q / %q, want 判小球 2.50 / 判大球 3.50", row.expGoals, row.heatGoals)
	}

	for _, line := range []string{"3.25", "3.5"} {
		if _, ok := buildFinaleGoalRow(match, history, goalPankou(line)); !ok {
			t.Fatalf("盘口%s 应当入选", line)
		}
	}

	// 高盘口同样不限：历史 10 球、近期 2 球 → 期望=0.3×10+0.7×2=4.4（判小）、
	// 热度=6.0（判大），盘口 5 仍构成反向，本组合除 2.25 外不卡任何区间。
	if _, ok := buildFinaleGoalRow(match, goalsHistory("A", "B", 5, 5, 1, 1), goalPankou("5")); !ok {
		t.Fatal("盘口5 应当入选：本组合只剔 2.25，不卡上下限")
	}
}

func TestFinaleGoalComboOverHeatExcludesOnlyTheExactLine(t *testing.T) {
	// 本组合只挖掉 2.25 这一档，两侧相邻的盘口都要留下。
	// 历史 6 球、近期 0 球 → 期望=0.3×6=1.8（判小），热度=3.0（判大），
	// 盘口 2 / 2.25 / 2.5 都夹在两者之间、都构成反向，差别只在这条剔除规则。
	history := goalsHistory("A", "B", 3, 3, 0, 0)
	match := statisticsMatch{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B"}

	for _, line := range []string{"2", "2.5"} {
		row, ok := buildFinaleGoalRow(match, history, goalPankou(line))
		if !ok || row.combo != finaleGoalComboOverHeat {
			t.Fatalf("盘口%s = %+v/%v, want 入选（只有 2.25 那一档被剔除）", line, row, ok)
		}
	}
	if _, ok := buildFinaleGoalRow(match, history, goalPankou("2.25")); ok {
		t.Fatal("盘口2.25 不该入选：这一档要单独剔除")
	}
}

func TestFinaleGoalRequiresHeadToHeadHistory(t *testing.T) {
	// 两队没有交锋记录的比赛整场剔除：期望球数 0.7 的权重压在交锋上，缺了它
	// 算出来的不是同一个口径。近期战绩照给，确认不是因为缺近期才被剔除。
	match := statisticsMatch{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B"}
	onlyRecent := map[string]interface{}{
		"against_list":      `[]`, // 无交锋
		"recent_home_list":  `[{"matchTime":"2026-02-01","home":"A","guest":"X","goal":[2,0]}]`,
		"recent_guest_list": `[{"matchTime":"2026-02-02","home":"B","guest":"Y","goal":[2,0]}]`,
	}
	for _, line := range []string{"1.5", "2.5", "3"} {
		if _, ok := buildFinaleGoalRow(match, onlyRecent, goalPankou(line)); ok {
			t.Fatalf("盘口%s：无交锋记录的比赛不该入选", line)
		}
	}
	// 补上交锋后同一个盘口就该入选，证明上面是被交锋缺失挡的。
	if _, ok := buildFinaleGoalRow(match, goalsHistory("A", "B", 0, 0, 2, 2), goalPankou("2.5")); !ok {
		t.Fatal("有交锋记录时应当入选")
	}
}

func TestFinaleGoalSkipsSameDirection(t *testing.T) {
	// 同向的比赛一律不要：历史 4 球、近期 4 球 → 期望与热度都是 4.0，盘口 2.5，
	// 两者都判大球，不构成反向。
	history := goalsHistory("A", "B", 3, 1, 2, 2)
	match := statisticsMatch{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B"}
	if _, ok := buildFinaleGoalRow(match, history, goalPankou("2.5")); ok {
		t.Fatal("同向场次不该入选，本模块只收反向组合")
	}
}

func TestSettleFinaleGoalRecordBuysOver(t *testing.T) {
	line := 2.5
	// 打出大球（3 球 > 2.5）→ 命中。
	hit := models.FinaleGoalPrediction{Combo: finaleGoalComboOverHeat, Pick: "over", OuLineValue: &line}
	fields := settleFinaleGoalRecord(&hit, goalMatch(2, 1), time.Now())
	if hit.HitPick == nil || !*hit.HitPick {
		t.Fatalf("3球 hit_pick = %v, want true", hit.HitPick)
	}
	if hit.Result != "over" || fields["result"] != "over" {
		t.Fatalf("result = %q, want over", hit.Result)
	}

	// 打出小球（1 球 < 2.5）→ 未命中，但必须是 false 而不是 nil。
	miss := models.FinaleGoalPrediction{Combo: finaleGoalComboUnderHeat, Pick: "over", OuLineValue: &line}
	settleFinaleGoalRecord(&miss, goalMatch(1, 0), time.Now())
	if miss.HitPick == nil || *miss.HitPick {
		t.Fatalf("1球 hit_pick = %v, want false", miss.HitPick)
	}
}

func TestSettleFinaleGoalRecordPushIsNotAMiss(t *testing.T) {
	// 走盘（总进球正好等于整数盘口）不算没中，必须留 nil 不进分母。
	line := 3.0
	push := models.FinaleGoalPrediction{Combo: finaleGoalComboOverHeat, Pick: "over", OuLineValue: &line}
	fields := settleFinaleGoalRecord(&push, goalMatch(2, 1), time.Now())
	if push.HitPick != nil {
		t.Fatalf("走盘 hit_pick = %v, want nil", *push.HitPick)
	}
	if fields["hit_pick"] != (*bool)(nil) {
		t.Fatalf("入库字段 hit_pick = %v, want nil（必须真的写 NULL）", fields["hit_pick"])
	}
	if push.Result != "" {
		t.Fatalf("走盘 result = %q, want 空", push.Result)
	}
}

func TestFinaleGoalAccuracySplitsByCombo(t *testing.T) {
	yes, no := true, false
	records := []models.FinaleGoalPrediction{
		{Combo: finaleGoalComboUnderHeat, Settled: true, HitPick: &yes},
		{Combo: finaleGoalComboUnderHeat, Settled: true, HitPick: &no},
		{Combo: finaleGoalComboOverHeat, Settled: true, HitPick: &yes},
		{Combo: finaleGoalComboOverHeat, Settled: true, HitPick: nil}, // 走盘，不进分母
	}
	accuracy := finaleGoalAccuracyOf(records)
	columns, ok := accuracy["columns"].([]gin.H)
	if !ok {
		t.Fatalf("columns 类型 = %T, want []gin.H", accuracy["columns"])
	}
	got := map[string][2]int{}
	for _, column := range columns {
		key, _ := column["key"].(string)
		matched, _ := column["matched"].(int)
		hit, _ := column["hit"].(int)
		got[key] = [2]int{matched, hit}
	}
	// 合计 3 场命中 2（走盘那场不计分母）；两个组合各自 2/1 与 1/1。
	if got["all"] != [2]int{3, 2} {
		t.Fatalf("合计 = %v, want [3 2]", got["all"])
	}
	if got[finaleGoalComboUnderHeat] != [2]int{2, 1} {
		t.Fatalf("split_under = %v, want [2 1]", got[finaleGoalComboUnderHeat])
	}
	if got[finaleGoalComboOverHeat] != [2]int{1, 1} {
		t.Fatalf("split_over = %v, want [1 1]", got[finaleGoalComboOverHeat])
	}
}
