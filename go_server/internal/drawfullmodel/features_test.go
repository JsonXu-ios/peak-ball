package drawfullmodel

import (
	"encoding/json"
	"reflect"
	"testing"
)

func fullTestInput() map[string]interface{} {
	return map[string]interface{}{
		"matchId": "target", "matchTime": "2026-11-04T12:15:00Z", "result": 1, "homeScore": 0, "awayScore": 0,
		"homeHeat": 27.48, "history": map[string]interface{}{},
		"analysisInputs": map[string]interface{}{"marketProbabilities": []float64{24.32, 24.32, 51.37}, "historyGoalAverage": nil, "recentGoalAverage": 3.6, "availability": map[string]interface{}{"recentHome": true, "recentGuest": true, "historySummary": false}, "tradeFields": map[string]interface{}{"tzbl.asupportRate": 78, "jyykSpf.ay": -19.34, "jyykRqspf.ay": -45.6}},
		"goalsHeat":      map[string]interface{}{"available": true, "over": 65.3, "under": 34.7},
		"fullAnalysis": map[string]interface{}{
			"goddessWoman":  map[string]interface{}{"prediction": "客胜", "homeScore": 55.5, "guestScore": 62.5, "probabilities": map[string]interface{}{"home": 27., "draw": 34., "away": 39.}},
			"platform":      map[string]interface{}{"bookmaker": map[string]interface{}{"outcome": "away", "score": "1:3", "goal": map[string]interface{}{"total": 4}}, "platform": map[string]interface{}{"outcome": "home", "score": "2:1"}, "goals": map[string]interface{}{"main": map[string]interface{}{"home": 1.7, "guest": 1.8}}, "warningRows": []interface{}{map[string]interface{}{"value": "投注热度过热；历史约55%命中"}}, "goalBalanceAlertRows": []interface{}{map[string]interface{}{"label": "回归小球警示", "value": "历史默认0"}}, "evilCult": map[string]interface{}{"prediction": map[string]interface{}{"outcome": "away", "score": "0:2", "secondPassReversed": true}, "scores": map[string]interface{}{"over": 70.5, "under": 40.1}}},
			"roiSimulation": map[string]interface{}{"markets": []interface{}{map[string]interface{}{"key": "sportterySim", "simulated": true, "oddsAvailable": true, "bookmakerByOutcome": []interface{}{map[string]interface{}{"outcome": "away", "odds": 1.71, "available": true, "retailShare": 71., "bookmakerRoi": -21.41, "bookmakerProfit": -10705000., "totalStake": 50000000.}}}, map[string]interface{}{"key": "sportteryRqspfSim", "simulated": true, "goal": "+1", "oddsAvailable": true, "bookmakerByOutcome": []interface{}{map[string]interface{}{"outcome": "away", "odds": 2.87, "available": true, "retailShare": 30.7, "bookmakerRoi": 11.89}}}}},
		},
	}
}

func extractFullTest(t *testing.T, row map[string]interface{}) map[string]float64 {
	t.Helper()
	data, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	features, err := ExtractFeatures(data)
	if err != nil {
		t.Fatal(err)
	}
	return features
}

func TestComprehensiveAllPageModulesHaveUsableWhitelistedInputs(t *testing.T) {
	f := extractFullTest(t, fullTestInput())
	for key, want := range map[string]float64{"full.heat.asian.away": 72.52, "full.heat.goals.over": 65.3, "full.goddess.probabilities.away": 39, "full.platform.bookmaker.outcome.away": 1, "full.platform.platform.outcome.home": 1, "full.platform.bookmaker.score.total": 4, "full.platform.goals.main.home": 1.7, "full.evil.prediction.score.total": 2, "full.evil.prediction.secondPassReversed": 1, "full.warning.overheated": 1, "full.profit.sportterySim.away.roi": -21.41, "full.profit.sportterySim.simulated": 1, "full.profit.sportteryRqspfSim.away.roi": 11.89, "full.trade.jyykRqspf.ay": -45.6} {
		got, ok := f[key]
		if !ok || got != want {
			t.Errorf("missing or wrong module input %s: got=%v available=%v want=%v", key, got, ok, want)
		}
	}
	if _, ok := f["full.summary.historyGoalAverage"]; ok {
		t.Fatal("missing history became a numeric zero")
	}
	if f["full.warning.goalReversion"] != 0 {
		t.Fatal("history-dependent reversion warning used missing history default")
	}
	seen := map[string]bool{}
	for _, m := range Metadata() {
		if seen[m.Name] {
			t.Fatal("duplicate metadata", m.Name)
		}
		seen[m.Name] = true
		if m.Module == "goddess" && !m.Synthetic {
			t.Fatal("goddess metadata lost synthetic provenance")
		}
	}
	for key := range f {
		if !seen[key] {
			t.Fatal("unregistered input", key)
		}
	}
}

func TestComprehensiveIgnoresTargetsIdentityAndArbitraryFields(t *testing.T) {
	r := fullTestInput()
	before := extractFullTest(t, r)
	r["result"] = 2
	r["homeScore"] = 19
	r["awayScore"] = 23
	r["status"] = 999
	r["predictionHit"] = true
	r["home"] = "another name"
	r["guest"] = "other"
	r["matchId"] = "other id"
	r["fullAnalysis"].(map[string]interface{})["currentScore"] = []int{19, 23}
	r["fullAnalysis"].(map[string]interface{})["unknownFutureModule"] = 99
	if !reflect.DeepEqual(before, extractFullTest(t, r)) {
		t.Fatal("target, identity or unregistered arbitrary field changed features")
	}
	meta := Metadata()
	meta[0].Name = "changed"
	if Metadata()[0].Name == "changed" {
		t.Fatal("metadata returned mutable shared state")
	}
}

func TestComprehensiveUnsafeDerivedHistoryIsMissing(t *testing.T) {
	r := fullTestInput()
	r["history"].(map[string]interface{})["usedRecentTargetId"] = 1
	f := extractFullTest(t, r)
	for _, key := range []string{"full.platform.bookmaker.outcome.away", "full.goddess.probabilities.away", "full.evil.prediction.score.total", "full.profit.sportterySim.away.roi", "full.heat.goals.over"} {
		if _, ok := f[key]; ok {
			t.Error("unsafe derived input retained", key)
		}
	}
	if f["full.available.historySafe"] != 0 {
		t.Fatal("unsafe history was marked available")
	}
	if f["full.trade.jyykSpf.ay"] != -19.34 || f["full.heat.asian.away"] != 72.52 {
		t.Fatal("independent raw market observations were erased with derived history")
	}
}
