// Offline Vue rendering checks. No application service, database or network.
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')
const assert = require('node:assert/strict')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const vue = adminRequire('vue')
const ts = adminRequire('typescript')
const { parse, compileScript } = adminRequire('@vue/compiler-sfc')
const { renderToString } = adminRequire('@vue/server-renderer')
const source = fs.readFileSync(path.join(root, 'admin/src/views/statistics/DrawObservationMetrics.vue'), 'utf8')
const { descriptor } = parse(source)
const script = compileScript(descriptor, { id: 'draw-metrics-offline-review', inlineTemplate: true })
const compiled = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
const profitSupportModule = { exports: {} }
const profitSupportCompiled = ts.transpileModule(fs.readFileSync(path.join(root, 'admin/src/utils/drawProfitSupport.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
new Function('require', 'module', 'exports', profitSupportCompiled)(adminRequire, profitSupportModule, profitSupportModule.exports)
const secondaryModule = { exports: {} }
const secondaryCompiled = ts.transpileModule(fs.readFileSync(path.join(root, 'admin/src/utils/drawSecondarySignal.ts'), 'utf8'), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
new Function('require', 'module', 'exports', secondaryCompiled)(adminRequire, secondaryModule, secondaryModule.exports)
const loaded = { exports: {} }
new Function('require', 'module', 'exports', compiled)(name => name === '@/utils/drawProfitSupport' ? profitSupportModule.exports : name === '@/utils/drawSecondarySignal' ? secondaryModule.exports : adminRequire(name), loaded, loaded.exports)
const component = loaded.exports.default
const zero = { selected: 0, settled: 0, hits: 0, misses: 0, pending: 0, precision: null, coverage: null }
const fixture = {
  status: 'available', modelVersion: 'frozen-v1', secondaryVersion: 'secondary-v2-input-guards', secondaryThreshold: .225,
  scopeMatches: 100, settledMatches: 80, settledDraws: 40,
  eligibleMatches: 6, eligibleSettled: 4, eligibleDraws: 2,
  modelAvailable: 6, modelUnavailable: 0, outOfScope: 94, secondaryUnavailable: 0,
  original: { selected: 5, settled: 3, hits: 1, misses: 2, pending: 2, precision: 1 / 3, coverage: .5 },
  secondary: { selected: 4, settled: 2, hits: 1, misses: 1, pending: 2, precision: .5, coverage: .5 },
  rejected: { selected: 1, settled: 1, hits: 0, misses: 1, pending: 0, precision: 0, coverage: 0 },
}
let checks = 0
const check = (condition, message) => { assert.ok(condition, message); checks++ }
const cases = []
async function render(name, metrics, queried = true) {
  const warnings = []
  const app = vue.createSSRApp({ render: () => vue.h(component, { metrics, queried, date: '2026-10-29' }) })
  app.config.warnHandler = warning => warnings.push(warning)
  for (const name of ['VTable', 'VAlert']) app.component(name, { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
  const html = await renderToString(app)
  check(warnings.length === 0, `${name}: Vue renders without warnings`)
  cases.push({ name, html })
  return html
}
async function main() {
  const current = await render('complete scoped totals', fixture)
  check(current.includes('33.33%') && current.includes('50.00%'), 'server ratios are formatted as percentages')
  check(current.includes('1 / 2 场竞彩实际平局'), 'coverage denominator is eligible lottery draws, never all-scope draws')
  check(current.includes('1 / 3 场确认完赛入选比赛'), 'precision denominator excludes pending selections')
  check(current.includes('不受标签页、次数条件及分页影响'), 'complete-window scope is explicit')
  check(current.includes('二次保留') && current.includes('误剔平局'), 'both rule stages and false exclusions remain visible')
  const outdated = await render('old secondary statistics', { ...fixture, secondaryVersion: 'secondary-v1' })
  check(outdated.includes('统计待后端更新') && !outdated.includes('data-metric-group="secondary"'), 'old secondary statistics cannot masquerade as current guards')
  const missing = await render('legacy response', undefined)
  check(missing.includes('统计待后端更新'), 'missing metrics require a backend update')
  check(!missing.includes('0.00%'), 'missing metrics never masquerade as zero precision')
  const unqueried = await render('not queried', undefined, false)
  check(unqueried.includes('查询后显示') && !unqueried.includes('统计待后端更新'), 'initial state is distinct from an old backend')
  const empty = await render('empty denominator', { ...fixture, scopeMatches: 0, eligibleMatches: 0, eligibleSettled: 0, eligibleDraws: 0, modelAvailable: 0, original: zero, secondary: zero, rejected: zero })
  check(!empty.includes('0.00%') && empty.includes('—'), 'zero denominator displays a dash')
  const miss = await render('measured zero hit rate', { ...fixture, original: { ...fixture.original, hits: 0, precision: 0, coverage: 0 } })
  check(miss.includes('0.00%'), 'a real zero ratio remains visible')
  const unavailable = await render('all model inputs missing', { ...fixture, status: 'incomplete', modelAvailable: 0, modelUnavailable: 6, original: { ...zero, coverage: 0 }, secondary: { ...zero, coverage: 0 }, rejected: zero })
  check(unavailable.includes('当前统计不完整') && unavailable.includes('6 场竞彩缺少可用模型结果'), 'missing model coverage is identified')
  check(!unavailable.includes('0.00%'), 'all-missing model data cannot produce a false zero rate')
  const partial = await render('partial secondary inputs', { ...fixture, status: 'incomplete', secondaryUnavailable: 1 })
  check(partial.includes('1 场原入选比赛缺少有效二次筛选结果'), 'secondary input absence is distinct from rejection')
  const support = {
    version: 'profit-support-v1', status: 'incomplete',
    secondaryComparable: { selected: 3, settled: 2, hits: 1, misses: 1, pending: 1, precision: .5, coverage: .5 },
    secondarySupported: { selected: 1, settled: 1, hits: 1, misses: 0, pending: 0, precision: 1, coverage: .5 },
    secondaryNotSupported: { selected: 2, settled: 1, hits: 0, misses: 1, pending: 1, precision: 0, coverage: 0 },
    secondaryUnavailable: { selected: 1, settled: 0, hits: 0, misses: 0, pending: 1, precision: null, coverage: 0 },
  }
  const supported = await render('profit support cohorts', { ...fixture, profitSupport: support })
  check(supported.includes('二次可比基准') && supported.includes('盈亏分歧增强') && supported.includes('有共同方向'), 'profit support separates its comparable baseline and both subgroups')
  check(supported.includes('1 场缺少完整盈亏数据，待判') && supported.includes('不改变原候选池'), 'unknown source data is neither a negative feature nor a selection veto')
  check(supported.includes('100.00%') && supported.includes('增强组命中率只代表该分组'), 'a subgroup rate is explicitly distinguished from the whole pool')
  const unsupportedVersion = await render('unknown profit rule version', { ...fixture, profitSupport: { ...support, version: 'unrecognized-rule' } })
  check(unsupportedVersion.includes('增强分组统计待后端更新') && !unsupportedVersion.includes('data-profit-support-metric-group="supported"'), 'unknown feature versions cannot masquerade as current measurements')
  const page = fs.readFileSync(path.join(root, 'admin/src/views/statistics/DrawHistory.vue'), 'utf8')
  check(page.includes(':metrics="report?.observationMetrics"'), 'parent passes backend full-window metrics')
  check(page.includes('本页原入选') && page.includes('本页数量仅用于核对当前列表'), 'page counters are distinguished from full-window metrics')
  check(page.includes('不会随历史增加自动训练') && page.includes('尚未锁定首次赛前决定'), 'fixed rules and snapshot limitation remain visible')
  let actualResponse = { status: 'skipped', reason: 'No current response export supplied.' }
  const actualPath = process.env.DRAW_CURRENT_RESPONSE_JSON || path.join(os.tmpdir(), 'peak-ball-2026-10-31-dynamic-score-preview.json')
  if (fs.existsSync(actualPath)) {
    const response = JSON.parse(fs.readFileSync(actualPath, 'utf8').replace(/^\uFEFF/, ''))
    if (response.observationMetrics?.secondaryVersion === secondaryModule.exports.SECONDARY_DRAW_VERSION) {
      const html = await render('actual current decision window', response.observationMetrics)
      const secondaryRow = html.match(/<tr[^>]*data-metric-group="secondary"[^>]*>([\s\S]*?)<\/tr>/)?.[1] || ''
      check(secondaryRow.includes(`<td>${response.observationMetrics.secondary.selected}</td>`), 'actual backend secondary count is displayed without client recomputation')
      if (response.observationMetrics.secondary.settled === 0) check(!secondaryRow.includes('0.00%'), 'actual pending cohort has no fabricated zero accuracy')
      actualResponse = { status: 'passed', rows: response.items.length, secondarySelected: response.observationMetrics.secondary.selected }
    }
  }
  const output = path.join(root, 'reports/draw-observation-metrics-ui-review')
  fs.mkdirSync(output, { recursive: true })
  fs.writeFileSync(path.join(output, 'states.html'), `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>当期平局指标离线核对</title><style>body{background:#151515;color:#eee;font:14px system-ui;margin:24px}section{border:1px solid #666;padding:16px;margin-bottom:16px}table{border-collapse:collapse}td,th{border:1px solid #555;padding:8px}.text-caption{font-size:12px}.text-warning{color:#ffcb72}.mt-2{margin-top:8px}.mt-3{margin-top:12px}</style>${cases.map(item => `<section><h2>${item.name}</h2>${item.html}</section>`).join('')}</html>`, 'utf8')
  const result = { status: 'passed', checks, renderedStates: cases.length, actualResponse, networkRequests: false, servicesStarted: false, limitations: ['Offline SSR checks real template content with simple Vuetify container stubs; not a browser layout test.'] }
  fs.writeFileSync(path.join(output, 'review.json'), JSON.stringify(result, null, 2) + '\n')
  console.log(JSON.stringify(result, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
