<script setup lang="ts">
import { computed, ref } from 'vue'

import { s3api } from '../api'
import { confirmDialog } from '../confirm'
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

const configured = ref(false)
const algorithm = ref('AES256')
const kmsKeyId = ref('')
const bucketKeyEnabled = ref(true)

const algorithms = computed(() => [
  { value: 'AES256', label: 'SSE-S3 (AES256)' },
  { value: 'aws:kms', label: 'SSE-KMS (aws:kms)' },
  { value: 'aws:kms:dsse', label: t('encryption.algoDsse') },
])

const { loading, saving, save } = useBucketSetting({
  bucket: () => props.bucket,
  onError: (m) => emit('error', m),
  onChanged: () => emit('changed'),
  load: async () => {
    const r = await s3api.getBucketEncryption(props.accountId, props.bucket)
    configured.value = r.configured
    algorithm.value = r.algorithm || 'AES256'
    kmsKeyId.value = r.kmsKeyId || ''
    bucketKeyEnabled.value = r.bucketKeyEnabled
  },
})

async function disable() {
  const ok = await confirmDialog({
    title: t('encryption.disableTitle'),
    message: tf('encryption.disableConfirm', { bucket: props.bucket }),
    confirmText: t('common.close'),
    danger: true,
  })
  if (!ok) return
  await save(async () => {
    await s3api.deleteBucketEncryption(props.accountId, props.bucket)
  }, t('encryption.toastDisabled'))
}

async function saveEncryption() {
  await save(async () => {
    await s3api.putBucketEncryption(props.accountId, {
      bucket: props.bucket,
      algorithm: algorithm.value,
      kmsKeyId: kmsKeyId.value,
      bucketKeyEnabled: bucketKeyEnabled.value,
    })
  }, t('encryption.toastSaved'))
}
</script>

<template>
  <div v-if="loading" class="empty" style="padding:20px">{{ t('encryption.loading') }}</div>
  <div v-else>
    <label class="field">
      {{ t('encryption.algorithm') }}
      <select v-model="algorithm">
        <option v-for="a in algorithms" :key="a.value" :value="a.value">{{ a.label }}</option>
      </select>
    </label>
    <label class="field" v-if="algorithm.startsWith('aws:kms')">
      {{ t('encryption.kmsKey') }}
      <input v-model="kmsKeyId" class="mono" :placeholder="t('encryption.kmsKeyPh')" />
    </label>
    <label class="field">
      <label class="check-row"><input type="checkbox" v-model="bucketKeyEnabled" /> {{ t('encryption.bucketKey') }}</label>
    </label>
    <div class="row" style="margin-top:14px">
      <button class="btn sm" :disabled="saving" @click="saveEncryption">{{ saving ? t('common.saving') : t('common.save') }}</button>
      <button class="btn danger sm" :disabled="saving" @click="disable">{{ t('encryption.disableBtn') }}</button>
    </div>
    <div class="badge" style="margin-top:10px; color:var(--muted)">{{ t('encryption.hint') }}</div>
  </div>
</template>
