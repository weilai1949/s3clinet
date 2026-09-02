<script setup lang="ts">
import { ref } from 'vue'

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

const indexDocument = ref('')
const errorDocument = ref('')
const redirectAllRequestsTo = ref('')

const { loading, saving, save } = useBucketSetting({
  bucket: () => props.bucket,
  onError: (m) => emit('error', m),
  onChanged: () => emit('changed'),
  load: async () => {
    const r = await s3api.getBucketWebsite(props.accountId, props.bucket)
    indexDocument.value = r.indexDocument || ''
    errorDocument.value = r.errorDocument || ''
    redirectAllRequestsTo.value = r.redirectAllRequestsTo || ''
  },
})

async function disable() {
  const ok = await confirmDialog({
    title: t('website.disableTitle'),
    message: tf('website.disableConfirm', { bucket: props.bucket }),
    confirmText: t('common.close'),
    danger: true,
  })
  if (!ok) return
  await save(async () => {
    await s3api.deleteBucketWebsite(props.accountId, props.bucket)
  }, t('website.toastDisabled'))
}

async function saveWebsite() {
  await save(async () => {
    if (!redirectAllRequestsTo.value && !indexDocument.value) {
      throw new Error(t('website.needIndexOrRedirect'))
    }
    await s3api.putBucketWebsite(props.accountId, {
      bucket: props.bucket,
      indexDocument: indexDocument.value,
      errorDocument: errorDocument.value,
      redirectAllRequestsTo: redirectAllRequestsTo.value,
    })
  }, t('website.toastSaved'))
}
</script>

<template>
  <div v-if="loading" class="empty" style="padding:20px">{{ t('website.loading') }}</div>
  <div v-else>
    <label class="field">
      {{ t('website.indexDoc') }}
      <input v-model="indexDocument" class="mono" placeholder="index.html" />
    </label>
    <label class="field">
      {{ t('website.errorDoc') }}
      <input v-model="errorDocument" class="mono" :placeholder="t('website.errorDocPh')" />
    </label>
    <label class="field">
      {{ t('website.redirectAll') }}
      <input v-model="redirectAllRequestsTo" class="mono" :placeholder="t('website.redirectPh')" />
    </label>
    <div class="row" style="margin-top:14px">
      <button class="btn sm" :disabled="saving" @click="saveWebsite">{{ saving ? t('common.saving') : t('common.save') }}</button>
      <button class="btn danger sm" :disabled="saving" @click="disable">{{ t('website.disableBtn') }}</button>
    </div>
    <div class="badge" style="margin-top:10px; color:var(--muted)">{{ t('website.hint') }}</div>
  </div>
</template>
