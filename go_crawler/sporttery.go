package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func fetchSportteryBrowser(matchID string) ([]byte, error) {
	if ok, _ := regexp.MatchString(`^[0-9]{1,20}$`, matchID); !ok {
		return nil, fmt.Errorf("invalid match ID")
	}
	for _, candidate := range []string{"../scripts/sporttery-browser.cjs", "scripts/sporttery-browser.cjs"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			script, err := filepath.Abs(candidate)
			if err != nil {
				return nil, err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", script, "--match-id", matchID)
			cmd.WaitDelay = 5 * time.Second
			body, err := cmd.Output()
			if failure, ok := err.(*exec.ExitError); ok {
				detail := strings.TrimSpace(string(failure.Stderr))
				if len(detail) > 512 {
					detail = detail[:512]
				}
				return nil, fmt.Errorf("sporttery browser collection failed: %w %s", err, detail)
			}
			return body, err
		}
	}
	return nil, fmt.Errorf("sporttery browser collector script was not found")
}

func fetchSportteryOdds(matchID string) ([]byte, error) {
	body, err := fetch(fmt.Sprintf("%s/match/jczq/sporttery/%s", BaseURL, matchID))
	if err != nil && strings.Contains(err.Error(), "status code error: 403") {
		body, err = fetchSportteryBrowser(matchID)
	}
	if err != nil {
		return nil, err
	}
	if records, _ := sportteryPriceState(body); !records && !sportteryEmptyMarket(body) {
		return nil, fmt.Errorf("%s returned no complete sporttery spf.odds records", matchID)
	}
	return body, nil
}

func sportteryEmptyMarket(body []byte) bool {
	var payload struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Code == nil || *payload.Code != 0 || len(payload.Data) == 0 {
		return false
	}
	if strings.TrimSpace(string(payload.Data)) == "null" {
		return true
	}
	var data map[string]json.RawMessage
	if json.Unmarshal(payload.Data, &data) != nil {
		return false
	}
	if len(data) == 0 {
		return true
	}
	if raw, ok := data["spf"]; ok {
		var spf struct {
			Odds []json.RawMessage `json:"odds"`
		}
		return json.Unmarshal(raw, &spf) == nil && len(spf.Odds) == 0
	}
	return false
}

func sportteryPriceState(body []byte) (records, priced bool) {
	var payload struct {
		Code *int `json:"code"`
		Data struct {
			Spf struct {
				Odds []struct {
					Home *float64 `json:"spf3"`
					Draw *float64 `json:"spf1"`
					Away *float64 `json:"spf0"`
				} `json:"odds"`
			} `json:"spf"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &payload) != nil || payload.Code == nil || *payload.Code != 0 {
		return false, false
	}
	for _, row := range payload.Data.Spf.Odds {
		if row.Home != nil && row.Draw != nil && row.Away != nil && *row.Home >= 0 && *row.Draw >= 0 && *row.Away >= 0 && !math.IsInf(*row.Home+*row.Draw+*row.Away, 0) {
			records = true
			if *row.Home > 0 && *row.Draw > 0 && *row.Away > 0 {
				priced = true
			}
		}
	}
	return records, priced
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
	newRecords, newPriced := sportteryPriceState(odds)
	_, oldPriced := sportteryPriceState(payload["sportteryOdds"])
	if (newRecords || sportteryEmptyMarket(odds)) && (newPriced || !oldPriced) {
		payload["sportteryOdds"] = json.RawMessage(odds)
	}
	if len(payload) == 0 {
		return nil
	}
	body, _ := json.Marshal(payload)
	return body
}
