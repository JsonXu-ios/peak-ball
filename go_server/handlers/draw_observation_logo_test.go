package handlers

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"go_server/models"
)

func TestDrawObservationLogosFollowExactMatchAndCurrentTable(t *testing.T) {
	kickoff := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	current := drawObservationMain(drawObservationGame("logo-match", "1", "2", kickoff, 1, 1)).Match
	current.HomeLogo, current.GuestLogo = "/footballimg/current-home", "https://cdn.vipc.cn/vipc-sport/image/current-away"
	legacy := current
	legacy.HomeLogo, legacy.GuestLogo = "/footballimg/old-home", "/footballimg/old-away"
	other := current
	other.MatchId, other.HomeLogo, other.GuestLogo = "same-team-names", "/footballimg/other-home", "/footballimg/other-away"
	response := buildDrawObservations(drawObservationData{
		Targets: mergeDrawObservationTargets([]models.Money{current, other}, []models.Money{legacy}),
	}, drawObservationQuery{mode: "today", date: drawObservationDate(current), page: 1, size: 50})
	if len(response.Items) != 2 {
		t.Fatalf("expected two distinct matches, got %+v", response.Items)
	}
	logos := map[string][2]string{}
	for _, row := range response.Items {
		logos[row.MatchID] = [2]string{row.HomeLogo, row.GuestLogo}
	}
	want := map[string][2]string{
		"logo-match":      {current.HomeLogo, current.GuestLogo},
		"same-team-names": {other.HomeLogo, other.GuestLogo},
	}
	if !reflect.DeepEqual(logos, want) {
		t.Fatalf("logos mixed across teams, fixtures or legacy tables: got=%v want=%v", logos, want)
	}
	// The API and durable day share this JSON representation; preserve each
	// source path exactly and let the client use the existing image proxy.
	encoded, err := json.Marshal(drawObservationStoredDay{Items: response.Items})
	if err != nil {
		t.Fatal(err)
	}
	var restored drawObservationStoredDay
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.Items, response.Items) {
		t.Fatal("durable JSON round trip changed logo or match fields")
	}
}

func TestDrawObservationLogoProjectionAndRevisionTrackBothSourceTables(t *testing.T) {
	for _, table := range []string{"moneys", "money"} {
		schema := drawObservationSchema{table: {"match_id": true, "date": true, "home_logo": true, "guest_logo": true, "updated_at": true}}
		columns := drawObservationProjection(schema, table, drawObservationHeaderColumns)
		if !reflect.DeepEqual(columns, []string{"match_id", "date", "home_logo", "guest_logo"}) {
			t.Fatalf("%s header omitted source logos: %v", table, columns)
		}
		for _, global := range []bool{false, true} {
			fields := strings.Join(drawObservationRevisionFields(schema, table, global), ",")
			if !strings.Contains(fields, "`home_logo`") || !strings.Contains(fields, "`guest_logo`") {
				t.Fatalf("%s global=%v signature can reuse results without logos: %s", table, global, fields)
			}
		}
		delete(schema[table], "home_logo")
		delete(schema[table], "guest_logo")
		if got := drawObservationProjection(schema, table, drawObservationHeaderColumns); !reflect.DeepEqual(got, []string{"match_id", "date"}) {
			t.Fatalf("%s legacy schema selected missing logo columns: %v", table, got)
		}
	}
}

func TestDrawObservationMissingLogosStayAbsent(t *testing.T) {
	encoded, err := json.Marshal(drawObservationItem{MatchID: "no-logo"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "homeLogo") || strings.Contains(string(encoded), "guestLogo") {
		t.Fatal("missing source logos must not create fabricated image URLs")
	}
}
