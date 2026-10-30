package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/config"
	"go_server/database"
	"peakball/researchstore"
)

const researchPipelineVersion = "research-pipeline-v1"
const researchFeatureVersion = "observation-research-features-v1"
const researchEvaluationVersion = "research-evaluation-v3-dynamic-scores"

type ResearchSyncStatus struct {
	State            string     `json:"state"`
	RunID            int64      `json:"runId,omitempty"`
	DatasetID        int64      `json:"datasetId,omitempty"`
	SourceRevision   string     `json:"sourceRevision,omitempty"`
	PipelineCodeHash string     `json:"pipelineCodeHash,omitempty"`
	Unchanged        bool       `json:"unchanged,omitempty"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	FinishedAt       *time.Time `json:"finishedAt,omitempty"`
	Processed        int        `json:"processed"`
	Total            int        `json:"total"`
	Message          string     `json:"message"`
	Error            string     `json:"error,omitempty"`
}

var researchJob = struct {
	sync.Mutex
	status ResearchSyncStatus
}{status: ResearchSyncStatus{State: "idle", Message: "等待同步本地数据"}}

func OpenResearchStore(ctx context.Context) (*researchstore.Store, error) {
	return researchstore.Open(ctx, config.ResearchBaseDSN, config.ResearchDatabase)
}

func GetResearchOverview(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	store, err := OpenResearchStore(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "独立研究库尚未就绪，请执行研究库初始化命令；不会自动修改业务库"})
		return
	}
	defer store.Close()
	result, err := store.Overview(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "读取独立研究库结构失败", "detail": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func currentResearchStatus() ResearchSyncStatus {
	researchJob.Lock()
	defer researchJob.Unlock()
	return researchJob.status
}

func GetResearchSyncStatus(c *gin.Context) {
	status := currentResearchStatus()
	if status.State == "idle" {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		if store, err := OpenResearchStore(ctx); err == nil {
			defer store.Close()
			var id int64
			var state string
			var started time.Time
			var finished *time.Time
			var meta []byte
			if store.DB().QueryRowContext(ctx, "SELECT id,status,started_at,finished_at,metadata FROM audit_runs WHERE kind='source_sync' ORDER BY id DESC LIMIT 1").Scan(&id, &state, &started, &finished, &meta) == nil {
				_ = json.Unmarshal(meta, &status)
				status.RunID, status.State, status.StartedAt, status.FinishedAt = id, state, &started, finished
				if state == "completed" {
					status.Message = "上次同步已完成，档案与统计保存在独立库"
				}
				if state == "running" && (status.Message == "" || status.Message == "等待同步本地数据") {
					status.Message = "正在同步本地数据，原始记录和特征分阶段写入"
				}
				if state == "failed" && status.Message == "" {
					status.Message = "上次同步未完成，可重新同步；已有档案保留"
				}
			}
		}
	}
	c.JSON(http.StatusOK, status)
}

func beginResearchSync() bool {
	researchJob.Lock()
	defer researchJob.Unlock()
	if researchJob.status.State == "running" {
		return false
	}
	now := time.Now().UTC()
	researchJob.status = ResearchSyncStatus{State: "running", StartedAt: &now, Message: "正在读取业务库的一致快照"}
	return true
}

func updateResearchStatus(f func(*ResearchSyncStatus)) {
	researchJob.Lock()
	defer researchJob.Unlock()
	f(&researchJob.status)
}

func finishResearchSync(err error) {
	now := time.Now().UTC()
	updateResearchStatus(func(s *ResearchSyncStatus) {
		s.FinishedAt = &now
		if err != nil {
			s.State, s.Error, s.Message = "failed", err.Error(), "同步失败；已封存的数据不会被覆盖"
		} else {
			s.State, s.Message = "completed", "采集归档、清洗审计、特征快照和评估同步完成"
			if s.Unchanged {
				s.Message = "原始数据与特征版本未变化，沿用已封存数据"
			}
		}
	})
}

func StartResearchSync(c *gin.Context) {
	// Return immediately. Large imports never hold a page request open.
	if !beginResearchSync() {
		c.JSON(http.StatusConflict, currentResearchStatus())
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		err := runResearchSync(ctx)
		finishResearchSync(err)
		if err != nil {
			log.Printf("Research sync failed: %v", err)
		}
	}()
	c.JSON(http.StatusAccepted, currentResearchStatus())
}

// SyncResearchStore is also used by the finite maintenance CLI. It starts no
// HTTP listener and never initializes or migrates the business database.
func SyncResearchStore(ctx context.Context) (ResearchSyncStatus, error) {
	if !beginResearchSync() {
		return currentResearchStatus(), fmt.Errorf("研究库同步已在进行")
	}
	err := runResearchSync(ctx)
	finishResearchSync(err)
	return currentResearchStatus(), err
}

// StartResearchStoreWorker is part of the existing API process. A single MySQL
// advisory lock also prevents a CLI and this worker from importing concurrently.
// Set RESEARCH_SYNC_INTERVAL=0 to use manual synchronization only.
func StartResearchStoreWorker() {
	interval := 15 * time.Minute
	if raw := strings.TrimSpace(os.Getenv("RESEARCH_SYNC_INTERVAL")); raw != "" {
		if raw == "0" {
			return
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < time.Minute {
			log.Print("Invalid RESEARCH_SYNC_INTERVAL; using 15m")
		} else {
			interval = parsed
		}
	}
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			_, err := SyncResearchStore(ctx)
			cancel()
			if err != nil {
				log.Printf("Research periodic sync: %v", err)
			}
			timer := time.NewTimer(interval)
			<-timer.C
		}
	}()
}

func researchJSON(value any) json.RawMessage { body, _ := json.Marshal(value); return body }

func researchDatabaseReady() error {
	if database.DB == nil {
		return fmt.Errorf("业务数据库尚未连接")
	}
	return nil
}
