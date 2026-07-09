package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go_admin/database"
	"go_admin/models"

	"github.com/gin-gonic/gin"
)

// GetCrawlerTasks returns all crawler tasks
func GetCrawlerTasks(c *gin.Context) {
	ensureDefaultCrawlerTasks()

	var tasks []models.CrawlerTask
	if err := database.DB.Order("id ASC").Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch tasks"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"list": tasks})
}

func ensureDefaultCrawlerTasks() {
	defaults := []models.CrawlerTask{
		{
			Name:        "启动后全量同步",
			Type:        "all",
			Status:      "pending",
			Config:      `{}`,
			Description: "先拉今日+明日比赛列表，再按顺序逐场拉历史、排名/积分榜、欧赔和盘口；启动项目后优先运行",
			IsEnabled:   true,
		},
		{
			Name:        "联赛排名/杯赛积分榜",
			Type:        "rank",
			Status:      "pending",
			Config:      `{}`,
			Description: "按日期批量获取联赛排名或杯赛积分榜；也可在 config 中填 match_id 只跑单场",
			IsEnabled:   true,
		},
		{
			Name:        "阶段赔率盘口刷新",
			Type:        "odds_refresh",
			Status:      "pending",
			Config:      `{}`,
			Description: "比赛临近或进行中反复运行，强制刷新欧赔、亚盘和大小球；不重拉历史",
			IsEnabled:   true,
		},
	}

	for _, task := range defaults {
		var existing models.CrawlerTask
		if err := database.DB.Where("type = ?", task.Type).First(&existing).Error; err == nil {
			continue
		}
		database.DB.Create(&task)
	}
}

// CreateCrawlerTask creates a new crawler task
func CreateCrawlerTask(c *gin.Context) {
	var task models.CrawlerTask
	if err := c.ShouldBindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := database.DB.Create(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create task"})
		return
	}

	c.JSON(http.StatusCreated, task)
}

// UpdateCrawlerTask updates a crawler task
func UpdateCrawlerTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	var task models.CrawlerTask
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	var req models.CrawlerTask
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	database.DB.Model(&task).Updates(map[string]interface{}{
		"name":        req.Name,
		"type":        req.Type,
		"schedule":    req.Schedule,
		"config":      req.Config,
		"description": req.Description,
		"is_enabled":  req.IsEnabled,
	})

	c.JSON(http.StatusOK, gin.H{"message": "Task updated successfully"})
}

// DeleteCrawlerTask deletes a crawler task
func DeleteCrawlerTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	if err := database.DB.Delete(&models.CrawlerTask{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete task"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Task deleted successfully"})
}

// RunCrawlerTask manually runs a crawler task
func RunCrawlerTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	var task models.CrawlerTask
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}
	if !task.IsEnabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Task is disabled"})
		return
	}

	async := c.DefaultQuery("async", "true") == "true"
	config := parseCrawlerTaskConfig(task.Config)
	req := crawlRequest{
		Type:     task.Type,
		Date:     config["date"],
		MatchID:  config["match_id"],
		Force:    strings.EqualFold(config["force"], "true"),
		TaskID:   task.ID,
		TaskName: task.Name,
	}

	result, execErr := startCrawl(req, async)
	if execErr != nil {
		status := http.StatusInternalServerError
		if strings.Contains(execErr.Error(), "already running") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": execErr.Error()})
		return
	}

	if async {
		c.JSON(http.StatusOK, gin.H{
			"message": "Task started asynchronously",
			"result":  result,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Task completed",
		"result":  result,
	})
}

func parseCrawlerTaskConfig(rawConfig string) map[string]string {
	config := map[string]string{}
	if strings.TrimSpace(rawConfig) == "" {
		return config
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(rawConfig), &raw); err != nil {
		return config
	}
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			config[key] = strings.TrimSpace(typed)
		case bool:
			config[key] = strconv.FormatBool(typed)
		case float64:
			config[key] = strconv.FormatFloat(typed, 'f', -1, 64)
		default:
			config[key] = strings.TrimSpace(strings.Trim(fmt.Sprint(typed), "\""))
		}
	}
	return config
}

// ToggleCrawlerTask enables or disables a crawler task
func ToggleCrawlerTask(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid task ID"})
		return
	}

	var task models.CrawlerTask
	if err := database.DB.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Task not found"})
		return
	}

	newStatus := !task.IsEnabled
	database.DB.Model(&task).Update("is_enabled", newStatus)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Task toggled successfully",
		"is_enabled": newStatus,
	})
}

// GetCrawlerLogs returns crawler execution logs
func GetCrawlerLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	status := c.Query("status")
	taskName := c.Query("task_name")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize
	query := database.DB.Model(&models.CrawlerLog{})

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if taskName != "" {
		query = query.Where("task_name = ?", taskName)
	}

	var total int64
	query.Count(&total)

	var logs []models.CrawlerLog
	if err := query.Offset(offset).Limit(pageSize).Order("id DESC").Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch logs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"list":      logs,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// GetCrawlerLogDetail returns detailed information for a crawler log
func GetCrawlerLogDetail(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid log ID"})
		return
	}

	var log models.CrawlerLog
	if err := database.DB.First(&log, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Log not found"})
		return
	}

	c.JSON(http.StatusOK, log)
}
