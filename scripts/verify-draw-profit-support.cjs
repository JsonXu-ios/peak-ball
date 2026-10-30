// Independent offline review. No server, browser, database, training or network.
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')
const crypto = require('node:crypto')
const assert = require('node:assert/strict')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const ts = adminRequire('typescript')
const vue = adminRequire('vue')
const { parse, compileScript } = adminRequire('@vue/compiler-sfc')
const { renderToString } = adminRequire('@vue/server-renderer')
const folder = path.join(root, 'reports/draw-profit-support-review')
const read = relative => fs.readFileSync(path.join(root, relative), 'utf8')
const parseJSON = file => JSON.parse(fs.readFileSync(file, 'utf8').replace(/^\uFEFF/, ''))
const shaFile = file => crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex')
const sha = relative => shaFile(path.join(root, relative))
const outcomeKeys = ['home', 'draw', 'away']
let checks = 0
const equal = (actual, expected, message) => { assert.deepEqual(actual, expected, message); checks++ }
const check = (condition, message) => { assert.ok(condition, message); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const support = load(read('admin/src/utils/drawProfitSupport.ts'))
const secondary = load(read('admin/src/utils/drawSecondarySignal.ts'))
const frozen = load(read('admin/src/utils/drawFrozenSignal.ts'))
const profit = load(read('admin/src/utils/drawProfit.ts'))
const originals = {
  'admin/src/utils/drawFrozenSignal.ts': 'd68476cd87630c2ca69198963ead381c3e03f8092f63ef4c753d7976048d3358',
  'admin/src/views/statistics/DrawFrozenSignal.vue': '82540784e4b58cbe6b175ac146c9be010132adf5b26df11fb4a1a415fa555f14',
  'go_server/internal/drawmodeldata/observation_v1.json': 'e7cb7d50668a76e916d2f86a6ab598a6bcb23ab5e6c961dbd972875346afd987',
}

function simulation(values, extra = {}) {
  return {
    key: 'sportterySim', name: '竞彩模拟对比', simulated: true, oddsAvailable: true,
    bookmakerByOutcome: outcomeKeys.map((outcome, i) => ({ outcome, odds: 2, available: true, bookmakerProfit: values[i], totalStake: 100, bookmakerRoi: 999 })),
    ...extra,
  }
}
function fixture(official = [-10, 5, 20], simulated = [5, -10, 20], extra = {}) {
  return {
    jingcaiId: '周四001',
    frozenDrawSignal: { state: 'selected', score: .3, modelVersion: 'sporttery-draw-observation-v1' },
    secondaryDecision: { version: 'secondary-v2-input-guards', decision: extra.frozenDrawSignal?.state === 'not_selected' ? 'not_selected' : extra.frozenDrawSignal?.score < .225 ? 'rejected' : 'retained', threshold: .225, reason: '离线展示用二次判定', headToHeadSample: 8, minimumHeadToHead: 5, sportteryQuotesAvailable: true },
    frontendAnalysis: { weightedFeatureInputs: { homeHeat: null, officialProfitRates: official }, roiSimulation: { totalStake: 100, markets: [simulation(simulated)] } },
    settled: false, regulationSettled: false, homeScore: 0, awayScore: 0,
    ...extra,
  }
}
const classify = row => support.classifyDrawProfitSupport(row)
const finite = value => typeof value === 'number' && Number.isFinite(value)
const minima = values => outcomeKeys.filter((key, i) => values[i] <= Math.min(...values) + 1e-9)

// The research reference uses explicit raw presence and the raw source market,
// independently of production weighted inputs. It never fills a missing zero.
function reference(source) {
  if (!String(source.jingcaiId || '').trim()) return { state: 'out_of_scope' }
  const presence = source.profitInputs?.officialProfitRatePresence
  const markets = source.frontend?.roiSimulation?.markets || []
  const originals = markets.filter(m => m.key === 'sporttery' && m.simulated !== true)
  const sims = markets.filter(m => m.simulated === true && !['sportteryRqspf', 'sportteryRqspfSim'].includes(m.key))
  if (!Array.isArray(presence) || presence.length !== 3 || !presence.every(x => x === true) || originals.length !== 1 || originals[0].oddsAvailable !== true || sims.length !== 1 || sims[0].oddsAvailable !== true) return { state: 'unavailable' }
  const officialRates = [], simulatedRates = []
  for (const key of outcomeKeys) {
    const a = (originals[0].bookmakerByOutcome || []).filter(x => x.outcome === key)
    const b = (sims[0].bookmakerByOutcome || []).filter(x => x.outcome === key)
    if (a.length !== 1 || b.length !== 1 || !finite(a[0].odds) || a[0].odds <= 0 || !finite(a[0].officialProfitRate) || b[0].available !== true || !finite(b[0].odds) || b[0].odds <= 0 || !finite(b[0].bookmakerProfit) || !finite(b[0].totalStake) || b[0].totalStake <= 0) return { state: 'unavailable' }
    const rate = b[0].bookmakerProfit / b[0].totalStake * 100
    if (!finite(rate)) return { state: 'unavailable' }
    officialRates.push(a[0].officialProfitRate); simulatedRates.push(rate)
  }
  const officialMinimum = minima(officialRates), simulatedMinimum = minima(simulatedRates)
  return { state: officialMinimum.some(key => simulatedMinimum.includes(key)) ? 'not_supported' : 'supported', officialRates, simulatedRates, officialMinimum, simulatedMinimum }
}
function stats(rows, predicate, draws) {
  const selected = rows.filter(predicate), hits = selected.filter(x => x.result === 1).length
  return { matches: selected.length, draws: hits, nonDraws: selected.length - hits, precision: selected.length ? hits / selected.length : null, coverage: draws ? hits / draws : null }
}
function fixtureMetrics() {
  const group = (selected, hits) => ({ selected, settled: selected, hits, misses: selected - hits, pending: 0, precision: selected ? hits / selected : null, coverage: hits / 135 })
  return {
    status: 'available', secondaryVersion: 'secondary-v2-input-guards', modelAvailable: 552, modelUnavailable: 0, secondaryUnavailable: 0, outOfScope: 0,
    scopeMatches: 552, eligibleMatches: 552, eligibleSettled: 552, eligibleDraws: 135,
    original: group(341, 129), secondary: group(305, 125), rejected: group(36, 4),
    profitSupport: { version: 'profit-support-v1', status: 'available', secondaryComparable: group(296, 124), secondarySupported: group(141, 66), secondaryNotSupported: group(155, 58), secondaryUnavailable: group(9, 1) },
  }
}

async function main() {
  for (const [file, expected] of Object.entries(originals)) equal(sha(file), expected, `existing model and second-stage rules unchanged: ${file}`)
  equal(support.DRAW_PROFIT_SUPPORT_VERSION, 'profit-support-v1', 'version frozen')
  equal(support.DRAW_PROFIT_SUPPORT_EPS, 1e-9, 'tie tolerance frozen')
  const matched = classify(fixture())
  equal(matched.state, 'supported', 'different lowest directions support the observation tag')
  equal(matched.officialMinimum, ['home'], 'source minimum direction')
  equal(matched.simulatedMinimum, ['draw'], 'simulation minimum comes from amount/base, not decoy bookmakerRoi')
  equal(classify(fixture([0, 5, 10], [5, 0, 10])).state, 'supported', 'real zero is a valid observation')
  equal(classify(fixture([0, 0, 5], [5, 0, 5])).state, 'not_supported', 'all minimum ties are retained, common draw blocks enhancement')
  equal(classify(fixture([0, 0, 0], [1, 2, 3])).state, 'not_supported', 'three-way source tie always shares a minimum')
  equal(classify(fixture([0, 1e-9, 5], [5, -1, 5])).state, 'not_supported', 'exact epsilon tie stays in minimum set')
  equal(classify(fixture([0, 2e-9, 5], [5, -1, 5])).state, 'supported', 'beyond epsilon is a distinct minimum')
  for (const invalid of [null, undefined, NaN, Infinity, -Infinity, '0']) {
    const row = fixture(); row.frontendAnalysis.weightedFeatureInputs.officialProfitRates[1] = invalid
    equal(classify(row).state, 'unavailable', 'missing/nonfinite/non-numeric official rate cannot become zero')
  }
  const absentStrict = fixture(); delete absentStrict.frontendAnalysis.weightedFeatureInputs
  absentStrict.frontendAnalysis.roiSimulation.markets.push({ key: 'sporttery', oddsAvailable: true, bookmakerByOutcome: outcomeKeys.map(outcome => ({ outcome, odds: 2, officialProfitRate: 0, available: true })) })
  equal(classify(absentStrict).state, 'unavailable', 'legacy default source zero is not borrowed without strict presence')
  const missingSource = fixture(); missingSource.frontendAnalysis.weightedFeatureInputs.officialProfitRates.pop()
  equal(classify(missingSource).state, 'unavailable', 'all three official outcomes are required')
  for (const mutate of [
    m => { m.oddsAvailable = false },
    m => { m.bookmakerByOutcome[0].available = false },
    m => { m.bookmakerByOutcome[0].odds = 0 },
    m => { m.bookmakerByOutcome[0].odds = NaN },
    m => { m.bookmakerByOutcome[0].totalStake = 0 },
    m => { m.bookmakerByOutcome[0].totalStake = -1 },
    m => { m.bookmakerByOutcome[0].bookmakerProfit = Infinity },
    m => { m.bookmakerByOutcome[0].bookmakerProfit = undefined },
    m => { m.bookmakerByOutcome[0].totalStake = '100' },
    m => { m.bookmakerByOutcome.pop() },
    m => { m.bookmakerByOutcome.push({ ...m.bookmakerByOutcome[0] }) },
    m => { m.bookmakerByOutcome[0].bookmakerProfit = 1e308; m.bookmakerByOutcome[0].totalStake = 1e-308 },
  ]) {
    const row = fixture(); mutate(row.frontendAnalysis.roiSimulation.markets[0])
    equal(classify(row).state, 'unavailable', 'invalid/partial/duplicate simulated inputs are not enhancement')
  }
  const duplicate = fixture(); duplicate.frontendAnalysis.roiSimulation.markets.push(simulation([5, -10, 20]))
  equal(classify(duplicate).state, 'unavailable', 'multiple non-handicap simulations are ambiguous')
  const handicapOnly = fixture(); handicapOnly.frontendAnalysis.roiSimulation.markets[0].key = 'sportteryRqspfSim'
  equal(classify(handicapOnly).state, 'unavailable', 'handicap simulation is not a match-result comparison')
  const plusHandicap = fixture(); plusHandicap.frontendAnalysis.roiSimulation.markets.push(simulation([-100, 100, 100], { key: 'sportteryRqspfSim' }))
  equal(classify(plusHandicap), classify(fixture()), 'handicap simulation cannot alter the result-market feature')
  for (const blank of ['', '  ', undefined]) equal(classify(fixture(undefined, undefined, { jingcaiId: blank })).state, 'out_of_scope', 'non-lottery scope is explicit')
  equal(classify({ jingcaiId: '周四001' }).state, 'unavailable', 'absent analysis stays pending')
  const onlyInputs = new Proxy(fixture(), { get(target, key) { if (!['jingcaiId', 'frontendAnalysis'].includes(key)) throw new Error(`Feature accessed target/model field ${String(key)}`); return target[key] } })
  equal(classify(onlyInputs).state, 'supported', 'feature classification reads only intended inputs')
  const immutable = fixture(); Object.freeze(immutable.frontendAnalysis.weightedFeatureInputs.officialProfitRates); Object.freeze(immutable.frontendAnalysis.roiSimulation.markets[0].bookmakerByOutcome); Object.freeze(immutable)
  equal(classify(immutable).state, 'supported', 'classification works on immutable source rows')
  for (const state of ['selected', 'not_selected', 'unavailable', 'out_of_scope']) for (const score of [.21, .225, .9]) for (const result of [[0, 0, false], [1, 1, true], [2, 0, true]]) {
    const input = fixture(undefined, undefined, { frozenDrawSignal: { state, score }, homeScore: result[0], awayScore: result[1], settled: result[2], regulationSettled: result[2], result: result[0] === result[1] ? 1 : 0 })
    const before = JSON.stringify(input), priorFrozen = frozen.frozenDrawPresentation(input), priorSecondary = secondary.classifySecondaryDraw(input)
    equal(classify(input).state, 'supported', 'model choice and target score cannot change feature input classification')
    equal(JSON.stringify(input), before, 'tag cannot mutate model or target row')
    equal(frozen.frozenDrawPresentation(input), priorFrozen, 'original selection/result label is unchanged')
    equal(secondary.classifySecondaryDraw(input), priorSecondary, 'feature cannot promote or reject an existing candidate')
  }

  const datasetPath = path.join(os.tmpdir(), 'peak-ball-lines-20261029-readonly.json')
  const scorePath = path.join(os.tmpdir(), 'peak-ball-lines-20261029-frozen-scores.json')
  const goReferencePath = path.join(os.tmpdir(), 'peak-ball-profit-support-go-reference-20261029.json')
  const goReference = fs.existsSync(goReferencePath) ? parseJSON(goReferencePath) : null
  let goParity = { status: 'skipped', reason: 'Optional Go full-snapshot fixture output is absent.' }
  let historical = { status: 'skipped', reason: 'Optional original snapshot/scores absent; no substitute or live query.' }
  if (fs.existsSync(datasetPath) && fs.existsSync(scorePath)) {
    const data = parseJSON(datasetPath), scores = parseJSON(scorePath).scores
    const sourceRows = data.rows.filter(row => String(row.jingcaiId || '').trim())
    const rows = sourceRows.map(source => {
      const scored = scores[String(source.matchId)]; check(scored && finite(scored.score), `real score exists for ${source.matchId}`)
      const input = { ...source, frontendAnalysis: source.frontend, settled: true, regulationSettled: true, frozenDrawSignal: { state: scored.originalSelected ? 'selected' : 'not_selected', score: scored.score } }
      const expected = reference(source), actual = classify(input), before = JSON.stringify(input)
      equal(actual.state, expected.state, `${source.matchId} strict raw-presence reference state`)
      if (goReference) {
        const go = goReference.signals[String(source.matchId)]
        check(go, 'Go fixture covers every real fixture ID')
        equal(actual.state, go.state, 'Go and frontend classify the same strict input identically')
        if (['supported', 'not_supported'].includes(actual.state)) {
          equal(actual.officialMinimum, go.officialMinimum, 'Go and frontend source minimum sets agree')
          equal(actual.simulatedMinimum, go.simulatedMinimum, 'Go and frontend simulation minimum sets agree')
          for (const field of ['officialRates', 'simulatedRates']) for (let i = 0; i < 3; i++) check(Math.abs(actual[field][i] - go[field][i]) <= 1e-10, 'Go and frontend ready rates agree within float tolerance')
        }
      }
      if (expected.state === 'supported' || expected.state === 'not_supported') {
        equal(actual.officialRates, expected.officialRates, 'official raw source values agree')
        for (let i = 0; i < 3; i++) check(Math.abs(actual.simulatedRates[i] - expected.simulatedRates[i]) <= 1e-9, 'simulated amount/base ROI agrees')
        equal(actual.officialMinimum, expected.officialMinimum, 'source minimum set matches independent reference')
        equal(actual.simulatedMinimum, expected.simulatedMinimum, 'simulation minimum set matches independent reference')
      }
      const oldChoice = secondary.classifySecondaryDraw(input), frozenChoice = frozen.frozenDrawPresentation(input)
      classify({ ...input, homeScore: 99, awayScore: 98, result: 2, settled: false, regulationSettled: false, status: -999 })
      equal(classify({ ...input, homeScore: 99, awayScore: 98, result: 2, settled: false, regulationSettled: false }).state, actual.state, 'actual result cannot leak into support tag')
      equal(JSON.stringify(input), before, 'full historical source is not mutated')
      equal(secondary.classifySecondaryDraw(input), oldChoice, 'historical secondary choice remains original')
      equal(frozen.frozenDrawPresentation(input), frozenChoice, 'historical fixed model marker remains original')
      return { matchId: String(source.matchId), result: source.result, original: scored.originalSelected, secondary: scored.secondaryRetained, state: actual.state }
    })
    const draws = rows.filter(x => x.result === 1).length
    const cohorts = {}
    for (const [name, predicate] of [['raw', () => true], ['original', x => x.original], ['secondary', x => x.secondary]]) {
      const subset = rows.filter(predicate)
      cohorts[name] = { baseline: stats(subset, () => true, draws), comparable: stats(subset, x => ['supported', 'not_supported'].includes(x.state), draws), supported: stats(subset, x => x.state === 'supported', draws), notSupported: stats(subset, x => x.state === 'not_supported', draws), unavailable: stats(subset, x => x.state === 'unavailable', draws) }
    }
    equal([sourceRows.length, draws], [552, 135], 'latest read-only snapshot cohort is explicit')
    for (const [name, expected] of Object.entries({ raw: [552, 135, 502, 130, 231, 68], original: [341, 129, 328, 125, 155, 66], secondary: [305, 125, 296, 124, 141, 66] })) {
      const x = cohorts[name]
      equal([x.baseline.matches, x.baseline.draws, x.comparable.matches, x.comparable.draws, x.supported.matches, x.supported.draws], expected, 'known research cohorts reproduced from production inputs')
      equal(x.comparable.matches, x.supported.matches + x.notSupported.matches, 'complete feature partitions by shared minimum')
      equal(x.baseline.matches, x.comparable.matches + x.unavailable.matches, 'unknown rows remain unknown, not negative')
    }
    historical = { status: 'passed', generatedAt: data.generatedAt, cohorts, targetMutations: rows.length, productionInputsDirect: true, sourceHashes: { dataset: shaFile(datasetPath), scores: shaFile(scorePath) } }
    if (goReference) {
      equal(Object.keys(goReference.signals).sort(), rows.map(row => row.matchId).sort(), 'Go fixture ID set exactly matches current cohort')
      const metric = goReference.metrics
      equal([metric.original.selected, metric.original.hits, metric.secondary.selected, metric.secondary.hits], [341, 129, 305, 125], 'Go complete-window aggregation preserves original and secondary choices')
      for (const [field, group] of [['secondaryComparable', 'comparable'], ['secondarySupported', 'supported'], ['secondaryNotSupported', 'notSupported'], ['secondaryUnavailable', 'unavailable']]) {
        const a = metric.profitSupport[field], b = cohorts.secondary[group]
        equal([a.selected, a.hits, a.misses, a.pending], [b.matches, b.draws, b.nonDraws, 0], 'Go metrics equal independently classified feature groups')
        check(Math.abs(a.precision - b.precision) <= 1e-12 && Math.abs(a.coverage - b.coverage) <= 1e-12, 'Go metric rates use matching settled and eligible-draw denominators')
      }
      goParity = { status: 'passed', fixtures: rows.length, referenceHash: shaFile(goReferencePath), incompleteDisplayDetail: 'Only ready minimum sets/rates are compared; unavailable rows may expose different partial explanatory values.' }
    }
  }

  const componentPath = 'admin/src/views/statistics/DrawProfitSupportSignal.vue'
  const descriptor = parse(read(componentPath)).descriptor
  const componentScript = compileScript(descriptor, { id: 'profit-support-offline', inlineTemplate: true })
  const imports = { '@/utils/drawProfitSupport': support, '@/utils/drawSecondarySignal': secondary, '@/utils/drawProfit': profit }
  const component = load(componentScript.content, imports).default
  const displayRows = [
    ['支持且二次保留', fixture()],
    ['支持但二次剔除', fixture(undefined, undefined, { frozenDrawSignal: { state: 'selected', score: .21 } })],
    ['支持但原未入选', fixture(undefined, undefined, { frozenDrawSignal: { state: 'not_selected', score: .9 } })],
    ['支持但模型未知', fixture(undefined, undefined, { frozenDrawSignal: undefined })],
    ['最低方向相同', fixture([-10, 5, 20], [-10, 5, 20])],
    ['并列最低有共同方向', fixture([0, 0, 5], [5, 0, 5])],
    ['严格来源缺失', fixture([null, 5, 20])],
    ['非竞彩', fixture(undefined, undefined, { jingcaiId: '' })],
    ['赛后平局不改特征', fixture(undefined, undefined, { settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 })],
  ]
  const rendered = [], warnings = []
  for (const [name, input] of displayRows) {
    const app = vue.createSSRApp({ render: () => vue.h(component, { row: input }) })
    app.config.warnHandler = message => warnings.push(message)
    for (const name of ['VChip', 'VIcon']) app.component(name, { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
    const html = await renderToString(app)
    check(html.length > 0, 'actual component renders state')
    if (name === '支持且二次保留') check(html.includes('二次增强'), 'enhancement is visible only in retained candidate pool')
    if (name === '支持但原未入选' || name === '支持但二次剔除') check(html.includes('未二次保留') && !html.includes('二次增强'), 'feature match never promotes a non-retained candidate')
    if (name === '严格来源缺失') check(html.includes('待判'), 'missing strict input displays pending')
    if (name === '非竞彩') check(html.includes('非竞彩'), 'out-of-scope display is explicit')
    rendered.push({ name, html })
  }
  equal(warnings, [], 'actual feature component renders without Vue warnings')
  const metricsPath = 'admin/src/views/statistics/DrawObservationMetrics.vue'
  const metricsDescriptor = parse(read(metricsPath)).descriptor
  const metricsComponent = load(compileScript(metricsDescriptor, { id: 'profit-metrics-offline', inlineTemplate: true }).content, imports).default
  const app = vue.createSSRApp({ render: () => vue.h(metricsComponent, { metrics: fixtureMetrics(), queried: true, date: '2026-10-29' }) })
  app.config.warnHandler = message => warnings.push(message)
  for (const name of ['VTable', 'VAlert']) app.component(name, { setup: (_, { slots }) => () => vue.h('div', slots.default?.()) })
  const metricsHTML = await renderToString(app)
  check(metricsHTML.includes('41.89%') && metricsHTML.includes('46.81%'), 'comparable baseline and enhanced subgroup are both visible')
  check(metricsHTML.includes('48.89%'), 'enhancement group coverage uses all eligible draws, not 124 only')
  check(metricsHTML.includes('92.59%'), 'main secondary candidate coverage stays unchanged')
  equal(warnings, [], 'metrics render has no Vue warnings')
  rendered.push({ name: '完整窗口对照统计', html: metricsHTML })
  const page = read('admin/src/views/statistics/DrawHistory.vue')
  check(page.includes('<DrawProfitSupportSignal') && page.includes('<DrawFrozenSignal') && page.includes('<DrawSecondarySignal'), 'new feature is displayed alongside original model and secondary choices')
  equal((page.match(/colspan="20"/g) || []).length, 2, 'expanded and empty rows span all 20 columns including the two-score baseline')
  check(page.includes('盈亏分歧'), 'feature column is discoverable')
  fs.mkdirSync(folder, { recursive: true })
  fs.writeFileSync(path.join(folder, 'states.html'), '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>盈亏方向分歧增强标签核对</title><style>body{background:#151515;color:#eee;font:14px system-ui;margin:24px}section{border:1px solid #555;border-radius:6px;padding:16px;margin:16px 0}h2{font-size:16px}.text-caption{font-size:12px}.text-medium-emphasis{color:#aaa}table{border-collapse:collapse}th,td{border:1px solid #555;padding:8px}.mt-1,.mt-2{margin-top:8px}</style>' + rendered.map(x => `<section><h2>${x.name}</h2>${x.html}</section>`).join('\n') + '</html>', 'utf8')
  const files = ['scripts/verify-draw-profit-support.cjs', 'admin/src/utils/drawProfitSupport.ts', componentPath, metricsPath, 'admin/src/views/statistics/DrawHistory.vue', ...Object.keys(originals)]
  const review = { status: 'passed', version: support.DRAW_PROFIT_SUPPORT_VERSION, checks, historical, goParity, renderedStates: rendered.length, vueWarnings: warnings, originalSourcesUnchanged: Object.keys(originals), servicesStarted: false, networkRequests: false, modelTraining: false, sourceHashes: Object.fromEntries(files.map(file => [file, sha(file)])), limitations: ['Historical subgroup performance includes training samples and is not prospective validation.', 'TEMP historical snapshot and Go reference are optional; missing data is explicitly skipped.', 'Offline Vue rendering uses simple Vuetify stubs and does not claim browser layout verification.'] }
  fs.writeFileSync(path.join(folder, 'review.json'), JSON.stringify(review, null, 2) + '\n')
  console.log(JSON.stringify(review, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
