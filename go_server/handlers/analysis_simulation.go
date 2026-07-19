package handlers

import (
	"fmt"
	"math"
	"strings"
)

// 非竞彩比赛的本地模拟层：
// 1. 模拟竞彩胜平负指数（平均欧赔隐含概率 × 竞彩返还率），让盈亏测算在非竞彩场次也能算出。
// 2. 模拟冷热指数（欧赔初盘→即时盘变化 + 亚盘投注热度），弥补竞彩官方 hlr/dlr/alr 缺失。
// 3. 按竞彩对标亚盘的规律模拟让球盘（让1/2/3球），再用泊松模型推出让球胜平负指数与盈亏。

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

// simulatedRqspfGoal 按竞彩比赛对标亚盘的规律，把亚盘即时盘映射为竞彩式整数让球数。
// 约定：负数=主队让球（同竞彩 jyykRqspf.goal），亚盘正盘口=主让。
// 规律：亚盘 0.75~1.5 → 让1；1.75~2.25 → 让2；≥2.5 → 让3；受让方向对称；
// 浅盘（|盘口|<0.75）时竞彩通常给弱势一方受让1球。
func simulatedRqspfGoal(response *analysisMatchResponse) (int, bool) {
	line := response.YapanPankou2
	if line == 0 {
		line = response.YapanPankou1
	}
	hasLine := response.YapanPankou1 != 0 || response.YapanPankou2 != 0
	switch {
	case line >= 2.5:
		return -3, true
	case line >= 1.75:
		return -2, true
	case line >= 0.75:
		return -1, true
	case line <= -2.5:
		return 3, true
	case line <= -1.75:
		return 2, true
	case line <= -0.75:
		return 1, true
	}
	if !hasLine && response.WinProbability == 0 && response.LoseProbability == 0 {
		return 0, false
	}
	// 浅盘：弱势一方受让1球。
	if response.WinProbability >= response.LoseProbability {
		return -1, true
	}
	return 1, true
}

// simulatedRqspfProbabilities 用泊松比分矩阵推让球后的胜平负概率。
// 总进球期望取大小球即时盘，主客分差期望取亚盘盘口。
func simulatedRqspfProbabilities(response *analysisMatchResponse, goal int) ([3]float64, bool) {
	totalLine := response.QiushuPankou2
	if totalLine <= 0 {
		totalLine = response.QiushuPankou1
	}
	if totalLine <= 0 {
		totalLine = 2.5
	}
	totalLine = clamp(totalLine, 1.2, 4.5)

	diff := clamp(response.YapanPankou2, -3.5, 3.5)
	if diff == 0 {
		diff = clamp(response.YapanPankou1, -3.5, 3.5)
	}

	homeLambda := math.Max(0.15, (totalLine+diff)/2)
	guestLambda := math.Max(0.15, (totalLine-diff)/2)

	const maxGoals = 9
	homePMF := poissonPMF(homeLambda, maxGoals)
	guestPMF := poissonPMF(guestLambda, maxGoals)

	win, draw, lose := 0.0, 0.0, 0.0
	for home := 0; home <= maxGoals; home++ {
		for guest := 0; guest <= maxGoals; guest++ {
			probability := homePMF[home] * guestPMF[guest]
			adjusted := home + goal
			switch {
			case adjusted > guest:
				win += probability
			case adjusted == guest:
				draw += probability
			default:
				lose += probability
			}
		}
	}
	total := win + draw + lose
	if total <= 0 {
		return [3]float64{}, false
	}
	return [3]float64{win / total, draw / total, lose / total}, true
}

func poissonPMF(lambda float64, maxCount int) []float64 {
	values := make([]float64, maxCount+1)
	term := math.Exp(-lambda)
	values[0] = term
	for count := 1; count <= maxCount; count++ {
		term = term * lambda / float64(count)
		values[count] = term
	}
	return values
}

// buildSimulatedRqspfMarket 生成"竞彩让球(模拟)"市场：让球数按亚盘规律，指数按泊松概率，
// 投注分布按概率归一（散户按赔率隐含强弱下注），盈亏由通用公式结算。
func buildSimulatedRqspfMarket(response *analysisMatchResponse) (bookmakerMarketResponse, bool) {
	goal, ok := simulatedRqspfGoal(response)
	if !ok {
		return bookmakerMarketResponse{}, false
	}
	probabilities, ok := simulatedRqspfProbabilities(response, goal)
	if !ok {
		return bookmakerMarketResponse{}, false
	}

	odds := [3]float64{}
	distribution := [3]float64{}
	for index := 0; index < 3; index++ {
		probability := math.Max(probabilities[index], 0.005)
		odds[index] = round2(clamp(simulatedSportteryReturn/probability, 1.01, 60))
		distribution[index] = round2(probabilities[index] * 100)
	}

	goalText := fmt.Sprintf("%+d", goal)
	source := bookmakerOddsSource{
		Key:  "sportteryRqspf",
		Name: "竞彩让球模拟(" + goalText + ")",
		Odds: odds,
		Goal: goalText,
	}
	market := buildBookmakerMarket(source, distribution, true)
	market.Simulated = true
	market.SimulationNote = "模拟数据：让球数按竞彩对标亚盘规律（亚盘 " + pfTrim(response.YapanPankou2, 2) + "），指数与分布由泊松模型和平均欧赔推算，非竞彩官方数据"
	if hotCold, ok := simulatedHotColdIndexes(response); ok {
		applySimulatedHotCold(&market, hotCold)
	}
	return market, true
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

// decorateSimulatedRqspfCompare 把竞彩比赛的模拟让球盘改成对比盘：独立 key，
// 备注里对比模拟让球数与官方让球数、模拟指数与官方指数的差距。
func decorateSimulatedRqspfCompare(simulated *bookmakerMarketResponse, official *bookmakerMarketResponse, trade sportteryTradeData) {
	simulated.Key = "sportteryRqspfSim"

	officialGoal := strings.TrimSpace(trade.JyykRqspf.Goal)
	note := simulated.SimulationNote
	if officialGoal != "" {
		agreement := "两者一致"
		if parsePankouLine(officialGoal) != parsePankouLine(simulated.Goal) {
			agreement = "两者不一致，注意校准映射"
		}
		note += fmt.Sprintf("；官方让球 %s vs 模拟让球 %s（%s）", officialGoal, simulated.Goal, agreement)
	}
	if official != nil && official.OddsAvailable && simulated.OddsAvailable {
		note += fmt.Sprintf("；与官方指数差 主%+.2f 平%+.2f 客%+.2f",
			simulated.Odds.Home-official.Odds.Home, simulated.Odds.Draw-official.Odds.Draw, simulated.Odds.Away-official.Odds.Away)
	}
	simulated.SimulationNote = note
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
