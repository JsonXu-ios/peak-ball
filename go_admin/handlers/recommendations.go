// Package handlers: recommendations.go powers the 高价值信号推荐 menu.
// The FULL dimension catalogue (heat tiers, handicap/goal models, warnings,
// deviation cases, …) is settled against all completed matches ON DEMAND
// (manual 重新计算; the heavy result is cached in memory). Page loads only do
// the cheap part: matching upcoming matches against the cached ACTIVE
// conditions (accuracy ≥70% 跟 or ≤30% 反向, sample ≥8). Recommendations are
// grouped into the four buy directions: 胜平负 / 亚盘(让球) / 大小球 / 比分.
package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	recommendMinSample   = 8
	recommendHighCutoff  = 70.0
	recommendLowCutoff   = 30.0
	recommendDefaultDays = 3
)

// ---------- per-match feature context ----------

type recommendCtx struct {
	match         statisticsMatch
	probabilities []float64
	basePred      string
	basePredProb  float64
	comfortDir    string
	hasComfort    bool
	lossDir       string
	hasLossBoth   bool
	kellyChoices  map[string]bool
	historyDiff   float64
	hasHistory    bool
	recentDiff    float64
	hasRecentDiff bool
	historyGoals  float64
	recentGoals   float64
	hasRecentGls  bool
	hcpConsensus  float64
	hasHcpAgree   bool
	goalConsensus float64
	hasGoalAgree  bool
	ahFirst       float64
	asianLine     float64
	hasAsian      bool
	dxqLine       float64
	hasDxq        bool
	homeWater     float64
	awayWater     float64
	hasAsianWater bool
	overWater     float64
	underWater    float64
	hasDxqWater   bool
	asianHeat     float64 // home side heat
	hasAsianHeat  bool
	goalsHeat     float64 // over side heat
	hasGoalsHeat  bool
}

func buildRecommendCtx(match statisticsMatch, historyRow, pankouRow, oddsRow map[string]interface{}) recommendCtx {
	ctx := recommendCtx{match: match}
	ctx.probabilities = statisticsProbabilities(oddsRow)
	ctx.basePred = pickBasePrediction(oddsRow)
	if len(ctx.probabilities) == 3 {
		ctx.basePredProb = ctx.probabilities[map[string]int{"home": 0, "draw": 1, "away": 2}[ctx.basePred]]
	}
	ctx.comfortDir, ctx.hasComfort = statisticsBookmakerComfort(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade"))
	ctx.lossDir, ctx.hasLossBoth = statisticsBookmakerLossBoth(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade"))
	ctx.kellyChoices = statisticsKellySportteryChoices(oddsRow)

	against, homeRecent, guestRecent := statisticsHistory(historyRow)
	ctx.historyDiff, ctx.historyGoals, ctx.hasHistory = statisticsHeadToHead(match, against)
	ctx.recentDiff, ctx.hasRecentDiff = statisticsRecentDifference(
		statisticsRecentForm(homeRecent, match.Home), statisticsRecentForm(guestRecent, match.Guest))
	ctx.recentGoals, ctx.hasRecentGls = statisticsRecentGoals(homeRecent, guestRecent)
	if ctx.hasHistory && ctx.hasRecentDiff && math.Abs(ctx.historyDiff-ctx.recentDiff) <= deviationHandicapAgree {
		ctx.hasHcpAgree = true
		ctx.hcpConsensus = (ctx.historyDiff + ctx.recentDiff) / 2
	}
	if ctx.hasHistory && ctx.hasRecentGls && math.Abs(ctx.historyGoals-ctx.recentGoals) <= deviationGoalsAgree {
		ctx.hasGoalAgree = true
		ctx.goalConsensus = (ctx.historyGoals + ctx.recentGoals) / 2
	}

	ctx.ahFirst, ctx.asianLine, ctx.hasAsian = statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
	_, ctx.dxqLine, ctx.hasDxq = statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
	ctx.homeWater, ctx.awayWater, ctx.hasAsianWater = statisticsAsianWater(pankouRow)
	ctx.overWater, ctx.underWater, ctx.hasDxqWater = pickDxqWater(pankouRow)

	if ctx.hasAsian && len(ctx.probabilities) == 3 {
		ctx.asianHeat = statisticsAsianHeat(ctx.probabilities[0], ctx.probabilities[2], ctx.ahFirst, ctx.asianLine)
		ctx.hasAsianHeat = true
	}
	if ctx.hasDxq && (ctx.hasRecentGls || ctx.hasHistory) {
		expected := statisticsMean(ctx.recentGoals, ctx.hasRecentGls, ctx.historyGoals, ctx.hasHistory)
		ctx.goalsHeat = statisticsClamp(50+(expected-ctx.dxqLine)*18, 0, 100)
		ctx.hasGoalsHeat = true
	}
	return ctx
}

// ---------- condition definitions ----------

type recommendFire struct {
	fires     bool
	pick      string
	settle    string // outcome / choices / cover / over
	direction string
	line      float64
	oddsValue float64
	extra     string
}

type recommendCondition struct {
	Key      string
	Title    string
	Market   string // spf / asian / dxq / score
	Evaluate func(ctx recommendCtx) recommendFire
}

func pfOutcomeLabelPlain(outcome, home, guest string) string {
	if outcome == "home" {
		return "主胜(" + home + ")"
	}
	if outcome == "away" {
		return "客胜(" + guest + ")"
	}
	return "平局"
}

func recommendOutcomeLabelFor(dir string, match statisticsMatch) string {
	return pfOutcomeLabelPlain(dir, match.Home, match.Guest)
}

// outcomeFromProjection mirrors statisticsOutcomeSignal: |值|≤0.25 判平。
func recommendOutcomeFromProjection(value float64) string {
	if math.Abs(value) <= statisticsHandicapBand {
		return "draw"
	}
	if value > 0 {
		return "home"
	}
	return "away"
}

func recommendOutcomeCondition(key, title string, project func(ctx recommendCtx) (float64, bool)) recommendCondition {
	return recommendCondition{
		Key: key, Title: title, Market: "spf",
		Evaluate: func(ctx recommendCtx) recommendFire {
			value, ok := project(ctx)
			if !ok {
				return recommendFire{}
			}
			direction := recommendOutcomeFromProjection(value)
			return recommendFire{
				fires: true, settle: "outcome", direction: direction,
				pick:  recommendOutcomeLabelFor(direction, ctx.match),
				extra: fmt.Sprintf("期望值%.2f", value),
			}
		},
	}
}

func recommendGoalsCondition(key, title string, project func(ctx recommendCtx) (float64, bool)) recommendCondition {
	return recommendCondition{
		Key: key, Title: title, Market: "dxq",
		Evaluate: func(ctx recommendCtx) recommendFire {
			value, ok := project(ctx)
			if !ok || !ctx.hasDxq || math.Abs(value-ctx.dxqLine) < statisticsPushEpsilon {
				return recommendFire{}
			}
			over := value > ctx.dxqLine
			direction, label, oddsValue := "under", "买小", ctx.underWater
			if over {
				direction, label, oddsValue = "over", "买大", ctx.overWater
			}
			if !ctx.hasDxqWater {
				oddsValue = 0
			}
			return recommendFire{
				fires: true, settle: "over", direction: direction, line: ctx.dxqLine, oddsValue: oddsValue,
				pick:  fmt.Sprintf("%s%.2f", label, ctx.dxqLine),
				extra: fmt.Sprintf("期望%.2f", value),
			}
		},
	}
}

// recommendCatalogue covers every dimension the statistics page settles.
func recommendCatalogue() []recommendCondition {
	conditions := []recommendCondition{}

	// ---- 13a 主推概率分档 (spf) ----
	probBand := func(key, title string, low, high float64) recommendCondition {
		return recommendCondition{
			Key: key, Title: title, Market: "spf",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if len(ctx.probabilities) != 3 || ctx.basePredProb <= 0 || ctx.basePredProb < low || ctx.basePredProb >= high {
					return recommendFire{}
				}
				return recommendFire{
					fires: true, settle: "outcome", direction: ctx.basePred,
					pick:  "跟主推：" + recommendOutcomeLabelFor(ctx.basePred, ctx.match),
					extra: fmt.Sprintf("主推概率%.1f%%", ctx.basePredProb),
				}
			},
		}
	}
	conditions = append(conditions,
		probBand("base_prob_65", "主推概率≥65%·跟主推", 65, 1000),
		probBand("base_prob_55_65", "主推概率55-65%·跟主推", 55, 65),
		probBand("base_prob_45_55", "主推概率45-55%·跟主推", 45, 55),
		probBand("base_prob_lt45", "主推概率<45%·跟主推", 0.01, 45),
	)

	// ---- 2 凯体交集 / 3 庄家舒服 / 凯体反差 / 庄家同向亏损 (spf) ----
	conditions = append(conditions,
		recommendCondition{
			Key: "pro_signal", Title: "凯体交集·跟交集方向", Market: "spf",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if len(ctx.kellyChoices) == 0 {
					return recommendFire{}
				}
				return recommendFire{
					fires: true, settle: "choices",
					pick: "跟凯体交集：" + statisticsChoiceLabel(ctx.kellyChoices),
				}
			},
		},
		recommendCondition{
			Key: "trade_comfort", Title: "交易盈亏同向·舒服方打出", Market: "spf",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasComfort {
					return recommendFire{}
				}
				return recommendFire{
					fires: true, settle: "outcome", direction: ctx.comfortDir,
					pick: "防" + recommendOutcomeLabelFor(ctx.comfortDir, ctx.match) + "——庄家舒服方大概率不打出",
				}
			},
		},
		recommendCondition{
			Key: "kelly_ticai_conflict", Title: "凯体反差·跟共识方向", Market: "spf",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if len(ctx.kellyChoices) == 0 || ctx.basePred == "" || ctx.kellyChoices[ctx.basePred] {
					return recommendFire{}
				}
				return recommendFire{
					fires: true, settle: "choices",
					pick: "坚持主推" + recommendOutcomeLabelFor(ctx.basePred, ctx.match) + "，勿跟凯体共识(" + statisticsChoiceLabel(ctx.kellyChoices) + ")",
				}
			},
		},
		recommendCondition{
			Key: "loss_both_away", Title: "庄家同向亏损(负)·客胜打出", Market: "spf",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasLossBoth || ctx.lossDir != "away" {
					return recommendFire{}
				}
				return recommendFire{
					fires: true, settle: "outcome", direction: "away",
					pick: "客胜(" + ctx.match.Guest + ")",
				}
			},
		},
	)

	// ---- 4/5/6 让球模型 (spf) ----
	conditions = append(conditions,
		recommendOutcomeCondition("history_handicap", "历史期望让球·判胜平负", func(ctx recommendCtx) (float64, bool) {
			return ctx.historyDiff, ctx.hasHistory
		}),
		recommendOutcomeCondition("recent_handicap", "近期状态让球·判胜平负", func(ctx recommendCtx) (float64, bool) {
			return ctx.recentDiff, ctx.hasRecentDiff
		}),
		recommendOutcomeCondition("asian_composite", "亚盘综合均值·判胜平负", func(ctx recommendCtx) (float64, bool) {
			return statisticsAverage(ctx.historyDiff, ctx.hasHistory, ctx.recentDiff, ctx.hasRecentDiff, ctx.asianLine, ctx.hasAsian)
		}),
	)

	// ---- 1a 亚盘热度分档 (asian) ----
	for _, tier := range statisticsHeatTiers {
		tierValue := tier
		conditions = append(conditions, recommendCondition{
			Key: fmt.Sprintf("asian_heat_%d", tierValue), Title: fmt.Sprintf("亚盘热度%d档·跟热度方向赢盘", tierValue), Market: "asian",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasAsianHeat {
					return recommendFire{}
				}
				heat := math.Max(ctx.asianHeat, 100-ctx.asianHeat)
				matchTier, ok := statisticsHeatTier(heat)
				if !ok || matchTier != tierValue {
					return recommendFire{}
				}
				pickHome := ctx.asianHeat >= 50
				side, oddsValue, direction := ctx.match.Guest, ctx.awayWater, "away"
				if pickHome {
					side, oddsValue, direction = ctx.match.Home, ctx.homeWater, "home"
				}
				if !ctx.hasAsianWater {
					oddsValue = 0
				}
				return recommendFire{
					fires: true, settle: "cover", direction: direction, line: ctx.asianLine, oddsValue: oddsValue,
					pick:  "买" + side + "赢盘(" + fmt.Sprintf("%.2f", ctx.asianLine) + ")",
					extra: fmt.Sprintf("热度%.1f%%", heat),
				}
			},
		})
	}

	// ---- 1b 大小球热度分档 (dxq) ----
	for _, tier := range statisticsHeatTiers {
		tierValue := tier
		conditions = append(conditions, recommendCondition{
			Key: fmt.Sprintf("goals_heat_%d", tierValue), Title: fmt.Sprintf("大小球热度%d档·跟热度方向", tierValue), Market: "dxq",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasGoalsHeat {
					return recommendFire{}
				}
				heat := math.Max(ctx.goalsHeat, 100-ctx.goalsHeat)
				matchTier, ok := statisticsHeatTier(heat)
				if !ok || matchTier != tierValue {
					return recommendFire{}
				}
				pickOver := ctx.goalsHeat >= 50
				direction, label, oddsValue := "under", "买小", ctx.underWater
				if pickOver {
					direction, label, oddsValue = "over", "买大", ctx.overWater
				}
				if !ctx.hasDxqWater {
					oddsValue = 0
				}
				return recommendFire{
					fires: true, settle: "over", direction: direction, line: ctx.dxqLine, oddsValue: oddsValue,
					pick:  fmt.Sprintf("%s%.2f", label, ctx.dxqLine),
					extra: fmt.Sprintf("热度%.1f%%", heat),
				}
			},
		})
	}

	// ---- 7 亚盘背离≥0.75 (asian) ----
	conditions = append(conditions, recommendCondition{
		Key: "line_discrepancy", Title: "亚盘背离≥0.75·反被高估方赢盘", Market: "asian",
		Evaluate: func(ctx recommendCtx) recommendFire {
			if !ctx.hasAsian || !ctx.hasHistory || !ctx.hasRecentDiff {
				return recommendFire{}
			}
			diffHistory := ctx.asianLine - ctx.historyDiff
			diffRecent := ctx.asianLine - ctx.recentDiff
			pickHome := false
			if diffHistory >= statisticsGoalDiscrepancy && diffRecent >= statisticsGoalDiscrepancy {
				pickHome = false // 盘口高估主队 → 买客赢盘
			} else if diffHistory <= -statisticsGoalDiscrepancy && diffRecent <= -statisticsGoalDiscrepancy {
				pickHome = true
			} else {
				return recommendFire{}
			}
			side, oddsValue, direction := ctx.match.Guest, ctx.awayWater, "away"
			if pickHome {
				side, oddsValue, direction = ctx.match.Home, ctx.homeWater, "home"
			}
			if !ctx.hasAsianWater {
				oddsValue = 0
			}
			return recommendFire{
				fires: true, settle: "cover", direction: direction, line: ctx.asianLine, oddsValue: oddsValue,
				pick: "买" + side + "赢盘(" + fmt.Sprintf("%.2f", ctx.asianLine) + ")",
			}
		},
	})

	// ---- 15a 让球热度过热·反过热方 (asian) ----
	conditions = append(conditions, recommendCondition{
		Key: "asian_hot_fade", Title: "让球热度>65过热·反过热方赢盘", Market: "asian",
		Evaluate: func(ctx recommendCtx) recommendFire {
			if !ctx.hasAsianHeat {
				return recommendFire{}
			}
			hotHome := ctx.asianHeat > 65
			hotGuest := (100 - ctx.asianHeat) > 65
			if !hotHome && !hotGuest {
				return recommendFire{}
			}
			side, oddsValue, direction := ctx.match.Guest, ctx.awayWater, "away"
			if hotGuest {
				side, oddsValue, direction = ctx.match.Home, ctx.homeWater, "home"
			}
			if !ctx.hasAsianWater {
				oddsValue = 0
			}
			return recommendFire{
				fires: true, settle: "cover", direction: direction, line: ctx.asianLine, oddsValue: oddsValue,
				pick: "反过热：买" + side + "赢盘(" + fmt.Sprintf("%.2f", ctx.asianLine) + ")",
			}
		},
	})

	// ---- 15g 让球修正 (asian) ----
	conditions = append(conditions, recommendCondition{
		Key: "handicap_fix", Title: "让球修正·跟期望方赢盘", Market: "asian",
		Evaluate: func(ctx recommendCtx) recommendFire {
			if !ctx.hasAsian || !ctx.hasHistory || !ctx.hasRecentDiff {
				return recommendFire{}
			}
			implied, label := warningHandicapSignal(ctx.historyDiff, ctx.recentDiff, ctx.asianLine)
			if implied == "" {
				return recommendFire{}
			}
			side, oddsValue := ctx.match.Guest, ctx.awayWater
			if implied == "home" {
				side, oddsValue = ctx.match.Home, ctx.homeWater
			}
			if !ctx.hasAsianWater {
				oddsValue = 0
			}
			return recommendFire{
				fires: true, settle: "cover", direction: implied, line: ctx.asianLine, oddsValue: oddsValue,
				pick:  label + "：买" + side + "赢盘(" + fmt.Sprintf("%.2f", ctx.asianLine) + ")",
			}
		},
	})

	// ---- 16 亚盘夸大/隐藏 (asian) ----
	asianDeviation := func(key, title string, hidden bool, minDeviation, maxDeviation float64) recommendCondition {
		return recommendCondition{
			Key: key, Title: title, Market: "asian",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasHcpAgree || !ctx.hasAsian || math.Abs(ctx.hcpConsensus) < 0.2 {
					return recommendFire{}
				}
				sameDirection := ctx.hcpConsensus*ctx.asianLine > 0
				levelLine := math.Abs(ctx.asianLine) < 0.01
				deviation := math.Abs(ctx.asianLine) - math.Abs(ctx.hcpConsensus)
				if hidden {
					deviation = -deviation
					if !(sameDirection || levelLine) {
						return recommendFire{}
					}
				} else if !sameDirection {
					return recommendFire{}
				}
				if deviation < minDeviation || deviation >= maxDeviation {
					return recommendFire{}
				}
				favoriteHome := ctx.hcpConsensus > 0
				// 夸大→反强方；隐藏→买强方
				pickHomeSide := !favoriteHome
				if hidden {
					pickHomeSide = favoriteHome
				}
				side, oddsValue, direction := ctx.match.Guest, ctx.awayWater, "away"
				if pickHomeSide {
					side, oddsValue, direction = ctx.match.Home, ctx.homeWater, "home"
				}
				if !ctx.hasAsianWater {
					oddsValue = 0
				}
				return recommendFire{
					fires: true, settle: "cover", direction: direction, line: ctx.asianLine, oddsValue: oddsValue,
					pick:  "买" + side + "赢盘(" + fmt.Sprintf("%.2f", ctx.asianLine) + ")",
					extra: fmt.Sprintf("共识%.2f/盘口%.2f", ctx.hcpConsensus, ctx.asianLine),
				}
			},
		}
	}
	conditions = append(conditions,
		asianDeviation("asian_exaggerate_050", "夸大强势方≥0.5·反强方赢盘", false, 0.5, 1000),
		asianDeviation("asian_exaggerate_025", "夸大强势方0.25·反强方赢盘", false, 0.25, 0.5),
		asianDeviation("asian_hidden_050", "隐藏强势方≥0.5·买强方赢盘", true, 0.5, 1000),
		asianDeviation("asian_hidden_025", "隐藏强势方0.25·买强方赢盘", true, 0.25, 0.5),
	)

	// ---- 8/9/10/11 球数模型 (dxq) ----
	conditions = append(conditions,
		recommendGoalsCondition("history_goals", "历史平均球数·对盘判大小", func(ctx recommendCtx) (float64, bool) {
			return ctx.historyGoals, ctx.hasHistory
		}),
		recommendGoalsCondition("recent_goals", "近期平均球数·对盘判大小", func(ctx recommendCtx) (float64, bool) {
			return ctx.recentGoals, ctx.hasRecentGls
		}),
		recommendGoalsCondition("goals_composite", "球数综合均值·对盘判大小", func(ctx recommendCtx) (float64, bool) {
			return statisticsAverage(ctx.historyGoals, ctx.hasHistory, ctx.recentGoals, ctx.hasRecentGls)
		}),
		recommendCondition{
			Key: "goals_discrepancy", Title: "期望高于盘≥0.75·买大", Market: "dxq",
			Evaluate: func(ctx recommendCtx) recommendFire {
				composite, ok := statisticsAverage(ctx.historyGoals, ctx.hasHistory, ctx.recentGoals, ctx.hasRecentGls)
				if !ok || !ctx.hasDxq || composite-ctx.dxqLine < statisticsGoalDiscrepancy {
					return recommendFire{}
				}
				oddsValue := ctx.overWater
				if !ctx.hasDxqWater {
					oddsValue = 0
				}
				return recommendFire{
					fires: true, settle: "over", direction: "over", line: ctx.dxqLine, oddsValue: oddsValue,
					pick:  fmt.Sprintf("买大%.2f", ctx.dxqLine),
					extra: fmt.Sprintf("期望%.2f", composite),
				}
			},
		},
	)

	// ---- 15h 大小球回归 (dxq) ----
	conditions = append(conditions, recommendCondition{
		Key: "goal_balance", Title: "大小球回归(2.5均衡)·跟回归方向", Market: "dxq",
		Evaluate: func(ctx recommendCtx) recommendFire {
			if !ctx.hasDxq {
				return recommendFire{}
			}
			signal := warningGoalBalanceSignal(ctx.historyGoals, ctx.hasHistory, ctx.recentGoals, ctx.hasRecentGls, ctx.dxqLine, true)
			if signal == "" {
				return recommendFire{}
			}
			direction, label, oddsValue := "under", "买小", ctx.underWater
			if signal == "over" {
				direction, label, oddsValue = "over", "买大", ctx.overWater
			}
			if !ctx.hasDxqWater {
				oddsValue = 0
			}
			return recommendFire{
				fires: true, settle: "over", direction: direction, line: ctx.dxqLine, oddsValue: oddsValue,
				pick: fmt.Sprintf("回归%s%.2f", label, ctx.dxqLine),
			}
		},
	})

	// ---- 16 大小球盘口偏离 (dxq) ----
	goalDeviation := func(key, title string, above bool, minDeviation, maxDeviation float64, pickOver bool) recommendCondition {
		return recommendCondition{
			Key: key, Title: title, Market: "dxq",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasGoalAgree || !ctx.hasDxq || ctx.dxqLine <= 0 {
					return recommendFire{}
				}
				deviation := ctx.dxqLine - ctx.goalConsensus
				if !above {
					deviation = -deviation
				}
				if deviation < minDeviation || deviation >= maxDeviation {
					return recommendFire{}
				}
				direction, label, oddsValue := "under", "买小", ctx.underWater
				if pickOver {
					direction, label, oddsValue = "over", "买大", ctx.overWater
				}
				if !ctx.hasDxqWater {
					oddsValue = 0
				}
				return recommendFire{
					fires: true, settle: "over", direction: direction, line: ctx.dxqLine, oddsValue: oddsValue,
					pick:  fmt.Sprintf("%s%.2f", label, ctx.dxqLine),
					extra: fmt.Sprintf("共识%.2f/盘口%.2f", ctx.goalConsensus, ctx.dxqLine),
				}
			},
		}
	}
	conditions = append(conditions,
		goalDeviation("goal_line_above_050", "盘高于共识≥0.5·买大[跟市场]", true, 0.5, 1000, true),
		goalDeviation("goal_line_above_025", "盘高于共识0.25·买大[跟市场]", true, 0.25, 0.5, true),
		goalDeviation("goal_line_below_050", "盘低于共识≥0.5·买小[跟市场]", false, 0.5, 1000, false),
		goalDeviation("goal_line_below_025", "盘低于共识0.25·买大", false, 0.25, 0.5, true),
	)

	return conditions
}

// ---------- settle ----------

func recommendSettle(fire recommendFire, ctx recommendCtx) (bool, bool) {
	switch fire.settle {
	case "outcome":
		return statisticsActualOutcome(ctx.match) == fire.direction, true
	case "choices":
		return ctx.kellyChoices[statisticsActualOutcome(ctx.match)], true
	case "cover":
		homeCovered, valid := statisticsAsianCorrect(ctx.match, fire.line)
		if !valid {
			return false, false
		}
		return (fire.direction == "home") == homeCovered, true
	case "over":
		over, valid := statisticsOverOutcome(ctx.match, fire.line)
		if !valid {
			return false, false
		}
		return (fire.direction == "over") == over, true
	}
	return false, false
}

// ---------- cached stats snapshot (heavy part, manual recompute) ----------

type recommendStat struct {
	Sample, Hit int
	Stake, Ret  float64
	OddsN       int
}

type recommendSnapshot struct {
	GeneratedAt  time.Time
	SettledTotal int
	Stats        map[string]*recommendStat
	ActiveModes  map[string]string // key -> follow / inverse
}

var (
	recommendCacheMu sync.RWMutex
	recommendCache   *recommendSnapshot
)

func recomputeRecommendSnapshot() (*recommendSnapshot, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Find(&rawMatches).Error; err != nil {
		return nil, err
	}
	settled := make([]statisticsMatch, 0, len(rawMatches))
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if match.ID == "" || !match.Settled {
			continue
		}
		settled = append(settled, match)
		ids = append(ids, match.ID)
	}
	histories := loadStatisticsRows("history_moneys", ids)
	pankous := loadStatisticsRows("pankou_moneys", ids)
	odds := loadStatisticsRows("odds_moneys", ids)

	catalogue := recommendCatalogue()
	stats := map[string]*recommendStat{}
	for _, match := range settled {
		ctx := buildRecommendCtx(match, histories[match.ID], pankous[match.ID], odds[match.ID])
		for _, condition := range catalogue {
			fire := condition.Evaluate(ctx)
			if !fire.fires {
				continue
			}
			hit, valid := recommendSettle(fire, ctx)
			if !valid {
				continue
			}
			stat := stats[condition.Key]
			if stat == nil {
				stat = &recommendStat{}
				stats[condition.Key] = stat
			}
			stat.Sample++
			if hit {
				stat.Hit++
			}
			if fire.oddsValue > 0 {
				stat.Stake++
				stat.OddsN++
				if hit {
					stat.Ret += fire.oddsValue
				}
			}
		}
	}

	active := map[string]string{}
	for key, stat := range stats {
		if stat.Sample < recommendMinSample {
			continue
		}
		accuracy := float64(stat.Hit) / float64(stat.Sample) * 100
		if accuracy >= recommendHighCutoff {
			active[key] = "follow"
		} else if accuracy <= recommendLowCutoff {
			active[key] = "inverse"
		}
	}
	return &recommendSnapshot{
		GeneratedAt:  time.Now(),
		SettledTotal: len(settled),
		Stats:        stats,
		ActiveModes:  active,
	}, nil
}

// recommendInversePick turns a ≤30% condition's pick into its reverse advice.
func recommendInversePick(condition recommendCondition, fire recommendFire, ctx recommendCtx) string {
	switch fire.settle {
	case "outcome":
		return "防" + recommendOutcomeLabelFor(fire.direction, ctx.match) + "（该信号方向历史≤30%）"
	case "choices":
		return "反凯体交集：避开" + statisticsChoiceLabel(ctx.kellyChoices)
	case "cover":
		side := ctx.match.Guest
		if fire.direction == "away" {
			side = ctx.match.Home
		}
		return "反向：买" + side + "赢盘(" + fmt.Sprintf("%.2f", fire.line) + ")"
	case "over":
		if fire.direction == "over" {
			return fmt.Sprintf("反向：买小%.2f", fire.line)
		}
		return fmt.Sprintf("反向：买大%.2f", fire.line)
	}
	return "反向参考"
}

// GetSignalRecommendations serves the menu. Heavy stats come from the cache;
// refresh=1 recomputes them manually.
func GetSignalRecommendations(c *gin.Context) {
	days := recommendDefaultDays
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		fmt.Sscanf(raw, "%d", &days)
		if days < 1 {
			days = 1
		}
		if days > 14 {
			days = 14
		}
	}

	if c.Query("refresh") == "1" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "重算正在进行中，请稍候再试"})
			return
		}
		snapshot, err := recomputeRecommendSnapshot()
		statisticsRecomputeMu.Unlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		recommendCacheMu.Lock()
		recommendCache = snapshot
		recommendCacheMu.Unlock()
		// 落库：重启后无需重算即可直接读取。
		if payload, err := json.Marshal(snapshot); err == nil {
			_ = saveStatSnapshot(snapshotKindRecommendations, payload, snapshot.GeneratedAt)
		}
	}

	recommendCacheMu.RLock()
	snapshot := recommendCache
	recommendCacheMu.RUnlock()
	if snapshot == nil {
		// 内存没有 → 尝试读库（服务重启后的正常路径）。
		if payload, _, ok := loadStatSnapshot(snapshotKindRecommendations); ok {
			restored := &recommendSnapshot{}
			if json.Unmarshal(payload, restored) == nil && restored.Stats != nil {
				recommendCacheMu.Lock()
				recommendCache = restored
				recommendCacheMu.Unlock()
				snapshot = restored
			}
		}
	}
	if snapshot == nil {
		c.JSON(http.StatusOK, gin.H{
			"needs_recompute": true,
			"conditions":      []gin.H{},
			"recommendations": []gin.H{},
		})
		return
	}

	catalogue := recommendCatalogue()
	conditionByKey := map[string]recommendCondition{}
	activeConditions := []gin.H{}
	for _, condition := range catalogue {
		mode, isActive := snapshot.ActiveModes[condition.Key]
		if !isActive {
			continue
		}
		conditionByKey[condition.Key] = condition
		stat := snapshot.Stats[condition.Key]
		accuracy := math.Round(float64(stat.Hit)/float64(stat.Sample)*10000) / 100
		row := gin.H{
			"key": condition.Key, "title": condition.Title, "market": condition.Market,
			"sample": stat.Sample, "hit": stat.Hit, "accuracy": accuracy, "mode": mode,
		}
		if stat.Stake > 0 {
			row["roi"] = statisticsRound2(stat.Ret / stat.Stake * 100)
			row["roiSample"] = stat.OddsN
		}
		activeConditions = append(activeConditions, row)
	}

	// upcoming scan (cheap): only matches in the window, only active conditions
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Find(&rawMatches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	today := time.Now().Format("2006-01-02")
	horizon := time.Now().AddDate(0, 0, days).Format("2006-01-02")
	upcoming := make([]statisticsMatch, 0, 64)
	ids := make([]string, 0, 64)
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if match.ID == "" || match.Settled || match.Date < today || match.Date > horizon {
			continue
		}
		upcoming = append(upcoming, match)
		ids = append(ids, match.ID)
	}
	histories := loadStatisticsRows("history_moneys", ids)
	pankous := loadStatisticsRows("pankou_moneys", ids)
	odds := loadStatisticsRows("odds_moneys", ids)

	type recommendationRow struct {
		sortKey string
		payload gin.H
	}
	rows := []recommendationRow{}
	for _, match := range upcoming {
		ctx := buildRecommendCtx(match, histories[match.ID], pankous[match.ID], odds[match.ID])
		markets := map[string][]gin.H{"spf": {}, "asian": {}, "dxq": {}, "score": {}}
		fired := 0
		for key, condition := range conditionByKey {
			fire := condition.Evaluate(ctx)
			if !fire.fires {
				continue
			}
			mode := snapshot.ActiveModes[key]
			stat := snapshot.Stats[key]
			accuracy := math.Round(float64(stat.Hit)/float64(stat.Sample)*10000) / 100
			pick := fire.pick
			if mode == "inverse" {
				pick = recommendInversePick(condition, fire, ctx)
			}
			markets[condition.Market] = append(markets[condition.Market], gin.H{
				"key": key, "title": condition.Title, "mode": mode,
				"pick": pick, "extra": fire.extra,
				"accuracy": accuracy, "sample": stat.Sample,
			})
			fired++
		}
		if fired == 0 {
			continue
		}
		rows = append(rows, recommendationRow{
			sortKey: match.MatchTime + match.ID,
			payload: gin.H{
				"matchId": match.ID, "date": match.Date, "state": match.State,
				"matchTime": match.MatchTime, "league": match.League,
				"home": match.Home, "guest": match.Guest,
				"homeLogo": match.HomeLogo, "guestLogo": match.GuestLogo,
				"markets": markets,
			},
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].sortKey < rows[j].sortKey })
	recommendations := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		recommendations = append(recommendations, row.payload)
	}

	c.JSON(http.StatusOK, gin.H{
		"needs_recompute":    false,
		"stats_generated_at": snapshot.GeneratedAt.Format("2006-01-02 15:04:05"),
		"settled_total":      snapshot.SettledTotal,
		"upcoming_total":     len(upcoming),
		"days":               days,
		"min_sample":         recommendMinSample,
		"high_cutoff":        recommendHighCutoff,
		"low_cutoff":         recommendLowCutoff,
		"conditions":         activeConditions,
		"recommendations":    recommendations,
	})
}
