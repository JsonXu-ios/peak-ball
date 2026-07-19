package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// 命中率统计（原 MatchListHome 统计页的前端计算，整体迁到后端）。
// 口径与旧前端一致：固定规则池从 accuracyRuleStartDate 起到今天，
// 庄家/平台预测取 platform 决策块，规则匹配用 snapshot（缺失时现算兜底）。

const accuracyRuleStartDate = "2026-05-28"

type accuracyStatsRow struct {
	Label            string `json:"label"`
	Sample           int    `json:"sample"`
	BookmakerCorrect int    `json:"bookmakerCorrect"`
	PlatformCorrect  int    `json:"platformCorrect"`
	BothCorrect      int    `json:"bothCorrect"`
}

type accuracyOverallStats struct {
	Sample           int `json:"sample"`
	BookmakerCorrect int `json:"bookmakerCorrect"`
	PlatformCorrect  int `json:"platformCorrect"`
}

type evilCultAccuracyRow struct {
	Label          string `json:"label"`
	Sample         int    `json:"sample"`
	UnderCorrect   int    `json:"underCorrect"`
	OverCorrect    int    `json:"overCorrect"`
	FirstCorrect   int    `json:"firstCorrect"`
	MainCorrect    int    `json:"mainCorrect"`
	ReverseCorrect int    `json:"reverseCorrect"`
}

type accuracyFitSummary struct {
	Label     string  `json:"label"`
	Tone      string  `json:"tone"`
	Score     float64 `json:"score"`
	RuleCount int     `json:"ruleCount"`
	Rate      float64 `json:"rate"`
	Sample    int     `json:"sample"`
}

type accuracyMatchRow struct {
	MatchID       string             `json:"matchId"`
	Date          string             `json:"date"`
	MatchTitle    string             `json:"matchTitle"`
	League        string             `json:"league"`
	Time          string             `json:"time"`
	OutcomeFit    accuracyFitSummary `json:"outcomeFit"`
	GoalFit       accuracyFitSummary `json:"goalFit"`
	ScoreFit      accuracyFitSummary `json:"scoreFit"`
	Conclusion    string             `json:"conclusion"`
	Tone          string             `json:"tone"`
	Evidence      string             `json:"evidence"`
	ResultSummary string             `json:"resultSummary"`
	ResultTone    string             `json:"resultTone"`
}

type accuracyStatsSummary struct {
	StartDate           string                       `json:"startDate"`
	EndDate             string                       `json:"endDate"`
	Total               int                          `json:"total"`
	Overall             accuracyOverallStats         `json:"overall"`
	Rows                []accuracyStatsRow           `json:"rows"`
	EvilCultRows        []evilCultAccuracyRow        `json:"evilCultRows"`
	CommonRows          []analysisRuleSnapshotCommon `json:"commonRows"`
	GeneratedCommonRows []analysisRuleSnapshotCommon `json:"generatedCommonRows"`
	MatchRows           []accuracyMatchRow           `json:"matchRows"`
	SettledFitRows      []accuracyMatchRow           `json:"settledFitRows"`
}

// GetAnalysisAccuracyStats 返回历史完赛命中统计与当前日期的规则匹配行。
func GetAnalysisAccuracyStats(c *gin.Context) {
	selectedDate := strings.TrimSpace(c.DefaultQuery("date", time.Now().Format("2006-01-02")))
	if _, err := time.Parse("2006-01-02", selectedDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}
	scope := c.Query("scope")
	league := strings.TrimSpace(c.Query("league"))
	endDate := time.Now().Format("2006-01-02")

	poolItems, err := loadAccuracyItems(accuracyRuleStartDate, endDate, scope, league)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	settled := make([]analysisMatchResponse, 0, len(poolItems))
	for _, item := range poolItems {
		if accuracySettled(item) {
			settled = append(settled, item)
		}
	}

	currentItems, err := loadAccuracyItems(selectedDate, selectedDate, scope, league)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	generated := buildAccuracyCommonRowsFromItems(settled)
	commonRows := snapshotCommonRowsOrNil()
	if commonRows == nil {
		commonRows = generated
	}

	rows := buildAccuracyStatsRows(settled)
	summary := accuracyStatsSummary{
		StartDate:           accuracyRuleStartDate,
		EndDate:             endDate,
		Total:               len(settled),
		Overall:             buildAccuracyOverall(rows),
		Rows:                rows,
		EvilCultRows:        buildEvilCultAccuracyRows(settled),
		CommonRows:          commonRows,
		GeneratedCommonRows: generated,
		MatchRows:           buildAccuracyMatchRows(currentItems, commonRows, "upcoming"),
		SettledFitRows:      buildAccuracyMatchRows(currentItems, commonRows, "settledFit"),
	}
	c.JSON(http.StatusOK, summary)
}

func loadAccuracyItems(startDate string, endDate string, scope string, league string) ([]analysisMatchResponse, error) {
	var matches []models.Money
	query := database.DB.Where("date BETWEEN ? AND ?", startDate, endDate)
	if scope != "all" {
		query = query.Where("jingcai_id IS NOT NULL AND TRIM(jingcai_id) <> ?", "")
	}
	query = query.Where("display_state IS NULL OR display_state <> ?", detailOnlyDisplayState)
	if err := query.Order("match_time ASC").Find(&matches).Error; err != nil {
		return nil, err
	}

	items := make([]analysisMatchResponse, 0, len(matches))
	for _, match := range matches {
		item := buildAnalysisWithWeights(match, false)
		if league != "" && league != "all" && item.League != league {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func accuracySettled(item analysisMatchResponse) bool {
	return strings.Contains(item.DisplayState, "完") || item.Status >= 4
}

func accuracyBookmakerPrediction(item analysisMatchResponse) platformGuidePrediction {
	if item.Platform == nil {
		return platformGuidePrediction{Outcome: "draw"}
	}
	return item.Platform.Bookmaker
}

func accuracyPlatformPrediction(item analysisMatchResponse) platformGuidePrediction {
	if item.Platform == nil {
		return platformGuidePrediction{Outcome: "draw"}
	}
	return item.Platform.Platform
}

func accuracyActualOutcome(item analysisMatchResponse) string {
	return []string{"home", "draw", "away"}[actualOutcomeIndex(item.HomeScore, item.GuestScore)]
}

func accuracyActualTotal(item analysisMatchResponse) int {
	return item.HomeScore + item.GuestScore
}

var accuracyGoalRangePattern = regexp.MustCompile(`(\d+)\s*-\s*(\d+)球`)
var accuracyScorePattern = regexp.MustCompile(`^(\d+):(\d+)$`)

func accuracyGoalCorrect(item analysisMatchResponse, goal platformGoalResult) bool {
	total := accuracyActualTotal(item)
	label := goal.Label
	if strings.Contains(label, "以内") {
		return total <= goal.Total
	}
	if strings.Contains(label, "以上") {
		return total >= goal.Total
	}
	if matches := accuracyGoalRangePattern.FindStringSubmatch(label); len(matches) == 3 {
		low := int(parseFloat(matches[1]))
		high := int(parseFloat(matches[2]))
		return total >= low && total <= high
	}
	return total == goal.Total
}

func accuracyScoreCorrect(item analysisMatchResponse, score string) bool {
	matches := accuracyScorePattern.FindStringSubmatch(strings.TrimSpace(score))
	if len(matches) != 3 {
		return false
	}
	return int(parseFloat(matches[1])) == item.HomeScore && int(parseFloat(matches[2])) == item.GuestScore
}

func buildAccuracyStatsRows(settled []analysisMatchResponse) []accuracyStatsRow {
	outcomeRow := accuracyStatsRow{Label: "胜平负"}
	goalRow := accuracyStatsRow{Label: "大小球"}
	scoreRow := accuracyStatsRow{Label: "比分"}

	for _, item := range settled {
		bookmaker := accuracyBookmakerPrediction(item)
		platform := accuracyPlatformPrediction(item)
		actual := accuracyActualOutcome(item)

		bookmakerOutcomeHit := bookmaker.Outcome == actual
		platformOutcomeHit := platform.Outcome == actual
		outcomeRow.Sample++
		if bookmakerOutcomeHit {
			outcomeRow.BookmakerCorrect++
		}
		if platformOutcomeHit {
			outcomeRow.PlatformCorrect++
		}
		if bookmakerOutcomeHit && platformOutcomeHit {
			outcomeRow.BothCorrect++
		}

		bookmakerGoalHit := accuracyGoalCorrect(item, bookmaker.Goal)
		platformGoalHit := accuracyGoalCorrect(item, platform.Goal)
		goalRow.Sample++
		if bookmakerGoalHit {
			goalRow.BookmakerCorrect++
		}
		if platformGoalHit {
			goalRow.PlatformCorrect++
		}
		if bookmakerGoalHit && platformGoalHit {
			goalRow.BothCorrect++
		}

		bookmakerScoreHit := accuracyScoreCorrect(item, bookmaker.Score)
		platformScoreHit := accuracyScoreCorrect(item, platform.Score)
		scoreRow.Sample++
		if bookmakerScoreHit {
			scoreRow.BookmakerCorrect++
		}
		if platformScoreHit {
			scoreRow.PlatformCorrect++
		}
		// 比分：庄家或平台任一命中即算命中。
		if bookmakerScoreHit || platformScoreHit {
			scoreRow.BothCorrect++
		}
	}
	return []accuracyStatsRow{outcomeRow, goalRow, scoreRow}
}

func buildAccuracyOverall(rows []accuracyStatsRow) accuracyOverallStats {
	overall := accuracyOverallStats{}
	for _, row := range rows {
		overall.Sample += row.Sample
		overall.BookmakerCorrect += row.BookmakerCorrect
		overall.PlatformCorrect += row.PlatformCorrect
	}
	return overall
}

func buildEvilCultAccuracyRows(settled []analysisMatchResponse) []evilCultAccuracyRow {
	overall := evilCultAccuracyRow{Label: "综合"}
	goal := evilCultAccuracyRow{Label: "大小球"}
	total := evilCultAccuracyRow{Label: "球数"}
	score := evilCultAccuracyRow{Label: "比分"}
	outcome := evilCultAccuracyRow{Label: "胜平负"}

	for _, item := range settled {
		if item.Platform == nil {
			continue
		}
		prediction := item.Platform.EvilCult.Prediction
		actualOutcome := accuracyActualOutcome(item)
		actualTotal := accuracyActualTotal(item)

		checks := []struct {
			row   *evilCultAccuracyRow
			under bool
			over  bool
		}{
			{&goal, evilCultGoalDirectionCorrect(actualTotal, "under", prediction.UnderGoalLine), evilCultGoalDirectionCorrect(actualTotal, "over", prediction.OverGoalLine)},
			{&total, actualTotal == prediction.UnderTotalValue, actualTotal == prediction.OverTotalValue},
			{&score, accuracyScoreCorrect(item, prediction.UnderScore), accuracyScoreCorrect(item, prediction.OverScore)},
			{&outcome, actualOutcome == prediction.UnderOutcome, actualOutcome == prediction.OverOutcome},
		}
		for _, check := range checks {
			first := check.over
			if prediction.FirstDirection == "under" {
				first = check.under
			}
			main := check.over
			reverse := check.under
			if prediction.GoalDirection == "under" {
				main = check.under
				reverse = check.over
			}
			addEvilCultAccuracy(check.row, check.under, check.over, first, main, reverse)
			addEvilCultAccuracy(&overall, check.under, check.over, first, main, reverse)
		}
	}
	return []evilCultAccuracyRow{overall, goal, total, score, outcome}
}

func addEvilCultAccuracy(row *evilCultAccuracyRow, under bool, over bool, first bool, main bool, reverse bool) {
	row.Sample++
	if under {
		row.UnderCorrect++
	}
	if over {
		row.OverCorrect++
	}
	if first {
		row.FirstCorrect++
	}
	if main {
		row.MainCorrect++
	}
	if reverse {
		row.ReverseCorrect++
	}
}

func evilCultGoalDirectionCorrect(actualTotal int, direction string, line float64) bool {
	if !pfFinite(line) {
		return false
	}
	if direction == "over" {
		return float64(actualTotal) > line
	}
	return float64(actualTotal) < line
}

// ---------- 规则元素提取（与旧前端 resultCommonElements 等一致） ----------

func accuracyGoalDirectionLabel(goal platformGoalResult) string {
	if strings.Contains(goal.Label, "以上") {
		return "大球"
	}
	if strings.Contains(goal.Label, "以内") {
		return "小球"
	}
	return "盘口球"
}

func accuracyGoalBalanceLabel(signal string) string {
	switch signal {
	case "underHidden":
		return "小球隐藏"
	case "under":
		return "小球"
	case "overCorrected":
		return "大球修正"
	case "over":
		return "大球"
	default:
		return "-"
	}
}

func accuracyJoin(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, " / ")
}

func accuracyResultElements(item analysisMatchResponse) []string {
	bookmaker := accuracyBookmakerPrediction(item)
	platform := accuracyPlatformPrediction(item)
	sportteryComfort, rqComfort, professionalConsensus, handicapLabel := "", "", "", ""
	drawRiskScore := 0.0
	platformWarning := ""
	if item.Platform != nil {
		sportteryComfort = item.Platform.SportteryComfort
		rqComfort = item.Platform.RqspfComfort
		professionalConsensus = item.Platform.ProfessionalConsensus
		handicapLabel = item.Platform.HandicapPressureLabel
		drawRiskScore = item.Platform.DrawRisk.Score
		platformWarning = item.Platform.Platform.Warning
	}

	aligned := "庄平分歧"
	if bookmaker.Outcome == platform.Outcome {
		aligned = "庄平同向" + analysisRuleOutcomeLabel(bookmaker.Outcome)
	}
	elements := []string{
		"庄家" + analysisRuleOutcomeLabel(bookmaker.Outcome),
		"平台" + analysisRuleOutcomeLabel(platform.Outcome),
		aligned,
		analysisRuleOptionalLabel("凯体同向", professionalConsensus, analysisRuleOutcomeLabel),
		"凯利" + accuracyJoin(item.KaiLiResult),
		"体彩" + accuracyJoin(item.TiCaiResult),
		"亚盘" + analysisRuleHandicapBucket(item.YapanPankou2),
		"让球热度" + analysisRuleHeatBucket(item.YaPanTouZhu, "主热", "客热"),
		analysisRuleOptionalLabel("竞彩舒服", sportteryComfort, analysisRuleOutcomeLabel),
		analysisRuleOptionalLabel("让球舒服", rqComfort, analysisRuleOutcomeLabel),
	}
	if platformWarning != "" {
		elements = append(elements, "平台过热")
	}
	if handicapLabel != "" {
		elements = append(elements, "让球"+handicapLabel)
	}
	if drawRiskScore >= 4 {
		elements = append(elements, "平局风险高")
	}
	if drawRiskScore >= 5 {
		elements = append(elements, "平局风险强")
	}
	return elements
}

func accuracyGoalElements(item analysisMatchResponse) []string {
	bookmaker := accuracyBookmakerPrediction(item)
	platform := accuracyPlatformPrediction(item)
	signal := ""
	if item.Platform != nil {
		signal = item.Platform.GoalBalanceSignal
	}
	bookmakerLabel := accuracyGoalDirectionLabel(bookmaker.Goal)
	platformLabel := accuracyGoalDirectionLabel(platform.Goal)
	aligned := "庄平球数分歧"
	if bookmakerLabel == platformLabel {
		aligned = "庄平同向" + bookmakerLabel
	}
	elements := []string{
		"庄家" + bookmakerLabel,
		"平台" + platformLabel,
		aligned,
		"盘口" + analysisRuleGoalLineBucket(item.QiushuPankou2),
		"大小热度" + analysisRuleHeatBucket(item.QiuShuTouZhu, "大热", "小热"),
	}
	if signal != "" {
		elements = append(elements, "回归"+accuracyGoalBalanceLabel(signal))
	}
	return elements
}

func accuracyScoreElements(item analysisMatchResponse) []string {
	bookmaker := accuracyBookmakerPrediction(item)
	platform := accuracyPlatformPrediction(item)
	bookmakerShape := analysisRuleScoreShape(bookmaker.Score)
	platformShape := analysisRuleScoreShape(platform.Score)
	aligned := "庄平比分分歧"
	if bookmaker.Score == platform.Score {
		aligned = "庄平同比分" + bookmaker.Score
	}
	return []string{
		"庄家" + bookmaker.Score,
		"平台" + platform.Score,
		aligned,
		analysisRuleOptionalText("庄家形态", bookmakerShape),
		analysisRuleOptionalText("平台形态", platformShape),
		analysisRuleAlignedNonEmptyLabel("庄平同形态", bookmakerShape, platformShape),
		"庄家赛果" + analysisRuleOutcomeLabel(bookmaker.Outcome),
		"平台赛果" + analysisRuleOutcomeLabel(platform.Outcome),
		"庄家球数" + accuracyGoalDirectionLabel(bookmaker.Goal),
		"平台球数" + accuracyGoalDirectionLabel(platform.Goal),
		"亚盘" + analysisRuleHandicapBucket(item.YapanPankou2),
		"大小盘口" + analysisRuleGoalLineBucket(item.QiushuPankou2),
	}
}

// ---------- 规则池生成与匹配 ----------

func buildAccuracyCommonRowsFromItems(settled []analysisMatchResponse) []analysisRuleSnapshotCommon {
	return []analysisRuleSnapshotCommon{
		buildAccuracyCommonRowFromItems("胜平负双中", settled, func(item analysisMatchResponse) bool {
			actual := accuracyActualOutcome(item)
			return accuracyBookmakerPrediction(item).Outcome == actual && accuracyPlatformPrediction(item).Outcome == actual
		}, accuracyResultElements),
		buildAccuracyCommonRowFromItems("大小球双中", settled, func(item analysisMatchResponse) bool {
			return accuracyGoalCorrect(item, accuracyBookmakerPrediction(item).Goal) && accuracyGoalCorrect(item, accuracyPlatformPrediction(item).Goal)
		}, accuracyGoalElements),
		buildAccuracyCommonRowFromItems("比分命中", settled, func(item analysisMatchResponse) bool {
			return accuracyScoreCorrect(item, accuracyBookmakerPrediction(item).Score) || accuracyScoreCorrect(item, accuracyPlatformPrediction(item).Score)
		}, accuracyScoreElements),
	}
}

func buildAccuracyCommonRowFromItems(
	label string,
	items []analysisMatchResponse,
	predicate func(analysisMatchResponse) bool,
	extractor func(analysisMatchResponse) []string,
) analysisRuleSnapshotCommon {
	rules := map[string]*analysisRuleSnapshotRule{}
	bothSample := 0
	for _, item := range items {
		bothCorrect := predicate(item)
		if bothCorrect {
			bothSample++
		}
		for _, value := range uniqueAnalysisRuleStrings(extractor(item)) {
			current := rules[value]
			if current == nil {
				current = &analysisRuleSnapshotRule{Value: value}
				rules[value] = current
			}
			current.Sample++
			if bothCorrect {
				current.BothCorrect++
			}
			current.Rate = round4(float64(current.BothCorrect) / float64(current.Sample))
		}
	}

	isScoreRow := strings.Contains(label, "比分")
	minSample := maxInt(2, int(math.Ceil(float64(len(items))*0.08)))
	minRate := 0.45
	if isScoreRow {
		minSample = maxInt(4, int(math.Ceil(float64(len(items))*0.04)))
		minRate = 0.28
	}
	out := make([]analysisRuleSnapshotRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Sample >= minSample && rule.BothCorrect > 0 && rule.Rate >= minRate {
			out = append(out, *rule)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rate != out[j].Rate {
			return out[i].Rate > out[j].Rate
		}
		if out[i].BothCorrect != out[j].BothCorrect {
			return out[i].BothCorrect > out[j].BothCorrect
		}
		if out[i].Sample != out[j].Sample {
			return out[i].Sample > out[j].Sample
		}
		return out[i].Value < out[j].Value
	})
	if len(out) > 8 {
		out = out[:8]
	}
	return analysisRuleSnapshotCommon{Label: label, Sample: bothSample, Rules: out}
}

func snapshotCommonRowsOrNil() []analysisRuleSnapshotCommon {
	content, err := readAnalysisRuleSnapshot()
	if err != nil {
		return nil
	}
	var snapshot analysisRuleSnapshotResponse
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return nil
	}
	hasRules := false
	for _, row := range snapshot.CommonRows {
		if len(row.Rules) > 0 {
			hasRules = true
			break
		}
	}
	if !hasRules {
		return nil
	}
	return snapshot.CommonRows
}

func orderedAccuracyCommonRows(rows []analysisRuleSnapshotCommon) [3]analysisRuleSnapshotCommon {
	find := func(keyword string, fallbackIndex int) analysisRuleSnapshotCommon {
		for _, row := range rows {
			if strings.Contains(row.Label, keyword) {
				return row
			}
		}
		if fallbackIndex < len(rows) {
			return rows[fallbackIndex]
		}
		return analysisRuleSnapshotCommon{Label: keyword}
	}
	return [3]analysisRuleSnapshotCommon{
		find("胜平负", 0),
		find("大小球", 1),
		find("比分", 2),
	}
}

func matchAccuracyRules(row analysisRuleSnapshotCommon, elements []string) []analysisRuleSnapshotRule {
	elementSet := map[string]bool{}
	for _, value := range uniqueAnalysisRuleStrings(elements) {
		elementSet[value] = true
	}
	matched := []analysisRuleSnapshotRule{}
	for _, rule := range row.Rules {
		if elementSet[rule.Value] {
			matched = append(matched, rule)
		}
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].Rate != matched[j].Rate {
			return matched[i].Rate > matched[j].Rate
		}
		if matched[i].BothCorrect != matched[j].BothCorrect {
			return matched[i].BothCorrect > matched[j].BothCorrect
		}
		return matched[i].Sample > matched[j].Sample
	})
	return matched
}

func buildAccuracyFitSummary(rules []analysisRuleSnapshotRule) accuracyFitSummary {
	if len(rules) == 0 {
		return accuracyFitSummary{Label: "无匹配", Tone: "normal"}
	}
	sample, correct := 0, 0
	for _, rule := range rules {
		sample += rule.Sample
		correct += rule.BothCorrect
	}
	rate := 0.0
	if sample > 0 {
		rate = float64(correct) / float64(sample)
	}
	score := math.Min(100, math.Round(rate*100+math.Min(18, float64(len(rules))*4)))
	return accuracyFitSummary{
		Label:     fmt.Sprintf("%d条 %d%%", len(rules), int(math.Round(rate*100))),
		Tone:      accuracyScoreTone(score),
		Score:     score,
		RuleCount: len(rules),
		Rate:      rate,
		Sample:    sample,
	}
}

func accuracyScoreTone(score float64) string {
	if score >= 78 {
		return "green"
	}
	if score >= 58 {
		return "blue"
	}
	if score > 0 {
		return "red"
	}
	return "normal"
}

func accuracyConclusion(score float64) (string, string) {
	if score >= 78 {
		return "符合历史规律", "green"
	}
	if score >= 58 {
		return "部分符合", "blue"
	}
	if score > 0 {
		return "匹配偏弱", "red"
	}
	return "无历史支撑", "normal"
}

func accuracyRuleText(rule analysisRuleSnapshotRule) string {
	return fmt.Sprintf("%s %d/%d %d%%", rule.Value, rule.BothCorrect, rule.Sample, int(math.Round(rule.Rate*100)))
}

func isPredictableDoubleFit(fit accuracyFitSummary) bool {
	return fit.RuleCount >= 2 && fit.Rate >= 0.62 && fit.Score >= 70
}

func predictedDoubleHitSummary(outcomeFit accuracyFitSummary, goalFit accuracyFitSummary, scoreFit accuracyFitSummary) (string, string) {
	doubleValues := []string{}
	if isPredictableDoubleFit(outcomeFit) {
		doubleValues = append(doubleValues, "胜平负")
	}
	if isPredictableDoubleFit(goalFit) {
		doubleValues = append(doubleValues, "大小球")
	}
	scoreHit := isPredictableDoubleFit(scoreFit)
	if len(doubleValues) == 0 && !scoreHit {
		return "暂无命中预测", "normal"
	}
	strongCount := 0
	for _, fit := range []accuracyFitSummary{outcomeFit, goalFit, scoreFit} {
		if fit.Score >= 78 {
			strongCount++
		}
	}
	values := []string{}
	if len(doubleValues) > 0 {
		values = append(values, "预测双中 "+strings.Join(doubleValues, "/"))
	}
	if scoreHit {
		values = append(values, "预测比分命中")
	}
	tone := "blue"
	if strongCount > 0 {
		tone = "green"
	}
	return strings.Join(values, " + "), tone
}

func settledAccuracySummary(item analysisMatchResponse) (string, string) {
	if !accuracySettled(item) {
		return "待赛", "normal"
	}
	bookmaker := accuracyBookmakerPrediction(item)
	platform := accuracyPlatformPrediction(item)
	actual := accuracyActualOutcome(item)
	values := []string{}
	if bookmaker.Outcome == actual && platform.Outcome == actual {
		values = append(values, "胜平负")
	}
	if accuracyGoalCorrect(item, bookmaker.Goal) && accuracyGoalCorrect(item, platform.Goal) {
		values = append(values, "大小球")
	}
	if accuracyScoreCorrect(item, bookmaker.Score) || accuracyScoreCorrect(item, platform.Score) {
		values = append(values, "比分命中")
	}
	if len(values) == 0 {
		return "未命中", "red"
	}
	return strings.Join(values, "/"), "green"
}

func buildAccuracyMatchRowFor(item analysisMatchResponse, commonRows []analysisRuleSnapshotCommon) accuracyMatchRow {
	ordered := orderedAccuracyCommonRows(commonRows)
	matchedOutcome := matchAccuracyRules(ordered[0], accuracyResultElements(item))
	matchedGoal := matchAccuracyRules(ordered[1], accuracyGoalElements(item))
	matchedScore := matchAccuracyRules(ordered[2], accuracyScoreElements(item))
	outcomeFit := buildAccuracyFitSummary(matchedOutcome)
	goalFit := buildAccuracyFitSummary(matchedGoal)
	scoreFit := buildAccuracyFitSummary(matchedScore)
	totalScore := outcomeFit.Score*0.45 + goalFit.Score*0.35 + scoreFit.Score*0.2
	conclusion, tone := accuracyConclusion(totalScore)

	resultSummary, resultTone := "", ""
	if accuracySettled(item) {
		resultSummary, resultTone = settledAccuracySummary(item)
	} else {
		resultSummary, resultTone = predictedDoubleHitSummary(outcomeFit, goalFit, scoreFit)
	}

	evidenceParts := []string{}
	for index, rule := range matchedOutcome {
		if index >= 2 {
			break
		}
		evidenceParts = append(evidenceParts, accuracyRuleText(rule))
	}
	for index, rule := range matchedGoal {
		if index >= 2 {
			break
		}
		evidenceParts = append(evidenceParts, accuracyRuleText(rule))
	}
	if len(matchedScore) > 0 {
		evidenceParts = append(evidenceParts, accuracyRuleText(matchedScore[0]))
	}
	evidence := strings.Join(evidenceParts, "；")
	if evidence == "" {
		evidence = "暂无明显高命中规则匹配，按原预测谨慎处理。"
	}

	return accuracyMatchRow{
		MatchID:       item.MatchID,
		Date:          item.Date.Format("2006-01-02"),
		MatchTitle:    item.Home + " vs " + item.Guest,
		League:        firstNonEmptyString(item.League, "-"),
		Time:          item.MatchTime.Format("15:04"),
		OutcomeFit:    outcomeFit,
		GoalFit:       goalFit,
		ScoreFit:      scoreFit,
		Conclusion:    conclusion,
		Tone:          tone,
		Evidence:      evidence,
		ResultSummary: resultSummary,
		ResultTone:    resultTone,
	}
}

func isSettledFitRow(row accuracyMatchRow) bool {
	hasRuleMatch := row.Tone == "green" || isPredictableDoubleFit(row.OutcomeFit) || isPredictableDoubleFit(row.GoalFit) || isPredictableDoubleFit(row.ScoreFit)
	return hasRuleMatch && row.ResultTone == "green"
}

func buildAccuracyMatchRows(items []analysisMatchResponse, commonRows []analysisRuleSnapshotCommon, mode string) []accuracyMatchRow {
	rows := []accuracyMatchRow{}
	for _, item := range items {
		settled := accuracySettled(item)
		if mode == "upcoming" && settled {
			continue
		}
		if mode == "settledFit" && !settled {
			continue
		}
		rows = append(rows, buildAccuracyMatchRowFor(item, commonRows))
	}
	if mode == "settledFit" {
		filtered := []accuracyMatchRow{}
		for _, row := range rows {
			if isSettledFitRow(row) {
				filtered = append(filtered, row)
			}
		}
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].Date+" "+filtered[i].Time > filtered[j].Date+" "+filtered[j].Time
		})
		if len(filtered) > 30 {
			filtered = filtered[:30]
		}
		return filtered
	}
	return rows
}

// attachAccuracyFits 给列表/详情的每场比赛附上"匹配历史规律"结果（原前端本地计算）。
func attachAccuracyFits(items []analysisMatchResponse) {
	commonRows := snapshotCommonRowsOrNil()
	if commonRows == nil {
		commonRows = []analysisRuleSnapshotCommon{
			{Label: "胜平负双中"},
			{Label: "大小球双中"},
			{Label: "比分命中"},
		}
	}
	for index := range items {
		row := buildAccuracyMatchRowFor(items[index], commonRows)
		items[index].AccuracyFit = &row
	}
}
