package handlers

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func drawProfitSupportFixture(official, simulated [3]float64) drawObservationItem {
	row := drawMetricFixture("profit-fixture", "selected", drawMetricScore(.3), 1, 1, true)
	weighted := &drawObservationWeightedFeatureInputs{}
	market := bookmakerMarketResponse{Key: "sportterySim", Simulated: true, OddsAvailable: true}
	for i, direction := range []string{"home", "draw", "away"} {
		value := official[i]
		weighted.OfficialProfitRates[i] = &value
		market.BookmakerByOutcome = append(market.BookmakerByOutcome, bookmakerOutcomeResponse{Outcome: direction, Available: true, Odds: 2, TotalStake: 10000, BookmakerProfit: simulated[i] * 100})
	}
	row.FrontendAnalysis = &drawObservationFrontendAnalysis{WeightedFeatureInputs: weighted, RoiSimulation: &matchRoiSimulationResponse{Markets: []bookmakerMarketResponse{market}}}
	return row
}

func TestDrawProfitSupportDirectionSetsAndRealZero(t *testing.T) {
	for _, example := range []struct {
		name                              string
		official, simulated               [3]float64
		state                             string
		officialMinimum, simulatedMinimum []string
	}{
		{"disjoint", [3]float64{-10, 2, 5}, [3]float64{5, 2, -10}, "supported", []string{"home"}, []string{"away"}},
		{"same direction", [3]float64{2, -10, 5}, [3]float64{5, -5, 10}, "not_supported", []string{"draw"}, []string{"draw"}},
		{"true zero", [3]float64{0, 1, 2}, [3]float64{1, 0, 2}, "supported", []string{"home"}, []string{"draw"}},
		{"ties overlap", [3]float64{0, 0, 1}, [3]float64{2, 0, 0}, "not_supported", []string{"home", "draw"}, []string{"draw", "away"}},
		{"all zero", [3]float64{}, [3]float64{}, "not_supported", []string{"home", "draw", "away"}, []string{"home", "draw", "away"}},
		{"epsilon overlap", [3]float64{0, .5e-9, 1}, [3]float64{1, 0, 2}, "not_supported", []string{"home", "draw"}, []string{"draw"}},
		{"outside epsilon", [3]float64{0, 2e-9, 1}, [3]float64{1, 0, 2}, "supported", []string{"home"}, []string{"draw"}},
	} {
		t.Run(example.name, func(t *testing.T) {
			row := drawProfitSupportFixture(example.official, example.simulated)
			got := drawObservationProfitSupport(row)
			if got.Version != "profit-support-v1" || got.State != example.state || !reflect.DeepEqual(got.OfficialMinimum, example.officialMinimum) || !reflect.DeepEqual(got.SimulatedMinimum, example.simulatedMinimum) {
				t.Fatalf("wrong direction sets: %+v", got)
			}
		})
	}
}

func TestDrawProfitSupportMissingNeverBorrowsOrFabricatesZero(t *testing.T) {
	mutations := map[string]func(*drawObservationItem){
		"missing analysis":     func(row *drawObservationItem) { row.FrontendAnalysis = nil },
		"missing strict rates": func(row *drawObservationItem) { row.FrontendAnalysis.WeightedFeatureInputs = nil },
		"one absent source rate": func(row *drawObservationItem) {
			row.FrontendAnalysis.WeightedFeatureInputs.OfficialProfitRates[1] = nil
		},
		"source NaN": func(row *drawObservationItem) {
			row.FrontendAnalysis.WeightedFeatureInputs.OfficialProfitRates[0] = drawMetricScore(math.NaN())
		},
		"source infinity": func(row *drawObservationItem) {
			row.FrontendAnalysis.WeightedFeatureInputs.OfficialProfitRates[0] = drawMetricScore(math.Inf(1))
		},
		"missing simulation":      func(row *drawObservationItem) { row.FrontendAnalysis.RoiSimulation = nil },
		"missing simulation odds": func(row *drawObservationItem) { row.FrontendAnalysis.RoiSimulation.Markets[0].OddsAvailable = false },
		"amount unavailable": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome[0].Available = false
		},
		"invalid outcome odds": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome[0].Odds = 0
		},
		"nonfinite outcome odds": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome[0].Odds = math.Inf(1)
		},
		"invalid base": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome[0].TotalStake = 0
		},
		"nonfinite base": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome[0].TotalStake = math.Inf(1)
		},
		"nonfinite profit": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome[0].BookmakerProfit = math.NaN()
		},
		"missing outcome": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome = row.FrontendAnalysis.RoiSimulation.Markets[0].BookmakerByOutcome[:2]
		},
		"duplicate outcome": func(row *drawObservationItem) {
			m := &row.FrontendAnalysis.RoiSimulation.Markets[0]
			m.BookmakerByOutcome = append(m.BookmakerByOutcome, m.BookmakerByOutcome[0])
		},
		"ambiguous result simulation": func(row *drawObservationItem) {
			s := row.FrontendAnalysis.RoiSimulation
			s.Markets = append(s.Markets, s.Markets[0])
		},
		"handicap simulation only": func(row *drawObservationItem) {
			row.FrontendAnalysis.RoiSimulation.Markets[0].Key = "sportteryRqspfSim"
		},
		"other handicap simulation only": func(row *drawObservationItem) { row.FrontendAnalysis.RoiSimulation.Markets[0].Key = "sportteryRqspf" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			row := drawProfitSupportFixture([3]float64{0, 1, 2}, [3]float64{1, 0, 2})
			mutate(&row)
			if got := drawObservationProfitSupport(row); got.State != "unavailable" {
				t.Fatalf("missing/malformed data produced a feature: %+v", got)
			}
		})
	}
	row := drawProfitSupportFixture([3]float64{0, 1, 2}, [3]float64{1, 0, 2})
	row.JingcaiID = " "
	if got := drawObservationProfitSupport(row); got.State != "out_of_scope" {
		t.Fatalf("non-sporttery row entered feature observation: %+v", got)
	}
	row = drawProfitSupportFixture([3]float64{0, 1, 2}, [3]float64{1, 0, 2})
	row.FrontendAnalysis.RoiSimulation.Markets = append(row.FrontendAnalysis.RoiSimulation.Markets, bookmakerMarketResponse{Key: "sportteryRqspfSim", Simulated: true})
	if got := drawObservationProfitSupport(row); got.State != "supported" {
		t.Fatalf("unrelated handicap simulation contaminated comparison: %+v", got)
	}
}

func TestDrawProfitSupportIgnoresTargetResultAndModelChoice(t *testing.T) {
	row := drawProfitSupportFixture([3]float64{0, 1, 2}, [3]float64{1, 0, 2})
	before, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	want := drawObservationProfitSupport(row)
	after, _ := json.Marshal(row)
	if string(before) != string(after) {
		t.Fatal("feature extraction mutated input")
	}
	for _, state := range []string{"selected", "not_selected", "unavailable"} {
		for _, score := range [][2]int{{0, 0}, {1, 1}, {3, 0}, {0, 3}} {
			changed := row
			changed.HomeScore, changed.AwayScore = score[0], score[1]
			changed.Settled = !row.Settled
			changed.Status = -999
			changed.FrozenDrawSignal = &drawObservationFrozenSignal{State: state, Score: drawMetricScore(.1)}
			if got := drawObservationProfitSupport(changed); !reflect.DeepEqual(got, want) {
				t.Fatalf("result/model decision changed feature classification: %+v", got)
			}
		}
	}
}

func TestDrawProfitSupportMetricsDoNotAlterCandidatePool(t *testing.T) {
	supported := func(id string, score float64, home, away int, settled bool) drawObservationItem {
		row := drawProfitSupportFixture([3]float64{0, 1, 2}, [3]float64{1, 0, 2})
		row.MatchID, row.HomeScore, row.AwayScore, row.Settled = id, home, away, settled
		row.RegulationSettled = &settled
		row.FrozenDrawSignal.Score = drawMetricScore(score)
		return row
	}
	items := []drawObservationItem{supported("supported-draw", .225, 1, 1, true), supported("supported-miss", .3, 1, 0, true), supported("not-supported", .3, 0, 0, true), supported("unknown", .3, 2, 2, true), supported("pending", .3, 0, 0, false), supported("rejected", .22, 1, 1, true), supported("original-not-selected", .1, 1, 1, true), supported("out-of-scope", .3, 0, 0, true)}
	items[2].FrontendAnalysis.WeightedFeatureInputs.OfficialProfitRates[0] = drawMetricScore(2)
	items[2].FrontendAnalysis.WeightedFeatureInputs.OfficialProfitRates[1] = drawMetricScore(0)
	items[3].FrontendAnalysis = nil
	items[6].FrozenDrawSignal.State = "not_selected"
	items[7].JingcaiID = ""
	before, _ := json.Marshal(items)
	saved := drawObservationStoredDay{Date: "2026-10-21", Items: items, DrawIDs: map[string]bool{"supported-draw": true, "not-supported": true, "unknown": true, "rejected": true, "original-not-selected": true, "out-of-scope": true}}
	q := drawObservationQuery{date: saved.Date, scope: "all", mode: "today", page: 1, size: 2}
	base := saved.filter(q).ObservationMetrics
	got := base.ProfitSupport
	if base.Secondary.Selected != 5 || base.Secondary.Settled != 4 || base.Secondary.Hits != 3 || base.Rejected.Selected != 1 || base.EligibleDraws != 5 {
		t.Fatalf("profit tag changed original candidate accounting: %+v", base)
	}
	if got == nil || got.Version != "profit-support-v1" || got.Status != "incomplete" || got.SecondaryComparable.Selected != 4 || got.SecondaryComparable.Hits != 2 || got.SecondarySupported.Selected != 3 || got.SecondarySupported.Hits != 1 || got.SecondarySupported.Pending != 1 || got.SecondaryNotSupported.Selected != 1 || got.SecondaryUnavailable.Selected != 1 || got.SecondaryUnavailable.Hits != 1 {
		t.Fatalf("wrong support feature metrics: %+v", got)
	}
	drawMetricRequireRate(t, got.SecondaryComparable.Precision, 2.0/3)
	drawMetricRequireRate(t, got.SecondarySupported.Precision, .5)
	drawMetricRequireRate(t, got.SecondarySupported.Coverage, 1.0/5)
	for _, changed := range []drawObservationQuery{{date: saved.Date, scope: "all", mode: "history", page: 999, size: 1}, {date: saved.Date, scope: "sporttery", mode: "history", page: 2, size: 1}} {
		if now := saved.filter(changed).ObservationMetrics.ProfitSupport; !reflect.DeepEqual(now, got) {
			t.Fatalf("page/tab/scope changed complete support metrics: %+v", now)
		}
	}
	after, _ := json.Marshal(items)
	if string(before) != string(after) {
		t.Fatal("metrics mutated cached rows or original model choices")
	}
	unknown := supported("unknown", .3, 1, 1, true)
	unknown.FrontendAnalysis = nil
	metrics := buildDrawObservationMetrics([]drawObservationItem{unknown}, q).ProfitSupport
	if metrics.Status != "incomplete" || metrics.SecondaryComparable.Coverage != nil || metrics.SecondarySupported.Coverage != nil || metrics.SecondarySupported.Precision != nil {
		t.Fatalf("all-unavailable feature pretended to be measured zero: %+v", metrics)
	}
}

// This optional fixture remains a local research file, never an application
// dependency or database/network request. Missing fixture paths explicitly skip.
func TestDrawProfitSupportHistoricalFixture(t *testing.T) {
	datasetPath, scoresPath := os.Getenv("DRAW_PROFIT_SUPPORT_DATASET"), os.Getenv("DRAW_PROFIT_SUPPORT_SCORES")
	if datasetPath == "" || scoresPath == "" {
		t.Skip("set DRAW_PROFIT_SUPPORT_DATASET and DRAW_PROFIT_SUPPORT_SCORES for local full-history parity")
	}
	data, err := os.ReadFile(datasetPath)
	if err != nil {
		t.Fatal(err)
	}
	var dataset struct {
		Rows []struct {
			MatchID   string                           `json:"matchId"`
			Date      string                           `json:"date"`
			JingcaiID string                           `json:"jingcaiId"`
			HomeScore int                              `json:"homeScore"`
			AwayScore int                              `json:"awayScore"`
			Frontend  *drawObservationFrontendAnalysis `json:"frontend"`
		} `json:"rows"`
	}
	if err = json.Unmarshal(data, &dataset); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(scoresPath)
	if err != nil {
		t.Fatal(err)
	}
	var scores struct {
		Scores map[string]struct {
			Score            float64 `json:"score"`
			OriginalSelected bool    `json:"originalSelected"`
		} `json:"scores"`
	}
	if err = json.Unmarshal(data, &scores); err != nil {
		t.Fatal(err)
	}
	items := []drawObservationItem{}
	reference := map[string]drawObservationProfitSupportSignal{}
	fullComparable, fullSupported, fullComparableDraws, fullSupportedDraws, originalSupported, originalSupportedDraws := 0, 0, 0, 0, 0, 0
	for _, source := range dataset.Rows {
		if source.JingcaiID == "" {
			continue
		}
		model, ok := scores.Scores[source.MatchID]
		if !ok {
			t.Fatalf("missing frozen score for %s", source.MatchID)
		}
		confirmed := true
		state := "not_selected"
		if model.OriginalSelected {
			state = "selected"
		}
		row := drawObservationItem{MatchID: source.MatchID, JingcaiID: source.JingcaiID, Date: source.Date, HomeScore: source.HomeScore, AwayScore: source.AwayScore, Settled: true, RegulationSettled: &confirmed, FrontendAnalysis: source.Frontend, FrozenDrawSignal: &drawObservationFrozenSignal{State: state, Score: drawMetricScore(model.Score)}}
		signal := drawObservationProfitSupport(row)
		reference[row.MatchID] = signal
		if signal.State == "supported" || signal.State == "not_supported" {
			fullComparable++
			if row.HomeScore == row.AwayScore {
				fullComparableDraws++
			}
		}
		if signal.State == "supported" {
			fullSupported++
			if row.HomeScore == row.AwayScore {
				fullSupportedDraws++
			}
			if model.OriginalSelected {
				originalSupported++
				if row.HomeScore == row.AwayScore {
					originalSupportedDraws++
				}
			}
		}
		items = append(items, row)
	}
	metrics := buildDrawObservationMetrics(items, drawObservationQuery{scope: "sporttery"})
	actual := []int{len(items), metrics.EligibleDraws, fullComparable, fullComparableDraws, fullSupported, fullSupportedDraws, metrics.Original.Selected, metrics.Original.Hits, originalSupported, originalSupportedDraws, metrics.Secondary.Selected, metrics.Secondary.Hits, metrics.ProfitSupport.SecondaryComparable.Selected, metrics.ProfitSupport.SecondaryComparable.Hits, metrics.ProfitSupport.SecondarySupported.Selected, metrics.ProfitSupport.SecondarySupported.Hits}
	want := []int{552, 135, 502, 130, 231, 68, 341, 129, 155, 66, 305, 125, 296, 124, 141, 66}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("full-history parity mismatch: got %v want %v", actual, want)
	}
	if output := os.Getenv("DRAW_PROFIT_SUPPORT_REFERENCE_OUT"); output != "" {
		body, err := json.Marshal(struct {
			Signals map[string]drawObservationProfitSupportSignal `json:"signals"`
			Metrics *drawObservationMetrics                       `json:"metrics"`
		}{reference, metrics})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("real historical rows=%d secondary comparable=%d/%d supported=%d/%d", len(items), metrics.ProfitSupport.SecondaryComparable.Selected, metrics.ProfitSupport.SecondaryComparable.Hits, metrics.ProfitSupport.SecondarySupported.Selected, metrics.ProfitSupport.SecondarySupported.Hits)
}
