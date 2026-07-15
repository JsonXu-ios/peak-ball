package handlers

import "testing"

// TestStatisticsPankouLineObjectForm covers the legacy storage shape where the
// crawler packed both markets into asia_data as {"asia":[...],"dxq":[...]} and left
// dxq_data null. Both the Asian and O/U lines must still resolve.
func TestStatisticsPankouLineObjectForm(t *testing.T) {
	raw := `{"asia":[{"companyId":1,"pankou":"受一球半/二球"}],"dxq":[{"companyId":1,"pankou":"二球半"}]}`
	row := map[string]interface{}{"asia_data": []byte(raw), "dxq_data": nil}

	ah, ok := statisticsPankouLine(row, "bet365_asia", "asia_data")
	if !ok || ah != -1.75 {
		t.Errorf("asia line = %v (ok=%v), want -1.75", ah, ok)
	}
	ou, ok := statisticsPankouLine(row, "bet365_dxq", "dxq_data")
	if !ok || ou != 2.5 {
		t.Errorf("ou line = %v (ok=%v), want 2.5", ou, ok)
	}
}

func TestStatisticsLineResolvesRealValues(t *testing.T) {
	cases := map[string]float64{
		// 亚盘 (asia) values observed in pankou_moneys
		"平手/半球": 0.25, "半球/一球": 0.75, "平手": 0, "受平手/半球": -0.25,
		"一球": 1, "一球/一球半": 1.25, "半球": 0.5, "受半球/一球": -0.75,
		"受一球/一球半": -1.25, "二球": 2, "受一球": -1, "一球半": 1.5,
		"受一球半": -1.5, "二球半/三球": 2.75, "受一球半/二球": -1.75,
		"受二球/二球半": -2.25, "一球半/二球": 1.75,
		// 大小球 (dxq) values observed in pankou_moneys
		"二球半": 2.5, "二球/二球半": 2.25, "三球半": 3.5, "三球": 3,
		"三球/三球半": 3.25, "三球半/四球": 3.75, "四球/四球半": 4.25,
	}
	for raw, want := range cases {
		got, ok := statisticsLine(raw)
		if !ok {
			t.Errorf("statisticsLine(%q) returned ok=false, want %v", raw, want)
			continue
		}
		if got != want {
			t.Errorf("statisticsLine(%q) = %v, want %v", raw, got, want)
		}
	}
}
