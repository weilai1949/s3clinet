import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createProgressToast, toasts } from './store'

describe('createProgressToast（SSE 进度节流）', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    vi.useRealTimers()
    toasts.splice(0, toasts.length)
  })

  it('500ms 窗口内只保留一条，不新增 toast', () => {
    const progress = createProgressToast()
    progress('1/10')
    progress('2/10')
    progress('3/10')
    expect(toasts).toHaveLength(1)
    expect(toasts[0].text).toBe('1/10')
  })

  it('窗口过后就地更新同一条 toast，而不是新发一条', () => {
    const progress = createProgressToast()
    progress('1/10')
    const firstId = toasts[0].id
    vi.advanceTimersByTime(500)
    progress('2/10')
    expect(toasts).toHaveLength(1)
    expect(toasts[0].id).toBe(firstId)
    expect(toasts[0].text).toBe('2/10')
  })

  it('toast 已自动消失后再次触发会发出新的一条', () => {
    const progress = createProgressToast()
    progress('1/10')
    const firstId = toasts[0].id
    vi.advanceTimersByTime(4000) // 超过 3600ms 自动消失
    progress('2/10')
    expect(toasts).toHaveLength(1)
    expect(toasts[0].id).not.toBe(firstId)
    expect(toasts[0].text).toBe('2/10')
  })

  it('自定义节流窗口生效', () => {
    const progress = createProgressToast(100)
    progress('a')
    vi.advanceTimersByTime(100)
    progress('b')
    expect(toasts[0].text).toBe('b')
  })
})
