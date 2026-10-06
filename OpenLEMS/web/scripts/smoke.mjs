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
import { readFileSync } from 'node:fs'
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
  market: 'ahead', dataset: 'day_ahead_trade', scope: '发电侧', metric: '平均电价',
  unit: '元/MWh', model: 'timesfm-3.0', from: '2026-07-24', to: '2026-09-21',
  historyDates: ['2026-09-19', '2026-09-20', '2026-09-21'],
  historyValues: [380, 390, 400],
  forecast: [
    { runDate: '2026-09-22', point: 368.4, quantiles: { '0.1': 341.2, '0.5': 368.4, '0.9': 401.7 } },
    { runDate: '2026-09-23', point: 372.1, quantiles: { '0.1': 345, '0.5': 372.1, '0.9': 408 } },
  ],
  contextPoints: 60, inputTruncated: false, missingDays: 50, timeMs: 2063,
}

const requests = []
globalThis.fetch = async (url, init) => {
  requests.push({ url: String(url), body: init?.body ? JSON.parse(String(init.body)) : undefined })
  const u = String(url)
  if (u.includes('/api/device/name-list')) {
    return json({ deviceNames: { ess_1: '储能柜 1', pcs_1: 'PCS 1' } })
  }
  if (u.includes('/api/control/custom-services')) {
    return json({ services: FAKE_SERVICES })
  }
  if (u.includes('/api/device/telemetry')) {
    return json({
      telemetry: {
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
  if (u.includes('/api/effect/summary')) return json({ summary: FAKE_SUMMARY })
  if (u.includes('/api/effect/backtest2')) return json({})
  if (u.includes('/api/forecast/load')) return json({ forecast: FAKE_LOAD })
  if (u.includes('/api/forecast/price')) return json({ forecast: FAKE_PRICE })
  if (u.includes('/api/spot-report/trend')) {
    const q = new URL(u, 'http://x').searchParams
    return json({
      from: q.get('from') || '2026-10-01',
      to: q.get('to') || '2026-10-06',
      grain: q.get('grain') || 'week',
      excludedPending: 8,
      points: FAKE_TREND_POINTS,
    })
  }
  if (u.includes('/api/spot-report/collect')) return json({ report: FAKE_REPORTS[0] })
  if (u.includes('/api/spot-report/data')) {
    return json({ date: '2026-09-21', tradeDate: '2026-09-19', metrics: FAKE_METRICS, extract: FAKE_EXTRACT })
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
        days: [],
        curve: { date: '2026-09-21', load: [], dayAhead: [], realtime: [] },
      }
    : {}
  return json(body)
}

function json(data) {
  return new Response(JSON.stringify({ code: 0, data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

const rootEl = doc.createElement('div')
rootEl.id = 'app'
doc.body.appendChild(rootEl)

// --- nav 事实来源（纯 TS，可用 vite 编译） ---
const vite = await createServer({ root, server: { middlewareMode: true }, appType: 'custom', logLevel: 'error' })
const { NAV } = await vite.ssrLoadModule(resolve(root, 'src/nav/index.ts'))

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

console.log('\n== 导航结构 ==')
const tabEls = [...doc.querySelectorAll('.tabs .tab')]
check(tabEls.length === 3, `Tab 数量 = ${tabEls.length}（期望 3）`, tabEls.map((e) => e.textContent.trim()).join('/'))
check(NAV[0].pages.length === 7, `场景页 7 个（实际 ${NAV[0].pages.length}）`)
const totalPages = NAV.reduce((n, t) => n + t.pages.length, 0)
check(totalPages === 29, `页面总数 29（实际 ${totalPages}）`)

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
  await waitFor(() => sideTitles().length === tab.pages.length)

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
      if (page.status !== 'ready') {
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

  // 电价预测：日均 + 分位，且明确说明不是分时
  check(bodyHas('日前电价预测'), '有电价预测面板')
  check(bodyHas('预测是'), '明确说明是日均不是分时')
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

  // 待数据源的 panels
  check(bodyHas('日前市场竞争空间'), '点名缺竞争空间')
  check(bodyHas('历史相似日分析'), '点名缺相似日')

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
  const editInputs = [...doc.querySelectorAll('td.edit input')]
  check(editInputs.length === 3, `每行都有修订输入（实际 ${editInputs.length}）`)
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
  check(bodyHas('当前生效'), '标出当前生效的电价')
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


if (interopNoise.length) {
  console.log(`\n（忽略 ${interopNoise.length} 条 happy-dom 环境噪音：缺 canvas 与只认标准 Event）`)
}
await vite.close()
console.log(failed ? `\n${failed} 项失败` : '\n✅ 壳层冒烟全部通过')
process.exit(failed ? 1 : 0)
