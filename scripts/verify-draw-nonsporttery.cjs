// Offline scope isolation, settlement and actual Vue rendering checks.
// Does not operate a browser, service, database or production model.
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const ts = adminRequire('typescript')
const vue = adminRequire('vue')
const { parse, compileScript } = adminRequire('@vue/compiler-sfc')
const { renderToString } = adminRequire('@vue/server-renderer')
const read = file => fs.readFileSync(path.join(root, file), 'utf8')
let checks = 0
const equal = (actual, expected, reason) => { assert.deepEqual(actual, expected, reason); checks++ }
const check = (value, reason) => { assert.ok(value, reason); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', code)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const selectionContract = load(read('admin/src/utils/drawFrozenSignal.ts'))
const currentModel = extra => ({ selectionVersion: 'jc-h2h-required-v1', minimumHeadToHead: 1, headToHeadSample: 8, ...extra })
const frontend = load(read('admin/src/utils/drawFrontend.ts'))
const nonJc = load(read('admin/src/utils/drawNonSporttery.ts'), { './drawFrontend': frontend })
const jc = load(read('admin/src/utils/drawAlternativeOutcome.ts'), { './drawFrontend': frontend, './drawFrozenSignal': selectionContract })
const fixture = extra => ({
  matchId: 'nonjc-fixture', jingcaiId: '', home: '主队', guest: '客队',
  nonSportterySignal: { state: 'selected', score: .3, modelVersion: 'sporttery-draw-observation-v1' },
  frozenDrawSignal: currentModel({ state: 'out_of_scope' }), frontendAnalysis: { averageEuroOdds: [2.2, 3.3, 3.1] },
  settled: false, regulationSettled: false, homeScore: 0, awayScore: 0, ...extra,
})
const recommend = nonJc.recommendNonSportteryAlternative
const primary = nonJc.nonSportteryDrawPresentation
const presentation = nonJc.nonSportteryAlternativePresentation

async function main() {
  equal(recommend(fixture()).direction, 'home', 'non-JC uses real latest average HOME/AWAY low price')
  equal(recommend(fixture({ frontendAnalysis: { averageEuroOdds: [4, 3, 1.8] } })).direction, 'away', 'away low price remains available')
  equal(recommend(fixture({ frontendAnalysis: { averageEuroOdds: [2.2, null, 3.1] } })).direction, 'home', 'a draw quote is not needed to compare two side prices')
  for (const nonSportterySignal of [undefined, null, { state: 'unavailable' }]) {
    equal(recommend(fixture({ nonSportterySignal })).state, 'unavailable', 'missing backend selection cannot invent an alternative')
  }
  equal(primary(fixture({ nonSportterySignal: undefined })).label, '等待服务更新', 'an older API contract is explicitly identified')
  equal(primary(fixture({ nonSportterySignal: { state: 'unavailable', reason: '输入不足' } })).label, '数据不足', 'missing inputs remain unavailable')
  equal(recommend(fixture({ nonSportterySignal: { state: 'not_selected', score: .99 } })).state, 'not_selected', 'authoritative non-selection is never promoted by a score')
  equal(recommend(fixture({ nonSportterySignal: { state: 'selected', score: .21 } })).state, 'recommended', 'the JC secondary gate is not imported')
  const jcRow = fixture({ jingcaiId: '202611037001', frozenDrawSignal: currentModel({ state: 'selected', score: .3 }) })
  const originalJc = jc.recommendDrawAlternativeOutcome(jcRow)
  equal(recommend(jcRow).state, 'out_of_scope', 'JC rows never receive a second non-JC alternative')
  equal(primary(jcRow).label, '—', 'the non-JC column is empty for JC rows')
  for (const state of ['selected', 'not_selected', 'unavailable', 'out_of_scope']) {
    equal(jc.recommendDrawAlternativeOutcome({ ...jcRow, nonSportterySignal: { state, score: .99 } }), originalJc, 'non-JC signal cannot alter JC results')
    equal(recommend(fixture({ frozenDrawSignal: currentModel({ state, score: .99 }) })), recommend(fixture()), 'JC signal cannot alter non-JC results')
  }
  const scopeOnly = new Proxy(jcRow, { get(target, key) {
    if (key !== 'jingcaiId') throw Error(`JC exclusion unexpectedly read ${String(key)}`)
    return target[key]
  } })
  equal(recommend(scopeOnly).state, 'out_of_scope', 'scope exclusion reads no model, quote or result inputs')
  const noTargets = new Proxy(fixture(), { get(target, key) {
    if (!['jingcaiId', 'nonSportterySignal', 'frontendAnalysis'].includes(key)) throw Error(`Choice read result or unrelated input ${String(key)}`)
    return target[key]
  } })
  equal(recommend(noTargets), recommend(fixture()), 'the choice never reads outcomes, JC selection or secondary filters')
  for (const odds of [undefined, null, [], [null, 3, 2], [2, 3, 1], [NaN, 3, 2], [2, 3, Infinity], [2.5, 3, 2.5 + 5e-10]]) {
    equal(recommend(fixture({ frontendAnalysis: { averageEuroOdds: odds } })).state, 'unavailable', 'missing, malformed or tied prices abstain')
  }
  const averageMarket = { key: 'localAverageOdds', oddsAvailable: true, odds: { home: 2.2, draw: 3.3, away: 3.1 } }
  equal(recommend(fixture({ frontendAnalysis: { localMarket: averageMarket } })).direction, 'home', 'actual legacy average representation is supported')
  equal(recommend(fixture({ frontendAnalysis: { averageEuroOdds: null, localMarket: averageMarket } })).state, 'unavailable', 'explicit missing average cannot revive stale prices')
  equal(recommend(fixture({ frontendAnalysis: { localMarket: { ...averageMarket, key: 'sportterySim' } } })).state, 'unavailable', 'simulated quotes never replace real average quotes')
  const complete = (homeScore, awayScore, extra = {}) => fixture({ settled: true, regulationSettled: true, homeScore, awayScore, ...extra })
  equal(primary(fixture()).label, '入选 · 待赛果', 'unstarted 0:0 is not a historical hit')
  equal(primary(complete(0, 0)).label, '命中平局', 'confirmed 0:0 credits the primary draw')
  equal(primary(complete(2, 1)).label, '入选 · 未打平', 'a decisive result does not retroactively remove selection')
  equal(presentation(complete(2, 1)).outcomeLabel, '次选命中', 'matching decisive outcome credits alternative')
  equal(presentation(complete(1, 2)).outcomeLabel, '主次均未命中', 'opposite outcome is a real miss')
  equal(presentation(complete(1, 1)).outcomeLabel, '主选平命中', 'a draw never credits a binary alternative')
  for (const extra of [{ regulationSettled: undefined }, { settled: false }, { homeScore: -1 }, { awayScore: 1.5 }]) {
    equal(primary(complete(1, 1, extra)).label, '入选 · 待赛果', 'invalid regulation result remains pending')
  }
  const before = JSON.stringify(jcRow)
  recommend(jcRow); primary(jcRow); presentation(jcRow)
  equal(JSON.stringify(jcRow), before, 'separate UI never mutates API fields')

  const component = load(compileScript(parse(read('admin/src/views/statistics/DrawNonSporttery.vue')).descriptor, {
    id: 'draw-nonsporttery-offline', inlineTemplate: true,
  }).content, { '@/utils/drawNonSporttery': nonJc }).default
  const warnings = []
  const rendered = []
  const cases = [
    ['pending', fixture(), '入选 · 待赛果', '次选 主胜'],
    ['draw', complete(0, 0), '命中平局', '主选平命中'],
    ['alternative', complete(2, 1), '入选 · 未打平', '次选命中'],
    ['miss', complete(1, 2), '入选 · 未打平', '主次均未命中'],
    ['away', fixture({ frontendAnalysis: { averageEuroOdds: [4, 3, 1.8] } }), '入选 · 待赛果', '次选 客胜'],
    ['missing quotes', fixture({ frontendAnalysis: { averageEuroOdds: null } }), '次选暂无', '缺少有效'],
    ['same price', fixture({ frontendAnalysis: { averageEuroOdds: [2.5, 3, 2.5] } }), '次选暂无', '主客同价'],
    ['not selected', fixture({ nonSportterySignal: { state: 'not_selected' } }), '未入选', null],
    ['old API', fixture({ nonSportterySignal: undefined }), '等待服务更新', null],
    ['JC', jcRow, '—', null],
  ]
  for (const [name, row, label, detail] of cases) {
    const app = vue.createSSRApp({ render: () => vue.h(component, { row }) })
    app.component('VChip', { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
    app.config.warnHandler = warning => warnings.push(warning)
    const html = await renderToString(app)
    check(html.includes(label) && (!detail || html.includes(detail)), `${name}: actual Vue markup presents the correct state`)
    if (primary(row).state !== 'selected') check(!html.includes('data-nonsporttery-alternative='), `${name}: non-candidate displays no hypothetical backup`)
    rendered.push({ name, html })
  }
  equal(warnings, [], 'new states render without Vue warnings')
  const page = read('admin/src/views/statistics/DrawHistory.vue')
  const header = page.match(/<thead><tr>(.*?)<\/tr><\/thead>/s)[1]
  equal((header.match(/<th(?:\s|>)/g) || []).length, 23, 'main table retains the independent totals column alongside the non-JC observation')
  check(page.includes('const observationColumnCount = 23'), 'one constant controls full-width rows including totals')
  equal((page.match(/:colspan="observationColumnCount"/g) || []).length, 2, 'expanded and empty rows both span the new table')
  check(header.indexOf('非竞彩选平 / 次选') > header.indexOf('次选胜负'), 'the non-JC column is adjacent to the original alternative')
  check(page.includes('<DrawAlternativeOutcome :row="row" />') && page.includes('<DrawNonSporttery :row="row" />'), 'existing and new scope-specific components remain separate')
  const recordedDir = path.join(root, 'reports/nonjc-draw-secondary-20261104')
  let recordedSnapshot = { status: 'skipped', reason: 'No matched recorded snapshot and model replay supplied.' }
  const files = ['current-study.json', 'model-replay.json', 'report.json'].map(name => path.join(recordedDir, name))
  if (files.every(file => fs.existsSync(file))) {
    const body = fs.readFileSync(files[0])
    const dataset = JSON.parse(body.toString('utf8'))
    const replay = JSON.parse(fs.readFileSync(files[1], 'utf8'))
    const expected = JSON.parse(fs.readFileSync(files[2], 'utf8')).results.all
    equal(crypto.createHash('sha256').update(body).digest('hex'), replay.current.inputSHA256, 'real data and actual Go model replay use the same source snapshot')
    const predictions = new Map(replay.current.rows.map(row => [String(row.matchId), row]))
    const counts = { nonJc: 0, selected: 0, drawHits: 0, alternatives: 0, alternativeHits: 0, jcUnchanged: 0 }
    for (const raw of dataset.rows) {
      const prediction = predictions.get(String(raw.matchId))
      check(prediction, `recorded ${raw.matchId}: actual Go prediction exists`)
      const signal = { state: prediction.observedFeatures === 0 ? 'unavailable' : prediction.selected ? 'selected' : 'not_selected',
        score: prediction.score, modelVersion: replay.modelVersion }
      const row = { jingcaiId: raw.jingcaiId, settled: true, regulationSettled: true,
        homeScore: raw.homeScore, awayScore: raw.awayScore, frontendAnalysis: raw.frontend,
        frozenDrawSignal: raw.jingcaiId.trim() ? signal : { state: 'out_of_scope' },
        nonSportterySignal: raw.jingcaiId.trim() ? undefined : signal }
      if (row.jingcaiId.trim()) {
        equal(jc.recommendDrawAlternativeOutcome({ ...row, nonSportterySignal: { state: 'selected' } }), jc.recommendDrawAlternativeOutcome(row), `recorded JC ${raw.matchId}: independent signal cannot affect original alternative`)
        equal(recommend(row).state, 'out_of_scope', `recorded JC ${raw.matchId}: excluded from independent column`)
        counts.jcUnchanged++
        continue
      }
      counts.nonJc++
      if (signal.state !== 'selected') {
        check(recommend(row).state !== 'recommended', `recorded ${raw.matchId}: no recommendation for missing/unselected model`)
        continue
      }
      counts.selected++
      if (primary(row).label === '命中平局') counts.drawHits++
      if (recommend(row).state === 'recommended') counts.alternatives++
      if (presentation(row).outcomeLabel === '次选命中') counts.alternativeHits++
    }
    equal(counts.nonJc, expected.nonJcCompleted, 'real non-JC scope matches the independent offline audit')
    equal(counts.selected, expected.selected, 'real non-JC selected count matches the actual Go replay')
    equal(counts.drawHits, expected.selectedDraws, 'real confirmed draw results are presented correctly')
    equal(counts.alternatives, expected.secondaryAvailable, 'real available/abstained alternatives match independent audit')
    equal(counts.alternativeHits, expected.secondaryHits, 'real non-draw hits do not include primary draw hits')
    recordedSnapshot = { status: 'passed', ...counts }
  }
  const report = { status: 'passed', checks, renderedStates: rendered.length, recordedSnapshot, modelChanged: false, servicesStarted: false, warnings }
  const out = path.join(root, 'reports/nonjc-column-ui-review')
  fs.mkdirSync(out, { recursive: true })
  fs.writeFileSync(path.join(out, 'report.json'), JSON.stringify(report, null, 2))
  fs.writeFileSync(path.join(out, 'rendered.html'), '<!doctype html><meta charset="utf-8">' + rendered.map(x => `<section><h2>${x.name}</h2>${x.html}</section>`).join('\n'))
  console.log(JSON.stringify(report, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
