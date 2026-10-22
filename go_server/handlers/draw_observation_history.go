package handlers

import (
	"bytes"
	"encoding/json"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go_server/internal/drawmodel"
	"go_server/models"
)

// These values borrow the immutable document bytes only during parsing. Unlike
// json.RawMessage, they do not copy every field of every repeated fixture. No
// borrowed value is retained in the returned history or the observation cache.
type drawObservationHistoryRaw []byte

func (raw *drawObservationHistoryRaw) UnmarshalJSON(data []byte) error {
	*raw = data
	return nil
}

func drawObservationHistoryText(raw []byte) string {
	if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
		text := raw[1 : len(raw)-1]
		if bytes.IndexByte(text, '\\') < 0 && utf8.Valid(text) {
			return strings.TrimSpace(string(text))
		}
	}
	return drawRawString(raw)
}

func drawObservationHistoryID(raw []byte) string {
	value := drawObservationHistoryText(raw)
	if value == "" {
		return ""
	}
	if n, err := strconv.ParseInt(value, 10, 64); err == nil {
		if n <= 0 {
			return ""
		}
		return strconv.FormatInt(n, 10)
	}
	return value
}

type drawObservationHistoryRow struct {
	ID           drawObservationHistoryRaw `json:"matchId"`
	Kickoff      drawObservationHistoryRaw `json:"matchTime"`
	League       drawObservationHistoryRaw `json:"league"`
	LeagueID     drawObservationHistoryRaw `json:"leagueId"`
	HomeID       drawObservationHistoryRaw `json:"homeId"`
	AwayID       drawObservationHistoryRaw `json:"guestId"`
	Home         drawObservationHistoryRaw `json:"home"`
	Away         drawObservationHistoryRaw `json:"guest"`
	Goals        drawObservationHistoryRaw `json:"goal"`
	DisplayState drawObservationHistoryRaw `json:"displayState"`
	HomeOT       drawObservationHistoryRaw `json:"homeOtScore"`
	AwayOT       drawObservationHistoryRaw `json:"guestOtScore"`
	HomePenalty  drawObservationHistoryRaw `json:"homeOtPenalty"`
	AwayPenalty  drawObservationHistoryRaw `json:"guestOtPenalty"`
}

func (row drawObservationHistoryRow) allowed() bool {
	if len(row.DisplayState)+len(row.HomeOT)+len(row.AwayOT)+len(row.HomePenalty)+len(row.AwayPenalty) == 0 {
		return true
	}
	// Keep the shared status rules, including string/numeric zero handling.
	return drawHistoricalRowAllowed(map[string]json.RawMessage{
		"displayState": json.RawMessage(row.DisplayState), "homeOtScore": json.RawMessage(row.HomeOT),
		"guestOtScore": json.RawMessage(row.AwayOT), "homeOtPenalty": json.RawMessage(row.HomePenalty),
		"guestOtPenalty": json.RawMessage(row.AwayPenalty),
	})
}

func drawObservationHistoryArrays(history models.HistoryMoney) [3][]byte {
	var arrays [3][]byte
	present := false
	for i, raw := range [][]byte{history.RecentHomeList, history.RecentGuestList, history.AgainstList} {
		trimmed := bytes.TrimSpace(raw)
		if len(raw) > 0 && !bytes.Equal(trimmed, []byte("[]")) && !bytes.Equal(trimmed, []byte("null")) {
			arrays[i], present = raw, true
		}
	}
	if present {
		return arrays
	}
	// Preserve the existing all-split-columns-empty fallback and source order.
	var payload struct {
		Recent struct {
			Home struct {
				List drawObservationHistoryRaw `json:"list"`
			} `json:"home"`
			Guest struct {
				List drawObservationHistoryRaw `json:"list"`
			} `json:"guest"`
		} `json:"recent"`
		Against struct {
			List drawObservationHistoryRaw `json:"list"`
		} `json:"against"`
	}
	if json.Unmarshal(history.LeagueStat, &payload) == nil {
		arrays = [3][]byte{payload.Recent.Home.List, payload.Recent.Guest.List, payload.Against.List}
	}
	return arrays
}

type drawObservationDecodedHistory struct {
	rows [3][]drawObservationHistoryRow
}

func (decoded *drawObservationDecodedHistory) decode(history models.HistoryMoney) {
	for i, data := range drawObservationHistoryArrays(history) {
		rows := decoded.rows[i]
		clear(rows[:cap(rows)])
		rows = rows[:0]
		if json.Unmarshal(data, &rows) != nil {
			rows = rows[:0] // A malformed array must discard every partial row.
		}
		decoded.rows[i] = rows
	}
}

// Decode bounded batches concurrently, then consume strictly in document and
// source-array order. Registry validation/deduplication remains serial, so an
// invalid early copy never hides a later valid one and first-valid wins exactly
// as before. At most 32 decoded documents are retained, not the entire corpus.
func drawObservationEachDecodedHistory(histories []models.HistoryMoney, workers int, consume func(models.HistoryMoney, *drawObservationDecodedHistory)) {
	if workers <= 1 || len(histories) < 64 {
		var decoded drawObservationDecodedHistory
		for _, history := range histories {
			decoded.decode(history)
			consume(history, &decoded)
		}
		return
	}
	const batchSize = 32
	workers = min(workers, batchSize)
	var decoded [batchSize]drawObservationDecodedHistory
	type job struct{ slot, document int }
	jobs := make(chan job, batchSize)
	var batch, stopped sync.WaitGroup
	stopped.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer stopped.Done()
			for task := range jobs {
				decoded[task.slot].decode(histories[task.document])
				batch.Done()
			}
		}()
	}
	for start := 0; start < len(histories); start += batchSize {
		count := min(batchSize, len(histories)-start)
		batch.Add(count)
		for slot := 0; slot < count; slot++ {
			jobs <- job{slot: slot, document: start + slot}
		}
		batch.Wait()
		for slot := 0; slot < count; slot++ {
			consume(histories[start+slot], &decoded[slot])
		}
	}
	close(jobs)
	stopped.Wait()
}

func drawObservationParsedHistory(targets []drawObservationTarget, histories []models.HistoryMoney, now time.Time) []drawHistoricalGame {
	return drawObservationParsedHistoryWorkers(targets, histories, now, min(8, runtime.GOMAXPROCS(0)))
}

func drawObservationParsedHistoryWorkers(targets []drawObservationTarget, histories []models.HistoryMoney, now time.Time, workers int) []drawHistoricalGame {
	known := make(map[string]models.Money, len(targets))
	registry := make(map[string]drawHistoricalGame, len(targets))
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
	var goals []drawObservationHistoryRaw
	consume := func(history models.HistoryMoney, decoded *drawObservationDecodedHistory) {
		target := known[history.MatchId]
		for _, rows := range decoded.rows {
			for _, row := range rows {
				id := drawObservationHistoryID(row.ID)
				if id != "" {
					if _, exists := registry[id]; exists {
						continue
					}
				}
				if m, exists := known[id]; exists {
					if _, _, valid := drawRegulationResult(m); !valid || drawSourceTime(m.MatchTime).After(now) {
						continue
					}
				}
				if !row.allowed() {
					continue
				}
				goals = goals[:0]
				if json.Unmarshal(row.Goals, &goals) != nil || len(goals) < 2 {
					continue
				}
				h, errH := strconv.Atoi(drawObservationHistoryText(goals[0]))
				a, errA := strconv.Atoi(drawObservationHistoryText(goals[1]))
				if errH != nil || errA != nil || h < 0 || a < 0 || h > 30 || a > 30 {
					continue
				}
				kickoff, ok := drawHistoryTime(drawObservationHistoryText(row.Kickoff))
				if !ok || kickoff.After(now) {
					continue
				}
				leagueName := drawLeagueName(drawObservationHistoryText(row.League))
				leagueID := drawObservationHistoryID(row.LeagueID)
				if leagueID == "" && (leagueName == drawLeagueName(target.League) || leagueName == drawLeagueName(target.LeagueName)) {
					leagueID = drawID(target.LeagueId)
				}
				game := drawmodel.Match{ID: id, LeagueID: leagueID, LeagueName: leagueName, HomeTeamID: drawObservationHistoryID(row.HomeID), AwayTeamID: drawObservationHistoryID(row.AwayID), HomeTeamName: drawObservationHistoryText(row.Home), AwayTeamName: drawObservationHistoryText(row.Away), Kickoff: kickoff, CompletedAt: kickoff.Add(2 * time.Hour), HomeGoals: h, AwayGoals: a, Completed: true}
				key := drawObservationGameKey(game)
				if _, exists := registry[key]; !exists {
					registry[key] = drawHistoricalGame{Game: game, Sources: []string{"history_moneys"}}
				}
			}
		}
	}
	drawObservationEachDecodedHistory(histories, workers, consume)
	pool := make([]drawHistoricalGame, 0, len(registry))
	for _, game := range registry {
		pool = append(pool, game)
	}
	return pool
}
