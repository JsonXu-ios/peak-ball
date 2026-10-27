package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/database"
	"go_server/models"
	"gorm.io/gorm"
)

type drawObservationCalendarQuery struct {
	month, mode, scope string
}

type drawObservationCalendarDate struct {
	Date       string `json:"date"`
	Count      int    `json:"count"`
	MatchCount int    `json:"matchCount"`
	DrawCount  int    `json:"drawCount"`
}

type drawObservationCalendarResponse struct {
	Month            string                        `json:"month"`
	Mode             string                        `json:"mode"`
	Scope            string                        `json:"scope"`
	ScheduleTimezone string                        `json:"scheduleTimezone"`
	Dates            []drawObservationCalendarDate `json:"dates"`
}

func parseDrawObservationCalendarQuery(c *gin.Context, now time.Time) (drawObservationCalendarQuery, error) {
	q := drawObservationCalendarQuery{month: c.Query("month"), mode: c.DefaultQuery("mode", "today"), scope: c.DefaultQuery("scope", "all")}
	if q.month == "" {
		q.month = now.In(drawSourceLocation).Format("2006-01")
	}
	if month, err := time.Parse("2006-01", q.month); err != nil || month.Format("2006-01") != q.month || month.Year() < 1 {
		return q, fmt.Errorf("月份格式应为 YYYY-MM")
	}
	if q.mode != "today" && q.mode != "history" {
		return q, fmt.Errorf("mode 应为 today 或 history")
	}
	if q.scope != "all" && q.scope != "sporttery" {
		return q, fmt.Errorf("scope 应为 all 或 sporttery")
	}
	return q, nil
}

// GetDrawObservationDates reads a month of small match-table headers only. It
// never builds observation days, parses history/market JSON, or writes Redis.
// Calendar markers intentionally ignore league/search/draw-count filters.
func GetDrawObservationDates(c *gin.Context) {
	getDrawObservationDates(c, readDrawObservationCalendar)
}

func getDrawObservationDates(c *gin.Context, load func(drawObservationCalendarQuery) (drawObservationCalendarResponse, error)) {
	q, err := parseDrawObservationCalendarQuery(c, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	response, err := load(q)
	if err != nil {
		drawHTTPError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

// Keep the stored source date verbatim. A schedule day may intentionally differ
// from the calendar day of kickoff, so UTC conversion must not choose markers.
func buildDrawObservationCalendar(targets []drawObservationTarget, q drawObservationCalendarQuery) drawObservationCalendarResponse {
	response := drawObservationCalendarResponse{Month: q.month, Mode: q.mode, Scope: q.scope, ScheduleTimezone: drawSourceLocation.String(), Dates: []drawObservationCalendarDate{}}
	days := map[string]drawObservationCalendarDate{}
	for _, target := range targets {
		match := target.Match
		if !drawObservationScopeMatches(match, q.scope) {
			continue
		}
		date := drawObservationDate(match)
		if len(date) != 10 || !strings.HasPrefix(date, q.month+"-") {
			continue
		}
		day := days[date]
		day.Date = date
		day.MatchCount++
		if home, away, settled := drawRegulationResult(match); settled && home == away {
			day.DrawCount++
		}
		days[date] = day
	}
	for _, day := range days {
		day.Count = day.MatchCount
		if q.mode == "history" {
			day.Count = day.DrawCount
		}
		if day.Count > 0 {
			response.Dates = append(response.Dates, day)
		}
	}
	sort.Slice(response.Dates, func(i, j int) bool { return response.Dates[i].Date < response.Dates[j].Date })
	return response
}

var drawObservationCalendarColumns = []string{"match_id", "jingcai_id", "date", "match_time", "display_state", "home_score", "guest_score", "home_ot_score", "guest_ot_score", "home_ot_penalty", "guest_ot_penalty"}

func drawObservationCalendarSQLQuery(db *gorm.DB, schema drawObservationSchema, table string, q drawObservationCalendarQuery, ids []string) *gorm.DB {
	query := db.Table(table).Select(drawObservationProjection(schema, table, drawObservationCalendarColumns))
	if ids != nil {
		return query.Where("match_id IN ?", ids)
	}
	month, _ := time.Parse("2006-01", q.month)
	// A half-open range preserves the existing date index. No DATE()/UTC
	// expression, per-day query, history join or JSON projection is needed.
	return query.Where("date >= ? AND date < ?", q.month+"-01", month.AddDate(0, 1, 0).Format("2006-01-02"))
}

// Reading monthly rows and resolving legacy IDs' current-table headers
// prevents a moved fixture or changed sporttery ID from reappearing on its old
// day. The supplemental query is bounded by the legacy month's missing IDs.
func loadDrawObservationCalendarTargets(schema drawObservationSchema, read func(string, []string) ([]models.Money, error)) ([]drawObservationTarget, error) {
	sets := map[string][]models.Money{}
	for _, table := range []string{"moneys", "money"} {
		if !schema[table]["match_id"] {
			continue
		}
		rows, err := read(table, nil)
		if err != nil {
			return nil, err
		}
		sets[table] = rows
	}
	if schema["moneys"]["match_id"] && len(sets["money"]) > 0 {
		known := map[string]bool{}
		for _, match := range sets["moneys"] {
			known[match.MatchId] = true
		}
		ids := []string{}
		for _, match := range sets["money"] {
			if strings.TrimSpace(match.MatchId) != "" && !known[match.MatchId] {
				ids = append(ids, match.MatchId)
				known[match.MatchId] = true
			}
		}
		if len(ids) > 0 {
			rows, err := read("moneys", ids)
			if err != nil {
				return nil, err
			}
			sets["moneys"] = append(sets["moneys"], rows...)
		}
	}
	return mergeDrawObservationTargets(sets["moneys"], sets["money"]), nil
}

func readDrawObservationCalendar(q drawObservationCalendarQuery) (response drawObservationCalendarResponse, err error) {
	if database.DB == nil {
		return response, fmt.Errorf("数据库尚未连接")
	}
	err = database.DB.Transaction(func(tx *gorm.DB) error {
		schema, schemaErr := drawObservationReadSchema(tx)
		if schemaErr != nil {
			return schemaErr
		}
		targets, loadErr := loadDrawObservationCalendarTargets(schema, func(table string, ids []string) ([]models.Money, error) {
			rows := []models.Money{}
			readErr := drawObservationCalendarSQLQuery(tx, schema, table, q, ids).Find(&rows).Error
			return rows, readErr
		})
		if loadErr != nil {
			return loadErr
		}
		response = buildDrawObservationCalendar(targets, q)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return response, err
}
