// Package jcgoalsmodel provides the independent total-goals classifier used by
// the match-list over/under suggestion. It never reads fixture results or IDs.
package jcgoalsmodel

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
)

const Version = "jc-goals-classifier-v1"

//go:embed model_v1.json
var modelJSON []byte

type node struct {
	Leaf        bool    `json:"leaf"`
	Value       float64 `json:"value"`
	Feature     int     `json:"feature"`
	Threshold   float64 `json:"threshold"`
	Left        int     `json:"left"`
	Right       int     `json:"right"`
	MissingLeft bool    `json:"missingLeft"`
	Categorical bool    `json:"categorical"`
}

type artifact struct {
	Version  string `json:"version"`
	Protocol struct {
		FeatureNames []string `json:"featureNames"`
		SourceSHA256 string   `json:"sourceSHA256"`
	} `json:"protocol"`
	Chosen struct {
		AlphaModel float64 `json:"alphaModel"`
		AlphaJQ    float64 `json:"alphaJq"`
	} `json:"chosen"`
	Medians    []float64 `json:"medians"`
	Classifier struct {
		Kind       string      `json:"kind"`
		Classes    []int       `json:"classes"`
		Baseline   [][]float64 `json:"baseline"`
		Iterations [][][]node  `json:"iterations"`
	} `json:"classifier"`
}

// Model is immutable after loading and safe for concurrent predictions.
type Model struct {
	data artifact
}

// Market is the actual Bet365 total line and its two Hong Kong water quotes.
type Market struct {
	Line       float64 `json:"line"`
	OverWater  float64 `json:"overWater"`
	UnderWater float64 `json:"underWater"`
}

// Input contains only predictor observations. Missing/non-finite feature-map
// entries use the artifact's training medians; an observed zero remains zero.
// The caller must first check the three source blocks (JQ, both companies and
// safe team form) are available. This numerical package does not load sources.
type Input struct {
	Features        map[string]float64
	JQProbabilities []float64
	Bet365          Market
}

// Result is a model suggestion with no confidence or positive-return gate.
// Score is the predicted winning-stake share after excluding push stake; it is
// a model score, not a calibrated probability of a winning match.
type Result struct {
	ModelVersion      string     `json:"modelVersion"`
	Direction         string     `json:"direction"`
	Line              float64    `json:"line"`
	OverWater         float64    `json:"overWater"`
	UnderWater        float64    `json:"underWater"`
	Score             float64    `json:"score"`
	ExpectedNetReturn float64    `json:"expectedNetReturn"`
	OverEV            float64    `json:"overEV"`
	UnderEV           float64    `json:"underEV"`
	TotalScores       [8]float64 `json:"totalScores"`
	ObservedFeatures  int        `json:"observedFeatures"`
	TotalFeatures     int        `json:"totalFeatures"`
	Reason            string     `json:"reason"`
}

var loadOnce sync.Once
var loadedModel *Model
var loadedError error

// Load returns the validated, frozen embedded model. Repeated calls reuse the
// same immutable instance; a loading error never panics or starts a service.
func Load() (*Model, error) {
	loadOnce.Do(func() { loadedModel, loadedError = LoadJSON(modelJSON) })
	return loadedModel, loadedError
}

// LoadJSON validates an explicit artifact, useful for offline parity checks.
// Serving callers normally use Load so the reviewed embedded artifact is used.
func LoadJSON(body []byte) (*Model, error) {
	var data artifact
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("大小球模型文件无法读取: %w", err)
	}
	if data.Version != Version || data.Classifier.Kind != "hgb" || len(data.Protocol.FeatureNames) != 50 || len(data.Medians) != 50 {
		return nil, fmt.Errorf("大小球模型版本或输入格式无效")
	}
	if !finite(data.Chosen.AlphaModel) || !finite(data.Chosen.AlphaJQ) || data.Chosen.AlphaModel < 0 || data.Chosen.AlphaJQ < 0 ||
		math.Abs(data.Chosen.AlphaModel+data.Chosen.AlphaJQ-1) > 1e-12 {
		return nil, fmt.Errorf("大小球模型组合权重无效")
	}
	names := make(map[string]bool, 50)
	for i, name := range data.Protocol.FeatureNames {
		allowed := false
		for _, prefix := range []string{"jq.", "bet365.", "william.", "difference.", "form."} {
			allowed = allowed || strings.HasPrefix(name, prefix)
		}
		if !allowed || names[name] || !finite(data.Medians[i]) {
			return nil, fmt.Errorf("大小球模型输入定义无效")
		}
		names[name] = true
	}
	if len(data.Classifier.Classes) != 8 || len(data.Classifier.Baseline) != 1 || len(data.Classifier.Baseline[0]) != 8 || len(data.Classifier.Iterations) == 0 {
		return nil, fmt.Errorf("大小球模型分类结构无效")
	}
	classes := [8]bool{}
	for i, class := range data.Classifier.Classes {
		if class < 0 || class > 7 || classes[class] || !finite(data.Classifier.Baseline[0][i]) {
			return nil, fmt.Errorf("大小球模型分类顺序无效")
		}
		classes[class] = true
	}
	for _, targets := range data.Classifier.Iterations {
		if len(targets) != 8 {
			return nil, fmt.Errorf("大小球模型缺少分类树")
		}
		for _, tree := range targets {
			if err := validateTree(tree, len(data.Protocol.FeatureNames)); err != nil {
				return nil, err
			}
		}
	}
	return &Model{data: data}, nil
}

func validateTree(tree []node, columns int) error {
	if len(tree) == 0 {
		return fmt.Errorf("大小球模型树为空")
	}
	state := make([]uint8, len(tree))
	var visit func(int) error
	visit = func(index int) error {
		if index < 0 || index >= len(tree) {
			return fmt.Errorf("大小球模型子节点无效")
		}
		if state[index] == 1 {
			return fmt.Errorf("大小球模型树存在循环")
		}
		if state[index] == 2 {
			return nil
		}
		state[index] = 1
		n := tree[index]
		if !finite(n.Value) || n.Categorical {
			return fmt.Errorf("大小球模型节点格式无效")
		}
		if !n.Leaf {
			if n.Feature < 0 || n.Feature >= columns || !finite(n.Threshold) || n.Left == n.Right {
				return fmt.Errorf("大小球模型分支格式无效")
			}
			if err := visit(n.Left); err != nil {
				return err
			}
			if err := visit(n.Right); err != nil {
				return err
			}
		}
		state[index] = 2
		return nil
	}
	if err := visit(0); err != nil {
		return err
	}
	for _, s := range state {
		if s != 2 {
			return fmt.Errorf("大小球模型包含不可达节点")
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// FeatureNames returns a copy of the frozen feature order.
func (m *Model) FeatureNames() []string {
	if m == nil {
		return nil
	}
	return append([]string(nil), m.data.Protocol.FeatureNames...)
}

// ArtifactSHA256 identifies the exact embedded model bytes used by Load.
func ArtifactSHA256() string {
	hash := sha256.Sum256(modelJSON)
	return hex.EncodeToString(hash[:])
}

// Predict applies the reviewed HGB/JQ blend and selects the direction with
// higher expected unit net return at this exact Bet365 line and water pair.
func (m *Model) Predict(input Input) (Result, error) {
	if m == nil {
		return Result{}, fmt.Errorf("大小球模型尚未加载")
	}
	if err := validateMarket(input.Bet365); err != nil {
		return Result{}, err
	}
	// The accepted tolerance is a representation allowance, not a different
	// betting line. In particular, 2.00000000001 must still push at two goals.
	input.Bet365.Line = math.Round(input.Bet365.Line*4) / 4
	if len(input.JQProbabilities) != 8 {
		return Result{}, fmt.Errorf("缺少竞彩8档总进球报价分布")
	}
	jqSum := 0.0
	for _, value := range input.JQProbabilities {
		if !finite(value) || value <= 0 || value >= 1 {
			return Result{}, fmt.Errorf("竞彩总进球报价分布无效")
		}
		jqSum += value
	}
	if math.Abs(jqSum-1) > 1e-6 {
		return Result{}, fmt.Errorf("竞彩总进球报价分布未归一化")
	}
	values := make([]float64, len(m.data.Protocol.FeatureNames))
	observed := 0
	for i, name := range m.data.Protocol.FeatureNames {
		v, ok := input.Features[name]
		if ok && finite(v) {
			values[i] = v
			observed++
		} else {
			values[i] = m.data.Medians[i]
		}
	}
	var raw [8]float64
	copy(raw[:], m.data.Classifier.Baseline[0])
	for _, targets := range m.data.Classifier.Iterations {
		for class, tree := range targets {
			index := 0
			for !tree[index].Leaf {
				n := tree[index]
				if values[n.Feature] <= n.Threshold {
					index = n.Left
				} else {
					index = n.Right
				}
			}
			// Artifact leaf values already contain the fitted learning rate.
			raw[class] += tree[index].Value
		}
	}
	maximum := raw[0]
	for _, v := range raw[1:] {
		maximum = math.Max(maximum, v)
	}
	var native [8]float64
	sum := 0.0
	for class, value := range raw {
		native[class] = math.Exp(value - maximum)
		sum += native[class]
	}
	var distribution [8]float64
	for class, label := range m.data.Classifier.Classes {
		distribution[label] = m.data.Chosen.AlphaModel*native[class]/sum + m.data.Chosen.AlphaJQ*input.JQProbabilities[label]
	}
	sum = 0
	for _, v := range distribution {
		sum += v
	}
	for class := range distribution {
		distribution[class] /= sum
	}
	overWin, overLose := expectedFractions(distribution, input.Bet365.Line, "over")
	underWin, underLose := expectedFractions(distribution, input.Bet365.Line, "under")
	overEV := overWin*input.Bet365.OverWater - overLose
	underEV := underWin*input.Bet365.UnderWater - underLose
	direction, expected, win, lose, label := "under", underEV, underWin, underLose, "小球"
	if overEV > underEV+1e-12 {
		direction, expected, win, lose, label = "over", overEV, overWin, overLose, "大球"
	}
	if !finite(expected) || win+lose <= 0 {
		return Result{}, fmt.Errorf("大小球模型暂无可比较的非走盘评分")
	}
	return Result{
		ModelVersion: Version, Direction: direction, Line: input.Bet365.Line,
		OverWater: input.Bet365.OverWater, UnderWater: input.Bet365.UnderWater,
		Score: win / (win + lose), ExpectedNetReturn: expected, OverEV: overEV, UnderEV: underEV,
		TotalScores: distribution, ObservedFeatures: observed, TotalFeatures: len(values),
		Reason: fmt.Sprintf("竞彩总进球、两家公司大小球及近期进失球组合，当前盘口下%s的模型净收益评分较高；评分不是已校准概率", label),
	}, nil
}

func validateMarket(m Market) error {
	if !finite(m.Line) || m.Line <= 0 || m.Line > 6.5 || math.Round(m.Line*4) < 1 || math.Abs(m.Line*4-math.Round(m.Line*4)) >= 1e-8 {
		return fmt.Errorf("大小球盘口须为0到6.5之间的有效四分之一球盘口")
	}
	if !finite(m.OverWater) || !finite(m.UnderWater) || m.OverWater <= 0 || m.OverWater > 2 || m.UnderWater <= 0 || m.UnderWater > 2 {
		return fmt.Errorf("大小球双水须为0到2之间的有效香港水位")
	}
	return nil
}

// stakeFractions returns winning and losing fractions of one unit stake.
// Quarter lines divide the unit equally over the adjacent half-ball lines.
func stakeFractions(total int, line float64, direction string) (float64, float64) {
	quarters := int(math.Round(line * 4))
	parts := []float64{float64(quarters) / 4}
	if quarters%2 != 0 {
		parts = []float64{float64(quarters-1) / 4, float64(quarters+1) / 4}
	}
	win, lose := 0.0, 0.0
	weight := 1 / float64(len(parts))
	for _, part := range parts {
		delta := float64(total) - part
		if direction == "under" {
			delta = -delta
		}
		if delta > 0 {
			win += weight
		} else if delta < 0 {
			lose += weight
		}
	}
	return win, lose
}

func expectedFractions(scores [8]float64, line float64, direction string) (float64, float64) {
	win, lose := 0.0, 0.0
	for total, score := range scores {
		// The 7+ class settles identically for all its totals because line<=6.5.
		w, l := stakeFractions(total, line, direction)
		win += score * w
		lose += score * l
	}
	return win, lose
}
