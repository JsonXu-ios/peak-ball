package handlers

import "encoding/json"

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
	return quote(rows[0]), quote(rows[len(rows)-1]), true
}

func sportteryRecordedBody(raw []byte) json.RawMessage {
	var payload map[string]json.RawMessage
	_ = json.Unmarshal(raw, &payload)
	return payload["sportteryOdds"]
}
