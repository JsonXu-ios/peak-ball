package drawmodeldata

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestHistoryFeatureSafetyAndTargetIndependence(t *testing.T) {
	row := map[string]interface{}{"matchId": "target", "matchTime": "2026-10-26T18:00:00+08:00", "result": 1, "homeScore": 0, "awayScore": 0}
	record := func(id, kickoff, venue string, h, a float64) map[string]interface{} {
		return map[string]interface{}{"matchId": id, "matchTime": kickoff, "venue": venue, "homeScore": h, "awayScore": a}
	}
	records := []interface{}{
		record("target", "2026-10-20T00:00:00+08:00", "home", 0, 0),
		record("too-close", "2026-10-26T16:00:00+08:00", "home", 0, 0),
		record("future", "2026-10-27T10:00:00+08:00", "home", 0, 0),
		record("a", "2026-10-23T18:00:00+08:00", "away", 0, 1),
		record("a", "2026-10-22T18:00:00+08:00", "home", 0, 5),
		record("b", "2026-10-20T18:00:00+08:00", "home", 1, 1),
		record("c", "2026-10-17T18:00:00+08:00", "home", 1, 0),
		record("d", "2026-10-14T18:00:00+08:00", "home", 0, 1),
		record("e", "2026-10-11T18:00:00+08:00", "home", 0, 0),
	}
	row["preMatchHistory"] = map[string]interface{}{"homeRecent": map[string]interface{}{"matches": records}, "awayRecent": map[string]interface{}{"matches": records}, "headToHead": map[string]interface{}{"matches": records}}
	encode := func() map[string]float64 {
		data, _ := json.Marshal(row)
		features, err := ExtractFeatures(data)
		if err != nil {
			t.Fatal(err)
		}
		return features
	}
	before := encode()
	if before["safeRecent.home.10.n"] != 5 || before["safeRecent.home.10.wins"] != 2 || before["safeRecent.home.10.draws"] != 2 || before["conflict.recent.home.winStreak"] != 1 || before["context.schedule.home.restDays"] != 3 {
		t.Fatalf("incorrect safe history: %v", before)
	}
	row["homeScore"], row["awayScore"], row["result"], row["status"] = 8, 1, 0, 999
	if !reflect.DeepEqual(before, encode()) {
		t.Fatal("target result changed features")
	}
	delete(row, "preMatchHistory")
	missing := encode()
	if _, exists := missing["safeRecent.home.10.n"]; exists {
		t.Fatal("missing history became observed zero")
	}
	if _, err := ExtractFeatures([]byte("null")); err == nil {
		t.Fatal("null should be rejected")
	}
}

// The optional full frozen-data parity audit is explicit and offline. Ordinary
// package tests use the small contracts and do not need research exports.
func TestExtractFrozenFeatureParity(t *testing.T) {
	dataset, reference := os.Getenv("DRAW_MODEL_DATASET"), os.Getenv("DRAW_MODEL_REFERENCE")
	if dataset == "" || reference == "" {
		t.Skip("set frozen export/reference paths for the full offline audit")
	}
	var raw struct {
		Rows []json.RawMessage `json:"rows"`
	}
	var ref struct {
		Names []string `json:"names"`
		Rows  []struct {
			MatchID  string     `json:"matchId"`
			Values   []*float64 `json:"values"`
			Score    float64    `json:"score"`
			Selected bool       `json:"selected"`
		} `json:"rows"`
	}
	data, err := os.ReadFile(dataset)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if len(raw.Rows) != 4161 || len(ref.Rows) != len(raw.Rows) {
		t.Fatalf("unexpected reference size")
	}
	maxFeature, maxScore := 0., 0.
	checked, marked, selected, draws := 0, 0, 0, 0
	for i, encoded := range raw.Rows {
		var identity struct {
			MatchID   string `json:"matchId"`
			JingcaiID string `json:"jingcaiId"`
			Result    int    `json:"result"`
		}
		if err = json.Unmarshal(encoded, &identity); err != nil {
			t.Fatal(err)
		}
		if identity.MatchID != ref.Rows[i].MatchID {
			t.Fatal("reference identity mismatch")
		}
		features, err := ExtractFeatures(encoded)
		if err != nil {
			t.Fatal(err)
		}
		for j, name := range ref.Names {
			value, present := features[name]
			want := ref.Rows[i].Values[j]
			if want == nil {
				if present {
					t.Fatalf("%s %s missing became %v", identity.MatchID, name, value)
				}
				continue
			}
			if !present {
				t.Fatalf("%s %s expected %v but absent", identity.MatchID, name, *want)
			}
			diff := math.Abs(value - *want)
			maxFeature = math.Max(maxFeature, diff)
			checked++
			if diff > 1e-12 {
				t.Fatalf("%s %s got %.17g expected %.17g", identity.MatchID, name, value, *want)
			}
		}
		prediction := Predict(features)
		maxScore = math.Max(maxScore, math.Abs(prediction.Score-ref.Rows[i].Score))
		if math.Abs(prediction.Score-ref.Rows[i].Score) > 1e-12 || prediction.Selected != ref.Rows[i].Selected {
			t.Fatalf("%s inference mismatch: %+v want score %.17g selected %v", identity.MatchID, prediction, ref.Rows[i].Score, ref.Rows[i].Selected)
		}
		if identity.JingcaiID != "" {
			if prediction.ObservedFeatures == 0 {
				t.Fatalf("historical sporttery row %s would be marked unavailable", identity.MatchID)
			}
			marked++
			if prediction.Selected {
				selected++
				if identity.Result == 1 {
					draws++
				}
			}
		}
	}
	if marked != 511 || selected != 320 || draws != 122 {
		t.Fatalf("frozen marked cohort changed: %d/%d/%d", marked, selected, draws)
	}
	t.Logf("raw feature checks=%d, max feature error=%g max score error=%g; marked=%d selected=%d draws=%d", checked, maxFeature, maxScore, marked, selected, draws)
}
