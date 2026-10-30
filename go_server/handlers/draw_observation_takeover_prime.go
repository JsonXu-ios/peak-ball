package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"go_server/internal/drawmodeldata"
	"gorm.io/gorm"
	"peakball/researchstore"
)

type DrawResearchTakeoverVerification struct {
	Version                     string                     `json:"version"`
	GeneratedAt                 time.Time                  `json:"generatedAt"`
	Fixtures                    int                        `json:"fixtures"`
	Eligible                    int                        `json:"eligible"`
	Settled                     int                        `json:"settled"`
	Draws                       int                        `json:"draws"`
	ReadHits                    int                        `json:"readHits"`
	WriteThrough                int                        `json:"writeThrough"`
	Fallback                    int                        `json:"fallback"`
	Mismatches                  int                        `json:"mismatches"`
	ReadVerificationLegacyCalls int                        `json:"readVerificationLegacyCalls"`
	VerifiedReadHits            int                        `json:"verifiedReadHits"`
	Metrics                     map[string]*researchMetric `json:"metrics"`
	DatasetID                   int64                      `json:"datasetId,omitempty"`
	Ready                       bool                       `json:"ready"`
	ModelArtifactHash           string                     `json:"modelArtifactHash"`
}

// ExportDrawResearchCurrentSource exposes the same SELECT-only source adapter
// for the finite takeover command and its independent full-cohort checks.
func ExportDrawResearchCurrentSource(db *gorm.DB, now time.Time) (DrawFeatureStudyDataset, error) {
	data := DrawFeatureStudyDataset{GeneratedAt: now.UTC(), Version: "research-takeover-source-v1", Audit: map[string]interface{}{}, Rows: []drawFeatureStudyRow{}}
	err := db.Transaction(func(tx *gorm.DB) error {
		loaders := drawObservationSQLLoadersForDB(func() *gorm.DB { return tx })
		// Build the exact same source/enrichment adapter as the list, without
		// invoking its resolver yet. SQL projection metadata can differ from the
		// wider offline study export and must not silently choose another frame.
		cache := newDrawObservationCache(loaders, time.Hour, time.Now)
		core, err := cache.get(false)
		if err != nil {
			return err
		}
		targets := []drawObservationTarget{}
		ids := []string{}
		for _, target := range core.base.targets {
			if strings.TrimSpace(target.Match.JingcaiID) != "" {
				targets = append(targets, target)
				ids = append(ids, target.Match.MatchId)
			}
		}
		front, err := core.frontend(ids, targets, loaders)
		if err != nil {
			return err
		}
		known := map[string]bool{}
		for _, table := range []string{"money", "moneys"} {
			if !core.base.schema[table]["home_score"] || !core.base.schema[table]["guest_score"] {
				continue
			}
			var rows []struct {
				MatchID string
				Present bool
			}
			if err = tx.Table(table).Select("match_id,(home_score IS NOT NULL AND guest_score IS NOT NULL) AS present").Where("match_id IN ?", ids).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				known[row.MatchID] = row.Present
			}
		}
		for _, target := range targets {
			m := target.Match
			enrichment, ok := front[m.MatchId]
			if !ok || enrichment.modelInput == nil {
				return fmt.Errorf("比赛缺少与列表一致的输入：%s", m.MatchId)
			}
			row := *enrichment.modelInput
			row.MatchID, row.MatchTime, row.JingcaiID = m.MatchId, drawModelMatch(m).Kickoff, m.JingcaiID
			window := core.window(drawModelMatch(m))
			row.PreMatchHistory = &drawFeatureStudyPreMatchHistory{HomeRecent: window.home, AwayRecent: window.away, HeadToHead: window.h2h}
			row.Result, row.HomeScore, row.AwayScore = -1, -1, -1
			if h, a, confirmed := drawRegulationResult(m); confirmed && known[m.MatchId] && !row.MatchTime.After(now) {
				row.HomeScore, row.AwayScore = h, a
				row.Result = actualOutcomeIndex(h, a)
			}
			data.Rows = append(data.Rows, row)
		}
		data.Audit["mainUniqueMatches"] = len(core.base.targets)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return data, err
}

func researchTakeoverItem(row drawFeatureStudyRow) drawObservationItem {
	item := drawObservationItem{MatchID: row.MatchID, JingcaiID: row.JingcaiID, Date: row.Date, MatchTime: row.MatchTime, League: row.League, Home: row.Home, Guest: row.Guest, FrontendAnalysis: row.Frontend, CompanyQuotes: row.CompanyQuotes, ScoreInputs: row.ScoreInputs}
	for _, company := range row.CompanyQuotes {
		if company.Key == "bet365" {
			item.GoalsLine = company.GoalsLine
			item.AsianHandicap = company.AsianHandicap
			break
		}
	}
	if row.PreMatchHistory != nil {
		item.HomeRecent, item.AwayRecent, item.HeadToHead = row.PreMatchHistory.HomeRecent, row.PreMatchHistory.AwayRecent, row.PreMatchHistory.HeadToHead
	}
	return item
}

// PrimeDrawResearchTakeover compares every current sporttery fixture, archives
// exact-input misses and proves that a second read never runs the legacy model.
// Only the independent research database is written. It starts no service.
func PrimeDrawResearchTakeover(ctx context.Context, db *gorm.DB) (DrawResearchTakeoverVerification, error) {
	r := DrawResearchTakeoverVerification{Version: drawResearchTakeoverVersion, GeneratedAt: time.Now().UTC(), ModelArtifactHash: drawmodeldata.ArtifactSHA256(), Metrics: map[string]*researchMetric{"original": {}, "secondary": {}, "enhanced": {}}}
	data, err := ExportDrawResearchCurrentSource(db.WithContext(ctx), time.Now().UTC())
	if err != nil {
		return r, err
	}
	r.Fixtures = len(data.Rows)
	if n, ok := data.Audit["mainUniqueMatches"].(int); ok {
		r.Fixtures = n
	}
	store, err := OpenResearchStore(ctx)
	if err != nil {
		return r, err
	}
	defer store.Close()
	members := []researchstore.DatasetMember{}
	for _, row := range data.Rows {
		if strings.TrimSpace(row.JingcaiID) == "" {
			continue
		}
		r.Eligible++
		item := researchTakeoverItem(row)
		expected := drawObservationModelSignal(item, &row)
		got, decision := resolveDrawResearchSignalWithStore(ctx, store, item, &row, func() *drawObservationFrozenSignal { return expected })
		if !drawResearchSignalsEqual(expected, got) {
			r.Mismatches++
			return r, fmt.Errorf("当前输入的新旧模型结果不一致：%s", row.MatchID)
		}
		switch decision.Source {
		case "research":
			r.ReadHits++
		case "research_write_through":
			r.WriteThrough++
		default:
			r.Fallback++
			return r, fmt.Errorf("研究库未准备就绪：%s %s", row.MatchID, decision.Reason)
		}
		read, readDecision := resolveDrawResearchSignalWithStore(ctx, store, item, &row, func() *drawObservationFrozenSignal { r.ReadVerificationLegacyCalls++; return expected })
		if readDecision.Source != "research" || !drawResearchSignalsEqual(expected, read) {
			return r, fmt.Errorf("研究库回读未通过：%s %s", row.MatchID, readDecision.Reason)
		}
		r.VerifiedReadHits++
		member := researchstore.DatasetMember{SnapshotID: readDecision.SnapshotID, Split: "research"}
		var provenance string
		if err = store.DB().QueryRowContext(ctx, "SELECT provenance FROM feature_snapshots WHERE id=?", member.SnapshotID).Scan(&provenance); err != nil {
			return r, err
		}
		if provenance == "live" {
			member.Split = "prospective"
		}
		if row.Result >= 0 {
			r.Settled++
			if row.HomeScore == row.AwayScore {
				r.Draws++
			}
			resultID, err := researchTakeoverLabel(ctx, store, row)
			if err != nil {
				return r, err
			}
			member.ResultID = &resultID
			if err = settleResearchPredictions(ctx, store, row.MatchID, resultID, row.HomeScore, row.AwayScore, time.Now().UTC()); err != nil {
				return r, err
			}
			item.FrozenDrawSignal = read
			secondary := drawObservationSecondaryRetained(item)
			enhanced := secondary && drawObservationProfitSupport(item).State == "supported"
			scores := drawObservationScoreRecommendation(item)
			for name, m := range r.Metrics {
				m.Eligible++
				if row.HomeScore == row.AwayScore {
					m.Draws++
				}
				selected := read.State == "selected"
				if name == "secondary" {
					selected = secondary
				}
				if name == "enhanced" {
					selected = enhanced
				}
				if selected {
					m.Selected++
					if row.HomeScore == row.AwayScore {
						m.Hits++
					}
					if secondary {
						m.addScoreResult(scores.Scores, row.HomeScore, row.AwayScore)
					}
				}
			}
		}
		members = append(members, member)
	}
	for _, m := range r.Metrics {
		m.rates()
	}
	r.Ready = r.Mismatches == 0 && r.Fallback == 0 && r.ReadVerificationLegacyCalls == 0 && r.VerifiedReadHits == r.Eligible
	if !r.Ready {
		return r, fmt.Errorf("接管全量对账未通过")
	}
	manifest := researchJSON(map[string]any{"version": r.Version, "featureVersion": researchFeatureVersion, "artifactHash": r.ModelArtifactHash, "members": members, "policy": "exact current input revision; original model and secondary threshold unchanged"})
	id, err := store.SaveDataset(ctx, researchstore.Dataset{Name: "research-takeover-current-inputs", Version: researchstore.HashBytes(manifest), FeatureVersion: researchFeatureVersion, Manifest: manifest, CreatedAt: time.Now().UTC()})
	if err != nil {
		return r, err
	}
	r.DatasetID = id
	for i := range members {
		members[i].DatasetID = id
	}
	if err = store.AddDatasetMembers(ctx, members); err != nil {
		return r, err
	}
	_, err = store.SaveEvaluation(ctx, researchstore.Evaluation{DatasetID: &id, Version: researchEvaluationVersion, Scope: "settled_sporttery_retrospective", Metrics: researchJSON(map[string]any{"groups": r.Metrics, "historicalAssociationOnly": true, "allSourceCaptureTimesVerified": false, "takeoverVerified": true, "takeoverVersion": r.Version, "scoreMethod": drawScoreRecommendationVersion, "secondaryVersion": drawObservationSecondaryVersion}), CreatedAt: time.Now().UTC()})
	if err != nil {
		return r, err
	}
	if err = evaluateResearchProspective(ctx, store); err != nil {
		return r, err
	}
	return r, nil
}

func researchTakeoverLabel(ctx context.Context, store *researchstore.Store, row drawFeatureStudyRow) (int64, error) {
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
	now := time.Now().UTC()
	eventID, _, err := store.RecordLocalSnapshot(ctx, researchstore.IngestEvent{Source: "local_business_snapshot", Kind: "confirmed_regulation_label", MatchID: row.MatchID, ObservedAt: now, Provenance: "reconstructed", Payload: researchJSON(map[string]any{"matchId": row.MatchID, "homeScore": row.HomeScore, "awayScore": row.AwayScore, "regulationConfirmed": true}), ParserVersion: researchPipelineVersion})
	if err != nil {
		return 0, err
	}
	return saveResearchResult(ctx, store, row, eventID, now)
}
