package handlers

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"

	"go_server/models"
	"gorm.io/datatypes"
)

func TestDrawObservationsPredictionsRequireTheirOwnInputs(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	odds.SportteryTrade = nil
	// This is the previously misleading case: shared H5 logic produces a
	// direction using average odds despite the missing official source.
	old := buildAnalysisFromData(match, history, odds, pankou, sportteryTradeFromJSON(odds.SportteryTrade))
	if len(old.TiCaiResult) == 0 {
		t.Fatal("fixture no longer exercises the legacy substitution")
	}
	got := buildDrawObservationFrontend(match, history, odds, pankou).analysis
	if len(got.TiCaiResult) != 0 || got.PredictionAvailability.Ticai.Available || !strings.Contains(got.PredictionAvailability.Ticai.Reason, "缺少竞彩") {
		t.Fatalf("missing sporttery still produced a named prediction: %+v", got)
	}
	if len(got.KaiLiResult) != 1 || !got.PredictionAvailability.Kelly.Available {
		t.Fatal("Kelly incorrectly requires sporttery even though its own inputs are complete")
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"ticairesult":[]`) {
		t.Fatal("unavailable prediction must be an explicit empty result")
	}
}

func TestDrawObservationsPredictionValuesMatchActualSourceAndDecision(t *testing.T) {
	_, _, odds, _ := drawObservationDisplayFixture()
	// A valid ID 16 source takes precedence over Bet365, exactly as the prediction.
	odds.Data = datatypes.JSON(`{"odds":[{"companyId":"16","companyName":"10B**","odds":["2.2","3.0","3.6"]},{"companyId":"281","odds":["9","9","9"]},{"companyId":"115","odds":["2.25","3.50","2.88"]}]}`)
	odds.AvgOdds = datatypes.JSON(`{"odds":["2","3","4"]}`)
	odds.SportteryTrade = datatypes.JSON(`{"data":{"tzbl":{"h":2.03,"d":3.4,"a":2.92}}}`)
	got := drawObservationPredictions(odds)
	// Average return = 12/13. Kelly = company/average * return.
	for i, want := range []float64{1.1 * 12 / 13, 12.0 / 13, .9 * 12 / 13} {
		if len(got.Availability.Kelly.Values) != 3 || math.Abs(got.Availability.Kelly.Values[i]-want) > 1e-12 {
			t.Fatalf("Kelly values use a different source or formula: %+v", got.Availability.Kelly)
		}
	}
	for i, want := range []float64{.22, .10, -.04} {
		if len(got.Availability.Ticai.Values) != 3 || math.Abs(got.Availability.Ticai.Values[i]-want) > 1e-12 {
			t.Fatalf("sporttery display must keep the signed difference: %+v", got.Availability.Ticai)
		}
	}
	if !reflect.DeepEqual(got.Kelly, []string{"负"}) || !reflect.DeepEqual(got.Ticai, []string{"负"}) {
		t.Fatal("displaying values changed the chosen directions")
	}
	missing := drawObservationPredictions(models.OddsMoney{})
	if missing.Availability.Kelly.Values != nil || missing.Availability.Ticai.Values != nil {
		t.Fatal("missing inputs exposed zero or substituted calculation values")
	}
}

func TestDrawObservationsTicaiNeverBorrowsAnotherMarketOrCompany(t *testing.T) {
	_, _, base, _ := drawObservationDisplayFixture()
	for _, test := range []struct {
		name        string
		data, trade string
		want        string
	}{
		{"no William", `{"odds":[{"companyId":"281","odds":["2","3","4"]}]}`, string(base.SportteryTrade), "威廉"},
		{"zero official", string(base.Data), `{"data":{"tzbl":{"h":0,"d":0,"a":0}}}`, "竞彩"},
		{"partial official", string(base.Data), `{"data":{"tzbl":{"h":2,"d":3}}}`, "竞彩"},
		{"zero William", `{"odds":[{"companyId":"115","odds":["2","0","4"]}]}`, string(base.SportteryTrade), "威廉"},
		{"wrong provider name", `{"odds":[{"companyId":"281","companyName":"威廉","odds":["2","3","4"]}]}`, string(base.SportteryTrade), "威廉"},
		{"unopened capture overrides lr", string(base.Data), `{"data":{"tzbl":{"h":2,"d":3,"a":4}},"sportteryOdds":{"code":0,"data":{"spf":{"odds":[{"spf3":0,"spf1":0,"spf0":0}]}}}}`, "竞彩"},
	} {
		t.Run(test.name, func(t *testing.T) {
			odds := base
			odds.Data = datatypes.JSON(test.data)
			odds.SportteryTrade = datatypes.JSON(test.trade)
			got := drawObservationPredictions(odds)
			if len(got.Ticai) != 0 || got.Availability.Ticai.Available || !strings.Contains(got.Availability.Ticai.Reason, test.want) {
				t.Fatalf("unavailable inputs became a prediction: %+v", got)
			}
		})
	}
}

func TestDrawObservationsPredictionsUseActualPricesAndRejectGenericFallback(t *testing.T) {
	_, _, odds, _ := drawObservationDisplayFixture()
	odds.Data = datatypes.JSON(`{"odds":[{"companyId":"115","odds":["2.25","3.50","2.88"]},{"companyId":"281","odds":["2.35","3.50","2.88"]},{"companyId":"16","odds":["0","0","0"]}]}`)
	odds.SportteryTrade = datatypes.JSON(`{"data":{"tzbl":{"h":2.03,"d":3.4,"a":2.92}}}`)
	got := drawObservationPredictions(odds)
	if !reflect.DeepEqual(got.Ticai, []string{"负"}) || !got.Availability.Ticai.Available {
		t.Fatalf("expected smallest absolute difference 0.04: %+v", got)
	}
	if !got.Availability.Kelly.Available || !strings.Contains(got.KellyBasis, "Bet365") {
		t.Fatal("valid alternative company could not replace an unopened Kelly source")
	}
	odds.AvgOdds = datatypes.JSON(`{"odds":["0","3.1","3.5"]}`)
	got = drawObservationPredictions(odds)
	if got.Availability.Kelly.Available || len(got.Kelly) != 0 || !got.Availability.Ticai.Available {
		t.Fatal("invalid average prices produced Kelly, or unnecessarily suppressed independent ticai")
	}
	got = drawObservationPredictions(models.OddsMoney{})
	if len(got.Kelly) != 0 || len(got.Ticai) != 0 || got.Availability.Kelly.Available || got.Availability.Ticai.Available {
		t.Fatal("no data produced the generic win/draw/loss fallback")
	}
}
