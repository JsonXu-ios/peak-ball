// Offline serving-provenance and ranked-score UI contract checks. No service startup.
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
const read = file => fs.readFileSync(path.join(root, file), 'utf8')
let checks = 0
const equal = (a, b, message) => { assert.deepEqual(a, b, message); checks++ }
const check = (a, message) => { assert.ok(a, message); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const takeover = load(read('admin/src/utils/drawResearchTakeover.ts'))
const date = load(read('admin/src/utils/date.ts'))
const component = (file, imports) => load(compileScript(parse(read(file)).descriptor, { id: file, inlineTemplate: true }).content, imports).default

async function main() {
  const summary = { version: 'research-takeover-v1', mode: 'research', served: 2, persisted: 1, fallback: 0, unavailable: 0 }
  for (const bad of [undefined, null, {}, { ...summary, version: '' }, { ...summary, mode: 'guess' }, { ...summary, served: -1 }, { ...summary, persisted: '1' }, { ...summary, unavailable: NaN }]) {
    equal(takeover.researchTakeoverPresentation(bad).state, 'missing', 'missing/invalid response cannot claim successful takeover')
  }
  equal(takeover.researchTakeoverPresentation(summary).state, 'research', 'explicit complete research response is recognized')
  for (const mode of ['mixed', 'fallback']) equal(takeover.researchTakeoverPresentation({ ...summary, mode, fallback: 1 }).color, 'warning', 'fallback is visible without hiding existing data')
  const recorded = { version: 'research-takeover-v1', source: 'research', snapshotId: 12, predictionId: 23, asOf: '2026-10-29T10:00:00Z' }
  equal(takeover.researchDecisionPresentation(recorded).label, '库读取', 'existing archive lookup gets the precise badge')
  equal(takeover.researchDecisionPresentation({ ...recorded, source: 'research_write_through' }).label, '已入库', 'write-through is not reported as an existing lookup')
  equal(takeover.researchDecisionPresentation({ ...recorded, source: 'legacy_fallback', reason: '独立库暂不可用' }).label, '原链路回退', 'a real fallback remains explicit')
  equal(takeover.researchDecisionPresentation({ ...recorded, source: 'not_applicable' }).state, 'not_applicable', 'out-of-scope rows are separate from a database failure')
  for (const bad of [undefined, null, { ...recorded, version: '' }]) equal(takeover.researchDecisionPresentation(bad).state, 'missing', 'older rows do not acquire a stored badge')
  for (const bad of [{ ...recorded, snapshotId: 0 }, { ...recorded, predictionId: -1 }, { ...recorded, predictionId: '23' }, { ...recorded, snapshotId: Number.MAX_SAFE_INTEGER + 1 }, { ...recorded, source: 'unknown' }]) {
    equal(takeover.researchDecisionPresentation(bad).state, 'invalid', 'archive identifiers and source must be explicit and valid')
  }
  const Top = component('admin/src/views/statistics/DrawResearchTakeover.vue', { '@/utils/drawResearchTakeover': takeover })
  const Row = component('admin/src/views/statistics/DrawResearchSource.vue', { '@/utils/drawResearchTakeover': takeover, '@/utils/date': date })
  const states = [
    ['old-api', Top, {}, ['当前接口尚未返回研究库接管状态']],
    ['research', Top, { status: summary }, ['研究库读取已启用', '库读取 2 场', '新写入 1 场']],
    ['mixed', Top, { status: { ...summary, mode: 'mixed', fallback: 1 } }, ['部分比赛使用原链路回退', '回退 1 场']],
    ['fallback', Top, { status: { ...summary, mode: 'fallback', served: 0, persisted: 0, fallback: 3, reason: '独立库暂不可用' } }, ['当前使用原链路回退', '独立库暂不可用']],
    ['lookup', Row, { row: { researchDecision: recorded } }, ['库读取', '特征快照 #12', '预测 #23']],
    ['write-through', Row, { row: { researchDecision: { ...recorded, source: 'research_write_through' } } }, ['已入库']],
    ['row-fallback', Row, { row: { researchDecision: { ...recorded, source: 'legacy_fallback', reason: '输入版本不匹配' } } }, ['原链路回退', '输入版本不匹配']],
    ['row-old-api', Row, { row: {} }, ['来源待确认']],
  ]
  const warnings = []
  for (const [name, Component, props, expected] of states) {
    const app = vue.createSSRApp({ render: () => vue.h(Component, props) })
    app.config.warnHandler = warning => warnings.push(warning)
    for (const c of ['VChip', 'VAlert']) app.component(c, { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
    const html = await renderToString(app)
    for (const text of expected) check(html.includes(text), `${name}: actual component shows ${text}`)
  }
  equal(warnings, [], 'takeover components render without Vue warnings')
  let actualResponseReview = { status: 'skipped', reason: 'Real takeover response not provided; no substitute is labelled live.' }
  const responsePath = process.env.RESEARCH_TAKEOVER_JSON || path.join(os.tmpdir(), 'peak-ball-research-takeover-response.json')
  if (fs.existsSync(responsePath)) {
    const response = JSON.parse(fs.readFileSync(responsePath, 'utf8').replace(/^\uFEFF/, ''))
    check(Array.isArray(response.items), 'actual takeover response has list rows')
    check(takeover.researchTakeoverPresentation(response.researchTakeover).state !== 'missing', 'actual serving response has a complete takeover contract')
    const counts = { research: 0, research_write_through: 0, legacy_fallback: 0, not_applicable: 0 }
    for (const row of response.items) {
      const result = takeover.researchDecisionPresentation(row.researchDecision)
      check(!['missing', 'invalid'].includes(result.state), 'every actual row reports a valid explicit serving source')
      counts[result.state]++
      const app = vue.createSSRApp({ render: () => vue.h(Row, { row }) })
      app.config.warnHandler = warning => warnings.push(warning)
      app.component('VChip', { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
      const html = await renderToString(app)
      check(html.includes(result.label), 'actual serving source appears in the real Vue component')
    }
    equal(response.researchTakeover.served, counts.research, 'served counts existing lookups only')
    equal(response.researchTakeover.persisted, counts.research_write_through, 'persisted counts write-through separately')
    equal(response.researchTakeover.fallback, counts.legacy_fallback, 'fallback count agrees with visible row sources')
    equal(warnings, [], 'actual response rendering has no Vue warnings')
    actualResponseReview = { status: 'passed', rows: response.items.length, counts }
  }
  const page = read('admin/src/views/statistics/DrawHistory.vue')
  check(page.includes('<DrawResearchTakeover v-if="report"'), 'successful data response retains the provenance notice')
  check(!page.includes('report?.researchTakeover?.mode ===') && !page.includes('researchTakeover.mode !=='), 'serving provenance does not hide or filter list rows')
  equal((page.match(/colspan="20"/g) || []).length, 2, 'no extra wide provenance column is added')
  const scoreSource = read('admin/src/views/statistics/DrawScoreRecommendation.vue')
  check(scoreSource.includes("index === 0 ? '首选' : '次选'"), 'both ordered choices are explicit')
  const evaluation = read('admin/src/views/statistics/ResearchEvaluationTable.vue')
  check(evaluation.includes('firstScoreHitRate') && evaluation.includes('首选命中率') && evaluation.includes('前二命中率'), 'top-one and top-two denominators are separately presented')
  check(evaluation.includes("firstScoreHits ?? '暂无'"), 'old evaluations do not fabricate a missing top-one hit count')
  console.log(JSON.stringify({ status: 'passed', checks, renderedStates: states.length, actualResponseReview, servicesStarted: false, networkRequests: false, modelRulesChanged: false }, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
