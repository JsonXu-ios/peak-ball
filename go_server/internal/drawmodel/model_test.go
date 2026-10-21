package drawmodel

import (
	"fmt"
	"math"
	"testing"
	"time"
)

func testTime() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }

func testHistory(n int) []Match {
	end := testTime().Add(-7 * 24 * time.Hour)
	history := make([]Match, n)
	for i := range history {
		history[i] = Match{ID: fmt.Sprint(i), LeagueID: "league", HomeTeamID: "home", AwayTeamID: "away", HomeTeamName: "Home", AwayTeamName: "Away", Kickoff: end.Add(time.Duration(-i) * 24 * time.Hour), Completed: true, HomeGoals: i % 3, AwayGoals: (i + 1) % 3}
	}
	return history
}

func testInput() Input {
	return Input{Target: Match{ID: "target", LeagueID: "league", HomeTeamID: "home", AwayTeamID: "away", HomeTeamName: "Home", AwayTeamName: "Away", Kickoff: testTime().Add(24 * time.Hour)}, AsOf: testTime(), History: testHistory(100)}
}

func TestPoissonDrawAndDixonColesMass(t *testing.T) {
	low, _ := scoreDistribution(.9, .9, 0)
	high, _ := scoreDistribution(1.8, 1.8, 0)
	if math.Abs(low-.328871) > 1e-5 || low <= high {
		t.Fatalf("unexpected analytic draw probabilities: %.8f %.8f", low, high)
	}
	lh, la, rho := 1.3, .8, -.12
	mass, draw := 0.0, 0.0
	for h := 0; h < 30; h++ {
		for a := 0; a < 30; a++ {
			hf, _ := math.Lgamma(float64(h + 1))
			af, _ := math.Lgamma(float64(a + 1))
			p := math.Exp(-lh+float64(h)*math.Log(lh)-hf-la+float64(a)*math.Log(la)-af) * tau(h, a, lh, la, rho)
			if p < 0 {
				t.Fatal("negative score probability")
			}
			mass += p
			if h == a {
				draw += p
			}
		}
	}
	got, _ := scoreDistribution(lh, la, rho)
	if math.Abs(mass-1) > 1e-12 || math.Abs(got-draw) > 1e-12 {
		t.Fatalf("DC correction lost mass: mass=%v draw=%v got=%v", mass, draw, got)
	}
}

func TestDixonColesIsFittedOnlyWithSufficientEvidence(t *testing.T) {
	history := testHistory(120)
	for i := range history {
		history[i].HomeGoals, history[i].AwayGoals = 1, 1
	}
	m := fitStrength(history, testTime())
	rho, fitted := m.fitRho()
	if !fitted || rho >= -.1 {
		t.Fatalf("draw-heavy outcomes did not fit a low-score correction: rho=%v fitted=%v", rho, fitted)
	}
	if rho, fitted := fitStrength(history[:20], testTime()).fitRho(); fitted || rho != 0 {
		t.Fatal("small sample fabricated a fitted rho")
	}
}

func TestPredictionIgnoresFutureIncompleteAndDuplicateResults(t *testing.T) {
	in := testInput()
	before := Predict(in)
	future := in.History[0]
	future.ID = "future"
	future.Kickoff = in.AsOf.Add(time.Hour)
	future.HomeGoals = 20
	unavailable := in.History[0]
	unavailable.ID = "ongoing"
	unavailable.Kickoff = in.AsOf.Add(-time.Hour)
	unavailable.HomeGoals = 20
	incomplete := in.History[0]
	incomplete.ID = "incomplete"
	incomplete.Completed = false
	incomplete.HomeGoals = 20
	self := in.Target
	self.Completed = true
	self.Kickoff = in.AsOf.Add(-24 * time.Hour)
	self.HomeGoals = 20
	invalid := in.History[0]
	invalid.ID = "invalid"
	invalid.HomeGoals = -1
	in.History = append(in.History, future, unavailable, incomplete, self, invalid, in.History[0])
	after := Predict(in)
	if !before.Available || before.HomeLambda != after.HomeLambda || before.AwayLambda != after.AwayLambda || before.Probability != after.Probability {
		t.Fatalf("future/duplicate fixture changed prediction: before=%+v after=%+v", before, after)
	}
	if before.LeagueBaseline.N != after.LeagueBaseline.N {
		t.Fatal("invalid fixtures entered baseline")
	}
}

func TestRecentTenOverallAndVenueAreIndependent(t *testing.T) {
	in := testInput()
	in.History = nil
	for i := 0; i < 40; i++ {
		m := Match{ID: fmt.Sprint(i), LeagueID: "league", HomeTeamID: "home", AwayTeamID: "opponent", Kickoff: in.AsOf.Add(time.Duration(-i-1) * 24 * time.Hour), Completed: true, HomeGoals: 1, AwayGoals: 1}
		if i%2 != 0 {
			m.HomeTeamID, m.AwayTeamID = "opponent", "home"
			m.HomeGoals, m.AwayGoals = 2, 0
		}
		in.History = append(in.History, m)
	}
	r := Predict(in)
	if r.HomeOverall.N != 10 || r.HomeOverall.Draws != 5 || r.HomeVenue.N != 10 || r.HomeVenue.Draws != 10 {
		t.Fatalf("wrong form windows: overall=%+v venue=%+v", r.HomeOverall, r.HomeVenue)
	}
	if r.HomeOverall.GoalsFor != .5 || r.HomeOverall.GoalsAgainst != 1.5 || r.HomeVenue.LowScoreRate != 1 {
		t.Fatalf("form values are not averages: %+v", r.HomeOverall)
	}
	if r.HomeVenue.MatchIDs[9] != "18" {
		t.Fatalf("venue window was sliced out of overall ten: %+v", r.HomeVenue.MatchIDs)
	}
}

func TestIdentityAndMarketBoundaries(t *testing.T) {
	if yes, _ := sameTeam("1", "Same", "2", "Same"); yes {
		t.Fatal("known distinct IDs were name-matched")
	}
	if yes, fallback := sameTeam("", " Home ", "1", "home"); !yes || !fallback {
		t.Fatal("explicit name fallback missing")
	}
	in := testInput()
	in.Odds = &Odds{Home: 2, Draw: 3, Away: 4, CapturedAt: in.AsOf.Add(-time.Hour)}
	r := Predict(in)
	if r.MarketProbability == nil || math.Abs(*r.MarketProbability-4.0/13) > 1e-12 {
		t.Fatal("three-way overround was not removed")
	}
	in.Odds.CapturedAt = in.AsOf.Add(time.Nanosecond)
	if Predict(in).MarketProbability != nil {
		t.Fatal("future odds entered prediction")
	}
	in.Odds.CapturedAt = time.Time{}
	if Predict(in).MarketProbability != nil {
		t.Fatal("undated odds entered prediction")
	}
	if _, ok := MarketDrawProbability(Odds{Home: math.Inf(1), Draw: 3, Away: 4}); ok {
		t.Fatal("non-finite odds accepted")
	}
	in.History = nil
	r = Predict(in)
	if r.Available || r.Probability != 0 || r.Model.Calibrated {
		t.Fatal("empty history generated a fabricated prediction")
	}
}

func trainingExamples() []TrainingExample {
	history := testHistory(25)
	start := testTime().Add(24 * time.Hour)
	examples := make([]TrainingExample, MinTrainingSamples)
	for i := range examples {
		kickoff := start.Add(time.Duration(i) * 24 * time.Hour)
		draw := i%2 == 0
		if i >= 320 {
			draw = i%4 == 0
		}
		examples[i] = TrainingExample{Target: Match{ID: fmt.Sprintf("train-%d", i), LeagueID: "league", HomeTeamID: "home", AwayTeamID: "away", Kickoff: kickoff}, AsOf: kickoff.Add(-time.Hour), History: history, Draw: draw}
	}
	return examples
}

func TestFitUsesLaterCalibrationAndRejectsFutureArtifact(t *testing.T) {
	examples := trainingExamples()
	a := Fit(examples)
	if !a.Fitted || !a.Calibrated || a.TrainingSamples != 320 || a.CalibrationSamples != 80 {
		t.Fatalf("fit failed: %+v", a)
	}
	// All feature vectors are equal: the earlier balanced labels fit p=0.5;
	// only the later 25%% outcomes may move the separate calibration intercept.
	if math.Abs(a.Intercept) > 1e-7 {
		t.Fatalf("calibration labels contaminated training intercept: %v", a.Intercept)
	}
	wantCutoff := examples[len(examples)-1].Target.Kickoff.Add(2 * time.Hour)
	if !a.TrainingCutoff.Equal(wantCutoff) {
		t.Fatalf("training cutoff does not include observed calibration results: %v", a.TrainingCutoff)
	}
	in := testInput()
	in.AsOf = wantCutoff.Add(time.Hour)
	in.Target.Kickoff = in.AsOf.Add(time.Hour)
	in.History = examples[0].History
	in.Artifact = &a
	r := Predict(in)
	if !r.Model.Calibrated || !r.Model.FusionFitted || math.Abs(r.Probability-.25) > .01 {
		t.Fatalf("independent calibration not applied: %+v", r)
	}
	in.AsOf = wantCutoff.Add(-time.Minute)
	in.Target.Kickoff = in.AsOf.Add(time.Hour)
	r = Predict(in)
	if r.Model.FusionFitted || r.Probability != r.ScoreModelProbability {
		t.Fatal("future labels in artifact entered a historical prediction")
	}
	a.Coefficients[0] = math.NaN()
	in.AsOf = wantCutoff.Add(time.Hour)
	in.Target.Kickoff = in.AsOf.Add(time.Hour)
	if Predict(in).Model.FusionFitted {
		t.Fatal("non-finite artifact applied")
	}
}

func TestFitInsufficientAndTemporalPurge(t *testing.T) {
	if a := Fit(trainingExamples()[:100]); a.Fitted || a.Calibrated {
		t.Fatal("insufficient fit claimed calibration")
	}
	examples := trainingExamples()
	for i := range examples {
		examples[i].Target.Kickoff = testTime().Add(time.Duration(i+100) * 15 * time.Minute)
		examples[i].AsOf = examples[i].Target.Kickoff.Add(-24 * time.Hour)
	}
	a := Fit(examples)
	if a.Fitted || a.TrainingSamples >= minFitSamples {
		t.Fatalf("overlapping training labels were not purged: %+v", a)
	}
}
