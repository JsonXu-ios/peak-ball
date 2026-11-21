package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go_server/internal/drawmodeldata"
)

func modelUpdateTestSnapshot() DrawObservationTrainingSnapshot {
	info := drawmodeldata.Training()
	return DrawObservationTrainingSnapshot{Rows: make([]DrawObservationTrainingRow, 100), DataSHA256: info.DataSHA256, FeatureCodeSHA256: info.FeatureCodeSHA256, RowHashes: map[string]string{"same": "s", "changed": "new", "added": "a"}}
}

func modelUpdateTestTime(t *testing.T, offset time.Duration) string {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, drawmodeldata.Training().GeneratedAt)
	if err != nil {
		t.Fatal(err)
	}
	return at.Add(offset).Format(time.RFC3339Nano)
}
func TestDrawModelStartupUnchangedSkipsTrainingAndCountsChanges(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join("..", "..")
	snapshot := modelUpdateTestSnapshot()
	_ = drawModelWriteAtomic(filepath.Join(dir, "row-hashes.json"), []byte(`{"same":"s","changed":"old","deleted":"d"}`))
	called := false
	status := runDrawModelRefresh(context.Background(), root, dir, func() (DrawObservationTrainingSnapshot, error) { return snapshot, nil }, func(string, string) error { called = true; return errors.New("should not run") })
	if called || status.State != "unchanged" || status.Added != 1 || status.Changed != 1 || status.Removed != 1 {
		t.Fatalf("unexpected reuse: %+v called=%v", status, called)
	}
}
func TestDrawModelStartupFailureRetainsValidFileAndModel(t *testing.T) {
	for _, failure := range []string{"source", "trainer", "invalid", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			saved := drawmodeldata.ActiveArtifact()
			defer drawmodeldata.LoadArtifact(saved)
			hash := drawmodeldata.ArtifactSHA256()
			dir := t.TempDir()
			root := filepath.Join("..", "..")
			if err := drawModelWriteAtomic(filepath.Join(dir, "current.json"), saved); err != nil {
				t.Fatal(err)
			}
			snapshot := modelUpdateTestSnapshot()
			snapshot.DataSHA256 = strings.Repeat("c", 64)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if failure == "cancelled" {
				cancel()
			}
			status := runDrawModelRefresh(ctx, root, dir, func() (DrawObservationTrainingSnapshot, error) {
				if failure == "source" {
					return snapshot, errors.New("source unavailable")
				}
				return snapshot, nil
			}, func(_, output string) error {
				if failure == "trainer" {
					return errors.New("training interrupted")
				}
				return os.WriteFile(output, []byte(`{"schema":1}`), 0600)
			})
			body, err := os.ReadFile(filepath.Join(dir, "current.json"))
			if err != nil {
				t.Fatal(err)
			}
			if status.State != "fallback" || drawmodeldata.ArtifactSHA256() != hash || string(body) != string(saved) {
				t.Fatalf("failed update corrupted valid model: %+v", status)
			}
		})
	}
}
func TestDrawModelStartupValidatedCandidatePublishesAndNoChangeReuses(t *testing.T) {
	saved := drawmodeldata.ActiveArtifact()
	defer drawmodeldata.LoadArtifact(saved)
	dir := t.TempDir()
	root := filepath.Join("..", "..")
	snapshot := modelUpdateTestSnapshot()
	snapshot.DataSHA256 = strings.Repeat("d", 64)
	load := func() (DrawObservationTrainingSnapshot, error) { return snapshot, nil }
	called := 0
	runner := func(_, output string) error {
		called++
		var candidate map[string]interface{}
		_ = json.Unmarshal(saved, &candidate)
		candidate["version"] = "test-model-content-d"
		candidate["training"].(map[string]interface{})["dataSHA256"] = snapshot.DataSHA256
		body, _ := json.Marshal(candidate)
		return os.WriteFile(output, body, 0600)
	}
	status := runDrawModelRefresh(context.Background(), root, dir, load, runner)
	if status.State != "updated" || drawmodeldata.Version() != "test-model-content-d" || called != 1 {
		t.Fatalf("candidate not published: %+v", status)
	}
	previous, err := os.ReadFile(filepath.Join(dir, "previous.json"))
	if err != nil || string(previous) != string(saved) {
		t.Fatal("previous model not preserved")
	}
	status = runDrawModelRefresh(context.Background(), root, dir, load, runner)
	if status.State != "unchanged" || called != 1 {
		t.Fatalf("unchanged data retrained: %+v", status)
	}
}
func TestDrawModelVersionInvalidatesPersistedDay(t *testing.T) {
	day := drawObservationStoredDay{Items: []drawObservationItem{{JingcaiID: "001", FrozenDrawSignal: &drawObservationFrozenSignal{ModelVersion: drawmodeldata.Version()}}}}
	if !drawObservationModelStoredDayReady(day) {
		t.Fatal("current day rejected")
	}
	day.Items[0].FrozenDrawSignal.ModelVersion = "old"
	if drawObservationModelStoredDayReady(day) {
		t.Fatal("old model cache accepted")
	}
	day.Items[0].FrozenDrawSignal = nil
	if drawObservationModelStoredDayReady(day) {
		t.Fatal("versionless model cache accepted")
	}
	day.Items[0].JingcaiID = ""
	day.Items[0].NonSportterySignal = &drawObservationFrozenSignal{ModelVersion: "old"}
	if drawObservationModelStoredDayReady(day) {
		t.Fatal("old nonlottery cache accepted")
	}
}
func TestDrawTrainingDigestIgnoresCaptureTimeButIncludesLabelAndFeatures(t *testing.T) {
	// Build from a real portable row contract without any live database access.
	first := DrawFeatureStudyDataset{GeneratedAt: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC), Rows: []drawFeatureStudyRow{{MatchID: "a", Date: "2026-11-20", MatchTime: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC), Result: 0, Latest: drawFeatureStudyPhase{Average: []float64{2, 3, 4}}}}}
	a, err := BuildDrawObservationTrainingSnapshot(first)
	if err != nil {
		t.Fatal(err)
	}
	first.GeneratedAt = first.GeneratedAt.AddDate(0, 0, 1)
	b, _ := BuildDrawObservationTrainingSnapshot(first)
	if a.DataSHA256 != b.DataSHA256 {
		t.Fatal("capture clock alone changes training data")
	}
	first.Rows[0].Result = 1
	c, _ := BuildDrawObservationTrainingSnapshot(first)
	if b.DataSHA256 == c.DataSHA256 {
		t.Fatal("corrected result not detected")
	}
	first.Rows[0].AnalysisInputs.AverageLatest = []float64{2.2, 3, 4}
	d, _ := BuildDrawObservationTrainingSnapshot(first)
	if c.DataSHA256 == d.DataSHA256 {
		t.Fatal("changed odds not detected")
	}
}

func TestDrawModelOlderSameDayCannotRollbackBundledOnFailure(t *testing.T) {
	saved := drawmodeldata.ActiveArtifact()
	defer drawmodeldata.LoadArtifact(saved)
	before := drawmodeldata.ArtifactSHA256()
	dir := t.TempDir()
	var older map[string]interface{}
	_ = json.Unmarshal(saved, &older)
	older["version"] = "older-same-day"
	older["training"].(map[string]interface{})["generatedAt"] = modelUpdateTestTime(t, -time.Hour)
	body, _ := json.Marshal(older)
	if err := drawModelWriteAtomic(filepath.Join(dir, "current.json"), body); err != nil {
		t.Fatal(err)
	}
	status := runDrawModelRefresh(context.Background(), filepath.Join("..", ".."), dir, func() (DrawObservationTrainingSnapshot, error) {
		return DrawObservationTrainingSnapshot{}, errors.New("source unavailable")
	}, nil)
	if status.State != "fallback" || drawmodeldata.ArtifactSHA256() != before {
		t.Fatal("older same-day local model rolled back bundled model")
	}
}
func TestDrawModelFallbackCanRecoverPreviousWhenCurrentCorrupt(t *testing.T) {
	saved := drawmodeldata.ActiveArtifact()
	defer drawmodeldata.LoadArtifact(saved)
	dir := t.TempDir()
	var newer map[string]interface{}
	_ = json.Unmarshal(saved, &newer)
	newer["version"] = "valid-newer-previous"
	newer["training"].(map[string]interface{})["generatedAt"] = modelUpdateTestTime(t, time.Hour)
	body, _ := json.Marshal(newer)
	_ = drawModelWriteAtomic(filepath.Join(dir, "previous.json"), body)
	_ = drawModelWriteAtomic(filepath.Join(dir, "current.json"), []byte("interrupted"))
	status := runDrawModelRefresh(context.Background(), filepath.Join("..", ".."), dir, func() (DrawObservationTrainingSnapshot, error) {
		return DrawObservationTrainingSnapshot{}, errors.New("source unavailable")
	}, nil)
	if status.State != "fallback" || drawmodeldata.Version() != "valid-newer-previous" {
		t.Fatal("failed to recover newer last-good model")
	}
}
func TestDrawModelSourceGateIncludesModelRecipeFeatureAndDay(t *testing.T) {
	dir := t.TempDir()
	base := makeDrawModelSourceGate(strings.Repeat("a", 64), strings.Repeat("b", 64))
	if err := saveDrawModelGate(dir, base); err != nil {
		t.Fatal(err)
	}
	if !drawModelGateMatches(dir, base) {
		t.Fatal("same gate not reusable")
	}
	variants := []drawModelSourceGate{base, base, base, base, base}
	variants[0].Source = "changed"
	variants[1].Model = "changed"
	variants[2].Recipe = "changed"
	variants[3].Features = "changed"
	variants[4].ScheduleDay = "changed"
	for _, value := range variants {
		if drawModelGateMatches(dir, value) {
			t.Fatal("changed source/recipe/model/day reused")
		}
	}
}

func TestDrawModelNewerCorrectionCanMoveTrainingCutoffBackward(t *testing.T) {
	current := drawmodeldata.Training()
	candidate := current
	candidate.LatestDate = "2026-11-20"
	candidate.GeneratedAt = modelUpdateTestTime(t, time.Hour)
	candidate.Rows--
	if !drawModelAtLeastAsRecent(candidate, current) {
		t.Fatal("newer correction rejected because latest match was removed")
	}
	candidate.GeneratedAt = "bad"
	if drawModelAtLeastAsRecent(candidate, current) {
		t.Fatal("unknown publication time accepted")
	}
}
func TestDrawModelMissingTrainingCheckoutRetainsExplicitLastGood(t *testing.T) {
	saved := drawmodeldata.ActiveArtifact()
	defer drawmodeldata.LoadArtifact(saved)
	dir := t.TempDir()
	var newer map[string]interface{}
	_ = json.Unmarshal(saved, &newer)
	newer["version"] = "persisted-without-training-checkout"
	newer["training"].(map[string]interface{})["generatedAt"] = modelUpdateTestTime(t, time.Hour)
	body, _ := json.Marshal(newer)
	_ = drawModelWriteAtomic(filepath.Join(dir, "current.json"), body)
	t.Setenv("DRAW_MODEL_DATA_DIR", dir)
	t.Setenv("DRAW_MODEL_ROOT", "")
	t.Chdir(t.TempDir())
	status := RefreshDrawObservationModel(context.Background(), nil)
	if status.State != "fallback" || drawmodeldata.Version() != "persisted-without-training-checkout" {
		t.Fatalf("training checkout failure discarded last-good model: %+v", status)
	}
}
