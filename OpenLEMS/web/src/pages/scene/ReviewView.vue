<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import {
  fetchPriceBacktest,
  fetchRecordedForecast,
  type BacktestPoint,
  type RecordedForecastPoint,
} from '@/api/forecast'
import { fetchGdMetric, fetchMetricSeries } from '@/api/gdmetric'
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
import { anchorEndDate, rangeEndingAt } from '@/utils/latestRange'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 预测复盘。
 *
 * 两条数据腿，页面上必须分清（这是「复盘」二字的全部意义）：
 *   留痕对账 —— **当时**发出去的预测 vs 实际。真实复盘，但今天才开始积累。
 *   事后回测 —— **用现在的模型**在历史数据上重跑 vs 实际。立刻能看，
 *               但模型或历史数据一变数字就变，不等于当时预测。
 *
 * 指标口径见 metrics/forecast.ts：Spearman ρ、离散度分母=当日均价、峰平谷取自电价管理。
 */
type Source = 'recorded' | 'backtest'

const PRESETS = [
  { v: '7', t: '近 7 天', days: 6 },
  { v: '14', t: '近 14 天', days: 13 },
  { v: '30', t: '近 30 天', days: 29 },
]

const source = ref<Source>('backtest')
const market = ref('ahead')
const preset = ref('7')
const from = ref('')
const to = ref('')

const pairs = ref<PairedPoint[]>([])
const loading = ref(false)
const error = ref('')
const sourceNote = ref('')
const backtestMeta = ref<{ model: string; sampleDays: number; targetDates: string[]; skipped: string[]; actualMissing: number; timeMs: number } | null>(null)
const recordedMeta = ref<{ generatedDates: string[] } | null>(null)

/** CSV 里的 sourceId：999 = 电查查预测占位（该时刻实际值还没出），9 = 广东电力交易中心实际值 */
const PLACEHOLDER_SOURCE = 999
/** 区间内被剔掉的占位点数（留痕那条腿按时间戳取实际值时统计） */
const placeholderDropped = ref(0)

/** 电价时段 → 峰平谷查表函数；拿不到配置时全部归「全天」 */
const lookup = ref<(m: number) => Segment | null>(() => null)
const segmentConfigured = ref(false)

/** 数据最新日：优先用**有实际值**的那天（latestDate 之后往往只有电查查预测占位） */
const latestDataDate = ref<string | null>(null)
/** CSV 最新日（可能只有预测占位），用来提示「实际值缺到哪天」 */
const latestAnyDate = ref<string | null>(null)

function applyPreset() {
  // 右端锚到「数据最新日」而不是今天：数据常滞后一个多月，
  // 今天往前 7 天的区间会整段落在数据之外 → 回测直接报「没有完整 96 点数据」
  const end = anchorEndDate(latestDataDate.value)
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
 */
async function pairRecordedWithActual(points: RecordedForecastPoint[]): Promise<PairedPoint[]> {
  if (!points.length) return []
  const days = [...new Set(points.map((p) => p.targetTime.slice(0, 10)))].sort()
  const series = await fetchMetricSeries({
    from: days[0],
    to: days[days.length - 1],
    kind: market.value === 'real' ? 'realtime' : 'dayAhead',
  })
  // 电查查预测占位（999）不是实际值，拿它当基准比出来的是「两个模型差多少」→ 剔除
  const real = series.points.filter((s) => s.sourceId !== PLACEHOLDER_SOURCE)
  placeholderDropped.value = series.points.length - real.length
  // 实际值按 "YYYY-MM-DD HH:MM" 建索引；CSV 的 24:00 归次日 00:00，与目标时刻的生成口径一致
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
}

function nextDay(date: string): string {
  const d = new Date(`${date}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + 1)
  return d.toISOString().slice(0, 10)
}

async function load() {
  loading.value = true
  error.value = ''
  pairs.value = []
  sourceNote.value = ''
  backtestMeta.value = null
  recordedMeta.value = null
  placeholderDropped.value = 0
  try {
    if (source.value === 'backtest') {
      // 回测按「目标日」抽样，区间用日期
      const bt = await fetchPriceBacktest({ market: market.value, from: from.value, to: to.value, sampleDays: 3 })
      pairs.value = bt.points.map((p: BacktestPoint) => ({
        targetTime: p.targetTime,
        predicted: p.predicted,
        actual: p.actual,
      }))
      backtestMeta.value = {
        model: bt.model,
        sampleDays: bt.sampleDays,
        targetDates: bt.targetDates,
        skipped: bt.skipped,
        actualMissing: bt.actualMissing ?? 0,
        timeMs: bt.timeMs,
      }
      sourceNote.value =
        '回测：用**当前模型**在历史数据上重跑，不是当时发出去的预测。模型或历史数据一变，数字就变。'
    } else {
      // 留痕按目标时刻，intraday 要带时间
      const rec = await fetchRecordedForecast({
        market: market.value,
        grain: 'intraday',
        from: `${from.value} 00:00`,
        to: `${to.value} 23:59`,
      })
      // 留痕只有预测这一条腿。实际值要按同样时刻去取 96 点序列才能配对——
      // 这条路径在下面 loadActualForRecorded() 里补，配对不上的点不计入指标。
      pairs.value = await pairRecordedWithActual(rec.points)
      recordedMeta.value = { generatedDates: rec.generatedDates }
      sourceNote.value =
        '留痕：**当时**发出去的预测。这是真复盘，但留痕从启用之日起才开始积累。'
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

const disp = computed(() => dispersion(pairs.value.filter((p) => Number.isFinite(p.actual)), lookup.value))
const stats = computed(() => segmentStats(pairs.value.filter((p) => Number.isFinite(p.actual)), disp.value))
const overall = computed(() => stats.value.find((s) => s.segment === 'all'))
const days = computed(() => [...new Set(pairs.value.map((p) => String(p.targetTime).slice(0, 10)))].sort())

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
  // 先问一次数据最新到哪天，再定默认区间（拿不到就按今天，不影响可用性）
  try {
    const snap = await fetchGdMetric({})
    latestAnyDate.value = snap.latestDate ?? null
    // 区间右端锚到「有实际值的那天」：latestDate 之后常常只有电查查预测占位，
    // 拿它当右端会得到一个整段算不出准确度的区间
    latestDataDate.value = snap.latestRealDate || snap.latestDate || null
  } catch {
    latestAnyDate.value = null
    latestDataDate.value = null
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
              （已有生成日 {{ recordedMeta.generatedDates.length }} 天{{
                recordedMeta.generatedDates.length ? `：${recordedMeta.generatedDates.slice(-3).join('、')}` : ''
              }}）
            </span>
          </template>
        </template>
        <template v-else>
          <strong>事后回测</strong>
          <span>
            　用**当前模型**在历史数据上重跑，立刻能看；但模型或历史数据一变数字就变，
            <strong>不等于当时发出去的预测</strong>。真正复盘请用「留痕对账」。
          </span>
        </template>
      </div>

      <div v-if="!segmentConfigured" class="warn">
        <strong>峰平谷时段未取到</strong>
        <span>　「管理 · 电价管理」里没有当前生效的时段配置，所有点都归入「全天」。设好时段后回来重看。</span>
      </div>

      <div v-if="latestAnyDate && latestDataDate && latestDataDate < latestAnyDate" class="warn">
        <strong>实际值只到 {{ latestDataDate }}</strong>
        <span>
          　CSV 里 {{ latestDataDate }} 之后（到 {{ latestAnyDate }}）只有<strong>电查查的预测占位</strong>，
          不是实际出清价 —— 拿它当基准比出来的只是「两个模型差多少」。区间右端已锚到实际值最新那天。
        </span>
      </div>

      <div v-if="placeholderDropped" class="warn">
        <strong>剔除了 {{ placeholderDropped }} 个无实际值的点</strong>
        <span>　这些时刻的实际值还没出（CSV 里是电查查预测占位），不计入下面的指标。</span>
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
      </div>
    </PanelBox>

    <PanelBox
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
      sub="实际出清价（实线） / 预测（虚线）"
      :span="12"
      :loading="loading"
      foot="两条曲线都来自 15 分钟序列；时间轴按目标时刻"
    >
      <div v-if="pairs.length" ref="chartEl" class="chart" />
      <div v-else-if="!loading" class="empty">
        <template v-if="source === 'recorded'">
          该区间没有留痕数据。留痕从功能上线之日起记录 —— 今天生成过预测后，明天起这里就有东西可看。
        </template>
        <template v-else>回测没有产出对比点。可能区间内没有完整（96 点）的实际数据，换个区间再试。</template>
      </div>
    </PanelBox>

    <PanelBox
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
