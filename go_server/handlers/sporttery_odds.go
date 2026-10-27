package handlers

import (
	"encoding/json"
	"math"
	"strings"
)

// sportteryOdds is kept alongside the original lr payload, in the existing
// sporttery_trade JSON column. The provider's matchId belongs to another ID
// system; association is always by the match ID used in the requested URL.
func sportteryRecordedPrices(raw json.RawMessage) (opening, latest []*string, present bool) {
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Spf struct {
				Odds []map[string]json.RawMessage `json:"odds"`
			} `json:"spf"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Code != 0 || len(payload.Data.Spf.Odds) == 0 {
		return nil, nil, false
	}
	quote := func(row map[string]json.RawMessage) []*string {
		return []*string{drawObservationRawLine(row["spf3"]), drawObservationRawLine(row["spf1"]), drawObservationRawLine(row["spf0"])}
	}
	rows := payload.Data.Spf.Odds
	opening = make([]*string, 3)
	for _, row := range rows {
		prices := quote(row)
		valid := true
		for _, raw := range prices {
			if raw == nil {
				valid = false
				break
			}
			value := parseFloat(*raw)
			if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				valid = false
				break
			}
		}
		if valid {
			opening = prices
			break
		}
	}
	return opening, quote(rows[len(rows)-1]), true
}

func sportteryRecordedBody(raw []byte) json.RawMessage {
	var payload map[string]json.RawMessage
	_ = json.Unmarshal(raw, &payload)
	return payload["sportteryOdds"]
}

func sportteryRecordedOpeningStatus(raw json.RawMessage) string {
	if opening, _, present := sportteryRecordedPrices(raw); present {
		if opening[0] != nil {
			return "available"
		}
		return "unopened"
	}
	var payload struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Code != nil && *payload.Code == 0 && len(payload.Data) > 0 {
		if strings.TrimSpace(string(payload.Data)) == "null" {
			return "no_quotes"
		}
		var data map[string]json.RawMessage
		if json.Unmarshal(payload.Data, &data) == nil {
			return "no_quotes"
		}
	}
	return "not_collected"
}
