package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go_admin/config"

	"github.com/gin-gonic/gin"
)

// GetAnalysisRuleSnapshotInfo returns the configured portable rule snapshot path.
func GetAnalysisRuleSnapshotInfo(c *gin.Context) {
	absolutePath, err := filepath.Abs(config.AnalysisRuleSnapshotPath)
	if err != nil {
		absolutePath = config.AnalysisRuleSnapshotPath
	}
	_, statErr := os.Stat(absolutePath)

	c.JSON(http.StatusOK, gin.H{
		"path":          config.AnalysisRuleSnapshotPath,
		"absolute_path": absolutePath,
		"exists":        statErr == nil,
	})
}

// GetAnalysisRuleSnapshotData returns the current rule snapshot JSON for admin display.
func GetAnalysisRuleSnapshotData(c *gin.Context) {
	absolutePath, err := filepath.Abs(config.AnalysisRuleSnapshotPath)
	if err != nil {
		absolutePath = config.AnalysisRuleSnapshotPath
	}
	content, err := os.ReadFile(absolutePath)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"path":          config.AnalysisRuleSnapshotPath,
			"absolute_path": absolutePath,
			"exists":        false,
			"commonRows":    []any{},
		})
		return
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "规则池 JSON 格式错误"})
		return
	}
	payload["path"] = config.AnalysisRuleSnapshotPath
	payload["absolute_path"] = absolutePath
	payload["exists"] = true
	c.JSON(http.StatusOK, payload)
}

// GenerateAnalysisRuleSnapshot triggers the public API to rebuild and write the rule snapshot file.
func GenerateAnalysisRuleSnapshot(c *gin.Context) {
	endpoint := strings.TrimRight(config.AnalysisAPIBaseURL, "/") + "/analysis/rule-snapshot/generate"
	resp, err := http.Post(endpoint, "application/json", nil)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if resp.StatusCode >= 400 {
		c.Data(resp.StatusCode, "application/json; charset=utf-8", body)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}
