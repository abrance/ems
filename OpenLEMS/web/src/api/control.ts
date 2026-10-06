import { api } from './client'

/**
 * 值类型与组件类型（后端 c_enum.EConfigFieldsValueType / EConfigFieldsComponentType 的子集）。
 * 只声明实际会遇到的取值，遇到新值走 default 分支，不猜。
 */
export type FieldValueType = 'string' | 'int' | 'float' | 'bool'

export interface FieldDefinition {
  key: string
  name?: string
  valueType: FieldValueType
  componentType: string
  step?: number
  required: boolean
  unit?: string
  min?: number
  max?: number
  precise?: number
  default?: string
  description: string
  valueExplain?: { key: string; value: string }[]
}

export interface DriverService {
  icon?: string
  key: string
  name: string
  description: string
  params?: FieldDefinition[]
}

/** GET /api/control/custom-services —— 设备可执行的自定义服务（驱动提供） */
export function fetchCustomServices(deviceId: string, signal?: AbortSignal): Promise<DriverService[]> {
  return api
    .get<{ services: DriverService[] }>('/api/control/custom-services', { deviceId }, signal)
    .then((r) => r.services ?? [])
}

/**
 * POST /api/control/device —— 手动控制设备（后端 role=admin）。
 * parameters 是位置参数数组，顺序必须与驱动服务声明一致，不能按名字传。
 */
export function controlDevice(
  deviceId: string,
  commandName: string,
  parameters: unknown[],
): Promise<void> {
  return api.post<void>('/api/control/device', { deviceId, commandName, parameters })
}
