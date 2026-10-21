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
	"go_server/database"
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
	Company    string     `json:"company"`
	Opening    []*string  `json:"opening"`
	Latest     []*string  `json:"latest"`
	Source     string     `json:"source"`
	CapturedAt *time.Time `json:"capturedAt"`
}

type drawObservationItem struct {
	MatchID       string                  `json:"matchId"`
	Date          string                  `json:"date"`
	MatchTime     time.Time               `json:"matchTime"`
	League        string                  `json:"league"`
	Home          string                  `json:"home"`
	Guest         string                  `json:"guest"`
	HomeScore     int                     `json:"homeScore"`
	AwayScore     int                     `json:"awayScore"`
	Status        int                     `json:"status"`
	DisplayState  string                  `json:"displayState"`
	Settled       bool                    `json:"settled"`
	HomeRecent    drawObservationRecent   `json:"homeRecent"`
	AwayRecent    drawObservationRecent   `json:"awayRecent"`
	AsianHandicap drawObservationLine     `json:"asianHandicap"`
	GoalsLine     drawObservationLine     `json:"goalsLine"`
	EuroOdds      drawObservationEuroOdds `json:"euroOdds"`
	Sources       []string                `json:"sources"`
	Warnings      []string                `json:"warnings"`
}

type drawObservationSummary struct {
	HistoricalMatches int `json:"historicalMatches"`
	DrawMatches       int `json:"drawMatches"`
	AsianMatches      int `json:"asianMatches"`
	TotalMatches      int `json:"totalMatches"`
}

type drawObservationResponse struct {
	Mode     string                 `json:"mode"`
	Date     string                 `json:"date"`
	Summary  drawObservationSummary `json:"summary"`
	Total    int                    `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"pageSize"`
	Leagues  []string               `json:"leagues"`
	Items    []drawObservationItem  `json:"items"`
	Notes    []string               `json:"notes"`
}

type drawObservationQuery struct {
	mode, date                 string
	start, end, league, search string
	page, size                 int
}

func parseDrawObservationQuery(c *gin.Context) (drawObservationQuery, error) {
	q := drawObservationQuery{mode: c.DefaultQuery("mode", "today"), date: c.DefaultQuery("date", time.Now().UTC().Format("2006-01-02")), start: c.Query("start_date"), end: c.Query("end_date"), league: c.Query("league"), search: strings.ToLower(strings.TrimSpace(c.Query("search"))), page: 1, size: 50}
	if q.mode != "today" && q.mode != "history" {
		return q, fmt.Errorf("mode 应为 today 或 history")
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
	return q, nil
}

// GetDrawObservations reads raw local history on each request. It does not
// depend on a historical job, frozen forecast, learned model or saved report.
func GetDrawObservations(c *gin.Context) {
	q, err := parseDrawObservationQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	data, err := loadDrawObservationData()
	if err != nil {
		drawHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, buildDrawObservations(data, q))
}

type drawObservationTarget struct {
	Match   models.Money
	Sources []string
}

type drawObservationData struct {
	Targets []drawObservationTarget
	History []drawHistoricalGame
	Odds    map[string]models.OddsMoney
	Pankou  map[string]models.PankouMoney
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

func loadDrawObservationData() (drawObservationData, error) {
	data := drawObservationData{Odds: map[string]models.OddsMoney{}, Pankou: map[string]models.PankouMoney{}}
	var current, legacy []models.Money
	if err := database.DB.Find(&current).Error; err != nil {
		return data, err
	}
	if database.DB.Migrator().HasTable("money") {
		if err := database.DB.Table("money").Find(&legacy).Error; err != nil {
			return data, err
		}
	}
	data.Targets = mergeDrawObservationTargets(current, legacy)
	known, registry := map[string]models.Money{}, map[string]drawHistoricalGame{}
	for _, target := range data.Targets {
		m := target.Match
		known[m.MatchId] = m
		if hg, ag, ok := drawRegulationResult(m); ok {
			g := drawModelMatch(m)
			g.Completed, g.HomeGoals, g.AwayGoals = true, hg, ag
			g.CompletedAt = g.Kickoff.Add(2 * time.Hour)
			registry[g.ID] = drawHistoricalGame{Game: g, Sources: target.Sources}
		}
	}
	var histories []models.HistoryMoney
	if err := database.DB.Find(&histories).Error; err != nil {
		return data, err
	}
	for _, history := range histories {
		for _, game := range drawHistoryGames(history, known[history.MatchId]) {
			if m, exists := known[game.ID]; exists {
				if _, _, valid := drawRegulationResult(m); !valid {
					continue
				}
			}
			key := game.ID
			if key == "" {
				key = fmt.Sprintf("fixture:%s|%s|%d", game.HomeTeamName, game.AwayTeamName, game.Kickoff.UnixNano())
			}
			if _, exists := registry[key]; !exists {
				registry[key] = drawHistoricalGame{Game: game, Sources: []string{"history_moneys"}}
			}
		}
	}
	for _, game := range registry {
		data.History = append(data.History, game)
	}
	var odds []models.OddsMoney
	var pankou []models.PankouMoney
	if err := database.DB.Find(&odds).Error; err != nil {
		return data, err
	}
	if err := database.DB.Find(&pankou).Error; err != nil {
		return data, err
	}
	for _, quote := range odds {
		data.Odds[quote.MatchId] = quote
	}
	for _, quote := range pankou {
		data.Pankou[quote.MatchId] = quote
	}
	return data, nil
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
	return idx
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
	pool := append([]drawmodel.Match{}, idx.byID[id]...)
	pool = append(pool, idx.byName[normalized]...)
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
		fallback = true
		return true
	}
	sort.SliceStable(pool, func(i, j int) bool {
		if pool[i].Kickoff.Equal(pool[j].Kickoff) {
			return pool[i].ID < pool[j].ID
		}
		return pool[i].Kickoff.After(pool[j].Kickoff)
	})
	seen := map[string]bool{}
	for _, g := range pool {
		if !g.Completed || g.Kickoff.IsZero() || !g.Kickoff.Before(target.Kickoff) || g.ID != "" && g.ID == target.ID || g.HomeGoals < 0 || g.AwayGoals < 0 {
			continue
		}
		finished := g.CompletedAt
		if finished.IsZero() || !finished.After(g.Kickoff) {
			finished = g.Kickoff.Add(2 * time.Hour)
		}
		if !finished.Before(target.Kickoff) {
			continue
		}
		home, away := identity(g.HomeTeamID, g.HomeTeamName), identity(g.AwayTeamID, g.AwayTeamName)
		if !home && !away {
			continue
		}
		key := g.ID
		if key == "" {
			key = fmt.Sprintf("%s|%s|%s|%s|%d", g.HomeTeamID, g.HomeTeamName, g.AwayTeamID, g.AwayTeamName, g.Kickoff.UnixNano())
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		venue, gf, ga := "home", g.HomeGoals, g.AwayGoals
		if !home {
			venue, gf, ga = "away", g.AwayGoals, g.HomeGoals
		}
		result := "draw"
		if gf > ga {
			result = "win"
		} else if gf < ga {
			result = "loss"
		}
		if result == "draw" {
			recent.Draws++
		}
		recent.Matches = append(recent.Matches, drawObservationRecentMatch{MatchID: g.ID, Date: g.Kickoff.In(drawSourceLocation).Format("2006-01-02"), MatchTime: g.Kickoff, League: g.LeagueName, Home: g.HomeTeamName, Guest: g.AwayTeamName, HomeScore: g.HomeGoals, AwayScore: g.AwayGoals, Venue: venue, Result: result})
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
	q.search = strings.ToLower(strings.TrimSpace(q.search))
	if q.mode == "" {
		q.mode = "today"
	}
	if q.date == "" {
		q.date = time.Now().UTC().Format("2006-01-02")
	}
	response := drawObservationResponse{Mode: q.mode, Date: q.date, Page: q.page, PageSize: q.size, Leagues: []string{}, Items: []drawObservationItem{}, Notes: []string{"比赛列表及总数仅来自 money/moneys 主比赛表，按比赛 ID 去重，当前表优先。", "今日模式列出所选赛程日全部比赛；历史模式仅列出主表常规时间完赛平局。", "双方战绩仅作为近 10 场输入，按该场开赛前已完赛记录统计，不扩展为列表比赛。", "赔率、盘口与水位保留单家公司原文，缺失留空，不跨公司补齐。"}}
	if response.Page < 1 {
		response.Page = 1
	}
	if response.PageSize < 1 {
		response.PageSize = 50
	}
	if response.PageSize > 200 {
		response.PageSize = 200
	}
	response.Summary.HistoricalMatches = len(data.Targets)
	idx := indexDrawObservations(data.History)
	leagues := map[string]bool{}
	selected := []drawObservationTarget{}
	for _, target := range data.Targets {
		m := target.Match
		hg, ag, complete := drawRegulationResult(m)
		isDraw := complete && hg == ag
		if isDraw {
			response.Summary.DrawMatches++
		}
		date := drawObservationDate(m)
		if q.mode == "today" && date != q.date || q.mode == "history" && !isDraw {
			continue
		}
		ah, gl := drawObservationLines(data.Pankou[m.MatchId])
		if ah.Source != "missing" {
			response.Summary.AsianMatches++
		}
		if gl.Source != "missing" {
			response.Summary.TotalMatches++
		}
		league := drawObservationLeague(m)
		leagues[league] = true
		if q.start != "" && date < q.start || q.end != "" && date > q.end || q.league != "" && q.league != "all" && league != q.league {
			continue
		}
		if q.search != "" && !strings.Contains(strings.ToLower(m.Home+" "+m.Guest+" "+m.MatchId), q.search) {
			continue
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
		return a.After(b)
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
		home, hw := idx.recent(g, g.HomeTeamID, g.HomeTeamName)
		away, aw := idx.recent(g, g.AwayTeamID, g.AwayTeamName)
		ah, gl := drawObservationLines(data.Pankou[g.ID])
		warnings := []string{}
		warnings = append(warnings, hw...)
		warnings = append(warnings, aw...)
		if home.Sample < 10 {
			warnings = append(warnings, fmt.Sprintf("主队赛前可核对记录仅 %d 场", home.Sample))
		}
		if away.Sample < 10 {
			warnings = append(warnings, fmt.Sprintf("客队赛前可核对记录仅 %d 场", away.Sample))
		}
		response.Items = append(response.Items, drawObservationItem{MatchID: g.ID, Date: drawObservationDate(m), MatchTime: g.Kickoff, League: drawObservationLeague(m), Home: m.Home, Guest: m.Guest, HomeScore: m.HomeScore, AwayScore: m.GuestScore, Status: m.Status, DisplayState: m.DisplayState, Settled: drawCompleted(m), HomeRecent: home, AwayRecent: away, AsianHandicap: ah, GoalsLine: gl, EuroOdds: drawObservationEuro(data.Odds[g.ID]), Sources: append([]string{}, target.Sources...), Warnings: warnings})
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
