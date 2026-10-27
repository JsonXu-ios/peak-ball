package handlers

import (
	"encoding/json"
	"testing"
	"time"

	"go_server/models"
)

func TestDrawObservationsSerializeRegulationSettlementSeparatelyFromCompletion(t *testing.T) {
	date := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	fixtures := []struct {
		match   models.Money
		settled bool
		regular bool
	}{
		{match: models.Money{MatchId: "ordinary", DisplayState: "完场", HomeScore: 1, GuestScore: 1}, settled: true, regular: true},
		{match: models.Money{MatchId: "overtime", DisplayState: "完场", HomeScore: 1, GuestScore: 1, HomeOtScore: 2, GuestOtScore: 1}, settled: true},
		{match: models.Money{MatchId: "penalties", DisplayState: "完场", HomeScore: 2, GuestScore: 2, HomeOtPenalty: 3, GuestOtPenalty: 5}, settled: true},
		{match: models.Money{MatchId: "scheduled", DisplayState: "未开始"}},
	}
	targets := make([]drawObservationTarget, 0, len(fixtures))
	for _, fixture := range fixtures {
		match := fixture.match
		match.Date, match.MatchTime = date, date.Add(12*time.Hour)
		targets = append(targets, drawObservationTarget{Match: match})
	}
	response := buildDrawObservations(drawObservationData{Targets: targets}, drawObservationQuery{mode: "today", date: "2026-10-20", page: 1, size: 50})
	body, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != len(fixtures) {
		t.Fatalf("wanted all fixture states in today response: %s", body)
	}
	byID := map[string]map[string]json.RawMessage{}
	for _, item := range payload.Items {
		var id string
		if err := json.Unmarshal(item["matchId"], &id); err != nil {
			t.Fatal(err)
		}
		byID[id] = item
	}
	for _, fixture := range fixtures {
		item := byID[fixture.match.MatchId]
		var settled, regulationSettled bool
		if err := json.Unmarshal(item["settled"], &settled); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(item["regulationSettled"], &regulationSettled); err != nil {
			t.Fatalf("newly built %s must explicitly serialize regulation settlement: %v", fixture.match.MatchId, err)
		}
		if settled != fixture.settled || regulationSettled != fixture.regular {
			t.Errorf("%s: settled=%v regulationSettled=%v; expected %v / %v", fixture.match.MatchId, settled, regulationSettled, fixture.settled, fixture.regular)
		}
	}
}

func TestDrawObservationsLegacyRegulationSettlementStaysUnknown(t *testing.T) {
	var legacy drawObservationItem
	if err := json.Unmarshal([]byte(`{"matchId":"legacy","settled":true}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.RegulationSettled != nil {
		t.Fatal("legacy completion must not imply confirmed regulation settlement")
	}
	body, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["regulationSettled"]; exists {
		t.Fatal("legacy unknown settlement metadata must remain omitted")
	}
}
