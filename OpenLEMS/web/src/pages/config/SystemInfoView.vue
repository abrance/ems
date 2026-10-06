<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  fetchNetTraffic,
  fetchResourceChart,
  fetchSystemSummary,
  humanBytes,
  humanRate,
  type NetBrief,
  type SystemSummary,
} from '@/api/system'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 配置 · 系统信息。
 *
 * 数据来自 /api/system/*（CPU / 内存 / 磁盘 / 网络 / 运行时长 / 基础信息）。
 * 顶栏刻意不放这些实时指标（设计稿 §2 原则 6），它们属于这一页。
 *
 * 页面只读，但会 30 秒自动刷新一次 —— 系统信息看的是「现在」。
 */
const summary = ref<SystemSummary | null>(null)
const net = ref<NetBrief | null>(null)
const loading = ref(false)
const error = ref('')
let timer: number | undefined

function pct(v: number | undefined): string {
  return v === undefined || v === null ? '—' : `${v.toFixed(1)}`
}

const uptimeText = computed(() => {
  const u = summary.value?.uptime
  if (!u) return '—'
  const procDays = Math.floor((u.procUptimeSeconds ?? 0) / 86400)
  const procHours = Math.floor(((u.procUptimeSeconds ?? 0) % 86400) / 3600)
  return {
    system: `${u.day} 天 ${u.hour} 小时 ${u.minute} 分`,
    process: `${procDays} 天 ${procHours} 小时`,
    boot: u.bootTime,
    procStart: u.procStartTime,
  }
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [s, n] = await Promise.all([fetchSystemSummary(), fetchNetTraffic().catch(() => null)])
    summary.value = s
    net.value = n
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

/* ---------- 资源趋势 ---------- */
const chartEl = ref<HTMLElement>()
const chartSeries = ref<{ name: string; type: string; unit?: string; data: string[] }[]>([])
const chartX = ref<string[]>([])
const chartLoaded = ref(false)

// 图表在数据到达之前就挂载了，所以把数据依赖交给 composable 驱动重绘
useECharts(
  chartEl,
  (): EChartsOption => ({
    grid: { left: 56, right: 20, top: 28, bottom: 36 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: {
      type: 'category',
      data: chartX.value.map((t) => {
        const n = Number(t)
        return Number.isNaN(n)
          ? t
          : new Date(n).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
      }),
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
    },
    yAxis: {
      type: 'value',
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: '#eef0f3' } },
    },
    series: chartSeries.value.map((sr, i) => ({
      name: `${sr.name}${sr.unit ? ` (${sr.unit})` : ''}`,
      type: 'line' as const,
      smooth: true,
      showSymbol: false,
      itemStyle: { color: [CHART_COLORS.brand, CHART_COLORS.blue, CHART_COLORS.cyan, CHART_COLORS.purple][i % 4] },
      data: sr.data.map((v) => (v === '' ? null : Number(v))),
    })),
  }),
  [chartSeries, chartX],
)

async function loadChart() {
  const end = Date.now()
  try {
    const data = await fetchResourceChart('process', { startTime: end - 3600_000, endTime: end })
    chartSeries.value = data?.series ?? []
    chartX.value = data?.xAxis?.data ?? []
    chartLoaded.value = chartSeries.value.length > 0
  } catch {
    chartLoaded.value = false
  }
}

onMounted(async () => {
  await load()
  await loadChart()
  timer = window.setInterval(() => {
    if (!document.hidden) void load()
  }, 30000)
})
onBeforeUnmount(() => window.clearInterval(timer))
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load">重试</button>
  </div>

  <div class="grid">
    <PanelBox
      title="系统信息"
      :span="12"
      :loading="loading && !summary"
      :foot="`主机 ${summary?.sys.hostname || '—'} · 系统 ${summary?.sys.osName || '—'} ${summary?.sys.osVersion || ''} · 内核 ${summary?.sys.kernel || '—'} · 自动刷新 30s`"
    >
      <template #actions>
        <button class="btn sm" type="button" :disabled="loading" @click="load">刷新</button>
      </template>
      <div class="kpis">
        <KpiCard
          label="CPU 使用率"
          :value="pct(summary?.cpu.usage)"
          unit="%"
          :hint="`负载 ${summary?.cpu.load1 ?? '—'} / ${summary?.cpu.load5 ?? '—'} / ${summary?.cpu.load15 ?? '—'}`"
        />
        <KpiCard
          label="内存使用率"
          :value="pct(summary?.memory.usage)"
          unit="%"
          :hint="`${summary?.memory.usedGB?.toFixed(1) ?? '—'} / ${summary?.memory.totalGB?.toFixed(1) ?? '—'} GiB`"
        />
        <KpiCard
          label="磁盘使用率"
          :value="pct(summary?.disk.usage)"
          unit="%"
          :hint="`${summary?.disk.usedGB?.toFixed(1) ?? '—'} / ${summary?.disk.totalGB?.toFixed(1) ?? '—'} GiB（根分区）`"
        />
        <KpiCard
          label="本进程内存"
          :value="pct(summary?.memory.procUsage)"
          unit="%"
          :hint="`RSS ${summary?.memory.procRSSMB?.toFixed(1) ?? '—'} MiB`"
        />
        <KpiCard
          label="上传"
          :value="net ? humanRate(net.upBps) : '—'"
          :hint="`累计 ${net ? humanBytes(net.upBytes) : '—'}`"
        />
        <KpiCard
          label="下载"
          :value="net ? humanRate(net.downBps) : '—'"
          :hint="`累计 ${net ? humanBytes(net.downBytes) : '—'}`"
        />
      </div>
    </PanelBox>

    <PanelBox
      title="运行时长与时间"
      :span="6"
      :loading="loading && !summary"
      :foot="`开机 ${uptimeText === '—' ? '—' : uptimeText.boot} · 程序启动 ${uptimeText === '—' ? '—' : uptimeText.procStart}`"
    >
      <div class="rows">
        <div class="row"><span>系统运行</span><b>{{ uptimeText === '—' ? '—' : uptimeText.system }}</b></div>
        <div class="row"><span>EMS 程序运行</span><b>{{ uptimeText === '—' ? '—' : uptimeText.process }}</b></div>
        <div class="row"><span>系统时间</span><b>{{ summary?.time.now || '—' }}</b></div>
        <div class="row"><span>时区</span><b>{{ summary?.time.timezone || '—' }}</b></div>
        <div class="row">
          <span>NTP</span>
          <b>
            <StatusTag :tone="summary?.time.ntpRunning ? 'green' : 'gray'">
              {{ summary?.time.ntpRunning ? '已启用' : '未启用' }}
            </StatusTag>
            <span class="muted">{{ summary?.time.ntpServer || '' }}</span>
          </b>
        </div>
      </div>
    </PanelBox>

    <PanelBox
      title="本进程资源趋势"
      sub="近 1 小时"
      :span="6"
      :loading="loading"
      foot="数据来源：/api/system/resource/chart（category=process）"
    >
      <div v-if="chartLoaded" ref="chartEl" class="chart" />
      <div v-else class="empty">
        该接口没有返回可绘制数据。系统资源曲线依赖存储服务，未启用时此处为空。
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
.kpis { display: grid; grid-template-columns: repeat(6, 1fr); gap: var(--s4); }
.rows { display: flex; flex-direction: column; gap: var(--s3); }
.row { display: flex; align-items: center; gap: var(--s3); font-size: var(--fs-13); }
.row > span { min-width: 120px; color: var(--text-3); font-size: var(--fs-12); }
.row b { display: flex; align-items: center; gap: var(--s2); font-weight: 600; font-variant-numeric: tabular-nums; }
.chart { height: 260px; }
.empty { padding: var(--s4); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
.muted { color: var(--text-3); font-size: var(--fs-12); font-weight: 400; }
.btn {
  padding: 0 var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
.btn:disabled { opacity: 0.5; cursor: default; }
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(3, 1fr); }
}
</style>
