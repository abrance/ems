import { createRouter, createWebHistory } from 'vue-router'
import { buildRoutes } from './routes'
import { isAuthenticated } from '@/auth/session'

export const router = createRouter({
  history: createWebHistory(),
  routes: buildRoutes(),
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
