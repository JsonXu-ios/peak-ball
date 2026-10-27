package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go_admin/config"
)

func TestDrawCalendarAdminForwardsMonthScopeModeWithoutCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/draw/observations/dates?month=2026-10&scope=sporttery&mode=history" {
			t.Errorf("unexpected calendar URI: %s", r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("calendar proxy leaked admin credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"month":"2026-10","dates":[{"date":"2026-10-20","count":14}]}`))
	}))
	defer upstream.Close()
	previous := config.AnalysisAPIBaseURL
	config.AnalysisAPIBaseURL = upstream.URL + "/api"
	defer func() { config.AnalysisAPIBaseURL = previous }()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/statistics/draw/observations/dates?month=2026-10&scope=sporttery&mode=history", nil)
	c.Request.Header.Set("Authorization", "Bearer private")
	c.Request.Header.Set("Cookie", "private=true")
	GetDrawObservationDatesAdmin(c)
	if w.Code != http.StatusOK || w.Body.String() != `{"month":"2026-10","dates":[{"date":"2026-10-20","count":14}]}` {
		t.Fatalf("calendar proxy changed response: %d %s", w.Code, w.Body.String())
	}
}
