/** 全局输入对话框状态（单输入框，Promise 化，替代手写弹窗逻辑）。 */

import { reactive } from 'vue'
import { t } from './i18n'

export interface PromptRequest {
  title: string
  label?: string
  initial?: string
  placeholder?: string
  confirmText?: string
  /** 返回错误信息则阻止提交；返回 null 表示通过。 */
  validate?: (value: string) => string | null
}

interface PromptState extends PromptRequest {
  open: boolean
  value: string
  error: string
  resolve: ((value: string | null) => void) | null
}

export const promptState = reactive<PromptState>({
  open: false,
  title: '',
  label: '',
  initial: '',
  placeholder: '',
  confirmText: '',
  validate: undefined,
  value: '',
  error: '',
  resolve: null,
})

/** 弹出输入框，确认返回输入值，取消返回 null。 */
export function promptDialog(req: PromptRequest): Promise<string | null> {
  if (promptState.open && promptState.resolve) {
    promptState.resolve(null)
    promptState.resolve = null
  }
  return new Promise((resolve) => {
    promptState.title = req.title
    promptState.label = req.label ?? ''
    promptState.initial = req.initial ?? ''
    promptState.placeholder = req.placeholder ?? ''
    promptState.confirmText = req.confirmText ?? t('common.ok')
    promptState.validate = req.validate
    promptState.value = req.initial ?? ''
    promptState.error = ''
    promptState.resolve = resolve
    promptState.open = true
  })
}

/** 由 PromptDialog 组件调用：校验后关闭并回传结果。 */
export function settlePrompt(submit: boolean) {
  if (submit) {
    const err = promptState.validate?.(promptState.value) ?? null
    if (err) {
      promptState.error = err
      return
    }
  }
  promptState.open = false
  promptState.resolve?.(submit ? promptState.value : null)
  promptState.resolve = null
}
