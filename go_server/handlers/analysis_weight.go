package handlers

import (
	"math"
	"strings"
)

const bookmakerTotalStake = 50000000.0

var bookmakerOutcomeKeys = []string{"home", "draw", "away"}
var bookmakerOutcomeLabels = []string{"主胜打出", "平局打出", "客胜打出"}

type directionValues struct {
	Home float64 `json:"home"`
	Draw float64 `json:"draw"`
	Away float64 `json:"away"`
}

type sportteryTradePayload struct {
	Code int                `json:"code"`
	Data sportteryTradeData `json:"data"`
}

type sportteryTradeData struct {
	Tzbl      sportteryBettingRatio `json:"tzbl"`
	JyykSpf   sportteryProfitSpf    `json:"jyykSpf"`
	JyykRqspf sportteryProfitRqspf  `json:"jyykRqspf"`
}

type sportteryBettingRatio struct {
	H            float64 `json:"h"`
	D            float64 `json:"d"`
	A            float64 `json:"a"`
	HProbability float64 `json:"hprobability"`
	DProbability float64 `json:"dprobability"`
	AProbability float64 `json:"aprobability"`
	HSupportRate float64 `json:"hsupportRate"`
	DSupportRate float64 `json:"dsupportRate"`
	ASupportRate float64 `json:"asupportRate"`
	HError       float64 `json:"herror"`
	DError       float64 `json:"derror"`
	AError       float64 `json:"aerror"`
	PsyError     float64 `json:"psyError"`
}

type sportteryProfitSpf struct {
	H            float64 `json:"h"`
	D            float64 `json:"d"`
	A            float64 `json:"a"`
	HSupportRate float64 `json:"hsupportRate"`
	DSupportRate float64 `json:"dsupportRate"`
	ASupportRate float64 `json:"asupportRate"`
	HY           float64 `json:"hy"`
	DY           float64 `json:"dy"`
	AY           float64 `json:"ay"`
	HLR          float64 `json:"hlr"`
	DLR          float64 `json:"dlr"`
	ALR          float64 `json:"alr"`
}

type sportteryProfitRqspf struct {
	Goal         string  `json:"goal"`
	H            float64 `json:"h"`
	D            float64 `json:"d"`
	A            float64 `json:"a"`
	HSupportRate float64 `json:"hsupportRate"`
	DSupportRate float64 `json:"dsupportRate"`
	ASupportRate float64 `json:"asupportRate"`
	HY           float64 `json:"hy"`
	DY           float64 `json:"dy"`
	AY           float64 `json:"ay"`
}

type bookmakerOddsSource struct {
	Key       string
	Name      string
	CompanyID string
	Odds      [3]float64
	Goal      string
}

type bookmakerOutcomeResponse struct {
	Outcome            string   `json:"outcome"`
	OutcomeLabel       string   `json:"outcomeLabel"`
	RetailShare        float64  `json:"retailShare"`
	Probability        *float64 `json:"probability,omitempty"`
	Error              *float64 `json:"error,omitempty"`
	BetStake           float64  `json:"betStake"`
	TotalStake         float64  `json:"totalStake"`
	Odds               float64  `json:"odds"`
	Payout             float64  `json:"payout"`
	BookmakerProfit    float64  `json:"bookmakerProfit"`
	BookmakerLoss      float64  `json:"bookmakerLoss"`
	BookmakerRoi       float64  `json:"bookmakerRoi"`
	OfficialProfitRate *float64 `json:"officialProfitRate,omitempty"`
	HotColdIndex       *float64 `json:"hotColdIndex,omitempty"`
	BookmakerOutcome   string   `json:"bookmakerOutcome"`
	Available          bool     `json:"available"`
}

type bookmakerMarketResponse struct {
	Key                  string                     `json:"key"`
	Name                 string                     `json:"name"`
	CompanyID            string                     `json:"companyId,omitempty"`
	Goal                 string                     `json:"goal,omitempty"`
	Odds                 directionValues            `json:"odds"`
	OddsAvailable        bool                       `json:"oddsAvailable"`
	RetailDistribution   directionValues            `json:"retailDistribution"`
	PsychologyError      *float64                   `json:"psychologyError,omitempty"`
	PsychologyErrorLabel string                     `json:"psychologyErrorLabel,omitempty"`
	BettingRatio         []bookmakerOutcomeResponse `json:"bettingRatio,omitempty"`
	BookmakerByOutcome   []bookmakerOutcomeResponse `json:"bookmakerByOutcome"`
	Simulated            bool                       `json:"simulated,omitempty"`
	SimulationNote       string                     `json:"simulationNote,omitempty"`
}

type matchRoiSimulationResponse struct {
	TotalStake         float64                   `json:"totalStake"`
	RetailDistribution directionValues           `json:"retailDistribution"`
	Markets            []bookmakerMarketResponse `json:"markets"`
}

func attachAnalysisWeights(response *analysisMatchResponse) {
	retailDistribution, retailAvailable := sportterySupportDistribution(response.sportteryTrade)
	if !retailAvailable {
		retailDistribution, retailAvailable = sanhuDistribution(*response)
	}

	response.RoiSimulation = &matchRoiSimulationResponse{
		TotalStake:         bookmakerTotalStake,
		RetailDistribution: directionValuesFromArray(retailDistribution),
		Markets:            buildBookmakerMarkets(response, retailDistribution, retailAvailable),
	}
}

func sportterySupportDistribution(trade sportteryTradeData) ([3]float64, bool) {
	values := [3]float64{trade.Tzbl.HSupportRate, trade.Tzbl.DSupportRate, trade.Tzbl.ASupportRate}
	if percentages, ok := percentValues(values); ok {
		return percentages, true
	}
	values = [3]float64{trade.JyykSpf.HSupportRate, trade.JyykSpf.DSupportRate, trade.JyykSpf.ASupportRate}
	return percentValues(values)
}

func sanhuDistribution(response analysisMatchResponse) ([3]float64, bool) {
	if len(response.SanhuXinli) < 3 {
		return [3]float64{}, false
	}
	values := [3]float64{parseFloat(response.SanhuXinli[0]), parseFloat(response.SanhuXinli[1]), parseFloat(response.SanhuXinli[2])}
	return percentValues(values)
}

func buildBookmakerMarkets(response *analysisMatchResponse, retailDistribution [3]float64, retailAvailable bool) []bookmakerMarketResponse {
	sources := response.bookmakerOdds
	trade := response.sportteryTrade
	items := make([]bookmakerMarketResponse, 0, len(sources)+1)
	for _, source := range sources {
		marketDistribution := retailDistribution
		marketRetailAvailable := retailAvailable
		marketSimulated := false
		if source.Key == "sporttery" {
			if distribution, ok := sportterySpfDistribution(trade); ok {
				marketDistribution = distribution
				marketRetailAvailable = true
			}
			// 非竞彩比赛没有官方指数：用平均欧赔模拟竞彩指数，让盈亏测算不缺位。
			if !oddsAvailable(source.Odds) {
				if simulatedOdds, ok := simulatedSportteryOdds(response); ok {
					source.Odds = simulatedOdds
					source.Name = "竞彩模拟"
					marketSimulated = true
				}
			}
		}

		market := buildBookmakerMarket(source, marketDistribution, marketRetailAvailable)
		if source.Key == "sporttery" {
			decorateSportterySpfMarket(&market, trade)
			if marketSimulated {
				market.Simulated = true
				market.SimulationNote = "模拟数据：指数由平均欧赔按竞彩返还率折算，投注分布来自加权散户心理（亚盘/大小球/历史/近况/均值修正+羊群放大），非竞彩官方数据"
				// 投注比例和心理误差只来自竞彩官方，模拟盘清空避免误读。
				market.BettingRatio = nil
				market.PsychologyError = nil
				market.PsychologyErrorLabel = ""
			}
			// 官方冷热指数缺失时（非竞彩比赛），回退到本地模拟冷热指数。
			if hotCold, ok := simulatedHotColdIndexes(response); ok {
				if applySimulatedHotCold(&market, hotCold) && market.SimulationNote == "" {
					market.SimulationNote = "冷热指数为本地模拟（欧赔降幅+亚盘热度推算），非竞彩官方数据"
				}
			}
		}
		items = append(items, market)

		if source.Key != "sporttery" {
			continue
		}

		// 竞彩比赛官方盘之外再并排输出一份模拟盘（独立 key，不参与警示/舒服项计算），
		// 用于对比模拟算法与真实竞彩数据的差距。
		if !marketSimulated {
			if simMarket, ok := buildSimulatedSpfCompareMarket(response, &market); ok {
				items = append(items, simMarket)
			}
		}

		if rqspfMarket, ok := buildSportteryRqspfMarket(trade); ok {
			items = append(items, rqspfMarket)
			if simRqspf, ok := buildSimulatedRqspfMarket(response); ok {
				decorateSimulatedRqspfCompare(&simRqspf, &rqspfMarket, trade)
				items = append(items, simRqspf)
			}
		} else if simulatedRqspf, ok := buildSimulatedRqspfMarket(response); ok {
			// 非竞彩比赛：按亚盘规律模拟竞彩让球盘（让1/2/3球）并结算盈亏。
			items = append(items, simulatedRqspf)
		}
	}
	return items
}

func buildBookmakerMarket(source bookmakerOddsSource, retailDistribution [3]float64, retailAvailable bool) bookmakerMarketResponse {
	sourceOddsAvailable := oddsAvailable(source.Odds)
	return bookmakerMarketResponse{
		Key:                source.Key,
		Name:               source.Name,
		CompanyID:          source.CompanyID,
		Goal:               source.Goal,
		Odds:               directionValuesFromArray(source.Odds),
		OddsAvailable:      sourceOddsAvailable,
		RetailDistribution: directionValuesFromArray(retailDistribution),
		BookmakerByOutcome: buildBookmakerOutcomes(source, retailDistribution, sourceOddsAvailable && retailAvailable),
	}
}

func buildBookmakerOutcomes(source bookmakerOddsSource, retailDistribution [3]float64, available bool) []bookmakerOutcomeResponse {
	items := make([]bookmakerOutcomeResponse, 0, len(bookmakerOutcomeKeys))
	for index := range bookmakerOutcomeKeys {
		betStake := bookmakerTotalStake * retailDistribution[index] / 100
		item := bookmakerOutcomeResponse{
			Outcome:      bookmakerOutcomeKeys[index],
			OutcomeLabel: bookmakerOutcomeLabels[index],
			RetailShare:  round2(retailDistribution[index]),
			BetStake:     round2(betStake),
			TotalStake:   bookmakerTotalStake,
			Odds:         round2(source.Odds[index]),
			Available:    available,
		}
		if available {
			item.Payout = round2(betStake * source.Odds[index])
			item.BookmakerProfit = round2(bookmakerTotalStake - item.Payout)
			item.BookmakerLoss = round2(item.Payout - bookmakerTotalStake)
			item.BookmakerRoi = round2(item.BookmakerProfit / bookmakerTotalStake * 100)
			item.BookmakerOutcome = bookmakerOutcomeLabel(item.BookmakerProfit)
		}
		items = append(items, item)
	}
	return items
}

func buildSportteryRqspfMarket(trade sportteryTradeData) (bookmakerMarketResponse, bool) {
	odds := [3]float64{trade.JyykRqspf.H, trade.JyykRqspf.D, trade.JyykRqspf.A}
	distribution, distributionAvailable := sportteryRqspfDistribution(trade)
	if !oddsAvailable(odds) && !distributionAvailable {
		return bookmakerMarketResponse{}, false
	}

	source := bookmakerOddsSource{
		Key:  "sportteryRqspf",
		Name: sportteryRqspfName(trade.JyykRqspf.Goal),
		Odds: odds,
		Goal: trade.JyykRqspf.Goal,
	}
	market := buildBookmakerMarket(source, distribution, distributionAvailable)
	decorateSportteryRqspfMarket(&market, trade)
	return market, true
}

func buildSportteryBettingRatioRows(trade sportteryTradeData) []bookmakerOutcomeResponse {
	distribution, distributionAvailable := percentValues([3]float64{trade.Tzbl.HSupportRate, trade.Tzbl.DSupportRate, trade.Tzbl.ASupportRate})
	odds := [3]float64{trade.Tzbl.H, trade.Tzbl.D, trade.Tzbl.A}
	if !oddsAvailable(odds) && !distributionAvailable {
		return nil
	}

	source := bookmakerOddsSource{Key: "sportteryRatio", Name: "竞彩投注比例", Odds: odds}
	items := buildBookmakerOutcomes(source, distribution, oddsAvailable(odds) && distributionAvailable)
	probabilities := [3]float64{trade.Tzbl.HProbability, trade.Tzbl.DProbability, trade.Tzbl.AProbability}
	errors := [3]float64{trade.Tzbl.HError, trade.Tzbl.DError, trade.Tzbl.AError}
	for index := range items {
		items[index].Probability = roundedFloatPointer(probabilities[index])
		items[index].Error = roundedFloatPointer(errors[index])
	}
	return items
}

func decorateSportterySpfMarket(market *bookmakerMarketResponse, trade sportteryTradeData) {
	market.PsychologyError = roundedFloatPointer(trade.Tzbl.PsyError)
	market.PsychologyErrorLabel = psychologyErrorLabel(trade.Tzbl.PsyError)
	market.BettingRatio = buildSportteryBettingRatioRows(trade)
	if !sportterySpfAvailable(trade.JyykSpf) {
		return
	}
	profitRates := [3]float64{trade.JyykSpf.HY, trade.JyykSpf.DY, trade.JyykSpf.AY}
	hotColdIndexes := [3]float64{trade.JyykSpf.HLR, trade.JyykSpf.DLR, trade.JyykSpf.ALR}
	for index := range market.BookmakerByOutcome {
		market.BookmakerByOutcome[index].OfficialProfitRate = roundedFloatPointer(profitRates[index])
		market.BookmakerByOutcome[index].HotColdIndex = roundedFloatPointer(hotColdIndexes[index])
	}
}

func decorateSportteryRqspfMarket(market *bookmakerMarketResponse, trade sportteryTradeData) {
	profitRates := [3]float64{trade.JyykRqspf.HY, trade.JyykRqspf.DY, trade.JyykRqspf.AY}
	for index := range market.BookmakerByOutcome {
		market.BookmakerByOutcome[index].OfficialProfitRate = roundedFloatPointer(profitRates[index])
	}
}

func sportterySpfDistribution(trade sportteryTradeData) ([3]float64, bool) {
	return percentValues([3]float64{trade.JyykSpf.HSupportRate, trade.JyykSpf.DSupportRate, trade.JyykSpf.ASupportRate})
}

func sportteryRqspfDistribution(trade sportteryTradeData) ([3]float64, bool) {
	return percentValues([3]float64{trade.JyykRqspf.HSupportRate, trade.JyykRqspf.DSupportRate, trade.JyykRqspf.ASupportRate})
}

func sportterySpfAvailable(profit sportteryProfitSpf) bool {
	return oddsAvailable([3]float64{profit.H, profit.D, profit.A}) || hasPercentValues([3]float64{profit.HSupportRate, profit.DSupportRate, profit.ASupportRate}) || hasNonZeroValue([3]float64{profit.HY, profit.DY, profit.AY}) || hasNonZeroValue([3]float64{profit.HLR, profit.DLR, profit.ALR})
}

func sportteryRqspfName(goal string) string {
	goal = strings.TrimSpace(goal)
	if goal == "" {
		return "竞彩让球"
	}
	goal = strings.TrimSuffix(strings.TrimRight(goal, "0"), ".")
	return "竞彩让球(" + goal + ")"
}

func percentValues(values [3]float64) ([3]float64, bool) {
	total := values[0] + values[1] + values[2]
	if total <= 0 {
		return [3]float64{}, false
	}
	return [3]float64{
		round2(values[0]),
		round2(values[1]),
		round2(values[2]),
	}, true
}

func hasPercentValues(values [3]float64) bool {
	_, ok := percentValues(values)
	return ok
}

func hasNonZeroValue(values [3]float64) bool {
	for _, value := range values {
		if value != 0 {
			return true
		}
	}
	return false
}

func oddsAvailable(odds [3]float64) bool {
	return odds[0] > 0 && odds[1] > 0 && odds[2] > 0
}

func bookmakerOutcomeLabel(profit float64) string {
	if profit > 0 {
		return "庄家盈利"
	}
	if profit < 0 {
		return "庄家亏损"
	}
	return "庄家持平"
}

func psychologyErrorLabel(value float64) string {
	absolute := math.Abs(value)
	if absolute < 0.01 {
		return "正常"
	}
	if absolute >= 2 {
		return "误差大"
	}
	if absolute >= 1 {
		return "有误差"
	}
	return "误差小"
}

func directionValuesFromArray(values [3]float64) directionValues {
	return directionValues{Home: round2(values[0]), Draw: round2(values[1]), Away: round2(values[2])}
}

func roundedFloatPointer(value float64) *float64 {
	rounded := round2(value)
	return &rounded
}
