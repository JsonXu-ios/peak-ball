package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/models"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type drawObservationsFake struct {
	mu                                                          sync.Mutex
	targets                                                     []drawObservationTarget
	history                                                     []drawHistoricalGame
	baseCalls, targetCalls, marketCalls, quoteCalls             atomic.Int32
	baseFailures, targetFailures, marketFailures, quoteFailures atomic.Int32
	baseEntered, releaseBase                                    chan struct{}
	enteredOnce                                                 sync.Once
	quoteIDs                                                    [][]string
	marketLine                                                  string
}

func newDrawObservationsFake() *drawObservationsFake {
	fake := &drawObservationsFake{}
	base := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		h, a := fmt.Sprint(i*2+1), fmt.Sprint(i*2+2)
		fake.targets = append(fake.targets, drawObservationMain(drawObservationGame(fmt.Sprintf("day-%d", i), h, a, base.Add(-time.Duration(i)*time.Hour), 1, 1)))
		fake.history = append(fake.history, drawObservationGame(fmt.Sprintf("old-%d", i), h, a, base.Add(-7*24*time.Hour), i%2, 0))
	}
	fake.targets = append(fake.targets, drawObservationMain(drawObservationGame("previous-day", "7", "8", base.Add(-24*time.Hour), 0, 0)))
	return fake
}

func (fake *drawObservationsFake) loaders() drawObservationLoaders {
	return drawObservationLoaders{
		base: func() (drawObservationBase, error) {
			fake.baseCalls.Add(1)
			if fake.baseFailures.Add(-1) >= 0 {
				return drawObservationBase{}, errors.New("in-memory base failure")
			}
			if fake.baseEntered != nil {
				fake.enteredOnce.Do(func() { close(fake.baseEntered) })
			}
			if fake.releaseBase != nil {
				<-fake.releaseBase
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			return drawObservationBase{targets: append([]drawObservationTarget{}, fake.targets...), history: append([]drawHistoricalGame{}, fake.history...)}, nil
		},
		targets: func(_ drawObservationSchema, q drawObservationQuery) ([]drawObservationTarget, error) {
			fake.targetCalls.Add(1)
			if fake.targetFailures.Add(-1) >= 0 {
				return nil, errors.New("in-memory target failure")
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			rows := []drawObservationTarget{}
			for _, target := range fake.targets {
				if drawObservationDateMatches(drawObservationDate(target.Match), q) {
					rows = append(rows, target)
				}
			}
			return rows, nil
		},
		coverage: func(_ drawObservationSchema, ids []string) (map[string]drawObservationQuotesBundle, error) {
			fake.marketCalls.Add(1)
			if fake.marketFailures.Add(-1) >= 0 {
				return nil, errors.New("in-memory market failure")
			}
			rows := map[string]drawObservationQuotesBundle{}
			fake.mu.Lock()
			line := fake.marketLine
			fake.mu.Unlock()
			for _, id := range ids {
				asian, total := drawObservationLines(models.PankouMoney{})
				if line != "" {
					asian.Source, asian.Opening = "pankou_moneys", &line
				}
				rows[id] = drawObservationQuotesBundle{asian: asian, total: total}
			}
			return rows, nil
		},
		quotes: func(_ drawObservationSchema, ids []string) (map[string]drawObservationEuroOdds, error) {
			fake.quoteCalls.Add(1)
			if fake.quoteFailures.Add(-1) >= 0 {
				return nil, errors.New("in-memory quote failure")
			}
			fake.mu.Lock()
			fake.quoteIDs = append(fake.quoteIDs, append([]string{}, ids...))
			fake.mu.Unlock()
			rows := map[string]drawObservationEuroOdds{}
			for _, id := range ids {
				rows[id] = drawObservationEuro(models.OddsMoney{})
			}
			return rows, nil
		},
	}
}

func drawObservationsTestCache(fake *drawObservationsFake) (*drawObservationCache, *atomic.Int64) {
	clock := &atomic.Int64{}
	clock.Store(time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC).UnixNano())
	return newDrawObservationCache(fake.loaders(), 30*time.Second, func() time.Time { return time.Unix(0, clock.Load()) }), clock
}

func TestDrawObservationsCacheCoalescesConcurrentColdLoads(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.baseEntered, fake.releaseBase = make(chan struct{}), make(chan struct{})
	cache, _ := drawObservationsTestCache(fake)
	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := cache.read(drawObservationQuery{mode: "history", date: "2026-10-21", page: 1, size: 1})
			if err == nil && (response.Total != 3 || len(response.Items) != 1 || response.Items[0].HomeRecent.Sample != 1) {
				err = fmt.Errorf("unexpected in-memory response")
			}
			errors <- err
		}()
	}
	<-fake.baseEntered
	close(fake.releaseBase)
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 || fake.marketCalls.Load() != 1 || fake.quoteCalls.Load() != 1 {
		t.Fatalf("concurrent loads were duplicated: base=%d targets=%d markets=%d quotes=%d", fake.baseCalls.Load(), fake.targetCalls.Load(), fake.marketCalls.Load(), fake.quoteCalls.Load())
	}
	core, _ := cache.get(false)
	if len(core.windows) != 1 {
		t.Fatal("same target windows were rebuilt instead of memoized")
	}
}

func TestDrawObservationsCachePaginationAndFiltersOnlyLoadPageOdds(t *testing.T) {
	fake := newDrawObservationsFake()
	cache, _ := drawObservationsTestCache(fake)
	q := drawObservationQuery{mode: "history", date: "2026-10-21", page: 1, size: 1}
	first, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	q.page = 2
	second, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	q.page = 1
	q.homeDraws = drawObservationRange{max: drawTestPtr(0)}
	filtered, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || second.Items[0].MatchID == first.Items[0].MatchID || filtered.Total != 1 || filtered.Items[0].MatchID != "day-1" {
		t.Fatalf("cache changed pagination/count filters: first=%+v second=%+v filtered=%+v", first, second, filtered)
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 || fake.marketCalls.Load() != 1 || fake.quoteCalls.Load() != 2 {
		t.Fatal("turning pages or changing filters repeated base/history/day reads")
	}
	if !reflect.DeepEqual(fake.quoteIDs, [][]string{{"day-2"}, {"day-1"}}) {
		t.Fatalf("odds fetched outside requested pages: %v", fake.quoteIDs)
	}
	core, _ := cache.get(false)
	if len(core.windows) != 3 {
		t.Fatalf("count filters failed to compute all day windows before pagination: %d", len(core.windows))
	}
}

func TestDrawObservationsCacheTTLRefreshAndNewDataInvalidation(t *testing.T) {
	fake := newDrawObservationsFake()
	cache, clock := drawObservationsTestCache(fake)
	q := drawObservationQuery{mode: "today", date: "2026-10-21", page: 1, size: 50}
	before, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.targets = append(fake.targets, drawObservationMain(drawObservationGame("new-day-target", "9", "10", time.Date(2026, 10, 21, 13, 0, 0, 0, time.UTC), 0, 0)))
	fake.history = append(fake.history, drawObservationGame("new-prior-meeting", "1", "2", time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC), 1, 1))
	fake.marketLine = "0.5"
	fake.mu.Unlock()
	stale, _ := cache.read(q)
	if stale.Total != before.Total || fake.baseCalls.Load() != 1 {
		t.Fatal("unexpired cache unexpectedly rebuilt")
	}
	q.refresh = true
	fresh, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Total != before.Total+1 || fresh.Summary.HistoricalMatches != before.Summary.HistoricalMatches+1 || fake.baseCalls.Load() != 2 || fake.quoteCalls.Load() != 2 {
		t.Fatal("force refresh did not invalidate targets, summary and quote generation")
	}
	for _, item := range fresh.Items {
		if item.MatchID == "day-0" && (item.HomeRecent.Draws != 2 || item.HeadToHead.Draws != 2 || item.AsianHandicap.Opening == nil || *item.AsianHandicap.Opening != "0.5") {
			t.Fatalf("force refresh retained old windows or market: %+v", item)
		}
	}
	q.refresh = false
	clock.Add(int64(30 * time.Second))
	if _, err := cache.read(q); err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != 3 || fake.targetCalls.Load() != 3 || fake.marketCalls.Load() != 3 {
		t.Fatal("30-second TTL failed to replace parsed-history/index generation")
	}
}

func TestDrawObservationsCacheFailuresAreRetriedWithoutServingPartialData(t *testing.T) {
	for _, stage := range []string{"base", "targets", "markets", "quotes"} {
		t.Run(stage, func(t *testing.T) {
			fake := newDrawObservationsFake()
			switch stage {
			case "base":
				fake.baseFailures.Store(1)
			case "targets":
				fake.targetFailures.Store(1)
			case "markets":
				fake.marketFailures.Store(1)
			case "quotes":
				fake.quoteFailures.Store(1)
			}
			cache, _ := drawObservationsTestCache(fake)
			q := drawObservationQuery{mode: "history", date: "2026-10-21", page: 1, size: 1}
			if _, err := cache.read(q); err == nil {
				t.Fatal("failed stage was presented as successful data")
			}
			response, err := cache.read(q)
			if err != nil || response.Total != 3 {
				t.Fatalf("next read could not retry: response=%+v err=%v", response, err)
			}
			wantBase := int32(1)
			if stage == "base" {
				wantBase = 2
			}
			if fake.baseCalls.Load() != wantBase {
				t.Fatal("side-table failure unnecessarily reparsed global history")
			}
		})
	}
}

func TestDrawObservationsDateIsSingleDayAcrossModesAndExplicitDateWins(t *testing.T) {
	fake := newDrawObservationsFake()
	cache, _ := drawObservationsTestCache(fake)
	for _, mode := range []string{"today", "history"} {
		response, err := cache.read(drawObservationQuery{mode: mode, date: "2026-10-21", start: "2026-01-01", end: "2026-01-02", page: 1, size: 50})
		if err != nil || response.Total != 3 || response.Summary.HistoricalMatches != 4 {
			t.Fatalf("single-day %s did not take precedence over old range: %+v err=%v", mode, response, err)
		}
		for _, item := range response.Items {
			if item.Date != "2026-10-21" {
				t.Fatal("another day entered the response")
			}
		}
	}
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/draw/observations?mode=history&date=2026-10-21&start_date=bad&end_date=bad&refresh=1", nil)
	q, err := parseDrawObservationQuery(context)
	if err != nil || q.start != "" || q.end != "" || !q.refresh {
		t.Fatalf("explicit date/refresh semantics failed: %+v err=%v", q, err)
	}
	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/draw/observations?mode=history&start_date=2026-10-20&end_date=2026-10-21", nil)
	q, err = parseDrawObservationQuery(context)
	if err != nil || q.date != "" || q.start != "2026-10-20" {
		t.Fatalf("legacy range compatibility lost: %+v err=%v", q, err)
	}
}

func TestDrawObservationsProjectionExcludesBlobsAndMissingColumns(t *testing.T) {
	schema := drawObservationSchema{
		"history_moneys": {"match_id": true, "recent_home_list": true, "recent_guest_list": true, "against_list": true, "league_stat": true, "rank_data": true, "future_home": true},
		"odds_moneys":    {"match_id": true, "data": true, "sporttery_trade": true},
		"pankou_moneys":  {"match_id": true, "asia_data": true},
	}
	history := strings.Join(drawObservationHistoryProjection(schema), ",")
	if strings.Contains(history, "rank_data") || strings.Contains(history, "future_home") || !strings.Contains(history, "CASE WHEN") || !strings.Contains(history, "'null'") || !strings.Contains(history, "ELSE `league_stat`") {
		t.Fatalf("history projection lost selective null/empty fallback: %s", history)
	}
	odds := drawObservationProjection(schema, "odds_moneys", []string{"match_id", "data", "bet365", "captured_at"})
	if !reflect.DeepEqual(odds, []string{"match_id", "data"}) {
		t.Fatalf("nonexistent captured_at or unused trading blob selected: %v", odds)
	}
	// Inspect the GORM WHERE clause in memory, without opening any SQL driver.
	query := &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Clauses: map[string]clause.Clause{}}}
	query = drawObservationSQLScope(query, drawObservationQuery{date: "2026-10-21", start: "2026-01-01"})
	where := query.Statement.Clauses["WHERE"].Expression.(clause.Where)
	expression := where.Exprs[0].(clause.Expr)
	if len(where.Exprs) != 1 || expression.SQL != "date = ?" || !reflect.DeepEqual(expression.Vars, []interface{}{"2026-10-21"}) {
		t.Fatalf("day scope stopped using an indexed equality: %+v", where)
	}
	// A zero sql.DB plus disabled ping/version initialization cannot connect.
	// DryRun verifies that GORM emits CASE as SQL, not a quoted column name.
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	statement := db.Table("history_moneys").Select(drawObservationHistoryProjection(schema)).Find(&[]models.HistoryMoney{}).Statement.SQL.String()
	if !strings.Contains(statement, "CASE WHEN (") || strings.Contains(statement, "`CASE WHEN") {
		t.Fatalf("CASE projection was quoted as a column: %s", statement)
	}
}

func TestDrawObservationsCacheCurrentTableDatePreventsLegacyResurrection(t *testing.T) {
	fake := newDrawObservationsFake()
	old := fake.targets[0]
	current := old
	current.Match.Date = current.Match.Date.AddDate(0, 0, 1)
	fake.targets = []drawObservationTarget{current}
	loaders := fake.loaders()
	loaders.targets = func(_ drawObservationSchema, _ drawObservationQuery) ([]drawObservationTarget, error) {
		return []drawObservationTarget{old}, nil
	}
	cache := newDrawObservationCache(loaders, 30*time.Second, time.Now)
	response, err := cache.read(drawObservationQuery{mode: "history", date: "2026-10-21", size: 50})
	if err != nil || response.Total != 0 || len(response.Items) != 0 || response.Summary.HistoricalMatches != 1 {
		t.Fatalf("legacy copy resurrected a rescheduled current match: %+v err=%v", response, err)
	}
}

func TestDrawObservationsCacheSummaryIsNotAccumulatedAcrossReads(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.marketLine = "0.5"
	cache, _ := drawObservationsTestCache(fake)
	q := drawObservationQuery{mode: "history", date: "2026-10-21", size: 50}
	first, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary.AsianMatches != 3 || !reflect.DeepEqual(first.Summary, second.Summary) {
		t.Fatalf("cached summary mutated across reads: first=%+v second=%+v", first.Summary, second.Summary)
	}
}

func TestDrawObservationsParsedHistoryHonorsMainStatusAndFutureAuthority(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	good := drawObservationMain(drawObservationGame("primary", "1", "2", now.Add(-48*time.Hour), 2, 0))
	cancelled := drawObservationMain(drawObservationGame("cancelled", "1", "2", now.Add(-48*time.Hour), 0, 0))
	cancelled.Match.DisplayState = "取消"
	future := drawObservationMain(drawObservationGame("future", "1", "2", now.Add(24*time.Hour), 0, 0))
	rows := []map[string]interface{}{}
	for _, fixture := range []struct {
		id string
		at time.Time
	}{{"primary", now.Add(-48 * time.Hour)}, {"cancelled", now.Add(-48 * time.Hour)}, {"future", now.Add(24 * time.Hour)}, {"unknown-future", now.Add(24 * time.Hour)}, {"old-embedded", now.Add(-72 * time.Hour)}} {
		rows = append(rows, map[string]interface{}{"matchId": fixture.id, "homeId": "1", "guestId": "2", "home": "1", "guest": "2", "matchTime": fixture.at.Format(time.RFC3339), "goal": []int{0, 0}})
	}
	encoded, _ := json.Marshal(rows)
	pool := drawObservationParsedHistory([]drawObservationTarget{good, cancelled, future}, []models.HistoryMoney{{RecentHomeList: encoded}}, now)
	if len(pool) != 2 {
		t.Fatalf("cancelled/future source rows entered completed history: %+v", pool)
	}
	for _, game := range pool {
		if game.Game.ID == "primary" && (game.Game.HomeGoals != 2 || game.Game.AwayGoals != 0) {
			t.Fatal("embedded stale score overrode the authoritative match row")
		}
	}
}

func BenchmarkDrawObservationsCachedDayPagination(b *testing.B) {
	fake := newDrawObservationsFake()
	cache, _ := drawObservationsTestCache(fake)
	q := drawObservationQuery{mode: "history", date: "2026-10-21", page: 1, size: 1}
	if _, err := cache.read(q); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.page = i%3 + 1
		if i%2 == 0 {
			q.homeDraws = drawObservationRange{min: drawTestPtr(0)}
		} else {
			q.homeDraws = drawObservationRange{}
		}
		if _, err := cache.read(q); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 || fake.marketCalls.Load() != 1 {
		b.Fatal("cached pagination repeated history/base/day loads")
	}
	b.ReportMetric(float64(fake.baseCalls.Load()), "base_reads")
}

func TestDrawObservationsDefaultDayUsesBeijingMidnightAcrossModes(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	for _, fixture := range []struct{ instant, date string }{
		{"2026-10-20T15:59:59Z", "2026-10-20"},
		{"2026-10-20T16:00:00Z", "2026-10-21"},
		{"2026-10-20T23:30:00Z", "2026-10-21"},
		{"2026-10-21T16:00:00Z", "2026-10-22"},
	} {
		instant, err := time.Parse(time.RFC3339, fixture.instant)
		if err != nil {
			t.Fatal(err)
		}
		for _, mode := range []string{"today", "history"} {
			q := normalizeDrawObservationQueryAt(drawObservationQuery{mode: mode}, instant.In(time.FixedZone("HostWest", -7*60*60)))
			if q.date != fixture.date {
				t.Fatalf("%s default followed UTC/host date at %s: %+v", mode, fixture.instant, q)
			}
		}
		explicit := normalizeDrawObservationQueryAt(drawObservationQuery{mode: "history", date: "2026-10-01"}, instant)
		if explicit.date != "2026-10-01" {
			t.Fatal("calendar default changed explicit schedule date")
		}
	}
}

func TestDrawObservationsBeijingWallClockConvertsToUTCOnce(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	expected := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	// MySQL DATETIME has no timezone. The driver's location tag must not add
	// a second offset to a source wall clock of 18:00 Beijing.
	for _, label := range []*time.Location{time.UTC, time.FixedZone("HostWest", -7*60*60), drawSourceLocation} {
		raw := time.Date(2026, 10, 21, 18, 0, 0, 0, label)
		if got := drawSourceTime(raw); !got.Equal(expected) {
			t.Fatalf("wall clock was converted twice for %s: %s", label, got)
		}
	}
	plain, ok := drawHistoryTime("2026-10-21 18:00:00")
	if !ok || !plain.Equal(expected) {
		t.Fatal("timezone-less history did not match the source wall clock")
	}
	utc, ok := drawHistoryTime("2026-10-21T10:00:00Z")
	if !ok || !utc.Equal(expected) {
		t.Fatal("history's existing UTC offset was reinterpreted")
	}
	target := drawObservationMain(drawObservationGame("beijing-evening", "1", "2", expected, 1, 1))
	// A SQL DATE's calendar fields are labels, so never shift them as if
	// midnight were an event timestamp, even under an unusual driver tag.
	target.Match.Date = time.Date(2026, 10, 21, 0, 0, 0, 0, time.FixedZone("DriverTag", 14*60*60))
	data := drawObservationData{Targets: []drawObservationTarget{target}, History: []drawHistoricalGame{
		drawObservationGame("known-at-17-bj", "1", "2", expected.Add(-3*time.Hour), 0, 0),
		drawObservationGame("still-running-at-18-bj", "1", "2", expected.Add(-time.Hour), 0, 0),
	}}
	response := buildDrawObservations(data, drawObservationQuery{mode: "history", date: "2026-10-21", size: 50})
	if response.ScheduleTimezone != "Asia/Shanghai" || response.Total != 1 || !response.Items[0].MatchTime.Equal(expected) || response.Items[0].Date != "2026-10-21" || response.Items[0].HomeRecent.Sample != 1 || response.Items[0].HeadToHead.Sample != 1 {
		t.Fatalf("date/timestamp distinction broke query or historical cutoff: %+v", response)
	}
}
