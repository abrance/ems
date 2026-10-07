import type { SupplyBin } from '@/api/gdmetric'

/**
 * 反演供给曲线的决策用法。
 *
 * 反演出来的曲线本身只是「负荷 → 价格」的分布。能拿它做决定，是因为它回答了
 * 报价者真正关心的问题：
 *
 *   在某个负荷水平上，**报到多少钱**才能中标？中标概率多少？期望收益多少？
 *
 * 模型假设（必须写在页面上，这些假设不成立时结论就不成立）：
 *   1. 价格接受者：我们的报价不影响出清价。
 *   2. 边际成本已知且给定；只考虑「单位容量在单一时段」的期望收益。
 *   3. 中标即按出清价结算，不考虑容量约束、爬坡、启停、阻塞。
 *
 * 条件分布 F(p | 负荷≈q) 由分箱的分位点线性插值得到：
 * 第 k 段分位点之间的概率质量为 1/N，在该段内视作均匀分布。
 */

/** 从分箱分位点取出相邻两箱插值后的条件分位数组。 */
export function quantileGridAt(bins: SupplyBin[], load: number): number[] | null {
  if (!bins.length) return null
  const usable = bins.filter((b) => Array.isArray(b.q) && b.q.length >= 2)
  if (!usable.length) return null

  if (load <= usable[0].loadMid) return usable[0].q.slice()
  const last = usable[usable.length - 1]
  if (load >= last.loadMid) return last.q.slice()

  for (let i = 0; i < usable.length - 1; i++) {
    const a = usable[i]
    const b = usable[i + 1]
    if (load >= a.loadMid && load <= b.loadMid) {
      const span = b.loadMid - a.loadMid
      // 相邻两箱负荷中点重合时无法插值，用近的一箱
      if (span <= 0) return a.q.slice()
      const t = (load - a.loadMid) / span
      return a.q.map((v, k) => v + t * (b.q[k] - v))
    }
  }
  return last.q.slice()
}

/**
 * F(x) = P(价格 **<** x | 负荷)。
 * 取严格小于，是为了和 probAbove 同口径：`probAbove(x) = 1 - cdf(x) = P(价格 ≥ x)`，
 * 也就是「报价与出清价刚好相等时算中标」。分位点落在分位点上时，把等值平台整段算作不小于它。
 */
export function cdf(grid: number[], x: number): number {
  const spans = grid.length - 1
  if (spans <= 0) return x <= grid[0] ? 0 : 1
  if (x <= grid[0]) return 0
  if (x > grid[grid.length - 1]) return 1

  let k = spans
  for (let i = 0; i <= spans; i++) {
    if (grid[i] >= x) {
      k = i
      break
    }
  }
  if (k === 0) return 0
  if (grid[k] === x) {
    let j = k
    while (j > 0 && grid[j - 1] === x) j--
    return j / spans
  }
  return (k - 1 + (x - grid[k - 1]) / (grid[k] - grid[k - 1])) / spans
}

/** 报到 bid 时的中标概率 = P(出清价 ≥ bid)。 */
export function probAbove(grid: number[], bid: number): number {
  return clamp01(1 - cdf(grid, bid))
}

/**
 * 反解：要达到目标中标概率 prob，该报多少钱。
 * F 单调不减但可能有平台段，用二分而不是代数反解 —— 平台段上代数反解会算错。
 */
export function bidForProb(grid: number[], prob: number): number | null {
  const p = clamp01(prob)
  if (p >= 1) return grid[0]
  if (p <= 0) return grid[grid.length - 1]
  let lo = grid[0]
  let hi = grid[grid.length - 1]
  if (hi <= lo) return null
  for (let i = 0; i < 60; i++) {
    const mid = (lo + hi) / 2
    if (probAbove(grid, mid) > p) {
      lo = mid // 中标概率还偏高 → 还能报高一点
    } else {
      hi = mid
    }
  }
  return (lo + hi) / 2
}

/**
 * 单位容量的期望收益（代理）：
 *   E[(出清价 − 边际成本) · 1{出清价 ≥ bid}]
 * 报价高于成本时中标概率下降但单次收益上升，最优点在中间 —— 这正是要用它做决策的原因。
 */
export function expectedProfit(grid: number[], bid: number, cost: number): number {
  const spans = grid.length - 1
  if (spans <= 0) return 0
  // 每段（含退化为点的段）承载同样的 1/spans 概率质量。
  // 退化段不是「没有数据」，是「分位点相等」——它有质量、没有宽度，必须照样计入，
  // 否则这里的期望收益与 cdf 的口径就不一致（同一件事两个算法给两个数）。
  const mass = 1 / spans
  let total = 0
  for (let k = 0; k < spans; k++) {
    const lo = grid[k]
    const hi = grid[k + 1]
    if (hi > lo) {
      const from = Math.max(lo, bid)
      if (hi <= from) continue
      total += mass * ((hi - from) / (hi - lo)) * ((from + hi) / 2 - cost)
    } else if (lo >= bid) {
      total += mass * (lo - cost)
    }
  }
  return total
}

/** 中标时的平均出清价 = E[价格 | 价格 ≥ bid]。 */
export function priceGivenDispatched(grid: number[], bid: number): number | null {
  const p = probAbove(grid, bid)
  if (p <= 1e-9) return null
  // cost=0 时 expectedProfit 就是 E[p·1{p≥bid}]，直接除以中标概率即可。
  // 这里曾写成 (expectedProfit + bid*p)/p，把报价加了两次（拆项时用了 cost=bid 的恒等式）。
  return expectedProfit(grid, bid, 0) / p
}

export interface StrategyRow {
  key: 'aggressive' | 'steady' | 'conservative'
  label: string
  /** 目标中标概率 */
  prob: number
  /** 对应报价；无法反解时为 null */
  bid: number | null
  /** 实际中标概率（二分求得的报价再算回去，用于核对） */
  actualProb: number | null
  /** 中标时的平均出清价 */
  avgPrice: number | null
  /** 期望收益代理 */
  profit: number | null
}

/** 策略族：激进=低价高中标率，保守=高价低中标率。 */
export function strategyFamily(grid: number[], cost: number): StrategyRow[] {
  const defs: Array<Pick<StrategyRow, 'key' | 'label' | 'prob'>> = [
    { key: 'aggressive', label: '激进', prob: 0.8 },
    { key: 'steady', label: '稳健', prob: 0.5 },
    { key: 'conservative', label: '保守', prob: 0.2 },
  ]
  return defs.map((d) => {
    const bid = bidForProb(grid, d.prob)
    if (bid == null) return { ...d, bid: null, actualProb: null, avgPrice: null, profit: null }
    return {
      ...d,
      bid,
      actualProb: probAbove(grid, bid),
      avgPrice: priceGivenDispatched(grid, bid),
      profit: expectedProfit(grid, bid, cost),
    }
  })
}

export interface BidScanPoint {
  bid: number
  prob: number
  profit: number
}

/**
 * 报价扫描：在 [max(成本, 最低价), 最高价] 上均匀取点，
 * 返回「报价 → 中标概率 / 期望收益」曲线。
 * 下界取成本而不是最低价：报得比成本还低，中标了也是亏，不该出现在候选里。
 */
export function bidScan(grid: number[], cost: number, steps = 120): BidScanPoint[] {
  const lo = Math.max(grid[0], cost)
  const hi = grid[grid.length - 1]
  if (hi <= lo) return []
  const out: BidScanPoint[] = []
  for (let i = 0; i <= steps; i++) {
    const bid = lo + ((hi - lo) * i) / steps
    out.push({ bid, prob: probAbove(grid, bid), profit: expectedProfit(grid, bid, cost) })
  }
  return out
}

/** 扫描曲线上的最优点（期望收益最大）。 */
export function optimalBid(grid: number[], cost: number, steps = 240): BidScanPoint | null {
  const scan = bidScan(grid, cost, steps)
  if (!scan.length) return null
  let best = scan[0]
  for (const p of scan) {
    if (p.profit > best.profit) best = p
  }
  return best
}

/** 当前负荷落在历史负荷分布的哪个分位（0~1），按各分箱的观测数累加。 */
export function loadPercentile(bins: SupplyBin[], load: number): number | null {
  const total = bins.reduce((a, b) => a + b.count, 0)
  if (!total) return null
  let below = 0
  for (const b of bins) {
    if (load >= b.loadTo) below += b.count
    else if (load > b.loadFrom) below += (b.count * (load - b.loadFrom)) / (b.loadTo - b.loadFrom)
  }
  return clamp01(below / total)
}

function clamp01(v: number): number {
  return v < 0 ? 0 : v > 1 ? 1 : v
}
