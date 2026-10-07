import { api } from './client'

/**
 * 系统设置（/api/setting 系列）。
 *
 * ⚠️ `/setting/update` 是**全量覆盖**：请求里没给的字段会被写成默认值
 * （`isPublic` → false、`remark` → ""、`sort` → 0）。所以**不能**直接拿
 * {id, value} 去更新 —— 那条设置会当场变成非公开，页面下次就再也读不到它了。
 * 本模块的 saveSettingValue 会先读详情再把字段原样带回。
 */
export interface PublicSetting {
  id: string
  value: string
  groupName: string
  remark: string
}

export interface SettingDetail {
  id: string
  value: string
  isPublic: boolean
  enabled: boolean
  remark: string
  sort: number
  group: string
  createdAt?: string
  updatedAt?: string
}

/** 公开设置列表（不含私有设置） */
export function fetchPublicSettings(signal?: AbortSignal): Promise<PublicSetting[]> {
  return api.get<{ settings: PublicSetting[] }>('/api/setting', undefined, signal).then((r) => r.settings ?? [])
}

export function fetchSettingDetail(id: string, signal?: AbortSignal): Promise<SettingDetail> {
  return api.post<SettingDetail>('/api/setting/detail', { id }, signal)
}

/**
 * 只改值，其余字段原样保留。
 * 见文件头的告警：/setting/update 是全量覆盖，必须把详情读回来一起提交。
 */
export async function saveSettingValue(id: string, value: string): Promise<void> {
  const detail = await fetchSettingDetail(id)
  await api.post<void>('/api/setting/update', {
    id,
    value,
    group: detail.group,
    isPublic: detail.isPublic,
    enabled: detail.enabled,
    remark: detail.remark,
    sort: detail.sort,
  })
}

export const SETTING_LOG_LEVEL_GLOBAL = 'LogLevelGlobal'
export const SETTING_SYSTEM_ENABLE_DEBUG_LOG = 'SystemEnableDebugLog'

/** 业务日志模块：与后端 s_db_basic.LogLevelSettings 一一对应（顺序也一致） */
export const LOG_MODULES: Array<{ id: string; title: string; remark: string }> = [
  { id: 'LogLevelEms', title: 'ems', remark: '系统自身（启动、配置、Web、鉴权等）' },
  { id: 'LogLevelDevice', title: 'device', remark: '设备采集与设备控制' },
  { id: 'LogLevelProtocol', title: 'protocol', remark: '协议（Modbus / CAN / GPIO 等）' },
  { id: 'LogLevelPolicy', title: 'policy', remark: '策略（充放电决策等）' },
  { id: 'LogLevelAutomation', title: 'automation', remark: '自动化联动' },
  { id: 'LogLevelRemote', title: 'remote', remark: '远程调度与远程协议' },
]

/** GoFrame 接受的级别串（见 glog 的 levelStringMap）。ALL 表示不过滤。 */
export const LOG_LEVEL_OPTIONS = [
  { value: 'ALL', label: 'ALL（不过滤）' },
  { value: 'DEBUG', label: 'DEBUG' },
  { value: 'INFO', label: 'INFO' },
  { value: 'WARN', label: 'WARN' },
  { value: 'ERROR', label: 'ERROR' },
]
