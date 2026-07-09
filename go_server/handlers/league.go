// Package handlers implements HTTP request handlers for league standings.
package handlers

import (
	"net/http"
	"strconv"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

// GetLeagueStandings returns standings for a given league.
// Query param ?leagueId=36 &season=2025-2026.
func GetLeagueStandings(c *gin.Context) {
	leagueIDStr := c.DefaultQuery("leagueId", "36")
	season := c.DefaultQuery("season", "2025-2026")

	leagueID, err := strconv.Atoi(leagueIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid leagueId"})
		return
	}

	var standings []models.LeagueStanding
	if err := database.DB.Where("league_id = ? AND season = ?", leagueID, season).
		Order("`rank` ASC").
		Find(&standings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, standings)
}

// GetTopScorers returns the top scorers for a given league.
// Query param ?leagueId=36 &season=2025-2026 &type=goals|assists.
func GetTopScorers(c *gin.Context) {
	leagueIDStr := c.DefaultQuery("leagueId", "36")
	season := c.DefaultQuery("season", "2025-2026")

	leagueID, err := strconv.Atoi(leagueIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid leagueId"})
		return
	}

	orderField := "goals DESC"
	if c.Query("type") == "assists" {
		orderField = "assists DESC"
	}

	var scorers []models.TopScorer
	if err := database.DB.Where("league_id = ? AND season = ?", leagueID, season).
		Order(orderField).
		Limit(20).
		Find(&scorers).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, scorers)
}
