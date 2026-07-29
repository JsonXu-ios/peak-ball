package handlers

import "testing"

// 待赛列表必须只留「还没开赛」的场次。库里的 settled 要等结果拉回来才置位，
// 凌晨踢完、结果还没回填的比赛在此之前 settled 仍是 0——只按日期筛的话它会一直
// 挂在待赛里，看着像还能下注的场次。
func TestFinaleIsUpcomingDropsKickedOffMatches(t *testing.T) {
	today, horizon, cutoff := "2026-07-29", "2026-08-12", "2026-07-29 11:40"

	cases := []struct {
		name  string
		match statisticsMatch
		want  bool
	}{
		{"今天稍后开赛", statisticsMatch{ID: "1", Date: "2026-07-29", MatchTime: "2026-07-29 20:00"}, true},
		{"今天凌晨已开赛、结果未回填", statisticsMatch{ID: "2", Date: "2026-07-29", MatchTime: "2026-07-29 02:00"}, false},
		{"刚好卡在当前时刻", statisticsMatch{ID: "3", Date: "2026-07-29", MatchTime: "2026-07-29 11:40"}, false},
		{"明天开赛", statisticsMatch{ID: "4", Date: "2026-07-30", MatchTime: "2026-07-30 03:00"}, true},
		{"已结算", statisticsMatch{ID: "5", Date: "2026-07-29", MatchTime: "2026-07-29 20:00", Settled: true}, false},
		{"超出窗口", statisticsMatch{ID: "6", Date: "2026-09-01", MatchTime: "2026-09-01 20:00"}, false},
		{"昨天的比赛", statisticsMatch{ID: "7", Date: "2026-07-28", MatchTime: "2026-07-28 20:00"}, false},
		// 开赛时间缺失时无法判断，保守放行，交给日期窗口兜底。
		{"无开赛时间", statisticsMatch{ID: "8", Date: "2026-07-29"}, true},
	}
	for _, item := range cases {
		if got := finaleIsUpcoming(item.match, today, horizon, cutoff); got != item.want {
			t.Errorf("%s: finaleIsUpcoming = %v, want %v", item.name, got, item.want)
		}
	}
}
