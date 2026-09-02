<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api, api } from '../api'
import { proxyUrl } from '../proxy'
import { fmtDate, fmtSize } from '../format'
import { storageClassLabel } from '../storageClass'
import { lineDiff, looksBinary, type DiffLine } from '../versionDiff'
import { t, tf } from '../i18n'
import ModalDialog from './ModalDialog.vue'

export interface CompareVersion {
  versionId: string
  size: number
  etag: string
  lastModified: string
  storageClass: string
  isLatest: boolean
  contentType?: string
}

const props = defineProps<{
  open: boolean
  accountId: string
  bucket: string
  objectKey: string
  versions: CompareVersion[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'error', msg: string): void
}>()

const baseIdx = ref(0)
const targetIdx = ref(0)
const loading = ref(false)
const diff = ref<DiffLine[]>([])
const baseText = ref('')
const targetText = ref('')
const diffState = ref<'idle' | 'done' | 'binary' | 'error' | 'too-large'>('idle')
const compareError = ref('')

let compareCtrl: AbortController | null = null

function cancelCompare() {
  compareCtrl?.abort()
  compareCtrl = null
}

const base = computed(() => props.versions[baseIdx.value])
const target = computed(() => props.versions[targetIdx.value])

watch(
  () => props.open,
  async (o) => {
    if (!o) {
      cancelCompare()
      return
    }
    if (props.versions.length < 2) return
    // 默认：最新为新版本，期前一个为基准
    targetIdx.value = 0
    baseIdx.value = props.versions.length > 1 ? 1 : 0
    await runCompare()
  },
)

async function fetchVersionContent(v: CompareVersion, signal: AbortSignal): Promise<string> {
  const res = await s3api.presign(props.accountId, {
    method: 'get',
    bucket: props.bucket,
    key: props.objectKey,
    versionId: v.versionId,
    expiresIn: 900,
  })
  const r = await fetch(res.url, { signal })
  if (!r.ok) throw new Error(`HTTP ${r.status}`)
  return r.text()
}

async function runCompare() {
  if (!base.value || !target.value || base.value.versionId === target.value.versionId) return
  cancelCompare()
  const ctrl = new AbortController()
  compareCtrl = ctrl
  loading.value = true
  diffState.value = 'idle'
  compareError.value = ''
  baseText.value = ''
  targetText.value = ''
  diff.value = []
  try {
    const MAX = 2 * 1024 * 1024 // 2MB 上限，超出仅展示元数据
    if (base.value.size > MAX || target.value.size > MAX) {
      diffState.value = 'too-large'
      return
    }
    const [a, b] = await Promise.all([
      fetchVersionContent(base.value, ctrl.signal),
      fetchVersionContent(target.value, ctrl.signal),
    ])
    if (compareCtrl !== ctrl) return
    if (looksBinary(a) || looksBinary(b)) {
      diffState.value = 'binary'
      baseText.value = a
      targetText.value = b
      return
    }
    baseText.value = a
    targetText.value = b
    diff.value = lineDiff(a, b)
    diffState.value = 'done'
  } catch (err) {
    if (ctrl.signal.aborted || compareCtrl !== ctrl) return
    diffState.value = 'error'
    compareError.value = toErrorMessage(err)
  } finally {
    if (compareCtrl === ctrl) loading.value = false
  }
}

/** 通过服务端代理下载指定版本（force attachment，内容不进渲染管道）。 */
function downloadVersion(v: CompareVersion) {
  const a = document.createElement('a')
  a.href = proxyUrl(props.accountId, props.bucket, 'download', props.objectKey, api.base, v.versionId)
  a.download = props.objectKey.split('/').pop() ?? 'object'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}
</script>

<template>
  <ModalDialog :open="open" :title="tf('compare.title', { key: objectKey })" width="min(840px, 100%)" @close="emit('close')">
    <div class="row" style="align-items:center; flex-wrap:wrap; gap:8px">
      <label class="field" style="flex:1; min-width:220px">
        {{ t('compare.base') }}
        <select v-model="baseIdx" :disabled="loading">
          <option v-for="(v, i) in versions" :key="v.versionId" :value="i">
            {{ v.isLatest ? t('compare.latest') : (v.versionId.slice(0, 8) + '…') }} · {{ fmtDate(v.lastModified) }}
          </option>
        </select>
      </label>
      <span class="muted">{{ t('compare.arrow') }}</span>
      <label class="field" style="flex:1; min-width:220px">
        {{ t('compare.target') }}
        <select v-model="targetIdx" :disabled="loading">
          <option v-for="(v, i) in versions" :key="v.versionId" :value="i">
            {{ v.isLatest ? t('compare.latest') : (v.versionId.slice(0, 8) + '…') }} · {{ fmtDate(v.lastModified) }}
          </option>
        </select>
      </label>
      <button class="btn sm" :disabled="loading || base?.versionId === target?.versionId" @click="runCompare">
        {{ loading ? t('compare.running') : t('compare.run') }}
      </button>
    </div>

    <!-- 元数据对比 -->
    <table class="tbl detail-tbl" style="margin-top:12px">
      <thead><tr><th style="width:110px">{{ t('compare.prop') }}</th><th>{{ t('compare.colBase') }}</th><th>{{ t('compare.colTarget') }}</th></tr></thead>
      <tbody>
        <tr><th>{{ t('compare.versionId') }}</th><td class="mono" style="word-break:break-all">{{ base?.versionId }}</td><td class="mono" style="word-break:break-all">{{ target?.versionId }}</td></tr>
        <tr><th>{{ t('common.size') }}</th><td>{{ fmtSize(base?.size ?? 0) }}</td><td>{{ fmtSize(target?.size ?? 0) }}</td></tr>
        <tr><th>{{ t('compare.mtime') }}</th><td>{{ fmtDate(base?.lastModified) }}</td><td>{{ fmtDate(target?.lastModified) }}</td></tr>
        <tr><th>{{ t('compare.storageClass') }}</th><td>{{ base?.storageClass ? storageClassLabel(base.storageClass) : '—' }}</td><td>{{ target?.storageClass ? storageClassLabel(target.storageClass) : '—' }}</td></tr>
        <tr><th>{{ t('compare.etag') }}</th><td class="mono" style="word-break:break-all">{{ base?.etag || '—' }}</td><td class="mono" style="word-break:break-all">{{ target?.etag || '—' }}</td></tr>
      </tbody>
    </table>

    <div style="margin-top:10px">
      <button class="btn secondary sm" style="margin-right:8px" :disabled="loading" @click="downloadVersion(base!)">{{ t('compare.dlBase') }}</button>
      <button class="btn secondary sm" :disabled="loading" @click="downloadVersion(target!)">{{ t('compare.dlTarget') }}</button>
    </div>

    <!-- 内容差异 -->
    <div v-if="loading" class="empty" style="padding:22px">{{ t('compare.loading') }}</div>
    <div v-else-if="diffState === 'too-large'" class="badge" style="display:block; margin-top:12px; color:var(--muted)">
      {{ t('compare.tooLarge') }}
    </div>
    <div v-else-if="diffState === 'binary'" class="badge" style="display:block; margin-top:12px; color:var(--muted)">
      {{ t('compare.binary') }}
    </div>
    <div v-else-if="diffState === 'error'" class="msg err" style="margin-top:12px">
      {{ tf('compare.error', { msg: compareError }) }}
    </div>
    <div v-else-if="diffState === 'done'" class="diff" style="margin-top:12px">
      <div v-for="(line, i) in diff" :key="i" :class="'diff-' + line.type">{{ line.text || '␣' }}</div>
    </div>
    <div v-else class="empty" style="padding:20px">{{ t('compare.hint') }}</div>
  </ModalDialog>
</template>

<style scoped>
.detail-tbl th { width: 110px; background: none; border-bottom: 1px solid var(--border); }
.diff {
  max-height: 380px; overflow: auto;
  border: 1px solid var(--border); border-radius: var(--radius);
  font-family: var(--font-mono); font-size: 12px; line-height: 1.5;
}
.diff > div { padding: 0 8px; white-space: pre; }
.diff-removed { background: rgba(214, 69, 69, .12); color: #d64545; }
.diff-added { background: rgba(16, 163, 124, .12); color: var(--primary); }
.diff-context, .diff-same { color: var(--muted); }
</style>
