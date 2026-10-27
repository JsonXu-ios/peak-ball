package drawmodeldata

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestFrozenRuntimeAgainstSavedPythonModel(t *testing.T) {
	data, err := os.ReadFile("testdata.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string             `json:"name"`
		Features map[string]float64 `json:"features"`
		Score    float64            `json:"score"`
		Selected bool               `json:"selected"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 17 {
		t.Fatalf("expected 17 frozen fixtures, got %d", len(fixtures))
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			got := Predict(fixture.Features)
			if math.Abs(got.Score-fixture.Score) > 1e-12 || got.Selected != fixture.Selected {
				t.Fatalf("got %+v; want score %.17g selected %v", got, fixture.Score, fixture.Selected)
			}
			if got.UsedFeatures != 202 || got.ObservedFeatures > got.UsedFeatures {
				t.Fatalf("unexpected observation counts %+v", got)
			}
		})
	}
}

func TestFrozenModelContract(t *testing.T) {
	if Version() != "sporttery-draw-observation-v1" || ModelID() != "clean_d3_l40_i240" || Threshold() != 0.2016984853563883 {
		t.Fatal("unexpected frozen model identity or threshold")
	}
	features := Features()
	if len(features) != 391 || len(UsedFeatureNames()) != 202 {
		t.Fatal("unexpected input or used feature count")
	}
	features[0].Name = "mutated"
	if Features()[0].Name == "mutated" {
		t.Fatal("Features must not expose mutable shared metadata")
	}
	for _, tree := range frozen.Trees {
		for index, current := range tree {
			if current.Leaf {
				if !finite(current.Value) {
					t.Fatal("nonfinite leaf")
				}
				continue
			}
			if current.Feature < 0 || current.Feature >= len(features) || current.Left <= index || current.Right <= index || current.Left >= len(tree) || current.Right >= len(tree) || !finite(current.Threshold) {
				t.Fatal("invalid numeric tree structure")
			}
		}
	}
}

func TestUnknownAndInsufficientHistoryStayMissing(t *testing.T) {
	missing := Predict(nil)
	if missing.ObservedFeatures != 0 {
		t.Fatal("missing input falsely counted observed")
	}
	input := map[string]float64{}
	for _, feature := range frozen.Features {
		input[feature.Name] = math.NaN()
		if len(feature.Guards) > 0 {
			input[feature.Name] = 1e20
		}
	}
	got := Predict(input)
	if got != missing {
		t.Fatalf("unknown guards must fall back to same medians: %+v vs %+v", got, missing)
	}
}

// Opt-in offline reproduction uses the research matrix without embedding IDs,
// outcomes or a large historical dataset into the production application.
func TestOfflineAllRowsReference(t *testing.T) {
	path := os.Getenv("DRAW_MODEL_REFERENCE")
	if path == "" {
		t.Skip("set DRAW_MODEL_REFERENCE to the offline feature-reference.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Names []string `json:"names"`
		Rows  []struct {
			MatchID  string     `json:"matchId"`
			Values   []*float64 `json:"values"`
			Score    float64    `json:"score"`
			Selected bool       `json:"selected"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Rows) != 4161 {
		t.Fatalf("expected frozen 4161-row audit, got %d", len(reference.Rows))
	}
	maximumError := 0.0
	for _, row := range reference.Rows {
		if len(row.Values) != len(reference.Names) {
			t.Fatal("reference columns are not aligned")
		}
		features := make(map[string]float64, len(row.Values))
		for i, value := range row.Values {
			if value != nil {
				features[reference.Names[i]] = *value
			}
		}
		got := Predict(features)
		error := math.Abs(got.Score - row.Score)
		maximumError = math.Max(maximumError, error)
		if error > 1e-12 || got.Selected != row.Selected {
			t.Fatalf("%s got %+v want %.17g / %v", row.MatchID, got, row.Score, row.Selected)
		}
	}
	t.Logf("all %d scores/masks match; maximum absolute error %.17g", len(reference.Rows), maximumError)
}
