<script setup lang="ts">
import { ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { t, tf } from '../i18n'
import { toast } from '../store'
import { fmtDate } from '../format'
import ModalDialog from './ModalDialog.vue'
import type { BucketInfo } from '../types'

const props = defineProps<{
  open: boolean
  accountId: string
  bucket: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'error', msg: string): void
}>()

const info = ref<BucketInfo | null>(null)
const loading = ref(false)
const saving = ref(false)

watch(() => props.open, async (o) => {
  if (!o) return
  info.value = null
  loading.value = true
  try {
    info.value = await s3api.getBucketInfo(props.accountId, props.bucket)
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    loading.value = false
  }
})

function versioningLabel(v: string) {
  if (v === 'Enabled') return t('buckets.verEnabled')
  if (v === 'Suspended') return t('buckets.verSuspended')
  return t('buckets.verUnset')
}

async function toggleVersioning() {
  if (!info.value || saving.value) return
  const target: 'Enabled' | 'Suspended' = info.value.versioning === 'Enabled' ? 'Suspended' : 'Enabled'
  saving.value = true
  try {
    await s3api.putBucketVersioning(props.accountId, { bucket: props.bucket, status: target })
    toast(target === 'Enabled'
      ? tf('buckets.toastVerEnabled', { name: props.bucket })
      : tf('buckets.toastVerSuspended', { name: props.bucket }))
    info.value = await s3api.getBucketInfo(props.accountId, props.bucket)
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ModalDialog :open="open" :title="t('toolbar.bucketProps')" width="min(460px, 100%)" @close="emit('close')">
    <div v-if="loading" class="empty" style="padding:22px">{{ t('overview.loading') }}</div>
    <template v-else-if="info">
      <table class="tbl detail-tbl">
        <tbody>
          <tr><th>{{ t('overview.bucket') }}</th><td class="mono">{{ info.bucket }}</td></tr>
          <tr><th>{{ t('overview.region') }}</th><td class="mono">{{ info.region || '—' }}</td></tr>
          <tr><th>{{ t('overview.createdAt') }}</th><td>{{ info.createdAt ? fmtDate(info.createdAt) : '—' }}</td></tr>
          <tr>
            <th>{{ t('overview.versioning') }}</th>
            <td>
              <span class="badge">{{ versioningLabel(info.versioning) }}</span>
              <button class="btn secondary sm" style="margin-left:8px" :disabled="saving" @click="toggleVersioning">
                {{ saving ? t('buckets.processing') : info.versioning === 'Enabled' ? t('overview.suspend') : t('overview.enable') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="row" style="margin-top:14px">
        <button class="btn secondary sm" @click="emit('close')">{{ t('common.close') }}</button>
      </div>
    </template>
  </ModalDialog>
</template>

<style scoped>
.detail-tbl th { width: 120px; background: none; border-bottom: 1px solid var(--border); }
.detail-tbl th, .detail-tbl td { padding: 7px 10px; }
</style>
