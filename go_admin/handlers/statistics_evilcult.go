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
	// 一推/二推方向层（大小球口径）：各自方向对应的盘口线 与 方向是否判对。
	FirstGoalLine     float64 `json:"firstGoalLine"`
	FirstDirectionHit bool    `json:"firstDirectionHit"`
	MainGoalLine      float64 `json:"mainGoalLine"`
	MainDirectionHit  bool    `json:"mainDirectionHit"`
}

func evilCultBaseDetail(m evilCultBridgeMatch, goalLine float64) statisticsDetail {
	return statisticsDetail{
		MatchID: m.MatchID, Date: m.Date, MatchTime: m.MatchTime, League: m.League,
		Home: m.Home, Guest: m.Guest, HomeLogo: m.HomeLogo, GuestLogo: m.GuestLogo,
		HomeScore: m.HomeScore, GuestScore: m.GuestScore, State: "完",
		Line: statisticsFormatLine(goalLine),
	}
}

// evilCultFirstExactDetail 一推球数层明细：一推方向给出的精确球数预测的结算。
func evilCultFirstExactDetail(m evilCultBridgeMatch) statisticsDetail {
	pick := m.OverValue
	if m.FirstDirection == "under" {
		pick = m.UnderValue
	}
	detail := evilCultBaseDetail(m, m.FirstGoalLine)
	detail.Pick = fmt.Sprintf("%d球", pick)
	detail.Result = fmt.Sprintf("%d球", m.ActualTotal)
	detail.Hit = m.FirstHit
	return detail
}

// evilCultMainExactDetail 二推球数层明细：二推(主推)方向给出的精确球数预测的结算。
func evilCultMainExactDetail(m evilCultBridgeMatch) statisticsDetail {
	pick := m.OverValue
	if m.MainDirection == "under" {
		pick = m.UnderValue
	}
	detail := evilCultBaseDetail(m, m.MainGoalLine)
	detail.Pick = fmt.Sprintf("%d球", pick)
	detail.Result = fmt.Sprintf("%d球", m.ActualTotal)
	detail.Hit = m.MainHit
	return detail
}

const evilCultDimensionTitle = "18. 邪修一推/二推（大小球方向对 → 球数，由 go_server 结算）"

const evilCultDimensionDefinition = "桥接 go_server /analysis/accuracy-stats 的邪修一推与二推(主推)，双组合只找对的：" +
	"先按该推方向对大小球盘口线结算（推荐大球=总进球>线，推荐小球=<线），方向判对的场次才纳入；" +
	"在此基础上统计该推精确球数命中率（命中=实际总进球恰好等于预测球数），一推/二推 × 大球/小球 各一行。" +
	"明细盘口列为该推方向对应的大小球线。统计窗口由 go_server 固定（2026-05-28 起全部完赛），不随本页日期范围过滤。"

func evilCultDimensionFailed(reason string) gin.H {
	return gin.H{
		"key": "evil_cult", "title": evilCultDimensionTitle,
		"definition": evilCultDimensionDefinition + " 本次取数失败（需 go_server 在线）：" + reason,
		"matched":    0, "hit": 0, "miss": 0, "accuracy": 0.0,
		"matches": []statisticsDetail{},
	}
}

// evilCultAccuracyPayload 是 go_server /analysis/accuracy-stats 的邪修部分。
type evilCultAccuracyPayload struct {
	StartDate           string                `json:"startDate"`
	EndDate             string                `json:"endDate"`
	Total               int                   `json:"total"`
	EvilCultRows        []evilCultBridgeRow   `json:"evilCultRows"`
	EvilCultGoalMatches []evilCultBridgeMatch `json:"evilCultGoalMatches"`
}

// fetchEvilCultAccuracy 拉取 go_server 已结算的邪修正确率与逐场行（统计页与
// 推荐引擎共用）。go_server 端会对整个窗口逐场重建分析，允许较长耗时。
func fetchEvilCultAccuracy() (*evilCultAccuracyPayload, error) {
	endpoint := strings.TrimRight(config.AnalysisAPIBaseURL, "/") + "/analysis/accuracy-stats?scope=all"
	client := http.Client{Timeout: 180 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	payload := &evilCultAccuracyPayload{}
	if err := json.Unmarshal(body, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// recommendEvilPred 是一场比赛的邪修一推/二推预测（方向、各自大小球线、精确球数），
// 供推荐引擎使用。
type recommendEvilPred struct {
	FirstDirection string
	FirstLine      float64
	FirstValue     int
	MainDirection  string
	MainLine       float64
	MainValue      int
}

// evilCultPredFromBridge 由已结算逐场行还原预测（重算结算用）。
func evilCultPredFromBridge(m evilCultBridgeMatch) recommendEvilPred {
	pred := recommendEvilPred{
		FirstDirection: m.FirstDirection, FirstLine: m.FirstGoalLine, FirstValue: m.OverValue,
		MainDirection: m.MainDirection, MainLine: m.MainGoalLine, MainValue: m.OverValue,
	}
	if m.FirstDirection == "under" {
		pred.FirstValue = m.UnderValue
	}
	if m.MainDirection == "under" {
		pred.MainValue = m.UnderValue
	}
	return pred
}

// fetchEvilCultPredictions 按日期从 go_server /analysis/matches 拉取邪修预测
//（供推荐引擎评估待赛比赛）。任何一天失败都跳过，不阻塞页面。
func fetchEvilCultPredictions(dates []string) map[string]recommendEvilPred {
	out := map[string]recommendEvilPred{}
	client := http.Client{Timeout: 60 * time.Second}
	base := strings.TrimRight(config.AnalysisAPIBaseURL, "/")
	for _, date := range dates {
		resp, err := client.Get(base + "/analysis/matches?scope=all&date=" + date)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode >= 400 {
			continue
		}
		var items []struct {
			MatchID  string `json:"matchId"`
			Platform *struct {
				EvilCult struct {
					Prediction struct {
						FirstDirection  string  `json:"firstDirection"`
						GoalDirection   string  `json:"goalDirection"`
						UnderGoalLine   float64 `json:"underGoalLine"`
						OverGoalLine    float64 `json:"overGoalLine"`
						UnderTotalValue int     `json:"underTotalValue"`
						OverTotalValue  int     `json:"overTotalValue"`
					} `json:"prediction"`
				} `json:"evilCult"`
			} `json:"platform"`
		}
		if json.Unmarshal(body, &items) != nil {
			continue
		}
		for _, item := range items {
			if item.MatchID == "" || item.Platform == nil {
				continue
			}
			p := item.Platform.EvilCult.Prediction
			pred := recommendEvilPred{
				FirstDirection: p.FirstDirection, FirstLine: p.OverGoalLine, FirstValue: p.OverTotalValue,
				MainDirection: p.GoalDirection, MainLine: p.OverGoalLine, MainValue: p.OverTotalValue,
			}
			if p.FirstDirection == "under" {
				pred.FirstLine, pred.FirstValue = p.UnderGoalLine, p.UnderTotalValue
			}
			if p.GoalDirection == "under" {
				pred.MainLine, pred.MainValue = p.UnderGoalLine, p.UnderTotalValue
			}
			out[item.MatchID] = pred
		}
	}
	return out
}

// buildEvilCultSignals fetches the settled evil-cult accuracy from go_server
// and reshapes it into the statistics dimension payload.
func buildEvilCultSignals() gin.H {
	payload, err := fetchEvilCultAccuracy()
	if err != nil {
		return evilCultDimensionFailed(err.Error())
	}
	if len(payload.EvilCultRows) == 0 {
		return evilCultDimensionFailed("go_server 未返回邪修结算行")
	}

	if len(payload.EvilCultGoalMatches) == 0 {
		return evilCultDimensionFailed("go_server 未返回逐场行（需重启 go_server 到新版本后重算）")
	}

	// 一推/二推各两条：推荐大球+盘口判对 / 推荐小球+盘口判对 的场次为基数，
	// 在此基础上结算各自的精确球数命中率；方向判错的场次不纳入。
	firstOver, firstUnder := &statisticsSignal{}, &statisticsSignal{}
	mainOver, mainUnder := &statisticsSignal{}, &statisticsSignal{}
	for _, m := range payload.EvilCultGoalMatches {
		if m.FirstDirectionHit {
			if m.FirstDirection == "under" {
				firstUnder.add(evilCultFirstExactDetail(m))
			} else {
				firstOver.add(evilCultFirstExactDetail(m))
			}
		}
		if m.MainDirectionHit {
			if m.MainDirection == "under" {
				mainUnder.add(evilCultMainExactDetail(m))
			} else {
				mainOver.add(evilCultMainExactDetail(m))
			}
		}
	}

	buckets := []gin.H{
		firstOver.payload("evil-first-over-exact", "一推·推荐大球方向对 → 球数", ""),
		firstUnder.payload("evil-first-under-exact", "一推·推荐小球方向对 → 球数", ""),
		mainOver.payload("evil-main-over-exact", "二推·推荐大球方向对 → 球数", ""),
		mainUnder.payload("evil-main-under-exact", "二推·推荐小球方向对 → 球数", ""),
	}

	// 头条 = 一推方向判对的场次里，一推球数的整体命中率（二推看分组行）。
	matched := len(firstOver.details) + len(firstUnder.details)
	hit := firstOver.hit + firstUnder.hit
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	definition := evilCultDimensionDefinition + fmt.Sprintf(" 当前窗口 %s ~ %s，完赛基数 %d 场；一推纳入 %d 场，二推纳入 %d 场。",
		payload.StartDate, payload.EndDate, payload.Total, matched,
		len(mainOver.details)+len(mainUnder.details))

	return gin.H{
		"key": "evil_cult", "title": evilCultDimensionTitle, "definition": definition,
		"matched": matched, "hit": hit, "miss": matched - hit,
		"accuracy": accuracy,
		"buckets":  buckets,
	}
}
