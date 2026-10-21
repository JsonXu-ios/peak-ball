package drawmodel

import "time"

const HistoricalVersion = "draw-historical-v1"

// HistoricalExample is a reconstructed pre-match row. Base, when supplied,
// must have been generated from information available at AsOf; it is never a
// prediction produced with the target's result. Unverifiable existing analysis
// may still be compared, but must be marked Exploratory.
type HistoricalExample struct {
	Target         Match
	AsOf           time.Time
	History        []Match
	Odds           *Odds
	Draw           bool
	Base           *Result
	Features       map[string]float64
	OddsProvenance string
	Exploratory    bool
}

type HistoricalPrediction struct {
	ID                 string             `json:"id"`
	Partition          string             `json:"partition"`
	Available          bool               `json:"available"`
	Draw               bool               `json:"draw"`
	Probability        *float64           `json:"probability,omitempty"`
	GroupProbabilities map[string]float64 `json:"group_probabilities"`
	GroupExploratory   map[string]bool    `json:"group_exploratory"`
	GroupPartitions    map[string]string  `json:"group_partitions"`
	OutOfTime          bool               `json:"out_of_time"`
	Exploratory        bool               `json:"exploratory"`
	OddsProvenance     string             `json:"odds_provenance,omitempty"`
	Warnings           []string           `json:"warnings"`
}

type HistoricalCalibrationBin struct {
	Lower           float64 `json:"lower"`
	Upper           float64 `json:"upper"`
	Samples         int     `json:"samples"`
	MeanProbability float64 `json:"mean_probability"`
	DrawRate        float64 `json:"draw_rate"`
}

// Threshold is learned only from the calibration period's probabilities. The
// test results do not determine thresholds, parameters, or the selected model.
type HistoricalMetrics struct {
	Samples         int                        `json:"samples"`
	Draws           int                        `json:"draws"`
	DrawRate        float64                    `json:"draw_rate"`
	Brier           float64                    `json:"brier"`
	LogLoss         float64                    `json:"log_loss"`
	Threshold       float64                    `json:"threshold"`
	Selected        int                        `json:"selected"`
	Hits            int                        `json:"hits"`
	Precision       float64                    `json:"precision"`
	Recall          float64                    `json:"recall"`
	Coverage        float64                    `json:"coverage"`
	Top20Selected   int                        `json:"top20_selected"`
	Top20Hits       int                        `json:"top20_hits"`
	Top20Precision  float64                    `json:"top20_precision"`
	CalibrationBins []HistoricalCalibrationBin `json:"calibration_bins"`
}

type HistoricalComparison struct {
	Name               string            `json:"name"`
	Label              string            `json:"label"`
	FeatureNames       []string          `json:"feature_names"`
	Fitted             bool              `json:"fitted"`
	Calibrated         bool              `json:"calibrated"`
	Exploratory        bool              `json:"exploratory"`
	TrainingSamples    int               `json:"training_samples"`
	CalibrationSamples int               `json:"calibration_samples"`
	MinimumTraining    int               `json:"minimum_training"`
	IndependentSplit   bool              `json:"independent_split"`
	Test               HistoricalMetrics `json:"test"`
	CommonTest         HistoricalMetrics `json:"common_test"`
	Warnings           []string          `json:"warnings"`
}

type HistoricalSplit struct {
	TrainingSamples    int        `json:"training_samples"`
	CalibrationSamples int        `json:"calibration_samples"`
	TestSamples        int        `json:"test_samples"`
	PurgedSamples      int        `json:"purged_samples"`
	ExcludedSamples    int        `json:"excluded_samples"`
	CommonTestSamples  int        `json:"common_test_samples"`
	TrainingEnd        *time.Time `json:"training_end,omitempty"`
	CalibrationStart   *time.Time `json:"calibration_start,omitempty"`
	CalibrationEnd     *time.Time `json:"calibration_end,omitempty"`
	TestStart          *time.Time `json:"test_start,omitempty"`
}

type HistoricalModelArtifact struct {
	Name                 string             `json:"name"`
	FeatureNames         []string           `json:"feature_names"`
	Means                []float64          `json:"means"`
	Scales               []float64          `json:"scales"`
	Coefficients         []float64          `json:"coefficients"`
	Intercept            float64            `json:"intercept"`
	CalibrationIntercept float64            `json:"calibration_intercept"`
	CalibrationSlope     float64            `json:"calibration_slope"`
	Fitted               bool               `json:"fitted"`
	Calibrated           bool               `json:"calibrated"`
	Exploratory          bool               `json:"exploratory"`
	TrainingSamples      int                `json:"training_samples"`
	CalibrationSamples   int                `json:"calibration_samples"`
	TrainingCutoff       time.Time          `json:"training_cutoff"`
	Threshold            float64            `json:"threshold"`
	SelectionLogLoss     float64            `json:"selection_log_loss"`
	ImputationValues     map[string]float64 `json:"imputation_values,omitempty"`
}

// Models contain every fitted ablation, including clearly marked independent
// exploratory experiments. SelectedModel is chosen on the main calibration
// period among non-exploratory groups and excludes all main test labels.
type HistoricalArtifact struct {
	Version        string                             `json:"version"`
	SelectedModel  string                             `json:"selected_model"`
	Models         map[string]HistoricalModelArtifact `json:"models"`
	TrainingCutoff time.Time                          `json:"training_cutoff"`
	Warnings       []string                           `json:"warnings"`
}

type HistoricalEvaluation struct {
	Version  string                 `json:"version"`
	Rows     []HistoricalPrediction `json:"rows"`
	Models   []HistoricalComparison `json:"models"`
	Split    HistoricalSplit        `json:"split"`
	Artifact *HistoricalArtifact    `json:"artifact,omitempty"`
	Warnings []string               `json:"warnings"`
}
