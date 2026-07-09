// Package models defines the GORM database models.
package models

import "time"

// News represents a football news article.
type News struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Title     string    `gorm:"type:varchar(255);not null" json:"title"`
	Summary   string    `gorm:"type:text" json:"summary"`
	Content   string    `gorm:"type:text" json:"content"`
	ImageURL  string    `gorm:"type:varchar(500)" json:"imageUrl"`
	Category  string    `gorm:"type:varchar(50);index;not null;comment:latest/transfer/official/analysis/injury" json:"category"`
	Source    string    `gorm:"type:varchar(100)" json:"source"`
	Club      string    `gorm:"type:varchar(100);comment:相关俱乐部" json:"club"`
	IsHot     bool      `gorm:"default:false" json:"isHot"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TransferRumor represents a transfer rumor with a credibility index.
type TransferRumor struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	PlayerName   string    `gorm:"type:varchar(100);not null" json:"playerName"`
	FromClub     string    `gorm:"type:varchar(100);not null" json:"fromClub"`
	ToClub       string    `gorm:"type:varchar(100);not null" json:"toClub"`
	FromClubLogo string    `gorm:"type:varchar(500)" json:"fromClubLogo"`
	ToClubLogo   string    `gorm:"type:varchar(500)" json:"toClubLogo"`
	Value        string    `gorm:"type:varchar(50);comment:e.g. €85.0M" json:"value"`
	TrustLevel   int       `gorm:"default:0;comment:可信度百分比 0-100" json:"trustLevel"`
	Tier         string    `gorm:"type:varchar(20);comment:Tier 1/2/3" json:"tier"`
	Status       string    `gorm:"type:varchar(30);default:rumor;comment:rumor/confirmed/denied" json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
