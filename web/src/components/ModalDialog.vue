<script setup lang="ts">
import { nextTick, ref, toRef, watch } from 'vue'
import { useKeydownStack } from '../composables/useKeydownStack'
import { t } from '../i18n'

const props = defineProps<{
  open: boolean
  title: string
  /** 卡片宽度（CSS 值），默认 min(560px, 100%) */
  width?: string
}>()

const emit = defineEmits<{ (e: 'close'): void }>()

const card = ref<HTMLElement>()
const FOCUSABLE = 'a[href], button:not([disabled]), textarea, input, select, [tabindex]:not([tabindex="-1"])'

let previousFocus: HTMLElement | null = null
let previousOverflow = ''

function focusables(): HTMLElement[] {
  if (!card.value) return []
  return Array.from(card.value.querySelectorAll<HTMLElement>(FOCUSABLE)).filter((el) => el.offsetParent !== null)
}

// 打开时保存焦点并锁定滚动；关闭时恢复。打开时把焦点移入对话框（初始焦点：第一个可聚焦元素）。
watch(() => props.open, async (o) => {
  if (o) {
    previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    await nextTick()
    const els = focusables()
    ;(els[0] ?? card.value)?.focus()
  } else {
    document.body.style.overflow = previousOverflow
    if (previousFocus && document.contains(previousFocus)) {
      previousFocus.focus()
    }
    previousFocus = null
  }
})

function onKey(e: KeyboardEvent) {
  if (!props.open) return
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
    return
  }
  // 焦点陷阱：Tab 在对话框内循环，避免焦点逃逸到背后页面。
  if (e.key === 'Tab') {
    const els = focusables()
    if (!els.length) return
    const first = els[0]
    const last = els[els.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault()
      last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault()
      first.focus()
    }
  }
}

useKeydownStack(onKey, toRef(props, 'open'))
</script>

<template>
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="open" class="dlg-backdrop" @click.self="emit('close')">
        <div
          ref="card"
          class="dlg-card"
          :style="{ width: width ?? 'min(560px, 100%)' }"
          role="dialog"
          aria-modal="true"
          :aria-label="title"
          tabindex="-1"
        >
          <div class="dlg-head">
            <h3 class="dlg-title">{{ title }}</h3>
            <button class="dlg-x" :aria-label="t('common.close')" @click="emit('close')">✕</button>
          </div>
          <div class="dlg-body">
            <slot />
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.dlg-backdrop {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(8, 14, 11, .45);
  backdrop-filter: blur(3px);
  display: flex; align-items: center; justify-content: center;
  padding: 20px;
}
.dlg-card {
  max-width: 100%;
  max-height: 88vh;
  display: flex; flex-direction: column;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.dlg-head {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  padding: 14px 18px 0;
}
.dlg-title { margin: 0; font-size: 16px; font-weight: 700; }
.dlg-x {
  flex: none;
  width: 28px; height: 28px;
  border: none; border-radius: 8px;
  background: none;
  color: var(--muted); font-size: 14px;
  cursor: pointer;
  transition: all .15s ease;
}
.dlg-x:hover { background: var(--row-hover); color: var(--text); }
.dlg-body { padding: 14px 18px 18px; overflow: auto; }

.modal-fade-enter-active, .modal-fade-leave-active { transition: opacity .16s ease; }
.modal-fade-enter-active .dlg-card, .modal-fade-leave-active .dlg-card { transition: transform .16s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-from .dlg-card, .modal-fade-leave-to .dlg-card { transform: scale(.96) translateY(6px); }
</style>
