// Finite offline model maintenance. No HTTP listener, migrations or service restart.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go_server/config"
	"go_server/handlers"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"log"
	"os"
	"path/filepath"
	"time"
)

func main() {
	input := flag.String("input", "", "optional existing SELECT-only dataset")
	out := flag.String("out", "../reports/model-update-20261121/training.json", "training projection path")
	update := flag.Bool("update", false, "run startup lifecycle against latest DB; writes local model files only")
	flag.Parse()
	var source handlers.DrawFeatureStudyDataset
	if *input != "" {
		data, err := os.ReadFile(*input)
		if err != nil {
			log.Fatal(err)
		}
		if err = json.Unmarshal(data, &source); err != nil {
			log.Fatal(err)
		}
	} else {
		db, err := gorm.Open(mysql.Open(config.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			log.Fatal("source DB unavailable")
		}
		connection, err := db.DB()
		if err != nil {
			log.Fatal("source DB unavailable")
		}
		defer connection.Close()
		if *update {
			status := handlers.RefreshDrawObservationModel(context.Background(), db)
			body, _ := json.Marshal(status)
			fmt.Println(string(body))
			if status.State == "fallback" {
				os.Exit(1)
			}
			return
		}
		source, err = handlers.ExportDrawFeatureStudy(db, time.Now())
		if err != nil {
			log.Fatal(err)
		}
	}
	snapshot, err := handlers.BuildDrawObservationTrainingSnapshot(source)
	if err != nil {
		log.Fatal(err)
	}
	body, err := json.Marshal(snapshot)
	if err != nil {
		log.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		log.Fatal(err)
	}
	if err = os.WriteFile(*out, body, 0600); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("rows=%d dataSHA256=%s output=%s servicesStarted=false\n", len(snapshot.Rows), snapshot.DataSHA256, *out)
}
