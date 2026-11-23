package drawmodeldata

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"time"
)

// Venue-split history features (candidate set, not yet part of the frozen
// contract). This file is deliberately outside the embedded feature-code hash:
// adding it changes neither production inference, the research feature
// version nor the model refresh trigger. Promote these names into
// history_features.go and the manifest only after a forward validation.
//
// Venue semantics follow drawObservationRecord: each record's venue is the
// perspective team's side in that older match (target home team for H2H).

const venueFeatureMinimum = 3

//go:embed venue_features.go
var venueFeatureSource []byte

// VenueFeatureCodeSHA256 identifies this extractor for training-snapshot
// change detection, separately from the immutable research feature hash.
func VenueFeatureCodeSHA256() string {
	return fmt.Sprintf("%x", sha256.Sum256(venueFeatureSource))
}

var historyStatisticNames = []string{"n", "draws", "wins", "losses", "drawStreak", "drawRate", "winRate", "lossRate", "goalsFor", "goalsAgainst", "totalGoals", "goalDifference", "under25Rate", "zeroZeroRate", "oneOneRate", "cleanSheetRate", "scorelessRate"}

// VenueFeatureNames is the fixed, ordered list appended to the 391 baseline
// inputs by models trained with venue features (53 names).
func VenueFeatureNames() []string {
	names := []string{}
	for _, side := range []string{"home", "away"} {
		for _, key := range historyStatisticNames {
			names = append(names, "safeVenue."+side+".5."+key)
		}
	}
	for _, key := range historyStatisticNames {
		if key != "wins" && key != "losses" && key != "drawStreak" {
			names = append(names, "safeH2H.sameVenue."+key)
		}
	}
	return append(names, "context.venueForm.home.pointsPerGame", "context.venueForm.away.pointsPerGame",
		"context.venueForm.pointsPerGame.sum", "context.venueForm.pointsPerGame.absGap", "context.venueForm.pointsPerGame.homeMinusAway")
}

// ModelFeatures is the training contract for new models: the 391 baseline
// inputs (same order and guards) followed by the venue features (no guards).
func ModelFeatures() []InputFeature {
	result := BaselineFeatures()
	for _, name := range VenueFeatureNames() {
		result = append(result, InputFeature{Name: name, OriginalIndex: -1, Guards: []Guard{}, View: "safeHistory"})
	}
	return result
}

// ExtractModelFeatures returns baseline and venue features together. Use it
// wherever the active model scores a match or a training snapshot is built.
func ExtractModelFeatures(data []byte) (map[string]float64, error) {
	out, err := ExtractFeatures(data)
	if err != nil {
		return nil, err
	}
	venue, err := ExtractVenueFeatures(data)
	if err != nil {
		return nil, err
	}
	for name, value := range venue {
		out[name] = value
	}
	return out, nil
}

// ExtractVenueFeatures returns home-team-at-home, away-team-away and
// same-venue head-to-head statistics with the same pre-kickoff safety rules
// as ExtractFeatures.
func ExtractVenueFeatures(data []byte) (map[string]float64, error) {
	var row map[string]interface{}
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("model input must be an object")
	}
	out := make(map[string]float64, 64)
	extractVenueFeatures(row, out)
	return out, nil
}

// venueRecords reuses safeHistoryRecords for every safety rule and keeps only
// records whose raw venue matches. The record ID is rebuilt exactly as there.
func venueRecords(row map[string]interface{}, block string, home bool) ([]safeHistoryRecord, bool) {
	records, present := safeHistoryRecords(row, block)
	if !present {
		return nil, false
	}
	want := "away"
	if home {
		want = "home"
	}
	venues := map[string]string{}
	for _, item := range rawArray(rawPath(row, "preMatchHistory", block, "matches")) {
		r := rawObject(item)
		id := featureString(r["matchId"])
		if id == "" {
			kickoff, known := featureTime(r["matchTime"])
			if !known {
				continue
			}
			id = kickoff.Format(time.RFC3339Nano) + "|" + featureString(r["home"]) + "|" + featureString(r["guest"])
		}
		if _, seen := venues[id]; !seen {
			venues[id] = featureString(r["venue"])
		}
	}
	result := make([]safeHistoryRecord, 0, len(records))
	for _, r := range records {
		if venues[r.id] == want {
			result = append(result, r)
		}
	}
	return result, true
}

func extractVenueFeatures(row map[string]interface{}, out map[string]float64) {
	points := map[string]float64{}
	for _, side := range []struct {
		name, block string
		home        bool
	}{{"home", "homeRecent", true}, {"away", "awayRecent", false}} {
		records, present := venueRecords(row, side.block, side.home)
		if !present {
			continue
		}
		stats := historyStatistics(records, true, 5)
		emitHistory(out, "safeVenue."+side.name+".5", stats, false)
		if stats["n"] >= venueFeatureMinimum {
			p := (3*stats["wins"] + stats["draws"]) / stats["n"]
			points[side.name] = p
			putFeature(out, "context.venueForm."+side.name+".pointsPerGame", p)
		}
	}
	if h, ok := points["home"]; ok {
		if a, ok := points["away"]; ok {
			featurePair(out, "context.venueForm.pointsPerGame", h, a)
			putFeature(out, "context.venueForm.pointsPerGame.homeMinusAway", h-a)
		}
	}
	if records, present := venueRecords(row, "headToHead", true); present {
		emitHistory(out, "safeH2H.sameVenue", historyStatistics(records, true, 10), true)
	}
}
