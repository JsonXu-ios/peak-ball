package handlers

import (
	"database/sql"
	"errors"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go_server/models"
	"gorm.io/datatypes"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func drawObservationDisplayFixture() (models.Money, models.HistoryMoney, models.OddsMoney, models.PankouMoney) {
	match := drawObservationMain(drawObservationGame("display", "101", "202", time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC), 1, 1)).Match
	match.Home, match.Guest, match.HomeRank, match.GuestRank = "Home", "Away", "3", "7"
	history := models.HistoryMoney{
		AgainstSummary:  datatypes.JSON(`{"win":2,"draw":1,"lose":1,"all":4,"winGoal":6,"loseGoal":4}`),
		AgainstList:     datatypes.JSON(`[{"matchTime":"2026-09-01","home":"Home","guest":"Away","goal":[1,1]}]`),
		RecentHomeList:  datatypes.JSON(`[{"matchTime":"2026-10-01","home":"Home","guest":"Other","goal":[2,0]}]`),
		RecentGuestList: datatypes.JSON(`[{"matchTime":"2026-10-01","home":"Other","guest":"Away","goal":[1,1]}]`),
	}
	odds := models.OddsMoney{
		Data:           datatypes.JSON(`{"odds":[{"companyId":"281","companyName":"Bet3**","firstOdds":["2.00",0,"3.00"],"odds":["2.1","3.2","3.3"]},{"companyId":"115","companyName":"威廉**","firstOdds":["1.9","3.1","3.2"],"odds":["2.0","3.0","3.4"]}]}`),
		AvgOdds:        datatypes.JSON(`{"odds":["2.0","3.1","3.5"]}`),
		SportteryTrade: datatypes.JSON(`{"data":{"tzbl":{"h":2.00,"d":3.00,"a":3.30,"hsupportRate":40,"dsupportRate":30,"asupportRate":30},"jyykSpf":{"h":2.0,"d":3.0,"a":3.3,"hsupportRate":40,"dsupportRate":30,"asupportRate":30},"jyykRqspf":{"goal":"-1","h":3.5,"d":3.4,"a":1.8,"hsupportRate":25,"dsupportRate":25,"asupportRate":50}}}`),
	}
	pankou := models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":8,"companyName":"Bet3**","firstPankou":0,"pankou":"0/0.5","firstOdds":[0,"0.90"],"odds":["0.82","0.98"]},{"companyId":9,"companyName":"威廉**","firstPankou":"-0.5","pankou":"0","firstOdds":["0.91","0.92"],"odds":["0.81","0.82"]}],"dxq":[{"companyId":8,"companyName":"Bet3**","firstPankou":"2.5","pankou":"2.5/3","firstOdds":["0.80","1.00"],"odds":["0.85","0.95"]},{"companyId":9,"companyName":"威廉**","firstPankou":"2","pankou":"2.5","firstOdds":["0.89","0.99"],"odds":["0.90","0.98"]}]}`)}
	return match, history, odds, pankou
}

func TestDrawObservationsFrontendCopiesExistingCalculationAndKeepsRawSeparate(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	beforeOdds, beforePankou := string(odds.Data), string(pankou.AsiaData)
	response := buildAnalysisFromData(match, history, odds, pankou, sportteryTradeFromJSON(odds.SportteryTrade))
	attachAnalysisWeights(&response)
	response.Platform = buildPlatformDecision(&response)
	got := buildDrawObservationFrontend(match, history, odds, pankou)
	want := drawObservationFrontendValues(&response)
	want.Basis = got.analysis.Basis // Provenance supplements, without changing shared values.
	want.PredictionAvailability = got.analysis.PredictionAvailability
	want.ComparisonStages = got.analysis.ComparisonStages
	want.WeightedFeatureInputs = got.analysis.WeightedFeatureInputs // Strict source-presence supplements.
	if !reflect.DeepEqual(got.analysis, want) || !reflect.DeepEqual(got.analysis.LocalMarket, response.Platform.LocalMarket) {
		t.Fatalf("frontend display changed the shared calculation: got=%+v want=%+v", got.analysis, want)
	}
	if got.analysis.YapanPankou1 != response.YapanPankou1 || !reflect.DeepEqual(got.analysis.NewYaPanTouZhu, response.NewYaPanTouZhu) || got.analysis.WinProbability+got.analysis.DrawProbability+got.analysis.LoseProbability < 99 {
		t.Fatal("frontend fields were renamed, omitted or recomputed")
	}
	if string(odds.Data) != beforeOdds || string(pankou.AsiaData) != beforePankou || !reflect.DeepEqual(got.euro, drawObservationEuro(odds)) {
		t.Fatal("enrichment mutated the inputs or the original single-company quote")
	}
	// The existing analysis may fall back to numeric goal estimates; the raw
	// company quote must remain missing rather than presenting that as a quote.
	missing := buildDrawObservationFrontend(match, history, models.OddsMoney{}, models.PankouMoney{})
	if missing.analysis.QiushuPankou2 <= 0 || missing.companies[2].GoalsLine.Source != "missing" || missing.companies[0].EuroOdds.Source != "missing" {
		t.Fatal("frontend fallback/simulated values leaked into raw company quotes")
	}
	if missing.analysis.RoiSimulation == nil {
		t.Fatal("the shared frontend fallback was suppressed")
	}
}

func TestDrawObservationsAverageOddsExposeRealInputsWithoutGenericProbabilities(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	got := buildDrawObservationFrontend(match, history, odds, pankou).analysis
	if !reflect.DeepEqual(got.AverageEuroOdds, []float64{2, 3.1, 3.5}) {
		t.Fatalf("stored average not exposed: %v", got.AverageEuroOdds)
	}
	// No source average: share the existing mean of ALL companies, not only William.
	odds.AvgOdds = nil
	got = buildDrawObservationFrontend(match, history, odds, pankou).analysis
	if len(got.AverageEuroOdds) != 3 {
		t.Fatalf("company mean missing: %v", got.AverageEuroOdds)
	}
	for i, want := range []float64{2.05, 3.1, 3.35} {
		if math.Abs(got.AverageEuroOdds[i]-want) > 1e-12 {
			t.Fatalf("company mean differs from the real inputs: %v", got.AverageEuroOdds)
		}
	}
	for _, input := range []models.OddsMoney{{}, {AvgOdds: datatypes.JSON(`{"odds":["2","0","3"]}`)}} {
		missing := buildDrawObservationFrontend(match, history, input, pankou).analysis
		if missing.AverageEuroOdds != nil {
			t.Fatalf("invalid/missing average became odds: %v", missing.AverageEuroOdds)
		}
	}
}

func TestDrawObservationsAnalysisBasisReportsActualCompanyAndFallback(t *testing.T) {
	_, _, odds, pankou := drawObservationDisplayFixture()
	basis := drawObservationBasis(odds, pankou)
	if !strings.Contains(basis.Asian, "Bet365") || !strings.Contains(basis.Goals, "Bet365") || !strings.Contains(basis.Kelly, "Bet365") || !strings.Contains(basis.Ticai, "威廉与竞彩") {
		t.Fatalf("normal calculation source missing: %+v", basis)
	}
	// Existing shared logic falls back to the first company if Bet365 is absent.
	// The display must identify that actual company rather than label it Bet365.
	pankou = models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":9,"companyName":"***","firstPankou":"0","pankou":"0.5"}],"dxq":[]}`)}
	odds.Data = datatypes.JSON(`{"odds":[{"companyId":"16","companyName":"10B**","odds":["2.31","3.80","2.95"]},{"companyId":"115","companyName":"***","odds":["2.25","3.50","2.88"]}]}`)
	odds.SportteryTrade = nil
	basis = drawObservationBasis(odds, pankou)
	if !strings.Contains(basis.Asian, "威廉") || !strings.Contains(basis.Kelly, "10B**（欧赔 ID 16）") || !strings.Contains(basis.Goals, "回退") || !strings.Contains(basis.Ticai, "缺少竞彩") {
		t.Fatalf("fallback was relabeled as the preferred company: %+v", basis)
	}
	basis = drawObservationBasis(models.OddsMoney{}, models.PankouMoney{})
	if !strings.Contains(basis.Kelly, "缺少完整") || !strings.Contains(basis.Ticai, "缺少竞彩、威廉") || !strings.Contains(basis.Asian, "缺少盘口") {
		t.Fatalf("missing inputs falsely claim actual company prices: %+v", basis)
	}
}

func TestDrawObservationsCompanyQuotesKeepStrictCompaniesRawZeroAndMarkets(t *testing.T) {
	_, _, odds, pankou := drawObservationDisplayFixture()
	captured := time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)
	odds.CapturedAt, pankou.CapturedAt = &captured, &captured
	rows := drawObservationCompanies(odds, pankou)
	if len(rows) != 3 || rows[0].Key != "sporttery" || rows[1].Key != "william" || rows[2].Key != "bet365" {
		t.Fatalf("company rows were omitted or reordered: %+v", rows)
	}
	bet, william, sporttery := rows[2], rows[1], rows[0]
	if bet.EuroOdds.Source != "odds_moneys" || *bet.EuroOdds.Opening[0] != "2.00" || *bet.EuroOdds.Opening[1] != "0" || *bet.AsianHandicap.Opening != "0" || *bet.AsianHandicap.OpeningOdds[0] != "0" || *bet.GoalsLine.Latest != "2.5/3" {
		t.Fatalf("Bet365 raw precision, zero or company selection changed: %+v", bet)
	}
	if *william.EuroOdds.Latest[2] != "3.4" || *william.AsianHandicap.Opening != "-0.5" || *william.AsianHandicap.Latest != "0" || *william.GoalsLine.Latest != "2.5" || !william.EuroOdds.CapturedAt.Equal(captured) || !william.GoalsLine.CapturedAt.Equal(captured) {
		t.Fatalf("William borrowed another market/company or lost timestamps: %+v", william)
	}
	if sporttery.EuroOdds.Opening[0] != nil || *sporttery.EuroOdds.Latest[0] != "2.00" || *sporttery.EuroOdds.Latest[2] != "3.30" || sporttery.AsianHandicap.Source != "missing" || sporttery.GoalsLine.Source != "missing" || sporttery.AsianHandicap.Historical != nil {
		t.Fatalf("sporttery opening/Asian/totals quotes were fabricated from integer handicap: %+v", sporttery)
	}
}

func TestDrawObservationsCompanyQuotesPreferCanonicalIDsAndRejectCrossMarketIDs(t *testing.T) {
	odds := models.OddsMoney{Data: datatypes.JSON(`{"odds":[{"companyId":"281","companyName":"William","odds":[1,2,3]},{"companyId":"115","companyName":"Bet365","odds":[4,5,6]}]}`)}
	pankou := models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":281,"firstPankou":"99"},{"companyId":115,"firstPankou":"98"},{"companyId":8,"companyName":"William","pankou":"97"},{"companyId":9,"companyName":"Bet365","pankou":"96"}]}`)}
	companies := drawObservationCompanies(odds, pankou)
	if companies[0].EuroOdds.Source != "missing" || companies[0].AsianHandicap.Source != "missing" {
		t.Fatal("another company's canonical provider ID became a sporttery quote")
	}
	if *companies[2].EuroOdds.Latest[0] != "1" || *companies[2].AsianHandicap.Latest != "97" || companies[2].EuroOdds.Company != "Bet365" || companies[2].AsianHandicap.Company != "Bet365" {
		t.Fatalf("correct Bet365 provider IDs were hidden by stale names: %+v", companies[2])
	}
	if *companies[1].EuroOdds.Latest[0] != "4" || *companies[1].AsianHandicap.Latest != "96" || companies[1].EuroOdds.Company != "威廉" || companies[1].AsianHandicap.Company != "威廉" {
		t.Fatalf("correct William provider IDs were assigned using another company's name: %+v", companies[1])
	}
	for _, fixture := range []struct{ id, name, key, market string }{
		{"281", "Bet365", "bet365", "pankou"}, {"115", "威廉", "william", "pankou"},
		{"8", "Bet365", "bet365", "euro"}, {"9", "威廉", "william", "euro"},
	} {
		if drawObservationCompanyMatches(fixture.id, fixture.name, fixture.key, fixture.market, false) {
			t.Fatalf("cross-market provider ID was rescued by a display name: %+v", fixture)
		}
	}
	// A missing William row cannot fall back to the first (Bet365) company.
	pankou.AsiaData = datatypes.JSON(`{"asia":[{"companyId":8,"companyName":"Bet3**","pankou":"0"}],"dxq":[{"companyId":99,"companyName":"William Hill","pankou":"2.5"}]}`)
	rows := drawObservationCompanies(models.OddsMoney{}, pankou)
	if rows[1].AsianHandicap.Source != "missing" || rows[1].GoalsLine.Source != "pankou_moneys" || rows[2].AsianHandicap.Source != "pankou_moneys" || rows[2].GoalsLine.Source != "missing" {
		t.Fatalf("strict identity/name fallback mixed companies or markets: %+v", rows)
	}
}

func TestDrawObservationsCompanyCanonicalIDsOverrideEveryNameWithoutDoubleAssignment(t *testing.T) {
	for _, fixture := range []struct{ id, key, market string }{
		{"281", "bet365", "euro"}, {"115", "william", "euro"},
		{"8", "bet365", "pankou"}, {"9", "william", "pankou"},
	} {
		for _, name := range []string{"", "***", "旧名称", "Bet3**", "William", "竞彩"} {
			for _, key := range []string{"bet365", "william", "sporttery"} {
				got := drawObservationCompanyMatches(fixture.id, name, key, fixture.market, false)
				if got != (key == fixture.key) {
					t.Fatalf("provider ID lost authority or matched twice: fixture=%+v sourceName=%q key=%s got=%t", fixture, name, key, got)
				}
			}
		}
	}
	if !drawObservationCompanyMatches("999", "William Hill", "william", "pankou", false) || drawObservationCompanyMatches("999", "旧名称", "william", "pankou", false) {
		t.Fatal("unknown provider ID no longer requires an explicit matching company name")
	}
	// This is the previously failing source shape: authoritative ID plus an
	// unrecognized/masked name. Keep original zero and decimal texts intact.
	odds := models.OddsMoney{Data: datatypes.JSON(`{"odds":[{"companyId":"281","companyName":"旧名称","firstOdds":["2.00",0,"3.20"]},{"companyId":"115","companyName":"***","odds":["2.1","3.1","3.4"]}]}`)}
	pankou := models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[{"companyId":8,"companyName":"***","firstPankou":0,"firstOdds":[0,"0.90"]}],"dxq":[{"companyId":9,"companyName":"旧名称","pankou":"2.5/3","odds":["0.88","1.00"]}]}`)}
	rows := drawObservationCompanies(odds, pankou)
	if *rows[2].EuroOdds.Opening[0] != "2.00" || *rows[2].EuroOdds.Opening[1] != "0" || *rows[2].AsianHandicap.Opening != "0" || *rows[2].AsianHandicap.OpeningOdds[0] != "0" || *rows[1].GoalsLine.Latest != "2.5/3" || rows[1].GoalsLine.Company != "威廉" {
		t.Fatalf("masked/unknown names hid valid canonical provider rows or normalized source values: %+v", rows)
	}
}

func TestDrawObservationsCompanySportteryRawZeroStaysMissingAndSeparateFromSimulation(t *testing.T) {
	match, history, odds, pankou := drawObservationDisplayFixture()
	odds.SportteryTrade = datatypes.JSON(`{"data":{"tzbl":{"h":0,"d":0.00,"a":0}}}`)
	value := buildDrawObservationFrontend(match, history, odds, pankou)
	quote := value.companies[0].EuroOdds
	if quote.Source != "missing" || quote.Opening[0] != nil || *quote.Latest[0] != "0" || *quote.Latest[1] != "0.00" || *quote.Latest[2] != "0" {
		t.Fatalf("captured zero prices were dropped or falsely marked available: %+v", quote)
	}
	simulated := false
	for _, market := range value.analysis.RoiSimulation.Markets {
		if market.Key == "sporttery" && market.Simulated && market.OddsAvailable {
			simulated = true
		}
	}
	if !simulated {
		t.Fatal("the frontend simulation was changed instead of remaining separate from raw prices")
	}
}

func TestDrawObservationsCompanyQuotesCombinedEmptyBeatsStaleAndDoesNotMergeRows(t *testing.T) {
	odds := models.OddsMoney{Data: datatypes.JSON(`{"odds":[]}`), Bet365: datatypes.JSON(`{"odds":[9,9,9]}`), William: datatypes.JSON(`{"odds":[8,8,8]}`)}
	pankou := models.PankouMoney{AsiaData: datatypes.JSON(`{"asia":[],"dxq":[]}`), DxqData: datatypes.JSON(`[{"companyId":9,"pankou":"9"}]`), Bet365Asia: datatypes.JSON(`{"pankou":"8"}`), Bet365Dxq: datatypes.JSON(`{"pankou":"7"}`)}
	for _, row := range drawObservationCompanies(odds, pankou) {
		if row.EuroOdds.Source != "missing" || row.AsianHandicap.Source != "missing" || row.GoalsLine.Source != "missing" {
			t.Fatalf("explicit empty fresh payload revived stale dedicated values: %+v", row)
		}
	}
	pankou.AsiaData = datatypes.JSON(`[{"companyId":8,"firstPankou":"0","firstOdds":["0.9","1.0"]},{"companyId":8,"pankou":"1","odds":["0.8","1.1"]}]`)
	bet := drawObservationCompanies(models.OddsMoney{}, pankou)[2]
	if bet.AsianHandicap.Opening == nil || bet.AsianHandicap.Latest != nil || bet.AsianHandicap.LatestOdds[0] != nil {
		t.Fatalf("separate source rows were combined into an invented quote: %+v", bet)
	}
	// The dedicated slot provides provenance only if its own metadata agrees.
	pankou = models.PankouMoney{Bet365Asia: datatypes.JSON(`{"companyId":9,"companyName":"William","pankou":"3"}`)}
	if drawObservationCompanies(models.OddsMoney{}, pankou)[2].AsianHandicap.Source != "missing" {
		t.Fatal("a wrong-company dedicated slot was relabeled Bet365")
	}
}

func TestDrawObservationsFrontendBatchProjectionIsBoundedAndLegacySafe(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	statements, parameters := []string{}, [][]interface{}{}
	if err := db.Callback().Query().After("gorm:query").Register("observe_frontend_projection", func(query *gorm.DB) {
		statements = append(statements, query.Statement.SQL.String())
		parameters = append(parameters, append([]interface{}{}, query.Statement.Vars...))
	}); err != nil {
		t.Fatal(err)
	}
	schema := drawObservationSchema{}
	for table, columns := range map[string][]string{"moneys": drawObservationFrontendMainColumns, "history_moneys": drawObservationFrontendHistoryColumns, "odds_moneys": drawObservationFrontendOddsColumns, "pankou_moneys": drawObservationFrontendPankouColumns} {
		schema[table] = map[string]bool{}
		for _, column := range columns {
			if column != "captured_at" {
				schema[table][column] = true
			}
		}
	}
	match, _, _, _ := drawObservationDisplayFixture()
	second := match
	second.MatchId, second.HomeRank = "legacy-only", "11"
	targets := []drawObservationTarget{{Match: match}, {Match: second, Sources: []string{"money"}}}
	rows, err := drawObservationReadFrontend(db, schema, targets)
	if err != nil || len(rows) != 2 || rows["legacy-only"].analysis.SanhuXinli[3] != "11~7" || len(statements) != 4 {
		t.Fatalf("batch loader lost legacy fallback or made N+1 queries: rows=%+v queries=%v error=%v", rows, statements, err)
	}
	for i, statement := range statements {
		if !strings.Contains(statement, "match_id IN (?,?)") || strings.Contains(statement, "captured_at") || strings.Contains(statement, "future_home") || strings.Contains(statement, "rank_data") || len(parameters[i]) != 2 {
			t.Fatalf("unbounded or nonexistent-column projection: %s vars=%v", statement, parameters[i])
		}
	}
	if !strings.Contains(statements[0], "home_rank") || !strings.Contains(statements[2], "sporttery_trade") {
		t.Fatalf("frontend calculation inputs were omitted: %v", statements)
	}
}

func TestDrawObservationsFrontendCacheUsesOnlyPageIDsAndRetriesRefreshes(t *testing.T) {
	fake := newDrawObservationsFake()
	loaders := fake.loaders()
	var calls atomic.Int32
	var fail atomic.Bool
	seen := [][]string{}
	loaders.frontend = func(_ drawObservationSchema, targets []drawObservationTarget) (map[string]drawObservationFrontendEnrichment, error) {
		calls.Add(1)
		if fail.Swap(false) {
			return nil, errors.New("in-memory frontend failure")
		}
		ids := []string{}
		rows := map[string]drawObservationFrontendEnrichment{}
		for _, target := range targets {
			ids = append(ids, target.Match.MatchId)
			rows[target.Match.MatchId] = buildDrawObservationFrontend(target.Match, models.HistoryMoney{}, models.OddsMoney{}, models.PankouMoney{})
		}
		sort.Strings(ids)
		seen = append(seen, ids)
		return rows, nil
	}
	cache := newDrawObservationCache(loaders, 30*time.Second, time.Now)
	q := drawObservationQuery{mode: "today", date: "2026-10-21", page: 1, size: 1}
	first, err := cache.read(q)
	if err != nil || first.Items[0].FrontendAnalysis == nil || len(first.Items[0].CompanyQuotes) != 3 || fake.quoteCalls.Load() != 0 {
		t.Fatalf("frontend batch did not replace duplicate odds load: error=%v response=%+v", err, first)
	}
	q.page = 2
	if _, err := cache.read(q); err != nil {
		t.Fatal(err)
	}
	q.page, q.homeDraws = 1, drawObservationRange{max: drawTestPtr(0)}
	filtered, err := cache.read(q)
	if err != nil || filtered.Total != 1 || calls.Load() != 2 || !reflect.DeepEqual(seen, [][]string{{"day-0"}, {"day-1"}}) {
		t.Fatalf("filter/page change loaded off-page inputs or ignored cached values: seen=%v error=%v", seen, err)
	}
	q.homeDraws, q.page = drawObservationRange{}, 3
	fail.Store(true)
	if _, err := cache.read(q); err == nil {
		t.Fatal("frontend failure was hidden")
	}
	if _, err := cache.read(q); err != nil || calls.Load() != 4 {
		t.Fatalf("frontend failure was permanently cached: error=%v calls=%d", err, calls.Load())
	}
	q.page, q.refresh = 1, true
	if _, err := cache.read(q); err != nil || calls.Load() != 5 || fake.baseCalls.Load() != 2 {
		t.Fatalf("refresh retained frontend values from the old generation: error=%v calls=%d", err, calls.Load())
	}
}
