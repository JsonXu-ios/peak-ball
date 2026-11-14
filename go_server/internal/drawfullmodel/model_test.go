package drawfullmodel

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func runtimeFixture(t *testing.T) *Model {
	t.Helper()
	data := artifact{Schema: 1, Version: "test-independent", InputVersion: InputVersion,
		Features: []InputFeature{
			{Name: "euro.average.latest.odds.home", Median: 2, Label: "即时主均赔", Module: "odds"},
			{Name: "full.profits.simulated.away", Median: 0, Label: "模拟负盈利", Module: "profits"},
			{Name: "availability.any", Median: 0, Label: "可用标记", Module: "availability"},
		},
		Draw: decisionTree{Threshold: .6, Nodes: []treeNode{
			{Feature: 0, Threshold: 2, Left: 1, Right: 2},
			{Leaf: true, Value: .6, Samples: 10, Counts: []float64{4, 6}},
			{Leaf: true, Value: .2, Samples: 20, Counts: []float64{16, 4}},
		}},
		Alternative: decisionTree{Nodes: []treeNode{
			{Feature: 1, Threshold: 0, Left: 1, Right: 2},
			{Leaf: true, Value: .2, Samples: 30, Counts: []float64{24, 6}},
			{Leaf: true, Value: .8, Samples: 40, Counts: []float64{8, 32}},
		}}, Metadata: json.RawMessage(`{"chronologicalValidation":true}`),
	}
	body, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	model, err := Load(body)
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func TestRuntimeIndependentHeadsAndInclusiveThresholds(t *testing.T) {
	model := runtimeFixture(t)
	result := model.Predict(map[string]float64{"euro.average.latest.odds.home": 2, "full.profits.simulated.away": 5})
	if !result.Available || !result.Selected || result.Score != .6 || result.AlternativeDirection != "away" || result.AwayScore != .8 || math.Abs(result.HomeScore-.2) > 1e-12 {
		t.Fatalf("independent draw and side decisions were not preserved: %+v", result)
	}
	if !result.DrawPath[0].WentLeft || result.DrawPath[0].Imputed || result.DrawLeafSamples != 10 || result.AlternativeLeafSamples != 40 {
		t.Fatal("split equality, observed path, or training leaf samples differ")
	}
	// The side tree can choose away despite a low home price. No minimum-price
	// backup or existing selection gate is imported into this new head.
	changed := model.Predict(map[string]float64{"euro.average.latest.odds.home": 2.000001, "full.profits.simulated.away": 5})
	if changed.Selected || changed.AlternativeDirection != "away" || changed.Score != .2 {
		t.Fatal("draw head and side head unexpectedly coupled")
	}
}

func TestRuntimeMissingGuardsAndMedianAreExplicit(t *testing.T) {
	model := runtimeFixture(t)
	result := model.Predict(map[string]float64{"euro.average.latest.odds.home": 1.8})
	if !result.Available || !result.Selected || result.AlternativeDirection != "home" || !result.AlternativePath[0].Imputed || result.AlternativePath[0].Value != nil || result.AlternativePath[0].UsedValue != 0 {
		t.Fatalf("partial missing input did not use explicit training median: %+v", result)
	}
	model.artifact.Features[0].Guards = []FeatureGuard{{Feature: "safeRecent.home.5.n", Minimum: 5}}
	for _, sample := range []float64{0, 4} {
		result = model.Predict(map[string]float64{"euro.average.latest.odds.home": 1.8, "safeRecent.home.5.n": sample})
		if result.Available {
			t.Fatal("sample denominator alone or a failed guard became match evidence")
		}
	}
	if result = model.Predict(map[string]float64{"euro.average.latest.odds.home": 1.8, "safeRecent.home.5.n": 5}); !result.Available {
		t.Fatal("valid guarded source was rejected")
	}
}

func TestRuntimeMatchesSklearnFloat32Inference(t *testing.T) {
	model := runtimeFixture(t)
	model.artifact.Draw.Nodes[0].Threshold = 16777216.5
	// float32(16777217) rounds to 16777216, so sklearn takes the left branch.
	// Comparing the original float64 would instead take the wrong right leaf.
	result := model.Predict(map[string]float64{"euro.average.latest.odds.home": 16777217})
	if !result.Selected || !result.DrawPath[0].WentLeft || result.DrawPath[0].UsedValue != 16777216 || result.DrawPath[0].Value == nil || *result.DrawPath[0].Value != 16777217 {
		t.Fatalf("sklearn comparison precision or raw explanation differed: %+v", result)
	}
}

func TestRuntimeEmptyFlagsAndNonfiniteNeverSelect(t *testing.T) {
	model := runtimeFixture(t)
	for _, input := range []map[string]float64{
		nil, {}, {"availability.any": 1}, {"full.profits.simulated.away": 5},
		{"euro.average.latest.odds.home": math.NaN()}, {"euro.average.latest.odds.home": math.Inf(1)},
	} {
		result := model.Predict(input)
		if result.Available || result.Selected || result.ObservedEvidence != 0 || len(result.DrawPath) != 0 {
			t.Fatalf("empty or indicator-only fixture produced a selection: %+v", result)
		}
	}
}

func TestRuntimeTieAbstainsAndTargetsCannotChangeDecisions(t *testing.T) {
	model := runtimeFixture(t)
	model.artifact.Alternative.Nodes[2].Value = .5
	input := map[string]float64{"euro.average.latest.odds.home": 1.8, "full.profits.simulated.away": 5}
	before := model.Predict(input)
	if !before.Selected || before.AlternativeDirection != "" || before.AlternativeReason == "" {
		t.Fatal("equal side leaves must abstain without losing the draw selection")
	}
	input["result"], input["homeScore"], input["awayScore"], input["status"] = 2, 0, 19, -1
	if after := model.Predict(input); !reflect.DeepEqual(before, after) {
		t.Fatal("target outcomes or unrelated status changed the trees")
	}
}

func TestRuntimeRejectsBrokenArtifactsWithoutPanicking(t *testing.T) {
	model := runtimeFixture(t)
	for _, mutate := range []func(*artifact){
		func(a *artifact) { a.Schema = 99 },
		func(a *artifact) { a.InputVersion += "-other" },
		func(a *artifact) { a.Features[1].Name = a.Features[0].Name },
		func(a *artifact) { a.Draw.Nodes = nil },
		func(a *artifact) { a.Draw.Nodes[0].Left = 0 },
		func(a *artifact) { a.Draw.Nodes[0].Right = 200 },
		func(a *artifact) { a.Draw.Nodes[0].Feature = 99 },
		func(a *artifact) { a.Draw.Nodes[1].Value = 1.1 },
		func(a *artifact) { a.Draw.Nodes[1].Samples = 0 },
	} {
		body, _ := json.Marshal(model.artifact)
		var changed artifact
		if err := json.Unmarshal(body, &changed); err != nil {
			t.Fatal(err)
		}
		mutate(&changed)
		body, _ = json.Marshal(changed)
		if _, err := Load(body); err == nil {
			t.Fatal("invalid experimental artifact was accepted")
		}
	}
	for _, body := range [][]byte{nil, []byte("invalid"), []byte(`{"schema":1}`)} {
		if _, err := Load(body); err == nil {
			t.Fatal("incomplete experimental artifact was accepted")
		}
	}
}
