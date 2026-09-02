/** 版本内容差异比对：先裁去公共前缀/后缀，剩余部分视为变更（基础但可读）。 */

export interface DiffLine {
  type: 'added' | 'removed' | 'same'
  text: string
}

export function lineDiff(a: string, b: string): DiffLine[] {
  const al = a.split('\n')
  const bl = b.split('\n')
  let start = 0
  while (start < al.length && start < bl.length && al[start] === bl[start]) start++
  let endA = al.length
  let endB = bl.length
  while (endA > start && endB > start && al[endA - 1] === bl[endB - 1]) {
    endA--
    endB--
  }
  const out: DiffLine[] = []
  for (let i = 0; i < start; i++) out.push({ type: 'same', text: al[i] })
  for (let i = start; i < endA; i++) out.push({ type: 'removed', text: al[i] })
  for (let i = start; i < endB; i++) out.push({ type: 'added', text: bl[i] })
  for (let i = endA; i < al.length; i++) out.push({ type: 'same', text: al[i] })
  return out
}

/** 判断是否为可读文本（含 NUL 字节视为二进制）。 */
export function looksBinary(s: string): boolean {
  return s.indexOf('\u0000') >= 0
}
