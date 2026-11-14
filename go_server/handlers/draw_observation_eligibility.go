package handlers

import (
	"fmt"
	"strings"
	"time"
)

// This serving gate is separate from the frozen numeric model and its weights.
const drawObservationSelectionVersion = "jc-h2h-required-v1"
const drawObservationMinimumHeadToHead = 1

// H2H membership/identity is resolved by the canonical history index. Recheck
// actual records rather than accepting a stale or summary-only sample count.
func drawObservationHeadToHeadSample(row drawObservationItem) int {
	if row.MatchTime.IsZero() {
		return 0
	}
	seen := map[string]bool{}
	count := 0
	for _, match := range row.HeadToHead.Matches {
		id := strings.TrimSpace(match.MatchID)
		if id != "" && id == strings.TrimSpace(row.MatchID) || match.MatchTime.IsZero() || !match.MatchTime.Add(2*time.Hour).Before(row.MatchTime) || match.HomeScore < 0 || match.AwayScore < 0 {
			continue
		}
		gf, ga := match.HomeScore, match.AwayScore
		switch match.Venue {
		case "home":
		case "away":
			gf, ga = ga, gf
		default:
			continue
		}
		result := "draw"
		if gf > ga {
			result = "win"
		} else if gf < ga {
			result = "loss"
		}
		if match.Result != result {
			continue
		}
		if id == "" {
			id = fmt.Sprintf("%s|%s|%d", match.Home, match.Guest, match.MatchTime.UnixNano())
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		count++
	}
	return count
}

// Apply after both numeric-model and archived-research reads, so neither can
// bypass the current eligibility policy. Never mutate a shared archived signal.
func drawObservationApplySelectionEligibility(row drawObservationItem, signal *drawObservationFrozenSignal) *drawObservationFrozenSignal {
	if strings.TrimSpace(row.JingcaiID) == "" || signal == nil {
		return signal
	}
	copy := *signal
	copy.SelectionVersion = drawObservationSelectionVersion
	copy.MinimumHeadToHead = drawObservationMinimumHeadToHead
	copy.HeadToHeadSample = drawObservationHeadToHeadSample(row)
	if copy.State == "selected" && copy.HeadToHeadSample < drawObservationMinimumHeadToHead {
		copy.State = "not_selected"
		copy.Reason = "无有效赛前交锋记录，未达到至少1场交锋的入选条件"
	}
	return &copy
}

// Upgrade only affected persisted days, leaving the parsed-history cache and
// non-lottery results reusable. Unavailable model inputs are completed states.
func drawObservationSelectionStoredDayReady(day drawObservationStoredDay) bool {
	for _, row := range day.Items {
		if strings.TrimSpace(row.JingcaiID) == "" {
			continue
		}
		signal := row.FrozenDrawSignal
		if day.SelectionVersion != drawObservationSelectionVersion || signal == nil || signal.SelectionVersion != drawObservationSelectionVersion || signal.MinimumHeadToHead != drawObservationMinimumHeadToHead || signal.HeadToHeadSample != drawObservationHeadToHeadSample(row) {
			return false
		}
		if signal.State == "selected" && signal.HeadToHeadSample < drawObservationMinimumHeadToHead {
			return false
		}
	}
	return true
}
