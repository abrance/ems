<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  DATASET,
  fetchSpotMarketBoard,
  fmtNum,
  fmtPct,
  type MarketBoard,
  type MarketSeries,
} from '@/api/spotmarket'
import { fetchHome24h, fetchNodePrice, periodLabel, type Home24hDay, type NodePriceDay } from '@/api/homemarket'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 市场总览 —— 一屏看三个不同滞后的批次。
 *
 * **为什么单独一页，不并进「能源总览」**：能源总览是设备侧口径（SOC/功率/告警），
 * 数据来自现场采集、截止日就是当下；市场侧是官方披露，三个批次各有各的截止日。
 * 混在一页会出现「两套截止日」且看不出来哪个数属于哪一批 —— 那是这页要解决的问题，
 * 不是要复制的问题。
 *
 * 三批数据，粒度与滞后都不同，所以是三张表、三个接口，不混表：
 *
 *   地市节点价   gd_node_price   /api/gd-metric/node-price     T+0   42 条（21 市 × 高峰/低谷）
 *   日频指标     gd_spot_metric  /api/spot-market/board         T+2   日均价/成交量/申报均价…
 *   24 点分时    gd_home_24h     /api/gd-metric/home-24h        T-6   24 个整点
 *
 * 页头批次条把三个日期**分别**标出来（含来源与滞后），就是为了让人一眼看出
 * 「这三个数不是同一天的东西」，而不是拿其中一个当「最新数据」。
 *
 * 每个面板右上角有源角标；空日期给明确的「该日无数据」而不是空图。
 */
const HOME24H_SOURCE = '官网·24 点 T-6'
const DAILY_SOURCE = '官网·日频 T+2'
const NODE_SOURCE = '官网·节点价 T+0'

const daily = ref<MarketBoard | null>(null)
const home24h = ref<Home24hDay | null>(null)
const nodePrice = ref<NodePriceDay | null>(null)
const loadingDaily = ref(false)
const loading24h = ref(false)
const loadingNode = ref(false)
const errors = ref<string[]>([])

async function loadDaily() {
  loadingDaily.value = true
  try {
    daily.value = await fetchSpotMarketBoard({ days: 90 })
  } catch (err) {
    errors.value.push(`日频指标：${err instanceof Error ? err.message : '加载失败'}`)
    daily.value = null
  } finally {
    loadingDaily.value = false
  }
}

async function load24h() {
  loading24h.value = true
  try {
    // 不传 date：让后端给库里最新一天（源站 T-6，硬取今天永远是空）
    home24h.value = await fetchHome24h({})
  } catch (err) {
    errors.value.push(`24 点分时：${err instanceof Error ? err.message : '加载失败'}`)
    home24h.value = null
  } finally {
    loading24h.value = false
  }
}

async function loadNode() {
  loadingNode.value = true
  try {
    nodePrice.value = await fetchNodePrice({})
  } catch (err) {
    errors.value.push(`地市节点价：${err instanceof Error ? err.message : '加载失败'}`)
    nodePrice.value = null
  } finally {
    loadingNode.value = false
  }
}

/** 三路并行：任何一路慢/失败都不拖累另外两路（它们的源站本来就互不相关） */
function reloadAll() {
  errors.value = []
  void Promise.all([loadDaily(), load24h(), loadNode()])
}

onMounted(reloadAll)

/* ---------- 批次条：三个日期分别标注 ---------- */

const batches = computed(() => [
  {
    key: 'node',
    label: '地市节点价',
    date: nodePrice.value?.latestDate || '—',
    lag: nodePrice.value?.lagDays ?? -1,
    source: NODE_SOURCE,
    tone: 'green' as const,
  },
  {
    key: 'daily',
    label: '日频指标',
    date: daily.value?.date || '—',
    lag: lagOf(daily.value?.date),
    source: DAILY_SOURCE,
    tone: 'blue' as const,
  },
  {
    key: 'home24h',
    label: '24 点分时',
    date: home24h.value?.latestDate || '—',
    lag: home24h.value?.lagDays ?? -1,
    source: HOME24H_SOURCE,
    tone: 'brand' as const,
  },
])

function lagOf(date?: string): number {
  if (!date) return -1
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime())) return -1
  const today = new Date()
  const a = Date.UTC(d.getFullYear(), d.getMonth(), d.getDate())
  const b = Date.UTC(today.getFullYear(), today.getMonth(), today.getDate())
  const lag = Math.round((b - a) / 86400_000)
  return lag < 0 ? 0 : lag
}

/** 页脚：数据截至 X —— 三个日期一起写，不挑一个代表全部 */
const dataAsOf = computed(() => {
  const parts = batches.value.filter((b) => b.date !== '—').map((b) => `${b.label} ${b.date}`)
  return parts.length ? parts.join(' / ') : '—'
})

/* ---------- 6 张日频 KPI ---------- */

const dailySeries = computed<MarketSeries[]>(() => daily.value?.daily ?? [])
function pick(dataset: string, metricPart: string): MarketSeries | undefined {
  return dailySeries.value.find((s) => s.dataset === dataset && s.metric.includes(metricPart))
}

const aheadAvg = computed(() => pick(DATASET.ahead, '加权平均电价'))
const realAvg = computed(() => pick(DATASET.realtime, '加权平均电价'))
const declPrice = computed(() => pick(DATASET.ahead, '申报均价'))
const aheadVolGen = computed(() => pick(DATASET.ahead, '发电侧日前总成交电量'))
const aheadVolCon = computed(() => pick(DATASET.ahead, '用电侧日前总成交电量'))
const aheadMax = computed(() => pick(DATASET.ahead, '加权最高电价'))
const aheadMin = computed(() => pick(DATASET.ahead, '加权最低电价'))

const priceUnit = computed(() => aheadAvg.value?.unit || '元/兆瓦时')
const volUnit = computed(() => aheadVolGen.value?.unit || 'MWh')
const dailyEmpty = computed(() => !loadingDaily.value && dailySeries.value.length === 0)

/* ---------- 图 1：日频趋势 ---------- */

const trendEl = ref<HTMLElement>()
useECharts(
  trendEl,
  (): EChartsOption => {
    const a = aheadAvg.value
    const r = realAvg.value
    const periods = a?.points.map((p) => p.period) ?? []
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: periods,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: periods.length > 45 ? 30 : 0 },
      },
      yAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '日前加权均价',
          type: 'line',
          smooth: true,
          showSymbol: false,
          itemStyle: { color: CHART_COLORS.brand },
          data: a?.points.map((p) => p.value) ?? [],
        },
        {
          name: '实时加权均价',
          type: 'line',
          smooth: true,
          showSymbol: false,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.blue },
          data: r?.points.map((p) => p.value) ?? [],
        },
      ],
    }
  },
  [dailySeries],
)

/* ---------- 图 2：24 点电价曲线 ---------- */

const home24hPoints = computed(() => home24h.value?.points ?? [])
const home24hEmpty = computed(() => !loading24h.value && (home24h.value?.empty ?? true))

const price24hEl = ref<HTMLElement>()
useECharts(
  price24hEl,
  (): EChartsOption => {
    const pts = home24hPoints.value
    const hours = pts.map((p) => `${String(p.hour).padStart(2, '0')}:00`)
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: hours,
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          interval: (_i: number, value: string) => /^(00:00|04:00|08:00|12:00|16:00|20:00)$/.test(value),
        },
      },
      yAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '日前电价',
          type: 'line',
          showSymbol: false,
          itemStyle: { color: CHART_COLORS.brand },
          data: pts.map((p) => p.aheadPrice),
        },
        {
          name: '实时电价',
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.blue },
          data: pts.map((p) => p.realtimePrice),
        },
      ],
    }
  },
  [home24hPoints],
)

/* ---------- 图 3：24 点负荷（单独一张小图，不做双 y 轴） ---------- */

const power24hEl = ref<HTMLElement>()
useECharts(
  power24hEl,
  (): EChartsOption => {
    const pts = home24hPoints.value
    const hours = pts.map((p) => `${String(p.hour).padStart(2, '0')}:00`)
    return {
      grid: { left: 64, right: 20, top: 32, bottom: 40 },
      tooltip: {
        trigger: 'axis',
        valueFormatter: (v) => (v == null ? '—' : `${Number(v).toLocaleString('zh-CN', { maximumFractionDigits: 0 })} MW`),
      },
      xAxis: {
        type: 'category',
        data: hours,
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          interval: (_i: number, value: string) => /^(00:00|04:00|08:00|12:00|16:00|20:00)$/.test(value),
        },
      },
      yAxis: {
        type: 'value',
        name: 'MW',
        nameTextStyle: { fontSize: 12 },
        // 负荷动辄几万 MW：不除以万，y 轴标签会挤成一团
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          formatter: (v: number) => `${(v / 10000).toFixed(1)}万`,
        },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '系统负荷',
          type: 'line',
          showSymbol: false,
          areaStyle: { opacity: 0.14 },
          itemStyle: { color: CHART_COLORS.cyan },
          data: pts.map((p) => p.powerMw),
        },
      ],
    }
  },
  [home24hPoints],
)

/* ---------- 图 4：地市横向条形（42 条） ---------- */

const nodePoints = computed(() => nodePrice.value?.points ?? [])
const nodeEmpty = computed(() => !loadingNode.value && (nodePrice.value?.empty ?? true))

/** 横向条形按价格升序：最高的排最上面，读起来像排行榜 */
const nodeBars = computed(() =>
  [...nodePoints.value].sort((a, b) => a.price - b.price),
)

const nodeEl = ref<HTMLElement>()
useECharts(
  nodeEl,
  (): EChartsOption => {
    const rows = nodeBars.value
    const labels = rows.map((r) => `${r.city}·${periodLabel(r.periodType)}`)
    return {
      grid: { left: 110, right: 60, top: 16, bottom: 24 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      xAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      yAxis: {
        type: 'category',
        data: labels,
        axisLabel: { fontSize: 11, color: CHART_COLORS.gray },
      },
      series: [
        {
          type: 'bar',
          // 高峰用主色、低谷用蓝色：同一维度两种时段，不是两个指标
          data: rows.map((r) => ({
            value: r.price,
            itemStyle: { color: r.periodType === 1 ? CHART_COLORS.brand : CHART_COLORS.blue },
          })),
          label: {
            show: true,
            position: 'right',
            fontSize: 11,
            formatter: (p: { value?: unknown }) => Number(p.value ?? 0).toFixed(1),
          },
          barMaxWidth: 10,
        },
      ],
    }
  },
  [nodeBars],
)

/** 地市条形图要 42 行，容器按行数给高度，否则条被压扁 */
const nodeChartHeight = computed(() => `${Math.max(320, nodeBars.value.length * 18 + 60)}px`)
</script>

<template>
  <div v-if="errors.length" class="notice error">
    <strong>部分数据加载失败</strong>
    <span>　{{ errors.join('；') }}</span>
    <button type="button" @click="reloadAll">重试</button>
  </div>

  <!-- 批次条：三个日期分别标注来源与滞后，不拿一个日期代表全部 -->
  <div class="batches">
    <div v-for="b in batches" :key="b.key" class="batch">
      <span class="batch-label">{{ b.label }}</span>
      <strong class="batch-date">{{ b.date }}</strong>
      <StatusTag :tone="b.tone">{{ b.source }}</StatusTag>
      <span v-if="b.lag >= 0" class="batch-lag">滞后 {{ b.lag }} 天</span>
      <span v-else class="batch-lag">无数据</span>
    </div>
  </div>

  <div class="grid">
    <!-- 6 张日频 KPI -->
    <PanelBox
      title="日频指标"
      sub="现货市场总体运行情况 · 发电侧加权口径"
      :span="12"
      :loading="loadingDaily"
      :foot="`数据来源：广东电力交易中心披露接口（pm.gd.csg.cn）直采 · 最新运行日 ${daily?.date || '—'}`"
    >
      <template #actions>
        <StatusTag tone="blue">{{ DAILY_SOURCE }}</StatusTag>
      </template>
      <div v-if="dailyEmpty" class="empty">
        该日无数据：披露接口还没有这一天的指标（实时 T+2 才发布）。库里最新运行日 {{ daily?.date || '—' }}。
      </div>
      <div v-else class="kpis">
        <KpiCard
          label="日前加权均价"
          :value="fmtNum(aheadAvg?.latest.value)"
          :unit="priceUnit"
          :hint="`环比 ${fmtPct(aheadAvg?.momPct)} · 同比 ${fmtPct(aheadAvg?.yoyPct)}`"
        />
        <KpiCard
          label="实时加权均价"
          :value="fmtNum(realAvg?.latest.value)"
          :unit="priceUnit"
          :hint="`运行日 ${realAvg?.latest.period || '—'}（实时 T+2）`"
        />
        <KpiCard
          label="日前峰谷价差"
          :value="fmtNum((aheadMax?.latest.value ?? 0) - (aheadMin?.latest.value ?? 0))"
          :unit="priceUnit"
          :hint="`最高 ${fmtNum(aheadMax?.latest.value)} / 最低 ${fmtNum(aheadMin?.latest.value)}`"
        />
        <KpiCard
          label="日前-实时价差"
          :value="fmtNum(daily?.spreadMean)"
          :unit="priceUnit"
          hint="区间均值，正=日前更高"
        />
        <KpiCard
          label="日前成交电量·发电侧"
          :value="fmtNum(aheadVolGen?.latest.value, 0)"
          :unit="volUnit"
          :hint="`用电侧 ${fmtNum(aheadVolCon?.latest.value, 0)} · 环比 ${fmtPct(aheadVolGen?.momPct)}`"
        />
        <KpiCard
          label="日前申报均价"
          :value="fmtNum(declPrice?.latest.value)"
          :unit="priceUnit"
          :hint="`环比 ${fmtPct(declPrice?.momPct)} · 仅日前披露`"
        />
      </div>
    </PanelBox>

    <!-- 日频趋势 -->
    <PanelBox
      title="日频趋势"
      sub="日前 / 实时加权均价 · 近 90 天"
      :span="12"
      :loading="loadingDaily"
      :foot="`数据来源：${daily?.source || 'pm.gd.csg.cn'} · 运行日 ${daily?.from || '—'} ~ ${daily?.to || '—'}`"
    >
      <template #actions>
        <StatusTag tone="blue">{{ DAILY_SOURCE }}</StatusTag>
      </template>
      <div v-if="dailyEmpty" class="empty">该区间无数据（披露接口还没有落库）。</div>
      <div v-else ref="trendEl" class="chart" />
    </PanelBox>

    <!-- 24 点电价 -->
    <PanelBox
      title="24 点电价曲线"
      sub="日前 / 实时电价 · 整点"
      :span="8"
      :loading="loading24h"
      :foot="`数据来源：pm.gd.csg.cn 主页 24 点 · 运行日 ${home24h?.latestDate || '—'}（源站滞后约 6 天）`"
    >
      <template #actions>
        <StatusTag tone="brand">{{ HOME24H_SOURCE }}</StatusTag>
      </template>
      <div v-if="home24hEmpty" class="empty">
        该日无数据：主页 24 点滞后约 6 天才发布，库里最新到
        {{ home24h?.latestDate || '（还没有任何数据）' }}。
      </div>
      <div v-else ref="price24hEl" class="chart" />
    </PanelBox>

    <!-- 24 点负荷：单独一张小图，不做双 y 轴 -->
    <PanelBox
      title="24 点系统负荷"
      sub="MW · 与上图同一运行日"
      :span="4"
      :loading="loading24h"
      :foot="home24h && !home24hEmpty ? `峰值 ${fmtNum(home24h.peakPowerMw, 0)} MW · 谷值 ${fmtNum(home24h.lowPowerMw, 0)} MW` : '负荷单独成图：与电价量纲差两个数量级，双 y 轴会看错拐点'"
    >
      <template #actions>
        <StatusTag tone="brand">{{ HOME24H_SOURCE }}</StatusTag>
      </template>
      <div v-if="home24hEmpty" class="empty">该日无数据。</div>
      <div v-else ref="power24hEl" class="chart" />
    </PanelBox>

    <!-- 地市横向条形 -->
    <PanelBox
      title="地市节点电价"
      sub="21 市 × 高峰 / 低谷 · 元/兆瓦时"
      :span="12"
      :loading="loadingNode"
      :foot="`数据来源：pm.gd.csg.cn 主页地市节点价（T+0）· 运行日 ${nodePrice?.latestDate || '—'} · 高峰 ${nodePrice?.peakCities ?? 0} 市 / 低谷 ${nodePrice?.valleyCities ?? 0} 市`"
    >
      <template #actions>
        <StatusTag tone="green">{{ NODE_SOURCE }}</StatusTag>
      </template>
      <div v-if="nodeEmpty" class="empty">
        该日无数据：地市节点价 T+0 可得，库里最新到 {{ nodePrice?.latestDate || '（还没有任何数据）' }}。
      </div>
      <div v-else ref="nodeEl" class="chart" :style="{ height: nodeChartHeight }" />
    </PanelBox>
  </div>

  <div class="footer">数据截至 {{ dataAsOf }}。三个批次的滞后不同，不能互相当作「最新数据」。</div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.batches {
  display: flex;
  flex-wrap: wrap;
  gap: var(--s3);
  margin-bottom: var(--s4);
}
.batch {
  display: flex;
  align-items: center;
  gap: var(--s2);
  padding: var(--s2) var(--s3);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  font-size: var(--fs-13);
}
.batch-label { color: var(--text-3); font-size: var(--fs-12); }
.batch-date { font-variant-numeric: tabular-nums; }
.batch-lag { color: var(--text-3); font-size: var(--fs-12); }
.kpis { display: grid; grid-template-columns: repeat(6, 1fr); gap: var(--s4); }
.chart { height: 360px; }
.empty {
  display: grid;
  place-items: center;
  min-height: 140px;
  padding: var(--s4);
  color: var(--text-3);
  font-size: var(--fs-13);
  text-align: center;
  line-height: 1.7;
}
.footer {
  margin-top: var(--s4);
  padding: var(--s2) var(--s1);
  color: var(--text-3);
  font-size: var(--fs-12);
}
.notice {
  display: flex;
  align-items: center;
  gap: var(--s3);
  margin-bottom: var(--s4);
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--red);
  border-radius: var(--r-sm);
  background: var(--red-soft);
  color: var(--red-ink);
  font-size: var(--fs-13);
}
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
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(3, 1fr); }
}
</style>
