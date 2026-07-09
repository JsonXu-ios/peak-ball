// Package handlers implements HTTP request handlers for search operations.
package handlers

import (
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// Search performs a global search across teams, players, leagues, and experts.
// Query param ?q=keyword.
func Search(c *gin.Context) {
	q := c.Query("q")
	if q == "" {
		c.JSON(http.StatusOK, gin.H{"teams": []any{}, "experts": []any{}, "news": []any{}})
		return
	}

	keyword := "%" + q + "%"

	// Search in league standings for team names
	var teams []models.LeagueStanding
	database.DB.Where("team_name LIKE ?", keyword).
		Group("team_id").Limit(10).Find(&teams)

	// Search experts
	var experts []models.Expert
	database.DB.Where("name LIKE ? OR specialty LIKE ?", keyword, keyword).
		Limit(10).Find(&experts)

	// Search news
	var news []models.News
	database.DB.Where("title LIKE ? OR summary LIKE ?", keyword, keyword).
		Order("created_at DESC").Limit(10).Find(&news)

	c.JSON(http.StatusOK, gin.H{
		"teams":   teams,
		"experts": experts,
		"news":    news,
	})
}

// GetSearchHistory returns search history for the current user.
func GetSearchHistory(c *gin.Context) {
	var history []models.SearchHistory
	if err := database.DB.Where("user_id = ?", 1).
		Order("created_at DESC").Limit(10).
		Find(&history).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, history)
}

// SaveSearchHistory saves a search query.
func SaveSearchHistory(c *gin.Context) {
	var input struct {
		Query string `json:"query" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	record := models.SearchHistory{UserID: 1, Query: input.Query}
	if err := database.DB.Create(&record).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, record)
}

// ClearSearchHistory removes all search history for the current user.
func ClearSearchHistory(c *gin.Context) {
	if err := database.DB.Where("user_id = ?", 1).Delete(&models.SearchHistory{}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
