package main

import (
	"encoding/json"
	"testing"
)

func TestMergeSportteryBodiesKeepsIndependentSourceOnFailedFetch(t *testing.T) {
	previous := []byte(`{"data":{"tzbl":{"h":2}},"sportteryOdds":{"code":0,"data":{"spf":{"odds":[{"spf3":1.89,"spf1":3.55,"spf0":3.14}]}}}}`)
	for _, fresh := range [][]byte{nil, []byte(`{"data":{"tzbl":{"h":2.03}}}`)} {
		var merged map[string]json.RawMessage
		if err := json.Unmarshal(mergeSportteryBodies(previous, fresh, nil), &merged); err != nil {
			t.Fatal(err)
		}
		if len(merged["sportteryOdds"]) == 0 || len(merged["data"]) == 0 {
			t.Fatal("updating trade erased the official price history")
		}
	}
	if got := mergeSportteryBodies(nil, nil, nil); got != nil {
		t.Fatalf("empty inputs fabricated a capture: %s", got)
	}
}

func TestSportteryUnopenedCaptureDoesNotErasePricedHistory(t *testing.T) {
	priced := []byte(`{"code":0,"data":{"spf":{"odds":[{"spf3":1.55,"spf1":3.85,"spf0":4.45}]}}}`)
	unopened := []byte(`{"code":0,"data":{"spf":{"odds":[{"spf3":0,"spf1":0,"spf0":0}]}}}`)
	var payload map[string]json.RawMessage
	previous, _ := json.Marshal(map[string]json.RawMessage{"sportteryOdds": priced})
	if err := json.Unmarshal(mergeSportteryBodies(previous, nil, unopened), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := sportteryPriceState(payload["sportteryOdds"]); !ok {
		t.Fatal("unopened response erased actual opening history")
	}
	if err := json.Unmarshal(mergeSportteryBodies(nil, nil, unopened), &payload); err != nil {
		t.Fatal(err)
	}
	if records, prices := sportteryPriceState(payload["sportteryOdds"]); !records || prices {
		t.Fatal("unopened source was lost or turned into real prices")
	}
}
