// Package handlers: statistics_finale.go —「终章」未来比赛预测。
//
// 扫描未来 finaleHorizonDays 天内的待赛比赛，只输出命中以下四个信号之一的
// 场次（信号口径与「最后一舞」客胜命中率卡片完全一致，主推门槛≥70%）：
//
//	四信号齐备·同推客胜 / 前端主推≥70%·推荐客胜
//	四信号齐备·同推主胜 / 前端主推≥70%·推荐主胜
//
// 交易盈亏/模拟盈亏（舒服项映射）的推荐只随行展示，不参与预测。
// 待赛场次数量小，每次请求实时计算，不落快照。
package handlers

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
)

// finaleHorizonDays 预测向前看的天数窗口。
const finaleHorizonDays = 14

// GetFinaleStatistics serves the 终章 prediction list.
func GetFinaleStatistics(c *gin.Context) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	today := time.Now().Format("2006-01-02")
	horizon := time.Now().AddDate(0, 0, finaleHorizonDays).Format("2006-01-02")

	upcoming := make([]statisticsMatch, 0, 64)
	ids := make([]string, 0, 64)
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if match.ID == "" || match.Settled || match.Date < today || match.Date > horizon {
			continue
		}
		upcoming = append(upcoming, match)
		ids = append(ids, match.ID)
	}

	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)

	predictions := []gin.H{}
	for _, match := range upcoming {
		historyRow := histories[match.ID]
		pankouRow := pankous[match.ID]
		oddsRow := odds[match.ID]

		// —— 信号计算，口径与「最后一舞」buildLastDanceAwayCard 一致 ——
		proDir := ""
		if choices := statisticsKellySportteryChoices(oddsRow); len(choices) > 0 {
			for _, dir := range []string{"home", "draw", "away"} {
				if choices[dir] {
					proDir = dir
				}
			}
		}
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
		_, ahLine, hasAH := statisticsPankouLinePair(pankouRow, "bet365_asia", "asia_data")
		against, homeRecent, guestRecent := statisticsHistory(historyRow)
		historyDiff, _, hasHistory := statisticsHeadToHead(match, against)
		recentDiff, hasRecentDiff := statisticsRecentDifference(
			statisticsRecentForm(homeRecent, match.Home), statisticsRecentForm(guestRecent, match.Guest))
		composite, hasComposite := statisticsAverage(historyDiff, hasHistory, recentDiff, hasRecentDiff, ahLine, hasAH)
		compDir := ""
		if hasComposite {
			compDir, _ = statisticsOutcomeFromValue(composite, statisticsHandicapBand)
		}
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
				tradePick = statisticsOutcomeLabel(pick)
			}
		}
		if simDir, ok := statisticsSimulatedComfort(oddsRow, pankouRow, historyRow, match); ok {
			if pick, mapped := mapComfort(simDir); mapped {
				simPick = statisticsOutcomeLabel(pick)
			}
		}

		// —— 四个入选信号 ——
		basePromoted := baseProb >= lastDanceProbThreshold
		fourAway := basePromoted && baseDir == "away" && proDir == "away" &&
			hasRecentDiff && recentDiff < 0 && compDir == "away"
		fourHome := basePromoted && baseDir == "home" && proDir == "home" &&
			hasRecentDiff && recentDiff > 0 && compDir == "home"

		// 未命中信号的比赛 pick/signal 留空，仍然下发（前端默认只显示有预测的，
		// 可切换查看全部）。
		pick, signal := "", ""
		switch {
		case fourAway:
			pick, signal = "away", "四信号齐备"
		case fourHome:
			pick, signal = "home", "四信号齐备"
		case basePromoted && baseDir == "away":
			pick, signal = "away", "主推≥70%"
		case basePromoted && baseDir == "home":
			pick, signal = "home", "主推≥70%"
		}

		row := gin.H{
			"match_id":   match.ID,
			"date":       match.Date,
			"match_time": match.MatchTime,
			"league":     match.League,
			"home":       match.Home,
			"guest":      match.Guest,
			"home_logo":  match.HomeLogo,
			"guest_logo": match.GuestLogo,
			"pick":       pick,
			"signal":     signal,
			"base_prob":  statisticsRound2(baseProb),
			"pro_dir":    "",
			"trade_pick": tradePick,
			"sim_pick":   simPick,
		}
		if proDir != "" {
			row["pro_dir"] = statisticsOutcomeLabel(proDir)
		}
		if hasRecentDiff {
			row["recent_diff"] = statisticsRound2(recentDiff)
		}
		if hasComposite {
			row["composite"] = statisticsRound2(composite)
		}
		predictions = append(predictions, row)
	}

	sort.SliceStable(predictions, func(i, j int) bool {
		ti, _ := predictions[i]["match_time"].(string)
		tj, _ := predictions[j]["match_time"].(string)
		if ti != tj {
			return ti < tj
		}
		mi, _ := predictions[i]["match_id"].(string)
		mj, _ := predictions[j]["match_id"].(string)
		return mi < mj
	})

	c.JSON(http.StatusOK, gin.H{
		"generated_at":   time.Now().Format("2006-01-02 15:04:05"),
		"horizon_days":   finaleHorizonDays,
		"upcoming_total": len(upcoming),
		"predictions":    predictions,
	})
}
