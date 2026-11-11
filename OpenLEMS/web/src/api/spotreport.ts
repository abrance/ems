import { api } from './client'

/**
 * 广东电力现货运行日报：采集 / 解析 / 复核。
 *
 * 这是 overlay（spot-overlay.js）能力的迁入点 —— 采集台、档案列表、结构化数据、
 * 复核与修订、重跑，都在这里。接口沿用现网，没有新增后端。
 *
 * 口径提醒：后端把 manual（人工确认）与 pm-api（官方直采）都算已确认（s_db isConfirmedOrigin）；
 * 本文件的 isConfirmedOrigin 与它同口径，isPending = 取反，页面里只能引用这两处。
 * void（被 pm-api 取代的 OCR 旧值）两边都不返回、也不统计，既不算已确认也不算未复核。
 * 未复核数据不参与趋势聚合（见 /api/spot-report/trend）。
 */
export interface SpotPage {
  page: number
  label: string
  url: string
  key: string
  width: number
  height: number
}

export interface SpotReport {
  date: string
  title: string
  sourceUrl: string
  pageCount: number
  pages: SpotPage[]
  collectedAt: string
  pendingCount: number
  confirmedCount: number
}

export interface SpotMetric {
  runDate: string
  dataset: string
  scope: string
  metric: string
  value: number
  unit: string
  atTime: string
  textValue: string
  momPct: number | null
  sharePct: number | null
  source: string
  conf: number
  origin: string
  editedBy: string
  editedAt: string
}

export interface SpotExtractInfo {
  status: string
  ocrModel: string
  pageCount: number
  ocrMs: number
  warnings: string[]
  updatedAt: string
  extractorVersion: string
}

export interface SpotMetricAudit {
  runDate: string
  dataset: string
  scope: string
  metric: string
  oldValue: number | null
  newValue: number | null
  operation: string
  operator: string
  createdAt: string
}

export interface BatchStatus {
  id: string
  status: 'running' | 'done' | 'failed'
  total: number
  done: number
  succeeded: number
  failed: number
  overwritten: number
  current: string
  skippedConfirmed: string[]
  errors: string[]
  startedAt: string
  finishedAt: string
}

/** 数据集展示顺序与名称（后端 dataset 取值固定这几种） */
export const DATASET_LABELS: Record<string, string> = {
  summary: '正文摘要指标',
  day_ahead_trade: '日前成交电价',
  realtime_trade: '实时成交电价',
  day_ahead_decl: '日前申报信息',
  node_price_peak: '高峰节点电价档位',
  node_price_valley: '低谷节点电价档位',
}

/** 已确认判定只留这一处，与后端 spotreport.IsConfirmed / s_db isConfirmedOrigin 同口径 */
export function isConfirmedOrigin(origin: string): boolean {
  return origin === 'manual' || origin === 'pm-api'
}

/** 未复核判定只留这一处，别在页面里各写一遍。void 已作废，不属任何队列 */
export function isPending(m: Pick<SpotMetric, 'origin'>): boolean {
  return m.origin !== 'void' && !isConfirmedOrigin(m.origin)
}

export function listReports(signal?: AbortSignal): Promise<SpotReport[]> {
  return api.get<{ list: SpotReport[] }>('/api/spot-report', undefined, signal).then((r) => r.list ?? [])
}

export function getReportData(
  date: string,
  signal?: AbortSignal,
): Promise<{ date: string; tradeDate: string; metrics: SpotMetric[]; extract: SpotExtractInfo | null }> {
  return api.get('/api/spot-report/data', { date }, signal)
}

/** 趋势点：一个桶内一条序列的聚合结果 */
export interface SpotTrendPoint {
  bucket: string
  from: string
  to: string
  dataset: string
  scope: string
  metric: string
  avg: number
  max: number
  min: number
  sum: number
  days: number
  unit: string
  /** 仅申报容量类：Σ容量 ÷ Σ申报容量 × 100（桶内重算） */
  sharePct?: number
  /** 仅地市档位类：档位序号 / 区间文本 / 命中该档位的城市-天数 */
  value: number
  textValue?: string
  count?: number
}

export interface SpotTrend {
  from: string
  to: string
  grain: string
  points: SpotTrendPoint[]
  /** 区间内因未复核被排除的指标条数 */
  excludedPending: number
}

export function fetchTrend(
  params: { from?: string; to?: string; grain?: string },
  signal?: AbortSignal,
): Promise<SpotTrend> {
  return api.get<SpotTrend>('/api/spot-report/trend', params, signal)
}

export function listAudit(date: string, limit = 50, signal?: AbortSignal): Promise<SpotMetricAudit[]> {
  return api
    .get<{ list: SpotMetricAudit[] }>('/api/spot-report/audit', { date, limit }, signal)
    .then((r) => r.list ?? [])
}

/** POST /api/spot-report/collect → { report } */
export function collectReport(url: string): Promise<SpotReport> {
  return api.post<{ report: SpotReport }>('/api/spot-report/collect', { url }).then((r) => r.report)
}

/** POST /api/spot-report/extract → 扁平响应 */
export function extractReport(
  date: string,
  force: boolean,
): Promise<{ metricCount: number; status: string; warnings: string[]; preserved: number; pending: number }> {
  return api.post('/api/spot-report/extract', { date, force })
}

export function confirmMetrics(date: string, dataset = '', operator = ''): Promise<number> {
  return api
    .post<{ confirmed: number }>('/api/spot-report/confirm', { date, dataset, operator })
    .then((r) => r.confirmed)
}

export function updateMetric(params: {
  date: string
  runDate: string
  dataset: string
  scope: string
  metric: string
  value: number
  operator?: string
}): Promise<SpotMetric> {
  return api.post<{ metric: SpotMetric }>('/api/spot-report/metric', params).then((r) => r.metric)
}

export function batchExtract(params: {
  from?: string
  to?: string
  includeConfirmed?: boolean
  dryRun?: boolean
}): Promise<{ id: string; total: number; status: string; dates: string[]; skippedConfirmed: string[] }> {
  return api.post('/api/spot-report/extract/batch', params)
}

export function batchStatus(id = ''): Promise<BatchStatus> {
  return api.get<BatchStatus>('/api/spot-report/extract/batch/status', { id })
}
