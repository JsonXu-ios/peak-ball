// Package drawmodeldata contains the frozen sporttery draw observation model.
// Its historical fit is not an independently validated probability guarantee.
package drawmodeldata

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"
)

//go:embed observation_v1.json
var modelJSON []byte

//go:embed observation_current.json
var currentModelJSON []byte

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
	Schema    int               `json:"schema"`
	Version   string            `json:"version"`
	ModelID   string            `json:"modelId"`
	Threshold float64           `json:"threshold"`
	Baseline  float64           `json:"baseline"`
	Features  []InputFeature    `json:"features"`
	Trees     [][]node          `json:"trees"`
	Training  TrainingInfo      `json:"training"`
	Checks    []predictionCheck `json:"checks"`
}

var frozen = mustLoadModel()

// TrainingInfo is metadata only. Neither dates, IDs nor target outcomes are
// model inputs; the content digest determines whether startup must refit.
type TrainingInfo struct {
	DataSHA256        string `json:"dataSHA256"`
	RecipeSHA256      string `json:"recipeSHA256"`
	FeatureCodeSHA256 string `json:"featureCodeSHA256"`
	Rows              int    `json:"rows"`
	SportteryRows     int    `json:"sportteryRows"`
	LatestDate        string `json:"latestDate"`
	GeneratedAt       string `json:"generatedAt"`
}
type predictionCheck struct {
	Features map[string]float64 `json:"features"`
	Score    float64            `json:"score"`
	Selected bool               `json:"selected"`
}
type modelState struct {
	model artifact
	body  []byte
	hash  string
}

var active atomic.Pointer[modelState]

func init() {
	state, err := parseModel(currentModelJSON)
	if err != nil {
		panic("invalid current draw model: " + err.Error())
	}
	active.Store(state)
}

func parseModel(body []byte) (*modelState, error) {
	var model artifact
	if err := json.Unmarshal(body, &model); err != nil {
		return nil, err
	}
	// Either the original 391 inputs, or those followed by the venue features.
	contract := frozen.Features
	if len(model.Features) == len(frozen.Features)+len(VenueFeatureNames()) {
		contract = ModelFeatures()
	}
	if model.Schema != 1 || len(model.Features) != len(contract) || len(model.Trees) != 240 || model.Version == "" || !finite(model.Threshold) || model.Threshold <= 0 || model.Threshold >= 1 || !finite(model.Baseline) {
		return nil, fmt.Errorf("unsupported draw model contract")
	}
	for i, f := range model.Features {
		original := contract[i]
		if f.Name != original.Name || !finite(f.Median) || len(f.Guards) != len(original.Guards) {
			return nil, fmt.Errorf("invalid feature contract at %d", i)
		}
		for j, g := range f.Guards {
			if g != original.Guards[j] {
				return nil, fmt.Errorf("changed feature guard")
			}
		}
	}
	used := make([]bool, len(model.Features))
	for _, tree := range model.Trees {
		if len(tree) == 0 || len(tree) > 1024 {
			return nil, fmt.Errorf("invalid tree size")
		}
		for i, n := range tree {
			if n.Leaf {
				if !finite(n.Value) {
					return nil, fmt.Errorf("nonfinite tree leaf")
				}
				continue
			}
			if n.Feature < 0 || n.Feature >= len(model.Features) || n.Left <= i || n.Right <= i || n.Left >= len(tree) || n.Right >= len(tree) || !finite(n.Threshold) {
				return nil, fmt.Errorf("invalid tree branch")
			}
			used[n.Feature] = true
		}
	}
	for i, f := range model.Features {
		if f.UsedInTrees != used[i] {
			return nil, fmt.Errorf("incorrect used feature marker")
		}
	}
	if model.Version != frozen.Version && (len(model.Checks) < 3 || len(model.Training.DataSHA256) != 64 || model.Training.Rows < 100 || len(model.Training.RecipeSHA256) != 64) {
		return nil, fmt.Errorf("updated model lacks training audit or parity checks")
	}
	for _, c := range model.Checks {
		got := predictModel(&model, c.Features)
		if !finite(c.Score) || math.Abs(got.Score-c.Score) > 1e-11 || got.Selected != c.Selected {
			return nil, fmt.Errorf("Python/Go prediction check failed")
		}
	}
	return &modelState{model: model, body: append([]byte(nil), body...), hash: fmt.Sprintf("%x", sha256.Sum256(body))}, nil
}

// ValidateArtifact verifies the complete numeric contract without activation.
func ValidateArtifact(body []byte) error { _, err := parseModel(body); return err }

// LoadArtifact activates only a complete validated candidate. Call during
// startup before accepting requests so one response cannot mix model versions.
func LoadArtifact(body []byte) error {
	state, err := parseModel(body)
	if err != nil {
		return err
	}
	active.Store(state)
	return nil
}
func ActiveArtifact() []byte { return append([]byte(nil), active.Load().body...) }
func Training() TrainingInfo { return active.Load().model.Training }

// BaselineFeatures/Prediction preserve the original model for exact comparison.
func BaselineFeatures() []InputFeature                   { return copyFeatures(&frozen) }
func BaselineVersion() string                            { return frozen.Version }
func BaselineThreshold() float64                         { return frozen.Threshold }
func PredictBaseline(features map[string]float64) Result { return predictModel(&frozen, features) }

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

func Version() string    { return active.Load().model.Version }
func ModelID() string    { return active.Load().model.ModelID }
func Threshold() float64 { return active.Load().model.Threshold }

// Features returns a copy of the frozen ordered input/guard contract.
func Features() []InputFeature {
	return copyFeatures(&active.Load().model)
}
func copyFeatures(model *artifact) []InputFeature {
	result := append([]InputFeature(nil), model.Features...)
	for i := range result {
		result[i].Guards = append([]Guard(nil), result[i].Guards...)
	}
	return result
}

func UsedFeatureNames() []string {
	result := make([]string, 0, 202)
	for _, feature := range active.Load().model.Features {
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
	return predictModel(&active.Load().model, features)
}
func predictModel(model *artifact, features map[string]float64) Result {
	data := make([]float64, len(model.Features))
	result := Result{}
	for index, feature := range model.Features {
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
	raw := model.Baseline
	for _, tree := range model.Trees {
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
	result.Selected = result.Score >= model.Threshold
	return result
}
