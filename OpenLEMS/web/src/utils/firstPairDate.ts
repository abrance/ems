/**
 * 首个可配对交易日 = 首条留痕生成日 + 该市场的真值发布滞后。
 *
 * 留痕的 generated_on = 当天、target_time = 次日（application/internal/logic/forecast/daily_record.go:24-27），
 * 所以目标日 D+1 的实际值哪天才拿得到，取决于**哪个市场**的真值：
 * 源站口径见 tools/gdpm/gdpm_sqlite.py 的 report_of()：「实时是 T+2 发布；日前与日报同日」。
 *
 *   - ahead（日前，默认）：T+0 ⇒ 目标日 D+1 当天就能配对，偏移 +1
 *   - real（实时）：T+2      ⇒ 目标日 D+1 的实际值要到 D+3，偏移 +3
 *
 * 这里原先写死 +3，理由写的是「真值 T+2 才发布」—— 那是实时市场的口径，
 * 套在默认的日前腿上把可达日说慢了两天。
 *
 * 缺省/未知 market 按 ahead 处理：页面默认腿就是日前，未知值跟着默认腿走，
 * 宁可乐观提示，也不要拿实时口径把一个未知市场说慢两天。
 *
 * 注意这组偏移只描述**源站发布节奏**。实际能不能配上对，还要看 gd_spot_metric 多久更新一次：
 * tools/gdpm/README.md 的「每周更新（手动执行一次）」写明采集是用户手动跑的、没有定时任务，
 * 所以配对点目前最多每周前进一次。这句是给改这里的人看的，不进用户可见 UI。
 *
 * 拿不到或格式不对返回空串：模板里回落成「—」，绝不印出 NaN-NaN-NaN。
 */

/** 真值发布滞后（天）：实时比日前多等两天。两个市场之外的取值一律按日前。 */
function lagDays(market?: string | null): number {
  return String(market ?? '').trim() === 'real' ? 3 : 1
}

export function firstPairDate(generatedOn?: string | null, market?: string | null): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(generatedOn ?? '').trim())
  if (!m) return ''
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  if (Number.isNaN(d.getTime())) return ''
  d.setDate(d.getDate() + lagDays(market))
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
