package handlers

import (
	"encoding/json"

	"go_server/internal/drawfullmodel"
)

type drawComprehensiveAlternativeScores struct {
	Home float64 `json:"home"`
	Away float64 `json:"away"`
}

// An independent comparison result. No original signal, secondary threshold,
// archived prediction or target outcome is an input to this decision.
type drawComprehensiveSignal struct {
	ModelVersion           string                              `json:"modelVersion"`
	InputVersion           string                              `json:"inputVersion"`
	ArtifactHash           string                              `json:"artifactHash"`
	State                  string                              `json:"state"`
	Score                  *float64                            `json:"score,omitempty"`
	Threshold              float64                             `json:"threshold"`
	AlternativeDirection   string                              `json:"alternativeDirection,omitempty"`
	AlternativeScores      *drawComprehensiveAlternativeScores `json:"alternativeScores,omitempty"`
	DrawLeafSamples        int                                 `json:"drawLeafSamples"`
	AlternativeLeafSamples int                                 `json:"alternativeLeafSamples"`
	ObservedInputs         int                                 `json:"observedInputs"`
	ObservedEvidence       int                                 `json:"observedEvidence"`
	TotalInputs            int                                 `json:"totalInputs"`
	DrawPath               []drawfullmodel.PathStep            `json:"drawPath"`
	AlternativePath        []drawfullmodel.PathStep            `json:"alternativePath"`
	ModuleCoverage         []drawfullmodel.ModuleCoverage      `json:"moduleCoverage"`
	Reason                 string                              `json:"reason"`
	AlternativeReason      string                              `json:"alternativeReason,omitempty"`
}

func drawObservationComprehensiveSignal(input json.RawMessage) *drawComprehensiveSignal {
	info := drawfullmodel.Info()
	signal := &drawComprehensiveSignal{ModelVersion: info.Version, InputVersion: info.InputVersion, ArtifactHash: info.ArtifactHash, State: "unavailable", Threshold: info.Threshold,
		DrawPath: []drawfullmodel.PathStep{}, AlternativePath: []drawfullmodel.PathStep{}, ModuleCoverage: []drawfullmodel.ModuleCoverage{}, Reason: info.Reason}
	if !info.Available {
		return signal
	}
	if len(input) == 0 {
		signal.Reason = "尚无本场全量分析输入"
		return signal
	}
	features, err := drawfullmodel.ExtractFeatures(input)
	if err != nil {
		signal.Reason = "本场全量分析输入无法读取"
		return signal
	}
	result := drawfullmodel.Predict(features)
	signal.ObservedInputs, signal.ObservedEvidence, signal.TotalInputs = result.ObservedInputs, result.ObservedEvidence, result.TotalInputs
	signal.DrawPath, signal.AlternativePath, signal.ModuleCoverage, signal.Reason = result.DrawPath, result.AlternativePath, result.ModuleCoverage, result.Reason
	if !result.Available {
		return signal
	}
	signal.State = "not_selected"
	if result.Selected {
		signal.State = "selected"
		signal.AlternativeDirection = result.AlternativeDirection
		signal.AlternativeReason = result.AlternativeReason
	}
	score := result.Score
	signal.Score = &score
	signal.AlternativeScores = &drawComprehensiveAlternativeScores{Home: result.HomeScore, Away: result.AwayScore}
	signal.DrawLeafSamples, signal.AlternativeLeafSamples = result.DrawLeafSamples, result.AlternativeLeafSamples
	return signal
}
