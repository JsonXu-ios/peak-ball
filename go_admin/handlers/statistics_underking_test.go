package handlers

import (
	"encoding/json"
	"testing"

	"github.com/gin-gonic/gin"
)

// underKingDetail 造一条明细：推荐方向 pick、结算盘口 line、是否命中。
func underKingDetail(pick, line string, hit bool) statisticsDetail {
	return statisticsDetail{MatchID: pick + line, Pick: pick, Line: line, Hit: hit}
}

func TestUnderKingIsUnderPick(t *testing.T) {
	under := []string{"小球", "买小", "回归小球", "反大过热·买小", "判小球"}
	notUnder := []string{"大球", "买大", "回归大球", "反小过热·买大", "主胜", "平局", "主队赢盘", "3球", ""}
	for _, pick := range under {
		if !underKingIsUnderPick(pick) {
			t.Errorf("underKingIsUnderPick(%q) = false, want true", pick)
		}
	}
	for _, pick := range notUnder {
		if underKingIsUnderPick(pick) {
			t.Errorf("underKingIsUnderPick(%q) = true, want false", pick)
		}
	}
}

// TestUnderKingFromPayload 走一遍完整摊平：只留 underKingKeep 里的维度，分档信号
// 按整卡合计取、警示信号按单档取、横表按买法跨盘口合计。
func TestUnderKingFromPayload(t *testing.T) {
	source := gin.H{
		"settled_total": 100,
		"generated_at":  "2026-08-01T00:00:00Z",
		"signals": []gin.H{
			// 不在保留名单里 → 不收。
			{"key": "history_handicap", "title": "4. 历史期望让球", "matched": 2, "matches": []statisticsDetail{
				underKingDetail("主胜", "", true), underKingDetail("平局", "", false),
			}},
			{"key": "goals_heat", "title": "14. 大小球投注热度分档", "matched": 2, "buckets": []gin.H{
				{"key": "goals_heat-60-under", "title": "60%~65%·判小球", "matched": 1, "matches": []statisticsDetail{
					underKingDetail("小球", "2.5", true),
				}},
			}},
			// 混合方向 → 只留小球，命中率重算：3 场中 2 中。
			{"key": "history_goals", "title": "11. 历史平均球数", "matched": 5, "matches": []statisticsDetail{
				underKingDetail("大球", "2.5", true),
				underKingDetail("小球", "2.5", true),
				underKingDetail("小球", "2.5", false),
				underKingDetail("小球", "3", true),
				underKingDetail("大球", "3", false),
			}},
			// 分档信号取【各档合计】那一行。
			{"key": "base_qiu", "title": "28. 前端球数倾向·大小球（按压力强度分档）", "matched": 4, "buckets": []gin.H{
				{"key": "base-qiu-压力差≥30", "title": "压力差≥30", "matched": 2, "matches": []statisticsDetail{
					underKingDetail("小球", "2.5", true), underKingDetail("大球", "2.5", true),
				}},
				{"key": "base-qiu-压力差15-30", "title": "压力差15-30", "matched": 2, "matches": []statisticsDetail{
					underKingDetail("小球", "3", false), underKingDetail("小球", "3", true),
				}},
			}},
			// 警示信号取【单档】那两行；让球那档不在名单里。
			{"key": "warning_signals", "title": "29. 警示信号结算", "matched": 4, "buckets": []gin.H{
				{"key": "warn-让球热度过热·反过热方赢盘", "title": "让球热度过热·反过热方赢盘", "matched": 1, "matches": []statisticsDetail{
					underKingDetail("反主队(主过热)", "", true),
				}},
				{"key": "warn-大小球热度过热·反过热方向", "title": "大小球热度过热·反过热方向", "matched": 2, "matches": []statisticsDetail{
					underKingDetail("反大过热·买小", "2.5", true), underKingDetail("反小过热·买大", "2.5", true),
				}},
				{"key": "warn-大小球回归·跟回归方向", "title": "大小球回归·跟回归方向", "matched": 1, "matches": []statisticsDetail{
					underKingDetail("回归小球", "3", false),
				}},
			}},
			// 横表：买小的两格跨盘口合计；截尾那格不在名单里。
			{"key": "base70_goals", "title": "27. 前端主推≥70%·按大小球盘口分档", "line_rows": []gin.H{
				{"line": "2.5", "bets": []gin.H{
					{"key": "base70_goals-2.5-under", "title": "小 2.5", "matched": 2, "matches": []statisticsDetail{
						underKingDetail("小球", "2.5", true), underKingDetail("小球", "2.5", false),
					}},
					{"key": "base70_goals-2.5-over1", "title": "大 3.5", "matched": 1, "matches": []statisticsDetail{
						underKingDetail("大球", "3.5", true),
					}},
					{"key": "base70_goals-2.5-exp", "title": "期望球数对 2.5", "matched": 2, "matches": []statisticsDetail{
						underKingDetail("大球", "2.5", true), underKingDetail("小球", "2.5", true),
					}},
					{"key": "base70_goals-2.5-expunder", "title": "期望球数判小球对 2.5", "matched": 1, "matches": []statisticsDetail{
						underKingDetail("小球", "2.5", true),
					}},
					{"key": "base70_goals-2.5-exptruncunder", "title": "期望球数截尾判小球对 2.5", "matched": 1, "matches": []statisticsDetail{
						underKingDetail("小球", "2.5", false),
					}},
				}},
				{"line": "3", "bets": []gin.H{
					{"key": "base70_goals-3-under", "title": "小 3", "matched": 2, "matches": []statisticsDetail{
						underKingDetail("小球", "3", true), underKingDetail("小球", "3", true),
					}},
				}},
			}},
		},
	}

	payload, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}
	report, err := underKingFromPayload(payload)
	if err != nil {
		t.Fatalf("underKingFromPayload: %v", err)
	}
	rows, ok := report["rows"].([]underKingRow)
	if !ok {
		t.Fatalf("rows type = %T", report["rows"])
	}

	byKey := map[string]underKingRow{}
	for _, row := range rows {
		byKey[row.Key] = row
	}
	unwanted := []string{
		"history_handicap", "goals_heat", "goals_heat-60-under",
		"warn-让球热度过热·反过热方赢盘", "base70_goals-exptruncunder",
		"base-qiu-压力差≥30", "warning_signals",
	}
	for _, key := range unwanted {
		if _, present := byKey[key]; present {
			t.Errorf("%q 不在保留名单里，不应出现", key)
		}
	}
	// 11、28、29×2、27×2 = 6 行。
	if len(rows) != 6 {
		t.Fatalf("rows = %d, want 6 (%v)", len(rows), byKey)
	}

	// 按命中率降序排列。
	for index := 1; index < len(rows); index++ {
		if rows[index-1].Accuracy < rows[index].Accuracy {
			t.Errorf("rows 未按命中率降序：[%d]=%.2f < [%d]=%.2f", index-1, rows[index-1].Accuracy, index, rows[index].Accuracy)
		}
	}

	// 11：5 场里 3 场判小球，命中 2 场。
	goals := byKey["history_goals"]
	if goals.Matched != 3 || goals.Hit != 2 || goals.Accuracy != 66.67 {
		t.Errorf("history_goals = %d/%d %.2f%%, want 2/3 66.67%%", goals.Hit, goals.Matched, goals.Accuracy)
	}
	for _, detail := range goals.Matches {
		if detail.Pick != "小球" {
			t.Errorf("明细里出现了非小球方向：%q", detail.Pick)
		}
	}

	// 28：两档合计里判小球的 3 场，命中 2 场；标题被改写。
	qiu := byKey["base_qiu"]
	if qiu.Matched != 3 || qiu.Hit != 2 {
		t.Errorf("base_qiu = %d/%d, want 2/3", qiu.Hit, qiu.Matched)
	}
	if want := "28. 前端球数倾向判小球（全部压力档合计）"; qiu.Title != want {
		t.Errorf("base_qiu title = %q, want %q", qiu.Title, want)
	}

	// 29：两个大小球分档各成一行，各自只留暗示小球的那条明细。
	fade := byKey["warn-大小球热度过热·反过热方向"]
	if fade.Matched != 1 || fade.Hit != 1 {
		t.Errorf("反过热 = %d/%d, want 1/1", fade.Hit, fade.Matched)
	}
	if want := "29. 警示信号·大小球热度过热·反过热方向"; fade.Title != want {
		t.Errorf("反过热 title = %q, want %q", fade.Title, want)
	}
	if back := byKey["warn-大小球回归·跟回归方向"]; back.Matched != 1 || back.Hit != 0 {
		t.Errorf("回归 = %d/%d, want 0/1", back.Hit, back.Matched)
	}

	// 27：买小格跨盘口合计 = 2.5 档 1/2 + 3 档 2/2 = 3/4。
	buyUnder := byKey["base70_goals-under"]
	if buyUnder.Matched != 4 || buyUnder.Hit != 3 || buyUnder.Accuracy != 75 {
		t.Errorf("按盘口买小 = %d/%d %.2f%%, want 3/4 75%%", buyUnder.Hit, buyUnder.Matched, buyUnder.Accuracy)
	}
	if expUnder := byKey["base70_goals-expunder"]; expUnder.Matched != 1 || expUnder.Hit != 1 {
		t.Errorf("期望球数判小球 = %d/%d, want 1/1", expUnder.Hit, expUnder.Matched)
	}
}
