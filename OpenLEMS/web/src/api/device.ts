import { api } from './client'

/** 设备类型（与后端 c_enum.EDeviceType 对齐，只列本页用到的） */
export type DeviceType = 'pcs' | 'bms' | 'ess' | 'sess' | 'ammeter' | 'load' | 'pv'

/** GET /api/device/name-list → {设备ID: 名称} */
export function fetchDeviceNames(
  deviceTypes: DeviceType[],
  signal?: AbortSignal,
): Promise<Record<string, string>> {
  return api
    .get<{ deviceNames: Record<string, string> }>('/api/device/name-list', {
      deviceTypes: deviceTypes.join(','),
    }, signal)
    .then((r) => r.deviceNames ?? {})
}

export interface TelemetryData {
  lastUpdateTime: string | null
  telemetryValues: Record<string, unknown>
}

/** GET /api/device/telemetry */
export function fetchTelemetry(
  deviceId: string,
  signal?: AbortSignal,
): Promise<Record<string, TelemetryData>> {
  return api
    .get<{ telemetry: Record<string, TelemetryData> }>('/api/device/telemetry', { deviceId }, signal)
    .then((r) => r.telemetry ?? {})
}

/** 后端 c_chart.ChartData：X 轴与各序列都是字符串数组 */
export interface ChartData {
  xAxis: { type: string; data: string[] }
  series: { name: string; type: string; unit?: string; data: string[] }[]
}

/** POST /api/device/history */
export function fetchHistory(
  params: { deviceId: string; telemetryKeys: string[]; startTime: number; endTime: number; step?: number },
  signal?: AbortSignal,
): Promise<ChartData> {
  return api.post<ChartData>('/api/device/history', params, signal)
}
