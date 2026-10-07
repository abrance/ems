<script setup lang="ts">
import { computed, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'
import type { SpotTrendPoint } from '@/api/spotreport'

/**
 * 一个数据集一张图：x 轴是桶（运行日/周/月），每条序列一条 avg 折线。
 * tooltip 里给出 avg/max/min/参与天数 —— 单看 avg 会掩盖桶内分布。
 */
const props = defineProps<{ points: SpotTrendPoint[] }>()

const el = ref<HTMLElement>()

const PALETTE = [
  CHART_COLORS.brand,
  CHART_COLORS.blue,
  CHART_COLORS.cyan,
  CHART_COLORS.purple,
  CHART_COLORS.red,
  CHART_COLORS.green,
  CHART_COLORS.gold,
  CHART_COLORS.gray,
  CHART_COLORS.dark,
]

const buckets = computed(() => [...new Set(props.points.map((p) => p.bucket))].sort())

const seriesList = computed(() => {
  const map = new Map<string, { name: string; byBucket: Map<string, SpotTrendPoint> }>()
  for (const p of props.points) {
    // 档位类数据集的「城市」是样本不是维度，序列名用区间文本
    const name = p.textValue || p.scope
    const key = `${name}|${p.metric}`
    if (!map.has(key)) map.set(key, { name: `${name}·${p.metric}`, byBucket: new Map() })
    map.get(key)!.byBucket.set(p.bucket, p)
  }
  return [...map.values()]
})

useECharts(el, (): EChartsOption => {
  return {
    grid: { left: 52, right: 20, top: 24, bottom: 44 },
    tooltip: {
      trigger: 'axis',
      formatter: (items: unknown) => {
        const arr = items as { axisValue: string; seriesName: string; color: string; dataIndex: number }[]
        if (!arr?.length) return ''
        const bucket = arr[0].axisValue
        const lines = arr.map((it) => {
          const hit = seriesList.value.find((s) => s.name === it.seriesName)?.byBucket.get(bucket)
          if (!hit) return `${it.seriesName}：—`
          const extra: string[] = []
          if (hit.days > 1) extra.push(`max ${hit.max}`, `min ${hit.min}`, `${hit.days} 天`)
          if (hit.sharePct != null) extra.push(`占比 ${hit.sharePct.toFixed(1)}%`)
          if (hit.count != null && !hit.sharePct) extra.push(`${hit.count} 城·天`)
          return `${it.seriesName}：<b>${hit.avg}</b>${hit.unit || ''}${extra.length ? `（${extra.join(' · ')}）` : ''}`
        })
        return `${bucket}<br/>${lines.join('<br/>')}`
      },
    },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: {
      type: 'category',
      data: buckets.value,
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: buckets.value.length > 8 ? 30 : 0 },
    },
    yAxis: {
      type: 'value',
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: '#eef0f3' } },
    },
    series: seriesList.value.map((s, i) => ({
      name: s.name,
      type: 'line' as const,
      smooth: true,
      showSymbol: buckets.value.length <= 40,
      connectNulls: false,
      itemStyle: { color: PALETTE[i % PALETTE.length] },
      data: buckets.value.map((b) => s.byBucket.get(b)?.avg ?? null),
    })),
  }
}, [() => props.points])
</script>

<template>
  <div ref="el" class="chart" />
</template>

<style scoped>
.chart { width: 100%; height: 280px; }
</style>
