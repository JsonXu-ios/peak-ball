package main

// MatchListResponse corresponds to match_list.json
type MatchListResponse struct {
	Next  string `json:"next"`
	Items []Item `json:"items"`
	Full  bool   `json:"full"`
}

type Item struct {
	Date     string  `json:"date"`
	DateDesc string  `json:"dateDesc"`
	Matches  []Match `json:"matches"`
}

type Match struct {
	Type  string     `json:"type"`
	Model MatchModel `json:"model"`
}

type MatchModel struct {
	MatchId             string      `json:"matchId"`
	Home                string      `json:"home"`
	Guest               string      `json:"guest"`
	League              string      `json:"league"`
	MatchTime           string      `json:"matchTime"`
	Status              int         `json:"status"`
	HomeScore           int         `json:"homeScore"`
	GuestScore          int         `json:"guestScore"`
	HomeLogo            string      `json:"homeLogo"`
	GuestLogo           string      `json:"guestLogo"`
	LeagueId            int         `json:"leagueId"`
	HomeHalfScore       int         `json:"homeHalfScore"`
	GuestHalfScore      int         `json:"guestHalfScore"`
	Turn                interface{} `json:"turn"` // Sometimes null or int
	HomeTeamId          int         `json:"homeTeamId"`
	GuestTeamId         int         `json:"guestTeamId"`
	HomeRank            interface{} `json:"homeRank"`
	GuestRank           interface{} `json:"guestRank"`
	Season              string      `json:"season"`
	Round               string      `json:"round"`
	SportteryHome       interface{} `json:"sportteryHome"`
	SportteryGuest      interface{} `json:"sportteryGuest"`
	HomeNameV           interface{} `json:"homeNameV"`
	GuestNameV          interface{} `json:"guestNameV"`
	AnimationManual     interface{} `json:"animationManual"`
	ScheduleId          int         `json:"scheduleId"`
	SportteryLeague     interface{} `json:"sportteryLeague"`
	LeagueNameV         interface{} `json:"leagueNameV"`
	Groups              string      `json:"groups"`
	HasHighlights       bool        `json:"hasHighlights"`
	HomeOtScore         int         `json:"homeOtScore"`
	GuestOtScore        int         `json:"guestOtScore"`
	HomeOtPenalty       int         `json:"homeOtPenalty"`
	GuestOtPenalty      int         `json:"guestOtPenalty"`
	HasContent          bool        `json:"hasContent"`
	BdIssue             interface{} `json:"bdIssue"` // Can be string or null
	HomeCorner          int         `json:"homeCorner"`
	GuestCorner         int         `json:"guestCorner"`
	MatchState          int         `json:"matchState"`
	Time                string      `json:"time"`
	DisplayState        string      `json:"displayState"`
	LeagueName          string      `json:"leagueName"`
	Hot                 bool        `json:"hot"`
	HasSignal           bool        `json:"hasSignal"`
	Label               string      `json:"label"`
	OrderRecommendCount int         `json:"orderRecommendCount"`
	JingcaiId           interface{} `json:"jingcaiId"` // Can be string or null
	Description         string      `json:"description"`
}

// HistoryResponse corresponds to history.json
type HistoryResponse struct {
	LeagueStat    LeagueStat    `json:"leagueStat"`
	Against       Against       `json:"against"`
	Recent        Recent        `json:"recent"`
	Future        Future        `json:"future"`
	LeagueSummary LeagueSummary `json:"leagueSummary"`
	Rank          RankData      `json:"rank"`
}

type LeagueStat struct {
	Sid     string `json:"sid"`
	Spf     []int  `json:"spf"`
	PrevSpf []int  `json:"prevSpf"`
	League  string `json:"league"`
}

type Against struct {
	Summary Summary        `json:"summary"`
	List    []HistoryMatch `json:"list"`
}

type Recent struct {
	Home  RecentDetail `json:"home"`
	Guest RecentDetail `json:"guest"`
}

type RecentDetail struct {
	Summary Summary        `json:"summary"`
	List    []HistoryMatch `json:"list"`
}

type Future struct {
	Home  []FutureMatch `json:"home"`
	Guest []FutureMatch `json:"guest"`
}

type Summary struct {
	Win       int `json:"win"`
	Lose      int `json:"lose"`
	Draw      int `json:"draw"`
	All       int `json:"all"`
	WinGoal   int `json:"winGoal"`
	LoseGoal  int `json:"loseGoal"`
	HomeWin   int `json:"homeWin"`
	HomeLose  int `json:"homeLose"`
	HomeDraw  int `json:"homeDraw"`
	HomeAll   int `json:"homeAll"`
	GuestWin  int `json:"guestWin"`
	GuestDraw int `json:"guestDraw"`
	GuestLose int `json:"guestLose"`
	GuestAll  int `json:"guestAll"`
}

type HistoryMatch struct {
	MatchTime  string      `json:"matchTime"`
	Home       string      `json:"home"`
	Guest      string      `json:"guest"`
	HomeId     int         `json:"homeId"`
	GuestId    int         `json:"guestId"`
	Goal       []int       `json:"goal"`
	HalfGoal   []int       `json:"halfGoal"`
	League     string      `json:"league"`
	ScheduleId int         `json:"scheduleId"`
	MatchId    int         `json:"matchId"` // Note: In match_list it is string, here int in sample? Sample says 497943873 (no quotes)
	LetGoal    interface{} `json:"letGoal"`
	Separate   int         `json:"separate"`
}

type FutureMatch struct {
	MatchTime  string      `json:"matchTime"`
	Home       string      `json:"home"`
	Guest      string      `json:"guest"`
	HomeId     int         `json:"homeId"`
	GuestId    int         `json:"guestId"`
	Goal       []int       `json:"goal"`
	HalfGoal   []int       `json:"halfGoal"`
	League     string      `json:"league"`
	ScheduleId int         `json:"scheduleId"`
	MatchId    int         `json:"matchId"`
	LetGoal    interface{} `json:"letGoal"`
	Separate   int         `json:"separate"`
}

type LeagueSummary struct {
	HomeLeague  string                  `json:"homeLeague"`
	GuestLeague string                  `json:"guestLeague"`
	Home        LeagueSummaryDetailPack `json:"home"`
	Guest       LeagueSummaryDetailPack `json:"guest"`
}

type LeagueSummaryDetailPack struct {
	All   LeagueSummaryDetail `json:"all"`
	Home  LeagueSummaryDetail `json:"home"`
	Guest LeagueSummaryDetail `json:"guest"`
}

type LeagueSummaryDetail struct {
	WinScoreAvg float64 `json:"winScoreAvg"`
	LoseGoal    string  `json:"loseGoal"`
	Point       string  `json:"point"`
	Win         string  `json:"win"`
	Lose        string  `json:"lose"`
	Draw        string  `json:"draw"`
	Game        string  `json:"game"`
	Rank        string  `json:"rank"`
	WinGoal     string  `json:"winGoal"`
}

type RankData struct {
	List  []RankItem  `json:"list"`
	Color []ColorItem `json:"color"`
}

type RankItem struct {
	Rank       int    `json:"rank"`
	Name       string `json:"name"`
	Game       int    `json:"game"`
	Win        int    `json:"win"`
	Draw       int    `json:"draw"`
	Lose       int    `json:"lose"`
	WinGoal    int    `json:"winGoal"`
	LostGoal   int    `json:"lostGoal"`
	TotalScore int    `json:"totalScore"`
	Color      string `json:"color"`
	TeamId     int    `json:"teamId"`
}

type ColorItem struct {
	LeagueId   int    `json:"leagueId"`
	Season     string `json:"season"`
	Color      string `json:"color"`
	NameJ      string `json:"nameJ"`
	NameE      string `json:"nameE"`
	NameF      string `json:"nameF"`
	Shengjiang int    `json:"shengjiang"`
}

// OddsEuroResponse corresponds to odds_euro.json
type OddsEuroResponse struct {
	Odds        []EuroOdd   `json:"odds"`
	RiseAndFall interface{} `json:"riseAndFall"`
}

type EuroOdd struct {
	CompanyId        string      `json:"companyId"`
	CompanyName      string      `json:"companyName"`
	Odds             []string    `json:"odds"` // "2.67"
	FirstOdds        []string    `json:"firstOdds"`
	ReturnRatio      string      `json:"returnRatio"` // "91.24%"
	FirstReturnRatio string      `json:"firstReturnRatio"`
	Ratio            []string    `json:"ratio"` // "34%"
	FirstRatio       []string    `json:"firstRatio"`
	OddsTrend        []int       `json:"oddsTrend"` // -1, 1
	RatioTrend       []int       `json:"ratioTrend"`
	FirstKelly       interface{} `json:"firstKelly"` // Can be float array or string array in JSON
	Kelly            interface{} `json:"kelly"`
}

type RiseAndFall struct {
	RiseNumber  []int    `json:"riseNumber"`
	RisePercent []string `json:"risePercent"`
	FallNumber  []int    `json:"fallNumber"`
	FallPercent []string `json:"fallPercent"`
}

// OddsPankouResponse corresponds to odds_pankou.json
type OddsPankouResponse struct {
	Asia []PankouItem `json:"asia"`
	Dxq  []PankouItem `json:"dxq"`
}

type PankouItem struct {
	CompanyId        int      `json:"companyId"`
	CompanyName      string   `json:"companyName"`
	OddsTrend        []int    `json:"oddsTrend"`
	Odds             []string `json:"odds"`
	FirstOdds        []string `json:"firstOdds"`
	FirstPankou      string   `json:"firstPankou"`
	Pankou           string   `json:"pankou"`
	FirstReturnRatio string   `json:"firstReturnRatio"`
	ReturnRatio      string   `json:"returnRatio"`
}
