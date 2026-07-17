// Package handlers implements HTTP request handlers for match analysis.
package handlers

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go_server/config"
	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

const sportteryTradeAPIURL = "https://www.vipc.cn/i/match/jczq/lr/%s"
const teamProfileCacheTTL = 7 * 24 * time.Hour
const teamProfileMinSummaryLength = 60

const defaultAnalysisRuleSnapshot = `{
  "version": 1,
  "updatedAt": "",
  "sourceRange": { "startDate": "", "endDate": "" },
  "total": 0,
  "commonRows": []
}`

type analysisEuroOdd struct {
	CompanyID        string          `json:"companyId"`
	CompanyName      string          `json:"companyName"`
	Odds             []string        `json:"odds"`
	FirstOdds        []string        `json:"firstOdds"`
	ReturnRatio      string          `json:"returnRatio"`
	FirstReturnRatio string          `json:"firstReturnRatio"`
	Ratio            []string        `json:"ratio"`
	FirstRatio       []string        `json:"firstRatio"`
	OddsTrend        []int           `json:"oddsTrend"`
	RatioTrend       []int           `json:"ratioTrend"`
	FirstKelly       json.RawMessage `json:"firstKelly"`
	Kelly            json.RawMessage `json:"kelly"`
}

type analysisPankouItem struct {
	CompanyID        int      `json:"companyId"`
	CompanyName      string   `json:"companyName"`
	OddsTrend        []int    `json:"oddsTrend"`
	Odds             []string `json:"odds"`
	FirstOdds        []string `json:"firstOdds"`
	FirstPankou      string   `json:"firstPankou"`
	Pankou           string   `json:"pankou"`
	FirstReturnRatio string   `json:"firstReturnRatio"`
	ReturnRatio      string   `json:"returnRatio"`
}

type analysisHistorySummary struct {
	Win       int `json:"win"`
	Lose      int `json:"lose"`
	Draw      int `json:"draw"`
	All       int `json:"all"`
	WinGoal   int `json:"winGoal"`
	LoseGoal  int `json:"loseGoal"`
	HomeWin   int `json:"homeWin"`
	HomeLose  int `json:"homeLose"`
	HomeDraw  int `json:"homeDraw"`
	HomeAll   int `json:"homeAll"`
	GuestWin  int `json:"guestWin"`
	GuestDraw int `json:"guestDraw"`
	GuestLose int `json:"guestLose"`
	GuestAll  int `json:"guestAll"`
}

type analysisHistoryMatch struct {
	MatchTime string `json:"matchTime"`
	Home      string `json:"home"`
	Guest     string `json:"guest"`
	Goal      []int  `json:"goal"`
	HalfGoal  []int  `json:"halfGoal"`
	League    string `json:"league"`
}

type analysisTeamProfileResponse struct {
	TeamName    string    `json:"teamName"`
	League      string    `json:"league"`
	Summary     string    `json:"summary"`
	SourceTitle string    `json:"sourceTitle"`
	SourceURL   string    `json:"sourceUrl"`
	FetchedAt   time.Time `json:"fetchedAt"`
}

type analysisTeamProfilesResponse struct {
	Home  analysisTeamProfileResponse `json:"home"`
	Guest analysisTeamProfileResponse `json:"guest"`
}

type goddessWomanResponse struct {
	Title             string                 `json:"title"`
	Prediction        string                 `json:"prediction"`
	Confidence        string                 `json:"confidence"`
	HomeScore         float64                `json:"homeScore"`
	GuestScore        float64                `json:"guestScore"`
	Probabilities     directionValues        `json:"probabilities"`
	Formula           string                 `json:"formula"`
	ReasonSummary     string                 `json:"reasonSummary"`
	Reasons           []string               `json:"reasons"`
	DimensionScores   []goddessDimensionLine `json:"dimensionScores"`
	SeventhSenseLabel string                 `json:"seventhSenseLabel"`
}

type goddessDimensionLine struct {
	Label string  `json:"label"`
	Home  float64 `json:"home"`
	Guest float64 `json:"guest"`
}

type analysisMatchResponse struct {
	MatchID      string    `json:"matchId"`
	Date         time.Time `json:"date"`
	League       string    `json:"league"`
	Home         string    `json:"home"`
	Guest        string    `json:"guest"`
	MatchTime    time.Time `json:"matchTime"`
	DisplayState string    `json:"displayState"`
	Status       int       `json:"status"`
	JingcaiID    string    `json:"jingcaiId"`
	HomeScore    int       `json:"homeScore"`
	GuestScore   int       `json:"guestScore"`
	HomeLogo     string    `json:"homeLogo"`
	GuestLogo    string    `json:"guestLogo"`
	HomeRank     string    `json:"homeRank"`
	GuestRank    string    `json:"guestRank"`

	WinProbability  float64  `json:"winProbability"`
	DrawProbability float64  `json:"drawProbability"`
	LoseProbability float64  `json:"loseProbability"`
	Prediction      string   `json:"prediction"`
	QiuPrediction   string   `json:"qiuprediction"`
	Confidence      string   `json:"confidence"`
	Tags            []string `json:"tags"`
	Warnings        []string `json:"warnings"`

	SanhuXinli       []string      `json:"sanhuxinli"`
	KaiJuResult      []string      `json:"kaijuresult"`
	KaiLiResult      []string      `json:"kailiresult"`
	TiCaiResult      []string      `json:"ticairesult"`
	LiangDuiLiShi    []string      `json:"liangduilishi"`
	LiangDuiBiSai    []interface{} `json:"liangduibisai"`
	HomeZuijinBisai  []interface{} `json:"homezuijinbisai"`
	GuestZuijinBisai []interface{} `json:"guestzuijinbisai"`
	TouZhuE          []int         `json:"touzhue"`

	ChangGuiYaPan   string        `json:"changguiyapan"`
	ChangGuiQiuShu  string        `json:"changguiqiushu"`
	YaPanTouZhu     []interface{} `json:"yapantouzhu"`
	NewYaPanTouZhu  []interface{} `json:"newyapantouzhu"`
	QiuShuTouZhu    []interface{} `json:"qiushutouzhu"`
	NewQiuShuTouZhu []interface{} `json:"newqiushutouzhu"`
	QiuShuAll       []interface{} `json:"qiushuAll"`
	LiangDuiQiuShu  []interface{} `json:"liangduiqiushu"`
	YapanPankou1    float64       `json:"yapanpankou1"`
	YapanPankou2    float64       `json:"yapanpankou2"`
	NewPankou       float64       `json:"newpankou"`
	QiushuPankou1   float64       `json:"qiushupankou1"`
	QiushuPankou2   float64       `json:"qiushupankou2"`
	NewQiushu       float64       `json:"newqiushu"`
	YapanAI         []float64     `json:"yapanai"`
	QiushuAI        []float64     `json:"qiushuai"`

	OddsCompanyCount int       `json:"oddsCompanyCount"`
	AsiaCount        int       `json:"asiaCount"`
	DxqCount         int       `json:"dxqCount"`
	SportteryOdds    []float64 `json:"sportteryOdds"`

	Detail        analysisDetailResponse        `json:"detail"`
	RoiSimulation *matchRoiSimulationResponse   `json:"roiSimulation,omitempty"`
	Platform      *platformDecision             `json:"platform,omitempty"`
	MyAngle       *myAngleBlock                 `json:"myAngle,omitempty"`
	TeamProfiles  *analysisTeamProfilesResponse `json:"teamProfiles,omitempty"`
	GoddessWoman  *goddessWomanResponse         `json:"goddessWoman,omitempty"`

	bookmakerOdds  []bookmakerOddsSource
	sportteryTrade sportteryTradeData
}

type analysisDetailResponse struct {
	Date    time.Time     `json:"date"`
	MatchID string        `json:"matchId"`
	Home    string        `json:"home"`
	Test1   []string      `json:"test1"`
	Test2   []interface{} `json:"test2"`
	Test3   []string      `json:"test3"`
	Test4   []string      `json:"test4"`
	Test5   []string      `json:"test5"`
	Test6   []string      `json:"test6"`
	Test7   []int         `json:"test7"`
	Test8   []string      `json:"test8"`
	Test9   []interface{} `json:"test9"`
	Test10  []string      `json:"test10"`
	Test11  []string      `json:"test11"`
	Test14  []interface{} `json:"test14"`
	Test15  []interface{} `json:"test15"`
	Test16  []string      `json:"test16"`
	Test17  []interface{} `json:"test17"`
	Test19  []interface{} `json:"test19"`
	Test20  []interface{} `json:"test20"`
	Test21  string        `json:"test21"`
	Test22  string        `json:"test22"`
	Test23  []string      `json:"test23"`
}

type recentStats struct {
	For     float64
	Against float64
	MaxFor  float64
	Matches float64
	Streak  float64
	Last    []interface{}
}

type analysisHistoryData struct {
	AgainstSummary  analysisHistorySummary
	AgainstList     []analysisHistoryMatch
	RecentHomeList  []analysisHistoryMatch
	RecentGuestList []analysisHistoryMatch
}

type analysisHistorySection struct {
	Summary analysisHistorySummary `json:"summary"`
	List    []analysisHistoryMatch `json:"list"`
}

type analysisRecentPayload struct {
	Home  analysisHistorySection `json:"home"`
	Guest analysisHistorySection `json:"guest"`
}

type analysisHistoryPayload struct {
	Against analysisHistorySection `json:"against"`
	Recent  analysisRecentPayload  `json:"recent"`
}

type analysisOddsPayload struct {
	Odds []analysisEuroOdd `json:"odds"`
}

type analysisPankouPayload struct {
	Asia []analysisPankouItem `json:"asia"`
	Dxq  []analysisPankouItem `json:"dxq"`
}

// GetAnalysisMatches returns old-home compatible analysis rows.
func GetAnalysisMatches(c *gin.Context) {
	startDate, endDate, err := analysisDateWindow(c.Query("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}
	var matches []models.Money
	query := database.DB.Where("date BETWEEN ? AND ?", startDate, endDate)
	if c.Query("scope") != "all" {
		query = query.Where("jingcai_id IS NOT NULL AND TRIM(jingcai_id) <> ?", "")
	}
	query = query.Where("display_state IS NULL OR display_state <> ?", detailOnlyDisplayState)
	if err := query.Order("match_time ASC").Find(&matches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	items := make([]analysisMatchResponse, 0, len(matches))
	for _, match := range matches {
		items = append(items, buildAnalysisWithWeights(match, false))
	}
	// 我的镜像：把库主历史选择在同类盘型下的红黑表现附到每场比赛。
	attachMyAngle(items)

	c.JSON(http.StatusOK, items)
}

// GetAnalysisRuleSnapshot returns the checked-in historical rule snapshot.
func GetAnalysisRuleSnapshot(c *gin.Context) {
	content, err := readAnalysisRuleSnapshot()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", content)
}

// GetAnalysisDetail returns a single old-home compatible analysis row.
func GetAnalysisDetail(c *gin.Context) {
	matchID := c.Param("id")

	var match models.Money
	if err := database.DB.Where("match_id = ? AND (display_state IS NULL OR display_state <> ?)", matchID, detailOnlyDisplayState).First(&match).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}

	c.JSON(http.StatusOK, buildAnalysisWithWeights(match, true))
}

func readAnalysisRuleSnapshot() ([]byte, error) {
	paths := analysisRuleSnapshotCandidates()
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err == nil {
			if !json.Valid(content) {
				return nil, fmt.Errorf("analysis rule snapshot json invalid: %s", path)
			}
			return content, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return []byte(defaultAnalysisRuleSnapshot), nil
}

func analysisRuleSnapshotCandidates() []string {
	configured := strings.TrimSpace(config.AnalysisRuleSnapshotPath)
	candidates := []string{}
	add := func(path string) {
		if strings.TrimSpace(path) != "" {
			candidates = append(candidates, filepath.Clean(path))
		}
	}
	add(configured)
	add(filepath.Join("data", "analysis_rule_snapshot.json"))
	add(filepath.Join("go_server", "data", "analysis_rule_snapshot.json"))
	add(filepath.Join("..", "go_server", "data", "analysis_rule_snapshot.json"))
	return candidates
}

func buildAnalysisWithWeights(match models.Money, fetchSporttery bool) analysisMatchResponse {
	response := buildAnalysis(match, fetchSporttery)
	attachAnalysisWeights(&response)
	// The unified decision block replaces every recommendation/warning the H5
	// frontend used to compute locally; it must run after the ROI markets exist.
	response.Platform = buildPlatformDecision(&response)
	return response
}

func buildAnalysis(match models.Money, fetchSporttery bool) analysisMatchResponse {
	var history models.HistoryMoney
	database.DB.Where("match_id = ?", match.MatchId).Limit(1).Find(&history)

	var odds models.OddsMoney
	database.DB.Where("match_id = ?", match.MatchId).Limit(1).Find(&odds)

	var pankou models.PankouMoney
	database.DB.Where("match_id = ?", match.MatchId).Limit(1).Find(&pankou)

	avgOdd, oddsRows, warnings := resolveAverageOdds(odds)
	sportteryTrade := resolveSportteryTrade(match, &odds, fetchSporttery)
	sportteryOdd := sportteryOdds(sportteryTrade)
	bookmakerOdds := bookmakerOddsSources(oddsRows, sportteryOdd)
	probabilities := probabilitiesFromOdds(avgOdd)
	if probabilities == nil {
		probabilities = []float64{33, 34, 33}
	}

	probabilityLabels := []string{
		formatPercent(probabilities[0]),
		formatPercent(probabilities[1]),
		formatPercent(probabilities[2]),
	}

	predictionIndex := maxProbabilityIndex(probabilities)
	prediction := []string{"主胜", "平局", "客胜"}[predictionIndex]
	confidence := confidenceLabel(probabilities[predictionIndex])

	historyData := normalizeHistory(history)
	againstSummary := historyData.AgainstSummary
	againstList := historyData.AgainstList
	recentHomeList := historyData.RecentHomeList
	recentGuestList := historyData.RecentGuestList

	homeRecent := summarizeRecent(recentHomeList, match.Home)
	guestRecent := summarizeRecent(recentGuestList, match.Guest)
	liangDuiBiSai := firstHistoryRow(againstList)
	homeLast := homeRecent.Last
	guestLast := guestRecent.Last

	historyWinPct, historyDrawPct, historyLosePct := historyPercentages(againstSummary)
	historyGoalDiff := safeDivide(float64(againstSummary.WinGoal-againstSummary.LoseGoal), float64(maxInt(againstSummary.All, 1)))
	recentGoalDiff := safeDivide(homeRecent.For-homeRecent.Against, maxFloat(homeRecent.Matches, 1)) - safeDivide(guestRecent.For-guestRecent.Against, maxFloat(guestRecent.Matches, 1))
	historyTotalGoals := safeDivide(float64(againstSummary.WinGoal+againstSummary.LoseGoal), float64(maxInt(againstSummary.All, 1)))
	recentTotalGoals := safeDivide(homeRecent.For+homeRecent.Against+guestRecent.For+guestRecent.Against, maxFloat(homeRecent.Matches+guestRecent.Matches, 1))

	liangduilishi := []string{
		formatPercent(historyWinPct),
		formatPercent(historyDrawPct),
		formatPercent(historyLosePct),
		historySignal(againstSummary, match.Home, match.Guest),
		fmt.Sprintf("历史均球 %.2f", historyTotalGoals),
	}

	asianRows, dxqRows := pankouRows(pankou)
	bet365Asia := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Asia), asianRows, 8)
	bet365Dxq := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Dxq), dxqRows, 8)

	yapanPankou1 := parsePankouLine(bet365Asia.FirstPankou)
	yapanPankou2 := parsePankouLine(firstNonEmptyString(bet365Asia.Pankou, bet365Asia.FirstPankou))
	qiushuPankou1 := parsePankouLine(bet365Dxq.FirstPankou)
	qiushuPankou2 := parsePankouLine(firstNonEmptyString(bet365Dxq.Pankou, bet365Dxq.FirstPankou))
	if qiushuPankou2 == 0 {
		qiushuPankou2 = round2(maxFloat(recentTotalGoals, historyTotalGoals))
	}

	yapanHomePressure, yapanGuestPressure := pressurePair(probabilities[0], probabilities[2], yapanPankou1, yapanPankou2)
	overPressure := clamp(50+(recentTotalGoals-qiushuPankou2)*18, 0, 100)
	underPressure := round2(100 - overPressure)
	qiuPrediction := qiuPrediction(overPressure, underPressure)

	changguiYapan := fmt.Sprintf("%.2f:%.2f", historyGoalDiff, recentGoalDiff)
	changguiQiushu := fmt.Sprintf("%.2f:%.2f", historyTotalGoals, recentTotalGoals)
	yapanSignal := pressureSignal(yapanHomePressure, yapanGuestPressure, match.Home, match.Guest)
	qiushuSignal := fmt.Sprintf("期望 %.2f / 盘口 %.2f", recentTotalGoals, qiushuPankou2)

	yapantouzhu := []interface{}{
		round2(yapanHomePressure), round2(yapanGuestPressure), probabilityLabels[0], probabilityLabels[2],
		round2(historyGoalDiff), round2(recentGoalDiff), round2(probabilities[0] - probabilities[2]), round2(math.Abs(yapanPankou2 - yapanPankou1)),
		yapanPankou1, yapanPankou2, round2(homeRecent.For), round2(guestRecent.For), yapanSignal,
	}
	qiushutouzhu := []interface{}{
		round2(overPressure), round2(underPressure), round2(safeDivide(homeRecent.For+guestRecent.For, maxFloat(homeRecent.Matches+guestRecent.Matches, 1))), round2(safeDivide(homeRecent.Against+guestRecent.Against, maxFloat(homeRecent.Matches+guestRecent.Matches, 1))),
		qiushuPankou1, qiushuPankou2, qiushuSignal,
	}
	qiushuAll := []interface{}{round2(homeRecent.For), round2(homeRecent.MaxFor), round2(guestRecent.For), round2(guestRecent.MaxFor), round2(homeRecent.Against), round2(guestRecent.Against)}
	liangduiqiushu := []interface{}{round2(safeDivide(homeRecent.For, maxFloat(homeRecent.Matches, 1))), round2(safeDivide(guestRecent.For, maxFloat(guestRecent.Matches, 1))), round2(safeDivide(homeRecent.Against, maxFloat(homeRecent.Matches, 1))), round2(safeDivide(guestRecent.Against, maxFloat(guestRecent.Matches, 1))), homeRecent.Streak, guestRecent.Streak}

	touzhue := []int{int(math.Round(probabilities[0] * 8)), int(math.Round(probabilities[1] * 8)), int(math.Round(probabilities[2] * 8))}
	kaijuLabels := openingProbabilityLabels(oddsRows, probabilityLabels)
	kailiresult := kellyResult(odds, avgOdd, oddsRows)
	ticairesult := ticaiResult(oddsRows, avgOdd, sportteryOdd)
	if len(kailiresult) == 0 {
		kailiresult = []string{predictionShort(prediction)}
	}
	if len(ticairesult) == 0 {
		ticairesult = []string{predictionShort(prediction)}
	}

	sanhuxinli := []string{probabilityLabels[0], probabilityLabels[1], probabilityLabels[2], fmt.Sprintf("%s~%s", firstNonEmptyString(match.HomeRank, "-"), firstNonEmptyString(match.GuestRank, "-")), historySignal(againstSummary, match.Home, match.Guest)}
	tags := analysisTags(match.League, prediction, probabilities, againstSummary, yapanPankou1, yapanPankou2, recentTotalGoals, qiushuPankou2)
	goddessWoman := buildGoddessWoman(match, probabilities, againstSummary, againstList, homeRecent, guestRecent)

	test2 := []interface{}{match.League, fmt.Sprintf("%s(%s)", match.Home, firstNonEmptyString(match.HomeRank, "-")), fmt.Sprintf("%s(%s)", match.Guest, firstNonEmptyString(match.GuestRank, "-")), match.MatchTime}
	detail := analysisDetailResponse{
		Date:    match.Date,
		MatchID: match.MatchId,
		Home:    match.Home,
		Test1:   kaijuLabels,
		Test2:   test2,
		Test3:   kailiresult,
		Test4:   ticairesult,
		Test5:   liangduilishi,
		Test6:   sanhuxinli,
		Test7:   touzhue,
		Test8:   averageOddLabels(avgOdd),
		Test9:   []interface{}{bet365Asia.FirstOdds, bet365Asia.Odds, bet365Asia.FirstPankou, bet365Asia.Pankou, bet365Asia.FirstReturnRatio, bet365Asia.ReturnRatio, yapanPankou1, yapanPankou2},
		Test10:  []string{formatPercent(yapanHomePressure), formatPercent(yapanGuestPressure)},
		Test11:  []string{probabilityLabels[0], probabilityLabels[2]},
		Test14:  yapantouzhu,
		Test15:  []interface{}{bet365Dxq.FirstOdds, bet365Dxq.Odds, bet365Dxq.FirstPankou, bet365Dxq.Pankou, bet365Dxq.FirstReturnRatio, bet365Dxq.ReturnRatio, qiushuPankou1, qiushuPankou2},
		Test16:  []string{formatPercent(overPressure), formatPercent(underPressure)},
		Test17:  qiushutouzhu,
		Test19:  []interface{}{round2(historyTotalGoals), round2(recentTotalGoals), qiuPrediction},
		Test20:  qiushuAll,
		Test21:  changguiQiushu,
		Test22:  changguiYapan,
		Test23:  tags,
	}

	teamProfiles := resolveTeamProfiles(match, firstNonEmptyString(match.League, match.LeagueName), fetchSporttery)

	return analysisMatchResponse{
		MatchID:          match.MatchId,
		Date:             match.Date,
		League:           firstNonEmptyString(match.League, match.LeagueName),
		Home:             match.Home,
		Guest:            match.Guest,
		MatchTime:        match.MatchTime,
		DisplayState:     match.DisplayState,
		Status:           match.Status,
		JingcaiID:        match.JingcaiID,
		HomeScore:        match.HomeScore,
		GuestScore:       match.GuestScore,
		HomeLogo:         match.HomeLogo,
		GuestLogo:        match.GuestLogo,
		HomeRank:         match.HomeRank,
		GuestRank:        match.GuestRank,
		WinProbability:   round2(probabilities[0]),
		DrawProbability:  round2(probabilities[1]),
		LoseProbability:  round2(probabilities[2]),
		Prediction:       prediction,
		QiuPrediction:    qiuPrediction,
		Confidence:       confidence,
		Tags:             tags,
		Warnings:         warnings,
		SanhuXinli:       sanhuxinli,
		KaiJuResult:      kaijuLabels,
		KaiLiResult:      kailiresult,
		TiCaiResult:      ticairesult,
		LiangDuiLiShi:    liangduilishi,
		LiangDuiBiSai:    liangDuiBiSai,
		HomeZuijinBisai:  homeLast,
		GuestZuijinBisai: guestLast,
		TouZhuE:          touzhue,
		ChangGuiYaPan:    changguiYapan,
		ChangGuiQiuShu:   changguiQiushu,
		YaPanTouZhu:      yapantouzhu,
		NewYaPanTouZhu:   cloneInterfaceSlice(yapantouzhu),
		QiuShuTouZhu:     qiushutouzhu,
		NewQiuShuTouZhu:  cloneInterfaceSlice(qiushutouzhu),
		QiuShuAll:        qiushuAll,
		LiangDuiQiuShu:   liangduiqiushu,
		YapanPankou1:     yapanPankou1,
		YapanPankou2:     yapanPankou2,
		NewPankou:        yapanPankou2,
		QiushuPankou1:    qiushuPankou1,
		QiushuPankou2:    qiushuPankou2,
		NewQiushu:        qiushuPankou2,
		YapanAI:          []float64{round2(yapanHomePressure), round2(yapanGuestPressure)},
		QiushuAI:         []float64{round2(overPressure), round2(underPressure)},
		OddsCompanyCount: maxInt(odds.CompanyCount, len(oddsRows)),
		AsiaCount:        maxInt(pankou.AsiaCount, len(asianRows)),
		DxqCount:         maxInt(pankou.DxqCount, len(dxqRows)),
		SportteryOdds:    sportteryOdd,
		Detail:           detail,
		bookmakerOdds:    bookmakerOdds,
		sportteryTrade:   sportteryTrade,
		TeamProfiles:     teamProfiles,
		GoddessWoman:     &goddessWoman,
	}
}

func resolveTeamProfiles(match models.Money, league string, fetchOnline bool) *analysisTeamProfilesResponse {
	if !fetchOnline {
		return nil
	}

	home := resolveTeamProfile(match.Home, league)
	guest := resolveTeamProfile(match.Guest, league)
	if home.Summary == "" && guest.Summary == "" {
		return nil
	}
	return &analysisTeamProfilesResponse{Home: home, Guest: guest}
}

func resolveTeamProfile(teamName string, league string) analysisTeamProfileResponse {
	teamName = strings.TrimSpace(teamName)
	league = strings.TrimSpace(league)
	if teamName == "" {
		return analysisTeamProfileResponse{}
	}

	cache := models.TeamInfoCache{}
	err := database.DB.Where("team_name = ? AND league = ?", teamName, league).Limit(1).Find(&cache).Error
	if err == nil && cache.ID > 0 && teamInfoCacheFresh(cache) {
		return teamProfileFromCache(cache)
	}

	summary, sourceTitle, sourceURL := fetchTeamBasicInfo(teamName, league)
	if len([]rune(summary)) < teamProfileMinSummaryLength {
		summary = fmt.Sprintf("%s是%s参赛球队，本地分析将主要结合赛程、排名、近期状态、欧赔与盘口数据判断其本场走势。", teamName, firstNonEmptyString(league, "当前赛事"))
		sourceTitle = "本地赛程资料"
		sourceURL = ""
	}

	next := models.TeamInfoCache{
		TeamName:    teamName,
		League:      league,
		Summary:     clampRunes(summary, 420),
		SourceTitle: clampRunes(sourceTitle, 120),
		SourceURL:   clampRunes(sourceURL, 500),
		FetchedAt:   time.Now(),
	}
	if cache.ID > 0 {
		next.ID = cache.ID
		_ = database.DB.Model(&cache).Updates(map[string]interface{}{
			"summary":      next.Summary,
			"source_title": next.SourceTitle,
			"source_url":   next.SourceURL,
			"fetched_at":   next.FetchedAt,
		}).Error
	} else {
		_ = database.DB.Create(&next).Error
	}

	return teamProfileFromCache(next)
}

func teamInfoCacheFresh(cache models.TeamInfoCache) bool {
	if len([]rune(strings.TrimSpace(cache.Summary))) < teamProfileMinSummaryLength {
		return false
	}
	return time.Since(cache.FetchedAt) < teamProfileCacheTTL
}

func teamProfileFromCache(cache models.TeamInfoCache) analysisTeamProfileResponse {
	return analysisTeamProfileResponse{
		TeamName:    cache.TeamName,
		League:      cache.League,
		Summary:     cache.Summary,
		SourceTitle: cache.SourceTitle,
		SourceURL:   cache.SourceURL,
		FetchedAt:   cache.FetchedAt,
	}
}

func fetchTeamBasicInfo(teamName string, league string) (string, string, string) {
	if summary, title, sourceURL := fetchTeamInfoFromWikipedia(teamName, league); summary != "" {
		return summary, title, sourceURL
	}
	return fetchTeamInfoFromSearch(teamName, league)
}

func fetchTeamInfoFromWikipedia(teamName string, league string) (string, string, string) {
	searchURL := "https://zh.wikipedia.org/w/api.php?action=query&list=search&format=json&srlimit=1&srsearch=" + url.QueryEscape(strings.TrimSpace(teamName+" 足球 "+league))
	body, ok := fetchText(searchURL, 1<<20)
	if !ok {
		return "", "", ""
	}

	var search struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &search); err != nil || len(search.Query.Search) == 0 {
		return "", "", ""
	}

	title := search.Query.Search[0].Title
	extractURL := "https://zh.wikipedia.org/w/api.php?action=query&prop=extracts&exintro=1&explaintext=1&redirects=1&format=json&titles=" + url.QueryEscape(title)
	body, ok = fetchText(extractURL, 1<<20)
	if !ok {
		return "", "", ""
	}

	var extract struct {
		Query struct {
			Pages map[string]struct {
				Title   string `json:"title"`
				Extract string `json:"extract"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &extract); err != nil {
		return "", "", ""
	}
	for _, page := range extract.Query.Pages {
		summary := normalizeSpace(page.Extract)
		if len([]rune(summary)) >= teamProfileMinSummaryLength {
			pageTitle := firstNonEmptyString(page.Title, title)
			return summary, pageTitle, "https://zh.wikipedia.org/wiki/" + strings.ReplaceAll(url.PathEscape(pageTitle), "%20", "_")
		}
	}
	return "", "", ""
}

func fetchTeamInfoFromSearch(teamName string, league string) (string, string, string) {
	query := strings.TrimSpace(teamName + " " + league + " 足球 球队 资料")
	searchURL := "https://duckduckgo.com/html/?q=" + url.QueryEscape(query)
	body, ok := fetchText(searchURL, 2<<20)
	if !ok {
		return "", "", ""
	}

	htmlText := string(body)
	title := firstRegexGroup(htmlText, `(?s)<a[^>]+class="[^"]*result__a[^"]*"[^>]*>(.*?)</a>`)
	link := firstRegexGroup(htmlText, `(?s)<a[^>]+class="[^"]*result__a[^"]*"[^>]*href="([^"]+)"|<a[^>]+href="([^"]+)"[^>]+class="[^"]*result__a`)
	snippets := regexGroups(htmlText, `(?s)<a[^>]+class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</a>|<div[^>]+class="[^"]*result__snippet[^"]*"[^>]*>(.*?)</div>`, 4)

	parts := make([]string, 0, len(snippets))
	for _, snippet := range snippets {
		text := normalizeSpace(stripHTML(snippet))
		if text != "" {
			parts = append(parts, text)
		}
	}
	summary := strings.Join(parts, " ")
	return summary, normalizeSpace(stripHTML(title)), normalizeSearchURL(link)
}

func fetchText(requestURL string, limit int64) ([]byte, bool) {
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, false
	}
	request.Header.Set("User-Agent", "PeakBall/1.0 (+https://localhost)")
	client := http.Client{Timeout: 8 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, false
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit))
	return body, err == nil
}

func resolveAverageOdds(odds models.OddsMoney) ([]float64, []analysisEuroOdd, []string) {
	warnings := []string{}
	avg := jsonValue[analysisEuroOdd](odds.AvgOdds)
	if len(avg.Odds) >= 3 {
		return []float64{parseFloat(avg.Odds[0]), parseFloat(avg.Odds[1]), parseFloat(avg.Odds[2])}, euroOddsRows(odds), warnings
	}

	rows := euroOddsRows(odds)
	if len(rows) == 0 {
		return nil, rows, append(warnings, "缺少欧赔公司数据")
	}

	for _, row := range rows {
		if len(row.Odds) >= 3 && isAverageOddRow(row) {
			return []float64{parseFloat(row.Odds[0]), parseFloat(row.Odds[1]), parseFloat(row.Odds[2])}, rows, warnings
		}
	}

	sums := []float64{0, 0, 0}
	counts := []float64{0, 0, 0}
	for _, row := range rows {
		if len(row.Odds) < 3 || isAverageOddRow(row) {
			continue
		}
		for i := 0; i < 3; i++ {
			value := parseFloat(row.Odds[i])
			if value <= 0 {
				continue
			}
			sums[i] += value
			counts[i]++
		}
	}

	for _, count := range counts {
		if count == 0 {
			return nil, rows, append(warnings, "欧赔公司数据不完整")
		}
	}

	return []float64{sums[0] / counts[0], sums[1] / counts[1], sums[2] / counts[2]}, rows, append(warnings, "源站缺少平均欧赔，已用全部公司均值兜底")
}

func euroOddsRows(odds models.OddsMoney) []analysisEuroOdd {
	rows := jsonSlice[analysisEuroOdd](odds.Data)
	if len(rows) > 0 {
		return rows
	}
	payload := jsonValue[analysisOddsPayload](odds.Data)
	return payload.Odds
}

func isAverageOddRow(row analysisEuroOdd) bool {
	return row.CompanyID == "" || strings.Contains(row.CompanyName, "平均")
}

func analysisDateWindow(dateStr string) (string, string, error) {
	trimmed := strings.TrimSpace(dateStr)
	if trimmed == "" {
		today := time.Now()
		normalized := today.Format("2006-01-02")
		return normalized, normalized, nil
	}

	date, err := time.Parse("2006-01-02", trimmed)
	if err != nil {
		return "", "", err
	}
	normalized := date.Format("2006-01-02")
	return normalized, normalized, nil
}

func normalizeHistory(history models.HistoryMoney) analysisHistoryData {
	data := analysisHistoryData{
		AgainstSummary:  jsonValue[analysisHistorySummary](history.AgainstSummary),
		AgainstList:     jsonSlice[analysisHistoryMatch](history.AgainstList),
		RecentHomeList:  jsonSlice[analysisHistoryMatch](history.RecentHomeList),
		RecentGuestList: jsonSlice[analysisHistoryMatch](history.RecentGuestList),
	}
	if data.AgainstSummary.All > 0 || len(data.AgainstList) > 0 || len(data.RecentHomeList) > 0 || len(data.RecentGuestList) > 0 {
		return data
	}

	payload := jsonValue[analysisHistoryPayload](history.LeagueStat)
	data.AgainstSummary = payload.Against.Summary
	data.AgainstList = payload.Against.List
	data.RecentHomeList = payload.Recent.Home.List
	data.RecentGuestList = payload.Recent.Guest.List
	return data
}

func pankouRows(pankou models.PankouMoney) ([]analysisPankouItem, []analysisPankouItem) {
	asianRows := jsonSlice[analysisPankouItem](pankou.AsiaData)
	dxqRows := jsonSlice[analysisPankouItem](pankou.DxqData)
	if len(asianRows) > 0 || len(dxqRows) > 0 {
		return asianRows, dxqRows
	}

	payload := jsonValue[analysisPankouPayload](pankou.AsiaData)
	return payload.Asia, payload.Dxq
}

func selectPankouRow(preferred analysisPankouItem, rows []analysisPankouItem, companyIDs ...int) analysisPankouItem {
	if preferred.CompanyName != "" || len(preferred.Odds) > 0 || preferred.Pankou != "" || preferred.FirstPankou != "" {
		return preferred
	}
	for _, companyID := range companyIDs {
		for _, row := range rows {
			if row.CompanyID == companyID {
				return row
			}
		}
	}
	if len(rows) > 0 {
		return rows[0]
	}
	return analysisPankouItem{}
}

func probabilitiesFromOdds(odds []float64) []float64 {
	if len(odds) < 3 || odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return nil
	}
	total := 1/odds[0] + 1/odds[1] + 1/odds[2]
	if total == 0 {
		return nil
	}
	return []float64{round2(100 * (1 / odds[0]) / total), round2(100 * (1 / odds[1]) / total), round2(100 * (1 / odds[2]) / total)}
}

func summarizeRecent(list []analysisHistoryMatch, team string) recentStats {
	stats := recentStats{}
	limit := minInt(len(list), 5)
	for i := 0; i < limit; i++ {
		row := list[i]
		if len(row.Goal) < 2 {
			continue
		}
		teamFor, teamAgainst := float64(row.Goal[0]), float64(row.Goal[1])
		if row.Guest == team {
			teamFor, teamAgainst = teamAgainst, teamFor
		}
		stats.For += teamFor
		stats.Against += teamAgainst
		stats.MaxFor = math.Max(stats.MaxFor, teamFor)
		stats.Matches++
		if teamFor > teamAgainst {
			stats.Streak++
		}
		if i == 0 {
			stats.Last = []interface{}{row.MatchTime, row.Home, row.Guest, row.Goal[0], row.Goal[1], row.League}
		}
	}
	return stats
}

func firstHistoryRow(list []analysisHistoryMatch) []interface{} {
	if len(list) == 0 {
		return []interface{}{}
	}
	row := list[0]
	if len(row.Goal) < 2 {
		return []interface{}{row.MatchTime, row.Home, row.Guest, 0, 0, row.League}
	}
	return []interface{}{row.MatchTime, row.Home, row.Guest, row.Goal[0], row.Goal[1], row.League}
}

func historyPercentages(summary analysisHistorySummary) (float64, float64, float64) {
	if summary.All <= 0 {
		return 33, 34, 33
	}
	return round2(float64(summary.Win) / float64(summary.All) * 100), round2(float64(summary.Draw) / float64(summary.All) * 100), round2(float64(summary.Lose) / float64(summary.All) * 100)
}

func historySignal(summary analysisHistorySummary, home string, guest string) string {
	if summary.All == 0 {
		return "样本不足"
	}
	if summary.Win > summary.Lose {
		return home + "占优"
	}
	if summary.Lose > summary.Win {
		return guest + "占优"
	}
	return "交锋均衡"
}

func pressurePair(homeProbability float64, guestProbability float64, firstLine float64, currentLine float64) (float64, float64) {
	total := homeProbability + guestProbability
	strengthBase := 50.0
	if total > 0 {
		strengthBase = homeProbability / total * 100
	}

	balanceBase := 50.0
	if currentLine > 0 {
		balanceBase = 55
	} else if currentLine < 0 {
		balanceBase = 45
	}

	// The share-based strength (strengthBase-50) is compressed by the
	// /(home+away) normalisation, so the old *0.45 kept 让球投注热度 pinned near
	// 50 and it never reached the high tiers. 1.4 restores a usable spread,
	// matching the admin statistics 亚盘热度 (statisticsAsianHeat)改良.
	strengthAdjustment := (strengthBase - 50) * 1.4
	handicapCost := currentLine * 8
	movementCost := (currentLine - firstLine) / 0.25 * 1.5
	home := clamp(balanceBase+strengthAdjustment-handicapCost-movementCost, 0, 100)
	guest := 100 - home
	return home, guest
}

func pressureSignal(homePressure float64, guestPressure float64, home string, guest string) string {
	if homePressure-guestPressure >= 8 {
		return home + "压力更足"
	}
	if guestPressure-homePressure >= 8 {
		return guest + "压力更足"
	}
	return "压力接近"
}

func buildGoddessWoman(match models.Money, probabilities []float64, summary analysisHistorySummary, againstList []analysisHistoryMatch, homeRecent recentStats, guestRecent recentStats) goddessWomanResponse {
	homeProbability := probabilityAt(probabilities, 0, 33)
	drawProbability := probabilityAt(probabilities, 1, 34)
	guestProbability := probabilityAt(probabilities, 2, 33)

	homeTeamFavor := clamp(48+homeProbability*0.45+seededRange(match.MatchId, match.Home, "team", -7, 7), 35, 92)
	guestTeamFavor := clamp(48+guestProbability*0.45+seededRange(match.MatchId, match.Guest, "team", -7, 7), 35, 92)
	homeNameFavor := nameFavorScore(match.Home, match.MatchId)
	guestNameFavor := nameFavorScore(match.Guest, match.MatchId)
	homeRankFavor := rankFavorScore(match.HomeRank, match.MatchId, match.Home)
	guestRankFavor := rankFavorScore(match.GuestRank, match.MatchId, match.Guest)
	homeHistoryFavor, guestHistoryFavor := historyFavorScores(summary)
	homePreviousFavor, guestPreviousFavor := previousMatchFavorScores(againstList, match.Home, match.Guest)
	homeSeventhSense := clamp(50+seededRange(match.MatchId, match.Home, "seventh", -12, 12), 35, 65)
	guestSeventhSense := clamp(50+seededRange(match.MatchId, match.Guest, "seventh", -12, 12), 35, 65)

	homeScore := goddessWeightedScore(homeTeamFavor, homeNameFavor, homeRankFavor, homeHistoryFavor, homePreviousFavor, homeSeventhSense)
	guestScore := goddessWeightedScore(guestTeamFavor, guestNameFavor, guestRankFavor, guestHistoryFavor, guestPreviousFavor, guestSeventhSense)
	homeScore += clamp((homeRecent.Streak-guestRecent.Streak)*1.4, -5, 5)
	guestScore += clamp((guestRecent.Streak-homeRecent.Streak)*1.4, -5, 5)
	homeScore = round2(clamp(homeScore, 0, 100))
	guestScore = round2(clamp(guestScore, 0, 100))

	probabilitySet := goddessProbabilities(homeScore-guestScore, drawProbability)
	labels := []string{"主胜", "平局", "客胜"}
	values := []float64{probabilitySet.Home, probabilitySet.Draw, probabilitySet.Away}
	predictionIndex := maxProbabilityIndex(values)
	prediction := labels[predictionIndex]
	confidence := confidenceLabel(values[predictionIndex])

	lines := []goddessDimensionLine{
		{Label: "队伍好感度", Home: round2(homeTeamFavor), Guest: round2(guestTeamFavor)},
		{Label: "名字好感度", Home: round2(homeNameFavor), Guest: round2(guestNameFavor)},
		{Label: "排名好感度", Home: round2(homeRankFavor), Guest: round2(guestRankFavor)},
		{Label: "历史对战记录", Home: round2(homeHistoryFavor), Guest: round2(guestHistoryFavor)},
		{Label: "上一场对战记录", Home: round2(homePreviousFavor), Guest: round2(guestPreviousFavor)},
		{Label: "第七感", Home: round2(homeSeventhSense), Guest: round2(guestSeventhSense)},
	}

	return goddessWomanResponse{
		Title:             "上帝的女人",
		Prediction:        prediction,
		Confidence:        confidence,
		HomeScore:         homeScore,
		GuestScore:        guestScore,
		Probabilities:     probabilitySet,
		Formula:           "队伍好感20% + 名字好感15% + 排名好感20% + 历史对战15% + 上一场对战15% + 第七感15%",
		ReasonSummary:     goddessReasonSummary(match.Home, match.Guest, prediction, probabilitySet, homeScore, guestScore),
		Reasons:           goddessReasons(match, lines, homeScore, guestScore, prediction),
		DimensionScores:   lines,
		SeventhSenseLabel: seventhSenseLabel(homeSeventhSense, guestSeventhSense, match.Home, match.Guest),
	}
}

func goddessWeightedScore(teamFavor float64, nameFavor float64, rankFavor float64, historyFavor float64, previousFavor float64, seventhSense float64) float64 {
	return teamFavor*0.20 + nameFavor*0.15 + rankFavor*0.20 + historyFavor*0.15 + previousFavor*0.15 + seventhSense*0.15
}

func goddessProbabilities(scoreDiff float64, drawProbability float64) directionValues {
	homeRaw := 1 / (1 + math.Exp(-scoreDiff/14))
	closenessBonus := clamp(12-math.Abs(scoreDiff)*0.5, 0, 12)
	drawRaw := clamp(28+closenessBonus+(drawProbability-33)*0.35, 24, 42)
	remaining := 100 - drawRaw
	home := round2(remaining * homeRaw)
	away := round2(remaining - home)
	return directionValues{Home: home, Draw: round2(drawRaw), Away: away}
}

func goddessReasons(match models.Money, lines []goddessDimensionLine, homeScore float64, guestScore float64, prediction string) []string {
	reasons := make([]string, 0, 5)
	for _, line := range lines {
		diff := line.Home - line.Guest
		if math.Abs(diff) < 4 {
			continue
		}
		leader := match.Home
		if diff < 0 {
			leader = match.Guest
		}
		reasons = append(reasons, fmt.Sprintf("%s更偏向%s，差值%.1f分", line.Label, leader, math.Abs(diff)))
		if len(reasons) >= 4 {
			break
		}
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "两队整体感觉接近，平局权重被自然抬高")
	}
	reasons = append(reasons, fmt.Sprintf("固定公式总分%s %.1f，%s %.1f，推荐%s", match.Home, homeScore, match.Guest, guestScore, prediction))
	return reasons
}

func goddessReasonSummary(home string, guest string, prediction string, probabilities directionValues, homeScore float64, guestScore float64) string {
	switch prediction {
	case "主胜":
		return fmt.Sprintf("直觉盘偏向%s，综合好感分%.1f比%.1f，胜平负落点为主胜%s。", home, homeScore, guestScore, formatPercent(probabilities.Home))
	case "客胜":
		return fmt.Sprintf("直觉盘偏向%s，综合好感分%.1f比%.1f，胜平负落点为客胜%s。", guest, homeScore, guestScore, formatPercent(probabilities.Away))
	default:
		return fmt.Sprintf("两队好感差距不大，%s与%s互相压住，平局感提升到%s。", home, guest, formatPercent(probabilities.Draw))
	}
}

func seventhSenseLabel(homeSeventhSense float64, guestSeventhSense float64, home string, guest string) string {
	diff := homeSeventhSense - guestSeventhSense
	if math.Abs(diff) < 3 {
		return "第七感没有明显站队"
	}
	if diff > 0 {
		return "第七感轻微偏向" + home
	}
	return "第七感轻微偏向" + guest
}

func probabilityAt(values []float64, index int, fallback float64) float64 {
	if index < 0 || index >= len(values) || values[index] <= 0 {
		return fallback
	}
	return values[index]
}

func nameFavorScore(name string, salt string) float64 {
	cleaned := strings.TrimSpace(name)
	if cleaned == "" {
		return 50
	}
	runes := []rune(cleaned)
	lengthBonus := clamp(float64(len(runes))*2.2, 4, 16)
	vividBonus := 0.0
	for _, keyword := range []string{"皇", "城", "联", "冠", "圣", "星", "阿", "巴", "罗", "兰", "Real", "City", "United", "Saint", "Inter"} {
		if strings.Contains(cleaned, keyword) {
			vividBonus += 4
		}
	}
	return round2(clamp(42+lengthBonus+vividBonus+seededRange(salt, cleaned, "name", -8, 8), 32, 92))
}

func rankFavorScore(rank string, matchID string, team string) float64 {
	number := regexp.MustCompile(`\d+`).FindString(rank)
	if number == "" {
		return round2(clamp(58+seededRange(matchID, team, "rank", -6, 6), 45, 72))
	}
	parsed, err := strconv.ParseFloat(number, 64)
	if err != nil || parsed <= 0 {
		return 58
	}
	return round2(clamp(96-parsed*0.72, 35, 96))
}

func historyFavorScores(summary analysisHistorySummary) (float64, float64) {
	if summary.All <= 0 {
		return 52, 52
	}
	diff := safeDivide(float64(summary.Win-summary.Lose), float64(summary.All)) * 35
	drawLift := safeDivide(float64(summary.Draw), float64(summary.All)) * 6
	return round2(clamp(50+diff+drawLift, 25, 88)), round2(clamp(50-diff+drawLift, 25, 88))
}

func previousMatchFavorScores(list []analysisHistoryMatch, home string, guest string) (float64, float64) {
	if len(list) == 0 || len(list[0].Goal) < 2 {
		return 52, 52
	}
	row := list[0]
	homeGoals, guestGoals := goalsForPair(row, home, guest)
	if homeGoals == guestGoals {
		return 56, 56
	}
	margin := math.Abs(float64(homeGoals - guestGoals))
	winnerScore := clamp(64+margin*4, 64, 82)
	loserScore := clamp(46-margin*3, 34, 46)
	if homeGoals > guestGoals {
		return round2(winnerScore), round2(loserScore)
	}
	return round2(loserScore), round2(winnerScore)
}

func goalsForPair(row analysisHistoryMatch, home string, guest string) (int, int) {
	if len(row.Goal) < 2 {
		return 0, 0
	}
	rowHomeGoals := row.Goal[0]
	rowGuestGoals := row.Goal[1]
	homeGoals := rowHomeGoals
	guestGoals := rowGuestGoals
	if row.Guest == home {
		homeGoals = rowGuestGoals
	}
	if row.Home == guest {
		guestGoals = rowHomeGoals
	}
	return homeGoals, guestGoals
}

func seededRange(matchID string, team string, salt string, minValue float64, maxValue float64) float64 {
	if maxValue <= minValue {
		return minValue
	}
	seed := float64(positiveHash(matchID+"|"+team+"|"+salt)%10000) / 9999
	return minValue + seed*(maxValue-minValue)
}

func positiveHash(value string) uint32 {
	var hash uint32 = 2166136261
	for _, item := range []byte(value) {
		hash ^= uint32(item)
		hash *= 16777619
	}
	return hash
}

func qiuPrediction(overPressure float64, underPressure float64) string {
	if math.Abs(overPressure-underPressure) < 5 {
		return "盘口球"
	}
	if overPressure > underPressure {
		return "大球"
	}
	return "小球"
}

func openingProbabilityLabels(rows []analysisEuroOdd, fallback []string) []string {
	for _, companyID := range []string{"281", "115"} {
		row := findEuroOdd(rows, companyID, "")
		if len(row.Ratio) >= 3 {
			return []string{row.Ratio[0], row.Ratio[1], row.Ratio[2]}
		}
	}
	return fallback
}

func kellyResult(odds models.OddsMoney, avgOdds []float64, rows []analysisEuroOdd) []string {
	if len(avgOdds) < 3 {
		return nil
	}
	source := jsonValue[analysisEuroOdd](odds.Pinnacle)
	if len(source.Odds) < 3 {
		source = findEuroOdd(rows, "16", "")
	}
	if len(source.Odds) < 3 {
		source = jsonValue[analysisEuroOdd](odds.Bet365)
	}
	if len(source.Odds) < 3 {
		source = findEuroOdd(rows, "281", "")
	}
	if len(source.Odds) < 3 {
		return nil
	}

	avgReturn := returnRatioFromOdds(avgOdds)
	sourceReturn := returnRatioValue(source)
	labels := []string{"胜", "平", "负"}
	result := []string{}
	for i := 0; i < 3; i++ {
		value := parseFloat(source.Odds[i])
		if value <= 0 || avgOdds[i] <= 0 {
			continue
		}
		kelly := value / avgOdds[i] * avgReturn
		if sourceReturn > kelly {
			result = append(result, labels[i])
		}
	}
	return result
}

func ticaiResult(rows []analysisEuroOdd, avgOdds []float64, sportteryOdd []float64) []string {
	if len(rows) == 0 {
		return nil
	}
	reference := findEuroOdd(rows, "115", "威廉")
	if len(reference.Odds) >= 3 && len(sportteryOdd) >= 3 {
		diffs := []float64{math.Abs(parseFloat(reference.Odds[0]) - sportteryOdd[0]), math.Abs(parseFloat(reference.Odds[1]) - sportteryOdd[1]), math.Abs(parseFloat(reference.Odds[2]) - sportteryOdd[2])}
		return labelsForSmallestDiffs(diffs, 0)
	}
	if len(avgOdds) < 3 {
		return nil
	}
	if len(reference.Odds) < 3 {
		reference = firstEuroOdd(rows)
	}
	if len(reference.Odds) < 3 {
		return nil
	}

	diffs := []float64{math.Abs(parseFloat(reference.Odds[0]) - avgOdds[0]), math.Abs(parseFloat(reference.Odds[1]) - avgOdds[1]), math.Abs(parseFloat(reference.Odds[2]) - avgOdds[2])}
	return labelsForSmallestDiffs(diffs, 0.03)
}

func sportteryTradeFromJSON(raw datatypes.JSON) sportteryTradeData {
	payload := jsonValue[sportteryTradePayload](raw)
	if sportteryTradeHasData(payload.Data) {
		return payload.Data
	}

	data := jsonValue[sportteryTradeData](raw)
	if sportteryTradeHasData(data) {
		return data
	}
	return payload.Data
}

func resolveSportteryTrade(match models.Money, odds *models.OddsMoney, fetchSporttery bool) sportteryTradeData {
	trade := sportteryTradeFromJSON(odds.SportteryTrade)
	if sportteryTradeHasData(trade) {
		return trade
	}
	if !fetchSporttery {
		return trade
	}

	fetchedTrade, raw := fetchSportteryTrade(match)
	if !sportteryTradeHasData(fetchedTrade) {
		return trade
	}
	if len(raw) > 0 && odds.MatchId != "" {
		database.DB.Model(&models.OddsMoney{}).Where("match_id = ?", odds.MatchId).Update("sporttery_trade", datatypes.JSON(raw))
	}
	return fetchedTrade
}

func fetchSportteryTrade(match models.Money) (sportteryTradeData, []byte) {
	seen := map[string]bool{}
	for _, tradeID := range []string{match.MatchId, match.JingcaiID} {
		tradeID = strings.TrimSpace(tradeID)
		if tradeID == "" || seen[tradeID] {
			continue
		}
		seen[tradeID] = true

		trade, raw := fetchSportteryTradeByID(tradeID)
		if sportteryTradeHasData(trade) {
			return trade, raw
		}
	}
	return sportteryTradeData{}, nil
}

func fetchSportteryTradeByID(tradeID string) (sportteryTradeData, []byte) {
	client := http.Client{Timeout: 8 * time.Second}
	response, err := client.Get(fmt.Sprintf(sportteryTradeAPIURL, tradeID))
	if err != nil {
		return sportteryTradeData{}, nil
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return sportteryTradeData{}, nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return sportteryTradeData{}, nil
	}
	return sportteryTradeFromJSON(datatypes.JSON(body)), body
}

func sportteryTradeHasData(trade sportteryTradeData) bool {
	return len(sportteryOdds(trade)) >= 3 ||
		hasPercentValues([3]float64{trade.Tzbl.HSupportRate, trade.Tzbl.DSupportRate, trade.Tzbl.ASupportRate}) ||
		sportterySpfAvailable(trade.JyykSpf) ||
		oddsAvailable([3]float64{trade.JyykRqspf.H, trade.JyykRqspf.D, trade.JyykRqspf.A}) ||
		hasPercentValues([3]float64{trade.JyykRqspf.HSupportRate, trade.JyykRqspf.DSupportRate, trade.JyykRqspf.ASupportRate})
}

func sportteryOdds(trade sportteryTradeData) []float64 {
	odds := []float64{trade.Tzbl.H, trade.Tzbl.D, trade.Tzbl.A}
	if odds[0] > 0 && odds[1] > 0 && odds[2] > 0 {
		return odds
	}
	return nil
}

func bookmakerOddsSources(rows []analysisEuroOdd, sportteryOdd []float64) []bookmakerOddsSource {
	return []bookmakerOddsSource{
		{Key: "sporttery", Name: "竞彩", CompanyID: "", Odds: oddsArrayFromSlice(sportteryOdd)},
		{Key: "william", Name: "威廉", CompanyID: "115", Odds: euroOddValues(findEuroOdd(rows, "115", "威廉"))},
		{Key: "bet365", Name: "Bet365", CompanyID: "281", Odds: euroOddValues(findEuroOdd(rows, "281", ""))},
	}
}

func euroOddValues(row analysisEuroOdd) [3]float64 {
	if len(row.Odds) < 3 {
		return [3]float64{}
	}
	return [3]float64{parseFloat(row.Odds[0]), parseFloat(row.Odds[1]), parseFloat(row.Odds[2])}
}

func oddsArrayFromSlice(values []float64) [3]float64 {
	if len(values) < 3 {
		return [3]float64{}
	}
	return [3]float64{values[0], values[1], values[2]}
}

func findEuroOdd(rows []analysisEuroOdd, companyID string, namePart string) analysisEuroOdd {
	for _, row := range rows {
		if companyID != "" && row.CompanyID == companyID {
			return row
		}
		if namePart != "" && strings.Contains(row.CompanyName, namePart) {
			return row
		}
	}
	return analysisEuroOdd{}
}

func firstEuroOdd(rows []analysisEuroOdd) analysisEuroOdd {
	for _, row := range rows {
		if len(row.Odds) >= 3 && !isAverageOddRow(row) {
			return row
		}
	}
	return analysisEuroOdd{}
}

func labelsForSmallestDiffs(diffs []float64, tolerance float64) []string {
	if len(diffs) < 3 {
		return nil
	}
	minDiff := math.Min(diffs[0], math.Min(diffs[1], diffs[2]))
	labels := []string{"胜", "平", "负"}
	result := []string{}
	for i, diff := range diffs {
		if diff <= minDiff+tolerance {
			result = append(result, labels[i])
		}
	}
	return result
}

func analysisTags(league string, prediction string, probabilities []float64, summary analysisHistorySummary, firstLine float64, currentLine float64, expectedGoals float64, goalLine float64) []string {
	tags := []string{"初衷"}
	if strings.Contains(league, "周") {
		tags = append(tags, "某彩")
	}
	if strings.Contains(league, "北") {
		tags = append(tags, "北单")
	}
	if prediction != "客胜" {
		tags = append(tags, "胜平局")
	} else {
		tags = append(tags, "客胜")
	}

	strongest := probabilities[maxProbabilityIndex(probabilities)]
	if strongest >= 65 && summary.All > 0 {
		tags = append(tags, "碾压局")
	}
	if probabilities[1] >= 28 && strongest < 50 {
		tags = append(tags, "闹0区")
	}
	if strongest >= 80 {
		tags = append(tags, "极端场")
	}
	if math.Abs(currentLine-firstLine) >= 0.25 {
		tags = append(tags, "态盘")
	}
	if goalLine > 0 && math.Abs(expectedGoals-goalLine) >= 0.6 {
		tags = append(tags, "裂球")
	}
	if summary.All > 0 && ((prediction == "主胜" && summary.Lose > summary.Win) || (prediction == "客胜" && summary.Win > summary.Lose)) {
		tags = append(tags, "爆冷")
	}

	sort.Strings(tags[1:])
	return tags
}

func jsonValue[T any](data datatypes.JSON) T {
	var out T
	if len(data) == 0 || string(data) == "null" {
		return out
	}
	if err := json.Unmarshal(data, &out); err == nil {
		return out
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		_ = json.Unmarshal([]byte(text), &out)
	}
	return out
}

func jsonSlice[T any](data datatypes.JSON) []T {
	if len(data) == 0 || string(data) == "null" {
		return []T{}
	}
	var out []T
	if err := json.Unmarshal(data, &out); err != nil {
		return []T{}
	}
	return out
}

func averageOddLabels(odds []float64) []string {
	if len(odds) < 3 {
		return []string{}
	}
	return []string{fmt.Sprintf("%.2f", odds[0]), fmt.Sprintf("%.2f", odds[1]), fmt.Sprintf("%.2f", odds[2])}
}

func maxProbabilityIndex(values []float64) int {
	maxIndex := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[maxIndex] {
			maxIndex = i
		}
	}
	return maxIndex
}

func confidenceLabel(value float64) string {
	if value >= 50 {
		return "高信心"
	}
	if value >= 42 {
		return "中信心"
	}
	return "谨慎观察"
}

func predictionShort(prediction string) string {
	switch prediction {
	case "主胜":
		return "胜"
	case "平局":
		return "平"
	case "客胜":
		return "负"
	default:
		return prediction
	}
}

func returnRatioValue(odd analysisEuroOdd) float64 {
	value := parseFloat(strings.TrimSuffix(odd.ReturnRatio, "%"))
	if value > 1 {
		return value / 100
	}
	if value > 0 {
		return value
	}
	values := make([]float64, 0, 3)
	for _, item := range odd.Odds {
		values = append(values, parseFloat(item))
	}
	return returnRatioFromOdds(values)
}

func returnRatioFromOdds(odds []float64) float64 {
	if len(odds) < 3 || odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return 0
	}
	return 1 / (1/odds[0] + 1/odds[1] + 1/odds[2])
}

func parsePankouLine(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if parsed, err := strconv.ParseFloat(value, 64); err == nil {
		return parsed
	}

	negative := strings.Contains(value, "受")
	cleaned := strings.ReplaceAll(value, "受", "")
	mapping := map[string]float64{
		"平手": 0, "平": 0, "平/半": 0.25, "半": 0.5, "半球": 0.5, "半/一": 0.75, "半球/一球": 0.75, "一球": 1,
		"一/球半": 1.25, "一球/球半": 1.25, "一球/一球半": 1.25, "球半": 1.5, "一球半": 1.5, "球半/两": 1.75, "球半/两球": 1.75, "一球半/二球": 1.75, "两球": 2, "二球": 2, "两/两半": 2.25,
		"两球/两球半": 2.25, "二球/二球半": 2.25, "两半": 2.5, "两球半": 2.5, "二球半": 2.5, "两半/三": 2.75, "两球半/三球": 2.75, "二球半/三球": 2.75,
		"三球": 3, "三/三半": 3.25, "三球/三球半": 3.25, "三半": 3.5, "三球半": 3.5, "三球半/四球": 3.75, "四球": 4,
	}
	if result, ok := mapping[cleaned]; ok {
		if negative {
			return -result
		}
		return result
	}
	if strings.Contains(cleaned, "/") {
		parts := strings.Split(cleaned, "/")
		total := 0.0
		valid := 0
		for _, part := range parts {
			parsed := parsePankouLine(part)
			if parsed == 0 && strings.TrimSpace(part) != "平" && strings.TrimSpace(part) != "平手" {
				continue
			}
			total += parsed
			valid++
		}
		if valid == len(parts) && valid > 0 {
			result := total / float64(valid)
			if negative && result > 0 {
				return -result
			}
			return result
		}
	}

	number := regexp.MustCompile(`-?\d+(?:\.\d+)?`).FindString(value)
	if number == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0
	}
	if negative && parsed > 0 {
		return -parsed
	}
	return parsed
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func clampRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func normalizeSpace(value string) string {
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func stripHTML(value string) string {
	return regexp.MustCompile(`<[^>]+>`).ReplaceAllString(value, " ")
}

func firstRegexGroup(value string, pattern string) string {
	matches := regexp.MustCompile(pattern).FindStringSubmatch(value)
	for i := 1; i < len(matches); i++ {
		if strings.TrimSpace(matches[i]) != "" {
			return html.UnescapeString(matches[i])
		}
	}
	return ""
}

func regexGroups(value string, pattern string, limit int) []string {
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(value, limit)
	result := []string{}
	for _, match := range matches {
		for i := 1; i < len(match); i++ {
			if strings.TrimSpace(match[i]) != "" {
				result = append(result, html.UnescapeString(match[i]))
				break
			}
		}
	}
	return result
}

func normalizeSearchURL(value string) string {
	value = html.UnescapeString(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	redirect := parsed.Query().Get("uddg")
	if redirect != "" {
		return redirect
	}
	return value
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.0f%%", value)
}

func parseFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "%")), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0
	}
	return parsed
}

func safeDivide(value float64, divisor float64) float64 {
	if divisor == 0 {
		return 0
	}
	return value / divisor
}

func clamp(value float64, minValue float64, maxValue float64) float64 {
	return math.Max(minValue, math.Min(maxValue, value))
}

func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a float64, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func cloneInterfaceSlice(values []interface{}) []interface{} {
	out := make([]interface{}, len(values))
	copy(out, values)
	return out
}
