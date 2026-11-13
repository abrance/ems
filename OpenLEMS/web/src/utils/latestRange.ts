/**
 * 默认区间该从哪一天算起。
 *
 * 为什么需要：数据（gd_metric.csv、现货日报）常常**滞后于现实日期** ——
 * 部署上数据只到 2026-08-31，而当天是 10-07。于是「今天往前 7 天」这种默认区间
 * 整段落在数据之外，打开页面就是空/报错：
 *
 *   预测复盘：回测失败: 区间 2026-10-01 ~ 2026-10-07 内没有完整（96 点）的日前电价数据
 *   历史信息：当前区间没有已确认的现货指标
 *
 * 后端已经这么处理了（`/api/gd-metric` 默认取最新可用日，价格预测也会把锚点收到
 * 最新数据的日期），前端这里对齐同一口径：**取「今天」与「数据最新日」中较早的那个**。
 *
 * 注意别把所有区间都改小：90 天这类长窗口本来就覆盖得到数据（策略反演的默认区间
 * 就是 90 天，一直正常）。要收的是**窗口可能整段落在数据之后**的那些。
 */
export function anchorEndDate(latestDate?: string | null, today: Date = new Date()): Date {
  if (!latestDate) return today
  const parsed = new Date(`${latestDate}T00:00:00`)
  if (Number.isNaN(parsed.getTime())) return today
  return parsed.getTime() < today.getTime() ? parsed : today
}

/**
 * 上海时区的「今天」YYYY-MM-DD。
 *
 * 复盘页要判断「首个可配对日是否已经过去」，这个判断必须和后端（Asia/Shanghai）
 * 以及 lagDaysFrom 同一口径：容器里跑的无头浏览器是 UTC，用本地时区会在跨日前后
 * 把「今天」算成昨天，于是「约在今天」被误判成「已过」。
 */
export function todayInShanghai(today: Date = new Date()): string {
  return new Date(today.getTime() + 8 * 3600_000).toISOString().slice(0, 10)
}

/** 本地日期 → YYYY-MM-DD（不用 toISOString，那个会按 UTC 偏移一天） */
export function isoDate(d: Date): string {
  const loc = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
  return loc.toISOString().slice(0, 10)
}

/** 以 end 为右端往前 days 天的区间 */
export function rangeEndingAt(end: Date, days: number): { from: string; to: string } {
  return { from: isoDate(new Date(end.getTime() - days * 86400_000)), to: isoDate(end) }
}

/**
 * 数据最新日距今天几天（自然日，按 Asia/Shanghai 算）。
 *
 * 为什么固定时区：复盘页的「实际值截至 X · 滞后 N 天」是给广东现货市场看的，
 * 用浏览器本地时区算会在跨时区（或 UTC 容器里的无头浏览器）漂一天，
 * 同一个数据在两个地方显示成 2 天和 3 天。
 * 算法：都把「日期」换成上海时区的自然日序号再相减，不做毫秒除法。
 */
export function lagDaysFrom(latestDate: string, today: Date = new Date()): number {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(latestDate).trim())
  if (!m) return -1
  const dataDay = Math.floor(Date.UTC(Number(m[1]), Number(m[2]) - 1, Number(m[3])) / 86400000)
  const todayDay = Math.floor((today.getTime() + 8 * 3600_000) / 86400000)
  return todayDay - dataDay
}
