package handlers

import (
	"math"
	"strings"
)

const drawObservationProfitSupportVersion = "profit-support-v1"
const drawObservationProfitSupportEpsilon = 1e-9

// This is an observation label, not a promotion or exclusion of model picks.
// Rates retain source provenance: official percentages are not reconstructed
// from local amounts, and simulation percentages are not official observations.
type drawObservationProfitSupportSignal struct {
	Version          string      `json:"version"`
	State            string      `json:"state"`
	OfficialRates    [3]*float64 `json:"officialRates"`
	SimulatedRates   [3]*float64 `json:"simulatedRates"`
	OfficialMinimum  []string    `json:"officialMinimum"`
	SimulatedMinimum []string    `json:"simulatedMinimum"`
	Reason           string      `json:"reason"`
}

func drawObservationProfitSupport(row drawObservationItem) drawObservationProfitSupportSignal {
	result := drawObservationProfitSupportSignal{Version: drawObservationProfitSupportVersion, State: "unavailable", OfficialMinimum: []string{}, SimulatedMinimum: []string{}}
	if strings.TrimSpace(row.JingcaiID) == "" {
		result.State, result.Reason = "out_of_scope", "仅观察有竞彩编号的比赛"
		return result
	}
	analysis := row.FrontendAnalysis
	if analysis == nil || analysis.WeightedFeatureInputs == nil {
		result.Reason = "缺少真实竞彩源站盈亏率，待判"
		return result
	}
	// WeightedFeatureInputs preserves raw field presence. A legacy market's
	// default zero pointer must never stand in for a missing source observation.
	for i, value := range analysis.WeightedFeatureInputs.OfficialProfitRates {
		if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
			result.Reason = "竞彩源站三项盈亏率不完整，待判"
			return result
		}
		copy := *value
		result.OfficialRates[i] = &copy
	}
	if analysis.RoiSimulation == nil {
		result.Reason = "缺少非让球模拟盈亏，待判"
		return result
	}
	candidates := []bookmakerMarketResponse{}
	for _, market := range analysis.RoiSimulation.Markets {
		if market.Simulated && market.Key != "sportteryRqspf" && market.Key != "sportteryRqspfSim" {
			candidates = append(candidates, market)
		}
	}
	if len(candidates) != 1 || !candidates[0].OddsAvailable {
		result.Reason = "缺少唯一可用的非让球模拟盈亏，待判"
		return result
	}
	directions := []string{"home", "draw", "away"}
	for i, direction := range directions {
		matches := []bookmakerOutcomeResponse{}
		for _, outcome := range candidates[0].BookmakerByOutcome {
			if outcome.Outcome == direction {
				matches = append(matches, outcome)
			}
		}
		if len(matches) != 1 {
			result.Reason = "模拟盈亏方向缺失或重复，待判"
			return result
		}
		outcome := matches[0]
		if !outcome.Available || outcome.Odds <= 0 || math.IsNaN(outcome.Odds) || math.IsInf(outcome.Odds, 0) || outcome.TotalStake <= 0 || math.IsNaN(outcome.TotalStake) || math.IsInf(outcome.TotalStake, 0) || math.IsNaN(outcome.BookmakerProfit) || math.IsInf(outcome.BookmakerProfit, 0) {
			result.Reason = "模拟盈亏三项金额、赔率或测算基数不足，待判"
			return result
		}
		value := outcome.BookmakerProfit * 100 / outcome.TotalStake
		if math.IsNaN(value) || math.IsInf(value, 0) {
			result.Reason = "模拟盈亏率无效，待判"
			return result
		}
		result.SimulatedRates[i] = &value
	}
	minimum := func(values [3]*float64) []string {
		low := math.Min(*values[0], math.Min(*values[1], *values[2]))
		selected := []string{}
		for i, value := range values {
			if *value <= low+drawObservationProfitSupportEpsilon {
				selected = append(selected, directions[i])
			}
		}
		return selected
	}
	result.OfficialMinimum = minimum(result.OfficialRates)
	result.SimulatedMinimum = minimum(result.SimulatedRates)
	for _, official := range result.OfficialMinimum {
		for _, simulated := range result.SimulatedMinimum {
			if official == simulated {
				result.State, result.Reason = "not_supported", "两套最低盈亏方向有共同选项，未匹配增强标签"
				return result
			}
		}
	}
	result.State, result.Reason = "supported", "两套最低盈亏方向没有交集，匹配盈亏方向分歧标签"
	return result
}

type drawObservationProfitSupportMetrics struct {
	Version               string                     `json:"version"`
	Status                string                     `json:"status"`
	SecondaryComparable   drawObservationMetricGroup `json:"secondaryComparable"`
	SecondarySupported    drawObservationMetricGroup `json:"secondarySupported"`
	SecondaryNotSupported drawObservationMetricGroup `json:"secondaryNotSupported"`
	SecondaryUnavailable  drawObservationMetricGroup `json:"secondaryUnavailable"`
}

func (metrics *drawObservationProfitSupportMetrics) add(row drawObservationItem) {
	signal := drawObservationProfitSupport(row)
	switch signal.State {
	case "supported":
		metrics.SecondaryComparable.add(row)
		metrics.SecondarySupported.add(row)
	case "not_supported":
		metrics.SecondaryComparable.add(row)
		metrics.SecondaryNotSupported.add(row)
	default:
		metrics.Status = "incomplete"
		metrics.SecondaryUnavailable.add(row)
	}
}

func (metrics *drawObservationProfitSupportMetrics) rates(draws int, secondaryKnown bool) {
	// No comparable observations means an unknown feature coverage, not a
	// measured zero. Unknown rows remain in the existing eligible draw base.
	comparable := metrics.SecondaryComparable.Selected > 0
	metrics.SecondaryComparable.rates(draws, secondaryKnown && comparable)
	metrics.SecondarySupported.rates(draws, secondaryKnown && comparable)
	metrics.SecondaryNotSupported.rates(draws, secondaryKnown && comparable)
	metrics.SecondaryUnavailable.rates(draws, secondaryKnown)
}
