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

export function changePassword(body: { role?: string; newPassword: string }): Promise<Record<string, unknown>> {
  return api.post<Record<string, unknown>>('/api/auth/change-password', body)
}
