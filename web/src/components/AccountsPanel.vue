<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { state, toast, selectAccount, accountFormRequest } from '../store'
import { confirmDialog } from '../confirm'
import { t, tf } from '../i18n'
import ModalDialog from './ModalDialog.vue'
import type { Account, AccountInput, BucketItem } from '../types'
import {
  PROVIDER_GROUPS,
  providersInGroup,
  regionsFor,
  inferProvider,
  syncsPublicEndpoint,
  providerGroupLabel,
  providerLabel,
  providerDesc,
  regionLabel,
  type Provider,
} from '../regions'

const emit = defineEmits<{ (e: 'changed'): void }>()

const editingId = ref('')
const provider = ref<Provider>('s3')
const form = reactive<AccountInput>({
  name: '',
  endpoint: '',
  publicEndpoint: '',
  region: 'us-east-1',
  accessKey: '',
  secretKey: '',
  bucket: '',
  pathStyle: true,
  useSSL: false,
})
const showForm = ref(false)
const error = ref('')
const bucketOptions = ref<BucketItem[]>([])
const loadingBuckets = ref(false)
const bucketErr = ref('')

const regionOptions = computed(() => regionsFor(provider.value))

// key -> test state；ok === undefined 且 err 为空表示测试中
const tests = reactive<Record<string, { ok?: boolean; err?: string }>>({})

function providerDefaults(p: Provider) {
  return providersInGroup('compatible')
    .concat(providersInGroup('domestic'), providersInGroup('overseas'))
    .find((d) => d.value === p) ?? providersInGroup('compatible')[0]
}

function applyProvider(p: Provider) {
  provider.value = p
  const def = providerDefaults(p)
  form.pathStyle = def.pathStyle
  form.useSSL = def.useSSL
  form.region = def.defaultRegion
  form.endpoint = def.defaultEndpoint
  form.publicEndpoint = syncsPublicEndpoint(p) ? def.defaultEndpoint : ''
  bucketOptions.value = []
  bucketErr.value = ''
}

/** 区域选中后自动填 Endpoint（公共云同步公网 Endpoint；AWS 留空由 SDK 推导） */
function applyRegionPreset() {
  const preset = regionOptions.value.find((r) => r.region === form.region)
  if (!preset) return
  form.endpoint = preset.endpoint
  if (syncsPublicEndpoint(provider.value) && preset.endpoint) {
    form.publicEndpoint = preset.endpoint
  }
}

function resetForm() {
  form.name = ''
  form.endpoint = ''
  form.publicEndpoint = ''
  form.region = 'us-east-1'
  form.accessKey = ''
  form.secretKey = ''
  form.bucket = ''
  form.pathStyle = true
  form.useSSL = false
  provider.value = 's3'
  editingId.value = ''
  bucketOptions.value = []
  bucketErr.value = ''
}

async function load() {
  const res = await s3api.listAccounts()
  state.accounts = res.accounts
}

function startCreate() {
  resetForm()
  applyProvider('s3')
  showForm.value = true
  error.value = ''
}

function startEdit(a: Account) {
  editingId.value = a.id
  provider.value = inferProvider(a.endpoint)
  form.name = a.name
  form.endpoint = a.endpoint
  form.publicEndpoint = a.publicEndpoint ?? ''
  form.region = a.region
  form.accessKey = a.accessKey
  form.secretKey = ''
  form.bucket = a.bucket
  form.pathStyle = a.pathStyle
  form.useSSL = a.useSSL
  bucketOptions.value = []
  bucketErr.value = ''
  showForm.value = true
  error.value = ''
  fetchBuckets()
}

async function fetchBuckets() {
  bucketErr.value = ''
  loadingBuckets.value = true
  try {
    let res: { buckets: BucketItem[] }
    if (editingId.value) {
      res = await s3api.listBuckets(editingId.value)
    } else {
      if (!form.endpoint || !form.accessKey || !form.secretKey) {
        throw new Error(t('accounts.needCreds'))
      }
      res = await s3api.previewBuckets(form)
    }
    bucketOptions.value = res.buckets ?? []
    if (bucketOptions.value.length === 1 && !form.bucket) {
      form.bucket = bucketOptions.value[0].name
    }
    toast(tf('accounts.toastFetched', { n: bucketOptions.value.length }))
  } catch (e) {
    bucketErr.value = toErrorMessage(e)
  } finally {
    loadingBuckets.value = false
  }
}

async function submit() {
  error.value = ''
  try {
    if (editingId.value) {
      await s3api.updateAccount(editingId.value, form)
      selectAccount(editingId.value)
      toast(t('accounts.toastUpdated'))
    } else {
      const created = await s3api.createAccount(form)
      selectAccount(created.id)
      toast(t('accounts.toastCreated'))
    }
    showForm.value = false
    resetForm()
    await load()
    emit('changed')
  } catch (e) {
    error.value = toErrorMessage(e)
  }
}

async function remove(a: Account) {
  const ok = await confirmDialog({
    title: t('accounts.deleteTitle'),
    message: tf('accounts.deleteConfirm', { name: a.name }),
  })
  if (!ok) return
  try {
    await s3api.deleteAccount(a.id)
    if (state.currentAccountId === a.id) selectAccount('')
    toast(tf('accounts.toastDeleted', { name: a.name }))
    await load()
    emit('changed')
  } catch (e) {
    error.value = toErrorMessage(e)
  }
}

async function test(a: Account) {
  tests[a.id] = { ok: undefined }
  try {
    const r = await s3api.testAccount(a.id)
    tests[a.id] = { ok: r.ok, err: r.error }
  } catch (e) {
    tests[a.id] = { ok: false, err: toErrorMessage(e) }
  }
}

function select(a: Account) {
  selectAccount(a.id)
}

onMounted(load)

// 其他面板的空状态引导「创建第一个账号」→ 自动打开新增表单
watch(accountFormRequest, () => startCreate())
</script>

<template>
  <div class="panel">
    <div class="toolbar">
      <h3 style="margin:0">{{ t('accounts.title') }}</h3>
      <span class="spacer" />
      <button class="btn sm" @click="startCreate">{{ t('accounts.add') }}</button>
    </div>

    <div v-if="error" class="msg err" style="margin-bottom:12px">{{ error }}</div>

    <!-- 账号新增/编辑表单（弹窗） -->
    <ModalDialog
      :open="showForm"
      :title="editingId ? t('accounts.editTitle') : t('accounts.createTitle')"
      width="min(760px, 100%)"
      @close="showForm = false"
    >
      <div class="provider-picker" style="margin-bottom:12px">
        <div v-for="g in PROVIDER_GROUPS" :key="g.id" class="row provider-row">
          <span class="badge provider-group">{{ providerGroupLabel(g.id) }}</span>
          <button
            v-for="p in providersInGroup(g.id)"
            :key="p.value"
            type="button"
            class="btn sm"
            :class="provider === p.value ? '' : 'secondary'"
            :title="providerDesc(p.value)"
            @click="applyProvider(p.value)"
          >
            {{ providerLabel(p.value) }}
          </button>
        </div>
      </div>
      <div class="grid" style="grid-template-columns: repeat(auto-fit,minmax(240px,1fr))">
        <label class="field">{{ t('accounts.name') }} <input v-model="form.name" :placeholder="t('accounts.namePh')" /></label>
        <label class="field">
          {{ t('accounts.region') }}
          <input
            v-model="form.region"
            list="region-list"
            :placeholder="t('accounts.regionPh')"
            @change="applyRegionPreset"
          />
          <datalist id="region-list">
            <option v-for="r in regionOptions" :key="r.region" :value="r.region">{{ regionLabel(r.region, r.label) }}</option>
          </datalist>
          <span v-if="regionOptions.length" class="badge">{{ t('accounts.regionHint') }}</span>
        </label>
        <label class="field">{{ t('accounts.endpoint') }} <input v-model="form.endpoint" :placeholder="t('accounts.endpointPh')" /></label>
        <label class="field">
          {{ t('accounts.publicEndpoint') }}
          <input v-model="form.publicEndpoint" :placeholder="t('accounts.publicEndpointPh')" />
        </label>
        <label class="field">
          {{ t('accounts.defaultBucket') }}
          <div class="bucket-row">
            <select v-model="form.bucket">
              <option value="">{{ t('accounts.bucketSelect') }}</option>
              <option v-for="b in bucketOptions" :key="b.name" :value="b.name">{{ b.name }}</option>
            </select>
            <input v-model="form.bucket" :placeholder="t('accounts.bucketManualPh')" list="bucket-suggest" />
            <button
              type="button"
              class="btn secondary sm"
              :disabled="loadingBuckets"
              @click="fetchBuckets"
            >
              {{ loadingBuckets ? t('accounts.fetchingBuckets') : t('accounts.fetchBuckets') }}
            </button>
          </div>
          <datalist id="bucket-suggest">
            <option v-for="b in bucketOptions" :key="'d-' + b.name" :value="b.name" />
          </datalist>
          <span v-if="bucketErr" class="badge" style="color:var(--danger)">{{ bucketErr }}</span>
          <span v-else-if="!editingId" class="badge">{{ t('accounts.fetchHint') }}</span>
        </label>
        <label class="field">AccessKey ID <input v-model="form.accessKey" autocomplete="off" /></label>
        <label class="field">
          AccessKey Secret
          <input v-model="form.secretKey" type="password" :placeholder="editingId ? t('accounts.secretKeepPh') : ''" autocomplete="new-password" />
        </label>
      </div>
      <div class="row" style="margin:14px 0">
        <label class="field" style="flex-direction:row; align-items:center">
          <input type="checkbox" v-model="form.pathStyle" /> {{ t('accounts.pathStyle') }}
        </label>
        <label class="field" style="flex-direction:row; align-items:center">
          <input type="checkbox" v-model="form.useSSL" /> {{ t('accounts.useSSL') }}
        </label>
      </div>
      <div class="row">
        <button class="btn sm" @click="submit">{{ editingId ? t('common.save') : t('accounts.saveLogin') }}</button>
        <button class="btn secondary sm" @click="showForm = false">{{ t('common.cancel') }}</button>
      </div>
    </ModalDialog>

    <div v-if="state.accounts.length" class="tbl-wrap">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width:32px"></th>
            <th>{{ t('accounts.colName') }}</th>
            <th>{{ t('accounts.colEndpoint') }}</th>
            <th style="width:110px">{{ t('accounts.colRegion') }}</th>
            <th style="width:120px">{{ t('accounts.colBucket') }}</th>
            <th style="width:90px">{{ t('accounts.colHealth') }}</th>
            <th style="width:190px; text-align:right">{{ t('accounts.colActions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in state.accounts" :key="a.id" :class="{ selected: a.id === state.currentAccountId }">
            <td>
              <input type="radio" name="acc" :aria-label="tf('accounts.selectAria', { name: a.name })" :checked="a.id === state.currentAccountId" @change="select(a)" />
            </td>
            <td><b>{{ a.name }}</b></td>
            <td class="mono">{{ a.endpoint }}</td>
            <td>{{ a.region }}</td>
            <td>{{ a.bucket || 'default' }}</td>
            <td>
              <span v-if="!tests[a.id]" class="tag">{{ t('accounts.healthUntested') }}</span>
              <span v-else-if="tests[a.id].ok === undefined && !tests[a.id].err" class="tag">{{ t('accounts.healthChecking') }}</span>
              <span v-else-if="tests[a.id].ok" class="tag ok">{{ t('accounts.healthOk') }}</span>
              <span v-else class="tag bad" :title="tests[a.id].err">{{ t('accounts.healthFail') }}</span>
            </td>
            <td>
              <div class="actions">
                <button class="btn secondary sm" @click="test(a)">{{ t('accounts.test') }}</button>
                <button class="btn secondary sm" @click="startEdit(a)">{{ t('common.edit') }}</button>
                <button class="btn danger sm" @click="remove(a)">{{ t('common.delete') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else class="empty">
      <span class="empty-icon" aria-hidden="true">🔑</span>
      {{ t('accounts.empty') }}
    </div>
  </div>
</template>

<style scoped>
.provider-picker {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.provider-row {
  align-items: flex-start;
}
.provider-group {
  margin-top: 6px;
  flex-shrink: 0;
  min-width: 2.5em;
  font-weight: 600;
  color: var(--fg);
}
.bucket-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}
.bucket-row select {
  min-width: 140px;
  flex: 1;
}
.bucket-row input {
  min-width: 140px;
  flex: 1;
}
</style>
