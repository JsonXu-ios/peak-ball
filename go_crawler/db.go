package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB(dsn string) error {
	var err error
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second,  // Slow SQL threshold
			LogLevel:                  logger.Error, // Log level
			IgnoreRecordNotFoundError: true,         // Ignore ErrRecordNotFound error for logger
			Colorful:                  true,         // Disable color
		},
	)

	// Attempt to create database if it doesn't exist
	// This simple string replacement relies on the DSN format known in main.go
	baseDSN := strings.Replace(dsn, "/football_data", "/", 1)
	tmpDB, err := gorm.Open(mysql.Open(baseDSN), &gorm.Config{Logger: newLogger})
	if err == nil {
		tmpDB.Exec("CREATE DATABASE IF NOT EXISTS football_data CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;")
		sqlDB, _ := tmpDB.DB()
		sqlDB.Close()
	}

	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: newLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to connect database: %v", err)
	}

	// Migrate the schema
	err = DB.Set("gorm:table_options", "ENGINE=InnoDB").AutoMigrate(&Money{}, &HistoryMoney{}, &OddsMoney{}, &PankouMoney{})
	if err != nil {
		return fmt.Errorf("failed to migrate database: %v", err)
	}
	fmt.Println("Database connection established and migrated.")
	return nil
}
