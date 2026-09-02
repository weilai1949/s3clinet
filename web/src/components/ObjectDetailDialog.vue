<script setup lang="ts">
import { fmtDate, fmtSize } from '../format'
import { t } from '../i18n'
import ModalDialog from './ModalDialog.vue'
import { storageClassLabel } from '../storageClass'
import type { ObjectMeta } from '../types'

defineProps<{
  open: boolean
  detail: ObjectMeta | null
  accountId: string
  bucket: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'editHeaders', key: string): void
  (e: 'openAcl', key: string): void
  (e: 'openTags', key: string): void
  (e: 'openVersions', key: string): void
  (e: 'openStorageClass', key: string): void
  (e: 'error', msg: string): void
}>()
</script>

<template>
  <ModalDialog :open="open" :title="t('detail.title')" width="min(640px, 100%)" @close="emit('close')">
    <template v-if="detail">
      <table class="tbl detail-tbl">
        <tbody>
          <tr><th>{{ t('detail.key') }}</th><td class="mono" style="word-break:break-all">{{ detail.key }}</td></tr>
          <tr><th>{{ t('common.size') }}</th><td>{{ fmtSize(detail.size) }}</td></tr>
          <tr><th>{{ t('detail.mtime') }}</th><td>{{ fmtDate(detail.lastModified) }}</td></tr>
          <tr><th>{{ t('detail.contentType') }}</th><td class="mono">{{ detail.contentType || '—' }}</td></tr>
          <tr><th>{{ t('objects.storageClass') }}</th><td>
            {{ detail.storageClass ? storageClassLabel(detail.storageClass) : '—' }}
            <span v-if="detail.storageClass" class="mono badge" style="margin-left:6px">{{ detail.storageClass }}</span>
            <button class="btn secondary sm" style="margin-left:10px" @click="emit('openStorageClass', detail.key)">{{ t('detail.switch') }}</button>
          </td></tr>
          <tr><th>{{ t('detail.etag') }}</th><td class="mono" style="word-break:break-all">{{ detail.etag || '—' }}</td></tr>
          <tr v-if="detail.metadata && Object.keys(detail.metadata).length">
            <th>{{ t('detail.metadata') }}</th>
            <td>
              <div v-for="(v, k) in detail.metadata" :key="k" class="mono">{{ k }}: {{ v }}</div>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="row" style="margin-top:12px">
        <button class="btn sm" @click="emit('editHeaders', detail.key)">{{ t('detail.editHeaders') }}</button>
        <button class="btn sm" @click="emit('openAcl', detail.key)">{{ t('detail.acl') }}</button>
        <button class="btn sm" @click="emit('openTags', detail.key)">{{ t('detail.tags') }}</button>
        <button class="btn sm" @click="emit('openVersions', detail.key)">{{ t('detail.versions') }}</button>
        <span class="badge">{{ t('detail.metaBadge') }}</span>
      </div>
    </template>
  </ModalDialog>
</template>

<style scoped>
.detail-tbl th { width: 120px; background: none; border-bottom: 1px solid var(--border); }
.detail-tbl th, .detail-tbl td { padding: 7px 10px; }
</style>
