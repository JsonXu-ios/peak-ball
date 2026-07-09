// Package handlers implements HTTP request handlers for wallet operations.
package handlers

import (
	"net/http"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetWalletBalance returns the current user's points balance and lifetime earned.
func GetWalletBalance(c *gin.Context) {
	var user models.User
	if err := database.DB.First(&user, 1).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}

	var lifetimeEarned int64
	database.DB.Model(&models.WalletTransaction{}).
		Where("user_id = ? AND amount > 0", 1).
		Select("COALESCE(SUM(amount), 0)").Scan(&lifetimeEarned)

	c.JSON(http.StatusOK, gin.H{
		"balance":        user.Balance,
		"lifetimeEarned": lifetimeEarned,
	})
}

// GetWalletTransactions returns the transaction history for the current user.
// Query param ?type=earned|spent filters.
func GetWalletTransactions(c *gin.Context) {
	txType := c.Query("type")

	query := database.DB.Where("user_id = ?", 1).Order("created_at DESC").Limit(50)
	if txType == "earned" {
		query = query.Where("amount > 0")
	} else if txType == "spent" {
		query = query.Where("amount < 0")
	}

	var transactions []models.WalletTransaction
	if err := query.Find(&transactions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, transactions)
}

// GetRewards returns available rewards in the store.
func GetRewards(c *gin.Context) {
	var rewards []models.Reward
	if err := database.DB.Where("is_active = ?", true).Find(&rewards).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, rewards)
}
