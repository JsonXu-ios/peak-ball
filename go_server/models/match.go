// Package models defines the GORM database models for football match data.
package models

import (
	"time"

	"gorm.io/datatypes"
)

// Money corresponds to the 'moneys' table (match main table).
// All fields from the crawler API response are stored and served.
type Money struct {
	ID                  uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	MatchId             string    `gorm:"type:varchar(32);uniqueIndex;not null;comment:接口中的 matchId" json:"matchId"`
	Date                time.Time `gorm:"type:date;not null;index;comment:比赛日期" json:"date"`
	League              string    `gorm:"type:varchar(100);comment:联赛短名" json:"league"`
	LeagueName          string    `gorm:"type:varchar(100);comment:联赛全名" json:"leagueName"`
	LeagueId            int       `gorm:"comment:leagueId" json:"leagueId"`
	Home                string    `gorm:"type:varchar(100);comment:主队" json:"home"`
	Guest               string    `gorm:"type:varchar(100);comment:客队" json:"guest"`
	HomeTeamId          int       `gorm:"comment:homeTeamId" json:"homeTeamId"`
	GuestTeamId         int       `gorm:"comment:guestTeamId" json:"guestTeamId"`
	MatchTime           time.Time `gorm:"type:datetime;comment:比赛时间" json:"matchTime"`
	Status              int       `gorm:"comment:比赛状态码" json:"status"`
	MatchState          int       `gorm:"comment:比赛进行状态" json:"matchState"`
	DisplayState        string    `gorm:"type:varchar(50);comment:显示状态" json:"displayState"`
	Time                string    `gorm:"type:varchar(20);comment:比赛进行时间" json:"time"`
	HomeScore           int       `gorm:"default:0" json:"homeScore"`
	GuestScore          int       `gorm:"default:0" json:"guestScore"`
	HomeHalfScore       int       `gorm:"default:0" json:"homeHalfScore"`
	GuestHalfScore      int       `gorm:"default:0" json:"guestHalfScore"`
	HomeOtScore         int       `gorm:"default:0" json:"homeOtScore"`
	GuestOtScore        int       `gorm:"default:0" json:"guestOtScore"`
	HomeOtPenalty       int       `gorm:"default:0" json:"homeOtPenalty"`
	GuestOtPenalty      int       `gorm:"default:0" json:"guestOtPenalty"`
	HomeCorner          int       `gorm:"default:0" json:"homeCorner"`
	GuestCorner         int       `gorm:"default:0" json:"guestCorner"`
	HomeRank            string    `gorm:"type:varchar(50)" json:"homeRank"`
	GuestRank           string    `gorm:"type:varchar(50)" json:"guestRank"`
	HomeLogo            string    `gorm:"type:varchar(255)" json:"homeLogo"`
	GuestLogo           string    `gorm:"type:varchar(255)" json:"guestLogo"`
	Season              string    `gorm:"type:varchar(50)" json:"season"`
	Round               string    `gorm:"type:varchar(50)" json:"round"`
	Groups              string    `gorm:"type:varchar(50)" json:"groups"`
	ScheduleId          int       `gorm:"comment:日程ID" json:"scheduleId"`
	Hot                 bool      `json:"hot"`
	HasSignal           bool      `json:"hasSignal"`
	HasHighlights       bool      `json:"hasHighlights"`
	HasContent          bool      `json:"hasContent"`
	Label               string    `gorm:"type:varchar(100)" json:"label"`
	JingcaiID           string    `gorm:"type:varchar(50);index;comment:竞彩足球编号 jingcaiId" json:"jingcaiId"`
	Description         string    `gorm:"type:text" json:"description"`
	OrderRecommendCount int       `json:"orderRecommendCount"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// TableName overrides GORM's irregular singularization for Money.
func (Money) TableName() string {
	return "moneys"
}

// HistoryMoney corresponds to the 'history_moneys' table.
type HistoryMoney struct {
	ID                 uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	MatchId            string         `gorm:"type:varchar(32);uniqueIndex;not null" json:"matchId"`
	Date               time.Time      `gorm:"type:date;not null" json:"date"`
	LeagueStat         datatypes.JSON `gorm:"type:json" json:"leagueStat"`
	AgainstSummary     datatypes.JSON `gorm:"type:json" json:"againstSummary"`
	AgainstList        datatypes.JSON `gorm:"type:json" json:"againstList"`
	RecentHomeSummary  datatypes.JSON `gorm:"type:json" json:"recentHomeSummary"`
	RecentHomeList     datatypes.JSON `gorm:"type:json" json:"recentHomeList"`
	RecentGuestSummary datatypes.JSON `gorm:"type:json" json:"recentGuestSummary"`
	RecentGuestList    datatypes.JSON `gorm:"type:json" json:"recentGuestList"`
	LeagueSummary      datatypes.JSON `gorm:"type:json" json:"leagueSummary"`
	RankData           datatypes.JSON `gorm:"type:json" json:"rankData"`
	FutureHome         datatypes.JSON `gorm:"type:json" json:"futureHome"`
	FutureGuest        datatypes.JSON `gorm:"type:json" json:"futureGuest"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

// OddsMoney corresponds to the 'odds_moneys' table.
type OddsMoney struct {
	ID             uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	MatchId        string         `gorm:"type:varchar(32);uniqueIndex;not null" json:"matchId"`
	Date           time.Time      `gorm:"type:date;not null" json:"date"`
	Data           datatypes.JSON `gorm:"type:json" json:"data"`
	RiseAndFall    datatypes.JSON `gorm:"type:json" json:"riseAndFall"`
	AvgOdds        datatypes.JSON `gorm:"type:json" json:"avgOdds"`
	William        datatypes.JSON `gorm:"type:json" json:"william"`
	Bet365         datatypes.JSON `gorm:"type:json" json:"bet365"`
	Pinnacle       datatypes.JSON `gorm:"type:json" json:"pinnacle"`
	SportteryTrade datatypes.JSON `gorm:"type:json" json:"sportteryTrade"`
	CompanyCount   int            `json:"companyCount"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// PankouMoney corresponds to the 'pankou_moneys' table.
type PankouMoney struct {
	ID         uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	MatchId    string         `gorm:"type:varchar(32);uniqueIndex;not null" json:"matchId"`
	Date       time.Time      `gorm:"type:date;not null" json:"date"`
	AsiaData   datatypes.JSON `gorm:"type:json" json:"asiaData"`
	DxqData    datatypes.JSON `gorm:"type:json" json:"dxqData"`
	Bet365Asia datatypes.JSON `gorm:"type:json" json:"bet365Asia"`
	Bet365Dxq  datatypes.JSON `gorm:"type:json" json:"bet365Dxq"`
	AsiaCount  int            `json:"asiaCount"`
	DxqCount   int            `json:"dxqCount"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}
