package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type drawResultMemoryStore struct {
	mu                 sync.Mutex
	values             map[string][]byte
	getError, setError error
}

func newDrawResultMemoryStore() *drawResultMemoryStore {
	return &drawResultMemoryStore{values: map[string][]byte{}}
}
func (s *drawResultMemoryStore) get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.values[key]...), s.getError
}
func (s *drawResultMemoryStore) set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setError != nil {
		return s.setError
	}
	s.values[key] = append([]byte(nil), value...)
	return nil
}
func drawResultTestSource(q drawObservationQuery) (drawObservationSourceVersion, error) {
	return drawObservationSourceVersion{Namespace: "test:draw", Core: "core-1", Day: "day-1:" + q.date}, nil
}
func drawResultComparable(t *testing.T, response drawObservationResponse) interface{} {
	t.Helper()
	response.DataVersion = ""
	response.GeneratedAt = nil
	// The internal page generator is intentionally not a metrics endpoint. The
	// durable day derives complete-window metrics once all pages are present;
	// dedicated metrics tests compare that field independently.
	response.ObservationMetrics = nil
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var value interface{}
	if err = json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestDrawObservationsPersistenceOneDayServesAllTabsScopesFiltersAndPages(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.targets[0].Match.JingcaiID = "周三001"
	fake.targets[1].Match.JingcaiID = "周三002"
	fake.targets[0].Match.League = "Test League"
	fake.targets[1].Match.League = "Other League"
	fake.marketLine = "0"
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	canonical := newDrawObservationCache(fake.loaders(), time.Hour, time.Now)
	zero, one := 0, 1
	cases := []drawObservationQuery{
		{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 200},
		{date: "2026-10-21", mode: "history", scope: "all", page: 1, size: 1},
		{date: "2026-10-21", mode: "history", scope: "all", page: 2, size: 1},
		{date: "2026-10-21", mode: "history", scope: "sporttery", page: 1, size: 50},
		{date: "2026-10-21", mode: "today", scope: "sporttery", league: "Test League", search: "day-0", page: 1, size: 50},
		{date: "2026-10-21", mode: "history", scope: "all", homeDraws: drawObservationRange{min: &zero, max: &one}, page: 1, size: 50},
		{date: "2026-10-21", mode: "history", scope: "all", h2hDraws: drawObservationRange{min: &zero, max: &zero}, page: 1, size: 50},
		{date: "2026-10-21", mode: "today", scope: "all", page: 999, size: 1},
	}
	// Reuse a day stored in the old descending order, including its page
	// boundaries, without rebuilding the source data.
	source, _ := drawResultTestSource(cases[0])
	saved, err := reader.buildDay(cases[0], source, false)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(saved.Items)-1; i < j; i, j = i+1, j-1 {
		saved.Items[i], saved.Items[j] = saved.Items[j], saved.Items[i]
	}
	encoded, err := drawObservationPack(func(w io.Writer) error { return json.NewEncoder(w).Encode(saved) })
	if err != nil {
		t.Fatal(err)
	}
	if err = store.set(context.Background(), source.Namespace+":day:"+saved.Date, encoded); err != nil {
		t.Fatal(err)
	}
	for _, q := range cases {
		got, err := reader.read(q)
		if err != nil {
			t.Fatal(err)
		}
		want, err := canonical.read(q)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(drawResultComparable(t, got), drawResultComparable(t, want)) {
			t.Fatalf("durable filtering differs for %+v", q)
		}
		if got.DataVersion == "" || got.GeneratedAt == nil {
			t.Fatal("missing generation metadata")
		}
	}
	if len(store.values) != 2 {
		t.Fatalf("stored queries individually instead of base+one day: %d", len(store.values))
	}
}

func TestDrawObservationsPersistenceSurvivesNewReaderAndDifferentDateWithoutReparsing(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	first := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	a, err := first.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != 1 {
		t.Fatal("unexpected base calls")
	}
	// A new reader has no process memory from the first reader.
	restarted := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	b, err := restarted.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) || fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 {
		t.Fatal("known date regenerated after restart")
	}
	q.date = "2026-10-20"
	next, err := restarted.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if next.Total != 1 || fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 2 {
		t.Fatalf("new date reparsed global source: total=%d base=%d day=%d", next.Total, fake.baseCalls.Load(), fake.targetCalls.Load())
	}
	if len(store.values) != 3 {
		t.Fatal("missing second durable day")
	}
}

func TestDrawObservationsPersistenceInvalidatesQuotesWithoutStaleInProcessMarkets(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.marketLine = "0"
	store := newDrawResultMemoryStore()
	core, day := "core-1", "day-1"
	source := func(q drawObservationQuery) (drawObservationSourceVersion, error) {
		return drawObservationSourceVersion{Namespace: "test:draw", Core: core, Day: day + q.date}, nil
	}
	reader := newDrawObservationResultReader(store, source, fake.loaders())
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	first, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	fake.marketLine = "0.5"
	day = "day-2"
	second, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if *first.Items[0].AsianHandicap.Opening != "0" || *second.Items[0].AsianHandicap.Opening != "0.5" || fake.baseCalls.Load() != 1 || fake.marketCalls.Load() != 2 {
		t.Fatal("quote revision reused stale market or reparsed global history")
	}
	core, day = "core-2", "day-3"
	if _, err := reader.read(q); err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != 2 {
		t.Fatal("source history revision did not regenerate base")
	}
	q.refresh = true
	if _, err := reader.read(q); err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != 3 {
		t.Fatal("explicit regeneration was ignored")
	}
}

func TestDrawObservationsPersistenceConcurrentGenerationAndFailureRetry(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.baseEntered, fake.releaseBase = make(chan struct{}), make(chan struct{})
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	var group sync.WaitGroup
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			response, err := reader.read(q)
			if err == nil && response.Total != 3 {
				err = fmt.Errorf("unexpected total %d", response.Total)
			}
			failures <- err
		}()
	}
	<-fake.baseEntered
	close(fake.releaseBase)
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 {
		t.Fatalf("duplicate generation base=%d day=%d", fake.baseCalls.Load(), fake.targetCalls.Load())
	}
	broken := newDrawResultMemoryStore()
	broken.getError = errors.New("redis disconnected")
	reader = newDrawObservationResultReader(broken, drawResultTestSource, fake.loaders())
	if _, err := reader.read(q); err == nil {
		t.Fatal("Redis error hidden as successful persisted result")
	}
	broken.getError = nil
	broken.setError = errors.New("redis write failed")
	if _, err := reader.read(q); err == nil {
		t.Fatal("write failure hidden")
	}
	broken.setError = nil
	if _, err := reader.read(q); err != nil {
		t.Fatal("failed generation could not retry", err)
	}
}

func TestDrawObservationsPersistenceRejectsChangingSourcesAndRebuildsCorruption(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	calls := 0
	source := func(q drawObservationQuery) (drawObservationSourceVersion, error) {
		calls++
		return drawObservationSourceVersion{Namespace: "test:draw", Core: fmt.Sprint(calls), Day: fmt.Sprint(calls)}, nil
	}
	reader := newDrawObservationResultReader(store, source, fake.loaders())
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	if _, err := reader.read(q); err == nil || !strings.Contains(err.Error(), "正在更新") {
		t.Fatal("mixed source snapshot was published", err)
	}
	if _, ok := store.values["test:draw:day:2026-10-21"]; ok {
		t.Fatal("changing source day persisted")
	}
	reader = newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	store.values["test:draw:base"] = []byte("broken")
	store.values["test:draw:day:2026-10-21"] = []byte("broken")
	if response, err := reader.read(q); err != nil || response.Total != 3 {
		t.Fatal("corrupt own snapshot not rebuilt", err)
	}
}

func TestDrawObservationsPersistenceRetriesOneSourceUpdateBeforePublishing(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	calls := 0
	source := func(q drawObservationQuery) (drawObservationSourceVersion, error) {
		calls++
		version := "new"
		if calls == 1 {
			version = "old"
		}
		return drawObservationSourceVersion{Namespace: "test:draw", Core: version, Day: version}, nil
	}
	reader := newDrawObservationResultReader(store, source, fake.loaders())
	result, err := reader.read(drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50})
	if err != nil || result.DataVersion != "new" || fake.baseCalls.Load() != 2 {
		t.Fatalf("source update not retried safely: version=%s calls=%d error=%v", result.DataVersion, fake.baseCalls.Load(), err)
	}
}

func TestDrawObservationsPersistenceBulkForceParsesHistoryOnlyOnce(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	previous := drawObservationResults
	drawObservationResults = reader
	defer func() { drawObservationResults = previous }()
	if err := MaterializeDrawObservationDates([]string{"2026-10-21", "2026-10-20"}, true, nil); err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 2 {
		t.Fatalf("bulk rebuild repeated global parsing: base=%d days=%d", fake.baseCalls.Load(), fake.targetCalls.Load())
	}
}

func TestDrawObservationsPersistenceSnapshotSurvivesContinuousExternalUpdates(t *testing.T) {
	outside := newDrawObservationsFake()
	frozen := newDrawObservationsFake()
	frozen.marketLine = "0.25"
	store := newDrawResultMemoryStore()
	sourceCalls := 0
	reader := newDrawObservationResultReader(store, func(q drawObservationQuery) (drawObservationSourceVersion, error) {
		sourceCalls++
		return drawObservationSourceVersion{Namespace: "test:draw", Core: fmt.Sprint(sourceCalls), Day: fmt.Sprint(sourceCalls)}, nil
	}, outside.loaders())
	snapshots := 0
	reader.snapshot = func(q drawObservationQuery, build func(drawObservationSourceVersion, drawObservationLoaders) (*drawObservationStoredDay, error)) (*drawObservationStoredDay, error) {
		snapshots++
		// The transaction has its own fixed version even when the fast check
		// saw a preceding version and the crawler continues writing outside it.
		version := drawObservationSourceVersion{Namespace: "test:draw", Core: "snapshot", Day: "snapshot"}
		return build(version, frozen.loaders())
	}
	result, err := reader.read(drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50})
	if err != nil {
		t.Fatal("consistent transaction snapshot rejected concurrent source changes", err)
	}
	if sourceCalls != 1 || snapshots != 1 || result.DataVersion != "snapshot" || result.Total != 3 {
		t.Fatalf("snapshot was retried against live source: source=%d snapshots=%d result=%+v", sourceCalls, snapshots, result)
	}
	if outside.baseCalls.Load() != 0 || outside.targetCalls.Load() != 0 || frozen.baseCalls.Load() != 1 || frozen.targetCalls.Load() != 1 || *result.Items[0].AsianHandicap.Opening != "0.25" {
		t.Fatal("snapshot generation mixed transaction and outside loaders")
	}
	saved, err := reader.storedDay("test:draw:day:2026-10-21", "2026-10-21", "snapshot")
	if err != nil || saved == nil || saved.Version != result.DataVersion {
		t.Fatal("snapshot result was not persisted under its own version", err)
	}
}

func TestDrawObservationsPersistenceSnapshotFailureDoesNotPublishDayOrKeepClosedLoader(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.baseFailures.Store(1)
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	reader.snapshot = func(q drawObservationQuery, build func(drawObservationSourceVersion, drawObservationLoaders) (*drawObservationStoredDay, error)) (*drawObservationStoredDay, error) {
		closed := false
		loaders := fake.loaders()
		original := loaders.base
		loaders.base = func() (drawObservationBase, error) {
			if closed {
				return drawObservationBase{}, errors.New("transaction is already closed")
			}
			return original()
		}
		version, _ := drawResultTestSource(q)
		day, err := build(version, loaders)
		closed = true
		return day, err
	}
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	if _, err := reader.read(q); err == nil {
		t.Fatal("failed snapshot reported success")
	}
	if _, exists := store.values["test:draw:day:2026-10-21"]; exists {
		t.Fatal("failed snapshot published a durable day")
	}
	if result, err := reader.read(q); err != nil || result.Total != 3 {
		t.Fatalf("failed snapshot kept a closed transaction loader: total=%d error=%v", result.Total, err)
	}
}

func TestDrawObservationsPersistenceDoesNotPublishBeforeSnapshotCommit(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	reader.snapshot = func(q drawObservationQuery, build func(drawObservationSourceVersion, drawObservationLoaders) (*drawObservationStoredDay, error)) (*drawObservationStoredDay, error) {
		version, _ := drawResultTestSource(q)
		day, err := build(version, fake.loaders())
		if err != nil {
			return day, err
		}
		return day, errors.New("read transaction commit failed")
	}
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	if _, err := reader.read(q); err == nil || !strings.Contains(err.Error(), "commit failed") {
		t.Fatal("snapshot commit error hidden", err)
	}
	if _, exists := store.values["test:draw:day:2026-10-21"]; exists {
		t.Fatal("day was published before the snapshot committed")
	}
}

func TestDrawObservationsPersistenceSnapshotDifferentDateUsesCurrentTransactionLoaders(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	reader.snapshot = func(q drawObservationQuery, build func(drawObservationSourceVersion, drawObservationLoaders) (*drawObservationStoredDay, error)) (*drawObservationStoredDay, error) {
		closed := false
		loaders := fake.loaders()
		base, targets, coverage, quotes := loaders.base, loaders.targets, loaders.coverage, loaders.quotes
		loaders.base = func() (drawObservationBase, error) {
			if closed {
				return drawObservationBase{}, errors.New("closed transaction base")
			}
			return base()
		}
		loaders.targets = func(schema drawObservationSchema, query drawObservationQuery) ([]drawObservationTarget, error) {
			if closed {
				return nil, errors.New("closed transaction targets")
			}
			return targets(schema, query)
		}
		loaders.coverage = func(schema drawObservationSchema, ids []string) (map[string]drawObservationQuotesBundle, error) {
			if closed {
				return nil, errors.New("closed transaction coverage")
			}
			return coverage(schema, ids)
		}
		loaders.quotes = func(schema drawObservationSchema, ids []string) (map[string]drawObservationEuroOdds, error) {
			if closed {
				return nil, errors.New("closed transaction quotes")
			}
			return quotes(schema, ids)
		}
		version, _ := drawResultTestSource(q)
		day, err := build(version, loaders)
		closed = true
		return day, err
	}
	for _, date := range []string{"2026-10-21", "2026-10-20"} {
		if _, err := reader.read(drawObservationQuery{date: date, mode: "today", scope: "all", page: 1, size: 50}); err != nil {
			t.Fatal("different date retained closed transaction loaders", err)
		}
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 2 {
		t.Fatal("immutable base was not shared independently from transaction day loaders")
	}
}

func TestDrawObservationsPersistenceSnapshotPublishesOnlyInItsDatabaseNamespace(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	namespace := "database-before"
	source := func(q drawObservationQuery) (drawObservationSourceVersion, error) {
		return drawObservationSourceVersion{Namespace: namespace, Core: "core", Day: "day:" + q.date}, nil
	}
	reader := newDrawObservationResultReader(store, source, fake.loaders())
	snapshots := 0
	reader.snapshot = func(q drawObservationQuery, build func(drawObservationSourceVersion, drawObservationLoaders) (*drawObservationStoredDay, error)) (*drawObservationStoredDay, error) {
		snapshots++
		namespace = "database-snapshot"
		version, _ := source(q)
		return build(version, fake.loaders())
	}
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	if _, err := reader.read(q); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.values["database-before:day:2026-10-21"]; exists {
		t.Fatal("snapshot published in the fast check's old database namespace")
	}
	if _, exists := store.values["database-snapshot:day:2026-10-21"]; !exists {
		t.Fatal("snapshot missing from its own database namespace")
	}
	if _, err := reader.read(q); err != nil || snapshots != 1 {
		t.Fatal("database-specific persisted snapshot was not reused", err)
	}
}
