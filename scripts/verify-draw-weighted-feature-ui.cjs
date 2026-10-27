// Archived experiment parity only. This retired algorithm is not used by the page.
// No browser, services, network, or DB.
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const crypto = require('node:crypto')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const ts = adminRequire('typescript')
function load(relative, imports = {}) {
  const module = { exports: {} }
  const source = fs.readFileSync(path.join(root, relative), 'utf8')
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(key => key in imports ? imports[key] : adminRequire(key), module, module.exports)
  return module.exports
}
const frontend = load('admin/src/utils/drawFrontend.ts')
const archivedFeature = 'reports/draw-weighted-feature-study/archive/drawFeature.ts'
const archivedComponent = 'reports/draw-weighted-feature-study/archive/DrawFeatureSignal.vue'
const feature = load(archivedFeature, { './drawFrontend': frontend })
let checks = 0
function same(actual, expected, message) {
  if (Array.isArray(expected)) {
    assert.ok(Array.isArray(actual) && actual.length === expected.length, message)
    for (let i = 0; i < expected.length; i++) same(actual[i], expected[i], `${message}[${i}]`)
    return
  }
  if (typeof expected === 'number') assert.ok(typeof actual === 'number' && Number.isFinite(actual) && Math.abs(actual - expected) <= 1e-9, `${message}: ${actual} != ${expected}`)
  else assert.equal(actual, expected, message)
  checks++
}
const support = (values, key = 'sporttery') => feature.drawWeightedSource(key, values)
same(support([-.1, -.2, 1]).support, [1, 1, 0], 'negative pair')
same(support([.1, .2, 1]).support, [2 / 3, 2 / 3, 0], 'positive pair')
same(support([-.1, .2, 1]).support, [1, 1 / 3, 0], 'mixed pair')
same(support([0, .2, 1]).support, [1 / 3, 2 / 3, 0], 'zero plus positive')
same(support([-.2, 0, 1]).support, [1, 1 / 3, 0], 'zero plus negative')
same(support([.1, 1, -.2]).support, [1 / 3, 1 / 6, 1], 'omitted draw fallback')
same(support([1, .1, -.2]).support, [0, 1 / 3, 1], 'no omitted non-draw fallback')
same(support([.1, .2, -.2]).support, [1 / 2, 1 / 3, 1 / 2], 'boundary pair weights averaged')
same(support([.1, .2, -.2]).drawFallback, false, 'partially selected draw no fallback')
same(support([1, .9, 1.1], 'kelly').support, [1, 1, 0], 'Kelly small pair')
for (const value of [0, .2, -.2]) {
  same(support([value, value, value]).support, [0, 0, 0], 'flat differences neutral')
  same(support([value, value, value]).available, true, 'flat differences observed')
}
same(support([1, 1, 1], 'kelly').support, [0, 0, 0], 'flat Kelly neutral')
for (const values of [null, [null, 1, 2], [NaN, 1, 2], ['0', 1, 2], [false, 1, 2], [1, 2]]) same(support(values).available, false, 'invalid source unavailable')
same(support([0, 1, 2], 'kelly').available, false, 'zero Kelly invalid')
same(support([0, .6e-9, -.6e-9]).noDiscrimination, false, 'flat check uses full range')
same(feature.drawSupportVerdict([1, 2, 1]).state, 'candidate', 'strict draw first')
same(feature.drawSupportVerdict([2, 2, 1]).state, 'suspected', 'draw tie suspected')
same(feature.drawSupportVerdict([3, 2, 1]).state, 'not_selected', 'draw second not strict candidate')
same(feature.drawInTopTwo([3, 2, 1]), true, 'draw second broad flag')
same(feature.drawInTopTwo([3, 1, 2]), false, 'draw third no broad flag')
same(feature.drawInTopTwo([3, 1, 1]), true, 'second-place tie retained')
same(feature.drawInTopTwo([0, 0, 0]), false, 'all same no broad flag')
same(feature.drawInTopTwo(null), false, 'missing no broad flag')

const heat = (homeHeat, rates) => feature.drawWeightedHeatAdjustment({ homeHeat, officialProfitRates: rates })
same(heat(65, [null, null, null]).known, true, 'no hot does not need rates')
same(heat(65, [null, null, null]).applied, false, 'exact 65 not hot')
same(heat(35, [null, null, null]).applied, false, 'exact away 65 not hot')
same(heat(65.00001, [27.999, null, null]).bonus, [1, 0, 0], 'strict greater heat and lower rate')
same(heat(80, [28, null, null]).bonus, [0, 0, 0], 'rate exactly 28 not supported')
same(heat(20, [null, null, 0]).bonus, [0, 0, 1], 'recorded zero rate applies')
same(heat(20, [null, null, -5]).bonus, [0, 0, 1], 'negative rate applies')
same(heat(80, [null, 0, 0]).known, false, 'missing hot-side rate unknown')
same(heat(null, [0, 0, 0]).known, false, 'missing heat unknown')
same(heat(101, [0, 0, 0]).known, false, 'out of range heat unknown')
same(feature.drawWeightedHeatAdjustment().known, false, 'old API no strict inputs unknown')

const referencePath = 'reports/draw-weighted-feature-study/ui-reference.json'
const references = JSON.parse(fs.readFileSync(path.join(root, referencePath), 'utf8'))
let sourceGroups = 0, fullBase = 0, fullAdjusted = 0
for (const row of references) {
  const actual = feature.drawWeightedSignalFromInputs(row.inputs, { homeHeat: row.homeHeat, officialProfitRates: row.officialProfitRates })
  for (const phase of ['opening', 'latest']) {
    for (const source of actual.phases[phase].sources) {
      const expected = row.expected.phases[phase][source.key]
      const message = `${row.matchId}.${phase}.${source.key}`
      same(source.available, expected !== null, `${message}.available`)
      if (expected) {
        same(source.values, expected.values, `${message}.values`)
        same(source.support, expected.support, `${message}.support`)
        same(source.pairs.map(pair => pair.indices), expected.pairs, `${message}.pairs`)
        same(!source.noDiscrimination, expected.informative, `${message}.informative`)
      }
      sourceGroups++
    }
    const wanted = ['kelly', 'sporttery', 'average'].map(source => row.expected.phases[phase][source])
    const expectedMean = wanted.some(value => value === null) ? null : [0, 1, 2].map(index => wanted.reduce((sum, value) => sum + value.support[index], 0) / 3)
    same(actual.phases[phase].support, expectedMean, `${row.matchId}.${phase}.mean`)
  }
  same(actual.base, row.expected.base, `${row.matchId}.base`)
  same(actual.adjusted, row.expected.adjusted, `${row.matchId}.adjusted`)
  same(feature.drawSupportVerdict(actual.base).state, row.expected.baseState, `${row.matchId}.baseState`)
  same(feature.drawSupportVerdict(actual.adjusted).state, row.expected.adjustedState, `${row.matchId}.adjustedState`)
  const hotState = !actual.heat.known ? 'unknown' : actual.heat.direction === null ? 'no_hot' : actual.heat.applied ? 'supported' : 'not_supported'
  same(hotState, row.expected.hot.state, `${row.matchId}.hotState`)
  same(actual.heat.direction, row.expected.hot.direction, `${row.matchId}.hotDirection`)
  same(actual.heat.profitRate, row.expected.hot.rate, `${row.matchId}.hotRate`)
  for (const key of ['base', 'adjusted']) {
    const scores = row.expected[key]
    const expectedTopTwo = scores !== null && Math.max(...scores) - Math.min(...scores) > 1e-9 && scores[1] >= [...scores].sort((a, b) => b - a)[1] - 1e-9
    same(feature.drawInTopTwo(actual[key]), expectedTopTwo, `${row.matchId}.${key}.topTwo`)
  }
  if (actual.base) fullBase++
  if (actual.adjusted) fullAdjusted++
}

// Exercise the page adapter independently of the pure raw-input path.
const stages = ['opening', 'latest'].map(key => ({ key, kelly: { available: true, values: [.9, 1, 1.1] }, william: [2, 3, 4], sporttery: [2.1, 3.2, 3], average: [2.1, 3.2, 3] }))
const pageRow = { frontendAnalysis: { comparisonStages: stages, weightedFeatureInputs: { homeHeat: 80, officialProfitRates: [0, null, null] } } }
const pageSignal = feature.drawFeatureSignal(pageRow)
same(pageSignal.base, [10, 10, 0], 'page original quotes and 3:7 composition')
same(pageSignal.adjusted, [11, 10, 0], 'page hot adjustment goes to home, not draw')
const changedOutcome = { ...pageRow, homeScore: 9, awayScore: 0, settled: false, status: 1 }
assert.deepEqual(feature.drawFeatureSignal(changedOutcome), pageSignal); checks++
const legacyOnly = { frontendAnalysis: { ...pageRow.frontendAnalysis, weightedFeatureInputs: undefined, yapantouzhu: [80, 20], roiSimulation: { markets: [{ key: 'sporttery', bookmakerByOutcome: [{ outcome: 'home', officialProfitRate: 0 }] }] } } }
same(feature.drawFeatureSignal(legacyOnly).adjusted, null, 'legacy/default-zero display inputs never substituted')
const incomplete = structuredClone(pageRow)
incomplete.frontendAnalysis.comparisonStages[0].sporttery = null
same(feature.drawFeatureSignal(incomplete).base, null, 'missing opening price does not borrow latest')
same(feature.drawFeatureSignal(incomplete).phases.latest.support, [1, 1, 0], 'available phase preserved when other missing')

const sourcePaths = [archivedFeature, archivedComponent, 'admin/src/types/drawHistory.ts', referencePath, 'scripts/verify-draw-weighted-feature-ui.cjs']
const result = { status: 'passed', archivedExperiment: true, productionAlgorithm: false, checks, rows: references.length, sourceGroups, fullBase, fullAdjusted, network: false, servicesStarted: false,
  sourceHashes: Object.fromEntries(sourcePaths.map(relative => [relative, crypto.createHash('sha256').update(fs.readFileSync(path.join(root, relative))).digest('hex')])) }
fs.writeFileSync(path.join(root, 'reports/draw-weighted-feature-study/archive-ui-review.json'), `${JSON.stringify(result, null, 2)}\n`)
console.log(JSON.stringify(result, null, 2))
