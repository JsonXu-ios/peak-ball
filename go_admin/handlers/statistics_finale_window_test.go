package handlers

import "testing"

// 窗口只按日期划：今天的比赛不管开没开赛、完没完赛都留下（早先会把已开赛/已完赛
// 的剔掉，导致今天的列表踢一场少一场，到晚上基本清空）。
func TestFinaleInWindowKeepsWholeDay(t *testing.T) {
	today, horizon := "2026-07-29", "2026-08-12"

	cases := []struct {
		name  string
		match statisticsMatch
		want  bool
	}{
		{"今天稍后开赛", statisticsMatch{ID: "1", Date: "2026-07-29", MatchTime: "2026-07-29 20:00"}, true},
		{"今天凌晨已开赛", statisticsMatch{ID: "2", Date: "2026-07-29", MatchTime: "2026-07-29 02:00"}, true},
		{"今天已完赛", statisticsMatch{ID: "3", Date: "2026-07-29", MatchTime: "2026-07-29 02:00", Settled: true}, true},
		{"无开赛时间", statisticsMatch{ID: "4", Date: "2026-07-29"}, true},
		{"窗口内的未来某天", statisticsMatch{ID: "5", Date: "2026-08-12", MatchTime: "2026-08-12 03:00"}, true},
		{"超出窗口", statisticsMatch{ID: "6", Date: "2026-09-01", MatchTime: "2026-09-01 20:00"}, false},
		{"昨天的比赛", statisticsMatch{ID: "7", Date: "2026-07-28", MatchTime: "2026-07-28 20:00"}, false},
		{"没有比赛ID", statisticsMatch{Date: "2026-07-29"}, false},
	}
	for _, item := range cases {
		if got := finaleInWindow(item.match, today, horizon); got != item.want {
			t.Errorf("%s: finaleInWindow = %v, want %v", item.name, got, item.want)
		}
	}
}
