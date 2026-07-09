package handlers

import (
	"net/http"
	"strconv"

	"go_admin/database"
	"go_admin/models"

	"github.com/gin-gonic/gin"
)

// GetMenus returns all menus as flat list
func GetMenus(c *gin.Context) {
	var menus []models.Menu
	if err := database.DB.Order("sort ASC, id ASC").Find(&menus).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch menus"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"list": menus})
}

// GetMenuTree returns menus as tree structure
func GetMenuTree(c *gin.Context) {
	var menus []models.Menu
	if err := database.DB.Order("sort ASC, id ASC").Find(&menus).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch menus"})
		return
	}

	tree := buildMenuTree(menus, 0)
	c.JSON(http.StatusOK, gin.H{"list": tree})
}

// CreateMenu creates a new menu
func CreateMenu(c *gin.Context) {
	var menu models.Menu
	if err := c.ShouldBindJSON(&menu); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.DB.Create(&menu).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create menu"})
		return
	}

	c.JSON(http.StatusCreated, menu)
}

// UpdateMenu updates a menu
func UpdateMenu(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid menu ID"})
		return
	}

	var menu models.Menu
	if err := database.DB.First(&menu, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Menu not found"})
		return
	}

	var req struct {
		ParentID  uint   `json:"parent_id"`
		Name      string `json:"name"`
		Title     string `json:"title"`
		Icon      string `json:"icon"`
		Path      string `json:"path"`
		Component string `json:"component"`
		Sort      int    `json:"sort"`
		Status    int    `json:"status"`
		MenuType  string `json:"menu_type"`
		Hidden    bool   `json:"hidden"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	database.DB.Model(&menu).Updates(map[string]interface{}{
		"parent_id": req.ParentID,
		"name":      req.Name,
		"title":     req.Title,
		"icon":      req.Icon,
		"path":      req.Path,
		"component": req.Component,
		"sort":      req.Sort,
		"status":    req.Status,
		"menu_type": req.MenuType,
		"hidden":    req.Hidden,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Menu updated successfully"})
}

// DeleteMenu deletes a menu and its children
func DeleteMenu(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid menu ID"})
		return
	}

	// Check for child menus
	var childCount int64
	database.DB.Model(&models.Menu{}).Where("parent_id = ?", id).Count(&childCount)
	if childCount > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot delete menu with children"})
		return
	}

	if err := database.DB.Delete(&models.Menu{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete menu"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Menu deleted successfully"})
}
