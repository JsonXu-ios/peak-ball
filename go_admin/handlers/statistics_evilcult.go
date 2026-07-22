// Package handlers: statistics_evilcult.go is dimension 19 — 邪修一推/二推/反向推.
//
// 邪修评分（先小/追大双向评分 + 反诱导二推 + 反向推）在 go_server 的
// platform.evilCult 里实现（600+ 行，含综合均值/回归修正/盘口升降/水位/近期压力
// 多路信号）。为避免两套口径漂移，这里不做移植，而是桥接 go_server 已经结算好的
// /analysis/accuracy-stats（其 evilCultRows 就是 H5 统计页的"邪修正确率"表），
// 与 analysis_rule.go 使用 AnalysisAPIBaseURL 的方式一致。
package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"go_admin/config"

	"github.com/gin-gonic/gin"
)

type evilCultBridgeRow struct {
	Label          string `json:"label"`
	Sample         int    `json:"sample"`
	UnderCorrect   int    `json:"underCorrect"`
	OverCorrect    int    `json:"overCorrect"`
	FirstCorrect   int    `json:"firstCorrect"`
	MainCorrect    int    `json:"mainCorrect"`
	ReverseCorrect int    `json:"reverseCorrect"`
}

// evilCultBridgeMatch 是 go_server 输出的邪修【球数】口径逐场行。
type evilCultBridgeMatch struct {
	MatchID        string `json:"matchId"`
	Date           string `json:"date"`
	MatchTime      string `json:"matchTime"`
	League         string `json:"league"`
	Home           string `json:"home"`
	Guest          string `json:"guest"`
	HomeLogo       string `json:"homeLogo"`
	GuestLogo      string `json:"guestLogo"`
	HomeScore      int    `json:"homeScore"`
	GuestScore     int    `json:"guestScore"`
	ActualTotal    int    `json:"actualTotal"`
	UnderValue     int    `json:"underValue"`
	OverValue      int    `json:"overValue"`
	FirstDirection string `json:"firstDirection"`
	MainDirection  string `json:"mainDirection"`
	UnderHit       bool   `json:"underHit"`
	OverHit        bool   `json:"overHit"`
	FirstHit       bool   `json:"firstHit"`
	MainHit        bool   `json:"mainHit"`
	ReverseHit     bool   `json:"reverseHit"`
}

// evilCultStrategyPickHit 返回某策略在该场的预测球数与是否命中。
func evilCultStrategyPickHit(name string, m evilCultBridgeMatch) (int, bool) {
	switch name {
	case "小球组":
		return m.UnderValue, m.UnderHit
	case "追大组":
		return m.OverValue, m.OverHit
	case "一推":
		if m.FirstDirection == "under" {
			return m.UnderValue, m.FirstHit
		}
		return m.OverValue, m.FirstHit
	case "二推(主推)":
		if m.MainDirection == "under" {
			return m.UnderValue, m.MainHit
		}
		return m.OverValue, m.MainHit
	default: // 反向推 = 与二推相反的一侧
		if m.MainDirection == "under" {
			return m.OverValue, m.ReverseHit
		}
		return m.UnderValue, m.ReverseHit
	}
}

// evilCultStrategyDetails 把逐场行转成某策略的明细列表。
func evilCultStrategyDetails(name string, rows []evilCultBridgeMatch) []statisticsDetail {
	details := make([]statisticsDetail, 0, len(rows))
	for _, m := range rows {
		pick, hit := evilCultStrategyPickHit(name, m)
		details = append(details, statisticsDetail{
			MatchID: m.MatchID, Date: m.Date, MatchTime: m.MatchTime, League: m.League,
			Home: m.Home, Guest: m.Guest, HomeLogo: m.HomeLogo, GuestLogo: m.GuestLogo,
			HomeScore: m.HomeScore, GuestScore: m.GuestScore, State: "完",
			Pick:   fmt.Sprintf("%d球", pick),
			Result: fmt.Sprintf("%d球", m.ActualTotal),
			Hit:    hit,
		})
	}
	return details
}

const evilCultDimensionTitle = "18. 邪修一推/二推/反向推（口径=H5 platform.evilCult，由 go_server 结算）"

const evilCultDimensionDefinition = "桥接 go_server /analysis/accuracy-stats 的邪修正确率，只取【球数】（精确球数）口径。" +
	"小球组/追大组=邪修两套原始方向；一推=首轮评分高的一侧；二推(主推)=反诱导修正后的最终主推；反向推=与二推相反。" +
	"统计窗口由 go_server 固定（2026-05-28 起全部完赛），不随本页日期范围过滤；点分档「查看」可看逐场明细。"

func evilCultDimensionFailed(reason string) gin.H {
	return gin.H{
		"key": "evil_cult", "title": evilCultDimensionTitle,
		"definition": evilCultDimensionDefinition + " 本次取数失败（需 go_server 在线）：" + reason,
		"matched":    0, "hit": 0, "miss": 0, "accuracy": 0.0,
		"buckets": []gin.H{},
	}
}

// buildEvilCultSignals fetches the settled evil-cult accuracy from go_server
// and reshapes it into the statistics dimension payload.
func buildEvilCultSignals() gin.H {
	endpoint := strings.TrimRight(config.AnalysisAPIBaseURL, "/") + "/analysis/accuracy-stats?scope=all"
	// go_server 端会对整个窗口逐场重建分析，允许较长耗时（本维度只在手动重算时触发）。
	client := http.Client{Timeout: 180 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return evilCultDimensionFailed(err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return evilCultDimensionFailed(err.Error())
	}
	if resp.StatusCode >= 400 {
		return evilCultDimensionFailed(fmt.Sprintf("HTTP %d", resp.StatusCode))
	}

	var payload struct {
		StartDate           string                `json:"startDate"`
		EndDate             string                `json:"endDate"`
		Total               int                   `json:"total"`
		EvilCultRows        []evilCultBridgeRow   `json:"evilCultRows"`
		EvilCultGoalMatches []evilCultBridgeMatch `json:"evilCultGoalMatches"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return evilCultDimensionFailed(err.Error())
	}
	if len(payload.EvilCultRows) == 0 {
		return evilCultDimensionFailed("go_server 未返回邪修结算行")
	}

	strategies := []struct {
		name    string
		correct func(evilCultBridgeRow) int
	}{
		{"小球组", func(row evilCultBridgeRow) int { return row.UnderCorrect }},
		{"追大组", func(row evilCultBridgeRow) int { return row.OverCorrect }},
		{"一推", func(row evilCultBridgeRow) int { return row.FirstCorrect }},
		{"二推(主推)", func(row evilCultBridgeRow) int { return row.MainCorrect }},
		{"反向推", func(row evilCultBridgeRow) int { return row.ReverseCorrect }},
	}

	// 只保留【球数】（精确球数）口径的一行；综合/大小球/比分/胜平负 都不再展示，
	// 避免出现"综合=四类合计"导致符合场次是完赛基数4倍的迷惑数字。
	buckets := make([]gin.H, 0, len(strategies))
	headlineMatched, headlineHit := 0, 0
	for _, row := range payload.EvilCultRows {
		if row.Label != "球数" {
			continue
		}
		headlineMatched = row.Sample
		headlineHit = row.MainCorrect
		for _, strategy := range strategies {
			hit := strategy.correct(row)
			accuracy := 0.0
			if row.Sample > 0 {
				accuracy = math.Round(float64(hit)/float64(row.Sample)*10000) / 100
			}
			buckets = append(buckets, gin.H{
				"key":        "evil-" + row.Label + "-" + strategy.name,
				"title":      row.Label + "·" + strategy.name,
				"definition": "",
				"matched":    row.Sample, "hit": hit, "miss": row.Sample - hit,
				"accuracy": accuracy,
				"matches":  evilCultStrategyDetails(strategy.name, payload.EvilCultGoalMatches),
			})
		}
	}
	if len(buckets) == 0 {
		return evilCultDimensionFailed("go_server 未返回【球数】口径的邪修结算行")
	}

	headlineAccuracy := 0.0
	if headlineMatched > 0 {
		headlineAccuracy = math.Round(float64(headlineHit)/float64(headlineMatched)*10000) / 100
	}
	definition := evilCultDimensionDefinition + fmt.Sprintf(" 当前窗口 %s ~ %s，完赛基数 %d 场；头条数字为 球数·二推(主推)。",
		payload.StartDate, payload.EndDate, payload.Total)

	return gin.H{
		"key": "evil_cult", "title": evilCultDimensionTitle, "definition": definition,
		"matched": headlineMatched, "hit": headlineHit, "miss": headlineMatched - headlineHit,
		"accuracy": headlineAccuracy,
		"buckets":  buckets,
	}
}
