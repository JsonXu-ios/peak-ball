// Finite offline read-only export. Starts no application or background service.
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
	"go_server/internal/drawfullmodel"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func save(path string, value interface{}) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	return body, os.WriteFile(path, body, 0600)
}

func main() {
	out := flag.String("out", "../reports/full-model-20261104/raw.json", "new independent full input dataset")
	matrix := flag.String("matrix", "", "optional stable Go-extracted matrix")
	input := flag.String("input", "", "optional existing full raw dataset; extract offline without database access")
	flag.Parse()
	var data handlers.DrawComprehensiveStudyDataset
	var body []byte
	var err error
	if *input != "" {
		body, err = os.ReadFile(*input)
		if err == nil {
			err = json.Unmarshal(body, &data)
		}
		if err != nil {
			log.Fatal(err)
		}
	} else {
		db, openErr := gorm.Open(mysql.Open(config.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if openErr != nil {
			log.Fatal("cannot connect source database (credentials withheld)")
		}
		connection, openErr := db.DB()
		if openErr != nil {
			log.Fatal("cannot open source connection")
		}
		defer connection.Close()
		data, err = handlers.ExportDrawComprehensiveStudy(db, time.Now())
		if err != nil {
			log.Fatal(err)
		}
		body, err = save(*out, data)
		if err != nil {
			log.Fatal(err)
		}
	}
	sourceSHA := fmt.Sprintf("%x", sha256.Sum256(body))
	if *matrix != "" {
		metadata := drawfullmodel.Metadata()
		names := make([]string, len(metadata))
		for i, m := range metadata {
			names[i] = m.Name
		}
		type matrixRow struct {
			MatchID string     `json:"matchId"`
			Values  []*float64 `json:"values"`
		}
		rows := make([]matrixRow, 0, len(data.Rows))
		coverage := map[string]int{}
		for _, raw := range data.Rows {
			var id struct {
				MatchID string `json:"matchId"`
			}
			if err = json.Unmarshal(raw, &id); err != nil {
				log.Fatal(err)
			}
			features, err := drawfullmodel.ExtractFeatures(raw)
			if err != nil {
				log.Fatal(err)
			}
			values := make([]*float64, len(metadata))
			modules := map[string]bool{}
			for j, m := range metadata {
				if v, ok := features[m.Name]; ok {
					x := v
					values[j] = &x
					modules[m.Module] = true
				}
			}
			for module := range modules {
				coverage[module]++
			}
			rows = append(rows, matrixRow{MatchID: id.MatchID, Values: values})
		}
		if _, err = save(*matrix, map[string]interface{}{"version": drawfullmodel.InputVersion, "sourceSHA256": sourceSHA, "sourceGeneratedAt": data.GeneratedAt, "featureNames": names, "metadata": metadata, "rows": rows, "moduleCoverage": coverage}); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("matrix=%s columns=%d rows=%d modules=%v\n", *matrix, len(metadata), len(rows), coverage)
	}
	fmt.Printf("output=%s rows=%d sourceSHA256=%s readOnly=true servicesStarted=false\n", *out, len(data.Rows), sourceSHA)
}
