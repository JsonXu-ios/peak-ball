package handlers

import (
	"strings"

	"go_server/internal/drawmodeldata"
)

// Counts include the complete query window, before tab/count/pagination filters.
// A selected row enters rate denominators only after confirmed regulation time.
type drawObservationMetricGroup struct {
	Selected  int      `json:"selected"`
	Settled   int      `json:"settled"`
	Hits      int      `json:"hits"`
	Misses    int      `json:"misses"`
	Pending   int      `json:"pending"`
	Precision *float64 `json:"precision"`
	Coverage  *float64 `json:"coverage"`
}

type drawObservationMetrics struct {
	Status               string                               `json:"status"`
	ModelVersion         string                               `json:"modelVersion"`
	SecondaryVersion     string                               `json:"secondaryVersion"`
	SecondaryThreshold   float64                              `json:"secondaryThreshold"`
	ScopeMatches         int                                  `json:"scopeMatches"`
	SettledMatches       int                                  `json:"settledMatches"`
	SettledDraws         int                                  `json:"settledDraws"`
	EligibleMatches      int                                  `json:"eligibleMatches"`
	EligibleSettled      int                                  `json:"eligibleSettled"`
	EligibleDraws        int                                  `json:"eligibleDraws"`
	ModelAvailable       int                                  `json:"modelAvailable"`
	ModelUnavailable     int                                  `json:"modelUnavailable"`
	OutOfScope           int                                  `json:"outOfScope"`
	SecondaryUnavailable int                                  `json:"secondaryUnavailable"`
	Original             drawObservationMetricGroup           `json:"original"`
	Secondary            drawObservationMetricGroup           `json:"secondary"`
	Rejected             drawObservationMetricGroup           `json:"rejected"`
	ProfitSupport        *drawObservationProfitSupportMetrics `json:"profitSupport,omitempty"`
}

func drawObservationMetricWindowMatches(row drawObservationItem, q drawObservationQuery) bool {
	if !drawObservationDateMatches(row.Date, q) || q.scope == "sporttery" && strings.TrimSpace(row.JingcaiID) == "" {
		return false
	}
	if q.league != "" && q.league != "all" && row.League != q.league {
		return false
	}
	search := strings.ToLower(strings.TrimSpace(q.search))
	return search == "" || strings.Contains(strings.ToLower(row.Home+" "+row.Guest+" "+row.MatchID), search)
}

func drawObservationMetricSettled(row drawObservationItem) bool {
	return row.Settled && row.RegulationSettled != nil && *row.RegulationSettled && row.HomeScore >= 0 && row.AwayScore >= 0
}

func (group *drawObservationMetricGroup) add(row drawObservationItem) {
	group.Selected++
	if !drawObservationMetricSettled(row) {
		group.Pending++
		return
	}
	group.Settled++
	if row.HomeScore == row.AwayScore {
		group.Hits++
	} else {
		group.Misses++
	}
}

func (group *drawObservationMetricGroup) rates(draws int, modelAvailable bool) {
	if group.Settled > 0 {
		value := float64(group.Hits) / float64(group.Settled)
		group.Precision = &value
	}
	if draws > 0 && modelAvailable {
		value := float64(group.Hits) / float64(draws)
		group.Coverage = &value
	}
}

// The persisted day already contains every original score. Deriving metrics
// here does not query MySQL, rebuild the model, or change any Redis key format.
func buildDrawObservationMetrics(items []drawObservationItem, q drawObservationQuery) *drawObservationMetrics {
	result := &drawObservationMetrics{Status: "available", ModelVersion: drawmodeldata.Version(), SecondaryVersion: drawObservationSecondaryVersion, SecondaryThreshold: drawObservationSecondaryThreshold, ProfitSupport: &drawObservationProfitSupportMetrics{Version: drawObservationProfitSupportVersion, Status: "available"}}
	for _, row := range items {
		if !drawObservationMetricWindowMatches(row, q) {
			continue
		}
		result.ScopeMatches++
		settled := drawObservationMetricSettled(row)
		if settled {
			result.SettledMatches++
			if row.HomeScore == row.AwayScore {
				result.SettledDraws++
			}
		}
		// The frozen production model only applies to locally marked sporttery
		// fixtures. Other fixtures are counted separately, never as missed draws.
		if strings.TrimSpace(row.JingcaiID) == "" {
			result.OutOfScope++
			continue
		}
		result.EligibleMatches++
		if settled {
			result.EligibleSettled++
			if row.HomeScore == row.AwayScore {
				result.EligibleDraws++
			}
		}
		signal := row.FrozenDrawSignal
		if signal == nil || (signal.State != "selected" && signal.State != "not_selected") {
			result.ModelUnavailable++
			continue
		}
		result.ModelAvailable++
		if signal.State != "selected" {
			continue
		}
		result.Original.add(row)
		secondary := drawObservationSecondaryDecision(row)
		if secondary.Decision == "unavailable" {
			result.SecondaryUnavailable++
			continue
		}
		if secondary.Decision == "retained" {
			result.Secondary.add(row)
			result.ProfitSupport.add(row)
		} else if secondary.Decision == "rejected" {
			result.Rejected.add(row)
		}
	}
	if result.ModelUnavailable > 0 || result.SecondaryUnavailable > 0 {
		result.Status = "incomplete"
	}
	result.Original.rates(result.EligibleDraws, result.ModelAvailable > 0)
	secondaryKnown := result.ModelAvailable > 0 && (result.Original.Selected == 0 || result.SecondaryUnavailable < result.Original.Selected)
	result.Secondary.rates(result.EligibleDraws, secondaryKnown)
	result.Rejected.rates(result.EligibleDraws, secondaryKnown)
	result.ProfitSupport.rates(result.EligibleDraws, secondaryKnown)
	return result
}
