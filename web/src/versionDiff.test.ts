import { describe, expect, it } from 'vitest'
import { lineDiff, looksBinary } from './versionDiff'

describe('lineDiff', () => {
  it('完全一致：全部 same', () => {
    const d = lineDiff('a\nb\nc', 'a\nb\nc')
    expect(d).toEqual([
      { type: 'same', text: 'a' },
      { type: 'same', text: 'b' },
      { type: 'same', text: 'c' },
    ])
  })

  it('无相同行：整块变更', () => {
    const d = lineDiff('x\ny', 'p\nq')
    expect(d).toEqual([
      { type: 'removed', text: 'x' },
      { type: 'removed', text: 'y' },
      { type: 'added', text: 'p' },
      { type: 'added', text: 'q' },
    ])
  })

  it('公共后缀标记为 same（此前误标为 context）', () => {
    const d = lineDiff('a\nb\nc', 'a\nB\nc')
    const types = d.map((x) => x.type)
    // 首行 same、末行 same、中间 removed+added
    expect(types[0]).toBe('same')
    expect(types[types.length - 1]).toBe('same')
    expect(types).toContain('removed')
    expect(types).toContain('added')
    expect(d[d.length - 1]).toEqual({ type: 'same', text: 'c' })
  })

  it('尾部追加行：added', () => {
    const d = lineDiff('a', 'a\nnew')
    expect(d).toEqual([
      { type: 'same', text: 'a' },
      { type: 'added', text: 'new' },
    ])
  })

  it('删除尾行：removed', () => {
    const d = lineDiff('a\ngone', 'a')
    expect(d).toEqual([{ type: 'same', text: 'a' }, { type: 'removed', text: 'gone' }])
  })
})

describe('looksBinary', () => {
  it('NUL 字节判为二进制', () => {
    expect(looksBinary('ab\u0000cd')).toBe(true)
    expect(looksBinary('plain text')).toBe(false)
  })
})
