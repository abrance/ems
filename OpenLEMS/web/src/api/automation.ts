import { api } from './client'

/**
 * 自动化联动。
 *
 * 规则模型（后端 s_automation / entity.SAutomation）：
 *   triggerRule = {
 *     anyMatch: [条件],        // OR：任一满足即触发
 *     subMatch: [条件],        // 与 anyMatch 的复合关系由 subMatchAll 控制
 *     subMatchAll: bool,
 *     executionInterval: 秒,   // 0 = 实时
 *   }
 *   条件 = { deviceCondition?: {deviceId, from, rule}, timeCondition?: {...} }
 *     · deviceCondition.rule 是**表达式字符串**（如 "Power>30"、"SOC<10"），
 *       不是结构化比较 —— 所以编辑时由「点位 + 比较符 + 值」拼出来，读回来直接展示。
 *   executeRule = { rules: [{deviceId, service, params: []}] }
 *     · params 是位置参数数组，顺序必须与驱动服务声明一致（与手动控制页同一约定）。
 */
export interface DeviceCondition {
  deviceId: string
  from: string
  rule: string
}

export interface TimeCondition {
  hour?: number | null
  minute?: number | null
  dayOfWeek?: number | null
  dayOfMonth?: number | null
  month?: number | null
  startTime?: string
  endTime?: string
}

export interface TriggerCondition {
  deviceCondition?: DeviceCondition
  timeCondition?: TimeCondition
}

export interface TriggerConfig {
  anyMatch: TriggerCondition[]
  subMatch?: TriggerCondition[]
  subMatchAll?: boolean
  executionInterval: number
}

export interface ExecuteRuleItem {
  deviceId: string
  service: string
  params: unknown[]
}

export interface ExecuteConfig {
  rules: ExecuteRuleItem[]
}

export interface Automation {
  id: number
  name: string
  startTime?: string
  endTime?: string
  timeRangeType?: string
  timeRangeValue?: string
  triggerRule?: TriggerConfig | null
  executeRule?: ExecuteConfig | null
  enabled: boolean
  createdAt?: string
  updatedAt?: string
}

export interface AutomationPayload {
  name: string
  triggerRule: TriggerConfig
  /** 后端要 JSON 字符串 */
  executeRule: string
  enabled: boolean
}

export function listAutomations(
  params: { page?: number; pageSize?: number; deviceId?: string; enabled?: boolean } = {},
  signal?: AbortSignal,
): Promise<{ list: Automation[]; total: number }> {
  return api
    .get<{ list: Automation[]; total: number }>(
      '/api/automation/page',
      { page: params.page ?? 1, pageSize: params.pageSize ?? 100, deviceId: params.deviceId, enabled: params.enabled },
      signal,
    )
    .then((r) => ({ list: r.list ?? [], total: r.total ?? 0 }))
}

export function createAutomation(payload: AutomationPayload): Promise<number> {
  return api.post<{ id: number }>('/api/automation', payload).then((r) => r.id)
}

export function updateAutomation(id: number, payload: AutomationPayload): Promise<void> {
  return api.request<void>(`/api/automation/${id}`, { method: 'PUT', body: { id, ...payload } })
}

export function deleteAutomation(id: number): Promise<void> {
  return api.request<void>(`/api/automation/${id}`, { method: 'DELETE' })
}

export function toggleAutomation(id: number, enable: boolean): Promise<void> {
  return api.post<void>(`/api/automation/${id}/toggle`, { id, enable })
}

/* ---------- 表达式与展示 ---------- */

export const RULE_OPERATORS = ['>', '<', '>=', '<=', '==', '!='] as const

/** "Power>30" → { point: "Power", op: ">", value: "30" }；解析不出来时 point 为空 */
export function parseRule(rule: string): { point: string; op: string; value: string } {
  const m = String(rule ?? '').match(/^\s*([A-Za-z_][\w.]*)\s*(>=|<=|==|!=|>|<)\s*(.+?)\s*$/)
  if (!m) return { point: String(rule ?? ''), op: '>', value: '' }
  return { point: m[1], op: m[2], value: m[3] }
}

export function buildRule(point: string, op: string, value: string): string {
  return `${point.trim()}${op}${value.trim()}`
}

/** 条件摘要（列表用） */
export function conditionText(c: TriggerCondition): string {
  if (c.deviceCondition) {
    const d = c.deviceCondition
    return `${d.deviceId} ${d.rule || '（未设条件）'}`
  }
  if (c.timeCondition) {
    const t = c.timeCondition
    const parts: string[] = []
    if (t.startTime || t.endTime) parts.push(`${t.startTime || '00:00'}~${t.endTime || '24:00'}`)
    if (t.dayOfWeek != null) parts.push(`周${['日', '一', '二', '三', '四', '五', '六'][t.dayOfWeek] ?? t.dayOfWeek}`)
    if (t.dayOfMonth != null) parts.push(`${t.dayOfMonth} 日`)
    if (t.month != null) parts.push(`${t.month} 月`)
    if (t.hour != null) parts.push(`${t.hour} 时`)
    if (t.minute != null) parts.push(`${t.minute} 分`)
    return parts.length ? parts.join('·') : '（未设时间）'
  }
  return '（空条件）'
}
