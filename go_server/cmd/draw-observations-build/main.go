// The offline builder opens no listening port and never calls database.Init.
package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"go_server/config"
	"go_server/database"
	"go_server/handlers"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	days := flag.String("dates", "", "Comma-separated Beijing source schedule dates")
	all := flag.Bool("all-dates", false, "Generate all source schedule dates")
	force := flag.Bool("force", false, "Explicitly regenerate existing results")
	flag.Parse()
	if (*days == "") == !*all {
		log.Fatal("specify exactly one of -dates or -all-dates")
	}
	db, err := gorm.Open(mysql.Open(config.DSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal("cannot connect source database")
	}
	database.DB = db
	connection, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()
	dates := []string{}
	if *all {
		var tables []string
		if err = db.Raw("SELECT TABLE_NAME FROM information_schema.tables WHERE table_schema=DATABASE() AND TABLE_NAME IN ('moneys','money')").Scan(&tables).Error; err != nil {
			log.Fatal(err)
		}
		unique := map[string]bool{}
		for _, table := range tables {
			var rows []string
			if err = db.Table(table).Distinct("DATE_FORMAT(date, '%Y-%m-%d')").Where("date IS NOT NULL").Pluck("DATE_FORMAT(date, '%Y-%m-%d')", &rows).Error; err != nil {
				log.Fatal(err)
			}
			for _, day := range rows {
				if day != "" {
					unique[day] = true
				}
			}
		}
		for day := range unique {
			dates = append(dates, day)
		}
		sort.Strings(dates)
	} else {
		for _, day := range strings.Split(*days, ",") {
			dates = append(dates, strings.TrimSpace(day))
		}
	}
	start := time.Now()
	if err = handlers.MaterializeDrawObservationDates(dates, *force, func(day string, count int, took time.Duration) {
		fmt.Printf("%s matches=%d elapsed=%s\n", day, count, took.Round(time.Millisecond))
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("completed dates=%d elapsed=%s; source database read-only\n", len(dates), time.Since(start).Round(time.Millisecond))
}
