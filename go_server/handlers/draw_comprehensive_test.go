package handlers

import (
	"encoding/json"
	"reflect"
	"testing"

	"go_server/internal/drawfullmodel"
)

func TestComprehensiveUnavailableCannotChangeOriginalFields(t *testing.T) {
	original := drawObservationItem{MatchID: "same-fixture", JingcaiID: "20261104001", HomeScore: 0, AwayScore: 1,
		FrozenDrawSignal: &drawObservationFrozenSignal{State: "selected", ModelVersion: "original", Score: drawTestPtr(.3)}}
	before, _ := json.Marshal(original)
	for _, input := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"result":2,"homeScore":0,"awayScore":19}`), json.RawMessage(`invalid`)} {
		signal := drawObservationComprehensiveSignal(input)
		if signal == nil || signal.State != "unavailable" || signal.Reason == "" {
			t.Fatalf("missing or untrained comprehensive input must be explicit: %+v", signal)
		}
	}
	after, _ := json.Marshal(original)
	if string(before) != string(after) {
		t.Fatal("independent comprehensive availability changed original selection/result")
	}
}

func TestComprehensiveSignalIsRawTargetBlind(t *testing.T) {
	if !drawfullmodel.Info().Available {
		t.Skip("training artifact not generated yet; runtime pure-tree target blindness is tested separately")
	}
	input := map[string]any{
		"matchId": "independent-fixture", "matchTime": "2026-11-04T12:15:00Z",
		"analysisInputs": map[string]any{"averageLatest": []float64{3.76, 3.76, 1.78}, "averageOpening": []float64{3.63, 3.51, 1.84}},
		"frontend":       map[string]any{"averageEuroOdds": []float64{3.76, 3.76, 1.78}},
	}
	body, _ := json.Marshal(input)
	before := drawObservationComprehensiveSignal(body)
	if before.State == "unavailable" {
		t.Fatal("real average odds must allow the new independent model to decide")
	}
	input["result"], input["homeScore"], input["awayScore"], input["status"], input["jingcaiId"] = 2, 0, 19, -1, "20261104001"
	input["frozenDrawSignal"], input["secondaryDecision"] = map[string]any{"state": "not_selected"}, map[string]any{"decision": "rejected"}
	body, _ = json.Marshal(input)
	after := drawObservationComprehensiveSignal(body)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("old selection, lottery scope or target outcome changed the independent model")
	}
}
