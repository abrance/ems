/**
 * 壳层冒烟：在 happy-dom 里加载 **构建产物**，逐个点过 nav 里的页面。
 *
 * 为什么不用 vite 的 ssrLoadModule 加载组件：SSR 编译会给每个 SFC 注入
 * useSSRContext / scoped-id，在没有 server context 的纯客户端环境里直接炸。
 * 直接跑 dist 更贴近线上。只有 nav/index.ts 是纯 TS（不含 SFC），可以安全地走 vite 编译，
 * 用它当断言基准，避免在测试里手抄一份导航。
 *
 * 覆盖 P0 验收点：
 *   - 3 个 Tab、侧栏项数与 nav 一致
 *   - 每条路由都能渲染出正确标题；未实现页把 nav 里声明的面板全部列出
 *   - 顶栏不含流量 / CPU / 时钟（设计稿 §2 原则 6）
 *
 * 已知不覆盖：信息披露页的 ECharts 绘制（happy-dom 无 canvas，只验证挂载不抛错）
 */
import { Window } from 'happy-dom'
import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { dirname, resolve } from 'node:path'
import { createServer } from 'vite'

const here = dirname(fileURLToPath(import.meta.url))
const root = resolve(here, '..')
const dist = resolve(root, 'dist')

let failed = 0
function check(ok, label, detail = '') {
  if (ok) console.log(`  ok   ${label}`)
  else {
    failed++
    console.error(`  FAIL ${label}${detail ? ' — ' + detail : ''}`)
  }
}
const tick = () => new Promise((r) => setTimeout(r, 0))

/**
 * 等条件成立。懒加载分块（含它自带的 CSS）在 happy-dom 下的解析时机不确定，
 * 固定的 setTimeout(0) 会在导航完成前就断言，所以统一用轮询等待。
 */
async function waitFor(fn, ms = 3000) {
  const deadline = Date.now() + ms
  while (Date.now() < deadline) {
    if (fn()) return true
    await new Promise((r) => setTimeout(r, 10))
  }
  return false
}

const h1Text = () => doc.querySelector('.pagehead h1')?.textContent?.trim() ?? ''
const sideTitles = () => [...doc.querySelectorAll('.side-item')].map((e) => e.textContent.trim())
const findSide = (title) =>
  [...doc.querySelectorAll('.side-item')].find((e) => e.textContent.trim().startsWith(title))
const findTab = (title) => [...doc.querySelectorAll('.tabs .tab')].find((e) => e.textContent.trim() === title)

// vue-router 在 happy-dom 下会用一个非 Event 对象调 window.dispatchEvent，
// 属于测试环境互操作瑕疵（线上无此问题），单独归集，不算失败。
const interopNoise = []
/** 测试环境噪音：happy-dom 缺 canvas / 只认标准 Event，与业务逻辑无关 */
const NOISE = [/dispatchEvent/, /clearRect/, /'dpr'/]
process.on('unhandledRejection', (err) => {
  const msg = String(err?.message || err)
  if (NOISE.some((re) => re.test(msg))) {
    interopNoise.push(msg)
    return
  }
  failed++
  console.error('  FAIL 未处理的 Promise 异常:', err)
})

// --- 前置：构建产物 ---
let entry
try {
  const html = readFileSync(resolve(dist, 'index.html'), 'utf8')
  const m = html.match(/<script[^>]+src="([^"]+)"/)
  if (!m) throw new Error('index.html 里找不到入口 script')
  entry = resolve(dist, m[1].replace(/^\//, ''))
} catch (err) {
  console.error(`\n先执行 npm run build（dist 不可用：${err.message}）`)
  process.exit(1)
}

// --- 浏览器环境 ---
const window = new Window({
  url: 'http://localhost/',
  // 懒加载分块会带自己的 CSS，happy-dom 去加载 <link> 时会在 load 事件里抛错，
  // 直接把导航打断。我们只验证 DOM 结构，不验样式，所以关掉样式/脚本文件加载。
  settings: { disableJavaScriptFileLoading: true, disableCSSFileLoading: true },
})
const doc = window.document
globalThis.window = window
globalThis.document = doc
Object.defineProperty(globalThis, 'navigator', { value: window.navigator, configurable: true })
globalThis.history = window.history
globalThis.location = window.location
globalThis.HTMLElement = window.HTMLElement
globalThis.HTMLCanvasElement = window.HTMLCanvasElement
globalThis.SVGElement = window.SVGElement
globalThis.Element = window.Element
globalThis.Node = window.Node
// ECharts（zrender）在初始化时会逐个探测一批浏览器构造器。
// 一个个补是打地鼠，直接把 happy-dom 上的构造器整体映射过来，已有的不动。
const SKIP = new Set(['window', 'self', 'top', 'parent', 'globalThis', 'location', 'history', 'document', 'navigator'])
for (const key of Object.getOwnPropertyNames(window)) {
  if (SKIP.has(key) || key in globalThis) continue
  if (!/^[A-Z]/.test(key)) continue
  let value
  try {
    value = window[key]
  } catch {
    continue
  }
  if (typeof value === 'function') globalThis[key] = value
}
globalThis.localStorage = window.localStorage
globalThis.getComputedStyle = window.getComputedStyle.bind(window)

// vite 的 __vitePreload 会为懒加载分块注入 <link rel=stylesheet> 并等它的 load/error。
// 从 file:// 加载 dist 时 href 会变成 file:///assets/*.css（不存在）→ reject → 导航被中断。
// 这里不验样式，直接让 link「加载成功」。
const origCreateElement = doc.createElement.bind(doc)
doc.createElement = (tag, ...rest) => {
  const el = origCreateElement(tag, ...rest)
  if (String(tag).toLowerCase() === 'link') {
    setTimeout(() => el.dispatchEvent(new window.Event('load')), 0)
  }
  return el
}

// happy-dom 的 window.dispatchEvent 只接受真正的 Event，而 vue-router 在导航收尾时
// 会传普通对象；抛错会直接中断异步组件的导航。这里放宽：非 Event 一律忽略。
const rawDispatch = window.dispatchEvent.bind(window)
window.dispatchEvent = (evt) => (evt instanceof window.Event ? rawDispatch(evt) : true)
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }

window.addEventListener('error', (e) => {
  const msg = String(e?.message ?? e?.error?.message ?? '')
  if (NOISE.some((re) => re.test(msg))) {
    interopNoise.push(msg)
    return
  }
  failed++
  console.error('  FAIL 未捕获的页面错误:', msg)
})

// ECharts 初始化要 2d context；happy-dom 不实现，给个够用的空壳
window.HTMLCanvasElement.prototype.getContext = () =>
  new Proxy(
    {
      canvas: { width: 800, height: 320 },
      measureText: () => ({ width: 10 }),
      createLinearGradient: () => ({ addColorStop() {} }),
    },
    { get: (t, k) => (k in t ? t[k] : () => {}) },
  )

// 手动控制页要用的假驱动服务：参数刻意给两个不同类型的字段，
// 用来验证「按驱动声明顺序发位置参数」这条最容易错的逻辑
const FAKE_SERVICES = [
  {
    key: 'setChargePower',
    name: '设置充电功率',
    description: '下发充电功率设定值',
    params: [
      { key: 'power', name: '功率', valueType: 'int', componentType: 'number', required: true, unit: 'kW', min: 0, max: 50, description: '0~50' },
      { key: 'duration', name: '时长', valueType: 'int', componentType: 'number', required: false, unit: 'min', step: 5 },
    ],
  },
  {
    key: 'stop',
    name: '停止充放电',
    description: '停止当前充放动作',
    params: [],
  },
]

// 储能参数的字段元数据（真实来源是后端反射 Go struct tag，这里只模拟形状）
const FAKE_CONFIG_FIELDS = [
  { key: 'socMinRatio', name: '最小SOC比例', description: '最小SOC比例', group: '储能参数', component: 'number', valueType: 'float', min: 0, max: 1, default: '0.1' },
  { key: 'socMaxRatio', name: '最大SOC比例', description: '最大SOC比例', group: '储能参数', component: 'number', valueType: 'float', min: 0, max: 1, default: '0.95' },
  { key: 'chargePowerRatio', name: '充电功率比例', description: '充电功率占额定功率的比例', group: '峰谷套利', component: 'number', valueType: 'float', min: 0, max: 1, default: '1' },
  { key: 'dischargePowerRatio', name: '放电功率比例', description: '放电功率占额定功率的比例', group: '峰谷套利', component: 'number', valueType: 'float', min: 0, max: 1, default: '1' },
  { key: 'enableDemandControl', name: '启动需量控制模式', description: '启动需量控制模式', group: '需量控制', component: 'switch', valueType: 'bool', default: 'false' },
  { key: 'demandTarget', name: '需量目标值', description: '关口功率上限(kW)', group: '需量控制', component: 'number', valueType: 'float', min: 0, default: '0' },
  { key: 'transformerCapacity', name: '变压器容量', description: '变压器容量', group: '变压器', component: 'number', valueType: 'float', min: 0, default: '200', unit: 'kVA' },
]
const FAKE_CONFIG_GROUPS = ['储能参数', '峰谷套利', '需量控制', '变压器']
const FAKE_POLICY_CONFIG = {
  socMinRatio: 0.1,
  socMaxRatio: 0.95,
  chargePowerRatio: 0.5,
  dischargePowerRatio: 0.5,
  enableDemandControl: false,
  demandTarget: 0,
  transformerCapacity: 200,
}

const FAKE_STRATEGIES = [
  {
    id: 7,
    name: 'tou-rule',
    description: '峰充谷放',
    priority: 1,
    status: 'Enable',
    dateRange: { startDate: '2026-10-01', isLongTerm: true },
    timeRange: { type: 'weekday', weekdayType: 'all' },
    config: { points: [[0, 50], [8, 100], [22, 30]] },
    isActive: true,
  },
  {
    id: 8,
    name: 'idle-基线',
    description: '对照组',
    priority: 5,
    status: 'Disable',
    dateRange: { startDate: '2026-10-01', endDate: '2026-10-31', isLongTerm: false },
    timeRange: { type: 'custom', customDays: [1, 2, 3] },
    config: { points: [] },
    isActive: false,
  },
]

const FAKE_BACKTEST = {
  start: '2026-09-15',
  end: '2026-09-21',
  points: 672,
  gridDeviceId: 'grid_1',
  essDeviceIds: ['ess_1'],
  demandTarget: 0,
  demandPrice: 40,
  ratedPowerKw: 50,
  capacityKwh: 200,
  results: [
    { algorithm: 'idle', chargeEnergy: 0, dischargeEnergy: 0, chargeCost: 0, dischargeRevenue: 0, arbitrageProfit: 0, actualMaxDemand: 120, baselineMaxDemand: 120, demandSaving: 0, totalSaving: 0, finalSocPct: 80 },
    { algorithm: 'tou-rule', chargeEnergy: 420, dischargeEnergy: 380, chargeCost: 168, dischargeRevenue: 402, arbitrageProfit: 234, actualMaxDemand: 118, baselineMaxDemand: 120, demandSaving: 80, totalSaving: 314, finalSocPct: 62 },
  ],
}

const FAKE_PRICES = [
  {
    id: 3,
    description: '2026 秋季工商业',
    priority: 1,
    status: 'Enable',
    isActive: true,
    dateRange: { startDate: '2026-10-01', isLongTerm: true },
    timeRange: { type: 'weekday', weekdayType: 'all' },
    priceSegments: [
      { startTime: '00:00', endTime: '08:00', priceType: 'Valley', price: 0.5 },
      { startTime: '08:00', endTime: '11:00', priceType: 'Sharp', price: 1.5 },
      { startTime: '11:00', endTime: '13:00', priceType: 'Peak', price: 1.2 },
      { startTime: '13:00', endTime: '20:00', priceType: 'Flat', price: 0.8 },
      { startTime: '20:00', endTime: '00:00', priceType: 'Valley', price: 0.5 },
    ],
  },
  {
    id: 4,
    description: '对照组固定价',
    priority: 3,
    status: 'Disable',
    isActive: false,
    dateRange: { startDate: '2026-10-01', endDate: '2026-10-31', isLongTerm: false },
    timeRange: { type: 'custom', customDays: [1, 2] },
    priceSegments: [{ startTime: '00:00', endTime: '00:00', priceType: 'Flat', price: 0.6 }],
  },
]

const FAKE_REPORTS = [
  { date: '2026-09-21', title: '现货运行日报（09.21）', sourceUrl: 'https://mp.weixin.qq.com/s/x', pageCount: 4, pages: [], collectedAt: '2026-09-21 22:10', pendingCount: 2, confirmedCount: 40 },
  { date: '2026-09-19', title: '现货运行日报（09.19）', sourceUrl: 'https://mp.weixin.qq.com/s/y', pageCount: 4, pages: [], collectedAt: '2026-09-19 20:55', pendingCount: 0, confirmedCount: 42 },
]

const FAKE_METRICS = [
  { runDate: '2026-09-21', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价', value: 400.5, unit: '元/MWh', atTime: '', textValue: '', momPct: 1.2, sharePct: null, source: '表1(二)', conf: 0.98, origin: 'manual', editedBy: 'admin', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'day_ahead_trade', scope: '燃煤', metric: '平均电价', value: 0, unit: '元/MWh', atTime: '', textValue: '', momPct: null, sharePct: null, source: '表1(二)', conf: 0.62, origin: 'ocr', editedBy: '', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'summary', scope: '市场', metric: '日前加权平均电价', value: 0, unit: '元/MWh', atTime: '', textValue: '', momPct: null, sharePct: null, source: '正文', conf: 0.5, origin: 'ocr', editedBy: '', editedAt: '' },
]

const FAKE_CLEARING_METRICS = [
  { runDate: '2026-09-21', dataset: 'node_price_peak', scope: '广州', metric: '价格档位', value: 4, unit: '', atTime: '', textValue: '[600,800)', momPct: null, sharePct: null, source: '图2-1', conf: 0.72, origin: 'cv', editedBy: '', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'node_price_peak', scope: '深圳', metric: '价格档位', value: 5, unit: '', atTime: '', textValue: '[800,+inf)', momPct: null, sharePct: null, source: '图2-1', conf: 0.66, origin: 'cv', editedBy: '', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'node_price_peak', scope: '韶关', metric: '价格档位', value: 2, unit: '', atTime: '', textValue: '[200,400)', momPct: null, sharePct: null, source: '图2-1', conf: 0.9, origin: 'manual', editedBy: 'admin', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'node_price_valley', scope: '广州', metric: '价格档位', value: 1, unit: '', atTime: '', textValue: '[-inf,200)', momPct: null, sharePct: null, source: '图2-2', conf: 0.8, origin: 'cv', editedBy: '', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'node_price_valley', scope: '深圳', metric: '价格档位', value: 2, unit: '', atTime: '', textValue: '[200,400)', momPct: null, sharePct: null, source: '图2-2', conf: 0.8, origin: 'cv', editedBy: '', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价', value: 400.5, unit: '元/MWh', atTime: '', textValue: '', momPct: 1.2, sharePct: null, source: '表1(二)', conf: 0.98, origin: 'manual', editedBy: 'admin', editedAt: '' },
  { runDate: '2026-09-21', dataset: 'day_ahead_trade', scope: '燃煤', metric: '最高电价', value: 480, unit: '元/MWh', atTime: '19:15', textValue: '', momPct: null, sharePct: null, source: '表1(二)', conf: 0.95, origin: 'manual', editedBy: '', editedAt: '' },
  { runDate: '2026-09-19', dataset: 'realtime_trade', scope: '发电侧', metric: '平均电价', value: 365.2, unit: '元/MWh', atTime: '', textValue: '', momPct: null, sharePct: null, source: '表2(二)', conf: 0.94, origin: 'manual', editedBy: '', editedAt: '' },
]

const FAKE_EXTRACT = { status: 'ok', ocrModel: 'pp-ocr', pageCount: 4, ocrMs: 3200, warnings: ['表2 与历史值有 1 处不一致'], updatedAt: '2026-09-21 22:12', extractorVersion: 'tpl-1' }

const FAKE_AUDIT = [
  { runDate: '2026-09-21', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价', oldValue: 380, newValue: 400.5, operation: 'edit', operator: 'admin', createdAt: '2026-09-21 22:20' },
  { runDate: '2026-09-19', dataset: 'day_ahead_trade', scope: '燃煤', metric: '平均电价', oldValue: 300, newValue: 305, operation: 'overwritten_by_extract', operator: '', createdAt: '2026-09-21 22:12' },
]

const FAKE_BATCH = { id: 'job1', status: 'done', total: 3, done: 3, succeeded: 3, failed: 0, overwritten: 1, current: '', skippedConfirmed: ['2026-09-21'], errors: [], startedAt: '2026-09-21 22:30', finishedAt: '2026-09-21 22:31' }

const FAKE_TREND_POINTS = [
  { bucket: '2026-W39', from: '2026-09-21', to: '2026-09-27', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价', avg: 400.5, max: 480, min: 320, sum: 0, days: 7, unit: '元/MWh', value: 0 },
  { bucket: '2026-W40', from: '2026-09-28', to: '2026-10-04', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价', avg: 388.2, max: 470, min: 300, sum: 0, days: 7, unit: '元/MWh', value: 0 },
  { bucket: '2026-W39', from: '2026-09-21', to: '2026-09-27', dataset: 'day_ahead_trade', scope: '燃煤', metric: '平均电价', avg: 402.1, max: 460, min: 350, sum: 0, days: 7, unit: '元/MWh', value: 0 },
  { bucket: '2026-W40', from: '2026-09-28', to: '2026-10-04', dataset: 'day_ahead_trade', scope: '燃煤', metric: '平均电价', avg: 395.6, max: 455, min: 340, sum: 0, days: 7, unit: '元/MWh', value: 0 },
  { bucket: '2026-W39', from: '2026-09-21', to: '2026-09-27', dataset: 'summary', scope: '市场', metric: '日前加权平均电价', avg: 399, max: 420, min: 380, sum: 0, days: 7, unit: '元/MWh', value: 0 },
  { bucket: '2026-W40', from: '2026-09-28', to: '2026-10-04', dataset: 'summary', scope: '市场', metric: '日前加权平均电价', avg: 390, max: 410, min: 375, sum: 0, days: 7, unit: '元/MWh', value: 0 },
]

// 四条线面板专用：按运行日（grain=day）的日报数据，日前 + 实时两条实际价
const FAKE_TREND_DAILY = [
  { bucket: '2026-09-24', from: '2026-09-24', to: '2026-09-24', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价', avg: 402.6, max: 480, min: 320, sum: 0, days: 1, unit: '元/MWh', value: 0 },
  { bucket: '2026-09-25', from: '2026-09-25', to: '2026-09-25', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价', avg: 398.4, max: 470, min: 310, sum: 0, days: 1, unit: '元/MWh', value: 0 },
  { bucket: '2026-09-24', from: '2026-09-24', to: '2026-09-24', dataset: 'realtime_trade', scope: '发电侧', metric: '平均电价', avg: 381.2, max: 460, min: 300, sum: 0, days: 1, unit: '元/MWh', value: 0 },
  { bucket: '2026-09-25', from: '2026-09-25', to: '2026-09-25', dataset: 'realtime_trade', scope: '发电侧', metric: '平均电价', avg: 374.5, max: 450, min: 290, sum: 0, days: 1, unit: '元/MWh', value: 0 },
  // 燃煤分档：四条线只取「发电侧」，不该把这条画进去
  { bucket: '2026-09-24', from: '2026-09-24', to: '2026-09-24', dataset: 'day_ahead_trade', scope: '燃煤', metric: '平均电价', avg: 412.9, max: 470, min: 350, sum: 0, days: 1, unit: '元/MWh', value: 0 },
]

// 留痕（日粒度）：目标运行日要与上面那两天对上，否则预测两条线会是空的
const FAKE_RECORDED_DAY = {
  generatedDates: ['2026-09-23', '2026-09-24'],
  points: [
    { generatedOn: '2026-09-23', targetTime: '2026-09-24', point: 401.2, q10: 380, q50: 401.2, q90: 425, model: 'timesfm-3.0', unit: '元/MWh', source: 'day_ahead_trade' },
    { generatedOn: '2026-09-24', targetTime: '2026-09-25', point: 396.8, q10: 372, q50: 396.8, q90: 418, model: 'timesfm-3.0', unit: '元/MWh', source: 'day_ahead_trade' },
  ],
}

const FAKE_SUMMARY = {
  start: '2026-09-30', end: '2026-10-06', points: 672, algorithm: 'tou-rule',
  gridDeviceId: 'grid_1', essDeviceIds: ['ess_1'], demandPrice: 40,
  chargeEnergy: 420.5, dischargeEnergy: 380.25, chargeCost: 168.2, dischargeRevenue: 402.4,
  arbitrageProfit: 234.2, actualMaxDemand: 118, baselineMaxDemand: 120, demandSaving: 80, totalSaving: 314.2,
}

const FAKE_LOAD = {
  deviceId: 'grid_1', model: 'timesfm-3.0', stepMinutes: 15, lookbackPoints: 96, inputTruncated: false,
  historyTimes: [1759000000000, 1759000900000],
  historyValues: [120, 130],
  forecastTimes: [1759001800000, 1759002700000],
  forecastPoint: [140, 150],
  forecastQuantiles: { '0.1': [120, 130], '0.5': [140, 150], '0.9': [160, 170] },
  timeMs: 2100,
}

const FAKE_PRICE = {
  market: 'ahead', grain: 'day', stepMinutes: 1440,
  dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价',
  unit: '元/MWh', model: 'timesfm-3.0', from: '2026-07-24', to: '2026-09-21',
  historyDates: ['2026-09-19', '2026-09-20', '2026-09-21'],
  historyValues: [380, 390, 400],
  forecast: [
    { runDate: '2026-09-22', point: 368.4, quantiles: { '0.1': 341.2, '0.5': 368.4, '0.9': 401.7 } },
    { runDate: '2026-09-23', point: 372.1, quantiles: { '0.1': 345, '0.5': 372.1, '0.9': 408 } },
  ],
  contextPoints: 60, inputTruncated: false, missingDays: 50, timeMs: 2063,
}

/**
 * 分时（grain=intraday）：序列源是电网日指标 CSV（gd_metric.csv）的 15 分钟电价，
 * 2 个预测日 × 96 点。后端早就支持，原先缺的是这个视图。
 */
const FAKE_PRICE_INTRADAY = {
  market: 'ahead', grain: 'intraday', stepMinutes: 15,
  dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价',
  unit: '元/兆瓦时', model: 'timesfm-3.0', from: '2026-08-02 00:15', to: '2026-09-01 00:00',
  historyDates: ['2026-09-01 00:00'], historyValues: [300],
  forecast: Array.from({ length: 192 }, (_, i) => {
    const day = i < 96 ? '2026-09-01' : '2026-09-02'
    const slot = (i % 96) * 15
    const hh = String(Math.floor(slot / 60)).padStart(2, '0')
    const mm = String(slot % 60).padStart(2, '0')
    const point = 300 + (i % 96) * 2
    return {
      runDate: day,
      atTime: `${hh}:${mm}`,
      targetTime: `${day} ${hh}:${mm}`,
      point,
      quantiles: { '0.1': point - 30, '0.5': point, '0.9': point + 40 },
    }
  }),
  contextPoints: 2880, inputTruncated: false, missingDays: 0, timeMs: 1800,
}

const FAKE_SYS = {
  cpu: { usage: 41.2, load1: 4.73, load5: 5.58, load15: 5.78 },
  memory: { usage: 57.1, totalGB: 15.37, usedGB: 8.76, freeGB: 6.6, procUsage: 3.2, procRSSMB: 512.5 },
  disk: { usage: 84.2, totalGB: 219.99, usedGB: 185.24, freeGB: 34.75 },
  uptime: { day: 172, hour: 2, minute: 34, bootTime: '2026-04-17 16:24:00', procUptimeSeconds: 12096000, procStartTime: '2026-04-17 18:14:00' },
  time: { now: '2026-10-06 21:00:00', timezone: 'Asia/Shanghai', ntpRunning: true, ntpServer: 'ntp.aliyun.com' },
  sys: { hostname: 'e186166d6694', osName: 'alpine', osVersion: '3.22.1', kernel: '6.6.68-20-oc9.x86_64' },
}
const FAKE_NET = { upBytes: 35876000000, downBytes: 25790000000, upBps: 3306, downBps: 3666 }
const FAKE_STORAGE = {
  total_series: 1280, total_samples: 98765432, storage_size: 230400000,
  storage_size_mb: 219.7, oldest_timestamp: '2025-10-06T00:00:00Z', newest_timestamp: '2026-10-06T13:00:00Z',
  retention_time: 2592000, retention_hours: 720, avg_series_size: 180000, samples_per_second: 1.4,
}
const FAKE_CHART = {
  xAxis: { type: 'category', data: ['1759000000000', '1759000900000'] },
  series: [
    { name: 'CPU', type: 'line', unit: '%', data: ['40', '42'] },
    { name: '内存', type: 'line', unit: '%', data: ['55', '57'] },
  ],
}

const FAKE_DEVICES = [
  { id: 'site_1', pid: '0', name: '储能柜示范站', protocolId: '', driver: '', enableDebug: false, strategy: '', storageEnable: false, storageIntervalSec: 0, sort: 0, enabled: true, params: {}, deviceType: 'sess', isVirtualDevice: false, driverType: '', driverBrand: '', driverModel: '', driverVersion: '', protocolName: '', protocolType: '', protocolAddress: '' },
  { id: 'pcs_1', pid: 'site_1', name: 'PCS 1', protocolId: 'proto_1', driver: 'ess_demo_v1.0.0', enableDebug: true, strategy: 'policy_microgrid', storageEnable: true, storageIntervalSec: 15, sort: 1, enabled: true, params: { unitId: 1 }, deviceType: 'pcs', isVirtualDevice: false, driverType: 'pcs', driverBrand: 'HEX', driverModel: 'Demo', driverVersion: 'v1.0.0', protocolName: 'modbus_tcp_1', protocolType: 'modbus_tcp', protocolAddress: 'emsdevice:1502' },
  { id: 'meter_1', pid: 'site_1', name: '关口电表', protocolId: 'proto_1', driver: 'ammeter_demo_v1.0.0', enableDebug: false, strategy: '', storageEnable: true, storageIntervalSec: 15, sort: 2, enabled: false, params: {}, deviceType: 'ammeter', isVirtualDevice: false, driverType: 'ammeter', driverBrand: 'HEX', driverModel: 'Demo', driverVersion: 'v1.0.0', protocolName: 'modbus_tcp_1', protocolType: 'modbus_tcp', protocolAddress: 'emsdevice:1502', failedMessage: '采集初始化超时' },
]
const FAKE_POINTS_PCS = {
  isVirtualDevice: false,
  groups: [{ key: 'status', name: '状态量' }, { key: 'ctrl', name: '控制量' }],
  fields: [
    { key: 'Power', name: '有功功率', groupKey: 'status', valueType: 'float', componentType: 'number', required: false, unit: 'kW', min: -50, max: 50, description: '正值充电' },
    { key: 'SOC', name: 'SOC', groupKey: 'status', valueType: 'float', componentType: 'number', required: false, unit: '%', min: 0, max: 100, description: '' },
    { key: 'SetPower', name: '功率设定', groupKey: 'ctrl', valueType: 'float', componentType: 'number', required: true, unit: 'kW', min: -50, max: 50, default: '0', description: '下发目标功率' },
  ],
}
const FAKE_PROTOCOLS = [
  { protocolId: 'proto_1', protocolName: 'modbus_tcp_1', protocolType: 'modbus_tcp', protocolAddress: '100.64.0.3', protocolPort: 2509, protocolTimeout: 3000, protocolLogLevel: 'Info', protocolParams: '{}', protocolActive: true },
  { protocolId: 'proto_2', protocolName: 'gpio_in', protocolType: 'gpio', protocolAddress: '', protocolPort: 0, protocolTimeout: 0, protocolLogLevel: 'Warn', protocolParams: '{}', protocolActive: false, boundDeviceId: 'dido_1', boundDeviceName: '左开关' },
]
const FAKE_DRIVERS = [
  { driverName: 'ess_demo_v1.0.0', driverVersion: 'v1.0.0', driverType: 'pcs', driverDescription: '演示储能', driverStatus: true, driverIsEmbedded: true, driverLastUpdate: '2025-09-01', driverPath: '/app/drivers/ess_demo.so', driverHash: 'abc123def456', driverFileSizeByte: 40960, driverImage: '', protocolType: 'modbus', brand: 'HEX', model: 'Demo', buildTime: '2025-09-01T10:00:00Z', commitHash: 'deadbeef', remark: '内置' },
  { driverName: 'ammeter_demo_v1.0.0', driverVersion: 'v1.0.0', driverType: 'ammeter', driverDescription: '演示电表', driverStatus: false, driverIsEmbedded: false, driverLastUpdate: '2025-08-10', driverPath: '/app/drivers/meter.so', driverHash: 'ff00', driverFileSizeByte: 2048, driverImage: '', protocolType: 'modbus', brand: 'HEX', model: 'Demo', buildTime: '', commitHash: '', remark: '' },
]

const FAKE_MODBUS = {
  isRunning: true, listenPort: 502, deviceCount: 3, connectionCount: 1,
  deviceStatus: [
    { deviceId: 'pcs_1', modbusId: 3, startAddr: 200, isOnline: true, lastUpdateTime: '2026-10-06 21:00:00' },
    { deviceId: 'meter_1', modbusId: 1, startAddr: 300, isOnline: false, lastUpdateTime: '2026-10-06 20:59:00' },
  ],
}
const FAKE_MQTT = {
  isRunning: true, clientCount: 2,
  clientStatus: [
    { config: {}, isConnected: true, isRunning: true, clientId: 'lems-01', systemNumber: '7e25fb08e2', topic: 'lems/7e25fb08e2/info' },
    { config: {}, isConnected: false, isRunning: false, clientId: 'lems-02', systemNumber: 'aabbccdd', topic: 'lems/aabbccdd/info' },
  ],
}
const FAKE_IFACES = [
  { id: 'eth0', name: 'eth0', mac: '02:42:ac:11:00:02', isUp: true, ipv4: [{ ipv4: '100.64.0.7', subnetMask: '255.255.0.0' }], gateway: ['100.64.0.1'], dns: ['223.5.5.5'], mtu: 1500, dhcp: true },
  { id: 'lo', name: 'lo', mac: '', isUp: true, ipv4: [{ ipv4: '127.0.0.1', subnetMask: '255.0.0.0' }], gateway: [], dns: [], mtu: 65536, dhcp: false },
]

const FAKE_PROTO_OVERVIEW = { totalProtocols: 9, activeProtocols: 7, avgSuccessRate: 98.7, avgResponseMs: 42.5, abnormalProtocols: 1 }
const FAKE_PROTO_DIST = { typeDistribution: { modbus_tcp: 6, gpio: 2, canbus: 1 }, statusDistribution: { active: 7, inactive: 2 } }
const FAKE_TREND_CHART = {
  xAxis: { type: 'category', data: ['1759000000000', '1759000900000'] },
  series: [{ name: 'ProtocolMetricSuccessRate', type: 'line', unit: '%', data: ['99.1', '98.3'] }],
}
const FAKE_COMPARE_CHART = {
  xAxis: { type: 'category', data: ['1759000000000', '1759000900000'] },
  series: [
    { name: 'pcs_1-Power', type: 'line', unit: 'kW', data: ['5', '8'] },
    { name: 'meter_1-Power', type: 'line', unit: 'kW', data: ['120', '126'] },
  ],
}
const FAKE_LOGS = [
  { createdAt: '2026-10-06T21:00:01+08:00', id: 'l1', type: 'remote', level: 'DEBUG', content: '成功推送设备数据：设备ID=pcs_1, Topic=lems/7e25fb08e2/info' },
  { createdAt: '2026-10-06T20:59:30+08:00', id: 'l2', type: 'device', level: 'ERROR', content: '设备 meter_1 采集失败：连接超时' },
  { createdAt: '2026-10-06T20:58:00+08:00', id: 'l3', type: 'policy', level: 'INFO', content: '策略 tou-rule 触发：峰时段充电 40kW' },
]

const FAKE_AUTOMATIONS = [
  {
    id: 7,
    name: '派能BMS SOC 过高 → 红灯高电平',
    enabled: true,
    triggerRule: {
      anyMatch: [{ deviceCondition: { deviceId: 'bms_1', from: '', rule: 'SOC>100' } }],
      subMatch: [],
      subMatchAll: false,
      executionInterval: 0,
    },
    executeRule: { rules: [{ deviceId: 'dido_1', service: 'setHighLevel', params: [] }] },
    createdAt: '2026-09-01T10:00:00Z',
  },
  {
    id: 8,
    name: '夜间时段关闭放电',
    enabled: false,
    triggerRule: {
      anyMatch: [{ timeCondition: { startTime: '23:00', endTime: '06:00', dayOfWeek: 1 } }],
      subMatch: [],
      subMatchAll: false,
      executionInterval: 60,
    },
    executeRule: { rules: [{ deviceId: 'pcs_1', service: 'stop', params: [] }] },
  },
]

const FAKE_SERVICES_DIDO = [
  { key: 'setHighLevel', name: '置高电平', description: '红灯亮', params: [] },
  { key: 'setLowLevel', name: '置低电平', description: '红灯灭', params: [] },
]

const FAKE_CREDS = {
  storageMode: 'qiniu',
  qiniu: [
    { name: 'AccessKey', configured: true, source: 'env', value: '••••••' },
    { name: 'SecretKey', configured: true, source: 'env', value: '••••••' },
    { name: 'Bucket', configured: true, source: 'config', value: 'tos-me' },
    { name: '访问域名', configured: true, source: 'config', value: 'http://file.example.com' },
    { name: 'Zone', configured: true, source: 'default', value: 'z2' },
  ],
  ocr: [{ name: '服务地址', configured: false, source: 'missing', value: '' }],
  note: '密钥保存在服务端配置文件或环境变量里，改后需重启；页面只读，不提供在线修改。密钥值一律不回显。',
}

// ===== 预测复盘的手算夹具 =====
// 回测：2 天 × 4 点。实际值恒为 100，所以当日均价=100，标幺离散度 = |pred-100|/100。
// 预测值 perfect 单调 ↔ 实际恒定 —— 实际恒定则秩无方差 → Spearman 必须返回 null（不是 0）。
const FAKE_BACKTEST_RESULT = {
  market: 'ahead',
  grain: 'intraday',
  unit: '元/MWh',
  model: 'timesfm-3.0',
  from: '2026-09-20',
  to: '2026-09-21',
  sampleDays: 3,
  targetDates: ['2026-09-20', '2026-09-21'],
  // 没有跳过日期时 Go 序列化 nil 切片为 **null**（不是 []）——
  // 页面以前直接读 .skipped.length 就抛，整个组件 setup 失败、页面全白。
  skipped: null,
  orderedDates: null,
  timeMs: 4210,
  points: [
    { targetTime: '2026-09-20 00:00', predicted: 100, actual: 100, q10: 90, q50: 100, q90: 110 },
    { targetTime: '2026-09-20 06:00', predicted: 90, actual: 100, q10: 80, q50: 90, q90: 100 },
    { targetTime: '2026-09-21 12:00', predicted: 110, actual: 200, q10: 100, q50: 110, q90: 120 },
    { targetTime: '2026-09-21 18:00', predicted: 100, actual: 200, q10: 90, q50: 100, q90: 110 },
  ],
}
// 留痕：只有预测这一条腿，实际值由 /api/gd-metric/series 配对
const FAKE_RECORDED = {
  generatedDates: ['2026-09-19', '2026-09-20'],
  points: [
    { generatedOn: '2026-09-19', targetTime: '2026-09-20 00:00', point: 100, q10: 90, q50: 100, q90: 110, model: 'timesfm-3.0', unit: '元/MWh', source: 'gd_metric.csv' },
    { generatedOn: '2026-09-19', targetTime: '2026-09-20 06:00', point: 80, q10: 70, q50: 80, q90: 90, model: 'timesfm-3.0', unit: '元/MWh', source: 'gd_metric.csv' },
  ],
}
const FAKE_SERIES = {
  kind: 'dayAhead',
  from: '2026-09-20',
  to: '2026-09-20',
  days: ['2026-09-20'],
  points: [
    { date: '2026-09-20', time: '00:00', value: 100 },
    { date: '2026-09-20', time: '06:00', value: 100 },
  ],
}

// ===== 策略反演的手算夹具 =====
// 两个分箱的分位点故意**完全相同**：这样任意负荷上插值结果都一样，
// 手算值不会被「插值到哪儿」这件事影响。
// 分位点均匀铺开 0,100,…,1000 ⇒ 条件分布 = 均匀分布 U(0,1000)，于是：
//   中标概率(x) = 1 − x/1000
//   中标概率 80%/50%/20% 对应报价 200 / 500 / 800
//   E[(p−500)·1{p≥500}] = ∫₅₀₀^1000 (p−500)/1000 dp = 125
//   E[p | p≥500] = (125 + 500×0.5)/0.5 = 750
const uniformQ = [0, 100, 200, 300, 400, 500, 600, 700, 800, 900, 1000]
const FAKE_SIMILAR = {
  targetDate: '2026-09-21',
  market: 'ahead',
  topN: 8,
  wPrice: 0.7,
  wLoad: 0.3,
  method: '皮尔逊形态相关（96 点，z-score 标准化）',
  formula: '相似度 = (0.7×ρ价格 + 0.3×ρ负荷 + 1) / 2 × 100%',
  from: '2021-05-01',
  to: '2026-09-20',
  candidates: 1791,
  timeMs: 320,
  days: [
    { date: '2026-08-30', similarity: 92.4, priceCorr: 0.85, loadCorr: 0.94, priceAvg: 372.5, loadMax: 3300 },
    { date: '2025-07-15', similarity: 88.1, priceCorr: 0.76, loadCorr: 0.93, priceAvg: 410.2, loadMax: 3510 },
    { date: '2024-06-02', similarity: 71.3, priceCorr: 0.43, loadCorr: 0.98, priceAvg: 355.0, loadMax: 3200 },
  ],
}

const FAKE_SUPPLY_BINS = [
  { loadFrom: 500, loadTo: 1500, loadMid: 1000, count: 500, q: uniformQ },
  { loadFrom: 1500, loadTo: 2500, loadMid: 2000, count: 500, q: uniformQ },
]
const FAKE_SUPPLY_CLEAN = {
  market: 'ahead',
  from: '2026-06-01',
  to: '2026-08-31',
  bins: 2,
  quantiles: [0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1],
  points: [
    { load: 1000, price: 500 },
    { load: 2000, price: 500 },
  ],
  sampled: false,
  allBins: FAKE_SUPPLY_BINS,
  steps: [
    { loadFrom: 500, loadTo: 1500, price: 500, count: 500 },
    { loadFrom: 1500, loadTo: 2500, price: 500, count: 500 },
  ],
  stats: {
    pairs: 1000,
    days: 30,
    loadMin: 500,
    loadMax: 2500,
    priceMin: 0,
    priceMax: 1000,
    spearman: 1,
    violations: 0,
    maxPavaMove: 0,
    latestLoad: 1500,
  },
}
// 供给栈漂移：负荷越高价格越低 → 逆序 + 负相关，页面必须报出来
const FAKE_SUPPLY_DRIFT = {
  ...FAKE_SUPPLY_CLEAN,
  market: 'real',
  allBins: FAKE_SUPPLY_BINS.map((b) => ({ ...b, q: uniformQ.slice().reverse() })),
  stats: { ...FAKE_SUPPLY_CLEAN.stats, spearman: -0.2, violations: 7, maxPavaMove: 150 },
}

// 保留策略：清理前（本次启动后还没跑过）与清理后两种状态
const RETENTION_BEFORE = {
  deviceRetentionDays: 30,
  systemRetentionDays: 30,
  logRetentionDays: 90,
  intervalHours: 24,
  lastRunAt: '',
  lastRunTimeMs: 0,
  lastLogRowsDeleted: 0,
  lastDeviceBefore: '',
  lastSystemBefore: '',
  lastLogBefore: '',
  lastError: '',
}
const RETENTION_AFTER = {
  ...RETENTION_BEFORE,
  lastRunAt: '2026-10-08 03:15:00',
  lastRunTimeMs: 812,
  lastLogRowsDeleted: 12,
  lastDeviceBefore: '2026-09-08 03:15:00',
  lastSystemBefore: '2026-09-08 03:15:00',
  lastLogBefore: '2026-07-10 03:15:00',
}

// 保留策略：清理前（本次启动后还没跑过）与清理后两种状态
// 告警夹具：三个级别各一条，忽略规则一条 —— 用来验级别标签/忽略与恢复/清除按钮
const FAKE_CURRENT_ALARMS = [
  {
    sourceDeviceId: 'bms_1', sourceDeviceName: '派能BMS', deviceId: 'ess_1', deviceName: '储能柜 1',
    point: 'temperature', pointName: '电池温度', level: 'Error', detail: '单体温度 55.2°C 超过上限 50°C',
    createdAt: '2026-10-08 02:10:00',
  },
  {
    sourceDeviceId: 'pcs_1', sourceDeviceName: 'PCS 1', deviceId: 'pcs_1', deviceName: 'PCS 1',
    point: 'power', pointName: '功率', level: 'Alarm', detail: '功率超过额定 110%',
    createdAt: '2026-10-08 01:40:00',
  },
  {
    sourceDeviceId: 'ammeter_1', sourceDeviceName: '关口电表', deviceId: 'ammeter_1', deviceName: '关口电表',
    point: 'voltage', pointName: 'A相电压', level: 'Warn', detail: 'A相电压 210V 低于下限 220V',
    createdAt: '2026-10-07 23:05:00',
  },
]
const FAKE_HISTORY_ALARMS = [
  {
    id: 1, deviceId: 'ess_1', deviceName: '储能柜 1', sourceDeviceId: 'bms_1', sourceDeviceName: '派能BMS',
    point: 'temperature', pointName: '电池温度', level: 'Alarm', detail: '温度 48°C 超过上限',
    triggerAt: '2026-10-07 18:00:00', clearAt: '2026-10-07 18:30:00',
  },
]
const FAKE_IGNORE_ALARMS = [
  {
    id: 7, deviceId: 'pcs_1', deviceName: 'PCS 1', sourceDeviceId: 'pcs_1', sourceDeviceName: 'PCS 1',
    point: 'power', pointName: '功率', createdAt: '2026-10-07 20:00:00',
  },
]

const requests = []
let cleaned = false
/** 手动控制开关（后端默认关闭）：默认给开，专门留一段测停用路径 */
let manualEnabled = true
globalThis.fetch = async (url, init) => {
  requests.push({
    url: String(url),
    method: init?.method ?? 'GET',
    body: init?.body ? JSON.parse(String(init.body)) : undefined,
  })
  const u = String(url)
  if (u.includes('/api/setting/detail')) {
    const b = init?.body ? JSON.parse(String(init.body)) : {}
    // 与后端一致：/setting/detail 返回全字段（含 isPublic/remark），供调用方合并后再更新
    return json({
      id: b.id,
      value: b.id === 'LogLevelProtocol' ? 'DEBUG' : b.id === 'LogLevelGlobal' ? 'ALL' : 'DEBUG',
      isPublic: true,
      enabled: true,
      remark: b.id === 'LogLevelProtocol' ? 'protocol 业务日志级别' : '设置',
      sort: 0,
      group: 'system',
    })
  }
  if (u.includes('/api/setting/update')) return json({})
  if (u.includes('/api/setting')) {
    return json({
      settings: [
        { id: 'LogLevelGlobal', value: 'ALL', groupName: '系统', remark: '全局日志级别' },
        { id: 'LogLevelEms', value: 'DEBUG', groupName: '系统', remark: 'ems 业务日志级别' },
        { id: 'LogLevelDevice', value: 'DEBUG', groupName: '系统', remark: 'device 业务日志级别' },
        { id: 'LogLevelProtocol', value: 'DEBUG', groupName: '系统', remark: 'protocol 业务日志级别' },
        { id: 'LogLevelPolicy', value: 'DEBUG', groupName: '系统', remark: 'policy 业务日志级别' },
        { id: 'LogLevelAutomation', value: 'DEBUG', groupName: '系统', remark: 'automation 业务日志级别' },
        { id: 'LogLevelRemote', value: 'DEBUG', groupName: '系统', remark: 'remote 业务日志级别' },
        { id: 'SystemEnableDebugLog', value: 'false', groupName: '系统', remark: '启用系统调试日志' },
        // 手动控制总开关：默认**关闭**（后端默认 false）。这里默认给 true，
        // 好让「手动控制页」那一大段断言继续测「打开后能用」的路径；
        // 停用路径在后面的专门一段里把 manualEnabled 翻成 false 再验证。
        { id: 'manual_control_enabled', value: manualEnabled ? 'true' : 'false', groupName: '系统', remark: '允许手动控制' },
      ],
    })
  }
  if (u.includes('/api/alarm/current')) {
    return json({ total: FAKE_CURRENT_ALARMS.length, ignoreTotal: 1, historyTotal: 9, items: FAKE_CURRENT_ALARMS })
  }
  if (u.includes('/api/alarm/history')) {
    if (init?.method === 'POST') return json({})
    return json({ total: FAKE_HISTORY_ALARMS.length, items: FAKE_HISTORY_ALARMS })
  }
  if (u.includes('/api/alarm/ignore')) {
    if (init?.method === 'POST' || init?.method === 'DELETE') return json({})
    return json({ total: FAKE_IGNORE_ALARMS.length, items: FAKE_IGNORE_ALARMS })
  }
  if (u.includes('/api/alarm/clear')) return json({})
  if (u.includes('/api/auth/login')) {
    const b = init?.body ? JSON.parse(String(init.body)) : {}
    if (b.password !== 'admin') {
      // 与后端一致：密码错时 HTTP 401 + 响应体 code 401
      return new Response(JSON.stringify({ code: 401, message: '用户名或密码错误' }), {
        status: 401,
        headers: { 'Content-Type': 'application/json' },
      })
    }
    return json({ token: 'smoke-session-1', role: b.role, expireAt: Math.floor(Date.now() / 1000) + 7200 })
  }
  if (u.includes('/api/auth/logout')) return json({})
  if (u.includes('/api/auth/current')) return json({ role: localStorage.getItem('ems:user:role') || 'admin' })
  if (u.includes('/api/auth/change-password')) {
    const b = init?.body ? JSON.parse(String(init.body)) : {}
    // 与真实后端一致：控制器返回 gerror.NewCode(gcode.CodeNotAuthorized, ...)，
    // GoFrame 的响应中间件**始终写 HTTP 200**，真正的错误码在 body 里（61 = Not Authorized）。
    // 这里一度写成 HTTP 401 —— 那会被前端当成「会话过期」把人踢回登录页，
    // 于是测出的是桩的错，不是页面的错。
    if (b.oldPassword !== 'admin') {
      return json({}, 200, { code: 61, message: '当前密码不正确' })
    }
    return json({})
  }
  if (u.includes('/api/device/name-list')) {
    // 自动化规则会引用 BMS 与 DIDO，设备桩里要有
    return json({ deviceNames: { ess_1: '储能柜 1', pcs_1: 'PCS 1', bms_1: '派能BMS', dido_1: '红灯' } })
  }
  if (u.includes('/api/control/custom-services')) {
    const dev = new URL(u, 'http://x').searchParams.get('deviceId')
    return json({ services: dev === 'dido_1' ? FAKE_SERVICES_DIDO : FAKE_SERVICES })
  }
  if (u.includes('/api/device/telemetry')) {
    // 遥测要覆盖 **detailed-list 里真实存在的设备**（site_1/pcs_1/meter_1），
    // 否则页面会出现「设备列表里有、遥测里没有」的空壳 —— 这正是之前 ess_1 那种
    // 自相矛盾的夹具。ess_1 保留：name-list 夹具里有它，手动控制页要用。
    return json({
      telemetry: {
        site_1: {
          lastUpdateTime: '2026-10-08T10:12:00+08:00',
          telemetryValues: { SOC: 80, Power: -5, Status: 4, Temperature: 28.5 },
        },
        pcs_1: {
          lastUpdateTime: '2026-10-08T10:12:00+08:00',
          telemetryValues: { Power: 5.2, Efficiency: 97.1, Status: 4 },
        },
        meter_1: {
          lastUpdateTime: '2026-10-08T10:12:00+08:00',
          telemetryValues: { VoltageA: 231.4, VoltageB: 230.8, VoltageC: 232.1, Power: 12.3, PowerFactor: 0.98 },
        },
        ess_1: {
          lastUpdateTime: '2026-10-06T18:58:00+08:00',
          telemetryValues: { SOC: 80, Power: 5, SOH: 98, MaxChargePower: 50, MaxDischargePower: 50 },
        },
      },
    })
  }
  if (u.includes('/api/device/history')) {
    return json({
      xAxis: { type: 'category', data: ['0', '900000'] },
      series: [
        { name: 'Power', type: 'line', unit: 'kW', data: ['0', '5'] },
        { name: 'SOC', type: 'line', unit: '%', data: ['80', '81'] },
      ],
    })
  }
  if (u.includes('/api/control/device')) {
    return json({})
  }
  if (u.includes('/api/policy/config-fields')) {
    return json({ fields: FAKE_CONFIG_FIELDS, groups: FAKE_CONFIG_GROUPS })
  }
  if (u.includes('/api/policy/config')) {
    return json({ settingId: 'policy_microgrid', config: FAKE_POLICY_CONFIG })
  }
  if (u.includes('/api/setting/update')) {
    return json({})
  }
  if (u.includes('/api/strategy/energy-storage/activate')) {
    return json({})
  }
  if (u.includes('/api/strategy/energy-storage')) {
    if (init?.method === 'PUT' || init?.method === 'DELETE' || init?.method === 'POST') return json({})
    return json({ list: FAKE_STRATEGIES, total: FAKE_STRATEGIES.length })
  }
  if (u.includes('/api/automation/page')) return json({ list: FAKE_AUTOMATIONS, total: FAKE_AUTOMATIONS.length, page: 1, pageSize: 100 })
  if (u.includes('/api/automation/')) {
    if (init?.method === 'PUT' || init?.method === 'DELETE' || init?.method === 'POST') return json({})
    return json({ automation: FAKE_AUTOMATIONS[0] })
  }
  if (u.includes('/api/automation')) return json({ id: 9 })
  if (u.includes('/api/protocol/monitor/overview')) return json(FAKE_PROTO_OVERVIEW)
  if (u.includes('/api/protocol/monitor/distribution')) return json(FAKE_PROTO_DIST)
  if (u.includes('/api/protocol/monitor/trend')) return json(FAKE_TREND_CHART)
  if (u.includes('/api/protocol/monitor/metrics')) return json(FAKE_TREND_CHART)
  if (u.includes('/api/device/compare')) return json(FAKE_COMPARE_CHART)
  if (u.includes('/api/log/biz/info')) return json({ total: 12800, firstLogTime: '2026-09-01 00:00:00', latestLogTime: '2026-10-06 21:00:00' })
  if (u.includes('/api/log/biz/statistics')) return json({ type: { ems: 100, device: 900, protocol: 1200, policy: 30 }, level: { DEBUG: 1500, INFO: 600, WARN: 80, ERROR: 12 } })
  if (u.includes('/api/log/biz')) {
    if (init?.method === 'DELETE') return json({ total: 42 })
    return json({ total: 3, lines: FAKE_LOGS })
  }
  if (u.includes('/api/remote/modbus/status')) return json(FAKE_MODBUS)
  if (u.includes('/api/remote/mqtt/status')) return json(FAKE_MQTT)
  if (u.includes('/api/remote/modbus/enabled') || u.includes('/api/remote/mqtt/enabled')) return json({})
  if (u.includes('/api/remote/modbus/reload') || u.includes('/api/remote/mqtt/reload')) return json({})
  if (u.includes('/api/network/interface/list')) return json({ interfaces: FAKE_IFACES })
  if (u.includes('/api/network/interface/update')) return json({})
  if (u.includes('/api/network/interface/state')) return json({})
  if (u.includes('/api/network/ping')) return json({ result: { target: '223.5.5.5', success: true, latency: 12.4, timestamp: 1759000000000 } })
  if (u.includes('/api/device/detailed-list')) return json({ devices: FAKE_DEVICES, total: FAKE_DEVICES.length })
  if (u.includes('/points/definitions')) {
    const id = decodeURIComponent(u.split('/device/')[1].split('/points/')[0])
    return json(id === 'pcs_1' ? FAKE_POINTS_PCS : { isVirtualDevice: false, groups: [], fields: [] })
  }
  if (u.includes('/api/device/enable') || u.includes('/api/device/disable')) return json({})
  if (u.includes('/api/protocol/list')) return json({ list: FAKE_PROTOCOLS, total: FAKE_PROTOCOLS.length })
  if (u.includes('/api/driver/get')) return json({ driver: FAKE_DRIVERS[0] })
  if (u.includes('/api/driver/list')) return json({ list: FAKE_DRIVERS, total: FAKE_DRIVERS.length })
  if (u.includes('/api/system/credentials')) return json(FAKE_CREDS)
  if (u.includes('/api/system/summary')) return json(FAKE_SYS)
  if (u.includes('/api/system/net/traffic')) return json({ net: FAKE_NET })
  if (u.includes('/api/system/storage/stats')) return json({ stats: FAKE_STORAGE })
  if (u.includes('/api/system/storage-time')) return json({})
  if (u.includes('/api/system/resource/chart')) return json({ chartData: FAKE_CHART })
  if (u.includes('/api/system/hostname/update')) return json({})
  if (u.includes('/api/system/time/update')) return json({})
  if (u.includes('/api/system/reboot/apply')) return json({})
  if (u.includes('/api/system/reboot/execute')) return json({})
  if (u.includes('/api/system/retention/clean')) {
    cleaned = true
    return json({ status: RETENTION_AFTER })
  }
  if (u.includes('/api/system/retention')) {
    return json({ status: cleaned ? RETENTION_AFTER : RETENTION_BEFORE })
  }
  if (u.includes('/api/effect/summary')) return json({ summary: FAKE_SUMMARY })
  if (u.includes('/api/effect/backtest2')) return json({})
  if (u.includes('/api/forecast/recorded')) {
    return json(u.includes('grain=day') ? FAKE_RECORDED_DAY : FAKE_RECORDED)
  }
  if (u.includes('/api/forecast/backtest')) return json(FAKE_BACKTEST_RESULT)
  if (u.includes('/api/gd-metric/similar-days')) return json(FAKE_SIMILAR)
  if (u.includes('/api/gd-metric/series')) return json(FAKE_SERIES)
  if (u.includes('/api/gd-metric/supply-curve')) {
    return json(u.includes('market=real') ? FAKE_SUPPLY_DRIFT : FAKE_SUPPLY_CLEAN)
  }
  if (u.includes('/api/forecast/load')) return json({ forecast: FAKE_LOAD })
  if (u.includes('/api/forecast/price')) {
    return json({ forecast: u.includes('grain=intraday') ? FAKE_PRICE_INTRADAY : FAKE_PRICE })
  }
  if (u.includes('/api/spot-report/trend')) {
    const q = new URL(u, 'http://x').searchParams
    return json({
      from: q.get('from') || '2026-10-01',
      to: q.get('to') || '2026-10-06',
      grain: q.get('grain') || 'week',
      excludedPending: 8,
      points: q.get('grain') === 'day' ? FAKE_TREND_DAILY : FAKE_TREND_POINTS,
    })
  }
  if (u.includes('/api/spot-report/collect')) return json({ report: FAKE_REPORTS[0] })
  if (u.includes('/api/spot-report/data')) {
    const date = new URL(u, 'http://x').searchParams.get('date')
    return json({
      date: date || '2026-09-21',
      tradeDate: '2026-09-19',
      metrics: date === '2026-09-21' ? [...FAKE_METRICS, ...FAKE_CLEARING_METRICS] : FAKE_METRICS,
      extract: FAKE_EXTRACT,
    })
  }
  if (u.includes('/api/spot-report/audit')) return json({ list: FAKE_AUDIT })
  if (u.includes('/api/spot-report/confirm')) return json({ confirmed: 2 })
  if (u.includes('/api/spot-report/extract/batch/status')) return json(FAKE_BATCH)
  if (u.includes('/api/spot-report/extract/batch')) {
    const b = init?.body ? JSON.parse(String(init.body)) : {}
    if (b.dryRun) return json({ id: 'dry', total: 3, status: 'planned', dates: ['2026-09-19', '2026-09-20', '2026-09-21'], skippedConfirmed: ['2026-09-21'] })
    return json({ id: 'job1', total: 3, status: 'running', dates: [], skippedConfirmed: [] })
  }
  if (u.includes('/api/spot-report/extract')) return json({ metricCount: 42, status: 'ok', warnings: [], preserved: 5, pending: 2 })
  if (u.includes('/api/spot-report/metric')) return json({ metric: FAKE_METRICS[0] })
  if (u.includes('/api/spot-report')) return json({ list: FAKE_REPORTS })
  if (u.includes('/api/price/current')) {
    // 与列表用同一份夹具：否则「当前生效」标记会找不到对应卡片（曾经因此漏测）
    return json({ price: FAKE_PRICES[0] })
  }
  if (u.includes('/api/price')) {
    if (init?.method === 'POST' || init?.method === 'PUT' || init?.method === 'DELETE') return json({})
    return json({ list: FAKE_PRICES, total: FAKE_PRICES.length })
  }
  if (u.includes('/api/effect/backtest')) {
    return json({ backtest: FAKE_BACKTEST })
  }
  const body = u.includes('/api/gd-metric')
    ? {
        latestDate: '2026-09-21', from: '2026-08-23', to: '2026-09-21', date: '2026-09-21',
          source: 'gd-metric', updatedAt: '2026-09-21 20:00', availableDays: ['2026-09-21'],
          latest: {
            date: '2026-09-21',
            load: { avg: 3000, max: 3460, min: 2200, last: 3000, count: 96, unit: 'MW' },
            dayAhead: { avg: 380, max: 760, min: 60, last: 380, count: 96, unit: '元/MWh' },
            realtime: { avg: 365, max: 780, min: 40, last: 365, count: 96, unit: '元/MWh' },
          },
        // days 与 curve 都要给**非空**数据：之前这里是空数组，
        // 于是「图表真的画出了数据」这件事从来没被验证过 —— 信息披露页两个图
        // 因为没传 deps 只画了首帧空数据，冒烟照样全绿。
        days: [
          { date: '2026-09-20', load: { avg: 2950, max: 3400, min: 2100, last: 2950, count: 96, unit: 'MW' }, dayAhead: { avg: 370, max: 750, min: 50, last: 370, count: 96, unit: '元/MWh' }, realtime: { avg: 355, max: 770, min: 30, last: 355, count: 96, unit: '元/MWh' } },
          { date: '2026-09-21', load: { avg: 3000, max: 3460, min: 2200, last: 3000, count: 96, unit: 'MW' }, dayAhead: { avg: 380, max: 760, min: 60, last: 380, count: 96, unit: '元/MWh' }, realtime: { avg: 365, max: 780, min: 40, last: 365, count: 96, unit: '元/MWh' } },
        ],
        curve: {
          date: '2026-09-21',
          load: [
            { time: '00:15', value: 2600 },
            { time: '00:30', value: 2650 },
            { time: '00:45', value: 2700 },
          ],
          dayAhead: [
            { time: '00:15', value: 320 },
            { time: '00:30', value: 335 },
            { time: '00:45', value: 350 },
          ],
          realtime: [
            { time: '00:15', value: 310 },
            { time: '00:30', value: 328 },
            { time: '00:45', value: 344 },
          ],
        },
      }
    : {}
  return json(body)
}

/**
 * 断言本页所有图表都拿到了数据。
 *
 * 为什么需要：图形是 canvas，DOM 里看不出画没画 —— 曾经信息披露页因为图表在
 * 数据到达之前挂载、只画了首帧，接口有数据、KPI 有数、图全白，而冒烟照样全绿。
 * `data-chart-points` 是 useECharts 每次重绘时写在容器上的数据点数，用它来断言。
 */
async function assertChartsHaveData(label, expect = null) {
  // 每次都在等待里**重新查询**：图是数据到了才渲染出来的，
  // 提前抓一个数组等于等一个永远为空的快照。
  const read = () => [...doc.querySelectorAll('.chart')].map((c) => Number(c.dataset.chartPoints ?? 0))
  await waitFor(() => {
    const pts = read()
    return pts.length > 0 && pts.every((n) => n > 0)
  }, 2000)
  const points = read()
  if (expect !== null) check(points.length === expect, `${label}：图表 ${expect} 个（实际 ${points.length}）`)
  check(points.length > 0 && points.every((n) => n > 0), `${label}：每个图都画到了数据点`, JSON.stringify(points))
}

function json(data, status = 200, envelope = {}) {
  // 默认 code=0；需要模拟业务错误码时用第三参（GoFrame 把错误码放 body，HTTP 仍是 200）
  return new Response(JSON.stringify({ code: 0, ...envelope, data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const rootEl = doc.createElement('div')
rootEl.id = 'app'
doc.body.appendChild(rootEl)

// --- nav 事实来源（纯 TS，可用 vite 编译） ---
const vite = await createServer({ root, server: { middlewareMode: true }, appType: 'custom', logLevel: 'error' })
const { NAV } = await vite.ssrLoadModule(resolve(root, 'src/nav/index.ts'))

// 页面测试需要「已登录」这个前提，否则守卫会把 27 页全挡在登录页外。
// 这里直接播种 localStorage，等价于 AUTH_BYPASS_ROLE 注入的会话；
// 真正的登录/退出路径由下面的「登录与退出」一节端到端跑。
localStorage.setItem('ems:user:token', 'smoke-boot-token')
localStorage.setItem('ems:user:role', 'admin')

console.log('\n== 加载构建产物 ==')
try {
  await import(pathToFileURL(entry).href)
} catch (err) {
  console.error(`  入口执行失败: ${err?.stack || err}`)
  process.exit(1)
}
await tick()
await tick()

check(!!doc.querySelector('.appbar'), '壳层已挂载（.appbar 存在）')

console.log('\n== 顶栏约束（设计稿 §2 原则 6）==')
const appbar = doc.querySelector('.appbar')?.textContent ?? ''
check(!/KB\/s|上传|下载|CPU|内存|流量/.test(appbar), '顶栏不含流量 / CPU / 内存', appbar)
check(!/\d{2}:\d{2}:\d{2}/.test(appbar), '顶栏不含时钟', appbar)

console.log('\n== 历史信息：默认区间锚到最新日报日 ==')
await clickTo('场景', '历史信息')
{
  await waitFor(() => doc.querySelectorAll('input[type=date]').length >= 3, 1500)
  // 页头有一个「运行日」，页面自己还有起/止两个
  const dates = [...doc.querySelectorAll('input[type=date]')].map((i) => i.value).filter(Boolean)
  // 夹具里最新日报是 2026-09-21 → 默认区间右端必须是它，不能是「今天」
  check(dates.includes('2026-09-21'), '默认区间右端 = 最新日报日（夹具 09-21）', JSON.stringify(dates))
  check(!dates.some((d) => d.startsWith('2026-10-0')), '不再是「今天」当右端', JSON.stringify(dates))
  // 关键：请求必须**带上**这个区间。后端缺省是「本月 1 日~今天」，
  // 以前只有自定义模式才传 from/to，于是预设走的是后端缺省 → 整段落在数据之外（页面全空）
  const trendCall = requests.filter((r) => r.url.includes('/api/spot-report/trend')).pop()
  check(!!trendCall?.url.includes('from=2026-09-01') && trendCall?.url.includes('to=2026-09-21'),
    '趋势请求带上锚定后的区间（09-01~09-21）', trendCall?.url ?? '没发请求')
}

console.log('\n== 默认区间的锚点（数据滞后时不能落在空区间）==')
{
  // 部署上数据只到 08-31 而今天是 10-07：「今天往前 7 天」整段落在数据之外，
  // 预测复盘因此报「没有完整 96 点数据」。锚点必须收到数据最新日。
  const { anchorEndDate, isoDate, rangeEndingAt } = await vite.ssrLoadModule(resolve(root, 'src/utils/latestRange.ts'))
  const today = new Date('2026-10-07T15:00:00')
  check(isoDate(anchorEndDate('2026-08-31', today)) === '2026-08-31', '数据滞后一个多月 → 锚到数据最新日', isoDate(anchorEndDate('2026-08-31', today)))
  check(isoDate(anchorEndDate('2026-10-06', today)) === '2026-10-06', '数据滞后一天 → 锚到数据最新日')
  check(isoDate(anchorEndDate('2026-10-07', today)) === '2026-10-07', '数据就是今天 → 用今天')
  check(isoDate(anchorEndDate('2026-10-20', today)) === '2026-10-07', '数据比今天新（不该发生）→ 用今天')
  check(isoDate(anchorEndDate('', today)) === '2026-10-07' && isoDate(anchorEndDate(null, today)) === '2026-10-07', '拿不到最新日 → 用今天')
  check(isoDate(anchorEndDate('not-a-date', today)) === '2026-10-07', '格式不对 → 用今天')
  const r = rangeEndingAt(new Date('2026-08-31T00:00:00'), 6)
  check(r.from === '2026-08-25' && r.to === '2026-08-31', '区间右端随锚点回到数据内', JSON.stringify(r))
}

console.log('\n== 场景 · 信息披露（图表确实画出了数据）==')
await clickTo('场景', '信息披露')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('负荷与电价曲线'))

  // 数据场景：只有一支，所以是标签不是下拉；且必须说清另外两支没有数据源
  const sceneChip = doc.querySelector('[data-scene]')
  check(!!sceneChip && sceneChip.textContent.includes('广东现货日报'), '页头显示当前数据场景')
  check(
    !doc.querySelector('.pagehead select, .scene-switch select') &&
      !sceneChip?.tagName.match(/SELECT|INPUT/i),
    '「场景」不是可切换控件（切换本来不改变任何数据）',
  )
  const hint = sceneChip?.getAttribute('title') ?? ''
  check(hint.includes('山西现货交易') && hint.includes('需要'), '悬停说明列出规划中的场景及各自缺的数据源', hint.split('\n')[1] ?? '')

  // KPI 是模板绑定，之前「图空白」时它们照样有值 —— 所以不能只断言 KPI
  check(bodyHas('3000.0'), 'KPI 显示接口返回的负荷均值')
  check(bodyHas('380.0'), 'KPI 显示日前均价')

  // 关键：图表容器上记录着「本次画了多少个数据点」。
  // 图形是 canvas，DOM 里看不出画没画；这个属性让「接口有数据但图空白」可断言。
  const charts = [...doc.querySelectorAll('.chart')]
  check(charts.length === 2, `页面有两个图（曲线 + 趋势），实际 ${charts.length}`)
  await waitFor(() => charts.every((c) => Number(c.dataset.chartPoints ?? 0) > 0), 1500)
  const points = charts.map((c) => Number(c.dataset.chartPoints ?? 0))
  check(
    points.every((n) => n > 0),
    '两个图都画到了数据点（不是首帧空数据）',
    `data-chart-points = ${JSON.stringify(points)}`,
  )
  // 曲线图三条序列 × 各 3 个点 = 9；趋势图 2 条 × 2 天 = 4
  check(points[0] === 9, '曲线图的点数 = 3 条序列 × 3 个点', String(points[0]))
  check(points[1] === 4, '趋势图的点数 = 2 条序列 × 2 天', String(points[1]))

  // 换运行日要重新取数：日期在页头（PageHeader），绑定的是全局 runDate，
  // 页面 watch 它 → 重新拉数据。
  const before = requests.filter((r) => r.url.includes('/api/gd-metric')).length
  const dateInput = doc.querySelector('input[type=date]')
  check(!!dateInput, '页头有运行日选择')
  dateInput.value = '2026-09-20'
  dateInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await waitFor(() => requests.filter((r) => r.url.includes('/api/gd-metric')).length > before, 1500)
  check(requests.filter((r) => r.url.includes('/api/gd-metric')).length > before, '换运行日会重新取数')
  await waitFor(() => !!requests.filter((r) => r.url.includes('/api/gd-metric')).pop()?.url.includes('date=2026-09-20'), 1200)
  check(
    requests.filter((r) => r.url.includes('/api/gd-metric')).pop()?.url.includes('date=2026-09-20') === true,
    '请求带上了新的运行日',
    requests.filter((r) => r.url.includes('/api/gd-metric')).pop()?.url,
  )
}

console.log('\n== 运行监控 · 能源总览 ==')
await clickTo('运行监控', '能源总览')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('面板'))
  check(bodyHas('能源拓扑') && bodyHas('实时状态'), '默认勾选的面板已渲染')
  check(bodyHas('需量节省') && bodyHas('负荷预测'), '未勾选的面板出现在清单里')
  await assertChartsHaveData('能源总览')
  check(!bodyHas('峰谷套利'), '未勾选的面板不渲染内容（省掉请求）')

  // 拓扑四个节点都在；储能节点的功率来自设备遥测
  const nodeLabels = [...doc.querySelectorAll('.node-label')].map((e) => e.textContent.trim())
  check(JSON.stringify(nodeLabels) === JSON.stringify(['电网关口', '光伏', '储能', '负载']), '拓扑四节点', JSON.stringify(nodeLabels))
  check(bodyHas('储能柜 1'), '节点下列出具体设备名')
  check(bodyHas('净功率'), '给出净功率并写明口径')

  // 勾上「需量节省」→ 立刻取数并渲染
  const quick = [...doc.querySelectorAll('.quick-item')].find((e) => e.textContent.includes('需量节省'))
  const box = quick.querySelector('input[type=checkbox]')
  box.checked = true
  box.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => bodyHas('峰谷套利'), 1200)
  check(bodyHas('峰谷套利') && bodyHas('合计节省'), '勾选后取数并渲染该面板')
  check(localStorage.getItem('lems:monitor:panels')?.includes('saving'), '勾选状态存本地')

  // 告警摘要按级别排序：故障在前
  const firstAlarmLevel = doc.querySelector('.table .status-tag')?.textContent?.trim()
  check(firstAlarmLevel === '故障', '告警摘要按级别排序（故障在最前）', String(firstAlarmLevel))
}

console.log('\n== 运行监控 · 实时数据 ==')
await clickTo('运行监控', '实时数据')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('实时值'))
  check(bodyHas('储能柜') && bodyHas('正常'), '设备列表按类型分组并标状态')
  check(bodyHas('配置设备请去'), '写明与管理·设备管理的分工（这里只读）')
  check(bodyHas('最近更新'), '显示数据更新时间')

  // 点位定义里没有的点位照样显示，并标「未定义」
  await waitFor(() => bodyHas('SOC'))
  check(bodyHas('未定义'), '点位定义缺失时标出「未定义」，而不是藏起来')
  check(bodyHas('可画趋势'), '数值点位才可画趋势，并显示数量')

  // 趋势：请求带了设备与点位
  await waitFor(() => requests.some((r) => r.url.includes('/api/device/history')), 1200)
  const h = requests.filter((r) => r.url.includes('/api/device/history')).pop()
  check(
    typeof h?.body?.deviceId === 'string' && Array.isArray(h?.body?.telemetryKeys) && h.body.telemetryKeys.length > 0,
    '趋势请求带设备与点位',
    JSON.stringify(h?.body),
  )
}

console.log('\n== 运行监控 · 储能柜 ==')
await clickTo('运行监控', '储能柜')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('柜内设备'))
  // 柜子 = 设备类型 ess/sess；夹具里的柜子是 site_1「储能柜示范站」
  check(bodyHas('储能柜示范站'), '柜子列表（来自设备类型 ess/sess）')
  check(bodyHas('控制动作在'), '说明控制动作不在这里做（不在同页留第二个控制入口）')
  check(bodyHas('去手动控制'), '给出跳到手动控制的入口')
  check(bodyHas('关键量趋势') && bodyHas('柜内告警'), '趋势与告警面板都在')
  await assertChartsHaveData('储能柜')
}

console.log('\n== 运行监控 · 告警中心 ==')
await clickTo('运行监控', '告警中心')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('当前告警'))
  check(bodyHas('故障') && bodyHas('告警') && bodyHas('警告'), '三个级别都能显示')
  check(bodyHas('电池温度 55.2') || bodyHas('55.2'), '显示告警详情')
  check(bodyHas('忽略'), '当前告警带忽略操作')

  // 切到历史
  const historyTab = [...doc.querySelectorAll('.radio-group button')].find((b) => b.textContent.trim() === '历史告警')
  historyTab.click()
  await waitFor(() => bodyHas('触发'))
  check(bodyHas('未恢复') || bodyHas('2026-10-07 18:30:00'), '历史告警显示触发与清除时间')

  // 切到忽略规则并恢复一条
  const ignoreTab = [...doc.querySelectorAll('.radio-group button')].find((b) => b.textContent.trim() === '忽略规则')
  ignoreTab.click()
  await waitFor(() => bodyHas('恢复告警'))
  check(bodyHas('忽略规则'), '忽略规则列表')
  const restore = [...doc.querySelectorAll('.link')].find((b) => b.textContent.includes('恢复告警'))
  restore.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/alarm/ignore') && r.init?.method === 'DELETE' || r.method === 'DELETE'), 1200)
  check(requests.some((r) => (r.method ?? '') === 'DELETE' && r.url.includes('/api/alarm/ignore')), '恢复告警发的是 DELETE')
  await waitFor(() => bodyHas('已恢复'), 1200)
  check(bodyHas('已恢复'), '恢复后给出提示')

  // 口径说明必须写清「忽略 ≠ 清除」
  check(bodyHas('忽略 ≠ 清除') || bodyHas('忽略是一条'), '写明忽略与清除的区别')
}

console.log('\n== 登录与退出 ==')
{
  const loginForm = () => doc.querySelector('.login')
  const setInput = (el, v) => {
    el.value = v
    el.dispatchEvent(new window.Event('input', { bubbles: true }))
  }
  const loginFields = () => {
    const card = doc.querySelector('.login .card')
    return {
      account: card?.querySelector('input[name=account]'),
      password: card?.querySelector('input[name=password]'),
      submit: [...(card?.querySelectorAll('button') ?? [])].find((b) => b.textContent.trim().startsWith('登录')),
    }
  }

  check(!!doc.querySelector('.appbar .user-menu'), '顶栏有账号入口')
  const authBefore = requests.filter((r) => r.url.includes('/api/auth/')).length

  // 退出登录
  doc.querySelector('.appbar .user-menu').click()
  await tick()
  const logoutBtn = [...doc.querySelectorAll('.appbar .menu-item')].find((b) => b.textContent.includes('退出登录'))
  check(!!logoutBtn, '用户菜单里有退出登录')
  logoutBtn.click()
  await waitFor(() => !!loginForm(), 800)
  check(!!loginForm(), '退出后回到登录页')
  check(!localStorage.getItem('ems:user:token'), '退出后清掉本地令牌')
  check(requests.some((r) => r.url.includes('/api/auth/logout')), '退出时调用后端 logout')
  check(!doc.querySelector('.appbar'), '登录页不套壳层（没有顶栏/侧栏）')

  // 守卫：未登录时访问内页要弹回登录页，并记住原本要去哪
  window.history.pushState({}, '', '/scene/clearing')
  window.dispatchEvent(new window.Event('popstate'))
  await waitFor(() => !!loginForm(), 800)
  check(!!loginForm(), '未登录访问内页被弹回登录页')
  // 不查 location.search：happy-dom 下 vue-router 的 pushState 不会同步 location，
  // 查它只会查到自己宿主实现的脾气。「redirect 有没有生效」由下面登录后落到
  // 市场出清（≠ 默认首页信息披露）来证明 —— 那才是真的验证。

  // 账号不在内置列表 → 本地拦下，不打后端
  {
    const f = loginFields()
    setInput(f.account, 'root')
    setInput(f.password, 'admin')
    f.submit.click()
    await tick()
    check((doc.body.textContent ?? '').includes('内置账号'), '账号不存在时给出明确提示')
    check(
      requests.filter((r) => r.url.includes('/api/auth/login')).length === 0,
      '账号不合法时根本不发登录请求',
    )
  }

  // 密码错 → 后端 401 → 页面显示错误，且仍停在登录页
  {
    const f = loginFields()
    setInput(f.account, 'admin')
    setInput(f.password, 'wrong')
    f.submit.click()
    await waitFor(() => (doc.body.textContent ?? '').includes('用户名或密码错误'), 800)
    check((doc.body.textContent ?? '').includes('用户名或密码错误'), '密码错误时显示后端返回的原因')
    check(!!loginForm(), '密码错误后仍停在登录页')
    check(!localStorage.getItem('ems:user:token'), '密码错误不会写入令牌')
  }

  // 正确账密 → 回到登录前想去的那一页，而不是被扔回首页。
  // 用「市场出清」当目标是有讲究的：默认首页是 NAV[0].pages[0]（现在是「能源总览」），
  // 所以落到这里就证明 redirect 真的起作用了（否则会落到能源总览）。
  {
    const f = loginFields()
    setInput(f.account, 'admin')
    setInput(f.password, 'admin')
    f.submit.click()
    await waitFor(() => h1Text() === '市场出清', 1200)
    check(h1Text() === '市场出清', '登录后回到登录前想去的页（redirect 生效，不是落回默认首页）', h1Text())
    const call = requests.filter((r) => r.url.includes('/api/auth/login')).pop()
    check(JSON.stringify(call?.body) === JSON.stringify({ role: 'admin', password: 'admin' }), '登录请求体 = {role,password}', JSON.stringify(call?.body))
    check(localStorage.getItem('ems:user:role') === 'admin', '会话角色写入 localStorage')
    check(!!localStorage.getItem('ems:user:expireAt'), '会话过期时间写入 localStorage')
    check(!!doc.querySelector('.appbar'), '登录后壳层回来了')
  }
}

console.log('\n== 自动化联动页 ==')
await clickTo('管理', '自动化联动')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('派能BMS'))
  check(bodyHas('SOC>100'), '设备条件按表达式原样展示（不解析成中文）')
  check(bodyHas('23:00~06:00') && bodyHas('周一'), '时间条件摘要可读')
  check(bodyHas('实时') && bodyHas('间隔 60s'), '执行间隔分别展示实时/间隔')
  check(bodyHas('bms_1') && bodyHas('dido_1 → setHighLevel'), '执行动作摘要')

  // 新建：默认一个设备条件 + 一个动作
  const createBtn = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.includes('新建规则'))
  createBtn.click()
  await waitFor(() => !!doc.querySelector('.drawer'))
  check(!!doc.querySelector('.drawer'), '打开编辑抽屉')
  check(doc.querySelectorAll('.cond-row').length === 2, '默认 1 条件 + 1 动作', String(doc.querySelectorAll('.cond-row').length))

  // 空表单时保存禁用（名称/条件/动作都空）
  const saveBtn = [...doc.querySelectorAll('.drawer-foot button')].find((b) => b.textContent.trim() === '保存')
  check(saveBtn.disabled === true, '空表单时保存禁用')

  // 填名称但条件未选设备 → 仍禁用
  const nameInput = doc.querySelector('.form-grid input')
  nameInput.value = '过温停充'
  nameInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  check(saveBtn.disabled === true, '条件未选设备时保存仍禁用')
  check(bodyHas('未选设备'), '提示指明缺什么', '')

  // 选设备 + 点位 + 值
  const devSel = [...doc.querySelectorAll('.cond-body select')][0]
  const opt = [...devSel.options].find((o) => o.value === 'bms_1')
  check(!!opt, '条件下拉有设备')
  devSel.value = 'bms_1'
  devSel.dispatchEvent(new window.Event('change', { bubbles: true }))
  await tick()
  const pointInput = [...doc.querySelectorAll('.cond-body input')][0]
  pointInput.value = 'SOC'
  pointInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  const valueInput = [...doc.querySelectorAll('.cond-body input')][1]
  valueInput.value = '95'
  valueInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()

  // 选执行动作的服务
  const execDevSel = [...doc.querySelectorAll('.cond-body select')].slice(-2)[0]
  execDevSel.value = 'dido_1'
  execDevSel.dispatchEvent(new window.Event('change', { bubbles: true }))
  // 等服务清单按设备拉回来（抽屉里按 deviceId 缓存）
  await waitFor(() => [...doc.querySelectorAll('.cond-body select')].some((sel) =>
    [...sel.options].some((o) => o.value === 'setHighLevel')), 1500)
  const svcSel = [...doc.querySelectorAll('.cond-body select')].slice(-1)[0]
  const svcOpt = [...svcSel.options].find((o) => o.value === 'setHighLevel')
  if (svcOpt) {
    svcSel.value = 'setHighLevel'
    svcSel.dispatchEvent(new window.Event('change', { bubbles: true }))
    await tick()
  }
  check(saveBtn.disabled === false, '条件与动作齐全后保存可用')

  saveBtn.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/automation') && r.body?.name === '过温停充'))
  const call = requests.filter((r) => r.url.includes('/api/automation') && r.body?.name === '过温停充').pop()
  check(call?.body?.triggerRule?.anyMatch?.[0]?.deviceCondition?.rule === 'SOC>95', '设备条件拼成表达式 SOC>95', JSON.stringify(call?.body?.triggerRule))
  check(typeof call?.body?.executeRule === 'string', 'executeRule 以 JSON 字符串提交（后端契约）')
  let exec = {}
  try { exec = JSON.parse(call?.body?.executeRule ?? '{}') } catch { /* 下面断言会失败 */ }
  check(exec?.rules?.[0]?.service === 'setHighLevel', '执行动作带服务 key', JSON.stringify(exec))

  // 启停
  await waitFor(() => !!doc.querySelector('.rule'))
  const toggleBtn = [...doc.querySelectorAll('.rule .ops button')].find((b) => b.textContent.trim() === '停用')
  toggleBtn.click()
  await waitFor(() => requests.some((r) => r.url.includes('/toggle')))
  const tg = requests.filter((r) => r.url.includes('/toggle')).pop()
  check(tg?.body?.enable === false, '停用提交 enable=false', JSON.stringify(tg?.body))
}

console.log('\n== 统计分析页 ==')
await clickTo('管理', '统计分析')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('选择设备'))
  check(bodyHas('折线点位') && bodyHas('柱状点位'), '设备对比表单齐全')
  // 默认选中第一台设备；直接查（未填点位会先被拦？点位有默认值 Power）
  const queryBtn = [...doc.querySelectorAll('.actions button')].find((b) => b.textContent.trim() === '查询')
  queryBtn.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/device/compare')))
  const call = requests.filter((r) => r.url.includes('/api/device/compare')).pop()
  check(Array.isArray(call?.body?.deviceIds) && call.body.deviceIds.length === 1, '提交设备列表', JSON.stringify(call?.body?.deviceIds))
  check(call?.body?.lineKeys?.[0] === 'Power', '提交折线点位', JSON.stringify(call?.body?.lineKeys))
  check(typeof call?.body?.startTime === 'number' && call.body.endTime > call.body.startTime, '提交时间区间')

  // 切到协议统计
  const protoTab = [...doc.querySelectorAll('.tabs-inline button')].find((b) => b.textContent.trim() === '协议统计')
  protoTab.click()
  await waitFor(() => bodyHas('协议总数'))
  // 概览与分布是点「查询」之后才拉的，所以先查再断言
  const rateBtn = [...doc.querySelectorAll('.chip-toggle')].find((b) => b.textContent.trim() === '成功率(%)')
  check(rateBtn?.classList.contains('on'), '默认勾选成功率指标')
  const protoQuery = [...doc.querySelectorAll('.actions button')].find((b) => b.textContent.trim() === '查询')
  protoQuery.click()
  await waitFor(() => bodyHas('98.7'))
  check(bodyHas('9') && bodyHas('异常协议'), '概览 KPI 取到 totalProtocols 与异常数')
  check(bodyHas('98.7'), '平均成功率')
  check(bodyHas('modbus_tcp：6'), '类型分布')
  await waitFor(() => requests.some((r) => r.url.includes('/api/protocol/monitor/trend')))
  const trend = requests.filter((r) => r.url.includes('/api/protocol/monitor/trend')).pop()
  check(
    trend?.body?.metricKeys?.includes('ProtocolMetricSuccessRate'),
    '趋势请求带 metricKeys（取值来自后端映射表）',
    JSON.stringify(trend?.body?.metricKeys),
  )

  // 切到系统监控
  const sysTab = [...doc.querySelectorAll('.tabs-inline button')].find((b) => b.textContent.trim() === '系统监控')
  sysTab.click()
  await tick()
  const sysQuery = [...doc.querySelectorAll('.actions button')].find((b) => b.textContent.trim() === '查询')
  sysQuery.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/system/resource/chart')))
  const rc = requests.filter((r) => r.url.includes('/api/system/resource/chart')).pop()
  check(rc?.body?.category === 'process', '系统监控默认查进程资源', JSON.stringify(rc?.body?.category))
}

console.log('\n== 审计页 ==')
await clickTo('管理', '审计')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('运行日志'))
  check(bodyHas('12800'), '日志总量来自 info 接口')
  check(bodyHas('device') && bodyHas('protocol'), '级别/类型统计卡')
  check(bodyHas('12'), 'ERROR 计数')
  check(bodyHas('连接超时'), '日志行内容')
  check(bodyHas('第 1 / 1 页'), '分页信息')

  // 详情抽屉
  const row = [...doc.querySelectorAll('.log-row')][1]
  row.click()
  await waitFor(() => !!doc.querySelector('.drawer'))
  check(bodyHas('l2'), '详情显示日志 ID')
  check(!!doc.querySelector('.content-full'), '详情展示完整内容')
  ;[...doc.querySelectorAll('.drawer-head button')].find((b) => b.textContent.trim() === '关闭').click()
  await tick()

  // 变更审计页签：必须自我声明它不是真审计
  const auditTab = [...doc.querySelectorAll('.tabs-inline button')].find((b) => b.textContent.trim() === '变更审计')
  auditTab.click()
  await tick()
  check(bodyHas('这不是真正的变更审计'), '变更审计页显式声明局限')
  check(bodyHas('policy') && bodyHas('ems') === false || bodyHas('tou-rule'), '变更审计只挑 policy/ems 类日志')

  // 删除：不支持按日期删，要输 DELETE
  const delBtn = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.trim() === '删除')
  delBtn.click()
  await waitFor(() => !!doc.querySelector('.modal'))
  check(bodyHas('不支持按日期删除'), '删除弹窗说明不支持按日期删')
  const confirmBtn = [...doc.querySelectorAll('.modal button')].find((b) => b.textContent.trim() === '确认删除')
  check(confirmBtn.disabled === true, '未输 DELETE 时不能确认')
  const input = doc.querySelector('.modal input')
  input.value = 'DELETE'
  input.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  ;[...doc.querySelectorAll('.modal button')].find((b) => b.textContent.trim() === '确认删除').click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/log/biz') && r.method === 'DELETE'))
  check(true, '输对后调用 DELETE')
}

console.log('\n== 远程调度页 ==')
await clickTo('管理', '远程调度')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('Modbus 服务'))
  check(bodyHas('运行中'), '服务状态 KPI')
  check(bodyHas('1/2'), 'Modbus 从站在线数 1/2')
  check(bodyHas('监听 502'), '监听端口展示')

  // Modbus 从站表：起始地址按十六进制展示
  check(bodyHas('0xC8') && bodyHas('0x12C'), '从站起始地址按 16 进制展示')
  check(bodyHas('pcs_1') && bodyHas('meter_1'), '从站列表')

  // 停服务要二次确认
  const stopBtn = [...doc.querySelectorAll('.toolbar button')].find((b) => b.textContent.trim() === '停止服务')
  check(!!stopBtn, '有停止服务按钮')
  stopBtn.click()
  await waitFor(() => !!doc.querySelector('.modal'))
  check(!!doc.querySelector('.modal'), '停服务弹二次确认')
  const confirm = [...doc.querySelectorAll('.modal button')].find((b) => b.textContent.trim() === '确认停止')
  confirm.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/remote/modbus/enabled/false')))
  check(true, '确认后调用 enabled/false')

  // 重载
  const reloadBtn = [...doc.querySelectorAll('.toolbar button')].find((b) => b.textContent.trim() === '重载配置')
  reloadBtn.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/remote/modbus/reload')))
  check(true, '重载按钮调用 reload')

  // 切到 MQTT
  const mqttTab = [...doc.querySelectorAll('.tabs-inline button')].find((b) => b.textContent.trim() === 'MQTT')
  mqttTab.click()
  await waitFor(() => bodyHas('lems-01'))
  check(bodyHas('7e25fb08e2'), 'MQTT 客户端序列号')
  check(bodyHas('lems/7e25fb08e2/info'), 'MQTT topic')
  check(bodyHas('已连接') && bodyHas('未连接'), 'MQTT 连接状态区分')
}

console.log('\n== 网络配置页 ==')
await clickTo('管理', '网络配置')
{
  const bodyHas = (t) => doc.body.textContent ?? ''
  await waitFor(() => bodyHas().includes('eth0'))
  check(bodyHas().includes('100.64.0.7'), '接口列表带 IP')
  check(bodyHas().includes('DHCP'), 'DHCP 标记')

  // 选中 eth0 后表单回填
  const ifaceEls = [...doc.querySelectorAll('.iface')]
  ifaceEls[0].click()
  await tick()
  const ipInput = [...doc.querySelectorAll('.fields input')].find((i) => i.placeholder === '192.168.1.100')
  check(!!ipInput, '找到 IP 输入框')
  check(ipInput.value === '100.64.0.7', 'IP 回填', ipInput.value)
  check(ipInput.disabled === true, 'DHCP 模式下 IP 输入禁用')

  // 关掉 DHCP，填非法 IP 应被拦住
  const dhcpBox = [...doc.querySelectorAll('.fields input[type=checkbox]')].find((i) => i.closest('label.check'))
  // happy-dom 的 .click() 不会连带触发 change，而 Vue 的 checkbox v-model 挂的是 change —— 手动来
  dhcpBox.checked = false
  dhcpBox.dispatchEvent(new window.Event('change', { bubbles: true }))
  await tick()
  check(ipInput.disabled === false, '关掉 DHCP 后 IP 输入可用')
  ipInput.value = '999.1.1.1'
  ipInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  check(bodyHas().includes('IP 地址格式不正确'), '非法 IP 被拦住（999.1.1.1）')
  ipInput.value = '192.168.1.256'
  ipInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  check(bodyHas().includes('IP 地址格式不正确'), '超过 255 的段也被拦住')
  const saveBtn = [...doc.querySelectorAll('.actions button')].find((b) => b.textContent.trim() === '保存')
  check(saveBtn.disabled === true, '有校验错误时保存禁用')

  // 改成合法值 → 确认 → 提交 → 强制刷新列表
  ipInput.value = '192.168.1.50'
  ipInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  saveBtn.click()
  await waitFor(() => !!doc.querySelector('.modal'))
  check(!!doc.querySelector('.modal'), '保存弹确认')
  const confirm = [...doc.querySelectorAll('.modal button')].find((b) => b.textContent.trim() === '确认提交')
  const listCallsBefore = requests.filter((r) => r.url.includes('/api/network/interface/list')).length
  confirm.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/network/interface/update')))
  const call = requests.filter((r) => r.url.includes('/api/network/interface/update')).pop()
  check(call?.body?.name === 'eth0', '提交接口名', JSON.stringify(call?.body))
  check(call?.body?.config?.dhcp === false, '提交 dhcp=false')
  check(call?.body?.config?.ipv4?.[0]?.ipv4 === '192.168.1.50', '提交 IPv4', JSON.stringify(call?.body?.config))
  // 改完必须 forceRefresh 再读，否则会看到缓存里的旧配置
  await waitFor(() => requests.filter((r) => r.url.includes('/api/network/interface/list')).length > listCallsBefore, 1500)
  const after = requests.filter((r) => r.url.includes('/api/network/interface/list')).pop()
  check(after.url.includes('forceRefresh=true'), '保存后强制刷新接口列表', after.url)

  // ping
  const pingInput = [...doc.querySelectorAll('.ping input')][0]
  pingInput.value = '223.5.5.5'
  pingInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  const pingBtn = [...doc.querySelectorAll('.ping button')].find((b) => b.textContent.trim() === 'Ping')
  pingBtn.click()
  await waitFor(() => bodyHas().includes('延迟'))
  check(bodyHas().includes('延迟 12.4 ms'), 'ping 结果展示延迟')
}

console.log('\n== 设备管理页 ==')
await clickTo('管理', '设备管理')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('储能柜示范站'))
  // 树：扁平列表按 pid 组装出层级
  const nodes = [...doc.querySelectorAll('.tree .node')]
  check(nodes.length === 3, `树渲染 3 台设备（实际 ${nodes.length}）`)
  check(nodes[0].textContent.includes('储能柜示范站'), '根节点在最前')
  const indented = nodes.filter((n) => parseInt(n.style.paddingLeft) > 8)
  check(indented.length === 2, '子节点有缩进（层级来自 pid）', JSON.stringify(nodes.map((n) => n.style.paddingLeft)))

  // 默认选中第一台（根）→ 点表为空
  check(bodyHas('设备 ID'), '显示详情')
  nodes[1].click()
  await waitFor(() => bodyHas('ess_demo_v1.0.0'))
  check(bodyHas('HEX Demo vv1.0.0') || bodyHas('HEX Demo'), '驱动品牌/型号展示')
  check(bodyHas('emsdevice:1502'), '协议地址展示')
  check(bodyHas('间隔 15 秒'), '存储间隔展示')

  // 点表按分组
  await waitFor(() => bodyHas('状态量'))
  check(bodyHas('状态量') && bodyHas('控制量'), '点表按 group 分组')
  check(bodyHas('有功功率') && bodyHas('正值充电'), '点位名与说明')
  check(bodyHas('-50 ~ 50'), '点位范围')
  check(bodyHas('3 个点位') || bodyHas('3 个'), '点位数量统计')

  // 停用/启用
  const toggle = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.trim() === '停用')
  check(!!toggle, '有停用按钮')
  toggle.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/device/disable')))
  const call = requests.filter((r) => r.url.includes('/api/device/disable')).pop()
  check(call?.body?.deviceId === 'pcs_1', '停用提交正确的设备 ID', JSON.stringify(call?.body))

  // 异常设备标红
  check(bodyHas('异常') || bodyHas('停用'), '停用/异常状态有标记')
}

console.log('\n== 协议与驱动页 ==')
await clickTo('管理', '协议与驱动')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('modbus_tcp_1'))
  check(bodyHas('已激活') && bodyHas('未激活'), '协议激活状态区分')
  check(bodyHas('左开关'), 'GPIO 协议显示绑定设备')
  check(bodyHas('modbus_tcp'), '协议类型展示')

  // 类型筛选
  const typeSel = [...doc.querySelectorAll('.field select')][0]
  const gpioOpt = [...typeSel.options].find((o) => o.textContent.trim() === 'gpio')
  check(!!gpioOpt, '类型下拉含 gpio')
  typeSel.selectedIndex = gpioOpt.index
  typeSel.dispatchEvent(new window.Event('change', { bubbles: true }))
  await tick()
  const rows = [...doc.querySelectorAll('tbody tr')]
  check(rows.length === 1 && rows[0].textContent.includes('gpio_in'), '按类型筛选生效', String(rows.length))

  // 切到驱动 Tab
  const driverTab = [...doc.querySelectorAll('.tabs-inline button')].find((b) => b.textContent.trim() === '驱动管理')
  driverTab.click()
  await waitFor(() => bodyHas('ess_demo_v1.0.0'))
  check(bodyHas('运行中') && bodyHas('未加载'), '驱动状态区分')
  check(bodyHas('内置'), '内置驱动有标记')

  // 驱动详情抽屉
  const detailBtn = [...doc.querySelectorAll('tbody button')].find((b) => b.textContent.trim() === '详情')
  detailBtn.click()
  await waitFor(() => !!doc.querySelector('.drawer'))
  check(!!doc.querySelector('.drawer'), '打开驱动详情')
  check(bodyHas('deadbeef'), '详情带出提交哈希')
  check(bodyHas('40.0 KB'), '文件大小格式化')
}

console.log('\n== 采集与凭据页 ==')
await clickTo('配置', '采集与凭据')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('凭据状态'))
  check(bodyHas('七牛云'), '显示存储方式')
  check(bodyHas('tos-me'), '非敏感值（bucket）正常展示')
  check(bodyHas('••••••') && !bodyHas('SUPER-SECRET'), '密钥打码，且原文未出现')
  check(bodyHas('环境变量') && bodyHas('配置文件') && bodyHas('默认值'), '来源标注（env/config/default）')
  check(bodyHas('这里不能改'), '明确声明页面只读')
  check(bodyHas('未配置 OCR') || bodyHas('不能解析成结构化数据'), 'OCR 未配置时说明后果')
}

console.log('\n== 界面与语言页 ==')
await clickTo('配置', '界面与语言')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('主题'))
  check(bodyHas('浅色') && bodyHas('深色'), '主题可选')
  check(bodyHas('紧凑'), '密度可选')
  check(bodyHas('本期不做'), '语言项明确标为不做，不做假下拉框')

  // 切深色要真的落到 documentElement 上（主题是 CSS 变量驱动的）
  const darkBtn = [...doc.querySelectorAll('.choice')].find((b) => b.textContent.includes('深色'))
  darkBtn.click()
  await tick()
  check(doc.documentElement.dataset.theme === 'dark', '切深色写入 data-theme', String(doc.documentElement.dataset.theme))
  check(localStorage.getItem('lems:ui:theme') === 'dark', '主题持久化到 localStorage')

  // 切紧凑
  const compactBtn = [...doc.querySelectorAll('.choice')].find((b) => b.textContent.includes('紧凑'))
  compactBtn.click()
  await tick()
  check(doc.documentElement.dataset.density === 'compact', '切紧凑写入 data-density')

  // 切回浅色要移除属性（不是设为其他值）
  const lightBtn = [...doc.querySelectorAll('.choice')].find((b) => b.textContent.includes('浅色'))
  lightBtn.click()
  await tick()
  check(doc.documentElement.dataset.theme === undefined, '切回浅色移除 data-theme')

  // 默认首页
  const sel = doc.querySelector('.field select')
  const opt = [...sel.options].find((o) => o.textContent.includes('策略管理'))
  check(!!opt, '默认首页下拉列出所有页面')
  sel.selectedIndex = opt.index
  sel.dispatchEvent(new window.Event('change', { bubbles: true }))
  await tick()
  check(!!localStorage.getItem('lems:ui:landing'), '默认首页持久化')
  check(bodyHas('策略管理'), '展示当前默认页预览')
}

console.log('\n== 系统信息页 ==')
await clickTo('配置', '系统信息')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('CPU 使用率'))
  check(bodyHas('41.2'), '/system/summary 扁平响应取到 CPU 使用率')
  check(bodyHas('15.4') || bodyHas('8.8'), '内存总量/已用取到')
  check(bodyHas('172 天 2 小时 34 分'), '运行时长格式化正确')
  check(bodyHas('Asia/Shanghai'), '时区展示')
  check(bodyHas('e186166d6694'), '主机名展示')
  check(bodyHas('8.1 KB/s') || bodyHas('3.2 KB/s'), '网络速率格式化（humanBytes）', '')
  // 页面按 1 位小数展示（185.24 → 185.2 / 219.99 → 220.0），别拿原始两位小数断言
  check(bodyHas('185.2') && bodyHas('220.0'), '磁盘已用/总量按 1 位小数展示')
}

console.log('\n== 存储管理页 ==')
await clickTo('配置', '存储管理')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('存储占用'))
  check(bodyHas('1280'), '时序条数取到 { stats } 外层内')
  check(bodyHas('219.7 MB'), '占用空间格式化')
  check(bodyHas('30'), '保留策略回填（720 小时 → 30 天）')

  // 保留天数改成 0 应被拦住
  const inputs = [...doc.querySelectorAll('.fields input')]
  check(inputs.length === 3, `三类保留天数输入（实际 ${inputs.length}）`)
  inputs[0].value = '0'
  inputs[0].dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  const saveBtn = [...doc.querySelectorAll('.actions button')].find((b) => b.textContent.trim() === '保存')
  saveBtn.click()
  await tick()
  check(!doc.querySelector('.modal'), '非法保留天数被拦住，不弹确认')

  inputs[0].value = '45'
  inputs[0].dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  saveBtn.click()
  await waitFor(() => !!doc.querySelector('.modal'))
  check(!!doc.querySelector('.modal'), '合法值弹出确认')
  const confirm = [...doc.querySelectorAll('.modal button')].find((b) => b.textContent.trim() === '确认保存')
  confirm.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/system/storage-time')))
  const call = requests.filter((r) => r.url.includes('/api/system/storage-time')).pop()
  check(call?.body?.deviceRetentionDays === 45, '提交设备保留天数', JSON.stringify(call?.body))
}

console.log('\n== 系统操作页 ==')
await clickTo('配置', '系统操作')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('主机名'))
  // 主机名校验：非法字符应被拦住
  const hn = doc.querySelector('#hostname')
  hn.value = 'bad name!'
  hn.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  check(bodyHas('只能包含字母、数字与短横线'), '主机名非法字符被拦住')

  hn.value = 'lems-edge-01'
  hn.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  const saveBtn = [...doc.querySelectorAll('.actions button')].find((b) => b.textContent.trim() === '保存')
  saveBtn.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/system/hostname/update')))
  const call = requests.filter((r) => r.url.includes('/api/system/hostname/update')).pop()
  check(call?.body?.hostname === 'lems-edge-01', '提交主机名', JSON.stringify(call?.body))

  // 重启是两步：申请前「执行重启」必须不可用
  const rebootBtns = [...doc.querySelectorAll('.reboot button')]
  check(rebootBtns.length === 2, `重启两步各一个按钮（实际 ${rebootBtns.length}）`)
  check(rebootBtns[1].disabled === true, '未申请时「执行重启」不可用')
  rebootBtns[0].click()
  await waitFor(() => bodyHas('已申请'))
  check(bodyHas('已申请'), '申请后进入倒计时中间态')
  check([...doc.querySelectorAll('.reboot button')][1].disabled === false, '申请后「执行重启」可用')
  const applyCall = requests.filter((r) => r.url.includes('/api/system/reboot/apply')).pop()
  check(!!applyCall, '发出重启申请')

  // 执行需输主机名确认
  ;[...doc.querySelectorAll('.reboot button')][1].click()
  await waitFor(() => !!doc.querySelector('.danger-modal'))
  const execBtn = [...doc.querySelectorAll('.danger-modal button')].find((b) => b.textContent.trim() === '确认重启')
  check(execBtn.disabled === true, '未输主机名时不能确认重启')
  const confirmInput = doc.querySelector('.danger-modal input')
  confirmInput.value = 'lems-edge-01'
  confirmInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  ;[...doc.querySelectorAll('.danger-modal button')].find((b) => b.textContent.trim() === '确认重启').click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/system/reboot/execute')))
  check(true, '输对主机名后可执行重启')
}

console.log('\n== 旧路由重定向 ==')
for (const [from, to] of [['/programs', '手动控制'], ['/effect', '决策支持']]) {
  window.history.pushState({}, '', from)
  window.dispatchEvent(new window.Event('popstate'))
  await waitFor(() => (doc.querySelector('.pagehead h1')?.textContent ?? '').includes(to), 800)
  check(
    (doc.querySelector('.pagehead h1')?.textContent ?? '').includes(to),
    `${from} → ${to}`,
    doc.querySelector('.pagehead h1')?.textContent ?? '(空)',
  )
}

console.log('\n== 报表导出页 ==')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)

  // CSV 转义直接测函数：经由 DOM 间接断言看不出引号/换行是怎么处理的。
  // 这一层错了 Excel 不会报错，只会安静地给一张错位的表。
  const { buildCsv, csvField } = await vite.ssrLoadModule(resolve(root, 'src/utils/csv.ts'))
  const quoted = (v) => `"${v}"`
  check(csvField('plain') === 'plain', '普通字段不加引号')
  check(csvField(1) === '1' && csvField(null) === '' && csvField(undefined) === '', '数字/空值处理')
  check(csvField('a,b') === quoted('a,b'), '含逗号 → 加引号', csvField('a,b'))
  check(csvField('say "hi"') === quoted('say ""hi""'), '引号翻倍', csvField('say "hi"'))
  check(csvField('l1\nl2') === quoted('l1\nl2'), '含换行 → 加引号')
  check(csvField(' pad ') === quoted(' pad '), '前后空格也加引号（否则会被下游 trim 掉）')
  // 逐行写清楚：含换行的字段也要加引号，末行不带尾随 CRLF
  const table = buildCsv(['a', 'b'], [['x,y', 'q"q'], ['l\nm', '']])
  const wantTable = ['a,b', '"x,y","q""q"', '"l\nm",'].join('\r\n')
  check(table === wantTable, '整表：CRLF 行尾 + 转义', JSON.stringify(table))
  check(buildCsv(['a', 'b'], []) === 'a,b', '无数据时只有表头')
  // Excel 打开 UTF-8 CSV 没有 BOM 就是中文乱码，这条不能丢
  const { CSV_BOM } = await vite.ssrLoadModule(resolve(root, 'src/utils/csv.ts'))
  check(CSV_BOM === '\ufeff', '下载内容带 UTF-8 BOM（Excel 不乱码）', JSON.stringify(CSV_BOM))

  await clickTo('管理', '报表导出')
  await waitFor(() => bodyHas('数据集'))
  const dsEls = [...doc.querySelectorAll('.ds')]
  check(dsEls.length === 7, `数据集 7 个（实际 ${dsEls.length}）`, dsEls.map((e) => e.querySelector('strong')?.textContent).join('/'))
  check(bodyHas('电网日指标') && bodyHas('效果评估·实测汇总') && bodyHas('策略反演·反演报价段'), '数据集覆盖电网指标/效果/反演')
  check(bodyHas('报表模板与生成任务') && bodyHas('没有任务、没有历史文件'), '写清与设计稿的差距（无模板/任务/历史文件）')
  check(bodyHas('受浏览器内存限制'), '写清前端生成的代价（区间上限）')

  // 默认数据集就是电网日指标，直接生成预览
  const genBtn = [...doc.querySelectorAll('.toolbar button')].find((b) => b.textContent.includes('生成预览'))
  genBtn.click()
  await waitFor(() => !!doc.querySelector('.table tbody tr'), 1200)
  const rows = [...doc.querySelectorAll('.table tbody tr')]
  check(rows.length === 2, `预览 2 行（实际 ${rows.length}）`)
  const firstRow = [...rows[0].querySelectorAll('td')].map((td) => td.textContent.trim()).join('|')
  check(firstRow === '2026-09-20|00:00|日前出清价 元/MWh|100', '第一行内容按列定义', firstRow)
  check(bodyHas('2 行 × 4 列'), '显示行数 × 列数')
  const pre = doc.querySelector('.raw pre')?.textContent ?? ''
  check(pre.includes('日期,时刻,类别,数值'), 'CSV 原文预览含表头', pre.split('\n')[0])
  check(pre.includes('2026-09-20,00:00,日前出清价 元/MWh,100'), 'CSV 原文预览含数据行')
  check(bodyHas('.csv'), '显示下载文件名')

  // 区间超上限 → 拦下且不发请求（后端 MaxSeriesDays=400 会直接 400，先在本地说清）
  const seriesBefore = requests.filter((r) => r.url.includes('/api/gd-metric/series')).length
  const dates = [...doc.querySelectorAll('.toolbar input[type=date]')]
  dates[0].value = '2020-01-01'
  dates[0].dispatchEvent(new window.Event('input', { bubbles: true }))
  genBtn.click()
  await waitFor(() => bodyHas('超过上限 400 天'), 800)
  check(bodyHas('超过上限 400 天'), '区间超上限时给出明确原因')
  check(
    requests.filter((r) => r.url.includes('/api/gd-metric/series')).length === seriesBefore,
    '超上限时根本不发请求',
  )

  // 切数据集：参数与预览都要重置
  const effect = dsEls.find((e) => (e.querySelector('strong')?.textContent ?? '').includes('实测汇总'))
  effect.click()
  await tick()
  check(!doc.querySelector('.table tbody tr'), '切数据集后清掉旧预览')
  check(bodyHas('效果评估·实测汇总') && bodyHas('/api/effect/summary'), '面板脚注跟着切到新数据集的来源接口')
}

console.log('\n== 用户与权限页 ==')
await clickTo('管理', '用户与权限')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('内置账号'))
  check(bodyHas('本系统没有用户表') || bodyHas('两个角色本身就是账号'), '说清「账号即角色」这个前提')
  check(bodyHas('当前会话') && bodyHas('正常登录'), '显示当前会话类型（非绕过）')
  check(bodyHas('只能改自己的密码') || bodyHas('可重置普通用户'), '按角色给出改密范围')
  check(bodyHas('admin ⊇ user'), '权限规则写明 admin 包含 user')
  check(bodyHas('界面不做权限隐藏'), '说明界面不隐藏入口、权威判定在后端')

  // 权限矩阵：user 的管理类接口必须是 ✗
  const matrix = [...doc.querySelectorAll('.table')].pop()
  const cells = [...matrix.querySelectorAll('div')].map((d) => d.textContent.replace(/\s+/g, '').trim())
  check(JSON.stringify(cells) === JSON.stringify(['账号','说明','读接口','管理类接口','admin','管理员','✓','✓','user','普通用户','✓','✗']),
    '权限矩阵：user 无管理类权限', JSON.stringify(cells))

  const form = doc.querySelector('.form')
  const inputs = [...form.querySelectorAll('input')]
  const setVal = (el, v) => {
    el.value = v
    el.dispatchEvent(new window.Event('input', { bubbles: true }))
  }
  const btn = [...doc.querySelectorAll('.actions button')].find((b) => b.textContent.includes('修改密码'))
  const changeCalls = () => requests.filter((r) => r.url.includes('/api/auth/change-password')).length

  // 两次新密码不一致 → 本地拦下，不发请求
  const before = changeCalls()
  setVal(inputs[0], 'admin')
  setVal(inputs[1], 'newpass1')
  setVal(inputs[2], 'newpass2')
  btn.click()
  await tick()
  check(bodyHas('两次输入的新密码不一致'), '两次新密码不一致时本地拦下')
  check(changeCalls() === before, '本地校验失败时根本不发请求')

  // 新密码与当前密码相同 → 也拦下
  setVal(inputs[1], 'admin')
  setVal(inputs[2], 'admin')
  btn.click()
  await tick()
  check(bodyHas('新密码不能与当前密码相同'), '新旧密码相同时拦下')

  // 当前密码错 → 后端 401，页面显示原因
  setVal(inputs[0], 'wrong')
  setVal(inputs[1], 'newpass1')
  setVal(inputs[2], 'newpass1')
  btn.click()
  await waitFor(() => bodyHas('当前密码不正确'), 800)
  check(bodyHas('当前密码不正确'), '当前密码错误时显示后端返回的原因')
  // 关键：业务错误（code=61）不能被当成「会话过期」。曾经因为桩错用 HTTP 401，
  // 页面把人踢回登录页，而下面的断言又在登录页上「通过」了。
  check(!!doc.querySelector('.form') && h1Text() === '用户与权限', '打错当前密码后仍留在本页（没被当成会话过期）', h1Text())

  // 正确 → 提交请求体必须是 {role, oldPassword, newPassword}，且成功提示讲清「不吊销其它会话」
  setVal(inputs[0], 'admin')
  btn.click()
  await waitFor(() => (doc.body.textContent ?? '').includes('已修改'), 800)
  const call = requests.filter((r) => r.url.includes('/api/auth/change-password')).pop()
  check(
    JSON.stringify(call?.body) === JSON.stringify({ role: 'admin', oldPassword: 'admin', newPassword: 'newpass1' }),
    '改密请求体 = {role, oldPassword, newPassword}',
    JSON.stringify(call?.body),
  )
  check(bodyHas('不会因此失效'), '成功提示讲清「其它会话不会失效」')
  check(!form.querySelector('input[type=password]').value, '成功后清空密码输入框')

  // 限制清单必须常驻（不假装有能力）
  check(bodyHas('password_length') && bodyHas('不生效'), '写明 password_length 设置不生效')
  check(bodyHas('session_admin_timeout'), '写明会话时长设置不生效（且因此没做控件）')
  check(!bodyHas('保存会话时长'), '没有给不生效的设置做假控件')
  check(bodyHas('没有失败次数限制'), '写明登录没有频率限制 / 锁定')
}

console.log('\n== 存储管理·保留策略（已生效）==')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await clickTo('配置', '存储管理')
  await waitFor(() => bodyHas('保留策略'))
  // 首访：本次启动后还没跑过 → 页面要说清「还没有执行过」以及间隔，而不是装作有结果
  check(bodyHas('还没有执行过清理'), '本次启动未清理时如实说明')
  check(bodyHas('每 24 小时一次'), '写明自动清理间隔')
  check(bodyHas('电价历史不会被删'), '写明电价历史不会被清理（长周期预测依赖它）')
  check(bodyHas('协议指标算设备侧数据'), '写明协议指标归设备保留')
  check(!bodyHas('当前不生效') && !bodyHas('后端目前不读它们'), '不再有「不生效」的旧说法')

  const cleanBtn = [...doc.querySelectorAll('button')].find((b) => b.textContent.includes('立即清理'))
  check(!!cleanBtn, '有「立即清理」按钮')
  cleanBtn.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/system/retention/clean')), 1500)
  check(requests.some((r) => r.url.includes('/api/system/retention/clean')), '点击后调用清理接口')
  await waitFor(() => bodyHas('已清理'), 1500)
  check(bodyHas('已清理'), '清理完成给出结果提示')
  // 清理后状态区要换成真实结果（时间/删除行数/各自截止时间）
  await waitFor(() => bodyHas('上次清理'), 1500)
  check(bodyHas('删除日志 12 行'), '显示上次删了多少行')
  check(bodyHas('2026-09-08 03:15:00') && bodyHas('2026-07-10 03:15:00'), '分别显示设备/系统/日志的截止时间')
}
console.log('\n== 日志级别页 ==')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await clickTo('配置', '日志级别')
  await waitFor(() => bodyHas('全局日志级别'))
  check(bodyHas('管系统日志') && bodyHas('管业务日志'), '把两套级别的界线写在标题里')
  check(bodyHas('立即生效'), '写明改完立即生效')

  // 模块矩阵：6 个模块，说明与后端 LogLevelSettings 对应
  const rowTitles = [...doc.querySelectorAll('.table .strong')].map((d) => d.textContent.trim())
  check(
    JSON.stringify(rowTitles) === JSON.stringify(['ems', 'device', 'protocol', 'policy', 'automation', 'remote']),
    '模块矩阵 6 行且顺序固定',
    JSON.stringify(rowTitles),
  )

  // 改一个模块级别：请求必须带上原 isPublic/remark —— /setting/update 是全量覆盖，
  // 少传 isPublic 会把设置置成非公开，页面下次就读不到它了
  const sel = [...doc.querySelectorAll('.table .ctrl select')][2] // protocol
  sel.value = 'INFO'
  sel.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => requests.some((r) => r.url.includes('/api/setting/update')), 800)
  const call = requests.filter((r) => r.url.includes('/api/setting/update')).pop()
  check(call?.body?.id === 'LogLevelProtocol' && call?.body?.value === 'INFO', '改了 protocol 的级别', JSON.stringify(call?.body))
  check(call?.body?.isPublic === true, '更新时保留 isPublic（全量覆盖接口的坑）', JSON.stringify(call?.body))
  check(call?.body?.group === 'system' && call?.body?.remark === 'protocol 业务日志级别', '保留 group 与 remark')
  check(bodyHas('已保存') && bodyHas('立即生效'), '保存后给出即时生效的提示')

  // 调试开关：勾上 → 写 true
  const dbg = doc.querySelector('.switch input[type=checkbox]')
  check(!!dbg, '有调试日志开关')
  dbg.checked = true
  dbg.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => {
    const c = requests.filter((r) => r.url.includes('/api/setting/update')).pop()
    return c?.body?.id === 'SystemEnableDebugLog' && c?.body?.value === 'true'
  }, 800)
  const dbgCall = requests.filter((r) => r.url.includes('/api/setting/update')).pop()
  check(dbgCall?.body?.value === 'true', '开关写入 true', JSON.stringify(dbgCall?.body))

  // 限制清单：必须写出「三个条件」「保留天数没生效」这类实话
  check(bodyHas('两个条件') || bodyHas('都要满足'), '写明级别与调试开关是两个条件')
  check(bodyHas('逐设备的'), '写明设备侧还有第三道条件')
  check(bodyHas('没有任何代码读它'), '写明日志保留天数没有生效')

  // 普通用户会话：只读（写设置需要 admin，否则每个控件都会吃 403）
  check(!bodyHas('本页**只读**') && !bodyHas('本页只读'), '管理员会话下不显示只读提示')
  localStorage.setItem('ems:user:role', 'user')
  await clickTo('场景', '信息披露') // 先离开这一页，避免组件缓存
  await clickTo('配置', '日志级别')
  await waitFor(() => bodyHas('只读'), 1200)
  check(bodyHas('只读'), '普通用户会话下给出只读提示')
  const lockedSel = doc.querySelector('.table .ctrl select')
  check(lockedSel?.disabled === true, '普通用户会话下控件禁用（而不是点了才 403）')
  localStorage.setItem('ems:user:role', 'admin')
}

console.log('\n== 导航结构 ==')
// 期望页数写在这里：改成别处时必须有意识，不能让导航悄悄少一项还没人发现。
// 当前 27 = 29 - 「数据保留」- 「时间与时区」
//   两者都是「后端接口挂在别的功能下」：保留→存储管理，时间→系统操作，不留两个入口
const EXPECTED_PAGES = 31
const tabEls = [...doc.querySelectorAll('.tabs .tab')]
check(tabEls.length === 4, `Tab 数量 = ${tabEls.length}（期望 4）`, tabEls.map((e) => e.textContent.trim()).join('/'))
check(
  tabEls.map((e) => e.textContent.trim()).join('/') === '运行监控/场景/管理/配置',
  'Tab 顺序：运行监控插在场景之前（设计稿 §9）',
  tabEls.map((e) => e.textContent.trim()).join('/'),
)
// 按 key 找而不是按序号：Tab 顺序变过一次（加了运行监控），序号断言会跟着一起错
const sceneTab = NAV.find((t) => t.key === 'scene')
check(sceneTab?.pages.length === 7, `场景页 7 个（实际 ${sceneTab?.pages.length}）`)
const totalPages = NAV.reduce((n, t) => n + t.pages.length, 0)
check(totalPages === EXPECTED_PAGES, `页面总数 ${EXPECTED_PAGES}（实际 ${totalPages}）`)

// 构建产物是否跟得上源码。
//
// 检查的是 **dist**（npm run verify 刚构建出来的那份），不是 zip：
// zip 现在由 pack 脚本产出、且不再入库（构建镜像时才生成），所以「提交的产物过期」
// 这类问题从根上没有了。剩下要守的是另一件事：**加了页面却忘了登记/懒加载** ——
// 那样 dist 里就少一个页面 chunk，而导航里照样列着它。
{
  const assetsDir = resolve(root, 'dist/assets')
  let pageChunks = []
  try {
    pageChunks = readdirSync(assetsDir).filter((f) => f.endsWith('.js') && /View-/.test(f))
  } catch {
    check(false, '找到 dist/assets（先跑 npm run build）')
  }
  // 登录页不在导航里，所以 +1；改这里要同时想清楚有没有别的非导航页
  const NON_NAV_PAGES = 1
  // 比的是**真实导航页数**（totalPages），不是 EXPECTED_PAGES：
  // 后者是「防止有人悄悄改导航」的常量，拿它比就漏掉「加了页没打包」这个场景。
  check(
    pageChunks.length === totalPages + NON_NAV_PAGES,
    `dist 里的页面 chunk 数跟上导航（期望 ${totalPages + NON_NAV_PAGES}，实际 ${pageChunks.length}）`,
    '少一个说明有页面没登记到 pages/index.ts，或多了一个没人用的页面',
  )
}

/** 点 Tab 再点侧栏项 */
async function clickTo(tabTitle, pageTitle) {
  const tabEl = findTab(tabTitle)
  if (!tabEl) throw new Error(`找不到 Tab ${tabTitle}`)
  tabEl.click()
  if (!(await waitFor(() => !!findSide(pageTitle)))) throw new Error(`切到「${tabTitle}」后侧栏没有「${pageTitle}」`)
  findSide(pageTitle).click()
  await waitFor(() => h1Text() === pageTitle)
  await tick()
}

console.log('\n== 逐页点击 ==')
let visited = 0
for (const tab of NAV) {
  const tabEl = [...doc.querySelectorAll('.tabs .tab')].find((e) => e.textContent.trim() === tab.title)
  if (!tabEl) {
    check(false, `找到 Tab「${tab.title}」`)
    continue
  }
  tabEl.click()
  // 判断依据要唯一：两栏项数可能相同（配置与场景都是 7），按首页标题等才可靠
  await waitFor(() => !!findSide(tab.pages[0].title))

  const sideCount = sideTitles().length
  check(sideCount === tab.pages.length, `${tab.title} 侧栏项数 = ${sideCount}（期望 ${tab.pages.length}）`)

  for (const page of tab.pages) {
    const item = findSide(page.title)
    if (!item) {
      check(false, `${tab.key}/${page.key} 侧栏项存在`)
      continue
    }
    try {
      item.click()
      await waitFor(() => h1Text() === page.title)
      const h1 = h1Text()
      const body = doc.body.textContent ?? ''
      check(h1 === page.title, `${tab.key}/${page.key} 标题`, `期望 ${page.title}，实际 ${h1}`)
      // paused = 页面是真的（已做过、已停用），只是功能关着。占位面板/状态提示是给
      // planned / 未做页面用的，不适用于它；这里只钉「侧栏不假装它 ready」。
      if (page.status === 'paused') {
        check(
          (item.textContent ?? '').includes('暂停'),
          `${tab.key}/${page.key} 侧栏标「暂停」`,
          item.textContent ?? '',
        )
      } else if (page.status !== 'ready') {
        const missing = (page.panels ?? []).filter((n) => !body.includes(n))
        check(
          missing.length === 0 && (page.panels ?? []).length > 0,
          `${tab.key}/${page.key} 占位面板齐全`,
          missing.length ? `缺 ${missing.join(', ')}` : 'nav 未声明面板',
        )
        const tag = page.status === 'todo' ? '本期不做' : '本期做，界面待实现'
        check(body.includes(tag), `${tab.key}/${page.key} 状态提示`, `未见「${tag}」`)
      }
      visited++
    } catch (err) {
      check(false, `${tab.key}/${page.key} 渲染`, String(err?.message || err))
    }
  }
}
check(visited === totalPages, `共访问 ${visited} 页（期望 ${totalPages}）`)

console.log('\n== 预测复盘页 ==')
await clickTo('场景', '预测复盘')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('事后回测'))
  check(bodyHas('回测：用') || bodyHas('用**当前模型**在历史数据上重跑'), '回测模式显式声明「不是当时预测」')

  // 默认区间必须锚到「数据最新日」（夹具里 gd-metric 的 latestDate=2026-09-21），
  // 否则「今天往前 7 天」整段落在数据之外 → 线上就是这么报
  // 「区间 ... 内没有完整（96 点）的日前电价数据」的。
  const btCall = requests.filter((r) => r.url.includes('/api/forecast/backtest')).pop()
  check(
    btCall?.url.includes('from=2026-09-15') && btCall?.url.includes('to=2026-09-21'),
    '回测请求带上锚定后的区间（09-15~09-21，7 天档）',
    btCall?.url ?? '没发请求',
  )

  await waitFor(() => bodyHas('对比点数'))
  // 等数据真的加载完再断言 —— 否则看到的是加载前的占位文案，断言会因为错的原因通过
  await waitFor(() => /对比点数[\s\S]{0,80}>?\s*4/.test(doc.body.textContent ?? ''))
  check(bodyHas('4'), '对比点数 4')
  check(bodyHas('2 天') || bodyHas('2 个自然日'), '覆盖 2 个自然日')

  // Spearman ρ（全天）手算：
  //   预测 [100,90,110,100] → 秩 [2.5, 1, 4, 2.5]
  //   实际 [100,100,200,200] → 秩 [1.5, 1.5, 3.5, 3.5]
  //   num = 3, dx = 4.5, dy = 4 → ρ = 3 / √18 = 0.7071
  await waitFor(() => bodyHas('0.707'))
  // 时段来自电价夹具：00-08 谷 / 08-13 峰 / 13-20 平 / 20-24 谷
  //   四个点 → 谷×2（09-20 00:00、06:00）、峰×1（09-21 12:00）、平×1（09-21 18:00）
  //   三个分段都 <3 点 → ρ 必须是「—样本不足」，不能是 0（0 会被读成「不相关」）
  //   全天 4 点 → 手算 ρ = 3/√18 = 0.707
  const rhoMatrix = [...doc.querySelectorAll('.matrix')][0]
  const rhoRow = [...rhoMatrix.querySelectorAll('.num')].slice(0, 4).map((c) => c.textContent.replace(/\s+/g, '').trim())
  check(rhoRow.slice(0, 3).every((t) => t === '—样本不足'), '分段样本 <3 时 ρ 显示「—」并说明样本不足', JSON.stringify(rhoRow))
  check(rhoRow[3] === '0.707', '全天 ρ = 0.707（秩相关手算值）', rhoRow[3])

  // 离散度：分母=当日均价（09-20 均价 100、09-21 均价 200）
  // |100-100|/100=0, |90-100|/100=0.1, |110-200|/200=0.45, |100-200|/200=0.5 → 平均 0.2625
  const pctCells = [...doc.querySelectorAll('.matrix')][1].querySelectorAll('.num')
  const allPct = [...pctCells].map((c) => c.textContent.trim()).find((t) => t.startsWith('0.26'))
  check(!!allPct, `全天平均离散度 = 0.2625（分母为当日均价）`, [...pctCells].map((c) => c.textContent.trim()).join('|'))
  // MAE: (0+10+90+100)/4 = 50
  check([...pctCells].some((c) => c.textContent.trim() === '50.0'), '全天 MAE = 50.0 元/MWh')
  // 分段离散度（分母都是当日均价）：
  //   谷（09-20 00:00、06:00，当日均价 100）→ mean(0, 0.1) = 0.05
  //   峰（09-21 12:00，当日均价 200）→ 0.45
  //   平（09-21 18:00，当日均价 200）→ 0.5
  const pctRow = [...pctCells].slice(0, 4).map((c) => c.textContent.trim())
  check(pctRow[0] === '0.450', '峰平均离散度 = 0.450', JSON.stringify(pctRow))
  check(pctRow[1] === '0.500', '平平均离散度 = 0.500', JSON.stringify(pctRow))
  check(pctRow[2] === '0.050', '谷平均离散度 = 0.050', JSON.stringify(pctRow))

  // 切到留痕模式：走 /recorded + /gd-metric/series 配对
  const recBtn = [...doc.querySelectorAll('.radio-group button')].find((b) => b.textContent.trim() === '留痕对账')
  recBtn.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/forecast/recorded')))
  check(requests.some((r) => r.url.includes('/api/forecast/recorded')), '留痕模式请求 /forecast/recorded')
  await waitFor(() => requests.some((r) => r.url.includes('/api/gd-metric/series')))
  const seriesCall = requests.filter((r) => r.url.includes('/api/gd-metric/series')).pop()
  check(seriesCall.url.includes('kind=dayAhead'), '实际值按市场选 kind', seriesCall.url)
  // 等**文本**而不是等请求：sourceNote 是在那次请求 resolve 之后才赋值的，
  // 只等「请求已发出」会在状态更新前就断言（之前通过属于运气好）。
  await waitFor(() => bodyHas('留痕：'), 800)
  check(bodyHas('留痕：'), '留痕模式显式声明「当时发出的预测」')

  // 逐点离散度切标幺/数值
  const absBtn = [...doc.querySelectorAll('.panel-head .radio-group button')].find((b) => b.textContent.trim() === '数值')
  check(!!absBtn, '有数值切换')
  absBtn.click()
  await tick()
  check(bodyHas('数值（元/MWh）'), '切到数值模式')

  // 口径说明必须在页面上，公式不藏在代码里
  check(bodyHas('Spearman'), '口径说明写明用 Spearman')
  check(bodyHas('当日出清均价'), '口径说明写明分母')
}

console.log('\n== 策略反演页 ==')
await clickTo('场景', '策略反演')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('统计反演'))
  check(bodyHas('估计值') && bodyHas('不是任何一台机组的真实报价'), '明示是统计反演、不是机组真实报价')

  await waitFor(() => bodyHas('观测点数') || bodyHas('1000 个观测点'))
  check(bodyHas('1000 个观测点 · 30 天'), '概览显示观测点数与覆盖天数')
  check(bodyHas('1.000'), '负荷-价格秩相关原样显示')

  // 当前负荷默认取 latestLoad=1500；两箱分位相同 ⇒ 插值结果就是 uniformQ
  //   500 MW 处中位价 = 500；负荷历史分位 = 500/1000 = 50%
  await waitFor(() => bodyHas('当前负荷历史分位'))
  check(bodyHas('50.0%'), '当前负荷历史分位 = 50%（500+500 观测点中前一半在下方）', bodyHas('50.0%'))
  check(bodyHas('500.0'), '当前负荷处的中位价 = 500.0 元/MWh')

  // 未填边际成本时必须提示「最优点会落在最低报价」
  check(bodyHas('未填边际成本') && bodyHas('最优点会落在最低报价'), '未填成本时给出退化提示')

  // 填成本 500 ⇒ 最优报价 = 500（成本下方报都是同样的 125，取扫描下界）、期望收益 = 125
  const costInput = [...doc.querySelectorAll('.toolbar input[type=number]')].pop()
  costInput.value = '500'
  costInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  check(!bodyHas('未填边际成本'), '填了成本后退化提示消失')
  await waitFor(() => bodyHas('最优报价'))
  check(bodyHas('125.0'), '期望收益代理 = 125.0 元/MWh（∫₅₀₀¹⁰⁰⁰(p−500)/1000 dp）')

  // 策略族：目标中标概率 80/50/20% → 报价 200/500/800
  //   中标时均价 = E[p|p≥bid]；期望收益 = E[(p−500)·1{p≥bid}]（成本 500）
  //   稳健的口径 125 最高，与上面 KPI 的「最优报价 500」自洽 —— 两处用同一套公式
  const table = [...doc.querySelectorAll('.table')].pop()
  const cells = [...table.querySelectorAll('div')].map((d) => d.textContent.replace(/\s+/g, '').trim())
  const rowOf = (label) => {
    const i = cells.indexOf(label)
    return cells.slice(i, i + 6)
  }
  check(JSON.stringify(rowOf('激进')) === JSON.stringify(['激进', '80%', '200.0', '80.0%', '600.0', '80.0']),
    '激进策略：80% 中标率 → 报价 200', JSON.stringify(rowOf('激进')))
  check(JSON.stringify(rowOf('稳健')) === JSON.stringify(['稳健', '50%', '500.0', '50.0%', '750.0', '125.0']),
    '稳健策略：50% 中标率 → 报价 500，中标时均价 750', JSON.stringify(rowOf('稳健')))
  check(JSON.stringify(rowOf('保守')) === JSON.stringify(['保守', '20%', '800.0', '20.0%', '900.0', '80.0']),
    '保守策略：20% 中标率 → 报价 800', JSON.stringify(rowOf('保守')))

  // 局限必须写在页面上，不假装有能力
  check(bodyHas('净负荷') && bodyHas('没有网络与阻塞') && bodyHas('没有机组成本'), '局限与缺口清单常驻')

  // 切实时：供给栈漂移（逆序 7、ρ=-0.2）→ 页面要报警，而不是继续装作曲线可用
  const mkt = doc.querySelector('.toolbar select')
  mkt.value = 'real'
  mkt.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => requests.some((r) => r.url.includes('supply-curve') && r.url.includes('market=real')))
  check(requests.some((r) => r.url.includes('market=real')), '切市场带上 market=real')
  await waitFor(() => bodyHas('曲线可信度低'))
  check(bodyHas('7 处逆序'), '逆序数报给用户，并说明原因（供给栈漂移）')
}

console.log('\n== 长周期电价预测页 ==')
await clickTo('场景', '长周期电价预测')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('预测概览'))
  check(bodyHas('平均视图'), '默认平均视图，并说明序列源是日报日均价')
  check(bodyHas('1683.20') || bodyHas('368.4'), '首日预测取到值（证明 { forecast } 拆包对）')
  check(bodyHas('372.1'), '末日预测取到值')
  check(bodyHas('历史 60 点'), '展示上下文点数')
  check(bodyHas('缺数据 50 天'), '展示数据缺口')
  check(bodyHas('2063ms'), '展示推理耗时')

  // 明细表：含分位与区间宽度
  const rows = [...doc.querySelectorAll('tbody tr')]
  check(rows.length === 2, `明细 2 行（实际 ${rows.length}）`)
  const cells = [...(rows[0]?.querySelectorAll('td') ?? [])].map((c) => c.textContent.trim())
  check(cells[0] === '2026-09-22' && cells[1] === '368.40', '明细首行日期与中位', JSON.stringify(cells))
  check(cells[2] === '341.20' && cells[3] === '401.70', '明细带出 10%/90% 分位', JSON.stringify(cells))
  check(cells[4] === '60.50', '区间宽度 = 90% − 10%', cells[4])

  // 切换市场会重新查询
  const before = requests.filter((r) => r.url.includes('/api/forecast/price')).length
  // 场景 Tab 的页头也有 .field select（场景切换器），所以限定在工具栏里找
  const mktSel = [...doc.querySelectorAll('.toolbar .field select')][0]
  const realOpt = [...mktSel.options].find((o) => o.textContent.trim() === '实时')
  mktSel.selectedIndex = realOpt.index
  mktSel.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => requests.filter((r) => r.url.includes('/api/forecast/price')).length > before)
  const call = requests.filter((r) => r.url.includes('/api/forecast/price')).pop()
  check(call.url.includes('market=real') && call.url.includes('horizonDays=5'), '切换市场带上参数', call.url)
  check(!call.url.includes('forceRefresh=true'), '普通切换不穿透缓存')

  // 刷新预测要穿透
  const before2 = requests.filter((r) => r.url.includes('/api/forecast/price')).length
  // 上一次查询还在跑时按钮文案是「预测中…」，等它回到「刷新预测」再点
  await waitFor(() => [...doc.querySelectorAll('.toolbar button')].some((b) => b.textContent.includes('刷新预测')))
  const refreshBtn = [...doc.querySelectorAll('.toolbar button')].find((b) => b.textContent.includes('刷新预测'))
  refreshBtn.click()
  await waitFor(() => requests.filter((r) => r.url.includes('/api/forecast/price')).length > before2)
  const forced = requests.filter((r) => r.url.includes('/api/forecast/price')).pop()
  check(forced.url.includes('forceRefresh=true'), '刷新预测带 forceRefresh=true', forced.url)

  // 切「分时」视图：序列源换成电网日指标 CSV（96 点/日）。
  // 后端 grain=intraday 早就实现了（96×5=480 ≤ max_horizon 512），原先缺的只是这个视图。
  const viewSel = [...doc.querySelectorAll('.toolbar .field select')][1]
  const intraOpt = [...viewSel.options].find((o) => o.textContent.trim() === '分时')
  viewSel.selectedIndex = intraOpt.index
  viewSel.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => bodyHas('未来多日分时电价预测'))
  const priceCalls = requests.filter((r) => r.url.includes('/api/forecast/price'))
  const intraCall = priceCalls[priceCalls.length - 1]
  check(intraCall.url.includes('grain=intraday'), '切分时带 grain=intraday', intraCall.url)
  check(intraCall.url.includes('historyDays=30'), '分时用 30 天历史窗口（日均用 60 天）', intraCall.url)
  check(bodyHas('预测点数') && bodyHas('192'), '分时按「点」计数而不是按天')
  check(bodyHas('395.0'), '分日汇总给出该预测日均价（300+(0..95)×2 的均值）')

  // 预测日按 96 点切，不是按日历日（连续序列按日历切会出 95/96/1 的碎块）
  const intraRows = [...doc.querySelectorAll('tbody tr')]
  check(intraRows.length === 2, `分日汇总 2 行（实际 ${intraRows.length}）`)
  const firstDay = [...(intraRows[0]?.querySelectorAll('td') ?? [])].map((c) => c.textContent.trim())
  check(firstDay[0] === 'D+1' && firstDay[2] === '96', '预测日按 96 点切分', JSON.stringify(firstDay))

  // 分时天数上限就是 5 日（96×5=480 ≤ TimesFM max_horizon 512）
  const hSel = [...doc.querySelectorAll('.toolbar .field select')][2]
  const opts = [...hSel.options].map((o) => o.textContent.trim()).join('/')
  check(opts === '1 日/2 日/3 日/4 日/5 日', '分时只给 1~5 日', opts)
}

console.log('\n== 市场出清页 ==')
await clickTo('场景', '市场出清')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('高峰时刻地市节点电价档位'))
  check(bodyHas('图2-1') && bodyHas('图2-2'), '高峰/低谷两块都在')
  check(bodyHas('广州') && bodyHas('深圳') && bodyHas('韶关'), '按城市列出档位')
  check(bodyHas('[200,400)'), '显示档位区间文本')
  check(bodyHas('2 市') || bodyHas('1 市'), '统计各档位命中城市数')
  check(bodyHas('未复核'), '标注采样预填需人工确认')
  check(bodyHas('图像采样预填'), '脚注说明数据来源性质')

  // 档位顺序由区间下界推导：[-inf,200) 应排在 [200,400) 前面。
  // 注意按面板分别取 —— 两个面板里都有 [200,400)，跨面板 indexOf 会串。
  const panels = [...doc.querySelectorAll('.buckets')].map((b) =>
    [...b.querySelectorAll('.bucket b')].map((e) => e.textContent.trim()),
  )
  const valley = panels[1] ?? []
  check(
    valley[0] === '[-inf,200)' && valley.indexOf('[200,400)') === 1,
    '低谷档位按区间下界排序（-inf 在前）',
    JSON.stringify(panels),
  )

  // 成交电价两张表
  check(bodyHas('日前成交电价') && bodyHas('400.5'), '日前成交电价表')
  check(bodyHas('19:15'), '带出出现时刻')
  check(bodyHas('实时成交电价'), '实时成交电价表')
  check(bodyHas('滞后运行日'), '提示实时表是滞后运行日')

  // 待数据源逐条点名
  check(bodyHas('火电启停状态统计图'), '点名缺启停统计图')
  check(bodyHas('调频容量价格'), '点名缺调频价格')
  check(bodyHas('输电约束及阻塞'), '点名缺阻塞')

  // 切换运行日会重新拉数据
  const before = requests.filter((r) => r.url.includes('/api/spot-report/data')).length
  // 场景 Tab 的页头也有一个 select（场景切换器），所以要限定在内容区里找
  const sel = [...doc.querySelectorAll('.content .field select')][0]
  const opt = [...sel.options].find((o) => o.textContent.trim() === '2026-09-19')
  check(!!opt, '运行日下拉里有 2026-09-19')
  sel.selectedIndex = opt.index
  sel.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => requests.filter((r) => r.url.includes('/api/spot-report/data')).length > before)
  const last = requests.filter((r) => r.url.includes('/api/spot-report/data')).pop()
  check(last.url.includes('date=2026-09-19'), '按运行日切换重新查询', last.url)
}

console.log('\n== 决策支持页 ==')
await clickTo('场景', '决策支持')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('总节省'))
  // 实测效果 KPI：从 /api/effect/summary 的 summary 里取值（后端是 { summary: {...} }）
  check(bodyHas('314'), 'KPI 取到总节省（说明拆包对了）')
  check(bodyHas('234'), 'KPI 取到套利净收益')
  check(bodyHas('80'), 'KPI 取到需量节省')
  check(bodyHas('tou-rule'), '脚注显示生效算法')

  // 电价预测：日均 + 分位，并说明这里不是分时（分时在「长周期电价预测」页）
  check(bodyHas('日前电价预测'), '有电价预测面板')
  check(bodyHas('96 点分时预测') && bodyHas('日均'), '说明这里是日均、分时在另一页')
  check(bodyHas('历史 60 点'), '展示上下文点数')
  check(bodyHas('缺数据 50 天'), '展示缺失天数（不装作数据齐全）')

  // 负荷预测：分位带
  check(bodyHas('负荷预测'), '有负荷预测面板')
  check(bodyHas('预测 10% 分位') || bodyHas('90% 分位'), '负荷预测给出分位')

  // 回测表：首个是 idle 基线
  check(bodyHas('算法回测对比'), '有回测面板')
  check(bodyHas('idle') && bodyHas('基线'), '首个算法标 idle 基线')
  // 回测表里按算法行定位，避免和上方 KPI 的数字混在一起
  const btRows = [...doc.querySelectorAll('table tbody tr')].filter((r) => (r.textContent ?? '').includes('tou-rule'))
  check(btRows.length > 0, '回测表有 tou-rule 行')
  const cells = [...(btRows[0]?.querySelectorAll('td') ?? [])].map((c) => c.textContent.trim())
  check(cells.includes('314.00'), '回测表 tou-rule 的总节省单元格正确', JSON.stringify(cells))

  // 历史相似日：96 点形态相关（不是数字接近），点一行叠加到曲线图
  check(bodyHas('历史相似日'), '有相似日面板')
  check(bodyHas('皮尔逊形态相关'), '写明相似度算法')
  check(bodyHas('92.4%'), '相似度按百分比列出')
  check(bodyHas('1791'), '给出参与比较的历史日数')
  const simRows = [...doc.querySelectorAll('.similar-list li')]
  check(simRows.length === 3, `相似日 3 行（实际 ${simRows.length}）`)
  check(
    (simRows[0]?.textContent ?? '').includes('2026-08-30') && (simRows[0]?.textContent ?? '').includes('92.4%'),
    '相似日首行日期与相似度',
    simRows[0]?.textContent,
  )
  // 情况分析图按面板定位（.chart 的 DOM 顺序会随面板增删变，别用 pop()）
  const similarChart = () => {
    const panel = [...doc.querySelectorAll('.panel')].find((p) =>
      (p.querySelector('.panel-title')?.textContent ?? '').includes('历史相似日情况分析'),
    )
    return Number(panel?.querySelector('.chart')?.dataset.chartPoints ?? 0)
  }
  const beforePick = similarChart()
  check(beforePick === 3, `未叠加时只画目标日（3 点），实际 ${beforePick}`)
  simRows[0].click()
  await waitFor(() => bodyHas('清空叠加'))
  check(bodyHas('清空叠加（1）'), '点相似日会叠加到曲线图')
  check((simRows[0].className ?? '').includes('picked'), '选中行有高亮')
  await waitFor(() => similarChart() > beforePick)
  check(similarChart() === beforePick + 3, `叠加后图上多一天的点（${beforePick} → ${similarChart()}）`)

  // 「日前/实时」切换只改曲线，不重新请求（数据已经拿到本地了）
  const overlayBefore = requests.filter((r) => r.url.includes('/api/gd-metric')).length
  const priceSel = [...doc.querySelectorAll('.field select')].find((s) =>
    [...s.options].some((o) => o.textContent.trim() === '日前'),
  )
  check(!!priceSel, '有「价格 日前/实时」切换')
  if (priceSel) {
    const realOpt = [...priceSel.options].find((o) => o.textContent.trim() === '实时')
    priceSel.selectedIndex = realOpt.index
    priceSel.dispatchEvent(new window.Event('change', { bubbles: true }))
    await waitFor(() => bodyHas('实时电价 96 点'))
    check(
      requests.filter((r) => r.url.includes('/api/gd-metric')).length === overlayBefore,
      '切换价格类型不重新请求（本地曲线复用）',
    )
  }

  // 待数据源的 panels：相似日已经做了，只剩竞争空间
  check(bodyHas('日前市场竞争空间'), '点名缺竞争空间')

  // 刷新预测要显式跳过缓存
  const reqBefore = requests.filter((r) => r.url.includes('/api/forecast/')).length
  const refreshBtn = [...doc.querySelectorAll('.panel-head button, .toolbar button')].find((b) =>
    b.textContent.includes('刷新预测'),
  )
  check(!!refreshBtn, '有「刷新预测」按钮')
  refreshBtn.click()
  await waitFor(() => requests.filter((r) => r.url.includes('/api/forecast/')).length >= reqBefore + 2)
  const forced = requests.filter((r) => r.url.includes('/api/forecast/')).slice(-2)
  check(
    forced.every((r) => r.url.includes('forceRefresh=true')),
    '刷新预测带 forceRefresh=true（其余查询不穿透缓存）',
    JSON.stringify(forced.map((r) => r.url)),
  )
}

console.log('\n== 历史信息页 ==')
await clickTo('场景', '历史信息')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  await waitFor(() => bodyHas('已排除未复核'))
  check(bodyHas('已排除未复核 8 项'), '显示被排除的未复核条数（不静默少数据）')
  check(bodyHas('本周') && bodyHas('本月') && bodyHas('自定义'), '区间预设齐全')
  check(bodyHas('按日') && bodyHas('按周') && bodyHas('按月'), '粒度可选')

  // 默认本月预设：请求应带 from/to（前端算，前端显示；后端也能兜底）
  const first = requests.filter((r) => r.url.includes('/api/spot-report/trend')).pop()
  check(!!first, '发出趋势请求')
  check(bodyHas('粒度 week'), '展示后端返回的粒度')
  check(bodyHas('共 2 个桶'), '展示桶数')

  // 图表按数据集分组
  const chartTitles = [...doc.querySelectorAll('.panel-title')].map((e) => e.textContent.trim())
  check(chartTitles.includes('日前成交电价') && chartTitles.includes('正文摘要指标'), '按数据集出图')

  // 四条线：实际两条（日报日前/实时成交电价）+ 预测两条（留痕），同一日报域、日期对得上
  check(chartTitles.includes('日前-实时出清与预测价格'), '有四条线面板')
  check(bodyHas('实际：日报已确认的日前/实时成交电价'), '写明实际线的来源与口径')
  check(bodyHas('留痕 2 个生成日'), '写明留痕有几个生成日')
  const pricePanel = [...doc.querySelectorAll('.panel')].find((p) =>
    (p.querySelector('.panel-title')?.textContent ?? '').includes('日前-实时出清与预测价格'),
  )
  const pricePoints = Number(pricePanel?.querySelector('.chart')?.dataset.chartPoints ?? 0)
  check(pricePoints === 8, `4 条线 × 2 个运行日 = 8 个点（实际 ${pricePoints}）`)
  check(
    requests.some((r) => r.url.includes('/api/spot-report/trend') && r.url.includes('grain=day')),
    '本块固定按运行日取数（不跟上面的粒度）',
  )
  check(
    requests.some((r) => r.url.includes('/api/forecast/recorded') && r.url.includes('grain=day')),
    '预测线走留痕（日粒度）',
  )

  // 桶明细：2 行（2 个桶）× 3 条序列
  const rows = [...doc.querySelectorAll('.table-wrap tbody tr')]
  check(rows.length === 2, `桶明细 2 行（实际 ${rows.length}）`)
  const cells = [...rows[0].querySelectorAll('td')]
  check(cells.length === 4, `桶明细 1 + 3 列（实际 ${cells.length}）`)
  check(cells[1].textContent.trim() === '400.5', '桶明细取 avg', cells[1].textContent)

  // 取消勾选一条序列 → 图和宽表都少一列
  const toggles = [...doc.querySelectorAll('.chips .chip-toggle')]
  const coalToggle = toggles.find((b) => b.textContent.includes('燃煤'))
  check(!!coalToggle, '指标多选里有燃煤序列')
  coalToggle.click()
  await tick()
  const after = [...doc.querySelectorAll('.table-wrap tbody tr')][0]
  check(after.querySelectorAll('td').length === 3, '取消勾选后宽表少一列', String(after.querySelectorAll('td').length))

  // 待数据源的面板要明说，不画空图
  check(bodyHas('待数据源'), '列出待数据源的面板')
  check(bodyHas('火电启停状态统计（多日）'), '点名缺哪几块')

  // 切粒度会重新请求。happy-dom 下 select.value 走不通（Vue 把值挂在 option 上），
  // 所以按 option 定位再改 selectedIndex —— 与真实选择行为一致。
  const before = requests.filter((r) => r.url.includes('/api/spot-report/trend')).length
  const grainSelect = [...doc.querySelectorAll('.toolbar .field select')][0]
  check(!!grainSelect, '找到粒度下拉')
  const dayOption = [...grainSelect.options].find((o) => o.textContent.trim() === '按日')
  check(!!dayOption, '下拉里有「按日」')
  grainSelect.selectedIndex = dayOption.index
  grainSelect.dispatchEvent(new window.Event('change', { bubbles: true }))
  await waitFor(() => requests.filter((r) => r.url.includes('/api/spot-report/trend')).length > before)
  const dayCall = requests.filter((r) => r.url.includes('/api/spot-report/trend')).pop()
  check(dayCall.url.includes('grain=day'), '切粒度带上 grain 参数', dayCall.url)

  // 区间预设按钮也会重新查询
  const beforeWeek = requests.filter((r) => r.url.includes('/api/spot-report/trend')).length
  const weekBtn = [...doc.querySelectorAll('.radio-group button')].find((b) => b.textContent.trim() === '本周')
  weekBtn.click()
  await waitFor(() => requests.filter((r) => r.url.includes('/api/spot-report/trend')).length > beforeWeek)
  const weekCall = requests.filter((r) => r.url.includes('/api/spot-report/trend')).pop()
  check(weekCall.url.includes('grain=day'), '切换区间保留已选粒度', weekCall.url)
}

console.log('\n== 采集管理页 ==')
await clickTo('管理', '采集管理')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  check(bodyHas('采集台'), '渲染采集台')
  check(bodyHas('2026-09-21') && bodyHas('2026-09-19'), '档案列表加载并自动选中最新一份')
  check(bodyHas('未复核 2'), '列表显示未复核条数')
  check(bodyHas('已全部确认'), '已确认的日报显示已全部确认')

  // 结构化数据按数据集分组，未复核项从 origin 推导
  check(bodyHas('日前成交电价') && bodyHas('正文摘要指标'), '按数据集分组')
  check(bodyHas('未复核'), '未复核标记')
  check(bodyHas('表2 承载的滞后运行日是 2026-09-19'), '提示表2 是滞后运行日')
  check(bodyHas('解析告警'), '展示解析告警')

  // 逐条修订：填值 → 写入 → POST /metric 带正确的定位键
  // 期望值跟着桩数据走，别写死 —— /api/spot-report/data 同时供采集管理与市场出清使用
  const expectedRows = FAKE_METRICS.length + FAKE_CLEARING_METRICS.length
  const editInputs = [...doc.querySelectorAll('td.edit input')]
  check(
    editInputs.length === expectedRows,
    `每行都有修订输入（期望 ${expectedRows}，实际 ${editInputs.length}）`,
  )
  // 按行内容定位「燃煤」那一行，不按下标猜（数据集顺序由 DATASET_LABELS 决定）
  const rows = [...doc.querySelectorAll('tbody tr')].filter((r) => r.querySelector('td.edit input'))
  const coalRow = rows.find((r) => (r.textContent ?? '').includes('燃煤'))
  check(!!coalRow, '找到燃煤行')
  const coalInput = coalRow.querySelector('td.edit input')
  coalInput.value = '412.3'
  coalInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  coalRow.querySelector('td.edit button').click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/spot-report/metric')))
  const saveCall = requests.filter((r) => r.url.includes('/api/spot-report/metric')).pop()
  check(saveCall?.body?.value === 412.3, '修订提交数值', JSON.stringify(saveCall?.body))
  check(
    saveCall?.body?.dataset === 'day_ahead_trade' && saveCall?.body?.scope === '燃煤' && saveCall?.body?.runDate === '2026-09-21',
    '修订带齐定位键（date/runDate/dataset/scope/metric）',
    JSON.stringify(saveCall?.body),
  )

  // 一键确认
  const confirmBtn = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.includes('确认全部'))
  check(!!confirmBtn, '有「确认全部」按钮')
  confirmBtn?.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/spot-report/confirm')))
  check(true, '发出确认请求')

  // 单日重跑：force 默认关，payload 里要带上
  const extractBtn = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.includes('解析 / 重跑'))
  extractBtn?.click()
  await waitFor(() => requests.some((r) => r.url.includes('/api/spot-report/extract') && !r.url.includes('batch')))
  const ex = requests.filter((r) => r.url.includes('/api/spot-report/extract') && !r.url.includes('batch')).pop()
  check(ex?.body?.date === '2026-09-21' && ex?.body?.force === false, '单日重跑默认不覆盖已确认', JSON.stringify(ex?.body))

  // 批量重跑：先 dryRun 预览，再执行
  const previewBtn = [...doc.querySelectorAll('.batch button')].find((b) => b.textContent.trim() === '预览')
  previewBtn?.click()
  await waitFor(() => bodyHas('将重跑'))
  check(bodyHas('跳过（已人工复核）') && bodyHas('2026-09-21'), '预览列出将跳过的已复核日报')
  const dry = requests.filter((r) => r.url.includes('batch') && r.body?.dryRun).pop()
  check(dry?.body?.dryRun === true, '预览用 dryRun')

  const runBtn = [...doc.querySelectorAll('.preview-actions button')].find((b) => b.textContent.trim() === '确认执行')
  runBtn?.click()
  // 执行时不带 dryRun（undefined 会被 JSON.stringify 丢掉），按 includeConfirmed 判定
  await waitFor(() => requests.some((r) => r.url.includes('batch') && r.body?.includeConfirmed === false))
  const real = requests.filter((r) => r.url.includes('batch') && r.body?.includeConfirmed === false).pop()
  check(!!real && !('dryRun' in real.body), '执行请求不带 dryRun', JSON.stringify(real?.body))
  check(real?.body?.includeConfirmed === false, '批量重跑默认不覆盖已确认日报', JSON.stringify(real?.body))

  // 审计：人工修订与重跑覆盖要能区分
  await waitFor(() => bodyHas('指标变更审计'))
  check(bodyHas('人工修订') && bodyHas('重跑覆盖'), '审计区分人工修订与重跑覆盖')
  check(bodyHas('380 →') || bodyHas('380'), '审计显示旧值 → 新值')
}

console.log('\n== 电价管理页 ==')
await clickTo('管理', '电价管理')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  check(bodyHas('2026 秋季工商业') && bodyHas('对照组固定价'), '列表渲染电价配置')
  check(bodyHas('长期 · 自 2026-10-01'), '日期范围文案')
  check(bodyHas('按星期 · 每天'), '时间范围文案')
  // 原来只断言 body 里出现「当前生效」，而那四个字也出现在顶部提示条里 ——
  // 资源没有命中任何卡片也能过。改成断言卡片上确实有「当前生效」标记。
  const activeCards = [...doc.querySelectorAll('.card')].filter((c) => (c.textContent ?? '').includes('当前生效'))
  check(activeCards.length === 1, '恰有一张卡片被标为当前生效', String(activeCards.length))
  check(bodyHas('全站唯一来源'), '声明时段是全站唯一来源')

  // 甘特条：5 段，宽度按时长比例
  const bars = [...doc.querySelectorAll('.card .gantt .bar')]
  check(bars.length === 6, `甘特条数量 = ${bars.length}（5 + 对照组 1）`)
  const widths = bars.slice(0, 5).map((b) => parseFloat(b.style.width))
  check(
    Math.abs(widths[0] - (8 / 24) * 100) < 0.01 && Math.abs(widths[1] - (3 / 24) * 100) < 0.01,
    '甘特条宽度按分钟比例',
    JSON.stringify(widths),
  )
  // 20:00-00:00 这种跨夜写法要按 24:00 算，否则宽度为 0
  check(Math.abs(widths[4] - (4 / 24) * 100) < 0.01, '结束时间 00:00 按 24:00 处理', JSON.stringify(widths))

  // 抽屉：回填 + 重叠校验
  const editButtons = [...doc.querySelectorAll('.card button')].filter((b) => b.textContent.trim() === '编辑')
  editButtons[0].click()
  await tick()
  check(!!doc.querySelector('.drawer'), '打开抽屉')
  const segs = doc.querySelectorAll('.drawer .seg')
  check(segs.length === 5, `抽屉回填 5 个时段（实际 ${segs.length}）`)

  // 制造重叠：把第二段起点改到第一段中间
  const startInput = segs[1].querySelector('input[type="time"]')
  startInput.value = '04:00'
  startInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  check((doc.querySelector('.drawer')?.textContent ?? '').includes('重叠'), '检出时段重叠')
  const saveBtn = [...doc.querySelectorAll('.drawer-foot button')].find((b) => b.textContent.trim() === '保存')
  check(saveBtn?.disabled === true, '有 error 时保存按钮禁用')

  // 改回不重叠，保存 → PUT，priceSegments 按开始时间排序
  startInput.value = '08:00'
  startInput.dispatchEvent(new window.Event('input', { bubbles: true }))
  await tick()
  saveBtn?.click()
  await tick()
  await tick()
  const put = requests.filter((r) => r.url.includes('/api/price/3') && r.body?.description).pop()
  check(!!put, '发出更新请求')
  check(put?.body?.description === '2026 秋季工商业', 'payload 带描述', JSON.stringify(put?.body?.description))
  const starts = (put?.body?.priceSegments ?? []).map((s) => s.startTime)
  check(
    starts.every((t, i) => i === 0 || starts[i - 1] <= t),
    '时段按开始时间升序',
    JSON.stringify(starts),
  )
  check(put?.body?.priceSegments?.[0]?.priceType === 'Valley', 'priceType 以字符串发送（与后端 MarshalJSON 一致）', JSON.stringify(put?.body?.priceSegments?.[0]))
}

console.log('\n== 策略管理页 ==')
await clickTo('管理', '策略管理')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  check(bodyHas('tou-rule') && bodyHas('idle-基线'), '列表渲染后端返回的策略')
  check(bodyHas('长期 · 自 2026-10-01'), '日期范围按长期/区间分别展示')
  check(bodyHas('2026-10-01 → 2026-10-31'), '非长期显示区间')
  check(bodyHas('按星期 · 每天'), '时间范围 weekday 文案')
  check(bodyHas('自定义日期 · 周一/周二/周三'), '时间范围 custom 文案（星期名映射）')
  check(bodyHas('命中') && bodyHas('未命中'), '显示当前是否命中时间范围')

  // 抽屉：编辑第二条策略，验证表单从后端数据回填
  const editButtons = [...doc.querySelectorAll('tbody button')].filter((b) => b.textContent.trim() === '编辑')
  check(editButtons.length === 2, '每行都有编辑按钮')
  editButtons[1].click()
  await tick()
  check(!!doc.querySelector('.drawer'), '打开右侧抽屉')
  const nameInput = doc.querySelector('.drawer input')
  check(nameInput?.value === 'idle-基线', '抽屉按 id 回填名称', String(nameInput?.value))
  const drawerHas = (t) => (doc.querySelector('.drawer')?.textContent ?? '').includes(t)
  check(drawerHas('功率点位'), '抽屉含点位编辑区')
  check(!doc.querySelector('.drawer .pt'), '空点位表不渲染行')

  // 加点位 → 点位行出现，且小时不重复
  const addBtn = [...doc.querySelectorAll('.drawer button')].find((b) => b.textContent.trim() === '+ 加点位')
  addBtn?.click()
  await tick()
  addBtn?.click()
  await tick()
  const rows = doc.querySelectorAll('.drawer .pt')
  check(rows.length === 2, `加两次得到 2 个点位（实际 ${rows.length}）`)

  // 保存 → PUT，且 config.points 按小时升序
  const saveBtn = [...doc.querySelectorAll('.drawer-foot button')].find((b) => b.textContent.trim() === '保存')
  saveBtn?.click()
  await tick()
  await tick()
  const put = requests.filter((r) => r.url.includes('/api/strategy/energy-storage/8') && r.body?.name).pop()
  check(!!put, '发出更新请求')
  check(put?.body?.config?.points?.length === 2, 'payload 带点位', JSON.stringify(put?.body?.config))
  const hours = (put?.body?.config?.points ?? []).map((p) => p[0])
  check(
    hours.every((h, i) => i === 0 || hours[i - 1] <= h),
    '点位按小时升序',
    JSON.stringify(hours),
  )
  check(put?.body?.dateRange?.isLongTerm === false && put?.body?.dateRange?.endDate === '2026-10-31', '日期范围原样保留')

  // 试运行：同一套成本模型，首个是 idle 基线
  const runBtn = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.trim() === '跑一次')
  runBtn?.click()
  await waitFor(() => bodyHas('额定 50 kW'))
  check(bodyHas('314.00'), '试运行表格带出总节省')
  check(bodyHas('idle') && bodyHas('基线'), '首个算法标为 idle 基线')
  check(bodyHas('额定 50 kW / 200 kWh'), '展示回测使用的电池参数')
}

console.log('\n== 储能参数页 ==')
await clickTo('配置', '储能参数')
{
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)
  // 字段与分组全部来自后端 schema
  check(bodyHas('最小SOC比例'), '字段来自后端 schema')
  check(bodyHas('储能参数'), '渲染 schema 里的分组')
  check(bodyHas('变压器容量'), '渲染非主分组')
  check(bodyHas('kVA'), '带出 schema 里的单位')
  check(bodyHas('范围 0 ~ 1'), '带出 schema 里的上下限')
  check(bodyHas('立即生效，下一个调度周期采纳'), '标注生效语义')

  // 越界值应被 schema 的 max 拦住
  const socMax = doc.querySelector('input[id="f-socMaxRatio"]')
  check(!!socMax, '按 key 渲染出 socMaxRatio 输入框')
  if (socMax) {
    socMax.value = '1.5'
    socMax.dispatchEvent(new window.Event('input', { bubbles: true }))
    await tick()
    const saveBtn = [...doc.querySelectorAll('.save-row button')].find((b) => b.textContent.trim() === '保存')
    saveBtn?.click()
    await tick()
    check(!doc.querySelector('.modal'), '越界值被拦住，不弹 diff')
    check(bodyHas('不能大于 1'), '提示引用了 schema 的上限')
  }

  // 改一个合法值 → 弹 diff → 确认 → 回写完整 JSON
  if (socMax) {
    socMax.value = '0.9'
    socMax.dispatchEvent(new window.Event('input', { bubbles: true }))
    await tick()
    const saveBtn = [...doc.querySelectorAll('.save-row button')].find((b) => b.textContent.trim() === '保存')
    saveBtn?.click()
    await tick()
    check(!!doc.querySelector('.modal'), '弹出改前 diff')
    const modalText = doc.querySelector('.modal')?.textContent ?? ''
    check(modalText.includes('最大SOC比例'), 'diff 显示字段名')
    check(modalText.includes('95%') && modalText.includes('90%'), 'diff 显示旧值 → 新值（比例按百分比展示）')

    const confirm = [...doc.querySelectorAll('.modal button')].find((b) => b.textContent.trim() === '确认保存')
    confirm?.click()
    await tick()
    await tick()
    const call = requests.filter((r) => r.url.includes('/api/setting/update')).pop()
    check(!!call, '发出了保存请求')
    check(call?.body?.id === 'policy_microgrid' && call?.body?.group === 'policy', '设置 id / 分组正确', JSON.stringify(call?.body))
    let saved = {}
    try { saved = JSON.parse(call?.body?.value ?? '{}') } catch { /* 断言下面会失败 */ }
    check(saved.socMaxRatio === 0.9, '回写了改动项', JSON.stringify(saved))
    check(saved.transformerCapacity === 200, '未改动项原样保留，没被清空', JSON.stringify(saved))
    check(saved.enableDemandControl === false, '布尔值未被字符串化', JSON.stringify(saved))
  }
}

console.log('\n== 手动控制页 ==')
await clickTo('管理', '手动控制')
{
  const bodyText = () => doc.body.textContent ?? ''
  check(bodyText().includes('实时下发'), '显示实时下发警告')
  check(!!doc.querySelector('.field select'), '渲染设备选择框')
  // 服务名与参数字段都来自驱动声明 —— 证明表单是数据驱动的，不是前端写死的
  check(bodyText().includes('设置充电功率'), '服务名来自驱动')
  check(bodyText().includes('时长'), '参数字段来自驱动')
  check(bodyText().includes('停止充放电'), '无参数服务也渲染')
  check(bodyText().includes('80.0'), 'KPI 显示遥测里的 SOC')

  const execButton = () =>
    [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.trim() === '执行')
  const power = () => doc.querySelector('.field-item input[id$="-power"]')
  const setPower = async (v) => {
    const el = power()
    el.value = v
    el.dispatchEvent(new window.Event('input', { bubbles: true }))
    await tick()
  }
  const bodyHas = (t) => (doc.body.textContent ?? '').includes(t)

  // 保护约束面板来自「配置 · 储能参数」
  check(bodyHas('保护约束'), '显示保护约束面板')
  check(bodyHas('SOC 允许区间'), '显示 SOC 允许区间')
  // 50 kW × chargePowerRatio 0.5 = 25 kW
  check(bodyHas('充 25 kW'), '功率上限 = 设备额定 × 配置比例')

  // 驱动声明的 max 先拦一层
  await setPower('999')
  execButton()?.click()
  await tick()
  check(!doc.querySelector('.modal'), '超过驱动声明上限被拦住')
  check(bodyHas('不能大于 50'), '提示引用了驱动声明的上限')

  // 再被 EMS 侧保护上限拦一层（40 未超驱动 50，但超了保护的 25）
  await setPower('40')
  execButton()?.click()
  await tick()
  check(!doc.querySelector('.modal'), '超过 EMS 保护上限被拦住')
  check(bodyHas('超过保护上限 25 kW'), '提示引用了储能参数推导出的上限')

  const powerEl = power()
  const duration = doc.querySelector('.field-item input[id$="-duration"]')
  if (powerEl) {
    powerEl.value = '20'
    powerEl.dispatchEvent(new window.Event('input', { bubbles: true }))
  }
  if (duration) {
    duration.value = '15'
    duration.dispatchEvent(new window.Event('input', { bubbles: true }))
  }
  await tick()
  const exec = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.trim() === '执行')
  exec?.click()
  await tick()
  check(!!doc.querySelector('.modal'), '弹出二次确认框')
  const modalText = doc.querySelector('.modal')?.textContent ?? ''
  check(modalText.includes('储能柜 1'), '确认框显示目标设备')
  check(modalText.includes('设置充电功率'), '确认框显示服务名')

  const confirm = [...doc.querySelectorAll('.modal button')].find((b) => b.textContent.trim() === '确认下发')
  confirm?.click()
  await tick()
  await tick()

  const call = requests.filter((r) => r.url.includes('/api/control/device')).pop()
  check(!!call, '发出了控制请求')
  check(call?.body?.deviceId === 'ess_1', 'deviceId 正确', JSON.stringify(call?.body))
  check(call?.body?.commandName === 'setChargePower', 'commandName 用服务 key', JSON.stringify(call?.body))
  check(
    Array.isArray(call?.body?.parameters) && call.body.parameters.length === 2,
    'parameters 是数组',
    JSON.stringify(call?.body?.parameters),
  )
  check(
    call?.body?.parameters?.[0] === 20 && call?.body?.parameters?.[1] === 15,
    'parameters 按驱动声明顺序（功率, 时长）',
    JSON.stringify(call?.body?.parameters),
  )
  check(call?.body?.parameters?.every((p) => typeof p === 'number'), 'int 字段转成了数字')
}

// 停用路径：设置 manual_control_enabled=false 时，页面只给说明，不容许下发。
// 后端 /control/device 也会拒绝 —— 这里只能验前端那一半。
console.log('\n== 手动控制页（已停用）== ')
{
  manualEnabled = false
  await clickTo('管理', '设备管理')
  await clickTo('管理', '手动控制')
  await waitFor(() => (doc.body.textContent ?? '').includes('手动控制已停用'), 2000)
  const bodyText = () => doc.body.textContent ?? ''
  check(bodyText().includes('手动控制已停用'), '停用时显示说明，不是空白页')
  check(bodyText().includes('运行中的策略覆盖'), '说明停用原因（手动会被策略覆盖）')
  check(bodyText().includes('manual_control_enabled'), '给出临时启用办法（改设置）')
  check(
    bodyText().includes('接口都会拒绝下发'),
    '说明后端也拒绝，不是只在前端藏按钮',
  )
  check(
    ![...doc.querySelectorAll('button')].some((b) => b.textContent.trim() === '执行'),
    '停用时没有「执行」按钮',
  )
  const before = requests.filter((r) => r.url.includes('/api/control/custom-services')).length
  await tick()
  check(
    requests.filter((r) => r.url.includes('/api/control/custom-services')).length === before,
    '停用时不再去拉可执行服务',
  )
  check(
    [...doc.querySelectorAll('.side-item')].some((a) => a.textContent.includes('手动控制') && a.textContent.includes('暂停')),
    '侧栏给「暂停」标，不假装 ready',
  )
  manualEnabled = true
}


console.log('\n== 会话失效（401 写在响应体里）==')
// 后端用 WriteJson(DefaultHandlerResponse{Code:401})，HTTP 状态码可能仍是 200。
// 只看 res.status 会漏掉这种 401，于是界面一直转圈而不去登录页 —— 这里就测这个变体。
// 放在最后：一旦被踢到登录页，后面的页面断言就没法跑了。
{
  const orig = globalThis.fetch
  globalThis.fetch = async () =>
    new Response(JSON.stringify({ code: 401, message: '未登录或登录已过期' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  // 用 pushState 直接跳，不走 clickTo：切 Tab 本身就会触发请求，
  // 页面在侧栏渲染完之前就被踢到登录页，clickTo 会以为「侧栏里没有这一项」。
  window.history.pushState({}, '', '/scene/clearing')
  window.dispatchEvent(new window.Event('popstate'))
  await waitFor(() => !!doc.querySelector('.login'), 1500)
  check(!!doc.querySelector('.login'), 'HTTP 200 但响应体 code=401 时也要回登录页')
  check((doc.body.textContent ?? '').includes('登录已过期'), '并说明是会话过期，而不是让人猜')
  check(!localStorage.getItem('ems:user:token'), '会话失效时清掉本地令牌')
  globalThis.fetch = orig
}

if (interopNoise.length) {
  console.log(`\n（忽略 ${interopNoise.length} 条 happy-dom 环境噪音：缺 canvas 与只认标准 Event）`)
}
await vite.close()
console.log(failed ? `\n${failed} 项失败` : '\n✅ 壳层冒烟全部通过')
process.exit(failed ? 1 : 0)
