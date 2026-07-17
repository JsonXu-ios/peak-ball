// Package handlers: picks.go records the site owner's betting choices for
// completed matches. The entry endpoint deliberately MASKS the real result
// (scores/display state) so backfilling picks is blind and unbiased.
package handlers

import (
	"net/http"
	"strings"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

var pickMarkets = map[string]bool{"spf": true, "rqspf": true, "dxq": true, "score": true}
var pickDirections = map[string]bool{"follow": true, "fade": true, "self": true}

// pickEntryRow is a full analysis row (platform block included), plus any picks
// already recorded. Settled matches have the real result stripped (blind
// backfill); upcoming matches keep their state so picks can be recorded
// pre-match (source=live).
type pickEntryRow struct {
	analysisMatchResponse
	Settled bool              `json:"settled"`
	Picks   []models.UserPick `json:"picks"`
}

func pickMatchSettled(match models.Money) bool {
	return strings.Contains(match.DisplayState, "完") || match.Status >= 4
}

// GetPickEntryMatches returns completed matches for a date with results hidden.
func GetPickEntryMatches(c *gin.Context) {
	startDate, endDate, err := analysisDateWindow(c.Query("date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}
	var matches []models.Money
	query := database.DB.Where("date BETWEEN ? AND ?", startDate, endDate)
	if c.Query("scope") != "all" {
		query = query.Where("jingcai_id IS NOT NULL AND TRIM(jingcai_id) <> ?", "")
	}
	query = query.Where("display_state IS NULL OR display_state <> ?", detailOnlyDisplayState)
	if err := query.Order("match_time ASC").Find(&matches).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.MatchId)
	}

	picksByMatch := map[string][]models.UserPick{}
	if len(ids) > 0 {
		var picks []models.UserPick
		if err := database.DB.Where("match_id IN ?", ids).Find(&picks).Error; err == nil {
			for _, pick := range picks {
				picksByMatch[pick.MatchId] = append(picksByMatch[pick.MatchId], pick)
			}
		}
	}

	rows := make([]pickEntryRow, 0, len(matches))
	for _, match := range matches {
		settled := pickMatchSettled(match)
		response := buildAnalysisWithWeights(match, false)
		if settled {
			// Blind backfill: hide anything that reveals the final result.
			response.HomeScore = -1
			response.GuestScore = -1
			response.DisplayState = "已完赛"
		}
		picks := picksByMatch[match.MatchId]
		if picks == nil {
			picks = []models.UserPick{}
		}
		rows = append(rows, pickEntryRow{analysisMatchResponse: response, Settled: settled, Picks: picks})
	}
	c.JSON(http.StatusOK, rows)
}

type savePickRequest struct {
	MatchId    string   `json:"matchId" binding:"required"`
	Market     string   `json:"market" binding:"required"`
	Pick       string   `json:"pick" binding:"required"`
	Line       *float64 `json:"line"`
	Direction  string   `json:"direction"`
	Confidence int      `json:"confidence"`
	Note       string   `json:"note"`
	Source     string   `json:"source"`
}

// SaveUserPick upserts one pick keyed by (matchId, market).
func SaveUserPick(c *gin.Context) {
	var request savePickRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !pickMarkets[request.Market] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "market must be spf/rqspf/dxq/score"})
		return
	}
	if request.Direction == "" {
		request.Direction = "self"
	}
	if !pickDirections[request.Direction] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "direction must be follow/fade/self"})
		return
	}
	if request.Confidence < 0 || request.Confidence > 3 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "confidence must be 0-3"})
		return
	}
	source := request.Source
	if source == "" {
		source = "backfill"
	}

	var existing models.UserPick
	err := database.DB.Where("match_id = ? AND market = ?", request.MatchId, request.Market).First(&existing).Error
	pick := models.UserPick{
		MatchId:    request.MatchId,
		Market:     request.Market,
		Pick:       strings.TrimSpace(request.Pick),
		Line:       request.Line,
		Direction:  request.Direction,
		Confidence: request.Confidence,
		Note:       strings.TrimSpace(request.Note),
		Source:     source,
	}
	if err == nil {
		pick.ID = existing.ID
		pick.CreatedAt = existing.CreatedAt
		if saveErr := database.DB.Save(&pick).Error; saveErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": saveErr.Error()})
			return
		}
	} else if createErr := database.DB.Create(&pick).Error; createErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": createErr.Error()})
		return
	}
	c.JSON(http.StatusOK, pick)
}

// DeleteUserPick removes one recorded pick by id.
func DeleteUserPick(c *gin.Context) {
	if err := database.DB.Delete(&models.UserPick{}, "id = ?", c.Param("id")).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": true})
}

// ListUserPicks returns every recorded pick (newest first) for export/stats.
func ListUserPicks(c *gin.Context) {
	var picks []models.UserPick
	query := database.DB.Order("created_at DESC")
	if matchID := strings.TrimSpace(c.Query("match_id")); matchID != "" {
		query = query.Where("match_id = ?", matchID)
	}
	if err := query.Find(&picks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, picks)
}
