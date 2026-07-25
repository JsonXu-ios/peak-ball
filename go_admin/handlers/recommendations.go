// Package handlers: recommendations.go 定义信号维度目录（recommendCatalogue）、
// 每场特征上下文（buildRecommendCtx / recommendCtx）与结算口径（recommendSettle）。
// 这套目录覆盖完赛统计页全部 18 个维度（热度分档、让球/球数模型、警示、盘口偏离、
// 邪修…），现由「信号命中率总台」(statistics_workshop.go) 复用来构建特征矩阵。
package handlers

import (
	"fmt"
	"math"
)

// ---------- per-match feature context ----------

type recommendCtx struct {
	match         statisticsMatch
	probabilities []float64
	basePred      string
	basePredProb  float64
	comfortDir    string
	hasComfort    bool
	simComfortDir string
	hasSimComfort bool
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
	// 维度15 前端球数倾向（与统计端 pickQiuPrediction/pickQiuStrength 同口径）
	qiuDirection string
	qiuLine      float64
	qiuStrength  float64
	hasQiu       bool
	// 维度18 邪修一推/二推预测（由 go_server 桥接，nil=该场无预测）
	evil *recommendEvilPred
}

func buildRecommendCtx(match statisticsMatch, historyRow, pankouRow, oddsRow map[string]interface{}, evil *recommendEvilPred) recommendCtx {
	ctx := recommendCtx{match: match}
	ctx.probabilities = statisticsProbabilities(oddsRow)
	ctx.basePred = pickBasePrediction(oddsRow)
	if len(ctx.probabilities) == 3 {
		ctx.basePredProb = ctx.probabilities[map[string]int{"home": 0, "draw": 1, "away": 2}[ctx.basePred]]
	}
	ctx.comfortDir, ctx.hasComfort = statisticsBookmakerComfort(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade"))
	ctx.simComfortDir, ctx.hasSimComfort = statisticsSimulatedComfort(oddsRow, pankouRow, historyRow, match)
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
	ctx.qiuDirection, ctx.qiuLine, ctx.hasQiu = pickQiuPrediction(historyRow, pankouRow, match)
	if ctx.hasQiu && ctx.qiuDirection != "" {
		ctx.qiuStrength = pickQiuStrength(historyRow, ctx.qiuLine, match)
	}
	ctx.evil = evil
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

// recommendCatalogue 覆盖完赛统计页全部 18 个维度：
// 1凯体同向 2交易盈亏 3模拟盈亏 4/5/6让球模型 7主推分档 8亚盘热度 9亚盘背离
// 10/11/12球数模型 13大小球热度 14期望背离 15球数压力分档 16警示(过热/修正/回归/
// 反差/同向亏损) 17盘口偏离(夸大/隐藏/大小球+对照组) 18邪修一推/二推。
// 维度18 的预测由 go_server 桥接：重算时用 accuracy-stats 的已结算逐场行，
// 页面加载时（仅当邪修条件上岗）按日期拉 /analysis/matches 的待赛预测。
func recommendCatalogue() []recommendCondition {
	conditions := []recommendCondition{}

	// ---- 维度7 前端主推·概率分档 (spf) ----
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

	// ---- 维度1 凯体同向 / 2 庄家舒服 / 3 模拟舒服 / 16警示(凯体反差·同向亏损) (spf) ----
	conditions = append(conditions,
		recommendCondition{
			Key: "pro_signal", Title: "凯体同向·跟首选方向", Market: "spf",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if len(ctx.kellyChoices) == 0 {
					return recommendFire{}
				}
				return recommendFire{
					fires: true, settle: "choices",
					pick: "跟凯体首选：" + statisticsChoiceLabel(ctx.kellyChoices),
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
			Key: "sim_trade_comfort", Title: "模拟交易盈亏同向·舒服方打出", Market: "spf",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasSimComfort {
					return recommendFire{}
				}
				return recommendFire{
					fires: true, settle: "outcome", direction: ctx.simComfortDir,
					pick: "防" + recommendOutcomeLabelFor(ctx.simComfortDir, ctx.match) + "——模拟盘庄家舒服方大概率不打出",
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

	// ---- 维度4/5/6 让球模型 (spf) ----
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

	// ---- 维度8 亚盘热度分档 (asian)，与统计维度一致：档位×主客方向拆分 ----
	for _, tier := range statisticsHeatTiers {
		tierValue := tier
		for _, dir := range []struct {
			suffix, label string
			home          bool
		}{
			{"home", "朝主队", true},
			{"guest", "朝客队", false},
		} {
			condDir := dir
			conditions = append(conditions, recommendCondition{
				Key:    fmt.Sprintf("asian_heat_%d_%s", tierValue, condDir.suffix),
				Title:  fmt.Sprintf("亚盘热度%d档·%s跟赢盘", tierValue, condDir.label),
				Market: "asian",
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
					if pickHome != condDir.home {
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
						pick:  "买" + side + "赢盘(" + fmt.Sprintf("%.2f", ctx.asianLine) + ")",
						extra: fmt.Sprintf("热度%.1f%%", heat),
					}
				},
			})
		}
	}

	// ---- 维度13 大小球热度分档 (dxq)，与统计维度一致：档位×大小方向拆分 ----
	for _, tier := range statisticsGoalsHeatTiers {
		tierValue := tier
		for _, dir := range []struct {
			suffix, label string
			over          bool
		}{
			{"over", "判大", true},
			{"under", "判小", false},
		} {
			condDir := dir
			conditions = append(conditions, recommendCondition{
				Key:    fmt.Sprintf("goals_heat_%d_%s", tierValue, condDir.suffix),
				Title:  fmt.Sprintf("大小球热度%d档·%s跟方向", tierValue, condDir.label),
				Market: "dxq",
				Evaluate: func(ctx recommendCtx) recommendFire {
					if !ctx.hasGoalsHeat {
						return recommendFire{}
					}
					heat := math.Max(ctx.goalsHeat, 100-ctx.goalsHeat)
					matchTier, ok := statisticsHeatTierIn(statisticsGoalsHeatTiers, heat)
					if !ok || matchTier != tierValue {
						return recommendFire{}
					}
					pickOver := ctx.goalsHeat >= 50
					if pickOver != condDir.over {
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
						extra: fmt.Sprintf("热度%.1f%%", heat),
					}
				},
			})
		}
	}

	// ---- 维度9 亚盘背离≥0.75 (asian) ----
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

	// ---- 维度16警示 让球热度过热·反过热方 (asian) ----
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

	// ---- 维度16警示 让球修正 (asian) ----
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
				pick: label + "：买" + side + "赢盘(" + fmt.Sprintf("%.2f", ctx.asianLine) + ")",
			}
		},
	})

	// ---- 维度17 亚盘夸大/隐藏 (asian) ----
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

	// ---- 维度10/11/12/14 球数模型 (dxq) ----
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

	// ---- 维度15 前端球数倾向·压力分档 (dxq)，与统计维度同口径（盘口球<5不计入） ----
	qiuBand := func(key, title string, low, high float64) recommendCondition {
		return recommendCondition{
			Key: key, Title: title, Market: "dxq",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasQiu || ctx.qiuDirection == "" || ctx.qiuStrength < low || ctx.qiuStrength >= high {
					return recommendFire{}
				}
				direction, label, oddsValue := "under", "买小", ctx.underWater
				if ctx.qiuDirection == "over" {
					direction, label, oddsValue = "over", "买大", ctx.overWater
				}
				if !ctx.hasDxqWater {
					oddsValue = 0
				}
				return recommendFire{
					fires: true, settle: "over", direction: direction, line: ctx.qiuLine, oddsValue: oddsValue,
					pick:  fmt.Sprintf("%s%.2f", label, ctx.qiuLine),
					extra: fmt.Sprintf("压力差%.1f", ctx.qiuStrength),
				}
			},
		}
	}
	conditions = append(conditions,
		qiuBand("base_qiu_30", "前端球数压力差≥30·跟方向", 30, 1000),
		qiuBand("base_qiu_15_30", "前端球数压力差15-30·跟方向", 15, 30),
		qiuBand("base_qiu_5_15", "前端球数压力差5-15·跟方向", 5, 15),
	)

	// ---- 维度18 邪修一推/二推 (dxq)：跟该推方向对其大小球线；结算=方向正确 ----
	evilCond := func(key, title string, main bool, over bool) recommendCondition {
		return recommendCondition{
			Key: key, Title: title, Market: "dxq",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if ctx.evil == nil {
					return recommendFire{}
				}
				name, dir, line, value := "一推", ctx.evil.FirstDirection, ctx.evil.FirstLine, ctx.evil.FirstValue
				if main {
					name, dir, line, value = "二推", ctx.evil.MainDirection, ctx.evil.MainLine, ctx.evil.MainValue
				}
				wantDir := "over"
				if !over {
					wantDir = "under"
				}
				if dir != wantDir || line <= 0 {
					return recommendFire{}
				}
				label, direction := "买大", "over"
				if !over {
					label, direction = "买小", "under"
				}
				return recommendFire{
					fires: true, settle: "over", direction: direction, line: line,
					pick:  fmt.Sprintf("邪修%s：%s%.2f", name, label, line),
					extra: fmt.Sprintf("%s精确球数%d", name, value),
				}
			},
		}
	}
	conditions = append(conditions,
		evilCond("evil_first_over", "邪修一推·判大球跟方向", false, true),
		evilCond("evil_first_under", "邪修一推·判小球跟方向", false, false),
		evilCond("evil_main_over", "邪修二推·判大球跟方向", true, true),
		evilCond("evil_main_under", "邪修二推·判小球跟方向", true, false),
	)

	// ---- 维度16警示 大小球回归 (dxq) ----
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

	// ---- 维度17 大小球盘口偏离 (dxq) ----
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
	// 与统计17 v2 同口径：大小球偏离统一买大，另加 盘≈共识 对照组。
	conditions = append(conditions,
		goalDeviation("goal_line_above_050", "盘高于共识≥0.5·买大[跟市场]", true, 0.5, 1000, true),
		goalDeviation("goal_line_above_025", "盘高于共识0.25·买大[跟市场]", true, 0.25, 0.5, true),
		goalDeviation("goal_line_below_050", "盘低于共识≥0.5·买大", false, 0.5, 1000, true),
		goalDeviation("goal_line_below_025", "盘低于共识0.25·买大", false, 0.25, 0.5, true),
		recommendCondition{
			Key: "goal_line_near", Title: "盘≈共识(<0.25)·买大[对照组]", Market: "dxq",
			Evaluate: func(ctx recommendCtx) recommendFire {
				if !ctx.hasGoalAgree || !ctx.hasDxq || ctx.dxqLine <= 0 || math.Abs(ctx.dxqLine-ctx.goalConsensus) >= 0.25 {
					return recommendFire{}
				}
				oddsValue := ctx.overWater
				if !ctx.hasDxqWater {
					oddsValue = 0
				}
				return recommendFire{
					fires: true, settle: "over", direction: "over", line: ctx.dxqLine, oddsValue: oddsValue,
					pick:  fmt.Sprintf("买大%.2f", ctx.dxqLine),
					extra: fmt.Sprintf("共识%.2f/盘口%.2f", ctx.goalConsensus, ctx.dxqLine),
				}
			},
		},
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
