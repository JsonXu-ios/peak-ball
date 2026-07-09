// Package models defines the GORM database models.
package models

import "time"

// Notification represents a user notification.
type Notification struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64     `gorm:"index;not null" json:"userId"`
	Type      string     `gorm:"type:varchar(30);index;not null;comment:goal/red_card/lineup/expert_tip/reward/system" json:"type"`
	Title     string     `gorm:"type:varchar(255);not null" json:"title"`
	Message   string     `gorm:"type:text" json:"message"`
	Icon      string     `gorm:"type:varchar(50);comment:material icon name" json:"icon"`
	MatchID   string     `gorm:"type:varchar(32);comment:关联比赛ID" json:"matchId"`
	IsRead    bool       `gorm:"default:false" json:"isRead"`
	ReadAt    *time.Time `gorm:"type:datetime" json:"readAt"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}
