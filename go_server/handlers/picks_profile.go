// Package handlers: picks_profile.go turns the owner's settled picks into a
// per-market, per-handicap-bucket profile, and attaches a per-match "myAngle"
// block so the H5 page can show how the owner historically performs in the
// current match's market shape (红区=跟自己 / 黑区=考虑反向).
package handlers

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"go_server/database"
	"go_server/models"

	"github.com/gin-gonic/gin"
)

type myAngleMarket struct {
	Bucket   string  `json:"bucket"`
	Sample   int     `json:"sample"`
	Hit      int     `json:"hit"`
	Accuracy float64 `json:"accuracy"`
	Verdict  string  `json:"verdict"` // red / black / neutral
}

type myAngleBlock struct {
	TotalPicks int           `json:"totalPicks"`
	Spf        myAngleMarket `json:"spf"`
	Rqspf      myAngleMarket `json:"rqspf"`
	Dxq        myAngleMarket `json:"dxq"`
}

type pickBucketStat struct{ Sample, Hit int }

// pickOwnerProfile aggregates the settled picks once per request.
type pickOwnerProfile struct {
	TotalPicks   int
	SpfByBucket  map[string]*pickBucketStat
	RqByBucket   map[string]*pickBucketStat
	DxqByBucket  map[string]*pickBucketStat
	MarketTotals map[string]*pickBucketStat // spf/rqspf/dxq/score overall
}

func profileAsianBucket(line float64, hasLine bool) string {
	if !hasLine {
		return "无亚盘"
	}
	switch {
	case line <= -1:
		return "受让深(≤-1)"
	case line <= -0.5:
		return "受让中(-0.75~-0.5)"
	case line < 0.5:
		return "平/浅(±0.25)"
	case line < 1:
		return "主让中(0.5~0.75)"
	default:
		return "主让深(≥1)"
	}
}

func profileGoalBucket(line float64, hasLine bool) string {
	if !hasLine {
		return "无盘口"
	}
	switch {
	case line <= 2.25:
		return "低盘(≤2.25)"
	case line < 2.75:
		return "中盘(2.5)"
	default:
		return "高盘(≥2.75)"
	}
}

func profileOutcome(homeScore, guestScore int) string {
	if homeScore > guestScore {
		return "home"
	}
	if homeScore < guestScore {
		return "away"
	}
	return "draw"
}

func profilePickOutcome(text string) string {
	if strings.Contains(text, "平") {
		return "draw"
	}
	if strings.Contains(text, "客") || strings.Contains(text, "负") {
		return "away"
	}
	if strings.Contains(text, "主") || strings.Contains(text, "胜") {
		return "home"
	}
	return ""
}

func profileBucketAdd(buckets map[string]*pickBucketStat, key string, hit bool) {
	stat := buckets[key]
	if stat == nil {
		stat = &pickBucketStat{}
		buckets[key] = stat
	}
	stat.Sample++
	if hit {
		stat.Hit++
	}
}

// buildPickOwnerProfile settles every pick against completed matches and
// buckets them by the market handicap shape.
func buildPickOwnerProfile() *pickOwnerProfile {
	profile := &pickOwnerProfile{
		SpfByBucket:  map[string]*pickBucketStat{},
		RqByBucket:   map[string]*pickBucketStat{},
		DxqByBucket:  map[string]*pickBucketStat{},
		MarketTotals: map[string]*pickBucketStat{},
	}
	var picks []models.UserPick
	if database.DB.Find(&picks).Error != nil || len(picks) == 0 {
		return profile
	}
	ids := make([]string, 0, len(picks))
	seen := map[string]bool{}
	for _, pick := range picks {
		if !seen[pick.MatchId] {
			seen[pick.MatchId] = true
			ids = append(ids, pick.MatchId)
		}
	}
	var matches []models.Money
	if database.DB.Where("match_id IN ?", ids).Find(&matches).Error != nil {
		return profile
	}
	matchByID := map[string]models.Money{}
	for _, match := range matches {
		if pickMatchSettled(match) {
			matchByID[match.MatchId] = match
		}
	}
	var pankous []models.PankouMoney
	database.DB.Where("match_id IN ?", ids).Find(&pankous)
	pankouByID := map[string]models.PankouMoney{}
	for _, pankou := range pankous {
		pankouByID[pankou.MatchId] = pankou
	}

	for _, pick := range picks {
		match, ok := matchByID[pick.MatchId]
		if !ok {
			continue
		}
		pankou := pankouByID[pick.MatchId]
		asianRows, dxqRows := pankouRows(pankou)
		asianItem := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Asia), asianRows, 8)
		dxqItem := selectPankouRow(jsonValue[analysisPankouItem](pankou.Bet365Dxq), dxqRows, 8)
		asianLineText := firstNonEmptyString(asianItem.Pankou, asianItem.FirstPankou)
		dxqLineText := firstNonEmptyString(dxqItem.Pankou, dxqItem.FirstPankou)
		asianLine := parsePankouLine(asianLineText)
		hasAsian := strings.TrimSpace(asianLineText) != ""
		actual := profileOutcome(match.HomeScore, match.GuestScore)

		switch pick.Market {
		case "spf":
			chosen := profilePickOutcome(pick.Pick)
			if chosen == "" {
				continue
			}
			hit := chosen == actual
			profileBucketAdd(profile.SpfByBucket, profileAsianBucket(asianLine, hasAsian), hit)
			profileBucketAdd(profile.MarketTotals, "spf", hit)
			profile.TotalPicks++
		case "rqspf":
			if pick.Line == nil {
				continue
			}
			chosen := ""
			if strings.Contains(pick.Pick, "胜") {
				chosen = "home"
			} else if strings.Contains(pick.Pick, "平") {
				chosen = "draw"
			} else if strings.Contains(pick.Pick, "负") {
				chosen = "away"
			}
			if chosen == "" {
				continue
			}
			adjusted := float64(match.HomeScore) + *pick.Line - float64(match.GuestScore)
			actualRq := "draw"
			if adjusted > 0.001 {
				actualRq = "home"
			} else if adjusted < -0.001 {
				actualRq = "away"
			}
			hit := chosen == actualRq
			profileBucketAdd(profile.RqByBucket, profileAsianBucket(asianLine, hasAsian), hit)
			profileBucketAdd(profile.MarketTotals, "rqspf", hit)
			profile.TotalPicks++
		case "dxq":
			if pick.Line == nil {
				continue
			}
			total := float64(match.HomeScore + match.GuestScore)
			if math.Abs(total-*pick.Line) < 0.001 {
				continue // push
			}
			chosenOver := strings.Contains(pick.Pick, "大")
			if !chosenOver && !strings.Contains(pick.Pick, "小") {
				continue
			}
			hit := chosenOver == (total > *pick.Line)
			bucketLine := *pick.Line
			hasBucket := true
			if bucketLine <= 0 {
				bucketLine = parsePankouLine(dxqLineText)
				hasBucket = strings.TrimSpace(dxqLineText) != ""
			}
			profileBucketAdd(profile.DxqByBucket, profileGoalBucket(bucketLine, hasBucket), hit)
			profileBucketAdd(profile.MarketTotals, "dxq", hit)
			profile.TotalPicks++
		case "score":
			actualScore := fmt.Sprintf("%d:%d", match.HomeScore, match.GuestScore)
			normalized := strings.ReplaceAll(strings.ReplaceAll(pick.Pick, "－", "-"), "：", ":")
			hit := false
			for _, splitter := range []string{"，", "、", "/", ";", "；"} {
				normalized = strings.ReplaceAll(normalized, splitter, ",")
			}
			for _, candidate := range strings.Split(normalized, ",") {
				candidate = strings.TrimSpace(strings.ReplaceAll(candidate, "-", ":"))
				if candidate == actualScore {
					hit = true
					break
				}
			}
			profileBucketAdd(profile.MarketTotals, "score", hit)
			profile.TotalPicks++
		}
	}
	return profile
}

func profileVerdict(stat *pickBucketStat) string {
	if stat == nil || stat.Sample < 5 {
		return "neutral"
	}
	accuracy := float64(stat.Hit) / float64(stat.Sample) * 100
	if accuracy >= 65 {
		return "red"
	}
	if accuracy <= 35 {
		return "black"
	}
	return "neutral"
}

func profileMarketFor(buckets map[string]*pickBucketStat, bucket string) myAngleMarket {
	stat := buckets[bucket]
	market := myAngleMarket{Bucket: bucket, Verdict: "neutral"}
	if stat != nil {
		market.Sample = stat.Sample
		market.Hit = stat.Hit
		if stat.Sample > 0 {
			market.Accuracy = round2(float64(stat.Hit) / float64(stat.Sample) * 100)
		}
		market.Verdict = profileVerdict(stat)
	}
	return market
}

// attachMyAngle decorates analysis rows with the owner's bucket stats for each
// match's current market shape.
func attachMyAngle(rows []analysisMatchResponse) {
	profile := buildPickOwnerProfile()
	if profile.TotalPicks == 0 {
		return
	}
	for index := range rows {
		row := &rows[index]
		hasAsian := row.YapanPankou2 != 0 || row.YapanPankou1 != 0
		asianBucket := profileAsianBucket(row.YapanPankou2, hasAsian)
		goalBucket := profileGoalBucket(row.QiushuPankou2, row.QiushuPankou2 > 0)
		row.MyAngle = &myAngleBlock{
			TotalPicks: profile.TotalPicks,
			Spf:        profileMarketFor(profile.SpfByBucket, asianBucket),
			Rqspf:      profileMarketFor(profile.RqByBucket, asianBucket),
			Dxq:        profileMarketFor(profile.DxqByBucket, goalBucket),
		}
	}
}

// GetPickProfile exposes the whole profile (radar-ready) for the H5.
func GetPickProfile(c *gin.Context) {
	profile := buildPickOwnerProfile()
	bucketRows := func(buckets map[string]*pickBucketStat) []gin.H {
		rows := []gin.H{}
		for _, key := range []string{"受让深(≤-1)", "受让中(-0.75~-0.5)", "平/浅(±0.25)", "主让中(0.5~0.75)", "主让深(≥1)", "无亚盘", "低盘(≤2.25)", "中盘(2.5)", "高盘(≥2.75)", "无盘口"} {
			stat := buckets[key]
			if stat == nil || stat.Sample == 0 {
				continue
			}
			rows = append(rows, gin.H{
				"bucket": key, "sample": stat.Sample, "hit": stat.Hit,
				"accuracy": round2(float64(stat.Hit) / float64(stat.Sample) * 100),
				"verdict":  profileVerdict(stat),
			})
		}
		return rows
	}
	marketRow := func(key string) gin.H {
		stat := profile.MarketTotals[key]
		if stat == nil {
			return gin.H{"sample": 0, "hit": 0, "accuracy": 0}
		}
		return gin.H{
			"sample": stat.Sample, "hit": stat.Hit,
			"accuracy": round2(float64(stat.Hit) / float64(stat.Sample) * 100),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"totalPicks":   profile.TotalPicks,
		"markets":      gin.H{"spf": marketRow("spf"), "rqspf": marketRow("rqspf"), "dxq": marketRow("dxq"), "score": marketRow("score")},
		"asianBuckets": bucketRows(profile.SpfByBucket),
		"rqspfBuckets": bucketRows(profile.RqByBucket),
		"goalBuckets":  bucketRows(profile.DxqByBucket),
	})
}
