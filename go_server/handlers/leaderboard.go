// Package handlers implements HTTP request handlers for leaderboard operations.
package handlers

import (
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetLeaderboard returns leaderboard entries for a given period.
// Query param ?period=weekly|monthly (defaults to weekly).
func GetLeaderboard(c *gin.Context) {
	period := c.DefaultQuery("period", "weekly")

	var entries []models.LeaderboardEntry
	if err := database.DB.Where("period = ?", period).
		Preload("User").
		Order("`rank` ASC").
		Find(&entries).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, entries)
}
