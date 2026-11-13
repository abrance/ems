#!/usr/bin/env node
/**
 * 真浏览器检查：图表到底画出来了没有。
 *
 * 为什么需要（两次真实事故，冒烟都放过了）：
 *   1. 图表在数据到达前挂载，只画了首帧 → 数据到了也不重绘；
 *   2. 图表的容器被换成另一个 DOM 节点（PanelBox 加载中不渲染 slot），
 *      而 ECharts 实例还挂在脱离文档的旧节点上 → 容器空、整页没有 canvas、**且不报错**。
 *
 * 冒烟抓不到这一类，因为 happy-dom **没有 canvas**：`echarts.init` 直接抛错，
 * 被 try/catch 接住写成 data-chart-error ——「有没有画出来」在冒烟里根本不存在，
 * 它只能断言「数据到了容器上」，而那正是上面两个 bug 里都成立的部分。
 *
 * 做法：把 `dist` 起成本地站点（注入 API 桩，桩的形状照抄真实响应），
 * 用无头 Firefox 通过 Marionette 协议加载页面，然后查 DOM：
 *   每个 `.chart` 必须有 ≥1 个 canvas、尺寸不为 0，且不能带 data-chart-error。
 *
 * 环境要求：firefox（本机与 GitHub ubuntu runner 都自带）。
 *   没有 firefox 时 **默认跳过**（本地友好）；CI 里设 BROWSER_CHECK_REQUIRED=1 让它变红。
 */
import { spawn } from 'node:child_process'
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import net from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const distDir = join(webDir, 'dist')

/* ------------------------------------------------------------------ *
 * 1) API 桩：形状照抄真实响应（数据是编的，验的是「画没画出来」）
 * ------------------------------------------------------------------ */

const U = 'MW'
const stat = (avg = 3000, max = 3460, min = 2200) => ({ avg, max, min, last: avg, count: 96, unit: U })

function curve96(kind) {
  const out = []
  for (let i = 0; i < 96; i++) {
    const hh = String(Math.floor((i * 15) / 60)).padStart(2, '0')
    const mm = String((i * 15) % 60).padStart(2, '0')
    const base = kind === 'load' ? 120000 : kind === 'dayAhead' ? 380 : 300
    out.push({ time: `${hh}:${mm}`, value: base + ((i * 37) % 400) * (kind === 'load' ? 60 : 1) })
  }
  return out
}

const DAYS = ['2026-08-29', '2026-08-30', '2026-08-31']
const quantileGrid = [200, 240, 280, 320, 340, 360, 380, 400, 430, 470, 520]

const FIXTURES = {
  '/api/gd-metric': {
    latestDate: '2026-08-31',
    from: '2026-08-02',
    to: '2026-08-31',
    date: '2026-08-31',
    source: 'https://example.test/gd_metric.csv',
    updatedAt: '2026-08-31T10:00:00+08:00',
    availableDays: DAYS,
    latest: { date: '2026-08-31', load: stat(123730, 142979, 98000), dayAhead: stat(387.3, 760, 60), realtime: stat(301.7, 780, 40) },
    days: DAYS.map((d) => ({ date: d, load: stat(), dayAhead: stat(380, 760, 60), realtime: stat(365, 780, 40) })),
    curve: { date: '2026-08-31', load: curve96('load'), dayAhead: curve96('dayAhead'), realtime: curve96('realtime') },
  },
  '/api/gd-metric/series': {
    kind: 'dayAhead',
    from: '2026-08-30',
    to: '2026-08-31',
    days: ['2026-08-30', '2026-08-31'],
    points: DAYS.flatMap((date) => curve96('dayAhead').map((p) => ({ date, time: p.time, value: p.value }))),
  },
  '/api/gd-metric/similar-days': {
    targetDate: '2026-08-31',
    market: 'ahead',
    topN: 5,
    wPrice: 0.7,
    wLoad: 0.3,
    method: '皮尔逊形态相关（96 点，z-score 标准化）',
    formula: '相似度 = (0.7×ρ价格 + 0.3×ρ负荷 + 1) / 2 × 100%',
    from: '2021-05-01',
    to: '2026-08-30',
    candidates: 1791,
    timeMs: 320,
    days: DAYS.slice(0, 2).map((d, i) => ({
      date: d,
      similarity: 90 - i * 7,
      priceCorr: 0.8 - i * 0.2,
      loadCorr: 0.9,
      priceAvg: 372.5,
      loadMax: 3300,
    })),
  },
  '/api/gd-metric/supply-curve': {
    market: 'ahead',
    from: '2026-06-01',
    to: '2026-08-31',
    bins: 2,
    quantiles: [0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1],
    points: [{ load: 90000, price: 300 }, { load: 150000, price: 480 }],
    sampled: false,
    allBins: [
      { loadFrom: 80000, loadTo: 120000, loadMid: 100000, count: 500, q: quantileGrid },
      { loadFrom: 120000, loadTo: 160000, loadMid: 140000, count: 500, q: quantileGrid.map((v) => v + 60) },
    ],
    steps: [
      { loadFrom: 80000, loadTo: 120000, price: 360, count: 1000 },
      { loadFrom: 120000, loadTo: 160000, price: 420, count: 1000 },
    ],
    stats: { pairs: 2000, days: 30, loadMin: 80000, loadMax: 160000, priceMin: 200, priceMax: 580, spearman: 0.8, violations: 0, maxPavaMove: 0, latestLoad: 140000 },
  },
  '/api/forecast/price': {
    forecast: {
      market: 'ahead',
      grain: 'day',
      stepMinutes: 1440,
      dataset: 'day_ahead_trade',
      scope: '发电侧',
      metric: '平均电价',
      unit: '元/兆瓦时',
      model: 'timesfm-3.0',
      from: '2026-09-01',
      to: '2026-09-05',
      historyDates: DAYS,
      historyValues: [380, 390, 387],
      contextPoints: 288,
      inputTruncated: false,
      missingDays: 0,
      timeMs: 1200,
      generatedOn: '2026-08-31',
      forecast: Array.from({ length: 96 }, (_, i) => ({
        runDate: `2026-09-0${1 + Math.floor(i / 32)}T${String(Math.floor((i % 32) * 15 / 60)).padStart(2, '0')}:00`,
        targetTime: `2026-09-01 ${String(Math.floor((i * 15) / 60)).padStart(2, '0')}:${String((i * 15) % 60).padStart(2, '0')}`,
        point: 300 + ((i * 13) % 200),
        quantiles: { '0.1': 260, '0.5': 300, '0.9': 420 },
      })),
    },
  },
  // 分时（grain=intraday）：序列源是电网日指标 CSV，5 个预测日 × 96 点 = 480 点
  // （96×5=480 ≤ TimesFM max_horizon 512）。stub 里按 query 选这一份。
  '/api/forecast/price?grain=intraday': {
    forecast: {
      market: 'ahead',
      grain: 'intraday',
      stepMinutes: 15,
      dataset: 'day_ahead_trade',
      scope: '发电侧',
      metric: '平均电价',
      unit: '元/兆瓦时',
      model: 'timesfm-3.0',
      from: '2026-08-02 00:15',
      to: '2026-09-01 00:00',
      historyDates: ['2026-09-01 00:00'],
      historyValues: [380],
      contextPoints: 2880,
      inputTruncated: false,
      missingDays: 0,
      timeMs: 1800,
      forecast: Array.from({ length: 480 }, (_, i) => {
        const slot = (i % 96) * 15
        const hh = String(Math.floor(slot / 60)).padStart(2, '0')
        const mm = String(slot % 60).padStart(2, '0')
        const day = `2026-09-0${1 + Math.floor(i / 96)}`
        const point = 300 + ((i * 13) % 200)
        return {
          runDate: day,
          atTime: `${hh}:${mm}`,
          targetTime: `${day} ${hh}:${mm}`,
          point,
          quantiles: { '0.1': point - 40, '0.5': point, '0.9': point + 50 },
        }
      }),
    },
  },
  '/api/forecast/backtest': {
    market: 'ahead', grain: 'intraday', unit: '元/兆瓦时', model: 'timesfm-3.0',
    from: '2026-08-24', to: '2026-08-31', sampleDays: 3,
    targetDates: ['2026-08-31'], skipped: null, timeMs: 1200,
    points: Array.from({ length: 96 }, (_, i) => ({
      targetTime: `2026-08-31 ${String(Math.floor((i * 15) / 60)).padStart(2, '0')}:${String((i * 15) % 60).padStart(2, '0')}`,
      predicted: 300 + ((i * 7) % 150), actual: 310 + ((i * 11) % 140), q10: 260, q50: 300, q90: 420,
    })),
  },
  // 复盘页默认腿 = 当时预测（日频留痕）：真值由后端按官方日均价配好（actual），
  // 一天一条才能和官方日均价 1:1 配对 —— 也给「实际 vs 预测」这条图提供可画的点。
  '/api/forecast/recorded': {
    generatedDates: ['2026-08-29', '2026-08-30'],
    dataAsOf: '2026-08-31',
    lagDays: 2,
    stale: false,
    truthSource: 'gd_spot_metric（官方直采，发电侧加权平均电价）',
    backfilled: 0,
    actualMissing: 0,
    points: [
      { generatedOn: '2026-08-29', targetTime: '2026-08-30', point: 392, q10: 370, q50: 392, q90: 415, model: 'timesfm-3.0', unit: '元/兆瓦时', source: 'day_ahead_trade', actual: 380 },
      { generatedOn: '2026-08-30', targetTime: '2026-08-31', point: 402, q10: 380, q50: 402, q90: 425, model: 'timesfm-3.0', unit: '元/兆瓦时', source: 'day_ahead_trade', actual: 400.5 },
    ],
  },
  // 官方指标看板：复盘页用它取「实际值最新日」（按市场），决定区间锚点与陈旧档位
  '/api/spot-market/board': {
    date: '2026-08-31',
    from: '2026-06-03',
    to: '2026-08-31',
    days: 90,
    daily: [],
    periodic: [],
    aheadRealtimeSpread: [],
    spreadPeak: 0,
    spreadValley: 0,
    spreadMean: 0,
    coverage: [
      { dataset: 'day_ahead_trade', from: '2026-06-03', to: '2026-08-31', periods: 90, missing: [], lastDate: '2026-08-31' },
      { dataset: 'realtime_trade', from: '2026-06-03', to: '2026-08-29', periods: 88, missing: [], lastDate: '2026-08-29' },
    ],
    source: 'pm.gd.csg.cn',
    updatedAt: '2026-08-31T10:00:00+08:00',
  },
  // 注意外壳：真实响应是 { forecast: {...} }（api/forecast.ts 会拆包），
  // 桩少一层外壳 → 页面读出 undefined.timeMs → 组件 setup 抛异常、整页空白。
  '/api/forecast/load': {
    forecast: {
      deviceId: 'dev_ess_1', model: 'timesfm-3.0', stepMinutes: 15,
    lookbackPoints: 96, inputTruncated: false,
    historyTimes: curve96('load').map((_, i) => 1756000000000 + i * 900000),
    historyValues: curve96('load').map((p) => p.value),
    forecastTimes: curve96('load').map((_, i) => 1757000000000 + i * 900000),
    forecastPoint: curve96('load').map((p) => p.value * 1.02),
      forecastQuantiles: { '0.5': curve96('load').map((p) => p.value) },
      timeMs: 900,
    },
  },
  // 刻意只给一台**顶层储能柜（没有子设备）**：这是现场常见装法，也正是曾经漏掉的那种 ——
  // 储能柜页的趋势循环当时只遍历子设备，于是顶层柜子一条曲线都画不出来，
  // 页面还把「没遍历到」说成「没有开启存储的数值点位」。夹具给一台带子设备的柜子会把这个问题掩盖掉。
  '/api/device/detailed-list': {
    total: 2,
    devices: [
      { id: 'dev_ess_1', pid: '0', name: '储能柜 1', deviceType: 'ess', enabled: true, driver: 'ess_demo_v1.0.0', protocolId: 'p1', params: {}, storageEnable: true, storageIntervalSec: 60, sort: 0, isVirtualDevice: false },
      { id: 'dev_pcs_9', pid: 'dev_other', name: 'PCS 9', deviceType: 'pcs', enabled: true, driver: 'pcs_demo', protocolId: 'p1', params: {}, storageEnable: true, storageIntervalSec: 60, sort: 1, isVirtualDevice: false },
    ],
  },
  '/api/device/telemetry': {
    telemetry: {
      dev_ess_1: { lastUpdateTime: '2026-08-31T10:00:00+08:00', telemetryValues: { SOC: 62, Power: -40, Status: 4, Temperature: 28.4 } },
      dev_pcs_1: { lastUpdateTime: '2026-08-31T10:00:00+08:00', telemetryValues: { Power: -40, Efficiency: 97.2 } },
    },
  },
  '/api/device/history': {
    xAxis: { type: 'category', data: curve96('load').map((p) => p.time) },
    series: [
      { name: 'Power', type: 'line', unit: 'kW', data: curve96('dayAhead').map((p) => String(p.value / 100)) },
      { name: 'SOC', type: 'line', unit: '%', data: curve96('load').map((_, i) => String(50 + (i % 20))) },
    ],
  },
  '/api/alarm/current': { total: 1, ignoreTotal: 0, historyTotal: 1, items: [{ sourceDeviceId: 'dev_ess_1', sourceDeviceName: '储能柜 1', deviceId: 'dev_ess_1', deviceName: '储能柜 1', point: 'Power', pointName: '功率', level: 'Warn', detail: '功率越限（示例）', createdAt: '2026-08-31 09:00:00' }] },
  '/api/alarm/history': { total: 1, items: [{ id: 1, deviceId: 'dev_ess_1', deviceName: '储能柜 1', point: 'Power', pointName: '功率', level: 'Warn', detail: '示例', triggerAt: '2026-08-31 08:00:00', clearAt: '2026-08-31 08:10:00' }] },
  '/api/alarm/ignore': { total: 0, items: [] },
  '/api/effect/summary': {
    summary: { start: '2026-08-25', end: '2026-08-31', points: 672, algorithm: 'rule_based', gridDeviceId: 'dev_meter', essDeviceIds: ['dev_ess_1'], demandPrice: 40, chargeEnergy: 1200, dischargeEnergy: 1100, chargeCost: 380, dischargeRevenue: 620, arbitrageProfit: 240, actualMaxDemand: 180, baselineMaxDemand: 210, demandSaving: 1200, totalSaving: 1440 },
  },
  '/api/effect/backtest': {
    backtest: { start: '2026-08-25', end: '2026-08-31', points: 672, gridDeviceId: 'dev_meter', essDeviceIds: ['dev_ess_1'], demandTarget: 200, demandPrice: 40, ratedPowerKw: 100, capacityKwh: 200, results: [{ algorithm: 'rule_based', chargeEnergy: 1200, dischargeEnergy: 1100, chargeCost: 380, dischargeRevenue: 620, arbitrageProfit: 240, actualMaxDemand: 180, baselineMaxDemand: 210, demandSaving: 1200, totalSaving: 1440, finalSocPct: 61 }] },
  },
  '/api/auth/current': { role: 'admin' },
  // 形状要与真实响应一致（dataset/scope/metric/avg）：
  // 四条线面板按 dataset=day_ahead_trade|realtime_trade + 发电侧口径取数，metric 用库里真实的
  // 官方写法「发电侧日前/实时加权平均电价」（曾按 '平均电价' 全等匹配 → 面板永远空态）。
  '/api/spot-report/trend': {
    from: '2026-08-25',
    to: '2026-08-31',
    grain: 'day',
    excludedPending: 0,
    points: DAYS.flatMap((d) => [
      { bucket: d, from: d, to: d, dataset: 'day_ahead_trade', scope: '发电侧', metric: '发电侧日前加权平均电价', avg: 380, max: 460, min: 300, sum: 0, days: 1, unit: '元/MWh', value: 0 },
      { bucket: d, from: d, to: d, dataset: 'day_ahead_trade', scope: '燃煤', metric: '平均电价', avg: 392, max: 470, min: 320, sum: 0, days: 1, unit: '元/MWh', value: 0 },
      { bucket: d, from: d, to: d, dataset: 'realtime_trade', scope: '发电侧', metric: '发电侧实时加权平均电价', avg: 365, max: 450, min: 280, sum: 0, days: 1, unit: '元/MWh', value: 0 },
    ]),
  },
}

/** 点位定义：按设备 id 给一份（名字/单位/枚举色都从这里来） */
function pointDefinitions(deviceId) {
  return {
    isVirtualDevice: false,
    groups: [{ key: 'status', name: '状态量' }],
    fields: [
      { key: 'Power', name: '功率', groupKey: 'status', valueType: 'float', componentType: 'number', required: false, unit: 'kW' },
      { key: 'SOC', name: 'SOC', groupKey: 'status', valueType: 'float', componentType: 'number', required: false, unit: '%' },
      {
        key: 'Status', name: '运行状态', groupKey: 'status', valueType: 'int', componentType: 'number', required: false,
        valueExplain: [{ key: '4', value: '放电中', color: '#0098FA', fromParam: false }],
      },
      { key: 'Temperature', name: '温度', groupKey: 'status', valueType: 'float', componentType: 'number', required: false, unit: '°C' },
      { key: 'Efficiency', name: '效率', groupKey: 'status', valueType: 'float', componentType: 'number', required: false, unit: '%' },
      { key: `dev_${deviceId}`, name: '', groupKey: '', valueType: 'float', componentType: 'number', required: false },
    ],
  }
}

/* ------------------------------------------------------------------ *
 * 2) 本地站点：dist + 注入 API 桩（SPA 兜底）
 * ------------------------------------------------------------------ */

const STUB_SCRIPT = `<script>
// 在最前面装错误钩子：组件 setup 抛异常时页面会**整片空白且不报错**（预测复盘线上就是这样），
// 只有把错误抓下来，失败原因才是可读的。
window.__errors = [];
window.addEventListener('error', function (e) {
  window.__errors.push('onerror: ' + ((e.error && e.error.message) || e.message));
});
window.addEventListener('unhandledrejection', function (e) {
  window.__errors.push('reject: ' + ((e.reason && e.reason.message) || String(e.reason)));
});
(function () {
  var orig = console.error.bind(console);
  console.error = function () {
    var args = Array.prototype.slice.call(arguments);
    var err = args.find(function (x) { return x instanceof Error; });
    if (err) window.__errors.push('console: ' + err.message);
    orig.apply(null, args);
  };
})();
localStorage.setItem('ems:user:token','browser-check');
localStorage.setItem('ems:user:role','admin');
const __FIXTURES__ = ${JSON.stringify(FIXTURES)};
window.fetch = async (input) => {
  const url = new URL(typeof input === 'string' ? input : input.url, location.origin);
  const p = url.pathname;
  let data = __FIXTURES__[p];
  if (p === '/api/forecast/price' && url.searchParams.get('grain') === 'intraday') {
    data = __FIXTURES__['/api/forecast/price?grain=intraday'];
  }
  if (data === undefined && /\\/points\\/definitions$/.test(p)) data = ${JSON.stringify(pointDefinitions('x'))};
  if (data === undefined) data = {};
  return new Response(JSON.stringify({ code: 0, message: 'OK', data }), {
    status: 200, headers: { 'Content-Type': 'application/json' },
  });
};
</script>`

const MIME = { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html', '.svg': 'image/svg+xml', '.woff2': 'font/woff2', '.png': 'image/png' }

function startServer() {
  const server = createServer((req, res) => {
    const path = decodeURIComponent(req.url.split('?')[0])
    const file = join(distDir, path.replace(/^\//, ''))
    if (path !== '/' && existsSync(file) && !file.endsWith('index.html')) {
      const ext = file.slice(file.lastIndexOf('.'))
      res.writeHead(200, { 'Content-Type': MIME[ext] ?? 'application/octet-stream' })
      res.end(readFileSync(file))
      return
    }
    // SPA 兜底 + 注入桩：前端路由（/scene/disclosure 等）都走这里
    const html = readFileSync(join(distDir, 'index.html'), 'utf8').replace('<head>', `<head>${STUB_SCRIPT}`)
    res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
    res.end(html)
  })
  return new Promise((ok) => server.listen(0, '127.0.0.1', () => ok({ server, port: server.address().port })))
}

/* ------------------------------------------------------------------ *
 * 3) Marionette 客户端（长度前缀 JSON over TCP）
 * ------------------------------------------------------------------ */

class Marionette {
  constructor(socket) {
    this.socket = socket
    this.buf = Buffer.alloc(0)
    this.msgId = 0
    socket.on('data', (d) => {
      this.buf = Buffer.concat([this.buf, d])
      this.drain()
    })
  }

  static async connect(port, timeoutMs = 45000) {
    const deadline = Date.now() + timeoutMs
    while (Date.now() < deadline) {
      try {
        const socket = await new Promise((ok, no) => {
          const s = net.createConnection({ host: '127.0.0.1', port })
          s.once('connect', () => ok(s))
          s.once('error', no)
        })
        return new Marionette(socket)
      } catch {
        await new Promise((r) => setTimeout(r, 500))
      }
    }
    throw new Error(`Marionette 未就绪（端口 ${port}）`)
  }

  drain() {
    for (;;) {
      const colon = this.buf.indexOf(0x3a) // ':'
      if (colon < 0) return
      const len = Number(this.buf.subarray(0, colon).toString())
      if (!Number.isFinite(len)) {
        this.buf = this.buf.subarray(colon + 1)
        continue
      }
      if (this.buf.length < colon + 1 + len) return
      const payload = this.buf.subarray(colon + 1, colon + 1 + len)
      this.buf = this.buf.subarray(colon + 1 + len)
      const msg = JSON.parse(payload.toString())
      if (Array.isArray(msg) && msg[1] === this.msgId) {
        this.resolve(msg[2], msg[3])
      }
    }
  }

  call(command, params = {}) {
    this.msgId += 1
    const body = Buffer.from(JSON.stringify([0, this.msgId, command, params]))
    const id = this.msgId
    return new Promise((ok, no) => {
      this.resolve = (err, res) => (err ? no(new Error(`${command} 失败: ${JSON.stringify(err)}`)) : ok(res))
      // handshake 里第一条消息没有 msgId，这里忽略长度前缀的读取由 drain 负责
      this.socket.write(Buffer.concat([Buffer.from(`${body.length}:`), body]))
      setTimeout(() => no(new Error(`${command} 超时`)), 30000)
      void id
    })
  }
}

/* ------------------------------------------------------------------ *
 * 4) 要检查的页面：每个都必须画到 canvas
 * ------------------------------------------------------------------ */

const PAGES = [
  { name: '场景 · 信息披露', path: '/scene/disclosure', min: 2 },
  // 决策支持现在有三张图：电价预测、负荷预测、相似日情况分析（目标日 + 叠加日）
  { name: '场景 · 决策支持', path: '/scene/decision', min: 3 },
  // 三张图：日前成交电价、实时成交电价、四条线面板（官方口径能画出实际两条线才会是 3）
  { name: '场景 · 历史信息', path: '/scene/history', min: 3 },
  { name: '场景 · 长周期电价预测', path: '/scene/forecast', min: 1 },
  {
    // 「分时」视图是 480 个点（96×5）的连续曲线 + 分位带：happy-dom 没有 canvas，
    // 这张图只有真浏览器能验证它真的画出来了（切下拉 → 等图 → 数 canvas）。
    name: '场景 · 长周期电价预测（分时）',
    path: '/scene/forecast',
    min: 1,
    act: `(() => {
      const sel = [...document.querySelectorAll('.toolbar .field select')][1];
      if (!sel) return false;
      const opt = [...sel.options].find((o) => o.textContent.trim() === '分时');
      if (!opt) return false;
      sel.selectedIndex = opt.index;
      sel.dispatchEvent(new Event('change', { bubbles: true }));
      return true;
    })()`,
  },
  { name: '场景 · 策略反演', path: '/scene/inference', min: 1 },
  { name: '场景 · 预测复盘', path: '/scene/review', min: 1 },
  { name: '运行监控 · 能源总览', path: '/monitor/overview', min: 1 },
  { name: '运行监控 · 储能柜', path: '/monitor/cabinet', min: 1 },
]

/* ------------------------------------------------------------------ *
 * 5) 主流程
 * ------------------------------------------------------------------ */

function skip(reason) {
  if (process.env.BROWSER_CHECK_REQUIRED === '1') {
    console.error(`✗ ${reason}（BROWSER_CHECK_REQUIRED=1，不允许跳过）`)
    process.exit(1)
  }
  console.log(`⚠ 跳过真浏览器检查：${reason}`)
  process.exit(0)
}

function findFirefox() {
  for (const bin of ['firefox', 'firefox-esr']) {
    const found = process.env.PATH.split(':').map((d) => join(d, bin)).find((p) => existsSync(p))
    if (found) return found
  }
  return null
}

async function main() {
  if (!existsSync(join(distDir, 'index.html'))) {
    console.error('✗ dist 不存在，先跑 npm run build（或 npm run verify）')
    process.exit(1)
  }
  const firefox = findFirefox()
  if (!firefox) skip('找不到 firefox')

  const port = 2900 + Math.floor(Math.random() * 90)
  const profile = mkdtempSync(join(tmpdir(), 'ems-browser-'))
  writeFileSync(join(profile, 'user.js'), `user_pref("marionette.enabled", true);\nuser_pref("marionette.port", ${port});\n`)

  const { server, port: httpPort } = await startServer()
  const base = `http://127.0.0.1:${httpPort}`
  console.log(`本地站点: ${base}（Marionette 端口 ${port}）`)

  const ff = spawn(firefox, ['--headless', '--new-instance', '--profile', profile, 'about:blank'], {
    env: { ...process.env, MOZ_MARIONETTE: '1', MOZ_HEADLESS: '1' },
    stdio: 'ignore',
  })

  let failed = 0
  try {
    const m = await Marionette.connect(port)
    await m.call('WebDriver:NewSession', { capabilities: {} })

    for (const page of PAGES) {
      await m.call('WebDriver:Navigate', { url: base + '/' })
      await new Promise((r) => setTimeout(r, 300))
      await m.call('WebDriver:Navigate', { url: base + page.path })

      // 有的页面要先把界面切到目标状态（下拉/开关），否则那块图根本不在 DOM 里
      if (page.act) {
        await m.call('WebDriver:ExecuteScript', { script: page.act, args: [] })
      }

      // 轮询等图出现，不用固定 sleep：有的页面要等接口回来才渲染（固定 2.5s 会偶发 0 图，
      // 那是检查自己不稳，不是页面有问题）。最多等 8 秒。
      const deadline = Date.now() + 8000
      for (;;) {
        const n = await m.call('WebDriver:ExecuteScript', {
          script: "return document.querySelectorAll('.chart').length;",
          args: [],
        })
        const count = typeof n === 'number' ? n : Number(n?.value ?? 0)
        if (count >= page.min || Date.now() > deadline) break
        await new Promise((r) => setTimeout(r, 300))
      }

      const raw = await m.call('WebDriver:ExecuteScript', {
        script: `return JSON.stringify((() => {
          const charts = [...document.querySelectorAll('.chart')];
          const txt = (sel) => [...document.querySelectorAll(sel)].map(e => e.textContent.replace(/\s+/g,' ').trim()).filter(Boolean);
          return {
            h1: document.querySelector('.pagehead h1')?.textContent ?? null,
            totalCanvas: document.querySelectorAll('canvas').length,
            empty: txt('.empty').slice(0, 2),
            errors: txt('.alert.error, .notice.error').slice(0, 2),
            loading: txt('.panel-state').slice(0, 2),
            jsErrors: (window.__errors || []).slice(0, 3),
            charts: charts.map((c) => ({
              points: Number(c.dataset.chartPoints ?? 0),
              error: c.dataset.chartError ?? null,
              canvases: c.querySelectorAll('canvas').length,
              size: [...c.querySelectorAll('canvas')].map((cv) => cv.width * cv.height),
            })),
          };
        })());`,
        args: [],
      })
      const info = JSON.parse(typeof raw === 'string' ? raw : raw.value)
      const problems = []
      if (info.charts.length < page.min) problems.push(`图表容器 ${info.charts.length} 个（至少 ${page.min}）`)
      for (const [i, c] of info.charts.entries()) {
        if (c.error) problems.push(`第 ${i + 1} 个图报错: ${c.error}`)
        if (c.canvases === 0) problems.push(`第 ${i + 1} 个图没有 canvas（容器空；数据点 ${c.points}）`)
        else if (c.size.some((s) => !s)) problems.push(`第 ${i + 1} 个图 canvas 尺寸为 0`)
      }
      if (problems.length) {
        failed++
        const why = [...info.jsErrors, ...info.errors, ...info.empty, ...info.loading].slice(0, 3)
        console.error(
          `  FAIL ${page.name} — ${problems.join('；')}  [h1=${info.h1} canvas=${info.totalCanvas}]` +
            (why.length ? `\n         ← ${why.join('\n         ← ')}` : '  （页面没有任何空态/错误文字，也没有 JS 异常）'),
        )
      } else {
        console.log(`  ok   ${page.name}（${info.charts.length} 个图，canvas 共 ${info.totalCanvas}）`)
      }
    }
  } finally {
    ff.kill('SIGKILL')
    server.close()
    rmSync(profile, { recursive: true, force: true })
  }

  if (failed) {
    console.error(`\n${failed} 个页面没画出图`)
    process.exit(1)
  }
  console.log('\n✅ 真浏览器检查通过：所有该有图的页面都画出了 canvas')
}

await main()
