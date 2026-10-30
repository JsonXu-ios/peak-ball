package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"go_server/internal/drawmodeldata"
	"peakball/researchstore"
)

// Evaluate exactly the first available frozen pre-match prediction per match
// and model/rule/score version. Reconstructed inputs and later, better-looking
// forecasts cannot enter this prospective cohort. Pending matches stay pending.
func evaluateResearchProspective(ctx context.Context, store *researchstore.Store) error {
	rule := drawObservationSecondaryVersion + "+" + drawObservationProfitSupportVersion
	rows, err := store.DB().QueryContext(ctx, `
SELECT p.id,p.snapshot_id,p.state,p.secondary_selected,p.enhanced,p.scorelines,r.id,r.home_score,r.away_score,r.regulation_confirmed
FROM predictions p
JOIN feature_snapshots s ON s.id=p.snapshot_id
LEFT JOIN match_results r ON r.id=(SELECT r2.id FROM match_results r2 WHERE r2.match_id=p.match_id ORDER BY r2.observed_at DESC,r2.id DESC LIMIT 1)
WHERE s.provenance='live' AND s.as_of<s.kickoff AND p.predicted_at<s.kickoff
AND s.feature_version=? AND p.state IN ('selected','not_selected')
AND p.model_version=? AND p.rule_version=? AND p.score_method=?
AND NOT EXISTS (
 SELECT 1 FROM predictions earlier JOIN feature_snapshots es ON es.id=earlier.snapshot_id
 WHERE earlier.match_id=p.match_id AND earlier.model_version=p.model_version
 AND earlier.rule_version=p.rule_version AND earlier.score_method=p.score_method
 AND earlier.state IN ('selected','not_selected') AND es.provenance='live' AND es.feature_version=s.feature_version
 AND es.as_of<es.kickoff AND earlier.predicted_at<es.kickoff
 AND (earlier.predicted_at<p.predicted_at OR (earlier.predicted_at=p.predicted_at AND earlier.id<p.id))
)
ORDER BY p.match_id`, researchFeatureVersion, drawmodeldata.Version(), rule, drawScoreRecommendationVersion)
	if err != nil {
		return err
	}
	members := []researchstore.DatasetMember{}
	metrics := map[string]*researchMetric{"original": {}, "secondary": {}, "enhanced": {}}
	pending, settled := 0, 0
	for rows.Next() {
		var pid, snapshotID int64
		var state string
		var secondary, enhanced bool
		var scoreJSON []byte
		var resultID, h, a sql.NullInt64
		var confirmed sql.NullBool
		if err = rows.Scan(&pid, &snapshotID, &state, &secondary, &enhanced, &scoreJSON, &resultID, &h, &a, &confirmed); err != nil {
			rows.Close()
			return err
		}
		member := researchstore.DatasetMember{SnapshotID: snapshotID, Split: "prospective"}
		var scorelines []drawRecommendedScore
		if err = json.Unmarshal(scoreJSON, &scorelines); err != nil {
			rows.Close()
			return err
		}
		if resultID.Valid && h.Valid && a.Valid && confirmed.Valid && confirmed.Bool {
			id := resultID.Int64
			member.ResultID = &id
			settled++
			for key, m := range metrics {
				m.Eligible++
				if h.Int64 == a.Int64 {
					m.Draws++
				}
				selected := state == "selected"
				if key == "secondary" {
					selected = secondary
				}
				if key == "enhanced" {
					selected = enhanced
				}
				if selected {
					m.Selected++
					if secondary {
						m.addScoreResult(scorelines, int(h.Int64), int(a.Int64))
					}
					if h.Int64 == a.Int64 {
						m.Hits++
					}
				}
			}
		} else {
			pending++
		}
		members = append(members, member)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range metrics {
		m.rates()
	}
	manifest := researchJSON(map[string]any{"policy": "first_available_pre_match_prediction_per_match_model_rule_score_version", "featureVersion": researchFeatureVersion, "members": members, "modelVersion": drawmodeldata.Version(), "artifactHash": drawmodeldata.ArtifactSHA256(), "reconstructedExcluded": true})
	datasetID, err := store.SaveDataset(ctx, researchstore.Dataset{Name: "prospective-first-predictions", Version: researchstore.HashBytes(manifest), FeatureVersion: researchFeatureVersion, Manifest: manifest, CreatedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	for i := range members {
		members[i].DatasetID = datasetID
	}
	if err = store.AddDatasetMembers(ctx, members); err != nil {
		return err
	}
	_, err = store.SaveEvaluation(ctx, researchstore.Evaluation{DatasetID: &datasetID, Version: researchEvaluationVersion, Scope: "sporttery_prospective_first_prediction", Metrics: researchJSON(map[string]any{"groups": metrics, "pending": pending, "settled": settled, "policy": "first available prediction; one match once", "scoreMethod": drawScoreRecommendationVersion, "secondaryVersion": drawObservationSecondaryVersion, "independentOfHistoricalReconstruction": true}), CreatedAt: time.Now().UTC()})
	if err != nil {
		return fmt.Errorf("save prospective evaluation: %w", err)
	}
	return nil
}
