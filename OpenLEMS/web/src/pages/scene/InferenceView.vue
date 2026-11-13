<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import type { EChartsOption } from 'echarts'
import PanelBox from '@/components/PanelBox.vue'
import KpiCard from '@/components/KpiCard.vue'
import { fetchSupplyCurve, type SupplyCurve } from '@/api/gdmetric'
import { bidScan, loadPercentile, optimalBid, quantileGridAt, strategyFamily } from '@/metrics/supply'
import { CHART_COLORS, useECharts } from '@/composables/useECharts'

/**
 * 场景 · 策略反演（基础版：统计反演）。
 *
 * 设计稿 §14.1 里列了两种方法：
 *   ① 逆优化 —— 把出清模型当约束反推报价。最可信，但必须有网络模型 + 出清模型 + 机组成本，EMS 都没有。
 *   ② 统计拟合 —— 用观测量反推。不保证与真实出清一致，只能作参考。
 *
 * 本页做的是 ②。**能成立的理由**：负荷基本是刚需（价格弹性极低），出清价是需求与供给栈
 * 的交点，所以历史上每一对 (负荷, 出清价) **本身就是市场聚合供给曲线上的一个点**。
 * 按负荷分箱取价格分位，得到的就是这条曲线。
 *
 * **反演的是市场整体，不是任何一台机组** —— 这一点必须在页面上讲清楚，
 * 否则会被当成某台机组的真实报价用。
 */
const PRESETS = [
  { v: '30', t: '近 30 天', days: 29 },
  { v: '90', t: '近 90 天', days: 89 },
  { v: '180', t: '近 180 天', days: 179 },
]

const market = ref('ahead')
const preset = ref('90')
const from = ref('')
const to = ref('')
const curve = ref<SupplyCurve | null>(null)
const loading = ref(false)
const error = ref('')

/** 当前负荷（MW）。默认取序列最后时刻的值，可手改。 */
const load = ref<number | null>(null)
/** 边际成本（元/MWh）。留空 = 0，此时最优点会落在最低报价，页面会说明。 */
const cost = ref<number | null>(null)

const stats = computed(() => curve.value?.stats ?? null)
const bins = computed(() => curve.value?.allBins ?? [])
const effectiveCost = computed(() => (Number.isFinite(cost.value) ? Number(cost.value) : 0))
const grid = computed(() => (curve.value && load.value != null ? quantileGridAt(bins.value, load.value) : null))
const costMissing = computed(() => !Number.isFinite(cost.value) || cost.value == null)

const scan = computed(() => (grid.value ? bidScan(grid.value, effectiveCost.value) : []))
const best = computed(() => (grid.value ? optimalBid(grid.value, effectiveCost.value) : null))
const family = computed(() => (grid.value ? strategyFamily(grid.value, effectiveCost.value) : []))
const loadPct = computed(() => (bins.value.length && load.value != null ? loadPercentile(bins.value, load.value) : null))

function fmt(d: Date): string {
  const loc = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
  return loc.toISOString().slice(0, 10)
}

function applyPreset() {
  const end = new Date()
  const days = PRESETS.find((p) => p.v === preset.value)?.days ?? 89
  from.value = fmt(new Date(end.getTime() - days * 86400_000))
  to.value = fmt(end)
}

async function loadCurve() {
  loading.value = true
  error.value = ''
  try {
    const res = await fetchSupplyCurve({ from: from.value, to: to.value, market: market.value, bins: 40, steps: 10 })
    curve.value = res
    // 当前负荷默认取序列最后一个时刻；用户改过就不再覆盖
    if (load.value == null || load.value === 0) load.value = res.stats.latestLoad
  } catch (err) {
    error.value = err instanceof Error ? err.message : '反演失败'
    curve.value = null
  } finally {
    loading.value = false
  }
}

function fmtNum(v: number | null | undefined, digits = 1): string {
  return v == null || !Number.isFinite(v) ? '—' : v.toFixed(digits)
}

function pct(v: number | null | undefined, digits = 1): string {
  return v == null || !Number.isFinite(v) ? '—' : `${(v * 100).toFixed(digits)}%`
}

/** 供给曲线置信度：ρ 低或逆序多，说明这一区间不适合反演 */
const quality = computed(() => {
  const s = stats.value
  if (!s) return { level: 'unknown' as const, text: '' }
  if (s.violations > 0 || s.spearman < 0.3) {
    return {
      level: 'bad' as const,
      text: `P50 曲线有 ${s.violations} 处逆序、负荷-价格秩相关仅 ${s.spearman.toFixed(2)}：区间内供给栈整体漂移过（燃料价/检修/装机变化），曲线只能当参考。把区间缩短再试。`,
    }
  }
  if (s.maxPavaMove > (s.priceMax - s.priceMin) * 0.1) {
    return {
      level: 'warn' as const,
      text: `单调化把报价段最多挪动了 ${s.maxPavaMove.toFixed(1)} 元/MWh，原始曲线本身不够单调，报价段读数要打折。`,
    }
  }
  return { level: 'ok' as const, text: '' }
})

/* ---------- 图：反演供给曲线 ---------- */
const curveEl = ref<HTMLElement>()
useECharts(
  curveEl,
  (): EChartsOption => {
    const c = curve.value
    if (!c) return {}
    const p10 = c.allBins.map((b) => [b.loadMid, b.q[1]])
    const p50 = c.allBins.map((b) => [b.loadMid, b.q[Math.floor(b.q.length / 2)]])
    const p90 = c.allBins.map((b) => [b.loadMid, b.q[b.q.length - 2]])
    // 分位带用「堆叠 + 差值」画：下沿透明，差值系列填充，才能得到带而不是面积
    const band = c.allBins.map((b) => [b.loadMid, b.q[b.q.length - 2] - b.q[1]])
    const lv = load.value
    return {
      grid: { left: 64, right: 24, top: 36, bottom: 44 },
      tooltip: {
        trigger: 'axis',
        formatter: (params: unknown) => {
          const arr = (Array.isArray(params) ? params : [params]) as Array<{
            value: [number, number]
            seriesName: string
          }>
          const keep = arr.filter((p) => !p.seriesName.includes('~') && p.seriesName !== 'P10下沿')
          if (!keep.length) return ''
          const [x] = keep[0].value
          const lines = keep.map((p) => `${p.seriesName}：${Number(p.value[1]).toFixed(1)}`)
          return `负荷 ${Number(x).toFixed(0)} MW<br/>${lines.join('<br/>')}`
        },
      },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'value',
        name: '系统负荷 MW',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        name: '出清价 元/MWh',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { lineStyle: { color: 'var(--divider)' } },
      },
      series: [
        {
          name: '历史观测',
          type: 'scatter',
          large: true,
          symbolSize: 3,
          itemStyle: { color: CHART_COLORS.gray, opacity: 0.3 },
          data: c.points.map((p) => [p.load, p.price]),
        },
        {
          name: 'P10下沿',
          type: 'line',
          stack: 'band',
          showSymbol: false,
          lineStyle: { opacity: 0 },
          silent: true,
          data: p10,
        },
        {
          name: 'P10~P90 分位带',
          type: 'line',
          stack: 'band',
          showSymbol: false,
          lineStyle: { opacity: 0 },
          areaStyle: { color: CHART_COLORS.brand, opacity: 0.12 },
          silent: true,
          data: band,
        },
        {
          name: 'P50 反演曲线',
          type: 'line',
          showSymbol: false,
          lineStyle: { width: 2, color: CHART_COLORS.brand },
          itemStyle: { color: CHART_COLORS.brand },
          data: p50,
          markLine: lv
            ? {
                silent: true,
                symbol: 'none',
                lineStyle: { type: 'dashed', color: CHART_COLORS.blue },
                label: { formatter: `当前负荷 ${Number(lv).toFixed(0)}`, fontSize: 12 },
                data: [{ xAxis: lv }],
              }
            : undefined,
        },
        {
          name: 'P90',
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dotted', color: CHART_COLORS.gray },
          data: p90,
        },
        {
          name: 'P10',
          type: 'line',
          showSymbol: false,
          lineStyle: { type: 'dotted', color: CHART_COLORS.gray },
          data: p10,
        },
      ],
    }
  },
  [curve, load],
)

/* ---------- 图：反演报价段（阶梯） ---------- */
const stepEl = ref<HTMLElement>()
useECharts(
  stepEl,
  (): EChartsOption => {
    const steps = curve.value?.steps ?? []
    return {
      grid: { left: 64, right: 56, top: 32, bottom: 44 },
      tooltip: { trigger: 'axis' },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'value',
        name: '负荷 MW',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { show: false },
      },
      yAxis: [
        {
          type: 'value',
          name: '报价 元/MWh',
          nameTextStyle: { fontSize: 12 },
          axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
          splitLine: { lineStyle: { color: 'var(--divider)' } },
        },
        {
          type: 'value',
          name: '观测点',
          nameTextStyle: { fontSize: 12 },
          axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
          splitLine: { show: false },
        },
      ],
      series: [
        {
          name: '反演报价段',
          type: 'line',
          step: 'end',
          symbol: 'rect',
          symbolSize: 6,
          lineStyle: { width: 2, color: CHART_COLORS.brand },
          itemStyle: { color: CHART_COLORS.brand },
          data: steps.map((s) => [s.loadTo, s.price]),
          label: {
            show: true,
            position: 'top',
            fontSize: 12,
            formatter: (p: { value?: unknown }) => {
              const v = p.value as [number, number]
              return Number(v[1]).toFixed(0)
            },
          },
        },
        {
          name: '该段观测点数',
          type: 'bar',
          yAxisIndex: 1,
          itemStyle: { color: CHART_COLORS.cyan, opacity: 0.35 },
          data: steps.map((s) => [(s.loadFrom + s.loadTo) / 2, s.count]),
        },
      ],
    }
  },
  [curve],
)

/* ---------- 图：报价扫描 ---------- */
const scanEl = ref<HTMLElement>()
useECharts(
  scanEl,
  (): EChartsOption => {
    const pts = scan.value
    if (!pts.length) return {}
    return {
      grid: { left: 64, right: 64, top: 32, bottom: 44 },
      tooltip: {
        trigger: 'axis',
        formatter: (params: unknown) => {
          const arr = (Array.isArray(params) ? params : [params]) as Array<{
            value: [number, number]
            seriesName: string
          }>
          if (!arr.length) return ''
          const bid = arr[0].value[0]
          const lines = arr.map((p) =>
            p.seriesName === '中标概率'
              ? `中标概率：${(p.value[1] * 100).toFixed(1)}%`
              : `期望收益：${Number(p.value[1]).toFixed(1)} 元/MWh`,
          )
          return `报价 ${Number(bid).toFixed(1)}<br/>${lines.join('<br/>')}`
        },
      },
      legend: { top: 0, icon: 'rect', itemWidth: 12, itemHeight: 3, textStyle: { fontSize: 12 } },
      xAxis: {
        type: 'value',
        name: '报价 元/MWh',
        nameTextStyle: { fontSize: 12 },
        axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
        splitLine: { show: false },
      },
      yAxis: [
        {
          type: 'value',
          name: '中标概率',
          min: 0,
          max: 1,
          nameTextStyle: { fontSize: 12 },
          axisLabel: { fontSize: 12, color: CHART_COLORS.gray, formatter: (v: number) => `${v * 100}%` },
          splitLine: { lineStyle: { color: 'var(--divider)' } },
        },
        {
          type: 'value',
          name: '期望收益 元/MWh',
          nameTextStyle: { fontSize: 12 },
          axisLabel: { fontSize: 12, color: CHART_COLORS.gray },
          splitLine: { show: false },
        },
      ],
      series: [
        {
          name: '中标概率',
          type: 'line',
          showSymbol: false,
          lineStyle: { color: CHART_COLORS.cyan },
          itemStyle: { color: CHART_COLORS.cyan },
          data: pts.map((p) => [p.bid, p.prob]),
        },
        {
          name: '期望收益',
          type: 'line',
          yAxisIndex: 1,
          showSymbol: false,
          lineStyle: { width: 2, color: CHART_COLORS.green },
          itemStyle: { color: CHART_COLORS.green },
          data: pts.map((p) => [p.bid, p.profit]),
          markLine: best.value
            ? {
                silent: true,
                symbol: 'none',
                lineStyle: { type: 'dashed', color: CHART_COLORS.green },
                label: { formatter: `最优报价 ${best.value.bid.toFixed(0)}`, fontSize: 12 },
                data: [{ xAxis: best.value.bid }],
              }
            : undefined,
        },
      ],
    }
  },
  [scan, best],
)

watch(preset, () => {
  applyPreset()
  loadCurve()
})

onMounted(() => {
  applyPreset()
  loadCurve()
})
</script>

<template>
  <div v-if="error" class="notice error">
    <strong>反演失败</strong><span>　{{ error }}</span>
    <button type="button" @click="loadCurve">重试</button>
  </div>

  <div class="grid">
    <PanelBox title="参数" :span="12" :foot="false">
      <div class="toolbar">
        <label class="field">
          市场
          <select v-model="market" @change="loadCurve">
            <option value="ahead">日前出清价</option>
            <option value="real">实时出清价</option>
          </select>
        </label>
        <span class="radio-group">
          <button v-for="p in PRESETS" :key="p.v" type="button" :class="{ on: preset === p.v }" @click="preset = p.v">
            {{ p.t }}
          </button>
        </span>
        <label class="field">起<input v-model="from" type="date" @change="loadCurve" /></label>
        <span class="muted">→</span>
        <label class="field">止<input v-model="to" type="date" @change="loadCurve" /></label>
        <label class="field">
          当前负荷 MW
          <input v-model.number="load" type="number" min="0" step="100" />
        </label>
        <label class="field">
          边际成本 元/MWh
          <input v-model.number="cost" type="number" min="0" step="10" placeholder="未填=0" />
        </label>
        <span class="spacer" />
        <button class="btn primary" type="button" :disabled="loading" @click="loadCurve">
          {{ loading ? '反演中…' : '重新反演' }}
        </button>
      </div>

      <div class="warn">
        <strong>这是统计反演，估计值</strong>
        <span>
          　从观测到的 (负荷, 出清价) 反推市场<strong>整体</strong>的聚合报价曲线，
          <strong>不是任何一台机组的真实报价</strong>。逐机组的反演需要出清机组明细 / 机组台账，
          逆优化还需要网络模型与出清模型 —— EMS 目前都没有。
        </span>
      </div>
      <div v-if="quality.text" class="warn" :class="{ bad: quality.level === 'bad' }">
        <strong>{{ quality.level === 'bad' ? '曲线可信度低' : '注意' }}</strong><span>　{{ quality.text }}</span>
      </div>
      <div v-if="costMissing" class="warn">
        <strong>未填边际成本</strong>
        <span>
          　期望收益按 0 成本算，于是「报得越低越好」，最优点会落在最低报价上。
          填上真实边际成本，最优报价才会落在中间 —— 这个取舍正是报价要解决的问题。
        </span>
      </div>
    </PanelBox>

    <PanelBox
      title="反演概览"
      :sub="stats ? `${stats.pairs} 个观测点 · ${stats.days} 天` : ''"
      :span="12"
      :loading="loading"
      :foot="
        stats
          ? `负荷 ${stats.loadMin.toFixed(0)}~${stats.loadMax.toFixed(0)} MW · 出清价 ${stats.priceMin.toFixed(0)}~${stats.priceMax.toFixed(0)} 元/MWh${curve?.sampled ? ' · 散点为等距抽样' : ''}`
          : '数据来源：电网日指标（gd_metric）'
      "
    >
      <div class="kpis">
        <KpiCard
          label="负荷-价格秩相关"
          :value="stats ? stats.spearman.toFixed(3) : '—'"
          :hint="stats && stats.spearman > 0.7 ? '价格基本由负荷单调决定，反演可信' : '相关性偏弱，曲线当参考'"
        />
        <KpiCard
          label="当前负荷历史分位"
          :value="pct(loadPct)"
          :hint="load != null ? `${Number(load).toFixed(0)} MW 在区间所有观测点中的位置` : '未设置当前负荷'"
        />
        <KpiCard
          label="当前负荷处的中位价"
          :value="grid ? fmtNum(grid[Math.floor(grid.length / 2)]) : '—'"
          unit="元/MWh"
          hint="P50 反演曲线在该负荷上的取值"
        />
        <KpiCard
          label="最优报价"
          :value="best ? fmtNum(best.bid) : '—'"
          unit="元/MWh"
          :hint="`期望收益 ${best ? fmtNum(best.profit) : '—'} 元/MWh（代理）`"
        />
      </div>
    </PanelBox>

    <PanelBox
      title="反演市场供给曲线"
      sub="横轴系统负荷 · 纵轴出清价 · 每点是一个历史观测"
      :span="12"
      :loading="loading"
      foot="负荷是刚需，出清价是需求与供给栈的交点，所以每个 (负荷, 价格) 观测点本身就是聚合供给曲线上的一个点｜只取实际值日（电查查预测占位不算）"
    >
      <div v-if="curve?.points.length" ref="curveEl" class="chart tall" />
      <div v-else-if="!loading" class="empty">
        区间内没有可用的观测点。换个区间，或确认该区间有负荷与出清价数据。
      </div>
    </PanelBox>

    <PanelBox
      title="反演报价段"
      sub="按观测点数等分成 10 段 · 段价取段内 P50 后单调化"
      :span="12"
      :loading="loading"
      :foot="
        stats
          ? `报价段必须单调，否则「负荷越高报价越低」的阶梯读不出意思；单调化最大改动 ${stats.maxPavaMove.toFixed(1)} 元/MWh`
          : '报价段'
      "
    >
      <div v-if="curve?.steps.length" ref="stepEl" class="chart" />
      <div v-else-if="!loading" class="empty">没有可切的报价段。</div>
    </PanelBox>

    <PanelBox
      title="报价扫描"
      :sub="`当前负荷 ${load != null ? Number(load).toFixed(0) : '—'} MW · 边际成本 ${effectiveCost} 元/MWh`"
      :span="12"
      :loading="loading"
      foot="假设价格接受者、成本已知、只算单位容量单时段收益；不含容量约束、爬坡、启停与阻塞"
    >
      <div v-if="scan.length" ref="scanEl" class="chart" />
      <div v-else-if="!loading" class="empty">先填一个落在历史负荷范围内的当前负荷。</div>
    </PanelBox>

    <PanelBox
      title="策略族对比"
      sub="给定目标中标概率，反解该报多少钱"
      :span="12"
      :loading="loading"
      foot="激进=低价高中标率，保守=高价低中标率；期望收益为代理值，未扣容量与启停成本"
    >
      <div v-if="family.length" class="table">
        <div class="th">策略</div>
        <div class="th">目标中标概率</div>
        <div class="th">建议报价 元/MWh</div>
        <div class="th">实际中标概率</div>
        <div class="th">中标时均价 元/MWh</div>
        <div class="th">期望收益代理</div>
        <template v-for="row in family" :key="row.key">
          <div>{{ row.label }}</div>
          <div class="num">{{ pct(row.prob, 0) }}</div>
          <div class="num strong">{{ fmtNum(row.bid) }}</div>
          <div class="num">{{ pct(row.actualProb) }}</div>
          <div class="num">{{ fmtNum(row.avgPrice) }}</div>
          <div class="num">{{ fmtNum(row.profit) }}</div>
        </template>
      </div>
      <div v-else-if="!loading" class="empty">没有可对比的策略。</div>
    </PanelBox>

    <PanelBox title="反演方法" :span="6" foot="设计稿 §14.1 的两种方法：本页是第 ② 种">
      <ol class="notes">
        <li><strong>为什么能反演</strong>：负荷基本刚需，出清价 = 需求 ∩ 供给栈；供给栈不变时负荷怎么变价格就沿着同一条曲线走，于是观测点全部落在供给曲线上。</li>
        <li><strong>怎么做</strong>：按负荷等宽分箱 → 每箱取价格分位 → 中位数即 P50 曲线，P10/P90 是不同日子供给栈陡缓的差异。</li>
        <li><strong>怎么用</strong>：在给定负荷上，分位点就是「报到这个价能中标的日子占比」，由此反解报价、中标概率与期望收益。</li>
        <li><strong>报价段</strong>：按观测点数等分（不是按负荷宽度等分 —— 低负荷往往挤了大量点），段价再做保序回归（PAVA）保证单调。</li>
      </ol>
    </PanelBox>

    <PanelBox title="局限与缺口" :span="6" foot="不假装有能力：这些缺口决定了本页只是参考">
      <ul class="notes">
        <li><strong>反演的是市场整体</strong>：不是某台机组的报价。逐机组反演要出清机组明细与机组台账。</li>
        <li><strong>用系统负荷而非净负荷</strong>：新能源出力数据未接入，同样的负荷下实际供需紧张度差别很大，散点因此偏散。</li>
        <li><strong>没有网络与阻塞</strong>：节点价差、断面限额都看不到，阻塞造成的价差会被当成报价行为。</li>
        <li><strong>没有机组成本</strong>：期望收益依赖输入的边际成本；成本填错，最优报价就跟着错。</li>
        <li><strong>供给栈会漂移</strong>：燃料价、检修、新增装机让曲线随时间变；逆序数与秩相关就是用来提示这件事的。</li>
        <li><strong>逆优化那一路没做</strong>：需要出清模型（SCUC/SCED）+ 网络模型 + 机组成本，属外部数据依赖。</li>
      </ul>
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
  padding: 0 var(--s2);
  border: 0;
  border-radius: var(--r-sm);
  background: var(--red);
  color: #fff;
  font: inherit;
  font-size: var(--fs-12);
  cursor: pointer;
}
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
.field input,
.field select {
  width: 120px;
  padding: var(--s1) var(--s2);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-2);
  color: var(--text-1);
  font: inherit;
  font-size: var(--fs-13);
}
.field input[type='date'] { width: 140px; }
.warn {
  display: flex;
  align-items: flex-start;
  gap: var(--s2);
  flex-wrap: wrap;
  margin-top: var(--s3);
  padding: var(--s2) var(--s3);
  border-left: 3px solid var(--brand);
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand-ink);
  font-size: var(--fs-12);
}
.warn.bad { border-left-color: var(--red); background: var(--red-soft); color: var(--red-ink); }
.kpis { display: grid; grid-template-columns: repeat(4, 1fr); gap: var(--s4); }
.chart { height: 320px; }
.chart.tall { height: 420px; }
.table {
  display: grid;
  grid-template-columns: 72px repeat(5, 1fr);
  gap: 1px;
  background: var(--border);
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  overflow: hidden;
  font-size: var(--fs-13);
}
.table > div { padding: var(--s2) var(--s3); background: var(--bg-2); }
.table .th { background: var(--bg-3); color: var(--text-3); font-size: var(--fs-12); }
.table .num { text-align: right; font-variant-numeric: tabular-nums; }
.table .strong { color: var(--brand-ink); font-weight: 600; }
.notes { margin: 0; padding-left: var(--s5); font-size: var(--fs-13); color: var(--text-2); line-height: 1.9; }
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
@media (max-width: 1280px) {
  .grid :deep(.panel) { grid-column: span 12 !important; }
  .kpis { grid-template-columns: repeat(2, 1fr); }
}
</style>
