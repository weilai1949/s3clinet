<script setup lang="ts">
import { ref } from 'vue'

import { s3api } from '../api'
import { toast } from '../store'
import { confirmDialog } from '../confirm'
import { fmtDate } from '../format'
import { t, tf } from '../i18n'
import { useBucketSetting } from '../composables/useBucketSetting'

const props = defineProps<{
  accountId: string
  bucket: string
}>()

const emit = defineEmits<{
  (e: 'error', msg: string): void
  (e: 'changed'): void
}>()

const info = ref<{ region: string; createdAt: string; versioning: '' | 'Enabled' | 'Suspended' }>({ region: '', createdAt: '', versioning: '' })
const toggling = ref(false)

const { loading, reload } = useBucketSetting({
  bucket: () => props.bucket,
  onError: (m) => emit('error', m),
  onChanged: () => emit('changed'),
  load: async () => {
    const r = await s3api.getBucketInfo(props.accountId, props.bucket)
    info.value = { region: r.region, createdAt: r.createdAt, versioning: r.versioning }
  },
})

async function toggleVersioning() {
  if (toggling.value) return
  const target = info.value.versioning === 'Enabled' ? 'Suspended' : 'Enabled'
  const ok = await confirmDialog({
    title: target === 'Enabled' ? t('overview.enableTitle') : t('overview.suspendTitle'),
    message: target === 'Enabled'
      ? tf('overview.enableConfirm', { bucket: props.bucket })
      : tf('overview.suspendConfirm', { bucket: props.bucket }),
    confirmText: target === 'Enabled' ? t('overview.enable') : t('overview.suspend'),
    danger: target === 'Suspended',
  })
  if (!ok) return
  toggling.value = true
  try {
    await s3api.putBucketVersioning(props.accountId, { bucket: props.bucket, status: target })
    toast(target === 'Enabled' ? t('overview.toastEnabled') : t('overview.toastSuspended'))
    await reload()
    emit('changed')
  } catch (err: unknown) {
    emit('error', err instanceof Error ? err.message : String(err))
  } finally {
    toggling.value = false
  }
}
</script>

<template>
  <div v-if="loading" class="empty" style="padding:20px">{{ t('overview.loading') }}</div>
  <div v-else>
    <table class="tbl detail-tbl">
      <tbody>
        <tr><th>{{ t('overview.bucket') }}</th><td class="mono" style="word-break:break-all">{{ bucket }}</td></tr>
        <tr><th>{{ t('overview.region') }}</th><td>{{ info.region || '—' }}</td></tr>
        <tr><th>{{ t('overview.createdAt') }}</th><td>{{ info.createdAt ? fmtDate(info.createdAt) : '—' }}</td></tr>
        <tr><th>{{ t('overview.versioning') }}</th><td>
          <span class="badge" :style="info.versioning === 'Enabled' ? 'color:var(--primary)' : ''">{{ info.versioning || t('overview.versioningOff') }}</span>
          <button class="btn secondary sm" style="margin-left:10px" :disabled="toggling" @click="toggleVersioning">
            {{ info.versioning === 'Enabled' ? t('overview.suspend') : t('overview.enable') }}
          </button>
        </td></tr>
      </tbody>
    </table>
    <div class="badge" style="margin-top:10px; color:var(--muted)">{{ t('overview.hint') }}</div>
  </div>
</template>

<style scoped>
.detail-tbl th { width: 110px; background: none; border-bottom: 1px solid var(--border); }
.detail-tbl th, .detail-tbl td { padding: 7px 10px; }
</style>
