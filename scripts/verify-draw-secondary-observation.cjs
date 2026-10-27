// Independent, offline review of secondary-v1. No service, browser, database,
// model training, cache, or production file is started or modified.
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
const folder = path.join(root, 'reports/draw-secondary-observation-review')
const read = relative => fs.readFileSync(path.join(root, relative), 'utf8')
const shaFile = absolute => crypto.createHash('sha256').update(fs.readFileSync(absolute)).digest('hex')
const sha = relative => shaFile(path.join(root, relative))
let checks = 0
function equal(actual, expected, message) { assert.deepEqual(actual, expected, message); checks++ }
function check(actual, message) { assert.ok(actual, message); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const secondary = load(read('admin/src/utils/drawSecondarySignal.ts'))
const frozen = load(read('admin/src/utils/drawFrozenSignal.ts'))
const unchangedSources = {
  'admin/src/utils/drawFrozenSignal.ts': 'd68476cd87630c2ca69198963ead381c3e03f8092f63ef4c753d7976048d3358',
  'admin/src/views/statistics/DrawFrozenSignal.vue': '82540784e4b58cbe6b175ac146c9be010132adf5b26df11fb4a1a415fa555f14',
  'admin/src/views/statistics/DrawScheduleDate.vue': '637b3309ee1e91c5603dc221dfbadaf15d22bdf46980aab13817a5dbf6e89bb7',
  'admin/src/utils/drawCalendar.ts': 'd044549d9a77de66006fc0ed916abae51debf2aba9d92e880213741c5088f735',
  'go_server/internal/drawmodeldata/observation_v1.json': 'e7cb7d50668a76e916d2f86a6ab598a6bcb23ab5e6c961dbd972875346afd987',
}

function row(score, state = 'selected', extra = {}) {
  return { frozenDrawSignal: { state, modelVersion: 'sporttery-draw-observation-v1', score }, settled: false, regulationSettled: false, homeScore: 0, awayScore: 0, ...extra }
}
function summary(rows, select, totalDraws) {
  const selected = rows.filter(select)
  const draws = selected.filter(item => item.result === 1).length
  return { matches: selected.length, draws, nonDraws: selected.length - draws, precision: selected.length ? draws / selected.length : null, coverage: totalDraws ? draws / totalDraws : null }
}
function auditCohort(rows, lookup, expected) {
  const cohort = rows.filter(item => typeof item.jingcaiId === 'string' && item.jingcaiId.trim())
  const drawCount = cohort.filter(item => item.result === 1).length
  let selectedStateCount = 0
  const observations = cohort.map(source => {
    const result = lookup(String(source.matchId))
    check(result && Number.isFinite(result.score), `missing real score for ${source.matchId}`)
    const observation = row(result.score, result.selected ? 'selected' : 'not_selected', {
      matchId: source.matchId, result: source.result, settled: true, regulationSettled: true,
      homeScore: source.homeScore, awayScore: source.awayScore,
    })
    const prior = frozen.frozenDrawPresentation(observation)
    const before = JSON.stringify(observation)
    const classification = secondary.classifySecondaryDraw(observation)
    secondary.secondaryDrawPresentation(observation)
    equal(JSON.stringify(observation), before, 'secondary helper cannot mutate original row')
    equal(frozen.frozenDrawPresentation(observation), prior, 'original fixed-model presentation is preserved')
    const alternateResult = { ...observation, result: (source.result + 1) % 3, homeScore: 99, awayScore: 98, status: -999, settled: false, regulationSettled: false }
    equal(secondary.classifySecondaryDraw(alternateResult), classification, 'actual target result cannot select or reject a row')
    if (result.selected) selectedStateCount++
    return { ...source, modelSelected: result.selected, decision: classification.decision }
  })
  const baseline = summary(observations, item => item.modelSelected, drawCount)
  const retained = summary(observations, item => item.decision === 'retained', drawCount)
  const rejected = summary(observations, item => item.decision === 'rejected', drawCount)
  equal(baseline.matches, retained.matches + rejected.matches, 'all valid selected rows partition into retained/rejected')
  equal(baseline.draws, retained.draws + rejected.draws, 'selected draw accounting reconciles')
  equal([cohort.length, drawCount, baseline.matches, baseline.draws, retained.matches, retained.draws], expected, 'real historical cohort totals reconcile')
  return { cohortMatches: cohort.length, cohortDraws: drawCount, baseline, retained, rejected, targetMutations: cohort.length, originalSelectedStatesPreserved: selectedStateCount }
}

async function main() {
  equal(secondary.SECONDARY_DRAW_VERSION, 'secondary-v1', 'version frozen explicitly')
  equal(secondary.SECONDARY_DRAW_THRESHOLD, .225, 'threshold frozen explicitly')
  for (const [file, expected] of Object.entries(unchangedSources)) equal(sha(file), expected, `${file} must remain unchanged`)
  for (const [score, expected] of [[.2016984853563883, 'rejected'], [.22499999999999998, 'rejected'], [.225, 'retained'], [.22500000000000003, 'retained'], [.25, 'retained'], [0, 'rejected']]) {
    equal(secondary.classifySecondaryDraw(row(score)).decision, expected, `strict numeric boundary at ${score}`)
  }
  for (const invalid of [undefined, null, NaN, Infinity, -Infinity, '0.225']) equal(secondary.classifySecondaryDraw(row(invalid)).decision, 'unavailable', 'missing/non-numeric/non-finite score is not a rejection')
  for (const state of ['unavailable', 'unknown', 'SELECTED']) equal(secondary.classifySecondaryDraw(row(.3, state)).decision, 'unavailable', 'unknown states cannot enter selection')
  equal(secondary.classifySecondaryDraw({ frozenDrawSignal: { score: .3 } }).decision, 'unavailable', 'missing state is unavailable')
  equal(secondary.classifySecondaryDraw({}).decision, 'unavailable', 'old API result cannot be silently rejected')
  equal(secondary.classifySecondaryDraw(row(.9, 'not_selected')).decision, 'not_selected', 'secondary never promotes a non-selected original row')
  equal(secondary.classifySecondaryDraw(row(.9, 'out_of_scope')).decision, 'out_of_scope', 'non-lottery rows stay out of scope')

  const resultCases = [
    ['pending', { settled: false, regulationSettled: false, homeScore: 0, awayScore: 0 }, '二次入选 · 待赛果', '待赛果核对'],
    ['unconfirmed regulation', { settled: true, regulationSettled: undefined, homeScore: 1, awayScore: 1 }, '二次入选 · 待赛果', '待赛果核对'],
    ['missing score', { settled: true, regulationSettled: true, homeScore: NaN, awayScore: 1 }, '二次入选 · 待赛果', '待赛果核对'],
    ['confirmed draw', { settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 }, '命中平局', '误剔平局'],
    ['confirmed non-draw', { settled: true, regulationSettled: true, homeScore: 2, awayScore: 0 }, '二次入选 · 未打平', '剔除正确'],
  ]
  for (const [name, result, retainedLabel, rejectionOutcome] of resultCases) {
    equal(secondary.secondaryDrawPresentation(row(.23, 'selected', result)).label, retainedLabel, name)
    equal(secondary.secondaryDrawPresentation(row(.22, 'selected', result)).outcomeLabel, rejectionOutcome, name)
    equal(secondary.classifySecondaryDraw(row(.23, 'selected', result)), secondary.classifySecondaryDraw(row(.23)), 'settlement cannot alter retained decision')
    equal(secondary.classifySecondaryDraw(row(.22, 'selected', result)), secondary.classifySecondaryDraw(row(.22)), 'settlement cannot alter rejected decision')
  }
  const frozenRow = Object.freeze({ ...row(.22), frozenDrawSignal: Object.freeze(row(.22).frozenDrawSignal) })
  equal(secondary.classifySecondaryDraw(frozenRow).decision, 'rejected', 'works on immutable source objects')
  const noOutcomeAccess = new Proxy(row(.23), {
    get(target, key) {
      if (key !== 'frozenDrawSignal') throw new Error(`Selection accessed non-model field ${String(key)}`)
      return target[key]
    },
  })
  equal(secondary.classifySecondaryDraw(noOutcomeAccess).decision, 'retained', 'classification reads only original model output, never target results or current-page odds')

  const originalRows = JSON.parse(read('reports/draw-full-feature-study/dataset.json')).rows
  const scoreSource = JSON.parse(read('reports/draw-observation-model/frozen-scores.json'))
  const originalScores = new Map(scoreSource.scores.map(item => [String(item.matchId), item]))
  equal(originalRows.length, 4161, 'use full original settled history')
  const original = auditCohort(originalRows, id => originalScores.get(id), [511, 124, 320, 122, 285, 118])
  const currentPath = path.join(os.tmpdir(), 'peak-ball-match-202610257025-readonly.json')
  const currentScoresPath = path.join(os.tmpdir(), 'peak-ball-smallest-intersection-independent-audit.json')
  let current = { status: 'skipped', reason: 'Optional read-only TEMP snapshot or scores are absent; no substitute or network request was made.' }
  if (fs.existsSync(currentPath) && fs.existsSync(currentScoresPath)) {
    const currentRows = JSON.parse(fs.readFileSync(currentPath, 'utf8').replace(/^\uFEFF/, '')).rows
    const currentScores = JSON.parse(fs.readFileSync(currentScoresPath, 'utf8').replace(/^\uFEFF/, '')).newScores
    const originalThreshold = .2016984853563883
    current = { status: 'passed', ...auditCohort(currentRows, id => ({ score: currentScores[id], selected: currentScores[id] >= originalThreshold }), [525, 127, 326, 124, 291, 120]), sourceHashes: { snapshot: shaFile(currentPath), scores: shaFile(currentScoresPath) } }
  }

  const componentPath = 'admin/src/views/statistics/DrawSecondarySignal.vue'
  const componentSource = read(componentPath)
  const { descriptor } = parse(componentSource)
  const script = compileScript(descriptor, { id: 'secondary-observation-offline', inlineTemplate: true })
  const component = load(script.content, { '@/utils/drawSecondarySignal': secondary }).default
  const fixtures = [
    ['保留／待赛', row(.225)],
    ['保留／命中', row(.3, 'selected', { settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 })],
    ['保留／未平', row(.3, 'selected', { settled: true, regulationSettled: true, homeScore: 2, awayScore: 0 })],
    ['剔除／待赛', row(.22)],
    ['剔除／正确', row(.22, 'selected', { settled: true, regulationSettled: true, homeScore: 2, awayScore: 0 })],
    ['剔除／误删', row(.22, 'selected', { settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 })],
    ['原模型未入选', row(.3, 'not_selected')], ['非竞彩', row(.3, 'out_of_scope')],
    ['缺少分数', row(undefined)], ['尚无原模型', {}],
  ]
  const rendered = []
  const warnings = []
  for (const [name, input] of fixtures) {
    const app = vue.createSSRApp({ render: () => vue.h(component, { row: input }) })
    app.config.warnHandler = message => warnings.push(message)
    app.component('VChip', { props: ['color', 'size', 'variant'], setup: (props, { slots }) => () => vue.h('span', { class: ['chip', props.color] }, slots.default?.()) })
    const html = await renderToString(app)
    const presentation = secondary.secondaryDrawPresentation(input)
    check(html.includes(presentation.label), `${name} primary marker is visible in actual Vue rendering`)
    check(html.includes(`data-secondary-state="${presentation.decision}"`), `${name} state exposed`) 
    check(html.includes('分数不是平局概率'), 'tooltip explicitly disambiguates model score')
    check(!/模型分数[^<]*%/.test(html), 'model score must not be labelled a percentage')
    if (presentation.outcomeLabel) check(html.includes(presentation.outcomeLabel), `${name} outcome audit is visible`)
    if (Number.isFinite(presentation.score)) check(html.includes(presentation.score.toFixed(4)), `${name} numeric score is visible`)
    rendered.push(`<section><h2>${name}</h2>${html}</section>`)
  }
  equal(warnings, [], 'actual component renders without Vue warnings')
  const page = read('admin/src/views/statistics/DrawHistory.vue')
  check(page.includes('DrawSecondarySignal') && page.includes('二次筛选'), 'observation table includes independent secondary column')
  check(page.includes('<DrawFrozenSignal'), 'original selection column still exists')

  fs.mkdirSync(folder, { recursive: true })
  fs.writeFileSync(path.join(folder, 'states.html'), `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>二次筛选离线状态核对</title><style>body{background:#151515;color:#eee;font:14px system-ui;margin:24px;display:grid;grid-template-columns:repeat(3,minmax(240px,1fr));gap:16px}section{border:1px solid #555;padding:16px;border-radius:6px}h2{font-size:15px}.chip{display:inline-block;border-radius:18px;padding:5px 12px;background:#333}.success,.text-success{color:#75d29a}.warning{color:#ffc65c}.error,.text-error{color:#ff7b7b}.primary{color:#6eb2ff}.text-caption{font-size:12px}.mt-1{margin-top:6px}.text-medium-emphasis{color:#aaa}</style>${rendered.join('\n')}</html>`, 'utf8')
  const sourceFiles = ['admin/src/utils/drawSecondarySignal.ts', componentPath, 'admin/src/views/statistics/DrawHistory.vue', 'scripts/verify-draw-secondary-observation.cjs', 'reports/draw-full-feature-study/dataset.json', 'reports/draw-observation-model/frozen-scores.json', ...Object.keys(unchangedSources)]
  const result = { status: 'passed', version: secondary.SECONDARY_DRAW_VERSION, threshold: secondary.SECONDARY_DRAW_THRESHOLD, checks, original, current, actualVueRenderedStates: fixtures.length, vueWarnings: warnings, originalSourcesUnchanged: Object.keys(unchangedSources), servicesStarted: false, networkRequests: false, modelTraining: false, sourceHashes: Object.fromEntries(sourceFiles.map(file => [file, sha(file)])), limitations: ['Historical metrics are fitted-history observations, not prospective validation.', 'Current 525-match snapshot is optional and read from existing TEMP files only.', 'SSR uses a simple chip stub; it validates actual template content and conditional visibility, not full Vuetify browser layout.'] }
  fs.writeFileSync(path.join(folder, 'review.json'), JSON.stringify(result, null, 2) + '\n')
  console.log(JSON.stringify(result, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
