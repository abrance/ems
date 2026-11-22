import { api } from './client'

/**
 * TimesFM 预测服务（地址由部署配置提供，后端转发）的两个入口。
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
  /** 仅 intraday / hour：HH:00，便于按天分组绘图（day 粒度不下发） */
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
  /** 数据源：spot_metric=官方日频 / home24h=官网主页 24 点 / gd_metric_csv=第三方 96 点（已停更） */
  source: string
  /** day=日均 / hour=24 点 / intraday=96 点 */
  grain: string
  /** 目标日（仅 hour）：预测终止日，可能晚于锚点 to */
  targetDate?: string
  /** 采样步长（分钟）：day=1440，hour=60，intraday=15 */
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
  /** 仅 hour：目标日 24 点实际值；该日不满 24 点时为 [] */
  actual?: number[]
  contextPoints: number
  inputTruncated: boolean
  /** 送入模型的通道数：1=只用本市场历史；实时电价=3（实时价+日前价+负荷） */
  inputChannels?: number
  missingDays: number
  timeMs: number
  /** 仅 hour + compare：第二算法（modelman price-gbm / LightGBM）对同一目标日的预测 */
  gbm?: GbmForecast
  /** 对比算法不可用的原因（未配置/调用失败）。有它时 forecast 仍然有效 */
  gbmError?: string
}

/** 第二算法结果，与 forecast 同构，可并排画也能逐点算误差 */
export interface GbmForecast {
  model: string
  targetDate: string
  /** 目标日距锚点天数 */
  horizonDays: number
  /** 送入的连续完整日数 */
  historyDays: number
  forecast: PriceForecastPoint[]
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
    actual: f.actual ?? [],
  }
}

export function fetchPriceForecast(
  params: {
    market?: string
    /** 数据源；与 grain 交叉校验（不传则按 grain 反推，见后端 ParsePriceSource） */
    source?: 'spot_metric' | 'home24h' | 'gd_metric_csv'
    /** day=日均 / hour=24 点 / intraday=96 点 */
    grain?: string
    /** 目标日 YYYY-MM-DD（仅 grain=hour，默认今天） */
    targetDate?: string
    historyDays?: number
    horizonDays?: number
    quantiles?: string
    forceRefresh?: boolean
    /** 仅 grain=hour：额外跑 GBM（price-gbm 服务）做算法对比 */
    compare?: boolean
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

/**
 * 算法回测（留痕口径）：读每天 00:05 写下的 hour 留痕，按 algo 与实际值配对。
 * 传播口径：fusion 由后端用 timesfm / gbm 的点预测均值实时算，不落库、没有分位带。
 */
export interface AlgoReviewSummary {
  algo: string
  label: string
  days: number
  pairs: number
  mae: number
  rmse: number
  /** %，仅 |实际| >= 1 的点；没有可用点时为 0 */
  mape: number
  mapeExcluded: number
  /** 平均有符号误差（预测-实际），正=偏高 */
  bias: number
  /** P10~P90 覆盖率（%）；融合没有分位带，为 null */
  coverage: number | null
}

export interface AlgoReviewDaily {
  algo: string
  date: string
  pairs: number
  mae: number
  mape: number
  mapeExcluded: number
  coverage: number | null
}

export interface AlgoReviewHourly {
  algo: string
  hour: number
  pairs: number
  mae: number
  mape: number
}

export interface AlgoReviewPoint {
  algo: string
  hour: number
  targetTime: string
  predicted: number
  actual: number | null
  q10: number
  q50: number
  q90: number
}

export interface AlgoReview {
  market: string
  unit: string
  from: string
  to: string
  algos: string[]
  summary: AlgoReviewSummary[]
  daily: AlgoReviewDaily[]
  hourly: AlgoReviewHourly[]
  detail: AlgoReviewPoint[]
  dropped: { noActual: number; backfilled: number; incompleteDay: number }
  dataAsOf: string
  /** 实际值滞后天数；-1 表示拿不到 */
  lagDays: number
  truthSource: string
}

export function fetchAlgoReview(
  params: { from: string; to: string; market?: string; algos?: string; date?: string },
  signal?: AbortSignal,
): Promise<AlgoReview> {
  return api.get<AlgoReview>('/api/forecast/algo-review', params, signal).then((r) => ({
    ...r,
    algos: r.algos ?? [],
    summary: r.summary ?? [],
    daily: r.daily ?? [],
    hourly: r.hourly ?? [],
    detail: r.detail ?? [],
    dropped: r.dropped ?? { noActual: 0, backfilled: 0, incompleteDay: 0 },
  }))
}

/** 主动运行一次小时预测并写留痕；与看统计解耦。 */
export interface AlgoReviewRunResult {
  days: number
  written: Record<string, number>
  skipped: string[]
}

export function runAlgoReview(
  params: { from: string; to: string; market?: string },
  signal?: AbortSignal,
): Promise<AlgoReviewRunResult> {
  return api.post<AlgoReviewRunResult>('/api/forecast/algo-review/run', params, signal).then((r) => ({
    ...r,
    written: r.written ?? {},
    skipped: r.skipped ?? [],
  }))
}
