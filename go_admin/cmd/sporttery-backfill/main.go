// Backfill existing fixtures only; no listener, database.Init or migrations.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"go_admin/config"
	"go_admin/database"
	"go_admin/handlers"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	dates := flag.String("dates", "", "Comma-separated source schedule dates")
	match := flag.String("match-id", "", "One existing match ID")
	all := flag.Bool("all-missing", false, "Backfill all existing sporttery fixtures missing records")
	audit := flag.Bool("audit", false, "Read-only counts of captured, unopened and missing sporttery histories")
	force := flag.Bool("force", false, "Also refresh already captured records")
	flag.Parse()
	choices := 0
	for _, selected := range []bool{*dates != "", *match != "", *all, *audit} {
		if selected {
			choices++
		}
	}
	if choices != 1 {
		log.Fatal("specify exactly one of -dates, -match-id, -all-missing or -audit")
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=UTC", config.DBUser, config.DBPassword, config.DBHost, config.DBPort, config.DBName)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		log.Fatal("cannot connect source database")
	}
	database.DB = db
	connection, err := db.DB()
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()
	query := db.Table("moneys m").Joins("LEFT JOIN odds_moneys o ON o.match_id=m.match_id").Where("TRIM(COALESCE(m.jingcai_id,'')) <> ''")
	if *audit {
		var rows []struct {
			MatchID string
			Raw     string
		}
		if err = db.Table("moneys m").Joins("LEFT JOIN odds_moneys o ON o.match_id=m.match_id").Where("TRIM(COALESCE(m.jingcai_id,'')) <> ''").Select("m.match_id,COALESCE(o.sporttery_trade,'') AS raw").Scan(&rows).Error; err != nil {
			log.Fatal("cannot audit captures")
		}
		counts := map[string]int{"total": len(rows), "priced": 0, "unopened": 0, "no_quotes": 0, "missing": 0, "invalid": 0}
		for _, row := range rows {
			var payload map[string]json.RawMessage
			_ = json.Unmarshal([]byte(row.Raw), &payload)
			if len(payload["sportteryOdds"]) == 0 {
				counts["missing"]++
				continue
			}
			counts[handlers.SportteryOddsCaptureState(payload["sportteryOdds"])]++
		}
		body, _ := json.Marshal(counts)
		fmt.Println(string(body))
		return
	}
	if *match != "" {
		query = query.Where("m.match_id = ?", *match)
	}
	if *dates != "" {
		values := strings.Split(*dates, ",")
		for i, value := range values {
			values[i] = strings.TrimSpace(value)
			if _, err = time.Parse("2006-01-02", values[i]); err != nil {
				log.Fatal("invalid date")
			}
		}
		query = query.Where("m.date IN ?", values)
	}
	if !*force {
		query = query.Where("JSON_EXTRACT(o.sporttery_trade, '$.sportteryOdds') IS NULL OR JSON_TYPE(JSON_EXTRACT(o.sporttery_trade, '$.sportteryOdds')) = 'NULL'")
	}
	var ids []string
	if err = query.Distinct("m.match_id").Pluck("m.match_id", &ids).Error; err != nil {
		log.Fatal("cannot enumerate existing fixtures")
	}
	sort.Strings(ids)
	fmt.Printf("selected=%d source=browser existing_matches_only=true\n", len(ids))
	if len(ids) == 0 {
		return
	}
	script, err := handlers.SportteryBrowserScript()
	if err != nil {
		log.Fatal(err)
	}
	cmd := exec.Command("node", script, "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		log.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 65536), 4<<20)
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	done, success, failed, unopened, noQuotes := 0, 0, 0, 0, 0
	started := time.Now()
	for scanner.Scan() {
		var result struct {
			MatchID string `json:"matchId"`
			Status  int    `json:"status"`
			Body    string `json:"body"`
			Error   string `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &result) != nil || !wanted[result.MatchID] {
			failed++
			fmt.Println("invalid collector response")
			continue
		}
		delete(wanted, result.MatchID)
		done++
		if result.Status != 200 {
			failed++
			fmt.Printf("%d/%d match=%s failed HTTP=%d error=%s\n", done, len(ids), result.MatchID, result.Status, result.Error)
			continue
		}
		if err = handlers.ImportSportteryOddsCapture(result.MatchID, []byte(result.Body)); err != nil {
			failed++
			fmt.Printf("%d/%d match=%s failed=%s\n", done, len(ids), result.MatchID, err)
			continue
		}
		success++
		state := handlers.SportteryOddsCaptureState([]byte(result.Body))
		if state == "unopened" {
			unopened++
		}
		if state == "no_quotes" {
			noQuotes++
		}
		fmt.Printf("%d/%d match=%s saved state=%s\n", done, len(ids), result.MatchID, state)
	}
	if err = scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		log.Fatal("collector output failed")
	}
	processErr := cmd.Wait()
	fmt.Printf("completed selected=%d saved=%d priced=%d unopened=%d no_quotes=%d failed=%d missing=%d elapsed=%s\n", len(ids), success, success-unopened-noQuotes, unopened, noQuotes, failed, len(wanted), time.Since(started).Round(time.Millisecond))
	if processErr != nil || failed > 0 || len(wanted) > 0 {
		os.Exit(1)
	}
}
