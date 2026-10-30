package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go_server/models"
	"gorm.io/gorm"
)

// An offline, read-only export of actual fixture outcomes and the exact market
// calculations displayed in draw observations. History documents are read only
// for the existing frontend's simulated profit calculation, with their timing
// risks exported separately; target scores never enter the market features.
type DrawFeatureStudyDataset struct {
	GeneratedAt time.Time              `json:"generatedAt"`
	Version     string                 `json:"version"`
	Audit       map[string]interface{} `json:"audit"`
	Rows        []drawFeatureStudyRow  `json:"rows"`
}

type drawFeatureStudyPhase struct {
	Kelly         []float64 `json:"kelly"`
	KellyResult   []string  `json:"kellyResult"`
	KellySource   string    `json:"kellySource"`
	William       []float64 `json:"william"`
	Sporttery     []float64 `json:"sporttery"`
	Average       []float64 `json:"average"`
	SportteryDiff []float64 `json:"sportteryDiff"`
	AverageDiff   []float64 `json:"averageDiff"`
}

type drawFeatureStudyTiming struct {
	EuroCapturedAt        *time.Time `json:"euroCapturedAt"`
	LineCapturedAt        *time.Time `json:"lineCapturedAt"`
	EuroPreKickoff        *bool      `json:"euroPreKickoff"`
	LinePreKickoff        *bool      `json:"linePreKickoff"`
	SportteryFirstTime    string     `json:"sportteryFirstTime"`
	SportteryLatestTime   string     `json:"sportteryLatestTime"`
	SportteryCaptureKnown bool       `json:"sportteryCaptureKnown"`
}

type drawFeatureStudyHistory struct {
	Present                       bool       `json:"present"`
	UpdatedAt                     *time.Time `json:"updatedAt"`
	CapturedAt                    *time.Time `json:"capturedAt"`
	CapturedPreKickoff            *bool      `json:"capturedPreKickoff"`
	AgainstSummaryCount           int        `json:"againstSummaryCount"`
	RecentHomeUsedCount           int        `json:"recentHomeUsedCount"`
	RecentGuestUsedCount          int        `json:"recentGuestUsedCount"`
	UsedRecentAtOrAfterKickoff    int        `json:"usedRecentAtOrAfterKickoff"`
	UsedRecentTargetID            int        `json:"usedRecentTargetId"`
	UsedRecentUnknownTime         int        `json:"usedRecentUnknownTime"`
	AgainstListedAtOrAfterKickoff int        `json:"againstListedAtOrAfterKickoff"`
	AgainstListedTargetID         int        `json:"againstListedTargetId"`
	AgainstListedUnknownTime      int        `json:"againstListedUnknownTime"`
}

type drawFeatureStudyProfitInputs struct {
	OfficialProfitRatePresence [3]bool           `json:"officialProfitRatePresence"`
	DistributionSources        map[string]string `json:"distributionSources"`
}

type drawFeatureStudyGoalsHeatInputs struct {
	RecentHomeMatches     int     `json:"recentHomeMatches"`
	RecentGuestMatches    int     `json:"recentGuestMatches"`
	RecentHomeTotalGoals  float64 `json:"recentHomeTotalGoals"`
	RecentGuestTotalGoals float64 `json:"recentGuestTotalGoals"`
	RecentTotalGoals      float64 `json:"recentTotalGoals"`
	HistoryMatches        int     `json:"historyMatches"`
	HistoryTotalGoals     float64 `json:"historyTotalGoals"`
	CalculationLine       float64 `json:"calculationLine"`
	RecentScoresValid     bool    `json:"recentScoresValid"`
}

type drawFeatureStudyGoalsHeat struct {
	Available   bool                            `json:"available"`
	Reason      string                          `json:"reason"`
	Over        *float64                        `json:"over"`
	Under       *float64                        `json:"under"`
	OpeningLine *float64                        `json:"openingLine"`
	LatestLine  *float64                        `json:"latestLine"`
	Source      string                          `json:"source"`
	LineSource  string                          `json:"lineSource"`
	Inputs      drawFeatureStudyGoalsHeatInputs `json:"inputs"`
}

type drawFeatureStudyPreMatchHistory struct {
	HomeRecent drawObservationRecent `json:"homeRecent"`
	AwayRecent drawObservationRecent `json:"awayRecent"`
	HeadToHead drawObservationRecent `json:"headToHead"`
	Warnings   []string              `json:"warnings"`
}

type drawFeatureStudyAnalysisInputs struct {
	MarketProbabilities     []float64            `json:"marketProbabilities"`
	AverageOpening          []float64            `json:"averageOpening"`
	AverageLatest           []float64            `json:"averageLatest"`
	HistoryGoalAverage      *float64             `json:"historyGoalAverage"`
	RecentGoalAverage       *float64             `json:"recentGoalAverage"`
	CombinedGoalAverage     *float64             `json:"combinedGoalAverage"`
	CombinedHandicapAverage *float64             `json:"combinedHandicapAverage"`
	HomeRank                string               `json:"homeRank"`
	GuestRank               string               `json:"guestRank"`
	Tags                    []string             `json:"tags"`
	Warnings                []string             `json:"warnings"`
	KaiJuResult             []string             `json:"kaijuresult"`
	ChangGuiYaPan           string               `json:"changguiyapan"`
	ChangGuiQiuShu          string               `json:"changguiqiushu"`
	QiuShuAll               []interface{}        `json:"qiushuAll"`
	LiangDuiQiuShu          []interface{}        `json:"liangduiqiushu"`
	TouZhuE                 []int                `json:"touzhue"`
	LegacyKelly             []string             `json:"legacyKelly"`
	LegacyTicai             []string             `json:"legacyTicai"`
	Platform                *platformDecision    `json:"platform"`
	Availability            map[string]bool      `json:"availability"`
	TradeFields             map[string]*float64  `json:"tradeFields"`
	HotColdSources          map[string][3]string `json:"hotColdSources"`
	DistributionSources     map[string]string    `json:"distributionSources"`
}

type drawFeatureStudyRow struct {
	MatchID         string                           `json:"matchId"`
	Date            string                           `json:"date"`
	MatchTime       time.Time                        `json:"matchTime"`
	League          string                           `json:"league"`
	Home            string                           `json:"home"`
	Guest           string                           `json:"guest"`
	JingcaiID       string                           `json:"jingcaiId"`
	Result          int                              `json:"result"`
	HomeScore       int                              `json:"homeScore"`
	AwayScore       int                              `json:"awayScore"`
	HomeHeat        *float64                         `json:"homeHeat"`
	AsianOpening    *float64                         `json:"asianOpening"`
	AsianLine       *float64                         `json:"asianLine"`
	HeatSource      string                           `json:"heatSource"`
	Opening         drawFeatureStudyPhase            `json:"opening"`
	Latest          drawFeatureStudyPhase            `json:"latest"`
	Timing          drawFeatureStudyTiming           `json:"timing"`
	History         drawFeatureStudyHistory          `json:"history"`
	RoiSimulation   *matchRoiSimulationResponse      `json:"roiSimulation"`
	ProfitInputs    drawFeatureStudyProfitInputs     `json:"profitInputs"`
	GoalsHeat       drawFeatureStudyGoalsHeat        `json:"goalsHeat"`
	Frontend        *drawObservationFrontendAnalysis `json:"frontend"`
	CompanyQuotes   []drawObservationCompanyQuotes   `json:"companyQuotes"`
	PreMatchHistory *drawFeatureStudyPreMatchHistory `json:"preMatchHistory"`
	AnalysisInputs  drawFeatureStudyAnalysisInputs   `json:"analysisInputs"`
	ScoreInputs     *drawScoreInputs                 `json:"scoreInputs,omitempty"`
}

func drawStudyFrontendProjection(frontend *drawObservationFrontendAnalysis) *drawObservationFrontendAnalysis {
	if frontend == nil {
		return nil
	}
	result := *frontend
	if frontend.RoiSimulation != nil {
		simulation := *frontend.RoiSimulation
		simulation.Markets = []bookmakerMarketResponse{}
		for _, market := range frontend.RoiSimulation.Markets {
			if market.Simulated && (market.Key == "sportteryRqspf" || market.Key == "sportteryRqspfSim") {
				continue
			}
			simulation.Markets = append(simulation.Markets, market)
		}
		result.RoiSimulation = &simulation
	}
	return &result
}

func drawStudyPreMatchHistory(index drawObservationIndex, match models.Money) drawFeatureStudyPreMatchHistory {
	window := index.windows(drawModelMatch(match))
	return drawFeatureStudyPreMatchHistory{HomeRecent: window.home, AwayRecent: window.away, HeadToHead: window.h2h, Warnings: window.warnings}
}

func drawStudyTradeFields(odds models.OddsMoney) map[string]*float64 {
	result := map[string]*float64{}
	root := jsonValue[map[string]json.RawMessage](odds.SportteryTrade)
	if payload := jsonValue[sportteryTradePayload](odds.SportteryTrade); sportteryTradeHasData(payload.Data) {
		var selected map[string]json.RawMessage
		_ = json.Unmarshal(root["data"], &selected)
		root = selected
	}
	for section, fields := range map[string][]string{
		"tzbl":      {"h", "d", "a", "hprobability", "dprobability", "aprobability", "hsupportRate", "dsupportRate", "asupportRate", "herror", "derror", "aerror", "psyError"},
		"jyykSpf":   {"h", "d", "a", "hsupportRate", "dsupportRate", "asupportRate", "hy", "dy", "ay", "hlr", "dlr", "alr"},
		"jyykRqspf": {"h", "d", "a", "hsupportRate", "dsupportRate", "asupportRate", "hy", "dy", "ay"},
	} {
		var values map[string]json.RawMessage
		_ = json.Unmarshal(root[section], &values)
		for _, field := range fields {
			key := section + "." + field
			result[key] = nil
			raw, present := values[field]
			var value float64
			if present && len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" && json.Unmarshal(raw, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
				result[key] = &value
			}
		}
	}
	return result
}

func drawStudyAnalysisInputs(match models.Money, history models.HistoryMoney, odds models.OddsMoney, pankou models.PankouMoney, frontend *drawObservationFrontendAnalysis, timing drawFeatureStudyHistory) drawFeatureStudyAnalysisInputs {
	response := buildAnalysisFromData(match, history, odds, pankou, sportteryTradeFromJSON(odds.SportteryTrade))
	attachAnalysisWeights(&response)
	return drawStudyAnalysisInputsFromResponse(&response, odds, frontend, timing)
}

func drawStudyAnalysisInputsFromResponse(response *analysisMatchResponse, odds models.OddsMoney, frontend *drawObservationFrontendAnalysis, timing drawFeatureStudyHistory) drawFeatureStudyAnalysisInputs {
	result := drawFeatureStudyAnalysisInputs{HistoryGoalAverage: response.HistoryGoalAverage, RecentGoalAverage: response.RecentGoalAverage,
		CombinedGoalAverage: response.CombinedGoalAverage, CombinedHandicapAverage: response.CombinedHandicapAverage,
		HomeRank: response.HomeRank, GuestRank: response.GuestRank, Tags: response.Tags, Warnings: response.Warnings, KaiJuResult: response.KaiJuResult,
		ChangGuiYaPan: response.ChangGuiYaPan, ChangGuiQiuShu: response.ChangGuiQiuShu, QiuShuAll: response.QiuShuAll, LiangDuiQiuShu: response.LiangDuiQiuShu, TouZhuE: response.TouZhuE,
		LegacyKelly: response.KaiLiResult, LegacyTicai: response.TiCaiResult, Platform: buildPlatformDecision(response),
		Availability: map[string]bool{}, TradeFields: drawStudyTradeFields(odds), HotColdSources: map[string][3]string{}, DistributionSources: map[string]string{}}
	if drawObservationValidPrices(response.averageOdds) {
		result.AverageLatest = append([]float64(nil), response.averageOdds[:3]...)
		result.MarketProbabilities = probabilitiesFromOdds(response.averageOdds)
	}
	if drawObservationValidPrices(response.averageFirstOdds) {
		result.AverageOpening = append([]float64(nil), response.averageFirstOdds[:3]...)
	}
	result.Availability["averageLatest"], result.Availability["averageOpening"] = result.AverageLatest != nil, result.AverageOpening != nil
	result.Availability["historySummary"] = timing.AgainstSummaryCount > 0
	result.Availability["recentHome"], result.Availability["recentGuest"] = timing.RecentHomeUsedCount > 0, timing.RecentGuestUsedCount > 0
	result.Availability["historyRecentTimingClean"] = timing.UsedRecentAtOrAfterKickoff+timing.UsedRecentTargetID+timing.UsedRecentUnknownTime == 0
	if frontend != nil && frontend.PredictionAvailability != nil {
		result.Availability["strictKelly"] = frontend.PredictionAvailability.Kelly.Available
		result.Availability["strictTicai"] = frontend.PredictionAvailability.Ticai.Available
	}
	result.Availability["strictProfessionalInputs"] = result.Availability["strictKelly"] && result.Availability["strictTicai"]
	if frontend != nil && frontend.RoiSimulation != nil {
		result.DistributionSources = drawStudyProfitInputs(odds, frontend.RoiSimulation).DistributionSources
		for _, market := range frontend.RoiSimulation.Markets {
			if market.Key == "sportteryRqspf" && !market.Simulated {
				result.DistributionSources[market.Key] = "jyykRqspf"
			}
			sources := [3]string{"missing", "missing", "missing"}
			for index, item := range market.BookmakerByOutcome {
				if index >= 3 || item.HotColdIndex == nil {
					continue
				}
				if market.Key == "sporttery" && sportterySpfAvailable(response.sportteryTrade.JyykSpf) {
					if result.TradeFields["jyykSpf."+[]string{"hlr", "dlr", "alr"}[index]] != nil {
						sources[index] = "official"
					} else {
						sources[index] = "missing_default"
					}
				} else if market.Key == "sporttery" || market.Key == "sportterySim" {
					sources[index] = "local_simulated"
				}
			}
			result.HotColdSources[market.Key] = sources
		}
	}
	return result
}

// Copy the displayed heat, while describing whether its actual inputs are
// usable for research. The shared frontend still emits a pressure with empty
// history or a history-derived substitute line; those numeric values remain
// visible here but Available is false, never silently promoted to observations.
func drawStudyGoalsHeat(match models.Money, history models.HistoryMoney, pankou models.PankouMoney, frontend *drawObservationFrontendAnalysis, timing drawFeatureStudyHistory) drawFeatureStudyGoalsHeat {
	_, rows := pankouRows(pankou)
	selected := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Dxq), rows, 8)
	result := drawFeatureStudyGoalsHeat{Source: drawObservationLineBasis(selected, true), LineSource: "missing"}
	line := func(raw string) *float64 {
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		value := parsePankouLine(raw)
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil
		}
		return &value
	}
	result.OpeningLine, result.LatestLine = line(selected.FirstPankou), line(selected.Pankou)
	normalized := normalizeHistory(history)
	home, guest := summarizeRecent(normalized.RecentHomeList, match.Home), summarizeRecent(normalized.RecentGuestList, match.Guest)
	recentTotal := safeDivide(home.For+home.Against+guest.For+guest.Against, maxFloat(home.Matches+guest.Matches, 1))
	historyTotal := safeDivide(float64(normalized.AgainstSummary.WinGoal+normalized.AgainstSummary.LoseGoal), float64(maxInt(normalized.AgainstSummary.All, 1)))
	validScores := true
	for _, list := range [][]analysisHistoryMatch{normalized.RecentHomeList, normalized.RecentGuestList} {
		for _, row := range list[:minInt(len(list), 5)] {
			if len(row.Goal) >= 2 && (row.Goal[0] < 0 || row.Goal[1] < 0 || row.Goal[0] > 30 || row.Goal[1] > 30) {
				validScores = false
			}
		}
	}
	result.Inputs = drawFeatureStudyGoalsHeatInputs{RecentHomeMatches: int(home.Matches), RecentGuestMatches: int(guest.Matches), RecentHomeTotalGoals: home.For + home.Against, RecentGuestTotalGoals: guest.For + guest.Against,
		RecentTotalGoals: recentTotal, HistoryMatches: normalized.AgainstSummary.All, HistoryTotalGoals: historyTotal, RecentScoresValid: validScores}
	if frontend != nil {
		result.Inputs.CalculationLine = frontend.QiushuPankou2
		if len(frontend.QiuShuTouZhu) >= 2 {
			if over, ok := frontend.QiuShuTouZhu[0].(float64); ok && !math.IsNaN(over) && !math.IsInf(over, 0) {
				result.Over = &over
			}
			if under, ok := frontend.QiuShuTouZhu[1].(float64); ok && !math.IsNaN(under) && !math.IsInf(under, 0) {
				result.Under = &under
			}
		}
		if parsePankouLine(firstNonEmptyString(selected.Pankou, selected.FirstPankou)) != 0 {
			if strings.TrimSpace(selected.Pankou) != "" {
				result.LineSource = "latest"
			} else {
				result.LineSource = "opening_fallback"
			}
		} else if frontend.QiushuPankou2 > 0 {
			result.LineSource = "history_fallback"
		}
	}
	switch {
	case result.LatestLine == nil:
		result.Reason = "missing_or_invalid_latest_line"
	case home.Matches <= 0:
		result.Reason = "missing_recent_home"
	case guest.Matches <= 0:
		result.Reason = "missing_recent_guest"
	case !validScores:
		result.Reason = "invalid_recent_score"
	case timing.UsedRecentAtOrAfterKickoff+timing.UsedRecentTargetID+timing.UsedRecentUnknownTime > 0:
		result.Reason = "unsafe_or_unknown_recent_timing"
	case result.Over == nil || result.Under == nil:
		result.Reason = "missing_frontend_heat"
	case result.LineSource != "latest" || math.Abs(result.Inputs.CalculationLine-*result.LatestLine) > 1e-9:
		result.Reason = "calculation_line_not_real_latest"
	default:
		result.Available = true
	}
	return result
}

func drawStudyProfitInputs(odds models.OddsMoney, simulation *matchRoiSimulationResponse) drawFeatureStudyProfitInputs {
	result := drawFeatureStudyProfitInputs{DistributionSources: map[string]string{}}
	trade := sportteryTradeFromJSON(odds.SportteryTrade)
	root := jsonValue[map[string]json.RawMessage](odds.SportteryTrade)
	if payload := jsonValue[sportteryTradePayload](odds.SportteryTrade); sportteryTradeHasData(payload.Data) {
		var selected map[string]json.RawMessage
		_ = json.Unmarshal(root["data"], &selected)
		root = selected
	}
	var rawProfit map[string]json.RawMessage
	_ = json.Unmarshal(root["jyykSpf"], &rawProfit)
	for index, key := range []string{"hy", "dy", "ay"} {
		raw, present := rawProfit[key]
		var value float64
		result.OfficialProfitRatePresence[index] = present && len(raw) > 0 && string(raw) != "null" &&
			json.Unmarshal(raw, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
	}
	baseSource := "localSanhu"
	if _, available := percentValues([3]float64{trade.Tzbl.HSupportRate, trade.Tzbl.DSupportRate, trade.Tzbl.ASupportRate}); available {
		baseSource = "tzbl"
	} else if _, available := sportterySpfDistribution(trade); available {
		baseSource = "jyykSpf"
	}
	if simulation != nil {
		for _, market := range simulation.Markets {
			source := baseSource
			if market.Key == "sportterySim" {
				source = "localSanhu"
			} else if market.Key == "sporttery" {
				if _, available := sportterySpfDistribution(trade); available {
					source = "jyykSpf"
				}
			}
			result.DistributionSources[market.Key] = source
		}
	}
	return result
}

// Preserve exactly the markets shown by drawProfitMarkets, including the
// simulated sporttery fallback, but excluding both handicap simulation keys.
// Availability flags and optional official rates are copied without converting
// missing values to zero. Consumers must apply the same frontend availability
// gates before treating the always-numeric response fields as observations.
func drawStudyProfitMarkets(simulation *matchRoiSimulationResponse) *matchRoiSimulationResponse {
	if simulation == nil {
		return nil
	}
	result := *simulation
	result.Markets = []bookmakerMarketResponse{}
	for _, market := range simulation.Markets {
		if (market.Key == "sporttery" && !market.Simulated) ||
			(market.Simulated && market.Key != "sportteryRqspf" && market.Key != "sportteryRqspfSim") {
			result.Markets = append(result.Markets, market)
		}
	}
	return &result
}

type drawStudyHistoryEntry struct {
	MatchID   json.RawMessage `json:"matchId"`
	MatchTime string          `json:"matchTime"`
	Goal      []int           `json:"goal"`
}

func drawStudyHistoryInputs(match models.Money, history models.HistoryMoney) drawFeatureStudyHistory {
	result := drawFeatureStudyHistory{Present: history.MatchId != "", CapturedAt: history.CapturedAt,
		CapturedPreKickoff: drawStudyCaptureBefore(history.CapturedAt, drawSourceTime(match.MatchTime))}
	if !history.UpdatedAt.IsZero() {
		updated := history.UpdatedAt
		result.UpdatedAt = &updated
	}
	normalized := normalizeHistory(history)
	result.AgainstSummaryCount = normalized.AgainstSummary.All
	arrays := [3][]drawStudyHistoryEntry{
		jsonSlice[drawStudyHistoryEntry](history.RecentHomeList),
		jsonSlice[drawStudyHistoryEntry](history.RecentGuestList),
		jsonSlice[drawStudyHistoryEntry](history.AgainstList),
	}
	// Match normalizeHistory's all-split-data-empty fallback, including the
	// summary presence condition, rather than mixing legacy and split sections.
	if normalizedSplit := jsonValue[analysisHistorySummary](history.AgainstSummary); normalizedSplit.All <= 0 &&
		len(jsonSlice[analysisHistoryMatch](history.AgainstList)) == 0 &&
		len(jsonSlice[analysisHistoryMatch](history.RecentHomeList)) == 0 &&
		len(jsonSlice[analysisHistoryMatch](history.RecentGuestList)) == 0 {
		var payload struct {
			Against struct {
				List []drawStudyHistoryEntry `json:"list"`
			} `json:"against"`
			Recent struct {
				Home struct {
					List []drawStudyHistoryEntry `json:"list"`
				} `json:"home"`
				Guest struct {
					List []drawStudyHistoryEntry `json:"list"`
				} `json:"guest"`
			} `json:"recent"`
		}
		if json.Unmarshal(history.LeagueStat, &payload) == nil {
			arrays = [3][]drawStudyHistoryEntry{payload.Recent.Home.List, payload.Recent.Guest.List, payload.Against.List}
		}
	}
	for section, entries := range arrays {
		if section < 2 {
			entries = entries[:minInt(len(entries), 5)]
		}
		for _, entry := range entries {
			if section < 2 && len(entry.Goal) < 2 {
				continue // summarizeRecent ignores these rows, without replacing them.
			}
			if section == 0 {
				result.RecentHomeUsedCount++
			}
			if section == 1 {
				result.RecentGuestUsedCount++
			}
			id := drawObservationHistoryID(entry.MatchID)
			kickoff, known := drawHistoryTime(entry.MatchTime)
			sameID := id != "" && id == match.MatchId
			atOrAfter := known && !kickoff.Before(drawSourceTime(match.MatchTime))
			if section < 2 {
				if sameID {
					result.UsedRecentTargetID++
				}
				if atOrAfter {
					result.UsedRecentAtOrAfterKickoff++
				}
				if !known {
					result.UsedRecentUnknownTime++
				}
			} else {
				if sameID {
					result.AgainstListedTargetID++
				}
				if atOrAfter {
					result.AgainstListedAtOrAfterKickoff++
				}
				if !known {
					result.AgainstListedUnknownTime++
				}
			}
		}
	}
	return result
}

func drawStudyPhase(stage drawObservationComparisonStage) drawFeatureStudyPhase {
	phase := drawFeatureStudyPhase{KellyResult: []string{}, William: stage.William, Sporttery: stage.Sporttery, Average: stage.Average}
	if stage.Kelly.Available {
		phase.Kelly, phase.KellyResult, phase.KellySource = stage.Kelly.Values, stage.Kelly.Result, stage.Kelly.Basis
	}
	if stage.Ticai.Available {
		phase.SportteryDiff = stage.Ticai.Values
	}
	if stage.WilliamAverage.Available {
		phase.AverageDiff = stage.WilliamAverage.Values
	}
	return phase
}

func drawStudyCaptureBefore(captured *time.Time, kickoff time.Time) *bool {
	if captured == nil || captured.IsZero() {
		return nil
	}
	value := captured.Before(kickoff)
	return &value
}

func drawStudyAsianSource(pankou models.PankouMoney) drawObservationQuote {
	var preferred map[string]json.RawMessage
	_ = json.Unmarshal(pankou.Bet365Asia, &preferred)
	if preferred != nil {
		quote := drawObservationQuoteRow(preferred)
		if quote.company != "" || quote.opening != nil || quote.latest != nil || drawObservationAnyRawOdds(quote.latestOdds) {
			return quote
		}
	}
	rows, _ := drawObservationMarketRows(pankou.AsiaData, "asia")
	for _, row := range rows {
		if row.companyID == "8" {
			return row
		}
	}
	if len(rows) > 0 {
		return rows[0]
	}
	return drawObservationQuote{}
}

type drawStudyAsianHeatInputs struct {
	HomeHeat *float64
	Opening  *float64
	Latest   *float64
	Source   string
}

// Keep the research export and observation score on the same strict inputs.
// The shared frontend may fall back to zero or an opening line when a current
// quote is absent; those fallback values are not observed current handicaps.
func drawStudyStrictAsianHeat(odds models.OddsMoney, pankou models.PankouMoney) drawStudyAsianHeatInputs {
	result := drawStudyAsianHeatInputs{}
	avg, _, _ := resolveAverageOdds(odds)
	source := drawStudyAsianSource(pankou)
	if source.opening != nil {
		result.Opening = drawLine(*source.opening, false)
	}
	if source.latest != nil {
		result.Latest = drawLine(*source.latest, false)
	}
	if drawObservationValidPrices(avg) && result.Opening != nil && result.Latest != nil {
		probabilities := probabilitiesFromOdds(avg)
		home, _ := pressurePair(probabilities[0], probabilities[2], *result.Opening, *result.Latest)
		home = round2(home)
		result.HomeHeat, result.Source = &home, fmt.Sprintf("%s (pankou ID %s)", source.company, source.companyID)
	}
	return result
}

func drawFeatureStudyBuildRow(match models.Money, history models.HistoryMoney, odds models.OddsMoney, pankou models.PankouMoney) drawFeatureStudyRow {
	enrichment := buildDrawObservationFrontend(match, history, odds, pankou)
	return drawFeatureStudyBuildRowWithEnrichment(match, history, odds, pankou, enrichment)
}

func drawFeatureStudyBuildRowWithEnrichment(match models.Money, history models.HistoryMoney, odds models.OddsMoney, pankou models.PankouMoney, enrichment drawObservationFrontendEnrichment) drawFeatureStudyRow {
	row := drawFeatureStudyRow{MatchID: match.MatchId, Date: drawObservationDate(match), MatchTime: drawSourceTime(match.MatchTime).In(drawSourceLocation), League: drawObservationLeague(match), Home: match.Home, Guest: match.Guest, JingcaiID: match.JingcaiID}
	frontend := enrichment.analysis
	row.Frontend, row.CompanyQuotes = drawStudyFrontendProjection(frontend), enrichment.companies
	row.ScoreInputs = drawObservationBuildScoreInputs(match.MatchId, odds)
	row.RoiSimulation = drawStudyProfitMarkets(frontend.RoiSimulation)
	row.ProfitInputs = drawStudyProfitInputs(odds, row.RoiSimulation)
	row.History = drawStudyHistoryInputs(match, history)
	row.GoalsHeat = drawStudyGoalsHeat(match, history, pankou, frontend, row.History)
	if enrichment.studyResponse != nil {
		row.AnalysisInputs = drawStudyAnalysisInputsFromResponse(enrichment.studyResponse, odds, row.Frontend, row.History)
	} else {
		row.AnalysisInputs = drawStudyAnalysisInputs(match, history, odds, pankou, row.Frontend, row.History)
	}
	stages := drawObservationComparisonStages(odds, drawObservationPredictions(odds))
	row.Opening, row.Latest = drawStudyPhase(stages[0]), drawStudyPhase(stages[len(stages)-1])
	asian := drawStudyStrictAsianHeat(odds, pankou)
	row.HomeHeat, row.AsianOpening, row.AsianLine, row.HeatSource = asian.HomeHeat, asian.Opening, asian.Latest, asian.Source
	row.Timing = drawFeatureStudyTiming{EuroCapturedAt: odds.CapturedAt, LineCapturedAt: pankou.CapturedAt, EuroPreKickoff: drawStudyCaptureBefore(odds.CapturedAt, row.MatchTime), LinePreKickoff: drawStudyCaptureBefore(pankou.CapturedAt, row.MatchTime)}
	var official struct {
		Code *int `json:"code"`
		Data struct {
			Spf struct {
				Odds []map[string]json.RawMessage `json:"odds"`
			} `json:"spf"`
		} `json:"data"`
	}
	if json.Unmarshal(sportteryRecordedBody(odds.SportteryTrade), &official) == nil && official.Code != nil && *official.Code == 0 {
		for _, quote := range official.Data.Spf.Odds {
			if drawObservationRawPrices([]*string{drawObservationRawLine(quote["spf3"]), drawObservationRawLine(quote["spf1"]), drawObservationRawLine(quote["spf0"])}) != nil {
				row.Timing.SportteryFirstTime = drawRawString(quote["updateTime"])
				break
			}
		}
		if n := len(official.Data.Spf.Odds); n > 0 {
			row.Timing.SportteryLatestTime = drawRawString(official.Data.Spf.Odds[n-1]["updateTime"])
		}
	}
	return row
}

// ExportDrawFeatureStudy executes SELECTs in one read-only repeatable-read
// transaction. It does not initialize a server, migrate schemas or write Redis.
func ExportDrawFeatureStudy(db *gorm.DB, now time.Time) (DrawFeatureStudyDataset, error) {
	return exportDrawFeatureStudy(db, now, false)
}

// exportDrawFeatureStudy's research variant also archives upcoming fixtures.
// Unconfirmed results are explicitly -1 and never become training labels.
func exportDrawFeatureStudy(db *gorm.DB, now time.Time, includeUpcoming bool) (DrawFeatureStudyDataset, error) {
	data := DrawFeatureStudyDataset{GeneratedAt: now.UTC(), Version: "draw-market-feature-study-v4-full-inputs", Audit: map[string]interface{}{}, Rows: []drawFeatureStudyRow{}}
	err := db.Transaction(func(tx *gorm.DB) error {
		schema, err := drawObservationReadSchema(tx)
		if err != nil {
			return err
		}
		targets, err := drawObservationReadTargets(tx, schema, nil)
		if err != nil {
			return err
		}
		knownScores := map[string]bool{}
		for _, table := range []string{"money", "moneys"} {
			if !schema[table]["match_id"] || !schema[table]["home_score"] || !schema[table]["guest_score"] {
				continue
			}
			var rows []struct {
				MatchID string
				Present bool
			}
			if err = tx.Table(table).Select("match_id, (home_score IS NOT NULL AND guest_score IS NOT NULL) AS present").Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				knownScores[row.MatchID] = row.Present
			}
		}
		finished := []drawObservationTarget{}
		excluded := map[string]int{"unfinished_or_ambiguous_regulation": 0, "unknown_score": 0, "missing_or_future_kickoff": 0}
		for _, target := range targets {
			m := target.Match
			if includeUpcoming && !m.MatchTime.IsZero() {
				finished = append(finished, target)
				continue
			}
			if !knownScores[m.MatchId] {
				excluded["unknown_score"]++
				continue
			}
			if _, _, ok := drawRegulationResult(m); !ok {
				excluded["unfinished_or_ambiguous_regulation"]++
				continue
			}
			if m.MatchTime.IsZero() || drawSourceTime(m.MatchTime).After(now) {
				excluded["missing_or_future_kickoff"]++
				continue
			}
			finished = append(finished, target)
		}
		sort.Slice(finished, func(i, j int) bool {
			a, b := finished[i].Match, finished[j].Match
			if drawObservationDate(a) != drawObservationDate(b) {
				return drawObservationDate(a) < drawObservationDate(b)
			}
			if !a.MatchTime.Equal(b.MatchTime) {
				return a.MatchTime.Before(b.MatchTime)
			}
			return a.MatchId < b.MatchId
		})
		ids := make([]string, 0, len(finished))
		for _, target := range finished {
			ids = append(ids, target.Match.MatchId)
		}
		var oddsRows []models.OddsMoney
		var lineRows []models.PankouMoney
		var historyRows []models.HistoryMoney
		var mainRows []models.Money
		if len(ids) > 0 && schema["moneys"]["match_id"] {
			if err = tx.Table("moneys").Select(drawObservationProjection(schema, "moneys", drawObservationFrontendMainColumns)).Where("match_id IN ?", ids).Find(&mainRows).Error; err != nil {
				return err
			}
		}
		if len(ids) > 0 && schema["odds_moneys"]["match_id"] {
			if err = tx.Table("odds_moneys").Select(drawObservationProjection(schema, "odds_moneys", drawObservationFrontendOddsColumns)).Where("match_id IN ?", ids).Find(&oddsRows).Error; err != nil {
				return err
			}
		}
		if len(ids) > 0 && schema["pankou_moneys"]["match_id"] {
			if err = tx.Table("pankou_moneys").Select(drawObservationProjection(schema, "pankou_moneys", drawObservationFrontendPankouColumns)).Where("match_id IN ?", ids).Find(&lineRows).Error; err != nil {
				return err
			}
		}
		if schema["history_moneys"]["match_id"] {
			columns := append(append([]string{}, drawObservationFrontendHistoryColumns...), "updated_at", "captured_at")
			if err = tx.Table("history_moneys").Select(drawObservationProjection(schema, "history_moneys", columns)).Find(&historyRows).Error; err != nil {
				return err
			}
		}
		odds := map[string]models.OddsMoney{}
		lines := map[string]models.PankouMoney{}
		histories := map[string]models.HistoryMoney{}
		mainByID := map[string]models.Money{}
		for _, m := range mainRows {
			mainByID[m.MatchId] = m
		}
		for _, o := range oddsRows {
			odds[o.MatchId] = o
		}
		for _, p := range lineRows {
			lines[p.MatchId] = p
		}
		for _, h := range historyRows {
			histories[h.MatchId] = h
		}
		historyPool := drawObservationParsedHistory(targets, historyRows, now)
		historyIndex := indexDrawObservations(historyPool)
		coverage := map[string]int{}
		confirmedCount := 0
		for _, target := range finished {
			m := target.Match
			if main, exists := mainByID[m.MatchId]; exists {
				m = main
			}
			row := drawFeatureStudyBuildRow(m, histories[m.MatchId], odds[m.MatchId], lines[m.MatchId])
			preMatch := drawStudyPreMatchHistory(historyIndex, m)
			row.PreMatchHistory = &preMatch
			row.HomeScore, row.AwayScore, row.Result = -1, -1, -1
			if h, a, confirmed := drawRegulationResult(m); confirmed && knownScores[m.MatchId] && !drawSourceTime(m.MatchTime).After(now) {
				row.HomeScore, row.AwayScore = h, a
				row.Result = actualOutcomeIndex(h, a)
			}
			if row.Result == 1 {
				coverage["draws"]++
			}
			if row.Result >= 0 {
				confirmedCount++
			}
			if preMatch.HomeRecent.Sample == 10 {
				coverage["pre_match_home10_complete"]++
			}
			if preMatch.AwayRecent.Sample == 10 {
				coverage["pre_match_away10_complete"]++
			}
			if preMatch.HeadToHead.Sample > 0 {
				coverage["pre_match_head_to_head_present"]++
			}
			if strings.TrimSpace(row.JingcaiID) != "" {
				coverage["sporttery_marked"]++
			}
			if row.HomeHeat != nil {
				coverage["heat"]++
			}
			if row.GoalsHeat.Available {
				coverage["goals_heat_available"]++
			} else {
				coverage["goals_heat_unavailable_"+row.GoalsHeat.Reason]++
			}
			if row.GoalsHeat.OpeningLine != nil && row.GoalsHeat.LatestLine != nil {
				coverage["goals_real_opening_latest_complete"]++
			}
			coverage["goals_line_source_"+row.GoalsHeat.LineSource]++
			if row.History.Present {
				coverage["history_document_present"]++
			}
			if row.History.RecentHomeUsedCount+row.History.RecentGuestUsedCount > 0 {
				coverage["history_used_recent_present"]++
			}
			if row.History.UsedRecentAtOrAfterKickoff+row.History.UsedRecentTargetID > 0 {
				coverage["history_used_recent_time_risk"]++
			}
			if row.History.AgainstListedAtOrAfterKickoff+row.History.AgainstListedTargetID > 0 {
				coverage["history_listed_against_time_risk"]++
			}
			if row.History.UsedRecentUnknownTime > 0 {
				coverage["history_used_recent_unknown_time"]++
			}
			if row.History.UpdatedAt != nil {
				coverage["history_updated_at_present"]++
			}
			if row.History.CapturedPreKickoff == nil {
				coverage["history_capture_unknown"]++
			} else if *row.History.CapturedPreKickoff {
				coverage["history_capture_before_kickoff"]++
			} else {
				coverage["history_capture_after_kickoff"]++
			}
			if row.RoiSimulation != nil {
				for _, market := range row.RoiSimulation.Markets {
					prefix := "profit_" + market.Key
					if market.Simulated {
						prefix += "_simulated"
					}
					coverage[prefix+"_market"]++
					complete, rates := market.OddsAvailable && len(market.BookmakerByOutcome) == 3, market.OddsAvailable && len(market.BookmakerByOutcome) == 3
					for index, outcome := range market.BookmakerByOutcome {
						complete = complete && outcome.Available && outcome.Odds > 0
						rates = rates && outcome.Odds > 0 && outcome.OfficialProfitRate != nil && row.ProfitInputs.OfficialProfitRatePresence[index]
					}
					if complete {
						coverage[prefix+"_amount_roi_complete"]++
					}
					if !market.Simulated && rates {
						coverage[prefix+"_official_rate_complete"]++
					}
				}
			}
			for _, phase := range []struct {
				name  string
				value drawFeatureStudyPhase
			}{{"opening", row.Opening}, {"latest", row.Latest}} {
				if phase.value.Kelly != nil {
					coverage[phase.name+"_kelly"]++
				}
				if phase.value.SportteryDiff != nil {
					coverage[phase.name+"_sporttery_difference"]++
				}
				if phase.value.AverageDiff != nil {
					coverage[phase.name+"_average_difference"]++
				}
			}
			for _, capture := range []struct {
				name   string
				before *bool
			}{{"euro", row.Timing.EuroPreKickoff}, {"line", row.Timing.LinePreKickoff}} {
				state := "unknown"
				if capture.before != nil {
					state = "after_kickoff"
					if *capture.before {
						state = "before_kickoff"
					}
				}
				coverage[capture.name+"_capture_"+state]++
			}
			data.Rows = append(data.Rows, row)
		}
		data.Audit["mainUniqueMatches"], data.Audit["settledRegulationMatches"], data.Audit["excluded"], data.Audit["coverage"] = len(targets), confirmedCount, excluded, coverage
		if includeUpcoming {
			data.Audit["archivedMatches"] = len(finished)
		}
		data.Audit["historyDocumentsRead"], data.Audit["historyPoolUniqueMatches"] = len(historyRows), len(historyPool)
		data.Audit["featureDefinitions"] = map[string]string{"sportteryDiff": "William - Sporttery; order home/draw/away", "averageDiff": "William - market average", "literalSportteryAverageDiff": "Sporttery - average = averageDiff - sportteryDiff (or directly from raw prices)", "homeHeat": "UI pressurePair: de-vig average probabilities and actual opening/latest Asian handicap; unavailable inputs excluded", "outcomes": "0=home, 1=draw, 2=away; regulation time only"}
		data.Audit["profitDefinitions"] = map[string]string{
			"source":              "Exact shared buildDrawObservationFrontend calculation; only markets displayed by drawProfitMarkets (official sporttery plus simulated result markets, excluding sportteryRqspf/sportteryRqspfSim). No target scores enter any exported profit input.",
			"amount":              "bookmakerProfit = fixed totalStake (50,000,000) - totalStake * retailShare/100 * odds, rounded as the frontend; positive=bookmaker profit. Require market.oddsAvailable, outcome.odds>0 and outcome.available. Unavailable numeric zeros are not observations.",
			"roi":                 "bookmakerRoi = 100*bookmakerProfit/totalStake, same fixed base; amount and ROI are redundant features, not independent evidence. Simulated page rate uses bookmakerRoi.",
			"officialRate":        "Official page rate is source jyykSpf hy/dy/ay, not bookmakerRoi. Require priced outcome, officialProfitRate pointer AND profitInputs.officialProfitRatePresence for the corresponding home/draw/away field. The shared frontend can populate a zero pointer when raw fields are absent; presence flags distinguish these.",
			"distributionSources": "Per market key: jyykSpf=stored official SPF support percentages, tzbl=stored official betting-ratio support percentages, localSanhu=existing local weighted retail psychology. Official sporttery key alone does not prove its amount distribution is official.",
			"simulation":          "sportterySim is a separate local comparison using average prices, 0.88 return and localSanhu; sporttery with simulated=true is the frontend fallback and can use stored official support if present. Treat these keys as distinct strata.",
		}
		data.Audit["historyDefinitions"] = map[string]string{
			"source": "Same history_moneys columns consumed by the observation frontend (league_stat and split against/recent JSON). Simulation uses normalized against summary and first five recent array slots with a complete goal pair, without kickoff truncation. Original calculation is preserved, not silently repaired for this study.",
			"timing": "history.updatedAt is metadata only, never capture proof. capturedAt is copied only when actually stored; neither a later update nor a missing capture proves pre-match availability. Recent at/after kickoff and target-ID counts cover only goal-valid entries in the actually used first five slots. Against listed risk audits its supporting list; it cannot certify the summary denominator or reconstruct an earlier immutable summary.",
		}
		data.Audit["goalsHeatDefinitions"] = map[string]string{
			"display":      "over/under copy the exact shared frontend QiuShuTouZhu[0:2] numeric values, including when research availability is false; these are calculated pressure percentages, not observed betting-fund percentages.",
			"formula":      "Shared buildAnalysisFromData: recentTotalGoals=(sum of goals for+against across both teams' first five recent array slots with complete score pairs)/(total valid recent rows); over=clamp(50+(recentTotalGoals-calculationLine)*18,0,100), under=100-over, rounded as frontend.",
			"line":         "Actual selected source is the same selectPankouRow(Bet365Dxq, goals rows, ID 8). openingLine/latestLine parse only their own stored raw lines, missing/zero/negative lines are null. calculationLine copies the shared frontend value; lineSource marks latest/opening_fallback/history_fallback/missing, preserving the existing fallback formula.",
			"availability": "Strict research availability requires a real positive latest line matching calculationLine, at least one scored recent match for EACH team, valid recent scores, no target-ID/at-or-after-kickoff/unknown-time risk among the actually used first five slots, and finite displayed over/under values. Missing opening line is allowed and can be tested as an extra sensitivity filter. A real 0:0 recent score is valid, never treated as missing.",
			"dependence":   "No target result is used. Heat directly reuses recent-history totals and the goals line also used by simulated retail psychology/profit; adding it is not independent bookmaker evidence. All snapshot-timing limitations remain.",
		}
		data.Audit["fullFeatureDefinitions"] = map[string]string{
			"frontend":        "Exact observation frontend projection, with all real bookmaker markets retained but both simulated handicap keys removed. Top-level roiSimulation remains the old two-column selected-market projection. No feature container includes the current target score/result, PredictionHit, MyAngle, AccuracyFit or GoddessWoman.",
			"companyQuotes":   "Exact original three-company sporttery/William/Bet365 raw euro, Asian handicap and total-goals opening/latest quotes and water prices, with source/capture metadata. No cross-company or cross-phase filling. Presence and finite valid prices, not nonzero defaults, define availability.",
			"preMatchHistory": "The same canonical pool/identity resolver/index as the observation page, built once from all source history documents and authoritative main matches. Team ID priority and unambiguous normalized-name fallback; each side at most ten, head-to-head all prior meetings. Current target ID, future/unfinished/ambiguous-regulation records excluded. drawObservationBefore requires completedAt<target kickoff; unknown or invalid completion uses kickoff+2h, and embedded rows use the same conservative 2h buffer. This is an as-of data view, not proof the unknown-capture source snapshot was immutable before kickoff.",
			"analysisInputs":  "Narrow explicit projection of shared buildAnalysisFromData and deterministic buildPlatformDecision: real-price de-vig probabilities, source average opening/latest prices, original history/recent/combined averages and page analytics. No whole analysisMatchResponse is serialized. Raw tags, ranks, opening labels, legacy psychology and platform are snapshot-derived audit features; opening labels may fall back and are not source opening odds.",
			"platform":        "Pure deterministic legacy professional/compound strategy output, not independent evidence. Uses original legacy Kelly/Ticai with possible fallback and may indirectly consume simulated handicap markets. availability.strictKelly/strictTicai describe actual inputs; strictProfessionalInputs requires both. Strings such as warningAdjustedSummary/professionalConflict contain hard-coded past hit rates; never parse those percentages, display tones, names or template text as model inputs.",
			"tradeFields":     "Raw selected stored trade JSON numeric fields, null for absent/null/invalid and preserving real numeric zero. Use these to validate source probabilities/errors/psychology/profit/hot-cold fields that legacy structs can default to zero. hotColdSources separately distinguishes official/local_simulated/missing_default/missing per market outcome. distributionSources covers every retained market; even William/Bet365 ROI uses a shared official or local support distribution, not separate company capital flows.",
		}
		data.Audit["timingWarning"] = "Local-latest retrospective association study. Missing euro/line capture timestamps and no separate sporttery capture timestamp cannot establish immutable pre-match availability; sporttery updateTime has no year. Do not label the results verified prospective prediction accuracy."
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return data, err
}
