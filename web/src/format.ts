/** 共享格式化工具。 */

/** 把字节数格式化为人类可读大小（B/KB/MB/GB/TB）。 */
export function fmtSize(n: number): string {
  if (n < 1024) return n + ' B'
  const u = ['KB', 'MB', 'GB', 'TB']
  let i = -1
  let v = n
  do { v /= 1024; i++ } while (v >= 1024 && i < u.length - 1)
  return v.toFixed(1) + ' ' + u[i]
}

/** 把 ISO 时间字符串格式化为本地可读时间；空值返回空串。 */
export function fmtDate(s: string): string {
  if (!s) return ''
  return new Date(s).toLocaleString()
}
