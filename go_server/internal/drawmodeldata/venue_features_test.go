package drawmodeldata

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestVenueFeaturesSplitAndSafety(t *testing.T) {
	kickoff := time.Date(2026, 11, 20, 19, 0, 0, 0, time.UTC)
	record := func(id string, daysAgo int, venue string, gf, ga int) map[string]interface{} {
		home, away := gf, ga
		if venue == "away" {
			home, away = ga, gf
		}
		return map[string]interface{}{"matchId": id, "matchTime": kickoff.Add(-time.Duration(daysAgo) * 24 * time.Hour).Format(time.RFC3339Nano),
			"homeScore": home, "awayScore": away, "venue": venue}
	}
	homeRecent := []interface{}{}
	for i := 0; i < 4; i++ {
		homeRecent = append(homeRecent, record(fmt.Sprintf("h-home-%d", i), 3+7*i, "home", 2, 0)) // 4 home wins
		homeRecent = append(homeRecent, record(fmt.Sprintf("h-away-%d", i), 6+7*i, "away", 0, 1)) // 4 away losses
	}
	homeRecent = append(homeRecent, record("future", -1, "home", 0, 5)) // after kickoff: must be ignored
	awayRecent := []interface{}{record("a1", 4, "away", 1, 1), record("a2", 11, "away", 0, 2), record("a3", 18, "away", 1, 1), record("a4", 9, "home", 3, 0)}
	h2h := []interface{}{record("x1", 100, "home", 1, 0), record("x2", 200, "away", 0, 0), record("x3", 300, "home", 2, 2)}
	row := map[string]interface{}{"matchId": "target", "matchTime": kickoff.Format(time.RFC3339Nano), "preMatchHistory": map[string]interface{}{
		"homeRecent": map[string]interface{}{"matches": homeRecent}, "awayRecent": map[string]interface{}{"matches": awayRecent},
		"headToHead": map[string]interface{}{"matches": h2h}}}
	body, _ := json.Marshal(row)
	f, err := ExtractVenueFeatures(body)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"safeVenue.home.5.n": 4, "safeVenue.home.5.winRate": 1, "safeVenue.home.5.goalDifference": 2,
		"safeVenue.away.5.n": 3, "safeVenue.away.5.draws": 2, "safeVenue.away.5.lossRate": 1.0 / 3,
		"context.venueForm.home.pointsPerGame": 3, "context.venueForm.away.pointsPerGame": 2.0 / 3,
		"context.venueForm.pointsPerGame.homeMinusAway": 3 - 2.0/3,
		"safeH2H.sameVenue.n":                           2, "safeH2H.sameVenue.winRate": 0.5, "safeH2H.sameVenue.drawRate": 0.5,
	}
	for name, v := range want {
		if got, ok := f[name]; !ok || got-v > 1e-12 || v-got > 1e-12 {
			t.Fatalf("%s = %v (present %v), want %v", name, got, ok, v)
		}
	}
	if _, ok := f["safeH2H.sameVenue.wins"]; ok {
		t.Fatal("H2H must not emit wins")
	}
	// Production features are untouched by the candidate set.
	base, err := ExtractFeatures(body)
	if err != nil {
		t.Fatal(err)
	}
	for name := range f {
		if _, clash := base[name]; clash {
			t.Fatalf("candidate feature %s collides with production feature", name)
		}
	}
}

func TestVenueFeatureContract(t *testing.T) {
	names := VenueFeatureNames()
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			t.Fatalf("duplicate venue feature %s", name)
		}
		seen[name] = true
	}
	if len(names) != 53 || len(ModelFeatures()) != 444 {
		t.Fatalf("unexpected contract size %d/%d", len(names), len(ModelFeatures()))
	}
	for i, f := range BaselineFeatures() {
		if ModelFeatures()[i].Name != f.Name {
			t.Fatal("baseline inputs must lead the model contract")
		}
	}
	kickoff := time.Date(2026, 11, 20, 19, 0, 0, 0, time.UTC)
	matches := []interface{}{}
	for i := 0; i < 8; i++ {
		venue := "home"
		if i%2 == 1 {
			venue = "away"
		}
		matches = append(matches, map[string]interface{}{"matchId": fmt.Sprintf("m%d", i), "matchTime": kickoff.Add(-time.Duration(i+1) * 72 * time.Hour).Format(time.RFC3339Nano), "homeScore": i % 3, "awayScore": 1, "venue": venue})
	}
	block := map[string]interface{}{"matches": matches}
	body, _ := json.Marshal(map[string]interface{}{"matchId": "t", "matchTime": kickoff.Format(time.RFC3339Nano),
		"preMatchHistory": map[string]interface{}{"homeRecent": block, "awayRecent": block, "headToHead": block}})
	got, err := ExtractVenueFeatures(body)
	if err != nil {
		t.Fatal(err)
	}
	for name := range got {
		if !seen[name] {
			t.Fatalf("extracted %s is not in the fixed contract", name)
		}
	}
	if len(got) != len(names) {
		t.Fatalf("full history should emit every venue feature: %d/%d", len(got), len(names))
	}
	model, err := ExtractModelFeatures(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := model["safeVenue.home.5.n"]; !ok {
		t.Fatal("combined extraction must include venue features")
	}
}
