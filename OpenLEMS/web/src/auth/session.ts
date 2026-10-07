import { reactive } from 'vue'
import * as authApi from '@/api/auth'

/**
 * 会话。存储键与后端注入的引导脚本（internal/cmd/web.go 的 authBypassScript）
 * 完全一致，不要另起一套 —— 否则 AUTH_BYPASS_ROLE 场景下前端读不到会话。
 *
 * 不用 Pinia：路由守卫要在导航时读会话，而守卫可能早于 Pinia 安装执行；
 * 用模块级的 reactive 对象可以彻底避开这个时序问题。
 */

const TOKEN_KEY = 'ems:user:token'
const ROLE_KEY = 'ems:user:role'
const EXPIRE_KEY = 'ems:user:expireAt'

export interface SessionState {
  token: string
  role: string
  /** 过期时间戳（秒）；0 表示未知（如鉴权绕过注入的会话不过期） */
  expireAt: number
}

export const session = reactive<SessionState>({ token: '', role: '', expireAt: 0 })

function load(): void {
  if (typeof localStorage === 'undefined') return
  session.token = localStorage.getItem(TOKEN_KEY) ?? ''
  session.role = localStorage.getItem(ROLE_KEY) ?? ''
  const raw = localStorage.getItem(EXPIRE_KEY)
  const parsed = raw ? Number(raw) : 0
  session.expireAt = Number.isFinite(parsed) ? parsed : 0
}
load()

/** 有令牌且未过期。expireAt 为 0 视为不过期（绕过模式注入的会话就是这种）。 */
export function isAuthenticated(): boolean {
  if (!session.token) return false
  if (!session.expireAt) return true
  return session.expireAt * 1000 > Date.now()
}

export function clearSession(): void {
  session.token = ''
  session.role = ''
  session.expireAt = 0
  if (typeof localStorage === 'undefined') return
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(ROLE_KEY)
  localStorage.removeItem(EXPIRE_KEY)
}

export async function signIn(account: string, password: string): Promise<void> {
  const res = await authApi.login({ role: account.trim().toLowerCase(), password })
  session.token = res.token
  session.role = res.role
  session.expireAt = res.expireAt || 0
  localStorage.setItem(TOKEN_KEY, session.token)
  localStorage.setItem(ROLE_KEY, session.role)
  if (session.expireAt) localStorage.setItem(EXPIRE_KEY, String(session.expireAt))
  else localStorage.removeItem(EXPIRE_KEY)
}

/**
 * 退出登录。silent 用于「服务端已经说会话失效」的场景：那时再调 logout 只会再吃一个 401。
 */
export async function signOut(options: { silent?: boolean } = {}): Promise<void> {
  if (!options.silent) {
    try {
      await authApi.logout()
    } catch {
      // 退出必须总能成功：后端不可用时也不能把用户困在界面里
    }
  }
  clearSession()
}

export const ACCOUNTS = ['admin', 'user'] as const

export const ROLE_LABEL: Record<string, string> = {
  admin: '管理员',
  user: '普通用户',
}
