package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"go_server/database"
	"go_server/internal/drawmodel"
	"go_server/models"
)

type drawExistingSignal struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Market    string   `json:"market"`
	Direction string   `json:"direction"`
	Value     *float64 `json:"value"`
}

type drawExistingAnalysis struct {
	MatchID    string               `json:"matchId"`
	Provenance string               `json:"provenance"`
	Features   map[string]float64   `json:"features"`
	Signals    []drawExistingSignal `json:"signals"`
}

type drawHistoricalGame struct {
	Game     drawmodel.Match
	Sources  []string
	Warnings []string
	Handicap *float64
}

type drawHistorySource struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Title       string `json:"title"`
	Records     int    `json:"records"`
	Matches     int    `json:"matches"`
	Usable      int    `json:"usable"`
	Notes       string `json:"notes"`
	Description string `json:"description"`
}

type drawHistoricalData struct {
	Games    []drawHistoricalGame
	Sources  []drawHistorySource
	Odds     map[string]models.OddsMoney
	Pankou   map[string]models.PankouMoney
	Existing map[string]drawExistingAnalysis
}

func drawSource(key, label string, records, matches, usable int, notes string) drawHistorySource {
	return drawHistorySource{Key: key, Label: label, Title: label, Records: records, Matches: matches, Usable: usable, Notes: notes, Description: notes}
}

// loadDrawHistoricalData expands all stored recent/against fixtures, rather
// than treating each history_moneys document as only one available match.
func loadDrawHistoricalData(existing []drawExistingAnalysis) (drawHistoricalData, error) {
	data := drawHistoricalData{Sources: []drawHistorySource{}, Odds: map[string]models.OddsMoney{}, Pankou: map[string]models.PankouMoney{}, Existing: map[string]drawExistingAnalysis{}}
	var current, legacy []models.Money
	if err := database.DB.Find(&current).Error; err != nil {
		return data, err
	}
	if database.DB.Migrator().HasTable("money") {
		if err := database.DB.Table("money").Find(&legacy).Error; err != nil {
			return data, err
		}
	}
	known := map[string]models.Money{}
	for _, m := range legacy {
		known[m.MatchId] = m
	}
	for _, m := range current {
		known[m.MatchId] = m
	}
	registry := map[string]drawHistoricalGame{}
	add := func(game drawmodel.Match, source string, handicap *float64) {
		if game.Kickoff.IsZero() || !game.Completed || game.HomeGoals < 0 || game.AwayGoals < 0 || game.Kickoff.After(time.Now()) {
			return
		}
		key := game.ID
		if key == "" {
			key = fmt.Sprintf("fixture:%s|%s|%s", game.HomeTeamName, game.AwayTeamName, game.Kickoff.Format(time.RFC3339))
			game.ID = key
		}
		old, found := registry[key]
		if !found {
			registry[key] = drawHistoricalGame{Game: game, Sources: []string{source}, Warnings: []string{}, Handicap: handicap}
			return
		}
		if old.Game.HomeGoals != game.HomeGoals || old.Game.AwayGoals != game.AwayGoals {
			old.Warnings = append(old.Warnings, "不同历史来源比分不一致，优先保留主比赛记录")
		}
		if !drawContains(old.Sources, source) {
			old.Sources = append(old.Sources, source)
		}
		if old.Handicap == nil {
			old.Handicap = handicap
		}
		registry[key] = old
	}
	countCompleted := func(rows []models.Money) int {
		n := 0
		for _, m := range rows {
			if _, _, ok := drawRegulationResult(m); ok {
				n++
			}
		}
		return n
	}
	// Current rows have authority over the legacy table and embedded copies.
	for _, tableRows := range []struct {
		name string
		rows []models.Money
	}{{"moneys", current}, {"money", legacy}} {
		for _, m := range tableRows.rows {
			canonical := known[m.MatchId]
			h, a, ok := drawRegulationResult(canonical)
			if !ok {
				continue
			}
			g := drawModelMatch(canonical)
			g.Completed = true
			g.HomeGoals = h
			g.AwayGoals = a
			g.CompletedAt = g.Kickoff.Add(2 * time.Hour)
			add(g, tableRows.name, nil)
		}
	}
	data.Sources = append(data.Sources, drawSource("moneys", "当前比赛表", len(current), countCompleted(current), countCompleted(current), "主比赛表的常规时间完赛结果"), drawSource("money", "历史比赛表", len(legacy), countCompleted(legacy), countCompleted(legacy), "合并旧版 money 表；与当前表按比赛 ID 去重"))
	var histories []models.HistoryMoney
	if err := database.DB.Find(&histories).Error; err != nil {
		return data, err
	}
	rowCount := 0
	embeddedIDs := map[string]bool{}
	for _, history := range histories {
		games := drawHistoryGames(history, known[history.MatchId])
		lines := drawEmbeddedHandicaps(history)
		for _, game := range games {
			rowCount++
			embeddedIDs[game.ID] = true
			add(game, "history_moneys", lines[game.ID])
		}
	}
	data.Sources = append(data.Sources, drawSource("history_moneys", "历史战绩明细", rowCount, len(embeddedIDs), len(embeddedIDs), fmt.Sprintf("展开 %d 份双方近况与交锋文档；使用目标比赛之前的已完赛历史重建特征", len(histories))))
	var odds []models.OddsMoney
	var pankou []models.PankouMoney
	if err := database.DB.Find(&odds).Error; err != nil {
		return data, err
	}
	if err := database.DB.Find(&pankou).Error; err != nil {
		return data, err
	}
	oddsN, lineN := 0, 0
	for _, o := range odds {
		data.Odds[o.MatchId] = o
		if _, ok := registry[o.MatchId]; ok {
			oddsN++
		}
	}
	for _, p := range pankou {
		data.Pankou[p.MatchId] = p
		if _, ok := registry[p.MatchId]; ok {
			lineN++
		}
	}
	data.Sources = append(data.Sources, drawSource("odds_moneys", "已有欧赔", len(odds), oddsN, oddsN, "有赛前采集时间时使用当时报价；否则仅用来源初赔作探索对照，明确标记时间未经验证"), drawSource("pankou_moneys", "已有盘口", len(pankou), lineN, lineN, "复用历史让球、初盘和大小球；不把终场比分或半场比分作为赛前特征"))
	for _, e := range existing {
		if e.MatchID != "" {
			data.Existing[e.MatchID] = e
		}
	}
	data.Sources = append(data.Sources, drawSource("existing_analysis", "已有分析信号", len(existing), len(existing), len(existing), "复用现有推荐目录、期望让球、球数和市场方向；无赛前时间证明的信号仅列为探索分析"))
	for _, g := range registry {
		data.Games = append(data.Games, g)
	}
	sort.Slice(data.Games, func(i, j int) bool {
		if data.Games[i].Game.Kickoff.Equal(data.Games[j].Game.Kickoff) {
			return data.Games[i].Game.ID < data.Games[j].Game.ID
		}
		return data.Games[i].Game.Kickoff.Before(data.Games[j].Game.Kickoff)
	})
	return data, nil
}

func drawContains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func drawEmbeddedHandicaps(h models.HistoryMoney) map[string]*float64 {
	result := map[string]*float64{}
	for _, raw := range [][]byte{h.RecentHomeList, h.RecentGuestList, h.AgainstList} {
		var rows []map[string]json.RawMessage
		if json.Unmarshal(raw, &rows) != nil {
			continue
		}
		for _, row := range rows {
			id := drawRawID(row["matchId"])
			if v := drawLine(drawRawString(row["letGoal"]), false); v != nil && id != "" {
				result[id] = v
			}
		}
	}
	return result
}

type drawHistoricalIndex struct{ leagues, teams map[string][]drawmodel.Match }

func drawHistoricalTeamKey(id, name string) string {
	if id != "" {
		return "id:" + id
	}
	return "name:" + strings.TrimSpace(name)
}
func indexDrawHistory(games []drawHistoricalGame) drawHistoricalIndex {
	idx := drawHistoricalIndex{leagues: map[string][]drawmodel.Match{}, teams: map[string][]drawmodel.Match{}}
	for i := len(games) - 1; i >= 0; i-- {
		g := games[i].Game
		league := drawLeagueName(g.LeagueName)
		idx.leagues[league] = append(idx.leagues[league], g)
		h, a := drawHistoricalTeamKey(g.HomeTeamID, g.HomeTeamName), drawHistoricalTeamKey(g.AwayTeamID, g.AwayTeamName)
		idx.teams[h] = append(idx.teams[h], g)
		idx.teams[a] = append(idx.teams[a], g)
	}
	return idx
}

func (idx drawHistoricalIndex) before(target drawmodel.Match, asOf time.Time) []drawmodel.Match {
	rows := make([]drawmodel.Match, 0, 1600)
	seen := map[string]bool{}
	appendPast := func(pool []drawmodel.Match, limit int) {
		n := 0
		for _, g := range pool {
			if g.ID == target.ID || !g.CompletedAt.Before(asOf) || seen[g.ID] {
				continue
			}
			seen[g.ID] = true
			rows = append(rows, g)
			n++
			if n >= limit {
				break
			}
		}
	}
	appendPast(idx.leagues[drawLeagueName(target.LeagueName)], 1500)
	appendPast(idx.teams[drawHistoricalTeamKey(target.HomeTeamID, target.HomeTeamName)], 40)
	appendPast(idx.teams[drawHistoricalTeamKey(target.AwayTeamID, target.AwayTeamName)], 40)
	return rows
}

func historicalDrawOdds(data drawHistoricalData, game drawHistoricalGame, asOf time.Time) (*drawmodel.Odds, string) {
	odds, pankou := data.Odds[game.Game.ID], data.Pankou[game.Game.ID]
	if quote, _, _ := drawMarketData(odds, pankou, asOf); quote != nil {
		return quote, "captured_prematch"
	}
	// Opening odds describe an earlier market but lack a certified capture time.
	// This group remains explicitly exploratory, including during comparison.
	rows := euroOddsRows(odds)
	sum := [3]float64{}
	n := 0
	seen := map[string]bool{}
	for _, row := range rows {
		if isAverageOddRow(row) || len(row.FirstOdds) < 3 || seen[row.CompanyID] {
			continue
		}
		v := [3]float64{parseFloat(row.FirstOdds[0]), parseFloat(row.FirstOdds[1]), parseFloat(row.FirstOdds[2])}
		if !drawValidOdds(v) {
			continue
		}
		seen[row.CompanyID] = true
		z := 1/v[0] + 1/v[1] + 1/v[2]
		for i, x := range v {
			sum[i] += (1 / x) / z
		}
		n++
	}
	var quote *drawmodel.Odds
	if n > 0 {
		quote = &drawmodel.Odds{Home: float64(n) / sum[0], Draw: float64(n) / sum[1], Away: float64(n) / sum[2], CapturedAt: asOf}
	}
	var handicap, totalLine *float64
	if game.Handicap != nil {
		handicap = game.Handicap
	}
	asia, totals := pankouRows(pankou)
	a := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Asia), asia, 8, 281)
	t := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Dxq), totals, 8, 281)
	if v := drawLine(a.FirstPankou, false); v != nil {
		handicap = v
	}
	totalLine = drawLine(t.FirstPankou, true)
	if quote == nil && (handicap != nil || totalLine != nil) {
		quote = &drawmodel.Odds{CapturedAt: asOf}
	}
	if quote != nil {
		quote.AsianHandicap = handicap
		quote.GoalLine = totalLine
		return quote, "opening_reconstructed"
	}
	return nil, "missing"
}
