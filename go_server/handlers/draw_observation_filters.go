package handlers

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type drawObservationStatusOption struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

// Source display states are open-ended; an unfamiliar state remains selectable
// by its exact label instead of being guessed from a numeric status code.
func normalizeDrawObservationMatchStatus(value string) (string, error) {
	invalid := func() (string, error) {
		return "", fmt.Errorf("match_status 应为 all、finished、unfinished、unknown 或 state:比赛原始状态")
	}
	if !utf8.ValidString(value) {
		return invalid()
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return invalid()
		}
	}
	value = strings.TrimSpace(value)
	switch value {
	case "all", "finished", "unfinished", "unknown":
		return value, nil
	}
	if strings.HasPrefix(value, "state:") {
		state := strings.TrimSpace(strings.TrimPrefix(value, "state:"))
		if state != "" && utf8.RuneCountInString(state) <= 100 {
			return "state:" + state, nil
		}
	}
	return invalid()
}

// Enumerate the whole day before display filters and pagination, so selecting
// one status never removes the other available source states from the menu.
func drawObservationMatchStatusOptions(items []drawObservationItem, scope string) []drawObservationStatusOption {
	unique := map[string]string{}
	for _, row := range items {
		if scope == "sporttery" && strings.TrimSpace(row.JingcaiID) == "" {
			continue
		}
		state := strings.TrimSpace(row.DisplayState)
		switch {
		case row.Settled:
			unique["finished"] = "完赛"
		case state == "":
			unique["unknown"] = "状态未知"
		default:
			if value, err := normalizeDrawObservationMatchStatus("state:" + state); err == nil {
				unique[value] = state
			}
		}
	}
	options := make([]drawObservationStatusOption, 0, len(unique))
	for value, title := range unique {
		options = append(options, drawObservationStatusOption{Title: title, Value: value})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Title == options[j].Title {
			return options[i].Value < options[j].Value
		}
		return options[i].Title < options[j].Title
	})
	return options
}

// Apply display-only status predicates to the complete materialized day. They
// do not change model inputs, source revisions or the metric window.
func drawObservationListStatusMatches(row drawObservationItem, q drawObservationQuery) bool {
	if q.matchStatus == "finished" && !row.Settled {
		return false
	}
	if q.matchStatus == "unfinished" && row.Settled {
		return false
	}
	if q.matchStatus == "unknown" && (row.Settled || strings.TrimSpace(row.DisplayState) != "") {
		return false
	}
	if strings.HasPrefix(q.matchStatus, "state:") && strings.TrimSpace(row.DisplayState) != strings.TrimPrefix(q.matchStatus, "state:") {
		return false
	}
	switch q.selection {
	case "sporttery":
		return strings.TrimSpace(row.JingcaiID) != "" && row.FrozenDrawSignal != nil && row.FrozenDrawSignal.State == "selected"
	case "non_sporttery":
		return strings.TrimSpace(row.JingcaiID) == "" && row.NonSportterySignal != nil && row.NonSportterySignal.State == "selected"
	default:
		return true
	}
}
