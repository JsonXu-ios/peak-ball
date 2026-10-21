package handlers

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"go_server/internal/drawmodel"
	"go_server/models"

	"gorm.io/datatypes"
)

func drawIntegrationJSON(t *testing.T, value any) datatypes.JSON {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return datatypes.JSON(encoded)
}

func TestDrawMarketAggregationDeVigsEachBookmakerAndDeduplicates(t *testing.T) {
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	rows := []analysisEuroOdd{
		{CompanyID: "a", CompanyName: "A", Odds: []string{"2", "3", "4"}},
		{CompanyID: "a", CompanyName: "A duplicate", Odds: []string{"9", "9", "9"}},
		{CompanyID: "b", CompanyName: "B", Odds: []string{"1.5", "5", "7"}},
		{CompanyID: "average", CompanyName: "平均欧赔", Odds: []string{"3", "3", "3"}},
		{CompanyID: "broken", CompanyName: "Broken", Odds: []string{"2", "-", "4"}},
	}
	odds := models.OddsMoney{Data: drawIntegrationJSON(t, rows), CapturedAt: drawTestPtr(asOf.Add(-time.Minute)), UpdatedAt: asOf.Add(-time.Minute)}
	modelOdds, markets, _ := drawMarketData(odds, models.PankouMoney{}, asOf)
	if modelOdds == nil || markets.Companies != 2 {
		t.Fatalf("wanted 2 valid distinct companies, got %+v", markets)
	}
	want := ((1.0/3)/(1.0/2+1.0/3+1.0/4) + (1.0/5)/(1.0/1.5+1.0/5+1.0/7)) / 2
	actual, ok := drawmodel.MarketDrawProbability(*modelOdds)
	if !ok || math.Abs(actual-want) > 1e-12 {
		t.Fatalf("market probability=%v, want separately de-vigged average %v", actual, want)
	}
}

func TestDrawMarketMissingInvalidAndFutureQuotesStayMissing(t *testing.T) {
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	for _, odds := range []models.OddsMoney{
		{},
		{Data: drawIntegrationJSON(t, []analysisEuroOdd{{CompanyID: "a", Odds: []string{"2", "0", "4"}}}), CapturedAt: drawTestPtr(asOf.Add(-time.Minute)), UpdatedAt: asOf.Add(-time.Minute)},
		{Data: drawIntegrationJSON(t, []analysisEuroOdd{{CompanyID: "a", Odds: []string{"2", "3", "4"}}}), CapturedAt: drawTestPtr(asOf.Add(time.Second)), UpdatedAt: asOf.Add(time.Second)},
		// An auxiliary trade refresh can advance UpdatedAt without fetching odds.
		{Data: drawIntegrationJSON(t, []analysisEuroOdd{{CompanyID: "a", Odds: []string{"2", "3", "4"}}}), UpdatedAt: asOf.Add(-time.Minute)},
	} {
		modelOdds, markets, _ := drawMarketData(odds, models.PankouMoney{}, asOf)
		if modelOdds != nil || markets.DrawOdds != nil || markets.ObservedAt != nil {
			t.Fatalf("unverifiable quote must not create market probability: %+v", markets)
		}
	}
}

func TestDrawMarketCanKeepHandicapFeaturesWithoutInventingEuroProbability(t *testing.T) {
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	pankou := models.PankouMoney{
		Bet365Asia: drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "0", Odds: []string{"0.9", "0.95"}}),
		Bet365Dxq:  drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "2/2.5", Odds: []string{"0.9", "0.95"}}),
		CapturedAt: drawTestPtr(asOf.Add(-time.Minute)),
		UpdatedAt:  asOf.Add(-time.Minute),
	}
	modelOdds, markets, _ := drawMarketData(models.OddsMoney{}, pankou, asOf)
	if modelOdds == nil || modelOdds.AsianHandicap == nil || *modelOdds.AsianHandicap != 0 || modelOdds.GoalLine == nil || *modelOdds.GoalLine != 2.25 || markets.DrawOdds != nil {
		t.Fatalf("valid independent handicap features must survive missing euro odds: %+v, %+v", modelOdds, markets)
	}
	if _, ok := drawmodel.MarketDrawProbability(*modelOdds); ok {
		t.Fatal("handicap-only input must not invent a market draw probability")
	}
	pankou.UpdatedAt = asOf.Add(time.Second)
	pankou.CapturedAt = drawTestPtr(asOf.Add(time.Second))
	modelOdds, markets, _ = drawMarketData(models.OddsMoney{}, pankou, asOf)
	if modelOdds != nil || markets.Handicap != nil || markets.TotalLine != nil {
		t.Fatal("future handicap data must not enter model or market view")
	}
}

func TestDrawHandicapParsesQuarterLinesAndPreservesMissingValues(t *testing.T) {
	for _, test := range []struct {
		line  string
		total bool
		want  *float64
	}{
		{"0", false, drawTestPtr(0.0)},
		{"-0.5/0", false, drawTestPtr(-.25)},
		{"2/2.5", true, drawTestPtr(2.25)},
		{"0", true, nil},
		{"-", false, nil},
		{"", false, nil},
		{"NaN", false, nil},
		{"无盘口", true, nil},
	} {
		got := drawLine(test.line, test.total)
		if (got == nil) != (test.want == nil) || (got != nil && math.Abs(*got-*test.want) > 1e-12) {
			t.Fatalf("line %q total=%v parsed %v, want %v", test.line, test.total, got, test.want)
		}
	}
}

func TestDrawItemNeverBackfillsStartedMatchOrInventsMissingHistory(t *testing.T) {
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	bundle := drawBundle{}
	for _, match := range []models.Money{
		{MatchId: "completed", MatchTime: asOf.Add(-3 * time.Hour), DisplayState: "完场", HomeScore: 1, GuestScore: 1},
		{MatchId: "started", MatchTime: asOf.Add(-time.Minute), DisplayState: "上半场"},
		{MatchId: "empty-history", MatchTime: asOf.Add(time.Hour), DisplayState: "未开始"},
	} {
		item, _, _ := buildDrawItem(match, bundle, asOf)
		if item.Probability != nil || item.Candidate {
			t.Fatalf("must not invent prediction for %s: %+v", match.MatchId, item)
		}
	}
}

func TestDrawItemFrozenProbabilityAndCandidateSurviveSourceChanges(t *testing.T) {
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	saved := drawMatchResponse{MatchID: "frozen", Probability: drawTestPtr(34.5), Candidate: true, AsOf: asOf, ModelVersion: "original-model", Quality: "complete"}
	bundle := drawBundle{Predictions: map[string]models.DrawPrediction{
		"frozen": {ID: 7, Payload: drawIntegrationJSON(t, saved), Settled: true, IsDraw: drawTestPtr(false), HomeScore: drawTestPtr(2), AwayScore: drawTestPtr(1)},
	}}
	item, _, _ := buildDrawItem(models.Money{MatchId: "frozen", MatchTime: asOf.Add(time.Hour), DisplayState: "完场", HomeScore: 2, GuestScore: 1}, bundle, asOf.Add(4*time.Hour))
	if !item.Frozen || item.PredictionID == nil || *item.PredictionID != 7 || !item.Candidate || item.Probability == nil || *item.Probability != 34.5 || item.ModelVersion != "original-model" || !item.AsOf.Equal(asOf) || !item.Result.Settled || *item.Result.IsDraw {
		t.Fatalf("frozen forecast was rewritten: %+v", item)
	}
}

func TestDrawHistoryConservativeDateOnlyAvailabilityAndInvalidScores(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	history := models.HistoryMoney{RecentHomeList: drawIntegrationJSON(t, []map[string]any{
		{"matchId": 42, "homeId": 1, "guestId": 2, "home": "H", "guest": "A", "matchTime": "2026-10-19", "goal": []int{1, 1}, "league": "L"},
		{"matchId": 43, "homeId": 1, "guestId": 3, "matchTime": "2026-10-18", "goal": []int{-1, 0}},
		{"matchId": 44, "homeId": 1, "guestId": 4, "matchTime": "unknown", "goal": []int{0, 0}},
	})}
	games := drawHistoryGames(history, models.Money{League: "L", LeagueId: 9})
	wantKickoff := drawSourceTime(time.Date(2026, 10, 19, 23, 59, 59, 0, time.UTC))
	if len(games) != 1 || games[0].ID != "42" || games[0].LeagueID != "9" || !games[0].Kickoff.Equal(wantKickoff) || !games[0].CompletedAt.Equal(games[0].Kickoff.Add(2*time.Hour)) {
		t.Fatalf("incorrect date-only or score handling: %+v", games)
	}
}

func TestDrawHistoryNormalizesMissingAndZeroIdentifiers(t *testing.T) {
	history := models.HistoryMoney{RecentHomeList: drawIntegrationJSON(t, []map[string]any{
		{"matchId": 0, "homeId": 0, "guestId": -1, "home": "H", "guest": "A", "matchTime": "2026-10-19", "goal": []int{1, 1}, "leagueId": 0, "league": "Other"},
	})}
	games := drawHistoryGames(history, models.Money{League: "L", LeagueId: 9})
	if len(games) != 1 || games[0].ID != "" || games[0].HomeTeamID != "" || games[0].AwayTeamID != "" || games[0].LeagueID != "" {
		t.Fatalf("zero and negative IDs must remain missing for safe name fallback: %+v", games)
	}
}

func TestDrawSourceWallClockConversionUsesConfiguredTimezone(t *testing.T) {
	raw := time.Date(2026, 10, 20, 16, 30, 0, 0, time.UTC)
	drawTestSourceZone(t, "Asia/Shanghai")
	if got, want := drawSourceTime(raw), time.Date(2026, 10, 20, 8, 30, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Beijing source wall clock got %v, want %v", got, want)
	}
	if !drawSourceTime(time.Time{}).IsZero() {
		t.Fatal("missing source time must remain missing")
	}
	drawTestSourceZone(t, "UTC")
	if got := drawSourceTime(raw); !got.Equal(raw) {
		t.Fatalf("UTC source wall clock must remain UTC: %v", got)
	}
}
