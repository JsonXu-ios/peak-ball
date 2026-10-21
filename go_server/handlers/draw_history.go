package handlers

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go_server/database"
	"go_server/internal/drawmodel"
	"go_server/models"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type drawHistoricalRow struct {
	MatchID             string               `json:"matchId"`
	Date                string               `json:"date"`
	MatchTime           time.Time            `json:"matchTime"`
	League              string               `json:"league"`
	Home                string               `json:"home"`
	Guest               string               `json:"guest"`
	HomeScore           int                  `json:"homeScore"`
	AwayScore           int                  `json:"awayScore"`
	IsDraw              bool                 `json:"isDraw"`
	Probability         *float64             `json:"probability"`
	RawProbability      *float64             `json:"rawProbability"`
	MarketProbability   *float64             `json:"marketProbability"`
	Selected            bool                 `json:"selected"`
	Outcome             string               `json:"outcome"`
	Split               string               `json:"split"`
	Sources             []string             `json:"sources"`
	Warnings            []string             `json:"warnings"`
	Signals             []drawExistingSignal `json:"signals"`
	OutOfTime           bool                 `json:"outOfTime"`
	Exploratory         bool                 `json:"exploratory"`
	Eligible            bool                 `json:"eligible"`
	GroupProbabilities  map[string]float64   `json:"groupProbabilities"`
	GroupPartitions     map[string]string    `json:"groupPartitions"`
	HomeRecent          drawFormResponse     `json:"homeRecent"`
	AwayRecent          drawFormResponse     `json:"awayRecent"`
	HomeVenue           drawFormResponse     `json:"homeVenue"`
	AwayVenue           drawFormResponse     `json:"awayVenue"`
	HomeExpectedGoals   *float64             `json:"homeExpectedGoals"`
	AwayExpectedGoals   *float64             `json:"awayExpectedGoals"`
	BaselineProbability *float64             `json:"baselineProbability"`
	BaselineSample      int                  `json:"baselineSample"`
	OddsProvenance      string               `json:"oddsProvenance"`
}

type drawHistoricalReport struct {
	ModelVersion string                         `json:"modelVersion"`
	GeneratedAt  time.Time                      `json:"generatedAt"`
	Rows         []drawHistoricalRow            `json:"rows"`
	Sources      []drawHistorySource            `json:"sources"`
	Evaluation   drawmodel.HistoricalEvaluation `json:"evaluation"`
	Notes        []string                       `json:"notes"`
}

type drawHistoricalProgress struct {
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Phase string `json:"phase"`
}

var drawHistoryState struct {
	sync.RWMutex
	Running  bool
	Progress drawHistoricalProgress
	Error    string
	Report   *drawHistoricalReport
	Artifact *drawmodel.HistoricalArtifact
	Active   bool
	Loaded   bool
	Pool     []drawmodel.Match
}

func encodeDrawArchive(value any) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func decodeDrawArchive(data []byte, value any) error {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer reader.Close()
	return json.NewDecoder(io.LimitReader(reader, 256<<20)).Decode(value)
}

func loadDrawHistoricalState() error {
	drawHistoryState.Lock()
	defer drawHistoryState.Unlock()
	if drawHistoryState.Loaded {
		return nil
	}
	var run models.DrawHistoricalRun
	err := database.DB.Order("id DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		drawHistoryState.Loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	var report drawHistoricalReport
	if err := decodeDrawArchive(run.Report, &report); err != nil {
		return err
	}
	drawHistoryState.Report = &report
	var examples []drawmodel.HistoricalExample
	if err := decodeDrawArchive(run.Examples, &examples); err != nil {
		return err
	}
	for _, e := range examples {
		drawHistoryState.Pool = append(drawHistoryState.Pool, e.Target)
	}
	var activeRun models.DrawHistoricalRun
	activeError := database.DB.Where("active = ?", true).Order("id DESC").First(&activeRun).Error
	if activeError != nil && !errors.Is(activeError, gorm.ErrRecordNotFound) {
		return activeError
	}
	drawHistoryState.Active = activeRun.ID > 0
	if activeRun.ID > 0 && len(activeRun.Artifact) > 0 && string(activeRun.Artifact) != "null" {
		var artifact drawmodel.HistoricalArtifact
		if json.Unmarshal(activeRun.Artifact, &artifact) == nil {
			drawHistoryState.Artifact = &artifact
		}
	}
	drawHistoryState.Loaded = true
	return nil
}

func activeDrawHistoricalArtifact() *drawmodel.HistoricalArtifact {
	if loadDrawHistoricalState() != nil {
		return nil
	}
	drawHistoryState.RLock()
	defer drawHistoryState.RUnlock()
	if !drawHistoryState.Active {
		return nil
	}
	return drawHistoryState.Artifact
}

func drawHistoricalModelVersion(artifact *drawmodel.HistoricalArtifact) string {
	if artifact == nil {
		return drawmodel.HistoricalVersion
	}
	return artifact.Version + ":" + artifact.SelectedModel + ":" + artifact.TrainingCutoff.UTC().Format("20060102T150405Z")
}

func drawHistoricalCanActivate(artifact *drawmodel.HistoricalArtifact) bool {
	if artifact == nil {
		return false
	}
	selected, ok := artifact.Models[artifact.SelectedModel]
	return ok && selected.Fitted && selected.Calibrated && !selected.Exploratory
}

func drawHistoricalVoteFeatures(features map[string]float64, signals []drawExistingSignal) map[string]float64 {
	if len(signals) == 0 {
		return features
	}
	if features == nil {
		features = map[string]float64{}
	}
	counts := map[string]float64{}
	for _, signal := range signals {
		for _, direction := range strings.Split(signal.Direction, ",") {
			counts[direction]++
		}
	}
	features["signalTriggeredCount"] = float64(len(signals))
	for _, direction := range []string{"draw", "home", "away", "over", "under"} {
		features["signalShare_"+direction] = counts[direction] / float64(len(signals))
	}
	return features
}

func drawHistoryJobProgress(done, total int, phase string) {
	drawHistoryState.Lock()
	drawHistoryState.Progress = drawHistoricalProgress{done, total, phase}
	drawHistoryState.Unlock()
}

func startDrawHistoricalJob(existing []drawExistingAnalysis, calibrate bool) bool {
	drawHistoryState.Lock()
	if drawHistoryState.Running {
		drawHistoryState.Unlock()
		return false
	}
	drawHistoryState.Running = true
	drawHistoryState.Error = ""
	drawHistoryState.Progress = drawHistoricalProgress{Phase: "读取全部历史及已有分析"}
	drawHistoryState.Unlock()
	go func() {
		err := runDrawHistoricalJob(existing, calibrate)
		drawHistoryState.Lock()
		defer drawHistoryState.Unlock()
		drawHistoryState.Running = false
		if err != nil {
			drawHistoryState.Error = err.Error()
			drawHistoryState.Progress.Phase = "计算失败"
		} else {
			drawHistoryState.Progress.Phase = "完成"
		}
	}()
	return true
}

// RebuildDrawHistory reconstructs historical model inputs and does not insert
// synthetic forecasts into the genuine pre-match draw_predictions table.
func RebuildDrawHistory(c *gin.Context) {
	var request struct {
		Existing []drawExistingAnalysis `json:"existingAnalysis"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "历史重建参数无效"})
			return
		}
	}
	startDrawHistoricalJob(request.Existing, false)
	c.JSON(http.StatusAccepted, gin.H{"running": true, "message": "正在展开全部历史，按比赛时间重建模型并计算碰撞结果"})
}

// CalibrateDrawHistory fits exclusively the historical training/calibration
// segments and activates their selected non-exploratory model for future games.
func CalibrateDrawHistory(c *gin.Context) {
	if err := loadDrawHistoricalState(); err != nil {
		drawHTTPError(c, err)
		return
	}
	drawHistoryState.RLock()
	hasReport := drawHistoryState.Report != nil
	drawHistoryState.RUnlock()
	if !hasReport {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请先重建历史模型，再使用历史样本校准"})
		return
	}
	startDrawHistoricalJob(nil, true)
	c.JSON(http.StatusAccepted, gin.H{"running": true, "message": "正在使用历史训练及校准区间拟合，独立测试区间只用于检验"})
}

func runDrawHistoricalJob(existing []drawExistingAnalysis, calibrate bool) error {
	var report drawHistoricalReport
	var examples []drawmodel.HistoricalExample
	if calibrate {
		var run models.DrawHistoricalRun
		if err := database.DB.Order("id DESC").First(&run).Error; err != nil {
			return err
		}
		if err := decodeDrawArchive(run.Report, &report); err != nil {
			return err
		}
		if err := decodeDrawArchive(run.Examples, &examples); err != nil {
			return err
		}
		drawHistoryJobProgress(0, len(examples), "历史训练与概率校准")
	} else {
		data, err := loadDrawHistoricalData(existing)
		if err != nil {
			return err
		}
		index := indexDrawHistory(data.Games)
		examples = make([]drawmodel.HistoricalExample, len(data.Games))
		report.Rows = make([]drawHistoricalRow, len(data.Games))
		report.Sources = data.Sources
		report.Notes = []string{"历史回算不等于真实赛前留档；原始比分特征逐场截断至当时，目标比赛结果只用于结算。", "训练/校准/独立测试按时间约60%/20%/20%切分；独立测试标签不参与模型、校准或阈值选择。", "缺少赛前采集时间的初赔、盘口及存量分析信号保留用于探索对照，不冒充已验证的赛前信息。", "全量包含当前比赛表、旧版比赛表和双方战绩/交锋中的历史比赛；不能计算的早期样本仍列出并说明原因。"}
		jobs := make(chan int)
		var wg sync.WaitGroup
		var progressMu sync.Mutex
		done := 0
		for worker := 0; worker < 4; worker++ {
			wg.Go(func() {
				for i := range jobs {
					g := data.Games[i]
					asOf := g.Game.Kickoff.Add(-time.Second)
					history := index.before(g.Game, asOf)
					quote, provenance := historicalDrawOdds(data, g, asOf)
					base := drawmodel.Predict(drawmodel.Input{Target: g.Game, AsOf: asOf, History: history, Odds: quote})
					e := drawmodel.HistoricalExample{Target: g.Game, AsOf: asOf, Draw: g.Game.HomeGoals == g.Game.AwayGoals, Base: &base, Odds: quote, OddsProvenance: provenance}
					analysis := data.Existing[g.Game.ID]
					e.Features = analysis.Features
					e.Exploratory = len(analysis.Features) > 0
					examples[i] = e
					row := drawHistoricalRow{MatchID: g.Game.ID, Date: g.Game.Kickoff.In(drawSourceLocation).Format("2006-01-02"), MatchTime: g.Game.Kickoff, League: g.Game.LeagueName, Home: g.Game.HomeTeamName, Guest: g.Game.AwayTeamName, HomeScore: g.Game.HomeGoals, AwayScore: g.Game.AwayGoals, IsDraw: e.Draw, Sources: g.Sources, Warnings: append([]string{}, g.Warnings...), Signals: append([]drawExistingSignal{}, analysis.Signals...), GroupProbabilities: map[string]float64{}, Split: "excluded"}
					if base.Available {
						row.RawProbability = drawFloat(drawPercent(base.ScoreModelProbability))
						row.MarketProbability = nil
						if base.MarketProbability != nil {
							row.MarketProbability = drawFloat(drawPercent(*base.MarketProbability))
						}
					}
					if !base.Available {
						row.Warnings = append(row.Warnings, "该场之前缺少可计算历史，保留明细但不计入模型命中率")
					}
					if provenance == "opening_reconstructed" {
						row.Warnings = append(row.Warnings, "欧赔/盘口按存量初盘重建，赛前采集时间未验证")
					}
					if len(analysis.Signals) > 0 {
						row.Warnings = append(row.Warnings, "已有分析信号为存量重算，单独标记探索性碰撞")
					}
					report.Rows[i] = row
					progressMu.Lock()
					done++
					if done%25 == 0 || done == len(examples) {
						drawHistoryJobProgress(done, len(examples), "逐场重建历史概率")
					}
					progressMu.Unlock()
				}
			})
		}
		for i := range data.Games {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
	}
	report.Notes = []string{"历史回算不等于真实赛前留档；原始比分特征逐场截断至当时，目标比赛结果只用于结算。", "主模型训练/校准/独立测试按时间约60%/20%/20%切分；融合参数、校准与阈值选择不使用测试标签，滚动比分特征只使用当场之前已结束的比赛。", "近期才有数据的对照组使用各自时间切分并标明探索；比较时同时核对各组样本及共同测试交集。", "缺少赛前采集时间的初赔、盘口及存量分析信号保留用于探索对照，不冒充已验证的赛前信息。", "全量包含当前比赛表、旧版比赛表和双方战绩/交锋中的历史比赛；不能计算的早期样本仍列出并说明原因。"}
	drawHistoryJobProgress(len(examples), len(examples), "拟合对照模型及独立测试")
	for i := range examples {
		examples[i].Features = drawHistoricalVoteFeatures(examples[i].Features, report.Rows[i].Signals)
		if len(report.Rows[i].Signals) > 0 {
			examples[i].Exploratory = true
		}
		row := &report.Rows[i]
		base := examples[i].Base
		if base != nil {
			row.HomeRecent = drawForm(base.HomeOverall, "all")
			row.AwayRecent = drawForm(base.AwayOverall, "all")
			row.HomeVenue = drawForm(base.HomeVenue, "home")
			row.AwayVenue = drawForm(base.AwayVenue, "away")
			if base.Available {
				row.HomeExpectedGoals = drawFloat(round2(base.HomeLambda))
				row.AwayExpectedGoals = drawFloat(round2(base.AwayLambda))
				row.BaselineProbability = drawFloat(drawPercent(base.LeagueBaseline.DrawRate))
				row.BaselineSample = base.LeagueBaseline.N
			}
		}
		row.OddsProvenance = examples[i].OddsProvenance
	}
	evaluation := drawmodel.FitHistorical(examples)
	report.Evaluation = evaluation
	report.ModelVersion = evaluation.Version
	report.GeneratedAt = time.Now().UTC()
	predictions := map[string]drawmodel.HistoricalPrediction{}
	for _, p := range evaluation.Rows {
		predictions[p.ID] = p
	}
	for i := range report.Rows {
		row := &report.Rows[i]
		p := predictions[row.MatchID]
		row.Split = p.Partition
		row.OutOfTime = p.OutOfTime
		row.Exploratory = p.Exploratory
		row.Eligible = p.Available
		row.Warnings = append(row.Warnings, p.Warnings...)
		row.GroupProbabilities = map[string]float64{}
		row.GroupPartitions = p.GroupPartitions
		for k, v := range p.GroupProbabilities {
			row.GroupProbabilities[k] = drawPercent(v)
		}
		if p.Probability != nil {
			row.Probability = drawFloat(drawPercent(*p.Probability))
		}
		if !p.Available {
			row.Probability = nil
		}
	}
	encoded, err := encodeDrawArchive(&report)
	if err != nil {
		return err
	}
	inputData, err := encodeDrawArchive(examples)
	if err != nil {
		return err
	}
	artifactJSON := []byte("null")
	if evaluation.Artifact != nil {
		artifactJSON, err = drawmodel.HistoricalArtifactJSON(evaluation.Artifact)
		if err != nil {
			return err
		}
	}
	active := calibrate && drawHistoricalCanActivate(evaluation.Artifact)
	if calibrate && !active {
		report.Notes = append(report.Notes, "历史数据已回算，但本次样本分布尚不足以激活校准模型")
		encoded, err = encodeDrawArchive(&report)
		if err != nil {
			return err
		}
	}
	run := models.DrawHistoricalRun{ModelVersion: report.ModelVersion, GeneratedAt: report.GeneratedAt, Samples: len(report.Rows), Active: active, Report: encoded, Examples: inputData, Artifact: datatypes.JSON(artifactJSON)}
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		if active {
			if err := tx.Model(&models.DrawHistoricalRun{}).Where("active = ?", true).Update("active", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(&run).Error
	}); err != nil {
		return err
	}
	drawHistoryState.Lock()
	drawHistoryState.Report = &report
	drawHistoryState.Loaded = true
	drawHistoryState.Pool = make([]drawmodel.Match, 0, len(examples))
	for _, e := range examples {
		drawHistoryState.Pool = append(drawHistoryState.Pool, e.Target)
	}
	if active {
		drawHistoryState.Artifact = evaluation.Artifact
		drawHistoryState.Active = true
	}
	drawHistoryState.Unlock()
	return nil
}

type drawHistoricalSummary struct {
	Total          int      `json:"total"`
	Eligible       int      `json:"eligible"`
	ActualDraws    int      `json:"actualDraws"`
	Selected       int      `json:"selected"`
	Hits           int      `json:"hits"`
	FalsePositives int      `json:"falsePositives"`
	FalseNegatives int      `json:"falseNegatives"`
	TrueNegatives  int      `json:"trueNegatives"`
	Precision      *float64 `json:"precision"`
	Recall         *float64 `json:"recall"`
	Coverage       *float64 `json:"coverage"`
	DrawRate       *float64 `json:"drawRate"`
	Brier          *float64 `json:"brier"`
	MarketBrier    *float64 `json:"marketBrier"`
}

func classifyDrawHistoricalRow(row drawHistoricalRow, threshold float64) drawHistoricalRow {
	row.Selected = row.Probability != nil && *row.Probability >= threshold
	switch {
	case row.Probability == nil:
		row.Outcome = "unavailable"
	case row.Selected && row.IsDraw:
		row.Outcome = "hit"
	case row.Selected:
		row.Outcome = "false_positive"
	case row.IsDraw:
		row.Outcome = "missed"
	default:
		row.Outcome = "correct_rejection"
	}
	return row
}

func summarizeDrawHistoricalRows(rows []drawHistoricalRow) (drawHistoricalSummary, []drawCalibrationBin) {
	s := drawHistoricalSummary{Total: len(rows)}
	var errorSum, marketError float64
	marketN, eligibleDraws := 0, 0
	bins := [10]struct {
		n                 int
		predicted, actual float64
	}{}
	for _, r := range rows {
		if r.IsDraw {
			s.ActualDraws++
		}
		if r.Probability == nil {
			continue
		}
		s.Eligible++
		p := *r.Probability / 100
		y := 0.0
		if r.IsDraw {
			y = 1
			eligibleDraws++
		}
		errorSum += (p - y) * (p - y)
		if r.MarketProbability != nil {
			m := *r.MarketProbability / 100
			marketError += (m - y) * (m - y)
			marketN++
		}
		switch r.Outcome {
		case "hit":
			s.Hits++
			s.Selected++
		case "false_positive":
			s.FalsePositives++
			s.Selected++
		case "missed":
			s.FalseNegatives++
		case "correct_rejection":
			s.TrueNegatives++
		}
		i := int(p * 10)
		if i > 9 {
			i = 9
		}
		if i < 0 {
			i = 0
		}
		bins[i].n++
		bins[i].predicted += p
		bins[i].actual += y
	}
	s.Precision = drawRatio(s.Hits, s.Selected, 100)
	s.Recall = drawRatio(s.Hits, eligibleDraws, 100)
	s.Coverage = drawRatio(s.Selected, s.Eligible, 100)
	s.DrawRate = drawRatio(s.ActualDraws, s.Total, 100)
	s.Brier = drawAverage(errorSum, s.Eligible)
	s.MarketBrier = drawAverage(marketError, marketN)
	result := []drawCalibrationBin{}
	for i, b := range bins {
		if b.n > 0 {
			result = append(result, drawCalibrationBin{From: float64(i * 10), To: float64((i + 1) * 10), Count: b.n, Predicted: 100 * b.predicted / float64(b.n), Actual: 100 * b.actual / float64(b.n)})
		}
	}
	return s, result
}

func drawHistoryComparisons(report *drawHistoricalReport, rows []drawHistoricalRow, threshold float64) []gin.H {
	result := []gin.H{}
	for _, model := range report.Evaluation.Models {
		group := []drawHistoricalRow{}
		for _, row := range rows {
			partition := row.Split
			if ownPartition, ok := row.GroupPartitions[model.Name]; ok {
				partition = ownPartition
			}
			if partition != "test" {
				continue
			}
			if p, ok := row.GroupProbabilities[model.Name]; ok {
				row.Probability = drawFloat(p)
				group = append(group, classifyDrawHistoricalRow(row, threshold))
			}
		}
		s, _ := summarizeDrawHistoricalRows(group)
		notes := strings.Join(model.Warnings, "；")
		if model.Exploratory {
			notes = "探索对照：含缺少赛前时点证明的数据。" + notes
		}
		notes += fmt.Sprintf("全报告固定共同测试交集 %d 场（不随筛选变化）；各组覆盖不同请同时核对样本数。", model.CommonTest.Samples)
		var commonBrier *float64
		if model.CommonTest.Samples > 0 {
			commonBrier = drawFloat(model.CommonTest.Brier)
		}
		result = append(result, gin.H{"key": model.Name, "label": model.Label, "sample": s.Eligible, "selected": s.Selected, "hits": s.Hits, "precision": s.Precision, "coverage": s.Coverage, "recall": s.Recall, "brier": s.Brier, "exploratory": model.Exploratory, "notes": notes, "commonSample": model.CommonTest.Samples, "commonBrier": commonBrier, "independentSplit": model.IndependentSplit})
	}
	return result
}

func drawHistorySignalSummary(rows []drawHistoricalRow) []gin.H {
	type total struct {
		label    string
		n, hits  int
		drawPick bool
	}
	counts := map[string]*total{}
	for _, r := range rows {
		for _, s := range r.Signals {
			key := s.Key + ":" + s.Direction
			t := counts[key]
			if t == nil {
				t = &total{label: s.Label + " → " + s.Direction, drawPick: drawContains(strings.Split(s.Direction, ","), "draw")}
				counts[key] = t
			}
			t.n++
			if r.IsDraw {
				t.hits++
			}
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return counts[keys[i]].n > counts[keys[j]].n })
	result := []gin.H{}
	for _, key := range keys {
		t := counts[key]
		var precision *float64
		if t.drawPick {
			precision = drawRatio(t.hits, t.n, 100)
		}
		result = append(result, gin.H{"key": key, "label": t.label, "sample": t.n, "hits": t.hits, "precision": precision, "drawRate": drawRatio(t.hits, t.n, 100), "notes": "存量分析重算的探索碰撞；非选平方向显示触发时的实际平局率"})
	}
	return result
}

func drawHistoryContributions(rows []drawHistoricalRow) []gin.H {
	result := []gin.H{}
	for _, source := range []struct{ key, label string }{{"history_moneys", "历史战绩与主客场近况"}, {"money", "旧版比赛数据"}, {"moneys", "当前比赛数据"}, {"market", "欧赔/盘口对照"}, {"existing", "已有分析目录"}} {
		n, d := 0, 0
		for _, r := range rows {
			keep := drawContains(r.Sources, source.key) || (source.key == "market" && r.MarketProbability != nil) || (source.key == "existing" && len(r.Signals) > 0)
			if keep {
				n++
				if r.IsDraw {
					d++
				}
			}
		}
		result = append(result, gin.H{"key": source.key, "label": source.label, "sample": n, "drawRate": drawRatio(d, n, 100), "notes": "此表描述可用数据覆盖和实际平局分布；预测增益应对照上方模型分组及共同测试样本"})
	}
	return result
}

// GetDrawHistory always returns all historical fixtures through server-side
// pagination. Filters do not erase unavailable rows or hide model failures.
func GetDrawHistory(c *gin.Context) {
	if err := loadDrawHistoricalState(); err != nil {
		drawHTTPError(c, err)
		return
	}
	for _, key := range []string{"start_date", "end_date"} {
		if value := c.Query(key); value != "" {
			if _, err := drawDate(value); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "历史筛选日期格式无效"})
				return
			}
		}
	}
	threshold := 32.0
	if raw := c.Query("threshold"); raw != "" {
		value := drawNumeric(raw)
		if value == nil || *value < 0 || *value > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "阈值应在0至100之间"})
			return
		}
		threshold = *value
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	drawHistoryState.RLock()
	report := drawHistoryState.Report
	running, progress, jobError, active := drawHistoryState.Running, drawHistoryState.Progress, drawHistoryState.Error, drawHistoryState.Active
	drawHistoryState.RUnlock()
	if c.Query("threshold") == "" && report != nil && report.Evaluation.Artifact != nil {
		if selected, ok := report.Evaluation.Artifact.Models[report.Evaluation.Artifact.SelectedModel]; ok && selected.Threshold > 0 {
			threshold = drawPercent(selected.Threshold)
		}
	}
	if report == nil {
		data, err := loadDrawHistoricalData(nil)
		if err != nil {
			drawHTTPError(c, err)
			return
		}
		empty := drawHistoricalReport{ModelVersion: drawmodel.HistoricalVersion, Sources: data.Sources, Rows: []drawHistoricalRow{}, Notes: []string{"点击重建历史模型，即可展开所有已有历史并逐场计算碰撞结果"}}
		for _, g := range data.Games {
			empty.Rows = append(empty.Rows, drawHistoricalRow{MatchID: g.Game.ID, Date: g.Game.Kickoff.In(drawSourceLocation).Format("2006-01-02"), MatchTime: g.Game.Kickoff, League: g.Game.LeagueName, Home: g.Game.HomeTeamName, Guest: g.Game.AwayTeamName, HomeScore: g.Game.HomeGoals, AwayScore: g.Game.AwayGoals, IsDraw: g.Game.HomeGoals == g.Game.AwayGoals, Sources: g.Sources, Split: "excluded", Warnings: []string{"尚未计算"}, Signals: []drawExistingSignal{}, GroupProbabilities: map[string]float64{}})
		}
		report = &empty
	}
	all := []drawHistoricalRow{}
	testRows := []drawHistoricalRow{}
	items := []drawHistoricalRow{}
	leagueSet := map[string]bool{}
	start, end := "", ""
	for _, raw := range report.Rows {
		leagueSet[raw.League] = true
		if start == "" || raw.Date < start {
			start = raw.Date
		}
		if raw.Date > end {
			end = raw.Date
		}
		if value := c.Query("start_date"); value != "" && raw.Date < value {
			continue
		}
		if value := c.Query("end_date"); value != "" && raw.Date > value {
			continue
		}
		if league := c.Query("league"); league != "" && league != "all" && raw.League != league {
			continue
		}
		if search := strings.TrimSpace(c.Query("search")); search != "" && !strings.Contains(strings.ToLower(raw.Home+" "+raw.Guest+" "+raw.MatchID), strings.ToLower(search)) {
			continue
		}
		row := classifyDrawHistoricalRow(raw, threshold)
		if row.Split == "test" {
			testRows = append(testRows, row)
		}
		all = append(all, row)
		if split := c.Query("split"); split != "" && split != "all" && row.Split != split {
			continue
		}
		outcome := c.Query("outcome")
		if outcome != "" && outcome != "all" {
			switch outcome {
			case "draw":
				if !row.IsDraw {
					continue
				}
			case "selected":
				if !row.Selected {
					continue
				}
			default:
				if row.Outcome != outcome {
					continue
				}
			}
		}
		items = append(items, row)
	}
	summary, bins := summarizeDrawHistoricalRows(items)
	testSummary, _ := summarizeDrawHistoricalRows(testRows)
	sort.SliceStable(items, func(i, j int) bool { return items[i].MatchTime.After(items[j].MatchTime) })
	total := len(items)
	offset := (page - 1) * size
	if offset > total {
		offset = total
	}
	limit := offset + size
	if limit > total {
		limit = total
	}
	pageItems := items[offset:limit]
	leagues := make([]string, 0, len(leagueSet))
	for league := range leagueSet {
		leagues = append(leagues, league)
	}
	sort.Strings(leagues)
	model := gin.H{"calibrated": active, "trainingSamples": report.Evaluation.Split.TrainingSamples, "calibrationSamples": report.Evaluation.Split.CalibrationSamples, "testSamples": report.Evaluation.Split.TestSamples, "canCalibrate": drawHistoricalCanActivate(report.Evaluation.Artifact), "minSamples": 80, "activeGroup": "", "recommendedThreshold": threshold, "message": "历史训练、后段校准、独立测试分开；阈值由校准段确定，手动调整用于探索"}
	if report.Evaluation.Artifact != nil {
		model["activeGroup"] = report.Evaluation.Artifact.SelectedModel
		if selected, ok := report.Evaluation.Artifact.Models[report.Evaluation.Artifact.SelectedModel]; ok {
			model["recommendedThreshold"] = drawPercent(selected.Threshold)
		}
	}
	notes := append([]string{}, report.Notes...)
	if jobError != "" {
		notes = append(notes, "最近计算失败："+jobError)
	}
	c.JSON(http.StatusOK, gin.H{"threshold": threshold, "running": running, "progress": progress, "modelVersion": report.ModelVersion, "generatedAt": report.GeneratedAt, "range": gin.H{"start": start, "end": end}, "total": total, "eligible": summary.Eligible, "page": page, "pageSize": size, "items": pageItems, "summary": summary, "testSummary": testSummary, "bins": bins, "comparisons": drawHistoryComparisons(report, all, threshold), "contributions": drawHistoryContributions(all), "sources": report.Sources, "signalsSummary": drawHistorySignalSummary(all), "leagues": leagues, "model": model, "notes": notes, "confusion": gin.H{"truePositive": summary.Hits, "falsePositive": summary.FalsePositives, "falseNegative": summary.FalseNegatives, "trueNegative": summary.TrueNegatives}})
}
