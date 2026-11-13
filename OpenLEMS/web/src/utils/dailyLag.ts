/**
 * 日频指标的空态分级。
 *
 * 为什么必须分级：采集停了 10 天时页面还在说「官方还没发，等着就行」，等于把
 * 采集挂了说成正常节奏 —— 和 homemarket 里 stopped 误报是同一类错误。判据只有
 * lag 一个数，但处置完全不同：
 *
 *   never  库里近一年没有日频指标（空库，board.date 回落成今天 ⇒ lag 恒为 0）
 *   t2     在正常发布节奏内（日前 T+0、实时腿 T+2），等就行
 *   lag    超出正常节奏，要留意
 *   stop   > 7 天，采集多半停了，去查采集
 *
 * **两条腿的正常滞后不一样**（tools/gdpm/gdpm_sqlite.py 的 report_of()：「实时是
 * T+2 发布；日前与日报同日」）：日前当日发布，lag=1 就已经是「昨天没入库」；实时
 * 腿 T+2 发布，lag=2 才是正常。用一个阈值必然把其中一条腿说错 —— 日前 lag=2 会
 * 被当成正常节奏，实为漏了两天。
 */
export type DailyTier = 'never' | 't2' | 'lag' | 'stop' | 'unknown'

/** 日频指标的两条腿。缺省 ahead：MarketView 的 board.date 取的就是日前运行日。 */
export type MarketLeg = 'ahead' | 'real'

/** 各腿的正常滞后上限：日前 T+0、实时 T+2 */
const NORMAL_LAG: Record<MarketLeg, number> = { ahead: 1, real: 2 }

export function dailyTier(lag: number, dailyEmpty: boolean, leg: MarketLeg = 'ahead'): DailyTier {
  // board.date 在库里没有任何日前数据时回落成查询末日（spotmarket/board.go），
  // 于是 lag === 0。空库不是「还没发布」，是从来没采到数。
  if (dailyEmpty && lag === 0) return 'never'
  if (lag < 0) return 'unknown'
  if (lag <= NORMAL_LAG[leg]) return 't2'
  if (lag <= 7) return 'lag'
  return 'stop'
}
