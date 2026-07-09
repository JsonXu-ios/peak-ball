// Package config provides application configuration values.
package config

import "os"

var (
	// DSN is the MySQL data source name.
	DSN = getEnv("GO_SERVER_DSN", "root:123456@tcp(127.0.0.1:3306)/football_data?charset=utf8mb4&parseTime=True&loc=Local")

	// ServerAddr is the address the HTTP server listens on.
	ServerAddr = getEnv("GO_SERVER_ADDR", ":18080")

	// FootballImgDir is the path to the football images directory.
	FootballImgDir = getEnv("FOOTBALL_IMG_DIR", "../public/footballimg")

	// AnalysisRuleSnapshotPath is the portable checked-in rule snapshot used by the analysis page.
	AnalysisRuleSnapshotPath = getEnv("ANALYSIS_RULE_SNAPSHOT_PATH", "go_server/data/analysis_rule_snapshot.json")
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
