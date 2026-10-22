package handlers

import (
	"encoding/json"
	"go_server/models"
	"gorm.io/datatypes"
	"reflect"
	"testing"
)

func TestSportteryRecordedOpeningLatestAndOutcomeOrder(t *testing.T) {
	// spf3=home, spf1=draw, spf0=away. frist/last are home-only
	// summaries; rqspf is a separate market and must never fill 1X2.
	record := json.RawMessage(`{"code":0,"data":{"matchId":3073080,"spf":{"frist":99,"last":98,"odds":[{"spf0":3.14,"spf1":3.55,"spf3":1.89},{"spf0":2.92,"spf1":3.4,"spf3":2.03}]},"rqspf":{"odds":[{"rq0":1.6,"rq1":3.75,"rq3":4.2}]}}}`)
	wrapped, _ := json.Marshal(map[string]json.RawMessage{"sportteryOdds": record, "data": json.RawMessage(`{"tzbl":{"h":9,"d":8,"a":7}}`)})
	quote := drawObservationCompanyEuro(models.OddsMoney{SportteryTrade: datatypes.JSON(wrapped)}, "sporttery", "竞彩")
	if quote.Source != "odds_moneys" || *quote.Opening[0] != "1.89" || *quote.Opening[1] != "3.55" || *quote.Opening[2] != "3.14" || *quote.Latest[0] != "2.03" || *quote.Latest[1] != "3.4" || *quote.Latest[2] != "2.92" {
		t.Fatalf("wrong record/phase/outcome, or lr won over official records: %+v", quote)
	}
	if got := sportteryOdds(sportteryTradeFromJSON(datatypes.JSON(wrapped))); !reflect.DeepEqual(got, []float64{2.03, 3.4, 2.92}) {
		t.Fatalf("analysis and raw company quotes use different latest prices: %v", got)
	}
	// Attaching prices must not make the nested decoder swallow a legacy direct
	// lr object and drop its real support rates / profit fields.
	direct, _ := json.Marshal(map[string]json.RawMessage{"sportteryOdds": record, "tzbl": json.RawMessage(`{"h":2.03,"d":3.4,"a":2.92,"hsupportRate":52,"dsupportRate":24,"asupportRate":24}`)})
	if trade := sportteryTradeFromJSON(datatypes.JSON(direct)); trade.Tzbl.HSupportRate != 52 || trade.Tzbl.DSupportRate != 24 || trade.Tzbl.ASupportRate != 24 {
		t.Fatalf("new official prices hid legacy direct lr data: %+v", trade)
	}
	for _, bad := range []string{`{"code":1,"data":{"spf":{"odds":[{"spf3":1.5,"spf1":3,"spf0":4}]}}}`, `{"data":{"rqspf":{"odds":[{"rq3":2,"rq1":3,"rq0":4}]}}}`, `{}`} {
		if _, _, present := sportteryRecordedPrices(json.RawMessage(bad)); present {
			t.Fatalf("non-SPF or failed response became actual odds: %s", bad)
		}
	}
}

func TestSportteryRecordedSingleRowAndUnopenedLatest(t *testing.T) {
	wrapped := datatypes.JSON(`{"sportteryOdds":{"code":0,"data":{"spf":{"odds":[{"spf3":"1.90","spf1":"3.50","spf0":"3.10"}]}}}}`)
	quote := drawObservationCompanyEuro(models.OddsMoney{SportteryTrade: wrapped}, "sporttery", "竞彩")
	if *quote.Opening[0] != "1.90" || *quote.Latest[0] != "1.90" || quote.Source != "odds_moneys" {
		t.Fatal("one actual opening record is also the latest record")
	}
	wrapped = datatypes.JSON(`{"sportteryOdds":{"code":0,"data":{"spf":{"odds":[{"spf3":1.9,"spf1":3.5,"spf0":3.1},{"spf3":0,"spf1":0,"spf0":0}]}}},"data":{"tzbl":{"h":2,"d":3,"a":4}}}`)
	quote = drawObservationCompanyEuro(models.OddsMoney{SportteryTrade: wrapped}, "sporttery", "竞彩")
	if *quote.Opening[0] != "1.9" || *quote.Latest[0] != "0" || quote.Source != "missing" || sportteryOdds(sportteryTradeFromJSON(wrapped)) != nil {
		t.Fatal("unopened latest record borrowed an older lr quote")
	}
}
