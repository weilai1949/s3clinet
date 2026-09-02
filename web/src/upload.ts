import { s3api, directUpload } from './api'
import { t, tf } from './i18n'

/** 上传目标（账号 + 桶 + key）。 */
export interface UploadTarget {
  accId: string
  bucket?: string
  key: string
}

/** 超过该大小的文件自动走 S3 分段上传（单 PUT 上限 5GB，分段对超大文件更稳、可并行）。 */
export const MULTIPART_THRESHOLD = 100 * 1024 * 1024 // 100MB

// S3 分段：除最后一段外每段最小 5MB；这里取 10MB 平衡并发数与段数量。
const PART_SIZE = 10 * 1024 * 1024
const PART_CONCURRENCY = 4

/** 分段 PUT 重试退避（ms）：约 500 → 1500 → 3500。 */
export const PART_RETRY_DELAYS_MS = [500, 1500, 3500] as const

function isAbortError(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'AbortError'
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('Aborted', 'AbortError'))
      return
    }
    const t = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    const onAbort = () => {
      clearTimeout(t)
      reject(new DOMException('Aborted', 'AbortError'))
    }
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

/**
 * 带指数退避的重试：不重试 AbortError；最多尝试 delays.length + 1 次。
 * 导出供单测与其它弱网路径复用。
 */
export async function withRetries<T>(
  fn: () => Promise<T>,
  delaysMs: readonly number[] = PART_RETRY_DELAYS_MS,
  signal?: AbortSignal,
): Promise<T> {
  let lastErr: unknown
  const attempts = delaysMs.length + 1
  for (let i = 0; i < attempts; i++) {
    if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
    try {
      return await fn()
    } catch (e) {
      lastErr = e
      if (isAbortError(e)) throw e
      if (i >= delaysMs.length) break
      await sleep(delaysMs[i], signal)
    }
  }
  throw lastErr
}

/** 计算分段上传的段数（供测试与 UI 预估）。 */
export function calcMultipartParts(fileSize: number, partSize = PART_SIZE): number {
  if (fileSize <= 0) return 0
  return Math.ceil(fileSize / partSize)
}

/** 是否应走分段上传。 */
export function shouldUseMultipart(fileSize: number): boolean {
  return fileSize >= MULTIPART_THRESHOLD
}

/**
 * 上传单个文件：小文件走单次 PUT（presign），大文件自动分段上传。
 * onProgress 回调整数百分比（0-100）。signal 触发时中止 XHR 并尽力 abort 分段会话。
 */
export async function uploadObject(
  file: File,
  target: UploadTarget,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
  if (file.size < MULTIPART_THRESHOLD) {
    const presign = await s3api.presign(target.accId, {
      method: 'put',
      key: target.key,
      bucket: target.bucket,
      expiresIn: 3600,
    })
    if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
    return directUpload(presign.url, file, onProgress, signal)
  }
  return multipartUpload(file, target, onProgress, signal)
}

/** PUT 一个分段并返回其 ETag（complete 需要）。 */
function putPartReturnEtag(
  url: string,
  blob: Blob,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<string> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    let onAbort: (() => void) | undefined
    if (signal) {
      onAbort = () => {
        xhr.abort()
        reject(new DOMException('Aborted', 'AbortError'))
      }
      if (signal.aborted) {
        onAbort()
        return
      }
      signal.addEventListener('abort', onAbort, { once: true })
    }
    const cleanup = () => {
      if (signal && onAbort) signal.removeEventListener('abort', onAbort)
    }
    xhr.open('PUT', url)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100))
    }
    xhr.onload = () => {
      cleanup()
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr.getResponseHeader('ETag') ?? '')
      } else {
        reject(new Error(tf('upload.partHttpError', { status: xhr.status })))
      }
    }
    xhr.onerror = () => {
      cleanup()
      reject(new Error(t('upload.partNetworkError')))
    }
    xhr.onabort = () => {
      cleanup()
      reject(new DOMException('Aborted', 'AbortError'))
    }
    xhr.send(blob)
  })
}

async function multipartUpload(
  file: File,
  target: UploadTarget,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<void> {
  const init = await s3api.multipartInit(target.accId, {
    bucket: target.bucket,
    key: target.key,
    contentType: file.type || undefined,
  })
  if (signal?.aborted) {
    s3api.multipartAbort(target.accId, { bucket: target.bucket, key: target.key, uploadId: init.uploadId }).catch(() => {})
    throw new DOMException('Aborted', 'AbortError')
  }
  const uploadId = init.uploadId
  const totalParts = calcMultipartParts(file.size)
  const parts: { partNumber: number; etag: string }[] = new Array(totalParts)
  let doneBytes = 0
  let aborted = false
  const abort = () => {
    if (aborted) return
    aborted = true
    s3api.multipartAbort(target.accId, { bucket: target.bucket, key: target.key, uploadId }).catch(() => {})
  }
  signal?.addEventListener('abort', abort, { once: true })

  try {
    let idx = 0
    const worker = async () => {
      while (idx < totalParts) {
        if (signal?.aborted || aborted) throw new DOMException('Aborted', 'AbortError')
        const n = idx++ + 1
        const start = (n - 1) * PART_SIZE
        const end = Math.min(start + PART_SIZE, file.size)
        const blob = file.slice(start, end)
        const etag = await withRetries(async () => {
          const presign = await s3api.multipartPart(target.accId, {
            bucket: target.bucket,
            key: target.key,
            uploadId,
            partNumber: n,
            expiresIn: 3600,
          })
          if (signal?.aborted || aborted) throw new DOMException('Aborted', 'AbortError')
          return putPartReturnEtag(presign.url, blob, (p) => {
            const partBytes = Math.round((p / 100) * blob.size)
            const overall = Math.min(100, Math.round(((doneBytes + partBytes) / file.size) * 100))
            onProgress?.(overall)
          }, signal)
        }, PART_RETRY_DELAYS_MS, signal)
        if (!etag) {
          throw new Error(t('upload.partNoEtag'))
        }
        parts[n - 1] = { partNumber: n, etag }
        doneBytes += blob.size
        onProgress?.(Math.min(100, Math.round((doneBytes / file.size) * 100)))
      }
    }
    await Promise.all(Array.from({ length: Math.min(PART_CONCURRENCY, totalParts) }, worker))
    if (signal?.aborted || aborted) throw new DOMException('Aborted', 'AbortError')
    await s3api.multipartComplete(target.accId, {
      bucket: target.bucket,
      key: target.key,
      uploadId,
      parts,
    })
    onProgress?.(100)
  } catch (e) {
    if (!isAbortError(e)) abort()
    throw e
  } finally {
    signal?.removeEventListener('abort', abort)
  }
}
