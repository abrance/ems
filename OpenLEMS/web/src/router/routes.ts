import type { RouteRecordRaw } from 'vue-router'
import { DEFAULT_ROUTE, NAV } from '@/nav'
import { PAGE_LOADERS, PlaceholderView } from '@/pages'

/**
 * 路由表由 nav 生成，不手写第二份。
 * 单独成文件、且只导出纯函数：不在这里 new router（创建 web history 需要浏览器环境），
 * 这样测试可以用内存 history 复用同一份表。
 */
export function buildRoutes(): RouteRecordRaw[] {
  const routes: RouteRecordRaw[] = NAV.flatMap((tab) =>
    tab.pages.map((page) => ({
      path: page.path,
      name: page.key,
      component: PAGE_LOADERS[page.key] ?? PlaceholderView,
      meta: { tab: tab.key, page: page.key, title: page.title },
    })),
  )
  return [
    { path: '/', redirect: DEFAULT_ROUTE },
    ...routes,
    { path: '/:pathMatch(.*)*', redirect: DEFAULT_ROUTE },
  ]
}
