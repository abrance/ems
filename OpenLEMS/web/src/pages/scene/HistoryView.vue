<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import TrendChart from '@/components/TrendChart.vue'
import { fetchRecordedForecast } from '@/api/forecast'
import { DATASET_LABELS, fetchTrend, listReports, type SpotTrend, type SpotTrendPoint } from '@/api/spotreport'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'
import { anchorEndDate } from '@/utils/latestRange'

/**
 * 场景 · 历史信息 —— overlay 趋势卡（spot-overlay.js）能力的迁入点。
 *
 * 数据来自 GET /api/spot-report/trend：按运行日分桶（日/周/月）聚合，**只看已确认**。
 * 「只看已确认」是后端在 SQL 里做的，被排除的条数由 excludedPending 带出来，
 * 页面上要显示，否则用户会以为数据少了。
 *
 * 设计稿 §5.7 里还列了火电启停/备用/调频/出清电量几块 —— 那些没有数据源，
 * 页面底部按「待数据源」明确列出，不画空图。
 */
const PRESETS: { v: 'week' | 'month' | 'custom'; t: string }[] = [
  { v: 'week', t: '本周' },
  { v: 'month', t: '本月' },
  { v: 'custom', t: '自定义' },
]

const GRAINS = [
  { value: '', label: '自动' },
  { value: 'day', label: '按日' },
  { value: 'week', label: '按周' },
  { value: 'month', label: '按月' },
]

const range = reactive<{ preset: 'week' | 'month' | 'custom'; from: string; to: string }>({
  preset: 'month',
  from: '',
  to: '',
})
const grain = ref('')

const trend = ref<SpotTrend | null>(null)
const loading = ref(false)
const error = ref('')

/** 勾选控制哪些序列画出来；默认全选 */
const hidden = reactive<Record<string, boolean>>({})
/** 勾选控制哪些数据集出图 */
const hiddenDataset = reactive<Record<string, boolean>>({})

function fmt(d: Date): string {
  const loc = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
  return loc.toISOString().slice(0, 10)
}

/** 最新日报日（现货指标的日期）；拿不到就用今天 */
const latestReportDate = ref<string | null>(null)

function applyPreset() {
  // 与预测复盘同理：日报数据可能滞后，默认区间落到「最新日报日」才不会一开就是空的
  const today = anchorEndDate(latestReportDate.value)
  if (range.preset === 'week') {
    // ISO 周：周一 → 今天，与后端「按周」分桶口径一致，不会错位
    const dow = (today.getDay() + 6) % 7
    const monday = new Date(today.getTime() - dow * 86400_000)
    range.from = fmt(monday)
    range.to = fmt(today)
  } else if (range.preset === 'month') {
    range.from = fmt(new Date(today.getFullYear(), today.getMonth(), 1))
    range.to = fmt(today)
  }
}

function seriesKey(p: SpotTrendPoint): string {
  return `${p.dataset}|${p.textValue || p.scope}|${p.metric}`
}

const allKeys = computed(() => {
  const set = new Set<string>()
  for (const p of trend.value?.points ?? []) set.add(seriesKey(p))
  return [...set]
})

const allDatasets = computed(() => {
  const set = new Set<string>()
  for (const p of trend.value?.points ?? []) set.add(p.dataset)
  return [...set].sort(
    (a, b) => Object.keys(DATASET_LABELS).indexOf(a) - Object.keys(DATASET_LABELS).indexOf(b),
  )
})

const visiblePoints = computed(() =>
  (trend.value?.points ?? []).filter((p) => !hidden[seriesKey(p)] && !hiddenDataset[p.dataset]),
)

/** 数据集 → 该数据集下的点 */
const byDataset = computed(() => {
  const map = new Map<string, SpotTrendPoint[]>()
  for (const p of visiblePoints.value) {
    if (!map.has(p.dataset)) map.set(p.dataset, [])
    map.get(p.dataset)!.push(p)
  }
  return [...map.entries()].sort(
    (a, b) => Object.keys(DATASET_LABELS).indexOf(a[0]) - Object.keys(DATASET_LABELS).indexOf(b[0]),
  )
})

/** 档位类数据集是分类值不是价格，单独提示，别当折线读 */
const BUCKET_DATASETS = new Set(['node_price_peak', 'node_price_valley'])

const buckets = computed(() => [...new Set(visiblePoints.value.map((p) => p.bucket))].sort())

const wideRows = computed(() => {
  const keys = subjects.value
  return buckets.value.map((b) => ({
    bucket: b,
    cells: keys.map((k) => {
      const hit = visiblePoints.value.find((p) => p.bucket === b && seriesKey(p) === k)
      return hit ? hit.avg : null
    }),
  }))
})

const subjects = computed(() => [...new Set(visiblePoints.value.map(seriesKey))])

function unitOf(key: string): string {
  const hit = visiblePoints.value.find((p) => seriesKey(p) === key)
  return hit?.unit ?? ''
}

function labelOf(key: string): string {
  const [dataset, scope, metric] = key.split('|')
  return `${DATASET_LABELS[dataset] ?? dataset} · ${scope} · ${metric}`
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    // **一律显式传区间**：后端的缺省是「本月 1 日 ~ 今天」，
    // 而日报数据常常滞后（部署上最新到 09-25，而当天是 10-07）→ 缺省区间整段是空的，
    // 页面就显示「当前区间没有已确认的现货指标」。
    // 区间由 applyPreset 算好（右端锚到最新日报日），这里只管传。
    trend.value = await fetchTrend({
      from: range.from || undefined,
      to: range.to || undefined,
      grain: grain.value || undefined,
    })
    // 后端可能对区间做收口（如不足一天补整），回填是为了让界面显示与实际查询一致
    if (trend.value && range.preset !== 'custom') {
      range.from = trend.value.from
      range.to = trend.value.to
    }
    for (const k of Object.keys(hidden)) delete hidden[k]
    for (const k of Object.keys(hiddenDataset)) delete hiddenDataset[k]
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
    trend.value = null
  } finally {
    loading.value = false
  }
}

/* ---------- 日前-实时出清与预测价格（设计稿 §5.7 的四条线） ---------- */

/** 一行 = 一个运行日 */
interface PriceCompareRow {
  date: string
  dayAhead: number | null
  realtime: number | null
  aheadForecast: number | null
  realForecast: number | null
}

const priceRows = ref<PriceCompareRow[]>([])
const priceLoading = ref(false)
const priceError = ref('')
/** 日报侧（实际）真实区间，来自 trend 的收口结果 */
const priceRange = ref('')
/** 有留痕的生成日：留痕是「当时真发的预测」，不是回测重跑 */
const recordedDays = ref<string[]>([])

/**
 * 四条线：实际两条与预测两条都取**日报域**。
 *
 * 为什么不用电网日指标 CSV 当实际：CSV 现在只覆盖到 2026-08-31，而留痕的目标运行日从 09-22 起，
 * 两者永远不重叠，图会永远只有两条线。日报的「日前/实时成交电价」与留痕是同一个域（同一个运行日），
 * 对得上才有意义。
 *
 * 本块**固定按运行日**（不跟上面的粒度走）：留痕天生就是一天一个点，混周/月会把口径搞乱。
 */
async function loadPriceCompare() {
  if (!range.from || !range.to) return
  priceLoading.value = true
  priceError.value = ''
  try {
    const [daily, ahead, real] = await Promise.all([
      fetchTrend({ from: range.from, to: range.to, grain: 'day' }),
      // 留痕查不到不算错（还没积累），降级成只有实际线
      fetchRecordedForecast({ from: range.from, to: range.to, market: 'ahead', grain: 'day' }).catch(() => null),
      fetchRecordedForecast({ from: range.from, to: range.to, market: 'real', grain: 'day' }).catch(() => null),
    ])
    priceRange.value = daily.from && daily.to ? `${daily.from} ~ ${daily.to}` : ''

    /** 只看「发电侧 · 平均电价」，与留痕的口径一致（别把燃煤/市场加权混进来） */
    const priceOf = (dataset: string): Map<string, number> => {
      const map = new Map<string, number>()
      for (const p of daily.points ?? []) {
        if (p.dataset === dataset && p.metric === '平均电价' && p.scope === '发电侧') map.set(p.bucket, p.avg)
      }
      return map
    }
    const aheadActual = priceOf('day_ahead_trade')
    const realActual = priceOf('realtime_trade')

    const byDate = new Map<string, PriceCompareRow>()
    for (const date of [...aheadActual.keys(), ...realActual.keys()].sort()) {
      byDate.set(date, {
        date,
        dayAhead: aheadActual.has(date) ? (aheadActual.get(date) as number) : null,
        realtime: realActual.has(date) ? (realActual.get(date) as number) : null,
        aheadForecast: null,
        realForecast: null,
      })
    }
    const fill = (
      points: { targetTime: string; point: number }[] | undefined,
      key: 'aheadForecast' | 'realForecast',
    ) => {
      for (const p of points ?? []) {
        const date = String(p.targetTime ?? '').slice(0, 10)
        const row = byDate.get(date)
        if (row) row[key] = p.point // 实际那两天没有记录的日子不硬凑轴
      }
    }
    fill(ahead?.points, 'aheadForecast')
    fill(real?.points, 'realForecast')

    priceRows.value = [...byDate.values()]
    recordedDays.value = [...new Set([...(ahead?.generatedDates ?? []), ...(real?.generatedDates ?? [])])].sort()
  } catch (err) {
    priceRows.value = []
    priceError.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    priceLoading.value = false
  }
}

const priceEl = ref<HTMLElement>()
useECharts(
  priceEl,
  (): EChartsOption => {
    const rows = priceRows.value
    const pick = (key: keyof PriceCompareRow) => rows.map((r) => (r[key] as number | null))
    return {
      grid: { left: 60, right: 20, top: 40, bottom: 48 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v}`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: rows.map((r) => r.date),
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: rows.length > 20 ? 40 : 0 },
      },
      yAxis: {
        type: 'value',
        name: '元/MWh',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        { name: '日前出清价（实际）', type: 'line', showSymbol: true, symbolSize: 5, itemStyle: { color: CHART_COLORS.brand }, data: pick('dayAhead') },
        { name: '实时出清价（实际）', type: 'line', showSymbol: true, symbolSize: 5, itemStyle: { color: CHART_COLORS.cyan }, data: pick('realtime') },
        // 预测一律虚线（与「实际」区分）；留痕没有的日期是 null，图直接断点
        { name: '日前预测价（留痕）', type: 'line', showSymbol: true, symbolSize: 5, lineStyle: { type: 'dashed' }, itemStyle: { color: CHART_COLORS.brand }, data: pick('aheadForecast') },
        { name: '实时预测价（留痕）', type: 'line', showSymbol: true, symbolSize: 5, lineStyle: { type: 'dashed' }, itemStyle: { color: CHART_COLORS.cyan }, data: pick('realForecast') },
      ],
    }
  },
  [priceRows],
)

const priceFoot = computed(() => {
  const parts = [`实际：官方直采（origin=pm-api）的日前/实时成交电价（发电侧·平均电价），区间 ${priceRange.value || '—'}`]
  parts.push(
    recordedDays.value.length
      ? `预测：留痕 ${recordedDays.value.length} 个生成日（${recordedDays.value[0]} 起）`
      : '预测：暂无留痕（这是「当时真发的预测」，不是回测重跑）',
  )
  return parts.join(' · ')
})

function reload() {
  void Promise.all([load(), loadPriceCompare()])
}

function onPreset(p: 'week' | 'month' | 'custom') {
  range.preset = p
  if (p !== 'custom') applyPreset()
  if (p !== 'custom') reload()
}

onMounted(async () => {
  // 先问一次最新日报日，再定默认区间：日报常滞后，今天往前一周可能整段是空的
  try {
    const reports = await listReports()
    const dates = reports.map((r) => String(r.date ?? '').slice(0, 10)).filter(Boolean).sort()
    latestReportDate.value = dates.length ? dates[dates.length - 1] : null
  } catch {
    latestReportDate.value = null
  }
  applyPreset()
  await Promise.all([load(), loadPriceCompare()])
})
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="reload">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="查询条件" :span="12" :foot="false">
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

        <label class="field">起<input v-model="range.from" type="date" /></label>
        <span class="muted">→</span>
        <label class="field">止<input v-model="range.to" type="date" /></label>

        <label class="field">
          粒度
          <select v-model="grain" @change="load">
            <option v-for="g in GRAINS" :key="g.value" :value="g.value">{{ g.label }}</option>
          </select>
        </label>

        <span class="spacer" />
        <button class="btn primary" type="button" :disabled="loading" @click="reload">
          {{ loading ? '查询中…' : '查询' }}
        </button>
      </div>

      <div class="toolbar second">
        <span class="muted">实际区间 {{ trend?.from || '—' }} → {{ trend?.to || '—' }}</span>
        <StatusTag tone="blue">粒度 {{ trend?.grain || '—' }}</StatusTag>
        <StatusTag v-if="trend?.excludedPending" tone="brand">
          已排除未复核 {{ trend.excludedPending }} 项
        </StatusTag>
        <StatusTag v-else tone="green">无未复核项被排除</StatusTag>
        <span class="spacer" />
        <span v-if="buckets.length" class="muted">共 {{ buckets.length }} 个桶</span>
      </div>
    </PanelBox>

    <PanelBox
      v-if="allKeys.length"
      title="指标多选"
      :sub="`${allKeys.length} 条序列 · 取消勾选即从图和宽表里移除`"
      :span="12"
      :foot="false"
    >
      <div class="chips">
        <button
          v-for="d in allDatasets"
          :key="d"
          class="chip-toggle"
          :class="{ on: !hiddenDataset[d] }"
          type="button"
          @click="hiddenDataset[d] = !hiddenDataset[d]"
        >
          {{ DATASET_LABELS[d] ?? d }}
        </button>
      </div>
      <div class="chips">
        <button
          v-for="k in allKeys"
          :key="k"
          class="chip-toggle"
          :class="{ on: !hidden[k] }"
          type="button"
          @click="hidden[k] = !hidden[k]"
        >
          {{ labelOf(k) }}
        </button>
      </div>
    </PanelBox>

    <PanelBox
      v-for="[dataset, points] in byDataset"
      :key="dataset"
      :title="DATASET_LABELS[dataset] ?? dataset"
      :sub="BUCKET_DATASETS.has(dataset) ? '档位计数（分类值，不是价格）' : `${new Set(points.map((p) => p.scope)).size} 个对象`"
      :span="12"
      :loading="loading"
      :foot="`数据来源：官方直采指标（origin=pm-api） · 按运行日分桶（${trend?.grain || 'day'}）`"
    >
      <TrendChart v-if="points.length" :points="points" />
      <div v-else class="empty">该数据集在当前区间没有已确认数据</div>
      <div v-if="buckets.length < 2" class="tip">
        当前区间只聚出 {{ buckets.length }} 个桶，图上看不出趋势。换个区间或把粒度调细。
      </div>
    </PanelBox>

    <PanelBox
      v-if="subjects.length"
      title="桶明细"
      sub="每行一个桶，每列一条序列（avg）"
      :span="12"
      :foot="false"
    >
      <div class="table-wrap">
        <table>
          <thead>
            <tr>
              <th>桶</th>
              <th v-for="k in subjects" :key="k" class="num">
                {{ labelOf(k) }}<span v-if="unitOf(k)" class="right">（{{ unitOf(k) }}）</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in wideRows" :key="row.bucket">
              <td>{{ row.bucket }}</td>
              <td v-for="(c, i) in row.cells" :key="i" class="num">{{ c ?? '—' }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </PanelBox>

    <PanelBox
      v-if="!allKeys.length && !loading"
      title="暂无数据"
      :span="12"
      :foot="false"
    >
      <div class="empty">
        当前区间没有已确认的现货指标。数据由官方直采（广东电力交易中心信息披露接口）
        入库，没有记录说明该区间还没有入库。
      </div>
    </PanelBox>

    <PanelBox
      title="日前-实时出清与预测价格"
      sub="按运行日（固定日粒度，不跟上面的粒度）· 实际两条 + 留痕预测两条"
      :span="12"
      :loading="priceLoading"
      :foot="priceFoot"
    >
      <div v-if="priceError" class="empty">{{ priceError }}</div>
      <template v-else>
        <div v-if="!recordedDays.length" class="hint">
          预测两条线来自<strong>留痕</strong>（当时真发出去的预测）：目前还没有留痕，所以只有实际两条线。
          留痕从 2026-10-08 起开始积累，攒起来后会自动出现；
          「用现在模型在历史重跑」是另一条腿，见「预测复盘」页，不画在这里。
        </div>
        <div v-if="priceRows.length" ref="priceEl" class="chart" />
        <div v-else class="empty">
          本区间没有已确认的日前/实时成交电价。数据同为官方直采入库，
          没有记录说明该区间还没有入库。
        </div>
      </template>
    </PanelBox>

    <PanelBox title="待数据源的面板" :span="12" foot="设计稿 §5.7 里列出的其余几块">
      <div class="pending-list">
        <div class="pending">
          <StatusTag tone="brand">待数据源</StatusTag>
          <span>火电启停状态统计（多日）、日前市场火电开机安排（多日）：日报未提取启停结果。</span>
        </div>
        <div class="pending">
          <StatusTag tone="brand">待数据源</StatusTag>
          <span>日前系统备用需求、日前系统调频辅助服务需求：单位与时段口径未定（§10.9）。</span>
        </div>
        <div class="pending">
          <StatusTag tone="brand">待口径</StatusTag>
          <span>日前-实时市场出清电量：「实时用量」是否为实时电量待确认。</span>
        </div>
      </div>
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
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.toolbar { display: flex; align-items: center; gap: var(--s3); flex-wrap: wrap; }
.toolbar.second { margin-top: var(--s3); gap: var(--s2); }
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
.chips { display: flex; flex-wrap: wrap; gap: var(--s1); margin-bottom: var(--s2); }
.chip-toggle {
  padding: var(--s1) var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.chip-toggle.on { background: var(--brand-soft); color: var(--brand-ink); font-weight: 600; }
.tip { margin-top: var(--s2); font-size: var(--fs-12); color: var(--brand-ink); }
.table-wrap { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--border);
  background: var(--bg-2);
  color: var(--text-3);
  font-size: var(--fs-12);
  font-weight: 500;
  text-align: left;
  white-space: nowrap;
}
td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); font-variant-numeric: tabular-nums; white-space: nowrap; }
th.num, td.num { text-align: right; }
.right { color: var(--text-3); font-size: var(--fs-12); }
.pending-list { display: flex; flex-direction: column; gap: var(--s3); }
.pending { display: flex; align-items: flex-start; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
.hint { margin-bottom: var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.chart { height: 360px; }
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
}
</style>
