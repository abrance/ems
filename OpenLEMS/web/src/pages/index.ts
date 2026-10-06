import type { RouteComponent } from 'vue-router'
import PlaceholderView from '@/components/PlaceholderView.vue'

/**
 * 页面组件注册表：key 与 src/nav/index.ts 的 NavPage.key 对应。
 * 没在这里登记的页面统一落到 PlaceholderView（按 nav 里的 panels 列出结构）。
 * 做完一个页面 = 这里加一行 + 把 nav 里该页 status 改成 'ready'。
 */
export const PAGE_LOADERS: Record<string, RouteComponent> = {
  disclosure: () => import('./scene/DisclosureView.vue'),
  history: () => import('./scene/HistoryView.vue'),
  decision: () => import('./scene/DecisionView.vue'),
  clearing: () => import('./scene/ClearingView.vue'),
  manual: () => import('./manage/ManualControlView.vue'),
  policies: () => import('./manage/PolicyListView.vue'),
  devices: () => import('./manage/DevicesView.vue'),
  drivers: () => import('./manage/DriversView.vue'),
  remote: () => import('./manage/RemoteView.vue'),
  network: () => import('./manage/NetworkView.vue'),
  statistics: () => import('./manage/StatisticsView.vue'),
  automation: () => import('./manage/AutomationView.vue'),
  audit: () => import('./manage/AuditView.vue'),
  price: () => import('./manage/PriceView.vue'),
  collect: () => import('./manage/CollectView.vue'),
  ess: () => import('./config/EssParamsView.vue'),
  info: () => import('./config/SystemInfoView.vue'),
  storage: () => import('./config/StorageView.vue'),
  operations: () => import('./config/OperationsView.vue'),
  credentials: () => import('./config/CredentialsView.vue'),
  ui: () => import('./config/UiView.vue'),
}

export { PlaceholderView }
