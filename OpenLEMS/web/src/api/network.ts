import { api } from './client'

/**
 * 网络配置。
 *
 * `/network/interface/list` 后端有缓存（默认 5 分钟），所以带 `forceRefresh` 开关；
 * 但**改配置之后不会自动失效缓存**，页面必须显式 forceRefresh 再读，否则会看到旧值。
 */
export interface Ipv4Config {
  ipv4: string
  subnetMask: string
}

export interface InterfaceSummary {
  id: string
  name: string
  mac: string
  isUp: boolean
  ipv4: Ipv4Config[] | null
  gateway: string[]
  dns: string[]
  mtu: number
  dhcp: boolean
}

export interface InterfaceConfig {
  ipv4?: Ipv4Config[]
  gateway?: string[]
  dns?: string[]
  dhcp?: boolean
}

export interface PingResult {
  target: string
  success: boolean
  latency: number
  error?: string
  timestamp: number
}

export function listInterfaces(params: { includeLoopback?: boolean; forceRefresh?: boolean } = {}, signal?: AbortSignal): Promise<InterfaceSummary[]> {
  return api
    .get<{ interfaces: InterfaceSummary[] }>('/api/network/interface/list', params, signal)
    .then((r) => r.interfaces ?? [])
}

export function updateInterface(name: string, config: InterfaceConfig): Promise<void> {
  return api.post<void>('/api/network/interface/update', { name, config })
}

export function setInterfaceState(name: string, up: boolean): Promise<void> {
  return api.post<void>('/api/network/interface/state', { name, up })
}

export function ping(target: string): Promise<PingResult> {
  return api.post<{ result: PingResult }>('/api/network/ping', { target }).then((r) => r.result)
}
