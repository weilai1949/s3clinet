export interface Account {
  id: string
  name: string
  endpoint: string
  publicEndpoint?: string
  region: string
  accessKey: string
  secretKey: string
  bucket: string
  pathStyle: boolean
  useSSL: boolean
  createdAt?: string
  updatedAt?: string
}

export interface AccountInput {
  name: string
  endpoint: string
  publicEndpoint?: string
  region: string
  accessKey: string
  secretKey: string
  bucket: string
  pathStyle: boolean
  useSSL: boolean
}

export interface ObjectItem {
  key: string
  size: number
  lastModified: string
  etag: string
  contentType: string
  storageClass?: string
  isDir: boolean
}

export interface ListObjectsResponse {
  objects: ObjectItem[]
  commonPrefixes: string[]
  isTruncated: boolean
  nextToken: string
}

export interface PresignResponse {
  method: 'get' | 'put' | 'post'
  bucket: string
  key: string
  url: string
  fields?: Record<string, string>
  expiresIn: number
}

export interface BucketItem {
  name: string
  creationDate: string
}

/** HeadObject 返回的对象元数据详情。 */
export interface ObjectMeta {
  key: string
  size: number
  lastModified: string
  etag: string
  contentType: string
  storageClass?: string
  metadata?: Record<string, string>
}

/** 桶属性：区域 / 创建时间 / 版本控制状态。 */
export interface BucketInfo {
  bucket: string
  region: string
  createdAt: string
  versioning: '' | 'Enabled' | 'Suspended'
}

/** 单个对象版本（ListObjectVersions）。 */
export interface ObjectVersion {
  key: string
  versionId: string
  isLatest: boolean
  lastModified: string
  size: number
  etag: string
  storageClass?: string
}

export interface ListVersionsResponse {
  versions: ObjectVersion[]
  deleteMarkers: { key: string; versionId: string; isLatest: boolean; lastModified: string }[]
  isTruncated: boolean
  nextKeyMarker: string
  nextVersionIdMarker: string
}

/** 生命周期过期规则（简化版：前缀 + 天数）。 */
export interface LifecycleRule {
  id: string
  prefix: string
  days: number
}

export interface MigrationResult {
  migrated: number
  failed: number
  lastError?: string
  failedKeys?: string[]
}

/** 桶 CORS 规则。 */
export interface CorsRule {
  id?: string
  allowedMethods: string[]
  allowedOrigins: string[]
  allowedHeaders?: string[]
  exposeHeaders?: string[]
  maxAgeSeconds?: number
}

export type SortKey = 'name' | 'size' | 'time'

export interface Entry {
  kind: 'folder' | 'file'
  key: string
  name: string
  size?: number
  lastModified?: string
  object?: ObjectItem
}

/** 前端本地保存的后端连接配置（多服务端可选） */
export interface ServerProfile {
  id: string
  name: string
  base: string
  token: string
}

