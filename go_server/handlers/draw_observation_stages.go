package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"go_server/models"
)

type drawObservationComparisonMetric struct {
	drawObservationPredictionState
	Result []string `json:"result"`
	Basis  string   `json:"basis"`
}

type drawObservationMiddleSample struct {
	Prices []float64 `json:"prices"`
	Index  int       `json:"index"`
	Total  int       `json:"total"`
	Time   string    `json:"time"`
}

type drawObservationComparisonStage struct {
	Key            string                          `json:"key"`
	Label          string                          `json:"label"`
	Note           string                          `json:"note"`
	Kelly          drawObservationComparisonMetric `json:"kelly"`
	Ticai          drawObservationComparisonMetric `json:"ticai"`
	WilliamAverage drawObservationComparisonMetric `json:"williamAverage"`
	William        []float64                       `json:"william"`
	Sporttery      []float64                       `json:"sporttery"`
	Average        []float64                       `json:"average"`
	MiddleSample   *drawObservationMiddleSample    `json:"middleSample,omitempty"`
}

func drawObservationStageDifference(a, b []float64, basis, missing string) drawObservationComparisonMetric {
	metric := drawObservationComparisonMetric{Result: []string{}, Basis: basis}
	if !drawObservationValidPrices(a) || !drawObservationValidPrices(b) {
		metric.Reason = missing
		return metric
	}
	metric.Values = make([]float64, 3)
	abs := make([]float64, 3)
	for i := range metric.Values {
		metric.Values[i] = a[i] - b[i]
		abs[i] = math.Abs(metric.Values[i])
	}
	metric.Available, metric.Result = true, labelsForSmallestDiffs(abs, 0)
	return metric
}

// Raw records are used here so numeric quote text and partial source records
// cannot accidentally make an opening comparison borrow the latest prices.
func drawObservationOpeningAverage(odds models.OddsMoney, rows []map[string]json.RawMessage) []float64 {
	var stored map[string]json.RawMessage
	_ = json.Unmarshal(odds.AvgOdds, &stored)
	if prices := drawObservationRawPrices(drawObservationRawOdds(stored["firstOdds"], 3)); prices != nil {
		return prices
	}
	for _, row := range rows {
		if drawRawID(row["companyId"]) == "" || strings.Contains(drawRawString(row["companyName"]), "平均") {
			if prices := drawObservationRawPrices(drawObservationRawOdds(row["firstOdds"], 3)); prices != nil {
				return prices
			}
		}
	}
	sums, counts := [3]float64{}, [3]int{}
	for _, row := range rows {
		if drawRawID(row["companyId"]) == "" || strings.Contains(drawRawString(row["companyName"]), "平均") {
			continue
		}
		for i, raw := range drawObservationRawOdds(row["firstOdds"], 3) {
			if raw == nil {
				continue
			}
			value := parseFloat(*raw)
			if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
				sums[i] += value
				counts[i]++
			}
		}
	}
	for _, count := range counts {
		if count == 0 {
			return nil
		}
	}
	return []float64{sums[0] / float64(counts[0]), sums[1] / float64(counts[1]), sums[2] / float64(counts[2])}
}

func drawObservationOpeningKelly(odds models.OddsMoney, rows []map[string]json.RawMessage, average []float64) drawObservationComparisonMetric {
	metric := drawObservationComparisonMetric{Result: []string{}}
	if !drawObservationValidPrices(average) {
		metric.Reason = "缺少完整有效的平均欧赔初盘"
		return metric
	}
	var preferred, bet365 map[string]json.RawMessage
	_ = json.Unmarshal(odds.Pinnacle, &preferred)
	_ = json.Unmarshal(odds.Bet365, &bet365)
	find := func(id string) map[string]json.RawMessage {
		for _, row := range rows {
			if drawRawID(row["companyId"]) == id {
				return row
			}
		}
		return nil
	}
	for _, row := range []map[string]json.RawMessage{preferred, find("16"), bet365, find("281")} {
		prices := drawObservationRawPrices(drawObservationRawOdds(row["firstOdds"], 3))
		if prices == nil {
			continue
		}
		values := make([]float64, 3)
		for i := range values {
			values[i] = prices[i] / average[i] * returnRatioFromOdds(average)
		}
		if !drawObservationValidPrices(values) {
			continue
		}
		metric.Available, metric.Values, metric.Result = true, values, labelsForSmallestDiffs(values, 0)
		metric.Basis = drawObservationEuroSource(analysisEuroOdd{CompanyID: drawRawID(row["companyId"]), CompanyName: drawRawString(row["companyName"])}) + "初盘赔率 ÷ 平均欧赔初盘 × 初盘平均返还率，取最小方向"
		return metric
	}
	metric.Reason = "缺少完整有效的凯利来源公司初盘"
	return metric
}

// Source order is the order used for official opening/latest prices. Select
// the earlier middle record for an even count; never reuse either endpoint.
func drawObservationSportteryMiddle(raw json.RawMessage) *drawObservationMiddleSample {
	var payload struct {
		Code *int `json:"code"`
		Data struct {
			Spf struct {
				Odds []map[string]json.RawMessage `json:"odds"`
			} `json:"spf"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Code == nil || *payload.Code != 0 {
		return nil
	}
	valid := []drawObservationMiddleSample{}
	for _, row := range payload.Data.Spf.Odds {
		prices := drawObservationRawPrices([]*string{drawObservationRawLine(row["spf3"]), drawObservationRawLine(row["spf1"]), drawObservationRawLine(row["spf0"])})
		if prices != nil {
			valid = append(valid, drawObservationMiddleSample{Prices: prices, Time: drawRawString(row["updateTime"])})
		}
	}
	if len(valid) < 3 {
		return nil
	}
	index := (len(valid) - 1) / 2
	sample := valid[index]
	sample.Index, sample.Total = index+1, len(valid)
	return &sample
}

func drawObservationComparisonStages(odds models.OddsMoney, latest drawObservationPredictionSet) []drawObservationComparisonStage {
	rows, _ := drawObservationEuroRows(odds.Data)
	william, sporttery := drawObservationCompanyEuro(odds, "william", "威廉"), drawObservationCompanyEuro(odds, "sporttery", "竞彩")
	opening := drawObservationComparisonStage{Key: "opening", Label: "初盘", Note: "各公司首个有效初盘分别比较，不混用即时盘；初盘报价时间可能不同。", William: drawObservationRawPrices(william.Opening), Sporttery: drawObservationRawPrices(sporttery.Opening), Average: drawObservationOpeningAverage(odds, rows)}
	opening.Kelly = drawObservationOpeningKelly(odds, rows, opening.Average)
	opening.Ticai = drawObservationStageDifference(opening.William, opening.Sporttery, "威廉初盘－竞彩初盘，取绝对差最小方向", "缺少完整有效的威廉或竞彩初盘")
	opening.WilliamAverage = drawObservationStageDifference(opening.William, opening.Average, "威廉初盘－平均欧赔初盘", "缺少完整有效的威廉或平均欧赔初盘")
	middle := drawObservationComparisonStage{Key: "middle", Label: "中间盘", Note: "按源站顺序取有效变动记录中间一条；偶数取靠前一条，至少需 3 条有效记录。", MiddleSample: drawObservationSportteryMiddle(sportteryRecordedBody(odds.SportteryTrade))}
	middle.Kelly = drawObservationComparisonMetric{drawObservationPredictionState: drawObservationPredictionState{Reason: "缺少凯利来源公司和平均欧赔的真实中间变动记录"}, Result: []string{}}
	middle.Ticai = drawObservationStageDifference(nil, nil, "威廉真实中间盘－竞彩真实中间盘", "缺少威廉真实中间变动记录；不能计算中间段体彩差值")
	middle.WilliamAverage = drawObservationStageDifference(nil, nil, "威廉真实中间盘－真实中间平均欧赔", "缺少威廉和平均欧赔的真实中间变动记录")
	if middle.MiddleSample != nil {
		middle.Sporttery = middle.MiddleSample.Prices
		middle.Note += fmt.Sprintf(" 竞彩取第 %d/%d 条有效记录；不同公司须有同阶段记录才可比较。", middle.MiddleSample.Index, middle.MiddleSample.Total)
	} else {
		middle.Note += " 竞彩暂无独立中间记录。"
	}
	avg, _, _ := resolveAverageOdds(odds)
	if !drawObservationValidPrices(avg) {
		avg = nil
	}
	current := drawObservationComparisonStage{Key: "latest", Label: "即时盘", Note: "使用本地已采集的最新报价。", William: drawObservationRawPrices(william.Latest), Sporttery: drawObservationRawPrices(sporttery.Latest), Average: avg,
		Kelly: drawObservationComparisonMetric{drawObservationPredictionState: latest.Availability.Kelly, Result: latest.Kelly, Basis: latest.KellyBasis},
		Ticai: drawObservationComparisonMetric{drawObservationPredictionState: latest.Availability.Ticai, Result: latest.Ticai, Basis: latest.TicaiBasis}}
	current.WilliamAverage = drawObservationStageDifference(current.William, current.Average, "威廉即时盘－平均欧赔即时盘", "缺少完整有效的威廉或平均欧赔即时盘")
	return []drawObservationComparisonStage{opening, middle, current}
}
