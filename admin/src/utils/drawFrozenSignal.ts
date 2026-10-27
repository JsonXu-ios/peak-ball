import type { DrawObservation } from '@/types/drawHistory'

export function frozenDrawPresentation(row: Pick<DrawObservation, 'frozenDrawSignal' | 'settled' | 'regulationSettled' | 'homeScore' | 'awayScore'>) {
  const signal = row.frozenDrawSignal
  const reason = signal?.reason || ''
  if (!signal) return { label: '暂无模型数据', color: undefined, detail: '当前接口未返回定型模型结果' }
  if (signal.state === 'out_of_scope') return { label: '非竞彩', color: undefined, detail: reason || '该模型仅观察有竞彩编号的比赛' }
  if (signal.state === 'unavailable') return { label: '数据不足', color: undefined, detail: reason || '本场缺少模型所需输入' }
  if (signal.state === 'not_selected') return { label: '未入选', color: undefined, detail: reason }
  if (signal.state !== 'selected') return { label: '暂无模型数据', color: undefined, detail: '模型状态无法识别' }
  // Only the model state determines selection. Results settle selected rows later.
  if (!row.settled || row.regulationSettled !== true || !Number.isFinite(row.homeScore) || !Number.isFinite(row.awayScore)) {
    return { label: '入选 · 待赛果', color: 'primary', detail: reason }
  }
  return row.homeScore === row.awayScore
    ? { label: '命中平局', color: 'success', detail: reason }
    : { label: '入选 · 未打平', color: 'warning', detail: reason }
}
