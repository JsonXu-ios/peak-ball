package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"go_server/internal/drawmodeldata"
	"peakball/researchstore"
)

const drawResearchTakeoverVersion = "research-takeover-v2-score-guards"

var drawResearchFeatureCodeHash = drawmodeldata.FeatureCodeSHA256()

type drawObservationResearchDecision struct {
	Version      string     `json:"version"`
	Source       string     `json:"source"`
	SnapshotID   int64      `json:"snapshotId,omitempty"`
	PredictionID int64      `json:"predictionId,omitempty"`
	AsOf         *time.Time `json:"asOf,omitempty"`
	Reason       string     `json:"reason"`
}

type drawResearchKey struct {
	matchID, revision, provenance string
	kickoff                       time.Time
}

type drawResearchStoredRecord struct {
	prediction              researchstore.Prediction
	id, snapshotID          int64
	matchID, featureVersion string
	revision, provenance    string
	asOf, kickoff           time.Time
	knownBy                 *time.Time
	predictionHash          string
	input                   []byte
	inputHash               string
	inputObservedAt         time.Time
	inputProvenance         string
}

type drawResearchBackend interface {
	lookup(context.Context, drawResearchKey) (*drawResearchStoredRecord, error)
	write(context.Context, drawObservationItem, drawFeatureStudyRow, []byte, drawResearchKey, *drawObservationFrozenSignal, time.Time) error
}

type drawResearchSQLBackend struct{ store *researchstore.Store }

// A shared connection pool and bounded retry replace per-row connection opens.
// A research outage must never turn the existing observations list into an error.
var drawResearchPool = struct {
	sync.Mutex
	store   *researchstore.Store
	retryAt time.Time
}{}

func drawResearchPooledStore(ctx context.Context) (*researchstore.Store, error) {
	drawResearchPool.Lock()
	defer drawResearchPool.Unlock()
	if time.Now().Before(drawResearchPool.retryAt) {
		return nil, errors.New("research connection retry is deferred")
	}
	if drawResearchPool.store != nil {
		return drawResearchPool.store, nil
	}
	connect, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	store, err := OpenResearchStore(connect)
	if err != nil {
		drawResearchPool.retryAt = time.Now().Add(5 * time.Second)
		return nil, err
	}
	drawResearchPool.store = store
	return store, nil
}

// ResolveDrawResearchSignal reads a prediction only for the exact current input
// revision and frozen versions. A miss computes once, seals, then reads it back.
// The legacy closure is never executed on a verified research read hit.
func ResolveDrawResearchSignal(ctx context.Context, item drawObservationItem, input *drawFeatureStudyRow, legacy func() *drawObservationFrozenSignal) (*drawObservationFrozenSignal, *drawObservationResearchDecision) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(item.JingcaiID) == "" || input == nil {
		return legacy(), drawResearchDecision("not_applicable", "不适用研究库接管的模型输入")
	}
	request, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	store, err := drawResearchPooledStore(request)
	if err != nil {
		return legacy(), drawResearchDecision("legacy_fallback", "研究库暂不可用，保留原计算结果")
	}
	signal, decision := resolveDrawResearchSignalWithBackend(request, drawResearchSQLBackend{store}, item, input, legacy, time.Now().UTC())
	if decision.Source == "legacy_fallback" && (strings.Contains(decision.Reason, "读取研究库失败") || strings.Contains(decision.Reason, "写入失败")) {
		drawResearchPool.Lock()
		drawResearchPool.retryAt = time.Now().Add(5 * time.Second)
		drawResearchPool.Unlock()
	}
	return signal, decision
}

// The injected store is useful for complete offline shadow comparison. It never
// initializes schemas or alters the source/business database.
func resolveDrawResearchSignalWithStore(ctx context.Context, store *researchstore.Store, item drawObservationItem, input *drawFeatureStudyRow, legacy func() *drawObservationFrozenSignal) (*drawObservationFrozenSignal, *drawObservationResearchDecision) {
	return resolveDrawResearchSignalWithBackend(ctx, drawResearchSQLBackend{store}, item, input, legacy, time.Now().UTC())
}

func drawResearchDecision(source, reason string) *drawObservationResearchDecision {
	return &drawObservationResearchDecision{Version: drawResearchTakeoverVersion, Source: source, Reason: reason}
}

func prepareDrawResearchInput(item drawObservationItem, input *drawFeatureStudyRow, now time.Time) (drawFeatureStudyRow, []byte, drawResearchKey, error) {
	if input == nil || strings.TrimSpace(item.MatchID) == "" || item.MatchTime.IsZero() {
		return drawFeatureStudyRow{}, nil, drawResearchKey{}, errors.New("research fixture identity or kickoff missing")
	}
	row := *input
	// Exactly the same override as the original observation model. No current
	// target result is copied into the label-blind serialized input.
	row.MatchID, row.MatchTime = item.MatchID, item.MatchTime
	// Eligibility belongs to the current fixture, not a possibly older optional
	// enrichment row. Bind it into the revision even though numeric trees do not
	// read the lottery ID itself.
	row.JingcaiID = item.JingcaiID
	row.PreMatchHistory = &drawFeatureStudyPreMatchHistory{HomeRecent: item.HomeRecent, AwayRecent: item.AwayRecent, HeadToHead: item.HeadToHead}
	body, err := researchInput(row)
	if err != nil {
		return row, nil, drawResearchKey{}, err
	}
	provenance := "reconstructed"
	if now.Before(row.MatchTime) {
		provenance = "live"
	}
	revision := researchstore.HashBytes(researchJSON(map[string]any{"input": json.RawMessage(body), "version": researchFeatureVersion, "provenance": provenance}))
	return row, body, drawResearchKey{matchID: row.MatchID, revision: revision, provenance: provenance, kickoff: row.MatchTime}, nil
}

func resolveDrawResearchSignalWithBackend(ctx context.Context, backend drawResearchBackend, item drawObservationItem, input *drawFeatureStudyRow, legacy func() *drawObservationFrozenSignal, now time.Time) (*drawObservationFrozenSignal, *drawObservationResearchDecision) {
	now = now.UTC().Truncate(time.Microsecond)
	if strings.TrimSpace(item.JingcaiID) == "" || input == nil {
		return legacy(), drawResearchDecision("not_applicable", "仅接管具有有效输入的竞彩模型计算")
	}
	row, body, key, err := prepareDrawResearchInput(item, input, now)
	if err != nil {
		return legacy(), drawResearchDecision("legacy_fallback", "无法确认当前输入版本，保留原计算结果")
	}
	record, err := backend.lookup(ctx, key)
	if err == nil {
		signal, verifyErr := validateDrawResearchRecord(record, key, body)
		if verifyErr == nil {
			return signal, drawResearchRecordDecision(record, "research", "读取相同输入版本的封存预测")
		}
		return legacy(), drawResearchDecision("legacy_fallback", "库内预测或输入证据校验未通过，保留原计算结果")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return legacy(), drawResearchDecision("legacy_fallback", "读取研究库失败，保留原计算结果")
	}
	signal := legacy()
	if signal == nil {
		return signal, drawResearchDecision("legacy_fallback", "原模型结果不可用，未封存预测")
	}
	if err = backend.write(ctx, item, row, body, key, signal, now); err != nil {
		return signal, drawResearchDecision("legacy_fallback", "原计算成功但研究档案写入失败，保留原计算结果")
	}
	record, err = backend.lookup(ctx, key)
	if err != nil {
		return signal, drawResearchDecision("legacy_fallback", "档案写入后尚不能确认，保留原计算结果")
	}
	read, err := validateDrawResearchRecord(record, key, body)
	if err != nil {
		return signal, drawResearchDecision("legacy_fallback", "档案回读校验未通过，保留原计算结果")
	}
	if !drawResearchSignalsEqual(read, signal) {
		return signal, drawResearchDecision("legacy_fallback", "档案回读与原模型不一致，保留原计算结果")
	}
	return read, drawResearchRecordDecision(record, "research_write_through", "新输入已封存并从研究库回读")
}

func drawResearchRecordDecision(r *drawResearchStoredRecord, source, reason string) *drawObservationResearchDecision {
	asOf := r.asOf
	return &drawObservationResearchDecision{Version: drawResearchTakeoverVersion, Source: source, SnapshotID: r.snapshotID, PredictionID: r.id, AsOf: &asOf, Reason: reason}
}

func (b drawResearchSQLBackend) lookup(ctx context.Context, key drawResearchKey) (*drawResearchStoredRecord, error) {
	return lookupDrawResearchPrediction(ctx, b.store, key)
}

func lookupDrawResearchPrediction(ctx context.Context, store *researchstore.Store, key drawResearchKey) (*drawResearchStoredRecord, error) {
	r := &drawResearchStoredRecord{}
	var score, threshold sql.NullFloat64
	var known sql.NullTime
	err := store.DB().QueryRowContext(ctx, `
SELECT p.id,s.id,s.match_id,s.feature_version,s.data_revision,s.provenance,s.as_of,s.kickoff,s.known_by,
p.model_version,p.rule_version,p.predicted_at,p.state,p.score,p.threshold_value,p.secondary_selected,p.enhanced,p.score_method,p.scorelines,p.payload,p.prediction_hash,e.payload,e.payload_hash,e.observed_at,e.provenance
FROM feature_snapshots s JOIN predictions p ON p.snapshot_id=s.id
JOIN snapshot_sources ss ON ss.snapshot_id=s.id JOIN ingest_events e ON e.id=ss.event_id
WHERE s.match_id=? AND s.feature_version=? AND s.data_revision=? AND s.provenance=?
AND p.model_version=? AND p.rule_version=? AND p.score_method=?
AND e.source='local_feature_input' AND e.kind='label_blind_input'
AND EXISTS(SELECT 1 FROM feature_definitions d WHERE d.feature_version=s.feature_version AND d.code_hash=?)
ORDER BY p.id DESC,e.id ASC LIMIT 1`, key.matchID, researchFeatureVersion, key.revision, key.provenance, drawmodeldata.Version(), drawObservationSecondaryVersion+"+"+drawObservationProfitSupportVersion, drawScoreRecommendationVersion, drawResearchFeatureCodeHash).Scan(
		&r.id, &r.snapshotID, &r.matchID, &r.featureVersion, &r.revision, &r.provenance, &r.asOf, &r.kickoff, &known,
		&r.prediction.ModelVersion, &r.prediction.RuleVersion, &r.prediction.PredictedAt, &r.prediction.State, &score, &threshold, &r.prediction.Secondary, &r.prediction.Enhanced, &r.prediction.ScoreMethod, &r.prediction.Scorelines, &r.prediction.Payload, &r.predictionHash, &r.input, &r.inputHash, &r.inputObservedAt, &r.inputProvenance)
	if err != nil {
		return nil, err
	}
	r.prediction.MatchID, r.prediction.SnapshotID = r.matchID, r.snapshotID
	if score.Valid {
		v := score.Float64
		r.prediction.Score = &v
	}
	if threshold.Valid {
		v := threshold.Float64
		r.prediction.Threshold = &v
	}
	if known.Valid {
		v := known.Time
		r.knownBy = &v
	}
	return r, nil
}

func drawResearchCanonicalJSON(body []byte) (json.RawMessage, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func validateDrawResearchRecord(r *drawResearchStoredRecord, key drawResearchKey, expectedInput []byte) (*drawObservationFrozenSignal, error) {
	if r == nil || r.id <= 0 || r.snapshotID <= 0 || r.asOf.IsZero() || r.matchID != key.matchID || r.featureVersion != researchFeatureVersion || r.revision != key.revision || r.provenance != key.provenance || !r.kickoff.Equal(key.kickoff) {
		return nil, errors.New("research input version mismatch")
	}
	if !bytes.Equal(r.input, expectedInput) || r.inputHash != researchstore.HashBytes(r.input) {
		return nil, errors.New("research source evidence mismatch")
	}
	p := r.prediction
	if p.ModelVersion != drawmodeldata.Version() || p.RuleVersion != drawObservationSecondaryVersion+"+"+drawObservationProfitSupportVersion || p.ScoreMethod != drawScoreRecommendationVersion || p.Threshold == nil || *p.Threshold != drawmodeldata.Threshold() {
		return nil, errors.New("research model contract mismatch")
	}
	if r.provenance == "live" && (r.knownBy == nil || r.knownBy.After(r.asOf) || !r.asOf.Before(r.kickoff) || p.PredictedAt.Before(r.asOf) || !p.PredictedAt.Before(r.kickoff) || r.inputProvenance != "live" || r.inputObservedAt.IsZero() || r.inputObservedAt.After(r.asOf) || r.inputObservedAt.After(*r.knownBy)) {
		return nil, errors.New("research pre-match cutoff mismatch")
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(p.Payload, &payload) != nil {
		return nil, errors.New("invalid research prediction payload")
	}
	var provenance string
	if json.Unmarshal(payload["provenance"], &provenance) != nil || provenance != r.provenance {
		return nil, errors.New("prediction provenance differs from snapshot")
	}
	var signal drawObservationFrozenSignal
	if json.Unmarshal(payload["original"], &signal) != nil {
		return nil, errors.New("invalid original model signal")
	}
	if signal.ModelVersion != p.ModelVersion || signal.State != p.State {
		return nil, errors.New("research signal identity mismatch")
	}
	// MySQL JSON may print a neighboring decimal at the last binary digit.
	// Authoritative DOUBLE columns preserve the original signal and its hash.
	signal.Score = p.Score
	signal.Threshold = *p.Threshold
	if signal.State == "selected" || signal.State == "not_selected" {
		if signal.Score == nil || math.IsNaN(*signal.Score) || math.IsInf(*signal.Score, 0) || *signal.Score < 0 || *signal.Score > 1 {
			return nil, errors.New("invalid model score")
		}
		if (signal.State == "selected") != (*signal.Score >= signal.Threshold) {
			return nil, errors.New("model state disagrees with score")
		}
	} else if signal.State != "unavailable" && signal.State != "out_of_scope" {
		return nil, errors.New("invalid model state")
	}
	var inputRow drawFeatureStudyRow
	if json.Unmarshal(r.input, &inputRow) != nil {
		return nil, errors.New("invalid archived secondary and score inputs")
	}
	item := researchTakeoverItem(inputRow)
	item.FrozenDrawSignal = &signal
	if p.Secondary != drawObservationSecondaryRetained(item) {
		return nil, errors.New("secondary decision changed")
	}
	var scores []drawRecommendedScore
	if json.Unmarshal(p.Scorelines, &scores) != nil {
		return nil, errors.New("invalid archived scores")
	}
	expected := drawObservationScoreRecommendation(item)
	if len(scores) != len(expected.Scores) {
		return nil, errors.New("archived score method changed")
	}
	for i := range scores {
		if scores[i] != expected.Scores[i] {
			return nil, errors.New("archived score choice changed")
		}
	}
	secondary := drawObservationSecondaryDecision(item)
	// Raw source evidence above is byte-for-byte exact. Only the derived JSON
	// projection may print a neighboring float after MySQL JSON round-tripping;
	// verify its structure and values, then restore it from those exact inputs.
	if !drawResearchDerivedJSONEqual(payload["secondaryDecision"], researchJSON(secondary)) || !drawResearchDerivedJSONEqual(payload["scoreRecommendation"], researchJSON(expected)) {
		return nil, errors.New("archived decision evidence differs from source inputs")
	}
	payload["original"] = researchJSON(signal)
	payload["secondaryDecision"] = researchJSON(secondary)
	payload["scoreRecommendation"] = researchJSON(expected)
	canonical, err := drawResearchCanonicalJSON(researchJSON(payload))
	if err != nil {
		return nil, err
	}
	p.Payload = canonical
	p.Scorelines, err = drawResearchCanonicalJSON(p.Scorelines)
	if err != nil {
		return nil, err
	}
	p.PredictedAt = time.Time{}
	if researchstore.HashBytes(researchJSON(p)) != r.predictionHash {
		return nil, errors.New("archived prediction fingerprint mismatch")
	}
	return &signal, nil
}

func drawResearchDerivedJSONEqual(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	var equal func(any, any) bool
	equal = func(x, y any) bool {
		switch value := x.(type) {
		case float64:
			other, ok := y.(float64)
			return ok && math.Abs(value-other) <= 1e-12*math.Max(1, math.Max(math.Abs(value), math.Abs(other)))
		case map[string]any:
			other, ok := y.(map[string]any)
			if !ok || len(value) != len(other) {
				return false
			}
			for key, entry := range value {
				candidate, exists := other[key]
				if !exists || !equal(entry, candidate) {
					return false
				}
			}
			return true
		case []any:
			other, ok := y.([]any)
			if !ok || len(value) != len(other) {
				return false
			}
			for i := range value {
				if !equal(value[i], other[i]) {
					return false
				}
			}
			return true
		case string:
			other, ok := y.(string)
			return ok && value == other
		case bool:
			other, ok := y.(bool)
			return ok && value == other
		default:
			return x == nil && y == nil
		}
	}
	return equal(left, right)
}

func drawResearchSignalsEqual(a, b *drawObservationFrozenSignal) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.ModelVersion != b.ModelVersion || a.State != b.State || a.Threshold != b.Threshold || a.Reason != b.Reason || a.ObservedInputs != b.ObservedInputs || a.UsedInputs != b.UsedInputs {
		return false
	}
	if a.Score == nil || b.Score == nil {
		return a.Score == nil && b.Score == nil
	}
	return *a.Score == *b.Score
}

func (b drawResearchSQLBackend) write(ctx context.Context, item drawObservationItem, row drawFeatureStudyRow, input []byte, key drawResearchKey, signal *drawObservationFrozenSignal, observed time.Time) error {
	store := b.store
	eventID, _, err := store.RecordLocalSnapshot(ctx, researchstore.IngestEvent{Source: "local_feature_input", Kind: "label_blind_input", MatchID: key.matchID, ObservedAt: observed, Provenance: key.provenance, Payload: input, ParserVersion: researchPipelineVersion})
	if err != nil {
		return err
	}
	var exists int
	err = store.DB().QueryRowContext(ctx, "SELECT 1 FROM matches WHERE match_id=?", key.matchID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		date := row.Date
		if date == "" {
			date = item.Date
		}
		if _, dateErr := time.Parse("2006-01-02", date); dateErr != nil {
			return errors.New("source schedule date is missing or invalid")
		}
		// Unknown team IDs remain explicitly empty rather than being guessed
		// from names. The normal collector later enriches the canonical fixture.
		// Insert-only is deliberate: a normal sync can create the full fixture
		// between our SELECT and INSERT, and must never lose its known team IDs.
		home, away, league := row.Home, row.Guest, row.League
		if home == "" {
			home = item.Home
		}
		if away == "" {
			away = item.Guest
		}
		if league == "" {
			league = item.League
		}
		_, err = store.DB().ExecContext(ctx, `INSERT INTO matches(match_id,home_id,away_id,home_name,away_name,league_id,league_name,schedule_date,kickoff,jingcai_id,source_event_id,updated_at) VALUES(?, '', '', ?, ?, '', ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE match_id=match_id`, key.matchID, home, away, league, date, key.kickoff.UTC(), item.JingcaiID, eventID, observed)
	}
	if err != nil {
		return err
	}
	features, err := drawmodeldata.ExtractFeatures(input)
	if err != nil {
		return err
	}
	values, missing := researchFeatureValues(features)
	quality, asOf := "research_only", key.kickoff
	var known *time.Time
	if key.provenance == "live" {
		quality, asOf = "pre_match_observed", observed
		v := observed
		known = &v
	}
	flags := []string{}
	if row.Timing.EuroCapturedAt == nil {
		flags = append(flags, "euro_source_capture_unknown")
	}
	if row.Timing.LineCapturedAt == nil {
		flags = append(flags, "line_source_capture_unknown")
	}
	if row.History.CapturedAt == nil {
		flags = append(flags, "history_source_capture_unknown")
	}
	if key.provenance == "reconstructed" {
		flags = append(flags, "historical_reconstruction_not_prospective")
	}
	if row.History.UsedRecentAtOrAfterKickoff+row.History.UsedRecentTargetID > 0 {
		flags = append(flags, "legacy_derived_history_timing_risk")
	}
	if row.PreMatchHistory.HomeRecent.Sample < 10 || row.PreMatchHistory.AwayRecent.Sample < 10 {
		flags = append(flags, "recent_history_incomplete")
	}
	snapshotID, err := store.SaveSnapshot(ctx, researchstore.FeatureSnapshot{MatchID: key.matchID, AsOf: asOf, Kickoff: key.kickoff, FeatureVersion: researchFeatureVersion, DataRevision: key.revision, Provenance: key.provenance, KnownBy: known, Quality: quality, Values: values, Missing: missing, QualityFlags: researchJSON(flags), SourceEventIDs: []int64{eventID}})
	if err != nil {
		return err
	}
	decisionItem := researchTakeoverItem(row)
	decisionItem.FrozenDrawSignal = signal
	scores := drawObservationScoreRecommendation(decisionItem)
	secondaryDecision := drawObservationSecondaryDecision(decisionItem)
	secondary := secondaryDecision.Decision == "retained"
	profitItem := decisionItem
	predictedAt := time.Now().UTC().Truncate(time.Microsecond)
	if key.provenance == "live" && !predictedAt.Before(key.kickoff) {
		return errors.New("kickoff passed before prediction could be sealed")
	}
	_, err = store.SavePrediction(ctx, researchstore.Prediction{SnapshotID: snapshotID, MatchID: key.matchID, ModelVersion: drawmodeldata.Version(), RuleVersion: drawObservationSecondaryVersion + "+" + drawObservationProfitSupportVersion, PredictedAt: predictedAt, State: signal.State, Score: signal.Score, Threshold: &signal.Threshold, Secondary: secondary, Enhanced: secondary && drawObservationProfitSupport(profitItem).State == "supported", ScoreMethod: drawScoreRecommendationVersion, Scorelines: researchJSON(scores.Scores), Payload: researchJSON(map[string]any{"original": signal, "secondaryDecision": secondaryDecision, "scoreRecommendation": scores, "provenance": key.provenance, "probabilityCalibrated": false})})
	if err != nil {
		return fmt.Errorf("seal research prediction: %w", err)
	}
	return nil
}
