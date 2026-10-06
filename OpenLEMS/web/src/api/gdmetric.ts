import { api } from './client'

/**
 * 电网负荷与现货电价日指标（后端 GET /api/gd-metric，来源 logic/gdmetric）。
 * 这是现网唯一「已经有数据」的市场侧接口，信息披露先用它把架子立起来。
 */
export interface SeriesStat {
  avg: number
  max: number
  min: number
  last: number
  count: number
  unit: string
}

export interface DayRow {
  date: string
  load: SeriesStat
  dayAhead: SeriesStat
  realtime: SeriesStat
}

export interface Sample {
  time: string
  value: number
}

export interface DayCurve {
  date: string
  load: Sample[]
  dayAhead: Sample[]
  realtime: Sample[]
}

export interface GdMetricSnapshot {
  latestDate: string
  from: string
  to: string
  date: string
  source: string
  updatedAt: string
  availableDays: string[]
  latest: DayRow | null
  days: DayRow[]
  curve: DayCurve | null
}

export function fetchGdMetric(params: {
  from?: string
  to?: string
  date?: string
  signal?: AbortSignal
}): Promise<GdMetricSnapshot> {
  return api.get<GdMetricSnapshot>(
    '/api/gd-metric',
    { from: params.from, to: params.to, date: params.date },
    params.signal,
  )
}
