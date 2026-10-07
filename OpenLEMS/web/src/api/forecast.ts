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
}

/** 预测留痕：复盘的第一条腿 —— **当时**发出去的预测 */
export interface RecordedForecastPoint {
  generatedOn: string
  targetTime: string
  point: number
  q10: number
  q50: number
  q90: number
  model: string
  unit: string
  source: string
}

export interface RecordedForecast {
  points: RecordedForecastPoint[]
  generatedDates: string[]
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
  timeMs: number
}

export interface PriceForecast {
  market: string
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
    /** day=日均（日报）/ intraday=96 点（电网日指标 CSV） */
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
  params: { market?: string; from: string; to: string; sampleDays?: number; forceRefresh?: boolean },
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
