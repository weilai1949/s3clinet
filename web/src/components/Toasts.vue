<script setup lang="ts">
import { toasts, dismissToast } from '../store'
import { t } from '../i18n'
</script>

<template>
  <div class="toasts" aria-live="polite">
    <div v-for="item in toasts" :key="item.id" class="toast" :class="item.kind" role="status">
      <span class="t-icon" aria-hidden="true">{{ item.kind === 'ok' ? '✓' : '✕' }}</span>
      <span style="flex:1; word-break:break-all">{{ item.text }}</span>
      <button v-if="item.action" class="toast-action" @click="item.action!.onClick()">
        {{ item.action.label }}
      </button>
      <button class="link" style="color:var(--muted)" :aria-label="t('common.close')" @click="dismissToast(item.id)">✕</button>
    </div>
  </div>
</template>
