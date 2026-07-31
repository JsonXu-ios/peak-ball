package handlers

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// #27 前端主推≥70%·按大小球盘口分档的三种买法：每个盘口档一行，行内三格——
// 买小(本档)、买大(本档+1)、买大(本档+2)。同行三格针对同一批比赛，场次重复。

// avgOdds 造一行平均欧赔；主推概率 = 三项归一化后最大的那个。
func avgOdds(home, draw, away string) map[string]interface{} {
	return map[string]interface{}{"avg_odds": `{"odds":[` + home + `,` + draw + `,` + away + `]}`}
}

func base70Rows(t *testing.T, report gin.H) []gin.H {
	t.Helper()
	sig := statSignal(t, report, "base70_goals")
	rows, ok := sig["line_rows"].([]gin.H)
	if !ok {
		t.Fatalf("line_rows 类型 = %T, want []gin.H", sig["line_rows"])
	}
	return rows
}

// base70Bucket 按 key 找某一档盘口下某种买法的那一格。
func base70Bucket(t *testing.T, report gin.H, bucketKey string) gin.H {
	t.Helper()
	for _, row := range base70Rows(t, report) {
		bets, _ := row["bets"].([]gin.H)
		for _, bet := range bets {
			if key, _ := bet["key"].(string); key == bucketKey {
				return bet
			}
		}
	}
	t.Fatalf("bucket %q not found", bucketKey)
	return nil
}

func TestBase70GoalsSettlesThreeBets(t *testing.T) {
	// 一场主推 76.9%（1.2/6/12）、盘口 3.5、实际 3-2 共 5 球的比赛：
	//   买小 3.5 → 5 > 3.5 打出大球 → 未命中
	//   买大 4.5 → 5 > 4.5 → 命中
	//   买大 5.5 → 5 < 5.5 → 未命中
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 2},
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"3.5"}]`},
	}
	odds := map[string]map[string]interface{}{"1": avgOdds("1.2", "6", "12")}
	report := buildSignalStatistics(matches, nil, pankous, odds)

	for _, want := range []struct {
		key     string
		line    string
		pick    string
		matched int
		hit     int
	}{
		{"base70_goals-3.5-under", "3.5", "小球", 1, 0},
		{"base70_goals-3.5-over1", "4.5", "大球", 1, 1},
		{"base70_goals-3.5-over2", "5.5", "大球", 1, 0},
	} {
		bucket := base70Bucket(t, report, want.key)
		if bucket["matched"] != want.matched || bucket["hit"] != want.hit {
			t.Fatalf("%s matched/hit = %v/%v, want %d/%d", want.key, bucket["matched"], bucket["hit"], want.matched, want.hit)
		}
		details, _ := bucket["matches"].([]statisticsDetail)
		if len(details) != 1 || details[0].Line != want.line || details[0].Pick != want.pick {
			t.Fatalf("%s 明细 = %+v, want 盘口%s/%s", want.key, details, want.line, want.pick)
		}
	}
}

func TestBase70GoalsSkipsLowProbabilityMatches(t *testing.T) {
	// 主推只有 68.7%（1.4/5/8），达不到 70% 门槛，整张表都不该有行。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 2},
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"3.5"}]`},
	}
	odds := map[string]map[string]interface{}{"1": avgOdds("1.4", "5", "8")}
	report := buildSignalStatistics(matches, nil, pankous, odds)

	if rows := base70Rows(t, report); len(rows) != 0 {
		t.Fatalf("line_rows = %d 行, want 0（主推未达 70%%，不该建盘口档）", len(rows))
	}
	if matched := statSignal(t, report, "base70_goals")["matched"]; matched != 0 {
		t.Fatalf("matched = %v, want 0", matched)
	}
}

func TestBase70GoalsGroupsByLine(t *testing.T) {
	// 三场主推都达标、盘口 2.25 / 3.25 / 2.25：应当生成两档，按线值升序，
	// 2.25 档收 2 场、3.25 档收 1 场。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 1, GuestScore: 0},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 3, GuestScore: 2},
		{ID: "3", Date: "2026-03-01", Home: "E", Guest: "F", HomeScore: 2, GuestScore: 0},
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.25"}]`},
		"2": {"dxq_data": `[{"companyId":8,"pankou":"3.25"}]`},
		"3": {"dxq_data": `[{"companyId":8,"pankou":"2.25"}]`},
	}
	odds := map[string]map[string]interface{}{
		"1": avgOdds("1.2", "6", "12"),
		"2": avgOdds("1.2", "6", "12"),
		"3": avgOdds("1.2", "6", "12"),
	}
	report := buildSignalStatistics(matches, nil, pankous, odds)

	rows := base70Rows(t, report)
	if len(rows) != 2 {
		t.Fatalf("line_rows = %d 行, want 2（2.25 与 3.25 两档）", len(rows))
	}
	if line, _ := rows[0]["line"].(string); line != "2.25" {
		t.Fatalf("rows[0].line = %q, want 2.25（盘口需按线值升序）", line)
	}
	if line, _ := rows[1]["line"].(string); line != "3.25" {
		t.Fatalf("rows[1].line = %q, want 3.25", line)
	}
	// 「场次」是该档的比赛数（去重），不是三种买法相加。
	if rows[0]["matched"] != 2 || rows[1]["matched"] != 1 {
		t.Fatalf("各档场次 = %v/%v, want 2/1", rows[0]["matched"], rows[1]["matched"])
	}
	// 每行三格，格子标题写出该格实际结算的线（前端放进悬浮提示）。
	bets, _ := rows[0]["bets"].([]gin.H)
	if len(bets) != 7 {
		t.Fatalf("2.25档 bets = %d 格, want 7", len(bets))
	}
	for i, want := range []string{
		"小 2.25", "大 3.25", "大 4.25",
		"期望球数对 2.25", "期望球数截尾取整对 2.25",
		"期望球数判小球对 2.25", "期望球数截尾判小球对 2.25",
	} {
		if title, _ := bets[i]["title"].(string); title != want {
			t.Fatalf("2.25档第%d格标题 = %q, want %q", i+1, title, want)
		}
	}
}

func TestBase70GoalsExpectedDirectionCell(t *testing.T) {
	// 第四格「跟期望球数」：期望球数高于本档盘口判大球、低于判小球，结算对的是本档盘口。
	// 交锋 5 球、近期 1 球 → 期望=0.3×5+0.7×1=2.2，盘口 2.5 → 2.2 < 2.5 判小球；
	// 实际 1-0 共 1 球打出小球 → 命中。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 1, GuestScore: 0},
	}
	histories := map[string]map[string]interface{}{"1": goalsHistory("A", "B", 4, 1, 1, 0)}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
	}
	odds := map[string]map[string]interface{}{"1": avgOdds("1.2", "6", "12")}
	report := buildSignalStatistics(matches, histories, pankous, odds)

	bucket := base70Bucket(t, report, "base70_goals-2.5-exp")
	if bucket["matched"] != 1 || bucket["hit"] != 1 {
		t.Fatalf("期望球数格 matched/hit = %v/%v, want 1/1", bucket["matched"], bucket["hit"])
	}
	details, _ := bucket["matches"].([]statisticsDetail)
	if len(details) != 1 || details[0].Pick != "小球" || details[0].Line != "2.5" {
		t.Fatalf("期望球数格明细 = %+v, want 判小球/结算线2.5", details)
	}
	// 数值列放期望球数本身（2.2），不是主推概率——方便看它离盘口多远。
	if details[0].Value != 2.2 {
		t.Fatalf("期望球数格数值 = %v, want 2.2（期望球数本身）", details[0].Value)
	}
	// 同一行的买小格数值列仍是主推概率，两格互不影响。
	if under, _ := base70Bucket(t, report, "base70_goals-2.5-under")["matches"].([]statisticsDetail); under[0].Value == 2.2 {
		t.Fatal("买小格的数值列应当是主推概率，不该被期望球数覆盖")
	}
}

func TestBase70GoalsTruncatedCellCutsDecimals(t *testing.T) {
	// 第五格「截尾取整」：抹掉小数、不四舍五入。交锋 4 球、近期 2 球 →
	// 期望=0.3×4+0.7×2=2.6 → 截尾成 2，盘口 2.5 → 2 < 2.5 判小球。
	// 注意四舍五入会得到 3（判大球），两格方向相反，正好验证用的是截尾不是舍入。
	// 实际 1-0 共 1 球打出小球 → 截尾格命中、期望球数原值格（2.6 > 2.5 判大球）未命中。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 1, GuestScore: 0},
	}
	histories := map[string]map[string]interface{}{"1": goalsHistory("A", "B", 3, 1, 1, 1)}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
	}
	odds := map[string]map[string]interface{}{"1": avgOdds("1.2", "6", "12")}
	report := buildSignalStatistics(matches, histories, pankous, odds)

	trunc := base70Bucket(t, report, "base70_goals-2.5-exptrunc")
	if trunc["matched"] != 1 || trunc["hit"] != 1 {
		t.Fatalf("截尾格 matched/hit = %v/%v, want 1/1", trunc["matched"], trunc["hit"])
	}
	details, _ := trunc["matches"].([]statisticsDetail)
	if len(details) != 1 || details[0].Pick != "小球" {
		t.Fatalf("截尾格明细 = %+v, want 判小球（2.6 截尾成 2，小于盘口 2.5）", details)
	}
	// 数值列放截尾后的整数 2，不是原值 2.6——否则看不出截尾这一步做了什么。
	if details[0].Value != 2 {
		t.Fatalf("截尾格数值 = %v, want 2（截尾后的整数）", details[0].Value)
	}
	// 同一场在「期望球数原值」格里是判大球（2.6 > 2.5）且未命中，方向与截尾格相反。
	exp := base70Bucket(t, report, "base70_goals-2.5-exp")
	expDetails, _ := exp["matches"].([]statisticsDetail)
	if exp["hit"] != 0 || len(expDetails) != 1 || expDetails[0].Pick != "大球" {
		t.Fatalf("期望球数原值格 = %v 命中 %v，明细 %+v, want 判大球且未命中", exp["matched"], exp["hit"], expDetails)
	}
}

func TestBase70GoalsExpectedUnderCellKeepsOnlyUnderCalls(t *testing.T) {
	// 第六格只收「期望球数判小球」的场次，是第四格判小的那一半。
	// 比赛1：交锋 4 球、近期 4 球 → 期望 4.0 > 盘口 2.5 判大球 → 第四格收、第六格不收。
	// 比赛2：交锋 1 球、近期 1 球 → 期望 1.0 < 盘口 2.5 判小球，实际 1-0 共 1 球
	//        打出小球 → 两格都收、都命中。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
		{ID: "2", Date: "2026-03-01", Home: "C", Guest: "D", HomeScore: 1, GuestScore: 0},
	}
	histories := map[string]map[string]interface{}{
		"1": goalsHistory("A", "B", 3, 1, 2, 2),
		"2": goalsHistory("C", "D", 1, 0, 1, 0),
	}
	line := map[string]interface{}{"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`}
	pankous := map[string]map[string]interface{}{"1": line, "2": line}
	odds := map[string]map[string]interface{}{
		"1": avgOdds("1.2", "6", "12"),
		"2": avgOdds("1.2", "6", "12"),
	}
	report := buildSignalStatistics(matches, histories, pankous, odds)

	// 第四格两场都收（一场判大一场判小），都命中。
	if exp := base70Bucket(t, report, "base70_goals-2.5-exp"); exp["matched"] != 2 || exp["hit"] != 2 {
		t.Fatalf("期望球数格 = %v/%v, want 2/2", exp["matched"], exp["hit"])
	}
	// 第六格只收判小的那一场。
	under := base70Bucket(t, report, "base70_goals-2.5-expunder")
	if under["matched"] != 1 || under["hit"] != 1 {
		t.Fatalf("判小球格 = %v/%v, want 1/1（只收判小的那场）", under["matched"], under["hit"])
	}
	details, _ := under["matches"].([]statisticsDetail)
	if len(details) != 1 || details[0].MatchID != "2" || details[0].Pick != "小球" {
		t.Fatalf("判小球格明细 = %+v, want 比赛2/判小球", details)
	}
	// 第七格同理：比赛2 截尾成 1 仍 < 2.5 判小球，只收它；比赛1 截尾成 4，判大球不收。
	truncUnder := base70Bucket(t, report, "base70_goals-2.5-exptruncunder")
	if truncUnder["matched"] != 1 || truncUnder["hit"] != 1 {
		t.Fatalf("截尾判小球格 = %v/%v, want 1/1", truncUnder["matched"], truncUnder["hit"])
	}
	truncDetails, _ := truncUnder["matches"].([]statisticsDetail)
	if len(truncDetails) != 1 || truncDetails[0].MatchID != "2" || truncDetails[0].Value != 1 {
		t.Fatalf("截尾判小球格明细 = %+v, want 比赛2/数值1（截尾后的整数）", truncDetails)
	}
}

func TestBase70GoalsTruncatedUnderCellCatchesBorderlineOvers(t *testing.T) {
	// 截尾把「压线过的大球」翻成判小球：交锋 4 球、近期 2 球 → 期望 2.6，盘口 2.5。
	//   第⑥格（原值 2.6 > 2.5 判大球）→ 不收
	//   第⑦格（截尾成 2 < 2.5 判小球）→ 收
	// 这正是第⑦格比第⑥格多收的那一批场次。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 3, GuestScore: 1},
	}
	histories := map[string]map[string]interface{}{"1": goalsHistory("A", "B", 3, 1, 1, 1)}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
	}
	odds := map[string]map[string]interface{}{"1": avgOdds("1.2", "6", "12")}
	report := buildSignalStatistics(matches, histories, pankous, odds)

	if under := base70Bucket(t, report, "base70_goals-2.5-expunder"); under["matched"] != 0 {
		t.Fatalf("期望球数判小球格 matched = %v, want 0（原值 2.6 > 2.5 判大球）", under["matched"])
	}
	truncUnder := base70Bucket(t, report, "base70_goals-2.5-exptruncunder")
	// 实际 3-1 共 4 球打出大球，买小球未命中——但必须计入分母。
	if truncUnder["matched"] != 1 || truncUnder["hit"] != 0 {
		t.Fatalf("截尾判小球格 = %v/%v, want 1/0（截尾成 2 判小球，实际打出大球）", truncUnder["matched"], truncUnder["hit"])
	}
}

func TestBase70GoalsExpectedCellSkipsMatchesWithoutHistory(t *testing.T) {
	// 无交锋记录时期望球数不成立，第四格不收这场；但前三格照常统计——
	// 各格分母本来就允许不一样。
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 1, GuestScore: 0},
	}
	histories := map[string]map[string]interface{}{
		"1": {
			"against_list":      `[]`,
			"recent_home_list":  `[{"matchTime":"2026-02-01","home":"A","guest":"X","goal":[1,0]}]`,
			"recent_guest_list": `[{"matchTime":"2026-02-02","home":"B","guest":"Y","goal":[1,0]}]`,
		},
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"2.5"}]`},
	}
	odds := map[string]map[string]interface{}{"1": avgOdds("1.2", "6", "12")}
	report := buildSignalStatistics(matches, histories, pankous, odds)

	for _, key := range []string{
		"base70_goals-2.5-exp", "base70_goals-2.5-exptrunc",
		"base70_goals-2.5-expunder", "base70_goals-2.5-exptruncunder",
	} {
		if bucket := base70Bucket(t, report, key); bucket["matched"] != 0 {
			t.Fatalf("%s matched = %v, want 0（无交锋记录）", key, bucket["matched"])
		}
	}
	if bucket := base70Bucket(t, report, "base70_goals-2.5-under"); bucket["matched"] != 1 {
		t.Fatalf("买小格 matched = %v, want 1（前三格不受交锋缺失影响）", bucket["matched"])
	}
}

func TestBase70GoalsPushIsNotAMiss(t *testing.T) {
	// 盘口 3（整数）、实际 2-2 共 4 球：
	//   买小 3  → 4 > 3 → 未命中（计入分母）
	//   买大 4  → 总进球正好 4 = 走盘 → 不适用，不进分母
	//   买大 5  → 4 < 5 → 未命中
	matches := []statisticsMatch{
		{ID: "1", Date: "2026-03-01", Home: "A", Guest: "B", HomeScore: 2, GuestScore: 2},
	}
	pankous := map[string]map[string]interface{}{
		"1": {"dxq_data": `[{"companyId":8,"pankou":"3"}]`},
	}
	odds := map[string]map[string]interface{}{"1": avgOdds("1.2", "6", "12")}
	report := buildSignalStatistics(matches, nil, pankous, odds)

	if bucket := base70Bucket(t, report, "base70_goals-3-under"); bucket["matched"] != 1 || bucket["hit"] != 0 {
		t.Fatalf("买小3 = %v/%v, want 1/0", bucket["matched"], bucket["hit"])
	}
	if bucket := base70Bucket(t, report, "base70_goals-3-over1"); bucket["matched"] != 0 {
		t.Fatalf("买大4 matched = %v, want 0（走盘不进分母）", bucket["matched"])
	}
	if bucket := base70Bucket(t, report, "base70_goals-3-over2"); bucket["matched"] != 1 || bucket["hit"] != 0 {
		t.Fatalf("买大5 = %v/%v, want 1/0", bucket["matched"], bucket["hit"])
	}
	// 该档比赛数仍是 1（走盘只影响那一格的分母，不影响行的场次）。
	if rows := base70Rows(t, report); rows[0]["matched"] != 1 {
		t.Fatalf("该档场次 = %v, want 1", rows[0]["matched"])
	}
}
