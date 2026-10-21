// Package drawmodel contains the independent, pre-match draw probability model.
// It has no dependency on the database, HTTP handlers, or crawler payloads.
package drawmodel

import "time"

const Version = "draw-poisson-dc-v1"

// Match goals must be regulation-time goals, including stoppage time. Never
// include extra-time or shoot-out goals. IDs are preferred over team names.
type Match struct {
	ID           string    `json:"id"`
	LeagueID     string    `json:"league_id"`
	LeagueName   string    `json:"league_name"`
	HomeTeamID   string    `json:"home_team_id"`
	AwayTeamID   string    `json:"away_team_id"`
	HomeTeamName string    `json:"home_team_name"`
	AwayTeamName string    `json:"away_team_name"`
	Kickoff      time.Time `json:"kickoff"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	HomeGoals    int       `json:"home_goals"`
	AwayGoals    int       `json:"away_goals"`
	Completed    bool      `json:"completed"`
}

// Odds must be a simultaneous three-way decimal quote. A missing timestamp
// cannot establish that a quote was available before a prediction.
type Odds struct {
	Home          float64   `json:"home"`
	Draw          float64   `json:"draw"`
	Away          float64   `json:"away"`
	CapturedAt    time.Time `json:"captured_at"`
	AsianHandicap *float64  `json:"asian_handicap,omitempty"`
	GoalLine      *float64  `json:"goal_line,omitempty"`
}

type Input struct {
	Target   Match
	AsOf     time.Time
	History  []Match
	Odds     *Odds
	Artifact *Artifact
}

type Form struct {
	N                int      `json:"n"`
	Draws            int      `json:"draws"`
	DrawRate         float64  `json:"draw_rate"`
	SmoothedDrawRate float64  `json:"smoothed_draw_rate"`
	GoalsFor         float64  `json:"goals_for"`
	GoalsAgainst     float64  `json:"goals_against"`
	LowScoreRate     float64  `json:"low_score_rate"`
	CleanSheetRate   float64  `json:"clean_sheet_rate"`
	ScorelessRate    float64  `json:"scoreless_rate"`
	MatchIDs         []string `json:"match_ids"`
}

type Baseline struct {
	N         int     `json:"n"`
	DrawRate  float64 `json:"draw_rate"`
	HomeGoals float64 `json:"home_goals"`
	AwayGoals float64 `json:"away_goals"`
	Source    string  `json:"source"`
}

type Scoreline struct {
	Home        int     `json:"home"`
	Away        int     `json:"away"`
	Probability float64 `json:"probability"`
}

type ModelInfo struct {
	Version            string     `json:"version"`
	Rho                float64    `json:"rho"`
	RhoFitted          bool       `json:"rho_fitted"`
	FusionFitted       bool       `json:"fusion_fitted"`
	Calibrated         bool       `json:"calibrated"`
	TrainingSamples    int        `json:"training_samples"`
	CalibrationSamples int        `json:"calibration_samples"`
	TrainingCutoff     *time.Time `json:"training_cutoff,omitempty"`
	NameFallback       bool       `json:"name_fallback"`
	HalfLifeDays       float64    `json:"half_life_days"`
	ShrinkageMatches   float64    `json:"shrinkage_matches"`
}

type Result struct {
	Available             bool        `json:"available"`
	Probability           float64     `json:"probability"`
	ScoreModelProbability float64     `json:"score_model_probability"`
	MarketProbability     *float64    `json:"market_probability,omitempty"`
	HomeLambda            float64     `json:"home_lambda"`
	AwayLambda            float64     `json:"away_lambda"`
	HomeOverall           Form        `json:"home_overall"`
	HomeVenue             Form        `json:"home_venue"`
	AwayOverall           Form        `json:"away_overall"`
	AwayVenue             Form        `json:"away_venue"`
	LeagueBaseline        Baseline    `json:"league_baseline"`
	TopScorelines         []Scoreline `json:"top_scorelines"`
	Warnings              []string    `json:"warnings"`
	Reasons               []string    `json:"reasons"`
	Model                 ModelInfo   `json:"model"`
}

// TrainingExample must contain an immutable pre-match snapshot. Draw is the
// observed regulation-time outcome. Fit never uses target goals as features.
type TrainingExample struct {
	Target  Match
	AsOf    time.Time
	History []Match
	Odds    *Odds
	Draw    bool
}

// Artifact is serializable. A rejected or insufficient fit has Fitted=false
// and explanatory Warnings, and Predict continues using the score model.
type Artifact struct {
	Version              string    `json:"version"`
	Fitted               bool      `json:"fitted"`
	Calibrated           bool      `json:"calibrated"`
	FeatureNames         []string  `json:"feature_names"`
	Means                []float64 `json:"means"`
	Scales               []float64 `json:"scales"`
	Coefficients         []float64 `json:"coefficients"`
	Intercept            float64   `json:"intercept"`
	CalibrationIntercept float64   `json:"calibration_intercept"`
	CalibrationSlope     float64   `json:"calibration_slope"`
	TrainingSamples      int       `json:"training_samples"`
	CalibrationSamples   int       `json:"calibration_samples"`
	TrainingStart        time.Time `json:"training_start"`
	TrainingEnd          time.Time `json:"training_end"`
	CalibrationStart     time.Time `json:"calibration_start"`
	CalibrationEnd       time.Time `json:"calibration_end"`
	TrainingCutoff       time.Time `json:"training_cutoff"`
	Warnings             []string  `json:"warnings"`
}
