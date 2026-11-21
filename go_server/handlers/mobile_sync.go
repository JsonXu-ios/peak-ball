package handlers

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/database"
	"go_server/internal/drawmodeldata"
	"go_server/models"
	"gorm.io/gorm"
)

// The mobile protocol exports every visible match, regardless of day, result,
// lottery ID or model selection. Each revision is immutable for the duration of
// a download. The app commits it only after all requested records verify.
const mobileSyncSchema = 1

type mobileSyncEntry struct {
	MatchID  string `json:"matchId"`
	Revision string `json:"revision"`
}
type mobileSyncManifest struct {
	Schema           int               `json:"schema"`
	Snapshot         string            `json:"snapshot"`
	ModelVersion     string            `json:"modelVersion"`
	GeneratedAt      time.Time         `json:"generatedAt"`
	ScheduleTimezone string            `json:"scheduleTimezone"`
	Total            int               `json:"total"`
	Entries          []mobileSyncEntry `json:"entries"`
}
type mobileSyncRecord struct {
	MatchID  string `json:"matchId"`
	Revision string `json:"revision"`
	// A JSON string preserves the exact bytes covered by SHA-256 on all clients.
	Payload string `json:"payload"`
}
type mobileSyncFlight struct {
	done     chan struct{}
	manifest mobileSyncManifest
	err      error
}
type mobileSyncStore struct {
	mu         sync.Mutex
	root       string
	flight     *mobileSyncFlight
	lastSource string
	last       mobileSyncManifest
	indexes    map[string]map[string]string
	source     func(context.Context) (string, error)
	export     func(context.Context, func(string, []byte) error) (string, error)
}

func newMobileSyncStore(root string) *mobileSyncStore {
	return &mobileSyncStore{root: root, source: mobileSyncSourceRevision, export: mobileSyncExport, indexes: map[string]map[string]string{}}
}

var mobileSyncSnapshots = newMobileSyncStore(mobileSyncRoot())

func mobileSyncRoot() string {
	if value := strings.TrimSpace(os.Getenv("MOBILE_SYNC_DIR")); value != "" {
		return value
	}
	return filepath.Join("data", "mobile-sync")
}
func mobileSyncSHA(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Database bookkeeping is not match evidence. Crawlers may touch updated_at
// without changing a quote/result; exclude storage-only fields so a refresh
// does not force phones to download otherwise identical records. Provider
// capture timestamps and every business-data field remain intact.
func mobileSyncStablePayload(value interface{}) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err = json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	for _, key := range []string{"history", "rawMatch", "rawEuroOdds", "rawHandicapOdds"} {
		var object map[string]json.RawMessage
		if len(root[key]) == 0 || string(root[key]) == "null" {
			continue
		}
		if err = json.Unmarshal(root[key], &object); err != nil {
			return nil, err
		}
		for _, field := range []string{"id", "createdAt", "updatedAt"} {
			delete(object, field)
		}
		root[key], err = json.Marshal(object)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(root)
}
func mobileSyncValidHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && strings.ToLower(value) == value
}

func (s *mobileSyncStore) manifest(ctx context.Context) (mobileSyncManifest, error) {
	s.mu.Lock()
	if current := s.flight; current != nil {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return mobileSyncManifest{}, ctx.Err()
		case <-current.done:
			return current.manifest, current.err
		}
	}
	flight := &mobileSyncFlight{done: make(chan struct{})}
	s.flight = flight
	s.mu.Unlock()
	manifest, err := s.build(ctx)
	s.mu.Lock()
	flight.manifest, flight.err = manifest, err
	s.flight = nil
	close(flight.done)
	s.mu.Unlock()
	return manifest, err
}

func (s *mobileSyncStore) build(ctx context.Context) (mobileSyncManifest, error) {
	version, err := s.source(ctx)
	if err != nil {
		return mobileSyncManifest{}, err
	}
	if version == s.lastSource && s.last.Snapshot != "" {
		if _, err := os.Stat(filepath.Join(s.root, s.last.Snapshot, "manifest.json")); err == nil {
			return s.last, nil
		}
	}
	if err := os.MkdirAll(s.root, 0700); err != nil {
		return mobileSyncManifest{}, err
	}
	staging, err := os.MkdirTemp(s.root, "building-")
	if err != nil {
		return mobileSyncManifest{}, err
	}
	defer os.RemoveAll(staging)
	entries := []mobileSyncEntry{}
	seen := map[string]bool{}
	modelVersion, err := s.export(ctx, func(id string, payload []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if id == "" || seen[id] {
			return fmt.Errorf("重复或空比赛 ID")
		}
		seen[id] = true
		if !json.Valid(payload) {
			return fmt.Errorf("比赛数据不是有效 JSON")
		}
		revision := mobileSyncSHA(payload)
		if err := os.WriteFile(filepath.Join(staging, mobileSyncSHA([]byte(id))+".json"), payload, 0600); err != nil {
			return err
		}
		entries = append(entries, mobileSyncEntry{MatchID: id, Revision: revision})
		return nil
	})
	if err != nil {
		return mobileSyncManifest{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].MatchID < entries[j].MatchID })
	manifest := mobileSyncManifest{Schema: mobileSyncSchema, ModelVersion: modelVersion, GeneratedAt: time.Now().UTC(), ScheduleTimezone: "Asia/Shanghai", Total: len(entries), Entries: entries}
	manifest.Snapshot = drawObservationDigest([]interface{}{mobileSyncSchema, modelVersion, entries})
	data, err := json.Marshal(manifest)
	if err != nil {
		return manifest, err
	}
	if err = os.WriteFile(filepath.Join(staging, "manifest.json"), data, 0600); err != nil {
		return manifest, err
	}
	target := filepath.Join(s.root, manifest.Snapshot)
	if _, err = os.Stat(target); os.IsNotExist(err) {
		err = os.Rename(staging, target)
	}
	if err != nil {
		return manifest, err
	}
	// Keep the prior two complete revisions for clients already downloading.
	s.prune(manifest.Snapshot)
	s.lastSource, s.last = version, manifest
	return manifest, nil
}

func (s *mobileSyncStore) prune(current string) {
	entries, _ := os.ReadDir(s.root)
	type saved struct {
		name string
		time time.Time
	}
	all := []saved{}
	for _, entry := range entries {
		if !entry.IsDir() || !mobileSyncValidHash(entry.Name()) || entry.Name() == current {
			continue
		}
		info, err := entry.Info()
		if err == nil {
			all = append(all, saved{entry.Name(), info.ModTime()})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].time.After(all[j].time) })
	for i := 2; i < len(all); i++ {
		// Large first downloads remain valid even if several phones refresh.
		if time.Since(all[i].time) > 2*time.Hour {
			_ = os.RemoveAll(filepath.Join(s.root, all[i].name))
			s.mu.Lock()
			delete(s.indexes, all[i].name)
			s.mu.Unlock()
		}
	}
}

func (s *mobileSyncStore) batch(snapshot string, ids []string) ([]mobileSyncRecord, error) {
	if !mobileSyncValidHash(snapshot) || len(ids) == 0 || len(ids) > 20 {
		return nil, fmt.Errorf("每批应有 1–20 场比赛及有效快照")
	}
	s.mu.Lock()
	known := s.indexes[snapshot]
	s.mu.Unlock()
	if known == nil {
		data, err := os.ReadFile(filepath.Join(s.root, snapshot, "manifest.json"))
		if err != nil {
			return nil, err
		}
		var manifest mobileSyncManifest
		if err = json.Unmarshal(data, &manifest); err != nil {
			return nil, err
		}
		known = map[string]string{}
		for _, e := range manifest.Entries {
			known[e.MatchID] = e.Revision
		}
		s.mu.Lock()
		s.indexes[snapshot] = known
		s.mu.Unlock()
	}
	result := make([]mobileSyncRecord, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 128 || seen[id] || known[id] == "" {
			return nil, fmt.Errorf("比赛不属于此快照，或 ID 重复")
		}
		seen[id] = true
		payload, err := os.ReadFile(filepath.Join(s.root, snapshot, mobileSyncSHA([]byte(id))+".json"))
		if err != nil {
			return nil, err
		}
		if mobileSyncSHA(payload) != known[id] {
			return nil, fmt.Errorf("服务端快照校验失败")
		}
		result = append(result, mobileSyncRecord{MatchID: id, Revision: known[id], Payload: string(payload)})
	}
	return result, nil
}

func GetMobileSyncManifest(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	manifest, err := mobileSyncSnapshots.manifest(ctx)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "生成离线数据失败，请稍后重试", "detail": err.Error()})
		return
	}
	c.Header("Cache-Control", "no-store")
	mobileSyncJSON(c, manifest)
}
func GetMobileSyncBatch(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var request struct {
		Snapshot string   `json:"snapshot"`
		IDs      []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || !mobileSyncValidHash(request.Snapshot) || len(request.IDs) == 0 || len(request.IDs) > 20 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "快照及比赛 ID 参数无效"})
		return
	}
	rows, err := mobileSyncSnapshots.batch(request.Snapshot, request.IDs)
	if err != nil {
		status := http.StatusConflict
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusGone
		}
		c.JSON(status, gin.H{"error": "快照已变化或不可用，请重新同步"})
		return
	}
	c.Header("Cache-Control", "no-store")
	mobileSyncJSON(c, gin.H{"snapshot": request.Snapshot, "rows": rows})
}

func mobileSyncJSON(c *gin.Context, payload interface{}) {
	c.Header("Vary", "Accept-Encoding")
	compress := false
	for _, encoding := range strings.Split(c.GetHeader("Accept-Encoding"), ",") {
		parts := strings.Split(strings.TrimSpace(strings.ToLower(encoding)), ";")
		if parts[0] != "gzip" {
			continue
		}
		allowed := true
		for _, parameter := range parts[1:] {
			value := strings.TrimSpace(parameter)
			if value == "q=0" || value == "q=0.0" || value == "q=0.00" || value == "q=0.000" {
				allowed = false
			}
		}
		compress = compress || allowed
	}
	if !compress {
		c.JSON(http.StatusOK, payload)
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Encoding", "gzip")
	c.Status(http.StatusOK)
	writer := gzip.NewWriter(c.Writer)
	_ = json.NewEncoder(writer).Encode(payload)
	_ = writer.Close()
}

// Include every exported column, including rankings and source JSON. This also
// detects a corrected result or a manual edit that kept updated_at unchanged.
func mobileSyncSourceRevision(ctx context.Context) (string, error) {
	if database.DB == nil {
		return "", fmt.Errorf("数据库尚未连接")
	}
	db := database.DB.WithContext(ctx)
	schema, err := drawObservationReadSchema(db)
	if err != nil {
		return "", err
	}
	parts := []interface{}{mobileSyncSchema, drawObservationResultVersion, drawmodeldata.Version(), drawmodeldata.ArtifactSHA256(), drawmodeldata.Threshold(), drawTotalsServingVersion()}
	for _, table := range []string{"moneys", "money", "history_moneys", "odds_moneys", "pankou_moneys"} {
		if !schema[table]["match_id"] {
			continue
		}
		columns := []string{}
		for column := range schema[table] {
			columns = append(columns, column)
		}
		sort.Strings(columns)
		expressions := []string{}
		for _, column := range columns {
			expressions = append(expressions, "COALESCE(CAST(`"+column+"` AS CHAR),'NULL')")
		}
		rowHash := "CRC32(CONCAT_WS(CHAR(31)," + strings.Join(expressions, ",") + "))"
		var value struct {
			N        uint64
			Checksum uint64
			Total    string
		}
		if err := db.Table(table).Select("COUNT(*) AS n, COALESCE(BIT_XOR(" + rowHash + "),0) AS checksum, COALESCE(CAST(SUM(" + rowHash + ") AS CHAR),'0') AS total").Scan(&value).Error; err != nil {
			return "", err
		}
		parts = append(parts, table, columns, value)
	}
	return drawObservationDigest(parts), nil
}

func mobileSyncExport(ctx context.Context, emit func(string, []byte) error) (string, error) {
	if database.DB == nil {
		return "", fmt.Errorf("数据库尚未连接")
	}
	modelVersion := drawmodeldata.Version()
	modelDigest := drawmodeldata.ArtifactSHA256()
	err := database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		loaders := drawObservationSQLLoadersForDB(func() *gorm.DB { return tx })
		loaders.research = false
		cache := newDrawObservationCache(loaders, 24*time.Hour, time.Now)
		core, err := cache.get(false)
		if err != nil {
			return err
		}
		query := drawObservationQuery{mode: "today", scope: "all", start: "0001-01-01", end: "9999-12-31", page: 1, size: 100, matchStatus: "all", selection: "all"}
		for {
			response, err := cache.read(query)
			if err != nil {
				return err
			}
			ids := make([]string, len(response.Items))
			for i, row := range response.Items {
				ids[i] = row.MatchID
			}
			if len(ids) == 0 {
				break
			}
			main := map[string]models.Money{}
			history := map[string]models.HistoryMoney{}
			odds := map[string]models.OddsMoney{}
			pankou := map[string]models.PankouMoney{}
			for _, table := range []string{"money", "moneys"} {
				if core.base.schema[table]["match_id"] {
					var rows []models.Money
					if err := tx.Table(table).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
						return err
					}
					for _, row := range rows {
						main[row.MatchId] = row
					}
				}
			}
			if core.base.schema["history_moneys"]["match_id"] {
				var rows []models.HistoryMoney
				if err := tx.Table("history_moneys").Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
					return err
				}
				for _, row := range rows {
					history[row.MatchId] = row
				}
			}
			if core.base.schema["odds_moneys"]["match_id"] {
				var rows []models.OddsMoney
				if err := tx.Table("odds_moneys").Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
					return err
				}
				for _, row := range rows {
					odds[row.MatchId] = row
				}
			}
			if core.base.schema["pankou_moneys"]["match_id"] {
				var rows []models.PankouMoney
				if err := tx.Table("pankou_moneys").Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
					return err
				}
				for _, row := range rows {
					pankou[row.MatchId] = row
				}
			}
			for _, row := range response.Items {
				row.FrontendAnalysis = drawObservationFrontendWithoutSimulatedHandicap(row.FrontendAnalysis)
				payload := struct {
					drawObservationItem
					History         *models.HistoryMoney `json:"history"`
					RawMatch        models.Money         `json:"rawMatch"`
					RawEuroOdds     *models.OddsMoney    `json:"rawEuroOdds"`
					RawHandicapOdds *models.PankouMoney  `json:"rawHandicapOdds"`
				}{drawObservationItem: row, RawMatch: main[row.MatchID]}
				if value, ok := history[row.MatchID]; ok {
					payload.History = &value
				}
				if value, ok := odds[row.MatchID]; ok {
					payload.RawEuroOdds = &value
				}
				if value, ok := pankou[row.MatchID]; ok {
					payload.RawHandicapOdds = &value
				}
				data, err := mobileSyncStablePayload(payload)
				if err != nil {
					return err
				}
				if err = emit(row.MatchID, data); err != nil {
					return err
				}
			}
			if query.page*query.size >= response.Total {
				break
			}
			query.page++
		}
		if drawmodeldata.Version() != modelVersion || drawmodeldata.ArtifactSHA256() != modelDigest {
			return fmt.Errorf("模型正在更新，请重新同步")
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return modelVersion, err
}
