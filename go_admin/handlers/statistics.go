package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go_admin/database"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// statisticsDB returns a session with SQL logging disabled. The statistics
// queries carry `IN (...~2000 ids...)` clauses; logging them floods stdout
// (and freezes the IDE debug console) for no benefit.
func statisticsDB() *gorm.DB {
	return database.DB.Session(&gorm.Session{Logger: gormlogger.Discard})
}

// statisticsRecomputeMu serializes the heavy full-table recomputes so stacked
// refresh clicks cannot pile up concurrent multi-hundred-MB computations.
var statisticsRecomputeMu sync.Mutex

// Column lists for the side tables: only what the signal builders actually read.
// history_moneys in particular carries several unused multi-KB JSON columns
// (future_*, rank_data, *_summary) that would double the working set.
const (
	statisticsMoneysColumns  = "match_id, date, match_time, home, guest, home_score, guest_score, status, display_state, league, home_logo, guest_logo"
	statisticsHistoryColumns = "match_id, against_list, recent_home_list, recent_guest_list, league_stat"
	statisticsPankouColumns  = "match_id, bet365_asia, bet365_dxq, asia_data, dxq_data"
	statisticsOddsColumns    = "match_id, avg_odds, pinnacle, bet365, william, sporttery_trade, data"
)

// computeMatchStatistics builds the full report for the given date range.
func computeMatchStatistics(start, end string) (gin.H, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		return nil, err
	}
	matches := make([]statisticsMatch, 0, len(rawMatches))
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if !match.Settled || match.ID == "" || (start != "" && match.Date < start) || (end != "" && match.Date > end) {
			continue
		}
		matches = append(matches, match)
		ids = append(ids, match.ID)
	}

	historyByMatch := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankouByMatch := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	oddsByMatch := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)
	report := buildMatchStatistics(matches, historyByMatch, pankouByMatch, oddsByMatch)
	if signals, ok := report["signals"].([]gin.H); ok {
		pickSignals := buildPickSignals(matches, historyByMatch, pankouByMatch, oddsByMatch)
		signals = append(signals, buildWarningSignals(matches, historyByMatch, pankouByMatch, oddsByMatch))
		signals = append(signals, buildDeviationSignals(matches, historyByMatch, pankouByMatch))
		signals = append(signals, buildEvilCultSignals())
		report["signals"] = annotateSignalMarkets(append(signals, pickSignals...))
	}
	report["start_date"] = start
	report["end_date"] = end
	report["generated_at"] = time.Now().Format(time.RFC3339)
	report["needs_recompute"] = false
	return report, nil
}

// 信号命中赛果分类：把每个信号归到它命中率真正结算的那个赛果，而不是它名字里
// 的玩法。#4/#5/#6 名字带“让球”，但命中=胜平负判断正确，所以归 spf；#8/#9/#10
// 由交锋/近况球数期望驱动归 goals，而按盘口热度/背离结算的大小球信号归 dxq。
var signalMarketMap = map[string]string{
	"asian_heat":         "asian",
	"goals_heat":         "dxq",
	"qiu_heat":           "dxq",
	"pro_signal":         "spf",
	"trade_comfort":      "spf",
	"sim_trade_comfort":  "spf",
	"history_handicap":   "spf",
	"recent_handicap":    "spf",
	"asian_composite":    "spf",
	"front_handicap_avg": "spf",
	"line_discrepancy":   "asian",
	"history_goals":      "goals",
	"recent_goals":       "goals",
	"goals_composite":    "goals",
	"goals_discrepancy":  "dxq",
	"warning_signals":    "mixed",
	"deviation_signals":  "mixed",
	"evil_cult":          "mixed",
	"base_spf":           "spf",
	"base_qiu":           "dxq",
	"goals_align_under":  "dxq",
	"goals_align_over":   "dxq",
	"goals_split_over":   "dxq",
	"goals_split_under":  "dxq",
	"qiu_align_under":    "dxq",
	"qiu_align_over":     "dxq",
	"qiu_split_over":     "dxq",
	"qiu_split_under":    "dxq",
	"goals_rounded":      "dxq",
	"goals_diff_band":    "dxq",
}

// annotateSignalMarkets 给每个信号贴上 market（命中赛果分类）字段，并按赛前方向
// 拆分命中率。未登记的信号归入 mixed。
func annotateSignalMarkets(signals []gin.H) []gin.H {
	for _, sig := range signals {
		key, _ := sig["key"].(string)
		market := signalMarketMap[key]
		if market == "" {
			market = "mixed"
		}
		sig["market"] = market
		// 按赛前方向拆分：赛前同主胜/平/客胜（或大/小、主/客赢盘）各一列，各自命中率。
		if details, ok := sig["matches"].([]statisticsDetail); ok {
			if rows := statisticsDirectionBreakdown(details); len(rows) >= 2 {
				sig["directions"] = rows
			}
		}
	}
	return signals
}

// statisticsDirectionOrder 常见方向标签的展示顺序（胜→平→负、大→小、主→客）。
var statisticsDirectionOrder = map[string]int{
	"主胜": 1, "平局": 2, "客胜": 3,
	"大球": 1, "小球": 2,
	"主队赢盘": 1, "客队赢盘": 2,
}

// statisticsDirectionBreakdown 把一个信号的明细按 赛前方向(Pick)×盘口线(Line) 分组：
// 盘口类信号每个 方向+线 一行（大球×2.5、小球×3 …），无盘口的信号按方向一行，各自
// 统计场次/命中/命中率。盘口线就是该行统计的结算依据。
func statisticsDirectionBreakdown(details []statisticsDetail) []gin.H {
	type directionKey struct{ pick, line string }
	type directionTally struct{ matched, hit int }
	byKey := map[directionKey]*directionTally{}
	order := []directionKey{}
	for _, d := range details {
		key := directionKey{d.Pick, d.Line}
		tally := byKey[key]
		if tally == nil {
			tally = &directionTally{}
			byKey[key] = tally
			order = append(order, key)
		}
		tally.matched++
		if d.Hit {
			tally.hit++
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		oa, aKnown := statisticsDirectionOrder[a.pick]
		ob, bKnown := statisticsDirectionOrder[b.pick]
		if aKnown != bKnown {
			return aKnown
		}
		if a.pick != b.pick {
			if aKnown {
				return oa < ob
			}
			return byKey[a].matched > byKey[b].matched
		}
		// 同方向 → 按盘口线升序
		la, errA := strconv.ParseFloat(a.line, 64)
		lb, errB := strconv.ParseFloat(b.line, 64)
		if errA == nil && errB == nil {
			return la < lb
		}
		return a.line < b.line
	})
	rows := make([]gin.H, 0, len(order))
	for _, key := range order {
		tally := byKey[key]
		accuracy := math.Round(float64(tally.hit)/float64(tally.matched)*10000) / 100
		rows = append(rows, gin.H{
			"pick": key.pick, "line": key.line,
			"matched": tally.matched, "hit": tally.hit,
			"miss": tally.matched - tally.hit, "accuracy": accuracy,
		})
	}
	return rows
}

// GetMatchStatistics serves the base accuracy report. The default (no date
// range) view is MANUALLY computed: refresh=1 recomputes and persists to
// stat_snapshots; plain loads read the stored snapshot. Explicit date ranges
// still compute live (they are ad-hoc queries and are not cached).
func GetMatchStatistics(c *gin.Context) {
	start, end, err := statisticsDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 YYYY-MM-DD"})
		return
	}

	// Ad-hoc date range: compute live, never touches the snapshot.
	if start != "" || end != "" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "统计计算正在进行中，请稍候再试"})
			return
		}
		report, err := computeMatchStatistics(start, end)
		statisticsRecomputeMu.Unlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, report)
		return
	}

	if c.Query("refresh") == "1" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "重算正在进行中，请稍候再试"})
			return
		}
		defer statisticsRecomputeMu.Unlock()
		report, err := computeMatchStatistics("", "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		payload, err := json.Marshal(report)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := saveStatSnapshot(snapshotKindMatchStatistics, payload, time.Now()); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
		return
	}

	if payload, _, ok := loadStatSnapshot(snapshotKindMatchStatistics); ok {
		c.Data(http.StatusOK, "application/json; charset=utf-8", payload)
		return
	}
	c.JSON(http.StatusOK, gin.H{"needs_recompute": true, "settled_total": 0, "signals": []gin.H{}})
}

type statisticsMatch struct {
	ID, Date, Home, Guest string
	HomeScore, GuestScore int
	State                 string
	League                string
	MatchTime             string
	HomeLogo, GuestLogo   string
	Settled               bool
}

type statisticsHistoryMatch struct {
	Date, Home, Guest     string
	HomeScore, GuestScore int
}

type statisticsTeamForm struct {
	For, Against, Matches float64
}

// Tuning knobs for the directional signals. Kept as named constants so the
// thresholds are easy to review and adjust without hunting through the logic.
const (
	statisticsHandicapBand    = 0.25 // |让球期望| ≤ 此值算平局，否则算主/客
	statisticsGoalDiscrepancy = 0.75 // #7 / #11 期望与盘口的最小背离
	statisticsPushEpsilon     = 0.001
	// 期望球数综合均值的历史权重（近期权重=1-它）。必须与 go_server 的
	// combinedGoalAverage / pfCombinedGoalAverage 一致，否则统计和 H5 对不上。
	statisticsGoalsHistoryWeight = 0.3
)

var statisticsHeatTiers = []int{90, 85, 80, 75, 70, 65, 60}

// 大小球热度单独多一个55档：热度=50+(期望-盘口)×18，60档要求偏离≥0.56球，
// 庄家开盘贴着期望开导致大多数比赛进不了档；55档(偏离≥0.28球)扩大覆盖面。
var statisticsGoalsHeatTiers = []int{90, 85, 80, 75, 70, 65, 60, 55}

// statisticsDetail is one drill-down row: the completed match plus what the
// signal picked and whether it hit.
type statisticsDetail struct {
	MatchID    string  `json:"match_id"`
	Date       string  `json:"date"`
	MatchTime  string  `json:"match_time"`
	League     string  `json:"league"`
	Home       string  `json:"home"`
	Guest      string  `json:"guest"`
	HomeLogo   string  `json:"home_logo"`
	GuestLogo  string  `json:"guest_logo"`
	HomeScore  int     `json:"home_score"`
	GuestScore int     `json:"guest_score"`
	State      string  `json:"state"`
	Pick       string  `json:"pick"`
	Result     string  `json:"result"`
	Hit        bool    `json:"hit"`
	Value      float64 `json:"value"`
	// Line 是该场结算用的盘口线（大小球线/亚盘线），仅盘口类信号填写。
	Line string `json:"line,omitempty"`
	// ExpGoals/HeatGoals 仅大小球方向组(#15~#22)填写：期望球数与对照信号各自的
	// 方向+数值。HeatGoals 带信号名前缀（「热度 …」/「倾向 …」），因为这一列的
	// 来源随信号而变。明细里并排看就知道这场是同向还是反向。
	ExpGoals  string `json:"exp_goals,omitempty"`
	HeatGoals string `json:"heat_goals,omitempty"`
}

// statisticsFormatLine 把盘口线格式化成简洁字符串（去掉多余的0）。
func statisticsFormatLine(line float64) string {
	s := strconv.FormatFloat(line, 'f', 2, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// statisticsSignal accumulates the matches that satisfied one condition.
type statisticsSignal struct {
	details []statisticsDetail
	hit     int
}

func (s *statisticsSignal) add(d statisticsDetail) {
	s.details = append(s.details, d)
	if d.Hit {
		s.hit++
	}
}

func (s *statisticsSignal) accuracy() float64 {
	if len(s.details) == 0 {
		return 0
	}
	return math.Round(float64(s.hit)/float64(len(s.details))*10000) / 100
}

func (s *statisticsSignal) list() []statisticsDetail {
	if s.details == nil {
		return []statisticsDetail{}
	}
	return s.details
}

func (s *statisticsSignal) payload(key, title, definition string) gin.H {
	return gin.H{
		"key": key, "title": title, "definition": definition,
		"matched": len(s.details), "hit": s.hit, "miss": len(s.details) - s.hit,
		"accuracy": s.accuracy(), "matches": s.list(),
	}
}

func statisticsRound2(value float64) float64 { return math.Round(value*100) / 100 }

func statisticsOutcomeLabel(outcome string) string {
	switch outcome {
	case "home":
		return "主胜"
	case "away":
		return "客胜"
	default:
		return "平局"
	}
}

func buildMatchStatistics(matches []statisticsMatch, histories, pankous, odds map[string]map[string]interface{}) gin.H {
	return buildSignalStatistics(matches, histories, pankous, odds)
}

// buildSignalStatistics walks every completed match once and files it under each
// signal whose condition it satisfies. Every signal reports how many matches it
// matched, how many it got right, and the full drill-down list.
func buildSignalStatistics(matches []statisticsMatch, histories, pankous, odds map[string]map[string]interface{}) gin.H {
	// 亚盘热度按 档位×方向(朝主队/朝客队) 双维分桶。
	asianHeatHome := map[int]*statisticsSignal{}
	asianHeatGuest := map[int]*statisticsSignal{}
	for _, tier := range statisticsHeatTiers {
		asianHeatHome[tier] = &statisticsSignal{}
		asianHeatGuest[tier] = &statisticsSignal{}
	}
	// 大小球热度按 档位×方向(判大/判小) 双维分桶。
	goalsHeatOver := map[int]*statisticsSignal{}
	goalsHeatUnder := map[int]*statisticsSignal{}
	// 前端球数倾向同样分档：公式与热度一致，只是驱动值换成「只看近期」的均值。
	qiuHeatOver := map[int]*statisticsSignal{}
	qiuHeatUnder := map[int]*statisticsSignal{}
	for _, tier := range statisticsGoalsHeatTiers {
		goalsHeatOver[tier] = &statisticsSignal{}
		goalsHeatUnder[tier] = &statisticsSignal{}
		qiuHeatOver[tier] = &statisticsSignal{}
		qiuHeatUnder[tier] = &statisticsSignal{}
	}
	proSignal := &statisticsSignal{}
	tradeComfort := &statisticsSignal{}
	simTradeComfort := &statisticsSignal{}
	historyHandicap := &statisticsSignal{}
	recentHandicap := &statisticsSignal{}
	asianComposite := &statisticsSignal{}
	frontHandicapAvg := &statisticsSignal{}
	lineDiscrepancy := &statisticsSignal{}
	historyGoalsSig := &statisticsSignal{}
	recentGoalsSig := &statisticsSignal{}
	goalsComposite := &statisticsSignal{}
	goalsDiscrepancy := &statisticsSignal{}
	// #15~#18：期望球数与球数热度的方向关系，同向/反向 × 热度判大/判小四种组合各
	// 一行。四行互斥且穷尽，合起来就是全部样本齐全且不走盘的比赛。
	goalsAlignUnder := &statisticsSignal{} // 同向且热度判小球
	goalsAlignOver := &statisticsSignal{}  // 同向且热度判大球
	goalsSplitOver := &statisticsSignal{}  // 反向且热度判大球
	goalsSplitUnder := &statisticsSignal{} // 反向且热度判小球
	// #19~#22：同样四种组合，但对照信号换成前端球数倾向（只看近期）。
	qiuAlignUnder := &statisticsSignal{}
	qiuAlignOver := &statisticsSignal{}
	qiuSplitOver := &statisticsSignal{}
	qiuSplitUnder := &statisticsSignal{}
	// #23：期望球数先四舍五入成整数球数，再与盘口比大小。
	goalsRounded := &statisticsSignal{}
	// #24：同一批比赛，按「期望球数原值 − 盘口」的差值每 0.1 球一档分桶。
	// key = 差值下界的十分位（如 +0.20~+0.30 这档 key 为 2，-0.30~-0.20 为 -3）。
	goalsDiffBuckets := map[int]*statisticsSignal{}

	for _, match := range matches {
		history := histories[match.ID]
		pankou := pankous[match.ID]
		oddsRow := odds[match.ID]
		ahFirstLine, ahLine, hasAH := statisticsPankouLinePair(pankou, "bet365_asia", "asia_data")
		ouLine, hasOU := statisticsPankouLine(pankou, "bet365_dxq", "dxq_data")
		against, homeRecent, guestRecent := statisticsHistory(history)
		historyDiff, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		homeForm := statisticsRecentForm(homeRecent, match.Home)
		guestForm := statisticsRecentForm(guestRecent, match.Guest)
		recentDiff, hasRecentDiff := statisticsRecentDifference(homeForm, guestForm)
		recentGoals, hasRecentGoals := statisticsRecentGoals(homeRecent, guestRecent)
		probabilities := statisticsProbabilities(oddsRow)

		// 1a. Asian betting heat, bucketed into a single (non-overlapping) tier.
		// Identical to the frontend pressurePair, including the line-movement term.
		if hasAH && len(probabilities) == 3 {
			if correct, valid := statisticsAsianCorrect(match, ahLine); valid {
				statisticsFileAsianHeat(asianHeatHome, asianHeatGuest, match, correct,
					statisticsAsianHeat(probabilities[0], probabilities[2], ahFirstLine, ahLine), ahLine)
			}
		}
		// 1b. Over/under betting heat, bucketed by tier × direction (判大/判小).
		if hasOU && (hasRecentGoals || hasHistory) {
			if over, valid := statisticsOverOutcome(match, ouLine); valid {
				expected := statisticsMean(recentGoals, hasRecentGoals, historyGoals, hasHistory)
				overHeat := statisticsClamp(50+(expected-ouLine)*18, 0, 100)
				heat := math.Max(overHeat, 100-overHeat)
				if tier, ok := statisticsHeatTierIn(statisticsGoalsHeatTiers, heat); ok {
					pickOver := overHeat >= 50
					detail := statisticsBaseDetail(match)
					detail.Value = statisticsRound2(heat)
					detail.Line = statisticsFormatLine(ouLine)
					detail.Pick = statisticsOverLabel(pickOver)
					detail.Result = statisticsOverLabel(over)
					detail.Hit = pickOver == over
					if pickOver {
						goalsHeatOver[tier].add(detail)
					} else {
						goalsHeatUnder[tier].add(detail)
					}
				}
			}
		}
		// 1c. 前端球数倾向分档：公式与 1b 完全一致（clamp(50+(值-盘口)×18) 后取
		// 大/小两侧较大值分档），唯一差别是驱动值——这里用「只看近期战绩」的总进球
		// 均值，不掺交锋。原来的球数倾向用「压力差<5 就不表态」，换成分档后门槛更
		// 严：55 档已经要求偏离盘口≥0.28 球，而压力差 5 只相当于偏离 0.14 球。
		if hasOU {
			if recentTotal, ok := statisticsRecentTotalGoals(homeForm, guestForm); ok {
				if over, valid := statisticsOverOutcome(match, ouLine); valid {
					overHeat := statisticsClamp(50+(recentTotal-ouLine)*18, 0, 100)
					heat := math.Max(overHeat, 100-overHeat)
					if tier, ok := statisticsHeatTierIn(statisticsGoalsHeatTiers, heat); ok {
						pickOver := overHeat >= 50
						detail := statisticsBaseDetail(match)
						detail.Value = statisticsRound2(heat)
						detail.Line = statisticsFormatLine(ouLine)
						detail.Pick = statisticsOverLabel(pickOver)
						detail.Result = statisticsOverLabel(over)
						detail.Hit = pickOver == over
						if pickOver {
							qiuHeatOver[tier].add(detail)
						} else {
							qiuHeatUnder[tier].add(detail)
						}
					}
				}
			}
		}

		// 2. Professional signal: Kelly and Sporttery proxies agree on a direction.
		if choices := statisticsKellySportteryChoices(oddsRow); len(choices) > 0 {
			actual := statisticsActualOutcome(match)
			detail := statisticsBaseDetail(match)
			detail.Pick = statisticsChoiceLabel(choices)
			detail.Result = statisticsOutcomeLabel(actual)
			detail.Hit = choices[actual]
			proSignal.add(detail)
		}

		// 3. Trade profit alignment (Sporttery only): 胜平负 & 让球 most-comfortable side agree.
		if dir, ok := statisticsBookmakerComfort(statisticsValue(oddsRow, "sporttery_trade", "sportteryTrade")); ok {
			actual := statisticsActualOutcome(match)
			detail := statisticsBaseDetail(match)
			detail.Pick = statisticsOutcomeLabel(dir)
			detail.Result = statisticsOutcomeLabel(actual)
			detail.Hit = dir == actual
			tradeComfort.add(detail)
		}

		// 3s. Simulated trade profit alignment: 竞彩模拟(胜平负) & 让球模拟 most-comfortable
		// side agree. Rebuilt locally from avg 欧赔 + 盘口 + 交锋/近况 (no official 竞彩 data),
		// so it also covers 非竞彩 fixtures the official #3 skips.
		if dir, ok := statisticsSimulatedComfort(oddsRow, pankou, history, match); ok {
			actual := statisticsActualOutcome(match)
			detail := statisticsBaseDetail(match)
			detail.Pick = statisticsOutcomeLabel(dir)
			detail.Result = statisticsOutcomeLabel(actual)
			detail.Hit = dir == actual
			simTradeComfort.add(detail)
		}

		// 4-6. Handicap expectations, each read as a home/draw/away call.
		if hasHistory {
			statisticsOutcomeSignal(historyHandicap, match, historyDiff)
		}
		if hasRecentDiff {
			statisticsOutcomeSignal(recentHandicap, match, recentDiff)
		}
		if composite, ok := statisticsAverage(historyDiff, hasHistory, recentDiff, hasRecentDiff, ahLine, hasAH); ok {
			statisticsOutcomeSignal(asianComposite, match, composite)
		}
		// 6b. 前端 H5「期望让球·综合均值」原样复刻：只取历史与近期的等权平均，
		// 不含亚盘线（这是它与 #6 的唯一差别）。go_server 缺样本时会拿 0 参与
		// 平均，这里改成缺任一样本就不纳入统计。
		if hasHistory && hasRecentDiff {
			statisticsOutcomeSignal(frontHandicapAvg, match, (historyDiff+recentDiff)/2)
		}

		// 7. Current Asian line diverges from both history and recent form by ≥0.75.
		if hasAH && hasHistory && hasRecentDiff {
			diffHistory := ahLine - historyDiff
			diffRecent := ahLine - recentDiff
			fired, pickHome := false, false
			if diffHistory >= statisticsGoalDiscrepancy && diffRecent >= statisticsGoalDiscrepancy {
				fired = true // 盘口高估主队 → 站客队赢盘
			} else if diffHistory <= -statisticsGoalDiscrepancy && diffRecent <= -statisticsGoalDiscrepancy {
				fired, pickHome = true, true
			}
			if fired {
				if correct, valid := statisticsAsianCorrect(match, ahLine); valid {
					detail := statisticsBaseDetail(match)
					detail.Value = statisticsRound2(math.Min(math.Abs(diffHistory), math.Abs(diffRecent)))
					detail.Line = statisticsFormatLine(ahLine)
					detail.Pick = statisticsCoverLabel(pickHome)
					detail.Result = statisticsCoverLabel(correct)
					detail.Hit = pickHome == correct
					lineDiscrepancy.add(detail)
				}
			}
		}

		// 8-11. Goal expectations vs the current O/U line.
		if hasOU {
			if hasHistory {
				statisticsGoalSignal(historyGoalsSig, match, historyGoals, ouLine)
			}
			if hasRecentGoals {
				statisticsGoalSignal(recentGoalsSig, match, recentGoals, ouLine)
			}
			// 15~18. 期望球数与球数热度的方向关系，按 同向/反向 × 热度判大/判小
			// 拆成四行。期望球数=0.3×历史+0.7×近期（缺一侧时用另一侧顶上，不按 0
			// 顶替），热度侧用等权均值；推荐方向一律跟热度。高于盘口一丝就算判大球
			//（3.6 对盘口 3.5），不设最小偏离、也不要求热度进档。
			// 两队没有交锋记录的比赛整场剔除：期望球数 0.7 的权重就压在交锋上，
			// 缺了它算出来的不是同一个口径。近期样本缺失时用交锋单独顶上。
			if hasHistory {
				expected, _ := statisticsGoalsExpected(historyGoals, hasHistory, recentGoals, hasRecentGoals)
				heatValue := statisticsMean(recentGoals, hasRecentGoals, historyGoals, hasHistory)
				heatOver := heatValue >= ouLine
				over, valid := statisticsOverOutcome(match, ouLine)
				// 期望正好落在盘口线上就没有方向可跟；走盘同样不参与结算。
				if valid && math.Abs(expected-ouLine) >= statisticsPushEpsilon {
					expectedOver := expected > ouLine
					aligned := heatOver == expectedOver
					detail := statisticsBaseDetail(match)
					detail.Value = statisticsRound2(heatValue)
					detail.Line = statisticsFormatLine(ouLine)
					detail.Pick = statisticsOverLabel(heatOver)
					detail.Result = statisticsOverLabel(over)
					detail.Hit = heatOver == over
					detail.ExpGoals = statisticsGoalsDirText(expected, expectedOver)
					detail.HeatGoals = "热度 " + statisticsGoalsDirText(heatValue, heatOver)
					switch {
					case aligned && !heatOver:
						goalsAlignUnder.add(detail)
					case aligned && heatOver:
						goalsAlignOver.add(detail)
					case !aligned && heatOver:
						goalsSplitOver.add(detail)
					default:
						goalsSplitUnder.add(detail)
					}

					// 19~22. 同一批比赛换个对照信号：把球数热度换成【前端球数倾向】
					// （只看近期战绩的总进球均值），同样按 同向/反向 × 判大/判小 分四行，
					// 推荐一律跟倾向。倾向与期望球数的差 = 0.7×|历史-近期|，比热度那
					// 一对（0.2×|历史-近期|）宽得多，所以反向两行的样本会明显更多。
					if recentTotal, ok := statisticsRecentTotalGoals(homeForm, guestForm); ok {
						qiuOver := recentTotal >= ouLine
						qiuDetail := detail
						qiuDetail.Value = statisticsRound2(recentTotal)
						qiuDetail.Pick = statisticsOverLabel(qiuOver)
						qiuDetail.Hit = qiuOver == over
						qiuDetail.HeatGoals = "倾向 " + statisticsGoalsDirText(recentTotal, qiuOver)
						switch qiuAligned := qiuOver == expectedOver; {
						case qiuAligned && !qiuOver:
							qiuAlignUnder.add(qiuDetail)
						case qiuAligned && qiuOver:
							qiuAlignOver.add(qiuDetail)
						case !qiuAligned && qiuOver:
							qiuSplitOver.add(qiuDetail)
						default:
							qiuSplitUnder.add(qiuDetail)
						}
					}
				}

				// 23. 期望球数先换算成真实球数（四舍五入并夹到 0~8）再与盘口比：
				// 整数球数 ≥ 盘口判大球（相等也算大球），< 盘口判小球。这一行不看
				// 走盘 epsilon——取整后本来就可能正好等于整数盘口，那种情况按大球处理。
				if over, valid := statisticsOverOutcome(match, ouLine); valid {
					rounded := statisticsClamp(math.Round(expected), 0, 8)
					pickOver := rounded >= ouLine
					detail := statisticsBaseDetail(match)
					detail.Value = rounded
					detail.Line = statisticsFormatLine(ouLine)
					detail.Pick = statisticsOverLabel(pickOver)
					detail.Result = statisticsOverLabel(over)
					detail.Hit = pickOver == over
					detail.ExpGoals = fmt.Sprintf("期望 %.2f → %.0f 球", expected, rounded)
					// 差值用【原值】减盘口，不用取整值：取整会把 2.6 和 3.4 都压成 3，
					// 差值若也跟着取整就看不出这场到底贴不贴盘了。
					// 先四舍五入到 2 位再消掉浮点负零——期望正好等于盘口时
					// 3.0-3.0 可能算出 -4.4e-16，直接格式化会显示成「-0.00」，
					// 看着像低于盘口，而实际是正好贴线。
					diff := statisticsRound2(expected - ouLine)
					if diff == 0 {
						diff = 0
					}
					detail.HeatGoals = fmt.Sprintf("差值 %+.2f", diff)
					goalsRounded.add(detail)
					// 24. 同一场再按差值每 0.1 球一档归桶。用整数分（cents）算档位，
					// 避免 0.3*10 在浮点下变成 2.9999… 而掉进上一档。
					step := statisticsDiffStep(diff)
					if goalsDiffBuckets[step] == nil {
						goalsDiffBuckets[step] = &statisticsSignal{}
					}
					goalsDiffBuckets[step].add(detail)
				}
			}
			if composite, ok := statisticsAverage(historyGoals, hasHistory, recentGoals, hasRecentGoals); ok {
				statisticsGoalSignal(goalsComposite, match, composite, ouLine)
				if composite-ouLine >= statisticsGoalDiscrepancy {
					if over, valid := statisticsOverOutcome(match, ouLine); valid {
						detail := statisticsBaseDetail(match)
						detail.Value = statisticsRound2(composite - ouLine)
						detail.Line = statisticsFormatLine(ouLine)
						detail.Pick = statisticsOverLabel(true)
						detail.Result = statisticsOverLabel(over)
						detail.Hit = over
						goalsDiscrepancy.add(detail)
					}
				}
			}
		}
	}

	signals := []gin.H{
		statisticsDirectionalHeatPayload("asian_heat", "8. 亚盘投注热度分档（档位×主客方向）", "热度=平衡点 + (主队胜负份额-50)×1.4 - 即时盘×8 - 盘口移动×1.5，与前端 pressurePair 完全一致，并按热度朝主队/朝客队方向拆分；盘口移动项是扩散到高档的主因。命中=热度方向赢盘。", statisticsHeatTiers, []statisticsHeatDirection{
			{suffix: "home", label: "朝主队", buckets: asianHeatHome},
			{suffix: "guest", label: "朝客队", buckets: asianHeatGuest},
		}),
		statisticsDirectionalHeatPayload("goals_heat", "13. 大小球投注热度分档（档位×大小方向）", "按大小球投注热度(大/小压力较大值)分档并按判大球/判小球方向拆分；热度=50+(期望球数-盘口)×18，55档要求期望偏离盘口≥0.28球，60档≥0.56球；命中=大/小方向正确。", statisticsGoalsHeatTiers, []statisticsHeatDirection{
			{suffix: "over", label: "判大球", buckets: goalsHeatOver},
			{suffix: "under", label: "判小球", buckets: goalsHeatUnder},
		}),
		statisticsDirectionalHeatPayload("qiu_heat", "13c. 前端球数倾向分档（档位×大小方向）", "与 #13 同一套公式：倾向值=clamp(50+(近期总进球均值-盘口)×18)，取大/小两侧较大值分档并按判大球/判小球拆分；命中=大/小方向正确。唯一差别是驱动值——本行只看【近期战绩】（主客各最近5场的进球+失球总平均），完全不含交锋历史，而 #13 用的是交锋与近期的等权均值。两行对照即可看出「掺不掺交锋」对大小球判断到底有没有帮助。注：原来的球数倾向以「压力差<5 就不表态」为门槛，换成分档后 55 档已要求偏离盘口≥0.28 球，比原门槛(≈0.14球)严一倍。", statisticsGoalsHeatTiers, []statisticsHeatDirection{
			{suffix: "over", label: "判大球", buckets: qiuHeatOver},
			{suffix: "under", label: "判小球", buckets: qiuHeatUnder},
		}),
		proSignal.payload("pro_signal", "1. 专业信号（凯利×体彩同向）", "与前台H5专业信号同口径：凯利取凯利值最小的一个方向、体彩取威廉差值最小的一个方向，两者同向时纳入；命中=该方向即实际赛果。"),
		tradeComfort.payload("trade_comfort", "2. 交易盈亏同向（庄家舒服）", "仅体彩比赛；胜平负交易盈亏与让球交易盈亏最舒服方向一致且均为庄家盈利；命中=该方向即实际赛果。"),
		simTradeComfort.payload("sim_trade_comfort", "3. 模拟交易盈亏同向（庄家舒服）", "本地模拟盘：竞彩模拟对比(胜平负)与竞彩让球模拟由平均欧赔+加权散户心理+泊松让球推算（不含竞彩官方数据，覆盖非竞彩场次）；两者最舒服方向一致且均为庄家盈利时纳入，命中=该方向即实际赛果。"),
		historyHandicap.payload("history_handicap", "4. 历史期望让球", "赛前3年内交锋净胜球期望；|期望|≤0.25判平，否则判主/客；命中=胜平负判断正确。"),
		recentHandicap.payload("recent_handicap", "5. 近期状态让球", "两队各自最近5场净胜球差；判断口径同上。"),
		asianComposite.payload("asian_composite", "6. 亚盘综合均值", "取【历史期望让球】【近期状态让球】【当前亚盘线】中有值者求平均；判断口径同上。"),
		frontHandicapAvg.payload("front_handicap_avg", "6b. 前端期望让球·综合均值（不含亚盘线）", "与 H5【期望让球】区块显示的「综合均值」同口径：(历史期望让球 + 近期状态让球) / 2，不掺当前亚盘线——这是它与 #6 的唯一差别。两项样本缺任一则不纳入统计（H5 的显示值在缺样本时会拿0参与平均，此处不复刻该行为）。|期望|≤0.25判平，否则判主/客；命中=胜平负判断正确。"),
		lineDiscrepancy.payload("line_discrepancy", "9. 亚盘即时盘背离≥0.75", "当前亚盘线较历史与近期期望同时背离≥0.75时纳入；盘口高估一方则站另一方赢盘。"),
		historyGoalsSig.payload("history_goals", "10. 历史平均球数", "赛前3年内交锋场均总进球；与当前大小球线比较判大/小；命中=大小球判断正确。"),
		recentGoalsSig.payload("recent_goals", "11. 近期平均球数", "两队最近5场场均总进球；判断口径同上。"),
		goalsComposite.payload("goals_composite", "12. 球数综合均值", "取【历史平均球数】【近期平均球数】求平均(不含盘口线)；判断口径同上。"),
		goalsDiscrepancy.payload("goals_discrepancy", "14. 期望球数高于大小球即时盘≥0.75", "球数综合均值高于当前大小球线≥0.75时纳入，判大球；命中=实际打出大球。"),
		goalsAlignUnder.payload("goals_align_under", "15. 期望球数×球数热度同向·热度判小球", goalsDirCommon+
			"本行取两者同向、且热度判小球的场次。推荐即判小球，命中=实际打出小球。"+goalsDirPartition),
		goalsAlignOver.payload("goals_align_over", "16. 期望球数×球数热度同向·热度判大球", goalsDirCommon+
			"本行取两者同向、且热度判大球的场次。推荐即判大球，命中=实际打出大球。与 #15 对比即可看出同向时买大球和买小球哪边更稳。"+goalsDirPartition),
		goalsSplitOver.payload("goals_split_over", "17. 期望球数×球数热度反向·热度判大球", goalsDirCommon+
			"本行取两者判到相反侧、且热度判大球（即期望球数判小球）的场次。推荐跟热度即判大球，命中=实际打出大球。"+goalsDirSplitNote+goalsDirPartition),
		goalsSplitUnder.payload("goals_split_under", "18. 期望球数×球数热度反向·热度判小球", goalsDirCommon+
			"本行取两者判到相反侧、且热度判小球（即期望球数判大球）的场次。推荐跟热度即判小球，命中=实际打出小球。"+goalsDirSplitNote+goalsDirPartition),
		qiuAlignUnder.payload("qiu_align_under", "19. 期望球数×前端球数倾向同向·倾向判小球", qiuDirCommon+
			"本行取两者同向、且倾向判小球的场次。推荐即判小球，命中=实际打出小球。"+qiuDirPartition),
		qiuAlignOver.payload("qiu_align_over", "20. 期望球数×前端球数倾向同向·倾向判大球", qiuDirCommon+
			"本行取两者同向、且倾向判大球的场次。推荐即判大球，命中=实际打出大球。"+qiuDirPartition),
		qiuSplitOver.payload("qiu_split_over", "21. 期望球数×前端球数倾向反向·倾向判大球", qiuDirCommon+
			"本行取两者判到相反侧、且倾向判大球（即期望球数判小球）的场次。推荐跟倾向即判大球，命中=实际打出大球。"+qiuDirPartition),
		goalsRounded.payload("goals_rounded", "23. 期望球数取整（0~8球）对盘口",
			"期望球数=0.3×历史场均+0.7×近期场均，先四舍五入成一个真实的整数球数（夹在 0~8），再拿这个整数和大小球盘口比："+
				"大于判大球，小于判小球，正好相等也判大球。命中=大小球方向正确，走盘（总进球正好等于盘口）不计入分母。"+
				"明细里列出原始期望值、取整后的球数，以及差值——差值取【期望球数原值 − 盘口】，不是取整后的球数减盘口。"+
				"因为取整会把 2.6 和 3.4 都压成 3 球，差值若也跟着取整就看不出这场到底贴不贴盘；用原值才能分清「勉强过线」和「稳稳高出」。"+
				"上方的方向拆分表按 判大/判小 × 盘口线分组，可以直接看出哪些盘口档位靠谱。两队没有交锋记录的比赛整场剔除。"),
		statisticsDiffBucketPayload("goals_diff_band", "24. 期望球数取整·按差值分档（每 0.1 球一档）",
			"与 #23 完全同一批比赛、同一套判断（取整球数对盘口，相等判大球），这里只是把它们按【期望球数原值 − 盘口】的差值每 0.1 球一档拆开看命中率，只列出有场次的档。"+
				"差值为正=期望高于盘口。注意判断用的是【取整后】的球数，所以差值为负的档里照样会有判大球的场次（如期望 2.70、盘口 3，取整成 3 与盘口相等即判大球），两者不是一回事。"+
				"看这张表是为了找出「差值多大才靠得住」的那条线。⚠️ 单档场次少时命中率就是噪声，先看场次再看百分比；相邻几档合起来看比盯着某一档更稳。",
			goalsDiffBuckets),
		qiuSplitUnder.payload("qiu_split_under", "22. 期望球数×前端球数倾向反向·倾向判小球", qiuDirCommon+
			"本行取两者判到相反侧、且倾向判小球（即期望球数判大球）的场次。推荐跟倾向即判小球，命中=实际打出小球。"+qiuDirPartition),
	}
	return gin.H{"settled_total": len(matches), "signals": signals}
}

// goalsDirCommon 是 #15/#16 共用的口径说明，两行各写一遍容易写歪。
const goalsDirCommon = "期望球数=0.3×历史场均+0.7×近期场均（两队没有交锋记录的比赛整场剔除；近期样本缺失时用交锋顶上，不按 0 顶替），高于盘口即判大球——3.6 对盘口 3.5 也算，不设最小偏离；" +
	"球数热度方向=等权综合均值对盘口的方向（热度=50+(等权均值-盘口)×18，≥50 判大球），同样不要求进档。" +
	"推荐方向一律跟球数热度，明细的数值列也是热度值，并另有两列并排显示期望球数与热度各自的方向。走盘不计。"

// qiuDirCommon 是 #19~#22 共用的口径说明。与 #15~#18 只差一个对照信号：
// 那边用球数热度（交锋与近期等权），这边用前端球数倾向（只看近期）。
const qiuDirCommon = "期望球数=0.3×历史场均+0.7×近期场均（两队没有交锋记录的比赛整场剔除）；" +
	"前端球数倾向=主客各最近5场的总进球均值对盘口的方向，完全不含交锋。" +
	"两者高于盘口即判大球，不设最小偏离、也不要求进档；推荐一律跟倾向，明细的数值列是倾向值。命中=大/小方向正确，走盘不计。" +
	"⚠️ 倾向与期望球数的差恒等于 0.3×|历史场均-近期场均|，比 #15~#18 那对（0.2×|历史-近期|）宽 1.5 倍，所以这四行的反向样本会略多于 #17/#18。"

// qiuDirPartition #19~#22 的关系说明。
const qiuDirPartition = "（#19~#22 是同向/反向 × 判大/判小的四种组合，互不重叠，合起来就是全部可算的比赛，场次可以相加；它们与 #15~#18 是同一批比赛的两种切法，不能跨组相加。）"

// goalsDirSplitNote 反向两行(#17/#18)共用的样本量提醒。
const goalsDirSplitNote = "反向只发生在盘口落进加权均值与等权均值之间的窄带，样本天然比同向两行少，先看场次再看命中率。"

// goalsDirPartition 四行的关系说明：互斥且穷尽，场次可以相加。
const goalsDirPartition = "（#15~#18 是同向/反向 × 判大/判小的四种组合，互不重叠，合起来就是全部历史+近期样本齐全且不走盘的比赛，场次可以相加。）"

// statisticsGoalsExpected 期望球数综合均值：历史与近期按 statisticsGoalsHistoryWeight
// 加权。缺一侧时不按 0 顶替，而是用另一侧单独顶上（按实际存在的权重归一化），口径与
// go_server 的 weightedAveragePointer 一致。两侧都缺才返回 false。
//
// 注意：只剩一侧样本时，加权均值必然等于等权均值（都等于那一侧本身），所以这类
// 比赛的「期望球数」与「球数热度」方向永远一致，不可能构成反向组合。
func statisticsGoalsExpected(history float64, hasHistory bool, recent float64, hasRecent bool) (float64, bool) {
	sum, weight := 0.0, 0.0
	if hasHistory {
		sum += history * statisticsGoalsHistoryWeight
		weight += statisticsGoalsHistoryWeight
	}
	if hasRecent {
		sum += recent * (1 - statisticsGoalsHistoryWeight)
		weight += 1 - statisticsGoalsHistoryWeight
	}
	if weight <= 0 {
		return 0, false
	}
	return sum / weight, true
}

// statisticsDiffStep 把差值折算成 0.1 一档的档位下标：+0.20~+0.30 → 2，
// -0.30~-0.20 → -3。走整数分再取下界，避免 0.3×10 在浮点下算成 2.9999…
// 掉进上一档（差值本身已四舍五入到 2 位）。
func statisticsDiffStep(diff float64) int {
	cents := int(math.Round(diff * 100))
	step := cents / 10
	if cents < 0 && cents%10 != 0 {
		step-- // Go 的整数除法向零取整，负数要再退一档才是下界
	}
	return step
}

// statisticsDiffBucketPayload 把差值分桶渲染成 buckets 列表（升序，只输出有场次的档）。
func statisticsDiffBucketPayload(key, title, definition string, buckets map[int]*statisticsSignal) gin.H {
	steps := make([]int, 0, len(buckets))
	for step := range buckets {
		steps = append(steps, step)
	}
	sort.Ints(steps)

	rows := make([]gin.H, 0, len(steps))
	matched, hit := 0, 0
	for _, step := range steps {
		sig := buckets[step]
		if sig == nil || len(sig.details) == 0 {
			continue
		}
		matched += len(sig.details)
		hit += sig.hit
		low := float64(step) / 10
		row := sig.payload(fmt.Sprintf("%s-%d", key, step),
			fmt.Sprintf("%+.1f ~ %+.1f 球", low, low+0.1), "")
		row["from"] = statisticsRound2(low)
		rows = append(rows, row)
	}
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	return gin.H{
		"key": key, "title": title, "definition": definition,
		"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": rows,
	}
}

// statisticsGoalsDirText 把一个球数期望渲染成「判大球 3.73」，供明细里并排展示
// 期望球数与球数热度各自的方向。
func statisticsGoalsDirText(value float64, over bool) string {
	return "判" + statisticsOverLabel(over) + " " + strconv.FormatFloat(statisticsRound2(value), 'f', 2, 64)
}

func statisticsBaseDetail(match statisticsMatch) statisticsDetail {
	return statisticsDetail{
		MatchID: match.ID, Date: match.Date, MatchTime: match.MatchTime, League: match.League,
		Home: match.Home, Guest: match.Guest, HomeLogo: match.HomeLogo, GuestLogo: match.GuestLogo,
		HomeScore: match.HomeScore, GuestScore: match.GuestScore, State: match.State,
	}
}

// statisticsFileAsianHeat buckets one Asian-heat reading (home-cover confidence)
// and records whether that side actually covered.
func statisticsFileAsianHeat(homeBuckets, guestBuckets map[int]*statisticsSignal, match statisticsMatch, homeCovered bool, homeHeat, ahLine float64) {
	heat := math.Max(homeHeat, 100-homeHeat)
	tier, ok := statisticsHeatTier(heat)
	if !ok {
		return
	}
	pickHome := homeHeat >= 50
	detail := statisticsBaseDetail(match)
	detail.Value = statisticsRound2(heat)
	detail.Line = statisticsFormatLine(ahLine)
	detail.Pick = statisticsCoverLabel(pickHome)
	detail.Result = statisticsCoverLabel(homeCovered)
	detail.Hit = pickHome == homeCovered
	if pickHome {
		homeBuckets[tier].add(detail)
	} else {
		guestBuckets[tier].add(detail)
	}
}

// statisticsHeatTier returns the highest tier the heat clears; buckets do not
// overlap, so each match lands in exactly one tier.
func statisticsHeatTier(heat float64) (int, bool) {
	return statisticsHeatTierIn(statisticsHeatTiers, heat)
}

func statisticsHeatTierIn(tiers []int, heat float64) (int, bool) {
	for _, tier := range tiers { // descending
		if heat >= float64(tier) {
			return tier, true
		}
	}
	return 0, false
}

func statisticsCoverLabel(home bool) string {
	if home {
		return "主队赢盘"
	}
	return "客队赢盘"
}

func statisticsOverLabel(over bool) string {
	if over {
		return "大球"
	}
	return "小球"
}

func statisticsChoiceLabel(choices map[string]bool) string {
	labels := make([]string, 0, 3)
	for _, key := range []string{"home", "draw", "away"} {
		if choices[key] {
			labels = append(labels, statisticsOutcomeLabel(key))
		}
	}
	return strings.Join(labels, "/")
}

// statisticsOutcomeSignal files a home/draw/away call derived from a handicap
// expectation (positive = home favoured).
func statisticsOutcomeSignal(sig *statisticsSignal, match statisticsMatch, value float64) {
	pred, _ := statisticsOutcomeFromValue(value, statisticsHandicapBand)
	actual := statisticsActualOutcome(match)
	detail := statisticsBaseDetail(match)
	detail.Value = statisticsRound2(value)
	detail.Pick = statisticsOutcomeLabel(pred)
	detail.Result = statisticsOutcomeLabel(actual)
	detail.Hit = pred == actual
	sig.add(detail)
}

// statisticsGoalSignal files an over/under call from a goals expectation against
// the current line; pushes and too-close forecasts are dropped.
func statisticsGoalSignal(sig *statisticsSignal, match statisticsMatch, value, line float64) {
	if math.Abs(value-line) < statisticsPushEpsilon {
		return
	}
	over, valid := statisticsOverOutcome(match, line)
	if !valid {
		return
	}
	predOver := value > line
	detail := statisticsBaseDetail(match)
	detail.Value = statisticsRound2(value)
	detail.Line = statisticsFormatLine(line)
	detail.Pick = statisticsOverLabel(predOver)
	detail.Result = statisticsOverLabel(over)
	detail.Hit = predOver == over
	sig.add(detail)
}

// statisticsHeatDirection pairs one direction's label with its tier buckets for
// the tier × direction heat dimensions (1a 主/客, 1b 大/小).
type statisticsHeatDirection struct {
	suffix, label string
	buckets       map[int]*statisticsSignal
}

// statisticsDirectionalHeatPayload renders a heat dimension bucketed by
// tier × direction: each tier gets one row per direction.
func statisticsDirectionalHeatPayload(key, title, definition string, tiers []int, directions []statisticsHeatDirection) gin.H {
	rows := make([]gin.H, 0, len(tiers)*len(directions))
	matched, hit := 0, 0
	for index, tier := range tiers {
		tierLabel := fmt.Sprintf("%d%% ~ %d%%", tier, tier+5)
		if index == 0 {
			tierLabel = fmt.Sprintf("≥ %d%%", tier)
		}
		for _, direction := range directions {
			sig := direction.buckets[tier]
			if sig == nil {
				sig = &statisticsSignal{}
			}
			matched += len(sig.details)
			hit += sig.hit
			row := sig.payload(fmt.Sprintf("%s-%d-%s", key, tier, direction.suffix), tierLabel+"·"+direction.label, "")
			row["tier"] = tier
			rows = append(rows, row)
		}
	}
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	return gin.H{
		"key": key, "title": title, "definition": definition,
		"matched": matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": rows,
	}
}

// statisticsBookmakerComfort reads the Sporttery trade payload and returns the
// outcome direction that is most profitable for the bookmaker when 胜平负 and
// 让球胜平负 agree on it and both are net profits (庄家舒服).
func statisticsBookmakerComfort(value interface{}) (string, bool) {
	payload, ok := statisticsJSON(value).(map[string]interface{})
	if !ok {
		return "", false
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		payload = data
	}
	spf, ok1 := statisticsJSON(payload["jyykSpf"]).(map[string]interface{})
	rq, ok2 := statisticsJSON(payload["jyykRqspf"]).(map[string]interface{})
	if !ok1 || !ok2 {
		return "", false
	}
	spfDir, spfProfit := statisticsBestProfit(spf)
	rqDir, rqProfit := statisticsBestProfit(rq)
	if spfDir == "" || spfDir != rqDir || spfProfit <= 0 || rqProfit <= 0 {
		return "", false
	}
	return spfDir, true
}

// statisticsBestProfit returns the outcome with the highest bookmaker profit
// rate (hy/dy/ay) and that rate; missing fields are skipped.
func statisticsBestProfit(row map[string]interface{}) (string, float64) {
	best, bestValue := "", math.Inf(-1)
	for _, item := range []struct{ key, dir string }{{"hy", "home"}, {"dy", "draw"}, {"ay", "away"}} {
		raw := statisticsValue(row, item.key)
		if raw == nil {
			continue
		}
		if value := statisticsNumber(raw); value > bestValue {
			best, bestValue = item.dir, value
		}
	}
	if best == "" {
		return "", 0
	}
	return best, bestValue
}

// loadStatisticsRows fetches only the listed columns, in id batches, so the
// per-query result set stays bounded as the match count grows.
func loadStatisticsRows(table, columns string, ids []string) map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}
	const batch = 500
	for start := 0; start < len(ids); start += batch {
		end := start + batch
		if end > len(ids) {
			end = len(ids)
		}
		var rows []map[string]interface{}
		if statisticsDB().Table(table).Select(columns).Where("match_id IN ?", ids[start:end]).Find(&rows).Error != nil {
			continue
		}
		for _, row := range rows {
			if id := statisticsText(statisticsValue(row, "match_id", "matchId")); id != "" {
				result[id] = row
			}
		}
	}
	return result
}

// statisticsDateTime formats a raw match_time as "2006-01-02 15:04".
func statisticsDateTime(value interface{}) string {
	if typed, ok := value.(time.Time); ok {
		return typed.Format("2006-01-02 15:04")
	}
	text := statisticsText(value)
	if len(text) >= 16 {
		return strings.ReplaceAll(text[:16], "T", " ")
	}
	return text
}

func parseStatisticsMatch(row map[string]interface{}) statisticsMatch {
	status := statisticsText(statisticsValue(row, "status"))
	display := statisticsText(statisticsValue(row, "display_state", "displayState"))
	state := display
	if strings.TrimSpace(state) == "" {
		state = status
	}
	if strings.TrimSpace(state) == "" {
		state = "完赛"
	}
	return statisticsMatch{
		ID:         statisticsText(statisticsValue(row, "match_id", "matchId")),
		Date:       statisticsDate(statisticsValue(row, "date", "match_time", "matchTime")),
		Home:       statisticsText(statisticsValue(row, "home")),
		Guest:      statisticsText(statisticsValue(row, "guest")),
		HomeScore:  int(statisticsNumber(statisticsValue(row, "home_score", "homeScore"))),
		GuestScore: int(statisticsNumber(statisticsValue(row, "guest_score", "guestScore"))),
		State:      state,
		League:     statisticsText(statisticsValue(row, "league", "league_name", "leagueName")),
		MatchTime:  statisticsDateTime(statisticsValue(row, "match_time", "matchTime")),
		HomeLogo:   statisticsText(statisticsValue(row, "home_logo", "homeLogo")),
		GuestLogo:  statisticsText(statisticsValue(row, "guest_logo", "guestLogo")),
		Settled:    strings.Contains(display, "完") || strings.Contains(status, "完") || strings.EqualFold(status, "finished") || statisticsNumber(statisticsValue(row, "status", "match_state", "matchState")) >= 4,
	}
}

func statisticsDateRange(start, end string) (string, string, error) {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	for _, value := range []string{start, end} {
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return "", "", err
			}
		}
	}
	if start != "" && end != "" && start > end {
		return "", "", fmt.Errorf("invalid range")
	}
	return start, end, nil
}

func statisticsHistory(row map[string]interface{}) (against, home, guest []statisticsHistoryMatch) {
	against = statisticsHistoryList(statisticsValue(row, "against_list", "againstList"))
	home = statisticsHistoryList(statisticsValue(row, "recent_home_list", "recentHomeList"))
	guest = statisticsHistoryList(statisticsValue(row, "recent_guest_list", "recentGuestList"))
	if len(against) > 0 || len(home) > 0 || len(guest) > 0 {
		return
	}
	payload, _ := statisticsJSON(statisticsValue(row, "league_stat", "leagueStat")).(map[string]interface{})
	if payload == nil {
		return
	}
	if item, ok := payload["against"].(map[string]interface{}); ok {
		against = statisticsHistoryList(item["list"])
	}
	if recent, ok := payload["recent"].(map[string]interface{}); ok {
		if item, ok := recent["home"].(map[string]interface{}); ok {
			home = statisticsHistoryList(item["list"])
		}
		if item, ok := recent["guest"].(map[string]interface{}); ok {
			guest = statisticsHistoryList(item["list"])
		}
	}
	return
}

func statisticsHistoryList(value interface{}) []statisticsHistoryMatch {
	items, _ := statisticsJSON(value).([]interface{})
	result := make([]statisticsHistoryMatch, 0, len(items))
	for _, value := range items {
		row, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		scores := statisticsNumbers(statisticsValue(row, "goal", "score"))
		if len(scores) < 2 {
			continue
		}
		result = append(result, statisticsHistoryMatch{Date: statisticsDate(statisticsValue(row, "matchTime", "match_time", "date")), Home: statisticsText(statisticsValue(row, "home")), Guest: statisticsText(statisticsValue(row, "guest")), HomeScore: int(scores[0]), GuestScore: int(scores[1])})
	}
	return result
}

func statisticsHeadToHead(match statisticsMatch, rows []statisticsHistoryMatch) (float64, float64, bool) {
	matchTime, err := time.Parse("2006-01-02", match.Date)
	if err != nil {
		return 0, 0, false
	}
	cutoff := matchTime.AddDate(-3, 0, 0)
	diffs, totals := []float64{}, []float64{}
	for _, row := range rows {
		date, err := time.Parse("2006-01-02", row.Date)
		if err != nil || date.Before(cutoff) || !date.Before(matchTime) {
			continue
		}
		diff := float64(row.HomeScore - row.GuestScore)
		if row.Home == match.Home && row.Guest == match.Guest {
		} else if row.Home == match.Guest && row.Guest == match.Home {
			diff = -diff
		} else {
			continue
		}
		diffs = append(diffs, diff)
		totals = append(totals, float64(row.HomeScore+row.GuestScore))
	}
	if len(diffs) == 0 {
		return 0, 0, false
	}
	return statisticsSliceMean(diffs), statisticsSliceMean(totals), true
}

func statisticsRecentForm(rows []statisticsHistoryMatch, team string) statisticsTeamForm {
	form := statisticsTeamForm{}
	for _, row := range rows {
		if form.Matches >= 5 {
			break
		}
		if row.Home == team {
			form.For += float64(row.HomeScore)
			form.Against += float64(row.GuestScore)
		} else if row.Guest == team {
			form.For += float64(row.GuestScore)
			form.Against += float64(row.HomeScore)
		} else {
			continue
		}
		form.Matches++
	}
	return form
}

// statisticsRecentTotalGoals 前端「球数倾向」用的近期总进球均值：主客两队各取最近
// 5 场，把两边的进球+失球全加起来除以总场次。它只看近期战绩、完全不含交锋历史，
// 这是它与「球数热度」（等权掺入交锋）的唯一区别。
func statisticsRecentTotalGoals(home, guest statisticsTeamForm) (float64, bool) {
	matches := home.Matches + guest.Matches
	if matches <= 0 {
		return 0, false
	}
	return (home.For + home.Against + guest.For + guest.Against) / matches, true
}

func statisticsRecentDifference(home, guest statisticsTeamForm) (float64, bool) {
	if home.Matches == 0 || guest.Matches == 0 {
		return 0, false
	}
	return (home.For-home.Against)/home.Matches - (guest.For-guest.Against)/guest.Matches, true
}
func statisticsRecentGoals(homeRows, guestRows []statisticsHistoryMatch) (float64, bool) {
	totals := []float64{}
	for _, rows := range [][]statisticsHistoryMatch{homeRows, guestRows} {
		for index, row := range rows {
			if index >= 5 {
				break
			}
			totals = append(totals, float64(row.HomeScore+row.GuestScore))
		}
	}
	if len(totals) == 0 {
		return 0, false
	}
	return statisticsSliceMean(totals), true
}

// statisticsPankouRows returns the per-company rows for a market, tolerating both
// storage shapes the crawler produced: a bare JSON array of companies (newer rows),
// or the combined object {"asia":[...],"dxq":[...]} that older rows packed into
// asia_data while leaving dxq_data null.
func statisticsPankouRows(row map[string]interface{}, rowsKey string) []interface{} {
	market := strings.TrimSuffix(rowsKey, "_data")
	if rows := statisticsMarketRows(statisticsValue(row, rowsKey), market); rows != nil {
		return rows
	}
	// 兼容旧组合格式：asia_data 为 {asia:[...], dxq:[...]} 时可从中取对应市场；
	// 但 asia_data 是普通亚盘数组时绝不能当成其他市场——否则大小球线会误读成
	// 亚盘线（如 0.5 半球），并用它错误结算大小球。
	if rowsKey != "asia_data" {
		if combined, ok := statisticsJSON(statisticsValue(row, "asia_data")).(map[string]interface{}); ok {
			if rows, ok := statisticsJSON(combined[market]).([]interface{}); ok {
				return rows
			}
		}
	}
	return nil
}

func statisticsMarketRows(value interface{}, market string) []interface{} {
	switch typed := statisticsJSON(value).(type) {
	case []interface{}:
		return typed
	case map[string]interface{}:
		if rows, ok := statisticsJSON(typed[market]).([]interface{}); ok {
			return rows
		}
	}
	return nil
}

// statisticsPankouMedian 取全部公司可解析盘口线的中位数（偶数取下中位，保持真实
// 盘口值）。bet365 缺席时的兜底：单家公司偶发脏行（如某家 0.5 混进一片 2.25 的大
// 小球盘），取第一行会拿到离群值，中位数天然稳健。
func statisticsPankouMedian(items []interface{}, keys ...string) (float64, bool) {
	lines := []float64{}
	for _, value := range items {
		if item, ok := value.(map[string]interface{}); ok {
			if line, ok := statisticsLine(statisticsText(statisticsValue(item, keys...))); ok {
				lines = append(lines, line)
			}
		}
	}
	if len(lines) == 0 {
		return 0, false
	}
	sort.Float64s(lines)
	return lines[(len(lines)-1)/2], true
}

func statisticsPankouLine(row map[string]interface{}, preferred, rowsKey string) (float64, bool) {
	if item, ok := statisticsJSON(statisticsValue(row, preferred)).(map[string]interface{}); ok {
		if line, ok := statisticsLine(statisticsText(statisticsValue(item, "pankou", "firstPankou", "first_pankou"))); ok {
			return line, true
		}
	}
	items := statisticsPankouRows(row, rowsKey)
	for _, value := range items {
		item, ok := value.(map[string]interface{})
		if !ok || int(statisticsNumber(statisticsValue(item, "companyId", "company_id"))) != 8 {
			continue
		}
		if line, ok := statisticsLine(statisticsText(statisticsValue(item, "pankou", "firstPankou", "first_pankou"))); ok {
			return line, true
		}
	}
	return statisticsPankouMedian(items, "pankou", "firstPankou", "first_pankou")
}

// statisticsPankouLinePair resolves both the opening line (firstPankou/初盘) and
// the current line (pankou/即时盘) from the same company row, using the same
// company-selection priority as statisticsPankouLine. It lets the Asian heat
// include the frontend's line-movement term. When firstPankou is missing it
// falls back to the current line (movement = 0).
func statisticsPankouLinePair(row map[string]interface{}, preferred, rowsKey string) (float64, float64, bool) {
	read := func(item map[string]interface{}) (float64, float64, bool) {
		current, ok := statisticsLine(statisticsText(statisticsValue(item, "pankou", "firstPankou", "first_pankou")))
		if !ok {
			return 0, 0, false
		}
		first, ok := statisticsLine(statisticsText(statisticsValue(item, "firstPankou", "first_pankou")))
		if !ok {
			first = current
		}
		return first, current, true
	}
	if item, ok := statisticsJSON(statisticsValue(row, preferred)).(map[string]interface{}); ok {
		if first, current, ok := read(item); ok {
			return first, current, true
		}
	}
	items := statisticsPankouRows(row, rowsKey)
	for _, value := range items {
		item, ok := value.(map[string]interface{})
		if !ok || int(statisticsNumber(statisticsValue(item, "companyId", "company_id"))) != 8 {
			continue
		}
		if first, current, ok := read(item); ok {
			return first, current, true
		}
	}
	// bet365 缺席 → 初盘/即时盘各取全公司中位数，避免第一行离群值。
	if current, ok := statisticsPankouMedian(items, "pankou", "firstPankou", "first_pankou"); ok {
		first, ok := statisticsPankouMedian(items, "firstPankou", "first_pankou")
		if !ok {
			first = current
		}
		return first, current, true
	}
	return 0, 0, false
}

// statisticsPankouTerms maps the Chinese handicap wording to its numeric line.
// Both 二/两 spellings are included because the crawler stores 二球 for O/U while
// Asian lines use 两球; combined quarter lines (含「/」) fall back to averaging the
// two adjacent single terms, so this table only needs the base terms plus the few
// combinations worth spelling out for clarity.
var statisticsPankouTerms = map[string]float64{
	"平手": 0, "平": 0,
	"半": 0.5, "半球": 0.5,
	"一球": 1,
	"球半": 1.5, "一球半": 1.5,
	"两球": 2, "二球": 2,
	"两球半": 2.5, "二球半": 2.5,
	"三球":  3,
	"三球半": 3.5, "三半": 3.5,
	"四球":  4,
	"四球半": 4.5,
	"五球":  5,
	"平/半": 0.25, "平手/半球": 0.25,
	"半/一": 0.75, "半球/一球": 0.75,
	"一/球半": 1.25, "一球/球半": 1.25, "一球/一球半": 1.25,
	"球半/两": 1.75, "球半/两球": 1.75, "一球半/二球": 1.75,
	"两/两半": 2.25, "两球/两球半": 2.25, "二球/二球半": 2.25,
	"两半/三": 2.75, "两球半/三球": 2.75, "二球半/三球": 2.75,
	"三/三半": 3.25, "三球/三球半": 3.25,
	"三球半/四球": 3.75,
	"四球/四球半": 4.25,
}

// statisticsLine converts a raw handicap string into a numeric line. It returns
// ok=false when the value cannot be resolved so callers can drop that match
// instead of mistaking an unparseable line for a pick'em (0) line.
func statisticsLine(value string) (float64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number, true
	}
	negative := strings.Contains(value, "受")
	line, ok := statisticsPankouTerm(strings.ReplaceAll(value, "受", ""))
	if !ok {
		return 0, false
	}
	if negative {
		line = -line
	}
	return line, true
}

// statisticsPankouTerm resolves a single 受-stripped term to its numeric line,
// averaging the parts of a combined line such as "两球/两球半".
func statisticsPankouTerm(term string) (float64, bool) {
	term = strings.TrimSpace(term)
	if term == "" {
		return 0, false
	}
	if number, err := strconv.ParseFloat(term, 64); err == nil {
		return number, true
	}
	if line, ok := statisticsPankouTerms[term]; ok {
		return line, true
	}
	if strings.Contains(term, "/") {
		parts := strings.Split(term, "/")
		total := 0.0
		for _, part := range parts {
			line, ok := statisticsPankouTerm(part)
			if !ok {
				return 0, false
			}
			total += line
		}
		return total / float64(len(parts)), true
	}
	return 0, false
}

func statisticsAsianCorrect(match statisticsMatch, line float64) (bool, bool) {
	result := float64(match.HomeScore-match.GuestScore) - line
	if math.Abs(result) < .001 {
		return false, false
	}
	return result > 0, true
}
func statisticsOverOutcome(match statisticsMatch, line float64) (bool, bool) {
	result := float64(match.HomeScore+match.GuestScore) - line
	if math.Abs(result) < .001 {
		return false, false
	}
	return result > 0, true
}
func statisticsActualOutcome(match statisticsMatch) string {
	if match.HomeScore > match.GuestScore {
		return "home"
	}
	if match.HomeScore < match.GuestScore {
		return "away"
	}
	return "draw"
}
func statisticsOutcomeFromValue(value, tolerance float64) (string, bool) {
	if math.Abs(value) <= tolerance {
		return "draw", true
	}
	if value > 0 {
		return "home", true
	}
	return "away", true
}
func statisticsProbabilities(row map[string]interface{}) []float64 {
	avg := statisticsOdds(statisticsValue(row, "avg_odds", "avgOdds"))
	if len(avg) < 3 {
		avg = statisticsAverageOdds(statisticsOddsRows(row))
	}
	if len(avg) < 3 || avg[0] <= 0 || avg[1] <= 0 || avg[2] <= 0 {
		return nil
	}
	total := 1/avg[0] + 1/avg[1] + 1/avg[2]
	return []float64{100 / avg[0] / total, 100 / avg[1] / total, 100 / avg[2] / total}
}

// statisticsAsianHeat mirrors the frontend pressurePair exactly:
// balance + share-strength - handicap cost - line-movement cost. The 1.4 share
// coefficient (up from the original 0.45) and the line-movement term are both
// needed to spread the heat into the high tiers — the movement term is in fact
// the dominant driver, since a line that has moved marks a hot side.
func statisticsAsianHeat(home, away, firstLine, currentLine float64) float64 {
	base := 50.0
	if home+away > 0 {
		base = home / (home + away) * 100
	}
	balance := 50.0
	if currentLine > 0 {
		balance = 55
	} else if currentLine < 0 {
		balance = 45
	}
	movement := (currentLine - firstLine) / 0.25 * 1.5
	return statisticsClamp(balance+(base-50)*1.4-currentLine*8-movement, 0, 100)
}

func statisticsKellySportteryChoices(row map[string]interface{}) map[string]bool {
	avg := statisticsOdds(statisticsValue(row, "avg_odds", "avgOdds"))
	oddsRows := statisticsOddsRows(row)
	if len(avg) < 3 {
		avg = statisticsAverageOdds(oddsRows)
	}
	if len(avg) < 3 {
		return nil
	}
	source := statisticsOdds(statisticsValue(row, "pinnacle"))
	if len(source) < 3 {
		source = statisticsFindOdds(oddsRows, "16", "")
	}
	if len(source) < 3 {
		source = statisticsOdds(statisticsValue(row, "bet365"))
	}
	if len(source) < 3 {
		source = statisticsFindOdds(oddsRows, "281", "")
	}
	if len(source) < 3 {
		return nil
	}
	labels := []string{"home", "draw", "away"}
	avgReturn := statisticsReturn(avg)
	// 凯利首选（与前台 kellyResult 同口径）：取凯利值最小的方向；
	// 打平取靠后的（胜平→平，胜负/平负→负）。
	kellyBest, kellyBestValue := -1, 0.0
	for i := 0; i < 3; i++ {
		if source[i] <= 0 || avg[i] <= 0 {
			continue
		}
		kelly := source[i] / avg[i] * avgReturn
		if kellyBest == -1 || kelly <= kellyBestValue {
			kellyBest = i
			kellyBestValue = kelly
		}
	}
	if kellyBest == -1 {
		return nil
	}

	// 体彩首选（与前台 ticaiResult 同口径）：威廉 vs 体彩官方赔率差值最小的方向；
	// 威廉或体彩缺失时回退 任一公司赔率 vs 平均欧赔。
	william := statisticsOdds(statisticsValue(row, "william"))
	if len(william) < 3 {
		william = statisticsFindOdds(oddsRows, "115", "威廉")
	}
	basis := statisticsSportteryOdds(statisticsValue(row, "sporttery_trade", "sportteryTrade"))
	if len(william) < 3 || len(basis) != 3 {
		if len(william) < 3 {
			for _, oddsRow := range oddsRows {
				if odds := statisticsOdds(oddsRow); len(odds) >= 3 {
					william = odds
					break
				}
			}
		}
		basis = avg
	}
	if len(william) < 3 {
		return nil
	}
	// 体彩差值打平同样取靠后的方向。
	ticaiBest := 0
	for i := 1; i < 3; i++ {
		if math.Abs(william[i]-basis[i]) <= math.Abs(william[ticaiBest]-basis[ticaiBest]) {
			ticaiBest = i
		}
	}

	// 前台专业信号显示 凯利X/体彩Y——两者同向才算凯体同向信号。
	if ticaiBest != kellyBest {
		return nil
	}
	return map[string]bool{labels[kellyBest]: true}
}

func statisticsSportteryOdds(value interface{}) []float64 {
	payload, ok := statisticsJSON(value).(map[string]interface{})
	if !ok {
		return nil
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		payload = data
	}
	tzbl, ok := payload["tzbl"].(map[string]interface{})
	if !ok {
		return nil
	}
	odds := []float64{statisticsNumber(tzbl["h"]), statisticsNumber(tzbl["d"]), statisticsNumber(tzbl["a"])}
	if odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return nil
	}
	return odds
}
func statisticsOddsRows(row map[string]interface{}) []map[string]interface{} {
	value := statisticsJSON(statisticsValue(row, "data"))
	items, ok := value.([]interface{})
	if !ok {
		// Most rows store the odds as {"odds":[...]} rather than a bare array,
		// same as the frontend euroOddsRows fallback. Unwrap the "odds" key so the
		// average-odds / Kelly paths cover those matches instead of dropping them.
		if obj, isObj := value.(map[string]interface{}); isObj {
			items, _ = statisticsJSON(obj["odds"]).([]interface{})
		}
	}
	result := make([]map[string]interface{}, 0, len(items))
	for _, value := range items {
		if item, ok := value.(map[string]interface{}); ok {
			result = append(result, item)
		}
	}
	return result
}
func statisticsFindOdds(rows []map[string]interface{}, id, name string) []float64 {
	for _, row := range rows {
		if statisticsText(statisticsValue(row, "companyId", "company_id")) == id || (name != "" && strings.Contains(statisticsText(statisticsValue(row, "companyName", "company_name")), name)) {
			return statisticsOdds(row)
		}
	}
	return nil
}
func statisticsOdds(value interface{}) []float64 {
	if row, ok := statisticsJSON(value).(map[string]interface{}); ok {
		return statisticsNumbers(statisticsValue(row, "odds"))
	}
	return nil
}
func statisticsAverageOdds(rows []map[string]interface{}) []float64 {
	sums, counts := [3]float64{}, [3]float64{}
	for _, row := range rows {
		if statisticsText(statisticsValue(row, "companyId", "company_id")) == "" {
			continue
		}
		odds := statisticsOdds(row)
		if len(odds) < 3 {
			continue
		}
		for i := 0; i < 3; i++ {
			if odds[i] > 0 {
				sums[i] += odds[i]
				counts[i]++
			}
		}
	}
	for _, count := range counts {
		if count == 0 {
			return nil
		}
	}
	return []float64{sums[0] / counts[0], sums[1] / counts[1], sums[2] / counts[2]}
}
func statisticsReturn(odds []float64) float64 {
	if len(odds) < 3 || odds[0] <= 0 || odds[1] <= 0 || odds[2] <= 0 {
		return 0
	}
	return 1 / (1/odds[0] + 1/odds[1] + 1/odds[2])
}

func statisticsValue(row map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return value
		}
		for actual, value := range row {
			if strings.EqualFold(actual, key) {
				return value
			}
		}
	}
	return nil
}
func statisticsJSON(value interface{}) interface{} {
	switch typed := value.(type) {
	case []byte:
		var out interface{}
		if json.Unmarshal(typed, &out) == nil {
			return out
		}
	case string:
		var out interface{}
		if json.Unmarshal([]byte(typed), &out) == nil {
			return out
		}
	default:
		return value
	}
	return nil
}
func statisticsText(value interface{}) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case time.Time:
		return typed.Format("2006-01-02")
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
func statisticsNumber(value interface{}) float64 {
	if value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	case float32:
		return float64(typed)
	case []byte:
		value = string(typed)
	}
	parsed, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
	return parsed
}
func statisticsNumbers(value interface{}) []float64 {
	value = statisticsJSON(value)
	switch typed := value.(type) {
	case []interface{}:
		result := make([]float64, 0, len(typed))
		for _, item := range typed {
			result = append(result, statisticsNumber(item))
		}
		return result
	case []string:
		result := make([]float64, 0, len(typed))
		for _, item := range typed {
			result = append(result, statisticsNumber(item))
		}
		return result
	case string:
		fields := strings.FieldsFunc(typed, func(r rune) bool { return r == ',' || r == ':' || r == '/' || r == '-' || r == ' ' })
		result := []float64{}
		for _, field := range fields {
			if field != "" {
				result = append(result, statisticsNumber(field))
			}
		}
		return result
	}
	return nil
}
func statisticsDate(value interface{}) string {
	text := statisticsText(value)
	if len(text) >= 10 {
		return text[:10]
	}
	return text
}
func statisticsSliceMean(values []float64) float64 {
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
func statisticsMean(first float64, firstOK bool, second float64, secondOK bool) float64 {
	value, _ := statisticsAverage(first, firstOK, second, secondOK)
	return value
}
func statisticsAverage(values ...interface{}) (float64, bool) {
	sum, count := 0.0, 0
	for index := 0; index+1 < len(values); index += 2 {
		value, ok := values[index].(float64)
		enabled, enabledOK := values[index+1].(bool)
		if ok && enabledOK && enabled {
			sum += value
			count++
		}
	}
	if count == 0 {
		return 0, false
	}
	return sum / float64(count), true
}
func statisticsClamp(value, min, max float64) float64 { return math.Max(min, math.Min(max, value)) }
