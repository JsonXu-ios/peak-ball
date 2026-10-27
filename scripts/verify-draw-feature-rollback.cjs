// Independent, offline review of the original difference display restoration.
// Production files, model artifacts, services and data stores are never modified.
const fs = require('node:fs')
const path = require('node:path')
const crypto = require('node:crypto')
const assert = require('node:assert/strict')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const ts = adminRequire('typescript')
const sha = relative => crypto.createHash('sha256').update(fs.readFileSync(path.join(root, relative))).digest('hex')
const text = relative => fs.readFileSync(path.join(root, relative), 'utf8')
function load(relative, imports = {}) {
  const module = { exports: {} }
  const js = ts.transpileModule(text(relative), { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', js)(key => key in imports ? imports[key] : adminRequire(key), module, module.exports)
  return module.exports
}
const frontend = load('admin/src/utils/drawFrontend.ts')
const feature = load('admin/src/utils/drawFeature.ts', { './drawFrontend': frontend })
const unchangedBeforeRollback = {
  'admin/src/utils/drawFrozenSignal.ts': 'd68476cd87630c2ca69198963ead381c3e03f8092f63ef4c753d7976048d3358',
  'admin/src/views/statistics/DrawFrozenSignal.vue': '82540784e4b58cbe6b175ac146c9be010132adf5b26df11fb4a1a415fa555f14',
  'admin/src/views/statistics/DrawHistory.vue': '3c03cc8804b2010b8e5bb8fba821aa8120e56ec2fa190920a47ba68facbe6cb3',
  'admin/src/views/statistics/DrawScheduleDate.vue': '637b3309ee1e91c5603dc221dfbadaf15d22bdf46980aab13817a5dbf6e89bb7',
  'admin/src/utils/drawCalendar.ts': 'd044549d9a77de66006fc0ed916abae51debf2aba9d92e880213741c5088f735',
}
for (const [file, expected] of Object.entries(unchangedBeforeRollback)) assert.equal(sha(file), expected, `${file} changed outside the intended rollback`)
assert.equal(feature.DRAW_FEATURE_NOTE, '初盘看平项绝对值最大；即时依次看平项绝对值最大、负数含平、正数含平。包含取三项绝对值升序的最后两项，边界并列保留。')
assert.equal(feature.drawWeightedSignalFromInputs, undefined, 'weighted scorer still exported by production')
const component = text('admin/src/views/statistics/DrawFeatureSignal.vue')
assert.ok(!/三源加权|weighted-v2|热度修正后|综合前二|drawSupportVerdict|drawInTopTwo/.test(component))
assert.ok(component.includes('平项并列最大，差值均为0') && component.includes('负数含平') && component.includes('正数含平'))
const archivedVerifier = text('scripts/verify-draw-weighted-feature-ui.cjs')
assert.ok(archivedVerifier.includes("const archivedFeature = 'reports/draw-weighted-feature-study/archive/drawFeature.ts'"))
assert.ok(archivedVerifier.includes('const feature = load(archivedFeature'))
assert.ok(archivedVerifier.includes('archive-ui-review.json'))
assert.ok(text('reports/draw-weighted-feature-study/archive/drawFeature.ts').includes('drawWeightedSignalFromInputs'))
assert.ok(text('reports/draw-weighted-feature-study/archive/DrawFeatureSignal.vue').includes('三源加权支持分'))

let classifications = 0, targetMutations = 0
function reference(values, phase) {
  if (!Array.isArray(values) || values.length !== 3 || values.some(v => typeof v !== 'number' || !Number.isFinite(v))) {
    return { kind: 'missing', differences: null, drawDifference: null, largest: [], topTwo: [], largestTied: false, topTwoTied: false }
  }
  const absolute = values.map(v => v < 0 ? -v : v)
  const maximum = Math.max(absolute[0], absolute[1], absolute[2])
  // Median magnitude is the cutoff for the largest two, never the smallest two.
  const median = absolute[0] + absolute[1] + absolute[2] - maximum - Math.min(...absolute)
  const largest = [0, 1, 2].filter(i => absolute[i] + 1e-9 >= maximum)
  const topTwo = [0, 1, 2].filter(i => absolute[i] + 1e-9 >= median)
  const kind = largest.includes(1) ? 'largest' : phase === 'latest' && topTwo.includes(1) && values[1] < -1e-9 ? 'negative' : phase === 'latest' && topTwo.includes(1) && values[1] > 1e-9 ? 'positive' : 'none'
  return { kind, differences: values, drawDifference: values[1], largest, topTwo, largestTied: largest.length > 1, topTwoTied: topTwo.length > 2 }
}
function check(values, phase) {
  assert.deepEqual(feature.classifyDrawDifferences(values, phase), reference(values, phase))
  classifications++
}
for (const phase of ['opening', 'latest']) {
  for (let a = -3; a <= 3; a++) for (let b = -3; b <= 3; b++) for (let c = -3; c <= 3; c++) check([a, b, c], phase)
  for (const values of [null, [1, null, 2], [1, NaN, 2], [1, Infinity, 2], [1, '2', 3], [1, 2], [3, -2, 2], [0, 0, 0], [-1, .1, 2], [3, -2, -1], [3, 2, -1]]) check(values, phase)
}
assert.equal(feature.classifyDrawDifferences([3, -2, -1], 'opening').kind, 'none')
assert.equal(feature.classifyDrawDifferences([3, -2, -1], 'latest').kind, 'negative')
assert.equal(feature.classifyDrawDifferences([3, 2, -1], 'latest').kind, 'positive')
assert.equal(feature.classifyDrawDifferences([3, -3, 1], 'latest').kind, 'largest')
assert.deepEqual(feature.classifyDrawDifferences([3, -2, 2], 'latest').topTwo, [0, 1, 2])

const rows = JSON.parse(text('reports/draw-full-feature-study/dataset.json')).rows
const prices = v => Array.isArray(v) && v.length === 3 && v.every(x => typeof x === 'number' && Number.isFinite(x) && x > 0)
for (const source of rows) {
  const row = { ...source, frontendAnalysis: source.frontend }
  const actual = feature.drawFeatureSignal(row)
  for (const phase of ['opening', 'latest']) for (const company of ['sporttery', 'average']) {
    const w = source[phase]?.william, c = source[phase]?.[company]
    const values = prices(w) && prices(c) ? w.map((v, i) => v - c[i]) : null
    const got = actual[phase].find(value => value.source === company)
    const expected = reference(values, phase)
    for (const key of Object.keys(expected)) assert.deepEqual(got[key], expected[key], `${source.matchId}.${phase}.${company}.${key}`)
    classifications++
  }
  assert.deepEqual(feature.drawFeatureSignal({ ...row, result: (row.result + 1) % 3, homeScore: 99, awayScore: 98, status: -999, settled: false }), actual)
  targetMutations++
}
const missingOpening = { frontendAnalysis: { comparisonStages: [{ key: 'opening', william: null, sporttery: null, average: null }, { key: 'latest', william: [2, 3, 4], sporttery: [1.9, 3.2, 4.1], average: [2.1, 2.8, 4.1] }] } }
assert.ok(feature.drawFeatureSignal(missingOpening).opening.every(source => source.kind === 'missing'), 'opening borrowed latest prices')
assert.ok(feature.drawFeatureSignal(missingOpening).latest.every(source => source.kind !== 'missing'))
const files = ['admin/src/utils/drawFeature.ts', 'admin/src/views/statistics/DrawFeatureSignal.vue', ...Object.keys(unchangedBeforeRollback), 'reports/draw-weighted-feature-study/archive/drawFeature.ts', 'reports/draw-weighted-feature-study/archive/DrawFeatureSignal.vue', 'scripts/verify-draw-weighted-feature-ui.cjs', 'scripts/verify-draw-feature-rollback.cjs']
const result = { status: 'passed', classifications, datasetRows: rows.length, targetMutations, unchangedProductionFiles: Object.keys(unchangedBeforeRollback).length, weightedRuntimeRemoved: true, archivedExperimentRetained: true, archivedVerifierIsolated: true, issues: [], sourceHashes: Object.fromEntries(files.map(file => [file, sha(file)])) }
const folder = path.join(root, 'reports/draw-feature-rollback')
fs.mkdirSync(folder, { recursive: true })
fs.writeFileSync(path.join(folder, 'review.json'), JSON.stringify(result, null, 2) + '\n')
console.log(JSON.stringify(result, null, 2))
