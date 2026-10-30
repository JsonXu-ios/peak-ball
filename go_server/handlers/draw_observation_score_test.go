package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"go_server/models"
	"gorm.io/datatypes"
)

func drawScoreTestFixture() drawObservationItem {
	score := .4
	kickoff := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	line := "三球"
	row := drawObservationItem{MatchID: "score-target", JingcaiID: "202611054001", MatchTime: kickoff, FrozenDrawSignal: &drawObservationFrozenSignal{State: "selected", Score: &score, ObservedInputs: 202, UsedInputs: 202}, HeadToHead: drawObservationRecent{Sample: 5}, CompanyQuotes: []drawObservationCompanyQuotes{drawSecondaryGuardQuoteFixture(), {Key: "bet365", GoalsLine: drawObservationLine{Source: "pankou_moneys", Company: "Bet365", Latest: &line}}}}
	row.ScoreInputs = &drawScoreInputs{MatchID: row.MatchID, Source: "sporttery_score_odds", Quotes: []drawScoreQuote{{Home: 0, Away: 0, Odds: 9, SourceField: "sd00"}, {Home: 1, Away: 1, Odds: 6, SourceField: "sd11"}, {Home: 2, Away: 2, Odds: 5, SourceField: "sd22"}, {Home: 3, Away: 3, Odds: 8, SourceField: "sd33"}}}
	for i := 0; i < 5; i++ {
		record := drawObservationRecentMatch{MatchID: fmt.Sprintf("home-%d", i), MatchTime: kickoff.Add(-time.Duration(i+1) * 24 * time.Hour), HomeScore: 2, AwayScore: 2, Result: "draw", Venue: "home"}
		row.HomeRecent.Matches = append(row.HomeRecent.Matches, record)
		record.MatchID = fmt.Sprintf("away-%d", i)
		row.AwayRecent.Matches = append(row.AwayRecent.Matches, record)
		record.MatchID = fmt.Sprintf("h2h-%d", i)
		row.HeadToHead.Matches = append(row.HeadToHead.Matches, record)
	}
	row.HomeRecent.Sample, row.AwayRecent.Sample = 5, 5
	return row
}

func TestDrawScoreRecommendationUsesRowAwareSecondaryDecision(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*drawObservationItem)
		want   string
	}{
		{"boundary passes", func(r *drawObservationItem) { v := .225; r.FrozenDrawSignal.Score = &v }, "recommended"},
		{"below threshold", func(r *drawObservationItem) { v := .224999999; r.FrozenDrawSignal.Score = &v }, "not_selected"},
		{"original not selected", func(r *drawObservationItem) { r.FrozenDrawSignal.State = "not_selected" }, "not_selected"},
		{"non lottery", func(r *drawObservationItem) { r.JingcaiID = "" }, "out_of_scope"},
		{"out of scope signal", func(r *drawObservationItem) { r.FrozenDrawSignal.State = "out_of_scope" }, "out_of_scope"},
		{"missing signal", func(r *drawObservationItem) { r.FrozenDrawSignal = nil }, "unavailable"},
		{"missing score", func(r *drawObservationItem) { r.FrozenDrawSignal.Score = nil }, "unavailable"},
		{"NaN", func(r *drawObservationItem) { v := math.NaN(); r.FrozenDrawSignal.Score = &v }, "unavailable"},
		{"Inf", func(r *drawObservationItem) { v := math.Inf(1); r.FrozenDrawSignal.Score = &v }, "unavailable"},
		{"outside score range", func(r *drawObservationItem) { v := 1.1; r.FrozenDrawSignal.Score = &v }, "unavailable"},
		{"four H2H", func(r *drawObservationItem) { r.HeadToHead.Sample = 4 }, "not_selected"},
		{"missing original SPF", func(r *drawObservationItem) { r.CompanyQuotes = r.CompanyQuotes[1:] }, "not_selected"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := drawScoreTestFixture()
			tt.mutate(&row)
			got := drawObservationScoreRecommendation(row)
			if got.State != tt.want || got.Version != drawScoreRecommendationVersion {
				t.Fatalf("got %+v, want %s", got, tt.want)
			}
			if tt.want != "recommended" && len(got.Scores) != 0 {
				t.Fatalf("rejected row leaked scores: %+v", got)
			}
		})
	}
}

func TestDrawScoreInputsBindCanonicalIdentityAndExactFields(t *testing.T) {
	raw := datatypes.JSON(`{"sportteryOdds":{"code":0,"data":{"matchId":3073080,"bf":{"sd00":18,"sd11":"7.25","sd22":10,"sd33":32,"sd4":1.01,"stop":false}}}}`)
	got := drawObservationBuildScoreInputs("498235450", models.OddsMoney{MatchId: "498235450", SportteryTrade: raw})
	if got == nil || got.MatchID != "498235450" || got.SourceMatchID != "3073080" || got.Source != "sporttery_score_odds" || len(got.Quotes) != 4 || got.Stopped == nil || *got.Stopped {
		t.Fatalf("wrong quote extraction: %+v", got)
	}
	for _, q := range got.Quotes {
		if q.Home != q.Away || q.Home > 3 || q.SourceField == "sd4" {
			t.Fatalf("aggregate leaked into exact score: %+v", q)
		}
	}
	if got := drawObservationBuildScoreInputs("other", models.OddsMoney{MatchId: "498235450", SportteryTrade: raw}); got != nil {
		t.Fatal("mismatched canonical row accepted")
	}
	for _, raw := range []string{`{}`, `null`, `garbage`, `{"sportteryOdds":{"data":{"bf":{"sd11":6,"sd22":5}}}}`, `{"sportteryOdds":{"code":1,"data":{"bf":{"sd11":6,"sd22":5}}}}`} {
		if got := drawObservationBuildScoreInputs("m", models.OddsMoney{MatchId: "m", SportteryTrade: datatypes.JSON(raw)}); got != nil {
			t.Fatalf("invalid response accepted: %s %+v", raw, got)
		}
	}
}

func TestDrawScoreRealPricesPermitTwoTwoAndThreeThree(t *testing.T) {
	row := drawScoreTestFixture()
	row.ScoreInputs.Quotes = []drawScoreQuote{{Home: 0, Away: 0, Odds: 20, SourceField: "sd00"}, {Home: 1, Away: 1, Odds: 9, SourceField: "sd11"}, {Home: 2, Away: 2, Odds: 6, SourceField: "sd22"}, {Home: 3, Away: 3, Odds: 5, SourceField: "sd33"}}
	got := drawObservationScoreRecommendation(row)
	want := []drawRecommendedScore{{Home: 3, Away: 3}, {Home: 2, Away: 2}}
	if got.State != "recommended" || got.Source != "sporttery_score_odds" || !reflect.DeepEqual(got.Scores, want) || got.Evidence == nil || got.Evidence.Quotes[0].Odds != 5 {
		t.Fatalf("wrong ranking: %+v", got)
	}
	row.HomeRecent.Matches = nil
	row.AwayRecent.Matches = nil
	row.HeadToHead.Matches = nil
	if again := drawObservationScoreRecommendation(row); !reflect.DeepEqual(again, got) {
		t.Fatal("BF quotes wrongly require fallback history")
	}
	row.HomeScore, row.AwayScore, row.Settled, row.Status = 8, 0, true, -1
	if again := drawObservationScoreRecommendation(row); !reflect.DeepEqual(again, got) {
		t.Fatal("target result influenced BF recommendation")
	}
}

func TestDrawScoreTieUsesRealBet365LineAndSurvivesJSON(t *testing.T) {
	row := drawScoreTestFixture()
	row.ScoreInputs.Quotes = []drawScoreQuote{{Home: 1, Away: 1, Odds: 5, SourceField: "sd11"}, {Home: 2, Away: 2, Odds: 5, SourceField: "sd22"}}
	line := "四球"
	row.CompanyQuotes[1].GoalsLine.Latest = &line
	historyLine := "二球"
	row.GoalsLine = drawObservationLine{Latest: &historyLine, Source: "history_moneys", Company: "historical"}
	got := drawObservationScoreRecommendation(row)
	if got.Scores[0] != (drawRecommendedScore{Home: 2, Away: 2}) || got.Evidence.GoalsSource != "pankou_moneys / Bet365" {
		t.Fatalf("true goals quote not used for tie: %+v", got)
	}
	body, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var restored drawObservationItem
	if err = json.Unmarshal(body, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, drawObservationScoreRecommendation(restored)) {
		t.Fatal("serialized row changed score recommendation")
	}
	row.CompanyQuotes[1].GoalsLine.Source = "history_moneys"
	if got := drawObservationScoreRecommendation(row); got.Scores[0] != (drawRecommendedScore{Home: 1, Away: 1}) || got.Evidence.GoalsLine != nil {
		t.Fatalf("history fallback influenced BF tie: %+v", got)
	}
	row.ScoreInputs.Quotes[0].Odds = 4
	if got := drawObservationScoreRecommendation(row); got.Scores[0] != (drawRecommendedScore{Home: 1, Away: 1}) {
		t.Fatalf("line overrode lower actual quote: %+v", got)
	}
}

func TestDrawScoreInvalidOrForeignQuotesCannotBecomeRecommendation(t *testing.T) {
	row := drawScoreTestFixture()
	row.HomeRecent.Matches = nil
	row.ScoreInputs.Quotes = []drawScoreQuote{{Home: 4, Away: 4, Odds: 1.01, SourceField: "sd4"}, {Home: 1, Away: 1, Odds: math.NaN(), SourceField: "sd11"}, {Home: 2, Away: 2, Odds: 1, SourceField: "sd22"}}
	if got := drawObservationScoreRecommendation(row); got.State != "unavailable" || len(got.Scores) != 0 {
		t.Fatalf("invalid quotes gave fixed fallback: %+v", got)
	}
	row.ScoreInputs = drawScoreTestFixture().ScoreInputs
	row.ScoreInputs.MatchID = "another-match"
	if got := drawObservationScoreRecommendation(row); got.Source == "sporttery_score_odds" || len(got.Scores) != 0 {
		t.Fatalf("another match's quotes were borrowed: %+v", got)
	}
	row.ScoreInputs = drawScoreTestFixture().ScoreInputs
	row.ScoreInputs.Quotes = append(row.ScoreInputs.Quotes, row.ScoreInputs.Quotes...)
	if got := drawObservationScoreRecommendation(row); got.State != "unavailable" {
		t.Fatalf("duplicate conflicting quote fields accepted: %+v", got)
	}
}

func TestDrawScorePoissonFallbackUsesSafeHistoryAndActualLine(t *testing.T) {
	row := drawScoreTestFixture()
	row.ScoreInputs = nil
	for _, records := range [][]drawObservationRecentMatch{row.HomeRecent.Matches, row.AwayRecent.Matches, row.HeadToHead.Matches} {
		for i := range records {
			records[i].HomeScore, records[i].AwayScore = 4, 3
			records[i].Result = "win"
		}
	}
	line := "六球"
	row.CompanyQuotes[1].GoalsLine.Latest = &line
	got := drawObservationScoreRecommendation(row)
	if got.State != "recommended" || got.Source != "goal_estimate" || got.Scores[0] != (drawRecommendedScore{Home: 3, Away: 3}) || got.Evidence == nil || got.Evidence.HomeSample != 5 || got.Evidence.AwaySample != 5 || got.Evidence.HeadToHeadSample != 5 || got.Evidence.Weights == nil || *got.Evidence.Weights != (drawScoreWeights{Recent: .5, History: .2, Market: .3}) || !strings.Contains(got.Reason, "非真实比分报价") {
		t.Fatalf("wrong transparent fallback: %+v", got)
	}
	body, _ := json.Marshal(row)
	var restored drawObservationItem
	_ = json.Unmarshal(body, &restored)
	if again := drawObservationScoreRecommendation(restored); !reflect.DeepEqual(again, got) {
		t.Fatal("fallback not stable after JSON")
	}
	row.HomeScore, row.AwayScore, row.Settled, row.Status = 99, 98, true, -1
	if again := drawObservationScoreRecommendation(row); !reflect.DeepEqual(again, got) {
		t.Fatal("target result influenced fallback")
	}
	bad := drawObservationRecentMatch{MatchID: row.MatchID, MatchTime: row.MatchTime.Add(-24 * time.Hour), HomeScore: 50, AwayScore: 50, Venue: "home", Result: "draw"}
	future := bad
	future.MatchID = "future"
	future.MatchTime = row.MatchTime.Add(time.Hour)
	boundary := bad
	boundary.MatchID = "boundary"
	boundary.MatchTime = row.MatchTime.Add(-2 * time.Hour)
	unknown := bad
	unknown.MatchID = "unknown-time"
	unknown.MatchTime = time.Time{}
	for _, list := range []*[]drawObservationRecentMatch{&row.HomeRecent.Matches, &row.AwayRecent.Matches, &row.HeadToHead.Matches} {
		*list = append([]drawObservationRecentMatch{bad, future, boundary, unknown}, (*list)...)
	}
	if again := drawObservationScoreRecommendation(row); !reflect.DeepEqual(again, got) {
		t.Fatalf("unsafe target/future/boundary records influenced fallback: %+v", again)
	}
}

func TestDrawScorePoissonFallbackRequiresEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*drawObservationItem)
	}{
		{"home fewer than five", func(r *drawObservationItem) { r.HomeRecent.Matches = r.HomeRecent.Matches[:4] }},
		{"away fewer than five", func(r *drawObservationItem) { r.AwayRecent.Matches = r.AwayRecent.Matches[:4] }},
		{"h2h actual records fewer than five", func(r *drawObservationItem) { r.HeadToHead.Matches = r.HeadToHead.Matches[:4] }},
		{"unknown kickoff", func(r *drawObservationItem) { r.MatchTime = time.Time{} }},
		{"no real line", func(r *drawObservationItem) { r.CompanyQuotes[1].GoalsLine.Source = "history_moneys" }},
		{"unknown team venue", func(r *drawObservationItem) { r.HomeRecent.Matches[0].Venue = "" }},
		{"unfinished marker", func(r *drawObservationItem) { r.HomeRecent.Matches[0].Result = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := drawScoreTestFixture()
			row.ScoreInputs = nil
			tt.mutate(&row)
			got := drawObservationScoreRecommendation(row)
			if got.State != "unavailable" || len(got.Scores) != 0 {
				t.Fatalf("missing evidence silently recommended: %+v", got)
			}
		})
	}
}
