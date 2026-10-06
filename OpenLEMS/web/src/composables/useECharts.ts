import { onBeforeUnmount, onMounted, watch, type Ref } from 'vue'
import * as echarts from 'echarts/core'
import { LineChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TooltipComponent,
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import type { EChartsOption } from 'echarts'

// 只注册用到的模块：ECharts 全量约 1MB，按需注册是唯一把体积压下来的办法
echarts.use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer])

/**
 * 把 ECharts 挂到一个 div 上：自动 init / 跟随容器尺寸 / 卸载时 dispose。
 * 只在真正需要交互图（tooltip、缩放、图例开关）时用它；静态小图不必上 ECharts。
 */
export function useECharts(el: Ref<HTMLElement | undefined>, build: () => EChartsOption) {
  let chart: echarts.ECharts | null = null
  let observer: ResizeObserver | null = null

  function render() {
    if (!el.value) return
    if (!chart) chart = echarts.init(el.value)
    chart.setOption(build(), true)
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

  watch(build, render, { deep: true })

  return { resize }
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
