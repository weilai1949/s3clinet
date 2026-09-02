<script lang="ts">
export type { Entry, SortKey } from '../types'
</script>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { fmtDate, fmtSize } from '../format'
import { t, tf } from '../i18n'
import { previewKind } from '../preview'
import type { Entry, ObjectItem, SortKey } from '../types'

const props = withDefaults(
  defineProps<{
    entries: Entry[] // 过滤 + 排序后的可见条目
    bucketView: 'list' | 'grid'
    selected: Set<string>
    sortKey: SortKey
    sortDir: 1 | -1
    filter: string
    filterActive: boolean
    loading: boolean
    totalCount: number // 未过滤条目总数（骨架屏「无任何条目」判断）
    visibleCount: number // 可见条目数（空态判断）
    nextToken: string
    isTruncated: boolean
    loadingAll: boolean
  }>(),
  {},
)

const emit = defineEmits<{
  (e: 'rowClick', entry: Entry): void
  (e: 'rowDbl', entry: Entry): void
  (e: 'ctx', event: MouseEvent, entry: Entry): void
  (e: 'ctxButton', event: MouseEvent, entry: Entry): void
  (e: 'checkToggle', key: string, shift: boolean): void
  (e: 'enterDir', path: string): void
  (e: 'download', obj: ObjectItem): void
  (e: 'preview', obj: ObjectItem): void
  (e: 'toggleSort', key: SortKey): void
  (e: 'loadMore'): void
  (e: 'loadAll'): void
}>()

const sortInd = computed(() => (props.sortDir === 1 ? '▲' : '▼'))

function ariaSort(key: SortKey): 'ascending' | 'descending' | 'none' {
  if (props.sortKey !== key) return 'none'
  return props.sortDir === 1 ? 'ascending' : 'descending'
}

/* shift 范围选择的临时记录：change 事件不含 shiftKey，需从 mousedown 捕获。 */
const shiftDown = ref(false)

/* 列表视图窗口化：仅渲染可视区 + overscan，避免大目录万级 DOM。 */
const ROW_HEIGHT = 38
const OVERSCAN = 12
const scrollEl = ref<HTMLElement | null>(null)
const scrollTop = ref(0)
const viewportH = ref(480)

const windowed = computed(() => {
  const total = props.entries.length
  const start = Math.max(0, Math.floor(scrollTop.value / ROW_HEIGHT) - OVERSCAN)
  const count = Math.ceil(viewportH.value / ROW_HEIGHT) + OVERSCAN * 2
  const end = Math.min(total, start + count)
  return {
    start,
    end,
    items: props.entries.slice(start, end),
    padTop: start * ROW_HEIGHT,
    padBottom: Math.max(0, (total - end) * ROW_HEIGHT),
  }
})

function onListScroll() {
  if (scrollEl.value) scrollTop.value = scrollEl.value.scrollTop
}

function measureViewport() {
  if (scrollEl.value) viewportH.value = scrollEl.value.clientHeight || 480
}

let resizeObs: ResizeObserver | undefined
onMounted(() => {
  measureViewport()
  if (scrollEl.value && typeof ResizeObserver !== 'undefined') {
    resizeObs = new ResizeObserver(measureViewport)
    resizeObs.observe(scrollEl.value)
  }
})
onBeforeUnmount(() => resizeObs?.disconnect())

/* 网格视图图标（按类型） */
function iconFor(e: Entry): string {
  const k = previewKind(e.key)
  if (k === 'image') return '🖼️'
  if (k === 'video') return '🎬'
  if (k === 'audio') return '🎵'
  if (k === 'pdf') return '📕'
  if (/\.(zip|tar|gz|7z|rar)$/i.test(e.key)) return '📦'
  if (k === 'text') return '📄'
  return '📎'
}
</script>

<template>
  <div v-if="loading && !totalCount" aria-busy="true" :aria-label="t('objects.loadingAria')">
    <div v-for="i in 6" :key="i" class="skel-row" />
  </div>

  <!-- 网格视图 -->
  <div v-else-if="bucketView === 'grid'" class="grid-view">
    <div
      v-for="e in entries"
      :key="e.key"
      class="grid-item"
      :class="{ selected: e.kind === 'file' && selected.has(e.key) }"
      role="button"
      tabindex="0"
      @click="emit('rowClick', e)"
      @dblclick="emit('rowDbl', e)"
      @keydown.enter.prevent="emit('rowDbl', e)"
      @keydown.space.prevent="emit('rowClick', e)"
      @contextmenu.prevent="emit('ctx', $event, e)"
    >
      <div class="gi-icon" aria-hidden="true">{{ e.kind === 'folder' ? '📁' : iconFor(e) }}</div>
      <div class="gi-name" :title="e.key">{{ e.name }}</div>
      <div class="gi-meta">{{ e.kind === 'folder' ? t('objects.folder') : (fmtSize(e.size ?? 0) + (e.object?.storageClass ? ' · ' + e.object.storageClass : '')) }}</div>
    </div>
    <div v-if="!entries.length && !loading" class="empty" style="grid-column:1/-1">
      <span class="empty-icon" aria-hidden="true">{{ filterActive ? '🔍' : '📭' }}</span>
      {{ filterActive ? tf('objects.noMatch', { q: filter }) : t('objects.emptyDir') }}
    </div>
  </div>

  <!-- 列表视图（窗口化 tbody） -->
  <div v-else ref="scrollEl" class="tbl-wrap tbl-virtual" @scroll.passive="onListScroll">
    <table class="tbl">
      <thead>
        <tr>
          <th style="width:30px"></th>
          <th
            class="sortable"
            tabindex="0"
            role="columnheader"
            :aria-sort="ariaSort('name')"
            @click="emit('toggleSort', 'name')"
            @keydown.enter.space.prevent="emit('toggleSort', 'name')"
          >
            {{ t('objects.colName') }}<span v-if="sortKey === 'name'" class="sort-ind">{{ sortInd }}</span>
          </th>
          <th
            class="sortable"
            style="width:90px"
            tabindex="0"
            role="columnheader"
            :aria-sort="ariaSort('size')"
            @click="emit('toggleSort', 'size')"
            @keydown.enter.space.prevent="emit('toggleSort', 'size')"
          >
            {{ t('objects.colSize') }}<span v-if="sortKey === 'size'" class="sort-ind">{{ sortInd }}</span>
          </th>
          <th
            class="sortable"
            style="width:160px"
            tabindex="0"
            role="columnheader"
            :aria-sort="ariaSort('time')"
            @click="emit('toggleSort', 'time')"
            @keydown.enter.space.prevent="emit('toggleSort', 'time')"
          >
            {{ t('objects.colTime') }}<span v-if="sortKey === 'time'" class="sort-ind">{{ sortInd }}</span>
          </th>
          <th style="width:170px; text-align:right">{{ t('common.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="windowed.padTop" class="v-spacer" aria-hidden="true">
          <td :colspan="5" :style="{ height: windowed.padTop + 'px' }" />
        </tr>
        <tr
          v-for="e in windowed.items"
          :key="e.key"
          class="v-row"
          :class="{ selected: e.kind === 'file' && selected.has(e.key), 'row-folder': e.kind === 'folder' }"
          @click="emit('rowClick', e)"
          @dblclick="emit('rowDbl', e)"
          @contextmenu.prevent="emit('ctx', $event, e)"
        >
          <template v-if="e.kind === 'folder'">
            <td></td>
            <td>
              <button class="folder-link" @click.stop="emit('enterDir', e.key)">
                <span class="folder-icon" aria-hidden="true">📁</span>
                <span>{{ e.name }}</span>
              </button>
            </td>
            <td class="muted">—</td>
            <td class="muted">—</td>
            <td>
              <div class="actions" @click.stop>
                <button class="btn secondary sm more-btn" :title="t('objects.moreActions')" :aria-label="t('objects.moreActions')" @click="emit('ctxButton', $event, e)">⋯</button>
              </div>
            </td>
          </template>
          <template v-else>
            <td>
              <input
                type="checkbox"
                :aria-label="tf('objects.selectItem', { name: e.name })"
                :checked="selected.has(e.key)"
                @click.stop
                @mousedown="shiftDown = ($event as MouseEvent).shiftKey"
                @change="emit('checkToggle', e.key, shiftDown)"
              />
            </td>
            <td>
              <span class="file-icon" aria-hidden="true">📄</span>
              <span class="mono">{{ e.name }}</span>
              <span v-if="e.object?.storageClass" class="badge sc-badge" :title="t('objects.storageClass') + '：' + e.object.storageClass">{{ e.object.storageClass }}</span>
            </td>
            <td>{{ fmtSize(e.size ?? 0) }}</td>
            <td class="muted">{{ fmtDate(e.lastModified ?? '') }}</td>
            <td>
              <div class="actions" @click.stop>
                <button class="btn secondary sm" @click="emit('download', e.object!)">{{ t('common.download') }}</button>
                <button class="btn secondary sm" @click="emit('preview', e.object!)">{{ t('objects.preview') }}</button>
                <button class="btn secondary sm more-btn" :title="t('objects.moreActionsHint')" :aria-label="t('objects.moreActions')" @click="emit('ctxButton', $event, e)">⋯</button>
              </div>
            </td>
          </template>
        </tr>
        <tr v-if="windowed.padBottom" class="v-spacer" aria-hidden="true">
          <td :colspan="5" :style="{ height: windowed.padBottom + 'px' }" />
        </tr>
      </tbody>
    </table>
    <div v-if="!entries.length && !loading" class="empty">
      <span class="empty-icon" aria-hidden="true">{{ filterActive ? '🔍' : '📭' }}</span>
      {{ filterActive ? tf('objects.noMatch', { q: filter }) : t('objects.emptyDir') }}
    </div>
  </div>

  <div class="toolbar" style="margin-top:12px; margin-bottom:0">
    <button class="btn secondary sm" :disabled="!nextToken || loading || loadingAll" @click="emit('loadMore')">
      {{ loading ? t('common.loading') : t('common.more') }}
    </button>
    <button class="btn secondary sm" :disabled="!nextToken || loading || loadingAll" @click="emit('loadAll')">
      {{ loadingAll ? t('common.loading') : t('objects.loadAll') }}
    </button>
    <span class="badge">{{ isTruncated ? t('objects.hasMore') : t('objects.endReached') }}</span>
  </div>
</template>

<style scoped>
.row-folder:hover { background: var(--row-hover); }

/* 虚拟列表滚动容器：固定可视高度，行高与 ROW_HEIGHT 对齐 */
.tbl-virtual {
  max-height: min(60vh, 640px);
  overflow: auto;
}
.tbl-virtual .v-row { height: 38px; }
.tbl-virtual .v-spacer td {
  padding: 0 !important;
  border: none !important;
  line-height: 0;
}

/* 行操作按钮：hover 显示（窄屏常显）；「⋯ 更多」常显 */
.tbl .actions { opacity: .35; transition: opacity .15s ease; }
.tbl .actions .more-btn { opacity: 1; }
.tbl tbody tr:hover .actions,
.tbl tbody tr:focus-within .actions,
.tbl tbody tr.selected .actions { opacity: 1; }
@media (max-width: 900px) {
  .tbl .actions { opacity: 1; }
}

/* 网格视图（COS 式卡片） */
.grid-view {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 12px;
  background: var(--panel);
}
.grid-item {
  display: flex; flex-direction: column; align-items: center; gap: 6px;
  padding: 14px 8px 10px;
  border: 1px solid transparent;
  border-radius: var(--radius-lg);
  cursor: pointer;
  transition: all .15s ease;
  text-align: center;
}
.grid-item:hover { background: var(--row-hover); border-color: var(--border); }
.grid-item.selected { background: var(--row-sel); border-color: var(--primary); }
.gi-icon { font-size: 30px; line-height: 1; }
.gi-name {
  width: 100%;
  font-size: 12px; font-weight: 600;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.gi-meta { font-size: 11px; color: var(--muted); }

.folder-link {
  display: inline-flex; align-items: center; gap: 8px;
  background: none; border: none; padding: 0;
  color: var(--text); font: inherit; font-weight: 600;
  cursor: pointer; text-align: left;
}
.folder-link:hover { color: var(--primary); }
.folder-link:hover .folder-icon { transform: scale(1.05); }
.folder-icon { font-size: 16px; transition: transform .12s ease; }
.file-icon { margin-right: 6px; opacity: .75; }
.sc-badge { margin-left: 6px; font-size: 10px; background: var(--row-sel); color: var(--muted); border: 1px solid var(--border); }
.sc-badge:not(:empty) { text-transform: uppercase; }
</style>
