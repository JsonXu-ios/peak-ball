package drawmodel

import (
	"math"
	"sort"
	"time"
)

const halfLifeDays = 180.0
const shrinkageMatches = 8.0

type teamStrength struct{ attack, defense float64 }
type scoredGame struct {
	home, away string
	hg, ag     int
	weight     float64
}
type strengthModel struct {
	teams              map[string]teamStrength
	baseHome, baseAway float64
	games              []scoredGame
}

func decay(t, cutoff time.Time) float64 {
	days := math.Max(0, cutoff.Sub(t).Hours()/24)
	return math.Exp(-math.Ln2 * days / halfLifeDays)
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func finite(v float64) bool           { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// The two attack/defense effects are fitted to a time-weighted Poisson
// likelihood with a ridge prior. They are not manually assigned scores.
func fitStrength(history []Match, cutoff time.Time) strengthModel {
	m := strengthModel{teams: map[string]teamStrength{}, games: make([]scoredGame, 0, len(history))}
	var total, homeGoals, awayGoals float64
	for _, g := range history {
		w := decay(g.Kickoff, cutoff)
		h, a := teamKey(g.HomeTeamID, g.HomeTeamName), teamKey(g.AwayTeamID, g.AwayTeamName)
		m.teams[h], m.teams[a] = teamStrength{}, teamStrength{}
		m.games = append(m.games, scoredGame{h, a, g.HomeGoals, g.AwayGoals, w})
		total += w
		homeGoals += w * float64(g.HomeGoals)
		awayGoals += w * float64(g.AwayGoals)
	}
	if total == 0 {
		return m
	}
	m.baseHome, m.baseAway = clamp(homeGoals/total, .05, 6), clamp(awayGoals/total, .05, 6)
	for iteration := 0; iteration < 70; iteration++ {
		type derivatives struct{ ga, gd, ha, hd float64 }
		d := make(map[string]derivatives, len(m.teams))
		for key, strength := range m.teams {
			d[key] = derivatives{shrinkageMatches * strength.attack, shrinkageMatches * strength.defense, shrinkageMatches, shrinkageMatches}
		}
		for _, g := range m.games {
			lh, la := m.lambdas(g.home, g.away)
			h, a := d[g.home], d[g.away]
			h.ga += g.weight * (lh - float64(g.hg))
			h.ha += g.weight * lh
			a.gd += g.weight * (lh - float64(g.hg))
			a.hd += g.weight * lh
			a.ga += g.weight * (la - float64(g.ag))
			a.ha += g.weight * la
			h.gd += g.weight * (la - float64(g.ag))
			h.hd += g.weight * la
			d[g.home], d[g.away] = h, a
		}
		maxChange := 0.0
		for key, old := range m.teams {
			v := d[key]
			next := teamStrength{clamp(old.attack-.45*v.ga/v.ha, -1.5, 1.5), clamp(old.defense-.45*v.gd/v.hd, -1.5, 1.5)}
			maxChange = math.Max(maxChange, math.Max(math.Abs(next.attack-old.attack), math.Abs(next.defense-old.defense)))
			m.teams[key] = next
		}
		if maxChange < 1e-5 {
			break
		}
	}
	return m
}

func (m strengthModel) lambdas(home, away string) (float64, float64) {
	h, a := m.teams[home], m.teams[away]
	return clamp(m.baseHome*math.Exp(h.attack+a.defense), .05, 6), clamp(m.baseAway*math.Exp(a.attack+h.defense), .05, 6)
}

func tau(h, a int, lh, la, rho float64) float64 {
	switch {
	case h == 0 && a == 0:
		return 1 - lh*la*rho
	case h == 0 && a == 1:
		return 1 + lh*rho
	case h == 1 && a == 0:
		return 1 + la*rho
	case h == 1 && a == 1:
		return 1 - rho
	default:
		return 1
	}
}

func rhoBounds(lh, la float64) (float64, float64) {
	return math.Max(-.25, math.Max(-.99/lh, -.99/la)), math.Min(.25, math.Min(.99/(lh*la), .99))
}

// fitRho maximizes the likelihood of observed low score outcomes while
// preserving positive probabilities. Insufficient data keeps rho exactly 0.
func (m strengthModel) fitRho() (float64, bool) {
	if len(m.games) < 80 {
		return 0, false
	}
	type observation struct {
		h, a      int
		lh, la, w float64
	}
	observations := []observation{}
	lo, hi, effective := -.25, .25, 0.0
	for _, g := range m.games {
		lh, la := m.lambdas(g.home, g.away)
		l, h := rhoBounds(lh, la)
		lo, hi = math.Max(lo, l), math.Min(hi, h)
		effective += g.weight
		if g.hg <= 1 && g.ag <= 1 {
			observations = append(observations, observation{g.hg, g.ag, lh, la, g.weight})
		}
	}
	if effective < 40 || len(observations) < 30 {
		return 0, false
	}
	loss := func(rho float64) float64 {
		// Weak zero-centred prior limits pathological boundary estimates.
		value := -2 * rho * rho
		for _, o := range observations {
			value += o.w * math.Log(tau(o.h, o.a, o.lh, o.la, rho))
		}
		return value
	}
	for i := 0; i < 70; i++ {
		a, b := lo+(hi-lo)/3, hi-(hi-lo)/3
		if loss(a) < loss(b) {
			lo = a
		} else {
			hi = b
		}
	}
	return (lo + hi) / 2, true
}

func scoreDistribution(lh, la, rho float64) (float64, []Scoreline) {
	// At lambda <= 6, the omitted mass above 40 goals is negligible (<1e-18).
	const maxGoals = 40
	hp, ap := make([]float64, maxGoals+1), make([]float64, maxGoals+1)
	hp[0], ap[0] = math.Exp(-lh), math.Exp(-la)
	for n := 1; n <= maxGoals; n++ {
		hp[n] = hp[n-1] * lh / float64(n)
		ap[n] = ap[n-1] * la / float64(n)
	}
	draw, mass := 0.0, 0.0
	scores := make([]Scoreline, 0, (maxGoals+1)*(maxGoals+1))
	for h := 0; h <= maxGoals; h++ {
		for a := 0; a <= maxGoals; a++ {
			p := hp[h] * ap[a] * tau(h, a, lh, la, rho)
			mass += p
			if h == a {
				draw += p
			}
			scores = append(scores, Scoreline{h, a, p})
		}
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Probability == scores[j].Probability {
			if scores[i].Home == scores[j].Home {
				return scores[i].Away < scores[j].Away
			}
			return scores[i].Home < scores[j].Home
		}
		return scores[i].Probability > scores[j].Probability
	})
	scores = scores[:5]
	for i := range scores {
		scores[i].Probability /= mass
	}
	return clamp(draw/mass, 0, 1), scores
}
