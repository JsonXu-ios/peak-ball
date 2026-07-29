package handlers

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// #13c 前端球数倾向分档：公式与 #13 大小球热度一致，唯一差别是驱动值只看近期
// 战绩、不含交锋。两行必须各算各的，绝不能互相串味。

func TestRecentTotalGoalsIgnoresHeadToHead(t *testing.T) {
	// 主队近期 1 场共 3 球、客队近期 1 场共 5 球 → 均值 (3+5)/2 = 4.0。
	// 交锋战绩再离谱也不影响这个数——这是它与球数热度的唯一区别。
	home := statisticsTeamForm{For: 2, Against: 1, Matches: 1}
	guest := statisticsTeamForm{For: 4, Against: 1, Matches: 1}
	value, ok := statisticsRecentTotalGoals(home, guest)
	if !ok || value != 4 {
		t.Fatalf("近期总进球均值 = %v/%v, want 4/true", value, ok)
	}
	// 两边都没有近期场次时无值可算，不能返回 0 当成「场均0球」。
	if _, ok := statisticsRecentTotalGoals(statisticsTeamForm{}, statisticsTeamForm{}); ok {
		t.Fatal("无近期场次时应当返回 false，不能拿 0 顶替")
	}
}

func TestQiuHeatBucketsByRecentOnly(t *testing.T) {
	// 交锋 0 球、近期 6 球、盘口 2.5：
	//   #13  热度用等权均值 (0+6)/2 = 3.0 → 50+(3.0-2.5)×18 = 59 → 55档 判大球
	//   #13c 倾向只看近期 6.0        → 50+(6.0-2.5)×18 = 113 → clamp 100 → 90档 判大球
	// 两个维度的档位必须不同，说明各自用了各自的驱动值。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{
		"1": {
			"against_list":      `[{"matchTime":"2025-06-01","home":"A","guest":"B","goal":[0,0]}]`,
			"recent_home_list":  `[{"matchTime":"2026-02-01","home":"A","guest":"X","goal":[3,3]}]`,
			"recent_guest_list": `[{"matchTime":"2026-02-02","home":"B","guest":"Y","goal":[3,3]}]`,
		},
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
	}
	report := buildSignalStatistics(matches, histories, pankous, nil)

	tierOf := func(key, bucketKey string) (int, int) {
		sig := statSignal(t, report, key)
		buckets, ok := sig["buckets"].([]gin.H)
		if !ok {
			t.Fatalf("%s buckets 类型 = %T", key, sig["buckets"])
		}
		for _, bucket := range buckets {
			if k, _ := bucket["key"].(string); k == bucketKey {
				matched, _ := bucket["matched"].(int)
				hit, _ := bucket["hit"].(int)
				return matched, hit
			}
		}
		return 0, 0
	}

	// 实际 3-1 共 4 球 > 2.5 打出大球，两个维度判大球都命中。
	if matched, hit := tierOf("goals_heat", "goals_heat-55-over"); matched != 1 || hit != 1 {
		t.Fatalf("#13 55档判大球 = %d/%d, want 1/1（等权均值 3.0）", matched, hit)
	}
	if matched, hit := tierOf("qiu_heat", "qiu_heat-90-over"); matched != 1 || hit != 1 {
		t.Fatalf("#13c 90档判大球 = %d/%d, want 1/1（近期均值 6.0）", matched, hit)
	}
	// 反过来确认没有互相串桶。
	if matched, _ := tierOf("qiu_heat", "qiu_heat-55-over"); matched != 0 {
		t.Fatalf("#13c 55档不该有场次 = %d", matched)
	}
}

func TestQiuDirRowsFollowTendencyAndPartition(t *testing.T) {
	// #19~#22：期望球数 vs 前端球数倾向，四种组合各造一场，推荐一律跟倾向。
	// 交锋 h、近期 r → 期望=0.3h+0.7r，倾向=r（只看近期）；盘口统一 2.5。
	//   1: h=2 r=2 → 期望 2.0 / 倾向 2.0 → 同向判小（#19）
	//   2: h=4 r=4 → 期望 4.0 / 倾向 4.0 → 同向判大（#20）
	//   3: h=0 r=3 → 期望 2.1 / 倾向 3.0 → 反向、倾向判大（#21）
	//   4: h=9 r=0 → 期望 2.7 / 倾向 0.0 → 反向、倾向判小（#22）
	// 全部 3-1 共 4 球打出大球，于是跟倾向判大的两行命中、判小的两行未命中。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 3, GuestScore: 1},
		{ID: "3", Date: "2026-03-01", Home: "E", Guest: "F", HomeScore: 3, GuestScore: 1},
		{ID: "4", Date: "2026-03-01", Home: "G", Guest: "H", HomeScore: 3, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{
		"1": goalsHistory("A", "B", 1, 1, 1, 1),
		"2": goalsHistory("C", "D", 3, 1, 2, 2),
		"3": goalsHistory("E", "F", 0, 0, 2, 1),
		"4": goalsHistory("G", "H", 5, 4, 0, 0),
	}
	line := map[string]interface{}{"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`}
	pankous := map[string]map[string]interface{}{"1": line, "2": line, "3": line, "4": line}
	report := buildSignalStatistics(matches, histories, pankous, nil)

	want := []struct {
		key     string
		matchID string
		pick    string
		hit     bool
	}{
		{"qiu_align_under", "1", "小球", false},
		{"qiu_align_over", "2", "大球", true},
		{"qiu_split_over", "3", "大球", true},
		{"qiu_split_under", "4", "小球", false},
	}
	seen := map[string]string{}
	for _, item := range want {
		sig := statSignal(t, report, item.key)
		if sig["matched"] != 1 {
			t.Fatalf("%s matched = %v, want 1", item.key, sig["matched"])
		}
		details, _ := sig["matches"].([]statisticsDetail)
		if len(details) != 1 || details[0].MatchID != item.matchID {
			t.Fatalf("%s 明细 = %+v, want 比赛%s", item.key, details, item.matchID)
		}
		if details[0].Pick != item.pick || details[0].Hit != item.hit {
			t.Fatalf("%s = %s/%v, want %s/%v（推荐必须跟倾向）", item.key, details[0].Pick, details[0].Hit, item.pick, item.hit)
		}
		// 对照信号列要带「倾向」前缀，免得和 #15~#18 的热度列混淆。
		if len(details[0].HeatGoals) < 6 || details[0].HeatGoals[:6] != "倾向" {
			t.Fatalf("%s 对照信号列 = %q, want 以「倾向」开头", item.key, details[0].HeatGoals)
		}
		if prev, dup := seen[item.matchID]; dup {
			t.Fatalf("比赛 %s 同时进了 %s 和 %s，四行必须互斥", item.matchID, prev, item.key)
		}
		seen[item.matchID] = item.key
	}
	if len(seen) != len(matches) {
		t.Fatalf("#19~#22 共覆盖 %d 场，want %d（不能漏场）", len(seen), len(matches))
	}
}
