<script setup lang="ts">
import { ref } from 'vue'

import { s3api } from '../api'
import type { CorsRule } from '../types'
import { t, tf } from '../i18n'
import { useBucketSetting } from '../composables/useBucketSetting'

const props = defineProps<{
  accountId: string
  bucket: string
}>()

const emit = defineEmits<{
  (e: 'error', msg: string): void
  (e: 'changed'): void
}>()

const rules = ref<CorsRule[]>([])

const METHOD_OPTIONS = ['GET', 'PUT', 'POST', 'DELETE', 'HEAD']

function addRule() {
  rules.value.push({ id: '', allowedMethods: ['GET'], allowedOrigins: ['*'], allowedHeaders: ['*'], exposeHeaders: [], maxAgeSeconds: 3600 })
}

const { loading, saving, save } = useBucketSetting({
  bucket: () => props.bucket,
  onError: (m) => emit('error', m),
  onChanged: () => emit('changed'),
  load: async () => {
    const r = await s3api.getBucketCors(props.accountId, props.bucket)
    rules.value = (r.rules ?? []).map((x) => ({ ...x, allowedMethods: x.allowedMethods ?? [], allowedOrigins: x.allowedOrigins ?? [] }))
    if (!rules.value.length) addRule()
  },
})

function removeRule(i: number) {
  rules.value.splice(i, 1)
}

function toggleMethod(i: number, m: string) {
  const arr = rules.value[i].allowedMethods
  const ix = arr.indexOf(m)
  if (ix >= 0) arr.splice(ix, 1)
  else arr.push(m)
}

function splitList(s: string): string[] {
  return s.split(',').map((x) => x.trim()).filter(Boolean)
}

function joinList(a?: string[]): string {
  return (a ?? []).join(', ')
}

async function clear() {
  await save(async () => {
    await s3api.deleteBucketCors(props.accountId, props.bucket)
    rules.value = []
    addRule()
  }, t('cors.toastCleared'))
}

async function saveCors() {
  await save(async () => {
    await s3api.putBucketCors(props.accountId, { bucket: props.bucket, rules: rules.value })
  }, t('cors.toastSaved'))
}
</script>

<template>
  <div v-if="loading" class="empty" style="padding:20px">{{ t('cors.loading') }}</div>
  <div v-else>
    <div v-for="(r, i) in rules" :key="i" class="cors-rule">
      <div class="row" style="gap:8px; align-items:center">
        <span class="badge">{{ tf('cors.ruleN', { n: i + 1 }) }}</span>
        <input v-model="r.id" class="mono" :placeholder="t('cors.ruleIdPh')" style="flex:1" />
        <button class="btn secondary sm" @click="removeRule(i)">{{ t('common.remove') }}</button>
      </div>
      <label class="field">
        {{ t('cors.methods') }}
        <div class="chips">
          <label v-for="m in METHOD_OPTIONS" :key="m" class="chip">
            <input type="checkbox" :checked="r.allowedMethods.includes(m)" @change="toggleMethod(i, m)" /> {{ m }}
          </label>
        </div>
      </label>
      <label class="field">
        {{ t('cors.origins') }}
        <input :value="joinList(r.allowedOrigins)" @input="r.allowedOrigins = splitList(($event.target as HTMLInputElement).value)" class="mono" :placeholder="t('cors.originsPh')" />
      </label>
      <label class="field">
        {{ t('cors.headers') }}
        <input :value="joinList(r.allowedHeaders)" @input="r.allowedHeaders = splitList(($event.target as HTMLInputElement).value)" class="mono" :placeholder="t('cors.headersPh')" />
      </label>
      <label class="field">
        {{ t('cors.expose') }}
        <input :value="joinList(r.exposeHeaders)" @input="r.exposeHeaders = splitList(($event.target as HTMLInputElement).value)" class="mono" :placeholder="t('cors.exposePh')" />
      </label>
      <label class="field">
        {{ t('cors.maxAge') }}
        <input type="number" v-model.number="r.maxAgeSeconds" class="mono" />
      </label>
    </div>
    <div class="row" style="margin-top:12px">
      <button class="btn secondary sm" @click="addRule">{{ t('cors.addRule') }}</button>
      <button class="btn sm" :disabled="saving" @click="saveCors">{{ saving ? t('common.saving') : t('common.save') }}</button>
      <button class="btn danger sm" :disabled="saving" @click="clear">{{ t('cors.clearAll') }}</button>
    </div>
  </div>
</template>

<style scoped>
.cors-rule { border: 1px solid var(--border); border-radius: var(--radius); padding: 12px; margin-bottom: 12px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 4px; }
.chip { display: inline-flex; align-items: center; gap: 4px; font-size: 12px; }
.field { display: block; margin-bottom: 8px; }
</style>
