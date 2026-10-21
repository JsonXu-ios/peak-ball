package models

import (
	"time"

	"gorm.io/datatypes"
)

// DrawFeatureSnapshot is the immutable information available at prediction time.
// Input serializes drawmodel.Input; its Artifact may be omitted because the
// prediction records its model version and artifacts are stored separately.
type DrawFeatureSnapshot struct {
	ID             uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	MatchID        string         `gorm:"type:varchar(32);not null;index" json:"matchId"`
	AsOf           time.Time      `gorm:"not null;index" json:"asOf"`
	Kickoff        time.Time      `gorm:"not null;index" json:"kickoff"`
	FeatureVersion string         `gorm:"type:varchar(100);not null" json:"featureVersion"`
	Input          datatypes.JSON `gorm:"type:json;not null" json:"input"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// DrawPrediction freezes the first pre-match prediction for each match. All
// probability columns are fractions in [0,1]; Payload contains the API view in
// percentage points. Only result/settlement columns may be updated afterwards.
type DrawPrediction struct {
	ID                  uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	MatchID             string         `gorm:"type:varchar(32);not null;uniqueIndex" json:"matchId"`
	SnapshotID          uint64         `gorm:"not null;index" json:"snapshotId"`
	PredictionAt        time.Time      `gorm:"not null;index" json:"predictionAt"`
	Kickoff             time.Time      `gorm:"not null;index" json:"kickoff"`
	ModelVersion        string         `gorm:"type:varchar(100);not null;index" json:"modelVersion"`
	Probability         float64        `gorm:"not null" json:"probability"`
	ModelProbability    float64        `gorm:"not null" json:"modelProbability"`
	MarketProbability   *float64       `json:"marketProbability"`
	BaselineProbability float64        `gorm:"not null" json:"baselineProbability"`
	Candidate           bool           `gorm:"not null;default:false;index" json:"candidate"`
	Quality             string         `gorm:"type:varchar(32);not null" json:"quality"`
	Payload             datatypes.JSON `gorm:"type:json;not null" json:"payload"`
	Settled             bool           `gorm:"not null;default:false;index" json:"settled"`
	IsDraw              *bool          `json:"isDraw"`
	HomeScore           *int           `json:"homeScore"`
	AwayScore           *int           `json:"awayScore"`
	SettledAt           *time.Time     `json:"settledAt"`
	ResultAvailableAt   *time.Time     `json:"resultAvailableAt"`
	CreatedAt           time.Time      `json:"createdAt"`
}

// DrawModelRun retains every training attempt, including rejected ones. Only a
// successfully fitted and calibrated artifact can be Active.
type DrawModelRun struct {
	ID           uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	ModelVersion string         `gorm:"type:varchar(100);not null;index" json:"modelVersion"`
	Artifact     datatypes.JSON `gorm:"type:json;not null" json:"artifact"`
	Samples      int            `gorm:"not null" json:"samples"`
	Calibrated   bool           `gorm:"not null;default:false" json:"calibrated"`
	Active       bool           `gorm:"not null;default:false;index" json:"active"`
	TrainedAt    time.Time      `gorm:"not null" json:"trainedAt"`
	CreatedAt    time.Time      `json:"createdAt"`
}
