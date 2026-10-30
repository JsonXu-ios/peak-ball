package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"go_server/internal/drawmodeldata"
	"peakball/researchstore"
)

type drawResearchFakeBackend struct {
	record              *drawResearchStoredRecord
	lookupErr, writeErr error
	lookups, writes     int
}

func (b *drawResearchFakeBackend) lookup(context.Context, drawResearchKey) (*drawResearchStoredRecord, error) {
	b.lookups++
	if b.lookupErr != nil {
		return nil, b.lookupErr
	}
	if b.record == nil {
		return nil, sql.ErrNoRows
	}
	return b.record, nil
}
func (b *drawResearchFakeBackend) write(_ context.Context, item drawObservationItem, row drawFeatureStudyRow, body []byte, key drawResearchKey, signal *drawObservationFrozenSignal, now time.Time) error {
	b.writes++
	if b.writeErr != nil {
		return b.writeErr
	}
	b.record = drawResearchFakeRecord(key, body, signal, now)
	return nil
}

func drawResearchFakeInput() (drawObservationItem, *drawFeatureStudyRow, time.Time) {
	now := time.Date(2026, 10, 29, 16, 0, 0, 0, time.UTC)
	kickoff := now.Add(-24 * time.Hour)
	item := drawObservationItem{MatchID: "498177797", JingcaiID: "202610246021", MatchTime: kickoff, Date: "2026-10-28", Home: "主队", Guest: "客队", HomeRecent: drawObservationRecent{Sample: 10, Draws: 3}, AwayRecent: drawObservationRecent{Sample: 10, Draws: 2}, HeadToHead: drawObservationRecent{Sample: 5}, CompanyQuotes: []drawObservationCompanyQuotes{drawSecondaryGuardQuoteFixture()}}
	item.ScoreInputs = &drawScoreInputs{MatchID: item.MatchID, Source: "sporttery_score_odds", Quotes: []drawScoreQuote{{Home: 1, Away: 1, Odds: 6, SourceField: "sd11"}, {Home: 2, Away: 2, Odds: 5, SourceField: "sd22"}}}
	input := &drawFeatureStudyRow{MatchID: item.MatchID, MatchTime: kickoff, Date: item.Date, JingcaiID: item.JingcaiID, Home: item.Home, Guest: item.Guest, CompanyQuotes: item.CompanyQuotes, ScoreInputs: item.ScoreInputs, Opening: drawFeatureStudyPhase{Average: []float64{2.3, 3.1, 2.9}}}
	return item, input, now
}

func drawResearchFakeSignal() *drawObservationFrozenSignal {
	score := .323789458480385
	return &drawObservationFrozenSignal{ModelVersion: drawmodeldata.Version(), State: "selected", Score: &score, Threshold: drawmodeldata.Threshold(), ObservedInputs: 202, UsedInputs: 202}
}

func drawResearchFakeRecord(key drawResearchKey, body []byte, signal *drawObservationFrozenSignal, now time.Time) *drawResearchStoredRecord {
	var input drawFeatureStudyRow
	if err := json.Unmarshal(body, &input); err != nil {
		panic(err)
	}
	item := researchTakeoverItem(input)
	item.FrozenDrawSignal = signal
	scores := drawObservationScoreRecommendation(item)
	p := researchstore.Prediction{SnapshotID: 41, MatchID: key.matchID, ModelVersion: drawmodeldata.Version(), RuleVersion: drawObservationSecondaryVersion + "+" + drawObservationProfitSupportVersion, PredictedAt: now, State: signal.State, Score: signal.Score, Threshold: &signal.Threshold, Secondary: drawObservationSecondaryRetained(item), ScoreMethod: drawScoreRecommendationVersion, Scorelines: researchJSON(scores.Scores), Payload: researchJSON(map[string]any{"original": signal, "secondaryDecision": drawObservationSecondaryDecision(item), "scoreRecommendation": scores, "provenance": key.provenance, "probabilityCalibrated": false})}
	p.Payload, _ = drawResearchCanonicalJSON(p.Payload)
	p.Scorelines, _ = drawResearchCanonicalJSON(p.Scorelines)
	copy := p
	copy.PredictedAt = time.Time{}
	r := &drawResearchStoredRecord{id: 81, snapshotID: 41, matchID: key.matchID, featureVersion: researchFeatureVersion, revision: key.revision, provenance: key.provenance, kickoff: key.kickoff, asOf: key.kickoff, prediction: p, predictionHash: researchstore.HashBytes(researchJSON(copy)), input: append([]byte(nil), body...), inputHash: researchstore.HashBytes(body)}
	if key.provenance == "live" {
		r.asOf = now
		v := now
		r.knownBy = &v
	}
	r.inputObservedAt, r.inputProvenance = now, key.provenance
	return r
}

func TestResearchTakeoverReadsVerifiedArchiveWithoutRunningLegacy(t *testing.T) {
	item, input, now := drawResearchFakeInput()
	_, body, key, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		t.Fatal(err)
	}
	b := &drawResearchFakeBackend{record: drawResearchFakeRecord(key, body, drawResearchFakeSignal(), now)}
	calls := 0
	signal, decision := resolveDrawResearchSignalWithBackend(context.Background(), b, item, input, func() *drawObservationFrozenSignal { calls++; return drawResearchFakeSignal() }, now)
	if calls != 0 || b.writes != 0 || b.lookups != 1 || decision.Source != "research" || decision.SnapshotID != 41 || decision.PredictionID != 81 || !drawResearchSignalsEqual(signal, drawResearchFakeSignal()) {
		t.Fatalf("archive was not the source: calls=%d backend=%#v decision=%#v signal=%#v", calls, b, decision, signal)
	}
	var picks []drawRecommendedScore
	if json.Unmarshal(b.record.prediction.Scorelines, &picks) != nil || !b.record.prediction.Secondary || !reflect.DeepEqual(picks, []drawRecommendedScore{{Home: 2, Away: 2}, {Home: 1, Away: 1}}) {
		t.Fatalf("fake input must exercise a retained dynamic recommendation: %s", b.record.prediction.Scorelines)
	}
}

func TestResearchTakeoverMissSealsThenReadsBack(t *testing.T) {
	item, input, now := drawResearchFakeInput()
	b := &drawResearchFakeBackend{}
	calls := 0
	signal, decision := resolveDrawResearchSignalWithBackend(context.Background(), b, item, input, func() *drawObservationFrozenSignal { calls++; return drawResearchFakeSignal() }, now)
	if calls != 1 || b.writes != 1 || b.lookups != 2 || decision.Source != "research_write_through" || !drawResearchSignalsEqual(signal, drawResearchFakeSignal()) {
		t.Fatalf("write-through flow failed: calls=%d backend=%#v decision=%#v", calls, b, decision)
	}
	_, again := resolveDrawResearchSignalWithBackend(context.Background(), b, item, input, func() *drawObservationFrozenSignal { calls++; return drawResearchFakeSignal() }, now)
	if calls != 1 || again.Source != "research" || b.writes != 1 {
		t.Fatalf("same revision recalculated: calls=%d writes=%d decision=%#v", calls, b.writes, again)
	}
}

func TestResearchTakeoverFallbackNeverLosesOriginalDecision(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*drawResearchFakeBackend, drawResearchKey, []byte, time.Time)
	}{
		{"read unavailable", func(b *drawResearchFakeBackend, _ drawResearchKey, _ []byte, _ time.Time) {
			b.lookupErr = errors.New("offline")
		}},
		{"write unavailable", func(b *drawResearchFakeBackend, _ drawResearchKey, _ []byte, _ time.Time) {
			b.writeErr = errors.New("offline")
		}},
		{"wrong input revision", func(b *drawResearchFakeBackend, k drawResearchKey, body []byte, n time.Time) {
			b.record = drawResearchFakeRecord(k, body, drawResearchFakeSignal(), n)
			b.record.revision = "other-input"
		}},
		{"tampered source bytes", func(b *drawResearchFakeBackend, k drawResearchKey, body []byte, n time.Time) {
			b.record = drawResearchFakeRecord(k, body, drawResearchFakeSignal(), n)
			b.record.input = []byte(`{"different":1}`)
		}},
		{"tampered plausible score", func(b *drawResearchFakeBackend, k drawResearchKey, body []byte, n time.Time) {
			b.record = drawResearchFakeRecord(k, body, drawResearchFakeSignal(), n)
			v := .8
			b.record.prediction.Score = &v
		}},
		{"wrong secondary", func(b *drawResearchFakeBackend, k drawResearchKey, body []byte, n time.Time) {
			b.record = drawResearchFakeRecord(k, body, drawResearchFakeSignal(), n)
			b.record.prediction.Secondary = false
		}},
		{"old rule", func(b *drawResearchFakeBackend, k drawResearchKey, body []byte, n time.Time) {
			b.record = drawResearchFakeRecord(k, body, drawResearchFakeSignal(), n)
			b.record.prediction.RuleVersion = "secondary-v1+profit-support-v1"
			p := b.record.prediction
			p.PredictedAt = time.Time{}
			b.record.predictionHash = researchstore.HashBytes(researchJSON(p))
		}},
		{"old score method", func(b *drawResearchFakeBackend, k drawResearchKey, body []byte, n time.Time) {
			b.record = drawResearchFakeRecord(k, body, drawResearchFakeSignal(), n)
			b.record.prediction.ScoreMethod = "draw-score-baseline-v1"
			p := b.record.prediction
			p.PredictedAt = time.Time{}
			b.record.predictionHash = researchstore.HashBytes(researchJSON(p))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item, input, now := drawResearchFakeInput()
			_, body, key, _ := prepareDrawResearchInput(item, input, now)
			b := &drawResearchFakeBackend{}
			tc.setup(b, key, body, now)
			calls := 0
			got, d := resolveDrawResearchSignalWithBackend(context.Background(), b, item, input, func() *drawObservationFrozenSignal { calls++; return drawResearchFakeSignal() }, now)
			if calls != 1 || d.Source != "legacy_fallback" || d.Reason == "" || !drawResearchSignalsEqual(got, drawResearchFakeSignal()) {
				t.Fatalf("fallback changed result: calls=%d decision=%#v signal=%#v", calls, d, got)
			}
			if b.record != nil && b.writes != 0 {
				t.Fatal("corrupt immutable archive was overwritten")
			}
		})
	}
}

func TestResearchTakeoverRestoresJSONFloatFromDoubleBeforeFingerprint(t *testing.T) {
	item, input, now := drawResearchFakeInput()
	_, body, key, _ := prepareDrawResearchInput(item, input, now)
	r := drawResearchFakeRecord(key, body, drawResearchFakeSignal(), now)
	var payload map[string]any
	if err := json.Unmarshal(r.prediction.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	original := payload["original"].(map[string]any)
	original["score"] = math.Nextafter(*r.prediction.Score, 0)
	original["threshold"] = math.Nextafter(*r.prediction.Threshold, 0)
	r.prediction.Payload = researchJSON(payload)
	got, err := validateDrawResearchRecord(r, key, body)
	if err != nil || !drawResearchSignalsEqual(got, drawResearchFakeSignal()) {
		t.Fatalf("JSON formatting changed authoritative score: %v %#v", err, got)
	}
}

func TestResearchTakeoverDoesNotReadTargetResultOrUseAnotherTimePhase(t *testing.T) {
	item, input, now := drawResearchFakeInput()
	_, before, key, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		t.Fatal(err)
	}
	item.HomeScore, item.AwayScore, item.Settled = 0, 5, true
	input.Result, input.HomeScore, input.AwayScore = 2, 0, 5
	_, after, key2, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) || key.revision != key2.revision {
		t.Fatal("target result entered research lookup key")
	}
	_, _, future, err := prepareDrawResearchInput(item, input, item.MatchTime.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if future.provenance != "live" || future.revision == key.revision {
		t.Fatal("live and historical cutoffs were collapsed")
	}
	r := drawResearchFakeRecord(future, before, drawResearchFakeSignal(), item.MatchTime.Add(-time.Minute))
	bad := item.MatchTime.Add(time.Minute)
	r.knownBy = &bad
	if _, err = validateDrawResearchRecord(r, future, before); err == nil {
		t.Fatal("future evidence accepted as pre-match")
	}
}

func TestResearchTakeoverNotApplicableAvoidsDatabase(t *testing.T) {
	item, input, now := drawResearchFakeInput()
	item.JingcaiID = ""
	b := &drawResearchFakeBackend{lookupErr: errors.New("must not be touched")}
	calls := 0
	_, decision := resolveDrawResearchSignalWithBackend(context.Background(), b, item, input, func() *drawObservationFrozenSignal {
		calls++
		return &drawObservationFrozenSignal{State: "out_of_scope"}
	}, now)
	if calls != 1 || decision.Source != "not_applicable" || b.lookups != 0 || b.writes != 0 {
		t.Fatalf("out-of-scope touched research backend: %#v %#v", b, decision)
	}
}

func TestResearchTakeoverRevisionBindsCurrentFixtureEligibility(t *testing.T) {
	item, input, now := drawResearchFakeInput()
	input.JingcaiID = "older-enrichment-id"
	row, body, key, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		t.Fatal(err)
	}
	if row.JingcaiID != item.JingcaiID {
		t.Fatal("fixture eligibility was not archived")
	}
	item.JingcaiID = "changed-current-id"
	_, next, nextKey, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == string(next) || key.revision == nextKey.revision {
		t.Fatal("eligibility changed without changing input revision")
	}
}

func TestResearchTakeoverBFOnlyChangeBindsRevisionWithoutChangingModel(t *testing.T) {
	item, input, now := drawResearchFakeInput()
	_, before, key, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		t.Fatal(err)
	}
	firstFeatures, err := drawmodeldata.ExtractFeatures(before)
	if err != nil {
		t.Fatal(err)
	}
	firstPrediction := drawmodeldata.Predict(firstFeatures)
	input.ScoreInputs.Quotes[1].Odds = 8
	_, after, nextKey, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		t.Fatal(err)
	}
	nextFeatures, err := drawmodeldata.ExtractFeatures(after)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) || key.revision == nextKey.revision || !reflect.DeepEqual(firstFeatures, nextFeatures) || !reflect.DeepEqual(firstPrediction, drawmodeldata.Predict(nextFeatures)) {
		t.Fatal("BF-only change must change archive input revision without changing frozen selection features")
	}
	var firstRow, nextRow drawFeatureStudyRow
	if json.Unmarshal(before, &firstRow) != nil || json.Unmarshal(after, &nextRow) != nil {
		t.Fatal("invalid exact inputs")
	}
	firstItem, nextItem := researchTakeoverItem(firstRow), researchTakeoverItem(nextRow)
	firstItem.FrozenDrawSignal, nextItem.FrozenDrawSignal = drawResearchFakeSignal(), drawResearchFakeSignal()
	if reflect.DeepEqual(drawObservationScoreRecommendation(firstItem).Scores, drawObservationScoreRecommendation(nextItem).Scores) {
		t.Fatal("BF quote update did not change the dynamically ordered scores")
	}
}

func TestResearchTakeoverDerivedMetadataToleratesOnlyAdjacentFloatFormatting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		change   func(map[string]any)
		accepted bool
	}{
		{"secondary score adjacent float", func(p map[string]any) {
			d := p["secondaryDecision"].(map[string]any)
			d["score"] = math.Nextafter(d["score"].(float64), 0)
		}, true},
		{"BF evidence adjacent float", func(p map[string]any) {
			e := p["scoreRecommendation"].(map[string]any)["evidence"].(map[string]any)
			q := e["quotes"].([]any)[0].(map[string]any)
			q["odds"] = math.Nextafter(q["odds"].(float64), 0)
		}, true},
		{"secondary metadata tampered", func(p map[string]any) {
			p["secondaryDecision"].(map[string]any)["sportteryQuotesAvailable"] = false
		}, false},
		{"BF evidence materially tampered", func(p map[string]any) {
			e := p["scoreRecommendation"].(map[string]any)["evidence"].(map[string]any)
			e["quotes"].([]any)[0].(map[string]any)["odds"] = 99.0
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item, input, now := drawResearchFakeInput()
			_, body, key, err := prepareDrawResearchInput(item, input, now)
			if err != nil {
				t.Fatal(err)
			}
			r := drawResearchFakeRecord(key, body, drawResearchFakeSignal(), now)
			var payload map[string]any
			if err := json.Unmarshal(r.prediction.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			tc.change(payload)
			r.prediction.Payload = researchJSON(payload)
			got, err := validateDrawResearchRecord(r, key, body)
			if tc.accepted && (err != nil || !drawResearchSignalsEqual(got, drawResearchFakeSignal())) {
				t.Fatalf("derived adjacent float must preserve original signal: %v %#v", err, got)
			}
			if !tc.accepted && err == nil {
				t.Fatal("materially changed derived metadata accepted")
			}
		})
	}
}
