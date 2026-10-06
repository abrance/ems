import { api } from './client'

/**
 * 储能效果 / 算法回测。策略「试运行」复用同一套成本模型，
 * 不另造一套估算 —— 否则同一个策略在两个页面会得出两个收益。
 */
export interface BacktestAlgorithmResult {
  algorithm: string
  chargeEnergy: number
  dischargeEnergy: number
  chargeCost: number
  dischargeRevenue: number
  arbitrageProfit: number
  actualMaxDemand: number
  baselineMaxDemand: number
  demandSaving: number
  totalSaving: number
  finalSocPct: number
}

/** 实测效果汇总（峰谷套利 + 需量节省） */
export interface EffectSummary {
  start: string
  end: string
  points: number
  algorithm: string
  gridDeviceId: string
  essDeviceIds: string[]
  demandPrice: number
  chargeEnergy: number
  dischargeEnergy: number
  chargeCost: number
  dischargeRevenue: number
  arbitrageProfit: number
  actualMaxDemand: number
  baselineMaxDemand: number
  demandSaving: number
  totalSaving: number
}

export interface EffectBacktest {
  start: string
  end: string
  points: number
  gridDeviceId: string
  essDeviceIds: string[]
  demandTarget: number
  demandPrice: number
  ratedPowerKw: number
  capacityKwh: number
  /** 首个为 idle 基线 */
  results: BacktestAlgorithmResult[]
}

/** GET /api/effect/summary → { summary } */
export function fetchEffectSummary(
  params: { start: string; end: string },
  signal?: AbortSignal,
): Promise<EffectSummary> {
  return api
    .get<{ summary: EffectSummary }>(
      '/api/effect/summary',
      { start: params.start, end: params.end },
      signal,
    )
    .then((r) => r.summary)
}

export function fetchBacktest(
  params: { start: string; end: string },
  signal?: AbortSignal,
): Promise<EffectBacktest> {
  // 后端响应是 { backtest: {...} }（GetEffectBacktestRes），别把外层一起当成结果
  return api
    .get<{ backtest: EffectBacktest }>(
      '/api/effect/backtest',
      { start: params.start, end: params.end },
      signal,
    )
    .then((r) => r.backtest)
}
