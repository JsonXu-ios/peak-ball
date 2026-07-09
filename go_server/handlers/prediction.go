// Package handlers implements HTTP request handlers for prediction operations.
package handlers

import (
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetUserPredictions returns predictions for the current user.
// Query param ?status=ongoing|settled filters results.
func GetUserPredictions(c *gin.Context) {
	status := c.Query("status")

	query := database.DB.Where("user_id = ?", 1).Preload("Match").Order("created_at DESC")
	if status == "ongoing" {
		query = query.Where("status = ?", "ongoing")
	} else if status == "settled" {
		query = query.Where("status IN ?", []string{"won", "lost", "void"})
	}

	var predictions []models.Prediction
	if err := query.Find(&predictions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, predictions)
}

// GetUserStats returns computed stats for the current user.
func GetUserStats(c *gin.Context) {
	var total, won, lost, ongoing int64

	database.DB.Model(&models.Prediction{}).Where("user_id = ?", 1).Count(&total)
	database.DB.Model(&models.Prediction{}).Where("user_id = ? AND status = ?", 1, "won").Count(&won)
	database.DB.Model(&models.Prediction{}).Where("user_id = ? AND status = ?", 1, "lost").Count(&lost)
	database.DB.Model(&models.Prediction{}).Where("user_id = ? AND status = ?", 1, "ongoing").Count(&ongoing)

	var totalProfit float64
	database.DB.Model(&models.Prediction{}).Where("user_id = ?", 1).
		Select("COALESCE(SUM(profit), 0)").Scan(&totalProfit)

	accuracy := float64(0)
	settled := won + lost
	if settled > 0 {
		accuracy = float64(won) / float64(settled) * 100
	}

	stats := models.UserStats{
		TotalPredictions: int(total),
		Won:              int(won),
		Lost:             int(lost),
		Ongoing:          int(ongoing),
		Accuracy:         accuracy,
		TotalProfit:      totalProfit,
		ProfitChange:     12.5,
	}

	c.JSON(http.StatusOK, stats)
}

// CreatePrediction creates a new prediction for the current user.
func CreatePrediction(c *gin.Context) {
	var input struct {
		MatchID string  `json:"matchId" binding:"required"`
		Pick    string  `json:"pick" binding:"required"`
		Odds    float64 `json:"odds" binding:"required"`
		Stake   float64 `json:"stake" binding:"required"`
	}

	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	prediction := models.Prediction{
		UserID:  1,
		MatchID: input.MatchID,
		Pick:    input.Pick,
		Odds:    input.Odds,
		Stake:   input.Stake,
		Status:  "ongoing",
	}

	if err := database.DB.Create(&prediction).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, prediction)
}
