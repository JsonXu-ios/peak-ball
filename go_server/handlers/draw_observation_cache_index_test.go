package handlers

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

func drawObservationIndexFixture(n int) []drawHistoricalGame {
	games := make([]drawHistoricalGame, n)
	base := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for i := range games {
		home, away := fmt.Sprint(i%1400+1), fmt.Sprint((i+137)%1400+1)
		games[i] = drawObservationGame(fmt.Sprint(i+1), home, away, base.Add(-time.Duration(i%3400)*time.Hour), i%3, i%2)
		games[i].Game.HomeTeamName, games[i].Game.AwayTeamName = "Team "+home, "Team "+away
	}
	rand.New(rand.NewSource(42)).Shuffle(len(games), func(i, j int) { games[i], games[j] = games[j], games[i] })
	return games
}

func TestDrawObservationsCacheIndexMatchesGenericPoolsAndWindows(t *testing.T) {
	games := drawObservationIndexFixture(3000)
	base := games[0].Game.Kickoff.Add(365 * 24 * time.Hour)
	// Matching names with different known IDs must remain ambiguous, while a
	// repeated identity within one fixture must enter its pool only once.
	games[0].Game.HomeTeamName, games[0].Game.AwayTeamName = "Shared", "Shared"
	games[1].Game.HomeTeamID, games[1].Game.AwayTeamID = "same", "same"
	games[1].Game.HomeTeamName, games[1].Game.AwayTeamName = " Same ", "same"
	games[2].Game.HomeTeamID, games[2].Game.HomeTeamName = "", "Shared"
	games[3].Game.ID, games[4].Game.ID = "", ""
	games[5].Game.HomeTeamName = "Old Team Name"
	games[6].Game.HomeTeamID = ""
	for i := 0; i < 14; i++ {
		game := drawObservationGame("", "tie-home", fmt.Sprint("tie-away-", i), base.Add(-24*time.Hour), i%2, 0)
		game.Game.HomeTeamName = "Tie Home"
		games = append(games, game)
	}
	for _, duplicate := range []bool{false, true} {
		t.Run(fmt.Sprint("duplicates=", duplicate), func(t *testing.T) {
			input := append([]drawHistoricalGame{}, games...)
			if duplicate {
				input = append(input, games[0], games[1])
				conflict := games[0]
				conflict.Game.HomeTeamID, conflict.Game.HomeTeamName, conflict.Game.Kickoff = "conflict-team", "Conflict", base
				input = append(input, conflict)
			}
			got, want := indexDrawObservationCache(input), indexDrawObservations(input)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("cache index changed ordering, identity ambiguity or deduplication")
			}
			for _, game := range games[:60] {
				target := game.Game
				target.ID = "future-target"
				target.Kickoff = base
				if !reflect.DeepEqual(got.windows(target), want.windows(target)) {
					t.Fatal("cache index changed pre-match recent/H2H windows")
				}
			}
			tieTarget := drawObservationGame("tie-target", "tie-home", "tie-away-0", base, 0, 0).Game
			if !reflect.DeepEqual(got.windows(tieTarget), want.windows(tieTarget)) {
				t.Fatal("same-time empty-ID stable ties changed the ten-match cutoff")
			}
		})
	}
}

func BenchmarkDrawObservationsHistoryIndex(b *testing.B) {
	games := drawObservationIndexFixture(51836)
	for _, implementation := range []struct {
		name string
		fn   func([]drawHistoricalGame) drawObservationIndex
	}{{"generic", indexDrawObservations}, {"reserved_global_order", indexDrawObservationCache}} {
		b.Run(implementation.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if len(implementation.fn(games).byID) != 1400 {
					b.Fatal("fixture lost team identities")
				}
			}
		})
	}
}
