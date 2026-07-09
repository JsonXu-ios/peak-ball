// Package routes defines API routes.
package routes

import (
	"go_admin/handlers"

	"github.com/gin-gonic/gin"
)

// SetupPublicRoutes sets up public routes (no authentication required)
func SetupPublicRoutes(r *gin.Engine) {
	api := r.Group("/api")
	{
		// Authentication
		api.POST("/login", handlers.Login)
	}
}

// SetupProtectedRoutes sets up protected routes (authentication required)
func SetupProtectedRoutes(r *gin.RouterGroup) {
	// User management
	r.GET("/user/info", handlers.GetUserInfo)
	r.GET("/users", handlers.GetUsers)
	r.POST("/users", handlers.CreateUser)
	r.PUT("/users/:id", handlers.UpdateUser)
	r.DELETE("/users/:id", handlers.DeleteUser)
	r.PATCH("/users/:id/status", handlers.UpdateUserStatus)
	r.PATCH("/users/:id/password", handlers.ResetPassword)

	// Role management
	r.GET("/roles", handlers.GetRoles)
	r.POST("/roles", handlers.CreateRole)
	r.PUT("/roles/:id", handlers.UpdateRole)
	r.DELETE("/roles/:id", handlers.DeleteRole)
	r.GET("/roles/:id/menus", handlers.GetRoleMenus)
	r.PUT("/roles/:id/menus", handlers.UpdateRoleMenus)
	r.GET("/roles/:id/permissions", handlers.GetRolePermissions)
	r.PUT("/roles/:id/permissions", handlers.UpdateRolePermissions)

	// Menu management
	r.GET("/menus", handlers.GetMenus)
	r.GET("/menus/tree", handlers.GetMenuTree)
	r.POST("/menus", handlers.CreateMenu)
	r.PUT("/menus/:id", handlers.UpdateMenu)
	r.DELETE("/menus/:id", handlers.DeleteMenu)

	// Permission management
	r.GET("/permissions", handlers.GetPermissions)
	r.POST("/permissions", handlers.CreatePermission)
	r.PUT("/permissions/:id", handlers.UpdatePermission)
	r.DELETE("/permissions/:id", handlers.DeletePermission)

	// Crawler data management
	r.GET("/crawler/matches", handlers.GetCrawlerMatches)
	r.GET("/crawler/matches/:id", handlers.GetCrawlerMatchDetail)
	r.DELETE("/crawler/matches/:id", handlers.DeleteCrawlerMatch)
	r.POST("/crawler/sync", handlers.SyncCrawlerData)
	r.GET("/crawler/analysis-rule-snapshot", handlers.GetAnalysisRuleSnapshotInfo)
	r.GET("/crawler/analysis-rule-snapshot/data", handlers.GetAnalysisRuleSnapshotData)
	r.POST("/crawler/analysis-rule-snapshot/generate", handlers.GenerateAnalysisRuleSnapshot)

	// Crawler task management
	r.GET("/crawler/tasks", handlers.GetCrawlerTasks)
	r.POST("/crawler/tasks", handlers.CreateCrawlerTask)
	r.PUT("/crawler/tasks/:id", handlers.UpdateCrawlerTask)
	r.DELETE("/crawler/tasks/:id", handlers.DeleteCrawlerTask)
	r.POST("/crawler/tasks/:id/run", handlers.RunCrawlerTask)
	r.PATCH("/crawler/tasks/:id/toggle", handlers.ToggleCrawlerTask)

	// Crawler logs
	r.GET("/crawler/logs", handlers.GetCrawlerLogs)
	r.GET("/crawler/logs/:id", handlers.GetCrawlerLogDetail)

	// Operation logs
	r.GET("/logs/operations", handlers.GetOperationLogs)

	// Dashboard statistics
	r.GET("/dashboard/stats", handlers.GetDashboardStats)
}
