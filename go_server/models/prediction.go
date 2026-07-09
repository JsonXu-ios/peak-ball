package models

import "time"

// Prediction represents a user's prediction on a match.
type Prediction struct {
	ID        uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64     `gorm:"index;not null" json:"userId"`
	MatchID   string     `gorm:"type:varchar(32);index;not null" json:"matchId"`
	Pick      string     `gorm:"type:varchar(20);not null;comment:home/draw/away/over/under" json:"pick"`
	Odds      float64    `gorm:"type:decimal(6,2)" json:"odds"`
	Stake     float64    `gorm:"type:decimal(10,2)" json:"stake"`
	Profit    float64    `gorm:"type:decimal(10,2);default:0" json:"profit"`
	Status    string     `gorm:"type:varchar(20);default:ongoing;comment:ongoing/won/lost/void" json:"status"`
	SettledAt *time.Time `gorm:"type:datetime" json:"settledAt"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`

	// Eager-load helpers (not a column)
	Match *Money `gorm:"foreignKey:MatchID;references:MatchId" json:"match,omitempty"`
}

// UserStats holds computed stats for a user. Not stored in DB — computed on the fly.
type UserStats struct {
	TotalPredictions int     `json:"totalPredictions"`
	Won              int     `json:"won"`
	Lost             int     `json:"lost"`
	Ongoing          int     `json:"ongoing"`
	Accuracy         float64 `json:"accuracy"`
	TotalProfit      float64 `json:"totalProfit"`
	ProfitChange     float64 `json:"profitChange"`
}
