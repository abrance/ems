import { api } from './client'

/**
 * 协议监控与设备对比（统计分析页用）。
 *
 * `/protocol/monitor/trend` 的 metricKeys 取值来自后端 controller 的映射表
 * （internal/controller/protocol/protocol_v1_post_monitor_trend.go）：
 *   ProtocolMetricTotal / Success / Failed / SuccessRate / AvgResponseMs
 * 后端会把未映射的键原样透传，所以这里列出的 5 个就是可用集合。
 */
export interface ChartData {
  xAxis: { type: string; data: string[] }
  series: { name: string; type: string; unit?: string; data: string[] }[]
}

export interface ProtocolOverview {
  totalProtocols: number
  activeProtocols: number
  avgSuccessRate: number
  avgResponseMs: number
  abnormalProtocols: number
}

export interface ProtocolDistribution {
  typeDistribution: Record<string, number>
  statusDistribution: { active: number; inactive: number }
}

export const PROTOCOL_METRICS = [
  { key: 'ProtocolMetricTotal', label: '请求总数' },
  { key: 'ProtocolMetricSuccess', label: '成功数' },
  { key: 'ProtocolMetricFailed', label: '失败数' },
  { key: 'ProtocolMetricSuccessRate', label: '成功率(%)' },
  { key: 'ProtocolMetricAvgResponseMs', label: '平均响应(ms)' },
]

export function fetchProtocolOverview(signal?: AbortSignal): Promise<ProtocolOverview> {
  return api.post<ProtocolOverview>('/api/protocol/monitor/overview', {}, signal)
}

export function fetchProtocolDistribution(signal?: AbortSignal): Promise<ProtocolDistribution> {
  return api.post<ProtocolDistribution>('/api/protocol/monitor/distribution', {}, signal)
}

export function fetchProtocolTrend(
  metricKeys: string[],
  params: { startTime?: number; endTime?: number; step?: number } = {},
  signal?: AbortSignal,
): Promise<ChartData> {
  return api.post<ChartData>('/api/protocol/monitor/trend', { metricKeys, ...params }, signal)
}

export function fetchDeviceCompare(
  params: {
    deviceIds: string[]
    lineKeys?: string[]
    barKeys?: string[]
    startTime?: number
    endTime?: number
    step?: number
  },
  signal?: AbortSignal,
): Promise<ChartData> {
  return api.post<ChartData>('/api/device/compare', params, signal)
}
