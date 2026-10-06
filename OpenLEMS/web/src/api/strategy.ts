import { api } from './client'

/**
 * 储能策略（定时充放计划）。
 *
 * 与「配置 · 储能参数」的区别：
 * - 储能参数（setting policy_microgrid）是**运行中的微电网策略参数**，全局一份；
 * - 这里的储能策略是**可排期的计划**：日期范围 + 时间范围 + 24 小时功率点位表，
 *   多条并存、按优先级与时间范围命中，靠 activate 启用/停用。
 * 两者都作用于充放电，但一个是参数、一个是排期，别混。
 */
export type EStatus = 'Enable' | 'Disable'

export interface SDateRange {
  startDate: string
  endDate?: string
  isLongTerm: boolean
}

export type ETimeRangeType = 'weekday' | 'custom' | 'monthly'
export type EWeekdayType = 'workday' | 'weekend' | 'all'

export interface STimeRange {
  type: ETimeRangeType
  weekdayType?: EWeekdayType
  /** type=custom 时为星期（1-7）；type=monthly 时为每月几号（1-31） */
  customDays?: number[]
  /** 仅 type=monthly 用：月份（1-12） */
  customMonths?: number[]
}

export interface SStrategyConfig {
  /** 每个点位是 [小时 0-23, 功率百分比 0-100] */
  points: number[][]
}

export interface EnergyStorageStrategy {
  id: number
  name: string
  description?: string
  priority: number
  status: EStatus
  dateRange?: SDateRange
  timeRange?: STimeRange
  config?: SStrategyConfig
  /** 时间/日期范围当前是否命中（不含启用状态） */
  isActive: boolean
  createdAt?: string
  updatedAt?: string
  createdBy?: string
}

export interface StrategyListParams {
  page?: number
  pageSize?: number
  status?: EStatus
  priority?: number
  keyword?: string
}

export function listStrategies(
  params: StrategyListParams = {},
  signal?: AbortSignal,
): Promise<{ list: EnergyStorageStrategy[]; total: number }> {
  return api
    .get<{ list: EnergyStorageStrategy[]; total: number }>(
      '/api/strategy/energy-storage',
      {
        page: params.page,
        pageSize: params.pageSize,
        status: params.status,
        priority: params.priority,
        keyword: params.keyword,
      },
      signal,
    )
    .then((r) => ({ list: r.list ?? [], total: r.total ?? 0 }))
}

export function getStrategy(id: number, signal?: AbortSignal): Promise<EnergyStorageStrategy> {
  return api.get<EnergyStorageStrategy>(`/api/strategy/energy-storage/${id}`, undefined, signal)
}

export type StrategyPayload = Omit<
  EnergyStorageStrategy,
  'id' | 'isActive' | 'createdAt' | 'updatedAt' | 'createdBy'
>

export function createStrategy(payload: StrategyPayload): Promise<void> {
  return api.post<void>('/api/strategy/energy-storage', payload)
}

export function updateStrategy(id: number, payload: StrategyPayload): Promise<void> {
  return api.request<void>(`/api/strategy/energy-storage/${id}`, { method: 'PUT', body: payload })
}

export function deleteStrategy(id: number): Promise<void> {
  return api.request<void>(`/api/strategy/energy-storage/${id}`, { method: 'DELETE' })
}

export function activateStrategy(id: number, status: EStatus): Promise<void> {
  return api.post<void>('/api/strategy/energy-storage/activate', { id, status })
}

/** 点位表排序并去重（同一小时只保留一个值，后写覆盖先写） */
export function normalizePoints(points: number[][] | undefined): number[][] {
  const map = new Map<number, number>()
  for (const p of points ?? []) {
    if (!Array.isArray(p) || p.length < 2) continue
    const hour = Number(p[0])
    const pct = Number(p[1])
    if (!Number.isFinite(hour) || !Number.isFinite(pct)) continue
    map.set(Math.trunc(hour), pct)
  }
  return [...map.entries()].sort((a, b) => a[0] - b[0]).map(([h, v]) => [h, v])
}

export const WEEKDAY_LABELS = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
