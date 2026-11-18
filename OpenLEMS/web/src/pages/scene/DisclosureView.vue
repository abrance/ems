<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import { RangePicker } from '@arco-design/web-vue'
import '@arco-design/web-vue/es/date-picker/style/css.js'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import {
  DATASET,
  DATASET_LABELS,
  fmtNum,
  fmtPct,
  fetchSpotMarketBoard,
  fetchSpotMarketLatest,
  seriesLabel,
  type MarketBoard,
  type MarketSeries,
  type SeriesPoint,
} from '@/api/spotmarket'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'
import { dailyTier } from '@/utils/dailyLag'
import { isoDate, lagDaysFrom, rangeEndingAt, todayInShanghai } from '@/utils/latestRange'

/**
 * 场景 · 信息披露 —— 广东电力市场官方指标。
 *
 * 数据源只有一条：广东电力交易中心信息披露接口直采（origin=pm-api），
 * 经 tools/gdpm/gdpm_sqlite.py 落到业务库 gd_spot_metric。
 * OCR 日报旧值与第三方 gdmetric CSV 都已停用作废（旧值保留为 origin=void 可追溯），
 * 所以这一页**不再**读 /api/gd-metric。
 *
 * 分两个页签，因为它们的时间粒度不同、看的也是不同的问题：
 *   日频     —— 现货：日前/实时 × 发电侧/用电侧 的成交量、加权均价、加权极值、申报均价
 *   月频年频 —— 中长期合约、绿电、零售用户数、HHI、Top-m 集中度
 *
 * 环比/同比/峰谷价差都由后端算（见 internal/logic/spotmarket），页面只展示，
 * 避免同一口径在前后端各实现一遍、然后慢慢漂移。
 */
const tabs = [
  { v: 'daily', t: '日频（现货）' },
  { v: 'periodic', t: '月频 / 年频' },
] as const
const DEFAULT_DAYS = 90
const MAX_DAYS = 730

const tab = ref<'daily' | 'periodic'>('daily')
/** 日频图/表窗口。右端锚到库里最新有数日，不锚「今天」。 */
const range = ref<[string, string]>(['', ''])
const rangeHint = ref('')
/** KPI 固定看全局最新（不受下方范围影响），单独一张快照。 */
const kpiBoard = ref<MarketBoard | null>(null)
const board = ref<MarketBoard | null>(null)
const loading = ref(false)
const error = ref('')
let boardController: AbortController | null = null
let kpiController: AbortController | null = null

const DAY_MS = 86400_000
function parseDay(s: string): number {
  return Date.parse(`${s}T00:00:00Z`)
}
function spanDays(from: string, to: string): number {
  return Math.round((parseDay(to) - parseDay(from)) / DAY_MS) + 1
}
function dayString(v: unknown): string {
  if (v instanceof Date) return isoDate(v)
  if (typeof v === 'number') return isoDate(new Date(v))
  return /^\d{4}-\d{2}-\d{2}$/.test(String(v ?? '')) ? String(v) : ''
}
function disabledFutureDate(date: Date): boolean {
  return isoDate(date) > todayInShanghai()
}

function onRangeChange(v: unknown) {
  if (!Array.isArray(v) || v.length < 2) return
  let from = dayString(v[0])
  const to = dayString(v[1])
  if (!from || !to || parseDay(from) > parseDay(to)) return
  rangeHint.value = ''
  if (spanDays(from, to) > MAX_DAYS) {
    from = rangeEndingAt(new Date(`${to}T00:00:00Z`), MAX_DAYS - 1).from
    rangeHint.value = `范围超过 ${MAX_DAYS} 天，已按 ${MAX_DAYS} 天取数`
  }
  range.value = [from, to]
  void loadBoard()
}

async function loadBoard() {
  const [from, to] = range.value
  if (!from || !to) return
  boardController?.abort()
  boardController = new AbortController()
  loading.value = true
  error.value = ''
  try {
    board.value = await fetchSpotMarketBoard(
      { date: to, days: spanDays(from, to) },
      boardController.signal,
    )
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') return
    error.value = err instanceof Error ? err.message : '加载失败'
    board.value = null
  } finally {
    loading.value = false
  }
}

function reload() {
  void Promise.all([loadBoard(), loadKpi()])
}

async function loadKpi() {
  kpiController?.abort()
  kpiController = new AbortController()
  try {
    kpiBoard.value = await fetchSpotMarketBoard({ days: MAX_DAYS }, kpiController.signal)
  } catch {
    // KPI 失败不拖垮图/表；error 由 loadBoard 负责说
    kpiBoard.value = null
  }
}

onMounted(async () => {
  let end = todayInShanghai()
  try {
    const latest = await fetchSpotMarketLatest()
    if (latest.date) end = latest.date
  } catch {
    error.value = '最新运行日获取失败，默认按今天回看 90 天'
  }
  range.value = [rangeEndingAt(new Date(`${end}T00:00:00Z`), DEFAULT_DAYS - 1).from, end]
  await Promise.all([loadBoard(), loadKpi()])
})

/* ---------- 日频取数 ---------- */

const daily = computed<MarketSeries[]>(() => board.value?.daily ?? [])
const kpiDaily = computed<MarketSeries[]>(() => kpiBoard.value?.daily ?? [])

/** 按数据集 + 指标名片段取一条序列（指标名是源站原名，只能按片段匹配） */
function pick(dataset: string, metricPart: string, from: MarketSeries[]): MarketSeries | undefined {
  return from.find((s) => s.dataset === dataset && s.metric.includes(metricPart))
}

// 图/表用区间内的数据；KPI 单独用全局最新快照，两者不能混用同一变量
const aheadAvg = computed(() => pick(DATASET.ahead, '加权平均电价', daily.value))
const realAvg = computed(() => pick(DATASET.realtime, '加权平均电价', daily.value))
const aheadVolGen = computed(() => pick(DATASET.ahead, '发电侧日前总成交电量', daily.value))
const aheadVolCon = computed(() => pick(DATASET.ahead, '用电侧日前总成交电量', daily.value))

const aheadAvgKpi = computed(() => pick(DATASET.ahead, '加权平均电价', kpiDaily.value))
const realAvgKpi = computed(() => pick(DATASET.realtime, '加权平均电价', kpiDaily.value))
const declPriceKpi = computed(() => pick(DATASET.ahead, '申报均价', kpiDaily.value))
const aheadVolGenKpi = computed(() => pick(DATASET.ahead, '发电侧日前总成交电量', kpiDaily.value))
const aheadVolConKpi = computed(() => pick(DATASET.ahead, '用电侧日前总成交电量', kpiDaily.value))

const priceUnit = computed(() => aheadAvgKpi.value?.unit || '元/兆瓦时')
const volUnit = computed(() => aheadVolGenKpi.value?.unit || 'MWh')

/** KPI 峰谷价差：全局快照里日前加权均价的最高-最低（后端已算，这里只取） */
const aheadSpreadKpi = computed(() => aheadAvgKpi.value?.spread ?? 0)

/** 缺失日期按日去重：同一天多个数据集缺数只算一天。 */
const coverageNote = computed(() => {
  if (!board.value || board.value.daily.length === 0) return '无覆盖数据'
  const missing = [...new Set((board.value?.coverage ?? []).flatMap((c) => c.missing))].sort()
  if (missing.length === 0) return '区间内无缺日'
  const head = missing.slice(0, 3).join('、')
  return `区间内缺 ${missing.length} 天（${head}${missing.length > 3 ? '…' : ''}）`
})

/** 空/滞后分级与市场总览同口径，避免「采集停了」被说成「还没发布」。 */
const latestRunDate = computed(() => kpiBoard.value?.date || '')
const dailyLag = computed(() => lagDaysFrom(latestRunDate.value))
const dailyEmpty = computed(() => !loading.value && daily.value.length === 0)
const dailyTierNow = computed(() => dailyTier(dailyLag.value, dailyEmpty.value, 'ahead'))
const dailyStateNote = computed(() => {
  if (dailyTierNow.value === 't2') return ''
  const latest = latestRunDate.value || '—'
  if (dailyTierNow.value === 'never') {
    return '库里近一年没有日频指标。日频来自披露接口直采，请检查采集是否在跑。'
  }
  if (dailyTierNow.value === 'unknown') return ''
  const head = `采集${dailyTierNow.value === 'stop' ? '已落后' : '滞后'}：库里最新运行日 ${latest}（${dailyLag.value} 天前）`
  return `${head}，${dailyTierNow.value === 'stop' ? '请检查采集是否在跑' : '已超出日前当日发布的预期'}`
})

/* ---------- 图 1：加权均价 ---------- */
const priceEl = ref<HTMLElement>()
useECharts(
  priceEl,
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
        splitLine: { lineStyle: { color: '#eef0f3' } },
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
  [daily],
)

/* ---------- 图 2：成交电量 ---------- */
const volEl = ref<HTMLElement>()
useECharts(
  volEl,
  (): EChartsOption => {
    const gen = aheadVolGen.value
    const con = aheadVolCon.value
    const periods = gen?.points.map((p) => p.period) ?? []
    return {
      grid: { left: 70, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} MWh`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: periods,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: periods.length > 45 ? 30 : 0 },
      },
      yAxis: {
        type: 'value',
        name: 'MWh',
        nameTextStyle: { fontSize: 12 },
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          formatter: (v: number) => `${(v / 10000).toFixed(0)}万`,
        },
        splitLine: { lineStyle: { color: '#eef0f3' } },
      },
      series: [
        {
          name: '日前·发电侧',
          type: 'line',
          smooth: true,
          showSymbol: false,
          areaStyle: { opacity: 0.12 },
          itemStyle: { color: CHART_COLORS.brand },
          data: gen?.points.map((p) => p.value) ?? [],
        },
        {
          name: '日前·用电侧',
          type: 'line',
          smooth: true,
          showSymbol: false,
          itemStyle: { color: CHART_COLORS.cyan },
          data: con?.points.map((p) => p.value) ?? [],
        },
      ],
    }
  },
  [daily],
)

/* ---------- 月频 / 年频 ---------- */

const periodic = computed<MarketSeries[]>(() => board.value?.periodic ?? [])

function periodicOf(dataset: string, metricPart?: string): MarketSeries[] {
  return periodic.value.filter(
    (s) => s.dataset === dataset && (!metricPart || s.metric.includes(metricPart)),
  )
}

/** 中长期月度：净/累计 均价（两条线，同一期次） */
const mlPrice = computed(() => {
  const net = periodicOf('mid_long_term_month', '净合约成交均价')[0]
  const total = periodicOf('mid_long_term_month', '累计合约成交均价')[0]
  return { net, total }
})

const mlPriceEl = ref<HTMLElement>()
useECharts(
  mlPriceEl,
  (): EChartsOption => {
    const { net, total } = mlPrice.value
    const periods = net?.points.map((p) => p.period) ?? []
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: periods,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: periods.length > 18 ? 30 : 0 },
      },
      yAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: '#eef0f3' } },
      },
      series: [
        {
          name: '净合约均价',
          type: 'line',
          smooth: true,
          itemStyle: { color: CHART_COLORS.brand },
          data: net?.points.map((p) => p.value) ?? [],
        },
        {
          name: '累计合约均价',
          type: 'line',
          smooth: true,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.dark },
          data: total?.points.map((p) => p.value) ?? [],
        },
      ],
    }
  },
  [periodic],
)

const mlVolEl = ref<HTMLElement>()
useECharts(
  mlVolEl,
  (): EChartsOption => {
    const net = periodicOf('mid_long_term_month', '净合约成交电量')[0]
    const periods = net?.points.map((p) => p.period) ?? []
    return {
      grid: { left: 70, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} MWh`) },
      xAxis: {
        type: 'category',
        data: periods,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: periods.length > 18 ? 30 : 0 },
      },
      yAxis: {
        type: 'value',
        name: 'MWh',
        nameTextStyle: { fontSize: 12 },
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          formatter: (v: number) => `${(v / 10000).toFixed(0)}万`,
        },
        splitLine: { lineStyle: { color: '#eef0f3' } },
      },
      series: [
        {
          name: '净合约成交电量',
          type: 'bar',
          itemStyle: { color: CHART_COLORS.cyan },
          data: net?.points.map((p) => p.value) ?? [],
        },
      ],
    }
  },
  [periodic],
)

/**
 * 绿电月度按「交易序列」入库（一条 gd_spot_metric 只放得下一个数），
 * 这里按月份汇总：电量求和、电能量均价按电量加权。
 *
 * 加权口径写在图注里：直接对各序列均价取算术平均，会被小电量序列带偏。
 */
const greenMonthly = computed(() => {
  const vol = periodicOf('green_power_month', '成交电量')
  const price = periodicOf('green_power_month', '电能量成交均价')
  const byMonth = new Map<string, { volume: number; cost: number }>()
  for (const s of vol) {
    for (const p of s.points) {
      const cur = byMonth.get(p.period) ?? { volume: 0, cost: 0 }
      cur.volume += p.value
      byMonth.set(p.period, cur)
    }
  }
  for (const s of price) {
    for (const p of s.points) {
      const cur = byMonth.get(p.period)
      // 均价序列与电量序列按 (期次, 序列名称) 一一对应；用同期电量做权重
      const w = p.value
      if (!cur || w <= 0) continue
      const sameVol = vol.find((v) => v.scope === s.scope)?.points.find((x) => x.period === p.period)
      cur.cost += p.value * (sameVol?.value ?? 0)
    }
  }
  const periods = [...byMonth.keys()].sort()
  return periods.map((period) => {
    const { volume, cost } = byMonth.get(period)!
    return { period, volume, price: volume > 0 ? cost / volume : 0 }
  })
})

const greenEl = ref<HTMLElement>()
useECharts(
  greenEl,
  (): EChartsOption => {
    const rows = greenMonthly.value
    return {
      grid: { left: 70, right: 60, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis' },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: rows.map((r) => r.period),
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: rows.length > 12 ? 30 : 0 },
      },
      yAxis: [
        {
          type: 'value',
          name: 'MWh',
          nameTextStyle: { fontSize: 12 },
          axisLabel: {
            fontSize: 12,
            color: CHART_COLORS.gray,
            formatter: (v: number) => `${(v / 10000).toFixed(0)}万`,
          },
          splitLine: { lineStyle: { color: '#eef0f3' } },
        },
        {
          type: 'value',
          name: '元/兆瓦时',
          nameTextStyle: { fontSize: 12 },
          axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
          splitLine: { show: false },
        },
      ],
      series: [
        {
          name: '绿电成交电量',
          type: 'bar',
          itemStyle: { color: CHART_COLORS.green },
          data: rows.map((r) => Math.round(r.volume)),
        },
        {
          name: '电能量加权均价',
          type: 'line',
          yAxisIndex: 1,
          smooth: true,
          itemStyle: { color: CHART_COLORS.gold },
          data: rows.map((r) => Math.round(r.price * 100) / 100),
        },
      ],
    }
  },
  [periodic],
)

/* ---------- 表 ---------- */

/** 涨跌只区分方向，不套「红涨绿跌」的好坏语义（电量涨了不一定是好事） */
function dirClass(v: number | null | undefined): string {
  if (v === null || v === undefined) return ''
  return v > 0 ? 'up' : v < 0 ? 'down' : ''
}

/** 日频表按数据集分组，方便对照日前/实时 */
const dailyGroups = computed(() => {
  const groups = new Map<string, MarketSeries[]>()
  for (const s of daily.value) {
    const list = groups.get(s.dataset) ?? []
    list.push(s)
    groups.set(s.dataset, list)
  }
  return [...groups.entries()].map(([dataset, series]) => ({
    dataset,
    label: DATASET_LABELS[dataset] ?? dataset,
    series,
  }))
})

/** 单表最多列多少条：绿电按交易序列入库，一组能有上百条，全铺出来没法看 */
const ROWS_PER_GROUP = 12

const periodicGroups = computed(() => {
  const groups = new Map<string, MarketSeries[]>()
  for (const s of periodic.value) {
    const list = groups.get(s.dataset) ?? []
    list.push(s)
    groups.set(s.dataset, list)
  }
  return [...groups.entries()].map(([dataset, series]) => ({
    dataset,
    label: DATASET_LABELS[dataset] ?? dataset,
    series: series.slice(0, ROWS_PER_GROUP),
    total: series.length,
  }))
})

/** 月频/年频的期次显示：2026-10-01 -> 2026-10，2025-01-01 -> 2025 */
function periodLabel(p: string, grain: string): string {
  return grain === 'year' ? p.slice(0, 4) : p.slice(0, 7)
}

/** 序列散点：给「日前-实时价差」图用 */
const spreadEl = ref<HTMLElement>()
const spreadPoints = computed<SeriesPoint[]>(() => board.value?.aheadRealtimeSpread ?? [])
useECharts(
  spreadEl,
  (): EChartsOption => {
    const pts = spreadPoints.value
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      xAxis: {
        type: 'category',
        data: pts.map((p) => p.period),
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: pts.length > 45 ? 30 : 0 },
      },
      yAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: '#eef0f3' } },
      },
      series: [
        {
          name: '日前-实时价差',
          type: 'bar',
          // 每根柱子按正负着色：正=日前更高（红），负=实时更高（绿）
          data: pts.map((p) => ({
            value: p.value,
            itemStyle: { color: p.value >= 0 ? CHART_COLORS.red : CHART_COLORS.green },
          })),
        },
      ],
    }
  },
  [spreadPoints],
)
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>数据加载失败</strong>
    <span>　{{ error }}</span>
    <button type="button" @click="reload">重试</button>
  </div>

  <div class="toolbar">
    <div class="radio-group">
      <button
        v-for="t in tabs"
        :key="t.v"
        type="button"
        :class="{ on: tab === t.v }"
        @click="tab = t.v"
      >
        {{ t.t }}
      </button>
    </div>
    <span v-if="tab === 'daily'" class="field">回看区间</span>
    <RangePicker
      v-if="tab === 'daily'"
      class="range-picker"
      :model-value="range"
      value-format="YYYY-MM-DD"
      format="YYYY-MM-DD"
      :allow-clear="false"
      :disabled-date="disabledFutureDate"
      :placeholder="['开始日期', '结束日期']"
      @change="onRangeChange"
    />
    <span v-if="tab === 'daily' && rangeHint" class="range-hint">{{ rangeHint }}</span>
    <span class="spacer" />
    <span v-if="tab === 'daily'" class="meta">日前最新运行日 {{ latestRunDate || '—' }} · {{ coverageNote }}</span>
  </div>

  <!-- ===== 日频 ===== -->
  <template v-if="tab === 'daily'">
    <div v-if="dailyStateNote" class="notice" :class="dailyTierNow === 'stop' || dailyTierNow === 'never' ? 'is-alert' : ''">
      {{ dailyStateNote }}
    </div>
    <div v-if="dailyEmpty && !dailyStateNote" class="notice">该区间无数据：所选起止日内没有已落库的日频指标。</div>
    <div class="kpi-note">KPI 固定为全局最新（不受下方回看区间影响）；图/表按所选区间取数。</div>
    <div class="kpis">
      <KpiCard
        label="日前加权均价"
        :value="fmtNum(aheadAvgKpi?.latest.value)"
        :unit="priceUnit"
        :hint="`环比 ${fmtPct(aheadAvgKpi?.momPct)} · 同比 ${fmtPct(aheadAvgKpi?.yoyPct)}`"
      />
      <KpiCard
        label="实时加权均价"
        :value="fmtNum(realAvgKpi?.latest.value)"
        :unit="priceUnit"
        :hint="`运行日 ${realAvgKpi?.latest.period || '—'}（实时 T+2 发布）`"
      />
      <KpiCard
        label="日前峰谷价差"
        :value="fmtNum(aheadSpreadKpi)"
        :unit="priceUnit"
        :hint="`区间最高 ${fmtNum(aheadAvgKpi?.peak)} / 最低 ${fmtNum(aheadAvgKpi?.valley)}`"
      />
      <KpiCard
        label="日前-实时价差"
        :value="fmtNum(kpiBoard?.spreadMean)"
        :unit="priceUnit"
        hint="区间均值，正=日前更高"
      />
      <KpiCard
        label="日前成交电量·发电侧"
        :value="fmtNum(aheadVolGenKpi?.latest.value, 0)"
        :unit="volUnit"
        :hint="`环比 ${fmtPct(aheadVolGenKpi?.momPct)}`"
      />
      <KpiCard
        label="日前申报均价"
        :value="fmtNum(declPriceKpi?.latest.value)"
        :unit="priceUnit"
        :hint="`环比 ${fmtPct(declPriceKpi?.momPct)} · 仅日前披露`"
      />
      <KpiCard
        label="日前成交电量·用电侧"
        :value="fmtNum(aheadVolConKpi?.latest.value, 0)"
        :unit="volUnit"
        :hint="`环比 ${fmtPct(aheadVolConKpi?.momPct)}`"
      />
    </div>

    <div class="grid">
      <PanelBox
        title="日前 / 实时加权均价"
        sub="发电侧 · 元/兆瓦时"
        :span="8"
        :loading="loading"
        :foot="`数据来源：${board?.source || '官方披露接口'} · 运行日 ${board?.from || '—'} ~ ${board?.to || '—'}`"
      >
        <div ref="priceEl" class="chart" />
      </PanelBox>

      <PanelBox
        title="日前-实时价差"
        sub="发电侧加权均价之差，正=日前更高"
        :span="4"
        :loading="loading"
        foot="口径：同一运行日的日前加权均价 − 实时加权均价；实时按 T+2 发布，最近两天没有点属正常"
      >
        <div ref="spreadEl" class="chart" />
      </PanelBox>

      <PanelBox
        title="日前成交电量"
        sub="发电侧 / 用电侧 · MWh"
        :span="12"
        :loading="loading"
        foot="口径：源站「总成交电量」，日频按自然日汇总"
      >
        <div ref="volEl" class="chart" />
      </PanelBox>

      <PanelBox
        title="日频指标明细"
        sub="最新一期的值 + 区间环比 / 同比 / 峰谷"
        :span="12"
        :loading="loading"
        :foot="`申报均价仅日前披露；实时只披露发电侧的成交电量与加权均价（源站口径，不是页面省略）`"
      >
        <div v-for="g in dailyGroups" :key="g.dataset" class="table-wrap">
          <div class="table-title">{{ g.label }}</div>
          <table>
            <thead>
              <tr>
                <th>指标</th>
                <th>单位</th>
                <th class="num">最新值</th>
                <th>期次</th>
                <th class="num">环比</th>
                <th class="num">同比</th>
                <th class="num">区间最高</th>
                <th class="num">区间最低</th>
                <th class="num">峰谷价差</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in g.series" :key="`${s.dataset}-${s.scope}-${s.metric}`">
                <td>{{ seriesLabel(s) }}</td>
                <td class="dim">{{ s.unit || '—' }}</td>
                <td class="num">{{ fmtNum(s.latest.value) }}</td>
                <td class="dim">{{ s.latest.period }}</td>
                <td class="num" :class="dirClass(s.momPct)">{{ fmtPct(s.momPct) }}</td>
                <td class="num" :class="dirClass(s.yoyPct)">{{ fmtPct(s.yoyPct) }}</td>
                <td class="num">{{ fmtNum(s.peak) }}</td>
                <td class="num">{{ fmtNum(s.valley) }}</td>
                <td class="num">{{ fmtNum(s.spread) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </PanelBox>
    </div>
  </template>

  <!-- ===== 月频 / 年频 ===== -->
  <template v-else>
    <div class="grid">
      <PanelBox
        title="中长期合约成交均价"
        sub="月度 · 净合约 / 累计合约 · 元/兆瓦时"
        :span="6"
        :loading="loading"
        foot="口径：源站「中长期月度交易成交均价及电量」，净合约=当月新增，累计=当年累计"
      >
        <div ref="mlPriceEl" class="chart" />
      </PanelBox>

      <PanelBox
        title="中长期合约成交电量"
        sub="月度 · 净合约 · MWh"
        :span="6"
        :loading="loading"
        foot="口径：净合约成交电量（当月新增签约量）"
      >
        <div ref="mlVolEl" class="chart" />
      </PanelBox>

      <PanelBox
        title="绿电月度成交"
        sub="按交易序列汇总 · 电量求和 / 均价按电量加权"
        :span="12"
        :loading="loading"
        foot="口径：源站「绿电月度交易」按交易序列逐条披露，本图按月份汇总；均价是电量加权，不是各序列简单平均"
      >
        <div ref="greenEl" class="chart" />
      </PanelBox>

      <PanelBox
        title="月频 / 年频指标明细"
        sub="最新一期的值 + 环比 / 同比"
        :span="12"
        :loading="loading"
        foot="HHI 与 Top-m 是年度口径；绿电按交易序列逐条入库，明细行即单条序列"
      >
        <div v-if="periodicGroups.length === 0" class="pending">暂无月频/年频数据</div>
        <div v-for="g in periodicGroups" :key="g.dataset" class="table-wrap">
          <div class="table-title">{{ g.label }}</div>
          <table>
            <thead>
              <tr>
                <th>指标</th>
                <th>单位</th>
                <th class="num">最新值</th>
                <th>期次</th>
                <th class="num">环比</th>
                <th class="num">同比</th>
                <th class="num">区间最高</th>
                <th class="num">区间最低</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in g.series" :key="`${s.dataset}-${s.scope}-${s.metric}`">
                <td>{{ seriesLabel(s) }}</td>
                <td class="dim">{{ s.unit || '—' }}</td>
                <td class="num">{{ fmtNum(s.latest.value) }}</td>
                <td class="dim">{{ periodLabel(s.latest.period, s.grain) }}</td>
                <td class="num" :class="dirClass(s.momPct)">{{ fmtPct(s.momPct) }}</td>
                <td class="num" :class="dirClass(s.yoyPct)">{{ fmtPct(s.yoyPct) }}</td>
                <td class="num">{{ fmtNum(s.peak) }}</td>
                <td class="num">{{ fmtNum(s.valley) }}</td>
              </tr>
            </tbody>
          </table>
          <p v-if="g.total > g.series.length" class="more">
            共 {{ g.total }} 条，此处只列前 {{ g.series.length }} 条（逐条明细按交易序列入库，
            汇总看上方图表）。
          </p>
        </div>
      </PanelBox>
    </div>
  </template>

  <p class="source">
    数据来源：{{ board?.source || '广东电力交易中心披露接口' }} ·
    口径与更新方式见 tools/gdpm/README.md · 最近一次入库 {{ board?.updatedAt || '—' }}
  </p>
</template>

<style scoped>
.toolbar {
  display: flex;
  align-items: center;
  gap: var(--s3);
  flex-wrap: wrap;
  margin-bottom: var(--s4);
}
.radio-group { display: inline-flex; padding: 2px; border-radius: var(--r-sm); background: var(--bg-3); }
.radio-group button {
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-xs);
  background: transparent;
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.radio-group button.on { background: var(--bg-2); color: var(--brand-ink); font-weight: 600; }
.field { font-size: var(--fs-13); color: var(--text-3); }
.range-picker { width: 260px; }
.range-hint { font-size: var(--fs-12); color: var(--text-3); }
.spacer { flex: 1; }
.meta { font-size: var(--fs-12); color: var(--text-3); }
.kpi-note { margin-bottom: var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.kpis {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: var(--s4);
  margin-bottom: var(--s4);
}
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.chart { height: 300px; }
.table-wrap { overflow-x: auto; margin-bottom: var(--s4); }
.table-title { font-size: var(--fs-13); font-weight: 600; margin-bottom: var(--s2); color: var(--text-2); }
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th, td { padding: var(--s2) var(--s3); text-align: left; border-bottom: 1px solid var(--divider); }
th { font-weight: 600; color: var(--text-3); white-space: nowrap; }
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
.dim { color: var(--text-3); }
td.up { color: var(--red-ink); }
td.down { color: var(--green-ink); }
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
.notice:not(.error):not(.is-alert) {
  border-left-color: var(--border);
  background: var(--bg-3);
  color: var(--text-2);
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
.pending { padding: var(--s4); color: var(--text-3); font-size: var(--fs-13); }
.more { margin: var(--s2) 0 0; font-size: var(--fs-12); color: var(--text-3); }
.source { margin-top: var(--s4); font-size: var(--fs-12); color: var(--text-3); }
@media (max-width: 1280px) {
  .kpis { grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); }
  .grid { grid-template-columns: repeat(6, 1fr); }
}
</style>
