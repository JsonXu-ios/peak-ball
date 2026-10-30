package handlers

import "time"

type drawObservationResearchTakeover struct {
	Version     string `json:"version"`
	Mode        string `json:"mode"`
	Served      int    `json:"served"`
	Persisted   int    `json:"persisted"`
	Fallback    int    `json:"fallback"`
	Unavailable int    `json:"unavailable"`
	Reason      string `json:"reason,omitempty"`
}

func emptyDrawResearchTakeover() *drawObservationResearchTakeover {
	return &drawObservationResearchTakeover{Version: drawResearchTakeoverVersion, Mode: "research", Reason: "本页暂无可接管的竞彩模型结果"}
}

// Counts describe the returned page, never an independently filtered window.
func buildDrawResearchTakeover(items []drawObservationItem) *drawObservationResearchTakeover {
	r := &drawObservationResearchTakeover{Version: drawResearchTakeoverVersion, Mode: "research"}
	present := false
	for _, row := range items {
		d := row.ResearchDecision
		if d == nil {
			continue
		}
		present = true
		if row.FrozenDrawSignal == nil || row.FrozenDrawSignal.State == "unavailable" {
			r.Unavailable++
		}
		switch d.Source {
		case "research":
			r.Served++
		case "research_write_through":
			r.Persisted++
		case "legacy_fallback":
			r.Fallback++
		}
	}
	if !present {
		return nil
	}
	if r.Fallback > 0 {
		r.Mode = "mixed"
		r.Reason = "部分比赛暂回退原计算链路，原入选结果继续显示"
		if r.Served+r.Persisted == 0 {
			r.Mode = "fallback"
		}
	}
	if r.Served+r.Persisted+r.Fallback == 0 {
		r.Reason = "本页暂无可接管的竞彩模型结果"
	}
	return r
}

// Old durable days are upgraded individually while their parsed history/base
// remains reusable. A degraded day retries after a short recovery interval.
func drawResearchStoredDayReady(day drawObservationStoredDay, now time.Time) bool {
	if day.TakeoverVersion != drawResearchTakeoverVersion {
		return false
	}
	for _, row := range day.Items {
		if row.ResearchDecision == nil || row.ResearchDecision.Version != drawResearchTakeoverVersion {
			return false
		}
		if row.SecondaryDecision == nil || row.SecondaryDecision.Version != drawObservationSecondaryVersion || row.ScoreRecommendation == nil || row.ScoreRecommendation.Version != drawScoreRecommendationVersion {
			return false
		}
		if row.ResearchDecision.Source == "legacy_fallback" && now.Sub(day.GeneratedAt) > 30*time.Second {
			return false
		}
	}
	return true
}
