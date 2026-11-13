import { api } from './client'

/**
 * TimesFM 预测服务（forecast.xiaoyxq.top）经后端转发的两个入口。
 *
 * 都是 **CPU 推理**（单次≈2s，服务端 max_concurrency=2），后端已加 5 分钟缓存，
 * 所以页面切换不要刻意绕过缓存：只有在用户明确点「刷新」时才带 forceRefresh。
 */
export interface LoadForecast {
  deviceId: string
  model: string
  stepMinutes: number
  lookbackPoints: number
  inputTruncated: boolean
  historyTimes: number[]
  historyValues: number[]
  forecastTimes: number[]
  forecastPoint: number[]
  /** 键为分位字符串，如 "0.5" */
  forecastQuantiles: Record<string, number[]>
  timeMs: number
}

export interface PriceForecastPoint {
  runDate: string
  point: number
  /** 键为分位字符串，如 "0.1" / "0.5" / "0.9" */
  quantiles: Record<string, number>
  /** 仅 intraday：HH:MM，便于按天分组绘图（day 粒度不下发） */
  atTime?: string
}

/** 预测留痕：复盘的第一条腿 —— **当时**发出去的预测 */
export interface RecordedForecastPoint {
  generatedOn: string
  /** day 粒度是 YYYY-MM-DD，intraday 是 YYYY-MM-DD HH:MM */
  targetTime: string
  point: number
  q10: number
  q50: number
  q90: number
  model: string
  unit: string
  source: string
  /** 官方日均实际值（与日频回测同一口径）；没有实际值时为 null */
  actual?: number | null
}

export interface RecordedForecast {
  points: RecordedForecastPoint[]
  generatedDates: string[]
  /** 真值口径与日频回测腿一致：官方直采最新实际值日 / 滞后天数 / 是否陈旧 */
  dataAsOf?: string
  lagDays?: number
  stale?: boolean
  truthSource?: string
  /** 生成日晚于目标日、按「事后回填」剔除的条数（那不是当时发出的预测） */
  backfilled?: number
  /** 有留痕但官方实际值还没出的点数 */
  actualMissing?: number
  /** 真值读不到 / 不可配对的原因（96 点没有官方分时真值） */
  actualUnavailable?: string
}

/** 回测：用**当前模型**在历史数据上重跑（≠ 当时预测，页面必须讲清） */
export interface BacktestPoint {
  targetTime: string
  predicted: number
  actual: number
  q10: number
  q50: number
  q90: number
}

export interface PriceBacktest {
  market: string
  grain: string
  unit: string
  model: string
  from: string
  to: string
  sampleDays: number
  targetDates: string[]
  skipped: string[]
  points: BacktestPoint[]
  /** 因该时刻没有实际值而丢弃的预测点数（按时间戳配对配不上的） */
  actualMissing?: number
  timeMs: number
  /** 每次 as-of 预测送入模型的历史天数（day 粒度） */
  historyDays?: number
  /** 本次用到的实际值最新日期（YYYY-MM-DD） */
  dataAsOf?: string
  /** 实际值滞后天数；-1 表示拿不到可用日期 */
  lagDays?: number
  /** 滞后超阈值：结果只代表历史，不代表近期 */
  stale?: boolean
  /** 真值来源说明（页面原样展示，别改写成营销话术） */
  truthSource?: string
}

export interface PriceForecast {
  market: string
  /** day=日均（官方直采 pm-api）/ intraday=96 点（电网日指标 CSV） */
  grain: string
  /** 采样步长（分钟）：day=1440，intraday=15 */
  stepMinutes: number
  dataset: string
  scope: string
  metric: string
  unit: string
  model: string
  from: string
  to: string
  historyDates: string[]
  historyValues: number[]
  forecast: PriceForecastPoint[]
  contextPoints: number
  inputTruncated: boolean
  /** 送入模型的通道数：1=只用本市场历史；实时电价=3（实时价+日前价+负荷） */
  inputChannels?: number
  missingDays: number
  timeMs: number
}

/** GET /api/forecast/load → { forecast } */
export function fetchLoadForecast(
  params: { deviceId?: string; lookbackHours?: number; historyHours?: number; horizon?: number; forceRefresh?: boolean },
  signal?: AbortSignal,
): Promise<LoadForecast> {
  return api
    .get<{ forecast: LoadForecast }>('/api/forecast/load', params, signal)
    .then((r) => ({
      ...r.forecast,
      historyTimes: r.forecast?.historyTimes ?? [],
      historyValues: r.forecast?.historyValues ?? [],
      forecastTimes: r.forecast?.forecastTimes ?? [],
      forecastPoint: r.forecast?.forecastPoint ?? [],
    }))
}

/** GET /api/forecast/price → { forecast } */
/** 归一化：nil 切片 → []（见 fetchPriceBacktest 的注释） */
function normalizeForecast(f: PriceForecast): PriceForecast {
  return {
    ...f,
    historyDates: f.historyDates ?? [],
    historyValues: f.historyValues ?? [],
    forecast: f.forecast ?? [],
  }
}

export function fetchPriceForecast(
  params: {
    market?: string
    /** day=日均（官方直采 pm-api）/ intraday=96 点（电网日指标 CSV） */
    grain?: string
    historyDays?: number
    horizonDays?: number
    quantiles?: string
    forceRefresh?: boolean
  },
  signal?: AbortSignal,
): Promise<PriceForecast> {
  return api
    .get<{ forecast: PriceForecast }>('/api/forecast/price', params, signal)
    .then((r) => normalizeForecast(r.forecast))
}

export function fetchRecordedForecast(
  params: { from: string; to: string; market?: string; grain?: string },
  signal?: AbortSignal,
): Promise<RecordedForecast> {
  return api.get<RecordedForecast>('/api/forecast/recorded', params, signal).then((r) => ({
    ...r,
    points: r.points ?? [],
    generatedDates: r.generatedDates ?? [],
  }))
}

export function fetchPriceBacktest(
  params: {
    market?: string
    /** day（默认，官方日频日均价）/ intraday（96 点，第三方通道已停更，仅历史演示） */
    grain?: 'day' | 'intraday'
    from: string
    to: string
    sampleDays?: number
    forceRefresh?: boolean
    /** 真值滞后超阈值时仍要结果（只有 grain=intraday 的历史演示需要） */
    allowStale?: boolean
  },
  signal?: AbortSignal,
): Promise<PriceBacktest> {
  // 归一化数组字段：**Go 的 nil 切片序列化成 null**（不是 []），页面里 `.length` 会直接抛，
  // 而组件 setup 一抛就是整页空白（预测复盘页线上就是这么白的）。
  // 统一在 api 层收口，别让每个页面各自记得加 ?. 和 ?? []。
  return api.get<PriceBacktest>('/api/forecast/backtest', params, signal).then((r) => ({
    ...r,
    targetDates: r.targetDates ?? [],
    skipped: r.skipped ?? [],
    points: r.points ?? [],
  }))
}
