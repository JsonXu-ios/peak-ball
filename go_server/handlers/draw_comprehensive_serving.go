package handlers

import (
	"encoding/json"
	"go_server/internal/drawfullmodel"
)

func drawComprehensiveServingVersion() string {
	info := drawfullmodel.Info()
	return info.Version + ":" + info.InputVersion + ":" + drawfullmodel.ArtifactSHA256()
}

// The market batch is shared, but safe history comes from this exact list row.
// Only the independent new projection is patched; original inputs stay intact.
func drawComprehensiveSignalForItem(raw json.RawMessage, item drawObservationItem) *drawComprehensiveSignal {
	var data map[string]json.RawMessage
	if json.Unmarshal(raw, &data) != nil || data == nil {
		return drawObservationComprehensiveSignal(nil)
	}
	data["matchId"], _ = json.Marshal(item.MatchID)
	data["matchTime"], _ = json.Marshal(item.MatchTime)
	data["preMatchHistory"], _ = json.Marshal(drawFeatureStudyPreMatchHistory{HomeRecent: item.HomeRecent, AwayRecent: item.AwayRecent, HeadToHead: item.HeadToHead})
	encoded, err := json.Marshal(data)
	if err != nil {
		return drawObservationComprehensiveSignal(nil)
	}
	return drawObservationComprehensiveSignal(encoded)
}

// New artifacts upgrade a durable day once. This is independent of the source
// revision, old model version and all existing candidate/metric namespaces.
func drawComprehensiveStoredDayReady(saved drawObservationStoredDay) bool {
	if len(saved.Items) == 0 {
		return true
	}
	if saved.ComprehensiveVersion != drawComprehensiveServingVersion() {
		return false
	}
	info := drawfullmodel.Info()
	for _, item := range saved.Items {
		signal := item.ComprehensiveSignal
		if signal == nil || signal.ModelVersion != info.Version || signal.InputVersion != info.InputVersion {
			return false
		}
		if signal.State != "selected" && signal.State != "not_selected" && signal.State != "unavailable" {
			return false
		}
	}
	return true
}
