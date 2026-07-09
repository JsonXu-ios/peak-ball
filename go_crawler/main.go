package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

const (
	BaseURL    = "https://vipc.cn/i"
	UserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.36"
	DSN        = "root:123456@tcp(127.0.0.1:3306)/football_data?charset=utf8mb4&parseTime=True&loc=Local"
	ImgBaseDir = "../public/footballimg"
)

func downloadImage(url string) (string, error) {
	if url == "" {
		return "", nil
	}
	// Extract filename from URL
	parts := strings.Split(url, "/")
	filename := parts[len(parts)-1]
	if filename == "" {
		return "", nil
	}

	localPath := filepath.Join(ImgBaseDir, filename)

	// Skip if already exists
	if _, err := os.Stat(localPath); err == nil {
		return filename, nil
	}

	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(localPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return filename, err
}

func generateRandomIP() string {
	return fmt.Sprintf("%d.%d.%d.%d", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

func fetch(url string) ([]byte, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://www.vipc.cn/live/football")
	req.Header.Set("Origin", "https://www.vipc.cn")
	req.Header.Set("X-Forwarded-For", generateRandomIP())

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status code error: %d %s", resp.StatusCode, resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func fetchSportteryTrade(matchID string, jingcaiID string) ([]byte, error) {
	var lastErr error
	seen := map[string]bool{}
	for _, tradeID := range []string{matchID, jingcaiID} {
		tradeID = strings.TrimSpace(tradeID)
		if tradeID == "" || seen[tradeID] {
			continue
		}
		seen[tradeID] = true

		tradeURL := fmt.Sprintf("%s/match/jczq/lr/%s", BaseURL, tradeID)
		body, err := fetch(tradeURL)
		if err != nil {
			lastErr = err
			continue
		}
		if sportteryTradeJSONHasData(body) {
			return body, nil
		}
		lastErr = fmt.Errorf("%s returned empty sporttery trade data", tradeID)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("empty match_id and jingcai_id")
}

func sportteryTradeJSONHasData(body []byte) bool {
	var payload struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && sportteryTradeMapHasData(payload.Data) {
		return true
	}

	var direct map[string]json.RawMessage
	if err := json.Unmarshal(body, &direct); err == nil && sportteryTradeMapHasData(direct) {
		return true
	}

	var text string
	if err := json.Unmarshal(body, &text); err == nil {
		return sportteryTradeJSONHasData([]byte(text))
	}
	return false
}

func sportteryTradeMapHasData(values map[string]json.RawMessage) bool {
	for _, key := range []string{"tzbl", "jyykSpf", "jyykRqspf"} {
		raw, ok := values[key]
		if !ok {
			continue
		}
		text := strings.TrimSpace(string(raw))
		if text != "" && text != "null" && text != "{}" {
			return true
		}
	}
	return false
}

func main() {
	rand.Seed(time.Now().UnixNano())

	// Init DB
	var useDB bool
	dbErr := InitDB(DSN)
	if dbErr != nil {
		fmt.Printf("Error: Database initialization failed: %v\nCannot proceed without database as file output is disabled.\n", dbErr)
		return
	}
	fmt.Println("Database initialized successfully.")
	useDB = true

	// Ensure img dir exists
	if _, err := os.Stat(ImgBaseDir); os.IsNotExist(err) {
		os.MkdirAll(ImgBaseDir, 0755)
	}

	// Target strictly "today" via specific endpoint
	targetDate := time.Now().Format("2006-01-02")
	fmt.Printf("\n=== Processing Data for Today (%s) ===\n", targetDate)

	listURL := fmt.Sprintf("%s/live/football/date/today/next", BaseURL)
	body, err := fetch(listURL)
	if err != nil {
		fmt.Printf("Error fetching match list (today/next): %v\n", err)
		return
	}

	var listResp MatchListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		fmt.Printf("Error unmarshaling match list: %v\n", err)
		return
	}

	totalMatches := 0
	for _, item := range listResp.Items {
		totalMatches += len(item.Matches)
	}
	fmt.Printf("Parsed List. Found %d items groups, total %d matches.\n", len(listResp.Items), totalMatches)

	// Iterate matches
	for _, item := range listResp.Items {
		for _, match := range item.Matches {
			matchId := match.Model.MatchId
			if matchId == "" {
				continue
			}

			fmt.Printf("--- Processing Match %s (%s vs %s) ---\n", matchId, match.Model.Home, match.Model.Guest)

			// Download Logos
			homeImg, _ := downloadImage(match.Model.HomeLogo)
			guestImg, _ := downloadImage(match.Model.GuestLogo)
			if homeImg != "" {
				match.Model.HomeLogo = "/footballimg/" + homeImg
			}
			if guestImg != "" {
				match.Model.GuestLogo = "/footballimg/" + guestImg
			}

			// Save Match to DB
			if useDB {
				money := ConvertMatchToMoney(match.Model, targetDate)
				if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&money).Error; err != nil {
					fmt.Printf("  Error saving Match to DB: %v\n", err)
				}
			}

			// History
			historyURL := fmt.Sprintf("%s/match/football/%s/history", BaseURL, matchId)
			hBody, err := fetch(historyURL)
			if err == nil {
				var hResp HistoryResponse
				if err := json.Unmarshal(hBody, &hResp); err != nil {
					fmt.Printf("  Error parsing history: %v\n", err)
				} else {
					if useDB {
						hm := ConvertHistoryToHistoryMoney(matchId, targetDate, hResp)
						if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&hm).Error; err != nil {
							fmt.Printf("  Error saving History to DB: %v\n", err)
						}
					}
				}
			}

			// Odds Euro
			oddsEuroURL := fmt.Sprintf("%s/match/football/%s/odds/euro", BaseURL, matchId)
			oeBody, err := fetch(oddsEuroURL)
			if err == nil {
				var oeResp OddsEuroResponse
				if err := json.Unmarshal(oeBody, &oeResp); err != nil {
					fmt.Printf("  Error parsing odds euro: %v\n", err)
				} else {
					if useDB {
						sportteryTradeBody, err := fetchSportteryTrade(matchId, optionalString(match.Model.JingcaiId))
						if err != nil {
							fmt.Printf("  Error fetching sporttery trade: %v\n", err)
						}
						om := ConvertOddsEuroToOddsMoney(matchId, targetDate, oeResp, sportteryTradeBody)
						if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&om).Error; err != nil {
							fmt.Printf("  Error saving Odds Euro to DB: %v\n", err)
						}
					}
				}
			}

			// Odds Pankou
			oddsPankouURL := fmt.Sprintf("%s/match/football/%s/odds/pankou", BaseURL, matchId)
			opBody, err := fetch(oddsPankouURL)
			if err == nil {
				var opResp OddsPankouResponse
				if err := json.Unmarshal(opBody, &opResp); err != nil {
					fmt.Printf("  Error parsing odds pankou: %v\n", err)
				} else {
					if useDB {
						pm := ConvertOddsPankouToPankouMoney(matchId, targetDate, opResp)
						if err := DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&pm).Error; err != nil {
							fmt.Printf("  Error saving Odds Pankou to DB: %v\n", err)
						}
					}
				}
			}

			// Delay at least 1 second as requested
			time.Sleep(1500 * time.Millisecond)
		}
	}

	fmt.Println("All matches processed.")
}
