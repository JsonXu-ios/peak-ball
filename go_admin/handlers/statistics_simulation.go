// Package handlers: statistics_simulation.go ports the H5 "模拟盘" (simulated
// Sporttery) layer so the admin statistics can settle the simulated
// trade-profit-alignment (庄家舒服) dimension the same way the H5 platform block
// does. The H5 markets sportterySim / sportteryRqspfSim are computed live in
// go_server and never stored; this file rebuilds their bookmaker profit from the
// same raw inputs the admin already loads (avg 欧赔 + 盘口 + 交锋/近况), matching
// go_server/handlers/analysis_simulation.go's algorithm.
package handlers

import "math"

const (
	// 竞彩单关返还率，与 go_server simulatedSportteryReturn 保持一致。
	simSportteryReturn = 0.88
	// 散户羊群放大系数，与 go_server retailHerdingGamma 保持一致。
	simRetailHerdingGamma = 1.8
	// 盈亏测算基数，与 go_server bookmakerTotalStake 保持一致。
	simTotalStake = 50000000.0
)

// simRetailInputs mirrors go_server retailWeightInputs.
type simRetailInputs struct {
	probabilities    [3]float64
	handicapLine     float64
	openingLine      float64
	goalLine         float64
	historyWinPct    float64
	historyLosePct   float64
	historyAvailable bool
	recentGoalDiff   float64
	recentAvailable  bool
	combinedHandicap *float64
	combinedGoals    *float64
}

// simRetailDistribution ports go_server simulatedRetailDistribution: implied
// probability base shifted by 亚盘/大小球/历史/近况/均值 signals plus 羊群放大.
func simRetailDistribution(in simRetailInputs) [3]float64 {
	home, draw, away := in.probabilities[0], in.probabilities[1], in.probabilities[2]
	if home <= 0 || draw <= 0 || away <= 0 {
		return in.probabilities
	}

	sideShift := 0.0
	sideShift += statisticsClamp(in.handicapLine*3, -6, 6)
	if in.openingLine != 0 || in.handicapLine != 0 {
		sideShift += statisticsClamp((in.handicapLine-in.openingLine)*8, -4, 4)
	}
	if in.historyAvailable {
		sideShift += statisticsClamp((in.historyWinPct-in.historyLosePct)*0.06, -4, 4)
	}
	if in.recentAvailable {
		sideShift += statisticsClamp(in.recentGoalDiff*3, -4, 4)
	}
	if in.combinedHandicap != nil && in.handicapLine != 0 {
		sideShift += statisticsClamp((*in.combinedHandicap-in.handicapLine)*4, -3, 3)
	}
	sideShift = statisticsClamp(sideShift, -12, 12)

	goalAnchor := in.goalLine
	if in.combinedGoals != nil && goalAnchor > 0 {
		goalAnchor = goalAnchor*0.6 + *in.combinedGoals*0.4
	} else if in.combinedGoals != nil {
		goalAnchor = *in.combinedGoals
	}
	drawShift := 0.0
	if goalAnchor > 0 {
		drawShift = statisticsClamp((2.5-goalAnchor)*6, -5, 5)
	}

	home += sideShift
	away -= sideShift
	draw += drawShift
	sideTotal := home + away
	if sideTotal > 0 {
		home -= drawShift * home / sideTotal
		away -= drawShift * away / sideTotal
	}

	home = statisticsClamp(home, 5, 85)
	draw = statisticsClamp(draw, 5, 85)
	away = statisticsClamp(away, 5, 85)

	home = math.Pow(home, simRetailHerdingGamma)
	draw = math.Pow(draw, simRetailHerdingGamma)
	away = math.Pow(away, simRetailHerdingGamma)

	total := home + draw + away
	return [3]float64{
		statisticsRound2(home / total * 100),
		statisticsRound2(draw / total * 100),
		statisticsRound2(away / total * 100),
	}
}

// simPoissonPMF ports go_server poissonPMF.
func simPoissonPMF(lambda float64, maxCount int) []float64 {
	values := make([]float64, maxCount+1)
	term := math.Exp(-lambda)
	values[0] = term
	for count := 1; count <= maxCount; count++ {
		term = term * lambda / float64(count)
		values[count] = term
	}
	return values
}

// simRqspfGoal ports go_server simulatedRqspfGoal: maps the Asian line to a
// Sporttery-style integer handicap. current/first are 亚盘即时/初盘 (主让为正),
// winProb/loseProb are implied 胜/负 probabilities.
func simRqspfGoal(current, first, winProb, loseProb float64) (int, bool) {
	line := current
	if line == 0 {
		line = first
	}
	hasLine := current != 0 || first != 0
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
	if !hasLine && winProb == 0 && loseProb == 0 {
		return 0, false
	}
	if winProb >= loseProb {
		return -1, true
	}
	return 1, true
}

// simRqspfProbabilities ports go_server simulatedRqspfProbabilities: a Poisson
// score matrix with total-goal expectation = 大小球即时盘 and score-diff = 亚盘盘口,
// re-settled to 胜/平/负 after applying the handicap goal.
func simRqspfProbabilities(goalCurrent, goalFirst, ahCurrent, ahFirst float64, goal int) ([3]float64, bool) {
	totalLine := goalCurrent
	if totalLine <= 0 {
		totalLine = goalFirst
	}
	if totalLine <= 0 {
		totalLine = 2.5
	}
	totalLine = statisticsClamp(totalLine, 1.2, 4.5)

	diff := statisticsClamp(ahCurrent, -3.5, 3.5)
	if diff == 0 {
		diff = statisticsClamp(ahFirst, -3.5, 3.5)
	}

	homeLambda := math.Max(0.15, (totalLine+diff)/2)
	guestLambda := math.Max(0.15, (totalLine-diff)/2)

	const maxGoals = 9
	homePMF := simPoissonPMF(homeLambda, maxGoals)
	guestPMF := simPoissonPMF(guestLambda, maxGoals)

	win, draw, lose := 0.0, 0.0, 0.0
	for h := 0; h <= maxGoals; h++ {
		for g := 0; g <= maxGoals; g++ {
			probability := homePMF[h] * guestPMF[g]
			adjusted := h + goal
			switch {
			case adjusted > g:
				win += probability
			case adjusted == g:
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

// simComfortDirection returns the outcome whose bookmaker profit is highest,
// rebuilt exactly like go_server buildBookmakerOutcomes (5000万池, round2 payout),
// with home winning ties (same as go_server's strict-greater comparison). ok=false
// when the strongest outcome is not actually a bookmaker profit.
func simComfortDirection(distribution, odds [3]float64) (string, bool) {
	keys := [3]string{"home", "draw", "away"}
	best, bestProfit := "", math.Inf(-1)
	for index := 0; index < 3; index++ {
		betStake := simTotalStake * distribution[index] / 100
		payout := statisticsRound2(betStake * odds[index])
		profit := statisticsRound2(simTotalStake - payout)
		if profit > bestProfit {
			best, bestProfit = keys[index], profit
		}
	}
	if best == "" || bestProfit <= 0 {
		return "", false
	}
	return best, true
}

// simSpfComfort rebuilds the 竞彩模拟对比(胜平负) market and returns its 庄家舒服 dir.
func simSpfComfort(oddsRow, pankouRow, historyRow map[string]interface{}, match statisticsMatch) (string, bool) {
	avg := pickAvgOdds(oddsRow)
	if avg == nil {
		return "", false
	}
	total := 1/avg[0] + 1/avg[1] + 1/avg[2]
	if total <= 0 {
		return "", false
	}
	odds := [3]float64{
		statisticsRound2(simSportteryReturn * avg[0] * total),
		statisticsRound2(simSportteryReturn * avg[1] * total),
		statisticsRound2(simSportteryReturn * avg[2] * total),
	}
	distribution := simRetailDistribution(buildSimRetailInputs(oddsRow, pankouRow, historyRow, match))
	return simComfortDirection(distribution, odds)
}

// simRqspfComfort rebuilds the 竞彩让球模拟 market and returns its 庄家舒服 dir.
func simRqspfComfort(oddsRow, pankouRow, historyRow map[string]interface{}, match statisticsMatch) (string, bool) {
	probs := statisticsProbabilities(oddsRow)
	if len(probs) < 3 {
		return "", false
	}
	ahFirst, ahCurrent, _ := statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
	goalFirst, goalCurrent, hasGoal := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
	if !hasGoal || goalCurrent <= 0 {
		// go_server 用 max(近期均球, 历史均球) 兜底缺失的大小球即时盘。
		against, homeRecent, guestRecent := statisticsHistory(historyRow)
		_, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		recentGoals, hasRecent := statisticsRecentGoals(homeRecent, guestRecent)
		fallback := 0.0
		if hasRecent {
			fallback = recentGoals
		}
		if hasHistory && historyGoals > fallback {
			fallback = historyGoals
		}
		if fallback > 0 {
			goalCurrent = statisticsRound2(fallback)
		}
	}

	goal, ok := simRqspfGoal(ahCurrent, ahFirst, probs[0], probs[2])
	if !ok {
		return "", false
	}
	probabilities, ok := simRqspfProbabilities(goalCurrent, goalFirst, ahCurrent, ahFirst, goal)
	if !ok {
		return "", false
	}
	odds := [3]float64{}
	distribution := [3]float64{}
	for index := 0; index < 3; index++ {
		probability := math.Max(probabilities[index], 0.005)
		odds[index] = statisticsRound2(statisticsClamp(simSportteryReturn/probability, 1.01, 60))
		distribution[index] = statisticsRound2(probabilities[index] * 100)
	}
	return simComfortDirection(distribution, odds)
}

// statisticsSimulatedComfort returns the aligned 庄家舒服 direction when the two
// simulated markets (胜平负 & 让球) agree — the admin analogue of go_server's
// pfProfitAlignmentWarningRows simulated branch and of statisticsBookmakerComfort
// for the official markets.
func statisticsSimulatedComfort(oddsRow, pankouRow, historyRow map[string]interface{}, match statisticsMatch) (string, bool) {
	spfDir, ok := simSpfComfort(oddsRow, pankouRow, historyRow, match)
	if !ok {
		return "", false
	}
	rqDir, ok := simRqspfComfort(oddsRow, pankouRow, historyRow, match)
	if !ok || spfDir != rqDir {
		return "", false
	}
	return spfDir, true
}

// buildSimRetailInputs assembles the 散户心理 inputs from the admin's loaded
// tables, matching go_server analysis.go's construction of retailWeightInputs.
func buildSimRetailInputs(oddsRow, pankouRow, historyRow map[string]interface{}, match statisticsMatch) simRetailInputs {
	probs := statisticsProbabilities(oddsRow)
	in := simRetailInputs{}
	if len(probs) >= 3 {
		// go_server 传入的是 round2 后的隐含概率(probabilitiesFromOdds)，此处对齐口径。
		in.probabilities = [3]float64{statisticsRound2(probs[0]), statisticsRound2(probs[1]), statisticsRound2(probs[2])}
	}

	ahFirst, ahCurrent, _ := statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
	in.handicapLine = ahCurrent
	in.openingLine = ahFirst

	against, homeRecentRows, guestRecentRows := statisticsHistory(historyRow)
	historyDiff, historyGoals, hasHistory := statisticsHeadToHead(match, against)
	winPct, losePct, hasWinLose := simHistoryWinLose(match, against)
	in.historyWinPct = winPct
	in.historyLosePct = losePct
	in.historyAvailable = hasWinLose

	recentDiff, hasRecent := statisticsRecentDifference(
		statisticsRecentForm(homeRecentRows, match.Home), statisticsRecentForm(guestRecentRows, match.Guest))
	in.recentGoalDiff = recentDiff
	in.recentAvailable = hasRecent
	recentGoals, hasRecentGoals := statisticsRecentGoals(homeRecentRows, guestRecentRows)

	// 大小球即时盘（缺失时用 max(近期均球, 历史均球) 兜底，同 go_server）。
	goalFirst, goalCurrent, hasGoal := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
	_ = goalFirst
	if !hasGoal || goalCurrent <= 0 {
		fallback := 0.0
		if hasRecentGoals {
			fallback = recentGoals
		}
		if hasHistory && historyGoals > fallback {
			fallback = historyGoals
		}
		if fallback > 0 {
			goalCurrent = statisticsRound2(fallback)
		}
	}
	in.goalLine = goalCurrent

	// combinedHandicap = (历史净胜球期望 + 近期净胜球期望)/2，go_server 恒有值（缺项按0）。
	histDiffOrZero := 0.0
	if hasHistory {
		histDiffOrZero = historyDiff
	}
	recentDiffOrZero := 0.0
	if hasRecent {
		recentDiffOrZero = recentDiff
	}
	combinedHandicap := statisticsRound2((histDiffOrZero + recentDiffOrZero) / 2)
	in.combinedHandicap = &combinedHandicap

	// combinedGoals = 历史均球(0.45) 与 近期均球(0.55) 的加权，两者皆缺时为 nil。
	if in.combinedGoals = simWeightedGoals(historyGoals, hasHistory, recentGoals, hasRecentGoals); in.combinedGoals != nil {
		rounded := statisticsRound2(*in.combinedGoals)
		in.combinedGoals = &rounded
	}
	return in
}

// simHistoryWinLose computes head-to-head 胜率/负率 (主队视角, 3年内) from the
// against rows, mirroring go_server historyPercentages(againstSummary).
func simHistoryWinLose(match statisticsMatch, rows []statisticsHistoryMatch) (float64, float64, bool) {
	win, lose, all := 0, 0, 0
	for _, row := range rows {
		homeScore, guestScore := row.HomeScore, row.GuestScore
		if row.Home == match.Home && row.Guest == match.Guest {
		} else if row.Home == match.Guest && row.Guest == match.Home {
			homeScore, guestScore = row.GuestScore, row.HomeScore
		} else {
			continue
		}
		all++
		switch {
		case homeScore > guestScore:
			win++
		case homeScore < guestScore:
			lose++
		}
	}
	if all == 0 {
		return 0, 0, false
	}
	return float64(win) / float64(all) * 100, float64(lose) / float64(all) * 100, true
}

// simWeightedGoals ports go_server weightedAveragePointer over history(0.45) and
// recent(0.55) goal averages, normalising by the present weights.
func simWeightedGoals(history float64, hasHistory bool, recent float64, hasRecent bool) *float64 {
	sum, weight := 0.0, 0.0
	if hasHistory {
		sum += history * 0.45
		weight += 0.45
	}
	if hasRecent {
		sum += recent * 0.55
		weight += 0.55
	}
	if weight <= 0 {
		return nil
	}
	value := sum / weight
	return &value
}
