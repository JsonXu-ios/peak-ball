package handlers

import "strings"

// A response/day-result contract independent of lottery inference, research
// archives and the shared parsed-history cache namespace.
const drawObservationNonSportteryVersion = "non-sporttery-observation-v1"

func drawObservationNonSportterySignal(item drawObservationItem, input *drawFeatureStudyRow) *drawObservationFrozenSignal {
	if strings.TrimSpace(item.JingcaiID) != "" {
		return nil
	}
	return drawObservationFrozenModelSignal(item, input)
}

// Upgrade only persisted days containing non-lottery fixtures. The existing
// global result namespace/base/history and lottery-only days remain reusable.
// An unavailable signal is a completed calculation, not an upgrade failure.
func drawNonSportteryStoredDayReady(day drawObservationStoredDay) bool {
	for _, row := range day.Items {
		if strings.TrimSpace(row.JingcaiID) != "" {
			continue
		}
		if day.NonSportteryVersion != drawObservationNonSportteryVersion || row.NonSportterySignal == nil {
			return false
		}
		switch row.NonSportterySignal.State {
		case "selected", "not_selected", "unavailable":
		default:
			return false
		}
	}
	return true
}
