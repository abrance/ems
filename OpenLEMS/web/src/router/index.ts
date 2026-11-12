import { createRouter, createWebHistory } from 'vue-router'
import { buildRoutes } from './routes'
import { isAuthenticated } from '@/auth/session'

export const router = createRouter({
  history: createWebHistory(),
  routes: buildRoutes(),
})

/**
 * 懒加载 chunk 取不到时自动重载一次。
 *
 * 单副本 Recreate 部署会整批换掉 assets 里的 hash 文件名：还开着的旧标签页
 * 一点页面就会 404（Failed to fetch dynamically imported module），用户看到的就是
 * 一个白屏 + 报错。重载一次即可拿到新壳；限流是为了版本真的对不上时不来回刷。
 */
const CHUNK_RELOAD_AT = 'lems:chunk-reload-at'
const CHUNK_RELOAD_WINDOW_MS = 60_000

function isChunkLoadError(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err ?? '')
  return /Failed to fetch dynamically imported module|ChunkLoadError|Importing a module script failed/i.test(
    message,
  )
}

router.onError((err) => {
  if (!isChunkLoadError(err)) return
  const last = Number(sessionStorage.getItem(CHUNK_RELOAD_AT) ?? 0)
  if (last > 0 && Date.now() - last < CHUNK_RELOAD_WINDOW_MS) return
  sessionStorage.setItem(CHUNK_RELOAD_AT, String(Date.now()))
  window.location.reload()
})

/**
 * 登录守卫。
 *
 * 放在这里而不是每个页面里判断：漏一页就等于漏一个未授权入口，
 * 而这种「忘了加」在加新页面时随时会发生。
 */
router.beforeEach((to) => {
  if (to.meta.public === true) return true
  if (isAuthenticated()) return true
  // 记下原本要去的地方，登录后跳回去（否则每次登录都被扔回首页）
  return { path: '/login', query: { redirect: to.fullPath } }
})
