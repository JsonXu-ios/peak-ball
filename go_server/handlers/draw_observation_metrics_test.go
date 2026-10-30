package handlers

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func drawMetricFixture(id, state string, score *float64, home, away int, settled bool) drawObservationItem {
	confirmed := settled
	return drawObservationItem{MatchID: id, JingcaiID: "周三001", Date: "2026-10-21", League: "League", Home: "Home " + id, Guest: "Guest", HomeScore: home, AwayScore: away, Settled: settled, RegulationSettled: &confirmed, HeadToHead: drawObservationRecent{Sample: 5}, CompanyQuotes: []drawObservationCompanyQuotes{drawSecondaryGuardQuoteFixture()}, FrozenDrawSignal: &drawObservationFrozenSignal{State: state, Score: score}}
}

func drawMetricScore(value float64) *float64 { return &value }

func drawMetricRequireRate(t *testing.T, value *float64, want float64) {
	t.Helper()
	if value == nil || math.Abs(*value-want) > 1e-12 {
		t.Fatalf("rate=%v want %v", value, want)
	}
}

func TestDrawObservationMetricsCompleteWindowNotDrawOnlyPage(t *testing.T) {
	items := []drawObservationItem{
		drawMetricFixture("draw-retained", "selected", drawMetricScore(.3), 1, 1, true),
		drawMetricFixture("miss-rejected", "selected", drawMetricScore(.22), 2, 1, true),
		drawMetricFixture("draw-boundary", "selected", drawMetricScore(.225), 0, 0, true),
		drawMetricFixture("draw-rejected", "selected", drawMetricScore(.21), 2, 2, true),
		drawMetricFixture("pending", "selected", drawMetricScore(.3), 0, 0, false),
		drawMetricFixture("draw-not-selected", "not_selected", drawMetricScore(.1), 1, 1, true),
		drawMetricFixture("draw-unavailable", "unavailable", nil, 1, 1, true),
		drawMetricFixture("score-missing", "selected", nil, 1, 0, true),
		drawMetricFixture("other-scope", "out_of_scope", nil, 0, 0, true),
		drawMetricFixture("other-date", "selected", drawMetricScore(.3), 1, 1, true),
	}
	items[8].JingcaiID = ""
	items[9].Date = "2026-10-22"
	saved := drawObservationStoredDay{Date: "2026-10-21", Items: items, DrawIDs: map[string]bool{}}
	for _, row := range items {
		if drawObservationMetricSettled(row) && row.HomeScore == row.AwayScore {
			saved.DrawIDs[row.MatchID] = true
		}
	}
	query := drawObservationQuery{date: saved.Date, scope: "all", mode: "today", page: 1, size: 2}
	base := saved.filter(query).ObservationMetrics
	if base == nil || base.ScopeMatches != 9 || base.SettledMatches != 8 || base.SettledDraws != 6 || base.EligibleMatches != 8 || base.EligibleSettled != 7 || base.EligibleDraws != 5 || base.ModelAvailable != 7 || base.ModelUnavailable != 1 || base.OutOfScope != 1 || base.SecondaryUnavailable != 1 || base.Status != "incomplete" {
		t.Fatalf("wrong cohort/missing counts: %+v", base)
	}
	if got := base.Original; got.Selected != 6 || got.Settled != 5 || got.Hits != 3 || got.Misses != 2 || got.Pending != 1 {
		t.Fatalf("wrong original totals: %+v", got)
	}
	drawMetricRequireRate(t, base.Original.Precision, 3.0/5)
	drawMetricRequireRate(t, base.Original.Coverage, 3.0/5)
	if got := base.Secondary; got.Selected != 3 || got.Settled != 2 || got.Hits != 2 || got.Misses != 0 || got.Pending != 1 {
		t.Fatalf("wrong secondary totals/boundary: %+v", got)
	}
	drawMetricRequireRate(t, base.Secondary.Precision, 1)
	drawMetricRequireRate(t, base.Secondary.Coverage, 2.0/5)
	if got := base.Rejected; got.Selected != 2 || got.Settled != 2 || got.Hits != 1 || got.Misses != 1 || got.Pending != 0 {
		t.Fatalf("wrong rejected totals: %+v", got)
	}
	drawMetricRequireRate(t, base.Rejected.Precision, .5)
	zero, ten := 0, 10
	for _, changed := range []drawObservationQuery{
		{date: saved.Date, scope: "all", mode: "history", page: 1, size: 1},
		{date: saved.Date, scope: "all", mode: "today", page: 2, size: 2},
		{date: saved.Date, scope: "all", mode: "history", page: 999, size: 200},
		{date: saved.Date, scope: "all", mode: "history", homeDraws: drawObservationRange{min: &ten}, h2hDraws: drawObservationRange{max: &zero}, page: 1, size: 50},
	} {
		if got := saved.filter(changed).ObservationMetrics; !reflect.DeepEqual(got, base) {
			t.Fatalf("tab/count/page changed full metrics: q=%+v got=%+v want=%+v", changed, got, base)
		}
	}
	query.scope = "sporttery"
	lottery := saved.filter(query).ObservationMetrics
	if lottery.ScopeMatches != 8 || lottery.SettledDraws != 5 || lottery.OutOfScope != 0 || !reflect.DeepEqual(lottery.Original, base.Original) || !reflect.DeepEqual(lottery.Secondary, base.Secondary) {
		t.Fatalf("switching all/lottery changed eligible rates: %+v", lottery)
	}
}

func TestDrawObservationMetricsLeagueSearchAndDate(t *testing.T) {
	items := []drawObservationItem{drawMetricFixture("one", "selected", drawMetricScore(.3), 1, 1, true), drawMetricFixture("two", "selected", drawMetricScore(.22), 1, 0, true), drawMetricFixture("three", "selected", drawMetricScore(.3), 1, 1, true)}
	items[1].League = "Other"
	items[2].Date = "2026-10-22"
	for _, query := range []drawObservationQuery{
		{date: "2026-10-21", scope: "all", league: "League"},
		{date: "2026-10-21", scope: "all", search: " HOME ONE "},
		{date: "2026-10-21", scope: "all", search: "one"},
	} {
		got := buildDrawObservationMetrics(items, query)
		if got.ScopeMatches != 1 || got.Original.Hits != 1 || got.Secondary.Hits != 1 || got.EligibleDraws != 1 {
			t.Fatalf("wrong query window for %+v: %+v", query, got)
		}
	}
	got := buildDrawObservationMetrics(items, drawObservationQuery{date: "2026-10-22", scope: "all"})
	if got.ScopeMatches != 1 || got.Secondary.Hits != 1 {
		t.Fatalf("different date not respected: %+v", got)
	}
}

func TestDrawObservationMetricsUnknownAndZeroRatesStayNull(t *testing.T) {
	query := drawObservationQuery{date: "2026-10-21", scope: "all"}
	for _, rows := range [][]drawObservationItem{nil, {drawMetricFixture("pending", "selected", drawMetricScore(.3), 0, 0, false)}} {
		got := buildDrawObservationMetrics(rows, query)
		for _, group := range []drawObservationMetricGroup{got.Original, got.Secondary, got.Rejected} {
			if group.Precision != nil || group.Coverage != nil {
				t.Fatalf("unknown/zero denominator must not yield 0/100%%: %+v", got)
			}
		}
		packed, err := json.Marshal(got)
		if err != nil || len(packed) == 0 {
			t.Fatalf("metrics must safely serialize zero/null rates: %s %v", packed, err)
		}
	}
	oldRow := drawMetricFixture("legacy", "selected", drawMetricScore(.3), 1, 1, true)
	oldRow.FrozenDrawSignal = nil
	got := buildDrawObservationMetrics([]drawObservationItem{oldRow}, query)
	if got.Status != "incomplete" || got.ModelUnavailable != 1 || got.EligibleDraws != 1 || got.Original.Selected != 0 || got.Original.Coverage != nil {
		t.Fatalf("legacy cached rows silently counted as negatives: %+v", got)
	}
	for _, score := range []*float64{nil, drawMetricScore(math.NaN()), drawMetricScore(math.Inf(1)), drawMetricScore(-.1), drawMetricScore(1.1)} {
		row := drawMetricFixture("invalid", "selected", score, 1, 1, true)
		got := buildDrawObservationMetrics([]drawObservationItem{row}, query)
		if got.Original.Selected != 1 || got.Secondary.Selected != 0 || got.Rejected.Selected != 0 || got.SecondaryUnavailable != 1 || got.Status != "incomplete" || got.Secondary.Coverage != nil {
			t.Fatalf("invalid score misclassified as retained/rejected: %+v", got)
		}
	}
}

func TestDrawObservationMetricsRequiresConfirmedRegulationResult(t *testing.T) {
	row := drawMetricFixture("unconfirmed", "selected", drawMetricScore(.3), 0, 0, true)
	row.RegulationSettled = nil
	got := buildDrawObservationMetrics([]drawObservationItem{row}, drawObservationQuery{date: row.Date, scope: "all"})
	if got.SettledMatches != 0 || got.EligibleDraws != 0 || got.Secondary.Pending != 1 || got.Secondary.Hits != 0 || got.Secondary.Precision != nil {
		t.Fatalf("unconfirmed 0:0 counted as a hit: %+v", got)
	}
	row.RegulationSettled = new(bool)
	*row.RegulationSettled = true
	row.HomeScore, row.AwayScore = -1, -1
	got = buildDrawObservationMetrics([]drawObservationItem{row}, drawObservationQuery{date: row.Date, scope: "all"})
	if got.Secondary.Pending != 1 || got.Secondary.Hits != 0 {
		t.Fatalf("invalid unknown score counted as draw: %+v", got)
	}
}
