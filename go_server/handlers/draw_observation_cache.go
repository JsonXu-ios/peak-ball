package handlers

import (
	"fmt"
	"sync"
	"time"

	"go_server/internal/drawmodel"
	"go_server/models"
)

type drawObservationSchema map[string]map[string]bool
type drawObservationCoverage struct{ asian, total bool }
type drawObservationQuotesBundle struct {
	asian, total drawObservationLine
	euro         drawObservationEuroOdds
}
type drawObservationBase struct {
	targets []drawObservationTarget
	history []drawHistoricalGame
	schema  drawObservationSchema
}

// Loaders perform only SELECTs. Keeping them injectable lets cache behavior be
// tested without connecting to a database or starting an HTTP server.
type drawObservationLoaders struct {
	base     func() (drawObservationBase, error)
	targets  func(drawObservationSchema, drawObservationQuery) ([]drawObservationTarget, error)
	coverage func(drawObservationSchema, []string) (map[string]drawObservationQuotesBundle, error)
	quotes   func(drawObservationSchema, []string) (map[string]drawObservationEuroOdds, error)
	frontend func(drawObservationSchema, []drawObservationTarget) (map[string]drawObservationFrontendEnrichment, error)
}

type drawObservationDay struct {
	targets  []drawObservationTarget
	coverage map[string]drawObservationCoverage
}
type drawObservationDayFlight struct {
	done chan struct{}
	day  *drawObservationDay
	err  error
}
type drawObservationCore struct {
	base             drawObservationBase
	index            drawObservationIndex
	summary          drawObservationSummary
	sportterySummary drawObservationSummary
	authority        map[string]drawObservationTarget
	windowMu         sync.Mutex
	windows          map[string]drawObservationWindows
	dayMu            sync.Mutex
	days             map[string]*drawObservationDayFlight
	quoteMu          sync.Mutex
	lineCache        map[string]drawObservationQuotesBundle
	euroCache        map[string]drawObservationEuroOdds
	frontendMu       sync.Mutex
	frontendCache    map[string]drawObservationFrontendEnrichment
}
type drawObservationCoreFlight struct {
	done chan struct{}
	core *drawObservationCore
	err  error
}
type drawObservationCache struct {
	mu      sync.Mutex
	core    *drawObservationCore
	expires time.Time
	flight  *drawObservationCoreFlight
	loaders drawObservationLoaders
	ttl     time.Duration
	now     func() time.Time
}

func newDrawObservationCache(loaders drawObservationLoaders, ttl time.Duration, now func() time.Time) *drawObservationCache {
	return &drawObservationCache{loaders: loaders, ttl: ttl, now: now}
}

var drawObservationReads = newDrawObservationCache(drawObservationSQLLoaders(), 30*time.Second, time.Now)

func (cache *drawObservationCache) get(force bool) (*drawObservationCore, error) {
	cache.mu.Lock()
	// Concurrent cold loads and refreshes join one in-flight rebuild. A forced
	// refresh replaces the entire generation, including day/window/quote data.
	if cache.flight != nil {
		flight := cache.flight
		cache.mu.Unlock()
		<-flight.done
		return flight.core, flight.err
	}
	if !force && cache.core != nil && cache.now().Before(cache.expires) {
		core := cache.core
		cache.mu.Unlock()
		return core, nil
	}
	cache.core = nil
	flight := &drawObservationCoreFlight{done: make(chan struct{})}
	cache.flight = flight
	cache.mu.Unlock()
	base, err := cache.loaders.base()
	var core *drawObservationCore
	if err == nil {
		core = &drawObservationCore{base: base, index: indexDrawObservationCache(base.history), authority: map[string]drawObservationTarget{}, windows: map[string]drawObservationWindows{}, days: map[string]*drawObservationDayFlight{}, lineCache: map[string]drawObservationQuotesBundle{}, euroCache: map[string]drawObservationEuroOdds{}, frontendCache: map[string]drawObservationFrontendEnrichment{}}
		core.summary.HistoricalMatches = len(base.targets)
		for _, target := range base.targets {
			core.authority[target.Match.MatchId] = target
			isSporttery := drawObservationScopeMatches(target.Match, "sporttery")
			if isSporttery {
				core.sportterySummary.HistoricalMatches++
			}
			if hg, ag, settled := drawRegulationResult(target.Match); settled && hg == ag {
				core.summary.DrawMatches++
				if isSporttery {
					core.sportterySummary.DrawMatches++
				}
			}
		}
	}
	cache.mu.Lock()
	flight.core, flight.err = core, err
	if err == nil {
		cache.core, cache.expires = core, cache.now().Add(cache.ttl)
	}
	cache.flight = nil
	close(flight.done)
	cache.mu.Unlock()
	return core, err
}

func (core *drawObservationCore) window(target drawmodel.Match) drawObservationWindows {
	key := fmt.Sprintf("%s|%d|%s|%s|%s|%s", target.ID, target.Kickoff.UnixNano(), target.HomeTeamID, target.AwayTeamID, target.HomeTeamName, target.AwayTeamName)
	core.windowMu.Lock()
	defer core.windowMu.Unlock()
	if window, ok := core.windows[key]; ok {
		return window
	}
	window := core.index.windows(target)
	core.windows[key] = window
	return window
}

func (core *drawObservationCore) day(q drawObservationQuery, loaders drawObservationLoaders) (*drawObservationDay, error) {
	key := q.date + "|" + q.start + "|" + q.end
	core.dayMu.Lock()
	if flight := core.days[key]; flight != nil {
		core.dayMu.Unlock()
		<-flight.done
		return flight.day, flight.err
	}
	flight := &drawObservationDayFlight{done: make(chan struct{})}
	core.days[key] = flight
	core.dayMu.Unlock()
	// This date pool is deliberately shared by scopes. Resolve current/legacy
	// authority before any scope predicate, then filter the immutable all pool.
	loadQuery := q
	loadQuery.scope = "all"
	targets, err := loaders.targets(core.base.schema, loadQuery)
	var day *drawObservationDay
	if err == nil {
		// A legacy row from the selected day cannot resurrect a current-table
		// match that has been moved to another day or invalidated there.
		filtered := make([]drawObservationTarget, 0, len(targets))
		ids := []string{}
		for _, target := range targets {
			if authority, exists := core.authority[target.Match.MatchId]; exists && !drawObservationDateMatches(drawObservationDate(authority.Match), q) {
				continue
			}
			filtered = append(filtered, target)
			ids = append(ids, target.Match.MatchId)
		}
		markets, loadErr := loaders.coverage(core.base.schema, ids)
		err = loadErr
		if err == nil {
			coverage := map[string]drawObservationCoverage{}
			core.quoteMu.Lock()
			for _, id := range ids {
				quote, exists := markets[id]
				if !exists {
					quote = drawObservationEmptyQuotes()
				}
				core.lineCache[id] = quote
				coverage[id] = drawObservationCoverage{asian: quote.asian.Source != "missing", total: quote.total.Source != "missing"}
			}
			core.quoteMu.Unlock()
			day = &drawObservationDay{targets: filtered, coverage: coverage}
		}
	}
	core.dayMu.Lock()
	flight.day, flight.err = day, err
	if err != nil {
		delete(core.days, key)
	}
	close(flight.done)
	core.dayMu.Unlock()
	return day, err
}

func (core *drawObservationCore) quotes(ids []string, loaders drawObservationLoaders) (map[string]drawObservationQuotesBundle, error) {
	core.quoteMu.Lock()
	defer core.quoteMu.Unlock()
	missing := []string{}
	for _, id := range ids {
		if _, ok := core.euroCache[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		loaded, err := loaders.quotes(core.base.schema, missing)
		if err != nil {
			return nil, err
		}
		for _, id := range missing {
			quote, ok := loaded[id]
			if !ok {
				quote = drawObservationEuro(models.OddsMoney{})
			}
			core.euroCache[id] = quote
		}
	}
	result := make(map[string]drawObservationQuotesBundle, len(ids))
	for _, id := range ids {
		quote, exists := core.lineCache[id]
		if !exists {
			quote = drawObservationEmptyQuotes()
		}
		quote.euro = core.euroCache[id]
		result[id] = quote
	}
	return result, nil
}

func (cache *drawObservationCache) read(q drawObservationQuery) (drawObservationResponse, error) {
	q = normalizeDrawObservationQuery(q)
	core, err := cache.get(q.refresh)
	if err != nil {
		return drawObservationResponse{}, err
	}
	day, err := core.day(q, cache.loaders)
	if err != nil {
		return drawObservationResponse{}, err
	}
	summary := core.summary
	if q.scope == "sporttery" {
		summary = core.sportterySummary
	}
	response := buildDrawObservations(drawObservationData{Targets: day.targets, coverage: day.coverage, summary: &summary, window: core.window}, q)
	ids := make([]string, 0, len(response.Items))
	for _, item := range response.Items {
		ids = append(ids, item.MatchID)
	}
	frontend, err := core.frontend(ids, day.targets, cache.loaders)
	if err != nil {
		return drawObservationResponse{}, err
	}
	quotes, err := core.quotes(ids, cache.loaders)
	if err != nil {
		return drawObservationResponse{}, err
	}
	for i := range response.Items {
		quote := quotes[response.Items[i].MatchID]
		response.Items[i].AsianHandicap, response.Items[i].GoalsLine, response.Items[i].EuroOdds = quote.asian, quote.total, quote.euro
		if value, exists := frontend[response.Items[i].MatchID]; exists {
			response.Items[i].FrontendAnalysis = value.analysis
			response.Items[i].CompanyQuotes = value.companies
		}
	}
	return response, nil
}

func drawObservationEmptyQuotes() drawObservationQuotesBundle {
	asian, total := drawObservationLines(models.PankouMoney{})
	return drawObservationQuotesBundle{asian: asian, total: total, euro: drawObservationEuro(models.OddsMoney{})}
}
