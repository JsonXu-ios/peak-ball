package drawmodel

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const maxHistory = 5000

func normalizedName(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

func teamKey(id, name string) string {
	if strings.TrimSpace(id) != "" {
		return "id:" + strings.TrimSpace(id)
	}
	if name = normalizedName(name); name != "" {
		return "name:" + name
	}
	return ""
}

// Matching uses names only when at least one identifier is missing. Distinct
// known IDs are never merged because their display names happen to coincide.
func sameTeam(id, name, otherID, otherName string) (bool, bool) {
	id, otherID = strings.TrimSpace(id), strings.TrimSpace(otherID)
	if id != "" && otherID != "" {
		return id == otherID, false
	}
	a, b := normalizedName(name), normalizedName(otherName)
	return a != "" && a == b, a != "" && a == b
}

func sameLeague(a, b Match) bool {
	if a.LeagueID != "" && b.LeagueID != "" {
		return a.LeagueID == b.LeagueID
	}
	an, bn := normalizedName(a.LeagueName), normalizedName(b.LeagueName)
	return an != "" && an == bn
}

func availableAt(m Match) time.Time {
	if !m.CompletedAt.IsZero() && m.CompletedAt.After(m.Kickoff) {
		return m.CompletedAt
	}
	return m.Kickoff.Add(2 * time.Hour)
}

func matchKey(m Match) string {
	if strings.TrimSpace(m.ID) != "" {
		return "id:" + strings.TrimSpace(m.ID)
	}
	return fmt.Sprintf("fixture:%s|%s|%d", teamKey(m.HomeTeamID, m.HomeTeamName), teamKey(m.AwayTeamID, m.AwayTeamName), m.Kickoff.UnixNano())
}

func usableHistory(in Input, cutoff time.Time) ([]Match, int) {
	// Sort before deduplication so a stale copy cannot replace a later, valid
	// completed copy simply because of the input order.
	valid := make([]Match, 0, len(in.History))
	for _, m := range in.History {
		if !m.Completed || m.Kickoff.IsZero() || !m.Kickoff.Before(cutoff) || availableAt(m).After(cutoff) ||
			m.HomeGoals < 0 || m.AwayGoals < 0 || m.HomeGoals > 30 || m.AwayGoals > 30 ||
			teamKey(m.HomeTeamID, m.HomeTeamName) == "" || teamKey(m.AwayTeamID, m.AwayTeamName) == "" {
			continue
		}
		if same, _ := sameTeam(m.HomeTeamID, m.HomeTeamName, m.AwayTeamID, m.AwayTeamName); same {
			continue
		}
		if in.Target.ID != "" && m.ID == in.Target.ID {
			continue
		}
		if m.Kickoff.Equal(in.Target.Kickoff) {
			h, _ := sameTeam(in.Target.HomeTeamID, in.Target.HomeTeamName, m.HomeTeamID, m.HomeTeamName)
			a, _ := sameTeam(in.Target.AwayTeamID, in.Target.AwayTeamName, m.AwayTeamID, m.AwayTeamName)
			if h && a {
				continue
			}
		}
		valid = append(valid, m)
	}
	sort.SliceStable(valid, func(i, j int) bool {
		if valid[i].Kickoff.Equal(valid[j].Kickoff) {
			if valid[i].CompletedAt.Equal(valid[j].CompletedAt) {
				return matchKey(valid[i]) < matchKey(valid[j])
			}
			return valid[i].CompletedAt.After(valid[j].CompletedAt)
		}
		return valid[i].Kickoff.After(valid[j].Kickoff)
	})
	seen := make(map[string]bool, len(valid))
	result := make([]Match, 0, len(valid))
	duplicates := 0
	for _, m := range valid {
		key := matchKey(m)
		if seen[key] {
			duplicates++
			continue
		}
		seen[key] = true
		result = append(result, m)
		if len(result) == maxHistory {
			break
		}
	}
	return result, duplicates
}

func recentForm(history []Match, id, name string, venue int, baseline float64) (Form, bool) {
	f := Form{MatchIDs: []string{}}
	fallback := false
	for _, m := range history {
		home, hf := sameTeam(id, name, m.HomeTeamID, m.HomeTeamName)
		away, af := sameTeam(id, name, m.AwayTeamID, m.AwayTeamName)
		if !(home || away) || (venue == 1 && !home) || (venue == -1 && !away) {
			continue
		}
		fallback = fallback || hf || af
		gf, ga := m.HomeGoals, m.AwayGoals
		if away {
			gf, ga = ga, gf
		}
		f.N++
		f.MatchIDs = append(f.MatchIDs, m.ID)
		f.GoalsFor += float64(gf)
		f.GoalsAgainst += float64(ga)
		if gf == ga {
			f.Draws++
		}
		if gf+ga <= 2 {
			f.LowScoreRate++
		}
		if ga == 0 {
			f.CleanSheetRate++
		}
		if gf == 0 {
			f.ScorelessRate++
		}
		if f.N == 10 {
			break
		}
	}
	if f.N > 0 {
		n := float64(f.N)
		f.DrawRate = float64(f.Draws) / n
		f.GoalsFor /= n
		f.GoalsAgainst /= n
		f.LowScoreRate /= n
		f.CleanSheetRate /= n
		f.ScorelessRate /= n
	}
	// Eight pseudo-matches prevent ten-game draw rates from being interpreted
	// as an unregularized estimate of the next match's draw probability.
	f.SmoothedDrawRate = (float64(f.Draws) + 8*baseline) / (float64(f.N) + 8)
	return f, fallback
}
