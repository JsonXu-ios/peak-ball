package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/internal/drawmodeldata"
	"gorm.io/gorm"
)

type DrawObservationModelStatus struct {
	State          string                     `json:"state"`
	Source         string                     `json:"source"`
	Message        string                     `json:"message"`
	CheckedAt      time.Time                  `json:"checkedAt"`
	ModelVersion   string                     `json:"modelVersion"`
	ArtifactSHA256 string                     `json:"artifactSHA256"`
	Training       drawmodeldata.TrainingInfo `json:"training"`
	Added          int                        `json:"added"`
	Changed        int                        `json:"changed"`
	Removed        int                        `json:"removed"`
}

var drawModelUpdateStatus = struct {
	sync.RWMutex
	value DrawObservationModelStatus
}{}
var drawModelUpdateLock sync.Mutex

func CurrentDrawObservationModelStatus() DrawObservationModelStatus {
	drawModelUpdateStatus.RLock()
	result := drawModelUpdateStatus.value
	drawModelUpdateStatus.RUnlock()
	if result.State == "" {
		result.State = "embedded"
		result.Source = "local_database"
		result.Message = "使用随程序发布的有效模型"
	}
	result.ModelVersion = drawmodeldata.Version()
	result.ArtifactSHA256 = drawmodeldata.ArtifactSHA256()
	result.Training = drawmodeldata.Training()
	return result
}
func GetDrawObservationModelStatus(c *gin.Context) { c.JSON(200, CurrentDrawObservationModelStatus()) }

// RefreshDrawObservationModel is intentionally synchronous and must be called
// after DB initialization and BEFORE research workers or HTTP listeners start.
// Source collection remains the existing crawler's job: this checks the newest
// committed local database snapshot, not an unverifiable external refresh.
func RefreshDrawObservationModel(ctx context.Context, db *gorm.DB) DrawObservationModelStatus {
	drawModelUpdateLock.Lock()
	defer drawModelUpdateLock.Unlock()
	// A separately configured persisted model remains usable even if this
	// deployment is missing optional training scripts or a project checkout.
	dir := strings.TrimSpace(os.Getenv("DRAW_MODEL_DATA_DIR"))
	if dir != "" {
		loadDrawModelLastGood(dir)
	}
	root, err := drawModelProjectRoot()
	if err != nil {
		return finishDrawModelUpdate(DrawObservationModelStatus{State: "fallback", Message: err.Error()})
	}
	if dir == "" {
		dir = filepath.Join(root, "data", "draw-model")
	}
	python := strings.TrimSpace(os.Getenv("DRAW_MODEL_PYTHON"))
	if python == "" {
		python = filepath.Join(dir, "python-env", "Scripts", "python.exe")
		if _, err := os.Stat(python); err != nil {
			python = filepath.Join(dir, "python-env", "bin", "python")
			if _, err = os.Stat(python); err != nil {
				python = "python"
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	loadDrawModelLastGood(dir)
	if db == nil {
		return finishDrawModelUpdate(DrawObservationModelStatus{State: "fallback", Message: "数据库未连接，沿用有效模型"})
	}
	recipe, recipeErr := drawModelRecipeHash(root)
	revision, revisionErr := drawModelSourceRevision(db.WithContext(ctx))
	if recipeErr == nil && revisionErr == nil && drawModelGateMatches(dir, makeDrawModelSourceGate(revision, recipe)) {
		if err := archiveActiveDrawModel(dir); err != nil {
			return finishDrawModelUpdate(DrawObservationModelStatus{State: "fallback", Message: "模型归档失败；继续使用有效模型"})
		}
		return finishDrawModelUpdate(DrawObservationModelStatus{State: "unchanged", Message: "已核对全部源数据内容指纹，无新增或变更，沿用当前模型"})
	}
	status := runDrawModelRefresh(ctx, root, dir, func() (DrawObservationTrainingSnapshot, error) {
		if db == nil {
			return DrawObservationTrainingSnapshot{}, fmt.Errorf("模型更新数据库未连接")
		}
		source, err := ExportDrawFeatureStudy(db.WithContext(ctx), time.Now())
		if err != nil {
			return DrawObservationTrainingSnapshot{}, fmt.Errorf("读取最新比赛数据失败: %w", err)
		}
		return BuildDrawObservationTrainingSnapshot(source)
	}, func(input, output string) error {
		command := exec.CommandContext(ctx, python, filepath.Join(root, "scripts", "update-draw-observation-model.py"), "--input", input, "--output", output)
		command.Dir = root
		outputText, err := command.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(outputText))
			if len(text) > 1200 {
				text = text[len(text)-1200:]
			}
			return fmt.Errorf("训练未完成，沿用有效模型 (%v): %s", err, text)
		}
		return nil
	})
	if status.State != "fallback" {
		if err := archiveActiveDrawModel(dir); err != nil {
			status.Message += "；内容归档失败"
		} else if recipeErr == nil && revisionErr == nil {
			// A changing crawler during export must not certify a mixed signature.
			if after, err := drawModelSourceRevision(db.WithContext(ctx)); err == nil && after == revision {
				_ = saveDrawModelGate(dir, makeDrawModelSourceGate(after, recipe))
			}
		}
	}
	return finishDrawModelUpdate(status)
}

func finishDrawModelUpdate(status DrawObservationModelStatus) DrawObservationModelStatus {
	status.Source = "local_database"
	status.CheckedAt = time.Now().UTC()
	status.ModelVersion = drawmodeldata.Version()
	status.ArtifactSHA256 = drawmodeldata.ArtifactSHA256()
	status.Training = drawmodeldata.Training()
	drawModelUpdateStatus.Lock()
	drawModelUpdateStatus.value = status
	drawModelUpdateStatus.Unlock()
	return status
}
func drawModelProjectRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv("DRAW_MODEL_ROOT")); root != "" {
		return filepath.Abs(root)
	}
	cwd, _ := os.Getwd()
	executable, _ := os.Executable()
	for _, start := range []string{cwd, filepath.Dir(executable)} {
		for dir, depth := start, 0; depth < 5; depth++ {
			if _, err := os.Stat(filepath.Join(dir, "scripts", "update-draw-observation-model.py")); err == nil {
				return dir, nil
			}
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			dir = next
		}
	}
	return "", fmt.Errorf("找不到模型训练脚本；请设置 DRAW_MODEL_ROOT，当前有效模型继续可用")
}
func drawModelRecipeHash(root string) (string, error) {
	hash := sha256.New()
	for _, name := range []string{"update-draw-observation-model.py", "draw-runtime-requirements.txt"} {
		body, err := os.ReadFile(filepath.Join(root, "scripts", name))
		if err != nil {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(body)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
func drawModelWriteAtomic(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".model-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(body); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
func runDrawModelRefresh(ctx context.Context, root, dir string, load func() (DrawObservationTrainingSnapshot, error), train func(string, string) error) DrawObservationModelStatus {
	status := DrawObservationModelStatus{State: "fallback"}
	fail := func(err error) DrawObservationModelStatus {
		status.Message = err.Error()
		return finishDrawModelUpdate(status)
	}
	current := filepath.Join(dir, "current.json")
	loadDrawModelLastGood(dir)
	snapshot, err := load()
	if err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	if len(snapshot.Rows) < 100 || len(snapshot.DataSHA256) != 64 {
		return fail(fmt.Errorf("有效已完赛训练数据不足；保留当前模型"))
	}
	var previous map[string]string
	if body, err := os.ReadFile(filepath.Join(dir, "row-hashes.json")); err == nil {
		_ = json.Unmarshal(body, &previous)
	}
	for id, hash := range snapshot.RowHashes {
		if old, ok := previous[id]; !ok {
			status.Added++
		} else if old != hash {
			status.Changed++
		}
	}
	for id := range previous {
		if _, ok := snapshot.RowHashes[id]; !ok {
			status.Removed++
		}
	}
	recipe, err := drawModelRecipeHash(root)
	if err != nil {
		return fail(err)
	}
	training := drawmodeldata.Training()
	hashes, _ := json.Marshal(snapshot.RowHashes)
	if training.DataSHA256 == snapshot.DataSHA256 && training.RecipeSHA256 == recipe && training.FeatureCodeSHA256 == snapshot.FeatureCodeSHA256 {
		if err = drawModelWriteAtomic(filepath.Join(dir, "row-hashes.json"), hashes); err != nil {
			return fail(err)
		}
		status.State = "unchanged"
		status.Message = "已核对最新数据，没有训练输入变动，沿用当前模型"
		return finishDrawModelUpdate(status)
	}
	input := filepath.Join(dir, "training-input.json")
	body, err := json.Marshal(snapshot)
	if err != nil {
		return fail(err)
	}
	if err = drawModelWriteAtomic(input, body); err != nil {
		return fail(err)
	}
	candidate, err := os.CreateTemp(dir, "candidate-*.json")
	if err != nil {
		return fail(err)
	}
	candidatePath := candidate.Name()
	candidate.Close()
	defer os.Remove(candidatePath)
	if err = train(input, candidatePath); err != nil {
		return fail(err)
	}
	if err = ctx.Err(); err != nil {
		return fail(err)
	}
	body, err = os.ReadFile(candidatePath)
	if err != nil {
		return fail(err)
	}
	if err = drawmodeldata.ValidateArtifact(body); err != nil {
		return fail(fmt.Errorf("新模型校验失败: %w", err))
	}
	var metadata struct {
		Training drawmodeldata.TrainingInfo `json:"training"`
	}
	if err = json.Unmarshal(body, &metadata); err != nil {
		return fail(err)
	}
	if metadata.Training.DataSHA256 != snapshot.DataSHA256 || metadata.Training.RecipeSHA256 != recipe || metadata.Training.FeatureCodeSHA256 != snapshot.FeatureCodeSHA256 {
		return fail(fmt.Errorf("新模型与本次数据或训练配置不一致"))
	}
	// Publish only after parity/schema validation. The previous artifact survives
	// interrupted training and filesystem failures, and the release is a fallback.
	if err = drawModelWriteAtomic(filepath.Join(dir, "previous.json"), drawmodeldata.ActiveArtifact()); err != nil {
		return fail(err)
	}
	if err = drawModelWriteAtomic(current, body); err != nil {
		return fail(err)
	}
	if err = drawmodeldata.LoadArtifact(body); err != nil {
		return fail(err)
	}
	status.State = "updated"
	status.Message = "新增或变更比赛已纳入模型，已保存并启用新版本"
	if err = drawModelWriteAtomic(filepath.Join(dir, "row-hashes.json"), hashes); err != nil {
		status.Message += "；变更索引写入失败，下次将重新核对"
	}
	return finishDrawModelUpdate(status)
}

// Persistent day responses were computed using a particular model. Do not
// reuse an older selection even when the underlying source rows are unchanged.
func drawObservationModelStoredDayReady(day drawObservationStoredDay) bool {
	for _, row := range day.Items {
		signal := row.FrozenDrawSignal
		if strings.TrimSpace(row.JingcaiID) == "" {
			signal = row.NonSportterySignal
		}
		if signal == nil || signal.ModelVersion != drawmodeldata.Version() {
			return false
		}
	}
	return true
}
