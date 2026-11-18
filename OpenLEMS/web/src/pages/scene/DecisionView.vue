<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import SourceBadge from '@/components/SourceBadge.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchBacktest, fetchEffectSummary, type EffectBacktest, type EffectSummary } from '@/api/effect'
import { fetchLoadForecast, fetchPriceForecast, type LoadForecast, type PriceForecast } from '@/api/forecast'
import { fetchGdMetric, fetchSimilarDays, type DayCurve, type SimilarDaysResult } from '@/api/gdmetric'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'
import { useAppStore } from '@/stores/app'

/**
 * 场景 · 决策支持。
 *
 * 内容来源：
 *   实测效果 KPI + 算法回测 → effect-overlay 迁入（/api/effect/*）
 *   负荷预测 / 电价预测     → TimesFM（/api/forecast/*）
 *   历史相似日             → /api/gd-metric/similar-days（96 点形态相关，见后端 similar.go）
 * 都走同一套成本模型/同一个预测服务，页面不另做估算。
 *
 * 「日前市场竞争空间」仍无数据源，底部明确列出，不造数。
 */
type Preset = 'week' | 'month' | 'custom'

/** 最多叠加 3 天：再多就分不清那条线是谁了（设计稿 §5.3） */
const MAX_PICKS = 3

const PRESETS: { v: Preset; t: string }[] = [
  { v: 'week', t: '近 7 天' },
  { v: 'month', t: '近 30 天' },
  { v: 'custom', t: '自定义' },
]

const range = reactive<{ preset: Preset; start: string; end: string }>({
  preset: 'week',
  start: '',
  end: '',
})

const summary = ref<EffectSummary | null>(null)
const backtest = ref<EffectBacktest | null>(null)
const load = ref<LoadForecast | null>(null)
const price = ref<PriceForecast | null>(null)
/** 小时粒度：目标日 24 点预测。默认今天；数据滞后 T-6，锚点会早于目标日（见面板文案）。 */
const priceHour = ref<PriceForecast | null>(null)
const hourDate = ref(todayStr())

const app = useAppStore()

const loading = reactive({ summary: false, backtest: false, load: false, price: false, priceHour: false })
const errors = reactive<Record<string, string>>({})

function todayStr(): string {
  const d = new Date()
  return fmt(d)
}

function fmt(d: Date): string {
  const loc = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
  return loc.toISOString().slice(0, 10)
}

function applyPreset() {
  const end = new Date()
  const days = range.preset === 'week' ? 6 : 29
  range.start = fmt(new Date(end.getTime() - days * 86400_000))
  range.end = fmt(end)
}

const dates = computed(() => ({ start: range.start, end: range.end }))

async function loadSummary() {
  loading.summary = true
  errors.summary = ''
  try {
    summary.value = await fetchEffectSummary(dates.value)
  } catch (err) {
    summary.value = null
    errors.summary = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.summary = false
  }
}

async function loadBacktest() {
  loading.backtest = true
  errors.backtest = ''
  try {
    backtest.value = await fetchBacktest(dates.value)
  } catch (err) {
    backtest.value = null
    errors.backtest = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.backtest = false
  }
}

async function loadForecast(force = false) {
  loading.load = true
  loading.price = true
  loading.priceHour = true
  errors.load = ''
  errors.price = ''
  errors.priceHour = ''
  const [l, p, h] = await Promise.allSettled([
    fetchLoadForecast({ historyHours: 24, horizon: 96, forceRefresh: force }),
    fetchPriceForecast({ source: 'spot_metric', historyDays: 60, horizonDays: 5, forceRefresh: force }),
    // 目标日默认今天：锚点会落在数据最新的完整日（T-6 左右），两者不同是正常的
    fetchPriceForecast({
      source: 'home24h',
      grain: 'hour',
      targetDate: hourDate.value,
      historyDays: 14,
      forceRefresh: force,
    }),
  ])
  if (l.status === 'fulfilled') load.value = l.value
  else {
    load.value = null
    errors.load = l.reason instanceof Error ? l.reason.message : '负荷预测失败'
  }
  if (p.status === 'fulfilled') price.value = p.value
  else {
    price.value = null
    errors.price = p.reason instanceof Error ? p.reason.message : '电价预测失败'
  }
  if (h.status === 'fulfilled') priceHour.value = h.value
  else {
    priceHour.value = null
    errors.priceHour = h.reason instanceof Error ? h.reason.message : '小时电价预测失败'
  }
  loading.load = false
  loading.price = false
  loading.priceHour = false
}

/** 只重取小时这一路：改目标日不该连带重算日均与负荷 */
async function loadHour(force = false) {
  loading.priceHour = true
  errors.priceHour = ''
  try {
    priceHour.value = await fetchPriceForecast({
      source: 'home24h',
      grain: 'hour',
      targetDate: hourDate.value,
      historyDays: 14,
      forceRefresh: force,
    })
  } catch (err) {
    priceHour.value = null
    errors.priceHour = err instanceof Error ? err.message : '小时电价预测失败'
  } finally {
    loading.priceHour = false
  }
}

function refreshAll(force = false) {
  void Promise.all([loadSummary(), loadBacktest(), loadForecast(force), loadSimilar()])
}

function onPreset(p: Preset) {
  range.preset = p
  if (p !== 'custom') {
    applyPreset()
    void Promise.all([loadSummary(), loadBacktest()])
  }
}

function money(v: number | undefined): string {
  return v === undefined || v === null ? '—' : v.toFixed(0)
}

function fmtMs(ms: number): string {
  return new Date(ms).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

/**
 * 实际配对窗口。
 *
 * 效果是按「关口 ∩ 储能」的**时间戳交集**算的，而请求区间只是查询范围 —— 刚接入的设备
 * 历史很短时，窗口会远小于请求区间（实测：请求「 09-08 ~ 10-08」，窗口只有 10-08 那天，
 * 因为关口电表是 10-08 才接入的）。不把实际窗口显示出来，「近 7 天省了 X 元」会被读成 7 天的数。
 */
const effectWindow = computed(() => {
  const s = summary.value
  if (!s || !s.firstTs || !s.lastTs) return ''
  return `${fmtMs(s.firstTs)} ~ ${fmtMs(s.lastTs)}`
})

/** 窗口为什么短：谁的历史更晚就从谁说起 */
const windowShortReason = computed(() => {
  const s = summary.value
  if (!s || !s.firstTs) return ''
  const reqStart = Date.parse((s.start || '').replace(' ', 'T'))
  if (!Number.isFinite(reqStart)) return ''
  const lag = (ms?: number) => (ms && ms - reqStart > 3600_000 ? ms : 0)
  if (lag(s.gridFrom)) return `关口设备历史自 ${fmtMs(lag(s.gridFrom) as number)} 起`
  if (lag(s.essFrom)) return `储能设备历史自 ${fmtMs(lag(s.essFrom) as number)} 起`
  return ''
})

/* ---------- 历史相似日 ---------- */

const similar = ref<SimilarDaysResult | null>(null)
const similarLoading = ref(false)
const similarError = ref('')
/** 勾选叠加的历史日（最多 MAX_PICKS） */
const picked = ref<string[]>([])
/** date → 当天 96 点曲线（含目标日与勾选日），懒加载 */
const curveOf = ref<Record<string, DayCurve | null>>({})
const overlayPrice = ref<'dayAhead' | 'realtime'>('dayAhead')

async function ensureCurve(date: string) {
  if (date in curveOf.value) return
  try {
    const snap = await fetchGdMetric({ date })
    curveOf.value = { ...curveOf.value, [date]: snap.curve ?? null }
  } catch {
    curveOf.value = { ...curveOf.value, [date]: null }
  }
}

async function loadSimilar() {
  similarLoading.value = true
  similarError.value = ''
  try {
    // 不给日期就让后端用最新**有实际值且 96 点齐全**的运行日
    // （最新运行日往往只有电查查预测占位，不能当历史观测比对）
    const r = await fetchSimilarDays({ date: app.runDate || undefined, top: 8 })
    similar.value = r
    picked.value = []
    curveOf.value = {}
    await ensureCurve(r.targetDate)
  } catch (err) {
    similar.value = null
    similarError.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    similarLoading.value = false
  }
}

function togglePick(date: string) {
  const i = picked.value.indexOf(date)
  if (i >= 0) {
    picked.value.splice(i, 1)
    return
  }
  if (picked.value.length >= MAX_PICKS) picked.value.shift()
  picked.value.push(date)
  void ensureCurve(date)
}

function clearPicks() {
  picked.value = []
}

const targetCurve = computed(() => {
  const d = similar.value?.targetDate
  return d ? (curveOf.value[d] ?? null) : null
})

const similarEl = ref<HTMLElement>()
useECharts(
  similarEl,
  (): EChartsOption => {
    const target = targetCurve.value
    const axis = target?.[overlayPrice.value].map((s) => s.time) ?? []
    const series: EChartsOption['series'] = []
    if (target) {
      series.push({
        name: `目标日 ${similar.value?.targetDate ?? ''}`,
        type: 'line',
        showSymbol: false,
        lineStyle: { width: 2.5 },
        itemStyle: { color: CHART_COLORS.brand },
        data: target[overlayPrice.value].map((s) => s.value),
      })
    }
    picked.value.forEach((date, i) => {
      const curve = curveOf.value[date]
      if (!curve) return
      series.push({
        name: date,
        type: 'line',
        showSymbol: false,
        lineStyle: { type: 'dashed', width: 1.5, opacity: 0.85 },
        itemStyle: { color: [CHART_COLORS.blue, CHART_COLORS.cyan, CHART_COLORS.purple][i % 3] },
        data: curve[overlayPrice.value].map((s) => s.value),
      })
    })
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 44 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v}`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: axis,
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          interval: (_i: number, value: string) => /^(00:00|04:00|08:00|12:00|16:00|20:00|24:00)$/.test(value),
        },
      },
      yAxis: {
        type: 'value',
        name: '元/MWh',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series,
    }
  },
  [similar, picked, curveOf, overlayPrice],
)

/* ---------- 负荷预测：实际 vs 预测 + 分位带 ---------- */
const loadEl = ref<HTMLElement>()
useECharts(loadEl, (): EChartsOption => {
  const f = load.value
  const label = (ms: number) =>
    new Date(ms).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  const xs = [...(f?.historyTimes ?? []).map(label), ...(f?.forecastTimes ?? []).map(label)]
  const pad = new Array(f?.historyTimes.length ?? 0).fill(null)
  const q = f?.forecastQuantiles ?? {}
  const band = (key: string) => [...pad, ...(q[key] ?? [])]
  return {
    grid: { left: 56, right: 20, top: 32, bottom: 48 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: {
      type: 'category',
      data: xs,
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: 30 },
    },
    yAxis: {
      type: 'value',
      name: 'kW',
      nameTextStyle: { fontSize: 12 },
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: '#eef0f3' } },
    },
    series: [
      // 分位带用两条虚线表示（0.1 / 0.9）；区间越宽说明模型越不确定
      { name: '预测 10% 分位', type: 'line', showSymbol: false, lineStyle: { type: 'dotted', width: 1 }, itemStyle: { color: CHART_COLORS.gray }, data: band('0.1') },
      { name: '预测 90% 分位', type: 'line', showSymbol: false, lineStyle: { type: 'dotted', width: 1 }, itemStyle: { color: CHART_COLORS.gray }, data: band('0.9') },
      { name: '实测', type: 'line', smooth: true, showSymbol: false, lineStyle: { width: 2 }, itemStyle: { color: CHART_COLORS.blue }, data: [...(f?.historyValues ?? []), ...pad] },
      { name: '点预测', type: 'line', smooth: true, showSymbol: false, lineStyle: { type: 'dashed' }, itemStyle: { color: CHART_COLORS.brand }, data: [...pad, ...(f?.forecastPoint ?? [])] },
    ],
  }
}, [load])

/* ---------- 电价预测：日均 + 分位 ---------- */
const priceEl = ref<HTMLElement>()
useECharts(priceEl, (): EChartsOption => {
  const f = price.value
  const dates = [...(f?.historyDates ?? []), ...(f?.forecast ?? []).map((p) => p.runDate)]
  const histPadTail = new Array(f?.forecast.length ?? 0).fill(null)
  const fcPadHead = new Array(f?.historyDates.length ?? 0).fill(null)
  const q = (key: string) => [...fcPadHead, ...(f?.forecast ?? []).map((p) => p.quantiles?.[key] ?? null)]
  return {
    grid: { left: 56, right: 20, top: 32, bottom: 40 },
    tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} ${f?.unit ?? ''}`) },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: { type: 'category', data: dates, axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: 30 } },
    yAxis: {
      type: 'value',
      name: f?.unit ?? '',
      nameTextStyle: { fontSize: 12 },
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: '#eef0f3' } },
    },
    series: [
      { name: '历史（官方直采日均）', type: 'line', showSymbol: false, itemStyle: { color: CHART_COLORS.blue }, data: [...(f?.historyValues ?? []), ...histPadTail] },
      { name: '预测 10%', type: 'line', showSymbol: false, lineStyle: { type: 'dotted', width: 1 }, itemStyle: { color: CHART_COLORS.gray }, data: q('0.1') },
      { name: '预测 90%', type: 'line', showSymbol: false, lineStyle: { type: 'dotted', width: 1 }, itemStyle: { color: CHART_COLORS.gray }, data: q('0.9') },
      { name: '预测中位', type: 'line', showSymbol: true, lineStyle: { type: 'dashed' }, itemStyle: { color: CHART_COLORS.brand }, data: [...fcPadHead, ...(f?.forecast ?? []).map((p) => p.point)] },
    ],
  }
}, [price])

/* ---------- 小时粒度：目标日 24 点 + 实际值 ---------- */
const hourEl = ref<HTMLElement>()
useECharts(
  hourEl,
  (): EChartsOption => {
    const f = priceHour.value
    const atTimes = (f?.forecast ?? []).map((p) => p.atTime ?? p.runDate)
    const actual = f?.actual ?? []
    const series: EChartsOption['series'] = []
    if (actual.length) {
      series.push({
        name: '实际（官网主页 24 点）',
        type: 'line',
        showSymbol: false,
        lineStyle: { width: 2 },
        itemStyle: { color: CHART_COLORS.gray },
        data: actual,
      })
    }
    series.push(
      {
        name: '预测 10%',
        type: 'line',
        showSymbol: false,
        lineStyle: { type: 'dotted', width: 1 },
        itemStyle: { color: CHART_COLORS.gray },
        data: (f?.forecast ?? []).map((p) => p.quantiles?.['0.1'] ?? null),
      },
      {
        name: '预测 90%',
        type: 'line',
        showSymbol: false,
        lineStyle: { type: 'dotted', width: 1 },
        itemStyle: { color: CHART_COLORS.gray },
        data: (f?.forecast ?? []).map((p) => p.quantiles?.['0.9'] ?? null),
      },
      {
        name: '预测中位',
        type: 'line',
        showSymbol: true,
        symbolSize: 5,
        lineStyle: { type: 'dashed', width: 2 },
        itemStyle: { color: CHART_COLORS.brand },
        data: (f?.forecast ?? []).map((p) => p.point),
      },
    )
    return {
      grid: { left: 56, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} ${f?.unit ?? ''}`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: atTimes,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, interval: 2 },
      },
      yAxis: {
        type: 'value',
        name: f?.unit ?? '',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: '#eef0f3' } },
      },
      series,
    }
  },
  [priceHour],
)

onMounted(() => {
  applyPreset()
  refreshAll(false)
})

// 运行日变了要重算相似日（目标日就是它）
watch(
  () => app.runDate,
  () => void loadSimilar(),
)
</script>

<template>
  <div class="grid">
    <PanelBox title="区间与来源" :span="12" :foot="false">
      <div class="toolbar">
        <span class="radio-group">
          <button
            v-for="p in PRESETS"
            :key="p.v"
            type="button"
            :class="{ on: range.preset === p.v }"
            @click="onPreset(p.v)"
          >
            {{ p.t }}
          </button>
        </span>
        <label class="field">起<input v-model="range.start" type="date" /></label>
        <span class="muted">→</span>
        <label class="field">止<input v-model="range.end" type="date" /></label>
        <span class="spacer" />
        <button class="btn" type="button" :disabled="loading.summary || loading.backtest" @click="refreshAll(false)">
          查询
        </button>
        <button class="btn primary" type="button" :disabled="loading.load" @click="refreshAll(true)">
          刷新预测（跳过缓存）
        </button>
      </div>
      <div class="hint">
        效果与回测用同一套成本模型（峰谷套利 + 需量节省）；预测来自 TimesFM。
        预测服务是 CPU 推理，后端缓存 5 分钟 —— 只有点「刷新预测」才会穿透缓存。
      </div>
    </PanelBox>

    <PanelBox
      title="实测效果"
      sub="峰谷套利 + 需量节省"
      :span="12"
      :loading="loading.summary"
      :foot="summary ? `区间 ${summary.start} ~ ${summary.end} · 采样 ${summary.points} 点 · 生效算法 ${summary.algorithm || '—'} · 需量电价 ${summary.demandPrice} 元/kW` : '数据来源：/api/effect/summary'"
    >
      <div v-if="errors.summary" class="empty">{{ errors.summary }}</div>
      <div v-else class="kpis">
        <KpiCard label="总节省" :value="money(summary?.totalSaving)" unit="元" hint="套利 + 需量" />
        <KpiCard label="套利净收益" :value="money(summary?.arbitrageProfit)" unit="元" hint="放电收益 − 充电电费" />
        <KpiCard label="需量节省" :value="money(summary?.demandSaving)" unit="元" hint="基线需量 − 实际需量" />
        <KpiCard label="充电量" :value="money(summary?.chargeEnergy)" unit="kWh" :hint="`电费 ${money(summary?.chargeCost)} 元`" />
        <KpiCard label="放电量" :value="money(summary?.dischargeEnergy)" unit="kWh" :hint="`收益 ${money(summary?.dischargeRevenue)} 元`" />
        <KpiCard
          label="最大需量"
          :value="money(summary?.actualMaxDemand)"
          unit="kW"
          :hint="`无储能基线 ${money(summary?.baselineMaxDemand)} kW`"
        />
      </div>
      <div v-if="effectWindow" class="hint">
        实际对比窗口 <strong>{{ effectWindow }}</strong>（{{ summary?.points }} 点）
        <template v-if="windowShortReason">
          —— {{ windowShortReason }}；效果按「关口 ∩ 储能」的时间戳交集配对，
          窗口比请求区间（{{ summary?.start }} ~ {{ summary?.end }}）短是正常的，
          上面的金额<strong>只代表这段窗口</strong>。
        </template>
      </div>
    </PanelBox>

    <PanelBox
      title="日前电价预测"
      sub="TimesFM · 日均序列"
      :span="12"
      :loading="loading.price"
      :foot="price ? `模型 ${price.model} · 历史 ${price.contextPoints} 点 · 区间 ${price.from} ~ ${price.to} · 缺数据 ${price.missingDays} 天 · 耗时 ${price.timeMs.toFixed(0)}ms` : '数据来源：官方直采日均成交价（gd_spot_metric）'"
    >
      <template #actions>
        <SourceBadge tone="blue" label="官网·披露接口" />
      </template>
      <div v-if="errors.price" class="empty">{{ errors.price }}</div>
      <template v-else>
        <div class="warn">
          本块出的是<strong>日均</strong>序列（官方直采日均成交价）。15 分钟（96 点）分时预测
          已于 2026-11-13 停用：它依赖的第三方分时通道止于 2026-10-02，官方匿名接口没有 96 点。
          两者<strong>不是同一份数据</strong>，不要互相换算。
        </div>
        <div ref="priceEl" class="chart" />
      </template>
    </PanelBox>

    <PanelBox
      title="日前电价预测 · 小时（24 点）"
      :sub="priceHour ? `目标日 ${priceHour.targetDate} · 锚点 ${priceHour.to}` : 'TimesFM · 目标日 24 个整点'"
      :span="12"
      :loading="loading.priceHour"
      :foot="priceHour ? `锚点 D=${priceHour.to} · 历史 ${priceHour.contextPoints} 点 · 模型 ${priceHour.model} · 步长 ${priceHour.stepMinutes} 分钟 · 耗时 ${priceHour.timeMs.toFixed(0)}ms` : '数据来源：官网主页 24 点分时（gd_home_24h）'"
    >
      <template #actions>
        <SourceBadge tone="blue" label="官网·24点" />
      </template>
      <div class="toolbar">
        <label class="field">
          目标日
          <input v-model="hourDate" type="date" :max="todayStr()" @change="loadHour(false)" />
        </label>
        <span class="spacer" />
        <button class="btn" type="button" :disabled="loading.priceHour" @click="loadHour(true)">
          刷新本图（跳过缓存）
        </button>
      </div>
      <div v-if="errors.priceHour" class="empty">{{ errors.priceHour }}</div>
      <template v-else>
        <div class="warn">
          24 点数据<strong>滞后约 T-6</strong>：锚点 <strong>{{ priceHour?.to || '—' }}</strong>
          与目标日 <strong>{{ priceHour?.targetDate || hourDate }}</strong>
          不同是正常的 —— 预测从锚点 D 滚动到目标日，不是“明日电价”。
          口径也与上方日均图不同（主页日前价 ≠ 发电侧日前加权平均电价），
          <strong>两图不可对齐、不可加总、不可互相换算</strong>。
        </div>
        <div ref="hourEl" class="chart" />
      </template>
    </PanelBox>

    <PanelBox
      title="历史相似日"
      :sub="similar ? `目标日 ${similar.targetDate} · 候选 ${similar.candidates} 天` : ''"
      :span="4"
      :loading="similarLoading"
      :foot="similar ? `${similar.method}｜${similar.formula}｜只统计实际值日（电查查预测占位不算）` : '数据来源：96 点序列（第三方通道已停更）'"
    >
      <template #actions>
        <SourceBadge tone="gray" label="第三方·已停用" />
      </template>
      <div v-if="similarError" class="empty">{{ similarError }}</div>
      <template v-else-if="similar">
        <div class="muted">点一行叠加到右图（最多 {{ MAX_PICKS }} 天）。</div>
        <ul class="similar-list">
          <li
            v-for="d in similar.days"
            :key="d.date"
            :class="{ picked: picked.includes(d.date) }"
            @click="togglePick(d.date)"
          >
            <span class="date">{{ d.date }}</span>
            <span class="bar"><i :style="{ width: `${Math.max(2, d.similarity)}%` }" /></span>
            <span class="pct">{{ d.similarity.toFixed(1) }}%</span>
            <span class="meta">{{ d.priceAvg.toFixed(0) }} 元/MWh · 峰 {{ d.loadMax.toFixed(0) }} MW</span>
          </li>
        </ul>
        <div class="muted">
          相似度按 96 点<strong>形态</strong>算（z-score 后加权皮尔逊相关），不是价位接近；
          只做参考，不产出预测值。区间 {{ similar.from }} ~ {{ similar.to }}。
        </div>
      </template>
    </PanelBox>

    <PanelBox
      title="历史相似日情况分析"
      :sub="overlayPrice === 'dayAhead' ? '日前电价 96 点' : '实时电价 96 点'"
      :span="8"
      :loading="similarLoading"
      foot="目标日实线加粗；勾选的相似日为虚线（先切「日前/实时」再叠加）"
    >
      <template #actions>
        <SourceBadge tone="gray" label="第三方·已停用" />
      </template>
      <template v-if="similar">
        <div class="toolbar">
          <label class="field">
            价格
            <select v-model="overlayPrice">
              <option value="dayAhead">日前</option>
              <option value="realtime">实时</option>
            </select>
          </label>
          <span class="spacer" />
          <button v-if="picked.length" class="btn" type="button" @click="clearPicks">
            清空叠加（{{ picked.length }}）
          </button>
        </div>
        <div v-if="targetCurve" ref="similarEl" class="chart" />
        <div v-else class="empty">
          目标日 {{ similar.targetDate }} 没有 96 点曲线数据（CSV 只覆盖有数据的运行日）。
        </div>
      </template>
      <div v-else class="empty">等相似日结果。</div>
    </PanelBox>

    <PanelBox
      title="负荷预测"
      sub="实测 vs 预测 + 10%~90% 分位"
      :span="12"
      :loading="loading.load"
      :foot="load ? `设备 ${load.deviceId} · ${load.stepMinutes} 分钟/点 · 模型 ${load.model} · 送入 ${load.lookbackPoints} 点${load.inputTruncated ? '（已截断）' : ''} · 耗时 ${load.timeMs.toFixed(0)}ms` : '数据来源：/api/forecast/load'"
    >
      <div v-if="errors.load" class="empty">{{ errors.load }}</div>
      <div v-else ref="loadEl" class="chart" />
    </PanelBox>

    <PanelBox
      title="算法回测对比"
      sub="同一段历史负荷上跑各算法，同一成本模型结算"
      :span="12"
      :loading="loading.backtest"
      :foot="backtest ? `额定 ${backtest.ratedPowerKw} kW / ${backtest.capacityKwh} kWh · 需量目标 ${backtest.demandTarget || '未启用'}` : '数据来源：/api/effect/backtest'"
    >
      <div v-if="errors.backtest" class="empty">{{ errors.backtest }}</div>
      <table v-else-if="backtest?.results?.length">
        <thead>
          <tr>
            <th>算法</th>
            <th class="num">套利净收益(元)</th>
            <th class="num">需量节省(元)</th>
            <th class="num">总节省(元)</th>
            <th class="num">充/放电量(kWh)</th>
            <th class="num">实际最大需量(kW)</th>
            <th class="num">终止 SOC</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in backtest.results" :key="r.algorithm">
            <td>
              {{ r.algorithm }}
              <StatusTag v-if="r.algorithm === 'idle'" tone="gray">基线</StatusTag>
            </td>
            <td class="num">{{ r.arbitrageProfit.toFixed(2) }}</td>
            <td class="num">{{ r.demandSaving.toFixed(2) }}</td>
            <td class="num"><strong>{{ r.totalSaving.toFixed(2) }}</strong></td>
            <td class="num">{{ r.chargeEnergy.toFixed(0) }} / {{ r.dischargeEnergy.toFixed(0) }}</td>
            <td class="num">{{ r.actualMaxDemand.toFixed(0) }}</td>
            <td class="num">{{ r.finalSocPct.toFixed(0) }}%</td>
          </tr>
        </tbody>
      </table>
      <div v-else class="empty">该区间没有可回测的数据（需要关口与储能的实测序列）。</div>
    </PanelBox>

    <PanelBox title="待数据源的面板" :span="12" foot="设计稿 §5.3 里列出的其余一块">
      <div class="pending-list">
        <div class="pending">
          <StatusTag tone="brand">待数据源</StatusTag>
          <span>
            日前市场竞争空间：需要「火电最小出力 / 固定出力 / 风电 / 光伏 / 各类可调」分档数据，
            日报未提取。
          </span>
        </div>
      </div>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.toolbar { display: flex; align-items: center; gap: var(--s3); flex-wrap: wrap; }
.spacer { flex: 1; }
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
.field { display: inline-flex; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
.field input {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.hint { margin-top: var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.warn {
  margin-bottom: var(--s3);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-12);
}
.kpis { display: grid; grid-template-columns: repeat(6, 1fr); gap: var(--s4); }
.chart { height: 320px; }
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--text-3);
  font-size: var(--fs-12);
  font-weight: 500;
  text-align: left;
}
td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); font-variant-numeric: tabular-nums; }
th.num, td.num { text-align: right; }
.pending-list { display: flex; flex-direction: column; gap: var(--s3); }
.similar-list { list-style: none; margin: var(--s2) 0; padding: 0; }
.similar-list li {
  display: grid;
  grid-template-columns: 88px 1fr 52px;
  align-items: center;
  gap: var(--s2);
  padding: var(--s2) var(--s1);
  border-radius: var(--r-sm);
  cursor: pointer;
}
.similar-list li:hover { background: var(--bg-2); }
.similar-list li.picked { background: var(--brand-soft); }
.similar-list .date { font-size: var(--fs-13); font-variant-numeric: tabular-nums; }
.similar-list .bar { height: 8px; border-radius: 4px; background: var(--bg-3); overflow: hidden; }
.similar-list .bar i { display: block; height: 100%; background: var(--brand); }
.similar-list .pct { text-align: right; font-size: var(--fs-13); font-variant-numeric: tabular-nums; }
.similar-list .meta { grid-column: 2 / -1; color: var(--text-3); font-size: var(--fs-12); }
.pending { display: flex; align-items: flex-start; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
.empty { padding: var(--s4); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.muted { color: var(--text-3); font-size: var(--fs-12); }
.btn {
  padding: var(--s1) var(--s4);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.btn:disabled { opacity: 0.5; cursor: default; }
.btn.primary { background: var(--brand-btn); color: #fff; }
.btn.primary:hover:not(:disabled) { background: var(--brand-btn-h); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(3, 1fr); }
}
</style>
