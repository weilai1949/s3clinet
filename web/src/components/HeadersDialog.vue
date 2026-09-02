<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { t, tf } from '../i18n'
import { toast } from '../store'
import ModalDialog from './ModalDialog.vue'
import type { ObjectMeta } from '../types'

const props = defineProps<{
  open: boolean
  accountId: string
  bucket: string
  objectKey: string
  detail?: ObjectMeta | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
  (e: 'error', msg: string): void
}>()

const contentType = ref('')
const meta = reactive<{ key: string; value: string }[]>([])

watch(() => props.open, (o) => {
  if (!o) return
  contentType.value = ''
  meta.splice(0, meta.length)
  const d = props.detail
  if (d && d.key === props.objectKey) {
    contentType.value = d.contentType || ''
    for (const [k, v] of Object.entries(d.metadata ?? {})) {
      meta.push({ key: k, value: v })
    }
  }
})

function addMetaRow() {
  meta.push({ key: '', value: '' })
}

function removeMetaRow(i: number) {
  meta.splice(i, 1)
}

async function submitHeaders() {
  const m: Record<string, string> = {}
  for (const item of meta) {
    const k = item.key.trim()
    if (k && item.value) m[k] = item.value
  }
  try {
    await s3api.setHeaders(props.accountId, {
      bucket: props.bucket,
      key: props.objectKey,
      contentType: contentType.value.trim(),
      metadata: m,
    })
    toast(tf('headers.toastUpdated', { key: props.objectKey }))
    emit('saved')
  } catch (err) {
    emit('error', toErrorMessage(err))
  }
}
</script>

<template>
  <ModalDialog :open="open" :title="t('headers.title')" width="min(540px, 100%)" @close="emit('close')">
    <div class="grid" style="grid-template-columns:1fr">
      <label class="field">{{ t('headers.object') }} <span class="mono badge">{{ objectKey }}</span></label>
      <label class="field">
        {{ t('headers.contentType') }}
        <input v-model="contentType" :placeholder="t('headers.contentTypePh')" autocomplete="off" spellcheck="false" />
      </label>
      <div class="field">
        <div class="row" style="justify-content:space-between; margin-bottom:4px">
          <span>{{ t('headers.customMeta') }}</span>
          <button class="btn secondary sm" @click="addMetaRow">{{ t('headers.add') }}</button>
        </div>
        <div v-for="(m, i) in meta" :key="i" class="row" style="margin-top:6px">
          <input v-model="m.key" :placeholder="t('headers.keyPh')" style="flex:1" autocomplete="off" spellcheck="false" />
          <input v-model="m.value" :placeholder="t('headers.valuePh')" style="flex:1" autocomplete="off" spellcheck="false" />
          <button class="btn secondary sm" :aria-label="t('common.delete')" @click="removeMetaRow(i)">✕</button>
        </div>
      </div>
    </div>
    <div class="row" style="margin-top:16px">
      <button class="btn sm" @click="submitHeaders">{{ t('common.save') }}</button>
      <button class="btn secondary sm" @click="emit('close')">{{ t('common.cancel') }}</button>
    </div>
  </ModalDialog>
</template>
