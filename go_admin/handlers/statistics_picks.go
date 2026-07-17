// Package handlers: statistics_picks.go is the owner-pick analytics engine.
// It settles every recorded pick (user_picks) against completed matches and
// produces:
//   - dimension 12: per-market hit rates + REAL-ODDS ROI (欧赔/竞彩让球赔率/大小球水位)
//   - dimension 13: the H5 header prediction (胜平负主推 + 球数倾向) settled
//   - cross signals: my picks 同向/反向 vs the platform dimensions
//   - pickProfile: hexagon radar + per-handicap-bucket red/black distribution
package handlers

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// ---------- loading ----------

func loadUserPicks(ids []string) map[string][]map[string]interface{} {
	result := map[string][]map[string]interface{}{}
	if len(ids) == 0 {
		return result
	}
	var rows []map[string]interface{}
	if statisticsDB().Table("user_picks").Where("match_id IN ?", ids).Find(&rows).Error != nil {
		return result
	}
	for _, row := range rows {
		if id := statisticsText(statisticsValue(row, "match_id", "matchId")); id != "" {
			result[id] = append(result[id], row)
		}
	}
	return result
}

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

// pickRqspfOdds reads 竞彩让球 odds (h/d/a) from sporttery_trade.
func pickRqspfOdds(oddsRow map[string]interface{}) []float64 {
	payload, ok := statisticsJSON(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade")).(map[string]interface{})
	if !ok {
		return nil
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		payload = data
	}
	rq, ok := statisticsJSON(payload["jyykRqspf"]).(map[string]interface{})
	if !ok {
		return nil
	}
	odds := []float64{statisticsNumber(rq["h"]), statisticsNumber(rq["d"]), statisticsNumber(rq["a"])}
	if odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return nil
	}
	return odds
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

// ---------- settle helpers ----------

func pickOutcomeFromText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.Contains(text, "平") {
		return "draw"
	}
	if strings.Contains(text, "客") || strings.Contains(text, "负") {
		return "away"
	}
	if strings.Contains(text, "主") || strings.Contains(text, "胜") {
		return "home"
	}
	return ""
}

// pickRqspfActual settles the handicap 胜平负 (竞彩口径：line 为主队让球数，主让为负).
func pickRqspfActual(match statisticsMatch, line float64) string {
	adjusted := float64(match.HomeScore) + line - float64(match.GuestScore)
	if math.Abs(adjusted) < statisticsPushEpsilon {
		return "draw"
	}
	if adjusted > 0 {
		return "home"
	}
	return "away"
}

func pickRqspfLabel(outcome string) string {
	switch outcome {
	case "home":
		return "让胜"
	case "away":
		return "让负"
	}
	return "让平"
}

func pickNormalizeScore(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "-", ":"), "：", ":"))
	parts := strings.SplitN(text, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[0]) + ":" + strings.TrimSpace(parts[1])
}

func pickScoreHit(pickText string, match statisticsMatch) bool {
	actual := fmt.Sprintf("%d:%d", match.HomeScore, match.GuestScore)
	for _, splitter := range []string{"，", "、", "/", ";", "；"} {
		pickText = strings.ReplaceAll(pickText, splitter, ",")
	}
	for _, candidate := range strings.Split(pickText, ",") {
		if normalized := pickNormalizeScore(candidate); normalized != "" && normalized == actual {
			return true
		}
	}
	return false
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
	sig    statisticsSignal
	stake  float64
	ret    float64
	odds   int
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

func pickFlag(matched, hit int) string {
	if matched < 5 {
		return ""
	}
	accuracy := float64(hit) / float64(matched) * 100
	if accuracy >= 65 {
		return "red"
	}
	if accuracy <= 35 {
		return "black"
	}
	return ""
}

func (t *pickTally) bucketPayload(key, title string) gin.H {
	payload := t.payload(key, title, "")
	payload["flag"] = pickFlag(len(t.sig.details), t.sig.hit)
	return payload
}

// ---------- bucket labels ----------

func pickAsianBucket(line float64, hasLine bool) string {
	if !hasLine {
		return "无亚盘"
	}
	switch {
	case line <= -1:
		return "受让深(≤-1)"
	case line <= -0.5:
		return "受让中(-0.75~-0.5)"
	case line < 0.5:
		return "平/浅(±0.25)"
	case line < 1:
		return "主让中(0.5~0.75)"
	default:
		return "主让深(≥1)"
	}
}

var pickAsianBucketOrder = []string{"受让深(≤-1)", "受让中(-0.75~-0.5)", "平/浅(±0.25)", "主让中(0.5~0.75)", "主让深(≥1)", "无亚盘"}

func pickGoalBucket(line float64, hasLine bool) string {
	if !hasLine {
		return "无盘口"
	}
	switch {
	case line <= 2.25:
		return "低盘(≤2.25)"
	case line < 2.75:
		return "中盘(2.5)"
	default:
		return "高盘(≥2.75)"
	}
}

var pickGoalBucketOrder = []string{"低盘(≤2.25)", "中盘(2.5)", "高盘(≥2.75)", "无盘口"}

// ---------- radar ----------

func pickRadarScore(accuracy, base, good float64, sample int) float64 {
	if sample < 3 {
		return 0
	}
	return statisticsRound2(statisticsClamp((accuracy-base)/(good-base)*100, 0, 100))
}

// ---------- main builder ----------

func buildPickSignals(matches []statisticsMatch, picksByMatch map[string][]map[string]interface{}, histories, pankous, odds map[string]map[string]interface{}) ([]gin.H, gin.H) {
	overview := &pickTally{}
	spf := &pickTally{}
	rqspf := &pickTally{}
	dxq := &pickTally{}
	scorePick := &pickTally{}
	confident := &pickTally{}
	base13 := &pickTally{}
	base13Bands := map[string]*pickTally{}
	qiu13 := &pickTally{}
	qiu13Bands := map[string]*pickTally{}
	dxqPush, rqspfMissingLine := 0, 0

	// cross tallies: my pick 同向/反向 vs a dimension
	cross := map[string]*pickTally{}
	crossAdd := func(key string, detail statisticsDetail, oddsValue float64) {
		tally := cross[key]
		if tally == nil {
			tally = &pickTally{}
			cross[key] = tally
		}
		tally.add(detail, oddsValue)
	}

	asianBuckets := map[string]*pickTally{}
	goalBuckets := map[string]*pickTally{}
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
		rqOdds := pickRqspfOdds(oddsRow)
		overWater, underWater, hasWater := pickDxqWater(pankouRow)
		asianLine, hasAsian := statisticsPankouLine(pankouRow, "bet365_asia", "asia_data")

		// dim13 settles on every completed match (independent of my picks),
		// bucketed by signal strength so "为什么胜率高" can be answered by band.
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

		for _, pick := range picksByMatch[match.ID] {
			market := statisticsText(statisticsValue(pick, "market"))
			pickText := statisticsText(statisticsValue(pick, "pick"))
			lineRaw := statisticsValue(pick, "line")
			line := statisticsNumber(lineRaw)
			hasLine := lineRaw != nil && statisticsText(lineRaw) != ""
			confidence := int(statisticsNumber(statisticsValue(pick, "confidence")))
			detail := statisticsBaseDetail(match)
			detail.Pick = pickText
			if hasLine {
				detail.Value = statisticsRound2(line)
				detail.Pick = fmt.Sprintf("%s(%s)", pickText, statisticsText(lineRaw))
			}

			switch market {
			case "spf":
				chosen := pickOutcomeFromText(pickText)
				if chosen == "" {
					continue
				}
				detail.Result = statisticsOutcomeLabel(actualOutcome)
				detail.Hit = chosen == actualOutcome
				oddsValue := 0.0
				if avgOdds != nil {
					oddsValue = avgOdds[map[string]int{"home": 0, "draw": 1, "away": 2}[chosen]]
				}
				spf.add(detail, oddsValue)
				overview.add(detail, oddsValue)
				if confidence >= 2 {
					confident.add(detail, oddsValue)
				}
				bucketAdd(asianBuckets, pickAsianBucket(asianLine, hasAsian), detail, oddsValue)
				if chosen == basePrediction {
					crossAdd("spf_base_same", detail, oddsValue)
				} else {
					crossAdd("spf_base_diff", detail, oddsValue)
				}
				if comfort, ok := statisticsBookmakerComfort(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade")); ok {
					if chosen == comfort {
						crossAdd("spf_comfort_same", detail, oddsValue)
					} else {
						crossAdd("spf_comfort_diff", detail, oddsValue)
					}
				}
			case "rqspf":
				if !hasLine {
					rqspfMissingLine++
					continue
				}
				chosen := ""
				if strings.Contains(pickText, "胜") {
					chosen = "home"
				} else if strings.Contains(pickText, "平") {
					chosen = "draw"
				} else if strings.Contains(pickText, "负") {
					chosen = "away"
				}
				if chosen == "" {
					continue
				}
				actual := pickRqspfActual(match, line)
				detail.Result = pickRqspfLabel(actual)
				detail.Hit = chosen == actual
				oddsValue := 0.0
				if rqOdds != nil {
					oddsValue = rqOdds[map[string]int{"home": 0, "draw": 1, "away": 2}[chosen]]
				}
				rqspf.add(detail, oddsValue)
				overview.add(detail, oddsValue)
				if confidence >= 2 {
					confident.add(detail, oddsValue)
				}
				bucketAdd(asianBuckets, pickAsianBucket(asianLine, hasAsian), detail, oddsValue)
			case "dxq":
				if !hasLine {
					continue
				}
				over, valid := statisticsOverOutcome(match, line)
				if !valid {
					dxqPush++
					continue
				}
				chosenOver := strings.Contains(pickText, "大")
				if !chosenOver && !strings.Contains(pickText, "小") {
					continue
				}
				detail.Result = statisticsOverLabel(over)
				detail.Hit = chosenOver == over
				oddsValue := 0.0
				if hasWater {
					if chosenOver {
						oddsValue = overWater
					} else {
						oddsValue = underWater
					}
				}
				dxq.add(detail, oddsValue)
				overview.add(detail, oddsValue)
				if confidence >= 2 {
					confident.add(detail, oddsValue)
				}
				bucketAdd(goalBuckets, pickGoalBucket(line, true), detail, oddsValue)
				if hasQiu && qiuDirection != "" {
					if (qiuDirection == "over") == chosenOver {
						crossAdd("dxq_qiu_same", detail, oddsValue)
					} else {
						crossAdd("dxq_qiu_diff", detail, oddsValue)
					}
				}
				// 综合均值方向（历史+近期平均球数 vs 盘口）
				against, homeRecent, guestRecent := statisticsHistory(historyRow)
				_, historyGoals, hasHistory := statisticsHeadToHead(match, against)
				recentGoals, hasRecent := statisticsRecentGoals(homeRecent, guestRecent)
				if composite, ok := statisticsAverage(historyGoals, hasHistory, recentGoals, hasRecent); ok && math.Abs(composite-line) >= statisticsPushEpsilon {
					compositeOver := composite > line
					if compositeOver == chosenOver {
						crossAdd("dxq_composite_same", detail, oddsValue)
					} else {
						crossAdd("dxq_composite_diff", detail, oddsValue)
					}
				}
			case "score":
				detail.Result = fmt.Sprintf("%d:%d", match.HomeScore, match.GuestScore)
				detail.Hit = pickScoreHit(pickText, match)
				scorePick.add(detail, 0)
				overview.add(detail, 0)
				if confidence >= 2 {
					confident.add(detail, 0)
				}
			}
		}
	}

	// 反噬指数 for the binary 大小球
	dxqPayload := dxq.payload("pick_dxq", "12c. 我的大小球（反噬指数）", fmt.Sprintf("按记录盘口线结算，走盘剔除(%d场)；ROI按bet365水位。z≤-1.64且样本≥30才判定显著更差。", dxqPush))
	sample := len(dxq.sig.details)
	if sample > 0 {
		hit := dxq.sig.hit
		z := (float64(hit)/float64(sample) - 0.5) / math.Sqrt(0.25/float64(sample))
		shrunkMiss := (float64(sample-hit) + 20*0.5) / (float64(sample) + 20)
		dxqPayload["z"] = statisticsRound2(z)
		dxqPayload["shrunkMissRate"] = statisticsRound2(shrunkMiss * 100)
		dxqPayload["fadeEv"] = statisticsRound2((shrunkMiss*1.9 - 1) * 100)
		dxqPayload["fadeTriggered"] = sample >= 30 && z <= -1.64 && shrunkMiss*1.9-1 > 0.06
	}

	base13Payload := base13.payload("base_spf", "13a. 前端主推·胜平负（按主推概率分档）", "平均欧赔隐含概率最大方向（H5卡片中央的主推）；命中=该方向即赛果；ROI=按平均欧赔每场投1单位。分档回答“胜率高在哪”。")
	base13Payload["buckets"] = pickBucketRows(base13Bands, pickBaseProbBandOrder, "base-spf")
	qiu13Payload := qiu13.payload("base_qiu", "13b. 前端球数倾向·大小球（按压力强度分档）", "近期场均总进球对当前盘口的压力方向；盘口球(压力差<5)不计入；ROI按bet365水位。")
	qiu13Payload["buckets"] = pickBucketRows(qiu13Bands, pickQiuStrengthBandOrder, "base-qiu")

	signals := []gin.H{
		base13Payload,
		qiu13Payload,
		overview.payload("pick_overview", "12. 我的选择总览", "全部选择按各自玩法结算；ROI=真实赔率回报(1单位/注)，比分玩法无赔率不计ROI。"),
		spf.payload("pick_spf", "12a. 我的胜平负", "ROI按平均欧赔；基准约40%(胜)/23%(平)。"),
		rqspf.payload("pick_rqspf", "12b. 我的让球胜平负", fmt.Sprintf("按记录让球线结算（主让为负）；ROI按竞彩让球赔率%s。", pickMissingLineNote(rqspfMissingLine))),
		dxqPayload,
		scorePick.payload("pick_score", "12d. 我的比分", "多比分任一命中即命中；库中无比分赔率，不计ROI；基准约8%。"),
		pickCrossPayload("cross_spf_base", "14a. 我的胜平负 × 前端主推", "同向=我买的方向与13a一致。", cross["spf_base_same"], cross["spf_base_diff"]),
		pickCrossPayload("cross_spf_comfort", "14b. 我的胜平负 × 庄家舒服", "同向=我买的方向正好是竞彩交易盈亏里庄家最舒服的方向。", cross["spf_comfort_same"], cross["spf_comfort_diff"]),
		pickCrossPayload("cross_dxq_qiu", "14c. 我的大小球 × 前端球数倾向", "同向=我与13b方向一致。", cross["dxq_qiu_same"], cross["dxq_qiu_diff"]),
		pickCrossPayload("cross_dxq_composite", "14d. 我的大小球 × 球数综合均值", "同向=我与(历史+近期均值 vs 盘口)方向一致。", cross["dxq_composite_same"], cross["dxq_composite_diff"]),
	}

	profile := gin.H{
		"radar": []gin.H{
			pickRadarAxis("胜平负", spf, 40, 65),
			pickRadarAxis("让球", rqspf, 33, 60),
			pickRadarAxis("大小球", dxq, 50, 75),
			pickRadarAxis("比分", scorePick, 8, 25),
			pickRadarAxis("高信心执行", confident, 45, 75),
			pickRoiAxis("ROI", overview),
		},
		"asianBuckets": pickBucketRows(asianBuckets, pickAsianBucketOrder, "asian"),
		"goalBuckets":  pickBucketRows(goalBuckets, pickGoalBucketOrder, "goal"),
	}
	return signals, profile
}

func pickCrossPayload(key, title, definition string, same, diff *pickTally) gin.H {
	if same == nil {
		same = &pickTally{}
	}
	if diff == nil {
		diff = &pickTally{}
	}
	sameRow := same.bucketPayload(key+"-same", "同向")
	diffRow := diff.bucketPayload(key+"-diff", "反向")
	matched := len(same.sig.details) + len(diff.sig.details)
	hit := same.sig.hit + diff.sig.hit
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	return gin.H{
		"key": key, "title": title, "definition": definition,
		"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": []gin.H{sameRow, diffRow},
	}
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

func pickRadarAxis(axis string, tally *pickTally, base, good float64) gin.H {
	sample := len(tally.sig.details)
	accuracy := tally.sig.accuracy()
	return gin.H{
		"axis": axis, "sample": sample, "accuracy": accuracy,
		"score": pickRadarScore(accuracy, base, good, sample),
	}
}

func pickRoiAxis(axis string, tally *pickTally) gin.H {
	roi := 0.0
	if tally.stake > 0 {
		roi = tally.ret / tally.stake
	}
	score := 0.0
	if tally.odds >= 3 {
		score = statisticsRound2(statisticsClamp((roi-0.7)/(1.15-0.7)*100, 0, 100))
	}
	return gin.H{
		"axis": axis, "sample": tally.odds,
		"accuracy": statisticsRound2(roi * 100),
		"score":    score,
	}
}

func pickMissingLineNote(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("；%d条缺让球线未结算", count)
}
