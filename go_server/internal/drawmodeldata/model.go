// Package drawmodeldata contains the frozen sporttery draw observation model.
// Its historical fit is not an independently validated probability guarantee.
package drawmodeldata

import (
	_ "embed"
	"encoding/json"
	"math"
)

//go:embed observation_v1.json
var modelJSON []byte

type Guard struct {
	Feature string  `json:"feature"`
	Minimum float64 `json:"minimum"`
}

type InputFeature struct {
	Name          string  `json:"name"`
	OriginalIndex int     `json:"originalIndex"`
	Median        float64 `json:"median"`
	Guards        []Guard `json:"guards"`
	UsedInTrees   bool    `json:"usedInTrees"`
	View          string  `json:"view"`
	Label         string  `json:"label"`
}

type node struct {
	Leaf      bool    `json:"leaf"`
	Value     float64 `json:"value"`
	Feature   int     `json:"feature"`
	Threshold float64 `json:"threshold"`
	Left      int     `json:"left"`
	Right     int     `json:"right"`
}

type artifact struct {
	Schema    int            `json:"schema"`
	Version   string         `json:"version"`
	ModelID   string         `json:"modelId"`
	Threshold float64        `json:"threshold"`
	Baseline  float64        `json:"baseline"`
	Features  []InputFeature `json:"features"`
	Trees     [][]node       `json:"trees"`
}

var frozen = mustLoadModel()

func mustLoadModel() artifact {
	var model artifact
	if err := json.Unmarshal(modelJSON, &model); err != nil {
		panic("invalid embedded draw observation model: " + err.Error())
	}
	if model.Schema != 1 || len(model.Features) != 391 || len(model.Trees) != 240 {
		panic("unexpected embedded draw observation model schema")
	}
	return model
}

// Result reports model selection, not the actual eventual match result.
// ObservedFeatures counts finite guarded inputs that appear in real tree splits.
// A caller may display unavailable when this is zero; the score still describes
// the exact frozen median fallback, which is useful for reproducibility tests.
type Result struct {
	Score            float64 `json:"score"`
	Selected         bool    `json:"selected"`
	ObservedFeatures int     `json:"observedFeatures"`
	UsedFeatures     int     `json:"usedFeatures"`
}

func Version() string    { return frozen.Version }
func ModelID() string    { return frozen.ModelID }
func Threshold() float64 { return frozen.Threshold }

// Features returns a copy of the frozen ordered input/guard contract.
func Features() []InputFeature {
	result := append([]InputFeature(nil), frozen.Features...)
	for i := range result {
		result[i].Guards = append([]Guard(nil), result[i].Guards...)
	}
	return result
}

func UsedFeatureNames() []string {
	result := make([]string, 0, 202)
	for _, feature := range frozen.Features {
		if feature.UsedInTrees {
			result = append(result, feature.Name)
		}
	}
	return result
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// Predict applies original historical sample guards, fixed median imputation,
// then the exact 240 numeric trees. It never reads match IDs, status or scores.
// features must contain raw (not imputed) named numeric observations. Omitted
// keys and nonfinite numbers mean unknown; an observed zero remains zero.
func Predict(features map[string]float64) Result {
	data := make([]float64, len(frozen.Features))
	result := Result{}
	for index, feature := range frozen.Features {
		value, valid := features[feature.Name]
		valid = valid && finite(value)
		for _, guard := range feature.Guards {
			observed, present := features[guard.Feature]
			valid = valid && present && finite(observed) && observed >= guard.Minimum
		}
		if feature.UsedInTrees {
			result.UsedFeatures++
			if valid {
				result.ObservedFeatures++
			}
		}
		if valid {
			data[index] = value
		} else {
			data[index] = feature.Median
		}
	}
	raw := frozen.Baseline
	for _, tree := range frozen.Trees {
		index := 0
		for !tree[index].Leaf {
			current := tree[index]
			if data[current.Feature] <= current.Threshold {
				index = current.Left
			} else {
				index = current.Right
			}
		}
		// Leaf values already include the original learning rate.
		raw += tree[index].Value
	}
	result.Score = 1 / (1 + math.Exp(-raw))
	result.Selected = result.Score >= frozen.Threshold
	return result
}
