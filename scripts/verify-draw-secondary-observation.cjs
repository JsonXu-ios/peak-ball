// Offline contract and actual Vue SSR checks for secondary-v2 and ranked scores.
// No service, database, browser, network or model training is used.
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')
const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const ts = adminRequire('typescript')
const vue = adminRequire('vue')
const { parse, compileScript } = adminRequire('@vue/compiler-sfc')
const { renderToString } = adminRequire('@vue/server-renderer')
const read = name => fs.readFileSync(path.join(root, name), 'utf8')
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
const decision = extra => ({ version: 'secondary-v2-input-guards', decision: 'retained', threshold: .225, score: .3,
  reason: '报价完整，赛前交锋8场，二次保留', headToHeadSample: 8, minimumHeadToHead: 5, sportteryQuotesAvailable: true, ...extra })
const recommendation = extra => ({ version: 'draw-score-dynamic-v2', state: 'recommended', source: 'sporttery_score_odds',
  scores: [{ home: 2, away: 2 }, { home: 1, away: 1 }], reason: '按本场竞彩比分报价排序', ...extra })
const fixture = extra => ({ jingcaiId: '202610305002', frozenDrawSignal: { state: 'selected', score: .3 },
  secondaryDecision: decision(), scoreRecommendation: recommendation(), settled: false, regulationSettled: false, homeScore: 0, awayScore: 0, ...extra })
async function render(component, row, warnings) {
  const app = vue.createSSRApp({ render: () => vue.h(component, { row }) })
  app.config.warnHandler = warning => warnings.push(warning)
  app.component('VChip', { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
  return renderToString(app)
}
async function main() {
  equal(secondary.SECONDARY_DRAW_VERSION, 'secondary-v2-input-guards', 'current secondary contract is versioned')
  equal(score.DRAW_SCORE_RECOMMENDATION_VERSION, 'draw-score-dynamic-v2', 'dynamic score contract is versioned')
  equal(crypto.createHash('sha256').update(fs.readFileSync(path.join(root, 'go_server/internal/drawmodeldata/observation_v1.json'))).digest('hex'),
    'e7cb7d50668a76e916d2f86a6ab598a6bcb23ab5e6c961dbd972875346afd987', 'original frozen model is unchanged')
  equal(secondary.classifySecondaryDraw(fixture()).decision, 'retained', 'current backend retention is accepted')
  for (const payload of [undefined, null, decision({ version: 'secondary-v1' }), decision({ version: 'unknown' }), decision({ decision: 'SELECTED' }), decision({ threshold: .2 })]) {
    const row = fixture({ secondaryDecision: payload, frozenDrawSignal: { state: 'selected', score: .99 } })
    equal(secondary.classifySecondaryDraw(row).decision, 'unavailable', 'old/missing/invalid backend decision never falls back to high score')
    equal(secondary.secondaryDrawPresentation(row).label, '待更新', 'old contract is visibly waiting for update')
    equal(score.recommendDrawScores(row).scores, [], 'old secondary cannot receive scores')
  }
  const causes = [
    ['quotes', '竞彩初盘或即时盘报价不完整', { sportteryQuotesAvailable: false, reasonCodes: ['sporttery_quotes_missing'] }],
    ['history', '赛前交锋仅2场，少于5场', { headToHeadSample: 2, reasonCodes: ['head_to_head_insufficient'] }],
    ['score', '模型分数0.2100低于二次门槛0.225', { score: .21, reasonCodes: ['score_below_threshold'] }],
  ]
  for (const [name, reason, fields] of causes) {
    const row = fixture({ secondaryDecision: decision({ ...fields, decision: 'rejected', reason }) })
    equal(secondary.classifySecondaryDraw(row).decision, 'rejected', `${name}: follow backend rejection even with original selected`)
    equal(score.recommendDrawScores(row).state, 'not_selected', `${name}: rejected candidate cannot receive recommendation`)
    equal(row.frozenDrawSignal.state, 'selected', `${name}: original model marker is not erased`)
  }
  const multipleReasons = secondary.classifySecondaryDraw(fixture({ secondaryDecision: decision({ decision: 'rejected', reason: '条件不足', reasons: ['竞彩报价不完整', '赛前交锋仅2场'] }) }))
  check(multipleReasons.reason.includes('竞彩报价不完整') && multipleReasons.reason.includes('赛前交锋仅2场'), 'all independent rejection reasons stay visible')
  const onlyInputs = new Proxy(fixture(), { get(target, key) {
    if (!['frozenDrawSignal', 'secondaryDecision'].includes(key)) throw new Error(`Secondary accessed target/current-page field ${String(key)}`)
    return target[key]
  } })
  equal(secondary.classifySecondaryDraw(onlyInputs).decision, 'retained', 'secondary consumes only backend decision, never current-page history/odds/outcomes')
  equal(secondary.classifySecondaryDraw(fixture({ frozenDrawSignal: { state: 'not_selected', score: .9 } })).decision, 'unavailable', 'contradictory backend cannot promote original non-selection')
  const low = fixture({ secondaryDecision: decision({ decision: 'rejected', reason: causes[2][1], score: .21 }) })
  for (const outcome of [{ settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 }, { settled: true, regulationSettled: true, homeScore: 5, awayScore: 0 }]) {
    equal(secondary.classifySecondaryDraw({ ...low, ...outcome }), secondary.classifySecondaryDraw(low), 'outcomes cannot change rejection')
    equal(score.recommendDrawScores({ ...fixture(), ...outcome }), score.recommendDrawScores(fixture()), 'outcomes cannot reorder scores')
  }
  equal(secondary.secondaryDrawPresentation({ ...low, settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 }).outcomeLabel, '误剔平局', 'excluded actual draw stays visible')
  equal(secondary.secondaryDrawPresentation({ ...low, settled: true, regulationSettled: true, homeScore: 2, awayScore: 0 }).outcomeLabel, '剔除正确', 'excluded decisive result is audited')
  equal(score.recommendDrawScores(fixture()).scores, ['2:2', '1:1'], 'backend ranking is preserved without a fixed fallback')
  equal(score.recommendDrawScores(fixture({ scoreRecommendation: recommendation({ scores: [{ home: 3, away: 3 }, { home: 2, away: 2 }], source: 'goal_estimate' }) })).scores, ['3:3', '2:2'], 'higher draw scores are accepted in server order')
  equal(score.recommendDrawScores(fixture({ scoreRecommendation: recommendation({ scores: [{ home: 6, away: 6 }, { home: 0, away: 0 }] }) })).scores, ['6:6', '0:0'], 'valid server draw range includes zero and six')
  for (const payload of [undefined, null, recommendation({ version: 'draw-score-baseline-v1' }), recommendation({ version: 'unknown' }), recommendation({ state: 'not_selected' }), recommendation({ reason: '' })]) {
    equal(score.recommendDrawScores(fixture({ scoreRecommendation: payload })).state, 'unavailable', 'old/missing score payload stays pending with no manufactured 1:1/0:0')
    equal(score.recommendDrawScores(fixture({ scoreRecommendation: payload })).scores, [], 'no fallback scores are fabricated')
  }
  for (const bad of [null, [], [{ home: 2, away: 2 }], [{ home: 2, away: 2 }, { home: 2, away: 2 }],
    [{ home: 2, away: 1 }, { home: 0, away: 0 }], [{ home: 7, away: 7 }, { home: 1, away: 1 }],
    [{ home: -1, away: -1 }, { home: 0, away: 0 }], [{ home: 1.5, away: 1.5 }, { home: 1, away: 1 }],
    [{ home: '2', away: '2' }, { home: 1, away: 1 }], [{ home: NaN, away: NaN }, { home: 0, away: 0 }],
    [null, { home: 1, away: 1 }], [{ home: 1, away: 1 }, { home: 2, away: 2 }, { home: 3, away: 3 }]]) {
    equal(score.recommendDrawScores(fixture({ scoreRecommendation: recommendation({ scores: bad }) })).state, 'unavailable', 'malformed, duplicate or non-draw backend scores are rejected')
  }
  const proxiedScores = new Proxy(fixture(), { get(target, key) {
    if (!['jingcaiId', 'frozenDrawSignal', 'secondaryDecision', 'scoreRecommendation'].includes(key)) throw new Error(`Score choice read target field ${String(key)}`)
    return target[key]
  } })
  equal(score.recommendDrawScores(proxiedScores).scores, ['2:2', '1:1'], 'score choice never reads final goals/status')
  const mutated = score.recommendDrawScores(fixture()); mutated.scores.pop()
  equal(score.recommendDrawScores(fixture()).scores.length, 2, 'recommendation arrays are isolated between calls')
  equal(score.drawScorePresentation(fixture({ settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 })).outcomeLabel, '首选命中', 'first dynamic score settles correctly')
  equal(score.drawScorePresentation(fixture({ settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 })).outcomeLabel, '次选命中（前二命中）', 'second dynamic score settles correctly')
  equal(score.drawScorePresentation(fixture({ settled: true, regulationSettled: true, homeScore: 0, awayScore: 0 })).outcomeLabel, '前二未命中', 'no hidden 0:0 recommendation survives')
  for (const extra of [{ settled: true, regulationSettled: false }, { settled: true, regulationSettled: true, homeScore: NaN }, { settled: true, regulationSettled: true, homeScore: -1 }, { settled: true, regulationSettled: true, homeScore: 1.2 }]) {
    equal(score.drawScorePresentation(fixture(extra)).outcomeLabel, '待赛果核对', 'ambiguous or invalid results do not settle recommendations')
  }
  const components = {
    secondary: load(compileScript(parse(read('admin/src/views/statistics/DrawSecondarySignal.vue')).descriptor, { id: 'secondary-v2-offline', inlineTemplate: true }).content, { '@/utils/drawSecondarySignal': secondary }).default,
    scores: load(compileScript(parse(read('admin/src/views/statistics/DrawScoreRecommendation.vue')).descriptor, { id: 'scores-v2-offline', inlineTemplate: true }).content, { '@/utils/drawScoreRecommendation': score }).default,
  }
  const cases = [
    ['retained', fixture(), '二次入选', '首选'],
    ...causes.map(([name, reason, fields]) => [name, fixture({ secondaryDecision: decision({ ...fields, decision: 'rejected', reason }) }), reason, '未二次入选']),
    ['legacy', fixture({ secondaryDecision: undefined }), '待更新', '待更新'],
    ['no scores', fixture({ scoreRecommendation: undefined }), '二次入选', '待更新'],
    ['old scores', fixture({ scoreRecommendation: recommendation({ version: 'draw-score-baseline-v1' }) }), '二次入选', '待更新'],
    ['estimate', fixture({ scoreRecommendation: recommendation({ source: 'goal_estimate', reason: '按近期进球、交锋和大小球估计', scores: [{ home: 3, away: 3 }, { home: 2, away: 2 }] }) }), '二次入选', '3:3'],
    ['first hit', fixture({ settled: true, regulationSettled: true, homeScore: 2, awayScore: 2 }), '命中平局', '首选命中'],
    ['second hit', fixture({ settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 }), '命中平局', '次选命中'],
  ]
  const warnings = [], rendered = []
  for (const [name, row, secondText, scoreText] of cases) {
    const a = await render(components.secondary, row, warnings), b = await render(components.scores, row, warnings)
    check(a.includes(secondText), `${name}: backend secondary reason/decision is visible`)
    check(b.includes(scoreText), `${name}: ranked recommendation or pending state is visible`)
    check(!b.includes('基线推荐') && !b.includes('固定推荐'), `${name}: obsolete baseline wording is removed`)
    if (score.recommendDrawScores(row).state === 'recommended') check(b.includes(row.scoreRecommendation.reason), `${name}: recommendation source reasoning is visible`)
    rendered.push({ name, secondaryHTML: a, scoresHTML: b })
  }
  equal(warnings, [], 'real Vue components render all decision and score states without warnings')
  const page = read('admin/src/views/statistics/DrawHistory.vue')
  check(page.includes('<DrawFrozenSignal :row="row"') && page.includes('<DrawResearchSource :row="row"'), 'original model marker and research provenance are preserved')
  check(page.includes('赛前交锋至少5场') && !page.includes('二次筛选 v1'), 'shared list explains current guards')
  check(!page.includes('固定推荐1:1、0:0'), 'shared list has no fixed-score claim')
  const realPath = process.env.DRAW_CURRENT_RESPONSE_JSON || path.join(os.tmpdir(), 'peak-ball-20261031-current-api-review.json')
  let realReview = { status: 'skipped', reason: 'No actual response supplied; no substitute is fabricated.' }
  if (fs.existsSync(realPath)) {
    const response = JSON.parse(fs.readFileSync(realPath, 'utf8').replace(/^\uFEFF/, ''))
    check(Array.isArray(response.items), 'actual response rows exist')
    let oldPending = 0, current = 0, retained = 0, rejected = 0, recommended = 0, higherDraws = 0
    for (const row of response.items) {
      const result = secondary.classifySecondaryDraw(row)
      if (row.secondaryDecision?.version !== secondary.SECONDARY_DRAW_VERSION) { equal(result.decision, 'unavailable', 'actual old API row cannot masquerade as v2'); oldPending++ }
      else { equal(result.decision, row.secondaryDecision.decision, 'actual current API uses backend decision'); current++ }
      const html = await render(components.secondary, row, warnings)
      check(html.includes(secondary.secondaryDrawPresentation(row).label), 'actual API decision renders correctly')
      if (result.decision === 'retained') retained++
      if (result.decision === 'rejected') {
        rejected++
        for (const reason of result.reasons) check(html.includes(reason), 'actual backend exclusion reasons are visible')
      }
      const ranked = score.recommendDrawScores(row), scoreHTML = await render(components.scores, row, warnings)
      check(scoreHTML.includes(score.drawScorePresentation(row).label), 'actual score source/pending state is visible')
      if (ranked.state === 'recommended') {
        recommended++
        equal(ranked.scores, row.scoreRecommendation.scores.map(x => `${x.home}:${x.away}`), 'actual backend score rank is not reordered')
        for (const value of ranked.scores) check(scoreHTML.includes(value), 'actual recommended score renders')
        if (row.scoreRecommendation.scores.some(x => x.home >= 2)) higherDraws++
        check(scoreHTML.includes(row.scoreRecommendation.reason), 'actual score source reason is shown')
      } else check(!scoreHTML.includes('首选 '), 'non-candidates have no hidden fixed score fallback')
    }
    if (oldPending === 0 && response.observationMetrics?.secondaryVersion === secondary.SECONDARY_DRAW_VERSION) {
      equal(retained, response.observationMetrics.secondary.selected, 'actual per-row decisions match full-window secondary total')
      equal(rejected, response.observationMetrics.rejected.selected, 'actual per-row exclusions match full-window rejected total')
    }
    equal(warnings, [], 'actual response rows render without Vue warnings')
    realReview = { status: 'passed', rows: response.items.length, oldPending, current, retained, rejected, recommended, higherDraws }
  }
  const report = { status: 'passed', checks, version: secondary.SECONDARY_DRAW_VERSION, scoreVersion: score.DRAW_SCORE_RECOMMENDATION_VERSION,
    renderedStates: cases.length, actualResponse: realReview, servicesStarted: false, networkRequests: false, originalModelChanged: false, vueWarnings: warnings }
  fs.writeFileSync(path.join(os.tmpdir(), 'peak-ball-secondary-v2-ui-review.json'), JSON.stringify(report, null, 2))
  fs.writeFileSync(path.join(os.tmpdir(), 'peak-ball-secondary-v2-ui-states.html'), '<!doctype html><meta charset="utf-8">' + rendered.map(x => `<section><h2>${x.name}</h2>${x.secondaryHTML}${x.scoresHTML}</section>`).join('\n'))
  console.log(JSON.stringify(report, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
