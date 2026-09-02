import { describe, expect, it } from 'vitest'
import { toErrorMessage } from './errors'

describe('toErrorMessage', () => {
  it('Error 实例取 message', () => {
    expect(toErrorMessage(new Error('boom'))).toBe('boom')
  })
  it('字符串原样返回', () => {
    expect(toErrorMessage('oops')).toBe('oops')
  })
  it('对象序列化', () => {
    expect(toErrorMessage({ a: 1 })).toContain('"a"')
  })
  it('undefined/null 得到字符串', () => {
    expect(typeof toErrorMessage(undefined)).toBe('string')
    expect(typeof toErrorMessage(null)).toBe('string')
  })
})
