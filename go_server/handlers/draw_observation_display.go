package handlers

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go_server/models"
	"gorm.io/gorm"
)

type drawObservationFrontendAnalysis struct {
	Basis           *drawObservationAnalysisBasis `json:"basis,omitempty"`
	YapanPankou1    float64                       `json:"yapanpankou1"`
	YapanPankou2    float64                       `json:"yapanpankou2"`
	QiushuPankou1   float64                       `json:"qiushupankou1"`
	QiushuPankou2   float64                       `json:"qiushupankou2"`
	YaPanTouZhu     []interface{}                 `json:"yapantouzhu"`
	NewYaPanTouZhu  []interface{}                 `json:"newyapantouzhu"`
	QiuShuTouZhu    []interface{}                 `json:"qiushutouzhu"`
	NewQiuShuTouZhu []interface{}                 `json:"newqiushutouzhu"`
	WinProbability  float64                       `json:"winProbability"`
	DrawProbability float64                       `json:"drawProbability"`
	LoseProbability float64                       `json:"loseProbability"`
	SanhuXinli      []string                      `json:"sanhuxinli"`
	KaiLiResult     []string                      `json:"kailiresult"`
	TiCaiResult     []string                      `json:"ticairesult"`
	RoiSimulation   *matchRoiSimulationResponse   `json:"roiSimulation"`
	LocalMarket     *bookmakerMarketResponse      `json:"localMarket"`
}

type drawObservationAnalysisBasis struct {
	Asian string `json:"asian"`
	Goals string `json:"goals"`
	Kelly string `json:"kelly"`
	Ticai string `json:"ticai"`
}

func drawObservationEuroSource(row analysisEuroOdd) string {
	name := row.CompanyName
	if key, _ := drawObservationMarketCompanyID(row.CompanyID, "euro"); key != "" {
		name = map[string]string{"william": "威廉", "bet365": "Bet365"}[key]
	}
	if name == "" {
		name = "原始公司报价"
	}
	if row.CompanyID != "" {
		name += "（欧赔 ID " + row.CompanyID + "）"
	}
	return name
}

func drawObservationLineBasis(row analysisPankouItem, goals bool) string {
	if goals && parsePankouLine(firstNonEmptyString(row.Pankou, row.FirstPankou)) == 0 {
		return "缺少可用大小球盘口，当前分析值回退为近期与交锋场均总进球的较大值"
	}
	if row.FirstPankou == "" && row.Pankou == "" {
		return "缺少盘口记录，前台原有分析按 0 计算"
	}
	name := row.CompanyName
	if key, _ := drawObservationMarketCompanyID(fmt.Sprint(row.CompanyID), "pankou"); key != "" {
		name = map[string]string{"bet365": "Bet365", "william": "威廉"}[key]
	}
	if name == "" {
		name = "原始公司报价"
	}
	if row.CompanyID != 0 {
		name += fmt.Sprintf("（盘口 ID %d）", row.CompanyID)
	}
	if row.Pankou == "" {
		name += "；即时盘缺失，沿用该公司初盘"
	}
	if row.FirstPankou == "" {
		name += "；初盘缺失，初盘分析值按 0 计算"
	}
	return name
}

func drawObservationBasis(odds models.OddsMoney, pankou models.PankouMoney) *drawObservationAnalysisBasis {
	asia, goals := pankouRows(pankou)
	basis := &drawObservationAnalysisBasis{
		Asian: drawObservationLineBasis(selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Asia), asia, 8), false),
		Goals: drawObservationLineBasis(selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Dxq), goals, 8), true),
		Kelly: "可用赔率不足，回退平均欧赔隐含概率最高方向；平均欧赔缺失时使用胜33%/平34%/负33%",
		Ticai: "可用赔率不足，回退平均欧赔隐含概率最高方向；平均欧赔缺失时使用胜33%/平34%/负33%",
	}
	avg, rows, _ := resolveAverageOdds(odds)
	if len(kellyResult(odds, avg, rows)) > 0 {
		basis.Kelly = drawObservationEuroSource(kellySource(odds, rows)) + "即时赔率 ÷ 平均欧赔 × 平均返还率，取凯利值最小方向；并列优先负、平、胜"
	}
	if len(ticaiResult(rows, avg, sportteryOdds(sportteryTradeFromJSON(odds.SportteryTrade)))) > 0 {
		reference := findEuroOdd(rows, "115", "威廉")
		comparison := "竞彩即时赔率"
		if len(reference.Odds) < 3 || len(sportteryOdds(sportteryTradeFromJSON(odds.SportteryTrade))) < 3 {
			comparison = "平均欧赔（竞彩或威廉数据不足时的原有回退）"
			if len(reference.Odds) < 3 {
				reference = firstEuroOdd(rows)
			}
		}
		basis.Ticai = drawObservationEuroSource(reference) + "即时赔率与" + comparison + "逐项相减，取绝对差最小方向；并列优先负、平、胜"
	}
	return basis
}

type drawObservationCompanyQuotes struct {
	Key           string                  `json:"key"`
	Name          string                  `json:"name"`
	EuroOdds      drawObservationEuroOdds `json:"euroOdds"`
	AsianHandicap drawObservationLine     `json:"asianHandicap"`
	GoalsLine     drawObservationLine     `json:"goalsLine"`
}

type drawObservationFrontendEnrichment struct {
	analysis  *drawObservationFrontendAnalysis
	companies []drawObservationCompanyQuotes
	euro      drawObservationEuroOdds
}

func drawObservationFrontendValues(response *analysisMatchResponse) *drawObservationFrontendAnalysis {
	return &drawObservationFrontendAnalysis{
		YapanPankou1: response.YapanPankou1, YapanPankou2: response.YapanPankou2,
		QiushuPankou1: response.QiushuPankou1, QiushuPankou2: response.QiushuPankou2,
		YaPanTouZhu: response.YaPanTouZhu, NewYaPanTouZhu: response.NewYaPanTouZhu,
		QiuShuTouZhu: response.QiuShuTouZhu, NewQiuShuTouZhu: response.NewQiuShuTouZhu,
		WinProbability: response.WinProbability, DrawProbability: response.DrawProbability, LoseProbability: response.LoseProbability,
		SanhuXinli: response.SanhuXinli, KaiLiResult: response.KaiLiResult, TiCaiResult: response.TiCaiResult,
		RoiSimulation: response.RoiSimulation, LocalMarket: pfLocalProfitMarket(response),
	}
}

func buildDrawObservationFrontend(match models.Money, history models.HistoryMoney, odds models.OddsMoney, pankou models.PankouMoney) drawObservationFrontendEnrichment {
	response := buildAnalysisFromData(match, history, odds, pankou, sportteryTradeFromJSON(odds.SportteryTrade))
	attachAnalysisWeights(&response)
	analysis := drawObservationFrontendValues(&response)
	analysis.Basis = drawObservationBasis(odds, pankou)
	return drawObservationFrontendEnrichment{analysis: analysis, companies: drawObservationCompanies(odds, pankou), euro: drawObservationEuro(odds)}
}

// Frontend enrichment happens after count filters and pagination. All inputs
// are loaded in batches, and a cache generation shares each ID's values across
// page/filter changes. Loading odds here also supplies the original euro field.
func (core *drawObservationCore) frontend(ids []string, targets []drawObservationTarget, loaders drawObservationLoaders) (map[string]drawObservationFrontendEnrichment, error) {
	if loaders.frontend == nil || len(ids) == 0 {
		return nil, nil
	}
	core.frontendMu.Lock()
	defer core.frontendMu.Unlock()
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		if _, exists := core.frontendCache[id]; !exists {
			wanted[id] = true
		}
	}
	if len(wanted) > 0 {
		missing := make([]drawObservationTarget, 0, len(wanted))
		for _, target := range targets {
			if wanted[target.Match.MatchId] {
				missing = append(missing, target)
			}
		}
		loaded, err := loaders.frontend(core.base.schema, missing)
		if err != nil {
			return nil, err
		}
		core.quoteMu.Lock()
		for id := range wanted {
			value, exists := loaded[id]
			if !exists {
				value.euro = drawObservationEuro(models.OddsMoney{})
			}
			core.frontendCache[id] = value
			core.euroCache[id] = value.euro
		}
		core.quoteMu.Unlock()
	}
	result := make(map[string]drawObservationFrontendEnrichment, len(ids))
	for _, id := range ids {
		result[id] = core.frontendCache[id]
	}
	return result, nil
}

// Only columns consumed by the shared frontend calculation are read. Column
// intersection supports old databases without captured_at; no schema changes.
var drawObservationFrontendMainColumns = append(append([]string{}, drawObservationHeaderColumns...), "home_rank", "guest_rank")
var drawObservationFrontendHistoryColumns = []string{"match_id", "league_stat", "against_summary", "against_list", "recent_home_list", "recent_guest_list"}
var drawObservationFrontendOddsColumns = []string{"match_id", "data", "avg_odds", "bet365", "william", "pinnacle", "sporttery_trade", "company_count", "captured_at"}
var drawObservationFrontendPankouColumns = []string{"match_id", "asia_data", "dxq_data", "bet365_asia", "bet365_dxq", "asia_count", "dxq_count", "captured_at"}

func drawObservationReadFrontend(db *gorm.DB, schema drawObservationSchema, targets []drawObservationTarget) (map[string]drawObservationFrontendEnrichment, error) {
	result := make(map[string]drawObservationFrontendEnrichment, len(targets))
	if len(targets) == 0 {
		return result, nil
	}
	ids := make([]string, 0, len(targets))
	main := make(map[string]models.Money, len(targets))
	for _, target := range targets {
		ids = append(ids, target.Match.MatchId)
		main[target.Match.MatchId] = target.Match // Includes legacy-only targets.
	}
	if schema["moneys"]["match_id"] {
		var rows []models.Money
		if err := db.Table("moneys").Select(drawObservationProjection(schema, "moneys", drawObservationFrontendMainColumns)).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			main[row.MatchId] = row
		}
	}
	var histories []models.HistoryMoney
	var odds []models.OddsMoney
	var pankou []models.PankouMoney
	for _, table := range []struct {
		name    string
		columns []string
		rows    interface{}
	}{{"history_moneys", drawObservationFrontendHistoryColumns, &histories}, {"odds_moneys", drawObservationFrontendOddsColumns, &odds}, {"pankou_moneys", drawObservationFrontendPankouColumns, &pankou}} {
		if !schema[table.name]["match_id"] {
			continue
		}
		if err := db.Table(table.name).Select(drawObservationProjection(schema, table.name, table.columns)).Where("match_id IN ?", ids).Find(table.rows).Error; err != nil {
			return nil, err
		}
	}
	historyByID := make(map[string]models.HistoryMoney, len(histories))
	oddsByID := make(map[string]models.OddsMoney, len(odds))
	pankouByID := make(map[string]models.PankouMoney, len(pankou))
	for _, row := range histories {
		historyByID[row.MatchId] = row
	}
	for _, row := range odds {
		oddsByID[row.MatchId] = row
	}
	for _, row := range pankou {
		pankouByID[row.MatchId] = row
	}
	for _, id := range ids {
		result[id] = buildDrawObservationFrontend(main[id], historyByID[id], oddsByID[id], pankouByID[id])
	}
	return result, nil
}

func drawObservationCompanyNameKey(name string) string {
	name = strings.ToLower(strings.Join(strings.Fields(name), ""))
	switch {
	case strings.Contains(name, "bet365"), name == "bet3**":
		return "bet365"
	case strings.Contains(name, "william"), strings.Contains(name, "威廉"):
		return "william"
	case strings.Contains(name, "sporttery"), strings.Contains(name, "竞彩"):
		return "sporttery"
	}
	return ""
}

func drawObservationMarketCompanyID(id, market string) (string, bool) {
	if market == "euro" {
		if id == "8" || id == "9" {
			return "", true
		}
		switch id {
		case "281":
			return "bet365", false
		case "115":
			return "william", false
		}
	} else {
		if id == "281" || id == "115" {
			return "", true
		}
		switch id {
		case "8":
			return "bet365", false
		case "9":
			return "william", false
		}
	}
	return "", false
}

func drawObservationCompanyMatches(id, name, key, market string, dedicated bool) bool {
	idKey, otherMarket := drawObservationMarketCompanyID(id, market)
	if otherMarket {
		return false
	}
	// Provider IDs identify the company even when the source name is masked,
	// outdated or incorrect. Only unknown IDs may fall back to a clear name.
	if idKey != "" {
		return idKey == key
	}
	if nameKey := drawObservationCompanyNameKey(name); nameKey != "" {
		return nameKey == key
	}
	if strings.TrimSpace(name) != "" {
		return false
	}
	return dedicated && id == ""
}

func drawObservationCompanyLabel(id, sourceName, key, name, market string) string {
	if idKey, _ := drawObservationMarketCompanyID(id, market); idKey == key {
		return name
	}
	return firstNonEmptyString(sourceName, name)
}

func drawObservationEuroRows(raw []byte) ([]map[string]json.RawMessage, bool) {
	var rows []map[string]json.RawMessage
	if json.Unmarshal(raw, &rows) == nil && strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		return rows, true
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) == nil {
		if market, present := payload["odds"]; present {
			_ = json.Unmarshal(market, &rows)
			return rows, true
		}
	}
	return nil, false
}

func drawObservationAnyRawOdds(values ...[]*string) bool {
	for _, group := range values {
		for _, value := range group {
			if value != nil {
				return true
			}
		}
	}
	return false
}

func drawObservationCompanyEuro(odds models.OddsMoney, key, name string) drawObservationEuroOdds {
	result := drawObservationEuroOdds{Opening: make([]*string, 3), Latest: make([]*string, 3), Source: "missing", CapturedAt: odds.CapturedAt}
	if key == "sporttery" {
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(odds.SportteryTrade, &raw)
		var data map[string]json.RawMessage
		if json.Unmarshal(raw["data"], &data) == nil {
			if _, present := data["tzbl"]; present {
				raw = data
			}
		}
		var ratios map[string]json.RawMessage
		_ = json.Unmarshal(raw["tzbl"], &ratios)
		for i, field := range []string{"h", "d", "a"} {
			result.Latest[i] = drawObservationRawLine(ratios[field])
		}
		if opening, latest, present := sportteryRecordedPrices(sportteryRecordedBody(odds.SportteryTrade)); present {
			result.Opening, result.Latest = opening, latest
		}
		if drawObservationAnyRawOdds(result.Latest) {
			result.Company = name
		}
		// Keep captured zero text visible, but do not mark an unopened/partial
		// official price as available or borrow the frontend's simulated price.
		if len(sportteryOdds(sportteryTradeFromJSON(odds.SportteryTrade))) == 3 {
			result.Source = "odds_moneys"
		}
		return result
	}
	rows, present := drawObservationEuroRows(odds.Data)
	if !present {
		var preferred map[string]json.RawMessage
		raw := odds.William
		if key == "bet365" {
			raw = odds.Bet365
		}
		if json.Unmarshal(raw, &preferred) == nil {
			rows = []map[string]json.RawMessage{preferred}
		}
	}
	for _, row := range rows {
		company, id := drawRawString(row["companyName"]), drawRawID(row["companyId"])
		if !drawObservationCompanyMatches(id, company, key, "euro", !present) {
			continue
		}
		opening, latest := drawObservationRawOdds(row["firstOdds"], 3), drawObservationRawOdds(row["odds"], 3)
		if !drawObservationAnyRawOdds(opening, latest) {
			continue
		}
		result.Company, result.Opening, result.Latest, result.Source = drawObservationCompanyLabel(id, company, key, name, "euro"), opening, latest, "odds_moneys"
		break
	}
	return result
}

func drawObservationCompanyLine(rows []drawObservationQuote, preferred []byte, key, name string, capturedAt *time.Time) drawObservationLine {
	result := drawObservationLine{Source: "missing", OpeningOdds: make([]*string, 2), LatestOdds: make([]*string, 2), CapturedAt: capturedAt}
	dedicated := len(preferred) > 0
	if dedicated {
		var row map[string]json.RawMessage
		if json.Unmarshal(preferred, &row) == nil {
			rows = []drawObservationQuote{drawObservationQuoteRow(row)}
		}
	}
	for _, quote := range rows {
		if !drawObservationCompanyMatches(quote.companyID, quote.company, key, "pankou", dedicated) {
			continue
		}
		if quote.opening == nil && quote.latest == nil && !drawObservationAnyRawOdds(quote.openingOdds, quote.latestOdds) {
			continue
		}
		result.Company, result.Source = drawObservationCompanyLabel(quote.companyID, quote.company, key, name, "pankou"), "pankou_moneys"
		result.Opening, result.Latest, result.OpeningOdds, result.LatestOdds = quote.opening, quote.latest, quote.openingOdds, quote.latestOdds
		break
	}
	return result
}

func drawObservationCompanies(odds models.OddsMoney, pankou models.PankouMoney) []drawObservationCompanyQuotes {
	asia, asianPresent := drawObservationMarketRows(pankou.AsiaData, "asia")
	var totals []drawObservationQuote
	totalPresent := false
	var combined map[string]json.RawMessage
	if json.Unmarshal(pankou.AsiaData, &combined) == nil {
		if raw, present := combined["dxq"]; present {
			totals, totalPresent = drawObservationQuotes(raw, "dxq"), true
		}
	}
	if !totalPresent {
		totals, totalPresent = drawObservationMarketRows(pankou.DxqData, "dxq")
	}
	result := make([]drawObservationCompanyQuotes, 0, 3)
	for _, company := range []struct{ key, name string }{{"sporttery", "竞彩"}, {"william", "威廉"}, {"bet365", "Bet365"}} {
		var asianPreferred, totalPreferred []byte
		if company.key == "bet365" {
			if !asianPresent {
				asianPreferred = pankou.Bet365Asia
			}
			if !totalPresent {
				totalPreferred = pankou.Bet365Dxq
			}
		}
		result = append(result, drawObservationCompanyQuotes{Key: company.key, Name: company.name,
			EuroOdds:      drawObservationCompanyEuro(odds, company.key, company.name),
			AsianHandicap: drawObservationCompanyLine(asia, asianPreferred, company.key, company.name, pankou.CapturedAt),
			GoalsLine:     drawObservationCompanyLine(totals, totalPreferred, company.key, company.name, pankou.CapturedAt)})
	}
	return result
}
