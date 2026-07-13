package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go_admin/database"

	"github.com/gin-gonic/gin"
)

// GetMatchStatistics calculates the base accuracy report from every settled match.
// It deliberately reads the crawler tables directly: this report is not limited to
// sporttery matches and can be used as the stable foundation for later filters.
func GetMatchStatistics(c *gin.Context) {
	start, end, err := statisticsDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 YYYY-MM-DD"})
		return
	}

	var rawMatches []map[string]interface{}
	if err := database.DB.Table("moneys").Find(&rawMatches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	matches := make([]statisticsMatch, 0, len(rawMatches))
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if !match.Settled || match.ID == "" || (start != "" && match.Date < start) || (end != "" && match.Date > end) {
			continue
		}
		matches = append(matches, match)
		ids = append(ids, match.ID)
	}

	historyByMatch := loadStatisticsRows("history_moneys", ids)
	pankouByMatch := loadStatisticsRows("pankou_moneys", ids)
	oddsByMatch := loadStatisticsRows("odds_moneys", ids)
	report := buildMatchStatistics(matches, historyByMatch, pankouByMatch, oddsByMatch)
	report["start_date"] = start
	report["end_date"] = end
	report["generated_at"] = time.Now().Format(time.RFC3339)
	c.JSON(http.StatusOK, report)
}

type statisticsMatch struct {
	ID, Date, Home, Guest string
	HomeScore, GuestScore int
	Settled               bool
}

type statisticsHistoryMatch struct {
	Date, Home, Guest     string
	HomeScore, GuestScore int
}

type statisticsTeamForm struct {
	For, Against, Matches float64
}

type statisticsCounter struct{ Sample, Correct int }

func (s *statisticsCounter) add(correct bool) {
	s.Sample++
	if correct {
		s.Correct++
	}
}
func (s statisticsCounter) row(key, label string) gin.H {
	accuracy := 0.0
	if s.Sample > 0 {
		accuracy = math.Round(float64(s.Correct)/float64(s.Sample)*10000) / 100
	}
	return gin.H{"key": key, "label": label, "sample": s.Sample, "correct": s.Correct, "accuracy": accuracy}
}

func buildMatchStatistics(matches []statisticsMatch, histories, pankous, odds map[string]map[string]interface{}) gin.H {
	ahByLine, ouByLine := map[string]statisticsCounter{}, map[string]statisticsCounter{}
	kellySporttery := statisticsCounter{}
	heat := map[string]statisticsCounter{}
	for _, market := range []string{"ah", "ou"} {
		for _, threshold := range []int{60, 70, 80, 90} {
			heat[fmt.Sprintf("%s-%d", market, threshold)] = statisticsCounter{}
		}
	}
	metrics := map[string]statisticsCounter{}

	for _, match := range matches {
		history := histories[match.ID]
		pankou := pankous[match.ID]
		oddsRow := odds[match.ID]
		ahLine, hasAH := statisticsPankouLine(pankou, "bet365_asia", "asia_data")
		ouLine, hasOU := statisticsPankouLine(pankou, "bet365_dxq", "dxq_data")
		against, homeRecent, guestRecent := statisticsHistory(history)
		historyDiff, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		homeForm := statisticsRecentForm(homeRecent, match.Home)
		guestForm := statisticsRecentForm(guestRecent, match.Guest)
		recentDiff, hasRecentDiff := statisticsRecentDifference(homeForm, guestForm)
		recentGoals, hasRecentGoals := statisticsRecentGoals(homeRecent, guestRecent)
		attackDefenseGoals, hasAttackDefense := statisticsAttackDefenseGoals(homeForm, guestForm)

		// 1. Asian handicap line: settle the home side at the collected current line.
		if hasAH {
			if correct, valid := statisticsAsianCorrect(match, ahLine); valid {
				key := statisticsLineKey(ahLine)
				counter := ahByLine[key]
				counter.add(correct)
				ahByLine[key] = counter
			}
		}
		// 2. O/U line: use the current line and settle the over side; pushes are excluded.
		if hasOU {
			if correct, valid := statisticsOverCorrect(match, ouLine); valid {
				key := statisticsLineKey(ouLine)
				counter := ouByLine[key]
				counter.add(correct)
				ouByLine[key] = counter
			}
		}

		// 3. A prediction is included only when the Kelly and Sporttery proxies overlap.
		if choices := statisticsKellySportteryChoices(oddsRow); len(choices) > 0 {
			kellySporttery.add(choices[statisticsActualOutcome(match)])
		}

		probabilities := statisticsProbabilities(oddsRow)
		if len(probabilities) == 3 {
			if hasAH {
				homeHeat := statisticsAsianHeat(probabilities[0], probabilities[2], ahLine)
				if actual, valid := statisticsOutcomeFromValue(float64(match.HomeScore-match.GuestScore), 0); valid {
					pred, _ := statisticsOutcomeFromValue(homeHeat-50, 0)
					for _, threshold := range []int{60, 70, 80, 90} {
						if math.Max(homeHeat, 100-homeHeat) >= float64(threshold) {
							counter := heat[fmt.Sprintf("ah-%d", threshold)]
							counter.add(pred == actual)
							heat[fmt.Sprintf("ah-%d", threshold)] = counter
						}
					}
				}
			}
		}
		if hasOU && (hasRecentGoals || hasHistory) {
			expected := statisticsMean(recentGoals, hasRecentGoals, historyGoals, hasHistory)
			overHeat := statisticsClamp(50+(expected-ouLine)*18, 0, 100)
			if actual, valid := statisticsOverOutcome(match, ouLine); valid {
				prediction := overHeat >= 50
				for _, threshold := range []int{60, 70, 80, 90} {
					if math.Max(overHeat, 100-overHeat) >= float64(threshold) {
						counter := heat[fmt.Sprintf("ou-%d", threshold)]
						counter.add(prediction == actual)
						heat[fmt.Sprintf("ou-%d", threshold)] = counter
					}
				}
			}
		}

		// 5-7: handicap forecasts are compared with the normal-time result (home/draw/away).
		if hasHistory {
			statisticsAddOutcome(&metrics, "history_handicap", historyDiff, match)
		}
		if hasRecentDiff {
			statisticsAddOutcome(&metrics, "recent_handicap", recentDiff, match)
		}
		if composite, ok := statisticsAverage(historyDiff, hasHistory, recentDiff, hasRecentDiff, ahLine, hasAH); ok {
			statisticsAddOutcome(&metrics, "asian_composite", composite, match)
		}

		// 8-11: goal forecasts are compared against the current O/U line; pushes and equal forecasts are excluded.
		if hasOU {
			if hasHistory {
				statisticsAddGoals(&metrics, "history_goals", historyGoals, ouLine, match)
			}
			if hasRecentGoals {
				statisticsAddGoals(&metrics, "recent_goals", recentGoals, ouLine, match)
			}
			if composite, ok := statisticsAverage(historyGoals, hasHistory, recentGoals, hasRecentGoals); ok {
				statisticsAddGoals(&metrics, "ou_composite", composite, ouLine, match)
			}
			if hasAttackDefense {
				statisticsAddGoals(&metrics, "last_five_attack_defense", attackDefenseGoals, ouLine, match)
			}
			if equilibrium, ok := statisticsAverage(historyGoals, hasHistory, recentGoals, hasRecentGoals, attackDefenseGoals, hasAttackDefense); ok {
				if actual, valid := statisticsOverOutcome(match, 2.5); valid {
					metrics["balance_25"] = statisticsCounter{Sample: metrics["balance_25"].Sample + 1, Correct: metrics["balance_25"].Correct + statisticsBoolInt((equilibrium > 2.5) == actual)}
				}
			}
		}
	}

	return gin.H{
		"settled_total": len(matches),
		"groups": []gin.H{
			statisticsRowsGroup("asian_handicap_lines", "1. 亚盘各盘口正确率", "按当前亚盘的主队让球结算；走盘不计入样本。", ahByLine),
			statisticsRowsGroup("over_under_lines", "2. 大小球各盘口正确率", "按当前大小球盘口买大球结算；走盘不计入样本。", ouByLine),
			{"key": "kelly_sporttery", "title": "3. 凯利与体彩同向推测", "definition": "凯利结果与体彩参考结果有交集时纳入；实际赛果落在交集内即命中。", "rows": []gin.H{kellySporttery.row("kelly_sporttery", "凯利 × 体彩同向")}},
			statisticsHeatGroup(heat),
			{"key": "handicap_models", "title": "5-7. 让球模型正确率", "definition": "历史交锋只统计赛前 3 年内记录；近期状态取各队最近 5 场。", "rows": []gin.H{metrics["history_handicap"].row("history_handicap", "历史期望让球（3年内）"), metrics["recent_handicap"].row("recent_handicap", "近期状态让球"), metrics["asian_composite"].row("asian_composite", "亚盘综合均值")}},
			{"key": "goal_models", "title": "8-12. 球数模型正确率", "definition": "历史交锋只统计赛前 3 年内记录；与当前大小球盘口比较，走盘不计入样本。2.5 均衡值以全部可用球数预期均值判断大/小 2.5。", "rows": []gin.H{metrics["history_goals"].row("history_goals", "历史平均球数（3年内）"), metrics["recent_goals"].row("recent_goals", "近期平均球数"), metrics["ou_composite"].row("ou_composite", "大小球综合均值"), metrics["last_five_attack_defense"].row("last_five_attack_defense", "最近5场平均进球/丢球"), metrics["balance_25"].row("balance_25", "2.5 均衡值")}},
		},
	}
}

func statisticsRowsGroup(key, title, definition string, counters map[string]statisticsCounter) gin.H {
	keys := make([]string, 0, len(counters))
	for key := range counters {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, _ := strconv.ParseFloat(keys[i], 64)
		b, _ := strconv.ParseFloat(keys[j], 64)
		return a < b
	})
	rows := make([]gin.H, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, counters[key].row(key, key))
	}
	return gin.H{"key": key, "title": title, "definition": definition, "rows": rows}
}

func statisticsHeatGroup(counters map[string]statisticsCounter) gin.H {
	rows := make([]gin.H, 0, 8)
	for _, market := range []struct{ key, label string }{{"ah", "亚盘热度"}, {"ou", "大小球热度"}} {
		for _, threshold := range []int{60, 70, 80, 90} {
			key := fmt.Sprintf("%s-%d", market.key, threshold)
			rows = append(rows, counters[key].row(key, fmt.Sprintf("%s ≥ %d%%", market.label, threshold)))
		}
	}
	return gin.H{"key": "heat", "title": "4. 投注比例热度正确率", "definition": "分别统计亚盘/大小球热度达到阈值的场次；阈值为累计口径（≥）。", "rows": rows}
}

func loadStatisticsRows(table string, ids []string) map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}
	if len(ids) == 0 {
		return result
	}
	var rows []map[string]interface{}
	if database.DB.Table(table).Where("match_id IN ?", ids).Find(&rows).Error != nil {
		return result
	}
	for _, row := range rows {
		if id := statisticsText(statisticsValue(row, "match_id", "matchId")); id != "" {
			result[id] = row
		}
	}
	return result
}

func parseStatisticsMatch(row map[string]interface{}) statisticsMatch {
	status := statisticsText(statisticsValue(row, "status"))
	display := statisticsText(statisticsValue(row, "display_state", "displayState"))
	return statisticsMatch{ID: statisticsText(statisticsValue(row, "match_id", "matchId")), Date: statisticsDate(statisticsValue(row, "date", "match_time", "matchTime")), Home: statisticsText(statisticsValue(row, "home")), Guest: statisticsText(statisticsValue(row, "guest")), HomeScore: int(statisticsNumber(statisticsValue(row, "home_score", "homeScore"))), GuestScore: int(statisticsNumber(statisticsValue(row, "guest_score", "guestScore"))), Settled: strings.Contains(display, "完") || strings.Contains(status, "完") || strings.EqualFold(status, "finished") || statisticsNumber(statisticsValue(row, "status", "match_state", "matchState")) >= 4}
}

func statisticsDateRange(start, end string) (string, string, error) {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	for _, value := range []string{start, end} {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return "", "", err
			}
		}
	}
	if start != "" && end != "" && start > end {
		return "", "", fmt.Errorf("invalid range")
	}
	return start, end, nil
}

func statisticsHistory(row map[string]interface{}) (against, home, guest []statisticsHistoryMatch) {
	against = statisticsHistoryList(statisticsValue(row, "against_list", "againstList"))
	home = statisticsHistoryList(statisticsValue(row, "recent_home_list", "recentHomeList"))
	guest = statisticsHistoryList(statisticsValue(row, "recent_guest_list", "recentGuestList"))
	if len(against) > 0 || len(home) > 0 || len(guest) > 0 {
		return
	}
	payload, _ := statisticsJSON(statisticsValue(row, "league_stat", "leagueStat")).(map[string]interface{})
	if payload == nil {
		return
	}
	if item, ok := payload["against"].(map[string]interface{}); ok {
		against = statisticsHistoryList(item["list"])
	}
	if recent, ok := payload["recent"].(map[string]interface{}); ok {
		if item, ok := recent["home"].(map[string]interface{}); ok {
			home = statisticsHistoryList(item["list"])
		}
		if item, ok := recent["guest"].(map[string]interface{}); ok {
			guest = statisticsHistoryList(item["list"])
		}
	}
	return
}

func statisticsHistoryList(value interface{}) []statisticsHistoryMatch {
	items, _ := statisticsJSON(value).([]interface{})
	result := make([]statisticsHistoryMatch, 0, len(items))
	for _, value := range items {
		row, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		scores := statisticsNumbers(statisticsValue(row, "goal", "score"))
		if len(scores) < 2 {
			continue
		}
		result = append(result, statisticsHistoryMatch{Date: statisticsDate(statisticsValue(row, "matchTime", "match_time", "date")), Home: statisticsText(statisticsValue(row, "home")), Guest: statisticsText(statisticsValue(row, "guest")), HomeScore: int(scores[0]), GuestScore: int(scores[1])})
	}
	return result
}

func statisticsHeadToHead(match statisticsMatch, rows []statisticsHistoryMatch) (float64, float64, bool) {
	matchTime, err := time.Parse("2006-01-02", match.Date)
	if err != nil {
		return 0, 0, false
	}
	cutoff := matchTime.AddDate(-3, 0, 0)
	diffs, totals := []float64{}, []float64{}
	for _, row := range rows {
		date, err := time.Parse("2006-01-02", row.Date)
		if err != nil || date.Before(cutoff) || !date.Before(matchTime) {
			continue
		}
		diff := float64(row.HomeScore - row.GuestScore)
		if row.Home == match.Home && row.Guest == match.Guest {
		} else if row.Home == match.Guest && row.Guest == match.Home {
			diff = -diff
		} else {
			continue
		}
		diffs = append(diffs, diff)
		totals = append(totals, float64(row.HomeScore+row.GuestScore))
	}
	if len(diffs) == 0 {
		return 0, 0, false
	}
	return statisticsSliceMean(diffs), statisticsSliceMean(totals), true
}

func statisticsRecentForm(rows []statisticsHistoryMatch, team string) statisticsTeamForm {
	form := statisticsTeamForm{}
	for _, row := range rows {
		if form.Matches >= 5 {
			break
		}
		if row.Home == team {
			form.For += float64(row.HomeScore)
			form.Against += float64(row.GuestScore)
		} else if row.Guest == team {
			form.For += float64(row.GuestScore)
			form.Against += float64(row.HomeScore)
		} else {
			continue
		}
		form.Matches++
	}
	return form
}
func statisticsRecentDifference(home, guest statisticsTeamForm) (float64, bool) {
	if home.Matches == 0 || guest.Matches == 0 {
		return 0, false
	}
	return (home.For-home.Against)/home.Matches - (guest.For-guest.Against)/guest.Matches, true
}
func statisticsRecentGoals(homeRows, guestRows []statisticsHistoryMatch) (float64, bool) {
	totals := []float64{}
	for _, rows := range [][]statisticsHistoryMatch{homeRows, guestRows} {
		for index, row := range rows {
			if index >= 5 {
				break
			}
			totals = append(totals, float64(row.HomeScore+row.GuestScore))
		}
	}
	if len(totals) == 0 {
		return 0, false
	}
	return statisticsSliceMean(totals), true
}
func statisticsAttackDefenseGoals(home, guest statisticsTeamForm) (float64, bool) {
	if home.Matches == 0 || guest.Matches == 0 {
		return 0, false
	}
	return ((home.For / home.Matches) + (guest.Against / guest.Matches) + (guest.For / guest.Matches) + (home.Against / home.Matches)) / 2, true
}

func statisticsPankouLine(row map[string]interface{}, preferred, rowsKey string) (float64, bool) {
	if item, ok := statisticsJSON(statisticsValue(row, preferred)).(map[string]interface{}); ok {
		if line, ok := statisticsLine(statisticsText(statisticsValue(item, "pankou", "firstPankou", "first_pankou"))); ok {
			return line, true
		}
	}
	items, _ := statisticsJSON(statisticsValue(row, rowsKey)).([]interface{})
	for _, value := range items {
		item, ok := value.(map[string]interface{})
		if !ok || int(statisticsNumber(statisticsValue(item, "companyId", "company_id"))) != 8 {
			continue
		}
		if line, ok := statisticsLine(statisticsText(statisticsValue(item, "pankou", "firstPankou", "first_pankou"))); ok {
			return line, true
		}
	}
	for _, value := range items {
		if item, ok := value.(map[string]interface{}); ok {
			if line, ok := statisticsLine(statisticsText(statisticsValue(item, "pankou", "firstPankou", "first_pankou"))); ok {
				return line, true
			}
		}
	}
	return 0, false
}

func statisticsLine(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number, true
	}
	negative := strings.Contains(value, "受")
	value = strings.ReplaceAll(value, "受", "")
	mapping := map[string]float64{"平手": 0, "平": 0, "平/半": .25, "半": .5, "半球": .5, "半/一": .75, "一球": 1, "一/球半": 1.25, "一球/球半": 1.25, "球半": 1.5, "一球半": 1.5, "球半/两球": 1.75, "两球": 2, "两/两半": 2.25, "两球/两球半": 2.25, "两球半": 2.5, "两半/三": 2.75, "两球半/三球": 2.75, "三球": 3, "三/三半": 3.25, "三球半": 3.5}
	line, ok := mapping[value]
	if !ok {
		return 0, false
	}
	if negative {
		line = -line
	}
	return line, true
}

func statisticsAsianCorrect(match statisticsMatch, line float64) (bool, bool) {
	result := float64(match.HomeScore-match.GuestScore) - line
	if math.Abs(result) < .001 {
		return false, false
	}
	return result > 0, true
}
func statisticsOverCorrect(match statisticsMatch, line float64) (bool, bool) {
	over, valid := statisticsOverOutcome(match, line)
	return over, valid
}
func statisticsOverOutcome(match statisticsMatch, line float64) (bool, bool) {
	result := float64(match.HomeScore+match.GuestScore) - line
	if math.Abs(result) < .001 {
		return false, false
	}
	return result > 0, true
}
func statisticsActualOutcome(match statisticsMatch) string {
	if match.HomeScore > match.GuestScore {
		return "home"
	}
	if match.HomeScore < match.GuestScore {
		return "away"
	}
	return "draw"
}
func statisticsOutcomeFromValue(value, tolerance float64) (string, bool) {
	if math.Abs(value) <= tolerance {
		return "draw", true
	}
	if value > 0 {
		return "home", true
	}
	return "away", true
}
func statisticsAddOutcome(metrics *map[string]statisticsCounter, key string, projection float64, match statisticsMatch) {
	prediction, _ := statisticsOutcomeFromValue(projection, .12)
	counter := (*metrics)[key]
	counter.add(prediction == statisticsActualOutcome(match))
	(*metrics)[key] = counter
}
func statisticsAddGoals(metrics *map[string]statisticsCounter, key string, projection, line float64, match statisticsMatch) {
	if math.Abs(projection-line) < .001 {
		return
	}
	actual, valid := statisticsOverOutcome(match, line)
	if !valid {
		return
	}
	counter := (*metrics)[key]
	counter.add((projection > line) == actual)
	(*metrics)[key] = counter
}

func statisticsProbabilities(row map[string]interface{}) []float64 {
	avg := statisticsOdds(statisticsValue(row, "avg_odds", "avgOdds"))
	if len(avg) < 3 {
		avg = statisticsAverageOdds(statisticsOddsRows(row))
	}
	if len(avg) < 3 || avg[0] <= 0 || avg[1] <= 0 || avg[2] <= 0 {
		return nil
	}
	total := 1/avg[0] + 1/avg[1] + 1/avg[2]
	return []float64{100 / avg[0] / total, 100 / avg[1] / total, 100 / avg[2] / total}
}
func statisticsAsianHeat(home, away, line float64) float64 {
	base := 50.0
	if home+away > 0 {
		base = home / (home + away) * 100
	}
	balance := 50.0
	if line > 0 {
		balance = 55
	} else if line < 0 {
		balance = 45
	}
	return statisticsClamp(balance+(base-50)*.45-line*8, 0, 100)
}

func statisticsKellySportteryChoices(row map[string]interface{}) map[string]bool {
	avg := statisticsOdds(statisticsValue(row, "avg_odds", "avgOdds"))
	oddsRows := statisticsOddsRows(row)
	if len(avg) < 3 {
		avg = statisticsAverageOdds(oddsRows)
	}
	if len(avg) < 3 {
		return nil
	}
	source := statisticsOdds(statisticsValue(row, "pinnacle"))
	if len(source) < 3 {
		source = statisticsFindOdds(oddsRows, "16", "")
	}
	if len(source) < 3 {
		source = statisticsOdds(statisticsValue(row, "bet365"))
	}
	if len(source) < 3 {
		return nil
	}
	kelly := map[string]bool{}
	labels := []string{"home", "draw", "away"}
	sourceReturn, avgReturn := statisticsReturn(source), statisticsReturn(avg)
	for i := 0; i < 3; i++ {
		if source[i]/avg[i]*avgReturn < sourceReturn {
			kelly[labels[i]] = true
		}
	}
	william := statisticsOdds(statisticsValue(row, "william"))
	if len(william) < 3 {
		william = statisticsFindOdds(oddsRows, "115", "威廉")
	}
	if len(william) < 3 {
		return nil
	}
	// Prefer the cached official Sporttery odds when the crawler collected them.
	// The William-vs-average comparison is the same fallback used by the public analysis.
	ticaiReference := avg
	if sporttery := statisticsSportteryOdds(statisticsValue(row, "sporttery_trade", "sportteryTrade")); len(sporttery) == 3 {
		ticaiReference = sporttery
	}
	min := math.MaxFloat64
	diffs := make([]float64, 3)
	for i := range diffs {
		diffs[i] = math.Abs(william[i] - ticaiReference[i])
		if diffs[i] < min {
			min = diffs[i]
		}
	}
	common := map[string]bool{}
	for i, diff := range diffs {
		if diff <= min+.03 && kelly[labels[i]] {
			common[labels[i]] = true
		}
	}
	return common
}

func statisticsSportteryOdds(value interface{}) []float64 {
	payload, ok := statisticsJSON(value).(map[string]interface{})
	if !ok {
		return nil
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		payload = data
	}
	tzbl, ok := payload["tzbl"].(map[string]interface{})
	if !ok {
		return nil
	}
	odds := []float64{statisticsNumber(tzbl["h"]), statisticsNumber(tzbl["d"]), statisticsNumber(tzbl["a"])}
	if odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return nil
	}
	return odds
}
func statisticsOddsRows(row map[string]interface{}) []map[string]interface{} {
	items, _ := statisticsJSON(statisticsValue(row, "data")).([]interface{})
	result := make([]map[string]interface{}, 0, len(items))
	for _, value := range items {
		if item, ok := value.(map[string]interface{}); ok {
			result = append(result, item)
		}
	}
	return result
}
func statisticsFindOdds(rows []map[string]interface{}, id, name string) []float64 {
	for _, row := range rows {
		if statisticsText(statisticsValue(row, "companyId", "company_id")) == id || (name != "" && strings.Contains(statisticsText(statisticsValue(row, "companyName", "company_name")), name)) {
			return statisticsOdds(row)
		}
	}
	return nil
}
func statisticsOdds(value interface{}) []float64 {
	if row, ok := statisticsJSON(value).(map[string]interface{}); ok {
		return statisticsNumbers(statisticsValue(row, "odds"))
	}
	return nil
}
func statisticsAverageOdds(rows []map[string]interface{}) []float64 {
	sums, counts := [3]float64{}, [3]float64{}
	for _, row := range rows {
		if statisticsText(statisticsValue(row, "companyId", "company_id")) == "" {
			continue
		}
		odds := statisticsOdds(row)
		if len(odds) < 3 {
			continue
		}
		for i := 0; i < 3; i++ {
			if odds[i] > 0 {
				sums[i] += odds[i]
				counts[i]++
			}
		}
	}
	for _, count := range counts {
		if count == 0 {
			return nil
		}
	}
	return []float64{sums[0] / counts[0], sums[1] / counts[1], sums[2] / counts[2]}
}
func statisticsReturn(odds []float64) float64 {
	if len(odds) < 3 || odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return 0
	}
	return 1 / (1/odds[0] + 1/odds[1] + 1/odds[2])
}

func statisticsValue(row map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return value
		}
		for actual, value := range row {
			if strings.EqualFold(actual, key) {
				return value
			}
		}
	}
	return nil
}
func statisticsJSON(value interface{}) interface{} {
	switch typed := value.(type) {
	case []byte:
		var out interface{}
		if json.Unmarshal(typed, &out) == nil {
			return out
		}
	case string:
		var out interface{}
		if json.Unmarshal([]byte(typed), &out) == nil {
			return out
		}
	default:
		return value
	}
	return nil
}
func statisticsText(value interface{}) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.Format("2006-01-02")
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
func statisticsNumber(value interface{}) float64 {
	if value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case float32:
		return float64(typed)
	case []byte:
		value = string(typed)
	}
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
	return parsed
}
func statisticsNumbers(value interface{}) []float64 {
	value = statisticsJSON(value)
	switch typed := value.(type) {
	case []interface{}:
		result := make([]float64, 0, len(typed))
		for _, item := range typed {
			result = append(result, statisticsNumber(item))
		}
		return result
	case []string:
		result := make([]float64, 0, len(typed))
		for _, item := range typed {
			result = append(result, statisticsNumber(item))
		}
		return result
	case string:
		fields := strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == ':' || r == '/' || r == '-' || r == ' ' })
		result := []float64{}
		for _, field := range fields {
			if field != "" {
				result = append(result, statisticsNumber(field))
			}
		}
		return result
	}
	return nil
}
func statisticsDate(value interface{}) string {
	text := statisticsText(value)
	if len(text) >= 10 {
		return text[:10]
	}
	return text
}
func statisticsLineKey(value float64) string { return strconv.FormatFloat(value, 'f', 2, 64) }
func statisticsSliceMean(values []float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
func statisticsMean(first float64, firstOK bool, second float64, secondOK bool) float64 {
	value, _ := statisticsAverage(first, firstOK, second, secondOK)
	return value
}
func statisticsAverage(values ...interface{}) (float64, bool) {
	sum, count := 0.0, 0
	for index := 0; index+1 < len(values); index += 2 {
		value, ok := values[index].(float64)
		enabled, enabledOK := values[index+1].(bool)
		if ok && enabledOK && enabled {
			sum += value
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}
func statisticsClamp(value, min, max float64) float64 { return math.Max(min, math.Min(max, value)) }
func statisticsBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
