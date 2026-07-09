package main

import (
	"time"

	"gorm.io/datatypes"
)

// Money corresponds to the 'moneys' table (match main table).
// All fields from the API response MatchModel are stored here.
type Money struct {
	ID                  uint64    `gorm:"primaryKey;autoIncrement"`
	MatchId             string    `gorm:"type:varchar(32);uniqueIndex;not null;comment:接口中的 matchId"`
	Date                time.Time `gorm:"type:date;not null;index;comment:比赛日期"`
	League              string    `gorm:"type:varchar(100);comment:联赛短名 league"`
	LeagueName          string    `gorm:"type:varchar(100);comment:联赛全名 leagueName"`
	LeagueId            int       `gorm:"comment:leagueId"`
	Home                string    `gorm:"type:varchar(100);comment:主队 home"`
	Guest               string    `gorm:"type:varchar(100);comment:客队 guest"`
	HomeTeamId          int       `gorm:"comment:homeTeamId"`
	GuestTeamId         int       `gorm:"comment:guestTeamId"`
	MatchTime           time.Time `gorm:"type:datetime;comment:比赛时间 matchTime"`
	Status              int       `gorm:"comment:比赛状态码 status"`
	MatchState          int       `gorm:"comment:比赛进行状态 matchState"`
	DisplayState        string    `gorm:"type:varchar(50);comment:显示状态 displayState"`
	Time                string    `gorm:"type:varchar(20);comment:比赛进行时间 (如 HT, 45+2)"`
	HomeScore           int       `gorm:"default:0;comment:主队全场得分"`
	GuestScore          int       `gorm:"default:0;comment:客队全场得分"`
	HomeHalfScore       int       `gorm:"default:0;comment:主队半场得分"`
	GuestHalfScore      int       `gorm:"default:0;comment:客队半场得分"`
	HomeOtScore         int       `gorm:"default:0;comment:主队加时赛得分"`
	GuestOtScore        int       `gorm:"default:0;comment:客队加时赛得分"`
	HomeOtPenalty       int       `gorm:"default:0;comment:主队点球得分"`
	GuestOtPenalty      int       `gorm:"default:0;comment:客队点球得分"`
	HomeCorner          int       `gorm:"default:0;comment:主队角球数"`
	GuestCorner         int       `gorm:"default:0;comment:客队角球数"`
	HomeRank            string    `gorm:"type:varchar(50);comment:主队排名 homeRank"`
	GuestRank           string    `gorm:"type:varchar(50);comment:客队排名 guestRank"`
	HomeLogo            string    `gorm:"type:varchar(255);comment:主队Logo路径"`
	GuestLogo           string    `gorm:"type:varchar(255);comment:客队Logo路径"`
	Season              string    `gorm:"type:varchar(50);comment:赛季 season"`
	Round               string    `gorm:"type:varchar(50);comment:轮次 round"`
	Groups              string    `gorm:"type:varchar(50);comment:分组 groups"`
	ScheduleId          int       `gorm:"comment:日程ID scheduleId"`
	Hot                 bool      `gorm:"comment:是否热门"`
	HasSignal           bool      `gorm:"comment:是否有信号/直播"`
	HasHighlights       bool      `gorm:"comment:是否有集锦"`
	HasContent          bool      `gorm:"comment:是否有内容"`
	Label               string    `gorm:"type:varchar(100);comment:标签 label"`
	JingcaiID           string    `gorm:"type:varchar(50);index;comment:竞彩足球编号 jingcaiId"`
	Description         string    `gorm:"type:text;comment:描述 description"`
	OrderRecommendCount int       `gorm:"comment:推荐数 orderRecommendCount"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// TableName overrides GORM's irregular singularization for Money.
func (Money) TableName() string {
	return "moneys"
}

// HistoryMoney corresponds to the 'history_moneys' table.
// Stores all sections from the history API response as JSON columns.
type HistoryMoney struct {
	ID                 uint64         `gorm:"primaryKey;autoIncrement"`
	MatchId            string         `gorm:"type:varchar(32);uniqueIndex;not null"`
	Date               time.Time      `gorm:"type:date;not null"`
	LeagueStat         datatypes.JSON `gorm:"type:json;comment:leagueStat (sid/spf/prevSpf/league)"`
	AgainstSummary     datatypes.JSON `gorm:"type:json;comment:against.summary 交锋胜平负统计"`
	AgainstList        datatypes.JSON `gorm:"type:json;comment:against.list 交锋记录数组"`
	RecentHomeSummary  datatypes.JSON `gorm:"type:json;comment:recent.home.summary 主队近况汇总"`
	RecentHomeList     datatypes.JSON `gorm:"type:json;comment:recent.home.list 主队近期比赛"`
	RecentGuestSummary datatypes.JSON `gorm:"type:json;comment:recent.guest.summary 客队近况汇总"`
	RecentGuestList    datatypes.JSON `gorm:"type:json;comment:recent.guest.list 客队近期比赛"`
	LeagueSummary      datatypes.JSON `gorm:"type:json;comment:leagueSummary 联赛排名汇总 (含主客场胜率/进球统计)"`
	RankData           datatypes.JSON `gorm:"type:json;comment:rank 排名数据 (list + color)"`
	FutureHome         datatypes.JSON `gorm:"type:json;comment:future.home 主队未来赛程"`
	FutureGuest        datatypes.JSON `gorm:"type:json;comment:future.guest 客队未来赛程"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// OddsMoney corresponds to the 'odds_moneys' table (European odds).
// Stores all company odds as a JSON blob, plus extracts key companies.
type OddsMoney struct {
	ID             uint64         `gorm:"primaryKey;autoIncrement"`
	MatchId        string         `gorm:"type:varchar(32);uniqueIndex;not null"`
	Date           time.Time      `gorm:"type:date;not null"`
	Data           datatypes.JSON `gorm:"type:json;comment:完整的 odds 数组 (所有公司)"`
	RiseAndFall    datatypes.JSON `gorm:"type:json;comment:涨跌统计 riseAndFall"`
	AvgOdds        datatypes.JSON `gorm:"type:json;comment:平均欧赔"`
	William        datatypes.JSON `gorm:"type:json;comment:威廉希尔 (companyId=115)"`
	Bet365         datatypes.JSON `gorm:"type:json;comment:Bet365 (companyId=281)"`
	Pinnacle       datatypes.JSON `gorm:"type:json;comment:Pinnacle 平博"`
	SportteryTrade datatypes.JSON `gorm:"type:json;comment:竞彩投注比例和交易盈亏"`
	CompanyCount   int            `gorm:"comment:收录公司数量"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PankouMoney corresponds to the 'pankou_moneys' table (Asian handicap + Over/Under).
// Stores both Asia and DXQ arrays, plus extracts Bet365 specifically.
type PankouMoney struct {
	ID         uint64         `gorm:"primaryKey;autoIncrement"`
	MatchId    string         `gorm:"type:varchar(32);uniqueIndex;not null"`
	Date       time.Time      `gorm:"type:date;not null"`
	AsiaData   datatypes.JSON `gorm:"type:json;comment:asia 亚盘数组 (所有公司)"`
	DxqData    datatypes.JSON `gorm:"type:json;comment:dxq 大小球数组 (所有公司)"`
	Bet365Asia datatypes.JSON `gorm:"type:json;comment:Bet365 亚盘"`
	Bet365Dxq  datatypes.JSON `gorm:"type:json;comment:Bet365 大小球"`
	AsiaCount  int            `gorm:"comment:亚盘公司数量"`
	DxqCount   int            `gorm:"comment:大小球公司数量"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
