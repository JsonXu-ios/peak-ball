// Package config provides configuration settings for the admin server.
package config

import (
	"os"
)

var (
	// ServerAddr is the address the server will listen on
	ServerAddr = getEnv("ADMIN_SERVER_ADDR", ":8081")

	// DBHost is the database host
	DBHost = getEnv("DB_HOST", "127.0.0.1")

	// DBPort is the database port
	DBPort = getEnv("DB_PORT", "3306")

	// DBUser is the database user
	DBUser = getEnv("DB_USER", "root")

	// DBPassword is the database password
	DBPassword = getEnv("DB_PASSWORD", "123456")

	// DBName is the database name
	DBName = getEnv("DB_NAME", "football_data")

	// JWTSecret is the secret key for JWT token
	JWTSecret = getEnv("JWT_SECRET", "admin_secret_key_change_in_production")

	// FootballImgDir is the directory for football team logos
	FootballImgDir = getEnv("FOOTBALL_IMG_DIR", "../public/footballimg")

	// AnalysisRuleSnapshotPath is the checked-in historical rule snapshot file.
	AnalysisRuleSnapshotPath = getEnv("ANALYSIS_RULE_SNAPSHOT_PATH", "../go_server/data/analysis_rule_snapshot.json")

	// AnalysisAPIBaseURL points to the public analysis API used for rule snapshot generation.
	AnalysisAPIBaseURL = getEnv("ANALYSIS_API_BASE_URL", "http://127.0.0.1:18080/api")
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
