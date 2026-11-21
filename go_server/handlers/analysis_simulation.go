package handlers

import (
	"fmt"
	"math"
)

// 非竞彩比赛的本地模拟层：
// 1. 模拟竞彩胜平负指数（平均欧赔隐含概率 × 竞彩返还率），让盈亏测算在非竞彩场次也能算出。
// 2. 模拟冷热指数（欧赔初盘→即时盘变化 + 亚盘投注热度），弥补竞彩官方 hlr/dlr/alr 缺失。

// 竞彩单关实际返还率约 0.85~0.90，取 0.88 作为模拟指数的返还率。
const simulatedSportteryReturn = 0.88

// 散户羊群放大系数：真实彩民对热门的集中度是凸性的（热门越强，跟风越不成比例，
// 1.77 级热门可吸走 70%+ 资金），用幂变换 p^γ 模拟。γ=1 表示不放大。
// 用"竞彩模拟对比 vs 官方支持率"的批量差值回归校准时，只需调这一个值。
const retailHerdingGamma = 1.8

// simulatedSportteryOdds 用平均欧赔隐含概率生成竞彩风格的胜平负指数。
func simulatedSportteryOdds(response *analysisMatchResponse) ([3]float64, bool) {
	odds := response.averageOdds
	if len(odds) < 3 || odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return [3]float64{}, false
	}
	total := 1/odds[0] + 1/odds[1] + 1/odds[2]
	if total <= 0 {
		return [3]float64{}, false
	}
	return [3]float64{
		round2(simulatedSportteryReturn * odds[0] * total),
		round2(simulatedSportteryReturn * odds[1] * total),
		round2(simulatedSportteryReturn * odds[2] * total),
	}, true
}

// simulatedHotColdIndexes 本地模拟冷热指数（约 -10 ~ +10，正值=资金变热，负值=遇冷）。
// 依据：欧赔初盘到即时盘的降赔幅度（降赔=真金白银进场），叠加亚盘主客投注热度偏移。
func simulatedHotColdIndexes(response *analysisMatchResponse) ([3]float64, bool) {
	current := response.averageOdds
	first := response.averageFirstOdds
	drops := [3]float64{}
	hasDrop := false
	if len(current) >= 3 && len(first) >= 3 {
		for index := 0; index < 3; index++ {
			if first[index] > 0 && current[index] > 0 {
				drops[index] = (first[index] - current[index]) / first[index] * 100
				hasDrop = true
			}
		}
	}

	homeHeatBias, guestHeatBias := 0.0, 0.0
	hasHeat := false
	if len(response.YapanAI) >= 2 && (response.YapanAI[0] > 0 || response.YapanAI[1] > 0) {
		homeHeatBias = (response.YapanAI[0] - 50) / 5
		guestHeatBias = (response.YapanAI[1] - 50) / 5
		hasHeat = true
	}
	if !hasDrop && !hasHeat {
		return [3]float64{}, false
	}

	return [3]float64{
		round2(clamp(drops[0]*0.6+homeHeatBias, -10, 10)),
		round2(clamp(drops[1]*0.6, -10, 10)),
		round2(clamp(drops[2]*0.6+guestHeatBias, -10, 10)),
	}, true
}

// ---------- 加权散户心理（模拟散户资金分布，纯本地计算，不使用竞彩数据） ----------
//
// 以平均欧赔隐含概率为基底，按五组本地信号做权重偏移：
//   1. 亚盘深度：散户追深盘强队，盘口越深强方加权越多；
//   2. 升降盘：即时盘相对初盘升盘 → 资金追强方，退盘 → 流出；
//   3. 历史交锋：交锋占优一方吸引跟风盘；
//   4. 最近状态：主客近5场场均净胜球差反映状态热度；
//   5. 综合均值：期望让球比实际盘口更深 → 强方被低估加权；
//      大小球盘口与球数均值决定平局权重（低于2.5中轴 → 平局加权）。

type retailWeightInputs struct {
	probabilities    [3]float64 // 隐含概率（百分比）
	handicapLine     float64    // 亚盘即时盘（主让为正）
	openingLine      float64    // 亚盘初盘
	goalLine         float64    // 大小球即时盘
	historyWinPct    float64
	historyLosePct   float64
	historyAvailable bool
	recentGoalDiff   float64 // 主客近5场场均净胜球差
	recentAvailable  bool
	combinedHandicap *float64 // 期望让球综合均值
	combinedGoals    *float64 // 球数综合均值
}

func simulatedRetailDistribution(in retailWeightInputs) [3]float64 {
	home, draw, away := in.probabilities[0], in.probabilities[1], in.probabilities[2]
	if home <= 0 || draw <= 0 || away <= 0 {
		return in.probabilities
	}

	sideShift := 0.0 // 正值偏向主队（百分点）
	// 1. 亚盘深度
	sideShift += clamp(in.handicapLine*3, -6, 6)
	// 2. 升降盘
	if in.openingLine != 0 || in.handicapLine != 0 {
		sideShift += clamp((in.handicapLine-in.openingLine)*8, -4, 4)
	}
	// 3. 历史交锋
	if in.historyAvailable {
		sideShift += clamp((in.historyWinPct-in.historyLosePct)*0.06, -4, 4)
	}
	// 4. 最近状态
	if in.recentAvailable {
		sideShift += clamp(in.recentGoalDiff*3, -4, 4)
	}
	// 5. 期望让球 vs 实际盘口
	if in.combinedHandicap != nil && in.handicapLine != 0 {
		sideShift += clamp((*in.combinedHandicap-in.handicapLine)*4, -3, 3)
	}
	sideShift = clamp(sideShift, -12, 12)

	// 平局权重：大小球盘口与球数均值加权成锚点，低盘/低均值 → 平局加权。
	goalAnchor := in.goalLine
	if in.combinedGoals != nil && goalAnchor > 0 {
		goalAnchor = goalAnchor*0.6 + *in.combinedGoals*0.4
	} else if in.combinedGoals != nil {
		goalAnchor = *in.combinedGoals
	}
	drawShift := 0.0
	if goalAnchor > 0 {
		drawShift = clamp((2.5-goalAnchor)*6, -5, 5)
	}

	home += sideShift
	away -= sideShift
	draw += drawShift
	// 平局的加减由主客按占比分摊，保持三项总量平衡。
	sideTotal := home + away
	if sideTotal > 0 {
		home -= drawShift * home / sideTotal
		away -= drawShift * away / sideTotal
	}

	home = clamp(home, 5, 85)
	draw = clamp(draw, 5, 85)
	away = clamp(away, 5, 85)

	// 6. 羊群放大：线性偏移之后做幂变换，模拟散户对热门的非线性集中。
	// 均势场次（三项接近）几乎不受影响，热门越突出放大越明显。
	home = math.Pow(home, retailHerdingGamma)
	draw = math.Pow(draw, retailHerdingGamma)
	away = math.Pow(away, retailHerdingGamma)

	total := home + draw + away
	return [3]float64{
		round2(home / total * 100),
		round2(draw / total * 100),
		round2(away / total * 100),
	}
}

// buildSimulatedSpfCompareMarket 竞彩比赛的模拟对比盘：完全按非竞彩口径
// （平均欧赔折算指数 + 散户心理分布 + 模拟冷热）再算一遍，并在备注里给出与官方的指数差。
func buildSimulatedSpfCompareMarket(response *analysisMatchResponse, official *bookmakerMarketResponse) (bookmakerMarketResponse, bool) {
	simOdds, ok := simulatedSportteryOdds(response)
	if !ok {
		return bookmakerMarketResponse{}, false
	}
	distribution, ok := sanhuDistribution(*response)
	if !ok {
		return bookmakerMarketResponse{}, false
	}

	source := bookmakerOddsSource{Key: "sportterySim", Name: "竞彩模拟对比", Odds: simOdds}
	market := buildBookmakerMarket(source, distribution, true)
	market.Simulated = true

	note := "模拟对比：指数由平均欧赔按竞彩返还率折算，分布用加权散户心理（亚盘/大小球/历史/近况/均值修正+羊群放大），冷热为本地推算"
	if official != nil && official.OddsAvailable {
		note += fmt.Sprintf("；与官方指数差 主%+.2f 平%+.2f 客%+.2f",
			simOdds[0]-official.Odds.Home, simOdds[1]-official.Odds.Draw, simOdds[2]-official.Odds.Away)
	}
	market.SimulationNote = note

	if hotCold, ok := simulatedHotColdIndexes(response); ok {
		applySimulatedHotCold(&market, hotCold)
	}
	return market, true
}

// applySimulatedHotCold 把模拟冷热指数写入官方值缺失的赛果行，返回是否有写入。
func applySimulatedHotCold(market *bookmakerMarketResponse, hotCold [3]float64) bool {
	applied := false
	for index := range market.BookmakerByOutcome {
		if index < 3 && market.BookmakerByOutcome[index].HotColdIndex == nil {
			market.BookmakerByOutcome[index].HotColdIndex = roundedFloatPointer(hotCold[index])
			applied = true
		}
	}
	return applied
}
