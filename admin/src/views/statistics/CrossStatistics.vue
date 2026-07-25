<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getCrossStatistics } from '@/api'

// ==== 后端矩阵类型 ====
interface DimMeta { name: string; label: string; market: string; marketLabel: string }
interface Row {
  id: string; dt: string; lg: string; hm: string; gt: string; hs: number; gs: number
  mt?: string; up?: boolean
  t: boolean
  out: string; ah: string; ou: string; oe: string
  f: Record<string, number | undefined>
  d: Record<string, string | undefined>
  h: Record<string, number | undefined>
}
interface WorkshopData {
  needs_recompute: boolean
  generated_at?: string
  settled_total?: number
  upcoming_total?: number
  train_total?: number
  test_total?: number
  split_date?: string
  dims: DimMeta[]
  rows: Row[]
}

const loading = ref(false)
const recomputing = ref(false)
const error = ref('')
const data = ref<WorkshopData | null>(null)

async function fetchData(refresh = false) {
  if (refresh) recomputing.value = true
  else loading.value = true
  error.value = ''
  try {
    const { data: resp } = await getCrossStatistics(refresh ? { refresh: 1 } : {})
    data.value = resp as WorkshopData
  } catch (requestError) {
    const err = requestError as { response?: { data?: { error?: string } }; message?: string }
    error.value = err.response?.data?.error || err.message || '加载失败'
  } finally {
    loading.value = false
    recomputing.value = false
  }
}

// ==== 提取参数（与离线验证一致）====
// ==== 诚实统计原则（2026-07 实盘亏损复盘后重写）====
// 1. 规则筛选只允许看「训练段」(前70%)；「前瞻段」(后30%)绝不参与筛选，只用于报告。
//    （旧版把"前瞻也达标"当保留条件 → 从上万组合里挑两段都好看的幸存者 → 展示数字虚高
//      20-30pp，walk-forward 实测强研判仅 ~57%，与页面宣称的 75-90% 严重不符。）
// 2. 训练段达标用 Wilson 95% 下界（自动惩罚小样本），不是裸命中率。
// 3. 研判用前瞻段数字（不足时用收缩估计），强研判必须有前瞻确认。
const MINN = 60, HIGH = 70, LOW = 30, MARGIN = 10, FWD_MINN = 15
const DRAW_HI = 33, DRAW_MARGIN = 4, DRAW_MINN = 50
const NODRAW_MAX = 12, NODRAW_MARGIN = 6
const SHRINK_K = 30 // 收缩伪计数：前瞻样本不足时把训练命中率向基线收缩

function wilsonLB(hit: number, n: number): number {
  if (!n) return 0
  const p = hit / n, z = 1.96, z2 = z * z
  const den = 1 + z2 / n, c = p + z2 / (2 * n)
  const sd = z * Math.sqrt((p * (1 - p) + z2 / (4 * n)) / n)
  return Math.max(0, (c - sd) / den) * 100
}
function wilsonUB(hit: number, n: number): number {
  if (!n) return 100
  const p = hit / n, z = 1.96, z2 = z * z
  const den = 1 + z2 / n, c = p + z2 / (2 * n)
  const sd = z * Math.sqrt((p * (1 - p) + z2 / (4 * n)) / n)
  return Math.min(1, (c + sd) / den) * 100
}

// ==== 谓词构造 ====
interface Pred { label: string; hold: boolean[] }
function dirLabel(market: string, dir: string): string {
  if (dir === 'fire') return '触发'
  if (market === 'spf') return { home: '判主', draw: '判平', away: '判客' }[dir] ?? dir
  if (market === 'asian') return { home: '判主赢盘', away: '判客赢盘' }[dir] ?? dir
  if (market === 'dxq') return { over: '判大', under: '判小' }[dir] ?? dir
  return dir
}
const FEAT_BUCKETS: Array<[string, string, (v: number) => boolean]> = [
  ['让球线≤-1', 'ahLine', v => v <= -0.999],
  ['让球线(-1,-0.5]', 'ahLine', v => v > -0.999 && v <= -0.49],
  ['让球线(-0.5,-0.25]', 'ahLine', v => v > -0.49 && v <= -0.24],
  ['让球线≈0', 'ahLine', v => v > -0.24 && v < 0.26],
  ['让球线[0.5,1)', 'ahLine', v => v >= 0.5 && v < 1],
  ['让球线≥1', 'ahLine', v => v >= 1],
  ['大小球线≤2.25', 'ouLine', v => v <= 2.25],
  ['大小球线=2.5', 'ouLine', v => v > 2.4 && v < 2.76],
  ['大小球线≥3', 'ouLine', v => v >= 2.99],
  ['主推概率≥65', 'baseProb', v => v >= 65],
  ['主推概率55-65', 'baseProb', v => v >= 55 && v < 65],
  ['主推概率45-55', 'baseProb', v => v >= 45 && v < 55],
  ['主推概率<45', 'baseProb', v => v < 45],
  ['亚盘热度≥80', 'asianHeat', v => v >= 80],
  ['亚盘热度70-80', 'asianHeat', v => v >= 70 && v < 80],
  ['亚盘热度≤35', 'asianHeat', v => v <= 35],
  ['大小球热度≥65', 'goalsHeat', v => v >= 65],
  ['大小球热度≤45', 'goalsHeat', v => v <= 45],
  ['近期球数≥3.3', 'recentGoals', v => v >= 3.3],
  ['近期球数≤2.5', 'recentGoals', v => v <= 2.5],
  ['历史球数≤2.4', 'historyGoals', v => v <= 2.4],
  ['近期强弱差≥1.5', 'recentDiff', v => v >= 1.5],
  ['近期强弱差≤-1.5', 'recentDiff', v => v <= -1.5],
]

const predicates = computed<Pred[]>(() => {
  const d = data.value
  if (!d) return []
  const rows = d.rows
  const preds: Pred[] = []
  for (const [label, fkey, fn] of FEAT_BUCKETS) {
    preds.push({ label, hold: rows.map(r => { const v = r.f[fkey]; return v !== undefined && fn(v) }) })
  }
  for (const dim of d.dims) {
    const counts = new Map<string, number>()
    for (const r of rows) { const dir = r.d[dim.name]; if (dir) counts.set(dir, (counts.get(dir) ?? 0) + 1) }
    for (const [dir, cnt] of counts) {
      if (cnt < 40) continue
      preds.push({ label: `${dim.label}·${dirLabel(dim.market, dir)}`, hold: rows.map(r => r.d[dim.name] === dir) })
    }
  }
  return preds
})

// ==== 目标定义 ====
interface TDef { key: string; label: string; col: (r: Row) => string; usable: (r: Row) => boolean; sides: Array<{ v: string; l: string }> }
// usable 仅统计已完赛比赛（待赛 up 无结果，排除出统计域）
const TDEFS: TDef[] = [
  { key: 'spf', label: '胜平负', col: r => r.out, usable: r => !r.up, sides: [{ v: 'home', l: '主胜' }, { v: 'draw', l: '平局' }, { v: 'away', l: '客胜' }] },
  { key: 'asian', label: '让球赢盘', col: r => r.ah, usable: r => !r.up && r.ah !== '', sides: [{ v: 'home', l: '主队赢盘' }, { v: 'away', l: '客队赢盘' }] },
  { key: 'dxq', label: '大小球', col: r => r.ou, usable: r => !r.up && r.ou !== '', sides: [{ v: 'over', l: '大球' }, { v: 'under', l: '小球' }] },
  { key: 'oe', label: '单双', col: r => r.oe, usable: r => !r.up, sides: [{ v: 'odd', l: '单' }, { v: 'even', l: '双' }] },
]

// rate=训练段命中率（选规则只看它）；testRate/testN=前瞻段实测（选规则不看、只报告）
interface SideTag { act: '买' | '防' | '平' | '胜负'; side: string; label: string; rate: number; trainN: number; testRate: number; testN: number }
interface Finding { target: string; targetLabel: string; preds: string[]; n: number; testN: number; sides: SideTag[]; idx: Set<number>; holds: boolean[][]; dev: number }

// ==== 提取引擎（beam 搜索 + 相对基线阈值 + 集合重合度去重）====
const extraction = computed<{ buy: Finding[]; defend: Finding[]; draw: Finding[]; nodraw: Finding[]; baselines: Record<string, Record<string, number>> } | null>(() => {
  const d = data.value
  if (!d || !d.rows.length) return null
  const rows = d.rows
  const preds = predicates.value
  const baselines: Record<string, Record<string, number>> = {}
  const found: Finding[] = []

  for (const t of TDEFS) {
    const uni: number[] = []
    for (let i = 0; i < rows.length; i++) if (t.usable(rows[i])) uni.push(i)
    // 基线
    const base: Record<string, number> = {}
    for (const s of t.sides) base[s.v] = uni.length ? (uni.filter(i => t.col(rows[i]) === s.v).length / uni.length) * 100 : 0
    baselines[t.key] = base
    // 有效谓词（该目标域支持度≥MINN/2）
    const active = preds.filter(p => uni.reduce((c, i) => c + (p.hold[i] ? 1 : 0), 0) >= MINN / 2)

    let beam: Array<{ pi: number[]; idx: number[] }> = [{ pi: [], idx: uni }]
    const seen = new Set<string>()
    for (let depth = 1; depth <= 3; depth++) {
      const cand: Array<{ pi: number[]; idx: number[]; dev: number; sc: Record<string, [number, number]>; ten: number }> = []
      for (const path of beam) {
        for (let ai = 0; ai < active.length; ai++) {
          if (path.pi.includes(ai)) continue
          const key = [...path.pi, ai].sort((a, b) => a - b).join(',')
          if (seen.has(key)) continue
          seen.add(key)
          const idx = path.idx.filter(i => active[ai].hold[i])
          if (idx.length < MINN) continue
          let ten = 0
          const sc: Record<string, [number, number]> = {}
          for (const s of t.sides) sc[s.v] = [0, 0]
          for (const i of idx) {
            const r = rows[i]; const actual = t.col(r)
            if (r.t) ten++
            const c = sc[actual]; if (c) { c[0]++; if (r.t) c[1]++ }
          }
          let dev = 0
          for (const s of t.sides) dev = Math.max(dev, Math.abs((sc[s.v][0] / idx.length) * 100 - base[s.v]))
          cand.push({ pi: [...path.pi, ai], idx, dev, sc, ten })
        }
      }
      for (const c of cand) {
        const n = c.idx.length
        const trn = n - c.ten
        if (trn < 40) continue
        const tags: SideTag[] = []
        for (const s of t.sides) {
          // 只用训练段筛选（Wilson 下界防小样本假信号）；前瞻段只报告
          const trainH = c.sc[s.v][0] - c.sc[s.v][1]
          const train = (trainH / trn) * 100
          const test = c.ten ? (c.sc[s.v][1] / c.ten) * 100 : 0
          if (train >= HIGH && wilsonLB(trainH, trn) >= base[s.v] + MARGIN)
            tags.push({ act: '买', side: s.v, label: s.l, rate: train, trainN: trn, testRate: test, testN: c.ten })
          else if (train <= LOW && wilsonUB(trainH, trn) <= base[s.v] - MARGIN)
            tags.push({ act: '防', side: s.v, label: s.l, rate: train, trainN: trn, testRate: test, testN: c.ten })
        }
        if (tags.length) found.push({
          target: t.key, targetLabel: t.label, preds: c.pi.map(ai => active[ai].label),
          n, testN: c.ten, sides: tags, idx: new Set(c.idx), holds: c.pi.map(ai => active[ai].hold), dev: c.dev,
        })
      }
      cand.sort((a, b) => b.dev - a.dev)
      beam = cand.slice(0, 15).map(c => ({ pi: c.pi, idx: c.idx }))
      if (!beam.length) break
    }
  }

  // 集合重合度去重（同目标 Jaccard≥0.6，保留偏离大者）
  function dedup(items: Finding[]): Finding[] {
    const out: Finding[] = []
    for (const f of items) {
      let dup = false
      for (const g of out) {
        if (g.target !== f.target) continue
        let inter = 0
        for (const i of f.idx) if (g.idx.has(i)) inter++
        const union = f.idx.size + g.idx.size - inter
        if (union && inter / union >= 0.6) { dup = true; break }
      }
      if (!dup) out.push(f)
    }
    return out
  }
  const hasBuy = (f: Finding) => f.sides.some(s => s.act === '买')
  let buy = found.filter(hasBuy)
  buy.sort((a, b) => Math.max(...b.sides.filter(s => s.act === '买').map(s => s.rate)) - Math.max(...a.sides.filter(s => s.act === '买').map(s => s.rate)) || b.n - a.n)
  buy = dedup(buy)
  let defend = found.filter(f => !hasBuy(f) && f.sides.length)
  defend.sort((a, b) => Math.min(...a.sides.map(s => s.rate)) - Math.min(...b.sides.map(s => s.rate)) || b.n - a.n)
  defend = dedup(defend)

  // ==== 平局榜：胜平负·平局，排除让球线，相对基线抬升 ====
  const drawUni: number[] = []
  for (let i = 0; i < rows.length; i++) if (!rows[i].up) drawUni.push(i)
  // 排除方向性让球线（强弱悬殊/55开无信息），但保留平手盘 让球线≈0（势均力敌→易平，是真信号）
  const drawActive = preds.filter(p => !(p.label.startsWith('让球线') && !p.label.includes('≈0')) && drawUni.reduce((c, i) => c + (p.hold[i] ? 1 : 0), 0) >= DRAW_MINN / 2)
  const drawBase = baselines.spf?.draw ?? 0
  const drawFound: Finding[] = []
  let dbeam: Array<{ pi: number[]; idx: number[] }> = [{ pi: [], idx: drawUni }]
  const dseen = new Set<string>()
  for (let depth = 1; depth <= 3; depth++) {
    const cand: Array<{ pi: number[]; idx: number[]; rate: number }> = []
    for (const path of dbeam) {
      for (let ai = 0; ai < drawActive.length; ai++) {
        if (path.pi.includes(ai)) continue
        const pi = [...path.pi, ai].sort((a, b) => a - b)
        const key = pi.join(',')
        if (dseen.has(key)) continue
        dseen.add(key)
        const idx = path.idx.filter(i => drawActive[ai].hold[i])
        if (idx.length < DRAW_MINN) continue
        let ten = 0, dh = 0, th = 0
        for (const i of idx) { const r = rows[i]; const isD = r.out === 'draw'; if (isD) dh++; if (r.t) { ten++; if (isD) th++ } }
        const trn = idx.length - ten, trainH = dh - th
        const train = trn ? (trainH / trn) * 100 : 0
        const test = ten ? (th / ten) * 100 : 0
        cand.push({ pi, idx, rate: train })
        // 训练段筛选（Wilson 下界超基线），前瞻段只报告
        if (trn >= 40 && train >= DRAW_HI && wilsonLB(trainH, trn) >= drawBase + DRAW_MARGIN) {
          drawFound.push({
            target: 'spf', targetLabel: '胜平负', preds: pi.map(i => drawActive[i].label),
            n: idx.length, testN: ten, sides: [{ act: '平', side: 'draw', label: '平局', rate: train, trainN: trn, testRate: test, testN: ten }],
            idx: new Set(idx), holds: pi.map(i => drawActive[i].hold), dev: train - drawBase,
          })
        }
      }
    }
    cand.sort((a, b) => b.rate - a.rate)
    dbeam = cand.slice(0, 15).map(c => ({ pi: c.pi, idx: c.idx }))
    if (!dbeam.length) break
  }
  let draw = drawFound.sort((a, b) => b.sides[0].rate - a.sides[0].rate || b.n - a.n)
  draw = dedup(draw)

  // ==== 有胜负榜：把平局压到 ≤NODRAW_MAX（买有胜负 主+客 命中 ≥88%）；保留全部维度 ====
  const nodrawActive = preds.filter(p => drawUni.reduce((c, i) => c + (p.hold[i] ? 1 : 0), 0) >= DRAW_MINN / 2)
  const nodrawFound: Finding[] = []
  let nbeam: Array<{ pi: number[]; idx: number[] }> = [{ pi: [], idx: drawUni }]
  const nseen = new Set<string>()
  for (let depth = 1; depth <= 3; depth++) {
    const cand: Array<{ pi: number[]; idx: number[]; drawRate: number }> = []
    for (const path of nbeam) {
      for (let ai = 0; ai < nodrawActive.length; ai++) {
        if (path.pi.includes(ai)) continue
        const pi = [...path.pi, ai].sort((a, b) => a - b)
        const key = pi.join(',')
        if (nseen.has(key)) continue
        nseen.add(key)
        const idx = path.idx.filter(i => nodrawActive[ai].hold[i])
        if (idx.length < DRAW_MINN) continue
        let ten = 0, dh = 0, th = 0
        for (const i of idx) { const r = rows[i]; const isD = r.out === 'draw'; if (isD) dh++; if (r.t) { ten++; if (isD) th++ } }
        const trn = idx.length - ten, trainH = dh - th
        const trainDr = trn ? (trainH / trn) * 100 : 100
        const testDr = ten ? (th / ten) * 100 : 0
        cand.push({ pi, idx, drawRate: trainDr })
        // 训练段筛选（Wilson 上界压住平局），前瞻段只报告
        if (trn >= 40 && trainDr <= NODRAW_MAX && wilsonUB(trainH, trn) <= drawBase - NODRAW_MARGIN) {
          nodrawFound.push({
            target: 'spf', targetLabel: '胜平负', preds: pi.map(i => nodrawActive[i].label),
            n: idx.length, testN: ten, sides: [{ act: '胜负', side: 'draw', label: '有胜负(主+客)', rate: 100 - trainDr, trainN: trn, testRate: 100 - testDr, testN: ten }],
            idx: new Set(idx), holds: pi.map(i => nodrawActive[i].hold), dev: drawBase - trainDr,
          })
        }
      }
    }
    cand.sort((a, b) => a.drawRate - b.drawRate)
    nbeam = cand.slice(0, 15).map(c => ({ pi: c.pi, idx: c.idx }))
    if (!nbeam.length) break
  }
  let nodraw = nodrawFound.sort((a, b) => b.sides[0].rate - a.sides[0].rate || b.n - a.n)
  nodraw = dedup(nodraw)

  return { buy, defend, draw, nodraw, baselines }
})

// ==== 展示 ====
const viewKind = ref<'buy' | 'defend' | 'draw' | 'nodraw'>('buy')
const viewTarget = ref('all')
const TARGET_FILTER = [{ v: 'all', l: '全部' }, { v: 'spf', l: '胜平负' }, { v: 'asian', l: '让球赢盘' }, { v: 'dxq', l: '大小球' }, { v: 'oe', l: '单双' }]

const list = computed<Finding[]>(() => {
  const ex = extraction.value
  if (!ex) return []
  const src = viewKind.value === 'buy' ? ex.buy : viewKind.value === 'defend' ? ex.defend : viewKind.value === 'draw' ? ex.draw : ex.nodraw
  // 平局榜/有胜负榜只有胜平负，目标筛选对它们无意义
  if (viewKind.value === 'draw' || viewKind.value === 'nodraw') return src
  return viewTarget.value === 'all' ? src : src.filter(f => f.target === viewTarget.value)
})
const buyCount = computed(() => extraction.value?.buy.length ?? 0)
const defendCount = computed(() => extraction.value?.defend.length ?? 0)
const drawCount = computed(() => extraction.value?.draw.length ?? 0)
const nodrawCount = computed(() => extraction.value?.nodraw.length ?? 0)
const spfBase = computed(() => extraction.value?.baselines.spf)

function fmt(v: number) { return v.toFixed(0) + '%' }
function sideColor(act: string): string { return act === '买' || act === '胜负' ? 'success' : act === '防' ? 'error' : 'info' }
// 绿点 = 前瞻段（未参与筛选的数据）样本足且仍支持该方向；这是唯一可信的确认
function robust(s: SideTag): 'strong' | 'ok' {
  if (s.testN < FWD_MINN) return 'ok'
  return ((s.act === '买' && s.testRate >= 60) || (s.act === '防' && s.testRate <= 40) || (s.act === '平' && s.testRate >= 28) || (s.act === '胜负' && s.testRate >= 85)) ? 'strong' : 'ok'
}

// ==== 比赛清单下钻 ====
const expanded = ref<number | null>(null)
function toggle(idx: number) { expanded.value = expanded.value === idx ? null : idx }
watch([viewKind, viewTarget], () => { expanded.value = null })

const ACTUAL_LABEL: Record<string, Record<string, string>> = {
  spf: { home: '主胜', draw: '平局', away: '客胜' },
  asian: { home: '主赢盘', away: '客赢盘' },
  dxq: { over: '大球', under: '小球' },
  oe: { odd: '单', even: '双' },
}
function actualOf(target: string, r: Row): string {
  return target === 'spf' ? r.out : target === 'asian' ? r.ah : target === 'dxq' ? r.ou : r.oe
}
function actualLabel(target: string, r: Row): string {
  return ACTUAL_LABEL[target]?.[actualOf(target, r)] ?? '—'
}
function primarySide(f: Finding): SideTag {
  return f.sides.find(s => s.act === '买' || s.act === '平' || s.act === '胜负') ?? f.sides[0]
}
// 命中判定：买/平=实际==该面；防/胜负=实际!=该面（排除成功=有胜负）
function isHit(f: Finding, r: Row): boolean {
  const p = primarySide(f)
  const actual = actualOf(f.target, r)
  return (p.act === '防' || p.act === '胜负') ? actual !== p.side : actual === p.side
}
function hitText(f: Finding, r: Row): string {
  const act = primarySide(f).act
  const ok = isHit(f, r)
  if (act === '防') return ok ? '已排除' : '被打出'
  if (act === '胜负') return ok ? '有胜负' : '踢平'
  return ok ? '命中' : '未中'
}
function matchesOf(f: Finding): Row[] {
  const rows = data.value?.rows ?? []
  return [...f.idx].map(i => rows[i]).sort((a, b) => (a.dt < b.dt ? 1 : a.dt > b.dt ? -1 : 0))
}

// ==== 预测未来：把买榜/防榜规律套到待赛比赛 ====
const tab = ref<'past' | 'future'>('past')
interface Rec { target: string; targetLabel: string; act: '买' | '防' | '平' | '胜负'; side: string; label: string; rate: number; trainN: number; testRate: number; testN: number; ruleCount: number; sample: number }
interface Verdict { text: string; side: string; level: 'strong' | 'mid' | 'soft'; detail?: string }
interface Prediction { row: Row; recs: Rec[]; verdict?: Verdict }

// 综合研判：一场比赛只有一个答案。把所有信号折算成对 主/平/客 三个面各自的
// 「相对基线抬升(edge)」，取 edge 最高者 = 唯一投注答案；置信度由与次高面的差距决定。
//   买X → X 的 edge += rate−base[X]（正）
//   防X → X 的 edge += rate−base[X]（rate 低，为负，压该面）
//   平局榜 → 平 的 edge += rate−base[平]
//   有胜负榜 → 平 的 edge += (100−rate)−base[平]（压平局，把概率让给主/客）
function buildVerdict(recs: Rec[], base: Record<string, number>): Verdict | undefined {
  const spf = recs.filter(r => r.target === 'spf')
  const bH = base.home ?? 47, bD = base.draw ?? 22, bA = base.away ?? 31
  // 诚实估计：前瞻样本足(≥FWD_MINN)用前瞻实测；否则把训练命中率向基线收缩
  const used = (r: Rec, b: number) => r.testN >= FWD_MINN ? r.testRate : (r.rate * r.trainN + b * SHRINK_K) / (r.trainN + SHRINK_K)
  const best = (act: string, side: string) => {
    const rs = spf.filter(r => r.act === act && r.side === side)
    return rs.length ? rs.reduce((a, b) => (b.rate > a.rate ? b : a)) : null
  }
  const buyH = best('买', 'home'), buyA = best('买', 'away'), drawR = best('平', 'draw')
  const defH = best('防', 'home'), defA = best('防', 'away')
  const nodraw = spf.filter(r => r.act === '胜负')
  const nodrawBest = nodraw.length ? nodraw.reduce((a, b) => (b.rate > a.rate ? b : a)) : null
  const nodrawTop = nodrawBest ? used(nodrawBest, 100 - bD) : null

  let eH = 0, eD = 0, eA = 0
  if (buyH) eH += used(buyH, bH) - bH
  if (defH) eH += used(defH, bH) - bH
  if (buyA) eA += used(buyA, bA) - bA
  if (defA) eA += used(defA, bA) - bA
  if (drawR) eD += used(drawR, bD) - bD
  if (nodrawTop != null) eD += (100 - nodrawTop) - bD

  const arr: Array<[string, string, number]> = [['home', '主胜', eH], ['draw', '平局', eD], ['away', '客胜', eA]]
  arr.sort((a, b) => b[2] - a[2])
  const top = arr[0], second = arr[1]
  const edgeDetail = `edge 主${eH >= 0 ? '+' : ''}${Math.round(eH)} / 平${eD >= 0 ? '+' : ''}${Math.round(eD)} / 客${eA >= 0 ? '+' : ''}${Math.round(eA)}`
  if (top[2] < 5) {
    // 无单一面被显著抬升；若有胜负触发，答案就是「买有胜负(主+客)双选」
    if (nodrawTop != null) return { text: `买 有胜负(主+客)（分不出主客方向）`, side: '', level: nodrawTop >= 88 ? 'mid' : 'soft', detail: edgeDetail }
    return undefined
  }
  const margin = top[2] - second[2]
  let level: Verdict['level'] = margin >= 20 ? 'strong' : margin >= 8 ? 'mid' : 'soft'
  // 强研判必须有前瞻确认：驱动该面的正向规则前瞻样本≥FWD_MINN 且前瞻仍显著超基线
  if (level === 'strong') {
    const drive = top[0] === 'home' ? buyH : top[0] === 'away' ? buyA : drawR
    const db = top[0] === 'home' ? bH : top[0] === 'away' ? bA : bD
    if (!drive || drive.testN < FWD_MINN || drive.testRate - db < 10) level = 'mid'
  }
  const conf = level === 'soft' ? '（置信偏低，与次选接近）' : level === 'strong' ? '（前瞻确认）' : ''
  return { text: `买 ${top[1]}${conf}`, side: top[0], level, detail: edgeDetail }
}

const predictions = computed<Prediction[]>(() => {
  const ex = extraction.value
  const rows = data.value?.rows
  if (!ex || !rows) return []
  const upIdx: number[] = []
  rows.forEach((r, i) => { if (r.up) upIdx.push(i) })
  const out: Prediction[] = []
  for (const j of upIdx) {
    const map = new Map<string, Rec>()
    const consider = (findings: Finding[]) => {
      for (const f of findings) {
        if (!f.holds.every(h => h[j])) continue
        for (const s of f.sides) {
          const key = `${f.target}/${s.act}/${s.side}`
          const cur = map.get(key)
          if (!cur) map.set(key, { target: f.target, targetLabel: f.targetLabel, act: s.act, side: s.side, label: s.label, rate: s.rate, trainN: s.trainN, testRate: s.testRate, testN: s.testN, ruleCount: 1, sample: f.n })
          else {
            cur.ruleCount++
            // 取「前瞻样本足且前瞻最高」的规则代表该面；没有前瞻足的再按训练比
            const curKey = cur.testN >= FWD_MINN ? 1000 + cur.testRate : cur.rate
            const newKey = s.testN >= FWD_MINN ? 1000 + s.testRate : s.rate
            if (newKey > curKey) { cur.rate = s.rate; cur.trainN = s.trainN; cur.testRate = s.testRate; cur.testN = s.testN; cur.sample = f.n }
          }
        }
      }
    }
    consider(ex.buy); consider(ex.nodraw); consider(ex.draw); consider(ex.defend)
    const order: Record<string, number> = { '买': 0, '胜负': 1, '平': 2, '防': 3 }
    const recs = [...map.values()].sort((a, b) => a.act === b.act ? b.rate - a.rate : order[a.act] - order[b.act])
    const verdict = buildVerdict(recs, ex.baselines.spf ?? {})
    // 只保留有明确研判(唯一答案)的比赛，避免只有弱信号的比赛把列表塞满
    if (verdict) out.push({ row: rows[j], recs, verdict })
  }
  return out.sort((a, b) => ((a.row.dt + (a.row.mt ?? '')) < (b.row.dt + (b.row.mt ?? '')) ? -1 : 1))
})
// 置信度过滤：强 / 强+中 / 全部（默认只看「强」，最干净可下手）
const predConf = ref<'strong' | 'mid' | 'all'>('strong')
const LEVEL_RANK: Record<string, number> = { strong: 3, mid: 2, soft: 1 }
const predictionGroups = computed(() => {
  const min = predConf.value === 'strong' ? 3 : predConf.value === 'mid' ? 2 : 1
  const groups: Array<{ date: string; items: Prediction[] }> = []
  for (const p of predictions.value) {
    if (!p.verdict || LEVEL_RANK[p.verdict.level] < min) continue
    const last = groups[groups.length - 1]
    if (last && last.date === p.row.dt) last.items.push(p)
    else groups.push({ date: p.row.dt, items: [p] })
  }
  return groups
})
const predShownCount = computed(() => predictionGroups.value.reduce((n, g) => n + g.items.length, 0))
const strongPredCount = computed(() => predictions.value.filter(p => p.verdict?.level === 'strong').length)
function matchTime(r: Row): string {
  const raw = r.mt ?? ''
  return raw.length >= 16 ? raw.slice(11, 16) : raw
}
const predExpanded = ref<string | null>(null)
function togglePred(id: string) { predExpanded.value = predExpanded.value === id ? null : id }
const VERDICT_SIDE_COLOR: Record<string, string> = { home: 'success', away: 'success', draw: 'info', '': 'primary' }

onMounted(() => fetchData(false))
</script>

<template>
  <div>
    <div class="d-flex flex-wrap align-center mb-4 ga-3">
      <div>
        <h2 class="text-h5 font-weight-bold">信号命中率总台 · 总结过去 · 预测未来</h2>
        <div class="text-body-2 text-medium-emphasis mt-1">
          规则只在<b>训练段</b>（前70%完赛）发现；<b>前瞻段</b>（后30%）从不参与筛选、只如实报告。<b>请只信「前瞻」列</b>——训练命中率是挖掘上限，实盘必然向基线回归。
          Walk-forward 实测（近两周逐日模拟实盘）：<b>强研判前瞻命中 ≈61%</b>（主胜基线 47%）、中 ≈53%。这是本页的真实水平，没有 80%+ 的稳赢。
        </div>
      </div>
      <v-spacer />
      <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchData(true)">重新计算</v-btn>
      <v-btn :loading="loading" color="primary" variant="tonal" prepend-icon="mdi-refresh" @click="fetchData(false)">刷新</v-btn>
    </div>

    <v-alert v-if="error" type="error" variant="tonal" class="mb-5">{{ error }}</v-alert>

    <v-card v-if="data?.needs_recompute" variant="tonal" color="warning">
      <v-card-text class="text-center py-10">
        <div class="text-h6 font-weight-bold mb-2">尚未结算</div>
        <div class="text-body-2 text-medium-emphasis mb-4">点击「重新计算」对全部完赛比赛导出一次特征矩阵（约几秒），之后提取都在本地实时完成。</div>
        <v-btn :loading="recomputing" color="warning" prepend-icon="mdi-calculator" @click="fetchData(true)">重新计算</v-btn>
      </v-card-text>
    </v-card>

    <template v-else-if="data && extraction">
      <div class="d-flex flex-wrap ga-4 mb-3 text-body-2">
        <span>完赛样本 <b>{{ data.settled_total?.toLocaleString() }}</b></span>
        <span>待赛 <b>{{ data.upcoming_total }}</b></span>
        <span class="text-medium-emphasis">训练 {{ data.train_total }} / 验证 {{ data.test_total }}（切分 {{ data.split_date }}）</span>
        <span v-if="spfBase" class="text-medium-emphasis">胜平负基线 主{{ fmt(spfBase.home) }}/平{{ fmt(spfBase.draw) }}/客{{ fmt(spfBase.away) }}</span>
        <span class="text-medium-emphasis">结算于 {{ data.generated_at }}</span>
      </div>

      <v-tabs v-model="tab" color="primary" class="mb-3">
        <v-tab value="past">总结过去 · 高低命中清单</v-tab>
        <v-tab value="future">预测未来 · 待赛应用 ({{ predictions.length }})</v-tab>
      </v-tabs>

      <v-window v-model="tab">
      <v-window-item value="past">
      <v-card>
        <div class="px-4 py-3 d-flex flex-wrap align-center ga-4">
          <v-btn-toggle v-model="viewKind" mandatory density="comfortable" color="primary" variant="outlined">
            <v-btn value="buy" size="small">买榜·训练≥70 ({{ buyCount }})</v-btn>
            <v-btn value="defend" size="small">防榜·训练≤30 ({{ defendCount }})</v-btn>
            <v-btn value="draw" size="small">平局榜·训练≥33 ({{ drawCount }})</v-btn>
            <v-btn value="nodraw" size="small">有胜负榜 ({{ nodrawCount }})</v-btn>
          </v-btn-toggle>
          <v-spacer />
          <v-chip-group v-if="viewKind !== 'draw' && viewKind !== 'nodraw'" v-model="viewTarget" mandatory selected-class="text-primary" class="flex-grow-0">
            <v-chip v-for="tf in TARGET_FILTER" :key="tf.v" :value="tf.v" size="small" variant="tonal">{{ tf.l }}</v-chip>
          </v-chip-group>
        </div>
        <v-divider />
        <div class="px-4 pt-2 text-caption text-medium-emphasis">
          <template v-if="viewKind === 'buy'">「顺带防」= 同一条件下被压到 ≤30% 的另一面，可同时反投；圆点=样本外强成立。近似同一批比赛的局面已按重合度去重。</template>
          <template v-else-if="viewKind === 'defend'">无单一强买面、但某一面被压到 ≤30% 的局面，适合排除/买双选。</template>
          <template v-else-if="viewKind === 'draw'">平局是冷门（基线仅 {{ spfBase ? fmt(spfBase.draw) : '~22%' }}）。训练段抬到 ≥33% 的组合，<b>前瞻实测大多回落到 25–35%</b>——即多数仍不中，价值只在平局赔率(3.0+)能否覆盖，切勿当高命中信号重注。已排除方向性让球线、保留平手盘≈0。</template>
          <template v-else>把平局压到 ≤{{ NODRAW_MAX }}% 的组合 → <b>买「有胜负」(主+客双选)命中 ≥{{ 100 - NODRAW_MAX }}%</b>。驱动多为「强热门(主推概率≥65) + 预期球少」。命中率高但赔率低（约1.1–1.3），当安全腿/串关腿用；也可与买榜方向叠加锁定胜方（见预测未来）。</template>
        </div>

        <div v-if="!list.length" class="text-medium-emphasis py-8 text-center">该筛选下无达标局面。</div>
        <v-table v-else density="compact" class="mt-1">
          <thead>
            <tr>
              <th style="min-width: 250px">投注面 · 前瞻实测（训练参考）</th>
              <th class="text-right">样本(前瞻)</th>
              <th>目标</th>
              <th>触发条件（1–3 层组合）</th>
              <th class="text-center">核对</th>
            </tr>
          </thead>
          <tbody>
            <template v-for="(f, idx) in list" :key="idx">
              <tr class="finding-row" :class="{ open: expanded === idx }" @click="toggle(idx)">
                <td class="py-2">
                  <div v-for="(s, si) in f.sides" :key="si" class="d-flex align-center ga-2 mb-1">
                    <v-chip :color="sideColor(s.act)" :variant="s.act === '防' ? 'tonal' : 'flat'" size="small" class="side-chip">
                      {{ s.act }}【{{ s.label }}】
                    </v-chip>
                    <span class="font-weight-bold" :class="s.testN >= FWD_MINN ? `text-${sideColor(s.act)}` : 'text-medium-emphasis'">
                      前瞻 {{ s.testN ? fmt(s.testRate) : '—' }}<span class="text-caption">({{ s.testN }})</span>
                    </span>
                    <span class="text-caption text-medium-emphasis">训 {{ fmt(s.rate) }}</span>
                    <span v-if="robust(s) === 'strong'" class="robust" title="前瞻段样本足且仍成立（可信）" />
                  </div>
                </td>
                <td class="text-right text-no-wrap">{{ f.n }} <span class="text-medium-emphasis">({{ f.testN }})</span></td>
                <td class="text-no-wrap">{{ f.targetLabel }}</td>
                <td class="py-2">
                  <v-chip v-for="(p, pi) in f.preds" :key="pi" size="x-small" variant="tonal" class="mr-1 mb-1">{{ p }}</v-chip>
                </td>
                <td class="text-center">
                  <v-icon :icon="expanded === idx ? 'mdi-chevron-up' : 'mdi-chevron-down'" size="small" />
                </td>
              </tr>
              <tr v-if="expanded === idx" :key="'d' + idx" class="detail-row">
                <td colspan="5" class="pa-0">
                  <div class="match-list pa-3">
                    <div class="text-caption text-medium-emphasis mb-2">
                      共 {{ f.n }} 场触发（验证集 {{ f.testN }} 场）· 命中判定相对
                      <b>{{ primarySide(f).act }}【{{ primarySide(f).label }}】</b> · 逐场核对
                    </div>
                    <v-table density="compact" class="inner-table">
                      <thead>
                        <tr>
                          <th>日期</th>
                          <th>联赛</th>
                          <th>对阵 · 比分</th>
                          <th>实际结果</th>
                          <th class="text-center">命中</th>
                          <th class="text-center">集</th>
                        </tr>
                      </thead>
                      <tbody>
                        <tr v-for="m in matchesOf(f)" :key="m.id">
                          <td class="text-no-wrap">{{ m.dt }}</td>
                          <td class="text-no-wrap text-truncate" style="max-width: 120px">{{ m.lg }}</td>
                          <td class="text-no-wrap">
                            {{ m.hm }} <b class="score">{{ m.hs }}-{{ m.gs }}</b> {{ m.gt }}
                          </td>
                          <td class="text-no-wrap">{{ actualLabel(f.target, m) }}</td>
                          <td class="text-center">
                            <v-chip :color="isHit(f, m) ? 'success' : 'error'" size="x-small" variant="tonal">
                              {{ hitText(f, m) }}
                            </v-chip>
                          </td>
                          <td class="text-center">
                            <span class="text-caption" :class="m.t ? 'text-primary' : 'text-medium-emphasis'">{{ m.t ? '验证' : '训练' }}</span>
                          </td>
                        </tr>
                      </tbody>
                    </v-table>
                  </div>
                </td>
              </tr>
            </template>
          </tbody>
        </v-table>
      </v-card>

      <div class="text-caption text-medium-emphasis mt-3">
        筛选只看训练段：买 训练≥{{ HIGH }}% 且 Wilson95%下界超基线+{{ MARGIN }}pp / 防 训练≤{{ LOW }}% 且上界低于基线−{{ MARGIN }}pp；训练样本≥40。
        「前瞻」列是规则定型后在未参与筛选的数据上的实测——<b>它才是下注该预期的数字</b>；前瞻(n)&lt;{{ FWD_MINN }} 的规则视为未验证。
        灰色前瞻 = 样本不足；绿点 = 前瞻确认。
      </div>
      </v-window-item>

      <!-- ==== 预测未来 · 待赛应用 ==== -->
      <v-window-item value="future">
        <div class="d-flex flex-wrap align-center ga-3 mb-3">
          <div class="text-caption text-medium-emphasis flex-grow-1">
            未来 {{ data.upcoming_total }} 场待赛中，能给出<b>唯一研判</b>的比赛（edge 最高的面；用前瞻实测/收缩估计，不用训练命中率）。
            <b>诚实预期：强研判历史前瞻约 61%、中约 53%（主胜基线 47%）</b>——有优势但绝非稳赢，连错数场属正常波动，请按此控制注量、勿串关放大。
          </div>
          <v-btn-toggle v-model="predConf" mandatory density="compact" color="primary" variant="outlined">
            <v-btn value="strong" size="small">强 ({{ strongPredCount }})</v-btn>
            <v-btn value="mid" size="small">强+中</v-btn>
            <v-btn value="all" size="small">全部</v-btn>
          </v-btn-toggle>
        </div>
        <div v-if="!predShownCount" class="text-medium-emphasis py-10 text-center">
          当前置信度下暂无可研判的待赛比赛（可切「全部」）。
        </div>
        <template v-for="group in predictionGroups" :key="group.date">
          <div class="date-header px-4 py-2">{{ group.date }} <span class="text-medium-emphasis font-weight-regular">· {{ group.items.length }} 场</span></div>
          <template v-for="p in group.items" :key="p.row.id">
            <div class="pred-row d-flex align-center ga-3 px-4 py-2" @click="togglePred(p.row.id)">
              <span class="num text-medium-emphasis" style="width:44px">{{ matchTime(p.row) || '—' }}</span>
              <div class="pred-teams">
                <span class="font-weight-bold">{{ p.row.hm }}</span>
                <span class="text-medium-emphasis mx-1">vs</span>
                <span class="font-weight-bold">{{ p.row.gt }}</span>
                <v-chip v-if="p.row.lg" size="x-small" variant="text" class="ml-1 px-1 text-medium-emphasis">{{ p.row.lg }}</v-chip>
              </div>
              <v-spacer />
              <v-chip :color="VERDICT_SIDE_COLOR[p.verdict!.side] ?? 'primary'" variant="flat" class="verdict-chip font-weight-bold">
                {{ p.verdict!.text }}
              </v-chip>
              <span class="conf-dot" :class="`lv-${p.verdict!.level}`" :title="p.verdict!.level === 'strong' ? '强' : p.verdict!.level === 'mid' ? '中' : '低置信'" />
              <span class="text-caption text-medium-emphasis edge-mini" style="width:150px">{{ p.verdict!.detail }}</span>
              <v-icon :icon="predExpanded === p.row.id ? 'mdi-chevron-up' : 'mdi-chevron-down'" size="small" />
            </div>
            <div v-if="predExpanded === p.row.id" class="pred-detail px-4 py-2">
              <span class="text-caption text-medium-emphasis mr-2">支撑信号（各榜命中它才触发）：</span>
              <span v-for="(rec, ri) in p.recs" :key="ri" class="d-inline-flex align-center ga-1 mr-3">
                <v-chip :color="sideColor(rec.act)" :variant="rec.act === '防' ? 'tonal' : 'flat'" size="x-small">{{ rec.act }}{{ rec.label }}</v-chip>
                <span class="text-caption">{{ fmt(rec.rate) }}<span class="text-medium-emphasis">/验{{ fmt(rec.testRate) }}·{{ rec.ruleCount }}规则·n{{ rec.sample }}</span></span>
              </span>
            </div>
          </template>
        </template>
      </v-window-item>
      </v-window>
    </template>
  </div>
</template>

<style scoped>
.side-chip { min-width: 96px; justify-content: center; }
.robust {
  display: inline-block;
  width: 9px; height: 9px; border-radius: 50%;
  background: rgb(var(--v-theme-success));
}
.finding-row { cursor: pointer; }
.finding-row:hover { background: rgba(var(--v-theme-primary), 0.05); }
.finding-row.open { background: rgba(var(--v-theme-primary), 0.08); }
.detail-row > td { border-bottom: none; }
.match-list {
  background: rgba(var(--v-border-color), 0.05);
  max-height: 380px;
  overflow-y: auto;
}
.inner-table { background: transparent; }
.score { font-variant-numeric: tabular-nums; }
.num { font-variant-numeric: tabular-nums; }
.date-header {
  font-weight: 700; font-size: 0.85rem;
  background: rgba(var(--v-theme-primary), 0.06);
  border-top: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
  border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity));
}
.match-head { min-width: 200px; }
.pred-row { cursor: pointer; border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); transition: background .12s; }
.pred-row:hover { background: rgba(var(--v-theme-primary), 0.04); }
.pred-teams { min-width: 220px; }
.verdict-chip { font-size: 0.9rem; }
.edge-mini { text-align: right; white-space: nowrap; }
.conf-dot { width: 10px; height: 10px; border-radius: 50%; flex: none; }
.conf-dot.lv-strong { background: rgb(var(--v-theme-success)); }
.conf-dot.lv-mid { background: rgb(var(--v-theme-primary)); }
.conf-dot.lv-soft { background: rgb(var(--v-theme-warning)); }
.pred-detail { background: rgba(var(--v-border-color), 0.05); border-bottom: 1px solid rgba(var(--v-border-color), var(--v-border-opacity)); }
</style>
