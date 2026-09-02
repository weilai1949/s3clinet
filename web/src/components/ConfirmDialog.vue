<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { confirmState, settleConfirm } from '../confirm'
import { isTopKeydown, useKeydownStack } from '../composables/useKeydownStack'
import { t } from '../i18n'

const focusBtn = ref<HTMLButtonElement>()

function onKey(e: KeyboardEvent) {
  if (!confirmState.open) return
  if (e.key === 'Escape') {
    e.preventDefault()
    settleConfirm(false)
  } else if (e.key === 'Enter' && isTopKeydown(onKey)) {
    e.preventDefault()
    settleConfirm(true)
  }
}

useKeydownStack(onKey, computed(() => confirmState.open))

watch(
  () => confirmState.open,
  async (open) => {
    if (open) await nextTick()
    if (open) focusBtn.value?.focus()
  },
)
</script>

<template>
  <Teleport to="body">
    <Transition name="modal-fade">
      <div v-if="confirmState.open" class="modal-backdrop" @click.self="settleConfirm(false)">
        <div class="modal-card" role="alertdialog" aria-modal="true" :aria-label="confirmState.title">
          <div class="modal-icon" :class="confirmState.danger ? 'danger' : ''" aria-hidden="true">
            <svg v-if="confirmState.danger" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z" />
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M9.879 7.519c1.171-1.025 3.071-1.025 4.242 0 1.172 1.025 1.172 2.687 0 3.712-.203.179-.43.326-.67.442-.745.361-1.45.999-1.45 1.827v.75M21 12a9 9 0 11-18 0 9 9 0 0118 0zm-9 5.25h.008v.008H12v-.008z" />
            </svg>
          </div>
          <h3 class="modal-title">{{ confirmState.title }}</h3>
          <p class="modal-msg">{{ confirmState.message }}</p>
          <div class="modal-actions">
            <button ref="focusBtn" class="btn sm" :class="confirmState.danger ? 'danger' : ''" @click="settleConfirm(true)">
              {{ confirmState.confirmText ?? (confirmState.danger ? t('common.delete') : t('common.ok')) }}
            </button>
            <button class="btn secondary sm" @click="settleConfirm(false)">{{ t('common.cancel') }}</button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
.modal-backdrop {
  position: fixed; inset: 0; z-index: 200;
  background: rgba(8, 14, 11, .45);
  backdrop-filter: blur(3px);
  display: flex; align-items: center; justify-content: center;
  padding: 20px;
}
.modal-card {
  width: min(400px, 100%);
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: var(--radius-xl);
  padding: 24px;
  box-shadow: var(--shadow-lg);
  text-align: center;
}
.modal-icon {
  width: 52px; height: 52px; margin: 0 auto 14px;
  border-radius: 16px;
  display: flex; align-items: center; justify-content: center;
  background: var(--primary-soft); color: var(--primary);
}
.modal-icon svg { width: 26px; height: 26px; }
.modal-icon.danger { background: var(--danger-bg); color: var(--danger); }
.modal-title { margin: 0 0 8px; font-size: 16px; font-weight: 700; }
.modal-msg { margin: 0 0 20px; color: var(--muted); font-size: 13px; line-height: 1.6; word-break: break-all; }
.modal-actions { display: flex; justify-content: center; gap: 10px; }

.modal-fade-enter-active, .modal-fade-leave-active { transition: opacity .16s ease; }
.modal-fade-enter-active .modal-card, .modal-fade-leave-active .modal-card { transition: transform .16s ease; }
.modal-fade-enter-from, .modal-fade-leave-to { opacity: 0; }
.modal-fade-enter-from .modal-card, .modal-fade-leave-to .modal-card { transform: scale(.96) translateY(6px); }
</style>
