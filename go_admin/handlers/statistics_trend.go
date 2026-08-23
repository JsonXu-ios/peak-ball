// Package handlers: statistics_trend.go 趋势球——两队各自最近 N 场的进球/丢球走势。
//
// 数据来源优先级：history_moneys.recent_*_list（采集器给的近期战绩，固定 10 场）；
// 该字段自 2026-05-27 之后基本为空，因此缺失时退回用 moneys 表里本库已完赛的
// 比赛按球队自拼。两种来源都只取开赛时间之前的比赛，避免把未来信息算进走势。
package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	trendDefaultN = 5
	trendMaxN     = 10
	// 比赛层面的大小球倾向阈值：两队近期场均总球数
	trendOverLine  = 3.0
	trendUnderLine = 2.0
	// 单队攻击走势：后段均值 - 前段均值 超过该值才算上升/下降
	trendShiftEpsilon = 0.5

	trendMoneysColumns = "match_id, date, match_time, home, guest, home_team_id, guest_team_id, home_score, guest_score, home_half_score, guest_half_score, status, display_state, league, league_name, home_logo, guest_logo"
)

// trendRecord 某支球队视角下的一场已完赛比赛。
type trendRecord struct {
	MatchID      string `json:"match_id"`
	Date         string `json:"date"`
	MatchTime    string `json:"match_time"`
	League       string `json:"league"`
	Opponent     string `json:"opponent"`
	IsHome       bool   `json:"is_home"`
	Score        string `json:"score"`
	Scored       int    `json:"scored"`
	Conceded     int    `json:"conceded"`
	HalfScored   int    `json:"half_scored"`
	HalfConceded int    `json:"half_conceded"`
	Result       string `json:"result"` // 胜/平/负
}

type trendSummary struct {
	AvgScored     float64 `json:"avg_scored"`
	AvgConceded   float64 `json:"avg_conceded"`
	AvgTotal      float64 `json:"avg_total"`
	CleanSheets   int     `json:"clean_sheets"`
	FailedToScore int     `json:"failed_to_score"`
	Over25        int     `json:"over25"`
	Wins          int     `json:"wins"`
	Draws         int     `json:"draws"`
	Losses        int     `json:"losses"`
	AttackTrend   string  `json:"attack_trend"`  // 上升/持平/下降/样本不足
	DefenseTrend  string  `json:"defense_trend"` // 丢球走势：转好/持平/转差/样本不足
}

type trendTeam struct {
	TeamID  int64         `json:"team_id"`
	Name    string        `json:"name"`
	Logo    string        `json:"logo"`
	Sample  int           `json:"sample"`
	Want    int           `json:"want"`
	Source  string        `json:"source"` // history / self / none
	List    []trendRecord `json:"list"`
	Summary trendSummary  `json:"summary"`
}

type trendMatchReport struct {
	MatchID     string    `json:"match_id"`
	Date        string    `json:"date"`
	MatchTime   string    `json:"match_time"`
	League      string    `json:"league"`
	State       string    `json:"state"`
	Settled     bool      `json:"settled"`
	Home        trendTeam `json:"home"`
	Guest       trendTeam `json:"guest"`
	CombinedAvg float64   `json:"combined_avg"` // 两队近期场均总球数的均值
	Tag         string    `json:"tag"`          // 大球倾向/小球倾向/中性/样本不足
	Sufficient  bool      `json:"sufficient"`   // 两队样本都达到 N
	// 回测字段（仅已完赛）
	HomeScore  int   `json:"home_score"`
	GuestScore int   `json:"guest_score"`
	TotalGoals int   `json:"total_goals"`
	ActualOver bool  `json:"actual_over"`
	Hit        *bool `json:"hit"` // 倾向标签是否命中（中性/样本不足为 nil）
}

// trendMatch 是 moneys 一行的解析结果（在 statisticsMatch 基础上补球队 id 与半场）。
type trendMatch struct {
	statisticsMatch
	Status                        int
	HomeTeamID, GuestTeamID       int64
	HomeHalfScore, GuestHalfScore int
	SortKey                       string
}

// 采集源 status 含义：-1 完场、0 未开始、1~4 进行中，≤-10 为推迟/腰斩/取消。
func (m trendMatch) Abandoned() bool { return m.Status <= -10 }

func parseTrendMatch(row map[string]interface{}) trendMatch {
	match := trendMatch{
		statisticsMatch: parseStatisticsMatch(row),
		Status:          int(statisticsNumber(statisticsValue(row, "status"))),
		HomeTeamID:      int64(statisticsNumber(statisticsValue(row, "home_team_id", "homeTeamId"))),
		GuestTeamID:     int64(statisticsNumber(statisticsValue(row, "guest_team_id", "guestTeamId"))),
		HomeHalfScore:   int(statisticsNumber(statisticsValue(row, "home_half_score", "homeHalfScore"))),
		GuestHalfScore:  int(statisticsNumber(statisticsValue(row, "guest_half_score", "guestHalfScore"))),
	}
	match.SortKey = trendSortKey(match.Date, match.MatchTime)
	return match
}

// trendSortKey 用「日期 时:分」比较先后；没有时间的按当天 00:00。
func trendSortKey(date, matchTime string) string {
	if len(matchTime) >= 16 {
		return matchTime[:16]
	}
	return date + " 00:00"
}

func trendResult(scored, conceded int) string {
	switch {
	case scored > conceded:
		return "胜"
	case scored < conceded:
		return "负"
	}
	return "平"
}

// trendRecordFor 把一场完赛换算成 teamID 视角的记录；teamID 不在该场里返回 false。
func trendRecordFor(match trendMatch, teamID int64) (trendRecord, bool) {
	var rec trendRecord
	switch teamID {
	case match.HomeTeamID:
		rec = trendRecord{Opponent: match.Guest, IsHome: true, Scored: match.HomeScore, Conceded: match.GuestScore, HalfScored: match.HomeHalfScore, HalfConceded: match.GuestHalfScore}
	case match.GuestTeamID:
		rec = trendRecord{Opponent: match.Home, IsHome: false, Scored: match.GuestScore, Conceded: match.HomeScore, HalfScored: match.GuestHalfScore, HalfConceded: match.HomeHalfScore}
	default:
		return rec, false
	}
	rec.MatchID = match.ID
	rec.Date = match.Date
	rec.MatchTime = match.MatchTime
	rec.League = match.League
	rec.Score = strconv.Itoa(match.HomeScore) + "-" + strconv.Itoa(match.GuestScore)
	rec.Result = trendResult(rec.Scored, rec.Conceded)
	return rec, true
}

// trendHistoryRecords 解析 history_moneys 的 recent_*_list 为 teamID 视角的记录。
// 条目里没有 homeId/guestId 时按队名匹配。
func trendHistoryRecords(value interface{}, teamID int64, teamName string) []trendRecord {
	items, _ := statisticsJSON(value).([]interface{})
	records := make([]trendRecord, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		goals := statisticsNumbers(statisticsValue(row, "goal", "score"))
		if len(goals) < 2 {
			continue
		}
		half := statisticsNumbers(statisticsValue(row, "halfGoal", "half_goal"))
		for len(half) < 2 {
			half = append(half, 0)
		}
		homeID := int64(statisticsNumber(statisticsValue(row, "homeId", "home_id")))
		guestID := int64(statisticsNumber(statisticsValue(row, "guestId", "guest_id")))
		home := statisticsText(statisticsValue(row, "home"))
		guest := statisticsText(statisticsValue(row, "guest"))
		isHome := false
		switch {
		case teamID != 0 && homeID == teamID:
			isHome = true
		case teamID != 0 && guestID == teamID:
			isHome = false
		case teamName != "" && home == teamName:
			isHome = true
		case teamName != "" && guest == teamName:
			isHome = false
		default:
			continue
		}
		date := statisticsDate(statisticsValue(row, "matchTime", "match_time", "date"))
		rec := trendRecord{
			MatchID:   statisticsText(statisticsValue(row, "matchId", "match_id")),
			Date:      date,
			MatchTime: statisticsDateTime(statisticsValue(row, "matchTime", "match_time", "date")),
			League:    statisticsText(statisticsValue(row, "league")),
			Score:     strconv.Itoa(int(goals[0])) + "-" + strconv.Itoa(int(goals[1])),
			IsHome:    isHome,
		}
		if isHome {
			rec.Opponent, rec.Scored, rec.Conceded, rec.HalfScored, rec.HalfConceded = guest, int(goals[0]), int(goals[1]), int(half[0]), int(half[1])
		} else {
			rec.Opponent, rec.Scored, rec.Conceded, rec.HalfScored, rec.HalfConceded = home, int(goals[1]), int(goals[0]), int(half[1]), int(half[0])
		}
		rec.Result = trendResult(rec.Scored, rec.Conceded)
		records = append(records, rec)
	}
	return records
}

// trendLastN 取 before 之前的最近 n 场，按时间升序返回。
func trendLastN(records []trendRecord, before string, n int) []trendRecord {
	filtered := make([]trendRecord, 0, len(records))
	for _, rec := range records {
		if trendSortKey(rec.Date, rec.MatchTime) < before {
			filtered = append(filtered, rec)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return trendSortKey(filtered[i].Date, filtered[i].MatchTime) < trendSortKey(filtered[j].Date, filtered[j].MatchTime)
	})
	if len(filtered) > n {
		filtered = filtered[len(filtered)-n:]
	}
	return filtered
}

func trendShift(values []int, up, flat, down string) string {
	if len(values) < 4 {
		return "样本不足"
	}
	split := len(values) / 2
	head, tail := 0.0, 0.0
	for _, v := range values[:split] {
		head += float64(v)
	}
	for _, v := range values[split:] {
		tail += float64(v)
	}
	diff := tail/float64(len(values)-split) - head/float64(split)
	switch {
	case diff >= trendShiftEpsilon:
		return up
	case diff <= -trendShiftEpsilon:
		return down
	}
	return flat
}

func trendSummarize(list []trendRecord) trendSummary {
	var s trendSummary
	if len(list) == 0 {
		s.AttackTrend, s.DefenseTrend = "样本不足", "样本不足"
		return s
	}
	scored := make([]int, 0, len(list))
	conceded := make([]int, 0, len(list))
	for _, rec := range list {
		scored = append(scored, rec.Scored)
		conceded = append(conceded, rec.Conceded)
		s.AvgScored += float64(rec.Scored)
		s.AvgConceded += float64(rec.Conceded)
		if rec.Conceded == 0 {
			s.CleanSheets++
		}
		if rec.Scored == 0 {
			s.FailedToScore++
		}
		if rec.Scored+rec.Conceded > 2 {
			s.Over25++
		}
		switch rec.Result {
		case "胜":
			s.Wins++
		case "平":
			s.Draws++
		default:
			s.Losses++
		}
	}
	n := float64(len(list))
	s.AvgScored = statisticsRound(s.AvgScored/n, 2)
	s.AvgConceded = statisticsRound(s.AvgConceded/n, 2)
	s.AvgTotal = statisticsRound(s.AvgScored+s.AvgConceded, 2)
	s.AttackTrend = trendShift(scored, "上升", "持平", "下降")
	s.DefenseTrend = trendShift(conceded, "转差", "持平", "转好")
	return s
}

func statisticsRound(value float64, digits int) float64 {
	pow := 1.0
	for i := 0; i < digits; i++ {
		pow *= 10
	}
	if value >= 0 {
		return float64(int64(value*pow+0.5)) / pow
	}
	return float64(int64(value*pow-0.5)) / pow
}

// trendIndex 按球队 id 汇总本库所有完赛记录。
func trendIndex(matches []trendMatch) map[int64][]trendRecord {
	index := map[int64][]trendRecord{}
	for _, match := range matches {
		if !match.Settled {
			continue
		}
		for _, teamID := range []int64{match.HomeTeamID, match.GuestTeamID} {
			if teamID == 0 {
				continue
			}
			if rec, ok := trendRecordFor(match, teamID); ok {
				index[teamID] = append(index[teamID], rec)
			}
		}
	}
	return index
}

func trendBuildTeam(teamID int64, name, logo string, historyList interface{}, index map[int64][]trendRecord, before string, n int) trendTeam {
	team := trendTeam{TeamID: teamID, Name: name, Logo: logo, Want: n, Source: "none", List: []trendRecord{}}
	if list := trendLastN(trendHistoryRecords(historyList, teamID, name), before, n); len(list) > 0 {
		team.List, team.Source = list, "history"
	} else if list := trendLastN(index[teamID], before, n); len(list) > 0 {
		team.List, team.Source = list, "self"
	}
	team.Sample = len(team.List)
	team.Summary = trendSummarize(team.List)
	return team
}

func trendTag(home, guest trendTeam, n int) (float64, string, bool) {
	sufficient := home.Sample >= n && guest.Sample >= n
	if home.Sample == 0 || guest.Sample == 0 {
		return 0, "样本不足", false
	}
	combined := statisticsRound((home.Summary.AvgTotal+guest.Summary.AvgTotal)/2, 2)
	if !sufficient {
		return combined, "样本不足", false
	}
	switch {
	case combined >= trendOverLine:
		return combined, "大球倾向", true
	case combined <= trendUnderLine:
		return combined, "小球倾向", true
	}
	return combined, "中性", true
}

// buildTrendReport 对 targets 中每场比赛生成两队走势。
func buildTrendReport(targets []trendMatch, index map[int64][]trendRecord, historyByMatch map[string]map[string]interface{}, n int) []trendMatchReport {
	reports := make([]trendMatchReport, 0, len(targets))
	for _, match := range targets {
		history := historyByMatch[match.ID]
		var homeHist, guestHist interface{}
		if history != nil {
			homeHist = statisticsValue(history, "recent_home_list", "recentHomeList")
			guestHist = statisticsValue(history, "recent_guest_list", "recentGuestList")
		}
		home := trendBuildTeam(match.HomeTeamID, match.Home, match.HomeLogo, homeHist, index, match.SortKey, n)
		guest := trendBuildTeam(match.GuestTeamID, match.Guest, match.GuestLogo, guestHist, index, match.SortKey, n)
		combined, tag, sufficient := trendTag(home, guest, n)
		report := trendMatchReport{
			MatchID: match.ID, Date: match.Date, MatchTime: match.MatchTime, League: match.League, State: match.State, Settled: match.Settled,
			Home: home, Guest: guest, CombinedAvg: combined, Tag: tag, Sufficient: sufficient,
		}
		if match.Settled {
			report.HomeScore, report.GuestScore = match.HomeScore, match.GuestScore
			report.TotalGoals = match.HomeScore + match.GuestScore
			report.ActualOver = report.TotalGoals > 2
			switch tag {
			case "大球倾向":
				hit := report.ActualOver
				report.Hit = &hit
			case "小球倾向":
				hit := !report.ActualOver
				report.Hit = &hit
			}
		}
		reports = append(reports, report)
	}
	sort.SliceStable(reports, func(i, j int) bool {
		if reports[i].SortKeyOf() != reports[j].SortKeyOf() {
			return reports[i].SortKeyOf() < reports[j].SortKeyOf()
		}
		return reports[i].MatchID < reports[j].MatchID
	})
	return reports
}

func (r trendMatchReport) SortKeyOf() string { return trendSortKey(r.Date, r.MatchTime) }

// computeTrendStatistics mode=upcoming：未完赛且日期 ≥ start（默认今天）；
// mode=history：已完赛且日期在 [start,end]（默认最近 7 天）。
func computeTrendStatistics(mode, start, end string, n int) (gin.H, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(trendMoneysColumns).Find(&rawMatches).Error; err != nil {
		return nil, err
	}
	all := make([]trendMatch, 0, len(rawMatches))
	for _, row := range rawMatches {
		match := parseTrendMatch(row)
		if match.ID == "" || match.Date == "" {
			continue
		}
		all = append(all, match)
	}
	index := trendIndex(all)

	today := time.Now().Format("2006-01-02")
	if mode == "history" {
		if end == "" {
			end = today
		}
		if start == "" {
			start = time.Now().AddDate(0, 0, -7).Format("2006-01-02")
		}
	} else if start == "" {
		start = today
	}
	targets := make([]trendMatch, 0)
	ids := make([]string, 0)
	for _, match := range all {
		if mode == "history" {
			if !match.Settled || match.Date < start || match.Date > end {
				continue
			}
		} else if match.Settled || match.Abandoned() || match.Date < start || (end != "" && match.Date > end) {
			continue
		}
		targets = append(targets, match)
		ids = append(ids, match.ID)
	}
	historyByMatch := loadStatisticsRows("history_moneys", "match_id, recent_home_list, recent_guest_list", ids)
	reports := buildTrendReport(targets, index, historyByMatch, n)

	summary := gin.H{
		"total":      len(reports),
		"sufficient": 0,
		"over":       gin.H{"count": 0, "hit": 0},
		"under":      gin.H{"count": 0, "hit": 0},
		"neutral":    0,
	}
	var sufficient, overCount, overHit, underCount, underHit, neutral int
	for _, report := range reports {
		if report.Sufficient {
			sufficient++
		}
		switch report.Tag {
		case "大球倾向":
			overCount++
			if report.Hit != nil && *report.Hit {
				overHit++
			}
		case "小球倾向":
			underCount++
			if report.Hit != nil && *report.Hit {
				underHit++
			}
		case "中性":
			neutral++
		}
	}
	summary["sufficient"] = sufficient
	summary["neutral"] = neutral
	summary["over"] = gin.H{"count": overCount, "hit": overHit, "accuracy": statisticsRate(overHit, overCount)}
	summary["under"] = gin.H{"count": underCount, "hit": underHit, "accuracy": statisticsRate(underHit, underCount)}

	return gin.H{
		"mode":         mode,
		"n":            n,
		"start_date":   start,
		"end_date":     end,
		"settled_pool": len(index),
		"summary":      summary,
		"matches":      reports,
		"generated_at": time.Now().Format(time.RFC3339),
	}, nil
}

func statisticsRate(hit, total int) float64 {
	if total == 0 {
		return 0
	}
	return statisticsRound(float64(hit)*100/float64(total), 2)
}

// GetTrendStatistics 趋势球：GET /statistics/trend?mode=upcoming|history&start_date=&end_date=&n=5
// 只扫一张 moneys 表，计算很快，不走快照，每次实时算。
func GetTrendStatistics(c *gin.Context) {
	start, end, err := statisticsDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 YYYY-MM-DD"})
		return
	}
	mode := strings.TrimSpace(c.Query("mode"))
	if mode != "history" {
		mode = "upcoming"
	}
	n := trendDefaultN
	if raw := strings.TrimSpace(c.Query("n")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			n = parsed
		}
	}
	if n > trendMaxN {
		n = trendMaxN
	}
	report, err := computeTrendStatistics(mode, start, end, n)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}
