package drawmodel

import (
	"fmt"
	"math"
	"strings"
)

// MarketDrawProbability proportionally removes the three-way overround.
// The caller must separately enforce the quote's pre-match capture time.
func MarketDrawProbability(odds Odds) (float64, bool) {
	if !finite(odds.Home) || !finite(odds.Draw) || !finite(odds.Away) || odds.Home <= 1 || odds.Draw <= 1 || odds.Away <= 1 {
		return 0, false
	}
	total := 1/odds.Home + 1/odds.Draw + 1/odds.Away
	return (1 / odds.Draw) / total, true
}

func appendWarning(result *Result, warning string) {
	for _, old := range result.Warnings {
		if old == warning {
			return
		}
	}
	result.Warnings = append(result.Warnings, warning)
}

func modelKey(model strengthModel, history []Match, id, name string) (string, bool) {
	key := teamKey(id, name)
	if _, exists := model.teams[key]; exists {
		return key, strings.TrimSpace(id) == ""
	}
	keys := map[string]bool{}
	for _, g := range history {
		if yes, _ := sameTeam(id, name, g.HomeTeamID, g.HomeTeamName); yes {
			keys[teamKey(g.HomeTeamID, g.HomeTeamName)] = true
		}
		if yes, _ := sameTeam(id, name, g.AwayTeamID, g.AwayTeamName); yes {
			keys[teamKey(g.AwayTeamID, g.AwayTeamName)] = true
		}
	}
	// Ambiguous display names do not silently merge two known teams.
	if len(keys) == 1 {
		for candidate := range keys {
			return candidate, true
		}
	}
	return key, false
}

// Predict only reads information available before min(AsOf, Target.Kickoff).
// There is deliberately no equal-thirds fallback for missing histories/odds.
func Predict(in Input) Result {
	r := Result{
		Warnings: []string{}, Reasons: []string{}, TopScorelines: []Scoreline{},
		Model: ModelInfo{Version: Version, HalfLifeDays: halfLifeDays, ShrinkageMatches: shrinkageMatches},
	}
	if in.AsOf.IsZero() || in.Target.Kickoff.IsZero() {
		appendWarning(&r, "缺少预测时间或开赛时间，不能建立赛前数据边界")
		return r
	}
	cutoff := in.AsOf
	if cutoff.After(in.Target.Kickoff) {
		cutoff = in.Target.Kickoff
		appendWarning(&r, "预测时间晚于开赛，已按开赛时刻截断历史与赔率")
	}
	if teamKey(in.Target.HomeTeamID, in.Target.HomeTeamName) == "" || teamKey(in.Target.AwayTeamID, in.Target.AwayTeamName) == "" {
		appendWarning(&r, "缺少主客队身份，无法估计比赛")
		return r
	}
	if same, _ := sameTeam(in.Target.HomeTeamID, in.Target.HomeTeamName, in.Target.AwayTeamID, in.Target.AwayTeamName); same {
		appendWarning(&r, "主客队身份相同，无法估计比赛")
		return r
	}
	history, duplicates := usableHistory(in, cutoff)
	if duplicates > 0 {
		appendWarning(&r, fmt.Sprintf("已去除 %d 条重复历史比赛", duplicates))
	}
	if len(history) == 0 {
		appendWarning(&r, "没有预测时刻之前已完赛的有效历史，不生成概率")
		return r
	}
	leagueHistory := make([]Match, 0, len(history))
	for _, g := range history {
		if sameLeague(in.Target, g) {
			leagueHistory = append(leagueHistory, g)
		}
	}
	modelHistory := leagueHistory
	r.LeagueBaseline.Source = "league"
	if len(leagueHistory) < 20 {
		modelHistory = history
		r.LeagueBaseline.Source = "all_competitions"
		appendWarning(&r, fmt.Sprintf("同联赛有效历史仅 %d 场，使用可用赛事池估计基准与攻防，需留意跨联赛差异", len(leagueHistory)))
	}
	if len(modelHistory) > 1500 {
		modelHistory = modelHistory[:1500]
		appendWarning(&r, "攻防拟合仅使用最近 1500 场有效历史")
	}
	r.LeagueBaseline.N = len(modelHistory)
	for _, g := range modelHistory {
		if g.HomeGoals == g.AwayGoals {
			r.LeagueBaseline.DrawRate++
		}
		r.LeagueBaseline.HomeGoals += float64(g.HomeGoals)
		r.LeagueBaseline.AwayGoals += float64(g.AwayGoals)
	}
	n := float64(len(modelHistory))
	r.LeagueBaseline.DrawRate /= n
	r.LeagueBaseline.HomeGoals /= n
	r.LeagueBaseline.AwayGoals /= n
	var f1, f2, f3, f4 bool
	r.HomeOverall, f1 = recentForm(history, in.Target.HomeTeamID, in.Target.HomeTeamName, 0, r.LeagueBaseline.DrawRate)
	r.HomeVenue, f2 = recentForm(history, in.Target.HomeTeamID, in.Target.HomeTeamName, 1, r.LeagueBaseline.DrawRate)
	r.AwayOverall, f3 = recentForm(history, in.Target.AwayTeamID, in.Target.AwayTeamName, 0, r.LeagueBaseline.DrawRate)
	r.AwayVenue, f4 = recentForm(history, in.Target.AwayTeamID, in.Target.AwayTeamName, -1, r.LeagueBaseline.DrawRate)
	r.Model.NameFallback = f1 || f2 || f3 || f4
	if r.HomeOverall.N < 10 || r.AwayOverall.N < 10 {
		appendWarning(&r, "至少一队近 10 场历史不足，按实际样本数展示并向赛事基准收缩")
	}
	if r.HomeVenue.N < 10 || r.AwayVenue.N < 10 {
		appendWarning(&r, "至少一队对应主客场历史不足 10 场")
	}
	strength := fitStrength(modelHistory, cutoff)
	homeKey, hfallback := modelKey(strength, modelHistory, in.Target.HomeTeamID, in.Target.HomeTeamName)
	awayKey, afallback := modelKey(strength, modelHistory, in.Target.AwayTeamID, in.Target.AwayTeamName)
	r.Model.NameFallback = r.Model.NameFallback || hfallback || afallback
	if r.Model.NameFallback {
		appendWarning(&r, "部分球队缺少可匹配 ID，已按规范化名称回退匹配，存在同名风险")
	}
	_, homeKnown := strength.teams[homeKey]
	_, awayKnown := strength.teams[awayKey]
	if !homeKnown || !awayKnown {
		appendWarning(&r, "至少一队缺少可用攻防样本，其攻防参数收缩为赛事均值")
	}
	r.HomeLambda, r.AwayLambda = strength.lambdas(homeKey, awayKey)
	r.Model.Rho, r.Model.RhoFitted = strength.fitRho()
	if !r.Model.RhoFitted {
		appendWarning(&r, "低比分修正拟合样本不足，rho=0，采用独立泊松比分分布")
	}
	lo, hi := rhoBounds(r.HomeLambda, r.AwayLambda)
	if r.Model.Rho < lo || r.Model.Rho > hi {
		r.Model.Rho = clamp(r.Model.Rho, lo, hi)
		appendWarning(&r, "低比分修正已限制在本场比分概率有效范围内")
	}
	r.ScoreModelProbability, r.TopScorelines = scoreDistribution(r.HomeLambda, r.AwayLambda, r.Model.Rho)
	r.Probability = r.ScoreModelProbability
	r.Available = true
	if in.Odds == nil {
		appendWarning(&r, "缺少赛前三项欧赔，不生成市场概率")
	} else if in.Odds.CapturedAt.IsZero() || in.Odds.CapturedAt.After(cutoff) {
		appendWarning(&r, "欧赔采集时间缺失或晚于预测时刻，已排除市场特征")
	} else if p, ok := MarketDrawProbability(*in.Odds); ok {
		r.MarketProbability = &p
	} else {
		appendWarning(&r, "欧赔缺失、非有限数或小于等于 1，已排除市场特征")
	}
	if in.Artifact != nil && in.Artifact.Fitted {
		if artifactValid(*in.Artifact, cutoff) {
			x := featureVector(r, in.Odds, cutoff)
			p := artifactProbability(*in.Artifact, x)
			if finite(p) && p > 0 && p < 1 {
				r.Probability = p
				r.Model.FusionFitted = true
				r.Model.Calibrated = in.Artifact.Calibrated
				r.Model.TrainingSamples = in.Artifact.TrainingSamples
				r.Model.CalibrationSamples = in.Artifact.CalibrationSamples
				stamp := in.Artifact.TrainingCutoff
				r.Model.TrainingCutoff = &stamp
			}
		} else {
			appendWarning(&r, "融合模型版本、参数或训练时间边界无效，回退到比分模型")
		}
	}
	if !r.Model.FusionFitted {
		appendWarning(&r, "尚无合格的独立后段校准融合模型，最终概率为未校准比分模型估计")
	}
	r.Reasons = append(r.Reasons,
		fmt.Sprintf("基于 %d 场赛前历史拟合攻防，时间权重半衰期 %.0f 天", r.LeagueBaseline.N, halfLifeDays),
		fmt.Sprintf("主客预期进球 %.2f / %.2f，平局概率来自相同进球比分之和", r.HomeLambda, r.AwayLambda),
		fmt.Sprintf("主队近 %d 场 %d 平，客队近 %d 场 %d 平；对应主客场分别为 %d 场 %d 平、%d 场 %d 平", r.HomeOverall.N, r.HomeOverall.Draws, r.AwayOverall.N, r.AwayOverall.Draws, r.HomeVenue.N, r.HomeVenue.Draws, r.AwayVenue.N, r.AwayVenue.Draws))
	if r.MarketProbability != nil {
		r.Reasons = append(r.Reasons, fmt.Sprintf("三项欧赔按比例去水后的市场平局概率为 %.1f%%", *r.MarketProbability*100))
	}
	if r.Model.FusionFitted {
		r.Reasons = append(r.Reasons, "最终概率由历史样本学习的正则逻辑回归融合，并由独立后段样本校准")
	}
	return r
}

func logit(p float64) float64 { p = clamp(p, 1e-6, 1-1e-6); return math.Log(p / (1 - p)) }
func sigmoid(v float64) float64 {
	if v >= 0 {
		return 1 / (1 + math.Exp(-v))
	}
	e := math.Exp(v)
	return e / (1 + e)
}
