<script lang="ts">
// 条目结构收敛到共享状态机（useUploadQueue）；此处沿用历史导出名 UploadItem。
import type { UploadQueueItem } from '../composables/useUploadQueue'
export type UploadItem = UploadQueueItem
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { t } from '../i18n'

const props = defineProps<{
  items: UploadItem[]
}>()

const emit = defineEmits<{
  (e: 'cancel', it: UploadItem): void
}>()

const uploadPct = computed(() => {
  if (!props.items.length) return 0
  return Math.round(props.items.reduce((s, it) => s + it.pct, 0) / props.items.length)
})
const uploadDone = computed(() => props.items.filter((it) => it.status === 'done').length)

function statusText(it: UploadItem): string {
  if (it.status === 'done') return '✓'
  if (it.status === 'cancelled') return t('upload.statusCancelled')
  if (it.status === 'err') return it.err || t('upload.statusErr')
  return it.pct + '%'
}
</script>

<template>
  <div v-if="items.length" class="upload-inline" role="status">
    <div class="progress" style="flex:1"><div class="bar" :style="{ width: uploadPct + '%' }" /></div>
    <span class="badge">{{ uploadDone }}/{{ items.length }} · {{ uploadPct }}%</span>
    <div class="upload-items">
      <span v-for="it in items" :key="(it.bucket || '') + '|' + it.key" class="badge mono">
        {{ it.key }} — {{ statusText(it) }}
        <button
          v-if="it.status === 'uploading' || it.status === 'pending'"
          class="btn secondary sm cancel-btn"
          :aria-label="t('common.cancel')"
          :title="t('common.cancel')"
          @click="emit('cancel', it)"
        >✕</button>
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
.cancel-btn { margin-left: 6px; padding: 0 6px; line-height: 1.4; }
</style>
