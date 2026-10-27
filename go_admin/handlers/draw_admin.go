package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"go_admin/config"

	"github.com/gin-gonic/gin"
)

// The admin router authenticates these endpoints. Only the request payload and
// query are forwarded to the analysis service; admin credentials stay here.
var drawAdminClient = &http.Client{
	Timeout: 120 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func proxyDrawAdmin(c *gin.Context, path string, timeout time.Duration) {
	base, err := url.Parse(strings.TrimRight(config.AnalysisAPIBaseURL, "/"))
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "平局分析服务地址配置无效"})
		return
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	base.RawQuery = c.Request.URL.RawQuery
	base.Fragment = ""
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, c.Request.Method, base.String(), c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "无法创建平局分析请求"})
		return
	}
	if contentType := c.GetHeader("Content-Type"); contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	response, err := drawAdminClient.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			c.JSON(http.StatusGatewayTimeout, gin.H{"error": "平局分析计算超时，请稍后重试"})
		} else if c.Request.Context().Err() == nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "平局分析服务暂不可用"})
		}
		return
	}
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	c.Status(response.StatusCode)
	_, _ = io.Copy(c.Writer, response.Body)
}

func GetDrawHistoryAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/draw/history", 120*time.Second)
}

func GetDrawObservationsAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/draw/observations", 120*time.Second)
}

func GetDrawObservationDatesAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/draw/observations/dates", 15*time.Second)
}

func RebuildDrawHistoryAdmin(c *gin.Context) {
	// Add existing analysis inputs at the admin boundary, where the shared
	// catalogue and context builders are available. Never import the historical
	// hit columns: those are labels, not information known before a match.
	var request []byte
	var err error
	if c.Request.Body != nil {
		request, err = io.ReadAll(io.LimitReader(c.Request.Body, 2*1024*1024+1))
	}
	if err != nil || len(request) > 2*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "历史重算请求内容无效"})
		return
	}
	payload := make(map[string]json.RawMessage)
	if len(bytes.TrimSpace(request)) > 0 {
		if err := json.Unmarshal(request, &payload); err != nil || payload == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "历史重算请求应为 JSON 对象"})
			return
		}
	}
	existing, err := loadDrawExistingAnalysis()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取现有分析数据失败"})
		return
	}
	payload["existingAnalysis"], err = json.Marshal(existing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "现有分析数据格式无效"})
		return
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "历史重算请求内容无效"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(encoded))
	c.Request.ContentLength = int64(len(encoded))
	c.Request.Header.Set("Content-Type", "application/json")
	proxyDrawAdmin(c, "/draw/history/rebuild", 120*time.Second)
}

func CalibrateDrawHistoryAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/draw/history/calibrate", 120*time.Second)
}

func GetDrawMatchesAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/draw/matches", 30*time.Second)
}

func GenerateDrawPredictionsAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/draw/generate", 30*time.Second)
}

func GetDrawStatsAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/draw/stats", 30*time.Second)
}

const drawAnalysisProvenance = "analysis_recomputed_unverified"

type drawExistingAnalysisSignal struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Market    string   `json:"market"`
	Direction string   `json:"direction"`
	Value     *float64 `json:"value"`
}

type drawExistingAnalysis struct {
	MatchID    string                       `json:"matchId"`
	Provenance string                       `json:"provenance"`
	Features   map[string]float64           `json:"features"`
	Signals    []drawExistingAnalysisSignal `json:"signals"`
}

func loadDrawExistingAnalysis() ([]drawExistingAnalysis, error) {
	byID := make(map[string]statisticsMatch)
	// The older singular table contains a separate batch of source matches.
	// Modern rows take precedence when the same match exists in both tables.
	for _, table := range []string{"money", "moneys"} {
		if !statisticsDB().Migrator().HasTable(table) {
			continue
		}
		var raw []map[string]interface{}
		if err := statisticsDB().Table(table).Select(statisticsMoneysColumns).Find(&raw).Error; err != nil {
			return nil, fmt.Errorf("load draw analysis matches: %w", err)
		}
		for _, row := range raw {
			m := parseStatisticsMatch(row)
			if m.ID != "" {
				byID[m.ID] = m
			}
		}
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)
	catalogue := recommendCatalogue()
	rows := make([]drawExistingAnalysis, 0, len(ids))
	for _, id := range ids {
		row := buildDrawExistingAnalysis(byID[id], histories[id], pankous[id], odds[id], catalogue)
		if len(row.Features) > 0 || len(row.Signals) > 0 {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func buildDrawExistingAnalysis(match statisticsMatch, history, pankou, odds map[string]interface{}, catalogue []recommendCondition) drawExistingAnalysis {
	// Guard the export boundary against accidental dependence on settlement
	// fields. Only fixture identity/time and source analysis inputs reach ctx.
	match.HomeScore, match.GuestScore, match.Settled, match.State = 0, 0, false, ""
	ctx := buildRecommendCtx(match, history, pankou, odds, nil)
	row := drawExistingAnalysis{
		MatchID: match.ID, Provenance: drawAnalysisProvenance,
		Features: map[string]float64{}, Signals: []drawExistingAnalysisSignal{},
	}
	add := func(key string, value float64, valid bool) {
		if valid && !math.IsNaN(value) && !math.IsInf(value, 0) {
			row.Features[key] = value
		}
	}
	if len(ctx.probabilities) == 3 {
		add("marketHomeProbability", ctx.probabilities[0], true)
		add("marketDrawProbability", ctx.probabilities[1], true)
		add("marketAwayProbability", ctx.probabilities[2], true)
		add("baseProbability", ctx.basePredProb, true)
	}
	add("historyHandicap", ctx.historyDiff, ctx.hasHistory)
	add("recentHandicap", ctx.recentDiff, ctx.hasRecentDiff)
	add("historyGoals", ctx.historyGoals, ctx.hasHistory)
	add("recentGoals", ctx.recentGoals, ctx.hasRecentGls)
	add("handicapConsensus", ctx.hcpConsensus, ctx.hasHcpAgree)
	add("goalConsensus", ctx.goalConsensus, ctx.hasGoalAgree)
	add("handicapOpening", ctx.ahFirst, ctx.hasAsian)
	add("handicapLine", ctx.asianLine, ctx.hasAsian)
	add("totalLine", ctx.dxqLine, ctx.hasDxq)
	add("homeWater", ctx.homeWater, ctx.hasAsianWater)
	add("awayWater", ctx.awayWater, ctx.hasAsianWater)
	add("overWater", ctx.overWater, ctx.hasDxqWater)
	add("underWater", ctx.underWater, ctx.hasDxqWater)
	add("asianHeat", ctx.asianHeat, ctx.hasAsianHeat)
	add("goalsHeat", ctx.goalsHeat, ctx.hasGoalsHeat)
	add("qiuStrength", ctx.qiuStrength, ctx.hasQiu && ctx.qiuDirection != "")
	add("qiuLine", ctx.qiuLine, ctx.hasQiu)
	for _, condition := range catalogue {
		fire := condition.Evaluate(ctx)
		if !fire.fires {
			continue
		}
		direction := fire.direction
		if direction == "" && fire.settle == "choices" {
			choices := []string{}
			for _, dir := range []string{"home", "draw", "away"} {
				if ctx.kellyChoices[dir] {
					choices = append(choices, dir)
				}
			}
			direction = strings.Join(choices, ",")
		}
		if direction == "" {
			direction = "fire"
		}
		row.Signals = append(row.Signals, drawExistingAnalysisSignal{
			Key: condition.Key, Label: condition.Title, Market: condition.Market,
			Direction: direction, Value: drawAnalysisSignalValue(ctx, condition.Key, fire),
		})
	}
	return row
}

func drawAnalysisSignalValue(ctx recommendCtx, key string, fire recommendFire) *float64 {
	var value float64
	switch {
	case key == "history_handicap":
		value = ctx.historyDiff
	case key == "recent_handicap":
		value = ctx.recentDiff
	case key == "asian_composite":
		value, _ = statisticsAverage(ctx.historyDiff, ctx.hasHistory, ctx.recentDiff, ctx.hasRecentDiff, ctx.asianLine, ctx.hasAsian)
	case key == "history_goals":
		value = ctx.historyGoals
	case key == "recent_goals":
		value = ctx.recentGoals
	case key == "goals_composite":
		value, _ = statisticsAverage(ctx.historyGoals, ctx.hasHistory, ctx.recentGoals, ctx.hasRecentGls)
	case strings.HasPrefix(key, "base_prob_"):
		value = ctx.basePredProb
	case strings.HasPrefix(key, "asian_heat_"):
		value = math.Max(ctx.asianHeat, 100-ctx.asianHeat)
	case strings.HasPrefix(key, "goals_heat_"):
		value = math.Max(ctx.goalsHeat, 100-ctx.goalsHeat)
	case strings.HasPrefix(key, "base_qiu_"):
		value = ctx.qiuStrength
	case fire.settle == "cover" || fire.settle == "over":
		value = fire.line
	default:
		return nil
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	return &value
}
