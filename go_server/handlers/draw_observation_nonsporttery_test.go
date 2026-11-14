package handlers

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"go_server/internal/drawmodeldata"
)

func nonSportteryModelFixture() (drawObservationItem, drawFeatureStudyRow) {
	item := drawObservationItem{MatchID: "non-lottery-model", Date: "2026-11-04", MatchTime: time.Date(2026, 11, 4, 18, 0, 0, 0, drawSourceLocation)}
	input := drawFeatureStudyRow{}
	input.AnalysisInputs.AverageOpening = []float64{2.1, 3.1, 3.3}
	input.AnalysisInputs.AverageLatest = []float64{2.2, 3.0, 3.1}
	return item, input
}

func TestNonSportterySignalScopeMissingAndExactFrozenCalculation(t *testing.T) {
	item, input := nonSportteryModelFixture()
	if got := drawObservationNonSportterySignal(item, nil); got == nil || got.State != "unavailable" || got.Score != nil {
		t.Fatalf("missing input fabricated a selection: %+v", got)
	}
	if got := drawObservationNonSportterySignal(item, &drawFeatureStudyRow{}); got == nil || got.State != "unavailable" || got.Score != nil {
		t.Fatalf("pure median fallback fabricated a selection: %+v", got)
	}
	got := drawObservationNonSportterySignal(item, &input)
	if got == nil || got.Score == nil || got.ObservedInputs == 0 {
		t.Fatalf("real non-lottery numeric inputs not scored: %+v", got)
	}
	// Build the same strict per-row input independently of either scoped entry.
	row := input
	row.MatchID, row.MatchTime = item.MatchID, item.MatchTime
	row.PreMatchHistory = &drawFeatureStudyPreMatchHistory{HomeRecent: item.HomeRecent, AwayRecent: item.AwayRecent, HeadToHead: item.HeadToHead}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := drawmodeldata.ExtractFeatures(encoded)
	if err != nil {
		t.Fatal(err)
	}
	expected := drawmodeldata.Predict(raw)
	if *got.Score != expected.Score || (got.State == "selected") != expected.Selected || got.ObservedInputs != expected.ObservedFeatures || got.UsedInputs != expected.UsedFeatures || got.Threshold != drawmodeldata.Threshold() || got.ModelVersion != drawmodeldata.Version() {
		t.Fatalf("non-lottery column changed frozen calculation: got=%+v expected=%+v", got, expected)
	}
	if original := drawObservationModelSignal(item, &input); original.State != "out_of_scope" || original.Score != nil {
		t.Fatalf("new column removed the original lottery scope guard: %+v", original)
	}
	lottery := item
	lottery.JingcaiID = "20261104001"
	if nonLottery := drawObservationNonSportterySignal(lottery, &input); nonLottery != nil {
		t.Fatalf("non-lottery column claimed a lottery fixture: %+v", nonLottery)
	}
	if original := drawObservationModelSignal(lottery, &input); !reflect.DeepEqual(original, got) {
		t.Fatalf("identical numeric inputs changed between scoped columns: lottery=%+v non-lottery=%+v", original, got)
	}
	item.JingcaiID = " \t "
	if whitespace := drawObservationNonSportterySignal(item, &input); !reflect.DeepEqual(whitespace, got) {
		t.Fatal("whitespace-only lottery ID was treated as a lottery fixture")
	}
}

func TestNonSportterySignalIgnoresTargetResultsAndDoesNotMutateInputs(t *testing.T) {
	item, input := nonSportteryModelFixture()
	input.MatchID = "older-enrichment-id"
	input.MatchTime = item.MatchTime.Add(-24 * time.Hour)
	input.PreMatchHistory = &drawFeatureStudyPreMatchHistory{HomeRecent: drawObservationRecent{Sample: 99, Draws: 99}}
	beforeInput, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	before := drawObservationNonSportterySignal(item, &input)
	item.HomeScore, item.AwayScore, item.Settled, item.Status = 17, 23, true, 999
	confirmed := true
	item.RegulationSettled = &confirmed
	after := drawObservationNonSportterySignal(item, &input)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("target scores changed selection: before=%+v after=%+v", before, after)
	}
	afterInput, err := json.Marshal(input)
	if err != nil || string(beforeInput) != string(afterInput) {
		t.Fatal("non-lottery calculation mutated the shared enrichment input")
	}
	canonical := input
	canonical.PreMatchHistory = nil
	if currentWindows := drawObservationNonSportterySignal(item, &canonical); !reflect.DeepEqual(currentWindows, after) {
		t.Fatal("stale enrichment history replaced the current row's strict preceding windows")
	}
}

func TestNonSportteryFieldCannotChangeLotteryMetricsSecondaryOrScoreRules(t *testing.T) {
	jc := drawMetricFixture("lottery-still-original", "selected", drawMetricScore(.224), 1, 1, true)
	non := drawMetricFixture("non-lottery-new-column", "out_of_scope", nil, 0, 0, true)
	non.JingcaiID = ""
	query := drawObservationQuery{date: jc.Date, scope: "all"}
	before := buildDrawObservationMetrics([]drawObservationItem{jc, non}, query)
	secondary := drawObservationSecondaryDecision(jc)
	scores := drawObservationScoreRecommendation(jc)
	// Even a malformed caller attaching the new field to a lottery fixture must
	// not make any existing lottery consumer read it instead of FrozenDrawSignal.
	jc.NonSportterySignal = &drawObservationFrozenSignal{State: "selected", Score: drawMetricScore(.99)}
	non.NonSportterySignal = &drawObservationFrozenSignal{State: "selected", Score: drawMetricScore(.99)}
	after := buildDrawObservationMetrics([]drawObservationItem{jc, non}, query)
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(secondary, drawObservationSecondaryDecision(jc)) || !reflect.DeepEqual(scores, drawObservationScoreRecommendation(jc)) {
		t.Fatal("independent non-lottery field changed original lottery counts or decisions")
	}
	if got := drawObservationSecondaryDecision(non); got.Decision != "out_of_scope" {
		t.Fatalf("non-lottery signal escaped into lottery secondary screening: %+v", got)
	}
}

func TestNonSportteryStoredDayReadinessAllowsUnavailableAndLotteryOnly(t *testing.T) {
	jc := drawObservationItem{MatchID: "jc", JingcaiID: "20261104001"}
	non := drawObservationItem{MatchID: "non"}
	if !drawNonSportteryStoredDayReady(drawObservationStoredDay{}) || !drawNonSportteryStoredDayReady(drawObservationStoredDay{Items: []drawObservationItem{jc}}) {
		t.Fatal("empty or lottery-only durable days were unnecessarily invalidated")
	}
	day := drawObservationStoredDay{Items: []drawObservationItem{jc, non}}
	if drawNonSportteryStoredDayReady(day) {
		t.Fatal("old mixed day without non-lottery model field was accepted")
	}
	day.NonSportteryVersion = drawObservationNonSportteryVersion
	if drawNonSportteryStoredDayReady(day) {
		t.Fatal("version alone accepted a missing non-lottery model field")
	}
	for _, state := range []string{"selected", "not_selected", "unavailable"} {
		day.Items[1].NonSportterySignal = &drawObservationFrozenSignal{State: state}
		if !drawNonSportteryStoredDayReady(day) {
			t.Fatalf("completed non-lottery state %s would cause permanent rebuilding", state)
		}
	}
	day.Items[1].NonSportterySignal.State = "out_of_scope"
	if drawNonSportteryStoredDayReady(day) {
		t.Fatal("old scope-rejected non-lottery signal was accepted")
	}
}

func nonSportteryPersistDay(t *testing.T, store *drawResultMemoryStore, saved *drawObservationStoredDay) {
	t.Helper()
	encoded, err := drawObservationPack(func(w io.Writer) error { return json.NewEncoder(w).Encode(saved) })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.set(context.Background(), "test:draw:day:"+saved.Date, encoded); err != nil {
		t.Fatal(err)
	}
}

func TestNonSportteryDurableUpgradeReusesBaseAndSurvivesNewReader(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.targets[0].Match.JingcaiID = "20261021001"
	loaders := fake.loaders()
	var frontendCalls atomic.Int32
	loaders.frontend = func(_ drawObservationSchema, targets []drawObservationTarget) (map[string]drawObservationFrontendEnrichment, error) {
		frontendCalls.Add(1)
		result := map[string]drawObservationFrontendEnrichment{}
		for _, target := range targets {
			_, input := nonSportteryModelFixture()
			result[target.Match.MatchId] = drawObservationFrontendEnrichment{modelInput: &input}
		}
		return result, nil
	}
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, loaders)
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	first, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := drawResultTestSource(q)
	saved, err := reader.storedDay(source.Namespace+":day:"+q.date, q.date, source.Day)
	if err != nil || saved == nil {
		t.Fatalf("first day was not durable: %v", err)
	}
	for _, row := range first.Items {
		if row.JingcaiID == "" {
			if row.NonSportterySignal == nil || row.NonSportterySignal.Score == nil || row.FrozenDrawSignal == nil || row.FrozenDrawSignal.State != "out_of_scope" {
				t.Fatalf("new non-lottery result leaked into original column or was omitted: %+v", row)
			}
		} else if row.NonSportterySignal != nil {
			t.Fatal("lottery fixture has independent non-lottery result")
		}
	}
	// Reconstruct an old JSON day, preserving its existing source and base keys.
	saved.NonSportteryVersion = ""
	for index := range saved.Items {
		saved.Items[index].NonSportterySignal = nil
	}
	nonSportteryPersistDay(t, store, saved)
	upgradedReader := newDrawObservationResultReader(store, drawResultTestSource, loaders)
	upgraded, err := upgradedReader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 2 || frontendCalls.Load() != 2 {
		t.Fatalf("day upgrade discarded global base or duplicated reads: base=%d day=%d front=%d", fake.baseCalls.Load(), fake.targetCalls.Load(), frontendCalls.Load())
	}
	if !reflect.DeepEqual(drawResultComparable(t, first), drawResultComparable(t, upgraded)) {
		t.Fatal("upgrading an old mixed day changed the response beyond regeneration metadata")
	}
	for _, query := range []drawObservationQuery{q, {date: q.date, mode: "history", scope: "sporttery", page: 1, size: 50}} {
		freshReader := newDrawObservationResultReader(store, drawResultTestSource, loaders)
		if _, err := freshReader.read(query); err != nil {
			t.Fatal(err)
		}
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 2 || frontendCalls.Load() != 2 {
		t.Fatal("upgraded day was recomputed after reader recreation or a scope/tab change")
	}
}

func TestNonSportteryUnavailableDurableDayDoesNotRebuildForever(t *testing.T) {
	fake := newDrawObservationsFake()
	// No frontend loader means no usable enrichment input; this is a completed
	// unavailable observation, and must still survive JSON/Redis roundtripping.
	store := newDrawResultMemoryStore()
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	first, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range first.Items {
		if row.NonSportterySignal == nil || row.NonSportterySignal.State != "unavailable" {
			t.Fatalf("missing input did not produce explicit unavailable state: %+v", row.NonSportterySignal)
		}
	}
	for index := 0; index < 3; index++ {
		newReader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
		got, err := newReader.read(q)
		if err != nil || !reflect.DeepEqual(got, first) {
			t.Fatalf("unavailable durable result changed on reload: %v", err)
		}
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 {
		t.Fatal("unavailable non-lottery result caused permanent rebuilding")
	}
}

func TestNonSportteryOldLotteryOnlyDayReusesDurableResult(t *testing.T) {
	fake := newDrawObservationsFake()
	for index := range fake.targets {
		fake.targets[index].Match.JingcaiID = "20261021001"
	}
	store := newDrawResultMemoryStore()
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	first, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := drawResultTestSource(q)
	saved, err := reader.storedDay(source.Namespace+":day:"+q.date, q.date, source.Day)
	if err != nil || saved == nil {
		t.Fatalf("lottery day missing: %v", err)
	}
	saved.NonSportteryVersion = ""
	nonSportteryPersistDay(t, store, saved)
	newReader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	got, err := newReader.read(q)
	if err != nil || !reflect.DeepEqual(got, first) {
		t.Fatalf("non-lottery upgrade altered a lottery-only day: %v", err)
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 {
		t.Fatal("adding non-lottery support invalidated a lottery-only durable day")
	}
}
