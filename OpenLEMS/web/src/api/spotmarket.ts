import { api } from './client'

/**
 * 广东电力市场官方指标（广东电力交易中心 pm.gd.csg.cn 直采，origin=pm-api）。
 *
 * 这是**唯一权威源**：OCR 日报旧值（origin=void）与第三方 gdmetric CSV 都不再作为
 * 市场数据的依据。口径见 tools/gdpm/README.md。
 *
 * 一个接口拿全页：日频全套 + 月频年频全套 + 后端算好的环比/同比/峰谷价差。
 * 拆分多次请求会出现「同一天的各指标来自不同时刻」的拼盘，所以刻意做成快照。
 */
export interface SeriesPoint {
  /** 日 2026-11-11 / 月 2026-10-01 / 年 2025-01-01（月频年频是期初第一天） */
  period: string
  value: number
}

export interface MarketSeries {
  dataset: string
  scope: string
  metric: string
  unit: string
  grain: 'day' | 'month' | 'year'
  /** 期次升序 */
  points: SeriesPoint[]
  latest: SeriesPoint
  /** 环比（%）；上一期不存在或不相邻时为 null —— 不是 0 */
  momPct: number | null
  /** 同比（%）；去年同期没有点时为 null */
  yoyPct: number | null
  peak: number
  valley: number
  /** 峰谷价差 = peak - valley */
  spread: number
  mean: number
  days: number
}

export interface Coverage {
  dataset: string
  from: string
  to: string
  periods: number
  /** 仅日频：区间内缺的日期 */
  missing: string[]
  lastDate: string
}

export interface MarketBoard {
  /** 最近一个有日前数据的运行日 */
  date: string
  from: string
  to: string
  days: number
  daily: MarketSeries[]
  periodic: MarketSeries[]
  /** 日前-实时加权均价差（元/兆瓦时，发电侧） */
  aheadRealtimeSpread: SeriesPoint[]
  spreadPeak: number
  spreadValley: number
  spreadMean: number
  coverage: Coverage[]
  source: string
  updatedAt: string
}

/** GET /api/spot-market/board */
export function fetchSpotMarketBoard(
  params: { date?: string; days?: number },
  signal?: AbortSignal,
): Promise<MarketBoard> {
  // Go 的 nil 切片序列化成 null，页面里 .length/.map 会直接抛 —— 在 api 层统一收口
  return api.get<MarketBoard>('/api/spot-market/board', params, signal).then((r) => ({
    ...r,
    daily: r?.daily ?? [],
    periodic: r?.periodic ?? [],
    aheadRealtimeSpread: r?.aheadRealtimeSpread ?? [],
    // 逐条也要收口：后端只给日频算 missing，其余数据集是 nil 切片 → null
    coverage: (r?.coverage ?? []).map((c) => ({ ...c, missing: c.missing ?? [] })),
  }))
}

/** 日频数据集：日前/实时 */
export const DATASET = {
  ahead: 'day_ahead_trade',
  realtime: 'realtime_trade',
} as const

export const DATASET_LABELS: Record<string, string> = {
  day_ahead_trade: '日前现货',
  realtime_trade: '实时现货',
  mid_long_term_month: '中长期（月度）',
  mid_long_term_year: '中长期（年度）',
  green_power_month: '绿电（月度）',
  green_power_year: '绿电（年度）',
  green_power_retail: '绿电零售',
  energy_retail: '电能量零售',
  hhi: 'HHI 指标',
  top_m: 'Top-m 集中度',
}

/** 序列标签：指标名已经带「发电侧/用电侧」前缀的不重复拼 scope */
export function seriesLabel(s: Pick<MarketSeries, 'scope' | 'metric'>): string {
  if (!s.scope || s.metric.startsWith(s.scope)) return s.metric
  return `${s.scope}·${s.metric}`
}

/** 整数千分位；电量动辄百万 MWh，不格式化读不出量级 */
export function fmtNum(v: number | null | undefined, digits = 2): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  return v.toLocaleString('zh-CN', {
    minimumFractionDigits: digits,
    maximumFractionDigits: digits,
  })
}

/** 环比/同比：带符号的百分比，null 显示「—」（不是 0%） */
export function fmtPct(v: number | null | undefined): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  return `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`
}
