/** 统一的错误文本提取：unknown → 可展示字符串。 */
export function toErrorMessage(e: unknown): string {
  if (e instanceof Error) return e.message
  if (typeof e === 'string') return e
  if (typeof e === 'object' && e !== null) {
    try {
      return JSON.stringify(e)
    } catch {
      /* fallthrough */
    }
  }
  return String(e)
}
