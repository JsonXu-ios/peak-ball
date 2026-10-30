package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"peakball/researchstore"
)

const crawlerRawParserVersion = "vipc-crawler-raw-v1"

type researchEventRecorder interface {
	RecordIngest(context.Context, researchstore.IngestEvent) (int64, error)
}

// A research write must never replace a source payload or stop the existing
// business collector. Failures are retried and reported separately.
type crawlerResearchCapture struct {
	recorder researchEventRecorder
	close    func() error
	connect  func() (researchEventRecorder, func() error, error)
	logger   func(string, ...interface{})
	outbox   string
	now      func() time.Time
	mu       sync.Mutex
	saved    uint64
	failed   uint64
	queued   uint64
	replayed uint64
	retryAt  time.Time
}

var researchCapture *crawlerResearchCapture

func initResearchCapture(baseDSN string) *crawlerResearchCapture {
	capture := &crawlerResearchCapture{logger: log.Printf, now: time.Now, outbox: strings.TrimSpace(os.Getenv("RESEARCH_INGEST_OUTBOX"))}
	if configured := strings.TrimSpace(os.Getenv("RESEARCH_BASE_DSN")); configured != "" {
		baseDSN = configured
	}
	if capture.outbox == "" {
		capture.outbox = filepath.Join("data", "research-ingest-pending")
	}
	databaseName := strings.TrimSpace(os.Getenv("RESEARCH_DB_NAME"))
	if databaseName == "" {
		databaseName = researchstore.DefaultDatabase
	}
	// Open never creates a database or changes a schema. The explicit research
	// initialization command owns that operation.
	capture.connect = func() (researchEventRecorder, func() error, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		store, err := researchstore.Open(ctx, baseDSN, databaseName)
		if err != nil {
			return nil, nil, err
		}
		return store, store.Close, nil
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		recorder, closeStore, err := capture.connect()
		if err == nil {
			capture.recorder, capture.close = recorder, closeStore
			capture.logger("Research capture connected to independent database %s; business tables are unchanged.", databaseName)
			if err := capture.replayPending(10); err != nil {
				capture.retryAt = capture.clock().Add(30 * time.Second)
				capture.logf("Research pending evidence replay failed: %v; files remain durable and will retry after the limited interval.", err)
			}
			return capture
		}
		lastErr = err
	}
	capture.retryAt = capture.clock().Add(30 * time.Second)
	capture.logger("RESEARCH CAPTURE UNAVAILABLE: %v; source collection continues with a persistent evidence outbox. Connection retries are limited to one interval per 30 seconds.", lastErr)
	return capture
}

func (capture *crawlerResearchCapture) finish() {
	if capture == nil {
		return
	}
	capture.mu.Lock()
	saved, failed, queued, replayed := capture.saved, capture.failed, capture.queued, capture.replayed
	capture.mu.Unlock()
	capture.logf("Research capture summary: saved=%d direct_write_failures=%d queued=%d replayed=%d. Pending files retain their original receipt timestamps.", saved, failed, queued, replayed)
	if capture.close != nil {
		if err := capture.close(); err != nil {
			capture.logf("Error closing research capture database: %v", err)
		}
	}
}

// researchRequestIdentity stores only a public source endpoint, without query
// parameters, user info or fragments. Canonical match identity supplied by the
// caller takes priority over endpoint IDs (lr can use a lottery number).
func researchRequestIdentity(rawURL, canonicalMatchID, explicitKind string) (requestURL, matchID, kind string) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", strings.TrimSpace(canonicalMatchID), "unknown"
	}
	requestURL = parsed.Scheme + "://" + parsed.Host + parsed.EscapedPath()
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	kind, matchID = "unknown", strings.TrimSpace(canonicalMatchID)
	for index := range parts {
		if parts[index] == "live" && index+1 < len(parts) && parts[index+1] == "football" {
			kind = "match_schedule"
			break
		}
		if parts[index] != "match" || index+2 >= len(parts) {
			continue
		}
		if parts[index+1] == "football" {
			if matchID == "" {
				matchID = parts[index+2]
			}
			if index+3 < len(parts) {
				switch parts[index+3] {
				case "history":
					kind = "history"
				case "odds":
					if index+4 < len(parts) {
						switch parts[index+4] {
						case "euro":
							kind = "euro_odds"
						case "pankou":
							kind = "line_odds"
						}
					}
				}
			}
		} else if parts[index+1] == "jczq" && index+3 < len(parts) {
			switch parts[index+2] {
			case "sporttery":
				kind = "sporttery_odds"
				if matchID == "" {
					matchID = parts[index+3]
				}
			case "lr":
				kind = "sporttery_trade"
				// A lottery ID is not necessarily a canonical match ID. A
				// caller without a canonical mapping leaves it unassigned.
			}
		}
		break
	}
	if strings.TrimSpace(explicitKind) != "" {
		kind = explicitKind
	}
	return requestURL, matchID, kind
}

func makeResearchEvent(rawURL, canonicalMatchID, kind string, body []byte, observedAt time.Time, status int) researchstore.IngestEvent {
	requestURL, matchID, kind := researchRequestIdentity(rawURL, canonicalMatchID, kind)
	return researchstore.IngestEvent{
		Source:        "vipc",
		Kind:          kind,
		MatchID:       matchID,
		ObservedAt:    observedAt.UTC(),
		SourceAt:      nil,
		Provenance:    "live",
		Payload:       json.RawMessage(append([]byte(nil), body...)),
		ParserVersion: crawlerRawParserVersion,
		RequestURL:    requestURL,
		HTTPStatus:    status,
	}
}

func (capture *crawlerResearchCapture) record(event researchstore.IngestEvent) error {
	if capture == nil {
		return nil
	}
	capture.reconnectIfDue()
	var lastErr error
	if !capture.retryAt.IsZero() && capture.clock().Before(capture.retryAt) {
		lastErr = fmt.Errorf("research retry interval has not elapsed")
	} else if capture.recorder == nil {
		lastErr = fmt.Errorf("independent research connection unavailable")
	} else if err := capture.replayPending(5); err != nil {
		lastErr = err
	} else {
		lastErr = capture.write(event)
	}
	capture.mu.Lock()
	if lastErr == nil {
		capture.saved++
	} else {
		capture.failed++
	}
	capture.mu.Unlock()
	if lastErr != nil {
		// Queuing during the waiting interval must not push the next attempt
		// back again, or a busy collector could postpone recovery forever.
		if !capture.clock().Before(capture.retryAt) {
			capture.retryAt = capture.clock().Add(30 * time.Second)
		}
		if err := capture.enqueue(event); err != nil {
			capture.logf("RESEARCH EVIDENCE NOT SAVED kind=%s match=%s received=%s: database=%v outbox=%v; collection continues, but this evidence requires attention.", event.Kind, event.MatchID, event.ObservedAt.Format(time.RFC3339Nano), lastErr, err)
			return fmt.Errorf("research write failed: %w; outbox failed: %v", lastErr, err)
		}
		capture.mu.Lock()
		capture.queued++
		capture.mu.Unlock()
		capture.logf("RESEARCH CAPTURE QUEUED kind=%s match=%s received=%s: %v; original bytes and receipt time are pending durable replay.", event.Kind, event.MatchID, event.ObservedAt.Format(time.RFC3339Nano), lastErr)
	} else {
		capture.retryAt = time.Time{}
	}
	return lastErr
}

func (capture *crawlerResearchCapture) clock() time.Time {
	if capture.now != nil {
		return capture.now().UTC()
	}
	return time.Now().UTC()
}

func (capture *crawlerResearchCapture) logf(format string, values ...interface{}) {
	if capture.logger != nil {
		capture.logger(format, values...)
	}
}

func (capture *crawlerResearchCapture) reconnectIfDue() {
	if capture.recorder != nil || capture.connect == nil || capture.clock().Before(capture.retryAt) {
		return
	}
	capture.retryAt = capture.clock().Add(30 * time.Second)
	recorder, closeStore, err := capture.connect()
	if err != nil {
		capture.logf("Research capture reconnect failed: %v; new receipts remain in the outbox.", err)
		return
	}
	capture.recorder, capture.close = recorder, closeStore
	capture.retryAt = time.Time{}
	capture.logf("Research capture reconnected; replaying pending receipts without changing their timestamps.")
}

func (capture *crawlerResearchCapture) write(event researchstore.IngestEvent) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		// A failed acknowledgement can follow a committed write. Reusing the
		// same event on retries allows the independent store to deduplicate it.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, lastErr = capture.recorder.RecordIngest(ctx, event)
		cancel()
		if lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// Payload is encoded separately as bytes so even an invalid JSON/HTTP error
// page can be recovered exactly. The metadata's RawMessage is always null.
type pendingResearchEvent struct {
	Event researchstore.IngestEvent `json:"event"`
	Body  []byte                    `json:"body"`
}

func (capture *crawlerResearchCapture) enqueue(event researchstore.IngestEvent) error {
	if capture.outbox == "" {
		return fmt.Errorf("outbox directory is not configured")
	}
	entry := pendingResearchEvent{Event: event, Body: append([]byte(nil), event.Payload...)}
	entry.Event.Payload = nil
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(capture.outbox, 0700); err != nil {
		return err
	}
	hash := sha256.Sum256(data)
	name := fmt.Sprintf("%s_%x.json", event.ObservedAt.UTC().Format("20060102T150405.000000000"), hash[:])
	target := filepath.Join(capture.outbox, name)
	if existing, err := os.ReadFile(target); err == nil {
		if string(existing) == string(data) {
			return nil
		}
		return fmt.Errorf("outbox hash collision or existing record was changed")
	} else if !os.IsNotExist(err) {
		return err
	}
	file, err := os.CreateTemp(capture.outbox, ".capture-*.tmp")
	if err != nil {
		return err
	}
	nameTemp := file.Name()
	defer os.Remove(nameTemp)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(nameTemp, target)
}

func (capture *crawlerResearchCapture) replayPending(limit int) error {
	if capture.recorder == nil || capture.outbox == "" || limit <= 0 {
		return nil
	}
	entries, err := os.ReadDir(capture.outbox)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	processed := 0
	for _, entry := range entries {
		if processed >= limit {
			break
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			capture.logf("Research outbox symlink ignored: %s", entry.Name())
			continue
		}
		processed++
		path := filepath.Join(capture.outbox, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var pending pendingResearchEvent
		if err := json.Unmarshal(data, &pending); err != nil {
			capture.quarantinePending(path, entry.Name())
			continue
		}
		if pending.Event.ObservedAt.IsZero() || pending.Event.Provenance != "live" {
			capture.quarantinePending(path, entry.Name())
			continue
		}
		hash := sha256.Sum256(data)
		expectedName := fmt.Sprintf("%s_%x.json", pending.Event.ObservedAt.UTC().Format("20060102T150405.000000000"), hash[:])
		if entry.Name() != expectedName {
			capture.quarantinePending(path, entry.Name())
			continue
		}
		pending.Event.Payload = json.RawMessage(pending.Body)
		if err := capture.write(pending.Event); err != nil {
			return err
		}
		// A failure to delete keeps a replayable copy; the store's event hash
		// makes retrying that copy safe after a crash or partial failure.
		if err := os.Remove(path); err != nil {
			return err
		}
		capture.mu.Lock()
		capture.replayed++
		capture.mu.Unlock()
	}
	return nil
}

func (capture *crawlerResearchCapture) quarantinePending(path, name string) {
	// Preserve invalid evidence without allowing it to occupy every bounded
	// replay batch and indefinitely block the later valid receipts.
	if err := os.Rename(path, path+".invalid"); err != nil {
		capture.logf("Research outbox invalid record could not be marked for audit: %s: %v", name, err)
		return
	}
	capture.logf("Research outbox invalid record retained for audit as %s.invalid", name)
}

func archiveResearchResponse(rawURL, matchID, kind string, body []byte, receivedAt time.Time, status int) {
	if researchCapture != nil {
		_ = researchCapture.record(makeResearchEvent(rawURL, matchID, kind, body, receivedAt, status))
	}
}

func archiveResearchBrowserResponse(rawURL, matchID string, body []byte, receivedAt time.Time) {
	if researchCapture == nil {
		return
	}
	// The browser helper exposes page text, not the original network status.
	// Keep this transport distinct and never invent an HTTP 200 from a
	// successful process exit or pretend this was a raw HTTP response body.
	event := makeResearchEvent(rawURL, matchID, "sporttery_odds", body, receivedAt, 0)
	event.ParserVersion = "vipc-crawler-browser-response-v1"
	_ = researchCapture.record(event)
}
