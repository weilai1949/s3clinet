import { describe, expect, it } from 'vitest'
import { tabFromHash } from './router'

describe('tab hash router', () => {
  it('parses valid hash tabs', () => {
    expect(tabFromHash('#/objects')).toBe('objects')
    expect(tabFromHash('#objects')).toBe('objects')
    expect(tabFromHash('#/trash?x=1')).toBe('trash')
  })

  it('returns null for invalid hash', () => {
    expect(tabFromHash('')).toBe(null)
    expect(tabFromHash('#/unknown')).toBe(null)
  })
})
