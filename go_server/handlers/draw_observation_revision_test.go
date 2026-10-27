package handlers

import (
	"strings"
	"testing"
)

func TestDrawObservationsRevisionTracksConsumedContentInsteadOfCrawlerSaveTimestamp(t *testing.T) {
	schema := drawObservationSchema{
		"moneys":         {"id": true, "match_id": true, "date": true, "home_score": true, "home_rank": true, "updated_at": true, "unused_live_field": true},
		"history_moneys": {"id": true, "match_id": true, "recent_home_list": true, "recent_guest_list": true, "against_list": true, "league_stat": true, "against_summary": true, "updated_at": true, "future_home": true},
		"odds_moneys":    {"match_id": true, "updated_at": true, "data": true},
	}
	main := strings.Join(drawObservationRevisionFields(schema, "moneys", true), ",")
	if strings.Contains(main, "updated_at") || strings.Contains(main, "unused_live_field") || strings.Contains(main, "home_rank") || !strings.Contains(main, "`home_score`") || !strings.Contains(main, "content_sum") {
		t.Fatalf("global main signature follows non-base inputs: %s", main)
	}
	dayMain := strings.Join(drawObservationRevisionFields(schema, "moneys", false), ",")
	if !strings.Contains(dayMain, "`home_rank`") || strings.Contains(dayMain, "updated_at") {
		t.Fatalf("day signature omitted consumed frontend rank or follows save timestamp: %s", dayMain)
	}
	history := strings.Join(drawObservationRevisionFields(schema, "history_moneys", true), ",")
	if strings.Contains(history, "updated_at") || strings.Contains(history, "against_summary") || strings.Contains(history, "future_home") || !strings.Contains(history, "CASE WHEN") || !strings.Contains(history, "ELSE `league_stat`") || strings.Contains(history, "`CASE WHEN") {
		t.Fatalf("global history signature differs from selective base projection: %s", history)
	}
	dayHistory := strings.Join(drawObservationRevisionFields(schema, "history_moneys", false), ",")
	if !strings.Contains(dayHistory, "`against_summary`") || !strings.Contains(dayHistory, "`league_stat`") || strings.Contains(dayHistory, "updated_at") {
		t.Fatalf("day history signature omitted frontend inputs: %s", dayHistory)
	}
	quotes := strings.Join(drawObservationRevisionFields(schema, "odds_moneys", false), ",")
	if !strings.Contains(quotes, "updated_at") {
		t.Fatal("indexed day-scoped quote revision lost update detection")
	}
}
