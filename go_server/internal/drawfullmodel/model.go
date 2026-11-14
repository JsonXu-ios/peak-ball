// Package drawfullmodel serves the independent comprehensive decision trees.
// It never imports or modifies the original frozen model's decisions.
package drawfullmodel

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

//go:embed trees_v1.json
var artifactJSON []byte

type FeatureGuard struct {
	Feature string  `json:"feature"`
	Minimum float64 `json:"minimum"`
}

type InputFeature struct {
	Name     string         `json:"name"`
	Median   float64        `json:"median"`
	Label    string         `json:"label"`
	Module   string         `json:"module"`
	Guards   []FeatureGuard `json:"guards,omitempty"`
	Evidence *bool          `json:"evidence,omitempty"`
}

type treeNode struct {
	Leaf      bool      `json:"leaf"`
	Value     float64   `json:"value"`
	Samples   int       `json:"samples"`
	Counts    []float64 `json:"counts,omitempty"`
	Feature   int       `json:"feature"`
	Threshold float64   `json:"threshold"`
	Left      int       `json:"left"`
	Right     int       `json:"right"`
}

type decisionTree struct {
	Threshold float64    `json:"threshold"`
	Nodes     []treeNode `json:"nodes"`
}

type artifact struct {
	Schema       int             `json:"schema"`
	Version      string          `json:"version"`
	InputVersion string          `json:"inputVersion"`
	Features     []InputFeature  `json:"features"`
	Draw         decisionTree    `json:"draw"`
	Alternative  decisionTree    `json:"alternative"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

// Model is an immutable pair of trees loaded from a reviewed JSON artifact.
type Model struct {
	artifact artifact
	used     map[int]bool
}

type ModelInfo struct {
	Version      string          `json:"version"`
	InputVersion string          `json:"inputVersion"`
	ArtifactHash string          `json:"artifactHash"`
	Available    bool            `json:"available"`
	Reason       string          `json:"reason,omitempty"`
	Threshold    float64         `json:"threshold"`
	Features     int             `json:"features"`
	Metadata     json.RawMessage `json:"metadata,omitempty"`
}

type PathStep struct {
	Feature   string   `json:"feature"`
	Label     string   `json:"label"`
	Module    string   `json:"module"`
	Value     *float64 `json:"value"`
	UsedValue float64  `json:"usedValue"`
	Threshold float64  `json:"threshold"`
	Imputed   bool     `json:"imputed"`
	WentLeft  bool     `json:"wentLeft"`
}

type ModuleCoverage struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Available int    `json:"available"`
	Total     int    `json:"total"`
	Used      int    `json:"used"`
}

func moduleLabel(key string) string {
	if value := map[string]string{"odds": "公司赔率与变化", "differences": "赔率差值", "kelly": "凯利", "lines": "亚盘大小球与水位", "recent": "安全赛前近况", "h2h": "安全赛前交锋", "trade": "源站投注与盈亏", "heat": "亚盘大小球热度", "context": "赛程状态背景", "conflicts": "组合与分歧", "summary": "近期历史均值", "goddess": "上帝的女人", "platform": "庄家与平台预测", "evil": "邪修判断", "retail": "散户心理", "warnings": "前台警示", "profits": "真实模拟交易盈亏", "availability": "数据有效性"}[key]; value != "" {
		return value
	}
	return key
}

// Scores are training-leaf frequencies, not calibrated match probabilities.
type Result struct {
	Available              bool             `json:"available"`
	Reason                 string           `json:"reason,omitempty"`
	Score                  float64          `json:"score"`
	Threshold              float64          `json:"threshold"`
	Selected               bool             `json:"selected"`
	HomeScore              float64          `json:"homeScore"`
	AwayScore              float64          `json:"awayScore"`
	AlternativeDirection   string           `json:"alternativeDirection,omitempty"`
	AlternativeReason      string           `json:"alternativeReason,omitempty"`
	DrawLeafSamples        int              `json:"drawLeafSamples"`
	AlternativeLeafSamples int              `json:"alternativeLeafSamples"`
	ObservedInputs         int              `json:"observedInputs"`
	ObservedEvidence       int              `json:"observedEvidence"`
	TotalInputs            int              `json:"totalInputs"`
	DrawPath               []PathStep       `json:"drawPath"`
	AlternativePath        []PathStep       `json:"alternativePath"`
	ModuleCoverage         []ModuleCoverage `json:"moduleCoverage"`
}

var loadedModel, loadError = Load(artifactJSON)
var artifactHash = fmt.Sprintf("%x", sha256.Sum256(artifactJSON))

func modelFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// Load validates all reachable tree nodes. Bad or unfinished experimental
// artifacts produce an unavailable signal; they cannot stop the original app.
func Load(body []byte) (*Model, error) {
	var data artifact
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("全量决策树文件无法读取")
	}
	if data.Schema != 1 || strings.TrimSpace(data.Version) == "" || strings.TrimSpace(data.InputVersion) == "" || len(data.Features) == 0 {
		return nil, fmt.Errorf("全量决策树尚未完成训练或格式无效")
	}
	if data.InputVersion != InputVersion {
		return nil, fmt.Errorf("全量决策树与输入特征版本不一致")
	}
	if !modelFinite(data.Draw.Threshold) || data.Draw.Threshold < 0 || data.Draw.Threshold > 1 {
		return nil, fmt.Errorf("全量决策树选平门槛无效")
	}
	names := map[string]bool{}
	for _, feature := range data.Features {
		if strings.TrimSpace(feature.Name) == "" || names[feature.Name] || !modelFinite(feature.Median) || !modelFinite(float64(float32(feature.Median))) {
			return nil, fmt.Errorf("全量决策树输入定义无效")
		}
		names[feature.Name] = true
		for _, guard := range feature.Guards {
			if strings.TrimSpace(guard.Feature) == "" || !modelFinite(guard.Minimum) {
				return nil, fmt.Errorf("全量决策树样本保护定义无效")
			}
		}
	}
	model := &Model{artifact: data, used: map[int]bool{}}
	for _, tree := range []decisionTree{data.Draw, data.Alternative} {
		if err := model.validateTree(tree); err != nil {
			return nil, err
		}
	}
	return model, nil
}

func (model *Model) validateTree(tree decisionTree) error {
	if len(tree.Nodes) == 0 {
		return fmt.Errorf("全量决策树缺少分支")
	}
	states := make([]uint8, len(tree.Nodes))
	var visit func(int) error
	visit = func(index int) error {
		if index < 0 || index >= len(tree.Nodes) {
			return fmt.Errorf("全量决策树子节点无效")
		}
		if states[index] == 1 {
			return fmt.Errorf("全量决策树存在循环")
		}
		if states[index] == 2 {
			return nil
		}
		states[index] = 1
		node := tree.Nodes[index]
		if node.Leaf {
			if !modelFinite(node.Value) || node.Value < 0 || node.Value > 1 || node.Samples < 1 {
				return fmt.Errorf("全量决策树叶节点频率无效")
			}
			if len(node.Counts) != 0 && len(node.Counts) != 2 {
				return fmt.Errorf("全量决策树叶节点类别数无效")
			}
			for _, value := range node.Counts {
				if !modelFinite(value) || value < 0 {
					return fmt.Errorf("全量决策树叶节点样本无效")
				}
			}
		} else {
			if node.Feature < 0 || node.Feature >= len(model.artifact.Features) || !modelFinite(node.Threshold) {
				return fmt.Errorf("全量决策树分支输入无效")
			}
			model.used[node.Feature] = true
			if err := visit(node.Left); err != nil {
				return err
			}
			if err := visit(node.Right); err != nil {
				return err
			}
		}
		states[index] = 2
		return nil
	}
	return visit(0)
}

func Info() ModelInfo {
	if loadedModel == nil || loadError != nil {
		reason := "全量决策树暂不可用"
		if loadError != nil {
			reason = loadError.Error()
		}
		return ModelInfo{Version: "draw-full-cart-v1", InputVersion: InputVersion, ArtifactHash: artifactHash, Reason: reason}
	}
	return ModelInfo{Version: loadedModel.artifact.Version, InputVersion: loadedModel.artifact.InputVersion, ArtifactHash: artifactHash,
		Available: true, Threshold: loadedModel.artifact.Draw.Threshold, Features: len(loadedModel.artifact.Features), Metadata: append(json.RawMessage(nil), loadedModel.artifact.Metadata...)}
}

func ArtifactSHA256() string { return artifactHash }

func Features() []InputFeature {
	if loadedModel == nil {
		return nil
	}
	result := append([]InputFeature(nil), loadedModel.artifact.Features...)
	for i := range result {
		result[i].Guards = append([]FeatureGuard(nil), result[i].Guards...)
		if result[i].Evidence != nil {
			value := *result[i].Evidence
			result[i].Evidence = &value
		}
	}
	return result
}

func Predict(features map[string]float64) Result {
	if loadedModel == nil || loadError != nil {
		return Result{Reason: Info().Reason, DrawPath: []PathStep{}, AlternativePath: []PathStep{}, ModuleCoverage: []ModuleCoverage{}}
	}
	return loadedModel.Predict(features)
}

// hasEvidence excludes availability indicators, denominators and synthetic
// screen output alone from making an otherwise empty fixture selectable.
func hasEvidence(feature InputFeature) bool {
	name := strings.ToLower(feature.Name)
	if feature.Module == "availability" || strings.Contains(name, "availability") || strings.Contains(name, ".available.") || strings.Contains(name, "missing") || strings.Contains(name, "ambiguous") || strings.HasSuffix(name, ".n") || strings.HasSuffix(name, ".samples") || strings.HasSuffix(name, ".samplecount") {
		return false
	}
	if feature.Evidence != nil && !*feature.Evidence {
		return false
	}
	for _, prefix := range []string{"euro.", "difference.", "kelly.", "line.", "heat.", "trade.", "marketprofit.", "saferecent.", "safeh2h.", "context.form.", "context.goals."} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (model *Model) Predict(features map[string]float64) Result {
	result := Result{Threshold: model.artifact.Draw.Threshold, TotalInputs: len(model.artifact.Features), DrawPath: []PathStep{}, AlternativePath: []PathStep{}, ModuleCoverage: []ModuleCoverage{}}
	values, originalValues, observed := make([]float64, result.TotalInputs), make([]float64, result.TotalInputs), make([]bool, result.TotalInputs)
	modules := map[string]*ModuleCoverage{}
	for index, feature := range model.artifact.Features {
		value, valid := features[feature.Name]
		valid = valid && modelFinite(value) && modelFinite(float64(float32(value)))
		for _, guard := range feature.Guards {
			guardValue, exists := features[guard.Feature]
			valid = valid && exists && modelFinite(guardValue) && guardValue >= guard.Minimum
		}
		// sklearn CART casts its inference matrix to float32 even when the
		// exported split thresholds and imputer medians are float64. Match that
		// comparison exactly while preserving raw values for the explanation.
		values[index] = float64(float32(feature.Median))
		observed[index] = valid
		module := feature.Module
		if module == "" {
			module = "other"
		}
		if modules[module] == nil {
			modules[module] = &ModuleCoverage{Key: module, Label: moduleLabel(module)}
		}
		modules[module].Total++
		if model.used[index] {
			modules[module].Used++
		}
		if valid {
			originalValues[index] = value
			values[index] = float64(float32(value))
			result.ObservedInputs++
			modules[module].Available++
			if hasEvidence(feature) {
				result.ObservedEvidence++
			}
		}
	}
	for _, module := range modules {
		result.ModuleCoverage = append(result.ModuleCoverage, *module)
	}
	sort.Slice(result.ModuleCoverage, func(i, j int) bool { return result.ModuleCoverage[i].Key < result.ModuleCoverage[j].Key })
	if result.ObservedEvidence == 0 {
		result.Reason = "缺少有效赔率、盘口或赛前战绩，全量决策树暂不判断"
		return result
	}
	drawLeaf, drawPath := model.walk(model.artifact.Draw, values, originalValues, observed)
	sideLeaf, sidePath := model.walk(model.artifact.Alternative, values, originalValues, observed)
	result.Available, result.Score, result.Selected = true, drawLeaf.Value, drawLeaf.Value >= result.Threshold
	result.DrawPath, result.AlternativePath = drawPath, sidePath
	result.DrawLeafSamples, result.AlternativeLeafSamples = drawLeaf.Samples, sideLeaf.Samples
	result.AwayScore, result.HomeScore = sideLeaf.Value, 1-sideLeaf.Value
	if math.Abs(result.HomeScore-result.AwayScore) <= 1e-12 {
		result.AlternativeReason = "胜负树两侧评分相同，暂不提供次选"
	} else if result.HomeScore > result.AwayScore {
		result.AlternativeDirection = "home"
	} else {
		result.AlternativeDirection = "away"
	}
	result.Reason = "两棵独立决策树分别判断选平与胜负备选；评分为训练叶内频率，未作概率校准"
	return result
}

func (model *Model) walk(tree decisionTree, values, originalValues []float64, observed []bool) (treeNode, []PathStep) {
	path := []PathStep{}
	index := 0
	for !tree.Nodes[index].Leaf {
		node := tree.Nodes[index]
		feature := model.artifact.Features[node.Feature]
		step := PathStep{Feature: feature.Name, Label: feature.Label, Module: feature.Module, UsedValue: values[node.Feature], Threshold: node.Threshold, Imputed: !observed[node.Feature], WentLeft: values[node.Feature] <= node.Threshold}
		if observed[node.Feature] {
			value := originalValues[node.Feature]
			step.Value = &value
		}
		path = append(path, step)
		if step.WentLeft {
			index = node.Left
		} else {
			index = node.Right
		}
	}
	return tree.Nodes[index], path
}
