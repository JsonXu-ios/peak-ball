// Package handlers: statistics_chase.go is dimension 17 — 先小后补大双段策略.
// The owner's playbook: buy UNDER at the main line on everything; when the
// under loses, chase OVER at a higher line (小2.25 lost → 补大3.5) so a blowout
// recovers the stake. The only way to lose both legs is the DEAD ZONE — the
// integer totals strictly between the two lines (almost always exactly 3 goals).
//
// Per match the two-stage settle (chase stake 1.11u at assumed 1.90 water, so a
// successful chase exactly recovers both stakes):
//
//	小球胜   total < L            → +(实际小球水-1)
//	走盘     total == L (整数盘)   → 0
//	补大救回 total > C=⌈L⌉+0.5    → 0（回本）
//	死亡区   L < total < C        → -2.11（两头全输）
//
// Buckets condition the same settle on the existing indicators to answer
// "哪些指标能撮合出适合这套打法的比赛".
package handlers

import (
	"fmt"
	"math"

	"github.com/gin-gonic/gin"
)

const (
	chaseWater      = 1.90 // 补大段的假设水位（库中无副盘口真实水位）
	chaseStake      = 1.11 // 1/(1.9-1)：补中正好收回两段本金
	chaseFlagSample = 5
)

type chaseTally struct {
	sig      statisticsSignal
	underWin int
	pushes   int
	chaseWin int
	dead     int
	stakes   float64
	returns  float64
}

func (t *chaseTally) add(detail statisticsDetail, category string, underWater float64, hasWater bool) {
	switch category {
	case "underWin":
		t.underWin++
		t.stakes++
		if hasWater {
			t.returns += underWater
		} else {
			t.returns += chaseWater // 无水位时按1.9近似
		}
		detail.Hit = true
		detail.Result = "小球胜"
	case "push":
		t.pushes++
		t.stakes++
		t.returns++
		detail.Hit = true
		detail.Result = "走盘"
	case "chaseWin":
		t.chaseWin++
		t.stakes += 1 + chaseStake
		t.returns += chaseStake * chaseWater
		detail.Hit = true
		detail.Result = "补大救回"
	case "dead":
		t.dead++
		t.stakes += 1 + chaseStake
		detail.Hit = false
		detail.Result = "死亡区⚰"
	}
	t.sig.add(detail)
}

func (t *chaseTally) bucketPayload(key, base string) gin.H {
	total := len(t.sig.details)
	split := ""
	if total > 0 {
		split = fmt.Sprintf(" · 小胜%.0f%% 补成%.0f%% 死%.0f%%",
			float64(t.underWin+t.pushes)/float64(total)*100,
			float64(t.chaseWin)/float64(total)*100,
			float64(t.dead)/float64(total)*100)
	}
	payload := t.sig.payload(key, base+split, "")
	if t.stakes > 0 {
		payload["roi"] = statisticsRound2(t.returns / t.stakes * 100)
		payload["roiSample"] = total
	}
	payload["flag"] = ""
	if total >= chaseFlagSample {
		roi := t.returns / t.stakes * 100
		if roi >= 102 {
			payload["flag"] = "red"
		} else if roi <= 90 {
			payload["flag"] = "black"
		}
	}
	return payload
}

func chaseLineBucket(line float64) string {
	switch {
	case line < 2.25:
		return "盘≤2.0"
	case line < 2.5:
		return "盘2.25"
	case line < 2.75:
		return "盘2.5"
	case line < 3:
		return "盘2.75"
	default:
		return "盘≥3.0"
	}
}

// buildChaseSignals settles the two-stage strategy over every completed match
// and conditions it on the indicator states.
func buildChaseSignals(matches []statisticsMatch, histories, pankous map[string]map[string]interface{}) gin.H {
	order := []string{
		"基准·全部有盘比赛",
		"深补对照·补⌈盘⌉+1.5(死亡区翻倍)",
		"盘≤2.0", "盘2.25", "盘2.5", "盘2.75", "盘≥3.0",
		"模型判小(综合≤盘-0.25)",
		"模型中性(|综合-盘|<0.25)",
		"模型判大(综合≥盘+0.25)",
		"回归小球信号时",
		"回归大球信号时",
		"盘高于共识≥0.25时",
		"盘低于共识≥0.25时",
		"大球热度>65时",
		"小球热度>65时",
	}
	buckets := map[string]*chaseTally{}
	add := func(key string, detail statisticsDetail, category string, underWater float64, hasWater bool) {
		tally := buckets[key]
		if tally == nil {
			tally = &chaseTally{}
			buckets[key] = tally
		}
		tally.add(detail, category, underWater, hasWater)
	}

	for _, match := range matches {
		historyRow := histories[match.ID]
		pankouRow := pankous[match.ID]
		_, line, hasLine := statisticsPankouLinePair(pankouRow, "bet365_dxq", "dxq_data")
		if !hasLine || line <= 0 {
			continue
		}
		_, underWater, hasWater := pickDxqWater(pankouRow)
		total := float64(match.HomeScore + match.GuestScore)
		chaseLine := math.Ceil(line) + 0.5

		category := ""
		switch {
		case math.Abs(total-line) < statisticsPushEpsilon:
			category = "push"
		case total < line:
			category = "underWin"
		case total > chaseLine:
			category = "chaseWin"
		default:
			category = "dead"
		}

		detail := statisticsBaseDetail(match)
		detail.Value = statisticsRound2(line)
		detail.Pick = fmt.Sprintf("小%.2f→补大%.1f（总%d球）", line, chaseLine, int(total))

		add("基准·全部有盘比赛", detail, category, underWater, hasWater)
		add(chaseLineBucket(line), detail, category, underWater, hasWater)

		// 深补对照：⌈盘⌉+1.5，死亡区扩成两个整数
		deepChaseLine := math.Ceil(line) + 1.5
		deepCategory := category
		if category == "chaseWin" && total < deepChaseLine {
			deepCategory = "dead"
		}
		deepDetail := detail
		deepDetail.Pick = fmt.Sprintf("小%.2f→深补大%.1f（总%d球）", line, deepChaseLine, int(total))
		add("深补对照·补⌈盘⌉+1.5(死亡区翻倍)", deepDetail, deepCategory, underWater, hasWater)

		// ---- 指标切分 ----
		against, homeRecent, guestRecent := statisticsHistory(historyRow)
		_, historyGoals, hasHistory := statisticsHeadToHead(match, against)
		recentGoals, hasRecent := statisticsRecentGoals(homeRecent, guestRecent)

		if composite, ok := statisticsAverage(historyGoals, hasHistory, recentGoals, hasRecent); ok {
			switch {
			case composite <= line-0.25:
				add("模型判小(综合≤盘-0.25)", detail, category, underWater, hasWater)
			case composite >= line+0.25:
				add("模型判大(综合≥盘+0.25)", detail, category, underWater, hasWater)
			default:
				add("模型中性(|综合-盘|<0.25)", detail, category, underWater, hasWater)
			}
		}

		if signal := warningGoalBalanceSignal(historyGoals, hasHistory, recentGoals, hasRecent, line, true); signal != "" {
			if signal == "under" {
				add("回归小球信号时", detail, category, underWater, hasWater)
			} else {
				add("回归大球信号时", detail, category, underWater, hasWater)
			}
		}

		if hasHistory && hasRecent && math.Abs(historyGoals-recentGoals) <= deviationGoalsAgree {
			consensus := (historyGoals + recentGoals) / 2
			if line-consensus >= 0.25 {
				add("盘高于共识≥0.25时", detail, category, underWater, hasWater)
			} else if consensus-line >= 0.25 {
				add("盘低于共识≥0.25时", detail, category, underWater, hasWater)
			}
		}

		if hasRecent || hasHistory {
			expected := statisticsMean(recentGoals, hasRecent, historyGoals, hasHistory)
			overHeat := statisticsClamp(50+(expected-line)*18, 0, 100)
			if overHeat > 65 {
				add("大球热度>65时", detail, category, underWater, hasWater)
			} else if (100 - overHeat) > 65 {
				add("小球热度>65时", detail, category, underWater, hasWater)
			}
		}
	}

	rows := []gin.H{}
	matched, hit := 0, 0
	for _, key := range order {
		tally := buckets[key]
		if tally == nil || len(tally.sig.details) == 0 {
			continue
		}
		matched += len(tally.sig.details)
		hit += tally.sig.hit
		rows = append(rows, tally.bucketPayload("chase-"+key, key))
	}
	base := buckets["基准·全部有盘比赛"]
	definition := fmt.Sprintf(
		"策略：全部先买小（真实水位），小输后补大⌈盘⌉+0.5（假设1.9水、注码1.11倍=补中回本）。命中=不亏（小胜/走盘/补大救回），未命中=死亡区（总进球恰好落在两盘之间，基本=正好3球）。深补对照展示补4.5类深盘会把死亡区扩大一倍。ROI=双段总回报/总投入。补大段为赛中价近似，实际临场水位可能更好。")
	if base != nil && len(base.sig.details) > 0 {
		total := len(base.sig.details)
		definition += fmt.Sprintf(" 基准%d场：小球胜%.1f%%，补大救回%.1f%%，死亡区%.1f%%。",
			total,
			float64(base.underWin+base.pushes)/float64(total)*100,
			float64(base.chaseWin)/float64(total)*100,
			float64(base.dead)/float64(total)*100)
	}
	accuracy := 0.0
	if matched > 0 {
		accuracy = math.Round(float64(hit)/float64(matched)*10000) / 100
	}
	return gin.H{
		"key":        "chase_signals",
		"title":      "17. 先小后补大（大小球双段策略）",
		"definition": definition,
		"matched":    matched, "hit": hit, "miss": matched - hit, "accuracy": accuracy,
		"buckets": rows,
	}
}
