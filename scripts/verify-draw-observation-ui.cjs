// Offline checks for calendar requests and result-independent selection display.
// No browser, dev server, database, or network is used.
const fs = require('node:fs')
const path = require('node:path')
const assert = require('node:assert/strict')
const { createRequire } = require('node:module')
const root = path.resolve(__dirname, '..')
const adminRequire = createRequire(path.join(root, 'admin/package.json'))
const ts = adminRequire('typescript')
const vue = adminRequire('vue')
const { parse, compileScript } = adminRequire('@vue/compiler-sfc')

function loadTypescript(source, imports = {}) {
  const module = { exports: {} }
  const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', compiled)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
function loadFile(relative, imports) { return loadTypescript(fs.readFileSync(path.join(root, relative), 'utf8'), imports) }
const calendar = loadFile('admin/src/utils/drawCalendar.ts')
const signal = loadFile('admin/src/utils/drawFrozenSignal.ts')
const dates = loadFile('admin/src/utils/date.ts')

async function main() {
  let checks = 0
  function check(condition, message) { assert.ok(condition, message); checks++ }
  for (const timezone of ['Etc/UTC', 'Asia/Shanghai', 'America/Los_Angeles']) {
    process.env.TZ = timezone
    for (const day of ['2026-10-26', '2026-01-01', '2026-12-31', '2024-02-29']) {
      check(calendar.scheduleCalendarDate(calendar.parseScheduleCalendarDate(day)) === day, `calendar round-trip ${timezone} ${day}`)
    }
    for (const day of ['2026-02-29', '2026-13-01', '2026-10-32', '2026-1-01', '']) check(calendar.parseScheduleCalendarDate(day) === undefined, `reject invalid ${day}`)
  }
  check(calendar.scheduleCalendarDate(new Date('invalid')) === undefined, 'invalid picker date rejected')
  check(calendar.scheduleCalendarDate('2026-10-26') === undefined, 'picker accepts actual calendar date objects only')

  const base = { settled: true, regulationSettled: true, homeScore: 1, awayScore: 1 }
  const model = state => ({ modelVersion: 'fixed-v1', state })
  for (const score of [[0, 0], [2, 2], [3, 0]]) {
    check(signal.frozenDrawPresentation({ ...base, homeScore: score[0], awayScore: score[1], frozenDrawSignal: model('not_selected') }).label === '未入选', 'outcome never promotes not-selected')
  }
  check(signal.frozenDrawPresentation({ ...base, frozenDrawSignal: model('selected') }).label === '命中平局', 'confirmed selected draw hits')
  check(signal.frozenDrawPresentation({ ...base, awayScore: 0, frozenDrawSignal: model('selected') }).label === '入选 · 未打平', 'selected non-draw explicitly misses')
  check(signal.frozenDrawPresentation({ ...base, homeScore: 0, awayScore: 0, settled: false, frozenDrawSignal: model('selected') }).label === '入选 · 待赛果', 'unsettled 0:0 never hits')
  check(signal.frozenDrawPresentation({ ...base, regulationSettled: undefined, frozenDrawSignal: model('selected') }).label === '入选 · 待赛果', 'unconfirmed regulation result never hits')
  check(signal.frozenDrawPresentation({ ...base, frozenDrawSignal: model('unavailable') }).label === '数据不足', 'missing model data not negative')
  check(signal.frozenDrawPresentation({ ...base, frozenDrawSignal: model('out_of_scope') }).label === '非竞彩', 'scope preserved even for draw')
  check(signal.frozenDrawPresentation({ ...base }).label === '暂无模型数据', 'old service reports missing model data')
  check(signal.frozenDrawPresentation({ ...base, frozenDrawSignal: model('invalid') }).label === '暂无模型数据', 'unknown state cannot produce a selection')

  const pending = []
  const { descriptor } = parse(fs.readFileSync(path.join(root, 'admin/src/views/statistics/DrawScheduleDate.vue'), 'utf8'))
  const script = compileScript(descriptor, { id: 'draw-schedule-date-offline-test' })
  const component = loadTypescript(script.content, {
    vue: { ...vue, onBeforeUnmount: () => {} },
    'vuetify/locale': { zhHans: {} },
    '@/api/drawHistory': { getDrawObservationDates: params => new Promise((resolve, reject) => pending.push({ params, resolve, reject })) },
    '@/utils/date': dates,
    '@/utils/drawCalendar': calendar,
  }).default
  const props = vue.reactive({ modelValue: '2026-10-26', mode: 'today', scope: 'all', revision: 0 })
  const events = []
  const effects = vue.effectScope()
  const vm = effects.run(() => component.setup(props, { expose: () => {}, emit: (...event) => { events.push(event); if (event[0] === 'update:modelValue') props.modelValue = event[1] } }))
  const tick = async () => { await vue.nextTick(); await Promise.resolve(); await vue.nextTick() }
  const response = (request, items) => ({ data: { ...request.params, scheduleTimezone: 'Asia/Shanghai', dates: items } })

  vm.setOpen(true)
  await tick()
  check(pending.length === 1, 'opening calendar triggers one month request')
  assert.deepEqual(pending[0].params, { month: '2026-10', mode: 'today', scope: 'all' }); checks++
  pending[0].resolve(response(pending[0], [{ date: '2026-10-26', count: 12, matchCount: 12, drawCount: 2 }]))
  await tick()
  check(vm.counts.value['2026-10-26'] === 12, 'month data populates day marker')
  check(vm.dayTitle('2026-10-26').includes('12 场比赛'), 'marker title exposes match count')
  vm.calendarMonth.value = 10
  await tick()
  check(pending.length === 2 && pending[1].params.month === '2026-11', 'month navigation makes one request, never one per day')
  check(Object.keys(vm.counts.value).length === 0, 'new month does not display old markers')
  props.scope = 'sporttery'
  props.mode = 'history'
  await tick()
  check(pending.length === 3 && pending[2].params.scope === 'sporttery' && pending[2].params.mode === 'history', 'scope/tab changes batch into one request')
  pending[2].resolve(response(pending[2], [{ date: '2026-11-04', count: 3, matchCount: 10, drawCount: 3 }]))
  await tick()
  pending[1].resolve(response(pending[1], [{ date: '2026-11-05', count: 99, matchCount: 99, drawCount: 1 }]))
  await tick()
  check(vm.counts.value['2026-11-04'] === 3 && vm.counts.value['2026-11-05'] === undefined, 'late previous scope response cannot replace markers')
  check(vm.dayTitle('2026-11-04').includes('3 场已完赛平局'), 'history marker uses draw count wording')

  vm.choose(calendar.parseScheduleCalendarDate('2026-11-04'))
  await tick()
  assert.deepEqual(events, [['update:modelValue', '2026-11-04'], ['change']]); checks++
  check(vm.open.value === false, 'choosing date closes calendar')
  check(pending.length === 3, 'choosing date does not query individual day markers')
  vm.setOpen(true)
  await tick()
  pending[3].reject(new Error('test month marker error'))
  await tick()
  check(vm.error.value === 'test month marker error' && vm.loading.value === false, 'marker error is local and releases loading')
  check(events.length === 2 && props.modelValue === '2026-11-04', 'marker failure never changes selected date or queries parent')
  vm.loadDates()
  pending[4].resolve({ data: { ...response(pending[4], []).data, scheduleTimezone: 'UTC' } })
  await tick()
  check(vm.error.value.includes('尚未更新'), 'wrong timezone metadata cannot draw false markers')
  vm.loadDates()
  vm.setOpen(false)
  await tick()
  pending[5].resolve(response(pending[5], [{ date: '2026-11-04', count: 1, matchCount: 1, drawCount: 1 }]))
  await tick()
  check(Object.keys(vm.counts.value).length === 0, 'response after close is ignored')
  effects.stop()
  console.log(JSON.stringify({ status: 'passed', checks, calendarRequests: pending.length, network: false, servicesStarted: false }, null, 2))
}
main().catch(error => { console.error(error); process.exitCode = 1 })
