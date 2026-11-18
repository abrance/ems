<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  DATASET,
  fetchSpotMarketBoard,
  fmtNum,
  fmtPct,
  type MarketBoard,
  type MarketSeries,
} from '@/api/spotmarket'
import {
  fetchHome24h,
  fetchMarketStatus,
  fetchNodePrice,
  periodLabel,
  statusNote,
  type Home24hDay,
  type MarketStatus,
  type NodePriceDay,
} from '@/api/homemarket'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'
import { dailyTier } from '@/utils/dailyLag'

/**
 * 场景 · 市场总览 —— 一屏看三个不同滞后的批次。
 *
 * **为什么单独一页，不并进「能源总览」**：能源总览是设备侧口径（SOC/功率/告警），
 * 数据来自现场采集、截止日就是当下；市场侧是官方披露，三个批次各有各的截止日。
 * 混在一页会出现「两套截止日」且看不出来哪个数属于哪一批 —— 那是这页要解决的问题，
 * 不是要复制的问题。
 *
 * 三批数据，粒度与滞后都不同，所以是三张表、三个接口，不混表：
 *
 *   地市节点价   gd_node_price   /api/gd-metric/node-price     T+0   42 条（21 市 × 高峰/低谷）
 *   日频指标     gd_spot_metric  /api/spot-market/board         T+0（实时腿 T+2）  日均价/成交量/申报均价…
 *   24 点分时    gd_home_24h     /api/gd-metric/home-24h        T-6   24 个整点
 *
 * 页头批次条标两批固定日期，24 点日期与「查看日期」框放在自己的区块标题，
 * 就是为了让人一眼看出「这三个数不是同一天的东西」，而不是拿一个当「最新数据」。
 *
 * 页脚另有一句口径警告：24 点日均（463.27）与官方日频日均（456.55）不可互校，
 * 差约 1.5% 且无换算关系 —— 实测值写死在页面里，不从任一侧推导。
 *
 * 每个面板右上角有源角标；空日期给明确的「该日无数据」而不是空图。
 */
const HOME24H_SOURCE = '官网·24 点 T-6'
const DAILY_SOURCE = '官网·披露接口'
const NODE_SOURCE = '官网·节点价 T+0'

const daily = ref<MarketBoard | null>(null)
const home24h = ref<Home24hDay | null>(null)
const nodePrice = ref<NodePriceDay | null>(null)
/**
 * 两路采集状态。**只用来决定空态怎么说**，不参与任何数值（数值只来自
 * home-24h / node-price）。请求失败时保持 null，页面退回老文案「该日无数据」。
 */
const marketStatus = ref<MarketStatus | null>(null)
/** 24 点区块自己的查看日。空 = 后端最新日；组件卸载即失效（切页回默认）。 */
const selected24hDate = ref('')
const loadingDaily = ref(false)
const loading24h = ref(false)
const loadingNode = ref(false)
const errors = ref<string[]>([])

async function loadDaily() {
  loadingDaily.value = true
  try {
    daily.value = await fetchSpotMarketBoard({ days: 90 })
  } catch (err) {
    errors.value.push(`日频指标：${err instanceof Error ? err.message : '加载失败'}`)
    daily.value = null
  } finally {
    loadingDaily.value = false
  }
}

async function load24h() {
  loading24h.value = true
  try {
    // 不选日期才让后端给库里最新一天（源站 T-6，硬取今天永远是空）
    home24h.value = await fetchHome24h({ date: selected24hDate.value || undefined })
  } catch (err) {
    errors.value.push(`24 点分时：${err instanceof Error ? err.message : '加载失败'}`)
    home24h.value = null
  } finally {
    loading24h.value = false
  }
}

async function loadNode() {
  loadingNode.value = true
  try {
    nodePrice.value = await fetchNodePrice({})
  } catch (err) {
    errors.value.push(`地市节点价：${err instanceof Error ? err.message : '加载失败'}`)
    nodePrice.value = null
  } finally {
    loadingNode.value = false
  }
}

async function loadStatus() {
  try {
    marketStatus.value = await fetchMarketStatus()
  } catch {
    // 状态端点自己挂了不许把整页带到白屏：退回「该日无数据」比什么都不说清楚
    marketStatus.value = null
  }
}

/** 三路并行：任何一路慢/失败都不拖累另外两路（它们的源站本来就互不相关） */
function reloadAll() {
  errors.value = []
  void Promise.all([loadDaily(), load24h(), loadNode(), loadStatus()])
}

onMounted(reloadAll)

/* ---------- 批次条：两批固定日期 + 页脚三批汇总；24 点日期在区块标题 ---------- */

// 页脚要三批都点到；页头批次条只留两批固定日期，24 点的日期移到自己的区块标题。
const batches = computed(() => [
  {
    key: 'node',
    label: '地市节点价',
    date: nodePrice.value?.latestDate || '—',
    lag: nodePrice.value?.lagDays ?? -1,
    source: NODE_SOURCE,
    tone: 'green' as const,
  },
  {
    key: 'daily',
    label: '日频指标',
    date: daily.value?.date || '—',
    lag: lagOf(daily.value?.date),
    source: DAILY_SOURCE,
    tone: 'blue' as const,
  },
  {
    key: 'home24h',
    label: '24 点分时',
    date: home24h.value?.latestDate || '—',
    lag: home24h.value?.lagDays ?? -1,
    source: HOME24H_SOURCE,
    tone: 'brand' as const,
  },
])

const visibleBatches = computed(() => batches.value.filter((b) => b.key !== 'home24h'))

/** 区块标题的滞后：默认日沿用后端口径；回溯日按所选日算，不能拿最新日的滞后冒充。 */
const home24hViewLag = computed(() => {
  const d = home24h.value
  if (!d) return -1
  if (!selected24hDate.value || selected24hDate.value === d.latestDate) return d.lagDays
  return lagOf(d.date)
})

function lagOf(date?: string): number {
  if (!date) return -1
  const d = new Date(`${date}T00:00:00`)
  if (Number.isNaN(d.getTime())) return -1
  const today = new Date()
  const a = Date.UTC(d.getFullYear(), d.getMonth(), d.getDate())
  const b = Date.UTC(today.getFullYear(), today.getMonth(), today.getDate())
  const lag = Math.round((b - a) / 86400_000)
  return lag < 0 ? 0 : lag
}

/** 页脚：数据截至 X —— 三个日期一起写，不挑一个代表全部 */
const dataAsOf = computed(() => {
  const parts = batches.value.filter((b) => b.date !== '—').map((b) => `${b.label} ${b.date}`)
  return parts.length ? parts.join(' / ') : '—'
})

/* ---------- 6 张日频 KPI ---------- */

const dailySeries = computed<MarketSeries[]>(() => daily.value?.daily ?? [])
function pick(dataset: string, metricPart: string): MarketSeries | undefined {
  return dailySeries.value.find((s) => s.dataset === dataset && s.metric.includes(metricPart))
}

const aheadAvg = computed(() => pick(DATASET.ahead, '加权平均电价'))
const realAvg = computed(() => pick(DATASET.realtime, '加权平均电价'))
const declPrice = computed(() => pick(DATASET.ahead, '申报均价'))
const aheadVolGen = computed(() => pick(DATASET.ahead, '发电侧日前总成交电量'))
const aheadVolCon = computed(() => pick(DATASET.ahead, '用电侧日前总成交电量'))
const aheadMax = computed(() => pick(DATASET.ahead, '加权最高电价'))
const aheadMin = computed(() => pick(DATASET.ahead, '加权最低电价'))

const priceUnit = computed(() => aheadAvg.value?.unit || '元/兆瓦时')
const volUnit = computed(() => aheadVolGen.value?.unit || 'MWh')
const dailyEmpty = computed(() => !loadingDaily.value && dailySeries.value.length === 0)
/**
 * 日频空态分级。三档比一句话多的那点代码，换来的是「采集停了」不再被说成「还没发布」。
 * 见 utils/dailyLag.ts 的 tier 语义。board.date 是**日前运行日**，所以传 ahead：
 * 日前当日发布，lag=1 就已经漏了一天，拿实时腿的 T+2 当阈值会说成正常。
 */
const dailyLag = computed(() => lagOf(daily.value?.date))
const dailyTierNow = computed(() => dailyTier(dailyLag.value, dailyEmpty.value, 'ahead'))
/** 库里近一年没有日频指标：空库时 board.date 回落成今天，lag 恒为 0，不能拿去说「还没发布」 */
const dailyNever = computed(() => dailyTierNow.value === 'never')

const dailyEmptyText = computed(() => {
  const latest = daily.value?.date || '—'
  switch (dailyTierNow.value) {
    case 'never':
      // 写「近一年」不写「从未」：latestDate 回看 456 天，只有一年前的老数据也落进这里，
      // 那同样是「采集早停了」，告警一样成立
      return '库里近一年没有日频指标。日频来自披露接口直采，请检查采集是否在跑。'
    case 'lag':
      // 阈值跟着腿走：这里判的是日前运行日，官方当日发布，滞后 1 天以上就算超出节奏。
      // 写「官方正常 T+2」会把日前漏采两天说成正常。
      return `采集滞后：库里最新运行日 ${latest}（${dailyLag.value} 天前），官方日前当日发布，已超出预期`
    case 'stop':
      return `采集已落后：库里最新运行日 ${latest}（${dailyLag.value} 天前），请检查采集是否在跑`
    default:
      // 不写「实时 T+2 才发布」：这里判的是日前运行日，日前当日发布，
      // 把一个实时腿的节奏罩到日前腿上属事实错误（#173 / #178）。
      return `该日无数据：披露接口这一天的指标还没落库。库里最新运行日 ${latest}。`
  }
})
/** 空库与「采集落后」都要告警；3~7 天只是中性偏提醒，红绿不混 */
const dailyEmptyTone = computed(() =>
  dailyTierNow.value === 'never' || dailyTierNow.value === 'stop'
    ? 'is-alert'
    : dailyTierNow.value === 'unknown'
      ? ''
      : 'is-neutral',
)

/* ---------- 图 1：日频趋势 ---------- */

const trendEl = ref<HTMLElement>()
useECharts(
  trendEl,
  (): EChartsOption => {
    const a = aheadAvg.value
    const r = realAvg.value
    const periods = a?.points.map((p) => p.period) ?? []
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: periods,
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray, rotate: periods.length > 45 ? 30 : 0 },
      },
      yAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '日前加权均价',
          type: 'line',
          smooth: true,
          showSymbol: false,
          itemStyle: { color: CHART_COLORS.brand },
          data: a?.points.map((p) => p.value) ?? [],
        },
        {
          name: '实时加权均价',
          type: 'line',
          smooth: true,
          showSymbol: false,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.blue },
          data: r?.points.map((p) => p.value) ?? [],
        },
      ],
    }
  },
  [dailySeries],
)

/* ---------- 图 2：24 点电价曲线 ---------- */

const home24hPoints = computed(() => home24h.value?.points ?? [])
const home24hEmpty = computed(() => !loading24h.value && (home24h.value?.empty ?? true))

/**
 * 24 点负荷面板页脚。
 *
 * 后端只在满 24 点时下发峰谷（缺点日 omitempty），所以缺点日不能只印「峰值 — MW」，
 * 读者会当成系统坏了。用 pointCount 说清「没算」这件事本身。
 */
const HOME24H_POINTS_PER_DAY = 24
const home24hFoot = computed(() => {
  const d = home24h.value
  if (!d || home24hEmpty.value) return '负荷单独成图：与电价量纲差两个数量级，双 y 轴会看错拐点'
  if (d.pointCount !== HOME24H_POINTS_PER_DAY) {
    return `仅 ${d.pointCount}/${HOME24H_POINTS_PER_DAY} 点，均价与峰谷不计算（缺点日不冒充全天）`
  }
  return `峰值 ${fmtNum(d.peakPowerMw, 0)} MW · 谷值 ${fmtNum(d.lowPowerMw, 0)} MW`
})

const price24hEl = ref<HTMLElement>()
useECharts(
  price24hEl,
  (): EChartsOption => {
    const pts = home24hPoints.value
    const hours = pts.map((p) => `${String(p.hour).padStart(2, '0')}:00`)
    return {
      grid: { left: 60, right: 20, top: 32, bottom: 40 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'category',
        data: hours,
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          interval: (_i: number, value: string) => /^(00:00|04:00|08:00|12:00|16:00|20:00)$/.test(value),
        },
      },
      yAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '日前电价',
          type: 'line',
          showSymbol: false,
          itemStyle: { color: CHART_COLORS.brand },
          data: pts.map((p) => p.aheadPrice),
        },
        {
          name: '实时电价',
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dashed' },
          itemStyle: { color: CHART_COLORS.blue },
          data: pts.map((p) => p.realtimePrice),
        },
      ],
    }
  },
  [home24hPoints],
)

/* ---------- 图 3：24 点负荷（单独一张小图，不做双 y 轴） ---------- */

const power24hEl = ref<HTMLElement>()
useECharts(
  power24hEl,
  (): EChartsOption => {
    const pts = home24hPoints.value
    const hours = pts.map((p) => `${String(p.hour).padStart(2, '0')}:00`)
    return {
      grid: { left: 64, right: 20, top: 32, bottom: 40 },
      tooltip: {
        trigger: 'axis',
        valueFormatter: (v) => (v == null ? '—' : `${Number(v).toLocaleString('zh-CN', { maximumFractionDigits: 0 })} MW`),
      },
      xAxis: {
        type: 'category',
        data: hours,
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          interval: (_i: number, value: string) => /^(00:00|04:00|08:00|12:00|16:00|20:00)$/.test(value),
        },
      },
      yAxis: {
        type: 'value',
        name: 'MW',
        nameTextStyle: { fontSize: 12 },
        // 负荷动辄几万 MW：不除以万，y 轴标签会挤成一团
        axisLabel: {
          fontSize: 12,
          color: CHART_COLORS.gray,
          formatter: (v: number) => `${(v / 10000).toFixed(1)}万`,
        },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '系统负荷',
          type: 'line',
          showSymbol: false,
          areaStyle: { opacity: 0.14 },
          itemStyle: { color: CHART_COLORS.cyan },
          data: pts.map((p) => p.powerMw),
        },
      ],
    }
  },
  [home24hPoints],
)

/* ---------- 图 4：地市横向条形（42 条） ---------- */

/* ---------- 采集状态：四态空态 ---------- */

const note24h = computed(() => statusNote(marketStatus.value?.home24h))
const noteNode = computed(() => statusNote(marketStatus.value?.nodePrice))
const state24h = computed(() => marketStatus.value?.home24h.state ?? '')
const stateNode = computed(() => marketStatus.value?.nodePrice.state ?? '')
/**
 * 空态文案：有四态就用状态端点的话，没有（接口自己挂了）就退回原来的
 * 「该日无数据 + 库里最新到哪天」。两种都不会白屏，区别只在说得多具体。
 */
const home24hEmptyText = computed(
  () =>
    note24h.value?.text ??
    `该日无数据：主页 24 点滞后约 6 天才发布，库里最新到 ${home24h.value?.latestDate || '（还没有任何数据）'}。`,
)
const nodeEmptyText = computed(
  () =>
    noteNode.value?.text ??
    `该日无数据：地市节点价 T+0 可得，库里最新到 ${nodePrice.value?.latestDate || '（还没有任何数据）'}。`,
)

const nodePoints = computed(() => nodePrice.value?.points ?? [])
const nodeEmpty = computed(() => !loadingNode.value && (nodePrice.value?.empty ?? true))

/** 横向条形按价格升序：最高的排最上面，读起来像排行榜 */
const nodeBars = computed(() =>
  [...nodePoints.value].sort((a, b) => a.price - b.price),
)

const nodeEl = ref<HTMLElement>()
useECharts(
  nodeEl,
  (): EChartsOption => {
    const rows = nodeBars.value
    const labels = rows.map((r) => `${r.city}·${periodLabel(r.periodType)}`)
    return {
      grid: { left: 110, right: 60, top: 16, bottom: 24 },
      tooltip: { trigger: 'axis', valueFormatter: (v) => (v == null ? '—' : `${v} 元/兆瓦时`) },
      xAxis: {
        type: 'value',
        name: '元/兆瓦时',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      yAxis: {
        type: 'category',
        data: labels,
        axisLabel: { fontSize: 11, color: CHART_COLORS.gray },
      },
      series: [
        {
          type: 'bar',
          // 高峰用主色、低谷用蓝色：同一维度两种时段，不是两个指标
          data: rows.map((r) => ({
            value: r.price,
            itemStyle: { color: r.periodType === 1 ? CHART_COLORS.brand : CHART_COLORS.blue },
          })),
          label: {
            show: true,
            position: 'right',
            fontSize: 11,
            formatter: (p: { value?: unknown }) => Number(p.value ?? 0).toFixed(1),
          },
          barMaxWidth: 10,
        },
      ],
    }
  },
  [nodeBars],
)

/** 地市条形图要 42 行，容器按行数给高度，否则条被压扁 */
const NODE_ROWS_EXPECTED = 42
const nodeChartHeight = computed(() => `${Math.max(320, nodeBars.value.length * 18 + 60)}px`)
/** 空态按「该有的 42 行」占位：塌成一条线会让下方内容上下跳 */
const nodeBoxHeight = computed(() => `${Math.max(320, NODE_ROWS_EXPECTED * 18 + 60)}px`)
</script>

<template>
  <div v-if="errors.length" class="notice error">
    <strong>部分数据加载失败</strong>
    <span>　{{ errors.join('；') }}</span>
    <button type="button" @click="reloadAll">重试</button>
  </div>

  <!-- 批次条：两批固定日期；24 点日期跟着自己的区块走 -->
  <div class="batches">
    <div v-for="b in visibleBatches" :key="b.key" class="batch">
      <span class="batch-label">{{ b.label }}</span>
      <strong class="batch-date">{{ b.date }}</strong>
      <StatusTag :tone="b.tone">{{ b.source }}</StatusTag>
      <span v-if="b.lag >= 0" class="batch-lag">滞后 {{ b.lag }} 天</span>
      <span v-else class="batch-lag">无数据</span>
    </div>
  </div>

  <section class="zone">
    <div class="zone-head">
      <h2>多日指标与趋势</h2>
    </div>
    <div class="grid">
    <!-- 6 张日频 KPI -->
    <PanelBox
      title="日频指标"
      sub="现货市场总体运行情况 · 发电侧加权口径"
      :span="12"
      :loading="loadingDaily"
      :foot="`数据来源：广东电力交易中心披露接口直采 · 最新运行日 ${daily?.date || '—'}`"
    >
      <template #actions>
        <StatusTag tone="blue">{{ DAILY_SOURCE }}</StatusTag>
      </template>
      <div v-if="dailyEmpty" class="empty" :class="dailyEmptyTone" data-daily-state>
        {{ dailyEmptyText }}
      </div>
      <div v-else class="kpis">
        <KpiCard
          label="日前加权均价"
          :value="fmtNum(aheadAvg?.latest.value)"
          :unit="priceUnit"
          :hint="`环比 ${fmtPct(aheadAvg?.momPct)} · 同比 ${fmtPct(aheadAvg?.yoyPct)}`"
        />
        <KpiCard
          label="实时加权均价"
          :value="fmtNum(realAvg?.latest.value)"
          :unit="priceUnit"
          :hint="`运行日 ${realAvg?.latest.period || '—'}（实时 T+2）`"
        />
        <KpiCard
          label="日前峰谷价差"
          :value="fmtNum((aheadMax?.latest.value ?? 0) - (aheadMin?.latest.value ?? 0))"
          :unit="priceUnit"
          :hint="`最高 ${fmtNum(aheadMax?.latest.value)} / 最低 ${fmtNum(aheadMin?.latest.value)}`"
        />
        <KpiCard
          label="日前-实时价差"
          :value="fmtNum(daily?.spreadMean)"
          :unit="priceUnit"
          hint="区间均值，正=日前更高"
        />
        <KpiCard
          label="日前成交电量·发电侧"
          :value="fmtNum(aheadVolGen?.latest.value, 0)"
          :unit="volUnit"
          :hint="`用电侧 ${fmtNum(aheadVolCon?.latest.value, 0)} · 环比 ${fmtPct(aheadVolGen?.momPct)}`"
        />
        <KpiCard
          label="日前申报均价"
          :value="fmtNum(declPrice?.latest.value)"
          :unit="priceUnit"
          :hint="`环比 ${fmtPct(declPrice?.momPct)} · 仅日前披露`"
        />
      </div>
    </PanelBox>

    <!-- 日频趋势 -->
    <PanelBox
      title="日频趋势"
      sub="日前 / 实时加权均价 · 近 90 天"
      :span="12"
      :loading="loadingDaily"
      :foot="`数据来源：${daily?.source || '官方披露接口'} · 运行日 ${daily?.from || '—'} ~ ${daily?.to || '—'}`"
    >
      <template #actions>
        <StatusTag tone="blue">{{ DAILY_SOURCE }}</StatusTag>
      </template>
      <!-- 空库时趋势图同样是空的：不加 !dailyNever，红色告警旁边会再写一句中性的
           「该区间无数据」，两句互相抵消 -->
      <div v-if="dailyEmpty && !dailyNever" class="empty">该区间无数据（披露接口还没有落库）。</div>
      <div v-else-if="dailyNever" class="empty is-alert" data-daily-state>{{ dailyEmptyText }}</div>
      <div v-else ref="trendEl" class="chart" />
    </PanelBox>
    </div>
  </section>

  <section class="zone">
    <div class="zone-head" data-home24h-zone>
      <h2>24 点分时</h2>
      <strong class="batch-date">{{ home24h?.date || '—' }}</strong>
      <span v-if="home24hViewLag >= 0" class="batch-lag">滞后 {{ home24hViewLag }} 天</span>
      <span v-else class="batch-lag">无数据</span>
      <span v-if="home24h?.date && home24h.date !== home24h.latestDate" class="batch-lag">
        库里最新 {{ home24h.latestDate }}
      </span>
      <label class="zone-date">
        查看日期
        <input v-model="selected24hDate" type="date" data-home24h-date @change="load24h()" />
      </label>
    </div>
    <div class="grid">
    <!-- 24 点电价 -->
    <PanelBox
      title="24 点电价曲线"
      sub="日前 / 实时电价 · 整点"
      :span="8"
      :loading="loading24h"
      :foot="`数据来源：官方主页 24 点 · 运行日 ${home24h?.date || '—'}（源站滞后约 6 天）`"
    >
      <template #actions>
        <StatusTag tone="brand">{{ HOME24H_SOURCE }}</StatusTag>
      </template>
      <div
        v-if="home24hEmpty"
        class="empty empty--chart"
        :class="note24h ? (note24h.tone === 'alert' ? 'is-alert' : 'is-neutral') : ''"
        :data-state="state24h"
      >{{ home24hEmptyText }}</div>
      <template v-else>
        <div
          v-if="note24h"
          class="state-note"
          :class="note24h.tone === 'alert' ? 'is-alert' : 'is-neutral'"
          :data-state="state24h"
        >{{ note24h.text }}</div>
        <div ref="price24hEl" class="chart" />
      </template>
    </PanelBox>

    <!-- 24 点负荷：单独一张小图，不做双 y 轴 -->
    <PanelBox
      title="24 点系统负荷"
      sub="MW · 与上图同一运行日"
      :span="4"
      :loading="loading24h"
      :foot="home24hFoot"
    >
      <template #actions>
        <StatusTag tone="brand">{{ HOME24H_SOURCE }}</StatusTag>
      </template>
      <div
        v-if="home24hEmpty"
        class="empty empty--chart"
        :class="note24h ? (note24h.tone === 'alert' ? 'is-alert' : 'is-neutral') : ''"
        :data-state="state24h"
      >{{ home24hEmptyText }}</div>
      <div v-else ref="power24hEl" class="chart" />
    </PanelBox>
    </div>
  </section>

  <section class="zone">
    <div class="zone-head">
      <h2>地市节点价</h2>
    </div>
    <div class="grid">
    <!-- 地市横向条形 -->
    <PanelBox
      title="地市节点电价"
      sub="21 市 × 高峰 / 低谷 · 元/兆瓦时"
      :span="12"
      :loading="loadingNode"
      :foot="`数据来源：官方主页地市节点价（T+0）· 运行日 ${nodePrice?.latestDate || '—'} · 高峰 ${nodePrice?.peakCities ?? 0} 市 / 低谷 ${nodePrice?.valleyCities ?? 0} 市`"
    >
      <template #actions>
        <StatusTag tone="green">{{ NODE_SOURCE }}</StatusTag>
      </template>
      <div
        v-if="nodeEmpty"
        class="empty"
        :class="noteNode ? (noteNode.tone === 'alert' ? 'is-alert' : 'is-neutral') : ''"
        :data-state="stateNode"
        :style="{ height: nodeBoxHeight }"
      >{{ nodeEmptyText }}</div>
      <template v-else>
        <div
          v-if="noteNode"
          class="state-note"
          :class="noteNode.tone === 'alert' ? 'is-alert' : 'is-neutral'"
          :data-state="stateNode"
        >{{ noteNode.text }}</div>
        <div ref="nodeEl" class="chart" :style="{ height: nodeChartHeight }" />
      </template>
    </PanelBox>
    </div>
  </section>

  <div class="footer">
    <div>数据截至 {{ dataAsOf }}。三个批次的滞后不同，不能互相当作「最新数据」。</div>
    <div class="footer-caliber">
      口径差异：24 点日均 463.27 ≠ 官方日频日均 456.55（同为 2026-11-07 实测），不可互校。
      两者一个是主页 24 个整点的算术均值、一个是披露口径日频加权均价，差约 1.5%；
      这是 11-07 的实测对照，不是可以从任一侧算出来的换算关系。
    </div>
  </div>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(12, 1fr); gap: var(--s4); }
.batches {
  display: flex;
  flex-wrap: wrap;
  gap: var(--s3);
  margin-bottom: var(--s4);
}
.batch {
  display: flex;
  align-items: center;
  gap: var(--s2);
  padding: var(--s2) var(--s3);
  background: var(--bg-2);
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  font-size: var(--fs-13);
}
.batch-label { color: var(--text-3); font-size: var(--fs-12); }
.batch-date { font-variant-numeric: tabular-nums; }
.batch-lag { color: var(--text-3); font-size: var(--fs-12); }
.zone + .zone { margin-top: var(--s5); }
.zone-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--s2) var(--s3);
  margin-bottom: var(--s3);
}
.zone-head h2 { margin: 0; font-size: var(--fs-14); font-weight: 600; }
.zone-date {
  display: inline-flex;
  align-items: center;
  gap: var(--s2);
  margin-left: auto;
  color: var(--text-3);
  font-size: var(--fs-12);
}
.zone-date input {
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-12);
}
.kpis { display: grid; grid-template-columns: repeat(6, 1fr); gap: var(--s4); }
.chart { height: 360px; }
.empty {
  display: grid;
  place-items: center;
  min-height: 140px;
  padding: var(--s4);
  color: var(--text-3);
  font-size: var(--fs-13);
  text-align: center;
  line-height: 1.7;
}
/* 空态占住图表原来的高度：塔成一条线会让下方内容往上一跳（切换日期时尤其明显） */
.empty--chart { min-height: 0; height: 360px; }
/*
 * 四态提示。
 * neutral = 滞后（源站还没发），正常口气、次要样式；
 * is-alert = 失败 / 从未成功 / 停更，才用告警色。
 * 这两类红绿一律不混：把「等就行」的东西标红，等于让人去查一个没坏的东西。
 */
.state-note {
  margin-bottom: var(--s3);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-3);
  color: var(--text-2);
  font-size: var(--fs-13);
  line-height: 1.6;
}
.state-note.is-alert,
.empty.is-alert {
  border-left-color: var(--red);
  background: var(--red-soft);
  color: var(--red-ink);
}
.empty.is-alert,
.empty.is-neutral { text-align: left; }
.footer {
  margin-top: var(--s4);
  padding: var(--s2) var(--s1);
  color: var(--text-3);
  font-size: var(--fs-12);
}
.footer-caliber {
  margin-top: var(--s1);
  color: var(--text-3);
  font-size: var(--fs-12);
  line-height: 1.7;
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
  padding: var(--s1) var(--s3);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-13);
  cursor: pointer;
}
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(3, 1fr); }
}
</style>
