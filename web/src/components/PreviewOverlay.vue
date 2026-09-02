<script lang="ts">
import type { PreviewKind } from '../preview'

export interface PreviewState {
  key: string
  kind: PreviewKind
  url: string // inline 代理 URL（图片/PDF/媒体）
}
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { api } from '../api'
import { proxyUrl } from '../proxy'
import { t, tf } from '../i18n'
import { useKeydownStack } from '../composables/useKeydownStack'

const props = defineProps<{
  preview: PreviewState | null
  accountId: string
  bucket: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'error', msg: string): void
}>()

const text = ref('')
const truncated = ref(false)
const loading = ref(false)

let fetchCtrl: AbortController | null = null

function cancelFetch() {
  fetchCtrl?.abort()
  fetchCtrl = null
}

// Escape 关闭预览（与 ModalDialog 行为一致；经 keydown 栈，仅顶层生效）。
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.preview) {
    e.preventDefault()
    emit('close')
  }
}
useKeydownStack(onKeydown, computed(() => !!props.preview))
onBeforeUnmount(() => {
  cancelFetch()
})

watch(() => props.preview, async (p) => {
  cancelFetch()
  if (!p) {
    text.value = ''
    truncated.value = false
    loading.value = false
    return
  }
  text.value = ''
  truncated.value = false
  if (p.kind === 'text') {
    const ctrl = new AbortController()
    fetchCtrl = ctrl
    loading.value = true
    try {
      const res = await fetch(proxyUrl(props.accountId, props.bucket, 'text', p.key, api.base), {
        headers: api.token ? { Authorization: `Bearer ${api.token}` } : {},
        signal: ctrl.signal,
      })
      if (fetchCtrl !== ctrl) return
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
      text.value = await res.text()
      if (fetchCtrl !== ctrl) return
      truncated.value = res.headers.get('X-Preview-Truncated') === '1'
    } catch (err) {
      if (ctrl.signal.aborted || fetchCtrl !== ctrl) return
      text.value = tf('preview.fail', { msg: toErrorMessage(err) })
    } finally {
      if (fetchCtrl === ctrl) loading.value = false
    }
  }
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="preview" class="pv-backdrop" @click.self="emit('close')">
        <div class="pv-card" role="dialog" aria-modal="true" :aria-label="tf('preview.aria', { key: preview.key })">
          <div class="pv-head">
            <span class="mono" style="word-break:break-all">{{ preview.key }}</span>
            <div class="pv-actions">
              <a :href="proxyUrl(props.accountId, props.bucket, 'download', preview.key, api.base)" class="btn secondary sm" style="text-decoration:none">{{ t('common.download') }}</a>
              <button class="btn secondary sm" @click="emit('close')">{{ t('common.close') }}</button>
            </div>
          </div>
          <div class="pv-body">
            <!-- 图片（含 SVG）：img 上下文脚本不执行 -->
            <img v-if="preview.kind === 'image'" :src="preview.url" :alt="preview.key" @error="emit('close')" />
            <!-- 视频/音频：原生播放器（服务端代理支持 Range 拖动） -->
            <video v-else-if="preview.kind === 'video'" :src="preview.url" controls class="pv-media" />
            <audio v-else-if="preview.kind === 'audio'" :src="preview.url" controls class="pv-audio" />
            <!-- PDF：sandbox iframe，禁脚本/弹窗 -->
            <iframe v-else-if="preview.kind === 'pdf'" :src="preview.url" sandbox="" class="pv-iframe" :title="t('preview.pdfTitle')" />
            <!-- 文本/代码：服务端强制纯文本，前端转义渲染 -->
            <div v-else-if="preview.kind === 'text'" class="pv-text-wrap">
              <pre class="pv-pre" v-if="!loading">{{ text }}</pre>
              <div v-else class="empty" style="padding:30px">{{ t('preview.loadingText') }}</div>
              <div v-if="truncated" class="badge" style="color:var(--warn)">
                {{ t('preview.truncated') }}
              </div>
            </div>
            <!-- 未知类型 -->
            <div v-else class="empty">
              <span class="empty-icon" aria-hidden="true">📄</span>
              {{ t('preview.unsupported') }}
            </div>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* 图片预览 */
.pv-backdrop {
  position: fixed; inset: 0; z-index: 190;
  background: rgba(5, 10, 8, .7);
  backdrop-filter: blur(4px);
  display: flex; align-items: center; justify-content: center;
  padding: 24px;
}
.pv-card {
  display: flex; flex-direction: column;
  max-width: min(1100px, 100%);
  max-height: 100%;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.pv-head {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 10px 14px;
  border-bottom: 1px solid var(--border);
}
.pv-actions { display: flex; gap: 8px; flex: none; }
.pv-body { padding: 12px; overflow: auto; display: flex; align-items: center; justify-content: center; }
.pv-body img { max-width: 100%; max-height: 72vh; border-radius: var(--radius); }
.pv-media { max-width: 100%; max-height: 72vh; border-radius: var(--radius); }
.pv-audio { width: min(560px, 100%); }
.pv-iframe {
  width: min(1100px, 80vw); height: 72vh;
  border: 1px solid var(--border); border-radius: var(--radius);
  background: var(--panel-2);
}
.pv-text-wrap { width: min(900px, 100%); }
.pv-pre {
  margin: 0; padding: 14px;
  max-height: 68vh; overflow: auto;
  background: var(--panel-2);
  border: 1px solid var(--border); border-radius: var(--radius);
  font-family: var(--font-mono); font-size: 12px; line-height: 1.7;
  white-space: pre-wrap; word-break: break-all;
  color: var(--text);
}
</style>
