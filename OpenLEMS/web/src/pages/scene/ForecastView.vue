<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import { fetchPriceForecast, type PriceForecast } from '@/api/forecast'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 长周期电价预测。
 *
 * 只出**日频**：序列源是官方直采（origin=pm-api）的日均成交价，一天一个点，
 * 出「未来 7 天日均价 + 分位带」。默认 7 天（滚动一周，与信息披露页同一口径）。
 *
 * 15 分钟（96 点）视图已停用（2026-11-13）：它读的是第三方分时通道 gd_metric.csv，
 * 该通道实际值止于 2026-10-02，#139 已把 gdmetric 默认入口改成 fail-closed。
 * 后端 `grain=intraday` 与 `api/forecast.ts` 的调用都保留，拿到合规 96 点再开；
 * 旧书签 `?view=intraday` 过来看到的是停用说明 —— 不 404，也不假装在跑。
 *
 * TimesFM 是 CPU 推理（后端缓存 5 分钟），所以区分「查询」（走缓存）与
 * 「刷新预测」（forceRefresh 穿透），不让人每次切页都在打模型。
 */
const markets = [
  { v: 'ahead', t: '日前' },
  { v: 'real', t: '实时' },
]
/** 日均序列一天一个点，未来 7 / 14 / 21 天三档就够 */
const horizons = [7, 14, 21]

const route = useRoute()
const router = useRouter()
/** 旧书签 `?view=intraday`：96 点视图已停用，给说明而不是 404 */
const intradayPaused = computed(() => route.query.view === 'intraday')

/** 深链过来的人下一步想看的是日均预测，给他一步到位的出口（清掉 query，刷新不会又落回停用页） */
function showDayView() {
  void router.replace({ path: route.path, query: {} }).then(() => load(false))
}

const market = ref('ahead')
const horizonDays = ref(7)
const data = ref<PriceForecast | null>(null)
const loading = ref(false)
const error = ref('')

async function load(force = false) {
  loading.value = true
  error.value = ''
  try {
    data.value = await fetchPriceForecast({
      market: market.value,
      // 日均序列一天一个点，给 60 天历史
      historyDays: 60,
      horizonDays: horizonDays.value,
      forceRefresh: force,
    })
  } catch (err) {
    data.value = null
    error.value = err instanceof Error ? err.message : '加载失败'
  } finally {
    loading.value = false
  }
}

function onFilterChange() {
  void load(false)
}

/* ---------- 平均视图：日均序列 ---------- */

/** 日均明细：把分位也摊开，方便直接抄数 */
const dayRows = computed(() => {
  return (data.value?.forecast ?? []).map((p) => {
    const lo = p.quantiles?.['0.1']
    const hi = p.quantiles?.['0.9']
    return {
      runDate: p.runDate,
      point: p.point,
      lo,
      hi,
      spread: lo != null && hi != null ? hi - lo : null,
    }
  })
})

const firstPoint = computed(() => dayRows.value[0]?.point)
const lastPoint = computed(() => dayRows.value[dayRows.value.length - 1]?.point)
const trend = computed(() => {
  if (firstPoint.value == null || lastPoint.value == null || dayRows.value.length < 2) return ''
  const delta = lastPoint.value - firstPoint.value
  const pctVal = firstPoint.value ? (delta / firstPoint.value) * 100 : 0
  return `${delta >= 0 ? '+' : ''}${delta.toFixed(1)}（${pctVal >= 0 ? '+' : ''}${pctVal.toFixed(1)}%）`
})

const dayChartEl = ref<HTMLElement>()
useECharts(
  dayChartEl,
  (): EChartsOption => {
    const f = data.value
    const dates = [...(f?.historyDates ?? []), ...(f?.forecast ?? []).map((p) => p.runDate)]
    const head = new Array(f?.historyDates.length ?? 0).fill(null)
    const tail = new Array(f?.forecast.length ?? 0).fill(null)
    const q = (key: string) => [...head, ...(f?.forecast ?? []).map((p) => p.quantiles?.[key] ?? null)]
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 44 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v}`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: dates,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: dates.length > 20 ? 40 : 0 },
      },
      yAxis: {
        type: 'value',
        name: f?.unit ?? '',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '历史（官方直采日均）',
          type: 'line',
          showSymbol: false,
          lineStyle: { width: 2 },
          itemStyle: { color: CHART_COLORS.blue },
          data: [...(f?.historyValues ?? []), ...tail],
        },
        {
          name: '预测 10% 分位',
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dotted', width: 1 },
          itemStyle: { color: CHART_COLORS.gray },
          data: q('0.1'),
        },
        {
          name: '预测 90% 分位',
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dotted', width: 1 },
          itemStyle: { color: CHART_COLORS.gray },
          data: q('0.9'),
        },
        {
          name: '预测中位',
          type: 'line',
          showSymbol: true,
          symbolSize: 6,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.brand },
          data: [...head, ...(f?.forecast ?? []).map((p) => p.point)],
        },
      ],
    }
  },
  [data],
)

onMounted(() => {
  // 停用的深链不发请求：省一次必然失败的往返
  if (!intradayPaused.value) load(false)
})
</script>

<template>
  <div v-if="intradayPaused" class="notice paused">
    <strong>96 点分时视图已停用</strong>
    <span>
      　本页只出日均价预测。15 分钟（96 点）视图读的是第三方分时通道 gd_metric.csv，
      该通道实际值止于 2026-10-02；官方匿名接口没有 96 点出清价。
      恢复条件：拿到广东电力交易中心市场成员账号的合规 96 点（T+1）。代码与接口都保留，不在停用期删。
    </span>
    <button type="button" @click="showDayView">看日均价预测</button>
  </div>

  <div v-else-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load(true)">重试</button>
  </div>

  <div v-else class="grid">
    <PanelBox title="查询条件" :span="12" :foot="false">
      <div class="toolbar">
        <label class="field">
          市场
          <select v-model="market" @change="onFilterChange">
            <option v-for="m in markets" :key="m.v" :value="m.v">{{ m.t }}</option>
          </select>
        </label>
        <label class="field">
          未来
          <select v-model.number="horizonDays" @change="onFilterChange">
            <option v-for="h in horizons" :key="h" :value="h">{{ h }} 日</option>
          </select>
        </label>
        <span class="spacer" />
        <button class="btn" type="button" :disabled="loading" @click="load(false)">查询（走缓存）</button>
        <button class="btn primary" type="button" :disabled="loading" @click="load(true)">
          {{ loading ? '预测中…' : '刷新预测（跳过缓存）' }}
        </button>
      </div>
      <div class="warn">
        <strong>日均视图</strong>
        <span>
          　序列源：广东电力交易中心信息披露接口<strong>官方直采</strong>（origin=pm-api）的日均成交价，一天一个点。
          15 分钟（96 点）视图已停用：依赖的第三方分时通道止于 2026-10-02，等合规 96 点。
        </span>
      </div>
    </PanelBox>

    <PanelBox
      title="预测概览"
      :sub="data ? `${data.scope} · ${data.metric}` : ''"
      :span="12"
      :loading="loading"
      :foot="data ? `模型 ${data.model} · 历史 ${data.contextPoints} 点 · 区间 ${data.from} ~ ${data.to} · 缺数据 ${data.missingDays} 天 · 耗时 ${data.timeMs.toFixed(0)}ms` : '数据来源：/api/forecast/price?grain=day'"
    >
      <div class="kpis">
        <KpiCard label="预测天数" :value="dayRows.length || '—'" unit="日" :hint="`D+1 ~ D+${dayRows.length}`" />
        <KpiCard
          label="首日预测"
          :value="firstPoint != null ? firstPoint.toFixed(1) : '—'"
          :unit="data?.unit || ''"
          :hint="dayRows[0]?.runDate || ''"
        />
        <KpiCard
          label="末日预测"
          :value="lastPoint != null ? lastPoint.toFixed(1) : '—'"
          :unit="data?.unit || ''"
          :hint="dayRows[dayRows.length - 1]?.runDate || ''"
        />
        <KpiCard label="区间变化" :value="trend || '—'" hint="首日 → 末日（中位）" />
        <KpiCard
          label="平均不确定性"
          :value="dayRows.length && dayRows[0].spread != null ? (dayRows.reduce((a, r) => a + (r.spread ?? 0), 0) / dayRows.length).toFixed(1) : '—'"
          :unit="data?.unit || ''"
          hint="10%~90% 分位带平均宽度"
        />
        <KpiCard
          label="数据缺口"
          :value="data?.missingDays ?? '—'"
          unit="天"
          :dir="data?.missingDays ? 'up' : undefined"
          hint="历史区间内无已确认数据的天数"
        />
      </div>
    </PanelBox>

    <PanelBox
      title="未来 7 天日均价预测（滚动）"
      sub="历史（官方直采 pm-api）+ 预测中位 + 10%/90% 分位"
      :span="12"
      :loading="loading"
      foot="虚线为预测；分位带越宽说明模型越不确定"
    >
      <div v-if="dayRows.length" ref="dayChartEl" class="chart" />
      <div v-else-if="!loading" class="empty">
        没有预测结果。需要库里有足够的历史日均成交价（官方直采，至少 7 天）。
      </div>
    </PanelBox>

    <PanelBox v-if="dayRows.length" title="预测明细" :sub="`${dayRows.length} 天`" :span="12" :foot="false">
      <table>
        <thead>
          <tr>
            <th>运行日</th>
            <th class="num">中位</th>
            <th class="num">10% 分位</th>
            <th class="num">90% 分位</th>
            <th class="num">区间宽度</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in dayRows" :key="r.runDate">
            <td>{{ r.runDate }}</td>
            <td class="num"><strong>{{ r.point.toFixed(2) }}</strong></td>
            <td class="num">{{ r.lo != null ? r.lo.toFixed(2) : '—' }}</td>
            <td class="num">{{ r.hi != null ? r.hi.toFixed(2) : '—' }}</td>
            <td class="num">{{ r.spread != null ? r.spread.toFixed(2) : '—' }}</td>
          </tr>
        </tbody>
      </table>
      <div class="hint">
        单位：{{ data?.unit || '—' }}。预测为估计值，滚动更新；下游策略是否采纳由「策略管理」决定。
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
.notice.paused {
  align-items: flex-start;
  border-left-color: var(--brand);
  background: var(--brand-soft);
  color: var(--brand-ink);
}
.notice.paused strong { flex: 0 0 auto; }
/* 停用说明里的出口按钮跟错误条的红底按钮不是一回事，单独给它主色 */
.notice.paused button { background: var(--brand-btn); }
.notice.paused button:hover { background: var(--brand-btn-h); }
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
.spacer { flex: 1; }
.field { display: inline-flex; align-items: center; gap: var(--s2); font-size: var(--fs-13); color: var(--text-3); }
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
  margin-top: var(--s3);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-12);
}
.kpis { display: grid; grid-template-columns: repeat(6, 1fr); gap: var(--s4); }
.chart { height: 360px; }
table { width: 100%; border-collapse: collapse; font-size: var(--fs-13); }
th {
  padding: var(--s2) var(--s3);
  border-bottom: 1px solid var(--border);
  color: var(--text-3);
  font-size: var(--fs-12);
  font-weight: 500;
  text-align: left;
}
td { padding: var(--s2) var(--s3); border-bottom: 1px solid var(--divider); font-variant-numeric: tabular-nums; }
th.num, td.num { text-align: right; }
.hint { margin-top: var(--s3); font-size: var(--fs-12); color: var(--text-3); }
.empty { padding: var(--s5); color: var(--text-3); font-size: var(--fs-13); text-align: center; }
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
