// Offline source/SSR checks. Does not start services, control the desktop or train models.
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')
const assert = require('node:assert/strict')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const ts = adminRequire('typescript')
const vue = adminRequire('vue')
const { parse, compileScript } = adminRequire('@vue/compiler-sfc')
const { renderToString } = adminRequire('@vue/server-renderer')
const read = relative => fs.readFileSync(path.join(root, relative), 'utf8')
let checks = 0
const equal = (a, b, message) => { assert.deepEqual(a, b, message); checks++ }
const check = (condition, message) => { assert.ok(condition, message); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const secondary = load(read('admin/src/utils/drawSecondarySignal.ts'))
const score = load(read('admin/src/utils/drawScoreRecommendation.ts'), { './drawSecondarySignal': secondary })
const fixture = extra => ({ jingcaiId: '202610305002', frozenDrawSignal: { state: 'selected', score: .3 },
  secondaryDecision: { version: 'secondary-v2-input-guards', decision: 'retained', threshold: .225, score: .3, reason: '报价完整，赛前交锋8场', headToHeadSample: 8, minimumHeadToHead: 5, sportteryQuotesAvailable: true },
  scoreRecommendation: { version: 'draw-score-dynamic-v2', state: 'recommended', source: 'sporttery_score_odds', scores: [{ home: 2, away: 2 }, { home: 1, away: 1 }], reason: '按本场竞彩比分报价排序' },
  settled: false, regulationSettled: false, homeScore: 0, awayScore: 0, ...extra })
const choose = input => score.recommendDrawScores(input)
const present = input => score.drawScorePresentation(input)

async function main() {
  equal(score.DRAW_SCORE_RECOMMENDATION_VERSION, 'draw-score-dynamic-v2', 'dynamic recommendation version is explicit')
  equal(choose(fixture()).scores, ['2:2', '1:1'], 'backend score ordering is preserved')
  equal(choose(fixture({ scoreRecommendation: undefined })).state, 'unavailable', 'missing backend has no fixed-score fallback')
  equal(choose(fixture({ secondaryDecision: undefined })).state, 'unavailable', 'old secondary contract cannot recommend scores')
  const recorded = fixture().scoreRecommendation
  equal(choose(fixture({ scoreRecommendation: { ...recorded, scores: [{ home: 3, away: 3 }, { home: 2, away: 2 }] } })).scores, ['3:3', '2:2'], 'new ranking accepts higher valid draw scores')
  for (const bad of [
    { ...recorded, version: 'draw-score-baseline-v1' }, { ...recorded, state: 'not_selected' },
    { ...recorded, scores: [{ home: 2, away: 2 }, { home: 2, away: 2 }] },
    { ...recorded, scores: [{ home: 1, away: 0 }, { home: 2, away: 2 }] },
    { ...recorded, scores: [{ home: 7, away: 7 }, { home: 2, away: 2 }] },
  ]) equal(choose(fixture({ scoreRecommendation: bad })).state, 'unavailable', 'old or malformed score contracts cannot be displayed')
  equal(present(fixture({ settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 })).outcomeLabel, '首选命中', 'first dynamic score is a first-choice hit')
  equal(present(fixture({ settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 })).outcomeLabel, '次选命中（前二命中）', 'second dynamic score is a second-choice hit')
  const descriptor = parse(read('admin/src/views/statistics/DrawScoreRecommendation.vue')).descriptor
  const component = load(compileScript(descriptor, { id: 'score-dynamic-offline', inlineTemplate: true }).content, { '@/utils/drawScoreRecommendation': score }).default
  const states = [
    ['pending', fixture(), ['首选', '2:2', '次选', '1:1', '竞彩比分报价', '待赛果核对']],
    ['hit', fixture({ settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 }), ['首选命中']],
    ['second_hit', fixture({ settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 }), ['次选命中（前二命中）']],
    ['miss', fixture({ settled: true, regulationSettled: true, homeScore: 0, awayScore: 0 }), ['前二未命中']],
    ['not_selected', fixture({ secondaryDecision: { ...fixture().secondaryDecision, decision: 'rejected', reason: '赛前交锋不足5场' } }), ['未二次入选']],
    ['out_of_scope', fixture({ jingcaiId: '' }), ['非竞彩']],
    ['old_backend', fixture({ secondaryDecision: undefined }), ['待更新']],
    ['missing_score', fixture({ scoreRecommendation: undefined }), ['待更新']],
  ]
  const warnings = [], rendered = []
  for (const [name, input, words] of states) {
    const app = vue.createSSRApp({ render: () => vue.h(component, { row: input }) })
    app.config.warnHandler = warning => warnings.push(warning)
    app.component('VChip', { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
    const html = await renderToString(app)
    for (const word of words) check(html.includes(word), `${name}: ${word} is visible in actual component`)
    check(!html.includes('基线推荐') && !html.includes('固定推荐'), 'current list no longer claims a fixed baseline')
    rendered.push({ name, html })
  }
  equal(warnings, [], 'actual component renders without Vue warnings')
  const page = read('admin/src/views/statistics/DrawHistory.vue')
  check(page.includes('<DrawScoreRecommendation :row="row"'), 'existing shared list displays the new score component')
  check(page.includes('<DrawResearchTakeover v-if="report" :status="report.researchTakeover"'), 'list shows the actual response takeover state')
  check(page.includes('<DrawResearchSource :row="row"'), 'archive source is visible without creating another table column')
  equal((page.match(/colspan="20"/g) || []).length, 2, 'expanded and empty rows span the complete 20-column list')
  check(read('admin/src/router/index.ts').includes('statistics/research-store'), 'research overview has an authenticated admin route')
  check(read('admin/src/layouts/AdminLayout.vue').includes('/statistics/research-store'), 'research overview is discoverable in the menu')
  const research = read('admin/src/views/statistics/ResearchStore.vue')
  const schema = read('admin/src/views/statistics/ResearchSchemaTable.vue')
  for (const field of ['table.columns', 'table.indexes', 'table.foreignKeys']) check(schema.includes(field), 'overview exposes all fields, indexes and relationships')
  for (const field of ['liveSnapshots', 'reconstructedSnapshots']) check(research.includes(field), 'overview exposes distinct provenance groups')
  check(!research.includes('onMounted(sync)'), 'reading the page never triggers a write or synchronization')
  let schemaReview = { status: 'skipped', reason: 'Real overview export not supplied; no fabricated substitute.' }
  const overviewPath = process.env.RESEARCH_OVERVIEW_JSON || path.join(os.tmpdir(), 'peak-ball-research-overview.json')
  if (fs.existsSync(overviewPath)) {
    const overview = JSON.parse(fs.readFileSync(overviewPath, 'utf8').replace(/^\uFEFF/, ''))
    check(overview.database && Array.isArray(overview.tables) && overview.tables.length > 0, 'real independent database returns schema tables')
    const names = new Set(overview.tables.map(table => table.name))
    equal(names.size, overview.tables.length, 'schema table names are unique')
    const schemaComponent = load(compileScript(parse(schema).descriptor, { id: 'research-schema-offline', inlineTemplate: true }).content).default
    const escape = text => String(text).replace(/[&<>"']/g, value => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[value]))
    let columnCount = 0, relationCount = 0
    for (const table of overview.tables) {
      check(Array.isArray(table.columns) && table.columns.length > 0, `${table.name}: real columns exist`)
      check(Number.isInteger(table.rows) && table.rows >= 0, `${table.name}: actual row count is valid`)
      const columns = new Set(table.columns.map(column => column.name))
      equal(columns.size, table.columns.length, `${table.name}: unique columns`)
      const app = vue.createSSRApp({ render: () => vue.h(schemaComponent, { table }) })
      app.config.warnHandler = warning => warnings.push(warning)
      for (const name of ['VTable', 'VChip']) app.component(name, { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
      const html = await renderToString(app)
      for (const column of table.columns) {
        check(typeof column.nullable === 'boolean' && typeof column.primaryKey === 'boolean', 'nullability and primary keys have explicit semantics')
        check(html.includes(escape(column.name)) && html.includes(escape(column.type)), `${table.name}.${column.name}: real column and type are rendered`)
        columnCount++
      }
      for (const index of table.indexes) {
        check(index.columns.every(column => columns.has(column)), `${table.name}: every index references existing columns`)
        check(html.includes(escape(index.name)), 'real index is visible')
      }
      for (const relation of table.foreignKeys) {
        check(columns.has(relation.column) && names.has(relation.referencedTable), `${table.name}: relationship references known table and source column`)
        check(html.includes(escape(`${relation.referencedTable}.${relation.referencedColumn}`)), 'real relationship is visible')
        relationCount++
      }
      rendered.push({ name: `schema ${table.name}`, html })
    }
    schemaReview = { status: 'passed', database: overview.database, tables: overview.tables.length, columns: columnCount, relationships: relationCount }
  }
  equal(warnings, [], 'schema render has no Vue warnings')
  const evaluationUtils = load(read('admin/src/utils/researchEvaluation.ts'))
  for (const value of [null, undefined, NaN, Infinity, -.01, 1.01, '0.5']) equal(evaluationUtils.researchRate(value), '暂无', 'unknown or malformed rates never become reported percentages')
  equal(evaluationUtils.researchRate(0), '0.00%', 'a real zero rate stays zero')
  equal(evaluationUtils.researchRate(1), '100.00%', 'fraction endpoints convert correctly')
  equal(evaluationUtils.researchRate(.35), '35.00%', 'backend fractions convert once to percent')
  const date = load(read('admin/src/utils/date.ts'))
  const evaluationComponent = load(compileScript(parse(read('admin/src/views/statistics/ResearchEvaluationTable.vue')).descriptor, { id: 'research-evaluation-offline', inlineTemplate: true }).content,
    { '@/utils/researchEvaluation': evaluationUtils, '@/utils/date': date }).default
  const emptyGroup = { eligible: 0, draws: 0, selected: 0, hits: 0, precision: null, recall: null, scoreCandidates: 0, scoreHits: 0, scoreHitRate: null }
  const evaluationStates = [
    { id: 1, scope: 'semantic-empty-prospective', version: 'research-evaluation-v3-dynamic-scores', createdAt: '2026-10-29T12:00:00Z', metrics: { groups: { original: emptyGroup, secondary: emptyGroup, enhanced: emptyGroup }, pending: 5, settled: 0, independentOfHistoricalReconstruction: true } },
    { id: 2, scope: 'semantic-historical', version: 'research-evaluation-v3-dynamic-scores', createdAt: '2026-10-29T12:00:00Z', metrics: { groups: { secondary: { ...emptyGroup, eligible: 10, draws: 4, selected: 5, hits: 2, precision: .4, recall: .5, scoreCandidates: 5, firstScoreHits: 0, firstScoreHitRate: 0, scoreHits: 1, scoreHitRate: .2 } }, historicalAssociationOnly: true } },
    { id: 3, scope: 'unrecognized', version: 'semantic-test', createdAt: '2026-10-29T12:00:00Z', metrics: { groups: {} } },
  ]
  equal(evaluationUtils.researchEvaluationBasis({ metrics: { historicalAssociationOnly: true, independentOfHistoricalReconstruction: true } }).label, '历史重建关联', 'historical evidence cannot be relabelled prospective by conflicting flags')
  equal(evaluationUtils.isResearchScoreRankMetrics({ version: 'research-evaluation-v2-score-ranks', metrics: {} }), true, 'v2 baseline ranks remain readable as historical method')
  equal(evaluationUtils.isResearchScoreRankMetrics({ version: 'research-evaluation-v3-dynamic-scores', metrics: { scoreMethod: 'draw-score-dynamic-v2' } }), true, 'v3 dynamic ranks are recognized')
  equal(evaluationUtils.isResearchScoreRankMetrics({ version: 'research-evaluation-v3-dynamic-scores', metrics: { scoreMethod: 'draw-score-baseline-v1' } }), false, 'conflicting score method cannot masquerade as dynamic accuracy')
  equal(evaluationUtils.researchScoreMethod({ version: 'research-evaluation-v2-score-ranks', metrics: {} }).kind, 'historical-baseline', 'old fixed scores are explicitly labelled historical')
  evaluationStates.push({ id: 4, scope: 'semantic-legacy-baseline', version: 'research-evaluation-v2-score-ranks', createdAt: '2026-10-29T12:00:00Z', metrics: { groups: { secondary: { ...emptyGroup, scoreCandidates: 100, firstScoreHits: 20, firstScoreHitRate: .2, scoreHits: 32, scoreHitRate: .32 } }, scoreMethod: 'draw-score-baseline-v1', historicalAssociationOnly: true } })
  evaluationStates.push({ id: 5, scope: 'semantic-method-conflict', version: 'research-evaluation-v3-dynamic-scores', createdAt: '2026-10-29T12:00:00Z', metrics: { groups: { secondary: { ...emptyGroup, scoreCandidates: 100, firstScoreHits: 20, firstScoreHitRate: .2, scoreHits: 32, scoreHitRate: .32 } }, scoreMethod: 'draw-score-baseline-v1', historicalAssociationOnly: true } })

  let actualEvaluationReview = { status: 'skipped', reason: 'Real evaluation export not supplied.' }
  const inspectorFixturePath = process.env.RESEARCH_INSPECTOR_JSON || path.join(os.tmpdir(), 'peak-ball-research-inspector-fixture.json')
  const inspectorFixture = fs.existsSync(inspectorFixturePath) ? JSON.parse(fs.readFileSync(inspectorFixturePath, 'utf8').replace(/^\uFEFF/, '')) : null
  const evaluationPath = process.env.RESEARCH_EVALUATIONS_JSON || path.join(os.tmpdir(), 'peak-ball-research-evaluations.json')
  const actualEvaluations = fs.existsSync(evaluationPath) ? JSON.parse(fs.readFileSync(evaluationPath, 'utf8').replace(/^\uFEFF/, '')) : inspectorFixture?.evaluations
  if (actualEvaluations) {
    const actual = actualEvaluations
    check(Array.isArray(actual.items), 'real evaluations response is a list')
    evaluationStates.push(...actual.items)
    actualEvaluationReview = { status: 'passed', records: actual.items.length }
  }
  const featureComponent = load(compileScript(parse(read('admin/src/views/statistics/ResearchFeatureTable.vue')).descriptor, { id: 'research-features-offline', inlineTemplate: true }).content).default
  const auditComponent = load(compileScript(parse(read('admin/src/views/statistics/ResearchAuditTable.vue')).descriptor, { id: 'research-audits-offline', inlineTemplate: true }).content, { '@/utils/date': date }).default
  let actualInspectorReview = { status: 'skipped', reason: 'Real inspector export not supplied.' }
  if (inspectorFixture) {
    const escape = text => String(text).replace(/[&<>"']/g, value => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[value]))
    for (const [name, Component, data] of [['features', featureComponent, inspectorFixture.features], ['audits', auditComponent, inspectorFixture.auditIssues]]) {
      check(Array.isArray(data.items) && Number.isInteger(data.total) && data.total >= data.items.length, `${name}: real paged list has consistent totals`)
      check(data.items.length <= data.pageSize, `${name}: pagination stays bounded`)
      const app = vue.createSSRApp({ render: () => vue.h(Component, { items: data.items, loading: false, error: '' }) })
      app.config.warnHandler = warning => warnings.push(warning)
      for (const name of ['VTable', 'VBtn', 'VChip']) app.component(name, { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
      const html = await renderToString(app)
      for (const row of data.items) {
        for (const field of name === 'features' ? ['name', 'version', 'description', 'formula', 'missingPolicy'] : ['code', 'field', 'message', 'matchId']) {
          if (row[field]) check(html.includes(escape(row[field])), `${name}: real ${field} is visible`)
        }
      }
      rendered.push({ name: `real ${name}`, html })
    }
    actualInspectorReview = { status: 'passed', featureRows: inspectorFixture.features.items.length, featureTotal: inspectorFixture.features.total, auditRows: inspectorFixture.auditIssues.items.length, auditTotal: inspectorFixture.auditIssues.total, auditRun: inspectorFixture.auditIssues.runId }
  }
  for (const evaluation of evaluationStates) {
    const app = vue.createSSRApp({ render: () => vue.h(evaluationComponent, { evaluation }) })
    app.config.warnHandler = warning => warnings.push(warning)
    for (const name of ['VCard', 'VChip', 'VTable']) app.component(name, { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
    const html = await renderToString(app)
    check(html.includes(evaluationUtils.researchEvaluationBasis(evaluation).label), 'evaluation basis is explicit in actual component')
    for (const group of Object.values(evaluation.metrics.groups)) for (const rate of ['precision', 'recall', 'firstScoreHitRate', 'scoreHitRate']) {
      const value = rate.includes('Score') || rate === 'scoreHitRate' ? evaluationUtils.isResearchScoreRankMetrics(evaluation) ? group[rate] : null : group[rate]
      check(html.includes(evaluationUtils.researchRate(value)), 'actual group rates/nulls are displayed without recomputing a denominator')
    }
    check(html.includes(evaluationUtils.researchScoreMethod(evaluation).label), 'each record visibly identifies its score method')
    if (evaluation.scope === 'semantic-legacy-baseline') check(html.includes('历史固定比分：1:1 / 0:0') && html.includes('不代表当前推荐的正确率') && html.includes('20.00%'), 'old baseline accuracy stays explicitly historical')
    if (evaluation.scope === 'semantic-method-conflict') check(html.includes('比分评估口径待确认') && !html.includes('20.00%') && !html.includes('32.00%'), 'method-conflicting score statistics are hidden')
    if (evaluation.scope === 'semantic-empty-prospective') check(html.includes('暂无') && !html.includes('0.00%'), 'pending-only prospective sample does not report a zero success rate')
    if (evaluation.scope === 'semantic-historical') check(html.includes('40.00%') && html.includes('50.00%') && html.includes('20.00%') && html.includes('0.00%'), 'draw precision, coverage, top-one and top-two precision remain separate')
    rendered.push({ name: `evaluation ${evaluation.scope}`, html })
  }
  let inspectorRequests = 0
  const inspectorSource = compileScript(parse(read('admin/src/views/statistics/ResearchDataInspector.vue')).descriptor, { id: 'research-inspector-offline', inlineTemplate: true }).content
  const mockGet = () => { inspectorRequests++; throw new Error('Unexpected eager data request') }
  const inspectorComponent = load(inspectorSource, { '@/api/researchStore': { getResearchFeatures: mockGet, getResearchAuditIssues: mockGet, getResearchEvaluations: mockGet }, './ResearchEvaluationTable.vue': { default: evaluationComponent }, './ResearchFeatureTable.vue': { default: featureComponent }, './ResearchAuditTable.vue': { default: auditComponent } }).default
  const inspectorApp = vue.createSSRApp({ render: () => vue.h(inspectorComponent, { featureSets: [], revision: 'real-overview-revision' }) })
  inspectorApp.config.warnHandler = warning => warnings.push(warning)
  for (const match of inspectorSource.matchAll(/resolveComponent\("([^"]+)"\)/g)) inspectorApp.component(match[1], { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
  for (const name of ['VTable', 'VChip']) inspectorApp.component(name, { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
  const inspectorHTML = await renderToString(inspectorApp)
  for (const label of ['特征字典', '清洗问题', '评估记录']) check(inspectorHTML.includes(label), 'each on-demand section is discoverable')
  equal(inspectorRequests, 0, 'overview render never eagerly queries feature, audit or evaluation lists')
  equal(warnings, [], 'all actual research components render without Vue warnings')
  const report = { status: 'passed', checks, renderedStates: states.length, schemaReview, actualInspectorReview, actualEvaluationReview, evaluationStates: evaluationStates.length, inspectorRequestsOnInitialRender: inspectorRequests, servicesStarted: false, networkRequests: false, modelTraining: false, limitations: ['Offline SSR uses Vuetify stubs; this is not a browser layout audit.', 'Small synthetic evaluation records test display semantics only and are not claimed as real match statistics.'] }
  const directory = path.join(os.tmpdir(), 'peak-ball-research-ui-review')
  fs.mkdirSync(directory, { recursive: true })
  fs.writeFileSync(path.join(directory, 'review.json'), JSON.stringify(report, null, 2))
  fs.writeFileSync(path.join(directory, 'score-states.html'), '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>Ranked draw-score states</title>' + rendered.map(state => `<section><h2>${state.name}</h2>${state.html}</section>`).join('\n') + '</html>')
  console.log(JSON.stringify(report, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
