package handlers

import (
	"math"
	"strings"

	"go_server/models"
)

type drawObservationPredictionAvailability struct {
	Kelly drawObservationPredictionState `json:"kelly"`
	Ticai drawObservationPredictionState `json:"ticai"`
}

type drawObservationPredictionState struct {
	Available bool      `json:"available"`
	Reason    string    `json:"reason"`
	Values    []float64 `json:"values"`
}

type drawObservationPredictionSet struct {
	Kelly, Ticai           []string
	KellyBasis, TicaiBasis string
	Availability           drawObservationPredictionAvailability
}

func drawObservationValidPrices(values []float64) bool {
	if len(values) < 3 {
		return false
	}
	for _, value := range values[:3] {
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func drawObservationRawPrices(values []*string) []float64 {
	if len(values) < 3 {
		return nil
	}
	result := make([]float64, 3)
	for i, raw := range values[:3] {
		if raw == nil {
			return nil
		}
		result[i] = parseFloat(*raw)
	}
	if !drawObservationValidPrices(result) {
		return nil
	}
	return result
}

// The observation module never substitutes a different model when the inputs
// of a named prediction are missing. H5's other analysis rules are unchanged.
func drawObservationPredictions(odds models.OddsMoney) drawObservationPredictionSet {
	result := drawObservationPredictionSet{Kelly: []string{}, Ticai: []string{}}
	avg, rows, _ := resolveAverageOdds(odds)
	var source analysisEuroOdd
	var sourcePrices []float64
	for _, candidate := range []analysisEuroOdd{jsonValue[analysisEuroOdd](odds.Pinnacle), findEuroOdd(rows, "16", ""), jsonValue[analysisEuroOdd](odds.Bet365), findEuroOdd(rows, "281", "")} {
		prices := euroOddValues(candidate)
		if drawObservationValidPrices(prices[:]) {
			source, sourcePrices = candidate, prices[:]
			break
		}
	}
	if !drawObservationValidPrices(avg) {
		result.Availability.Kelly.Reason = "缺少完整、有效的平均欧赔"
	} else if !drawObservationValidPrices(sourcePrices) {
		result.Availability.Kelly.Reason = "缺少完整、有效的凯利来源公司即时赔率"
	} else {
		values := make([]float64, 3)
		for i := range values {
			values[i] = sourcePrices[i] / avg[i] * returnRatioFromOdds(avg)
		}
		if drawObservationValidPrices(values) {
			result.Availability.Kelly.Values = values
			result.Kelly = labelsForSmallestDiffs(values, 0)
			result.Availability.Kelly.Available = true
			result.KellyBasis = drawObservationEuroSource(source) + "即时赔率 ÷ 平均欧赔 × 平均返还率，取凯利值最小方向；并列优先负、平、胜。此项不使用竞彩赔率"
		} else {
			result.Availability.Kelly.Reason = "凯利计算数值异常"
		}
	}
	sporttery := drawObservationRawPrices(drawObservationCompanyEuro(odds, "sporttery", "竞彩").Latest)
	william := drawObservationRawPrices(drawObservationCompanyEuro(odds, "william", "威廉").Latest)
	missing := []string{}
	if sporttery == nil {
		missing = append(missing, "竞彩")
	}
	if william == nil {
		missing = append(missing, "威廉")
	}
	if len(missing) > 0 {
		result.Availability.Ticai.Reason = "缺少" + strings.Join(missing, "、") + "完整有效的即时赔率"
	} else {
		diffs := make([]float64, 3)
		signed := make([]float64, 3)
		for i := range diffs {
			signed[i] = william[i] - sporttery[i]
			diffs[i] = math.Abs(signed[i])
		}
		result.Availability.Ticai.Values = signed
		result.Ticai = labelsForSmallestDiffs(diffs, 0)
		result.Availability.Ticai.Available = true
		result.TicaiBasis = "威廉与竞彩真实即时赔率逐项相减，取绝对差最小方向；并列优先负、平、胜"
	}
	if !result.Availability.Kelly.Available {
		result.KellyBasis = result.Availability.Kelly.Reason + "，未计算"
	}
	if !result.Availability.Ticai.Available {
		result.TicaiBasis = result.Availability.Ticai.Reason + "，未计算"
	}
	return result
}
