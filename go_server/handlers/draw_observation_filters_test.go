package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/internal/drawmodeldata"
)

func TestDrawObservationListStatusQueryValidationAndEcho(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"", "&match_status=all&selection=all", "&match_status=finished&selection=sporttery", "&match_status=unfinished&selection=sporttery", "&match_status=unfinished&selection=non_sporttery", "&match_status=unknown", "&match_status=" + url.QueryEscape("state:上半场"), "&selection=non_sporttery"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?date=2026-11-04"+query, nil)
		q, err := parseDrawObservationQuery(c)
		if err != nil {
			t.Fatal(query, err)
		}
		if q.matchStatus == "" || q.selection == "" {
			t.Fatalf("missing normalized defaults: %+v", q)
		}
		response := (&drawObservationStoredDay{Date: "2026-11-04"}).filter(q)
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["matchStatus"] != q.matchStatus || fields["selection"] != q.selection {
			t.Fatalf("filter capability echo differs: %s", encoded)
		}
	}
	for _, query := range []string{"match_status=settled", "match_status=Finished", "match_status=Unfinished", "match_status=finished,all", "selection=selected", "selection=Sporttery", "selection=non-sporttery"} {
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		c.Request = httptest.NewRequest("GET", "/?"+query, nil)
		GetDrawObservations(c)
		if writer.Code != 400 {
			t.Fatalf("invalid filter reached source reads: %s status=%d", query, writer.Code)
		}
	}
	for _, value := range []string{"state:", "state:   ", "state:" + strings.Repeat("球", 101), "state:上\n半场", "state:未开始\u0000", "state:上半场\t"} {
		writer := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(writer)
		c.Request = httptest.NewRequest("GET", "/?date=2026-11-04&match_status="+url.QueryEscape(value), nil)
		GetDrawObservations(c)
		if writer.Code != 400 {
			t.Fatalf("invalid source state was accepted: %q status=%d", value, writer.Code)
		}
	}
	for _, value := range []string{"state:" + strings.Repeat("球", 100), "state:  新的源状态  "} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?date=2026-11-04&match_status="+url.QueryEscape(value), nil)
		q, err := parseDrawObservationQuery(c)
		if err != nil || q.matchStatus != "state:"+strings.TrimSpace(strings.TrimPrefix(value, "state:")) {
			t.Fatalf("valid exact source state failed normalization: %q q=%+v err=%v", value, q, err)
		}
	}
	for _, query := range []string{"match_status=finished", "match_status=state%3A%E4%B8%8A%E5%8D%8A%E5%9C%BA", "selection=sporttery"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?start_date=2026-11-01&end_date=2026-11-04&"+query, nil)
		if _, err := parseDrawObservationQuery(c); err == nil {
			t.Fatal("unsupported legacy range silently ignored status/selection")
		}
	}
}

func TestDrawObservationListStatusExactSourceStatesAndUnknown(t *testing.T) {
	states := []string{"未开始", "上半场", "中场", "下半场", "进行中", "加时", "点球", "延期", "推迟", "取消", "腰斩", "中断", "暂停", "待定", "新的源状态"}
	for _, state := range states {
		row := drawObservationItem{DisplayState: " " + state + " ", Status: 0, NonSportterySignal: &drawObservationFrozenSignal{State: "selected"}}
		for _, filter := range states {
			if got := drawObservationListStatusMatches(row, drawObservationQuery{matchStatus: "state:" + filter, selection: "non_sporttery"}); got != (state == filter) {
				t.Fatalf("exact state was guessed or conflated: source=%q filter=%q got=%v", state, filter, got)
			}
		}
		if drawObservationListStatusMatches(row, drawObservationQuery{matchStatus: "unknown"}) || drawObservationListStatusMatches(row, drawObservationQuery{matchStatus: "finished"}) {
			t.Fatalf("source state %q incorrectly classified as missing or completed", state)
		}
	}
	for _, row := range []drawObservationItem{{}, {DisplayState: "   "}} {
		if !drawObservationListStatusMatches(row, drawObservationQuery{matchStatus: "unknown"}) || drawObservationListStatusMatches(row, drawObservationQuery{matchStatus: "state:未开始"}) {
			t.Fatal("missing source state was guessed to be not-started")
		}
	}
	if drawObservationListStatusMatches(drawObservationItem{Settled: true}, drawObservationQuery{matchStatus: "unknown"}) {
		t.Fatal("confirmed completion counted as an unknown state")
	}
}

func TestDrawObservationListStatusOptionsKeepWholeDayAndScope(t *testing.T) {
	saved := &drawObservationStoredDay{Date: "2026-11-04", DrawIDs: map[string]bool{"done": true}}
	for i, state := range []string{"上半场", "取消", "新的源状态", " ", "完场", "已结束", "上半场"} {
		row := drawObservationItem{MatchID: fmt.Sprint(i), Date: saved.Date, DisplayState: state, MatchTime: time.Date(2026, 11, 4, 12, i, 0, 0, drawSourceLocation), League: "League", Home: "主队", Settled: state == "完场" || state == "已结束", NonSportterySignal: &drawObservationFrozenSignal{State: "selected"}}
		if i != 2 {
			row.JingcaiID = "20261104001"
			row.FrozenDrawSignal = &drawObservationFrozenSignal{State: "selected"}
		}
		saved.Items = append(saved.Items, row)
	}
	want := map[string]string{"state:上半场": "上半场", "state:取消": "取消", "state:新的源状态": "新的源状态", "unknown": "状态未知", "finished": "完赛"}
	baseline := saved.filter(drawObservationQuery{size: 1})
	got := map[string]string{}
	for _, option := range baseline.MatchStatusOptions {
		got[option.Value] = option.Title
	}
	if !reflect.DeepEqual(got, want) || len(baseline.MatchStatusOptions) != len(want) {
		t.Fatalf("options lost source states, duplicated finished aliases or guessed missing status: %+v", baseline.MatchStatusOptions)
	}
	for _, q := range []drawObservationQuery{{matchStatus: "state:上半场", size: 1}, {matchStatus: "state:上半场", size: 1, page: 2, selection: "sporttery"}, {matchStatus: "state:取消", mode: "history", search: "no-match", league: "missing", homeDraws: drawObservationRange{min: drawTestPtr(5)}}} {
		filtered := saved.filter(q)
		if !reflect.DeepEqual(filtered.MatchStatusOptions, baseline.MatchStatusOptions) {
			t.Fatal("display filtering or pagination shrank whole-day status options")
		}
		if q.matchStatus == "state:上半场" && (filtered.Total != 2 || len(filtered.Items) != 1 || filtered.Items[0].DisplayState != "上半场") {
			t.Fatal("exact state was applied after pagination or total calculation")
		}
	}
	jc := saved.filter(drawObservationQuery{scope: "sporttery"})
	for _, option := range jc.MatchStatusOptions {
		if option.Value == "state:新的源状态" {
			t.Fatal("non-JC-only state leaked into JC status options")
		}
	}
}

func TestDrawObservationListStatusSelectionUsesIndependentSignals(t *testing.T) {
	selected := &drawObservationFrozenSignal{State: "selected"}
	notSelected := &drawObservationFrozenSignal{State: "not_selected"}
	for _, test := range []struct {
		name string
		row  drawObservationItem
		jc   bool
		non  bool
	}{
		{"jc-selected", drawObservationItem{JingcaiID: " 20261103001 ", FrozenDrawSignal: selected}, true, false},
		{"jc-not-selected", drawObservationItem{JingcaiID: "20261103002", FrozenDrawSignal: notSelected}, false, false},
		{"non-selected", drawObservationItem{JingcaiID: " \t ", NonSportterySignal: selected}, false, true},
		{"non-not-selected", drawObservationItem{NonSportterySignal: notSelected}, false, false},
		{"non-old-response", drawObservationItem{}, false, false},
		{"non-wrong-jc-signal", drawObservationItem{FrozenDrawSignal: selected}, false, false},
		{"jc-wrong-non-signal", drawObservationItem{JingcaiID: "20261103003", FrozenDrawSignal: notSelected, NonSportterySignal: selected}, false, false},
		{"non-unavailable", drawObservationItem{NonSportterySignal: &drawObservationFrozenSignal{State: "unavailable"}}, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !drawObservationListStatusMatches(test.row, drawObservationQuery{}) || drawObservationListStatusMatches(test.row, drawObservationQuery{selection: "sporttery"}) != test.jc || drawObservationListStatusMatches(test.row, drawObservationQuery{selection: "non_sporttery"}) != test.non {
				t.Fatal("selection crossed JC scope, guessed missing selection, or changed default")
			}
			if drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "finished"}) {
				t.Fatal("pending/cancelled row counted as finished")
			}
			if !drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "unfinished"}) || drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "unfinished", selection: "sporttery"}) != test.jc || drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "unfinished", selection: "non_sporttery"}) != test.non {
				t.Fatal("unfinished and selection predicates were not combined")
			}
			test.row.Settled = true
			if !drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "finished"}) || drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "finished", selection: "sporttery"}) != test.jc || drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "finished", selection: "non_sporttery"}) != test.non {
				t.Fatal("finished and selection predicates were not combined")
			}
			if drawObservationListStatusMatches(test.row, drawObservationQuery{matchStatus: "unfinished"}) {
				t.Fatal("a finished row remained in the unfinished complement")
			}
		})
	}
}

func TestDrawObservationListStatusFiltersBeforePaginationWithoutChangingMetricWindow(t *testing.T) {
	saved := &drawObservationStoredDay{Date: "2026-11-04", Version: "existing-version", DrawIDs: map[string]bool{"jc-finished": true, "non-finished": true}, AllSummary: drawObservationSummary{HistoricalMatches: 100, DrawMatches: 20}}
	for i, id := range []string{"non-pending", "jc-pending", "non-finished", "jc-finished", "cancelled"} {
		row := drawObservationItem{MatchID: id, Date: saved.Date, League: "League", MatchTime: time.Date(2026, 11, 4, 12-i, 0, 0, 0, drawSourceLocation), Settled: id == "jc-finished" || id == "non-finished", RegulationSettled: drawTestPtr(true), HomeRecent: drawObservationRecent{Sample: 10, Draws: 2}}
		if id == "jc-finished" || id == "jc-pending" {
			row.JingcaiID = "20261104001"
			row.FrozenDrawSignal = &drawObservationFrozenSignal{State: "selected", Score: drawTestPtr(.3)}
		} else {
			row.NonSportterySignal = &drawObservationFrozenSignal{State: "selected"}
		}
		saved.Items = append(saved.Items, row)
	}
	cases := []struct {
		q   drawObservationQuery
		ids []string
	}{
		{drawObservationQuery{matchStatus: "finished", size: 1}, []string{"jc-finished"}},
		{drawObservationQuery{matchStatus: "finished", size: 1, page: 2}, []string{"non-finished"}},
		{drawObservationQuery{matchStatus: "finished", selection: "sporttery", size: 1}, []string{"jc-finished"}},
		{drawObservationQuery{matchStatus: "finished", selection: "non_sporttery", size: 1}, []string{"non-finished"}},
		{drawObservationQuery{matchStatus: "finished", selection: "sporttery", page: 2, size: 1}, []string{}},
		{drawObservationQuery{matchStatus: "finished", selection: "non_sporttery", scope: "sporttery"}, []string{}},
		{drawObservationQuery{matchStatus: "finished", selection: "non_sporttery", mode: "history", homeDraws: drawObservationRange{min: drawTestPtr(2)}, search: "non", league: "League"}, []string{"non-finished"}},
		{drawObservationQuery{matchStatus: "finished", selection: "non_sporttery", mode: "history", homeDraws: drawObservationRange{min: drawTestPtr(3)}}, []string{}},
		{drawObservationQuery{matchStatus: "unfinished", size: 1}, []string{"cancelled"}},
		{drawObservationQuery{matchStatus: "unfinished", size: 1, page: 2}, []string{"jc-pending"}},
		{drawObservationQuery{matchStatus: "unfinished", size: 1, page: 3}, []string{"non-pending"}},
		{drawObservationQuery{matchStatus: "unfinished", selection: "sporttery", size: 1}, []string{"jc-pending"}},
		{drawObservationQuery{matchStatus: "unfinished", selection: "non_sporttery", size: 1, page: 2}, []string{"non-pending"}},
		{drawObservationQuery{matchStatus: "unfinished", mode: "history"}, []string{}},
	}
	for _, test := range cases {
		test.q.date = saved.Date
		got := saved.filter(test.q)
		unfilteredQuery := test.q
		unfilteredQuery.matchStatus, unfilteredQuery.selection = "all", "all"
		baseline := saved.filter(unfilteredQuery)
		ids := []string{}
		for _, row := range got.Items {
			ids = append(ids, row.MatchID)
		}
		if !reflect.DeepEqual(ids, test.ids) {
			t.Fatalf("filters/page combination %+v: got %v want %v", test.q, ids, test.ids)
		}
		if !reflect.DeepEqual(got.ObservationMetrics, baseline.ObservationMetrics) {
			t.Fatal("display-only status changed complete-date metrics")
		}
		if !reflect.DeepEqual(got.Summary, baseline.Summary) {
			t.Fatal("display-only status changed summary coverage")
		}
	}
	if got := saved.filter(drawObservationQuery{matchStatus: "finished", page: 1, size: 1}); got.Total != 2 {
		t.Fatalf("total was computed after pagination: %d", got.Total)
	}
	if got := saved.filter(drawObservationQuery{matchStatus: "unfinished", page: 1, size: 1}); got.Total != 3 {
		t.Fatalf("unfinished total was computed after pagination: %d", got.Total)
	}
}

func TestDrawObservationListStatusReusesDurableDayAcrossQueriesAndReaders(t *testing.T) {
	fake := newDrawObservationsFake()
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	q := drawObservationQuery{date: "2026-10-21", size: 1}
	if _, err := reader.read(q); err != nil {
		t.Fatal(err)
	}
	source, _ := drawResultTestSource(q)
	saved, err := reader.storedDay(source.Namespace+":day:"+q.date, q.date, source.Day)
	if err != nil || saved == nil {
		t.Fatal("missing materialized day", err)
	}
	for i := range saved.Items {
		if i%2 == 0 {
			saved.Items[i].JingcaiID = "20261021001"
			saved.Items[i].FrozenDrawSignal = &drawObservationFrozenSignal{State: "selected", ModelVersion: drawmodeldata.Version()}
		} else {
			saved.Items[i].JingcaiID = ""
			saved.Items[i].NonSportterySignal = &drawObservationFrozenSignal{State: "selected", ModelVersion: drawmodeldata.Version()}
		}
		// Keep the independent totals fixture consistent with the changed scope.
		saved.Items[i].TotalsPrediction = drawObservationTotalsSignal(saved.Items[i], nil)
		saved.Items[i].FrozenDrawSignal = drawObservationApplySelectionEligibility(saved.Items[i], saved.Items[i].FrozenDrawSignal)
	}
	packed, err := drawObservationPack(func(w io.Writer) error { return json.NewEncoder(w).Encode(saved) })
	if err != nil {
		t.Fatal(err)
	}
	if err = store.set(context.Background(), source.Namespace+":day:"+q.date, packed); err != nil {
		t.Fatal(err)
	}
	for _, selection := range []string{"all", "sporttery", "non_sporttery"} {
		for _, status := range []string{"all", "finished", "unfinished", "unknown", "state:上半场", "state:新的源状态"} {
			q.selection, q.matchStatus = selection, status
			// A new reader has no warm generator and must use the same stored day.
			fresh := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
			if _, err := fresh.read(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	if fake.baseCalls.Load() != 1 || fake.targetCalls.Load() != 1 || len(store.values) != 2 {
		t.Fatalf("status queries rebuilt model/day or added query-specific keys: base=%d day=%d keys=%d", fake.baseCalls.Load(), fake.targetCalls.Load(), len(store.values))
	}
}
