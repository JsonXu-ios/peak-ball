// Offline checks for draw-list filters and team logos; starts no services.
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
const equal = (actual, expected, reason) => { assert.deepEqual(actual, expected, reason); checks++ }
const check = (value, reason) => { assert.ok(value, reason); checks++ }
const throws = (fn, reason) => { assert.throws(fn, Error, reason); checks++ }
function load(source, imports = {}) {
  const module = { exports: {} }
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText
  new Function('require', 'module', 'exports', code)(name => name in imports ? imports[name] : adminRequire(name), module, module.exports)
  return module.exports
}
const filters = load(read('admin/src/utils/drawObservationFilters.ts'))
const logos = load(read('admin/src/utils/teamLogo.ts'))
const api = load(read('admin/src/api/drawHistory.ts'), { './request': {} })
const fixture = extra => ({
  matchId: 'fixture', jingcaiId: '', settled: true,
  frozenDrawSignal: { state: 'selected' }, nonSportterySignal: { state: 'selected' }, ...extra,
})

// A minimal memory host exercises actual Vue reactive/error behavior without a DOM.
function memoryRenderer() {
  const node = (kind, text = '') => ({ kind, text, children: [], props: {}, parent: null })
  return vue.createRenderer({
    createElement: type => node(type), createText: text => node('#text', text), createComment: text => node('#comment', text),
    setText: (target, text) => { target.text = text },
    setElementText: (target, text) => { target.text = text; target.children = [] },
    patchProp: (target, key, previous, next) => { target.props[key] = next },
    parentNode: target => target.parent,
    nextSibling: target => target.parent?.children[target.parent.children.indexOf(target) + 1] || null,
    insert: (target, parent, anchor) => {
      if (target.parent) target.parent.children.splice(target.parent.children.indexOf(target), 1)
      const index = anchor ? parent.children.indexOf(anchor) : -1
      if (index < 0) parent.children.push(target); else parent.children.splice(index, 0, target)
      target.parent = parent
    },
    remove: target => { target.parent?.children.splice(target.parent.children.indexOf(target), 1); target.parent = null },
  })
}
const find = (node, predicate) => predicate(node) ? node : node.children.map(child => find(child, predicate)).find(Boolean)

async function main() {
  for (const jingcaiId of ['', '   ', '20261104001']) {
    for (const state of ['selected', 'not_selected', 'unavailable', undefined]) {
      for (const settled of [true, false]) {
        const row = fixture({ jingcaiId, settled, frozenDrawSignal: state ? { state } : undefined, nonSportterySignal: state ? { state } : undefined })
        const jc = !!jingcaiId.trim()
        for (const selection of ['all', 'sporttery', 'non_sporttery']) {
          for (const match_status of ['all', 'finished', 'unfinished']) {
            const expected = (match_status === 'all' || (match_status === 'finished' ? settled : !settled)) &&
              (selection === 'all' || (state === 'selected' && (selection === 'sporttery' ? jc : !jc)))
            equal(filters.matchesDrawObservationFilters(row, { match_status, selection }), expected, 'scope, selection and settlement combine independently')
          }
        }
      }
    }
  }
  equal(filters.matchesDrawObservationFilters(fixture({ jingcaiId: '20261104001', frozenDrawSignal: { state: 'selected' }, nonSportterySignal: undefined }), { selection: 'sporttery' }), true, 'JC uses its own primary marker')
  equal(filters.matchesDrawObservationFilters(fixture({ frozenDrawSignal: { state: 'out_of_scope' }, nonSportterySignal: { state: 'selected' } }), { selection: 'non_sporttery' }), true, 'non-JC does not inherit JC out-of-scope state')
  equal(filters.matchesDrawObservationFilters(fixture({ settled: 'true' }), { match_status: 'finished' }), false, 'untrusted truthy settlement is not a finished match')
  for (const settled of [undefined, null, 0, 'false', true]) {
    equal(filters.matchesDrawObservationFilters(fixture({ settled }), { match_status: 'unfinished' }), false, 'unfinished requires an explicit false settlement')
  }
  const sourceStates = ['未开始', '进行中', '上半场', '中场', '下半场', '加时', '点球', '推迟', '延期', '取消', '腰斩', '中断', '暂停', '待定', '技术延迟开球']
  for (const sourceState of sourceStates) {
    for (const jingcaiId of ['', '20261104001']) {
      for (const state of ['selected', 'not_selected', undefined]) {
        const row = fixture({
          displayState: ` ${sourceState} `, settled: false, jingcaiId,
          frozenDrawSignal: state ? { state } : undefined,
          nonSportterySignal: state ? { state } : undefined,
        })
        for (const selection of ['all', 'sporttery', 'non_sporttery']) {
          const query = { match_status: `state:${sourceState}`, selection }
          const expected = selection === 'all' || (state === 'selected' && (selection === 'sporttery' ? !!jingcaiId : !jingcaiId))
          equal(filters.matchesDrawObservationFilters(row, query), expected, `${sourceState}: real source phase combines with independent selection`)
          equal(filters.matchesDrawObservationFilters({ ...row, displayState: '其他状态' }, query), false, `${sourceState}: another phase cannot pass`)
        }
      }
    }
  }
  equal(filters.matchesDrawObservationFilters(fixture({ settled: false, displayState: '上半场' }), { match_status: 'state: 上半场 ' }), true, 'source state trims surrounding whitespace consistently')
  equal(filters.matchesDrawObservationFilters(fixture({ settled: false, displayState: '上半场补时' }), { match_status: 'state:上半场' }), false, 'phase names use exact matching, not substring matching')
  equal(filters.matchesDrawObservationFilters(fixture({ settled: false, status: 0, displayState: '' }), { match_status: 'state:未开始' }), false, 'numeric source status does not invent a missing phase label')
  for (const displayState of ['取消', '延期', '推迟']) {
    equal(filters.matchesDrawObservationFilters(fixture({ settled: false, status: 0, displayState }), { match_status: 'state:未开始' }), false, 'cancelled/postponed fixtures are not scheduled fixtures')
  }
  for (const displayState of ['', ' \t ']) {
    equal(filters.matchesDrawObservationFilters(fixture({ settled: false, displayState }), { match_status: 'unknown' }), true, 'empty uncompleted source state is explicitly unknown')
    equal(filters.matchesDrawObservationFilters(fixture({ settled: true, displayState }), { match_status: 'unknown' }), false, 'completed fixtures are not reclassified as unknown')
  }
  for (const settled of [undefined, null, 0, 'false']) {
    equal(filters.matchesDrawObservationFilters(fixture({ settled, displayState: '' }), { match_status: 'unknown' }), false, 'unknown requires explicit false settlement')
  }
  equal(filters.matchesDrawObservationFilters(fixture({ settled: false, displayState: '待定' }), { match_status: 'unknown' }), false, 'pending is a real source state, not an empty unknown state')
  for (const selection of ['all', 'sporttery', 'non_sporttery']) {
    for (const jingcaiId of ['', '20261104001']) {
      const expected = selection === 'all' || (selection === 'sporttery' ? !!jingcaiId : !jingcaiId)
      equal(filters.matchesDrawObservationFilters(fixture({ settled: false, displayState: '', jingcaiId }), { match_status: 'unknown', selection }), expected, 'unknown combines with JC and non-JC without changing their signals')
    }
  }
  const before = fixture({ jingcaiId: '20261104001' })
  const serialized = JSON.stringify(before)
  filters.validateDrawObservationFilters({ matchStatus: 'finished', selection: 'sporttery', items: [before] }, { match_status: 'finished', selection: 'sporttery' })
  equal(JSON.stringify(before), serialized, 'validation never changes model or settlement data')
  for (const query of [{ match_status: 'finished' }, { match_status: 'unfinished' }, { match_status: 'unknown' }, { match_status: 'state:上半场' }, { selection: 'sporttery' }, { selection: 'non_sporttery' }]) {
    throws(() => filters.validateDrawObservationFilters({ items: [] }, query), 'an empty legacy response cannot silently ignore an active filter')
  }
  equal(filters.validateDrawObservationFilters({ items: [] }, {}), undefined, 'default all accepts the legacy response contract')
  equal(filters.validateDrawObservationFilters({ items: [] }, { match_status: 'all', selection: 'all' }), undefined, 'explicit all remains backward compatible')
  throws(() => filters.validateDrawObservationFilters({ matchStatus: 'finished', items: [fixture({ settled: false })] }, { match_status: 'finished' }), 'server echo alone cannot pass an unfinished row')
  throws(() => filters.validateDrawObservationFilters({ matchStatus: 'unfinished', items: [fixture({ settled: true })] }, { match_status: 'unfinished' }), 'server echo alone cannot pass a finished row')
  equal(filters.validateDrawObservationFilters({ matchStatus: 'unfinished', selection: 'non_sporttery', items: [fixture({ settled: false })] }, { match_status: 'unfinished', selection: 'non_sporttery' }), undefined, 'unfinished combines with independent non-JC selection')
  equal(filters.validateDrawObservationFilters({ matchStatus: 'state:上半场', items: [fixture({ settled: false, displayState: '上半场' })] }, { match_status: 'state:上半场' }), undefined, 'new source state is accepted only with matching server echo and row')
  throws(() => filters.validateDrawObservationFilters({ matchStatus: 'state:上半场', items: [fixture({ settled: false, displayState: '中场' })] }, { match_status: 'state:上半场' }), 'correct echo cannot conceal a different source state')
  throws(() => filters.validateDrawObservationFilters({ matchStatus: 'unknown', items: [fixture({ settled: false, displayState: '未开始' })] }, { match_status: 'unknown' }), 'unknown echo cannot conceal a named state')
  throws(() => filters.validateDrawObservationFilters({ selection: 'sporttery', items: [fixture()] }, { selection: 'sporttery' }), 'server echo alone cannot mix scopes')
  throws(() => filters.validateDrawObservationFilters({ selection: 'non_sporttery', items: [fixture({ nonSportterySignal: undefined })] }, { selection: 'non_sporttery' }), 'missing non-JC signal is not a candidate')
  for (const query of [{ match_status: 'finished' }, { match_status: 'unfinished' }, { match_status: 'unknown' }, { match_status: 'state:上半场' }, { selection: 'sporttery' }, { selection: 'non_sporttery' }]) {
    const base = { date: '2026-11-04', page: 1, page_size: 50 }
    check(api.drawObservationCacheKey(base) !== api.drawObservationCacheKey({ ...base, ...query }), 'different server-side filters cannot share a frontend request cache key')
  }

  const commonOptions = filters.drawMatchStatusOptions()
  for (const label of ['全部状态', '未开始', '进行中', '上半场', '中场', '下半场', '加时', '点球', '完赛', '未完赛（汇总）', '推迟', '延期', '取消', '腰斩', '中断', '暂停', '待定', '状态未知']) {
    check(commonOptions.some(option => option.title === label), `${label}: the dropdown includes the complete standard state list`)
  }
  equal(new Set(commonOptions.map(option => option.value)).size, commonOptions.length, 'standard statuses have unique submitted values')
  const observed = [
    { title: '技术延迟开球', value: 'state:技术延迟开球' },
    { title: '重复记录', value: 'state:技术延迟开球' },
    { title: '上半场', value: 'state:上半场' },
    { title: '不是可靠标题', value: 'state:加时赛中场' },
  ]
  const observedBefore = JSON.stringify(observed)
  const combinedOptions = filters.drawMatchStatusOptions(observed)
  equal(combinedOptions.filter(option => option.value === 'state:技术延迟开球'), [{ title: '技术延迟开球', value: 'state:技术延迟开球' }], 'an unseen source state is added once from its actual value')
  equal(combinedOptions.filter(option => option.value === 'state:上半场').length, 1, 'observed common states do not duplicate existing options')
  check(combinedOptions.some(option => option.title === '加时赛中场' && option.value === 'state:加时赛中场'), 'additional source phases are preserved without guessing their numeric status')
  equal(JSON.stringify(observed), observedBefore, 'option merging does not mutate the server response')
  check(filters.drawMatchStatusOptions([], 'state:技术延迟开球').some(option => option.title === '技术延迟开球'), 'the current source selection remains labelled while date/scope changes')
  combinedOptions[0].title = 'changed externally'
  equal(filters.drawMatchStatusOptions()[0].title, '全部状态', 'returned options cannot mutate future common options')
  for (const message of ['match_status 应为 all 或 finished', 'match_status 应为 all、finished 或 unfinished']) {
    const visible = filters.drawObservationFilterError(message)
    check(visible.includes('28080') && visible.includes('重启') && !visible.includes('match_status'), 'legacy status rejection explains that the running service needs the update')
  }
  for (const message of ['数据库连接失败', '读取本地比赛失败，请稍后重试', 'selection 应为 all、sporttery 或 non_sporttery']) {
    equal(filters.drawObservationFilterError(message), message, 'unrelated errors keep their original diagnostic detail')
  }

  const page = parse(read('admin/src/views/statistics/DrawHistory.vue')).descriptor
  for (const [model, key, options] of [['matchStatus', 'match_status', []], ['selection', 'selection', ['全部比赛', '竞彩入选', '非竞彩入选']]]) {
    check(page.template.content.includes(`v-model="${model}"`), `${model}: control is rendered`)
    check(new RegExp(`${key}: ${model}\\.value`).test(page.scriptSetup.content), `${model}: control reaches the server query`)
    check(new RegExp(`function reset\\(\\)[\\s\\S]*?${model}\\.value = 'all'`).test(page.scriptSetup.content), `${model}: reset restores all`)
    for (const option of options) check(page.scriptSetup.content.includes(`title: '${option}'`), `${model}: ${option} is available`)
  }
  check(page.scriptSetup.content.includes('drawMatchStatusOptions(observedMatchStatuses.value, matchStatus.value)'), 'the control merges source states with all common options')
  check(page.scriptSetup.content.includes('observedMatchStatuses.value = data.matchStatusOptions || []'), 'dynamic options come from the server full-day response')
  check(page.template.content.includes(':items="matchStatusOptions"'), 'the status dropdown renders all computed options')
  check(page.scriptSetup.content.includes('error.value = drawObservationFilterError('), 'the old-service status rejection is translated in the real request error path')
  check(page.template.content.includes('v-for="row in report?.items || []"'), 'the list renders the server page without locally dropping rows')
  check(page.scriptSetup.content.includes('validateDrawObservationFilters(data, submitted)'), 'all tabs validate server filter behavior')
  check(page.template.content.includes('class="filter-selection" @update:model-value="changeStatusFilter"'), 'selection changes immediately submit a fresh first-page request')
  check(page.scriptSetup.content.includes('function changeStatusFilter() {\r\n  invalidateResults()\r\n  return load(true)') || /function changeStatusFilter\(\) \{\s+invalidateResults\(\)\s+return load\(true\)/.test(page.scriptSetup.content), 'status changes reset pagination through load(true)')

  for (const [input, expected] of [
    [undefined, ''], [null, ''], ['', ''], ['  ', ''],
    ['/footballimg/team-id', '/footballimg/team-id'], ['/images/team.png', '/images/team.png'],
    ['https://img.vipc.cn/vipc-sport/image/team-id?x=1', '/footballimg/team-id'],
    ['//vipc.cn/vipc-sport/image/team-id', '/footballimg/team-id'],
    ['https://images.example.org/team.png', 'https://images.example.org/team.png'],
    ['javascript:alert(1)', ''], ['data:image/png;base64,abc', ''],
    ['relative-invalid', ''],
    ['https://notvipc.cn/vipc-sport/image/team-id', 'https://notvipc.cn/vipc-sport/image/team-id'],
  ]) equal(logos.teamLogoUrl(input), expected, 'stored logo URL is resolved without fetching or confusing VIPC hostnames')
  const component = load(compileScript(parse(read('admin/src/views/statistics/DrawTeamLogo.vue')).descriptor, {
    id: 'draw-team-logo-offline', inlineTemplate: true,
  }).content, { '@/utils/teamLogo': logos }).default
  const warnings = []
  for (const [src, name, expected] of [['/footballimg/home-id', '主队', '/footballimg/home-id'], ['https://img.vipc.cn/vipc-sport/image/away-id', '客队', '/footballimg/away-id'], ['', '主队', null]]) {
    const app = vue.createSSRApp({ render: () => vue.h(component, { src, name }) })
    app.config.warnHandler = warning => warnings.push(warning)
    const html = await renderToString(app)
    if (expected) {
      check(html.includes(`src="${expected}"`), 'actual Vue markup points to the collected logo')
      check(html.includes('loading="lazy"') && html.includes('decoding="async"'), 'offscreen logos do not eagerly load the full table')
      check(html.includes('width="24"') && html.includes('height="24"'), 'logo dimensions are stable before loading')
    } else check(html.includes('team-logo-fallback') && html.includes('主') && !html.includes('<img'), 'missing logo renders a stable team placeholder')
  }
  const source = vue.ref('/footballimg/first')
  const renderer = memoryRenderer()
  const host = { kind: 'root', children: [], props: {}, parent: null }
  const app = renderer.createApp({ render: () => vue.h(component, { src: source.value, name: '主队' }) })
  app.config.warnHandler = warning => warnings.push(warning)
  app.mount(host)
  const image = find(host, node => node.kind === 'img')
  check(!!image, 'mounted logo starts as an image')
  image.props.onError()
  await vue.nextTick()
  check(!find(host, node => node.kind === 'img') && !!find(host, node => node.props.class === 'team-logo-fallback'), 'a failed image is replaced by the placeholder')
  source.value = '/footballimg/second'
  await vue.nextTick()
  equal(find(host, node => node.kind === 'img')?.props.src, '/footballimg/second', 'a new logo resets the prior image failure')
  app.unmount()
  equal(warnings, [], 'actual Vue SSR and memory mounting produce no warnings')
  console.log(`PASS ${checks} draw-list filter and logo checks; 3 SSR states; image-error and replacement verified offline.`)
}
main().catch(error => { console.error(error); process.exitCode = 1 })
