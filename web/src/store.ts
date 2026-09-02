import { reactive } from 'vue'
import type { Account } from './types'

/** 记住上次选中的账号（互联网应用习惯：刷新/重开后恢复现场）。 */
const LS_CURRENT_ACCOUNT = 's3c.currentAccountId'

export const state = reactive({
  accounts: [] as Account[],
  currentAccountId: '',
})

/** 跨面板跳转信号：任何组件可请求切换到某 tab（App 监听执行）。 */
export const tabRequest = reactive({ tab: '', seq: 0 })

export function requestTab(tab: string) {
  tabRequest.tab = tab
  tabRequest.seq++
}

/** 请求账号面板自动打开「新增登录」表单。 */
export const accountFormRequest = reactive({ seq: 0 })

export function requestAccountForm() {
  accountFormRequest.seq++
}

export function currentAccount(): Account | undefined {
  return state.accounts.find((a) => a.id === state.currentAccountId)
}

/** 切换当前账号并持久化，下次打开自动恢复。 */
export function selectAccount(id: string) {
  state.currentAccountId = id
  if (id) localStorage.setItem(LS_CURRENT_ACCOUNT, id)
  else localStorage.removeItem(LS_CURRENT_ACCOUNT)
}

/** 读取上次选中的账号 id（可能已不存在，由调用方校验）。 */
export function rememberedAccountId(): string {
  return localStorage.getItem(LS_CURRENT_ACCOUNT) ?? ''
}

/* ---- 全局 Toast 反馈（支持操作按钮） ---- */

export interface ToastItem {
  id: number
  kind: 'ok' | 'err'
  text: string
  /** 可选动作按钮（例如「查看对象」跳转）。 */
  action?: { label: string; onClick: () => void }
}

export const toasts = reactive<ToastItem[]>([])

let toastSeq = 0
const toastTimers = new Map<number, ReturnType<typeof setTimeout>>()

export function toast(text: string, kind: 'ok' | 'err' = 'ok', action?: ToastItem['action']) {
  const id = ++toastSeq
  toasts.push({ id, kind, text, action })
  toastTimers.set(id, setTimeout(() => dismissToast(id), 3600))
}

export function dismissToast(id: number) {
  const timer = toastTimers.get(id)
  if (timer !== undefined) {
    clearTimeout(timer)
    toastTimers.delete(id)
  }
  const i = toasts.findIndex((t) => t.id === id)
  if (i >= 0) toasts.splice(i, 1)
}
