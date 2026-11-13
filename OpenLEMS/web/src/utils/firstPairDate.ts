/**
 * 首个可配对交易日 = 首条留痕生成日 + 3。
 *
 * 留痕的 generated_on = 当天、target_time = 次日（application/internal/logic/.../daily_record.go），
 * 而真值（官方实际出清价）是 T+2 才发布 ⇒ 目标日 D+1 的实际值在 D+3 才拿得到。
 * 所以「启用日 + 3」之前复盘页一定是空的，这不是故障。
 *
 * 拿不到或格式不对返回空串：模板里回落成「—」，绝不印出 NaN-NaN-NaN。
 */
export function firstPairDate(generatedOn?: string | null): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(String(generatedOn ?? '').trim())
  if (!m) return ''
  const d = new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]))
  if (Number.isNaN(d.getTime())) return ''
  d.setDate(d.getDate() + 3)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
