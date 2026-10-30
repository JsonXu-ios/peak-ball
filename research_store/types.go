// Package researchstore stores immutable source evidence, feature versions and
// predictions in an independent schema within the existing MySQL instance.
package researchstore

import (
	"encoding/json"
	"time"
)

const DefaultDatabase = "football_research"
const SchemaVersion = "001"

type IngestEvent struct {
	Source        string          `json:"source"`
	Kind          string          `json:"kind"`
	MatchID       string          `json:"matchId"`
	ObservedAt    time.Time       `json:"observedAt"`
	SourceAt      *time.Time      `json:"sourceAt,omitempty"`
	Provenance    string          `json:"provenance"` // live or reconstructed
	Payload       json.RawMessage `json:"payload"`
	ParserVersion string          `json:"parserVersion"`
	RequestURL    string          `json:"requestUrl,omitempty"`
	HTTPStatus    int             `json:"httpStatus,omitempty"`
}

type Entity struct {
	Key        string          `json:"key"`
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Source     string          `json:"source"`
	ExternalID string          `json:"externalId"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type EntityAlias struct {
	EntityKey  string `json:"entityKey"`
	Source     string `json:"source"`
	ExternalID string `json:"externalId"`
	Alias      string `json:"alias"`
	Confidence string `json:"confidence"`
}

type Match struct {
	MatchID       string    `json:"matchId"`
	HomeID        string    `json:"homeId"`
	AwayID        string    `json:"awayId"`
	Home          string    `json:"home"`
	Away          string    `json:"away"`
	LeagueID      string    `json:"leagueId"`
	League        string    `json:"league"`
	ScheduleDate  string    `json:"scheduleDate"` // source Beijing schedule day, not UTC date
	Kickoff       time.Time `json:"kickoff"`
	JingcaiID     string    `json:"jingcaiId"`
	SourceEventID *int64    `json:"sourceEventId,omitempty"`
}

type MarketQuote struct {
	MatchID       string          `json:"matchId"`
	Company       string          `json:"company"`
	Market        string          `json:"market"`
	Phase         string          `json:"phase"`
	SourceEventID *int64          `json:"sourceEventId,omitempty"`
	SourceAt      *time.Time      `json:"sourceAt,omitempty"`
	ObservedAt    time.Time       `json:"observedAt"`
	Provenance    string          `json:"provenance"`
	Home          *float64        `json:"home,omitempty"`
	Draw          *float64        `json:"draw,omitempty"`
	Away          *float64        `json:"away,omitempty"`
	Line          *float64        `json:"line,omitempty"`
	HomeWater     *float64        `json:"homeWater,omitempty"`
	AwayWater     *float64        `json:"awayWater,omitempty"`
	Raw           json.RawMessage `json:"raw,omitempty"`
}

type MatchResult struct {
	MatchID             string     `json:"matchId"`
	Home                *int       `json:"home,omitempty"`
	Away                *int       `json:"away,omitempty"`
	ExtraHome           *int       `json:"extraHome,omitempty"`
	ExtraAway           *int       `json:"extraAway,omitempty"`
	PenaltyHome         *int       `json:"penaltyHome,omitempty"`
	PenaltyAway         *int       `json:"penaltyAway,omitempty"`
	RegulationConfirmed bool       `json:"regulationConfirmed"`
	Status              string     `json:"status"`
	AvailableAt         *time.Time `json:"availableAt,omitempty"`
	ObservedAt          time.Time  `json:"observedAt"`
	SourceEventID       *int64     `json:"sourceEventId,omitempty"`
	Provenance          string     `json:"provenance"`
}

type FeatureDefinition struct {
	Version       string          `json:"version"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Unit          string          `json:"unit"`
	Description   string          `json:"description"`
	Formula       string          `json:"formula"`
	Origin        string          `json:"origin"`
	MissingPolicy string          `json:"missingPolicy"`
	CodeHash      string          `json:"codeHash"`
	Metadata      json.RawMessage `json:"metadata"`
}

type FeatureSnapshot struct {
	MatchID        string          `json:"matchId"`
	AsOf           time.Time       `json:"asOf"`
	Kickoff        time.Time       `json:"kickoff"`
	FeatureVersion string          `json:"featureVersion"`
	DataRevision   string          `json:"dataRevision"`
	Provenance     string          `json:"provenance"`
	KnownBy        *time.Time      `json:"knownBy,omitempty"`
	Quality        string          `json:"quality"`
	Values         json.RawMessage `json:"values"`
	Missing        json.RawMessage `json:"missing"`
	QualityFlags   json.RawMessage `json:"qualityFlags"`
	SourceEventIDs []int64         `json:"sourceEventIds"`
}

type Prediction struct {
	SnapshotID   int64           `json:"snapshotId"`
	MatchID      string          `json:"matchId"`
	ModelVersion string          `json:"modelVersion"`
	RuleVersion  string          `json:"ruleVersion"`
	PredictedAt  time.Time       `json:"predictedAt"`
	State        string          `json:"state"`
	Score        *float64        `json:"score,omitempty"`
	Threshold    *float64        `json:"threshold,omitempty"`
	Secondary    bool            `json:"secondary"`
	Enhanced     bool            `json:"enhanced"`
	ScoreMethod  string          `json:"scoreMethod"`
	Scorelines   json.RawMessage `json:"scorelines"`
	Payload      json.RawMessage `json:"payload"`
}

type Settlement struct {
	PredictionID int64     `json:"predictionId"`
	ResultID     int64     `json:"resultId"`
	IsDraw       bool      `json:"isDraw"`
	ScorelineHit bool      `json:"scorelineHit"`
	SettledAt    time.Time `json:"settledAt"`
}

type AuditRun struct {
	Kind      string          `json:"kind"`
	Version   string          `json:"version"`
	StartedAt time.Time       `json:"startedAt"`
	Metadata  json.RawMessage `json:"metadata"`
}

type AuditIssue struct {
	RunID         int64           `json:"runId"`
	MatchID       string          `json:"matchId"`
	SourceEventID *int64          `json:"sourceEventId,omitempty"`
	Severity      string          `json:"severity"`
	Code          string          `json:"code"`
	Field         string          `json:"field"`
	Message       string          `json:"message"`
	Detail        json.RawMessage `json:"detail"`
}

type Dataset struct {
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	FeatureVersion string          `json:"featureVersion"`
	Manifest       json.RawMessage `json:"manifest"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type DatasetMember struct {
	DatasetID  int64  `json:"datasetId"`
	SnapshotID int64  `json:"snapshotId"`
	ResultID   *int64 `json:"resultId,omitempty"`
	Split      string `json:"split"`
}

type ModelRun struct {
	Version      string          `json:"version"`
	DatasetID    int64           `json:"datasetId"`
	ArtifactHash string          `json:"artifactHash"`
	ArtifactURI  string          `json:"artifactUri"`
	Config       json.RawMessage `json:"config"`
	Status       string          `json:"status"`
	StartedAt    time.Time       `json:"startedAt"`
}

type Evaluation struct {
	ModelRunID *int64          `json:"modelRunId,omitempty"`
	DatasetID  *int64          `json:"datasetId,omitempty"`
	Version    string          `json:"version"`
	Scope      string          `json:"scope"`
	Metrics    json.RawMessage `json:"metrics"`
	CreatedAt  time.Time       `json:"createdAt"`
}

type ColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	Key        string `json:"key"`
	PrimaryKey bool   `json:"primaryKey"`
	Comment    string `json:"comment"`
}
type IndexInfo struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Columns []string `json:"columns"`
}
type TableInfo struct {
	Name        string           `json:"name"`
	Label       string           `json:"label"`
	Layer       string           `json:"layer"`
	Description string           `json:"description"`
	Rows        int64            `json:"rows"`
	Comment     string           `json:"comment"`
	Columns     []ColumnInfo     `json:"columns"`
	Indexes     []IndexInfo      `json:"indexes"`
	ForeignKeys []ForeignKeyInfo `json:"foreignKeys"`
}
type ForeignKeyInfo struct {
	Column           string `json:"column"`
	ReferencedTable  string `json:"referencedTable"`
	ReferencedColumn string `json:"referencedColumn"`
}
type Summary struct {
	Matches            int64 `json:"matches"`
	IngestEvents       int64 `json:"ingestEvents"`
	Quotes             int64 `json:"quotes"`
	FeatureDefinitions int64 `json:"featureDefinitions"`
	FeatureSnapshots   int64 `json:"featureSnapshots"`
	Predictions        int64 `json:"predictions"`
	AuditIssues        int64 `json:"auditIssues"`
	Datasets           int64 `json:"datasets"`
	Evaluations        int64 `json:"evaluations"`
}
type QualitySummary struct {
	LiveSnapshots          int64            `json:"liveSnapshots"`
	ReconstructedSnapshots int64            `json:"reconstructedSnapshots"`
	OpenIssues             int64            `json:"openIssues"`
	BySeverity             map[string]int64 `json:"bySeverity"`
}
type FeatureSetInfo struct {
	Version  string `json:"version"`
	Features int64  `json:"features"`
}
type Overview struct {
	Database      string           `json:"database"`
	SchemaVersion string           `json:"schemaVersion"`
	GeneratedAt   time.Time        `json:"generatedAt"`
	Tables        []TableInfo      `json:"tables"`
	Provenance    map[string]int64 `json:"provenance"`
	Summary       Summary          `json:"summary"`
	Quality       QualitySummary   `json:"quality"`
	FeatureSets   []FeatureSetInfo `json:"featureSets"`
	AuditIssues   map[string]int64 `json:"auditIssues"`
}
