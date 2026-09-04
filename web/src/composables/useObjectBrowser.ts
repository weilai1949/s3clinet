import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, watch } from 'vue'
import { toErrorMessage } from '../errors'

import { s3api } from '../api'
import { state, currentAccount, toast, selectAccount } from '../store'
import { confirmDialog } from '../confirm'
import { t, tf } from '../i18n'
import type { BucketItem, ObjectItem } from '../types'
import type { Entry, SortKey } from '../types'

/** 右键菜单状态（面板传给 ObjectContextMenu，动作/预览经此读取当前条目）。 */
export interface CtxMenu {
  x: number
  y: number
  entry: Entry
}

/**
 * 跨组合式注入的键盘/双击回调：由面板在创建 actions/preview 后回填。
 * 由于监听器在 onMounted 注册、且只在用户输入时触发，回填时刻不影响行为。
 */
export interface KeyBindings {
  previewOrDownload: (o: ObjectItem) => void
  ctxRenameKey: (key: string) => void
  removeSelected: () => void
}

export function useObjectBrowser(bindings: KeyBindings) {
  const prefix = ref('')
  const currentBucket = ref('')
  const buckets = ref<BucketItem[]>([])
  const objects = ref<ObjectItem[]>([])
  const commonPrefixes = ref<string[]>([])
  const nextToken = ref('')
  const isTruncated = ref(false)
  const loading = ref(false)
  const loadingBuckets = ref(false)
  const error = ref('')
  const selected = ref<Set<string>>(new Set())

  /* ---- 本地过滤（只过滤已加载条目，不重新请求） ---- */
  const filter = ref('')

  /* ---- 列排序：文件夹恒在前，文件按列排序 ---- */
  const sortKey = ref<SortKey>('name')
  const sortDir = ref<1 | -1>(1) // 1=升序 -1=降序

  function toggleSort(k: SortKey) {
    if (sortKey.value === k) sortDir.value = sortDir.value === 1 ? -1 : 1
    else {
      sortKey.value = k
      sortDir.value = 1
    }
  }

  /* ---- 右键菜单 ---- */
  const ctxMenu = ref<CtxMenu | null>(null)

  function openCtx(e: MouseEvent, entry: Entry) {
    ctxMenu.value = { x: e.clientX, y: e.clientY, entry }
  }

  /** 从行内「⋯ 更多」按钮打开菜单：锚定按钮下方、右对齐（互联网表格习惯）。 */
  function openCtxFromButton(e: MouseEvent, entry: Entry) {
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    ctxMenu.value = { x: rect.right - 190, y: rect.bottom + 4, entry }
  }

  function closeCtx() {
    ctxMenu.value = null
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') closeCtx()
  }

  const account = computed(() => currentAccount())
  const fileObjects = computed(() => objects.value.filter((o) => !o.isDir))
  const allSelected = computed(() => fileObjects.value.length > 0 && selected.value.size === fileObjects.value.length)

  const entries = computed<Entry[]>(() => {
    const folders: Entry[] = commonPrefixes.value.map((p) => ({
      kind: 'folder',
      key: p,
      name: relName(p, true),
    }))
    const files: Entry[] = fileObjects.value.map((o) => ({
      kind: 'file',
      key: o.key,
      name: relName(o.key, false),
      size: o.size,
      lastModified: o.lastModified,
      object: o,
    }))
    folders.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }))
    // files 不在此排序：唯一消费方 visibleEntries 总会按 sortKey 重排文件
    return [...folders, ...files]
  })

  /* 过滤 + 排序后的可见条目（文件夹恒在前） */
  const visibleEntries = computed<Entry[]>(() => {
    const kw = filter.value.trim().toLowerCase()
    const pool = kw ? entries.value.filter((e) => e.name.toLowerCase().includes(kw)) : entries.value
    const dirs = pool.filter((e) => e.kind === 'folder')
    const files = pool.filter((e) => e.kind === 'file')
    const cmp = (a: Entry, b: Entry): number => {
      let r: number
      if (sortKey.value === 'name') r = a.name.localeCompare(b.name, undefined, { numeric: true })
      else if (sortKey.value === 'size') r = (a.size ?? 0) - (b.size ?? 0)
      else r = (a.lastModified ?? '').localeCompare(b.lastModified ?? '')
      return r * sortDir.value
    }
    files.sort(cmp)
    return [...dirs, ...files]
  })

  const filterActive = computed(() => filter.value.trim() !== '')

  const crumbs = computed(() => {
    const parts = prefix.value.split('/').filter(Boolean)
    return parts.map((name, i) => ({ name, path: parts.slice(0, i + 1).join('/') + '/' }))
  })

  function relName(full: string, isFolder: boolean): string {
    let s = full.startsWith(prefix.value) ? full.slice(prefix.value.length) : full
    if (isFolder) s = s.replace(/\/$/, '')
    return s || full.replace(/\/$/, '').split('/').pop() || full
  }

  async function loadBuckets() {
    const acc = account.value
    if (!acc) return
    const seq = ++loadSeq.value
    loadingBuckets.value = true
    try {
      const res = await s3api.listBuckets(acc.id)
      if (seq !== loadSeq.value) return
      buckets.value = res.buckets ?? []
      // 控制台习惯：currentBucket 失效则回到 Bucket 列表页；有默认桶时自动进入
      if (currentBucket.value && !buckets.value.some((b) => b.name === currentBucket.value)) {
        currentBucket.value = ''
      }
      if (!currentBucket.value && acc.bucket) {
        currentBucket.value = acc.bucket
      }
    } catch (e: unknown) {
      if (seq === loadSeq.value) error.value = e instanceof Error ? e.message : String(e)
    } finally {
      if (seq === loadSeq.value) loadingBuckets.value = false
    }
  }

  /* ---- Bucket 管理（控制台化：列表页 / 创建 / 删除 / 进入） ---- */
  const bucketView = ref<'list' | 'grid'>('list')
  const creatingBucket = ref(false)

  function enterBucket(name: string) {
    prefix.value = ''
    currentBucket.value = name
    load(true)
  }

  function openCreateBucket() {
    creatingBucket.value = true
  }

  async function onCreateBucket({ name }: { name: string; region: string; acl: string }) {
    creatingBucket.value = false
    toast(tf('buckets.toastCreatedNamed', { name }))
    await loadBuckets()
    enterBucket(name)
  }

  async function removeBucket(b: BucketItem) {
    const ok = await confirmDialog({
      title: t('buckets.deleteTitle'),
      message: tf('buckets.deleteConfirm', { name: b.name }),
    })
    if (!ok) return
    try {
      await s3api.deleteBucket(account.value!.id, b.name)
      toast(tf('buckets.deleted', { name: b.name }))
      if (currentBucket.value === b.name) currentBucket.value = ''
      await loadBuckets()
    } catch (e) {
      error.value = toErrorMessage(e)
    }
  }

  /* ---- 统计（对象数 / 总大小 / 选中合计，控制台习惯） ---- */
  const loadedSize = computed(() => fileObjects.value.reduce((s, o) => s + o.size, 0))
  const selectedSize = computed(() => fileObjects.value.filter((o) => selected.value.has(o.key)).reduce((s, o) => s + o.size, 0))

  // 导航序号：进入目录/切桶/重置时 +1；过期响应直接丢弃，避免快速导航串数据。
  const loadSeq = ref(0)

  /** KeepAlive 下仅在面板可见时响应账号切换，避免后台 Tab 重复拉取。 */
  const panelActive = ref(false)
  let syncedAccountId = state.currentAccountId

  async function onAccountSwitch() {
    prefix.value = ''
    currentBucket.value = ''
    syncedAccountId = state.currentAccountId
    if (account.value) {
      await loadBuckets()
      await load(true)
    }
  }

  async function load(reset = true, seqOverride?: number) {
    const acc = account.value
    if (!acc || !currentBucket.value) return
    const seq = seqOverride ?? ++loadSeq.value
    if (reset) loadingAll.value = false
    loading.value = true
    error.value = ''
    try {
      const q: Record<string, string> = {
        bucket: currentBucket.value,
        prefix: prefix.value,
        delimiter: '/',
        maxKeys: '100',
      }
      if (!reset && nextToken.value) q.continuationToken = nextToken.value
      const res = await s3api.listObjects(acc.id, q)
      if (seq !== loadSeq.value) return // 过期响应，丢弃
      if (reset) {
        objects.value = res.objects ?? []
        commonPrefixes.value = res.commonPrefixes ?? []
        selected.value = new Set()
        lastSelIdx.value = -1
      } else {
        objects.value = [...objects.value, ...(res.objects ?? [])]
        commonPrefixes.value = [...new Set([...commonPrefixes.value, ...(res.commonPrefixes ?? [])])]
      }
      nextToken.value = res.nextToken
      isTruncated.value = res.isTruncated
    } catch (e: unknown) {
      if (seq === loadSeq.value) error.value = e instanceof Error ? e.message : String(e)
    } finally {
      if (seq === loadSeq.value) loading.value = false
    }
  }

  function onBucketChange() {
    prefix.value = ''
    load(true)
  }

  function enterPrefix(p: string) {
    prefix.value = p
    load(true)
  }

  function goRoot() {
    prefix.value = ''
    load(true)
  }

  function goUp() {
    const parts = prefix.value.split('/').filter(Boolean)
    parts.pop()
    prefix.value = parts.length ? parts.join('/') + '/' : ''
    load(true)
  }

  function toggle(k: string) {
    const s = new Set(selected.value)
    if (s.has(k)) s.delete(k)
    else s.add(k)
    selected.value = s
  }

  /** 行点击：文件=切换选中，文件夹=进入（文件管理器习惯）。 */
  function onRowClick(e: Entry) {
    if (e.kind === 'folder') enterPrefix(e.key)
    else toggle(e.key)
  }

  function selectAll() {
    if (allSelected.value) {
      selected.value = new Set()
    } else {
      selected.value = new Set(fileObjects.value.map((o) => o.key))
    }
  }

  /* ---- 键盘快捷键（文件管理器习惯；输入框聚焦时不触发） ---- */
  function onGlobalKey(e: KeyboardEvent) {
    // KeepAlive 缓存下监听仍存活：面板被切走后不得用旧 selected 触发删除/预览/全选
    if (!panelActive.value) return
    const t = e.target as HTMLElement | null
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.tagName === 'BUTTON' || t.isContentEditable)) return
    if (!account.value || !currentBucket.value) return
    const files = fileObjects.value.filter((o) => selected.value.has(o.key))
    const first = files[0]
    if (e.key === 'Enter') {
      if (first) {
        e.preventDefault()
        bindings.previewOrDownload(first)
      }
    } else if (e.key === 'F2') {
      if (first) {
        e.preventDefault()
        bindings.ctxRenameKey(first.key)
      }
    } else if (e.key === 'Delete' || e.key === 'Backspace') {
      if (files.length) {
        e.preventDefault()
        bindings.removeSelected()
      }
    } else if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'a') {
      e.preventDefault()
      selectAll()
    }
  }

  /* ---- 账号快速切换（面板头部下拉，无需回账号面板） ---- */
  const accSel = computed({
    get: () => state.currentAccountId,
    set: (v: string) => selectAccount(v),
  })

  /* ---- 快捷键提示条（可关闭） ---- */
  const LS_HINTS = 's3c.hintsHidden'
  const hintsHidden = ref(localStorage.getItem(LS_HINTS) === '1')

  function hideHints() {
    hintsHidden.value = true
    localStorage.setItem(LS_HINTS, '1')
  }

  /* ---- 耗时操作统一 busy 防重复提交 ---- */
  const opsBusy = ref(false)

  /* ---- 面包屑地址栏（可编辑跳转） ---- */
  const pathEditing = ref(false)
  const pathDraft = ref('')

  function startPathEdit() {
    pathDraft.value = prefix.value
    pathEditing.value = true
  }

  function commitPath() {
    pathEditing.value = false
    const p = pathDraft.value.trim()
    enterPrefix(p ? (p.endsWith('/') ? p : p + '/') : '')
  }

  /* ---- Shift 范围多选（文件管理器习惯） ---- */
  const fileList = computed(() => visibleEntries.value.filter((e) => e.kind === 'file'))
  const lastSelIdx = ref(-1)

  function toggleWithShift(k: string, shift: boolean) {
    const idx = fileList.value.findIndex((e) => e.key === k)
    if (shift && lastSelIdx.value >= 0 && idx >= 0) {
      const s = new Set(selected.value)
      const [a, b] = [Math.min(lastSelIdx.value, idx), Math.max(lastSelIdx.value, idx)]
      for (let i = a; i <= b; i++) s.add(fileList.value[i].key)
      selected.value = s
    } else {
      toggle(k)
    }
    lastSelIdx.value = idx
  }

  /** 双击：文件=查看（预览，未知类型下载），文件夹=进入。 */
  function onRowDblClick(e: Entry) {
    if (e.kind === 'folder') enterPrefix(e.key)
    else if (e.object) bindings.previewOrDownload(e.object)
  }

  async function refreshAll() {
    await loadBuckets()
    await load(true)
  }

  /** 加载全部：循环分页直到末尾（上限保护，避免超大桶卡死）。 */
  const loadingAll = ref(false)
  const MAX_ALL_PAGES = 200 // 单页 100 条 → 最多 2 万条

  async function loadAll() {
    const acc = account.value
    if (!acc || !currentBucket.value || loading.value) return
    const seq = ++loadSeq.value
    loadingAll.value = true
    error.value = ''
    try {
      let guard = 0
      while (nextToken.value && guard < MAX_ALL_PAGES) {
        if (seq !== loadSeq.value) break // 期间发生导航，中止
        guard++
        await load(false, seq)
      }
      if (seq !== loadSeq.value) return
      toast(tf('objects.toastLoadedAll', { files: fileObjects.value.length, folders: commonPrefixes.value.length }))
      if (isTruncated.value) toast(tf('objects.toastLoadedCap', { n: guard * 100 }), 'err')
    } catch (err: unknown) {
      if (seq === loadSeq.value) error.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (seq === loadSeq.value) loadingAll.value = false
    }
  }

  /* ---- 工具栏事件转发（供 ObjectToolbar / ObjectList 调用） ---- */
  function onBucketSelect(v: string) {
    currentBucket.value = v
    onBucketChange()
  }

  function backToBuckets() {
    currentBucket.value = ''
    loadBuckets()
  }

  function cancelPathEdit() {
    pathEditing.value = false
  }

  function togglePathEdit() {
    if (pathEditing.value) commitPath()
    else startPathEdit()
  }

  function toggleView() {
    bucketView.value = bucketView.value === 'list' ? 'grid' : 'list'
  }

  function loadMore() {
    load(false)
  }

  watch(() => state.currentAccountId, async () => {
    if (!panelActive.value) return
    await onAccountSwitch()
  })

  onActivated(async () => {
    panelActive.value = true
    if (state.currentAccountId !== syncedAccountId) await onAccountSwitch()
  })

  onDeactivated(() => {
    panelActive.value = false
  })

  onMounted(async () => {
    panelActive.value = true
    syncedAccountId = state.currentAccountId
    window.addEventListener('click', closeCtx)
    window.addEventListener('keydown', onKey)
    window.addEventListener('keydown', onGlobalKey)
    window.addEventListener('blur', closeCtx)
    window.addEventListener('scroll', closeCtx, true)
    if (account.value) {
      await loadBuckets()
      await load(true)
    }
  })

  onBeforeUnmount(() => {
    window.removeEventListener('click', closeCtx)
    window.removeEventListener('keydown', onKey)
    window.removeEventListener('keydown', onGlobalKey)
    window.removeEventListener('blur', closeCtx)
    window.removeEventListener('scroll', closeCtx, true)
  })

  return {
    prefix,
    currentBucket,
    buckets,
    objects,
    commonPrefixes,
    nextToken,
    isTruncated,
    loading,
    loadingBuckets,
    error,
    selected,
    filter,
    sortKey,
    sortDir,
    toggleSort,
    ctxMenu,
    openCtx,
    openCtxFromButton,
    closeCtx,
    onKey,
    account,
    fileObjects,
    allSelected,
    entries,
    visibleEntries,
    filterActive,
    crumbs,
    relName,
    loadBuckets,
    bucketView,
    creatingBucket,
    enterBucket,
    openCreateBucket,
    onCreateBucket,
    removeBucket,
    loadedSize,
    selectedSize,
    load,
    onBucketChange,
    enterPrefix,
    goRoot,
    goUp,
    toggle,
    onRowClick,
    selectAll,
    onGlobalKey,
    accSel,
    hintsHidden,
    hideHints,
    opsBusy,
    pathEditing,
    pathDraft,
    startPathEdit,
    commitPath,
    fileList,
    lastSelIdx,
    toggleWithShift,
    onRowDblClick,
    refreshAll,
    loadingAll,
    loadAll,
    onBucketSelect,
    backToBuckets,
    cancelPathEdit,
    togglePathEdit,
    toggleView,
    loadMore,
  }
}
