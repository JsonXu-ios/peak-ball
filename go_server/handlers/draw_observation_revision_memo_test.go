package handlers

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This SELECT-only driver exposes independent metadata/content counters. A
// transaction freezes both counters while a simulated crawler can keep writing
// outside it, exercising the actual GORM transaction/connection routing.
type drawRevisionMemoData struct{ metadata, content int64 }
type drawRevisionMemoSQLState struct {
	mu                                  sync.Mutex
	data                                drawRevisionMemoData
	metadataReads, contentReads, begins int
	afterMetadata                       func(bool)
}
type drawRevisionMemoConnector struct{ state *drawRevisionMemoSQLState }
type drawRevisionMemoDriver struct{}
type drawRevisionMemoConn struct {
	state    *drawRevisionMemoSQLState
	snapshot *drawRevisionMemoData
}
type drawRevisionMemoTx struct{ conn *drawRevisionMemoConn }
type drawRevisionMemoRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

func (c drawRevisionMemoConnector) Connect(context.Context) (driver.Conn, error) {
	return &drawRevisionMemoConn{state: c.state}, nil
}
func (c drawRevisionMemoConnector) Driver() driver.Driver { return drawRevisionMemoDriver{} }
func (drawRevisionMemoDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}
func (c *drawRevisionMemoConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("only direct SELECTs are allowed")
}
func (c *drawRevisionMemoConn) Close() error { return nil }
func (c *drawRevisionMemoConn) Begin() (driver.Tx, error) {
	return nil, errors.New("explicit read-only isolation required")
}
func (c *drawRevisionMemoConn) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	if !options.ReadOnly || options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		return nil, fmt.Errorf("unsafe snapshot options: %+v", options)
	}
	c.state.mu.Lock()
	data := c.state.data
	c.state.begins++
	c.state.mu.Unlock()
	c.snapshot = &data
	return &drawRevisionMemoTx{conn: c}, nil
}
func (tx *drawRevisionMemoTx) Commit() error   { tx.conn.snapshot = nil; return nil }
func (tx *drawRevisionMemoTx) Rollback() error { tx.conn.snapshot = nil; return nil }
func (c *drawRevisionMemoConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "SELECT ") {
		return nil, errors.New("source writes are forbidden")
	}
	c.state.mu.Lock()
	data := c.state.data
	if c.snapshot != nil {
		data = *c.snapshot
	}
	if strings.Contains(query, " AS checksum") {
		c.state.contentReads++
		c.state.mu.Unlock()
		return &drawRevisionMemoRows{columns: []string{"row_count", "last_id", "checksum", "content_sum"}, values: []driver.Value{int64(3), int64(3), data.content, fmt.Sprint(data.content * 3)}}, nil
	}
	if strings.Contains(query, " AS update_sum") {
		c.state.metadataReads++
		after := c.state.afterMetadata
		c.state.afterMetadata = nil
		c.state.mu.Unlock()
		if after != nil {
			after(c.snapshot != nil)
		}
		return &drawRevisionMemoRows{columns: []string{"row_count", "last_id", "changed", "update_sum"}, values: []driver.Value{int64(3), int64(3), fmt.Sprint(data.metadata), fmt.Sprint(data.metadata * 3)}}, nil
	}
	c.state.mu.Unlock()
	return nil, fmt.Errorf("unexpected query: %s", query)
}
func (r *drawRevisionMemoRows) Columns() []string { return r.columns }
func (r *drawRevisionMemoRows) Close() error      { return nil }
func (r *drawRevisionMemoRows) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	copy(values, r.values)
	r.read = true
	return nil
}

func drawRevisionMemoDB(t *testing.T, state *drawRevisionMemoSQLState) *gorm.DB {
	t.Helper()
	connection := sql.OpenDB(drawRevisionMemoConnector{state: state})
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = connection.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: connection, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func drawRevisionMemoSchema() drawObservationSchema {
	return drawObservationSchema{"moneys": {"id": true, "match_id": true, "date": true, "home_score": true, "updated_at": true}}
}

func TestDrawObservationsRevisionMemoSurvivesNewReaderWithoutContentScan(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	store := newDrawResultMemoryStore()
	first, err := drawObservationMemoizedTableRevision(drawRevisionMemoDB(t, state), store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh DB reader has no process-local memo from the first one.
	second, err := drawObservationMemoizedTableRevision(drawRevisionMemoDB(t, state), store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, false)
	if err != nil || first != second || state.contentReads != 1 || state.begins != 1 || len(store.values) != 1 {
		t.Fatalf("durable memo not reused: content=%d transactions=%d keys=%d error=%v", state.contentReads, state.begins, len(store.values), err)
	}
}

func TestDrawObservationsRevisionMemoDistinguishesSaveTimestampFromChangedContent(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	store := newDrawResultMemoryStore()
	db := drawRevisionMemoDB(t, state)
	read := func() string {
		value, err := drawObservationMemoizedTableRevision(db, store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, false)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	first := read()
	state.data.metadata++
	if value := read(); value != first || state.contentReads != 2 {
		t.Fatal("timestamp-only update changed derived version or did not verify content")
	}
	state.data.metadata++
	state.data.content++
	if value := read(); value == first || state.contentReads != 3 {
		t.Fatal("changed input content did not invalidate derived version")
	}
	if len(store.values) != 1 {
		t.Fatal("source updates created unbounded revision keys")
	}
}

func TestDrawObservationsRevisionMemoCannotCrossDatabaseScopeOrProjection(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	store := newDrawResultMemoryStore()
	db := drawRevisionMemoDB(t, state)
	schema := drawRevisionMemoSchema()
	read := func(namespace string, ids []string) {
		_, err := drawObservationMemoizedTableRevision(db, store, namespace, schema, "moneys", ids, false, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	read("database-a", nil)
	read("database-b", nil)
	if state.contentReads != 2 {
		t.Fatal("different databases shared a memo")
	}
	read("database-a", []string{"match-a"})
	read("database-a", []string{"match-b"})
	if state.contentReads != 4 {
		t.Fatal("different selected matches shared a memo")
	}
	schema["moneys"]["home_rank"] = true
	read("database-a", []string{"match-b"})
	if state.contentReads != 5 {
		t.Fatal("changed consumed projection reused old signature")
	}
	if len(store.values) != 3 {
		t.Fatalf("selected dates or source updates grew extra memo keys: %d", len(store.values))
	}
}

func TestDrawObservationsRevisionMemoDateRoundTripReusesPersistentSignatures(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	store := newDrawResultMemoryStore()
	read := func(date, id string) {
		db := drawRevisionMemoDB(t, state) // Also discard any process-local state.
		scope := drawObservationRevisionDayScope(drawObservationQuery{date: date})
		_, err := drawObservationMemoizedTableRevision(db, store, "database-a", drawRevisionMemoSchema(), "moneys", []string{id}, false, false, scope)
		if err != nil {
			t.Fatal(err)
		}
	}
	read("2026-10-17", "a")
	read("2026-10-20", "b")
	read("2026-10-21", "c")
	read("2026-10-17", "a")
	if state.contentReads != 3 || len(store.values) != 3 {
		t.Fatalf("date round trip rescanned JSON: content=%d keys=%d", state.contentReads, len(store.values))
	}
	// A new match on the same date replaces that date's memo, not a new key.
	read("2026-10-17", "new-a")
	if state.contentReads != 4 || len(store.values) != 3 {
		t.Fatal("same-date source growth created an unbounded memo key")
	}
}

func TestDrawObservationsRevisionMemoExplicitRefreshDetectsPreservedTimestamp(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	store := newDrawResultMemoryStore()
	db := drawRevisionMemoDB(t, state)
	first, err := drawObservationMemoizedTableRevision(db, store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	state.data.content++ // Emulate a manual SQL update without updated_at.
	second, err := drawObservationMemoizedTableRevision(db, store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, true)
	if err != nil || first == second || state.contentReads != 2 {
		t.Fatalf("explicit refresh did not bypass memo: scans=%d error=%v", state.contentReads, err)
	}
}

func TestDrawObservationsRevisionMemoMissReadsMetadataAndContentFromSameSnapshot(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	state.afterMetadata = func(inTransaction bool) {
		if inTransaction {
			t.Fatal("expected initial fast metadata read outside transaction")
		}
		state.mu.Lock()
		state.data = drawRevisionMemoData{metadata: 2, content: 200}
		state.mu.Unlock()
	}
	store := newDrawResultMemoryStore()
	db := drawRevisionMemoDB(t, state)
	first, err := drawObservationMemoizedTableRevision(db, store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := drawObservationMemoizedTableRevision(db, store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, false)
	if err != nil || first != second || state.contentReads != 1 {
		t.Fatalf("memo was published against a pre-update fingerprint: content=%d error=%v", state.contentReads, err)
	}
}

func TestDrawObservationsRevisionMemoUsesCallerSnapshotDuringCrawlerUpdate(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	state.afterMetadata = func(inTransaction bool) {
		if !inTransaction {
			t.Fatal("caller snapshot was bypassed")
		}
		state.mu.Lock()
		state.data = drawRevisionMemoData{metadata: 2, content: 200}
		state.mu.Unlock()
	}
	store := newDrawResultMemoryStore()
	db := drawRevisionMemoDB(t, state)
	var first string
	err := db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		first, txErr = drawObservationMemoizedTableRevision(tx, store, "database-a", drawRevisionMemoSchema(), "moneys", nil, true, false)
		return txErr
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := drawObservationMemoizedTableRevision(db, store, "database-a", drawRevisionMemoSchema(), "moneys", nil, false, false)
	if err != nil || first == second || state.contentReads != 2 {
		t.Fatalf("crawler update mixed snapshot metadata/content: first=%s second=%s error=%v", first, second, err)
	}
}

func TestDrawObservationsRevisionMemoRedisErrorsAndLegacySchemas(t *testing.T) {
	state := &drawRevisionMemoSQLState{data: drawRevisionMemoData{metadata: 1, content: 100}}
	store := newDrawResultMemoryStore()
	db := drawRevisionMemoDB(t, state)
	schema := drawRevisionMemoSchema()
	store.getError = errors.New("redis read failed")
	if _, err := drawObservationMemoizedTableRevision(db, store, "database-a", schema, "moneys", nil, false, false); err == nil {
		t.Fatal("memo read error hidden")
	}
	store.getError = nil
	store.setError = errors.New("redis write failed")
	if _, err := drawObservationMemoizedTableRevision(db, store, "database-a", schema, "moneys", nil, false, false); err == nil {
		t.Fatal("memo write error hidden")
	}
	store.setError = nil
	delete(schema["moneys"], "updated_at")
	before := state.contentReads
	for i := 0; i < 2; i++ {
		if _, err := drawObservationMemoizedTableRevision(db, store, "database-a", schema, "moneys", nil, false, false); err != nil {
			t.Fatal(err)
		}
	}
	if state.contentReads != before+2 {
		t.Fatal("legacy table without timestamps accepted a metadata-only memo")
	}
}
