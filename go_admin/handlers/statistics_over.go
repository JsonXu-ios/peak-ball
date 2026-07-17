// Package handlers: statistics_over.go is dimension 18 — 直接买大.
// The simplest possible O/U baseline: buy OVER at the current bet365 line on
// every completed match, settled at the real over water. Pushes are excluded.
// Buckets condition the same settle on line size and the existing indicators,
// so it doubles as the control group for dimension 17 (先小后补大): if a bucket
// is red here, 直接买大 alone beats the two-stage play in that spot.
package handlers

import (
	"fmt"
	"math"

	"github.com/gin-gonic/gin"
)

// buildDirectOverSignals settles 直接买大 over every completed match with an
// O/U line and conditions it on the indicator states.
func buildDirectOverSignals(matches []statisticsMatch, histories, pankous map[string]map[string]interface{}) gin.H {
	order := []string{
		"基准·全部有盘比赛",
		"盘≤2.0", "盘2.25", "盘2.5", "盘2.75", "盘≥3.0",
		"模型判大(综合≥盘+0.25)",
		"模型中性(|综合-盘|<0.25)",
		"模型判小(综合≤盘-0.25)",
		"回归大球信号时",
		"回归小球信号时",
		"盘高于共识≥0.25时",
		"盘低于共识≥0.25时",
		"大球热度>65时",
		"小球热度>65时",
	}
	buckets := map[string]*pickTally{}
	add := func(key string, detail statisticsDetail, oddsValue float64) {
		tally := buckets[key]
		if tally == nil {
			tally = &pickTally{}
			buckets[key] = tally
		}
		tally.add(detail, oddsValue)
	}

	pushes := 0
	for _, match := range matches {
		historyRow := histories[match.ID]
		pankouRow := pankous[match.ID]
		_, line, hasLine := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
		if !hasLine || line <= 0 {
			continue
		}
		over, valid := statisticsOverOutcome(match, line)
		if !valid {
			pushes++
			continue
		}
		overWater, _, hasWater := pickDxqWater(pankouRow)
		oddsValue := 0.0
		if hasWater {
			oddsValue = overWater
		}
		total := match.HomeScore + match.GuestScore

		detail := statisticsBaseDetail(match)
		detail.Value = statisticsRound2(line)
		detail.Pick = fmt.Sprintf("买大%.2f（总%d球）", line, total)
		detail.Result = statisticsOverLabel(over)
		detail.Hit = over

		add("基准·全部有盘比赛", detail, oddsValue)
		add(chaseLineBucket(line), detail, oddsValue)

		// ---- 指标切分 ----
		against, homeRecent, guestRecent := statisticsHistory(historyRow)
		_, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		recentGoals, hasRecent := statisticsRecentGoals(homeRecent, guestRecent)

		if composite, ok := statisticsAverage(historyGoals, hasHistory, recentGoals, hasRecent); ok {
			switch {
			case composite >= line+0.25:
				add("模型判大(综合≥盘+0.25)", detail, oddsValue)
			case composite <= line-0.25:
				add("模型判小(综合≤盘-0.25)", detail, oddsValue)
			default:
				add("模型中性(|综合-盘|<0.25)", detail, oddsValue)
			}
		}

		if signal := warningGoalBalanceSignal(historyGoals, hasHistory, recentGoals, hasRecent, line, true); signal != "" {
			if signal == "over" {
				add("回归大球信号时", detail, oddsValue)
			} else {
				add("回归小球信号时", detail, oddsValue)
			}
		}

		if hasHistory && hasRecent && math.Abs(historyGoals-recentGoals) <= deviationGoalsAgree {
			consensus := (historyGoals + recentGoals) / 2
			if line-consensus >= 0.25 {
				add("盘高于共识≥0.25时", detail, oddsValue)
			} else if consensus-line >= 0.25 {
				add("盘低于共识≥0.25时", detail, oddsValue)
			}
		}

		if hasRecent || hasHistory {
			expected := statisticsMean(recentGoals, hasRecent, historyGoals, hasHistory)
			overHeat := statisticsClamp(50+(expected-line)*18, 0, 100)
			if overHeat > 65 {
				add("大球热度>65时", detail, oddsValue)
			} else if (100 - overHeat) > 65 {
				add("小球热度>65时", detail, oddsValue)
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
		rows = append(rows, tally.bucketPayload("over-"+key, key))
	}
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	definition := fmt.Sprintf(
		"每场按bet365当前大小球盘直接买大：命中=总进球>盘口，走盘剔除(%d场)；ROI=按真实大球水位每场投1单位。分档与维度17同口径（盘口大小、模型综合、回归信号、共识偏离、热度），可直接对照——某分档在这里是红区，说明该局面下不用双段、单买大即可。", pushes)
	base := buckets["基准·全部有盘比赛"]
	if base != nil && len(base.sig.details) > 0 {
		total := len(base.sig.details)
		definition += fmt.Sprintf(" 基准%d场：大球率%.1f%%。", total, float64(base.sig.hit)/float64(total)*100)
	}
	return gin.H{
		"key":        "direct_over_signals",
		"title":      "18. 直接买大（全场大小球基准）",
		"definition": definition,
		"matched":    matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": rows,
	}
}
