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

func TestDrawObservationsStagesKeepOpeningAndLatestIndependent(t *testing.T) {
	odds := models.OddsMoney{
		Data:           datatypes.JSON(`{"odds":[{"companyId":"","companyName":"平均欧赔","firstOdds":["2","3","4"],"odds":["2","3","4"]},{"companyId":"16","companyName":"10B**","firstOdds":["2.4","3","3.2"],"odds":["1.8","3.3","4.4"]},{"companyId":"115","firstOdds":["2.1","3.2","3.8"],"odds":["2.25","3.50","2.88"]}]}`),
		SportteryTrade: datatypes.JSON(`{"sportteryOdds":{"code":0,"data":{"spf":{"odds":[{"spf3":0,"spf1":0,"spf0":0},{"spf3":2,"spf1":3.4,"spf0":4.1,"updateTime":"10-20 10:00"},{"spf3":2.1,"spf1":3.2,"spf0":3.7,"updateTime":"10-20 11:00"},{"spf3":2.03,"spf1":3.4,"spf0":2.92,"updateTime":"10-20 12:00"}]}}}}`),
	}
	beforeData, beforeTrade := string(odds.Data), string(odds.SportteryTrade)
	latest := drawObservationPredictions(odds)
	stages := drawObservationComparisonStages(odds, latest)
	if len(stages) != 3 || stages[0].Key != "opening" || stages[1].Key != "middle" || stages[2].Key != "latest" {
		t.Fatal("all stages must be present in chronological order")
	}
	first, mid, last := stages[0], stages[1], stages[2]
	if !reflect.DeepEqual(first.Average, []float64{2, 3, 4}) || !reflect.DeepEqual(first.Kelly.Result, []string{"负"}) || !reflect.DeepEqual(last.Kelly.Result, []string{"胜"}) {
		t.Fatalf("opening prices were replaced by current prices: first=%+v last=%+v", first, last)
	}
	for i, want := range []float64{1.2 * 12 / 13, 12.0 / 13, .8 * 12 / 13} {
		if math.Abs(first.Kelly.Values[i]-want) > 1e-12 {
			t.Fatalf("wrong opening Kelly: %v", first.Kelly.Values)
		}
	}
	for i, want := range []float64{.1, -.2, -.3} {
		if math.Abs(first.Ticai.Values[i]-want) > 1e-12 {
			t.Fatalf("mixed opening and latest sporttery: %v", first.Ticai.Values)
		}
	}
	for i, want := range []float64{.1, .2, -.2} {
		if math.Abs(first.WilliamAverage.Values[i]-want) > 1e-12 {
			t.Fatal("opening mean is not from the opening average row")
		}
	}
	if !reflect.DeepEqual(last.Kelly.drawObservationPredictionState, latest.Availability.Kelly) || !reflect.DeepEqual(last.Ticai.drawObservationPredictionState, latest.Availability.Ticai) || !reflect.DeepEqual(last.Ticai.Result, latest.Ticai) {
		t.Fatal("the new stages changed the existing latest calculation")
	}
	if mid.MiddleSample == nil || mid.MiddleSample.Index != 2 || mid.MiddleSample.Total != 3 || !reflect.DeepEqual(mid.Sporttery, []float64{2.1, 3.2, 3.7}) {
		t.Fatalf("middle sample not taken from the actual valid history: %+v", mid.MiddleSample)
	}
	for _, metric := range []drawObservationComparisonMetric{mid.Kelly, mid.Ticai, mid.WilliamAverage} {
		if metric.Available || metric.Values != nil || len(metric.Result) != 0 || !strings.Contains(metric.Reason, "中间") {
			t.Fatal("missing real company history was interpolated or borrowed")
		}
	}
	if beforeData != string(odds.Data) || beforeTrade != string(odds.SportteryTrade) {
		t.Fatal("comparison mutated source captures")
	}
}

func TestDrawObservationsMiddleUsesRealRecordsAndRequiresInteriorSample(t *testing.T) {
	for _, test := range []struct{ count, index int }{{0, 0}, {1, 0}, {2, 0}, {3, 2}, {4, 2}, {5, 3}, {6, 3}} {
		rows := []map[string]interface{}{{"spf3": 0, "spf1": 0, "spf0": 0}, {"spf3": 2, "spf1": 3}}
		for i := 0; i < test.count; i++ {
			rows = append(rows, map[string]interface{}{"spf3": 2 + float64(i), "spf1": 3, "spf0": 4, "updateTime": "10-21 12:00"})
		}
		body, _ := json.Marshal(map[string]interface{}{"code": 0, "data": map[string]interface{}{"spf": map[string]interface{}{"odds": rows}}})
		got := drawObservationSportteryMiddle(body)
		if test.index == 0 {
			if got != nil {
				t.Fatalf("%d records incorrectly yielded a middle sample", test.count)
			}
			continue
		}
		if got == nil || got.Index != test.index || got.Total != test.count || got.Prices[0] != float64(test.index+1) {
			t.Fatalf("count=%d wrong middle: %+v", test.count, got)
		}
	}
	if drawObservationSportteryMiddle(json.RawMessage(`{"code":1,"data":{"spf":{"odds":[]}}}`)) != nil {
		t.Fatal("failed capture supplied a middle sample")
	}
}

func TestDrawObservationsOpeningNeverUsesLatestOrGenericFallback(t *testing.T) {
	_, _, odds, _ := drawObservationDisplayFixture()
	stages := drawObservationComparisonStages(odds, drawObservationPredictions(odds))
	if stages[0].Ticai.Available || stages[0].Ticai.Values != nil {
		t.Fatal("lr latest prices became official opening prices")
	}
	if stages[0].Kelly.Available {
		t.Fatal("zero/partial opening prices became valid Kelly data")
	}
	missing := drawObservationComparisonStages(models.OddsMoney{}, drawObservationPredictions(models.OddsMoney{}))
	for _, stage := range missing {
		for _, metric := range []drawObservationComparisonMetric{stage.Kelly, stage.Ticai, stage.WilliamAverage} {
			if metric.Available || metric.Values != nil || len(metric.Result) > 0 {
				t.Fatal("missing records fabricated a stage comparison")
			}
		}
	}
	rows, _ := drawObservationEuroRows(datatypes.JSON(`{"odds":[{"companyId":"115","firstOdds":["2","3","4"]},{"companyId":"281","firstOdds":["4","5","6"]}]}`))
	got := drawObservationOpeningAverage(models.OddsMoney{}, rows)
	if !reflect.DeepEqual(got, []float64{3, 4, 5}) {
		t.Fatalf("wrong all-company opening mean: %v", got)
	}
	got = drawObservationOpeningAverage(models.OddsMoney{AvgOdds: datatypes.JSON(`{"firstOdds":["1.5","3.5","5.5"],"odds":["9","9","9"]}`)}, rows)
	if !reflect.DeepEqual(got, []float64{1.5, 3.5, 5.5}) {
		t.Fatal("stored source opening average lost priority")
	}
}
