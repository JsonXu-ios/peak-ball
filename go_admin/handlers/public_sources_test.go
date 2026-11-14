package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func isolatedPublicSource(t *testing.T) string {
	t.Helper()
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python runtime is not available")
	}
	_, worker, _ := publicSourcePaths()
	if _, err := os.Stat(worker); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "independent.sqlite3")
	t.Setenv("PUBLIC_SOURCE_WORKER", worker)
	t.Setenv("PUBLIC_SOURCE_PYTHON", python)
	t.Setenv("PUBLIC_SOURCE_DB", db)
	return db
}

func TestPublicSourceBridgeUsesDedicatedDatabase(t *testing.T) {
	db := isolatedPublicSource(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := callPublicSource(ctx, "overview", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Matches []any  `json:"matches"`
		Storage string `json:"storage"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 0 || result.Storage != "独立 SQLite / ps_*" {
		t.Fatalf("unexpected independent overview: %s", raw)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatal("independent database was not initialized", err)
	}
}

func TestPublicSourceRoutesRejectInvalidJobsBeforeStartingCollector(t *testing.T) {
	isolatedPublicSource(t)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/collect", RunPublicSourceCollection)
	for _, body := range []string{`{"date":"2026-02-30","limit":1}`, `{"date":"2026-11-14","limit":0}`, `{"date":"2026-11-14","officialId":"../vipc"}`} {
		req := httptest.NewRequest("POST", "/collect", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("invalid job was not rejected: %d %s", response.Code, response.Body.String())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := callPublicSource(ctx, "overview", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Jobs []any `json:"jobs"`
	}
	if json.Unmarshal(raw, &result) != nil || len(result.Jobs) != 0 {
		t.Fatalf("invalid requests created a collector job: %s", raw)
	}
}
