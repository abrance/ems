import { api } from './client'

/**
 * 协议与驱动。
 *
 * 协议 = 通信通道（Modbus TCP / CANbus / GPIO …），驱动 = 设备型号适配。
 * 一台设备要同时绑定两者（见 DeviceDetailedInfo 的 protocolId + driver）。
 */
export interface Protocol {
  protocolId: string
  protocolName: string
  protocolType: string
  protocolAddress: string
  protocolPort: number
  protocolTimeout: number
  protocolLogLevel: string
  protocolParams: string
  protocolActive: boolean
  /** 仅 GPIO 协议有值 */
  boundDeviceId?: string
  boundDeviceName?: string
}

export interface Driver {
  driverName: string
  driverVersion: string
  driverType: string
  driverDescription: string
  driverStatus: boolean
  driverIsEmbedded: boolean
  driverLastUpdate: string
  driverPath: string
  driverHash: string
  driverFileSizeByte: number
  driverImage: string
  protocolType: string
  brand: string
  model: string
  buildTime: string
  commitHash: string
  remark: string
}

export function listProtocols(type = '', signal?: AbortSignal): Promise<Protocol[]> {
  return api
    .get<{ list: Protocol[]; total: number }>('/api/protocol/list', { type }, signal)
    .then((r) => r.list ?? [])
}

export function listDrivers(signal?: AbortSignal): Promise<Driver[]> {
  return api.get<{ list: Driver[]; total: number }>('/api/driver/list', undefined, signal).then((r) => r.list ?? [])
}

export function getDriver(driverName: string, signal?: AbortSignal): Promise<Driver> {
  return api
    .get<{ driver: Driver }>('/api/driver/get', { driverName }, signal)
    .then((r) => r.driver)
}
