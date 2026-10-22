package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"go_server/database"
	"gorm.io/gorm"
)

func drawObservationDigest(value interface{}) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Schema/identity are small stable metadata. Every request checks only source
// counters/timestamps and the selected day's quote counters, never history JSON.
func newDrawObservationSourceLoader() func(drawObservationQuery) (drawObservationSourceVersion, error) {
	var mu sync.Mutex
	var schema drawObservationSchema
	namespace := ""
	return func(q drawObservationQuery) (drawObservationSourceVersion, error) {
		db := database.DB
		if db == nil {
			return drawObservationSourceVersion{}, fmt.Errorf("数据库尚未连接")
		}
		mu.Lock()
		if q.refresh {
			schema = nil
		}
		if schema == nil {
			value, err := drawObservationReadSchema(db)
			if err != nil {
				mu.Unlock()
				return drawObservationSourceVersion{}, err
			}
			var identity struct{ Name, Server string }
			if err := db.Raw("SELECT DATABASE() AS name, @@server_uuid AS server").Scan(&identity).Error; err != nil {
				mu.Unlock()
				return drawObservationSourceVersion{}, err
			}
			schema = value
			namespace = "peak-ball:draw-observations:" + drawObservationResultVersion + ":" + drawObservationDigest(identity)[:20]
		}
		currentSchema, currentNamespace := schema, namespace
		mu.Unlock()
		coreParts := []interface{}{drawObservationResultVersion, currentSchema}
		ids := []string{}
		for _, table := range []string{"moneys", "money", "history_moneys"} {
			if !currentSchema[table]["match_id"] {
				continue
			}
			version, err := drawObservationTableRevision(db, currentSchema, table, nil)
			if err != nil {
				return drawObservationSourceVersion{}, err
			}
			coreParts = append(coreParts, table, version)
			if table != "history_moneys" {
				var rows []string
				if err := drawObservationSQLScope(db.Table(table), q).Pluck("match_id", &rows).Error; err != nil {
					return drawObservationSourceVersion{}, err
				}
				ids = append(ids, rows...)
			}
		}
		core := drawObservationDigest(coreParts)
		dayParts := []interface{}{core, q.date, q.start, q.end}
		for _, table := range []string{"odds_moneys", "pankou_moneys"} {
			if !currentSchema[table]["match_id"] {
				continue
			}
			version, err := drawObservationTableRevision(db, currentSchema, table, ids)
			if err != nil {
				return drawObservationSourceVersion{}, err
			}
			dayParts = append(dayParts, table, version)
		}
		return drawObservationSourceVersion{Namespace: currentNamespace, Core: core, Day: drawObservationDigest(dayParts)}, nil
	}
}

func drawObservationTableRevision(db *gorm.DB, schema drawObservationSchema, table string, ids []string) (string, error) {
	query := db.Table(table)
	if ids != nil {
		if len(ids) == 0 {
			return "empty", nil
		}
		query = query.Where("match_id IN ?", ids)
	}
	fields := []string{"COUNT(*) AS row_count"}
	if schema[table]["id"] {
		fields = append(fields, "COALESCE(MAX(id),0) AS last_id")
	}
	if schema[table]["updated_at"] {
		fields = append(fields, "COALESCE(CAST(MAX(updated_at) AS CHAR),'') AS changed")
		fields = append(fields, "COALESCE(CAST(SUM(TIMESTAMPDIFF(MICROSECOND,'1970-01-01 00:00:00',updated_at)) AS CHAR),'') AS update_sum")
	} else {
		// Old schemas without an update timestamp need a content signature; a
		// count alone must never silently accept edited historical data.
		columns := []string{}
		wanted := drawObservationHeaderColumns
		switch table {
		case "history_moneys":
			wanted = []string{"match_id", "league_stat", "against_summary", "against_list", "recent_home_list", "recent_guest_list"}
		case "odds_moneys":
			wanted = drawObservationFrontendOddsColumns
		case "pankou_moneys":
			wanted = drawObservationFrontendPankouColumns
		}
		for _, column := range drawObservationProjection(schema, table, wanted) {
			columns = append(columns, "COALESCE(CAST(`"+column+"` AS CHAR),'')")
		}
		if len(columns) > 0 {
			fields = append(fields, "COALESCE(BIT_XOR(CRC32(CONCAT_WS('#',"+strings.Join(columns, ",")+"))),0) AS checksum")
		}
	}
	var value struct {
		RowCount, LastID uint64
		Changed          string
		UpdateSum        string
		Checksum         uint64
	}
	if err := query.Select(strings.Join(fields, ",")).Scan(&value).Error; err != nil {
		return "", err
	}
	return drawObservationDigest(value), nil
}
