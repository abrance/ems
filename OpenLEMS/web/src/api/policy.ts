import { api } from './client'

/**
 * 策略 / 储能参数配置。
 *
 * 值是 JSON 字符串存在 setting 表里（id=policy_microgrid，group=policy）；
 * 表单的字段元数据由后端反射 Go struct tag 得到，前端不复制第二份 —— 见
 * GET /api/policy/config-fields。
 */
export interface ConfigField {
  key: string
  name: string
  description: string
  group: string
  /** number / switch / singleSelect / multiSelect / text */
  component: string
  /** float / int / bool / string */
  valueType: string
  unit?: string
  default?: string
  min?: number
  max?: number
  step?: number
}

export interface ConfigFields {
  fields: ConfigField[]
  groups: string[]
}

/** 策略配置的当前值（对象形式，不是 JSON 字符串） */
export type PolicyConfig = Record<string, unknown>

export function fetchPolicyConfigFields(signal?: AbortSignal): Promise<ConfigFields> {
  return api.get<ConfigFields>('/api/policy/config-fields', undefined, signal)
}

export function fetchPolicyConfig(signal?: AbortSignal): Promise<{ settingId: string; config: PolicyConfig }> {
  return api.get<{ settingId: string; config: PolicyConfig }>('/api/policy/config', undefined, signal)
}

/** POST /api/setting/update —— 配置值以 JSON 字符串回写 */
export function updateSetting(params: {
  id: string
  value: string
  group: string
  remark?: string
  enabled?: boolean
  isPublic?: boolean
}): Promise<void> {
  return api.post<void>('/api/setting/update', {
    id: params.id,
    value: params.value,
    group: params.group,
    remark: params.remark ?? '',
    enabled: params.enabled ?? true,
    isPublic: params.isPublic ?? false,
  })
}

/** 文案统一在「秒」与「0~1」上，避免各页各写一遍 */
export function formatFieldValue(f: ConfigField, v: unknown): string {
  if (v === undefined || v === null || v === '') return '—'
  if (f.valueType === 'bool') return v ? '开' : '关'
  const n = Number(v)
  if (Number.isNaN(n)) return String(v)
  // 比例类字段按百分比展示，人更容易看懂
  if (f.max === 1 && f.min === 0 && f.component === 'number') return `${(n * 100).toFixed(0)}%`
  return `${n}${f.unit ?? ''}`
}
