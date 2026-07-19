package handlers

import (
	"fmt"
	"math"
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// 单场详情页（MatchDetailView）的分析结论，原先全部在前端本地计算，现由后端统一产出。
// 口径与旧前端保持一致：概率取整、信心分档（≥50高/≥42中）、近5场 W=3/D=1/L=0 评分等。

type matchInsightResponse struct {
	HomeWinPct      int      `json:"homeWinPct"`
	DrawPct         int      `json:"drawPct"`
	AwayWinPct      int      `json:"awayWinPct"`
	StrongestPct    int      `json:"strongestPct"`
	PredictedPick   string   `json:"predictedPick"`
	ConfidenceLabel string   `json:"confidenceLabel"`
	ConfidenceTone  string   `json:"confidenceTone"`
	OddsSignal      string   `json:"oddsSignal"`
	FormSignal      string   `json:"formSignal"`
	H2hSignal       string   `json:"h2hSignal"`
	ConclusionText  string   `json:"conclusionText"`
	HomeRecentForm  []string `json:"homeRecentForm"`
	GuestRecentForm []string `json:"guestRecentForm"`
	H2hHomePct      int      `json:"h2hHomePct"`
	H2hAwayPct      int      `json:"h2hAwayPct"`
	H2hGoalHomePct  int      `json:"h2hGoalHomePct"`
	H2hGoalAwayPct  int      `json:"h2hGoalAwayPct"`
}

// GetMatchInsight 返回单场详情页的全部本地分析结论。
func GetMatchInsight(c *gin.Context) {
	matchID := c.Param("id")

	var match models.Money
	if err := database.DB.Where("match_id = ?", matchID).First(&match).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}

	var history models.HistoryMoney
	database.DB.Where("match_id = ?", match.MatchId).Limit(1).Find(&history)
	var odds models.OddsMoney
	database.DB.Where("match_id = ?", match.MatchId).Limit(1).Find(&odds)

	avgOdd, _, _ := resolveAverageOdds(odds)
	homePct, drawPct := 33, 34
	if probabilities := probabilitiesFromOdds(avgOdd); probabilities != nil {
		homePct = int(math.Round(probabilities[0]))
		drawPct = int(math.Round(probabilities[1]))
	}
	awayPct := 100 - homePct - drawPct
	strongest := maxInt(homePct, maxInt(drawPct, awayPct))

	predictedPick := "平局方向"
	if homePct >= drawPct && homePct >= awayPct {
		predictedPick = firstNonEmptyString(match.Home, "主队方向")
	} else if awayPct >= homePct && awayPct >= drawPct {
		predictedPick = firstNonEmptyString(match.Guest, "客队方向")
	}

	confidenceLabel, confidenceTone := "谨慎观察", "low"
	if strongest >= 50 {
		confidenceLabel, confidenceTone = "高信心", "high"
	} else if strongest >= 42 {
		confidenceLabel, confidenceTone = "中信心", "mid"
	}

	historyData := normalizeHistory(history)
	homeForm := insightRecentForm(historyData.RecentHomeList, match.Home)
	guestForm := insightRecentForm(historyData.RecentGuestList, match.Guest)
	formSignal := insightFormSignal(homeForm, guestForm, match.Home, match.Guest)
	summary := historyData.AgainstSummary
	h2hSignal := historySignal(summary, match.Home, match.Guest)

	h2hHomePct, h2hAwayPct := 50, 50
	if summary.All > 0 {
		h2hHomePct = int(math.Round(float64(summary.Win) / float64(summary.All) * 100))
		h2hAwayPct = int(math.Round(float64(summary.Lose) / float64(summary.All) * 100))
	}
	h2hGoalHomePct := 50
	if goalTotal := summary.WinGoal + summary.LoseGoal; goalTotal > 0 {
		h2hGoalHomePct = int(math.Round(float64(summary.WinGoal) / float64(goalTotal) * 100))
	}

	c.JSON(http.StatusOK, matchInsightResponse{
		HomeWinPct:      homePct,
		DrawPct:         drawPct,
		AwayWinPct:      awayPct,
		StrongestPct:    strongest,
		PredictedPick:   predictedPick,
		ConfidenceLabel: confidenceLabel,
		ConfidenceTone:  confidenceTone,
		OddsSignal:      fmt.Sprintf("%s %d%%", predictedPick, strongest),
		FormSignal:      formSignal,
		H2hSignal:       h2hSignal,
		ConclusionText: fmt.Sprintf("综合平均欧赔、近况和交锋记录，当前倾向 %s。赔率隐含概率最高为 %d%%，近期状态为%s，历史交锋为%s。",
			predictedPick, strongest, formSignal, h2hSignal),
		HomeRecentForm:  homeForm,
		GuestRecentForm: guestForm,
		H2hHomePct:      h2hHomePct,
		H2hAwayPct:      h2hAwayPct,
		H2hGoalHomePct:  h2hGoalHomePct,
		H2hGoalAwayPct:  100 - h2hGoalHomePct,
	})
}

func insightRecentForm(list []analysisHistoryMatch, team string) []string {
	form := []string{}
	for index, row := range list {
		if index >= 5 {
			break
		}
		homeGoal, awayGoal := 0, 0
		if len(row.Goal) >= 2 {
			homeGoal, awayGoal = row.Goal[0], row.Goal[1]
		}
		teamFor, teamAgainst := homeGoal, awayGoal
		if team != "" && row.Guest == team {
			teamFor, teamAgainst = awayGoal, homeGoal
		}
		switch {
		case teamFor > teamAgainst:
			form = append(form, "W")
		case teamFor < teamAgainst:
			form = append(form, "L")
		default:
			form = append(form, "D")
		}
	}
	return form
}

func insightFormSignal(homeForm []string, guestForm []string, home string, guest string) string {
	if len(homeForm) == 0 && len(guestForm) == 0 {
		return "样本不足"
	}
	diff := insightFormScore(homeForm) - insightFormScore(guestForm)
	if diff >= 3 {
		return firstNonEmptyString(home, "主队") + "更稳"
	}
	if diff <= -3 {
		return firstNonEmptyString(guest, "客队") + "更稳"
	}
	return "接近均衡"
}

func insightFormScore(form []string) int {
	score := 0
	for _, result := range form {
		if result == "W" {
			score += 3
		} else if result == "D" {
			score++
		}
	}
	return score
}
