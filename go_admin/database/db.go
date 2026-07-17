// Package database provides database connection and initialization.
package database

import (
	"fmt"
	"log"
	"os"
	"time"

	"go_admin/config"
	"go_admin/models"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the global database instance
var DB *gorm.DB

// Init initializes the database connection
func Init() error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		config.DBUser,
		config.DBPassword,
		config.DBHost,
		config.DBPort,
		config.DBName,
	)

	// ParameterizedQueries keeps bound values out of the SQL log. Without it a
	// single snapshot save inlines the multi-MB report JSON into one log line,
	// which freezes the IDE console.
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.New(
			log.New(os.Stdout, "\r\n", log.LstdFlags),
			logger.Config{
				SlowThreshold:        200 * time.Millisecond,
				LogLevel:             logger.Info,
				ParameterizedQueries: true,
			},
		),
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	DB = db
	log.Println("Database connection established")
	return nil
}

// AutoMigrate runs automatic migration for admin models
func AutoMigrate() error {
	return DB.AutoMigrate(
		&models.AdminUser{},
		&models.Role{},
		&models.Menu{},
		&models.Permission{},
		&models.RoleMenu{},
		&models.RolePermission{},
		&models.AdminUserRole{},
		&models.OperationLog{},
		&models.CrawlerTask{},
		&models.CrawlerLog{},
		&models.StatSnapshot{},
	)
}
