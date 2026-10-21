package drawmodel

import (
	"math"
	"sort"
	"time"
)

const MinTrainingSamples = 400
const minFitSamples = 240
const minCalibrationSamples = 80

var featureNames = []string{
	"score_logit", "market_logit", "market_missing", "home_overall_draw", "away_overall_draw",
	"home_venue_draw", "away_venue_draw", "expected_total_goals", "expected_goal_gap",
	"home_low_score_rate", "away_low_score_rate", "home_history_fraction", "away_history_fraction",
	"goal_line", "goal_line_missing", "absolute_handicap", "handicap_missing",
}

func featureVector(r Result, odds *Odds, cutoff time.Time) []float64 {
	market, marketMissing := 0.0, 1.0
	if r.MarketProbability != nil {
		market = logit(*r.MarketProbability)
		marketMissing = 0
	}
	goalLine, goalMissing, handicap, handicapMissing := 0.0, 1.0, 0.0, 1.0
	if odds != nil && !odds.CapturedAt.IsZero() && !odds.CapturedAt.After(cutoff) {
		if odds.GoalLine != nil && finite(*odds.GoalLine) && *odds.GoalLine >= 0 && *odds.GoalLine <= 10 {
			goalLine = *odds.GoalLine
			goalMissing = 0
		}
		if odds.AsianHandicap != nil && finite(*odds.AsianHandicap) && math.Abs(*odds.AsianHandicap) <= 10 {
			handicap = math.Abs(*odds.AsianHandicap)
			handicapMissing = 0
		}
	}
	return []float64{
		logit(r.ScoreModelProbability), market, marketMissing,
		r.HomeOverall.SmoothedDrawRate - r.LeagueBaseline.DrawRate, r.AwayOverall.SmoothedDrawRate - r.LeagueBaseline.DrawRate,
		r.HomeVenue.SmoothedDrawRate - r.LeagueBaseline.DrawRate, r.AwayVenue.SmoothedDrawRate - r.LeagueBaseline.DrawRate,
		r.HomeLambda + r.AwayLambda, math.Abs(r.HomeLambda - r.AwayLambda), r.HomeOverall.LowScoreRate, r.AwayOverall.LowScoreRate,
		float64(r.HomeOverall.N) / 10, float64(r.AwayOverall.N) / 10, goalLine, goalMissing, handicap, handicapMissing,
	}
}

type trainingRow struct {
	x                       []float64
	y                       float64
	asOf, kickoff, finished time.Time
	key                     string
}

// Fit uses the earlier 80% for regularized logistic regression and the later
// 20% for calibration. Boundary fixtures whose results were unavailable at the
// beginning of calibration are purged. No random split or future quote is used.
// Calibration data are for fitting the calibrator, not a claimed test set.
func Fit(examples []TrainingExample) Artifact {
	a := Artifact{Version: Version, FeatureNames: append([]string{}, featureNames...), Warnings: []string{}}
	if len(examples) < MinTrainingSamples {
		a.Warnings = append(a.Warnings, "冻结赛前训练样本不足 400，继续采用未校准比分模型")
		return a
	}
	ordered := append([]TrainingExample{}, examples...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Target.Kickoff.Before(ordered[j].Target.Kickoff) })
	if len(ordered) > 2000 {
		ordered = ordered[len(ordered)-2000:]
		a.Warnings = append(a.Warnings, "本次拟合仅使用最近 2000 条冻结样本")
	}
	rows := make([]trainingRow, 0, len(ordered))
	seen := map[string]bool{}
	for _, e := range ordered {
		if e.Target.Kickoff.IsZero() || e.AsOf.IsZero() || e.AsOf.After(e.Target.Kickoff) {
			continue
		}
		key := matchKey(e.Target)
		if seen[key] {
			continue
		}
		seen[key] = true
		r := Predict(Input{Target: e.Target, AsOf: e.AsOf, History: e.History, Odds: e.Odds})
		if !r.Available {
			continue
		}
		y := 0.0
		if e.Draw {
			y = 1
		}
		rows = append(rows, trainingRow{x: featureVector(r, e.Odds, e.AsOf), y: y, asOf: e.AsOf, kickoff: e.Target.Kickoff, finished: availableAt(e.Target), key: key})
	}
	if len(rows) < MinTrainingSamples {
		a.Warnings = append(a.Warnings, "去重并校验赛前边界后的有效训练样本不足 400")
		return a
	}
	split := int(float64(len(rows)) * .8)
	calibration := rows[split:]
	calibrationAsOf := calibration[0].asOf
	for _, row := range calibration {
		if row.asOf.Before(calibrationAsOf) {
			calibrationAsOf = row.asOf
		}
	}
	train := make([]trainingRow, 0, split)
	for _, row := range rows[:split] {
		if !row.finished.After(calibrationAsOf) {
			train = append(train, row)
		}
	}
	a.TrainingSamples, a.CalibrationSamples = len(train), len(calibration)
	if len(train) < minFitSamples || len(calibration) < minCalibrationSamples {
		a.Warnings = append(a.Warnings, "时间切分及边界清除后训练或独立校准样本不足（至少 240 / 80）")
		return a
	}
	if !hasBothClasses(train, 20) || !hasBothClasses(calibration, 10) {
		a.Warnings = append(a.Warnings, "训练或校准时段缺少足够的平局与非平局样本，拒绝拟合")
		return a
	}
	a.TrainingStart, a.TrainingEnd = train[0].kickoff, train[len(train)-1].kickoff
	a.CalibrationStart, a.CalibrationEnd = calibration[0].kickoff, calibration[len(calibration)-1].kickoff
	for _, row := range rows {
		if row.finished.After(a.TrainingCutoff) {
			a.TrainingCutoff = row.finished
		}
	}
	a.Means, a.Scales = standardization(train)
	standardized := make([][]float64, len(train))
	labels := make([]float64, len(train))
	for i, row := range train {
		standardized[i] = standardizedVector(row.x, a.Means, a.Scales)
		labels[i] = row.y
	}
	a.Intercept, a.Coefficients = fitLogistic(standardized, labels, .025)
	calibrationLogits, calibrationLabels := make([]float64, len(calibration)), make([]float64, len(calibration))
	for i, row := range calibration {
		calibrationLogits[i] = linear(a.Intercept, a.Coefficients, standardizedVector(row.x, a.Means, a.Scales))
		calibrationLabels[i] = row.y
	}
	a.CalibrationIntercept, a.CalibrationSlope = fitCalibration(calibrationLogits, calibrationLabels)
	a.Fitted, a.Calibrated = true, true
	if !artifactValid(a, a.TrainingCutoff) {
		a.Fitted, a.Calibrated = false, false
		a.Warnings = append(a.Warnings, "拟合参数未通过数值与时间边界检查")
	}
	return a
}

func hasBothClasses(rows []trainingRow, minimum int) bool {
	yes := 0
	for _, row := range rows {
		if row.y == 1 {
			yes++
		}
	}
	return yes >= minimum && len(rows)-yes >= minimum
}

func standardization(rows []trainingRow) ([]float64, []float64) {
	means, scales := make([]float64, len(featureNames)), make([]float64, len(featureNames))
	for _, row := range rows {
		for j, x := range row.x {
			means[j] += x / float64(len(rows))
		}
	}
	for _, row := range rows {
		for j, x := range row.x {
			diff := x - means[j]
			scales[j] += diff * diff / float64(len(rows))
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

func standardizedVector(x, means, scales []float64) []float64 {
	result := make([]float64, len(x))
	for j, v := range x {
		result[j] = clamp((v-means[j])/scales[j], -12, 12)
	}
	return result
}

func linear(intercept float64, coefficients, x []float64) float64 {
	v := intercept
	for j, c := range coefficients {
		v += c * x[j]
	}
	return v
}

func softplus(v float64) float64 {
	if v > 30 {
		return v
	}
	if v < -30 {
		return math.Exp(v)
	}
	return math.Log1p(math.Exp(v))
}

// Full-batch gradient descent with backtracking fits the convex regularized
// objective. The unpenalized intercept retains the observed event frequency.
func fitLogistic(x [][]float64, y []float64, l2 float64) (float64, []float64) {
	coefficients := make([]float64, len(x[0]))
	mean := 0.0
	for _, v := range y {
		mean += v / float64(len(y))
	}
	intercept := logit(mean)
	loss := func(b float64, c []float64) float64 {
		v := 0.0
		for i, row := range x {
			z := linear(b, c, row)
			v += (softplus(z) - y[i]*z) / float64(len(y))
		}
		for _, w := range c {
			v += l2 * w * w / 2
		}
		return v
	}
	for iteration := 0; iteration < 1200; iteration++ {
		gradient, gb := make([]float64, len(coefficients)), 0.0
		for i, row := range x {
			residual := (sigmoid(linear(intercept, coefficients, row)) - y[i]) / float64(len(y))
			gb += residual
			for j, v := range row {
				gradient[j] += residual * v
			}
		}
		norm := gb * gb
		for j, w := range coefficients {
			gradient[j] += l2 * w
			norm += gradient[j] * gradient[j]
		}
		if norm < 1e-12 {
			break
		}
		current, step := loss(intercept, coefficients), 1.0
		next := make([]float64, len(coefficients))
		for attempt := 0; attempt < 25; attempt++ {
			for j, w := range coefficients {
				next[j] = w - step*gradient[j]
			}
			if loss(intercept-step*gb, next) <= current-1e-4*step*norm {
				break
			}
			step /= 2
		}
		intercept -= step * gb
		copy(coefficients, next)
	}
	return intercept, coefficients
}

// A positive-slope Platt calibrator preserves the model's ranking. Its
// parameters are fitted exclusively on the later time segment.
func fitCalibration(x, y []float64) (float64, float64) {
	intercept, slope := 0.0, 1.0
	loss := func(b, s float64) float64 {
		v := .01 * (s - 1) * (s - 1) / 2
		for i, z := range x {
			logOdds := b + s*z
			v += softplus(logOdds) - y[i]*logOdds
		}
		return v
	}
	for iteration := 0; iteration < 100; iteration++ {
		g0, g1, h00, h01, h11 := 0.0, .01*(slope-1), 1e-6, 0.0, .01
		for i, z := range x {
			p := sigmoid(intercept + slope*z)
			residual, v := p-y[i], p*(1-p)
			g0 += residual
			g1 += residual * z
			h00 += v
			h01 += v * z
			h11 += v * z * z
		}
		determinant := h00*h11 - h01*h01
		if determinant < 1e-12 {
			break
		}
		db, ds := (h11*g0-h01*g1)/determinant, (h00*g1-h01*g0)/determinant
		if math.Abs(db)+math.Abs(ds) < 1e-7 {
			break
		}
		step, current := 1.0, loss(intercept, slope)
		accepted := false
		for attempt := 0; attempt < 25; attempt++ {
			b, s := clamp(intercept-step*db, -15, 15), clamp(slope-step*ds, .05, 10)
			if loss(b, s) <= current {
				intercept, slope, accepted = b, s, true
				break
			}
			step /= 2
		}
		if !accepted {
			break
		}
	}
	return intercept, slope
}

func artifactValid(a Artifact, cutoff time.Time) bool {
	if !a.Fitted || !a.Calibrated || a.Version != Version || a.TrainingSamples < minFitSamples || a.CalibrationSamples < minCalibrationSamples || a.TrainingCutoff.IsZero() || a.TrainingCutoff.After(cutoff) {
		return false
	}
	if a.TrainingStart.IsZero() || a.TrainingEnd.Before(a.TrainingStart) || a.CalibrationStart.IsZero() || !a.CalibrationStart.After(a.TrainingEnd) || a.CalibrationEnd.Before(a.CalibrationStart) || a.TrainingCutoff.Before(a.CalibrationEnd) {
		return false
	}
	if len(a.FeatureNames) != len(featureNames) || len(a.Coefficients) != len(featureNames) || len(a.Means) != len(featureNames) || len(a.Scales) != len(featureNames) {
		return false
	}
	if !finite(a.Intercept) || !finite(a.CalibrationIntercept) || !finite(a.CalibrationSlope) || a.CalibrationSlope <= 0 {
		return false
	}
	for i, name := range featureNames {
		if a.FeatureNames[i] != name || !finite(a.Coefficients[i]) || !finite(a.Means[i]) || !finite(a.Scales[i]) || a.Scales[i] <= 0 {
			return false
		}
	}
	return true
}

func artifactProbability(a Artifact, x []float64) float64 {
	z := linear(a.Intercept, a.Coefficients, standardizedVector(x, a.Means, a.Scales))
	return sigmoid(a.CalibrationIntercept + a.CalibrationSlope*z)
}
