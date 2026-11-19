import { api } from './client'

/**
 * 系统信息 / 存储 / 运维操作。
 *
 * 注意各接口的外层形状不一致（GoFrame 的响应结构体各不相同），
 * 拆包统一在 api 层做完，页面只拿业务对象：
 *   /system/summary          扁平（cpu/memory/disk/uptime/time/sys 直接在顶层）
 *   /system/net/traffic      { net }
 *   /system/process/memory   { processMemory }
 *   /system/storage/stats    { stats }
 *   /system/resource/chart   { chartData }
 */
export interface CPUInfo {
  usage: number
  load1: number
  load5: number
  load15: number
}
export interface MemoryInfo {
  usage: number
  totalGB: number
  usedGB: number
  freeGB: number
  procUsage: number
  procRSSMB: number
}
export interface DiskInfo {
  usage: number
  totalGB: number
  usedGB: number
  freeGB: number
}
export interface UptimeInfo {
  day: number
  hour: number
  minute: number
  bootTime: string
  procUptimeSeconds: number
  procStartTime: string
}
export interface TimeInfo {
  now: string
  timezone: string
  ntpRunning: boolean
  ntpServer: string
}
export interface SysInfo {
  hostname: string
  osName: string
  osVersion: string
  kernel: string
}
export interface NetBrief {
  upBytes: number
  downBytes: number
  upBps: number
  downBps: number
}

export interface SystemSummary {
  cpu: CPUInfo
  memory: MemoryInfo
  disk: DiskInfo
  uptime: UptimeInfo
  time: TimeInfo
  sys: SysInfo
}

export interface StorageStats {
  total_series: number
  total_samples: number
  storage_size: number
  storage_size_mb: number
  oldest_timestamp: string
  newest_timestamp: string
  retention_time: number
  retention_hours: number
  avg_series_size: number
  samples_per_second: number
}

export interface ChartData {
  xAxis: { type: string; data: string[] }
  series: { name: string; type: string; unit?: string; data: string[] }[]
}

export function fetchSystemSummary(signal?: AbortSignal): Promise<SystemSummary> {
  return api.get<SystemSummary>('/api/system/summary', undefined, signal)
}

export function fetchNetTraffic(signal?: AbortSignal): Promise<NetBrief> {
  return api.get<{ net: NetBrief }>('/api/system/net/traffic', undefined, signal).then((r) => r.net)
}

export function fetchStorageStats(signal?: AbortSignal): Promise<StorageStats> {
  return api.get<{ stats: StorageStats }>('/api/system/storage/stats', undefined, signal).then((r) => r.stats)
}

/** 资源曲线：category = process / network / service / storage */
export function fetchResourceChart(
  category: 'process' | 'network' | 'service' | 'storage',
  params: { startTime?: number; endTime?: number; step?: number } = {},
  signal?: AbortSignal,
): Promise<ChartData> {
  return api
    .post<{ chartData: ChartData }>('/api/system/resource/chart', { category, ...params }, signal)
    .then((r) => r.chartData)
}

/** 保留天数设置（后端就叫 storage-time） */
export function updateRetention(params: {
  deviceRetentionDays: number
  systemRetentionDays: number
  logRetentionDays: number
}): Promise<void> {
  return api.post<void>('/api/system/storage-time', params)
}

export function updateHostname(hostname: string): Promise<void> {
  return api.post<void>('/api/system/hostname/update', { hostname })
}

export function updateSystemTime(time: string, timezone?: string): Promise<void> {
  return api.post<void>('/api/system/time/update', { time, timezone })
}

/** 重启是两步：先申请（30 秒内有效），再执行 */
export function rebootApply(): Promise<void> {
  return api.post<void>('/api/system/reboot/apply')
}
export function rebootExecute(): Promise<void> {
  return api.post<void>('/api/system/reboot/execute')
}

/* ---------- 展示工具 ---------- */

/** 数据保留策略状态（后端按 device/system/log 三条设置清理历史数据） */
export interface RetentionStatus {
  deviceRetentionDays: number
  systemRetentionDays: number
  logRetentionDays: number
  /** 自动清理间隔（小时） */
  intervalHours: number
  /** 上次清理时间（本地时间；空 = 本次启动后还没跑过） */
  lastRunAt: string
  lastRunTimeMs: number
  /** 上次删掉的磁盘日志文件个数（out/logs/app 下超期 .log） */
  lastLogFilesDeleted: number
  /** 上次释放的磁盘日志字节数 */
  lastLogBytesFreed: number
  lastDeviceBefore: string
  lastSystemBefore: string
  lastLogBefore: string
  /** 上次清理的错误（空 = 成功） */
  lastError: string
}

export function fetchRetention(signal?: AbortSignal): Promise<RetentionStatus> {
  return api.get<{ status: RetentionStatus }>('/api/system/retention', undefined, signal).then((r) => r.status)
}

/** 立即执行一次清理（管理员）——不用等定时任务 */
export function cleanRetentionNow(): Promise<RetentionStatus> {
  return api.post<{ status: RetentionStatus }>('/api/system/retention/clean', {}).then((r) => r.status)
}

export function humanBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`
}

export function humanRate(bps: number): string {
  return `${humanBytes(bps)}/s`
}
