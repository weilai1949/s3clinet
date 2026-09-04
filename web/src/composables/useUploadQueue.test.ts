import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { UPLOAD_CONCURRENCY, useUploadQueue } from './useUploadQueue'
import type { UploadQueueItem, UploadQueueOptions } from './useUploadQueue'
import { uploadObject } from '../upload'

vi.mock('../upload', () => ({ uploadObject: vi.fn() }))

vi.mocked(uploadObject).mockImplementation((_file, _target, _p, signal) => {
  return new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('Aborted', 'AbortError'))
      return
    }
    signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')), { once: true })
    inFlight.push({ resolve, reject })
  })
})

/** 挂起中的上传，测试里手动推进完成。 */
const inFlight: { resolve: () => void; reject: (e: unknown) => void }[] = []

/** 挂一个宿主组件：setup 内调用组合式，把返回值存到外层变量供断言。 */
function mountQueue(options: Partial<UploadQueueOptions> = {}): ReturnType<typeof useUploadQueue> {
  let captured!: ReturnType<typeof useUploadQueue>
  const Host = defineComponent({
    setup() {
      captured = useUploadQueue({ target: (it) => ({ accId: 'a1', key: it.key }), ...options })
      return () => null
    },
  })
  mount(Host)
  return captured
}

function enqueue(q: ReturnType<typeof useUploadQueue>, names: string[]): UploadQueueItem[] {
  return names.map((name) => {
    const it: UploadQueueItem = { file: new File(['x'], name), key: name, pct: 0, status: 'pending' }
    q.items.value.push(it)
    return it
  })
}

describe('useUploadQueue 共享状态机', () => {
  beforeEach(() => {
    inFlight.length = 0
    vi.mocked(uploadObject).mockClear()
  })

  it('cancel 策略：取消等待中的条目直接跳过（不发起请求、不占用并发位），cancelled 保持终态', async () => {
    const q = mountQueue() // 默认 cancel 策略 + drain（对象面板语义）
    // 并发位之外再多排 2 条：1 条待取消、1 条正常等待
    const items = enqueue(q, Array.from({ length: UPLOAD_CONCURRENCY + 2 }, (_, i) => `f${i}.txt`))
    const run = q.run()
    await vi.waitFor(() => expect(inFlight.length).toBe(UPLOAD_CONCURRENCY))

    const cancelled = items[UPLOAD_CONCURRENCY]
    q.abortItem(cancelled)
    expect(cancelled.status).toBe('cancelled')

    inFlight.splice(0).forEach((f) => f.resolve())
    // 并发位空出后，批内下一条（未被取消）继续上传，被取消的那条被跳过
    await vi.waitFor(() => expect(inFlight.length).toBe(1))
    inFlight[0].resolve()
    await run

    expect(uploadObject).toHaveBeenCalledTimes(UPLOAD_CONCURRENCY + 1)
    expect(cancelled.status).toBe('cancelled')
    expect(q.items.value.filter((it) => it.status === 'done').length).toBe(UPLOAD_CONCURRENCY + 1)
  })

  it('requeue 策略（上传面板语义）：中止在途条目回 pending，可再次上传到 done', async () => {
    const q = mountQueue({ onAbort: 'requeue', drain: false })
    const [a] = enqueue(q, ['a.txt'])
    const run = q.run()
    await vi.waitFor(() => expect(inFlight.length).toBe(1))

    q.abortItem(a) // 仅中止，不标记 cancelled（状态仍 uploading，待 catch 回 pending）
    expect(a.status).toBe('uploading')
    inFlight.splice(0).forEach((f) => f.reject(new DOMException('Aborted', 'AbortError')))
    await run
    expect(a.status).toBe('pending')
    expect(a.pct).toBe(0)

    const run2 = q.run()
    await vi.waitFor(() => expect(inFlight.length).toBe(1))
    inFlight[0].resolve()
    const processed = await run2
    expect(a.status).toBe('done')
    expect(a.pct).toBe(100)
    expect(processed.map((it) => it.key)).toContain('a.txt')
  })
})
