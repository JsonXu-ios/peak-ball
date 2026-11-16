package handlers

// Collector-only input preparation. These extra market parameters are kept
// outside both frozen models: adding a number does not give it trained weight.
import (
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"
)

type publicSourceCPQuote struct {
	Source          string   `json:"source"`
	SourceCompanyID string   `json:"sourceCompanyId"`
	SourceMatchID   string   `json:"sourceMatchId"`
	CPMatchID       string   `json:"cpMatchId"`
	ZgzcwMatchID    string   `json:"zgzcwMatchId"`
	Label           string   `json:"sourceLabel"`
	Opening         []string `json:"opening"`
	Latest          []string `json:"latest"`
	Kelly           []string `json:"kelly"`
	Status          string   `json:"status"`
	ReceivedAt      string   `json:"receivedAt"`
	QuoteReceivedAt string   `json:"quoteReceivedAt"`
	KellyReceivedAt string   `json:"kellyReceivedAt"`
}

type publicSourceEuroInput struct {
	Company         string   `json:"company"`
	Source          string   `json:"source"`
	SourceCompanyID string   `json:"sourceCompanyId"`
	SourceMatchID   string   `json:"sourceMatchId"`
	ReceivedAt      string   `json:"receivedAt"`
	Opening         []string `json:"opening"`
	Latest          []string `json:"latest"`
	Kelly           []string `json:"kelly"`
	OpeningKelly    []string `json:"openingKelly"`
	OpeningSource   string   `json:"openingSource"`
	OpeningAt       string   `json:"openingAt,omitempty"`
	Reason          string   `json:"reason"`
}

func publicSourcePositiveTriple(values []string) bool {
	if len(values) != 3 {
		return false
	}
	for _, value := range values {
		n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return false
		}
	}
	return true
}

func publicSourceSameEuro(a, b []string) bool {
	if !publicSourceValidTriple(a) || !publicSourceValidTriple(b) {
		return false
	}
	return publicSourceSamePositive(a, b)
}

func publicSourceSamePositive(a, b []string) bool {
	if !publicSourcePositiveTriple(a) || !publicSourcePositiveTriple(b) {
		return false
	}
	for i := range a {
		x, ok := new(big.Rat).SetString(strings.TrimSpace(a[i]))
		y, other := new(big.Rat).SetString(strings.TrimSpace(b[i]))
		if !ok || !other || x.Cmp(y) != 0 {
			return false
		}
	}
	return true
}

func publicSourceHistoryTime(raw string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if at, err := time.ParseInLocation(layout, strings.TrimSpace(raw), drawSourceLocation); err == nil {
			return at, true
		}
	}
	return time.Time{}, false
}

func publicSourceCleanHistoryValues(values []string) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(v), "↑"), "↓"))
	}
	return result
}

// Recover a whole opening phase only from the earliest valid full-history
// record. The summary Kelly/prices/time must independently identify that same
// row; no price from one phase is combined with another phase's Kelly.
func publicSourcePairedHistoryOpening(input PublicSourceModelInput, q publicSourceQuote, kickoff time.Time) ([]string, []string, bool) {
	capture, e := time.Parse(time.RFC3339Nano, input.Provenance["euro"].Received)
	marked, ok := publicSourceHistoryTime(q.OpeningKellyTime)
	if e != nil || !capture.Before(kickoff) || !ok || (q.SourceMatchID != "" && input.Match.ZgzcwID != "" && q.SourceMatchID != input.Match.ZgzcwID) {
		return nil, nil, false
	}
	var earliest time.Time
	var prices, kelly []string
	ambiguous := false
	for _, row := range q.FullQuoteHistory {
		if len(row) < 12 {
			continue
		}
		at, valid := publicSourceHistoryTime(row[1])
		p, k := publicSourceCleanHistoryValues(row[3:6]), publicSourceCleanHistoryValues(row[9:12])
		if !valid || !publicSourceValidTriple(p) || !publicSourcePositiveTriple(k) {
			continue
		}
		if earliest.IsZero() || at.Before(earliest) {
			earliest, prices, kelly, ambiguous = at, p, k, false
		} else if at.Equal(earliest) && (!publicSourceSameEuro(p, prices) || !publicSourceSamePositive(k, kelly)) {
			ambiguous = true
		}
	}
	if earliest.IsZero() || ambiguous || !earliest.Equal(marked) || earliest.After(capture) || !earliest.Before(kickoff) || !publicSourceSameEuro(prices, publicSourceCleanHistoryValues(q.OpeningKellyPrices)) || !publicSourceSamePositive(kelly, publicSourceCleanHistoryValues(q.OpeningKelly)) {
		return nil, nil, false
	}
	return prices, kelly, true
}

func publicSourceCPProblem(input PublicSourceModelInput, q publicSourceQuote, kickoff time.Time) string {
	cp, bonus := q.CPExtras, input.Parts.Bonus
	if cp == nil {
		return ""
	}
	company := publicSourceCompany(q)
	wantID := map[string]string{"average": "0", "william": "9", "bet365": "2"}[company]
	if cp.Source != "zgzcw_cp" || wantID == "" || cp.SourceCompanyID != wantID || publicSourceCompany(publicSourceQuote{Label: cp.Label}) != company || (cp.Status != "matched" && cp.Status != "price_mismatch") {
		return "CP 公司或合并状态未核实，保留原报价"
	}
	// The collector has already verified the cross-provider match link. Its
	// accepted heading supplies Match.ZgzcwID; the bonus supplies the CP ID.
	// Display names can differ across all three sources, so they neither prove
	// nor veto this mandatory, exact ID chain. Never guess missing IDs by name.
	if input.Match.ZgzcwID == "" || bonus.Source != "zgzcw_cp" || bonus.SourceMatchID == "" || bonus.CPMatchID != bonus.SourceMatchID || bonus.ZgzcwMatchID != input.Match.ZgzcwID || cp.SourceMatchID != bonus.SourceMatchID || cp.CPMatchID != bonus.SourceMatchID || cp.ZgzcwMatchID != input.Match.ZgzcwID || (q.SourceMatchID != "" && q.SourceMatchID != input.Match.ZgzcwID) {
		return "CP 与本场比赛身份未配对，保留原报价"
	}
	if !publicSourceValidTriple(cp.Opening) || !publicSourceValidTriple(cp.Latest) || !publicSourcePositiveTriple(cp.Kelly) {
		return "CP 欧赔与凯利组合不完整，保留原报价"
	}
	capture, err := time.Parse(time.RFC3339Nano, input.Provenance["euro"].Received)
	if err != nil || !capture.Before(kickoff) {
		return "CP 缺少可核对的欧赔快照时间，保留原报价"
	}
	// Old saved bundles predate per-field receipt timestamps. The complete
	// parent snapshot is a conservative capture upper bound, not source update
	// time. A partially supplied or explicit invalid receipt is never erased.
	if cp.QuoteReceivedAt == "" && cp.KellyReceivedAt == "" && cp.ReceivedAt == "" {
		return ""
	}
	var latest time.Time
	for _, stamp := range []string{cp.QuoteReceivedAt, cp.KellyReceivedAt, cp.ReceivedAt} {
		at, e := time.Parse(time.RFC3339Nano, stamp)
		if e != nil || !at.Before(kickoff) || at.After(capture) {
			return "CP 报价或凯利采集时间未核实为赛前，保留原报价"
		}
		if at.After(latest) {
			latest = at
		}
	}
	at, _ := time.Parse(time.RFC3339Nano, cp.ReceivedAt)
	if !at.Equal(latest) {
		return "CP 组合时间早于字段采集时间，保留原报价"
	}
	return ""
}

func publicSourceSelectEuro(input PublicSourceModelInput, q publicSourceQuote, kickoff time.Time) (publicSourceQuote, publicSourceEuroInput) {
	source := publicSourceEuroInput{Company: publicSourceCompany(q), Source: q.Source, SourceCompanyID: q.SourceCompanyID, SourceMatchID: q.SourceMatchID, ReceivedAt: input.Provenance["euro"].Received, Reason: "采用原来源报价"}
	if source.Source == "" {
		source.Source = "source_quote"
	}
	completeOriginal := publicSourceValidTriple(q.Opening) && publicSourceValidTriple(q.Latest) && publicSourcePositiveTriple(q.Kelly)
	if completeOriginal {
		source.Reason = "采用已有完整原来源欧赔与即时凯利"
	} else if q.CPExtras != nil {
		if problem := publicSourceCPProblem(input, q, kickoff); problem != "" {
			source.Reason = problem
		} else {
			cp := q.CPExtras
			q.Opening, q.Latest, q.Kelly = cp.Opening, cp.Latest, cp.Kelly
			source.Source, source.SourceCompanyID, source.SourceMatchID, source.ReceivedAt = cp.Source, cp.SourceCompanyID, cp.SourceMatchID, cp.ReceivedAt
			source.Reason = "采用同一 CP 来源的完整初即欧赔与即时凯利组合"
			if cp.ReceivedAt == "" {
				source.ReceivedAt = input.Provenance["euro"].Received
				source.Reason += "；旧快照无独立字段采集时间，使用欧赔快照赛前捕获上界"
			}
		}
	}
	source.OpeningSource = source.Source
	if len(q.OpeningKelly) > 0 && (!publicSourcePositiveTriple(q.OpeningKelly) || !publicSourceSameEuro(q.OpeningKellyPrices, q.Opening)) {
		if prices, kelly, ok := publicSourcePairedHistoryOpening(input, q, kickoff); ok {
			q.Opening, q.OpeningKelly = prices, kelly
			source.OpeningSource, source.OpeningAt = "zgzcw_full_quote_history", q.OpeningKellyTime
			source.Reason = strings.ReplaceAll(source.Reason, "完整初即欧赔与即时凯利组合", "完整即时欧赔与凯利组合")
			source.Reason += "；初盘采用来源完整历史最早配对报价与凯利，即时阶段来源保持上述选择"
		} else {
			q.OpeningKelly = nil
			source.Reason += "；初始凯利对应赔率与所选初赔不一致或缺少配对证据，未带入"
		}
	}
	source.Opening, source.Latest, source.Kelly, source.OpeningKelly = q.Opening, q.Latest, q.Kelly, q.OpeningKelly
	return q, source
}

func publicSourceEuroSources(input PublicSourceModelInput, kickoff time.Time) ([]publicSourceEuroInput, []string) {
	result, warnings, counts := []publicSourceEuroInput{}, []string{}, map[string]int{}
	for _, q := range input.Parts.Euro {
		counts[publicSourceCompany(q)]++
	}
	for _, q := range input.Parts.Euro {
		key := publicSourceCompany(q)
		if key == "" || key == "sporttery" || counts[key] != 1 {
			continue
		}
		_, source := publicSourceSelectEuro(input, q, kickoff)
		result = append(result, source)
		if strings.Contains(source.Reason, "保留原报价") || strings.Contains(source.Reason, "未带入") {
			warnings = append(warnings, key+"："+source.Reason)
		}
	}
	return result, warnings
}

type publicSourceMarketContext struct {
	Version            string                      `json:"version"`
	UsedByFrozenModels bool                        `json:"usedByFrozenModels"`
	Note               string                      `json:"note"`
	Averaging          string                      `json:"averaging"`
	AsianSign          string                      `json:"asianSign"`
	Markets            []publicSourceContextMarket `json:"markets"`
	Features           map[string]float64          `json:"features"`
}
type publicSourceContextMarket struct {
	Key          string                     `json:"key"`
	LeftMeaning  string                     `json:"leftMeaning"`
	RightMeaning string                     `json:"rightMeaning"`
	Phases       []publicSourceContextPhase `json:"phases"`
}
type publicSourceContextPhase struct {
	Phase          string                         `json:"phase"`
	Macau          *publicSourceContextQuote      `json:"macau"`
	Averages       []publicSourceContextQuote     `json:"averages"`
	MacauVsAverage *publicSourceContextDifference `json:"macauVsAverage"`
}
type publicSourceContextQuote struct {
	Line         float64  `json:"line"`
	LeftWater    float64  `json:"leftWater"`
	RightWater   float64  `json:"rightWater"`
	WaterGap     float64  `json:"waterGap"`
	OverWater    *float64 `json:"overWater,omitempty"`
	UnderWater   *float64 `json:"underWater,omitempty"`
	CompanyCount int      `json:"companyCount"`
	Companies    []string `json:"companies"`
}
type publicSourceContextDifference struct {
	LeftWaterDifference  float64 `json:"leftWaterDifference"`
	RightWaterDifference float64 `json:"rightWaterDifference"`
	WaterGapDifference   float64 `json:"waterGapDifference"`
	CompanyCount         int     `json:"companyCount"`
}

func publicSourceContextCompany(q publicSourceQuote) string {
	if key := publicSourceCompany(q); key != "" {
		return key
	}
	label := q.Company
	if label == "" {
		label = q.Label
	}
	label = strings.ToLower(strings.TrimSpace(label))
	switch label {
	case "澳*", "澳门", "澳門", "macau", "macao":
		return "macau"
	case "明*":
		return "*陞"
	case "12b*t":
		return "12b*"
	case "18b*t":
		return "18b*"
	}
	return label
}

func publicSourceContextLine(raw string, goals bool) (float64, bool) {
	if goals {
		parts, sum := strings.Split(strings.TrimSpace(raw), "/"), 0.0
		if len(parts) > 2 {
			return 0, false
		}
		values := []float64{}
		for _, p := range parts {
			n, e := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(p), "球")), 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || math.Abs(n*4-math.Round(n*4)) > 1e-8 {
				return 0, false
			}
			sum += n
			values = append(values, n)
		}
		if len(values) == 2 && (values[1]-values[0] != .5 || math.Mod(values[0]*2, 1) != 0) {
			return 0, false
		}
		return sum / float64(len(parts)), true
	}
	// Numeric Asian lines have conflicting provider signs; accept only the
	// source's explicit give/receive words, matching the Python join contract.
	text, sign := strings.ReplaceAll(strings.TrimSpace(raw), " ", ""), 1.0
	if strings.HasPrefix(text, "受让") {
		sign = -1
		text = strings.TrimPrefix(text, "受让")
	} else if strings.HasPrefix(text, "受") {
		sign = -1
		text = strings.TrimPrefix(text, "受")
	} else {
		text = strings.TrimPrefix(text, "让")
	}
	words := map[string]float64{"平": 0, "平手": 0, "半": .5, "半球": .5, "球半": 1.5}
	for i, w := range []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"} {
		n := float64(i + 1)
		words[w] = n
		words[w+"球"] = n
		words[w+"半"] = n + .5
		words[w+"球半"] = n + .5
	}
	words["两"], words["两球"], words["两半"], words["两球半"] = 2, 2, 2.5, 2.5
	parts := strings.Split(text, "/")
	if len(parts) > 2 {
		return 0, false
	}
	a, ok := words[parts[0]]
	if !ok {
		return 0, false
	}
	if len(parts) == 2 {
		b, ok := words[parts[1]]
		if !ok || b-a != .5 {
			return 0, false
		}
		a = (a + b) / 2
	}
	return sign * a, true
}

func publicSourceContextPrices(raw []string, goals bool) *publicSourceContextQuote {
	if len(raw) != 3 {
		return nil
	}
	l, e := strconv.ParseFloat(strings.TrimSpace(raw[0]), 64)
	r, f := strconv.ParseFloat(strings.TrimSpace(raw[2]), 64)
	line, ok := publicSourceContextLine(raw[1], goals)
	if e != nil || f != nil || !ok || l <= 0 || r <= 0 || math.IsNaN(l) || math.IsNaN(r) || math.IsInf(l, 0) || math.IsInf(r, 0) {
		return nil
	}
	q := &publicSourceContextQuote{Line: line, LeftWater: l, RightWater: r, WaterGap: l - r, CompanyCount: 1}
	if goals {
		q.OverWater = &q.LeftWater
		q.UnderWater = &q.RightWater
	}
	return q
}

func publicSourceBuildMarketContext(input PublicSourceModelInput) *publicSourceMarketContext {
	result := &publicSourceMarketContext{Version: "public-source-market-context-v1", Note: "参数已准备，当前固定模型未使用", Averaging: "同市场同阶段同盘口真实公司等权均值，包含澳门，排除平均合成行与重复公司；水位差为原左减原右", AsianSign: "正值主让，负值主受让；原左为主队，原右为客队", Markets: []publicSourceContextMarket{}, Features: map[string]float64{}}
	for _, market := range []struct {
		key    string
		quotes []publicSourceQuote
	}{{"asian", input.Parts.Asian}, {"totals", input.Parts.Totals}} {
		goals := market.key == "totals"
		m := publicSourceContextMarket{Key: market.key, LeftMeaning: "home", RightMeaning: "away", Phases: []publicSourceContextPhase{}}
		if goals {
			m.LeftMeaning, m.RightMeaning = "over", "under"
		}
		counts := map[string]int{}
		for _, q := range market.quotes {
			counts[publicSourceContextCompany(q)]++
		}
		for _, phase := range []string{"opening", "latest"} {
			p := publicSourceContextPhase{Phase: phase, Averages: []publicSourceContextQuote{}}
			groups := map[float64]*publicSourceContextQuote{}
			for _, q := range market.quotes {
				key := publicSourceContextCompany(q)
				if key == "" || key == "average" || counts[key] != 1 || (q.SourceMatchID != "" && input.Match.ZgzcwID != "" && q.SourceMatchID != input.Match.ZgzcwID) {
					continue
				}
				raw := q.Opening
				if phase == "latest" {
					raw = q.Latest
				}
				v := publicSourceContextPrices(raw, goals)
				if v == nil {
					continue
				}
				v.Companies = []string{q.Label}
				if key == "macau" {
					p.Macau = v
				}
				g := groups[v.Line]
				if g == nil {
					g = &publicSourceContextQuote{Line: v.Line, Companies: []string{}}
					groups[v.Line] = g
				}
				g.LeftWater += v.LeftWater
				g.RightWater += v.RightWater
				g.CompanyCount++
				g.Companies = append(g.Companies, q.Label)
			}
			lines := []float64{}
			for line := range groups {
				lines = append(lines, line)
			}
			sort.Float64s(lines)
			for _, line := range lines {
				g := groups[line]
				g.LeftWater /= float64(g.CompanyCount)
				g.RightWater /= float64(g.CompanyCount)
				g.WaterGap = g.LeftWater - g.RightWater
				sort.Strings(g.Companies)
				if goals {
					g.OverWater = &g.LeftWater
					g.UnderWater = &g.RightWater
				}
				p.Averages = append(p.Averages, *g)
			}
			prefix := market.key + "." + phase + "."
			put := func(name string, q *publicSourceContextQuote) {
				for field, value := range map[string]float64{"line": q.Line, "leftWater": q.LeftWater, "rightWater": q.RightWater, "waterGap": q.WaterGap, "companyCount": float64(q.CompanyCount)} {
					result.Features[prefix+name+"."+field] = value
				}
				if goals {
					result.Features[prefix+name+".overWater"] = q.LeftWater
					result.Features[prefix+name+".underWater"] = q.RightWater
				}
			}
			for _, g := range p.Averages {
				put("averageByLine."+strconv.FormatFloat(g.Line, 'f', -1, 64), &g)
			}
			if p.Macau != nil {
				put("macau", p.Macau)
				g := groups[p.Macau.Line]
				put("averageAtMacauLine", g)
				p.MacauVsAverage = &publicSourceContextDifference{LeftWaterDifference: p.Macau.LeftWater - g.LeftWater, RightWaterDifference: p.Macau.RightWater - g.RightWater, WaterGapDifference: p.Macau.WaterGap - g.WaterGap, CompanyCount: g.CompanyCount}
				result.Features[prefix+"macauVsAverage.leftWaterDifference"] = p.MacauVsAverage.LeftWaterDifference
				result.Features[prefix+"macauVsAverage.rightWaterDifference"] = p.MacauVsAverage.RightWaterDifference
				result.Features[prefix+"macauVsAverage.waterGapDifference"] = p.MacauVsAverage.WaterGapDifference
			}
			m.Phases = append(m.Phases, p)
		}
		result.Markets = append(result.Markets, m)
	}
	return result
}
