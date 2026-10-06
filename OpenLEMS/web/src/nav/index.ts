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
      {
        key: 'decision',
        title: '决策支持',
        path: '/scene/decision',
        status: 'planned',
        note: '等价格预测模型接入（/api/forecast/price 已就绪）',
        panels: ['市场价格预测', '日前市场竞争空间', '历史相似日分析', '历史相似日情况分析'],
      },
      {
        key: 'clearing',
        title: '市场出清',
        path: '/scene/clearing',
        status: 'planned',
        note: '等日报提取出清结果（表1(二)/表2）',
        panels: [
          '日前市场火电启停状态统计图（t96）',
          '日前火电机组启停状态统计表',
          '日前调频价格（容量 / 里程）',
          '日前输电约束及阻塞情况',
        ],
      },
      {
        key: 'review',
        title: '预测复盘',
        path: '/scene/review',
        status: 'planned',
        note: '趋势相关度 / 分时离散度的公式待定（见设计稿 §10.9）',
        panels: [
          '市场出清与预测价格',
          '趋势相关度（峰 / 平 / 谷 / 全天）',
          '分时离散度（标幺 / 数值）',
          '峰平谷平均离散度',
        ],
      },
      {
        key: 'inference',
        title: '策略反演',
        path: '/scene/inference',
        status: 'todo',
        note: '缺网络模型 / 出清模型 / 机组成本参数，只靠日报无法反演（设计稿 §14.1）',
        panels: ['机组实时出力', '市场成员报价策略反演', '机组累计电量', '日前市场火电开机安排'],
      },
      {
        key: 'history',
        title: '历史信息',
        path: '/scene/history',
        status: 'planned',
        note: '多日聚合口径待逐指标定义',
        panels: [
          '火电启停状态统计（多日）',
          '日前市场火电开机安排（多日）',
          '日前系统备用需求',
          '日前系统调频辅助服务需求',
          '日前-实时市场出清与预测价格',
          '日前-实时市场出清电量',
        ],
      },
      {
        key: 'forecast',
        title: '长周期电价预测',
        path: '/scene/forecast',
        status: 'planned',
        note: '接口已就绪（/api/forecast/price，日均序列）；96 点分时待日报提取曲线',
        panels: ['未来多日电价预测-分时', '未来多日电价预测-平均'],
      },
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
      { key: 'automation', title: '自动化联动', path: '/manage/automation', status: 'todo', perm: 'manage:automation', panels: ['自动化规则'] },
      { key: 'devices', title: '设备管理', path: '/manage/devices', status: 'planned', perm: 'manage:device', note: '本期只接 emsdevice mock', panels: ['设备树', '点表（holding 0xC8–0xD5）'] },
      { key: 'drivers', title: '协议与驱动', path: '/manage/drivers', status: 'todo', perm: 'manage:driver', panels: ['驱动管理', '协议配置'] },
      { key: 'remote', title: '远程调度', path: '/manage/remote', status: 'todo', perm: 'manage:remote', panels: ['MQTT', 'Modbus', '远程日志'] },
      { key: 'network', title: '网络配置', path: '/manage/network', status: 'todo', perm: 'manage:network', panels: ['网卡配置'] },
      { key: 'price', title: '电价管理', path: '/manage/price', status: 'ready', perm: 'manage:price' },
      { key: 'collect', title: '采集管理', path: '/manage/collect', status: 'ready', perm: 'manage:collect' },
      { key: 'statistics', title: '统计分析', path: '/manage/statistics', status: 'todo', perm: 'manage:statistics', panels: ['设备对比', '系统监控', '协议统计'] },
      { key: 'audit', title: '审计', path: '/manage/audit', status: 'planned', perm: 'manage:audit', panels: ['操作审计', '运行日志'] },
      { key: 'exports', title: '报表导出', path: '/manage/exports', status: 'todo', perm: 'manage:export', panels: ['报表模板', '生成任务'] },
      { key: 'users', title: '用户与权限', path: '/manage/users', status: 'todo', perm: 'manage:user', note: '本期不做；只分管理员 / 普通用户两级', panels: ['用户列表', '角色'] },
    ],
  },
  {
    key: 'config',
    title: '配置',
    pages: [
      { key: 'ess', title: '储能参数', path: '/config/ess', status: 'ready', perm: 'config:ess' },
      { key: 'info', title: '系统信息', path: '/config/info', status: 'planned', perm: 'config:system', panels: ['CPU', '内存', '磁盘', '网络', '运行时长', '其他'] },
      { key: 'storage', title: '存储管理', path: '/config/storage', status: 'todo', perm: 'config:storage', panels: ['存储路径', '用量', '清理'] },
      { key: 'operations', title: '系统操作', path: '/config/operations', status: 'todo', perm: 'config:operation', panels: ['备份', '恢复', '升级', '重启'] },
      { key: 'credentials', title: '采集与凭据', path: '/config/credentials', status: 'planned', perm: 'config:credential', note: '密钥只写不读，保存后不回显', panels: ['七牛', 'OCR', '微信来源'] },
      { key: 'retention', title: '数据保留', path: '/config/retention', status: 'todo', perm: 'config:retention', panels: ['遥测', '日报与图片', '日志', '清理策略'] },
      { key: 'time', title: '时间与时区', path: '/config/time', status: 'planned', perm: 'config:time', panels: ['NTP', '时区'] },
      { key: 'ui', title: '界面与语言', path: '/config/ui', status: 'todo', perm: 'config:ui', panels: ['主题', '语言', '密度', '默认首页'] },
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
