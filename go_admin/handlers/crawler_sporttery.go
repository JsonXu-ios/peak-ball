package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"go_admin/database"
)

func sportteryOddsJSONHasData(body []byte) bool {
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Spf struct {
				Odds []struct {
					Home float64 `json:"spf3"`
					Draw float64 `json:"spf1"`
					Away float64 `json:"spf0"`
				} `json:"odds"`
			} `json:"spf"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Code != 0 || len(payload.Data.Spf.Odds) == 0 {
		return false
	}
	for _, row := range payload.Data.Spf.Odds {
		if row.Home > 0 && row.Draw > 0 && row.Away > 0 && !math.IsInf(row.Home+row.Draw+row.Away, 0) {
			return true
		}
	}
	return false
}

func hasExistingCrawlerSportteryOdds(matchID string) bool {
	var raw sql.NullString
	if err := database.DB.Table("odds_moneys").Select("sporttery_trade").Where("match_id = ?", matchID).Limit(1).Scan(&raw).Error; err != nil || !raw.Valid {
		return false
	}
	var payload map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw.String), &payload)
	return sportteryOddsJSONHasData(payload["sportteryOdds"])
}

func crawlSportteryOdds(matchID string) error {
	// This URL takes the site's match ID, not the ccId or the different provider
	// matchId contained inside the response.
	body, err := makeRequest(fmt.Sprintf("%s/match/jczq/sporttery/%s", vipcAPIBaseURL, matchID))
	if err != nil {
		return fmt.Errorf("failed to fetch sporttery opening/latest odds: %w", err)
	}
	if !sportteryOddsJSONHasData(body) {
		return fmt.Errorf("sporttery odds has no valid spf.odds records for %s", matchID)
	}
	patch, err := json.Marshal(map[string]json.RawMessage{"sportteryOdds": body})
	if err != nil {
		return err
	}
	return upsertCrawlerJSON("odds_moneys", "sporttery_trade", matchID, string(patch))
}
