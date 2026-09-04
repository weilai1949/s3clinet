<script setup lang="ts">
defineOptions({ name: 'UploadPanel' })

import { computed, ref } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { currentAccount, toast, requestTab } from '../store'
import { copyText } from '../clipboard'
import { fmtSize } from '../format'
import { t, tf } from '../i18n'
import { useUploadQueue } from '../composables/useUploadQueue'
import type { UploadQueueItem } from '../composables/useUploadQueue'

const prefix = ref('')
const error = ref('')
const dragging = ref(false)
const fileInput = ref<HTMLInputElement>()

const account = computed(() => currentAccount())

function keyFor(filename: string): string {
  return (prefix.value ? prefix.value.replace(/\/+$/, '') + '/' : '') + filename
}

// 共享上传队列：并发控制 / 状态机 / abort 见 useUploadQueue。
// 本面板语义：中止回 pending（可再次上传，requeue）、单批快照（不带边传边加）。
const queue = useUploadQueue({
  target: (it) => ({ accId: account.value!.id, key: it.key }),
  onItemStart: (it) => (it.key = keyFor(it.file.name)), // 用当前前缀
  selectBatch: (all) => all.filter((it) => it.status !== 'done' && it.status !== 'cancelled'),
  drain: false,
  onAbort: 'requeue',
})
const items = queue.items
const running = queue.running

const hasErr = computed(() => items.value.some((it) => it.status === 'err'))
const doneCount = computed(() => items.value.filter((it) => it.status === 'done').length)
const totalPct = computed(() => {
  if (!items.value.length) return 0
  return Math.round(items.value.reduce((s, it) => s + it.pct, 0) / items.value.length)
})

function addFiles(files: FileList | null) {
  queue.enqueue(files, (f) => ({ key: keyFor(f.name) }))
  // 允许重复选择同一文件
  if (fileInput.value) fileInput.value.value = ''
}

function onDrop(e: DragEvent) {
  dragging.value = false
  addFiles(e.dataTransfer?.files ?? null)
}

async function uploadAll() {
  if (!account.value) return
  error.value = ''
  const processed = await queue.run()
  const failed = processed.filter((it) => it.status === 'err').length
  if (processed.length) {
    // 完成 Toast 带「查看对象」跳转（跨面板联动，互联网应用习惯）
    const goObjects = { label: t('upload.viewObjects'), onClick: () => requestTab('objects') }
    if (failed) toast(tf('upload.toastPartial', { ok: processed.length - failed, fail: failed }), 'err', goObjects)
    else toast(tf('upload.toastOk', { n: processed.length }), 'ok', goObjects)
  }
}

function retryFailed() {
  for (const it of items.value) {
    if (it.status === 'err') {
      it.status = 'pending'
      it.pct = 0
      it.err = undefined
    }
  }
  uploadAll()
}

function removeItem(i: number) {
  queue.abortItem(items.value[i])
  items.value.splice(i, 1)
}

/** 为已完成项生成 1 小时签名 GET 链接并复制（互联网上传面板习惯：传完即可分享）。 */
async function copyDoneLink(it: UploadQueueItem) {
  if (!account.value) return
  try {
    const res = await s3api.presign(account.value.id, {
      method: 'get',
      key: it.key,
      expiresIn: 3600,
    })
    await copyText(res.url)
    toast(t('upload.copiedLink'))
  } catch (e) {
    toast(toErrorMessage(e), 'err')
  }
}

function clearList() {
  queue.abortAll()
  items.value = []
}

/** 只移除已完成项，保留等待/失败项便于重试（互联网上传面板习惯）。 */
function clearDone() {
  items.value = items.value.filter((it) => it.status !== 'done')
}
</script>

<template>
  <div class="panel">
    <div class="toolbar">
      <h3 style="margin:0">{{ t('upload.title') }}</h3>
      <span class="spacer" />
      <span class="badge">{{ tf('upload.currentAccount', { name: account?.name || t('common.noAccount') }) }}</span>
    </div>

    <div v-if="!account" class="empty">
      <span class="empty-icon" aria-hidden="true">🗂️</span>
      {{ t('upload.needAccount') }}
    </div>

    <template v-else>
      <div class="toolbar">
        <label class="field" style="flex-direction:row; align-items:center">
          {{ t('upload.prefix') }}
          <input v-model="prefix" :placeholder="t('upload.prefixPlaceholder')" />
        </label>
        <span class="spacer" />
        <button class="btn sm" :disabled="!items.length || running" @click="uploadAll">{{ t('upload.start') }}</button>
        <button class="btn secondary sm" :disabled="running || !hasErr" @click="retryFailed">{{ t('upload.retryFailed') }}</button>
        <button class="btn secondary sm" :disabled="running || !doneCount" @click="clearDone">{{ t('upload.clearDone') }}</button>
        <button class="btn secondary sm" :disabled="running || !items.length" @click="clearList">{{ t('upload.clearAll') }}</button>
      </div>

      <div
        class="dropzone"
        :class="{ over: dragging }"
        role="button"
        tabindex="0"
        :aria-label="t('upload.dropAria')"
        @click="fileInput?.click()"
        @keydown.enter.prevent="fileInput?.click()"
        @keydown.space.prevent="fileInput?.click()"
        @dragover.prevent="dragging = true"
        @dragleave.prevent="dragging = false"
        @drop.prevent="onDrop"
      >
        <div class="dz-icon" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 14.899A7 7 0 1115.71 8h1.79a4.5 4.5 0 012.5 8.242M12 12v9m0-9l-3.5 3.5M12 12l3.5 3.5" />
          </svg>
        </div>
        <div class="dz-title">{{ t('upload.dropTitle') }}</div>
        <div class="dz-sub">{{ t('upload.dropSub') }}</div>
        <input ref="fileInput" type="file" multiple style="display:none" @change="(e) => addFiles((e.target as HTMLInputElement).files)" />
      </div>

      <div v-if="error" class="msg err" style="margin-bottom:10px">{{ error }}</div>

      <template v-if="items.length">
        <div class="row" style="margin-bottom:10px">
          <div class="progress" style="flex:1" role="progressbar" :aria-valuenow="totalPct" aria-valuemin="0" aria-valuemax="100">
            <div class="bar" :style="{ width: totalPct + '%' }"></div>
          </div>
          <span class="badge">{{ doneCount }}/{{ items.length }} · {{ totalPct }}%</span>
        </div>

        <div v-for="(it, i) in items" :key="it.id" class="list-item">
          <div style="min-width:0">
            <div class="mono" style="word-break:break-all">{{ it.key }}</div>
            <div class="badge">{{ fmtSize(it.file.size) }}</div>
          </div>
          <div style="width:240px; display:flex; align-items:center; gap:10px">
            <div style="flex:1">
              <div class="progress" v-if="it.status === 'uploading' || it.status === 'done'">
                <div class="bar" :style="{ width: it.pct + '%' }"></div>
              </div>
              <div>
                <span v-if="it.status === 'pending'" class="tag">{{ t('upload.statusPending') }}</span>
                <span v-else-if="it.status === 'signing'" class="tag">{{ t('upload.statusSigning') }}</span>
                <span v-else-if="it.status === 'uploading'" class="tag">{{ it.pct }}%</span>
                <span v-else-if="it.status === 'done'" class="tag ok">{{ t('upload.statusDone') }}</span>
                <span v-else class="tag bad">{{ t('upload.statusErr') }}</span>
                <button v-if="it.status === 'done'" class="btn secondary sm" style="margin-left:8px" @click="copyDoneLink(it)">{{ t('upload.copyLink') }}</button>
              </div>
              <div v-if="it.err" class="badge" style="color:var(--danger)">{{ it.err }}</div>
            </div>
            <button
              v-if="!running && it.status !== 'done'"
              class="btn secondary sm"
              :aria-label="t('common.remove')"
              @click="removeItem(i)"
            >✕</button>
          </div>
        </div>
      </template>
    </template>
  </div>
</template>
