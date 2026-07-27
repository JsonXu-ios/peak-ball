package handlers

import (
	"testing"
	"time"

	"go_admin/models"
)

func finaleMatch(homeScore, guestScore int) statisticsMatch {
	return statisticsMatch{ID: "1", HomeScore: homeScore, GuestScore: guestScore, Settled: true}
}

func hitOf(t *testing.T, fields map[string]interface{}, key string) *bool {
	t.Helper()
	value, ok := fields[key]
	if !ok {
		t.Fatalf("%s missing from settle fields", key)
	}
	hit, ok := value.(*bool)
	if !ok {
		t.Fatalf("%s = %T, want *bool", key, value)
	}
	return hit
}

func wantHit(t *testing.T, fields map[string]interface{}, key string, expected bool) {
	t.Helper()
	hit := hitOf(t, fields, key)
	if hit == nil {
		t.Fatalf("%s = nil (不适用), want %v", key, expected)
	}
	if *hit != expected {
		t.Fatalf("%s = %v, want %v", key, *hit, expected)
	}
}

func wantNA(t *testing.T, fields map[string]interface{}, key string) {
	t.Helper()
	if hit := hitOf(t, fields, key); hit != nil {
		t.Fatalf("%s = %v, want nil (不适用，不进命中率分母)", key, *hit)
	}
}

func TestSettleFinaleRecordDirections(t *testing.T) {
	ouLine, ahLine := 2.5, 0.5
	record := models.FinalePrediction{
		Pick: "home", BaseDir: "home", ExpAhDir: "away",
		OuHeatDir: "over", OuPickDir: "under", ExpOuDir: "over",
		TradeDir: "home", SimDir: "away",
		AhLineValue: &ahLine, OuLineValue: &ouLine,
	}
	// 2:1 → 主胜；净胜1 > 让0.5 → 主队赢盘；总进球3 > 2.5 → 大球。
	fields := settleFinaleRecord(&record, finaleMatch(2, 1), time.Now())

	if fields["result"] != "home" {
		t.Fatalf("result = %v, want home", fields["result"])
	}
	wantHit(t, fields, "hit_pick", true)     // 推主胜，主胜打出
	wantHit(t, fields, "hit_base", true)     // 同上
	wantHit(t, fields, "hit_exp_ah", false)  // 判客队赢盘，实际主队赢盘，没中
	wantHit(t, fields, "hit_ou_heat", true)  // 判大，大球打出
	wantHit(t, fields, "hit_ou_pick", false) // 判小，没中
	wantHit(t, fields, "hit_exp_ou", true)   // 判大，中

	// 盈亏提示按反买结算：提示主胜→买客胜，主胜打出所以没中；
	// 提示客胜→买主胜，主胜打出所以中。
	wantHit(t, fields, "hit_trade", false)
	wantHit(t, fields, "hit_sim", true)
}

func TestSettleFinaleRecordExpAhSettlesAgainstTheLineNotTheResult(t *testing.T) {
	// 这一列的核心：期望让球必须跟亚盘线比，不能按胜平负判。
	// 盘口让2，主队 2:0 赢球但只净胜2 → 走盘不算；改用 3:0 净胜3 才算赢盘。
	ahLine := 2.0

	// 期望2.10 > 盘口2 → 判主队赢盘。主队净胜3，赢盘 → 中。
	deep := models.FinalePrediction{ExpAhDir: "home", AhLineValue: &ahLine}
	wantHit(t, settleFinaleRecord(&deep, finaleMatch(3, 0), time.Now()), "hit_exp_ah", true)

	// 期望1.90 < 盘口2 → 判客队赢盘。主队只净胜1，主队输盘 → 判对了。
	// 注意主队是赢球的，按旧的胜平负口径这里会判成「没中」——正是要修的 bug。
	shallow := models.FinalePrediction{ExpAhDir: "away", AhLineValue: &ahLine}
	fields := settleFinaleRecord(&shallow, finaleMatch(1, 0), time.Now())
	if fields["result"] != "home" {
		t.Fatalf("result = %v, want home（主队确实赢球，但输盘）", fields["result"])
	}
	wantHit(t, fields, "hit_exp_ah", true)

	// 净胜球正好等于盘口 → 走盘，不适用。
	push := models.FinalePrediction{ExpAhDir: "home", AhLineValue: &ahLine}
	wantNA(t, settleFinaleRecord(&push, finaleMatch(2, 0), time.Now()), "hit_exp_ah")

	// 没存下亚盘线 → 无从判断，不适用。
	noLine := models.FinalePrediction{ExpAhDir: "home"}
	wantNA(t, settleFinaleRecord(&noLine, finaleMatch(3, 0), time.Now()), "hit_exp_ah")
}

func TestSettleFinaleRecordPushIsNotAMiss(t *testing.T) {
	// 盘口 3.0、总进球正好 3 → 走盘。三个大小球列都必须留 nil，
	// 否则会被当成「算了但没中」拉低命中率。
	line := 3.0
	record := models.FinalePrediction{
		Pick: "home", BaseDir: "home",
		OuHeatDir: "over", OuPickDir: "under", ExpOuDir: "over",
		OuLineValue: &line,
	}
	fields := settleFinaleRecord(&record, finaleMatch(2, 1), time.Now())

	wantHit(t, fields, "hit_pick", true)
	wantNA(t, fields, "hit_ou_heat")
	wantNA(t, fields, "hit_ou_pick")
	wantNA(t, fields, "hit_exp_ou")
}

func TestSettleFinaleRecordEmptySignalsStayNil(t *testing.T) {
	// 页面上显示 "-" 的列（信号本来就没算出来）不能进命中率分母。
	// 同时：没有存下盘口线时，大小球列一律不适用。
	record := models.FinalePrediction{Pick: "away", BaseDir: "away"}
	fields := settleFinaleRecord(&record, finaleMatch(0, 2), time.Now())

	wantHit(t, fields, "hit_pick", true)
	wantHit(t, fields, "hit_base", true)
	for _, key := range []string{
		"hit_exp_ah", "hit_ou_heat", "hit_ou_pick", "hit_exp_ou", "hit_trade", "hit_sim",
	} {
		wantNA(t, fields, key)
	}
}

func TestSettleFinaleRecordDrawSettlesSpfColumns(t *testing.T) {
	// 平局：胜平负列照常结算（判主/客的都算没中，判平的算中），不是不适用。
	// 期望让球是赢盘列，不受「平局」影响——受让盘上平局照样能赢盘。
	ahLine := -0.5
	record := models.FinalePrediction{
		Pick: "home", BaseDir: "draw", ExpAhDir: "home", TradeDir: "home", AhLineValue: &ahLine,
	}
	fields := settleFinaleRecord(&record, finaleMatch(1, 1), time.Now())

	if fields["result"] != "draw" {
		t.Fatalf("result = %v, want draw", fields["result"])
	}
	wantHit(t, fields, "hit_pick", false)
	wantHit(t, fields, "hit_base", true)
	// 主队受让0.5，平局即赢盘；判主队赢盘 → 中。
	wantHit(t, fields, "hit_exp_ah", true)
	// 反买：提示主胜→买客胜，平局，没中。
	wantHit(t, fields, "hit_trade", false)
}

func TestSettleFinaleRecordAlsoWritesBackToStruct(t *testing.T) {
	// 回算路径（buildFinaleRecompute）不入库，完全靠 settleFinaleRecord 把结果
	// 写回 struct 后直接渲染/统计。这里锁住这个行为。
	line := 2.5
	record := models.FinalePrediction{Pick: "away", BaseDir: "away", ExpOuDir: "under", OuLineValue: &line}
	settleFinaleRecord(&record, finaleMatch(0, 1), time.Now())

	if !record.Settled || record.Result != "away" {
		t.Fatalf("record settled=%v result=%q, want true/away", record.Settled, record.Result)
	}
	if record.HomeScore != 0 || record.GuestScore != 1 {
		t.Fatalf("record score = %d-%d, want 0-1", record.HomeScore, record.GuestScore)
	}
	if record.HitPick == nil || !*record.HitPick {
		t.Fatalf("record.HitPick = %v, want true", record.HitPick)
	}
	// 总进球1 < 2.5 → 小球，判小命中。
	if record.HitExpOu == nil || !*record.HitExpOu {
		t.Fatalf("record.HitExpOu = %v, want true", record.HitExpOu)
	}
	if record.SettledAt == nil {
		t.Fatal("record.SettledAt = nil, want a timestamp")
	}
}

func TestFinaleOppositeOnlyFlipsHomeAway(t *testing.T) {
	if got := finaleOpposite("home"); got != "away" {
		t.Fatalf("finaleOpposite(home) = %q, want away", got)
	}
	if got := finaleOpposite("away"); got != "home" {
		t.Fatalf("finaleOpposite(away) = %q, want home", got)
	}
	// 无盈亏数据时方向为空，反买也必须是空——否则会被当成「买平局」结算。
	if got := finaleOpposite(""); got != "" {
		t.Fatalf("finaleOpposite(\"\") = %q, want empty", got)
	}
}
