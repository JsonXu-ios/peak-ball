package models

import (
	"gorm.io/datatypes"
	"time"
)

// DrawHistoricalRun retains reconstructed history separately from genuine
// frozen pre-match predictions. Compressed payloads bound storage for large pools.
type DrawHistoricalRun struct {
	ID           uint64         `gorm:"primaryKey;autoIncrement"`
	ModelVersion string         `gorm:"type:varchar(100);not null"`
	GeneratedAt  time.Time      `gorm:"not null;index"`
	Samples      int            `gorm:"not null"`
	Active       bool           `gorm:"not null;default:false;index"`
	Report       []byte         `gorm:"type:longblob;not null"`
	Examples     []byte         `gorm:"type:longblob;not null"`
	Artifact     datatypes.JSON `gorm:"type:json"`
	CreatedAt    time.Time
}
