package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"
)

func parseTime(tStr string) time.Time {
	layout := "2006-01-02 15:04:05"
	t, err := time.Parse(layout, tStr)
	if err != nil {
		layoutDate := "2006-01-02"
		t, err = time.Parse(layoutDate, tStr)
		if err != nil {
			return time.Time{}
		}
	}
	return t
}

func parseDate(tStr string) time.Time {
	layout := "2006-01-02"
	t, err := time.Parse(layout, tStr)
	if err != nil {
		return time.Time{}
	}
	return t
}

func toJSON(v interface{}) datatypes.JSON {
	b, _ := json.Marshal(v)
	return datatypes.JSON(b)
}

func calculateAverageEuroOdd(odds []EuroOdd) (EuroOdd, bool) {
	if len(odds) == 0 {
		return EuroOdd{}, false
	}

	sums := make([]float64, 3)
	counts := make([]int, 3)
	for _, odd := range odds {
		if len(odd.Odds) < 3 || odd.CompanyName == "平均欧赔" || odd.CompanyId == "" {
			continue
		}

		for i := 0; i < 3; i++ {
			value, err := strconv.ParseFloat(odd.Odds[i], 64)
			if err != nil || value <= 0 {
				continue
			}
			sums[i] += value
			counts[i]++
		}
	}

	for _, count := range counts {
		if count == 0 {
			return EuroOdd{}, false
		}
	}

	return EuroOdd{
		CompanyId:   "average",
		CompanyName: "平均欧赔",
		Odds: []string{
			fmt.Sprintf("%.2f", sums[0]/float64(counts[0])),
			fmt.Sprintf("%.2f", sums[1]/float64(counts[1])),
			fmt.Sprintf("%.2f", sums[2]/float64(counts[2])),
		},
	}, true
}

// ConvertMatchToMoney maps ALL fields from the API MatchModel to the DB Money struct.
func ConvertMatchToMoney(m MatchModel, dateStr string) Money {
	return Money{
		MatchId:             m.MatchId,
		Date:                parseDate(dateStr),
		League:              m.League,
		LeagueName:          m.LeagueName,
		LeagueId:            m.LeagueId,
		Home:                m.Home,
		Guest:               m.Guest,
		HomeTeamId:          m.HomeTeamId,
		GuestTeamId:         m.GuestTeamId,
		MatchTime:           parseTime(m.MatchTime),
		Status:              m.Status,
		MatchState:          m.MatchState,
		DisplayState:        m.DisplayState,
		Time:                m.Time,
		HomeScore:           m.HomeScore,
		GuestScore:          m.GuestScore,
		HomeHalfScore:       m.HomeHalfScore,
		GuestHalfScore:      m.GuestHalfScore,
		HomeOtScore:         m.HomeOtScore,
		GuestOtScore:        m.GuestOtScore,
		HomeOtPenalty:       m.HomeOtPenalty,
		GuestOtPenalty:      m.GuestOtPenalty,
		HomeCorner:          m.HomeCorner,
		GuestCorner:         m.GuestCorner,
		HomeRank:            optionalString(m.HomeRank),
		GuestRank:           optionalString(m.GuestRank),
		HomeLogo:            m.HomeLogo,
		GuestLogo:           m.GuestLogo,
		Season:              m.Season,
		Round:               m.Round,
		Groups:              m.Groups,
		ScheduleId:          m.ScheduleId,
		Hot:                 m.Hot,
		HasSignal:           m.HasSignal,
		HasHighlights:       m.HasHighlights,
		HasContent:          m.HasContent,
		Label:               m.Label,
		JingcaiID:           optionalString(m.JingcaiId),
		Description:         m.Description,
		OrderRecommendCount: m.OrderRecommendCount,
	}
}

func optionalString(value interface{}) string {
	if value == nil {
		return ""
	}
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || strings.EqualFold(trimmed, "<nil>") || strings.EqualFold(trimmed, "null") {
		return ""
	}
	return trimmed
}

// ConvertHistoryToHistoryMoney maps ALL sections from the history API response.
// Now includes LeagueSummary which was previously missing.
func ConvertHistoryToHistoryMoney(matchId string, dateStr string, h HistoryResponse) HistoryMoney {
	return HistoryMoney{
		MatchId:            matchId,
		Date:               parseDate(dateStr),
		LeagueStat:         toJSON(h.LeagueStat),
		AgainstSummary:     toJSON(h.Against.Summary),
		AgainstList:        toJSON(h.Against.List),
		RecentHomeSummary:  toJSON(h.Recent.Home.Summary),
		RecentHomeList:     toJSON(h.Recent.Home.List),
		RecentGuestSummary: toJSON(h.Recent.Guest.Summary),
		RecentGuestList:    toJSON(h.Recent.Guest.List),
		LeagueSummary:      toJSON(h.LeagueSummary),
		RankData:           toJSON(h.Rank),
		FutureHome:         toJSON(h.Future.Home),
		FutureGuest:        toJSON(h.Future.Guest),
	}
}

// ConvertOddsEuroToOddsMoney maps ALL fields from the euro odds API response.
// Now stores RiseAndFall data and Pinnacle odds, plus company count.
func ConvertOddsEuroToOddsMoney(matchId string, dateStr string, oe OddsEuroResponse, sportteryTradeBody []byte) OddsMoney {
	out := OddsMoney{
		MatchId:      matchId,
		Date:         parseDate(dateStr),
		Data:         toJSON(oe.Odds),
		RiseAndFall:  toJSON(oe.RiseAndFall),
		CompanyCount: len(oe.Odds),
	}
	if len(sportteryTradeBody) > 0 {
		out.SportteryTrade = datatypes.JSON(sportteryTradeBody)
	}

	for _, odd := range oe.Odds {
		switch odd.CompanyId {
		case "115":
			out.William = toJSON(odd)
		case "281":
			out.Bet365 = toJSON(odd)
		case "16":
			out.Pinnacle = toJSON(odd)
		}
		if odd.CompanyName == "平均欧赔" || odd.CompanyId == "" {
			out.AvgOdds = toJSON(odd)
		}
	}

	if len(out.AvgOdds) == 0 {
		avgOdd, ok := calculateAverageEuroOdd(oe.Odds)
		if ok {
			out.AvgOdds = toJSON(avgOdd)
		}
	}
	return out
}

// ConvertOddsPankouToPankouMoney maps ALL fields from the pankou API response.
// Now stores company counts for monitoring data completeness.
func ConvertOddsPankouToPankouMoney(matchId string, dateStr string, op OddsPankouResponse) PankouMoney {
	out := PankouMoney{
		MatchId:   matchId,
		Date:      parseDate(dateStr),
		AsiaData:  toJSON(op.Asia),
		DxqData:   toJSON(op.Dxq),
		AsiaCount: len(op.Asia),
		DxqCount:  len(op.Dxq),
	}

	for _, p := range op.Asia {
		if p.CompanyId == 8 {
			out.Bet365Asia = toJSON(p)
		}
	}
	for _, p := range op.Dxq {
		if p.CompanyId == 8 {
			out.Bet365Dxq = toJSON(p)
		}
	}
	return out
}
