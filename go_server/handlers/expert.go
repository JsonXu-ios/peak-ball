// Package handlers implements HTTP request handlers for expert operations.
package handlers

import (
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetExperts returns all verified experts.
func GetExperts(c *gin.Context) {
	var experts []models.Expert
	if err := database.DB.Order("accuracy DESC").Find(&experts).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, experts)
}

// GetMatchExpertTips returns expert tips for a specific match.
func GetMatchExpertTips(c *gin.Context) {
	matchID := c.Param("id")

	var tips []models.ExpertTip
	if err := database.DB.Where("match_id = ?", matchID).
		Preload("Expert").
		Order("created_at DESC").
		Find(&tips).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, tips)
}
