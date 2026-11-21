package handlers

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"go_server/database"
)

func TestMobileSyncGzipPreservesPayload(t *testing.T) {
	for _, encoding := range []string{"gzip", "gzip;q=0", "br, gzip;q=1.0", ""} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("POST", "/api/mobile/sync/batch", nil)
		c.Request.Header.Set("Accept-Encoding", encoding)
		payload := mobileSyncRecord{MatchID: "1", Revision: mobileSyncSHA([]byte(`{"matchId":"1","home":"球队"}`)), Payload: `{"matchId":"1","home":"球队"}`}
		mobileSyncJSON(c, payload)
		data := recorder.Body.Bytes()
		if encoding == "gzip" || encoding == "br, gzip;q=1.0" {
			if recorder.Header().Get("Content-Encoding") != "gzip" {
				t.Fatal("gzip not negotiated")
			}
			reader, err := gzip.NewReader(recorder.Body)
			if err != nil {
				t.Fatal(err)
			}
			data, err = io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			_ = reader.Close()
		} else if recorder.Header().Get("Content-Encoding") != "" {
			t.Fatal("gzip rejected by client but still encoded")
		}
		var got mobileSyncRecord
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got != payload || mobileSyncSHA([]byte(got.Payload)) != got.Revision {
			t.Fatal("gzip changed raw hash payload")
		}
	}
}

func TestMobileSyncOnlyStorageTimestampChangeDoesNotRedownload(t *testing.T) {
	row := map[string]interface{}{"matchId": "1", "rawMatch": map[string]interface{}{"matchId": "1", "homeScore": 0, "updatedAt": "before"}, "rawEuroOdds": map[string]interface{}{"capturedAt": "provider-capture", "data": []float64{1.8, 3.2, 4.1}}}
	a, err := mobileSyncStablePayload(row)
	if err != nil {
		t.Fatal(err)
	}
	row["rawMatch"].(map[string]interface{})["updatedAt"] = "after"
	b, err := mobileSyncStablePayload(row)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatal("storage-only update forced download")
	}
	row["rawMatch"].(map[string]interface{})["homeScore"] = 1
	c, err := mobileSyncStablePayload(row)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == string(c) {
		t.Fatal("corrected score was ignored")
	}
	var got map[string]interface{}
	_ = json.Unmarshal(c, &got)
	if got["rawEuroOdds"].(map[string]interface{})["capturedAt"] != "provider-capture" {
		t.Fatal("provider capture time lost")
	}
}

func TestMobileSyncImmutableIncrementalAndFallback(t *testing.T) {
	store := newMobileSyncStore(t.TempDir())
	source := "source1"
	calls := 0
	rows := map[string]string{"1": `{"matchId":"1","home":"甲","history":{"leagueSummary":{"home":{"all":{"rank":2,"game":12}}}}}`, "2": `{"matchId":"2","status":0}`}
	store.source = func(context.Context) (string, error) { return source, nil }
	store.export = func(_ context.Context, emit func(string, []byte) error) (string, error) {
		calls++
		for id, payload := range rows {
			if err := emit(id, []byte(payload)); err != nil {
				return "", err
			}
		}
		return "model-1", nil
	}
	first, err := store.manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || first.ScheduleTimezone != "Asia/Shanghai" {
		t.Fatalf("bad manifest: %+v", first)
	}
	unchanged, err := store.manifest(context.Background())
	if err != nil || unchanged.Snapshot != first.Snapshot || calls != 1 {
		t.Fatalf("unchanged data rebuilt: %d %v", calls, err)
	}
	firstRows, err := store.batch(first.Snapshot, []string{"1", "2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range firstRows {
		if mobileSyncSHA([]byte(row.Payload)) != row.Revision {
			t.Fatal("payload checksum mismatch")
		}
	}
	// A corrected result and a deleted fixture must appear in the next manifest.
	source = "source2"
	rows["1"] = `{"matchId":"1","homeScore":2,"awayScore":1}`
	delete(rows, "2")
	rows["3"] = `{"matchId":"3","status":0}`
	second, err := store.manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Snapshot == first.Snapshot || second.Total != 2 || second.Entries[1].MatchID != "3" {
		t.Fatal("change/deletion missing")
	}
	old, err := store.batch(first.Snapshot, []string{"1"})
	if err != nil || old[0].Payload != firstRows[0].Payload {
		t.Fatal("old snapshot mutated while downloading")
	}
	// Failed rebuild neither publishes partial files nor destroys the last cache.
	source = "source3"
	store.export = func(_ context.Context, emit func(string, []byte) error) (string, error) {
		_ = emit("1", []byte(`{"matchId":"1"}`))
		return "", errors.New("database interrupted")
	}
	if _, err = store.manifest(context.Background()); err == nil {
		t.Fatal("expected failure")
	}
	if _, err = store.batch(second.Snapshot, []string{"1", "3"}); err != nil {
		t.Fatal("previous complete generation lost", err)
	}
	files, _ := os.ReadDir(store.root)
	for _, file := range files {
		if !mobileSyncValidHash(file.Name()) {
			t.Fatalf("partial generation left behind: %s", file.Name())
		}
	}
}

func TestMobileSyncSnapshotValidation(t *testing.T) {
	store := newMobileSyncStore(t.TempDir())
	store.source = func(context.Context) (string, error) { return "1", nil }
	store.export = func(_ context.Context, emit func(string, []byte) error) (string, error) {
		return "v1", emit("../fixture", []byte(`{"matchId":"../fixture"}`))
	}
	manifest, err := store.manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.batch("../../outside", []string{"../fixture"}); err == nil {
		t.Fatal("accepted invalid snapshot path")
	}
	if _, err = store.batch(manifest.Snapshot, []string{"missing"}); err == nil {
		t.Fatal("accepted match absent from manifest")
	}
	if _, err = store.batch(manifest.Snapshot, []string{"../fixture", "../fixture"}); err == nil {
		t.Fatal("accepted duplicate ids")
	}
	file := filepath.Join(store.root, manifest.Snapshot, mobileSyncSHA([]byte("../fixture"))+".json")
	if err = os.WriteFile(file, []byte(`{"matchId":"wrong"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = store.batch(manifest.Snapshot, []string{"../fixture"}); err == nil {
		t.Fatal("corrupt payload accepted")
	}
}

// Opt-in integration only: no listening server, crawler or migration is run.
func TestMobileSyncReadOnlyExportIntegration(t *testing.T) {
	if os.Getenv("MOBILE_SYNC_READONLY_TEST") != "1" {
		t.Skip("set MOBILE_SYNC_READONLY_TEST=1 for local SELECT-only export")
	}
	if err := database.Init(); err != nil {
		t.Fatal(err)
	}
	store := newMobileSyncStore(t.TempDir())
	manifest, err := store.manifest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Total == 0 {
		t.Fatal("no main-table matches exported")
	}
	if measure := os.Getenv("MOBILE_SYNC_MEASURE_SCRIPT"); measure != "" {
		output, err := exec.CommandContext(context.Background(), "python", measure, filepath.Join(store.root, manifest.Snapshot)).CombinedOutput()
		if err != nil {
			t.Fatalf("cache measurement: %v %s", err, output)
		}
		t.Logf("cache measurement: %s", output)
	}
	hasNovember, hasPending, hasNonSporttery := false, false, false
	for _, entry := range manifest.Entries {
		rows, err := store.batch(manifest.Snapshot, []string{entry.MatchID})
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]interface{}
		if err = json.Unmarshal([]byte(rows[0].Payload), &row); err != nil {
			t.Fatal(err)
		}
		if date, _ := row["date"].(string); len(date) >= 7 && date[:7] == "2026-11" {
			hasNovember = true
		}
		if settled, _ := row["settled"].(bool); !settled {
			hasPending = true
		}
		if id, _ := row["jingcaiId"].(string); id == "" {
			hasNonSporttery = true
		}
		for _, key := range []string{"history", "rawMatch", "rawEuroOdds", "rawHandicapOdds", "companyQuotes", "euroOdds"} {
			if _, ok := row[key]; !ok && key != "companyQuotes" {
				t.Fatalf("missing %s", key)
			}
		}
	}
	if !hasNovember || !hasPending || !hasNonSporttery {
		t.Fatalf("not whole library: November=%v pending=%v non-JC=%v", hasNovember, hasPending, hasNonSporttery)
	}
	t.Logf("verified %d complete offline payloads; model=%s snapshot=%s", manifest.Total, manifest.ModelVersion, manifest.Snapshot)
}
