package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go_server/database"
	"go_server/internal/drawmodeldata"
	"go_server/models"
	"gorm.io/gorm"
	"peakball/researchstore"
)

type researchSourceRow struct {
	table, matchID string
	payload        json.RawMessage
}
type researchSourceSnapshot struct {
	data       DrawFeatureStudyDataset
	raw        []researchSourceRow
	main       map[string]models.Money
	capturedAt time.Time
	issues     []researchstore.AuditIssue
	revision   string
	unchanged  bool
}

// The source transaction is SELECT-only. Writes go exclusively through the
// separately opened research connection, after this consistent source read.
func loadResearchSource(ctx context.Context, db *gorm.DB, previousRevision string) (researchSourceSnapshot, error) {
	out := researchSourceSnapshot{main: map[string]models.Money{}}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		var err error
		schema, err := drawObservationReadSchema(tx)
		if err != nil {
			return err
		}
		targets, err := drawObservationReadTargets(tx, schema, nil)
		if err != nil {
			return err
		}
		for _, target := range targets {
			out.main[target.Match.MatchId] = target.Match
		}
		for _, table := range []string{"money", "moneys", "history_moneys", "odds_moneys", "pankou_moneys"} {
			if !schema[table]["match_id"] {
				continue
			}
			var rows []map[string]interface{}
			if err := tx.Table(table).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				firstIssue := len(out.issues)
				for key, value := range row {
					if b, ok := value.([]byte); ok && researchJSONColumn(table, key) && len(b) > 0 && !json.Valid(b) {
						out.issues = append(out.issues, researchstore.AuditIssue{Severity: "error", Code: "INVALID_SOURCE_JSON", Field: table + "." + key, Message: "JSON语法无效；原文保留，解析结果按缺失处理", Detail: researchJSON(map[string]string{"payloadHash": researchstore.HashBytes(b)})})
					}
					row[key] = researchSourceValue(table, key, value)
				}
				id := strings.TrimSpace(fmt.Sprint(row["match_id"]))
				if id == "" || id == "<nil>" {
					id = ""
					out.issues = append(out.issues, researchstore.AuditIssue{Severity: "error", Code: "MATCH_ID_MISSING", Field: table + ".match_id", Message: "比赛ID缺失，保留原始记录并隔离，不生成特征快照", Detail: researchJSON(map[string]string{"table": table})})
				}
				for i := firstIssue; i < len(out.issues); i++ {
					out.issues[i].MatchID = id
				}
				body, err := json.Marshal(row)
				if err != nil {
					return err
				}
				out.raw = append(out.raw, researchSourceRow{table: table, matchID: id, payload: body})
			}
		}
		// Compare exact source content, not a time-to-live cache. With unchanged
		// evidence and extractor code there is no reason to rerun every feature.
		sort.Slice(out.raw, func(i, j int) bool {
			a, b := out.raw[i], out.raw[j]
			if a.table != b.table {
				return a.table < b.table
			}
			if a.matchID != b.matchID {
				return a.matchID < b.matchID
			}
			return string(a.payload) < string(b.payload)
		})
		hash := sha256.New()
		hash.Write([]byte(researchPipelineVersion))
		hash.Write([]byte(researchPipelineCodeHash))
		hash.Write([]byte(drawmodeldata.FeatureCodeSHA256()))
		for _, raw := range out.raw {
			hash.Write([]byte(raw.table))
			hash.Write([]byte{0})
			hash.Write(raw.payload)
			hash.Write([]byte{0})
		}
		out.revision = fmt.Sprintf("%x", hash.Sum(nil))
		if previousRevision != "" && out.revision == previousRevision {
			out.unchanged = true
			return nil
		}
		out.data, err = exportDrawFeatureStudy(tx, now, true)
		if err != nil {
			return err
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	out.capturedAt = time.Now().UTC().Truncate(time.Microsecond)
	return out, err
}

func researchSourceValue(table, key string, value any) any {
	b, ok := value.([]byte)
	if !ok {
		return value
	}
	// Numeric-looking IDs and ordinary text must never be interpreted as JSON
	// numbers (and then rounded or formatted in scientific notation).
	if researchJSONColumn(table, key) {
		var parsed any
		decoder := json.NewDecoder(strings.NewReader(string(b)))
		decoder.UseNumber()
		if json.Valid(b) && decoder.Decode(&parsed) == nil {
			return parsed
		}
	}
	return string(b)
}

func researchJSONColumn(table, key string) bool {
	jsonColumns := map[string]string{
		"history_moneys": " league_stat against_summary against_list recent_home_summary recent_home_list recent_guest_summary recent_guest_list league_summary rank_data future_home future_guest ",
		"odds_moneys":    " data rise_and_fall avg_odds william bet365 pinnacle sporttery_trade ",
		"pankou_moneys":  " asia_data dxq_data bet365_asia bet365_dxq ",
	}
	return strings.Contains(jsonColumns[table], " "+key+" ")
}

func researchInput(row drawFeatureStudyRow) (json.RawMessage, error) {
	// A structural exclusion is retained in addition to the label-blind extractor.
	// A source result may exist in an ingest event, never in the feature input.
	body, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var input map[string]json.RawMessage
	if err = json.Unmarshal(body, &input); err != nil {
		return nil, err
	}
	for _, key := range []string{"result", "homeScore", "awayScore", "status", "settled", "predictionHit"} {
		delete(input, key)
	}
	return json.Marshal(input)
}

func researchDefinitions() ([]researchstore.FeatureDefinition, error) {
	var manifest struct {
		Definitions []map[string]interface{} `json:"definitions"`
	}
	if err := json.Unmarshal(drawmodeldata.ResearchManifest(), &manifest); err != nil {
		return nil, err
	}
	inputs := map[string]drawmodeldata.InputFeature{}
	for _, f := range drawmodeldata.Features() {
		inputs[f.Name] = f
	}
	definitions := make([]researchstore.FeatureDefinition, 0, len(manifest.Definitions))
	for _, meta := range manifest.Definitions {
		name, _ := meta["name"].(string)
		label, _ := meta["label"].(string)
		unit, _ := meta["unit"].(string)
		view, _ := meta["view"].(string)
		source, _ := meta["source"].(string)
		if f, ok := inputs[name]; ok {
			meta["modelInput"] = true
			meta["usedInTrees"] = f.UsedInTrees
			meta["imputationMedian"] = f.Median
			meta["modelGuards"] = f.Guards
		}
		definitions = append(definitions, researchstore.FeatureDefinition{Version: researchFeatureVersion, Name: name, Type: "float64", Unit: unit, Description: label, Formula: source, Origin: view, MissingPolicy: "NULL in snapshots; original fixed training median only at model inference", CodeHash: drawmodeldata.FeatureCodeSHA256(), Metadata: researchJSON(meta)})
	}
	return definitions, nil
}

func researchFeatureValues(raw map[string]float64) (json.RawMessage, json.RawMessage) {
	values := map[string]*float64{}
	var catalogue struct {
		Definitions []struct {
			Name string `json:"name"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(drawmodeldata.ResearchManifest(), &catalogue); err != nil {
		panic(err)
	}
	for _, f := range catalogue.Definitions {
		values[f.Name] = nil
	}
	for name, value := range raw {
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			v := value
			values[name] = &v
		}
	}
	for _, f := range drawmodeldata.Features() {
		v, valid := raw[f.Name]
		valid = valid && !math.IsNaN(v) && !math.IsInf(v, 0)
		for _, g := range f.Guards {
			gv, ok := raw[g.Feature]
			valid = valid && ok && gv >= g.Minimum && !math.IsNaN(gv) && !math.IsInf(gv, 0)
		}
		if !valid {
			values[f.Name] = nil
		}
	}
	missing := []string{}
	for name, value := range values {
		if value == nil {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return researchJSON(values), researchJSON(missing)
}

func researchIdentity(ctx context.Context, store *researchstore.Store, m models.Money, eventID *int64) error {
	teamKey := func(id int, side string) string {
		if id > 0 {
			return "vipc:team:" + strconv.Itoa(id)
		}
		return "unresolved:" + m.MatchId + ":" + side
	}
	home, away := teamKey(m.HomeTeamId, "home"), teamKey(m.GuestTeamId, "away")
	entities := []researchstore.Entity{}
	aliases := []researchstore.EntityAlias{}
	for _, t := range []struct {
		key, name string
		id        int
	}{{home, m.Home, m.HomeTeamId}, {away, m.Guest, m.GuestTeamId}} {
		confidence, external := "id", strconv.Itoa(t.id)
		if t.id <= 0 {
			confidence, external = "unresolved", ""
		}
		entities = append(entities, researchstore.Entity{Key: t.key, Kind: "team", Name: t.name, Source: "vipc", ExternalID: external, Metadata: researchJSON(map[string]string{"identity": confidence})})
		aliases = append(aliases, researchstore.EntityAlias{EntityKey: t.key, Source: "vipc", ExternalID: external, Alias: t.name, Confidence: confidence})
	}
	return store.SaveFixture(ctx, entities, aliases, researchstore.Match{MatchID: m.MatchId, HomeID: home, AwayID: away, Home: m.Home, Away: m.Guest, LeagueID: strconv.Itoa(m.LeagueId), League: drawObservationLeague(m), ScheduleDate: drawObservationDate(m), Kickoff: drawSourceTime(m.MatchTime), JingcaiID: m.JingcaiID, SourceEventID: eventID})
}

func researchNumeric(s *string) *float64 {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(*s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return &v
}

func saveResearchQuotes(ctx context.Context, store *researchstore.Store, row drawFeatureStudyRow, eventID int64, observed time.Time, provenance string) error {
	quotes := []researchstore.MarketQuote{}
	for _, company := range row.CompanyQuotes {
		for _, phase := range []string{"opening", "latest"} {
			prices := company.EuroOdds.Opening
			if phase == "latest" {
				prices = company.EuroOdds.Latest
			}
			if len(prices) == 3 {
				h, d, a := researchNumeric(prices[0]), researchNumeric(prices[1]), researchNumeric(prices[2])
				if h != nil && d != nil && a != nil && *h > 1 && *d > 1 && *a > 1 {
					quotes = append(quotes, researchstore.MarketQuote{MatchID: row.MatchID, Company: company.Key, Market: "euro", Phase: phase, SourceEventID: &eventID, ObservedAt: observed, Provenance: provenance, Home: h, Draw: d, Away: a, Raw: researchJSON(prices)})
				}
			}
			for _, market := range []struct {
				key  string
				line drawObservationLine
			}{{"asian", company.AsianHandicap}, {"goals", company.GoalsLine}} {
				line, waters := market.line.Opening, market.line.OpeningOdds
				if phase == "latest" {
					line, waters = market.line.Latest, market.line.LatestOdds
				}
				if line == nil {
					continue
				}
				value := drawLine(*line, market.key == "goals")
				if value == nil {
					continue
				}
				q := researchstore.MarketQuote{MatchID: row.MatchID, Company: company.Key, Market: market.key, Phase: phase, SourceEventID: &eventID, ObservedAt: observed, Provenance: provenance, Line: value, Raw: researchJSON(map[string]any{"line": line, "water": waters})}
				if len(waters) >= 2 {
					q.HomeWater, q.AwayWater = researchNumeric(waters[0]), researchNumeric(waters[1])
				}
				quotes = append(quotes, q)
			}
		}
	}
	return store.SaveQuotes(ctx, quotes)
}

type researchMetric struct {
	Eligible          int      `json:"eligible"`
	Draws             int      `json:"draws"`
	Selected          int      `json:"selected"`
	Hits              int      `json:"hits"`
	ScoreHits         int      `json:"scoreHits"`
	ScoreCandidates   int      `json:"scoreCandidates"`
	FirstScoreHits    int      `json:"firstScoreHits"`
	Precision         *float64 `json:"precision"`
	Recall            *float64 `json:"recall"`
	ScoreHitRate      *float64 `json:"scoreHitRate"`
	FirstScoreHitRate *float64 `json:"firstScoreHitRate"`
}

func (m *researchMetric) rates() {
	if m.Selected > 0 {
		p := float64(m.Hits) / float64(m.Selected)
		m.Precision = &p
	}
	if m.ScoreCandidates > 0 {
		s := float64(m.ScoreHits) / float64(m.ScoreCandidates)
		m.ScoreHitRate = &s
		first := float64(m.FirstScoreHits) / float64(m.ScoreCandidates)
		m.FirstScoreHitRate = &first
	}
	if m.Draws > 0 {
		r := float64(m.Hits) / float64(m.Draws)
		m.Recall = &r
	}
}

// Count the actual archived ranked choices. This remains correct if a later,
// separately versioned score method recommends 2:2 or other draw scorelines.
func (m *researchMetric) addScoreResult(scores []drawRecommendedScore, home, away int) {
	if len(scores) == 0 {
		return
	}
	m.ScoreCandidates++
	if scores[0].Home == home && scores[0].Away == away {
		m.FirstScoreHits++
	}
	for i, score := range scores {
		if i >= 2 {
			break
		}
		if score.Home == home && score.Away == away {
			m.ScoreHits++
			break
		}
	}
}

func settleResearchPredictions(ctx context.Context, store *researchstore.Store, matchID string, resultID int64, h, a int, now time.Time) error {
	rows, err := store.DB().QueryContext(ctx, "SELECT id,scorelines FROM predictions WHERE match_id=?", matchID)
	if err != nil {
		return err
	}
	type pending struct {
		id     int64
		scores []drawRecommendedScore
	}
	items := []pending{}
	for rows.Next() {
		var id int64
		var body []byte
		if err = rows.Scan(&id, &body); err != nil {
			rows.Close()
			return err
		}
		var scores []drawRecommendedScore
		if err = json.Unmarshal(body, &scores); err != nil {
			rows.Close()
			return err
		}
		items = append(items, pending{id, scores})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range items {
		hit := false
		for _, s := range p.scores {
			hit = hit || s.Home == h && s.Away == a
		}
		if err := store.SaveSettlement(ctx, researchstore.Settlement{PredictionID: p.id, ResultID: resultID, IsDraw: h == a, ScorelineHit: hit, SettledAt: now}); err != nil {
			return err
		}
	}
	return nil
}

func saveResearchResult(ctx context.Context, store *researchstore.Store, row drawFeatureStudyRow, eventID int64, observed time.Time) (int64, error) {
	// A periodic archive is not a new result event when the confirmed score is
	// unchanged. Corrections append a new result and preserve previous labels.
	var id int64
	var h, a sql.NullInt64
	var confirmed bool
	err := store.DB().QueryRowContext(ctx, "SELECT id,home_score,away_score,regulation_confirmed FROM match_results WHERE match_id=? ORDER BY observed_at DESC,id DESC LIMIT 1", row.MatchID).Scan(&id, &h, &a, &confirmed)
	if err == nil && confirmed && h.Valid && a.Valid && int(h.Int64) == row.HomeScore && int(a.Int64) == row.AwayScore {
		return id, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	home, away := row.HomeScore, row.AwayScore
	return store.SaveResult(ctx, researchstore.MatchResult{MatchID: row.MatchID, Home: &home, Away: &away, RegulationConfirmed: true, Status: "finished_regulation", ObservedAt: observed, SourceEventID: &eventID, Provenance: "reconstructed"})
}

func runResearchSync(ctx context.Context) (err error) {
	if err = researchDatabaseReady(); err != nil {
		return err
	}
	store, err := OpenResearchStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	lock, err := store.DB().Conn(ctx)
	if err != nil {
		return err
	}
	defer lock.Close()
	var acquired int
	if err = lock.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", "peakball_research_sync_"+store.Name()).Scan(&acquired); err != nil {
		return err
	}
	if acquired != 1 {
		return fmt.Errorf("另一个研究库同步任务正在运行")
	}
	defer lock.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", "peakball_research_sync_"+store.Name())
	runID, err := store.StartAudit(ctx, researchstore.AuditRun{Kind: "source_sync", Version: researchPipelineVersion, StartedAt: time.Now().UTC(), Metadata: researchJSON(map[string]string{"source": "business read-only snapshot", "scoreMethod": drawScoreRecommendationVersion})})
	if err != nil {
		return err
	}
	updateResearchStatus(func(s *ResearchSyncStatus) { s.RunID = runID })
	defer func() {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		final := currentResearchStatus()
		final.State = status
		finished := time.Now().UTC()
		final.FinishedAt = &finished
		if err != nil {
			final.Error = err.Error()
			final.Message = "同步失败，已归档数据保留"
		} else {
			final.Message = "同步完成"
		}
		_ = store.FinishAudit(cleanup, runID, status, researchJSON(final))
	}()
	var previousRevision string
	var previousDatasetID int64
	_ = store.DB().QueryRowContext(ctx, "SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(metadata,'$.sourceRevision')),''),COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata,'$.datasetId')) AS UNSIGNED),0) FROM audit_runs WHERE kind='source_sync' AND status='completed' ORDER BY id DESC LIMIT 1").Scan(&previousRevision, &previousDatasetID)
	source, err := loadResearchSource(ctx, database.DB, previousRevision)
	if err != nil {
		return err
	}
	updateResearchStatus(func(s *ResearchSyncStatus) {
		s.SourceRevision = source.revision
		s.Unchanged = source.unchanged
		s.PipelineCodeHash = researchPipelineCodeHash
	})
	if source.unchanged {
		updateResearchStatus(func(s *ResearchSyncStatus) {
			s.Message = "原始数据与特征版本未变化，沿用已封存数据"
			s.DatasetID = previousDatasetID
		})
		return nil
	}
	updateResearchStatus(func(s *ResearchSyncStatus) {
		s.Total = len(source.data.Rows)
		s.Message = "正在追加原始数据与质量审计"
	})
	definitions, err := researchDefinitions()
	if err != nil {
		return err
	}
	if err = store.RegisterFeatures(ctx, definitions); err != nil {
		return err
	}
	refs := map[string][]int64{}
	mainEvents := map[string]int64{}
	for n, raw := range source.raw {
		provenance := "reconstructed"
		if m, ok := source.main[raw.matchID]; ok && drawSourceTime(m.MatchTime).After(source.capturedAt) {
			provenance = "live"
		}
		id, _, e := store.RecordLocalSnapshot(ctx, researchstore.IngestEvent{Source: "local_business_snapshot", Kind: raw.table, MatchID: raw.matchID, ObservedAt: source.capturedAt, Provenance: provenance, Payload: raw.payload, ParserVersion: researchPipelineVersion})
		if e != nil {
			return e
		}
		refs[raw.matchID] = append(refs[raw.matchID], id)
		if raw.table == "moneys" || raw.table == "money" {
			mainEvents[raw.matchID] = id
		}
		if n%250 == 0 {
			updateResearchStatus(func(s *ResearchSyncStatus) {
				s.Message = fmt.Sprintf("原始数据归档：%d / %d", n+1, len(source.raw))
			})
			if _, e = store.DB().ExecContext(ctx, "UPDATE audit_runs SET metadata=? WHERE id=? AND status='running'", researchJSON(currentResearchStatus()), runID); e != nil {
				return e
			}
		}
	}
	members := []researchstore.DatasetMember{}
	metrics := map[string]*researchMetric{"original": {}, "secondary": {}, "enhanced": {}}
	issues := source.issues
	for i := range issues {
		issues[i].RunID = runID
	}
	for n, row := range source.data.Rows {
		if e := ctx.Err(); e != nil {
			return e
		}
		m, ok := source.main[row.MatchID]
		if !ok {
			continue
		}
		if m.HomeTeamId > 0 && m.HomeTeamId == m.GuestTeamId {
			issues = append(issues, researchstore.AuditIssue{RunID: runID, MatchID: row.MatchID, Severity: "error", Code: "TEAM_ID_CONFLICT", Field: "moneys.home_team_id,guest_team_id", Message: "主客队ID相同，原始记录保留；隔离该场特征与预测", Detail: researchJSON(map[string]int{"teamId": m.HomeTeamId})})
			continue
		}
		if m.HomeTeamId <= 0 || m.GuestTeamId <= 0 {
			issues = append(issues, researchstore.AuditIssue{RunID: runID, MatchID: row.MatchID, Severity: "warning", Code: "TEAM_ID_UNRESOLVED", Field: "moneys.home_team_id,guest_team_id", Message: "球队ID缺失，使用比赛内隔离身份，禁止凭同名合并球队", Detail: researchJSON(map[string]int{"homeId": m.HomeTeamId, "awayId": m.GuestTeamId})})
		}
		eventID := mainEvents[row.MatchID]
		if err = researchIdentity(ctx, store, m, &eventID); err != nil {
			return err
		}
		input, e := researchInput(row)
		if e != nil {
			return e
		}
		rawFeatures, e := drawmodeldata.ExtractFeatures(input)
		if e != nil {
			return e
		}
		values, missing := researchFeatureValues(rawFeatures)
		now := time.Now().UTC().Truncate(time.Microsecond)
		provenance, quality, asOf := "reconstructed", "research_only", row.MatchTime
		var knownBy *time.Time
		if now.Before(row.MatchTime) {
			provenance, quality, asOf = "live", "pre_match_observed", now
			known := source.capturedAt
			knownBy = &known
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
		if provenance == "reconstructed" {
			flags = append(flags, "historical_reconstruction_not_prospective")
		}
		if row.History.UsedRecentAtOrAfterKickoff+row.History.UsedRecentTargetID > 0 {
			flags = append(flags, "legacy_derived_history_timing_risk")
		}
		if row.PreMatchHistory.HomeRecent.Sample < 10 || row.PreMatchHistory.AwayRecent.Sample < 10 {
			flags = append(flags, "recent_history_incomplete")
		}
		inputEvent, firstObserved, e := store.RecordLocalSnapshot(ctx, researchstore.IngestEvent{Source: "local_feature_input", Kind: "label_blind_input", MatchID: row.MatchID, ObservedAt: source.capturedAt, Provenance: provenance, Payload: input, ParserVersion: researchPipelineVersion})
		if e != nil {
			return e
		}
		eventIDs := append(append([]int64{}, refs[row.MatchID]...), inputEvent)
		revision := researchstore.HashBytes(researchJSON(map[string]any{"input": json.RawMessage(input), "version": researchFeatureVersion, "provenance": provenance}))
		snapshotID, e := store.SaveSnapshot(ctx, researchstore.FeatureSnapshot{MatchID: row.MatchID, AsOf: asOf, Kickoff: row.MatchTime, FeatureVersion: researchFeatureVersion, DataRevision: revision, Provenance: provenance, KnownBy: knownBy, Quality: quality, Values: values, Missing: missing, QualityFlags: researchJSON(flags), SourceEventIDs: eventIDs})
		if e != nil {
			return e
		}
		if err = saveResearchQuotes(ctx, store, row, inputEvent, firstObserved, provenance); err != nil {
			return err
		}
		item := researchTakeoverItem(row)
		signal := drawObservationModelSignal(item, &row)
		item.FrozenDrawSignal = signal
		scores := drawObservationScoreRecommendation(item)
		secondaryDecision := drawObservationSecondaryDecision(item)
		secondary := secondaryDecision.Decision == "retained"
		enhanced := secondary && drawObservationProfitSupport(item).State == "supported"
		predictionID, e := store.SavePrediction(ctx, researchstore.Prediction{SnapshotID: snapshotID, MatchID: row.MatchID, ModelVersion: drawmodeldata.Version(), RuleVersion: drawObservationSecondaryVersion + "+" + drawObservationProfitSupportVersion, PredictedAt: now, State: signal.State, Score: signal.Score, Threshold: &signal.Threshold, Secondary: secondary, Enhanced: enhanced, ScoreMethod: drawScoreRecommendationVersion, Scorelines: researchJSON(scores.Scores), Payload: researchJSON(map[string]any{"original": signal, "secondaryDecision": secondaryDecision, "scoreRecommendation": scores, "provenance": provenance, "probabilityCalibrated": false})})
		if e != nil {
			return e
		}
		_ = predictionID
		var resultID *int64
		if row.Result >= 0 {
			h, a := row.HomeScore, row.AwayScore
			id, e := saveResearchResult(ctx, store, row, eventID, source.capturedAt)
			if e != nil {
				return e
			}
			resultID = &id
			if err = settleResearchPredictions(ctx, store, row.MatchID, id, h, a, now); err != nil {
				return err
			}
			if strings.TrimSpace(row.JingcaiID) != "" {
				for key, v := range metrics {
					v.Eligible++
					if h == a {
						v.Draws++
					}
					selected := signal.State == "selected"
					if key == "secondary" {
						selected = secondary
					}
					if key == "enhanced" {
						selected = enhanced
					}
					if selected {
						v.Selected++
						if secondary {
							v.addScoreResult(scores.Scores, h, a)
						}
						if h == a {
							v.Hits++
						}
					}
				}
			}
		}
		if row.Result < 0 && !row.MatchTime.After(now) {
			issues = append(issues, researchstore.AuditIssue{RunID: runID, MatchID: row.MatchID, Severity: "warning", Code: "REGULATION_RESULT_UNCONFIRMED", Field: "result", Message: "常规时间赛果尚未确认；保留特征，不写训练标签、不计命中率", Detail: researchJSON(map[string]any{"status": m.Status, "displayState": m.DisplayState})})
		}
		split := "research"
		if provenance == "live" {
			split = "prospective"
		}
		members = append(members, researchstore.DatasetMember{SnapshotID: snapshotID, ResultID: resultID, Split: split})
		if len(flags) > 0 {
			issues = append(issues, researchstore.AuditIssue{RunID: runID, MatchID: row.MatchID, SourceEventID: &inputEvent, Severity: "warning", Code: "SOURCE_QUALITY", Field: "inputs", Message: "来源时点、历史样本或重建属性需要分层评估", Detail: researchJSON(map[string]any{"flags": flags, "quality": quality})})
		}
		if row.PreMatchHistory.HeadToHead.Sample == 0 {
			issues = append(issues, researchstore.AuditIssue{RunID: runID, MatchID: row.MatchID, Severity: "info", Code: "HEAD_TO_HEAD_MISSING", Field: "preMatchHistory.headToHead", Message: "本地交锋缺失，保持缺失；不是零次交锋平局", Detail: researchJSON(map[string]int{"sample": 0})})
		}
		if len(issues) >= 250 {
			if err = store.SaveIssues(ctx, issues); err != nil {
				return err
			}
			issues = nil
		}
		if n%25 == 0 {
			updateResearchStatus(func(s *ResearchSyncStatus) {
				s.Processed = n + 1
				s.Message = "正在封存特征、预测及90分钟赛果"
			})
			if _, e = store.DB().ExecContext(ctx, "UPDATE audit_runs SET metadata=? WHERE id=? AND status='running'", researchJSON(currentResearchStatus()), runID); e != nil {
				return e
			}
		}
	}
	if len(issues) > 0 {
		if err = store.SaveIssues(ctx, issues); err != nil {
			return err
		}
	}
	manifest := map[string]any{"pipeline": researchPipelineVersion, "pipelineCodeHash": researchPipelineCodeHash, "featureVersion": researchFeatureVersion, "featureCodeHash": drawmodeldata.FeatureCodeSHA256(), "modelVersion": drawmodeldata.Version(), "artifactHash": drawmodeldata.ArtifactSHA256(), "members": members, "timing": "research = historical reconstruction; prospective = archived before kickoff, source-time quality flags retained", "noTrainingPerformed": true}
	body := researchJSON(manifest)
	datasetVersion := researchstore.HashBytes(body)
	datasetID, e := store.SaveDataset(ctx, researchstore.Dataset{Name: "observation-archive", Version: datasetVersion, FeatureVersion: researchFeatureVersion, Manifest: body, CreatedAt: time.Now().UTC()})
	if e != nil {
		return e
	}
	updateResearchStatus(func(s *ResearchSyncStatus) { s.DatasetID = datasetID })
	for i := range members {
		members[i].DatasetID = datasetID
	}
	if err = store.AddDatasetMembers(ctx, members); err != nil {
		return err
	}
	modelID, e := store.SaveModelRun(ctx, researchstore.ModelRun{Version: drawmodeldata.Version(), DatasetID: datasetID, ArtifactHash: drawmodeldata.ArtifactSHA256(), ArtifactURI: "repo://go_server/internal/drawmodeldata/observation_v1.json", Status: "registered_existing", Config: researchJSON(map[string]any{"trained": false, "secondaryThreshold": drawObservationSecondaryThreshold, "scoreMethod": drawScoreRecommendationVersion}), StartedAt: time.Now().UTC()})
	if e != nil {
		return e
	}
	for _, v := range metrics {
		v.rates()
	}
	_, err = store.SaveEvaluation(ctx, researchstore.Evaluation{ModelRunID: &modelID, DatasetID: &datasetID, Version: researchEvaluationVersion, Scope: "settled_sporttery_retrospective", Metrics: researchJSON(map[string]any{"groups": metrics, "historicalAssociationOnly": true, "allSourceCaptureTimesVerified": false, "scoreMethod": drawScoreRecommendationVersion, "secondaryVersion": drawObservationSecondaryVersion}), CreatedAt: time.Now().UTC()})
	if err == nil {
		err = evaluateResearchProspective(ctx, store)
	}
	updateResearchStatus(func(s *ResearchSyncStatus) { s.Processed = len(source.data.Rows); s.Total = len(source.data.Rows) })
	return err
}
