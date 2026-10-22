package handlers

import (
	"fmt"
	"strings"
	"time"

	"go_server/database"
	"go_server/models"
	"gorm.io/gorm"
)

var drawObservationHeaderColumns = []string{"match_id", "jingcai_id", "date", "match_time", "league", "league_name", "league_id", "home", "guest", "home_team_id", "guest_team_id", "status", "match_state", "display_state", "home_score", "guest_score", "home_ot_score", "guest_ot_score", "home_ot_penalty", "guest_ot_penalty"}

func drawObservationProjection(schema drawObservationSchema, table string, wanted []string) []string {
	columns := []string{}
	for _, name := range wanted {
		if schema[table][name] {
			columns = append(columns, name)
		}
	}
	return columns
}

func drawObservationHistoryProjection(schema drawObservationSchema) []string {
	columns := drawObservationProjection(schema, "history_moneys", []string{"match_id", "recent_home_list", "recent_guest_list", "against_list"})
	if schema["history_moneys"]["league_stat"] {
		present := []string{}
		for _, name := range []string{"recent_home_list", "recent_guest_list", "against_list"} {
			if schema["history_moneys"][name] {
				present = append(present, fmt.Sprintf("(`%s` IS NOT NULL AND TRIM(CAST(`%s` AS CHAR)) NOT IN ('','[]','null'))", name, name))
			}
		}
		if len(present) == 0 {
			columns = append(columns, "league_stat")
		} else {
			columns = append(columns, "CASE WHEN "+strings.Join(present, " OR ")+" THEN NULL ELSE `league_stat` END AS league_stat")
		}
	}
	return columns
}

func drawObservationSQLScope(query *gorm.DB, q drawObservationQuery) *gorm.DB {
	if q.date != "" {
		return query.Where("date = ?", q.date)
	}
	if q.start != "" {
		query = query.Where("date >= ?", q.start)
	}
	if q.end != "" {
		query = query.Where("date <= ?", q.end)
	}
	return query
}

func drawObservationReadTargets(db *gorm.DB, schema drawObservationSchema, q *drawObservationQuery) ([]drawObservationTarget, error) {
	sets := map[string][]models.Money{}
	for _, table := range []string{"moneys", "money"} {
		if !schema[table]["match_id"] {
			continue
		}
		query := db.Table(table).Select(drawObservationProjection(schema, table, drawObservationHeaderColumns))
		if q != nil {
			query = drawObservationSQLScope(query, *q)
		}
		var rows []models.Money
		if err := query.Find(&rows).Error; err != nil {
			return nil, err
		}
		sets[table] = rows
	}
	return mergeDrawObservationTargets(sets["moneys"], sets["money"]), nil
}

func drawObservationReadSchema(db *gorm.DB) (drawObservationSchema, error) {
	var rows []struct{ TableName, ColumnName string }
	if err := db.Raw("SELECT TABLE_NAME AS table_name, COLUMN_NAME AS column_name FROM information_schema.columns WHERE table_schema = DATABASE() AND TABLE_NAME IN ?", []string{"moneys", "money", "history_moneys", "odds_moneys", "pankou_moneys"}).Scan(&rows).Error; err != nil {
		return nil, err
	}
	schema := drawObservationSchema{}
	for _, row := range rows {
		if schema[row.TableName] == nil {
			schema[row.TableName] = map[string]bool{}
		}
		schema[row.TableName][row.ColumnName] = true
	}
	if !schema["moneys"]["match_id"] && !schema["money"]["match_id"] {
		return nil, fmt.Errorf("缺少可读取的主比赛表")
	}
	return schema, nil
}

func drawObservationSQLLoaders() drawObservationLoaders {
	return drawObservationLoaders{
		frontend: func(schema drawObservationSchema, targets []drawObservationTarget) (map[string]drawObservationFrontendEnrichment, error) {
			return drawObservationReadFrontend(database.DB, schema, targets)
		},
		base: func() (drawObservationBase, error) {
			base := drawObservationBase{}
			schema, err := drawObservationReadSchema(database.DB)
			if err != nil {
				return base, err
			}
			base.schema = schema
			base.targets, err = drawObservationReadTargets(database.DB, schema, nil)
			if err != nil {
				return base, err
			}
			var history []models.HistoryMoney
			if schema["history_moneys"]["match_id"] {
				if err := database.DB.Table("history_moneys").Select(drawObservationHistoryProjection(schema)).Find(&history).Error; err != nil {
					return base, err
				}
			}
			base.history = drawObservationParsedHistory(base.targets, history, time.Now().UTC())
			return base, nil
		},
		targets: func(schema drawObservationSchema, q drawObservationQuery) ([]drawObservationTarget, error) {
			return drawObservationReadTargets(database.DB, schema, &q)
		},
		// Only the selected day's market JSON is transferred. Decode it once
		// for coverage and reuse the exact same company selection on each page.
		coverage: func(schema drawObservationSchema, ids []string) (map[string]drawObservationQuotesBundle, error) {
			quotes := map[string]drawObservationQuotesBundle{}
			if len(ids) == 0 || !schema["pankou_moneys"]["match_id"] {
				return quotes, nil
			}
			var rows []models.PankouMoney
			columns := drawObservationProjection(schema, "pankou_moneys", []string{"match_id", "asia_data", "dxq_data", "bet365_asia", "bet365_dxq", "captured_at"})
			if err := database.DB.Table("pankou_moneys").Select(columns).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
				return nil, err
			}
			for _, row := range rows {
				asian, total := drawObservationLines(row)
				quotes[row.MatchId] = drawObservationQuotesBundle{asian: asian, total: total}
			}
			return quotes, nil
		},
		quotes: func(schema drawObservationSchema, ids []string) (map[string]drawObservationEuroOdds, error) {
			quotes := map[string]drawObservationEuroOdds{}
			if len(ids) == 0 || !schema["odds_moneys"]["match_id"] {
				return quotes, nil
			}
			var rows []models.OddsMoney
			columns := drawObservationProjection(schema, "odds_moneys", []string{"match_id", "data", "bet365", "william", "pinnacle", "captured_at"})
			if err := database.DB.Table("odds_moneys").Select(columns).Where("match_id IN ?", ids).Find(&rows).Error; err != nil {
				return nil, err
			}
			for _, row := range rows {
				quotes[row.MatchId] = drawObservationEuro(row)
			}
			return quotes, nil
		},
	}
}
