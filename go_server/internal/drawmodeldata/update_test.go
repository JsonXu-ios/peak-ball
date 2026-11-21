package drawmodeldata

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestCurrentReleaseHasNovemberAndPythonParity(t *testing.T) {
	if Training().LatestDate < "2026-11-21" || Training().Rows < 6873 || Training().SportteryRows < 857 {
		t.Fatalf("release lacks November data: %+v", Training())
	}
	if err := ValidateArtifact(ActiveArtifact()); err != nil {
		t.Fatal(err)
	}
	if Version() == frozen.Version {
		t.Fatal("current model still uses October baseline")
	}
	if len(active.Load().model.Checks) < 20 {
		t.Fatal("missing score parity evidence")
	}
	for _, check := range active.Load().model.Checks {
		got := Predict(check.Features)
		if math.Abs(got.Score-check.Score) > 1e-11 || got.Selected != check.Selected {
			t.Fatal("runtime score parity failure")
		}
	}
}

func TestInvalidModelCannotReplaceActive(t *testing.T) {
	before := ArtifactSHA256()
	saved := ActiveArtifact()
	defer LoadArtifact(saved)
	var model map[string]interface{}
	if err := json.Unmarshal(saved, &model); err != nil {
		t.Fatal(err)
	}
	mutate := []func(map[string]interface{}){
		func(m map[string]interface{}) { m["schema"] = 77 },
		func(m map[string]interface{}) {
			m["features"].([]interface{})[0].(map[string]interface{})["name"] = "target.result"
		},
		func(m map[string]interface{}) {
			for _, n := range m["trees"].([]interface{})[0].([]interface{}) {
				v := n.(map[string]interface{})
				if v["leaf"] == false {
					v["left"] = 0
					break
				}
			}
		},
		func(m map[string]interface{}) {
			m["checks"].([]interface{})[0].(map[string]interface{})["score"] = .999999
		},
	}
	for _, change := range mutate {
		var copy map[string]interface{}
		_ = json.Unmarshal(saved, &copy)
		change(copy)
		data, _ := json.Marshal(copy)
		if err := LoadArtifact(data); err == nil {
			t.Fatal("invalid artifact accepted")
		}
		if ArtifactSHA256() != before {
			t.Fatal("failed validation replaced active model")
		}
	}
	copy := ActiveArtifact()
	copy[0] = '!'
	if string(ActiveArtifact()) != string(saved) {
		t.Fatal("active bytes exposed")
	}
}

// Optional 6k+ row validation of the training projection against the Go runtime.
// This opens only the given local file; no DB or services are involved.
func TestCurrentModelAllExportedRows(t *testing.T) {
	path := os.Getenv("DRAW_UPDATED_MODEL_REFERENCE")
	if path == "" {
		t.Skip("set DRAW_UPDATED_MODEL_REFERENCE to local prediction-reference.json")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Version string `json:"version"`
		Rows    []struct {
			Features map[string]float64 `json:"features"`
			Score    float64            `json:"score"`
			Selected bool               `json:"selected"`
		} `json:"rows"`
	}
	if err = json.Unmarshal(body, &source); err != nil {
		t.Fatal(err)
	}
	if source.Version != Version() || len(source.Rows) < 6873 {
		t.Fatal("wrong reference model or sample")
	}
	for i, row := range source.Rows {
		got := Predict(row.Features)
		if math.Abs(got.Score-row.Score) > 1e-11 || got.Selected != row.Selected {
			t.Fatalf("parity failure at %d", i)
		}
	}
}
