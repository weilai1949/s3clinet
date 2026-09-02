import { describe, expect, it } from 'vitest'
import { storageClassLabel, STORAGE_CLASS_VALUES, storageClassOptions } from './storageClass'

describe('storageClassLabel', () => {
  it('已知类型返回本地化标签', () => {
    expect(storageClassLabel('STANDARD')).toContain('STANDARD')
    expect(storageClassLabel('GLACIER')).toContain('GLACIER')
  })
  it('未知类型原样返回', () => {
    expect(storageClassLabel('WEIRD')).toBe('WEIRD')
  })
  it('枚举非空且值唯一', () => {
    expect(STORAGE_CLASS_VALUES.length).toBeGreaterThanOrEqual(8)
    expect(new Set(STORAGE_CLASS_VALUES).size).toBe(STORAGE_CLASS_VALUES.length)
    expect(storageClassOptions().map((s) => s.value)).toEqual([...STORAGE_CLASS_VALUES])
  })
})
