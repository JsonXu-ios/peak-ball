package handlers

import (
	"net/http"
	"strconv"

	"go_admin/database"
	"go_admin/models"

	"github.com/gin-gonic/gin"
)

// GetOperationLogs returns operation logs
func GetOperationLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")
	method := c.Query("method")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	query := database.DB.Model(&models.OperationLog{})

	if keyword != "" {
		query = query.Where("username LIKE ? OR path LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if method != "" {
		query = query.Where("method = ?", method)
	}

	var total int64
	query.Count(&total)

	var logs []models.OperationLog
	if err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"list":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}
