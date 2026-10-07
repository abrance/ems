import { api } from './client'

/**
 * 告警（/api/alarm）。后端一直有告警能力，但前端此前没有任何告警界面 ——
 * 这是「运行监控」Tab 里最实的一块。
 */
export interface CurrentAlarm {
  sourceDeviceId: string
  sourceDeviceName: string
  deviceId: string
  deviceName: string
  point: string
  pointName: string
  level: string
  detail: string
  createdAt: string | null
}

export interface HistoryAlarm {
  id: number
  deviceId: string
  deviceName: string
  sourceDeviceId: string
  sourceDeviceName: string
  point: string
  pointName: string
  level: string
  detail: string
  triggerAt: string | null
  clearAt: string | null
}

export interface IgnoreAlarm {
  id: number
  deviceId: string
  deviceName: string
  sourceDeviceId: string
  sourceDeviceName: string
  point: string
  pointName: string
  createdAt: string | null
}

export interface CurrentAlarmsResult {
  total: number
  /** 被忽略规则过滤掉的数量 */
  ignoreTotal: number
  historyTotal: number
  items: CurrentAlarm[]
}

export interface AlarmPage<T> {
  total: number
  items: T[]
}

/**
 * 告警级别。后端取值是 None/Warn/Alarm/Error（大小写敏感），
 * 与日志级别不是一套 —— 别复用日志的颜色表。
 */
export const ALARM_LEVELS = [
  { value: '', label: '全部级别' },
  { value: 'Warn', label: '警告' },
  { value: 'Alarm', label: '告警' },
  { value: 'Error', label: '故障' },
] as const

export const ALARM_LEVEL_TAG: Record<string, { label: string; tone: 'gray' | 'brand' | 'red' }> = {
  None: { label: '正常', tone: 'gray' },
  Warn: { label: '警告', tone: 'brand' },
  Alarm: { label: '告警', tone: 'red' },
  Error: { label: '故障', tone: 'red' },
}

export function fetchCurrentAlarms(
  params: { deviceId?: string; level?: string; page?: number; pageSize?: number },
  signal?: AbortSignal,
): Promise<CurrentAlarmsResult> {
  return api
    .get<CurrentAlarmsResult>('/api/alarm/current', params, signal)
    .then((r) => ({
      total: r.total ?? 0,
      ignoreTotal: r.ignoreTotal ?? 0,
      historyTotal: r.historyTotal ?? 0,
      items: r.items ?? [],
    }))
}

export function fetchHistoryAlarms(
  params: { deviceId?: string; level?: string; date?: string; pointName?: string; page?: number; pageSize?: number },
  signal?: AbortSignal,
): Promise<AlarmPage<HistoryAlarm>> {
  return api
    .get<AlarmPage<HistoryAlarm>>('/api/alarm/history', params, signal)
    .then((r) => ({ total: r.total ?? 0, items: r.items ?? [] }))
}

export function fetchIgnoreAlarms(
  params: { deviceId?: string; point?: string; page?: number; pageSize?: number },
  signal?: AbortSignal,
): Promise<AlarmPage<IgnoreAlarm>> {
  return api
    .get<AlarmPage<IgnoreAlarm>>('/api/alarm/ignore', params, signal)
    .then((r) => ({ total: r.total ?? 0, items: r.items ?? [] }))
}

/** 忽略规则：命中 (deviceId, point) 的告警不再出现在当前告警里 */
export function ignoreAlarm(body: {
  deviceId: string
  sourceDeviceId?: string
  point: string
  pointName?: string
}): Promise<void> {
  return api.post<void>('/api/alarm/ignore', body)
}

export function unignoreAlarm(body: { deviceId: string; point: string }): Promise<void> {
  return api.request<void>('/api/alarm/ignore', { method: 'DELETE', body })
}

/** 清空当前告警（admin） */
export function clearCurrentAlarms(): Promise<void> {
  return api.post<void>('/api/alarm/clear', {})
}

/** 清空历史告警（admin） */
export function clearAlarmHistory(): Promise<void> {
  return api.post<void>('/api/alarm/history/clear', {})
}
