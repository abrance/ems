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
  return api
    .get<GdMetricSnapshot>(
      '/api/gd-metric',
      { from: params.from, to: params.to, date: params.date },
      params.signal,
    )
    .then((r) => ({ ...r, days: r.days ?? [], availableDays: r.availableDays ?? [] }))
}

/** 区间 96 点序列（复盘的第二条腿：实际值） */
export interface MetricSeriesPoint {
  date: string
  time: string
  value: number
}

export interface MetricSeries {
  kind: string
  from: string
  to: string
  points: MetricSeriesPoint[]
  /** 区间内实际有数据的日期（升序），供前端灰显无数据日期 */
  days: string[]
}

export function fetchMetricSeries(
  params: { from: string; to: string; kind?: string },
  signal?: AbortSignal,
): Promise<MetricSeries> {
  return api.get<MetricSeries>('/api/gd-metric/series', params, signal).then((r) => ({
    ...r,
    points: r.points ?? [],
    days: r.days ?? [],
  }))
}

/** 反演：一个观测点（系统负荷 + 该时刻出清价） */
export interface SupplyPair {
  load: number
  price: number
}

/** 反演：一个负荷分箱上的价格分位分布 */
export interface SupplyBin {
  loadFrom: number
  loadTo: number
  loadMid: number
  count: number
  /** 价格分位；下标 i 对应概率 i/(len-1)。默认 11 个（0,0.1,…,1） */
  q: number[]
}

/** 反演报价曲线的报一段（对应参考界面的「报价段一…十」） */
export interface SupplyStep {
  loadFrom: number
  loadTo: number
  price: number
  count: number
}

export interface SupplyStats {
  pairs: number
  days: number
  loadMin: number
  loadMax: number
  priceMin: number
  priceMax: number
  /** 负荷与价格的秩相关；越接近 1 说明价格越由负荷单调决定 */
  spearman: number
  /** P50 曲线的逆序数：>0 说明供给栈在区间内整体漂移过，曲线只能当参考 */
  violations: number
  /** 单调化对报价段价格的最大改动 元/MWh */
  maxPavaMove: number
  latestLoad: number
}

/**
 * 反演出的市场聚合报价（供给）曲线。
 *
 * 原理：负荷基本刚需，出清价是需求与供给栈的交点，所以每个 (负荷, 出清价)
 * 观测点本身就是供给曲线上的一个点。按负荷分箱取价格分位即得。
 */
export interface SupplyCurve {
  market: string
  from: string
  to: string
  bins: number
  quantiles: number[]
  /** 观测散点（超过上限按等距抽样） */
  points: SupplyPair[]
  sampled: boolean
  allBins: SupplyBin[]
  steps: SupplyStep[]
  stats: SupplyStats
}

export function fetchSupplyCurve(
  params: { from: string; to: string; market?: string; bins?: number; steps?: number },
  signal?: AbortSignal,
): Promise<SupplyCurve> {
  // 归一化：nil 切片 → []（Go 的 nil 切片是 null，页面读 .length 会崩）
  return api.get<SupplyCurve>('/api/gd-metric/supply-curve', params, signal).then((r) => ({
    ...r,
    quantiles: r.quantiles ?? [],
    points: r.points ?? [],
    allBins: r.allBins ?? [],
    steps: r.steps ?? [],
  }))
}

/** 历史相似日 · 单个候选日 */
export interface SimilarDay {
  date: string
  /** 0~100 = (ρ+1)/2×100 */
  similarity: number
  priceCorr: number
  loadCorr: number
  priceAvg: number
  loadMax: number
}

/**
 * 历史相似日结果。
 *
 * 口径来自后端 `internal/logic/gdmetric/similar.go`：96 点价格/负荷形态的加权
 * 皮尔逊相关（z-score 标准化后），不是绝对价位相近。页面要把 method/formula 展示出来，
 * 别让人以为「相似」= 数字接近。
 */
export interface SimilarDaysResult {
  targetDate: string
  market: string
  topN: number
  wPrice: number
  wLoad: number
  method: string
  formula: string
  from: string
  to: string
  candidates: number
  days: SimilarDay[]
  timeMs: number
}

export function fetchSimilarDays(
  params: { date?: string; market?: string; top?: number; wPrice?: number; wLoad?: number },
  signal?: AbortSignal,
): Promise<SimilarDaysResult> {
  return api
    .get<SimilarDaysResult>('/api/gd-metric/similar-days', params, signal)
    .then((r) => ({ ...r, days: r.days ?? [] }))
}
