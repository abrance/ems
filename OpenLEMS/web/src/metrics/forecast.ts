/**
 * 预测复盘指标。
 *
 * 公式集中在这一个文件里，页面不各算一遍 —— 公式还在试，改一处就全改。
 *
 * 口径（与用户逐条确认，改动前先看这里）：
 *   1. 趋势相关度用 **Spearman ρ**（秩相关）。电价有尖峰、有负价，Pearson 会被尖峰主导。
 *   2. 分时离散度分母用 **当日出清均价**，不是逐点实际值 ——
 *      广东有负电价，`|err|/实际值` 在分母趋 0 时会爆表。
 *   3. 峰/平/谷 时段来自「管理 · 电价管理」的时段配置（单一真源），
 *      Peak+Sharp → 峰，Flat → 平，Valley+DeepValley → 谷。
 */

/** 时段归类 */
export type Segment = 'peak' | 'flat' | 'valley' | 'all'

export const SEGMENT_LABEL: Record<Segment, string> = {
  peak: '峰',
  flat: '平',
  valley: '谷',
  all: '全天',
}

/** 电价类型 → 时段（与后端 EPriceType 的字符串值对应） */
export function segmentOfPriceType(priceType: string): Segment {
  switch (priceType) {
    case 'Peak':
    case 'Sharp':
      return 'peak'
    case 'Flat':
      return 'flat'
    case 'Valley':
    case 'DeepValley':
      return 'valley'
    default:
      return 'flat'
  }
}

export interface SegmentRange {
  /** "HH:MM" */
  startTime: string
  endTime: string
  segment: Segment
}

/** 从电价配置的时段构造「分钟区间 → 时段」的查表函数 */
export function buildSegmentLookup(ranges: SegmentRange[]): (minutes: number) => Segment | null {
  const spans = ranges
    .map((r) => ({ start: toMinutes(r.startTime), end: endMinutes(r.endTime), segment: r.segment }))
    .filter((s) => Number.isFinite(s.start) && Number.isFinite(s.end) && s.end > s.start)
  return (minutes: number) => {
    for (const s of spans) {
      if (minutes >= s.start && minutes < s.end) return s.segment
    }
    return null
  }
}

export function toMinutes(hhmm: string): number {
  const [h, m] = String(hhmm || '').split(':').map(Number)
  if (!Number.isFinite(h) || !Number.isFinite(m)) return NaN
  return h * 60 + m
}

/** 00:00 作为一天结束要当 24:00；否则 20:00-00:00 会被判成 end < start */
export function endMinutes(hhmm: string): number {
  const v = toMinutes(hhmm)
  return v === 0 ? 24 * 60 : v
}

/**
 * Spearman 秩相关系数。
 * 并列值取平均秩；样本 < 3 或某一侧全等（秩无方差）时返回 null ——
 * 这两种情况相关系数没有意义，返回 0 会让人误以为「不相关」。
 */
export function spearman(xs: number[], ys: number[]): number | null {
  const n = Math.min(xs.length, ys.length)
  if (n < 3) return null
  const rx = averageRanks(xs.slice(0, n))
  const ry = averageRanks(ys.slice(0, n))
  const mx = mean(rx)
  const my = mean(ry)
  let num = 0
  let dx = 0
  let dy = 0
  for (let i = 0; i < n; i++) {
    const a = rx[i] - mx
    const b = ry[i] - my
    num += a * b
    dx += a * a
    dy += b * b
  }
  if (dx === 0 || dy === 0) return null
  return num / Math.sqrt(dx * dy)
}

/** 平均秩（升序；并列取平均） */
function averageRanks(values: number[]): number[] {
  const idx = values.map((v, i) => ({ v, i })).sort((a, b) => a.v - b.v)
  const ranks = new Array<number>(values.length)
  let i = 0
  while (i < idx.length) {
    let j = i
    while (j + 1 < idx.length && idx[j + 1].v === idx[i].v) j++
    const avg = (i + j) / 2 + 1 // 秩从 1 开始
    for (let k = i; k <= j; k++) ranks[idx[k].i] = avg
    i = j + 1
  }
  return ranks
}

export function mean(values: number[]): number {
  if (!values.length) return 0
  return values.reduce((a, b) => a + b, 0) / values.length
}

export interface PairedPoint {
  /** 目标时刻，便于按日分组与按 HH:MM 取时段 */
  targetTime: string
  predicted: number
  actual: number
}

export interface DispersionPoint {
  targetTime: string
  /** 标幺：|pred − actual| / 当日均价 */
  pct: number
  /** 数值：|pred − actual|，单位与电价一致 */
  abs: number
  segment: Segment
  /** 当日均价，用于 tooltip 说明分母 */
  dayAvg: number
}

/**
 * 逐点离散度。分母 = 该点所属**当日实际值的均价**（口径 2）。
 */
export function dispersion(
  points: PairedPoint[],
  lookup: (minutes: number) => Segment | null,
): DispersionPoint[] {
  const byDay = new Map<string, number[]>()
  for (const p of points) {
    const day = dayOf(p.targetTime)
    if (!byDay.has(day)) byDay.set(day, [])
    byDay.get(day)!.push(p.actual)
  }
  const dayAvg = new Map<string, number>()
  for (const [day, values] of byDay) {
    const avg = mean(values)
    // 均价接近 0 时标幺没有意义（负价+正价可以互相抵消到 0），标记为不可用
    dayAvg.set(day, Math.abs(avg) < 1e-6 ? NaN : avg)
  }

  return points.map((p) => {
    const day = dayOf(p.targetTime)
    const avg = dayAvg.get(day) ?? NaN
    const err = Math.abs(p.predicted - p.actual)
    return {
      targetTime: p.targetTime,
      pct: Number.isFinite(avg) ? err / Math.abs(avg) : NaN,
      abs: err,
      segment: lookup(minutesOf(p.targetTime)) ?? 'all',
      dayAvg: avg,
    }
  })
}

export interface SegmentStats {
  segment: Segment
  /** 样本点数 */
  count: number
  /** Spearman ρ，样本不足时为 null */
  rho: number | null
  /** 平均离散度（标幺），无有效分母时为 null */
  avgPct: number | null
  /** 平均绝对误差（元/MWh） */
  mae: number | null
}

/** 分区段汇总：ρ + 平均离散度 + MAE */
export function segmentStats(points: PairedPoint[], disp: DispersionPoint[]): SegmentStats[] {
  const out: SegmentStats[] = []
  for (const segment of ['peak', 'flat', 'valley', 'all'] as Segment[]) {
    const picked = segment === 'all' ? points : points.filter((p) => disp.find((d) => d.targetTime === p.targetTime)?.segment === segment)
    const pickedDisp = segment === 'all' ? disp : disp.filter((d) => d.segment === segment)
    if (!picked.length) {
      out.push({ segment, count: 0, rho: null, avgPct: null, mae: null })
      continue
    }
    const usable = pickedDisp.filter((d) => Number.isFinite(d.pct))
    out.push({
      segment,
      count: picked.length,
      rho: spearman(
        picked.map((p) => p.predicted),
        picked.map((p) => p.actual),
      ),
      avgPct: usable.length ? mean(usable.map((d) => d.pct)) : null,
      mae: mean(pickedDisp.map((d) => d.abs)),
    })
  }
  return out
}

function dayOf(targetTime: string): string {
  return String(targetTime || '').slice(0, 10)
}

function minutesOf(targetTime: string): number {
  const t = String(targetTime || '')
  const hhmm = t.length > 10 ? t.slice(11, 16) : ''
  return toMinutes(hhmm)
}
