// Package handlers: statistics_picks.go is dimension 13 — the H5 header
// prediction (胜平负主推 + 球数倾向) settled over every completed match,
// bucketed by signal strength (faithful go_server replication).
package handlers

import (
	"math"
	"sort"

	"github.com/gin-gonic/gin"
)

// ---------- odds extractors (for ROI) ----------

// pickAvgOdds returns the average European odds triple for a match.
func pickAvgOdds(oddsRow map[string]interface{}) []float64 {
	avg := statisticsOdds(statisticsValue(oddsRow, "avg_odds", "avgOdds"))
	if len(avg) < 3 {
		avg = statisticsAverageOdds(statisticsOddsRows(oddsRow))
	}
	if len(avg) < 3 || avg[0] <= 0 || avg[1] <= 0 || avg[2] <= 0 {
		return nil
	}
	return avg
}

// pickDxqWater returns decimal odds (1+water) for over/under from bet365 dxq.
func pickDxqWater(pankouRow map[string]interface{}) (float64, float64, bool) {
	read := func(item map[string]interface{}) (float64, float64, bool) {
		arr := statisticsNumbers(statisticsValue(item, "odds"))
		if len(arr) < 2 || arr[0] <= 0 || arr[1] <= 0 {
			return 0, 0, false
		}
		return 1 + arr[0], 1 + arr[1], true
	}
	if item, ok := statisticsJSON(statisticsValue(pankouRow, "bet365_dxq")).(map[string]interface{}); ok {
		if over, under, ok := read(item); ok {
			return over, under, true
		}
	}
	for _, value := range statisticsPankouRows(pankouRow, "dxq_data") {
		if item, ok := value.(map[string]interface{}); ok && int(statisticsNumber(statisticsValue(item, "companyId", "company_id"))) == 8 {
			if over, under, ok := read(item); ok {
				return over, under, true
			}
		}
	}
	return 0, 0, false
}

// ---------- dim13: the H5 header prediction (faithful go_server replication) ----------

// pickBasePrediction replicates go_server buildAnalysis: max of implied
// probabilities (fallback 33/34/33 → 平局).
func pickBasePrediction(oddsRow map[string]interface{}) string {
	probabilities := statisticsProbabilities(oddsRow)
	if len(probabilities) != 3 {
		return "draw"
	}
	best, bestValue := "home", probabilities[0]
	if probabilities[1] > bestValue {
		best, bestValue = "draw", probabilities[1]
	}
	if probabilities[2] > bestValue {
		best = "away"
	}
	return best
}

// pickQiuPrediction replicates go_server 球数倾向: over/under pressure from
// recent average total goals vs the current O/U line. Returns "over"/"under"/""
// ("" = 盘口球, no direction) plus the line used.
func pickQiuPrediction(historyRow, pankouRow map[string]interface{}, match statisticsMatch) (string, float64, bool) {
	_, homeRecent, guestRecent := statisticsHistory(historyRow)
	homeForm := statisticsRecentForm(homeRecent, match.Home)
	guestForm := statisticsRecentForm(guestRecent, match.Guest)
	totalMatches := homeForm.Matches + guestForm.Matches
	recentTotalGoals := 0.0
	if totalMatches > 0 {
		recentTotalGoals = (homeForm.For + homeForm.Against + guestForm.For + guestForm.Against) / totalMatches
	}
	line, hasLine := statisticsPankouLine(pankouRow, "bet365_dxq", "dxq_data")
	if !hasLine || line == 0 {
		// go_server falls back to max(recent, history) when the line is missing.
		against, _, _ := statisticsHistory(historyRow)
		_, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		fallback := recentTotalGoals
		if hasHistory && historyGoals > fallback {
			fallback = historyGoals
		}
		line = statisticsRound2(fallback)
		if line <= 0 {
			return "", 0, false
		}
	}
	overPressure := statisticsClamp(50+(recentTotalGoals-line)*18, 0, 100)
	underPressure := 100 - overPressure
	if math.Abs(overPressure-underPressure) < 5 {
		return "", line, true
	}
	if overPressure > underPressure {
		return "over", line, true
	}
	return "under", line, true
}

// pickQiuStrength returns |大球压力-小球压力| for banding dim 13b.
func pickQiuStrength(historyRow map[string]interface{}, line float64, match statisticsMatch) float64 {
	_, homeRecent, guestRecent := statisticsHistory(historyRow)
	homeForm := statisticsRecentForm(homeRecent, match.Home)
	guestForm := statisticsRecentForm(guestRecent, match.Guest)
	totalMatches := homeForm.Matches + guestForm.Matches
	recentTotalGoals := 0.0
	if totalMatches > 0 {
		recentTotalGoals = (homeForm.For + homeForm.Against + guestForm.For + guestForm.Against) / totalMatches
	}
	overPressure := statisticsClamp(50+(recentTotalGoals-line)*18, 0, 100)
	return math.Abs(overPressure - (100 - overPressure))
}

func pickBaseProbBand(probability float64) string {
	switch {
	case probability >= 65:
		return "主推概率≥65%"
	case probability >= 55:
		return "主推概率55-65%"
	case probability >= 45:
		return "主推概率45-55%"
	default:
		return "主推概率<45%"
	}
}

var pickBaseProbBandOrder = []string{"主推概率≥65%", "主推概率55-65%", "主推概率45-55%", "主推概率<45%"}

func pickQiuStrengthBand(strength float64) string {
	switch {
	case strength >= 30:
		return "压力差≥30"
	case strength >= 15:
		return "压力差15-30"
	default:
		return "压力差5-15"
	}
}

var pickQiuStrengthBandOrder = []string{"压力差≥30", "压力差15-30", "压力差5-15"}

// ---------- tallies (hit + ROI) ----------

type pickTally struct {
	sig   statisticsSignal
	stake float64
	ret   float64
	odds  int
}

func (t *pickTally) add(detail statisticsDetail, oddsValue float64) {
	t.sig.add(detail)
	if oddsValue > 0 {
		t.stake++
		t.odds++
		if detail.Hit {
			t.ret += oddsValue
		}
	}
}

func (t *pickTally) payload(key, title, definition string) gin.H {
	payload := t.sig.payload(key, title, definition)
	if t.stake > 0 {
		payload["roi"] = statisticsRound2(t.ret / t.stake * 100)
		payload["roiSample"] = t.odds
	}
	return payload
}

func (t *pickTally) bucketPayload(key, title string) gin.H {
	return t.payload(key, title, "")
}

// ---------- main builder ----------

// buildPickSignals settles dim 13a/13b (前端主推 + 球数倾向) over every
// completed match.
func buildPickSignals(matches []statisticsMatch, histories, pankous, odds map[string]map[string]interface{}) []gin.H {
	base13 := &pickTally{}
	base13Bands := map[string]*pickTally{}
	qiu13 := &pickTally{}
	qiu13Bands := map[string]*pickTally{}

	bucketAdd := func(buckets map[string]*pickTally, key string, detail statisticsDetail, oddsValue float64) {
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
		avgOdds := pickAvgOdds(oddsRow)
		overWater, underWater, hasWater := pickDxqWater(pankouRow)

		// 13a settles on every completed match, bucketed by signal strength so
		// "为什么胜率高" can be answered by band.
		basePrediction := pickBasePrediction(oddsRow)
		actualOutcome := statisticsActualOutcome(match)
		detail13 := statisticsBaseDetail(match)
		detail13.Pick = statisticsOutcomeLabel(basePrediction)
		detail13.Result = statisticsOutcomeLabel(actualOutcome)
		detail13.Hit = basePrediction == actualOutcome
		baseOddsValue := 0.0
		baseProbability := 0.0
		if probabilities := statisticsProbabilities(oddsRow); len(probabilities) == 3 {
			index := map[string]int{"home": 0, "draw": 1, "away": 2}[basePrediction]
			baseProbability = probabilities[index]
			if avgOdds != nil {
				baseOddsValue = avgOdds[index]
			}
			detail13.Value = statisticsRound2(baseProbability)
		}
		base13.add(detail13, baseOddsValue)
		bucketAdd(base13Bands, pickBaseProbBand(baseProbability), detail13, baseOddsValue)

		qiuDirection, qiuLine, hasQiu := pickQiuPrediction(historyRow, pankouRow, match)
		if hasQiu && qiuDirection != "" {
			if over, valid := statisticsOverOutcome(match, qiuLine); valid {
				detailQ := statisticsBaseDetail(match)
				detailQ.Value = statisticsRound2(qiuLine)
				detailQ.Line = statisticsFormatLine(qiuLine)
				detailQ.Pick = statisticsOverLabel(qiuDirection == "over")
				detailQ.Result = statisticsOverLabel(over)
				detailQ.Hit = (qiuDirection == "over") == over
				qiuOddsValue := 0.0
				if hasWater {
					if qiuDirection == "over" {
						qiuOddsValue = overWater
					} else {
						qiuOddsValue = underWater
					}
				}
				strength := pickQiuStrength(historyRow, qiuLine, match)
				qiu13.add(detailQ, qiuOddsValue)
				bucketAdd(qiu13Bands, pickQiuStrengthBand(strength), detailQ, qiuOddsValue)
			}
		}
	}

	base13Payload := base13.payload("base_spf", "7. 前端主推·胜平负（按主推概率分档）", "平均欧赔隐含概率最大方向（H5卡片中央的主推）；命中=该方向即赛果；ROI=按平均欧赔每场投1单位。分档回答“胜率高在哪”。")
	base13Payload["buckets"] = pickBucketRows(base13Bands, pickBaseProbBandOrder, "base-spf")
	qiu13Payload := qiu13.payload("base_qiu", "15. 前端球数倾向·大小球（按压力强度分档）", "近期场均总进球对当前盘口的压力方向；盘口球(压力差<5)不计入；ROI按bet365水位。")
	qiu13Payload["buckets"] = pickBucketRows(qiu13Bands, pickQiuStrengthBandOrder, "base-qiu")

	return []gin.H{base13Payload, qiu13Payload}
}

func pickBucketRows(buckets map[string]*pickTally, order []string, prefix string) []gin.H {
	rows := []gin.H{}
	for _, key := range order {
		tally := buckets[key]
		if tally == nil || len(tally.sig.details) == 0 {
			continue
		}
		rows = append(rows, tally.bucketPayload(prefix+"-"+key, key))
	}
	// any unexpected keys (shouldn't happen) appended deterministically
	extras := []string{}
	for key := range buckets {
		found := false
		for _, known := range order {
			if key == known {
				found = true
				break
			}
		}
		if !found {
			extras = append(extras, key)
		}
	}
	sort.Strings(extras)
	for _, key := range extras {
		rows = append(rows, buckets[key].bucketPayload(prefix+"-"+key, key))
	}
	return rows
}
