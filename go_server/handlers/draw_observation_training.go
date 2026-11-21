package handlers

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go_server/internal/drawmodeldata"
)

// Training rows contain the exact same named inputs/guards used at serving.
// Labels and identifiers live beside Features and are never passed as inputs.
type DrawObservationTrainingRow struct {
	MatchID          string             `json:"matchId"`
	Date             string             `json:"date"`
	MatchTime        time.Time          `json:"matchTime"`
	JingcaiID        string             `json:"jingcaiId"`
	Result           int                `json:"result"`
	Eligible         bool               `json:"eligible"`
	Alternative      int                `json:"alternative"`
	Features         map[string]float64 `json:"features"`
	BaselineScore    float64            `json:"baselineScore"`
	BaselineObserved int                `json:"baselineObserved"`
}
type DrawObservationTrainingSnapshot struct {
	Version           string                       `json:"version"`
	GeneratedAt       time.Time                    `json:"generatedAt"`
	FeatureCodeSHA256 string                       `json:"featureCodeSHA256"`
	DataSHA256        string                       `json:"dataSHA256"`
	Features          []drawmodeldata.InputFeature `json:"features"`
	Rows              []DrawObservationTrainingRow `json:"rows"`
	RowHashes         map[string]string            `json:"rowHashes"`
}

func BuildDrawObservationTrainingSnapshot(source DrawFeatureStudyDataset) (DrawObservationTrainingSnapshot, error) {
	result := DrawObservationTrainingSnapshot{Version: "draw-observation-training-v1", GeneratedAt: source.GeneratedAt,
		FeatureCodeSHA256: drawTrainingFeatureHash, Features: drawmodeldata.BaselineFeatures(),
		Rows: []DrawObservationTrainingRow{}, RowHashes: map[string]string{}}
	needed := map[string]bool{}
	for _, f := range result.Features {
		needed[f.Name] = true
		for _, g := range f.Guards {
			needed[g.Feature] = true
		}
	}
	for _, row := range source.Rows {
		if row.Result < 0 || row.Result > 2 || row.MatchTime.IsZero() || row.MatchTime.After(source.GeneratedAt) {
			continue
		}
		if strings.TrimSpace(row.MatchID) == "" {
			return result, fmt.Errorf("training match ID is missing")
		}
		if _, exists := result.RowHashes[row.MatchID]; exists {
			return result, fmt.Errorf("duplicate training match ID")
		}
		raw, err := json.Marshal(row)
		if err != nil {
			return result, err
		}
		features, err := drawmodeldata.ExtractFeatures(raw)
		if err != nil {
			return result, err
		}
		for name, v := range features {
			if !needed[name] || math.IsNaN(v) || math.IsInf(v, 0) {
				delete(features, name)
			}
		}
		prediction := drawmodeldata.PredictBaseline(features)
		alternative := -1
		prices := row.Latest.Average
		if len(prices) == 3 && prices[0] > 1 && prices[2] > 1 && prices[0] != prices[2] {
			alternative = 0
			if prices[2] < prices[0] {
				alternative = 2
			}
		}
		eligible := strings.TrimSpace(row.JingcaiID) == ""
		if row.PreMatchHistory != nil {
			eligible = eligible || drawObservationHeadToHeadSample(drawObservationItem{MatchID: row.MatchID, MatchTime: row.MatchTime, HeadToHead: row.PreMatchHistory.HeadToHead}) >= drawObservationMinimumHeadToHead
		}
		item := DrawObservationTrainingRow{row.MatchID, row.Date, row.MatchTime, row.JingcaiID, row.Result, eligible, alternative, features, prediction.Score, prediction.ObservedFeatures}
		body, err := json.Marshal(item)
		if err != nil {
			return result, err
		}
		result.RowHashes[row.MatchID] = fmt.Sprintf("%x", sha256.Sum256(body))
		result.Rows = append(result.Rows, item)
	}
	sort.Slice(result.Rows, func(i, j int) bool {
		a, b := result.Rows[i], result.Rows[j]
		if a.MatchTime.Equal(b.MatchTime) {
			return a.MatchID < b.MatchID
		}
		return a.MatchTime.Before(b.MatchTime)
	})
	body, err := json.Marshal(struct {
		Version, Features string
		Rows              map[string]string
	}{result.Version, result.FeatureCodeSHA256, result.RowHashes})
	if err != nil {
		return result, err
	}
	result.DataSHA256 = fmt.Sprintf("%x", sha256.Sum256(body))
	return result, nil
}
