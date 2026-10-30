export interface ResearchColumn {
  name: string
  type: string
  nullable: boolean
  primaryKey: boolean
  comment: string
}

export interface ResearchTable {
  name: string
  label: string
  layer: string
  description: string
  rows: number
  columns: ResearchColumn[]
  indexes: Array<{ name: string; unique: boolean; columns: string[] }>
  foreignKeys: Array<{ column: string; referencedTable: string; referencedColumn: string }>
}

export interface ResearchOverview {
  database: string
  schemaVersion: string
  generatedAt: string
  tables: ResearchTable[]
  summary: {
    matches: number
    ingestEvents: number
    quotes: number
    featureDefinitions: number
    featureSnapshots: number
    predictions: number
    auditIssues: number
    datasets: number
    evaluations: number
  }
  quality: {
    liveSnapshots: number
    reconstructedSnapshots: number
    openIssues: number
    bySeverity: Record<string, number>
  }
  featureSets: Array<{ version: string; features: number }>
}

export interface ResearchSyncStatus {
  state: 'idle' | 'running' | 'completed' | 'failed'
  runId?: number
  startedAt?: string
  finishedAt?: string
  processed: number
  total: number
  message: string
  error?: string
}

export interface ResearchPage<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

export interface ResearchFeatureDefinition {
  version: string
  name: string
  type: string
  unit: string
  description: string
  formula: string
  origin: string
  missingPolicy: string
  codeHash: string
  metadata: Record<string, unknown>
}

export interface ResearchAuditIssue {
  id: number
  runId: number
  matchId: string
  severity: string
  code: string
  field: string
  message: string
  detail: Record<string, unknown>
  createdAt: string
}

export type ResearchAuditPage = ResearchPage<ResearchAuditIssue> & { runId: number }

export interface ResearchEvaluationGroup {
  eligible: number
  draws: number
  selected: number
  hits: number
  precision: number | null
  recall: number | null
  scoreCandidates: number
  scoreHits: number
  scoreHitRate: number | null
  /** Optional until the analysis service loads separate top-one settlement. */
  firstScoreHits?: number
  firstScoreHitRate?: number | null
}

export interface ResearchEvaluation {
  id: number
  scope: string
  version: string
  createdAt: string
  metrics: {
    groups: Partial<Record<'original' | 'secondary' | 'enhanced', ResearchEvaluationGroup>>
    pending?: number
    settled?: number
    historicalAssociationOnly?: boolean
    independentOfHistoricalReconstruction?: boolean
    scoreMethod?: string
    secondaryVersion?: string
  }
}
