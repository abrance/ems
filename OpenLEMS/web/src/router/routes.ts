import type { RouteRecordRaw } from 'vue-router'
import { DEFAULT_ROUTE, NAV } from '@/nav'
import { PAGE_LOADERS, PlaceholderView } from '@/pages'

/**
 * 入口 status=paused 时一般不该再渲染这个页面：能用的功能关掉了，页面自己也没写停用态，
 * 让人看到一个「看起来能用但一用就报错」的界面最糟。这类页统一落到 PlaceholderView（写停用原因）。
 *
 * 例外：页面自己实现了停用态（内容换成只读说明），就要留着它 —— 「手动控制」就是这样，
 * 它要告诉用户后端也拒绝、以及怎么临时启用。
 */
const PAUSED_KEEPS_VIEW = new Set(['manual'])

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
      component:
        page.status === 'paused' && !PAUSED_KEEPS_VIEW.has(page.key)
          ? PlaceholderView
          : (PAGE_LOADERS[page.key] ?? PlaceholderView),
      meta: { tab: tab.key, page: page.key, title: page.title },
    })),
  )
  // 默认首页可在「配置 · 界面与语言」里改；存的是 nav 里的 path
  const landing = localStorage.getItem('lems:ui:landing') || DEFAULT_ROUTE
  const allPaths = new Set(routes.map((r) => r.path))
  const landingTarget = allPaths.has(landing) ? landing : DEFAULT_ROUTE

  return [
    // 登录页不来自 nav：它不参与 Tab/侧栏结构，是壳层之外的独立页
    {
      path: '/login',
      name: 'login',
      component: () => import('@/pages/LoginView.vue'),
      meta: { public: true, title: '登录' },
    },
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
