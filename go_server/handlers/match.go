// Package handlers implements HTTP request handlers for the football match API.
package handlers

import (
	"net/http"
	"time"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

const detailOnlyDisplayState = "detail_only"

// GetMatches returns matches filtered by date (query param ?date=YYYY-MM-DD).
// If no date is provided, defaults to today.
func GetMatches(c *gin.Context) {
	dateStr := c.DefaultQuery("date", time.Now().Format("2006-01-02"))

	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}
	normalizedDate := date.Format("2006-01-02")

	var matches []models.Money
	if err := database.DB.Where("date = ? AND (display_state IS NULL OR display_state <> ?)", normalizedDate, detailOnlyDisplayState).Order("match_time ASC").Find(&matches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, matches)
}

// GetMatchDetail returns a single match by its match ID.
func GetMatchDetail(c *gin.Context) {
	matchId := c.Param("id")

	var match models.Money
	if err := database.DB.Where("match_id = ? AND (display_state IS NULL OR display_state <> ?)", matchId, detailOnlyDisplayState).First(&match).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
		return
	}

	c.JSON(http.StatusOK, match)
}

// GetMatchHistory returns the history data for a given match ID.
func GetMatchHistory(c *gin.Context) {
	matchId := c.Param("id")

	var history models.HistoryMoney
	if err := database.DB.Where("match_id = ?", matchId).First(&history).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "match history not found"})
		return
	}

	c.JSON(http.StatusOK, history)
}

// GetMatchOddsEuro returns the European odds data for a given match ID.
func GetMatchOddsEuro(c *gin.Context) {
	matchId := c.Param("id")

	var odds models.OddsMoney
	if err := database.DB.Where("match_id = ?", matchId).First(&odds).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "euro odds not found"})
		return
	}

	c.JSON(http.StatusOK, odds)
}

// GetMatchOddsPankou returns the Asian handicap odds data for a given match ID.
func GetMatchOddsPankou(c *gin.Context) {
	matchId := c.Param("id")

	var pankou models.PankouMoney
	if err := database.DB.Where("match_id = ?", matchId).First(&pankou).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "pankou odds not found"})
		return
	}

	c.JSON(http.StatusOK, pankou)
}
