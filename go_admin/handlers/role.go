package handlers

import (
	"net/http"
	"strconv"

	"go_admin/database"
	"go_admin/models"

	"github.com/gin-gonic/gin"
)

// GetRoles returns all roles
func GetRoles(c *gin.Context) {
	keyword := c.Query("keyword")
	query := database.DB.Model(&models.Role{})

	if keyword != "" {
		query = query.Where("name LIKE ? OR code LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}

	var roles []models.Role
	if err := query.Preload("Menus").Preload("Permissions").Order("sort ASC, id ASC").Find(&roles).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch roles"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"list": roles})
}

// CreateRole creates a new role
func CreateRole(c *gin.Context) {
	var role models.Role
	if err := c.ShouldBindJSON(&role); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.DB.Create(&role).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create role"})
		return
	}

	c.JSON(http.StatusCreated, role)
}

// UpdateRole updates a role
func UpdateRole(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	var role models.Role
	if err := database.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}

	var req struct {
		Name        string `json:"name"`
		Code        string `json:"code"`
		Description string `json:"description"`
		Sort        int    `json:"sort"`
		Status      int    `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	database.DB.Model(&role).Updates(map[string]interface{}{
		"name":        req.Name,
		"code":        req.Code,
		"description": req.Description,
		"sort":        req.Sort,
		"status":      req.Status,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Role updated successfully"})
}

// DeleteRole deletes a role
func DeleteRole(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	// Prevent deleting admin role
	if id == 1 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Cannot delete admin role"})
		return
	}

	if err := database.DB.Delete(&models.Role{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete role"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Role deleted successfully"})
}

// GetRoleMenus returns menus assigned to a role
func GetRoleMenus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	var role models.Role
	if err := database.DB.Preload("Menus").First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}

	var menuIDs []uint
	for _, menu := range role.Menus {
		menuIDs = append(menuIDs, menu.ID)
	}

	c.JSON(http.StatusOK, gin.H{"menu_ids": menuIDs})
}

// UpdateRoleMenus updates menus assigned to a role
func UpdateRoleMenus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	var req struct {
		MenuIDs []uint `json:"menu_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var role models.Role
	if err := database.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}

	var menus []models.Menu
	database.DB.Where("id IN ?", req.MenuIDs).Find(&menus)
	database.DB.Model(&role).Association("Menus").Replace(menus)

	c.JSON(http.StatusOK, gin.H{"message": "Role menus updated successfully"})
}

// GetRolePermissions returns permissions assigned to a role
func GetRolePermissions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	var role models.Role
	if err := database.DB.Preload("Permissions").First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}

	var permIDs []uint
	for _, perm := range role.Permissions {
		permIDs = append(permIDs, perm.ID)
	}

	c.JSON(http.StatusOK, gin.H{"permission_ids": permIDs})
}

// UpdateRolePermissions updates permissions assigned to a role
func UpdateRolePermissions(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid role ID"})
		return
	}

	var req struct {
		PermissionIDs []uint `json:"permission_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var role models.Role
	if err := database.DB.First(&role, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Role not found"})
		return
	}

	var perms []models.Permission
	database.DB.Where("id IN ?", req.PermissionIDs).Find(&perms)
	database.DB.Model(&role).Association("Permissions").Replace(perms)

	c.JSON(http.StatusOK, gin.H{"message": "Role permissions updated successfully"})
}
