<script lang="ts">
export interface UploadItem {
  file: File
  key: string
  /** 入队时所属桶（防止上传途中切换桶导致串桶）。 */
  bucket?: string
  pct: number
  status: 'pending' | 'uploading' | 'done' | 'err'
  err?: string
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { t } from '../i18n'

const props = defineProps<{
  items: UploadItem[]
}>()

const uploadPct = computed(() => {
  if (!props.items.length) return 0
  return Math.round(props.items.reduce((s, it) => s + it.pct, 0) / props.items.length)
})
const uploadDone = computed(() => props.items.filter((it) => it.status === 'done').length)
</script>

<template>
  <div v-if="items.length" class="upload-inline" role="status">
    <div class="progress" style="flex:1"><div class="bar" :style="{ width: uploadPct + '%' }" /></div>
    <span class="badge">{{ uploadDone }}/{{ items.length }} · {{ uploadPct }}%</span>
    <div class="upload-items">
      <span v-for="it in items" :key="(it.bucket || '') + '|' + it.key" class="badge mono">
        {{ it.key }} — {{ it.status === 'done' ? '✓' : it.status === 'err' ? (it.err || t('upload.statusErr')) : it.pct + '%' }}
      </span>
    </div>
  </div>
</template>

<style scoped>
/* 上传到当前目录 */
.upload-inline {
  display: flex; flex-direction: column; gap: 6px;
  padding: 10px 14px; margin-bottom: 12px;
  border: 1px solid var(--border); border-radius: var(--radius-lg);
  background: var(--panel-2);
}
.upload-items { display: flex; flex-wrap: wrap; gap: 4px 14px; max-height: 90px; overflow: auto; }
</style>
