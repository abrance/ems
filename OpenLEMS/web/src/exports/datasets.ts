import { fetchBacktest, fetchEffectSummary } from '@/api/effect'
import { fetchPriceBacktest, fetchPriceForecast } from '@/api/forecast'
import { fetchMetricSeries, fetchSupplyCurve } from '@/api/gdmetric'
import { getReportData, listReports } from '@/api/spotreport'

/**
 * 报表导出的数据集定义。
 *
 * 每个数据集 = 一组参数 + 一个加载器 + 一份列定义。刻意与页面分离：
 * 加一个数据集只改这个文件，UI 不用动（表单是按 params 渲染出来的）。
 *
 * **诚实边界**：这些都是浏览器里已有的数据（各页用的同一批接口），
 * 前端拼 CSV 直接下载。没有的东西也要写清楚 —— 见页面底部的「没做的部分」。
 */

export type ParamType = 'date' | 'select' | 'number'

export interface ParamDef {
  key: string
  label: string
  type: ParamType
  /** select 用 */
  options?: Array<{ value: string; label: string }>
  default: string
  hint?: string
  min?: number
  max?: number
}

export interface DatasetResult {
  columns: string[]
  rows: unknown[][]
  /** 覆盖默认文件名用（可留空） */
  suffix?: string
}

export interface Dataset {
  key: string
  title: string
  desc: string
  /** 数据集来源（哪几个接口），显示在面板脚注里 */
  source: string
  params: ParamDef[]
  load: (p: Record<string, string>) => Promise<DatasetResult>
}

const MARKET: ParamDef = {
  key: 'market',
  label: '市场',
  type: 'select',
  options: [
    { value: 'ahead', label: '日前' },
    { value: 'real', label: '实时' },
  ],
  default: 'ahead',
}

function day(offsetDays: number): string {
  const d = new Date(Date.now() - offsetDays * 86400_000)
  const loc = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
  return loc.toISOString().slice(0, 10)
}

function dateParam(key: string, label: string, offsetDays: number): ParamDef {
  return { key, label, type: 'date', default: day(offsetDays) }
}

const RANGE_PARAMS: ParamDef[] = [
  dateParam('from', '起始日期', 6),
  dateParam('to', '结束日期', 0),
]

function rangeDays(p: Record<string, string>): number {
  const from = new Date(`${p.from}T00:00:00Z`).getTime()
  const to = new Date(`${p.to}T00:00:00Z`).getTime()
  if (!Number.isFinite(from) || !Number.isFinite(to) || to < from) return NaN
  return Math.round((to - from) / 86400_000) + 1
}

const MARKET_LABEL: Record<string, string> = { ahead: '日前', real: '实时' }

export const DATASETS: Dataset[] = [
  {
    key: 'grid-metric',
    title: '电网日指标（96 点）',
    desc: '负荷或出清价的 15 分钟序列，按区间导出。',
    source: 'GET /api/gd-metric/series',
    params: [
      {
        key: 'kind',
        label: '类别',
        type: 'select',
        options: [
          { value: 'load', label: '系统负荷' },
          { value: 'dayAhead', label: '日前出清价' },
          { value: 'realtime', label: '实时出清价' },
        ],
        default: 'dayAhead',
      },
      dateParam('from', '起始日期', 6),
      dateParam('to', '结束日期', 0),
    ],
    async load(p) {
      // 后端 MaxSeriesDays = 400，超了会 400，这里先拦
      const days = rangeDays(p)
      if (!Number.isFinite(days)) throw new Error('日期区间无效')
      if (days > 400) throw new Error(`区间 ${days} 天，超过上限 400 天`)
      const kindLabel =
        p.kind === 'load' ? '系统负荷 MW' : p.kind === 'dayAhead' ? '日前出清价 元/MWh' : '实时出清价 元/MWh'
      const res = await fetchMetricSeries({ from: p.from, to: p.to, kind: p.kind })
      return {
        columns: ['日期', '时刻', '类别', '数值'],
        rows: res.points.map((s) => [s.date, s.time, kindLabel, s.value]),
      }
    },
  },
  {
    key: 'spot-metrics',
    title: '现货日报指标',
    desc: '日报里已提取的披露指标（含人工确认状态与来源）。每天一次请求，所以区间上限 31 天。',
    source: 'GET /api/spot-report · GET /api/spot-report/data?date',
    params: [dateParam('from', '起始日期', 6), dateParam('to', '结束日期', 0)],
    async load(p) {
      const days = rangeDays(p)
      if (!Number.isFinite(days)) throw new Error('日期区间无效')
      if (days > 31) throw new Error(`区间 ${days} 天，每天一次请求，上限 31 天`)
      const reports = await listReports()
      const inRange = reports
        .map((r) => String(r.date ?? '').slice(0, 10))
        .filter((d) => d && d >= p.from && d <= p.to)
        .sort()
      const rows: unknown[][] = []
      for (const date of inRange) {
        const data = await getReportData(date)
        for (const m of data.metrics ?? []) {
          rows.push([
            m.runDate,
            m.dataset,
            m.scope,
            m.metric,
            m.value,
            m.unit,
            m.atTime,
            m.textValue,
            m.origin,
            m.conf,
            m.source,
          ])
        }
      }
      return {
        // 来源/置信度必须一起导：人工确认前的指标带 origin=cv（图像采样）之类的标记，
        // 导出去以后没有页面替你分辨哪一行是估的。
        columns: ['运行日', '数据集', '口径', '指标', '数值', '单位', '时刻', '文本值', '来源', '置信度', '数据源'],
        rows,
      }
    },
  },
  {
    key: 'price-forecast',
    title: '电价预测（TimesFM）',
    desc: '模型输入的历史序列 + 预测点及分位带，一次导出成一张纵向表。',
    source: 'GET /api/forecast/price',
    params: [
      MARKET,
      {
        key: 'grain',
        label: '粒度',
        type: 'select',
        options: [
          { value: 'day', label: '日均（日报）' },
          { value: 'intraday', label: '96 点' },
        ],
        default: 'intraday',
      },
      { key: 'historyDays', label: '历史天数', type: 'number', default: '30', min: 1, max: 400 },
      { key: 'horizonDays', label: '预测天数', type: 'number', default: '5', min: 1, max: 15 },
    ],
    async load(p) {
      const res = await fetchPriceForecast({
        market: p.market,
        grain: p.grain,
        historyDays: Number(p.historyDays) || 30,
        horizonDays: Number(p.horizonDays) || 5,
      })
      const rows: unknown[][] = []
      // 历史点没有时刻字段（historyDates 是运行日），预测点有 runDate
      res.historyDates?.forEach((d, i) => {
        rows.push(['历史', d, '', '', '', res.historyValues?.[i] ?? ''])
      })
      for (const pt of res.forecast ?? []) {
        rows.push([
          '预测',
          pt.runDate,
          pt.quantiles?.['0.1'] ?? '',
          pt.quantiles?.['0.5'] ?? '',
          pt.quantiles?.['0.9'] ?? '',
          pt.point,
        ])
      }
      return {
        columns: ['类别', '目标时刻', 'P10', 'P50', 'P90', '值'],
        rows,
        suffix: `${MARKET_LABEL[p.market] ?? p.market}-${p.grain}`,
      }
    },
  },
  {
    key: 'forecast-backtest',
    title: '预测复盘·回测对比',
    desc: '用当前模型在历史数据上重跑，与实际逐点配对。注意这不是「当时发出去的预测」，真复盘看留痕。',
    source: 'GET /api/forecast/backtest',
    params: [MARKET, dateParam('from', '起始日期', 6), dateParam('to', '结束日期', 0)],
    async load(p) {
      const res = await fetchPriceBacktest({ market: p.market, from: p.from, to: p.to, sampleDays: 3 })
      return {
        columns: ['目标时刻', '实际', '预测', 'P10', 'P50', 'P90', '误差', '绝对误差'],
        rows: res.points.map((pt) => [
          pt.targetTime,
          pt.actual,
          pt.predicted,
          pt.q10,
          pt.q50,
          pt.q90,
          Number((pt.predicted - pt.actual).toFixed(4)),
          Number(Math.abs(pt.predicted - pt.actual).toFixed(4)),
        ]),
        suffix: MARKET_LABEL[p.market] ?? p.market,
      }
    },
  },
  {
    key: 'supply-curve',
    title: '策略反演·反演报价段',
    desc: '由 (负荷, 出清价) 反演出的市场聚合报价段（估计值，不是任何一台机组的真实报价）。',
    source: 'GET /api/gd-metric/supply-curve',
    params: [MARKET, dateParam('from', '起始日期', 89), dateParam('to', '结束日期', 0)],
    async load(p) {
      const res = await fetchSupplyCurve({ from: p.from, to: p.to, market: p.market })
      return {
        columns: ['负荷下限 MW', '负荷上限 MW', '反演报价 元/MWh', '观测点数'],
        rows: res.steps.map((s) => [
          Number(s.loadFrom.toFixed(0)),
          Number(s.loadTo.toFixed(0)),
          Number(s.price.toFixed(2)),
          s.count,
        ]),
        suffix: MARKET_LABEL[p.market] ?? p.market,
      }
    },
  },
  {
    key: 'effect-summary',
    title: '效果评估·实测汇总',
    desc: '区间内的实测套利与需量节省（单行汇总，列就是各指标）。',
    source: 'GET /api/effect/summary',
    params: RANGE_PARAMS,
    async load(p) {
      const s = await fetchEffectSummary({ start: p.from, end: p.to })
      return {
        columns: ['指标', '数值'],
        rows: [
          ['区间', `${s.start} ~ ${s.end}`],
          ['统计点数', s.points],
          ['算法', s.algorithm],
          ['充电量 kWh', s.chargeEnergy],
          ['放电量 kWh', s.dischargeEnergy],
          ['充电成本', s.chargeCost],
          ['放电收入', s.dischargeRevenue],
          ['峰谷套利收益', s.arbitrageProfit],
          ['实际最大需量 kW', s.actualMaxDemand],
          ['基线最大需量 kW', s.baselineMaxDemand],
          ['需量节省', s.demandSaving],
          ['合计节省', s.totalSaving],
        ],
      }
    },
  },
  {
    key: 'effect-backtest',
    title: '效果评估·策略回测对比',
    desc: '同一区间、同一成本模型下各策略的收益对比（含 idle 基线）。',
    source: 'GET /api/effect/backtest',
    params: RANGE_PARAMS,
    async load(p) {
      const b = await fetchBacktest({ start: p.from, end: p.to })
      return {
        columns: [
          '算法',
          '充电量 kWh',
          '放电量 kWh',
          '充电成本',
          '放电收入',
          '峰谷套利收益',
          '实际最大需量 kW',
          '基线最大需量 kW',
          '需量节省',
          '合计节省',
          '末端 SOC %',
        ],
        rows: (b.results ?? []).map((r) => [
          r.algorithm,
          r.chargeEnergy,
          r.dischargeEnergy,
          r.chargeCost,
          r.dischargeRevenue,
          r.arbitrageProfit,
          r.actualMaxDemand,
          r.baselineMaxDemand,
          r.demandSaving,
          r.totalSaving,
          r.finalSocPct,
        ]),
      }
    },
  },
]

export function findDataset(key: string): Dataset | undefined {
  return DATASETS.find((d) => d.key === key)
}

/** 按定义生成一组默认参数 */
export function defaultParams(ds: Dataset): Record<string, string> {
  const out: Record<string, string> = {}
  for (const p of ds.params) out[p.key] = p.default
  return out
}
