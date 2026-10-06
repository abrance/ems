<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchDeviceNames, type DeviceType } from '@/api/device'
import { fetchResourceChart, type ChartData } from '@/api/system'
import {
  fetchDeviceCompare,
  fetchProtocolDistribution,
  fetchProtocolOverview,
  fetchProtocolTrend,
  PROTOCOL_METRICS,
  type ProtocolDistribution,
  type ProtocolOverview,
} from '@/api/monitor'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 管理 · 统计分析。三个页内 Tab，对应现网的 Device Comparison / System Monitor / Protocol Statistics。
 *
 * 设备对比与协议趋势都要求先选对象、再查 —— 不自动查「全部」：
 * 全表扫一遍在多设备场景下代价高，而且结果没人看得懂。
 */
const tab = ref<'device' | 'system' | 'protocol'>('device')

const toast = ref<{ tone: 'green' | 'red'; text: string } | null>(null)
let toastTimer: number | undefined
function showToast(tone: 'green' | 'red', text: string) {
  toast.value = { tone, text }
  window.clearTimeout(toastTimer)
  toastTimer = window.setTimeout(() => (toast.value = null), 4000)
}

const RANGES = [
  { v: '1h', t: '近 1 小时', ms: 3600_000, step: 0 },
  { v: '24h', t: '近 24 小时', ms: 86400_000, step: 0 },
  { v: '7d', t: '近 7 天', ms: 7 * 86400_000, step: 0 },
]

/* ---------- 设备对比 ---------- */
const devices = ref<Record<string, string>>({})
const deviceIds = ref<string[]>([])
const deviceRange = ref('24h')
const lineKeys = ref('Power')
const barKeys = ref('')
const deviceChart = ref<ChartData | null>(null)
const deviceLoading = ref(false)
const deviceError = ref('')

function rangeOf(v: string) {
  const hit = RANGES.find((r) => r.v === v) ?? RANGES[0]
  return { start: Date.now() - hit.ms, end: Date.now(), step: hit.step }
}

async function runDeviceCompare() {
  if (!deviceIds.value.length) {
    showToast('red', '请先选择至少一台设备')
    return
  }
  if (!lineKeys.value.trim() && !barKeys.value.trim()) {
    showToast('red', '请至少填一个点位（折线或柱状）')
    return
  }
  deviceLoading.value = true
  deviceError.value = ''
  const { start, end, step } = rangeOf(deviceRange.value)
  try {
    deviceChart.value = await fetchDeviceCompare({
      deviceIds: [...deviceIds.value],
      lineKeys: lineKeys.value.split(/[,，\s]+/).filter(Boolean),
      barKeys: barKeys.value.split(/[,，\s]+/).filter(Boolean),
      startTime: start,
      endTime: end,
      step,
    })
  } catch (err) {
    deviceChart.value = null
    deviceError.value = err instanceof Error ? err.message : '查询失败'
  } finally {
    deviceLoading.value = false
  }
}

const deviceEl = ref<HTMLElement>()
useECharts(
  deviceEl,
  (): EChartsOption => chartOption(deviceChart.value, '值'),
  [deviceChart],
)

/* ---------- 系统监控 ---------- */
const sysCategory = ref<'process' | 'network' | 'service' | 'storage'>('process')
const sysChart = ref<ChartData | null>(null)
const sysLoading = ref(false)
const sysError = ref('')

async function runSystemMonitor() {
  sysLoading.value = true
  sysError.value = ''
  const { start, end } = rangeOf('1h')
  try {
    sysChart.value = await fetchResourceChart(sysCategory.value, { startTime: start, endTime: end })
  } catch (err) {
    sysChart.value = null
    sysError.value = err instanceof Error ? err.message : '查询失败'
  } finally {
    sysLoading.value = false
  }
}

const sysEl = ref<HTMLElement>()
useECharts(
  sysEl,
  (): EChartsOption => chartOption(sysChart.value, '值'),
  [sysChart],
)

/* ---------- 协议统计 ---------- */
const overview = ref<ProtocolOverview | null>(null)
const distribution = ref<ProtocolDistribution | null>(null)
const metricKeys = ref<string[]>(['ProtocolMetricSuccessRate'])
const protoChart = ref<ChartData | null>(null)
const protoLoading = ref(false)
const protoError = ref('')

async function runProtocol() {
  protoLoading.value = true
  protoError.value = ''
  const { start, end } = rangeOf('1h')
  try {
    const [o, d] = await Promise.all([
      fetchProtocolOverview().catch(() => null),
      fetchProtocolDistribution().catch(() => null),
    ])
    overview.value = o
    distribution.value = d
    if (metricKeys.value.length) {
      protoChart.value = await fetchProtocolTrend(metricKeys.value, { startTime: start, endTime: end })
    } else {
      protoChart.value = null
    }
  } catch (err) {
    protoChart.value = null
    protoError.value = err instanceof Error ? err.message : '查询失败'
  } finally {
    protoLoading.value = false
  }
}

const protoEl = ref<HTMLElement>()
useECharts(
  protoEl,
  (): EChartsOption => chartOption(protoChart.value, '值'),
  [protoChart],
)

function toggleMetric(key: string) {
  const i = metricKeys.value.indexOf(key)
  if (i >= 0) metricKeys.value.splice(i, 1)
  else metricKeys.value.push(key)
}

const distTotal = computed(() => {
  const d = distribution.value?.typeDistribution ?? {}
  return Object.values(d).reduce((a, b) => a + b, 0)
})

/** 三张图共用一套构建逻辑：后端都是 c_chart.ChartData */
function chartOption(data: ChartData | null, unit: string): EChartsOption {
  const times = (data?.xAxis?.data ?? []).map((t) => {
    const n = Number(t)
    return Number.isNaN(n) ? t : new Date(n).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  })
  return {
    grid: { left: 56, right: 20, top: 30, bottom: 40 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: { type: 'category', data: times, axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: 30 } },
    yAxis: {
      type: 'value',
      name: unit,
      nameTextStyle: { fontSize: 12 },
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: '#eef0f3' } },
    },
    series: (data?.series ?? []).map((s, i) => ({
      name: `${s.name}${s.unit ? ` (${s.unit})` : ''}`,
      type: 'line' as const,
      smooth: true,
      showSymbol: false,
      itemStyle: { color: [CHART_COLORS.brand, CHART_COLORS.blue, CHART_COLORS.cyan, CHART_COLORS.purple, CHART_COLORS.green][i % 5] },
      data: s.data.map((v) => (v === '' ? null : Number(v))),
    })),
  }
}

onMounted(async () => {
  const types: DeviceType[] = ['pcs', 'bms', 'ess', 'sess', 'ammeter', 'load', 'pv']
  devices.value = await fetchDeviceNames(types).catch(() => ({}))
  const first = Object.keys(devices.value)[0]
  if (first) deviceIds.value = [first]
})
</script>

<template>
  <div v-if="toast" class="toast" :class="toast.tone">{{ toast.text }}</div>

  <div class="grid">
    <PanelBox title="统计分析" :span="12" :loading="false" :foot="false">
      <div class="tabs-inline">
        <button type="button" :class="{ on: tab === 'device' }" @click="tab = 'device'">设备对比</button>
        <button type="button" :class="{ on: tab === 'system' }" @click="tab = 'system'">系统监控</button>
        <button type="button" :class="{ on: tab === 'protocol' }" @click="tab = 'protocol'">协议统计</button>
      </div>

      <!-- 设备对比 -->
      <template v-if="tab === 'device'">
        <div class="form">
          <label class="wide">
            选择设备（可多选）
            <select v-model="deviceIds" multiple size="4">
              <option v-for="(name, id) in devices" :key="id" :value="id">{{ name }}（{{ id }}）</option>
            </select>
          </label>
          <label>折线点位<input v-model="lineKeys" placeholder="Power,SOC（逗号分隔）" /></label>
          <label>柱状点位<input v-model="barKeys" placeholder="可留空" /></label>
          <label>
            时间范围
            <select v-model="deviceRange">
              <option v-for="r in RANGES" :key="r.v" :value="r.v">{{ r.t }}</option>
            </select>
          </label>
        </div>
        <div class="actions">
          <span class="muted">已选 {{ deviceIds.length }} 台设备</span>
          <span class="spacer" />
          <button class="btn primary" type="button" :disabled="deviceLoading" @click="runDeviceCompare">
            {{ deviceLoading ? '查询中…' : '查询' }}
          </button>
        </div>
        <div v-if="deviceError" class="empty">{{ deviceError }}</div>
        <div v-else-if="deviceChart?.series?.length" ref="deviceEl" class="chart" />
        <div v-else class="empty">选设备、填点位后点「查询」。点位名要和设备点表里的一致。</div>
      </template>

      <!-- 系统监控 -->
      <template v-else-if="tab === 'system'">
        <div class="form">
          <label>
            资源类别
            <select v-model="sysCategory" @change="runSystemMonitor">
              <option value="process">进程资源</option>
              <option value="network">网络</option>
              <option value="service">服务</option>
              <option value="storage">存储</option>
            </select>
          </label>
        </div>
        <div class="actions">
          <span class="muted">区间：近 1 小时</span>
          <span class="spacer" />
          <button class="btn primary" type="button" :disabled="sysLoading" @click="runSystemMonitor">
            {{ sysLoading ? '查询中…' : '查询' }}
          </button>
        </div>
        <div v-if="sysError" class="empty">{{ sysError }}</div>
        <div v-else-if="sysChart?.series?.length" ref="sysEl" class="chart" />
        <div v-else class="empty">点「查询」拉取近 1 小时的资源曲线。</div>
      </template>

      <!-- 协议统计 -->
      <template v-else>
        <div class="kpis">
          <KpiCard label="协议总数" :value="overview?.totalProtocols ?? '—'" />
          <KpiCard label="活跃协议" :value="overview?.activeProtocols ?? '—'" />
          <KpiCard label="异常协议" :value="overview?.abnormalProtocols ?? '—'" :dir="overview?.abnormalProtocols ? 'up' : undefined" />
          <KpiCard label="平均成功率" :value="overview ? overview.avgSuccessRate.toFixed(1) : '—'" unit="%" />
          <KpiCard label="平均响应" :value="overview ? overview.avgResponseMs.toFixed(1) : '—'" unit="ms" />
          <KpiCard label="状态分布" :value="distribution ? `${distribution.statusDistribution.active}/${distTotal}` : '—'" hint="激活 / 总数" />
        </div>

        <div class="metrics">
          <span class="muted">指标</span>
          <button
            v-for="m in PROTOCOL_METRICS"
            :key="m.key"
            class="chip-toggle"
            :class="{ on: metricKeys.includes(m.key) }"
            type="button"
            @click="toggleMetric(m.key)"
          >
            {{ m.label }}
          </button>
        </div>

        <div class="actions">
          <span class="muted">区间：近 1 小时</span>
          <span class="spacer" />
          <button class="btn primary" type="button" :disabled="protoLoading" @click="runProtocol">
            {{ protoLoading ? '查询中…' : '查询' }}
          </button>
        </div>
        <div v-if="protoError" class="empty">{{ protoError }}</div>
        <div v-else-if="protoChart?.series?.length" ref="protoEl" class="chart" />
        <div v-else class="empty">点「查询」拉取协议指标趋势。不选指标时只出上方概览。</div>

        <div v-if="distribution" class="types">
          <StatusTag v-for="(n, t) in distribution.typeDistribution" :key="t" tone="blue">
            {{ t }}：{{ n }}
          </StatusTag>
        </div>
      </template>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.tabs-inline {
  display: flex;
  gap: var(--s5);
  margin: calc(-1 * var(--s4)) calc(-1 * var(--s4)) var(--s4);
  padding: 0 var(--s4);
  border-bottom: 1px solid var(--border);
}
.tabs-inline button {
  position: relative;
  padding: var(--s3) 0;
  border: 0;
  background: none;
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
.tabs-inline button.on { color: var(--brand-ink); font-weight: 600; }
.tabs-inline button.on::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 2px;
  background: var(--brand);
}
.form { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--s3) var(--s4); }
.form label { display: flex; flex-direction: column; gap: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.form label.wide { grid-column: span 1; }
.form input,
.form select {
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.form select[multiple] { min-height: 96px; }
.kpis { display: grid; grid-template-columns: repeat(6, 1fr); gap: var(--s4); margin-bottom: var(--s4); }
.metrics { display: flex; flex-wrap: wrap; align-items: center; gap: var(--s2); margin: var(--s3) 0; }
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
.actions { display: flex; align-items: center; gap: var(--s2); margin: var(--s3) 0; }
.spacer { flex: 1; }
.chart { height: 340px; }
.types { display: flex; flex-wrap: wrap; gap: var(--s2); margin-top: var(--s3); }
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
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
.toast {
  position: fixed;
  top: calc(var(--h-appbar) + var(--s2));
  left: 50%;
  z-index: 70;
  transform: translateX(-50%);
  padding: var(--s3) var(--s5);
  border-radius: var(--r-sm);
  font-size: var(--fs-13);
  box-shadow: 0 4px 12px rgb(0 0 0 / 12%);
}
.toast.green { background: var(--green-soft); color: var(--green-ink); }
.toast.red { background: var(--red-soft); color: var(--red-ink); }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(3, 1fr); }
  .form { grid-template-columns: 1fr; }
}
</style>
