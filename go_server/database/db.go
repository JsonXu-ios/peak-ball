// Package database handles GORM database initialization and connection management.
package database

import (
	"fmt"
	"log"

	"go_server/config"
	"go_server/models"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// DB is the global database connection instance.
var DB *gorm.DB

// Init connects to MySQL. Schema changes require DB_AUTO_MIGRATE=true.
func Init() error {
	var err error
	DB, err = gorm.Open(mysql.Open(config.DSN), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	var identity struct {
		DatabaseName string
		Version      string
		Hostname     string
		Port         string
	}
	if err := DB.Raw("SELECT DATABASE() AS database_name, VERSION() AS version, @@hostname AS hostname, @@port AS port").Scan(&identity).Error; err != nil {
		return fmt.Errorf("reading database identity: %w", err)
	}
	log.Printf("Database connected: database=%s version=%s hostname=%s port=%s", identity.DatabaseName, identity.Version, identity.Hostname, identity.Port)
	if !config.AutoMigrateEnabled {
		log.Println("Automatic schema migration disabled (DB_AUTO_MIGRATE=false)")
		return nil
	}

	if err := DB.AutoMigrate(
		// Match data
		&models.Money{},
		&models.HistoryMoney{},
		&models.OddsMoney{},
		&models.PankouMoney{},
		// Independent pre-match draw prediction records
		&models.DrawFeatureSnapshot{},
		&models.DrawPrediction{},
		&models.DrawModelRun{},
		&models.DrawHistoricalRun{},
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
