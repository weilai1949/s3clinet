/** Tab 键与 URL hash 深链接（无需 vue-router 依赖）。 */
export type TabKey = 'accounts' | 'objects' | 'upload' | 'migrate' | 'buckets' | 'trash' | 'server'

const VALID_TABS = new Set<TabKey>([
  'accounts', 'objects', 'upload', 'migrate', 'buckets', 'trash', 'server',
])

const HASH_PREFIX = '#/'

/** 从 location.hash 解析 tab；无效或缺失时返回 null。 */
export function tabFromHash(hash: string = location.hash): TabKey | null {
  const raw = hash.replace(/^#\/?/, '').split('?')[0].split('/')[0]
  if (raw && VALID_TABS.has(raw as TabKey)) return raw as TabKey
  return null
}

/** 将 tab 写入 hash（不触发页面刷新）。 */
export function setTabHash(tab: TabKey) {
  const next = HASH_PREFIX + tab
  if (location.hash !== next) {
    history.replaceState(null, '', next)
  }
}

/** 监听浏览器前进/后退导致的 hash 变化。 */
export function onTabHashChange(cb: (tab: TabKey | null) => void): () => void {
  const handler = () => cb(tabFromHash())
  window.addEventListener('hashchange', handler)
  return () => window.removeEventListener('hashchange', handler)
}
