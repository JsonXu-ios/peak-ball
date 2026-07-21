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

const evilCultDimensionTitle = "19. 邪修一推/二推/反向推（口径=H5 platform.evilCult，由 go_server 结算）"

const evilCultDimensionDefinition = "桥接 go_server /analysis/accuracy-stats 的邪修正确率：每场完赛按大小球线、精确球数、比分、胜平负四类各结算一次，综合=四类合计。" +
	"小球组/追大组=邪修两套原始方向；一推=首轮评分高的一侧；二推(主推)=反诱导修正后的最终主推；反向推=与二推相反。" +
	"统计窗口由 go_server 固定（2026-05-28 起全部完赛），不随本页日期范围过滤，暂无逐场明细。"

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
		StartDate    string              `json:"startDate"`
		EndDate      string              `json:"endDate"`
		Total        int                 `json:"total"`
		EvilCultRows []evilCultBridgeRow `json:"evilCultRows"`
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

	buckets := make([]gin.H, 0, len(payload.EvilCultRows)*len(strategies))
	headlineMatched, headlineHit := 0, 0
	for _, row := range payload.EvilCultRows {
		// 维度头条用"综合·二推(主推)"——邪修的最终主推表现。
		if row.Label == "综合" {
			headlineMatched = row.Sample
			headlineHit = row.MainCorrect
		}
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
				"matches":  []statisticsDetail{},
			})
		}
	}

	headlineAccuracy := 0.0
	if headlineMatched > 0 {
		headlineAccuracy = math.Round(float64(headlineHit)/float64(headlineMatched)*10000) / 100
	}
	definition := evilCultDimensionDefinition + fmt.Sprintf(" 当前窗口 %s ~ %s，完赛基数 %d 场；头条数字为 综合·二推(主推)。",
		payload.StartDate, payload.EndDate, payload.Total)

	return gin.H{
		"key": "evil_cult", "title": evilCultDimensionTitle, "definition": definition,
		"matched": headlineMatched, "hit": headlineHit, "miss": headlineMatched - headlineHit,
		"accuracy": headlineAccuracy,
		"buckets":  buckets,
	}
}
