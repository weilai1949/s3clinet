<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { state, selectAccount, rememberedAccountId } from '../store'
import { s3api } from '../api'
import { toast } from '../store'
import { confirmDialog } from '../confirm'
import { fmtDate } from '../format'
import { t, tf } from '../i18n'
import type { BucketItem } from '../types'

interface TrashMarker {
  key: string
  versionId: string
  isLatest: boolean
  lastModified: string
}

const accSel = ref('')
const bucketSel = ref('')
const buckets = ref<BucketItem[]>([])
const markers = ref<TrashMarker[]>([])
const loading = ref(false)
const loadingMore = ref(false)
const error = ref('')
const isTruncated = ref(false)
const nextKeyMarker = ref('')
const nextVersionIdMarker = ref('')
const busy = ref(false)

const account = () => state.accounts.find((a) => a.id === accSel.value)

async function loadBuckets() {
  if (!accSel.value) {
    buckets.value = []
    return
  }
  try {
    const r = await s3api.listBuckets(accSel.value)
    buckets.value = r.buckets
    if (!bucketSel.value || !r.buckets.some((b) => b.name === bucketSel.value)) bucketSel.value = r.buckets[0]?.name ?? ''
  } catch (e) {
    error.value = toErrorMessage(e)
  }
}

async function loadMarkers(reset = true) {
  if (!accSel.value || !bucketSel.value) return
  if (reset) {
    markers.value = []
    nextKeyMarker.value = ''
    nextVersionIdMarker.value = ''
    isTruncated.value = false
  }
  loading.value = reset
  loadingMore.value = !reset
  try {
    // 空页自动向后翻少量页（版本列表可能夹杂非删除标记）；避免一次拉满 51 页撑爆 DOM
    let guard = 0
    const emptyPageSkip = reset ? 2 : 3
    for (;;) {
      const r = await s3api.listTrash(accSel.value, {
        bucket: bucketSel.value,
        keyMarker: nextKeyMarker.value,
        versionIdMarker: nextVersionIdMarker.value,
        maxKeys: 1000,
      })
      markers.value.push(...r.deleteMarkers)
      nextKeyMarker.value = r.nextKeyMarker
      nextVersionIdMarker.value = r.nextVersionIdMarker
      isTruncated.value = r.isTruncated
      if (r.deleteMarkers.length || !r.isTruncated || guard++ >= emptyPageSkip) break
    }
  } catch (e) {
    error.value = toErrorMessage(e)
  } finally {
    loading.value = false
    loadingMore.value = false
  }
}

onMounted(() => {
  const remembered = rememberedAccountId()
  if (state.accounts.some((a) => a.id === remembered)) accSel.value = remembered
  else accSel.value = state.currentAccountId && state.accounts.some((a) => a.id === state.currentAccountId) ? state.currentAccountId : (state.accounts[0]?.id ?? '')
  selectAccount(accSel.value)
  loadBuckets()
})

watch(accSel, (v) => {
  selectAccount(v)
  bucketSel.value = ''
  markers.value = []
  loadBuckets()
})

watch(bucketSel, (b) => {
  if (b) loadMarkers(true)
  else markers.value = []
})

async function restore(m: TrashMarker) {
  if (busy.value) return
  const ok = await confirmDialog({
    title: t('trash.restoreTitle'),
    message: tf('trash.restoreConfirm', { key: m.key }),
    confirmText: t('trash.restore'),
    danger: false,
  })
  if (!ok) return
  busy.value = true
  try {
    await s3api.restoreDeleteMarker(accSel.value, { bucket: bucketSel.value, key: m.key, versionId: m.versionId })
    toast(tf('trash.restored', { key: m.key }))
    markers.value = markers.value.filter((x) => !(x.key === m.key && x.versionId === m.versionId))
  } catch (e) {
    error.value = toErrorMessage(e)
  } finally {
    busy.value = false
  }
}

async function purge(m: TrashMarker) {
  if (busy.value) return
  const ok = await confirmDialog({
    title: t('trash.purgeTitle'),
    message: tf('trash.purgeConfirm', { key: m.key }),
    confirmText: t('trash.purge'),
    danger: true,
  })
  if (!ok) return
  busy.value = true
  try {
    const r = await s3api.purgeTrashObject(accSel.value, { bucket: bucketSel.value, key: m.key })
    toast(tf('trash.purged', { key: m.key, n: r.deleted }))
    markers.value = markers.value.filter((x) => x.key !== m.key)
  } catch (e) {
    error.value = toErrorMessage(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="panel">
    <div class="toolbar">
      <h3 style="margin:0">{{ t('trash.title') }}</h3>
      <span class="spacer" />
      <span class="badge">{{ t('trash.account') }}</span>
      <select v-model="accSel" class="acc-select" :title="tf('trash.switchAccount', { n: state.accounts.length })">
        <option v-if="!state.accounts.length" value="">{{ t('trash.noAccounts') }}</option>
        <option v-for="a in state.accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
      <span class="badge">{{ t('trash.bucket') }}</span>
      <select v-model="bucketSel" class="acc-select" :title="t('trash.switchBucket')">
        <option v-if="!buckets.length" value="">{{ t('trash.noBuckets') }}</option>
        <option v-for="b in buckets" :key="b.name" :value="b.name">{{ b.name }}</option>
      </select>
      <button class="btn secondary sm" :disabled="!bucketSel || loading" @click="loadMarkers(true)">{{ t('common.refresh') }}</button>
    </div>

    <div v-if="!account()" class="empty">
      <span class="empty-icon" aria-hidden="true">🗑️</span>
      {{ t('trash.needAccount') }}
    </div>

    <div v-else-if="error" class="msg err" style="margin-bottom:10px">
      <span style="flex:1">{{ error }}</span>
      <button class="link" style="flex:none" @click="loadMarkers(true)">{{ t('common.retry') }}</button>
    </div>

    <template v-else-if="bucketSel">
      <div v-if="loading" class="empty" style="padding:20px">{{ t('trash.loading') }}</div>
      <div v-else-if="!markers.length" class="empty">
        <span class="empty-icon" aria-hidden="true">🗑️</span>
        {{ t('trash.emptyHint') }}
      </div>
      <table v-else class="tbl">
        <thead><tr><th>{{ t('trash.colKey') }}</th><th style="width:120px">{{ t('trash.colVersion') }}</th><th style="width:160px">{{ t('trash.colDeletedAt') }}</th><th style="width:180px; text-align:right">{{ t('trash.colActions') }}</th></tr></thead>
        <tbody>
          <tr v-for="m in markers" :key="m.key + ':' + m.versionId">
            <td class="mono" style="word-break:break-all">{{ m.key }}</td>
            <td class="mono" style="word-break:break-all">{{ m.versionId }}</td>
            <td class="muted">{{ fmtDate(m.lastModified) }}</td>
            <td>
              <div class="actions" style="justify-content:flex-end; gap:6px">
                <button class="btn secondary sm" :disabled="busy" style="color:var(--primary)" @click="restore(m)">{{ t('trash.restore') }}</button>
                <button class="btn danger sm" :disabled="busy" @click="purge(m)">{{ t('trash.purge') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="toolbar" style="margin-top:12px; margin-bottom:0">
        <button class="btn secondary sm" :disabled="!isTruncated || loadingMore" @click="loadMarkers(false)">
          {{ loadingMore ? t('common.loading') : t('common.more') }}
        </button>
        <span class="badge">{{ isTruncated ? t('trash.hasMore') : t('trash.endReached') }}</span>
        <span class="badge" style="margin-left:auto">{{ tf('trash.shown', { n: markers.length }) }}</span>
      </div>
    </template>

    <div v-else class="empty">
      <span class="empty-icon" aria-hidden="true">🪣</span>
      {{ t('trash.pickBucket') }}
    </div>
  </div>
</template>

<style scoped>
.acc-select { max-width: 220px; padding: 5px 10px; font-size: 13px; }
.actions { display: flex; }
</style>
