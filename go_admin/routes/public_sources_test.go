package routes

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPublicSourceResultRoutesRemainInProtectedGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	SetupPublicRoutes(router)
	group := router.Group("/api/admin", func(c *gin.Context) { c.AbortWithStatus(401) })
	SetupProtectedRoutes(group)
	want := map[string]string{
		"POST /api/admin/public-sources/results":    "RunPublicSourceResults",
		"GET /api/admin/public-sources/results/raw": "GetPublicSourceResultRaw",
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if handler, ok := want[key]; ok {
			if !strings.HasSuffix(route.Handler, "."+handler) {
				t.Fatalf("unexpected result route handler: %+v", route)
			}
			delete(want, key)
		}
		if strings.Contains(route.Path, "/public-sources/results") && !strings.HasPrefix(route.Path, "/api/admin/") {
			t.Fatalf("result operation escaped the protected group: %+v", route)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing result routes: %v", want)
	}
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/admin/public-sources/results"},
		{"GET", "/api/admin/public-sources/results/raw?id=match-1&snapshotId=3"},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(route.method, route.path, strings.NewReader(`{"date":"2026-11-15"}`)))
		if response.Code != 401 {
			t.Fatalf("result operation bypassed group middleware: %s %d", route.path, response.Code)
		}
	}
}
