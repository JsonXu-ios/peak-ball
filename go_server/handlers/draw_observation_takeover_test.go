package handlers

import (
	"testing"
	"time"
)

func TestResearchTakeoverStoredDayUpgradeAndRecovery(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	old := drawObservationStoredDay{GeneratedAt: now, Items: []drawObservationItem{{MatchID: "a"}}}
	if drawResearchStoredDayReady(old, now) {
		t.Fatal("old cached model-only day bypassed takeover")
	}
	old.TakeoverVersion = drawResearchTakeoverVersion
	if drawResearchStoredDayReady(old, now) {
		t.Fatal("a partly upgraded cached day bypassed takeover")
	}
	old.Items[0].ResearchDecision = &drawObservationResearchDecision{Version: drawResearchTakeoverVersion, Source: "legacy_fallback"}
	if drawResearchStoredDayReady(old, now) {
		t.Fatal("legacy secondary/score fields bypassed dynamic upgrade")
	}
	old.Items[0].SecondaryDecision = &drawObservationSecondarySignal{Version: drawObservationSecondaryVersion}
	old.Items[0].ScoreRecommendation = &drawScoreRecommendation{Version: drawScoreRecommendationVersion}
	if !drawResearchStoredDayReady(old, now.Add(10*time.Second)) {
		t.Fatal("outage fallback was repeatedly regenerated")
	}
	if drawResearchStoredDayReady(old, now.Add(31*time.Second)) {
		t.Fatal("degraded cache can never recover")
	}
	old.Items[0].ResearchDecision.Source = "research"
	if !drawResearchStoredDayReady(old, now.Add(365*24*time.Hour)) {
		t.Fatal("durable healthy day was discarded by wall clock")
	}
}

func TestResearchTakeoverPageCountsDoNotDoubleCountWrites(t *testing.T) {
	score := .3
	items := []drawObservationItem{}
	for _, source := range []string{"research", "research_write_through", "legacy_fallback", "not_applicable"} {
		items = append(items, drawObservationItem{ResearchDecision: &drawObservationResearchDecision{Version: drawResearchTakeoverVersion, Source: source}, FrozenDrawSignal: &drawObservationFrozenSignal{State: "selected", Score: &score}})
	}
	s := buildDrawResearchTakeover(items)
	if s.Mode != "mixed" || s.Served != 1 || s.Persisted != 1 || s.Fallback != 1 {
		t.Fatalf("incorrect page counters: %#v", s)
	}
	if buildDrawResearchTakeover(nil) != nil {
		t.Fatal("old response was falsely identified as library-backed")
	}
}

func TestResearchRankedScoreMetricsUseActualChoices(t *testing.T) {
	m := researchMetric{}
	m.addScoreResult([]drawRecommendedScore{{Home: 2, Away: 2}, {Home: 1, Away: 1}}, 2, 2)
	m.addScoreResult([]drawRecommendedScore{{Home: 1, Away: 1}, {Home: 3, Away: 3}}, 3, 3)
	m.addScoreResult(nil, 0, 0)
	m.rates()
	if m.ScoreCandidates != 2 || m.FirstScoreHits != 1 || m.ScoreHits != 2 || m.FirstScoreHitRate == nil || *m.FirstScoreHitRate != .5 || m.ScoreHitRate == nil || *m.ScoreHitRate != 1 {
		t.Fatalf("rank metrics still hard-coded baseline scores: %#v", m)
	}
	empty := researchMetric{}
	empty.rates()
	if empty.FirstScoreHitRate != nil || empty.ScoreHitRate != nil {
		t.Fatal("no denominator must stay unknown")
	}
}
