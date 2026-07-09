// Package handlers implements HTTP request handlers for notification operations.
package handlers

import (
	"net/http"
	"time"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetNotifications returns notifications for the current user.
// Query param ?type=goal|red_card|lineup|expert_tip|reward|system filters by type.
func GetNotifications(c *gin.Context) {
	notifType := c.Query("type")

	query := database.DB.Where("user_id = ?", 1).Order("created_at DESC").Limit(50)
	if notifType != "" {
		// Map frontend tab names to actual DB type values.
		switch notifType {
		case "match":
			query = query.Where("type IN ?", []string{"goal", "red_card", "lineup"})
		case "expert":
			query = query.Where("type IN ?", []string{"expert_tip"})
		case "system":
			query = query.Where("type IN ?", []string{"reward", "system"})
		default:
			query = query.Where("type = ?", notifType)
		}
	}

	var notifications []models.Notification
	if err := query.Find(&notifications).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, notifications)
}

// MarkNotificationRead marks a single notification as read.
func MarkNotificationRead(c *gin.Context) {
	id := c.Param("id")
	now := time.Now()
	if err := database.DB.Model(&models.Notification{}).
		Where("id = ? AND user_id = ?", id, 1).
		Updates(map[string]any{"is_read": true, "read_at": now}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// MarkAllNotificationsRead marks all notifications as read for the current user.
func MarkAllNotificationsRead(c *gin.Context) {
	now := time.Now()
	if err := database.DB.Model(&models.Notification{}).
		Where("user_id = ? AND is_read = ?", 1, false).
		Updates(map[string]any{"is_read": true, "read_at": now}).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

// GetUnreadCount returns the count of unread notifications.
func GetUnreadCount(c *gin.Context) {
	var count int64
	database.DB.Model(&models.Notification{}).
		Where("user_id = ? AND is_read = ?", 1, false).
		Count(&count)
	c.JSON(http.StatusOK, gin.H{"count": count})
}
