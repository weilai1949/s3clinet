import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// happy-dom 默认不提供 localStorage/sessionStorage；用简单 Map 替身补齐。
class MemStorage implements Storage {
  private m = new Map<string, string>()
  get length(): number { return this.m.size }
  clear() { this.m.clear() }
  key(i: number): string | null { return [...this.m.keys()][i] ?? null }
  getItem(k: string): string | null { return this.m.get(k) ?? null }
  setItem(k: string, v: string) { this.m.set(k, String(v)) }
  removeItem(k: string) { this.m.delete(k) }
}

let memLocal: MemStorage
let memSession: MemStorage

beforeEach(() => {
  memLocal = new MemStorage()
  memSession = new MemStorage()
  Object.defineProperty(globalThis, 'localStorage', { value: memLocal, configurable: true, writable: true })
  Object.defineProperty(globalThis, 'sessionStorage', { value: memSession, configurable: true, writable: true })
  // 动态加载：每个用例独立的初始状态
  vi.resetModules()
})

afterEach(() => {
  // no-op
})

describe('api token 存储', () => {
  it('默认写入 sessionStorage，不写 localStorage', async () => {
    const { api } = await import('./api')
    api.token = 'session-token-1234567890'
    expect(memSession.getItem('s3c.token')).toBe('session-token-1234567890')
    expect(memLocal.getItem('s3c.token')).toBeNull()
    expect(api.token).toBe('session-token-1234567890')
  })

  it('setTokenPersistent(true) 同时写 sessionStorage 与 localStorage', async () => {
    const { api } = await import('./api')
    api.token = 'persist-token-1234567890'
    api.setTokenPersistent(true)
    expect(memSession.getItem('s3c.token')).toBe('persist-token-1234567890')
    expect(memLocal.getItem('s3c.token')).toBe('persist-token-1234567890')
    expect(api.isTokenPersistent).toBe(true)
  })

  it('setTokenPersistent(false) 把 token 从 localStorage 移走，仅留 sessionStorage', async () => {
    const { api } = await import('./api')
    api.setTokenPersistent(true)
    api.token = 't1'
    expect(memLocal.getItem('s3c.token')).toBe('t1')
    api.setTokenPersistent(false)
    expect(memLocal.getItem('s3c.token')).toBeNull()
    expect(memLocal.getItem('s3c_token_persistent')).toBeNull()
    expect(memSession.getItem('s3c.token')).toBe('t1')
  })

  it('旧版本遗留的 localStorage token 首次读取时自动迁移到 sessionStorage', async () => {
    memLocal.setItem('s3c.token', 'legacy-token-1234567890')
    const { api } = await import('./api')
    expect(api.token).toBe('legacy-token-1234567890')
    // 迁移后 localStorage 应清空
    expect(memLocal.getItem('s3c.token')).toBeNull()
    expect(memSession.getItem('s3c.token')).toBe('legacy-token-1234567890')
  })

  it('清空 token 字符串时 sessionStorage 与 localStorage 都被清空', async () => {
    const { api } = await import('./api')
    api.setTokenPersistent(true)
    api.token = 't2'
    expect(memSession.getItem('s3c.token')).toBe('t2')
    expect(memLocal.getItem('s3c.token')).toBe('t2')
    api.token = ''
    expect(memSession.getItem('s3c.token')).toBeNull()
    // 非 persistent 模式下清空也会移除 localStorage 残留
    expect(memLocal.getItem('s3c.token')).toBeNull()
  })
})
