// Package handlers: statistics_finale.go —「终章」比赛预测与前瞻实测存档。
//
// 扫描未来 finaleHorizonDays 天内的待赛比赛，输出前端主推≥70% 的场次（信号口径
// 与「最后一舞」客胜命中率卡片完全一致）。主推方向+概率（sig_base）、大小球投注
// 热度分档（ou_heat）、前端球数倾向（ou_pick）、期望让球综合均值（exp_ah）、期望
// 球数综合均值（exp_ou）、亚盘/大小球盘口，以及交易盈亏/模拟盈亏（舒服项映射）的
// 推荐，都只随行展示，不参与预测。信号取值照搬「完赛基础统计」的 13 / 15 / 6b /
// 12b 维度，但 exp_ah 的结算改成对亚盘线判赢盘：期望让球是个让球数，2.70 的期望
// 碰上 3.25 的盘口，主队赢球也照样输盘，按胜平负结算会把这一列判反。
//
// 每次请求顺带做两件事，不需要手动触发：
//
//  1. 把未来窗口内有预测、且尚未开赛的场次 upsert 进 finale_predictions；
//     已开赛的行不再覆盖，赛前信号值就此冻结。
//  2. 扫存档里 settled=0 且已完赛的行，用存档里的盘口线逐列判命中并写死。
//
// 待赛列表实时计算、不读存档；存档表只服务历史区间查询与命中率汇总。
package handlers

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"go_admin/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"
)

// finaleHorizonDays 预测向前看的天数窗口。
const finaleHorizonDays = 14

// finaleColumn 描述一个参与命中率统计的信号列：展示名，以及从存档行里取出该列
// 命中结果的方法。
type finaleColumn struct {
	Key   string
	Label string
	Hit   func(models.FinalePrediction) *bool
}

// 大小球热度/期望让球/期望球数三列已从页面撤下，命中率也不再统计（大小球那套
// 判断改由「大小球」选项卡专门跟踪）。它们的值仍照常算并写进存档，数据不断档，
// 日后想恢复只要把对应行加回这里、并把列加回表格即可。
var finaleColumns = []finaleColumn{
	{"pick", "预测（主推≥70%）", func(p models.FinalePrediction) *bool { return p.HitPick }},
	{"trade", "交易盈亏·反买", func(p models.FinalePrediction) *bool { return p.HitTrade }},
	{"sim", "模拟盈亏·反买", func(p models.FinalePrediction) *bool { return p.HitSim }},
}

// GetFinaleStatistics serves the 终章 page.
//
//	默认：返回未来待赛预测。
//	mode=history（可带 start/end）：返回存档行，只有有预测的场次入库。
//
// 两种模式都会先跑一次 upsert + 结算，并附带命中率汇总。
func GetFinaleStatistics(c *gin.Context) {
	start, end, err := statisticsDateRange(c.Query("start"), c.Query("end"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// mode=history 且不带日期 = 查全部存档；带日期则限定区间。
	history := c.Query("mode") == "history" || start != "" || end != ""

	upcoming, rows, err := buildFinaleUpcoming()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	archiveErr := archiveFinalePredictions(rows)
	settleErr := settleFinalePredictions()

	predictions := make([]gin.H, 0, len(rows))
	accuracy := finaleEmptyAccuracy()
	recomputeAccuracy := finaleEmptyAccuracy()
	var rangeErr error

	if history {
		archived, err := loadFinaleArchive(start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		seen := make(map[string]struct{}, len(archived))
		for _, record := range archived {
			seen[record.MatchID] = struct{}{}
			predictions = append(predictions, finaleArchiveRow(record, "archive"))
		}
		// 没有赛前存档的已完赛比赛即时回算，让上线前的日子也能看。
		recomputed, err := buildFinaleRecompute(start, end, seen)
		if err != nil {
			rangeErr = err
		}
		for _, record := range recomputed {
			predictions = append(predictions, finaleArchiveRow(record, "recompute"))
		}
		sort.SliceStable(predictions, func(i, j int) bool {
			ti, _ := predictions[i]["match_time"].(string)
			tj, _ := predictions[j]["match_time"].(string)
			if ti != tj {
				return ti < tj
			}
			mi, _ := predictions[i]["match_id"].(string)
			mj, _ := predictions[j]["match_id"].(string)
			return mi < mj
		})
		// 两套命中率分开算，绝不合并：一套是赛前存档的真前瞻，一套是事后回算。
		accuracy = finaleAccuracyOf(archived)
		recomputeAccuracy = finaleAccuracyOf(recomputed)
	} else {
		for _, row := range rows {
			predictions = append(predictions, row.payload())
		}
		// 待赛模式下顶部卡片统计全部已结算存档。
		var err error
		accuracy, err = buildFinaleAccuracy("", "")
		rangeErr = err
	}

	response := gin.H{
		"generated_at":       time.Now().Format("2006-01-02 15:04:05"),
		"horizon_days":       finaleHorizonDays,
		"upcoming_total":     upcoming,
		"mode":               map[bool]string{true: "history", false: "upcoming"}[history],
		"start":              start,
		"end":                end,
		"predictions":        predictions,
		"accuracy":           accuracy,
		"recompute_accuracy": recomputeAccuracy,
	}
	// 存档/结算失败不该挡住页面本身，降级成一条提示随响应返回。
	if warning := firstError(archiveErr, settleErr, rangeErr); warning != "" {
		response["warning"] = warning
	}
	c.JSON(http.StatusOK, response)
}

func firstError(errs ...error) string {
	for _, err := range errs {
		if err != nil {
			return err.Error()
		}
	}
	return ""
}

// ---------- 待赛预测计算 ----------

// finaleUpcomingWindow 返回待赛窗口的三个边界：今天、horizon 天后、当前时刻。
func finaleUpcomingWindow() (today, horizon, cutoff string) {
	now := time.Now()
	return now.Format("2006-01-02"),
		now.AddDate(0, 0, finaleHorizonDays).Format("2006-01-02"),
		now.Format("2006-01-02 15:04")
}

// finaleIsUpcoming 判断一场比赛是否还算「待赛」。
//
// 除了日期窗口与未结算，还必须【尚未开赛】：库里的 settled 要等结果拉回来才置位，
// 凌晨踢完、结果还没回填的比赛在此之前 settled 仍是 0，只按日期筛的话它会一直挂
// 在待赛列表里，看着像还能下注的场次。开赛时间一过就必须从待赛里消失——存档那边
// 本来就是按开赛时间冻结的，这里对齐同一条线。
func finaleIsUpcoming(match statisticsMatch, today, horizon, cutoff string) bool {
	if match.ID == "" || match.Settled || match.Date < today || match.Date > horizon {
		return false
	}
	// match_time 为空时无法判开赛，保守放行，交给日期窗口兜底。
	return match.MatchTime == "" || match.MatchTime > cutoff
}

// finaleRow 是一场待赛比赛算出来的全部信号：展示文本与结算所需的方向分开存放。
type finaleRow struct {
	match statisticsMatch

	pick      string // home/away，空=主推未达门槛
	sigBase   string
	ouHeat    string
	ouPick    string
	expAh     string
	expOu     string
	ahText    string
	ouText    string
	tradePick string // 展示文本（主胜/客胜）
	simPick   string
	// goalPick 命中「大小球」选项卡那两种反向组合之一时为「买大球」，否则为空。
	// 只展示，不在本页结算——命中率由大小球选项卡单独统计。
	goalPick string

	baseDir   string
	ouHeatDir string
	ouPickDir string
	expAhDir  string
	expOuDir  string
	tradeDir  string
	simDir    string

	ahLine    float64
	hasAHLine bool
	ouLine    float64
	hasOULine bool
}

func (r finaleRow) payload() gin.H {
	return gin.H{
		"match_id":   r.match.ID,
		"date":       r.match.Date,
		"match_time": r.match.MatchTime,
		"league":     r.match.League,
		"home":       r.match.Home,
		"guest":      r.match.Guest,
		"home_logo":  r.match.HomeLogo,
		"guest_logo": r.match.GuestLogo,
		"pick":       r.pick,
		"sig_base":   r.sigBase,
		"ou_heat":    r.ouHeat,
		"ou_pick":    r.ouPick,
		"exp_ah":     r.expAh,
		"exp_ou":     r.expOu,
		"ah_line":    r.ahText,
		"ou_line":    r.ouText,
		"trade_pick": r.tradePick,
		"sim_pick":   r.simPick,
		"goal_pick":  r.goalPick,
		"settled":    false,
	}
}

func (r finaleRow) record(now time.Time) models.FinalePrediction {
	record := models.FinalePrediction{
		MatchID: r.match.ID, Date: r.match.Date, MatchTime: r.match.MatchTime,
		League: r.match.League, Home: r.match.Home, Guest: r.match.Guest,
		HomeLogo: r.match.HomeLogo, GuestLogo: r.match.GuestLogo,
		Pick: r.pick, SigBase: r.sigBase, OuHeat: r.ouHeat, OuPick: r.ouPick,
		ExpAh: r.expAh, ExpOu: r.expOu, AhLine: r.ahText, OuLine: r.ouText,
		TradePick: r.tradePick, SimPick: r.simPick, GoalPick: r.goalPick,
		BaseDir: r.baseDir, OuHeatDir: r.ouHeatDir, OuPickDir: r.ouPickDir,
		ExpAhDir: r.expAhDir, ExpOuDir: r.expOuDir, TradeDir: r.tradeDir, SimDir: r.simDir,
		SnapshotAt: now,
	}
	if r.hasAHLine {
		line := r.ahLine
		record.AhLineValue = &line
	}
	if r.hasOULine {
		line := r.ouLine
		record.OuLineValue = &line
	}
	return record
}

// buildFinaleUpcoming 计算未来窗口内每场待赛比赛的信号，返回待赛总场次与有预测
// 的行（按开赛时间升序）。
func buildFinaleUpcoming() (int, []finaleRow, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		return 0, nil, err
	}
	today, horizon, cutoff := finaleUpcomingWindow()

	upcoming := make([]statisticsMatch, 0, 64)
	ids := make([]string, 0, 64)
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if !finaleIsUpcoming(match, today, horizon, cutoff) {
			continue
		}
		upcoming = append(upcoming, match)
		ids = append(ids, match.ID)
	}

	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)

	rows := make([]finaleRow, 0, len(upcoming))
	for _, match := range upcoming {
		row := buildFinaleRow(match, histories[match.ID], pankous[match.ID], odds[match.ID])
		if row.pick == "" {
			continue
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].match.MatchTime != rows[j].match.MatchTime {
			return rows[i].match.MatchTime < rows[j].match.MatchTime
		}
		return rows[i].match.ID < rows[j].match.ID
	})
	return len(upcoming), rows, nil
}

// buildFinaleRow 算出一场比赛的全部信号列。
func buildFinaleRow(match statisticsMatch, historyRow, pankouRow, oddsRow map[string]interface{}) finaleRow {
	row := finaleRow{match: match}

	// —— 前端主推方向与概率，口径与「最后一舞」buildLastDanceAwayCard 一致 ——
	baseDir, baseProb := "", 0.0
	if probabilities := statisticsProbabilities(oddsRow); len(probabilities) == 3 {
		baseDir, baseProb = "home", probabilities[0]
		if probabilities[1] > baseProb {
			baseDir, baseProb = "draw", probabilities[1]
		}
		if probabilities[2] > baseProb {
			baseDir, baseProb = "away", probabilities[2]
		}
	}
	row.baseDir = baseDir
	if baseDir != "" {
		row.sigBase = fmt.Sprintf("%s %.1f%%", statisticsOutcomeLabel(baseDir), baseProb)
	}
	// 入选信号：前端主推≥70%，方向为平不预测。
	if baseProb >= lastDanceProbThreshold && (baseDir == "home" || baseDir == "away") {
		row.pick = baseDir
	}

	_, ahLine, hasAH := statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
	row.ahLine, row.hasAHLine = ahLine, hasAH
	if hasAH {
		row.ahText = fmt.Sprintf("%g", statisticsRound2(ahLine))
	}
	_, ouLine, hasOU := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
	row.ouLine, row.hasOULine = ouLine, hasOU
	if hasOU {
		row.ouText = fmt.Sprintf("%g", statisticsRound2(ouLine))
	}

	// —— 交易/模拟盈亏的舒服项映射推荐（仅展示，命中率按反买结算） ——
	mapComfort := func(comfort string) (string, bool) {
		switch comfort {
		case "home":
			return "home", true
		case "draw", "away":
			if !hasAH || ahLine == 0 {
				return "", false
			}
			if ahLine > 0 {
				return "away", true
			}
			return "home", true
		}
		return "", false
	}
	if tradeDir, ok := statisticsBookmakerComfort(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade")); ok {
		if pick, mapped := mapComfort(tradeDir); mapped {
			row.tradeDir, row.tradePick = pick, statisticsOutcomeLabel(pick)
		}
	}
	if simDir, ok := statisticsSimulatedComfort(oddsRow, pankouRow, historyRow, match); ok {
		if pick, mapped := mapComfort(simDir); mapped {
			row.simDir, row.simPick = pick, statisticsOutcomeLabel(pick)
		}
	}

	against, homeRecent, guestRecent := statisticsHistory(historyRow)
	historyDiff, historyGoals, hasHistory := statisticsHeadToHead(match, against)
	recentGoals, hasRecentGoals := statisticsRecentGoals(homeRecent, guestRecent)

	// 13. 大小球投注热度分档：期望球数=历史交锋场均与近期场均的等权平均，
	// 热度=clamp(50+(期望-盘口)×18)，取大/小两侧较大值分档(90…55)。
	// 终章只认 65 档及以上（55/60 档命中率不稳），低档位留空、不参与结算。
	if hasOU && (hasRecentGoals || hasHistory) {
		expected := statisticsMean(recentGoals, hasRecentGoals, historyGoals, hasHistory)
		overHeat := statisticsClamp(50+(expected-ouLine)*18, 0, 100)
		heat := math.Max(overHeat, 100-overHeat)
		if tier, ok := statisticsHeatTierIn(statisticsGoalsHeatTiers, heat); ok && tier >= 65 {
			row.ouHeatDir = finaleOverDirection(overHeat >= 50)
			row.ouHeat = fmt.Sprintf("%d档 判%s %.1f", tier, statisticsOverLabel(overHeat >= 50), heat)
		}
	}
	// 15. 前端球数倾向：近期场均总进球对盘口的压力方向，压力差<5(盘口球)为空。
	if qiuDir, _, ok := pickQiuPrediction(historyRow, pankouRow, match); ok && qiuDir != "" {
		row.ouPickDir = qiuDir
		row.ouPick = "判" + statisticsOverLabel(qiuDir == "over")
	}

	// —— H5 两个「综合均值」的判断（让球同「完赛基础统计」6b；球数为 0.3/0.7 加权）——
	// 历史与近期缺任一样本即留空，不用 0 顶替。
	recentDiff, hasRecentDiff := statisticsRecentDifference(
		statisticsRecentForm(homeRecent, match.Home), statisticsRecentForm(guestRecent, match.Guest))
	// 期望让球是个让球数，必须拿去和亚盘线比才有意义：期望比盘口深→主队让得过
	// →判主队赢盘；期望比盘口浅→主队让不过→判客队赢盘。直接按正负判胜平负会
	// 完全忽略盘口（期望2.70 而盘口3.25 时，主队赢球也照样输盘）。
	if hasHistory && hasRecentDiff && hasAH {
		composite := (historyDiff + recentDiff) / 2
		if math.Abs(composite-ahLine) >= statisticsPushEpsilon {
			coverHome := composite > ahLine
			row.expAhDir = finaleCoverDirection(coverHome)
			row.expAh = fmt.Sprintf("%s %.2f(盘%g)",
				statisticsCoverLabel(coverHome), composite, statisticsRound2(ahLine))
		}
	}
	// 大小球推荐：命中「大小球」选项卡那两种反向组合之一就显示买大球，口径完全
	// 复用同一个函数，避免两处判断走偏。只展示，不在本页结算。
	if _, ok := buildFinaleGoalRow(match, historyRow, pankouRow); ok {
		row.goalPick = "买大球"
	}
	if hasOU && hasHistory && hasRecentGoals {
		composite := historyGoals*0.3 + recentGoals*0.7
		if math.Abs(composite-ouLine) >= statisticsPushEpsilon {
			row.expOuDir = finaleOverDirection(composite > ouLine)
			row.expOu = fmt.Sprintf("判%s %.2f", statisticsOverLabel(composite > ouLine), composite)
		}
	}
	return row
}

func finaleOverDirection(over bool) string {
	if over {
		return "over"
	}
	return "under"
}

// finaleCoverDirection 把「主队赢盘与否」编码成方向，结算时与实际赢盘方比较。
func finaleCoverDirection(home bool) string {
	if home {
		return "home"
	}
	return "away"
}

// ---------- 存档 ----------

// archiveFinalePredictions upserts 尚未开赛的预测行。已开赛（存档里 settled=1，
// 或库里已有行且开赛时间已过）的场次不再覆盖，赛前信号值就此冻结。
func archiveFinalePredictions(rows []finaleRow) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now()
	cutoff := now.Format("2006-01-02 15:04")

	// 已结算的行绝不能被重新写成待赛状态。
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.match.ID)
	}
	frozen := map[string]struct{}{}
	var settledIDs []string
	if err := statisticsDB().Model(&models.FinalePrediction{}).
		Where("match_id IN ? AND settled = ?", ids, true).Pluck("match_id", &settledIDs).Error; err != nil {
		return err
	}
	for _, id := range settledIDs {
		frozen[id] = struct{}{}
	}

	records := make([]models.FinalePrediction, 0, len(rows))
	for _, row := range rows {
		if _, ok := frozen[row.match.ID]; ok {
			continue
		}
		// match_time 为空时保守处理：按日期判断是否已过。
		started := row.match.MatchTime != "" && row.match.MatchTime <= cutoff
		if started {
			continue
		}
		records = append(records, row.record(now))
	}
	if len(records) == 0 {
		return nil
	}
	// 冲突时覆盖展示与结算依据字段，绝不碰 settled/hit_*/比分。
	return statisticsDB().Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "match_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"date", "match_time", "league", "home", "guest", "home_logo", "guest_logo",
			"pick", "sig_base", "ou_heat", "ou_pick", "exp_ah", "exp_ou", "ah_line", "ou_line",
			"trade_pick", "sim_pick", "goal_pick",
			"base_dir", "ou_heat_dir", "ou_pick_dir", "exp_ah_dir", "exp_ou_dir",
			"trade_dir", "sim_dir", "ah_line_value", "ou_line_value", "snapshot_at", "updated_at",
		}),
	}).CreateInBatches(&records, 200).Error
}

// ---------- 结算 ----------

// settleFinalePredictions 扫存档里 settled=0 的行，已完赛的按比分逐列判命中并
// 写死。已经结算过的行一律不碰，所以这个函数是幂等的、越跑越便宜。
func settleFinalePredictions() error {
	var pending []models.FinalePrediction
	if err := statisticsDB().Where("settled = ?", false).Find(&pending).Error; err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	ids := make([]string, 0, len(pending))
	for _, record := range pending {
		ids = append(ids, record.MatchID)
	}
	moneys := loadStatisticsRows("moneys", statisticsMoneysColumns, ids)

	now := time.Now()
	for _, record := range pending {
		raw, ok := moneys[record.MatchID]
		if !ok {
			continue
		}
		match := parseStatisticsMatch(raw)
		if !match.Settled {
			continue
		}
		// 用 map 而不是 struct 更新：nil 的 hit_* 必须真的写成 NULL，
		// struct 更新会把 nil 当零值跳过。
		if err := statisticsDB().Model(&models.FinalePrediction{}).
			Where("id = ?", record.ID).Updates(settleFinaleRecord(&record, match, now)).Error; err != nil {
			return err
		}
	}
	return nil
}

// settleFinaleRecord 按赛果逐列判命中：结果写进 record，同时返回等价的待更新字段
// 表（入库用）。每个 hit_* 只在该列本场有判断、且能结算时才写值；信号为空、走盘、
// 盘口缺失一律留 nil，不进命中率分母。
func settleFinaleRecord(record *models.FinalePrediction, match statisticsMatch, now time.Time) map[string]interface{} {
	outcome := statisticsActualOutcome(match)

	// 盘口类结算：必须用存档时的盘口线（事后盘口会变），走盘不计。
	homeCovered, coverValid := false, false
	if record.AhLineValue != nil {
		homeCovered, coverValid = statisticsAsianCorrect(match, *record.AhLineValue)
	}
	over, valid := false, false
	if record.OuLineValue != nil {
		over, valid = statisticsOverOutcome(match, *record.OuLineValue)
	}

	record.Settled = true
	record.SettledAt = &now
	record.HomeScore = match.HomeScore
	record.GuestScore = match.GuestScore
	record.Result = outcome
	// 胜平负结算的列：方向对上赛果即命中。
	record.HitPick = finaleOutcomeHit(record.Pick, outcome)
	record.HitBase = finaleOutcomeHit(record.BaseDir, outcome)
	// 盈亏提示按反买结算：提示主胜则买客胜，命中=反方向打出。
	record.HitTrade = finaleOutcomeHit(finaleOpposite(record.TradeDir), outcome)
	record.HitSim = finaleOutcomeHit(finaleOpposite(record.SimDir), outcome)
	// 期望让球按赢盘结算（对的是亚盘线，不是胜平负）。
	record.HitExpAh = finaleCoverHit(record.ExpAhDir, homeCovered, coverValid)
	// 大小球三列。
	record.HitOuHeat = finaleOverHit(record.OuHeatDir, over, valid)
	record.HitOuPick = finaleOverHit(record.OuPickDir, over, valid)
	record.HitExpOu = finaleOverHit(record.ExpOuDir, over, valid)

	return map[string]interface{}{
		"settled":     record.Settled,
		"settled_at":  record.SettledAt,
		"home_score":  record.HomeScore,
		"guest_score": record.GuestScore,
		"result":      record.Result,
		"hit_pick":    record.HitPick,
		"hit_base":    record.HitBase,
		"hit_exp_ah":  record.HitExpAh,
		"hit_trade":   record.HitTrade,
		"hit_sim":     record.HitSim,
		"hit_ou_heat": record.HitOuHeat,
		"hit_ou_pick": record.HitOuPick,
		"hit_exp_ou":  record.HitExpOu,
	}
}

func finaleOutcomeHit(direction, outcome string) *bool {
	if direction == "" {
		return nil
	}
	hit := direction == outcome
	return &hit
}

func finaleOverHit(direction string, over, valid bool) *bool {
	if direction == "" || !valid {
		return nil
	}
	hit := (direction == "over") == over
	return &hit
}

// finaleCoverHit 结算赢盘方向。走盘（valid=false）与无方向一律不适用。
func finaleCoverHit(direction string, homeCovered, valid bool) *bool {
	if direction == "" || !valid {
		return nil
	}
	hit := (direction == "home") == homeCovered
	return &hit
}

func finaleOpposite(direction string) string {
	switch direction {
	case "home":
		return "away"
	case "away":
		return "home"
	}
	return ""
}

// ---------- 历史查询与命中率汇总 ----------

// buildFinaleRecompute 对区间内「没有赛前存档」的已完赛比赛即时回算信号并结算。
//
// 存档只能从功能上线那一刻往后累积，更早的比赛永远没有赛前快照。回算让这些日子
// 也能看，但它读的是库里当前的赔率/盘口，不是赛前那一刻的值——所以结果只用于展示
// 与参考，绝不写库、也绝不和存档的命中率合并统计。混进去就毁了前瞻实测的可信度。
func buildFinaleRecompute(start, end string, archived map[string]struct{}) ([]models.FinalePrediction, error) {
	var rawMatches []map[string]interface{}
	query := statisticsDB().Table("moneys").Select(statisticsMoneysColumns)
	if start != "" {
		query = query.Where("date >= ?", start)
	}
	if end != "" {
		query = query.Where("date <= ?", end)
	}
	if err := query.Find(&rawMatches).Error; err != nil {
		return nil, err
	}

	matches := make([]statisticsMatch, 0, len(rawMatches))
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if match.ID == "" || !match.Settled {
			continue
		}
		if _, ok := archived[match.ID]; ok {
			continue
		}
		matches = append(matches, match)
		ids = append(ids, match.ID)
	}
	if len(matches) == 0 {
		return nil, nil
	}

	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)

	now := time.Now()
	records := make([]models.FinalePrediction, 0, len(matches))
	for _, match := range matches {
		row := buildFinaleRow(match, histories[match.ID], pankous[match.ID], odds[match.ID])
		if row.pick == "" {
			continue
		}
		record := row.record(now)
		settleFinaleRecord(&record, match, now)
		records = append(records, record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].MatchTime != records[j].MatchTime {
			return records[i].MatchTime < records[j].MatchTime
		}
		return records[i].MatchID < records[j].MatchID
	})
	return records, nil
}

func loadFinaleArchive(start, end string) ([]models.FinalePrediction, error) {
	var records []models.FinalePrediction
	query := statisticsDB().Model(&models.FinalePrediction{})
	if start != "" {
		query = query.Where("date >= ?", start)
	}
	if end != "" {
		query = query.Where("date <= ?", end)
	}
	err := query.Order("match_time ASC, match_id ASC").Find(&records).Error
	return records, err
}

// finaleOuHeatTierOK 终章大小球热度只认 65 档及以上。口径收紧前按 55/60 档写入
// 的旧存档在读取层过滤：展示留空、命中率不计分母。档位是赛前已知特征，事后按它
// 筛选不构成前视偏差；库里的原始快照不改写，随时可回退。
func finaleOuHeatTierOK(text string) bool {
	var tier int
	if _, err := fmt.Sscanf(text, "%d档", &tier); err != nil {
		return text != "" // 解析不出档位的旧格式按原样放行
	}
	return tier >= 65
}

// finaleApplyOuHeatGate 对一条存档行套用档位闸门（只改内存副本，不写库）。
func finaleApplyOuHeatGate(record *models.FinalePrediction) {
	if !finaleOuHeatTierOK(record.OuHeat) {
		record.OuHeat, record.OuHeatDir, record.HitOuHeat = "", "", nil
	}
}

// finaleArchiveRow 渲染一条存档/回算行。source 必须如实标注：archive=赛前存档
// （真前瞻），recompute=事后用当前赔率回算（仅参考），前端据此打标签。
func finaleArchiveRow(record models.FinalePrediction, source string) gin.H {
	finaleApplyOuHeatGate(&record)
	return gin.H{
		"source":      source,
		"match_id":    record.MatchID,
		"date":        record.Date,
		"match_time":  record.MatchTime,
		"league":      record.League,
		"home":        record.Home,
		"guest":       record.Guest,
		"home_logo":   record.HomeLogo,
		"guest_logo":  record.GuestLogo,
		"pick":        record.Pick,
		"sig_base":    record.SigBase,
		"ou_heat":     record.OuHeat,
		"ou_pick":     record.OuPick,
		"exp_ah":      record.ExpAh,
		"exp_ou":      record.ExpOu,
		"ah_line":     record.AhLine,
		"ou_line":     record.OuLine,
		"trade_pick":  record.TradePick,
		"sim_pick":    record.SimPick,
		"goal_pick":   record.GoalPick,
		"settled":     record.Settled,
		"home_score":  record.HomeScore,
		"guest_score": record.GuestScore,
		"result":      record.Result,
		"snapshot_at": record.SnapshotAt.Format("2006-01-02 15:04"),
		"hits": gin.H{
			"pick":     record.HitPick,
			"sig_base": record.HitBase,
			"ou_heat":  record.HitOuHeat,
			"ou_pick":  record.HitOuPick,
			"exp_ah":   record.HitExpAh,
			"exp_ou":   record.HitExpOu,
			"trade":    record.HitTrade,
			"sim":      record.HitSim,
		},
	}
}

// buildFinaleAccuracy 汇总赛前存档的逐列命中率。区间为空时统计全部已结算存档。
func buildFinaleAccuracy(start, end string) (gin.H, error) {
	query := statisticsDB().Model(&models.FinalePrediction{}).Where("settled = ?", true)
	if start != "" {
		query = query.Where("date >= ?", start)
	}
	if end != "" {
		query = query.Where("date <= ?", end)
	}
	var records []models.FinalePrediction
	if err := query.Find(&records).Error; err != nil {
		return finaleEmptyAccuracy(), err
	}
	return finaleAccuracyOf(records), nil
}

// finaleAccuracyOf 按列聚合命中率。hit_* 为 nil 的列（信号为空、走盘、盘口缺失）
// 不计入分母——不适用不等于没中。
func finaleAccuracyOf(records []models.FinalePrediction) gin.H {
	for i := range records {
		finaleApplyOuHeatGate(&records[i])
	}
	settled := 0
	for _, record := range records {
		if record.Settled {
			settled++
		}
	}
	columns := make([]gin.H, 0, len(finaleColumns))
	for _, column := range finaleColumns {
		matched, hit := 0, 0
		for _, record := range records {
			value := column.Hit(record)
			if value == nil {
				continue
			}
			matched++
			if *value {
				hit++
			}
		}
		accuracy := 0.0
		if matched > 0 {
			accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
		}
		columns = append(columns, gin.H{
			"key": column.Key, "label": column.Label,
			"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		})
	}
	return gin.H{"settled_total": settled, "columns": columns}
}

func finaleEmptyAccuracy() gin.H {
	return finaleAccuracyOf(nil)
}
