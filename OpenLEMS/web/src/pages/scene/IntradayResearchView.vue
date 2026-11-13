<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import { fetchIntradayResearch, type IntradayResearch, type IntradayResearchDay } from '@/api/research'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 市场数据 · 分时研究（第三方 · 非官方）。
 *
 * 为什么单独一页、为什么不进复盘：
 *   官方披露接口只有日频（`gd_spot_metric`，一天一条），96 点（15 分钟）官方匿名拿不到。
 *   这一页画的是**第三方冻结快照**（`gd_metric.csv`，实际值止于 2026-10-02），
 *   所以它必须常驻「第三方 · 非官方」角标与「截至 X」，并且**不参与复盘 MAE** ——
 *   复盘页的 MAE 口径是官方日频，两套口径同屏出现就是第二个 MAE，用户没法对账。
 *
 * 只读：没有刷新按钮、没有定时器、没有采集入口。区间变了才重新取数（一次 GET，无副作用）。
 *
 * 三态（硬要求）：
 *   - 截至日之外 / 未入库的日期 → 空态文字，**不画 0 线、不把缺口连成直线**；
 *   - 滞后 >7 天 → 服务端给 stale=true，页面按「已停更」着色，但仍可看历史；
 *   - 本地完全没有数据（CI / 新机器）→ 整页空态说明，不是报错。
 */
const range = reactive({ from: '', to: '' })
const data = ref<IntradayResearch | null>(null)
const loading = ref(false)
const error = ref('')
let controller: AbortController | null = null

async function load() {
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  error.value = ''
  try {
    data.value = await fetchIntradayResearch(
      { from: range.from || undefined, to: range.to || undefined },
      controller.signal,
    )
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') return
    error.value = err instanceof Error ? err.message : '加载失败'
    data.value = null
  } finally {
    loading.value = false
  }
}

onMounted(load)

function onDateChange() {
  void load()
}

/* ---------- 口径文案 ---------- */

const asOf = computed(() => data.value?.asOf ?? '')
const lagDays = computed(() => data.value?.lagDays ?? -1)
const stale = computed(() => data.value?.stale === true)
const days = computed<IntradayResearchDay[]>(() => data.value?.days ?? [])
const missing = computed<string[]>(() => data.value?.missing ?? [])

/** 截至日与滞后：数据日缺失时不给「今天」，直接说未入库 */
const asOfText = computed(() => {
  if (!asOf.value) return '未入库'
  if (lagDays.value < 0) return `截至 ${asOf.value}`
  return `截至 ${asOf.value} · 滞后 ${lagDays.value} 天`
})

/** 一个点都不出的时候整页走空态（不画空图） */
const empty = computed(() => !loading.value && days.value.length === 0)

/* ---------- 图：按日一条 96 点曲线 ---------- */

const chartEl = ref<HTMLElement>()

/** x 轴用「时间点」而不是索引：不同日子的 96 点必须落在同一列上才能比 */
const timeAxis = computed(() => days.value[0]?.points.map((p) => p.time) ?? [])

const PALETTE = [CHART_COLORS.blue, CHART_COLORS.brand, CHART_COLORS.cyan, CHART_COLORS.purple, CHART_COLORS.green]

useECharts(
  chartEl,
  (): EChartsOption => ({
    grid: { left: 60, right: 24, top: 32, bottom: 44 },
    tooltip: { trigger: 'axis' },
    legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
    xAxis: {
      type: 'category',
      data: timeAxis.value,
      axisLabel: {
        fontSize: 12,
        color: CHART_COLORS.gray,
        // 96 个刻度全画会糊成一片：每 12 个点（3 小时）留一个
        interval: Math.max(0, Math.floor(timeAxis.value.length / 8) - 1),
      },
    },
    yAxis: {
      type: 'value',
      name: '元/兆瓦时',
      nameTextStyle: { fontSize: 12 },
      axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
      splitLine: { lineStyle: { color: 'var(--divider)' } },
    },
    series: days.value.map((d, i) => {
      const byTime = new Map(d.points.map((p) => [p.time, p.value]))
      return {
        name: d.date,
        type: 'line' as const,
        showSymbol: false,
        // 缺口不连：某天少几个点时不能拉一条直线糊过去（那会凭空造出数据）
        connectNulls: false,
        lineStyle: { width: 1.6 },
        itemStyle: { color: PALETTE[i % PALETTE.length] },
        data: timeAxis.value.map((t) => byTime.get(t) ?? null),
      }
    }),
  }),
  [days],
)

/* ---------- 每日统计：比曲线更实用的一眼读数 ---------- */

interface DayStat {
  date: string
  points: number
  avg: number
  max: number
  min: number
}

const stats = computed<DayStat[]>(() =>
  days.value.map((d) => {
    const vs = d.points.map((p) => p.value)
    const sum = vs.reduce((a, b) => a + b, 0)
    return {
      date: d.date,
      points: d.points.length,
      avg: vs.length ? sum / vs.length : NaN,
      max: vs.length ? Math.max(...vs) : NaN,
      min: vs.length ? Math.min(...vs) : NaN,
    }
  }),
)

function num(v: number, digits = 2): string {
  return Number.isFinite(v) ? v.toFixed(digits) : '—'
}
</script>

<template>
  <div class="page">
    <!-- 页头角标：来源与截至日必须常驻，不能只在 hover 里出现 -->
    <div class="banner" :class="{ stale }">
      <StatusTag :tone="stale ? 'red' : 'gray'">
        <span data-thirdparty>第三方 · 非官方</span>
      </StatusTag>
      <span class="asof" data-asof>{{ asOfText }}</span>
      <span class="spacer" />
      <span class="note" data-no-mae>本页不参与复盘 MAE</span>
    </div>

    <p class="explain">
      96 点（15 分钟）日前电价。官方披露接口只有日频，这一页读的是**第三方冻结快照**
      （<code>gd_metric.csv</code>，已停更），因此只读、不采集、不刷新，也不进复盘口径 ——
      复盘页的 MAE 只算官方日频。{{ data?.source?.name || '' }}
    </p>

    <div class="controls">
      <label class="field">
        起
        <input v-model="range.from" type="date" @change="onDateChange" />
      </label>
      <label class="field">
        止
        <input v-model="range.to" type="date" @change="onDateChange" />
      </label>
      <span class="hint">留空 = 截至日往前 7 天</span>
    </div>

    <div v-if="error" class="blocked">
      <p class="blocked-title">读取失败</p>
      <p>{{ error }}</p>
    </div>

    <template v-else-if="loading">
      <PanelBox title="96 点分时" :foot="false" loading />
    </template>

    <template v-else-if="empty">
      <div class="blocked" data-empty>
        <p class="blocked-title">未入库（空态）</p>
        <p>
          这个区间没有第三方分时数据。本页**只读**已有快照，不做采集，也不补数 ——
          空态就是空态，不画 0 线。
        </p>
        <p v-if="missing.length" class="muted" data-missing>区间内未入库：{{ missing.join('、') }}</p>
        <p v-if="data?.available?.length" class="muted">
          本地快照覆盖：{{ data.available[0] }} ~ {{ data.available[data.available.length - 1] }}
        </p>
      </div>
    </template>

    <template v-else>
      <div class="kpis">
        <KpiCard
          label="入库天数"
          :value="stats.length"
          unit="天"
          :hint="`区间 ${data?.from} ~ ${data?.to}`"
        />
        <KpiCard
          label="未入库"
          :value="missing.length"
          unit="天"
          :hint="missing.length ? '按空态处理，不补数' : '区间内无缺日'"
        />
        <KpiCard
          label="区间均价"
          :value="num(stats.reduce((a, s) => a + (Number.isFinite(s.avg) ? s.avg : 0), 0) / (stats.length || 1))"
          unit="元/兆瓦时"
          hint="各日 96 点均价的算术平均"
        />
        <KpiCard
          label="数据截至"
          :value="asOf || '—'"
          :hint="lagDays >= 0 ? `滞后 ${lagDays} 天` : '滞后未知'"
          :badge="stale ? '已停更' : undefined"
        />
      </div>

      <div class="grid">
        <PanelBox
          title="96 点日前电价"
          :sub="`${stats.length} 天 · 元/兆瓦时`"
          foot="来源：第三方分时快照（gd_metric.csv）· 非官方 · 只取实际值，剔除第三方预测占位"
        >
          <div ref="chartEl" class="chart" />
        </PanelBox>

        <PanelBox title="逐日读数" sub="每天 96 点的均价 / 最高 / 最低" foot="同源；未入库的日期不出现在此表">
          <table class="table">
            <thead>
              <tr>
                <th>运行日</th>
                <th class="num">点数</th>
                <th class="num">均价</th>
                <th class="num">最高</th>
                <th class="num">最低</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in stats" :key="s.date">
                <td>{{ s.date }}</td>
                <td class="num">{{ s.points }}</td>
                <td class="num">{{ num(s.avg) }}</td>
                <td class="num">{{ num(s.max) }}</td>
                <td class="num">{{ num(s.min) }}</td>
              </tr>
            </tbody>
          </table>
        </PanelBox>

        <PanelBox
          v-if="missing.length"
          title="未入库日期"
          sub="空态，不补数"
          foot="截至日之外或本地快照没覆盖的日期"
        >
          <div class="missing" data-missing>{{ missing.join('、') }}</div>
          <p class="muted">
            这些日子没有数据：不画 0 线、不把缺口连成直线。要补齐需要显式采集（本页不做）。
          </p>
        </PanelBox>
      </div>
    </template>
  </div>
</template>

<style scoped>
.page { display: flex; flex-direction: column; gap: var(--s4); padding: var(--s4); }
.banner {
  display: flex;
  align-items: center;
  gap: var(--s3);
  padding: var(--s2) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-3);
  font-size: var(--fs-13);
}
.banner.stale { border-color: var(--red); background: var(--red-soft); }
.asof { color: var(--text-2); font-variant-numeric: tabular-nums; }
.spacer { flex: 1; }
.note { color: var(--text-3); font-size: var(--fs-12); }
.explain {
  margin: 0;
  color: var(--text-2);
  font-size: var(--fs-13);
  line-height: 1.8;
}
.explain code {
  padding: 0 var(--s1);
  border-radius: var(--r-xs);
  background: var(--bg-3);
}
.controls { display: flex; align-items: center; gap: var(--s3); }
.field {
  display: inline-flex;
  align-items: center;
  gap: var(--s2);
  font-size: var(--fs-13);
  color: var(--text-3);
}
.field input {
  padding: var(--s1) var(--s3);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.hint { color: var(--text-3); font-size: var(--fs-12); }
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.grid :deep(.panel) { grid-column: span 12; }
.chart { width: 100%; height: 340px; }
.table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
.table th,
.table td {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--divider);
  text-align: left;
}
.table th { color: var(--text-3); font-weight: 500; font-size: var(--fs-12); }
.table .num { text-align: right; font-variant-numeric: tabular-nums; }
.missing { font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
.muted { color: var(--text-3); font-size: var(--fs-12); }
.blocked {
  display: flex;
  flex-direction: column;
  gap: var(--s2);
  align-items: flex-start;
  padding: var(--s5);
  border: 1px dashed var(--border);
  border-radius: var(--r-md);
  background: var(--bg-2);
  color: var(--text-2);
  font-size: var(--fs-13);
}
.blocked p { margin: 0; }
.blocked-title { font-size: var(--fs-16); font-weight: 600; }
@media (max-width: 1280px) {
  .kpis { grid-template-columns: repeat(2, 1fr); }
}
</style>
