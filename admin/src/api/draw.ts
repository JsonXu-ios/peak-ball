import api from './request'
import type { DrawActionResponse, DrawHistoryQuery, DrawHistoryReport, DrawUpcomingReport } from '@/types/draw'

const base = '/admin/statistics/draw'

export default {
  history(params: DrawHistoryQuery) {
    return api.get<DrawHistoryReport>(`${base}/history`, { params, timeout: 300000 })
  },
  rebuild() {
    return api.post<DrawActionResponse>(`${base}/rebuild`, {}, { timeout: 300000 })
  },
  calibrate() {
    return api.post<DrawActionResponse>(`${base}/calibrate`, {}, { timeout: 300000 })
  },
  matches(date: string) {
    return api.get<DrawUpcomingReport>(`${base}/matches`, { params: { date }, timeout: 120000 })
  },
  generate(date: string) {
    return api.post<DrawActionResponse>(`${base}/generate`, { date }, { timeout: 120000 })
  },
}
