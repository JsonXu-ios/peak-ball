package drawmodeldata

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

const marketEpsilon = 1e-9

var marketOutcomes = []string{"home", "draw", "away"}
var marketKellyID = regexp.MustCompile(`(?i)(?:欧赔\s*)?ID\s*[:：]?\s*(\d+)`)

// Python 3.13's built-in sum(float sequence) uses compensated summation.
// Reproducing that arithmetic matters: the fitted histogram thresholds can
// equal an observed overround exactly, so a one-ulp shift changes a tree path.
// Do not replace this with a blanket threshold tolerance.
func marketSum(values ...float64) float64 {
	high, low := 0.0, 0.0
	for _, value := range values {
		total := high + value
		if math.Abs(high) >= math.Abs(value) {
			low += (high - total) + value
		} else {
			low += (value - total) + high
		}
		high = total
	}
	return high + low
}

func marketTrue(v interface{}) bool { value, ok := v.(bool); return ok && value }

func marketString(v interface{}) string { value, _ := v.(string); return value }

func marketValidSource(value interface{}, rejectHistory bool) bool {
	if value == nil {
		return false
	}
	text, ok := value.(string)
	return !ok || (text != "" && text != "missing" && (!rejectHistory || text != "history_moneys"))
}

func marketPositive(value interface{}) (float64, bool) {
	x, ok := featureNumber(value)
	return x, ok && x > 0
}

func marketLine(value interface{}, goals bool) (float64, bool) {
	var result float64
	if text, ok := value.(string); ok {
		text = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(text), " ", ""), "／", "/")
		if text == "" {
			return 0, false
		}
		negative := strings.HasPrefix(text, "受")
		if strings.HasPrefix(text, "受让") {
			text = strings.TrimPrefix(text, "受让")
		} else if negative {
			text = strings.TrimPrefix(text, "受")
		}
		words := map[string]float64{"平手": 0, "平": 0, "半球": .5, "半": .5, "一球": 1, "一": 1,
			"球半": 1.5, "一球半": 1.5, "两球": 2, "二球": 2, "两": 2, "两球半": 2.5, "二球半": 2.5,
			"三球": 3, "三": 3, "三球半": 3.5, "四球": 4, "四": 4, "四球半": 4.5, "五球": 5,
			"五球半": 5.5, "六球": 6, "六球半": 6.5, "七球": 7}
		parts := strings.Split(text, "/")
		if len(parts) > 2 {
			return 0, false
		}
		for _, part := range parts {
			number, known := words[part]
			if !known {
				var valid bool
				number, valid = featureNumber(part)
				if !valid {
					return 0, false
				}
			}
			result += number
		}
		result /= float64(len(parts))
		if negative {
			result = -math.Abs(result)
		}
	} else {
		var ok bool
		result, ok = featureNumber(value)
		if !ok {
			return 0, false
		}
	}
	return result, finite(result) && math.Abs(result) <= 15 && (!goals || result > 0)
}

func marketSourceID(descriptions ...interface{}) string {
	ids := map[string]bool{}
	for _, raw := range descriptions {
		match := marketKellyID.FindStringSubmatch(marketString(raw))
		if len(match) == 2 {
			ids[match[1]] = true
		}
	}
	if len(ids) == 1 {
		for id := range ids {
			return id
		}
	}
	return ""
}

func marketDifferencePairs(out map[string]float64, phase, comparison string, difference []float64) {
	absolute := []float64{math.Abs(difference[0]), math.Abs(difference[1]), math.Abs(difference[2])}
	sorted := append([]float64(nil), absolute...)
	sort.Float64s(sorted)
	for _, rank := range []string{"absLargestTwo", "absSmallestTwo"} {
		members, count := [3]bool{}, 0
		for i, value := range absolute {
			members[i] = value >= sorted[1]-marketEpsilon
			if rank == "absSmallestTwo" {
				members[i] = value <= sorted[1]+marketEpsilon
			}
			if members[i] {
				count++
			}
		}
		prefix := "conflict.difference." + phase + "." + comparison + "." + rank + "."
		for name, pair := range map[string][2]int{"home_draw": {0, 1}, "draw_away": {1, 2}, "home_away": {0, 2}} {
			value := 0.0
			if count == 2 && members[pair[0]] && members[pair[1]] {
				value = 1
			}
			putFeature(out, prefix+name, value)
		}
		ambiguous := 0.0
		if count != 2 {
			ambiguous = 1
		}
		putFeature(out, prefix+"ambiguous", ambiguous)
	}
}

// extractMarketFeatures follows the frozen Python whitelist exactly. It does
// not reuse newer page fallbacks, borrow another company, or repair explicitly
// missing source fields. Raw numeric absence is left absent for the imputer.
func extractMarketFeatures(row map[string]interface{}, out map[string]float64) {
	frontend, inputs := rawObject(row["frontend"]), rawObject(row["analysisInputs"])
	quotes := map[string]map[string]interface{}{}
	for _, raw := range rawArray(row["companyQuotes"]) {
		quote := rawObject(raw)
		quotes[marketString(quote["key"])] = quote
	}
	prices, probabilities, margins := map[string][]float64{}, map[string][]float64{}, map[string]float64{}
	for _, company := range []string{"sporttery", "william", "bet365", "average"} {
		for _, phase := range []string{"opening", "latest"} {
			var raw interface{}
			if company == "average" {
				inputKey := "averageLatest"
				if phase == "opening" {
					inputKey = "averageOpening"
				}
				var present bool
				raw, present = inputs[inputKey]
				if !present {
					raw = rawPath(row, phase, "average")
				}
			} else {
				euro := rawObject(quotes[company]["euroOdds"])
				if marketValidSource(euro["source"], false) {
					raw = euro[phase]
				}
				if len(euro) == 0 && company != "bet365" {
					raw = rawPath(row, phase, company)
				}
			}
			price := featureTriple(raw, true)
			if len(price) != 3 {
				continue
			}
			key, prefix := company+"."+phase, "euro."+company+"."+phase
			prices[key] = price
			inverse := []float64{1 / price[0], 1 / price[1], 1 / price[2]}
			total := marketSum(inverse...)
			prob := []float64{inverse[0] / total, inverse[1] / total, inverse[2] / total}
			probabilities[key], margins[key] = prob, total-1
			entropyTerms := make([]float64, 3)
			for i, outcome := range marketOutcomes {
				putFeature(out, prefix+".odds."+outcome, price[i])
				putFeature(out, prefix+".prob."+outcome, prob[i])
				entropyTerms[i] = prob[i] * math.Log(prob[i])
			}
			putFeature(out, prefix+".overround", total-1)
			putFeature(out, prefix+".entropy", -marketSum(entropyTerms...))
			putFeature(out, prefix+".sideGap", math.Abs(prob[0]-prob[2]))
			putFeature(out, prefix+".drawVsStrongSide", prob[1]-math.Max(prob[0], prob[2]))
		}
		op, la := prices[company+".opening"], prices[company+".latest"]
		if len(op) == 3 && len(la) == 3 {
			po, pl := probabilities[company+".opening"], probabilities[company+".latest"]
			for i, outcome := range marketOutcomes {
				putFeature(out, "euro."+company+".change.oddsChange."+outcome, la[i]-op[i])
				putFeature(out, "euro."+company+".change.relativeChange."+outcome, la[i]/op[i]-1)
				putFeature(out, "euro."+company+".change.probChange."+outcome, pl[i]-po[i])
			}
			putFeature(out, "euro."+company+".change.overround", margins[company+".latest"]-margins[company+".opening"])
		}
	}
	for _, phase := range []string{"opening", "latest"} {
		for _, comparison := range []string{"sporttery", "average"} {
			w, c := prices["william."+phase], prices[comparison+"."+phase]
			if len(w) != 3 || len(c) != 3 {
				continue
			}
			diff, absolute := []float64{}, []float64{}
			prefix := "difference." + phase + "." + comparison + "."
			for i, outcome := range marketOutcomes {
				diff = append(diff, w[i]-c[i])
				absolute = append(absolute, math.Abs(w[i]-c[i]))
				putFeature(out, prefix+outcome, diff[i])
			}
			sorted := append([]float64(nil), absolute...)
			sort.Float64s(sorted)
			largest, topTwo := absolute[1] >= sorted[2]-marketEpsilon, absolute[1] >= sorted[1]-marketEpsilon
			rank := 1.0
			for _, value := range absolute {
				if value > absolute[1]+marketEpsilon {
					rank++
				}
			}
			putFeature(out, prefix+"drawAbsRank", rank)
			for name, yes := range map[string]bool{"drawAbsMaximum": largest, "negativeTopTwo": !largest && topTwo && diff[1] < -marketEpsilon, "positiveTopTwo": !largest && topTwo && diff[1] > marketEpsilon} {
				value := 0.0
				if yes {
					value = 1
				}
				putFeature(out, prefix+name, value)
			}
			marketDifferencePairs(out, phase, comparison, diff)
		}
	}
	marketExtractKelly(row, frontend, out)
	marketExtractLines(row, quotes, out)
	marketExtractSourceProfit(row, frontend, inputs, out)
	if heat, ok := featureNumber(row["homeHeat"]); ok && heat >= 0 && heat <= 100 {
		putFeature(out, "heat.asianHome", heat)
	}
}

func marketExtractKelly(row, frontend map[string]interface{}, out map[string]float64) {
	values, sources := map[string][]float64{}, map[string]string{}
	for _, phase := range []string{"opening", "latest"} {
		candidates := []map[string]interface{}{}
		for _, value := range rawArray(frontend["comparisonStages"]) {
			stage := rawObject(value)
			if stage["key"] == phase {
				candidates = append(candidates, stage)
			}
		}
		metric := map[string]interface{}{}
		if len(candidates) == 1 {
			metric = rawObject(candidates[0]["kelly"])
		}
		var raw interface{}
		if marketTrue(metric["available"]) {
			raw = metric["values"]
		} else if len(metric) == 0 {
			raw = rawPath(row, phase, "kelly")
		}
		values[phase] = featureTriple(raw, true)
		sources[phase] = marketSourceID(metric["basis"], rawPath(row, phase, "kellySource"))
		k := values[phase]
		if len(k) != 3 {
			continue
		}
		minimum, maximum := math.Min(k[0], math.Min(k[1], k[2])), math.Max(k[0], math.Max(k[1], k[2]))
		for i, outcome := range marketOutcomes {
			putFeature(out, "kelly."+phase+"."+outcome, k[i])
			value := 0.0
			if k[i] <= minimum+marketEpsilon {
				value = 1
			}
			putFeature(out, "kelly."+phase+".minimum."+outcome, value)
		}
		putFeature(out, "kelly."+phase+".spread", maximum-minimum)
	}
	if sources["opening"] != "" && sources["opening"] == sources["latest"] && len(values["opening"]) == 3 && len(values["latest"]) == 3 {
		for i, outcome := range marketOutcomes {
			putFeature(out, "kelly.change."+outcome, values["latest"][i]-values["opening"][i])
		}
	}
}

func marketExtractLines(row map[string]interface{}, quotes map[string]map[string]interface{}, out map[string]float64) {
	for _, company := range []string{"william", "bet365", "selected"} {
		for _, name := range []string{"asianHandicap", "goalsLine"} {
			block := rawObject(quotes[company][name])
			if company == "selected" {
				block = rawObject(row[name])
				if len(block) == 0 {
					block = rawObject(rawPath(row, "observation", name))
				}
				if len(block) == 0 {
					block = map[string]interface{}{"source": "strict_export", "opening": row["asianOpening"], "latest": row["asianLine"]}
					if name == "goalsLine" {
						block["opening"], block["latest"] = rawPath(row, "goalsHeat", "openingLine"), rawPath(row, "goalsHeat", "latestLine")
					}
				}
			}
			if !marketValidSource(block["source"], true) {
				continue
			}
			components := map[string]map[string]float64{}
			for _, phase := range []string{"opening", "latest"} {
				prefix := "line." + company + "." + name + "." + phase + "."
				values := map[string]float64{}
				if line, ok := marketLine(block[phase], name == "goalsLine"); ok {
					values["line"] = line
				}
				water := rawArray(block[phase+"Odds"])
				if len(water) == 2 {
					if value, ok := marketPositive(water[0]); ok {
						values["water1"] = value
					}
					if value, ok := marketPositive(water[1]); ok {
						values["water2"] = value
					}
				}
				w1, ok1 := values["water1"]
				w2, ok2 := values["water2"]
				if ok1 && ok2 {
					values["waterSpread"] = w1 - w2
				}
				for key, value := range values {
					putFeature(out, prefix+key, value)
				}
				components[phase] = values
			}
			for _, key := range []string{"line", "water1", "water2"} {
				a, okA := components["opening"][key]
				b, okB := components["latest"][key]
				if okA && okB {
					putFeature(out, "line."+company+"."+name+".change."+key, b-a)
				}
			}
		}
	}
}

func marketExtractSourceProfit(row, frontend, inputs map[string]interface{}, out map[string]float64) {
	simulation := rawObject(frontend["roiSimulation"])
	if len(simulation) == 0 {
		simulation = rawObject(row["roiSimulation"])
	}
	distributionSources, trade := rawObject(inputs["distributionSources"]), rawObject(inputs["tradeFields"])
	for _, company := range []string{"sporttery", "william", "bet365"} {
		candidates := []map[string]interface{}{}
		for _, raw := range rawArray(simulation["markets"]) {
			market := rawObject(raw)
			if market["key"] == company && !marketTrue(market["simulated"]) {
				candidates = append(candidates, market)
			}
		}
		if len(candidates) != 1 || !marketTrue(candidates[0]["oddsAvailable"]) {
			continue
		}
		market := candidates[0]
		distribution := marketString(distributionSources[marketString(market["key"])])
		if distribution != "tzbl" && distribution != "jyykSpf" {
			continue
		}
		support := featureTriple([]interface{}{trade[distribution+".hsupportRate"], trade[distribution+".dsupportRate"], trade[distribution+".asupportRate"]}, false)
		if len(support) != 3 || support[0] < 0 || support[0] > 100 || support[1] < 0 || support[1] > 100 || support[2] < 0 || support[2] > 100 || support[0]+support[1]+support[2] <= 0 {
			continue
		}
		outcomes := make([]map[string]interface{}, 3)
		valid := true
		for i, outcome := range marketOutcomes {
			matching := []map[string]interface{}{}
			for _, raw := range rawArray(market["bookmakerByOutcome"]) {
				detail := rawObject(raw)
				if detail["outcome"] == outcome {
					matching = append(matching, detail)
				}
			}
			if len(matching) != 1 || !marketTrue(matching[0]["available"]) {
				valid = false
				break
			}
			if _, ok := marketPositive(matching[0]["odds"]); !ok {
				valid = false
				break
			}
			outcomes[i] = matching[0]
		}
		if !valid {
			continue
		}
		for i, outcome := range marketOutcomes {
			profit, okProfit := featureNumber(outcomes[i]["bookmakerProfit"])
			stake, okStake := marketPositive(outcomes[i]["totalStake"])
			if okProfit && okStake {
				putFeature(out, "marketProfit."+company+".roi."+outcome, profit/stake*100)
			}
		}
	}
	tradePaths := []string{"tzbl.hsupportRate", "tzbl.dsupportRate", "tzbl.asupportRate", "tzbl.hprobability", "tzbl.dprobability", "tzbl.aprobability", "tzbl.herror", "tzbl.derror", "tzbl.aerror", "tzbl.psyError", "jyykSpf.hsupportRate", "jyykSpf.dsupportRate", "jyykSpf.asupportRate", "jyykSpf.hy", "jyykSpf.dy", "jyykSpf.ay", "jyykSpf.hlr", "jyykSpf.dlr", "jyykSpf.alr"}
	for _, path := range tradePaths {
		if value, ok := featureNumber(trade[path]); ok {
			putFeature(out, "trade."+path, value)
		}
	}
}
