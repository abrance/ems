/**
 * CSV 生成与下载。
 *
 * 报表导出走**前端生成**：数据都在浏览器里（各页用的同一批接口），
 * 再绕一趟后端只会多一个接口、多一份权限，不会更快。
 * 代价是数据量受浏览器内存限制 —— 所以每个数据集都设了区间上限。
 */

/** Excel 打开 UTF-8 CSV 的必备前缀：没有它中文会变乱码。 */
export const CSV_BOM = '\ufeff'

/**
 * 按 RFC4180 转义一个字段：
 * 含逗号 / 引号 / 换行（以及前后空格）时整个字段加引号，内部引号写成两个。
 *
 * 不做这一步的后果很具体：指标名里一个逗号就会把一行数据错位到多列，
 * 而 Excel 不会报错，只会安静地给你一张错的表。
 */
export function csvField(value: unknown): string {
  if (value === null || value === undefined) return ''
  const s = String(value)
  if (!/[",\r\n]/.test(s) && s === s.trim()) return s
  return `"${s.replace(/"/g, '""')}"`
}

/** 表头 + 数据行。行尾用 CRLF（RFC4180），Excel 与文本工具都认。 */
export function buildCsv(columns: string[], rows: unknown[][]): string {
  const lines = [columns.map(csvField).join(',')]
  for (const row of rows) lines.push(row.map(csvField).join(','))
  return lines.join('\r\n')
}

/** 下载。文件名带上数据集与时间戳，避免同一天导出多次互相覆盖。 */
export function downloadCsv(filename: string, csv: string): void {
  if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return
  const blob = new Blob([CSV_BOM + csv], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

/** 文件名里只留安全字符，避免各系统对各异的表现 */
export function safeFilename(name: string): string {
  return name.replace(/[^\w\u4e00-\u9fa5.-]+/g, '_')
}

export function stamp(d = new Date()): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}`
}
