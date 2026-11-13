/**
 * 日频指标的空态分级。
 *
 * 为什么必须分级：官方披露接口是 T+2 发布，滞后 2 天是**正常节奏**；但采集停了
 * 10 天时页面还在说「实时 T+2 才发布」，等于把采集挂了说成正常节奏 —— 和
 * homemarket 里 stopped 误报是同一类错误。判据只有 lag 一个数，但处置完全不同：
 *
 *   never  库里近一年没有日频指标（空库，board.date 回落成今天 ⇒ lag 恒为 0）
 *   t2     lag ≤ 2，官方还没发，等就行
 *   lag    3~7 天，超出正常节奏，要留意
 *   stop   > 7 天，采集多半停了，去查采集
 */
export type DailyTier = 'never' | 't2' | 'lag' | 'stop' | 'unknown'

export function dailyTier(lag: number, dailyEmpty: boolean): DailyTier {
  // board.date 在库里没有任何日前数据时回落成查询末日（spotmarket/board.go），
  // 于是 lag === 0。空库不是 T+2，是从来没采到数。
  if (dailyEmpty && lag === 0) return 'never'
  if (lag < 0) return 'unknown'
  if (lag <= 2) return 't2'
  if (lag <= 7) return 'lag'
  return 'stop'
}
