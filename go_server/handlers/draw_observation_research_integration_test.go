package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"go_server/config"
	"go_server/internal/drawmodeldata"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"peakball/researchstore"
)

type takeoverReadOnlyBackend struct {
	store         *researchstore.Store
	writeAttempts int
}

func (b *takeoverReadOnlyBackend) lookup(ctx context.Context, key drawResearchKey) (*drawResearchStoredRecord, error) {
	return lookupDrawResearchPrediction(ctx, b.store, key)
}
func (b *takeoverReadOnlyBackend) write(context.Context, drawObservationItem, drawFeatureStudyRow, []byte, drawResearchKey, *drawObservationFrozenSignal, time.Time) error {
	b.writeAttempts++
	return sql.ErrConnDone
}

// Root's finite preparation command owns write-through. This independent gate
// can only read: any miss, fallback, legacy callback or attempted write fails.
func TestDrawResearchCurrentSourceReadOnlyTakeover(t *testing.T) {
	if os.Getenv("DRAW_TAKEOVER_INTEGRATION") != "1" {
		t.Skip("set DRAW_TAKEOVER_INTEGRATION=1 only after current secondary-v2/dynamic-score inputs have been explicitly archived")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	source, err := gorm.Open(mysql.Open(config.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("business read source unavailable")
	}
	connection, err := source.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	started := time.Now()
	data, err := ExportDrawResearchCurrentSource(source.WithContext(ctx), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenResearchStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var prepared int
	if err = store.DB().QueryRowContext(ctx, "SELECT COUNT(DISTINCT match_id) FROM predictions WHERE model_version=? AND rule_version=? AND score_method=?", drawmodeldata.Version(), drawObservationSecondaryVersion+"+"+drawObservationProfitSupportVersion, drawScoreRecommendationVersion).Scan(&prepared); err != nil {
		t.Fatal(err)
	}
	if prepared < len(data.Rows) {
		t.Fatalf("current-version archive is not prepared: need %d unique fixtures, found %d for rule=%s score=%s; this read-only test will not write or upgrade old archives", len(data.Rows), prepared, drawObservationSecondaryVersion, drawScoreRecommendationVersion)
	}
	backend := &takeoverReadOnlyBackend{store: store}
	legacyCalls, served, settled := 0, 0, 0
	for _, row := range data.Rows {
		item := researchTakeoverItem(row)
		expected := drawObservationModelSignal(item, &row)
		got, decision := resolveDrawResearchSignalWithBackend(ctx, backend, item, &row, func() *drawObservationFrozenSignal { legacyCalls++; return expected }, time.Now().UTC())
		if decision.Source != "research" {
			t.Fatalf("current source is not already prepared: %s source=%s reason=%s writes=%d legacy=%d", row.MatchID, decision.Source, decision.Reason, backend.writeAttempts, legacyCalls)
		}
		if !drawResearchSignalsEqual(expected, got) {
			t.Fatalf("takeover changed exact current inference: %s", row.MatchID)
		}
		expectedItem, gotItem := item, item
		expectedItem.FrozenDrawSignal, gotItem.FrozenDrawSignal = expected, got
		if !reflect.DeepEqual(drawObservationSecondaryDecision(expectedItem), drawObservationSecondaryDecision(gotItem)) || !reflect.DeepEqual(drawObservationScoreRecommendation(expectedItem), drawObservationScoreRecommendation(gotItem)) {
			t.Fatalf("takeover changed guarded secondary or dynamic scores: %s", row.MatchID)
		}
		if decision.SnapshotID <= 0 || decision.PredictionID <= 0 || decision.AsOf == nil {
			t.Fatalf("untraceable serving decision: %s", row.MatchID)
		}
		served++
		if row.Result >= 0 {
			settled++
		}
	}
	if served < 597 || settled < 597 || legacyCalls != 0 || backend.writeAttempts != 0 {
		t.Fatalf("full read-only gate incomplete: served=%d settled=%d legacy=%d writes=%d", served, settled, legacyCalls, backend.writeAttempts)
	}
	t.Logf("exact current source served=%d settled=%d mismatches=0 fallback=0 legacyCalls=%d attemptedWrites=%d elapsed=%s", served, settled, legacyCalls, backend.writeAttempts, time.Since(started))

	// The response was produced by the real in-memory HTTP handler; inspecting
	// it and filtering its immutable day requires no HTTP/Redis/database writes.
	path := os.Getenv("DRAW_TAKEOVER_RESPONSE")
	if path == "" {
		path = filepath.Join(os.TempDir(), "peak-ball-research-takeover-response.json")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("actual takeover response fixture unavailable: ", err)
	}
	var response drawObservationResponse
	if err = json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	if response.Date != "2026-10-24" || response.Scope != "sporttery" || response.Total != 32 || len(response.Items) != 32 || response.ResearchTakeover == nil || response.ResearchTakeover.Mode != "research" || response.ResearchTakeover.Served != 32 || response.ResearchTakeover.Fallback != 0 {
		t.Fatalf("actual day response not fully research served: %#v", response.ResearchTakeover)
	}
	for _, row := range response.Items {
		if row.ResearchDecision == nil || row.ResearchDecision.Source != "research" || row.ResearchDecision.SnapshotID <= 0 || row.ResearchDecision.PredictionID <= 0 {
			t.Fatalf("actual HTTP row lacks verified library source: %s", row.MatchID)
		}
	}
	day := drawObservationStoredDay{Version: response.DataVersion, Date: response.Date, TakeoverVersion: drawResearchTakeoverVersion, AllSummary: response.Summary, SportterySummary: response.Summary, Items: response.Items, DrawIDs: map[string]bool{}}
	for _, row := range day.Items {
		if row.Settled && row.RegulationSettled != nil && *row.RegulationSettled && row.HomeScore == row.AwayScore {
			day.DrawIDs[row.MatchID] = true
		}
	}
	base := drawObservationQuery{date: response.Date, scope: "sporttery", mode: "today", page: 1, size: 200}
	whole := day.filter(base)
	expectedSecondary, expectedHits := 0, 0
	for _, row := range day.Items {
		if independentTakeoverSecondary(row) {
			expectedSecondary++
			if drawObservationMetricSettled(row) && row.HomeScore == row.AwayScore {
				expectedHits++
			}
		}
	}
	if whole.ObservationMetrics == nil || whole.ObservationMetrics.Original.Selected != 21 || whole.ObservationMetrics.Original.Hits != 10 || whole.ObservationMetrics.Secondary.Selected != expectedSecondary || whole.ObservationMetrics.Secondary.Hits != expectedHits {
		t.Fatal("day-level original/secondary metrics changed")
	}
	for _, mode := range []string{"today", "history"} {
		q := base
		q.mode = mode
		filtered := day.filter(q)
		want := 32
		if mode == "history" {
			want = 11
		}
		if filtered.Total != want || !reflect.DeepEqual(filtered.ObservationMetrics, whole.ObservationMetrics) {
			t.Fatalf("tab changed shared metrics or match count: %s total=%d", mode, filtered.Total)
		}
		ids := []string{}
		for page := 1; len(ids) < want; page++ {
			q.page, q.size = page, 5
			part := day.filter(q)
			if !reflect.DeepEqual(part.ObservationMetrics, whole.ObservationMetrics) {
				t.Fatalf("page changed shared metrics: %s/%d", mode, page)
			}
			for _, row := range part.Items {
				ids = append(ids, row.MatchID)
			}
		}
		if len(ids) != len(filtered.Items) {
			t.Fatalf("page count mismatch: %s", mode)
		}
		for i, id := range ids {
			if id != filtered.Items[i].MatchID {
				t.Fatalf("page ordering changed: %s/%d", mode, i)
			}
		}
	}
	t.Logf("actual HTTP day32/32 research; today/history tabs and 5-row paging preserve original21/10, guarded secondary %d/%d; no writes or listener", expectedHits, expectedSecondary)
}

// This opt-in gate reads every available archived lottery prediction. It never
// starts an HTTP listener, sync, migration, source writer or inference resolver.
// The exact original input blob, not MySQL's formatted values_json, is the
// reference for a frozen-model comparison.
func TestDrawResearchFullArchiveInvariance(t *testing.T) {
	if os.Getenv("DRAW_TAKEOVER_INTEGRATION") != "1" {
		t.Skip("set DRAW_TAKEOVER_INTEGRATION=1 to replay the complete existing research archive read-only")
	}
	const expectedArtifact = "e7cb7d50668a76e916d2f86a6ab598a6bcb23ab5e6c961dbd972875346afd987"
	if drawmodeldata.ArtifactSHA256() != expectedArtifact || drawObservationSecondaryThreshold != .225 {
		t.Fatal("original model artifact or secondary threshold changed during read-source takeover")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := OpenResearchStore(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.DB().QueryContext(ctx, `SELECT p.id,p.match_id,p.state,p.score,p.threshold_value,p.secondary_selected,p.enhanced,p.scorelines,p.payload,
s.id,s.data_revision,s.provenance,s.kickoff,e.payload,r.home_score,r.away_score,r.regulation_confirmed
FROM predictions p JOIN feature_snapshots s ON s.id=p.snapshot_id
JOIN snapshot_sources ss ON ss.snapshot_id=s.id
JOIN ingest_events e ON e.id=ss.event_id AND e.source='local_feature_input' AND e.kind='label_blind_input'
LEFT JOIN match_results r ON r.id=(SELECT rr.id FROM match_results rr WHERE rr.match_id=p.match_id ORDER BY rr.observed_at DESC,rr.id DESC LIMIT 1)
WHERE p.model_version=? AND p.rule_version=? AND p.score_method=? AND s.feature_version=?
AND p.state IN ('selected','not_selected')
ORDER BY p.predicted_at,p.id`, drawmodeldata.Version(), drawObservationSecondaryVersion+"+"+drawObservationProfitSupportVersion, drawScoreRecommendationVersion, researchFeatureVersion)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type result struct {
		secondary, enhanced, draw, firstHit, twoHit bool
		settled, recommended                        bool
	}
	latest := map[string]result{}
	replayed, live, reconstructed := 0, 0, 0
	started := time.Now()
	for rows.Next() {
		var predictionID, snapshotID int64
		var matchID, state, revision, provenance string
		var score, threshold sql.NullFloat64
		var secondary, enhanced bool
		var scoresJSON, payload, input []byte
		var kickoff time.Time
		var home, away sql.NullInt64
		var confirmed sql.NullBool
		if err := rows.Scan(&predictionID, &matchID, &state, &score, &threshold, &secondary, &enhanced, &scoresJSON, &payload, &snapshotID, &revision, &provenance, &kickoff, &input, &home, &away, &confirmed); err != nil {
			t.Fatal(err)
		}
		var row drawFeatureStudyRow
		if err := json.Unmarshal(input, &row); err != nil {
			t.Fatal(err)
		}
		if row.MatchID != matchID || !row.MatchTime.Equal(kickoff) || row.JingcaiID == "" {
			t.Fatalf("archive input identity mismatch: match=%s prediction=%d snapshot=%d", matchID, predictionID, snapshotID)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(input, &object); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"homeScore", "awayScore", "result", "status", "settled", "predictionHit"} {
			if _, ok := object[key]; ok {
				t.Fatalf("target label in archived feature input: %s/%s", matchID, key)
			}
		}
		expectedRevision := researchstore.HashBytes(researchJSON(map[string]any{"input": json.RawMessage(input), "version": researchFeatureVersion, "provenance": provenance}))
		if revision != expectedRevision {
			t.Fatalf("wrong-input revision association: %s/%d", matchID, snapshotID)
		}
		// Exercise the production reader against the exact archived key without
		// invoking its write-through path or asking it to infer a different time.
		key := drawResearchKey{matchID: matchID, revision: revision, provenance: provenance, kickoff: kickoff}
		record, err := lookupDrawResearchPrediction(ctx, store, key)
		if err != nil {
			t.Fatalf("exact archive lookup: %s/%d: %v", matchID, snapshotID, err)
		}
		served, err := validateDrawResearchRecord(record, key, input)
		if err != nil {
			t.Fatalf("archive serving verification: %s/%d: %v", matchID, snapshotID, err)
		}
		raw, err := drawmodeldata.ExtractFeatures(input)
		if err != nil {
			t.Fatal(err)
		}
		computed := drawmodeldata.Predict(raw)
		if !score.Valid || math.Abs(score.Float64-computed.Score) > 1e-12 || !threshold.Valid || threshold.Float64 != drawmodeldata.Threshold() || (state == "selected") != computed.Selected {
			t.Fatalf("original inference differs: %s/%d score=%.17g expected=%.17g", matchID, predictionID, score.Float64, computed.Score)
		}
		if served.Score == nil || *served.Score != score.Float64 || served.State != state || served.Threshold != threshold.Float64 {
			t.Fatalf("reader changed authoritative prediction: %s/%d", matchID, predictionID)
		}
		item := researchTakeoverItem(row)
		item.FrozenDrawSignal = &drawObservationFrozenSignal{ModelVersion: drawmodeldata.Version(), State: state, Score: &computed.Score, Threshold: drawmodeldata.Threshold(), ObservedInputs: computed.ObservedFeatures, UsedInputs: computed.UsedFeatures}
		expectedSecondary := independentTakeoverSecondary(item)
		if secondary != expectedSecondary {
			t.Fatalf("secondary decision differs: %s/%d", matchID, predictionID)
		}
		if enhanced != (expectedSecondary && drawObservationProfitSupport(item).State == "supported") {
			t.Fatalf("profit enhancement differs: %s/%d", matchID, predictionID)
		}
		var scores []drawRecommendedScore
		if err := json.Unmarshal(scoresJSON, &scores); err != nil {
			t.Fatal(err)
		}
		expectedScores := drawObservationScoreRecommendation(item)
		if !reflect.DeepEqual(scores, expectedScores.Scores) {
			t.Fatalf("dynamic score recommendation differs: %s/%d", matchID, predictionID)
		}
		var saved struct {
			Original *drawObservationFrozenSignal `json:"original"`
		}
		if err := json.Unmarshal(payload, &saved); err != nil {
			t.Fatal(err)
		}
		if saved.Original == nil || saved.Original.Score == nil || saved.Original.ModelVersion != drawmodeldata.Version() || saved.Original.State != state || math.Abs(*saved.Original.Score-computed.Score) > 1e-12 || saved.Original.ObservedInputs != computed.ObservedFeatures || saved.Original.UsedInputs != computed.UsedFeatures {
			t.Fatalf("stored signal payload differs: %s/%d", matchID, predictionID)
		}
		object["homeScore"], object["awayScore"], object["result"], object["settled"] = json.RawMessage("9"), json.RawMessage("8"), json.RawMessage("2"), json.RawMessage("true")
		mutated, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		mutatedFeatures, err := drawmodeldata.ExtractFeatures(mutated)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(raw, mutatedFeatures) {
			t.Fatalf("target result mutation changed input features: %s", matchID)
		}
		r := result{secondary: secondary, enhanced: enhanced, recommended: expectedScores.State == "recommended" && len(scores) == 2, settled: confirmed.Valid && confirmed.Bool && home.Valid && away.Valid}
		if r.settled {
			r.draw = home.Int64 == away.Int64
			for index, pick := range scores {
				if int64(pick.Home) == home.Int64 && int64(pick.Away) == away.Int64 {
					r.firstHit = r.firstHit || index == 0
					r.twoHit = true
				}
			}
		}
		latest[matchID] = r
		replayed++
		if provenance == "live" {
			live++
		} else {
			reconstructed++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(latest) < 597 {
		t.Fatalf("full-sample gate incomplete: %d unique available lottery matches", len(latest))
	}
	settled, totalDraws, secondary, secondaryDraws, secondaryFirst, secondaryTwo, enhanced, enhancedFirst, enhancedTwo := 0, 0, 0, 0, 0, 0, 0, 0, 0
	secondaryRecommended, enhancedRecommended := 0, 0
	for _, r := range latest {
		if !r.settled {
			continue
		}
		settled++
		if r.draw {
			totalDraws++
		}
		if r.secondary {
			secondary++
			if r.recommended {
				secondaryRecommended++
			}
			if r.draw {
				secondaryDraws++
			}
			if r.firstHit {
				secondaryFirst++
			}
			if r.twoHit {
				secondaryTwo++
			}
		}
		if r.enhanced {
			enhanced++
			if r.recommended {
				enhancedRecommended++
			}
			if r.firstHit {
				enhancedFirst++
			}
			if r.twoHit {
				enhancedTwo++
			}
		}
	}
	t.Logf("full archive replayed=%d uniqueLottery=%d liveVersions=%d reconstructedVersions=%d elapsed=%s artifact=%s", replayed, len(latest), live, reconstructed, time.Since(started), drawmodeldata.ArtifactSHA256())
	t.Logf("latest-settled retrospective matches=%d draws=%d secondary=%d/%d recommended=%d first=%d topTwo=%d enhanced=%d recommended=%d first=%d topTwo=%d", settled, totalDraws, secondaryDraws, secondary, secondaryRecommended, secondaryFirst, secondaryTwo, enhanced, enhancedRecommended, enhancedFirst, enhancedTwo)
}

// Independent row-level reference for the integration metrics. It intentionally
// does not invoke the production secondary classifier or read the target score.
func independentTakeoverSecondary(row drawObservationItem) bool {
	s := row.FrozenDrawSignal
	if strings.TrimSpace(row.JingcaiID) == "" || s == nil || s.State != "selected" || s.Score == nil || math.IsNaN(*s.Score) || math.IsInf(*s.Score, 0) || *s.Score < .225 || *s.Score > 1 || row.HeadToHead.Sample < 5 {
		return false
	}
	n := 0
	for _, q := range row.CompanyQuotes {
		if strings.TrimSpace(q.Key) != "sporttery" {
			continue
		}
		n++
		if q.EuroOdds.Source != "odds_moneys" || q.EuroOdds.OpeningStatus != "available" {
			return false
		}
		for _, prices := range [][]*string{q.EuroOdds.Opening, q.EuroOdds.Latest} {
			if len(prices) != 3 {
				return false
			}
			for _, p := range prices {
				if p == nil {
					return false
				}
				v, err := strconv.ParseFloat(strings.TrimSpace(*p), 64)
				if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 1 {
					return false
				}
			}
		}
	}
	return n == 1
}
