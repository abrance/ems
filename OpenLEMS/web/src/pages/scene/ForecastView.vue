<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import { fetchPriceForecast, type PriceForecast } from '@/api/forecast'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 长周期电价预测。
 *
 * 两个视图的**序列源不同**，不是同一份数据的两种画法：
 *
 *   平均（grain=day）     —— 官方直采（origin=pm-api）的日均成交价，一天一个点，
 *                            出「未来 7 天日均价 + 分位带」。
 *   分时（grain=intraday）—— 电网日指标 CSV（gd_metric.csv）的 15 分钟电价，
 *                            送 30 天 × 96 点历史，出未来 5 日 × 96 点。
 *
 * 分时**不需要**日报图4 OCR：CSV 里本来就有 96 点日前/实时电价（2021-05 起，5 年+）。
 * 这一页原先写「分时待数据源、要等图4 纳入提取」是判断错了 —— 后端 `grain=intraday`
 * 早就实现了，缺的只是这个视图。
 *
 * 日均视图默认出**未来 7 天**（滚动一周，信息披露页与预测页对同一口径）。
 * 分时上限 5 日：96 × 5 = 480 ≤ TimesFM max_horizon 512。
 *
 * TimesFM 是 CPU 推理（后端缓存 5 分钟），所以区分「查询」（走缓存）与
 * 「刷新预测」（forceRefresh 穿透），不让人每次切页都在打模型。
 */
const markets = [
  { v: 'ahead', t: '日前' },
  { v: 'real', t: '实时' },
]
const views = [
  { v: 'day', t: '平均' },
  { v: 'intraday', t: '分时' },
]
/** 96 × 5 = 480 ≤ max_horizon 512；再往上要改模型侧限额，不是本页能决定的 */
const INTRA_MAX_DAYS = 5

const market = ref('ahead')
const view = ref<'day' | 'intraday'>('day')
const horizonDays = ref(7)
const data = ref<PriceForecast | null>(null)
const loading = ref(false)
const error = ref('')

const isIntraday = computed(() => view.value === 'intraday')
const horizons = computed(() => (isIntraday.value ? [1, 2, 3, 4, 5] : [7, 14, 21]))

async function load(force = false) {
  loading.value = true
  error.value = ''
  try {
    data.value = await fetchPriceForecast({
      market: market.value,
      grain: isIntraday.value ? 'intraday' : undefined,
      // 分时点密度高，30 天 × 96 = 2880 点已经够 TimesFM；日均序列一天一个点，给 60 天
      historyDays: isIntraday.value ? 30 : 60,
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

function onViewChange() {
  // 切到分时先把天数收进 5 天以内，否则后端直接报「horizon 超上限」
  if (isIntraday.value && horizonDays.value > INTRA_MAX_DAYS) horizonDays.value = INTRA_MAX_DAYS
  void load(false)
}

/* ---------- 平均视图：日均序列 ---------- */

/** 日均明细：把分位也摊开，方便直接抄数 */
const dayRows = computed(() => {
  if (data.value?.grain === 'intraday') return []
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

/* ---------- 分时视图：96 点序列 ---------- */

interface IntraRow {
  runDate: string
  atTime: string
  label: string
  point: number
  lo: number | null
  hi: number | null
}

const intraRows = computed<IntraRow[]>(() => {
  if (data.value?.grain !== 'intraday') return []
  return (data.value?.forecast ?? []).map((p) => ({
    runDate: p.runDate,
    atTime: p.atTime ?? '',
    label: p.atTime ? `${p.runDate.slice(5)} ${p.atTime}` : p.runDate,
    point: p.point,
    lo: p.quantiles?.['0.1'] ?? null,
    hi: p.quantiles?.['0.9'] ?? null,
  }))
})

/**
 * 按 96 点一块切分。
 *
 * 刻意**不按日历日分组**：这条序列是从「历史末点 + 15 分钟」开始的连续曲线
 * （第一天 00:15 起，最后一天 00:00 止），按 runDate 分组会得到 95/96/96/96/96/1
 * 六个组 —— 看着像少了数据，其实一个点都不少。按 96 点切才是模型真正的预测日，
 * 所以窗口原样写出来给人看。
 */
const intraChunks = computed(() => {
  const list = intraRows.value
  const out: {
    label: string
    date: string
    window: string
    points: IntraRow[]
    n: number
    avg: number
    max: number
    maxAt: string
    min: number
    minAt: string
    band: number
  }[] = []
  for (let i = 0; i < list.length; i += 96) {
    const chunk = list.slice(i, i + 96)
    if (!chunk.length) continue
    const pts = chunk.map((c) => c.point)
    const iMax = pts.indexOf(Math.max(...pts))
    const iMin = pts.indexOf(Math.min(...pts))
    const widths = chunk.map((c) => (c.lo != null && c.hi != null ? c.hi - c.lo : 0))
    const head = chunk[0]
    const tail = chunk[chunk.length - 1]
    const date = head.runDate.slice(5)
    out.push({
      label: `D+${out.length + 1}`,
      date,
      window: `${date} ${head.atTime} ~ ${tail.runDate.slice(5)} ${tail.atTime}`,
      points: chunk,
      n: chunk.length,
      avg: pts.reduce((a, b) => a + b, 0) / pts.length,
      max: pts[iMax],
      maxAt: chunk[iMax].runDate.slice(5) === date ? chunk[iMax].atTime : `${chunk[iMax].runDate.slice(5)} ${chunk[iMax].atTime}`,
      min: pts[iMin],
      minAt: chunk[iMin].runDate.slice(5) === date ? chunk[iMin].atTime : `${chunk[iMin].runDate.slice(5)} ${chunk[iMin].atTime}`,
      band: widths.reduce((a, b) => a + b, 0) / widths.length,
    })
  }
  return out
})

/** 一天一条曲线的共同横轴：取第一个窗口的 96 个时刻；末点跨到次日 00:00，写 24:00 才不会让人以为回到了当天开头 */
const intraAxis = computed(() => {
  const first = intraChunks.value[0]?.points ?? []
  return first.map((p, i) => (i === first.length - 1 && p.atTime === '00:00' ? '24:00' : p.atTime))
})

const intraStats = computed(() => {
  const list = intraRows.value
  if (!list.length) return null
  const pts = list.map((r) => r.point)
  const iMax = pts.indexOf(Math.max(...pts))
  const iMin = pts.indexOf(Math.min(...pts))
  const widths = list.map((r) => (r.lo != null && r.hi != null ? r.hi - r.lo : 0))
  return {
    n: list.length,
    days: intraChunks.value.length,
    avg: pts.reduce((a, b) => a + b, 0) / pts.length,
    max: pts[iMax],
    maxAt: list[iMax].label,
    min: pts[iMin],
    minAt: list[iMin].label,
    band: widths.reduce((a, b) => a + b, 0) / widths.length,
  }
})

const intraChartEl = ref<HTMLElement>()

/** 逐日配色：D+1 用主色加粗（当天的决策依据），其余按顺序淡化 */
const INTRA_COLORS = [
  CHART_COLORS.brand,
  CHART_COLORS.blue,
  CHART_COLORS.cyan,
  CHART_COLORS.purple,
  CHART_COLORS.green,
]

useECharts(
  intraChartEl,
  (): EChartsOption => {
    const chunks = intraChunks.value
    const first = chunks[0]
    const series: EChartsOption['series'] = [
      // 分位带只叠 D+1（5 条线再叠 5 个带就没法看了）；其余日的区间宽度在「分日汇总」里给
      {
        name: 'D+1 10% 分位',
        type: 'line',
        stack: 'band',
        showSymbol: false,
        silent: true,
        lineStyle: { opacity: 0 },
        areaStyle: { opacity: 0 },
        data: first?.points.map((p) => p.lo) ?? [],
      },
      {
        name: 'D+1 10~90% 分位带',
        type: 'line',
        stack: 'band',
        showSymbol: false,
        silent: true,
        lineStyle: { opacity: 0 },
        areaStyle: { color: CHART_COLORS.brand, opacity: 0.14 },
        data: first?.points.map((p) => (p.lo != null && p.hi != null ? p.hi - p.lo : null)) ?? [],
      },
      ...chunks.map((c, i) => ({
        name: `${c.label} ${c.date}`,
        type: 'line' as const,
        showSymbol: false,
        // 预测曲线一律虚线（与历史实际区分）；D+1 加粗
        lineStyle: { type: 'dashed' as const, width: i === 0 ? 2 : 1.5, opacity: i === 0 ? 1 : 0.55 },
        itemStyle: { color: INTRA_COLORS[i % INTRA_COLORS.length] },
        data: c.points.map((p) => p.point),
      })),
    ]
    return {
      grid: { left: 60, right: 20, top: 40, bottom: 44 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v}`) },
      legend: {
        top: 0,
        icon: 'rect',
        itemWidth: 12,
        itemHeight: 3,
        textStyle: { fontSize: 12 },
        data: series.filter((s) => !(s as { silent?: boolean }).silent).map((s) => (s as { name: string }).name),
      },
      xAxis: {
        type: 'category',
        data: intraAxis.value,
        // 96 个时刻全标会糊成一片：每 4 小时一根骨架
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          interval: (_i: number, value: string) => /^(00:00|04:00|08:00|12:00|16:00|20:00|24:00)$/.test(value),
        },
      },
      yAxis: {
        type: 'value',
        name: data.value?.unit ?? '',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series,
    }
  },
  [data],
)

onMounted(() => load(false))
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>加载失败</strong><span>　{{ error }}</span>
    <button type="button" @click="load(true)">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="查询条件" :span="12" :foot="false">
      <div class="toolbar">
        <label class="field">
          市场
          <select v-model="market" @change="onFilterChange">
            <option v-for="m in markets" :key="m.v" :value="m.v">{{ m.t }}</option>
          </select>
        </label>
        <label class="field">
          视图
          <select v-model="view" @change="onViewChange">
            <option v-for="v in views" :key="v.v" :value="v.v">{{ v.t }}</option>
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
      <div v-if="!isIntraday" class="warn">
        <strong>平均视图</strong>
        <span>
          　序列源：广东电力交易中心信息披露接口<strong>官方直采</strong>（origin=pm-api）的日均成交价，一天一个点。
          要看 15 分钟曲线请切「分时」。
        </span>
      </div>
      <div v-else class="warn">
        <strong>分时视图</strong>
        <span>
          　序列源：电网日指标 CSV（<code>gd_metric.csv</code>）的 15 分钟电价，历史 30 天 × 96 点送入模型，
          出未来 {{ horizonDays }} 日 × 96 点。上限 {{ INTRA_MAX_DAYS }} 日（96 × 5 = 480 ≤ TimesFM 512）。
          与「平均」是**两条不同的序列**，数字对不上是正常的。
        </span>
      </div>
    </PanelBox>

    <!-- 平均视图 -->
    <template v-if="!isIntraday">
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
    </template>

    <!-- 分时视图 -->
    <template v-else>
      <PanelBox
        title="预测概览"
        sub="15 分钟粒度"
        :span="12"
        :loading="loading"
        :foot="data ? `模型 ${data.model} · 步长 ${data.stepMinutes} 分钟 · 历史送入 ${data.contextPoints} 点${data.inputTruncated ? '（已截断至模型上限）' : ''} · 耗时 ${data.timeMs.toFixed(0)}ms` : '数据来源：/api/forecast/price?grain=intraday'"
      >
        <div class="kpis">
          <KpiCard label="预测天数" :value="intraStats?.days || '—'" unit="日" hint="每 96 点为一个预测日" />
          <KpiCard label="预测点数" :value="intraStats?.n || '—'" unit="点" hint="15 分钟一个点" />
          <KpiCard
            label="区间均价"
            :value="intraStats ? intraStats.avg.toFixed(1) : '—'"
            :unit="data?.unit || ''"
            hint="全部点位的算术平均"
          />
          <KpiCard
            label="最高价"
            :value="intraStats ? intraStats.max.toFixed(1) : '—'"
            :unit="data?.unit || ''"
            :hint="intraStats?.maxAt || ''"
          />
          <KpiCard
            label="最低价"
            :value="intraStats ? intraStats.min.toFixed(1) : '—'"
            :unit="data?.unit || ''"
            :hint="intraStats?.minAt || ''"
          />
          <KpiCard
            label="平均不确定性"
            :value="intraStats ? intraStats.band.toFixed(1) : '—'"
            :unit="data?.unit || ''"
            hint="10%~90% 分位带平均宽度"
          />
        </div>
      </PanelBox>

      <PanelBox
        title="未来多日分时电价预测"
        sub="每日一条 96 点曲线（虚线=预测）；D+1 加粗并叠 10%~90% 分位带"
        :span="12"
        :loading="loading"
        foot="横轴每 4 小时一根刻度；悬停显示该时刻全部日期；其余日期的分位带宽度见「分日汇总」"
      >
        <div v-if="intraChunks.length" ref="intraChartEl" class="chart" />
        <div v-else-if="!loading" class="empty">
          没有预测结果。分时序列来自电网日指标 CSV，检查采集是否正常。
        </div>
      </PanelBox>

      <PanelBox v-if="intraChunks.length" title="分日汇总" :sub="`${intraChunks.length} 个预测日`" :span="12" :foot="false">
        <table>
          <thead>
            <tr>
              <th>预测日</th>
              <th>窗口（起 ~ 止）</th>
              <th class="num">点数</th>
              <th class="num">均价</th>
              <th class="num">最高</th>
              <th class="num">最低</th>
              <th class="num">区间宽度</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in intraChunks" :key="d.label">
              <td>{{ d.label }}</td>
              <td>{{ d.window }}</td>
              <td class="num">{{ d.n }}</td>
              <td class="num"><strong>{{ d.avg.toFixed(1) }}</strong></td>
              <td class="num">{{ d.max.toFixed(1) }}<span class="at"> @{{ d.maxAt }}</span></td>
              <td class="num">{{ d.min.toFixed(1) }}<span class="at"> @{{ d.minAt }}</span></td>
              <td class="num">{{ d.band.toFixed(1) }}</td>
            </tr>
          </tbody>
        </table>
        <div class="hint">
          单位：{{ data?.unit || '—' }}。窗口按<strong>每 96 点</strong>切分（模型的一个预测日），
          不是日历日 —— 这条序列从历史末点往后连续生成，按日历切会出现 95/96/1 点的碎块，看着像缺数据。
        </div>
      </PanelBox>
    </template>
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
.at { color: var(--text-3); font-size: var(--fs-12); }
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
