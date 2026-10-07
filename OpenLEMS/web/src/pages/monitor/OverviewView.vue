<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  fetchDetailedList,
  fetchHistory,
  fetchTelemetry,
  type DeviceDetailedInfo,
  type TelemetryData,
} from '@/api/device'
import { ALARM_LEVEL_TAG, fetchCurrentAlarms, type CurrentAlarm } from '@/api/alarm'
import { fetchEffectSummary, type EffectSummary } from '@/api/effect'
import { fetchLoadForecast } from '@/api/forecast'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 运行监控 · 能源总览。
 *
 * 侧栏面板清单是**可勾选**的（设计稿 §9.1 参考山西的「快速导航」），勾选即显隐，
 * 配置存 localStorage —— 不同岗位关心的东西不一样，不该由我们定死一张看板。
 *
 * 数据全部来自既有接口，这一页不新增后端。
 */
type PanelKey = 'topology' | 'status' | 'trend' | 'alarm' | 'saving' | 'forecast'

const PANELS: { key: PanelKey; title: string; desc: string }[] = [
  { key: 'topology', title: '能源拓扑', desc: '电网 / 光伏 / 储能 / 负载 的功率与流向' },
  { key: 'status', title: '实时状态', desc: '关口电压、有功、功率因数' },
  { key: 'trend', title: '功率趋势', desc: '电网 / 光伏 / 储能 功率曲线' },
  { key: 'alarm', title: '告警摘要', desc: '当前告警（按级别排序）' },
  { key: 'saving', title: '需量节省', desc: '近 7 天实测套利与需量节省' },
  { key: 'forecast', title: '负荷预测', desc: '模型给出的未来负荷曲线' },
]

const STORAGE_KEY = 'lems:monitor:panels'

const enabled = ref<Record<PanelKey, boolean>>({
  topology: true,
  status: true,
  trend: true,
  alarm: true,
  saving: false,
  forecast: false,
})

function loadPrefs() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) Object.assign(enabled.value, JSON.parse(raw))
  } catch {
    // 存坏了就用默认，不因为一个本地配置打不开页面
  }
}
watch(
  enabled,
  () => {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(enabled.value))
    } catch {
      // 忽略：存不下不影响使用
    }
  },
  { deep: true },
)

const devices = ref<DeviceDetailedInfo[]>([])
const telemetry = ref<Record<string, TelemetryData>>({})
const alarms = ref<CurrentAlarm[]>([])
const effect = ref<EffectSummary | null>(null)
const forecast = ref<{ times: string[]; values: number[] } | null>(null)
const trend = ref<{ name: string; data: number[] }[]>([])
const trendTimes = ref<string[]>([])
const loading = ref(false)
const error = ref('')

/** 按设备类型归类到四个节点。电表当关口（它本来就是「代表某个分组的表」）。 */
const NODES: { key: string; label: string; types: string[] }[] = [
  { key: 'grid', label: '电网关口', types: ['ammeter'] },
  { key: 'pv', label: '光伏', types: ['pv'] },
  { key: 'ess', label: '储能', types: ['ess', 'sess'] },
  { key: 'load', label: '负载', types: ['load'] },
]

// 点位键随驱动而异（ess_demo 是小写 power，某些驱动是 Power），所以匹配大小写不敏感；
// 再优先取**精确的** power/setPower 之外的那个 —— 否则可能选中 MaxChargePower 这类量。
const POWER_RE = /power|功率/i

function powerOf(id: string): { key: string; value: number; unit: string } | null {
  const values = telemetry.value[id]?.telemetryValues ?? {}
  const numeric = Object.entries(values).filter(([, v]) => typeof v === 'number')
  const exact = numeric.find(([k]) => k.toLowerCase() === 'power' || k === '功率')
  const entry = exact ?? numeric.find(([k]) => POWER_RE.test(k))
  if (!entry) return null
  return { key: entry[0], value: Number(entry[1]), unit: 'kW' }
}

const nodes = computed(() =>
  NODES.map((n) => {
    const list = devices.value.filter((d) => n.types.includes(d.deviceType))
    const rows = list.map((d) => ({ device: d, power: powerOf(d.id) }))
    const total = rows.reduce((s, r) => s + (r.power?.value ?? 0), 0)
    return { ...n, rows, total, hasValue: rows.some((r) => r.power) }
  }),
)

/** 关口的三相电压/有功/功率因数：能从点位原始 key 里认出多少就显示多少 */
const statusRows = computed(() => {
  const grid = devices.value.find((d) => d.deviceType === 'ammeter')
  if (!grid) return []
  const values = telemetry.value[grid.id]?.telemetryValues ?? {}
  return Object.entries(values)
    .filter(([k, v]) => typeof v === 'number' && /voltage|电压|power|功率|current|电流|factor|因数|frequency|频率/i.test(k))
    .slice(0, 9)
    .map(([k, v]) => ({ key: k, value: Number(v) }))
})

const topAlarms = computed(() =>
  [...alarms.value]
    .sort((a, b) => levelRank(b.level) - levelRank(a.level))
    .slice(0, 8),
)

function levelRank(l: string): number {
  return l === 'Error' ? 3 : l === 'Alarm' ? 2 : l === 'Warn' ? 1 : 0
}


/** 预测点的时间戳 → HH:MM（图上一列一列看，不显示完整日期） */
function forecastAxis(times: number[]): string[] {
  return times.map((t) => {
    const d = new Date(t)
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  })
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    devices.value = await fetchDetailedList()
    const tel = await fetchTelemetry('')
    telemetry.value = tel

    const [al, ef] = await Promise.all([
      fetchCurrentAlarms({ pageSize: 100 }).catch(() => ({ total: 0, ignoreTotal: 0, historyTotal: 0, items: [] })),
      enabled.value.saving ? fetchEffectSummary({ start: daysAgo(6), end: today() }).catch(() => null) : Promise.resolve(null),
    ])
    alarms.value = al.items
    effect.value = ef

    if (enabled.value.forecast) {
      try {
        const f = await fetchLoadForecast({ lookbackHours: 72, horizon: 24 })
        forecast.value = {
          times: forecastAxis(f.forecastTimes ?? []),
          values: (f.forecastPoint ?? []).map((v) => Number(v)),
        }
      } catch {
        forecast.value = null
      }
    }
    if (enabled.value.trend) await loadTrend()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

async function loadTrend() {
  const end = Date.now()
  const start = end - 12 * 3600_000
  const out: { name: string; data: number[] }[] = []
  let axis: string[] = []
  for (const n of NODES) {
    const d = devices.value.find((x) => n.types.includes(x.deviceType) && powerOf(x.id))
    if (!d) continue
    const p = powerOf(d.id)!
    try {
      const c = await fetchHistory({ deviceId: d.id, telemetryKeys: [p.key], startTime: start, endTime: end })
      if ((c.xAxis?.data ?? []).length > axis.length) axis = c.xAxis?.data ?? []
      out.push({ name: n.label, data: (c.series?.[0]?.data ?? []).map((v) => Number(v)) })
    } catch {
      // 单个节点取不到历史不影响其它曲线
    }
  }
  trendTimes.value = axis
  trend.value = out
}

function today(): string {
  return iso(new Date())
}
function daysAgo(n: number): string {
  return iso(new Date(Date.now() - n * 86400_000))
}
function iso(d: Date): string {
  const loc = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
  return loc.toISOString().slice(0, 10)
}

const trendEl = ref<HTMLElement>()
useECharts(
  trendEl,
  (): EChartsOption => ({
    grid: { left: 60, right: 20, top: 34, bottom: 48 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: {
      type: 'category',
      data: trendTimes.value,
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray, hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      name: 'kW',
      nameTextStyle: { fontSize: 12 },
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: 'var(--divider)' } },
    },
    series: trend.value.map((s, i) => ({
      name: s.name,
      type: 'line' as const,
      showSymbol: false,
      lineStyle: { width: 2 },
      itemStyle: { color: [CHART_COLORS.blue, CHART_COLORS.gold, CHART_COLORS.brand, CHART_COLORS.green][i % 4] },
      data: s.data,
    })),
  }),
  [trend, trendTimes],
)

const forecastEl = ref<HTMLElement>()
useECharts(
  forecastEl,
  (): EChartsOption => ({
    grid: { left: 60, right: 20, top: 28, bottom: 48 },
    tooltip: { trigger: 'axis' },
    xAxis: {
      type: 'category',
      data: forecast.value?.times ?? [],
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray, hideOverlap: true },
    },
    yAxis: {
      type: 'value',
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: 'var(--divider)' } },
    },
    series: [
      {
        name: '预测负荷',
        type: 'line',
        showSymbol: false,
        lineStyle: { width: 2, type: 'dashed' },
        itemStyle: { color: CHART_COLORS.brand },
        data: forecast.value?.values ?? [],
      },
    ],
  }),
  [forecast],
)

onMounted(async () => {
  loadPrefs()
  await load()
})

watch(
  () => ({ ...enabled.value }),
  async (now, before) => {
    // 关掉再打开的面板要重新取一次数（省掉了没勾选时的请求）
    if (now.saving && !before?.saving) {
      effect.value = await fetchEffectSummary({ start: daysAgo(6), end: today() }).catch(() => null)
    }
    if (now.forecast && !before?.forecast) {
      try {
        const f = await fetchLoadForecast({ lookbackHours: 72, horizon: 24 })
        forecast.value = {
          times: forecastAxis(f.forecastTimes ?? []),
          values: (f.forecastPoint ?? []).map((v) => Number(v)),
        }
      } catch {
        forecast.value = null
      }
    }
    if (now.trend && !before?.trend) await loadTrend()
  },
)
</script>

<template>
  <div class="layout">
    <aside class="quick">
      <div class="quick-title">面板</div>
      <label v-for="p in PANELS" :key="p.key" class="quick-item">
        <input v-model="enabled[p.key]" type="checkbox" />
        <span>
          <strong>{{ p.title }}</strong>
          <small>{{ p.desc }}</small>
        </span>
      </label>
      <p class="quick-note">勾选即显隐，配置存在本机（不跟账号走）。</p>
    </aside>

    <div class="grid">
      <div v-if="error" class="notice error" style="grid-column: span 12">
        <strong>加载失败</strong><span>　{{ error }}</span>
      </div>

      <PanelBox
        v-if="enabled.topology"
        title="能源拓扑"
        sub="按设备类型归类，节点名就是设备名"
        :span="12"
        :loading="loading"
        foot="电表按其定义「代表某个实际分组」处理，因此归到关口节点；净功率为各节点相加，仅作参考"
      >
        <div class="flow">
          <div v-for="n in nodes" :key="n.key" class="node">
            <div class="node-label">{{ n.label }}</div>
            <div class="node-value">
              {{ n.hasValue ? n.total.toFixed(1) : '—' }}<small>kW</small>
            </div>
            <div class="node-devices">
              <div v-for="r in n.rows" :key="r.device.id" class="node-device">
                <span class="dot" :class="{ off: !r.device.enabled || !!r.device.failedMessage }" />
                {{ r.device.name || r.device.id }}
                <span class="muted">{{ r.power ? r.power.value.toFixed(1) : '—' }}</span>
              </div>
              <div v-if="!n.rows.length" class="muted">无设备</div>
            </div>
          </div>
        </div>
        <p class="net">
          净功率（发电 + 放电 − 负载）：<strong>{{ (nodes.reduce((s, n) => s + n.total * (n.key === 'load' ? -1 : 1), 0)).toFixed(1) }}</strong>
          kW —— 正数表示向电网送电/多发电，负数表示从电网取电。这里只做各节点求和，不含线损与同时率。
        </p>
      </PanelBox>

      <PanelBox
        v-if="enabled.status"
        title="实时状态"
        sub="关口电表的关键量"
        :span="4"
        :loading="loading"
        foot="点位名与单位取自设备点位定义"
      >
        <div v-if="statusRows.length" class="kv">
          <div v-for="r in statusRows" :key="r.key" class="kv-row">
            <span class="k">{{ r.key }}</span>
            <span class="v">{{ r.value }}</span>
          </div>
        </div>
        <div v-else-if="!loading" class="empty">没有电表设备，或它还没有上报电压/电流类点位。</div>
      </PanelBox>

      <PanelBox
        v-if="enabled.trend"
        title="功率趋势"
        sub="近 12 小时"
        :span="enabled.status ? 8 : 12"
        :loading="loading"
        foot="每个节点取一台有功率点位的设备；曲线缺失说明该设备没有开启存储"
      >
        <div v-if="trend.length" ref="trendEl" class="chart" />
        <div v-else-if="!loading" class="empty">
          没有可画的历史：先确认设备点位的「存储」已开启（设备管理 · 点位设置）。
        </div>
      </PanelBox>

      <PanelBox
        v-if="enabled.alarm"
        title="告警摘要"
        :sub="`共 ${alarms.length} 条`"
        :span="12"
        :loading="loading"
        foot="按级别排序，只显示前 8 条；完整列表在「运行监控 · 告警中心」"
      >
        <template #actions>
          <RouterLink class="btn" to="/monitor/alarms">去告警中心</RouterLink>
        </template>
        <div v-if="topAlarms.length" class="table">
          <div class="th">级别</div>
          <div class="th">设备</div>
          <div class="th">点位</div>
          <div class="th">详情</div>
          <template v-for="(a, i) in topAlarms" :key="i">
            <div>
              <StatusTag :tone="(ALARM_LEVEL_TAG[a.level] ?? { tone: 'gray' }).tone as 'gray'">
                {{ (ALARM_LEVEL_TAG[a.level] ?? { label: a.level }).label }}
              </StatusTag>
            </div>
            <div>{{ a.deviceName || a.deviceId }}</div>
            <div>{{ a.pointName || a.point }}</div>
            <div class="detail">{{ a.detail }}</div>
          </template>
        </div>
        <div v-else-if="!loading" class="empty">当前没有告警。</div>
      </PanelBox>

      <PanelBox
        v-if="enabled.saving"
        title="需量节省"
        sub="近 7 天实测"
        :span="6"
        :loading="loading"
        foot="实测汇总（不是策略回测）；算法与设备口径见「场景 · 决策支持」"
      >
        <div v-if="effect" class="kpis">
          <KpiCard label="峰谷套利" :value="effect.arbitrageProfit.toFixed(1)" unit="元" hint="放电收入 − 充电成本" />
          <KpiCard label="需量节省" :value="effect.demandSaving.toFixed(1)" unit="元" hint="基线最大需量 − 实际最大需量" />
          <KpiCard label="合计节省" :value="effect.totalSaving.toFixed(1)" unit="元" hint="套利 + 需量" />
          <KpiCard label="统计点数" :value="effect.points" unit="点" :hint="`${effect.start} ~ ${effect.end}`" />
        </div>
        <div v-else-if="!loading" class="empty">该区间没有实测数据。</div>
      </PanelBox>

      <PanelBox
        v-if="enabled.forecast"
        title="负荷预测"
        :span="6"
        :loading="loading"
        foot="模型来自 /api/forecast/load；虚线表示预测"
      >
        <div v-if="forecast?.values?.length" ref="forecastEl" class="chart" />
        <div v-else-if="!loading" class="empty">预测服务未返回数据（检查微电网策略里的预测服务地址）。</div>
      </PanelBox>
    </div>
  </div>
</template>

<style scoped>
.layout { display: grid; grid-template-columns: 220px 1fr; gap: var(--s4); align-items: start; }
.quick {
  padding: var(--s3);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  position: sticky;
  top: var(--s4);
}
.quick-title { font-size: var(--fs-12); color: var(--text-3); margin-bottom: var(--s2); }
.quick-item {
  display: flex;
  gap: var(--s2);
  padding: var(--s2) var(--s1);
  border-radius: var(--r-xs);
  cursor: pointer;
}
.quick-item:hover { background: var(--bg-3); }
.quick-item input { margin-top: 2px; }
.quick-item span { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.quick-item strong { font-size: var(--fs-13); color: var(--text-1); font-weight: 500; }
.quick-item small { font-size: var(--fs-12); color: var(--text-3); line-height: 1.5; }
.quick-note { margin: var(--s3) 0 0; font-size: var(--fs-12); color: var(--text-3); line-height: 1.6; }
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.kpis { display: grid; grid-template-columns: repeat(2, 1fr); gap: var(--s3); }
.flow { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s3); }
.node {
  padding: var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-1);
}
.node-label { font-size: var(--fs-12); color: var(--text-3); }
.node-value { margin: var(--s1) 0 var(--s2); font-size: var(--fs-22); font-weight: 700; color: var(--text-1); font-variant-numeric: tabular-nums; }
.node-value small { margin-left: 3px; font-size: var(--fs-12); font-weight: 400; color: var(--text-3); }
.node-devices { display: flex; flex-direction: column; gap: var(--s1); }
.node-device { display: flex; align-items: center; gap: var(--s1); font-size: var(--fs-12); color: var(--text-2); }
.node-device .muted { margin-left: auto; color: var(--text-3); }
.dot { width: 6px; height: 6px; border-radius: 50%; background: var(--green); flex: 0 0 auto; }
.dot.off { background: var(--gray-ink); }
.net { margin: var(--s3) 0 0; font-size: var(--fs-12); color: var(--text-3); line-height: 1.8; }
.kv { display: flex; flex-direction: column; gap: var(--s1); }
.kv-row { display: flex; align-items: baseline; justify-content: space-between; gap: var(--s2); font-size: var(--fs-12); }
.kv-row .k { color: var(--text-3); }
.kv-row .v { color: var(--text-1); font-weight: 600; font-variant-numeric: tabular-nums; }
.table {
  display: grid;
  grid-template-columns: 80px 1.2fr 1.2fr 2fr;
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
.chart { height: 300px; }
.notice.error {
  display: flex;
  align-items: center;
  gap: var(--s3);
  padding: var(--s3) var(--s4);
  border-left: 3px solid var(--red);
  border-radius: var(--r-sm);
  background: var(--red-soft);
  color: var(--red-ink);
  font-size: var(--fs-13);
}
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.btn {
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-12);
  text-decoration: none;
  cursor: pointer;
}
@media (max-width: 1280px) {
  .layout { grid-template-columns: 1fr; }
  .quick { position: static; }
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .flow { grid-template-columns: repeat(2, 1fr); }
}
</style>
