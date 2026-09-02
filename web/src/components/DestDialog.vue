<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api, subscribeMigrateEvents, type MigrateProgress } from '../api'
import { toast } from '../store'
import { t, tf } from '../i18n'
import ModalDialog from './ModalDialog.vue'
import type { BucketItem } from '../types'

const props = defineProps<{
  open: boolean
  accountId: string
  sourceBucket: string
  kind: 'file' | 'folder' | 'multi'
  mode: 'copy' | 'move'
  objectKey: string
  keys?: string[]
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit'): void
  (e: 'error', msg: string): void
}>()

const buckets = ref<BucketItem[]>([])
const targetBucket = ref('')
const targetPath = ref('')
const busy = ref(false)

watch(() => props.open, async (o) => {
  if (!o) return
  targetBucket.value = props.sourceBucket
  targetPath.value = props.kind === 'multi' ? '' : props.objectKey
  busy.value = false
  buckets.value = []
  try {
    const res = await s3api.listBuckets(props.accountId)
    buckets.value = res.buckets ?? []
  } catch (err) {
    emit('error', toErrorMessage(err))
  }
})

const title = computed(() => {
  const verb = t(props.mode === 'copy' ? 'dest.verbCopy' : 'dest.verbMove')
  const what =
    props.kind === 'file'
      ? t('dest.whatFile')
      : props.kind === 'folder'
        ? t('dest.whatFolder')
        : t('dest.whatMulti')
  return tf('dest.title', { verb, what })
})

const pathLabel = computed(() => {
  if (props.kind === 'folder') return t('dest.targetPathFolder')
  if (props.kind === 'multi') return t('dest.targetPathMulti')
  return t('dest.targetPath')
})

function modeAction(kind: 'plain' | 'past' | 'ing'): string {
  if (kind === 'past') return t(props.mode === 'copy' ? 'dest.actionCopied' : 'dest.actionMoved')
  if (kind === 'ing') return t(props.mode === 'copy' ? 'dest.actionCopying' : 'dest.actionMoving')
  return t(props.mode === 'copy' ? 'dest.actionCopy' : 'dest.actionMove')
}

function waitMigrateJob(jobId: string, onProgress?: (p: MigrateProgress) => void): Promise<{ ok: number; failed: number }> {
  return new Promise((resolve, reject) => {
    let last = { ok: 0, failed: 0 }
    const stop = subscribeMigrateEvents(
      jobId,
      (p) => {
        last = { ok: p.migrated ?? 0, failed: p.failed ?? 0 }
        onProgress?.(p)
        if (p.status === 'done' || p.status === 'cancelled') {
          stop()
          resolve(last)
        }
      },
      (e) => {
        stop()
        reject(e)
      },
    )
  })
}

async function submitDest() {
  if (busy.value) return
  const bucket = targetBucket.value.trim()
  const path = targetPath.value.trim()
  if (!bucket) {
    emit('error', t('dest.errBucket'))
    return
  }
  // 批量复制/移动允许目标前缀留空（=目标桶根目录）；单文件/文件夹必填
  if (!path && props.kind !== 'multi') {
    emit('error', t('dest.errPath'))
    return
  }
  busy.value = true
  try {
    if (props.kind === 'file') {
      if (props.mode === 'copy') {
        const r = await s3api.copyObject(props.accountId, {
          bucket: props.sourceBucket,
          key: props.objectKey,
          newBucket: bucket,
          newKey: path,
        })
        toast(tf('dest.toastCopiedTo', { path: `${r.bucket}/${r.copied}` }))
      } else {
        const r = await s3api.renameObject(props.accountId, {
          bucket: props.sourceBucket,
          key: props.objectKey,
          newBucket: bucket,
          newKey: path,
        })
        toast(tf('dest.toastMovedTo', { path: r.renamed }))
      }
      emit('submit')
    } else if (props.kind === 'folder') {
      const targetPrefix = path.endsWith('/') ? path : path + '/'
      toast(t('dest.toastCopyingFolder'))
      const start = await s3api.copyPrefixAsync(props.accountId, {
        bucket: props.sourceBucket,
        prefix: props.objectKey,
        targetBucket: bucket,
        targetPrefix,
      })
      const result = await waitMigrateJob(start.jobId)
      if (props.mode === 'copy') {
        toast(
          result.failed
            ? tf('dest.toastCopiedPartial', { n: result.ok, fail: result.failed })
            : tf('dest.toastCopiedN', { n: result.ok }),
        )
      } else {
        if (result.failed) {
          toast(tf('dest.toastMovePartialKeep', { fail: result.failed }), 'err')
          return
        }
        toast(t('dest.toastDeletingFolder'))
        const del = await s3api.deletePrefixAsync(props.accountId, {
          bucket: props.sourceBucket,
          prefix: props.objectKey,
        })
        const d = await waitMigrateJob(del.jobId, (p) => {
          if (p.total > 0) toast(tf('dest.toastDeleteProgress', { done: p.done, total: p.total }))
        })
        toast(
          del.truncated
            ? tf('dest.toastDeletedTruncated', { n: d.ok })
            : tf('dest.toastMovedN', { n: d.ok }),
        )
      }
      emit('submit')
    } else {
      const targetPrefix = path ? (path.endsWith('/') ? path : path + '/') : ''
      toast(tf('dest.toastWorking', { action: modeAction('ing') }))
      const start = await s3api.copyFilesAsync(props.accountId, {
        bucket: props.sourceBucket,
        targetBucket: bucket,
        targetPrefix,
        keys: props.keys ?? [],
        deleteSource: props.mode === 'move',
      })
      const result = await waitMigrateJob(start.jobId)
      const action = modeAction('past')
      toast(
        result.failed
          ? tf('dest.toastDonePartial', { action, n: result.ok, fail: result.failed })
          : tf('dest.toastDone', { action, n: result.ok }),
      )
      emit('submit')
    }
  } catch (err) {
    emit('error', toErrorMessage(err))
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <ModalDialog :open="open" :title="title" width="min(520px, 100%)" @close="emit('close')">
    <div v-if="open" class="mono badge" style="display:block; margin-bottom:10px; word-break:break-all">
      <template v-if="kind === 'multi'">{{ tf('dest.selected', { n: keys?.length ?? 0 }) }}</template>
      <template v-else>{{ tf('dest.source', { key: objectKey }) }}</template>
    </div>
    <div class="grid" style="grid-template-columns:1fr">
      <label class="field">
        {{ t('dest.targetBucket') }}
        <select v-model="targetBucket">
          <option v-if="!buckets.length" value="">{{ t('dest.noBucket') }}</option>
          <option v-for="b in buckets" :key="b.name" :value="b.name">{{ b.name }}</option>
        </select>
      </label>
      <label class="field">
        {{ pathLabel }}
        <input
          v-model="targetPath"
          :placeholder="kind === 'multi' ? t('dest.phMulti') : t('dest.phSingle')"
          autocomplete="off"
          spellcheck="false"
          @keydown.enter.prevent="submitDest"
        />
      </label>
      <div v-if="kind === 'folder'" class="badge" style="line-height:1.6">
        {{ tf('dest.hintFolder', { action: modeAction('past') }) }}
      </div>
      <div v-else-if="kind === 'multi'" class="badge" style="line-height:1.6">
        {{ tf('dest.hintMulti', { action: modeAction('plain') }) }}
      </div>
    </div>
    <div class="row" style="margin-top:16px">
      <button class="btn sm" :disabled="busy" @click="submitDest">
        {{ mode === 'copy' ? t('common.copy') : t('common.move') }}
      </button>
      <button class="btn secondary sm" :disabled="busy" @click="emit('close')">{{ t('common.cancel') }}</button>
    </div>
  </ModalDialog>
</template>
