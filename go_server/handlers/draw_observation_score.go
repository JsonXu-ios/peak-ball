package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"go_server/models"
)

const drawScoreRecommendationVersion = "draw-score-dynamic-v2"

type drawRecommendedScore struct {
	Home int `json:"home"`
	Away int `json:"away"`
}
type drawScoreQuote struct {
	Home        int     `json:"home"`
	Away        int     `json:"away"`
	Odds        float64 `json:"odds"`
	SourceField string  `json:"sourceField"`
}

// The canonical odds row owns these quotes. SourceMatchID belongs to another
// provider identity and is provenance, not a canonical match-ID comparison.
// Euro CapturedAt cannot certify when the score quotes were received.
type drawScoreInputs struct {
	MatchID       string           `json:"matchId"`
	Source        string           `json:"source"`
	SourceMatchID string           `json:"sourceMatchId,omitempty"`
	Quotes        []drawScoreQuote `json:"quotes"`
	Stopped       *bool            `json:"stopped,omitempty"`
}
type drawScoreWeights struct {
	Recent  float64 `json:"recent"`
	History float64 `json:"history"`
	Market  float64 `json:"market"`
}
type drawScoreEvidence struct {
	MatchID          string            `json:"matchId"`
	QuoteSource      string            `json:"quoteSource,omitempty"`
	SourceMatchID    string            `json:"sourceMatchId,omitempty"`
	Quotes           []drawScoreQuote  `json:"quotes,omitempty"`
	Stopped          *bool             `json:"stopped,omitempty"`
	HomeSample       int               `json:"homeSample,omitempty"`
	AwaySample       int               `json:"awaySample,omitempty"`
	HeadToHeadSample int               `json:"headToHeadSample,omitempty"`
	GoalsLine        *float64          `json:"goalsLine,omitempty"`
	GoalsSource      string            `json:"goalsSource,omitempty"`
	RecentGoalMean   *float64          `json:"recentGoalMean,omitempty"`
	HeadToHeadMean   *float64          `json:"headToHeadMean,omitempty"`
	ExpectedGoals    *float64          `json:"expectedGoals,omitempty"`
	HomeGoalRate     *float64          `json:"homeGoalRate,omitempty"`
	AwayGoalRate     *float64          `json:"awayGoalRate,omitempty"`
	Weights          *drawScoreWeights `json:"weights,omitempty"`
	MaximumDrawGoals int               `json:"maximumDrawGoals,omitempty"`
}
type drawScoreRecommendation struct {
	Version  string                 `json:"version"`
	State    string                 `json:"state"`
	Scores   []drawRecommendedScore `json:"scores"`
	Reason   string                 `json:"reason"`
	Source   string                 `json:"source,omitempty"`
	Evidence *drawScoreEvidence     `json:"evidence,omitempty"`
}

// Parse only the four exact draw prices: sd4 means other draws, never 4:4.
func drawObservationBuildScoreInputs(matchID string, odds models.OddsMoney) *drawScoreInputs {
	if strings.TrimSpace(matchID) == "" || strings.TrimSpace(odds.MatchId) != strings.TrimSpace(matchID) {
		return nil
	}
	var body struct {
		Code *int `json:"code"`
		Data struct {
			MatchID json.RawMessage            `json:"matchId"`
			BF      map[string]json.RawMessage `json:"bf"`
		} `json:"data"`
	}
	if json.Unmarshal(sportteryRecordedBody(odds.SportteryTrade), &body) != nil || body.Code == nil || *body.Code != 0 || len(body.Data.BF) == 0 {
		return nil
	}
	r := &drawScoreInputs{MatchID: strings.TrimSpace(matchID), Source: "sporttery_score_odds", SourceMatchID: drawRawID(body.Data.MatchID), Quotes: []drawScoreQuote{}}
	if raw, present := body.Data.BF["stop"]; present {
		var stopped bool
		if json.Unmarshal(raw, &stopped) == nil {
			r.Stopped = &stopped
		}
	}
	for goals, field := range []string{"sd00", "sd11", "sd22", "sd33"} {
		price, err := strconv.ParseFloat(strings.TrimSpace(drawRawString(body.Data.BF[field])), 64)
		if err == nil && drawScoreValidOdds(price) {
			r.Quotes = append(r.Quotes, drawScoreQuote{Home: goals, Away: goals, Odds: price, SourceField: field})
		}
	}
	return r
}
func drawScoreValidOdds(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 1
}

// Target scores and settlement are not used. The list, metrics and score stage
// use the same row-aware secondary gate.
func drawObservationScoreRecommendation(item drawObservationItem) drawScoreRecommendation {
	r := drawScoreRecommendation{Version: drawScoreRecommendationVersion, State: "unavailable", Scores: []drawRecommendedScore{}, Reason: "等待原模型和二次筛选结果"}
	signal := item.FrozenDrawSignal
	if signal == nil {
		return r
	}
	if signal.State == "out_of_scope" || strings.TrimSpace(item.JingcaiID) == "" {
		r.State, r.Reason = "out_of_scope", "仅为二次入选的竞彩比赛提供两个平局比分"
		return r
	}
	if signal.State == "not_selected" {
		r.State, r.Reason = "not_selected", "原模型未入选"
		return r
	}
	if signal.State != "selected" || signal.Score == nil || math.IsNaN(*signal.Score) || math.IsInf(*signal.Score, 0) || *signal.Score < 0 || *signal.Score > 1 {
		return r
	}
	if !drawObservationSecondaryRetained(item) {
		r.State, r.Reason = "not_selected", "未通过当前二次筛选，暂不推荐比分"
		return r
	}
	quotes := drawScoreExactQuotes(item.MatchID, item.ScoreInputs)
	goals, goalsSource := drawScoreRealGoalsLine(item)
	if len(quotes) >= 2 {
		sort.SliceStable(quotes, func(i, j int) bool {
			if quotes[i].Odds != quotes[j].Odds {
				return quotes[i].Odds < quotes[j].Odds
			}
			if goals != nil {
				a, b := math.Abs(float64(2*quotes[i].Home)-*goals), math.Abs(float64(2*quotes[j].Home)-*goals)
				if a != b {
					return a < b
				}
			}
			return quotes[i].Home < quotes[j].Home
		})
		r.State, r.Source = "recommended", "sporttery_score_odds"
		r.Scores = []drawRecommendedScore{{Home: quotes[0].Home, Away: quotes[0].Away}, {Home: quotes[1].Home, Away: quotes[1].Away}}
		r.Reason = fmt.Sprintf("竞彩精确平局比分报价排序（%d/4项有效）：首选%d:%d（%.2f），次选%d:%d（%.2f）；不代表已校准的比分概率", len(quotes), quotes[0].Home, quotes[0].Away, quotes[0].Odds, quotes[1].Home, quotes[1].Away, quotes[1].Odds)
		r.Evidence = &drawScoreEvidence{MatchID: item.MatchID, QuoteSource: "odds_moneys.sporttery_trade.sportteryOdds.data.bf", SourceMatchID: item.ScoreInputs.SourceMatchID, Quotes: quotes, Stopped: item.ScoreInputs.Stopped, GoalsLine: goals, GoalsSource: goalsSource}
		if item.ScoreInputs.Stopped != nil && *item.ScoreInputs.Stopped {
			r.Reason += "；源站标记停售，保留报价仅供排序参考"
		}
		return r
	}
	return drawScoreGoalEstimate(item, r, goals, goalsSource)
}
func drawScoreExactQuotes(matchID string, input *drawScoreInputs) []drawScoreQuote {
	if input == nil || input.Source != "sporttery_score_odds" || strings.TrimSpace(matchID) == "" || input.MatchID != matchID {
		return nil
	}
	quotes, duplicate := map[int]drawScoreQuote{}, map[int]bool{}
	for _, q := range input.Quotes {
		if q.Home < 0 || q.Home > 3 || q.Home != q.Away || q.SourceField != fmt.Sprintf("sd%d%d", q.Home, q.Home) || !drawScoreValidOdds(q.Odds) {
			continue
		}
		if _, present := quotes[q.Home]; present {
			duplicate[q.Home] = true
		}
		quotes[q.Home] = q
	}
	r := []drawScoreQuote{}
	for goals := 0; goals <= 3; goals++ {
		if q, present := quotes[goals]; present && !duplicate[goals] {
			r = append(r, q)
		}
	}
	return r
}
func drawScoreRealGoalsLine(item drawObservationItem) (*float64, string) {
	read := func(line drawObservationLine) (*float64, string) {
		if line.Source != "pankou_moneys" || strings.TrimSpace(line.Company) == "" || line.Latest == nil {
			return nil, ""
		}
		v := drawLine(*line.Latest, true)
		if v == nil || *v <= 0 || *v > 15 || math.IsNaN(*v) || math.IsInf(*v, 0) {
			return nil, ""
		}
		return v, "pankou_moneys / " + line.Company
	}
	for _, q := range item.CompanyQuotes {
		if q.Key == "bet365" {
			if line, source := read(q.GoalsLine); line != nil {
				return line, source
			}
		}
	}
	return read(item.GoalsLine)
}
func drawScoreHistoryRecords(item drawObservationItem, recent drawObservationRecent, limit int) []drawObservationRecentMatch {
	r := []drawObservationRecentMatch{}
	seen := map[string]bool{}
	if item.MatchTime.IsZero() {
		return r
	}
	for _, m := range recent.Matches {
		// No completion time is available here; retain a conservative two-hour
		// allowance and require the actual completed W/D/L marker.
		if strings.TrimSpace(m.MatchID) == "" || m.MatchID == item.MatchID || seen[m.MatchID] || m.MatchTime.IsZero() || !m.MatchTime.Add(2*time.Hour).Before(item.MatchTime) || m.HomeScore < 0 || m.AwayScore < 0 {
			continue
		}
		if m.Result != "win" && m.Result != "draw" && m.Result != "loss" {
			continue
		}
		seen[m.MatchID] = true
		r = append(r, m)
		if limit > 0 && len(r) >= limit {
			break
		}
	}
	return r
}
func drawScoreTeamRates(records []drawObservationRecentMatch) (goalsFor, goalsAgainst float64, samples int) {
	for _, m := range records {
		switch m.Venue {
		case "home":
			goalsFor, goalsAgainst = goalsFor+float64(m.HomeScore), goalsAgainst+float64(m.AwayScore)
		case "away":
			goalsFor, goalsAgainst = goalsFor+float64(m.AwayScore), goalsAgainst+float64(m.HomeScore)
		default:
			continue
		}
		samples++
	}
	if samples > 0 {
		goalsFor, goalsAgainst = goalsFor/float64(samples), goalsAgainst/float64(samples)
	}
	return
}
func drawScoreGoalEstimate(item drawObservationItem, r drawScoreRecommendation, line *float64, source string) drawScoreRecommendation {
	hf, ha, hn := drawScoreTeamRates(drawScoreHistoryRecords(item, item.HomeRecent, 10))
	af, aa, an := drawScoreTeamRates(drawScoreHistoryRecords(item, item.AwayRecent, 10))
	history := drawScoreHistoryRecords(item, item.HeadToHead, 0)
	r.Reason = "缺少至少两个有效竞彩精确比分报价；进球估计需双方各至少5场、交锋至少5场安全赛前记录及真实大小球盘口"
	if hn < 5 || an < 5 || len(history) < 5 || line == nil {
		return r
	}
	historyMean := 0.0
	for _, m := range history {
		historyMean += float64(m.HomeScore + m.AwayScore)
	}
	historyMean /= float64(len(history))
	he, ae := (hf+aa)/2, (af+ha)/2
	recentMean := he + ae
	weights := drawScoreWeights{Recent: .5, History: .2, Market: .3}
	total := weights.Recent*recentMean + weights.History*historyMean + weights.Market*(*line)
	if math.IsNaN(total) || math.IsInf(total, 0) || total <= 0 {
		return r
	}
	ratio := .5
	if recentMean > 0 {
		ratio = he / recentMean
	}
	hr, ar := total*ratio, total*(1-ratio)
	if hr <= 0 || ar <= 0 {
		r.Reason = "竞彩精确比分报价不足，近期攻防估计不足以给出两个有效平局比分"
		return r
	}
	type candidate struct {
		goals int
		log   float64
	}
	candidates := []candidate{}
	for goals := 0; goals <= 6; goals++ {
		factorial, _ := math.Lgamma(float64(goals + 1))
		log := -hr - ar + float64(goals)*(math.Log(hr)+math.Log(ar)) - 2*factorial
		candidates = append(candidates, candidate{goals: goals, log: log})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].log != candidates[j].log {
			return candidates[i].log > candidates[j].log
		}
		return candidates[i].goals < candidates[j].goals
	})
	r.State, r.Source = "recommended", "goal_estimate"
	r.Scores = []drawRecommendedScore{{Home: candidates[0].goals, Away: candidates[0].goals}, {Home: candidates[1].goals, Away: candidates[1].goals}}
	r.Reason = fmt.Sprintf("进球估计：双方近期%d/%d场、交锋%d场；近期攻防50%%＋交锋20%%＋真实大小球30%%（%.2f）；按泊松假设在0:0至6:6中排序，非真实比分报价或已校准概率", hn, an, len(history), *line)
	r.Evidence = &drawScoreEvidence{MatchID: item.MatchID, HomeSample: hn, AwaySample: an, HeadToHeadSample: len(history), GoalsLine: line, GoalsSource: source, RecentGoalMean: &recentMean, HeadToHeadMean: &historyMean, ExpectedGoals: &total, HomeGoalRate: &hr, AwayGoalRate: &ar, Weights: &weights, MaximumDrawGoals: 6}
	return r
}
