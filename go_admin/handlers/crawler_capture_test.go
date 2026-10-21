package handlers

import (
	"testing"
	"time"
)

func TestCrawlerPayloadMetadataCapturesOnlyOriginalSourceBodies(t *testing.T) {
	now := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		table, column string
		captured      bool
	}{
		{"history_moneys", "league_stat", true},
		{"odds_moneys", "data", true},
		{"pankou_moneys", "asia_data", true},
		{"history_moneys", "rank_data", false},
		{"history_moneys", "rank", false},
		{"odds_moneys", "sporttery_trade", false},
	} {
		t.Run(test.table+"."+test.column, func(t *testing.T) {
			metadata := crawlerPayloadMetadata(test.table, test.column, false, now)
			if _, exists := metadata["captured_at"]; exists != test.captured {
				t.Fatalf("captured_at present=%v, want %v", exists, test.captured)
			}
			if updated, ok := metadata["updated_at"].(time.Time); !ok || !updated.Equal(now) {
				t.Fatal("each successful body write must record its actual update time")
			}
			if _, exists := metadata["created_at"]; exists {
				t.Fatal("updating an existing record must preserve its created_at")
			}
		})
	}
}

func TestCrawlerTradeRefreshCannotAdvanceQuoteCaptureOrFillUnknownCapture(t *testing.T) {
	oldCapture := time.Date(2026, 10, 19, 8, 0, 0, 0, time.UTC)
	now := oldCapture.Add(24 * time.Hour)
	for _, hasOriginalCapture := range []bool{true, false} {
		row := map[string]interface{}{"updated_at": oldCapture}
		if hasOriginalCapture {
			row["captured_at"] = oldCapture
		}
		for column, value := range crawlerPayloadMetadata("odds_moneys", "sporttery_trade", false, now) {
			row[column] = value
		}
		if captured, exists := row["captured_at"]; exists != hasOriginalCapture || (exists && captured != oldCapture) {
			t.Fatalf("trade refresh changed/forged quote capture: %+v", row)
		}
		if row["updated_at"] != now {
			t.Fatal("trade update time should still reflect the completed write")
		}
	}
}

func TestCrawlerInsertRecordsCreationWithoutFabricatingSecondaryQuoteAge(t *testing.T) {
	now := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	metadata := crawlerPayloadMetadata("odds_moneys", "sporttery_trade", true, now)
	if metadata["created_at"] != now || metadata["updated_at"] != now {
		t.Fatal("first insert must record creation and update times")
	}
	if _, exists := metadata["captured_at"]; exists {
		t.Fatal("first trade-only insert does not prove any odds body was captured")
	}
}
