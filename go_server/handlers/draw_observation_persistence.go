package handlers

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go_server/database"
	"gorm.io/gorm"
)

// Results have no TTL. Source revisions, calculation version and explicit
// refresh control regeneration; application restarts do not discard them.
const drawObservationResultVersion = "v10-weighted-feature-inputs"

type drawObservationResultStore interface {
	get(context.Context, string) ([]byte, error)
	set(context.Context, string, []byte) error
}
type drawObservationRedisStore struct {
	client *redis.Client
	err    error
}

func newDrawObservationRedisStore() *drawObservationRedisStore {
	address := strings.TrimSpace(os.Getenv("DRAW_OBSERVATION_REDIS_URL"))
	if address == "" {
		address = "redis://127.0.0.1:6379/0"
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		return &drawObservationRedisStore{err: fmt.Errorf("平局结果 Redis 地址配置无效")}
	}
	options.Protocol = 2
	options.DialTimeout = time.Second
	options.ReadTimeout = 2 * time.Second
	options.WriteTimeout = 2 * time.Second
	options.MaxRetries = 1
	options.PoolSize = 4
	return &drawObservationRedisStore{client: redis.NewClient(options)}
}
func (store *drawObservationRedisStore) get(ctx context.Context, key string) ([]byte, error) {
	if store.err != nil {
		return nil, store.err
	}
	data, err := store.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取平局持久结果失败，请检查 Redis 连接: %w", err)
	}
	return data, nil
}
func (store *drawObservationRedisStore) set(ctx context.Context, key string, data []byte) error {
	if store.err != nil {
		return store.err
	}
	if err := store.client.Set(ctx, key, data, 0).Err(); err != nil {
		return fmt.Errorf("保存平局持久结果失败: %w", err)
	}
	return nil
}

type drawObservationSourceVersion struct{ Namespace, Core, Day string }
type drawObservationStoredBase struct {
	Version string
	Targets []drawObservationTarget
	History []drawHistoricalGame
	Schema  drawObservationSchema
}
type drawObservationStoredDay struct {
	Version          string                 `json:"version"`
	Date             string                 `json:"date"`
	GeneratedAt      time.Time              `json:"generatedAt"`
	AllSummary       drawObservationSummary `json:"allSummary"`
	SportterySummary drawObservationSummary `json:"sportterySummary"`
	DrawIDs          map[string]bool        `json:"drawIds"`
	Items            []drawObservationItem  `json:"items"`
	Notes            []string               `json:"notes"`
}
type drawObservationResultFlight struct {
	done chan struct{}
	day  *drawObservationStoredDay
	err  error
}
type drawObservationResultReader struct {
	store   drawObservationResultStore
	source  func(drawObservationQuery) (drawObservationSourceVersion, error)
	loaders drawObservationLoaders
	// SQL readers build from one MVCC snapshot. Injectable loaders without a
	// snapshot retain the bounded optimistic validation used by unit callers.
	snapshot   func(drawObservationQuery, func(drawObservationSourceVersion, drawObservationLoaders) (*drawObservationStoredDay, error)) (*drawObservationStoredDay, error)
	mu         sync.Mutex
	generation string
	generator  *drawObservationCache
	flights    map[string]*drawObservationResultFlight
}

func newDrawObservationResultReader(store drawObservationResultStore, source func(drawObservationQuery) (drawObservationSourceVersion, error), loaders drawObservationLoaders) *drawObservationResultReader {
	return &drawObservationResultReader{store: store, source: source, loaders: loaders, flights: map[string]*drawObservationResultFlight{}}
}

var drawObservationResults = newDrawObservationSQLResultReader(newDrawObservationRedisStore())

func newDrawObservationSQLResultReader(store drawObservationResultStore) *drawObservationResultReader {
	reader := newDrawObservationResultReader(store, newDrawObservationSourceLoaderWithMemo(func() *gorm.DB { return database.DB }, store, false), drawObservationSQLLoaders())
	reader.snapshot = func(q drawObservationQuery, build func(drawObservationSourceVersion, drawObservationLoaders) (*drawObservationStoredDay, error)) (day *drawObservationStoredDay, err error) {
		if database.DB == nil {
			return nil, fmt.Errorf("数据库尚未连接")
		}
		err = database.DB.Transaction(func(tx *gorm.DB) error {
			resolve := func() *gorm.DB { return tx }
			source, sourceErr := newDrawObservationSourceLoaderWithMemo(resolve, store, true)(q)
			if sourceErr != nil {
				return sourceErr
			}
			day, sourceErr = build(source, drawObservationSQLLoadersForDB(resolve))
			return sourceErr
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		return day, err
	}
	return reader
}

func drawObservationPack(encode func(io.Writer) error) ([]byte, error) {
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}
	if err = encode(writer); err != nil {
		_ = writer.Close()
		return nil, err
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func drawObservationUnpack(data []byte, decode func(io.Reader) error) error {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer reader.Close()
	const limit = 128 << 20
	unpacked, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if len(unpacked) > limit {
		return fmt.Errorf("平局持久结果超过大小限制")
	}
	return decode(bytes.NewReader(unpacked))
}

func (reader *drawObservationResultReader) generationCache(source drawObservationSourceVersion, force bool) *drawObservationCache {
	return reader.generationCacheWithLoaders(source, force, reader.loaders)
}

func (reader *drawObservationResultReader) generationCacheWithLoaders(source drawObservationSourceVersion, force bool, originalLoaders drawObservationLoaders) *drawObservationCache {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	generation := source.Namespace + ":" + source.Core
	if !force && reader.generator != nil && reader.generation == generation {
		return reader.generator
	}
	loaders := originalLoaders
	original := loaders.base
	loaders.base = func() (drawObservationBase, error) {
		key := source.Namespace + ":base"
		if !force {
			data, err := reader.store.get(context.Background(), key)
			if err != nil {
				return drawObservationBase{}, err
			}
			if len(data) > 0 {
				var saved drawObservationStoredBase
				if drawObservationUnpack(data, func(r io.Reader) error { return gob.NewDecoder(r).Decode(&saved) }) == nil && saved.Version == source.Core {
					return drawObservationBase{targets: saved.Targets, history: saved.History, schema: saved.Schema}, nil
				}
			}
		}
		base, err := original()
		if err != nil {
			return base, err
		}
		saved := drawObservationStoredBase{Version: source.Core, Targets: base.targets, History: base.history, Schema: base.schema}
		data, err := drawObservationPack(func(w io.Writer) error { return gob.NewEncoder(w).Encode(saved) })
		if err != nil {
			return base, err
		}
		if err = reader.store.set(context.Background(), key, data); err != nil {
			return base, err
		}
		return base, nil
	}
	// A changed source revision, not a wall-clock TTL, replaces this generation.
	reader.generator = newDrawObservationCache(loaders, 100*365*24*time.Hour, time.Now)
	reader.generation = generation
	return reader.generator
}

func (reader *drawObservationResultReader) buildDay(q drawObservationQuery, source drawObservationSourceVersion, forceBase bool) (*drawObservationStoredDay, error) {
	return reader.buildDayWithLoaders(q, source, forceBase, reader.loaders)
}

func (reader *drawObservationResultReader) buildDayWithLoaders(q drawObservationQuery, source drawObservationSourceVersion, forceBase bool, loaders drawObservationLoaders) (*drawObservationStoredDay, error) {
	generator := reader.generationCacheWithLoaders(source, forceBase, loaders)
	shared, err := generator.get(false)
	if err != nil {
		// A failed transaction-bound base loader cannot outlive its transaction.
		reader.mu.Lock()
		if reader.generator == generator {
			reader.generator, reader.generation = nil, ""
		}
		reader.mu.Unlock()
		return nil, err
	}
	// Reuse only immutable history/index data. A quote-only revision must not
	// reuse an earlier day's market or frontend caches from this process.
	generator = drawObservationFreshQueryCache(shared, loaders)
	core := generator.core
	baseQuery := drawObservationQuery{mode: "today", scope: "all", date: q.date, page: 1, size: 200}
	day, err := core.day(baseQuery, generator.loaders)
	if err != nil {
		return nil, err
	}
	saved := &drawObservationStoredDay{Version: source.Day, Date: q.date, GeneratedAt: time.Now().UTC(), AllSummary: core.summary, SportterySummary: core.sportterySummary, DrawIDs: map[string]bool{}, Items: []drawObservationItem{}}
	for _, target := range day.targets {
		if h, a, ok := drawRegulationResult(target.Match); ok && h == a {
			saved.DrawIDs[target.Match.MatchId] = true
		}
	}
	for {
		page, err := generator.read(baseQuery)
		if err != nil {
			return nil, err
		}
		saved.Items = append(saved.Items, page.Items...)
		saved.Notes = page.Notes
		if len(saved.Items) >= page.Total {
			break
		}
		baseQuery.page++
	}
	return saved, nil
}

func drawObservationFreshQueryCache(shared *drawObservationCore, loaders drawObservationLoaders) *drawObservationCache {
	core := &drawObservationCore{base: shared.base, index: shared.index, summary: shared.summary, sportterySummary: shared.sportterySummary, authority: shared.authority, windows: map[string]drawObservationWindows{}, days: map[string]*drawObservationDayFlight{}, lineCache: map[string]drawObservationQuotesBundle{}, euroCache: map[string]drawObservationEuroOdds{}, frontendCache: map[string]drawObservationFrontendEnrichment{}}
	cache := newDrawObservationCache(loaders, 100*365*24*time.Hour, time.Now)
	cache.core = core
	cache.expires = time.Now().Add(100 * 365 * 24 * time.Hour)
	return cache
}

func (reader *drawObservationResultReader) read(q drawObservationQuery) (drawObservationResponse, error) {
	return reader.readWithParsedBase(q, false)
}

func (reader *drawObservationResultReader) readWithParsedBase(q drawObservationQuery, reuseParsedBase bool) (drawObservationResponse, error) {
	q = normalizeDrawObservationQuery(q)
	source, err := reader.source(q)
	if err != nil {
		return drawObservationResponse{}, err
	}
	// Retain old range clients; the UI uses one source schedule date only.
	if q.date == "" {
		shared, err := reader.generationCache(source, q.refresh).get(false)
		if err != nil {
			return drawObservationResponse{}, err
		}
		return drawObservationFreshQueryCache(shared, reader.loaders).read(q)
	}
	key := source.Namespace + ":day:" + q.date
	if !q.refresh {
		saved, err := reader.storedDay(key, q.date, source.Day)
		if err != nil {
			return drawObservationResponse{}, err
		}
		if saved != nil {
			return saved.filter(q), nil
		}
	}
	flightKey := key + ":" + source.Day
	reader.mu.Lock()
	if pending := reader.flights[flightKey]; pending != nil {
		reader.mu.Unlock()
		<-pending.done
		if pending.err != nil {
			return drawObservationResponse{}, pending.err
		}
		return pending.day.filter(q), nil
	}
	pending := &drawObservationResultFlight{done: make(chan struct{})}
	reader.flights[flightKey] = pending
	reader.mu.Unlock()
	// A prior leader may have published between this request's initial GET
	// and its flight claim. Recheck before doing any expensive generation.
	if !q.refresh {
		pending.day, pending.err = reader.storedDay(key, q.date, source.Day)
	}
	if pending.day == nil && pending.err == nil && reader.snapshot != nil {
		pending.day, pending.err = reader.snapshot(q, func(version drawObservationSourceVersion, loaders drawObservationLoaders) (*drawObservationStoredDay, error) {
			// A writer may have committed after the fast version check. Resolve
			// the day and all of its inputs under the transaction's own version.
			source = version
			key = source.Namespace + ":day:" + q.date
			if !q.refresh {
				saved, err := reader.storedDay(key, q.date, source.Day)
				if err != nil || saved != nil {
					return saved, err
				}
			}
			return reader.buildDayWithLoaders(q, source, q.refresh && !reuseParsedBase, loaders)
		})
		// Publish only after the read-only transaction successfully commits.
		if pending.err == nil {
			var packed []byte
			packed, pending.err = drawObservationPack(func(w io.Writer) error { return json.NewEncoder(w).Encode(pending.day) })
			if pending.err == nil {
				pending.err = reader.store.set(context.Background(), key, packed)
			}
		}
	}
	// Optimistic validation prevents publishing a snapshot assembled while the
	// crawler changed its source data. No source tables are written or locked.
	for attempt := 0; pending.day == nil && pending.err == nil && attempt < 2; attempt++ {
		pending.day, pending.err = reader.buildDay(q, source, q.refresh && !reuseParsedBase)
		if pending.err != nil {
			break
		}
		after, checkErr := reader.source(q)
		if checkErr != nil {
			pending.err = checkErr
			break
		}
		if after != source {
			source = after
			pending.day = nil
			if attempt == 1 {
				pending.err = fmt.Errorf("比赛数据正在更新，请稍后重新查询")
			}
			continue
		}
		var packed []byte
		packed, pending.err = drawObservationPack(func(w io.Writer) error { return json.NewEncoder(w).Encode(pending.day) })
		if pending.err == nil {
			pending.err = reader.store.set(context.Background(), key, packed)
		}
		break
	}
	reader.mu.Lock()
	delete(reader.flights, flightKey)
	close(pending.done)
	reader.mu.Unlock()
	if pending.err != nil {
		return drawObservationResponse{}, pending.err
	}
	return pending.day.filter(q), nil
}

func (reader *drawObservationResultReader) storedDay(key, date, version string) (*drawObservationStoredDay, error) {
	data, err := reader.store.get(context.Background(), key)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	var saved drawObservationStoredDay
	if drawObservationUnpack(data, func(r io.Reader) error { return json.NewDecoder(r).Decode(&saved) }) != nil || saved.Version != version || saved.Date != date {
		return nil, nil
	}
	return &saved, nil
}

func (saved *drawObservationStoredDay) filter(q drawObservationQuery) drawObservationResponse {
	page, size := q.page, q.size
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	generated := saved.GeneratedAt
	response := drawObservationResponse{ScheduleTimezone: drawSourceLocation.String(), Mode: q.mode, Scope: q.scope, Date: saved.Date, Page: page, PageSize: size, Summary: saved.AllSummary, Leagues: []string{}, Items: []drawObservationItem{}, Notes: append([]string{}, saved.Notes...), DataVersion: saved.Version, GeneratedAt: &generated}
	if q.scope == "sporttery" {
		response.Summary = saved.SportterySummary
	}
	response.Summary.AsianMatches, response.Summary.TotalMatches = 0, 0
	leagues := map[string]bool{}
	selected := []drawObservationItem{}
	search := strings.ToLower(strings.TrimSpace(q.search))
	for _, row := range saved.Items {
		if q.scope == "sporttery" && strings.TrimSpace(row.JingcaiID) == "" || q.mode == "history" && !saved.DrawIDs[row.MatchID] {
			continue
		}
		if row.AsianHandicap.Source != "missing" {
			response.Summary.AsianMatches++
		}
		if row.GoalsLine.Source != "missing" {
			response.Summary.TotalMatches++
		}
		leagues[row.League] = true
		if q.league != "" && q.league != "all" && q.league != row.League {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(row.Home+" "+row.Guest+" "+row.MatchID), search) {
			continue
		}
		if !q.homeDraws.matches(row.HomeRecent) || !q.awayDraws.matches(row.AwayRecent) || !q.h2hDraws.matches(row.HeadToHead) {
			continue
		}
		selected = append(selected, row)
	}
	for league := range leagues {
		response.Leagues = append(response.Leagues, league)
	}
	sort.Strings(response.Leagues)
	response.Total = len(selected)
	offset := len(selected)
	if page-1 <= len(selected)/size {
		offset = (page - 1) * size
	}
	end := min(offset+size, len(selected))
	response.Items = append(response.Items, selected[offset:end]...)
	return response
}

// MaterializeDrawObservationDates is used by the explicit offline builder. It
// performs SELECTs and writes only this feature's namespaced Redis results.
func MaterializeDrawObservationDates(dates []string, force bool, progress func(string, int, time.Duration)) error {
	for index, date := range dates {
		if _, err := drawDate(date); err != nil {
			return fmt.Errorf("赛程日期格式应为 YYYY-MM-DD")
		}
		start := time.Now()
		response, err := drawObservationResults.readWithParsedBase(drawObservationQuery{mode: "today", scope: "all", date: date, page: 1, size: 200, refresh: force}, force && index > 0)
		if err != nil {
			return err
		}
		if progress != nil {
			progress(date, response.Total, time.Since(start))
		}
	}
	return nil
}
