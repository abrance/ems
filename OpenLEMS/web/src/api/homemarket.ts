import { api } from './client'

/**
 * 官网主页两路市场数据（后端 logic/homemarket）。
 *
 * **与 gdmetric.ts 不是一路**：
 *   - gdmetric.ts 读的是第三方 CSV 的 96 点通道（已停更，默认入口 fail-closed）；
 *   - spotmarket.ts 读的是披露接口的日频指标（gd_spot_metric，日前 T+0 / 实时腿 T+2）；
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

/* ---------- 采集状态（/gd-metric/home-market/status） ---------- */

/**
 * 与 logic/homemarket/status.go 的四态一一对应。
 *
 * 这个端点**不带任何数值**，只回答「这一路现在能不能用、为什么不能用」。
 * 所以它不能反过来决定图上的数（那是 home-24h / node-price 的事），只决定
 * 空态怎么说、以及要不要用告警口气。
 */
export type MarketState = 'ok' | 'lagging' | 'failed' | 'never' | 'stopped'

export interface DatasetStatus {
  /** home24h / nodePrice */
  dataset: string
  state: MarketState
  /** 最新有数据的那一天；库里一条都没有时为空串 */
  asOf: string
  /** 最近一次成功采集的时间；从没成功过时为空串 */
  fetchedAt: string
  /** asOf 距今多少天；无数据时 -1 */
  lagDays: number
  rows: number
  rowsExpected: number
  lastError: string
  /** blocked / network / parse */
  errorKind: string
}

export interface MarketStatus {
  home24h: DatasetStatus
  nodePrice: DatasetStatus
}

/** GET /api/gd-metric/home-market/status */
export function fetchMarketStatus(signal?: AbortSignal): Promise<MarketStatus> {
  return api.get<MarketStatus>('/api/gd-metric/home-market/status', undefined, signal)
}

/** 三种失败类别处置不同：拦截要换网络/加白名单，网络异常重试，解析失败是页面改版 */
const ERROR_KIND_LABEL: Record<string, string> = {
  blocked: '被源站拦截',
  network: '网络异常',
  parse: '数据解析失败',
}

/** 状态 → 页面上的一句话，以及该用哪种口气 */
export interface StatusNote {
  /** neutral = 正常语气；alert = 需要人处理 */
  tone: 'neutral' | 'alert'
  text: string
}

/**
 * 五态里 `ok` 不需要任何提示（有数、滞后在预期内，说话只是噪音）。
 *
 * `lagging` 的语义是「最近成功过，但最新数据超出了预期滞后上限」——status.go
 * 里写得很清楚：「超了就是采集没跟上，而不是源站还没发布」。所以它必须带
 * 提醒，不能写成「等下一轮采集」，那句话会让人一直等一个不会自己好的东西。
 * 正常滞后（24 点 T-6、地市 T+0）落在 ok 里，不弹提示。
 */
export function statusNote(st?: DatasetStatus | null): StatusNote | null {
  if (!st) return null
  switch (st.state) {
    case 'lagging':
      return {
        tone: 'alert',
        text: `采集已落后：库里最新到 ${st.asOf}（${st.lagDays} 天前），超出正常滞后，请检查采集是否在跑`,
      }
    case 'failed':
      return {
        tone: 'alert',
        text: `采集失败：${ERROR_KIND_LABEL[st.errorKind] ?? '未知原因'}，最近一次成功是 ${st.fetchedAt || '—'}`,
      }
    case 'never':
      return { tone: 'alert', text: '从未采集成功，请检查采集配置' }
    case 'stopped':
      // 判据是「滞后越过预期上限」（status.go 里 stopped 的 lag 前置条件），
      // 「连续 3 次空」只是必要条件 —— 健康的 T-6 滞后每轮都会有日期返回空。
      // 按连续空轮次说，会让人去查一个没坏的地方。
      return {
        tone: 'alert',
        text: st.asOf
          ? `源站已 ${st.lagDays} 天没有发布新数据（截至 ${st.asOf}），超出正常滞后，疑似停更或页面改版`
          : '源站连续返回空数据且库里一条都没有，疑似停更或页面改版',
      }
    default:
      return null
  }
}
