// Package models defines the GORM database models.
package models

import "time"

// WalletTransaction represents a points/wallet transaction.
type WalletTransaction struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      uint64    `gorm:"index;not null" json:"userId"`
	Type        string    `gorm:"type:varchar(20);not null;comment:earned/spent/topup/redeem" json:"type"`
	Amount      int       `gorm:"not null;comment:正数为收入 负数为支出" json:"amount"`
	Description string    `gorm:"type:varchar(255)" json:"description"`
	Detail      string    `gorm:"type:varchar(255);comment:详情如比赛名" json:"detail"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Reward represents a redeemable reward in the points store.
type Reward struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(100);not null" json:"name"`
	Description string    `gorm:"type:varchar(255)" json:"description"`
	Icon        string    `gorm:"type:varchar(50);comment:material icon name" json:"icon"`
	IconColor   string    `gorm:"type:varchar(30);comment:tailwind color name" json:"iconColor"`
	Cost        int       `gorm:"not null;comment:所需积分" json:"cost"`
	IsActive    bool      `gorm:"default:true" json:"isActive"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SearchHistory represents a user's search record.
type SearchHistory struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"index;not null" json:"userId"`
	Query     string    `gorm:"type:varchar(255);not null" json:"query"`
	CreatedAt time.Time `json:"createdAt"`
}

// LeagueStanding represents a team's position in a league table.
type LeagueStanding struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	LeagueID int    `gorm:"index;not null" json:"leagueId"`
	League   string `gorm:"type:varchar(100);not null" json:"league"`
	Season   string `gorm:"type:varchar(20);not null;comment:e.g. 2025-2026" json:"season"`
	TeamID   int    `gorm:"index;not null" json:"teamId"`
	TeamName string `gorm:"type:varchar(100);not null" json:"teamName"`
	TeamLogo string `gorm:"type:varchar(500)" json:"teamLogo"`
	Rank     int    `gorm:"not null" json:"rank"`
	Played   int    `gorm:"default:0" json:"played"`
	Won      int    `gorm:"default:0" json:"won"`
	Drawn    int    `gorm:"default:0" json:"drawn"`
	Lost     int    `gorm:"default:0" json:"lost"`
	GoalsFor int    `gorm:"default:0" json:"goalsFor"`
	GoalsAg  int    `gorm:"default:0;comment:goals against" json:"goalsAgainst"`
	GoalDiff int    `gorm:"default:0" json:"goalDiff"`
	Points   int    `gorm:"default:0" json:"points"`
	Form     string `gorm:"type:varchar(20);comment:e.g. WWDWL" json:"form"`
	Zone     string `gorm:"type:varchar(50);comment:champion/europa/conference/relegation" json:"zone"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TopScorer represents a player in the top scorer / assist table.
type TopScorer struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	LeagueID   int    `gorm:"index;not null" json:"leagueId"`
	League     string `gorm:"type:varchar(100);not null" json:"league"`
	Season     string `gorm:"type:varchar(20);not null" json:"season"`
	PlayerName string `gorm:"type:varchar(100);not null" json:"playerName"`
	TeamName   string `gorm:"type:varchar(100)" json:"teamName"`
	Avatar     string `gorm:"type:varchar(500)" json:"avatar"`
	Goals      int    `gorm:"default:0" json:"goals"`
	Assists    int    `gorm:"default:0" json:"assists"`
	Rank       int    `gorm:"not null" json:"rank"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TeamInfoCache stores lightweight team background text used by analysis plans.
type TeamInfoCache struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TeamName    string    `gorm:"type:varchar(100);not null;uniqueIndex:idx_team_info_team_league" json:"teamName"`
	League      string    `gorm:"type:varchar(100);uniqueIndex:idx_team_info_team_league" json:"league"`
	Summary     string    `gorm:"type:text" json:"summary"`
	SourceTitle string    `gorm:"type:varchar(255)" json:"sourceTitle"`
	SourceURL   string    `gorm:"type:varchar(500)" json:"sourceUrl"`
	FetchedAt   time.Time `gorm:"index" json:"fetchedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
