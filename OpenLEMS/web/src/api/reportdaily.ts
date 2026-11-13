import { api } from './client'

/**
 * 日报核对（后端 logic/reportdaily）。
 *
 * 与 spotreport.ts 不是一路：那组是「一份日报的完整提取 + 人工复核」（写 gd_spot_metric），
 * 这里读的是独立的日报原值表 gd_report_daily（左列）+ 官方直采 pm-api（右列）。
 * 两边不覆盖、不合并、无 fallback —— 所以这页只读，没有任何写回按钮。
 *
 * compare 三档由**后端**定，前端不判：
 *   diff            真同义（只有日前/实时发电侧均价两行），有 diff / diffPct
 *   caliber         口径不同（申报均价：简单 vs 加权），只并排
 *   official_missing 官网无此口径（分电源、申报结构），只并排
 */

export type CompareKind = 'diff' | 'caliber' | 'official_missing'

export interface ReconcileItem {
  /** day_ahead_trade / realtime_trade / day_ahead_decl：区分同 scope+metric 的两行 */
  dataset: string
  scope: string
  metric: string
  /** 单位由后端给，前端不猜 */
  unit: string
  source: string
  /** null = 没有（该日无日报 / 官网无此口径）。**不是 0** */
  reportValue: number | null
  officialValue: number | null
  diff: number | null
  diffPct: number | null
  compare: CompareKind
  note: string
}

export interface ReconcileDay {
  runDate: string
  reportDate: string
  missingReport: boolean
  items: ReconcileItem[]
}

export interface Reconcile {
  asOfReport: string
  asOfOfficial: string
  officialOrigin: string
  from: string
  to: string
  days: ReconcileDay[]
  warnings: string[]
}

/** GET /api/report-daily/reconcile */
export function fetchReportReconcile(
  params: { from?: string; to?: string },
  signal?: AbortSignal,
): Promise<Reconcile> {
  // Go 的 nil 切片序列化成 null，页面里 .map 会直接抛 —— 在 api 层统一收口
  return api.get<Reconcile>('/api/report-daily/reconcile', { from: params.from, to: params.to }, signal).then((r) => ({
    ...r,
    days: (r?.days ?? []).map((d) => ({ ...d, items: d?.items ?? [] })),
    warnings: r?.warnings ?? [],
  }))
}

/** 数据集 → 中文行标签：日前/实时两行 scope+metric 完全相同，只能靠它区分 */
export function datasetLabel(dataset: string): string {
  if (dataset === 'day_ahead_trade') return '日前成交'
  if (dataset === 'realtime_trade') return '实时成交'
  if (dataset === 'day_ahead_decl') return '日前申报'
  return dataset
}
