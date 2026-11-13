/**
 * 未入库日期折叠。
 *
 * 为什么必须折叠：缺一天列一天，缺几十天就是几十个顿号连成一行。默认窗口一打开
 * 就是 39 个日期，手选 2020 年起是 712 天 —— 版面被列表铺满，真正有用的信息
 * （缺多少天、缺在哪几段）反而看不见。这是每次打开都会遇到的常态，不是极端操作。
 *
 * 口径：
 *   - 3 天以内照旧逐个列（短列表比区间好读）；
 *   - 超过 3 天按连续区间折叠，最多列 3 段，其余写成「等 N 段」；
 *   - 有解析不了的日期就整体退回逐个列 —— 宁可长，不许悄悄吞掉一天。
 */
export interface DateRange {
  from: string
  to: string
  /** 这段连续多少天（含首尾） */
  days: number
}

const DAY_MS = 86_400_000
/** 折叠门槛：不超过这么多天就逐个列 */
const FOLD_OVER = 3
/** 折叠后最多列几段 */
const MAX_SEGMENTS = 3

function dayNumber(iso: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
  if (!m) return null
  const t = Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  return Number.isFinite(t) ? Math.round(t / DAY_MS) : null
}

function isoOf(day: number): string {
  return new Date(day * DAY_MS).toISOString().slice(0, 10)
}

/** 把日期列表折成连续区间（升序、去重）；有解析不了的日期时返回空数组 */
export function missingRanges(dates: string[]): DateRange[] {
  const nums: number[] = []
  for (const d of dates) {
    const n = dayNumber(d)
    if (n === null) return []
    nums.push(n)
  }
  const sorted = [...new Set(nums)].sort((a, b) => a - b)
  if (!sorted.length) return []

  const out: DateRange[] = []
  let start = sorted[0]
  let prev = sorted[0]
  for (let i = 1; i <= sorted.length; i++) {
    const cur = sorted[i]
    if (cur !== undefined && cur === prev + 1) {
      prev = cur
      continue
    }
    out.push({ from: isoOf(start), to: isoOf(prev), days: prev - start + 1 })
    start = cur
    prev = cur
  }
  return out
}

/**
 * 页面直接渲染的一句话。空态与「未入库日期」面板共用它，避免两处口径分叉。
 * 空列表返回空串（调用方按 falsy 判断，不渲染）。
 */
export function missingSummary(dates: string[]): string {
  if (!dates.length) return ''
  const unique = [...new Set(dates)]
  const ranges = missingRanges(unique)
  // 日期解析不了就退回逐个列：折叠的前提是「知道缺了哪几天」，不知道就别折
  if (unique.length <= FOLD_OVER || !ranges.length) {
    // 逐个列也要按日期升序：调用方传乱序时印输入顺序，会被读成「缺了这几天」的错序。
    // ranges 非空就说明全部日期可解析，能排；有坏日期时（ranges 为空）退回原样，
    // 宁可长，不许悄悄丢一天。
    const listed = ranges.length
      ? [...unique].sort((a, b) => (dayNumber(a) as number) - (dayNumber(b) as number))
      : unique
    return `区间内未入库：${listed.join('、')}`
  }

  const multi = ranges.length > 1
  const shown = ranges.slice(0, MAX_SEGMENTS).map((r) => {
    if (r.days === 1) return r.from
    // 只有一段时「连续」二字已够，多段才需要各自的连续天数
    return `${r.from} ~ ${r.to}${multi ? `（连续 ${r.days} 天）` : '（连续）'}`
  })
  const rest = ranges.length - shown.length
  if (rest > 0) shown.push(`等 ${rest} 段`)
  return `区间内未入库 ${unique.length} 天：${shown.join('、')}`
}
