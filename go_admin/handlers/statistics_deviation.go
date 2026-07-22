// Package handlers: statistics_deviation.go is dimension 16 — 盘口偏离特殊局面.
// The owner's spec: when 历史期望 and 近期状态 AGREE with each other, but the
// market line deviates from that consensus, the deviation itself is a signal:
//
//	夸大强势方  历史≈近期≈让0.25，盘口却让0.5/0.75  → 反强方赢盘
//	隐藏强势方  历史≈近期≈让0.5， 盘口却让0.25/0   → 买强方赢盘
//	夸大大球    历史≈近期≈2.5，   盘口开2.75/3     → 买小
//	隐藏大球    历史≈近期≈2.5，   盘口开2.25       → 买大
//
// Each scenario is split by deviation size (0.25 vs ≥0.5) and settled at real
// water so both hit rate and ROI are visible.
package handlers

import (
	"math"

	"github.com/gin-gonic/gin"
)

const (
	deviationHandicapAgree = 0.5  // 历史与近期让球期望的最大分歧（v2 放宽）
	deviationGoalsAgree    = 0.5  // 历史与近期球数期望的最大分歧（v2 放宽）
	deviationTrigger       = 0.25 // 盘口相对共识的最小偏离
)

func deviationBand(deviation float64) string {
	if deviation >= 0.5 {
		return "偏离≥0.5"
	}
	return "偏离0.25"
}

// buildDeviationSignals settles the four special-deviation scenarios.
func buildDeviationSignals(matches []statisticsMatch, histories, pankous map[string]map[string]interface{}) gin.H {
	buckets := map[string]*pickTally{}
	order := []string{
		"夸大强势方(偏离0.25)·反强方赢盘",
		"夸大强势方(偏离≥0.5)·反强方赢盘",
		"隐藏强势方(偏离0.25)·买强方赢盘",
		"隐藏强势方(偏离≥0.5)·买强方赢盘",
		"盘高于共识(偏离0.25)·买大[跟市场]",
		"盘高于共识(偏离≥0.5)·买大[跟市场]",
		"盘低于共识(偏离0.25)·买大",
		"盘低于共识(偏离≥0.5)·买大",
		"盘≈共识(<0.25)·买大[对照组]",
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
		against, homeRecent, guestRecent := statisticsHistory(historyRow)
		historyDiff, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		recentDiff, hasRecentDiff := statisticsRecentDifference(
			statisticsRecentForm(homeRecent, match.Home), statisticsRecentForm(guestRecent, match.Guest))
		recentGoals, hasRecentGoals := statisticsRecentGoals(homeRecent, guestRecent)

		// ---- 亚盘：夸大/隐藏强势方 ----
		if hasHistory && hasRecentDiff && math.Abs(historyDiff-recentDiff) <= deviationHandicapAgree {
			consensus := (historyDiff + recentDiff) / 2
			_, line, hasLine := statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
			homeWater, awayWater, hasWater := statisticsAsianWater(pankouRow)
			if hasLine && math.Abs(consensus) >= 0.2 {
				sameDirection := consensus*line > 0
				levelLine := math.Abs(line) < 0.01
				deviation := math.Abs(line) - math.Abs(consensus)
				favoriteHome := consensus > 0

				if homeCovered, valid := statisticsAsianCorrect(match, line); valid {
					detail := statisticsBaseDetail(match)
					detail.Value = statisticsRound2(deviation)

					// 夸大：同方向且盘口比共识深 ≥0.25 → 反强方赢盘
					if sameDirection && deviation >= deviationTrigger {
						fadeHit := homeCovered != favoriteHome // 强方没赢盘
						detail.Pick = "反强方(" + statisticsCoverLabel(!favoriteHome) + ")"
						detail.Result = statisticsCoverLabel(homeCovered)
						detail.Hit = fadeHit
						oddsValue := 0.0
						if hasWater {
							if favoriteHome {
								oddsValue = awayWater
							} else {
								oddsValue = homeWater
							}
						}
						add("夸大强势方("+deviationBand(deviation)+")·反强方赢盘", detail, oddsValue)
					}

					// 隐藏：同方向(或降到平手)且盘口比共识浅 ≥0.25 → 买强方赢盘
					if (sameDirection || levelLine) && -deviation >= deviationTrigger {
						followHit := homeCovered == favoriteHome
						detail.Pick = "买强方(" + statisticsCoverLabel(favoriteHome) + ")"
						detail.Result = statisticsCoverLabel(homeCovered)
						detail.Hit = followHit
						oddsValue := 0.0
						if hasWater {
							if favoriteHome {
								oddsValue = homeWater
							} else {
								oddsValue = awayWater
							}
						}
						add("隐藏强势方("+deviationBand(-deviation)+")·买强方赢盘", detail, oddsValue)
					}
				}
			}
		}

		// ---- 大小球：夸大/隐藏大球 ----
		if hasHistory && hasRecentGoals && math.Abs(historyGoals-recentGoals) <= deviationGoalsAgree {
			consensus := (historyGoals + recentGoals) / 2
			_, line, hasLine := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
			overWater, underWater, hasWater := pickDxqWater(pankouRow)
			if hasLine && line > 0 {
				deviation := line - consensus
				if over, valid := statisticsOverOutcome(match, line); valid {
					detail := statisticsBaseDetail(match)
					detail.Value = statisticsRound2(deviation)
					detail.Result = statisticsOverLabel(over)
					detail.Pick = "买大"
					detail.Hit = over
					overOddsValue := 0.0
					if hasWater {
						overOddsValue = overWater
					}
					_ = underWater

					// v1 回测：盘高于共识时买小仅25%命中 → v2 翻转为跟市场买大。
					if deviation >= deviationTrigger {
						add("盘高于共识("+deviationBand(deviation)+")·买大[跟市场]", detail, overOddsValue)
					} else if -deviation >= deviationTrigger {
						add("盘低于共识("+deviationBand(-deviation)+")·买大", detail, overOddsValue)
					} else {
						// 对照组：盘≈共识。若对照组命中率与偏离组相当，
						// 说明是“历史≈近期局面下大球被低估”，而非偏离信号本身。
						add("盘≈共识(<0.25)·买大[对照组]", detail, overOddsValue)
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
		rows = append(rows, tally.bucketPayload("dev-"+key, key))
	}
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	return gin.H{
		"key":   "deviation_signals",
		"title": "17. 盘口偏离特殊局面 v2（夸大/隐藏强势方；大小球统一买大+对照组）",
		"definition": "前提：历史期望与近期状态一致（差≤0.5）。让球：夸大强势方=盘口比共识深≥0.25→反强方赢盘，隐藏强势方=盘口比共识浅≥0.25→买强方赢盘。大小球：v1显示盘高于共识买小仅25%，故v2全部按买大结算并加对照组（盘≈共识）——若对照组与偏离组命中率相当，则规律是“该局面下大球被低估”而非偏离本身。ROI按真实水位每场1单位。",
		"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": rows,
	}
}
