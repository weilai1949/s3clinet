<script setup lang="ts">
import { ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { toast } from '../store'
import { copyText } from '../clipboard'
import { t, tf } from '../i18n'
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

const aclPublic = ref(false)
const aclGrants = ref<{ grantee: string; permission: string }[]>([])
const aclOwner = ref('')
const aclUrl = ref('')
const loading = ref(false)
const saving = ref(false)

watch(() => props.open, async (o) => {
  if (!o) return
  aclPublic.value = false
  aclGrants.value = []
  aclOwner.value = ''
  aclUrl.value = ''
  loading.value = true
  try {
    const r = await s3api.getObjectAcl(props.accountId, { bucket: props.bucket, key: props.objectKey })
    aclPublic.value = r.public
    aclGrants.value = r.grants
    aclOwner.value = r.owner ?? ''
    aclUrl.value = r.url
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    loading.value = false
  }
})

async function submitAcl() {
  if (saving.value) return
  saving.value = true
  try {
    await s3api.putObjectAcl(props.accountId, {
      bucket: props.bucket,
      key: props.objectKey,
      acl: aclPublic.value ? 'public-read' : 'private',
    })
    toast(aclPublic.value
      ? tf('acl.toastPublic', { key: props.objectKey })
      : tf('acl.toastPrivate', { key: props.objectKey }))
    emit('close')
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    saving.value = false
  }
}

async function copyPublicUrl() {
  if (!aclUrl.value) return
  await copyText(aclUrl.value)
  toast(t('acl.toastCopiedLink'))
}
</script>

<template>
  <ModalDialog :open="open" :title="t('acl.title')" width="min(520px, 100%)" @close="emit('close')">
    <div v-if="loading" class="empty" style="padding:22px">{{ t('acl.loading') }}</div>
    <template v-else>
      <div class="mono badge" style="display:block; margin-bottom:12px; word-break:break-all">{{ objectKey }}</div>
      <label class="field">
        {{ t('acl.access') }}
        <select v-model="aclPublic">
          <option :value="false">{{ t('acl.private') }}</option>
          <option :value="true">{{ t('acl.publicRead') }}</option>
        </select>
      </label>
      <div style="margin-top:10px">
        <div class="badge">{{ tf('acl.owner', { name: aclOwner || '—' }) }}</div>
        <table v-if="aclGrants.length" class="tbl" style="margin-top:8px">
          <thead><tr><th>{{ t('acl.grantee') }}</th><th>{{ t('acl.permission') }}</th></tr></thead>
          <tbody>
            <tr v-for="(g, i) in aclGrants" :key="i">
              <td>{{ g.grantee }}</td>
              <td class="mono">{{ g.permission }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="aclUrl" class="mono badge" style="display:block; margin-top:10px; word-break:break-all">
        {{ tf('acl.publicUrl', { url: aclUrl }) }}
      </div>
      <div class="row" style="margin-top:16px">
        <button class="btn sm" :disabled="saving" @click="submitAcl">{{ t('common.save') }}</button>
        <button class="btn secondary sm" :disabled="saving || !aclUrl" @click="copyPublicUrl">{{ t('acl.copyPublicUrl') }}</button>
        <button class="btn secondary sm" :disabled="saving" @click="emit('close')">{{ t('common.close') }}</button>
      </div>
    </template>
  </ModalDialog>
</template>
