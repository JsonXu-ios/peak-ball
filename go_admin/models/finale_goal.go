package models

import "time"

// FinaleGoalPrediction 是「终章 · 大小球」对一场待赛比赛的预测存档。
//
// 只收录两种「期望球数与球数热度判到相反侧」的组合，两种都买大球：
//
//	split_under 反向·热度判小球（期望球数判大球），只取大小球盘口 < 3.75 的场次；
//	split_over  反向·热度判大球（期望球数判小球），只剔除盘口正好是 2.25 的场次。
//
// 一场比赛一行（match_id 唯一）：开赛前反复覆盖成最新值，开赛后冻结。HitPick 用
// *bool：null 表示走盘或盘口缺失，不计入命中率分母，与「买了但没中」区分。
type FinaleGoalPrediction struct {
	ID        uint   `gorm:"primaryKey" json:"-"`
	MatchID   string `gorm:"size:64;uniqueIndex;comment:比赛ID" json:"match_id"`
	Date      string `gorm:"size:16;index;comment:比赛日期" json:"date"`
	MatchTime string `gorm:"size:32;comment:开赛时间" json:"match_time"`
	League    string `gorm:"size:128;comment:联赛" json:"league"`
	Home      string `gorm:"size:128;comment:主队" json:"home"`
	Guest     string `gorm:"size:128;comment:客队" json:"guest"`
	HomeLogo  string `gorm:"size:255" json:"home_logo"`
	GuestLogo string `gorm:"size:255" json:"guest_logo"`

	// Combo 命中的是哪一种组合：split_under / split_over。
	Combo string `gorm:"size:16;index;comment:组合 split_under/split_over" json:"combo"`
	// Pick 恒为 over（两种组合都买大球），显式存下来以免日后加规则时看串。
	Pick string `gorm:"size:8;comment:投注方向，恒为 over" json:"pick"`

	// 展示文本，历史行用同一套模板原样渲染。
	ExpGoals  string `gorm:"size:32;comment:期望球数列，如 判大球 3.60" json:"exp_goals"`
	HeatGoals string `gorm:"size:32;comment:球数热度列，如 判小球 3.50" json:"heat_goals"`
	OuLine    string `gorm:"size:16;comment:大小球盘口列" json:"ou_line"`

	// 结算依据：必须用快照时的盘口线，事后盘口会变。
	OuLineValue *float64 `gorm:"comment:结算用大小球线" json:"-"`

	SnapshotAt time.Time `gorm:"comment:最后一次覆盖时间" json:"snapshot_at"`

	// 结算结果。
	Settled    bool       `gorm:"index;comment:是否已结算" json:"settled"`
	HomeScore  int        `gorm:"comment:主队进球" json:"home_score"`
	GuestScore int        `gorm:"comment:客队进球" json:"guest_score"`
	Result     string     `gorm:"size:8;comment:大小球赛果 over/under，走盘为空" json:"result"`
	SettledAt  *time.Time `gorm:"comment:结算时间" json:"settled_at"`
	HitPick    *bool      `gorm:"comment:买大球是否命中，null=走盘/盘口缺失" json:"hit_pick"`

	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}
