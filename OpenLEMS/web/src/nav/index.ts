/**
 * 导航唯一真源。
 *
 * Tab、侧栏、路由、面包屑、权限码、页面状态全从这里出。
 * 新增一个页面 = 在这里加一条 + 在 src/pages/index.ts 注册组件；不改壳层。
 * 口径见 .monkeycode/specs/2026-10-06-ems-frontend-shell/design.md §3。
 */

/** ready=已实现；planned=本期做但还没写；todo=本期不做，仅留入口；paused=做过但已停用（页面会说明原因） */
export type PageStatus = 'ready' | 'planned' | 'todo' | 'paused'

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
    key: 'monitor',
    title: '运行监控',
    pages: [
      {
        key: 'overview',
        title: '能源总览',
        path: '/monitor/overview',
        status: 'ready',
        perm: 'monitor:overview',
        note: '看板面板可勾选显隐，配置存本地',
      },
      { key: 'realtime', title: '实时数据', path: '/monitor/realtime', status: 'ready', perm: 'monitor:realtime' },
      { key: 'cabinet', title: '储能柜', path: '/monitor/cabinet', status: 'ready', perm: 'monitor:cabinet' },
      { key: 'alarms', title: '告警中心', path: '/monitor/alarms', status: 'ready', perm: 'monitor:alarm' },
    ],
  },
  // 原「场景」一个 Tab 7 页，按「看数据」与「做预测」拆成两个 Tab。
  // path 一律留在 /scene/*：书签、lems:ui:landing、冒烟断言都锚在 path 上，
  // 拆 Tab 只改归属，不改地址（改 path 收益为零、代价是三条锚全断）。
  {
    key: 'market',
    title: '市场数据',
    scene: true,
    pages: [
      {
        key: 'disclosure',
        title: '信息披露',
        path: '/scene/disclosure',
        status: 'ready',
      },
      { key: 'clearing', title: '市场出清', path: '/scene/clearing', status: 'ready' },
      { key: 'history', title: '历史信息', path: '/scene/history', status: 'ready' },
      {
        key: 'intradayResearch',
        title: '分时研究',
        path: '/scene/intraday-research',
        status: 'ready',
        note:
          '第三方 · 非官方：96 点（15 分钟）冻结快照，只读、不采集、不刷新。官方披露只有日频，所以本页不进复盘 MAE 口径',
      },
    ],
  },
  {
    key: 'analysis',
    title: '分析预测',
    scene: true,
    pages: [
      { key: 'decision', title: '决策支持', path: '/scene/decision', status: 'ready' },
      { key: 'review', title: '预测复盘', path: '/scene/review', status: 'ready' },
      {
        key: 'inference',
        title: '策略反演',
        path: '/scene/inference',
        status: 'ready',
        note: '基础版 = 统计反演：由 (负荷, 出清价) 观测反推市场聚合报价曲线。逐机组反演与逆优化仍需出清明细/网络模型（设计稿 §14.1）',
      },
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
        status: 'paused',
        perm: 'manage:control',
        note: '已停用：手动指令会被运行中的策略覆盖（策略每周期重写目标功率），且暂无临时接管机制。后端接口也拒绝下发；要临时启用需改设置 manual_control_enabled。',
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
      {
        key: 'exports',
        title: '报表导出',
        path: '/manage/exports',
        status: 'ready',
        perm: 'manage:export',
        note: '基础版：7 个数据集前端拼 CSV 直接下载。没有模板 / 异步任务 / 历史文件（那需要后端任务队列）',
      },
      {
        key: 'users',
        title: '用户与权限',
        path: '/manage/users',
        status: 'ready',
        perm: 'manage:user',
        note: '本系统没有用户表：admin / user 两个角色本身就是账号（登录页也要填这两个词之一）',
      },
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
      {
        key: 'logging',
        title: '日志级别',
        path: '/config/logging',
        status: 'ready',
        perm: 'config:log',
        note: '两套级别分开：全局（系统日志，写文件）+ 按模块（业务日志，入库）。改完立即生效',
      },
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
