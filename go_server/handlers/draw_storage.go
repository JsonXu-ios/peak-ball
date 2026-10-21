package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"go_server/database"
	"go_server/internal/drawmodel"
	"go_server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var drawTrainingMu sync.Mutex

type drawCalibrationBin struct {
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Count     int     `json:"count"`
	Predicted float64 `json:"predicted"`
	Actual    float64 `json:"actual"`
}

type drawStatsResponse struct {
	Predictions        int                  `json:"predictions"`
	Settled            int                  `json:"settled"`
	Candidates         int                  `json:"candidates"`
	CandidateSettled   int                  `json:"candidateSettled"`
	CandidateHits      int                  `json:"candidateHits"`
	HitRate            *float64             `json:"hitRate"`
	Coverage           *float64             `json:"coverage"`
	Brier              *float64             `json:"brier"`
	MarketBrier        *float64             `json:"marketBrier"`
	PairedModelBrier   *float64             `json:"pairedModelBrier"`
	MarketSamples      int                  `json:"marketSamples"`
	BaselineBrier      *float64             `json:"baselineBrier"`
	CalibrationBins    []drawCalibrationBin `json:"calibrationBins"`
	ModelVersion       string               `json:"modelVersion"`
	Calibrated         bool                 `json:"calibrated"`
	TrainingSamples    int                  `json:"trainingSamples"`
	MinTrainingSamples int                  `json:"minTrainingSamples"`
	CanTrain           bool                 `json:"canTrain"`
	Note               string               `json:"note"`
}

func validDrawProbability(p float64) bool {
	return !math.IsNaN(p) && !math.IsInf(p, 0) && p >= 0 && p <= 1
}

// buildDrawStats scores only immutable, pre-match forecasts. Market comparison
// uses the identical subset for both models instead of filling missing odds.
func buildDrawStats(rows []models.DrawPrediction) drawStatsResponse {
	stats := drawStatsResponse{
		Predictions: len(rows), CalibrationBins: []drawCalibrationBin{},
		ModelVersion: drawmodel.Version,
		Note:         "仅统计首次赛前冻结记录；命中率按候选场次计算。Brier 越低越好，市场比较使用同一有市场赔率的样本子集。样本积累后再判断筛选增益。",
	}
	var probabilitySum, baselineSum, marketSum, pairedModelSum float64
	var probabilityCount, baselineCount int
	var bins [10]struct {
		count             int
		predicted, actual float64
	}
	for _, row := range rows {
		if row.Candidate {
			stats.Candidates++
		}
		if !row.Settled || row.IsDraw == nil || row.PredictionAt.IsZero() || !row.PredictionAt.Before(row.Kickoff) {
			continue
		}
		stats.Settled++
		actual := 0.0
		if *row.IsDraw {
			actual = 1
		}
		if row.Candidate {
			stats.CandidateSettled++
			if *row.IsDraw {
				stats.CandidateHits++
			}
		}
		if validDrawProbability(row.Probability) {
			probabilitySum += math.Pow(row.Probability-actual, 2)
			probabilityCount++
			index := int(row.Probability * 10)
			if index == 10 {
				index = 9
			}
			bins[index].count++
			bins[index].predicted += row.Probability
			bins[index].actual += actual
			if row.MarketProbability != nil && validDrawProbability(*row.MarketProbability) {
				stats.MarketSamples++
				marketSum += math.Pow(*row.MarketProbability-actual, 2)
				pairedModelSum += math.Pow(row.Probability-actual, 2)
			}
		}
		if validDrawProbability(row.BaselineProbability) {
			baselineSum += math.Pow(row.BaselineProbability-actual, 2)
			baselineCount++
		}
	}
	stats.HitRate = drawRatio(stats.CandidateHits, stats.CandidateSettled, 100)
	stats.Coverage = drawRatio(stats.Candidates, stats.Predictions, 100)
	stats.Brier = drawAverage(probabilitySum, probabilityCount)
	stats.BaselineBrier = drawAverage(baselineSum, baselineCount)
	stats.MarketBrier = drawAverage(marketSum, stats.MarketSamples)
	stats.PairedModelBrier = drawAverage(pairedModelSum, stats.MarketSamples)
	for index, bin := range bins {
		if bin.count == 0 {
			continue
		}
		stats.CalibrationBins = append(stats.CalibrationBins, drawCalibrationBin{
			From: float64(index * 10), To: float64((index + 1) * 10), Count: bin.count,
			Predicted: 100 * bin.predicted / float64(bin.count), Actual: 100 * bin.actual / float64(bin.count),
		})
	}
	return stats
}

func drawAverage(total float64, count int) *float64 {
	if count == 0 {
		return nil
	}
	result := total / float64(count)
	return &result
}

func drawRatio(numerator, denominator int, scale float64) *float64 {
	return drawAverage(float64(numerator)*scale, denominator)
}

// MySQL may store datetime values at lower precision than JSON timestamps.
// Accept only its sub-second rounding; all pre-kickoff checks remain strict.
func drawSameStoredTime(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	difference := a.Sub(b)
	return difference > -time.Second && difference < time.Second
}

// drawRegulationResult rejects any ambiguous extra-time/shoot-out match. The
// crawler currently exposes no separately verified 90-minute score for those
// records, so skipping them is safer than treating a final total as regulation.
func drawRegulationResult(match models.Money) (home, away int, ok bool) {
	if !drawCompleted(match) || match.HomeScore < 0 || match.GuestScore < 0 {
		return 0, 0, false
	}
	state := strings.ToLower(match.DisplayState)
	if match.HomeOtScore != 0 || match.GuestOtScore != 0 || match.HomeOtPenalty != 0 || match.GuestOtPenalty != 0 ||
		strings.Contains(state, "加时") || strings.Contains(state, "点球") || strings.Contains(state, "aet") || strings.Contains(state, "pen") || state == "et" {
		return 0, 0, false
	}
	return match.HomeScore, match.GuestScore, true
}

// drawObservedResultTime uses the source row's recorded update as an upper
// bound on when the result was known, with a conservative two-hour match
// duration. It is distinct from when this module happens to reconcile results.
func drawObservedResultTime(match models.Money, kickoff, now time.Time) time.Time {
	if match.UpdatedAt.IsZero() || match.UpdatedAt.After(now) {
		return now
	}
	available := match.UpdatedAt
	if !match.MatchTime.IsZero() {
		kickoff = drawSourceTime(match.MatchTime)
	}
	if earliest := kickoff.Add(2 * time.Hour); available.Before(earliest) {
		available = earliest
	}
	if available.After(now) {
		return now
	}
	return available
}

// settleDrawPredictions only updates settlement columns. It never recomputes
// probabilities, eligibility, features or payload after kickoff.
func settleDrawPredictions() error {
	var predictions []models.DrawPrediction
	if err := database.DB.Where("settled = ? AND kickoff < ?", false, time.Now()).Find(&predictions).Error; err != nil {
		return err
	}
	if len(predictions) == 0 {
		return nil
	}
	ids := make([]string, 0, len(predictions))
	for _, prediction := range predictions {
		ids = append(ids, prediction.MatchID)
	}
	var matches []models.Money
	if err := database.DB.Where("match_id IN ?", ids).Find(&matches).Error; err != nil {
		return err
	}
	byID := make(map[string]models.Money, len(matches))
	for _, match := range matches {
		byID[match.MatchId] = match
	}
	now := time.Now().UTC()
	for _, prediction := range predictions {
		match, found := byID[prediction.MatchID]
		if !found || prediction.PredictionAt.IsZero() || !prediction.PredictionAt.Before(prediction.Kickoff) {
			continue
		}
		home, away, completed := drawRegulationResult(match)
		if !completed || drawSourceTime(match.MatchTime).After(now) {
			continue
		}
		availableAt := drawObservedResultTime(match, prediction.Kickoff, now)
		if err := database.DB.Model(&models.DrawPrediction{}).Where("id = ? AND settled = ?", prediction.ID, false).Updates(map[string]any{
			"settled": true, "is_draw": home == away, "home_score": home, "away_score": away, "settled_at": now, "result_available_at": availableAt,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func loadActiveDrawArtifact() (*drawmodel.Artifact, error) {
	var run models.DrawModelRun
	err := database.DB.Where("active = ? AND calibrated = ?", true, true).Order("id DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var artifact drawmodel.Artifact
	if err := json.Unmarshal(run.Artifact, &artifact); err != nil {
		return nil, fmt.Errorf("读取平局模型失败: %w", err)
	}
	if !artifact.Fitted || !artifact.Calibrated || artifact.Version != drawmodel.Version {
		return nil, nil
	}
	return &artifact, nil
}

// GetDrawStats evaluates saved forecasts rather than recalculating predictions
// using information that became available after kickoff.
func GetDrawStats(c *gin.Context) {
	if err := settleDrawPredictions(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var predictions []models.DrawPrediction
	if err := database.DB.Find(&predictions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stats := buildDrawStats(predictions)
	artifact, err := loadActiveDrawArtifact()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if artifact != nil {
		stats.ModelVersion, stats.Calibrated = drawArtifactVersion(artifact), artifact.Calibrated
	}
	examples, err := loadDrawTrainingExamples()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stats.TrainingSamples = len(examples)
	stats.MinTrainingSamples = drawmodel.MinTrainingSamples
	stats.CanTrain = stats.TrainingSamples >= stats.MinTrainingSamples
	c.JSON(http.StatusOK, stats)
}

// drawTrainingExamples validates snapshot identity and chronology before a
// settled result can become a training label. The original target's goals are
// replaced by the regulation-time result only after pre-match inputs are read.
func drawTrainingExamples(predictions []models.DrawPrediction, snapshots map[uint64]models.DrawFeatureSnapshot) []drawmodel.TrainingExample {
	examples := make([]drawmodel.TrainingExample, 0, len(predictions))
	seen := map[string]bool{}
	for _, prediction := range predictions {
		if seen[prediction.MatchID] || !prediction.Settled || prediction.IsDraw == nil || prediction.HomeScore == nil || prediction.AwayScore == nil || prediction.SettledAt == nil ||
			prediction.PredictionAt.IsZero() || !prediction.PredictionAt.Before(prediction.Kickoff) || prediction.SettledAt.Before(prediction.Kickoff) {
			continue
		}
		snapshot, found := snapshots[prediction.SnapshotID]
		if !found || snapshot.MatchID != prediction.MatchID || !drawSameStoredTime(snapshot.Kickoff, prediction.Kickoff) || !snapshot.AsOf.Before(snapshot.Kickoff) {
			continue
		}
		var input drawmodel.Input
		if json.Unmarshal(snapshot.Input, &input) != nil || input.Target.ID != prediction.MatchID || input.Target.Completed ||
			input.AsOf.IsZero() || !drawSameStoredTime(input.AsOf, snapshot.AsOf) || !drawSameStoredTime(input.AsOf, prediction.PredictionAt) || !drawSameStoredTime(input.Target.Kickoff, snapshot.Kickoff) || !input.AsOf.Before(input.Target.Kickoff) {
			continue
		}
		if *prediction.HomeScore < 0 || *prediction.AwayScore < 0 || (*prediction.HomeScore == *prediction.AwayScore) != *prediction.IsDraw {
			continue
		}
		availableAt := *prediction.SettledAt
		if prediction.ResultAvailableAt != nil {
			if prediction.ResultAvailableAt.IsZero() || prediction.ResultAvailableAt.Before(prediction.Kickoff) || prediction.ResultAvailableAt.After(*prediction.SettledAt) {
				continue
			}
			availableAt = *prediction.ResultAvailableAt
		}
		input.Target.Completed = true
		input.Target.CompletedAt = availableAt
		input.Target.HomeGoals, input.Target.AwayGoals = *prediction.HomeScore, *prediction.AwayScore
		examples = append(examples, drawmodel.TrainingExample{
			Target: input.Target, AsOf: input.AsOf, History: input.History, Odds: input.Odds, Draw: *prediction.IsDraw,
		})
		seen[prediction.MatchID] = true
	}
	sort.Slice(examples, func(i, j int) bool { return examples[i].AsOf.Before(examples[j].AsOf) })
	return examples
}

func loadDrawTrainingExamples() ([]drawmodel.TrainingExample, error) {
	var predictions []models.DrawPrediction
	if err := database.DB.Where("settled = ?", true).Order("prediction_at ASC").Find(&predictions).Error; err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(predictions))
	for _, prediction := range predictions {
		ids = append(ids, prediction.SnapshotID)
	}
	snapshots := make(map[uint64]models.DrawFeatureSnapshot, len(ids))
	if len(ids) > 0 {
		var rows []models.DrawFeatureSnapshot
		if err := database.DB.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, snapshot := range rows {
			snapshots[snapshot.ID] = snapshot
		}
	}
	return drawTrainingExamples(predictions, snapshots), nil
}

// TrainDrawModel stores every run and atomically activates only a fitted and
// calibrated artifact. Rejected fits leave the previous active model intact.
func TrainDrawModel(c *gin.Context) {
	if !drawTrainingMu.TryLock() {
		c.JSON(http.StatusConflict, gin.H{"error": "平局模型正在训练，请稍后重试"})
		return
	}
	defer drawTrainingMu.Unlock()
	if err := settleDrawPredictions(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	examples, err := loadDrawTrainingExamples()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	artifact := drawmodel.Fit(examples)
	encoded, err := json.Marshal(artifact)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	qualified := artifact.Fitted && artifact.Calibrated && !artifact.TrainingCutoff.After(time.Now())
	run := models.DrawModelRun{
		ModelVersion: drawArtifactVersion(&artifact), Artifact: datatypes.JSON(encoded), Samples: len(examples),
		Calibrated: artifact.Calibrated, Active: qualified, TrainedAt: time.Now().UTC(),
	}
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if qualified {
			if err := tx.Model(&models.DrawModelRun{}).Where("active = ?", true).Update("active", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(&run).Error
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	message := "已使用冻结样本完成按时间分割的拟合与校准，模型供后续赛前预测使用；仍需用未来封存结果验证效果。"
	if !qualified {
		message = fmt.Sprintf("模型未激活：现有有效赛前冻结完赛样本 %d 场，最低要求 %d 场，且需满足按时间分割与标签数量要求。", len(examples), drawmodel.MinTrainingSamples)
		if len(artifact.Warnings) > 0 {
			message += " " + strings.Join(artifact.Warnings, "；")
		}
	}
	c.JSON(http.StatusOK, gin.H{"modelVersion": run.ModelVersion, "calibrated": qualified, "samples": len(examples), "message": message})
}

func drawArtifactVersion(artifact *drawmodel.Artifact) string {
	if artifact == nil || !artifact.Fitted || artifact.TrainingCutoff.IsZero() {
		return drawmodel.Version
	}
	return artifact.Version + "-" + artifact.TrainingCutoff.UTC().Format("20060102T150405Z")
}
