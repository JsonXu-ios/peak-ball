package handlers

import "testing"

func detail(pick, result string) statisticsDetail {
	return statisticsDetail{Pick: pick, Result: result, Hit: pick == result}
}

func TestStatisticsPankouRowsNoAsianFallbackForDxq(t *testing.T) {
	// dxq_data 缺失、asia_data 是普通亚盘数组时，绝不能把亚盘行当大小球行
	//（否则 0.5 半球会被当成大小球线去结算）。
	row := map[string]interface{}{
		"asia_data": `[{"companyId":8,"pankou":"0.5"}]`,
	}
	if line, ok := statisticsPankouLine(row, "bet365_dxq", "dxq_data"); ok {
		t.Fatalf("dxq line = %v from asian rows, want none", line)
	}
	// 旧组合格式 asia_data={asia:[...],dxq:[...]} 仍然可用。
	combined := map[string]interface{}{
		"asia_data": `{"asia":[{"companyId":8,"pankou":"0.5"}],"dxq":[{"companyId":8,"pankou":"2.5"}]}`,
	}
	if line, ok := statisticsPankouLine(combined, "bet365_dxq", "dxq_data"); !ok || line != 2.5 {
		t.Fatalf("combined dxq line = %v/%v, want 2.5/true", line, ok)
	}
	if line, ok := statisticsPankouLine(row, "bet365_asia", "asia_data"); !ok || line != 0.5 {
		t.Fatalf("asia line = %v/%v, want 0.5/true", line, ok)
	}
}

func TestStatisticsPankouLineMedianFallback(t *testing.T) {
	// 真实场景（match 498116085）：bet365 缺席，第一行是立博的 0.5 离群脏行，
	// 其余公司都在 2/2.25/2.5。兜底必须取中位数 2.25，绝不能取第一行 0.5。
	row := map[string]interface{}{
		"asia_data": `{"dxq":[
			{"companyId":4,"pankou":"半球"},
			{"companyId":14,"pankou":"二球/二球半"},
			{"companyId":17,"pankou":"二球/二球半"},
			{"companyId":31,"pankou":"二球"},
			{"companyId":9,"pankou":"二球半"},
			{"companyId":24,"pankou":"二球/二球半"},
			{"companyId":3,"pankou":"二球/二球半"}
		]}`,
	}
	line, ok := statisticsPankouLine(row, "bet365_dxq", "dxq_data")
	if !ok || line != 2.25 {
		t.Fatalf("median fallback line = %v/%v, want 2.25/true", line, ok)
	}
	// bet365 在场时仍然优先 bet365。
	withBet365 := map[string]interface{}{
		"asia_data": `{"dxq":[{"companyId":4,"pankou":"半球"},{"companyId":8,"pankou":"二球半"}]}`,
	}
	if line, ok := statisticsPankouLine(withBet365, "bet365_dxq", "dxq_data"); !ok || line != 2.5 {
		t.Fatalf("bet365 line = %v/%v, want 2.5/true", line, ok)
	}
}

func TestStatisticsKellySportterySingleChoice(t *testing.T) {
	// 构造一场凯利在主胜、平局两个方向同时有价值，且威廉-体彩差值都在容差内的比赛：
	// 旧算法会给出 主胜/平局 双选；新算法必须只保留一个首选方向。
	row := map[string]interface{}{
		"avg_odds": map[string]interface{}{"odds": []interface{}{2.10, 3.40, 3.60}},
		"pinnacle": map[string]interface{}{"odds": []interface{}{2.00, 3.25, 3.90}},
		"william":  map[string]interface{}{"odds": []interface{}{2.10, 3.40, 3.60}},
	}
	choices := statisticsKellySportteryChoices(row)
	if len(choices) > 1 {
		t.Fatalf("choices = %v, want at most one direction", choices)
	}
}

func TestStatisticsDirectionBreakdown(t *testing.T) {
	rows := statisticsDirectionBreakdown([]statisticsDetail{
		detail("客胜", "客胜"), detail("客胜", "主胜"),
		detail("主胜", "主胜"), detail("主胜", "主胜"), detail("主胜", "平局"),
		detail("平局", "平局"),
	})
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	// 按 胜→平→负 顺序
	if rows[0]["pick"] != "主胜" || rows[1]["pick"] != "平局" || rows[2]["pick"] != "客胜" {
		t.Fatalf("order = %v %v %v", rows[0]["pick"], rows[1]["pick"], rows[2]["pick"])
	}
	if rows[0]["matched"].(int) != 3 || rows[0]["hit"].(int) != 2 {
		t.Fatalf("主胜 matched/hit = %v/%v, want 3/2", rows[0]["matched"], rows[0]["hit"])
	}
	if rows[0]["accuracy"].(float64) != 66.67 {
		t.Fatalf("主胜 accuracy = %v, want 66.67", rows[0]["accuracy"])
	}
	if rows[2]["accuracy"].(float64) != 50 {
		t.Fatalf("客胜 accuracy = %v, want 50", rows[2]["accuracy"])
	}
}

func lineDetail(pick, line, result string) statisticsDetail {
	return statisticsDetail{Pick: pick, Line: line, Result: result, Hit: pick == result}
}

func TestStatisticsDirectionBreakdownByLine(t *testing.T) {
	// 盘口类信号：按 方向×盘口线 拆行，同方向内盘口线升序。
	rows := statisticsDirectionBreakdown([]statisticsDetail{
		lineDetail("小球", "2.5", "小球"),
		lineDetail("大球", "3", "小球"),
		lineDetail("大球", "2.5", "大球"),
		lineDetail("大球", "2.5", "小球"),
		lineDetail("小球", "2.25", "大球"),
	})
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(rows))
	}
	type want struct {
		pick, line string
		matched    int
	}
	wants := []want{{"大球", "2.5", 2}, {"大球", "3", 1}, {"小球", "2.25", 1}, {"小球", "2.5", 1}}
	for i, w := range wants {
		if rows[i]["pick"] != w.pick || rows[i]["line"] != w.line || rows[i]["matched"].(int) != w.matched {
			t.Fatalf("row %d = %v/%v/%v, want %v/%v/%v", i, rows[i]["pick"], rows[i]["line"], rows[i]["matched"], w.pick, w.line, w.matched)
		}
	}
	if rows[0]["accuracy"].(float64) != 50 {
		t.Fatalf("大球@2.5 accuracy = %v, want 50", rows[0]["accuracy"])
	}
}
