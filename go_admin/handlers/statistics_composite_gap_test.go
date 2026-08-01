package handlers

import "testing"

// #32 球数综合均值±0.75 的分段判断：±0.75 以内常规比较，超出则反向。
// gap = 盘口 − 综合均值（为正表示均值低于盘口）。
func TestStatisticsCompositeGapPick(t *testing.T) {
	cases := []struct {
		name      string
		line      float64
		composite float64
		wantOver  bool
		wantOK    bool
	}{
		// 常规段：差值在 ±0.75 以内，均值在盘口哪边就判哪边。
		{"盘口2.5 均值2.25 → 差+0.25 常规判小", 2.5, 2.25, false, true},
		{"盘口2.5 均值2.75 → 差-0.25 常规判大", 2.5, 2.75, true, true},
		{"盘口2.5 均值1.76 → 差+0.74 仍在常规段判小", 2.5, 1.76, false, true},
		{"盘口2.5 均值3.24 → 差-0.74 仍在常规段判大", 2.5, 3.24, true, true},
		// 反向段：差值绝对值到 0.75 就翻面。边界 0.75 算已超出。
		{"盘口2.5 均值1.75 → 差+0.75 反向判大", 2.5, 1.75, true, true},
		{"盘口2.5 均值1.5 → 差+1.00 反向判大", 2.5, 1.5, true, true},
		{"盘口2.5 均值3.25 → 差-0.75 反向判小", 2.5, 3.25, false, true},
		{"盘口2.5 均值4.2 → 差-1.70 反向判小", 2.5, 4.2, false, true},
		// 均值正好压在盘口上：没有方向，不纳入统计。
		{"盘口2.5 均值2.5 → 无方向", 2.5, 2.5, false, false},
	}
	for _, item := range cases {
		gotOver, gotOK := statisticsCompositeGapPick(item.line - item.composite)
		if gotOK != item.wantOK {
			t.Errorf("%s: ok = %v, want %v", item.name, gotOK, item.wantOK)
			continue
		}
		if gotOK && gotOver != item.wantOver {
			t.Errorf("%s: 判%s, want 判%s", item.name,
				statisticsOverLabel(gotOver), statisticsOverLabel(item.wantOver))
		}
	}
}

// 常规段与反向段必须在 |差值|=statisticsCompositeGapBand 处正好换边，否则边界
// 附近会判反。这里按常量取值，调分界时这条测试跟着走、不用改。
func TestStatisticsCompositeGapFlipsAtBand(t *testing.T) {
	band := statisticsCompositeGapBand
	just := band - 0.01 // 差一点点没到分界

	if over, _ := statisticsCompositeGapPick(just); over {
		t.Errorf("差值 +%.2f 应仍在常规段判小球", just)
	}
	if over, _ := statisticsCompositeGapPick(band); !over {
		t.Errorf("差值 +%.2f 应进入反向段判大球", band)
	}
	if over, _ := statisticsCompositeGapPick(-just); !over {
		t.Errorf("差值 -%.2f 应仍在常规段判大球", just)
	}
	if over, _ := statisticsCompositeGapPick(-band); over {
		t.Errorf("差值 -%.2f 应进入反向段判小球", band)
	}
}
