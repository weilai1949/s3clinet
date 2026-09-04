import { computed, defineComponent, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useObjectActions, type ObjectBrowserCtx } from './useObjectActions'
import { uploadObject } from '../upload'
import type { UploadItem } from '../components/UploadQueue.vue'
import type { Account } from '../types'

vi.mock('../api', () => ({
  s3api: {
    deleteObjects: vi.fn(),
    presign: vi.fn(),
    headObject: vi.fn(),
    renameObject: vi.fn(),
    mkdirObject: vi.fn(),
    downloadZipToDisk: vi.fn(),
    deletePrefixAsync: vi.fn(),
    copyPrefixAsync: vi.fn(),
    copyFilesAsync: vi.fn(),
  },
  api: { base: '', isTauri: false, getActiveServer: () => null, token: '' },
  subscribeMigrateEvents: vi.fn(),
}))
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

const acc: Account = {
  id: 'a1',
  name: 'A',
  endpoint: '',
  region: '',
  accessKey: '',
  secretKey: '',
  bucket: 'b1',
  pathStyle: true,
  useSSL: true,
}

function makeCtx(): ObjectBrowserCtx {
  return {
    account: computed(() => acc),
    currentBucket: ref('b1'),
    selected: ref(new Set<string>()),
    fileObjects: computed(() => []),
    prefix: ref(''),
    opsBusy: ref(false),
    error: ref(''),
    ctxMenu: ref(null),
    load: vi.fn(async () => {}),
    enterPrefix: vi.fn(),
    closeCtx: vi.fn(),
  }
}

/** 挂一个宿主组件：setup 内调用组合式，把返回值存到外层变量供断言。 */
function mountActions(): ReturnType<typeof useObjectActions> {
  let captured!: ReturnType<typeof useObjectActions>
  const Host = defineComponent({
    setup() {
      captured = useObjectActions(makeCtx())
      return () => null
    },
  })
  mount(Host)
  return captured
}

function enqueue(actions: ReturnType<typeof useObjectActions>, name: string): UploadItem {
  const it: UploadItem = { file: new File(['x'], name), key: name, bucket: 'b1', pct: 0, status: 'pending' }
  actions.uploadQueue.value.push(it)
  return it
}

describe('上传取消状态机（cancelled 为终态）', () => {
  beforeEach(() => {
    inFlight.length = 0
    vi.mocked(uploadObject).mockClear()
  })

  it('中止上传中的条目：置 cancelled，不回 pending，也不重新组批上传', async () => {
    const actions = mountActions()
    const a = enqueue(actions, 'a.txt')
    const b = enqueue(actions, 'b.txt')
    const run = actions.runUpload()
    await vi.waitFor(() => expect(inFlight.length).toBe(2))

    actions.abortUploadItem(a)
    expect(a.status).toBe('cancelled')
    inFlight[1].resolve()
    await run

    // cancelled 终态：不被 catch 送回 pending，也不会被外层 for(;;) 重新组批
    expect(a.status).toBe('cancelled')
    expect(uploadObject).toHaveBeenCalledTimes(2)
    expect(uploadObject).toHaveBeenNthCalledWith(1, a.file, expect.objectContaining({ key: 'a.txt' }), expect.anything(), expect.anything())
    expect(b.status).toBe('done')
  })

  it('取消等待中的条目：跳过上传，不占用 worker', async () => {
    const actions = mountActions()
    enqueue(actions, 'a.txt')
    enqueue(actions, 'b.txt')
    const c = enqueue(actions, 'c.txt')
    const run = actions.runUpload()

    await vi.waitFor(() => expect(inFlight.length).toBe(2))
    actions.abortUploadItem(c)
    expect(c.status).toBe('cancelled')

    inFlight[0].resolve()
    inFlight[1].resolve()
    await run

    // 第三条从未发起上传（组批跳过 cancelled）
    expect(uploadObject).toHaveBeenCalledTimes(2)
    expect(c.status).toBe('cancelled')
  })
})
