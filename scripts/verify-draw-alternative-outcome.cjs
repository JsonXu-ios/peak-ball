// Offline input guards, result-blind selection and actual Vue SSR checks.
// No service, database, browser, network or model training is used.
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
const read = name => fs.readFileSync(path.join(root, name), 'utf8')
let checks = 0
const equal = (actual, expected, message) => { assert.deepEqual(actual, expected, message); checks++ }
const check = (condition, message) => { assert.ok(condition, message); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const frontend = load(read('admin/src/utils/drawFrontend.ts'))
const alternative = load(read('admin/src/utils/drawAlternativeOutcome.ts'), { './drawFrontend': frontend })
const fixture = extra => ({
  matchId: 'alternative-fixture', jingcaiId: '202611017001', home: '主队', guest: '客队',
  frozenDrawSignal: { state: 'selected', score: .3, modelVersion: 'sporttery-draw-observation-v1' },
  frontendAnalysis: { averageEuroOdds: [2.2, 3.3, 3.1] },
  secondaryDecision: { version: 'secondary-v3-quotes-only', decision: 'rejected', threshold: .225 },
  settled: false, regulationSettled: false, homeScore: 0, awayScore: 0,
  ...extra,
})
const quote = odds => fixture({ frontendAnalysis: { averageEuroOdds: odds } })
const completed = (homeScore, awayScore, extra = {}) => fixture({ settled: true, regulationSettled: true, homeScore, awayScore, ...extra })
async function render(component, row, warnings) {
  const app = vue.createSSRApp({ render: () => vue.h(component, { row }) })
  app.config.warnHandler = warning => warnings.push(warning)
  app.component('VChip', { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
  return renderToString(app)
}

async function main() {
  equal(alternative.DRAW_ALTERNATIVE_OUTCOME_VERSION, 'draw-average-alternative-v1', 'market alternative is versioned separately from the draw model')
  const recommend = alternative.recommendDrawAlternativeOutcome
  const presentation = alternative.drawAlternativeOutcomePresentation
  equal(recommend(fixture()).direction, 'home', 'lower latest average home price chooses home')
  equal(recommend(quote([4.1, 3.2, 1.95])).direction, 'away', 'lower latest average away price chooses away')
  equal(recommend(quote([2.2, null, 3.1])).direction, 'home', 'draw quote is not an input to a home/away alternative')
  equal(recommend(fixture()).source, 'average_euro_latest', 'actual latest average quote source is explicit')
  equal(recommend(fixture({ frozenDrawSignal: { state: 'not_selected', score: .99 } })).state, 'not_selected', 'original non-selection is never promoted by a high score')
  for (const signal of [undefined, null, { state: 'unavailable' }]) {
    equal(recommend(fixture({ frozenDrawSignal: signal })).state, 'unavailable', 'missing original selection cannot receive a choice')
  }
  equal(recommend(fixture({ frozenDrawSignal: { state: 'out_of_scope' } })).state, 'out_of_scope', 'model scope is preserved')
  for (const jingcaiId of [undefined, null, '', '  ']) {
    equal(recommend(fixture({ jingcaiId })).state, 'out_of_scope', 'non-jingcai rows have no market alternative')
  }
  for (const secondaryDecision of [undefined, null, { decision: 'rejected' }, { version: 'secondary-v2-input-guards', decision: 'rejected' }]) {
    equal(recommend(fixture({ secondaryDecision })), recommend(fixture()), 'secondary rejection or an old API contract does not erase the original model alternative')
  }
  equal(recommend(fixture({ frozenDrawSignal: { state: 'selected', score: .21 } })).state, 'recommended', 'authoritative original selection does not acquire the secondary .225 gate')
  for (const odds of [undefined, null, [], [null, 3, 2], [2, 3, null]]) {
    equal(recommend(quote(odds)).state, 'unavailable', 'missing latest real home/away quotes remain unavailable')
  }
  for (const invalid of [0, 1, -1, NaN, Infinity, -Infinity, '2.2']) {
    for (const odds of [[invalid, 3, 2.5], [2.2, 3, invalid]]) {
      equal(recommend(quote(odds)).state, 'unavailable', 'nonfinite, nonnumeric or non-decimal prices cannot create a direction')
    }
  }
  for (const gap of [0, 5e-10, -5e-10]) {
    equal(recommend(quote([2.5, 3, 2.5 + gap])).state, 'unavailable', 'same-price tolerance does not invent a winner')
  }
  equal(recommend(quote([2.5, 3, 2.5 + 1e-8])).direction, 'home', 'meaningful price difference outside tolerance is accepted')
  const realAverageMarket = { key: 'localAverageOdds', oddsAvailable: true, odds: { home: 2.2, draw: 3.3, away: 3.1 } }
  equal(recommend(fixture({ frontendAnalysis: { localMarket: realAverageMarket } })).direction, 'home', 'legacy real-average representation remains usable')
  equal(recommend(fixture({ frontendAnalysis: { averageEuroOdds: null, localMarket: realAverageMarket } })).state, 'unavailable', 'explicit missing real average does not revive a stale fallback')
  equal(recommend(fixture({ frontendAnalysis: { localMarket: { ...realAverageMarket, key: 'sportterySim' } } })).state, 'unavailable', 'a simulated market is never treated as the real average')
  const unrelated = {
    averageEuroOdds: null,
    companies: [{ company: 'William', latest: [1.8, 3.4, 4.1] }, { company: 'Bet365', latest: [1.9, 3.3, 3.8] }],
    localMarket: { ...realAverageMarket, key: 'sportterySim' },
    opening: { averageEuroOdds: [1.5, 4, 5] },
    comparison: { opening: { averageEuroOdds: [1.5, 4, 5] } },
  }
  equal(recommend(fixture({ frontendAnalysis: unrelated })).state, 'unavailable', 'company, opening and simulated quotes cannot substitute for missing latest mean')

  const inputsOnly = new Proxy(fixture(), { get(target, key) {
    if (!['jingcaiId', 'frozenDrawSignal', 'frontendAnalysis'].includes(key)) throw new Error(`Choice read unrelated or result field ${String(key)}`)
    return target[key]
  } })
  equal(recommend(inputsOnly), recommend(fixture()), 'choice reads only original selection, jingcai scope and quotes, never results or secondary state')
  const baseline = recommend(fixture())
  for (const result of [
    { settled: true, regulationSettled: true, homeScore: 4, awayScore: 0 },
    { settled: true, regulationSettled: true, homeScore: 0, awayScore: 4 },
    { settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 },
    { status: 'finished', displayState: '完场', homeScore: 1, awayScore: 2 },
  ]) equal(recommend(fixture(result)), baseline, 'changing the target result cannot change the recommendation or reason')
  const unchanged = fixture(), before = JSON.stringify(unchanged)
  recommend(unchanged); presentation(unchanged)
  equal(JSON.stringify(unchanged), before, 'selection and settlement never mutate API data')
  const isolated = recommend(fixture()); isolated.direction = 'away'; isolated.homeOdds = 99
  equal(recommend(fixture()), baseline, 'returned choices do not mutate subsequent calls')

  equal(presentation(fixture()).outcomeState, 'pending', 'unstarted 0:0 already has a recommendation but is not a settled draw')
  equal(presentation(completed(2, 1)).outcomeState, 'alternative_hit', 'matching decisive home result settles the alternative')
  equal(presentation(completed(1, 2)).outcomeState, 'miss', 'opposite decisive result is recorded as a miss')
  equal(presentation(completed(1, 2)).outcomeLabel, '主次均未命中', 'a miss is not presented as a successful fallback')
  equal(presentation(completed(1, 2, { frontendAnalysis: { averageEuroOdds: [4.1, 3.2, 1.95] } })).outcomeState, 'alternative_hit', 'away alternative settles independently of the home example')
  for (const goals of [[0, 0], [1, 1], [3, 3]]) {
    equal(presentation(completed(...goals)).outcomeState, 'primary_hit', 'confirmed regulation draw credits the primary draw, not a backup win')
    equal(presentation(completed(...goals)).outcomeLabel, '主选平命中', 'primary draw hit is visibly distinct')
  }
  for (const invalid of [
    { settled: false }, { settled: true, regulationSettled: undefined }, { settled: true, regulationSettled: false },
    { settled: 1 }, { regulationSettled: 1 }, { homeScore: -1 }, { awayScore: -1 },
    { homeScore: 1.5 }, { awayScore: 1.5 }, { homeScore: NaN }, { awayScore: Infinity }, { homeScore: '2' },
  ]) equal(presentation(completed(2, 1, invalid)).outcomeState, 'pending', 'unconfirmed or malformed regulation results do not settle the pick')
  equal(presentation(completed(2, 1, { frozenDrawSignal: { state: 'not_selected' } })).outcomeState, 'none', 'a non-candidate has no hypothetical hit marker')

  const component = load(compileScript(parse(read('admin/src/views/statistics/DrawAlternativeOutcome.vue')).descriptor, {
    id: 'draw-alternative-outcome-offline', inlineTemplate: true,
  }).content, { '@/utils/drawAlternativeOutcome': alternative }).default
  const cases = [
    ['pending home', fixture(), '次选 主胜', '待赛果核对'],
    ['pending away', quote([4.1, 3.2, 1.95]), '次选 客胜', '待赛果核对'],
    ['primary draw', completed(2, 2), '次选 主胜', '主选平命中'],
    ['alternative hit', completed(2, 1), '次选 主胜', '次选命中'],
    ['both miss', completed(1, 2), '次选 主胜', '主次均未命中'],
    ['secondary rejected', fixture({ secondaryDecision: { decision: 'rejected' } }), '次选 主胜', '待赛果核对'],
    ['same price', quote([2.5, 3, 2.5]), '暂无', '主客同价'],
    ['missing quote', quote(null), '暂无', '缺少有效'],
    ['original not selected', fixture({ frozenDrawSignal: { state: 'not_selected' } }), '未入选', '原模型未入选'],
    ['non jingcai', fixture({ jingcaiId: '' }), '非竞彩', '仅为原模型入选'],
  ]
  const warnings = [], rendered = []
  for (const [name, row, label, outcome] of cases) {
    const html = await render(component, row, warnings)
    check(html.includes(label) && html.includes(outcome), `${name}: actual Vue output shows choice and accurate settlement/reason`)
    check(html.includes('不是决策树自身的第二名'), `${name}: tooltip identifies the market-derived method`)
    if (recommend(row).state === 'recommended') {
      const choice = recommend(row)
      check(html.includes(`即时均赔：主 ${choice.homeOdds.toFixed(2)} / 客 ${choice.awayOdds.toFixed(2)}`), `${name}: actual source prices are visible`)
    }
    rendered.push({ name, html })
  }

  const realPath = process.env.DRAW_ALTERNATIVE_RESPONSE_JSON || path.join(os.tmpdir(), 'peak-ball-2026-11-01-user-loss-streak-current.json')
  let actualResponse = { status: 'skipped', reason: 'No recorded API response supplied; no real-data substitute is fabricated.' }
  if (fs.existsSync(realPath)) {
    const response = JSON.parse(fs.readFileSync(realPath, 'utf8').replace(/^\uFEFF/, ''))
    check(Array.isArray(response.items), 'recorded actual API response contains rows')
    const counts = { recommended: 0, pending: 0, primary_hit: 0, alternative_hit: 0, miss: 0 }
    for (const row of response.items) {
      const choice = recommend(row), view = presentation(row)
      const prices = row.frontendAnalysis?.averageEuroOdds
      // This independently checks actual latest-mean rows, without the utility helper.
      if (row.jingcaiId?.trim() && row.frozenDrawSignal?.state === 'selected' && Array.isArray(prices)
        && Number.isFinite(prices[0]) && prices[0] > 1 && Number.isFinite(prices[2]) && prices[2] > 1
        && Math.abs(prices[0] - prices[2]) > 1e-9) {
        equal(choice.direction, prices[0] < prices[2] ? 'home' : 'away', `actual ${row.matchId}: direction agrees with independent quote comparison`)
      }
      if (choice.state === 'recommended') counts.recommended++
      if (view.outcomeState in counts) counts[view.outcomeState]++
      const html = await render(component, row, warnings)
      check(html.includes(view.label), `actual ${row.matchId}: recommendation state renders`)
      if (view.outcomeLabel) check(html.includes(view.outcomeLabel), `actual ${row.matchId}: outcome annotation renders`)
    }
    const todayExamples = [
      ['498164257', 'home', 'miss'],
      ['498269872', 'home', 'alternative_hit'],
    ]
    const verifiedExamples = []
    for (const [id, direction, outcome] of todayExamples) {
      const row = response.items.find(x => String(x.matchId) === id)
      if (!row) continue
      equal(recommend(row).direction, direction, `today ${id}: result cannot change the observed home alternative`)
      equal(presentation(row).outcomeState, outcome, `today ${id}: actual decisive outcome is correctly audited`)
      verifiedExamples.push({ matchId: id, home: row.home, guest: row.guest, odds: row.frontendAnalysis.averageEuroOdds,
        score: `${row.homeScore}:${row.awayScore}`, direction, outcome, secondary: row.secondaryDecision?.decision })
    }
    actualResponse = { status: 'passed', rows: response.items.length, ...counts, examples: verifiedExamples }
  }
  equal(warnings, [], 'all fixture and actual API rows render without Vue warnings')
  const report = { status: 'passed', checks, version: alternative.DRAW_ALTERNATIVE_OUTCOME_VERSION, renderedStates: cases.length,
    actualResponse, servicesStarted: false, networkRequests: false, modelChanged: false, vueWarnings: warnings }
  fs.writeFileSync(path.join(os.tmpdir(), 'peak-ball-draw-alternative-outcome-review.json'), JSON.stringify(report, null, 2))
  fs.writeFileSync(path.join(os.tmpdir(), 'peak-ball-draw-alternative-outcome-states.html'), '<!doctype html><meta charset="utf-8">' + rendered.map(x => `<section><h2>${x.name}</h2>${x.html}</section>`).join('\n'))
  console.log(JSON.stringify(report, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
