import { describe, expect, it } from 'vitest'
import { fmtDate, fmtSize } from './format'

describe('fmtSize', () => {
  it('边界值', () => {
    expect(fmtSize(0)).toBe('0 B')
    expect(fmtSize(1023)).toBe('1023 B')
    expect(fmtSize(1024)).toBe('1.0 KB')
    expect(fmtSize(1024 * 1024)).toBe('1.0 MB')
    expect(fmtSize(1024 * 1024 * 1024)).toBe('1.0 GB')
    expect(fmtSize(1024 * 1024 * 1024 * 1024)).toBe('1.0 TB')
  })
  it('非整倍舍入一位', () => {
    expect(fmtSize(1500 * 1024)).toBe('1.5 MB')
  })
})

describe('fmtDate', () => {
  it('空值返回空串', () => {
    expect(fmtDate('')).toBe('')
  })
  it('合法时间可解析', () => {
    expect(fmtDate('2026-09-01T00:00:00.000Z')).not.toBe('')
  })
})
