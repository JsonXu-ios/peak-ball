package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// Exercise the actual subprocess bridge and background dispatch using only a
// temporary worker. No network or production database is used by these tests.
func fakePublicSourceWorker(t *testing.T, failCollect bool) string {
	t.Helper()
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python runtime is not available")
	}
	dir := t.TempDir()
	worker, trace := filepath.Join(dir, "worker.py"), filepath.Join(dir, "trace.jsonl")
	script := `import argparse, json, os, sys
p = argparse.ArgumentParser()
p.add_argument('--db')
p.add_argument('--action')
a = p.parse_args()
body = json.load(sys.stdin)
with open(os.environ['PUBLIC_SOURCE_TEST_TRACE'], 'a', encoding='utf-8') as trace:
    trace.write(json.dumps({'action': a.action, 'payload': body, 'db': a.db}) + '\n')
if a.action in ('enqueue', 'enqueue-results'):
    print(json.dumps({'id': 41, 'status': 'queued'}))
elif a.action == 'collect' and os.environ.get('PUBLIC_SOURCE_TEST_FAIL_COLLECT') == '1':
    print(json.dumps({'error': 'test collector failure'}))
    sys.exit(1)
elif a.action == 'result-raw':
    print(json.dumps({'source': 'result-test', 'payload': 'isolated result snapshot'}))
else:
    print(json.dumps({'ok': True}))
`
	if err := os.WriteFile(worker, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PUBLIC_SOURCE_WORKER", worker)
	t.Setenv("PUBLIC_SOURCE_PYTHON", python)
	t.Setenv("PUBLIC_SOURCE_DB", filepath.Join(dir, "unused.sqlite3"))
	t.Setenv("PUBLIC_SOURCE_TEST_TRACE", trace)
	fail := "0"
	if failCollect {
		fail = "1"
	}
	t.Setenv("PUBLIC_SOURCE_TEST_FAIL_COLLECT", fail)
	return trace
}

type publicSourceTestCall struct {
	Action  string         `json:"action"`
	Payload map[string]any `json:"payload"`
}

func waitPublicSourceCalls(t *testing.T, path string, count int) []publicSourceTestCall {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(body)), "\n")
			calls := make([]publicSourceTestCall, len(lines))
			valid := true
			for i, line := range lines {
				if json.Unmarshal([]byte(line), &calls[i]) != nil {
					valid = false
				}
			}
			if valid && len(calls) >= count {
				return calls
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("background worker did not complete expected dispatch")
	return nil
}

func TestPublicSourceJobDispatchPreservesCollectionAndAddsResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, action string
		handler      gin.HandlerFunc
		fail         bool
	}{
		{"collection", "enqueue", RunPublicSourceCollection, false},
		{"results", "enqueue-results", RunPublicSourceResults, false},
		{"result failure cleanup", "enqueue-results", RunPublicSourceResults, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace := fakePublicSourceWorker(t, test.fail)
			router := gin.New()
			router.POST("/job", test.handler)
			req := httptest.NewRequest("POST", "/job", strings.NewReader(`{"date":"2026-11-15","officialId":"2042771"}`))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != 202 || !strings.Contains(response.Body.String(), `"queued"`) {
				t.Fatalf("expected queued response: %d %s", response.Code, response.Body.String())
			}
			count := 2
			if test.fail {
				count = 3
			}
			calls := waitPublicSourceCalls(t, trace, count)
			if calls[0].Action != test.action || calls[0].Payload["date"] != "2026-11-15" || calls[0].Payload["officialId"] != "2042771" {
				t.Fatalf("wrong enqueue action or payload: %+v", calls)
			}
			if calls[1].Action != "collect" || calls[1].Payload["id"] != float64(41) || len(calls[1].Payload) != 1 {
				t.Fatalf("worker must dispatch solely by queued job ID: %+v", calls)
			}
			if test.fail && (calls[2].Action != "fail-job" || calls[2].Payload["id"] != float64(41)) {
				t.Fatalf("failed background execution must mark the same job: %+v", calls)
			}
		})
	}
}

func TestPublicSourceResultRawUsesSeparateSnapshotAction(t *testing.T) {
	trace := fakePublicSourceWorker(t, false)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/results/raw", GetPublicSourceResultRaw)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/results/raw?id=match-1&snapshotId=3", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "isolated result snapshot") {
		t.Fatalf("unexpected result snapshot response: %d %s", response.Code, response.Body.String())
	}
	calls := waitPublicSourceCalls(t, trace, 1)
	if calls[0].Action != "result-raw" || calls[0].Payload["id"] != "match-1" || calls[0].Payload["snapshotId"] != "3" {
		t.Fatalf("result snapshot must not query the prediction snapshot action: %+v", calls)
	}
}
