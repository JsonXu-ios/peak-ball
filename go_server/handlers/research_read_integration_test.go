package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/internal/drawmodeldata"
	"peakball/researchstore"
)

// Opt-in, read-only checks against an already initialized research database.
// No database initialization, source writes, server/listener or sync is invoked.
// Run after the finite import completes:
// RESEARCH_INTEGRATION=1 go test ./handlers -run TestResearchReadOnlyIntegration -v
func TestResearchReadOnlyIntegration(t *testing.T) {
	if os.Getenv("RESEARCH_INTEGRATION") != "1" {
		t.Skip("set RESEARCH_INTEGRATION=1 after the research import completes")
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/overview", GetResearchOverview)
	router.GET("/features", GetResearchFeatures)
	router.GET("/issues", GetResearchIssues)
	router.GET("/evaluations", GetResearchEvaluations)
	get := func(t *testing.T, path string, target any) {
		t.Helper()
		start := time.Now()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		t.Logf("GET %s: %d in %s", path, recorder.Code, time.Since(start))
		if recorder.Code != http.StatusOK {
			t.Fatalf("read-only endpoint failed (%s): %d %s", path, recorder.Code, recorder.Body.String())
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
			t.Fatalf("invalid endpoint JSON: %v", err)
		}
	}
	t.Run("complete_database_structure", func(t *testing.T) {
		var overview researchstore.Overview
		get(t, "/overview", &overview)
		if len(overview.Tables) != 18 {
			t.Fatalf("expected 18 tables, got %d", len(overview.Tables))
		}
		columns, definitions := 0, int64(0)
		tables := map[string]map[string]bool{}
		for _, table := range overview.Tables {
			if table.Label == "" || table.Layer == "" || table.Description == "" {
				t.Fatalf("table lacks readable documentation: %s", table.Name)
			}
			fields := map[string]bool{}
			for _, column := range table.Columns {
				if column.Name == "" || column.Type == "" || column.Comment == "" {
					t.Fatalf("undocumented field: %s.%s", table.Name, column.Name)
				}
				if fields[column.Name] {
					t.Fatalf("duplicate field: %s.%s", table.Name, column.Name)
				}
				fields[column.Name] = true
			}
			tables[table.Name] = fields
			columns += len(table.Columns)
			for _, index := range table.Indexes {
				if len(index.Columns) == 0 {
					t.Fatalf("empty index: %s.%s", table.Name, index.Name)
				}
				for _, column := range index.Columns {
					if !fields[column] {
						t.Fatalf("index references missing field: %s.%s", table.Name, column)
					}
				}
			}
		}
		if columns != 173 {
			t.Fatalf("expected 173 documented columns, got %d", columns)
		}
		for _, table := range overview.Tables {
			for _, key := range table.ForeignKeys {
				if !tables[table.Name][key.Column] || !tables[key.ReferencedTable][key.ReferencedColumn] {
					t.Fatalf("foreign key points outside the shown schema: %#v", key)
				}
			}
		}
		for _, set := range overview.FeatureSets {
			if set.Version == researchFeatureVersion {
				definitions = set.Features
			}
		}
		if definitions != 530 || overview.Summary.FeatureDefinitions < 530 {
			t.Fatalf("feature registration pending or incomplete: %d", definitions)
		}
		t.Logf("database=%s tables=%d fields=%d definitions=%d snapshots=%d predictions=%d", overview.Database, len(overview.Tables), columns, definitions, overview.Summary.FeatureSnapshots, overview.Summary.Predictions)
	})
	t.Run("feature_dictionary_paging_search_and_raw_json", func(t *testing.T) {
		type page struct {
			Items    []researchstore.FeatureDefinition `json:"items"`
			Total    int                               `json:"total"`
			Page     int                               `json:"page"`
			PageSize int                               `json:"pageSize"`
		}
		seen := map[string]bool{}
		search := ""
		for number := 1; number <= 3; number++ {
			var result page
			get(t, "/features?page="+string(rune('0'+number))+"&page_size=200", &result)
			if result.Total != 530 || result.Page != number || result.PageSize != 200 {
				t.Fatalf("dictionary pagination mismatch: %#v", result)
			}
			for _, item := range result.Items {
				if seen[item.Name] {
					t.Fatalf("page overlap: %s", item.Name)
				}
				seen[item.Name] = true
				if search == "" && !strings.ContainsAny(item.Name, "_%") {
					search = item.Name
				}
				assertResearchJSONObject(t, item.Metadata, "feature metadata")
			}
		}
		if len(seen) != 530 {
			t.Fatalf("dictionary pages did not cover all features: %d", len(seen))
		}
		var filtered page
		get(t, "/features?search="+url.QueryEscape(search)+"&page_size=200", &filtered)
		if filtered.Total < 1 {
			t.Fatal("exact feature name could not be searched")
		}
		for _, item := range filtered.Items {
			if !strings.Contains(item.Name, search) && !strings.Contains(item.Description, search) {
				t.Fatalf("search returned unrelated feature: %s", item.Name)
			}
		}
	})
	t.Run("audit_issues_raw_json", func(t *testing.T) {
		var result struct {
			Items []researchIssueView `json:"items"`
			Total int                 `json:"total"`
			RunID int64               `json:"runId"`
		}
		get(t, "/issues?page_size=5", &result)
		if result.Total == 0 {
			t.Skip("PENDING: latest import has no audit issues yet; issue-content validation not performed")
		}
		if result.RunID <= 0 || len(result.Items) == 0 {
			t.Fatal("audit paging missing active batch or records")
		}
		for _, item := range result.Items {
			if item.RunID != result.RunID || item.Code == "" || item.Field == "" || item.Message == "" {
				t.Fatalf("audit record lacks an actionable reason: %#v", item)
			}
			assertResearchJSONObject(t, item.Detail, "issue detail")
		}
	})
	t.Run("evaluation_cohorts_and_denominators", func(t *testing.T) {
		var response struct {
			Items []researchEvaluationView `json:"items"`
		}
		get(t, "/evaluations", &response)
		if len(response.Items) == 0 {
			t.Skip("PENDING: import has not written evaluations; cohort/denominator checks not performed")
		}
		scopes := map[string]bool{}
		for _, item := range response.Items {
			assertResearchJSONObject(t, item.Metrics, "evaluation metrics")
			var metrics struct {
				Groups      map[string]researchMetric `json:"groups"`
				Pending     int                       `json:"pending"`
				Settled     int                       `json:"settled"`
				Historical  bool                      `json:"historicalAssociationOnly"`
				Independent bool                      `json:"independentOfHistoricalReconstruction"`
			}
			if err := json.Unmarshal(item.Metrics, &metrics); err != nil {
				t.Fatal(err)
			}
			for name, group := range metrics.Groups {
				if group.Hits > group.Selected || group.Selected > group.Eligible || group.Draws > group.Eligible || group.ScoreHits > group.ScoreCandidates || group.ScoreCandidates > group.Selected {
					t.Fatalf("inconsistent cohort counts: %s/%s %#v", item.Scope, name, group)
				}
				assertResearchRate(t, group.Precision, group.Hits, group.Selected, "precision")
				assertResearchRate(t, group.Recall, group.Hits, group.Draws, "recall")
				assertResearchRate(t, group.ScoreHitRate, group.ScoreHits, group.ScoreCandidates, "scoreHitRate")
				if item.Scope == "sporttery_prospective_first_prediction" && group.Eligible != metrics.Settled {
					t.Fatal("pending/unconfirmed matches entered the prospective denominator")
				}
			}
			if item.Scope == "sporttery_prospective_first_prediction" && !metrics.Independent {
				t.Fatal("prospective scope missing reconstruction exclusion marker")
			}
			if item.Scope == "settled_sporttery_retrospective" && !metrics.Historical {
				t.Fatal("retrospective scope is mislabeled")
			}
			scopes[item.Scope] = true
			t.Logf("scope=%s groups=%d pending=%d settled=%d", item.Scope, len(metrics.Groups), metrics.Pending, metrics.Settled)
		}
		if !scopes["settled_sporttery_retrospective"] || !scopes["sporttery_prospective_first_prediction"] {
			t.Fatal("completed import must expose separate retrospective and prospective evaluations")
		}
	})
	t.Run("frozen_snapshot_replay_and_live_boundaries", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		store, err := OpenResearchStore(ctx)
		if err != nil {
			t.Fatal("research connection unavailable")
		}
		defer store.Close()
		var snapshots int64
		if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM feature_snapshots").Scan(&snapshots); err != nil {
			t.Fatal(err)
		}
		if snapshots == 0 {
			t.Skip("PENDING: input snapshots are not archived yet; replay checks not performed")
		}
		var invalidCutoff, invalidEvidence, invalidPrediction int64
		err = store.DB().QueryRowContext(ctx, `SELECT
(SELECT COUNT(*) FROM feature_snapshots WHERE provenance='live' AND (known_by IS NULL OR known_by>as_of OR as_of>=kickoff)),
(SELECT COUNT(*) FROM feature_snapshots s JOIN snapshot_sources ss ON ss.snapshot_id=s.id JOIN ingest_events e ON e.id=ss.event_id WHERE s.provenance='live' AND (e.provenance<>'live' OR e.observed_at>s.as_of OR e.observed_at>s.known_by)),
(SELECT COUNT(*) FROM predictions p JOIN feature_snapshots s ON s.id=p.snapshot_id WHERE s.provenance='live' AND (p.predicted_at>=s.kickoff OR p.predicted_at<s.as_of))`).Scan(&invalidCutoff, &invalidEvidence, &invalidPrediction)
		if err != nil {
			t.Fatal(err)
		}
		if invalidCutoff+invalidEvidence+invalidPrediction != 0 {
			t.Fatalf("live temporal violations: cutoff=%d evidence=%d prediction=%d", invalidCutoff, invalidEvidence, invalidPrediction)
		}
		rows, err := store.DB().QueryContext(ctx, `SELECT p.match_id,p.state,p.score,p.secondary_selected,p.scorelines,s.values_json,e.payload
FROM predictions p JOIN feature_snapshots s ON s.id=p.snapshot_id
JOIN snapshot_sources ss ON ss.snapshot_id=s.id JOIN ingest_events e ON e.id=ss.event_id AND e.source='local_feature_input'
WHERE p.model_version=? AND p.state IN ('selected','not_selected')
ORDER BY CRC32(p.match_id),p.id LIMIT 12`, drawmodeldata.Version())
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		replayed := 0
		roundTrippedFields := 0
		maxJSONRoundTripDifference := 0.0
		for rows.Next() {
			var matchID, state string
			var score sql.NullFloat64
			var secondary bool
			var scoreJSON, archivedValues, input []byte
			if err := rows.Scan(&matchID, &state, &score, &secondary, &scoreJSON, &archivedValues, &input); err != nil {
				t.Fatal(err)
			}
			raw, err := drawmodeldata.ExtractFeatures(input)
			if err != nil {
				t.Fatal(err)
			}
			prediction := drawmodeldata.Predict(raw)
			if !score.Valid || math.Abs(score.Float64-prediction.Score) > 1e-12 || (state == "selected") != prediction.Selected {
				t.Fatalf("frozen model replay mismatch: %s", matchID)
			}
			expectedSecondary := prediction.Selected && prediction.Score >= drawObservationSecondaryThreshold
			if secondary != expectedSecondary {
				t.Fatalf("secondary decision changed: %s", matchID)
			}
			var scores []drawRecommendedScore
			if err := json.Unmarshal(scoreJSON, &scores); err != nil {
				t.Fatalf("scorelines are not a JSON array: %v", err)
			}
			if secondary && !reflect.DeepEqual(scores, []drawRecommendedScore{{Home: 1, Away: 1}, {Home: 0, Away: 0}}) || !secondary && len(scores) != 0 {
				t.Fatalf("two-score baseline leaked onto an unselected match: %s", matchID)
			}
			expectedValues, _ := researchFeatureValues(raw)
			// MySQL's binary JSON parser can round a decimal float by an IEEE
			// last bit. The original raw input is retained losslessly as a blob.
			// Compare the query copy against the exact database JSON contract,
			// then independently verify both numerical representations infer
			// the same original score and decisions. No value tolerance is
			// introduced into the JSON/NULL/schema comparison.
			var normalizedExpected []byte
			if err := store.DB().QueryRowContext(ctx, "SELECT CAST(? AS JSON)", string(expectedValues)).Scan(&normalizedExpected); err != nil {
				t.Fatal(err)
			}
			var before, after map[string]*float64
			if json.Unmarshal(archivedValues, &before) != nil || json.Unmarshal(normalizedExpected, &after) != nil || !reflect.DeepEqual(before, after) {
				t.Logf("value dictionaries: stored=%d expected=%d", len(before), len(after))
				differences := 0
				for key, wanted := range after {
					stored, ok := before[key]
					if !ok || (stored == nil) != (wanted == nil) {
						t.Logf("value presence mismatch: %s present=%v storedNull=%v expectedNull=%v", key, ok, stored == nil, wanted == nil)
						differences++
					} else if stored != nil && *stored != *wanted {
						t.Logf("value differs: %s stored=%.17g expected=%.17g delta=%.17g", key, *stored, *wanted, *stored-*wanted)
						differences++
					}
					if differences >= 5 {
						break
					}
				}
				t.Fatalf("archived values differ from their source input: %s", matchID)
			}
			var originalValues map[string]*float64
			if err := json.Unmarshal(expectedValues, &originalValues); err != nil {
				t.Fatal(err)
			}
			storedFeatures := map[string]float64{}
			for name, stored := range before {
				if stored == nil {
					continue
				}
				storedFeatures[name] = *stored
				if original := originalValues[name]; original != nil && *original != *stored {
					roundTrippedFields++
					maxJSONRoundTripDifference = math.Max(maxJSONRoundTripDifference, math.Abs(*original-*stored))
				}
			}
			storedPrediction := drawmodeldata.Predict(storedFeatures)
			if math.Abs(storedPrediction.Score-score.Float64) > 1e-12 || storedPrediction.Selected != prediction.Selected || (storedPrediction.Selected && storedPrediction.Score >= drawObservationSecondaryThreshold) != secondary {
				t.Fatalf("database JSON numerical representation changed frozen model inference: %s rawScore=%.17g storedScore=%.17g", matchID, prediction.Score, storedPrediction.Score)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(input, &object); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"result", "homeScore", "awayScore"} {
				if _, ok := object[key]; ok {
					t.Fatalf("target label in archived input: %s/%s", matchID, key)
				}
			}
			replayed++
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if replayed == 0 {
			t.Skip("PENDING: no available lottery predictions archived yet; numerical replay not performed")
		}
		t.Logf("replayed=%d snapshotCount=%d; no live cutoff/evidence violations; MySQL JSON last-bit changes=%d maxAbsoluteDifference=%.17g; raw and query-vector model decisions identical", replayed, snapshots, roundTrippedFields, maxJSONRoundTripDifference)
	})
}

func assertResearchJSONObject(t *testing.T, raw json.RawMessage, name string) {
	t.Helper()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		t.Fatalf("%s must be a JSON object, not a quoted/base64 string: %s", name, trimmed)
	}
}

func assertResearchRate(t *testing.T, actual *float64, numerator, denominator int, name string) {
	t.Helper()
	if denominator == 0 {
		if actual != nil {
			t.Fatalf("%s has no denominator and must be null", name)
		}
		return
	}
	if actual == nil || math.Abs(*actual-float64(numerator)/float64(denominator)) > 1e-12 {
		t.Fatalf("%s denominator mismatch: %d/%d %#v", name, numerator, denominator, actual)
	}
}
