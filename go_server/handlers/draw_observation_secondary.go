package handlers

import (
	"math"
	"strconv"
	"strings"
)

const drawObservationSecondaryVersion = "secondary-v2-input-guards"
const drawObservationSecondaryThreshold = 0.225
const drawObservationSecondaryMinimumHeadToHead = 5

// Secondary screening does not change the frozen model's state or score. It
// only admits candidates with original sporttery quotes and a safe H2H sample.
// HeadToHead is the same strictly pre-match window supplied to the model/list.
type drawObservationSecondarySignal struct {
	Version                  string   `json:"version"`
	Decision                 string   `json:"decision"`
	Threshold                float64  `json:"threshold"`
	Score                    *float64 `json:"score,omitempty"`
	Reason                   string   `json:"reason"`
	Reasons                  []string `json:"reasons,omitempty"`
	ReasonCodes              []string `json:"reasonCodes,omitempty"`
	HeadToHeadSample         int      `json:"headToHeadSample"`
	MinimumHeadToHead        int      `json:"minimumHeadToHead"`
	SportteryQuotesAvailable bool     `json:"sportteryQuotesAvailable"`
}

// OpeningStatus=available is emitted only from the source's recorded SPF
// history. An lr/latest-only or synthetic quote must not satisfy this guard.
// A single genuine SPF record may be both the opening and latest quote.
func drawObservationSecondarySportteryQuotes(row drawObservationItem) bool {
	var source *drawObservationEuroOdds
	for index := range row.CompanyQuotes {
		company := &row.CompanyQuotes[index]
		if strings.TrimSpace(company.Key) != "sporttery" {
			continue
		}
		if source != nil { // ambiguous duplicate companies are not evidence
			return false
		}
		source = &company.EuroOdds
	}
	if source == nil || strings.TrimSpace(source.Source) != "odds_moneys" || source.OpeningStatus != "available" {
		return false
	}
	valid := func(values []*string) bool {
		if len(values) != 3 {
			return false
		}
		for _, raw := range values {
			if raw == nil {
				return false
			}
			value, err := strconv.ParseFloat(strings.TrimSpace(*raw), 64)
			if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 1 {
				return false
			}
		}
		return true
	}
	return valid(source.Opening) && valid(source.Latest)
}

// The decision deliberately never reads this match's score, settled state,
// profit fields, or score recommendations. Missing quote/history guards have
// their own reasons, rather than being described as a low model score.
func drawObservationSecondaryDecision(row drawObservationItem) drawObservationSecondarySignal {
	result := drawObservationSecondarySignal{
		Version: drawObservationSecondaryVersion, Decision: "unavailable",
		Threshold:        drawObservationSecondaryThreshold,
		HeadToHeadSample: row.HeadToHead.Sample, MinimumHeadToHead: drawObservationSecondaryMinimumHeadToHead,
		SportteryQuotesAvailable: drawObservationSecondarySportteryQuotes(row),
	}
	finish := func(decision, reason, code string) drawObservationSecondarySignal {
		result.Decision, result.Reason = decision, reason
		result.Reasons, result.ReasonCodes = []string{reason}, []string{code}
		return result
	}
	if strings.TrimSpace(row.JingcaiID) == "" {
		return finish("out_of_scope", "仅对有竞彩编号的比赛做二次筛选", "not_sporttery")
	}
	signal := row.FrozenDrawSignal
	if signal == nil {
		return finish("unavailable", "等待原固定模型结果", "model_unavailable")
	}
	if signal.State == "out_of_scope" {
		return finish("out_of_scope", "原模型不适用于本场比赛", "model_out_of_scope")
	}
	if signal.Score != nil && !math.IsNaN(*signal.Score) && !math.IsInf(*signal.Score, 0) && *signal.Score >= 0 && *signal.Score <= 1 {
		value := *signal.Score
		result.Score = &value
	}
	if signal.State == "not_selected" {
		return finish("not_selected", "原模型未入选，不进入二次筛选", "model_not_selected")
	}
	if signal.State != "selected" {
		return finish("unavailable", "原固定模型数据不足", "model_unavailable")
	}
	if result.Score == nil {
		return finish("unavailable", "原固定模型分数缺失或无效", "score_unavailable")
	}
	if !result.SportteryQuotesAvailable {
		result.Reasons = append(result.Reasons, "竞彩报价不完整：需要真实初盘及即时盘胜平负")
		result.ReasonCodes = append(result.ReasonCodes, "sporttery_quotes_incomplete")
	}
	if row.HeadToHead.Sample < drawObservationSecondaryMinimumHeadToHead {
		result.Reasons = append(result.Reasons, "赛前历史交锋不足5场")
		result.ReasonCodes = append(result.ReasonCodes, "head_to_head_insufficient")
	}
	if len(result.Reasons) > 0 {
		result.Decision, result.Reason = "rejected", strings.Join(result.Reasons, "；")
		return result
	}
	if *result.Score < drawObservationSecondaryThreshold {
		return finish("rejected", "原模型分数低于固定二次门槛0.225", "score_below_threshold")
	}
	return finish("retained", "原模型入选，竞彩初即时报价完整、赛前交锋至少5场且分数达到0.225", "retained")
}

func drawObservationSecondaryRetained(row drawObservationItem) bool {
	return drawObservationSecondaryDecision(row).Decision == "retained"
}
