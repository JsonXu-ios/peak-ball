package handlers

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"go_server/models"
	"gorm.io/datatypes"
)

// Retain the prior observation adapter around the shared parser as the oracle.
// These fixtures exercise the complete source -> registry -> windows path.
func drawObservationsGenericHistory(targets []drawObservationTarget, histories []models.HistoryMoney, now time.Time) []drawHistoricalGame {
	known, registry := map[string]models.Money{}, map[string]drawHistoricalGame{}
	for _, target := range targets {
		m := target.Match
		known[m.MatchId] = m
		if hg, ag, ok := drawRegulationResult(m); ok {
			g := drawModelMatch(m)
			if g.Kickoff.IsZero() || g.Kickoff.After(now) {
				continue
			}
			g.Completed, g.HomeGoals, g.AwayGoals, g.CompletedAt = true, hg, ag, g.Kickoff.Add(2*time.Hour)
			registry[g.ID] = drawHistoricalGame{Game: g, Sources: target.Sources}
		}
	}
	for _, history := range histories {
		for _, game := range drawHistoryGames(history, known[history.MatchId]) {
			if game.Kickoff.After(now) {
				continue
			}
			if m, exists := known[game.ID]; exists {
				if _, _, valid := drawRegulationResult(m); !valid || drawSourceTime(m.MatchTime).After(now) {
					continue
				}
			}
			key := drawObservationGameKey(game)
			if _, exists := registry[key]; !exists {
				registry[key] = drawHistoricalGame{Game: game, Sources: []string{"history_moneys"}}
			}
		}
	}
	pool := make([]drawHistoricalGame, 0, len(registry))
	for _, game := range registry {
		pool = append(pool, game)
	}
	return pool
}

func drawObservationsSortedHistory(pool []drawHistoricalGame) []drawHistoricalGame {
	sort.Slice(pool, func(i, j int) bool {
		return drawObservationGameKey(pool[i].Game) < drawObservationGameKey(pool[j].Game)
	})
	return pool
}

func TestDrawObservationsTypedHistoryMatchesGenericRawInputs(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	main := drawObservationMain(drawObservationGame("main", "101", "202", now.Add(-72*time.Hour), 2, 1))
	main.Match.League, main.Match.LeagueId = "League", 99
	blocked := drawObservationMain(drawObservationGame("blocked", "101", "202", now.Add(-24*time.Hour), 0, 0))
	blocked.Match.DisplayState = "取消"
	mainFuture := drawObservationMain(drawObservationGame("main-future", "101", "202", now.Add(24*time.Hour), 0, 0))
	unsettled := drawObservationMain(drawObservationGame("unsettled", "101", "202", now.Add(-24*time.Hour), 0, 0))
	unsettled.Match.DisplayState, unsettled.Match.Status = "未开赛", 0
	ot := drawObservationMain(drawObservationGame("main-ot", "101", "202", now.Add(-24*time.Hour), 1, 1))
	ot.Match.HomeOtScore = 1
	targets := []drawObservationTarget{main, blocked, mainFuture, unsettled, ot}
	for _, fixture := range []struct{ name, raw string }{
		{"source_fields_and_types", `[
		 {"matchId":123,"matchTime":"2026-10-21 18:00:00","homeId":101,"guestId":"202","home":" 主队 ","guest":"客队","goal":["0",0,9],"league":"League"},
		 {"matchId":"00124","matchTime":"2026-10-21T10:00:00Z","homeId":"00101","guestId":202,"home":"主\u961f","guest":"客队","goal":[1,"2"],"leagueId":"7"},
		 {"matchId":"offset","matchTime":"2026-10-21T18:00:00+08:00","homeId":101,"guestId":202,"home":"Quote \" club","guest":202,"goal":[30,30]},
		 {"matchId":"date-only","matchTime":"2026/10/20","homeId":101,"guestId":202,"goal":[0,0]},
		 {"matchId":0,"matchTime":"2026-10-20","homeId":0,"guestId":-2,"home":"No ID","guest":"Other","goal":[0,0]},
		 {"matchId":-2,"matchTime":"2026-10-20","homeId":0,"guestId":-2,"home":"No ID","guest":"Other","goal":[0,0]},
		 {"matchId":"","matchTime":"2026-10-20","home":"different","guest":"Other","goal":[0,0]},
		 {"matchTime":"2026-10-20 18:00","homeId":101,"guestId":202,"home":"主队","guest":"客队","goal":[0,0]},
		 {"matchId":"fraction","matchTime":"2026-10-20 18:00","homeId":1.5,"guestId":true,"goal":[1,1]},
		 null
		]`},
		{"invalid_rows_then_valid_duplicate", `[
		 {"matchId":"after-bad-goals","matchTime":"2026-10-20 18:00","goal":[null,0]},
		 {"matchId":"after-bad-goals","matchTime":"2026-10-20 18:00","homeId":101,"guestId":202,"goal":[1,1]},
		 {"matchId":"after-bad-date","matchTime":"bad","goal":[1,1]},
		 {"matchId":"after-bad-date","matchTime":"2026-10-20 18:00","goal":[1,0]},
		 {"matchId":"after-blocked","matchTime":"2026-10-20 18:00","displayState":"比赛推迟","goal":[0,0]},
		 {"matchId":"after-blocked","matchTime":"2026-10-20 18:00","goal":[0,0]},
		 {"matchId":"after-future","matchTime":"2027-10-20 18:00","goal":[0,0]},
		 {"matchId":"after-future","matchTime":"2026-10-20 18:00","goal":[0,0]},
		 {"matchId":"too-many","matchTime":"2026-10-20 18:00","goal":[31,0]},
		 {"matchId":"too-few","matchTime":"2026-10-20 18:00","goal":[1]},
		 {"matchId":"bad-goal","matchTime":"2026-10-20 18:00","goal":["bad",0]},
		 {"matchId":"negative","matchTime":"2026-10-20 18:00","goal":[-1,0]},
		 {"matchId":"null-goal","matchTime":"2026-10-20 18:00","goal":null},
		 {"matchId":"ot","matchTime":"2026-10-20 18:00","homeOtScore":"1","goal":[0,0]},
		 {"matchId":"penalty","matchTime":"2026-10-20 18:00","guestOtPenalty":2,"goal":[0,0]},
		 {"matchId":"zero-ot","matchTime":"2026-10-20 18:00","homeOtScore":"0","guestOtScore":0,"homeOtPenalty":null,"goal":[0,0]}
		]`},
		{"first_valid_copy_and_main_authority", `[
		 {"matchId":"first","matchTime":"2026-10-20 18:00","home":"first name","goal":[1,0]},
		 {"matchId":"first","matchTime":"2026-10-20 18:00","homeId":101,"guestId":202,"home":"later name","goal":[0,0]},
		 {"matchId":"main","matchTime":"2026-10-20 18:00","home":"stale","goal":[0,0]},
		 {"matchId":"blocked","matchTime":"2026-10-20 18:00","goal":[0,0]},
		 {"matchId":"main-future","matchTime":"2026-10-20 18:00","goal":[0,0]},
		 {"matchId":"unsettled","matchTime":"2026-10-20 18:00","goal":[0,0]},
		 {"matchId":"main-ot","matchTime":"2026-10-20 18:00","goal":[0,0]},
		 {"matchId":"at-now","matchTime":"2026-10-22T12:00:00Z","goal":[0,0]},
		 {"matchId":"past-now","matchTime":"2026-10-22T12:00:00.000000001Z","goal":[0,0]}
		]`},
		{"malformed_array_discards_all", `[{"matchId":"must-not-enter","matchTime":"2026-10-20 18:00","goal":[0,0]},7]`},
		{"malformed_json_discards_all", `[{"matchId":"must-not-enter","matchTime":"2026-10-20 18:00","goal":[0,0]},`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			histories := []models.HistoryMoney{{MatchId: "main", RecentHomeList: datatypes.JSON(fixture.raw)}}
			want := drawObservationsSortedHistory(drawObservationsGenericHistory(targets, histories, now))
			got := drawObservationsSortedHistory(drawObservationParsedHistory(targets, histories, now))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("typed parser changed results\ngot: %+v\nwant: %+v", got, want)
			}
		})
	}
}

func TestDrawObservationsTypedAgainstToHeadToHeadMatchesGeneric(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	target := drawObservationMain(drawObservationGame("today", "101", "202", now.Add(-4*time.Hour), 1, 1))
	target.Match.Home, target.Match.Guest = "Home", "Away"
	first := `[{"matchId":1,"matchTime":"2026-09-01 18:00:00","homeId":101,"guestId":202,"home":"Home","guest":"Away","goal":[0,0]}]`
	second := `[{"matchId":2,"matchTime":"2026-08-01 18:00:00","homeId":202,"guestId":101,"home":"Away","guest":"Home","goal":[2,1]}]`
	against := `[
	 {"matchId":1,"matchTime":"2026-09-01 18:00:00","homeId":101,"guestId":202,"home":"Home","guest":"Away","goal":[8,0]},
	 {"matchId":3,"matchTime":"2026-07-01 18:00:00","homeId":202,"guestId":101,"home":"Away","guest":"Home","goal":[0,0]},
	 {"matchId":"today","matchTime":"2026-10-22T08:00:00Z","homeId":101,"guestId":202,"home":"Home","guest":"Away","goal":[1,1]},
	 {"matchId":4,"matchTime":"2026-10-22T07:00:00Z","homeId":101,"guestId":202,"home":"Home","guest":"Away","goal":[0,0]},
	 {"matchId":5,"matchTime":"2026-10-23 18:00:00","homeId":101,"guestId":202,"home":"Home","guest":"Away","goal":[0,0]}
	]`
	for _, packed := range []bool{false, true} {
		t.Run(fmt.Sprintf("packed=%t", packed), func(t *testing.T) {
			history := models.HistoryMoney{MatchId: "today", RecentHomeList: datatypes.JSON(first), RecentGuestList: datatypes.JSON(second), AgainstList: datatypes.JSON(against)}
			if packed {
				history = models.HistoryMoney{MatchId: "today", RecentHomeList: datatypes.JSON(" null "), AgainstList: datatypes.JSON(" [] "), LeagueStat: datatypes.JSON(`{"recent":{"home":{"list":` + first + `},"guest":{"list":` + second + `}},"against":{"list":` + against + `}}`)}
			}
			histories := []models.HistoryMoney{history, history}
			got := drawObservationsSortedHistory(drawObservationParsedHistory([]drawObservationTarget{target}, histories, now))
			want := drawObservationsSortedHistory(drawObservationsGenericHistory([]drawObservationTarget{target}, histories, now))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("source precedence or fallback changed: got=%+v want=%+v", got, want)
			}
			gotWindow := indexDrawObservations(got).windows(drawModelMatch(target.Match))
			wantWindow := indexDrawObservations(want).windows(drawModelMatch(target.Match))
			if !reflect.DeepEqual(gotWindow, wantWindow) || gotWindow.h2h.Sample != 3 || gotWindow.h2h.Draws != 2 || gotWindow.h2h.Matches[1].Result != "loss" || gotWindow.h2h.Matches[1].Venue != "away" {
				t.Fatalf("raw against data did not reach correct H2H window: %+v", gotWindow.h2h)
			}
		})
	}
}

func TestDrawObservationsParallelHistoryPreservesDocumentOrderAndFirstValidAcrossBatches(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	main := drawObservationMain(drawObservationGame("main", "101", "202", now.Add(-72*time.Hour), 2, 1))
	blocked := drawObservationMain(drawObservationGame("blocked", "101", "202", now.Add(-72*time.Hour), 0, 0))
	blocked.Match.DisplayState = "取消"
	targets := []drawObservationTarget{main, blocked}
	histories := make([]models.HistoryMoney, 97)
	for i := range histories {
		row := fmt.Sprintf(`[{"matchId":"row-%d","matchTime":"2026-09-01 18:00:00","homeId":101,"guestId":202,"home":"Home","guest":"Away","goal":[%d,0]}]`, i, i%3)
		histories[i] = models.HistoryMoney{RecentHomeList: datatypes.JSON(row)}
		if i%5 == 0 {
			histories[i] = models.HistoryMoney{RecentHomeList: datatypes.JSON("null"), AgainstList: datatypes.JSON("[]"), LeagueStat: datatypes.JSON(`{"against":{"list":` + row + `}}`)}
		}
	}
	histories[0].RecentHomeList = datatypes.JSON(`[{"matchId":"main","matchTime":"2026-09-01","goal":[0,0]},{"matchId":"blocked","matchTime":"2026-09-01","goal":[0,0]}]`)
	histories[30].AgainstList = datatypes.JSON(`[{"matchId":"first-valid","matchTime":"2026-09-01","homeId":101,"guestId":202,"goal":[2,0]}]`)
	histories[31].RecentHomeList = datatypes.JSON(`[{"matchId":"after-invalid","matchTime":"2026-09-01","goal":[null,0]}]`)
	histories[32].RecentHomeList = datatypes.JSON(`[{"matchId":"after-invalid","matchTime":"2026-09-01","homeId":101,"guestId":202,"goal":[1,1]}]`)
	histories[63].AgainstList = datatypes.JSON(`[{"matchId":"first-valid","matchTime":"2026-09-01","homeId":303,"guestId":404,"goal":[0,0]}]`)
	histories[64].RecentHomeList = datatypes.JSON(`[{"matchId":"after-future","matchTime":"2027-09-01","goal":[0,0]}]`)
	histories[65].RecentHomeList = datatypes.JSON(`[{"matchId":"after-future","matchTime":"2026-09-01","homeId":101,"guestId":202,"goal":[0,0]}]`)
	histories[66].RecentHomeList = datatypes.JSON(`[{"matchId":"bad-array","matchTime":"2026-09-01","goal":[0,0]},7]`)
	histories[67].RecentHomeList = datatypes.JSON(`[{"matchId":"bad-array","matchTime":"2026-09-01","homeId":101,"guestId":202,"goal":[3,0]}]`)
	// Preserve even the old malformed whitespace-split suppression of a blob.
	histories[96] = models.HistoryMoney{RecentHomeList: datatypes.JSON(" "), LeagueStat: datatypes.JSON(`{"against":{"list":[{"matchId":"must-stay-suppressed","matchTime":"2026-09-01","goal":[0,0]}]}}`)}
	want := drawObservationsSortedHistory(drawObservationsGenericHistory(targets, histories, now))
	for _, workers := range []int{1, 4, 8} {
		got := drawObservationsSortedHistory(drawObservationParsedHistoryWorkers(targets, histories, now, workers))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%d workers changed source/first-valid priority", workers)
		}
		future := drawObservationGame("future-window", "101", "202", now.Add(24*time.Hour), 0, 0).Game
		if !reflect.DeepEqual(indexDrawObservationCache(got).windows(future), indexDrawObservations(want).windows(future)) {
			t.Fatalf("%d workers changed recent/H2H windows", workers)
		}
	}
}

func BenchmarkDrawObservationsHistoryParsing(b *testing.B) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	// Each fixture appears in three source arrays, approximating repeated
	// recent/against records. The benchmark measures CPU/allocations, not SQL.
	histories := make([]models.HistoryMoney, 400)
	var bytes int64
	for i := range histories {
		rows := make([]map[string]any, 30)
		for j := range rows {
			id := i*len(rows) + j + 1
			rows[j] = map[string]any{"matchId": id, "matchTime": "2026-09-01 18:00:00", "homeId": id%120 + 1, "guestId": id%120 + 121, "home": fmt.Sprintf("主队%d", id%120), "guest": fmt.Sprintf("客队%d", id%120), "league": "League", "goal": []int{id % 3, id % 2}, "halfGoal": []int{0, 0}, "scheduleId": 8, "letGoal": 0, "separate": 0}
		}
		raw, err := json.Marshal(rows)
		if err != nil {
			b.Fatal(err)
		}
		histories[i] = models.HistoryMoney{RecentHomeList: raw, RecentGuestList: raw, AgainstList: raw}
		bytes += int64(3 * len(raw))
	}
	for _, parser := range []struct {
		name string
		fn   func([]drawObservationTarget, []models.HistoryMoney, time.Time) []drawHistoricalGame
	}{{"generic", drawObservationsGenericHistory}, {"typed_serial", func(targets []drawObservationTarget, histories []models.HistoryMoney, at time.Time) []drawHistoricalGame {
		return drawObservationParsedHistoryWorkers(targets, histories, at, 1)
	}}, {"typed_parallel4", func(targets []drawObservationTarget, histories []models.HistoryMoney, at time.Time) []drawHistoricalGame {
		return drawObservationParsedHistoryWorkers(targets, histories, at, 4)
	}}, {"typed_parallel8", drawObservationParsedHistory}} {
		b.Run(parser.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(bytes)
			for i := 0; i < b.N; i++ {
				if got := len(parser.fn(nil, histories, now)); got != 12000 {
					b.Fatalf("fixture changed: got %d unique matches", got)
				}
			}
		})
	}
}
