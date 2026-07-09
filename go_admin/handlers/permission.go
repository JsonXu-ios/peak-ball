package handlers

import (
	"net/http"
	"strconv"

	"go_admin/database"
	"go_admin/models"

	"github.com/gin-gonic/gin"
)

// GetPermissions returns all permissions
func GetPermissions(c *gin.Context) {
	keyword := c.Query("keyword")
	category := c.Query("category")
	query := database.DB.Model(&models.Permission{})

	if keyword != "" {
		query = query.Where("name LIKE ? OR code LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if category != "" {
		query = query.Where("category = ?", category)
	}

	var permissions []models.Permission
	if err := query.Order("category ASC, id ASC").Find(&permissions).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch permissions"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"list": permissions})
}

// CreatePermission creates a new permission
func CreatePermission(c *gin.Context) {
	var perm models.Permission
	if err := c.ShouldBindJSON(&perm); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.DB.Create(&perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create permission"})
		return
	}

	c.JSON(http.StatusCreated, perm)
}

// UpdatePermission updates a permission
func UpdatePermission(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid permission ID"})
		return
	}

	var perm models.Permission
	if err := database.DB.First(&perm, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Permission not found"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Code        string `json:"code"`
		Description string `json:"description"`
		Category    string `json:"category"`
		Status      int    `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	database.DB.Model(&perm).Updates(map[string]interface{}{
		"name":        req.Name,
		"code":        req.Code,
		"description": req.Description,
		"category":    req.Category,
		"status":      req.Status,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Permission updated successfully"})
}

// DeletePermission deletes a permission
func DeletePermission(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid permission ID"})
		return
	}

	if err := database.DB.Delete(&models.Permission{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete permission"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Permission deleted successfully"})
}
