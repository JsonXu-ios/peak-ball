package models

import "time"

// FinalePrediction 是「终章」页对一场待赛比赛的预测存档。赛前把页面上各列的
// 展示文本、以及结算所需的方向与盘口线原样固化下来，比赛完赛后再逐列回填命中
// 结果。一场比赛一行（match_id 唯一）：开赛前反复覆盖成最新值，开赛后冻结。
//
// 存档只收录有预测的场次（前端主推≥70%）。Hit* 用 *bool：null 表示该列本场不
// 适用（信号为空、走盘、盘口缺失），不计入命中率分母，与「算了但没中」区分。
type FinalePrediction struct {
	ID        uint   `gorm:"primaryKey" json:"-"`
	MatchID   string `gorm:"size:64;uniqueIndex;comment:比赛ID" json:"match_id"`
	Date      string `gorm:"size:16;index;comment:比赛日期" json:"date"`
	MatchTime string `gorm:"size:32;comment:开赛时间" json:"match_time"`
	League    string `gorm:"size:128;comment:联赛" json:"league"`
	Home      string `gorm:"size:128;comment:主队" json:"home"`
	Guest     string `gorm:"size:128;comment:客队" json:"guest"`
	HomeLogo  string `gorm:"size:255" json:"home_logo"`
	GuestLogo string `gorm:"size:255" json:"guest_logo"`

	// 页面各列的展示文本，历史行用同一套模板原样渲染。
	Pick    string `gorm:"size:8;comment:预测方向 home/away" json:"pick"`
	SigBase string `gorm:"size:32;comment:主推列" json:"sig_base"`
	// BaseProb 赛前那一刻的主推概率数值。SigBase 里本来就带这个数（「客胜 72.3%」），
	// 单开一列是为了留个不用解析文本的原始值备查——页面目前不读它，但概率是赛前
	// 冻结下来最有分析价值的一项，照常写入。
	BaseProb  float64 `gorm:"comment:主推概率(%)" json:"base_prob"`
	OuHeat    string  `gorm:"size:32;comment:大小球热度列" json:"ou_heat"`
	OuPick    string  `gorm:"size:16;comment:球数倾向列" json:"ou_pick"`
	ExpAh     string  `gorm:"size:32;comment:期望让球列" json:"exp_ah"`
	ExpOu     string  `gorm:"size:32;comment:期望球数列" json:"exp_ou"`
	AhLine    string  `gorm:"size:16;comment:亚盘列" json:"ah_line"`
	OuLine    string  `gorm:"size:16;comment:大小球列" json:"ou_line"`
	TradePick string  `gorm:"size:8;comment:交易盈亏提示方向文本" json:"trade_pick"`
	SimPick   string  `gorm:"size:8;comment:模拟盈亏提示方向文本" json:"sim_pick"`
	// 大小球推荐拆成互斥的两列（差值=盘口−球数综合均值，口径同「完赛基础统计」）：
	//   GoalPick    |差值| ≥ 0.75 的超出段（#33 反向判断）
	//   GoalPickMid |差值| < 0.75 的带内（#32 常规判断）
	// 只推小球，取值为「买小球」或空。只展示，不进逐列命中率表。
	//（旧存档里可能还留着按更早口径写下的「买大球」。）
	GoalPick    string `gorm:"size:8;comment:买小球推荐(超出带)" json:"goal_pick"`
	GoalPickMid string `gorm:"size:8;comment:买小球推荐(带内)" json:"goal_pick_mid"`

	// 结算依据。让球/大小球各列必须用快照时的盘口线结算——事后盘口会变。
	BaseDir     string   `gorm:"size:8;comment:主推方向 home/draw/away" json:"-"`
	OuHeatDir   string   `gorm:"size:8;comment:热度方向 over/under" json:"-"`
	OuPickDir   string   `gorm:"size:8;comment:球数倾向 over/under" json:"-"`
	ExpAhDir    string   `gorm:"size:8;comment:期望让球对亚盘的赢盘方向 home/away" json:"-"`
	ExpOuDir    string   `gorm:"size:8;comment:期望球数方向 over/under" json:"-"`
	TradeDir    string   `gorm:"size:8;comment:交易盈亏方向 home/away" json:"-"`
	SimDir      string   `gorm:"size:8;comment:模拟盈亏方向 home/away" json:"-"`
	AhLineValue *float64 `gorm:"comment:结算用亚盘线" json:"-"`
	OuLineValue *float64 `gorm:"comment:结算用大小球线" json:"-"`

	SnapshotAt time.Time `gorm:"comment:最后一次覆盖时间" json:"snapshot_at"`

	// 结算结果。
	Settled    bool       `gorm:"index;comment:是否已结算" json:"settled"`
	HomeScore  int        `gorm:"comment:主队进球" json:"home_score"`
	GuestScore int        `gorm:"comment:客队进球" json:"guest_score"`
	Result     string     `gorm:"size:8;comment:赛果 home/draw/away" json:"result"`
	SettledAt  *time.Time `gorm:"comment:结算时间" json:"settled_at"`

	HitPick   *bool `gorm:"comment:预测列命中" json:"hit_pick"`
	HitBase   *bool `gorm:"comment:主推列命中" json:"hit_base"`
	HitOuHeat *bool `gorm:"comment:大小球热度列命中" json:"hit_ou_heat"`
	HitOuPick *bool `gorm:"comment:球数倾向列命中" json:"hit_ou_pick"`
	HitExpAh  *bool `gorm:"comment:期望让球列命中" json:"hit_exp_ah"`
	HitExpOu  *bool `gorm:"comment:期望球数列命中" json:"hit_exp_ou"`
	HitTrade  *bool `gorm:"comment:交易盈亏反买命中" json:"hit_trade"`
	HitSim    *bool `gorm:"comment:模拟盈亏反买命中" json:"hit_sim"`
	// 两列大小球推荐各自是否打出。只用来给列表里的推荐上色，不进逐列命中率表
	// （那张表按「本页只展示、不结算」的口径不收这两列）。
	HitGoalPick    *bool `gorm:"comment:大小球推荐(超出带)命中" json:"hit_goal_pick"`
	HitGoalPickMid *bool `gorm:"comment:大小球推荐(带内)命中" json:"hit_goal_pick_mid"`

	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}
