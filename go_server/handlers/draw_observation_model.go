package handlers

import (
	"encoding/json"
	"strings"

	"go_server/internal/drawmodeldata"
)

type drawObservationFrozenSignal struct {
	ModelVersion      string   `json:"modelVersion"`
	State             string   `json:"state"`
	Score             *float64 `json:"score,omitempty"`
	Threshold         float64  `json:"threshold"`
	Reason            string   `json:"reason,omitempty"`
	ObservedInputs    int      `json:"observedInputs,omitempty"`
	UsedInputs        int      `json:"usedInputs,omitempty"`
	SelectionVersion  string   `json:"selectionVersion,omitempty"`
	MinimumHeadToHead int      `json:"minimumHeadToHead,omitempty"`
	HeadToHeadSample  int      `json:"headToHeadSample"`
}

// Target results never determine selection. The UI separately checks regulation
// settlement to label an already selected match as a hit or a non-draw.
func drawObservationModelSignal(item drawObservationItem, input *drawFeatureStudyRow) *drawObservationFrozenSignal {
	if strings.TrimSpace(item.JingcaiID) == "" {
		return &drawObservationFrozenSignal{ModelVersion: drawmodeldata.Version(), Threshold: drawmodeldata.Threshold(), State: "out_of_scope", Reason: "仅观察有竞彩编号的比赛"}
	}
	return drawObservationFrozenModelSignal(item, input)
}

// Both independently scoped observation columns use the exact frozen numeric
// calculation. Eligibility stays in their callers; no fixture identity is forged.
func drawObservationFrozenModelSignal(item drawObservationItem, input *drawFeatureStudyRow) *drawObservationFrozenSignal {
	result := &drawObservationFrozenSignal{ModelVersion: drawmodeldata.Version(), Threshold: drawmodeldata.Threshold(), State: "unavailable"}
	if input == nil {
		result.Reason = "尚无本场模型输入"
		return result
	}
	// Input construction and prediction use the same strict preceding matches
	// shown in this row. Copy before assigning, since enrichment can be shared.
	row := *input
	row.MatchID, row.MatchTime = item.MatchID, item.MatchTime
	row.PreMatchHistory = &drawFeatureStudyPreMatchHistory{HomeRecent: item.HomeRecent, AwayRecent: item.AwayRecent, HeadToHead: item.HeadToHead}
	data, err := json.Marshal(row)
	if err != nil {
		result.Reason = "模型输入无法读取"
		return result
	}
	features, err := drawmodeldata.ExtractModelFeatures(data)
	if err != nil {
		result.Reason = "模型输入无法读取"
		return result
	}
	prediction := drawmodeldata.Predict(features)
	if prediction.ObservedFeatures == 0 {
		result.Reason = "本场暂无可用赔率或赛前战绩"
		return result
	}
	result.Score = &prediction.Score
	result.ObservedInputs, result.UsedInputs = prediction.ObservedFeatures, prediction.UsedFeatures
	result.State = "not_selected"
	if prediction.Selected {
		result.State = "selected"
	}
	return result
}
