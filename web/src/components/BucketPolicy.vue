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

const configured = ref(false)
const policy = ref('')

const { loading, saving, save } = useBucketSetting({
  bucket: () => props.bucket,
  onError: (m) => emit('error', m),
  onChanged: () => emit('changed'),
  load: async () => {
    const r = await s3api.getBucketPolicy(props.accountId, props.bucket)
    configured.value = r.configured
    policy.value = r.policy || ''
  },
})

function validate(): string | null {
  try {
    JSON.parse(policy.value || '{}')
    return null
  } catch {
    return t('policy.invalidJson')
  }
}

async function remove() {
  const ok = await confirmDialog({
    title: t('policy.removeTitle'),
    message: tf('policy.removeConfirm', { bucket: props.bucket }),
    confirmText: t('common.remove'),
    danger: true,
  })
  if (!ok) return
  await save(async () => {
    await s3api.deleteBucketPolicy(props.accountId, props.bucket)
  }, t('policy.toastRemoved'))
}

async function savePolicy() {
  await save(async () => {
    const v = validate()
    if (v) throw new Error(v)
    if (!policy.value.trim()) throw new Error(t('policy.emptyErr'))
    await s3api.putBucketPolicy(props.accountId, { bucket: props.bucket, policy: policy.value })
  }, t('policy.toastSaved'))
}
</script>

<template>
  <div v-if="loading" class="empty" style="padding:20px">{{ t('policy.loading') }}</div>
  <div v-else>
    <div class="badge" style="color:var(--muted)">{{ t('policy.hint') }}</div>
    <textarea v-model="policy" class="mono policy-area" spellcheck="false" placeholder='{"Version":"2012-10-17","Statement":[...]}'></textarea>
    <div class="row" style="margin-top:12px">
      <button class="btn sm" :disabled="saving" @click="savePolicy">{{ saving ? t('common.saving') : t('common.save') }}</button>
      <button class="btn danger sm" :disabled="saving" @click="remove">{{ t('policy.removeBtn') }}</button>
    </div>
  </div>
</template>

<style scoped>
.policy-area {
  width: 100%; min-height: 240px; margin-top: 10px;
  padding: 10px; border: 1px solid var(--border); border-radius: var(--radius);
  font-family: var(--font-mono); font-size: 12px; line-height: 1.5;
  background: var(--panel-2); color: var(--text); resize: vertical;
}
</style>
