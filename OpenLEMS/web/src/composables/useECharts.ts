import { onBeforeUnmount, onMounted, onUpdated, watch, type Ref, type WatchSource } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsOption } from 'echarts'

// 只注册用到的模块：ECharts 全量约 1MB，按需注册是唯一把体积压下来的办法
echarts.use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

/**
 * 把 ECharts 挂到一个 div 上：init / 跟随容器尺寸 / 卸载时 dispose。
 *
 * `deps` 是数据依赖（推荐传，省掉无谓重绘）。**不传也不会画空**：
 * 没给 deps 时退化成「组件每次更新就重绘」，因为图表容器常常在数据到达之前就挂载了
 * （面板一直渲染，只是里面还没数）。这个兜底是补一个真实事故：信息披露页两个图都没传 deps，
 * 于是只画了首帧的空数据 —— 接口有数据、KPI 有数，只有图是白的，页面上没有任何提示。
 *
 * 每次重绘会在容器上写 `data-chart-points`＝本次画了多少个数据点。
 * 图形是 canvas，画没画出来在 DOM 上看不见；这个属性让「图空白」变成可断言、可排查的信号。
 */
export function useECharts(
  el: Ref<HTMLElement | undefined>,
  build: () => EChartsOption,
  deps: WatchSource[] = [],
) {
  let chart: echarts.ECharts | null = null
  let observer: ResizeObserver | null = null

  /** 数一数这次画了多少个数据点（各 series 的 data 长度之和） */
  function countPoints(option: EChartsOption): number {
    const raw = (option as { series?: unknown }).series
    const list = Array.isArray(raw) ? raw : raw ? [raw] : []
    let n = 0
    for (const s of list) {
      const data = (s as { data?: unknown })?.data
      if (Array.isArray(data)) n += data.length
    }
    return n
  }

  function render() {
    if (!el.value) return
    const option = build()
    // **先记点数再碰 ECharts**：这个属性描述的是「页面把多少数据交给了图」，
    // 与渲染器是否可用无关。冒烟的 happy-dom 没有 canvas，init 会直接失败 ——
    // 若把记录放在后面，属性永远不会被写上，也就无从区分「没数据」和「画不出来」。
    el.value.dataset.chartPoints = String(countPoints(option))
    try {
      // 容器可能被**换成另一个 DOM 节点**：面板在 loading 时销毁 slot 内容、加载完再新建一个 div。
      // 这时实例还挂在那个已脱离文档的旧节点上，继续 setOption 只会往看不见的 canvas 上画 ——
      // 现象是「容器空、整页一个 canvas 都没有」，而且**一点报错都没有**。
      // （只有真浏览器能发现：冒烟用的 happy-dom 里 init 本来就失败。
      //   部署上信息披露页的曲线图就是这么空的：数据点写到容器上了，画却画在别处。）
      if (chart && chart.getDom() !== el.value) {
        chart.dispose()
        chart = null
      }
      if (!chart) chart = echarts.init(el.value)
      chart.setOption(option, true)
      delete el.value.dataset.chartError
    } catch (err) {
      // 渲染器起不来时不要把整个页面带崩：记在容器上并说清原因，页面其余部分照常可用
      el.value.dataset.chartError = err instanceof Error ? err.message : String(err)
      console.error('[useECharts] 图表渲染失败', err)
    }
  }

  function resize() {
    chart?.resize()
  }

  onMounted(() => {
    render()
    if (el.value && typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(resize)
      observer.observe(el.value)
    } else {
      window.addEventListener('resize', resize)
    }
  })

  onBeforeUnmount(() => {
    observer?.disconnect()
    window.removeEventListener('resize', resize)
    chart?.dispose()
    chart = null
  })

  // 容器本身可能是**后出现的**：图挂在 PanelBox 的 slot 里，加载中时那一块不渲染，
  // 数据到了才创建出来。而 Vue 的 watch 默认在 DOM 更新**之前**跑 —— 于是首帧扑空
  // （el 还是 undefined，render 直接 return），此后没人再叫它，图永远空白。
  // 两个补救：post（DOM 更新后再跑）＋单独盯住元素本身（容器出现时补画一次）。
  if (deps.length) {
    watch(deps, render, { deep: true, flush: 'post' })
  } else {
    // 没给 deps：兜底成「组件更新就重绘」，宁可多画一次，也不要留一张永远空白的图
    onUpdated(render)
  }
  watch(el, render, { flush: 'post' })

  return { render, resize }
}

export const CHART_COLORS = {
  brand: '#ff7d00',
  blue: '#165dff',
  cyan: '#0fc6c2',
  gray: '#86909c',
  dark: '#4e5969',
  purple: '#722ed1',
  green: '#00b42a',
  red: '#f53f3f',
  gold: '#f7ba1e',
}

export { echarts, type EChartsOption }
