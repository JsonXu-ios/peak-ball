package handlers

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"
	"time"

	"go_server/internal/drawfullmodel"
)

func TestComprehensiveServingReadinessIsIndependent(t *testing.T) {
	if !drawComprehensiveStoredDayReady(drawObservationStoredDay{}) {
		t.Fatal("empty days must not rebuild for an unused experimental model")
	}
	info := drawfullmodel.Info()
	original := &drawObservationFrozenSignal{State: "selected", ModelVersion: "keep-original", Score: drawTestPtr(.31)}
	row := drawObservationItem{MatchID: "separate", FrozenDrawSignal: original}
	saved := drawObservationStoredDay{Items: []drawObservationItem{row}}
	if drawComprehensiveStoredDayReady(saved) {
		t.Fatal("old durable rows without comprehensive input were marked current")
	}
	saved.ComprehensiveVersion = drawComprehensiveServingVersion()
	for _, state := range []string{"selected", "not_selected", "unavailable"} {
		saved.Items[0].ComprehensiveSignal = &drawComprehensiveSignal{ModelVersion: info.Version, InputVersion: info.InputVersion, State: state}
		if !drawComprehensiveStoredDayReady(saved) {
			t.Fatalf("completed new calculation must persist its %s state", state)
		}
	}
	saved.Items[0].ComprehensiveSignal.ModelVersion = "old-experiment"
	if drawComprehensiveStoredDayReady(saved) {
		t.Fatal("experimental model version changed without upgrading its result")
	}
	if original.State != "selected" || *original.Score != .31 || saved.Items[0].FrozenDrawSignal != original {
		t.Fatal("experimental readiness changed the original signal")
	}
}

func TestComprehensiveServingPatchesOnlyItsOwnRawInput(t *testing.T) {
	if !drawfullmodel.Info().Available {
		t.Skip("independent artifact not trained yet")
	}
	item := drawObservationItem{MatchID: "actual-fixture", MatchTime: time.Date(2026, 11, 4, 12, 15, 0, 0, time.UTC),
		HomeRecent: drawObservationRecent{Matches: []drawObservationRecentMatch{}}, AwayRecent: drawObservationRecent{Matches: []drawObservationRecentMatch{}},
		HeadToHead: drawObservationRecent{Matches: []drawObservationRecentMatch{}}, FrozenDrawSignal: &drawObservationFrozenSignal{State: "selected", Score: drawTestPtr(.3)}}
	raw := json.RawMessage(`{"matchId":"other","matchTime":"2027-01-01T00:00:00Z","analysisInputs":{"averageLatest":[3.76,3.76,1.78]},"preMatchHistory":{"headToHead":{"matches":[{"matchId":"future","matchTime":"2026-12-01T00:00:00Z","homeScore":9,"awayScore":9,"venue":"home"}]}}}`)
	beforeItem, _ := json.Marshal(item)
	beforeRaw := append([]byte(nil), raw...)
	got := drawComprehensiveSignalForItem(raw, item)
	var exact map[string]json.RawMessage
	_ = json.Unmarshal(raw, &exact)
	exact["matchId"], _ = json.Marshal(item.MatchID)
	exact["matchTime"], _ = json.Marshal(item.MatchTime)
	exact["preMatchHistory"], _ = json.Marshal(drawFeatureStudyPreMatchHistory{HomeRecent: item.HomeRecent, AwayRecent: item.AwayRecent, HeadToHead: item.HeadToHead})
	wantRaw, _ := json.Marshal(exact)
	if want := drawObservationComprehensiveSignal(wantRaw); !reflect.DeepEqual(got, want) {
		t.Fatal("new signal did not use the exact list row's safe history")
	}
	afterItem, _ := json.Marshal(item)
	if string(beforeItem) != string(afterItem) || string(beforeRaw) != string(raw) {
		t.Fatal("new serving projection mutated original row or shared raw input")
	}
}

func TestComprehensiveRetiredKeepsCachedDayAndOriginalResults(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	loaders := fake.loaders()
	// Research remains enabled for the original model, but the abandoned
	// comprehensive experiment must no longer invalidate the durable day.
	loaders.research = true
	reader := newDrawObservationResultReader(store, drawResultTestSource, loaders)
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", size: 200}
	before, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := drawResultTestSource(q)
	key := source.Namespace + ":day:" + q.date
	saved, err := reader.storedDay(key, q.date, source.Day)
	if err != nil || saved == nil {
		t.Fatal("new day was not persisted", err)
	}
	saved.ComprehensiveVersion = "retired-experiment-version"
	for i := range saved.Items {
		saved.Items[i].ComprehensiveSignal = &drawComprehensiveSignal{State: "selected", ModelVersion: "retired-experiment-version"}
	}
	packed, err := drawObservationPack(func(w io.Writer) error { return json.NewEncoder(w).Encode(saved) })
	if err != nil {
		t.Fatal(err)
	}
	if err := store.set(context.Background(), key, packed); err != nil {
		t.Fatal(err)
	}
	fresh := newDrawObservationResultReader(store, drawResultTestSource, loaders)
	after, err := fresh.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 || len(store.values) != 2 {
		t.Fatalf("retired experiment rebuilt a reusable original day: base=%d day=%d keys=%d", fake.baseCalls.Load(), fake.targetCalls.Load(), len(store.values))
	}
	if len(before.Items) != len(after.Items) || !reflect.DeepEqual(before.ObservationMetrics, after.ObservationMetrics) || !reflect.DeepEqual(before.Summary, after.Summary) {
		t.Fatal("experimental upgrade changed original match scope or metrics")
	}
	for i := range before.Items {
		if after.Items[i].ComprehensiveSignal != nil {
			t.Fatal("retired experimental result leaked from a cached day")
		}
		before.Items[i].ComprehensiveSignal, after.Items[i].ComprehensiveSignal = nil, nil
	}
	if !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("experimental upgrade changed original row/model/secondary recommendations")
	}
	if _, err := fresh.read(q); err != nil {
		t.Fatal(err)
	}
	if fake.targetCalls.Load() != 1 {
		t.Fatal("retired artifact caused repeated durable-day rebuilding")
	}
}
