package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"
)

// This bridge deliberately has no reference to database.DB or VIPC models.
// SQLite, snapshots, jobs and derived analysis are owned by public_sources.
func publicSourcePaths() (python, worker, db string) {
	python = os.Getenv("PUBLIC_SOURCE_PYTHON")
	if python == "" {
		python = "python"
	}
	worker = os.Getenv("PUBLIC_SOURCE_WORKER")
	if worker == "" {
		_, source, _, _ := runtime.Caller(0)
		executable, _ := os.Executable()
		candidates := []string{
			filepath.Join("..", "scripts", "public_sources", "worker.py"),
			filepath.Join("scripts", "public_sources", "worker.py"),
			filepath.Join(filepath.Dir(executable), "..", "scripts", "public_sources", "worker.py"),
			filepath.Join(filepath.Dir(source), "..", "..", "scripts", "public_sources", "worker.py"),
		}
		for _, candidate := range candidates {
			if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
				worker = candidate
				break
			}
		}
		if worker == "" {
			worker = candidates[0]
		}
	}
	worker, _ = filepath.Abs(worker)
	db = os.Getenv("PUBLIC_SOURCE_DB")
	if db == "" {
		db = filepath.Join(filepath.Dir(worker), "..", "..", "data", "public-sources", "public_sources.sqlite3")
	}
	db, _ = filepath.Abs(db)
	return
}

func callPublicSource(ctx context.Context, action string, payload any) (json.RawMessage, error) {
	python, worker, db := publicSourcePaths()
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, python, worker, "--db", db, "--action", action)
	hidePublicSourceProcess(cmd)
	cmd.Stdin = bytes.NewReader(body)
	// Do not echo process environment, stderr, proxy URLs or credentials.
	output, runErr := cmd.Output()
	var result map[string]json.RawMessage
	if json.Unmarshal(output, &result) != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("独立采集操作超时")
		}
		return nil, fmt.Errorf("独立采集进程不可用，请检查Python及PUBLIC_SOURCE_WORKER配置")
	}
	if raw, ok := result["error"]; ok {
		var message string
		_ = json.Unmarshal(raw, &message)
		return nil, fmt.Errorf("%s", message)
	}
	if runErr != nil {
		return nil, fmt.Errorf("独立采集进程执行失败")
	}
	return json.RawMessage(output), nil
}

func publicSourceResponse(c *gin.Context, action string, payload any) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	result, err := callPublicSource(ctx, action, payload)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.Data(200, "application/json; charset=utf-8", result)
}

func GetPublicSourceOverview(c *gin.Context) {
	publicSourceResponse(c, "overview", gin.H{"date": c.Query("date")})
}

func GetPublicSourceMatch(c *gin.Context) {
	publicSourceResponse(c, "detail", gin.H{"id": c.Query("id")})
}

func GetPublicSourceRaw(c *gin.Context) {
	publicSourceResponse(c, "raw", gin.H{"id": c.Query("id"), "snapshotId": c.Query("snapshotId")})
}

func UpdatePublicSourceMapping(c *gin.Context) {
	var payload struct {
		ID      string `json:"id" binding:"required"`
		ZgzcwID string `json:"zgzcwId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(400, gin.H{"error": "请填写比赛与足彩网ID"})
		return
	}
	publicSourceResponse(c, "mapping", payload)
}

func UpdatePublicSourceCompanyMapping(c *gin.Context) {
	var payload struct {
		Label   string `json:"label" binding:"required"`
		Company string `json:"company" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(400, gin.H{"error": "请填写来源标签和公司名"})
		return
	}
	publicSourceResponse(c, "company-mapping", payload)
}

func RunPublicSourceCollection(c *gin.Context) {
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(400, gin.H{"error": "请填写采集日期"})
		return
	}
	// json.Unmarshal into map turns integers into float64. Python receives a
	// JSON integer when encoded by Go, so worker validation stays strict.
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	result, err := callPublicSource(ctx, "enqueue", payload)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var job struct {
		ID int64 `json:"id"`
	}
	if json.Unmarshal(result, &job) != nil || job.ID <= 0 {
		c.JSON(500, gin.H{"error": "独立任务初始化失败"})
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if _, err := callPublicSource(ctx, "collect", gin.H{"id": job.ID}); err != nil {
			cleanup, done := context.WithTimeout(context.Background(), 20*time.Second)
			defer done()
			_, _ = callPublicSource(cleanup, "fail-job", gin.H{"id": job.ID})
		}
	}()
	c.Data(202, "application/json; charset=utf-8", result)
}
