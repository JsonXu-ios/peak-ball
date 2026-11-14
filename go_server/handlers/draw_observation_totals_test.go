package handlers

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"go_server/internal/jcgoalsmodel"
	"go_server/models"
	"gorm.io/datatypes"
)

type drawTotalsReference struct {
	FeatureNames []string `json:"featureNames"`
	Rows         []struct {
		MatchID          string              `json:"matchId"`
		Item             drawObservationItem `json:"item"`
		SportteryTrade   json.RawMessage     `json:"sportteryTrade"`
		ExpectedFeatures []*float64          `json:"expectedFeatures"`
		Eligible         bool                `json:"eligible"`
		Direction        string              `json:"expectedDirection"`
		Distribution     []float64           `json:"expectedDistribution"`
		Confidence       float64             `json:"expectedConfidence"`
		EV               float64             `json:"expectedEV"`
	} `json:"rows"`
}

func drawTotalsReadReference(t *testing.T) drawTotalsReference {
	t.Helper()
	body, err := os.ReadFile("testdata/jc_goals_inputs.json")
	if err != nil {
		t.Fatal(err)
	}
	var data drawTotalsReference
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

// Independent raw-quote/history fixtures from the Python research pipeline.
// Test all JC matches, not a sample: 59 missing-data rows and 634 predictions.
func TestDrawTotalsInputsPython693Reference(t *testing.T) {
	data := drawTotalsReadReference(t)
	if len(data.Rows) != 693 || len(data.FeatureNames) != 50 {
		t.Fatal("incomplete source reference")
	}
	model, err := jcgoalsmodel.Load()
	if err != nil {
		t.Fatal(err)
	}
	eligible, directions := 0, map[string]int{}
	for _, row := range data.Rows {
		inputs := drawObservationBuildTotalsInputs(row.MatchID, models.OddsMoney{MatchId: row.MatchID, SportteryTrade: datatypes.JSON(row.SportteryTrade)})
		input, reason := drawObservationTotalsModelInput(row.Item, inputs)
		if (reason == "") != row.Eligible {
			t.Fatalf("%s eligibility=%v expected=%v: %s", row.MatchID, reason == "", row.Eligible, reason)
		}
		signal := drawObservationTotalsSignal(row.Item, inputs)
		if !row.Eligible {
			if signal.State != "unavailable" || signal.Reason == "" {
				t.Fatalf("%s should explain missing data: %+v", row.MatchID, signal)
			}
			continue
		}
		eligible++
		for i, name := range data.FeatureNames {
			want := row.ExpectedFeatures[i]
			got, present := input.Features[name]
			if want == nil {
				if present {
					t.Fatalf("%s %s fabricated optional value %g", row.MatchID, name, got)
				}
			} else if !present || math.Abs(got-*want) > 1e-12 {
				t.Fatalf("%s %s got %.17g present=%v want %.17g", row.MatchID, name, got, present, *want)
			}
		}
		prediction, err := model.Predict(input)
		if err != nil {
			t.Fatalf("%s %v", row.MatchID, err)
		}
		if prediction.Direction != row.Direction || signal.State != "recommended" || signal.Direction != row.Direction {
			t.Fatalf("%s direction=%s signal=%+v want=%s", row.MatchID, prediction.Direction, signal, row.Direction)
		}
		for i, want := range row.Distribution {
			if math.Abs(prediction.TotalScores[i]-want) > 1e-11 {
				t.Fatalf("%s class%d probability=%.17g want=%.17g", row.MatchID, i, prediction.TotalScores[i], want)
			}
		}
		if math.Abs(prediction.Score-row.Confidence) > 1e-11 || math.Abs(prediction.ExpectedNetReturn-row.EV) > 1e-11 {
			t.Fatalf("%s score/EV mismatch", row.MatchID)
		}
		directions[signal.Direction]++
		// Current results and the original draw/secondary selections cannot alter
		// the new independent recommendation.
		item := row.Item
		item.HomeScore, item.AwayScore, item.Status, item.Settled = 99, 0, -1, false
		item.FrozenDrawSignal = &drawObservationFrozenSignal{State: "not_selected"}
		if changed := drawObservationTotalsSignal(item, inputs); !reflect.DeepEqual(changed, signal) {
			t.Fatalf("%s target results/old selection changed totals", row.MatchID)
		}
	}
	if eligible != 634 || directions["over"] != 344 || directions["under"] != 290 {
		t.Fatalf("unexpected full reference counts %d %v", eligible, directions)
	}
}

func TestDrawTotalsRejectsStoppedIncompleteAndForeignJQ(t *testing.T) {
	quotes := map[string]interface{}{"stop": false}
	for i := 0; i < 8; i++ {
		quotes["t"+strconv.Itoa(i)] = 4.0 + float64(i)
	}
	build := func(q map[string]interface{}, id string, code int) *drawTotalsInputs {
		body, _ := json.Marshal(map[string]interface{}{"sportteryOdds": map[string]interface{}{"code": code, "data": map[string]interface{}{"jq": q, "matchId": "provider-123"}}})
		return drawObservationBuildTotalsInputs("canonical", models.OddsMoney{MatchId: id, SportteryTrade: datatypes.JSON(body)})
	}
	if build(quotes, "canonical", 0) == nil {
		t.Fatal("provider ID must not be compared with canonical ID")
	}
	if build(quotes, "foreign", 0) != nil || build(quotes, "canonical", 1) != nil {
		t.Fatal("accepted wrong canonical identity/provider error")
	}
	for _, stop := range []interface{}{true, nil, "false"} {
		quotes["stop"] = stop
		if build(quotes, "canonical", 0) != nil {
			t.Fatalf("accepted stop=%v", stop)
		}
	}
	delete(quotes, "stop")
	if build(quotes, "canonical", 0) != nil {
		t.Fatal("accepted absent stop flag")
	}
	quotes["stop"] = false
	for _, price := range []interface{}{nil, 1.0, -2, "NaN", "Inf", "-"} {
		quotes["t7"] = price
		if build(quotes, "canonical", 0) != nil {
			t.Fatalf("accepted incomplete/invalid price %v", price)
		}
	}
}

func TestDrawTotalsSafeFormAndNoFabricatedMarketInputs(t *testing.T) {
	data := drawTotalsReadReference(t)
	var item drawObservationItem
	var inputs *drawTotalsInputs
	for _, row := range data.Rows {
		if row.Eligible {
			item = row.Item
			inputs = drawObservationBuildTotalsInputs(row.MatchID, models.OddsMoney{MatchId: row.MatchID, SportteryTrade: datatypes.JSON(row.SportteryTrade)})
			break
		}
	}
	valid := item.HomeRecent.Matches[:6]
	item.HomeRecent.Matches = append([]drawObservationRecentMatch(nil), valid...)
	input, reason := drawObservationTotalsModelInput(item, inputs)
	if reason != "" {
		t.Fatal(reason)
	}
	form := drawTotalsSafeForm(item, item.HomeRecent)
	gf, ga := 0.0, 0.0
	for _, r := range form {
		if r.Venue == "home" {
			gf += float64(r.HomeScore)
			ga += float64(r.AwayScore)
		} else {
			gf += float64(r.AwayScore)
			ga += float64(r.HomeScore)
		}
	}
	if input.Features["form.home.gf10"] != gf/6 || input.Features["form.home.ga10"] != ga/6 {
		t.Fatal("5..9 matches must use available matches for recent10")
	}
	unsafe := valid[0]
	item.HomeRecent.Matches = append([]drawObservationRecentMatch(nil), valid[:4]...)
	item.HomeRecent.Matches = append(item.HomeRecent.Matches, unsafe) // duplicate
	unsafe.MatchID = item.MatchID
	item.HomeRecent.Matches = append(item.HomeRecent.Matches, unsafe)
	unsafe.MatchID, unsafe.MatchTime = "boundary", item.MatchTime.Add(-2*time.Hour)
	item.HomeRecent.Matches = append(item.HomeRecent.Matches, unsafe)
	unsafe.MatchID, unsafe.MatchTime = "future", item.MatchTime.Add(time.Hour)
	item.HomeRecent.Matches = append(item.HomeRecent.Matches, unsafe)
	unsafe.MatchID, unsafe.MatchTime, unsafe.Venue = "bad-venue", item.MatchTime.Add(-72*time.Hour), "unknown"
	item.HomeRecent.Matches = append(item.HomeRecent.Matches, unsafe)
	if _, reason := drawObservationTotalsModelInput(item, inputs); reason == "" {
		t.Fatal("unsafe form must not satisfy the 5-match requirement")
	}
	item.HomeRecent.Matches = valid
	for i := range item.CompanyQuotes {
		if item.CompanyQuotes[i].Key == "william" {
			item.CompanyQuotes[i].GoalsLine.Source = "history_moneys"
		}
	}
	if _, reason := drawObservationTotalsModelInput(item, inputs); reason == "" {
		t.Fatal("historical/other-company line cannot replace William latest quote")
	}
	item.JingcaiID = ""
	if drawObservationTotalsSignal(item, inputs).State != "out_of_scope" {
		t.Fatal("non-JC fixtures out of scope")
	}
}
