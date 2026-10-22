package handlers

import (
	"cmp"
	"slices"
	"strings"

	"go_server/internal/drawmodel"
)

// The observation parser has already resolved source priority into a unique
// registry. Order that registry once and reserve each team's exact capacity,
// instead of separately sorting, copying and deduplicating every ID/name pool.
// Arbitrary injected input with duplicate keys retains the generic behavior.
func indexDrawObservationCache(games []drawHistoricalGame) drawObservationIndex {
	type entry struct {
		game       *drawmodel.Match
		ids, names [2]string
	}
	entries := make([]entry, len(games))
	order := make([]int, len(games))
	seen := make(map[string]bool, len(games))
	idCounts, nameCounts := map[string]int{}, map[string]int{}
	idName := map[string]string{}
	idMultipleNames, nameMissingID := map[string]bool{}, map[string]bool{}
	idx := drawObservationIndex{byID: map[string][]drawmodel.Match{}, byName: map[string][]drawmodel.Match{}, nameIDs: map[string]map[string]bool{}}
	for i := range games {
		game := &games[i].Game
		key := drawObservationGameKey(*game)
		if seen[key] {
			return indexDrawObservations(games)
		}
		seen[key] = true
		item := entry{game: game, ids: [2]string{strings.TrimSpace(game.HomeTeamID), strings.TrimSpace(game.AwayTeamID)}, names: [2]string{drawObservationName(game.HomeTeamName), drawObservationName(game.AwayTeamName)}}
		entries[i] = item
		order[i] = i
		for side := 0; side < 2; side++ {
			id, name := item.ids[side], item.names[side]
			if id != "" {
				if old, exists := idName[id]; exists && old != name {
					idMultipleNames[id] = true
				} else {
					idName[id] = name
				}
			} else if name != "" {
				nameMissingID[name] = true
			}
			if id != "" && (side == 0 || id != item.ids[0]) {
				idCounts[id]++
			}
			if name != "" {
				if side == 0 || name != item.names[0] {
					nameCounts[name]++
				}
				if id != "" {
					if idx.nameIDs[name] == nil {
						idx.nameIDs[name] = map[string]bool{}
					}
					idx.nameIDs[name][id] = true
				}
			}
		}
	}
	for id, count := range idCounts {
		idx.byID[id] = make([]drawmodel.Match, 0, count)
	}
	sharedNames := map[string]string{}
	for name, count := range nameCounts {
		// Both indexes are immutable. When every occurrence has exactly this
		// ID/name pair, their ordered contents are identical and may share one
		// backing slice. Aliases, missing IDs and ambiguous names stay separate.
		if !nameMissingID[name] && len(idx.nameIDs[name]) == 1 {
			for id := range idx.nameIDs[name] {
				if !idMultipleNames[id] && idName[id] == name {
					sharedNames[name] = id
				}
			}
		}
		if sharedNames[name] != "" {
			continue
		}
		idx.byName[name] = make([]drawmodel.Match, 0, count)
	}
	slices.SortFunc(order, func(i, j int) int {
		a, b := entries[i].game, entries[j].game
		if result := b.Kickoff.Compare(a.Kickoff); result != 0 {
			return result
		}
		if result := cmp.Compare(a.ID, b.ID); result != 0 {
			return result
		}
		// Original position makes this equivalent to the old stable ordering.
		return cmp.Compare(i, j)
	})
	for _, i := range order {
		item := entries[i]
		for side := 0; side < 2; side++ {
			id, name := item.ids[side], item.names[side]
			if id != "" && (side == 0 || id != item.ids[0]) {
				idx.byID[id] = append(idx.byID[id], *item.game)
			}
			if name != "" && sharedNames[name] == "" && (side == 0 || name != item.names[0]) {
				idx.byName[name] = append(idx.byName[name], *item.game)
			}
		}
	}
	for name, id := range sharedNames {
		idx.byName[name] = idx.byID[id]
	}
	return idx
}
