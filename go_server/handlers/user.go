// Package handlers implements HTTP request handlers for user-related operations.
package handlers

import (
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetCurrentUser returns the current user. For now, always returns user ID=1.
func GetCurrentUser(c *gin.Context) {
	var user models.User
	if err := database.DB.First(&user, 1).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, user)
}

// GetFollowedTeams returns teams the current user follows.
func GetFollowedTeams(c *gin.Context) {
	var teams []models.FollowedTeam
	if err := database.DB.Where("user_id = ?", 1).Find(&teams).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, teams)
}
