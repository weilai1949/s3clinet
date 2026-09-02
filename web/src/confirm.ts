/** 全局确认对话框状态（替代原生 confirm 的 Promise 化实现）。 */

import { reactive } from 'vue'

export interface ConfirmRequest {
  title: string
  message: string
  /** 确认按钮文案（默认「删除」）；danger=false 时默认「确定」 */
  confirmText?: string
  danger?: boolean
}

interface ConfirmState extends ConfirmRequest {
  open: boolean
  resolve: ((ok: boolean) => void) | null
}

export const confirmState = reactive<ConfirmState>({
  open: false,
  title: '',
  message: '',
  danger: true,
  resolve: null,
})

/** 弹出确认框，返回用户选择（Promise<boolean>）。 */
export function confirmDialog(req: ConfirmRequest): Promise<boolean> {
  // 新弹窗覆盖旧弹窗时，先 settle 旧 Promise，避免悬挂回调
  if (confirmState.open && confirmState.resolve) {
    confirmState.resolve(false)
    confirmState.resolve = null
  }
  return new Promise((resolve) => {
    confirmState.title = req.title
    confirmState.message = req.message
    confirmState.confirmText = req.confirmText
    confirmState.danger = req.danger ?? true
    confirmState.resolve = resolve
    confirmState.open = true
  })
}

/** 由 ConfirmDialog 组件调用，关闭并回传结果。 */
export function settleConfirm(ok: boolean) {
  confirmState.open = false
  confirmState.resolve?.(ok)
  confirmState.resolve = null
}
