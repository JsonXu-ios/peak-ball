package handlers

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"go_server/internal/drawmodel"
	"go_server/models"

	"gorm.io/datatypes"
)

func drawTestPtr[T any](value T) *T { return &value }

func drawTestSourceZone(t *testing.T, name string) {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	previous := drawSourceLocation
	drawSourceLocation = location
	t.Cleanup(func() { drawSourceLocation = previous })
}

func drawTestClose(t *testing.T, label string, got *float64, want float64) {
	t.Helper()
	if got == nil || math.Abs(*got-want) > 1e-10 {
		t.Fatalf("%s = %v, want %g", label, got, want)
	}
}

func TestDrawStatsEmptyDoesNotInventResults(t *testing.T) {
	stats := buildDrawStats(nil)
	if stats.HitRate != nil || stats.Coverage != nil || stats.Brier != nil || stats.MarketBrier != nil || stats.BaselineBrier != nil || stats.PairedModelBrier != nil {
		t.Fatal("empty sample must expose null rates and scores")
	}
	if stats.CalibrationBins == nil || len(stats.CalibrationBins) != 0 {
		t.Fatal("empty calibration must be []")
	}
}

func TestDrawStatsUsesFrozenCandidatesAndPairedMarketSubset(t *testing.T) {
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	rows := []models.DrawPrediction{
		{PredictionAt: asOf, Kickoff: asOf.Add(time.Hour), Probability: .4, BaselineProbability: .3, MarketProbability: drawTestPtr(.35), Candidate: true, Settled: true, IsDraw: drawTestPtr(true)},
		{PredictionAt: asOf, Kickoff: asOf.Add(time.Hour), Probability: .2, BaselineProbability: .3, Candidate: true, Settled: true, IsDraw: drawTestPtr(false)},
		{PredictionAt: asOf, Kickoff: asOf.Add(time.Hour), Probability: .3, BaselineProbability: .3, MarketProbability: drawTestPtr(.25), Settled: true, IsDraw: drawTestPtr(true)},
		{PredictionAt: asOf, Kickoff: asOf.Add(time.Hour), Probability: .9, Candidate: true},
	}
	stats := buildDrawStats(rows)
	if stats.Predictions != 4 || stats.Settled != 3 || stats.Candidates != 3 || stats.CandidateSettled != 2 || stats.CandidateHits != 1 || stats.MarketSamples != 2 {
		t.Fatalf("wrong denominators: %+v", stats)
	}
	drawTestClose(t, "hitRate", stats.HitRate, 50)
	drawTestClose(t, "coverage", stats.Coverage, 75)
	drawTestClose(t, "brier", stats.Brier, (.36+.04+.49)/3)
	drawTestClose(t, "baselineBrier", stats.BaselineBrier, (.49+.09+.49)/3)
	drawTestClose(t, "marketBrier", stats.MarketBrier, (.4225+.5625)/2)
	drawTestClose(t, "pairedModelBrier", stats.PairedModelBrier, (.36+.49)/2)
	if len(stats.CalibrationBins) != 3 || stats.CalibrationBins[0].Count != 1 || stats.CalibrationBins[2].Actual != 100 {
		t.Fatalf("wrong calibration bins: %+v", stats.CalibrationBins)
	}
}

func TestDrawStatsRejectsLateForecastAndHandlesCertainProbability(t *testing.T) {
	kickoff := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	rows := []models.DrawPrediction{
		{PredictionAt: kickoff, Kickoff: kickoff, Probability: .9, Candidate: true, Settled: true, IsDraw: drawTestPtr(true)},
		{PredictionAt: kickoff.Add(-time.Hour), Kickoff: kickoff, Probability: 1, Settled: true, IsDraw: drawTestPtr(true)},
	}
	stats := buildDrawStats(rows)
	if stats.Settled != 1 || stats.CandidateSettled != 0 || stats.HitRate != nil || len(stats.CalibrationBins) != 1 || stats.CalibrationBins[0].From != 90 || stats.CalibrationBins[0].To != 100 {
		t.Fatalf("late forecast or probability boundary mishandled: %+v", stats)
	}
	drawTestClose(t, "brier", stats.Brier, 0)
}

func drawTrainingFixture(t *testing.T) (models.DrawPrediction, models.DrawFeatureSnapshot, drawmodel.Input) {
	t.Helper()
	asOf := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	kickoff, settledAt := asOf.Add(time.Hour), asOf.Add(3*time.Hour)
	input := drawmodel.Input{Target: drawmodel.Match{ID: "fixture", Kickoff: kickoff}, AsOf: asOf, History: []drawmodel.Match{}}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := models.DrawFeatureSnapshot{ID: 1, MatchID: "fixture", AsOf: asOf, Kickoff: kickoff, Input: datatypes.JSON(encoded)}
	prediction := models.DrawPrediction{
		ID: 1, MatchID: "fixture", SnapshotID: 1, PredictionAt: asOf, Kickoff: kickoff, Settled: true,
		IsDraw: drawTestPtr(true), HomeScore: drawTestPtr(1), AwayScore: drawTestPtr(1), SettledAt: &settledAt,
	}
	return prediction, snapshot, input
}

func TestDrawTrainingUsesImmutablePreMatchInputAndObservedResultTime(t *testing.T) {
	prediction, snapshot, _ := drawTrainingFixture(t)
	examples := drawTrainingExamples([]models.DrawPrediction{prediction, prediction}, map[uint64]models.DrawFeatureSnapshot{snapshot.ID: snapshot})
	if len(examples) != 1 || !examples[0].Draw || !examples[0].Target.Completed || examples[0].Target.HomeGoals != 1 || !examples[0].Target.CompletedAt.Equal(*prediction.SettledAt) {
		t.Fatalf("incorrect snapshot label or deduplication: %+v", examples)
	}
	var original drawmodel.Input
	if err := json.Unmarshal(snapshot.Input, &original); err != nil || original.Target.Completed || original.Target.HomeGoals != 0 {
		t.Fatal("training must not change original pre-match snapshot")
	}
}

func TestDrawTrainingUsesSourceResultAvailabilityBeforeDelayedReconciliation(t *testing.T) {
	prediction, snapshot, _ := drawTrainingFixture(t)
	observed := prediction.Kickoff.Add(2 * time.Hour)
	prediction.ResultAvailableAt = &observed
	prediction.SettledAt = drawTestPtr(prediction.Kickoff.Add(7 * 24 * time.Hour))
	examples := drawTrainingExamples([]models.DrawPrediction{prediction}, map[uint64]models.DrawFeatureSnapshot{snapshot.ID: snapshot})
	if len(examples) != 1 || !examples[0].Target.CompletedAt.Equal(observed) {
		t.Fatal("delayed reconciliation must preserve the source's earlier known result time")
	}
}

func TestDrawObservedResultTimeIsConservativeAndNeverFuture(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	kickoff := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	rawSourceKickoff := time.Date(2026, 10, 20, 16, 0, 0, 0, time.UTC)
	now := kickoff.Add(7 * 24 * time.Hour)
	for _, test := range []struct {
		name      string
		updatedAt time.Time
		want      time.Time
	}{
		{"source updated after completion", kickoff.Add(3 * time.Hour), kickoff.Add(3 * time.Hour)},
		{"stale source clamped to match duration", kickoff.Add(time.Minute), kickoff.Add(2 * time.Hour)},
		{"missing timestamp uses reconciliation", time.Time{}, now},
		{"future source timestamp uses reconciliation", now.Add(time.Hour), now},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := drawObservedResultTime(models.Money{UpdatedAt: test.updatedAt, MatchTime: rawSourceKickoff}, kickoff, now); !got.Equal(test.want) {
				t.Fatalf("available time=%v, want %v", got, test.want)
			}
		})
	}
	if got := drawObservedResultTime(models.Money{UpdatedAt: kickoff.Add(90 * time.Minute), MatchTime: rawSourceKickoff}, kickoff, kickoff.Add(100*time.Minute)); got.After(kickoff.Add(100 * time.Minute)) {
		t.Fatal("a completed result cannot be assigned a future availability time")
	}
}

func TestDrawObservedResultTimeConvertsRescheduledSourceWallClock(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	oldKickoff := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	// Source has moved the match to 18:00 Beijing time, or 10:00 UTC.
	rawRescheduled := time.Date(2026, 10, 20, 18, 0, 0, 0, time.UTC)
	now := oldKickoff.Add(24 * time.Hour)
	match := models.Money{MatchTime: rawRescheduled, UpdatedAt: oldKickoff.Add(time.Minute)}
	if got, want := drawObservedResultTime(match, oldKickoff, now), oldKickoff.Add(4*time.Hour); !got.Equal(want) {
		t.Fatalf("result floor must follow converted rescheduled kickoff: got %v, want %v", got, want)
	}
	match.MatchTime = time.Time{}
	if got, want := drawObservedResultTime(match, oldKickoff, now), oldKickoff.Add(2*time.Hour); !got.Equal(want) {
		t.Fatalf("missing source kickoff must preserve prediction kickoff: got %v, want %v", got, want)
	}
}

func TestDrawTrainingRejectsUnverifiableChronologyAndOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*models.DrawPrediction, *models.DrawFeatureSnapshot, *drawmodel.Input)
	}{
		{"late prediction", func(p *models.DrawPrediction, _ *models.DrawFeatureSnapshot, _ *drawmodel.Input) {
			p.PredictionAt = p.Kickoff
		}},
		{"snapshot from another match", func(_ *models.DrawPrediction, s *models.DrawFeatureSnapshot, _ *drawmodel.Input) { s.MatchID = "other" }},
		{"target already complete", func(_ *models.DrawPrediction, _ *models.DrawFeatureSnapshot, in *drawmodel.Input) {
			in.Target.Completed = true
		}},
		{"input timestamp mismatch", func(_ *models.DrawPrediction, _ *models.DrawFeatureSnapshot, in *drawmodel.Input) {
			in.AsOf = in.AsOf.Add(time.Minute)
		}},
		{"kickoff mismatch", func(_ *models.DrawPrediction, _ *models.DrawFeatureSnapshot, in *drawmodel.Input) {
			in.Target.Kickoff = in.Target.Kickoff.Add(time.Hour)
		}},
		{"unknown observed time", func(p *models.DrawPrediction, _ *models.DrawFeatureSnapshot, _ *drawmodel.Input) { p.SettledAt = nil }},
		{"result observed before kickoff", func(p *models.DrawPrediction, _ *models.DrawFeatureSnapshot, _ *drawmodel.Input) {
			p.SettledAt = drawTestPtr(p.Kickoff.Add(-time.Second))
		}},
		{"draw label disagrees with goals", func(p *models.DrawPrediction, _ *models.DrawFeatureSnapshot, _ *drawmodel.Input) {
			p.AwayScore = drawTestPtr(2)
		}},
		{"source result before kickoff", func(p *models.DrawPrediction, _ *models.DrawFeatureSnapshot, _ *drawmodel.Input) {
			p.ResultAvailableAt = drawTestPtr(p.Kickoff.Add(-time.Second))
		}},
		{"source result after reconciliation", func(p *models.DrawPrediction, _ *models.DrawFeatureSnapshot, _ *drawmodel.Input) {
			p.ResultAvailableAt = drawTestPtr(p.SettledAt.Add(time.Hour))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			prediction, snapshot, input := drawTrainingFixture(t)
			test.mutate(&prediction, &snapshot, &input)
			encoded, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Input = datatypes.JSON(encoded)
			examples := drawTrainingExamples([]models.DrawPrediction{prediction}, map[uint64]models.DrawFeatureSnapshot{snapshot.ID: snapshot})
			if len(examples) != 0 {
				t.Fatal("unverifiable snapshot must not enter training")
			}
		})
	}
}

func TestDrawTrainingAcceptsDatabaseTimestampPrecision(t *testing.T) {
	prediction, snapshot, input := drawTrainingFixture(t)
	input.AsOf = input.AsOf.Add(123456789 * time.Nanosecond)
	prediction.PredictionAt = input.AsOf.Truncate(time.Millisecond)
	snapshot.AsOf = prediction.PredictionAt
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Input = datatypes.JSON(encoded)
	if got := drawTrainingExamples([]models.DrawPrediction{prediction}, map[uint64]models.DrawFeatureSnapshot{snapshot.ID: snapshot}); len(got) != 1 {
		t.Fatal("database millisecond precision must not discard a genuine pre-match snapshot")
	}
}

func TestDrawRegulationResultRejectsAmbiguousExtraTimeAndShootouts(t *testing.T) {
	for _, match := range []models.Money{
		{DisplayState: "完场", HomeScore: 2, GuestScore: 1, HomeOtScore: 1},
		{DisplayState: "完场", HomeScore: 1, GuestScore: 1, HomeOtPenalty: 5, GuestOtPenalty: 4},
		{DisplayState: "加时完场", HomeScore: 2, GuestScore: 1},
		{DisplayState: "点球结束", HomeScore: 1, GuestScore: 1},
		{DisplayState: "取消", HomeScore: 0, GuestScore: 0, Status: 5},
	} {
		if _, _, ok := drawRegulationResult(match); ok {
			t.Fatalf("ambiguous match must be excluded: %+v", match)
		}
	}
	if home, away, ok := drawRegulationResult(models.Money{DisplayState: "完场", HomeScore: 1, GuestScore: 1}); !ok || home != 1 || away != 1 {
		t.Fatal("normal completed 90-minute draw should be usable")
	}
}
