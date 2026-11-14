// Finite offline replay of the original frozen model. No listeners or writes
// to source databases; target results are never read by the prediction API.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"go_server/internal/drawmodeldata"
)

func main() {
	input := flag.String("input", "", "existing original feature-study JSON")
	out := flag.String("out", "", "offline replay output JSON")
	flag.Parse()
	if *input == "" || *out == "" {
		log.Fatal("both -input and -out are required")
	}
	body, err := os.ReadFile(*input)
	if err != nil {
		log.Fatal(err)
	}
	var source struct {
		GeneratedAt time.Time         `json:"generatedAt"`
		Rows        []json.RawMessage `json:"rows"`
	}
	if err = json.Unmarshal(body, &source); err != nil {
		log.Fatal(err)
	}
	type row struct {
		MatchID  string  `json:"matchId"`
		Score    float64 `json:"score"`
		Selected bool    `json:"selected"`
		Observed int     `json:"observedFeatures"`
		Used     int     `json:"usedFeatures"`
	}
	rows := make([]row, 0, len(source.Rows))
	selected := 0
	seen := map[string]bool{}
	for _, raw := range source.Rows {
		var identity struct {
			MatchID string `json:"matchId"`
		}
		if err = json.Unmarshal(raw, &identity); err != nil {
			log.Fatal(err)
		}
		if identity.MatchID == "" || seen[identity.MatchID] {
			log.Fatal("missing or duplicate match ID")
		}
		seen[identity.MatchID] = true
		features, err := drawmodeldata.ExtractFeatures(raw)
		if err != nil {
			log.Fatal(err)
		}
		prediction := drawmodeldata.Predict(features)
		pick := prediction.ObservedFeatures > 0 && prediction.Selected
		if pick {
			selected++
		}
		rows = append(rows, row{identity.MatchID, prediction.Score, pick, prediction.ObservedFeatures, prediction.UsedFeatures})
	}
	result := map[string]interface{}{"version": "original-frozen-replay-v1", "modelVersion": drawmodeldata.Version(), "threshold": drawmodeldata.Threshold(), "inputSHA256": fmt.Sprintf("%x", sha256.Sum256(body)), "sourceGeneratedAt": source.GeneratedAt, "rows": rows, "selected": selected, "servicesStarted": false, "sourceDatabaseAccess": false, "modelTrained": false}
	encoded, err := json.Marshal(result)
	if err != nil {
		log.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		log.Fatal(err)
	}
	if err = os.WriteFile(*out, encoded, 0600); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("rows=%d selected=%d model=%s output=%s servicesStarted=false modelTrained=false\n", len(rows), selected, drawmodeldata.Version(), *out)
}
