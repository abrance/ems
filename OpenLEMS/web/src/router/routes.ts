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
  // 默认首页可在「配置 · 界面与语言」里改；存的是 nav 里的 path
  const landing = localStorage.getItem('lems:ui:landing') || DEFAULT_ROUTE
  const allPaths = new Set(routes.map((r) => r.path))
  const landingTarget = allPaths.has(landing) ? landing : DEFAULT_ROUTE

  return [
    { path: '/', redirect: landingTarget },
    // 旧前端用过的路径，留着防书签失效：
    //   /programs 旧 SPA 的「系统管理」页（overlay 的返回目标）
    //   /effect   effect-overlay 自建的效果评估页（已并入 场景·决策支持）
    { path: '/programs', redirect: '/manage/manual' },
    { path: '/effect', redirect: '/scene/decision' },
    ...routes,
    { path: '/:pathMatch(.*)*', redirect: DEFAULT_ROUTE },
  ]
}
