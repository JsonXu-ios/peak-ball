package handlers

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"go_server/models"
)

func publicModelFixture() PublicSourceModelInput {
	var input PublicSourceModelInput
	input.Match.ID = "sporttery:2026-11-14:7"
	input.Match.Date = "2026-11-14"
	input.Match.Home = "主队"
	input.Match.Away = "客队"
	input.Match.Kickoff = "2026-11-14 18:15:00"
	input.Match.Lottery = "周六003"
	input.Provenance = map[string]struct {
		Received string `json:"received_at"`
		ID       int64  `json:"id"`
	}{}
	for i, kind := range []string{"euro", "asian", "totals", "history", "bonus"} {
		input.Provenance[kind] = struct {
			Received string `json:"received_at"`
			ID       int64  `json:"id"`
		}{"2026-11-14T09:00:00Z", int64(i + 1)}
	}
	input.Parts.Euro = []publicSourceQuote{{Label: "平均欧赔", Opening: []string{"2.8", "3.2", "2.8"}, Latest: []string{"2.7", "3.0", "2.9"}}, {Label: "威*", Opening: []string{"2.8", "3.2", "2.8"}, Latest: []string{"2.7", "3.0", "2.9"}}, {Label: "36*", Opening: []string{"2.8", "3.2", "2.8"}, Latest: []string{"2.7", "3.0", "2.9"}}}
	input.Parts.Asian = []publicSourceQuote{{Label: "36*", Opening: []string{"0.9", "受半/一", "0.9"}, Latest: []string{"0.9", "受半球", "0.9"}}}
	input.Parts.Totals = []publicSourceQuote{{Label: "36*", Opening: []string{"0.9", "2.5/3球", "0.9"}, Latest: []string{"0.9", "3球", "0.9"}}, {Label: "威*", Opening: []string{"0.9", "2.5球", "0.9"}, Latest: []string{"0.9", "3球", "0.9"}}}
	input.Parts.Bonus.History = map[string][]map[string]interface{}{"hadList": {{"h": "2.5", "d": "2.9", "a": "2.5", "updateDate": "2026-11-14", "updateTime": "09:00:00"}}}
	for i := 0; i < 8; i++ {
		v := float64(5 + i)
		input.Parts.Bonus.TotalGoals = append(input.Parts.Bonus.TotalGoals, &v)
	}
	input.Parts.Bonus.DrawScores = map[string]*float64{}
	for _, s := range []string{"0:0", "1:1", "2:2", "3:3"} {
		v := 10.0
		if s == "1:1" {
			v = 6
		}
		input.Parts.Bonus.DrawScores[s] = &v
	}
	tables := [][][]string{{}, {}, {}}
	for i := 0; i < 6; i++ {
		day := time.Date(2026, 10, 30-i, 0, 0, 0, 0, drawSourceLocation).Format("06-1-2")
		tables[0] = append(tables[0], []string{"联赛", "", day, "主队", "1:1", "对手", "0:0"})
		tables[1] = append(tables[1], []string{"联赛", "", day, "对手", "1:1", "客队", "0:0"})
	}
	tables[2] = [][]string{{"联赛", "", "26-9-1", "主队", "1:1", "客队", "0:0"}}
	input.Parts.History.Tables = tables
	return input
}

func TestPublicSourceModelMatchesOriginalPurePipeline(t *testing.T) {
	input := publicModelFixture()
	got := AnalyzePublicSourceModel(input)
	if got.State != "ready" || got.Observation == nil {
		t.Fatalf("not ready: %+v", got)
	}
	row := *got.Observation
	match := models.Money{MatchId: input.Match.ID, Home: input.Match.Home, Guest: input.Match.Away, JingcaiID: input.Match.Lottery, MatchTime: row.MatchTime}
	history := publicSourceHistoryModel(match, row.HomeRecent, row.AwayRecent, row.HeadToHead)
	odds, lines := publicSourceMarkets(input, match)
	enrichment := buildDrawObservationFrontend(match, history, odds, lines)
	study := drawFeatureStudyBuildRowWithEnrichment(match, history, odds, lines, enrichment)
	want := drawObservationApplySelectionEligibility(row, drawObservationModelSignal(row, &study))
	if !reflect.DeepEqual(row.FrozenDrawSignal, want) {
		t.Fatalf("adapter changed original model: %+v / %+v", row.FrozenDrawSignal, want)
	}
	secondary := drawObservationSecondaryDecision(row)
	if !reflect.DeepEqual(row.SecondaryDecision, &secondary) {
		t.Fatal("secondary rule diverged")
	}
	scores := drawObservationScoreRecommendation(row)
	if !reflect.DeepEqual(row.ScoreRecommendation, &scores) {
		t.Fatal("score rule diverged")
	}
	if !reflect.DeepEqual(row.TotalsPrediction, drawObservationTotalsSignal(row, enrichment.totalsInputs)) {
		t.Fatal("totals model diverged")
	}
	if row.HomeRecent.Sample != 6 || row.AwayRecent.Sample != 6 || row.HeadToHead.Sample != 1 {
		t.Fatal("history not projected")
	}
	if row.Settled || row.RegulationSettled == nil || *row.RegulationSettled || row.HomeScore != -1 {
		t.Fatal("unconfirmed results were fabricated")
	}
}

func TestPublicSourceHistoryExcludesTargetSameDayFutureAndDuplicates(t *testing.T) {
	kickoff := time.Date(2026, 11, 14, 18, 15, 0, 0, drawSourceLocation)
	rows := [][]string{{"联赛", "", "26-11-14", "主队", "1:1", "客队", "0:0"}, {"联赛", "", "26-11-13", "主队", "VS", "客队", ""}, {"联赛", "", "26-11-15", "主队", "2:2", "客队", "0:0"}, {"联赛", "", "26-11-12", "主队", "0:1", "客队", "0:0"}, {"联赛", "", "26-11-12", "主队", "0:1", "客队", "0:0"}, {"联赛", "", "26-11-10", "主队", "1:1", "其他队", "0:0"}}
	got := publicSourceHistory(rows, "主队", "客队", kickoff)
	if got.Sample != 1 || got.Matches[0].Result != "loss" {
		t.Fatalf("unsafe history accepted: %+v", got)
	}
}

func TestPublicSourceModelRejectsUnknownOrPostKickoffCapture(t *testing.T) {
	for _, at := range []string{"", "2026-11-14T10:15:00Z", "2026-11-14T11:00:00Z"} {
		input := publicModelFixture()
		p := input.Provenance["euro"]
		p.Received = at
		input.Provenance["euro"] = p
		if got := AnalyzePublicSourceModel(input); got.State != "unavailable" || got.Observation != nil {
			t.Fatal("post-match snapshot accepted")
		}
	}
}

func TestPublicSourceModelRequiresRealHistoryForSelection(t *testing.T) {
	input := publicModelFixture()
	input.Parts.History.Tables[2] = nil
	got := AnalyzePublicSourceModel(input)
	if got.Observation.FrozenDrawSignal.State == "selected" || got.Observation.FrozenDrawSignal.HeadToHeadSample != 0 {
		t.Fatal("missing H2H selected")
	}
	input = publicModelFixture()
	input.Parts.Totals = input.Parts.Totals[:1]
	got = AnalyzePublicSourceModel(input)
	if got.Observation.TotalsPrediction.State != "unavailable" || !strings.Contains(got.Observation.TotalsPrediction.Reason, "威廉") {
		t.Fatal("missing William totals silently substituted")
	}
}

func TestPublicSourceLinesAndExplicitCompanyMapping(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		goals bool
		want  float64
	}{{"受半/一", false, -.75}, {"受半球", false, -.5}, {"受一球", false, -1}, {"2.5/3球", true, 2.75}, {"3球", true, 3}} {
		if got := parsePankouLine(publicSourceLine(tt.raw, tt.goals)); got != tt.want {
			t.Fatalf("line %q got %v", tt.raw, got)
		}
	}
	if publicSourceCompany(publicSourceQuote{Label: "36*", Company: "other-company"}) != "" {
		t.Fatal("manual company override ignored")
	}
	input := publicModelFixture()
	input.Parts.Euro = append(input.Parts.Euro, input.Parts.Euro[1])
	kickoff, _ := time.ParseInLocation("2006-01-02 15:04:05", input.Match.Kickoff, drawSourceLocation)
	odds, _ := publicSourceMarkets(input, models.Money{MatchTime: kickoff})
	if len(odds.William) != 0 {
		t.Fatal("ambiguous duplicate William accepted")
	}
}

func TestPublicSourceIgnoresInjectedCurrentResults(t *testing.T) {
	input := publicModelFixture()
	before := AnalyzePublicSourceModel(input)
	raw, _ := json.Marshal(input)
	var payload map[string]interface{}
	_ = json.Unmarshal(raw, &payload)
	payload["match"].(map[string]interface{})["homeScore"] = 99
	payload["match"].(map[string]interface{})["awayScore"] = 1
	payload["match"].(map[string]interface{})["settled"] = true
	raw, _ = json.Marshal(payload)
	_ = json.Unmarshal(raw, &input)
	after := AnalyzePublicSourceModel(input)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("target results changed recommendation")
	}
}
