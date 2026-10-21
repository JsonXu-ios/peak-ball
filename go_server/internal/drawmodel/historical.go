package drawmodel

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type historicalRow struct {
	example HistoricalExample
	base    Result
	index   int
	y       float64
	market  *float64
}

type historicalGroup struct {
	name, label string
	features    []string
	raw         bool
	imputation  map[string]float64
}

func historicalGroups(rows []historicalRow, predictions []HistoricalPrediction) []historicalGroup {
	groups := []historicalGroup{
		{name: "score_model", label: "原始比分模型", features: []string{"score_probability"}, raw: true},
		{name: "market_only", label: "市场去水平局概率", features: []string{"market_probability"}, raw: true},
		{name: "score_calibrated", label: "比分概率校准", features: []string{"score_logit"}},
		{name: "score_recent", label: "比分 + 近期及主客场平局", features: []string{"score_logit", "overall_draw_delta", "venue_draw_delta"}},
		{name: "score_market", label: "比分 + 欧赔", features: []string{"score_logit", "market_logit"}},
		{name: "score_market_lines", label: "比分 + 欧赔 + 亚盘及大小球", features: []string{"score_logit", "market_logit", "goal_line", "absolute_handicap"}},
	}
	counts, sums := map[string]int{}, map[string]float64{}
	hasExisting, hasSignalVotes := false, false
	for _, row := range rows {
		for key, value := range row.example.Features {
			if strings.TrimSpace(key) != "" && finite(value) {
				if strings.HasPrefix(key, "signal") {
					hasSignalVotes = true
					continue
				}
				hasExisting = true
				// Feature names, coverage ordering and imputation values are
				// learned on the training period, never on held-out rows.
				if predictions[row.index].Partition == "train" {
					counts[key]++
					sums[key] += value
				}
			}
		}
	}
	ordered := make([]string, 0, len(counts))
	for key, n := range counts {
		if n >= 10 {
			ordered = append(ordered, key)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if counts[ordered[i]] == counts[ordered[j]] {
			return ordered[i] < ordered[j]
		}
		return counts[ordered[i]] > counts[ordered[j]]
	})
	// Four values plus four missingness indicators avoid discarding every
	// partially populated analysis row or using dozens of weak parameters.
	if len(ordered) > 4 {
		ordered = ordered[:4]
	}
	if hasExisting {
		features := []string{"score_logit"}
		imputation := map[string]float64{}
		for _, key := range ordered {
			name := "existing:" + key
			features = append(features, name, "existing_missing:"+key)
			imputation[name] = sums[key] / float64(counts[key])
		}
		groups = append(groups, historicalGroup{name: "existing_analysis", label: "比分 + 已有分析特征", features: features, imputation: imputation})
	}
	if hasSignalVotes {
		groups = append(groups, historicalGroup{name: "existing_signal_votes", label: "比分 + 已有条件平局投票", features: []string{"score_logit", "existing:signalShare_draw", "existing:signalTriggeredCount"}})
	}
	return groups
}

func historicalMarket(e HistoricalExample) *float64 {
	if e.Odds == nil {
		return nil
	}
	known := !e.Odds.CapturedAt.IsZero() && !e.Odds.CapturedAt.After(e.AsOf)
	exploratory := e.OddsProvenance == "opening_reconstructed" || e.OddsProvenance == "historical_unknown"
	if !known && !exploratory {
		return nil
	}
	if p, ok := MarketDrawProbability(*e.Odds); ok {
		return &p
	}
	return nil
}

func historicalFeature(row historicalRow, name string) (float64, bool) {
	r := row.base
	switch name {
	case "score_probability":
		return r.ScoreModelProbability, true
	case "score_logit":
		return logit(r.ScoreModelProbability), true
	case "market_probability":
		if row.market != nil {
			return *row.market, true
		}
	case "market_logit":
		if row.market != nil {
			return logit(*row.market), true
		}
	case "overall_draw_delta":
		return (r.HomeOverall.SmoothedDrawRate+r.AwayOverall.SmoothedDrawRate)/2 - r.LeagueBaseline.DrawRate, true
	case "venue_draw_delta":
		return (r.HomeVenue.SmoothedDrawRate+r.AwayVenue.SmoothedDrawRate)/2 - r.LeagueBaseline.DrawRate, true
	case "goal_line", "absolute_handicap":
		if row.example.Odds == nil {
			return 0, false
		}
		odds := row.example.Odds
		known := !odds.CapturedAt.IsZero() && !odds.CapturedAt.After(row.example.AsOf)
		exploratory := row.example.OddsProvenance == "opening_reconstructed" || row.example.OddsProvenance == "historical_unknown"
		if !known && !exploratory {
			return 0, false
		}
		if name == "goal_line" && odds.GoalLine != nil && finite(*odds.GoalLine) && *odds.GoalLine >= 0 && *odds.GoalLine <= 10 {
			return *odds.GoalLine, true
		}
		if name == "absolute_handicap" && odds.AsianHandicap != nil && finite(*odds.AsianHandicap) && math.Abs(*odds.AsianHandicap) <= 10 {
			return math.Abs(*odds.AsianHandicap), true
		}
	default:
		if strings.HasPrefix(name, "existing_missing:") {
			value, ok := row.example.Features[strings.TrimPrefix(name, "existing_missing:")]
			if !ok || !finite(value) {
				return 1, true
			}
			return 0, true
		}
		if strings.HasPrefix(name, "existing:") {
			value, ok := row.example.Features[strings.TrimPrefix(name, "existing:")]
			return value, ok && finite(value)
		}
	}
	return 0, false
}

func historicalGroupVector(row historicalRow, group historicalGroup) ([]float64, bool) {
	if group.name != "existing_analysis" {
		return historicalVector(row, group.features)
	}
	// A match with no existing analysis must not be counted as an analysis
	// prediction merely because its baseline score is available.
	hasObserved := false
	x := make([]float64, len(group.features))
	for i, name := range group.features {
		value, ok := historicalFeature(row, name)
		if strings.HasPrefix(name, "existing:") {
			if ok {
				hasObserved = true
			} else {
				value, ok = group.imputation[name]
			}
		}
		if !ok || !finite(value) {
			return nil, false
		}
		x[i] = value
	}
	return x, hasObserved
}

func historicalVector(row historicalRow, features []string) ([]float64, bool) {
	x := make([]float64, len(features))
	for i, name := range features {
		value, ok := historicalFeature(row, name)
		if !ok || !finite(value) {
			return nil, false
		}
		x[i] = value
	}
	return x, true
}

func historicalExploratory(row historicalRow, group historicalGroup) bool {
	if (group.name == "existing_analysis" || group.name == "existing_signal_votes") && row.example.Exploratory {
		return true
	}
	if group.name == "market_only" || group.name == "score_market" || group.name == "score_market_lines" {
		return row.example.OddsProvenance == "opening_reconstructed" || row.example.OddsProvenance == "historical_unknown"
	}
	return false
}

// FitHistorical performs a deterministic chronological 60/20/20 split. Labels
// unavailable at the next period's earliest prediction time are purged. Main
// test rows never fit the selected model. Late-starting exploratory groups may
// use their own explicitly marked periods; their own test is never fitted.
// Every supplied row is returned, including excluded rows and training rows
// explicitly labeled as exploratory rather than out-of-time validation.
func FitHistorical(examples []HistoricalExample) HistoricalEvaluation {
	evaluation := HistoricalEvaluation{Version: HistoricalVersion, Rows: make([]HistoricalPrediction, len(examples)), Models: []HistoricalComparison{}, Warnings: []string{}}
	rows := make([]historicalRow, 0, len(examples))
	seen := map[string]bool{}
	for i, e := range examples {
		p := HistoricalPrediction{ID: e.Target.ID, Partition: "excluded", Draw: e.Draw, GroupProbabilities: map[string]float64{}, GroupExploratory: map[string]bool{}, GroupPartitions: map[string]string{}, OddsProvenance: e.OddsProvenance, Warnings: []string{}}
		evaluation.Rows[i] = p
		if e.Target.Kickoff.IsZero() || e.AsOf.IsZero() || e.AsOf.After(e.Target.Kickoff) {
			evaluation.Rows[i].Warnings = append(evaluation.Rows[i].Warnings, "缺少可靠赛前时间边界")
			continue
		}
		key := matchKey(e.Target)
		if seen[key] {
			evaluation.Rows[i].Warnings = append(evaluation.Rows[i].Warnings, "重复历史比赛，未重复计入拟合与测试")
			continue
		}
		seen[key] = true
		var base Result
		if e.Base != nil {
			base = *e.Base
		} else {
			base = Predict(Input{Target: e.Target, AsOf: e.AsOf, History: e.History, Odds: e.Odds})
		}
		if !base.Available || !finite(base.ScoreModelProbability) || base.ScoreModelProbability <= 0 || base.ScoreModelProbability >= 1 {
			evaluation.Rows[i].Warnings = append(evaluation.Rows[i].Warnings, "赛前有效历史不足，未生成基础比分概率")
			continue
		}
		y := 0.0
		if e.Draw {
			y = 1
		}
		evaluation.Rows[i].Available = true
		rows = append(rows, historicalRow{e, base, i, y, historicalMarket(e)})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].example.Target.Kickoff.Equal(rows[j].example.Target.Kickoff) {
			return rows[i].example.Target.ID < rows[j].example.Target.ID
		}
		return rows[i].example.Target.Kickoff.Before(rows[j].example.Target.Kickoff)
	})
	evaluation.Split.ExcludedSamples = len(examples) - len(rows)
	if len(rows) == 0 {
		evaluation.Warnings = append(evaluation.Warnings, "没有可按赛前边界回算的历史样本")
		return evaluation
	}
	trainEnd, calibrationEnd := int(float64(len(rows))*.6), int(float64(len(rows))*.8)
	if trainEnd < 1 {
		trainEnd = 1
	}
	if calibrationEnd < trainEnd {
		calibrationEnd = trainEnd
	}
	calibrationStart := historicalEarliestAsOf(rows[trainEnd:calibrationEnd])
	testStart := historicalEarliestAsOf(rows[calibrationEnd:])
	trainingBoundary := calibrationStart
	if trainingBoundary.IsZero() || (!testStart.IsZero() && testStart.Before(trainingBoundary)) {
		trainingBoundary = testStart
	}
	for i, row := range rows {
		p := &evaluation.Rows[row.index]
		switch {
		case i < trainEnd:
			p.Partition = "train"
			if !trainingBoundary.IsZero() && availableAt(row.example.Target).After(trainingBoundary) {
				p.Partition = "excluded"
			}
		case i < calibrationEnd:
			p.Partition = "calibration"
			if !testStart.IsZero() && availableAt(row.example.Target).After(testStart) {
				p.Partition = "excluded"
			}
		default:
			p.Partition = "test"
		}
		if p.Partition == "excluded" {
			evaluation.Split.PurgedSamples++
			p.Warnings = append(p.Warnings, "分区边界清除：本场赛果在下一阶段最早预测时刻尚不可知")
		}
		p.Exploratory = p.Partition != "test"
		switch p.Partition {
		case "train":
			evaluation.Split.TrainingSamples++
			t := row.example.Target.Kickoff
			evaluation.Split.TrainingEnd = &t
		case "calibration":
			evaluation.Split.CalibrationSamples++
			t := row.example.Target.Kickoff
			if evaluation.Split.CalibrationStart == nil {
				evaluation.Split.CalibrationStart = &t
			}
			evaluation.Split.CalibrationEnd = &t
		case "test":
			evaluation.Split.TestSamples++
			t := row.example.Target.Kickoff
			if evaluation.Split.TestStart == nil {
				evaluation.Split.TestStart = &t
			}
		}
	}
	evaluation.Split.ExcludedSamples += evaluation.Split.PurgedSamples
	artifact := &HistoricalArtifact{Version: HistoricalVersion, Models: map[string]HistoricalModelArtifact{}, SelectedModel: "score_model", Warnings: []string{}}
	groups := historicalGroups(rows, evaluation.Rows)
	for _, group := range groups {
		model, comparison := fitHistoricalGroup(rows, evaluation.Rows, group)
		groupPredictions := evaluation.Rows
		// Late-starting market/analysis datasets deserve their own temporal
		// experiment. They never become part of the main validation claim or
		// activate a model, because their training may overlap the main test.
		if (model == nil || !model.Fitted || !model.Calibrated) && (group.name == "score_market" || group.name == "score_market_lines" || group.name == "existing_analysis" || group.name == "existing_signal_votes") {
			if local, ok := historicalIndependentSplit(rows, evaluation.Rows, group); ok {
				localGroup := group
				if group.name == "existing_analysis" {
					for _, candidate := range historicalGroups(rows, local) {
						if candidate.name == group.name {
							localGroup = candidate
							break
						}
					}
				}
				localModel, localComparison := fitHistoricalGroup(rows, local, localGroup)
				localComparison.Exploratory, localComparison.IndependentSplit = true, true
				localComparison.Warnings = append(localComparison.Warnings, "该组数据集中在较晚时段，按自身可用样本独立 60%/20%/20% 时间拆分及边界清除；其训练可能位于主模型测试期，仅属独立探索，不能与主测试混称或激活")
				if localModel != nil {
					localModel.Exploratory = true
				}
				model, comparison, group, groupPredictions = localModel, localComparison, localGroup, local
			}
		}
		for _, row := range rows {
			evaluation.Rows[row.index].GroupPartitions[group.name] = groupPredictions[row.index].Partition
		}
		if model != nil {
			artifact.Models[group.name] = *model
			if model.TrainingCutoff.After(artifact.TrainingCutoff) {
				artifact.TrainingCutoff = model.TrainingCutoff
			}
		}
		for _, row := range rows {
			p := &evaluation.Rows[row.index]
			x, ok := historicalGroupVector(row, group)
			if !ok {
				continue
			}
			if group.raw {
				p.GroupProbabilities[group.name] = clamp(x[0], 1e-6, 1-1e-6)
			} else if model != nil && model.Fitted {
				p.GroupProbabilities[group.name] = historicalProbability(*model, x, true)
			} else {
				continue
			}
			p.GroupExploratory[group.name] = historicalExploratory(row, group) || (model != nil && model.Exploratory) || historicalPartition(*p, group.name) != "test"
		}
		evaluation.Models = append(evaluation.Models, comparison)
	}
	// Selection compares raw validation loss before the calibrator is fitted;
	// exploratory historical odds/analysis can never activate a future model.
	best := math.Inf(1)
	for _, group := range groups {
		model, ok := artifact.Models[group.name]
		if !ok || !model.Fitted || !model.Calibrated || model.Exploratory {
			continue
		}
		penalized := model.SelectionLogLoss + .5*float64(len(model.FeatureNames))/float64(model.CalibrationSamples)
		if penalized < best {
			best, artifact.SelectedModel = penalized, model.Name
		}
	}
	if artifact.SelectedModel == "score_model" {
		evaluation.Warnings = append(evaluation.Warnings, "独立训练/校准样本或类别不足，保留原始比分模型；历史逐场回算及测试指标仍可查看")
	}
	for _, row := range rows {
		p := &evaluation.Rows[row.index]
		probability, ok := p.GroupProbabilities[artifact.SelectedModel]
		if !ok {
			probability, ok = p.GroupProbabilities["score_model"]
		}
		if ok {
			p.Probability = &probability
			p.Exploratory = p.Partition != "test" || p.GroupExploratory[artifact.SelectedModel]
			p.OutOfTime = p.Partition == "test" && !p.Exploratory
		}
	}
	for i := range evaluation.Models {
		comparison := &evaluation.Models[i]
		threshold := .3
		if model, ok := artifact.Models[comparison.Name]; ok {
			threshold = model.Threshold
		}
		comparison.Test = historicalMetrics(evaluation.Rows, comparison.Name, threshold, nil)
	}
	// The common holdout is an intersection, so missing market/analysis rows
	// cannot create a misleading apparent improvement on an easier sample.
	common := map[int]bool{}
	for _, row := range rows {
		p := evaluation.Rows[row.index]
		if p.Partition != "test" {
			continue
		}
		complete := true
		for _, comparison := range evaluation.Models {
			if !comparison.Fitted || comparison.Test.Samples == 0 {
				continue
			}
			if _, ok := p.GroupProbabilities[comparison.Name]; !ok || historicalPartition(p, comparison.Name) != "test" {
				complete = false
				break
			}
		}
		if complete {
			common[row.index] = true
		}
	}
	evaluation.Split.CommonTestSamples = len(common)
	for i := range evaluation.Models {
		comparison := &evaluation.Models[i]
		threshold := .3
		if model, ok := artifact.Models[comparison.Name]; ok {
			threshold = model.Threshold
		}
		comparison.CommonTest = historicalMetrics(evaluation.Rows, comparison.Name, threshold, common)
	}
	for _, comparison := range evaluation.Models {
		if comparison.Exploratory {
			evaluation.Warnings = append(evaluation.Warnings, "历史赔率或已有分析缺少可靠赛前采集时间，相应分组仅用于探索比较，不能视为真实时点重放，也不参与未来默认模型选择")
			break
		}
	}
	artifact.Warnings = append(artifact.Warnings, evaluation.Warnings...)
	evaluation.Artifact = artifact
	return evaluation
}

func historicalPartition(row HistoricalPrediction, name string) string {
	if partition, ok := row.GroupPartitions[name]; ok {
		return partition
	}
	return row.Partition
}

func historicalIndependentSplit(rows []historicalRow, predictions []HistoricalPrediction, group historicalGroup) ([]HistoricalPrediction, bool) {
	candidates := []historicalRow{}
	for _, row := range rows {
		usable := false
		if group.name == "existing_analysis" {
			for key, value := range row.example.Features {
				if !strings.HasPrefix(key, "signal") && finite(value) {
					usable = true
					break
				}
			}
		} else {
			_, usable = historicalGroupVector(row, group)
		}
		if usable {
			candidates = append(candidates, row)
		}
	}
	if len(candidates) < 80 {
		return nil, false
	}
	local := append([]HistoricalPrediction{}, predictions...)
	for i := range local {
		local[i].Partition = "excluded"
	}
	trainEnd, calibrationEnd := int(float64(len(candidates))*.6), int(float64(len(candidates))*.8)
	calibrationStart := historicalEarliestAsOf(candidates[trainEnd:calibrationEnd])
	testStart := historicalEarliestAsOf(candidates[calibrationEnd:])
	boundary := calibrationStart
	if testStart.Before(boundary) {
		boundary = testStart
	}
	for i, row := range candidates {
		partition := "test"
		if i < trainEnd {
			partition = "train"
			if availableAt(row.example.Target).After(boundary) {
				partition = "excluded"
			}
		} else if i < calibrationEnd {
			partition = "calibration"
			if availableAt(row.example.Target).After(testStart) {
				partition = "excluded"
			}
		}
		local[row.index].Partition = partition
	}
	return local, true
}

func historicalEarliestAsOf(rows []historicalRow) time.Time {
	var earliest time.Time
	for _, row := range rows {
		if earliest.IsZero() || row.example.AsOf.Before(earliest) {
			earliest = row.example.AsOf
		}
	}
	return earliest
}

func fitHistoricalGroup(rows []historicalRow, predictions []HistoricalPrediction, group historicalGroup) (*HistoricalModelArtifact, HistoricalComparison) {
	k := len(group.features)
	minimum := 12 * (k + 1)
	comparison := HistoricalComparison{Name: group.name, Label: group.label, FeatureNames: append([]string{}, group.features...), MinimumTraining: minimum, Warnings: []string{}}
	train, calibration := []trainingRow{}, []trainingRow{}
	for _, row := range rows {
		x, ok := historicalGroupVector(row, group)
		if !ok {
			continue
		}
		p := predictions[row.index]
		if historicalExploratory(row, group) {
			comparison.Exploratory = true
		}
		prepared := trainingRow{x: x, y: row.y, asOf: row.example.AsOf, kickoff: row.example.Target.Kickoff, finished: availableAt(row.example.Target)}
		if p.Partition == "train" {
			train = append(train, prepared)
		} else if p.Partition == "calibration" {
			calibration = append(calibration, prepared)
		}
	}
	comparison.TrainingSamples, comparison.CalibrationSamples = len(train), len(calibration)
	model := &HistoricalModelArtifact{Name: group.name, FeatureNames: append([]string{}, group.features...), TrainingSamples: len(train), CalibrationSamples: len(calibration), Exploratory: comparison.Exploratory, Threshold: .3, CalibrationSlope: 1, ImputationValues: group.imputation}
	if group.name == "existing_analysis" && len(group.features) == 1 {
		comparison.Exploratory = true
		comparison.Warnings = append(comparison.Warnings, "已有分析存在，但训练时段没有任一特征达到 10 场覆盖；保留逐信号碰撞，不使用测试时段选特征或反向训练")
		return nil, comparison
	}
	for _, period := range [][]trainingRow{train, calibration} {
		for _, row := range period {
			if row.finished.After(model.TrainingCutoff) {
				model.TrainingCutoff = row.finished
			}
		}
	}
	if group.raw {
		comparison.Fitted = true
		if len(calibration) > 0 {
			values := make([]float64, len(calibration))
			for i, row := range calibration {
				values[i] = row.x[0]
			}
			model.Threshold = historicalTop20Threshold(values)
		}
		return model, comparison
	}
	// The count depends on model dimension, with each class represented. No
	// future-freeze-only global 400-row requirement applies to historical data.
	classMinimum := int(math.Max(6, float64(2*(k+1))))
	if len(train) < minimum || !hasBothClasses(train, classMinimum) {
		comparison.Warnings = append(comparison.Warnings, fmt.Sprintf("训练至少需要 %d 场、平局与非平局各 %d 场；当前 %d 场，保留其他可用分组", minimum, classMinimum, len(train)))
		return nil, comparison
	}
	model.Means, model.Scales = historicalStandardization(train, k)
	x, y := make([][]float64, len(train)), make([]float64, len(train))
	for i, row := range train {
		x[i], y[i] = standardizedVector(row.x, model.Means, model.Scales), row.y
	}
	model.Intercept, model.Coefficients = fitLogistic(x, y, .1)
	model.Fitted, comparison.Fitted = true, true
	calibrationLogits, calibrationLabels := make([]float64, len(calibration)), make([]float64, len(calibration))
	for i, row := range calibration {
		z := linear(model.Intercept, model.Coefficients, standardizedVector(row.x, model.Means, model.Scales))
		calibrationLogits[i], calibrationLabels[i] = z, row.y
		model.SelectionLogLoss += (softplus(z) - row.y*z) / float64(len(calibration))
	}
	if len(calibration) >= 20 && hasBothClasses(calibration, 4) {
		model.CalibrationIntercept, model.CalibrationSlope = fitCalibration(calibrationLogits, calibrationLabels)
		model.Calibrated, comparison.Calibrated = true, true
	} else {
		comparison.Warnings = append(comparison.Warnings, "独立校准至少需要 20 场且平局、非平局各 4 场；当前仅展示未校准融合，未激活为默认模型")
	}
	if len(calibration) > 0 {
		values := make([]float64, len(calibration))
		for i, row := range calibration {
			values[i] = historicalProbability(*model, row.x, true)
		}
		model.Threshold = historicalTop20Threshold(values)
	}
	return model, comparison
}

func historicalStandardization(rows []trainingRow, k int) ([]float64, []float64) {
	means, scales := make([]float64, k), make([]float64, k)
	for _, row := range rows {
		for j, value := range row.x {
			means[j] += value / float64(len(rows))
		}
	}
	for _, row := range rows {
		for j, value := range row.x {
			scales[j] += (value - means[j]) * (value - means[j]) / float64(len(rows))
		}
	}
	for j := range scales {
		scales[j] = math.Sqrt(scales[j])
		if scales[j] < 1e-6 {
			scales[j] = 1
		}
	}
	return means, scales
}

func historicalProbability(model HistoricalModelArtifact, x []float64, calibrated bool) float64 {
	z := linear(model.Intercept, model.Coefficients, standardizedVector(x, model.Means, model.Scales))
	if calibrated && model.Calibrated {
		z = model.CalibrationIntercept + model.CalibrationSlope*z
	}
	return clamp(sigmoid(z), 1e-6, 1-1e-6)
}

func historicalTop20Threshold(values []float64) float64 {
	sorted := append([]float64{}, values...)
	sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
	return sorted[int(math.Ceil(float64(len(sorted))*.2))-1]
}

func historicalMetrics(rows []HistoricalPrediction, name string, threshold float64, include map[int]bool) HistoricalMetrics {
	type outcome struct{ p, y float64 }
	outcomes := []outcome{}
	m := HistoricalMetrics{Threshold: threshold, CalibrationBins: []HistoricalCalibrationBin{}}
	bins := make([]HistoricalCalibrationBin, 10)
	for i := range bins {
		bins[i].Lower, bins[i].Upper = float64(i)/10, float64(i+1)/10
	}
	for i, row := range rows {
		if historicalPartition(row, name) != "test" || (include != nil && !include[i]) {
			continue
		}
		p, ok := row.GroupProbabilities[name]
		if !ok {
			continue
		}
		y := 0.0
		if row.Draw {
			y = 1
			m.Draws++
		}
		m.Samples++
		m.Brier += (p - y) * (p - y)
		m.LogLoss -= y*math.Log(clamp(p, 1e-6, 1-1e-6)) + (1-y)*math.Log(clamp(1-p, 1e-6, 1-1e-6))
		if p >= threshold {
			m.Selected++
			if row.Draw {
				m.Hits++
			}
		}
		bin := int(clamp(math.Floor(p*10), 0, 9))
		bins[bin].Samples++
		bins[bin].MeanProbability += p
		bins[bin].DrawRate += y
		outcomes = append(outcomes, outcome{p, y})
	}
	if m.Samples == 0 {
		return m
	}
	n := float64(m.Samples)
	m.Brier, m.LogLoss, m.DrawRate, m.Coverage = m.Brier/n, m.LogLoss/n, float64(m.Draws)/n, float64(m.Selected)/n
	if m.Selected > 0 {
		m.Precision = float64(m.Hits) / float64(m.Selected)
	}
	if m.Draws > 0 {
		m.Recall = float64(m.Hits) / float64(m.Draws)
	}
	sort.SliceStable(outcomes, func(i, j int) bool { return outcomes[i].p > outcomes[j].p })
	m.Top20Selected = int(math.Ceil(n * .2))
	for _, outcome := range outcomes[:m.Top20Selected] {
		if outcome.y == 1 {
			m.Top20Hits++
		}
	}
	m.Top20Precision = float64(m.Top20Hits) / float64(m.Top20Selected)
	for _, bin := range bins {
		if bin.Samples > 0 {
			bin.MeanProbability /= float64(bin.Samples)
			bin.DrawRate /= float64(bin.Samples)
			m.CalibrationBins = append(m.CalibrationBins, bin)
		}
	}
	return m
}

// HistoricalArtifactJSON saves every parameter and the selection/audit data;
// callers can unmarshal it into HistoricalArtifact for future predictions.
func HistoricalArtifactJSON(artifact *HistoricalArtifact) ([]byte, error) {
	return json.Marshal(artifact)
}

// PredictHistorical applies the selected non-exploratory historical artifact
// only after its training/calibration results were available. Its main holdout
// outcomes never enter its coefficients, calibrator or selection threshold.
func PredictHistorical(in Input, artifact *HistoricalArtifact) Result {
	// Keep the old frozen-snapshot fusion and the historical fusion separate.
	// An invalid historical artifact must fall back to the documented score
	// baseline rather than retain a different model's calibrated status.
	in.Artifact = nil
	r := Predict(in)
	if !r.Available || artifact == nil || artifact.Version != HistoricalVersion || artifact.SelectedModel == "score_model" {
		return r
	}
	cutoff := in.AsOf
	if cutoff.After(in.Target.Kickoff) {
		cutoff = in.Target.Kickoff
	}
	model, ok := artifact.Models[artifact.SelectedModel]
	if !ok || !historicalArtifactValid(model, cutoff) {
		appendWarning(&r, "历史融合模型参数、赛前边界或来源不符合使用条件，采用基础比分模型")
		return r
	}
	row := historicalRow{example: HistoricalExample{Target: in.Target, AsOf: cutoff, Odds: in.Odds}, base: r}
	row.market = historicalMarket(row.example)
	x, ok := historicalGroupVector(row, historicalGroup{name: model.Name, features: model.FeatureNames, imputation: model.ImputationValues})
	if !ok {
		appendWarning(&r, "本场缺少历史融合模型所需特征，采用基础比分模型")
		return r
	}
	r.Probability = historicalProbability(model, x, true)
	r.Model.FusionFitted, r.Model.Calibrated = true, model.Calibrated
	r.Model.TrainingSamples, r.Model.CalibrationSamples = model.TrainingSamples, model.CalibrationSamples
	timestamp := model.TrainingCutoff
	r.Model.TrainingCutoff = &timestamp
	r.Model.Version = HistoricalVersion + ":" + model.Name
	filtered := r.Warnings[:0]
	for _, warning := range r.Warnings {
		if warning != "尚无合格的独立后段校准融合模型，最终概率为未校准比分模型估计" {
			filtered = append(filtered, warning)
		}
	}
	r.Warnings = filtered
	r.Reasons = append(r.Reasons, fmt.Sprintf("融合模型 %s 使用已有历史的前段训练、后段校准；独立测试结果未参与拟合", model.Name))
	return r
}

func historicalArtifactValid(model HistoricalModelArtifact, cutoff time.Time) bool {
	k := len(model.FeatureNames)
	if !model.Fitted || !model.Calibrated || model.Exploratory || model.TrainingCutoff.IsZero() || model.TrainingCutoff.After(cutoff) || model.TrainingSamples < 12*(k+1) || model.CalibrationSamples < 20 || k == 0 || len(model.Coefficients) != k || len(model.Means) != k || len(model.Scales) != k {
		return false
	}
	if !finite(model.Intercept) || !finite(model.CalibrationIntercept) || !finite(model.CalibrationSlope) || model.CalibrationSlope <= 0 {
		return false
	}
	for j := range model.FeatureNames {
		if !finite(model.Coefficients[j]) || !finite(model.Means[j]) || !finite(model.Scales[j]) || model.Scales[j] <= 0 {
			return false
		}
	}
	for _, value := range model.ImputationValues {
		if !finite(value) {
			return false
		}
	}
	return true
}
