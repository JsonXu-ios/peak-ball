// Package database handles GORM database initialization and connection management.
package database

import (
	"fmt"

	"go_server/config"
	"go_server/models"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// DB is the global database connection instance.
var DB *gorm.DB

// Init initializes the MySQL database connection and runs auto-migration.
func Init() error {
	var err error
	DB, err = gorm.Open(mysql.Open(config.DSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}

	if err := DB.AutoMigrate(
		// Match data
		&models.Money{},
		&models.HistoryMoney{},
		&models.OddsMoney{},
		&models.PankouMoney{},
		// User & social
		&models.User{},
		&models.FollowedTeam{},
		&models.Expert{},
		&models.ExpertTip{},
		&models.Prediction{},
		&models.LeaderboardEntry{},
		// News & transfers
		&models.News{},
		&models.TransferRumor{},
		// Notifications
		&models.Notification{},
		// Wallet & points
		&models.WalletTransaction{},
		&models.Reward{},
		// Search
		&models.SearchHistory{},
		// Owner betting picks (portable sample data)
		&models.UserPick{},
		// League standings
		&models.LeagueStanding{},
		&models.TopScorer{},
		&models.TeamInfoCache{},
	); err != nil {
		return fmt.Errorf("running auto-migration: %w", err)
	}

	return nil
}
