// Package drawfullmodel is an independent experiment. It never changes the
// frozen drawmodeldata contract or the original list decisions.
package drawfullmodel

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go_server/internal/drawmodeldata"
)

const InputVersion = "draw-comprehensive-input-v1"

type FeatureMetadata struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Module    string `json:"module"`
	Source    string `json:"source"`
	Synthetic bool   `json:"synthetic,omitempty"`
	Derived   bool   `json:"derived,omitempty"`
}

var fullOutcomes = []string{"home", "draw", "away"}
var fullMarketKeys = []string{"sporttery", "sportterySim", "sportteryRqspf", "sportteryRqspfSim", "william", "bet365", "localAverageOdds"}
var fullNumericPaths = map[string][]string{
	"summary":  {"historyGoalAverage", "recentGoalAverage", "combinedGoalAverage", "combinedHandicapAverage", "homeRank", "guestRank", "historyGoalDifference", "recentGoalDifference", "goalBalanceValue"},
	"goddess":  {"homeScore", "guestScore", "probabilities.home", "probabilities.draw", "probabilities.away"},
	"platform": {"bookmaker.goal.total", "platform.goal.total", "warningAdjusted.goal.total", "drawRisk.score", "goals.under.home", "goals.under.guest", "goals.main.home", "goals.main.guest", "goals.over.home", "goals.over.guest"},
	"evil":     {"line", "prediction.underTotalValue", "prediction.overTotalValue", "prediction.underGoalLine", "prediction.overGoalLine", "prediction.secondOverScore", "prediction.secondUnderScore", "prediction.mainTotal", "prediction.secondaryTotalValue", "prediction.goalLine", "prediction.secondaryGoalLine", "scores.over", "scores.under", "scores.overPercent", "scores.underPercent"},
}
var fullDirectionPaths = map[string][]string{
	"summary":  {"prediction", "qiuPrediction", "legacyKelly", "legacyTicai"},
	"goddess":  {"prediction"},
	"platform": {"bookmaker.outcome", "platform.outcome", "warningAdjusted.outcome", "professionalConsensus", "sportteryComfort", "rqspfComfort"},
	"evil":     {"prediction.underOutcome", "prediction.overOutcome", "prediction.outcome", "prediction.secondaryOutcome", "prediction.firstDirection", "prediction.goalDirection", "prediction.secondaryGoalDirection"},
}
var fullScorePaths = map[string][]string{
	"platform": {"bookmaker.score", "bookmaker.secondaryScore", "platform.score", "platform.secondaryScore", "warningAdjusted.score", "warningAdjusted.secondaryScore"},
	"evil":     {"prediction.underScore", "prediction.overScore", "prediction.score", "prediction.secondaryScore"},
}
var fullWarningPatterns = []struct{ key, text string }{
	{"overheated", "投注热度过热"}, {"profitAligned", "交易盈亏同向"}, {"lossAligned", "庄家同向亏损"},
	{"handicapCorrection", "平台已纳入让球修正"}, {"heatCorrection", "平台已纳入让球热度修正"},
	{"lossCorrection", "平台已纳入庄家同向亏损修正"}, {"goalOverHeat", "大小球热度偏大"},
	{"goalUnderHeat", "大小球热度偏小"}, {"goalReversion", "回归小球警示"}, {"hiddenLine", "盘口隐藏提醒"},
	{"professionalConflict", "专业信号"}, {"deepHandicap", "盘口偏深"}, {"zeroGoals", "零进球"},
}

func fullBaseModule(name string) string {
	switch strings.Split(name, ".")[0] {
	case "euro":
		return "odds"
	case "difference":
		return "differences"
	case "kelly":
		return "kelly"
	case "line":
		return "lines"
	case "safeRecent":
		return "recent"
	case "safeH2H":
		return "h2h"
	case "marketProfit", "trade":
		return "trade"
	case "heat":
		return "heat"
	case "context":
		return "context"
	default:
		return "conflicts"
	}
}

func fullChineseLabel(name, module string) string {
	titles := map[string]string{"summary": "前台综合值", "goddess": "上帝的女人（合成）", "platform": "庄家与平台", "evil": "邪修", "recent": "近期进失球", "heat": "投注热度", "retail": "散户心理", "profits": "盈亏测算", "trade": "源站原始数据", "warnings": "前台警示", "odds": "赔率概率", "availability": "数据可用性"}
	title := titles[module]
	if title == "" {
		title = module
	}
	path := strings.TrimPrefix(name, "full."+module+".")
	replace := strings.NewReplacer("sportteryRqspfSim", "竞彩让球模拟", "sportteryRqspf", "竞彩让球", "sportterySim", "竞彩模拟", "sporttery", "竞彩", "localAverageOdds", "本地均赔", "bet365", "Bet365", "william", "威廉", "warningAdjusted", "警示修正", "bookmaker", "庄家", "platform", "平台", "secondaryScore", "次选比分", "secondPassReversed", "二推反向", "secondPassForced", "二推强制反向", "secondOverScore", "二推大球分", "secondUnderScore", "二推小球分", "underGoalLine", "小球盘口", "overGoalLine", "大球盘口", "underTotalValue", "小球球数", "overTotalValue", "大球球数", "secondaryTotalValue", "次选球数", "mainTotal", "主推球数", "officialRoi", "源站盈亏率", "hotCold", "冷热指数", "betStake", "投注金额", "probability", "概率", "probabilities", "概率", "support", "投注占比", "simulated", "模拟标记", "payout", "赔付金额", "profit", "盈利金额", "loss", "亏损金额", "roi", "收益率", "odds", "赔率", "goalDifference", "净胜球", "goalsForTotal", "进球总数", "goalsAgainstTotal", "失球总数", "goalsForMean", "场均进球", "goalsAgainstMean", "场均失球", "maxGoalsFor", "最大进球", "predictedGoals", "预计进球", "historyGoalAverage", "历史均球", "recentGoalAverage", "近期均球", "combinedGoalAverage", "综合均球", "combinedHandicapAverage", "综合让球", "historyGoalDifference", "历史净胜球", "recentGoalDifference", "近期净胜球", "goalBalanceValue", "球数均衡值", "historySafe", "历史时间安全", "legacyKelly", "凯利结论", "legacyTicai", "体彩结论", "qiuPrediction", "大小球结论", "prediction", "预测", "outcome", "方向", "firstDirection", "一推方向", "goalDirection", "球数方向", "secondaryOutcome", "次选方向", "secondaryGoalDirection", "次选大小球方向", "secondaryGoalLine", "次选盘口", "goalLine", "盘口", "goals", "进球", "goal", "球数", "score", "比分", "total", "总数", "difference", "差值", "home", "主队", "guest", "客队", "away", "客队", "draw", "平局", "over", "大球", "under", "小球", "opening", "初盘", "latest", "即时盘", "weighted", "加权", "unweighted", "未加权", "teamFavor", "队伍好感", "nameFavor", "名字好感", "rankFavor", "排名好感", "historyFavor", "交锋好感", "previousFavor", "上一场好感", "seventhSense", "第七感", "homeScore", "主队得分", "guestScore", "客队得分", "homeRank", "主队排名", "guestRank", "客队排名", "error", "偏差")
	return title + "：" + replace.Replace(strings.ReplaceAll(path, ".", " / "))
}

var fullMetadata = func() []FeatureMetadata {
	result := []FeatureMetadata{}
	seen := map[string]bool{}
	add := func(name, module, source string, derived, synthetic bool) {
		if seen[name] {
			panic("duplicate comprehensive feature: " + name)
		}
		seen[name] = true
		result = append(result, FeatureMetadata{Name: name, Label: fullChineseLabel(name, module), Module: module, Source: source, Derived: derived, Synthetic: synthetic})
	}
	for _, f := range drawmodeldata.BaselineFeatures() {
		add(f.Name, fullBaseModule(f.Name), "frozen numerical input reconstructed without targets", true, false)
		if f.Label != "" {
			result[len(result)-1].Label = f.Label
		}
	}
	for _, module := range []string{"summary", "goddess", "platform", "evil"} {
		for _, p := range fullNumericPaths[module] {
			add("full."+module+"."+p, module, "frontend calculated projection", true, module == "goddess")
		}
		for _, p := range fullDirectionPaths[module] {
			for _, o := range append(append([]string{}, fullOutcomes...), "over", "under") {
				add("full."+module+"."+p+"."+o, module, "frontend direction output; one-hot", true, module == "goddess")
			}
		}
		for _, p := range fullScorePaths[module] {
			for _, k := range []string{"home", "away", "total", "difference"} {
				add("full."+module+"."+p+"."+k, module, "frontend predicted score, never actual target score", true, false)
			}
		}
	}
	for _, k := range []string{"home", "away"} {
		for _, p := range []string{"goalsForTotal", "maxGoalsFor", "goalsAgainstTotal", "goalsForMean", "goalsAgainstMean", "predictedGoals"} {
			add("full.recent."+k+"."+p, "recent", "frontend recent five fixtures", true, false)
		}
		for _, p := range []string{"teamFavor", "nameFavor", "rankFavor", "historyFavor", "previousFavor", "seventhSense"} {
			add("full.goddess."+p+"."+k, "goddess", "synthetic page-derived score, not independent observed evidence", true, true)
		}
	}
	for _, p := range []string{"asian.home", "asian.away", "goals.over", "goals.under"} {
		add("full.heat."+p, "heat", "existing frontend pressure percentages, not measured funds", true, false)
	}
	for _, p := range []string{"unweighted", "weighted"} {
		for _, o := range fullOutcomes {
			add("full.retail."+p+"."+o, "retail", "page retail distribution", true, false)
		}
	}
	for _, p := range []string{"opening", "latest"} {
		for _, o := range fullOutcomes {
			add("full.probability."+p+"."+o, "odds", "page odds-implied probability", true, false)
		}
	}
	for _, p := range fullWarningPatterns {
		add("full.warning."+p.key, "warnings", "presence of explicitly named frontend warning, no template percentages", true, false)
	}
	for _, p := range []string{"secondPassReversed", "secondPassForced"} {
		add("full.evil.prediction."+p, "evil", "frontend boolean", true, false)
	}
	for _, market := range fullMarketKeys {
		for _, o := range fullOutcomes {
			for _, k := range []string{"odds", "support", "roi", "officialRoi", "hotCold", "probability", "error", "betStake", "payout", "profit", "loss"} {
				add("full.profit."+market+"."+o+"."+k, "profits", "separate original/simulated market; same signed bookmaker convention", true, false)
			}
		}
		add("full.profit."+market+".goal", "profits", "actual/simulated handicap line", true, false)
		add("full.profit."+market+".simulated", "profits", "explicit market provenance; source and fallback simulations remain distinguishable", false, false)
	}
	for section, fields := range map[string][]string{
		"tzbl":      {"h", "d", "a", "hprobability", "dprobability", "aprobability", "hsupportRate", "dsupportRate", "asupportRate", "herror", "derror", "aerror", "psyError"},
		"jyykSpf":   {"h", "d", "a", "hsupportRate", "dsupportRate", "asupportRate", "hy", "dy", "ay", "hlr", "dlr", "alr"},
		"jyykRqspf": {"h", "d", "a", "hsupportRate", "dsupportRate", "asupportRate", "hy", "dy", "ay"},
	} {
		for _, field := range fields {
			add("full.trade."+section+"."+field, "trade", "raw source field presence preserved", false, false)
		}
	}
	for _, module := range []string{"recent", "h2h", "historySafe", "goddess", "platform", "evil", "profits", "heat", "warnings"} {
		add("full.available."+module, "availability", "input availability/safety, never target result", false, false)
	}
	// Stability across Go map iteration, exports, training and serving.
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}()

func Metadata() []FeatureMetadata { return append([]FeatureMetadata(nil), fullMetadata...) }

func fullObject(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	if m == nil {
		return map[string]interface{}{}
	}
	return m
}
func fullArray(v interface{}) []interface{} { a, _ := v.([]interface{}); return a }
func fullPath(v interface{}, path string) interface{} {
	for _, p := range strings.Split(path, ".") {
		v = fullObject(v)[p]
	}
	return v
}
func fullNumber(v interface{}) (float64, bool) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case json.Number:
		var err error
		n, err = x.Float64()
		if err != nil {
			return 0, false
		}
	case string:
		var err error
		n, err = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(x), "%"), 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}
func fullBool(v interface{}) bool { b, _ := v.(bool); return b }
func fullPut(out map[string]float64, name string, value interface{}) {
	if n, ok := fullNumber(value); ok {
		out[name] = n
	}
}
func fullText(v interface{}) string { s, _ := v.(string); return s }

func fullSafeHistory(row map[string]interface{}) bool {
	h := fullObject(row["history"])
	for _, k := range []string{"usedRecentAtOrAfterKickoff", "usedRecentTargetId", "usedRecentUnknownTime", "againstListedAtOrAfterKickoff", "againstListedTargetId", "againstListedUnknownTime"} {
		if n, ok := fullNumber(h[k]); ok && n > 0 {
			return false
		}
	}
	return true
}

func fullDirections(out map[string]float64, prefix string, v interface{}) {
	values := []string{}
	if a, ok := v.([]interface{}); ok {
		for _, x := range a {
			values = append(values, fullText(x))
		}
	} else {
		values = []string{fullText(v)}
	}
	known := false
	set := map[string]bool{}
	for _, raw := range values {
		switch strings.TrimSpace(raw) {
		case "home", "胜", "主胜":
			set["home"] = true
			known = true
		case "draw", "平", "平局":
			set["draw"] = true
			known = true
		case "away", "负", "客胜":
			set["away"] = true
			known = true
		case "over", "大", "大球":
			set["over"] = true
			known = true
		case "under", "小", "小球":
			set["under"] = true
			known = true
		}
	}
	if known {
		for _, o := range []string{"home", "draw", "away", "over", "under"} {
			out[prefix+"."+o] = 0
			if set[o] {
				out[prefix+"."+o] = 1
			}
		}
	}
}

var fullScorePattern = regexp.MustCompile(`^\s*(\d{1,2})\s*[:：]\s*(\d{1,2})\s*$`)
var fullRankPattern = regexp.MustCompile(`(\d{1,3})\s*$`)

// ExtractFeatures reads only explicitly whitelisted projections. It never
// traverses arbitrary numbers, warning templates, current scores or identity.
func ExtractFeatures(data []byte) (map[string]float64, error) {
	var row map[string]interface{}
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("full model input must be an object")
	}
	base, err := drawmodeldata.ExtractFeatures(data)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, f := range drawmodeldata.BaselineFeatures() {
		n, ok := base[f.Name]
		for _, g := range f.Guards {
			v, p := base[g.Feature]
			ok = ok && p && v >= g.Minimum
		}
		if ok {
			out[f.Name] = n
		}
	}
	inputs, full := fullObject(row["analysisInputs"]), fullObject(row["fullAnalysis"])
	safe := fullSafeHistory(row)
	recent := fullBool(fullPath(inputs, "availability.recentHome")) && fullBool(fullPath(inputs, "availability.recentGuest"))
	history := fullBool(fullPath(inputs, "availability.historySummary"))
	putAvailable := func(name string, yes bool) {
		out["full.available."+name] = 0
		if yes {
			out["full.available."+name] = 1
		}
	}
	putAvailable("historySafe", safe)
	putAvailable("recent", safe && recent)
	putAvailable("h2h", safe && history)
	for _, k := range []string{"homeRank", "guestRank"} {
		if hit := fullRankPattern.FindStringSubmatch(fullText(full[k])); len(hit) == 2 {
			fullPut(out, "full.summary."+k, hit[1])
		}
	}
	for _, p := range []string{"historyGoalAverage", "recentGoalAverage", "combinedGoalAverage", "combinedHandicapAverage", "historyGoalDifference", "recentGoalDifference", "goalBalanceValue"} {
		valid := safe
		if strings.HasPrefix(p, "history") {
			valid = valid && history
		}
		if strings.Contains(p, "recent") || strings.Contains(p, "Recent") {
			valid = valid && recent
		}
		if p == "goalBalanceValue" {
			valid = valid && history && recent
		}
		if valid {
			v := full[p]
			if v == nil {
				v = inputs[p]
			}
			fullPut(out, "full.summary."+p, v)
		}
	}
	if len(fullArray(inputs["marketProbabilities"])) == 3 {
		fullDirections(out, "full.summary.prediction", full["prediction"])
	}
	if safe && recent && fullBool(fullPath(row, "goalsHeat.available")) {
		fullDirections(out, "full.summary.qiuPrediction", full["qiuPrediction"])
	}
	for _, p := range []string{"legacyKelly", "legacyTicai"} {
		flag := "strictKelly"
		if p == "legacyTicai" {
			flag = "strictTicai"
		}
		if fullBool(fullPath(inputs, "availability."+flag)) {
			fullDirections(out, "full.summary."+p, inputs[p])
		}
	}
	if safe && recent {
		for _, a := range []struct {
			key    string
			fields []string
		}{{"qiushuAll", []string{"home.goalsForTotal", "home.maxGoalsFor", "away.goalsForTotal", "away.maxGoalsFor", "home.goalsAgainstTotal", "away.goalsAgainstTotal"}}, {"liangduiqiushu", []string{"home.goalsForMean", "away.goalsForMean", "home.goalsAgainstMean", "away.goalsAgainstMean", "home.predictedGoals", "away.predictedGoals"}}} {
			values := fullArray(inputs[a.key])
			for i, p := range a.fields {
				if i < len(values) {
					fullPut(out, "full.recent."+p, values[i])
				}
			}
		}
	}
	if n, ok := fullNumber(row["homeHeat"]); ok && n >= 0 && n <= 100 {
		out["full.heat.asian.home"] = n
		out["full.heat.asian.away"] = 100 - n
		putAvailable("heat", true)
	}
	goals := fullObject(row["goalsHeat"])
	if safe && fullBool(goals["available"]) {
		fullPut(out, "full.heat.goals.over", goals["over"])
		fullPut(out, "full.heat.goals.under", goals["under"])
	}
	for _, phase := range []string{"opening", "latest"} {
		var values []interface{}
		if phase == "latest" {
			values = fullArray(inputs["marketProbabilities"])
		} else {
			if len(fullArray(inputs["averageOpening"])) != 3 {
				continue
			}
			values = fullArray(inputs["kaijuresult"])
		}
		for i, o := range fullOutcomes {
			if len(values) == 3 {
				fullPut(out, "full.probability."+phase+"."+o, values[i])
			}
		}
	}
	for _, module := range []string{"goddess", "platform", "evil"} {
		var block map[string]interface{}
		switch module {
		case "goddess":
			block = fullObject(full["goddessWoman"])
		case "platform":
			block = fullObject(full["platform"])
			if len(block) == 0 {
				block = fullObject(inputs["platform"])
			}
		case "evil":
			block = fullObject(fullPath(full, "platform.evilCult"))
			if len(block) == 0 {
				block = fullObject(fullPath(inputs, "platform.evilCult"))
			}
		}
		putAvailable(module, safe && len(block) > 0)
		if !safe || len(block) == 0 {
			continue
		}
		for _, p := range fullNumericPaths[module] {
			fullPut(out, "full."+module+"."+p, fullPath(block, p))
		}
		for _, p := range fullDirectionPaths[module] {
			fullDirections(out, "full."+module+"."+p, fullPath(block, p))
		}
		for _, p := range fullScorePaths[module] {
			if hit := fullScorePattern.FindStringSubmatch(fullText(fullPath(block, p))); len(hit) == 3 {
				h, _ := strconv.ParseFloat(hit[1], 64)
				a, _ := strconv.ParseFloat(hit[2], 64)
				out["full."+module+"."+p+".home"] = h
				out["full."+module+"."+p+".away"] = a
				out["full."+module+"."+p+".total"] = h + a
				out["full."+module+"."+p+".difference"] = h - a
			}
		}
		if module == "evil" {
			for _, p := range []string{"secondPassReversed", "secondPassForced"} {
				v, ok := fullPath(block, "prediction."+p).(bool)
				if ok {
					out["full.evil.prediction."+p] = 0
					if v {
						out["full.evil.prediction."+p] = 1
					}
				}
			}
		}
		if module == "goddess" {
			for i, v := range fullArray(block["dimensionScores"]) {
				if i >= 6 {
					break
				}
				key := []string{"teamFavor", "nameFavor", "rankFavor", "historyFavor", "previousFavor", "seventhSense"}[i]
				if (key == "historyFavor" || key == "previousFavor") && !history {
					continue
				}
				fullPut(out, "full.goddess."+key+".home", fullObject(v)["home"])
				fullPut(out, "full.goddess."+key+".away", fullObject(v)["guest"])
			}
		}
	}
	plat := fullObject(full["platform"])
	if len(plat) == 0 {
		plat = fullObject(inputs["platform"])
	}
	if safe && len(plat) > 0 {
		putAvailable("warnings", true)
		texts := []string{}
		for _, key := range []string{"warningRows", "handicapAlertRows", "goalBalanceAlertRows"} {
			if key == "goalBalanceAlertRows" && !history {
				continue
			}
			for _, v := range fullArray(plat[key]) {
				r := fullObject(v)
				texts = append(texts, fullText(r["label"])+" "+fullText(r["value"]))
			}
		}
		texts = append(texts, fullText(plat["handicapPressureLabel"]), fullText(plat["professionalConsensus"]))
		joined := strings.Join(texts, "\n")
		for _, p := range fullWarningPatterns {
			out["full.warning."+p.key] = 0
			if strings.Contains(joined, p.text) {
				out["full.warning."+p.key] = 1
			}
		}
		if len(fullObject(plat["professionalConflict"])) > 0 {
			out["full.warning.professionalConflict"] = 1
		}
	}
	trade := fullObject(inputs["tradeFields"])
	for _, meta := range fullMetadata {
		if strings.HasPrefix(meta.Name, "full.trade.") {
			fullPut(out, meta.Name, trade[strings.TrimPrefix(meta.Name, "full.trade.")])
		}
	}
	markets := fullArray(fullPath(full, "roiSimulation.markets"))
	if len(markets) == 0 {
		markets = fullArray(fullPath(row, "frontend.roiSimulation.markets"))
	}
	if local := fullPath(plat, "localMarket"); local != nil {
		markets = append(append([]interface{}{}, markets...), local)
	}
	knownMarket := map[string]bool{}
	for _, k := range fullMarketKeys {
		knownMarket[k] = true
	}
	for _, raw := range markets {
		market := fullObject(raw)
		key := fullText(market["key"])
		if !knownMarket[key] || !fullBool(market["oddsAvailable"]) {
			continue
		}
		if fullBool(market["simulated"]) && !safe {
			continue
		}
		putAvailable("profits", true)
		out["full.profit."+key+".simulated"] = 0
		if fullBool(market["simulated"]) {
			out["full.profit."+key+".simulated"] = 1
		}
		if key == "sportteryRqspf" || key == "sportteryRqspfSim" {
			fullPut(out, "full.profit."+key+".goal", market["goal"])
		}
		for _, r := range fullArray(market["bookmakerByOutcome"]) {
			v := fullObject(r)
			o := fullText(v["outcome"])
			if o != "home" && o != "draw" && o != "away" {
				continue
			}
			prefix := "full.profit." + key + "." + o + "."
			fullPut(out, prefix+"odds", v["odds"])
			if !fullBool(v["available"]) {
				continue
			}
			for to, from := range map[string]string{"support": "retailShare", "roi": "bookmakerRoi", "hotCold": "hotColdIndex", "probability": "probability", "error": "error", "betStake": "betStake", "payout": "payout", "profit": "bookmakerProfit", "loss": "bookmakerLoss"} {
				fullPut(out, prefix+to, v[from])
			}
			section := "jyykSpf"
			if key == "sportteryRqspf" {
				section = "jyykRqspf"
			}
			if !fullBool(market["simulated"]) && (key == "sporttery" || key == "sportteryRqspf") {
				field := map[string]string{"home": "hy", "draw": "dy", "away": "ay"}[o]
				fullPut(out, prefix+"officialRoi", trade[section+"."+field])
			}
			if key == "sportterySim" || key == "localAverageOdds" || (key == "sporttery" && fullBool(market["simulated"])) {
				if safe {
					fullPut(out, "full.retail.weighted."+o, v["retailShare"])
				}
			}
		}
	}
	for i, o := range fullOutcomes {
		v := fullArray(inputs["marketProbabilities"])
		if len(v) == 3 {
			fullPut(out, "full.retail.unweighted."+o, v[i])
		}
	}
	// Keep only the stable manifest: accidental new response properties and all
	// target/identity/timestamp values are incapable of entering the model.
	filtered := map[string]float64{}
	for _, m := range fullMetadata {
		if v, ok := out[m.Name]; ok {
			filtered[m.Name] = v
		}
	}
	return filtered, nil
}
