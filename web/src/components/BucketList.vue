<script setup lang="ts">
import { fmtDate } from '../format'
import { t, tf } from '../i18n'
import type { BucketItem } from '../types'

defineProps<{
  buckets: BucketItem[]
}>()

const emit = defineEmits<{
  (e: 'create'): void
  (e: 'enter', name: string): void
  (e: 'remove', b: BucketItem): void
  (e: 'lifecycle', name: string): void
}>()
</script>

<template>
  <div>
    <div class="toolbar">
      <h3 style="margin:0">{{ t('buckets.listTitle') }}</h3>
      <span class="badge">{{ tf('buckets.countN', { n: buckets.length }) }}</span>
      <span class="spacer" />
      <button class="btn sm" @click="emit('create')">{{ t('buckets.createAction') }}</button>
    </div>
    <div v-if="buckets.length" class="tbl-wrap">
      <table class="tbl">
        <thead>
          <tr>
            <th>{{ t('common.name') }}</th>
            <th style="width:200px">{{ t('buckets.colCreated') }}</th>
            <th style="width:220px; text-align:right">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="b in buckets" :key="b.name">
            <td>
              <span class="bucket-icon" aria-hidden="true">🗄️</span>
              <span class="mono" style="font-weight:600">{{ b.name }}</span>
            </td>
            <td class="muted">{{ fmtDate(b.creationDate) }}</td>
            <td>
              <div class="actions" style="justify-content:flex-end">
                <button class="btn secondary sm" @click="emit('lifecycle', b.name)">{{ t('buckets.tabLifecycle') }}</button>
                <button class="btn sm" @click="emit('enter', b.name)">{{ t('buckets.enter') }}</button>
                <button class="btn danger sm" @click="emit('remove', b)">{{ t('common.delete') }}</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else class="empty">
      <span class="empty-icon" aria-hidden="true">🗄️</span>
      {{ t('buckets.emptyHint') }}
    </div>
  </div>
</template>

<style scoped>
/* Bucket 列表 */
.bucket-icon { margin-right: 6px; opacity: .8; }
</style>
