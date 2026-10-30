package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"go_server/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// A finite, explicitly enabled source review. It has no listener, Redis,
// migration, collector, research-store write, or service lifecycle operation.
// Real SELECTs run in a read-only MVCC transaction; archive replay uses memory.
func TestDrawObservationUpdatedCurrentDayReadOnly(t *testing.T) {
	if os.Getenv("DRAW_OBSERVATION_LIVE_REVIEW") != "1" {
		t.Skip("set DRAW_OBSERVATION_LIVE_REVIEW=1 for the finite read-only day review")
	}
	date := os.Getenv("DRAW_OBSERVATION_REVIEW_DATE")
	if date == "" {
		date = "2026-10-31"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, err := gorm.Open(mysql.Open(config.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("cannot open the business read source")
	}
	connection, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var response drawObservationResponse
	archiveReads, legacyCalls, bfRecommendations := 0, 0, 0
	started := time.Now()
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaders := drawObservationSQLLoadersForDB(func() *gorm.DB { return tx })
		loaders.research = false // inference only; never write to the real archive
		cache := newDrawObservationCache(loaders, time.Hour, time.Now)
		var readErr error
		response, readErr = cache.read(drawObservationQuery{date: date, scope: "sporttery", mode: "today", page: 1, size: 200})
		if readErr != nil {
			return readErr
		}
		for _, row := range response.Items {
			if row.SecondaryDecision == nil || row.SecondaryDecision.Version != drawObservationSecondaryVersion || row.ScoreRecommendation == nil || row.ScoreRecommendation.Version != drawScoreRecommendationVersion {
				t.Fatalf("missing current contracts for %s", row.MatchID)
			}
			retained := row.SecondaryDecision.Decision == "retained"
			if retained && (!drawObservationSecondarySportteryQuotes(row) || row.HeadToHead.Sample < 5) {
				t.Fatalf("invalid candidate passed the guards: %s", row.MatchID)
			}
			if !retained && len(row.ScoreRecommendation.Scores) != 0 {
				t.Fatalf("rejected match still has score recommendations: %s", row.MatchID)
			}
			if row.ScoreRecommendation.State == "recommended" {
				if len(row.ScoreRecommendation.Scores) != 2 || row.ScoreRecommendation.Scores[0] == row.ScoreRecommendation.Scores[1] {
					t.Fatalf("invalid ranked scores for %s", row.MatchID)
				}
				if row.ScoreRecommendation.Source == "sporttery_score_odds" {
					bfRecommendations++
				}
			}
			enrichment := cache.core.frontendCache[row.MatchID]
			input := enrichment.modelInput
			if input == nil {
				t.Fatalf("missing exact source projection for %s", row.MatchID)
			}
			// Verify v2 guards, BF evidence, exact raw-input identity and ordered
			// scores can all survive the same archive lookup used by production.
			now := time.Now().UTC().Truncate(time.Microsecond)
			_, body, key, prepareErr := prepareDrawResearchInput(row, input, now)
			if prepareErr != nil {
				return prepareErr
			}
			backend := &drawResearchFakeBackend{record: drawResearchFakeRecord(key, body, row.FrozenDrawSignal, now)}
			signal, decision := resolveDrawResearchSignalWithBackend(ctx, backend, row, input, func() *drawObservationFrozenSignal {
				legacyCalls++
				return row.FrozenDrawSignal
			}, now)
			if decision.Source != "research" || backend.writes != 0 || !drawResearchSignalsEqual(signal, row.FrozenDrawSignal) {
				t.Fatalf("exact current-source archive replay failed: %s, %s", row.MatchID, decision.Reason)
			}
			archiveReads++
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) == 0 || legacyCalls != 0 || archiveReads != len(response.Items) {
		t.Fatalf("incomplete day review: rows=%d archive=%d legacy=%d", len(response.Items), archiveReads, legacyCalls)
	}
	metrics := buildDrawObservationMetrics(response.Items, drawObservationQuery{date: date, scope: "sporttery"})
	if metrics.SecondaryVersion != drawObservationSecondaryVersion {
		t.Fatal("metrics use an older rule version")
	}
	response.ObservationMetrics = metrics
	// Metrics are a whole-day value, independent of rows shown by pagination.
	day := drawObservationStoredDay{Date: date, Items: response.Items, DrawIDs: map[string]bool{}}
	for _, mode := range []string{"today", "history"} {
		page := day.filter(drawObservationQuery{date: date, scope: "sporttery", mode: mode, page: 1, size: 3})
		if !reflect.DeepEqual(page.ObservationMetrics, metrics) {
			t.Fatal("tab or pagination changed the full-day metrics")
		}
	}
	body, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("DRAW_OBSERVATION_REVIEW_OUTPUT")
	if path == "" {
		path = filepath.Join(os.TempDir(), "peak-ball-"+date+"-dynamic-score-preview.json")
	}
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("date=%s rows=%d original=%d secondary=%d BF=%d exactArchiveReads=%d legacy=%d databaseWrites=0 elapsed=%s output=%s", date, len(response.Items), metrics.Original.Selected, metrics.Secondary.Selected, bfRecommendations, archiveReads, legacyCalls, time.Since(started), path)
}
