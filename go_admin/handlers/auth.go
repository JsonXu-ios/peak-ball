// Package handlers provides HTTP request handlers.
package handlers

import (
	"net/http"
	"time"

	"go_admin/config"
	"go_admin/database"
	"go_admin/models"
	"go_admin/utils"

	"github.com/gin-gonic/gin"
)

// LoginRequest represents the login request body
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles admin user login
func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request"})
		return
	}

	var user models.AdminUser
	if err := database.DB.Preload("Roles.Menus").Preload("Roles.Permissions").
		Where("username = ?", req.Username).First(&user).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	if user.Status == 0 {
		c.JSON(http.StatusForbidden, gin.H{"error": "Account is disabled"})
		return
	}

	if !utils.CheckPassword(req.Password, user.Password) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid username or password"})
		return
	}

	// Generate JWT token
	token, err := utils.GenerateJWT(user.ID, user.Username, config.JWTSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate token"})
		return
	}

	// Update last login time
	now := time.Now()
	database.DB.Model(&user).Update("last_login", &now)

	c.JSON(http.StatusOK, gin.H{
		"token": token,
		"user":  user,
	})
}

// GetUserInfo returns the current user's information
func GetUserInfo(c *gin.Context) {
	userID, _ := c.Get("user_id")

	var user models.AdminUser
	if err := database.DB.Preload("Roles.Menus").Preload("Roles.Permissions").
		First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Build menu tree and permission list
	menuMap := make(map[uint]bool)
	permMap := make(map[string]bool)
	var menus []models.Menu

	for _, role := range user.Roles {
		for _, menu := range role.Menus {
			if !menuMap[menu.ID] {
				menuMap[menu.ID] = true
				menus = append(menus, menu)
			}
		}
		for _, perm := range role.Permissions {
			permMap[perm.Code] = true
		}
	}

	var permissions []string
	for code := range permMap {
		permissions = append(permissions, code)
	}

	c.JSON(http.StatusOK, gin.H{
		"user":        user,
		"menus":       buildMenuTree(menus, 0),
		"permissions": permissions,
	})
}

// buildMenuTree builds a tree structure from flat menu list
func buildMenuTree(menus []models.Menu, parentID uint) []models.Menu {
	var tree []models.Menu
	for _, menu := range menus {
		if menu.ParentID == parentID {
			menu.Children = buildMenuTree(menus, menu.ID)
			tree = append(tree, menu)
		}
	}
	return tree
}
