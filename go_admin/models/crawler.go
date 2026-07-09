// Package models defines crawler related models.
package models

import (
	"time"

	"gorm.io/gorm"
)

// CrawlerTask represents a crawler task
type CrawlerTask struct {
	ID          uint           `gorm:"primarykey" json:"id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
	Name        string         `gorm:"size:100;not null" json:"name"`
	Type        string         `gorm:"size:50;not null" json:"type"`            // match_list, history, rank, odds_euro, odds_pankou, odds_refresh, all
	Status      string         `gorm:"size:20;default:'pending'" json:"status"` // pending, running, success, failed
	Schedule    string         `gorm:"size:100" json:"schedule"`                // cron expression
	Config      string         `gorm:"type:text" json:"config"`                 // JSON config
	Description string         `gorm:"size:500" json:"description"`
	LastRunAt   *time.Time     `json:"last_run_at"`
	NextRunAt   *time.Time     `json:"next_run_at"`
	IsEnabled   bool           `gorm:"default:true" json:"is_enabled"`
	RunCount    int            `gorm:"default:0" json:"run_count"`
	SuccessRate float64        `gorm:"default:0" json:"success_rate"`
}

// CrawlerLog represents a crawler execution log
type CrawlerLog struct {
	ID           uint       `gorm:"primarykey" json:"id"`
	CreatedAt    time.Time  `json:"created_at"`
	TaskID       uint       `json:"task_id"`
	TaskName     string     `gorm:"size:100" json:"task_name"`
	Status       string     `gorm:"size:20" json:"status"` // running, success, failed
	StartTime    time.Time  `json:"start_time"`
	EndTime      *time.Time `json:"end_time"`
	Duration     int64      `json:"duration"` // milliseconds
	ItemsCount   int        `gorm:"default:0" json:"items_count"`
	SuccessCount int        `gorm:"default:0" json:"success_count"`
	FailedCount  int        `gorm:"default:0" json:"failed_count"`
	ErrorMsg     string     `gorm:"type:text" json:"error_msg"`
	Details      string     `gorm:"type:text" json:"details"` // JSON details
}

// Money represents match data from crawler (existing table)
type Money struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	MatchID    string    `gorm:"uniqueIndex;size:50" json:"match_id"`
	Date       string    `gorm:"index;size:20" json:"date"`
	League     string    `gorm:"index;size:100" json:"league"`
	Home       string    `gorm:"size:100" json:"home"`
	Guest      string    `gorm:"size:100" json:"guest"`
	Scores     string    `gorm:"size:20" json:"scores"`
	HomeScore  int       `json:"home_score"`
	GuestScore int       `json:"guest_score"`
	Status     string    `gorm:"size:20" json:"status"`
	HomeLogo   string    `gorm:"size:255" json:"home_logo"`
	GuestLogo  string    `gorm:"size:255" json:"guest_logo"`
	MatchTime  string    `gorm:"size:50" json:"match_time"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName specifies the table name for Money model
func (Money) TableName() string {
	return "moneys"
}

// HistoryMoney represents historical match data
type HistoryMoney struct {
	ID         uint      `gorm:"primarykey" json:"id"`
	MatchID    string    `gorm:"uniqueIndex;size:50" json:"match_id"`
	LeagueStat string    `gorm:"type:json" json:"league_stat"`
	Against    string    `gorm:"type:json" json:"against"`
	Recent     string    `gorm:"type:json" json:"recent"`
	Rank       string    `gorm:"type:json" json:"rank"`
	Future     string    `gorm:"type:json" json:"future"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TableName specifies the table name for HistoryMoney model
func (HistoryMoney) TableName() string {
	return "history_moneys"
}

// OddsMoney represents odds data
type OddsMoney struct {
	ID             uint      `gorm:"primarykey" json:"id"`
	MatchID        string    `gorm:"uniqueIndex;size:50" json:"match_id"`
	Data           string    `gorm:"type:json" json:"data"`
	AvgOdds        string    `gorm:"type:json" json:"avg_odds"`
	William        string    `gorm:"type:json" json:"william"`
	Bet365         string    `gorm:"type:json" json:"bet365"`
	SportteryTrade string    `gorm:"type:json" json:"sporttery_trade"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// TableName specifies the table name for OddsMoney model
func (OddsMoney) TableName() string {
	return "odds_moneys"
}

// PankouMoney represents pankou (Asian handicap) data
type PankouMoney struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	MatchID   string    `gorm:"uniqueIndex;size:50" json:"match_id"`
	AsiaData  string    `gorm:"type:json" json:"asia_data"`
	DxqData   string    `gorm:"type:json" json:"dxq_data"`
	Bet365    string    `gorm:"type:json" json:"bet365"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName specifies the table name for PankouMoney model
func (PankouMoney) TableName() string {
	return "pankou_moneys"
}
