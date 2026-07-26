// Package handlers: statistics_lastdance.go —「最后一舞」客胜命中率统计。
//
// 只保留一张卡片：各信号推荐客胜时的命中率（命中=客胜打出），加上大球推荐
// 的两组。信号口径全部沿用「完赛基础统计」对应维度：
//
//	1  四信号齐备同推客胜：前端主推≥70%、专业信号（凯体同向）、近期状态让球
//	   （净胜球差<0）、亚盘综合均值（期望<-0.25）同时推荐客胜。
//	2~5  单个信号推荐客胜，各自统计。
//	6  近期平均球数与球数综合均值同时高于大小球盘口（同推大球），命中=大球
//	   打出（走盘不计）。
//	7  存在任一客胜推荐（2~5条件之一）且同推大球的场次，命中=大球打出；
//	   明细推荐列标注客胜推荐来源。
//	8~10  交易盈亏/模拟盈亏（舒服项映射）推荐主胜时反买客胜，命中=客胜打出。
//	11~14 任一盈亏推荐主胜（反买客胜）×四个客胜信号的组合，命中=客胜打出。
//	15~16 主胜对照组：四信号齐备同推主胜、前端主推≥70%推荐主胜，命中=主胜打出。
//	17~18 纯反买：交易/模拟盈亏映射推荐的反方向，无其他条件，命中=反方向打出。
package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const lastDanceProbThreshold = 70.0 // 前端主推的最小主推概率

// GetLastDanceStatistics serves the 最后一舞 page. refresh=1 recomputes and
// persists to stat_snapshots; plain loads read the stored snapshot.
func GetLastDanceStatistics(c *gin.Context) {
	if c.Query("refresh") == "1" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "重算正在进行中，请稍候再试"})
			return
		}
		defer statisticsRecomputeMu.Unlock()
		report, err := computeLastDanceStatistics()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		payload, err := json.Marshal(report)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := saveStatSnapshot(snapshotKindLastDance, payload, time.Now()); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
		return
	}

	if payload, _, ok := loadStatSnapshot(snapshotKindLastDance); ok {
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
		return
	}
	c.JSON(http.StatusOK, gin.H{"needs_recompute": true, "settled_total": 0, "signals": []gin.H{}})
}

// computeLastDanceStatistics loads every settled match and settles the away-win
// recommendation card.
func computeLastDanceStatistics() (gin.H, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		return nil, err
	}
	matches := make([]statisticsMatch, 0, len(rawMatches))
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if !match.Settled || match.ID == "" {
			continue
		}
		matches = append(matches, match)
		ids = append(ids, match.ID)
	}

	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)

	return gin.H{
		"settled_total":   len(matches),
		"signals":         []gin.H{buildLastDanceAwayCard(matches, histories, pankous, odds)},
		"generated_at":    time.Now().Format(time.RFC3339),
		"needs_recompute": false,
	}, nil
}

// buildLastDanceAwayCard 逐场比赛结算各客胜推荐分组。
func buildLastDanceAwayCard(matches []statisticsMatch, histories, pankous, odds map[string]map[string]interface{}) gin.H {
	buckets := map[string]*statisticsSignal{}
	covered := map[string]struct{}{}
	order := []string{
		"1. 四信号齐备·同推客胜",
		"2. 前端主推≥70%·推荐客胜",
		"3. 近期状态让球·推荐客胜",
		"4. 专业信号·推荐客胜",
		"5. 亚盘综合均值·推荐客胜",
		"6. 双球数期望＞盘口·同推大球",
		"7. 有客胜推荐＋同推大球",
		"8. 交易盈亏推荐主胜·反买客胜",
		"9. 模拟盈亏推荐主胜·反买客胜",
		"10. 双盈亏同推主胜·反买客胜",
		"11. 盈亏反买客胜×主推≥70%客胜",
		"12. 盈亏反买客胜×近期状态让球客胜",
		"13. 盈亏反买客胜×专业信号客胜",
		"14. 盈亏反买客胜×亚盘综合均值客胜",
		"15. 四信号齐备·同推主胜",
		"16. 前端主推≥70%·推荐主胜",
		"17. 反买交易盈亏（推荐反向·无其他条件）",
		"18. 反买模拟盈亏（推荐反向·无其他条件）",
	}
	file := func(key string, detail statisticsDetail) {
		sig := buckets[key]
		if sig == nil {
			sig = &statisticsSignal{}
			buckets[key] = sig
		}
		sig.add(detail)
		covered[detail.MatchID] = struct{}{}
	}

	for _, match := range matches {
		historyRow := histories[match.ID]
		pankouRow := pankous[match.ID]
		oddsRow := odds[match.ID]
		actual := statisticsActualOutcome(match)

		// 专业信号（凯体同向）主推方向。
		proDir := ""
		if choices := statisticsKellySportteryChoices(oddsRow); len(choices) > 0 {
			for _, dir := range []string{"home", "draw", "away"} {
				if choices[dir] {
					proDir = dir
				}
			}
		}
		// 前端主推方向与概率。
		baseDir, baseProb := "", 0.0
		if probabilities := statisticsProbabilities(oddsRow); len(probabilities) == 3 {
			baseDir, baseProb = "home", probabilities[0]
			if probabilities[1] > baseProb {
				baseDir, baseProb = "draw", probabilities[1]
			}
			if probabilities[2] > baseProb {
				baseDir, baseProb = "away", probabilities[2]
			}
		}
		// 亚盘线、交锋/近况期望、亚盘综合均值。
		_, ahLine, hasAH := statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
		against, homeRecent, guestRecent := statisticsHistory(historyRow)
		historyDiff, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		recentDiff, hasRecentDiff := statisticsRecentDifference(
			statisticsRecentForm(homeRecent, match.Home), statisticsRecentForm(guestRecent, match.Guest))
		composite, hasComposite := statisticsAverage(historyDiff, hasHistory, recentDiff, hasRecentDiff, ahLine, hasAH)
		compDir := ""
		if hasComposite {
			compDir, _ = statisticsOutcomeFromValue(composite, statisticsHandicapBand)
		}
		// 大小球盘口与球数期望。
		ouLine, hasOU := statisticsPankouLine(pankouRow, "bet365_dxq", "dxq_data")
		recentGoals, hasRecentGoals := statisticsRecentGoals(homeRecent, guestRecent)
		goalsComp, hasGoalsComp := statisticsAverage(historyGoals, hasHistory, recentGoals, hasRecentGoals)

		// 交易/模拟盈亏的舒服项映射推荐（舒服项=胜→主胜；平/负→主让判客胜、
		// 主受让判主胜；平手盘在舒服项=平/负时无法判向）。
		mapComfort := func(comfort string) (string, bool) {
			switch comfort {
			case "home":
				return "home", true
			case "draw", "away":
				if !hasAH || ahLine == 0 {
					return "", false
				}
				if ahLine > 0 {
					return "away", true
				}
				return "home", true
			}
			return "", false
		}
		tradePick, simPick := "", ""
		if tradeDir, ok := statisticsBookmakerComfort(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade")); ok {
			if pick, mapped := mapComfort(tradeDir); mapped {
				tradePick = pick
			}
		}
		if simDir, ok := statisticsSimulatedComfort(oddsRow, pankouRow, historyRow, match); ok {
			if pick, mapped := mapComfort(simDir); mapped {
				simPick = pick
			}
		}
		tradeHome := tradePick == "home"
		simHome := simPick == "home"

		// 各信号是否推荐客胜。
		baseAway := baseDir == "away" && baseProb >= lastDanceProbThreshold
		recentAway := hasRecentDiff && recentDiff < 0
		proAway := proDir == "away"
		compAway := compDir == "away"
		awayHit := actual == "away"
		resultLabel := statisticsOutcomeLabel(actual)

		fileAway := func(key, pick string, value float64) {
			detail := statisticsBaseDetail(match)
			detail.Pick = pick
			detail.Value = statisticsRound2(value)
			detail.Result = resultLabel
			detail.Hit = awayHit
			file(key, detail)
		}
		fileHome := func(key, pick string, value float64) {
			detail := statisticsBaseDetail(match)
			detail.Pick = pick
			detail.Value = statisticsRound2(value)
			detail.Result = resultLabel
			detail.Hit = actual == "home"
			file(key, detail)
		}
		// 1. 四信号齐备且同推客胜。
		if baseAway && proAway && recentAway && compAway {
			fileAway("1. 四信号齐备·同推客胜",
				fmt.Sprintf("客胜（主推%.1f%%｜专业｜让球%.2f｜综合%.2f）", baseProb, recentDiff, composite), baseProb)
		}
		// 2~5. 单信号推荐客胜。
		if baseAway {
			fileAway("2. 前端主推≥70%·推荐客胜", "主推:客胜", baseProb)
		}
		if recentAway {
			fileAway("3. 近期状态让球·推荐客胜", "让球:客胜", recentDiff)
		}
		if proAway {
			fileAway("4. 专业信号·推荐客胜", "专业:客胜", 0)
		}
		if compAway {
			fileAway("5. 亚盘综合均值·推荐客胜", "综合:客胜", composite)
		}

		// 6/7. 双球数期望同推大球：近期平均球数与球数综合均值都高于盘口。
		bothOver := hasOU && hasRecentGoals && hasGoalsComp &&
			recentGoals-ouLine >= statisticsPushEpsilon && goalsComp-ouLine >= statisticsPushEpsilon
		if bothOver {
			if over, valid := statisticsOverOutcome(match, ouLine); valid {
				detail := statisticsBaseDetail(match)
				detail.Pick = fmt.Sprintf("判大（近期%.2f/综合%.2f）", recentGoals, goalsComp)
				detail.Value = statisticsRound2(goalsComp)
				detail.Line = statisticsFormatLine(ouLine)
				detail.Result = statisticsOverLabel(over)
				detail.Hit = over
				file("6. 双球数期望＞盘口·同推大球", detail)

				// 7. 同场存在任一客胜推荐时，大球推荐的命中率。
				sources := []string{}
				if baseAway {
					sources = append(sources, "主推")
				}
				if proAway {
					sources = append(sources, "专业")
				}
				if recentAway {
					sources = append(sources, "让球")
				}
				if compAway {
					sources = append(sources, "综合")
				}
				if len(sources) > 0 {
					combo := detail
					combo.Pick = fmt.Sprintf("判大（客胜推荐:%s）", strings.Join(sources, "/"))
					file("7. 有客胜推荐＋同推大球", combo)
				}
			}
		}

		// 8~10. 交易/模拟盈亏（舒服项映射）推荐主胜 → 反买客胜，命中=客胜打出。
		if tradeHome {
			fileAway("8. 交易盈亏推荐主胜·反买客胜", "反买客胜（交易推主胜）", 0)
		}
		if simHome {
			fileAway("9. 模拟盈亏推荐主胜·反买客胜", "反买客胜（模拟推主胜）", 0)
		}
		if tradeHome && simHome {
			fileAway("10. 双盈亏同推主胜·反买客胜", "反买客胜（交易+模拟推主胜）", 0)
		}
		// 11~14. 反买客胜与四个客胜信号的组合：任一盈亏推荐主胜且该信号推荐
		// 客胜，命中=客胜打出。
		if tradeHome || simHome {
			source := "交易"
			if tradeHome && simHome {
				source = "交易+模拟"
			} else if simHome {
				source = "模拟"
			}
			if baseAway {
				fileAway("11. 盈亏反买客胜×主推≥70%客胜", "反买客胜（"+source+"）×主推:客胜", baseProb)
			}
			if recentAway {
				fileAway("12. 盈亏反买客胜×近期状态让球客胜", "反买客胜（"+source+"）×让球:客胜", recentDiff)
			}
			if proAway {
				fileAway("13. 盈亏反买客胜×专业信号客胜", "反买客胜（"+source+"）×专业:客胜", 0)
			}
			if compAway {
				fileAway("14. 盈亏反买客胜×亚盘综合均值客胜", "反买客胜（"+source+"）×综合:客胜", composite)
			}
		}

		// 15~16. 主胜对照组：口径与分组1/2完全对称，方向换成主胜，命中=主胜打出。
		baseHome := baseDir == "home" && baseProb >= lastDanceProbThreshold
		if baseHome && proDir == "home" && hasRecentDiff && recentDiff > 0 && compDir == "home" {
			fileHome("15. 四信号齐备·同推主胜",
				fmt.Sprintf("主胜（主推%.1f%%｜专业｜让球%.2f｜综合%.2f）", baseProb, recentDiff, composite), baseProb)
		}
		if baseHome {
			fileHome("16. 前端主推≥70%·推荐主胜", "主推:主胜", baseProb)
		}

		// 17~18. 纯反买：盈亏映射推荐什么就买反方向（推主胜买客胜、推客胜买
		// 主胜），无其他条件；命中=反方向打出。
		fileFade := func(key, source, pick string) {
			opposite := "home"
			if pick == "home" {
				opposite = "away"
			}
			detail := statisticsBaseDetail(match)
			detail.Pick = "反买" + statisticsOutcomeLabel(opposite) + "（" + source + "推" + statisticsOutcomeLabel(pick) + "）"
			detail.Result = resultLabel
			detail.Hit = actual == opposite
			file(key, detail)
		}
		if tradePick != "" {
			fileFade("17. 反买交易盈亏（推荐反向·无其他条件）", "交易", tradePick)
		}
		if simPick != "" {
			fileFade("18. 反买模拟盈亏（推荐反向·无其他条件）", "模拟", simPick)
		}
	}

	rows := []gin.H{}
	matched, hit := 0, 0
	for _, key := range order {
		sig := buckets[key]
		if sig == nil || len(sig.details) == 0 {
			continue
		}
		matched += len(sig.details)
		hit += sig.hit
		rows = append(rows, sig.payload("dance-away-"+key, key, ""))
	}
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	return gin.H{
		"key":   "dance_away",
		"title": "客胜命中率（各信号推荐客胜的结算）",
		"definition": "1=前端主推≥70%、专业信号（凯体同向）、近期状态让球（净胜球差<0）、亚盘综合均值（期望<-0.25）四个信号同时推荐客胜；2~5=单个信号推荐客胜；命中=客胜打出，明细数值列为该信号数值。6=近期平均球数与球数综合均值同时高于大小球盘口（同推大球），命中=大球打出（走盘不计），数值列为综合期望、盘口列为大小球线。7=同场存在任一客胜推荐（2~5条件之一）且同推大球，命中=大球打出，明细推荐列标注客胜推荐来源。8~10=交易盈亏/模拟盈亏（舒服项映射：胜→主胜，平/负→主让判客胜、主受让判主胜）推荐主胜时反买客胜，命中=客胜打出。11~14=任一盈亏推荐主胜（反买客胜）且对应信号推荐客胜的组合，命中=客胜打出，明细推荐列标注盈亏来源。15~16=主胜对照组：四信号齐备同推主胜、前端主推≥70%推荐主胜，命中=主胜打出。17~18=纯反买：交易盈亏/模拟盈亏（舒服项映射）推荐什么就买反方向（推主胜买客胜、推客胜买主胜），无其他条件，命中=反方向打出。同一场比赛可进入多个分组。",
		"covered": len(covered),
		"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": rows,
	}
}

