package handlers

import (
	"reflect"
	"testing"
	"time"
)

func TestDrawFrozenSignalScopeMissingAndResultIndependence(t *testing.T) {
	item := drawObservationItem{MatchID: "new-live-match", JingcaiID: "周一001", MatchTime: time.Date(2026, 10, 26, 18, 0, 0, 0, drawSourceLocation)}
	if got := drawObservationModelSignal(item, nil); got.State != "unavailable" {
		t.Fatalf("missing inputs: %+v", got)
	}
	input := drawFeatureStudyRow{}
	if got := drawObservationModelSignal(item, &input); got.State != "unavailable" {
		t.Fatalf("empty input must not create selection: %+v", got)
	}
	input.AnalysisInputs.AverageOpening = []float64{2.1, 3.1, 3.3}
	input.AnalysisInputs.AverageLatest = []float64{2.2, 3.0, 3.1}
	before := drawObservationModelSignal(item, &input)
	if before.Score == nil || (before.State != "selected" && before.State != "not_selected") {
		t.Fatalf("numeric inputs not scored: %+v", before)
	}
	item.HomeScore, item.AwayScore, item.Settled, item.Status = 9, 1, true, 999
	settled := true
	item.RegulationSettled = &settled
	after := drawObservationModelSignal(item, &input)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("target result changed model state: %+v -> %+v", before, after)
	}
	item.MatchID = "another-never-seen-id"
	if got := drawObservationModelSignal(item, &input); !reflect.DeepEqual(before, got) {
		t.Fatal("selection uses target ID instead of records")
	}
	item.JingcaiID = "   "
	if got := drawObservationModelSignal(item, &input); got.State != "out_of_scope" || got.Score != nil {
		t.Fatalf("scope not enforced: %+v", got)
	}
}
