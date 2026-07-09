// Package models defines the GORM database models.
package models

import "time"

// LeaderboardEntry stores periodic leaderboard snapshots.
type LeaderboardEntry struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"index;not null" json:"userId"`
	Period    string    `gorm:"type:varchar(10);index;not null;comment:weekly/monthly" json:"period"`
	PeriodKey string    `gorm:"type:varchar(20);index;not null;comment:e.g. 2026-W06 or 2026-02" json:"periodKey"`
	Points    int       `gorm:"default:0" json:"points"`
	Accuracy  float64   `gorm:"type:decimal(5,2);default:0" json:"accuracy"`
	Rank      int       `gorm:"default:0" json:"rank"`
	Trend     string    `gorm:"type:varchar(10);default:none;comment:up/down/none" json:"trend"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Eager-load helpers
	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}
