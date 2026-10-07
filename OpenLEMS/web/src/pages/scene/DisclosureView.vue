<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchGdMetric, type GdMetricSnapshot } from '@/api/gdmetric'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'
import { useAppStore } from '@/stores/app'

/**
 * 信息披露 —— 目前唯一接了真实数据的页面。
 *
 * 已接：GET /api/gd-metric（电网负荷 / 日前电价 / 实时电价，15 分钟粒度）
 * 未接（面板上明确标注，不造数）：
 *   新能源出力、供需比、火电启停 —— 日报未提取 / 口径未定，见设计稿 §10.1。
 */
const app = useAppStore()

const snapshot = ref<GdMetricSnapshot | null>(null)
const loading = ref(false)
const error = ref('')
let controller: AbortController | null = null

async function load() {
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  try {
    snapshot.value = await fetchGdMetric({
      date: app.runDate || undefined,
      signal: controller.signal,
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') return
    error.value = err instanceof Error ? err.message : '加载失败'
    snapshot.value = null
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(() => app.runDate, load)

const hasData = computed(() => snapshot.value?.curve != null)

const latest = computed(() => snapshot.value?.latest ?? null)

function num(v: number | undefined, digits = 1): string {
  return v === undefined || Number.isNaN(v) ? '—' : v.toFixed(digits)
}

/* ---------- 图 1：当日 15 分钟曲线 ---------- */
const curveEl = ref<HTMLElement>()
useECharts(curveEl, (): EChartsOption => {
  const curve = snapshot.value?.curve
  const times = curve?.load.map((s) => s.time) ?? []
  return {
    grid: { left: 48, right: 16, top: 32, bottom: 28 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: { type: 'category', data: times, axisLabel: { fontSize: 12, color: CHART_COLORS.gray } },
    // 双 Y 轴：左轴负荷(MW)、右轴电价(元/MWh)，量级差一个数量级，不能共用一轴
    yAxis: [
      { type: 'value', name: 'MW', nameTextStyle: { fontSize: 12 }, axisLabel: { fontSize: 12, color: CHART_COLORS.gray }, splitLine: { lineStyle: { color: '#eef0f3' } } },
      { type: 'value', name: '元/MWh', nameTextStyle: { fontSize: 12 }, axisLabel: { fontSize: 12, color: CHART_COLORS.gray }, splitLine: { show: false } },
    ],
    series: [
      { name: '负荷 (MW)', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 2 }, itemStyle: { color: CHART_COLORS.blue }, data: curve?.load.map((s) => s.value) ?? [] },
      { name: '日前电价 (元/MWh)', type: 'line', smooth: true, showSymbol: false, yAxisIndex: 1, itemStyle: { color: CHART_COLORS.brand }, data: curve?.dayAhead.map((s) => s.value) ?? [] },
      { name: '实时电价 (元/MWh)', type: 'line', smooth: true, showSymbol: false, yAxisIndex: 1, lineStyle: { type: 'dashed' }, itemStyle: { color: CHART_COLORS.dark }, data: curve?.realtime.map((s) => s.value) ?? [] },
    ],
  }
}, [snapshot])

/* ---------- 图 2：日指标趋势 ---------- */
const trendEl = ref<HTMLElement>()
useECharts(trendEl, (): EChartsOption => {
  const days = snapshot.value?.days ?? []
  return {
    grid: { left: 48, right: 16, top: 32, bottom: 28 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: { type: 'category', data: days.map((d) => d.date), axisLabel: { fontSize: 12, color: CHART_COLORS.gray } },
    yAxis: { type: 'value', axisLabel: { fontSize: 12, color: CHART_COLORS.gray }, splitLine: { lineStyle: { color: '#eef0f3' } } },
    series: [
      { name: '日前均价 (元/MWh)', type: 'line', smooth: true, showSymbol: false, itemStyle: { color: CHART_COLORS.brand }, data: days.map((d) => d.dayAhead.avg) },
      { name: '实时均价 (元/MWh)', type: 'line', smooth: true, showSymbol: false, itemStyle: { color: CHART_COLORS.cyan }, data: days.map((d) => d.realtime.avg) },
    ],
  } as EChartsOption
}, [snapshot])
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>数据加载失败</strong>
    <span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <div v-if="!loading && !error && !hasData" class="notice">
    <strong>当前运行日暂无数据</strong>
    <span>　gd-metric 只覆盖 CSV 内有数据的运行日，可在右上角换日期。</span>
  </div>

  <div class="kpis">
    <KpiCard label="运行日" :value="snapshot?.date || '—'" :hint="snapshot?.latestDate ? `最新可用 ${snapshot.latestDate}` : ''" />
    <KpiCard label="负荷均值" :value="num(latest?.load.avg)" :unit="latest?.load.unit || 'MW'" hint="全时段算术平均" />
    <KpiCard label="最大负荷" :value="num(latest?.load.max)" :unit="latest?.load.unit || 'MW'" hint="当日 15 分钟峰值" />
    <KpiCard label="日前均价" :value="num(latest?.dayAhead.avg)" :unit="latest?.dayAhead.unit || '元/MWh'" hint="统一结算点" />
    <KpiCard label="实时均价" :value="num(latest?.realtime.avg)" :unit="latest?.realtime.unit || '元/MWh'" hint="统一结算点" />
    <KpiCard label="数据来源" :value="snapshot?.source || '—'" :hint="snapshot?.updatedAt ? `更新于 ${snapshot.updatedAt}` : ''" />
  </div>

  <div class="grid">
    <PanelBox title="负荷与电价曲线" sub="15 分钟粒度 · 左轴负荷 / 右轴电价" :span="12" :loading="loading"
      :foot="`数据来源：${snapshot?.source || 'gd-metric'} · 运行日 ${snapshot?.date || '—'}`">
      <div ref="curveEl" class="chart" />
    </PanelBox>

    <PanelBox title="日指标趋势" sub="近段各运行日的均价" :span="12" :loading="loading"
      foot="数据来源：gd-metric · 只含 CSV 内有数据的运行日">
      <div ref="trendEl" class="chart" />
    </PanelBox>

    <PanelBox title="新能源出力 / 供需比" :span="6" foot="日报未提取 · 口径待定（设计稿 §10.1）">
      <div class="pending">
        <StatusTag tone="brand">待数据源</StatusTag>
        <span>新能源发电量、最大净负荷、供需比（总负荷 / 净负荷）需要日报结构化提取，当前日报只抽了成交电价与申报信息。</span>
      </div>
    </PanelBox>

    <PanelBox title="火电机组启停状态" :span="6" foot="四分类口径待确认（设计稿 §10.9）">
      <div class="pending">
        <StatusTag tone="brand">待口径</StatusTag>
        <span>必开 / 必停 / 市场竞争已开 / 市场竞争已停 的来源表尚未确定。</span>
      </div>
    </PanelBox>
  </div>
</template>

<style scoped>
.kpis {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: var(--s4);
  margin-bottom: var(--s4);
}
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.chart { height: 320px; }
.notice {
  display: flex;
  align-items: center;
  gap: var(--s3);
  margin-bottom: var(--s4);
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-13);
}
.notice.error { border-left-color: var(--red); background: var(--red-soft); color: var(--red-ink); }
.notice button {
  margin-left: auto;
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.pending {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--s2);
  min-height: 120px;
  font-size: var(--fs-13);
  color: var(--text-3);
}
@media (max-width: 1280px) {
  .kpis { grid-template-columns: repeat(3, 1fr); }
  .grid { grid-template-columns: repeat(6, 1fr); }
}
</style>
