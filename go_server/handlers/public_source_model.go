package handlers

// A pure adapter for the independent SQLite collector. The legacy structures
// below are in-memory model contracts, never table reads or VIPC identities.
import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go_server/models"
	"gorm.io/datatypes"
)

const PublicSourceModelVersion = "public-source-model-v2"

type PublicSourceModelInput struct {
	Match struct {
		ID         string `json:"id"`
		Date       string `json:"business_date"`
		Home       string `json:"home"`
		Away       string `json:"away"`
		Kickoff    string `json:"kickoff"`
		Lottery    string `json:"lottery_number"`
		ZgzcwID    string `json:"zgzcw_id"`
		SourceHome string `json:"source_home"`
		SourceAway string `json:"source_away"`
	} `json:"match"`
	Parts struct {
		Euro    []publicSourceQuote `json:"euro"`
		Asian   []publicSourceQuote `json:"asian"`
		Totals  []publicSourceQuote `json:"totals"`
		History struct {
			Tables [][][]string `json:"tables"`
		} `json:"history"`
		Bonus struct {
			Source        string                              `json:"source"`
			SourceMatchID string                              `json:"sourceMatchId"`
			CPMatchID     string                              `json:"cpMatchId"`
			ZgzcwMatchID  string                              `json:"zgzcwMatchId"`
			Home          string                              `json:"home"`
			Away          string                              `json:"away"`
			History       map[string][]map[string]interface{} `json:"history"`
			DrawScores    map[string]*float64                 `json:"drawScores"`
			TotalGoals    []*float64                          `json:"totalGoals"`
		} `json:"bonus"`
	} `json:"parts"`
	Provenance map[string]struct {
		Received string `json:"received_at"`
		ID       int64  `json:"id"`
	} `json:"provenance"`
}

type publicSourceQuote struct {
	Label              string               `json:"sourceLabel"`
	Company            string               `json:"company"`
	Opening            []string             `json:"opening"`
	Latest             []string             `json:"latest"`
	Kelly              []string             `json:"kelly"`
	OpeningKelly       []string             `json:"openingKelly"`
	OpeningKellyPrices []string             `json:"openingKellyPrices"`
	OpeningKellyTime   string               `json:"openingKellyTime"`
	FullQuoteHistory   [][]string           `json:"fullQuoteHistory"`
	Source             string               `json:"source"`
	SourceCompanyID    string               `json:"sourceCompanyId"`
	SourceMatchID      string               `json:"sourceMatchId"`
	CPExtras           *publicSourceCPQuote `json:"cpExtras"`
}

type PublicSourceModelResult struct {
	Version       string                     `json:"version"`
	State         string                     `json:"state"`
	Reason        string                     `json:"reason,omitempty"`
	Observation   *drawObservationItem       `json:"observation,omitempty"`
	Warnings      []string                   `json:"warnings"`
	Sources       map[string]int64           `json:"snapshotIds"`
	MarketContext *publicSourceMarketContext `json:"marketContext,omitempty"`
	InputSources  []publicSourceEuroInput    `json:"inputSources,omitempty"`
}

func publicSourceJSON(value interface{}) datatypes.JSON { raw, _ := json.Marshal(value); return raw }

func publicSourceCompany(q publicSourceQuote) string {
	name := q.Company
	if name == "" || name == q.Label {
		name = q.Label
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "36*", "bet365", "bet 365":
		return "bet365"
	case "威*", "威廉", "威廉希尔", "william", "william hill":
		return "william"
	case "平*", "平博", "pinnacle":
		return "pinnacle"
	case "平均欧赔", "平均*", "平均值":
		return "average"
	case "官方(胜平负)", "竞彩", "sporttery":
		return "sporttery"
	}
	return ""
}

func publicSourceValidTriple(values []string) bool {
	if len(values) != 3 {
		return false
	}
	for _, raw := range values {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || v <= 1 || math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Translate shorthand lines before feeding the existing Chinese-line parser.
func publicSourceLine(raw string, goals bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if goals {
		raw = strings.TrimSuffix(raw, "球")
		parts := strings.Split(raw, "/")
		total := 0.0
		for _, part := range parts {
			n, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
			if err != nil {
				return ""
			}
			total += n
		}
		return strconv.FormatFloat(total/float64(len(parts)), 'f', -1, 64)
	}
	// Keep existing full words intact; normalize only published abbreviations.
	raw = strings.NewReplacer("半/一", "半球/一球", "一/球半", "一球/球半", "球半/两", "球半/两球", "两/两半", "两球/两球半", "两半/三", "两球半/三球").Replace(raw)
	if raw == "半" || raw == "受半" {
		raw += "球"
	}
	if raw == "一" || raw == "受一" || raw == "两" || raw == "受两" {
		raw += "球"
	}
	return raw
}

func publicSourceMarkets(input PublicSourceModelInput, match models.Money) (models.OddsMoney, models.PankouMoney) {
	odds := models.OddsMoney{MatchId: match.MatchId}
	lines := models.PankouMoney{MatchId: match.MatchId}
	euros := []analysisEuroOdd{}
	counts := map[string]int{}
	for _, q := range input.Parts.Euro {
		counts[publicSourceCompany(q)]++
	}
	for _, q := range input.Parts.Euro {
		key := publicSourceCompany(q)
		if key == "sporttery" || key == "" || counts[key] != 1 {
			continue
		}
		q, _ = publicSourceSelectEuro(input, q, match.MatchTime)
		id := map[string]string{"bet365": "281", "william": "115", "pinnacle": "16", "average": "0"}[key]
		row := analysisEuroOdd{CompanyID: id, CompanyName: key, Odds: q.Latest, FirstOdds: q.Opening, Kelly: json.RawMessage(publicSourceJSON(q.Kelly)), FirstKelly: json.RawMessage(publicSourceJSON(q.OpeningKelly))}
		euros = append(euros, row)
		switch key {
		case "average":
			odds.AvgOdds = publicSourceJSON(row)
		case "william":
			odds.William = publicSourceJSON(row)
		case "bet365":
			odds.Bet365 = publicSourceJSON(row)
		case "pinnacle":
			odds.Pinnacle = publicSourceJSON(row)
		}
	}
	odds.Data = publicSourceJSON(euros)
	for _, market := range []struct {
		quotes []publicSourceQuote
		goals  bool
	}{{input.Parts.Asian, false}, {input.Parts.Totals, true}} {
		rows := []analysisPankouItem{}
		seen := map[string]int{}
		for _, q := range market.quotes {
			seen[publicSourceCompany(q)]++
		}
		for _, q := range market.quotes {
			key := publicSourceCompany(q)
			if (key != "bet365" && key != "william") || seen[key] != 1 || len(q.Opening) != 3 || len(q.Latest) != 3 {
				continue
			}
			id := 8
			if key == "william" {
				id = 9
			}
			row := analysisPankouItem{CompanyID: id, CompanyName: key, FirstOdds: []string{q.Opening[0], q.Opening[2]}, Odds: []string{q.Latest[0], q.Latest[2]}, FirstPankou: publicSourceLine(q.Opening[1], market.goals), Pankou: publicSourceLine(q.Latest[1], market.goals)}
			rows = append(rows, row)
			if key == "bet365" {
				if market.goals {
					lines.Bet365Dxq = publicSourceJSON(row)
				} else {
					lines.Bet365Asia = publicSourceJSON(row)
				}
			}
		}
		if market.goals {
			lines.DxqData = publicSourceJSON(rows)
		} else {
			lines.AsiaData = publicSourceJSON(rows)
		}
	}
	// Build only the recorded official quotes used by the unchanged functions.
	spf := []map[string]interface{}{}
	for _, q := range input.Parts.Bonus.History["hadList"] {
		timestamp := fmt.Sprint(q["updateDate"]) + " " + fmt.Sprint(q["updateTime"])
		at, err := time.ParseInLocation("2006-01-02 15:04:05", timestamp, drawSourceLocation)
		if err != nil || !at.Before(match.MatchTime) {
			continue
		}
		spf = append(spf, map[string]interface{}{"spf3": q["h"], "spf1": q["d"], "spf0": q["a"], "updateTime": timestamp})
	}
	sort.SliceStable(spf, func(i, j int) bool { return fmt.Sprint(spf[i]["updateTime"]) < fmt.Sprint(spf[j]["updateTime"]) })
	bf := map[string]interface{}{"stop": false}
	for _, s := range []string{"0:0", "1:1", "2:2", "3:3"} {
		if p := input.Parts.Bonus.DrawScores[s]; p != nil {
			bf["sd"+strings.ReplaceAll(s, ":", "")] = *p
		}
	}
	jq := map[string]interface{}{"stop": false}
	for i, p := range input.Parts.Bonus.TotalGoals {
		if p != nil && i < 8 {
			jq[fmt.Sprintf("t%d", i)] = *p
		}
	}
	// stop=false describes this verified pre-kickoff capture under the user's
	// local kickoff rule, not a claimed source-provided official sale status.
	odds.SportteryTrade = publicSourceJSON(map[string]interface{}{"sportteryOdds": map[string]interface{}{"code": 0, "data": map[string]interface{}{"spf": map[string]interface{}{"odds": spf}, "bf": bf, "jq": jq}}})
	if p, ok := input.Provenance["euro"]; ok {
		if at, e := time.Parse(time.RFC3339Nano, p.Received); e == nil {
			odds.CapturedAt = &at
		}
	}
	if p, ok := input.Provenance["totals"]; ok {
		if at, e := time.Parse(time.RFC3339Nano, p.Received); e == nil {
			lines.CapturedAt = &at
		}
	}
	return odds, lines
}

var publicSourceScorePattern = regexp.MustCompile(`^(\d+):(\d+)$`)

func publicSourceHistory(table [][]string, target, opponent string, kickoff time.Time) drawObservationRecent {
	window := drawObservationRecent{Matches: []drawObservationRecentMatch{}}
	seen := map[string]bool{}
	for _, r := range table {
		if len(r) < 7 || (strings.TrimSpace(r[3]) != target && strings.TrimSpace(r[5]) != target) {
			continue
		}
		home, away := strings.TrimSpace(r[3]), strings.TrimSpace(r[5])
		if opponent != "" && !((home == target && away == opponent) || (away == target && home == opponent)) {
			continue
		}
		date := strings.TrimSpace(r[2])
		var at time.Time
		for _, layout := range []string{"06-1-2", "2006-1-2"} {
			if parsed, err := time.ParseInLocation(layout, date, drawSourceLocation); err == nil {
				at = parsed
				break
			}
		}
		// These tables only expose calendar dates; omit same-day records whose
		// completion time cannot be established. Never include the target row.
		if at.IsZero() || !at.Before(time.Date(kickoff.Year(), kickoff.Month(), kickoff.Day(), 0, 0, 0, 0, drawSourceLocation)) {
			continue
		}
		scores := publicSourceScorePattern.FindStringSubmatch(strings.TrimSpace(r[4]))
		if len(scores) != 3 {
			continue
		}
		h, _ := strconv.Atoi(scores[1])
		a, _ := strconv.Atoi(scores[2])
		if h > 50 || a > 50 {
			continue
		}
		identity := fmt.Sprintf("%s|%s|%s", at.Format("2006-01-02"), home, away)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		sum := sha256.Sum256([]byte(identity))
		id := fmt.Sprintf("zgzcw-history:%x", sum[:12])
		venue, result := "home", "draw"
		gf, ga := h, a
		if away == target {
			venue = "away"
			gf, ga = a, h
		}
		if gf > ga {
			result = "win"
		} else if gf < ga {
			result = "loss"
		}
		window.Matches = append(window.Matches, drawObservationRecentMatch{MatchID: id, Date: at.Format("2006-01-02"), MatchTime: at, League: r[0], Home: home, Guest: away, HomeScore: h, AwayScore: a, Venue: venue, Result: result})
	}
	sort.SliceStable(window.Matches, func(i, j int) bool { return window.Matches[i].MatchTime.After(window.Matches[j].MatchTime) })
	if len(window.Matches) > 10 {
		window.Matches = window.Matches[:10]
	}
	window.Sample = len(window.Matches)
	for _, m := range window.Matches {
		if m.Result == "draw" {
			window.Draws++
		}
	}
	return window
}

func publicSourceHistoryModel(match models.Money, home, away, h2h drawObservationRecent) models.HistoryMoney {
	history := models.HistoryMoney{MatchId: match.MatchId}
	convert := func(window drawObservationRecent) (datatypes.JSON, datatypes.JSON) {
		rows := []analysisHistoryMatch{}
		summary := analysisHistorySummary{}
		for _, m := range window.Matches {
			rows = append(rows, analysisHistoryMatch{MatchTime: m.MatchTime.Format(time.RFC3339), Home: m.Home, Guest: m.Guest, Goal: []int{m.HomeScore, m.AwayScore}, League: m.League})
			summary.All++
			gf, ga := m.HomeScore, m.AwayScore
			if m.Venue == "away" {
				gf, ga = ga, gf
			}
			summary.WinGoal += gf
			summary.LoseGoal += ga
			switch m.Result {
			case "win":
				summary.Win++
			case "loss":
				summary.Lose++
			case "draw":
				summary.Draw++
			}
		}
		return publicSourceJSON(rows), publicSourceJSON(summary)
	}
	history.RecentHomeList, history.RecentHomeSummary = convert(home)
	history.RecentGuestList, history.RecentGuestSummary = convert(away)
	history.AgainstList, history.AgainstSummary = convert(h2h)
	return history
}

// AnalyzePublicSourceModel reuses the production pure functions without DB,
// Redis, HTTP, research-store lookups, model retraining or service startup.
func AnalyzePublicSourceModel(input PublicSourceModelInput) PublicSourceModelResult {
	r := PublicSourceModelResult{Version: PublicSourceModelVersion, State: "unavailable", Warnings: []string{}, Sources: map[string]int64{}}
	kickoff, err := time.ParseInLocation("2006-01-02 15:04:05", input.Match.Kickoff, drawSourceLocation)
	if err != nil || input.Match.ID == "" || input.Match.Home == "" || input.Match.Away == "" {
		r.Reason = "比赛身份或开赛时间不完整"
		return r
	}
	// Defense in depth: reject a caller passing post-match or unknown-time
	// payloads even if the Python side forgot its pre-kickoff projection.
	for kind, source := range input.Provenance {
		at, e := time.Parse(time.RFC3339Nano, source.Received)
		if e != nil || !at.Before(kickoff) {
			r.Reason = "模型只接受可核对时间的赛前快照"
			return r
		}
		r.Sources[kind] = source.ID
	}
	if len(input.Provenance) == 0 {
		r.Reason = "缺少赛前快照时间证据"
		return r
	}
	match := models.Money{MatchId: input.Match.ID, Home: input.Match.Home, Guest: input.Match.Away, MatchTime: kickoff, JingcaiID: input.Match.Lottery}
	if date, e := time.ParseInLocation("2006-01-02", input.Match.Date, drawSourceLocation); e == nil {
		match.Date = date
	}
	window := func(n int, target, opponent string) drawObservationRecent {
		if len(input.Parts.History.Tables) <= n {
			return drawObservationRecent{Matches: []drawObservationRecentMatch{}}
		}
		return publicSourceHistory(input.Parts.History.Tables[n], target, opponent, kickoff)
	}
	home, away, h2h := window(0, match.Home, ""), window(1, match.Guest, ""), window(2, match.Home, match.Guest)
	history := publicSourceHistoryModel(match, home, away, h2h)
	if p, ok := input.Provenance["history"]; ok {
		at, _ := time.Parse(time.RFC3339Nano, p.Received)
		history.CapturedAt = &at
	}
	odds, lines := publicSourceMarkets(input, match)
	r.MarketContext = publicSourceBuildMarketContext(input)
	r.InputSources, r.Warnings = publicSourceEuroSources(input, kickoff)
	enrichment := buildDrawObservationFrontend(match, history, odds, lines)
	item := drawObservationItem{MatchID: match.MatchId, JingcaiID: match.JingcaiID, Date: input.Match.Date, MatchTime: kickoff, Home: match.Home, Guest: match.Guest, HomeScore: -1, AwayScore: -1, DisplayState: "未核对赛果", HomeRecent: home, AwayRecent: away, HeadToHead: h2h, FrontendAnalysis: enrichment.analysis, CompanyQuotes: enrichment.companies, EuroOdds: enrichment.euro, Sources: []string{"public_sources_sqlite"}, Warnings: []string{}}
	for _, q := range item.CompanyQuotes {
		if q.Key == "bet365" {
			item.AsianHandicap = q.AsianHandicap
			item.GoalsLine = q.GoalsLine
		}
	}
	study := drawFeatureStudyBuildRowWithEnrichment(match, history, odds, lines, enrichment)
	item.FrozenDrawSignal = drawObservationApplySelectionEligibility(item, drawObservationModelSignal(item, &study))
	secondary := drawObservationSecondaryDecision(item)
	item.SecondaryDecision = &secondary
	item.ScoreInputs = drawObservationBuildScoreInputs(item.MatchID, odds)
	scores := drawObservationScoreRecommendation(item)
	item.ScoreRecommendation = &scores
	item.TotalsPrediction = drawObservationTotalsSignal(item, enrichment.totalsInputs)
	// Keep outcome annotation pending: no target results enter this adapter.
	settled := false
	item.RegulationSettled = &settled
	r.State = "ready"
	r.Observation = &item
	if home.Sample < 5 || away.Sample < 5 {
		r.Warnings = append(r.Warnings, "部分球队不足5场安全赛前战绩，大小球可能暂不可用")
	}
	if h2h.Sample == 0 {
		r.Warnings = append(r.Warnings, "无有效赛前交锋，不能入选平")
	}
	return r
}
