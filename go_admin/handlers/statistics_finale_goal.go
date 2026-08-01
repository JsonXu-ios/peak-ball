// Package handlers: statistics_finale_goal.go —「终章 · 大小球」预测与前瞻实测存档。
//
// 只做一件事：找出「期望球数与球数热度判到相反侧」的比赛，买大球。两种组合各带
// 自己的盘口区间限制（口径见 finaleGoalCombos），命中=实际打出大球。
//
// 结构与胜平负那版终章完全一致，共用同一套「赛前存档 → 完赛结算 → 历史回算」的
// 做法：
//
//  1. 未来窗口内尚未开赛、且命中组合的场次 upsert 进 finale_goal_predictions；
//     已开赛的行不再覆盖，赛前信号值就此冻结。
//  2. 扫存档里 settled=0 且已完赛的行，用存档里的盘口线判命中并写死。
//  3. 历史区间里没有赛前存档的已完赛比赛即时回算，只用于展示参考，绝不写库、
//     也绝不和存档命中率合并统计——混进去就毁了前瞻实测的可信度。
package handlers

import (
	"math"
	"net/http"
	"sort"
	"time"

	"go_admin/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm/clause"
)

const (
	// 反向·热度判小球（此时期望球数判大球）：只买盘口 3.75 以下的场次，3.75 本身剔除。
	finaleGoalComboUnderHeat = "split_under"
	// 反向·热度判大球（此时期望球数判小球）：只剔除 2.25 这一档，其余盘口全要。
	finaleGoalComboOverHeat = "split_over"

	// 盘口比较用的容差：盘口都是 0.25 的整数倍，浮点直接比会在 3.75 这种边界上翻车。
	finaleGoalLineEpsilon = 1e-6
)

// finaleGoalCombo 描述一种入选组合：要求的热度方向、盘口准入区间与展示名。
type finaleGoalCombo struct {
	Key      string
	Label    string
	Range    string             // 盘口区间的说明文案，前端直接展示
	HeatOver bool               // 该组合要求的球数热度方向
	accepts  func(float64) bool // 盘口是否准入
}

// finaleGoalCombos 两种组合的口径。两者都要求期望球数与热度反向，且都买大球。
// 盘口边界用 finaleGoalLineEpsilon 容差比较：盘口都是 0.25 的整数倍，浮点直接比
// 会在 3.25 / 3.75 这种边界值上翻车。
var finaleGoalCombos = []finaleGoalCombo{
	{
		Key: finaleGoalComboUnderHeat, Label: "反向·热度判小球",
		Range: "盘口 < 3.75（3.75 本身也剔除）", HeatOver: false,
		accepts: func(line float64) bool { return line < 3.75-finaleGoalLineEpsilon },
	},
	{
		Key: finaleGoalComboOverHeat, Label: "反向·热度判大球",
		Range: "只剔除 2.25 这一档，其余盘口不限", HeatOver: true,
		accepts: func(line float64) bool { return math.Abs(line-2.25) > finaleGoalLineEpsilon },
	},
}

func finaleGoalComboLabel(key string) string {
	for _, combo := range finaleGoalCombos {
		if combo.Key == key {
			return combo.Label
		}
	}
	return key
}

// GetFinaleGoalStatistics serves 终章的「大小球」选项卡。
//
//	默认：返回未来待赛预测。
//	mode=history（可带 start/end）：返回存档行 + 无存档场次的回算行。
func GetFinaleGoalStatistics(c *gin.Context) {
	start, end, err := statisticsDateRange(c.Query("start"), c.Query("end"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	history := c.Query("mode") == "history" || start != "" || end != ""

	upcoming, rows, err := buildFinaleGoalUpcoming()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	archiveErr := archiveFinaleGoalPredictions(rows)
	settleErr := settleFinaleGoalPredictions()

	predictions := make([]gin.H, 0, len(rows))
	accuracy := finaleGoalEmptyAccuracy()
	recomputeAccuracy := finaleGoalEmptyAccuracy()
	var rangeErr error

	if history {
		archived, err := loadFinaleGoalArchive(start, end)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		seen := make(map[string]struct{}, len(archived))
		for _, record := range archived {
			seen[record.MatchID] = struct{}{}
			predictions = append(predictions, finaleGoalArchiveRow(record, "archive"))
		}
		recomputed, err := buildFinaleGoalRecompute(start, end, seen)
		if err != nil {
			rangeErr = err
		}
		for _, record := range recomputed {
			predictions = append(predictions, finaleGoalArchiveRow(record, "recompute"))
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
		accuracy = finaleGoalAccuracyOf(archived)
		recomputeAccuracy = finaleGoalAccuracyOf(recomputed)
	} else {
		for _, row := range rows {
			predictions = append(predictions, row.payload())
		}
		var err error
		accuracy, err = buildFinaleGoalAccuracy("", "")
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
	if warning := firstError(archiveErr, settleErr, rangeErr); warning != "" {
		response["warning"] = warning
	}
	c.JSON(http.StatusOK, response)
}

// ---------- 待赛预测计算 ----------

// finaleGoalRow 是一场待赛比赛算出来的大小球预测。
type finaleGoalRow struct {
	match     statisticsMatch
	combo     string
	expGoals  string
	heatGoals string
	ouText    string
	ouLine    float64
}

func (r finaleGoalRow) payload() gin.H {
	return gin.H{
		"match_id":    r.match.ID,
		"date":        r.match.Date,
		"match_time":  r.match.MatchTime,
		"league":      r.match.League,
		"home":        r.match.Home,
		"guest":       r.match.Guest,
		"home_logo":   r.match.HomeLogo,
		"guest_logo":  r.match.GuestLogo,
		"combo":       r.combo,
		"combo_label": finaleGoalComboLabel(r.combo),
		"pick":        "over",
		"exp_goals":   r.expGoals,
		"heat_goals":  r.heatGoals,
		"ou_line":     r.ouText,
		"settled":     false,
	}
}

func (r finaleGoalRow) record(now time.Time) models.FinaleGoalPrediction {
	line := r.ouLine
	return models.FinaleGoalPrediction{
		MatchID: r.match.ID, Date: r.match.Date, MatchTime: r.match.MatchTime,
		League: r.match.League, Home: r.match.Home, Guest: r.match.Guest,
		HomeLogo: r.match.HomeLogo, GuestLogo: r.match.GuestLogo,
		Combo: r.combo, Pick: "over",
		ExpGoals: r.expGoals, HeatGoals: r.heatGoals, OuLine: r.ouText,
		OuLineValue: &line,
		SnapshotAt:  now,
	}
}

// buildFinaleGoalUpcoming 计算未来窗口内每场待赛比赛，返回待赛总场次与入选的行。
func buildFinaleGoalUpcoming() (int, []finaleGoalRow, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		return 0, nil, err
	}
	today, horizon := finaleUpcomingWindow()

	upcoming := make([]statisticsMatch, 0, 64)
	ids := make([]string, 0, 64)
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if !finaleInWindow(match, today, horizon) {
			continue
		}
		upcoming = append(upcoming, match)
		ids = append(ids, match.ID)
	}

	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)

	rows := make([]finaleGoalRow, 0, len(upcoming))
	for _, match := range upcoming {
		if row, ok := buildFinaleGoalRow(match, histories[match.ID], pankous[match.ID]); ok {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].match.MatchTime != rows[j].match.MatchTime {
			return rows[i].match.MatchTime < rows[j].match.MatchTime
		}
		return rows[i].match.ID < rows[j].match.ID
	})
	return len(upcoming), rows, nil
}

// buildFinaleGoalRow 判断一场比赛是否命中两种组合之一。口径与「完赛基础统计」
// #17/#18 完全一致：期望球数=0.3×历史场均+0.7×近期场均，热度方向=等权综合均值
// 对盘口的方向，两者判到相反侧才算数；再按各自的盘口区间过滤。
func buildFinaleGoalRow(match statisticsMatch, historyRow, pankouRow map[string]interface{}) (finaleGoalRow, bool) {
	_, ouLine, hasOU := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
	if !hasOU {
		return finaleGoalRow{}, false
	}
	against, homeRecent, guestRecent := statisticsHistory(historyRow)
	_, historyGoals, hasHistory := statisticsHeadToHead(match, against)
	recentGoals, hasRecentGoals := statisticsRecentGoals(homeRecent, guestRecent)

	// 两队没有交锋记录的比赛整场剔除：交锋虽只占 0.3 的权重，缺了它
	// 算出来的不是同一个口径。近期样本缺失时用交锋单独顶上。
	if !hasHistory {
		return finaleGoalRow{}, false
	}
	expected, _ := statisticsGoalsExpected(historyGoals, hasHistory, recentGoals, hasRecentGoals)
	if math.Abs(expected-ouLine) < statisticsPushEpsilon {
		return finaleGoalRow{}, false // 期望正好落在盘口线上，没有方向
	}
	heatValue := statisticsMean(recentGoals, hasRecentGoals, historyGoals, hasHistory)
	heatOver := heatValue >= ouLine
	expectedOver := expected > ouLine
	if heatOver == expectedOver {
		return finaleGoalRow{}, false // 同向，两种组合都不要
	}

	for _, combo := range finaleGoalCombos {
		if combo.HeatOver != heatOver || !combo.accepts(ouLine) {
			continue
		}
		return finaleGoalRow{
			match:     match,
			combo:     combo.Key,
			expGoals:  statisticsGoalsDirText(expected, expectedOver),
			heatGoals: statisticsGoalsDirText(heatValue, heatOver),
			ouText:    statisticsFormatLine(ouLine),
			ouLine:    ouLine,
		}, true
	}
	return finaleGoalRow{}, false
}

// ---------- 存档 ----------

// archiveFinaleGoalPredictions upserts 尚未开赛的预测行。已结算或已开赛的场次不再
// 覆盖，赛前信号值就此冻结。
func archiveFinaleGoalPredictions(rows []finaleGoalRow) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now()
	cutoff := now.Format("2006-01-02 15:04")

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.match.ID)
	}
	frozen := map[string]struct{}{}
	var settledIDs []string
	if err := statisticsDB().Model(&models.FinaleGoalPrediction{}).
		Where("match_id IN ? AND settled = ?", ids, true).Pluck("match_id", &settledIDs).Error; err != nil {
		return err
	}
	for _, id := range settledIDs {
		frozen[id] = struct{}{}
	}

	records := make([]models.FinaleGoalPrediction, 0, len(rows))
	for _, row := range rows {
		if _, ok := frozen[row.match.ID]; ok {
			continue
		}
		if row.match.MatchTime != "" && row.match.MatchTime <= cutoff {
			continue
		}
		records = append(records, row.record(now))
	}
	if len(records) == 0 {
		return nil
	}
	return statisticsDB().Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "match_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"date", "match_time", "league", "home", "guest", "home_logo", "guest_logo",
			"combo", "pick", "exp_goals", "heat_goals", "ou_line", "ou_line_value",
			"snapshot_at", "updated_at",
		}),
	}).CreateInBatches(&records, 200).Error
}

// ---------- 结算 ----------

// settleFinaleGoalPredictions 扫存档里 settled=0 的行，已完赛的判命中并写死。
// 已结算的行一律不碰，所以这个函数是幂等的。
func settleFinaleGoalPredictions() error {
	var pending []models.FinaleGoalPrediction
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
		// 用 map 更新：nil 的 hit_pick 必须真的写成 NULL，struct 更新会当零值跳过。
		if err := statisticsDB().Model(&models.FinaleGoalPrediction{}).
			Where("id = ?", record.ID).Updates(settleFinaleGoalRecord(&record, match, now)).Error; err != nil {
			return err
		}
	}
	return nil
}

// settleFinaleGoalRecord 按比分判「买大球」是否命中：必须用存档时的盘口线，走盘
// （总进球正好等于盘口）留 nil 不进分母。
func settleFinaleGoalRecord(record *models.FinaleGoalPrediction, match statisticsMatch, now time.Time) map[string]interface{} {
	over, valid := false, false
	if record.OuLineValue != nil {
		over, valid = statisticsOverOutcome(match, *record.OuLineValue)
	}

	record.Settled = true
	record.SettledAt = &now
	record.HomeScore = match.HomeScore
	record.GuestScore = match.GuestScore
	record.Result = ""
	record.HitPick = nil
	if valid {
		record.Result = finaleOverDirection(over)
		hit := over // 两种组合都买大球，打出大球即命中
		record.HitPick = &hit
	}

	return map[string]interface{}{
		"settled":     record.Settled,
		"settled_at":  record.SettledAt,
		"home_score":  record.HomeScore,
		"guest_score": record.GuestScore,
		"result":      record.Result,
		"hit_pick":    record.HitPick,
	}
}

// ---------- 历史查询与命中率汇总 ----------

// buildFinaleGoalRecompute 对区间内「没有赛前存档」的已完赛比赛即时回算。
// 读的是库里当前的盘口，不是赛前那一刻的值——只用于展示参考，绝不写库、也绝不和
// 存档的命中率合并统计。
func buildFinaleGoalRecompute(start, end string, archived map[string]struct{}) ([]models.FinaleGoalPrediction, error) {
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

	now := time.Now()
	records := make([]models.FinaleGoalPrediction, 0, len(matches))
	for _, match := range matches {
		row, ok := buildFinaleGoalRow(match, histories[match.ID], pankous[match.ID])
		if !ok {
			continue
		}
		record := row.record(now)
		settleFinaleGoalRecord(&record, match, now)
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

func loadFinaleGoalArchive(start, end string) ([]models.FinaleGoalPrediction, error) {
	var records []models.FinaleGoalPrediction
	query := statisticsDB().Model(&models.FinaleGoalPrediction{})
	if start != "" {
		query = query.Where("date >= ?", start)
	}
	if end != "" {
		query = query.Where("date <= ?", end)
	}
	err := query.Order("match_time ASC, match_id ASC").Find(&records).Error
	return records, err
}

// finaleGoalArchiveRow 渲染一条存档/回算行。source 必须如实标注：archive=赛前存档
// （真前瞻），recompute=事后用当前盘口回算（仅参考），前端据此打标签。
func finaleGoalArchiveRow(record models.FinaleGoalPrediction, source string) gin.H {
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
		"combo":       record.Combo,
		"combo_label": finaleGoalComboLabel(record.Combo),
		"pick":        record.Pick,
		"exp_goals":   record.ExpGoals,
		"heat_goals":  record.HeatGoals,
		"ou_line":     record.OuLine,
		"settled":     record.Settled,
		"home_score":  record.HomeScore,
		"guest_score": record.GuestScore,
		"result":      record.Result,
		"snapshot_at": record.SnapshotAt.Format("2006-01-02 15:04"),
		"hit_pick":    record.HitPick,
	}
}

// buildFinaleGoalAccuracy 汇总赛前存档的命中率。区间为空时统计全部已结算存档。
func buildFinaleGoalAccuracy(start, end string) (gin.H, error) {
	query := statisticsDB().Model(&models.FinaleGoalPrediction{}).Where("settled = ?", true)
	if start != "" {
		query = query.Where("date >= ?", start)
	}
	if end != "" {
		query = query.Where("date <= ?", end)
	}
	var records []models.FinaleGoalPrediction
	if err := query.Find(&records).Error; err != nil {
		return finaleGoalEmptyAccuracy(), err
	}
	return finaleGoalAccuracyOf(records), nil
}

// finaleGoalAccuracyOf 按组合聚合命中率，外加一行合计。hit_pick 为 nil 的行（走盘、
// 盘口缺失）不计入分母——不适用不等于没中。
func finaleGoalAccuracyOf(records []models.FinaleGoalPrediction) gin.H {
	settled := 0
	for _, record := range records {
		if record.Settled {
			settled++
		}
	}
	tally := func(filter func(models.FinaleGoalPrediction) bool) gin.H {
		matched, hit := 0, 0
		for _, record := range records {
			if !filter(record) || record.HitPick == nil {
				continue
			}
			matched++
			if *record.HitPick {
				hit++
			}
		}
		accuracy := 0.0
		if matched > 0 {
			accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
		}
		return gin.H{"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy}
	}

	columns := make([]gin.H, 0, len(finaleGoalCombos)+1)
	total := tally(func(models.FinaleGoalPrediction) bool { return true })
	total["key"], total["label"], total["range"] = "all", "合计（买大球）", "两种组合汇总"
	columns = append(columns, total)
	for _, combo := range finaleGoalCombos {
		key := combo.Key
		row := tally(func(record models.FinaleGoalPrediction) bool { return record.Combo == key })
		row["key"], row["label"], row["range"] = key, combo.Label, combo.Range
		columns = append(columns, row)
	}
	return gin.H{"settled_total": settled, "columns": columns}
}

func finaleGoalEmptyAccuracy() gin.H {
	return finaleGoalAccuracyOf(nil)
}
