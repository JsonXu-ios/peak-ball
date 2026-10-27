package drawmodeldata

import (
	"math"
	"sort"
	"time"
)

type safeHistoryRecord struct {
	kickoff                time.Time
	id                     string
	forGoals, againstGoals float64
}

func featureTime(value interface{}) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	return parsed, err == nil
}

func safeHistoryRecords(row map[string]interface{}, block string) ([]safeHistoryRecord, bool) {
	target, valid := featureTime(row["matchTime"])
	container, present := rawPath(row, "preMatchHistory", block).(map[string]interface{})
	if !valid || !present {
		return nil, false
	}
	items, present := container["matches"].([]interface{})
	if !present {
		return nil, false
	}
	result := make([]safeHistoryRecord, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		r := rawObject(item)
		kickoff, known := featureTime(r["matchTime"])
		if !known || !kickoff.Add(2*time.Hour).Before(target) || featureString(r["matchId"]) == featureString(row["matchId"]) {
			continue
		}
		h, hok := featureNumber(r["homeScore"])
		a, aok := featureNumber(r["awayScore"])
		// Scores must be genuine JSON numbers, never parser-default strings.
		_, hn := r["homeScore"].(float64)
		_, an := r["awayScore"].(float64)
		venue := featureString(r["venue"])
		if !hok || !aok || !hn || !an || h < 0 || a < 0 || h > 30 || a > 30 || math.Trunc(h) != h || math.Trunc(a) != a || (venue != "home" && venue != "away") {
			continue
		}
		id := featureString(r["matchId"])
		if id == "" {
			id = kickoff.Format(time.RFC3339Nano) + "|" + featureString(r["home"]) + "|" + featureString(r["guest"])
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if venue == "away" {
			h, a = a, h
		}
		result = append(result, safeHistoryRecord{kickoff: kickoff, id: id, forGoals: h, againstGoals: a})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].kickoff.Equal(result[j].kickoff) {
			return result[i].id > result[j].id
		}
		return result[i].kickoff.After(result[j].kickoff)
	})
	return result, true
}

func historyStatistics(records []safeHistoryRecord, present bool, limit int) map[string]float64 {
	result := map[string]float64{}
	if !present {
		return result
	}
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	n := float64(len(records))
	var draws, wins, gf, ga, difference, under, zero, one, clean, scoreless, streak float64
	for i, record := range records {
		h, a := record.forGoals, record.againstGoals
		gf += h
		ga += a
		difference += h - a
		if h == a {
			draws++
			if streak == float64(i) {
				streak++
			}
		}
		if h > a {
			wins++
		}
		if h+a <= 2 {
			under++
		}
		if h == 0 && a == 0 {
			zero++
		}
		if h == 1 && a == 1 {
			one++
		}
		if a == 0 {
			clean++
		}
		if h == 0 {
			scoreless++
		}
	}
	result["n"], result["draws"], result["wins"], result["losses"], result["drawStreak"] = n, draws, wins, n-draws-wins, streak
	if n > 0 {
		for key, value := range map[string]float64{"drawRate": draws, "winRate": wins, "lossRate": n - draws - wins, "goalsFor": gf, "goalsAgainst": ga, "totalGoals": gf + ga, "goalDifference": difference, "under25Rate": under, "zeroZeroRate": zero, "oneOneRate": one, "cleanSheetRate": clean, "scorelessRate": scoreless} {
			result[key] = value / n
		}
	}
	return result
}

func emitHistory(out map[string]float64, prefix string, stats map[string]float64, h2h bool) {
	for key, value := range stats {
		if h2h && (key == "wins" || key == "losses" || key == "drawStreak") {
			continue
		}
		putFeature(out, prefix+"."+key, value)
	}
}

func extractHistoryFeatures(row map[string]interface{}, out map[string]float64) {
	target, targetKnown := featureTime(row["matchTime"])
	stats := map[string]map[string]float64{}
	rests := map[string]float64{}
	for _, side := range []string{"home", "away"} {
		block := "homeRecent"
		if side == "away" {
			block = "awayRecent"
		}
		records, present := safeHistoryRecords(row, block)
		for _, window := range []struct {
			name string
			n    int
		}{{"5", 5}, {"10", 10}} {
			values := historyStatistics(records, present, window.n)
			emitHistory(out, "safeRecent."+side+"."+window.name, values, false)
			if window.n == 10 {
				stats[side] = values
			}
		}
		if !present {
			continue
		}
		latest := records
		if len(latest) > 10 {
			latest = latest[:10]
		}
		prefix := "conflict.recent." + side
		n3 := len(latest)
		if n3 > 3 {
			n3 = 3
		}
		putFeature(out, prefix+".3.n", float64(n3))
		if n3 > 0 {
			wins, unbeaten, streakWins := 0., 0., 0.
			for _, r := range latest {
				if r.forGoals <= r.againstGoals {
					break
				}
				streakWins++
			}
			for _, r := range latest {
				if r.forGoals < r.againstGoals {
					break
				}
				unbeaten++
			}
			for _, r := range latest[:n3] {
				if r.forGoals > r.againstGoals {
					wins++
				}
			}
			putFeature(out, prefix+".winStreak", streakWins)
			putFeature(out, prefix+".unbeatenStreak", unbeaten)
			putFeature(out, prefix+".3.wins", wins)
			putFeature(out, prefix+".3.winRate", wins/float64(n3))
		}
		if targetKnown && len(records) > 0 {
			rest := target.Sub(records[0].kickoff).Hours() / 24
			rests[side] = rest
			putFeature(out, "context.schedule."+side+".restDays", rest)
			for _, days := range []int{7, 14} {
				count := 0.
				since := target.Add(-time.Duration(days) * 24 * time.Hour)
				for _, r := range records {
					if !r.kickoff.Before(since) {
						count++
					}
				}
				key := "known7Days"
				if days == 14 {
					key = "known14Days"
				}
				putFeature(out, "context.schedule."+side+"."+key, count)
			}
		}
	}
	records, present := safeHistoryRecords(row, "headToHead")
	emitHistory(out, "safeH2H.10", historyStatistics(records, present, 10), true)
	emitHistory(out, "safeH2H.all", historyStatistics(records, present, 0), true)
	for _, key := range []string{"drawRate", "goalsFor", "goalsAgainst", "totalGoals", "goalDifference"} {
		a, aok := stats["home"][key]
		b, bok := stats["away"][key]
		if aok && bok {
			featurePair(out, "safeRecent.pair."+key, a, b)
		}
	}
	if a, aok := rests["home"]; aok {
		if b, bok := rests["away"]; bok {
			putFeature(out, "context.schedule.restDays.absGap", math.Abs(a-b))
		}
	}
	if stats["home"]["n"] >= 5 && stats["away"]["n"] >= 5 {
		h, a := stats["home"], stats["away"]
		hp, ap := (3*h["wins"]+h["draws"])/h["n"], (3*a["wins"]+a["draws"])/a["n"]
		putFeature(out, "context.form.home.pointsPerGame", hp)
		putFeature(out, "context.form.away.pointsPerGame", ap)
		featurePair(out, "context.form.pointsPerGame", hp, ap)
		hg, ag := (h["goalsFor"]+a["goalsAgainst"])/2, (a["goalsFor"]+h["goalsAgainst"])/2
		putFeature(out, "context.goals.home.attackDefenseIndex", hg)
		putFeature(out, "context.goals.away.attackDefenseIndex", ag)
		featurePair(out, "context.goals.attackDefenseIndex", hg, ag)
	}
}
