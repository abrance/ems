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
    .then((r) => r.forecast)
}

/** GET /api/forecast/price → { forecast } */
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
    .then((r) => r.forecast)
}

export function fetchRecordedForecast(
  params: { from: string; to: string; market?: string; grain?: string },
  signal?: AbortSignal,
): Promise<RecordedForecast> {
  return api.get<RecordedForecast>('/api/forecast/recorded', params, signal)
}

export function fetchPriceBacktest(
  params: { market?: string; from: string; to: string; sampleDays?: number; forceRefresh?: boolean },
  signal?: AbortSignal,
): Promise<PriceBacktest> {
  return api.get<PriceBacktest>('/api/forecast/backtest', params, signal)
}
