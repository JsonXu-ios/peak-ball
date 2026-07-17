// Package handlers: statistics_warnings.go settles the H5 warning rows (警示)
// as dimension 15. Every warning type carries an implied direction; each bucket
// answers "when this warning fired, was its implication right, and what would
// betting it have returned at real odds".
package handlers

import (
	"math"

	"github.com/gin-gonic/gin"
)

// statisticsAsianWater returns decimal odds (1+water) for home/away on the
// bet365 Asian handicap.
func statisticsAsianWater(pankouRow map[string]interface{}) (float64, float64, bool) {
	read := func(item map[string]interface{}) (float64, float64, bool) {
		arr := statisticsNumbers(statisticsValue(item, "odds"))
		if len(arr) < 2 || arr[0] <= 0 || arr[1] <= 0 {
			return 0, 0, false
		}
		return 1 + arr[0], 1 + arr[1], true
	}
	if item, ok := statisticsJSON(statisticsValue(pankouRow, "bet365_asia")).(map[string]interface{}); ok {
		if home, away, ok := read(item); ok {
			return home, away, true
		}
	}
	for _, value := range statisticsPankouRows(pankouRow, "asia_data") {
		if item, ok := value.(map[string]interface{}); ok && int(statisticsNumber(statisticsValue(item, "companyId", "company_id"))) == 8 {
			if home, away, ok := read(item); ok {
				return home, away, true
			}
		}
	}
	return 0, 0, false
}

// statisticsBookmakerLossBoth returns the direction that is the WORST (most
// losing) for the bookmaker on both 胜平负 and 让球 trades, when they agree and
// both are actual losses.
func statisticsBookmakerLossBoth(value interface{}) (string, bool) {
	payload, ok := statisticsJSON(value).(map[string]interface{})
	if !ok {
		return "", false
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		payload = data
	}
	spf, ok1 := statisticsJSON(payload["jyykSpf"]).(map[string]interface{})
	rq, ok2 := statisticsJSON(payload["jyykRqspf"]).(map[string]interface{})
	if !ok1 || !ok2 {
		return "", false
	}
	worst := func(row map[string]interface{}) (string, float64) {
		best, bestValue := "", math.Inf(1)
		for _, item := range []struct{ key, dir string }{{"hy", "home"}, {"dy", "draw"}, {"ay", "away"}} {
			raw := statisticsValue(row, item.key)
			if raw == nil {
				continue
			}
			if v := statisticsNumber(raw); v < bestValue {
				best, bestValue = item.dir, v
			}
		}
		return best, bestValue
	}
	spfDir, spfValue := worst(spf)
	rqDir, rqValue := worst(rq)
	if spfDir == "" || spfDir != rqDir || spfValue >= 0 || rqValue >= 0 {
		return "", false
	}
	return spfDir, true
}

// warningHandicapSignal ports the H5 让球修正 trigger: expected line
// (历史45%+近期55%) vs the current Asian line. Returns implied outcome + label.
func warningHandicapSignal(historyDiff, recentDiff, currentLine float64) (string, string) {
	expected := historyDiff*0.45 + recentDiff*0.55
	direction := func(value float64) string {
		if math.Abs(value) < 0.01 {
			return "level"
		}
		if value > 0 {
			return "home"
		}
		return "guest"
	}
	currentDirection := direction(currentLine)
	expectedDirection := direction(expected)
	currentAbs := math.Abs(currentLine)
	expectedAbs := math.Abs(expected)
	outcomeOf := func(dir string) string {
		if dir == "home" {
			return "home"
		}
		if dir == "guest" {
			return "away"
		}
		return ""
	}
	if currentDirection != "level" && expectedDirection != "level" && currentDirection != expectedDirection {
		return outcomeOf(expectedDirection), "方向反转"
	}
	if currentDirection != "level" && currentAbs-expectedAbs >= 0.5 {
		if currentDirection == "home" {
			return "away", "盘口偏深防强方"
		}
		return "home", "盘口偏深防强方"
	}
	if expectedDirection != "level" && expectedAbs-currentAbs >= 0.5 {
		return outcomeOf(expectedDirection), "盘口偏浅防隐藏"
	}
	if math.Min(math.Abs(historyDiff-currentLine), math.Abs(recentDiff-currentLine)) > 0.75 {
		return outcomeOf(expectedDirection), "盘口异常偏离"
	}
	return "", ""
}

// warningGoalBalanceSignal ports the H5 大小球回归 trigger (2.5 均衡).
func warningGoalBalanceSignal(history float64, hasHistory bool, recent float64, hasRecent bool, openingLine float64, hasOpening bool) string {
	combined, hasCombined := statisticsAverage(history, hasHistory, recent, hasRecent)
	values := []struct {
		value  float64
		weight float64
		has    bool
	}{
		{history, 0.2, hasHistory}, {recent, 0.35, hasRecent}, {combined, 0.3, hasCombined}, {openingLine, 0.15, hasOpening},
	}
	sum, weightSum := 0.0, 0.0
	highCount, lowCount := 0, 0
	for _, item := range values {
		if !item.has {
			continue
		}
		sum += item.value * item.weight
		weightSum += item.weight
		if item.value >= 2.85 {
			highCount++
		}
		if item.value <= 2.15 {
			lowCount++
		}
	}
	if weightSum <= 0 {
		return ""
	}
	balance := sum / weightSum
	if balance >= 2.85 || highCount >= 2 {
		return "under"
	}
	if balance <= 2.15 || lowCount >= 2 {
		return "over"
	}
	return ""
}

// buildWarningSignals settles every warning family and returns dimension 15.
func buildWarningSignals(matches []statisticsMatch, histories, pankous, odds map[string]map[string]interface{}) gin.H {
	buckets := map[string]*pickTally{}
	order := []string{
		"让球热度过热·反过热方赢盘",
		"大小球热度过热·反过热方向",
		"交易盈亏同向·舒服方不打出",
		"庄家同向亏损(负)·客胜打出",
		"凯体反差·跟凯体共识",
		"让球修正·跟期望方赢盘",
		"大小球回归·跟回归方向",
	}
	add := func(key string, detail statisticsDetail, oddsValue float64) {
		tally := buckets[key]
		if tally == nil {
			tally = &pickTally{}
			buckets[key] = tally
		}
		tally.add(detail, oddsValue)
	}

	for _, match := range matches {
		historyRow := histories[match.ID]
		pankouRow := pankous[match.ID]
		oddsRow := odds[match.ID]
		probabilities := statisticsProbabilities(oddsRow)
		ahFirst, ahLine, hasAH := statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
		homeWater, awayWater, hasAsianWater := statisticsAsianWater(pankouRow)
		overWater, underWater, hasDxqWater := pickDxqWater(pankouRow)
		avgOdds := pickAvgOdds(oddsRow)
		actual := statisticsActualOutcome(match)

		// 15a 让球热度过热：过热方(>65)按赢盘结算，命中=过热方没赢盘（警示正确）。
		if hasAH && len(probabilities) == 3 {
			if homeCovered, valid := statisticsAsianCorrect(match, ahLine); valid {
				homeHeat := statisticsAsianHeat(probabilities[0], probabilities[2], ahFirst, ahLine)
				hotHome := homeHeat > 65
				hotGuest := (100 - homeHeat) > 65
				if hotHome || hotGuest {
					detail := statisticsBaseDetail(match)
					heat := homeHeat
					fadeOdds := awayWater
					if hotGuest {
						heat = 100 - homeHeat
						fadeOdds = homeWater
					}
					detail.Value = statisticsRound2(heat)
					if hotHome {
						detail.Pick = "反主队(主过热)"
						detail.Hit = !homeCovered
					} else {
						detail.Pick = "反客队(客过热)"
						detail.Hit = homeCovered
					}
					detail.Result = statisticsCoverLabel(homeCovered)
					if !hasAsianWater {
						fadeOdds = 0
					}
					add("让球热度过热·反过热方赢盘", detail, fadeOdds)
				}
			}
		}

		// 15b 大小球热度过热(压力>65)：反过热方向。
		if qiuDirection, qiuLine, hasQiu := pickQiuPrediction(historyRow, pankouRow, match); hasQiu && qiuDirection != "" {
			strength := pickQiuStrength(historyRow, qiuLine, match)
			if strength > 30 { // overPressure>65 或 underPressure>65
				if over, valid := statisticsOverOutcome(match, qiuLine); valid {
					detail := statisticsBaseDetail(match)
					detail.Value = statisticsRound2(qiuLine)
					fadeOver := qiuDirection == "under" // 小球过热→反着买大
					if fadeOver {
						detail.Pick = "反小过热·买大"
						detail.Hit = over
					} else {
						detail.Pick = "反大过热·买小"
						detail.Hit = !over
					}
					detail.Result = statisticsOverLabel(over)
					fadeOdds := 0.0
					if hasDxqWater {
						if fadeOver {
							fadeOdds = overWater
						} else {
							fadeOdds = underWater
						}
					}
					add("大小球热度过热·反过热方向", detail, fadeOdds)
				}
			}
		}

		// 15c 交易盈亏同向：舒服方向不打出=警示正确（无单一赔率，不计ROI）。
		if comfort, ok := statisticsBookmakerComfort(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade")); ok {
			detail := statisticsBaseDetail(match)
			detail.Pick = "防" + statisticsOutcomeLabel(comfort)
			detail.Result = statisticsOutcomeLabel(actual)
			detail.Hit = actual != comfort
			add("交易盈亏同向·舒服方不打出", detail, 0)
		}

		// 15d 庄家同向亏损(负)：胜平负+让球最大亏损项都是客胜 → 押客胜。
		if lossDir, ok := statisticsBookmakerLossBoth(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade")); ok && lossDir == "away" {
			detail := statisticsBaseDetail(match)
			detail.Pick = "客胜"
			detail.Result = statisticsOutcomeLabel(actual)
			detail.Hit = actual == "away"
			oddsValue := 0.0
			if avgOdds != nil {
				oddsValue = avgOdds[2]
			}
			add("庄家同向亏损(负)·客胜打出", detail, oddsValue)
		}

		// 15e 凯体反差：凯利∩体彩共识与主推不同 → 跟共识。
		if choices := statisticsKellySportteryChoices(oddsRow); len(choices) > 0 {
			base := pickBasePrediction(oddsRow)
			if !choices[base] {
				detail := statisticsBaseDetail(match)
				detail.Pick = statisticsChoiceLabel(choices)
				detail.Result = statisticsOutcomeLabel(actual)
				detail.Hit = choices[actual]
				oddsValue := 0.0
				if len(choices) == 1 && avgOdds != nil {
					for _, key := range []string{"home", "draw", "away"} {
						if choices[key] {
							oddsValue = avgOdds[map[string]int{"home": 0, "draw": 1, "away": 2}[key]]
						}
					}
				}
				add("凯体反差·跟凯体共识", detail, oddsValue)
			}
		}

		// 15g 让球修正：期望让球 vs 即时盘背离 → 跟期望方赢盘。
		if hasAH {
			against, homeRecent, guestRecent := statisticsHistory(historyRow)
			historyDiff, _, hasHistory := statisticsHeadToHead(match, against)
			recentDiff, hasRecent := statisticsRecentDifference(
				statisticsRecentForm(homeRecent, match.Home), statisticsRecentForm(guestRecent, match.Guest))
			if hasHistory && hasRecent {
				if implied, label := warningHandicapSignal(historyDiff, recentDiff, ahLine); implied != "" {
					if homeCovered, valid := statisticsAsianCorrect(match, ahLine); valid {
						detail := statisticsBaseDetail(match)
						detail.Pick = label + "→" + statisticsCoverLabel(implied == "home")
						detail.Result = statisticsCoverLabel(homeCovered)
						detail.Hit = (implied == "home") == homeCovered
						oddsValue := 0.0
						if hasAsianWater {
							if implied == "home" {
								oddsValue = homeWater
							} else {
								oddsValue = awayWater
							}
						}
						add("让球修正·跟期望方赢盘", detail, oddsValue)
					}
				}
			}
		}

		// 15h 大小球回归(2.5均衡)：回归方向按当前盘结算。
		{
			against, homeRecent, guestRecent := statisticsHistory(historyRow)
			_, historyGoals, hasHistory := statisticsHeadToHead(match, against)
			recentGoals, hasRecent := statisticsRecentGoals(homeRecent, guestRecent)
			ouFirst, ouLine, hasOU := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
			if hasOU {
				signal := warningGoalBalanceSignal(historyGoals, hasHistory, recentGoals, hasRecent, ouFirst, true)
				if signal != "" {
					if over, valid := statisticsOverOutcome(match, ouLine); valid {
						detail := statisticsBaseDetail(match)
						detail.Value = statisticsRound2(ouLine)
						detail.Pick = "回归" + statisticsOverLabel(signal == "over")
						detail.Result = statisticsOverLabel(over)
						detail.Hit = (signal == "over") == over
						oddsValue := 0.0
						if hasDxqWater {
							if signal == "over" {
								oddsValue = overWater
							} else {
								oddsValue = underWater
							}
						}
						add("大小球回归·跟回归方向", detail, oddsValue)
					}
				}
			}
		}
	}

	rows := []gin.H{}
	matched, hit := 0, 0
	for _, key := range order {
		tally := buckets[key]
		if tally == nil || len(tally.sig.details) == 0 {
			continue
		}
		matched += len(tally.sig.details)
		hit += tally.sig.hit
		rows = append(rows, tally.bucketPayload("warn-"+key, key))
	}
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	return gin.H{
		"key":   "warning_signals",
		"title": "15. 警示信号结算（每类警示暗示方向的胜率与ROI）",
		"definition": "把H5上的各类警示按其暗示方向结算：热度过热=反过热方，交易盈亏同向=舒服方不打出，同向亏损(负)=押客胜，凯体反差=跟共识，让球修正=跟期望方赢盘，大小球回归=跟回归方向。ROI为按该方向真实赔率每场投1单位（无明确单一赔率的不计）。",
		"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": rows,
	}
}
