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

/** 设备详细列表（扁平，带 pid 与协议/驱动信息）—— 前端自己拼树，一次请求拿全 */
export interface DeviceDetailedInfo {
  id: string
  pid: string
  name: string
  protocolId: string
  driver: string
  enableDebug: boolean
  strategy: string
  storageEnable: boolean
  storageIntervalSec: number
  sort: number
  enabled: boolean
  params: Record<string, unknown>
  createdAt?: string
  updatedAt?: string
  deviceType: string
  isVirtualDevice: boolean
  driverType: string
  driverBrand: string
  driverModel: string
  driverVersion: string
  protocolName: string
  protocolType: string
  protocolAddress: string
  failedMessage?: string
}

export function fetchDetailedList(signal?: AbortSignal): Promise<DeviceDetailedInfo[]> {
  return api
    .get<{ devices: DeviceDetailedInfo[]; total: number }>('/api/device/detailed-list', undefined, signal)
    .then((r) => r.devices ?? [])
}

/** 点位定义（按 group 分组，字段用 groupKey 关联） */
export interface PointField {
  key: string
  name?: string
  groupKey?: string
  valueType: string
  componentType: string
  step?: number
  required: boolean
  unit?: string
  min?: number
  max?: number
  default?: string
  description?: string
}

export interface PointDefinitions {
  isVirtualDevice: boolean
  groups: { key: string; name: string; sort?: number }[]
  fields: PointField[]
}

export function fetchPointDefinitions(deviceId: string, signal?: AbortSignal): Promise<PointDefinitions> {
  return api.get<PointDefinitions>(
    `/api/device/${encodeURIComponent(deviceId)}/points/definitions`,
    undefined,
    signal,
  )
}

export function setDeviceEnabled(deviceId: string, enabled: boolean): Promise<void> {
  return api.post<void>(enabled ? '/api/device/enable' : '/api/device/disable', { deviceId })
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
