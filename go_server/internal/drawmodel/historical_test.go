package drawmodel

import (
	"bytes"
	"fmt"
	"math"
	"testing"
	"time"
)

func historicalExamples(n int) []HistoricalExample {
	start := testTime().Add(-time.Duration(n+10) * 24 * time.Hour)
	examples := make([]HistoricalExample, n)
	for i := range examples {
		kickoff := start.Add(time.Duration(i) * 24 * time.Hour)
		p := .18 + float64(i%7)*.025
		form := Form{N: 10, SmoothedDrawRate: .2 + float64(i%4)*.04, LowScoreRate: .5}
		base := Result{Available: true, Probability: p, ScoreModelProbability: p, HomeLambda: 1, AwayLambda: 1.2, HomeOverall: form, AwayOverall: form, HomeVenue: form, AwayVenue: form, LeagueBaseline: Baseline{N: 100, DrawRate: .27}}
		examples[i] = HistoricalExample{Target: Match{ID: fmt.Sprint(i), HomeTeamID: "home", AwayTeamID: "away", Kickoff: kickoff}, AsOf: kickoff.Add(-time.Hour), Draw: i%4 == 0, Base: &base}
	}
	return examples
}

func findHistoricalComparison(e HistoricalEvaluation, name string) HistoricalComparison {
	for _, comparison := range e.Models {
		if comparison.Name == name {
			return comparison
		}
	}
	return HistoricalComparison{}
}

func TestHistoricalSmallDataFitsAndSeparatesThreePeriods(t *testing.T) {
	evaluation := FitHistorical(historicalExamples(200))
	if len(evaluation.Rows) != 200 || evaluation.Split.TrainingSamples != 120 || evaluation.Split.CalibrationSamples != 40 || evaluation.Split.TestSamples != 40 {
		t.Fatalf("unexpected historical split: %+v", evaluation.Split)
	}
	if evaluation.Artifact == nil || evaluation.Artifact.SelectedModel == "score_model" {
		t.Fatal("historical fit incorrectly requires 400 future frozen rows")
	}
	if evaluation.Split.CommonTestSamples != 40 {
		t.Fatalf("missing market data destroyed score-only common holdout: %+v", evaluation.Split)
	}
	for i, row := range evaluation.Rows {
		if row.Probability == nil {
			t.Fatalf("row %d lacks its history collision probability", i)
		}
		if i < 160 && (row.OutOfTime || !row.Exploratory) {
			t.Fatalf("training/calibration row claimed independent validation: %+v", row)
		}
		if i >= 160 && (!row.OutOfTime || row.Exploratory) {
			t.Fatalf("held-out row not marked out of time: %+v", row)
		}
	}
	comparison := findHistoricalComparison(evaluation, "score_recent")
	if !comparison.Fitted || !comparison.Calibrated || comparison.Test.Samples != 40 || comparison.CommonTest.Samples != 40 {
		t.Fatalf("historical ablation unavailable: %+v", comparison)
	}
	if comparison.Test.Top20Selected != 8 || len(comparison.Test.CalibrationBins) == 0 {
		t.Fatalf("holdout collision/ranking/calibration metrics missing: %+v", comparison.Test)
	}
}

func TestHistoricalTestLabelsCannotChangeArtifactOrThreshold(t *testing.T) {
	examples := historicalExamples(200)
	before := FitHistorical(examples)
	for i := 160; i < len(examples); i++ {
		examples[i].Draw = !examples[i].Draw
	}
	after := FitHistorical(examples)
	a, err := HistoricalArtifactJSON(before.Artifact)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HistoricalArtifactJSON(after.Artifact)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("held-out outcomes changed fitted parameters, model selection, or calibration threshold")
	}
	if findHistoricalComparison(before, "score_model").Test.DrawRate == findHistoricalComparison(after, "score_model").Test.DrawRate {
		t.Fatal("test labels did not affect evaluation")
	}
}

func TestHistoricalTemporalPurgeAndExcludedRows(t *testing.T) {
	examples := historicalExamples(200)
	for i := range examples {
		examples[i].AsOf = examples[i].Target.Kickoff.Add(-3 * 24 * time.Hour)
	}
	// Each earlier period has three results still unavailable when the next
	// period's first prediction is made; none may enter that period's fit.
	evaluation := FitHistorical(examples)
	if evaluation.Split.PurgedSamples != 6 || evaluation.Split.TrainingSamples != 117 || evaluation.Split.CalibrationSamples != 37 {
		t.Fatalf("overlapping label windows were not purged: %+v", evaluation.Split)
	}
	for _, model := range evaluation.Artifact.Models {
		if model.TrainingCutoff.After(examples[160].AsOf) {
			t.Fatalf("artifact contains labels unavailable at test prediction: %+v", model)
		}
	}
	for _, index := range []int{117, 118, 119, 157, 158, 159} {
		if evaluation.Rows[index].Partition != "excluded" || evaluation.Rows[index].OutOfTime {
			t.Fatalf("purged row %d incorrectly labeled: %+v", index, evaluation.Rows[index])
		}
	}
	examples = append(examples, examples[0], HistoricalExample{})
	if got := FitHistorical(examples); len(got.Rows) != 202 || got.Split.ExcludedSamples != 8 {
		t.Fatalf("invalid/duplicate rows lost or counted twice: %+v", got.Split)
	}
}

func TestHistoricalExploratoryOpeningAndExistingAnalysisNeverActivate(t *testing.T) {
	examples := historicalExamples(240)
	goalLine, handicap := 2.25, -.25
	for i := range examples {
		examples[i].Odds = &Odds{Home: 2.1, Draw: 3.1 + float64(i%3)*.1, Away: 3.5, GoalLine: &goalLine, AsianHandicap: &handicap}
		examples[i].OddsProvenance = "opening_reconstructed"
		examples[i].Features = map[string]float64{"existing_score": float64(i % 3)}
		examples[i].Exploratory = true
	}
	evaluation := FitHistorical(examples)
	for _, name := range []string{"score_market", "score_market_lines", "existing_analysis"} {
		comparison := findHistoricalComparison(evaluation, name)
		if !comparison.Fitted || !comparison.Exploratory || comparison.Test.Samples != 48 {
			t.Fatalf("existing data not available for honest exploratory comparison: %+v", comparison)
		}
		if evaluation.Artifact.SelectedModel == name {
			t.Fatalf("unverifiable historical group activated: %s", name)
		}
		if !evaluation.Rows[239].GroupExploratory[name] {
			t.Fatalf("unverifiable historical row claimed clean validation: %s", name)
		}
	}
}

func TestHistoricalSameSampleComparisonAndInsufficientClasses(t *testing.T) {
	examples := historicalExamples(200)
	for i := range examples {
		if i%2 == 0 {
			examples[i].Odds = &Odds{Home: 2, Draw: 3, Away: 4, CapturedAt: examples[i].AsOf}
		}
	}
	evaluation := FitHistorical(examples)
	if evaluation.Split.CommonTestSamples != 20 || findHistoricalComparison(evaluation, "score_model").Test.Samples != 40 || findHistoricalComparison(evaluation, "score_model").CommonTest.Samples != 20 {
		t.Fatalf("groups were compared on different samples: %+v", evaluation.Split)
	}
	for i := range examples {
		examples[i].Draw = false
	}
	evaluation = FitHistorical(examples)
	if findHistoricalComparison(evaluation, "score_calibrated").Fitted || evaluation.Artifact.SelectedModel != "score_model" {
		t.Fatal("single-class data fabricated a fitted/calibrated model")
	}
	if len(evaluation.Rows) != 200 || evaluation.Rows[199].Probability == nil {
		t.Fatal("insufficient calibration blocked useful full history replay")
	}
}

func TestPredictHistoricalChecksAvailabilityAndParameterIntegrity(t *testing.T) {
	evaluation := FitHistorical(historicalExamples(200))
	artifact := evaluation.Artifact
	in := testInput()
	base := Predict(in)
	got := PredictHistorical(in, artifact)
	if !got.Model.FusionFitted || !got.Model.Calibrated || got.Probability <= 0 || got.Probability >= 1 {
		t.Fatalf("historical artifact was not applied: %+v", got)
	}
	model := artifact.Models[artifact.SelectedModel]
	model.TrainingCutoff = in.AsOf.Add(time.Hour)
	artifact.Models[artifact.SelectedModel] = model
	if result := PredictHistorical(in, artifact); result.Model.FusionFitted || result.Probability != base.Probability {
		t.Fatal("future historical labels entered prediction")
	}
	model.TrainingCutoff = in.AsOf.Add(-time.Hour)
	model.Coefficients[0] = math.NaN()
	artifact.Models[artifact.SelectedModel] = model
	if PredictHistorical(in, artifact).Model.FusionFitted {
		t.Fatal("corrupt historical parameters accepted")
	}
}

func TestHistoricalLateSparseAnalysisUsesIndependentExploratoryPeriods(t *testing.T) {
	examples := historicalExamples(1000)
	line, handicap := 2.25, .25
	for i := 800; i < len(examples); i++ {
		examples[i].Odds = &Odds{Home: 2, Draw: 3 + float64(i%5)*.1, Away: 4, GoalLine: &line, AsianHandicap: &handicap}
		examples[i].OddsProvenance = "opening_reconstructed"
		examples[i].Exploratory = true
		// Neither value has complete coverage. Requiring all numeric fields
		// would throw out every one of these usable analysis rows.
		if i%2 == 0 {
			examples[i].Features = map[string]float64{"recent": float64(i % 3)}
		} else {
			examples[i].Features = map[string]float64{"goals": float64(i % 5)}
		}
		examples[i].Features["signalShare_draw"] = float64(i%3) / 3
		examples[i].Features["signalShare_home"] = .3
		examples[i].Features["signalTriggeredCount"] = float64(i%5 + 1)
	}
	evaluation := FitHistorical(examples)
	for _, name := range []string{"score_market", "score_market_lines", "existing_analysis", "existing_signal_votes"} {
		comparison := findHistoricalComparison(evaluation, name)
		if !comparison.Fitted || !comparison.Calibrated || !comparison.Exploratory || !comparison.IndependentSplit || comparison.TrainingSamples != 120 || comparison.CalibrationSamples != 40 || comparison.Test.Samples != 40 {
			t.Fatalf("late available data was not used in its own honest experiment: %+v", comparison)
		}
		if evaluation.Rows[810].Partition != "test" || evaluation.Rows[810].GroupPartitions[name] != "train" || evaluation.Rows[970].GroupPartitions[name] != "test" {
			t.Fatalf("main/independent periods were conflated for %s", name)
		}
		if evaluation.Artifact.SelectedModel == name || !evaluation.Rows[970].GroupExploratory[name] {
			t.Fatalf("independent exploratory experiment activated for %s", name)
		}
	}
	if evaluation.Split.CommonTestSamples != 40 || len(evaluation.Artifact.Models["existing_analysis"].ImputationValues) != 2 {
		t.Fatalf("sparse imputation or independent test intersection missing: %+v", evaluation.Split)
	}
	before, _ := HistoricalArtifactJSON(evaluation.Artifact)
	for i := 960; i < 1000; i++ {
		examples[i].Draw = !examples[i].Draw
		// A new feature discovered solely in an independent test must not
		// change its trained schema, imputation, coefficients or threshold.
		examples[i].Features["new_test_only_feature"] = 100
	}
	after := FitHistorical(examples)
	encoded, _ := HistoricalArtifactJSON(after.Artifact)
	if !bytes.Equal(before, encoded) {
		t.Fatal("independent test outcomes or new schema contaminated the artifact")
	}
}
