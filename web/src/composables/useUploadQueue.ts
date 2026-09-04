import { onBeforeUnmount, onDeactivated, ref, toRaw } from 'vue'
import type { Ref } from 'vue'
import { toErrorMessage } from '../errors'
import { uploadObject } from '../upload'

/**
 * 直传并发数（两套上传入口统一取原对象面板值 2）：
 * 过低慢，过高浏览器/对端都吃力；如需调整只改这一个常量。
 */
export const UPLOAD_CONCURRENCY = 2

/** 上传条目状态：cancelled 是用户主动中止的终态——不再回 pending 重新组批上传。 */
export type UploadItemStatus = 'pending' | 'signing' | 'uploading' | 'done' | 'err' | 'cancelled'

/** 统一的上传队列条目结构（UploadPanel 与对象面板共享）。 */
export interface UploadQueueItem {
  id?: number
  file: File
  key: string
  /** 入队时所属桶（防止上传途中切换桶导致串桶）。 */
  bucket?: string
  pct: number
  status: UploadItemStatus
  err?: string
}

export interface UploadQueueOptions {
  /** 单条上传目标（账号/桶/key），run 时逐条求值。 */
  target: (it: UploadQueueItem) => { accId: string; bucket?: string; key: string }
  /** 选中本轮参与上传的条目；缺省取全部 pending。 */
  selectBatch?: (items: UploadQueueItem[]) => UploadQueueItem[]
  /** 单批传完后继续吸收新入队的 pending（对象面板边传边加）；false = 单批快照（上传面板）。默认 true。 */
  drain?: boolean
  /** 单条中止后的去向：requeue=回 pending（可再次上传，上传面板）；cancel=先标 cancelled 终态再 abort（对象面板，默认）。 */
  onAbort?: 'requeue' | 'cancel'
  /** 单条开始上传前钩子（如按当前前缀重算 key）。 */
  onItemStart?: (it: UploadQueueItem) => void
  /** 进度回调（缺省写入 item.pct）。 */
  onProgress?: (it: UploadQueueItem, pct: number) => void
}

export interface UploadQueue {
  items: Ref<UploadQueueItem[]>
  running: Ref<boolean>
  /** 入队：跳过目录占位（size=0 且无类型）；make 供调用方补充 key/bucket 等字段。 */
  enqueue: (files: FileList | null, make?: (file: File) => Partial<UploadQueueItem>) => void
  /** 运行共享状态机；返回本轮实际选中的条目（按选中顺序）。 */
  run: () => Promise<UploadQueueItem[]>
  abortItem: (it: UploadQueueItem) => void
  abortAll: () => void
}

/**
 * 上传队列组合式：统一条目结构、并发控制、状态机与 abort 支持，
 * 供 UploadPanel（独立上传页）与 useObjectActions（对象面板内嵌上传）共用。
 * 两边只保留 UI 层差异（分组/展示/触发方式/完成后的提示与刷新）。
 */
export function useUploadQueue(options: UploadQueueOptions): UploadQueue {
  const items = ref<UploadQueueItem[]>([])
  const running = ref(false)
  /** 在途条目 → AbortController（toRaw 作键，避免响应式代理污染键）。 */
  const aborts = new Map<UploadQueueItem, AbortController>()
  let seq = 0

  const selectBatch = options.selectBatch ?? ((all: UploadQueueItem[]) => all.filter((it) => it.status === 'pending'))
  const writePct = options.onProgress ?? ((it: UploadQueueItem, pct: number) => (it.pct = pct))

  function enqueue(files: FileList | null, make?: (file: File) => Partial<UploadQueueItem>) {
    if (!files || !files.length) return
    for (const f of Array.from(files)) {
      if (!f.size && f.type === '') continue // 跳过目录占位
      items.value.push({ id: ++seq, file: f, key: '', pct: 0, status: 'pending', ...make?.(f) })
    }
  }

  /** 单条中止：cancel 策略先标 cancelled 终态再 abort——worker 的 catch 不会把它送回 pending 重新组批。 */
  function abortItem(it: UploadQueueItem) {
    if (options.onAbort !== 'requeue') {
      if (it.status === 'pending' || it.status === 'signing' || it.status === 'uploading') it.status = 'cancelled'
    }
    const raw = toRaw(it)
    aborts.get(raw)?.abort()
    aborts.delete(raw)
  }

  /** 全量中止（切后台 / 卸载 / 清空队列）：只作用于在途条目。 */
  function abortAll() {
    for (const it of [...aborts.keys()]) abortItem(it)
  }

  async function run(): Promise<UploadQueueItem[]> {
    if (running.value) return []
    running.value = true
    const processed: UploadQueueItem[] = []
    try {
      // 循环直到队列中没有待上传项（drain 模式下期间可继续追加文件）
      for (;;) {
        const batch = selectBatch(items.value)
        if (!batch.length) break
        processed.push(...batch)
        let i = 0
        const worker = async () => {
          while (i < batch.length) {
            const it = batch[i++]
            // 批内已被取消的条目直接跳过：不发起请求、不占用并发位
            if (it.status === 'cancelled') continue
            const ctrl = new AbortController()
            aborts.set(toRaw(it), ctrl)
            try {
              options.onItemStart?.(it)
              it.status = 'signing'
              it.err = undefined
              it.status = 'uploading'
              await uploadObject(it.file, options.target(it), (p) => writePct(it, p), ctrl.signal)
              it.status = 'done'
              it.pct = 100
            } catch (err) {
              // status 可能已被 abortItem 外部置为 cancelled（TS 无法看到跨函数可变状态）
              const st = it.status as UploadItemStatus
              if (st === 'cancelled') {
                // cancelled 是终态：保持，外层组批与批内 worker 都会跳过
              } else if (err instanceof DOMException && err.name === 'AbortError') {
                it.status = 'pending'
                it.pct = 0
              } else {
                it.status = 'err'
                it.err = toErrorMessage(err)
              }
            } finally {
              aborts.delete(toRaw(it))
            }
          }
        }
        await Promise.all(Array.from({ length: Math.min(UPLOAD_CONCURRENCY, batch.length) }, worker))
        if (options.drain === false) break
      }
      return processed
    } finally {
      running.value = false
    }
  }

  // 面板卸载 / KeepAlive 切走时中止在途上传（对象面板：cancelled 终态；上传面板：回 pending）
  onDeactivated(abortAll)
  onBeforeUnmount(abortAll)

  return { items, running, enqueue, run, abortItem, abortAll }
}
