package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"go_server/internal/drawmodeldata"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Full content SHA256s are calculated in MySQL; large JSON blobs do not cross
// the connection or get parsed merely to prove a restart has no new input.
// Unlike updated_at-only checks, same-timestamp corrections and deletions count.
func drawModelSourceRevision(db *gorm.DB) (string, error) {
	if db == nil {
		return "", fmt.Errorf("model source unavailable")
	}
	parts := []interface{}{"draw-model-source-v1"}
	err := db.Transaction(func(tx *gorm.DB) error {
		schema, err := drawObservationReadSchema(tx)
		if err != nil {
			return err
		}
		for _, table := range []string{"money", "moneys", "odds_moneys", "pankou_moneys", "history_moneys"} {
			if !schema[table]["match_id"] {
				parts = append(parts, table, "absent")
				continue
			}
			wanted := drawObservationFrontendMainColumns
			switch table {
			case "odds_moneys":
				wanted = drawObservationFrontendOddsColumns
			case "pankou_moneys":
				wanted = drawObservationFrontendPankouColumns
			case "history_moneys":
				wanted = append(append([]string{}, drawObservationFrontendHistoryColumns...), "updated_at", "captured_at")
			}
			columns := drawObservationProjection(schema, table, wanted)
			quoted := make([]string, 0, len(columns))
			for _, column := range columns {
				quoted = append(quoted, "`"+column+"`")
			}
			var rows []struct {
				ID        string     `gorm:"column:match_id"`
				Hash      string     `gorm:"column:content_hash"`
				MatchTime *time.Time `gorm:"column:match_time"`
				Past      bool       `gorm:"-"`
			}
			expression := "match_id, SHA2(CAST(JSON_ARRAY(" + strings.Join(quoted, ",") + ") AS CHAR),256) AS content_hash"
			if (table == "money" || table == "moneys") && schema[table]["match_time"] {
				expression += ", match_time"
			}
			if err = tx.Table(table).Select(expression).Scan(&rows).Error; err != nil {
				return err
			}
			for i := range rows {
				if rows[i].MatchTime != nil {
					rows[i].Past = !drawSourceTime(*rows[i].MatchTime).After(time.Now())
				}
			}
			sort.Slice(rows, func(i, j int) bool {
				if rows[i].ID == rows[j].ID {
					return rows[i].Hash < rows[j].Hash
				}
				return rows[i].ID < rows[j].ID
			})
			parts = append(parts, table, columns, rows)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return "", err
	}
	return drawObservationDigest(parts), nil
}

type drawModelSourceGate struct{ Source, Model, Recipe, Features, ScheduleDay string }

func makeDrawModelSourceGate(source, recipe string) drawModelSourceGate {
	return drawModelSourceGate{source, drawmodeldata.ArtifactSHA256(), recipe, drawTrainingFeatureHash, time.Now().In(drawSourceLocation).Format("2006-01-02")}
}
func drawModelGateMatches(dir string, gate drawModelSourceGate) bool {
	body, err := os.ReadFile(filepath.Join(dir, "source-gate.json"))
	if err != nil {
		return false
	}
	var saved drawModelSourceGate
	return json.Unmarshal(body, &saved) == nil && saved == gate && len(gate.Source) == 64
}
func saveDrawModelGate(dir string, gate drawModelSourceGate) error {
	body, err := json.Marshal(gate)
	if err != nil {
		return err
	}
	return drawModelWriteAtomic(filepath.Join(dir, "source-gate.json"), body)
}
func archiveActiveDrawModel(dir string) error {
	body := drawmodeldata.ActiveArtifact()
	path := filepath.Join(dir, "artifacts", drawmodeldata.ArtifactSHA256()+".json")
	if existing, err := os.ReadFile(path); err != nil || !bytes.Equal(existing, body) {
		if err = drawModelWriteAtomic(path, body); err != nil {
			return err
		}
	}
	if existing, err := os.ReadFile(filepath.Join(dir, "current.json")); err != nil || !bytes.Equal(existing, body) {
		return drawModelWriteAtomic(filepath.Join(dir, "current.json"), body)
	}
	return nil
}
func loadDrawModelLastGood(dir string) {
	for _, name := range []string{"previous.json", "current.json"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var metadata struct {
			Training drawmodeldata.TrainingInfo `json:"training"`
		}
		if json.Unmarshal(body, &metadata) == nil && drawModelAtLeastAsRecent(metadata.Training, drawmodeldata.Training()) {
			_ = drawmodeldata.LoadArtifact(body)
		}
	}
}

func drawModelAtLeastAsRecent(candidate, current drawmodeldata.TrainingInfo) bool {
	a, aErr := time.Parse(time.RFC3339Nano, candidate.GeneratedAt)
	b, bErr := time.Parse(time.RFC3339Nano, current.GeneratedAt)
	// A new correction/removal can legitimately move LatestDate backwards.
	// Publication time, not sample count or cutoff date, orders valid artifacts.
	return aErr == nil && (bErr != nil || !a.Before(b))
}
