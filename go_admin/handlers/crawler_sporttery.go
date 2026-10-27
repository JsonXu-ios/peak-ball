package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"go_admin/database"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var sportteryBrowserMu sync.Mutex
var sportteryMatchIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)

func crawlerSportteryJSONUpdate(body string) clause.Expr {
	var patch map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &patch) == nil && len(patch) == 1 && len(patch["sportteryOdds"]) > 0 {
		// JSON_MERGE_PATCH removes JSON null members recursively. Set the raw
		// capture as one value so the provider's data:null remains intact.
		return gorm.Expr("JSON_SET(CASE WHEN JSON_TYPE(sporttery_trade) = 'OBJECT' THEN sporttery_trade ELSE JSON_OBJECT() END, '$.sportteryOdds', CAST(? AS JSON))", string(patch["sportteryOdds"]))
	}
	return gorm.Expr("JSON_MERGE_PATCH(CASE WHEN JSON_TYPE(sporttery_trade) = 'OBJECT' THEN sporttery_trade ELSE JSON_OBJECT() END, CAST(? AS JSON))", body)
}

func SportteryBrowserScript() (string, error) {
	directory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(directory, "scripts", "sporttery-browser.cjs")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Abs(candidate)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return "", fmt.Errorf("sporttery browser collector script was not found")
}

func fetchSportteryOddsBrowser(matchID string) ([]byte, error) {
	if !sportteryMatchIDPattern.MatchString(matchID) {
		return nil, fmt.Errorf("invalid match ID")
	}
	// At most one short-lived fallback browser at a time. Normal queries never
	// invoke this collector, and no project service is launched/restarted.
	sportteryBrowserMu.Lock()
	defer sportteryBrowserMu.Unlock()
	script, err := SportteryBrowserScript()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", script, "--match-id", matchID)
	cmd.WaitDelay = 5 * time.Second
	body, err := cmd.Output()
	if err != nil {
		detail := ""
		if failure, ok := err.(*exec.ExitError); ok {
			detail = strings.TrimSpace(string(failure.Stderr))
			if len(detail) > 512 {
				detail = detail[:512]
			}
		}
		return nil, fmt.Errorf("sporttery browser collection failed for %s: %w %s", matchID, err, detail)
	}
	return body, nil
}

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

func sportteryOddsJSONHasRecords(body []byte) bool {
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
		return false
	}
	for _, row := range payload.Data.Spf.Odds {
		if row.Home != nil && row.Draw != nil && row.Away != nil && *row.Home >= 0 && *row.Draw >= 0 && *row.Away >= 0 && !math.IsInf(*row.Home+*row.Draw+*row.Away, 0) {
			return true
		}
	}
	return false
}

func SportteryOddsCaptureState(body []byte) string {
	if !sportteryOddsJSONHasRecords(body) {
		var payload struct {
			Code *int            `json:"code"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(body, &payload) == nil && payload.Code != nil && *payload.Code == 0 && len(payload.Data) > 0 {
			if strings.TrimSpace(string(payload.Data)) == "null" {
				return "no_quotes"
			}
			var data map[string]json.RawMessage
			if json.Unmarshal(payload.Data, &data) != nil {
				return "invalid"
			}
			if len(data) == 0 {
				return "no_quotes"
			}
			if raw, ok := data["spf"]; ok {
				var spf struct {
					Odds []json.RawMessage `json:"odds"`
				}
				if json.Unmarshal(raw, &spf) == nil && len(spf.Odds) == 0 {
					return "no_quotes"
				}
			}
		}
		return "invalid"
	}
	if !sportteryOddsJSONHasData(body) {
		return "unopened"
	}
	return "priced"
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
	if err != nil && strings.Contains(err.Error(), "status 403") {
		body, err = fetchSportteryOddsBrowser(matchID)
	}
	if err != nil {
		return fmt.Errorf("failed to fetch sporttery opening/latest odds: %w", err)
	}
	return ImportSportteryOddsCapture(matchID, body)
}

// ImportSportteryOddsCapture is shared by normal collection and the offline
// missing-data backfill. It never creates or alters database schema.
func ImportSportteryOddsCapture(matchID string, body []byte) error {
	if !sportteryMatchIDPattern.MatchString(matchID) {
		return fmt.Errorf("invalid match ID")
	}
	if SportteryOddsCaptureState(body) == "invalid" {
		return fmt.Errorf("sporttery odds has no complete spf.odds records for %s", matchID)
	}
	// Keep genuine unopened captures, but never erase an already recorded
	// opening price with a later response containing only zero quotes.
	if !sportteryOddsJSONHasData(body) && hasExistingCrawlerSportteryOdds(matchID) {
		return nil
	}
	patch, err := json.Marshal(map[string]json.RawMessage{"sportteryOdds": body})
	if err != nil {
		return err
	}
	return upsertCrawlerJSON("odds_moneys", "sporttery_trade", matchID, string(patch))
}
