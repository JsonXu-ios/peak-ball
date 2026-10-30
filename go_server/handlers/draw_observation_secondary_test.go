package handlers

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func drawSecondaryGuardQuoteFixture() drawObservationCompanyQuotes {
	prices := func() []*string {
		var result []*string
		for _, raw := range []string{"2.15", "3.3", "3.9"} {
			value := raw
			result = append(result, &value)
		}
		return result
	}
	return drawObservationCompanyQuotes{Key: "sporttery", EuroOdds: drawObservationEuroOdds{Source: "odds_moneys", OpeningStatus: "available", Opening: prices(), Latest: prices()}}
}

func TestDrawObservationSecondaryOriginalQuotesAndSafeHistoryGuards(t *testing.T) {
	tests := []struct {
		name           string
		mutate         func(*drawObservationItem)
		decision, code string
	}{
		{"complete single-record opening and latest", func(*drawObservationItem) {}, "retained", "retained"},
		{"boundary score", func(r *drawObservationItem) { r.FrozenDrawSignal.Score = drawMetricScore(.225) }, "retained", "retained"},
		{"low score distinct reason", func(r *drawObservationItem) { r.FrozenDrawSignal.Score = drawMetricScore(.224999) }, "rejected", "score_below_threshold"},
		{"missing quote", func(r *drawObservationItem) { r.CompanyQuotes = nil }, "rejected", "sporttery_quotes_incomplete"},
		{"latest-only lr fallback", func(r *drawObservationItem) { r.CompanyQuotes[0].EuroOdds.OpeningStatus = "not_collected" }, "rejected", "sporttery_quotes_incomplete"},
		{"unverified opening status", func(r *drawObservationItem) { r.CompanyQuotes[0].EuroOdds.OpeningStatus = "" }, "rejected", "sporttery_quotes_incomplete"},
		{"missing opening", func(r *drawObservationItem) { r.CompanyQuotes[0].EuroOdds.Opening = nil }, "rejected", "sporttery_quotes_incomplete"},
		{"missing latest", func(r *drawObservationItem) { r.CompanyQuotes[0].EuroOdds.Latest[1] = nil }, "rejected", "sporttery_quotes_incomplete"},
		{"simulated source", func(r *drawObservationItem) { r.CompanyQuotes[0].EuroOdds.Source = "simulated" }, "rejected", "sporttery_quotes_incomplete"},
		{"other company cannot fill", func(r *drawObservationItem) { r.CompanyQuotes[0].Key = "william" }, "rejected", "sporttery_quotes_incomplete"},
		{"duplicate sporttery is ambiguous", func(r *drawObservationItem) {
			r.CompanyQuotes = append(r.CompanyQuotes, drawSecondaryGuardQuoteFixture())
		}, "rejected", "sporttery_quotes_incomplete"},
		{"four h2h", func(r *drawObservationItem) { r.HeadToHead.Sample = 4 }, "rejected", "head_to_head_insufficient"},
		{"missing h2h", func(r *drawObservationItem) { r.HeadToHead.Sample = 0 }, "rejected", "head_to_head_insufficient"},
		{"missing lottery identity", func(r *drawObservationItem) { r.JingcaiID = " " }, "out_of_scope", "not_sporttery"},
		{"missing original model", func(r *drawObservationItem) { r.FrozenDrawSignal = nil }, "unavailable", "model_unavailable"},
		{"original not selected", func(r *drawObservationItem) { r.FrozenDrawSignal.State = "not_selected"; r.CompanyQuotes = nil }, "not_selected", "model_not_selected"},
		{"invalid score", func(r *drawObservationItem) { r.FrozenDrawSignal.Score = drawMetricScore(math.NaN()) }, "unavailable", "score_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := drawMetricFixture("secondary", "selected", drawMetricScore(.3), 0, 0, false)
			tt.mutate(&row)
			got := drawObservationSecondaryDecision(row)
			if got.Version != "secondary-v2-input-guards" || got.Threshold != .225 || got.MinimumHeadToHead != 5 || got.Decision != tt.decision || len(got.ReasonCodes) != 1 || got.ReasonCodes[0] != tt.code || drawObservationSecondaryRetained(row) != (tt.decision == "retained") {
				t.Fatalf("wrong decision: %+v", got)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Fatalf("decision must never expose nonfinite scores: %v", err)
			}
		})
	}
}

func TestDrawObservationSecondaryRejectsInvalidDecimalOdds(t *testing.T) {
	for _, phase := range []string{"opening", "latest"} {
		for _, raw := range []string{"", "0", "-2", "1", "NaN", "+Inf", "1.5invalid"} {
			t.Run(phase+"-"+raw, func(t *testing.T) {
				row := drawMetricFixture("invalid-quote", "selected", drawMetricScore(.3), 0, 0, false)
				values := row.CompanyQuotes[0].EuroOdds.Opening
				if phase == "latest" {
					values = row.CompanyQuotes[0].EuroOdds.Latest
				}
				values[0] = &raw
				got := drawObservationSecondaryDecision(row)
				if got.Decision != "rejected" || got.SportteryQuotesAvailable || got.ReasonCodes[0] != "sporttery_quotes_incomplete" {
					t.Fatalf("invalid %q quote accepted: %+v", raw, got)
				}
			})
		}
	}
}

func TestDrawObservationSecondaryGuardReasonsDoNotPretendLowScore(t *testing.T) {
	row := drawMetricFixture("two-guards", "selected", drawMetricScore(.21), 1, 1, true)
	row.CompanyQuotes, row.HeadToHead.Sample = nil, 4
	got := drawObservationSecondaryDecision(row)
	if got.Decision != "rejected" || !reflect.DeepEqual(got.ReasonCodes, []string{"sporttery_quotes_incomplete", "head_to_head_insufficient"}) || len(got.Reasons) != 2 || strings.Contains(got.Reason, "分数低") {
		t.Fatalf("missing data mislabeled as low score: %+v", got)
	}
}

func TestDrawObservationSecondaryDoesNotReadResultOrAddOtherHardFilters(t *testing.T) {
	row := drawMetricFixture("label-blind", "selected", drawMetricScore(.3), 0, 0, false)
	// Other analysis/recent inputs may be absent; only the declared guards apply.
	row.FrontendAnalysis = nil
	want := drawObservationSecondaryDecision(row)
	before, _ := json.Marshal(row)
	for _, scores := range [][2]int{{0, 0}, {3, 0}, {0, 5}, {2, 2}} {
		changed := row
		changed.HomeScore, changed.AwayScore, changed.Settled, changed.Status = scores[0], scores[1], true, 4
		if got := drawObservationSecondaryDecision(changed); !reflect.DeepEqual(got, want) {
			t.Fatalf("target result changed secondary decision: %+v", got)
		}
	}
	after, _ := json.Marshal(row)
	if string(before) != string(after) || row.FrozenDrawSignal.State != "selected" || *row.FrozenDrawSignal.Score != .3 {
		t.Fatal("secondary screening mutated the original model/input")
	}
	*want.Score = .1
	if *row.FrozenDrawSignal.Score != .3 {
		t.Fatal("secondary score aliases mutable original signal")
	}
}

func TestDrawObservationMetricsCountsGuardRejectionsInCompleteWindow(t *testing.T) {
	kept := drawMetricFixture("kept", "selected", drawMetricScore(.3), 1, 1, true)
	missingQuote := drawMetricFixture("missing-quote", "selected", drawMetricScore(.3), 0, 2, true)
	missingQuote.CompanyQuotes = nil
	shortHistory := drawMetricFixture("short-history", "selected", drawMetricScore(.3), 2, 2, true)
	shortHistory.HeadToHead.Sample = 4
	got := buildDrawObservationMetrics([]drawObservationItem{kept, missingQuote, shortHistory}, drawObservationQuery{date: kept.Date, scope: "sporttery"})
	if got.Original.Selected != 3 || got.Original.Hits != 2 || got.Secondary.Selected != 1 || got.Secondary.Hits != 1 || got.Rejected.Selected != 2 || got.Rejected.Hits != 1 || got.SecondaryUnavailable != 0 {
		t.Fatalf("guard rejections lost original cohort/coverage: %+v", got)
	}
	drawMetricRequireRate(t, got.Secondary.Precision, 1)
	drawMetricRequireRate(t, got.Secondary.Coverage, .5)
}
