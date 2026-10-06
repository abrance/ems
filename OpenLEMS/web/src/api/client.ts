/**
 * 后端调用统一入口。
 *
 * 约定（与现网 overlay 一致，不要另起一套）：
 * - 令牌放在 localStorage 的 `ems:user:token`，以 Bearer 发送；
 * - Cookie 一并带上（后端 session 名 ems_session_id）；
 * - GoFrame 统一响应为 {code, message, data}，code===0 才算成功，这里直接返回 data。
 */
const TOKEN_KEY = 'ems:user:token'

export class ApiError extends Error {
  constructor(
    message: string,
    readonly code?: number,
    readonly status?: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

interface Envelope<T> {
  code?: number
  message?: string
  data?: T
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  query?: Record<string, string | number | boolean | undefined>
  body?: unknown
  signal?: AbortSignal
}

function buildUrl(path: string, query?: RequestOptions['query']): string {
  if (!query) return path
  const usp = new URLSearchParams()
  for (const [k, v] of Object.entries(query)) {
    if (v !== undefined && v !== '') usp.set(k, String(v))
  }
  const qs = usp.toString()
  return qs ? `${path}?${qs}` : path
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'Accept-Language': 'zh-CN',
  }
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) headers.Authorization = `Bearer ${token}`
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'

  let res: Response
  try {
    res = await fetch(buildUrl(path, options.query), {
      method: options.method ?? 'GET',
      headers,
      credentials: 'include',
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
    })
  } catch (err) {
    throw new ApiError(err instanceof Error ? err.message : '网络请求失败')
  }

  if (res.status === 204) return undefined as T

  let payload: Envelope<T> | undefined
  try {
    payload = (await res.json()) as Envelope<T>
  } catch {
    payload = undefined
  }

  if (!res.ok) {
    throw new ApiError(payload?.message || `请求失败（HTTP ${res.status}）`, payload?.code, res.status)
  }
  if (payload && payload.code !== undefined && payload.code !== 0) {
    throw new ApiError(payload.message || '请求失败', payload.code)
  }
  return (payload?.data ?? (payload as unknown as T)) as T
}

export const api = {
  get: <T>(path: string, query?: RequestOptions['query'], signal?: AbortSignal) =>
    request<T>(path, { method: 'GET', query, signal }),
  post: <T>(path: string, body?: unknown, signal?: AbortSignal) =>
    request<T>(path, { method: 'POST', body, signal }),
  request: <T>(path: string, options: RequestOptions) => request<T>(path, options),
}
