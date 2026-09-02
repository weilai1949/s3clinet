<script setup lang="ts">
import { ref } from 'vue'

import { s3api } from '../api'
import { t } from '../i18n'
import { useBucketSetting } from '../composables/useBucketSetting'

const props = defineProps<{
  accountId: string
  bucket: string
}>()

const emit = defineEmits<{
  (e: 'error', msg: string): void
  (e: 'changed'): void
}>()

const tags = ref<{ key: string; value: string }[]>([])

const { loading, saving, save } = useBucketSetting({
  bucket: () => props.bucket,
  onError: (m) => emit('error', m),
  onChanged: () => emit('changed'),
  load: async () => {
    const r = await s3api.getBucketTags(props.accountId, props.bucket)
    tags.value = (r.tags ?? []).map((tag) => ({ key: tag.key, value: tag.value }))
  },
})

function addRow() {
  tags.value.push({ key: '', value: '' })
}

function removeRow(i: number) {
  tags.value.splice(i, 1)
}

async function clear() {
  await save(async () => {
    await s3api.deleteBucketTags(props.accountId, props.bucket)
    tags.value = []
  }, t('bucketTags.toastCleared'))
}

async function saveTags() {
  await save(async () => {
    const valid = tags.value.filter((row) => row.key.trim())
    for (const row of valid) {
      if (!row.key.trim()) throw new Error(t('bucketTags.errEmptyKey'))
    }
    await s3api.putBucketTags(props.accountId, {
      bucket: props.bucket,
      tags: valid.map((row) => ({ key: row.key.trim(), value: row.value })),
    })
  }, t('bucketTags.toastSaved'))
}
</script>

<template>
  <div v-if="loading" class="empty" style="padding:20px">{{ t('bucketTags.loading') }}</div>
  <div v-else>
    <table class="tbl">
      <thead><tr><th style="width:40%">{{ t('bucketTags.colKey') }}</th><th>{{ t('bucketTags.colValue') }}</th><th style="width:60px"></th></tr></thead>
      <tbody>
        <tr v-for="(row, i) in tags" :key="i">
          <td><input v-model="row.key" class="mono" placeholder="key" /></td>
          <td><input v-model="row.value" class="mono" placeholder="value" /></td>
          <td><button class="btn secondary sm" @click="removeRow(i)">{{ t('common.remove') }}</button></td>
        </tr>
      </tbody>
    </table>
    <div class="row" style="margin-top:12px">
      <button class="btn secondary sm" @click="addRow">{{ t('bucketTags.add') }}</button>
      <button class="btn sm" :disabled="saving" @click="saveTags">{{ saving ? t('common.saving') : t('common.save') }}</button>
      <button class="btn danger sm" :disabled="saving" @click="clear">{{ t('bucketTags.clearAll') }}</button>
    </div>
  </div>
</template>
