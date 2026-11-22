<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import { fetchAlgoReview, runAlgoReview, type AlgoReview, type AlgoReviewPoint } from '@/api/forecast'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 算法回测。
 *
 * 只统计留痕：每天 00:05 写下的当时预测（gd_price_forecast 的 grain=hour 行），
 * 按算法与实际值（gd_home_24h）逐点配对。不重跑模型 —— 数字不会因为今天换了模型而变。
 * 融合（fusion）由后端用 TimesFM / GBM 的点预测均值实时算（LEAR 不参与融合），没有分位带。
 */

const PRESETS = [
  { v: '7', t: '近 7 天', days: 6 },
  { v: '30', t: '近 30 天', days: 29 },
  { v: '90', t: '近 90 天', days: 89 },
  { v: '180', t: '近 180 天', days: 179 },
]

const ALGO_META: Record<string, { label: string; color: string }> = {
  timesfm: { label: 'TimesFM', color: CHART_COLORS.brand },
  gbm: { label: 'GBM', color: CHART_COLORS.blue },
  lear: { label: 'LEAR', color: CHART_COLORS.green },
  fusion: { label: '融合（两者均值）', color: CHART_COLORS.purple },
}

const market = ref<'ahead' | 'real'>('ahead')
const selected = ref<string[]>(['timesfm', 'gbm', 'lear', 'fusion'])
const from = ref('')
const to = ref('')
const preset = ref('30')

const data = ref<AlgoReview | null>(null)
const loading = ref(false)
const error = ref('')

const detailDate = ref('')
const detail = ref<AlgoReviewPoint[]>([])
const detailLoading = ref(false)
const detailError = ref('')

const running = ref(false)
const runMsg = ref('')
const runSkipped = ref<string[]>([])

const visibleAlgos = computed(() => selected.value.filter((a) => ALGO_META[a]))

function fmtDay(d: Date): string {
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

function applyPreset(days: number) {
  const end = new Date()
  const start = new Date(end)
  start.setDate(start.getDate() - days)
  from.value = fmtDay(start)
  to.value = fmtDay(end)
}

function algoLabel(algo: string): string {
  return ALGO_META[algo]?.label ?? algo
}

function algoColor(algo: string): string {
  return ALGO_META[algo]?.color ?? CHART_COLORS.gray
}

function fmt(v: number | null | undefined, digits = 2): string {
  if (v === null || v === undefined || Number.isNaN(v)) return '—'
  return v.toFixed(digits)
}

function pct(v: number | null | undefined): string {
  if (v === null || v === undefined) return '—'
  return `${v.toFixed(1)}%`
}

const bestMae = computed(() => {
  const rows = (data.value?.summary ?? []).filter((s) => s.pairs > 0)
  if (!rows.length) return null
  return Math.min(...rows.map((s) => s.mae))
})

const gbmMissing = computed(() => {
  if (!data.value) return false
  const gbm = data.value.summary.find((s) => s.algo === 'gbm')
  return gbm !== undefined ? gbm.pairs === 0 : false
})

const learMissing = computed(() => {
  if (!data.value) return false
  const lear = data.value.summary.find((s) => s.algo === 'lear')
  return lear !== undefined ? lear.pairs === 0 : false
})

const dropped = computed(() => data.value?.dropped ?? { noActual: 0, backfilled: 0, incompleteDay: 0 })

async function load() {
  if (!from.value || !to.value) return
  loading.value = true
  error.value = ''
  try {
    // 算法在本地勾选/取消；接口一次返回全部，避免每点一个复选框就重算。
    data.value = await fetchAlgoReview({ from: from.value, to: to.value, market: market.value })
    const dates = data.value.daily.map((d) => d.date).sort()
    if (!detailDate.value || detailDate.value < from.value || detailDate.value > to.value) {
      detailDate.value = dates.length ? dates[dates.length - 1] : ''
    }
    if (detailDate.value) await loadDetail()
    else {
      detail.value = []
      detailError.value = ''
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
    data.value = null
  } finally {
    loading.value = false
  }
}

async function loadDetail() {
  if (!detailDate.value) return
  detailLoading.value = true
  detailError.value = ''
  try {
    const res = await fetchAlgoReview({
      from: from.value,
      to: to.value,
      market: market.value,
      date: detailDate.value,
    })
    detail.value = res.detail
  } catch (e) {
    detailError.value = e instanceof Error ? e.message : String(e)
    detail.value = []
  } finally {
    detailLoading.value = false
  }
}

async function runNow() {
  if (!from.value || !to.value) return
  runMsg.value = ''
  runSkipped.value = []
  running.value = true
  try {
    const res = await runAlgoReview({ from: from.value, to: to.value, market: market.value })
    const rows = Object.values(res.written).reduce((a, b) => a + b, 0)
    runMsg.value = `已运行 ${res.days} 天，写入 ${rows} 行留痕`
    runSkipped.value = res.skipped.slice(0, 8)
    await load()
  } catch (e) {
    runMsg.value = `运行失败：${e instanceof Error ? e.message : String(e)}`
  } finally {
    running.value = false
  }
}

function toggleAlgo(algo: string) {  const i = selected.value.indexOf(algo)
  if (i >= 0) {
    if (selected.value.length === 1) return // 至少留一个，空图没有意义
    selected.value.splice(i, 1)
  } else {
    selected.value.push(algo)
  }
}

function onPreset(v: string) {
  preset.value = v
  const p = PRESETS.find((x) => x.v === v)
  if (p) applyPreset(p.days)
  void load()
}

/** 逐日 MAE：x = 目标日，series = 每个算法 */
const dailyEl = ref<HTMLElement>()
useECharts(
  dailyEl,
  (): EChartsOption => {
    const dates = Array.from(new Set((data.value?.daily ?? []).map((d) => d.date))).sort()
    return {
      grid: { left: 60, right: 20, top: 36, bottom: 48 },
      tooltip: { trigger: 'axis' },
      legend: { top: 4 },
      xAxis: { type: 'category', data: dates, axisLabel: { fontSize: 10 } },
      yAxis: { type: 'value', name: data.value?.unit ?? '' },
      series: visibleAlgos.value.map((algo) => ({
        name: algoLabel(algo),
        type: 'line',
        showSymbol: false,
        connectNulls: false,
        itemStyle: { color: algoColor(algo) },
        data: dates.map((d) => data.value?.daily.find((r) => r.algo === algo && r.date === d)?.mae ?? null),
      })),
    }
  },
  [data, selected],
)

/** 24 小时误差画像：跨天按整点聚合的 MAE */
const hourlyEl = ref<HTMLElement>()
useECharts(
  hourlyEl,
  (): EChartsOption => {
    const hours = Array.from({ length: 24 }, (_, h) => h)
    return {
      grid: { left: 60, right: 20, top: 36, bottom: 40 },
      tooltip: { trigger: 'axis' },
      legend: { top: 4 },
      xAxis: {
        type: 'category',
        data: hours.map((h) => `${String(h).padStart(2, '0')}:00`),
        axisLabel: { fontSize: 10 },
      },
      yAxis: { type: 'value', name: data.value?.unit ?? '' },
      series: visibleAlgos.value.map((algo) => ({
        name: algoLabel(algo),
        type: 'bar',
        barGap: '10%',
        itemStyle: { color: algoColor(algo) },
        data: hours.map((h) => data.value?.hourly.find((r) => r.algo === algo && r.hour === h)?.mae ?? null),
      })),
    }
  },
  [data, selected],
)

/** 单日 24 点：实际 vs 各算法预测 */
const detailEl = ref<HTMLElement>()
useECharts(
  detailEl,
  (): EChartsOption => {
    const hours = Array.from({ length: 24 }, (_, h) => h)
    const actual = hours.map((h) => detail.value.find((p) => p.hour === h && p.actual !== null)?.actual ?? null)
    const series: EChartsOption['series'] = [
      {
        name: '实际',
        type: 'line',
        showSymbol: true,
        symbolSize: 5,
        lineStyle: { width: 3 },
        itemStyle: { color: CHART_COLORS.dark },
        data: actual,
      },
    ]
    for (const algo of visibleAlgos.value) {
      series.push({
        name: algoLabel(algo),
        type: 'line',
        showSymbol: false,
        connectNulls: false,
        itemStyle: { color: algoColor(algo) },
        data: hours.map((h) => detail.value.find((p) => p.algo === algo && p.hour === h)?.predicted ?? null),
      })
    }
    return {
      grid: { left: 60, right: 20, top: 36, bottom: 40 },
      tooltip: { trigger: 'axis' },
      legend: { top: 4 },
      xAxis: {
        type: 'category',
        data: hours.map((h) => `${String(h).padStart(2, '0')}:00`),
        axisLabel: { fontSize: 10 },
      },
      // 不强制从 0 起：单日三条线数值接近，从 0 起会把差异压成一条平线。
      yAxis: { type: 'value', name: data.value?.unit ?? '', scale: true, splitNumber: 4 },
      series,
    }
  },
  [detail, selected, data],
)

function exportCsv() {
  const d = data.value
  if (!d) return
  const lines: string[] = []
  lines.push(`算法回测,市场=${d.market},区间=${d.from}~${d.to},单位=${d.unit}`)
  lines.push('算法,天数,配对点,MAE,RMSE,MAPE(%),MAPE剔除,Bias,覆盖率(%)')
  for (const s of d.summary) {
    lines.push(
      [algoLabel(s.algo), s.days, s.pairs, fmt(s.mae), fmt(s.rmse), fmt(s.mape), s.mapeExcluded, fmt(s.bias), s.coverage === null ? '' : fmt(s.coverage)].join(','),
    )
  }
  lines.push('')
  lines.push('目标日,算法,配对点,MAE,MAPE(%),覆盖率(%)')
  for (const r of [...d.daily].sort((a, b) => (a.date === b.date ? a.algo.localeCompare(b.algo) : a.date.localeCompare(b.date)))) {
    lines.push([r.date, algoLabel(r.algo), r.pairs, fmt(r.mae), fmt(r.mape), r.coverage === null ? '' : fmt(r.coverage)].join(','))
  }
  const blob = new Blob([`\uFEFF${lines.join('\n')}`], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `algo-review_${d.from}_${d.to}_${d.market}.csv`
  a.click()
  URL.revokeObjectURL(url)
}

onMounted(() => {
  applyPreset(29)
  void load()
})
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <p class="lead">
    只统计<strong>留痕</strong>：每天 00:05 写下的当时预测，按算法与实际值配对。不重跑模型，数字不会因为今天换了模型而变。
    融合 = 同一目标时刻 TimesFM 与 GBM 点预测的均值，没有分位带，覆盖率显示「—」。
  </p>

  <div class="grid">
    <PanelBox title="回测条件" :span="12" :foot="false">
      <div class="toolbar">
        <span class="radio-group">
          <button
            v-for="p in PRESETS"
            :key="p.v"
            type="button"
            :class="{ on: preset === p.v }"
            @click="onPreset(p.v)"
          >
            {{ p.t }}
          </button>
        </span>
        <label class="field">
          市场
          <select v-model="market" @change="load">
            <option value="ahead">日前</option>
            <option value="real">实时</option>
          </select>
        </label>
        <span class="radio-group algos">
          <button
            v-for="a in ['timesfm', 'gbm', 'fusion']"
            :key="a"
            type="button"
            :class="{ on: selected.includes(a) }"
            @click="toggleAlgo(a)"
          >
            {{ algoLabel(a) }}
          </button>
        </span>
        <label class="field">起<input v-model="from" type="date" /></label>
        <span class="muted">→</span>
        <label class="field">止<input v-model="to" type="date" /></label>
        <span class="spacer" />
        <button class="btn" type="button" :disabled="running || loading" @click="runNow">
          {{ running ? '运行中…' : '运行预测' }}
        </button>
        <button class="btn" type="button" :disabled="loading || !data" @click="exportCsv">导出 CSV</button>
        <button class="btn primary" type="button" :disabled="loading" @click="load">
          {{ loading ? '计算中…' : '查询' }}
        </button>
      </div>

      <div class="run-hint">
        运行 = 对区间内每个目标日按 as-of 锚点跑 TimesFM + GBM 并写留痕（上限 31 天）；跑完统计会自动刷新。
      </div>
      <div v-if="runMsg" class="run-msg">{{ runMsg }}</div>
      <ul v-if="runSkipped.length" class="run-skip">
        <li v-for="m in runSkipped" :key="m">{{ m }}</li>
      </ul>

      <div class="warn">
        <strong>口径</strong>
        <span>
          预测 = <code>gd_price_forecast</code> 的 <code>grain=hour</code> 留痕；实际 =
          <code>gd_home_24h</code> 的日前/实时价。只保留「生成日 ≤ 目标日」的当时预测，事后回填的点剔除。
        </span>
      </div>

      <div class="fresh" :class="(data?.lagDays ?? 99) <= 2 ? 'ok' : (data?.lagDays ?? 99) <= 14 ? 'lag' : 'stop'">
        <span class="dot" />
        <template v-if="data">
          <strong>实际值截至 {{ data.dataAsOf || '—' }} · 滞后 {{ data.lagDays < 0 ? '未知' : data.lagDays + ' 天' }}</strong>
          <span class="muted">{{ data.truthSource }}</span>
        </template>
        <span v-else class="muted">加载后显示实际值截止日</span>
      </div>

      <div v-if="data" class="dropped">
        剔除：
        <span>实际值缺失 {{ dropped.noActual }} 点</span>
        <span>事后回填 {{ dropped.backfilled }} 点</span>
        <span>不足 24 点整日弃用 {{ dropped.incompleteDay }} 组</span>
      </div>
      <div v-if="gbmMissing" class="hint">
        GBM 还没有留痕：线上 lems 未配置 <code>PRICE_GBM_BASE_URL</code>（或 <code>price_gbm.base_url</code>），每日任务只写了 TimesFM。
      </div>
      <div v-if="learMissing" class="hint">
        LEAR 还没有留痕：线上 lems 未配置 <code>PRICE_LEAR_BASE_URL</code>（或 <code>price_lear.base_url</code>），每日任务只写了其他算法。
      </div>
    </PanelBox>

    <PanelBox title="算法对比" :span="12" :foot="`真值：${data?.truthSource ?? '官网主页 24 点分时'}；MAPE 仅统计 |实际| ≥ 1 元/MWh 的点`">
      <div v-if="loading" class="empty">加载中…</div>
      <div v-else-if="!data || data.summary.every((s) => s.pairs === 0)" class="empty">
        <div class="empty-title">区间内没有可配对的留痕</div>
        <div>留痕从每日任务启用后开始积累；换一个更大的区间，或确认每日任务是否在跑。</div>
      </div>
      <table v-else class="summary">
        <thead>
          <tr>
            <th>算法</th>
            <th class="num">有效天数</th>
            <th class="num">配对点</th>
            <th class="num">MAE</th>
            <th class="num">RMSE</th>
            <th class="num">MAPE</th>
            <th class="num">覆盖率</th>
            <th class="num">Bias</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in data.summary" :key="s.algo" :class="{ dim: s.pairs === 0 }">
            <td>
              <span class="swatch" :style="{ background: algoColor(s.algo) }" />
              {{ s.label }}
              <span v-if="bestMae !== null && s.pairs > 0 && s.mae === bestMae" class="tag best">MAE 最低</span>
              <span v-if="s.pairs === 0" class="tag">无留痕</span>
            </td>
            <td class="num">{{ s.days }}</td>
            <td class="num">{{ s.pairs }}</td>
            <td class="num strong">{{ fmt(s.mae) }}</td>
            <td class="num">{{ fmt(s.rmse) }}</td>
            <td class="num">{{ fmt(s.mape) }}%</td>
            <td class="num">{{ pct(s.coverage) }}</td>
            <td class="num">{{ fmt(s.bias) }}</td>
          </tr>
        </tbody>
      </table>
    </PanelBox>

    <PanelBox title="逐日 MAE" :span="6" :foot="false">
      <div ref="dailyEl" class="chart" />
    </PanelBox>

    <PanelBox title="24 小时误差画像（MAE）" :span="6" :foot="false">
      <div ref="hourlyEl" class="chart" />
    </PanelBox>

    <PanelBox title="单日明细 · 预测 vs 实际" :span="12" :foot="false">
      <div class="toolbar">
        <label class="field">
          目标日
          <input v-model="detailDate" type="date" :min="from" :max="to" @change="loadDetail" />
        </label>
        <span v-if="detailLoading" class="muted">加载中…</span>
        <span v-if="detailError" class="muted error-text">{{ detailError }}</span>
        <span class="spacer" />
        <span class="muted">图上的每条线是各算法的 24 点预测，深色为实际值</span>
      </div>
      <div ref="detailEl" class="chart" />
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
.radio-group { display: inline-flex; padding: 2px; border-radius: var(--r-sm); background: var(--bg-3); flex-wrap: wrap; }
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
.dropped {
  display: flex;
  gap: var(--s4);
  flex-wrap: wrap;
  margin-top: var(--s2);
  color: var(--text-3);
  font-size: var(--fs-12);
}
.hint {
  margin-top: var(--s2);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--gold);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-12);
}
.run-hint { margin-top: var(--s2); color: var(--text-3); font-size: var(--fs-12); }
.run-msg { margin-top: var(--s2); color: var(--green-ink); font-size: var(--fs-12); }
.run-skip { margin: var(--s1) 0 0; padding-left: var(--s4); color: var(--red-ink); font-size: var(--fs-12); }
.summary { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
.summary th,
.summary td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); text-align: left; }
.summary th { color: var(--text-3); font-weight: 500; font-size: var(--fs-12); background: var(--bg-3); }
.summary .num { text-align: right; font-variant-numeric: tabular-nums; }
.summary .strong { font-weight: 600; color: var(--text-1); }
.summary tr.dim { opacity: 0.55; }
.swatch { display: inline-block; width: 8px; height: 8px; margin-right: var(--s2); border-radius: 50%; }
.tag {
  margin-left: var(--s2);
  padding: 0 var(--s2);
  border-radius: var(--r-pill);
  background: var(--gray-soft);
  color: var(--gray-ink);
  font-size: var(--fs-12);
  white-space: nowrap;
}
.tag.best { background: var(--green-soft); color: var(--green-ink); }
.chart { height: 320px; }
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--s2);
  padding: var(--s5);
  color: var(--text-3);
  font-size: var(--fs-13);
  text-align: center;
}
.empty-title { font-size: var(--fs-16); font-weight: 600; color: var(--text-2); }
.muted { color: var(--text-3); font-size: var(--fs-12); font-weight: 400; }
.error-text { color: var(--red-ink); }
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
code { padding: 0 var(--s1); border-radius: var(--r-xs); background: var(--bg-3); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
}
</style>
