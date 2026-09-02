<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { toast } from '../store'
import { confirmDialog } from '../confirm'
import { fmtDate, fmtSize } from '../format'
import { t, tf } from '../i18n'
import ModalDialog from './ModalDialog.vue'
import CompareDialog from './CompareDialog.vue'
import type { CompareVersion } from './CompareDialog.vue'

interface VersionRow {
  key: string
  versionId: string
  isLatest: boolean
  lastModified: string
  size: number
  etag: string
  storageClass: string
  isDeleteMarker: boolean
}

const props = defineProps<{
  open: boolean
  accountId: string
  bucket: string
  objectKey: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'error', msg: string): void
}>()

const rows = ref<VersionRow[]>([])
const loading = ref(false)
const busy = ref(false)
const compareOpen = ref(false)

/** 可参与内容比较的版本（排除删除标记）。 */
const contentVersions = computed<CompareVersion[]>(() =>
  rows.value
    .filter((v) => !v.isDeleteMarker)
    .map((v) => ({
      versionId: v.versionId,
      size: v.size,
      etag: v.etag,
      lastModified: v.lastModified,
      storageClass: v.storageClass,
      isLatest: v.isLatest,
    })),
)

async function load() {
  rows.value = []
  loading.value = true
  try {
    const r = await s3api.listVersions(props.accountId, { bucket: props.bucket, prefix: props.objectKey })
    const merged: VersionRow[] = []
    for (const m of r.deleteMarkers ?? []) {
      if (m.key !== props.objectKey) continue
      merged.push({ key: m.key, versionId: m.versionId, isLatest: m.isLatest, lastModified: m.lastModified, size: 0, etag: '', storageClass: '', isDeleteMarker: true })
    }
    for (const v of r.versions ?? []) {
      if (v.key !== props.objectKey) continue
      merged.push({ key: v.key, versionId: v.versionId, isLatest: v.isLatest, lastModified: v.lastModified, size: v.size, etag: v.etag, storageClass: v.storageClass ?? '', isDeleteMarker: false })
    }
    merged.sort((a, b) => (b.lastModified || '').localeCompare(a.lastModified || ''))
    rows.value = merged
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    loading.value = false
  }
}

watch(() => props.open, (o) => {
  if (o) load()
})

async function restore(v: VersionRow) {
  if (busy.value || v.isDeleteMarker) return
  const ok = await confirmDialog({
    title: t('versions.restoreTitle'),
    message: tf('versions.restoreConfirm', { key: props.objectKey, versionId: v.versionId }),
    confirmText: t('versions.restore'),
    danger: false,
  })
  if (!ok) return
  busy.value = true
  try {
    const r = await s3api.restoreObjectVersion(props.accountId, { bucket: props.bucket, key: props.objectKey, versionId: v.versionId })
    toast(tf('versions.restoreOk', { key: props.objectKey, versionId: r.versionId }))
    await load()
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    busy.value = false
  }
}

/** 一键还原删除标记（撤销删除）：移除该删除标记版本，对象回到被删除前的状态。 */
async function restoreDeleteMarker(v: VersionRow) {
  if (busy.value || !v.isDeleteMarker) return
  const ok = await confirmDialog({
    title: t('versions.restoreDmTitle'),
    message: tf('versions.restoreDmConfirm', { key: props.objectKey }),
    confirmText: t('trash.restore'),
    danger: false,
  })
  if (!ok) return
  busy.value = true
  try {
    await s3api.restoreDeleteMarker(props.accountId, { bucket: props.bucket, key: props.objectKey, versionId: v.versionId })
    toast(tf('versions.restoreDmOk', { key: props.objectKey }))
    await load()
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    busy.value = false
  }
}

async function removeVersion(v: VersionRow) {
  if (busy.value) return
  const ok = await confirmDialog({
    title: t('versions.deleteTitle'),
    message: tf('versions.deleteConfirm', { key: props.objectKey, versionId: v.versionId }),
    confirmText: t('common.delete'),
    danger: true,
  })
  if (!ok) return
  busy.value = true
  try {
    await s3api.deleteObjectVersion(props.accountId, { bucket: props.bucket, key: props.objectKey, versionId: v.versionId })
    toast(tf('versions.deleteOk', { key: props.objectKey, versionId: v.versionId }))
    await load()
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :open="open" :title="tf('versions.title', { key: objectKey })" width="min(820px, 100%)" @close="emit('close')">
    <div style="margin-bottom:10px" class="mono badge" v-if="objectKey">{{ tf('versions.object', { key: objectKey }) }}</div>
    <div v-if="loading" class="empty" style="padding:22px">{{ t('versions.loading') }}</div>
    <div v-else-if="!rows.length" class="empty" style="padding:22px">
      {{ t('versions.empty') }}
    </div>
    <template v-else>
      <div class="row" style="margin-bottom:10px">
        <button class="btn secondary sm" :disabled="contentVersions.length < 2" @click="compareOpen = true">
          {{ contentVersions.length < 2 ? t('versions.compareNeed') : t('versions.compare') }}
        </button>
        <span class="badge" v-if="rows.some((v) => v.isDeleteMarker)">{{ t('versions.hasDeleteMarker') }}</span>
      </div>
      <table class="tbl">
        <thead><tr><th style="width:96px">{{ t('versions.colType') }}</th><th>{{ t('versions.colVersionId') }}</th><th style="width:140px">{{ t('versions.colMtime') }}</th><th style="width:70px">{{ t('versions.colSize') }}</th><th style="width:96px">{{ t('versions.colStorage') }}</th><th style="width:190px">{{ t('versions.colActions') }}</th></tr></thead>
        <tbody>
          <tr v-for="v in rows" :key="v.versionId || 'del-' + v.lastModified">
            <td>
              <span v-if="v.isDeleteMarker" class="badge" style="color:#d64545">{{ t('versions.typeDeleteMarker') }}</span>
              <span v-else-if="v.isLatest" class="badge" style="color:var(--primary)">{{ t('versions.typeLatest') }}</span>
              <span v-else class="badge">{{ t('versions.typeHistory') }}</span>
            </td>
            <td class="mono" style="word-break:break-all">{{ v.versionId || 'null' }}</td>
            <td>{{ fmtDate(v.lastModified) }}</td>
            <td>{{ v.isDeleteMarker ? '—' : fmtSize(v.size) }}</td>
            <td>{{ v.storageClass || '—' }}</td>
            <td>
              <template v-if="v.isDeleteMarker">
                <button class="btn secondary sm" :disabled="busy" style="color:var(--primary)" @click="restoreDeleteMarker(v)">{{ t('trash.restore') }}</button>
                <button class="btn danger sm" :disabled="busy" style="margin-left:6px" @click="removeVersion(v)">{{ t('versions.deleteMarker') }}</button>
              </template>
              <template v-else>
                <button class="btn secondary sm" :disabled="busy" style="margin-right:4px" @click="restore(v)">{{ t('versions.restore') }}</button>
                <button class="btn danger sm" :disabled="busy" @click="removeVersion(v)">{{ t('common.delete') }}</button>
              </template>
            </td>
          </tr>
        </tbody>
      </table>
    </template>
    <div class="row" style="margin-top:14px">
      <button class="btn secondary sm" @click="emit('close')">{{ t('common.close') }}</button>
    </div>

    <CompareDialog
      :open="compareOpen"
      :account-id="accountId"
      :bucket="bucket"
      :object-key="objectKey"
      :versions="contentVersions"
      @close="compareOpen = false"
      @error="emit('error', $event)"
    />
  </ModalDialog>
</template>
