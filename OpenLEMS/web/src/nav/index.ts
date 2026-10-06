/**
 * 导航唯一真源。
 *
 * Tab、侧栏、路由、面包屑、权限码、页面状态全从这里出。
 * 新增一个页面 = 在这里加一条 + 在 src/pages/index.ts 注册组件；不改壳层。
 * 口径见 .monkeycode/specs/2026-10-06-ems-frontend-shell/design.md §3。
 */

/** ready=已实现；planned=本期做但还没写；todo=本期不做，仅留入口 */
export type PageStatus = 'ready' | 'planned' | 'todo'

export interface NavPage {
  key: string
  title: string
  path: string
  /** 权限码，与后端 role 对齐；P0 尚未接鉴权，仅登记 */
  perm?: string
  status: PageStatus
  /** 待办原因，展示在占位页上，避免「为什么是空的」 */
  note?: string
  /** 该页将包含的面板，占位页照此列出，防止结构跑偏 */
  panels?: string[]
}

export interface NavTab {
  key: string
  title: string
  /** 该 Tab 是否为场景型（页头多一个场景切换器 + 运行日） */
  scene?: boolean
  pages: NavPage[]
}

export const NAV: NavTab[] = [
  {
    key: 'scene',
    title: '场景',
    scene: true,
    pages: [
      {
        key: 'disclosure',
        title: '信息披露',
        path: '/scene/disclosure',
        status: 'ready',
      },
      { key: 'decision', title: '决策支持', path: '/scene/decision', status: 'ready' },
      { key: 'clearing', title: '市场出清', path: '/scene/clearing', status: 'ready' },
      { key: 'review', title: '预测复盘', path: '/scene/review', status: 'ready' },
      {
        key: 'inference',
        title: '策略反演',
        path: '/scene/inference',
        status: 'todo',
        note: '缺网络模型 / 出清模型 / 机组成本参数，只靠日报无法反演（设计稿 §14.1）',
        panels: ['机组实时出力', '市场成员报价策略反演', '机组累计电量', '日前市场火电开机安排'],
      },
      { key: 'history', title: '历史信息', path: '/scene/history', status: 'ready' },
      { key: 'forecast', title: '长周期电价预测', path: '/scene/forecast', status: 'ready' },
    ],
  },
  {
    key: 'manage',
    title: '管理',
    pages: [
      {
        key: 'manual',
        title: '手动控制',
        path: '/manage/manual',
        status: 'ready',
        perm: 'manage:control',
      },
      { key: 'policies', title: '策略管理', path: '/manage/policies', status: 'ready', perm: 'manage:policy' },
      { key: 'automation', title: '自动化联动', path: '/manage/automation', status: 'ready', perm: 'manage:automation' },
      { key: 'devices', title: '设备管理', path: '/manage/devices', status: 'ready', perm: 'manage:device' },
      { key: 'drivers', title: '协议与驱动', path: '/manage/drivers', status: 'ready', perm: 'manage:driver' },
      { key: 'remote', title: '远程调度', path: '/manage/remote', status: 'ready', perm: 'manage:remote' },
      { key: 'network', title: '网络配置', path: '/manage/network', status: 'ready', perm: 'manage:network' },
      { key: 'price', title: '电价管理', path: '/manage/price', status: 'ready', perm: 'manage:price' },
      { key: 'collect', title: '采集管理', path: '/manage/collect', status: 'ready', perm: 'manage:collect' },
      { key: 'statistics', title: '统计分析', path: '/manage/statistics', status: 'ready', perm: 'manage:statistics' },
      { key: 'audit', title: '审计', path: '/manage/audit', status: 'ready', perm: 'manage:audit' },
      { key: 'exports', title: '报表导出', path: '/manage/exports', status: 'todo', perm: 'manage:export', panels: ['报表模板', '生成任务'] },
      { key: 'users', title: '用户与权限', path: '/manage/users', status: 'todo', perm: 'manage:user', note: '本期不做；只分管理员 / 普通用户两级', panels: ['用户列表', '角色'] },
    ],
  },
  {
    key: 'config',
    title: '配置',
    pages: [
      { key: 'ess', title: '储能参数', path: '/config/ess', status: 'ready', perm: 'config:ess' },
      { key: 'info', title: '系统信息', path: '/config/info', status: 'ready', perm: 'config:system' },
      { key: 'storage', title: '存储管理', path: '/config/storage', status: 'ready', perm: 'config:storage' },
      { key: 'operations', title: '系统操作', path: '/config/operations', status: 'ready', perm: 'config:operation' },
      { key: 'credentials', title: '采集与凭据', path: '/config/credentials', status: 'ready', perm: 'config:credential' },
      { key: 'ui', title: '界面与语言', path: '/config/ui', status: 'ready', perm: 'config:ui' },
      { key: 'logging', title: '日志级别', path: '/config/logging', status: 'todo', perm: 'config:log', panels: ['各模块日志级别'] },
    ],
  },
]

export const DEFAULT_ROUTE = NAV[0].pages[0].path

export function findTabByKey(key: string): NavTab | undefined {
  return NAV.find((t) => t.key === key)
}

export function findPageByPath(path: string): { tab: NavTab; page: NavPage } | undefined {
  for (const tab of NAV) {
    const page = tab.pages.find((p) => p.path === path)
    if (page) return { tab, page }
  }
  return undefined
}
