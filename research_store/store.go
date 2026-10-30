package researchstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var databaseIdentifier = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)

type Store struct {
	db   *sql.DB
	name string
}

// IsolatedDSN parses rather than string-replaces DSNs. It rejects the business
// database even when the caller accidentally supplies its name as the target.
func IsolatedDSN(baseDSN, databaseName string) (string, error) {
	if databaseName == "" {
		databaseName = DefaultDatabase
	}
	if !databaseIdentifier.MatchString(databaseName) {
		return "", errors.New("invalid research database identifier")
	}
	cfg, err := mysql.ParseDSN(baseDSN)
	if err != nil {
		return "", errors.New("invalid database connection configuration")
	}
	if strings.EqualFold(databaseName, cfg.DBName) || strings.EqualFold(databaseName, "football_data") || strings.EqualFold(databaseName, "mysql") || strings.EqualFold(databaseName, "information_schema") || strings.EqualFold(databaseName, "performance_schema") || strings.EqualFold(databaseName, "sys") {
		return "", errors.New("research database must be separate from business and system databases")
	}
	cfg.DBName = databaseName
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false
	cfg.ClientFoundRows = false
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["charset"] = "utf8mb4"
	cfg.Params["time_zone"] = "'+00:00'"
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 30 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 30 * time.Second
	}
	return cfg.FormatDSN(), nil
}

// Open connects only. It never creates a database, migrates a table or changes
// the business schema. InitSchema is an explicit maintenance command.
func Open(ctx context.Context, baseDSN, databaseName string) (*Store, error) {
	dsn, err := IsolatedDSN(baseDSN, databaseName)
	if err != nil {
		return nil, err
	}
	if databaseName == "" {
		databaseName = DefaultDatabase
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open research database: %w", err)
	}
	db.SetMaxOpenConns(6)
	db.SetMaxIdleConns(3)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect research database %s: %w", databaseName, err)
	}
	return &Store{db: db, name: databaseName}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }
func (s *Store) Name() string { return s.name }

func InitSchema(ctx context.Context, baseDSN, databaseName string) error {
	dsn, err := IsolatedDSN(baseDSN, databaseName)
	if err != nil {
		return err
	}
	if databaseName == "" {
		databaseName = DefaultDatabase
	}
	cfg, _ := mysql.ParseDSN(dsn)
	cfg.DBName = ""
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return fmt.Errorf("open research schema connection: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+databaseName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return fmt.Errorf("create isolated research database: %w", err)
	}
	store, err := Open(ctx, baseDSN, databaseName)
	if err != nil {
		return err
	}
	defer store.Close()
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var acquired int
	lockName := "peakball_research_schema_" + databaseName
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,10)", lockName).Scan(&acquired); err != nil || acquired != 1 {
		return errors.New("cannot acquire isolated research schema lock")
	}
	defer conn.ExecContext(context.Background(), "SELECT RELEASE_LOCK(?)", lockName)
	files, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	bootstrap, err := migrationFiles.ReadFile("migrations/001_initial.sql")
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, strings.SplitN(string(bootstrap), ";", 2)[0]); err != nil {
		return fmt.Errorf("bootstrap research migrations: %w", err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		payload, err := migrationFiles.ReadFile("migrations/" + file.Name())
		if err != nil {
			return err
		}
		version := strings.SplitN(file.Name(), "_", 2)[0]
		checksum := HashBytes(payload)
		var existing string
		err = conn.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version=?", version).Scan(&existing)
		if err == nil {
			if existing != checksum {
				return fmt.Errorf("research migration %s checksum changed; add a new migration", version)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, statement := range strings.Split(string(payload), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply research migration %s: %w", version, err)
			}
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations(version,checksum,applied_at) VALUES(?,?,?)", version, checksum, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func HashBytes(raw []byte) string { value := sha256.Sum256(raw); return hex.EncodeToString(value[:]) }

func canonicalJSON(raw json.RawMessage, fallback string) ([]byte, error) {
	if len(raw) == 0 {
		return []byte(fallback), nil
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, errors.New("invalid structured JSON")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid trailing structured JSON")
	}
	return json.Marshal(value)
}

func hashValue(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return HashBytes(raw), nil
}
func utc(t time.Time) time.Time { return t.UTC().Truncate(time.Microsecond) }
func optionalTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return utc(*t)
}
func validProvenance(p string) bool { return p == "live" || p == "reconstructed" }
func finitePointer(v *float64) bool { return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0)) }

// SanitizeSourceURL keeps source provenance without persisting credentials or
// bearer tokens that can be attached to authenticated collection endpoints.
func SanitizeSourceURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "invalid-source-url"
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
		if strings.Contains(normalized, "token") || strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || normalized == "key" || normalized == "apikey" || normalized == "authorization" || normalized == "signature" || normalized == "auth" {
			query.Set(key, "[redacted]")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (s *Store) RecordIngest(ctx context.Context, event IngestEvent) (int64, error) {
	if event.Source == "" || event.Kind == "" || event.ObservedAt.IsZero() || !validProvenance(event.Provenance) {
		return 0, errors.New("ingest source, kind, observed time and explicit provenance required")
	}
	event.ObservedAt = utc(event.ObservedAt)
	if event.ParserVersion == "" {
		event.ParserVersion = "raw-v1"
	}
	event.RequestURL = SanitizeSourceURL(event.RequestURL)
	hash, err := hashValue(struct {
		Source, Kind, MatchID, PayloadHash, Provenance, Parser string
		At                                                     time.Time
	}{event.Source, event.Kind, event.MatchID, HashBytes(event.Payload), event.Provenance, event.ParserVersion, event.ObservedAt})
	if err != nil {
		return 0, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO ingest_events(event_hash,payload_hash,source,kind,match_id,observed_at,source_at,provenance,parser_version,request_url,http_status,payload) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, hash, HashBytes(event.Payload), event.Source, event.Kind, event.MatchID, event.ObservedAt, optionalTime(event.SourceAt), event.Provenance, event.ParserVersion, event.RequestURL, event.HTTPStatus, []byte(event.Payload))
	if err != nil {
		return 0, fmt.Errorf("save raw ingest event: %w", err)
	}
	return result.LastInsertId()
}

// RecordLocalSnapshot is content-deduplicated archival of a local database view,
// not a record of a new HTTP observation. Only the two named local sources may
// use it. It returns the first observed time, preserving the earliest evidence.
// RecordIngest deliberately keeps time-based identity for real source fetches.
func (s *Store) RecordLocalSnapshot(ctx context.Context, event IngestEvent) (int64, time.Time, error) {
	if event.Source != "local_business_snapshot" && event.Source != "local_feature_input" {
		return 0, time.Time{}, errors.New("content deduplication is restricted to explicit local snapshot sources")
	}
	if event.Kind == "" || event.ObservedAt.IsZero() || !validProvenance(event.Provenance) {
		return 0, time.Time{}, errors.New("local snapshot kind, observation time and provenance required")
	}
	event.ObservedAt = utc(event.ObservedAt)
	if event.ParserVersion == "" {
		event.ParserVersion = "raw-v1"
	}
	payloadHash := HashBytes(event.Payload)
	var id int64
	var firstObservedAt time.Time
	err := s.db.QueryRowContext(ctx, `SELECT id,observed_at FROM ingest_events WHERE source=? AND kind=? AND match_id=? AND payload_hash=? AND provenance=? AND parser_version=? ORDER BY observed_at,id LIMIT 1`, event.Source, event.Kind, event.MatchID, payloadHash, event.Provenance, event.ParserVersion).Scan(&id, &firstObservedAt)
	if err == nil {
		return id, firstObservedAt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, err
	}
	hash, err := localSnapshotHash(event)
	if err != nil {
		return 0, time.Time{}, err
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO ingest_events(event_hash,payload_hash,source,kind,match_id,observed_at,source_at,provenance,parser_version,request_url,http_status,payload) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, hash, payloadHash, event.Source, event.Kind, event.MatchID, event.ObservedAt, optionalTime(event.SourceAt), event.Provenance, event.ParserVersion, SanitizeSourceURL(event.RequestURL), event.HTTPStatus, []byte(event.Payload))
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("save local source snapshot: %w", err)
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, time.Time{}, err
	}
	// Also handles concurrent first callers: the winning archive's original
	// observation time is returned rather than the losing caller's newer time.
	if err = s.db.QueryRowContext(ctx, `SELECT observed_at FROM ingest_events WHERE id=?`, id).Scan(&firstObservedAt); err != nil {
		return 0, time.Time{}, err
	}
	return id, firstObservedAt, nil
}

func localSnapshotHash(event IngestEvent) (string, error) {
	if event.ParserVersion == "" {
		event.ParserVersion = "raw-v1"
	}
	return hashValue(struct{ Mode, Source, Kind, MatchID, PayloadHash, Provenance, Parser string }{"local-content-v1", event.Source, event.Kind, event.MatchID, HashBytes(event.Payload), event.Provenance, event.ParserVersion})
}

type statementExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) SaveEntity(ctx context.Context, entity Entity) error {
	return saveEntity(ctx, s.db, entity)
}

func saveEntity(ctx context.Context, executor statementExecutor, entity Entity) error {
	if entity.Key == "" || entity.Kind == "" {
		return errors.New("entity key and kind required")
	}
	meta, err := canonicalJSON(entity.Metadata, "{}")
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO entities(entity_key,entity_type,name,source,external_id,metadata,updated_at) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE name=VALUES(name),metadata=VALUES(metadata),updated_at=VALUES(updated_at)`, entity.Key, entity.Kind, entity.Name, entity.Source, entity.ExternalID, meta, time.Now().UTC())
	return err
}

func (s *Store) SaveAlias(ctx context.Context, alias EntityAlias) error {
	return saveAlias(ctx, s.db, alias)
}

func saveAlias(ctx context.Context, executor statementExecutor, alias EntityAlias) error {
	if alias.EntityKey == "" || alias.Source == "" {
		return errors.New("alias entity and source required")
	}
	hash, err := hashValue(alias)
	if err != nil {
		return err
	}
	_, err = executor.ExecContext(ctx, `INSERT INTO entity_aliases(entity_key,source,external_id,alias,confidence,mapping_hash) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, alias.EntityKey, alias.Source, alias.ExternalID, alias.Alias, alias.Confidence, hash)
	return err
}

func (s *Store) SaveMatch(ctx context.Context, match Match) error {
	return saveMatch(ctx, s.db, match)
}

func saveMatch(ctx context.Context, executor statementExecutor, match Match) error {
	if match.MatchID == "" || match.Kickoff.IsZero() {
		return errors.New("match ID and kickoff required")
	}
	if _, err := time.Parse("2006-01-02", match.ScheduleDate); err != nil {
		return errors.New("source Beijing schedule date must be YYYY-MM-DD")
	}
	_, err := executor.ExecContext(ctx, `INSERT INTO matches(match_id,home_id,away_id,home_name,away_name,league_id,league_name,schedule_date,kickoff,jingcai_id,source_event_id,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE home_id=VALUES(home_id),away_id=VALUES(away_id),home_name=VALUES(home_name),away_name=VALUES(away_name),league_id=VALUES(league_id),league_name=VALUES(league_name),schedule_date=VALUES(schedule_date),kickoff=VALUES(kickoff),jingcai_id=VALUES(jingcai_id),source_event_id=VALUES(source_event_id),updated_at=VALUES(updated_at)`, match.MatchID, match.HomeID, match.AwayID, match.Home, match.Away, match.LeagueID, match.League, match.ScheduleDate, utc(match.Kickoff), match.JingcaiID, match.SourceEventID, time.Now().UTC())
	return err
}

// SaveFixture writes the standard identity rows and match as one unit, avoiding
// one durable commit per identity while keeping the single-row APIs identical.
func (s *Store) SaveFixture(ctx context.Context, entities []Entity, aliases []EntityAlias, match Match) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, entity := range entities {
		if err := saveEntity(ctx, tx, entity); err != nil {
			return err
		}
	}
	for _, alias := range aliases {
		if err := saveAlias(ctx, tx, alias); err != nil {
			return err
		}
	}
	if err := saveMatch(ctx, tx, match); err != nil {
		return err
	}
	return tx.Commit()
}

func prepareQuote(quote MarketQuote) (MarketQuote, string, error) {
	if quote.MatchID == "" || quote.Company == "" || quote.Market == "" || quote.Phase == "" || quote.ObservedAt.IsZero() || !validProvenance(quote.Provenance) {
		return quote, "", errors.New("quote identity, time, phase and provenance required")
	}
	for _, value := range []*float64{quote.Home, quote.Draw, quote.Away, quote.Line, quote.HomeWater, quote.AwayWater} {
		if !finitePointer(value) {
			return quote, "", errors.New("nonfinite quote value")
		}
	}
	if quote.Market == "euro" {
		for _, value := range []*float64{quote.Home, quote.Draw, quote.Away} {
			if value != nil && *value <= 0 {
				return quote, "", errors.New("euro quote prices must be positive; missing must remain NULL")
			}
		}
	}
	raw, err := canonicalJSON(quote.Raw, "{}")
	if err != nil {
		return quote, "", err
	}
	quote.Raw = raw
	quote.ObservedAt = utc(quote.ObservedAt)
	hash, err := hashValue(quote)
	return quote, hash, err
}

func (s *Store) SaveQuote(ctx context.Context, quote MarketQuote) error {
	return s.SaveQuotes(ctx, []MarketQuote{quote})
}

// SaveQuotes atomically validates and saves a batch with one durable commit.
// Every row retains exactly the same phase/source/hash/null rules as SaveQuote.
func (s *Store) SaveQuotes(ctx context.Context, quotes []MarketQuote) error {
	if len(quotes) == 0 {
		return nil
	}
	prepared := make([]MarketQuote, len(quotes))
	hashes := make([]string, len(quotes))
	for i, quote := range quotes {
		var err error
		prepared[i], hashes[i], err = prepareQuote(quote)
		if err != nil {
			return fmt.Errorf("validate quote %d: %w", i, err)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `INSERT INTO market_quotes(quote_hash,match_id,company,market_type,phase,source_event_id,source_at,observed_at,provenance,home_value,draw_value,away_value,line_value,home_water,away_water,raw_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for i, quote := range prepared {
		if _, err := statement.ExecContext(ctx, hashes[i], quote.MatchID, quote.Company, quote.Market, quote.Phase, quote.SourceEventID, optionalTime(quote.SourceAt), quote.ObservedAt, quote.Provenance, quote.Home, quote.Draw, quote.Away, quote.Line, quote.HomeWater, quote.AwayWater, quote.Raw); err != nil {
			return fmt.Errorf("save quote %d: %w", i, err)
		}
	}
	return tx.Commit()
}

func (s *Store) SaveResult(ctx context.Context, result MatchResult) (int64, error) {
	if result.MatchID == "" || result.ObservedAt.IsZero() || !validProvenance(result.Provenance) {
		return 0, errors.New("result match, observed time and provenance required")
	}
	for _, v := range []*int{result.Home, result.Away, result.ExtraHome, result.ExtraAway, result.PenaltyHome, result.PenaltyAway} {
		if v != nil && *v < 0 {
			return 0, errors.New("negative score cannot be a match result")
		}
	}
	if result.RegulationConfirmed && (result.Home == nil || result.Away == nil) {
		return 0, errors.New("confirmed regulation result needs both scores")
	}
	result.ObservedAt = utc(result.ObservedAt)
	// Each call represents an observed revision. A -> B -> A must retain the
	// second A as a new revision; only replaying the same event is idempotent.
	hash, err := resultRevisionHash(result)
	if err != nil {
		return 0, err
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO match_results(result_hash,match_id,home_score,away_score,extra_home,extra_away,penalty_home,penalty_away,regulation_confirmed,status,available_at,observed_at,source_event_id,provenance) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, hash, result.MatchID, result.Home, result.Away, result.ExtraHome, result.ExtraAway, result.PenaltyHome, result.PenaltyAway, result.RegulationConfirmed, result.Status, optionalTime(result.AvailableAt), result.ObservedAt, result.SourceEventID, result.Provenance)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func resultRevisionHash(result MatchResult) (string, error) {
	return hashValue(struct {
		MatchID                                                    string
		Home, Away, ExtraHome, ExtraAway, PenaltyHome, PenaltyAway *int
		Confirmed                                                  bool
		Status, Provenance                                         string
		ObservedAt                                                 time.Time
		SourceEventID                                              *int64
	}{result.MatchID, result.Home, result.Away, result.ExtraHome, result.ExtraAway, result.PenaltyHome, result.PenaltyAway, result.RegulationConfirmed, result.Status, result.Provenance, utc(result.ObservedAt), result.SourceEventID})
}

func (s *Store) RegisterFeatures(ctx context.Context, definitions []FeatureDefinition) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, definition := range definitions {
		if definition.Version == "" || definition.Name == "" {
			return errors.New("feature version and name required")
		}
		meta, err := canonicalJSON(definition.Metadata, "{}")
		if err != nil {
			return err
		}
		definition.Metadata = meta
		hash, err := hashValue(definition)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO feature_definitions(feature_version,feature_name,value_type,unit,description,formula,origin,missing_policy,code_hash,metadata,definition_hash) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, definition.Version, definition.Name, definition.Type, definition.Unit, definition.Description, definition.Formula, definition.Origin, definition.MissingPolicy, definition.CodeHash, meta, hash)
		if err != nil {
			return err
		}
		var stored string
		if err = tx.QueryRowContext(ctx, `SELECT definition_hash FROM feature_definitions WHERE feature_version=? AND feature_name=?`, definition.Version, definition.Name).Scan(&stored); err != nil {
			return err
		}
		if stored != hash {
			return fmt.Errorf("feature definition %s changed under immutable version %s", definition.Name, definition.Version)
		}
	}
	return tx.Commit()
}

func prepareSnapshot(snapshot FeatureSnapshot) (FeatureSnapshot, string, error) {
	if snapshot.MatchID == "" || snapshot.AsOf.IsZero() || snapshot.Kickoff.IsZero() || snapshot.FeatureVersion == "" || snapshot.DataRevision == "" || !validProvenance(snapshot.Provenance) {
		return snapshot, "", errors.New("snapshot identity, cutoff, kickoff, versions and provenance required")
	}
	snapshot.AsOf = utc(snapshot.AsOf)
	snapshot.Kickoff = utc(snapshot.Kickoff)
	if snapshot.Provenance == "live" && (snapshot.KnownBy == nil || snapshot.KnownBy.IsZero() || snapshot.KnownBy.After(snapshot.AsOf) || !snapshot.AsOf.Before(snapshot.Kickoff)) {
		return snapshot, "", errors.New("live snapshot needs known-by <= as-of < kickoff")
	}
	var err error
	if snapshot.Values, err = canonicalJSON(snapshot.Values, "{}"); err != nil {
		return snapshot, "", err
	}
	if snapshot.Missing, err = canonicalJSON(snapshot.Missing, "[]"); err != nil {
		return snapshot, "", err
	}
	if snapshot.QualityFlags, err = canonicalJSON(snapshot.QualityFlags, "[]"); err != nil {
		return snapshot, "", err
	}
	snapshot.SourceEventIDs = append([]int64(nil), snapshot.SourceEventIDs...)
	sort.Slice(snapshot.SourceEventIDs, func(i, j int) bool { return snapshot.SourceEventIDs[i] < snapshot.SourceEventIDs[j] })
	// The revision is stable across repeated observations of unchanged input.
	// as_of/known_by and evidence are the first-seen immutable metadata only.
	hash, err := hashValue(struct {
		MatchID, FeatureVersion, DataRevision, Provenance, Quality string
		Values, Missing, Flags                                     json.RawMessage
	}{snapshot.MatchID, snapshot.FeatureVersion, snapshot.DataRevision, snapshot.Provenance, snapshot.Quality, snapshot.Values, snapshot.Missing, snapshot.QualityFlags})
	return snapshot, hash, err
}

func (s *Store) SaveSnapshot(ctx context.Context, snapshot FeatureSnapshot) (int64, error) {
	snapshot, hash, err := prepareSnapshot(snapshot)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var oldID int64
	var oldHash string
	err = tx.QueryRowContext(ctx, `SELECT id,snapshot_hash FROM feature_snapshots WHERE match_id=? AND feature_version=? AND data_revision=?`, snapshot.MatchID, snapshot.FeatureVersion, snapshot.DataRevision).Scan(&oldID, &oldHash)
	if err == nil {
		if oldHash != hash {
			return 0, errors.New("snapshot revision reused for different inputs")
		}
		return oldID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if snapshot.Provenance == "live" {
		if len(snapshot.SourceEventIDs) == 0 {
			return 0, errors.New("live snapshot needs source evidence")
		}
		for _, eventID := range snapshot.SourceEventIDs {
			var observed time.Time
			var provenance string
			if err := tx.QueryRowContext(ctx, `SELECT observed_at,provenance FROM ingest_events WHERE id=?`, eventID).Scan(&observed, &provenance); err != nil {
				return 0, err
			}
			if provenance != "live" || observed.After(snapshot.AsOf) || observed.After(*snapshot.KnownBy) {
				return 0, errors.New("source evidence was not known at live snapshot cutoff")
			}
		}
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO feature_snapshots(snapshot_hash,match_id,as_of,kickoff,feature_version,data_revision,provenance,known_by,quality,values_json,missing_json,quality_flags) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, hash, snapshot.MatchID, snapshot.AsOf, snapshot.Kickoff, snapshot.FeatureVersion, snapshot.DataRevision, snapshot.Provenance, optionalTime(snapshot.KnownBy), snapshot.Quality, snapshot.Values, snapshot.Missing, snapshot.QualityFlags)
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT snapshot_hash FROM feature_snapshots WHERE id=?`, id).Scan(&oldHash); err != nil {
		return 0, err
	}
	if oldHash != hash {
		return 0, errors.New("snapshot revision reused for different inputs")
	}
	inserted, err := r.RowsAffected()
	if err != nil {
		return 0, err
	}
	// A concurrent repeat may have won the same natural key after our SELECT.
	// Keep its first source evidence rather than growing an immutable snapshot.
	if inserted == 1 {
		for _, eventID := range snapshot.SourceEventIDs {
			if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO snapshot_sources(snapshot_id,event_id) VALUES(?,?)`, id, eventID); err != nil {
				return 0, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) SavePrediction(ctx context.Context, prediction Prediction) (int64, error) {
	if prediction.SnapshotID <= 0 || prediction.MatchID == "" || prediction.ModelVersion == "" || prediction.RuleVersion == "" || prediction.PredictedAt.IsZero() {
		return 0, errors.New("prediction snapshot, identity, versions and time required")
	}
	if !finitePointer(prediction.Score) || !finitePointer(prediction.Threshold) {
		return 0, errors.New("nonfinite model score")
	}
	var err error
	if prediction.Scorelines, err = canonicalJSON(prediction.Scorelines, "[]"); err != nil {
		return 0, err
	}
	if prediction.Payload, err = canonicalJSON(prediction.Payload, "{}"); err != nil {
		return 0, err
	}
	// Repeated sync must retain the first time, never change a model decision.
	copy := prediction
	copy.PredictedAt = time.Time{}
	hash, err := hashValue(copy)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var matchID string
	if err = tx.QueryRowContext(ctx, `SELECT match_id FROM feature_snapshots WHERE id=?`, prediction.SnapshotID).Scan(&matchID); err != nil {
		return 0, err
	}
	if matchID != prediction.MatchID {
		return 0, errors.New("prediction match differs from input snapshot")
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO predictions(prediction_hash,snapshot_id,match_id,model_version,rule_version,predicted_at,state,score,threshold_value,secondary_selected,enhanced,score_method,scorelines,payload) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, hash, prediction.SnapshotID, prediction.MatchID, prediction.ModelVersion, prediction.RuleVersion, utc(prediction.PredictedAt), prediction.State, prediction.Score, prediction.Threshold, prediction.Secondary, prediction.Enhanced, prediction.ScoreMethod, prediction.Scorelines, prediction.Payload)
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	var oldHash string
	if err = tx.QueryRowContext(ctx, `SELECT prediction_hash FROM predictions WHERE id=?`, id).Scan(&oldHash); err != nil {
		return 0, err
	}
	if oldHash != hash {
		return 0, errors.New("prediction changed for immutable model/rule/score version")
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) SaveSettlement(ctx context.Context, settlement Settlement) error {
	if settlement.PredictionID <= 0 || settlement.ResultID <= 0 || settlement.SettledAt.IsZero() {
		return errors.New("settlement prediction, result and time required")
	}
	var predictionMatch, resultMatch string
	var confirmed bool
	var h, a sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT p.match_id,r.match_id,r.regulation_confirmed,r.home_score,r.away_score FROM predictions p JOIN match_results r ON r.id=? WHERE p.id=?`, settlement.ResultID, settlement.PredictionID).Scan(&predictionMatch, &resultMatch, &confirmed, &h, &a); err != nil {
		return err
	}
	if predictionMatch != resultMatch || !confirmed || !h.Valid || !a.Valid {
		return errors.New("settlement needs confirmed regulation result of same match")
	}
	if settlement.IsDraw != (h.Int64 == a.Int64) {
		return errors.New("settlement draw flag disagrees with regulation result")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO settlements(prediction_id,result_id,is_draw,scoreline_hit,settled_at) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, settlement.PredictionID, settlement.ResultID, settlement.IsDraw, settlement.ScorelineHit, utc(settlement.SettledAt))
	return err
}

func (s *Store) StartAudit(ctx context.Context, audit AuditRun) (int64, error) {
	if audit.StartedAt.IsZero() {
		audit.StartedAt = time.Now().UTC()
	}
	meta, err := canonicalJSON(audit.Metadata, "{}")
	if err != nil {
		return 0, err
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO audit_runs(kind,version,started_at,status,metadata) VALUES(?,?,?,'running',?)`, audit.Kind, audit.Version, utc(audit.StartedAt), meta)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) SaveIssues(ctx context.Context, issues []AuditIssue) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, issue := range issues {
		if issue.RunID <= 0 || issue.Code == "" {
			return errors.New("audit run and issue code required")
		}
		if issue.Severity != "info" && issue.Severity != "warning" && issue.Severity != "error" {
			return errors.New("unknown audit severity")
		}
		detail, err := canonicalJSON(issue.Detail, "{}")
		if err != nil {
			return err
		}
		issue.Detail = detail
		hash, err := hashValue(issue)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_issues(issue_hash,run_id,match_id,source_event_id,severity,code,field_name,message,detail) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, hash, issue.RunID, issue.MatchID, issue.SourceEventID, issue.Severity, issue.Code, issue.Field, issue.Message, detail); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FinishAudit(ctx context.Context, runID int64, status string, metadata json.RawMessage) error {
	if status != "completed" && status != "failed" {
		return errors.New("audit completion status must be completed or failed")
	}
	meta, err := canonicalJSON(metadata, "{}")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE audit_runs SET finished_at=?,status=?,metadata=? WHERE id=? AND status='running'`, time.Now().UTC(), status, meta, runID)
	return err
}

func (s *Store) SaveDataset(ctx context.Context, dataset Dataset) (int64, error) {
	if dataset.Name == "" || dataset.Version == "" || dataset.FeatureVersion == "" {
		return 0, errors.New("dataset name and versions required")
	}
	meta, err := canonicalJSON(dataset.Manifest, "{}")
	if err != nil {
		return 0, err
	}
	if dataset.CreatedAt.IsZero() {
		dataset.CreatedAt = time.Now().UTC()
	}
	hash := HashBytes(meta)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, `INSERT INTO datasets(name,version,feature_version,manifest_hash,manifest,created_at) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, dataset.Name, dataset.Version, dataset.FeatureVersion, hash, meta, utc(dataset.CreatedAt))
	if err != nil {
		return 0, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return 0, err
	}
	var stored, version string
	if err = tx.QueryRowContext(ctx, `SELECT manifest_hash,feature_version FROM datasets WHERE id=?`, id).Scan(&stored, &version); err != nil {
		return 0, err
	}
	if stored != hash || version != dataset.FeatureVersion {
		return 0, errors.New("dataset manifest changed under immutable version")
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) AddDatasetMembers(ctx context.Context, members []DatasetMember) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, member := range members {
		if member.DatasetID <= 0 || member.SnapshotID <= 0 {
			return errors.New("dataset and snapshot IDs required")
		}
		switch member.Split {
		case "train", "calibration", "test", "research", "prospective":
		default:
			return errors.New("unknown dataset split")
		}
		var datasetVersion, snapshotVersion, snapshotMatch string
		var resultMatch sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT d.feature_version,s.feature_version,s.match_id,r.match_id FROM datasets d JOIN feature_snapshots s ON s.id=? LEFT JOIN match_results r ON r.id=? WHERE d.id=?`, member.SnapshotID, member.ResultID, member.DatasetID).Scan(&datasetVersion, &snapshotVersion, &snapshotMatch, &resultMatch); err != nil {
			return err
		}
		if datasetVersion != snapshotVersion {
			return errors.New("dataset feature version differs from snapshot")
		}
		if member.ResultID != nil && (!resultMatch.Valid || resultMatch.String != snapshotMatch) {
			return errors.New("dataset label belongs to a different match")
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO dataset_members(dataset_id,snapshot_id,result_id,split_name) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE dataset_id=dataset_id`, member.DatasetID, member.SnapshotID, member.ResultID, member.Split); err != nil {
			return err
		}
		var result sql.NullInt64
		var split string
		if err = tx.QueryRowContext(ctx, `SELECT result_id,split_name FROM dataset_members WHERE dataset_id=? AND snapshot_id=?`, member.DatasetID, member.SnapshotID).Scan(&result, &split); err != nil {
			return err
		}
		if split != member.Split || (member.ResultID == nil && result.Valid) || (member.ResultID != nil && (!result.Valid || result.Int64 != *member.ResultID)) {
			return errors.New("dataset member changed under immutable version")
		}
	}
	return tx.Commit()
}

func (s *Store) SaveModelRun(ctx context.Context, run ModelRun) (int64, error) {
	if run.Version == "" || run.DatasetID <= 0 || run.ArtifactHash == "" {
		return 0, errors.New("model version, dataset and artifact hash required")
	}
	config, err := canonicalJSON(run.Config, "{}")
	if err != nil {
		return 0, err
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO model_runs(version,dataset_id,artifact_hash,artifact_uri,config,status,started_at) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, run.Version, run.DatasetID, run.ArtifactHash, run.ArtifactURI, config, run.Status, utc(run.StartedAt))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (s *Store) SaveEvaluation(ctx context.Context, evaluation Evaluation) (int64, error) {
	if evaluation.Version == "" || evaluation.Scope == "" {
		return 0, errors.New("evaluation version and scope required")
	}
	metrics, err := canonicalJSON(evaluation.Metrics, "{}")
	if err != nil {
		return 0, err
	}
	evaluation.Metrics = metrics
	if evaluation.CreatedAt.IsZero() {
		evaluation.CreatedAt = time.Now().UTC()
	}
	copy := evaluation
	copy.CreatedAt = time.Time{}
	hash, err := hashValue(copy)
	if err != nil {
		return 0, err
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO evaluations(evaluation_hash,model_run_id,dataset_id,version,scope,metrics,created_at) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)`, hash, evaluation.ModelRunID, evaluation.DatasetID, evaluation.Version, evaluation.Scope, metrics, utc(evaluation.CreatedAt))
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}
