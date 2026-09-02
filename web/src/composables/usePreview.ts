import { ref } from 'vue'
import { api } from '../api'
import { proxyUrl } from '../proxy'
import { previewKind } from '../preview'
import type { Account } from '../types'
import type { ObjectItem } from '../types'
import type { PreviewState } from '../components/PreviewOverlay.vue'
import type { Entry } from '../types'
import type { ComputedRef, Ref } from 'vue'

/** 预览组合式所需的对象浏览核心 + 下载动作（由面板注入）。 */
export interface PreviewCtx {
  account: ComputedRef<Account | undefined>
  currentBucket: Ref<string>
  getCtxEntry: () => Entry | undefined
  closeCtx: () => void
  download: (o: ObjectItem) => void
}

/** 安全预览状态 + 显示/分发逻辑（文本取回由 PreviewOverlay 组件自身完成）。 */
export function usePreview(ctx: PreviewCtx) {
  const preview = ref<PreviewState | null>(null)

  function showPreview(o: ObjectItem) {
    const kind = previewKind(o.key)
    // 未知类型也打开预览面板（显示友好提示与下载入口），避免“右键没有查看入口”的困惑
    preview.value = {
      key: o.key,
      kind,
      url: kind === 'none' ? '' : proxyUrl(ctx.account.value!.id, ctx.currentBucket.value, 'inline', o.key, api.base),
    }
  }

  // previewOrDownload 查看对象：可预览类型 → 打开预览弹窗；未知类型 → 直接下载。
  // 对应云控制台「查看/打开」习惯（双击 = 查看）。
  function previewOrDownload(o: ObjectItem) {
    if (previewKind(o.key) === 'none') ctx.download(o)
    else showPreview(o)
  }

  function ctxPreview() {
    const e = ctx.getCtxEntry()
    ctx.closeCtx()
    if (!e || e.kind !== 'file' || !e.object) return
    showPreview(e.object)
  }

  return { preview, showPreview, previewOrDownload, ctxPreview }
}
