package handlers

import (
	"github.com/gin-gonic/gin"
	"time"
)

// The protected admin router keeps research operations behind the same login
// as the observations page; no database credentials enter the browser.
func GetResearchOverviewAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/research/overview", 30*time.Second)
}
func GetResearchSyncStatusAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/research/sync/status", 15*time.Second)
}
func StartResearchSyncAdmin(c *gin.Context) { proxyDrawAdmin(c, "/research/sync", 15*time.Second) }

func GetResearchFeaturesAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/research/features", 20*time.Second)
}
func GetResearchIssuesAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/research/audit/issues", 20*time.Second)
}
func GetResearchEvaluationsAdmin(c *gin.Context) {
	proxyDrawAdmin(c, "/research/evaluations", 20*time.Second)
}
