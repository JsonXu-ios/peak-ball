package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/models"
	"gorm.io/datatypes"
)

func TestDrawObservationsScopeDefaultInvalidAndAlwaysPresentFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scope := range []string{"", "all", "sporttery"} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", "/api/draw/observations?date=2026-10-21&scope="+scope, nil)
		q, err := parseDrawObservationQuery(context)
		want := scope
		if want == "" {
			want = "all"
		}
		if err != nil || q.scope != want {
			t.Fatalf("scope default/query changed: %+v error=%v", q, err)
		}
	}
	for _, scope := range []string{"other", "Sporttery", "sporttery,all", "%20all%20"} {
		writer := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(writer)
		context.Request = httptest.NewRequest("GET", "/api/draw/observations?scope="+scope, nil)
		GetDrawObservations(context)
		if writer.Code != 400 {
			t.Fatalf("invalid scope reached reads: %q status=%d", scope, writer.Code)
		}
	}
	fixture := drawObservationMain(drawObservationGame("plain", "1", "2", time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), 1, 1))
	response := buildDrawObservations(drawObservationData{Targets: []drawObservationTarget{fixture}}, drawObservationQuery{date: "2026-10-21", size: 50})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Scope string                   `json:"scope"`
		Items []map[string]interface{} `json:"items"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Scope != "all" || decoded.Items[0]["jingcaiId"] != "" {
		t.Fatalf("default scope or empty item jingcaiId disappeared from JSON: %s", encoded)
	}
}

func TestDrawObservationsScopeMergesCurrentBeforeSportteryAndNeverGuessesFromOdds(t *testing.T) {
	base := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	current := drawObservationMain(drawObservationGame("duplicate", "1", "2", base, 1, 1)).Match
	legacy := current
	legacy.JingcaiID = "周三001"
	eligible := drawObservationMain(drawObservationGame("legacy-only", "3", "4", base.Add(-time.Hour), 1, 1)).Match
	eligible.JingcaiID = "  周三002  "
	white := drawObservationMain(drawObservationGame("whitespace", "5", "6", base.Add(-2*time.Hour), 1, 1)).Match
	white.JingcaiID = " \t\n "
	// TRIM(nonempty), exactly as the frontend query, does not validate the ID.
	zero := drawObservationMain(drawObservationGame("literal-zero", "7", "8", base.Add(-3*time.Hour), 1, 1)).Match
	zero.JingcaiID = "0"
	targets := mergeDrawObservationTargets([]models.Money{current, white, zero}, []models.Money{legacy, eligible})
	data := drawObservationData{Targets: targets, Odds: map[string]models.OddsMoney{
		"duplicate":  {SportteryTrade: datatypes.JSON(`{"tzbl":{"h":2,"d":3,"a":4}}`)},
		"whitespace": {SportteryTrade: datatypes.JSON(`{"tzbl":{"h":2,"d":3,"a":4}}`)},
	}}
	got := buildDrawObservations(data, drawObservationQuery{scope: "sporttery", mode: "history", date: "2026-10-21", size: 50})
	if got.Total != 2 || got.Summary.HistoricalMatches != 2 || got.Summary.DrawMatches != 2 || got.Items[0].MatchID != "literal-zero" || got.Items[1].MatchID != "legacy-only" || got.Items[1].JingcaiID != "  周三002  " {
		t.Fatalf("legacy JC resurrected current non-JC or odds/whitespace guessed JC membership: %+v", got)
	}
	got = buildDrawObservations(data, drawObservationQuery{scope: "all", mode: "history", date: "2026-10-21", size: 50})
	if got.Total != 4 || got.Summary.HistoricalMatches != 4 || got.Summary.DrawMatches != 4 {
		t.Fatalf("all scope changed legacy/default behavior: %+v", got)
	}
}

func TestDrawObservationsScopeCacheCombinationPaginationSummaryAndAllHistory(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.targets[0].Match.JingcaiID = "周三001"
	fake.targets[1].Match.JingcaiID = ""
	fake.targets[2].Match.JingcaiID = "周三003"
	fake.targets[3].Match.JingcaiID = "周二001" // Other date, retained in global scope summary.
	fake.targets[0].Match.League = "Sport A"
	fake.targets[1].Match.League = "Non sport"
	fake.targets[2].Match.League = "Sport B"
	fake.marketLine = "0.5"
	loaders := fake.loaders()
	originalTargets := loaders.targets
	loaders.targets = func(schema drawObservationSchema, q drawObservationQuery) ([]drawObservationTarget, error) {
		if q.scope != "all" {
			t.Error("shared date loader received a narrowed scope before authoritative merge")
		}
		return originalTargets(schema, q)
	}
	cache := newDrawObservationCache(loaders, 30*time.Second, time.Now)
	q := drawObservationQuery{scope: "sporttery", mode: "history", date: "2026-10-21", page: 1, size: 1}
	first, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	q.page = 2
	second, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || first.Items[0].MatchID != "day-2" || second.Items[0].MatchID != "day-0" || first.Summary.HistoricalMatches != 3 || first.Summary.DrawMatches != 3 || first.Summary.AsianMatches != 2 || !reflect.DeepEqual(first.Leagues, []string{"Sport A", "Sport B"}) {
		t.Fatalf("scope did not precede options/coverage/page or changed global/date distinction: first=%+v second=%+v", first, second)
	}
	if second.Items[0].HomeRecent.Sample != 1 || second.Items[0].HeadToHead.Sample != 1 {
		t.Fatal("sporttery scope shrank historical inputs to JC-only matches")
	}
	q.scope, q.page, q.size = "all", 1, 50
	all, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 3 || all.Summary.HistoricalMatches != 4 || all.Summary.DrawMatches != 4 || all.Summary.AsianMatches != 3 || len(all.Leagues) != 3 {
		t.Fatalf("all reused a sporttery-only date cache: %+v", all)
	}
	q.scope, q.league, q.search, q.homeDraws, q.awayDraws, q.h2hDraws = "sporttery", "Sport B", "day-2", drawObservationRange{min: drawTestPtr(1)}, drawObservationRange{max: drawTestPtr(1)}, drawObservationRange{min: drawTestPtr(1), max: drawTestPtr(1)}
	combined, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if combined.Total != 1 || combined.Items[0].MatchID != "day-2" || !reflect.DeepEqual(combined.Summary, first.Summary) || !reflect.DeepEqual(combined.Leagues, first.Leagues) {
		t.Fatalf("scope combination changed summary, options or AND filtering: %+v", combined)
	}
	q.league, q.search, q.homeDraws, q.awayDraws, q.h2hDraws = "", "", drawObservationRange{max: drawTestPtr(0)}, drawObservationRange{}, drawObservationRange{}
	zero, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if zero.Total != 0 || zero.Summary.HistoricalMatches != 3 || zero.Summary.AsianMatches != 2 {
		t.Fatalf("non-sporttery zero-draw fixture leaked through combined filter: %+v", zero)
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 || fake.marketCalls.Load() != 1 {
		t.Fatalf("scope changes rebuilt shared history/day cache: base=%d targets=%d markets=%d", fake.baseCalls.Load(), fake.targetCalls.Load(), fake.marketCalls.Load())
	}
	q = drawObservationQuery{scope: "sporttery", mode: "today", date: "2026-10-30", size: 50}
	empty, err := cache.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Total != 0 || empty.Summary.HistoricalMatches != 3 || empty.Summary.DrawMatches != 3 || empty.Summary.AsianMatches != 0 || len(empty.Leagues) != 0 {
		t.Fatalf("empty date discarded scope-global totals or reused another day: %+v", empty)
	}
}

func TestDrawObservationsScopeMissingHeaderColumnRemainsCompatible(t *testing.T) {
	schema := drawObservationSchema{"moneys": {"match_id": true, "date": true, "match_time": true}}
	columns := drawObservationProjection(schema, "moneys", drawObservationHeaderColumns)
	if strings.Contains(strings.Join(columns, ","), "jingcai_id") {
		t.Fatal("legacy schema projection selected a missing jingcai_id column")
	}
	zero := models.Money{}
	if drawObservationScopeMatches(zero, "sporttery") || !drawObservationScopeMatches(zero, "all") {
		t.Fatal("unknown/missing JC column guessed sporttery membership or broke all scope")
	}
	schema["moneys"]["jingcai_id"] = true
	columns = drawObservationProjection(schema, "moneys", drawObservationHeaderColumns)
	count := 0
	for _, column := range columns {
		if column == "jingcai_id" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("header projection does not read jingcai_id exactly once: %v", columns)
	}
}
