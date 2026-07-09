// Package models defines the GORM database models.
package models

import "time"

// Expert represents a verified expert analyst.
type Expert struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"uniqueIndex;not null" json:"userId"`
	Name      string    `gorm:"type:varchar(100);not null" json:"name"`
	Avatar    string    `gorm:"type:varchar(255)" json:"avatar"`
	Specialty string    `gorm:"type:varchar(200);comment:e.g. Premier League & Champions League" json:"specialty"`
	Accuracy  float64   `gorm:"type:decimal(5,2);default:0" json:"accuracy"`
	Streak    int       `gorm:"default:0;comment:当前连红" json:"streak"`
	Followers int       `gorm:"default:0" json:"followers"`
	Verified  bool      `gorm:"default:false" json:"verified"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ExpertTip is a prediction published by an expert attached to a specific match.
type ExpertTip struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ExpertID  uint64    `gorm:"index;not null" json:"expertId"`
	MatchID   string    `gorm:"type:varchar(32);index;not null" json:"matchId"`
	Pick      string    `gorm:"type:varchar(20);not null;comment:home/draw/away/over/under" json:"pick"`
	Analysis  string    `gorm:"type:text" json:"analysis"`
	Odds      float64   `gorm:"type:decimal(6,2)" json:"odds"`
	Result    string    `gorm:"type:varchar(20);default:pending;comment:pending/won/lost" json:"result"`
	Likes     int       `gorm:"default:0" json:"likes"`
	Comments  int       `gorm:"default:0" json:"comments"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Eager-load helpers
	Expert *Expert `gorm:"foreignKey:ExpertID" json:"expert,omitempty"`
	Match  *Money  `gorm:"foreignKey:MatchID;references:MatchId" json:"match,omitempty"`
}
