package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"go_server/internal/drawfullmodel"
	"go_server/internal/drawmodeldata"
)

// Opt-in full snapshot parity. It performs no database, network or service
// operation, and tests the actual embedded artifact used by the new handler.
func TestComprehensiveAllSnapshotParity(t *testing.T) {
	rawPath := os.Getenv("DRAW_FULL_PARITY_RAW")
	if rawPath == "" {
		t.Skip("set DRAW_FULL_PARITY_RAW to a comprehensive export for offline parity")
	}
	base := filepath.Dir(rawPath)
	predictionPath := os.Getenv("DRAW_FULL_PARITY_PREDICTIONS")
	if predictionPath == "" {
		predictionPath = filepath.Join(base, "predictions.json")
	}
	body, err := os.ReadFile(predictionPath)
	if err != nil {
		t.Fatal(err)
	}
	var expected struct {
		SourceHash   string `json:"sourceSHA256"`
		ArtifactHash string `json:"artifactSHA256"`
		Rows         []struct {
			MatchID     string  `json:"matchId"`
			DrawScore   float64 `json:"drawScore"`
			Selected    bool    `json:"selected"`
			AwayScore   float64 `json:"awayScore"`
			Alternative int     `json:"alternative"`
			Eligible    bool    `json:"eligible"`
			OldSelected bool    `json:"oldSelected"`
			OldScore    float64 `json:"oldScore"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(body, &expected); err != nil {
		t.Fatal(err)
	}
	info := drawfullmodel.Info()
	if !info.Available || expected.ArtifactHash != drawfullmodel.ArtifactSHA256() {
		t.Fatalf("wrong or unavailable experimental artifact: %+v expected=%s", info, expected.ArtifactHash)
	}
	file, err := os.Open(rawPath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", digest.Sum(nil)) != expected.SourceHash {
		t.Fatal("Python reference was built from a different comprehensive export")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	lookup := map[string]int{}
	for index, row := range expected.Rows {
		if _, present := lookup[row.MatchID]; present {
			t.Fatal("duplicate Python reference fixture", row.MatchID)
		}
		lookup[row.MatchID] = index
	}
	seen := map[string]bool{}
	rows, available, selected, mutations := 0, 0, 0, 0
	maxDrawDifference, maxSideDifference, maxOldDifference := 0., 0., 0.
	var samples []map[string]interface{}
	sampleKinds := map[string]int{}
	decoder := json.NewDecoder(file)
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		t.Fatal("invalid comprehensive source root", err)
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			t.Fatal(err)
		}
		if key != "rows" {
			var discarded json.RawMessage
			if err := decoder.Decode(&discarded); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('[') {
			t.Fatal("invalid comprehensive source rows", err)
		}
		for decoder.More() {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				t.Fatal(err)
			}
			var identity struct {
				MatchID   string `json:"matchId"`
				JingcaiID string `json:"jingcaiId"`
				HomeScore int    `json:"homeScore"`
				AwayScore int    `json:"awayScore"`
			}
			if err := json.Unmarshal(raw, &identity); err != nil {
				t.Fatal(err)
			}
			index, exists := lookup[identity.MatchID]
			if !exists || seen[identity.MatchID] {
				t.Fatal("missing or duplicate reference fixture", identity.MatchID)
			}
			seen[identity.MatchID] = true
			want := expected.Rows[index]
			features, err := drawfullmodel.ExtractFeatures(raw)
			if err != nil {
				t.Fatal(identity.MatchID, err)
			}
			got := drawfullmodel.Predict(features)
			if got.Available != want.Eligible || got.Selected != want.Selected {
				t.Fatalf("%s eligibility/selection mismatch: got=(%v,%v evidence=%d) want=(%v,%v)", identity.MatchID, got.Available, got.Selected, got.ObservedEvidence, want.Eligible, want.Selected)
			}
			if got.Available {
				available++
				maxDrawDifference = math.Max(maxDrawDifference, math.Abs(got.Score-want.DrawScore))
				maxSideDifference = math.Max(maxSideDifference, math.Abs(got.AwayScore-want.AwayScore))
				if math.Abs(got.Score-want.DrawScore) > 1e-12 || math.Abs(got.AwayScore-want.AwayScore) > 1e-12 {
					t.Fatalf("%s tree scores differ: draw %.17g/%.17g side %.17g/%.17g", identity.MatchID, got.Score, want.DrawScore, got.AwayScore, want.AwayScore)
				}
			}
			direction := -1
			if got.AlternativeDirection == "home" {
				direction = 0
			} else if got.AlternativeDirection == "away" {
				direction = 2
			}
			if direction != want.Alternative {
				t.Fatalf("%s side direction differs: got=%d want=%d", identity.MatchID, direction, want.Alternative)
			}
			oldFeatures, err := drawmodeldata.ExtractFeatures(raw)
			if err != nil {
				t.Fatal(err)
			}
			old := drawmodeldata.Predict(oldFeatures)
			maxOldDifference = math.Max(maxOldDifference, math.Abs(old.Score-want.OldScore))
			if math.Abs(old.Score-want.OldScore) > 1e-12 || (old.Selected && old.ObservedFeatures > 0) != want.OldSelected {
				t.Fatalf("%s original frozen model changed after adding fullAnalysis", identity.MatchID)
			}
			signal := drawObservationComprehensiveSignal(raw)
			if signal.State == "selected" {
				selected++
			}
			if (signal.State == "selected") != want.Selected || (signal.State != "unavailable") != want.Eligible {
				t.Fatalf("%s actual handler disagreed with its runtime", identity.MatchID)
			}
			if !want.Selected && signal.AlternativeDirection != "" {
				t.Fatal("handler recommends a backup for a non-selected fixture")
			}
			if want.Selected && signal.AlternativeDirection != got.AlternativeDirection {
				t.Fatal("handler backup differs from learned side tree")
			}
			kind := signal.State
			if identity.HomeScore == identity.AwayScore {
				kind += ":draw"
			} else if identity.HomeScore > identity.AwayScore {
				kind += ":home"
			} else {
				kind += ":away"
			}
			if sampleKinds[kind] < 1 || identity.MatchID == "498239380" {
				sampleKinds[kind]++
				samples = append(samples, map[string]interface{}{"matchId": identity.MatchID, "jingcaiId": identity.JingcaiID, "settled": true, "regulationSettled": true,
					"homeScore": identity.HomeScore, "awayScore": identity.AwayScore, "comprehensiveSignal": signal})
			}
			if rows%250 == 0 || identity.MatchID == "498239380" {
				var changed map[string]json.RawMessage
				if err := json.Unmarshal(raw, &changed); err != nil {
					t.Fatal(err)
				}
				changed["result"], changed["homeScore"], changed["awayScore"], changed["status"] = json.RawMessage("2"), json.RawMessage("0"), json.RawMessage("29"), json.RawMessage("-1")
				changed["predictionHit"], changed["frozenDrawSignal"], changed["secondaryDecision"] = json.RawMessage("true"), json.RawMessage(`{"state":"not_selected"}`), json.RawMessage(`{"decision":"rejected"}`)
				encoded, _ := json.Marshal(changed)
				mutation, err := drawfullmodel.ExtractFeatures(encoded)
				if err != nil || !reflect.DeepEqual(features, mutation) || !reflect.DeepEqual(signal, drawObservationComprehensiveSignal(encoded)) {
					t.Fatalf("%s target/old-decision mutation changed the independent model", identity.MatchID)
				}
				mutations++
			}
			rows++
		}
		if _, err := decoder.Token(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		t.Fatal(err)
	}
	if rows == 0 || rows != len(expected.Rows) {
		t.Fatalf("row counts differ: %d/%d", rows, len(expected.Rows))
	}
	report := map[string]interface{}{"status": "passed", "rows": rows, "available": available, "selected": selected, "targetMutationChecks": mutations,
		"maxDrawScoreDifference": maxDrawDifference, "maxSideScoreDifference": maxSideDifference, "maxOriginalScoreDifference": maxOldDifference,
		"sourceSHA256": expected.SourceHash, "artifactSHA256": expected.ArtifactHash, "servicesStarted": false, "sourceDatabaseAccess": false,
		"originalSchemeChanged": false, "inferencePrecision": "sklearn float32 input, float64 split threshold", "handlerSampleCount": len(samples)}
	for name, value := range map[string]interface{}{"parity.json": report, "handler-signals.json": map[string]interface{}{"items": samples}} {
		output, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, name), output, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("full snapshot parity: %d fixtures, %d available, %d selected; %d label checks; old predictions unchanged", rows, available, selected, mutations)
}
