<script setup lang="ts">
import { ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { t, tf } from '../i18n'
import { toast } from '../store'
import ModalDialog from './ModalDialog.vue'
import { storageClassOptions, storageClassLabel } from '../storageClass'

const props = defineProps<{
  open: boolean
  accountId: string
  bucket: string
  objectKey: string
  currentClass: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'saved'): void
  (e: 'error', msg: string): void
}>()

const saving = ref(false)
const chosen = ref('')

watch(
  () => props.open,
  (o) => {
    if (!o) return
    // 默认选中当前存储类型；若当前值不在白名单则回退 STANDARD
    chosen.value = props.currentClass || 'STANDARD'
  },
)

async function submit() {
  if (saving.value) return
  if (chosen.value === props.currentClass) {
    toast(t('storage.unchanged'))
    return
  }
  saving.value = true
  try {
    const r = await s3api.changeStorageClass(props.accountId, {
      bucket: props.bucket,
      key: props.objectKey,
      storageClass: chosen.value,
    })
    toast(tf('storage.toastChanged', { key: props.objectKey, label: storageClassLabel(r.storageClass) }))
    emit('saved')
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <ModalDialog :open="open" :title="t('storage.title')" width="min(520px, 100%)" @close="emit('close')">
    <div class="mono badge" style="display:block; margin-bottom:12px; word-break:break-all">{{ objectKey }}</div>
    <label class="field">
      {{ t('storage.label') }}
      <select v-model="chosen">
        <option v-for="s in storageClassOptions()" :key="s.value" :value="s.value">{{ s.label }}</option>
      </select>
    </label>
    <div v-if="currentClass" class="badge" style="margin-top:8px">{{ tf('storage.current', { label: storageClassLabel(currentClass) }) }}</div>
    <div class="badge" style="margin-top:6px; color:var(--muted)">
      {{ t('storage.hint') }}
    </div>
    <div class="row" style="margin-top:16px">
      <button class="btn sm" :disabled="saving || chosen === currentClass" @click="submit">
        {{ saving ? t('storage.switching') : t('storage.switch') }}
      </button>
      <button class="btn secondary sm" :disabled="saving" @click="emit('close')">{{ t('common.close') }}</button>
    </div>
  </ModalDialog>
</template>
