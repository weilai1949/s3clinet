<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { toErrorMessage } from '../errors'

import { api } from '../api'
import { toast } from '../store'
import { confirmDialog } from '../confirm'
import { t, tf } from '../i18n'
import ModalDialog from './ModalDialog.vue'
import type { ServerProfile } from '../types'

const servers = ref<ServerProfile[]>([])
const activeId = ref('')
const showForm = ref(false)
const editingId = ref('')
const error = ref('')
const form = reactive({ name: '', base: '', token: '' })
const persistent = ref(api.isTokenPersistent)

const healthMap = reactive<Record<string, { ok?: boolean; err?: string; checking?: boolean; version?: string }>>({})

const active = computed(() => servers.value.find((s) => s.id === activeId.value))

function reload() {
  servers.value = api.listServers()
  activeId.value = api.activeServerId()
  persistent.value = api.isTokenPersistent
}

function resetForm() {
  form.name = ''
  form.base = ''
  form.token = ''
  editingId.value = ''
}

function startCreate() {
  resetForm()
  form.name = t('server.defaultName')
  form.base = 'http://127.0.0.1:8080'
  showForm.value = true
  error.value = ''
}

function startEdit(s: ServerProfile) {
  editingId.value = s.id
  form.name = s.name
  form.base = s.base
  form.token = s.token
  showForm.value = true
  error.value = ''
}

function onPersistentChange() {
  api.setTokenPersistent(persistent.value)
}

function submit() {
  error.value = ''
  if (!form.name.trim()) {
    error.value = t('server.nameRequired')
    return
  }
  const p = api.upsertServer({
    id: editingId.value || undefined,
    name: form.name,
    base: form.base,
    token: form.token,
  })
  toast(editingId.value ? t('server.toastUpdated') : t('server.toastAdded'))
  showForm.value = false
  resetForm()
  reload()
  // 若编辑的是当前生效项，已自动 apply；刷新账号数据
  if (p.id === activeId.value) {
    setTimeout(() => window.location.reload(), 300)
  }
}

async function remove(s: ServerProfile) {
  const ok = await confirmDialog({
    title: t('server.deleteTitle'),
    message: tf('server.deleteConfirm', { name: s.name }),
  })
  if (!ok) return
  const wasActive = s.id === activeId.value
  delete healthMap[s.id]
  api.deleteServer(s.id)
  toast(tf('server.toastDeleted', { name: s.name }))
  reload()
  if (wasActive) {
    toast(t('server.toastSwitchedActive'))
    setTimeout(() => window.location.reload(), 300)
  }
}

function select(s: ServerProfile) {
  if (s.id === activeId.value) return
  api.selectServer(s.id)
  activeId.value = s.id
  toast(tf('server.toastSelect', { name: s.name }))
  setTimeout(() => window.location.reload(), 300)
}

async function probe(s: ServerProfile) {
  healthMap[s.id] = { checking: true }
  try {
    const base = (s.base || '').replace(/\/+$/, '')
    const headers: Record<string, string> = {}
    if (s.token) headers['Authorization'] = `Bearer ${s.token}`
    const res = await fetch(base + '/api/health', { headers })
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
    let version = ''
    try {
      const j = (await res.json()) as { version?: string }
      version = j.version ?? ''
    } catch {
      /* non-JSON body */
    }
    healthMap[s.id] = { ok: true, version }
  } catch (e) {
    healthMap[s.id] = { ok: false, err: toErrorMessage(e) }
  }
}

async function probeAll() {
  await Promise.all(servers.value.map((s) => probe(s)))
}

onMounted(() => {
  reload()
  probeAll()
})
</script>

<template>
  <div class="panel">
    <div class="toolbar">
      <h3 style="margin:0">{{ t('server.title') }}</h3>
      <span class="spacer" />
      <span class="badge">{{ tf('server.active', { name: active?.name || '—' }) }}</span>
      <button class="btn secondary sm" @click="probeAll">{{ t('server.probeAll') }}</button>
      <button class="btn sm" @click="startCreate">{{ t('server.add') }}</button>
    </div>

    <p class="badge" style="margin:0 0 12px; display:block; line-height:1.5">
      {{ t('server.hint') }}
    </p>

    <label class="field ephemeral-field" style="margin-bottom:16px; display:flex; align-items:center; gap:8px; cursor:pointer">
      <input v-model="persistent" type="checkbox" @change="onPersistentChange" />
      <span>{{ t('server.persistent') }}</span>
    </label>

    <div v-if="error" class="msg err" style="margin-bottom:12px">{{ error }}</div>

    <!-- 服务端新增/编辑表单（弹窗） -->
    <ModalDialog
      :open="showForm"
      :title="editingId ? t('server.editTitle') : t('server.createTitle')"
      width="min(560px, 100%)"
      @close="showForm = false"
    >
      <div class="grid" style="grid-template-columns: repeat(auto-fit,minmax(220px,1fr))">
        <label class="field">{{ t('server.name') }} <input v-model="form.name" :placeholder="t('server.namePh')" /></label>
        <label class="field">{{ t('server.base') }} <input v-model="form.base" :placeholder="t('server.basePh')" autocomplete="off" /></label>
        <label class="field">
          {{ t('server.token') }}
          <input v-model="form.token" type="password" :placeholder="t('server.tokenPh')" autocomplete="new-password" />
          <span class="badge" style="display:block; margin-top:6px; line-height:1.45; white-space:normal">{{ t('server.tokenHint') }}</span>
          <span class="badge" style="display:block; margin-top:4px; line-height:1.45; white-space:normal">{{ t('server.tokenMultiHint') }}</span>
        </label>
      </div>
      <div class="row" style="margin-top:14px">
        <button class="btn sm" @click="submit">{{ t('common.save') }}</button>
        <button class="btn secondary sm" @click="showForm = false">{{ t('common.cancel') }}</button>
      </div>
    </ModalDialog>

    <div v-if="servers.length" class="tbl-wrap">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width:36px"></th>
            <th>{{ t('server.colName') }}</th>
            <th>{{ t('server.colBase') }}</th>
            <th style="width:80px">{{ t('server.colToken') }}</th>
            <th style="width:90px">{{ t('server.colHealth') }}</th>
            <th style="width:200px; text-align:right">{{ t('server.colActions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="s in servers"
            :key="s.id"
            :class="{ selected: s.id === activeId }"
          >
            <td>
              <input
                type="radio"
                name="server"
                :aria-label="tf('server.selectAria', { name: s.name })"
                :checked="s.id === activeId"
                @change="select(s)"
              />
            </td>
            <td>
              <b>{{ s.name }}</b>
              <span v-if="s.id === activeId" class="tag ok" style="margin-left:8px">{{ t('server.activeTag') }}</span>
            </td>
            <td class="mono">{{ s.base || t('server.sameOrigin') }}</td>
            <td>{{ s.token ? t('server.tokenSet') : '—' }}</td>
            <td>
              <span v-if="!healthMap[s.id]" class="tag">{{ t('server.healthUntested') }}</span>
              <span v-else-if="healthMap[s.id].checking" class="tag">{{ t('server.healthChecking') }}</span>
              <span v-else-if="healthMap[s.id].ok" class="tag ok">{{ t('server.healthOk') }}</span>
              <span v-else class="tag bad" :title="healthMap[s.id].err">{{ t('server.healthFail') }}</span>
              <span v-if="healthMap[s.id]?.ok && healthMap[s.id].version" class="tag" style="margin-left:6px">v{{ healthMap[s.id].version }}</span>
            </td>
            <td>
              <div class="actions">
                <button class="btn secondary sm" @click="probe(s)">{{ t('server.probe') }}</button>
                <button class="btn secondary sm" @click="startEdit(s)">{{ t('server.edit') }}</button>
                <button class="btn danger sm" @click="remove(s)">{{ t('server.delete') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else class="empty">
      <span class="empty-icon" aria-hidden="true">🖥️</span>
      {{ t('server.empty') }}
    </div>
  </div>
</template>
