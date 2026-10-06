import { api } from './client'

/**
 * 凭据与外部服务状态（只读）。
 *
 * 密钥存在服务端配置文件 / 环境变量里，改后要重启 —— 所以**没有在线修改接口**，
 * 这个页面只回答「配了没有、从哪来」。密钥值后端一律不回显（返回打码）。
 */
export interface CredentialItem {
  name: string
  configured: boolean
  /** env / config / default / missing */
  source: string
  /** 非敏感值（bucket、域名、服务地址）；敏感项为打码串 */
  value?: string
}

export interface CredentialsStatus {
  storageMode: 'qiniu' | 'local' | string
  qiniu: CredentialItem[]
  ocr: CredentialItem[]
  note: string
}

export function fetchCredentials(signal?: AbortSignal): Promise<CredentialsStatus> {
  return api.get<CredentialsStatus>('/api/system/credentials', undefined, signal)
}

export const SOURCE_LABEL: Record<string, string> = {
  env: '环境变量',
  config: '配置文件',
  default: '默认值',
  missing: '未配置',
}
