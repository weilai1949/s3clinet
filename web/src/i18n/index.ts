import { reactive, readonly } from 'vue'
import { accountsMessages } from './messages/accounts'
import { bucketsMessages } from './messages/buckets'
import { commonMessages } from './messages/common'
import { objectDialogsMessages } from './messages/objectDialogs'
import { objectsMessages } from './messages/objects'
import { uploadMessages } from './messages/upload'
import type { Locale, MessageBundle } from './messages/types'

export type { Locale } from './messages/types'

const LS_LOCALE = 's3c.locale'

function readLocale(): Locale {
  try {
    const v = localStorage.getItem(LS_LOCALE) as Locale
    if (v === 'zh-CN' || v === 'en-US') return v
  } catch {
    /* vitest / SSR */
  }
  return 'zh-CN'
}

const state = reactive({ locale: readLocale() })

/** 按命名空间拆分的消息模块（见 messages/ 目录），在此汇总为运行时字典。 */
function mergeBundles(...bundles: MessageBundle[]): Record<Locale, Record<string, string>> {
  return {
    'zh-CN': Object.assign({}, ...bundles.map((b) => b['zh-CN'])),
    'en-US': Object.assign({}, ...bundles.map((b) => b['en-US'])),
  }
}

const messages = mergeBundles(
  commonMessages,
  objectsMessages,
  objectDialogsMessages,
  bucketsMessages,
  uploadMessages,
  accountsMessages,
)

export function t(key: string): string {
  return messages[state.locale][key] ?? messages['zh-CN'][key] ?? key
}

/** Simple `{name}` placeholder replacement for wired UI strings. */
export function tf(key: string, vars: Record<string, string | number>): string {
  let s = t(key)
  for (const [k, v] of Object.entries(vars)) {
    s = s.replaceAll(`{${k}}`, String(v))
  }
  return s
}

export function locale(): Locale {
  return state.locale
}

/** Number of dictionary keys for a locale (used by tests / coverage checks). */
export function i18nKeyCount(loc: Locale = 'zh-CN'): number {
  return Object.keys(messages[loc]).length
}

export function setLocale(loc: Locale) {
  state.locale = loc
  try {
    localStorage.setItem(LS_LOCALE, loc)
  } catch {
    /* ignore */
  }
}

export function cycleLocale(): Locale {
  const order: Locale[] = ['zh-CN', 'en-US']
  const i = order.indexOf(state.locale)
  const next = order[(i + 1) % order.length]
  setLocale(next)
  return next
}

export const i18nState = readonly(state)
