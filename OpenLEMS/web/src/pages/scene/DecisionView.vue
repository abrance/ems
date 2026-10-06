<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchBacktest, fetchEffectSummary, type EffectBacktest, type EffectSummary } from '@/api/effect'
import { fetchLoadForecast, fetchPriceForecast, type LoadForecast, type PriceForecast } from '@/api/forecast'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 决策支持。
 *
 * 内容来源：
 *   实测效果 KPI + 算法回测 → effect-overlay 迁入（/api/effect/*）
 *   负荷预测 / 电价预测     → TimesFM（/api/forecast/*）
 * 两套都走同一套成本模型/同一个预测服务，页面不另做估算。
 *
 * 「日前市场竞争空间」「历史相似日分析」没有数据源，底部明确列出，不造数。
 */
type Preset = 'week' | 'month' | 'custom'

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

const loading = reactive({ summary: false, backtest: false, load: false, price: false })
const errors = reactive<Record<string, string>>({})

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
  errors.load = ''
  errors.price = ''
  const [l, p] = await Promise.allSettled([
    fetchLoadForecast({ historyHours: 24, horizon: 96, forceRefresh: force }),
    fetchPriceForecast({ historyDays: 60, horizonDays: 5, forceRefresh: force }),
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
  loading.load = false
  loading.price = false
}

function refreshAll(force = false) {
  void Promise.all([loadSummary(), loadBacktest(), loadForecast(force)])
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
})

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
      { name: '历史（已确认日均）', type: 'line', showSymbol: false, itemStyle: { color: CHART_COLORS.blue }, data: [...(f?.historyValues ?? []), ...histPadTail] },
      { name: '预测 10%', type: 'line', showSymbol: false, lineStyle: { type: 'dotted', width: 1 }, itemStyle: { color: CHART_COLORS.gray }, data: q('0.1') },
      { name: '预测 90%', type: 'line', showSymbol: false, lineStyle: { type: 'dotted', width: 1 }, itemStyle: { color: CHART_COLORS.gray }, data: q('0.9') },
      { name: '预测中位', type: 'line', showSymbol: true, lineStyle: { type: 'dashed' }, itemStyle: { color: CHART_COLORS.brand }, data: [...fcPadHead, ...(f?.forecast ?? []).map((p) => p.point)] },
    ],
  }
})

onMounted(() => {
  applyPreset()
  refreshAll(false)
})
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
    </PanelBox>

    <PanelBox
      title="日前电价预测"
      sub="TimesFM · 日均序列"
      :span="12"
      :loading="loading.price"
      :foot="price ? `模型 ${price.model} · 历史 ${price.contextPoints} 点 · 区间 ${price.from} ~ ${price.to} · 缺数据 ${price.missingDays} 天 · 耗时 ${price.timeMs.toFixed(0)}ms` : '数据来源：/api/forecast/price'"
    >
      <div v-if="errors.price" class="empty">{{ errors.price }}</div>
      <template v-else>
        <div class="warn">
          预测是<strong>日均</strong>不是 96 点分时：日报未提取价格曲线，所以这里只能给日均与分位。
          分时预测要等图4 价格曲线纳入提取（见设计稿 §14.5）。
        </div>
        <div ref="priceEl" class="chart" />
      </template>
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

    <PanelBox title="待数据源的面板" :span="12" foot="设计稿 §5.3 里列出的其余两块">
      <div class="pending-list">
        <div class="pending">
          <StatusTag tone="brand">待数据源</StatusTag>
          <span>
            日前市场竞争空间：需要「火电最小出力 / 固定出力 / 风电 / 光伏 / 各类可调」分档数据，
            日报未提取。
          </span>
        </div>
        <div class="pending">
          <StatusTag tone="brand">待口径</StatusTag>
          <span>历史相似日分析：相似度判定依据（价格形态？负荷？新能源？）与阈值未定（§10.9）。</span>
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
