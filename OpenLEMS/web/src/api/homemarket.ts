import { api } from './client'

/**
 * 官网主页两路市场数据（后端 logic/homemarket）。
 *
 * **与 gdmetric.ts 不是一路**：
 *   - gdmetric.ts 读的是第三方 CSV 的 96 点通道（已停更，默认入口 fail-closed）；
 *   - spotmarket.ts 读的是披露接口的日频指标（gd_spot_metric，T+2）；
 *   - 这里读的是主页的 24 个整点（T-6 左右）与 21 地市节点价（T+0）。
 *
 * 三者的粒度与滞后都不同，所以是三张表、三个接口，绝不混用：
 * 混了就会出现「同一个页面两个截止日」这种让人没法判断新鲜度的情况。
 */

/** 24 点里的一个整点 */
export interface Home24hPoint {
  hour: number
  /** 系统负荷 MW */
  powerMw: number
  /** 日前电价 元/兆瓦时 */
  aheadPrice: number
  /** 实时电价 元/兆瓦时 */
  realtimePrice: number
}

export interface Home24hDay {
  date: string
  /** 该日库里没有数据。源站 T-6 左右才发布，这是正常状态而不是错误 */
  empty: boolean
  /** 库里最近一个有数据的日期，页面靠它提示「最新到哪天」 */
  latestDate: string
  /** 相对今天滞后几天；无数据时 -1 */
  lagDays: number
  points: Home24hPoint[]
  source: string
  /** 该日实际落库的点数（正常 24）。缺点的日子不能拿来当全天统计 */
  pointCount: number
  // 下面四个只在满 24 点时后端才下发（缺点日 omitempty）——所以是可选，页面必须用 pointCount 说明原因
  meanAhead?: number
  meanRealtime?: number
  peakPowerMw?: number
  lowPowerMw?: number
}

/** GET /api/gd-metric/home-24h?date= */
export function fetchHome24h(params: { date?: string }, signal?: AbortSignal): Promise<Home24hDay> {
  // Go 的 nil 切片序列化成 null，页面里 .map 会直接抛 —— 在 api 层统一收口
  return api.get<Home24hDay>('/api/gd-metric/home-24h', { date: params.date }, signal).then((r) => {
    const points = r?.points ?? []
    return {
      ...r,
      points,
      // 老后端没这个字段：按实际点数兜底，缺点的日子仍然会被页脚点出来，而不是显示「峰值 —」
      pointCount: r?.pointCount ?? points.length,
    }
  })
}

/** 地市节点价的一个点 */
export interface NodePricePoint {
  city: string
  /** 1 高峰 / 2 低谷 */
  periodType: number
  price: number
}

export interface NodePriceDay {
  date: string
  empty: boolean
  latestDate: string
  lagDays: number
  points: NodePricePoint[]
  source: string
  peakCities: number
  valleyCities: number
}

/** GET /api/gd-metric/node-price?date= */
export function fetchNodePrice(params: { date?: string }, signal?: AbortSignal): Promise<NodePriceDay> {
  return api.get<NodePriceDay>('/api/gd-metric/node-price', { date: params.date }, signal).then((r) => ({
    ...r,
    points: r?.points ?? [],
  }))
}

/** 时段标签：源站 type 1=高峰 2=低谷 */
export function periodLabel(type: number): string {
  if (type === 1) return '高峰'
  if (type === 2) return '低谷'
  return `时段 ${type}`
}
