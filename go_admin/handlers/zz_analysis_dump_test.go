package handlers

// zz_analysis_dump_test.go — 信号命中率总台的冒烟自检（非回归测试）。
// 只在设置 DUMP_ANALYSIS=1 时运行，不影响常规 `go test ./handlers/`：
//   DUMP_ANALYSIS=1 go test ./handlers/ -run TestWorkshopRecompute -count=1 -v
// 可选 DUMP_PATH=snapshot.json 把整份矩阵快照写盘，供离线校验。

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"go_admin/database"
)

// TestDumpConditionCSV 导出每场完赛 × 全部目录条件的(方向,命中)明细，供离线
// 诚实性回测（walk-forward）。DUMP_ANALYSIS=1 且 DUMP_CSV 有路径时运行。
func TestDumpConditionCSV(t *testing.T) {
	if os.Getenv("DUMP_ANALYSIS") != "1" || os.Getenv("DUMP_CSV") == "" {
		t.Skip("set DUMP_ANALYSIS=1 and DUMP_CSV to run")
	}
	if err := database.Init(); err != nil {
		t.Fatalf("db init: %v", err)
	}
	var rawMatches []map[string]interface{}
	if err := statisticsDB().Table("moneys").Select(statisticsMoneysColumns).Find(&rawMatches).Error; err != nil {
		t.Fatalf("load moneys: %v", err)
	}
	settled := make([]statisticsMatch, 0, len(rawMatches))
	ids := make([]string, 0, len(rawMatches))
	for _, row := range rawMatches {
		m := parseStatisticsMatch(row)
		if m.ID == "" || !m.Settled {
			continue
		}
		settled = append(settled, m)
		ids = append(ids, m.ID)
	}
	histories := loadStatisticsRows("history_moneys", statisticsHistoryColumns, ids)
	pankous := loadStatisticsRows("pankou_moneys", statisticsPankouColumns, ids)
	odds := loadStatisticsRows("odds_moneys", statisticsOddsColumns, ids)
	catalogue := recommendCatalogue()

	f, err := os.Create(os.Getenv("DUMP_CSV"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	header := []string{"match_id", "date", "out", "ah_line", "ah_cover", "ou_line", "ou", "odd_even",
		"base_prob", "asian_heat", "goals_heat", "recent_goals", "history_goals", "recent_diff"}
	for _, c := range catalogue {
		header = append(header, c.Key+"_dir", c.Key+"_hit")
	}
	_ = w.Write(header)
	fnum := func(ok bool, v float64) string {
		if !ok {
			return "NA"
		}
		return strconv.FormatFloat(v, 'f', 4, 64)
	}
	for _, m := range settled {
		ctx := buildRecommendCtx(m, histories[m.ID], pankous[m.ID], odds[m.ID], nil)
		ah, ahLine := "NA", "NA"
		if ctx.hasAsian {
			ahLine = fnum(true, ctx.asianLine)
			if hc, valid := statisticsAsianCorrect(m, ctx.asianLine); valid {
				ah = map[bool]string{true: "home", false: "away"}[hc]
			} else {
				ah = "push"
			}
		}
		ou, ouLine := "NA", "NA"
		if ctx.hasDxq {
			ouLine = fnum(true, ctx.dxqLine)
			if ov, valid := statisticsOverOutcome(m, ctx.dxqLine); valid {
				ou = map[bool]string{true: "over", false: "under"}[ov]
			} else {
				ou = "push"
			}
		}
		rec := []string{m.ID, m.Date, statisticsActualOutcome(m), ahLine, ah, ouLine, ou,
			map[bool]string{true: "odd", false: "even"}[(m.HomeScore+m.GuestScore)%2 == 1],
			fnum(len(ctx.probabilities) == 3, ctx.basePredProb),
			fnum(ctx.hasAsianHeat, ctx.asianHeat), fnum(ctx.hasGoalsHeat, ctx.goalsHeat),
			fnum(ctx.hasRecentGls, ctx.recentGoals), fnum(ctx.hasHistory, ctx.historyGoals),
			fnum(ctx.hasRecentDiff, ctx.recentDiff)}
		for _, c := range catalogue {
			fire := c.Evaluate(ctx)
			if !fire.fires {
				rec = append(rec, "", "")
				continue
			}
			dir := fire.direction
			if dir == "" {
				dir = "fire"
			}
			hitCol := ""
			if hit, valid := recommendSettle(fire, ctx); valid {
				hitCol = map[bool]string{true: "1", false: "0"}[hit]
			}
			rec = append(rec, dir, hitCol)
		}
		_ = w.Write(rec)
	}
	t.Logf("dumped %d settled matches", len(settled))
}

func TestWorkshopRecompute(t *testing.T) {
	if os.Getenv("DUMP_ANALYSIS") != "1" {
		t.Skip("set DUMP_ANALYSIS=1 to run the workshop recompute check")
	}
	if err := database.Init(); err != nil {
		t.Fatalf("db init: %v", err)
	}
	snap, err := recomputeWorkshop()
	if err != nil {
		t.Fatalf("recompute: %v", err)
	}
	payload, _ := json.Marshal(snap)
	t.Logf("settled=%d upcoming=%d train=%d test=%d split=%s payloadKB=%d",
		snap.SettledTotal, snap.UpcomingTotal, snap.TrainTotal, snap.TestTotal, snap.SplitDate, len(payload)/1024)
	if p := os.Getenv("DUMP_PATH"); p != "" {
		_ = os.WriteFile(p, payload, 0644)
	}
	if len(snap.Rows) > 0 {
		r := snap.Rows[0]
		t.Logf("sample row: out=%s ah=%s ou=%s oe=%s feats=%d dims=%d", r.Out, r.Ah, r.Ou, r.Oe, len(r.Feat), len(r.Dims))
	}
}
