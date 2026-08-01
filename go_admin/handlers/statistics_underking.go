// Package handlers: statistics_underking.go 是「小球王」——从【完赛基础统计】里挑
// 出几个大小球维度，各取推荐方向为【小球】的场次，一个维度一行看总命中率。
// 本页不按盘口、不按热度档拆，要看细分去基础统计页。
//
// 它不另算一套口径：数据源就是基础统计的同一份 match_statistics 快照，逐条明细
// 按推荐方向筛一遍再重算命中率，所以两页的数字永远对得上。
package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 明细列表只回传前 N 场：前端本来也只展示这么多，场次统计仍用完整口径。
const underKingDetailCap = 300

// base70_goals 是横表信号，一行盘口里七种买法并排；这三格是买小球的，各自跨全部
// 盘口合计成一行。（「期望球数对本档盘口」那两格是判大判小混着算的，只留判小球那
// 一半后与这里的后两格完全重合，不重复统计。）
var underKingLineBetSuffixes = []string{"-under", "-expunder", "-exptruncunder"}

// 本页保留的维度，改这张表即可加减。key 是摊平后每一行的 key：
// 整卡信号 = 信号 key，分档信号 = 分档 key（或信号 key 表示各档合计），
// 横表信号 = 信号 key + 买法后缀。title 非空时改写标题——基础统计里那几个标题带
// 「分档」字样，合并成一行后就对不上了。
var underKingKeep = []struct{ key, title string }{
	{key: "history_goals"},
	{key: "recent_goals"},
	{key: "goals_composite"},
	{key: "goals_composite_gap", title: "32. 球数综合均值±0.75·判小球（常规段均值低于盘口 + 反向段均值高出盘口≥0.75）"},
	{key: "goals_composite_gap_outer", title: "33. 球数综合均值±0.75·剔除中间带后判小球（均值高出盘口≥0.75）"},
	{key: "goals_split_under"},
	{key: "base70_goals-under", title: "27. 主推≥70%·按盘口买小球"},
	{key: "base70_goals-expunder", title: "27. 主推≥70%·期望球数判小球"},
	{key: "base_qiu", title: "28. 前端球数倾向判小球（全部压力档合计）"},
	{key: "warn-大小球热度过热·反过热方向", title: "29. 警示信号·大小球热度过热·反过热方向"},
	{key: "warn-大小球回归·跟回归方向", title: "29. 警示信号·大小球回归·跟回归方向"},
}

// ---------- 输入：基础统计快照里的一个信号（只取筛选用得到的字段） ----------

type underKingSourceBucket struct {
	Key     string             `json:"key"`
	Title   string             `json:"title"`
	Matched int                `json:"matched"`
	Matches []statisticsDetail `json:"matches"`
}

type underKingSourceLineRow struct {
	Bets []underKingSourceBucket `json:"bets"`
}

type underKingSourceSignal struct {
	Key      string                   `json:"key"`
	Title    string                   `json:"title"`
	Matched  int                      `json:"matched"`
	Matches  []statisticsDetail       `json:"matches"`
	Buckets  []underKingSourceBucket  `json:"buckets"`
	LineRows []underKingSourceLineRow `json:"line_rows"`
}

type underKingSourceReport struct {
	SettledTotal int                     `json:"settled_total"`
	StartDate    string                  `json:"start_date"`
	EndDate      string                  `json:"end_date"`
	GeneratedAt  string                  `json:"generated_at"`
	Signals      []underKingSourceSignal `json:"signals"`
}

// ---------- 输出：一个维度一行 ----------

type underKingRow struct {
	Key      string             `json:"key"`
	Title    string             `json:"title"`
	Note     string             `json:"note,omitempty"`
	Matched  int                `json:"matched"`
	Hit      int                `json:"hit"`
	Miss     int                `json:"miss"`
	Accuracy float64            `json:"accuracy"`
	Matches  []statisticsDetail `json:"matches"`
}

// GetUnderKingStatistics 服务「小球王」页。数据源与【完赛基础统计】完全相同：
// 不带日期读 match_statistics 快照，refresh=1 重算并写回同一份快照（因此在这里
// 点重算，基础统计页也跟着更新），带日期范围则即时计算、不入库。
func GetUnderKingStatistics(c *gin.Context) {
	start, end, err := statisticsDateRange(c.Query("start_date"), c.Query("end_date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日期格式应为 YYYY-MM-DD"})
		return
	}

	if start != "" || end != "" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "统计计算正在进行中，请稍候再试"})
			return
		}
		report, err := computeMatchStatistics(start, end)
		statisticsRecomputeMu.Unlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 基础统计的明细在内存里是 []statisticsDetail、快照里是 JSON，走一趟
		// Marshal 让两条路径共用同一段解析逻辑。
		payload, err := json.Marshal(report)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		underKingRespond(c, payload)
		return
	}

	if c.Query("refresh") == "1" {
		if !statisticsRecomputeMu.TryLock() {
			c.JSON(http.StatusConflict, gin.H{"error": "重算正在进行中，请稍候再试"})
			return
		}
		defer statisticsRecomputeMu.Unlock()
		report, err := computeMatchStatistics("", "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		payload, err := json.Marshal(report)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := saveStatSnapshot(snapshotKindMatchStatistics, payload, time.Now()); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		underKingRespond(c, payload)
		return
	}

	if payload, _, ok := loadStatSnapshot(snapshotKindMatchStatistics); ok {
		underKingRespond(c, payload)
		return
	}
	c.JSON(http.StatusOK, gin.H{"needs_recompute": true, "settled_total": 0, "rows": []underKingRow{}})
}

func underKingRespond(c *gin.Context, payload []byte) {
	report, err := underKingFromPayload(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, report)
}

// underKingFromPayload 解析一份基础统计报表，摊平成「一个维度一行」的判小球命中率。
func underKingFromPayload(payload []byte) (gin.H, error) {
	var src underKingSourceReport
	if err := json.Unmarshal(payload, &src); err != nil {
		return nil, err
	}

	// 先把每个信号能产出的候选行都算出来（整卡一行、各分档一行、各档合计一行、
	// 横表每种买法一行），再按 underKingKeep 挑本页要的那几行——挑法集中在一处，
	// 加减维度不用动逻辑。
	candidates := map[string]underKingRow{}
	add := func(key, title string, srcMatched int, details []statisticsDetail) {
		if row, ok := underKingMakeRow(key, title, srcMatched, details); ok {
			candidates[key] = row
		}
	}
	for _, signal := range src.Signals {
		switch {
		case len(signal.LineRows) > 0:
			for _, suffix := range underKingLineBetSuffixes {
				details, matched := []statisticsDetail{}, 0
				for _, lineRow := range signal.LineRows {
					for _, bet := range lineRow.Bets {
						if strings.HasSuffix(bet.Key, suffix) {
							matched += bet.Matched
							details = append(details, bet.Matches...)
						}
					}
				}
				add(signal.Key+suffix, signal.Title, matched, details)
			}
		case len(signal.Buckets) > 0:
			details := []statisticsDetail{}
			for _, bucket := range signal.Buckets {
				details = append(details, bucket.Matches...)
				add(bucket.Key, bucket.Title, bucket.Matched, bucket.Matches)
			}
			add(signal.Key, signal.Title, signal.Matched, details)
		default:
			add(signal.Key, signal.Title, signal.Matched, signal.Matches)
		}
	}

	rows := make([]underKingRow, 0, len(underKingKeep))
	for _, keep := range underKingKeep {
		row, ok := candidates[keep.key]
		if !ok {
			continue
		}
		if keep.title != "" {
			row.Title = keep.title
		}
		rows = append(rows, row)
	}
	// 小球王看的就是谁最准：命中率高的排前面，同命中率场次多的在前。
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Accuracy != rows[j].Accuracy {
			return rows[i].Accuracy > rows[j].Accuracy
		}
		return rows[i].Matched > rows[j].Matched
	})

	return gin.H{
		"settled_total":   src.SettledTotal,
		"start_date":      src.StartDate,
		"end_date":        src.EndDate,
		"generated_at":    src.GeneratedAt,
		"needs_recompute": false,
		"rows":            rows,
	}, nil
}

// underKingMakeRow 从一批明细里筛出判小球的场次并结算成一行。没有就返回 false。
func underKingMakeRow(key, title string, srcMatched int, details []statisticsDetail) (underKingRow, bool) {
	kept := make([]statisticsDetail, 0, len(details))
	hit := 0
	for _, detail := range details {
		if !underKingIsUnderPick(detail.Pick) {
			continue
		}
		kept = append(kept, detail)
		if detail.Hit {
			hit++
		}
	}
	if len(kept) == 0 {
		return underKingRow{}, false
	}
	matched := len(kept)
	row := underKingRow{
		Key: key, Title: title,
		Matched: matched, Hit: hit, Miss: matched - hit,
		Accuracy: math.Round(float64(hit)/float64(matched)*10000) / 100,
		Matches:  kept,
	}
	if matched < srcMatched {
		row.Note = fmt.Sprintf("该维度在基础统计里共结算 %d 场，本行只取其中推荐=小球的 %d 场，命中率按这部分重算。", srcMatched, matched)
	} else {
		row.Note = "该维度本身只判小球方向，全部场次原样保留。"
	}
	if len(row.Matches) > underKingDetailCap {
		row.Matches = row.Matches[:underKingDetailCap]
	}
	return row, true
}

// underKingIsUnderPick 判断一条明细的推荐方向是不是【小球】。基础统计各维度的推荐
// 文案并不统一（「小球」「买小」「回归小球」「反大过热·买小」…），所以先排除带大球
// 标记的，再认小球标记；两种标记都没有的（主胜 / 主队赢盘 / 3球…）说明这个维度压根
// 不结算在大小球上，一律不收。
func underKingIsUnderPick(pick string) bool {
	if strings.Contains(pick, "大球") || strings.Contains(pick, "买大") {
		return false
	}
	return strings.Contains(pick, "小球") || strings.Contains(pick, "买小")
}
