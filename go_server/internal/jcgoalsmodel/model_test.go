package jcgoalsmodel

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

type parityCase struct {
	Name              string     `json:"name"`
	Values            []*float64 `json:"values"`
	JQProbabilities   []float64  `json:"jqProbabilities"`
	Bet365            Market     `json:"bet365"`
	TotalScores       [8]float64 `json:"totalScores"`
	Direction         string     `json:"direction"`
	Score             float64    `json:"score"`
	ExpectedNetReturn float64    `json:"expectedNetReturn"`
	OverEV            float64    `json:"overEV"`
	UnderEV           float64    `json:"underEV"`
}

type parityData struct {
	FeatureNames []string     `json:"featureNames"`
	Cases        []parityCase `json:"cases"`
}

func readParity(t *testing.T) parityData {
	t.Helper()
	body, err := os.ReadFile("testdata/parity_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var data parityData
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func parityInput(data parityData, c parityCase) Input {
	values := map[string]float64{}
	for i, v := range c.Values {
		if v != nil {
			values[data.FeatureNames[i]] = *v
		}
	}
	return Input{Features: values, JQProbabilities: append([]float64(nil), c.JQProbabilities...), Bet365: c.Bet365}
}

// These expected distributions and decisions are produced by the original
// sklearn HGB + JQ pipeline, not by this Go implementation. The 634 real inputs
// cover the complete research comparison pool. Three added inputs independently
// exercise missing openings, missing recent-10 features and training medians.
// The fixture contains no actual match scores or target goal labels.
func TestPredictParityWithPythonReference(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	data := readParity(t)
	if len(data.Cases) != 637 {
		t.Fatalf("expected complete 634-row reference plus 3 missing-input cases, got %d", len(data.Cases))
	}
	if len(m.FeatureNames()) != 50 || len(data.FeatureNames) != 50 {
		t.Fatal("unexpected feature contract")
	}
	quarters, tailModes, missing, negativeEV, lowScore := 0, 0, 0, 0, 0
	for _, c := range data.Cases {
		input := parityInput(data, c)
		got, err := m.Predict(input)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if got.Direction != c.Direction || got.ModelVersion != Version || got.Line != c.Bet365.Line || got.OverWater != c.Bet365.OverWater || got.UnderWater != c.Bet365.UnderWater {
			t.Fatalf("%s: direction/identity mismatch: %+v", c.Name, got)
		}
		assertNear(t, c.Name+" score", got.Score, c.Score)
		assertNear(t, c.Name+" net return", got.ExpectedNetReturn, c.ExpectedNetReturn)
		assertNear(t, c.Name+" over EV", got.OverEV, c.OverEV)
		assertNear(t, c.Name+" under EV", got.UnderEV, c.UnderEV)
		sum, mode := 0.0, 0
		for class, score := range got.TotalScores {
			assertNear(t, c.Name+" class", score, c.TotalScores[class])
			if !finite(score) || score <= 0 {
				t.Fatalf("%s: invalid total class score", c.Name)
			}
			sum += score
			if score > got.TotalScores[mode] {
				mode = class
			}
		}
		assertNear(t, c.Name+" sum", sum, 1)
		if got.ObservedFeatures != len(input.Features) || got.TotalFeatures != 50 {
			t.Fatalf("%s: missingness was not preserved", c.Name)
		}
		if int(math.Round(got.Line*4))%2 != 0 {
			quarters++
		}
		if mode == 7 {
			tailModes++
		}
		if got.ObservedFeatures < 50 {
			missing++
		}
		if got.ExpectedNetReturn < 0 {
			negativeEV++
		}
		if got.Score < .55 {
			lowScore++
		}
	}
	if quarters == 0 || tailModes == 0 || missing != 3 || negativeEV == 0 || lowScore == 0 {
		t.Fatalf("reference lacks required coverage: quarters=%d tail=%d missing=%d negativeEV=%d lowScore=%d", quarters, tailModes, missing, negativeEV, lowScore)
	}
}

func assertNear(t *testing.T, name string, got, want float64) {
	t.Helper()
	if !finite(got) || math.Abs(got-want) > 2e-12 {
		t.Fatalf("%s: got %.17g want %.17g", name, got, want)
	}
}

func TestStakeFractionsQuarterSettlementAndTail(t *testing.T) {
	for _, c := range []struct {
		goals     int
		line      float64
		direction string
		win, lose float64
	}{
		{2, 2.25, "under", .5, 0}, {2, 2.25, "over", 0, .5},
		{3, 2.75, "over", .5, 0}, {3, 2.75, "under", 0, .5},
		{2, 2, "over", 0, 0}, {2, 2, "under", 0, 0},
		{0, .25, "over", 0, .5}, {0, .25, "under", .5, 0},
		{7, 6.5, "over", 1, 0}, {7, 6.5, "under", 0, 1},
	} {
		w, l := stakeFractions(c.goals, c.line, c.direction)
		if w != c.win || l != c.lose {
			t.Fatalf("%+v: got %v/%v", c, w, l)
		}
	}
	for quarters := 1; quarters <= 26; quarters++ {
		line := float64(quarters) / 4
		for goals := 0; goals <= 20; goals++ {
			w, l := stakeFractions(goals, line, "over")
			uw, ul := stakeFractions(goals, line, "under")
			if w != ul || l != uw || w+l > 1 {
				t.Fatal("over/under stake fractions are inconsistent")
			}
			if goals >= 7 {
				tw, tl := stakeFractions(7, line, "over")
				if w != tw || l != tl {
					t.Fatal("7+ class changes settlement on a supported line")
				}
			}
		}
	}
}

func TestQuarterToleranceNormalizesSettlementAndReturnedLine(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	data := readParity(t)
	base := parityInput(data, data.Cases[0])
	for _, c := range []struct{ raw, snapped float64 }{
		{2.00000000001, 2}, {1.99999999999, 2},
		{2.25000000001, 2.25}, {2.24999999999, 2.25},
	} {
		input := base
		input.Bet365.Line = c.snapped
		want, err := m.Predict(input)
		if err != nil {
			t.Fatal(err)
		}
		input.Bet365.Line = c.raw
		got, err := m.Predict(input)
		if err != nil {
			t.Fatalf("valid quarter representation %.14g: %v", c.raw, err)
		}
		if got.Line != c.snapped || got.Direction != want.Direction {
			t.Fatalf("line was not normalized: %.14g -> %+v", c.raw, got)
		}
		assertNear(t, "normalized over return", got.OverEV, want.OverEV)
		assertNear(t, "normalized under return", got.UnderEV, want.UnderEV)
		assertNear(t, "normalized winning stake share", got.Score, want.Score)
		for _, side := range []string{"over", "under"} {
			win, lose := stakeFractions(2, c.raw, side)
			wantWin, wantLose := stakeFractions(2, c.snapped, side)
			if win != wantWin || lose != wantLose {
				t.Fatalf("near-quarter stake settlement changed: %.14g %s", c.raw, side)
			}
		}
	}
	for _, line := range []float64{2.000000003, 2.249999997, 0.00000000001} {
		input := base
		input.Bet365.Line = line
		if _, err := m.Predict(input); err == nil {
			t.Fatalf("accepted quarter deviation outside tolerance or a zero-normalizing line: %.14g", line)
		}
	}
}

func TestPredictRejectsInvalidSourceValues(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	data := readParity(t)
	base := parityInput(data, data.Cases[0])
	for _, line := range []float64{0, -.25, 2.3, 6.75, 7, math.NaN(), math.Inf(1)} {
		input := base
		input.Bet365.Line = line
		if _, err := m.Predict(input); err == nil {
			t.Fatalf("accepted invalid line %v", line)
		}
	}
	for _, water := range []float64{0, -.1, 2.01, math.NaN(), math.Inf(1)} {
		input := base
		input.Bet365.OverWater = water
		if _, err := m.Predict(input); err == nil {
			t.Fatalf("accepted invalid over water %v", water)
		}
		input = base
		input.Bet365.UnderWater = water
		if _, err := m.Predict(input); err == nil {
			t.Fatalf("accepted invalid under water %v", water)
		}
	}
	for _, values := range [][]float64{nil, {1, 0, 0, 0, 0, 0, 0, 0}, {.1, .1, .1, .1, .1, .1, .1, .1}, {.1, .1, .1, .1, .1, .1, .1}, {math.NaN(), .1, .1, .1, .1, .1, .1, .3}} {
		input := base
		input.JQProbabilities = values
		if _, err := m.Predict(input); err == nil {
			t.Fatalf("accepted invalid JQ distribution %v", values)
		}
	}
	var absent *Model
	if _, err := absent.Predict(base); err == nil {
		t.Fatal("nil model must be unavailable")
	}
}

func TestNonfinitePredictorUsesSameMedianAsMissing(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	data := readParity(t)
	input := parityInput(data, data.Cases[0])
	name := m.FeatureNames()[0]
	delete(input.Features, name)
	want, err := m.Predict(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		input.Features[name] = v
		got, err := m.Predict(input)
		if err != nil {
			t.Fatal(err)
		}
		assertNear(t, "missing predictor median", got.Score, want.Score)
		if got.ObservedFeatures != want.ObservedFeatures {
			t.Fatal("nonfinite predictor counted as observed")
		}
	}
}

func TestLoadRejectsInvalidArtifactsAndDoesNotExposeFeatureMutation(t *testing.T) {
	m, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	names := m.FeatureNames()
	names[0] = "actualGoals"
	if m.FeatureNames()[0] == "actualGoals" || len(ArtifactSHA256()) != 64 {
		t.Fatal("model metadata is mutable or hash unavailable")
	}
	if _, err := LoadJSON([]byte("{")); err == nil {
		t.Fatal("accepted malformed JSON")
	}
	mutations := []func(*artifact){
		func(a *artifact) { a.Version = "unexpected" },
		func(a *artifact) { a.Protocol.FeatureNames[0] = "actualGoals" },
		func(a *artifact) { a.Protocol.FeatureNames[1] = a.Protocol.FeatureNames[0] },
		func(a *artifact) { a.Chosen.AlphaJQ = .9 },
		func(a *artifact) { a.Classifier.Classes[0] = a.Classifier.Classes[1] },
		func(a *artifact) { a.Classifier.Iterations[0][0][0].Left = 0 },
		func(a *artifact) { a.Classifier.Iterations[0][0][0].Feature = 50 },
		func(a *artifact) { a.Classifier.Iterations[0][0][0].Categorical = true },
	}
	for i, mutate := range mutations {
		var a artifact
		if err := json.Unmarshal(modelJSON, &a); err != nil {
			t.Fatal(err)
		}
		mutate(&a)
		body, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := LoadJSON(body); err == nil {
			t.Fatalf("accepted invalid artifact mutation %d", i)
		}
	}
}
