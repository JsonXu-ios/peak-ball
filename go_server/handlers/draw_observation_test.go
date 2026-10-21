package handlers

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/internal/drawmodel"
	"go_server/models"
	"gorm.io/datatypes"
)

func drawObservationGame(id, home, away string, kickoff time.Time, hg, ag int) drawHistoricalGame {
	return drawHistoricalGame{Game: drawmodel.Match{ID: id, HomeTeamID: home, AwayTeamID: away, HomeTeamName: home, AwayTeamName: away, LeagueName: "League", Kickoff: kickoff, CompletedAt: kickoff.Add(2 * time.Hour), Completed: true, HomeGoals: hg, AwayGoals: ag}, Sources: []string{"history_moneys"}}
}

func drawObservationMain(game drawHistoricalGame) drawObservationTarget {
	g := game.Game
	home, _ := strconv.Atoi(g.HomeTeamID)
	away, _ := strconv.Atoi(g.AwayTeamID)
	date := g.Kickoff.In(drawSourceLocation)
	state := "未开赛"
	if g.Completed {
		state = "完场"
	}
	return drawObservationTarget{Match: models.Money{MatchId: g.ID, Date: time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location()), MatchTime: date, League: g.LeagueName, Home: g.HomeTeamName, Guest: g.AwayTeamName, HomeTeamId: home, GuestTeamId: away, HomeScore: g.HomeGoals, GuestScore: g.AwayGoals, DisplayState: state}, Sources: []string{"moneys"}}
}

func TestDrawObservationsRecentUsesOnlyPreMatchCompletedGames(t *testing.T) {
	kickoff := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	target := drawObservationGame("target", "H", "A", kickoff, 1, 1)
	games := []drawHistoricalGame{target}
	for i := 0; i < 9; i++ {
		prior := kickoff.Add(-time.Duration(i+1) * 24 * time.Hour)
		h := drawObservationGame(fmt.Sprintf("h-%d", i), "H", "X", prior, i%3, 0)
		a := drawObservationGame(fmt.Sprintf("a-%d", i), "Y", "A", prior, 2, 0)
		if i%2 != 0 {
			a = drawObservationGame(fmt.Sprintf("a-%d", i), "A", "Y", prior, 1, 0)
		}
		games = append(games, h, a)
	}
	// Neither self, simultaneous, later, incomplete nor still-playing scores
	// may inflate the previous nine observations into a fabricated ten.
	games = append(games,
		drawObservationGame("same-time", "H", "A", kickoff, 0, 0),
		drawObservationGame("future", "H", "A", kickoff.Add(24*time.Hour), 0, 0),
		drawObservationGame("still-playing", "H", "A", kickoff.Add(-time.Hour), 0, 0),
	)
	incomplete := drawObservationGame("incomplete", "H", "A", kickoff.Add(-48*time.Hour), 0, 0)
	incomplete.Game.Completed = false
	games = append(games, incomplete)
	response := buildDrawObservations(drawObservationData{Targets: []drawObservationTarget{drawObservationMain(target)}, History: games}, drawObservationQuery{mode: "history", page: 1, size: 50, search: "target"})
	if response.Total != 1 || len(response.Items) != 1 {
		t.Fatalf("expected target only: %+v", response)
	}
	row := response.Items[0]
	if response.Summary.HistoricalMatches != 1 || response.Summary.DrawMatches != 1 {
		t.Fatal("embedded team-history fixtures became observation targets or inflated totals")
	}
	if row.HomeRecent.Sample != 9 || row.HomeRecent.Draws != 3 || row.AwayRecent.Sample != 9 || row.AwayRecent.Draws != 0 {
		t.Fatalf("incorrect pre-match windows: home=%+v away=%+v", row.HomeRecent, row.AwayRecent)
	}
	if row.HomeRecent.Matches[0].MatchID != "h-0" || row.HomeRecent.Matches[8].MatchID != "h-8" || row.AwayRecent.Matches[0].Venue != "away" || row.AwayRecent.Matches[0].Result != "loss" || row.AwayRecent.Matches[1].Venue != "home" || row.AwayRecent.Matches[1].Result != "win" {
		t.Fatalf("ordering or team-relative results are wrong: %+v", row)
	}
	// A repeated copy of the same result must not count twice.
	idx := indexDrawObservations(append(games, games[1]))
	recent, _ := idx.recent(target.Game, "H", "H")
	if recent.Sample != 9 || recent.Draws != 3 {
		t.Fatalf("duplicate result changed the recent window: %+v", recent)
	}
}

func TestDrawObservationsTeamIdentityPrefersIDAndRejectsAmbiguousName(t *testing.T) {
	kickoff := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	target := drawObservationGame("target", "1", "A", kickoff, 1, 1)
	target.Game.HomeTeamName = "United"
	good := drawObservationGame("known-id", "1", "X", kickoff.Add(-24*time.Hour), 0, 0)
	good.Game.HomeTeamName = "Different Display Name"
	other := drawObservationGame("different-id", "2", "X", kickoff.Add(-48*time.Hour), 0, 0)
	other.Game.HomeTeamName = "United"
	missing := drawObservationGame("missing-id", "", "X", kickoff.Add(-72*time.Hour), 0, 0)
	missing.Game.HomeTeamName = " united "
	idx := indexDrawObservations([]drawHistoricalGame{target, good, other, missing})
	recent, warnings := idx.recent(target.Game, "1", "United")
	if recent.Sample != 1 || recent.Matches[0].MatchID != "known-id" || len(warnings) == 0 {
		t.Fatalf("different known IDs or ambiguous names were merged: %+v warnings=%v", recent, warnings)
	}
	// With no conflicting known ID, a genuinely missing ID may fall back to
	// normalized exact names, while direct ID matching still accepts aliases.
	idx = indexDrawObservations([]drawHistoricalGame{target, good, missing})
	recent, warnings = idx.recent(target.Game, "1", "United")
	if recent.Sample != 2 || len(warnings) == 0 {
		t.Fatalf("unambiguous missing-ID fallback was lost: %+v warnings=%v", recent, warnings)
	}
	// A missing target ID can resolve through one unambiguous name-to-ID
	// mapping, then include that ID's older display-name variants as well.
	recent, warnings = idx.recent(target.Game, "", "United")
	if recent.Sample != 2 || recent.Matches[0].MatchID != "known-id" || len(warnings) == 0 {
		t.Fatalf("resolved ID lost older aliases: %+v warnings=%v", recent, warnings)
	}
}

func TestDrawObservationsPankouPreservesZeroAndSingleCompany(t *testing.T) {
	pankou := models.PankouMoney{
		Bet365Asia: datatypes.JSON(`{"companyName":"Bet365"}`),
		AsiaData:   datatypes.JSON(`[{"companyId":3,"companyName":"Other","firstPankou":"半球","pankou":"一球","firstOdds":["9.9","9.8"]},{"companyId":8,"companyName":"Bet365","firstPankou":0,"pankou":"0/0.5","firstOdds":[0,"0.90"],"odds":["0.82"]}]`),
		DxqData:    datatypes.JSON(`[{"companyId":3,"companyName":"Other","firstPankou":"3","pankou":"3.5"},{"companyId":8,"companyName":"Bet365","firstPankou":"2/2.5","pankou":""}]`),
	}
	asia, totals := drawObservationLines(pankou)
	if asia.Opening == nil || *asia.Opening != "0" || asia.Latest == nil || *asia.Latest != "0/0.5" || asia.Company != "Bet365" || asia.Source != "pankou_moneys" {
		t.Fatalf("raw zero or preferred single company lost: %+v", asia)
	}
	if totals.Opening == nil || *totals.Opening != "2/2.5" || totals.Latest != nil || totals.Company != "Bet365" {
		t.Fatalf("a missing latest line was filled from another company: %+v", totals)
	}
	if len(asia.OpeningOdds) != 2 || *asia.OpeningOdds[0] != "0" || *asia.OpeningOdds[1] != "0.90" || len(asia.LatestOdds) != 2 || *asia.LatestOdds[0] != "0.82" || asia.LatestOdds[1] != nil {
		t.Fatalf("water values were normalized or mixed across companies: %+v", asia)
	}
	// Asian-only arrays cannot accidentally become totals, and nested letGoal
	// values are not an Asian-market fallback.
	pankou.DxqData = nil
	_, totals = drawObservationLines(pankou)
	if totals.Source != "missing" || totals.Opening != nil || totals.Latest != nil {
		t.Fatalf("Asian rows leaked into the totals market: %+v", totals)
	}
	embedded := models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyName":"One","pankou":"平手"}],"dxq":[{"companyName":"One","pankou":"2.5"}]}`)}
	asia, totals = drawObservationLines(embedded)
	if asia.Latest == nil || *asia.Latest != "平手" || totals.Latest == nil || *totals.Latest != "2.5" {
		t.Fatalf("old combined payload was not read: asia=%+v totals=%+v", asia, totals)
	}
}

func TestDrawObservationsFiltersDoNotChangeSummaryOrHideEmptyHistory(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	base := time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC) // Source calendar date is October 20.
	first := drawObservationGame("first", "H", "A", base, 0, 0)
	second := drawObservationGame("second", "B", "C", base.Add(-24*time.Hour), 1, 1)
	third := drawObservationGame("third", "D", "E", base.Add(-48*time.Hour), 2, 2)
	nonDraw := drawObservationGame("other", "F", "G", base.Add(-72*time.Hour), 1, 0)
	first.Handicap = drawFloat(-1) // Unclassified nested letGoal: never expose as Asian data.
	second.Game.LeagueName = "Cup"
	data := drawObservationData{Targets: []drawObservationTarget{drawObservationMain(third), drawObservationMain(first), drawObservationMain(nonDraw), drawObservationMain(second)}, Pankou: map[string]models.PankouMoney{
		"second": {Bet365Asia: datatypes.JSON(`{"firstPankou":"0","pankou":"0.5"}`), Bet365Dxq: datatypes.JSON(`{"pankou":"2.5"}`)},
	}}
	all := buildDrawObservations(data, drawObservationQuery{mode: "history", page: 2, size: 1})
	if all.Total != 3 || len(all.Items) != 1 || all.Items[0].MatchID != "second" || all.Summary.HistoricalMatches != 4 || all.Summary.DrawMatches != 3 || all.Summary.AsianMatches != 1 || all.Summary.TotalMatches != 1 {
		t.Fatalf("wrong draw-only ordering/pagination/global summary: %+v", all)
	}
	filtered := buildDrawObservations(data, drawObservationQuery{mode: "history", start: "2026-10-20", end: "2026-10-20", search: "H", league: "League", page: 1, size: 50})
	if filtered.Total != 1 || filtered.Items[0].MatchID != "first" || !reflect.DeepEqual(filtered.Summary, all.Summary) || filtered.Items[0].Date != "2026-10-20" || filtered.Items[0].HomeRecent.Sample != 0 || filtered.Items[0].HomeRecent.Matches == nil {
		t.Fatalf("filters changed the source summary or discarded empty histories: %+v", filtered)
	}
	if filtered.Items[0].AsianHandicap.Source != "missing" || filtered.Items[0].AsianHandicap.Historical != nil {
		t.Fatal("ambiguous nested letGoal was mislabeled as an Asian line")
	}
	empty := buildDrawObservations(data, drawObservationQuery{mode: "history", page: int(^uint(0) >> 1), size: 200})
	if empty.Items == nil || len(empty.Items) != 0 || empty.Total != 3 {
		t.Fatalf("large out-of-range page overflowed or hid total: %+v", empty)
	}
	encoded, err := json.Marshal(filtered)
	if err != nil || strings.Contains(string(encoded), "probability") || !strings.Contains(string(encoded), `"opening":null`) {
		t.Fatalf("observation contract must preserve missing data and contain no model result: %s error=%v", encoded, err)
	}
}

func TestDrawObservationsRejectInvalidQueryBeforeDatabaseRead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, path := range []string{"/api/draw/observations?start_date=2026-02-30", "/api/draw/observations?start_date=2026-10-21&end_date=2026-10-20", "/api/draw/observations?date=2026-02-30", "/api/draw/observations?mode=unknown"} {
		writer := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(writer)
		context.Request = httptest.NewRequest("GET", path, nil)
		GetDrawObservations(context)
		if writer.Code != 400 {
			t.Fatalf("invalid date query reached the loader: status=%d body=%s", writer.Code, writer.Body)
		}
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/api/draw/observations?page=-1&page_size=999", nil)
	query, err := parseDrawObservationQuery(context)
	if err != nil || query.page != 1 || query.size != 200 || query.mode != "today" || query.date != time.Now().UTC().Format("2006-01-02") {
		t.Fatalf("pagination normalization failed: %+v error=%v", query, err)
	}
}

func TestDrawObservationsMainTablesDefineTargetsAndTodayIncludesUnsettled(t *testing.T) {
	drawTestSourceZone(t, "Asia/Shanghai")
	kickoff := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	legacyDraw := drawObservationMain(drawObservationGame("legacy-draw", "1", "2", kickoff, 1, 1)).Match
	oldDuplicate := drawObservationMain(drawObservationGame("duplicate", "3", "4", kickoff, 0, 0)).Match
	newDuplicate := oldDuplicate
	newDuplicate.HomeScore, newDuplicate.GuestScore = 2, 0
	pending := drawObservationMain(drawObservationGame("scheduled", "5", "6", kickoff.Add(12*time.Hour), 0, 0)).Match
	pending.DisplayState = "未开赛"
	// Its source schedule date remains October 21 even if local kick-off is
	// the following morning. Date, not a UTC-day approximation, selects it.
	pending.Date = legacyDraw.Date
	shell := pending
	shell.MatchId, shell.DisplayState = "collection-shell", detailOnlyDisplayState
	targets := mergeDrawObservationTargets([]models.Money{newDuplicate, pending, shell}, []models.Money{legacyDraw, oldDuplicate})
	data := drawObservationData{Targets: targets, History: []drawHistoricalGame{drawObservationGame("embedded-only-draw", "1", "2", kickoff.Add(-24*time.Hour), 0, 0)}}
	today := buildDrawObservations(data, drawObservationQuery{mode: "today", date: "2026-10-21", page: 1, size: 50})
	if today.Total != 3 || today.Summary.HistoricalMatches != 3 || today.Summary.DrawMatches != 1 {
		t.Fatalf("target scope or current-table precedence is wrong: %+v", today)
	}
	if today.Items[0].MatchID != "scheduled" || today.Items[0].Settled || today.Items[0].DisplayState != "未开赛" || today.Items[0].Date != "2026-10-21" {
		t.Fatalf("unfinished schedule was hidden or marked as a draw: %+v", today.Items[0])
	}
	history := buildDrawObservations(data, drawObservationQuery{mode: "history", page: 1, size: 50})
	if history.Total != 1 || history.Items[0].MatchID != "legacy-draw" || !history.Items[0].Settled || history.Items[0].HomeRecent.Sample != 1 {
		t.Fatalf("embedded history became a target or stopped being a recent input: %+v", history)
	}
	for _, target := range targets {
		if target.Match.MatchId == "duplicate" && (target.Match.HomeScore != 2 || len(target.Sources) != 2) {
			t.Fatalf("money/moneys duplicate was not resolved in favor of current data: %+v", target)
		}
	}
}

func TestDrawObservationsEuroUsesSingleRawCompanyAndPreservesMissing(t *testing.T) {
	odds := models.OddsMoney{Data: datatypes.JSON(`{"odds":[{"companyId":1,"companyName":"平均","firstOdds":["9","9","9"]},{"companyId":2,"companyName":"Other","firstOdds":["1.1","2.2","3.3"],"odds":["4.4","5.5","6.6"]},{"companyId":8,"companyName":"Bet365","firstOdds":["2.10",3.4,"0"],"odds":[0,"3.25"]}]}`)}
	quote := drawObservationEuro(odds)
	if quote.Company != "Bet365" || quote.Source != "odds_moneys" || len(quote.Opening) != 3 || *quote.Opening[0] != "2.10" || *quote.Opening[1] != "3.4" || *quote.Opening[2] != "0" || len(quote.Latest) != 3 || *quote.Latest[0] != "0" || quote.Latest[2] != nil {
		t.Fatalf("three-way quote was normalized or mixed between companies: %+v", quote)
	}
	empty := drawObservationEuro(models.OddsMoney{})
	if empty.Source != "missing" || len(empty.Opening) != 3 || len(empty.Latest) != 3 || empty.Opening[0] != nil || empty.Latest[2] != nil {
		t.Fatalf("missing odds were fabricated or lost their shape: %+v", empty)
	}
}

func TestDrawObservationsNewCombinedMarketsOverrideStaleDedicatedColumns(t *testing.T) {
	captured := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
	pankou := models.PankouMoney{
		CapturedAt: &captured,
		AsiaData:   datatypes.JSON(`{"asia":[{"companyId":8,"companyName":"Bet365","firstPankou":"0","pankou":"0/0.5","firstOdds":["0.80","0.90"],"odds":["0.83","0.95"]}],"dxq":[{"companyId":8,"companyName":"Bet365","firstPankou":"2.5","pankou":"2.75","firstOdds":["0.91","0.92"],"odds":["0.93","0.94"]}]}`),
		Bet365Asia: datatypes.JSON(`{"companyName":"Bet365","pankou":"旧亚盘","odds":["9","9"]}`),
		Bet365Dxq:  datatypes.JSON(`{"companyName":"Bet365","pankou":"旧大小球"}`),
		DxqData:    datatypes.JSON(`[{"companyName":"Bet365","pankou":"旧独立大小球"}]`),
	}
	asia, total := drawObservationLines(pankou)
	if asia.Latest == nil || *asia.Latest != "0/0.5" || total.Latest == nil || *total.Latest != "2.75" || *asia.LatestOdds[0] != "0.83" || *total.LatestOdds[1] != "0.94" || asia.CapturedAt == nil || !asia.CapturedAt.Equal(captured) {
		t.Fatalf("stale dedicated columns replaced the new raw payload: asia=%+v total=%+v", asia, total)
	}
	pankou.AsiaData = datatypes.JSON(`{"asia":[],"dxq":[]}`)
	asia, total = drawObservationLines(pankou)
	if asia.Source != "missing" || total.Source != "missing" {
		t.Fatal("explicitly empty newly captured markets were filled with stale columns")
	}
	odds := models.OddsMoney{CapturedAt: &captured, Data: datatypes.JSON(`{"odds":[{"companyId":8,"companyName":"Bet365","firstOdds":["2","3","4"],"odds":["2.1","3.1","4.1"]}]}`), Bet365: datatypes.JSON(`{"companyName":"Bet365","odds":["9","9","9"]}`)}
	euro := drawObservationEuro(odds)
	if euro.Latest[0] == nil || *euro.Latest[0] != "2.1" || euro.CapturedAt == nil || !euro.CapturedAt.Equal(captured) {
		t.Fatalf("stale euro column replaced the new source payload: %+v", euro)
	}
	odds.Data = datatypes.JSON(`{"odds":[]}`)
	if drawObservationEuro(odds).Source != "missing" {
		t.Fatal("explicitly empty three-way payload was filled from stale preferred odds")
	}
}
