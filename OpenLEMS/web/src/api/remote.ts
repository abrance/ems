import { api } from './client'

/**
 * 远程调度：把本地数据推给上层平台。
 *
 * 两个方向、别混：
 *   Modbus —— EMS 作**服务端**监听端口，上层来读（本机开 502，挂若干从站设备）
 *   MQTT   —— EMS 作**客户端**连 broker，主动推送（客户端可多份配置）
 * 开关与重载都会实际启停服务，属于有副作用的操作，接口是 admin。
 */
export interface ModbusDeviceStatus {
  deviceId: string
  modbusId: number
  startAddr: number
  isOnline: boolean
  lastUpdateTime: string
}

export interface ModbusStatus {
  isRunning: boolean
  listenPort: number
  deviceCount: number
  connectionCount: number
  deviceStatus: ModbusDeviceStatus[]
}

export interface MqttConfig {
  clientId?: string
  broker?: string
  port?: number
  topic?: string
  username?: string
  [k: string]: unknown
}

export interface MqttClientStatus {
  config: MqttConfig | null
  isConnected: boolean
  isRunning: boolean
  clientId: string
  systemNumber: string
  topic: string
}

export interface MqttStatus {
  isRunning: boolean
  clientCount: number
  clientStatus: MqttClientStatus[]
}

export function fetchModbusStatus(signal?: AbortSignal): Promise<ModbusStatus> {
  return api.get<ModbusStatus>('/api/remote/modbus/status', undefined, signal)
}

export function fetchMqttStatus(signal?: AbortSignal): Promise<MqttStatus> {
  return api.get<MqttStatus>('/api/remote/mqtt/status', undefined, signal)
}

export function setModbusEnabled(enabled: boolean): Promise<void> {
  return api.request<void>(`/api/remote/modbus/enabled/${enabled}`, { method: 'PUT' })
}

export function setMqttEnabled(enabled: boolean): Promise<void> {
  return api.request<void>(`/api/remote/mqtt/enabled/${enabled}`, { method: 'PUT' })
}

export function reloadModbus(): Promise<void> {
  return api.post<void>('/api/remote/modbus/reload')
}

export function reloadMqtt(): Promise<void> {
  return api.post<void>('/api/remote/mqtt/reload')
}
