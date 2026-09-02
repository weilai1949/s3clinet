/** 共享的「服务端代理 / 下载 URL」构造器（安全预览与下载统一入口）。 */

/**
 * 构造安全代理 URL：
 *   download=强制 attachment 下载（内容不进渲染管道）
 *   inline=透传 Content-Type（图片/PDF/媒体预览）
 *   text=强制 text/plain + nosniff（文本预览）
 */
export function proxyUrl(
  accountId: string,
  bucket: string,
  mode: 'download' | 'inline' | 'text',
  key: string,
  apiBase: string,
  versionId?: string,
): string {
  const p = new URLSearchParams({ bucket, key, mode })
  if (versionId) p.set('versionId', versionId)
  return apiBase + `/api/accounts/${accountId}/proxy?${p.toString()}`
}
