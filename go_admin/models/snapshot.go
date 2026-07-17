package models

import "time"

// StatSnapshot persists a manually computed statistics result so page loads
// read it directly instead of recomputing (survives server restarts).
type StatSnapshot struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Kind        string    `gorm:"size:64;uniqueIndex;comment:快照类型" json:"kind"`
	Payload     string    `gorm:"type:longtext;comment:JSON结果" json:"-"`
	GeneratedAt time.Time `gorm:"comment:统计时间" json:"generatedAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
