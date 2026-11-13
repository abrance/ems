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
  type RecordedForecastPoint,
} from '@/api/forecast'
import { fetchMetricSeries, isRealSource } from '@/api/gdmetric'
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
 * 两条数据腿，页面上必须分清（这是「复盘」二字的全部意义）：
 *   留痕对账 —— **当时**发出去的预测 vs 实际。真实复盘，但今天才开始积累。
 *   事后回测 —— **用现在的模型**在历史数据上重跑 vs 实际。立刻能看，
 *               但模型或历史数据一变数字就变，不等于当时预测。
 *
 * 数据源（2026-11-13 定）：真值一律走官方直采（gd_spot_metric，日均加权价）。
 * 96 点分时只存在于已停更的第三方 CSV，默认不展示；要看得显式切到「96 点（冻结）」，
 * 并自己承担「数据止于 2026-10-02」这件事 —— 页面会把日期写在脸上。
 *
 * 指标口径见 metrics/forecast.ts：Spearman ρ、离散度分母=当日均价、峰平谷取自电价管理。
 */
type Source = 'recorded' | 'backtest'
/** day = 官方日频日均价（默认）；intraday = 冻结的 96 点（历史演示） */
type Grain = 'day' | 'intraday'

const PRESETS = [
  { v: '7', t: '近 7 天', days: 6 },
  { v: '14', t: '近 14 天', days: 13 },
  { v: '30', t: '近 30 天', days: 29 },
]

/** 后端停更错误码（api/forecast/v1.CodePriceTruthStale） */
const CODE_STALE = 6001

const source = ref<Source>('backtest')
const grain = ref<Grain>('day')
const market = ref('ahead')
const preset = ref('7')
const from = ref('')
const to = ref('')

/** 官方真值最新运行日（GET /api/spot-market/board 的 lastDate） */
const officialLastDate = ref<string | null>(null)
/** 冻结的 96 点通道实际值止于哪天（后端停更错误里给，取不到用现场核对值） */
const frozenAsOf = ref('2026-10-02')

const pairs = ref<PairedPoint[]>([])
const loading = ref(false)
const error = ref('')
/** 真值滞后超阈值时后端返回的原文（6001），页面据此给出「仍要查看」 */
const staleNotice = ref('')
const staleBlocked = ref(false)
const forceStale = ref(false)
const sourceNote = ref('')
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
} | null>(null)
const recordedMeta = ref<{ generatedDates: string[]; from: string; to: string; count: number } | null>(null)
/** 留痕腿取实际值失败的原因（通常是 96 点通道停更） */
const recordedActualUnavailable = ref('')

/** 区间内被剔掉的占位/来源不明点数（留痕那条腿按时间戳取实际值时统计） */
const placeholderDropped = ref(0)

/** 电价时段 → 峰平谷查表函数；拿不到配置时全部归「全天」 */
const lookup = ref<(m: number) => Segment | null>(() => null)
const segmentConfigured = ref(false)

/** 日频回测：1 个点/天，峰平谷矩阵与逐点离散度都失去意义，改用 MAE/MAPE */
const isDayGrain = computed(() => source.value === 'backtest' && grain.value === 'day')

/** 页面上要显示的「真值截至哪天」 */
const truthAsOf = computed(() => {
  if (isDayGrain.value) return backtestMeta.value?.dataAsOf || officialLastDate.value || ''
  return backtestMeta.value?.dataAsOf || frozenAsOf.value
})
const lagDays = computed(() => (truthAsOf.value ? lagDaysFrom(truthAsOf.value) : null))

function applyPreset() {
  // 右端锚到「真值最新日」而不是今天：数据滞后时，「今天往前 7 天」会整段落在数据之外。
  // 日频用官方最新运行日；96 点用冻结通道的最后一天（它不会再有新数据）。
  const end = isDayGrain.value ? anchorEndDate(officialLastDate.value) : anchorEndDate(frozenAsOf.value)
  const days = PRESETS.find((p) => p.v === preset.value)?.days ?? 6
  const r = rangeEndingAt(end, days)
  from.value = r.from
  to.value = r.to
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
 * 留痕 -> 与同区间的实际 96 点序列配对。
 * 只有时刻能对上的点才进入指标计算 —— 对不上就丢弃，不硬凑（硬凑出来的 ρ 没有意义）。
 *
 * 实际值来自冻结通道：请求必然带 allowFrozen，并且失败时把原因说出来，
 * 而不是显示「0 个点」让人以为「本来就没有」。
 */
async function pairRecordedWithActual(points: RecordedForecastPoint[]): Promise<PairedPoint[]> {
  if (!points.length) return []
  const days = [...new Set(points.map((p) => p.targetTime.slice(0, 10)))].sort()
  try {
    const series = await fetchMetricSeries({
      from: days[0],
      to: days[days.length - 1],
      kind: market.value === 'real' ? 'realtime' : 'dayAhead',
      allowFrozen: true,
    })
    // 电查查预测占位（999）与来源不明（0）都不是实际值，拿它当基准比出来的是「两个模型差多少」→ 剔除
    const real = series.points.filter((s) => isRealSource(s.sourceId))
    placeholderDropped.value = series.points.length - real.length
    const byKey = new Map<string, number>()
    for (const s of real) {
      const clock = s.time === '24:00' ? '00:00' : s.time
      const day = s.time === '24:00' ? nextDay(s.date) : s.date
      byKey.set(`${day} ${clock}`, s.value)
    }
    return points
      .map((p) => ({
        targetTime: p.targetTime,
        predicted: p.point,
        actual: byKey.get(p.targetTime) ?? NaN,
      }))
      .filter((p) => Number.isFinite(p.actual))
  } catch (err) {
    recordedActualUnavailable.value = err instanceof Error ? err.message : '实际值读取失败'
    return []
  }
}

function nextDay(date: string): string {
  const d = new Date(`${date}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + 1)
  return d.toISOString().slice(0, 10)
}

async function loadBacktest() {
  const day = isDayGrain.value
  try {
    const bt = await fetchPriceBacktest({
      market: market.value,
      grain: day ? 'day' : 'intraday',
      from: from.value,
      to: to.value,
      // 日频：一天一点，抽满 7 天也只要 7 次推理；96 点：每点一天 96 次配对，间隔抽样
      sampleDays: day ? 1 : 3,
      allowStale: day ? forceStale.value || undefined : true,
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
      skipped: bt.skipped,
      actualMissing: bt.actualMissing ?? 0,
      timeMs: bt.timeMs,
      dataAsOf: bt.dataAsOf ?? '',
      lagDays: bt.lagDays ?? -1,
      stale: bt.stale ?? false,
      truthSource: bt.truthSource ?? '',
      historyDays: bt.historyDays ?? 0,
    }
    staleNotice.value = ''
    staleBlocked.value = false
    sourceNote.value = day
      ? '回测：用**当前模型**、只用目标日之前的历史，在官方日均价上重跑。模型或历史数据一变，数字就变。'
      : '分时回测（历史演示）：真值来自已停更的第三方通道，只会给出 2026-10-02 之前的区间。'
  } catch (err) {
    // 真值滞后超阈值：后端拒绝在旧区间上算数字。这里不是「失败」，是「要不要看历史」的选择。
    if (err instanceof ApiError && err.code === CODE_STALE) {
      staleNotice.value = err.message
      staleBlocked.value = !forceStale.value
      pairs.value = []
      backtestMeta.value = null
      return
    }
    throw err
  }
}

async function loadRecorded() {
  const rec = await fetchRecordedForecast({
    market: market.value,
    grain: 'intraday',
    from: `${from.value} 00:00`,
    to: `${to.value} 23:59`,
  })
  pairs.value = await pairRecordedWithActual(rec.points)
  const times = rec.points.map((p) => p.targetTime).sort()
  recordedMeta.value = {
    generatedDates: rec.generatedDates,
    from: times[0] ?? '',
    to: times[times.length - 1] ?? '',
    count: rec.points.length,
  }
  sourceNote.value =
    '留痕：**当时**发出去的预测。这是真复盘，但留痕从启用之日起才开始积累。'
}

async function load() {
  loading.value = true
  error.value = ''
  pairs.value = []
  sourceNote.value = ''
  backtestMeta.value = null
  recordedMeta.value = null
  staleNotice.value = ''
  staleBlocked.value = false
  placeholderDropped.value = 0
  recordedActualUnavailable.value = ''
  try {
    if (source.value === 'backtest') await loadBacktest()
    else await loadRecorded()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

/** 用户明确要看陈旧区间（服务端 6001 之后的自选动作） */
async function viewStaleAnyway() {
  forceStale.value = true
  await load()
}

function switchGrain(next: Grain) {
  grain.value = next
  forceStale.value = false
  applyPreset()
  void load()
}

const disp = computed(() => dispersion(pairs.value.filter((p) => Number.isFinite(p.actual)), lookup.value))
const stats = computed(() => segmentStats(pairs.value.filter((p) => Number.isFinite(p.actual)), disp.value))
const overall = computed(() => stats.value.find((s) => s.segment === 'all'))
const days = computed(() => [...new Set(pairs.value.map((p) => String(p.targetTime).slice(0, 10)))].sort())

/** 日频回测的误差：一天一个点，MAE 与「占当日实际值的比例」才是可比的量 */
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
          name: '实际出清价',
          type: 'line',
          showSymbol: false,
          lineStyle: { width: 2 },
          itemStyle: { color: CHART_COLORS.blue },
          data: pairs.value.map((p) => (Number.isFinite(p.actual) ? p.actual : null)),
        },
        {
          name: '预测',
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.brand },
          data: pairs.value.map((p) => p.predicted),
        },
      ],
    }
  },
  [pairs],
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
  // 先问官方最新运行日，再定默认区间（拿不到就按今天，不影响可用性）
  try {
    const board = await fetchSpotMarketBoard({ days: 30 })
    officialLastDate.value = board.date || null
  } catch {
    officialLastDate.value = null
  }
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

  <div class="grid">
    <PanelBox title="数据来源" :span="12" :foot="false">
      <div class="toolbar">
        <span class="radio-group">
          <button type="button" :class="{ on: source === 'recorded' }" @click="((source = 'recorded'), load())">
            留痕对账
          </button>
          <button type="button" :class="{ on: source === 'backtest' }" @click="((source = 'backtest'), load())">
            事后回测
          </button>
        </span>
        <label class="field">
          市场
          <select v-model="market" @change="load">
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
        <label class="field">起<input v-model="from" type="date" /></label>
        <span class="muted">→</span>
        <label class="field">止<input v-model="to" type="date" /></label>
        <span class="spacer" />
        <button class="btn primary" type="button" :disabled="loading" @click="load">
          {{ loading ? '计算中…' : '查询' }}
        </button>
      </div>

      <div class="warn">
        <template v-if="source === 'recorded'">
          <strong>留痕对账</strong>
          <span>　用**当时**发出去的预测与实际比 —— 这才是复盘。但留痕从功能上线之日起才开始积累。</span>
          <template v-if="recordedMeta">
            <span class="muted">
              （样本 {{ recordedMeta.count }} 个点，覆盖 {{ recordedMeta.from || '—' }} ~ {{ recordedMeta.to || '—' }}；
              生成日 {{ recordedMeta.generatedDates.length }} 天{{
                recordedMeta.generatedDates.length ? `：${recordedMeta.generatedDates.slice(-3).join('、')}` : '，还没开始积累'
              }}）
            </span>
          </template>
        </template>
        <template v-else-if="isDayGrain">
          <strong>事后回测 · 官方日频</strong>
          <span>
            　真值 = 官方直采的<b>发电侧加权平均电价</b>（一天一个价），预测 = 当前模型只用目标日之前的历史重跑。
            数字会随模型/历史变化，<strong>不等于当时发出去的预测</strong>。
          </span>
        </template>
        <template v-else>
          <strong>事后回测 · 96 点（冻结）</strong>
          <span>
            　这条腿的真值来自<b>已停更</b>的第三方通道，官方披露没有 96 点分时口径。仅供历史演示，
            不要把这里的数字当成近期表现；要近期数据请切回「日频 · 官方」。
          </span>
        </template>
      </div>

      <!-- 诚实性：真值到哪天、滞后多少天，永远显示，不靠用户自己算 -->
      <div v-if="truthAsOf" class="warn">
        <strong>实际值截至 {{ truthAsOf }}（滞后 {{ lagDays }} 天）</strong>
        <span>
          　来源：{{ backtestMeta?.truthSource || (isDayGrain ? '官方直采（gd_spot_metric）' : '第三方通道（电查查 · 非官方 · T-7）') }}<template
            v-if="!isDayGrain"
          >　<span class="badge">第三方 · 非官方</span></template>。
          区间右端已锚到真值最新那天，所以「近 7 天」指的是 {{ truthAsOf }} 往前 7 天，不是今天往前 7 天。
        </span>
      </div>

      <div v-if="staleNotice" class="warn">
        <strong>真值滞后超出阈值，已拒绝默认回测</strong>
        <span>　{{ staleNotice }}</span>
        <button v-if="staleBlocked" class="btn" type="button" @click="viewStaleAnyway">
          仍要查看该历史区间（不代表近期）
        </button>
      </div>

      <div v-if="!segmentConfigured && !isDayGrain" class="warn">
        <strong>峰平谷时段未取到</strong>
        <span>　「管理 · 电价管理」里没有当前生效的时段配置，所有点都归入「全天」。设好时段后回来重看。</span>
      </div>

      <div v-if="placeholderDropped" class="warn">
        <strong>剔除了 {{ placeholderDropped }} 个无实际值的点</strong>
        <span>　这些时刻的实际值还没出（冻结通道里是电查查预测占位），不计入下面的指标。</span>
      </div>

      <div v-if="recordedActualUnavailable" class="warn">
        <strong>留痕与实际值配不上</strong>
        <span>　{{ recordedActualUnavailable }} —— 留痕本身还在，但没有可配对的实际值就不算指标（不硬凑）。</span>
      </div>
    </PanelBox>

    <PanelBox
      title="概览"
      :sub="`${pairs.length} 个对比点 · ${days.length} 天`"
      :span="12"
      :loading="loading"
      :foot="backtestMeta ? `模型 ${backtestMeta.model} · 抽样间隔 ${backtestMeta.sampleDays} 天 · 参与回测 ${backtestMeta.targetDates.length} 天${backtestMeta.skipped.length ? ` · 跳过 ${backtestMeta.skipped.length} 天` : ''}${backtestMeta.actualMissing ? ` · ${backtestMeta.actualMissing} 点无实际值已跳过` : ''} · 推理 ${backtestMeta.timeMs.toFixed(0)}ms` : sourceNote || '数据来源：留痕 / 回测'"
    >
      <div class="kpis">
        <template v-if="isDayGrain">
          <KpiCard label="平均绝对误差" :value="fmtNum(dayStats.mae, 1)" unit="元/MWh" hint="|预测 − 实际| 的均值（日均价）" />
          <KpiCard label="平均绝对百分比误差" :value="fmtNum(dayStats.mape, 2)" unit="%" hint="÷ 当日实际均价，负电价日按绝对值取分母" />
          <KpiCard label="对比天数" :value="dayStats.n" :hint="`目标日 ${backtestMeta?.targetDates.join('、') || '—'}`" />
          <KpiCard label="真值截至" :value="truthAsOf || '—'" :hint="`滞后 ${lagDays} 天 · ${backtestMeta?.truthSource || '官方直采'}`" />
        </template>
        <template v-else>
        <KpiCard
          label="趋势相关度（全天）"
          :value="fmtNum(overall?.rho)"
          :hint="overall?.rho == null ? '样本不足或秩无方差' : 'Spearman ρ，越接近 1 越准'"
        />
        <KpiCard
          label="平均离散度（全天）"
          :value="fmtNum(overall?.avgPct)"
          unit="标幺"
          hint="分母 = 当日出清均价"
        />
        <KpiCard label="平均绝对误差" :value="fmtNum(overall?.mae, 1)" unit="元/MWh" hint="|预测 − 实际| 的均值" />
        <KpiCard label="对比点数" :value="pairs.length" :hint="`覆盖 ${days.length} 个自然日`" />
        </template>
      </div>
    </PanelBox>

    <PanelBox
      v-if="isDayGrain"
      title="为什么没有峰平谷 / 逐点离散度"
      :span="12"
      :foot="false"
    >
      <div class="empty" style="padding: var(--s4)">
        官方披露只有<b>日均价</b>（加权平均/最高/最低/申报均价/成交量），没有 96 点分时口径，
        所以日频回测一天只有一个点：峰平谷分组、逐点离散度、Spearman ρ 在这里没有意义。
        按月/季看分时形态请用「信息披露」页的官方日频序列；96 点形态只能看冻结的历史演示。
      </div>
    </PanelBox>

    <PanelBox
      v-if="!isDayGrain"
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
      v-if="!isDayGrain"
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
      title="实际 vs 预测"
      :sub="isDayGrain ? '实际日均价（实线） / 预测日均价（虚线）· 一天一个点' : '实际出清价（实线） / 预测（虚线）'"
      :span="12"
      :loading="loading"
      :foot="isDayGrain ? '真值 = 官方直采的发电侧加权平均电价；预测 = 目标日之前的历史送入模型的输出' : '两条曲线都来自 15 分钟序列；时间轴按目标时刻'"
    >
      <div v-if="pairs.length" ref="chartEl" class="chart" />
      <div v-else-if="!loading" class="empty">
        <template v-if="source === 'recorded'">
          该区间没有留痕数据。留痕从功能上线之日起记录 —— 今天生成过预测后，明天起这里就有东西可看。
        </template>
        <template v-else-if="staleBlocked">
          真值滞后超出阈值，这里不给数字 —— 上面点「仍要查看该历史区间」才显示。
        </template>
        <template v-else-if="isDayGrain">
          日频回测没有产出对比点。多半是区间内没有官方已确认的实际值（真值截至 {{ truthAsOf || '未知' }}），换个区间再试。
        </template>
        <template v-else>
          回测没有产出对比点。96 点真值只到 {{ frozenAsOf }}，区间落在这之后就没有可配对的实际值。
        </template>
      </div>
    </PanelBox>

    <PanelBox
      v-if="!isDayGrain"
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
        <li><strong>趋势相关度</strong>：Spearman ρ（秩相关）。样本 &lt; 3 或某一侧秩无方差时显示「—」，不返回 0。</li>
        <li><strong>分时离散度</strong>：<code>|预测 − 实际| ÷ 当日出清均价</code>。当日均价接近 0 时标幺无意义，该点不参与均值。</li>
        <li>
          <strong>峰 / 平 / 谷</strong>：取自「管理 · 电价管理」的时段配置 —— 峰=Peak+Sharp，平=Flat，谷=Valley+DeepValley。
          当前{{ segmentConfigured ? '已取到' : '未取到（全部归入全天）' }}。
        </li>
        <li>
          <strong>真值来源</strong>：日频 = 官方直采（pm.gd.csg.cn 披露接口，
          <code>gd_spot_metric</code> 的「发电侧日前/实时加权平均电价」）；96 点 = 第三方 CSV，
          <b>已停更</b>，实际值止于 2026-10-02，只作历史演示。
        </li>
        <li>
          <strong>as-of 回测</strong>：预测目标日 D 时，只把 D-1 及以前的历史送进模型，不用「今天才知道的数据」，
          这样「当时能拿到的信息 + 现在的模型」才是回测，而不是事后诸葛亮。
        </li>
        <li><strong>回测 ≠ 当时预测</strong>：回测用当前模型重跑历史，数字会随后续模型/数据变化；留痕才是当时真实发出的预测。</li>
        <li>
          <strong>实际值只认 sourceId=9</strong>（广东电力交易中心）。CSV 里 999 是<strong>电查查的预测占位</strong>，
          出现在实际值还没出的时刻 —— 拿它当基准真值，算出来的是「两个模型谁准」，不是「预测准不准」。
          回测只挑有 96 点实际值的日子，留痕对账逐点剔除占位。
        </li>
        <li><strong>配对按时间戳</strong>：预测窗口与实际值只有时刻相同才配对，配不上的点丢弃 —— 不按「当天第几个点」硬凑。</li>
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
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.matrix { display: grid; grid-template-columns: 88px repeat(4, 1fr); gap: 1px; background: var(--border); border: 1px solid var(--border); border-radius: var(--r-sm); overflow: hidden; font-size: var(--fs-13); }
.matrix > div { padding: var(--s2) var(--s3); background: var(--bg-2); }
.matrix .h { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.matrix .num { text-align: right; font-variant-numeric: tabular-nums; }
.chart { height: 340px; }
.notes { margin: 0; padding-left: var(--s5); font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
.notes code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
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
