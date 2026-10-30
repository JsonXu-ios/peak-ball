package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"peakball/researchstore"
)

func researchReadConnection(c *gin.Context) (*researchstore.Store, context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	store, err := OpenResearchStore(ctx)
	if err != nil {
		cancel()
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "独立研究库尚未连接"})
		return nil, nil, func() {}
	}
	return store, ctx, cancel
}

func researchPage(c *gin.Context) (page, size int) {
	page, _ = strconv.Atoi(c.Query("page"))
	size, _ = strconv.Atoi(c.Query("page_size"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 25
	}
	if size > 200 {
		size = 200
	}
	if page > 100000 {
		page = 100000
	}
	return
}

func GetResearchFeatures(c *gin.Context) {
	store, ctx, cancel := researchReadConnection(c)
	if store == nil {
		return
	}
	defer cancel()
	defer store.Close()
	version := strings.TrimSpace(c.Query("version"))
	if version == "" {
		version = researchFeatureVersion
	}
	search := strings.TrimSpace(c.Query("search"))
	if len(search) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "搜索内容过长"})
		return
	}
	page, size := researchPage(c)
	where := "feature_version=?"
	args := []any{version}
	if search != "" {
		where += " AND (feature_name LIKE ? OR description LIKE ?)"
		args = append(args, "%"+search+"%", "%"+search+"%")
	}
	var total int64
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM feature_definitions WHERE "+where, args...).Scan(&total); err != nil {
		c.JSON(500, gin.H{"error": "读取特征字典失败"})
		return
	}
	rows, err := store.DB().QueryContext(ctx, "SELECT feature_version,feature_name,value_type,unit,description,formula,origin,missing_policy,code_hash,metadata FROM feature_definitions WHERE "+where+" ORDER BY feature_name LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if err != nil {
		c.JSON(500, gin.H{"error": "读取特征字典失败"})
		return
	}
	defer rows.Close()
	items := []researchstore.FeatureDefinition{}
	for rows.Next() {
		var f researchstore.FeatureDefinition
		if err = rows.Scan(&f.Version, &f.Name, &f.Type, &f.Unit, &f.Description, &f.Formula, &f.Origin, &f.MissingPolicy, &f.CodeHash, &f.Metadata); err != nil {
			c.JSON(500, gin.H{"error": "特征字典记录无法读取"})
			return
		}
		items = append(items, f)
	}
	if rows.Err() != nil {
		c.JSON(500, gin.H{"error": "读取特征字典中断"})
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total, "page": page, "pageSize": size})
}

type researchIssueView struct {
	ID        int64           `json:"id"`
	RunID     int64           `json:"runId"`
	MatchID   string          `json:"matchId"`
	Severity  string          `json:"severity"`
	Code      string          `json:"code"`
	Field     string          `json:"field"`
	Message   string          `json:"message"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt time.Time       `json:"createdAt"`
}

func GetResearchIssues(c *gin.Context) {
	store, ctx, cancel := researchReadConnection(c)
	if store == nil {
		return
	}
	defer cancel()
	defer store.Close()
	severity := strings.TrimSpace(c.Query("severity"))
	if severity != "" && severity != "info" && severity != "warning" && severity != "error" {
		c.JSON(400, gin.H{"error": "severity应为info、warning或error"})
		return
	}
	page, size := researchPage(c)
	var runID int64
	if err := store.DB().QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM audit_runs WHERE kind='source_sync' AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(metadata,'$.unchanged')),'false')<>'true'").Scan(&runID); err != nil {
		c.JSON(500, gin.H{"error": "读取审计批次失败"})
		return
	}
	where := "run_id=?"
	args := []any{runID}
	if severity != "" {
		where += " AND severity=?"
		args = append(args, severity)
	}
	var total int64
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_issues WHERE "+where, args...).Scan(&total); err != nil {
		c.JSON(500, gin.H{"error": "读取审计问题失败"})
		return
	}
	rows, err := store.DB().QueryContext(ctx, "SELECT id,run_id,match_id,severity,code,field_name,message,detail,created_at FROM audit_issues WHERE "+where+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, size, (page-1)*size)...)
	if err != nil {
		c.JSON(500, gin.H{"error": "读取审计问题失败"})
		return
	}
	defer rows.Close()
	items := []researchIssueView{}
	for rows.Next() {
		var item researchIssueView
		if err = rows.Scan(&item.ID, &item.RunID, &item.MatchID, &item.Severity, &item.Code, &item.Field, &item.Message, &item.Detail, &item.CreatedAt); err != nil {
			c.JSON(500, gin.H{"error": "审计问题记录无法读取"})
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		c.JSON(500, gin.H{"error": "读取审计问题中断"})
		return
	}
	c.JSON(200, gin.H{"items": items, "total": total, "page": page, "pageSize": size, "runId": runID})
}

type researchEvaluationView struct {
	ID        int64           `json:"id"`
	Scope     string          `json:"scope"`
	Version   string          `json:"version"`
	CreatedAt time.Time       `json:"createdAt"`
	Metrics   json.RawMessage `json:"metrics"`
}

func GetResearchEvaluations(c *gin.Context) {
	store, ctx, cancel := researchReadConnection(c)
	if store == nil {
		return
	}
	defer cancel()
	defer store.Close()
	rows, err := store.DB().QueryContext(ctx, "SELECT id,scope,version,created_at,metrics FROM evaluations WHERE id IN (SELECT MAX(id) FROM evaluations GROUP BY scope) ORDER BY id DESC")
	if err != nil {
		c.JSON(500, gin.H{"error": "读取评估记录失败"})
		return
	}
	defer rows.Close()
	items := []researchEvaluationView{}
	for rows.Next() {
		var item researchEvaluationView
		if err = rows.Scan(&item.ID, &item.Scope, &item.Version, &item.CreatedAt, &item.Metrics); err != nil {
			c.JSON(500, gin.H{"error": "评估记录无法读取"})
			return
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		c.JSON(500, gin.H{"error": "读取评估记录中断"})
		return
	}
	c.JSON(200, gin.H{"items": items})
}
