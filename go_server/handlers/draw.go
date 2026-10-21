package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"go_server/database"
	"go_server/internal/drawmodel"
	"go_server/models"

	"github.com/gin-gonic/gin"
	mysqlerr "github.com/go-sql-driver/mysql"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const drawFeatureVersion = "draw-features-v1"

var drawSourceLocation = func() *time.Location {
	name := strings.TrimSpace(os.Getenv("DRAW_SOURCE_TIMEZONE"))
	if name == "" {
		name = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		panic("invalid DRAW_SOURCE_TIMEZONE: " + name)
	}
	return location
}()

var drawLeaguePrefix = regexp.MustCompile(`^周[一二三四五六日天][0-9]+\s*`)

// Experimental policy, intentionally separate from the statistical model.
// Persisted in each payload so changing a threshold cannot rewrite past picks.
var drawPolicy = struct {
	Version        string  `json:"version"`
	MinProbability float64 `json:"minProbability"`
	MinLift        float64 `json:"minLift"`
	Label          string  `json:"label"`
}{"draw-selection-v1", 32, 3, "实验筛选阈值，尚未通过样本外验证"}

type drawFormResponse struct {
	Sample           int      `json:"sample"`
	Draws            int      `json:"draws"`
	DrawRate         *float64 `json:"drawRate"`
	SmoothedDrawRate *float64 `json:"smoothedDrawRate"`
	GoalsFor         *float64 `json:"goalsFor"`
	GoalsAgainst     *float64 `json:"goalsAgainst"`
	LowScoreRate     *float64 `json:"lowScoreRate"`
	Venue            string   `json:"venue"`
}

type drawMarketsResponse struct {
	HomeOdds           *float64   `json:"homeOdds"`
	DrawOdds           *float64   `json:"drawOdds"`
	AwayOdds           *float64   `json:"awayOdds"`
	Handicap           *float64   `json:"handicap"`
	TotalLine          *float64   `json:"totalLine"`
	HomeWater          *float64   `json:"homeWater"`
	AwayWater          *float64   `json:"awayWater"`
	OverWater          *float64   `json:"overWater"`
	UnderWater         *float64   `json:"underWater"`
	ObservedAt         *time.Time `json:"observedAt"`
	Companies          int        `json:"companies"`
	QuoteCompany       string     `json:"quoteCompany"`
	HandicapObservedAt *time.Time `json:"handicapObservedAt"`
}

type drawResultResponse struct {
	Settled   bool  `json:"settled"`
	IsDraw    *bool `json:"isDraw"`
	HomeScore *int  `json:"homeScore"`
	AwayScore *int  `json:"awayScore"`
}

type drawMatchResponse struct {
	MatchID           string           `json:"matchId"`
	League            string           `json:"league"`
	Home              string           `json:"home"`
	Guest             string           `json:"guest"`
	MatchTime         time.Time        `json:"matchTime"`
	Status            string           `json:"status"`
	Probability       *float64         `json:"probability"`
	ModelProbability  *float64         `json:"modelProbability"`
	MarketProbability *float64         `json:"marketProbability"`
	Edge              *float64         `json:"edge"`
	HomeExpectedGoals *float64         `json:"homeExpectedGoals"`
	AwayExpectedGoals *float64         `json:"awayExpectedGoals"`
	Candidate         bool             `json:"candidate"`
	Assessment        string           `json:"assessment"`
	Quality           string           `json:"quality"`
	Warnings          []string         `json:"warnings"`
	Reasons           []string         `json:"reasons"`
	HomeRecent        drawFormResponse `json:"homeRecent"`
	AwayRecent        drawFormResponse `json:"awayRecent"`
	HomeVenue         drawFormResponse `json:"homeVenue"`
	AwayVenue         drawFormResponse `json:"awayVenue"`
	LeagueBaseline    struct {
		Probability float64 `json:"probability"`
		Sample      int     `json:"sample"`
		Source      string  `json:"source"`
	} `json:"leagueBaseline"`
	Scorelines   []drawScoreResponse `json:"scorelines"`
	Markets      drawMarketsResponse `json:"markets"`
	ModelVersion string              `json:"modelVersion"`
	Calibrated   bool                `json:"calibrated"`
	AsOf         time.Time           `json:"asOf"`
	PredictionID *uint64             `json:"predictionId"`
	Frozen       bool                `json:"frozen"`
	Result       drawResultResponse  `json:"result"`
	Policy       any                 `json:"policy"`
	ModelDetails drawmodel.ModelInfo `json:"modelDetails"`
}

type drawScoreResponse struct {
	Score       string  `json:"score"`
	Probability float64 `json:"probability"`
}

type drawBundle struct {
	Matches            []models.Money
	Histories          map[string]models.HistoryMoney
	Odds               map[string]models.OddsMoney
	Pankou             map[string]models.PankouMoney
	Predictions        map[string]models.DrawPrediction
	Past               []drawmodel.Match
	Artifact           *drawmodel.Artifact
	HistoricalArtifact *drawmodel.HistoricalArtifact
}

// GetDrawMatches never backfills a prediction for a match that has started.
// Finished rows without a frozen pre-match prediction deliberately have no probability.
func GetDrawMatches(c *gin.Context) {
	date, err := drawDate(c.Query("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := settleDrawPredictions(); err != nil {
		drawHTTPError(c, err)
		return
	}
	asOf := time.Now()
	bundle, err := loadDrawBundle(date, asOf)
	if err != nil {
		drawHTTPError(c, err)
		return
	}
	items := make([]drawMatchResponse, 0, len(bundle.Matches))
	summary := gin.H{"total": len(bundle.Matches), "candidates": 0, "insufficient": 0, "frozen": 0}
	for _, match := range bundle.Matches {
		item, _, _ := buildDrawItem(match, bundle, asOf)
		if item.Candidate {
			summary["candidates"] = summary["candidates"].(int) + 1
		}
		if item.Assessment == "insufficient" {
			summary["insufficient"] = summary["insufficient"].(int) + 1
		}
		if item.Frozen {
			summary["frozen"] = summary["frozen"].(int) + 1
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if (items[i].Probability == nil) != (items[j].Probability == nil) {
			return items[i].Probability != nil
		}
		if items[i].Probability != nil && *items[i].Probability != *items[j].Probability {
			return *items[i].Probability > *items[j].Probability
		}
		return items[i].MatchTime.Before(items[j].MatchTime)
	})
	version, calibrated := drawVersion(bundle.Artifact), bundle.Artifact != nil && bundle.Artifact.Calibrated
	if bundle.HistoricalArtifact != nil {
		version = drawHistoricalModelVersion(bundle.HistoricalArtifact)
		calibrated = true
	}
	c.JSON(http.StatusOK, gin.H{"date": date, "asOf": asOf, "scheduleTimezone": drawSourceLocation.String(), "modelVersion": version, "calibrated": calibrated, "policy": drawPolicy, "items": items, "summary": summary})
}

var errDrawSkip = errors.New("draw prediction already exists or match unavailable")

// GenerateDrawPredictions freezes the first prediction, including non-candidates.
// This preserves the denominator needed to evaluate selection coverage honestly.
func GenerateDrawPredictions(c *gin.Context) {
	var request struct {
		Date string `json:"date"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请提供比赛日期"})
		return
	}
	date, err := drawDate(request.Date)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	asOf := time.Now().Truncate(time.Millisecond)
	bundle, err := loadDrawBundle(date, asOf)
	if err != nil {
		drawHTTPError(c, err)
		return
	}
	created, skipped := 0, 0
	for _, match := range bundle.Matches {
		item, input, result := buildDrawItem(match, bundle, asOf)
		if item.Frozen || item.Probability == nil || !drawUpcoming(match, time.Now()) {
			skipped++
			continue
		}
		// Keep the original source payloads alongside the standardized model input.
		snapshotData := struct {
			drawmodel.Input
			SourceRecords      any                           `json:"sourceRecords"`
			Policy             any                           `json:"policy"`
			HistoricalArtifact *drawmodel.HistoricalArtifact `json:"historicalArtifact,omitempty"`
		}{input, gin.H{"history": bundle.Histories[match.MatchId], "odds": bundle.Odds[match.MatchId], "pankou": bundle.Pankou[match.MatchId]}, drawPolicy, bundle.HistoricalArtifact}
		inputJSON, marshalErr := json.Marshal(snapshotData)
		if marshalErr != nil {
			drawHTTPError(c, marshalErr)
			return
		}
		err = database.DB.Transaction(func(tx *gorm.DB) error {
			var latest models.Money
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("match_id = ?", match.MatchId).First(&latest).Error; err != nil {
				return err
			}
			if !drawUpcoming(latest, time.Now()) || !latest.MatchTime.Equal(match.MatchTime) {
				return errDrawSkip
			}
			var existing int64
			if err := tx.Model(&models.DrawPrediction{}).Where("match_id = ?", match.MatchId).Count(&existing).Error; err != nil {
				return err
			}
			if existing > 0 {
				return errDrawSkip
			}
			snapshot := models.DrawFeatureSnapshot{MatchID: match.MatchId, AsOf: asOf, Kickoff: item.MatchTime, FeatureVersion: drawFeatureVersion, Input: datatypes.JSON(inputJSON)}
			if err := tx.Create(&snapshot).Error; err != nil {
				return err
			}
			item.Frozen = true
			payload, err := json.Marshal(item)
			if err != nil {
				return err
			}
			prediction := models.DrawPrediction{MatchID: match.MatchId, SnapshotID: snapshot.ID, PredictionAt: asOf, Kickoff: item.MatchTime, ModelVersion: item.ModelVersion, Probability: result.Probability, ModelProbability: result.ScoreModelProbability, MarketProbability: result.MarketProbability, BaselineProbability: result.LeagueBaseline.DrawRate, Candidate: item.Candidate, Quality: item.Quality, Payload: datatypes.JSON(payload)}
			return tx.Create(&prediction).Error
		})
		if errors.Is(err, errDrawSkip) || drawDuplicate(err) {
			skipped++
			continue
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "冻结预测失败，请刷新检查已保存记录", "created": created, "skipped": skipped})
			return
		}
		created++
	}
	c.JSON(http.StatusOK, gin.H{"created": created, "skipped": skipped})
}

func loadDrawBundle(date string, asOf time.Time) (drawBundle, error) {
	b := drawBundle{Histories: map[string]models.HistoryMoney{}, Odds: map[string]models.OddsMoney{}, Pankou: map[string]models.PankouMoney{}, Predictions: map[string]models.DrawPrediction{}}
	if err := database.DB.Where("date = ? AND (display_state IS NULL OR display_state <> ?)", date, detailOnlyDisplayState).Order("match_time ASC").Find(&b.Matches).Error; err != nil {
		return b, err
	}
	artifact, err := loadActiveDrawArtifact()
	if err != nil {
		return b, err
	}
	b.Artifact = artifact
	b.HistoricalArtifact = activeDrawHistoricalArtifact()
	if len(b.Matches) == 0 {
		return b, nil
	}
	ids := make([]string, 0, len(b.Matches))
	leagueIDs, teamIDs, names := []int{}, []int{}, []string{}
	for _, m := range b.Matches {
		ids = append(ids, m.MatchId)
		if m.LeagueId > 0 {
			leagueIDs = append(leagueIDs, m.LeagueId)
		}
		if m.HomeTeamId > 0 {
			teamIDs = append(teamIDs, m.HomeTeamId)
		}
		if m.GuestTeamId > 0 {
			teamIDs = append(teamIDs, m.GuestTeamId)
		}
		if m.League != "" {
			names = append(names, m.League)
		}
	}
	var histories []models.HistoryMoney
	var odds []models.OddsMoney
	var pankou []models.PankouMoney
	var predictions []models.DrawPrediction
	for _, query := range []struct{ dest any }{{&histories}, {&odds}, {&pankou}, {&predictions}} {
		if err := database.DB.Where("match_id IN ?", ids).Find(query.dest).Error; err != nil {
			return b, err
		}
	}
	for _, v := range histories {
		b.Histories[v.MatchId] = v
	}
	for _, v := range odds {
		b.Odds[v.MatchId] = v
	}
	for _, v := range pankou {
		b.Pankou[v.MatchId] = v
	}
	for _, v := range predictions {
		b.Predictions[v.MatchID] = v
	}
	// Bounded historical pool, restricted to relevant leagues and teams. Completion
	// is checked again in Go; cancellation must never become a 0:0 training label.
	var past []models.Money
	q := database.DB.Where("match_time >= ? AND match_time < ?", asOf.AddDate(-2, 0, 0).In(drawSourceLocation).Format("2006-01-02 15:04:05"), asOf.Add(-2*time.Hour).In(drawSourceLocation).Format("2006-01-02 15:04:05"))
	q = q.Where("league_id IN ? OR league IN ? OR home_team_id IN ? OR guest_team_id IN ?", leagueIDs, names, teamIDs, teamIDs)
	if err := q.Order("match_time DESC").Limit(16000).Find(&past).Error; err != nil {
		return b, err
	}
	for _, m := range past {
		if h, a, ok := drawRegulationResult(m); ok {
			game := drawModelMatch(m)
			game.Completed = true
			game.HomeGoals = h
			game.AwayGoals = a
			game.CompletedAt = game.Kickoff.Add(2 * time.Hour)
			b.Past = append(b.Past, game)
		}
	}
	for _, m := range b.Matches {
		b.Past = append(b.Past, drawHistoryGames(b.Histories[m.MatchId], m)...)
	}
	drawHistoryState.RLock()
	b.Past = append(b.Past, drawHistoryState.Pool...)
	drawHistoryState.RUnlock()
	return b, nil
}

func buildDrawItem(match models.Money, b drawBundle, asOf time.Time) (drawMatchResponse, drawmodel.Input, drawmodel.Result) {
	input := drawmodel.Input{Target: drawModelMatch(match), AsOf: asOf, Artifact: b.Artifact}
	item := drawMatchResponse{MatchID: match.MatchId, League: match.League, Home: match.Home, Guest: match.Guest, MatchTime: drawSourceTime(match.MatchTime), Status: match.DisplayState, Assessment: "insufficient", Quality: "insufficient", Warnings: []string{}, Reasons: []string{}, Scorelines: []drawScoreResponse{}, ModelVersion: drawVersion(b.Artifact), AsOf: asOf, Policy: drawPolicy}
	item.HomeRecent.Venue = "all"
	item.AwayRecent.Venue = "all"
	item.HomeVenue.Venue = "home"
	item.AwayVenue.Venue = "away"
	if p, ok := b.Predictions[match.MatchId]; ok {
		if err := json.Unmarshal(p.Payload, &item); err != nil {
			item.Warnings = append(item.Warnings, "已冻结记录无法解析，请检查数据")
			return item, input, drawmodel.Result{}
		}
		item.Frozen = true
		item.PredictionID = &p.ID
		item.Status = match.DisplayState
		item.Result = drawResultResponse{Settled: p.Settled, IsDraw: p.IsDraw, HomeScore: p.HomeScore, AwayScore: p.AwayScore}
		return item, input, drawmodel.Result{}
	}
	if !drawUpcoming(match, asOf) {
		item.Assessment = "closed"
		item.Warnings = append(item.Warnings, "比赛已开始、结束或赛程异常；没有赛前冻结记录，不补算预测")
		if h, a, ok := drawRegulationResult(match); ok {
			d := h == a
			item.Result = drawResultResponse{Settled: true, IsDraw: &d, HomeScore: &h, AwayScore: &a}
		}
		return item, input, drawmodel.Result{}
	}
	input.History = drawRelevantHistory(match, b.Past, asOf)
	input.Odds, item.Markets, item.Warnings = drawMarketData(b.Odds[match.MatchId], b.Pankou[match.MatchId], asOf)
	var result drawmodel.Result
	if b.HistoricalArtifact != nil {
		result = drawmodel.PredictHistorical(input, b.HistoricalArtifact)
		if result.Model.Calibrated {
			item.ModelVersion = drawHistoricalModelVersion(b.HistoricalArtifact)
		}
	} else {
		result = drawmodel.Predict(input)
	}
	item.ModelDetails = result.Model
	item.Calibrated = result.Model.Calibrated
	item.Warnings = append(item.Warnings, result.Warnings...)
	item.Reasons = append(item.Reasons, result.Reasons...)
	item.HomeRecent = drawForm(result.HomeOverall, "all")
	item.AwayRecent = drawForm(result.AwayOverall, "all")
	item.HomeVenue = drawForm(result.HomeVenue, "home")
	item.AwayVenue = drawForm(result.AwayVenue, "away")
	item.LeagueBaseline.Probability = drawPercent(result.LeagueBaseline.DrawRate)
	item.LeagueBaseline.Sample = result.LeagueBaseline.N
	item.LeagueBaseline.Source = result.LeagueBaseline.Source
	if !result.Available {
		return item, input, result
	}
	item.Probability = drawFloat(drawPercent(result.Probability))
	item.ModelProbability = drawFloat(drawPercent(result.ScoreModelProbability))
	item.HomeExpectedGoals = drawFloat(round2(result.HomeLambda))
	item.AwayExpectedGoals = drawFloat(round2(result.AwayLambda))
	if result.MarketProbability != nil {
		item.MarketProbability = drawFloat(drawPercent(*result.MarketProbability))
		item.Edge = drawFloat(round2(100 * (result.Probability - *result.MarketProbability)))
	}
	for _, score := range result.TopScorelines {
		item.Scorelines = append(item.Scorelines, drawScoreResponse{Score: fmt.Sprintf("%d:%d", score.Home, score.Away), Probability: drawPercent(score.Probability)})
	}
	if result.HomeOverall.N < 3 || result.AwayOverall.N < 3 {
		item.Warnings = append(item.Warnings, "任一队整体有效近况不足3场，仅展示基础估计，不进入候选")
		return item, input, result
	}
	item.Quality = "limited"
	item.Assessment = "watch"
	complete := result.HomeOverall.N >= 5 && result.AwayOverall.N >= 5 && result.HomeVenue.N >= 3 && result.AwayVenue.N >= 3 && result.LeagueBaseline.Source == "league" && result.LeagueBaseline.N >= 30 && result.MarketProbability != nil && !result.Model.NameFallback
	if item.Markets.ObservedAt != nil && asOf.Sub(*item.Markets.ObservedAt) > 48*time.Hour {
		complete = false
		item.Warnings = append(item.Warnings, "欧赔采集超过48小时，暂停进入候选")
	}
	if complete {
		item.Quality = "complete"
	}
	item.Candidate = complete && result.Probability >= drawPolicy.MinProbability/100 && result.Probability-result.LeagueBaseline.DrawRate >= drawPolicy.MinLift/100
	if b.HistoricalArtifact != nil && result.Model.Calibrated {
		if selected, ok := b.HistoricalArtifact.Models[b.HistoricalArtifact.SelectedModel]; ok {
			item.Policy = gin.H{"version": "draw-historical-selection-v1", "minProbability": drawPercent(selected.Threshold), "minLift": 0, "label": "筛选阈值仅使用历史校准段确定，独立测试不参与选择"}
			complete = result.HomeOverall.N >= 3 && result.AwayOverall.N >= 3 && !result.Model.NameFallback
			item.Candidate = complete && result.Probability >= selected.Threshold
			if complete {
				item.Quality = "complete"
			}
		}
	}
	if item.Candidate {
		item.Assessment = "candidate"
		if item.Calibrated {
			item.Reasons = append(item.Reasons, "达到由历史校准段确定的概率阈值")
		} else {
			item.Reasons = append(item.Reasons, "达到实验筛选阈值；阈值尚未通过样本外验证")
		}
	}
	if !item.Calibrated {
		item.Warnings = append(item.Warnings, "基础比分模型尚未完成独立概率校准，当前候选属于实验筛选")
	}
	return item, input, result
}

func drawForm(f drawmodel.Form, venue string) drawFormResponse {
	r := drawFormResponse{Sample: f.N, Draws: f.Draws, Venue: venue}
	if f.N > 0 {
		r.DrawRate = drawFloat(drawPercent(f.DrawRate))
		r.SmoothedDrawRate = drawFloat(drawPercent(f.SmoothedDrawRate))
		r.GoalsFor = drawFloat(round2(f.GoalsFor))
		r.GoalsAgainst = drawFloat(round2(f.GoalsAgainst))
		r.LowScoreRate = drawFloat(drawPercent(f.LowScoreRate))
	}
	return r
}

func drawModelMatch(m models.Money) drawmodel.Match {
	return drawmodel.Match{ID: m.MatchId, LeagueID: drawID(m.LeagueId), LeagueName: drawLeagueName(m.League), HomeTeamID: drawID(m.HomeTeamId), AwayTeamID: drawID(m.GuestTeamId), HomeTeamName: m.Home, AwayTeamName: m.Guest, Kickoff: drawSourceTime(m.MatchTime)}
}

// The crawler stores the source's timezone-less wall clock in a DATETIME.
// Reinterpret that clock in the source zone, independent of the server's zone.
func drawSourceTime(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), drawSourceLocation).UTC()
}

func drawLeagueName(name string) string {
	return strings.TrimSpace(drawLeaguePrefix.ReplaceAllString(strings.TrimSpace(name), ""))
}

// Keep the actual prediction input bounded before storing it. Unrelated leagues
// must not inflate every snapshot or crowd the target teams out of recent form.
func drawRelevantHistory(target models.Money, pool []drawmodel.Match, asOf time.Time) []drawmodel.Match {
	ordered := append([]drawmodel.Match{}, pool...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Kickoff.After(ordered[j].Kickoff) })
	result := make([]drawmodel.Match, 0, 1580)
	seen := map[string]bool{}
	leagueN, homeN, awayN := 0, 0, 0
	teamMatches := func(id int, name, otherID, otherName string) bool {
		if id > 0 && otherID != "" {
			return drawID(id) == otherID
		}
		return strings.TrimSpace(name) != "" && strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(otherName))
	}
	for _, game := range ordered {
		if !game.Completed || game.ID == target.MatchId || !game.Kickoff.Before(asOf) || game.CompletedAt.After(asOf) {
			continue
		}
		key := game.ID
		if key == "" {
			key = fmt.Sprintf("%s|%s|%s|%s|%s", game.HomeTeamID, game.HomeTeamName, game.AwayTeamID, game.AwayTeamName, game.Kickoff.Format(time.RFC3339Nano))
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		sameLeague := game.LeagueID != "" && target.LeagueId > 0 && game.LeagueID == drawID(target.LeagueId)
		if game.LeagueID == "" || target.LeagueId <= 0 {
			sameLeague = game.LeagueName != "" && (drawLeagueName(game.LeagueName) == drawLeagueName(target.League) || drawLeagueName(game.LeagueName) == drawLeagueName(target.LeagueName))
		}
		home := teamMatches(target.HomeTeamId, target.Home, game.HomeTeamID, game.HomeTeamName) || teamMatches(target.HomeTeamId, target.Home, game.AwayTeamID, game.AwayTeamName)
		away := teamMatches(target.GuestTeamId, target.Guest, game.HomeTeamID, game.HomeTeamName) || teamMatches(target.GuestTeamId, target.Guest, game.AwayTeamID, game.AwayTeamName)
		keep := (sameLeague && leagueN < 1500) || (home && homeN < 40) || (away && awayN < 40)
		if sameLeague {
			leagueN++
		}
		if home {
			homeN++
		}
		if away {
			awayN++
		}
		if keep {
			result = append(result, game)
		}
	}
	return result
}

// Unknown numeric status codes do not imply completion. Negative states take
// priority even if the source labels a cancelled fixture as ended.
func drawCompleted(m models.Money) bool {
	s := strings.ToLower(strings.TrimSpace(m.DisplayState))
	for _, blocked := range []string{"取消", "推迟", "延期", "腰斩", "中断", "待定", "cancel", "postpon", "abandon", "suspend"} {
		if strings.Contains(s, blocked) {
			return false
		}
	}
	return s == "完场" || s == "已完场" || s == "完赛" || s == "已完赛" || s == "已结束" || s == "结束" || s == "ft" || s == "finished" || s == "全场结束" || s == "比赛结束"
}

func drawUpcoming(m models.Money, asOf time.Time) bool {
	if m.MatchTime.IsZero() || !drawSourceTime(m.MatchTime).After(asOf) {
		return false
	}
	if m.Status != 0 || m.MatchState != 0 || m.HomeScore != 0 || m.GuestScore != 0 || m.HomeOtScore != 0 || m.GuestOtScore != 0 || m.HomeOtPenalty != 0 || m.GuestOtPenalty != 0 {
		return false
	}
	s := strings.TrimSpace(m.DisplayState)
	return s == "" || s == "未开始" || s == "未开赛" || s == "未赛" || s == "待开赛" || strings.EqualFold(s, "scheduled") || strings.EqualFold(s, "not started")
}

func drawMarketData(odds models.OddsMoney, pankou models.PankouMoney, asOf time.Time) (*drawmodel.Odds, drawMarketsResponse, []string) {
	markets := drawMarketsResponse{}
	warnings := []string{}
	var modelOdds *drawmodel.Odds
	if odds.CapturedAt != nil && !odds.CapturedAt.IsZero() && !odds.CapturedAt.After(asOf) {
		rows := euroOddsRows(odds)
		sum := [3]float64{}
		n := 0
		seen := map[string]bool{}
		for _, row := range rows {
			if isAverageOddRow(row) || len(row.Odds) < 3 {
				continue
			}
			key := row.CompanyID
			if key == "" {
				key = row.CompanyName
			}
			if seen[key] {
				continue
			}
			values := [3]float64{parseFloat(row.Odds[0]), parseFloat(row.Odds[1]), parseFloat(row.Odds[2])}
			if !drawValidOdds(values) {
				continue
			}
			seen[key] = true
			total := 1/values[0] + 1/values[1] + 1/values[2]
			for i, v := range values {
				sum[i] += (1 / v) / total
			}
			if n == 0 {
				markets.QuoteCompany = row.CompanyName
				markets.HomeOdds = drawFloat(values[0])
				markets.DrawOdds = drawFloat(values[1])
				markets.AwayOdds = drawFloat(values[2])
			}
			n++
		}
		if n > 0 {
			// Encode the aggregate fair distribution as an equivalent quote. Each
			// bookmaker is de-vigged before averaging; raw sources remain in snapshot.
			modelOdds = &drawmodel.Odds{Home: float64(n) / sum[0], Draw: float64(n) / sum[1], Away: float64(n) / sum[2], CapturedAt: *odds.CapturedAt}
			markets.ObservedAt = odds.CapturedAt
			markets.Companies = n
		} else {
			warnings = append(warnings, "缺少可校验的单公司三项欧赔，市场概率留空")
		}
	} else {
		warnings = append(warnings, "欧赔缺失或无有效采集时间，市场概率留空")
	}
	if pankou.CapturedAt != nil && !pankou.CapturedAt.IsZero() && !pankou.CapturedAt.After(asOf) {
		markets.HandicapObservedAt = pankou.CapturedAt
		asia, total := pankouRows(pankou)
		a := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Asia), asia, 8, 281)
		t := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Dxq), total, 8, 281)
		markets.Handicap = drawLine(a.Pankou, false)
		markets.TotalLine = drawLine(t.Pankou, true)
		if len(a.Odds) >= 2 {
			markets.HomeWater = drawNumeric(a.Odds[0])
			markets.AwayWater = drawNumeric(a.Odds[1])
		}
		if len(t.Odds) >= 2 {
			markets.OverWater = drawNumeric(t.Odds[0])
			markets.UnderWater = drawNumeric(t.Odds[1])
		}
		if markets.Handicap != nil || markets.TotalLine != nil {
			if modelOdds == nil {
				modelOdds = &drawmodel.Odds{CapturedAt: *pankou.CapturedAt}
			} else if pankou.CapturedAt.After(modelOdds.CapturedAt) {
				// All features become available only after the latest source capture.
				modelOdds.CapturedAt = *pankou.CapturedAt
			}
			modelOdds.AsianHandicap = markets.Handicap
			modelOdds.GoalLine = markets.TotalLine
		}
	}
	return modelOdds, markets, warnings
}

func drawValidOdds(v [3]float64) bool {
	for _, x := range v {
		if x <= 1 || math.IsNaN(x) || math.IsInf(x, 0) {
			return false
		}
	}
	return true
}

func drawLine(value string, total bool) *float64 {
	s := strings.TrimSpace(value)
	if s == "" || s == "-" {
		return nil
	}
	if n := drawNumeric(s); n != nil {
		if total && *n <= 0 {
			return nil
		}
		return n
	}
	negative := strings.HasPrefix(s, "受")
	s = strings.TrimPrefix(s, "受")
	words := map[string]float64{"平手": 0, "平": 0, "半球": .5, "半": .5, "一球": 1, "一": 1, "一球半": 1.5, "球半": 1.5, "二球": 2, "两球": 2, "二": 2, "二球半": 2.5, "两球半": 2.5, "三球": 3, "三": 3, "三球半": 3.5, "四球": 4, "四": 4, "四球半": 4.5, "五球": 5, "五球半": 5.5, "六球": 6, "六球半": 6.5}
	if v, ok := words[s]; ok {
		if negative {
			v = -v
		}
		if !total || v > 0 {
			return drawFloat(v)
		}
		return nil
	}
	parts := strings.Split(s, "/")
	if len(parts) == 2 {
		a, b := drawNumeric(parts[0]), drawNumeric(parts[1])
		if a == nil {
			if v, ok := words[parts[0]]; ok {
				a = drawFloat(v)
			}
		}
		if b == nil {
			if v, ok := words[parts[1]]; ok {
				b = drawFloat(v)
			}
		}
		if a != nil && b != nil {
			n := (*a + *b) / 2
			if negative {
				n = -n
			}
			if !total || n > 0 {
				return drawFloat(n)
			}
		}
	}
	return nil
}

func drawNumeric(value string) *float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	return &n
}
func drawFloat(v float64) *float64  { return &v }
func drawPercent(v float64) float64 { return round2(v * 100) }
func drawID(v int) string {
	if v <= 0 {
		return ""
	}
	return strconv.Itoa(v)
}
func drawVersion(a *drawmodel.Artifact) string {
	if a == nil || !a.Fitted {
		return drawmodel.Version
	}
	return a.Version + "-" + a.TrainingCutoff.UTC().Format("20060102T150405Z")
}
func drawDate(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().Format("2006-01-02"), nil
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil || d.Format("2006-01-02") != raw {
		return "", errors.New("日期格式应为 YYYY-MM-DD")
	}
	return raw, nil
}
func drawDuplicate(err error) bool {
	var e *mysqlerr.MySQLError
	return errors.As(err, &e) && e.Number == 1062
}
func drawHTTPError(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "平局数据加载失败：" + err.Error()})
}

// The source uses numeric or string IDs depending on endpoint. Preserve them
// instead of matching a team's display name wherever stable IDs are available.
func drawHistoryGames(history models.HistoryMoney, target models.Money) []drawmodel.Match {
	var arrays []json.RawMessage
	for _, data := range []datatypes.JSON{history.RecentHomeList, history.RecentGuestList, history.AgainstList} {
		if len(data) > 0 && strings.TrimSpace(string(data)) != "[]" && strings.TrimSpace(string(data)) != "null" {
			arrays = append(arrays, json.RawMessage(data))
		}
	}
	if len(arrays) == 0 {
		var payload struct {
			Recent struct {
				Home struct {
					List json.RawMessage `json:"list"`
				} `json:"home"`
				Guest struct {
					List json.RawMessage `json:"list"`
				} `json:"guest"`
			} `json:"recent"`
			Against struct {
				List json.RawMessage `json:"list"`
			} `json:"against"`
		}
		if json.Unmarshal(history.LeagueStat, &payload) == nil {
			arrays = append(arrays, payload.Recent.Home.List, payload.Recent.Guest.List, payload.Against.List)
		}
	}
	games := []drawmodel.Match{}
	for _, data := range arrays {
		var rows []map[string]json.RawMessage
		if json.Unmarshal(data, &rows) != nil {
			continue
		}
		for _, row := range rows {
			if !drawHistoricalRowAllowed(row) {
				continue
			}
			var goals []json.RawMessage
			if json.Unmarshal(row["goal"], &goals) != nil || len(goals) < 2 {
				continue
			}
			h, errH := strconv.Atoi(drawRawString(goals[0]))
			a, errA := strconv.Atoi(drawRawString(goals[1]))
			if errH != nil || errA != nil || h < 0 || a < 0 || h > 30 || a > 30 {
				continue
			}
			kickoff, ok := drawHistoryTime(drawRawString(row["matchTime"]))
			if !ok {
				continue
			}
			leagueName := drawLeagueName(drawRawString(row["league"]))
			leagueID := drawRawID(row["leagueId"])
			if leagueID == "" && (leagueName == drawLeagueName(target.League) || leagueName == drawLeagueName(target.LeagueName)) {
				leagueID = drawID(target.LeagueId)
			}
			games = append(games, drawmodel.Match{ID: drawRawID(row["matchId"]), LeagueID: leagueID, LeagueName: leagueName, HomeTeamID: drawRawID(row["homeId"]), AwayTeamID: drawRawID(row["guestId"]), HomeTeamName: drawRawString(row["home"]), AwayTeamName: drawRawString(row["guest"]), Kickoff: kickoff, CompletedAt: kickoff.Add(2 * time.Hour), HomeGoals: h, AwayGoals: a, Completed: true})
		}
	}
	return games
}

func drawRawID(raw json.RawMessage) string {
	value := drawRawString(raw)
	if value == "" {
		return ""
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n <= 0 {
			return ""
		}
		return strconv.FormatInt(n, 10)
	}
	return value
}

// Recent/against endpoints are completed-match lists. Honor explicit status or
// extra-time metadata whenever provided instead of turning it into a draw label.
func drawHistoricalRowAllowed(row map[string]json.RawMessage) bool {
	state := strings.ToLower(drawRawString(row["displayState"]))
	for _, blocked := range []string{"取消", "推迟", "延期", "腰斩", "中断", "待定", "加时", "点球", "cancel", "postpon", "abandon", "suspend", "aet", "pen"} {
		if strings.Contains(state, blocked) {
			return false
		}
	}
	for _, key := range []string{"homeOtScore", "guestOtScore", "homeOtPenalty", "guestOtPenalty"} {
		if value := drawNumeric(drawRawString(row[key])); value != nil && *value != 0 {
			return false
		}
	}
	return true
}

func drawRawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(string(raw))
}

func drawHistoryTime(raw string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006/01/02 15:04", "2006-01-02", "2006/01/02"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(raw), drawSourceLocation); err == nil {
			// Date-only records cannot establish intraday ordering. Treat them as
			// available only after the end of their calendar date plus match duration.
			if layout == "2006-01-02" || layout == "2006/01/02" {
				t = t.Add(24*time.Hour - time.Second)
			}
			return t, true
		}
	}
	return time.Time{}, false
}
