<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { t } from '../i18n'
import type { Entry } from '../types'

const props = withDefaults(
  defineProps<{
    menu: { x: number; y: number; entry: Entry } | null
  }>(),
  {},
)

const emit = defineEmits<{
  (e: 'open'): void
  (e: 'preview'): void
  (e: 'copy-link'): void
  (e: 'copy-key'): void
  (e: 'copy-folder'): void
  (e: 'move-folder'): void
  (e: 'delete-folder'): void
  (e: 'copy-file'): void
  (e: 'move-file'): void
  (e: 'rename'): void
  (e: 'detail'): void
  (e: 'acl'): void
  (e: 'tags'): void
  (e: 'versions'): void
  (e: 'delete'): void
}>()

const menuEl = ref<HTMLElement>()

// 打开时聚焦第一项；方向键在菜单内循环导航（无障碍键盘操作）。
watch(() => props.menu, async (m) => {
  if (m) {
    await nextTick()
    menuEl.value?.querySelector<HTMLElement>('button')?.focus()
  }
})

function onMenuKeydown(e: KeyboardEvent) {
  if (!menuEl.value) return
  const btns = Array.from(menuEl.value.querySelectorAll<HTMLElement>('button'))
  if (!btns.length) return
  const idx = btns.indexOf(document.activeElement as HTMLElement)
  if (e.key === 'ArrowDown') { e.preventDefault(); btns[(idx + 1) % btns.length].focus() }
  else if (e.key === 'ArrowUp') { e.preventDefault(); btns[(idx - 1 + btns.length) % btns.length].focus() }
  else if (e.key === 'Home') { e.preventDefault(); btns[0].focus() }
  else if (e.key === 'End') { e.preventDefault(); btns[btns.length - 1].focus() }
}

function menuStyle() {
  const m = props.menu
  if (!m) return {}
  return {
    left: Math.min(m.x, window.innerWidth - 200) + 'px',
    top: Math.min(m.y, window.innerHeight - 240) + 'px',
  }
}
</script>

<template>
  <Teleport to="body">
    <div v-if="menu" ref="menuEl" class="ctx-menu" :style="menuStyle()" role="menu" tabindex="-1" @click.stop @keydown="onMenuKeydown">
      <template v-if="menu.entry.kind === 'folder'">
        <button role="menuitem" @click="emit('open')">{{ t('ctx.openFolder') }}</button>
        <button role="menuitem" @click="emit('copy-key')">{{ t('ctx.copyPath') }}</button>
        <div class="ctx-sep" />
        <button role="menuitem" @click="emit('copy-folder')">{{ t('toolbar.copyTo') }}</button>
        <button role="menuitem" @click="emit('move-folder')">{{ t('toolbar.moveTo') }}</button>
        <button role="menuitem" class="danger" @click="emit('delete-folder')">{{ t('ctx.deleteFolder') }}</button>
      </template>
      <template v-else>
        <button role="menuitem" @click="emit('open')">{{ t('common.download') }}</button>
        <button role="menuitem" @click="emit('preview')">{{ t('objects.preview') }}</button>
        <button role="menuitem" @click="emit('copy-link')">{{ t('ctx.copySignedLink') }}</button>
        <button role="menuitem" @click="emit('copy-key')">{{ t('ctx.copyKey') }}</button>
        <div class="ctx-sep" />
        <button role="menuitem" @click="emit('copy-file')">{{ t('toolbar.copyTo') }}</button>
        <button role="menuitem" @click="emit('move-file')">{{ t('toolbar.moveTo') }}</button>
        <button role="menuitem" @click="emit('rename')">{{ t('common.rename') }}</button>
        <button role="menuitem" @click="emit('detail')">{{ t('ctx.detail') }}</button>
        <button role="menuitem" @click="emit('acl')">{{ t('ctx.acl') }}</button>
        <button role="menuitem" @click="emit('tags')">{{ t('ctx.tags') }}</button>
        <button role="menuitem" @click="emit('versions')">{{ t('ctx.versions') }}</button>
        <div class="ctx-sep" />
        <button role="menuitem" class="danger" @click="emit('delete')">{{ t('common.delete') }}</button>
      </template>
    </div>
  </Teleport>
</template>

<style scoped>
/* 右键菜单 */
.ctx-menu {
  position: fixed; z-index: 150;
  min-width: 190px;
  padding: 6px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-lg);
  animation: pop-in .12s ease;
  display: flex; flex-direction: column;
}
.ctx-menu button {
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px;
  border: none; border-radius: var(--radius-sm);
  background: none;
  color: var(--text); font-size: 13px; font-weight: 500;
  cursor: pointer; text-align: left;
  white-space: nowrap;
}
.ctx-menu button:hover { background: var(--row-hover); color: var(--primary); }
.ctx-menu button.danger { color: var(--danger); }
.ctx-menu button.danger:hover { background: var(--danger-bg); color: var(--danger); }
.ctx-sep { height: 1px; margin: 5px 8px; background: var(--border); }
</style>
