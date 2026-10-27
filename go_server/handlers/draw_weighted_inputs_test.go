package handlers

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"go_server/models"
	"gorm.io/datatypes"
)

func TestDrawWeightedInputsDistinguishRealZeroAndMissingOfficialRate(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	// The shared legacy response can contain default zero pointers here, but
	// the source JSON does not have hy/dy/ay and the new input must stay null.
	missing := buildDrawObservationFrontend(match, history, odds, pankou).analysis.WeightedFeatureInputs
	if missing == nil || missing.HomeHeat == nil {
		t.Fatal("complete stored handicap/average inputs should have strict heat")
	}
	for _, value := range missing.OfficialProfitRates {
		if value != nil {
			t.Fatal("missing raw source rate became an observed zero")
		}
	}
	encoded, err := json.Marshal(missing)
	if err != nil || !strings.Contains(string(encoded), `"officialProfitRates":[null,null,null]`) {
		t.Fatalf("missing rate array must be explicit nulls: %s %v", encoded, err)
	}
	odds.SportteryTrade = datatypes.JSON(`{"data":{"tzbl":{"h":2,"d":3,"a":4,"hsupportRate":40,"dsupportRate":30,"asupportRate":30},"jyykSpf":{"hy":0,"dy":-12,"ay":null}}}`)
	observed := buildDrawObservationFrontend(match, history, odds, pankou).analysis.WeightedFeatureInputs
	if observed.OfficialProfitRates[0] == nil || *observed.OfficialProfitRates[0] != 0 || observed.OfficialProfitRates[1] == nil || *observed.OfficialProfitRates[1] != -12 || observed.OfficialProfitRates[2] != nil {
		t.Fatalf("real zero and null were confused: %+v", observed.OfficialProfitRates)
	}
	encoded, err = json.Marshal(observed)
	if err != nil || !strings.Contains(string(encoded), `"officialProfitRates":[0,-12,null]`) {
		t.Fatalf("source observations must preserve numeric zero: %s %v", encoded, err)
	}
}

func TestDrawWeightedInputsHeatRequiresActualOpeningAndLatestHandicap(t *testing.T) {
	match, history, odds, _ := drawObservationDisplayFixture()
	for _, example := range []struct {
		Name   string
		Pankou models.PankouMoney
	}{
		{"no-handicap", models.PankouMoney{}},
		{"opening-only", models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":8,"companyName":"Bet365","firstPankou":"0.5","firstOdds":["0.9","0.9"]}]}`)}},
		{"latest-only", models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":8,"companyName":"Bet365","pankou":"0.5","odds":["0.9","0.9"]}]}`)}},
	} {
		t.Run(example.Name, func(t *testing.T) {
			got := buildDrawObservationFrontend(match, history, odds, example.Pankou).analysis.WeightedFeatureInputs
			if got == nil || got.HomeHeat != nil {
				t.Fatal("missing line was silently replaced by a default or another phase")
			}
			encoded, err := json.Marshal(got)
			if err != nil || !strings.Contains(string(encoded), `"homeHeat":null`) {
				t.Fatalf("missing heat must be explicit null: %s %v", encoded, err)
			}
		})
	}
	// An actual zero handicap is an observation, not the frontend's default.
	zero := models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":8,"companyName":"Bet365","firstPankou":0,"pankou":0,"firstOdds":["0.9","0.9"],"odds":["0.9","0.9"]}]}`)}
	got := buildDrawObservationFrontend(match, history, odds, zero).analysis.WeightedFeatureInputs
	if got.HomeHeat == nil || math.IsNaN(*got.HomeHeat) {
		t.Fatal("real zero handicap must retain valid heat")
	}
}

func TestDrawWeightedInputsExcludeUnpricedAndSimulatedMarkets(t *testing.T) {
	_, _, odds, _ := drawObservationDisplayFixture()
	odds.SportteryTrade = datatypes.JSON(`{"data":{"jyykSpf":{"hy":0,"dy":-12,"ay":3}}}`)
	zero, negative, positive := 0.0, -12.0, 3.0
	market := bookmakerMarketResponse{Key: "sporttery", OddsAvailable: true, BookmakerByOutcome: []bookmakerOutcomeResponse{
		{Outcome: "home", Odds: 2, Available: true, OfficialProfitRate: &zero},
		{Outcome: "draw", Odds: 3, Available: false, OfficialProfitRate: &negative},
		{Outcome: "away", Odds: 0, Available: true, OfficialProfitRate: &positive},
	}}
	got := drawObservationWeightedInputs(odds, models.PankouMoney{}, &matchRoiSimulationResponse{Markets: []bookmakerMarketResponse{market}})
	if got.OfficialProfitRates[0] == nil || got.OfficialProfitRates[1] == nil || *got.OfficialProfitRates[1] != -12 || got.OfficialProfitRates[2] != nil {
		t.Fatal("priced source rates are independent of local amount availability; unpriced rate stays missing")
	}
	market.Simulated = true
	got = drawObservationWeightedInputs(odds, models.PankouMoney{}, &matchRoiSimulationResponse{Markets: []bookmakerMarketResponse{market}})
	for _, value := range got.OfficialProfitRates {
		if value != nil {
			t.Fatal("simulated market was accepted as official rate")
		}
	}
}
