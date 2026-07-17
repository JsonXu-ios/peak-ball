// Package handlers: snapshot.go stores manually computed statistics results in
// the stat_snapshots table so page loads read them directly.
package handlers

import (
	"time"

	"go_admin/database"
	"go_admin/models"
)

const (
	snapshotKindMatchStatistics = "match_statistics"
	snapshotKindRecommendations = "signal_recommendations"
)

func saveStatSnapshot(kind string, payload []byte, generatedAt time.Time) error {
	var existing models.StatSnapshot
	err := database.DB.Where("kind = ?", kind).First(&existing).Error
	snapshot := models.StatSnapshot{Kind: kind, Payload: string(payload), GeneratedAt: generatedAt}
	if err == nil {
		snapshot.ID = existing.ID
		snapshot.CreatedAt = existing.CreatedAt
		return database.DB.Save(&snapshot).Error
	}
	return database.DB.Create(&snapshot).Error
}

func loadStatSnapshot(kind string) ([]byte, time.Time, bool) {
	var snapshot models.StatSnapshot
	if err := database.DB.Where("kind = ?", kind).First(&snapshot).Error; err != nil {
		return nil, time.Time{}, false
	}
	return []byte(snapshot.Payload), snapshot.GeneratedAt, true
}
