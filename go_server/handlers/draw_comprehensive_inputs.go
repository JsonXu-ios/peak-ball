package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go_server/internal/drawfullmodel"
	"go_server/models"
	"gorm.io/gorm"
)

// BuildDrawComprehensiveInput is a separate, label-blind model projection.
// Neither the original row nor its original JSON/hash is changed. The extra
// container deliberately omits fixture identity, current result and hit flags.
func BuildDrawComprehensiveInput(row *drawFeatureStudyRow, response *analysisMatchResponse) (json.RawMessage, error) {
	if row == nil {
		return nil, fmt.Errorf("missing comprehensive source row")
	}
	base, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(base, &result); err != nil {
		return nil, err
	}
	full := map[string]interface{}{}
	if response != nil {
		platform := response.Platform
		if platform == nil {
			platform = row.AnalysisInputs.Platform
		}
		if platform == nil {
			platform = buildPlatformDecision(response)
		}
		full = map[string]interface{}{
			"goddessWoman": response.GoddessWoman, "platform": platform,
			"roiSimulation": response.RoiSimulation,
			"homeRank":      response.HomeRank, "guestRank": response.GuestRank,
			"prediction": response.Prediction, "qiuPrediction": response.QiuPrediction,
			"historyGoalAverage":      response.HistoryGoalAverage,
			"recentGoalAverage":       response.RecentGoalAverage,
			"combinedGoalAverage":     response.CombinedGoalAverage,
			"combinedHandicapAverage": response.CombinedHandicapAverage,
		}
		parts := strings.Split(response.ChangGuiYaPan, ":")
		if len(parts) == 2 {
			full["historyGoalDifference"] = parts[0]
			full["recentGoalDifference"] = parts[1]
		}
		for _, alert := range platform.GoalBalanceAlertRows {
			if alert.Label == "2.5均衡值" {
				text := strings.TrimPrefix(alert.Value, "测算")
				full["goalBalanceValue"] = strings.Split(text, "，")[0]
			}
		}
	}
	encoded, err := json.Marshal(full)
	if err != nil {
		return nil, err
	}
	result["fullAnalysis"] = encoded
	result["fullInputVersion"], _ = json.Marshal(drawfullmodel.InputVersion)
	return json.Marshal(result)
}

type DrawComprehensiveStudyDataset struct {
	GeneratedAt time.Time              `json:"generatedAt"`
	Version     string                 `json:"version"`
	Audit       map[string]interface{} `json:"audit"`
	Rows        []json.RawMessage      `json:"rows"`
}

// ExportDrawComprehensiveStudy uses a single outer read-only MVCC snapshot.
// The original exporter is reused under a nested savepoint, then each source
// table is loaded once in batches. No server/listener/schema/Redis is touched.
func ExportDrawComprehensiveStudy(db *gorm.DB, now time.Time) (DrawComprehensiveStudyDataset, error) {
	result := DrawComprehensiveStudyDataset{GeneratedAt: now.UTC(), Version: drawfullmodel.InputVersion, Audit: map[string]interface{}{}, Rows: []json.RawMessage{}}
	err := db.Transaction(func(tx *gorm.DB) error {
		base, err := ExportDrawFeatureStudy(tx, now)
		if err != nil {
			return err
		}
		result.Audit = base.Audit
		result.Audit["comprehensiveInputVersion"] = drawfullmodel.InputVersion
		result.Audit["comprehensiveIsolation"] = "Separate fullAnalysis projection; original frozen row, model, hash and rules are unchanged"
		result.Audit["comprehensiveSource"] = "All original page modules, including goddess and real/simulated handicap. Synthetic derived outputs are identified in feature metadata. Current labels/identity never extracted."
		result.Audit["missingPolicy"] = "Absent source stays missing; history defaults are masked; unsafe history-derived outputs are unavailable. Timing remains retrospective unless capture is verified."
		schema, err := drawObservationReadSchema(tx)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(base.Rows))
		for _, row := range base.Rows {
			ids = append(ids, row.MatchID)
		}
		main := map[string]models.Money{}
		for _, table := range []string{"money", "moneys"} {
			if len(ids) == 0 || !schema[table]["match_id"] {
				continue
			}
			var rows []models.Money
			if err = tx.Table(table).Select(drawObservationProjection(schema, table, drawObservationFrontendMainColumns)).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				main[row.MatchId] = row
			}
		}
		histories := map[string]models.HistoryMoney{}
		odds := map[string]models.OddsMoney{}
		pankou := map[string]models.PankouMoney{}
		if len(ids) > 0 && schema["history_moneys"]["match_id"] {
			var rows []models.HistoryMoney
			if err = tx.Table("history_moneys").Select(drawObservationProjection(schema, "history_moneys", drawObservationFrontendHistoryColumns)).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				histories[row.MatchId] = row
			}
		}
		if len(ids) > 0 && schema["odds_moneys"]["match_id"] {
			var rows []models.OddsMoney
			if err = tx.Table("odds_moneys").Select(drawObservationProjection(schema, "odds_moneys", drawObservationFrontendOddsColumns)).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				odds[row.MatchId] = row
			}
		}
		if len(ids) > 0 && schema["pankou_moneys"]["match_id"] {
			var rows []models.PankouMoney
			if err = tx.Table("pankou_moneys").Select(drawObservationProjection(schema, "pankou_moneys", drawObservationFrontendPankouColumns)).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				pankou[row.MatchId] = row
			}
		}
		for i := range base.Rows {
			row := &base.Rows[i]
			match, ok := main[row.MatchID]
			if !ok {
				return fmt.Errorf("source fixture unavailable inside comprehensive snapshot")
			}
			response := buildAnalysisFromData(match, histories[row.MatchID], odds[row.MatchID], pankou[row.MatchID], sportteryTradeFromJSON(odds[row.MatchID].SportteryTrade))
			attachAnalysisWeights(&response)
			input, err := BuildDrawComprehensiveInput(row, &response)
			if err != nil {
				return err
			}
			result.Rows = append(result.Rows, input)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
