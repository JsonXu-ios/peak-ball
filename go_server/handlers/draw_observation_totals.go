package handlers

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go_server/internal/drawmodeldata"
	"go_server/internal/jcgoalsmodel"
	"go_server/models"
)

// Independent of the original draw/secondary selection and its input archive.
const drawTotalsInputVersion = "jc-goals-three-block-input-v1"

var drawTotalsVersion = jcgoalsmodel.Version + ":" + drawTotalsInputVersion + ":" + jcgoalsmodel.ArtifactSHA256()

type drawObservationTotalsPrediction struct {
	ModelVersion      string  `json:"modelVersion"`
	State             string  `json:"state"`
	Direction         string  `json:"direction,omitempty"`
	Line              float64 `json:"line,omitempty"`
	OverWater         float64 `json:"overWater,omitempty"`
	UnderWater        float64 `json:"underWater,omitempty"`
	Score             float64 `json:"score,omitempty"`
	ExpectedNetReturn float64 `json:"expectedNetReturn,omitempty"`
	Reason            string  `json:"reason"`
}

type drawTotalsInputs struct {
	matchID       string
	probabilities []float64
}

func drawTotalsServingVersion() string {
	return drawTotalsVersion
}

// Only lottery days need upgrading. Unavailable is a completed calculation.
func drawTotalsStoredDayReady(day drawObservationStoredDay) bool {
	for _, row := range day.Items {
		if strings.TrimSpace(row.JingcaiID) == "" {
			continue
		}
		p := row.TotalsPrediction
		if day.TotalsVersion != drawTotalsServingVersion() || p == nil || p.ModelVersion != jcgoalsmodel.Version {
			return false
		}
		if p.State != "recommended" && p.State != "unavailable" {
			return false
		}
	}
	return true
}

// Use the already batched canonical odds row, without changing old model inputs.
func drawObservationBuildTotalsInputs(matchID string, odds models.OddsMoney) *drawTotalsInputs {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || strings.TrimSpace(odds.MatchId) != matchID {
		return nil
	}
	var body struct {
		Code *int `json:"code"`
		Data struct {
			JQ map[string]json.RawMessage `json:"jq"`
		} `json:"data"`
	}
	if json.Unmarshal(sportteryRecordedBody(odds.SportteryTrade), &body) != nil || body.Code == nil || *body.Code != 0 {
		return nil
	}
	var stopped *bool
	if json.Unmarshal(body.Data.JQ["stop"], &stopped) != nil || stopped == nil || *stopped {
		return nil
	}
	inv := make([]float64, 8)
	for i := range inv {
		price, err := strconv.ParseFloat(strings.TrimSpace(drawRawString(body.Data.JQ["t"+strconv.Itoa(i)])), 64)
		if err != nil || !drawScoreValidOdds(price) {
			return nil
		}
		inv[i] = 1 / price
	}
	total := drawTotalsSum(inv...)
	for i := range inv {
		inv[i] /= total
	}
	return &drawTotalsInputs{matchID: matchID, probabilities: inv}
}

func drawObservationTotalsSignal(item drawObservationItem, inputs *drawTotalsInputs) *drawObservationTotalsPrediction {
	p := &drawObservationTotalsPrediction{ModelVersion: jcgoalsmodel.Version, State: "unavailable"}
	if strings.TrimSpace(item.JingcaiID) == "" {
		p.State, p.Reason = "out_of_scope", "仅竞彩比赛提供大小球推荐"
		return p
	}
	input, reason := drawObservationTotalsModelInput(item, inputs)
	if reason != "" {
		p.Reason = reason
		return p
	}
	model, err := jcgoalsmodel.Load()
	if err != nil {
		p.Reason = "大小球模型暂不可用"
		return p
	}
	result, err := model.Predict(input)
	if err != nil {
		p.Reason = "大小球输入暂不可用"
		return p
	}
	p.State, p.Direction, p.Reason = "recommended", result.Direction, result.Reason
	p.Line, p.OverWater, p.UnderWater = result.Line, result.OverWater, result.UnderWater
	p.Score, p.ExpectedNetReturn = result.Score, result.ExpectedNetReturn
	return p
}

func drawObservationTotalsModelInput(item drawObservationItem, inputs *drawTotalsInputs) (jcgoalsmodel.Input, string) {
	input := jcgoalsmodel.Input{Features: map[string]float64{}}
	if inputs == nil || inputs.matchID != strings.TrimSpace(item.MatchID) || len(inputs.probabilities) != 8 {
		return input, "缺少完整且未停售的竞彩总进球赔率"
	}
	input.JQProbabilities = append([]float64(nil), inputs.probabilities...)
	terms, entropy := make([]float64, 8), make([]float64, 8)
	for i, p := range input.JQProbabilities {
		input.Features["jq.prob."+strconv.Itoa(i)] = p
		terms[i], entropy[i] = float64(i)*p, p*math.Log(p)
	}
	input.Features["jq.meanLowerBound"], input.Features["jq.entropy"] = drawTotalsSum(terms...), -drawTotalsSum(entropy...)
	// Reuse the existing market-number parser, never its draw prediction. Only
	// these named companies' real totals quotes enter this independent model.
	quotes := []drawObservationCompanyQuotes{}
	for _, q := range item.CompanyQuotes {
		if (q.Key == "bet365" || q.Key == "william") && q.GoalsLine.Source == "pankou_moneys" {
			quotes = append(quotes, q)
		}
	}
	data, _ := json.Marshal(struct {
		Quotes []drawObservationCompanyQuotes `json:"companyQuotes"`
	}{quotes})
	market, err := drawmodeldata.ExtractFeatures(data)
	if err != nil {
		return input, "大小球盘口暂不可用"
	}
	for _, company := range []string{"bet365", "william"} {
		for _, phase := range []string{"opening", "latest"} {
			for _, field := range []string{"line", "water1", "water2"} {
				if v, ok := market["line."+company+".goalsLine."+phase+"."+field]; ok {
					input.Features[company+"."+phase+"."+field] = v
				}
			}
			prefix := company + "." + phase + "."
			_, lineOK := input.Features[prefix+"line"]
			a, aOK := input.Features[prefix+"water1"]
			b, bOK := input.Features[prefix+"water2"]
			if lineOK && aOK && bOK && a > 0 && b > 0 {
				x, y := 1/(1+a), 1/(1+b)
				input.Features[prefix+"impliedOver"] = x / (x + y)
			}
		}
		prefix := company + ".latest."
		line, lok := input.Features[prefix+"line"]
		a, aok := input.Features[prefix+"water1"]
		b, bok := input.Features[prefix+"water2"]
		if !lok || !aok || !bok || line <= 0 || line > 6.5 || math.Abs(line*4-math.Round(line*4)) >= 1e-8 || a <= 0 || a > 2 || b <= 0 || b > 2 {
			name := "Bet365"
			if company == "william" {
				name = "威廉"
			}
			return input, "缺少" + name + "有效即时大小球盘口及双侧水位"
		}
		if company == "bet365" {
			input.Bet365 = jcgoalsmodel.Market{Line: line, OverWater: a, UnderWater: b}
		}
		for _, field := range []string{"line", "water1", "water2"} {
			a, aok := input.Features[company+".latest."+field]
			b, bok := input.Features[company+".opening."+field]
			if aok && bok {
				input.Features[company+".change."+field] = a - b
			}
		}
	}
	for _, field := range []string{"line", "water1", "water2", "impliedOver"} {
		a, aok := input.Features["bet365.latest."+field]
		b, bok := input.Features["william.latest."+field]
		if aok && bok {
			input.Features["difference.latest."+field] = a - b
		}
	}
	for _, team := range []struct {
		key, label string
		recent     drawObservationRecent
	}{{"home", "主队", item.HomeRecent}, {"away", "客队", item.AwayRecent}} {
		form := drawTotalsSafeForm(item, team.recent)
		if len(form) < 5 {
			return input, team.label + "不足5场安全赛前战绩"
		}
		for _, n := range []int{5, 10} {
			count := n
			if len(form) < count {
				count = len(form)
			}
			gf, ga := 0.0, 0.0
			for _, r := range form[:count] {
				if r.Venue == "home" {
					gf += float64(r.HomeScore)
					ga += float64(r.AwayScore)
				} else {
					gf += float64(r.AwayScore)
					ga += float64(r.HomeScore)
				}
			}
			gf, ga = gf/float64(count), ga/float64(count)
			suffix := strconv.Itoa(n)
			input.Features["form."+team.key+".gf"+suffix], input.Features["form."+team.key+".ga"+suffix] = gf, ga
			input.Features["form."+team.key+".total"+suffix] = gf + ga
		}
	}
	for _, n := range []string{"5", "10"} {
		input.Features["form.expectedTotal"+n] = drawTotalsSum(input.Features["form.home.gf"+n], input.Features["form.home.ga"+n], input.Features["form.away.gf"+n], input.Features["form.away.ga"+n]) / 2
	}
	return input, ""
}

func drawTotalsSafeForm(item drawObservationItem, recent drawObservationRecent) []drawObservationRecentMatch {
	rows, seen := []drawObservationRecentMatch{}, map[string]bool{}
	if item.MatchTime.IsZero() {
		return rows
	}
	for _, r := range recent.Matches {
		if r.MatchID == item.MatchID || seen[r.MatchID] || r.MatchTime.IsZero() || !r.MatchTime.Add(2*time.Hour).Before(item.MatchTime) || r.HomeScore < 0 || r.AwayScore < 0 || (r.Venue != "home" && r.Venue != "away") {
			continue
		}
		seen[r.MatchID] = true
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].MatchTime.After(rows[j].MatchTime) })
	return rows
}

// Match the compensated Python arithmetic used to freeze the feature matrix.
func drawTotalsSum(values ...float64) float64 {
	high, low := 0.0, 0.0
	for _, value := range values {
		total := high + value
		if math.Abs(high) >= math.Abs(value) {
			low += (high - total) + value
		} else {
			low += (value - total) + high
		}
		high = total
	}
	return high + low
}
