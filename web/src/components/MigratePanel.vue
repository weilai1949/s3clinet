<script setup lang="ts">
defineOptions({ name: 'MigratePanel' })

import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api, subscribeMigrateEvents } from '../api'
import { state, currentAccount, toast, selectAccount, requestTab } from '../store'
import { fmtSize } from '../format'
import { t, tf } from '../i18n'
import ModalDialog from './ModalDialog.vue'
import type { BucketItem, ObjectItem } from '../types'

// 大对象列表（listAll 上限 200×1000）走窗口化渲染，避免数十万行直接 v-for 冻结页面。
const ROW_HEIGHT = 38
const OVERSCAN = 12
const scrollEl = ref<HTMLElement | null>(null)
const scrollTop = ref(0)
const viewportH = ref(480)
const windowed = computed(() => {
  const total = objects.value.length
  const start = Math.max(0, Math.floor(scrollTop.value / ROW_HEIGHT) - OVERSCAN)
  const count = Math.ceil(viewportH.value / ROW_HEIGHT) + OVERSCAN * 2
  const end = Math.min(total, start + count)
  return {
    start,
    end,
    items: objects.value.slice(start, end),
    padTop: start * ROW_HEIGHT,
    padBottom: Math.max(0, (total - end) * ROW_HEIGHT),
  }
})
function onListScroll() {
  if (scrollEl.value) scrollTop.value = scrollEl.value.scrollTop
}
function measureViewport() {
  if (scrollEl.value) viewportH.value = scrollEl.value.clientHeight || 480
}
let resizeObs: ResizeObserver | undefined
onMounted(() => {
  measureViewport()
  if (scrollEl.value && typeof ResizeObserver !== 'undefined') {
    resizeObs = new ResizeObserver(measureViewport)
    resizeObs.observe(scrollEl.value)
  }
})
onBeforeUnmount(() => {
  resizeObs?.disconnect()
  // 组件卸载时若仍有进行中的 SSE 订阅，立即断开（避免后台 goroutine 持续推事件）。
  if (activeUnsub) activeUnsub()
  if (activeJobId.value) {
    // 后端 job 不主动取消（用户离开后任务可能仍在 server 端进行；
    // 短期同步进度由 JobRegistry reap 处理）。
  }
})

const sourceBucket = ref('')
const sourcePrefix = ref('')
const targetAccountId = ref('')
const targetBucket = ref('')
const targetPrefix = ref('')
const objects = ref<ObjectItem[]>([])
const selected = ref<Set<string>>(new Set())
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const sourceBuckets = ref<BucketItem[]>([])
const targetBuckets = ref<BucketItem[]>([])
const loadingBuckets = ref(false)

/* ---- 迁移进度 ---- */
const progress = reactive({ done: 0, total: 0 })
const progressPct = computed(() => (progress.total ? Math.round((progress.done / progress.total) * 100) : 0))
const activeJobId = ref('')
// 组件级 SSE 取消器：保证 onBeforeUnmount 一定能断开正在进行的迁移事件流。
let activeUnsub: (() => void) | undefined
const cancelling = ref(false)

/* ---- 迁移结果弹窗 ---- */
const resultDialog = reactive({
  open: false,
  migrated: 0,
  failed: 0,
  failedKeys: [] as string[],
  lastError: '',
})
const resultTargetId = ref('')

const sourceAccount = computed(() => currentAccount())
const targetAccount = computed(() => state.accounts.find((a) => a.id === targetAccountId.value))
const selectedSize = computed(() => objects.value.filter((o) => selected.value.has(o.key)).reduce((s, o) => s + o.size, 0))

async function loadSourceBuckets() {
  const acc = sourceAccount.value
  if (!acc) return
  loadingBuckets.value = true
  try {
    const res = await s3api.listBuckets(acc.id)
    sourceBuckets.value = res.buckets ?? []
  } catch {
    sourceBuckets.value = []
  } finally {
    loadingBuckets.value = false
  }
}

async function loadTargetBuckets() {
  if (!targetAccountId.value) {
    targetBuckets.value = []
    return
  }
  loadingBuckets.value = true
  try {
    const res = await s3api.listBuckets(targetAccountId.value)
    targetBuckets.value = res.buckets ?? []
  } catch {
    targetBuckets.value = []
  } finally {
    loadingBuckets.value = false
  }
}

/** 目标账号默认值：优先另一个账号，其次源账号（同账号迁移=复制到其他桶/前缀）。 */
function ensureTargetAccount() {
  if (targetAccountId.value) return
  if (!state.accounts.length) return
  const srcId = sourceAccount.value?.id
  const other = state.accounts.find((a) => a.id !== srcId)
  targetAccountId.value = other?.id ?? srcId ?? state.accounts[0].id
}

async function loadSourceObjects() {
  const acc = sourceAccount.value
  if (!acc) return
  loading.value = true
  error.value = ''
  try {
    const res = await s3api.listObjects(acc.id, { bucket: sourceBucket.value, prefix: sourcePrefix.value, delimiter: '/', maxKeys: '200' })
    objects.value = res.objects.filter((o) => !o.isDir)
    selected.value = new Set()
  } catch (e) {
    error.value = toErrorMessage(e)
  } finally {
    loading.value = false
  }
}

/** 列出前缀下全部文件（含子目录）：循环分页，delimiter 置空不分目录。 */
const loadingAll = ref(false)
const MAX_ALL_PAGES = 200 // 单页 1000 → 最多 20 万个对象

async function loadAllSourceObjects() {
  const acc = sourceAccount.value
  if (!acc) return
  loadingAll.value = true
  error.value = ''
  try {
    const all: ObjectItem[] = []
    let token = ''
    let guard = 0
    for (;;) {
      const q: Record<string, string> = { bucket: sourceBucket.value, prefix: sourcePrefix.value, maxKeys: '1000' }
      if (token) q.continuationToken = token
      const res = await s3api.listObjects(acc.id, q)
      all.push(...res.objects.filter((o) => !o.isDir))
      if (!res.isTruncated || !res.nextToken) break
      if (++guard >= MAX_ALL_PAGES) {
        toast(tf('migrate.listedCap', { n: all.length }), 'err')
        break
      }
      token = res.nextToken
    }
    objects.value = all
    selected.value = new Set()
    toast(tf('migrate.listedAll', { n: all.length }))
  } catch (e) {
    error.value = toErrorMessage(e)
  } finally {
    loadingAll.value = false
  }
}

function toggle(k: string) {
  const s = new Set(selected.value)
  if (s.has(k)) s.delete(k)
  else s.add(k)
  selected.value = s
}

function selectAll() {
  if (selected.value.size === objects.value.length) selected.value = new Set()
  else selected.value = new Set(objects.value.map((o) => o.key))
}

async function migrate() {
  const src = sourceAccount.value
  if (!src) return
  if (selected.value.size === 0) return
  if (!targetAccountId.value) {
    error.value = t('migrate.selectTarget')
    return
  }
  busy.value = true
  error.value = ''
  progress.done = 0
  progress.total = selected.value.size
  const keys = [...selected.value]
  let unsub: (() => void) | undefined
  let finalStatus = ''
  try {
    const { jobId } = await s3api.migrateAsync({
      sourceAccountId: src.id,
      sourceBucket: sourceBucket.value || undefined,
      sourceKeys: keys,
      targetAccountId: targetAccountId.value,
      targetBucket: targetBucket.value || undefined,
      targetPrefix: targetPrefix.value,
    })
    activeJobId.value = jobId
    activeUnsub = undefined
    await new Promise<void>((resolve, reject) => {
      unsub = subscribeMigrateEvents(
        jobId,
        (p) => {
          progress.done = p.done
          progress.total = p.total
          if (p.status === 'done' || p.status === 'cancelled') {
            finalStatus = p.status
            resolve()
          }
        },
        reject,
      )
      activeUnsub = unsub
    })
    const st = await s3api.migrateJobStatus(jobId)
    const r = st.result ?? { migrated: 0, failed: 0 }
    resultTargetId.value = targetAccountId.value
    resultDialog.open = true
    resultDialog.migrated = r.migrated
    resultDialog.failed = r.failed
    resultDialog.failedKeys = (r.failedKeys ?? []).slice(0, 200)
    resultDialog.lastError = r.lastError ?? ''
    if (finalStatus === 'cancelled' || st.progress.status === 'cancelled') {
      toast(tf('migrate.toastCancelled', { ok: r.migrated, fail: r.failed }), 'err')
    } else if (r.failed) {
      toast(tf('migrate.toastPartial', { ok: r.migrated, fail: r.failed }), 'err')
    } else {
      toast(tf('migrate.toastOk', { n: r.migrated }))
    }
  } catch (e) {
    error.value = toErrorMessage(e)
  } finally {
    unsub?.()
    activeUnsub = undefined
    activeJobId.value = ''
    cancelling.value = false
    busy.value = false
  }
}

async function cancelMigrate() {
  if (!activeJobId.value || cancelling.value) return
  cancelling.value = true
  try {
    await s3api.migrateJobCancel(activeJobId.value)
    toast(t('migrate.cancelRequested'))
  } catch (e) {
    error.value = toErrorMessage(e)
    cancelling.value = false
  }
}

/** 迁移完成 → 去目标账号对象管理查看。 */
function gotoTargetObjects() {
  const target = state.accounts.find((a) => a.id === resultTargetId.value)
  if (target) selectAccount(target.id)
  resultDialog.open = false
  requestTab('objects')
}

watch(() => state.currentAccountId, async () => {
  sourceBucket.value = ''
  targetAccountId.value = ''
  objects.value = []
  selected.value = new Set()
  ensureTargetAccount()
  await loadSourceBuckets()
  await loadTargetBuckets()
  loadSourceObjects()
})

watch(targetAccountId, () => {
  targetBucket.value = ''
  loadTargetBuckets()
})

onMounted(async () => {
  ensureTargetAccount()
  await loadSourceBuckets()
  await loadTargetBuckets()
  loadSourceObjects()
})
</script>

<template>
  <div class="panel">
    <div class="toolbar">
      <h3 style="margin:0">{{ t('migrate.title') }}</h3>
      <span class="spacer" />
      <span class="badge">{{ tf('migrate.sourceAccount', { name: sourceAccount?.name || t('common.noAccount') }) }}</span>
    </div>

    <div v-if="!sourceAccount" class="empty">
      <span class="empty-icon" aria-hidden="true">🗂️</span>
      {{ t('migrate.needAccount') }}
    </div>

    <template v-else>
      <!-- 源：Bucket / 前缀 / 列出 -->
      <div class="toolbar">
        <label class="field">
          {{ t('migrate.sourceBucket') }}
          <select v-model="sourceBucket" :disabled="loadingBuckets" style="min-width:180px">
            <option value="">{{ tf('migrate.defaultBucket', { name: sourceAccount.bucket || 'default' }) }}</option>
            <option v-for="b in sourceBuckets" :key="b.name" :value="b.name">{{ b.name }}</option>
          </select>
        </label>
        <label class="field">
          {{ t('migrate.sourcePrefix') }}
          <input v-model="sourcePrefix" :placeholder="t('migrate.prefixPlaceholder')" @keyup.enter="loadSourceObjects" />
        </label>
        <button class="btn secondary sm" style="align-self:flex-end" :disabled="loading || busy" @click="loadSourceObjects">
          {{ loading ? t('migrate.listing') : t('migrate.listObjects') }}
        </button>
        <button class="btn secondary sm" style="align-self:flex-end" :disabled="loading || loadingAll || busy" @click="loadAllSourceObjects">
          {{ loadingAll ? t('migrate.collecting') : t('migrate.listAll') }}
        </button>
      </div>

      <!-- 目标：账号 / Bucket / 前缀 -->
      <div class="toolbar" style="margin-bottom:14px">
        <label class="field">
          {{ t('migrate.targetAccount') }}
          <select v-model="targetAccountId" style="min-width:180px">
            <option v-for="a in state.accounts" :key="a.id" :value="a.id">{{ a.name }}{{ a.id === sourceAccount.id ? t('migrate.sameAccount') : '' }}</option>
          </select>
        </label>
        <label class="field">
          {{ t('migrate.targetBucket') }}
          <select v-model="targetBucket" :disabled="!targetAccountId || loadingBuckets" style="min-width:180px">
            <option value="">{{ tf('migrate.defaultBucket', { name: targetAccount?.bucket || 'default' }) }}</option>
            <option v-for="b in targetBuckets" :key="b.name" :value="b.name">{{ b.name }}</option>
          </select>
        </label>
        <label class="field">
          {{ t('migrate.targetPrefix') }}
          <input v-model="targetPrefix" :placeholder="t('migrate.targetPrefixPh')" />
        </label>
      </div>

      <!-- 选择与操作 -->
      <div class="toolbar">
        <label><input type="checkbox" :checked="objects.length > 0 && selected.size === objects.length" @change="selectAll" /> {{ t('common.selectAll') }}</label>
        <span class="badge">{{ tf('toolbar.selectedFiles', { n: selected.size, size: fmtSize(selectedSize) }) }}</span>
        <span class="spacer" />
        <button class="btn sm" :disabled="!selected.size || !targetAccountId || busy" @click="migrate">
          {{ busy ? `${t('migrate.running')} ${progress.done}/${progress.total}` : t('migrate.start') }}
        </button>
        <button
          v-if="busy && activeJobId"
          class="btn secondary sm"
          :disabled="cancelling"
          @click="cancelMigrate"
        >
          {{ cancelling ? t('migrate.cancelling') : t('migrate.cancel') }}
        </button>
      </div>

      <!-- 迁移进度条 -->
      <div v-if="busy" class="progress" style="margin:10px 0" role="progressbar" :aria-valuenow="progressPct" aria-valuemin="0" aria-valuemax="100">
        <div class="bar" :style="{ width: progressPct + '%' }" />
      </div>

      <div v-if="error" class="msg err" style="margin:10px 0">{{ error }}</div>

      <div v-if="loading" aria-busy="true" :aria-label="t('migrate.listingAria')">
        <div v-for="i in 4" :key="i" class="skel-row" />
      </div>
      <div v-else-if="objects.length" ref="scrollEl" class="tbl-wrap tbl-virtual" @scroll.passive="onListScroll">
        <table class="tbl">
          <thead><tr><th style="width:30px"></th><th>Key</th><th style="width:100px">{{ t('common.size') }}</th></tr></thead>
          <tbody>
            <tr v-if="windowed.padTop" class="v-spacer" aria-hidden="true">
              <td :colspan="3" :style="{ height: windowed.padTop + 'px' }" />
            </tr>
            <tr v-for="o in windowed.items" :key="o.key" class="v-row" :class="{ selected: selected.has(o.key) }">
              <td><input type="checkbox" :aria-label="tf('objects.selectItem', { name: o.key })" :checked="selected.has(o.key)" @change="toggle(o.key)" /></td>
              <td class="mono">{{ o.key }}</td>
              <td class="muted">{{ fmtSize(o.size) }}</td>
            </tr>
            <tr v-if="windowed.padBottom" class="v-spacer" aria-hidden="true">
              <td :colspan="3" :style="{ height: windowed.padBottom + 'px' }" />
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty">
        <span class="empty-icon" aria-hidden="true">⇄</span>
        {{ t('migrate.emptyHint') }}
      </div>
    </template>

    <!-- 迁移结果弹窗 -->
    <ModalDialog :open="resultDialog.open" :title="t('migrate.resultTitle')" width="min(560px, 100%)" @close="resultDialog.open = false">
      <div class="row" style="margin-bottom:12px">
        <span class="tag ok">{{ tf('migrate.resultOk', { n: resultDialog.migrated }) }}</span>
        <span class="tag" :class="resultDialog.failed ? 'bad' : 'ok'">{{ tf('migrate.resultFail', { n: resultDialog.failed }) }}</span>
        <span class="badge">{{ tf('migrate.resultTotal', { n: resultDialog.migrated + resultDialog.failed }) }}</span>
      </div>
      <div v-if="resultDialog.failed" class="result-fail">
        <div class="badge" style="margin-bottom:6px">{{ t('migrate.failList') }}</div>
        <div class="fail-list">
          <div v-for="k in resultDialog.failedKeys" :key="k" class="mono fail-item">{{ k }}</div>
        </div>
        <div v-if="resultDialog.lastError" class="badge" style="margin-top:6px; color:var(--danger)">{{ tf('migrate.firstError', { msg: resultDialog.lastError }) }}</div>
      </div>
      <div class="row" style="margin-top:16px">
        <button class="btn sm" @click="gotoTargetObjects">{{ t('migrate.gotoTarget') }}</button>
        <button class="btn secondary sm" @click="resultDialog.open = false">{{ t('common.close') }}</button>
      </div>
    </ModalDialog>
  </div>
</template>

<style scoped>
.result-fail { margin-top: 4px; }
.fail-list {
  max-height: 180px; overflow: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius);
  background: var(--panel-2);
  padding: 8px 10px;
}
.fail-item { font-size: 12px; padding: 2px 0; }

/* 虚拟滚动：固定行高 + 上下垫片让滚动条反映真实总高度。 */
.tbl-virtual {
  max-height: 60vh;
  overflow: auto;
}
.tbl-virtual .v-row { height: 38px; }
.tbl-virtual .v-spacer td { padding: 0; border: 0; }
</style>
