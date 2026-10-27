package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/models"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func drawCalendarMatch(id string, day int, score int, state, jc string) models.Money {
	return models.Money{MatchId: id, Date: time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC), MatchTime: time.Date(2026, 10, day+1, 2, 0, 0, 0, time.UTC), HomeScore: score, GuestScore: score, DisplayState: state, JingcaiID: jc}
}

func TestDrawObservationCalendarScopeModeAndSourceDatesMatchList(t *testing.T) {
	current := []models.Money{
		drawCalendarMatch("draw", 20, 1, "完场", "周二001"),
		drawCalendarMatch("pending", 20, 0, "未开赛", "周二002"),
		drawCalendarMatch("cancelled", 20, 0, "取消", ""),
		drawCalendarMatch("extra", 21, 1, "完场", "周三001"),
		drawCalendarMatch("won", 21, 1, "完场", ""),
		drawCalendarMatch("legacy-duplicate", 22, 0, "未开赛", ""),
		drawCalendarMatch("detail-only", 22, 1, detailOnlyDisplayState, "周四001"),
	}
	current[3].HomeOtScore = 1
	current[4].HomeScore = 2
	legacy := []models.Money{drawCalendarMatch("legacy-duplicate", 20, 1, "完场", "周二003"), drawCalendarMatch("legacy", 21, 0, "完场", " ")}
	targets := mergeDrawObservationTargets(current, legacy)
	for _, mode := range []string{"today", "history"} {
		for _, scope := range []string{"all", "sporttery"} {
			t.Run(mode+"/"+scope, func(t *testing.T) {
				got := buildDrawObservationCalendar(targets, drawObservationCalendarQuery{month: "2026-10", mode: mode, scope: scope})
				if got.ScheduleTimezone != "Asia/Shanghai" || got.Dates == nil {
					t.Fatalf("lost Beijing contract or empty array: %+v", got)
				}
				markers := map[string]drawObservationCalendarDate{}
				for _, day := range got.Dates {
					markers[day.Date] = day
				}
				for day := 1; day <= 31; day++ {
					date := fmt.Sprintf("2026-10-%02d", day)
					list := buildDrawObservations(drawObservationData{Targets: targets}, drawObservationQuery{mode: mode, scope: scope, date: date, page: 1, size: 200})
					if markers[date].Count != list.Total {
						t.Errorf("%s count=%d list=%d", date, markers[date].Count, list.Total)
					}
				}
				if mode == "today" && scope == "all" && (!reflect.DeepEqual(got.Dates, []drawObservationCalendarDate{{Date: "2026-10-20", Count: 3, MatchCount: 3, DrawCount: 1}, {Date: "2026-10-21", Count: 3, MatchCount: 3, DrawCount: 1}, {Date: "2026-10-22", Count: 1, MatchCount: 1, DrawCount: 0}})) {
					t.Fatalf("unexpected daily counts: %+v", got.Dates)
				}
			})
		}
	}
	if got := buildDrawObservationCalendar(targets, drawObservationCalendarQuery{month: "2026-11", mode: "today", scope: "all"}); len(got.Dates) != 0 || got.Dates == nil {
		t.Fatalf("empty month must return an array: %+v", got)
	}
}

func TestDrawObservationCalendarResolvesCrossMonthCurrentAuthorityBeforeScope(t *testing.T) {
	monthCurrent := []models.Money{drawCalendarMatch("current", 21, 1, "完场", "周三001"), drawCalendarMatch("changed-id", 22, 1, "完场", "")}
	legacy := []models.Money{drawCalendarMatch("moved", 20, 1, "完场", "周二001"), drawCalendarMatch("changed-id", 20, 1, "完场", "周二002"), drawCalendarMatch("legacy", 20, 1, "完场", "周二003"), drawCalendarMatch("legacy", 20, 1, "完场", "周二003")}
	moved := drawCalendarMatch("moved", 20, 1, "完场", "周二001")
	moved.Date = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var calls []string
	targets, err := loadDrawObservationCalendarTargets(drawObservationSchema{"moneys": {"match_id": true}, "money": {"match_id": true}}, func(table string, ids []string) ([]models.Money, error) {
		calls = append(calls, table+":"+strings.Join(ids, ","))
		if ids != nil {
			if table != "moneys" || !reflect.DeepEqual(ids, []string{"moved", "legacy"}) {
				t.Fatalf("authority query must use only deduplicated missing month IDs: %s %v", table, ids)
			}
			return []models.Money{moved}, nil
		}
		if table == "moneys" {
			return monthCurrent, nil
		}
		return legacy, nil
	})
	if err != nil || len(calls) != 3 {
		t.Fatalf("expected at most three header reads: %v %v", calls, err)
	}
	got := buildDrawObservationCalendar(targets, drawObservationCalendarQuery{month: "2026-10", mode: "history", scope: "sporttery"})
	want := []drawObservationCalendarDate{{Date: "2026-10-20", Count: 1, MatchCount: 1, DrawCount: 1}, {Date: "2026-10-21", Count: 1, MatchCount: 1, DrawCount: 1}}
	if !reflect.DeepEqual(got.Dates, want) {
		t.Fatalf("stale day or old sporttery ID resurrected: got %+v want %+v", got.Dates, want)
	}
}

func TestDrawObservationCalendarValidatesBeforeAnyReadAndUsesBeijingMonth(t *testing.T) {
	for _, query := range []string{"month=2026-2", "month=2026-13", "month=0000-01", "month=2026-10-01", "mode=counts", "scope=jingcai"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/draw/observations/dates?"+query, nil)
		getDrawObservationDates(c, func(drawObservationCalendarQuery) (drawObservationCalendarResponse, error) {
			t.Fatal("invalid query reached database loader")
			return drawObservationCalendarResponse{}, nil
		})
		if w.Code != http.StatusBadRequest {
			t.Errorf("query %s status=%d", query, w.Code)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/draw/observations/dates", nil)
	q, err := parseDrawObservationCalendarQuery(c, time.Date(2026, 10, 31, 18, 0, 0, 0, time.UTC))
	if err != nil || q.month != "2026-11" || q.mode != "today" || q.scope != "all" {
		t.Fatalf("default month must cross midnight in Beijing: %+v %v", q, err)
	}
	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/draw/observations/dates?month=2026-10&mode=history&scope=sporttery&league=other&home_draws_min=10", nil)
	getDrawObservationDates(c, func(query drawObservationCalendarQuery) (drawObservationCalendarResponse, error) {
		if query != (drawObservationCalendarQuery{month: "2026-10", mode: "history", scope: "sporttery"}) {
			t.Fatalf("calendar unexpectedly consumed list filters: %+v", query)
		}
		return buildDrawObservationCalendar(nil, query), nil
	})
	var response drawObservationCalendarResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Dates == nil || w.Code != http.StatusOK {
		t.Fatalf("invalid success envelope: code=%d body=%s error=%v", w.Code, w.Body.String(), err)
	}
}

func TestDrawObservationCalendarSQLIsIndexedHeaderOnlyAndBounded(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: &sql.DB{}, SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	schema := drawObservationSchema{"moneys": {"match_id": true, "date": true, "match_time": true, "display_state": true, "home_score": true, "guest_score": true, "data": true, "recent_home_list": true}}
	q := drawObservationCalendarQuery{month: "2026-12", mode: "today", scope: "all"}
	statement := drawObservationCalendarSQLQuery(db, schema, "moneys", q, nil).Find(&[]models.Money{}).Statement
	query := statement.SQL.String()
	if !strings.Contains(query, "date >= ? AND date < ?") || strings.Contains(query, "DATE(") || strings.Contains(query, "data") || strings.Contains(query, "recent_home_list") || strings.Contains(query, "jingcai_id") || !reflect.DeepEqual(statement.Vars, []interface{}{"2026-12-01", "2027-01-01"}) {
		t.Fatalf("monthly query lost range/projection/schema bounds: %s %v", query, statement.Vars)
	}
	statement = drawObservationCalendarSQLQuery(db, schema, "moneys", q, []string{"moved"}).Find(&[]models.Money{}).Statement
	if !strings.Contains(statement.SQL.String(), "match_id IN (?)") || strings.Contains(statement.SQL.String(), "date >=") {
		t.Fatalf("authority read must resolve moved IDs across dates: %s", statement.SQL.String())
	}
	_, err = loadDrawObservationCalendarTargets(schema, func(string, []string) ([]models.Money, error) { return nil, errors.New("read failed") })
	if err == nil {
		t.Fatal("partial SQL read must not publish incomplete markers")
	}
}
