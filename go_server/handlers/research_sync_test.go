package handlers

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"go_server/internal/drawmodeldata"
)

func TestResearchInputExcludesTargetLabelsButRetainsPriorScores(t *testing.T) {
	row := drawFeatureStudyRow{
		MatchID: "498177797", Date: "2026-10-29",
		MatchTime: time.Date(2026, 10, 29, 15, 0, 0, 0, time.UTC),
		Home:      "home", Guest: "away", JingcaiID: "test",
		Result: 1, HomeScore: 2, AwayScore: 2,
		Opening: drawFeatureStudyPhase{William: []float64{2, 3.2, 3.8}, Sporttery: []float64{1.9, 3.1, 3.7}, Average: []float64{2.1, 3.3, 3.9}},
		PreMatchHistory: &drawFeatureStudyPreMatchHistory{HomeRecent: drawObservationRecent{
			Sample: 1, Draws: 1,
			Matches: []drawObservationRecentMatch{{MatchID: "prior", MatchTime: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC), HomeScore: 1, AwayScore: 1, Venue: "home", Result: "draw"}},
		}},
	}
	first, err := researchInput(row)
	if err != nil {
		t.Fatal(err)
	}
	mutated := row
	mutated.Result, mutated.HomeScore, mutated.AwayScore = 2, 0, 9
	second, err := researchInput(mutated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("target outcome changed archived feature input")
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(first, &input); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"result", "homeScore", "awayScore", "status", "settled", "predictionHit"} {
		if _, ok := input[key]; ok {
			t.Fatalf("target label remained in input: %s", key)
		}
	}
	if !bytes.Contains(input["preMatchHistory"], []byte(`"homeScore":1`)) {
		t.Fatal("historical goal labels must remain valid explanatory inputs")
	}
	f1, err := drawmodeldata.ExtractFeatures(first)
	if err != nil {
		t.Fatal(err)
	}
	f2, err := drawmodeldata.ExtractFeatures(second)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f1, f2) {
		t.Fatal("target result mutation changed numeric features")
	}
}

func TestResearchSourceIDsAndJSONNumbersRemainExact(t *testing.T) {
	for _, id := range []string{"498177797", "9007199254740993", "000123"} {
		got := researchSourceValue("moneys", "match_id", []byte(id))
		text, ok := got.(string)
		if !ok || text != id {
			t.Fatalf("source ID %q was rounded or converted: %#v", id, got)
		}
	}
	text := researchSourceValue("moneys", "home", []byte(`{"name":"literal"}`))
	if _, ok := text.(string); !ok {
		t.Fatal("ordinary text column was treated as structured JSON")
	}
	got := researchSourceValue("odds_moneys", "sporttery_trade", []byte(`{"matchId":9007199254740993,"zero":0,"missing":null}`))
	object, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("known JSON column not decoded: %#v", got)
	}
	id, ok := object["matchId"].(json.Number)
	if !ok || id.String() != "9007199254740993" {
		t.Fatalf("JSON ID lost integer precision: %#v", object["matchId"])
	}
	if object["missing"] != nil || object["zero"].(json.Number).String() != "0" {
		t.Fatal("JSON zero and missing values were conflated")
	}
	malformed := []byte(`{"bad":`)
	if got := researchSourceValue("history_moneys", "against_list", malformed); got != string(malformed) {
		t.Fatal("malformed source evidence was discarded or repaired silently")
	}
}

func TestResearchCatalogueCoversProductionContractWithoutDuplicateNames(t *testing.T) {
	definitions, err := researchDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 530 {
		t.Fatalf("research dictionary count: %d", len(definitions))
	}
	names := map[string]bool{}
	for _, definition := range definitions {
		if definition.Name == "" || names[definition.Name] {
			t.Fatalf("empty or duplicate definition: %s", definition.Name)
		}
		names[definition.Name] = true
		if definition.CodeHash != drawmodeldata.FeatureCodeSHA256() || definition.Description == "" || definition.Formula == "" {
			t.Fatalf("definition not reproducible: %s", definition.Name)
		}
	}
	for _, feature := range drawmodeldata.Features() {
		if !names[feature.Name] {
			t.Fatalf("production feature missing from dictionary: %s", feature.Name)
		}
	}
}

func TestResearchFeatureSnapshotPreservesUnknownZeroAndSampleGuards(t *testing.T) {
	features := drawmodeldata.Features()
	var zeroName, guardedName string
	for _, feature := range features {
		if strings.HasPrefix(feature.Name, "difference.") && len(feature.Guards) == 0 && zeroName == "" {
			zeroName = feature.Name
		}
		if len(feature.Guards) > 0 && guardedName == "" {
			guardedName = feature.Name
		}
	}
	if zeroName == "" || guardedName == "" {
		t.Fatal("required independent test cases absent from model contract")
	}
	values, missing := researchFeatureValues(map[string]float64{zeroName: 0, guardedName: 123})
	var decoded map[string]*float64
	if err := json.Unmarshal(values, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[zeroName] == nil || *decoded[zeroName] != 0 {
		t.Fatal("observed real zero was converted to missing")
	}
	if value, ok := decoded[guardedName]; !ok || value != nil {
		t.Fatalf("insufficient-history feature was imputed into archive: %s", guardedName)
	}
	definitions, err := researchDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range definitions {
		if _, ok := decoded[definition.Name]; !ok {
			t.Fatalf("snapshot schema omitted unknown feature instead of null: %s", definition.Name)
		}
	}
	var unknown []string
	if err := json.Unmarshal(missing, &unknown); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, name := range unknown {
		seen[name] = true
	}
	if seen[zeroName] || !seen[guardedName] {
		t.Fatal("missing mask disagrees with archived values")
	}
}
