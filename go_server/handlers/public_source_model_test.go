package handlers

import (
	"encoding/json"
	"math"
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

func publicSourceCPFixture() PublicSourceModelInput {
	input := publicModelFixture()
	input.Match.ZgzcwID = "4518110"
	b := &input.Parts.Bonus
	b.Source, b.SourceMatchID, b.CPMatchID, b.ZgzcwMatchID = "zgzcw_cp", "2042776", "2042776", input.Match.ZgzcwID
	b.Home, b.Away = input.Match.Home, input.Match.Away
	q := &input.Parts.Euro[0]
	q.Source, q.SourceCompanyID = "zgzcw_fenxi", "0"
	q.OpeningKelly = []string{"0.8", "0.9", "1.1"}
	q.OpeningKellyPrices = append([]string(nil), q.Opening...)
	q.CPExtras = &publicSourceCPQuote{Source: "zgzcw_cp", SourceCompanyID: "0", Label: "平均值", SourceMatchID: b.SourceMatchID, CPMatchID: b.CPMatchID, ZgzcwMatchID: b.ZgzcwMatchID, Opening: []string{"2.2", "3.3", "3.1"}, Latest: []string{"2.3", "3.2", "3.0"}, Kelly: []string{"0.92", "0.96", "0.95"}, Status: "price_mismatch", QuoteReceivedAt: "2026-11-14T08:55:00Z", KellyReceivedAt: "2026-11-14T08:56:00Z", ReceivedAt: "2026-11-14T08:56:00Z"}
	return input
}

func TestPublicSourceCPUsesPairedBundleWithoutMutatingSource(t *testing.T) {
	input := publicSourceCPFixture()
	before, _ := json.Marshal(input)
	got := AnalyzePublicSourceModel(input)
	if got.State != "ready" || len(got.InputSources) != 3 {
		t.Fatalf("not ready: %+v", got)
	}
	source := got.InputSources[0]
	cp := input.Parts.Euro[0].CPExtras
	if source.Source != "zgzcw_cp" || !reflect.DeepEqual(source.Opening, cp.Opening) || !reflect.DeepEqual(source.Latest, cp.Latest) || !reflect.DeepEqual(source.Kelly, cp.Kelly) || len(source.OpeningKelly) != 0 || !strings.Contains(source.Reason, "未带入") {
		t.Fatalf("not a paired bundle: %+v", source)
	}
	if !reflect.DeepEqual(got.Observation.FrontendAnalysis.AverageEuroOdds, []float64{2.3, 3.2, 3.0}) {
		t.Fatal("CP prices were not consumed by shared analysis")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("source quotation was overwritten")
	}
	input.Parts.Euro[0].OpeningKellyPrices = []string{"2.20", "3.30", "3.10"}
	matched := AnalyzePublicSourceModel(input)
	if !reflect.DeepEqual(matched.InputSources[0].OpeningKelly, input.Parts.Euro[0].OpeningKelly) {
		t.Fatal("verified opening Kelly was dropped")
	}
}

func TestPublicSourceCPRejectsUnsafeOrIncompleteBundles(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PublicSourceModelInput)
	}{
		{"wrong CP match", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.SourceMatchID = "other" }},
		{"wrong CP alias", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.CPMatchID = "other" }},
		{"wrong FENXI match", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.ZgzcwMatchID = "other" }},
		{"wrong quote match", func(x *PublicSourceModelInput) { x.Parts.Euro[0].SourceMatchID = "other" }},
		{"missing verified match", func(x *PublicSourceModelInput) { x.Match.ZgzcwID = "" }},
		{"wrong company ID", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.SourceCompanyID = "9" }},
		{"wrong company label", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.Label = "威*" }},
		{"ambiguous", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.Status = "ambiguous" }},
		{"missing Kelly", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.Kelly = nil }},
		{"invalid Kelly", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.Kelly = []string{"0.9", "NaN", "1"} }},
		{"invalid price", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.Latest = []string{"2", "3", "0"} }},
		{"missing timestamp", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.KellyReceivedAt = "" }},
		{"future timestamp", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.KellyReceivedAt = "2026-11-14T11:00:00Z" }},
		{"after snapshot", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.KellyReceivedAt = "2026-11-14T09:01:00Z" }},
		{"bundle before receipt", func(x *PublicSourceModelInput) { x.Parts.Euro[0].CPExtras.ReceivedAt = "2026-11-14T08:54:00Z" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			input := publicSourceCPFixture()
			tt.mutate(&input)
			got := AnalyzePublicSourceModel(input)
			if got.InputSources[0].Source != "zgzcw_fenxi" || !reflect.DeepEqual(got.InputSources[0].Latest, input.Parts.Euro[0].Latest) || !strings.Contains(got.InputSources[0].Reason, "保留原报价") || len(got.Warnings) == 0 {
				t.Fatalf("unsafe CP bundle accepted: %+v", got.InputSources[0])
			}
		})
	}
}

func TestPublicSourceCPKeepsCompleteOriginalAndSupportsLegacyCapture(t *testing.T) {
	input := publicSourceCPFixture()
	input.Parts.Euro[0].Kelly = []string{"0.8", "0.9", "1.1"}
	got := AnalyzePublicSourceModel(input)
	if got.InputSources[0].Source != "zgzcw_fenxi" || !reflect.DeepEqual(got.InputSources[0].Latest, input.Parts.Euro[0].Latest) || !reflect.DeepEqual(got.InputSources[0].OpeningKelly, input.Parts.Euro[0].OpeningKelly) {
		t.Fatal("complete source input was needlessly replaced by CP")
	}
	input = publicSourceCPFixture()
	cp := input.Parts.Euro[0].CPExtras
	cp.ReceivedAt, cp.QuoteReceivedAt, cp.KellyReceivedAt = "", "", ""
	got = AnalyzePublicSourceModel(input)
	if got.InputSources[0].Source != "zgzcw_cp" || got.InputSources[0].ReceivedAt != input.Provenance["euro"].Received || !strings.Contains(got.InputSources[0].Reason, "捕获上界") {
		t.Fatal("legacy complete bundle lost its conservative snapshot time")
	}
}

func TestPublicSourceCPUsesVerifiedIDsAcrossThreeDisplayNamePairs(t *testing.T) {
	input := publicSourceCPFixture()
	input.Match.Home, input.Match.Away = "马斯特里赫特", "维迪斯"
	input.Match.SourceHome, input.Match.SourceAway = "马斯特里赫特", "维特斯"
	input.Parts.Bonus.Home, input.Parts.Bonus.Away = "马斯特里", "维迪斯"
	got := AnalyzePublicSourceModel(input)
	if got.InputSources[0].Source != "zgzcw_cp" {
		t.Fatal("verified CP/FENXI ID binding was vetoed by display aliases")
	}
	input.Parts.Euro[0].CPExtras.ZgzcwMatchID = "wrong-fenxi"
	got = AnalyzePublicSourceModel(input)
	if got.InputSources[0].Source == "zgzcw_cp" {
		t.Fatal("display aliases overrode a wrong FENXI ID")
	}
	input.Parts.Euro[0].CPExtras.ZgzcwMatchID = input.Match.ZgzcwID
	input.Parts.Euro[0].CPExtras.SourceMatchID = "wrong-cp"
	if got = AnalyzePublicSourceModel(input); got.InputSources[0].Source == "zgzcw_cp" {
		t.Fatal("display aliases overrode a wrong CP ID")
	}
}

func publicSourceOpeningHistoryFixture(label string, cp bool) PublicSourceModelInput {
	input := publicSourceCPFixture()
	q := input.Parts.Euro[0]
	q.Label = label
	q.Opening = []string{"3.1", "3.7", "2.05"}
	q.Latest = []string{"2.8", "3.5", "2.2"}
	q.OpeningKelly = []string{"0.99", "0.96", "0.84"}
	q.OpeningKellyPrices = []string{"3.10", "3.70", "2.01"}
	q.OpeningKellyTime = "2026-11-09 19:25:19"
	q.FullQuoteHistory = [][]string{
		{"1", "2026-11-14 16:45:00", "赛前", "2.8↓", "3.5", "2.2↑", "", "", "", "0.9", "0.95", "0.91", "0.93"},
		{"2", q.OpeningKellyTime, "赛前", "3.10", "3.70", "2.01", "", "", "", "0.99", "0.96", "0.84", "0.93"},
	}
	if cp {
		q.CPExtras.Label, q.CPExtras.SourceCompanyID = "36*", "2"
	} else {
		q.CPExtras = nil
		q.Kelly = []string{"0.9", "0.95", "0.91"}
	}
	input.Parts.Euro = []publicSourceQuote{q}
	return input
}

func TestPublicSourceOpeningHistoryPreservesFourMissingKellyPairs(t *testing.T) {
	for _, tt := range []struct {
		number, label string
		cp            bool
	}{{"005", "36*", true}, {"006", "平*", false}, {"009", "威*", false}, {"010", "36*", false}} {
		t.Run(tt.number, func(t *testing.T) {
			input := publicSourceOpeningHistoryFixture(tt.label, tt.cp)
			before, _ := json.Marshal(input)
			got := AnalyzePublicSourceModel(input)
			s := got.InputSources[0]
			q := input.Parts.Euro[0]
			if s.OpeningSource != "zgzcw_full_quote_history" || s.OpeningAt != q.OpeningKellyTime || !reflect.DeepEqual(s.Opening, q.OpeningKellyPrices) || !reflect.DeepEqual(s.OpeningKelly, q.OpeningKelly) || !strings.Contains(s.Reason, "初盘采用来源完整历史最早配对报价") {
				t.Fatalf("history pair not consumed: %+v", s)
			}
			wantLatest, wantKelly := q.Latest, q.Kelly
			if tt.cp {
				wantLatest, wantKelly = q.CPExtras.Latest, q.CPExtras.Kelly
			}
			if !reflect.DeepEqual(s.Latest, wantLatest) || !reflect.DeepEqual(s.Kelly, wantKelly) {
				t.Fatal("opening recovery replaced the selected current phase")
			}
			kickoff, _ := time.ParseInLocation("2006-01-02 15:04:05", input.Match.Kickoff, drawSourceLocation)
			odds, _ := publicSourceMarkets(input, models.Money{MatchTime: kickoff})
			var rows []analysisEuroOdd
			if json.Unmarshal(odds.Data, &rows) != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0].FirstOdds, q.OpeningKellyPrices) {
				t.Fatal("historical opening was reported but not fed to model")
			}
			after, _ := json.Marshal(input)
			if string(before) != string(after) {
				t.Fatal("opening source display was overwritten")
			}
		})
	}
}

func TestPublicSourceOpeningHistoryRequiresCompleteSafePair(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*publicSourceQuote)
	}{
		{"missing history", func(q *publicSourceQuote) { q.FullQuoteHistory = nil }},
		{"missing marker time", func(q *publicSourceQuote) { q.OpeningKellyTime = "" }},
		{"wrong history prices", func(q *publicSourceQuote) { q.FullQuoteHistory[1][3] = "3.2" }},
		{"wrong history Kelly", func(q *publicSourceQuote) { q.FullQuoteHistory[1][9] = "0.98" }},
		{"marker is not earliest", func(q *publicSourceQuote) { q.FullQuoteHistory[1][1] = "2026-11-08 19:25:19" }},
		{"after capture", func(q *publicSourceQuote) {
			q.OpeningKellyTime = "2026-11-14 17:01:00"
			q.FullQuoteHistory = q.FullQuoteHistory[1:]
			q.FullQuoteHistory[0][1] = q.OpeningKellyTime
		}},
		{"after kickoff", func(q *publicSourceQuote) {
			q.OpeningKellyTime = "2026-11-14 18:16:00"
			q.FullQuoteHistory = q.FullQuoteHistory[1:]
			q.FullQuoteHistory[0][1] = q.OpeningKellyTime
		}},
		{"invalid history time", func(q *publicSourceQuote) { q.FullQuoteHistory[1][1] = "unknown" }},
		{"conflicting earliest", func(q *publicSourceQuote) {
			r := append([]string(nil), q.FullQuoteHistory[1]...)
			r[3] = "3.2"
			q.FullQuoteHistory = append(q.FullQuoteHistory, r)
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			input := publicSourceOpeningHistoryFixture("36*", true)
			tt.mutate(&input.Parts.Euro[0])
			got := AnalyzePublicSourceModel(input)
			s := got.InputSources[0]
			if s.OpeningSource == "zgzcw_full_quote_history" || len(s.OpeningKelly) != 0 || !strings.Contains(s.Reason, "未带入") || !reflect.DeepEqual(s.Opening, input.Parts.Euro[0].CPExtras.Opening) {
				t.Fatalf("unproven opening history entered model: %+v", s)
			}
		})
	}
}

func TestPublicSourceMarketContextSameLineAndFrozenIsolation(t *testing.T) {
	input := publicModelFixture()
	before := AnalyzePublicSourceModel(input)
	input.Parts.Totals = append(input.Parts.Totals,
		publicSourceQuote{Label: "澳*", Opening: []string{"0.8", "2.5/3球", "1.0"}, Latest: []string{"0.8", "3球", "1.0"}},
		publicSourceQuote{Label: "独立公司", Opening: []string{"0.6", "2.5/3球", "1.2"}, Latest: []string{"0.4", "3球", "1.2"}},
		publicSourceQuote{Label: "不同盘口", Opening: []string{"0.1", "2.5球", "1.9"}, Latest: []string{"0.1", "2.5球", "1.9"}},
		publicSourceQuote{Label: "平均值", Opening: []string{"5", "2.75", "5"}, Latest: []string{"5", "3", "5"}},
		publicSourceQuote{Label: "重复", Latest: []string{"9", "3", "9"}}, publicSourceQuote{Label: "重复", Latest: []string{"9", "3", "9"}},
		publicSourceQuote{Label: "无效水位", Latest: []string{"0", "3", "1"}})
	input.Parts.Asian = append(input.Parts.Asian, publicSourceQuote{Label: "澳门", Opening: []string{"0.8", "受半/一", "1.0"}, Latest: []string{"0.85", "受半球", "0.95"}})
	got := AnalyzePublicSourceModel(input)
	context := got.MarketContext
	if context == nil || context.UsedByFrozenModels || len(context.Markets) != 2 {
		t.Fatalf("missing context: %+v", context)
	}
	if !reflect.DeepEqual(before.Observation, got.Observation) {
		t.Fatal("new context changed frozen model or business output")
	}
	p := context.Markets[1].Phases[1]
	if p.Macau == nil || *p.Macau.OverWater != .8 || *p.Macau.UnderWater != 1 || context.Markets[1].LeftMeaning != "over" || context.Markets[1].RightMeaning != "under" {
		t.Fatal("totals source sides were swapped")
	}
	if p.MacauVsAverage == nil || p.MacauVsAverage.CompanyCount != 4 || math.Abs(p.MacauVsAverage.LeftWaterDifference) < 1e-8 {
		t.Fatalf("wrong matching-line average: %+v", p)
	}
	if math.Abs(context.Features["totals.latest.averageAtMacauLine.leftWater"]-.75) > 1e-8 || math.Abs(context.Features["totals.latest.averageAtMacauLine.rightWater"]-1) > 1e-8 {
		t.Fatalf("average mixed lines, synthetic rows or duplicates: %+v", context.Features)
	}
	if context.Markets[0].Phases[0].Macau.Line != -.75 || context.Markets[0].LeftMeaning != "home" {
		t.Fatal("Asian sign or sides changed")
	}
	raw, e := json.Marshal(context)
	if e != nil || !strings.Contains(string(raw), `"usedByFrozenModels":false`) {
		t.Fatal("context cannot be consumed as JSON")
	}
}

func TestPublicSourceMarketContextPreservesMissingAndRejectsAmbiguity(t *testing.T) {
	input := publicModelFixture()
	input.Parts.Asian = []publicSourceQuote{{Label: "澳*", Opening: []string{"0.9", "-0.5", "0.9"}, Latest: []string{"0.9", "未知盘口", "0.9"}}}
	input.Parts.Totals = []publicSourceQuote{{Label: "澳*", Latest: []string{"0.9", "2.5/3.5", "0.9"}}, {Label: "澳门", Latest: []string{"0.9", "3", "0.9"}}}
	context := publicSourceBuildMarketContext(input)
	for _, market := range context.Markets {
		for _, phase := range market.Phases {
			if phase.Macau != nil || phase.MacauVsAverage != nil || len(phase.Averages) != 0 {
				t.Fatal("invalid/missing/duplicate company became a market input")
			}
		}
	}
	if len(context.Features) != 0 {
		t.Fatal("missing data became zero-valued features")
	}
}
