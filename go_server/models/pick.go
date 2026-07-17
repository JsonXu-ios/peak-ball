package models

import "time"

// UserPick is one recorded betting choice of the site owner for a completed
// match. Rows live in MySQL (football_data) so the whole sample migrates with
// a plain mysqldump. One row per (match, market); saving again overwrites.
type UserPick struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	MatchId    string    `gorm:"size:32;uniqueIndex:idx_pick_match_market,priority:1;comment:比赛ID" json:"matchId"`
	Market     string    `gorm:"size:16;uniqueIndex:idx_pick_match_market,priority:2;comment:玩法 spf/rqspf/dxq/score" json:"market"`
	Pick       string    `gorm:"size:16;comment:选择内容" json:"pick"`
	Line       *float64  `gorm:"type:decimal(6,2);comment:盘口线(让球/大小球)" json:"line"`
	Direction  string    `gorm:"size:8;default:self;comment:依据 follow=正向 fade=反向 self=自主" json:"direction"`
	Confidence int       `gorm:"default:0;comment:信心等级1-3" json:"confidence"`
	Note       string    `gorm:"size:255;comment:备注" json:"note"`
	Source     string    `gorm:"size:16;default:backfill;comment:backfill=补录 live=赛前" json:"source"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
