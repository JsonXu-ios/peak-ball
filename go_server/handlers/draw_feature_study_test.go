package handlers

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"go_server/models"
	"gorm.io/datatypes"
)

func TestDrawFeatureStudyFullProjectionUsesSharedValuesAndRejectsOutcomeFields(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	row := drawFeatureStudyBuildRow(match, history, odds, pankou)
	want := buildDrawObservationFrontend(match, history, odds, pankou)
	if !reflect.DeepEqual(row.CompanyQuotes, want.companies) {
		t.Fatal("original company quotes changed")
	}
	if row.Frontend == nil || row.Frontend.RoiSimulation == nil {
		t.Fatal("full frontend projection missing")
	}
	keys := map[string]bool{}
	for _, market := range row.Frontend.RoiSimulation.Markets {
		if market.Simulated && (market.Key == "sportteryRqspf" || market.Key == "sportteryRqspfSim") {
			t.Fatal("simulated handicap was retained")
		}
		keys[market.Key] = true
		found := false
		for _, original := range want.analysis.RoiSimulation.Markets {
			if original.Key == market.Key {
				found = reflect.DeepEqual(original, market)
			}
		}
		if !found {
			t.Fatal("full-market quote, support or profit changed")
		}
	}
	for _, key := range []string{"william", "bet365", "sporttery", "sportterySim", "sportteryRqspf"} {
		if !keys[key] {
			t.Fatalf("real market or result simulation was dropped: %s", key)
		}
	}
	if row.AnalysisInputs.DistributionSources["william"] != "tzbl" || row.AnalysisInputs.DistributionSources["bet365"] != "tzbl" || row.AnalysisInputs.DistributionSources["sporttery"] != "jyykSpf" || row.AnalysisInputs.DistributionSources["sportterySim"] != "localSanhu" || row.AnalysisInputs.DistributionSources["sportteryRqspf"] != "jyykRqspf" {
		t.Fatalf("per-market support provenance lost: %+v", row.AnalysisInputs.DistributionSources)
	}
	response := buildAnalysisFromData(match, history, odds, pankou, sportteryTradeFromJSON(odds.SportteryTrade))
	attachAnalysisWeights(&response)
	if !reflect.DeepEqual(row.AnalysisInputs.Platform, buildPlatformDecision(&response)) || !reflect.DeepEqual(row.AnalysisInputs.MarketProbabilities, probabilitiesFromOdds(response.averageOdds)) {
		t.Fatal("pure platform or real-odds probability changed")
	}
	encoded, err := json.Marshal(struct {
		Frontend *drawObservationFrontendAnalysis `json:"frontend"`
		Inputs   drawFeatureStudyAnalysisInputs   `json:"analysisInputs"`
		Quotes   []drawObservationCompanyQuotes   `json:"companyQuotes"`
	}{row.Frontend, row.AnalysisInputs, row.CompanyQuotes})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"homeScore", "guestScore", "awayScore", "predictionHit", "goddessWoman", "myAngle", "accuracyFit", "displayState", "status"} {
		if strings.Contains(string(encoded), `"`+key+`":`) {
			t.Fatalf("target/post-outcome container leaked: %s", key)
		}
	}
	missing := drawFeatureStudyBuildRow(match, models.HistoryMoney{}, models.OddsMoney{}, models.PankouMoney{})
	if missing.AnalysisInputs.MarketProbabilities != nil || missing.AnalysisInputs.AverageLatest != nil || missing.AnalysisInputs.Availability["averageLatest"] || missing.AnalysisInputs.Availability["strictProfessionalInputs"] {
		t.Fatal("fallback probabilities or professional source became real market inputs")
	}
}

func TestDrawFeatureStudyRawTradeFieldsKeepSourceZerosSeparateFromDefaults(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	odds.SportteryTrade = datatypes.JSON(`{"data":{"tzbl":{"h":2,"d":3,"a":4,"hsupportRate":40,"dsupportRate":30,"asupportRate":30,"hprobability":0,"psyError":0},"jyykSpf":{"h":2,"d":3,"a":4,"hlr":0,"dlr":null,"alr":-2}}}`)
	row := drawFeatureStudyBuildRow(match, history, odds, pankou)
	for _, field := range []string{"tzbl.hprobability", "tzbl.psyError", "jyykSpf.hlr"} {
		if row.AnalysisInputs.TradeFields[field] == nil || *row.AnalysisInputs.TradeFields[field] != 0 {
			t.Fatalf("real source zero discarded: %s", field)
		}
	}
	for _, field := range []string{"tzbl.herror", "tzbl.dprobability", "jyykSpf.dlr", "jyykSpf.hy"} {
		if row.AnalysisInputs.TradeFields[field] != nil {
			t.Fatalf("absent or null source field became default zero: %s", field)
		}
	}
	if row.AnalysisInputs.HotColdSources["sporttery"] != [3]string{"official", "missing_default", "official"} || row.AnalysisInputs.HotColdSources["sportterySim"] != [3]string{"local_simulated", "local_simulated", "local_simulated"} {
		t.Fatalf("actual versus local/default hot-cold source lost: %+v", row.AnalysisInputs.HotColdSources)
	}
}

func TestDrawFeatureStudyPreMatchHistoryUsesCanonicalIdentityAndCompletionBuffer(t *testing.T) {
	kickoff := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	target := drawObservationGame("target", "101", "202", kickoff, 1, 1)
	main := drawObservationMain(target).Match
	games := []drawHistoricalGame{
		target,
		drawObservationGame("past-draw", "101", "202", kickoff.Add(-24*time.Hour), 0, 0),
		drawObservationGame("past-reversed", "202", "101", kickoff.Add(-48*time.Hour), 2, 0),
		drawObservationGame("still-playing", "101", "202", kickoff.Add(-time.Hour), 1, 1),
		drawObservationGame("exact-buffer", "101", "202", kickoff.Add(-2*time.Hour), 1, 1),
		drawObservationGame("future", "101", "202", kickoff.Add(24*time.Hour), 1, 1),
		drawObservationGame("target", "101", "202", kickoff.Add(-72*time.Hour), 9, 0),
	}
	unfinished := drawObservationGame("unfinished", "101", "202", kickoff.Add(-96*time.Hour), 1, 1)
	unfinished.Game.Completed = false
	games = append(games, unfinished)
	index := indexDrawObservations(games)
	got := drawStudyPreMatchHistory(index, main)
	if got.HomeRecent.Sample != 2 || got.AwayRecent.Sample != 2 || got.HeadToHead.Sample != 2 || got.HomeRecent.Draws != 1 || got.HeadToHead.Draws != 1 {
		t.Fatalf("strict pre-match windows changed: %+v", got)
	}
	for _, window := range []drawObservationRecent{got.HomeRecent, got.AwayRecent, got.HeadToHead} {
		for _, entry := range window.Matches {
			if entry.MatchID != "past-draw" && entry.MatchID != "past-reversed" {
				t.Fatalf("non-as-of history leaked: %s", entry.MatchID)
			}
			if !entry.MatchTime.Add(2 * time.Hour).Before(kickoff) {
				t.Fatal("completion buffer was omitted")
			}
		}
	}
	main.HomeScore, main.GuestScore, main.DisplayState = 7, 0, "未开始"
	if !reflect.DeepEqual(got, drawStudyPreMatchHistory(index, main)) {
		t.Fatal("target outcome or settlement state affected pre-match windows")
	}
}

func TestDrawFeatureStudyGoalsHeatCopiesFrontendAndExportsActualInputs(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	row := drawFeatureStudyBuildRow(match, history, odds, pankou)
	want := buildDrawObservationFrontend(match, history, odds, pankou).analysis
	heat := row.GoalsHeat
	if !heat.Available || heat.Over == nil || heat.Under == nil || *heat.Over != want.QiuShuTouZhu[0].(float64) || *heat.Under != want.QiuShuTouZhu[1].(float64) {
		t.Fatalf("goals heat differs from the actual shared frontend: %+v", heat)
	}
	if heat.OpeningLine == nil || *heat.OpeningLine != 2.5 || heat.LatestLine == nil || *heat.LatestLine != 2.75 || heat.LineSource != "latest" || heat.Inputs.CalculationLine != want.QiushuPankou2 {
		t.Fatalf("actual company lines or calculation-line source were lost: %+v", heat)
	}
	if heat.Inputs.RecentHomeMatches != 1 || heat.Inputs.RecentGuestMatches != 1 || heat.Inputs.RecentHomeTotalGoals != 2 || heat.Inputs.RecentGuestTotalGoals != 2 || heat.Inputs.RecentTotalGoals != 2 {
		t.Fatalf("actual goals-pressure inputs were changed: %+v", heat.Inputs)
	}
	expected := round2(clamp(50+(heat.Inputs.RecentTotalGoals-heat.Inputs.CalculationLine)*18, 0, 100))
	if math.Abs(*heat.Over-expected) > 1e-9 {
		t.Fatal("exported inputs cannot reproduce the displayed pressure")
	}
}

func TestDrawFeatureStudyGoalsHeatRejectsDefaultPressuresButKeepsScoredZeros(t *testing.T) {
	match, history, odds, _ := drawObservationDisplayFixture()
	pankou := models.PankouMoney{Bet365Dxq: datatypes.JSON(`{"companyId":8,"companyName":"Bet365","firstPankou":"2.5","pankou":"2.5"}`)}
	empty := drawFeatureStudyBuildRow(match, models.HistoryMoney{}, odds, pankou).GoalsHeat
	if empty.Available || empty.Over == nil || empty.Under == nil || *empty.Over != 5 || *empty.Under != 95 || empty.Reason != "missing_recent_home" {
		t.Fatalf("empty recent lists became an observed 95%% under pressure: %+v", empty)
	}
	noLine := drawFeatureStudyBuildRow(match, models.HistoryMoney{}, odds, models.PankouMoney{}).GoalsHeat
	if noLine.Available || noLine.Over == nil || *noLine.Over != 50 || noLine.LatestLine != nil || noLine.LineSource != "missing" {
		t.Fatalf("no history/no line became an observed 50%% pressure: %+v", noLine)
	}
	history.RecentHomeList = datatypes.JSON(`[{"matchTime":"2026-10-01","home":"Home","guest":"Other","goal":[0,0]}]`)
	history.RecentGuestList = datatypes.JSON(`[{"matchTime":"2026-10-01","home":"Other","guest":"Away","goal":[0,0]}]`)
	zeros := drawFeatureStudyBuildRow(match, history, odds, pankou).GoalsHeat
	if !zeros.Available || zeros.Over == nil || *zeros.Over != 5 || zeros.Inputs.RecentHomeMatches != 1 || zeros.Inputs.RecentGuestMatches != 1 || zeros.Inputs.RecentTotalGoals != 0 {
		t.Fatalf("genuine zero-goal recent results became missing data: %+v", zeros)
	}
	pankou.Bet365Dxq = datatypes.JSON(`{"companyId":8,"companyName":"Bet365","pankou":"2.5"}`)
	onlyLatest := drawFeatureStudyBuildRow(match, history, odds, pankou).GoalsHeat
	if !onlyLatest.Available || onlyLatest.OpeningLine != nil {
		t.Fatal("missing unused opening line suppressed a real latest-line pressure")
	}
	pankou.Bet365Dxq = datatypes.JSON(`{"companyId":8,"companyName":"Bet365","firstPankou":"2.5"}`)
	openingFallback := drawFeatureStudyBuildRow(match, history, odds, pankou).GoalsHeat
	if openingFallback.Available || openingFallback.LatestLine != nil || openingFallback.LineSource != "opening_fallback" || openingFallback.Inputs.CalculationLine != 2.5 {
		t.Fatal("opening-line substitution became a real latest quote")
	}
	historyFallback := drawFeatureStudyBuildRow(match, history, odds, models.PankouMoney{}).GoalsHeat
	if historyFallback.Available || historyFallback.LineSource != "history_fallback" || historyFallback.LatestLine != nil {
		t.Fatal("history-derived line became a real quote")
	}
}

func TestDrawFeatureStudyGoalsHeatRejectsUnsafeRecentInputs(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	for _, raw := range []string{
		`[{"matchId":"display","matchTime":"2026-10-01","home":"Home","guest":"Other","goal":[1,1]}]`,
		`[{"matchId":"other","matchTime":"2030-01-01","home":"Home","guest":"Other","goal":[1,1]}]`,
		`[{"matchId":"other","matchTime":"unknown","home":"Home","guest":"Other","goal":[1,1]}]`,
		`[{"matchId":"other","matchTime":"2026-10-01","home":"Home","guest":"Other","goal":[-1,1]}]`,
	} {
		history.RecentHomeList = datatypes.JSON(raw)
		heat := drawFeatureStudyBuildRow(match, history, odds, pankou).GoalsHeat
		if heat.Available {
			t.Fatalf("unsafe or invalid recent input became usable heat: %s", raw)
		}
	}
}

func TestDrawFeatureStudyProfitUsesTheDisplayedMarketsWithoutHandicapSimulations(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	row := drawFeatureStudyBuildRow(match, history, odds, pankou)
	want := buildDrawObservationFrontend(match, history, odds, pankou).analysis.RoiSimulation
	if row.RoiSimulation == nil || row.RoiSimulation.TotalStake != bookmakerTotalStake {
		t.Fatal("shared fixed profit base missing")
	}
	keys := map[string]bool{}
	for _, got := range row.RoiSimulation.Markets {
		if got.Key != "sporttery" && got.Key != "sportterySim" {
			t.Fatalf("not a displayed result-profit market: %s", got.Key)
		}
		keys[got.Key] = true
		found := false
		for _, original := range want.Markets {
			if original.Key == got.Key {
				found = reflect.DeepEqual(got, original)
			}
		}
		if !found {
			t.Fatal("export changed the shared frontend profit fields")
		}
	}
	if !keys["sporttery"] || !keys["sportterySim"] {
		t.Fatal("official and comparison market must remain separate")
	}
	if row.ProfitInputs.DistributionSources["sporttery"] != "jyykSpf" || row.ProfitInputs.DistributionSources["sportterySim"] != "localSanhu" {
		t.Fatal("official support and local comparison support were mixed")
	}
	for _, present := range row.ProfitInputs.OfficialProfitRatePresence {
		if present {
			t.Fatal("absent raw official profit rate became an observed zero")
		}
	}
}

func TestDrawFeatureStudyProfitPresenceKeepsRealZeroAndDistributionFallbacks(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	odds.SportteryTrade = datatypes.JSON(`{"data":{"tzbl":{"h":2,"d":3,"a":4,"hsupportRate":40,"dsupportRate":30,"asupportRate":30},"jyykSpf":{"hy":0,"dy":-12,"ay":null}}}`)
	row := drawFeatureStudyBuildRow(match, history, odds, pankou)
	if row.ProfitInputs.OfficialProfitRatePresence != [3]bool{true, true, false} || row.ProfitInputs.DistributionSources["sporttery"] != "tzbl" {
		t.Fatal("actual zero, missing rate, or official ratio fallback was lost")
	}
	odds.SportteryTrade = datatypes.JSON(`{"data":{"tzbl":{"h":2,"d":3,"a":4}}}`)
	row = drawFeatureStudyBuildRow(match, history, odds, pankou)
	if row.ProfitInputs.DistributionSources["sporttery"] != "localSanhu" {
		t.Fatal("official-priced market silently treated local fallback support as official")
	}
	odds.SportteryTrade = nil
	row = drawFeatureStudyBuildRow(match, history, odds, pankou)
	if len(row.RoiSimulation.Markets) != 1 || row.RoiSimulation.Markets[0].Key != "sporttery" || !row.RoiSimulation.Markets[0].Simulated || row.ProfitInputs.DistributionSources["sporttery"] != "localSanhu" {
		t.Fatal("non-sporttery fallback was merged with true official or comparison data")
	}
	missing := drawFeatureStudyBuildRow(match, models.HistoryMoney{}, models.OddsMoney{}, models.PankouMoney{})
	for _, market := range missing.RoiSimulation.Markets {
		for _, outcome := range market.BookmakerByOutcome {
			if market.OddsAvailable || outcome.Available {
				t.Fatal("missing odds became observed zero-profit data")
			}
		}
	}
}

func TestDrawFeatureStudyHistoryAuditFollowsActualFiveSlotsAndKeepsUpdateSeparate(t *testing.T) {
	match, history, _, _ := drawObservationDisplayFixture()
	kickoff := drawSourceTime(match.MatchTime)
	before, after := kickoff.Add(-24*time.Hour), kickoff.Add(time.Hour)
	history.MatchId, history.CapturedAt, history.UpdatedAt = match.MatchId, &before, after
	text := func(value time.Time) string { return value.In(drawSourceLocation).Format("2006-01-02 15:04:05") }
	entries := []map[string]interface{}{
		{"matchId": match.MatchId, "matchTime": text(kickoff), "goal": []int{1, 1}},
		{"matchId": "other", "matchTime": text(before), "goal": []int{2, 1}},
		{"matchId": "unknown", "matchTime": "unknown", "goal": []int{0, 0}},
		{"matchId": "future", "matchTime": text(after), "goal": []int{1, 0}},
		{"matchId": "ignored_invalid", "matchTime": text(after), "goal": []int{1}},
		{"matchId": match.MatchId, "matchTime": text(after), "goal": []int{1, 1}},
	}
	body, _ := json.Marshal(entries)
	history.RecentHomeList, history.RecentGuestList = datatypes.JSON(body), nil
	history.AgainstList = datatypes.JSON(body)
	audit := drawStudyHistoryInputs(match, history)
	if !audit.Present || audit.RecentHomeUsedCount != 4 || audit.RecentGuestUsedCount != 0 || audit.UsedRecentTargetID != 1 || audit.UsedRecentAtOrAfterKickoff != 2 || audit.UsedRecentUnknownTime != 1 || audit.AgainstListedTargetID != 2 {
		t.Fatalf("history audit differs from actual consumed recent slots: %+v", audit)
	}
	if audit.CapturedPreKickoff == nil || !*audit.CapturedPreKickoff || audit.UpdatedAt == nil || !audit.UpdatedAt.Equal(after) {
		t.Fatal("updated_at replaced real capture metadata")
	}
	history = models.HistoryMoney{MatchId: match.MatchId, LeagueStat: datatypes.JSON(`{"against":{"summary":{"all":3},"list":[]},"recent":{"home":{"list":[{"matchId":"display","matchTime":"unknown","goal":[1,1]}]},"guest":{"list":[]}}}`)}
	audit = drawStudyHistoryInputs(match, history)
	if audit.AgainstSummaryCount != 3 || audit.RecentHomeUsedCount != 1 || audit.UsedRecentTargetID != 1 || audit.UsedRecentUnknownTime != 1 {
		t.Fatal("legacy normalized history source not audited")
	}
}

func TestDrawFeatureStudyMarketInputsDoNotDependOnTargetOutcome(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	first := drawFeatureStudyBuildRow(match, history, odds, pankou)
	match.HomeScore, match.GuestScore = 0, 7
	match.HomeOtScore, match.GuestOtPenalty = 9, 5
	match.DisplayState = "未开始"
	second := drawFeatureStudyBuildRow(match, history, odds, pankou)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("target result or settlement state leaked into market inputs")
	}
	if first.HomeHeat == nil {
		t.Fatal("complete average and actual Asian opening/latest lines should yield the UI heat")
	}
}

func TestDrawFeatureStudyMissingMarketInputsStayMissing(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	for _, input := range []struct {
		o models.OddsMoney
		p models.PankouMoney
	}{{models.OddsMoney{}, pankou}, {odds, models.PankouMoney{}}, {models.OddsMoney{}, models.PankouMoney{}}} {
		row := drawFeatureStudyBuildRow(match, history, input.o, input.p)
		if row.HomeHeat != nil {
			t.Fatal("missing prices or actual lines became default 50% heat")
		}
	}
	row := drawFeatureStudyBuildRow(match, history, models.OddsMoney{}, models.PankouMoney{})
	if row.Latest.Kelly != nil || row.Latest.SportteryDiff != nil || row.Latest.AverageDiff != nil || row.Opening.Kelly != nil {
		t.Fatal("missing market became generic predictions or zero differences")
	}
	// A genuine level handicap is valid; it must not be confused with no record.
	pankou = models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":8,"firstPankou":"平手","pankou":"0"}]}`)}
	row = drawFeatureStudyBuildRow(match, history, odds, pankou)
	if row.HomeHeat == nil || row.AsianLine == nil || *row.AsianLine != 0 {
		t.Fatal("real level handicap was dropped")
	}
}

func TestDrawFeatureStudyTimingNeverUsesUpdatedAtAsCaptureProof(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	kickoff := drawSourceTime(match.MatchTime)
	before, after := kickoff.Add(-time.Hour), kickoff.Add(time.Hour)
	odds.UpdatedAt = before
	row := drawFeatureStudyBuildRow(match, history, odds, pankou)
	if row.Timing.EuroPreKickoff != nil || row.Timing.LinePreKickoff != nil || row.Timing.SportteryCaptureKnown {
		t.Fatal("missing or whole-row update time became separate pre-match quote proof")
	}
	odds.CapturedAt, pankou.CapturedAt = &before, &after
	row = drawFeatureStudyBuildRow(match, history, odds, pankou)
	if row.Timing.EuroPreKickoff == nil || !*row.Timing.EuroPreKickoff || row.Timing.LinePreKickoff == nil || *row.Timing.LinePreKickoff {
		t.Fatal("actual capture before/after kickoff classified incorrectly")
	}
}
