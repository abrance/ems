import { api } from './client'
import type { EStatus } from './strategy'

/**
 * 电价配置（峰平谷时段）。
 *
 * 这是**峰平谷时段的唯一来源**：策略套利、预测复盘的分时段相关度/离散度都读它。
 * 别在别处再定义一份时段 —— 两处不一致时没人知道哪个对。
 *
 * priceType 的线格式是**字符串**（后端 EPriceType 自定义了 MarshalJSON）：
 *   Valley / Peak / Flat / Sharp / DeepValley
 * 对应 common/c_enum/c_enum_price_type_e.go 的 1..5。改枚举时这里要跟着改。
 */
export const PRICE_TYPES = [
  { value: 'DeepValley', label: '深谷' },
  { value: 'Valley', label: '谷电' },
  { value: 'Flat', label: '平电' },
  { value: 'Peak', label: '峰电' },
  { value: 'Sharp', label: '尖峰' },
] as const

export type PriceType = (typeof PRICE_TYPES)[number]['value']

export const PRICE_TYPE_LABEL: Record<string, string> = Object.fromEntries(
  PRICE_TYPES.map((t) => [t.value, t.label]),
)

/** 时段配色：浅底 + 深字（不用饱和底配白字，对比过不了） */
export const PRICE_TYPE_TONE: Record<string, { bg: string; fg: string }> = {
  DeepValley: { bg: 'var(--blue-soft)', fg: 'var(--blue-ink)' },
  Valley: { bg: 'var(--green-soft)', fg: 'var(--green-ink)' },
  Flat: { bg: 'var(--gray-soft)', fg: 'var(--gray-ink)' },
  Peak: { bg: 'var(--brand-soft)', fg: 'var(--brand-ink)' },
  Sharp: { bg: 'var(--red-soft)', fg: 'var(--red-ink)' },
}

export interface PriceSegment {
  /** "HH:MM" */
  startTime: string
  /** "HH:MM"，等于 00:00 表示到当日 24:00 */
  endTime: string
  priceType: PriceType
  price: number
}

export interface PriceConfig {
  id: number
  description: string
  priority: number
  status: EStatus
  isActive: boolean
  dateRange?: { startDate: string; endDate?: string; isLongTerm: boolean }
  timeRange?: { type: string; weekdayType?: string; customDays?: number[]; customMonths?: number[] }
  priceSegments: PriceSegment[]
  remoteId?: string
  createdAt?: string
  updatedAt?: string
  createdBy?: string
}

export function listPrices(
  params: { page?: number; pageSize?: number; status?: EStatus; keyword?: string } = {},
  signal?: AbortSignal,
): Promise<{ list: PriceConfig[]; total: number }> {
  return api
    .get<{ list: PriceConfig[]; total: number }>(
      '/api/price',
      { page: params.page ?? 1, pageSize: params.pageSize ?? 100, status: params.status, keyword: params.keyword },
      signal,
    )
    .then((r) => ({ list: r.list ?? [], total: r.total ?? 0 }))
}

/** GET /api/price/current → { price } */
export function fetchCurrentPrice(signal?: AbortSignal): Promise<PriceConfig | null> {
  return api.get<{ price: PriceConfig | null }>('/api/price/current', undefined, signal).then((r) => r.price)
}

export type PricePayload = Omit<PriceConfig, 'id' | 'isActive' | 'createdAt' | 'updatedAt' | 'createdBy'>

export function createPrice(payload: PricePayload): Promise<void> {
  return api.post<void>('/api/price', payload)
}

export function updatePrice(id: number, payload: PricePayload): Promise<void> {
  return api.request<void>(`/api/price/${id}`, { method: 'PUT', body: payload })
}

export function deletePrice(id: number): Promise<void> {
  return api.request<void>(`/api/price/${id}`, { method: 'DELETE' })
}

export function togglePrice(id: number, status: EStatus): Promise<void> {
  return api.request<void>(`/api/price/${id}/toggle`, { method: 'PUT', body: { status } })
}

/* ---------------- 时段工具 ---------------- */

export function toMinutes(hhmm: string): number {
  const [h, m] = hhmm.split(':').map(Number)
  if (!Number.isFinite(h) || !Number.isFinite(m)) return NaN
  return h * 60 + m
}

/**
 * 把「结束时间」归一成分钟数：00:00 视为 24:00（一天结束）。
 * 不这么处理的话 20:00-00:00 这种跨夜写法会被判成 end < start。
 */
export function endMinutes(hhmm: string): number {
  const v = toMinutes(hhmm)
  return v === 0 ? 24 * 60 : v
}

export interface SegmentIssue {
  index: number
  level: 'error' | 'warning'
  message: string
}

/** 校验时段表：起止合法、不重叠；不铺满 24 小时只给提醒不阻断 */
export function validateSegments(segments: PriceSegment[]): SegmentIssue[] {
  const issues: SegmentIssue[] = []
  const spans: { start: number; end: number; index: number }[] = []

  segments.forEach((s, index) => {
    const start = toMinutes(s.startTime)
    const end = endMinutes(s.endTime)
    if (!Number.isFinite(start) || !Number.isFinite(end)) {
      issues.push({ index, level: 'error', message: '时间格式应为 HH:MM' })
      return
    }
    if (end <= start) {
      issues.push({ index, level: 'error', message: '结束时间必须晚于开始时间' })
      return
    }
    if (!Number.isFinite(Number(s.price)) || Number(s.price) < 0) {
      issues.push({ index, level: 'error', message: '电价必须是非负数' })
      return
    }
    spans.push({ start, end, index })
  })

  const sorted = [...spans].sort((a, b) => a.start - b.start)
  for (let i = 1; i < sorted.length; i++) {
    if (sorted[i].start < sorted[i - 1].end) {
      issues.push({
        index: sorted[i].index,
        level: 'error',
        message: `与第 ${sorted[i - 1].index + 1} 个时段重叠（${sorted[i - 1].start / 60}:00 起）`,
      })
    }
  }

  // 覆盖度：只提醒。业务上是否允许留空由用户判断，这里不擅自阻断保存。
  if (spans.length) {
    const total = sorted.reduce((sum, s) => sum + (s.end - s.start), 0)
    if (total < 24 * 60) {
      issues.push({
        index: -1,
        level: 'warning',
        message: `时段只覆盖 ${(total / 60).toFixed(1)} 小时，未覆盖的时段没有电价`,
      })
    }
  }
  return issues
}
