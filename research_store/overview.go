package researchstore

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// TableCatalog is intentionally explicit: Overview cannot be used to inspect an
// arbitrary business schema or table name supplied by an HTTP request.
var TableCatalog = map[string][2]string{
	"schema_migrations":   {"数据库版本", "治理"},
	"ingest_events":       {"原始采集档案", "原始证据"},
	"entities":            {"球队联赛身份", "标准数据"},
	"entity_aliases":      {"来源身份映射", "标准数据"},
	"matches":             {"标准比赛", "标准数据"},
	"market_quotes":       {"公司盘口报价事件", "标准数据"},
	"match_results":       {"常规加时点球赛果", "结果标签"},
	"feature_definitions": {"特征定义字典", "特征"},
	"feature_snapshots":   {"不可变特征快照", "特征"},
	"snapshot_sources":    {"特征来源血缘", "特征"},
	"predictions":         {"封存模型与双比分预测", "预测结算"},
	"settlements":         {"赛后结算", "预测结算"},
	"audit_runs":          {"清洗审计批次", "质量审计"},
	"audit_issues":        {"质量问题明细", "质量审计"},
	"datasets":            {"固定数据集", "学习评估"},
	"dataset_members":     {"数据集样本与切分", "学习评估"},
	"model_runs":          {"模型工件与训练登记", "学习评估"},
	"evaluations":         {"版本效果评估", "学习评估"},
}

func (s *Store) Overview(ctx context.Context) (Overview, error) {
	out := Overview{Database: s.name, SchemaVersion: SchemaVersion, GeneratedAt: time.Now().UTC(), Tables: []TableInfo{}, Provenance: map[string]int64{}, Quality: QualitySummary{BySeverity: map[string]int64{}}, FeatureSets: []FeatureSetInfo{}, AuditIssues: map[string]int64{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err:=tx.QueryRowContext(ctx,`SELECT version FROM schema_migrations ORDER BY applied_at DESC,version DESC LIMIT 1`).Scan(&out.SchemaVersion);err!=nil{return out,err}
	var names []string
	for name := range TableCatalog {
		names = append(names, name)
	}
	sort.Strings(names)
	counts := map[string]int64{}
	for _, name := range names {
		catalog := TableCatalog[name]
		table := TableInfo{Name: name, Label: catalog[0], Layer: catalog[1], Columns: []ColumnInfo{}, Indexes: []IndexInfo{}, ForeignKeys: []ForeignKeyInfo{}}
		if err := tx.QueryRowContext(ctx, `SELECT table_comment FROM information_schema.tables WHERE table_schema=? AND table_name=?`, s.name, name).Scan(&table.Comment); err != nil {
			return out, fmt.Errorf("read research table %s: %w", name, err)
		}
		table.Description = table.Comment
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+name+"`").Scan(&table.Rows); err != nil {
			return out, err
		}
		counts[name] = table.Rows
		rows, err := tx.QueryContext(ctx, `SELECT column_name,column_type,is_nullable,column_key,column_comment FROM information_schema.columns WHERE table_schema=? AND table_name=? ORDER BY ordinal_position`, s.name, name)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var col ColumnInfo
			var nullable string
			if err := rows.Scan(&col.Name, &col.Type, &nullable, &col.Key, &col.Comment); err != nil {
				rows.Close()
				return out, err
			}
			col.Nullable = nullable == "YES"
			col.PrimaryKey = col.Key == "PRI"
			table.Columns = append(table.Columns, col)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
		rows, err = tx.QueryContext(ctx, `SELECT index_name,non_unique,column_name FROM information_schema.statistics WHERE table_schema=? AND table_name=? ORDER BY index_name,seq_in_index`, s.name, name)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var index, column string
			var nonUnique int
			if err := rows.Scan(&index, &nonUnique, &column); err != nil {
				rows.Close()
				return out, err
			}
			if len(table.Indexes) == 0 || table.Indexes[len(table.Indexes)-1].Name != index {
				table.Indexes = append(table.Indexes, IndexInfo{Name: index, Unique: nonUnique == 0, Columns: []string{}})
			}
			last := len(table.Indexes) - 1
			table.Indexes[last].Columns = append(table.Indexes[last].Columns, column)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
		rows, err = tx.QueryContext(ctx, `SELECT column_name,referenced_table_name,referenced_column_name FROM information_schema.key_column_usage WHERE table_schema=? AND table_name=? AND referenced_table_name IS NOT NULL ORDER BY constraint_name,ordinal_position`, s.name, name)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var key ForeignKeyInfo
			if err := rows.Scan(&key.Column, &key.ReferencedTable, &key.ReferencedColumn); err != nil {
				rows.Close()
				return out, err
			}
			table.ForeignKeys = append(table.ForeignKeys, key)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return out, err
		}
		rows.Close()
		out.Tables = append(out.Tables, table)
	}
	out.Summary = Summary{Matches: counts["matches"], IngestEvents: counts["ingest_events"], Quotes: counts["market_quotes"], FeatureDefinitions: counts["feature_definitions"], FeatureSnapshots: counts["feature_snapshots"], Predictions: counts["predictions"], AuditIssues: counts["audit_issues"], Datasets: counts["datasets"], Evaluations: counts["evaluations"]}
	rows, err := tx.QueryContext(ctx, `SELECT provenance,COUNT(*) FROM feature_snapshots GROUP BY provenance`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var p string
		var n int64
		if err := rows.Scan(&p, &n); err != nil {
			rows.Close()
			return out, err
		}
		out.Provenance[p] = n
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	out.Quality.LiveSnapshots = out.Provenance["live"]
	out.Quality.ReconstructedSnapshots = out.Provenance["reconstructed"]
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_issues WHERE status='open'`).Scan(&out.Quality.OpenIssues); err != nil {
		return out, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT severity,COUNT(*) FROM audit_issues WHERE status='open' GROUP BY severity`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var severity string
		var n int64
		if err := rows.Scan(&severity, &n); err != nil {
			rows.Close()
			return out, err
		}
		out.Quality.BySeverity[severity] = n
		out.AuditIssues[severity] = n
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT feature_version,COUNT(*) FROM feature_definitions GROUP BY feature_version ORDER BY feature_version`)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var set FeatureSetInfo
		if err := rows.Scan(&set.Version, &set.Features); err != nil {
			rows.Close()
			return out, err
		}
		out.FeatureSets = append(out.FeatureSets, set)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	return out, tx.Commit()
}
