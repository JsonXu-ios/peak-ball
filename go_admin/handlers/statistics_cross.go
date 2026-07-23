// Package handlers: statistics_cross.go powers the 交叉信号分析 menu.
// It reuses the recommendation catalogue (recommendations.go) and settles three
// kinds of cross features against all completed matches:
//  1. 胜平负信号 × 比分   —— 信号触发时最终比分（按竞彩比分选项分桶）的分布；
//  2. 大小球信号 × 总进球 —— 信号触发时总进球数（0~7+）的分布；
//  3. 信号两两组合       —— 同场同时触发时的条件命中率（A|B 与 A 单独对比），
//     发现"单独未上岗、组合后 ≥70%/≤30%"的新维度。
//
// 之后基于上岗信号对待赛比赛做五市场推演：胜平负 / 让球(亚盘) / 大小球方向 +
// 比分 Top / 总进球 Top（用交叉分布，缺样本时退回全库基线）。
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

	"github.com/gin-gonic/gin"
)

const (
	crossPairUpliftPP = 10.0 // 组合较单独命中率的最小提升(百分点)，达到才值得展示
	crossMaxPairRows  = 200
	crossTopScores    = 5
	crossTopGoals     = 3
	crossMaxCombos    = 6
)

// ---------- buckets（与竞彩可购选项对齐） ----------

// crossScoreBucket maps a final score onto 竞彩比分 options
// (胜 1:0..5:2 / 平 0:0..3:3 / 负 0:1..2:5，其余归入 胜其他/平其他/负其他).
func crossScoreBucket(home, guest int) string {
	switch {
	case home > guest:
		if home <= 5 && guest <= 2 {
			return fmt.Sprintf("%d:%d", home, guest)
		}
		return "胜其他"
	case home == guest:
		if home <= 3 {
			return fmt.Sprintf("%d:%d", home, guest)
		}
		return "平其他"
	default:
		if guest <= 5 && home <= 2 {
			return fmt.Sprintf("%d:%d", home, guest)
		}
		return "负其他"
	}
}

// crossGoalsBucket maps total goals onto 竞彩总进球 options (0..6, 7+).
func crossGoalsBucket(total int) string {
	if total >= 7 {
		return "7+"
	}
	return strconv.Itoa(total)
}

// ---------- snapshot ----------

type crossDist struct {
	Sample  int
	Buckets map[string]int
}

func crossDistAdd(dists map[string]*crossDist, key, bucket string) {
	dist := dists[key]
	if dist == nil {
		dist = &crossDist{Buckets: map[string]int{}}
		dists[key] = dist
	}
	dist.Sample++
	dist.Buckets[bucket]++
}

// crossPairStat: A/B 按目录顺序排列（key 为 keyA|keyB）。
type crossPairStat struct {
	Sample, HitA, HitB, HitBoth int
}

type crossSnapshot struct {
	GeneratedAt    time.Time
	SettledTotal   int
	Stats          map[string]*recommendStat
	ActiveModes    map[string]string
	SpfScore       map[string]*crossDist // condKey|direction → 比分桶分布
	DxqGoals       map[string]*crossDist // condKey|direction → 总进球分布
	ScoreAll       map[string]int
	ScoreByOutcome map[string]map[string]int
	GoalsAll       map[string]int
	Pairs          map[string]*crossPairStat
}

var (
	crossCacheMu sync.RWMutex
	crossCache   *crossSnapshot
)

func recomputeCrossSnapshot() (*crossSnapshot, error) {
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		return nil, err
	}
	settled := make([]statisticsMatch, 0, len(rawMatches))
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		match := parseStatisticsMatch(row)
		if match.ID == "" || !match.Settled {
			continue
		}
		settled = append(settled, match)
		ids = append(ids, match.ID)
	}
	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)

	catalogue := recommendCatalogue()
	snap := &crossSnapshot{
		GeneratedAt:    time.Now(),
		SettledTotal:   len(settled),
		Stats:          map[string]*recommendStat{},
		ActiveModes:    map[string]string{},
		SpfScore:       map[string]*crossDist{},
		DxqGoals:       map[string]*crossDist{},
		ScoreAll:       map[string]int{},
		ScoreByOutcome: map[string]map[string]int{},
		GoalsAll:       map[string]int{},
		Pairs:          map[string]*crossPairStat{},
	}

	type crossFired struct {
		key string
		hit bool
	}
	for _, match := range settled {
		ctx := buildRecommendCtx(match, histories[match.ID], pankous[match.ID], odds[match.ID], nil)
		scoreBucket := crossScoreBucket(match.HomeScore, match.GuestScore)
		goalsBucket := crossGoalsBucket(match.HomeScore + match.GuestScore)
		outcome := statisticsActualOutcome(match)
		snap.ScoreAll[scoreBucket]++
		snap.GoalsAll[goalsBucket]++
		if snap.ScoreByOutcome[outcome] == nil {
			snap.ScoreByOutcome[outcome] = map[string]int{}
		}
		snap.ScoreByOutcome[outcome][scoreBucket]++

		fired := make([]crossFired, 0, 16)
		for _, condition := range catalogue {
			fire := condition.Evaluate(ctx)
			if !fire.fires {
				continue
			}
			hit, valid := recommendSettle(fire, ctx)
			if !valid {
				continue
			}
			stat := snap.Stats[condition.Key]
			if stat == nil {
				stat = &recommendStat{}
				snap.Stats[condition.Key] = stat
			}
			stat.Sample++
			if hit {
				stat.Hit++
			}
			// 目录遍历顺序固定，fired 天然按目录顺序 → pair key 全库一致。
			fired = append(fired, crossFired{condition.Key, hit})
			if condition.Market == "spf" && fire.settle == "outcome" {
				crossDistAdd(snap.SpfScore, condition.Key+"|"+fire.direction, scoreBucket)
			}
			if condition.Market == "dxq" && fire.settle == "over" {
				crossDistAdd(snap.DxqGoals, condition.Key+"|"+fire.direction, goalsBucket)
			}
		}
		for i := 0; i < len(fired); i++ {
			for j := i + 1; j < len(fired); j++ {
				pairKey := fired[i].key + "|" + fired[j].key
				stat := snap.Pairs[pairKey]
				if stat == nil {
					stat = &crossPairStat{}
					snap.Pairs[pairKey] = stat
				}
				stat.Sample++
				if fired[i].hit {
					stat.HitA++
				}
				if fired[j].hit {
					stat.HitB++
				}
				if fired[i].hit && fired[j].hit {
					stat.HitBoth++
				}
			}
		}
	}

	for key, stat := range snap.Stats {
		if stat.Sample < recommendMinSample {
			continue
		}
		accuracy := float64(stat.Hit) / float64(stat.Sample) * 100
		if accuracy >= recommendHighCutoff {
			snap.ActiveModes[key] = "follow"
		} else if accuracy <= recommendLowCutoff {
			snap.ActiveModes[key] = "inverse"
		}
	}
	return snap, nil
}

// ---------- response helpers ----------

func crossAccuracy(stat *recommendStat) (float64, int) {
	if stat == nil || stat.Sample == 0 {
		return 0, 0
	}
	return math.Round(float64(stat.Hit)/float64(stat.Sample)*10000) / 100, stat.Sample
}

// crossTopBuckets ranks a distribution and attaches the全库 baseline share.
func crossTopBuckets(dist *crossDist, baseline map[string]int, baselineTotal, topN int) []gin.H {
	type entry struct {
		bucket string
		count  int
	}
	entries := make([]entry, 0, len(dist.Buckets))
	for bucket, count := range dist.Buckets {
		entries = append(entries, entry{bucket, count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].bucket < entries[j].bucket
	})
	if len(entries) > topN {
		entries = entries[:topN]
	}
	rows := make([]gin.H, 0, len(entries))
	for _, item := range entries {
		row := gin.H{
			"bucket": item.bucket,
			"count":  item.count,
			"pct":    statisticsRound2(float64(item.count) / float64(dist.Sample) * 100),
		}
		if baselineTotal > 0 {
			row["basePct"] = statisticsRound2(float64(baseline[item.bucket]) / float64(baselineTotal) * 100)
		}
		rows = append(rows, row)
	}
	return rows
}

func crossOutcomeLabel(direction string) string {
	switch direction {
	case "home":
		return "主胜"
	case "away":
		return "客胜"
	}
	return "平局"
}

func crossSum(counts map[string]int) int {
	total := 0
	for _, count := range counts {
		total += count
	}
	return total
}

// crossMergedTop ranks an already merged bucket map (信号分布推演用).
func crossMergedTop(buckets map[string]int, topN int) []gin.H {
	dist := &crossDist{Sample: crossSum(buckets), Buckets: buckets}
	if dist.Sample == 0 {
		return nil
	}
	return crossTopBuckets(dist, nil, 0, topN)
}

// ---------- handler ----------

// GetCrossStatistics serves 交叉信号分析: heavy stats cached, refresh=1 recomputes.
func GetCrossStatistics(c *gin.Context) {
	days := recommendDefaultDays
	if raw := strings.TrimSpace(c.Query("days")); raw != "" {
		fmt.Sscanf(raw, "%d", &days)
		if days < 1 {
			days = 1
		}
		if days > 14 {
			days = 14
		}
	}

	if c.Query("refresh") == "1" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "重算正在进行中，请稍候再试"})
			return
		}
		snapshot, err := recomputeCrossSnapshot()
		statisticsRecomputeMu.Unlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		crossCacheMu.Lock()
		crossCache = snapshot
		crossCacheMu.Unlock()
		if payload, err := json.Marshal(snapshot); err == nil {
			_ = saveStatSnapshot(snapshotKindCrossStatistics, payload, snapshot.GeneratedAt)
		}
	}

	crossCacheMu.RLock()
	snapshot := crossCache
	crossCacheMu.RUnlock()
	if snapshot == nil {
		if payload, _, ok := loadStatSnapshot(snapshotKindCrossStatistics); ok {
			restored := &crossSnapshot{}
			if json.Unmarshal(payload, restored) == nil && restored.Stats != nil {
				crossCacheMu.Lock()
				crossCache = restored
				crossCacheMu.Unlock()
				snapshot = restored
			}
		}
	}
	if snapshot == nil {
		c.JSON(http.StatusOK, gin.H{
			"needs_recompute": true,
			"spf_score":       []gin.H{},
			"dxq_goals":       []gin.H{},
			"pairs":           []gin.H{},
			"derived":         []gin.H{},
		})
		return
	}

	catalogue := recommendCatalogue()
	conditionByKey := map[string]recommendCondition{}
	for _, condition := range catalogue {
		conditionByKey[condition.Key] = condition
	}
	scoreAllTotal := crossSum(snapshot.ScoreAll)
	goalsAllTotal := crossSum(snapshot.GoalsAll)

	// ---- 1. 胜平负信号 × 比分 ----
	spfScoreRows := []gin.H{}
	for _, condition := range catalogue {
		if condition.Market != "spf" {
			continue
		}
		for _, direction := range []string{"home", "draw", "away"} {
			dist := snapshot.SpfScore[condition.Key+"|"+direction]
			if dist == nil || dist.Sample < recommendMinSample {
				continue
			}
			spfScoreRows = append(spfScoreRows, gin.H{
				"key": condition.Key, "title": condition.Title,
				"direction": direction, "directionLabel": crossOutcomeLabel(direction),
				"sample": dist.Sample, "mode": snapshot.ActiveModes[condition.Key],
				"top": crossTopBuckets(dist, snapshot.ScoreAll, scoreAllTotal, crossTopScores),
			})
		}
	}
	sort.SliceStable(spfScoreRows, func(i, j int) bool {
		activeI := spfScoreRows[i]["mode"].(string) != ""
		activeJ := spfScoreRows[j]["mode"].(string) != ""
		if activeI != activeJ {
			return activeI
		}
		return spfScoreRows[i]["sample"].(int) > spfScoreRows[j]["sample"].(int)
	})

	// ---- 2. 大小球信号 × 总进球 ----
	dxqGoalsRows := []gin.H{}
	for _, condition := range catalogue {
		if condition.Market != "dxq" {
			continue
		}
		for _, direction := range []string{"over", "under"} {
			dist := snapshot.DxqGoals[condition.Key+"|"+direction]
			if dist == nil || dist.Sample < recommendMinSample {
				continue
			}
			label := "判小"
			if direction == "over" {
				label = "判大"
			}
			dxqGoalsRows = append(dxqGoalsRows, gin.H{
				"key": condition.Key, "title": condition.Title,
				"direction": direction, "directionLabel": label,
				"sample": dist.Sample, "mode": snapshot.ActiveModes[condition.Key],
				"top": crossTopBuckets(dist, snapshot.GoalsAll, goalsAllTotal, crossTopGoals),
			})
		}
	}
	sort.SliceStable(dxqGoalsRows, func(i, j int) bool {
		activeI := dxqGoalsRows[i]["mode"].(string) != ""
		activeJ := dxqGoalsRows[j]["mode"].(string) != ""
		if activeI != activeJ {
			return activeI
		}
		return dxqGoalsRows[i]["sample"].(int) > dxqGoalsRows[j]["sample"].(int)
	})

	// ---- 3. 信号两两组合：条件命中率 vs 单独命中率 ----
	type pairRow struct {
		payload   gin.H
		fresh     bool // 单独未上岗、组合后跨过 70/30 阈值 → 新维度
		deviation float64
	}
	pairRows := []pairRow{}
	for pairKey, stat := range snapshot.Pairs {
		if stat.Sample < recommendMinSample {
			continue
		}
		keys := strings.SplitN(pairKey, "|", 2)
		condA, okA := conditionByKey[keys[0]]
		condB, okB := conditionByKey[keys[1]]
		if !okA || !okB {
			continue
		}
		accA := float64(stat.HitA) / float64(stat.Sample) * 100
		accB := float64(stat.HitB) / float64(stat.Sample) * 100
		accBoth := float64(stat.HitBoth) / float64(stat.Sample) * 100
		globalA, sampleA := crossAccuracy(snapshot.Stats[keys[0]])
		globalB, sampleB := crossAccuracy(snapshot.Stats[keys[1]])
		crossedA := accA >= recommendHighCutoff || accA <= recommendLowCutoff
		crossedB := accB >= recommendHighCutoff || accB <= recommendLowCutoff
		freshA := crossedA && snapshot.ActiveModes[keys[0]] == ""
		freshB := crossedB && snapshot.ActiveModes[keys[1]] == ""
		upliftA := accA - globalA
		upliftB := accB - globalB
		if !freshA && !freshB && math.Abs(upliftA) < crossPairUpliftPP && math.Abs(upliftB) < crossPairUpliftPP {
			continue
		}
		deviation := math.Max(math.Abs(accA-50), math.Abs(accB-50))
		pairRows = append(pairRows, pairRow{
			payload: gin.H{
				"keyA": keys[0], "titleA": condA.Title, "marketA": condA.Market,
				"keyB": keys[1], "titleB": condB.Title, "marketB": condB.Market,
				"sample":  stat.Sample,
				"accA":    statisticsRound2(accA),
				"accB":    statisticsRound2(accB),
				"accBoth": statisticsRound2(accBoth),
				"globalA": globalA, "sampleA": sampleA,
				"globalB": globalB, "sampleB": sampleB,
				"upliftA": statisticsRound2(upliftA),
				"upliftB": statisticsRound2(upliftB),
				"freshA":  freshA, "freshB": freshB,
			},
			fresh:     freshA || freshB,
			deviation: deviation,
		})
	}
	sort.SliceStable(pairRows, func(i, j int) bool {
		if pairRows[i].fresh != pairRows[j].fresh {
			return pairRows[i].fresh
		}
		return pairRows[i].deviation > pairRows[j].deviation
	})
	if len(pairRows) > crossMaxPairRows {
		pairRows = pairRows[:crossMaxPairRows]
	}
	pairPayloads := make([]gin.H, 0, len(pairRows))
	freshPairs := 0
	for _, row := range pairRows {
		if row.fresh {
			freshPairs++
		}
		pairPayloads = append(pairPayloads, row.payload)
	}

	// ---- 4. 待赛五市场推演 ----
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	today := time.Now().Format("2006-01-02")
	horizon := time.Now().AddDate(0, 0, days).Format("2006-01-02")
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

	type derivedRow struct {
		sortKey string
		payload gin.H
	}
	derivedRows := []derivedRow{}
	for _, match := range upcoming {
		ctx := buildRecommendCtx(match, histories[match.ID], pankous[match.ID], odds[match.ID], nil)
		type firedEntry struct {
			condition recommendCondition
			fire      recommendFire
			mode      string // 上岗模式，未上岗为空
		}
		fired := make([]firedEntry, 0, 16)
		for _, condition := range catalogue {
			fire := condition.Evaluate(ctx)
			if !fire.fires {
				continue
			}
			fired = append(fired, firedEntry{condition, fire, snapshot.ActiveModes[condition.Key]})
		}

		// 上岗信号按市场投票 + 合并交叉分布。
		spfVotes := map[string]float64{}
		asianVotes := map[string]float64{}
		dxqVotes := map[string]float64{}
		spfSignals, asianSignals, dxqSignals := 0, 0, 0
		scoreMerged := map[string]int{}
		goalsMerged := map[string]int{}
		for _, entry := range fired {
			if entry.mode == "" {
				continue
			}
			inverse := entry.mode == "inverse"
			switch {
			case entry.condition.Market == "spf" && entry.fire.settle == "outcome":
				spfSignals++
				if inverse {
					for _, direction := range []string{"home", "draw", "away"} {
						if direction != entry.fire.direction {
							spfVotes[direction] += 0.5
						}
					}
				} else {
					spfVotes[entry.fire.direction]++
				}
				if dist := snapshot.SpfScore[entry.condition.Key+"|"+entry.fire.direction]; dist != nil && dist.Sample >= recommendMinSample {
					for bucket, count := range dist.Buckets {
						scoreMerged[bucket] += count
					}
				}
			case entry.condition.Market == "spf" && entry.fire.settle == "choices":
				spfSignals++
				choices := ctx.kellyChoices
				picked := 0
				for _, direction := range []string{"home", "draw", "away"} {
					if choices[direction] != inverse {
						picked++
					}
				}
				if picked > 0 {
					weight := 1 / float64(picked)
					for _, direction := range []string{"home", "draw", "away"} {
						if choices[direction] != inverse {
							spfVotes[direction] += weight
						}
					}
				}
			case entry.condition.Market == "asian" && entry.fire.settle == "cover":
				asianSignals++
				direction := entry.fire.direction
				if inverse {
					if direction == "home" {
						direction = "away"
					} else {
						direction = "home"
					}
				}
				asianVotes[direction]++
			case entry.condition.Market == "dxq" && entry.fire.settle == "over":
				dxqSignals++
				direction := entry.fire.direction
				if inverse {
					if direction == "over" {
						direction = "under"
					} else {
						direction = "over"
					}
				}
				dxqVotes[direction]++
				if dist := snapshot.DxqGoals[entry.condition.Key+"|"+entry.fire.direction]; dist != nil && dist.Sample >= recommendMinSample {
					for bucket, count := range dist.Buckets {
						goalsMerged[bucket] += count
					}
				}
			}
		}

		argmax := func(votes map[string]float64) string {
			best, bestValue, unique := "", 0.0, true
			for direction, value := range votes {
				if value > bestValue {
					best, bestValue, unique = direction, value, true
				} else if value == bestValue && value > 0 {
					unique = false
				}
			}
			if !unique {
				return ""
			}
			return best
		}

		payload := gin.H{
			"matchId": match.ID, "date": match.Date, "state": match.State,
			"matchTime": match.MatchTime, "league": match.League,
			"home": match.Home, "guest": match.Guest,
			"homeLogo": match.HomeLogo, "guestLogo": match.GuestLogo,
		}
		derivedAny := false

		if direction := argmax(spfVotes); direction != "" {
			payload["spf"] = gin.H{
				"direction": direction,
				"label":     recommendOutcomeLabelFor(direction, match),
				"signals":   spfSignals,
				"votes":     statisticsRound2(spfVotes[direction]),
			}
			derivedAny = true
			// 比分推演：优先信号交叉分布，样本不足退回全库该方向基线。
			scoreSource, scoreBuckets := "信号交叉分布", scoreMerged
			if crossSum(scoreMerged) < recommendMinSample {
				scoreSource, scoreBuckets = "全库基线", snapshot.ScoreByOutcome[direction]
			}
			if top := crossMergedTop(scoreBuckets, crossTopScores); top != nil {
				payload["score"] = gin.H{"source": scoreSource, "top": top}
			}
		}
		if direction := argmax(asianVotes); direction != "" && ctx.hasAsian {
			side := match.Guest
			if direction == "home" {
				side = match.Home
			}
			payload["asian"] = gin.H{
				"direction": direction,
				"label":     fmt.Sprintf("买%s赢盘(%.2f)", side, ctx.asianLine),
				"signals":   asianSignals,
				"votes":     statisticsRound2(asianVotes[direction]),
			}
			derivedAny = true
		}
		if direction := argmax(dxqVotes); direction != "" && ctx.hasDxq {
			label := fmt.Sprintf("买小%.2f", ctx.dxqLine)
			if direction == "over" {
				label = fmt.Sprintf("买大%.2f", ctx.dxqLine)
			}
			payload["dxq"] = gin.H{
				"direction": direction,
				"label":     label,
				"signals":   dxqSignals,
				"votes":     statisticsRound2(dxqVotes[direction]),
			}
			derivedAny = true
			goalsSource, goalsBuckets := "信号交叉分布", goalsMerged
			if crossSum(goalsMerged) < recommendMinSample {
				goalsSource, goalsBuckets = "全库基线", snapshot.GoalsAll
			}
			if top := crossMergedTop(goalsBuckets, crossTopGoals); top != nil {
				payload["goals"] = gin.H{"source": goalsSource, "top": top}
			}
		}

		// 组合信号命中：全目录 fired 两两查历史 pair 表，条件命中率跨过阈值的给出建议。
		combos := []gin.H{}
		for i := 0; i < len(fired) && len(combos) < crossMaxCombos; i++ {
			for j := i + 1; j < len(fired) && len(combos) < crossMaxCombos; j++ {
				stat := snapshot.Pairs[fired[i].condition.Key+"|"+fired[j].condition.Key]
				if stat == nil || stat.Sample < recommendMinSample {
					continue
				}
				appendCombo := func(target, other firedEntry, accuracy float64) {
					if len(combos) >= crossMaxCombos {
						return
					}
					pick := target.fire.pick
					comboMode := "follow"
					if accuracy <= recommendLowCutoff {
						comboMode = "inverse"
						pick = recommendInversePick(target.condition, target.fire, ctx)
					}
					combos = append(combos, gin.H{
						"title":    target.condition.Title,
						"withCond": other.condition.Title,
						"pick":     pick,
						"mode":     comboMode,
						"accuracy": statisticsRound2(accuracy),
						"sample":   stat.Sample,
					})
				}
				accA := float64(stat.HitA) / float64(stat.Sample) * 100
				accB := float64(stat.HitB) / float64(stat.Sample) * 100
				if accA >= recommendHighCutoff || accA <= recommendLowCutoff {
					appendCombo(fired[i], fired[j], accA)
				}
				if accB >= recommendHighCutoff || accB <= recommendLowCutoff {
					appendCombo(fired[j], fired[i], accB)
				}
			}
		}
		if len(combos) > 0 {
			payload["combos"] = combos
			derivedAny = true
		}

		if !derivedAny {
			continue
		}
		derivedRows = append(derivedRows, derivedRow{sortKey: match.MatchTime + match.ID, payload: payload})
	}
	sort.Slice(derivedRows, func(i, j int) bool { return derivedRows[i].sortKey < derivedRows[j].sortKey })
	derived := make([]gin.H, 0, len(derivedRows))
	for _, row := range derivedRows {
		derived = append(derived, row.payload)
	}

	c.JSON(http.StatusOK, gin.H{
		"needs_recompute":    false,
		"stats_generated_at": snapshot.GeneratedAt.Format("2006-01-02 15:04:05"),
		"settled_total":      snapshot.SettledTotal,
		"upcoming_total":     len(upcoming),
		"days":               days,
		"min_sample":         recommendMinSample,
		"high_cutoff":        recommendHighCutoff,
		"low_cutoff":         recommendLowCutoff,
		"pair_uplift_pp":     crossPairUpliftPP,
		"fresh_pairs":        freshPairs,
		"spf_score":          spfScoreRows,
		"dxq_goals":          dxqGoalsRows,
		"pairs":              pairPayloads,
		"derived":            derived,
	})
}
