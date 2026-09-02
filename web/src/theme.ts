/** 主题管理：auto（跟随系统）/ light / dark，选择持久化到 localStorage。 */

import { ref } from 'vue'

export type Theme = 'auto' | 'light' | 'dark'

const LS_THEME = 's3c.theme'

const mq = window.matchMedia('(prefers-color-scheme: dark)')

/** 系统主题变化时自增，供 UI 响应式刷新（matchMedia 本身非响应式）。 */
export const systemThemeTick = ref(0)

/** 读取用户选择（默认 auto）。 */
export function readTheme(): Theme {
  const v = localStorage.getItem(LS_THEME)
  if (v === 'light' || v === 'dark' || v === 'auto') return v
  return 'auto'
}

/** 当前实际生效的主题（light | dark）。 */
export function resolvedTheme(): 'light' | 'dark' {
  const t = readTheme()
  return t === 'auto' ? (mq.matches ? 'dark' : 'light') : t
}

/** 应用主题到 <html data-theme>，供 styles.css 的 [data-theme=...] 选择器使用。 */
export function applyTheme(t: Theme = readTheme()) {
  document.documentElement.dataset.theme = resolvedTheme()
  localStorage.setItem(LS_THEME, t)
}

/** 切换并返回新主题（auto → light → dark → auto 循环）。 */
export function cycleTheme(): Theme {
  const next: Record<Theme, Theme> = { auto: 'light', light: 'dark', dark: 'auto' }
  const t = next[readTheme()]
  applyTheme(t)
  return t
}

// 跟随系统：auto 模式下系统主题变化时即时切换
mq.addEventListener('change', () => {
  systemThemeTick.value++
  if (readTheme() === 'auto') applyTheme('auto')
})

// 启动时应用一次（避免暗色系统下白屏闪烁）
applyTheme()
