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

  // 越界值应被驱动声明的 max 拦住，不弹确认框
  const bad = doc.querySelector('.field-item input[id$="-power"]')
  if (bad) {
    bad.value = '999'
    bad.dispatchEvent(new window.Event('input', { bubbles: true }))
    await tick()
    const execBtn = [...doc.querySelectorAll('.panel-head button')].find((b) => b.textContent.trim() === '执行')
    execBtn?.click()
    await tick()
    check(!doc.querySelector('.modal'), '越界值被拦住，未弹确认框')
    check(bodyText().includes('不能大于 50'), '提示引用了驱动声明的上限')
  }

  const power = doc.querySelector('.field-item input[id$="-power"]')
  const duration = doc.querySelector('.field-item input[id$="-duration"]')
  if (power) {
    power.value = '30'
    power.dispatchEvent(new window.Event('input', { bubbles: true }))
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
    call?.body?.parameters?.[0] === 30 && call?.body?.parameters?.[1] === 15,
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
