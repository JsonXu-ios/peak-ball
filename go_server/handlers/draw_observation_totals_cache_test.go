package handlers

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"

	"go_server/internal/drawmodeldata"
	"go_server/internal/jcgoalsmodel"
)

func totalsCachePrediction(state string) *drawObservationTotalsPrediction {
	return &drawObservationTotalsPrediction{
		ModelVersion:      jcgoalsmodel.Version,
		State:             state,
		Direction:         "under",
		Line:              2.25,
		OverWater:         0.88,
		UnderWater:        0.98,
		Score:             0.576,
		ExpectedNetReturn: -0.0123,
		Reason:            "独立大小球组合模型",
	}
}

func totalsCacheDay(jingcaiID string, prediction *drawObservationTotalsPrediction) drawObservationStoredDay {
	day := drawObservationStoredDay{
		Version:             "totals-test-source-day",
		Date:                "2026-11-04",
		GeneratedAt:         time.Date(2026, 11, 5, 8, 0, 0, 0, time.UTC),
		NonSportteryVersion: drawObservationNonSportteryVersion,
		TotalsVersion:       drawTotalsServingVersion(),
		SelectionVersion:    drawObservationSelectionVersion,
		Items: []drawObservationItem{{
			MatchID:            "totals-cache-fixture",
			JingcaiID:          jingcaiID,
			Date:               "2026-11-04",
			MatchTime:          time.Date(2026, 11, 4, 12, 15, 0, 0, time.UTC),
			TotalsPrediction:   prediction,
			NonSportterySignal: &drawObservationFrozenSignal{State: "unavailable", ModelVersion: drawmodeldata.Version()},
		}},
	}
	day.Items[0].FrozenDrawSignal = drawObservationApplySelectionEligibility(day.Items[0], &drawObservationFrozenSignal{State: "unavailable", ModelVersion: drawmodeldata.Version()})
	return day
}

func totalsCacheStoreDay(t *testing.T, store *drawResultMemoryStore, key string, day drawObservationStoredDay) []byte {
	t.Helper()
	packed, err := drawObservationPack(func(w io.Writer) error { return json.NewEncoder(w).Encode(day) })
	if err != nil {
		t.Fatal(err)
	}
	if err = store.set(context.Background(), key, packed); err != nil {
		t.Fatal(err)
	}
	return packed
}

func TestDrawTotalsStoredDayReadyVersionAndStateContract(t *testing.T) {
	current := totalsCacheDay("20261104001", totalsCachePrediction("recommended"))
	missingField := totalsCacheDay("20261104001", nil)
	oldDay := totalsCacheDay("20261104001", nil)
	oldDay.TotalsVersion = ""
	oldArtifact := current
	oldArtifact.TotalsVersion = drawTotalsServingVersion() + ":different-artifact"
	oldModel := totalsCacheDay("20261104001", totalsCachePrediction("recommended"))
	oldModel.Items[0].TotalsPrediction.ModelVersion = "older-goals-model"
	missingModel := totalsCacheDay("20261104001", &drawObservationTotalsPrediction{State: "recommended"})
	unavailable := totalsCacheDay("20261104001", &drawObservationTotalsPrediction{
		ModelVersion: jcgoalsmodel.Version,
		State:        "unavailable",
		Reason:       "缺少完整且未停售的竞彩总进球赔率",
	})
	nonSporttery := totalsCacheDay("", nil)
	nonSporttery.TotalsVersion = ""
	whitespaceNonSporttery := totalsCacheDay(" \t ", nil)
	whitespaceNonSporttery.TotalsVersion = "old-totals-version"
	minimalRecommended := totalsCacheDay("20261104001", &drawObservationTotalsPrediction{
		ModelVersion: jcgoalsmodel.Version,
		State:        "recommended",
	})
	// Readiness intentionally validates the calculation version and completed
	// state. Recommendation-content validation belongs to its producer/UI.
	cases := []struct {
		name string
		day  drawObservationStoredDay
		want bool
	}{
		{"recommended_complete", current, true},
		{"unavailable_is_completed", unavailable, true},
		{"legacy_lottery_missing_marker_and_field", oldDay, false},
		{"current_lottery_missing_field", missingField, false},
		{"changed_artifact_rebuilds", oldArtifact, false},
		{"changed_model_version_rebuilds", oldModel, false},
		{"missing_model_version_rebuilds", missingModel, false},
		{"legacy_non_lottery_needs_no_upgrade", nonSporttery, true},
		{"whitespace_lottery_id_is_out_of_scope", whitespaceNonSporttery, true},
		{"recommended_without_payload_fields_follows_state_only_contract", minimalRecommended, true},
		{"lottery_out_of_scope_state_is_invalid", totalsCacheDay("20261104001", totalsCachePrediction("out_of_scope")), false},
		{"unknown_state_rebuilds", totalsCacheDay("20261104001", totalsCachePrediction("unknown")), false},
		{"missing_state_rebuilds", totalsCacheDay("20261104001", totalsCachePrediction("")), false},
		{"empty_day_needs_no_upgrade", drawObservationStoredDay{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := drawTotalsStoredDayReady(tc.day); got != tc.want {
				t.Fatalf("drawTotalsStoredDayReady = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDrawTotalsStoredDayRejectsOldLotteryButKeepsUnavailableAndNonLottery(t *testing.T) {
	for _, tc := range []struct {
		name string
		day  drawObservationStoredDay
		want bool
	}{
		{"old_lottery_without_new_field", func() drawObservationStoredDay {
			day := totalsCacheDay("20261104001", nil)
			day.TotalsVersion = ""
			return day
		}(), false},
		{"new_unavailable_is_retained", totalsCacheDay("20261104001", &drawObservationTotalsPrediction{
			ModelVersion: jcgoalsmodel.Version, State: "unavailable", Reason: "暂无有效盘口",
		}), true},
		{"old_non_lottery_still_retained", func() drawObservationStoredDay {
			day := totalsCacheDay("", nil)
			day.TotalsVersion = ""
			return day
		}(), true},
		{"different_artifact_invalidates_day", func() drawObservationStoredDay {
			day := totalsCacheDay("20261104001", totalsCachePrediction("recommended"))
			day.TotalsVersion = "previous-artifact"
			return day
		}(), false},
		{"recommended_survives_store", totalsCacheDay("20261104001", totalsCachePrediction("recommended")), true},
		{"current_marker_but_missing_field_invalidates_day", totalsCacheDay("20261104001", nil), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newDrawResultMemoryStore()
			key := "test:totals:day:" + tc.day.Date
			totalsCacheStoreDay(t, store, key, tc.day)
			reader := newDrawObservationResultReader(store, nil, drawObservationLoaders{})
			got, err := reader.storedDay(key, tc.day.Date, tc.day.Version)
			if err != nil {
				t.Fatal(err)
			}
			if (got != nil) != tc.want {
				t.Fatalf("storedDay returned day=%v, want retained=%v", got != nil, tc.want)
			}
		})
	}
}

func TestDrawTotalsPersistenceUpgradesOnceAndReusesUnavailable(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.targets[0].Match.JingcaiID = "20261021001"
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	source, err := drawResultTestSource(q)
	if err != nil {
		t.Fatal(err)
	}
	day, err := reader.buildDay(q, source, false)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the parsed-history base but simulate a durable day written before
	// the new column existed. No source revision or global namespace changes.
	day.TotalsVersion = ""
	for i := range day.Items {
		day.Items[i].TotalsPrediction = nil
	}
	key := source.Namespace + ":day:" + q.date
	totalsCacheStoreDay(t, store, key, *day)
	if old, err := reader.storedDay(key, q.date, source.Day); err != nil || old != nil {
		t.Fatalf("legacy lottery day was reused: day=%v err=%v", old != nil, err)
	}
	baseBefore, targetsBefore := fake.baseCalls.Load(), fake.targetCalls.Load()
	response, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != baseBefore || fake.targetCalls.Load() != targetsBefore+1 {
		t.Fatalf("upgrade should reuse base and rebuild one day: base=%d->%d targets=%d->%d",
			baseBefore, fake.baseCalls.Load(), targetsBefore, fake.targetCalls.Load())
	}
	found := false
	for _, row := range response.Items {
		if row.JingcaiID != "" {
			found = true
			if row.TotalsPrediction == nil || row.TotalsPrediction.State != "unavailable" {
				t.Fatalf("missing inputs must become explicit unavailable: %+v", row.TotalsPrediction)
			}
		}
	}
	if !found {
		t.Fatal("fixture did not exercise a lottery row")
	}
	saved, err := reader.storedDay(key, q.date, source.Day)
	if err != nil || saved == nil || saved.TotalsVersion != drawTotalsServingVersion() {
		t.Fatalf("upgraded unavailable result was not persisted: day=%v err=%v", saved, err)
	}
	baseAfter, targetsAfter := fake.baseCalls.Load(), fake.targetCalls.Load()
	marketAfter, quotesAfter := fake.marketCalls.Load(), fake.quoteCalls.Load()
	// A fresh reader has no in-process generation cache. Repeated reads must
	// still accept the persisted unavailable decision without recalculation.
	next := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	for i := 0; i < 3; i++ {
		if _, err := next.read(q); err != nil {
			t.Fatal(err)
		}
	}
	if fake.baseCalls.Load() != baseAfter || fake.targetCalls.Load() != targetsAfter || fake.marketCalls.Load() != marketAfter || fake.quoteCalls.Load() != quotesAfter {
		t.Fatal("completed unavailable totals decision caused repeated source reads")
	}
}

func TestDrawTotalsPersistenceGzipRetainsPredictionAndVersion(t *testing.T) {
	day := totalsCacheDay("20261104001", totalsCachePrediction("recommended"))
	store := newDrawResultMemoryStore()
	key := "test:totals:gzip:day:" + day.Date
	packed := totalsCacheStoreDay(t, store, key, day)
	if len(packed) < 2 || packed[0] != 0x1f || packed[1] != 0x8b {
		t.Fatal("test did not exercise the actual gzip persistence format")
	}
	reader := newDrawObservationResultReader(store, nil, drawObservationLoaders{})
	got, err := reader.storedDay(key, day.Date, day.Version)
	if err != nil || got == nil {
		t.Fatalf("gzip totals result unreadable: day=%v err=%v", got, err)
	}
	if got.TotalsVersion != day.TotalsVersion || !reflect.DeepEqual(got.Items[0].TotalsPrediction, day.Items[0].TotalsPrediction) {
		t.Fatalf("totals version or recommendation changed after gzip/JSON round trip: got=%+v want=%+v", got.Items[0].TotalsPrediction, day.Items[0].TotalsPrediction)
	}
}
