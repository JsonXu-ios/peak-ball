package handlers

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// Finite, opt-in replay of an already exported read-only snapshot. This test
// never opens a database, Redis connection or listener. Its expected scores
// were recorded before the independent non-sporttery column was introduced.
func TestDrawNonSportterySnapshotParityAndLotteryIsolation(t *testing.T) {
	path := os.Getenv("DRAW_NONSPORTTERY_PARITY_SOURCE")
	expectedPath := os.Getenv("DRAW_NONSPORTTERY_PARITY_REPLAY")
	if path == "" || expectedPath == "" {
		t.Skip("set existing offline source and pre-change replay paths for full parity")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var source DrawFeatureStudyDataset
	if err = json.Unmarshal(body, &source); err != nil {
		t.Fatal(err)
	}
	body, err = os.ReadFile(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	type expectedRow struct {
		MatchID          string  `json:"matchId"`
		Score            float64 `json:"score"`
		Selected         bool    `json:"selected"`
		ObservedFeatures int     `json:"observedFeatures"`
		UsedFeatures     int     `json:"usedFeatures"`
	}
	var expected struct {
		Current struct {
			Rows []expectedRow `json:"rows"`
		} `json:"current"`
	}
	if err = json.Unmarshal(body, &expected); err != nil {
		t.Fatal(err)
	}
	lookup := map[string]expectedRow{}
	for _, row := range expected.Current.Rows {
		lookup[row.MatchID] = row
	}
	lottery, nonLottery, unavailable := 0, 0, 0
	for _, row := range source.Rows {
		want, exists := lookup[row.MatchID]
		if !exists {
			t.Fatalf("missing pre-change prediction for %s", row.MatchID)
		}
		item := researchTakeoverItem(row)
		original := drawObservationModelSignal(item, &row)
		independent := drawObservationNonSportterySignal(item, &row)
		got := original
		if item.JingcaiID == "" {
			nonLottery++
			if original.State != "out_of_scope" || original.Score != nil {
				t.Fatalf("non-lottery fixture leaked into lottery selection: %s", row.MatchID)
			}
			got = independent
		} else {
			lottery++
			if independent != nil {
				t.Fatalf("independent column populated a lottery row: %s", row.MatchID)
			}
		}
		if got == nil {
			t.Fatalf("missing scoped result: %s", row.MatchID)
		}
		if want.ObservedFeatures == 0 {
			unavailable++
			if got.State != "unavailable" || got.Score != nil {
				t.Fatalf("zero-input fixture mechanically selected: %s", row.MatchID)
			}
			continue
		}
		state := "not_selected"
		if want.Selected {
			state = "selected"
		}
		if got.State != state || got.Score == nil || math.Abs(*got.Score-want.Score) > 1e-12 || got.ObservedInputs != want.ObservedFeatures || got.UsedInputs != want.UsedFeatures {
			t.Fatalf("pre-change inference differs for %s: got=%+v expected=%+v", row.MatchID, got, want)
		}
	}
	if len(lookup) != len(source.Rows) || lottery == 0 || nonLottery == 0 {
		t.Fatal("parity fixture did not cover both scopes completely")
	}
	t.Logf("pre-change exact replay: lottery=%d non-lottery=%d unavailable=%d; lottery changes=0", lottery, nonLottery, unavailable)
}
