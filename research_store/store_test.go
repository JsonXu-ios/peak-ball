package researchstore

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestResearchConnectionCannotTargetBusinessOrSystemSchema(t *testing.T) {
	base := "user:secret@tcp(127.0.0.1:3306)/football_data?parseTime=true"
	for _, name := range []string{"football_data", "FOOTBALL_DATA", "mysql", "sys", "information_schema", "performance_schema", "bad`name", "a;DROP DATABASE x", "a/b"} {
		if _, err := IsolatedDSN(base, name); err == nil {
			t.Fatalf("unsafe schema accepted: %q", name)
		}
	}
	if _, err := IsolatedDSN("user:secret@tcp(localhost:3306)/custom_business", "custom_business"); err == nil {
		t.Fatal("custom business schema accepted as target")
	}
	dsn, err := IsolatedDSN(base, "football_research")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBName != "football_research" || cfg.Addr != "127.0.0.1:3306" || cfg.Loc != time.UTC || !cfg.ParseTime || cfg.MultiStatements {
		t.Fatalf("connection isolation changed unintended settings: %#v", cfg)
	}
	if cfg.Params["time_zone"] != "'+00:00'" {
		t.Fatal("research timestamps must use UTC session time")
	}
}

func TestCanonicalJSONPreservesNullZeroAndRejectsTrailingData(t *testing.T) {
	first, err := canonicalJSON(json.RawMessage(`{"missing":null,"zero":0,"negative":-0.28}`), "{}")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(first, &got); err != nil {
		t.Fatal(err)
	}
	if got["missing"] != nil || got["zero"].(float64) != 0 || got["negative"].(float64) != -0.28 {
		t.Fatalf("lost null/zero/sign distinctions: %s", first)
	}
	for _, raw := range []string{`{} {}`, `null true`, `{"a":1} garbage`, `[1,`} {
		if _, err := canonicalJSON(json.RawMessage(raw), "{}"); err == nil {
			t.Fatalf("accepted invalid structured JSON: %s", raw)
		}
	}
}

func validSnapshot() FeatureSnapshot {
	cutoff := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	known := cutoff.Add(-time.Minute)
	return FeatureSnapshot{MatchID: "m1", AsOf: cutoff, Kickoff: cutoff.Add(time.Hour), FeatureVersion: "v1", DataRevision: "raw1", Provenance: "live", KnownBy: &known, Quality: "verified", Values: json.RawMessage(`{"x":0,"y":null}`), Missing: json.RawMessage(`["y"]`), QualityFlags: json.RawMessage(`[]`), SourceEventIDs: []int64{2, 1}}
}

func TestLiveSnapshotEnforcesKnownTimeAndKickoffBoundary(t *testing.T) {
	base := validSnapshot()
	if _, _, err := prepareSnapshot(base); err != nil {
		t.Fatal(err)
	}
	missingKnown := base
	missingKnown.KnownBy = nil
	lateKnown := base
	next := base.AsOf.Add(time.Microsecond)
	lateKnown.KnownBy = &next
	afterKickoff := base
	afterKickoff.AsOf = afterKickoff.Kickoff
	for _, invalid := range []FeatureSnapshot{missingKnown, lateKnown, afterKickoff} {
		if _, _, err := prepareSnapshot(invalid); err == nil {
			t.Fatal("accepted unverifiable live snapshot")
		}
	}
	old := missingKnown
	old.Provenance = "reconstructed"
	old.AsOf = old.Kickoff.Add(time.Hour)
	if _, _, err := prepareSnapshot(old); err != nil {
		t.Fatalf("old data must remain usable as explicitly reconstructed evidence: %v", err)
	}
}

func TestSnapshotRevisionIsStableButInputChangesAreDetectable(t *testing.T) {
	first := validSnapshot()
	_, a, err := prepareSnapshot(first)
	if err != nil {
		t.Fatal(err)
	}
	repeated := first
	repeated.AsOf = repeated.AsOf.Add(time.Minute)
	known := repeated.AsOf
	repeated.KnownBy = &known
	repeated.SourceEventIDs = []int64{30, 40}
	_, b, err := prepareSnapshot(repeated)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("unchanged input created a different immutable input revision")
	}
	changed := first
	changed.Values = json.RawMessage(`{"x":1,"y":null}`)
	_, c, err := prepareSnapshot(changed)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("changed input not detectable under same revision")
	}
	if first.SourceEventIDs[0] != 2 {
		t.Fatal("prepare mutated caller source list")
	}
	canonical := first
	canonical.Values = json.RawMessage(`{"y":null,"x":0}`)
	_, d, err := prepareSnapshot(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if a != d {
		t.Fatal("JSON key order must not change content identity")
	}
}

func TestMigrationHasCatalogAndColumnDocumentation(t *testing.T) {
	content, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(content)
	for table := range TableCatalog {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table+" (") {
			t.Fatalf("catalog table missing DDL: %s", table)
		}
	}
	for _, line := range strings.Split(sql, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "CREATE ") || strings.HasPrefix(trim, ")") || strings.HasPrefix(trim, "PRIMARY ") || strings.HasPrefix(trim, "UNIQUE ") || strings.HasPrefix(trim, "KEY ") || strings.HasPrefix(trim, "CONSTRAINT ") {
			continue
		}
		if !strings.Contains(line, "COMMENT '") {
			t.Fatalf("undocumented schema column: %s", line)
		}
	}
	if strings.Contains(strings.ToUpper(sql), "DROP TABLE") || strings.Contains(sql, "football_data.") {
		t.Fatal("migration must never delete or mutate business tables")
	}
}

func TestSourceURLDoesNotPersistAuthentication(t *testing.T) {
	clean := SanitizeSourceURL("https://name:password@example.org/i/match?token=abc&api_key=def&matchId=123")
	if strings.Contains(clean, "password") || strings.Contains(clean, "abc") || strings.Contains(clean, "def") {
		t.Fatalf("source URL retained credentials: %s", clean)
	}
	if !strings.Contains(clean, "matchId=123") {
		t.Fatal("source entity provenance lost")
	}
}

func TestLocalSnapshotIdentityPreservesContentAcrossSyncTimes(t *testing.T) {
	first := IngestEvent{Source: "local_business_snapshot", Kind: "odds", MatchID: "m1", ObservedAt: time.Now().UTC(), Provenance: "reconstructed", Payload: json.RawMessage(`{"raw":0}`)}
	a, err := localSnapshotHash(first)
	if err != nil {
		t.Fatal(err)
	}
	repeat := first
	repeat.ObservedAt = repeat.ObservedAt.Add(time.Hour)
	repeat.ParserVersion = "raw-v1"
	b, err := localSnapshotHash(repeat)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("unchanged local content would duplicate on each sync")
	}
	changed := first
	changed.Payload = json.RawMessage(`{"raw":1}`)
	c, err := localSnapshotHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("changed source input would be lost")
	}
	provenance := first
	provenance.Provenance = "live"
	d, err := localSnapshotHash(provenance)
	if err != nil {
		t.Fatal(err)
	}
	if a == d {
		t.Fatal("reconstructed and real-time local evidence must not merge")
	}
}

func TestResultRevisionReturningToEarlierScoreRemainsLatestEvidence(t *testing.T) {
	home, awayA, awayB := 1, 1, 2
	eventA, eventB, eventAgain := int64(10), int64(11), int64(12)
	first := MatchResult{MatchID: "m1", Home: &home, Away: &awayA, RegulationConfirmed: true, Status: "finished", ObservedAt: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC), SourceEventID: &eventA, Provenance: "live"}
	changed := first
	changed.Away = &awayB
	changed.ObservedAt = first.ObservedAt.Add(time.Minute)
	changed.SourceEventID = &eventB
	reverted := first
	reverted.ObservedAt = first.ObservedAt.Add(2 * time.Minute)
	reverted.SourceEventID = &eventAgain
	a, err := resultRevisionHash(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resultRevisionHash(changed)
	if err != nil {
		t.Fatal(err)
	}
	c, err := resultRevisionHash(reverted)
	if err != nil {
		t.Fatal(err)
	}
	if a == b || b == c || a == c {
		t.Fatal("A -> B -> A score corrections must keep three distinct observed revisions")
	}
	replay, err := resultRevisionHash(reverted)
	if err != nil {
		t.Fatal(err)
	}
	if c != replay {
		t.Fatal("replaying the same result event must remain idempotent")
	}
	sameTimeNewEvent := first
	sameTimeNewEvent.SourceEventID = &eventAgain
	d, err := resultRevisionHash(sameTimeNewEvent)
	if err != nil {
		t.Fatal(err)
	}
	if a == d {
		t.Fatal("distinct source events must retain distinct result evidence")
	}
}

func TestBatchQuoteValidationPreservesZeroAndRejectsBeforeTransaction(t *testing.T) {
	zero := 0.0
	quote := MarketQuote{MatchID: "m1", Company: "bet365", Market: "asian", Phase: "latest", ObservedAt: time.Now().UTC(), Provenance: "reconstructed", Line: &zero, Raw: json.RawMessage(`{"line":0,"missing":null}`)}
	prepared, _, err := prepareQuote(quote)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Line == nil || *prepared.Line != 0 || prepared.Home != nil {
		t.Fatal("quote batch lost real zero or original NULL")
	}
	invalid := quote
	bad := math.NaN()
	invalid.HomeWater = &bad
	// A nil connection intentionally proves the whole batch is validated before
	// any DB operation can begin. One bad quote cannot leave partial writes.
	store := &Store{}
	if err := store.SaveQuotes(context.Background(), []MarketQuote{quote, invalid}); err == nil {
		t.Fatal("invalid batch accepted")
	}
	if err := store.SaveQuotes(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	invalid = quote
	invalid.Market = "euro"
	invalid.Home = &zero
	if _, _, err := prepareQuote(invalid); err == nil {
		t.Fatal("real euro price zero must not silently become missing")
	}
}
