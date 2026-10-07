import { api } from './client'

/**
 * 认证接口。注意后端没有「用户名」字段 —— `role`（admin / user）本身就是账号，
 * 密码存在系统设置里（admin_password / user_password）。所以登录页的「账号」
 * 填的就是 admin 或 user。
 */
export interface LoginResult {
  token: string
  role: string
  /** 过期时间戳（秒） */
  expireAt: number
}

export function login(body: { role: string; password: string }): Promise<LoginResult> {
  return api.post<LoginResult>('/api/auth/login', body)
}

export function logout(): Promise<Record<string, unknown>> {
  return api.post<Record<string, unknown>>('/api/auth/logout', {})
}

/**
 * 改密码。
 *
 * - `role` 是**要改哪个账号**（留空 = 当前会话自己）。后端会做权限判定：
 *   普通用户只能改自己；管理员可以重置普通用户；任何情况下都不能改别人的 admin。
 * - `oldPassword` 是**当前会话角色本人**的密码，用于证明是你本人在操作 ——
 *   管理员替普通用户重置时，填的也是管理员自己的密码。
 */
export function changePassword(body: {
  role?: string
  oldPassword: string
  newPassword: string
}): Promise<Record<string, unknown>> {
  return api.post<Record<string, unknown>>('/api/auth/change-password', body)
}

/** 当前会话的角色（服务端确认，比读 localStorage 可靠） */
export function fetchCurrentUser(signal?: AbortSignal): Promise<{ role: string }> {
  return api.get<{ role: string }>('/api/auth/current', undefined, signal)
}
