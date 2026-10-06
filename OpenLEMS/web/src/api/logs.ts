import { api } from './client'

/**
 * 运行日志（/log/biz）。
 *
 * 注意这**不是**「操作审计」：后端只有按类型/级别运行日志，没有「谁在什么时候把哪个
 * 配置从 A 改成 B」的变更审计表。设计稿 §11.4 规矩 5 想要的那种审计需要新后端，
 * 页面上的「配置变更」一栏是空的，不拿运行日志冒充。
 */
export interface LogLine {
  createdAt: string | null
  id: string
  type: string
  level: string
  content: string
}

export interface LogPage {
  total: number
  lines: LogLine[]
}

export interface LogInfo {
  total: number
  firstLogTime: string
  latestLogTime: string
}

export interface LogStatistics {
  type: { ems: number; device: number; protocol: number; policy: number }
  level: { DEBUG: number; INFO: number; WARN: number; ERROR: number }
}

export const LOG_TYPES = [
  { value: '', label: '全部类型' },
  { value: 'ems', label: 'ems' },
  { value: 'device', label: 'device' },
  { value: 'protocol', label: 'protocol' },
  { value: 'policy', label: 'policy' },
  { value: 'remote', label: 'remote' },
]

export const LOG_LEVELS = [
  { value: '', label: '全部级别' },
  { value: 'DEBUG', label: 'DEBUG' },
  { value: 'INFO', label: 'INFO' },
  { value: 'WARN', label: 'WARN' },
  { value: 'ERROR', label: 'ERROR' },
]

export function fetchLogs(
  params: { type?: string; id?: string; date?: string; page?: number; pageSize?: number; level?: string },
  signal?: AbortSignal,
): Promise<LogPage> {
  return api.get<LogPage>('/api/log/biz', params, signal).then((r) => ({ total: r.total ?? 0, lines: r.lines ?? [] }))
}

export function fetchLogInfo(signal?: AbortSignal): Promise<LogInfo> {
  return api.get<LogInfo>('/api/log/biz/info', undefined, signal)
}

export function fetchLogStatistics(signal?: AbortSignal): Promise<LogStatistics> {
  return api.get<LogStatistics>('/api/log/biz/statistics', undefined, signal)
}

/** 删除日志：type/level 留空表示不限。后端只支持按类型 + 级别过滤，不支持按日期删。 */
export function deleteLogs(params: { type?: string; level?: string }): Promise<number> {
  return api
    .request<{ total: number }>('/api/log/biz', { method: 'DELETE', query: params })
    .then((r) => r.total ?? 0)
}
