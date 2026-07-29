// Package handlers: statistics_workshop.go powers the 交叉信号分析 menu (信号命中率工坊).
//
// 它把每场完赛比赛拍平成一张「特征矩阵」（复用信号目录 recommendCatalogue + ctx
// buildRecommendCtx + 结算口径），并按时间切分 70/30 打上训练/验证标记，一次性
// 下发给前端。直选 / 双选排除 / 叠加角逐 三种打法、目标标的切换、样本门槛与排序
// 全部在前端实时计算——改门槛不必重算，Go 只做矩阵导出与缓存。
//
// 与旧交叉分析(recomputeCrossSnapshot / 待赛五市场推演)相互独立：本文件只导出
// 已完赛比赛的结果矩阵，不做待赛推演。
package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ---- 维度定义：18 维在目录层展开成 23 个可用维度，分档维度聚合互斥子档 ----
type workshopDim struct {
	Name, Label, Market string
	Sub                 []string // 目录条件 key；伞形维度含多个互斥子档
}

func workshopHeatKeys(prefix string, tiers []int, a, b string) []string {
	keys := make([]string, 0, len(tiers)*2)
	for _, t := range tiers {
		keys = append(keys, fmt.Sprintf("%s_%d_%s", prefix, t, a), fmt.Sprintf("%s_%d_%s", prefix, t, b))
	}
	return keys
}

var workshopDims = []workshopDim{
	{"pro_signal", "D1 凯体同向", "spf", []string{"pro_signal"}},
	{"trade_comfort", "D2 交易盈亏同向", "spf", []string{"trade_comfort"}},
	{"sim_trade_comfort", "D3 模拟盈亏同向", "spf", []string{"sim_trade_comfort"}},
	{"history_handicap", "D4 历史期望让球", "spf", []string{"history_handicap"}},
	{"recent_handicap", "D5 近期状态让球", "spf", []string{"recent_handicap"}},
	{"asian_composite", "D6 亚盘综合均值", "spf", []string{"asian_composite"}},
	{"base_prob", "D7 前端主推分档", "spf", []string{"base_prob_65", "base_prob_55_65", "base_prob_45_55", "base_prob_lt45"}},
	{"kelly_conflict", "D16警示 凯体反差", "spf", []string{"kelly_ticai_conflict"}},
	{"loss_both_away", "D16警示 同向亏损·客", "spf", []string{"loss_both_away"}},
	{"asian_heat", "D8 亚盘热度分档", "asian", workshopHeatKeys("asian_heat", []int{90, 85, 80, 75, 70, 65, 60}, "home", "guest")},
	{"line_discrepancy", "D9 亚盘即时盘背离", "asian", []string{"line_discrepancy"}},
	{"asian_hot_fade", "D16警示 让球过热反打", "asian", []string{"asian_hot_fade"}},
	{"handicap_fix", "D16警示 让球修正", "asian", []string{"handicap_fix"}},
	{"asian_exaggerate", "D17偏离 亚盘夸大", "asian", []string{"asian_exaggerate_050", "asian_exaggerate_025"}},
	{"asian_hidden", "D17偏离 亚盘隐藏", "asian", []string{"asian_hidden_050", "asian_hidden_025"}},
	{"history_goals", "D10 历史平均球数", "dxq", []string{"history_goals"}},
	{"recent_goals", "D11 近期平均球数", "dxq", []string{"recent_goals"}},
	{"goals_composite", "D12 球数综合均值", "dxq", []string{"goals_composite"}},
	{"goals_discrepancy", "D14 期望高于盘", "dxq", []string{"goals_discrepancy"}},
	{"goals_heat", "D13 大小球热度分档", "dxq", workshopHeatKeys("goals_heat", []int{90, 85, 80, 75, 70, 65, 60, 55}, "over", "under")},
	{"base_qiu", "D15 前端球数压力分档", "dxq", []string{"base_qiu_30", "base_qiu_15_30", "base_qiu_5_15"}},
	{"goal_balance", "D16警示 大小球回归", "dxq", []string{"goal_balance"}},
	{"goal_line_dev", "D17偏离 大小球盘口", "dxq", []string{"goal_line_above_050", "goal_line_above_025", "goal_line_below_050", "goal_line_below_025", "goal_line_near"}},
}

var workshopMarketLabel = map[string]string{"spf": "胜平负", "asian": "让球赢盘", "dxq": "大小球"}

// workshopRow 是下发给前端的每场紧凑记录。特征值缺失时为 null。
type workshopRow struct {
	ID     string             `json:"id"`  // 比赛标识（供下钻核对）
	Date   string             `json:"dt"`  // 比赛日期
	League string             `json:"lg"`  // 联赛
	Home   string             `json:"hm"`  // 主队
	Guest  string             `json:"gt"`  // 客队
	HS     int                `json:"hs"`  // 主队进球（待赛为0）
	GS     int                `json:"gs"`  // 客队进球（待赛为0）
	MT     string             `json:"mt"`  // 比赛时间（待赛排序/展示用）
	Up     bool               `json:"up"`  // true=待赛（未完赛，供预测未来；无实际结果）
	T      bool               `json:"t"`   // true=验证集(日期≥切分点)
	Out    string             `json:"out"` // home/draw/away
	Ah     string             `json:"ah"`  // 让球赢盘方 home/away（走盘/无盘为""）
	Ou     string             `json:"ou"`  // over/under（走盘/无盘为""）
	Oe     string             `json:"oe"`  // odd/even
	Feat   map[string]float64 `json:"f"`   // 数值特征（仅含存在的项）
	Dims   map[string]string  `json:"d"`   // 维度→预测方向（仅含触发的项）
	Hit    map[string]int     `json:"h"`   // 维度→自身命中(1/0)，仅含触发且可结算的项（叠加角逐用）
}

type workshopSnapshot struct {
	GeneratedAt   time.Time     `json:"generated_at"`
	SettledTotal  int           `json:"settled_total"`
	UpcomingTotal int           `json:"upcoming_total"`
	TrainTotal    int           `json:"train_total"`
	TestTotal     int           `json:"test_total"`
	SplitDate     string        `json:"split_date"`
	Rows          []workshopRow `json:"rows"`
}

var (
	workshopCacheMu sync.RWMutex
	workshopCache   *workshopSnapshot
)

// workshopHorizonDays 待赛预测的向前天数窗口。
const workshopHorizonDays = 14

func recomputeWorkshop() (*workshopSnapshot, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	horizon := time.Now().AddDate(0, 0, workshopHorizonDays).Format("2006-01-02")

	settled := make([]statisticsMatch, 0, len(rawMatches))
	upcoming := make([]statisticsMatch, 0, 64)
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		m := parseStatisticsMatch(row)
		if m.ID == "" {
			continue
		}
		if m.Settled {
			settled = append(settled, m)
			ids = append(ids, m.ID)
		} else if m.Date >= today && m.Date <= horizon {
			upcoming = append(upcoming, m)
			ids = append(ids, m.ID)
		}
	}
	sort.SliceStable(settled, func(i, j int) bool {
		if settled[i].Date != settled[j].Date {
			return settled[i].Date < settled[j].Date
		}
		return settled[i].ID < settled[j].ID
	})
	splitDate := ""
	if len(settled) > 0 {
		splitDate = settled[int(float64(len(settled))*0.7)].Date
	}

	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)

	catalogue := recommendCatalogue()
	snap := &workshopSnapshot{
		GeneratedAt: time.Now(), SettledTotal: len(settled),
		UpcomingTotal: len(upcoming), SplitDate: splitDate,
	}

	build := func(m statisticsMatch, up bool) workshopRow {
		ctx := buildRecommendCtx(m, histories[m.ID], pankous[m.ID], odds[m.ID], nil)

		condDir := make(map[string]string, len(catalogue))
		condFire := make(map[string]recommendFire, len(catalogue))
		for _, cond := range catalogue {
			if fire := cond.Evaluate(ctx); fire.fires {
				dir := fire.direction
				if dir == "" {
					dir = "fire"
				}
				condDir[cond.Key] = dir
				condFire[cond.Key] = fire
			}
		}
		dims := make(map[string]string, len(workshopDims))
		dimHit := make(map[string]int, len(workshopDims))
		for _, d := range workshopDims {
			for _, sub := range d.Sub {
				if dir, ok := condDir[sub]; ok {
					dims[d.Name] = dir
					if !up { // 待赛无结果，不算维度命中
						if hit, valid := recommendSettle(condFire[sub], ctx); valid {
							if hit {
								dimHit[d.Name] = 1
							} else {
								dimHit[d.Name] = 0
							}
						}
					}
					break
				}
			}
		}

		row := workshopRow{
			ID: m.ID, Date: m.Date, League: m.League, Home: m.Home, Guest: m.Guest,
			MT: m.MatchTime, Up: up, Feat: map[string]float64{}, Dims: dims, Hit: dimHit,
		}
		if !up {
			row.HS, row.GS = m.HomeScore, m.GuestScore
			row.T = m.Date >= splitDate
			row.Out = statisticsActualOutcome(m)
			row.Oe = map[bool]string{true: "odd", false: "even"}[(m.HomeScore+m.GuestScore)%2 == 1]
		}
		if ctx.hasAsian {
			row.Feat["ahLine"] = ctx.asianLine
			if !up {
				if homeCov, valid := statisticsAsianCorrect(m, ctx.asianLine); valid {
					row.Ah = map[bool]string{true: "home", false: "away"}[homeCov]
				}
			}
		}
		if ctx.hasDxq {
			row.Feat["ouLine"] = ctx.dxqLine
			if !up {
				if over, valid := statisticsOverOutcome(m, ctx.dxqLine); valid {
					row.Ou = map[bool]string{true: "over", false: "under"}[over]
				}
			}
		}
		if len(ctx.probabilities) == 3 {
			row.Feat["baseProb"] = ctx.basePredProb
		}
		if ctx.hasAsianHeat {
			row.Feat["asianHeat"] = ctx.asianHeat
		}
		if ctx.hasGoalsHeat {
			row.Feat["goalsHeat"] = ctx.goalsHeat
		}
		if ctx.hasRecentGls {
			row.Feat["recentGoals"] = ctx.recentGoals
		}
		if ctx.hasHistory {
			row.Feat["historyGoals"] = ctx.historyGoals
			row.Feat["historyDiff"] = ctx.historyDiff
		}
		if ctx.hasRecentDiff {
			row.Feat["recentDiff"] = ctx.recentDiff
		}
		return row
	}

	for _, m := range settled {
		row := build(m, false)
		if row.T {
			snap.TestTotal++
		} else {
			snap.TrainTotal++
		}
		snap.Rows = append(snap.Rows, row)
	}
	for _, m := range upcoming {
		snap.Rows = append(snap.Rows, build(m, true))
	}
	return snap, nil
}

func workshopMeta() []gin.H {
	dims := make([]gin.H, 0, len(workshopDims))
	for _, d := range workshopDims {
		dims = append(dims, gin.H{"name": d.Name, "label": d.Label, "market": d.Market, "marketLabel": workshopMarketLabel[d.Market]})
	}
	return dims
}

// GetSignalWorkshop serves 交叉信号分析(信号命中率工坊)。refresh=1 全量重算并写快照，
// 否则读快照；矩阵较大(约数百KB)，前端拿到后本地实时做直选/双选/叠加分析。
func GetSignalWorkshop(c *gin.Context) {
	if c.Query("refresh") == "1" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "重算正在进行中，请稍候再试"})
			return
		}
		snapshot, err := recomputeWorkshop()
		statisticsRecomputeMu.Unlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		workshopCacheMu.Lock()
		workshopCache = snapshot
		workshopCacheMu.Unlock()
		if payload, err := json.Marshal(snapshot); err == nil {
			_ = saveStatSnapshot(snapshotKindWorkshop, payload, snapshot.GeneratedAt)
		}
	}

	workshopCacheMu.RLock()
	snapshot := workshopCache
	workshopCacheMu.RUnlock()
	if snapshot == nil {
		if payload, _, ok := loadStatSnapshot(snapshotKindWorkshop); ok {
			restored := &workshopSnapshot{}
			if json.Unmarshal(payload, restored) == nil && restored.Rows != nil {
				workshopCacheMu.Lock()
				workshopCache = restored
				workshopCacheMu.Unlock()
				snapshot = restored
			}
		}
	}
	if snapshot == nil {
		c.JSON(http.StatusOK, gin.H{"needs_recompute": true, "dims": workshopMeta(), "rows": []workshopRow{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"needs_recompute": false,
		"generated_at":    snapshot.GeneratedAt.Format("2006-01-02 15:04:05"),
		"settled_total":   snapshot.SettledTotal,
		"upcoming_total":  snapshot.UpcomingTotal,
		"train_total":     snapshot.TrainTotal,
		"test_total":      snapshot.TestTotal,
		"split_date":      snapshot.SplitDate,
		"dims":            workshopMeta(),
		"rows":            snapshot.Rows,
	})
}
