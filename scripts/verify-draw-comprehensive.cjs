// Offline checks for the independent full-input decision-tree column.
// No browser, services, database, model fitting or source-data mutation.
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const req = createRequire(path.join(root, 'admin/package.json'))
const ts = req('typescript')
const vue = req('vue')
const { parse, compileScript } = req('@vue/compiler-sfc')
const { renderToString } = req('@vue/server-renderer')
const read = file => fs.readFileSync(path.join(root, file), 'utf8')
let checks = 0
const equal = (actual, expected, reason) => { assert.deepEqual(actual, expected, reason); checks++ }
const check = (value, reason) => { assert.ok(value, reason); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', code)(name => name in imports ? imports[name] : req(name), module, module.exports)
  return module.exports
}
const full = load(read('admin/src/utils/drawComprehensive.ts'))
const selectionContract = load(read('admin/src/utils/drawFrozenSignal.ts'))
const currentModel = extra => ({ selectionVersion: 'jc-h2h-required-v1', minimumHeadToHead: 1, headToHeadSample: 8, ...extra })
const frontend = load(read('admin/src/utils/drawFrontend.ts'))
const legacy = load(read('admin/src/utils/drawAlternativeOutcome.ts'), { './drawFrontend': frontend, './drawFrozenSignal': selectionContract })
const nonJc = load(read('admin/src/utils/drawNonSporttery.ts'), { './drawFrontend': frontend })
const primary = full.comprehensiveDrawPresentation
const alternative = full.comprehensiveAlternativePresentation
const defaultSignal = () => ({
  modelVersion: 'draw-full-tree-v1', inputVersion: 'draw-full-input-v1', state: 'selected', score: .45, threshold: .4,
  alternativeDirection: 'away', alternativeScores: { home: .3, away: .7 }, drawLeafSamples: 70, alternativeLeafSamples: 55,
  observedInputs: 650, totalInputs: 720,
  drawPath: [{ feature: 'heat.home', label: '主队热度', module: 'heat', value: 72, usedValue: 72, threshold: 65, imputed: false, wentLeft: false }],
  alternativePath: [{ feature: 'profit.home', label: '主胜模拟盈亏', module: 'profit', value: null, usedValue: 12, threshold: 20, imputed: true, wentLeft: true }],
  moduleCoverage: [{ key: 'profit', label: '真实与模拟盈亏', available: 20, total: 24, used: 3 }],
  reason: '全量数据新树独立判断', alternativeReason: '胜负树选择客胜',
})
const fixture = extra => ({
  matchId: 'full-tree-fixture', jingcaiId: '', settled: false, regulationSettled: false, homeScore: 0, awayScore: 0,
  frozenDrawSignal: currentModel({ state: 'not_selected', score: .1 }), nonSportterySignal: { state: 'not_selected', score: .1 },
  secondaryDecision: { decision: 'rejected' }, frontendAnalysis: { averageEuroOdds: [1.2, 4, 8] },
  comprehensiveSignal: defaultSignal(), ...extra,
})
const settled = (h, a, extra = {}) => fixture({ settled: true, regulationSettled: true, homeScore: h, awayScore: a, ...extra })

async function main() {
  equal(primary(fixture()).label, '新树入选 · 待赛果', 'unstarted 0:0 is pending')
  equal(alternative(fixture()).direction, 'away', 'server tree side is honored even against the lowest odds')
  equal(alternative(fixture()).label, '树次选 客胜', 'new recommendation identifies the separate tree')
  equal(primary(settled(0, 0)).label, '新树命中平局', 'explicit completed regulation 0:0 credits the draw')
  equal(alternative(settled(0, 0)).outcomeLabel, '主选平命中', 'draw does not credit the binary alternative')
  equal(alternative(settled(1, 2)).outcomeLabel, '树次选命中', 'away decisive result credits away tree choice')
  equal(alternative(settled(2, 1)).outcomeLabel, '新树主次均未命中', 'opposite decisive outcome is a true miss')
  const home = fixture({ comprehensiveSignal: { ...defaultSignal(), alternativeDirection: 'home' } })
  equal(alternative(home).label, '树次选 主胜', 'home choice is supported')
  equal(alternative({ ...home, settled: true, regulationSettled: true, homeScore: 2, awayScore: 1 }).outcomeLabel, '树次选命中', 'home decisive result settles correctly')

  for (const comprehensiveSignal of [undefined, null]) {
    equal(primary(fixture({ comprehensiveSignal })).label, '等待服务更新', 'an older service never borrows an old model result')
    equal(alternative(fixture({ comprehensiveSignal })).state, 'not_selected', 'missing new selection has no alternative')
  }
  equal(primary(fixture({ comprehensiveSignal: { ...defaultSignal(), state: 'unavailable', reason: '模型尚未加载' } })).label, '新树暂不可用', 'new model failure is isolated')
  equal(primary(fixture({ comprehensiveSignal: { ...defaultSignal(), state: 'not_selected', score: .99 } })).state, 'not_selected', 'high numeric score cannot override the authoritative state')
  equal(alternative(fixture({ comprehensiveSignal: { ...defaultSignal(), state: 'not_selected' } })).state, 'not_selected', 'server side scores do not recommend for non-candidates')
  equal(primary(fixture({ comprehensiveSignal: { ...defaultSignal(), state: 'unexpected' } })).state, 'unavailable', 'unknown state is not promoted')
  for (const direction of [undefined, null, 'draw', '', 'other']) {
    const row = fixture({ comprehensiveSignal: { ...defaultSignal(), alternativeDirection: direction } })
    equal(alternative(row).state, 'unavailable', 'no server-owned side abstains rather than using odds or leaf scores')
    equal(alternative(row).direction, null, 'abstention supplies no hidden direction')
    equal(alternative(row).outcomeLabel, '', 'no side gets no hypothetical settlement credit')
  }
  for (const extra of [{ settled: false }, { regulationSettled: false }, { regulationSettled: undefined }, { homeScore: -1 }, { awayScore: 1.5 }, { homeScore: NaN }]) {
    const row = settled(1, 2, extra)
    equal(primary(row).label, '新树入选 · 待赛果', 'unconfirmed or malformed result is pending')
    equal(alternative(row).outcomeLabel, '待赛果核对', 'binary hit requires confirmed regulation result')
  }

  // No old eligibility, original secondary decision, prices or company data
  // can change this column's selection and direction.
  const reference = fixture()
  for (const jingcaiId of ['', '20261104001']) for (const state of ['selected', 'not_selected', 'unavailable', 'out_of_scope']) {
    const row = fixture({ jingcaiId, frozenDrawSignal: currentModel({ state, score: .99 }), nonSportterySignal: { state, score: .99 },
      secondaryDecision: { decision: 'rejected' }, frontendAnalysis: { averageEuroOdds: [8, 4, 1.2] } })
    equal(primary(row), primary(reference), 'JC, non-JC and old eligibility cannot change the new opinion')
    equal(alternative(row), alternative(reference), 'old opinions and reversed market prices cannot change the new tree side')
  }
  const pickOnly = new Proxy(reference, { get(target, key) {
    if (!['comprehensiveSignal', 'settled', 'regulationSettled', 'homeScore', 'awayScore'].includes(key)) throw Error(`new column read unrelated ${String(key)}`)
    return target[key]
  } })
  equal(primary(pickOnly), primary(reference), 'presentation reads no old model or business inputs')
  equal(alternative(pickOnly), alternative(reference), 'direction is exclusively server-owned')
  const legacyRow = fixture({ jingcaiId: '20261104001', frozenDrawSignal: currentModel({ state: 'selected', score: .3 }) })
  const nonJcRow = fixture({ nonSportterySignal: { state: 'selected', score: .3 } })
  for (const state of ['selected', 'not_selected', 'unavailable']) {
    const comprehensiveSignal = { ...defaultSignal(), state, alternativeDirection: 'away' }
    equal(legacy.recommendDrawAlternativeOutcome({ ...legacyRow, comprehensiveSignal }), legacy.recommendDrawAlternativeOutcome({ ...legacyRow, comprehensiveSignal: undefined }), 'independent field leaves the original JC backup unchanged')
    equal(nonJc.recommendNonSportteryAlternative({ ...nonJcRow, comprehensiveSignal }), nonJc.recommendNonSportteryAlternative({ ...nonJcRow, comprehensiveSignal: undefined }), 'independent field leaves the non-JC low-price backup unchanged')
  }
  const original = JSON.stringify(reference)
  primary(reference); alternative(reference)
  equal(JSON.stringify(reference), original, 'new presentation never mutates backend or old model fields')
  equal(full.comprehensiveNumber(null), '暂无', 'missing values are visibly unknown')
  equal(full.comprehensiveNumber(0), '0', 'observed zero is retained')
  equal(full.comprehensiveNumber(Infinity), '暂无', 'nonfinite scores are not displayed as confidence')

  const descriptor = parse(read('admin/src/views/statistics/DrawComprehensiveSignal.vue')).descriptor
  const component = load(compileScript(descriptor, { id: 'draw-comprehensive-offline', inlineTemplate: true }).content, { '@/utils/drawComprehensive': full }).default
  const warnings = []
  const cases = [
    ['pending', fixture(), ['新树入选 · 待赛果', '树次选 客胜', '查看新树依据']],
    ['draw', settled(0, 0), ['新树命中平局', '主选平命中']],
    ['alternative hit', settled(1, 2), ['树次选命中']],
    ['both miss', settled(2, 1), ['新树主次均未命中']],
    ['not selected', fixture({ comprehensiveSignal: { ...defaultSignal(), state: 'not_selected' } }), ['新树未选平']],
    ['unavailable', fixture({ comprehensiveSignal: { ...defaultSignal(), state: 'unavailable', reason: '全量模型未加载' } }), ['新树暂不可用', '全量模型未加载']],
    ['old API', fixture({ comprehensiveSignal: undefined }), ['等待服务更新']],
    ['side abstention', fixture({ comprehensiveSignal: { ...defaultSignal(), alternativeDirection: null } }), ['次选暂无']],
    ['home choice', home, ['树次选 主胜']],
  ]
  const rendered = []
  for (const [name, row, expected] of cases) {
    const app = vue.createSSRApp({ render: () => vue.h(component, { row }) })
    app.component('VChip', { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
    app.config.warnHandler = warning => warnings.push(warning)
    const markup = await renderToString(app)
    check(markup.includes('实验对照'), `${name}: new column is visibly marked as an experiment`)
    for (const text of expected) check(markup.includes(text), `${name}: actual Vue presents ${text}`)
    if (row.comprehensiveSignal) {
      check(markup.includes('未经概率校准'), `${name}: tree frequencies are not sold as match probabilities`)
      check(markup.includes('draw-full-tree-v1'), `${name}: model provenance is visible`)
    }
    if (primary(row).state !== 'selected') check(!markup.includes('data-comprehensive-alternative='), `${name}: non-candidates have no hypothetical backup`)
    if (row.comprehensiveSignal) {
      check(markup.includes('主队热度') && markup.includes('72'), `${name}: selected branch inputs are inspectable`)
      check(markup.includes('缺失，填补 12'), `${name}: imputation is not misrepresented as an observation`)
      check(markup.includes('真实与模拟盈亏') && markup.includes('20 / 24'), `${name}: module data coverage is transparent`)
    }
    rendered.push({ name, markup })
  }
  equal(warnings, [], 'new column renders with no Vue warnings')
  check(descriptor.template.content.includes('@click.stop') && descriptor.template.content.includes('@keydown.stop'), 'opening paths does not toggle the parent match row')
  const malicious = fixture({ comprehensiveSignal: { ...defaultSignal(), reason: '<img src=x onerror=alert(1)>' } })
  const app = vue.createSSRApp({ render: () => vue.h(component, { row: malicious }) })
  app.component('VChip', { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
  const escaped = await renderToString(app)
  check(!escaped.includes('<img src=x'), 'source reasons remain escaped text')

  const actualFile = process.env.DRAW_COMPREHENSIVE_SIGNALS || path.join(root, 'reports/full-model-20261104/handler-signals.json')
  let actualHandlerSignals = { status: 'skipped', reason: 'No offline handler-signal fixture supplied.' }
  if (fs.existsSync(actualFile)) {
    const recorded = JSON.parse(fs.readFileSync(actualFile, 'utf8').replace(/^\uFEFF/, ''))
    check(Array.isArray(recorded.items) && recorded.items.length > 0, 'real handler fixture contains observation rows')
    const counts = { rows: 0, selected: 0, notSelected: 0, unavailable: 0, primaryHits: 0, alternativeHits: 0, misses: 0, pending: 0 }
    const samples = new Map()
    for (const row of recorded.items) {
      const before = JSON.stringify(row)
      const signal = row.comprehensiveSignal
      check(signal && typeof signal.modelVersion === 'string', `actual ${row.matchId}: new handler provenance exists`)
      const first = primary(row), second = alternative(row)
      equal(first.state, signal.state, `actual ${row.matchId}: new handler selection is not recomputed`)
      counts.rows++
      if (first.state === 'selected') {
        counts.selected++
        const completed = row.settled === true && row.regulationSettled === true && Number.isInteger(row.homeScore) && row.homeScore >= 0 && Number.isInteger(row.awayScore) && row.awayScore >= 0
        if (!completed) {
          equal(first.label, '新树入选 · 待赛果', `actual ${row.matchId}: pending target is not a hit`)
          counts.pending++
        } else if (row.homeScore === row.awayScore) {
          equal(first.label, '新树命中平局', `actual ${row.matchId}: draw target is credited once`)
          counts.primaryHits++
        }
        if (['home', 'away'].includes(signal.alternativeDirection)) {
          equal(second.direction, signal.alternativeDirection, `actual ${row.matchId}: actual tree direction reaches the UI unchanged`)
          if (completed && row.homeScore !== row.awayScore) {
            const hit = (row.homeScore > row.awayScore ? 'home' : 'away') === signal.alternativeDirection
            equal(second.outcomeLabel, hit ? '树次选命中' : '新树主次均未命中', `actual ${row.matchId}: decisive settlement is correct`)
            if (hit) counts.alternativeHits++; else counts.misses++
          }
        } else equal(second.state, 'unavailable', `actual ${row.matchId}: abstained side is not reconstructed`)
      } else {
        equal(second.state, 'not_selected', `actual ${row.matchId}: non-selected row has no backup`)
        if (first.state === 'not_selected') counts.notSelected++; else counts.unavailable++
      }
      equal(JSON.stringify(row), before, `actual ${row.matchId}: UI leaves all old/new API fields unchanged`)
      const key = `${first.state}:${second.direction || 'none'}:${second.outcomeLabel}`
      if (!samples.has(key) && samples.size < 12) samples.set(key, row)
    }
    for (const [name, row] of samples) {
      const app = vue.createSSRApp({ render: () => vue.h(component, { row }) })
      app.component('VChip', { setup: (_, { slots }) => () => vue.h('span', slots.default?.()) })
      app.config.warnHandler = warning => warnings.push(warning)
      const markup = await renderToString(app)
      check(markup.includes('实验对照'), `actual ${name}: independent comparison is visibly experimental`)
      check(markup.includes(primary(row).label), `actual ${name}: real handler signal is rendered`)
      if (primary(row).state !== 'selected') check(!markup.includes('data-comprehensive-alternative='), `actual ${name}: unselected handler row displays no recommended alternative`)
      check(markup.includes('查看新树依据'), `actual ${name}: actual rules remain inspectable`)
    }
    equal(warnings, [], 'actual new handler signals render without Vue warnings')
    actualHandlerSignals = { status: 'passed', ...counts, renderedStates: samples.size }
  }

  const report = { status: 'passed', checks, renderedStates: cases.length, actualHandlerSignals, warnings, modelChanged: false, servicesStarted: false, networkRequests: false }
  const output = path.join(root, 'reports/comprehensive-column-ui-review')
  fs.mkdirSync(output, { recursive: true })
  fs.writeFileSync(path.join(output, 'verification.json'), JSON.stringify(report, null, 2))
  fs.writeFileSync(path.join(output, 'rendered.json'), JSON.stringify(rendered, null, 2))
  console.log(JSON.stringify(report, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
