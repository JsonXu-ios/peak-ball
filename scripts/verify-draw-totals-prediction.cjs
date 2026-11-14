// Offline quarter settlement, API guards and actual Vue SSR; no services.
const fs = require('node:fs')
const path = require('node:path')
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
const equal = (actual, expected, message) => { assert.deepEqual(actual, expected, message); checks++ }
const check = (condition, message) => { assert.ok(condition, message); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const selectionContract = load(read('admin/src/utils/drawFrozenSignal.ts'))
const currentModel = extra => ({ selectionVersion: 'jc-h2h-required-v1', minimumHeadToHead: 1, headToHeadSample: 8, ...extra })
const frontend = load(read('admin/src/utils/drawFrontend.ts'))
const alternative = load(read('admin/src/utils/drawAlternativeOutcome.ts'), { './drawFrontend': frontend, './drawFrozenSignal': selectionContract })
const totals = load(read('admin/src/utils/drawTotalsPrediction.ts'), { './drawAlternativeOutcome': alternative })
const fixture = (extra = {}) => ({
  jingcaiId: '202611043001', settled: false, regulationSettled: false, homeScore: 0, awayScore: 0,
  frozenDrawSignal: currentModel({ state: 'selected' }), frontendAnalysis: { averageEuroOdds: [2.2, 3.3, 3.1] },
  totalsPrediction: { modelVersion: 'test-totals-v1', state: 'recommended', direction: 'over', line: 2.75, overWater: .9, underWater: .95, reason: '按本场盘口推荐' },
  ...extra,
})
const completed = (homeScore, awayScore, extra = {}) => fixture({ settled: true, regulationSettled: true, homeScore, awayScore, ...extra })
const withPick = (row, changes) => ({ ...row, totalsPrediction: { ...row.totalsPrediction, ...changes } })
const view = totals.drawTotalsPredictionPresentation
const recommend = totals.drawTotalsRecommendation

async function main() {
  const cases = [
    ['over', 2, 3, 'full_win'], ['over', 2, 2, 'push'], ['over', 2, 1, 'full_loss'],
    ['over', 2.25, 3, 'full_win'], ['over', 2.25, 2, 'half_loss'], ['over', 2.25, 1, 'full_loss'],
    ['over', 2.5, 3, 'full_win'], ['over', 2.5, 2, 'full_loss'],
    ['over', 2.75, 3, 'half_win'], ['over', 2.75, 2, 'full_loss'],
    ['under', 2, 1, 'full_win'], ['under', 2, 2, 'push'], ['under', 2, 3, 'full_loss'],
    ['under', 2.25, 2, 'half_win'], ['under', 2.25, 3, 'full_loss'],
    ['under', 2.5, 2, 'full_win'], ['under', 2.5, 3, 'full_loss'],
    ['under', 2.75, 3, 'half_loss'], ['under', 2.75, 2, 'full_win'],
    ['under', .25, 0, 'half_win'], ['over', .75, 1, 'half_win'],
  ]
  for (const [direction, line, total, outcome] of cases) equal(totals.settleDrawTotals(direction, line, total), outcome, `${direction} ${line} at ${total} goals: correct quarter split`)
  for (let quarter = 1; quarter <= 36; quarter++) {
    for (let total = 0; total <= 12; total++) {
      const opposite = { full_win: 'full_loss', half_win: 'half_loss', push: 'push', half_loss: 'half_win', full_loss: 'full_win' }
      equal(totals.settleDrawTotals('under', quarter / 4, total), opposite[totals.settleDrawTotals('over', quarter / 4, total)], 'every quarter settlement has complementary over/under results')
    }
  }
  equal(recommend(fixture()).label, '大 2.75', 'server direction and line are visible')
  equal(recommend(withPick(fixture(), { direction: 'under' })).label, '小 2.75', 'under recommendation is independent')
  equal(recommend(withPick(fixture(), { direction: 'under' })).water, .95, 'under uses its own water')
  equal(recommend(fixture({ frozenDrawSignal: currentModel({ state: 'not_selected' }) })).state, 'recommended', 'totals does not require original draw selection')
  equal(recommend(fixture({ frozenDrawSignal: null, secondaryDecision: { decision: 'rejected' } })).state, 'recommended', 'draw and secondary model states do not filter totals')
  for (const totalsPrediction of [undefined, null]) {
    const row = fixture({ totalsPrediction, frontendAnalysis: { qiuPrediction: '大球', averageEuroOdds: [2.2, 3.3, 3.1] } })
    equal(recommend(row).label, '等待分析服务更新', 'missing API field is explicit and never falls back to old forecasts')
  }
  for (const id of [undefined, null, '', '  ']) equal(recommend(fixture({ jingcaiId: id })).state, 'out_of_scope', 'non-JC cannot invent a totals recommendation')
  equal(recommend(withPick(fixture(), { state: 'unavailable', reason: '缺少竞彩总球报价' })).reason, '缺少竞彩总球报价', 'server unavailable reason is retained')
  equal(recommend(withPick(fixture(), { state: 'out_of_scope' })).state, 'out_of_scope', 'server scope is honored')
  for (const bad of [0, -1, .1, 1e-12, NaN, Infinity, '2.75', null, undefined]) equal(recommend(withPick(fixture(), { line: bad })).state, 'unavailable', 'invalid/missing quarter line does not create a pick')
  for (const bad of [0, -1, NaN, Infinity, '.9', null, undefined]) equal(recommend(withPick(fixture(), { overWater: bad })).state, 'unavailable', 'invalid chosen-side water is unavailable')
  equal(recommend(withPick(fixture(), { underWater: undefined })).state, 'recommended', 'unused-side missing water does not erase a complete chosen side')
  equal(recommend(withPick(fixture(), { overWater: 2.1 })).state, 'recommended', 'UI does not impose an unrelated research water cap')
  for (const bad of [null, undefined, '', 'home', 'OVER']) equal(recommend(withPick(fixture(), { direction: bad })).state, 'unavailable', 'malformed direction is not guessed')
  const choiceProxy = new Proxy(fixture(), { get(target, key) {
    if (!['jingcaiId', 'totalsPrediction'].includes(key)) throw new Error(`Recommendation read unrelated field ${String(key)}`)
    return target[key]
  } })
  equal(recommend(choiceProxy), recommend(fixture()), 'recommendation reads only authoritative API contract and JC eligibility')
  const baseline = recommend(fixture())
  for (const score of [[0, 0], [2, 1], [0, 5]]) equal(recommend(completed(...score)), baseline, 'actual scores never change the selected direction')
  equal(view(fixture()).outcomeState, 'pending', 'unstarted 0:0 is pending')
  equal(view(fixture({ displayState: '推迟', status: -14 })).outcomeState, 'pending', 'postponed 0:0 stays pending')
  equal(view(withPick(completed(0, 0), { direction: 'under', line: 2.5 })).outcomeState, 'full_win', 'genuine regulation 0:0 settles under')
  equal(view(completed(0, 0)).outcomeState, 'full_loss', 'genuine 0:0 settles over as a loss')
  equal(view(completed(2, 1)).outcomeLabel, '赢半', 'half-win label is visible')
  equal(view(withPick(completed(1, 1), { line: 2.25 })).outcomeLabel, '输半', 'half-loss label is visible')
  equal(view(withPick(completed(1, 1), { line: 2 })).outcomeLabel, '走盘', 'integer line tie is push')
  for (const malformed of [{ settled: false }, { settled: 1 }, { regulationSettled: false }, { regulationSettled: undefined }, { regulationSettled: 1 }, { homeScore: -1 }, { awayScore: -1 }, { homeScore: 1.5 }, { awayScore: NaN }, { homeScore: '2' }, { awayScore: Infinity }]) equal(view(completed(2, 1, malformed)).outcomeState, 'pending', 'only explicit valid regulation results settle')
  equal(view(completed(2, 1)).jointLabel, '次选＋大小球同中', 'matching home alternative plus totals half-win is joint success')
  equal(view(completed(1, 2)).jointLabel, '', 'losing alternative cannot claim joint success')
  equal(view(withPick(completed(2, 1), { line: 3.25 })).jointLabel, '', 'totals half-loss cannot claim joint success')
  equal(view(withPick(completed(2, 1), { line: 3 })).jointLabel, '', 'totals push is not joint success')
  equal(view(withPick(completed(1, 1), { direction: 'under' })).jointLabel, '', 'a draw cannot credit the decisive alternative')
  equal(view(completed(2, 1, { frozenDrawSignal: currentModel({ state: 'not_selected' }) })).jointLabel, '', 'new totals recommendation does not create an old alternative')
  equal(view(completed(1, 2, { frontendAnalysis: { averageEuroOdds: [4.2, 3.1, 1.8] } })).jointLabel, '次选＋大小球同中', 'away alternative joins only its actual winning result')
  const immutable = fixture(), before = JSON.stringify(immutable)
  recommend(immutable); view(immutable)
  equal(JSON.stringify(immutable), before, 'display and settlement do not mutate API data')

  const component = load(compileScript(parse(read('admin/src/views/statistics/DrawTotalsPrediction.vue')).descriptor, { id: 'totals-offline-review', inlineTemplate: true }).content, { '@/utils/drawTotalsPrediction': totals }).default
  const warnings = []
  for (const [row, expected] of [
    [fixture(), ['大 2.75', '水位 0.90', '待赛果核对']],
    [withPick(completed(0, 0), { direction: 'under' }), ['小 2.75', '水位 0.95', '全赢']],
    [completed(2, 1), ['赢半', '次选＋大小球同中']],
    [fixture({ totalsPrediction: undefined }), ['等待分析服务更新']],
    [fixture({ jingcaiId: '' }), ['非竞彩']],
  ]) {
    const app = vue.createSSRApp({ render: () => vue.h(component, { row }) })
    app.component('v-chip', { setup(_, { slots }) { return () => vue.h('span', slots.default?.()) } })
    app.config.warnHandler = message => warnings.push(message)
    const html = await renderToString(app)
    for (const text of expected) check(html.includes(text), `actual Vue output contains ${text}`)
  }
  equal(warnings, [], 'Vue states render without warnings')
  const page = read('admin/src/views/statistics/DrawHistory.vue')
  check(/DrawAlternativeOutcome :row="row"[^]*?DrawTotalsPrediction :row="row"[^]*?DrawNonSporttery :row="row"/.test(page), 'new cell follows the existing alternative, preserving its neighbors')
  const header = page.match(/<thead><tr>([^]*?)<\/tr><\/thead>/)[1]
  equal((header.match(/<th(?:\s|>)/g) || []).length, 23, 'actual table header has 23 columns')
  check(page.includes('const observationColumnCount = 23'), 'expanded and empty rows share updated colspan')
  console.log(JSON.stringify({ status: 'passed', checks, servicesStarted: false, networkRequests: false, modelsChanged: false, vueWarnings: warnings }, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
