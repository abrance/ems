import { api } from './client'

/**
 * 分时研究（第三方 · 非官方）：96 点（15 分钟）日前电价。
 *
 * 数据源是**第三方冻结快照**（`gd_metric.csv`，实际值止于 2026-10-02），
 * 官方披露接口没有 96 点粒度（见 docs/data-source-matrix.md §2）。
 * 因此：
 *   - 这一页只读，不采集、不刷新、不定时（`GET /api/research/intraday` 无副作用）；
 *   - 页头必须常驻「第三方 · 非官方 / 截至 X」；
 *   - **不参与复盘 MAE**：复盘页的口径是官方日频，这一页是研究视图。
 *
 * `asOf` / `stale` 一律由后端给，前端不重算（一图一源一 asOf）。
 */
export interface IntradayResearchSource {
  id: string
  name: string
  /** official / thirdparty */
  kind: string
}

export interface IntradayResearchPoint {
  /** HH:MM，00:15 … 24:00 */
  time: string
  /** 元/兆瓦时 */
  value: number
}

export interface IntradayResearchDay {
  date: string
  points: IntradayResearchPoint[]
}

export interface IntradayResearch {
  source: IntradayResearchSource
  /** 最后一天有实际值的日期；空串 = 本地完全没有数据 */
  asOf: string
  /** asOf 距今自然日数，-1 = 未知 */
  lagDays: number
  /** 服务端判定：lagDays > 7 */
  stale: boolean
  from: string
  to: string
  /** 只有有数据的日期 */
  days: IntradayResearchDay[]
  /** 区间内没有入库的日期：页面渲染空态，不画 0 线、不把缺口连成直线 */
  missing: string[]
  available: string[]
}

export function fetchIntradayResearch(
  params: { from?: string; to?: string },
  signal?: AbortSignal,
): Promise<IntradayResearch> {
  // Go 的 nil 切片序列化成 null，页面里 .map/.length 会直接抛 —— 在 api 层统一收口
  return api.get<IntradayResearch>('/api/research/intraday', params, signal).then((r) => ({
    ...r,
    days: (r?.days ?? []).map((d) => ({ ...d, points: d.points ?? [] })),
    missing: r?.missing ?? [],
    available: r?.available ?? [],
    source: r?.source ?? { id: '', name: '', kind: 'thirdparty' },
  }))
}
