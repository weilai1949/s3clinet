<script setup lang="ts">
import { onMounted, ref, watch, computed } from 'vue'
import { toErrorMessage } from '../errors'

import { state, selectAccount, rememberedAccountId } from '../store'
import { s3api } from '../api'
import { toast } from '../store'
import { confirmDialog } from '../confirm'
import { fmtDate } from '../format'
import { t, tf } from '../i18n'
import CreateBucketDialog from './CreateBucketDialog.vue'
import LifecycleDialog from './LifecycleDialog.vue'
import BucketOverview from './BucketOverview.vue'
import BucketEncryption from './BucketEncryption.vue'
import BucketCors from './BucketCors.vue'
import BucketWebsite from './BucketWebsite.vue'
import BucketPolicy from './BucketPolicy.vue'
import BucketTags from './BucketTags.vue'
import type { BucketItem } from '../types'

type TabKey = 'overview' | 'lifecycle' | 'encryption' | 'cors' | 'website' | 'policy' | 'tags'

const tabs = computed(() => [
  { key: 'overview' as TabKey, label: t('buckets.tabOverview') },
  { key: 'lifecycle' as TabKey, label: t('buckets.tabLifecycle') },
  { key: 'encryption' as TabKey, label: t('buckets.tabEncryption') },
  { key: 'cors' as TabKey, label: t('buckets.tabCors') },
  { key: 'website' as TabKey, label: t('buckets.tabWebsite') },
  { key: 'policy' as TabKey, label: t('buckets.tabPolicy') },
  { key: 'tags' as TabKey, label: t('buckets.tabTags') },
])

const accSel = ref('')
const buckets = ref<BucketItem[]>([])
const loadingBuckets = ref(false)
const selectedBucket = ref('')
const activeTab = ref<TabKey>('overview')
const error = ref('')
const creatingBucket = ref(false)
const lifecycleOpen = ref(false)

const account = () => state.accounts.find((a) => a.id === accSel.value)

async function loadBuckets() {
  if (!accSel.value) {
    buckets.value = []
    return
  }
  loadingBuckets.value = true
  try {
    const r = await s3api.listBuckets(accSel.value)
    buckets.value = r.buckets
    if (!selectedBucket.value || !r.buckets.some((b) => b.name === selectedBucket.value)) {
      selectedBucket.value = r.buckets[0]?.name ?? ''
    }
  } catch (err) {
    error.value = toErrorMessage(err)
  } finally {
    loadingBuckets.value = false
  }
}

onMounted(() => {
  const remembered = rememberedAccountId()
  if (state.accounts.some((a) => a.id === remembered)) accSel.value = remembered
  else accSel.value = state.currentAccountId && state.accounts.some((a) => a.id === state.currentAccountId) ? state.currentAccountId : (state.accounts[0]?.id ?? '')
  selectAccount(accSel.value)
  loadBuckets()
})

watch(accSel, (v) => {
  selectAccount(v)
  selectedBucket.value = ''
  loadBuckets()
})

watch(
  () => state.accounts,
  () => {
    // 账号列表变化时保持 accSel 有效（仅监听 accounts，避免 currentAccountId 变更误触发）
    if (accSel.value && !state.accounts.some((a) => a.id === accSel.value)) {
      accSel.value = state.accounts[0]?.id ?? ''
    }
  },
)

async function onCreateBucket() {
  creatingBucket.value = false
  toast(t('buckets.created'))
  await loadBuckets()
}

async function removeBucket(name: string) {
  const ok = await confirmDialog({
    title: t('buckets.deleteTitle'),
    message: tf('buckets.deleteConfirm', { name }),
    confirmText: t('common.delete'),
    danger: true,
  })
  if (!ok) return
  try {
    await s3api.deleteBucket(accSel.value, name)
    toast(tf('buckets.deleted', { name }))
    await loadBuckets()
  } catch (err) {
    error.value = toErrorMessage(err)
  }
}
</script>

<template>
  <div class="panel">
    <div class="toolbar">
      <h3 style="margin:0">{{ t('buckets.title') }}</h3>
      <span class="spacer" />
      <span class="badge">{{ t('common.account') }}</span>
      <select v-model="accSel" class="acc-select" :title="tf('buckets.accountSwitch', { n: state.accounts.length })">
        <option v-if="!state.accounts.length" value="">{{ t('buckets.noAccounts') }}</option>
        <option v-for="a in state.accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
      <button class="btn sm" :disabled="!account()" @click="creatingBucket = true">{{ t('buckets.createBtn') }}</button>
    </div>

    <div v-if="!account()" class="empty">
      <span class="empty-icon" aria-hidden="true">🗂️</span>
      {{ t('buckets.needAccount') }}
    </div>

    <div v-else-if="error" class="msg err" style="margin-bottom:10px">
      <span style="flex:1">{{ error }}</span>
      <button class="link" style="flex:none" @click="loadBuckets">{{ t('common.retry') }}</button>
    </div>

    <!-- ===== 桶列表 ===== -->
    <template v-if="!selectedBucket">
      <div v-if="loadingBuckets" class="empty" style="padding:20px">{{ t('buckets.loading') }}</div>
      <div v-else-if="!buckets.length" class="empty">
        <span class="empty-icon" aria-hidden="true">🪣</span>
        {{ t('buckets.empty') }}
      </div>
      <table v-else class="tbl">
        <thead><tr><th>{{ t('buckets.colName') }}</th><th style="width:170px">{{ t('buckets.colCreated') }}</th><th style="width:200px; text-align:right">{{ t('common.actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="b in buckets" :key="b.name">
            <td class="mono">{{ b.name }}</td>
            <td class="muted">{{ fmtDate(b.creationDate) }}</td>
            <td>
              <div class="actions" style="justify-content:flex-end; gap:6px">
                <button class="btn secondary sm" @click="selectedBucket = b.name; activeTab = 'overview'">{{ t('common.manage') }}</button>
                <button class="btn danger sm" @click="removeBucket(b.name)">{{ t('common.delete') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </template>

    <!-- ===== 桶详情（设置页签） ===== -->
    <template v-else>
      <div class="toolbar" style="margin-bottom:10px">
        <button class="btn secondary sm" @click="selectedBucket = ''">{{ t('buckets.backList') }}</button>
        <span class="badge mono">{{ selectedBucket }}</span>
      </div>
      <nav class="tab-row" :aria-label="t('buckets.settingsAria')">
        <button v-for="tab in tabs" :key="tab.key" :class="{ active: activeTab === tab.key }" @click="activeTab = tab.key">{{ tab.label }}</button>
      </nav>
      <div class="tab-body">
        <BucketOverview v-if="activeTab === 'overview'" :account-id="accSel" :bucket="selectedBucket" @error="error = $event" @changed="loadBuckets" />
        <template v-else-if="activeTab === 'lifecycle'">
          <div class="empty" style="padding:18px">{{ t('buckets.lifecycleHint') }}</div>
          <button class="btn sm" @click="lifecycleOpen = true">{{ t('buckets.editLifecycle') }}</button>
        </template>
        <BucketEncryption v-else-if="activeTab === 'encryption'" :account-id="accSel" :bucket="selectedBucket" @error="error = $event" @changed="loadBuckets" />
        <BucketCors v-else-if="activeTab === 'cors'" :account-id="accSel" :bucket="selectedBucket" @error="error = $event" @changed="loadBuckets" />
        <BucketWebsite v-else-if="activeTab === 'website'" :account-id="accSel" :bucket="selectedBucket" @error="error = $event" @changed="loadBuckets" />
        <BucketPolicy v-else-if="activeTab === 'policy'" :account-id="accSel" :bucket="selectedBucket" @error="error = $event" @changed="loadBuckets" />
        <BucketTags v-else-if="activeTab === 'tags'" :account-id="accSel" :bucket="selectedBucket" @error="error = $event" @changed="loadBuckets" />
      </div>
    </template>

    <CreateBucketDialog
      :open="creatingBucket"
      :account-id="accSel"
      @close="creatingBucket = false"
      @created="onCreateBucket"
      @error="error = $event"
    />

    <LifecycleDialog
      :open="lifecycleOpen"
      :account-id="accSel"
      :bucket="selectedBucket"
      @close="lifecycleOpen = false"
      @error="error = $event"
    />
  </div>
</template>

<style scoped>
.acc-select { max-width: 220px; padding: 5px 10px; font-size: 13px; }
.tab-row { display: flex; flex-wrap: wrap; gap: 4px; border-bottom: 1px solid var(--border); margin-bottom: 14px; }
.tab-row button {
  padding: 7px 14px; border: none; background: none; cursor: pointer;
  color: var(--muted); font-size: 13px; border-bottom: 2px solid transparent;
}
.tab-row button.active { color: var(--primary); border-bottom-color: var(--primary); font-weight: 600; }
.tab-body { min-height: 200px; }
.actions { display: flex; }
</style>
