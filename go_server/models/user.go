// Package models defines the GORM database models.
package models

import "time"

// User represents a platform user.
type User struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Username  string    `gorm:"type:varchar(50);uniqueIndex;not null" json:"username"`
	Nickname  string    `gorm:"type:varchar(100);not null" json:"nickname"`
	Avatar    string    `gorm:"type:varchar(255)" json:"avatar"`
	Email     string    `gorm:"type:varchar(100)" json:"email"`
	Badge     string    `gorm:"type:varchar(50);comment:Pro Predictor / Expert / etc." json:"badge"`
	Balance   float64   `gorm:"type:decimal(12,2);default:0" json:"balance"`
	JoinedAt  time.Time `gorm:"type:date" json:"joinedAt"`
	Country   string    `gorm:"type:varchar(10)" json:"country"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// FollowedTeam tracks which teams a user follows.
type FollowedTeam struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"index;not null" json:"userId"`
	TeamName  string    `gorm:"type:varchar(100);not null" json:"teamName"`
	TeamLogo  string    `gorm:"type:varchar(255)" json:"teamLogo"`
	TeamID    int       `gorm:"comment:对应 homeTeamId/guestTeamId" json:"teamId"`
	CreatedAt time.Time `json:"createdAt"`
}
