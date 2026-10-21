package handlers

import (
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"go_server/internal/drawmodel"
	"go_server/models"
)

func drawHistoricalAssertFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-10 {
		t.Fatalf("%s = %v, want %.12f", name, got, want)
	}
}

func TestDrawHistoricalSummaryExcludesUnavailablePredictions(t *testing.T) {
	rows := []drawHistoricalRow{
		{MatchID: "tp", IsDraw: true, Probability: drawFloat(60), MarketProbability: drawFloat(50)},
		{MatchID: "fp", Probability: drawFloat(40), MarketProbability: drawFloat(35)},
		{MatchID: "fn", IsDraw: true, Probability: drawFloat(20)},
		{MatchID: "tn", Probability: drawFloat(10)},
		// Even stale UI fields cannot turn an uncomputed match into a hit.
		{MatchID: "missing-draw", IsDraw: true, Selected: true, Outcome: "hit", MarketProbability: drawFloat(99)},
		{MatchID: "missing-other", Selected: true, Outcome: "false_positive"},
	}
	wantOutcome := []string{"hit", "false_positive", "missed", "correct_rejection", "unavailable", "unavailable"}
	for i := range rows {
		rows[i] = classifyDrawHistoricalRow(rows[i], 32)
		if rows[i].Outcome != wantOutcome[i] {
			t.Fatalf("row %s outcome = %s, want %s", rows[i].MatchID, rows[i].Outcome, wantOutcome[i])
		}
	}
	summary, bins := summarizeDrawHistoricalRows(rows)
	if summary.Total != 6 || summary.Eligible != 4 || summary.ActualDraws != 3 || summary.Selected != 2 || summary.Hits != 1 || summary.FalsePositives != 1 || summary.FalseNegatives != 1 || summary.TrueNegatives != 1 {
		t.Fatalf("unexpected confusion matrix: %+v", summary)
	}
	drawHistoricalAssertFloat(t, "precision", summary.Precision, 50)
	drawHistoricalAssertFloat(t, "recall", summary.Recall, 50)
	drawHistoricalAssertFloat(t, "coverage", summary.Coverage, 50)
	drawHistoricalAssertFloat(t, "actual pool draw rate", summary.DrawRate, 50)
	drawHistoricalAssertFloat(t, "Brier", summary.Brier, .2425)
	drawHistoricalAssertFloat(t, "market Brier on available paired rows", summary.MarketBrier, .18625)
	n := 0
	for _, bin := range bins {
		n += bin.Count
	}
	if n != 4 {
		t.Fatalf("calibration bins contain %d rows, want only 4 computed predictions", n)
	}
	boundary := classifyDrawHistoricalRow(drawHistoricalRow{Probability: drawFloat(32), IsDraw: true}, 32)
	if !boundary.Selected || boundary.Outcome != "hit" {
		t.Fatal("a probability equal to the selected threshold should qualify")
	}
	empty, _ := summarizeDrawHistoricalRows(rows[4:])
	if empty.Eligible != 0 || empty.Precision != nil || empty.Recall != nil || empty.Coverage != nil || empty.Brier != nil || empty.MarketBrier != nil {
		t.Fatalf("unavailable-only sample must have no model metrics: %+v", empty)
	}
}

func TestDrawHistoricalIndexRespectsResultAvailability(t *testing.T) {
	base := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	target := drawmodel.Match{ID: "target", LeagueName: "L", HomeTeamID: "1", AwayTeamID: "2", Kickoff: base.Add(10 * time.Hour)}
	asOf := target.Kickoff.Add(-time.Second)
	game := func(id, league, home, away string, kickoff, completed time.Time) drawHistoricalGame {
		return drawHistoricalGame{Game: drawmodel.Match{ID: id, LeagueName: league, HomeTeamID: home, AwayTeamID: away, Kickoff: kickoff, CompletedAt: completed, Completed: true}, Sources: []string{"history_moneys"}}
	}
	games := []drawHistoricalGame{
		game("older", "L", "3", "4", base.Add(-48*time.Hour), base.Add(-46*time.Hour)),
		game("same-team-other-league", "Cup", "1", "5", base.Add(-24*time.Hour), base.Add(-22*time.Hour)),
		game("older", "L", "3", "4", base.Add(-48*time.Hour), base.Add(-46*time.Hour)),
		game("still-playing", "L", "1", "6", base.Add(9*time.Hour), base.Add(11*time.Hour)),
		// A post-match source can contain the target and later fixtures. The
		// source's presence in today's database must never admit their results.
		game("target", "L", "1", "2", target.Kickoff, base.Add(-time.Hour)),
		game("future", "L", "1", "2", base.Add(24*time.Hour), base.Add(26*time.Hour)),
	}
	sort.SliceStable(games, func(i, j int) bool { return games[i].Game.Kickoff.Before(games[j].Game.Kickoff) })
	got := indexDrawHistory(games).before(target, asOf)
	ids := make([]string, 0, len(got))
	for _, row := range got {
		ids = append(ids, row.ID)
		if !row.CompletedAt.Before(asOf) || row.ID == target.ID {
			t.Fatalf("unavailable result entered the historical pool: %+v", row)
		}
	}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"older", "same-team-other-league"}) {
		t.Fatalf("historical fixture IDs = %v", ids)
	}
}

func TestDrawHistoricalChineseHandicapAndTotalLines(t *testing.T) {
	for _, tc := range []struct {
		value string
		total bool
		want  float64
	}{
		{"平手", false, 0}, {"平手/半球", false, .25}, {"受平手/半球", false, -.25},
		{"半球/一球", false, .75}, {"一球/一球半", false, 1.25}, {"受一球半/二球", false, -1.75},
		{"受二球/二球半", false, -2.25}, {"二球/二球半", true, 2.25}, {"两球/两球半", true, 2.25},
		{"二球半/三球", true, 2.75}, {"三球/三球半", true, 3.25}, {"三球半/四球", true, 3.75},
		{"四球/四球半", true, 4.25}, {"0", false, 0}, {"-0.5/0", false, -.25},
	} {
		t.Run(tc.value, func(t *testing.T) {
			drawHistoricalAssertFloat(t, tc.value, drawLine(tc.value, tc.total), tc.want)
		})
	}
	for _, tc := range []struct {
		value string
		total bool
	}{{"", false}, {"-", false}, {"未知盘口", false}, {"平手", true}, {"受半球", true}} {
		if got := drawLine(tc.value, tc.total); got != nil {
			t.Errorf("invalid line %q total=%v became %v", tc.value, tc.total, *got)
		}
	}
}

func TestDrawHistoricalOddsSeparatesVerifiedAndOpeningSources(t *testing.T) {
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	game := drawHistoricalGame{Game: drawmodel.Match{ID: "fixture", Kickoff: asOf.Add(time.Second)}}
	t.Run("verified handicap alone", func(t *testing.T) {
		data := drawHistoricalData{Pankou: map[string]models.PankouMoney{"fixture": {
			CapturedAt: drawTestPtr(asOf.Add(-time.Minute)),
			Bet365Asia: drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "平手", FirstPankou: "半球"}),
			Bet365Dxq:  drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "二球/二球半", FirstPankou: "三球"}),
		}}}
		quote, provenance := historicalDrawOdds(data, game, asOf)
		if quote == nil || provenance != "captured_prematch" {
			t.Fatalf("valid independently captured handicap was lost: %v %s", quote, provenance)
		}
		drawHistoricalAssertFloat(t, "verified current handicap", quote.AsianHandicap, 0)
		drawHistoricalAssertFloat(t, "verified current total", quote.GoalLine, 2.25)
		if _, valid := drawmodel.MarketDrawProbability(*quote); valid {
			t.Fatal("handicap alone invented a three-way market probability")
		}
	})
	t.Run("unverified opening quotes never use terminal values", func(t *testing.T) {
		data := drawHistoricalData{
			Odds: map[string]models.OddsMoney{"fixture": {CapturedAt: drawTestPtr(asOf.Add(time.Hour)), Data: drawIntegrationJSON(t, []analysisEuroOdd{
				{CompanyID: "a", Odds: []string{"1.2", "8", "12"}, FirstOdds: []string{"4", "5", "6"}},
				{CompanyID: "a", Odds: []string{"1.2", "8", "12"}, FirstOdds: []string{"9", "9", "9"}},
				{CompanyID: "b", Odds: []string{"1.2", "8", "12"}, FirstOdds: []string{"6", "4", "2"}},
			})}},
			Pankou: map[string]models.PankouMoney{"fixture": {
				Bet365Asia: drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "两球", FirstPankou: "受平手/半球"}),
				Bet365Dxq:  drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "四球半", FirstPankou: "二球/二球半"}),
			}},
		}
		quote, provenance := historicalDrawOdds(data, game, asOf)
		if quote == nil || provenance != "opening_reconstructed" {
			t.Fatalf("unverified opening quote must remain exploratory: %v %s", quote, provenance)
		}
		got, valid := drawmodel.MarketDrawProbability(*quote)
		want := ((1.0/5)/(1.0/4+1.0/5+1.0/6) + (1.0/4)/(1.0/6+1.0/4+1.0/2)) / 2
		if !valid || math.Abs(got-want) > 1e-12 {
			t.Fatalf("opening probability = %.12f, want %.12f; current odds or duplicate company leaked", got, want)
		}
		drawHistoricalAssertFloat(t, "opening handicap", quote.AsianHandicap, -.25)
		drawHistoricalAssertFloat(t, "opening total", quote.GoalLine, 2.25)
	})
	t.Run("current-only unverified data stays missing", func(t *testing.T) {
		data := drawHistoricalData{
			Odds: map[string]models.OddsMoney{"fixture": {Data: drawIntegrationJSON(t, []analysisEuroOdd{{CompanyID: "a", Odds: []string{"2", "3", "4"}}})}},
			Pankou: map[string]models.PankouMoney{"fixture": {
				Bet365Asia: drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "半球"}),
				Bet365Dxq:  drawIntegrationJSON(t, analysisPankouItem{CompanyID: 8, Pankou: "二球半"}),
			}},
		}
		quote, provenance := historicalDrawOdds(data, game, asOf)
		if quote != nil || provenance != "missing" {
			t.Fatalf("terminal odds were substituted for missing opening values: %+v %s", quote, provenance)
		}
	})
}

func TestDrawHistoricalArchiveRoundTripPreservesUnavailableRows(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 123000000, time.UTC)
	report := drawHistoricalReport{
		ModelVersion: drawmodel.HistoricalVersion, GeneratedAt: at,
		Rows: []drawHistoricalRow{
			{MatchID: "unavailable", MatchTime: at.Add(-24 * time.Hour), Home: "主队", Guest: "客队", IsDraw: true, HomeScore: 1, AwayScore: 1, Probability: nil, RawProbability: nil, MarketProbability: nil, Split: "excluded", Warnings: []string{"没有当时可用的历史"}, Signals: nil, Sources: []string{"history_moneys"}, GroupProbabilities: nil},
			{MatchID: "computed", MatchTime: at, Probability: drawFloat(35.5), RawProbability: drawFloat(32), GroupProbabilities: map[string]float64{"score_model": 32}, Signals: []drawExistingSignal{{Key: "history_handicap", Label: "历史期望让球", Market: "spf", Direction: "draw", Value: drawFloat(0)}, {Key: "pro_signal", Direction: "draw", Value: nil}}, Warnings: []string{}},
		},
		Sources: []drawHistorySource{drawSource("history_moneys", "历史战绩", 12, 10, 9, "来源明细")},
		Evaluation: drawmodel.HistoricalEvaluation{
			Version:  drawmodel.HistoricalVersion,
			Rows:     []drawmodel.HistoricalPrediction{{ID: "unavailable", Partition: "excluded", Probability: nil, GroupProbabilities: nil, Warnings: []string{"缺少历史"}}},
			Artifact: nil,
		},
		Notes: []string{"历史重建，保留未计算记录"},
	}
	encoded, err := encodeDrawArchive(&report)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) < 2 || encoded[0] != 0x1f || encoded[1] != 0x8b {
		t.Fatal("historical report was not stored as gzip")
	}
	var decoded drawHistoricalReport
	if err := decodeDrawArchive(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report, decoded) {
		t.Fatalf("historical report changed in archive round trip:\nbefore=%+v\nafter=%+v", report, decoded)
	}
	if err := decodeDrawArchive([]byte("not an archive"), &decoded); err == nil {
		t.Fatal("invalid archive unexpectedly decoded")
	}
}
