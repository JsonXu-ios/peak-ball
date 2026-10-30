package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peakball/researchstore"
)

type researchRecorderFunc func(context.Context, researchstore.IngestEvent) (int64, error)

func (record researchRecorderFunc) RecordIngest(ctx context.Context, event researchstore.IngestEvent) (int64, error) {
	return record(ctx, event)
}

func TestResearchRequestIdentityPreservesCanonicalIDAndRemovesSecrets(t *testing.T) {
	tests := []struct {
		url, canonical, expectedKind, expectedID string
	}{
		{"https://vipc.cn/i/live/football/date/today/next", "", "match_schedule", ""},
		{"https://vipc.cn/i/match/football/4981/history", "", "history", "4981"},
		{"https://vipc.cn/i/match/football/4981/odds/euro", "", "euro_odds", "4981"},
		{"https://vipc.cn/i/match/football/4981/odds/pankou", "", "line_odds", "4981"},
		{"https://vipc.cn/i/match/jczq/sporttery/4981", "", "sporttery_odds", "4981"},
		{"https://vipc.cn/i/match/jczq/lr/202610246017", "4981", "sporttery_trade", "4981"},
		{"https://vipc.cn/i/match/jczq/lr/202610246017", "", "sporttery_trade", ""},
	}
	for _, test := range tests {
		_, id, kind := researchRequestIdentity(test.url, test.canonical, "")
		if id != test.expectedID || kind != test.expectedKind {
			t.Fatalf("%s: kind=%s id=%s, want kind=%s id=%s", test.url, kind, id, test.expectedKind, test.expectedID)
		}
	}
	endpoint, _, _ := researchRequestIdentity("https://user:secret@vipc.cn/i/match/football/4981/history?token=secret#fragment", "", "")
	if endpoint != "https://vipc.cn/i/match/football/4981/history" {
		t.Fatalf("source endpoint retained non-public URL material: %q", endpoint)
	}
}

func TestResearchEventPreservesBytesAndActualReceiptTime(t *testing.T) {
	received := time.Date(2026, 10, 29, 18, 25, 44, 123456789, time.FixedZone("Beijing", 8*3600))
	body := []byte("not JSON: \x00原始赔率")
	event := makeResearchEvent("https://vipc.cn/i/match/football/4981/odds/euro", "", "", body, received, 503)
	before := append([]byte(nil), body...)
	body[0] = 'X'
	if !bytes.Equal(event.Payload, before) {
		t.Fatal("captured bytes changed after the caller mutated its response buffer")
	}
	if !event.ObservedAt.Equal(received) || event.ObservedAt.Location() != time.UTC {
		t.Fatalf("receipt instant was changed: %s", event.ObservedAt)
	}
	if event.SourceAt != nil || event.Provenance != "live" || event.HTTPStatus != 503 {
		t.Fatal("raw capture invented a quote timestamp or lost the HTTP status")
	}
}

func TestResearchCaptureRetriesTheSameReceipt(t *testing.T) {
	received := time.Date(2026, 10, 29, 10, 30, 0, 123456000, time.UTC)
	event := makeResearchEvent("https://vipc.cn/i/match/football/4981/history", "", "", []byte(`{"data":[]}`), received, 200)
	var attempts []researchstore.IngestEvent
	capture := &crawlerResearchCapture{outbox: t.TempDir(), recorder: researchRecorderFunc(func(_ context.Context, got researchstore.IngestEvent) (int64, error) {
		attempts = append(attempts, got)
		if len(attempts) == 1 {
			return 0, errors.New("acknowledgement lost")
		}
		return 7, nil
	})}
	if err := capture.record(event); err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 2 || !attempts[0].ObservedAt.Equal(attempts[1].ObservedAt) || !bytes.Equal(attempts[0].Payload, attempts[1].Payload) {
		t.Fatal("retry produced a new receipt rather than reusing the original evidence")
	}
	if capture.saved != 1 || capture.failed != 0 || capture.queued != 0 {
		t.Fatalf("wrong capture counts: %+v", capture)
	}
}

func TestResearchOutboxReplaysOriginalNonJSONReceipt(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 30, 0, 123456000, time.UTC)
	received := now.Add(-time.Second)
	first := makeResearchEvent("https://vipc.cn/i/match/football/4981/history", "", "", []byte("<html>source refused\x00</html>"), received, 403)
	capture := &crawlerResearchCapture{outbox: t.TempDir(), now: func() time.Time { return now }, recorder: researchRecorderFunc(func(_ context.Context, _ researchstore.IngestEvent) (int64, error) {
		return 0, errors.New("database unavailable")
	})}
	if err := capture.record(first); err == nil {
		t.Fatal("outage was not reported")
	}
	files, err := filepath.Glob(filepath.Join(capture.outbox, "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatalf("receipt was not durably queued: %v %v", files, err)
	}
	var restored []researchstore.IngestEvent
	capture.recorder = researchRecorderFunc(func(_ context.Context, event researchstore.IngestEvent) (int64, error) {
		restored = append(restored, event)
		return int64(len(restored)), nil
	})
	now = now.Add(31 * time.Second)
	second := makeResearchEvent("https://vipc.cn/i/match/football/4981/odds/euro", "", "", []byte(`{"latest":true}`), now, 200)
	if err := capture.record(second); err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 || !restored[0].ObservedAt.Equal(received) || !bytes.Equal(restored[0].Payload, first.Payload) || restored[0].HTTPStatus != 403 || restored[0].SourceAt != nil {
		t.Fatalf("replay altered the original evidence: %+v", restored)
	}
	files, _ = filepath.Glob(filepath.Join(capture.outbox, "*.json"))
	if len(files) != 0 || capture.replayed != 1 || capture.queued != 1 || capture.failed != 1 || capture.saved != 1 {
		t.Fatalf("replay did not remove the acknowledged pending file: %+v %v", capture, files)
	}
}

func TestResearchReconnectIntervalDoesNotSlideWithNewReceipts(t *testing.T) {
	now := time.Date(2026, 10, 29, 10, 30, 0, 0, time.UTC)
	start := now
	opens := 0
	var archived []researchstore.IngestEvent
	capture := &crawlerResearchCapture{outbox: t.TempDir(), now: func() time.Time { return now }}
	capture.connect = func() (researchEventRecorder, func() error, error) {
		opens++
		if opens == 1 {
			return nil, nil, errors.New("database unavailable")
		}
		return researchRecorderFunc(func(_ context.Context, event researchstore.IngestEvent) (int64, error) {
			archived = append(archived, event)
			return int64(len(archived)), nil
		}), nil, nil
	}
	for _, offset := range []time.Duration{0, 10 * time.Second, 31 * time.Second} {
		now = start.Add(offset)
		event := makeResearchEvent("https://vipc.cn/i/match/football/4981/history", "", "", []byte(`{}`), now, 200)
		_ = capture.record(event)
		if offset == 10*time.Second && !capture.retryAt.Equal(start.Add(30*time.Second)) {
			t.Fatal("new traffic postponed the connection retry indefinitely")
		}
	}
	if opens != 2 || len(archived) != 3 || capture.replayed != 2 {
		t.Fatalf("connection did not recover and replay all original receipts: opens=%d archived=%d replayed=%d", opens, len(archived), capture.replayed)
	}
}

func TestResearchInvalidOutboxIsRetainedWithoutBlockingValidReceipts(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "000-bad.json")
	if err := os.WriteFile(bad, []byte("broken record"), 0600); err != nil {
		t.Fatal(err)
	}
	var archived []researchstore.IngestEvent
	capture := &crawlerResearchCapture{outbox: dir, recorder: researchRecorderFunc(func(_ context.Context, event researchstore.IngestEvent) (int64, error) {
		archived = append(archived, event)
		return 1, nil
	})}
	event := makeResearchEvent("https://vipc.cn/i/match/football/4981/history", "", "", []byte(`{}`), time.Now(), 200)
	if err := capture.enqueue(event); err != nil {
		t.Fatal(err)
	}
	if err := capture.replayPending(5); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bad + ".invalid"); err != nil || len(archived) != 1 {
		t.Fatal("invalid receipt was deleted or blocked the valid pending receipt")
	}
}

func TestFetchArchivesUnmodifiedResponseBeforeReturning(t *testing.T) {
	const raw = `{"data":{"spf":{"odds":[{"spf3":1.89,"spf1":3.55,"spf0":3.14}]}}}`
	for _, status := range []int{200, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(raw))
			}))
			defer server.Close()
			oldCapture := researchCapture
			defer func() { researchCapture = oldCapture }()
			var captured []researchstore.IngestEvent
			researchCapture = &crawlerResearchCapture{outbox: t.TempDir(), recorder: researchRecorderFunc(func(_ context.Context, event researchstore.IngestEvent) (int64, error) {
				captured = append(captured, event)
				return 1, nil
			})}
			before := time.Now()
			body, err := fetchForMatch(server.URL+"/i/match/jczq/sporttery/4981?token=secret", "4981", "sporttery_odds")
			after := time.Now()
			if len(captured) != 1 || string(captured[0].Payload) != raw || captured[0].HTTPStatus != status {
				t.Fatal("raw source response was not archived before returning")
			}
			if captured[0].ObservedAt.Before(before) || captured[0].ObservedAt.After(after) || captured[0].SourceAt != nil || strings.Contains(captured[0].RequestURL, "secret") {
				t.Fatal("receipt timestamp or URL sanitization was incorrect")
			}
			if status == 200 && (err != nil || string(body) != raw) {
				t.Fatalf("successful source behavior changed: %s %v", body, err)
			}
			if status == 403 && (err == nil || !strings.Contains(err.Error(), "status code error: 403")) {
				t.Fatal("the existing browser-fallback trigger was not preserved")
			}
		})
	}
}

func TestBrowserReceiptDoesNotInventHTTPStatus(t *testing.T) {
	oldCapture := researchCapture
	defer func() { researchCapture = oldCapture }()
	var captured []researchstore.IngestEvent
	researchCapture = &crawlerResearchCapture{outbox: t.TempDir(), recorder: researchRecorderFunc(func(_ context.Context, event researchstore.IngestEvent) (int64, error) {
		captured = append(captured, event)
		return 1, nil
	})}
	received := time.Date(2026, 10, 29, 10, 35, 0, 123456000, time.UTC)
	body := []byte(`{"data":{"spf":{}}}`)
	archiveResearchBrowserResponse("https://www.vipc.cn/i/match/jczq/sporttery/4981", "4981", body, received)
	if len(captured) != 1 || captured[0].HTTPStatus != 0 || captured[0].ParserVersion != "vipc-crawler-browser-response-v1" || !captured[0].ObservedAt.Equal(received) || !bytes.Equal(captured[0].Payload, body) {
		t.Fatal("browser receipt was not distinguished from an original HTTP capture")
	}
}
