package handlers

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
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

// Source signatures are aggregated inside MySQL: history JSON is never
// transferred or parsed just to check an already materialized day.
func newDrawObservationSourceLoader() func(drawObservationQuery) (drawObservationSourceVersion, error) {
	return newDrawObservationSourceLoaderForDB(func() *gorm.DB { return database.DB })
}

func newDrawObservationSourceLoaderForDB(resolve func() *gorm.DB) func(drawObservationQuery) (drawObservationSourceVersion, error) {
	return newDrawObservationSourceLoaderWithMemo(resolve, nil, false)
}

func newDrawObservationSourceLoaderWithMemo(resolve func() *gorm.DB, store drawObservationResultStore, inSnapshot bool) func(drawObservationQuery) (drawObservationSourceVersion, error) {
	var mu sync.Mutex
	var schema drawObservationSchema
	var metadataDB *gorm.DB
	namespace := ""
	return func(q drawObservationQuery) (drawObservationSourceVersion, error) {
		db := resolve()
		if db == nil {
			return drawObservationSourceVersion{}, fmt.Errorf("数据库尚未连接")
		}
		mu.Lock()
		if q.refresh || metadataDB != db {
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
			metadataDB = db
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
			version, err := drawObservationMemoizedTableRevision(db, store, currentNamespace, currentSchema, table, nil, inSnapshot, q.refresh)
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
		for _, table := range []string{"moneys", "money", "history_moneys", "odds_moneys", "pankou_moneys"} {
			if !currentSchema[table]["match_id"] {
				continue
			}
			version, err := drawObservationMemoizedTableRevision(db, store, currentNamespace, currentSchema, table, ids, inSnapshot, q.refresh, drawObservationRevisionDayScope(q))
			if err != nil {
				return drawObservationSourceVersion{}, err
			}
			dayParts = append(dayParts, table, version)
		}
		return drawObservationSourceVersion{Namespace: currentNamespace, Core: core, Day: drawObservationDigest(dayParts)}, nil
	}
}

type drawObservationRevisionMemo struct {
	Metadata  string `json:"metadata"`
	Signature string `json:"signature"`
}

// One Redis value per table/base or table/date survives process restarts without
// growing one key for every crawler timestamp. The metadata
// includes selected IDs and the exact consumed projection, so another date or
// schema cannot borrow a signature just because its counters happen to match.
// Normal crawler writes must advance updated_at (GORM's UpdateAll does); this
// optimization cannot detect a manual write that deliberately preserves it.
// Explicit refresh bypasses the memo and always verifies consumed content.
func drawObservationMemoizedTableRevision(db *gorm.DB, store drawObservationResultStore, namespace string, schema drawObservationSchema, table string, ids []string, inSnapshot, force bool, selectedScope ...string) (string, error) {
	if store == nil || !schema[table]["updated_at"] || table == "odds_moneys" || table == "pankou_moneys" || ids != nil && len(ids) == 0 {
		return drawObservationTableRevision(db, schema, table, ids)
	}
	scope := "base"
	if ids != nil {
		scope = "selected"
		if len(selectedScope) > 0 && selectedScope[0] != "" {
			scope = selectedScope[0]
		}
	}
	key := namespace + ":revision:" + table + ":" + scope
	metadata, err := drawObservationRevisionMetadata(db, schema, table, ids)
	if err != nil {
		return "", err
	}
	if !force {
		if signature, err := drawObservationStoredRevision(store, key, metadata); err != nil || signature != "" {
			return signature, err
		}
	}
	var signature string
	if inSnapshot {
		// The caller's source version, content and all later input reads share
		// this transaction; concurrent crawler commits cannot change them.
		signature, err = drawObservationTableRevision(db, schema, table, ids)
	} else {
		// A fast HTTP check has no enclosing transaction. On a memo miss, read
		// both metadata and content again in one snapshot before associating
		// them. An update between the initial GET and this read is harmless.
		err = db.Transaction(func(tx *gorm.DB) error {
			var txErr error
			metadata, txErr = drawObservationRevisionMetadata(tx, schema, table, ids)
			if txErr != nil {
				return txErr
			}
			if !force {
				signature, txErr = drawObservationStoredRevision(store, key, metadata)
				if txErr != nil || signature != "" {
					return txErr
				}
			}
			signature, txErr = drawObservationTableRevision(tx, schema, table, ids)
			return txErr
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	}
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(drawObservationRevisionMemo{Metadata: metadata, Signature: signature})
	if err != nil {
		return "", err
	}
	if err := store.set(context.Background(), key, data); err != nil {
		return "", err
	}
	return signature, nil
}

func drawObservationRevisionDayScope(q drawObservationQuery) string {
	if q.date != "" {
		return "day:" + q.date
	}
	return "range:" + drawObservationDigest([]string{q.start, q.end})
}

func drawObservationStoredRevision(store drawObservationResultStore, key, metadata string) (string, error) {
	data, err := store.get(context.Background(), key)
	if err != nil || len(data) == 0 {
		return "", err
	}
	var saved drawObservationRevisionMemo
	if len(data) > 16<<10 || json.Unmarshal(data, &saved) != nil || saved.Metadata != metadata {
		return "", nil
	}
	return saved.Signature, nil
}

func drawObservationRevisionMetadata(db *gorm.DB, schema drawObservationSchema, table string, ids []string) (string, error) {
	query := db.Table(table)
	sortedIDs := append([]string(nil), ids...)
	sort.Strings(sortedIDs)
	if ids != nil {
		query = query.Where("match_id IN ?", ids)
	}
	fields := []string{"COUNT(*) AS row_count", "COALESCE(CAST(MAX(updated_at) AS CHAR),'') AS changed", "COALESCE(CAST(SUM(TIMESTAMPDIFF(MICROSECOND,'1970-01-01 00:00:00',updated_at)) AS CHAR),'') AS update_sum"}
	if schema[table]["id"] {
		fields = append(fields, "COALESCE(MAX(id),0) AS last_id")
	}
	var value struct {
		RowCount, LastID   uint64
		Changed, UpdateSum string
	}
	if err := query.Select(strings.Join(fields, ",")).Scan(&value).Error; err != nil {
		return "", err
	}
	return drawObservationDigest([]interface{}{value, sortedIDs, drawObservationRevisionFields(schema, table, ids == nil)}), nil
}

func drawObservationTableRevision(db *gorm.DB, schema drawObservationSchema, table string, ids []string) (string, error) {
	query := db.Table(table)
	if ids != nil {
		if len(ids) == 0 {
			return "empty", nil
		}
		query = query.Where("match_id IN ?", ids)
	}
	fields := drawObservationRevisionFields(schema, table, ids == nil)
	var value struct {
		RowCount, LastID uint64
		Changed          string
		UpdateSum        string
		ContentSum       string
		Checksum         uint64
	}
	if err := query.Select(strings.Join(fields, ",")).Scan(&value).Error; err != nil {
		return "", err
	}
	return drawObservationDigest(value), nil
}

func drawObservationRevisionFields(schema drawObservationSchema, table string, global bool) []string {
	fields := []string{"COUNT(*) AS row_count"}
	if schema[table]["id"] {
		fields = append(fields, "COALESCE(MAX(id),0) AS last_id")
	}
	if schema[table]["updated_at"] && table != "moneys" && table != "money" && table != "history_moneys" {
		fields = append(fields, "COALESCE(CAST(MAX(updated_at) AS CHAR),'') AS changed")
		fields = append(fields, "COALESCE(CAST(SUM(TIMESTAMPDIFF(MICROSECOND,'1970-01-01 00:00:00',updated_at)) AS CHAR),'') AS update_sum")
	} else {
		// Crawlers save the complete match/history row, touching updated_at even
		// when only unused fields changed. The global base revision must follow
		// consumed content, or a live crawl invalidates every historical date.
		columns := []string{}
		wanted := drawObservationFrontendMainColumns
		if global {
			wanted = drawObservationHeaderColumns
		}
		switch table {
		case "history_moneys":
			wanted = drawObservationFrontendHistoryColumns
		case "odds_moneys":
			wanted = drawObservationFrontendOddsColumns
		case "pankou_moneys":
			wanted = drawObservationFrontendPankouColumns
		}
		projection := drawObservationProjection(schema, table, wanted)
		if global && table == "history_moneys" {
			// Match the base SELECT exactly. Most rows already have precise form
			// arrays, so their redundant (large) league_stat blob is not read.
			projection = drawObservationHistoryProjection(schema)
		}
		for _, column := range projection {
			expression := "`" + column + "`"
			if strings.HasSuffix(column, " AS league_stat") {
				expression = strings.TrimSuffix(column, " AS league_stat")
			}
			columns = append(columns, "COALESCE(CAST("+expression+" AS CHAR),'')")
		}
		if len(columns) > 0 {
			fields = append(fields, "COALESCE(BIT_XOR(CRC32(CONCAT_WS('#',"+strings.Join(columns, ",")+"))),0) AS checksum")
			fields = append(fields, "COALESCE(CAST(SUM(CRC32(CONCAT_WS('#',"+strings.Join(columns, ",")+"))) AS CHAR),'') AS content_sum")
		}
	}
	return fields
}
