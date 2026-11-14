package handlers

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func drawEligibilityFixture() drawObservationItem {
	return drawObservationItem{
		MatchID: "target-eligibility", JingcaiID: "20261106001",
		MatchTime: time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC),
		Home:      "Home", Guest: "Away",
	}
}

func drawEligibilityPast(row drawObservationItem, id string, offset time.Duration) drawObservationRecentMatch {
	return drawObservationRecentMatch{
		MatchID: id, MatchTime: row.MatchTime.Add(-offset),
		Home: row.Home, Guest: row.Guest, Venue: "home", HomeScore: 1, AwayScore: 1, Result: "draw",
	}
}

func drawEligibilityHistory(row drawObservationItem, sample int) drawObservationRecent {
	history := drawObservationRecent{Sample: sample, Draws: sample, Matches: []drawObservationRecentMatch{}}
	for i := 0; i < sample; i++ {
		history.Matches = append(history.Matches, drawEligibilityPast(row, row.MatchID+"-h2h-"+strconv.Itoa(i), time.Duration(i+1)*24*time.Hour))
	}
	return history
}

func TestDrawObservationHeadToHeadSampleUsesSafeRecordsNotSummary(t *testing.T) {
	row := drawEligibilityFixture()
	row.HeadToHead.Sample, row.HeadToHead.Draws = 999, 999
	if got := drawObservationHeadToHeadSample(row); got != 0 {
		t.Fatalf("unverified summary created %d head-to-head records", got)
	}
	valid := drawEligibilityPast(row, "past", 24*time.Hour)
	away := drawEligibilityPast(row, "past-away", 48*time.Hour)
	away.Home, away.Guest, away.Venue = row.Guest, row.Home, "away"
	away.HomeScore, away.AwayScore, away.Result = 2, 1, "loss"
	blankID := drawEligibilityPast(row, "", 72*time.Hour)
	boundary := drawEligibilityPast(row, "exact-completion-boundary", 2*time.Hour)
	justBefore := drawEligibilityPast(row, "finished-one-nanosecond-before", 2*time.Hour+time.Nanosecond)
	current := drawEligibilityPast(row, row.MatchID, 96*time.Hour)
	future := drawEligibilityPast(row, "future", -24*time.Hour)
	missingTime := drawEligibilityPast(row, "missing-time", 24*time.Hour)
	missingTime.MatchTime = time.Time{}
	badVenue := drawEligibilityPast(row, "invalid-venue", 24*time.Hour)
	badVenue.Venue = "neutral"
	badHome := drawEligibilityPast(row, "negative-home", 24*time.Hour)
	badHome.HomeScore = -1
	badAway := drawEligibilityPast(row, "negative-away", 24*time.Hour)
	badAway.AwayScore = -1
	badResult := drawEligibilityPast(row, "invalid-result", 24*time.Hour)
	badResult.Result = ""
	wrongAwayResult := away
	wrongAwayResult.MatchID, wrongAwayResult.Result = "wrong-away-result", "win"
	invalidFirst := valid
	invalidFirst.HomeScore = -1
	row.HeadToHead.Matches = []drawObservationRecentMatch{
		invalidFirst, valid, valid, away, blankID, blankID,
		boundary, justBefore, current, future, missingTime, badVenue, badHome, badAway, badResult, wrongAwayResult,
	}
	before := append([]drawObservationRecentMatch(nil), row.HeadToHead.Matches...)
	if got := drawObservationHeadToHeadSample(row); got != 4 {
		t.Fatalf("safe distinct records=%d, want 4", got)
	}
	if !reflect.DeepEqual(row.HeadToHead.Matches, before) {
		t.Fatal("sample validation mutated input records")
	}
	row.MatchTime = time.Time{}
	if got := drawObservationHeadToHeadSample(row); got != 0 {
		t.Fatalf("missing target kickoff created %d records", got)
	}
}

func TestDrawObservationSelectionEligibilityPreservesRawSignalAndTargetBlindness(t *testing.T) {
	row := drawEligibilityFixture()
	score := .35068304387324195
	raw := &drawObservationFrozenSignal{ModelVersion: "unchanged-numeric-model", State: "selected", Score: &score, Threshold: .2016984853563883, ObservedInputs: 190, UsedInputs: 202}
	before := *raw
	got := drawObservationApplySelectionEligibility(row, raw)
	if got == nil || got == raw || got.State != "not_selected" || got.SelectionVersion != drawObservationSelectionVersion || got.Score == nil || *got.Score != score || !strings.Contains(got.Reason, "交锋") {
		t.Fatalf("missing real head-to-head did not produce a copied exclusion: %+v", got)
	}
	if !reflect.DeepEqual(*raw, before) || raw.State != "selected" || raw.ModelVersion != got.ModelVersion || raw.Threshold != got.Threshold || raw.ObservedInputs != got.ObservedInputs || raw.UsedInputs != got.UsedInputs {
		t.Fatalf("eligibility changed frozen numeric evidence: before=%+v raw=%+v got=%+v", before, raw, got)
	}
	for _, scoreline := range [][2]int{{0, 0}, {9, 0}, {0, 8}, {2, 2}} {
		changed := row
		changed.HomeScore, changed.AwayScore, changed.Settled, changed.Status = scoreline[0], scoreline[1], true, 999
		confirmed := true
		changed.RegulationSettled = &confirmed
		if after := drawObservationApplySelectionEligibility(changed, raw); !reflect.DeepEqual(got, after) {
			t.Fatalf("current target score affected eligibility: %+v -> %+v", got, after)
		}
	}
	row.HeadToHead.Matches = []drawObservationRecentMatch{drawEligibilityPast(row, "one-real-meeting", 24*time.Hour)}
	row.HeadToHead.Sample = 0 // validated records, not the summary, control eligibility
	if after := drawObservationApplySelectionEligibility(row, raw); after.State != "selected" || after.Score == nil || *after.Score != score || after.SelectionVersion != drawObservationSelectionVersion {
		t.Fatalf("one real meeting did not retain the original selection: %+v", after)
	}
	for _, state := range []string{"not_selected", "unavailable", "out_of_scope"} {
		copy := *raw
		copy.State, copy.Reason = state, "original reason"
		missing := drawEligibilityFixture()
		if after := drawObservationApplySelectionEligibility(missing, &copy); after.State != state || after.Reason != copy.Reason || after.Score == nil || *after.Score != score {
			t.Fatalf("eligibility overwrote preexisting %s state: %+v", state, after)
		}
	}
	row.JingcaiID = " \t "
	if after := drawObservationApplySelectionEligibility(row, raw); !reflect.DeepEqual(after, raw) {
		t.Fatalf("non-JC calculation changed: raw=%+v got=%+v", raw, after)
	}
	if drawObservationApplySelectionEligibility(drawEligibilityFixture(), nil) != nil {
		t.Fatal("nil raw model was replaced with a fabricated signal")
	}
}

// Minimal immutable projection of the 2026-11-05/06 report's original nine
// selections. It freezes original numeric scores and source H2H records only;
// this test neither needs the report files nor reads a database.
func TestDrawObservationSelectionEligibilityRealTwoDayNineSelections(t *testing.T) {
	cases := []struct {
		id, kickoff string
		score       float64
		home, away  int
	}{
		{"498235499", "2026-11-05T04:00:00+08:00", .26696921674148266, 1, 9},
		{"498235500", "2026-11-05T04:00:00+08:00", .3043401912722917, 4, 1},
		{"498235501", "2026-11-05T04:00:00+08:00", .21133532698785534, 2, 1},
		{"498235502", "2026-11-05T04:00:00+08:00", .35068304387324195, 1, 2},
		{"498235484", "2026-11-06T01:45:00+08:00", .2800138828854881, 1, 1},
		{"498235485", "2026-11-06T01:45:00+08:00", .20296502373486278, 1, 3},
		{"498235497", "2026-11-06T01:45:00+08:00", .33964852111980676, 1, 1},
		{"498235554", "2026-11-06T04:00:00+08:00", .39318485028585937, 1, 2},
		{"498235555", "2026-11-06T04:00:00+08:00", .2384065668412874, 1, 2},
	}
	var selected []string
	for _, tc := range cases {
		row := drawEligibilityFixture()
		row.MatchID = tc.id
		var err error
		row.MatchTime, err = time.Parse(time.RFC3339, tc.kickoff)
		if err != nil {
			t.Fatal(err)
		}
		row.HomeScore, row.AwayScore = tc.home, tc.away
		if tc.id == "498235502" {
			past, err := time.Parse(time.RFC3339, "2024-07-13T19:00:00+08:00")
			if err != nil {
				t.Fatal(err)
			}
			row.HeadToHead = drawObservationRecent{Sample: 1, Draws: 1, Matches: []drawObservationRecentMatch{{MatchID: "483479083", MatchTime: past, Home: "埃因霍温", Guest: "布鲁日", Venue: "home", HomeScore: 1, AwayScore: 1, Result: "draw"}}}
		}
		raw := &drawObservationFrozenSignal{State: "selected", Score: drawMetricScore(tc.score)}
		got := drawObservationApplySelectionEligibility(row, raw)
		if got.Score == nil || *got.Score != tc.score || raw.State != "selected" {
			t.Fatalf("original numeric signal changed for %s", tc.id)
		}
		if got.State == "selected" {
			selected = append(selected, tc.id)
		}
	}
	if !reflect.DeepEqual(selected, []string{"498235502"}) {
		t.Fatalf("two-day eligible selections=%v, want only the one fixture with prior H2H", selected)
	}
}

func TestDrawObservationSelectionEligibilitySurvivesVerifiedResearchRead(t *testing.T) {
	row, input, now := drawResearchFakeInput()
	// Existing numeric model may use summary counts; policy must independently
	// reject an original selected signal without one revalidated actual record.
	row.HeadToHead.Matches = nil
	_, body, key, err := prepareDrawResearchInput(row, input, now)
	if err != nil {
		t.Fatal(err)
	}
	raw := drawObservationFrozenModelSignal(row, input)
	gated := drawObservationApplySelectionEligibility(row, raw)
	if raw.State != "selected" || gated.State != "not_selected" {
		t.Fatalf("test does not exercise high-score eligibility rejection: raw=%+v gated=%+v", raw, gated)
	}
	backend := &drawResearchFakeBackend{record: drawResearchFakeRecord(key, body, gated, now)}
	legacyCalls := 0
	archived, decision := resolveDrawResearchSignalWithBackend(context.Background(), backend, row, input, func() *drawObservationFrozenSignal {
		legacyCalls++
		return raw
	}, now)
	if decision.Source != "research" || legacyCalls != 0 || backend.writes != 0 || archived.State != "not_selected" || archived.SelectionVersion != drawObservationSelectionVersion {
		t.Fatalf("test did not exercise verified current-policy research read: signal=%+v decision=%+v legacy=%d", archived, decision, legacyCalls)
	}
	got := drawObservationApplySelectionEligibility(row, archived)
	if got.State != "not_selected" || got.Score == nil || *got.Score != *raw.Score || !reflect.DeepEqual(got, archived) || raw.State != "selected" {
		t.Fatalf("research source bypassed policy or changed numeric model: raw=%+v archived=%+v got=%+v", raw, archived, got)
	}
}

func TestDrawObservationSelectionLegacyDayUpgradesOnce(t *testing.T) {
	fake := newDrawObservationsFake()
	fake.targets[0].Match.JingcaiID = "20261021001"
	store := newDrawResultMemoryStore()
	reader := newDrawObservationResultReader(store, drawResultTestSource, fake.loaders())
	q := drawObservationQuery{date: "2026-10-21", mode: "today", scope: "all", page: 1, size: 50}
	source, err := drawResultTestSource(q)
	if err != nil {
		t.Fatal(err)
	}
	day, err := reader.buildDay(q, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if !drawObservationSelectionStoredDayReady(*day) {
		t.Fatal("newly generated day has no current selection contract")
	}
	day.SelectionVersion = ""
	for i := range day.Items {
		if day.Items[i].FrozenDrawSignal != nil {
			day.Items[i].FrozenDrawSignal.SelectionVersion = ""
		}
	}
	key := source.Namespace + ":day:" + q.date
	totalsCacheStoreDay(t, store, key, *day)
	if old, err := reader.storedDay(key, q.date, source.Day); err != nil || old != nil {
		t.Fatalf("pre-policy JC day was reused: day=%v err=%v", old != nil, err)
	}
	baseBefore, targetsBefore := fake.baseCalls.Load(), fake.targetCalls.Load()
	response, err := reader.read(q)
	if err != nil {
		t.Fatal(err)
	}
	if fake.baseCalls.Load() != baseBefore || fake.targetCalls.Load() != targetsBefore+1 {
		t.Fatalf("upgrade must reuse parsed base and rebuild one day: base %d->%d targets %d->%d", baseBefore, fake.baseCalls.Load(), targetsBefore, fake.targetCalls.Load())
	}
	for _, row := range response.Items {
		if strings.TrimSpace(row.JingcaiID) == "" {
			continue
		}
		if row.FrozenDrawSignal == nil || row.FrozenDrawSignal.SelectionVersion != drawObservationSelectionVersion {
			t.Fatalf("rebuilt JC row lacks current selection policy: %+v", row.FrozenDrawSignal)
		}
		if row.FrozenDrawSignal.State == "selected" && drawObservationHeadToHeadSample(row) < drawObservationMinimumHeadToHead {
			t.Fatal("rebuilt JC day retained an ineligible selection")
		}
	}
	ready, err := reader.storedDay(key, q.date, source.Day)
	if err != nil || ready == nil || !drawObservationSelectionStoredDayReady(*ready) {
		t.Fatalf("upgraded day cannot be reused: day=%v err=%v", ready != nil, err)
	}
	targetsAfter := fake.targetCalls.Load()
	if _, err := reader.read(q); err != nil || fake.targetCalls.Load() != targetsAfter {
		t.Fatalf("policy-ready day rebuilt again: calls %d->%d err=%v", targetsAfter, fake.targetCalls.Load(), err)
	}
}

func TestDrawObservationSelectionStoredDayReadinessValidatesPolicyAndScope(t *testing.T) {
	if drawObservationMinimumHeadToHead != 1 {
		t.Fatal("serving policy must require exactly one or more valid prior meetings")
	}
	makeDay := func() drawObservationStoredDay {
		row := drawEligibilityFixture()
		row.HeadToHead = drawEligibilityHistory(row, 1)
		row.FrozenDrawSignal = drawObservationApplySelectionEligibility(row, &drawObservationFrozenSignal{State: "selected", Score: drawMetricScore(.3)})
		return drawObservationStoredDay{SelectionVersion: drawObservationSelectionVersion, Items: []drawObservationItem{row}}
	}
	cases := []struct {
		name   string
		mutate func(*drawObservationStoredDay)
		want   bool
	}{
		{"current selected with one safe meeting", func(*drawObservationStoredDay) {}, true},
		{"old day marker", func(d *drawObservationStoredDay) { d.SelectionVersion = "previous" }, false},
		{"missing original signal", func(d *drawObservationStoredDay) { d.Items[0].FrozenDrawSignal = nil }, false},
		{"old row policy marker", func(d *drawObservationStoredDay) { d.Items[0].FrozenDrawSignal.SelectionVersion = "" }, false},
		{"different minimum", func(d *drawObservationStoredDay) { d.Items[0].FrozenDrawSignal.MinimumHeadToHead = 2 }, false},
		{"stale sample summary in signal", func(d *drawObservationStoredDay) { d.Items[0].FrozenDrawSignal.HeadToHeadSample = 99 }, false},
		{"removed source record invalidates stamp", func(d *drawObservationStoredDay) { d.Items[0].HeadToHead.Matches = nil }, false},
		{"stamped selected without records still invalid", func(d *drawObservationStoredDay) {
			d.Items[0].HeadToHead.Matches = nil
			d.Items[0].FrozenDrawSignal.HeadToHeadSample = 0
		}, false},
		{"completed exclusion without records reusable", func(d *drawObservationStoredDay) {
			d.Items[0].HeadToHead.Matches = nil
			d.Items[0].FrozenDrawSignal = drawObservationApplySelectionEligibility(d.Items[0], d.Items[0].FrozenDrawSignal)
		}, true},
		{"unavailable calculation is completed", func(d *drawObservationStoredDay) {
			d.Items[0].FrozenDrawSignal = drawObservationApplySelectionEligibility(d.Items[0], &drawObservationFrozenSignal{State: "unavailable"})
		}, true},
		{"legacy non-JC unaffected", func(d *drawObservationStoredDay) {
			d.SelectionVersion = ""
			d.Items[0].JingcaiID = ""
			d.Items[0].FrozenDrawSignal = nil
		}, true},
		{"whitespace JC identity out of scope", func(d *drawObservationStoredDay) {
			d.SelectionVersion = "old"
			d.Items[0].JingcaiID = " \t "
			d.Items[0].FrozenDrawSignal = nil
		}, true},
		{"empty day needs no upgrade", func(d *drawObservationStoredDay) {
			*d = drawObservationStoredDay{}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			day := makeDay()
			tc.mutate(&day)
			if got := drawObservationSelectionStoredDayReady(day); got != tc.want {
				t.Fatalf("policy-ready=%v, want %v: %+v", got, tc.want, day)
			}
		})
	}
}
