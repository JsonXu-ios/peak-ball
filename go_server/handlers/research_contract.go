package handlers

import (
	"crypto/sha256"
	"embed"
	"fmt"
)

// Preserve the cleaning/projection contract independently of the numeric
// feature definitions and model artifact. No match data is embedded here.
//
//go:embed research_sync.go research_evaluation.go draw_feature_study.go draw_observation.go draw_observation_history.go draw_observation_display.go draw_observation_stages.go draw_observation_predictions.go draw_observation_secondary.go draw_observation_score.go analysis.go analysis_weight.go analysis_simulation.go sporttery_odds.go
var researchContractFiles embed.FS

var researchPipelineCodeHash = func() string {
	hash := sha256.New()
	files, err := researchContractFiles.ReadDir(".")
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		body, err := researchContractFiles.ReadFile(file.Name())
		if err != nil {
			panic(err)
		}
		hash.Write([]byte(file.Name()))
		hash.Write([]byte{0})
		hash.Write(body)
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}()
