// Package handlers: snapshot.go stores manually computed statistics results in
// the stat_snapshots table so page loads read them directly.
package handlers

import (
	"time"

	"go_admin/models"
)

const (
	snapshotKindMatchStatistics = "match_statistics"
	snapshotKindRecommendations = "signal_recommendations"
	snapshotKindCrossStatistics = "cross_statistics"
)

func saveStatSnapshot(kind string, payload []byte, generatedAt time.Time) error {
	// statisticsDB: the payload is a multi-MB JSON blob; never let it near the SQL log.
	var existing models.StatSnapshot
	err := statisticsDB().Where("kind = ?", kind).First(&existing).Error
	snapshot := models.StatSnapshot{Kind: kind, Payload: string(payload), GeneratedAt: generatedAt}
	if err == nil {
		snapshot.ID = existing.ID
		snapshot.CreatedAt = existing.CreatedAt
		return statisticsDB().Save(&snapshot).Error
	}
	return statisticsDB().Create(&snapshot).Error
}

func loadStatSnapshot(kind string) ([]byte, time.Time, bool) {
	var snapshot models.StatSnapshot
	if err := statisticsDB().Where("kind = ?", kind).First(&snapshot).Error; err != nil {
		return nil, time.Time{}, false
	}
	return []byte(snapshot.Payload), snapshot.GeneratedAt, true
}
