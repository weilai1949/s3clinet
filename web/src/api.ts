import type {
  Account,
  AccountInput,
  BucketInfo,
  CorsRule,
  LifecycleRule,
  ListObjectsResponse,
  ListVersionsResponse,
  MigrationResult,
  ObjectMeta,
  PresignResponse,
  ServerProfile,
  BucketItem,
} from './types'
import { t } from './i18n'

// 安全权衡说明：本存储方案把各后端地址与 Bearer Token 持久化到 localStorage，
// 这是为「多服务端 + 记住配置」的便利性做的取舍——同源脚本可读取。若部署环境启用 S3C_TOKEN，
// 且存在对象内容可执行（inline 代理已按 MIME 白名单收紧），建议改用 sessionStorage 或内存 +
// 会话期重新输入；详见 README「安全说明」。
//
// Token 读写策略（不破坏既有用户）：
// 1) 若 localStorage `s3c_token_ephemeral` === '1'，读写 sessionStorage（关标签即清，适合共享/生产主机）；
// 2) 否则优先读 sessionStorage（若已有值），再回落 localStorage。
const LS_SERVERS = 's3c.servers'
const LS_ACTIVE = 's3c.activeServerId'
const LS_BASE = 's3c.apiBase'
const LS_TOKEN = 's3c.token'
const LS_TOKEN_EPHEMERAL = 's3c_token_ephemeral'

function tokenEphemeral(): boolean {
  try {
    return localStorage.getItem(LS_TOKEN_EPHEMERAL) === '1'
  } catch {
    return false
  }
}

function readToken(): string {
  try {
    if (tokenEphemeral()) return sessionStorage.getItem(LS_TOKEN) ?? ''
    const fromSession = sessionStorage.getItem(LS_TOKEN)
    if (fromSession) return fromSession
  } catch {
    /* ignore */
  }
  try {
    return localStorage.getItem(LS_TOKEN) ?? ''
  } catch {
    return ''
  }
}

function writeToken(v: string) {
  const trimmed = v.trim()
  try {
    if (tokenEphemeral()) {
      if (trimmed) sessionStorage.setItem(LS_TOKEN, trimmed)
      else sessionStorage.removeItem(LS_TOKEN)
      localStorage.removeItem(LS_TOKEN) // 避免残留持久 token
      return
    }
  } catch {
    /* fall through to localStorage */
  }
  try {
    if (trimmed) localStorage.setItem(LS_TOKEN, trimmed)
    else localStorage.removeItem(LS_TOKEN)
  } catch {
    /* ignore */
  }
}

function isTauri(): boolean {
  const w = window as any
  return (
    !!w.__TAURI_INTERNALS__ ||
    !!w.__TAURI__ ||
    navigator.userAgent.toLowerCase().includes('tauri') ||
    /^(?:tauri|app\.tauri)(?:\.localhost)?$/.test(location.hostname)
  )
}

function defaultBase(): string {
  return isTauri() ? 'http://127.0.0.1:8080' : ''
}

function newId(): string {
  return crypto.randomUUID?.() ?? `s-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
}

function readServers(): ServerProfile[] {
  try {
    const raw = localStorage.getItem(LS_SERVERS)
    if (raw) {
      const list = JSON.parse(raw) as ServerProfile[]
      if (Array.isArray(list)) return list
    }
  } catch {
    /* ignore */
  }
  // 首次：默认一条
  const p: ServerProfile = {
    id: newId(),
    name: isTauri() ? t('server.localBackend') : t('server.sameOriginDefault'),
    base: defaultBase(),
    token: '',
  }
  writeServers([p])
  localStorage.setItem(LS_ACTIVE, p.id)
  applyProfile(p)
  return [p]
}

function writeServers(list: ServerProfile[]) {
  localStorage.setItem(LS_SERVERS, JSON.stringify(list))
}

function applyProfile(p: ServerProfile) {
  localStorage.setItem(LS_BASE, (p.base || '').replace(/\/+$/, ''))
  writeToken(p.token || '')
  localStorage.setItem(LS_ACTIVE, p.id)
}

export const api = {
  get base(): string {
    const stored = localStorage.getItem(LS_BASE)
    return (stored !== null ? stored : defaultBase()).replace(/\/+$/, '')
  },
  set base(v: string) {
    localStorage.setItem(LS_BASE, v.replace(/\/+$/, ''))
  },
  get token(): string {
    return readToken()
  },
  set token(v: string) {
    writeToken(v)
  },
  /** Token 是否仅存 sessionStorage（关标签即失效）。 */
  get isTokenEphemeral(): boolean {
    return tokenEphemeral()
  },
  /**
   * 切换临时会话：启用时把当前 token 迁入 sessionStorage 并清除 localStorage 中的 token；
   * 关闭时迁回 localStorage 并清除 sessionStorage。
   */
  setTokenEphemeral(enabled: boolean) {
    const current = readToken()
    try {
      if (enabled) localStorage.setItem(LS_TOKEN_EPHEMERAL, '1')
      else localStorage.removeItem(LS_TOKEN_EPHEMERAL)
    } catch {
      /* ignore */
    }
    writeToken(current)
    if (!enabled) {
      try {
        sessionStorage.removeItem(LS_TOKEN)
      } catch {
        /* ignore */
      }
      // writeToken（非 ephemeral）只写 localStorage；再写一次确保迁回后可读
      writeToken(current)
    }
  },
  get isTauri(): boolean {
    return isTauri()
  },

  listServers(): ServerProfile[] {
    return readServers()
  },

  activeServerId(): string {
    const id = localStorage.getItem(LS_ACTIVE) ?? ''
    const list = readServers()
    if (list.find((s) => s.id === id)) return id
    return list[0]?.id ?? ''
  },

  getActiveServer(): ServerProfile | undefined {
    const id = this.activeServerId()
    return this.listServers().find((s) => s.id === id)
  },

  /** 设为当前生效并同步 base/token */
  selectServer(id: string): ServerProfile | undefined {
    const p = this.listServers().find((s) => s.id === id)
    if (!p) return undefined
    applyProfile(p)
    return p
  },

  upsertServer(input: { id?: string; name: string; base: string; token: string }): ServerProfile {
    const list = readServers()
    const base = (input.base || '').replace(/\/+$/, '')
    const token = (input.token || '').trim()
    const name = input.name.trim() || (base || t('server.sameOriginShort'))
    if (input.id) {
      const i = list.findIndex((s) => s.id === input.id)
      if (i >= 0) {
        list[i] = { ...list[i], name, base, token }
        writeServers(list)
        if (this.activeServerId() === input.id) applyProfile(list[i])
        return list[i]
      }
    }
    const p: ServerProfile = { id: newId(), name, base, token }
    list.push(p)
    writeServers(list)
    return p
  },

  deleteServer(id: string): void {
    let list = readServers().filter((s) => s.id !== id)
    if (!list.length) {
      list = [{ id: newId(), name: isTauri() ? t('server.localBackend') : t('server.sameOriginDefault'), base: defaultBase(), token: '' }]
    }
    writeServers(list)
    if (this.activeServerId() === id) {
      applyProfile(list[0])
    }
  },
}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = { ...((opts.headers as Record<string, string>) ?? {}) }
  if (opts.body != null) headers['Content-Type'] = 'application/json'
  if (api.token) headers['Authorization'] = `Bearer ${api.token}`
  const res = await fetch(api.base + path, { headers, ...opts })
  if (!res.ok) {
    let msg = res.statusText
    try {
      const j = await res.json()
      if (j?.error) msg = j.error
    } catch {
      /* ignore */
    }
    throw new Error(`${res.status} ${msg}`)
  }
  return res.json() as Promise<T>
}

/** 原始 Response（流式下载用）；失败时解析 JSON error。 */
async function requestResponse(path: string, opts: RequestInit = {}): Promise<Response> {
  const headers: Record<string, string> = { ...((opts.headers as Record<string, string>) ?? {}) }
  if (opts.body != null) headers['Content-Type'] = 'application/json'
  if (api.token) headers['Authorization'] = `Bearer ${api.token}`
  const res = await fetch(api.base + path, { headers, ...opts })
  if (!res.ok) {
    let msg = res.statusText
    try {
      const j = await res.json()
      if (j?.error) msg = j.error
    } catch {
      /* ignore */
    }
    throw new Error(`${res.status} ${msg}`)
  }
  return res
}

const ZIP_BLOB_MAX_BYTES = 500 * 1024 * 1024 // 500MB blob 兜底上限

type SaveFilePickerWindow = Window & {
  showSaveFilePicker?: (options?: {
    suggestedName?: string
    types?: { description: string; accept: Record<string, string[]> }[]
  }) => Promise<FileSystemFileHandle>
}

/** 将 ReadableStream 写入 File System Access API 的 writable。 */
async function streamBodyToFile(body: ReadableStream<Uint8Array>, handle: FileSystemFileHandle): Promise<void> {
  const writable = await handle.createWritable()
  try {
    await body.pipeTo(writable)
  } catch (e) {
    try {
      await writable.abort()
    } catch {
      /* ignore */
    }
    throw e
  }
}

/**
 * ZIP 下载：优先 File System Access API 流式落盘（避免整包进内存）；
 * 否则 blob 兜底，但 Content-Length > 500MB 或缺失且 keys>50 时拒绝。
 */
export async function downloadZipToDisk(
  id: string,
  body: { bucket?: string; keys: string[] },
  suggestedName?: string,
): Promise<void> {
  const keys = body.keys
  const path = `/api/accounts/${id}/download-zip`
  const res = await requestResponse(path, { method: 'POST', body: JSON.stringify(body) })
  const clHeader = res.headers.get('Content-Length')
  const contentLength = clHeader ? Number(clHeader) : NaN
  const filename =
    suggestedName ||
    `objects-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.zip`

  const w = window as SaveFilePickerWindow
  if (typeof w.showSaveFilePicker === 'function' && res.body) {
    try {
      const handle = await w.showSaveFilePicker({
        suggestedName: filename,
        types: [{ description: 'ZIP archive', accept: { 'application/zip': ['.zip'] } }],
      })
      await streamBodyToFile(res.body, handle)
      return
    } catch (e) {
      res.body.cancel().catch(() => {})
      throw e
    }
  }

  // blob 兜底：大包或未知大小且文件多时拒绝，避免 OOM
  if (
    (Number.isFinite(contentLength) && contentLength > ZIP_BLOB_MAX_BYTES) ||
    (!Number.isFinite(contentLength) && keys.length > 50)
  ) {
    res.body?.cancel().catch(() => {})
    throw new Error(t('api.zipTooLarge'))
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  try {
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  } finally {
    URL.revokeObjectURL(url)
  }
}

export const s3api = {

  listAccounts: () => request<{ accounts: Account[] }>('/api/accounts'),
  createAccount: (a: AccountInput) => request<Account>('/api/accounts', { method: 'POST', body: JSON.stringify(a) }),
  updateAccount: (id: string, a: Partial<AccountInput>) =>
    request<Account>(`/api/accounts/${id}`, { method: 'PUT', body: JSON.stringify(a) }),
  deleteAccount: (id: string) => request<{ deleted: string }>(`/api/accounts/${id}`, { method: 'DELETE' }),
  testAccount: (id: string) => request<{ ok: boolean; bucket: string; error?: string }>(`/api/accounts/${id}/test`, { method: 'POST' }),
  listBuckets: (id: string) => request<{ buckets: BucketItem[] }>(`/api/accounts/${id}/buckets`),
  createBucket: (id: string, body: { name: string; region?: string; acl?: string }) =>
    request<{ created: string; region: string; acl: string }>(`/api/accounts/${id}/bucket`, { method: 'POST', body: JSON.stringify(body) }),
  deleteBucket: (id: string, name: string) =>
    request<{ deleted: string }>(`/api/accounts/${id}/bucket?name=${encodeURIComponent(name)}`, { method: 'DELETE' }),
  previewBuckets: (a: AccountInput) =>
    request<{ buckets: BucketItem[] }>('/api/accounts/preview-buckets', { method: 'POST', body: JSON.stringify(a) }),

  listObjects: (id: string, q: Record<string, string>) => {
    const qs = new URLSearchParams(q).toString()
    return request<ListObjectsResponse>(`/api/accounts/${id}/objects?${qs}`)
  },
  headObject: (id: string, q: Record<string, string>) => {
    const qs = new URLSearchParams(q).toString()
    return request<ObjectMeta>(`/api/accounts/${id}/head?${qs}`)
  },
  mkdirObject: (id: string, body: { bucket?: string; key: string }) =>
    request<{ created: string; bucket: string }>(`/api/accounts/${id}/mkdir`, { method: 'POST', body: JSON.stringify(body) }),
  renameObject: (id: string, body: { bucket?: string; key: string; newKey: string; newBucket?: string }) =>
    request<{ renamed: string }>(`/api/accounts/${id}/rename`, { method: 'POST', body: JSON.stringify(body) }),
  copyObject: (id: string, body: { bucket?: string; key: string; newKey: string; newBucket?: string }) =>
    request<{ copied: string; bucket: string }>(`/api/accounts/${id}/copy-object`, { method: 'POST', body: JSON.stringify(body) }),
  copyFiles: (id: string, body: { bucket?: string; targetBucket?: string; targetPrefix?: string; keys: string[]; deleteSource?: boolean }) =>
    request<{ copied: number; failed: number; lastError?: string; failedKeys?: string[] }>(`/api/accounts/${id}/copy-objects`, { method: 'POST', body: JSON.stringify(body) }),
  /** 异步批量复制/移动：立即返回 jobId，进度走 migrate jobs SSE；`deleteSource` 由服务端在任务内删源。 */
  copyFilesAsync: (id: string, body: { bucket?: string; targetBucket?: string; targetPrefix?: string; keys: string[]; deleteSource?: boolean }) =>
    request<{ jobId: string; total: number }>(`/api/accounts/${id}/copy-objects/async`, { method: 'POST', body: JSON.stringify(body) }),
  getObjectAcl: (id: string, query: { bucket?: string; key: string }) => {
    const qs = new URLSearchParams()
    qs.set('key', query.key)
    if (query.bucket) qs.set('bucket', query.bucket)
    return request<{
      bucket: string
      key: string
      owner?: string
      public: boolean
      grants: { grantee: string; permission: string }[]
      url: string
    }>(`/api/accounts/${id}/object-acl?${qs.toString()}`)
  },
  putObjectAcl: (id: string, body: { bucket?: string; key: string; acl: string }) =>
    request<{ acl: string }>(`/api/accounts/${id}/object-acl`, { method: 'PUT', body: JSON.stringify(body) }),
  getObjectTags: (id: string, query: { bucket?: string; key: string }) => {
    const qs = new URLSearchParams()
    qs.set('key', query.key)
    if (query.bucket) qs.set('bucket', query.bucket)
    return request<{ tags: { key: string; value: string }[] }>(`/api/accounts/${id}/object-tags?${qs.toString()}`)
  },
  putObjectTags: (id: string, body: { bucket?: string; key: string; tags: { key: string; value: string }[] }) =>
    request<{ tags: { key: string; value: string }[] }>(`/api/accounts/${id}/object-tags`, { method: 'PUT', body: JSON.stringify(body) }),
  getBucketInfo: (id: string, bucket?: string) =>
    request<BucketInfo>(`/api/accounts/${id}/bucket-info?bucket=${encodeURIComponent(bucket ?? '')}`),
  putBucketVersioning: (id: string, body: { bucket?: string; status: 'Enabled' | 'Suspended' }) =>
    request<{ versioning: string }>(`/api/accounts/${id}/bucket-versioning`, { method: 'PUT', body: JSON.stringify(body) }),
  getBucketEncryption: (id: string, bucket?: string) =>
    request<{ bucket: string; configured: boolean; algorithm: string; kmsKeyId: string; bucketKeyEnabled: boolean }>(`/api/accounts/${id}/bucket/encryption?bucket=${encodeURIComponent(bucket ?? '')}`),
  putBucketEncryption: (id: string, body: { bucket?: string; algorithm: string; kmsKeyId?: string; bucketKeyEnabled?: boolean }) =>
    request<{ configured: boolean; algorithm: string }>(`/api/accounts/${id}/bucket/encryption`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteBucketEncryption: (id: string, bucket?: string) =>
    request<{ deleted: string }>(`/api/accounts/${id}/bucket/encryption?bucket=${encodeURIComponent(bucket ?? '')}`, { method: 'DELETE' }),
  getBucketCors: (id: string, bucket?: string) =>
    request<{ bucket: string; rules: CorsRule[] }>(`/api/accounts/${id}/bucket/cors?bucket=${encodeURIComponent(bucket ?? '')}`),
  putBucketCors: (id: string, body: { bucket?: string; rules: CorsRule[] }) =>
    request<{ updated: number; deleted?: string }>(`/api/accounts/${id}/bucket/cors`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteBucketCors: (id: string, bucket?: string) =>
    request<{ deleted: string }>(`/api/accounts/${id}/bucket/cors?bucket=${encodeURIComponent(bucket ?? '')}`, { method: 'DELETE' }),
  getBucketWebsite: (id: string, bucket?: string) =>
    request<{ bucket: string; configured: boolean; indexDocument: string; errorDocument: string; redirectAllRequestsTo: string }>(`/api/accounts/${id}/bucket/website?bucket=${encodeURIComponent(bucket ?? '')}`),
  putBucketWebsite: (id: string, body: { bucket?: string; indexDocument?: string; errorDocument?: string; redirectAllRequestsTo?: string }) =>
    request<{ configured: boolean }>(`/api/accounts/${id}/bucket/website`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteBucketWebsite: (id: string, bucket?: string) =>
    request<{ deleted: string }>(`/api/accounts/${id}/bucket/website?bucket=${encodeURIComponent(bucket ?? '')}`, { method: 'DELETE' }),
  getBucketPolicy: (id: string, bucket?: string) =>
    request<{ bucket: string; configured: boolean; policy: string }>(`/api/accounts/${id}/bucket/policy?bucket=${encodeURIComponent(bucket ?? '')}`),
  putBucketPolicy: (id: string, body: { bucket?: string; policy: string }) =>
    request<{ configured: boolean; deleted?: string }>(`/api/accounts/${id}/bucket/policy`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteBucketPolicy: (id: string, bucket?: string) =>
    request<{ deleted: string }>(`/api/accounts/${id}/bucket/policy?bucket=${encodeURIComponent(bucket ?? '')}`, { method: 'DELETE' }),
  getBucketTags: (id: string, bucket?: string) =>
    request<{ bucket: string; tags: { key: string; value: string }[] }>(`/api/accounts/${id}/bucket/tags?bucket=${encodeURIComponent(bucket ?? '')}`),
  putBucketTags: (id: string, body: { bucket?: string; tags: { key: string; value: string }[] }) =>
    request<{ updated: number; deleted?: string }>(`/api/accounts/${id}/bucket/tags`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteBucketTags: (id: string, bucket?: string) =>
    request<{ deleted: string }>(`/api/accounts/${id}/bucket/tags?bucket=${encodeURIComponent(bucket ?? '')}`, { method: 'DELETE' }),
  listVersions: (id: string, q: { bucket?: string; prefix?: string; keyMarker?: string; versionIdMarker?: string }) => {
    const qs = new URLSearchParams()
    if (q.bucket) qs.set('bucket', q.bucket)
    if (q.prefix) qs.set('prefix', q.prefix)
    if (q.keyMarker) qs.set('keyMarker', q.keyMarker)
    if (q.versionIdMarker) qs.set('versionIdMarker', q.versionIdMarker)
    return request<ListVersionsResponse>(`/api/accounts/${id}/versions?${qs.toString()}`)
  },
  deleteObjectVersion: (id: string, q: { bucket?: string; key: string; versionId: string }) => {
    const qs = new URLSearchParams()
    qs.set('key', q.key)
    qs.set('versionId', q.versionId)
    if (q.bucket) qs.set('bucket', q.bucket)
    return request<{ deleted: string; versionId: string }>(`/api/accounts/${id}/version?${qs.toString()}`, { method: 'DELETE' })
  },
  restoreObjectVersion: (id: string, body: { bucket?: string; key: string; versionId: string }) =>
    request<{ restored: string; versionId: string }>(`/api/accounts/${id}/version/restore`, { method: 'POST', body: JSON.stringify(body) }),
  restoreDeleteMarker: (id: string, body: { bucket?: string; key: string; versionId: string }) =>
    request<{ restored: string; versionId: string }>(`/api/accounts/${id}/delete-marker/restore`, { method: 'POST', body: JSON.stringify(body) }),
  listTrash: (id: string, q: { bucket?: string; prefix?: string; keyMarker?: string; versionIdMarker?: string; maxKeys?: number }) => {
    const qs = new URLSearchParams()
    if (q.bucket) qs.set('bucket', q.bucket)
    if (q.prefix) qs.set('prefix', q.prefix)
    if (q.keyMarker) qs.set('keyMarker', q.keyMarker)
    if (q.versionIdMarker) qs.set('versionIdMarker', q.versionIdMarker)
    if (q.maxKeys) qs.set('maxKeys', String(q.maxKeys))
    return request<{ deleteMarkers: { key: string; versionId: string; isLatest: boolean; lastModified: string }[]; isTruncated: boolean; nextKeyMarker: string; nextVersionIdMarker: string }>(`/api/accounts/${id}/trash?${qs.toString()}`)
  },
  purgeTrashObject: (id: string, body: { bucket?: string; key: string }) =>
    request<{ purged: string; deleted: number }>(`/api/accounts/${id}/trash/purge`, { method: 'POST', body: JSON.stringify(body) }),
  changeStorageClass: (id: string, body: { bucket?: string; key: string; versionId?: string; storageClass: string }) =>
    request<{ changed: string; versionId: string; storageClass: string }>(`/api/accounts/${id}/storage-class`, { method: 'POST', body: JSON.stringify(body) }),
  setHeaders: (id: string, body: { bucket?: string; key: string; contentType?: string; metadata?: Record<string, string> }) =>
    request<{ updated: string }>(`/api/accounts/${id}/set-headers`, { method: 'POST', body: JSON.stringify(body) }),
  getLifecycle: (id: string, bucket?: string) =>
    request<{ rules: LifecycleRule[] }>(`/api/accounts/${id}/lifecycle?bucket=${encodeURIComponent(bucket ?? '')}`),
  putLifecycle: (id: string, body: { bucket?: string; rules: LifecycleRule[] }) =>
    request<{ updated: number }>(`/api/accounts/${id}/lifecycle`, { method: 'PUT', body: JSON.stringify(body) }),
  presign: (id: string, body: { method?: string; key: string; bucket?: string; versionId?: string; expiresIn?: number }) =>
    request<PresignResponse>(`/api/accounts/${id}/presign`, { method: 'POST', body: JSON.stringify(body) }),
  multipartInit: (id: string, body: { bucket?: string; key: string; contentType?: string }) =>
    request<{ uploadId: string; key: string; bucket: string }>(`/api/accounts/${id}/multipart/init`, { method: 'POST', body: JSON.stringify(body) }),
  multipartPart: (id: string, body: { bucket?: string; key: string; uploadId: string; partNumber: number; expiresIn?: number }) =>
    request<{ partNumber: number; url: string; expiresIn: number }>(`/api/accounts/${id}/multipart/part`, { method: 'POST', body: JSON.stringify(body) }),
  multipartComplete: (id: string, body: { bucket?: string; key: string; uploadId: string; parts: { partNumber: number; etag: string }[] }) =>
    request<{ completed: string }>(`/api/accounts/${id}/multipart/complete`, { method: 'POST', body: JSON.stringify(body) }),
  multipartAbort: (id: string, body: { bucket?: string; key: string; uploadId: string }) =>
    request<{ aborted: boolean }>(`/api/accounts/${id}/multipart/abort`, { method: 'POST', body: JSON.stringify(body) }),
  deleteObjects: (id: string, body: { bucket?: string; keys: string[] }) =>
    request<{ deleted: number }>(`/api/accounts/${id}/delete`, { method: 'POST', body: JSON.stringify(body) }),
  deletePrefix: (id: string, body: { bucket?: string; prefix: string }) =>
    request<{ deleted: number; truncated: boolean }>(`/api/accounts/${id}/delete-prefix`, { method: 'POST', body: JSON.stringify(body) }),
  /** 异步前缀删除：立即返回 jobId，进度走 migrate jobs SSE。 */
  deletePrefixAsync: (id: string, body: { bucket?: string; prefix: string }) =>
    request<{ jobId: string; total: number; truncated?: boolean }>(`/api/accounts/${id}/delete-prefix/async`, { method: 'POST', body: JSON.stringify(body) }),
  copyPrefix: (id: string, body: { bucket?: string; prefix: string; targetBucket?: string; targetPrefix: string }) =>
    request<{ copied: number; failed: number; total: number; lastError?: string }>(`/api/accounts/${id}/copy-prefix`, { method: 'POST', body: JSON.stringify(body) }),
  copyPrefixAsync: (id: string, body: { bucket?: string; prefix: string; targetBucket?: string; targetPrefix: string }) =>
    request<{ jobId: string; total: number; truncated?: boolean }>(`/api/accounts/${id}/copy-prefix/async`, { method: 'POST', body: JSON.stringify(body) }),
  /** 流式 ZIP 落盘（优先 File System Access API）。 */
  downloadZipToDisk: (id: string, body: { bucket?: string; keys: string[] }, suggestedName?: string) =>
    downloadZipToDisk(id, body, suggestedName),
  migrate: (body: {
    sourceAccountId: string
    sourceBucket?: string
    sourceKeys: string[]
    targetAccountId: string
    targetBucket?: string
    targetPrefix?: string
  }) => request<MigrationResult>('/api/migrate', { method: 'POST', body: JSON.stringify(body) }),

  migrateAsync: (body: {
    sourceAccountId: string
    sourceBucket?: string
    sourceKeys: string[]
    targetAccountId: string
    targetBucket?: string
    targetPrefix?: string
  }) =>
    request<{ jobId: string; total: number }>('/api/migrate/async', { method: 'POST', body: JSON.stringify(body) }),

  migrateJobStatus: (jobId: string) =>
    request<{ jobId: string; done: boolean; progress: MigrateProgress; result?: MigrationResult }>(
      `/api/migrate/jobs/${encodeURIComponent(jobId)}`,
    ),

  migrateJobCancel: (jobId: string) =>
    request<{ jobId: string; cancelled: boolean; done?: boolean }>(
      `/api/migrate/jobs/${encodeURIComponent(jobId)}/cancel`,
      { method: 'POST' },
    ),
}

export interface MigrateProgress {
  done: number
  total: number
  migrated: number
  failed: number
  key?: string
  error?: string
  status?: string
}

/** 订阅迁移 SSE 进度（fetch 流式，支持 Bearer）。返回 abort 函数。 */
export function subscribeMigrateEvents(
  jobId: string,
  onProgress: (p: MigrateProgress) => void,
  onError: (err: Error) => void,
): () => void {
  const ctrl = new AbortController()
  const headers: Record<string, string> = { Accept: 'text/event-stream' }
  if (api.token) headers['Authorization'] = `Bearer ${api.token}`
  ;(async () => {
    let lastStatus: string | undefined
    try {
      const res = await fetch(`${api.base}/api/migrate/jobs/${encodeURIComponent(jobId)}/events`, {
        headers,
        signal: ctrl.signal,
      })
      if (!res.ok || !res.body) {
        throw new Error(`${res.status} ${res.statusText}`)
      }
      const reader = res.body.getReader()
      const dec = new TextDecoder()
      let buf = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += dec.decode(value, { stream: true })
        let idx: number
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          let eventName = ''
          for (const line of block.split('\n')) {
            if (line.startsWith('event:')) {
              eventName = line.slice(6).trim()
            } else if (line.startsWith('data:')) {
              // 心跳 ping 忽略
              if (eventName === 'ping') continue
              const raw = line.startsWith('data: ') ? line.slice(6) : line.slice(5).trimStart()
              try {
                const p = JSON.parse(raw) as MigrateProgress
                if (p.status) lastStatus = p.status
                onProgress(p)
              } catch {
                /* ignore partial */
              }
            }
          }
        }
      }
      // 流正常 EOF 但未收到终态：回读一次 job 状态，避免 UI 永久卡在「迁移中」
      if (lastStatus !== 'done' && lastStatus !== 'cancelled' && !ctrl.signal.aborted) {
        try {
          const st = await s3api.migrateJobStatus(jobId)
          const p: MigrateProgress = {
            ...st.progress,
            status: st.progress.status ?? (st.done ? 'done' : st.progress.status),
          }
          if (st.done && !p.status) p.status = 'done'
          onProgress(p)
        } catch {
          /* status 回读失败时由调用方超时/重试兜底 */
        }
      }
    } catch (e) {
      if (!ctrl.signal.aborted) onError(e instanceof Error ? e : new Error(String(e)))
    }
  })()
  return () => ctrl.abort()
}

export function directUpload(
  url: string,
  file: File,
  onProgress?: (pct: number) => void,
  signal?: AbortSignal,
): Promise<void> {
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
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream')
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100))
    }
    xhr.onload = () => {
      cleanup()
      if (xhr.status >= 200 && xhr.status < 300) resolve()
      else reject(new Error(`upload failed: HTTP ${xhr.status}`))
    }
    xhr.onerror = () => {
      cleanup()
      reject(new Error('upload network error'))
    }
    xhr.onabort = () => {
      cleanup()
      reject(new DOMException('Aborted', 'AbortError'))
    }
    xhr.send(file)
  })
}
