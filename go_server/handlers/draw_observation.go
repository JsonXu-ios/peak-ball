package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/internal/drawmodel"
	"go_server/models"
)

type drawObservationRecentMatch struct {
	MatchID   string    `json:"matchId"`
	Date      string    `json:"date"`
	MatchTime time.Time `json:"matchTime"`
	League    string    `json:"league"`
	Home      string    `json:"home"`
	Guest     string    `json:"guest"`
	HomeScore int       `json:"homeScore"`
	AwayScore int       `json:"awayScore"`
	Venue     string    `json:"venue"`
	Result    string    `json:"result"`
}

type drawObservationRecent struct {
	Sample  int                          `json:"sample"`
	Draws   int                          `json:"draws"`
	Matches []drawObservationRecentMatch `json:"matches"`
}

type drawObservationLine struct {
	Opening     *string    `json:"opening"`
	Latest      *string    `json:"latest"`
	Historical  *string    `json:"historical"`
	Company     string     `json:"company"`
	Source      string     `json:"source"`
	OpeningOdds []*string  `json:"openingOdds"`
	LatestOdds  []*string  `json:"latestOdds"`
	CapturedAt  *time.Time `json:"capturedAt"`
}

type drawObservationEuroOdds struct {
	OpeningStatus string     `json:"openingStatus,omitempty"`
	Company       string     `json:"company"`
	Opening       []*string  `json:"opening"`
	Latest        []*string  `json:"latest"`
	Source        string     `json:"source"`
	CapturedAt    *time.Time `json:"capturedAt"`
}

type drawObservationItem struct {
	MatchID             string                           `json:"matchId"`
	JingcaiID           string                           `json:"jingcaiId"`
	Date                string                           `json:"date"`
	MatchTime           time.Time                        `json:"matchTime"`
	League              string                           `json:"league"`
	Home                string                           `json:"home"`
	Guest               string                           `json:"guest"`
	HomeScore           int                              `json:"homeScore"`
	AwayScore           int                              `json:"awayScore"`
	Status              int                              `json:"status"`
	DisplayState        string                           `json:"displayState"`
	Settled             bool                             `json:"settled"`
	RegulationSettled   *bool                            `json:"regulationSettled,omitempty"`
	HomeRecent          drawObservationRecent            `json:"homeRecent"`
	AwayRecent          drawObservationRecent            `json:"awayRecent"`
	HeadToHead          drawObservationRecent            `json:"headToHead"`
	AsianHandicap       drawObservationLine              `json:"asianHandicap"`
	GoalsLine           drawObservationLine              `json:"goalsLine"`
	EuroOdds            drawObservationEuroOdds          `json:"euroOdds"`
	FrontendAnalysis    *drawObservationFrontendAnalysis `json:"frontendAnalysis,omitempty"`
	CompanyQuotes       []drawObservationCompanyQuotes   `json:"companyQuotes,omitempty"`
	Sources             []string                         `json:"sources"`
	Warnings            []string                         `json:"warnings"`
	FrozenDrawSignal    *drawObservationFrozenSignal     `json:"frozenDrawSignal,omitempty"`
	SecondaryDecision   *drawObservationSecondarySignal  `json:"secondaryDecision,omitempty"`
	ScoreInputs         *drawScoreInputs                 `json:"scoreInputs,omitempty"`
	ScoreRecommendation *drawScoreRecommendation         `json:"scoreRecommendation,omitempty"`
	ResearchDecision    *drawObservationResearchDecision `json:"researchDecision,omitempty"`
}

type drawObservationSummary struct {
	HistoricalMatches int `json:"historicalMatches"`
	DrawMatches       int `json:"drawMatches"`
	AsianMatches      int `json:"asianMatches"`
	TotalMatches      int `json:"totalMatches"`
}

type drawObservationResponse struct {
	DataVersion        string                           `json:"dataVersion,omitempty"`
	GeneratedAt        *time.Time                       `json:"generatedAt,omitempty"`
	ScheduleTimezone   string                           `json:"scheduleTimezone"`
	Mode               string                           `json:"mode"`
	Scope              string                           `json:"scope"`
	Date               string                           `json:"date"`
	Summary            drawObservationSummary           `json:"summary"`
	ObservationMetrics *drawObservationMetrics          `json:"observationMetrics,omitempty"`
	ResearchTakeover   *drawObservationResearchTakeover `json:"researchTakeover,omitempty"`
	Total              int                              `json:"total"`
	Page               int                              `json:"page"`
	PageSize           int                              `json:"pageSize"`
	Leagues            []string                         `json:"leagues"`
	Items              []drawObservationItem            `json:"items"`
	Notes              []string                         `json:"notes"`
}

type drawObservationQuery struct {
	mode, date, scope              string
	start, end, league, search     string
	page, size                     int
	refresh                        bool
	homeDraws, awayDraws, h2hDraws drawObservationRange
}

type drawObservationRange struct{ min, max *int }

func (r drawObservationRange) enabled() bool { return r.min != nil || r.max != nil }

func (r drawObservationRange) matches(window drawObservationRecent) bool {
	if !r.enabled() {
		return true
	}
	return window.Sample > 0 && (r.min == nil || window.Draws >= *r.min) && (r.max == nil || window.Draws <= *r.max)
}

func parseDrawObservationRange(c *gin.Context, prefix string, ceiling int) (drawObservationRange, error) {
	rangeValue := drawObservationRange{}
	for _, bound := range []struct {
		suffix string
		value  **int
	}{{"min", &rangeValue.min}, {"max", &rangeValue.max}} {
		key := prefix + "_draws_" + bound.suffix
		raw := c.Query(key)
		if raw == "" {
			continue
		}
		for _, digit := range raw {
			if digit < '0' || digit > '9' {
				return rangeValue, fmt.Errorf("%s 必须为非负整数", key)
			}
		}
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			return rangeValue, fmt.Errorf("%s 必须为非负整数", key)
		}
		if ceiling >= 0 && value > ceiling {
			return rangeValue, fmt.Errorf("%s 不能超过 %d", key, ceiling)
		}
		*bound.value = &value
	}
	if rangeValue.min != nil && rangeValue.max != nil && *rangeValue.min > *rangeValue.max {
		return rangeValue, fmt.Errorf("%s_draws_min 不能大于 %s_draws_max", prefix, prefix)
	}
	return rangeValue, nil
}

func parseDrawObservationQuery(c *gin.Context) (drawObservationQuery, error) {
	q := drawObservationQuery{mode: c.DefaultQuery("mode", "today"), scope: c.DefaultQuery("scope", "all"), date: c.Query("date"), start: c.Query("start_date"), end: c.Query("end_date"), league: c.Query("league"), search: strings.ToLower(strings.TrimSpace(c.Query("search"))), page: 1, size: 50, refresh: c.Query("refresh") == "1"}
	q = normalizeDrawObservationQuery(q)
	if q.mode != "today" && q.mode != "history" {
		return q, fmt.Errorf("mode 应为 today 或 history")
	}
	if q.scope != "all" && q.scope != "sporttery" {
		return q, fmt.Errorf("scope 应为 all 或 sporttery")
	}
	for _, value := range []string{q.date, q.start, q.end} {
		if value != "" {
			if _, err := drawDate(value); err != nil {
				return q, fmt.Errorf("日期格式应为 YYYY-MM-DD")
			}
		}
	}
	if q.start != "" && q.end != "" && q.start > q.end {
		return q, fmt.Errorf("开始日期不能晚于结束日期")
	}
	if page, err := strconv.Atoi(c.Query("page")); err == nil && page > 0 {
		q.page = page
	}
	if size, err := strconv.Atoi(c.Query("page_size")); err == nil && size > 0 {
		q.size = size
	}
	if q.size > 200 {
		q.size = 200
	}
	var err error
	if q.homeDraws, err = parseDrawObservationRange(c, "home", 10); err != nil {
		return q, err
	}
	if q.awayDraws, err = parseDrawObservationRange(c, "away", 10); err != nil {
		return q, err
	}
	if q.h2hDraws, err = parseDrawObservationRange(c, "h2h", -1); err != nil {
		return q, err
	}
	return q, nil
}

// GetDrawObservations reads local data through a short-lived read cache.
// refresh=1 invalidates reads only; it never collects or writes match data.
func GetDrawObservations(c *gin.Context) {
	q, err := parseDrawObservationQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := drawObservationResults.read(q)
	if err != nil {
		drawHTTPError(c, err)
		return
	}
	// Recompute the row-aware display contract from this exact saved input.
	// Older durable days are regenerated before reaching this point; never
	// fabricate a fixed score pair when source evidence is unavailable.
	for i := range response.Items {
		decision := drawObservationSecondaryDecision(response.Items[i])
		response.Items[i].SecondaryDecision = &decision
		recommendation := drawObservationScoreRecommendation(response.Items[i])
		response.Items[i].ScoreRecommendation = &recommendation
	}
	if takeover := buildDrawResearchTakeover(response.Items); takeover != nil {
		response.ResearchTakeover = takeover
	}
	c.JSON(http.StatusOK, response)
}

func normalizeDrawObservationQuery(q drawObservationQuery) drawObservationQuery {
	return normalizeDrawObservationQueryAt(q, time.Now())
}

func normalizeDrawObservationQueryAt(q drawObservationQuery, now time.Time) drawObservationQuery {
	if q.mode == "" {
		q.mode = "today"
	}
	if q.scope == "" {
		q.scope = "all"
	}
	if q.date != "" {
		q.start, q.end = "", ""
	} else if q.start == "" && q.end == "" {
		// The date column is the source's Beijing schedule day, not a UTC day.
		q.date = now.In(drawSourceLocation).Format("2006-01-02")
	}
	return q
}

func drawObservationScopeMatches(match models.Money, scope string) bool {
	return scope != "sporttery" || strings.TrimSpace(match.JingcaiID) != ""
}

func drawObservationDateMatches(date string, q drawObservationQuery) bool {
	if q.date != "" {
		return date == q.date
	}
	return (q.start == "" || date >= q.start) && (q.end == "" || date <= q.end)
}

type drawObservationTarget struct {
	Match   models.Money
	Sources []string
}

type drawObservationData struct {
	Targets  []drawObservationTarget
	History  []drawHistoricalGame
	Odds     map[string]models.OddsMoney
	Pankou   map[string]models.PankouMoney
	coverage map[string]drawObservationCoverage
	summary  *drawObservationSummary
	window   func(drawmodel.Match) drawObservationWindows
}

// Only actual match-table rows can become observation targets. Embedded
// recent/against fixtures are separate input and never increase these counts.
func mergeDrawObservationTargets(current, legacy []models.Money) []drawObservationTarget {
	byID := map[string]drawObservationTarget{}
	for _, source := range []struct {
		name    string
		matches []models.Money
	}{{"money", legacy}, {"moneys", current}} {
		for _, match := range source.matches {
			if strings.TrimSpace(match.MatchId) == "" || strings.EqualFold(strings.TrimSpace(match.DisplayState), detailOnlyDisplayState) {
				continue
			}
			old := byID[match.MatchId]
			sources := old.Sources
			if !drawContains(sources, source.name) {
				sources = append(sources, source.name)
			}
			byID[match.MatchId] = drawObservationTarget{Match: match, Sources: sources}
		}
	}
	targets := make([]drawObservationTarget, 0, len(byID))
	for _, target := range byID {
		targets = append(targets, target)
	}
	return targets
}

func drawObservationName(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), " ")
}

type drawObservationIndex struct {
	byID, byName map[string][]drawmodel.Match
	nameIDs      map[string]map[string]bool
}

func indexDrawObservations(games []drawHistoricalGame) drawObservationIndex {
	idx := drawObservationIndex{byID: map[string][]drawmodel.Match{}, byName: map[string][]drawmodel.Match{}, nameIDs: map[string]map[string]bool{}}
	for _, raw := range games {
		g := raw.Game
		for _, team := range []struct{ id, name string }{{g.HomeTeamID, g.HomeTeamName}, {g.AwayTeamID, g.AwayTeamName}} {
			id, name := strings.TrimSpace(team.id), drawObservationName(team.name)
			if id != "" {
				idx.byID[id] = append(idx.byID[id], g)
			}
			if name != "" {
				idx.byName[name] = append(idx.byName[name], g)
				if id != "" {
					if idx.nameIDs[name] == nil {
						idx.nameIDs[name] = map[string]bool{}
					}
					idx.nameIDs[name][id] = true
				}
			}
		}
	}
	for key, pool := range idx.byID {
		idx.byID[key] = drawObservationSortedPool(pool)
	}
	for key, pool := range idx.byName {
		idx.byName[key] = drawObservationSortedPool(pool)
	}
	return idx
}

func drawObservationSortedPool(pool []drawmodel.Match) []drawmodel.Match {
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].Kickoff.Equal(pool[j].Kickoff) {
			return pool[i].ID < pool[j].ID
		}
		return pool[i].Kickoff.After(pool[j].Kickoff)
	})
	result := make([]drawmodel.Match, 0, len(pool))
	seen := map[string]bool{}
	for _, game := range pool {
		key := drawObservationGameKey(game)
		if !seen[key] {
			seen[key] = true
			result = append(result, game)
		}
	}
	return result
}

func (idx drawObservationIndex) teamPool(id, name string) []drawmodel.Match {
	a, b := idx.byID[id], idx.byName[name]
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}
	pool := make([]drawmodel.Match, 0, len(a)+len(b))
	seen := map[string]bool{}
	for i, j := 0, 0; i < len(a) || j < len(b); {
		var game drawmodel.Match
		if j == len(b) || i < len(a) && (a[i].Kickoff.After(b[j].Kickoff) || a[i].Kickoff.Equal(b[j].Kickoff) && a[i].ID <= b[j].ID) {
			game = a[i]
			i++
		} else {
			game = b[j]
			j++
		}
		key := drawObservationGameKey(game)
		if !seen[key] {
			seen[key] = true
			pool = append(pool, game)
		}
	}
	return pool
}

func (idx drawObservationIndex) recent(target drawmodel.Match, id, name string) (drawObservationRecent, []string) {
	recent := drawObservationRecent{Matches: []drawObservationRecentMatch{}}
	warnings := []string{}
	id, normalized := strings.TrimSpace(id), drawObservationName(name)
	resolvedFromName := false
	if id == "" && len(idx.nameIDs[normalized]) == 1 {
		for knownID := range idx.nameIDs[normalized] {
			id, resolvedFromName = knownID, true
		}
	}
	pool := idx.teamPool(id, normalized)
	fallback, ambiguous := resolvedFromName, false
	identity := func(otherID, otherName string) bool {
		otherID = strings.TrimSpace(otherID)
		if id != "" && otherID != "" {
			return id == otherID
		}
		if normalized == "" || normalized != drawObservationName(otherName) {
			return false
		}
		if len(idx.nameIDs[normalized]) > 1 {
			ambiguous = true
			return false
		}
		if id != "" && len(idx.nameIDs[normalized]) == 1 && !idx.nameIDs[normalized][id] {
			ambiguous = true
			return false
		}
		fallback = true
		return true
	}
	seen := map[string]bool{}
	for _, g := range pool {
		if !drawObservationBefore(g, target) {
			continue
		}
		home, away := identity(g.HomeTeamID, g.HomeTeamName), identity(g.AwayTeamID, g.AwayTeamName)
		if !home && !away {
			continue
		}
		key := drawObservationGameKey(g)
		if seen[key] {
			continue
		}
		seen[key] = true
		record := drawObservationRecord(g, home)
		if record.Result == "draw" {
			recent.Draws++
		}
		recent.Matches = append(recent.Matches, record)
		if len(recent.Matches) == 10 {
			break
		}
	}
	recent.Sample = len(recent.Matches)
	if fallback {
		warnings = append(warnings, "部分球队 ID 缺失，近况按规范化同名回退匹配")
	}
	if ambiguous {
		warnings = append(warnings, "同名对应多个球队 ID，已排除无法确认身份的近况记录")
	}
	return recent, warnings
}

func drawObservationBefore(game, target drawmodel.Match) bool {
	if !game.Completed || game.Kickoff.IsZero() || !game.Kickoff.Before(target.Kickoff) || game.ID != "" && game.ID == target.ID || game.HomeGoals < 0 || game.AwayGoals < 0 {
		return false
	}
	finished := game.CompletedAt
	if finished.IsZero() || !finished.After(game.Kickoff) {
		finished = game.Kickoff.Add(2 * time.Hour)
	}
	return finished.Before(target.Kickoff)
}

func drawObservationGameKey(game drawmodel.Match) string {
	if game.ID != "" {
		return game.ID
	}
	return fmt.Sprintf("%s|%s|%s|%s|%d", game.HomeTeamID, game.HomeTeamName, game.AwayTeamID, game.AwayTeamName, game.Kickoff.UnixNano())
}

func drawObservationRecord(game drawmodel.Match, atHome bool) drawObservationRecentMatch {
	venue, gf, ga := "home", game.HomeGoals, game.AwayGoals
	if !atHome {
		venue, gf, ga = "away", game.AwayGoals, game.HomeGoals
	}
	result := "draw"
	if gf > ga {
		result = "win"
	} else if gf < ga {
		result = "loss"
	}
	return drawObservationRecentMatch{MatchID: game.ID, Date: game.Kickoff.In(drawSourceLocation).Format("2006-01-02"), MatchTime: game.Kickoff, League: game.LeagueName, Home: game.HomeTeamName, Guest: game.AwayTeamName, HomeScore: game.HomeGoals, AwayScore: game.AwayGoals, Venue: venue, Result: result}
}

// headToHead includes all available meetings, with results from the target
// home team's perspective even when that team was away in an older meeting.
func (idx drawObservationIndex) headToHead(target drawmodel.Match) (drawObservationRecent, []string) {
	window := drawObservationRecent{Matches: []drawObservationRecentMatch{}}
	fallback, ambiguous := false, false
	matcher := func(id, name string) (string, string, func(string, string) bool) {
		id, name = strings.TrimSpace(id), drawObservationName(name)
		if id == "" && len(idx.nameIDs[name]) == 1 {
			for knownID := range idx.nameIDs[name] {
				id, fallback = knownID, true
			}
		}
		return id, name, func(otherID, otherName string) bool {
			otherID = strings.TrimSpace(otherID)
			if id != "" && otherID != "" {
				return id == otherID
			}
			if name == "" || name != drawObservationName(otherName) {
				return false
			}
			if len(idx.nameIDs[name]) > 1 {
				ambiguous = true
				return false
			}
			if id != "" && len(idx.nameIDs[name]) == 1 && !idx.nameIDs[name][id] {
				ambiguous = true
				return false
			}
			fallback = true
			return true
		}
	}
	homeID, homeName, isHomeTeam := matcher(target.HomeTeamID, target.HomeTeamName)
	_, _, isAwayTeam := matcher(target.AwayTeamID, target.AwayTeamName)
	pool := idx.teamPool(homeID, homeName)
	seen := map[string]bool{}
	for _, game := range pool {
		if !drawObservationBefore(game, target) {
			continue
		}
		atHome := isHomeTeam(game.HomeTeamID, game.HomeTeamName) && isAwayTeam(game.AwayTeamID, game.AwayTeamName)
		atAway := isHomeTeam(game.AwayTeamID, game.AwayTeamName) && isAwayTeam(game.HomeTeamID, game.HomeTeamName)
		if !atHome && !atAway {
			continue
		}
		key := drawObservationGameKey(game)
		if seen[key] {
			continue
		}
		seen[key] = true
		record := drawObservationRecord(game, atHome)
		window.Matches = append(window.Matches, record)
		if record.Result == "draw" {
			window.Draws++
		}
	}
	window.Sample = len(window.Matches)
	warnings := []string{}
	if fallback {
		warnings = append(warnings, "部分交锋球队 ID 缺失，按规范化同名回退匹配")
	}
	if ambiguous {
		warnings = append(warnings, "交锋存在同名不同球队 ID，已排除身份不明记录")
	}
	return window, warnings
}

type drawObservationWindows struct {
	home, away, h2h drawObservationRecent
	warnings        []string
}

func (idx drawObservationIndex) windows(target drawmodel.Match) drawObservationWindows {
	home, hw := idx.recent(target, target.HomeTeamID, target.HomeTeamName)
	away, aw := idx.recent(target, target.AwayTeamID, target.AwayTeamName)
	h2h, cw := idx.headToHead(target)
	warnings := append(hw, aw...)
	warnings = append(warnings, cw...)
	return drawObservationWindows{home: home, away: away, h2h: h2h, warnings: warnings}
}

func drawObservationRawLine(raw json.RawMessage) *string {
	value := strings.TrimSpace(drawRawString(raw))
	if value == "" || value == "-" || value == "—" || strings.EqualFold(value, "null") || strings.EqualFold(value, "undefined") {
		return nil
	}
	return &value
}

type drawObservationQuote struct {
	companyID, company      string
	opening, latest         *string
	openingOdds, latestOdds []*string
}

func drawObservationQuoteRow(row map[string]json.RawMessage) drawObservationQuote {
	return drawObservationQuote{companyID: drawRawID(row["companyId"]), company: drawRawString(row["companyName"]), opening: drawObservationRawLine(row["firstPankou"]), latest: drawObservationRawLine(row["pankou"]), openingOdds: drawObservationRawOdds(row["firstOdds"], 2), latestOdds: drawObservationRawOdds(row["odds"], 2)}
}

func drawObservationRawOdds(raw json.RawMessage, n int) []*string {
	values := make([]*string, n)
	var source []json.RawMessage
	if json.Unmarshal(raw, &source) == nil {
		for i := 0; i < n && i < len(source); i++ {
			values[i] = drawObservationRawLine(source[i])
		}
	}
	return values
}

func drawObservationQuotes(raw []byte, market string) []drawObservationQuote {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		var payload map[string]json.RawMessage
		if json.Unmarshal(raw, &payload) != nil || json.Unmarshal(payload[market], &rows) != nil {
			return nil
		}
	}
	quotes := make([]drawObservationQuote, 0, len(rows))
	for _, row := range rows {
		quotes = append(quotes, drawObservationQuoteRow(row))
	}
	return quotes
}

func drawObservationMarketRows(raw []byte, market string) ([]drawObservationQuote, bool) {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) == nil && strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		return drawObservationQuotes(raw, market), true
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) == nil {
		if value, present := payload[market]; present {
			return drawObservationQuotes(value, market), true
		}
	}
	return nil, false
}

func drawObservationMarket(preferred []byte, rows []drawObservationQuote) drawObservationLine {
	line := drawObservationLine{Source: "missing", OpeningOdds: make([]*string, 2), LatestOdds: make([]*string, 2)}
	var selected *drawObservationQuote
	var row map[string]json.RawMessage
	if json.Unmarshal(preferred, &row) == nil {
		quote := drawObservationQuoteRow(row)
		if quote.opening != nil || quote.latest != nil {
			if quote.company == "" && quote.companyID == "" {
				quote.company = "Bet365"
			}
			selected = &quote
		}
	}
	if selected == nil {
		for i := range rows {
			quote := &rows[i]
			if quote.opening == nil && quote.latest == nil {
				continue
			}
			if quote.companyID == "8" || quote.companyID == "281" || strings.Contains(strings.ToLower(quote.company), "bet365") {
				selected = quote
				break
			}
		}
	}
	if selected == nil {
		for i := range rows {
			if rows[i].opening != nil || rows[i].latest != nil {
				selected = &rows[i]
				break
			}
		}
	}
	if selected != nil {
		line.Opening, line.Latest, line.Company, line.Source = selected.opening, selected.latest, selected.company, "pankou_moneys"
		line.OpeningOdds, line.LatestOdds = selected.openingOdds, selected.latestOdds
		if line.Company == "" && selected.companyID != "" {
			line.Company = "公司 " + selected.companyID
		}
	}
	return line
}

func drawObservationLines(pankou models.PankouMoney) (drawObservationLine, drawObservationLine) {
	asia, asianPresent := drawObservationMarketRows(pankou.AsiaData, "asia")
	var totals []drawObservationQuote
	totalPresent := false
	// Only object payloads can supply a second market; a bare Asian array
	// must never become the totals array.
	var combined map[string]json.RawMessage
	if json.Unmarshal(pankou.AsiaData, &combined) == nil {
		if raw, present := combined["dxq"]; present {
			totals, totalPresent = drawObservationQuotes(raw, "dxq"), true
		}
	}
	if !totalPresent {
		totals, totalPresent = drawObservationMarketRows(pankou.DxqData, "dxq")
	}
	asianPreferred, totalPreferred := []byte(pankou.Bet365Asia), []byte(pankou.Bet365Dxq)
	// The crawler refreshes whole payloads without updating old dedicated
	// columns. Even an explicitly empty new market is authoritative.
	if asianPresent {
		asianPreferred = nil
	}
	if totalPresent {
		totalPreferred = nil
	}
	ah := drawObservationMarket(asianPreferred, asia)
	gl := drawObservationMarket(totalPreferred, totals)
	ah.CapturedAt, gl.CapturedAt = pankou.CapturedAt, pankou.CapturedAt
	return ah, gl
}

func drawObservationEuro(odds models.OddsMoney) drawObservationEuroOdds {
	result := drawObservationEuroOdds{Source: "missing", Opening: make([]*string, 3), Latest: make([]*string, 3), CapturedAt: odds.CapturedAt}
	type euroQuote struct {
		company, id     string
		opening, latest []*string
	}
	parse := func(row map[string]json.RawMessage) *euroQuote {
		quote := &euroQuote{company: drawRawString(row["companyName"]), id: drawRawID(row["companyId"]), opening: drawObservationRawOdds(row["firstOdds"], 3), latest: drawObservationRawOdds(row["odds"], 3)}
		if strings.Contains(quote.company, "平均") {
			return nil
		}
		for _, values := range [][]*string{quote.opening, quote.latest} {
			for _, value := range values {
				if value != nil {
					return quote
				}
			}
		}
		return nil
	}
	var selected *euroQuote
	var rows []map[string]json.RawMessage
	present := false
	if json.Unmarshal(odds.Data, &rows) == nil && strings.HasPrefix(strings.TrimSpace(string(odds.Data)), "[") {
		present = true
	} else {
		var payload map[string]json.RawMessage
		if json.Unmarshal(odds.Data, &payload) == nil {
			if raw, exists := payload["odds"]; exists {
				present = true
				_ = json.Unmarshal(raw, &rows)
			}
		}
	}
	if present {
		var first *euroQuote
		for _, row := range rows {
			quote := parse(row)
			if quote == nil || quote.company == "" && quote.id == "" {
				continue
			}
			if first == nil {
				first = quote
			}
			if quote.id == "8" || quote.id == "281" || strings.Contains(strings.ToLower(quote.company), "bet365") {
				selected = quote
				break
			}
		}
		if selected == nil {
			selected = first
		}
	} else {
		for _, source := range []struct {
			raw     []byte
			company string
		}{{odds.Bet365, "Bet365"}, {odds.William, "William"}, {odds.Pinnacle, "Pinnacle"}} {
			var preferred map[string]json.RawMessage
			if json.Unmarshal(source.raw, &preferred) == nil {
				selected = parse(preferred)
			}
			if selected != nil {
				if selected.company == "" && selected.id == "" {
					selected.company = source.company
				}
				break
			}
		}
	}
	if selected != nil {
		result.Company, result.Opening, result.Latest, result.Source = selected.company, selected.opening, selected.latest, "odds_moneys"
		if result.Company == "" && selected.id != "" {
			result.Company = "公司 " + selected.id
		}
	}
	return result
}

func buildDrawObservations(data drawObservationData, q drawObservationQuery) drawObservationResponse {
	q = normalizeDrawObservationQuery(q)
	q.search = strings.ToLower(strings.TrimSpace(q.search))
	response := drawObservationResponse{ScheduleTimezone: drawSourceLocation.String(), Mode: q.mode, Scope: q.scope, Date: q.date, Page: q.page, PageSize: q.size, Leagues: []string{}, Items: []drawObservationItem{}, Notes: []string{"比赛列表及总数仅来自 money/moneys 主比赛表，按比赛 ID 去重，当前表优先。", "日期按北京时间源站赛程日筛选，默认当天；开赛时间以标准 UTC 时间传输。", "今日模式列出所选赛程日全部比赛；历史模式仅列出主表常规时间完赛平局。", "竞彩范围仅按主比赛表非空竞彩编号筛选；双方历史输入保留所有赛事。", "双方战绩仅作为近 10 场输入，按该场开赛前已完赛记录统计，不扩展为列表比赛。", "赔率、盘口与水位保留单家公司原文，缺失留空，不跨公司补齐。"}}
	if response.Page < 1 {
		response.Page = 1
	}
	if response.PageSize < 1 {
		response.PageSize = 50
	}
	if response.PageSize > 200 {
		response.PageSize = 200
	}
	if data.summary != nil {
		response.Summary = *data.summary
	}
	windowFor := data.window
	if windowFor == nil {
		idx := indexDrawObservations(data.History)
		windowFor = idx.windows
	}
	marketCache := map[string]drawObservationQuotesBundle{}
	leagues := map[string]bool{}
	selected := []drawObservationTarget{}
	filterCounts := q.homeDraws.enabled() || q.awayDraws.enabled() || q.h2hDraws.enabled()
	windowsByID := map[string]drawObservationWindows{}
	for _, target := range data.Targets {
		m := target.Match
		if !drawObservationScopeMatches(m, q.scope) {
			continue
		}
		if data.summary == nil {
			response.Summary.HistoricalMatches++
		}
		hg, ag, complete := drawRegulationResult(m)
		isDraw := complete && hg == ag
		if isDraw && data.summary == nil {
			response.Summary.DrawMatches++
		}
		date := drawObservationDate(m)
		if !drawObservationDateMatches(date, q) || q.mode == "history" && !isDraw {
			continue
		}
		coverage, found := data.coverage[m.MatchId]
		if data.coverage == nil {
			ah, gl := drawObservationLines(data.Pankou[m.MatchId])
			marketCache[m.MatchId] = drawObservationQuotesBundle{asian: ah, total: gl}
			coverage, found = drawObservationCoverage{asian: ah.Source != "missing", total: gl.Source != "missing"}, true
		}
		if found && coverage.asian {
			response.Summary.AsianMatches++
		}
		if found && coverage.total {
			response.Summary.TotalMatches++
		}
		league := drawObservationLeague(m)
		leagues[league] = true
		if q.league != "" && q.league != "all" && league != q.league {
			continue
		}
		if q.search != "" && !strings.Contains(strings.ToLower(m.Home+" "+m.Guest+" "+m.MatchId), q.search) {
			continue
		}
		if filterCounts {
			windows := windowFor(drawModelMatch(m))
			if !q.homeDraws.matches(windows.home) || !q.awayDraws.matches(windows.away) || !q.h2hDraws.matches(windows.h2h) {
				continue
			}
			windowsByID[m.MatchId] = windows
		}
		selected = append(selected, target)
	}
	for league := range leagues {
		response.Leagues = append(response.Leagues, league)
	}
	sort.Strings(response.Leagues)
	sort.SliceStable(selected, func(i, j int) bool {
		a, b := drawSourceTime(selected[i].Match.MatchTime), drawSourceTime(selected[j].Match.MatchTime)
		if a.Equal(b) {
			return selected[i].Match.MatchId < selected[j].Match.MatchId
		}
		return a.Before(b)
	})
	response.Total = len(selected)
	offset := len(selected)
	if response.Page-1 <= len(selected)/response.PageSize {
		offset = (response.Page - 1) * response.PageSize
	}
	end := offset + response.PageSize
	if end > len(selected) {
		end = len(selected)
	}
	for _, target := range selected[offset:end] {
		m := target.Match
		g := drawModelMatch(m)
		windows, cached := windowsByID[g.ID]
		if !cached {
			windows = windowFor(g)
		}
		home, away := windows.home, windows.away
		markets, loaded := marketCache[g.ID]
		if !loaded {
			markets = drawObservationEmptyQuotes()
		}
		ah, gl := markets.asian, markets.total
		warnings := []string{}
		warnings = append(warnings, windows.warnings...)
		if home.Sample < 10 {
			warnings = append(warnings, fmt.Sprintf("主队赛前可核对记录仅 %d 场", home.Sample))
		}
		if away.Sample < 10 {
			warnings = append(warnings, fmt.Sprintf("客队赛前可核对记录仅 %d 场", away.Sample))
		}
		_, _, regulationSettled := drawRegulationResult(m)
		response.Items = append(response.Items, drawObservationItem{MatchID: g.ID, JingcaiID: m.JingcaiID, Date: drawObservationDate(m), MatchTime: g.Kickoff, League: drawObservationLeague(m), Home: m.Home, Guest: m.Guest, HomeScore: m.HomeScore, AwayScore: m.GuestScore, Status: m.Status, DisplayState: m.DisplayState, Settled: drawCompleted(m), RegulationSettled: &regulationSettled, HomeRecent: home, AwayRecent: away, HeadToHead: windows.h2h, AsianHandicap: ah, GoalsLine: gl, EuroOdds: drawObservationEuro(data.Odds[g.ID]), Sources: append([]string{}, target.Sources...), Warnings: warnings})
	}
	return response
}

func drawObservationDate(m models.Money) string {
	if !m.Date.IsZero() {
		return m.Date.Format("2006-01-02")
	}
	if m.MatchTime.IsZero() {
		return ""
	}
	return drawSourceTime(m.MatchTime).In(drawSourceLocation).Format("2006-01-02")
}

func drawObservationLeague(m models.Money) string {
	if strings.TrimSpace(m.League) != "" {
		return drawLeagueName(m.League)
	}
	return drawLeagueName(m.LeagueName)
}
