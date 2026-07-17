package handlers

import (
	"encoding/json"
	"fmt"
	"testing"

	"go_server/database"
	"go_server/models"
)

// TestPlatformGolden prints the ported platform block for the golden match
// (特瑞特联 vs 科克城) so it can be compared with the values the old frontend
// showed. RUN: go test ./handlers/ -run TestPlatformGolden -v
func TestPlatformGolden(t *testing.T) {
	if err := database.Init(); err != nil {
		t.Skipf("no DB: %v", err)
	}
	var match models.Money
	if err := database.DB.Where("home = ? AND guest = ?", "特瑞特联", "科克城").Order("match_time DESC").First(&match).Error; err != nil {
		t.Skipf("golden match not found: %v", err)
	}
	response := buildAnalysisWithWeights(match, false)
	if response.Platform == nil {
		t.Fatal("platform block missing")
	}
	blob, _ := json.MarshalIndent(map[string]interface{}{
		"bookmaker": response.Platform.Bookmaker,
		"platform":  response.Platform.Platform,
		"warnings":  response.Platform.WarningRows,
		"evilFirst": response.Platform.EvilCult.Prediction.FirstPick,
		"evilMain":  response.Platform.EvilCult.Prediction.MainPick,
	}, "", "  ")
	fmt.Printf("GOLDEN %s\n", string(blob))
}
