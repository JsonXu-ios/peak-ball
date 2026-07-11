package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go_admin/config"
	"go_admin/database"
	"go_admin/models"

	"github.com/gin-gonic/gin"
)

var runningCrawls sync.Map

type crawlRequest struct {
	Type     string
	Date     string
	MatchID  string
	Force    bool
	TaskID   uint
	TaskName string
}

type crawlProgress struct {
	RunKey       string   `json:"run_key"`
	Type         string   `json:"type"`
	Date         string   `json:"date,omitempty"`
	MatchID      string   `json:"match_id,omitempty"`
	Force        bool     `json:"force"`
	Current      string   `json:"current,omitempty"`
	ItemsCount   int      `json:"items_count"`
	SuccessCount int      `json:"success_count"`
	FailedCount  int      `json:"failed_count"`
	SkippedCount int      `json:"skipped_count"`
	Notes        []string `json:"notes,omitempty"`
}

type crawlContext struct {
	logID    uint
	request  crawlRequest
	progress crawlProgress
}

// GetCrawlerMatches returns paginated crawler match data
func GetCrawlerMatches(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")
	date := c.Query("date")
	league := c.Query("league")
	status := c.Query("status")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	query := database.DB.Model(&models.Money{})

	if keyword != "" {
		query = query.Where("match_id LIKE ? OR home LIKE ? OR guest LIKE ? OR league LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	if date != "" {
		query = query.Where("date = ?", date)
	}
	if league != "" {
		query = query.Where("league = ?", league)
	}
	if status != "" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var matches []models.Money
	if err := query.Offset(offset).Limit(pageSize).Order("date DESC, id DESC").Find(&matches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch matches"})
		return
	}

	// Get distinct leagues for filter
	var leagues []string
	database.DB.Model(&models.Money{}).Distinct("league").Where("league != ''").Pluck("league", &leagues)

	// Get distinct dates for filter
	var dates []string
	database.DB.Model(&models.Money{}).Distinct("date").Order("date DESC").Limit(30).Pluck("date", &dates)

	c.JSON(http.StatusOK, gin.H{
		"list":      matches,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"leagues":   leagues,
		"dates":     dates,
	})
}

// GetCrawlerMatchDetail returns detailed data for a single match
func GetCrawlerMatchDetail(c *gin.Context) {
	matchID := c.Param("id")

	match := loadCrawlerRecord("moneys", matchID)
	history := loadCrawlerRecord("history_moneys", matchID)
	odds := loadCrawlerRecord("odds_moneys", matchID)
	pankou := loadCrawlerRecord("pankou_moneys", matchID)
	if len(match) == 0 && len(history) == 0 && len(odds) == 0 && len(pankou) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Match not found"})
		return
	}
	if len(match) == 0 {
		match = map[string]interface{}{
			"match_id": matchID,
			"date":     crawlerMatchDate(matchID),
			"league":   "详情数据",
			"home":     fmt.Sprintf("比赛 %s", matchID),
			"status":   "detail_only",
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"match":   match,
		"history": history,
		"odds":    odds,
		"pankou":  pankou,
	})
}

// DeleteCrawlerMatch deletes crawler match data
func DeleteCrawlerMatch(c *gin.Context) {
	matchID := c.Param("id")

	database.DB.Where("match_id = ?", matchID).Delete(&models.Money{})
	database.DB.Where("match_id = ?", matchID).Delete(&models.HistoryMoney{})
	database.DB.Where("match_id = ?", matchID).Delete(&models.OddsMoney{})
	database.DB.Where("match_id = ?", matchID).Delete(&models.PankouMoney{})

	c.JSON(http.StatusOK, gin.H{"message": "Match data deleted successfully"})
}

// SyncCrawlerData triggers sync of crawler data (calls the crawler)
func SyncCrawlerData(c *gin.Context) {
	var req struct {
		Type    string `json:"type" binding:"required"` // match_list, history, rank, odds_euro, odds_pankou, all
		Date    string `json:"date"`                    // optional for match_list
		MatchID string `json:"match_id"`                // optional for history/rank/odds
		Async   bool   `json:"async"`                   // sync or async execution
		Force   bool   `json:"force"`                   // force recrawling existing detail rows
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	crawlReq := crawlRequest{Type: req.Type, Date: req.Date, MatchID: req.MatchID, Force: req.Force}
	result, err := startCrawl(crawlReq, req.Async)
	if err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "already running") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	if req.Async {
		c.JSON(http.StatusOK, gin.H{
			"message": "Crawler task started asynchronously",
			"result":  result,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Crawler sync completed",
		"result":  result,
	})
}

func startCrawl(req crawlRequest, async bool) (map[string]interface{}, error) {
	req.Type = strings.TrimSpace(req.Type)
	req.Date = strings.TrimSpace(req.Date)
	req.MatchID = strings.TrimSpace(req.MatchID)
	if req.TaskName == "" {
		req.TaskName = req.Type
	}

	runKey := buildRunKey(req)
	if value, loaded := runningCrawls.LoadOrStore(runKey, time.Now()); loaded {
		return nil, fmt.Errorf("crawler already running: %s since %v", runKey, value)
	}

	logDetails := marshalProgress(crawlProgress{
		RunKey:  runKey,
		Type:    req.Type,
		Date:    req.Date,
		MatchID: req.MatchID,
		Force:   req.Force,
		Notes:   []string{"task queued"},
	})
	crawlerLog := models.CrawlerLog{
		TaskID:    req.TaskID,
		TaskName:  req.TaskName,
		Status:    "running",
		StartTime: time.Now(),
		Details:   logDetails,
	}
	if err := database.DB.Create(&crawlerLog).Error; err != nil {
		runningCrawls.Delete(runKey)
		return nil, fmt.Errorf("failed to create crawler log: %w", err)
	}

	if req.TaskID > 0 {
		now := time.Now()
		database.DB.Model(&models.CrawlerTask{}).Where("id = ?", req.TaskID).Updates(map[string]interface{}{
			"status":      "running",
			"last_run_at": &now,
		})
	}

	result := map[string]interface{}{
		"log_id":     crawlerLog.ID,
		"run_key":    runKey,
		"type":       req.Type,
		"start_time": crawlerLog.StartTime,
		"status":     "running",
	}

	if async {
		go func() {
			defer runningCrawls.Delete(runKey)
			_, _ = executeCrawlWithLog(crawlerLog.ID, req, runKey)
		}()
		return result, nil
	}

	defer runningCrawls.Delete(runKey)
	return executeCrawlWithLog(crawlerLog.ID, req, runKey)
}

func executeCrawlWithLog(logID uint, req crawlRequest, runKey string) (map[string]interface{}, error) {
	ctx := &crawlContext{
		logID:   logID,
		request: req,
		progress: crawlProgress{
			RunKey:  runKey,
			Type:    req.Type,
			Date:    req.Date,
			MatchID: req.MatchID,
			Force:   req.Force,
			Notes:   []string{"task started"},
		},
	}
	ctx.save("running", "")

	var err error
	switch req.Type {
	case "match_list":
		err = crawlMatchList(ctx, req.Date)
	case "history":
		if req.MatchID != "" {
			err = crawlHistory(ctx, req.MatchID)
		} else {
			err = crawlDetailsForDate(ctx, req.Date, crawlHistory)
		}
	case "rank":
		if req.MatchID != "" {
			err = crawlRank(ctx, req.MatchID)
		} else {
			err = crawlDetailsForDate(ctx, req.Date, crawlRank)
		}
	case "odds_euro":
		if req.MatchID != "" {
			err = crawlOddsEuro(ctx, req.MatchID)
		} else {
			err = crawlDetailsForDate(ctx, req.Date, crawlOddsEuro)
		}
	case "odds_pankou":
		if req.MatchID != "" {
			err = crawlOddsPankou(ctx, req.MatchID)
		} else {
			err = crawlDetailsForDate(ctx, req.Date, crawlOddsPankou)
		}
	case "odds_refresh":
		err = crawlOddsRefresh(ctx, req.Date)
	case "all":
		err = crawlAll(ctx, req.Date)
	default:
		err = fmt.Errorf("unknown crawl type: %s", req.Type)
	}

	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
		ctx.note(errMsg)
	}
	ctx.save(status, errMsg)
	updateCrawlerTaskAfterRun(req.TaskID, status)

	result := map[string]interface{}{
		"log_id":        logID,
		"run_key":       runKey,
		"type":          req.Type,
		"status":        status,
		"items_count":   ctx.progress.ItemsCount,
		"success_count": ctx.progress.SuccessCount,
		"failed_count":  ctx.progress.FailedCount,
		"skipped_count": ctx.progress.SkippedCount,
	}
	return result, err
}

func buildRunKey(req crawlRequest) string {
	parts := []string{req.Type}
	if req.Date != "" {
		parts = append(parts, normalizeCrawlerDate(req.Date, req.Date))
	}
	if req.MatchID != "" {
		parts = append(parts, req.MatchID)
	}
	return strings.Join(parts, ":")
}

func (ctx *crawlContext) setItemsCount(count int) {
	ctx.progress.ItemsCount = count
	ctx.save("running", "")
}

func (ctx *crawlContext) markSuccess(current string) {
	ctx.progress.SuccessCount++
	ctx.progress.Current = current
	ctx.save("running", "")
}

func (ctx *crawlContext) markFailed(current string, err error) {
	ctx.progress.FailedCount++
	ctx.progress.Current = current
	if err != nil {
		ctx.note(fmt.Sprintf("%s: %v", current, err))
	}
	ctx.save("running", "")
}

func (ctx *crawlContext) markSkipped(current string) {
	ctx.progress.SkippedCount++
	ctx.progress.Current = current
	ctx.note("skipped existing data: " + current)
	ctx.save("running", "")
}

func (ctx *crawlContext) note(message string) {
	if message == "" {
		return
	}
	ctx.progress.Notes = append(ctx.progress.Notes, message)
	if len(ctx.progress.Notes) > 20 {
		ctx.progress.Notes = ctx.progress.Notes[len(ctx.progress.Notes)-20:]
	}
}

func (ctx *crawlContext) save(status string, errMsg string) {
	updates := map[string]interface{}{
		"status":        status,
		"items_count":   ctx.progress.ItemsCount,
		"success_count": ctx.progress.SuccessCount,
		"failed_count":  ctx.progress.FailedCount,
		"details":       marshalProgress(ctx.progress),
	}
	if errMsg != "" {
		updates["error_msg"] = errMsg
	}
	if status != "running" {
		var crawlerLog models.CrawlerLog
		if err := database.DB.First(&crawlerLog, ctx.logID).Error; err == nil {
			endTime := time.Now()
			updates["end_time"] = &endTime
			updates["duration"] = endTime.Sub(crawlerLog.StartTime).Milliseconds()
		}
	}
	database.DB.Model(&models.CrawlerLog{}).Where("id = ?", ctx.logID).Updates(updates)
}

func marshalProgress(progress crawlProgress) string {
	data, err := json.Marshal(progress)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func updateCrawlerTaskAfterRun(taskID uint, status string) {
	if taskID == 0 {
		return
	}

	var task models.CrawlerTask
	if err := database.DB.First(&task, taskID).Error; err != nil {
		return
	}

	oldRunCount := task.RunCount
	successCount := task.SuccessRate / 100 * float64(oldRunCount)
	if status == "success" {
		successCount++
	}
	newRunCount := oldRunCount + 1
	successRate := 0.0
	if newRunCount > 0 {
		successRate = successCount / float64(newRunCount) * 100
	}

	database.DB.Model(&task).Updates(map[string]interface{}{
		"status":       status,
		"run_count":    newRunCount,
		"success_rate": successRate,
	})
}

// userAgents is a list of user agents for anti-bot rotation
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
}

const (
	vipcAPIBaseURL = "https://www.vipc.cn/i"
	vipcLiveURL    = "https://www.vipc.cn/live/football"
)

func generateRandomIP() string {
	return fmt.Sprintf("%d.%d.%d.%d", rand.Intn(256), rand.Intn(256), rand.Intn(256), rand.Intn(256))
}

func resolveCrawlerDate(date string) (string, string, error) {
	if date == "" || strings.EqualFold(date, "today") {
		today := time.Now().Format("2006-01-02")
		return today, "today", nil
	}

	for _, layout := range []string{"2006-01-02", "20060102"} {
		parsed, err := time.Parse(layout, date)
		if err == nil {
			normalized := parsed.Format("2006-01-02")
			return normalized, normalized, nil
		}
	}

	return "", "", fmt.Errorf("invalid date: %s", date)
}

func resolveCrawlerDateWindow(date string) (string, string, string, error) {
	startDate, apiDate, err := resolveCrawlerDate(date)
	if err != nil {
		return "", "", "", err
	}

	endDate := startDate
	if shouldIncludeNextCrawlerDate(date) {
		parsed, parseErr := time.Parse("2006-01-02", startDate)
		if parseErr != nil {
			return "", "", "", parseErr
		}
		endDate = parsed.AddDate(0, 0, 1).Format("2006-01-02")
	}

	return startDate, endDate, apiDate, nil
}

func shouldIncludeNextCrawlerDate(date string) bool {
	trimmed := strings.TrimSpace(date)
	return trimmed == "" || strings.EqualFold(trimmed, "today")
}

func crawlerDateInWindow(date string, startDate string, endDate string) bool {
	return date >= startDate && date <= endDate
}

func normalizeCrawlerDate(date string, fallback string) string {
	if date == "" {
		return fallback
	}

	for _, layout := range []string{"2006-01-02", "20060102", time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.Parse(layout, date)
		if err == nil {
			return parsed.Format("2006-01-02")
		}
	}

	return fallback
}

func formatScores(homeScore, guestScore int) string {
	return fmt.Sprintf("%d-%d", homeScore, guestScore)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ensureFootballLogo(rawLogo string) string {
	trimmed := strings.TrimSpace(rawLogo)
	if trimmed == "" {
		return ""
	}

	filename := footballLogoFilename(trimmed)
	if filename == "" {
		return trimmed
	}
	localURL := "/footballimg/" + filename
	if strings.HasPrefix(trimmed, "/footballimg/") {
		return localURL
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http") {
		return trimmed
	}

	logoDir := footballImgDir()
	if err := os.MkdirAll(logoDir, 0755); err != nil {
		return trimmed
	}
	localPath := filepath.Join(logoDir, filename)
	if _, err := os.Stat(localPath); err == nil {
		return localURL
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, trimmed, nil)
	if err != nil {
		return trimmed
	}
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Referer", vipcLiveURL)

	resp, err := client.Do(req)
	if err != nil {
		return trimmed
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return trimmed
	}

	tempPath := localPath + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		return trimmed
	}
	_, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tempPath)
		return trimmed
	}
	if err := os.Rename(tempPath, localPath); err != nil {
		_ = os.Remove(tempPath)
		return trimmed
	}

	return localURL
}

func footballLogoFilename(rawLogo string) string {
	if parsed, err := url.Parse(rawLogo); err == nil && parsed.Path != "" {
		return path.Base(parsed.Path)
	}
	trimmed := strings.Trim(strings.Split(rawLogo, "?")[0], "/")
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}

func footballImgDir() string {
	configured := strings.TrimSpace(config.FootballImgDir)
	if configured == "" {
		configured = "../public/footballimg"
	}
	if filepath.IsAbs(configured) {
		return configured
	}

	candidates := []string{configured, "../public/footballimg", "public/footballimg"}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		cleaned := filepath.Clean(candidate)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
			return cleaned
		}
	}

	return filepath.Clean(configured)
}

func makeRequest(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	randomIP := generateRandomIP()
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", vipcLiveURL)
	req.Header.Set("Origin", "https://www.vipc.cn")
	req.Header.Set("X-Forwarded-For", randomIP)
	req.Header.Set("X-Real-IP", randomIP)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return io.ReadAll(resp.Body)
}

func crawlMatchList(ctx *crawlContext, date string) error {
	requestDate, endDate, apiDate, err := resolveCrawlerDateWindow(date)
	if err != nil {
		return err
	}
	ctx.progress.Date = requestDate
	if endDate != requestDate {
		ctx.note(fmt.Sprintf("match list window: %s to %s", requestDate, endDate))
	}

	url := fmt.Sprintf("%s/live/football/date/%s/next", vipcAPIBaseURL, apiDate)
	body, err := makeRequest(url)
	if err != nil {
		return fmt.Errorf("failed to fetch match list: %w", err)
	}

	var result vipcMatchListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse match list: %w", err)
	}

	processed := 0
	total := 0
	for _, item := range result.Items {
		itemDate := normalizeCrawlerDate(item.Date, requestDate)
		if crawlerDateInWindow(itemDate, requestDate, endDate) {
			total += len(item.Matches)
		}
	}
	ctx.setItemsCount(total)

	for _, item := range result.Items {
		itemDate := normalizeCrawlerDate(item.Date, requestDate)
		if !crawlerDateInWindow(itemDate, requestDate, endDate) {
			continue
		}

		for _, match := range item.Matches {
			if strings.TrimSpace(match.Model.MatchID) == "" {
				continue
			}
			ctx.progress.Current = match.Model.MatchID
			match.Model.HomeLogo = ensureFootballLogo(match.Model.HomeLogo)
			match.Model.GuestLogo = ensureFootballLogo(match.Model.GuestLogo)

			if err := upsertMoneyRecord(match.Model, itemDate); err != nil {
				ctx.markFailed(match.Model.MatchID, err)
				continue
			}

			processed++
			ctx.markSuccess(match.Model.MatchID)
			time.Sleep(500 * time.Millisecond)
		}
	}

	if processed == 0 {
		return fmt.Errorf("no matches found from %s to %s", requestDate, endDate)
	}

	return nil
}

func crawlHistory(ctx *crawlContext, matchID string) error {
	if matchID == "" {
		return fmt.Errorf("match_id is required")
	}
	if ctx.request.Type != "all" && ctx.progress.ItemsCount == 0 {
		ctx.setItemsCount(1)
	}
	ctx.progress.MatchID = matchID
	if !ctx.request.Force && hasExistingCrawlerData("history_moneys", "league_stat", matchID) {
		ctx.markSkipped(matchID)
		return nil
	}

	url := fmt.Sprintf("%s/match/football/%s/history", vipcAPIBaseURL, matchID)
	body, err := makeRequest(url)
	if err != nil {
		ctx.markFailed(matchID, err)
		return fmt.Errorf("failed to fetch history: %w", err)
	}
	if err := ensureCrawlerMatchShell(matchID, "历史数据"); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}

	if err := upsertCrawlerJSON("history_moneys", "league_stat", matchID, string(body)); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}
	ctx.markSuccess(matchID)

	return nil
}

func crawlRank(ctx *crawlContext, matchID string) error {
	if matchID == "" {
		return fmt.Errorf("match_id is required")
	}
	if ctx.request.Type != "all" && ctx.progress.ItemsCount == 0 {
		ctx.setItemsCount(1)
	}
	ctx.progress.MatchID = matchID

	rankColumn, err := historyRankColumn()
	if err != nil {
		ctx.markFailed(matchID, err)
		return err
	}
	if !ctx.request.Force && hasExistingCrawlerData("history_moneys", rankColumn, matchID) {
		ctx.markSkipped(matchID)
		return nil
	}

	url := fmt.Sprintf("%s/match/football/%s/rank", vipcAPIBaseURL, matchID)
	body, err := makeRequest(url)
	if err != nil {
		ctx.markFailed(matchID, err)
		return fmt.Errorf("failed to fetch rank: %w", err)
	}
	if err := ensureCrawlerMatchShell(matchID, "排名/积分榜"); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}

	if err := upsertCrawlerJSON("history_moneys", rankColumn, matchID, string(body)); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}
	ctx.markSuccess(matchID)

	return nil
}

func crawlOddsEuro(ctx *crawlContext, matchID string) error {
	if matchID == "" {
		return fmt.Errorf("match_id is required")
	}
	if err := ensureOddsSportteryTradeColumn(); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}
	if ctx.request.Type != "all" && ctx.progress.ItemsCount == 0 {
		ctx.setItemsCount(1)
	}
	ctx.progress.MatchID = matchID
	isJingcai := crawlerMatchIsJingcai(matchID)
	dataExists := hasExistingCrawlerData("odds_moneys", "data", matchID)
	tradeExists := !isJingcai || hasExistingCrawlerData("odds_moneys", "sporttery_trade", matchID)
	if !ctx.request.Force && dataExists && tradeExists {
		ctx.markSkipped(matchID)
		return nil
	}
	if err := ensureCrawlerMatchShell(matchID, "欧赔数据"); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}

	if ctx.request.Force || !dataExists {
		url := fmt.Sprintf("%s/match/football/%s/odds/euro", vipcAPIBaseURL, matchID)
		body, err := makeRequest(url)
		if err != nil {
			ctx.markFailed(matchID, err)
			return fmt.Errorf("failed to fetch odds euro: %w", err)
		}

		if err := upsertCrawlerJSON("odds_moneys", "data", matchID, string(body)); err != nil {
			ctx.markFailed(matchID, err)
			return err
		}
	}

	if isJingcai && (ctx.request.Force || !tradeExists) {
		if err := crawlSportteryTrade(ctx, matchID); err != nil {
			ctx.markFailed(matchID, err)
			return err
		}
	}
	ctx.markSuccess(matchID)

	return nil
}

func crawlSportteryTrade(ctx *crawlContext, matchID string) error {
	jingcaiID := crawlerMatchJingcaiID(matchID)
	body, err := fetchCrawlerSportteryTrade(matchID, jingcaiID)
	if err != nil {
		return fmt.Errorf("failed to fetch sporttery trade: %w", err)
	}
	return upsertCrawlerJSON("odds_moneys", "sporttery_trade", matchID, string(body))
}

func fetchCrawlerSportteryTrade(matchID string, jingcaiID string) ([]byte, error) {
	var lastErr error
	seen := map[string]bool{}
	for _, tradeID := range []string{matchID, jingcaiID} {
		tradeID = strings.TrimSpace(tradeID)
		if tradeID == "" || seen[tradeID] {
			continue
		}
		seen[tradeID] = true

		url := fmt.Sprintf("%s/match/jczq/lr/%s", vipcAPIBaseURL, tradeID)
		body, err := makeRequest(url)
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

func crawlOddsPankou(ctx *crawlContext, matchID string) error {
	if matchID == "" {
		return fmt.Errorf("match_id is required")
	}
	if ctx.request.Type != "all" && ctx.progress.ItemsCount == 0 {
		ctx.setItemsCount(1)
	}
	ctx.progress.MatchID = matchID
	if !ctx.request.Force && hasExistingCrawlerData("pankou_moneys", "asia_data", matchID) {
		ctx.markSkipped(matchID)
		return nil
	}

	url := fmt.Sprintf("%s/match/football/%s/odds/pankou", vipcAPIBaseURL, matchID)
	body, err := makeRequest(url)
	if err != nil {
		ctx.markFailed(matchID, err)
		return fmt.Errorf("failed to fetch pankou: %w", err)
	}
	if err := ensureCrawlerMatchShell(matchID, "盘口数据"); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}

	if err := upsertCrawlerJSON("pankou_moneys", "asia_data", matchID, string(body)); err != nil {
		ctx.markFailed(matchID, err)
		return err
	}
	ctx.markSuccess(matchID)

	return nil
}

func crawlDetailsForDate(ctx *crawlContext, date string, crawlOne func(*crawlContext, string) error) error {
	resolvedDate, _, err := resolveCrawlerDate(date)
	if err != nil {
		return err
	}
	ctx.progress.Date = resolvedDate

	var matches []models.Money
	matchQuery := database.DB.Where("date = ?", resolvedDate)
	if databaseColumnExists("moneys", "display_state") {
		matchQuery = matchQuery.Where("display_state IS NULL OR display_state <> ?", "detail_only")
	}
	matchQuery.Order("match_time ASC").Find(&matches)
	if len(matches) == 0 {
		ctx.note(fmt.Sprintf("no match list for %s, fetching match list first", resolvedDate))
		if err := crawlMatchList(ctx, resolvedDate); err != nil {
			return fmt.Errorf("failed to crawl match list for %s: %w", resolvedDate, err)
		}
		matchQuery = database.DB.Where("date = ?", resolvedDate)
		if databaseColumnExists("moneys", "display_state") {
			matchQuery = matchQuery.Where("display_state IS NULL OR display_state <> ?", "detail_only")
		}
		matchQuery.Order("match_time ASC").Find(&matches)
	}

	if len(matches) == 0 {
		return fmt.Errorf("no matches found for date %s", resolvedDate)
	}

	ctx.setItemsCount(len(matches))
	successBefore := ctx.progress.SuccessCount
	skippedBefore := ctx.progress.SkippedCount
	for _, match := range matches {
		matchID := strings.TrimSpace(match.MatchID)
		if matchID == "" {
			continue
		}
		if err := crawlOne(ctx, matchID); err != nil {
			ctx.note(fmt.Sprintf("%s: %v", matchID, err))
		}
		time.Sleep(1500 * time.Millisecond)
	}

	if ctx.progress.SuccessCount == successBefore && ctx.progress.SkippedCount == skippedBefore {
		return fmt.Errorf("no %s records completed for date %s", ctx.request.Type, resolvedDate)
	}
	return nil
}

func crawlOddsRefresh(ctx *crawlContext, date string) error {
	startDate, endDate, _, err := resolveCrawlerDateWindow(date)
	if err != nil {
		return err
	}
	ctx.progress.Date = startDate
	ctx.request.Force = true
	ctx.note(fmt.Sprintf("odds refresh window: %s to %s", startDate, endDate))
	if ctx.request.MatchID != "" {
		ctx.setItemsCount(2)
		successBefore := ctx.progress.SuccessCount
		if err := crawlOddsEuro(ctx, ctx.request.MatchID); err != nil {
			ctx.note(fmt.Sprintf("%s odds euro: %v", ctx.request.MatchID, err))
		}
		time.Sleep(1500 * time.Millisecond)
		if err := crawlOddsPankou(ctx, ctx.request.MatchID); err != nil {
			ctx.note(fmt.Sprintf("%s odds pankou: %v", ctx.request.MatchID, err))
		}
		if ctx.progress.SuccessCount == successBefore {
			return fmt.Errorf("no odds records refreshed for match %s", ctx.request.MatchID)
		}
		return nil
	}

	var matches []models.Money
	matchQuery := database.DB.Where("date BETWEEN ? AND ?", startDate, endDate)
	if databaseColumnExists("moneys", "display_state") {
		matchQuery = matchQuery.Where("display_state IS NULL OR display_state <> ?", "detail_only")
	}
	matchQuery.Order("match_time ASC").Find(&matches)
	if len(matches) == 0 {
		ctx.note(fmt.Sprintf("no match list for %s to %s, fetching match list first", startDate, endDate))
		if err := crawlMatchList(ctx, date); err != nil {
			return fmt.Errorf("failed to crawl match list for odds refresh: %w", err)
		}
		matchQuery = database.DB.Where("date BETWEEN ? AND ?", startDate, endDate)
		if databaseColumnExists("moneys", "display_state") {
			matchQuery = matchQuery.Where("display_state IS NULL OR display_state <> ?", "detail_only")
		}
		matchQuery.Order("match_time ASC").Find(&matches)
	}

	if len(matches) == 0 {
		return fmt.Errorf("no matches found from %s to %s", startDate, endDate)
	}

	ctx.setItemsCount(len(matches) * 2)
	successBefore := ctx.progress.SuccessCount
	for _, match := range matches {
		matchID := strings.TrimSpace(match.MatchID)
		if matchID == "" {
			continue
		}
		if err := crawlOddsEuro(ctx, matchID); err != nil {
			ctx.note(fmt.Sprintf("%s odds euro: %v", matchID, err))
		}
		time.Sleep(1500 * time.Millisecond)
		if err := crawlOddsPankou(ctx, matchID); err != nil {
			ctx.note(fmt.Sprintf("%s odds pankou: %v", matchID, err))
		}
		time.Sleep(1500 * time.Millisecond)
	}

	if ctx.progress.SuccessCount == successBefore {
		return fmt.Errorf("no odds records refreshed from %s to %s", startDate, endDate)
	}
	return nil
}

func crawlAll(ctx *crawlContext, date string) error {
	resolvedDate, endDate, _, err := resolveCrawlerDateWindow(date)
	if err != nil {
		return err
	}
	ctx.progress.Date = resolvedDate

	ctx.note("step 1/5: match list and team logos")
	if err := crawlMatchList(ctx, date); err != nil {
		return fmt.Errorf("failed to crawl match list: %w", err)
	}

	var matches []models.Money
	matchQuery := database.DB.Where("date BETWEEN ? AND ?", resolvedDate, endDate)
	if databaseColumnExists("moneys", "display_state") {
		matchQuery = matchQuery.Where("display_state IS NULL OR display_state <> ?", "detail_only")
	}
	matchQuery.Order("match_time ASC").Find(&matches)
	ctx.setItemsCount(len(matches) * 5)
	ctx.note(fmt.Sprintf("step 2-5: details for %d matches", len(matches)))

	for _, match := range matches {
		matchID := strings.TrimSpace(match.MatchID)
		if matchID == "" {
			continue
		}
		time.Sleep(1500 * time.Millisecond) // Anti-bot delay

		if err := crawlHistory(ctx, matchID); err != nil {
			ctx.note(fmt.Sprintf("%s history: %v", matchID, err))
		}
		time.Sleep(1500 * time.Millisecond)

		if err := crawlRank(ctx, matchID); err != nil {
			ctx.note(fmt.Sprintf("%s rank: %v", matchID, err))
		}
		time.Sleep(1500 * time.Millisecond)

		if err := crawlOddsEuro(ctx, matchID); err != nil {
			ctx.note(fmt.Sprintf("%s odds euro: %v", matchID, err))
		}
		time.Sleep(1500 * time.Millisecond)

		if err := crawlOddsPankou(ctx, matchID); err != nil {
			ctx.note(fmt.Sprintf("%s odds pankou: %v", matchID, err))
		}
	}

	return nil
}

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
	return ""
}

func hasExistingCrawlerData(tableName, columnName, matchID string) bool {
	allowed := map[string]map[string]bool{
		"history_moneys": {"league_stat": true, "rank": true, "rank_data": true},
		"odds_moneys":    {"data": true, "date": true, "sporttery_trade": true},
		"pankou_moneys":  {"asia_data": true, "date": true},
	}
	if !allowed[tableName][columnName] {
		return false
	}

	var rawValue sql.NullString
	err := database.DB.Table(tableName).
		Select(columnName).
		Where("match_id = ?", matchID).
		Limit(1).
		Scan(&rawValue).Error
	if err != nil || !rawValue.Valid {
		return false
	}

	trimmed := strings.TrimSpace(rawValue.String)
	if tableName == "odds_moneys" && columnName == "sporttery_trade" {
		return sportteryTradeJSONHasData([]byte(trimmed))
	}
	return trimmed != "" && trimmed != "null" && trimmed != "[]" && trimmed != "{}"
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

func historyRankColumn() (string, error) {
	if databaseColumnExists("history_moneys", "rank_data") {
		return "rank_data", nil
	}
	if databaseColumnExists("history_moneys", "rank") {
		return "rank", nil
	}
	return "", fmt.Errorf("history_moneys has no rank_data or rank column")
}

func databaseColumnExists(tableName string, columnName string) bool {
	allowed := map[string]map[string]bool{
		"history_moneys": {"against": true, "date": true, "future": true, "league_stat": true, "rank": true, "rank_data": true, "recent": true},
		"odds_moneys":    {"data": true, "date": true, "sporttery_trade": true},
		"pankou_moneys":  {"asia_data": true, "date": true},
		"moneys": {
			"created_at":    true,
			"date":          true,
			"display_state": true,
			"guest":         true,
			"guest_logo":    true,
			"guest_rank":    true,
			"guest_score":   true,
			"guest_team_id": true,
			"home":          true,
			"home_logo":     true,
			"home_rank":     true,
			"home_score":    true,
			"home_team_id":  true,
			"jingcai_id":    true,
			"league":        true,
			"league_id":     true,
			"league_name":   true,
			"match_state":   true,
			"match_time":    true,
			"scores":        true,
			"status":        true,
			"time":          true,
			"updated_at":    true,
		},
	}
	if !allowed[tableName][columnName] {
		return false
	}

	var count int64
	database.DB.Raw(
		"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		tableName,
		columnName,
	).Scan(&count)
	return count > 0
}

func upsertCrawlerJSON(tableName string, columnName string, matchID string, body string) error {
	allowed := map[string]map[string]bool{
		"history_moneys": {"league_stat": true, "rank": true, "rank_data": true},
		"odds_moneys":    {"data": true, "sporttery_trade": true},
		"pankou_moneys":  {"asia_data": true},
	}
	if !allowed[tableName][columnName] {
		return fmt.Errorf("unsupported crawler json target: %s.%s", tableName, columnName)
	}
	if !databaseColumnExists(tableName, columnName) {
		return fmt.Errorf("%s has no %s column", tableName, columnName)
	}

	var count int64
	database.DB.Table(tableName).Where("match_id = ?", matchID).Count(&count)
	updates := map[string]interface{}{columnName: body}
	if count == 0 {
		updates["match_id"] = matchID
		if databaseColumnExists(tableName, "date") {
			updates["date"] = crawlerMatchDate(matchID)
		}
		return database.DB.Table(tableName).Create(updates).Error
	}
	return database.DB.Table(tableName).Where("match_id = ?", matchID).Updates(updates).Error
}

func crawlerMatchDate(matchID string) string {
	var match models.Money
	if err := database.DB.Where("match_id = ?", matchID).First(&match).Error; err == nil && match.Date != "" {
		return normalizeCrawlerDate(match.Date, time.Now().Format("2006-01-02"))
	}
	return time.Now().Format("2006-01-02")
}

func crawlerMatchJingcaiID(matchID string) string {
	var rows []map[string]interface{}
	if err := database.DB.Table("moneys").Select("jingcai_id").Where("match_id = ?", matchID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return ""
	}
	value := crawlerOptionalString(rows[0]["jingcai_id"])
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func crawlerMatchIsJingcai(matchID string) bool {
	if !databaseColumnExists("moneys", "jingcai_id") {
		return true
	}

	var count int64
	database.DB.Table("moneys").Where("match_id = ? AND jingcai_id IS NOT NULL AND TRIM(jingcai_id) <> ?", matchID, "").Count(&count)
	return count > 0
}

func upsertMoneyRecord(model vipcMatchModel, date string) error {
	if err := ensureJingcaiIDColumn(); err != nil {
		return err
	}

	record := map[string]interface{}{"match_id": model.MatchID}
	setExistingMoneyColumn(record, "date", date)
	setExistingMoneyColumn(record, "league", firstNonEmpty(model.LeagueName, model.League))
	setExistingMoneyColumn(record, "league_name", model.LeagueName)
	setExistingMoneyColumn(record, "league_id", model.LeagueID)
	setExistingMoneyColumn(record, "home", model.Home)
	setExistingMoneyColumn(record, "guest", model.Guest)
	setExistingMoneyColumn(record, "home_team_id", model.HomeTeamID)
	setExistingMoneyColumn(record, "guest_team_id", model.GuestTeamID)
	setExistingMoneyColumn(record, "home_rank", crawlerOptionalString(model.HomeRank))
	setExistingMoneyColumn(record, "guest_rank", crawlerOptionalString(model.GuestRank))
	setExistingMoneyColumn(record, "scores", formatScores(model.HomeScore, model.GuestScore))
	setExistingMoneyColumn(record, "home_score", model.HomeScore)
	setExistingMoneyColumn(record, "guest_score", model.GuestScore)
	setExistingMoneyColumn(record, "status", model.Status)
	setExistingMoneyColumn(record, "match_state", model.MatchState)
	setExistingMoneyColumn(record, "time", model.Time)
	setExistingMoneyColumn(record, "display_state", model.DisplayState)
	setExistingMoneyColumn(record, "jingcai_id", crawlerOptionalString(model.JingcaiID))
	setExistingMoneyColumn(record, "home_logo", model.HomeLogo)
	setExistingMoneyColumn(record, "guest_logo", model.GuestLogo)
	setExistingMoneyColumn(record, "match_time", model.MatchTime)

	return upsertMoneyColumns(model.MatchID, record)
}

func ensureJingcaiIDColumn() error {
	if databaseColumnExists("moneys", "jingcai_id") {
		return nil
	}
	return database.DB.Exec("ALTER TABLE moneys ADD COLUMN jingcai_id varchar(50) NULL").Error
}

func ensureOddsSportteryTradeColumn() error {
	if databaseColumnExists("odds_moneys", "sporttery_trade") {
		return nil
	}
	return database.DB.Exec("ALTER TABLE odds_moneys ADD COLUMN sporttery_trade JSON NULL").Error
}

func crawlerOptionalString(value any) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || strings.EqualFold(trimmed, "<nil>") || strings.EqualFold(trimmed, "null") {
		return nil
	}
	return trimmed
}

func ensureCrawlerMatchShell(matchID string, source string) error {
	if strings.TrimSpace(matchID) == "" {
		return nil
	}

	var count int64
	database.DB.Table("moneys").Where("match_id = ?", matchID).Count(&count)
	if count > 0 {
		return nil
	}

	record := map[string]interface{}{"match_id": matchID}
	setExistingMoneyColumn(record, "date", time.Now().Format("2006-01-02"))
	setExistingMoneyColumn(record, "league", source)
	setExistingMoneyColumn(record, "league_name", source)
	setExistingMoneyColumn(record, "home", fmt.Sprintf("比赛 %s", matchID))
	setExistingMoneyColumn(record, "guest", "")
	setExistingMoneyColumn(record, "display_state", "detail_only")

	return upsertMoneyColumns(matchID, record)
}

func setExistingMoneyColumn(record map[string]interface{}, column string, value interface{}) {
	if databaseColumnExists("moneys", column) {
		record[column] = value
	}
}

func upsertMoneyColumns(matchID string, record map[string]interface{}) error {
	var count int64
	database.DB.Table("moneys").Where("match_id = ?", matchID).Count(&count)

	now := time.Now()
	if databaseColumnExists("moneys", "updated_at") {
		record["updated_at"] = now
	}

	if count == 0 {
		if databaseColumnExists("moneys", "created_at") {
			record["created_at"] = now
		}
		return database.DB.Table("moneys").Create(record).Error
	}

	updates := make(map[string]interface{}, len(record))
	for key, value := range record {
		if key != "match_id" && key != "created_at" {
			updates[key] = value
		}
	}
	return database.DB.Table("moneys").Where("match_id = ?", matchID).Updates(updates).Error
}

func loadCrawlerRecord(tableName string, matchID string) map[string]interface{} {
	allowed := map[string]bool{
		"moneys":         true,
		"history_moneys": true,
		"odds_moneys":    true,
		"pankou_moneys":  true,
	}
	if !allowed[tableName] {
		return map[string]interface{}{}
	}

	var rows []map[string]interface{}
	if err := database.DB.Table(tableName).Where("match_id = ?", matchID).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return map[string]interface{}{}
	}

	return normalizeCrawlerRecord(rows[0])
}

func normalizeCrawlerRecord(record map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{}, len(record))
	for key, value := range record {
		switch typed := value.(type) {
		case []byte:
			normalized[key] = parseCrawlerJSONValue(string(typed))
		case string:
			normalized[key] = parseCrawlerJSONValue(typed)
		default:
			normalized[key] = value
		}
	}
	return normalized
}

func parseCrawlerJSONValue(value string) interface{} {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return value
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return value
	}

	var parsed interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return value
	}
	return parsed
}

type vipcMatchListResponse struct {
	Items []vipcMatchListItem `json:"items"`
}

type vipcMatchListItem struct {
	Date    string      `json:"date"`
	Matches []vipcMatch `json:"matches"`
}

type vipcMatch struct {
	Model vipcMatchModel `json:"model"`
}

type vipcMatchModel struct {
	MatchID      string `json:"matchId"`
	Home         string `json:"home"`
	Guest        string `json:"guest"`
	League       string `json:"league"`
	LeagueName   string `json:"leagueName"`
	LeagueID     int    `json:"leagueId"`
	MatchTime    string `json:"matchTime"`
	Status       int    `json:"status"`
	MatchState   int    `json:"matchState"`
	Time         string `json:"time"`
	HomeScore    int    `json:"homeScore"`
	GuestScore   int    `json:"guestScore"`
	HomeTeamID   int    `json:"homeTeamId"`
	GuestTeamID  int    `json:"guestTeamId"`
	HomeRank     any    `json:"homeRank"`
	GuestRank    any    `json:"guestRank"`
	HomeLogo     string `json:"homeLogo"`
	GuestLogo    string `json:"guestLogo"`
	DisplayState string `json:"displayState"`
	JingcaiID    any    `json:"jingcaiId"`
}
