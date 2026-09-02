<script setup lang="ts">
import { fmtSize } from '../format'
import { t, tf } from '../i18n'
import type { BucketItem } from '../types'

defineProps<{
  bucket: string // 当前桶
  buckets: BucketItem[]
  loadingBuckets: boolean
  loading: boolean
  prefix: string
  crumbs: { name: string; path: string }[]
  pathEditing: boolean
  filterActive: boolean
  visibleCount: number
  totalCount: number
  allSelected: boolean
  selectedCount: number
  selectedSize: number
  fileCount: number
  loadedSize: number
  bucketView: 'list' | 'grid'
  uploading: boolean
  opsBusy: boolean
  zipLoading: boolean
}>()

const filter = defineModel<string>('filter', { default: '' })
const pathDraft = defineModel<string>('pathDraft', { default: '' })

const emit = defineEmits<{
  (e: 'bucket-change', value: string): void
  (e: 'open-bucket-info'): void
  (e: 'go-root'): void
  (e: 'enter-prefix', path: string): void
  (e: 'go-up'): void
  (e: 'start-path-edit'): void
  (e: 'commit-path'): void
  (e: 'cancel-path-edit'): void
  (e: 'toggle-path-edit'): void
  (e: 'refresh'): void
  (e: 'back-to-buckets'): void
  (e: 'upload'): void
  (e: 'mkdir'): void
  (e: 'toggle-select-all'): void
  (e: 'open-dest-multi', mode: 'copy' | 'move'): void
  (e: 'copy-links'): void
  (e: 'download-zip'): void
  (e: 'remove-selected'): void
  (e: 'toggle-view'): void
}>()
</script>

<template>
  <!-- 位置栏：Bucket + 路径 + 过滤 -->
  <div class="toolbar locbar">
    <label class="field" style="flex-direction:row; align-items:center; gap:8px">
      Bucket
      <select :value="bucket" :disabled="loadingBuckets" style="min-width:160px" @change="emit('bucket-change', ($event.target as HTMLSelectElement).value)">
        <option v-if="!buckets.length" value="">{{ t('toolbar.noBucket') }}</option>
        <option v-for="b in buckets" :key="b.name" :value="b.name">{{ b.name }}</option>
      </select>
      <button class="btn secondary sm" :title="t('toolbar.bucketPropsHint')" :disabled="!bucket" @click="emit('open-bucket-info')">{{ t('toolbar.bucketProps') }}</button>
    </label>
    <span class="tb-divider" aria-hidden="true" />
    <div v-if="pathEditing" class="crumbs path-editor">
      <input
        v-model="pathDraft"
        class="path-input"
        :placeholder="t('toolbar.pathPlaceholder')"
        spellcheck="false"
        @keydown.enter.prevent="emit('commit-path')"
        @keydown.esc="emit('cancel-path-edit')"
        @blur="emit('commit-path')"
      />
    </div>
    <div v-else class="crumbs" :aria-label="t('toolbar.currentPath')">
      <button class="link" @click="emit('go-root')">{{ bucket || 'Bucket' }}</button>
      <template v-for="c in crumbs" :key="c.path">
        <span class="sep">/</span>
        <button v-if="c.path !== prefix" class="link" @click="emit('enter-prefix', c.path)">{{ c.name }}</button>
        <span v-else class="cur">{{ c.name }}</span>
      </template>
    </div>
    <button class="link path-edit-btn" :title="pathEditing ? '' : t('toolbar.editPath')" @click="emit('toggle-path-edit')">
      {{ pathEditing ? '✓' : '✎' }}
    </button>
    <span class="spacer" />
    <input
      v-model="filter"
      type="search"
      class="filter-input"
      :placeholder="t('toolbar.filterPlaceholder')"
      :title="filterActive ? t('toolbar.filterActiveHint') : t('toolbar.filterHint')"
    />
    <span v-if="filterActive" class="badge">
      {{ visibleCount }}/{{ totalCount }}
      <button class="link" style="margin-left:4px" @click="filter = ''">{{ t('common.clear') }}</button>
    </span>
  </div>

  <!-- 操作栏：浏览 / 上传 / 批量选择 -->
  <div class="toolbar opbar">
    <button class="btn secondary sm" :disabled="loading || loadingBuckets || opsBusy" @click="emit('refresh')">{{ t('common.refresh') }}</button>
    <button class="btn secondary sm" :disabled="!prefix || opsBusy" @click="emit('go-up')">{{ t('toolbar.goUp') }}</button>
    <button class="btn secondary sm" :disabled="loading || opsBusy" @click="emit('back-to-buckets')">{{ t('toolbar.backBuckets') }}</button>
    <span class="tb-divider" aria-hidden="true" />
    <button class="btn sm" :disabled="uploading || opsBusy" @click="emit('upload')">
      {{ uploading ? t('toolbar.uploading') : t('toolbar.upload') }}
    </button>
    <button class="btn sm" :disabled="opsBusy" @click="emit('mkdir')">+ {{ t('toolbar.mkdir') }}</button>
    <span class="tb-divider" aria-hidden="true" />
    <label title="Ctrl/Cmd+A"><input type="checkbox" :checked="allSelected" @change="emit('toggle-select-all')" /> {{ t('common.selectAll') }}</label>
    <span class="badge">{{ tf('toolbar.selectedStats', { n: selectedCount, size: fmtSize(selectedSize) }) }}</span>
    <button class="btn secondary sm" :disabled="!selectedCount || opsBusy" @click="emit('open-dest-multi', 'copy')">{{ t('toolbar.copyTo') }}</button>
    <button class="btn secondary sm" :disabled="!selectedCount || opsBusy" @click="emit('open-dest-multi', 'move')">{{ t('toolbar.moveTo') }}</button>
    <button class="btn secondary sm" :disabled="!selectedCount || opsBusy" @click="emit('copy-links')">{{ t('toolbar.copyLink') }}</button>
    <button class="btn secondary sm" :disabled="!selectedCount || zipLoading || opsBusy" @click="emit('download-zip')">
      {{ zipLoading ? t('toolbar.zipping') : t('toolbar.zipDownload') }}
    </button>
    <button class="btn danger sm" :disabled="!selectedCount || opsBusy" @click="emit('remove-selected')">{{ t('common.delete') }}</button>
  </div>

  <!-- 统计条 + 视图切换 -->
  <div class="toolbar stats-bar">
    <span class="badge">📊 {{ tf('toolbar.fileStats', { n: fileCount, size: fmtSize(loadedSize) }) }}</span>
    <span v-if="selectedCount" class="badge" style="color:var(--primary)">{{ tf('toolbar.selectedFiles', { n: selectedCount, size: fmtSize(selectedSize) }) }}</span>
    <span class="spacer" />
    <button class="btn secondary sm" @click="emit('toggle-view')">
      {{ bucketView === 'list' ? t('toolbar.gridView') : t('toolbar.listView') }}
    </button>
  </div>
</template>

<style scoped>
.filter-input { width: 200px; padding: 7px 12px; }

/* 工具条分组分隔线 */
.tb-divider { width: 1px; height: 20px; background: var(--border); flex: none; }

/* 统计条 */
.stats-bar { margin-bottom: 10px; padding: 8px 12px; border-radius: var(--radius); background: var(--panel-2); border: 1px solid var(--border); }

/* 路径编辑 */
.path-editor { flex: 1; min-width: 220px; }
.path-input { width: 100%; padding: 6px 10px; font-family: var(--font-mono); font-size: 12px; }
.path-edit-btn { margin-left: 4px; font-size: 13px; }
</style>
