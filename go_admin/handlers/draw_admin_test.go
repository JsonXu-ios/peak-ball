package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"go_admin/config"

	"github.com/gin-gonic/gin"
)

func TestDrawAdminProxyPreservesRequestWithoutAdminCredentials(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/api/draw/matches?date=2026-10-20&league=a%20b" {
			t.Errorf("request URI = %s", r.URL.RequestURI())
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("admin credentials forwarded to analysis service")
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"keep":true}` {
			t.Errorf("request body = %s", body)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer upstream.Close()
	previousBase := config.AnalysisAPIBaseURL
	config.AnalysisAPIBaseURL = upstream.URL + "/api"
	defer func() { config.AnalysisAPIBaseURL = previousBase }()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/statistics/draw/matches?date=2026-10-20&league=a%20b", strings.NewReader(`{"keep":true}`))
	c.Request.Header.Set("Authorization", "Bearer admin-only")
	c.Request.Header.Set("Cookie", "admin-session=private")
	c.Request.Header.Set("Content-Type", "application/json")
	GetDrawMatchesAdmin(c)
	if w.Code != http.StatusAccepted || w.Body.String() != `{"accepted":true}` || w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("response was not preserved: code=%d type=%s body=%s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}

func TestDrawExistingAnalysisDoesNotExportMatchOutcome(t *testing.T) {
	fixture := statisticsMatch{ID: "fixture", Date: "2026-10-20", Home: "H", Guest: "G", MatchTime: "2026-10-20 20:00:00"}
	history := map[string]interface{}{
		"against_list":      `[{"matchTime":"2026-09-01 20:00:00","home":"H","guest":"G","goal":[1,1]}]`,
		"recent_home_list":  `[{"matchTime":"2026-10-10 20:00:00","home":"H","guest":"X","goal":[1,1]}]`,
		"recent_guest_list": `[{"matchTime":"2026-10-10 20:00:00","home":"Y","guest":"G","goal":[1,1]}]`,
	}
	odds := map[string]interface{}{"avg_odds": `{"odds":[2.8,3.1,2.8]}`}
	pankou := map[string]interface{}{"bet365_asia": `{"pankou":"平手","odds":[0.9,0.9]}`, "bet365_dxq": `{"pankou":"二球半","odds":[0.9,0.9]}`}
	catalogue := recommendCatalogue()
	before := buildDrawExistingAnalysis(fixture, history, pankou, odds, catalogue)
	fixture.HomeScore, fixture.GuestScore, fixture.Settled, fixture.State = 9, 2, true, "完场"
	after := buildDrawExistingAnalysis(fixture, history, pankou, odds, catalogue)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("analysis export changes when only the fixture result changes")
	}
	if len(after.Features) == 0 || len(after.Signals) == 0 || after.Provenance != drawAnalysisProvenance {
		t.Fatalf("expected usable, explicitly unverified analysis features: %+v", after)
	}
	encoded, err := json.Marshal(after)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{`"hit"`, `"result"`, `"homeScore"`, `"awayScore"`, `"settled"`} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("outcome field %s escaped into features", forbidden)
		}
	}
}
