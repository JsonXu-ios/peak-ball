package handlers

import (
	"encoding/json"
	"reflect"
	"testing"

	"go_server/internal/drawfullmodel"
	"go_server/internal/drawmodeldata"
	"go_server/models"
)

func TestComprehensiveProjectionDoesNotChangeOriginalInputOrDecision(t *testing.T) {
	row := drawFeatureStudyRow{MatchID: "separate", HomeScore: 1, AwayScore: 1, Result: 1}
	row.AnalysisInputs.TradeFields = map[string]*float64{}
	before, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	response := analysisMatchResponse{MatchID: "separate", HomeScore: 1, GuestScore: 1, Prediction: "客胜", Confidence: "高信心", GoddessWoman: &goddessWomanResponse{Prediction: "平局", Probabilities: directionValues{Home: 27, Draw: 34, Away: 39}}}
	response.Platform = &platformDecision{Bookmaker: platformGuidePrediction{Outcome: "away", Score: "1:3"}, Platform: platformGuidePrediction{Outcome: "home", Score: "2:1"}}
	full, err := BuildDrawComprehensiveInput(&row, &response)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(row)
	if string(before) != string(after) {
		t.Fatal("building new projection mutated original frozen input")
	}
	base, err := drawmodeldata.ExtractFeatures(before)
	if err != nil {
		t.Fatal(err)
	}
	other, err := drawmodeldata.ExtractFeatures(full)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base, other) || !reflect.DeepEqual(drawmodeldata.Predict(base), drawmodeldata.Predict(other)) {
		t.Fatal("adding separate full input changed original frozen features or prediction")
	}
	var decoded map[string]json.RawMessage
	if err = json.Unmarshal(full, &decoded); err != nil {
		t.Fatal(err)
	}
	var projected map[string]json.RawMessage
	if err = json.Unmarshal(decoded["fullAnalysis"], &projected); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"matchId", "home", "guest", "homeScore", "guestScore", "awayScore", "result", "status", "predictionHit", "accuracyFit", "myAngle"} {
		if _, exists := projected[key]; exists {
			t.Fatal("full analysis leaked target or fixture identity", key)
		}
	}
	if _, exists := projected["goddessWoman"]; !exists {
		t.Fatal("omitted previously missing goddess module")
	}
	// The snapshot is independent of further changes to a shared response.
	response.GoddessWoman.Prediction = "主胜"
	var same map[string]json.RawMessage
	_ = json.Unmarshal(full, &same)
	if string(same["fullAnalysis"]) != string(decoded["fullAnalysis"]) {
		t.Fatal("input retained references to mutable analysis response")
	}
}

func TestComprehensiveRebuiltFrontendIsBlindToCurrentFixtureResult(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	project := func(target models.Money) map[string]float64 {
		t.Helper()
		enrichment := buildDrawObservationFrontend(target, history, odds, pankou)
		row := drawFeatureStudyBuildRowWithEnrichment(target, history, odds, pankou, enrichment)
		input, err := BuildDrawComprehensiveInput(&row, enrichment.studyResponse)
		if err != nil {
			t.Fatal(err)
		}
		features, err := drawfullmodel.ExtractFeatures(input)
		if err != nil {
			t.Fatal(err)
		}
		return features
	}
	before := project(match)
	if len(before) < 200 {
		t.Fatal("fixture no longer exercises comprehensive market and derived features")
	}
	for _, scores := range [][2]int{{0, 0}, {17, 23}, {9, 1}} {
		changed := match
		changed.HomeScore, changed.GuestScore = scores[0], scores[1]
		changed.Status, changed.MatchState, changed.DisplayState = 999, 999, "未开始"
		changed.HomeOtScore, changed.GuestOtScore, changed.HomeOtPenalty, changed.GuestOtPenalty = 3, 2, 5, 4
		if !reflect.DeepEqual(before, project(changed)) {
			t.Fatal("rebuilding page analysis from changed current score or state changed new model inputs")
		}
	}
}
