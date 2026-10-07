<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  fetchDetailedList,
  fetchHistory,
  fetchPointDefinitions,
  fetchTelemetry,
  type ChartData,
  type DeviceDetailedInfo,
  type PointDefinitions,
  type TelemetryData,
} from '@/api/device'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 运行监控 · 实时数据。
 *
 * 与管理·设备管理的分工：那边是**配置**（增删改、启停、点位参数），
 * 这边是**看**（此刻的值、趋势）。所以不设第二个启停入口，避免同一动作两个地方。
 *
 * 点位名/单位/枚举色的唯一来源是设备点位定义（`/device/{id}/points/definitions`）——
 * 页面里不写第二份映射表：设备型号不同，同一个 key 的含义和单位就可能不同。
 * 定义里没有的点位照样显示（退到原始 key），而不是藏起来装没有。
 */
const devices = ref<DeviceDetailedInfo[]>([])
const selectedId = ref('')
const telemetry = ref<TelemetryData | null>(null)
const defs = ref<PointDefinitions | null>(null)
const chart = ref<ChartData | null>(null)
const historyKey = ref('')
const historyHours = ref('6')
const loading = ref(false)
const error = ref('')

const TYPE_LABEL: Record<string, string> = {
  ess: '储能柜',
  sess: '总站储能',
  pcs: 'PCS',
  bms: 'BMS',
  ammeter: '电表',
  pv: '光伏',
  load: '负载',
  ca: '空调',
  cl: '液冷',
  fire: '消防',
  hum: '温湿度',
  gpio: 'GPIO',
  gpio_in: 'GPIO 输入',
  gpio_out: 'GPIO 输出',
  cp: '充电桩',
  gen: '发电机',
}

function typeLabel(t: string): string {
  return TYPE_LABEL[t] ?? t ?? '未知'
}

const selected = computed(() => devices.value.find((d) => d.id === selectedId.value) ?? null)

/** 按类型分组，侧栏才不会是一长串 */
const groups = computed(() => {
  const map = new Map<string, DeviceDetailedInfo[]>()
  for (const d of devices.value) {
    const key = d.deviceType || 'unknown'
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(d)
  }
  return [...map.entries()].map(([type, list]) => ({ type, label: typeLabel(type), list }))
})

interface Row {
  key: string
  name: string
  value: string
  unit: string
  color: string
  /** 有定义 = 有名字/单位/枚举说明；没有就是原始点位，界面要标出来 */
  defined: boolean
  numeric: boolean
}

/** 值 → 展示文本 + 颜色。枚举点位用定义里的 valueExplain，不自己编颜色表。 */
function explain(field: { valueExplain?: { key: string; value: string; color: string }[] } | undefined, raw: unknown) {
  const s = raw === null || raw === undefined ? '—' : String(raw)
  const hit = field?.valueExplain?.find((e) => e.key === s || e.key === String(Number(s)))
  return { text: hit?.value ?? s, color: hit?.color ?? '' }
}

const rows = computed<Row[]>(() => {
  const values = telemetry.value?.telemetryValues ?? {}
  const fieldByKey = new Map((defs.value?.fields ?? []).map((f) => [f.key, f]))
  const out: Row[] = []
  for (const [key, raw] of Object.entries(values)) {
    const f = fieldByKey.get(key)
    const info = explain(f, raw)
    out.push({
      key,
      name: f?.name || key,
      value: info.text,
      unit: f?.unit || '',
      color: info.color,
      defined: !!f,
      numeric: typeof raw === 'number',
    })
  }
  return out
})

const numericRows = computed(() => rows.value.filter((r) => r.numeric))

async function loadDevices() {
  devices.value = await fetchDetailedList()
  if (!selectedId.value && devices.value.length) selectedId.value = devices.value[0].id
}

async function loadDetail() {
  if (!selectedId.value) return
  loading.value = true
  error.value = ''
  try {
    const [tel, def] = await Promise.all([
      fetchTelemetry(selectedId.value),
      fetchPointDefinitions(selectedId.value).catch(() => null),
    ])
    telemetry.value = tel[selectedId.value] ?? null
    defs.value = def
    // 默认选第一个数值点位画趋势
    if (!historyKey.value || !numericRows.value.some((r) => r.key === historyKey.value)) {
      historyKey.value = numericRows.value[0]?.key ?? ''
    }
    await loadHistory()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
    telemetry.value = null
    defs.value = null
  } finally {
    loading.value = false
  }
}

async function loadHistory() {
  if (!selectedId.value || !historyKey.value) {
    chart.value = null
    return
  }
  const end = Date.now()
  const start = end - Number(historyHours.value) * 3600_000
  try {
    chart.value = await fetchHistory({
      deviceId: selectedId.value,
      telemetryKeys: [historyKey.value],
      startTime: start,
      endTime: end,
    })
  } catch {
    chart.value = null
  }
}

const chartEl = ref<HTMLElement>()
useECharts(
  chartEl,
  (): EChartsOption => {
    const c = chart.value
    if (!c) return {}
    const times = c.xAxis?.data ?? []
    const series = (c.series ?? []).map((s, i) => ({
      name: s.name,
      type: 'line' as const,
      showSymbol: false,
      lineStyle: { width: 2 },
      itemStyle: { color: [CHART_COLORS.brand, CHART_COLORS.blue, CHART_COLORS.cyan][i % 3] },
      data: (s.data ?? []).map((v) => Number(v)),
    }))
    return {
      grid: { left: 60, right: 20, top: 28, bottom: 48 },
      tooltip: { trigger: 'axis' },
      xAxis: {
        type: 'category',
        data: times,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, hideOverlap: true },
      },
      yAxis: {
        type: 'value',
        name: series[0]?.name ?? '',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series,
    }
  },
  [chart],
)

watch(selectedId, () => {
  telemetry.value = null
  defs.value = null
  historyKey.value = ''
  chart.value = null
  void loadDetail()
})
watch([historyKey, historyHours], () => void loadHistory())

onMounted(async () => {
  try {
    await loadDevices()
    await loadDetail()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  }
})
</script>

<template>
  <div class="grid">
    <PanelBox
      title="设备"
      :sub="`${devices.length} 台`"
      :span="4"
      :loading="loading"
      foot="配置设备请去「管理 · 设备管理」；这里只读"
    >
      <div v-if="error" class="alert error">{{ error }}</div>
      <div v-for="g in groups" :key="g.type" class="group">
        <div class="group-title">{{ g.label }}<span class="muted">（{{ g.list.length }}）</span></div>
        <button
          v-for="d in g.list"
          :key="d.id"
          type="button"
          class="dev"
          :class="{ on: d.id === selectedId }"
          @click="selectedId = d.id"
        >
          <span class="dev-name">{{ d.name || d.id }}</span>
          <StatusTag :tone="!d.enabled ? 'gray' : d.failedMessage ? 'red' : 'green'">
            {{ !d.enabled ? '已停用' : d.failedMessage ? '异常' : '正常' }}
          </StatusTag>
        </button>
      </div>
      <div v-if="!devices.length && !loading" class="empty">没有设备。</div>
    </PanelBox>

    <PanelBox
      title="实时值"
      :sub="selected ? selected.name || selected.id : '未选择设备'"
      :span="8"
      :loading="loading"
      :foot="telemetry?.lastUpdateTime ? `最近更新：${telemetry.lastUpdateTime}` : '数据来自设备遥测缓存'"
    >
      <div class="kpis">
        <KpiCard label="点位" :value="rows.length" unit="个" hint="该设备当前上报的全部点位" />
        <KpiCard
          label="可画趋势"
          :value="numericRows.length"
          unit="个"
          hint="数值型点位才能取历史曲线"
        />
        <KpiCard
          label="未定义点位"
          :value="rows.filter((r) => !r.defined).length"
          unit="个"
          hint="点位定义里没有它，只显示原始 key 与值"
        />
        <KpiCard
          label="设备类型"
          :value="selected ? typeLabel(selected.deviceType) : '—'"
          :hint="selected?.driver ? `驱动 ${selected.driver}` : '—'"
        />
      </div>

      <div v-if="rows.length" class="table">
        <div class="th">点位</div>
        <div class="th">值</div>
        <div class="th">单位</div>
        <div class="th">key</div>
        <template v-for="r in rows" :key="r.key">
          <div>{{ r.name }}</div>
          <div class="val" :style="r.color ? { color: r.color } : undefined">
            {{ r.value }}
            <small v-if="!r.defined" class="muted">未定义</small>
          </div>
          <div class="muted">{{ r.unit || '—' }}</div>
          <div class="muted">{{ r.key }}</div>
        </template>
      </div>
      <div v-else-if="selected && !loading" class="empty">该设备暂无遥测数据。</div>
      <div v-else-if="!selected" class="empty">先在左侧选一台设备。</div>
    </PanelBox>

    <PanelBox
      title="点位趋势"
      sub="历史数据来自存储（采样间隔由设备的存储设置决定）"
      :span="12"
      :loading="loading"
      foot="仅数值点位可画曲线；枚举/文本点位没有趋势可言"
    >
      <template #actions>
        <span class="toolbar">
          <label class="field">
            点位
            <select v-model="historyKey">
              <option value="">（无可选点位）</option>
              <option v-for="r in numericRows" :key="r.key" :value="r.key">{{ r.name }}</option>
            </select>
          </label>
          <span class="radio-group">
            <button v-for="h in ['1', '6', '24']" :key="h" type="button" :class="{ on: historyHours === h }" @click="historyHours = h">
              {{ h }}h
            </button>
          </span>
        </span>
      </template>
      <div v-if="chart && (chart.series ?? []).length" ref="chartEl" class="chart" />
      <div v-else-if="!loading && !numericRows.length" class="empty">该设备没有数值型点位，画不了趋势。</div>
      <div v-else-if="!loading" class="empty">该时间段没有历史数据（可能是存储间隔较长，或该点位未开启存储）。</div>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); margin-bottom: var(--s4); }
.group { margin-bottom: var(--s3); }
.group-title { padding: var(--s1) 0; font-size: var(--fs-12); color: var(--text-3); }
.dev {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--s2);
  padding: var(--s2) var(--s3);
  margin-bottom: var(--s1);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
  text-align: left;
  cursor: pointer;
}
.dev:hover { border-color: var(--brand); }
.dev.on { border-color: var(--brand); background: var(--brand-soft); }
.dev-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.toolbar { display: inline-flex; align-items: center; gap: var(--s3); }
.field { display: inline-flex; align-items: center; gap: var(--s2); font-size: var(--fs-12); color: var(--text-3); }
.field select {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.radio-group { display: inline-flex; padding: 2px; border-radius: var(--r-sm); background: var(--bg-3); }
.radio-group button {
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-xs);
  background: transparent;
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.radio-group button.on { background: var(--bg-2); color: var(--brand-ink); font-weight: 600; }
.table {
  display: grid;
  grid-template-columns: 1.6fr 1fr 70px 1.2fr;
  gap: 1px;
  background: var(--border);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
  font-size: var(--fs-13);
}
.table > div { padding: var(--s2) var(--s3); background: var(--bg-2); min-width: 0; }
.table .th { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.table .val { font-weight: 600; font-variant-numeric: tabular-nums; }
.table .muted { color: var(--text-3); font-size: var(--fs-12); }
.chart { height: 320px; }
.alert { margin-bottom: var(--s3); padding: var(--s2) var(--s3); border-radius: var(--r-sm); font-size: var(--fs-13); }
.alert.error { border-left: 3px solid var(--red); background: var(--red-soft); color: var(--red-ink); }
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(2, 1fr); }
}
</style>
