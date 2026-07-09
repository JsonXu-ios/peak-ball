package handlers

import (
	"net/http"

	"go_admin/database"
	"go_admin/models"

	"github.com/gin-gonic/gin"
)

// DashboardStats represents admin dashboard statistics
type DashboardStats struct {
	TotalMatches   int64 `json:"total_matches"`
	TodayMatches   int64 `json:"today_matches"`
	TotalUsers     int64 `json:"total_users"`
	ActiveUsers    int64 `json:"active_users"`
	TotalExperts   int64 `json:"total_experts"`
	TotalNews      int64 `json:"total_news"`
	CrawlerSuccess int64 `json:"crawler_success"`
	CrawlerFailed  int64 `json:"crawler_failed"`
}

// GetDashboardStats returns statistics for the admin dashboard
func GetDashboardStats(c *gin.Context) {
	var stats DashboardStats

	// Match statistics
	database.DB.Model(&models.Money{}).Count(&stats.TotalMatches)
	database.DB.Model(&models.Money{}).Where("date = CURDATE()").Count(&stats.TodayMatches)

	// Admin user statistics
	database.DB.Model(&models.AdminUser{}).Count(&stats.TotalUsers)
	database.DB.Model(&models.AdminUser{}).Where("status = 1").Count(&stats.ActiveUsers)

	// Crawler log statistics
	database.DB.Model(&models.CrawlerLog{}).Where("status = 'success'").Count(&stats.CrawlerSuccess)
	database.DB.Model(&models.CrawlerLog{}).Where("status = 'failed'").Count(&stats.CrawlerFailed)

	// Recent crawler logs
	var recentLogs []models.CrawlerLog
	database.DB.Order("id DESC").Limit(10).Find(&recentLogs)

	// Recent matches by league
	type LeagueStat struct {
		League string `json:"league"`
		Count  int64  `json:"count"`
	}
	var leagueStats []LeagueStat
	database.DB.Model(&models.Money{}).
		Select("league, COUNT(*) as count").
		Where("league != ''").
		Group("league").
		Order("count DESC").
		Limit(10).
		Scan(&leagueStats)

	c.JSON(http.StatusOK, gin.H{
		"stats":        stats,
		"recent_logs":  recentLogs,
		"league_stats": leagueStats,
	})
}
