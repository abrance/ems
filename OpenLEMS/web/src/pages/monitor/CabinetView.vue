<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  fetchDetailedList,
  fetchHistory,
  fetchPointDefinitions,
  fetchTelemetry,
  type DeviceDetailedInfo,
  type PointDefinitions,
  type TelemetryData,
} from '@/api/device'
import { ALARM_LEVEL_TAG, fetchCurrentAlarms, type CurrentAlarm } from '@/api/alarm'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 运行监控 · 储能柜。
 *
 * 一台储能柜下面挂着 PCS / BMS / 电表 / 辅控（消防、空调、液冷、温湿度）——
 * 它们靠设备树的 `pid` 挂到柜子上，所以这一页按 pid 归拢，而不是靠设备名前缀猜。
 *
 * 控制动作不在这一页做：这里只读，动手去「管理 · 手动控制」——
 * 同一个动作留两个入口，迟早会出现两边行为不一致。
 */
interface Card {
  key: string
  title: string
  device: DeviceDetailedInfo
  rows: { name: string; value: string; unit: string; color: string }[]
}

const devices = ref<DeviceDetailedInfo[]>([])
const cabinets = computed(() => devices.value.filter((d) => d.deviceType === 'ess' || d.deviceType === 'sess'))
const selectedId = ref('')

const telemetry = ref<Record<string, TelemetryData>>({})
const defsByDevice = ref<Record<string, PointDefinitions>>({})
const alarms = ref<CurrentAlarm[]>([])
const series = ref<{ name: string; unit: string; data: number[] }[]>([])
const times = ref<string[]>([])
const hours = ref('12')
const loading = ref(false)
const error = ref('')

/** 柜子下面挂哪些子设备 */
const members = computed(() => devices.value.filter((d) => d.pid && d.pid === selectedId.value))
const cabinet = computed(() => devices.value.find((d) => d.id === selectedId.value) ?? null)

const CARD_ORDER: Array<[string, string]> = [
  ['pcs', 'PCS'],
  ['bms', 'BMS'],
  ['ammeter', '电表'],
  ['fire', '消防'],
  ['ca', '空调'],
  ['cl', '液冷'],
  ['hum', '温湿度'],
]

function fieldInfo(deviceId: string, key: string) {
  return (defsByDevice.value[deviceId]?.fields ?? []).find((f) => f.key === key)
}

function display(deviceId: string, key: string, raw: unknown) {
  const f = fieldInfo(deviceId, key)
  const s = raw === null || raw === undefined ? '—' : String(raw)
  const hit = f?.valueExplain?.find((e) => e.key === s || e.key === String(Number(s)))
  return { text: hit?.value ?? s, unit: f?.unit ?? '', color: hit?.color ?? '', name: f?.name || key }
}

const cards = computed<Card[]>(() => {
  const byType = new Map<string, DeviceDetailedInfo[]>()
  for (const m of members.value) {
    const t = m.deviceType || 'unknown'
    if (!byType.has(t)) byType.set(t, [])
    byType.get(t)!.push(m)
  }
  const out: Card[] = []
  for (const [type, label] of CARD_ORDER) {
    for (const d of byType.get(type) ?? []) {
      const values = telemetry.value[d.id]?.telemetryValues ?? {}
      const rows = Object.entries(values)
        .slice(0, 8)
        .map(([k, v]) => {
          const info = display(d.id, k, v)
          return { name: info.name, value: info.text, unit: info.unit, color: info.color }
        })
      out.push({ key: `${type}-${d.id}`, title: `${label} · ${d.name || d.id}`, device: d, rows })
    }
  }
  return out
})

/** 柜子自己 + 子设备的告警：告警常常挂在 PCS/BMS 上，只看柜子本身会漏 */
const cabinetAlarms = computed(() => {
  const ids = new Set([selectedId.value, ...members.value.map((m) => m.id)])
  return alarms.value.filter((a) => ids.has(a.deviceId) || ids.has(a.sourceDeviceId))
})

async function load() {
  if (!selectedId.value) return
  loading.value = true
  error.value = ''
  try {
    const [tel, , current] = await Promise.all([
      fetchTelemetry(selectedId.value),
      Promise.resolve(),
      fetchCurrentAlarms({ pageSize: 100 }).catch(() => ({ total: 0, ignoreTotal: 0, historyTotal: 0, items: [] })),
    ])
    alarms.value = current.items

    const telMap: Record<string, TelemetryData> = { ...tel }
    const defs: Record<string, PointDefinitions> = {}
    // 柜子自己 + 子设备都要取遥测与点位定义：卡片上的名字/单位/枚举色全来自定义
    const ids = [selectedId.value, ...members.value.map((m) => m.id)]
    await Promise.all(
      ids.map(async (id) => {
        if (!telMap[id]) {
          const t = await fetchTelemetry(id).catch(() => ({}))
          Object.assign(telMap, t)
        }
        const d = await fetchPointDefinitions(id).catch(() => null)
        if (d) defs[id] = d
      }),
    )
    telemetry.value = telMap
    defsByDevice.value = defs
    await loadTrend()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

/** 趋势挑三类关键量：功率（PCS）、SOC（BMS）、温度（BMS/液冷） */
const TREND_KEYS: Record<string, string[]> = {
  pcs: ['power'],
  bms: ['soc', 'temperature', 'soh'],
  cl: ['temperature'],
}

async function loadTrend() {
  const end = Date.now()
  const start = end - Number(hours.value) * 3600_000
  const collected: { name: string; unit: string; data: number[] }[] = []
  let axis: string[] = []
  for (const d of members.value) {
    // 点位键大小写随驱动而异，按定义里的实际 key 做不敏感匹配
    const want = TREND_KEYS[d.deviceType] ?? []
    const keys = (defsByDevice.value[d.id]?.fields ?? [])
      .map((f) => f.key)
      .filter((k) => want.includes(k.toLowerCase()) && fieldInfo(d.id, k))
    if (!keys.length) continue
    try {
      const c = await fetchHistory({ deviceId: d.id, telemetryKeys: keys, startTime: start, endTime: end })
      if ((c.xAxis?.data ?? []).length > axis.length) axis = c.xAxis?.data ?? []
      for (const s of c.series ?? []) {
        collected.push({
          name: `${d.name || d.id} ${fieldInfo(d.id, s.name)?.name || s.name}`,
          unit: s.unit ?? '',
          data: (s.data ?? []).map((v) => Number(v)),
        })
      }
    } catch {
      // 单个设备取不到历史不影响其它曲线
    }
  }
  times.value = axis
  series.value = collected
}

const chartEl = ref<HTMLElement>()
useECharts(
  chartEl,
  (): EChartsOption => ({
    grid: { left: 60, right: 20, top: 34, bottom: 48 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: {
      type: 'category',
      data: times.value,
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray, hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: 'var(--divider)' } },
    },
    series: series.value.map((s, i) => ({
      name: s.name,
      type: 'line' as const,
      showSymbol: false,
      lineStyle: { width: 2 },
      itemStyle: { color: [CHART_COLORS.brand, CHART_COLORS.blue, CHART_COLORS.cyan, CHART_COLORS.green][i % 4] },
      data: s.data,
    })),
  }),
  [series, times],
)

watch(selectedId, () => {
  telemetry.value = {}
  defsByDevice.value = {}
  series.value = []
  times.value = []
  void load()
})
watch(hours, () => void loadTrend())

onMounted(async () => {
  try {
    devices.value = await fetchDetailedList()
    selectedId.value = cabinets.value[0]?.id ?? ''
    if (selectedId.value) await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  }
})
</script>

<template>
  <div class="grid">
    <PanelBox
      title="储能柜"
      :sub="`${cabinets.length} 台`"
      :span="4"
      :loading="loading"
      foot="子设备按设备树的 pid 归到柜子上，不靠设备名猜"
    >
      <div v-if="error" class="alert error">{{ error }}</div>
      <button
        v-for="c in cabinets"
        :key="c.id"
        type="button"
        class="dev"
        :class="{ on: c.id === selectedId }"
        @click="selectedId = c.id"
      >
        <span class="dev-name">{{ c.name || c.id }}</span>
        <StatusTag :tone="!c.enabled ? 'gray' : c.failedMessage ? 'red' : 'green'">
          {{ !c.enabled ? '已停用' : c.failedMessage ? '异常' : '正常' }}
        </StatusTag>
      </button>
      <div v-if="!cabinets.length && !loading" class="empty">没有储能柜设备。</div>
    </PanelBox>

    <PanelBox
      title="柜内设备"
      :sub="cabinet ? `${cabinet.name || cabinet.id} · ${members.length} 台子设备` : '未选择'"
      :span="8"
      :loading="loading"
      :foot="'控制动作在「管理 · 手动控制」里做，这一页只读'"
    >
      <template #actions>
        <RouterLink class="btn" to="/manage/manual">去手动控制</RouterLink>
      </template>
      <div v-if="cards.length" class="cards">
        <div v-for="c in cards" :key="c.key" class="card">
          <div class="card-head">
            <span class="card-title">{{ c.title }}</span>
            <StatusTag :tone="c.device.failedMessage ? 'red' : 'green'">
              {{ c.device.failedMessage ? '异常' : '正常' }}
            </StatusTag>
          </div>
          <div v-if="c.rows.length" class="card-rows">
            <div v-for="r in c.rows" :key="r.name" class="row">
              <span class="k">{{ r.name }}</span>
              <span class="v" :style="r.color ? { color: r.color } : undefined">
                {{ r.value }}<small v-if="r.unit">{{ r.unit }}</small>
              </span>
            </div>
          </div>
          <div v-else class="card-empty">暂无遥测</div>
        </div>
      </div>
      <div v-else-if="cabinet && !loading" class="empty">该柜子下没有挂子设备（检查设备树的父设备设置）。</div>
    </PanelBox>

    <PanelBox
      title="关键量趋势"
      sub="功率（PCS）/ SOC（BMS）/ 温度"
      :span="12"
      :loading="loading"
      foot="多条曲线叠在一张图里：同一时间轴才好比较充放与温升的关系"
    >
      <template #actions>
        <span class="radio-group">
          <button v-for="h in ['1', '12', '24', '72']" :key="h" type="button" :class="{ on: hours === h }" @click="hours = h">
            {{ h }}h
          </button>
        </span>
      </template>
      <div v-if="series.length" ref="chartEl" class="chart" />
      <div v-else-if="!loading" class="empty">没有可画的曲线（该柜子下没有开启存储的数值点位）。</div>
    </PanelBox>

    <PanelBox
      title="柜内告警"
      :sub="`${cabinetAlarms.length} 条`"
      :span="12"
      :loading="loading"
      foot="含子设备告警：告警常挂在 PCS/BMS 上，只看柜子本身会漏"
    >
      <div v-if="cabinetAlarms.length" class="table">
        <div class="th">级别</div>
        <div class="th">设备</div>
        <div class="th">点位</div>
        <div class="th">详情</div>
        <div class="th">时间</div>
        <template v-for="(a, i) in cabinetAlarms" :key="`${a.deviceId}-${a.point}-${i}`">
          <div>
            <StatusTag :tone="(ALARM_LEVEL_TAG[a.level] ?? { tone: 'gray' }).tone as 'gray'">
              {{ (ALARM_LEVEL_TAG[a.level] ?? { label: a.level }).label }}
            </StatusTag>
          </div>
          <div>{{ a.deviceName || a.deviceId }}</div>
          <div>{{ a.pointName || a.point }}</div>
          <div class="detail">{{ a.detail }}</div>
          <div class="muted">{{ a.createdAt ?? '—' }}</div>
        </template>
      </div>
      <div v-else-if="!loading" class="empty">该柜子当前没有告警。</div>
    </PanelBox>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
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
  cursor: pointer;
}
.dev:hover { border-color: var(--brand); }
.dev.on { border-color: var(--brand); background: var(--brand-soft); }
.dev-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.cards { display: grid; grid-template-columns: repeat(3, 1fr); gap: var(--s3); }
.card {
  padding: var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-1);
}
.card-head { display: flex; align-items: center; justify-content: space-between; gap: var(--s2); margin-bottom: var(--s2); }
.card-title { font-size: var(--fs-13); font-weight: 600; color: var(--text-1); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.card-rows { display: flex; flex-direction: column; gap: var(--s1); }
.row { display: flex; align-items: baseline; justify-content: space-between; gap: var(--s2); font-size: var(--fs-12); }
.row .k { color: var(--text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row .v { color: var(--text-1); font-weight: 600; font-variant-numeric: tabular-nums; }
.row .v small { margin-left: 2px; font-weight: 400; color: var(--text-3); }
.card-empty { font-size: var(--fs-12); color: var(--text-3); }
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
  grid-template-columns: 80px 1.2fr 1.2fr 2fr 1.4fr;
  gap: 1px;
  background: var(--border);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
  font-size: var(--fs-13);
}
.table > div { padding: var(--s2) var(--s3); background: var(--bg-2); min-width: 0; }
.table .th { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.table .detail { color: var(--text-2); overflow-wrap: anywhere; }
.table .muted { color: var(--text-3); font-size: var(--fs-12); }
.chart { height: 340px; }
.alert { margin-bottom: var(--s3); padding: var(--s2) var(--s3); border-radius: var(--r-sm); font-size: var(--fs-13); }
.alert.error { border-left: 3px solid var(--red); background: var(--red-soft); color: var(--red-ink); }
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.btn {
  padding: var(--s1) var(--s4);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-13);
  text-decoration: none;
  cursor: pointer;
}
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .cards { grid-template-columns: 1fr; }
}
</style>
