import { describe, expect, it } from 'vitest'
import { MULTIPART_THRESHOLD, calcMultipartParts, shouldUseMultipart, withRetries } from './upload'

describe('upload multipart helpers', () => {
  it('calcMultipartParts rounds up', () => {
    expect(calcMultipartParts(0)).toBe(0)
    expect(calcMultipartParts(1)).toBe(1)
    expect(calcMultipartParts(10 * 1024 * 1024)).toBe(1)
    expect(calcMultipartParts(10 * 1024 * 1024 + 1)).toBe(2)
  })

  it('shouldUseMultipart at 100MB threshold', () => {
    expect(shouldUseMultipart(MULTIPART_THRESHOLD - 1)).toBe(false)
    expect(shouldUseMultipart(MULTIPART_THRESHOLD)).toBe(true)
  })
})

describe('withRetries', () => {
  it('retries on failure then succeeds', async () => {
    let n = 0
    const result = await withRetries(
      async () => {
        n++
        if (n < 3) throw new Error('transient')
        return 'ok'
      },
      [1, 1, 1],
    )
    expect(result).toBe('ok')
    expect(n).toBe(3)
  })

  it('does not retry AbortError', async () => {
    let n = 0
    await expect(
      withRetries(async () => {
        n++
        throw new DOMException('Aborted', 'AbortError')
      }, [1, 1]),
    ).rejects.toMatchObject({ name: 'AbortError' })
    expect(n).toBe(1)
  })

  it('exhausts retries and throws last error', async () => {
    let n = 0
    await expect(
      withRetries(async () => {
        n++
        throw new Error(`fail-${n}`)
      }, [1, 1]),
    ).rejects.toThrow('fail-3')
    expect(n).toBe(3)
  })

  it('respects abort signal between attempts', async () => {
    const ctrl = new AbortController()
    let n = 0
    const p = withRetries(
      async () => {
        n++
        if (n === 1) {
          ctrl.abort()
          throw new Error('first')
        }
        return 'never'
      },
      [5, 5],
      ctrl.signal,
    )
    await expect(p).rejects.toMatchObject({ name: 'AbortError' })
    expect(n).toBe(1)
  })
})
