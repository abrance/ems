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

// vue-router 在 happy-dom 下会用一个非 Event 对象调 window.dispatchEvent，
// 属于测试环境互操作瑕疵（线上无此问题），单独归集，不算失败。
const interopNoise = []
process.on('unhandledRejection', (err) => {
  if (String(err?.message || err).includes('dispatchEvent')) {
    interopNoise.push(String(err.message))
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
const window = new Window({ url: 'http://localhost/' })
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
globalThis.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} }

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

globalThis.fetch = async (url) => {
  const body = String(url).includes('/api/gd-metric')
    ? {
        code: 0,
        data: {
          latestDate: '2026-09-21', from: '2026-08-23', to: '2026-09-21', date: '2026-09-21',
          source: 'gd-metric', updatedAt: '2026-09-21 20:00', availableDays: ['2026-09-21'],
          latest: {
            date: '2026-09-21',
            load: { avg: 3000, max: 3460, min: 2200, last: 3000, count: 96, unit: 'MW' },
            dayAhead: { avg: 380, max: 760, min: 60, last: 380, count: 96, unit: '元/MWh' },
            realtime: { avg: 365, max: 780, min: 40, last: 365, count: 96, unit: '元/MWh' },
          },
          days: [], curve: { date: '2026-09-21', load: [], dayAhead: [], realtime: [] },
        },
      }
    : { code: 0, data: {} }
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
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

console.log('\n== 逐页点击 ==')
let visited = 0
for (const tab of NAV) {
  const tabEl = [...doc.querySelectorAll('.tabs .tab')].find((e) => e.textContent.trim() === tab.title)
  if (!tabEl) {
    check(false, `找到 Tab「${tab.title}」`)
    continue
  }
  tabEl.click()
  await tick()

  const sideItems = [...doc.querySelectorAll('.side-item')]
  check(sideItems.length === tab.pages.length, `${tab.title} 侧栏项数 = ${sideItems.length}（期望 ${tab.pages.length}）`)

  for (const page of tab.pages) {
    const item = [...doc.querySelectorAll('.side-item')].find((e) => e.textContent.trim().startsWith(page.title))
    if (!item) {
      check(false, `${tab.key}/${page.key} 侧栏项存在`)
      continue
    }
    try {
      item.click()
      await tick()
      const h1 = doc.querySelector('.pagehead h1')?.textContent?.trim()
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

if (interopNoise.length) {
  console.log(`\n（忽略 ${interopNoise.length} 条 happy-dom 与 vue-router 的 dispatchEvent 互操作噪音）`)
}
await vite.close()
console.log(failed ? `\n${failed} 项失败` : '\n✅ 壳层冒烟全部通过')
process.exit(failed ? 1 : 0)
