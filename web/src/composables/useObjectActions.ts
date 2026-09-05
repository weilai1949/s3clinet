import { ref } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api, api, subscribeMigrateEvents } from '../api'
import { toast, createProgressToast } from '../store'
import { confirmDialog } from '../confirm'
import { promptDialog } from '../prompt'
import { copyText } from '../clipboard'
import { proxyUrl } from '../proxy'
import { t, tf } from '../i18n'
import { useUploadQueue } from './useUploadQueue'
import type { Account, ObjectItem, ObjectMeta } from '../types'
import type { ComputedRef, Ref } from 'vue'
import type { CtxMenu } from './useObjectBrowser'

/** 动作组合式所需的对象浏览核心（由面板注入，保持 state 单一来源）。 */
export interface ObjectBrowserCtx {
  account: ComputedRef<Account | undefined>
  currentBucket: Ref<string>
  selected: Ref<Set<string>>
  fileObjects: ComputedRef<ObjectItem[]>
  prefix: Ref<string>
  opsBusy: Ref<boolean>
  error: Ref<string>
  ctxMenu: Ref<CtxMenu | null>
  load: (reset?: boolean) => Promise<void>
  enterPrefix: (p: string) => void
  closeCtx: () => void
}

/* ---- 复制到 / 移动到（统一对话框：目标桶 + 目标路径，支持跨桶） ---- */
export interface DestCtx {
  mode: 'copy' | 'move'
  kind: 'file' | 'folder' | 'multi'
  key: string
  keys?: string[] // multi：所选文件 key 列表
}

export function useObjectActions(ctx: ObjectBrowserCtx) {
  const detail = ref<ObjectMeta | null>(null)
  const detailSeq = ref(0) // showDetail 序号守卫，过期响应丢弃

  /**
   * 解析当前账号 id。所有"非空断言"集中在这里：
   * 业务上 useObjectActions 仅在面板活跃且有账号时被调用；
   * 一旦没有账号（被卸载 / 切走），把异常交给上游 toast 处理而不是悄悄断言。
   */
  function requireAccId(): string {
    const id = ctx.account.value?.id
    if (!id) throw new Error('no active account')
    return id
  }

  /* ---- 编辑对象 HTTP 头（控制台共性：设置 Content-Type / 自定义元数据） ---- */
  const headersOpen = ref(false)
  const headersKey = ref('')

  function openHeadersDialog(key: string) {
    headersKey.value = key
    headersOpen.value = true
  }

  function onHeadersSaved() {
    headersOpen.value = false
    if (detail.value?.key === headersKey.value) showDetail(headersKey.value)
  }

  /* ---- 对象标签（Tagging） ---- */
  const tagsOpen = ref(false)
  const tagsKey = ref('')

  function ctxTags() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file') openTagsDialog(e.key)
  }

  function openTagsDialog(key: string) {
    tagsKey.value = key
    tagsOpen.value = true
  }

  /* ---- 桶属性（区域 / 创建时间 / 版本控制） ---- */
  const bucketInfoOpen = ref(false)

  function openBucketInfo() {
    bucketInfoOpen.value = true
  }

  /* ---- 对象版本列表（ListObjectVersions） ---- */
  const versionsOpen = ref(false)
  const versionsKey = ref('')

  function ctxVersions() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file') openVersions(e.key)
  }

  function openVersions(key: string) {
    versionsKey.value = key
    versionsOpen.value = true
  }

  /* ---- 对象存储类型（StorageClass）切换 ---- */
  const storageClassOpen = ref(false)
  const storageClassKey = ref('')

  function openStorageClass(key: string) {
    storageClassKey.value = key
    storageClassOpen.value = true
  }

  async function onStorageClassSaved() {
    storageClassOpen.value = false
    await ctx.load(true)
    // 若详情仍打开且是同一对象，刷新存储类型展示
    if (detail.value?.key === storageClassKey.value) {
      await showDetail(storageClassKey.value)
    }
  }

  /* ---- 生命周期规则（控制台共性：前缀过期删除） ---- */
  const lifecycleOpen = ref(false)
  const lifecycleBucket = ref('')

  function openLifecycle(bucket: string) {
    lifecycleBucket.value = bucket
    lifecycleOpen.value = true
  }

  /* ---- object 耗时操作统一 busy 防重复提交 ---- */
  async function removeSelected() {
    const keys = [...ctx.selected.value]
    if (!keys.length) return
    const ok = await confirmDialog({
      title: t('objects.deleteTitle'),
      message: tf('objects.deleteSelectedConfirm', { n: keys.length }),
    })
    if (!ok) return
    try {
      const r = await s3api.deleteObjects(requireAccId(), { bucket: ctx.currentBucket.value, keys })
      toast(tf('objects.toastDeleted', { n: r.deleted }))
      ctx.selected.value = new Set()
      await ctx.load(true)
    } catch (e) {
      ctx.error.value = toErrorMessage(e)
    }
  }

  async function removeOne(key: string) {
    const ok = await confirmDialog({
      title: t('objects.deleteTitle'),
      message: tf('objects.deleteOneConfirm', { key }),
    })
    if (!ok) return
    try {
      const r = await s3api.deleteObjects(requireAccId(), { bucket: ctx.currentBucket.value, keys: [key] })
      toast(tf('objects.toastDeleted', { n: r.deleted }))
      ctx.selected.value = new Set()
      await ctx.load(true)
    } catch (e) {
      ctx.error.value = toErrorMessage(e)
    }
  }

  // download 通过服务端代理下载：服务器强制 Content-Disposition: attachment，
  // 浏览器直接保存文件，恶意内容（HTML/SVG 脚本等）不会被渲染执行。
  function download(o: ObjectItem) {
    const a = document.createElement('a')
    a.href = proxyUrl(requireAccId(), ctx.currentBucket.value, 'download', o.key, api.base)
    a.download = o.key.split('/').pop() ?? 'object'
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
  }

  /** 生成 1 小时签名链接并复制（右键菜单用）。 */
  async function copySignLink(o: ObjectItem) {
    try {
      const res = await s3api.presign(requireAccId(), {
        method: 'get',
        key: o.key,
        bucket: ctx.currentBucket.value,
        expiresIn: 3600,
      })
      copyTextAndToast(res.url)
    } catch (e) {
      ctx.error.value = toErrorMessage(e)
    }
  }

  function copyTextAndToast(text: string, msg = t('objects.copiedClipboard')) {
    copyText(text).then(() => toast(msg))
  }

  /* 右键菜单动作 */
  function ctxOpen() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (!e) return
    if (e.kind === 'folder') ctx.enterPrefix(e.key)
    else if (e.object) download(e.object)
  }

  function ctxCopyKey() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e) copyTextAndToast(e.key)
  }

  function ctxCopyLink() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file' && e.object) copySignLink(e.object)
  }

  function ctxDelete() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file') removeOne(e.key)
  }

  /** 右键菜单入口：重命名 / 移动。 */
  function ctxRename() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file') ctxRenameKey(e.key)
  }

  /** 重命名 / 移动：复制到新 key 后删除源（同账号同桶，由后端保证先复制后删除）。 */
  async function ctxRenameKey(key: string) {
    const newKey = await promptDialog({
      title: t('objects.renameTitle'),
      label: t('objects.renameLabel'),
      initial: key,
      confirmText: t('common.save'),
      validate: (v) => (v.trim() ? null : t('objects.renameEmpty')),
    })
    if (newKey == null) return
    const target = newKey.trim()
    if (target === key) return
    try {
      const r = await s3api.renameObject(requireAccId(), {
        bucket: ctx.currentBucket.value,
        key,
        newKey: target,
      })
      toast(tf('objects.toastRenamed', { key: r.renamed }))
      await ctx.load(true)
    } catch (err) {
      ctx.error.value = toErrorMessage(err)
    }
  }

  /** 对象详情（HeadObject）。 */
  function ctxDetail() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e) showDetail(e.key)
  }

  async function showDetail(key: string) {
    // seq 守卫：快速切对象时丢弃过期响应，避免旧详情覆盖新详情。
    const seq = ++detailSeq.value
    detail.value = null
    try {
      const got = await s3api.headObject(requireAccId(), {
        bucket: ctx.currentBucket.value,
        key,
      })
      if (seq !== detailSeq.value) return
      detail.value = got
    } catch (err) {
      if (seq === detailSeq.value) ctx.error.value = toErrorMessage(err)
    }
  }

  /* ---- 对象权限（ACL） ---- */
  const aclOpen = ref(false)
  const aclKey = ref('')

  function ctxAcl() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file') openAcl(e.key)
  }

  function openAcl(key: string) {
    aclKey.value = key
    aclOpen.value = true
  }

  /** 新建文件夹：PUT 空对象（key 以 / 结尾）。 */
  async function mkdir() {
    const name = await promptDialog({
      title: t('objects.mkdirTitle'),
      label: t('objects.mkdirLabel'),
      placeholder: t('objects.mkdirPh'),
      confirmText: t('common.create'),
      validate: (v) => (v.trim() ? null : t('objects.mkdirEmpty')),
    })
    if (name == null) return
    const key = (ctx.prefix.value + name.trim().replace(/\/+$/, '') + '/').replace(/^\/+/, '')
    try {
      const r = await s3api.mkdirObject(requireAccId(), { bucket: ctx.currentBucket.value, key })
      toast(tf('objects.toastMkdir', { key: r.created }))
      await ctx.load(true)
    } catch (err) {
      ctx.error.value = toErrorMessage(err)
    }
  }

  /* ---- 上传到当前目录（共享 useUploadQueue 状态机；cancelled 为用户中止终态） ---- */
  const uploadInput = ref<HTMLInputElement>()
  const queue = useUploadQueue({
    target: (it) => ({ accId: requireAccId(), bucket: it.bucket || ctx.currentBucket.value, key: it.key }),
  })
  const uploadQueue = queue.items
  const uploading = queue.running
  const abortUploadItem = queue.abortItem
  const abortAllUploads = queue.abortAll

  function keyForUpload(name: string): string {
    return (ctx.prefix.value ? ctx.prefix.value.replace(/\/+$/, '') + '/' : '') + name
  }

  function pickUploadFiles() {
    if (uploading.value) return
    uploadInput.value?.click()
  }

  function onPickUpload(e: Event) {
    const files = (e.target as HTMLInputElement).files
    queue.enqueue(files, (f) => ({ key: keyForUpload(f.name), bucket: ctx.currentBucket.value }))
    ;(e.target as HTMLInputElement).value = ''
    runUpload()
  }

  async function runUpload() {
    if (uploading.value || !ctx.account.value) return
    try {
      await queue.run()
      const failed = uploadQueue.value.filter((it) => it.status === 'err').length
      if (failed) toast(tf('upload.toastPartial', { ok: uploadQueue.value.length - failed, fail: failed }), 'err')
      else toast(tf('objects.toastUploadOkDir', { n: uploadQueue.value.length }))
    } finally {
      uploadQueue.value = []
      uploading.value = false
      await ctx.load(true)
    }
  }

  /** 批量复制所选文件的 1 小时签名链接（每行一个）。 */
  async function copySelectedLinks() {
    const keys = ctx.fileObjects.value.filter((o) => ctx.selected.value.has(o.key))
    if (!keys.length) return
    try {
      const urls: string[] = []
      for (const o of keys) {
        const res = await s3api.presign(requireAccId(), {
          method: 'get',
          key: o.key,
          bucket: ctx.currentBucket.value,
          expiresIn: 3600,
        })
        urls.push(res.url)
      }
      copyTextAndToast(urls.join('\n'), tf('objects.toastCopiedLinks', { n: urls.length }))
    } catch (err) {
      ctx.error.value = toErrorMessage(err)
    }
  }

  /** 下载所选为 ZIP：优先流式落盘，避免大包 OOM。 */
  const zipLoading = ref(false)

  async function downloadSelectedZip() {
    const keys = ctx.fileObjects.value.filter((o) => ctx.selected.value.has(o.key)).map((o) => o.key)
    if (!keys.length) return
    zipLoading.value = true
    try {
      const name = `objects-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, '-')}.zip`
      await s3api.downloadZipToDisk(requireAccId(), { bucket: ctx.currentBucket.value, keys }, name)
      toast(tf('objects.toastZipped', { n: keys.length }))
    } catch (err) {
      // 用户取消「另存为」不提示错误
      if (err instanceof DOMException && err.name === 'AbortError') return
      ctx.error.value = toErrorMessage(err)
    } finally {
      zipLoading.value = false
    }
  }

  /** 复制 / 移动文件夹（递归）：打开统一目标选择对话框（支持跨桶）。 */
  function ctxCopyFolder() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'folder') openDest('copy', 'folder', e.key)
  }

  function ctxMoveFolder() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'folder') openDest('move', 'folder', e.key)
  }

  /** 删除文件夹（递归，含全部内容；大前缀走异步任务 + SSE 进度）。 */
  async function ctxDeleteFolder() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (!e || e.kind !== 'folder' || ctx.opsBusy.value) return
    const ok = await confirmDialog({
      title: t('dest.deleteFolderTitle'),
      message: tf('dest.deleteFolderConfirm', { key: e.key }),
    })
    if (!ok) return
    ctx.opsBusy.value = true
    try {
      toast(t('dest.toastDeletingFolder'))
      const start = await s3api.deletePrefixAsync(requireAccId(), {
        bucket: ctx.currentBucket.value,
        prefix: e.key,
      })
      // 进度 toast 节流（至多 500ms 一条，就地更新）；终态事件由下方完成 toast 立即发出
      const delProgress = createProgressToast()
      const deleted = await new Promise<number>((resolve, reject) => {
        let n = 0
        const stop = subscribeMigrateEvents(
          start.jobId,
          (p) => {
            n = p.migrated ?? 0
            if (p.total > 0 && p.status !== 'done' && p.status !== 'cancelled') {
              delProgress(tf('dest.toastDeleteProgress', { done: p.done, total: p.total }))
            }
            if (p.status === 'done' || p.status === 'cancelled') {
              stop()
              resolve(n)
            }
          },
          (err) => {
            stop()
            reject(err)
          },
        )
      })
      toast(
        start.truncated
          ? tf('dest.toastDeletedTruncated', { n: deleted })
          : tf('dest.toastDeletedN', { n: deleted }),
      )
      await ctx.load(true)
    } catch (err) {
      ctx.error.value = toErrorMessage(err)
    } finally {
      ctx.opsBusy.value = false
    }
  }

  const destOpen = ref(false)
  const destCtx = ref<DestCtx | null>(null)

  function openDest(mode: DestCtx['mode'], kind: DestCtx['kind'], key: string) {
    destCtx.value = { mode, kind, key }
    destOpen.value = true
  }

  /** 批量复制/移动所选文件（工具栏入口）。 */
  function openDestMulti(mode: DestCtx['mode']) {
    const keys = ctx.fileObjects.value.filter((o) => ctx.selected.value.has(o.key)).map((o) => o.key)
    if (!keys.length) return
    destCtx.value = { mode, kind: 'multi', key: '', keys }
    destOpen.value = true
  }

  function onDestSubmit() {
    destOpen.value = false
    ctx.load(true)
  }

  /** 文件复制 / 移动（右键菜单入口）。 */
  function ctxCopyFile() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file') openDest('copy', 'file', e.key)
  }

  function ctxMoveFile() {
    const e = ctx.ctxMenu.value?.entry
    ctx.closeCtx()
    if (e?.kind === 'file') openDest('move', 'file', e.key)
  }

  return {
      detail,
    headersOpen,
    headersKey,
    openHeadersDialog,
    onHeadersSaved,
    tagsOpen,
    tagsKey,
    ctxTags,
    openTagsDialog,
    bucketInfoOpen,
    openBucketInfo,
    versionsOpen,
    versionsKey,
    ctxVersions,
    openVersions,
    storageClassOpen,
    storageClassKey,
    openStorageClass,
    onStorageClassSaved,
    lifecycleOpen,
    lifecycleBucket,
    openLifecycle,
    removeSelected,
    removeOne,
    download,
    copySignLink,
    copyTextAndToast,
    ctxOpen,
    ctxCopyKey,
    ctxCopyLink,
    ctxDelete,
    ctxRename,
    ctxRenameKey,
    ctxDetail,
    showDetail,
    aclOpen,
    aclKey,
    ctxAcl,
    openAcl,
    mkdir,
    uploadInput,
    uploadQueue,
    uploading,
    abortUploadItem,
    abortAllUploads,
    keyForUpload,
    pickUploadFiles,
    onPickUpload,
    runUpload,
    copySelectedLinks,
    zipLoading,
    downloadSelectedZip,
    ctxCopyFolder,
    ctxMoveFolder,
    ctxDeleteFolder,
    destOpen,
    destCtx,
    openDest,
    openDestMulti,
    onDestSubmit,
    ctxCopyFile,
    ctxMoveFile,
  }
}
