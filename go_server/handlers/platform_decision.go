// Package handlers: platform_decision.go is the server-side port of every
// decision computation that used to live in the H5 frontend
// (src/views/match/MatchListHome.vue). The goal is a single computation
// outlet: the frontend renders these fields verbatim and computes nothing.
//
// The port is intentionally bug-for-bug faithful to the TypeScript source —
// historical picks were made against the old outputs, so behaviour (including
// quirks such as a missing opening line being treated as 0) must not drift.
package handlers

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ---------- output structures (JSON names mirror the old TS shapes) ----------

type platformGoalResult struct {
	Label string `json:"label"`
	Total int    `json:"total"`
	Tone  string `json:"tone"`
}

type platformGuidePrediction struct {
	Outcome        string             `json:"outcome"`
	Goal           platformGoalResult `json:"goal"`
	Score          string             `json:"score"`
	SecondaryScore string             `json:"secondaryScore"`
	Warning        string             `json:"warning,omitempty"`
	WarningTone    string             `json:"warningTone,omitempty"`
}

type platformWarningRow struct {
	Value string `json:"value"`
	Tone  string `json:"tone"`
}

type platformStatRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Tone  string `json:"tone"`
}

type platformGoalPair struct {
	Home  *float64 `json:"home"`
	Guest *float64 `json:"guest"`
}

type platformGoalBands struct {
	Under platformGoalPair `json:"under"`
	Main  platformGoalPair `json:"main"`
	Over  platformGoalPair `json:"over"`
}

type platformDrawRisk struct {
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons"`
}

type platformEvilCultRow struct {
	Label         string `json:"label"`
	Primary       string `json:"primary"`
	Secondary     string `json:"secondary"`
	Tone          string `json:"tone"`
	PrimaryTone   string `json:"primaryTone"`
	SecondaryTone string `json:"secondaryTone"`
}

type platformEvilCultStep struct {
	Label      string  `json:"label"`
	Detail     string  `json:"detail"`
	OverDelta  float64 `json:"overDelta"`
	UnderDelta float64 `json:"underDelta"`
	OverScore  float64 `json:"overScore"`
	UnderScore float64 `json:"underScore"`
}

type platformEvilCultScores struct {
	Over         float64                `json:"over"`
	Under        float64                `json:"under"`
	OverPercent  int                    `json:"overPercent"`
	UnderPercent int                    `json:"underPercent"`
	Steps        []platformEvilCultStep `json:"steps"`
}

type platformEvilCultAuditInput struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Detail string `json:"detail"`
}

type platformEvilCultPrediction struct {
	Goal                   string  `json:"goal"`
	SecondaryGoal          string  `json:"secondaryGoal"`
	Total                  string  `json:"total"`
	SecondaryTotal         string  `json:"secondaryTotal"`
	UnderGoal              string  `json:"underGoal"`
	OverGoal               string  `json:"overGoal"`
	UnderTotalText         string  `json:"underTotalText"`
	OverTotalText          string  `json:"overTotalText"`
	UnderTotalValue        int     `json:"underTotalValue"`
	OverTotalValue         int     `json:"overTotalValue"`
	UnderGoalLine          float64 `json:"underGoalLine"`
	OverGoalLine           float64 `json:"overGoalLine"`
	UnderScore             string  `json:"underScore"`
	OverScore              string  `json:"overScore"`
	UnderOutcome           string  `json:"underOutcome"`
	OverOutcome            string  `json:"overOutcome"`
	FirstPick              string  `json:"firstPick"`
	FirstDirection         string  `json:"firstDirection"`
	MainPick               string  `json:"mainPick"`
	ReversePick            string  `json:"reversePick"`
	MainReason             string  `json:"mainReason"`
	SecondPassReason       string  `json:"secondPassReason"`
	SecondPassReversed     bool    `json:"secondPassReversed"`
	SecondPassForced       bool    `json:"secondPassForced"`
	SecondOverScore        float64 `json:"secondOverScore"`
	SecondUnderScore       float64 `json:"secondUnderScore"`
	MainTotal              int     `json:"mainTotal"`
	SecondaryTotalValue    int     `json:"secondaryTotalValue"`
	GoalDirection          string  `json:"goalDirection"`
	SecondaryGoalDirection string  `json:"secondaryGoalDirection"`
	GoalLine               float64 `json:"goalLine"`
	SecondaryGoalLine      float64 `json:"secondaryGoalLine"`
	Score                  string  `json:"score"`
	SecondaryScore         string  `json:"secondaryScore"`
	Outcome                string  `json:"outcome"`
	SecondaryOutcome       string  `json:"secondaryOutcome"`
	GoalTone               string  `json:"goalTone"`
	ReverseTone            string  `json:"reverseTone"`
	Note                   string  `json:"note"`
	Reason                 string  `json:"reason"`
}

type platformEvilCult struct {
	Line       float64                      `json:"line"`
	Rows       []platformEvilCultRow        `json:"rows"`
	Prediction platformEvilCultPrediction   `json:"prediction"`
	Scores     platformEvilCultScores       `json:"scores"`
	Inputs     []platformEvilCultAuditInput `json:"inputs"`
}

type platformDecision struct {
	Bookmaker              platformGuidePrediction  `json:"bookmaker"`
	Platform               platformGuidePrediction  `json:"platform"`
	WarningRows            []platformWarningRow     `json:"warningRows"`
	WarningAdjusted        *platformGuidePrediction `json:"warningAdjusted,omitempty"`
	WarningAdjustedSummary string                   `json:"warningAdjustedSummary"`
	ProfessionalConflict   *platformWarningRow      `json:"professionalConflict,omitempty"`
	ProfessionalConsensus  string                   `json:"professionalConsensus"`
	SportteryComfort       string                   `json:"sportteryComfort"`
	RqspfComfort           string                   `json:"rqspfComfort"`
	DrawRisk               platformDrawRisk         `json:"drawRisk"`
	HandicapPressureLabel  string                   `json:"handicapPressureLabel"`
	GoalBalanceSignal      string                   `json:"goalBalanceSignal"`
	Goals                  platformGoalBands        `json:"goals"`
	ZeroGoalAdvice         string                   `json:"zeroGoalAdvice"`
	HandicapAlertRows      []platformStatRow        `json:"handicapAlertRows"`
	GoalBalanceAlertRows   []platformStatRow        `json:"goalBalanceAlertRows"`
	EvilCult               platformEvilCult         `json:"evilCult"`
	LocalMarket            *bookmakerMarketResponse `json:"localMarket,omitempty"`
}

// ---------- small helpers mirroring the TS utility layer ----------

var pfNaN = math.NaN()

func pfFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

// pfParseFloatPrefix mimics JS Number.parseFloat: longest numeric prefix.
func pfParseFloatPrefix(text string) float64 {
	text = strings.TrimSpace(text)
	end := 0
	seenDigit, seenDot := false, false
	for index, char := range text {
		if char == '+' || char == '-' {
			if index != 0 {
				break
			}
			end = index + 1
			continue
		}
		if char == '.' {
			if seenDot {
				break
			}
			seenDot = true
			end = index + 1
			continue
		}
		if char < '0' || char > '9' {
			break
		}
		seenDigit = true
		end = index + 1
	}
	if !seenDigit {
		return pfNaN
	}
	value, err := strconv.ParseFloat(strings.TrimSuffix(text[:end], "."), 64)
	if err != nil {
		return pfNaN
	}
	return value
}

// pfNum mirrors parseOptionalNumber: NaN when missing/'-'/empty.
func pfNum(value interface{}) float64 {
	switch typed := value.(type) {
	case nil:
		return pfNaN
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case string:
		text := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(typed), "%"))
		if text == "" || text == "-" {
			return pfNaN
		}
		return pfParseFloatPrefix(text)
	default:
		text := strings.TrimSpace(strings.TrimSuffix(fmt.Sprint(value), "%"))
		if text == "" || text == "-" {
			return pfNaN
		}
		return pfParseFloatPrefix(text)
	}
}

// pfNumOrZero mirrors parseNumericValue: 0 when missing.
func pfNumOrZero(value interface{}) float64 {
	number := pfNum(value)
	if !pfFinite(number) {
		return 0
	}
	return number
}

func pfAt(values []interface{}, index int) interface{} {
	if index < 0 || index >= len(values) {
		return nil
	}
	return values[index]
}

func pfRound(value float64, digits int) float64 {
	factor := math.Pow(10, float64(digits))
	return math.Round(value*factor) / factor
}

var pfTrimTrailingZero = regexp.MustCompile(`(\.\d*[1-9])0+$`)

// pfTrim mirrors trimFixed: toFixed then strip trailing zeros.
func pfTrim(value float64, digits int) string {
	text := strconv.FormatFloat(value, 'f', digits, 64)
	if strings.Contains(text, ".") {
		zeros := strings.TrimRight(strings.SplitN(text, ".", 2)[1], "0")
		if zeros == "" {
			text = strings.SplitN(text, ".", 2)[0]
		} else {
			text = pfTrimTrailingZero.ReplaceAllString(text, "$1")
		}
	}
	return text
}

type pfWeighted struct {
	Value  float64
	Weight float64
}

func pfWeightedAverage(items []pfWeighted) float64 {
	totalWeight, total := 0.0, 0.0
	for _, item := range items {
		if !pfFinite(item.Value) || item.Weight <= 0 {
			continue
		}
		totalWeight += item.Weight
		total += item.Value * item.Weight
	}
	if totalWeight <= 0 {
		return pfNaN
	}
	return total / totalWeight
}

func pfSplitPair(value string) (string, string) {
	parts := strings.Split(value, ":")
	first, second := "", ""
	if len(parts) > 0 {
		first = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		second = strings.TrimSpace(parts[1])
	}
	return first, second
}

func pfValueText(value interface{}) string {
	if value == nil {
		return "-"
	}
	if text, ok := value.(string); ok {
		if text == "" {
			return "-"
		}
		return text
	}
	if number, ok := value.(float64); ok {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return fmt.Sprint(value)
}

func pfPercentText(value interface{}) string {
	text := pfValueText(value)
	if text == "-" || strings.HasSuffix(text, "%") {
		return text
	}
	return text + "%"
}

func pfOutcomeShortLabel(outcome string) string {
	switch outcome {
	case "home":
		return "主胜"
	case "away":
		return "客胜"
	case "draw":
		return "平局"
	}
	return "-"
}

func pfOutcomeLabelByKey(outcome, home, guest string) string {
	if outcome == "home" {
		return "主胜(" + home + ")"
	}
	if outcome == "away" {
		return "客胜(" + guest + ")"
	}
	return "平局"
}

var pfDrawPattern = regexp.MustCompile(`平`)
var pfAwayPattern = regexp.MustCompile(`客胜|主负|负`)
var pfHomePattern = regexp.MustCompile(`主胜|胜`)

func pfTextOutcome(value interface{}) string {
	text := strings.TrimSpace(pfValueText(value))
	if text == "" || text == "-" {
		return ""
	}
	if pfDrawPattern.MatchString(text) {
		return "draw"
	}
	if pfAwayPattern.MatchString(text) {
		return "away"
	}
	if pfHomePattern.MatchString(text) {
		return "home"
	}
	return ""
}

func pfTextOutcomes(values []string) []string {
	outcomes := make([]string, 0, len(values))
	for _, value := range values {
		if outcome := pfTextOutcome(value); outcome != "" {
			outcomes = append(outcomes, outcome)
		}
	}
	return outcomes
}

func pfPrimaryTextOutcome(values []string) string {
	outcomes := pfTextOutcomes(values)
	if len(outcomes) == 0 {
		return ""
	}
	return outcomes[0]
}

func pfOutcomeTone(outcome string) string {
	if outcome == "home" {
		return "red"
	}
	if outcome == "away" {
		return "green"
	}
	return "blue"
}

func pfScoreOutcome(score string) string {
	parts := strings.Split(score, ":")
	if len(parts) < 2 {
		return "draw"
	}
	home, errHome := strconv.Atoi(strings.TrimSpace(parts[0]))
	guest, errGuest := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errHome != nil || errGuest != nil {
		return "draw"
	}
	if home > guest {
		return "home"
	}
	if home < guest {
		return "away"
	}
	return "draw"
}

func pfScoreTone(score string) string { return pfOutcomeTone(pfScoreOutcome(score)) }

func pfOppositeOutcome(outcome string) string {
	if outcome == "home" {
		return "away"
	}
	if outcome == "away" {
		return "home"
	}
	return ""
}

// outcomeFromScores replicates the stable home→draw→away tie-break.
func pfOutcomeFromScores(scores map[string]float64) string {
	best, bestValue := "draw", math.Inf(-1)
	for _, key := range []string{"home", "draw", "away"} {
		if scores[key] > bestValue {
			best, bestValue = key, scores[key]
		}
	}
	return best
}

// ---------- odds / market helpers ----------

type pfDirectionValues struct{ Home, Draw, Away float64 }

func pfOddsFromStrings(values []string) *pfDirectionValues {
	if len(values) < 3 {
		return nil
	}
	home := pfNumOrZero(values[0])
	draw := pfNumOrZero(values[1])
	away := pfNumOrZero(values[2])
	if home <= 0 || draw <= 0 || away <= 0 {
		return nil
	}
	return &pfDirectionValues{home, draw, away}
}

func pfOddsFromFloats(values []float64) *pfDirectionValues {
	if len(values) < 3 {
		return nil
	}
	if values[0] <= 0 || values[1] <= 0 || values[2] <= 0 {
		return nil
	}
	return &pfDirectionValues{values[0], values[1], values[2]}
}

func pfBookmakerOddsDistribution(response *analysisMatchResponse) *pfDirectionValues {
	if odds := pfOddsFromFloats(response.SportteryOdds); odds != nil {
		return odds
	}
	return pfOddsFromStrings(response.Detail.Test8)
}

func pfBookmakerOddsProbabilities(response *analysisMatchResponse) *pfDirectionValues {
	odds := pfBookmakerOddsDistribution(response)
	if odds == nil {
		return nil
	}
	home, draw, away := 1/odds.Home, 1/odds.Draw, 1/odds.Away
	total := home + draw + away
	if total <= 0 {
		return nil
	}
	return &pfDirectionValues{home / total, draw / total, away / total}
}

func pfMarketByKey(response *analysisMatchResponse, key string) *bookmakerMarketResponse {
	if response.RoiSimulation == nil {
		return nil
	}
	for index := range response.RoiSimulation.Markets {
		if response.RoiSimulation.Markets[index].Key == key {
			return &response.RoiSimulation.Markets[index]
		}
	}
	return nil
}

func pfMarketComfortRow(market *bookmakerMarketResponse) *bookmakerOutcomeResponse {
	if market == nil {
		return nil
	}
	var best *bookmakerOutcomeResponse
	for index := range market.BookmakerByOutcome {
		row := &market.BookmakerByOutcome[index]
		if !row.Available || !pfFinite(row.BookmakerProfit) {
			continue
		}
		if best == nil || row.BookmakerProfit > best.BookmakerProfit {
			best = row
		}
	}
	return best
}

func pfMarketLossRow(market *bookmakerMarketResponse) *bookmakerOutcomeResponse {
	if market == nil {
		return nil
	}
	var worst *bookmakerOutcomeResponse
	for index := range market.BookmakerByOutcome {
		row := &market.BookmakerByOutcome[index]
		if !row.Available || !pfFinite(row.BookmakerProfit) {
			continue
		}
		if worst == nil || row.BookmakerProfit < worst.BookmakerProfit {
			worst = row
		}
	}
	return worst
}

func pfStrongComfortRow(row *bookmakerOutcomeResponse) *bookmakerOutcomeResponse {
	if row == nil || row.BookmakerProfit <= 0 {
		return nil
	}
	if pfFinite(row.BookmakerRoi) && row.BookmakerRoi >= 8 {
		return row
	}
	return nil
}

func pfStrongLossRow(row *bookmakerOutcomeResponse) *bookmakerOutcomeResponse {
	if row == nil || row.BookmakerProfit >= 0 {
		return nil
	}
	if pfFinite(row.BookmakerRoi) && row.BookmakerRoi <= -8 {
		return row
	}
	return nil
}

func pfMarketComfortOutcome(response *analysisMatchResponse, key string) string {
	if row := pfMarketComfortRow(pfMarketByKey(response, key)); row != nil {
		return row.Outcome
	}
	return ""
}

func pfStrongMarketComfortRow(response *analysisMatchResponse, key string) *bookmakerOutcomeResponse {
	return pfStrongComfortRow(pfMarketComfortRow(pfMarketByKey(response, key)))
}

func pfStrongMarketLossRow(response *analysisMatchResponse, key string) *bookmakerOutcomeResponse {
	return pfStrongLossRow(pfMarketLossRow(pfMarketByKey(response, key)))
}

// localProfitMarket: rebuilt from average odds + 散户心理, same as the H5 did.
func pfLocalProfitMarket(response *analysisMatchResponse) *bookmakerMarketResponse {
	odds := pfOddsFromStrings(response.Detail.Test8)
	if odds == nil || len(response.SanhuXinli) < 3 {
		return nil
	}
	home := pfNumOrZero(response.SanhuXinli[0])
	draw := pfNumOrZero(response.SanhuXinli[1])
	away := pfNumOrZero(response.SanhuXinli[2])
	total := home + draw + away
	if total <= 0 {
		return nil
	}
	shares := [3]float64{pfRound(home/total*100, 2), pfRound(draw/total*100, 2), pfRound(away/total*100, 2)}
	oddsValues := [3]float64{odds.Home, odds.Draw, odds.Away}
	totalStake := bookmakerTotalStake
	if response.RoiSimulation != nil && response.RoiSimulation.TotalStake > 0 {
		totalStake = response.RoiSimulation.TotalStake
	}
	market := bookmakerMarketResponse{
		Key:                "localAverageOdds",
		Name:               "本地测算",
		Odds:               directionValues{Home: pfRound(odds.Home, 2), Draw: pfRound(odds.Draw, 2), Away: pfRound(odds.Away, 2)},
		OddsAvailable:      true,
		RetailDistribution: directionValues{Home: shares[0], Draw: shares[1], Away: shares[2]},
	}
	for index, outcome := range bookmakerOutcomeKeys {
		betStake := pfRound(totalStake*shares[index]/100, 2)
		payout := pfRound(betStake*oddsValues[index], 2)
		profit := pfRound(totalStake-payout, 2)
		market.BookmakerByOutcome = append(market.BookmakerByOutcome, bookmakerOutcomeResponse{
			Outcome:         outcome,
			RetailShare:     shares[index],
			BetStake:        betStake,
			TotalStake:      totalStake,
			Odds:            pfRound(oddsValues[index], 2),
			Payout:          payout,
			BookmakerProfit: profit,
			BookmakerLoss:   pfRound(payout-totalStake, 2),
			BookmakerRoi:    pfRound(profit/totalStake*100, 2),
			Available:       true,
		})
	}
	return &market
}

func pfLocalComfortOutcome(response *analysisMatchResponse) string {
	if row := pfMarketComfortRow(pfLocalProfitMarket(response)); row != nil {
		return row.Outcome
	}
	return ""
}

func pfStrongLocalComfortRow(response *analysisMatchResponse) *bookmakerOutcomeResponse {
	return pfStrongComfortRow(pfMarketComfortRow(pfLocalProfitMarket(response)))
}

func pfSignedMoneyText(value float64, available bool) string {
	if !available || !pfFinite(value) {
		return "-"
	}
	sign := ""
	if value > 0 {
		sign = "+"
	}
	return sign + pfMoneyCompactText(value)
}

func pfMoneyCompactText(value float64) string {
	absolute := math.Abs(value)
	signed := ""
	if value < 0 {
		signed = "-"
	}
	if absolute >= 100000000 {
		return signed + pfTrim(absolute/100000000, 2) + "亿"
	}
	if absolute >= 10000 {
		return signed + pfTrim(absolute/10000, 1) + "万"
	}
	return signed + pfTrim(absolute, 0)
}

// ---------- history / recent helpers ----------

func pfNormalizeTeamName(value interface{}) string {
	return strings.Join(strings.Fields(pfValueTextRaw(value)), "")
}

func pfValueTextRaw(value interface{}) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func pfMatchOutcomeForCurrentTeams(record []interface{}, currentHome, currentGuest string) string {
	if len(record) == 0 {
		return ""
	}
	homeTeam := pfNormalizeTeamName(pfAt(record, 1))
	guestTeam := pfNormalizeTeamName(pfAt(record, 2))
	scoreA := pfNum(pfAt(record, 3))
	scoreB := pfNum(pfAt(record, 4))
	if !pfFinite(scoreA) || !pfFinite(scoreB) {
		return ""
	}
	currentHomeName := pfNormalizeTeamName(currentHome)
	currentGuestName := pfNormalizeTeamName(currentGuest)
	homeScore, guestScore := scoreA, scoreB
	if strings.Contains(homeTeam, currentGuestName) || strings.Contains(guestTeam, currentHomeName) {
		homeScore, guestScore = scoreB, scoreA
	} else if !(strings.Contains(homeTeam, currentHomeName) || strings.Contains(guestTeam, currentGuestName)) {
		return ""
	}
	if homeScore > guestScore {
		return "home"
	}
	if homeScore < guestScore {
		return "away"
	}
	return "draw"
}

func pfMatchOutcomeForTeam(record []interface{}, team string) string {
	if len(record) == 0 {
		return ""
	}
	homeTeam := pfNormalizeTeamName(pfAt(record, 1))
	guestTeam := pfNormalizeTeamName(pfAt(record, 2))
	scoreA := pfNum(pfAt(record, 3))
	scoreB := pfNum(pfAt(record, 4))
	if !pfFinite(scoreA) || !pfFinite(scoreB) {
		return ""
	}
	teamName := pfNormalizeTeamName(team)
	isHomeTeam := strings.Contains(homeTeam, teamName)
	isGuestTeam := strings.Contains(guestTeam, teamName)
	if !isHomeTeam && !isGuestTeam {
		return ""
	}
	teamScore, opponentScore := scoreA, scoreB
	if !isHomeTeam {
		teamScore, opponentScore = scoreB, scoreA
	}
	if teamScore > opponentScore {
		return "win"
	}
	if teamScore < opponentScore {
		return "loss"
	}
	return "draw"
}

func pfRecentTeamOutcome(record []interface{}, team, side string) string {
	result := pfMatchOutcomeForTeam(record, team)
	if result == "" {
		return ""
	}
	if result == "draw" {
		return "draw"
	}
	if side == "home" {
		if result == "win" {
			return "home"
		}
		return "away"
	}
	if result == "win" {
		return "away"
	}
	return "home"
}

func pfHasHistoryGoalSample(response *analysisMatchResponse) bool {
	if len(response.SanhuXinli) < 5 {
		return false
	}
	signal := strings.TrimSpace(response.SanhuXinli[4])
	return signal != "" && signal != "样本不足"
}

func pfHasTeamRecentGoalSample(record []interface{}) bool { return len(record) >= 5 }

func pfHasRecentGoalSample(response *analysisMatchResponse) bool {
	return pfHasTeamRecentGoalSample(response.HomeZuijinBisai) || pfHasTeamRecentGoalSample(response.GuestZuijinBisai)
}

func pfHistoryGoalSampleValue(response *analysisMatchResponse, value interface{}) float64 {
	if !pfHasHistoryGoalSample(response) {
		return pfNaN
	}
	return pfNum(value)
}

func pfRecentGoalSampleValue(response *analysisMatchResponse, value interface{}) float64 {
	if !pfHasRecentGoalSample(response) {
		return pfNaN
	}
	return pfNum(value)
}

func pfCombinedGoalAverage(history, recent float64) float64 {
	return pfWeightedAverage([]pfWeighted{{history, 0.45}, {recent, 0.55}})
}

func pfHistoryMatchOutcome(response *analysisMatchResponse) string {
	if !pfHasHistoryGoalSample(response) {
		return ""
	}
	return pfMatchOutcomeForCurrentTeams(response.LiangDuiBiSai, response.Home, response.Guest)
}

func pfHistorySmallScore(response *analysisMatchResponse) bool {
	record := response.LiangDuiBiSai
	if len(record) == 0 {
		return false
	}
	homeScore := pfNum(pfAt(record, 3))
	guestScore := pfNum(pfAt(record, 4))
	return pfFinite(homeScore) && pfFinite(guestScore) && homeScore+guestScore <= 2
}

// ---------- expected goals ----------

func pfFallbackAverage(averageValue, totalValue interface{}) float64 {
	average := pfNum(averageValue)
	if pfFinite(average) {
		return average
	}
	total := pfNum(totalValue)
	if pfFinite(total) {
		return total / 5
	}
	return pfNaN
}

func pfExpectedTeamGoalBase(attackAverage float64, attackTotal, maxGoalValue interface{}, opponentAgainstAverage float64, opponentAgainstTotal interface{}) float64 {
	attack := attackAverage
	if !pfFinite(attack) {
		attack = pfFallbackAverage(nil, attackTotal)
	}
	against := opponentAgainstAverage
	if !pfFinite(against) {
		against = pfFallbackAverage(nil, opponentAgainstTotal)
	}
	maxGoal := pfNum(maxGoalValue)
	if !pfFinite(attack) && !pfFinite(against) && !pfFinite(maxGoal) {
		return pfNaN
	}
	attackBase := attack
	if !pfFinite(attackBase) {
		attackBase = against
	}
	againstBase := against
	if !pfFinite(againstBase) {
		againstBase = attackBase
	}
	peakBase := attackBase
	if pfFinite(maxGoal) {
		peakBase = math.Min(maxGoal, attackBase+1.5)
	}
	return pfRound(math.Max(0, attackBase*0.5+againstBase*0.35+peakBase*0.15), 2)
}

func pfZeroGoalRiskScore(attackAverage, opponentAgainstAverage, teamBase, totalGoalAnchor, handicap float64, side string) float64 {
	risk := 0.0
	if pfFinite(attackAverage) {
		if attackAverage <= 0.6 {
			risk += 0.35
		} else if attackAverage <= 1 {
			risk += 0.18
		}
	}
	if pfFinite(opponentAgainstAverage) {
		if opponentAgainstAverage <= 0.8 {
			risk += 0.3
		} else if opponentAgainstAverage <= 1.1 {
			risk += 0.15
		}
	}
	if pfFinite(teamBase) && teamBase <= 0.75 {
		risk += 0.2
	}
	if pfFinite(totalGoalAnchor) && totalGoalAnchor <= 2.25 {
		risk += 0.15
	}
	if pfFinite(handicap) {
		unsupported := handicap <= -0.25
		if side != "home" {
			unsupported = handicap >= 0.25
		}
		if unsupported {
			risk += 0.15
		}
	}
	return risk
}

func pfApplyZeroGoalRisk(expectedGoal, attackAverage, opponentAgainstAverage, teamBase, totalGoalAnchor, handicap float64, side string) float64 {
	risk := pfZeroGoalRiskScore(attackAverage, opponentAgainstAverage, teamBase, totalGoalAnchor, handicap, side)
	if risk >= 0.65 && expectedGoal < 0.9 {
		return 0.49
	}
	if risk >= 0.5 && expectedGoal < 1.15 {
		return expectedGoal * 0.7
	}
	return expectedGoal
}

type pfGoalScore struct{ Home, Guest float64 }

func pfExpectedGoalPair(response *analysisMatchResponse) pfGoalScore {
	homeRecentAvailable := pfHasTeamRecentGoalSample(response.HomeZuijinBisai)
	guestRecentAvailable := pfHasTeamRecentGoalSample(response.GuestZuijinBisai)
	homeAttackAverage, guestAttackAverage := pfNaN, pfNaN
	homeAgainstAverage, guestAgainstAverage := pfNaN, pfNaN
	if homeRecentAvailable {
		homeAttackAverage = pfFallbackAverage(pfAt(response.LiangDuiQiuShu, 0), pfAt(response.QiuShuAll, 0))
		homeAgainstAverage = pfFallbackAverage(pfAt(response.LiangDuiQiuShu, 2), pfAt(response.QiuShuAll, 4))
	}
	if guestRecentAvailable {
		guestAttackAverage = pfFallbackAverage(pfAt(response.LiangDuiQiuShu, 1), pfAt(response.QiuShuAll, 2))
		guestAgainstAverage = pfFallbackAverage(pfAt(response.LiangDuiQiuShu, 3), pfAt(response.QiuShuAll, 5))
	}
	homeBase := pfExpectedTeamGoalBase(homeAttackAverage, pfAt(response.QiuShuAll, 0), pfAt(response.QiuShuAll, 1), guestAgainstAverage, pfAt(response.QiuShuAll, 5))
	guestBase := pfExpectedTeamGoalBase(guestAttackAverage, pfAt(response.QiuShuAll, 2), pfAt(response.QiuShuAll, 3), homeAgainstAverage, pfAt(response.QiuShuAll, 4))
	if !pfFinite(homeBase) && !pfFinite(guestBase) {
		return pfGoalScore{pfNaN, pfNaN}
	}
	safeHomeBase, safeGuestBase := homeBase, guestBase
	if !pfFinite(safeHomeBase) {
		safeHomeBase = 0
	}
	if !pfFinite(safeGuestBase) {
		safeGuestBase = 0
	}
	baseTotal := safeHomeBase + safeGuestBase
	_, recentTotalGoalsValue := pfSplitPair(response.ChangGuiQiuShu)
	totalGoalAnchor := pfWeightedAverage([]pfWeighted{
		{baseTotal, 0.25},
		{pfRecentGoalSampleValue(response, recentTotalGoalsValue), 0.4},
		{response.QiushuPankou2, 0.35},
	})
	finalTotal := totalGoalAnchor
	if !pfFinite(finalTotal) {
		finalTotal = baseTotal
	}
	homeShare := 0.5
	if baseTotal > 0 {
		homeShare = safeHomeBase / baseTotal
	}
	rawHome := math.Max(0, finalTotal*homeShare)
	rawGuest := math.Max(0, finalTotal*(1-homeShare))
	return pfGoalScore{
		Home:  pfApplyZeroGoalRisk(rawHome, homeAttackAverage, guestAgainstAverage, safeHomeBase, finalTotal, response.YapanPankou2, "home"),
		Guest: pfApplyZeroGoalRisk(rawGuest, guestAttackAverage, homeAgainstAverage, safeGuestBase, finalTotal, response.YapanPankou2, "guest"),
	}
}

func pfAllocateIntegerGoalTotal(total float64, base pfGoalScore, handicap float64, hasHandicap bool) pfGoalScore {
	normalizedTotal := math.Max(0, math.Round(total))
	if normalizedTotal <= 0 {
		return pfGoalScore{0, 0}
	}
	baseTotal := base.Home + base.Guest
	homeShare := 0.5
	if pfFinite(baseTotal) && baseTotal > 0 {
		homeShare = base.Home / baseTotal
	}
	home := math.Max(0, math.Min(normalizedTotal, math.Round(normalizedTotal*homeShare)))
	guest := normalizedTotal - home
	if !hasHandicap || !pfFinite(handicap) || math.Abs(handicap) < 0.5 {
		return pfGoalScore{home, guest}
	}
	favorite := "guest"
	if handicap > 0 {
		favorite = "home"
	}
	minGap := math.Min(normalizedTotal, math.Ceil(math.Abs(handicap)))
	currentGap := home - guest
	if favorite == "guest" {
		currentGap = guest - home
	}
	if currentGap >= minGap {
		return pfGoalScore{home, guest}
	}
	favoriteGoals := math.Min(normalizedTotal, math.Ceil((normalizedTotal+minGap)/2))
	underdogGoals := normalizedTotal - favoriteGoals
	if favorite == "home" {
		return pfGoalScore{favoriteGoals, underdogGoals}
	}
	return pfGoalScore{underdogGoals, favoriteGoals}
}

func pfAllocateExpectedGoalTotal(total float64, base pfGoalScore, handicap float64, forceInteger bool) pfGoalScore {
	if !pfFinite(total) {
		return pfGoalScore{pfNaN, pfNaN}
	}
	if forceInteger {
		return pfAllocateIntegerGoalTotal(math.Max(0, math.Round(total)), base, handicap, true)
	}
	baseTotal := base.Home + base.Guest
	homeShare := 0.5
	if pfFinite(baseTotal) && baseTotal > 0 {
		homeShare = base.Home / baseTotal
	}
	return pfGoalScore{math.Max(0, total*homeShare), math.Max(0, total*(1-homeShare))}
}

type pfGoalBands struct{ Under, Main, Over pfGoalScore }

func pfExpectedGoalBands(response *analysisMatchResponse) pfGoalBands {
	main := pfExpectedGoalPair(response)
	goalLine := response.QiushuPankou2
	handicapLine := response.YapanPankou2
	mainTotal := main.Home + main.Guest
	underTotal := math.Max(0, math.Ceil(goalLine)-1)
	overTotal := math.Max(0, math.Floor(goalLine+2))
	if !pfFinite(goalLine) {
		underTotal, overTotal = mainTotal, mainTotal+2
	}
	return pfGoalBands{
		Under: pfAllocateExpectedGoalTotal(underTotal, main, handicapLine, true),
		Main:  main,
		Over:  pfAllocateExpectedGoalTotal(overTotal, main, handicapLine, true),
	}
}

func pfZeroGoalAdvice(response *analysisMatchResponse, expected pfGoalScore) string {
	teams := []string{}
	if pfFinite(expected.Home) && math.Round(expected.Home) == 0 {
		teams = append(teams, response.Home)
	}
	if pfFinite(expected.Guest) && math.Round(expected.Guest) == 0 {
		teams = append(teams, response.Guest)
	}
	if len(teams) == 0 {
		return ""
	}
	return "0球风险：" + strings.Join(teams, "、") + "触发低进攻/低失球/盘口压低修正；"
}

// ---------- goal signals ----------

func pfGoalSignalText(response *analysisMatchResponse) string {
	tags := response.Detail.Test23
	if len(tags) == 0 {
		tags = response.Tags
	}
	return response.QiuPrediction + " " + strings.Join(tags, " ")
}

func pfBaseGoalSignal(response *analysisMatchResponse) string {
	text := pfGoalSignalText(response)
	if strings.Contains(text, "小球") || strings.Contains(text, "闹0区") {
		return "under"
	}
	if strings.Contains(text, "大球") || strings.Contains(text, "裂球") {
		return "over"
	}
	return ""
}

func pfBaseGoalPredictionValue(response *analysisMatchResponse) float64 {
	line := response.QiushuPankou2
	signal := pfBaseGoalSignal(response)
	text := pfGoalSignalText(response)
	if signal == "under" {
		return math.Max(0, line-0.75)
	}
	if signal == "over" {
		if strings.Contains(text, "裂球") {
			return math.Max(3, line+1)
		}
		if line <= 2.25 {
			return line + 0.5
		}
		return line + 0.75
	}
	return pfNaN
}

func pfGoalBalanceSignal(history, recent, combined, openingLine float64) string {
	baseline, highThreshold, lowThreshold := 2.5, 2.85, 2.15
	balanceValue := pfWeightedAverage([]pfWeighted{
		{history, 0.2}, {recent, 0.35}, {combined, 0.3}, {openingLine, 0.15},
	})
	if !pfFinite(balanceValue) {
		return ""
	}
	values := []float64{}
	for _, value := range []float64{history, recent, combined, openingLine} {
		if pfFinite(value) {
			values = append(values, value)
		}
	}
	highCount, lowCount := 0, 0
	for _, value := range values {
		if value >= highThreshold {
			highCount++
		}
		if value <= lowThreshold {
			lowCount++
		}
	}
	if balanceValue >= highThreshold || highCount >= 2 {
		if pfFinite(openingLine) && openingLine <= baseline {
			return "underHidden"
		}
		return "under"
	}
	if balanceValue <= lowThreshold || lowCount >= 2 {
		if pfFinite(openingLine) && openingLine >= baseline {
			return "overCorrected"
		}
		return "over"
	}
	return ""
}

func pfGoalBalanceSignalForItem(response *analysisMatchResponse) string {
	historyGoals, recentGoals := pfSplitPair(response.ChangGuiQiuShu)
	history := pfNum(historyGoals)
	recent := pfNum(recentGoals)
	combined := pfCombinedGoalAverage(history, recent)
	return pfGoalBalanceSignal(history, recent, combined, response.QiushuPankou1)
}

func pfGoalBalanceDirection(signal string) string {
	if signal == "over" || signal == "overCorrected" {
		return "over"
	}
	if signal == "under" || signal == "underHidden" {
		return "under"
	}
	return ""
}

func pfGoalBalanceSignalLabel(signal string) string {
	switch signal {
	case "underHidden":
		return "小球隐藏"
	case "under":
		return "小球"
	case "overCorrected":
		return "大球修正"
	case "over":
		return "大球"
	}
	return "-"
}

func pfGoalHeatAdjustment(response *analysisMatchResponse) float64 {
	overHeat := pfNum(pfAt(response.QiuShuTouZhu, 0))
	underHeat := pfNum(pfAt(response.QiuShuTouZhu, 1))
	if !pfFinite(overHeat) || !pfFinite(underHeat) {
		return 0
	}
	if overHeat > 65 {
		return -0.35
	}
	if underHeat > 65 {
		return 0.35
	}
	diff := overHeat - underHeat
	if math.Abs(diff) < 10 {
		return 0
	}
	return pfRound(math.Max(-0.45, math.Min(0.45, diff/60)), 2)
}

func pfGoalBalanceAdjustment(response *analysisMatchResponse) float64 {
	switch pfGoalBalanceSignalForItem(response) {
	case "underHidden":
		return -0.85
	case "under":
		return -0.6
	case "overCorrected":
		return 0.85
	case "over":
		return 0.6
	}
	return 0
}

func pfNormalizedPlatformGoalTotal(expectedTotal float64, signal string) float64 {
	if !pfFinite(expectedTotal) {
		return 0
	}
	if signal == "underHidden" || signal == "under" {
		return math.Max(0, math.Floor(expectedTotal))
	}
	if signal == "overCorrected" || signal == "over" {
		return math.Max(0, math.Ceil(expectedTotal))
	}
	return math.Max(0, math.Round(expectedTotal))
}

// ---------- handicap signals ----------

func pfHandicapDirection(value float64) string {
	if !pfFinite(value) || math.Abs(value) < 0.01 {
		return "level"
	}
	if value > 0 {
		return "home"
	}
	return "guest"
}

func pfHandicapDirectionText(value float64, home, guest string) string {
	if !pfFinite(value) || math.Abs(value) < 0.01 {
		return "平手盘"
	}
	absolute := pfTrim(math.Abs(value), 2)
	if value > 0 {
		return home + "让" + absolute + "球"
	}
	return guest + "让" + absolute + "球（" + home + "受让" + absolute + "球）"
}

func pfHandicapDirectionOutcome(direction string) string {
	if direction == "home" {
		return "home"
	}
	if direction == "guest" {
		return "away"
	}
	return ""
}

func pfHandicapExpectationOutcome(response *analysisMatchResponse) string {
	historyHandicap, recentHandicap := pfSplitPair(response.ChangGuiYaPan)
	expected := pfWeightedAverage([]pfWeighted{
		{pfNum(historyHandicap), 0.45},
		{pfNum(recentHandicap), 0.55},
	})
	if pfFinite(expected) && math.Abs(expected) >= 0.25 {
		if expected > 0 {
			return "home"
		}
		return "away"
	}
	return ""
}

type pfHandicapPressure struct {
	Outcome string
	Weight  float64
}

func pfHandicapPressureSignal(response *analysisMatchResponse) pfHandicapPressure {
	historyText, recentText := pfSplitPair(response.ChangGuiYaPan)
	history := pfNum(historyText)
	recent := pfNum(recentText)
	currentLine := response.YapanPankou2
	if !pfFinite(history) || !pfFinite(recent) || !pfFinite(currentLine) {
		return pfHandicapPressure{}
	}
	expectedLine := pfWeightedAverage([]pfWeighted{{history, 0.45}, {recent, 0.55}})
	currentDirection := pfHandicapDirection(currentLine)
	expectedDirection := pfHandicapDirection(expectedLine)
	currentAbs := math.Abs(currentLine)
	expectedAbs := math.Abs(expectedLine)
	expectedOutcome := pfHandicapDirectionOutcome(expectedDirection)
	currentOpposite := ""
	if currentDirection == "home" {
		currentOpposite = "away"
	} else if currentDirection == "guest" {
		currentOpposite = "home"
	}
	if currentDirection != "level" && expectedDirection != "level" && currentDirection != expectedDirection {
		return pfHandicapPressure{expectedOutcome, 1.4}
	}
	if currentDirection != "level" && currentAbs-expectedAbs >= 0.5 {
		return pfHandicapPressure{currentOpposite, 1}
	}
	if expectedDirection != "level" && expectedAbs-currentAbs >= 0.5 {
		return pfHandicapPressure{expectedOutcome, 1.2}
	}
	if math.Min(math.Abs(history-currentLine), math.Abs(recent-currentLine)) > 0.75 {
		return pfHandicapPressure{expectedOutcome, 1.1}
	}
	return pfHandicapPressure{}
}

func pfHandicapPressureSignalLabel(response *analysisMatchResponse) string {
	historyText, recentText := pfSplitPair(response.ChangGuiYaPan)
	history := pfNum(historyText)
	recent := pfNum(recentText)
	currentLine := response.YapanPankou2
	if !pfFinite(history) || !pfFinite(recent) || !pfFinite(currentLine) {
		return ""
	}
	expectedLine := pfWeightedAverage([]pfWeighted{{history, 0.45}, {recent, 0.55}})
	currentDirection := pfHandicapDirection(currentLine)
	expectedDirection := pfHandicapDirection(expectedLine)
	currentAbs := math.Abs(currentLine)
	expectedAbs := math.Abs(expectedLine)
	if currentDirection != "level" && expectedDirection != "level" && currentDirection != expectedDirection {
		return "方向反转"
	}
	if currentDirection != "level" && currentAbs-expectedAbs >= 0.5 {
		return "盘口偏深，防夸大强势方"
	}
	if expectedDirection != "level" && expectedAbs-currentAbs >= 0.5 {
		return "盘口偏浅，防隐藏强势方"
	}
	if math.Min(math.Abs(history-currentLine), math.Abs(recent-currentLine)) > 0.75 {
		return "盘口异常偏离"
	}
	return ""
}

func pfExpectedGoalOutcome(response *analysisMatchResponse) string {
	goals := pfExpectedGoalPair(response)
	if !pfFinite(goals.Home) || !pfFinite(goals.Guest) {
		return ""
	}
	if goals.Home-goals.Guest >= 0.55 {
		return "home"
	}
	if goals.Guest-goals.Home >= 0.55 {
		return "away"
	}
	return "draw"
}

func pfHandicapHeatOutcome(response *analysisMatchResponse) string {
	homeHeat := pfNum(pfAt(response.YaPanTouZhu, 0))
	guestHeat := pfNum(pfAt(response.YaPanTouZhu, 1))
	if !pfFinite(homeHeat) || !pfFinite(guestHeat) {
		return ""
	}
	if homeHeat-guestHeat >= 10 {
		return "home"
	}
	if guestHeat-homeHeat >= 10 {
		return "away"
	}
	return ""
}

func pfHandicapHotOutcome(response *analysisMatchResponse) string {
	homeHeat := pfNum(pfAt(response.YaPanTouZhu, 0))
	guestHeat := pfNum(pfAt(response.YaPanTouZhu, 1))
	if pfFinite(homeHeat) && homeHeat > 65 {
		return "home"
	}
	if pfFinite(guestHeat) && guestHeat > 65 {
		return "away"
	}
	return ""
}

func pfHandicapGuideOutcome(response *analysisMatchResponse) string {
	openingLine := response.YapanPankou1
	currentLine := response.YapanPankou2
	delta := currentLine - openingLine
	if pfFinite(delta) && delta >= 0.25 {
		return "home"
	}
	if pfFinite(delta) && delta <= -0.25 {
		return "away"
	}
	if pfFinite(currentLine) {
		if currentLine >= 0.25 {
			return "home"
		}
		if currentLine <= -0.25 {
			return "away"
		}
	}
	return ""
}

// ---------- bookmaker & platform outcome scores ----------

func pfLowestOddsOutcome(response *analysisMatchResponse) string {
	odds := pfBookmakerOddsDistribution(response)
	if odds == nil {
		return ""
	}
	type row struct {
		outcome string
		value   float64
	}
	rows := []row{}
	for _, candidate := range []row{{"home", odds.Home}, {"draw", odds.Draw}, {"away", odds.Away}} {
		if pfFinite(candidate.value) && candidate.value > 0 {
			rows = append(rows, candidate)
		}
	}
	if len(rows) == 0 {
		return ""
	}
	best := rows[0]
	for _, candidate := range rows[1:] {
		if candidate.value < best.value {
			best = candidate
		}
	}
	return best.outcome
}

func pfBookmakerDrawScore(response *analysisMatchResponse) float64 {
	score := 0.0
	openingHandicap := response.YapanPankou1
	currentHandicap := response.YapanPankou2
	if pfFinite(currentHandicap) {
		if math.Abs(currentHandicap) <= 0.25 {
			score += 2
		}
		if pfFinite(openingHandicap) && math.Abs(currentHandicap) < math.Abs(openingHandicap) {
			score += 1
		}
	}
	odds := pfBookmakerOddsDistribution(response)
	if odds != nil {
		homeImplied := 1 / odds.Home
		drawImplied := 1 / odds.Draw
		awayImplied := 1 / odds.Away
		totalImplied := homeImplied + drawImplied + awayImplied
		drawProbability, sideProbability := 0.0, 0.0
		if totalImplied > 0 {
			drawProbability = drawImplied / totalImplied * 100
			sideProbability = math.Max(homeImplied, awayImplied) / totalImplied * 100
		}
		if drawProbability >= sideProbability-6 {
			score += 2
		} else if drawProbability >= sideProbability-10 {
			score += 1
		}
		if odds.Draw <= math.Min(odds.Home, odds.Away)*1.35 {
			score += 1
		}
	}
	openingGoalLine := response.QiushuPankou1
	currentGoalLine := response.QiushuPankou2
	if pfFinite(currentGoalLine) {
		if currentGoalLine <= 2.25 {
			score += 1
		}
		if pfFinite(openingGoalLine) && currentGoalLine < openingGoalLine {
			score += 1
		}
	}
	return score
}

func pfBookmakerCorrectionScores(response *analysisMatchResponse) map[string]float64 {
	scores := map[string]float64{"home": 0, "draw": 0, "away": 0}
	add := func(outcome string, weight float64) {
		if outcome != "" {
			scores[outcome] += weight
		}
	}
	oddsProbabilities := pfBookmakerOddsProbabilities(response)
	handicapOutcome := pfHandicapGuideOutcome(response)
	oddsOutcome := pfLowestOddsOutcome(response)
	drawScore := pfBookmakerDrawScore(response)
	currentHandicap := response.YapanPankou2
	strongHandicap := pfFinite(currentHandicap) && math.Abs(currentHandicap) >= 0.75

	if oddsProbabilities != nil {
		scores["home"] += oddsProbabilities.Home * 7
		scores["draw"] += oddsProbabilities.Draw * 7
		scores["away"] += oddsProbabilities.Away * 7
	}
	if handicapOutcome != "" && oddsOutcome != "" && handicapOutcome == oddsOutcome {
		add(handicapOutcome, 1.6)
	}
	if strongHandicap {
		add(handicapOutcome, 1.6)
	} else {
		add(handicapOutcome, 0.9)
	}
	if oddsProbabilities == nil {
		if oddsOutcome == "draw" {
			add(oddsOutcome, 2.2)
		} else {
			add(oddsOutcome, 1.8)
		}
	}
	if drawScore >= 4 {
		add("draw", 1.8)
	} else if drawScore >= 3 && !strongHandicap {
		add("draw", 1.2)
	} else if drawScore >= 2 && handicapOutcome == "" {
		add("draw", 0.8)
	}
	return scores
}

func pfBookmakerResultOutcome(response *analysisMatchResponse) string {
	return pfOutcomeFromScores(pfBookmakerCorrectionScores(response))
}

func pfBasePredictionOutcome(response *analysisMatchResponse) string {
	return pfTextOutcome(response.Prediction)
}

func pfPlatformTagOutcomes(response *analysisMatchResponse) []string {
	tags := response.Detail.Test23
	if len(tags) == 0 {
		tags = response.Tags
	}
	outcomes := []string{}
	for _, tag := range tags {
		text := tag
		if strings.Contains(text, "客胜") {
			outcomes = append(outcomes, "away")
		} else if strings.Contains(text, "胜平局") {
			if response.Prediction == "平局" {
				outcomes = append(outcomes, "draw")
			} else {
				outcomes = append(outcomes, "home")
			}
		} else if strings.Contains(text, "闹0区") {
			outcomes = append(outcomes, "draw")
		}
	}
	return outcomes
}

func pfProfessionalConsensusOutcome(response *analysisMatchResponse) string {
	kellyOutcomes := pfTextOutcomes(response.KaiLiResult)
	ticaiOutcomes := pfTextOutcomes(response.TiCaiResult)
	for _, outcome := range kellyOutcomes {
		for _, ticai := range ticaiOutcomes {
			if outcome == ticai {
				return outcome
			}
		}
	}
	return ""
}

func pfProfessionalConflictWarning(response *analysisMatchResponse) *platformWarningRow {
	base := pfBasePredictionOutcome(response)
	consensus := pfProfessionalConsensusOutcome(response)
	if base == "" || consensus == "" || consensus == base {
		return nil
	}
	// 回测(99场)：凯体共识方向仅约21%命中、ROI 67%——该信号应反向解读。
	return &platformWarningRow{
		Value: "凯体反差：凯利/体彩共同指向" + pfOutcomeLabelByKey(consensus, response.Home, response.Guest) + "，但该共识历史仅约21%命中——建议坚持主推" + pfOutcomeLabelByKey(base, response.Home, response.Guest),
		Tone:  "blue",
	}
}

func pfDrawRiskSignal(response *analysisMatchResponse) platformDrawRisk {
	score := 0.0
	reasons := []string{}
	add := func(condition bool, weight float64, reason string) {
		if !condition {
			return
		}
		score += weight
		reasons = append(reasons, reason)
	}
	openingHandicap := response.YapanPankou1
	currentHandicap := response.YapanPankou2
	absHandicap := math.Abs(currentHandicap)
	add(pfFinite(currentHandicap) && absHandicap <= 0.25, 2, "亚盘浅")
	add(pfFinite(currentHandicap) && absHandicap > 0.25 && absHandicap <= 0.5, 1, "受让/让球浅盘")
	add(pfFinite(openingHandicap) && pfFinite(currentHandicap) && math.Abs(openingHandicap)-math.Abs(currentHandicap) >= 0.25, 1.5, "盘口变浅")

	odds := pfBookmakerOddsDistribution(response)
	if odds != nil {
		homeImplied := 1 / odds.Home
		drawImplied := 1 / odds.Draw
		awayImplied := 1 / odds.Away
		totalImplied := homeImplied + drawImplied + awayImplied
		drawProbability, sideProbability := 0.0, 0.0
		if totalImplied > 0 {
			drawProbability = drawImplied / totalImplied * 100
			sideProbability = math.Max(homeImplied, awayImplied) / totalImplied * 100
		}
		add(odds.Draw <= 3.45 && drawProbability >= sideProbability-10, 1.5, "平赔不高")
		add(odds.Draw <= math.Min(odds.Home, odds.Away)*1.35, 1, "平赔差距小")
	} else if pfBookmakerDrawScore(response) >= 3 {
		add(true, 1, "欧赔平局支撑")
	}

	goalLine := response.QiushuPankou2
	add(pfFinite(goalLine) && goalLine <= 2.5, 1, "大小球低盘")

	bookmaker := pfBookmakerResultOutcome(response)
	base := pfBasePredictionOutcome(response)
	add(bookmaker != "" && base != "" && bookmaker != base && bookmaker != "draw" && base != "draw", 1, "庄平胜负分歧")
	add(pfProfessionalConflictWarning(response) != nil, 1, "凯利/体彩反差")

	goals := pfExpectedGoalPair(response)
	expectedTotal := goals.Home + goals.Guest
	add(pfFinite(expectedTotal) && expectedTotal > 0 && expectedTotal <= 2.5 && goals.Home <= 1.5 && goals.Guest <= 1.5, 1, "近期进丢球一般")

	historyOutcome := pfHistoryMatchOutcome(response)
	add(historyOutcome == "draw", 1.5, "历史有平局")
	add(pfHistorySmallScore(response), 1, "历史小比分")

	unique := []string{}
	seen := map[string]bool{}
	for _, reason := range reasons {
		if !seen[reason] {
			seen[reason] = true
			unique = append(unique, reason)
		}
	}
	return platformDrawRisk{Score: score, Reasons: unique}
}

func pfPlatformOverheatOutcome(response *analysisMatchResponse) string {
	base := pfBasePredictionOutcome(response)
	kellyOutcome := pfPrimaryTextOutcome(response.KaiLiResult)
	ticaiOutcome := pfPrimaryTextOutcome(response.TiCaiResult)
	sportteryOutcome := pfMarketComfortOutcome(response, "sporttery")
	rqOutcome := pfMarketComfortOutcome(response, "sportteryRqspf")
	for _, outcome := range []string{"home", "draw", "away"} {
		count := 0
		for _, signal := range []string{base, kellyOutcome, ticaiOutcome, sportteryOutcome, rqOutcome} {
			if signal == outcome {
				count++
			}
		}
		if count >= 4 {
			return outcome
		}
	}
	return ""
}

func pfPlatformLiveCorrectionScores(response *analysisMatchResponse) map[string]float64 {
	scores := map[string]float64{"home": 0, "draw": 0, "away": 0}
	add := func(outcome string, weight float64) {
		if outcome != "" {
			scores[outcome] += weight
		}
	}
	kellyOutcome := pfPrimaryTextOutcome(response.KaiLiResult)
	ticaiOutcome := pfPrimaryTextOutcome(response.TiCaiResult)
	professionalConsensus := pfProfessionalConsensusOutcome(response)
	base := pfBasePredictionOutcome(response)

	add(pfMarketComfortOutcome(response, "sporttery"), 3)
	add(pfMarketComfortOutcome(response, "sportteryRqspf"), 2.5)
	add(pfLocalComfortOutcome(response), 1.1)
	add(pfHandicapExpectationOutcome(response), 1.8)
	handicapAlert := pfHandicapPressureSignal(response)
	add(handicapAlert.Outcome, handicapAlert.Weight*1.6)
	add(pfHandicapHeatOutcome(response), 0.8)
	add(pfExpectedGoalOutcome(response), 1.2)
	add(kellyOutcome, 2.2)
	add(ticaiOutcome, 2.2)
	if professionalConsensus != "" && professionalConsensus != base {
		add(professionalConsensus, 2.2)
	} else {
		add(professionalConsensus, 1.2)
	}
	for _, outcome := range pfPlatformTagOutcomes(response) {
		add(outcome, 0.8)
	}
	add(pfHistoryMatchOutcome(response), 1.4)
	add(pfRecentTeamOutcome(response.HomeZuijinBisai, response.Home, "home"), 1)
	add(pfRecentTeamOutcome(response.GuestZuijinBisai, response.Guest, "away"), 1)

	if pfBookmakerDrawScore(response) >= 3 {
		add("draw", 1.4)
	}
	drawRisk := pfDrawRiskSignal(response)
	if drawRisk.Score >= 5 {
		add("draw", 3.2)
	} else if drawRisk.Score >= 4 {
		add("draw", 2)
	} else if drawRisk.Score >= 3 {
		add("draw", 1)
	}
	pfApplyPlatformRiskScores(response, scores)
	return scores
}

func pfApplyPlatformRiskScores(response *analysisMatchResponse, scores map[string]float64) {
	add := func(outcome string, weight float64) {
		if outcome != "" {
			scores[outcome] += weight
		}
	}
	overheat := pfPlatformOverheatOutcome(response)
	if overheat != "" {
		scores[overheat] -= 1
		add("draw", 0.6)
		add(pfOppositeOutcome(overheat), 0.5)
	}
	handicapHot := pfHandicapHotOutcome(response)
	if handicapHot != "" {
		scores[handicapHot] -= 0.8
		add(pfOppositeOutcome(handicapHot), 0.6)
	}
	sportteryLoss := pfStrongMarketLossRow(response, "sporttery")
	rqLoss := pfStrongMarketLossRow(response, "sportteryRqspf")
	if sportteryLoss != nil && rqLoss != nil && sportteryLoss.Outcome == rqLoss.Outcome {
		scores[sportteryLoss.Outcome] -= 1.2
		if sportteryLoss.Outcome != "draw" {
			add("draw", 0.4)
		}
		add(pfOppositeOutcome(sportteryLoss.Outcome), 0.7)
	}
	professionalWarning := pfProfessionalConflictWarning(response)
	professionalConsensus := pfProfessionalConsensusOutcome(response)
	if professionalWarning != nil && professionalConsensus != "" {
		if base := pfBasePredictionOutcome(response); base != "" {
			scores[base] -= 0.9
		}
		add(professionalConsensus, 1.1)
	}
	if handicapMovement := pfHandicapGuideOutcome(response); handicapMovement != "" {
		add(handicapMovement, 0.6)
	}
}

func pfPlatformLiveOutcome(response *analysisMatchResponse) string {
	base := pfBasePredictionOutcome(response)
	correctionScores := pfPlatformLiveCorrectionScores(response)
	correction := pfOutcomeFromScores(correctionScores)
	if base == "" {
		return correction
	}
	correctionScore := correctionScores[correction]
	baseScore := correctionScores[base]
	hasHandicapAlert := pfHandicapPressureSignal(response).Outcome != ""
	drawRisk := pfDrawRiskSignal(response)
	drawCanCoverSides := correctionScores["draw"] >= math.Max(correctionScores["home"], correctionScores["away"])-0.8
	if drawRisk.Score >= 5 && drawCanCoverSides {
		return "draw"
	}
	reverseThreshold, gapThreshold := 5.0, 2.8
	if hasHandicapAlert {
		reverseThreshold, gapThreshold = 4.2, 1.6
	}
	if correction != base && correctionScore >= reverseThreshold && correctionScore-baseScore >= gapThreshold {
		return correction
	}
	return base
}

// ---------- goal predictions + fused scores ----------

func pfBookmakerGoalResult(response *analysisMatchResponse) platformGoalResult {
	openingLine := response.QiushuPankou1
	currentLine := response.QiushuPankou2
	line := currentLine
	if !pfFinite(line) {
		line = openingLine
	}
	delta := currentLine - openingLine
	if pfFinite(delta) && delta >= 0.25 {
		total := 3
		if pfFinite(line) {
			total = int(math.Max(3, math.Floor(line+1.5)))
		}
		return platformGoalResult{fmt.Sprintf("%d球以上", total), total, "green"}
	}
	if pfFinite(delta) && delta <= -0.25 {
		total := 2
		if pfFinite(line) {
			total = int(math.Max(0, math.Ceil(line)-1))
		}
		return platformGoalResult{fmt.Sprintf("%d球以内", total), total, "red"}
	}
	if pfFinite(line) && line >= 2.75 {
		return platformGoalResult{"3球左右", 3, "green"}
	}
	if pfFinite(line) && line <= 2.25 {
		return platformGoalResult{"2球以内", 2, "red"}
	}
	return platformGoalResult{"2-3球", 2, "normal"}
}

func pfNormalizeScoreByOutcome(homeGoals, guestGoals float64, outcome string, total float64) string {
	home := math.Max(0, homeGoals)
	guest := math.Max(0, guestGoals)
	normalizedTotal := math.Max(0, math.Round(total))
	if outcome == "draw" {
		side := math.Max(0, math.Floor(normalizedTotal/2))
		return fmt.Sprintf("%d:%d", int(side), int(side))
	}
	winner := "guest"
	if outcome == "home" {
		winner = "home"
	}
	currentWinnerGoals, currentLoserGoals := guest, home
	if winner == "home" {
		currentWinnerGoals, currentLoserGoals = home, guest
	}
	if currentWinnerGoals > currentLoserGoals {
		return fmt.Sprintf("%d:%d", int(home), int(guest))
	}
	winnerGoals := math.Max(1, math.Ceil((normalizedTotal+1)/2))
	loserGoals := math.Max(0, normalizedTotal-winnerGoals)
	if winner == "home" {
		return fmt.Sprintf("%d:%d", int(winnerGoals), int(loserGoals))
	}
	return fmt.Sprintf("%d:%d", int(loserGoals), int(winnerGoals))
}

func pfBookmakerFusedScore(response *analysisMatchResponse, outcome string, total int) string {
	normalizedTotal := int(math.Max(0, math.Round(float64(total))))
	if normalizedTotal <= 0 {
		return "0:0"
	}
	handicap := response.YapanPankou2
	openingHandicap := response.YapanPankou1
	handicapLine := handicap
	if !pfFinite(handicapLine) {
		handicapLine = openingHandicap
	}
	favorite := outcome
	if pfFinite(handicapLine) {
		if handicapLine > 0 {
			favorite = "home"
		} else if handicapLine < 0 {
			favorite = "away"
		}
	}
	favoriteGoals := math.Max(1, math.Ceil(float64(normalizedTotal+1)/2))
	underdogGoals := math.Max(0, float64(normalizedTotal)-favoriteGoals)
	home, guest := underdogGoals, favoriteGoals
	if favorite == "home" {
		home, guest = favoriteGoals, underdogGoals
	}
	return pfNormalizeScoreByOutcome(home, guest, outcome, float64(normalizedTotal))
}

func pfPlatformFusedScore(response *analysisMatchResponse, outcome string, total int) string {
	normalizedTotal := int(math.Max(0, math.Round(float64(total))))
	if normalizedTotal <= 0 {
		return "0:0"
	}
	goals := pfAllocateIntegerGoalTotal(float64(normalizedTotal), pfExpectedGoalPair(response), 0, false)
	return pfNormalizeScoreByOutcome(math.Round(goals.Home), math.Round(goals.Guest), outcome, float64(normalizedTotal))
}

func pfSecondaryGoalBand(response *analysisMatchResponse, goal platformGoalResult) string {
	if strings.Contains(goal.Label, "以内") {
		return "over"
	}
	if strings.Contains(goal.Label, "以上") {
		return "under"
	}
	line := response.QiushuPankou2
	if pfFinite(line) {
		if float64(goal.Total) <= line {
			return "over"
		}
		return "under"
	}
	return "under"
}

func pfSecondaryGuideScore(response *analysisMatchResponse, outcome string, goal platformGoalResult, source string) string {
	bands := pfExpectedGoalBands(response)
	band := pfSecondaryGoalBand(response, goal)
	goals := bands.Under
	if band == "over" {
		goals = bands.Over
	}
	total := math.Max(0, math.Round(goals.Home+goals.Guest))
	if !pfFinite(goals.Home) || !pfFinite(goals.Guest) || !pfFinite(total) {
		return "-"
	}
	if source == "bookmaker" {
		return pfBookmakerFusedScore(response, outcome, int(total))
	}
	return pfNormalizeScoreByOutcome(math.Round(goals.Home), math.Round(goals.Guest), outcome, total)
}

func pfPlatformLiveGoalResult(response *analysisMatchResponse) platformGoalResult {
	bands := pfExpectedGoalBands(response)
	mainTotal := bands.Main.Home + bands.Main.Guest
	openingLine := response.QiushuPankou1
	currentLine := response.QiushuPankou2
	line := currentLine
	if !pfFinite(line) {
		line = openingLine
	}
	historyGoals, recentGoals := pfSplitPair(response.ChangGuiQiuShu)
	mainForAverage := mainTotal
	if !pfFinite(mainForAverage) {
		mainForAverage = pfNaN
	}
	correctionTotal := pfWeightedAverage([]pfWeighted{
		{mainForAverage, 0.35},
		{pfRecentGoalSampleValue(response, recentGoals), 0.25},
		{pfHistoryGoalSampleValue(response, historyGoals), 0.15},
		{line, 0.25},
	}) + pfGoalHeatAdjustment(response) + pfGoalBalanceAdjustment(response)
	baseTotal := pfBaseGoalPredictionValue(response)
	expectedTotal := correctionTotal
	if pfFinite(baseTotal) {
		expectedTotal = pfWeightedAverage([]pfWeighted{{baseTotal, 0.3}, {correctionTotal, 0.7}})
	}
	balanceSignal := pfGoalBalanceSignalForItem(response)
	total := 0
	if pfFinite(expectedTotal) {
		total = int(pfNormalizedPlatformGoalTotal(expectedTotal, balanceSignal))
	} else {
		total = pfBookmakerGoalResult(response).Total
	}
	signal := pfBaseGoalSignal(response)
	if signal == "over" && pfFinite(line) && float64(total) >= math.Ceil(line) {
		return platformGoalResult{fmt.Sprintf("%d球以上", total), total, "green"}
	}
	if signal == "under" && pfFinite(line) && float64(total) <= math.Floor(line) {
		return platformGoalResult{fmt.Sprintf("%d球以内", total), total, "red"}
	}
	if pfFinite(line) && float64(total) > line {
		return platformGoalResult{fmt.Sprintf("%d球以上", total), total, "green"}
	}
	if pfFinite(line) && float64(total) < line {
		return platformGoalResult{fmt.Sprintf("%d球以内", total), total, "red"}
	}
	return platformGoalResult{fmt.Sprintf("%d球左右", total), total, "normal"}
}

// ---------- warnings ----------

// 大小球热度不再进"过热"告警：回测显示反过热方向仅44.6%命中，
// 大小球热度应顺势解读（见 pfPlatformIntegratedWarningRows）。
func pfHeatWarningSummary(response *analysisMatchResponse) string {
	warnings := []string{}
	addHeatWarning := func(category, name string, value interface{}) {
		if pfNum(value) > 65 {
			warnings = append(warnings, category+name+pfPercentText(value))
		}
	}
	addHeatWarning("让球", response.Home, pfAt(response.YaPanTouZhu, 0))
	addHeatWarning("让球", response.Guest, pfAt(response.YaPanTouZhu, 1))
	if len(warnings) == 0 {
		return ""
	}
	return "投注热度过热：" + strings.Join(warnings, "，")
}

func pfProfitAlignmentWarningRows(response *analysisMatchResponse) []platformWarningRow {
	local := pfStrongLocalComfortRow(response)
	sporttery := pfStrongMarketComfortRow(response, "sporttery")
	rq := pfStrongMarketComfortRow(response, "sportteryRqspf")
	sportteryLoss := pfStrongMarketLossRow(response, "sporttery")
	rqLoss := pfStrongMarketLossRow(response, "sportteryRqspf")
	simulatedTag := pfSimulatedMarketsTag(response)
	warnings := []platformWarningRow{}

	if sporttery != nil && rq != nil && sporttery.Outcome == rq.Outcome {
		warnings = append(warnings, platformWarningRow{
			Value: "警示：交易盈亏同向：胜平负" + pfOutcomeShortLabel(sporttery.Outcome) + pfSignedMoneyText(sporttery.BookmakerProfit, sporttery.Available) + "、让球" + pfOutcomeShortLabel(rq.Outcome) + pfSignedMoneyText(rq.BookmakerProfit, rq.Available) + "，均为庄家舒服项" + simulatedTag,
			Tone:  "red",
		})
	}
	if sportteryLoss != nil && rqLoss != nil && sportteryLoss.Outcome == "away" && rqLoss.Outcome == "away" {
		warnings = append(warnings, platformWarningRow{Value: "警示：庄家同向亏损：胜平负负、让球负均为最大亏损项" + simulatedTag, Tone: "green"})
	}
	if local != nil {
		aligned := []string{}
		if sporttery != nil && sporttery.Outcome == local.Outcome {
			aligned = append(aligned, "竞彩")
		}
		if rq != nil && rq.Outcome == local.Outcome {
			aligned = append(aligned, "让球")
		}
		if len(aligned) > 0 {
			warnings = append(warnings, platformWarningRow{
				Value: "警示：庄家舒服项同向：本地测算、" + strings.Join(aligned, "、") + "均指向" + pfOutcomeShortLabel(local.Outcome) + "（庄家盈利方向）" + simulatedTag,
				Tone:  "red",
			})
		}
	}
	return warnings
}

// pfSimulatedMarketsTag 当竞彩/让球盈亏来自本地模拟时，为警示追加标注。
func pfSimulatedMarketsTag(response *analysisMatchResponse) string {
	simulated := []string{}
	if market := pfMarketByKey(response, "sporttery"); market != nil && market.Simulated {
		simulated = append(simulated, "胜平负")
	}
	if market := pfMarketByKey(response, "sportteryRqspf"); market != nil && market.Simulated {
		simulated = append(simulated, "让球")
	}
	if len(simulated) == 0 {
		return ""
	}
	return "（" + strings.Join(simulated, "、") + "为模拟盘）"
}

func pfPlatformIntegratedWarningRows(response *analysisMatchResponse) []platformWarningRow {
	rows := []platformWarningRow{}
	handicapLabel := pfHandicapPressureSignalLabel(response)
	goalSignal := pfGoalBalanceSignalForItem(response)
	overheat := pfPlatformOverheatOutcome(response)
	hotOutcome := pfHandicapHotOutcome(response)
	sportteryLoss := pfStrongMarketLossRow(response, "sporttery")
	rqLoss := pfStrongMarketLossRow(response, "sportteryRqspf")
	professionalWarning := pfProfessionalConflictWarning(response)
	drawRisk := pfDrawRiskSignal(response)

	if handicapLabel != "" {
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入让球修正：" + handicapLabel, Tone: "blue"})
	}
	if hotOutcome != "" {
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入让球热度修正：" + pfOutcomeShortLabel(hotOutcome) + "过热", Tone: "blue"})
	}
	if overheat != "" {
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入过热修正：" + pfOutcomeShortLabel(overheat) + "信号过热", Tone: "blue"})
	}
	if sportteryLoss != nil && rqLoss != nil && sportteryLoss.Outcome == rqLoss.Outcome {
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入庄家同向亏损修正：" + pfOutcomeShortLabel(sportteryLoss.Outcome), Tone: "green"})
	}
	if professionalWarning != nil {
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入凯利/体彩反差修正", Tone: "blue"})
	}
	if drawRisk.Score >= 4 {
		tone := "blue"
		if drawRisk.Score >= 5 {
			tone = "red"
		}
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入平局风险：" + strings.Join(drawRisk.Reasons, "，"), Tone: tone})
	}
	switch goalSignal {
	case "underHidden":
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入大小球修正：回归小球 + 盘口隐藏", Tone: "red"})
	case "under":
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入大小球修正：回归小球", Tone: "red"})
	case "overCorrected":
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入大小球修正：回归大球 + 盘口修正", Tone: "green"})
	case "over":
		rows = append(rows, platformWarningRow{Value: "警示：平台已纳入大小球修正：回归大球", Tone: "green"})
	}
	// 回测(166场)：反大小球热度方向仅44.6%命中——热度应顺势读，不再当"过热"警示。
	overGoalHeat := pfNum(pfAt(response.QiuShuTouZhu, 0))
	underGoalHeat := pfNum(pfAt(response.QiuShuTouZhu, 1))
	if overGoalHeat > 65 {
		rows = append(rows, platformWarningRow{Value: "提示：大小球热度偏大" + pfPercentText(pfAt(response.QiuShuTouZhu, 0)) + "，历史顺热方向约55%命中，可作大球顺势参考", Tone: "green"})
	} else if underGoalHeat > 65 {
		rows = append(rows, platformWarningRow{Value: "提示：大小球热度偏小" + pfPercentText(pfAt(response.QiuShuTouZhu, 1)) + "，历史顺热方向约55%命中，可作小球顺势参考", Tone: "red"})
	}
	return rows
}

func pfWarningRowsFromText(value, tone string) []platformWarningRow {
	if value == "" {
		return nil
	}
	return []platformWarningRow{{Value: "警示：" + value, Tone: tone}}
}

func pfGuideWarningRows(response *analysisMatchResponse, platform platformGuidePrediction) []platformWarningRow {
	professionalWarning := pfProfessionalConflictWarning(response)
	professionalText := ""
	if professionalWarning != nil {
		professionalText = professionalWarning.Value
	}
	warnings := []platformWarningRow{}
	warnings = append(warnings, pfWarningRowsFromText(professionalText, "red")...)
	warnings = append(warnings, pfWarningRowsFromText(pfHeatWarningSummary(response), "red")...)
	warnings = append(warnings, pfProfitAlignmentWarningRows(response)...)
	warnings = append(warnings, pfPlatformIntegratedWarningRows(response)...)
	if platform.Warning != "" {
		warnings = append(warnings, pfWarningRowsFromText("平台"+platform.Warning, "red")...)
	}
	seen := map[string]bool{}
	unique := []platformWarningRow{}
	for _, warning := range warnings {
		if seen[warning.Value] {
			continue
		}
		seen[warning.Value] = true
		unique = append(unique, warning)
	}
	return unique
}

func pfWarningAdjustedGoalResult(response *analysisMatchResponse) platformGoalResult {
	line := response.QiushuPankou2
	total := 2
	if pfFinite(line) {
		total = int(math.Max(1, math.Ceil(line)-1))
	}
	return platformGoalResult{fmt.Sprintf("%d球以内", total), total, "red"}
}

func pfWarningAdjustedScore(outcome string, total int) string {
	normalizedTotal := int(math.Max(1, math.Round(float64(total))))
	if outcome == "draw" {
		if normalizedTotal <= 1 {
			return "0:0"
		}
		return "1:1"
	}
	if normalizedTotal <= 2 {
		if outcome == "home" {
			return "1:0"
		}
		return "0:1"
	}
	if outcome == "home" {
		return "2:1"
	}
	return "1:2"
}

func pfWarningAdjustedPrediction(response *analysisMatchResponse) *platformGuidePrediction {
	professionalConsensus := pfProfessionalConsensusOutcome(response)
	base := pfBasePredictionOutcome(response)
	if professionalConsensus == "" || base == "" || professionalConsensus == base {
		return nil
	}
	goal := pfWarningAdjustedGoalResult(response)
	return &platformGuidePrediction{
		Outcome:        professionalConsensus,
		Goal:           goal,
		Score:          pfWarningAdjustedScore(professionalConsensus, goal.Total),
		SecondaryScore: pfWarningAdjustedScore(professionalConsensus, int(math.Max(1, float64(goal.Total+1)))),
	}
}

// ---------- alert row tables (复杂版 让球压力 / 大小球) ----------

func pfHandicapPressureAlertRows(response *analysisMatchResponse) []platformStatRow {
	historyText, recentText := pfSplitPair(response.ChangGuiYaPan)
	history := pfNum(historyText)
	recent := pfNum(recentText)
	currentLine := response.YapanPankou2
	home, guest := response.Home, response.Guest
	if !pfFinite(history) || !pfFinite(recent) || !pfFinite(currentLine) {
		return []platformStatRow{{Label: "注意盘口", Value: "期望让球或即时盘暂缺，先按主客方向观察。", Tone: "normal"}}
	}
	expectedLine := pfWeightedAverage([]pfWeighted{{history, 0.45}, {recent, 0.55}})
	currentDirection := pfHandicapDirection(currentLine)
	expectedDirection := pfHandicapDirection(expectedLine)
	currentAbs := math.Abs(currentLine)
	expectedAbs := math.Abs(expectedLine)
	rows := []platformStatRow{{Label: "盘口方向", Value: pfHandicapDirectionText(currentLine, home, guest), Tone: "normal"}}

	if currentDirection != "level" && expectedDirection != "level" && currentDirection != expectedDirection {
		expectedTeam := guest
		if expectedDirection == "home" {
			expectedTeam = home
		}
		rows = append(rows, platformStatRow{Label: "方向反转提醒", Value: "期望更偏" + expectedTeam + "，但即时盘是" + pfHandicapDirectionText(currentLine, home, guest) + "，需要防盘口方向反做。", Tone: "red"})
	} else if currentDirection != "level" && currentAbs-expectedAbs >= 0.5 {
		rows = append(rows, platformStatRow{Label: "重点提醒", Value: pfHandicapDirectionText(currentLine, home, guest) + "，比历史/近期期望" + pfTrim(expectedAbs, 2) + "更深，盘口过于明显，可能夸大强势方。", Tone: "blue"})
	} else if expectedDirection != "level" && expectedAbs-currentAbs >= 0.5 {
		rows = append(rows, platformStatRow{Label: "重点提醒", Value: "历史/近期期望约" + pfTrim(expectedAbs, 2) + "球，但即时盘只有" + pfHandicapDirectionText(currentLine, home, guest) + "，盘口偏浅，可能故意隐藏强势方。", Tone: "blue"})
	} else {
		rows = append(rows, platformStatRow{Label: "注意盘口", Value: "期望让球与即时盘没有明显偏离，继续结合盈亏和临场升降观察。", Tone: "normal"})
	}
	if math.Min(math.Abs(history-currentLine), math.Abs(recent-currentLine)) > 0.75 {
		rows = append(rows, platformStatRow{Label: "异常提示", Value: "期望让球与即时盘偏离超过0.75，注意盘口异常。", Tone: "green"})
	}
	return rows
}

func pfGoalBalanceAlertRows(response *analysisMatchResponse) []platformStatRow {
	baseline, highThreshold, lowThreshold := 2.5, 2.85, 2.15
	historyText, recentText := pfSplitPair(response.ChangGuiQiuShu)
	history := pfNum(historyText)
	recent := pfNum(recentText)
	combined := pfCombinedGoalAverage(pfHistoryGoalSampleValue(response, historyText), pfRecentGoalSampleValue(response, recentText))
	openingLine := response.QiushuPankou1
	balanceValue := pfWeightedAverage([]pfWeighted{
		{history, 0.2}, {recent, 0.35}, {combined, 0.3}, {openingLine, 0.15},
	})
	if !pfFinite(balanceValue) {
		return []platformStatRow{{Label: "2.5均衡警示", Value: "历史、近期、综合均值或初盘不足，暂按盘口变化观察大小球。", Tone: "normal"}}
	}
	values := []float64{}
	for _, value := range []float64{history, recent, combined, openingLine} {
		if pfFinite(value) {
			values = append(values, value)
		}
	}
	highCount, lowCount := 0, 0
	for _, value := range values {
		if value >= highThreshold {
			highCount++
		}
		if value <= lowThreshold {
			lowCount++
		}
	}
	rows := []platformStatRow{{Label: "2.5均衡值", Value: "测算" + pfTrim(balanceValue, 2) + "，中轴" + pfTrim(baseline, 1), Tone: "normal"}}
	missing := []string{}
	if !pfFinite(history) {
		missing = append(missing, "历史样本")
	}
	if !pfFinite(recent) {
		missing = append(missing, "近期样本")
	}
	if len(missing) > 0 {
		rows = append(rows, platformStatRow{Label: "样本修正", Value: strings.Join(missing, "、") + "不足，已从均衡计算中剔除，不按0球处理。", Tone: "normal"})
	}
	if balanceValue >= highThreshold || highCount >= 2 {
		rows = append(rows, platformStatRow{Label: "回归小球警示", Value: "历史/近期/综合/初盘整体高于" + pfTrim(baseline, 1) + "，连续大球不可持续，优先防回落到小球。", Tone: "red"})
		if pfFinite(openingLine) && openingLine <= baseline {
			rows = append(rows, platformStatRow{Label: "盘口隐藏提醒", Value: "均值偏大但初盘未抬高到" + pfTrim(baseline, 1) + "以上，说明盘口没有追大，回归小球信号更强。", Tone: "blue"})
		}
		return rows
	}
	if balanceValue <= lowThreshold || lowCount >= 2 {
		rows = append(rows, platformStatRow{Label: "回归大球警示", Value: "历史/近期/综合/初盘整体低于" + pfTrim(baseline, 1) + "，连续小球不可持续，优先防反弹到大球。", Tone: "green"})
		if pfFinite(openingLine) && openingLine >= baseline {
			rows = append(rows, platformStatRow{Label: "盘口修正提醒", Value: "均值偏小但初盘仍在" + pfTrim(baseline, 1) + "附近或以上，盘口可能已提前修正，防大球反弹。", Tone: "blue"})
		}
		return rows
	}
	rows = append(rows, platformStatRow{Label: "均衡判断", Value: "当前没有明显超出阈值，大小球先按" + pfTrim(baseline, 1) + "上下平衡观察。", Tone: "normal"})
	return rows
}

// ---------- 邪修 (evil cult) ----------

func pfEvilCultRawGoalLine(response *analysisMatchResponse) float64 {
	current := response.QiushuPankou2
	opening := response.QiushuPankou1
	if pfFinite(current) && current > 0 {
		return current
	}
	if pfFinite(opening) && opening > 0 {
		return opening
	}
	return 2.5
}

func pfNormalizeGoalHalfLine(value float64) float64 {
	if !pfFinite(value) || value < 0 {
		return 2.5
	}
	return math.Max(0.5, math.Floor(value)+0.5)
}

func pfEvilCultGoalLine(response *analysisMatchResponse) float64 {
	return pfNormalizeGoalHalfLine(pfEvilCultRawGoalLine(response))
}

func pfEvilCultGoalProjection(response *analysisMatchResponse) pfGoalScore {
	homeAttack := pfNum(pfAt(response.LiangDuiQiuShu, 0))
	guestAttack := pfNum(pfAt(response.LiangDuiQiuShu, 1))
	homeConcede := pfNum(pfAt(response.LiangDuiQiuShu, 2))
	guestConcede := pfNum(pfAt(response.LiangDuiQiuShu, 3))
	home := pfWeightedAverage([]pfWeighted{{homeAttack, 0.55}, {guestConcede, 0.45}})
	guest := pfWeightedAverage([]pfWeighted{{guestAttack, 0.55}, {homeConcede, 0.45}})
	if pfFinite(home) || pfFinite(guest) {
		safeHome, safeGuest := home, guest
		if !pfFinite(safeHome) {
			safeHome = 0
		}
		if !pfFinite(safeGuest) {
			safeGuest = 0
		}
		return pfGoalScore{safeHome, safeGuest}
	}
	return pfExpectedGoalPair(response)
}

type pfEvilWaterPair struct{ Over, Under float64 }

func pfEvilCultGoalWaterPair(value interface{}) pfEvilWaterPair {
	switch typed := value.(type) {
	case []interface{}:
		return pfEvilWaterPair{pfNum(pfAt(typed, 0)), pfNum(pfAt(typed, 1))}
	case []string:
		over, under := pfNaN, pfNaN
		if len(typed) > 0 {
			over = pfNum(typed[0])
		}
		if len(typed) > 1 {
			under = pfNum(typed[1])
		}
		return pfEvilWaterPair{over, under}
	default:
		return pfEvilWaterPair{pfNaN, pfNaN}
	}
}

type pfEvilMarketSignal struct {
	LineScore   float64
	WaterScore  float64
	LineDetail  string
	WaterDetail string
}

func pfEvilCultGoalMarketSignal(response *analysisMatchResponse) pfEvilMarketSignal {
	openingLine := response.QiushuPankou1
	currentLine := response.QiushuPankou2
	validLines := pfFinite(openingLine) && openingLine > 0 && pfFinite(currentLine) && currentLine > 0
	lineDelta := pfNaN
	if validLines {
		lineDelta = currentLine - openingLine
	}
	lineScore := 0.0
	if pfFinite(lineDelta) {
		lineScore = pfRound(math.Max(-12, math.Min(12, lineDelta/0.25*4)), 1)
	}
	lineDetail := "初盘或即时盘缺失，不做升降盘修正"
	if pfFinite(lineDelta) {
		if math.Abs(lineDelta) < 0.01 {
			lineDetail = "盘口维持" + pfTrim(currentLine, 2) + "，没有升降盘修正"
		} else {
			direction := "降盘支持小球"
			if lineDelta > 0 {
				direction = "升盘支持大球"
			}
			lineDetail = pfTrim(openingLine, 2) + " → " + pfTrim(currentLine, 2) + "，" + direction
		}
	}
	openingWater := pfEvilCultGoalWaterPair(pfAt(response.Detail.Test15, 0))
	currentWater := pfEvilCultGoalWaterPair(pfAt(response.Detail.Test15, 1))
	validWater := pfFinite(openingWater.Over) && pfFinite(openingWater.Under) && pfFinite(currentWater.Over) && pfFinite(currentWater.Under)
	movementDiscount := 1.0
	if pfFinite(lineDelta) && math.Abs(lineDelta) >= 0.25 {
		movementDiscount = 0.5
	}
	rawWaterScore := 0.0
	if validWater {
		rawWaterScore = ((openingWater.Over - currentWater.Over) + (currentWater.Under - openingWater.Under)) * 20 * movementDiscount
	}
	waterScore := pfRound(math.Max(-6, math.Min(6, rawWaterScore)), 1)
	waterDetail := "初盘或即时水位缺失，不做水位修正"
	if validWater {
		suffix := ""
		if movementDiscount < 1 {
			suffix = "；已升降盘，水位信号减半"
		}
		waterDetail = "大球" + pfTrim(openingWater.Over, 2) + "→" + pfTrim(currentWater.Over, 2) + "，小球" + pfTrim(openingWater.Under, 2) + "→" + pfTrim(currentWater.Under, 2) + suffix
	}
	return pfEvilMarketSignal{lineScore, waterScore, lineDetail, waterDetail}
}

type pfEvilScores struct {
	Over             float64
	Under            float64
	ModelLine        float64
	ExpectedHome     float64
	ExpectedGuest    float64
	ExpectedTotal    float64
	ScoringAverage   float64
	History          float64
	Recent           float64
	GoalStatsAverage float64
	Balance          string
	Base             string
	OverHeat         float64
	UnderHeat        float64
	Steps            []platformEvilCultStep
}

func pfEvilCultDeltaScore(pressureValue float64) float64 {
	return pfRound(math.Max(0, math.Min(10, (pressureValue-50)/5*2)), 1)
}

func pfEvilCultAuditNumber(value float64) string {
	if !pfFinite(value) {
		return "-"
	}
	return pfTrim(value, 2)
}

func pfEvilCultAuditPercent(value float64) string {
	if !pfFinite(value) {
		return "-"
	}
	return pfTrim(value, 1) + "%"
}

func pfEvilCultScoreText(value float64) string {
	if !pfFinite(value) {
		return "-"
	}
	return pfTrim(value, 1)
}

func pfEvilCultGoalScores(response *analysisMatchResponse, line float64) pfEvilScores {
	expected := pfEvilCultGoalProjection(response)
	expectedTotal := expected.Home + expected.Guest
	historyGoals, recentGoals := pfSplitPair(response.ChangGuiQiuShu)
	history := pfHistoryGoalSampleValue(response, historyGoals)
	recent := pfRecentGoalSampleValue(response, recentGoals)
	homeAttack := pfNum(pfAt(response.LiangDuiQiuShu, 0))
	guestAttack := pfNum(pfAt(response.LiangDuiQiuShu, 1))
	homeConcede := pfNum(pfAt(response.LiangDuiQiuShu, 2))
	guestConcede := pfNum(pfAt(response.LiangDuiQiuShu, 3))
	goalStatsAverage := pfWeightedAverage([]pfWeighted{
		{homeAttack, 0.25}, {guestAttack, 0.25}, {homeConcede, 0.25}, {guestConcede, 0.25},
	})
	rawAverage := pfWeightedAverage([]pfWeighted{
		{expectedTotal, 0.5}, {history, 0.1}, {line, 0.4},
	})
	average := rawAverage
	if !pfFinite(average) {
		average = line
	}
	balanceSignal := pfGoalBalanceSignalForItem(response)
	balance := pfGoalBalanceDirection(balanceSignal)
	scoringAverage := average
	if balance == "under" {
		scoringAverage = math.Min(average, line)
	} else if balance == "over" {
		scoringAverage = math.Max(average, line)
	}
	base := pfBaseGoalSignal(response)
	over, under := 50.0, 50.0
	steps := []platformEvilCultStep{}
	addStep := func(label, detail string, overDelta, underDelta float64) {
		over += overDelta
		under += underDelta
		steps = append(steps, platformEvilCultStep{label, detail, overDelta, underDelta, over, under})
	}
	addStep("基础分", "大小球从50:50开始", 0, 0)
	averageDelta := (scoringAverage - line) * 18
	averageDetail := "综合均值" + pfTrim(scoringAverage, 2) + "与盘口" + pfTrim(line, 2) + "的差值 × 18"
	if balance != "" {
		averageDetail = "原均值" + pfTrim(average, 2) + "触发" + pfGoalBalanceSignalLabel(balanceSignal) + "，评分均值限制为" + pfTrim(scoringAverage, 2)
	}
	addStep("综合均值", averageDetail, averageDelta, -averageDelta)
	balanceOverDelta := 0.0
	if balanceSignal == "overCorrected" {
		balanceOverDelta = 18
	} else if balance == "over" {
		balanceOverDelta = 12
	}
	balanceUnderDelta := 0.0
	if balanceSignal == "underHidden" {
		balanceUnderDelta = 18
	} else if balance == "under" {
		balanceUnderDelta = 12
	}
	balanceDetail := "未触发大小球回归信号"
	if balanceSignal != "" {
		balanceDetail = pfGoalBalanceSignalLabel(balanceSignal) + "信号进入主推评分"
	}
	addStep("回归修正", balanceDetail, balanceOverDelta, balanceUnderDelta)
	baseDetail := "现有球数标签没有明确方向"
	if base != "" {
		direction := "小球"
		if base == "over" {
			direction = "大球"
		}
		baseDetail = "现有标签指向" + direction + "，但它由近期均球推算，为避免重复计权只展示"
	}
	addStep("基础球数信号", baseDetail, 0, 0)
	market := pfEvilCultGoalMarketSignal(response)
	lineOverDelta, lineUnderDelta := 0.0, 0.0
	if market.LineScore > 0 {
		lineOverDelta = market.LineScore
	} else if market.LineScore < 0 {
		lineUnderDelta = math.Abs(market.LineScore)
	}
	addStep("盘口升降", market.LineDetail, lineOverDelta, lineUnderDelta)
	waterOverDelta, waterUnderDelta := 0.0, 0.0
	if market.WaterScore > 0 {
		waterOverDelta = market.WaterScore
	} else if market.WaterScore < 0 {
		waterUnderDelta = math.Abs(market.WaterScore)
	}
	addStep("大小球水位", market.WaterDetail, waterOverDelta, waterUnderDelta)
	overPressure := pfNum(pfAt(response.QiuShuTouZhu, 0))
	underPressure := pfNum(pfAt(response.QiuShuTouZhu, 1))
	pressureDirection := ""
	if pfFinite(overPressure) && pfFinite(underPressure) {
		if overPressure >= underPressure {
			pressureDirection = "over"
		} else {
			pressureDirection = "under"
		}
	}
	pressureValue := underPressure
	if pressureDirection == "over" {
		pressureValue = overPressure
	}
	pressureScore := 0.0
	if pfFinite(pressureValue) {
		pressureScore = pfEvilCultDeltaScore(pressureValue)
	}
	pressureLabel := "无方向"
	if pressureDirection == "over" {
		pressureLabel = "大球"
	} else if pressureDirection == "under" {
		pressureLabel = "小球"
	}
	pressureOverDelta, pressureUnderDelta := 0.0, 0.0
	if pressureDirection == "over" {
		pressureOverDelta = pressureScore
	} else if pressureDirection == "under" {
		pressureUnderDelta = pressureScore
	}
	addStep("近期压力值", "大"+pfEvilCultAuditPercent(overPressure)+" / 小"+pfEvilCultAuditPercent(underPressure)+"，近期压力直接支持"+pressureLabel, pressureOverDelta, pressureUnderDelta)
	return pfEvilScores{
		Over: over, Under: under, ModelLine: line,
		ExpectedHome: expected.Home, ExpectedGuest: expected.Guest,
		ExpectedTotal: average, ScoringAverage: scoringAverage,
		History: history, Recent: recent, GoalStatsAverage: goalStatsAverage,
		Balance: balanceSignal, Base: base,
		OverHeat: overPressure, UnderHeat: underPressure,
		Steps: steps,
	}
}

type pfEvilSecondPass struct {
	InitialDirection string
	FinalDirection   string
	OverScore        float64
	UnderScore       float64
	Reversed         bool
	Forced           bool
	Reason           string
}

func pfEvilCultSignalDirection(score float64) string {
	if !pfFinite(score) || math.Abs(score) < 0.1 {
		return ""
	}
	if score > 0 {
		return "over"
	}
	return "under"
}

func pfEvilCultSecondPass(response *analysisMatchResponse, scores pfEvilScores) pfEvilSecondPass {
	initialDirection := "over"
	if scores.Under >= scores.Over {
		initialDirection = "under"
	}
	oppositeDirection := "under"
	if initialDirection == "under" {
		oppositeDirection = "over"
	}
	market := pfEvilCultGoalMarketSignal(response)
	lineDirection := pfEvilCultSignalDirection(market.LineScore)
	waterDirection := pfEvilCultSignalDirection(market.WaterScore)
	pressureValue := scores.UnderHeat
	if initialDirection == "over" {
		pressureValue = scores.OverHeat
	}
	pressureDirection := ""
	if scores.OverHeat > 60 {
		pressureDirection = "over"
	} else if scores.UnderHeat > 60 {
		pressureDirection = "under"
	}
	sameLine := lineDirection == initialDirection
	sameWater := waterDirection == initialDirection
	samePressure := pressureDirection == initialDirection
	reasons := []string{}
	reverseBonus := 0.0
	if sameLine {
		reverseBonus += 12
		text := "升盘继续暗示大球"
		if initialDirection == "under" {
			text = "降盘继续暗示小球"
		}
		reasons = append(reasons, text)
	}
	if sameWater {
		reverseBonus += 4
		text := "水位继续暗示大球"
		if initialDirection == "under" {
			text = "水位继续暗示小球"
		}
		reasons = append(reasons, text)
	}
	if samePressure && pfFinite(pressureValue) {
		heatBonus := 6 + math.Max(0, math.Floor((pressureValue-60)/5))*3
		reverseBonus += math.Min(15, heatBonus)
		text := "大球"
		if initialDirection == "under" {
			text = "小球"
		}
		reasons = append(reasons, text+"近期压力"+pfEvilCultAuditPercent(pressureValue))
	}
	overScore, underScore := scores.Over, scores.Under
	if oppositeDirection == "over" {
		overScore += reverseBonus
	} else {
		underScore += reverseBonus
	}
	forced := sameLine && samePressure
	if forced {
		if oppositeDirection == "over" {
			overScore = math.Max(overScore, underScore+1)
		} else {
			underScore = math.Max(underScore, overScore+1)
		}
	}
	finalDirection := "over"
	if underScore >= overScore {
		finalDirection = "under"
	}
	reversed := finalDirection != initialDirection
	initialLabel := "大球"
	if initialDirection == "under" {
		initialLabel = "小球"
	}
	finalLabel := "大球"
	if finalDirection == "under" {
		finalLabel = "小球"
	}
	reason := "没有发现与一推" + initialLabel + "同向的盘口诱导组合，二推维持" + finalLabel
	if len(reasons) > 0 {
		forcedText := ""
		if forced {
			forcedText = "并触发强制反向"
		}
		reason = strings.Join(reasons, "，") + "；反诱导加" + pfEvilCultScoreText(reverseBonus) + "分" + forcedText + "，一推" + initialLabel + " → 二推" + finalLabel
	}
	return pfEvilSecondPass{initialDirection, finalDirection, overScore, underScore, reversed, forced, reason}
}

func pfEvilCultUnderTotal(line, expectedTotal float64) int {
	maxUnder := 2.0
	if pfFinite(line) {
		maxUnder = math.Max(0, math.Floor(line))
	}
	target := maxUnder
	if pfFinite(expectedTotal) {
		target = math.Round(expectedTotal)
	}
	return int(math.Max(0, math.Min(maxUnder, target)))
}

func pfEvilCultChaseOverTotal(line, expectedTotal float64) int {
	base := 2.0
	if pfFinite(line) {
		base = math.Max(0, math.Floor(line))
	}
	candidates := []float64{base + 1, base + 2, base + 4}
	target := base + 1
	if pfFinite(expectedTotal) {
		target = math.Max(base+1, expectedTotal)
	}
	best := candidates[0]
	bestGap := math.Abs(candidates[0] - target)
	for _, candidate := range candidates[1:] {
		gap := math.Abs(candidate - target)
		if gap < bestGap || (gap == bestGap && candidate < best) {
			best, bestGap = candidate, gap
		}
	}
	return int(best)
}

func pfEvilCultGoalAllocation(response *analysisMatchResponse, total int) pfGoalScore {
	expected := pfEvilCultGoalProjection(response)
	if !pfFinite(expected.Home) || !pfFinite(expected.Guest) || expected.Home+expected.Guest <= 0 {
		return pfAllocateIntegerGoalTotal(float64(total), pfGoalScore{1, 1}, response.YapanPankou2, true)
	}
	return pfAllocateIntegerGoalTotal(float64(total), expected, response.YapanPankou2, true)
}

func pfEvilCultReason(scores pfEvilScores, line float64, underTotal, overTotal int) string {
	side := "先小"
	if scores.Over >= scores.Under {
		side = "追大"
	}
	balanceText := "无回归修正"
	switch scores.Balance {
	case "underHidden":
		balanceText = "盘口隐藏回归小球"
	case "under":
		balanceText = "回归小球"
	case "overCorrected":
		balanceText = "盘口修正回归大球"
	case "over":
		balanceText = "回归大球"
	}
	floorLine := int(math.Floor(line))
	return "原始盘" + pfTrim(scores.ModelLine, 2) + "，结算按" + pfTrim(line, 2) + "；小球覆盖0-" + strconv.Itoa(floorLine) + "球，追大候选" + strconv.Itoa(floorLine+1) + "/" + strconv.Itoa(floorLine+2) + "/" + strconv.Itoa(floorLine+4) + "球；均值" + pfTrim(scores.ExpectedTotal, 2) + "，" + balanceText + "，追大" + strconv.Itoa(int(math.Round(scores.Over))) + "分/先小" + strconv.Itoa(int(math.Round(scores.Under))) + "分，一推" + side + "，小球点" + strconv.Itoa(underTotal) + "球/追大点" + strconv.Itoa(overTotal) + "球"
}

func pfEvilCultAuditInputs(response *analysisMatchResponse, line float64, scores pfEvilScores) []platformEvilCultAuditInput {
	openingLine := response.QiushuPankou1
	currentLine := response.QiushuPankou2
	goalStats := []float64{
		pfNum(pfAt(response.LiangDuiQiuShu, 0)),
		pfNum(pfAt(response.LiangDuiQiuShu, 1)),
		pfNum(pfAt(response.LiangDuiQiuShu, 2)),
		pfNum(pfAt(response.LiangDuiQiuShu, 3)),
	}
	goalStatsText := []string{}
	for _, value := range goalStats {
		goalStatsText = append(goalStatsText, pfEvilCultAuditNumber(value))
	}
	openingWater := pfEvilCultGoalWaterPair(pfAt(response.Detail.Test15, 0))
	currentWater := pfEvilCultGoalWaterPair(pfAt(response.Detail.Test15, 1))
	balanceLabel := pfGoalBalanceSignalLabel(scores.Balance)
	baseLabel := "无明确方向"
	if scores.Base == "over" {
		baseLabel = "大球"
	} else if scores.Base == "under" {
		baseLabel = "小球"
	}
	balanceDetail := "没有进入额外回归修正"
	if scores.Balance != "" {
		balanceDetail = "触发后限制同一批高低均值重复给原方向加分，并进入回归修正"
	}
	scoringDetail := "未触发回归，直接使用综合均值"
	if scores.Balance != "" {
		scoringDetail = "综合均值经过" + balanceLabel + "限制后的评分值"
	}
	return []platformEvilCultAuditInput{
		{"大小球盘口", pfEvilCultAuditNumber(openingLine) + " / " + pfEvilCultAuditNumber(currentLine) + " → " + pfEvilCultAuditNumber(line), "初盘 / 即时盘 → 结算半球线；评分保留原始盘口" + pfEvilCultAuditNumber(scores.ModelLine)},
		{"两队预期进球", pfEvilCultAuditNumber(scores.ExpectedHome) + " + " + pfEvilCultAuditNumber(scores.ExpectedGuest), "自身场均进球55% + 对手场均丢球45%，合计权重50%"},
		{"近期平均球数", pfEvilCultAuditNumber(scores.Recent), "和攻防场均来自同一批比赛，只用于核对，不再重复加权"},
		{"历史平均球数", pfEvilCultAuditNumber(scores.History), "有历史样本时参与，权重10%；缺失时自动剔除"},
		{"进失球统计均值", pfEvilCultAuditNumber(scores.GoalStatsAverage), "主进/客进/主丢/客丢场均：" + strings.Join(goalStatsText, " / ") + "；已修正为场均，不再使用5场总数"},
		{"盘口锚点", pfEvilCultAuditNumber(scores.ModelLine), "使用原始即时盘口进入综合均值，权重40%"},
		{"综合均值", pfEvilCultAuditNumber(scores.ExpectedTotal), "攻防预期50% + 历史10% + 原始即时盘口40%"},
		{"回归信号", balanceLabel, balanceDetail},
		{"实际评分均值", pfEvilCultAuditNumber(scores.ScoringAverage), scoringDetail},
		{"基础球数信号", baseLabel, "由近期均球推算，只展示，不再重复加分"},
		{"近期压力值", "大" + pfEvilCultAuditPercent(scores.OverHeat) + " / 小" + pfEvilCultAuditPercent(scores.UnderHeat), "参与一推评分；超过60%后，还会作为二推的热门同向诱导信号"},
		{"大小球水位", "大" + pfEvilCultAuditNumber(openingWater.Over) + "→" + pfEvilCultAuditNumber(currentWater.Over) + " / 小" + pfEvilCultAuditNumber(openingWater.Under) + "→" + pfEvilCultAuditNumber(currentWater.Under), "盘口不动时直接判断水位方向；发生升降盘时水位信号减半，避免重复计算"},
	}
}

func pfEvilCultScorePercent(scores pfEvilScores, side string) int {
	over := math.Max(0, scores.Over)
	under := math.Max(0, scores.Under)
	total := over + under
	if total <= 0 {
		return 50
	}
	value := under
	if side == "over" {
		value = over
	}
	return int(math.Round(value / total * 100))
}

func pfBuildEvilCult(response *analysisMatchResponse) platformEvilCult {
	line := pfEvilCultGoalLine(response)
	scores := pfEvilCultGoalScores(response, pfEvilCultRawGoalLine(response))
	underTotal := pfEvilCultUnderTotal(line, scores.ExpectedTotal)
	overTotal := pfEvilCultChaseOverTotal(line, scores.ExpectedTotal)
	secondPass := pfEvilCultSecondPass(response, scores)
	firstDirection := secondPass.InitialDirection
	mainDirection := secondPass.FinalDirection
	mainTotal := overTotal
	secondaryTotal := underTotal
	if mainDirection == "under" {
		mainTotal, secondaryTotal = underTotal, overTotal
	}
	underGoals := pfEvilCultGoalAllocation(response, underTotal)
	overGoals := pfEvilCultGoalAllocation(response, overTotal)
	mainGoals := pfEvilCultGoalAllocation(response, mainTotal)
	secondaryGoals := pfEvilCultGoalAllocation(response, secondaryTotal)
	score := fmt.Sprintf("%d:%d", int(mainGoals.Home), int(mainGoals.Guest))
	secondaryScore := fmt.Sprintf("%d:%d", int(secondaryGoals.Home), int(secondaryGoals.Guest))
	underScore := fmt.Sprintf("%d:%d", int(underGoals.Home), int(underGoals.Guest))
	overScore := fmt.Sprintf("%d:%d", int(overGoals.Home), int(overGoals.Guest))
	goalLineText := pfTrim(line, 2)
	underPick := "小球组：小" + goalLineText + " / " + strconv.Itoa(underTotal) + "球 / " + underScore
	overPick := "追大组：追大" + goalLineText + " / " + strconv.Itoa(overTotal) + "球 / " + overScore
	firstPick := overPick
	if firstDirection == "under" {
		firstPick = underPick
	}
	mainPick, reversePick := overPick, underPick
	if mainDirection == "under" {
		mainPick, reversePick = underPick, overPick
	}
	firstReason := pfEvilCultReason(scores, line, underTotal, overTotal)
	goalText := "小" + goalLineText
	secondaryGoalText := "追大" + goalLineText
	if mainDirection == "over" {
		goalText, secondaryGoalText = "追大"+goalLineText, "小"+goalLineText
	}
	goalTone, reverseTone := "green", "red"
	if mainDirection == "under" {
		goalTone, reverseTone = "red", "green"
	}
	note := "追大剧本更强，保留小球次选"
	if mainDirection == "under" {
		note = "固定先小，错了追大"
	}
	secondaryDirection := "under"
	if mainDirection == "under" {
		secondaryDirection = "over"
	}
	prediction := platformEvilCultPrediction{
		Goal:                   goalText,
		SecondaryGoal:          secondaryGoalText,
		Total:                  strconv.Itoa(mainTotal) + "球",
		SecondaryTotal:         strconv.Itoa(secondaryTotal) + "球",
		UnderGoal:              "小" + goalLineText,
		OverGoal:               "追大" + goalLineText,
		UnderTotalText:         strconv.Itoa(underTotal) + "球",
		OverTotalText:          strconv.Itoa(overTotal) + "球",
		UnderTotalValue:        underTotal,
		OverTotalValue:         overTotal,
		UnderGoalLine:          line,
		OverGoalLine:           line,
		UnderScore:             underScore,
		OverScore:              overScore,
		UnderOutcome:           pfScoreOutcome(underScore),
		OverOutcome:            pfScoreOutcome(overScore),
		FirstPick:              firstPick,
		FirstDirection:         firstDirection,
		MainPick:               mainPick,
		ReversePick:            reversePick,
		MainReason:             firstReason + "；二推：" + secondPass.Reason,
		SecondPassReason:       secondPass.Reason,
		SecondPassReversed:     secondPass.Reversed,
		SecondPassForced:       secondPass.Forced,
		SecondOverScore:        secondPass.OverScore,
		SecondUnderScore:       secondPass.UnderScore,
		MainTotal:              mainTotal,
		SecondaryTotalValue:    secondaryTotal,
		GoalDirection:          mainDirection,
		SecondaryGoalDirection: secondaryDirection,
		GoalLine:               line,
		SecondaryGoalLine:      line,
		Score:                  score,
		SecondaryScore:         secondaryScore,
		Outcome:                pfScoreOutcome(score),
		SecondaryOutcome:       pfScoreOutcome(secondaryScore),
		GoalTone:               goalTone,
		ReverseTone:            reverseTone,
		Note:                   note,
		Reason:                 firstReason + "；二推：" + secondPass.Reason,
	}
	rows := []platformEvilCultRow{
		{"大小球", prediction.UnderGoal, prediction.OverGoal, "normal", "red", "green"},
		{"球数", prediction.UnderTotalText, prediction.OverTotalText, "normal", "red", "green"},
		{"比分", prediction.UnderScore, prediction.OverScore, "normal", pfScoreTone(prediction.UnderScore), pfScoreTone(prediction.OverScore)},
		{"胜平负", pfOutcomeLabelByKey(prediction.UnderOutcome, response.Home, response.Guest), pfOutcomeLabelByKey(prediction.OverOutcome, response.Home, response.Guest), "normal", pfOutcomeTone(prediction.UnderOutcome), pfOutcomeTone(prediction.OverOutcome)},
	}
	return platformEvilCult{
		Line:       line,
		Rows:       rows,
		Prediction: prediction,
		Scores: platformEvilCultScores{
			Over:         scores.Over,
			Under:        scores.Under,
			OverPercent:  pfEvilCultScorePercent(scores, "over"),
			UnderPercent: pfEvilCultScorePercent(scores, "under"),
			Steps:        scores.Steps,
		},
		Inputs: pfEvilCultAuditInputs(response, line, scores),
	}
}

// ---------- assembly ----------

func pfGoalPairJSON(pair pfGoalScore) platformGoalPair {
	result := platformGoalPair{}
	if pfFinite(pair.Home) {
		home := pair.Home
		result.Home = &home
	}
	if pfFinite(pair.Guest) {
		guest := pair.Guest
		result.Guest = &guest
	}
	return result
}

// buildPlatformDecision computes the whole unified decision block. It must be
// called after attachAnalysisWeights so RoiSimulation markets are available.
func buildPlatformDecision(response *analysisMatchResponse) *platformDecision {
	bookmakerOutcome := pfBookmakerResultOutcome(response)
	bookmakerGoal := pfBookmakerGoalResult(response)
	bookmaker := platformGuidePrediction{
		Outcome:        bookmakerOutcome,
		Goal:           bookmakerGoal,
		Score:          pfBookmakerFusedScore(response, bookmakerOutcome, bookmakerGoal.Total),
		SecondaryScore: pfSecondaryGuideScore(response, bookmakerOutcome, bookmakerGoal, "bookmaker"),
	}

	platformOutcome := pfPlatformLiveOutcome(response)
	platformGoal := pfPlatformLiveGoalResult(response)
	platform := platformGuidePrediction{
		Outcome:        platformOutcome,
		Goal:           platformGoal,
		Score:          pfPlatformFusedScore(response, platformOutcome, platformGoal.Total),
		SecondaryScore: pfSecondaryGuideScore(response, platformOutcome, platformGoal, "platform"),
	}
	if pfPlatformOverheatOutcome(response) == platformOutcome {
		platform.Warning = "过热"
		platform.WarningTone = "blue"
	}

	warningAdjusted := pfWarningAdjustedPrediction(response)
	warningAdjustedSummary := ""
	if warningAdjusted != nil {
		// 该"警示后预测"取凯体共识方向，回测仅约21%命中——标注为反向参考。
		warningAdjustedSummary = "警示后预测（凯体共识向，历史仅约21%命中，建议反向参考）：" + pfOutcomeLabelByKey(warningAdjusted.Outcome, response.Home, response.Guest) + " / " + warningAdjusted.Goal.Label + " / " + warningAdjusted.Score
	}

	bands := pfExpectedGoalBands(response)
	return &platformDecision{
		Bookmaker:              bookmaker,
		Platform:               platform,
		WarningRows:            pfGuideWarningRows(response, platform),
		WarningAdjusted:        warningAdjusted,
		WarningAdjustedSummary: warningAdjustedSummary,
		ProfessionalConflict:   pfProfessionalConflictWarning(response),
		ProfessionalConsensus:  pfProfessionalConsensusOutcome(response),
		SportteryComfort:       pfMarketComfortOutcome(response, "sporttery"),
		RqspfComfort:           pfMarketComfortOutcome(response, "sportteryRqspf"),
		DrawRisk:               pfDrawRiskSignal(response),
		HandicapPressureLabel:  pfHandicapPressureSignalLabel(response),
		GoalBalanceSignal:      pfGoalBalanceSignalForItem(response),
		Goals: platformGoalBands{
			Under: pfGoalPairJSON(bands.Under),
			Main:  pfGoalPairJSON(bands.Main),
			Over:  pfGoalPairJSON(bands.Over),
		},
		ZeroGoalAdvice:       pfZeroGoalAdvice(response, bands.Main),
		HandicapAlertRows:    pfHandicapPressureAlertRows(response),
		GoalBalanceAlertRows: pfGoalBalanceAlertRows(response),
		EvilCult:             pfBuildEvilCult(response),
		LocalMarket:          pfLocalProfitMarket(response),
	}
}
