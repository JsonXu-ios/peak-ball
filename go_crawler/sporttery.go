package main

import (
	"encoding/json"
	"fmt"
	"math"
)

func fetchSportteryOdds(matchID string) ([]byte, error) {
	body, err := fetch(fmt.Sprintf("%s/match/jczq/sporttery/%s", BaseURL, matchID))
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf("%s returned no sporttery spf.odds records", matchID)
	}
	for _, row := range payload.Data.Spf.Odds {
		if row.Home > 0 && row.Draw > 0 && row.Away > 0 && !math.IsInf(row.Home+row.Draw+row.Away, 0) {
			return body, nil
		}
	}
	return nil, fmt.Errorf("%s returned no priced sporttery spf.odds records", matchID)
}

// Preserve previous captures when either endpoint fails, and never discard
// the independent opening-price history when only lr data is refreshed.
func mergeSportteryBodies(previous, trade, odds []byte) []byte {
	payload := map[string]json.RawMessage{}
	_ = json.Unmarshal(previous, &payload)
	if payload == nil {
		payload = map[string]json.RawMessage{}
	}
	var fresh map[string]json.RawMessage
	if json.Unmarshal(trade, &fresh) == nil {
		for key, raw := range fresh {
			payload[key] = raw
		}
	}
	if len(odds) > 0 && json.Valid(odds) {
		payload["sportteryOdds"] = json.RawMessage(odds)
	}
	if len(payload) == 0 {
		return nil
	}
	body, _ := json.Marshal(payload)
	return body
}
