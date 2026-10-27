package drawmodeldata

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
)

func TestMarketCompensatedPriceSums(t *testing.T) {
	// These actual price triples sit exactly on learned tree boundaries. Python
	// 3.13 compensated sums land on opposite sides from naive float addition.
	for _, example := range []struct {
		Prices string
		Want   float64
	}{
		{`[3.80,2.75,2.10]`, 0.10298473456368185},
		{`[12.00,6.00,1.20]`, 0.083333333333333259},
		{`[1.25,5.00,12.00]`, 0.083333333333333481},
	} {
		var prices interface{}
		if err := json.Unmarshal([]byte(example.Prices), &prices); err != nil {
			t.Fatal(err)
		}
		row := map[string]interface{}{"companyQuotes": []interface{}{
			map[string]interface{}{"key": "william", "euroOdds": map[string]interface{}{"source": "odds_moneys", "latest": prices}},
		}}
		out := map[string]float64{}
		extractMarketFeatures(row, out)
		if got := out["euro.william.latest.overround"]; got != example.Want {
			t.Fatalf("%s overround got %.17g want %.17g", example.Prices, got, example.Want)
		}
	}
}

// This audit checks ported market extraction against values produced by the
// original Python extractor from real raw rows, before any model imputation.
func TestMarketFrozenParity(t *testing.T) {
	datasetPath, referencePath := os.Getenv("DRAW_MODEL_DATASET"), os.Getenv("DRAW_MODEL_REFERENCE")
	if datasetPath == "" || referencePath == "" {
		t.Skip("set DRAW_MODEL_DATASET and DRAW_MODEL_REFERENCE for full offline parity audit")
	}
	data, err := os.ReadFile(datasetPath)
	if err != nil {
		t.Fatal(err)
	}
	var dataset struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	if err := json.Unmarshal(data, &dataset); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(referencePath)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Names []string `json:"names"`
		Rows  []struct {
			MatchID string     `json:"matchId"`
			Values  []*float64 `json:"values"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(dataset.Rows) != 4161 || len(reference.Rows) != 4161 {
		t.Fatal("expected aligned 4161-row frozen research dataset")
	}
	columns := []int{}
	for i, name := range reference.Names {
		if strings.HasPrefix(name, "euro.") || strings.HasPrefix(name, "difference.") || strings.HasPrefix(name, "conflict.difference.") || strings.HasPrefix(name, "kelly.") || strings.HasPrefix(name, "line.") || strings.HasPrefix(name, "marketProfit.") || strings.HasPrefix(name, "trade.") || name == "heat.asianHome" {
			columns = append(columns, i)
		}
	}
	checked, mismatches := 0, 0
	maximumError := 0.0
	for rowIndex, row := range dataset.Rows {
		want := reference.Rows[rowIndex]
		if marketString(row["matchId"]) != want.MatchID {
			t.Fatal("dataset reference ID alignment mismatch")
		}
		out := map[string]float64{}
		extractMarketFeatures(row, out)
		for _, i := range columns {
			name, expected := reference.Names[i], want.Values[i]
			got, exists := out[name]
			checked++
			if expected == nil {
				if exists {
					if mismatches < 10 {
						t.Errorf("%s %s got %.17g; want missing", want.MatchID, name, got)
					}
					mismatches++
				}
				continue
			}
			error := math.Abs(got - *expected)
			maximumError = math.Max(maximumError, error)
			if !exists || error > 1e-12 {
				if mismatches < 10 {
					t.Errorf("%s %s got %.17g (exists=%v); want %.17g", want.MatchID, name, got, exists, *expected)
				}
				mismatches++
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d market input mismatches among %d comparisons", mismatches, checked)
	}
	t.Logf("checked %d market columns x %d rows = %d raw fields; max absolute error %.17g", len(columns), len(reference.Rows), checked, maximumError)
}
