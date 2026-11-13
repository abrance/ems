<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import { ApiError } from '@/api/client'
import {
  fetchPriceBacktest,
  fetchRecordedForecast,
  type BacktestPoint,
} from '@/api/forecast'
import { fetchSpotMarketBoard } from '@/api/spotmarket'
import { fetchCurrentPrice } from '@/api/price'
import {
  buildSegmentLookup,
  dispersion,
  SEGMENT_LABEL,
  segmentOfPriceType,
  segmentStats,
  type PairedPoint,
  type Segment,
  type SegmentRange,
} from '@/metrics/forecast'
import { anchorEndDate, lagDaysFrom, rangeEndingAt } from '@/utils/latestRange'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 预测复盘。
 *
 * 这个页面叫「复盘」，唯一能回答「当时发出去的预测准不准」的只有当时的预测。
 * 所以默认腿 = **当时预测**（留痕对账）；「当前模型重跑」是模型评估，在页面上明确标成参考。
 *
 * 两条腿（页面上必须一眼分清）：
 *   当时预测 —— 留痕表里当时发出去的那条预测 vs 实际。真复盘，但留痕从启用之日起才积累。
 *   当前模型重跑 —— 用**现在的模型**在历史数据上重跑。立刻能看，但模型或历史数据一变数字就变，
 *                 不等于当时发出去的预测。
 *
 * 真值口径（2026-11-13 定，两条腿共用）：官方直采 pd-api 的**日均加权平均电价**
 * （gd_spot_metric）。第三方 CSV 通道（gd_metric.csv）已停更，只保留在「96 点 · 冻结」视图里
 * 做历史演示，并显式标「第三方 · 非官方」。官方披露没有 96 点分时口径 —— 这是数据事实，
 * 不是我们没写解析。
 *
 * 指标口径见 metrics/forecast.ts：Spearman ρ、离散度分母=当日均价、峰平谷取自电价管理。
 */
type Source = 'recorded' | 'backtest'
/** day = 官方日频日均价（默认）；intraday = 冻结的 96 点（历史演示） */
type Grain = 'day' | 'intraday'
/** 陈旧档位：ok = 0~2 天，lag = 3~14 天，stop = ≥15 天（默认不出数），unknown = 拿不到截止日 */
type Tier = 'ok' | 'lag' | 'stop' | 'unknown'

const PRESETS = [
  { v: '7', t: '近 7 天', days: 6 },
  { v: '14', t: '近 14 天', days: 13 },
  { v: '30', t: '近 30 天', days: 29 },
]

/** 后端停更错误码（api/forecast/v1.CodePriceTruthStale） */
const CODE_STALE = 6001
/** 冻结的 96 点通道最后一天（第三方 CSV 停更；不会再有新数据） */
const FROZEN_AS_OF = '2026-10-02'
/** ≥30 天额外提示「数据源可能已停止更新」 */
const ALERT_AFTER_DAYS = 30

const MODE_LABEL: Record<Source, string> = { recorded: '当时预测', backtest: '当前模型重跑' }

const source = ref<Source>('recorded')
const grain = ref<Grain>('day')
const market = ref('ahead')
const preset = ref('7')
const from = ref('')
const to = ref('')

/** 官方实际值最新日，按市场分别记（来自 board 的 coverage） */
const officialCutoff = ref<Record<string, string>>({})

const pairs = ref<PairedPoint[]>([])
const loading = ref(false)
const error = ref('')
/** 真值滞后超阈值时后端返回的原文（6001） */
const staleNotice = ref('')
/** 用户已经点开「查看历史表现」 */
const archive = ref(false)
/** 区间右端被截到实际值最新日 */
const clipped = ref(false)

const backtestMeta = ref<{
  model: string
  grain: string
  sampleDays: number
  targetDates: string[]
  skipped: string[]
  actualMissing: number
  timeMs: number
  dataAsOf: string
  lagDays: number
  stale: boolean
  truthSource: string
  historyDays: number
  from: string
  to: string
} | null>(null)

const recordedMeta = ref<{
  generatedDates: string[]
  from: string
  to: string
  count: number
  withActual: number
  dataAsOf: string
  lagDays: number
  truthSource: string
  backfilled: number
  actualMissing: number
  unavailable: string
} | null>(null)

/** 电价时段 → 峰平谷查表函数；拿不到配置时全部归「全天」 */
const lookup = ref<(m: number) => Segment | null>(() => null)
const segmentConfigured = ref(false)

/** 96 点冻结视图（真值固定止于 FROZEN_AS_OF，不需要请求） */
const frozenLeg = computed(() => source.value === 'backtest' && grain.value === 'intraday')
/** 一天一个点的腿（留痕对账 / 日频重跑）：KPI 用 MAE/MAPE，不摆峰平谷矩阵 */
const dayGrain = computed(() => source.value === 'recorded' || grain.value === 'day')

/** 真值截至哪天（服务端下发的 dataAsOf 优先，其次 board 的最新实际值日） */
const cutoff = computed(() => {
  if (frozenLeg.value) return FROZEN_AS_OF
  const server = source.value === 'recorded' ? recordedMeta.value?.dataAsOf : backtestMeta.value?.dataAsOf
  return server || officialCutoff.value[market.value] || ''
})

/** 滞后天数：服务端按 Asia/Shanghai 算过就用它的，没有才在前端补算 */
const lag = computed(() => {
  if (frozenLeg.value) return lagDaysFrom(FROZEN_AS_OF)
  const server = source.value === 'recorded' ? recordedMeta.value?.lagDays : backtestMeta.value?.lagDays
  if (server != null && server >= 0) return server
  return cutoff.value ? lagDaysFrom(cutoff.value) : null
})

const tier = computed<Tier>(() => {
  const l = lag.value
  if (l == null || l < 0) return 'unknown'
  if (l <= 2) return 'ok'
  if (l <= 14) return 'lag'
  return 'stop'
})

/** 停更档位默认不出数：数字区换成阻断卡，点「查看历史」才展开。
 *
 * 「确认陈旧」才算：服务端下发了滞后天数（它按 Asia/Shanghai 算），或冻结腿（截止日是常量）。
 * 只凭 board 的截止日不能直接阻断 —— board 拿不到时会把整页关掉，而真值其实可能是新的。
 */
const staleConfirmed = computed(() => {
  if (frozenLeg.value) return true
  const server = source.value === 'recorded' ? recordedMeta.value?.lagDays : backtestMeta.value?.lagDays
  return server != null && server >= 0
})
const numbersHidden = computed(() => tier.value === 'stop' && staleConfirmed.value && !archive.value)
/** 历史存档视图：置灰 + 水印 + KPI 角标 */
const archived = computed(() => archive.value && tier.value === 'stop')

const truthSourceLabel = computed(() => {
  if (frozenLeg.value) return '第三方 CSV（电查查 · 非官方）'
  const server = source.value === 'recorded' ? recordedMeta.value?.truthSource : backtestMeta.value?.truthSource
  return server || '官方直采（gd_spot_metric）'
})

const modeBadge = computed(() => (source.value === 'recorded' ? '真实记录' : '模型参考'))

/** 面板副标题：模式名开头，后面跟数据实际覆盖的区间 */
const rangeLabel = computed(() => {
  const meta = source.value === 'recorded' ? recordedMeta.value : backtestMeta.value
  const f = meta?.from || from.value
  const t = meta?.to || to.value
  return `${f || '—'} ~ ${t || '—'}`
})
const overviewSub = computed(
  () => `${MODE_LABEL[source.value]} · ${rangeLabel.value} · ${pairs.value.length} 个对比点 · ${days.value.length} 天`,
)
const chartSub = computed(() => `${MODE_LABEL[source.value]} · ${rangeLabel.value}`)

/** 图上的水印：重跑腿常驻，历史存档额外再标一次 */
const chartWatermark = computed(() => {
  const lines: string[] = []
  if (source.value === 'backtest') lines.push('当前模型重跑 · 非当时预测')
  if (archived.value) lines.push(`历史存档 · 数据截至 ${cutoff.value}`)
  return lines
})

function applyPreset() {
  // 右端锚到「实际值最新日」而不是今天：数据滞后时，「今天往前 7 天」会整段落在数据之外。
  const anchor = frozenLeg.value ? FROZEN_AS_OF : officialCutoff.value[market.value] || ''
  const end = anchor ? anchorEndDate(anchor) : new Date()
  const days = PRESETS.find((p) => p.v === preset.value)?.days ?? 6
  const r = rangeEndingAt(end, days)
  from.value = r.from
  to.value = r.to
}

/** 越界自动截断：选择器的 max 已经是截止日，手输/旧值再兜一层 */
function clampRange() {
  const c = cutoff.value
  clipped.value = false
  if (!c) return
  if (to.value > c) {
    to.value = c
    clipped.value = true
  }
  if (from.value > to.value) from.value = to.value
}

async function loadSegments() {
  try {
    const price = await fetchCurrentPrice()
    const ranges: SegmentRange[] = (price?.priceSegments ?? [])
      .filter((s) => s.priceType)
      .map((s) => ({
        startTime: s.startTime,
        endTime: s.endTime,
        segment: segmentOfPriceType(s.priceType),
      }))
    if (ranges.length) {
      lookup.value = buildSegmentLookup(ranges)
      segmentConfigured.value = true
    }
  } catch {
    // 拿不到时段配置就退化为「全天」，页面会说明
    segmentConfigured.value = false
  }
}

/**
 * 当时预测 -> 与官方日均实际值配对。
 *
 * 实际值由后端按同一口径（pm-api 发电侧加权平均电价）配好随留痕一起下发：
 * 一天一条留痕对一天一个日均价，1:1，没有「拿 96 点当基准」这种口径混用。
 * 真值没出的目标日（预测比实际早）不算进指标 —— 不硬凑。
 */
async function loadRecorded() {
  const rec = await fetchRecordedForecast({
    market: market.value,
    grain: 'day',
    from: from.value,
    to: to.value,
  })
  const matched = rec.points.filter((p) => p.actual != null && Number.isFinite(p.actual))
  pairs.value = matched.map((p) => ({
    targetTime: String(p.targetTime).slice(0, 10),
    predicted: p.point,
    actual: p.actual as number,
  }))
  const targets = rec.points.map((p) => String(p.targetTime).slice(0, 10)).sort()
  recordedMeta.value = {
    generatedDates: rec.generatedDates ?? [],
    from: targets[0] ?? '',
    to: targets[targets.length - 1] ?? '',
    count: rec.points.length,
    withActual: matched.length,
    dataAsOf: rec.dataAsOf ?? '',
    lagDays: rec.lagDays ?? -1,
    truthSource: rec.truthSource ?? '',
    backfilled: rec.backfilled ?? 0,
    actualMissing: rec.actualMissing ?? 0,
    unavailable: rec.actualUnavailable ?? '',
  }
}

async function loadBacktest() {
  const day = grain.value === 'day'
  try {
    const bt = await fetchPriceBacktest({
      market: market.value,
      grain: day ? 'day' : 'intraday',
      from: from.value,
      to: to.value,
      // 日频：一天一点，最多后端 maxBacktestPoints（6）个点 —— 所以「近 7 天」档实际是最近 6 天；
      // 96 点：每点一天 96 次配对，间隔抽样 3 天
      sampleDays: day ? 1 : 3,
      // 只有「黄色档（3~14 天）」与「用户主动展开历史」才穿透服务端阈值：
      // 绿色档不该无脑绕过守卫，红色档默认连数都不取。
      allowStale: tier.value === 'lag' || archive.value ? true : undefined,
    })
    pairs.value = bt.points.map((p: BacktestPoint) => ({
      targetTime: p.targetTime,
      predicted: p.predicted,
      actual: p.actual,
    }))
    backtestMeta.value = {
      model: bt.model,
      grain: bt.grain,
      sampleDays: bt.sampleDays,
      targetDates: bt.targetDates,
      skipped: bt.skipped ?? [],
      actualMissing: bt.actualMissing ?? 0,
      timeMs: bt.timeMs,
      dataAsOf: bt.dataAsOf ?? '',
      lagDays: bt.lagDays ?? -1,
      stale: bt.stale ?? false,
      truthSource: bt.truthSource ?? '',
      historyDays: bt.historyDays ?? 0,
      from: bt.from ?? '',
      to: bt.to ?? '',
    }
    staleNotice.value = ''
  } catch (err) {
    // 真值滞后超阈值：后端拒绝在旧区间上算数字。这里不是「失败」，是「要不要看历史」的选择。
    if (err instanceof ApiError && err.code === CODE_STALE) {
      staleNotice.value = err.message
      pairs.value = []
      backtestMeta.value = null
      return
    }
    throw err
  }
}

async function load() {
  loading.value = true
  error.value = ''
  pairs.value = []
  backtestMeta.value = null
  recordedMeta.value = null
  staleNotice.value = ''
  clampRange()
  // 冻结腿的截止日是常量：停更档位下连数都不取（不是「把按钮 disable 了但数字还在算」）
  if (frozenLeg.value && numbersHidden.value) {
    loading.value = false
    return
  }
  try {
    if (source.value === 'recorded') await loadRecorded()
    else await loadBacktest()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

/** 用户明确要看陈旧区间（停更档位的「查看历史表现」） */
async function viewHistory() {
  archive.value = true
  await load()
}

function switchSource(next: Source) {
  source.value = next
  if (next === 'recorded') grain.value = 'day'
  archive.value = false
  applyPreset()
  void load()
}

function switchGrain(next: Grain) {
  grain.value = next
  archive.value = false
  applyPreset()
  void load()
}

function switchMarket() {
  archive.value = false
  applyPreset()
  void load()
}

/** 留痕为空时的一键切换（不自动切：页面语义必须由用户自己确认） */
function switchToBacktest() {
  switchSource('backtest')
}

const disp = computed(() => dispersion(pairs.value.filter((p) => Number.isFinite(p.actual)), lookup.value))
const stats = computed(() => segmentStats(pairs.value.filter((p) => Number.isFinite(p.actual)), disp.value))
const overall = computed(() => stats.value.find((s) => s.segment === 'all'))
const days = computed(() => [...new Set(pairs.value.map((p) => String(p.targetTime).slice(0, 10)))].sort())

/** 一天一个点的腿：MAE 与「占当日实际值的比例」才是可比的量 */
const dayStats = computed(() => {
  const pts = pairs.value.filter((p) => Number.isFinite(p.actual))
  if (!pts.length) return { n: 0, mae: NaN, mape: NaN }
  let abs = 0
  let pct = 0
  let pctN = 0
  for (const p of pts) {
    const e = Math.abs(p.predicted - p.actual)
    abs += e
    if (p.actual !== 0) {
      pct += e / Math.abs(p.actual)
      pctN += 1
    }
  }
  return { n: pts.length, mae: abs / pts.length, mape: pctN ? (pct / pctN) * 100 : NaN }
})

function fmtNum(v: number | null | undefined, digits = 3): string {
  return v == null || !Number.isFinite(v) ? '—' : v.toFixed(digits)
}

/** 图表数据点：留痕腿是 2 条线，重跑腿也是 2 条线（实际 / 预测） */
const ACTUAL_LABEL = '实际出清价（官方日均价）'
const predictedLabel = computed(() => (source.value === 'recorded' ? '当时发出的预测' : '当前模型重跑值'))

/* ---------- 图：实际 vs 预测 ---------- */
const chartEl = ref<HTMLElement>()
useECharts(
  chartEl,
  (): EChartsOption => {
    const xs = pairs.value.map((p) => p.targetTime)
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 48 },
      tooltip: { trigger: 'axis' },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: xs,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: 40, interval: Math.max(0, Math.floor(xs.length / 12) - 1) },
      },
      yAxis: {
        type: 'value',
        name: '元/MWh',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: ACTUAL_LABEL,
          type: 'line',
          showSymbol: false,
          lineStyle: { width: 2 },
          itemStyle: { color: CHART_COLORS.blue },
          data: pairs.value.map((p) => (Number.isFinite(p.actual) ? p.actual : null)),
        },
        {
          name: predictedLabel.value,
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.brand },
          data: pairs.value.map((p) => p.predicted),
        },
      ],
    }
  },
  [pairs, source, archived],
)

/* ---------- 图：逐点离散度 ---------- */
const dispEl = ref<HTMLElement>()
const dispMode = ref<'pct' | 'abs'>('pct')
useECharts(
  dispEl,
  (): EChartsOption => ({
    grid: { left: 60, right: 20, top: 28, bottom: 48 },
    tooltip: {
      trigger: 'axis',
      valueFormatter: (v: unknown) => (v == null ? '—' : String(v)),
    },
    xAxis: {
      type: 'category',
      data: disp.value.map((d) => d.targetTime),
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: 40, interval: Math.max(0, Math.floor(disp.value.length / 12) - 1) },
    },
    yAxis: {
      type: 'value',
      name: dispMode.value === 'pct' ? '标幺' : '元/MWh',
      nameTextStyle: { fontSize: 12 },
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: 'var(--divider)' } },
    },
    series: [
      {
        name: '离散度',
        type: 'bar',
        itemStyle: { color: CHART_COLORS.brand },
        data: disp.value.map((d) => {
          const v = dispMode.value === 'pct' ? d.pct : d.abs
          return Number.isFinite(v) ? Number(v.toFixed(4)) : null
        }),
      },
    ],
  }),
  [disp, dispMode],
)

onMounted(async () => {
  // 先问官方最新实际值日（按市场），再定默认区间：数据滞后时「今天往前 7 天」会整段在数据之外
  try {
    const board = await fetchSpotMarketBoard({ days: 90 })
    const pick = (dataset: string) => board.coverage.find((c) => c.dataset === dataset)?.lastDate || ''
    officialCutoff.value = {
      ahead: pick('day_ahead_trade') || board.date || '',
      real: pick('realtime_trade') || '',
    }
  } catch {
    officialCutoff.value = {}
  }
  market.value = 'ahead'
  applyPreset()
  await loadSegments()
  await load()
})
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <p class="lead">复盘看「当时预测」；「当前模型重跑」只是模型评估，不是当时发出去的预测。</p>

  <div class="grid">
    <PanelBox title="数据来源" :span="12" :foot="false">
      <div class="toolbar">
        <span class="radio-group">
          <button type="button" :class="{ on: source === 'recorded' }" @click="switchSource('recorded')">
            当时预测
          </button>
          <button type="button" :class="{ on: source === 'backtest' }" @click="switchSource('backtest')">
            当前模型重跑
          </button>
        </span>
        <label class="field">
          市场
          <select v-model="market" @change="switchMarket">
            <option value="ahead">日前</option>
            <option value="real">实时</option>
          </select>
        </label>
        <span v-if="source === 'backtest'" class="radio-group">
          <button type="button" :class="{ on: grain === 'day' }" @click="switchGrain('day')">
            日频 · 官方
          </button>
          <button type="button" :class="{ on: grain === 'intraday' }" @click="switchGrain('intraday')">
            96 点 · 冻结
          </button>
        </span>
        <span class="radio-group">
          <button
            v-for="p in PRESETS"
            :key="p.v"
            type="button"
            :class="{ on: preset === p.v }"
            @click="((preset = p.v), applyPreset(), load())"
          >
            {{ p.t }}
          </button>
        </span>
        <label class="field">起<input v-model="from" type="date" :max="cutoff || undefined" /></label>
        <span class="muted">→</span>
        <label class="field">止<input v-model="to" type="date" :max="cutoff || undefined" /></label>
        <span class="spacer" />
        <button class="btn primary" type="button" :disabled="loading" @click="load">
          {{ loading ? '计算中…' : '查询' }}
        </button>
      </div>

      <div class="warn">
        <template v-if="source === 'recorded'">
          <strong>当时预测 · 留痕对账</strong>
          <span>　用<strong>当时</strong>发出去的预测与实际比 —— 这才是复盘。但留痕从功能上线之日起才开始积累。</span>
          <template v-if="recordedMeta">
            <span class="muted">
              （留痕 {{ recordedMeta.count }} 个点，其中 {{ recordedMeta.withActual }} 个已有官方实际值；
              覆盖 {{ recordedMeta.from || '—' }} ~ {{ recordedMeta.to || '—' }}；
              生成日 {{ recordedMeta.generatedDates.length }} 天{{
                recordedMeta.generatedDates.length ? `：${recordedMeta.generatedDates.slice(-3).join('、')}` : '，还没开始积累'
              }}）
            </span>
          </template>
        </template>
        <template v-else-if="grain === 'day'">
          <strong>当前模型重跑 · 官方日频</strong>
          <span>
            　真值 = 官方直采的<b>发电侧加权平均电价</b>（一天一个价），预测 = 当前模型只用目标日之前的历史重跑。
            数字会随模型/历史变化，<strong>不等于当时发出去的预测</strong>。
          </span>
        </template>
        <template v-else>
          <strong>当前模型重跑 · 96 点（冻结）</strong>
          <span>
            　这条腿的真值来自<b>已停更</b>的第三方通道，官方披露没有 96 点分时口径。仅供历史演示，
            不要把这里的数字当成近期表现；要近期数据请切回「日频 · 官方」。
          </span>
        </template>
      </div>

      <!-- 诚实性：实际值到哪天、滞后多少天，永远显示，三档用颜色区分 -->
      <div v-if="tier === 'ok'" class="fresh ok">
        <span class="dot" />
        <strong>实际值截至 {{ cutoff }} · 正常</strong>
        <span class="muted">来源：{{ truthSourceLabel }}</span>
      </div>
      <div v-else-if="tier === 'lag'" class="fresh lag">
        <strong>实际值截至 {{ cutoff }} · 滞后 {{ lag }} 天</strong>
        <span>来源：{{ truthSourceLabel }}。区间右端已锚到实际值最新那天，所以「近 7 天」指的是 {{ cutoff }} 往前 7 天。</span>
      </div>
      <div v-else-if="tier === 'stop'" class="fresh stop">
        <strong>实际值截至 {{ cutoff }} · 已停更 {{ lag }} 天</strong>
        <span>
          来源：{{ truthSourceLabel }}。默认不展示基于这段陈旧真值算出的指标 ——
          要看得自己展开历史存档。
        </span>
      </div>
      <div v-else class="fresh lag">
        <strong>实际值截止日未知</strong>
        <span>拿不到官方实际值的最新日期，下面的数字可能来自陈旧区间，请先确认数据源。</span>
      </div>

      <div v-if="clipped" class="warn">
        <strong>已截到实际值最新日 {{ cutoff }}</strong>
        <span>　选定的终止日晚于实际值截止日，已自动收到 {{ cutoff }}。</span>
      </div>

      <div v-if="recordedMeta && recordedMeta.backfilled" class="warn">
        <strong>剔除了 {{ recordedMeta.backfilled }} 条事后回填</strong>
        <span>　这些记录的生成日晚于目标日，是事后补的历史值，不是当时发出去的预测，不计入复盘。</span>
      </div>

      <div v-if="!segmentConfigured && !dayGrain" class="warn">
        <strong>峰平谷时段未取到</strong>
        <span>　「管理 · 电价管理」里没有当前生效的时段配置，所有点都归入「全天」。设好时段后回来重看。</span>
      </div>
    </PanelBox>

    <PanelBox title="概览" :sub="overviewSub" :span="12" :loading="loading" :foot="false">
      <template #actions>
        <span class="tag" :class="source === 'recorded' ? 'real' : 'model'">{{ modeBadge }}</span>
        <span v-if="archived" class="tag hist">历史</span>
      </template>

      <div v-if="numbersHidden" class="blocked">
        <div class="blocked-title">实际值已停更 {{ lag }} 天，默认不出数</div>
        <p>
          实际值只到 <b>{{ cutoff }}</b>（滞后 {{ lag }} 天）。在这种陈旧度上算出来的准确度只代表那个历史区间，
          不代表近期表现，所以这里不给数字。
        </p>
        <p v-if="(lag ?? 0) >= ALERT_AFTER_DAYS">
          已连续 {{ lag }} 天没有新实际值，数据源可能已停止更新，请联系数据维护。
        </p>
        <p v-if="staleNotice" class="muted">{{ staleNotice }}</p>
        <button class="btn" type="button" @click="viewHistory">
          查看 {{ cutoff }} 之前的历史表现
        </button>
      </div>

      <div v-else class="kpis" :class="{ archived }">
        <template v-if="dayGrain">
          <KpiCard label="平均绝对误差" :value="fmtNum(dayStats.mae, 1)" unit="元/MWh" hint="|预测 − 实际| 的均值（日均价）" :badge="archived ? '历史' : undefined" />
          <KpiCard label="平均绝对百分比误差" :value="fmtNum(dayStats.mape, 2)" unit="%" hint="÷ 当日实际均价，负电价日按绝对值取分母" :badge="archived ? '历史' : undefined" />
          <KpiCard label="对比天数" :value="dayStats.n" :hint="`目标日 ${days.join('、') || '—'}`" :badge="archived ? '历史' : undefined" />
          <KpiCard label="实际值截至" :value="cutoff || '—'" :hint="`滞后 ${lag} 天 · ${truthSourceLabel}`" :badge="archived ? '历史' : undefined" />
        </template>
        <template v-else>
          <KpiCard
            label="趋势相关度（全天）"
            :value="fmtNum(overall?.rho)"
            :hint="overall?.rho == null ? '样本不足或秩无方差' : 'Spearman ρ，越接近 1 越准'"
            :badge="archived ? '历史' : undefined"
          />
          <KpiCard
            label="平均离散度（全天）"
            :value="fmtNum(overall?.avgPct)"
            unit="标幺"
            hint="分母 = 当日出清均价"
            :badge="archived ? '历史' : undefined"
          />
          <KpiCard label="平均绝对误差" :value="fmtNum(overall?.mae, 1)" unit="元/MWh" hint="|预测 − 实际| 的均值" :badge="archived ? '历史' : undefined" />
          <KpiCard label="对比点数" :value="pairs.length" :hint="`覆盖 ${days.length} 个自然日`" :badge="archived ? '历史' : undefined" />
        </template>
      </div>
    </PanelBox>

    <PanelBox v-if="!numbersHidden && dayGrain" title="为什么没有峰平谷 / 逐点离散度" :span="12" :foot="false">
      <div class="empty" style="padding: var(--s4)">
        官方披露只有<b>日均价</b>（加权平均/最高/最低/申报均价/成交量），没有 96 点分时口径，
        所以这里一天只有一个点：峰平谷分组、逐点离散度、Spearman ρ 在这里没有意义。
        按月/季看分时形态请用「信息披露」页的官方日频序列；96 点形态只能看冻结的历史演示。
      </div>
    </PanelBox>

    <PanelBox
      v-if="!numbersHidden && !dayGrain"
      title="趋势相关度"
      sub="Spearman ρ · 峰 / 平 / 谷 / 全天"
      :span="6"
      :loading="loading"
      foot="秩相关：电价有尖峰与负价，Pearson 会被尖峰主导，所以用 Spearman"
    >
      <div class="matrix">
        <div class="h">时段</div>
        <div v-for="s in (['peak', 'flat', 'valley', 'all'] as Segment[])" :key="s" class="h">
          {{ SEGMENT_LABEL[s] }}
        </div>
        <div>ρ</div>
        <div v-for="s in stats" :key="`r-${s.segment}`" class="num">
          {{ fmtNum(s.rho) }}
          <span v-if="s.rho == null && s.count" class="muted">样本不足</span>
        </div>
        <div>样本点</div>
        <div v-for="s in stats" :key="`n-${s.segment}`" class="num">{{ s.count }}</div>
      </div>
    </PanelBox>

    <PanelBox
      v-if="!numbersHidden && !dayGrain"
      title="峰平谷平均离散度"
      sub="|预测 − 实际| ÷ 当日出清均价"
      :span="6"
      :loading="loading"
      foot="分母用当日均价而不是逐点实际值：广东有负电价，逐点做分母会爆表"
    >
      <div class="matrix">
        <div class="h">时段</div>
        <div v-for="s in (['peak', 'flat', 'valley', 'all'] as Segment[])" :key="s" class="h">
          {{ SEGMENT_LABEL[s] }}
        </div>
        <div>标幺</div>
        <div v-for="s in stats" :key="`p-${s.segment}`" class="num">{{ fmtNum(s.avgPct) }}</div>
        <div>元/MWh</div>
        <div v-for="s in stats" :key="`m-${s.segment}`" class="num">{{ fmtNum(s.mae, 1) }}</div>
      </div>
    </PanelBox>

    <PanelBox
      v-if="!numbersHidden"
      title="实际 vs 预测"
      :sub="chartSub"
      :span="12"
      :loading="loading"
      :foot="
        dayGrain
          ? `实际 = 官方直采的发电侧加权平均电价；${source === 'recorded' ? '预测 = 当时发出去的那条' : '预测 = 目标日之前的历史送入当前模型的输出'}`
          : '两条曲线都来自 15 分钟序列；时间轴按目标时刻'
      "
    >
      <template #actions>
        <span class="tag" :class="source === 'recorded' ? 'real' : 'model'">{{ modeBadge }}</span>
        <span v-if="archived" class="tag hist">历史</span>
      </template>

      <div v-if="pairs.length" class="chart-wrap" :class="{ archived }">
        <div v-if="chartWatermark.length" class="chart-wm">
          <span v-for="w in chartWatermark" :key="w">{{ w }}</span>
        </div>
        <div ref="chartEl" class="chart" />
      </div>

      <div v-else-if="!loading" class="empty">
        <template v-if="source === 'recorded'">
          <div class="empty-title">还没有可复盘的当时预测</div>
          <div>
            预测记录从 {{ recordedMeta?.generatedDates[0] || '—' }} 起，目前 {{ days.length }} 天。
            发过预测、且对应日期的实际出清价入库后，这里才会出现对比。
          </div>
          <div v-if="recordedMeta?.unavailable" class="muted">{{ recordedMeta.unavailable }}</div>
          <div v-else-if="recordedMeta?.actualMissing" class="muted">
            另有 {{ recordedMeta.actualMissing }} 条留痕的实际值还没出。
          </div>
          <button class="btn" type="button" @click="switchToBacktest">先看当前模型重跑</button>
        </template>
        <template v-else-if="grain === 'day'">
          日频重跑没有产出对比点。多半是区间内没有官方已确认的实际值（实际值截至 {{ cutoff || '未知' }}），换个区间再试。
        </template>
        <template v-else>
          96 点真值只到 {{ FROZEN_AS_OF }}，区间落在这之后就没有可配对的实际值。
        </template>
      </div>
    </PanelBox>

    <PanelBox
      v-if="!numbersHidden && !dayGrain"
      title="逐点离散度"
      :sub="dispMode === 'pct' ? '标幺（÷ 当日均价）' : '数值（元/MWh）'"
      :span="12"
      :loading="loading"
      foot="同日多个点的分母相同，所以同一竖列的高低就是预测误差的相对大小"
    >
      <template #actions>
        <span class="radio-group">
          <button type="button" :class="{ on: dispMode === 'pct' }" @click="dispMode = 'pct'">标幺</button>
          <button type="button" :class="{ on: dispMode === 'abs' }" @click="dispMode = 'abs'">数值</button>
        </span>
      </template>
      <div v-if="disp.length" ref="dispEl" class="chart" />
      <div v-else-if="!loading" class="empty">没有可计算的离散度。</div>
    </PanelBox>

    <PanelBox title="口径说明" :span="12" foot="改动公式前先看 metrics/forecast.ts 的文件头">
      <ul class="notes">
        <li><strong>两条腿</strong>：<b>当时预测</b> = 留痕表里当时发出去的那条，才是真复盘；<b>当前模型重跑</b> = 用当前模型在历史数据上重跑，只是模型评估。</li>
        <li><strong>趋势相关度</strong>：Spearman ρ（秩相关）。样本 &lt; 3 或某一侧秩无方差时显示「—」，不返回 0。</li>
        <li><strong>分时离散度</strong>：<code>|预测 − 实际| ÷ 当日出清均价</code>。当日均价接近 0 时标幺无意义，该点不参与均值。</li>
        <li>
          <strong>峰 / 平 / 谷</strong>：取自「管理 · 电价管理」的时段配置 —— 峰=Peak+Sharp，平=Flat，谷=Valley+DeepValley。
          当前{{ segmentConfigured ? '已取到' : '未取到（全部归入全天）' }}。
        </li>
        <li>
          <strong>真值来源（两条腿同一个口径）</strong>：官方直采（pm.gd.csg.cn 披露接口，
          <code>gd_spot_metric</code> 的「发电侧日前/实时加权平均电价」）。
          96 点分时只存在于第三方 CSV，<b>已停更</b>，实际值止于 {{ FROZEN_AS_OF }}，只作历史演示并标「第三方 · 非官方」。
        </li>
        <li>
          <strong>占位不入曲线</strong>：CSV 里 sourceId=999 是电查查预测占位（该时刻实际值还没出），
          回测与留痕都按「没有实际值」处理，不拿它当基准 —— 否则算出来的是「两个模型谁准」。
        </li>
        <li><strong>配对按日期</strong>：留痕与官方日均价一天对一天，实际值还没出的目标日不进指标，不硬凑。</li>
        <li>
          <strong>as-of 重跑</strong>：预测目标日 D 时，只把 D-1 及以前的历史送进模型，不用「今天才知道的数据」，
          这样「当时能拿到的信息 + 现在的模型」才是回测，而不是事后诸葛亮。
        </li>
        <li>
          <strong>模型输入</strong>：<strong>实时</strong>电价用多变量输入（实时价 + 日前价 + 系统负荷 三个通道），
          实测 24 天真实实际值 MAE 96.4 → 89.5 元/MWh（标幺 0.191 → 0.177）；
          <strong>日前</strong>是单变量 —— 加负荷 / 加实时价 / 加星期几通道都持平或更差。
        </li>
      </ul>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.lead {
  margin: 0 0 var(--s4);
  padding: var(--s2) var(--s4);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-2);
  font-size: var(--fs-13);
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
  padding: 0 var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
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
.field input,
.field select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.warn {
  display: flex;
  align-items: flex-start;
  gap: var(--s2);
  flex-wrap: wrap;
  margin-top: var(--s3);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-12);
}
/* 陈旧三档：0~2 天绿点 / 3~14 天黄条 / ≥15 天红条 */
.fresh {
  display: flex;
  align-items: center;
  gap: var(--s2);
  flex-wrap: wrap;
  margin-top: var(--s3);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font-size: var(--fs-12);
}
.fresh .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--green); flex: 0 0 auto; }
.fresh.ok { border-left-color: var(--green); background: var(--green-soft); color: var(--green-ink); }
.fresh.lag { border-left-color: var(--gold); background: var(--brand-soft); color: var(--brand-ink); }
.fresh.stop { border-left-color: var(--red); background: var(--red-soft); color: var(--red-ink); }
.tag {
  padding: 0 var(--s2);
  border-radius: var(--r-pill);
  background: var(--gray-soft);
  color: var(--gray-ink);
  font-size: var(--fs-12);
  white-space: nowrap;
}
.tag.real { background: var(--green-soft); color: var(--green-ink); }
.tag.model { background: var(--brand-soft); color: var(--brand-ink); }
.tag.hist { background: var(--gray-soft); color: var(--gray-ink); }
.blocked {
  display: flex;
  flex-direction: column;
  gap: var(--s2);
  align-items: flex-start;
  padding: var(--s5);
  border: 1px dashed var(--red);
  border-radius: var(--r-md);
  background: var(--red-soft);
  color: var(--red-ink);
  font-size: var(--fs-13);
}
.blocked p { margin: 0; }
.blocked-title { font-size: var(--fs-16); font-weight: 600; }
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.kpis.archived { opacity: 0.55; filter: grayscale(0.6); }
.matrix { display: grid; grid-template-columns: 88px repeat(4, 1fr); gap: 1px; background: var(--border); border: 1px solid var(--border); border-radius: var(--r-sm); overflow: hidden; font-size: var(--fs-13); }
.matrix > div { padding: var(--s2) var(--s3); background: var(--bg-2); }
.matrix .h { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.matrix .num { text-align: right; font-variant-numeric: tabular-nums; }
.chart-wrap { position: relative; }
.chart-wrap.archived { opacity: 0.6; filter: grayscale(0.5); }
.chart-wm {
  position: absolute;
  top: 0;
  right: 0;
  z-index: 1;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 2px;
  pointer-events: none;
  color: var(--text-3);
  font-size: var(--fs-12);
}
.chart { height: 340px; }
.notes { margin: 0; padding-left: var(--s5); font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
.notes code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
.empty { display: flex; flex-direction: column; align-items: center; gap: var(--s2); padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.empty-title { font-size: var(--fs-16); font-weight: 600; color: var(--text-2); }
.muted { color: var(--text-3); font-size: var(--fs-12); font-weight: 400; }
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
  .kpis { grid-template-columns: repeat(2, 1fr); }
}
</style>
