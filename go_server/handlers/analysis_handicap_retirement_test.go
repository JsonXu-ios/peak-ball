package handlers

import (
	"reflect"
	"testing"
)

func TestAnalysisMarketsKeepRealHandicapWithoutSimulatedReplacement(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	trade := sportteryTradeFromJSON(odds.SportteryTrade)
	for _, tc := range []struct {
		name                 string
		trade                sportteryTradeData
		wantHandicap         bool
		wantResultSimulation string
	}{
		{"real handicap", trade, true, "sportterySim"},
		{"no official quotes", sportteryTradeData{}, false, "sporttery"},
		{"handicap prices without support", sportteryTradeData{JyykRqspf: sportteryProfitRqspf{Goal: "+1", H: 1.52, D: 3.77, A: 4.71}}, true, "sporttery"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := buildAnalysisFromData(match, history, odds, pankou, tc.trade)
			attachAnalysisWeights(&response)
			var handicap *bookmakerMarketResponse
			foundSimulation := false
			for i := range response.RoiSimulation.Markets {
				market := &response.RoiSimulation.Markets[i]
				if market.Key == "sportteryRqspfSim" || market.Key == "sportteryRqspf" && market.Simulated {
					t.Fatalf("retired simulated handicap returned: %+v", market)
				}
				if market.Key == "sportteryRqspf" {
					handicap = market
				}
				if market.Key == tc.wantResultSimulation && market.Simulated {
					foundSimulation = true
				}
			}
			if !foundSimulation {
				t.Fatal("unrelated result simulation was removed")
			}
			want, ok := buildSportteryRqspfMarket(tc.trade)
			if ok != tc.wantHandicap || (handicap != nil) != tc.wantHandicap {
				t.Fatal("real handicap presence changed")
			}
			if handicap != nil && !reflect.DeepEqual(*handicap, want) {
				t.Fatal("real handicap prices, support or profit changed")
			}
		})
	}
}

func TestDrawStoredDayOmitsRetiredMarketsWithoutChangingCachedSignals(t *testing.T) {
	_, _, odds, _ := drawObservationDisplayFixture()
	real, ok := buildSportteryRqspfMarket(sportteryTradeFromJSON(odds.SportteryTrade))
	if !ok {
		t.Fatal("fixture lacks real handicap")
	}
	resultSimulation := bookmakerMarketResponse{Key: "sportterySim", Simulated: true}
	legacy := []bookmakerMarketResponse{
		{Key: "sportteryRqspfSim"}, // An older payload can lack its simulated flag.
		{Key: "sportteryRqspf", Simulated: true},
		real,
		resultSimulation,
	}
	analysis := &drawObservationFrontendAnalysis{RoiSimulation: &matchRoiSimulationResponse{TotalStake: 50000000, Markets: legacy}}
	score := 0.3
	signal := &drawObservationFrozenSignal{State: "selected", Score: &score}
	saved := drawObservationStoredDay{Date: "2026-11-21", Items: []drawObservationItem{{MatchID: "example", JingcaiID: "001", FrozenDrawSignal: signal, FrontendAnalysis: analysis}}}
	response := saved.filter(drawObservationQuery{scope: "all", mode: "today", size: 50, page: 1})
	if len(response.Items) != 1 {
		t.Fatal("fixture was removed")
	}
	got := response.Items[0]
	if !reflect.DeepEqual(got.FrontendAnalysis.RoiSimulation.Markets, []bookmakerMarketResponse{real, resultSimulation}) {
		t.Fatal("retired market survived or real/result market was removed")
	}
	if got.FrozenDrawSignal != signal || saved.Items[0].FrontendAnalysis != analysis || len(analysis.RoiSimulation.Markets) != 4 || analysis.RoiSimulation.Markets[0].Key != "sportteryRqspfSim" {
		t.Fatal("projection changed the shared cache or prediction")
	}
	if drawObservationFrontendWithoutSimulatedHandicap(got.FrontendAnalysis) != got.FrontendAnalysis {
		t.Fatal("projection is not idempotent")
	}
	if drawObservationFrontendWithoutSimulatedHandicap(nil) != nil {
		t.Fatal("nil input changed")
	}
}

func TestPlatformDoesNotUseLegacySimulatedHandicap(t *testing.T) {
	real := bookmakerMarketResponse{Key: "sportteryRqspf", Goal: "-1"}
	response := analysisMatchResponse{RoiSimulation: &matchRoiSimulationResponse{Markets: []bookmakerMarketResponse{
		{Key: "sportteryRqspfSim"},
		{Key: "sportteryRqspf", Simulated: true},
		real,
		{Key: "sportterySim", Simulated: true},
	}}}
	if pfMarketByKey(&response, "sportteryRqspfSim") != nil {
		t.Fatal("retired comparison used in platform decisions")
	}
	if got := pfMarketByKey(&response, "sportteryRqspf"); got == nil || !reflect.DeepEqual(*got, real) {
		t.Fatal("legacy fallback hid the real handicap")
	}
	if pfMarketByKey(&response, "sportterySim") == nil {
		t.Fatal("result simulation was removed")
	}
}
