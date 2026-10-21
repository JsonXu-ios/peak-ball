import api from './request'
import type { DrawObservationQuery, DrawObservationsResponse } from '@/types/drawHistory'

export function getDrawObservations(params: DrawObservationQuery) {
  return api.get<DrawObservationsResponse>('/admin/statistics/draw/observations', { params, timeout: 120000 })
}
