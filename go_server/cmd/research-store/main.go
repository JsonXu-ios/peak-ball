// Finite research-store maintenance. No listener, service restart, source DDL,
// source UPDATE or business AutoMigrate occurs in this command.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/config"
	"go_server/database"
	"go_server/handlers"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"peakball/researchstore"
)

func main() {
	action := flag.String("action", "overview", "init, sync, overview, schema, inspect, takeover or takeover-inspect")
	out := flag.String("out", "", "optional JSON output file (no connection credentials)")
	date := flag.String("date", "", "Beijing schedule date for takeover-inspect")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var result any
	switch *action {
	case "init":
		if err := researchstore.InitSchema(ctx, config.ResearchBaseDSN, config.ResearchDatabase); err != nil {
			log.Fatal(err)
		}
		result = map[string]any{"database": config.ResearchDatabase, "schemaVersion": researchstore.SchemaVersion, "initialized": true, "businessDatabaseModified": false}
	case "sync", "takeover", "takeover-inspect":
		db, err := gorm.Open(mysql.Open(config.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			log.Fatal("cannot connect to business read source")
		}
		database.DB = db
		connection, err := db.DB()
		if err != nil {
			log.Fatal(err)
		}
		defer connection.Close()
		if *action == "sync" {
			status, err := handlers.SyncResearchStore(ctx)
			if err != nil {
				body, _ := json.Marshal(status)
				fmt.Fprintln(os.Stderr, string(body))
				log.Fatal(err)
			}
			result = status
		} else if *action == "takeover" {
			verification, err := handlers.PrimeDrawResearchTakeover(ctx, db)
			if err != nil {
				body, _ := json.Marshal(verification)
				fmt.Fprintln(os.Stderr, string(body))
				log.Fatal(err)
			}
			result = verification
		} else {
			gin.SetMode(gin.ReleaseMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("GET", "/api/draw/observations?mode=today&scope=sporttery&page_size=200&date="+url.QueryEscape(*date), nil)
			started := time.Now()
			handlers.GetDrawObservations(c)
			if recorder.Code != 200 {
				log.Fatalf("takeover observation returned %d: %s", recorder.Code, recorder.Body.String())
			}
			fmt.Printf("takeover_inspect_elapsed_ms=%.2f\n", float64(time.Since(started).Microseconds())/1000)
			result = json.RawMessage(recorder.Body.Bytes())
		}
	case "overview", "schema":
		store, err := handlers.OpenResearchStore(ctx)
		if err != nil {
			log.Fatal(err)
		}
		defer store.Close()
		result, err = store.Overview(ctx)
		if err != nil {
			log.Fatal(err)
		}
	case "inspect":
		// Exercise the real handlers in memory: no TCP listener or service is
		// started. The resulting small JSON is useful for UI contract review.
		gin.SetMode(gin.ReleaseMode)
		payload := map[string]any{}
		timings := map[string]float64{}
		for _, item := range []struct {
			name, path string
			handler    gin.HandlerFunc
		}{
			{"overview", "/research/overview", handlers.GetResearchOverview},
			{"features", "/research/features?page_size=25", handlers.GetResearchFeatures},
			{"auditIssues", "/research/audit/issues?page_size=25", handlers.GetResearchIssues},
			{"evaluations", "/research/evaluations", handlers.GetResearchEvaluations},
			{"syncStatus", "/research/sync/status", handlers.GetResearchSyncStatus},
		} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("GET", item.path, nil)
			started := time.Now()
			item.handler(c)
			timings[item.name] = float64(time.Since(started).Microseconds()) / 1000
			if recorder.Code != 200 {
				log.Fatalf("%s returned %d: %s", item.name, recorder.Code, recorder.Body.String())
			}
			payload[item.name] = json.RawMessage(recorder.Body.Bytes())
		}
		payload["timingsMs"] = timings
		result = payload
	default:
		log.Fatal("unknown maintenance action")
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if *out != "" {
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
		fmt.Printf("action=%s output=%s business_database_modified=false\n", *action, path)
	} else {
		fmt.Println(string(body))
	}
}
