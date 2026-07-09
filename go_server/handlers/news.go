// Package handlers implements HTTP request handlers for news operations.
package handlers

import (
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetNews returns news articles filtered by category.
// Query param ?category=latest|transfer|official (defaults to all).
func GetNews(c *gin.Context) {
	category := c.Query("category")

	query := database.DB.Order("created_at DESC").Limit(30)
	if category != "" {
		query = query.Where("category = ?", category)
	}

	var news []models.News
	if err := query.Find(&news).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, news)
}

// GetTransferRumors returns transfer rumors ordered by trust level.
func GetTransferRumors(c *gin.Context) {
	var rumors []models.TransferRumor
	if err := database.DB.Order("trust_level DESC").Find(&rumors).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rumors)
}
