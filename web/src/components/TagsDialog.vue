<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { t, tf } from '../i18n'
import { toast } from '../store'
import ModalDialog from './ModalDialog.vue'

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

const rows = reactive<{ key: string; value: string }[]>([])
const loading = ref(false)
const saving = ref(false)

watch(() => props.open, async (o) => {
  if (!o) return
  rows.splice(0, rows.length)
  loading.value = true
  try {
    const r = await s3api.getObjectTags(props.accountId, { bucket: props.bucket, key: props.objectKey })
    for (const tag of r.tags ?? []) rows.push({ key: tag.key, value: tag.value })
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    loading.value = false
  }
})

function addTagRow() {
  rows.push({ key: '', value: '' })
}

function removeTagRow(i: number) {
  rows.splice(i, 1)
}

async function submitTags() {
  if (saving.value) return
  const tags = rows.filter((row) => row.key.trim()).map((row) => ({ key: row.key.trim(), value: row.value }))
  saving.value = true
  try {
    await s3api.putObjectTags(props.accountId, { bucket: props.bucket, key: props.objectKey, tags })
    toast(tags.length
      ? tf('tags.toastUpdated', { key: props.objectKey })
      : tf('tags.toastCleared', { key: props.objectKey }))
    emit('close')
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ModalDialog :open="open" :title="t('tags.title')" width="min(540px, 100%)" @close="emit('close')">
    <div class="grid" style="grid-template-columns:1fr">
      <label class="field">{{ t('tags.object') }} <span class="mono badge">{{ objectKey }}</span></label>
      <div class="field">
        <div class="row" style="justify-content:space-between; margin-bottom:4px">
          <span>{{ t('tags.hint') }}</span>
          <button class="btn secondary sm" @click="addTagRow">{{ t('tags.add') }}</button>
        </div>
        <div v-if="loading" class="empty" style="padding:14px">{{ t('tags.loading') }}</div>
        <div v-else-if="!rows.length" class="empty" style="padding:14px">{{ t('tags.empty') }}</div>
        <div v-for="(row, i) in rows" :key="i" class="row" style="margin-top:6px">
          <input v-model="row.key" :placeholder="t('tags.keyPh')" style="flex:1" autocomplete="off" spellcheck="false" />
          <input v-model="row.value" :placeholder="t('tags.valuePh')" style="flex:1" autocomplete="off" spellcheck="false" />
          <button class="btn secondary sm" :aria-label="t('common.delete')" @click="removeTagRow(i)">✕</button>
        </div>
      </div>
    </div>
    <div class="row" style="margin-top:16px">
      <button class="btn sm" :disabled="saving" @click="submitTags">{{ t('common.save') }}</button>
      <button class="btn secondary sm" :disabled="saving" @click="emit('close')">{{ t('common.cancel') }}</button>
    </div>
  </ModalDialog>
</template>
