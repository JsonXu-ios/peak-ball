// Offline SELECT-only feature export: no listener, migrations or source writes.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"go_server/config"
	"go_server/handlers"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	out := flag.String("out", "../reports/draw-feature-study/dataset.json", "Output dataset path")
	flag.Parse()
	db, err := gorm.Open(mysql.Open(config.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal("cannot connect source database")
	}
	connection, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()
	data, err := handlers.ExportDrawFeatureStudy(db, time.Now())
	if err != nil {
		log.Fatal(err)
	}
	body, err := json.Marshal(data)
	if err != nil {
		log.Fatal(err)
	}
	path, err := filepath.Abs(*out)
	if err != nil {
		log.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		log.Fatal(err)
	}
	if err = os.WriteFile(path, body, 0600); err != nil {
		log.Fatal(err)
	}
	audit, _ := json.Marshal(data.Audit)
	fmt.Printf("output=%s rows=%d sha256=%x source_database_read_only=true\naudit=%s\n", path, len(data.Rows), sha256.Sum256(body), audit)
}
