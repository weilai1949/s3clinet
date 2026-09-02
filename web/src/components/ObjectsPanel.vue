<script setup lang="ts">
import { state, requestAccountForm } from '../store'
import { t, tf } from '../i18n'
import BucketInfoDialog from './BucketInfoDialog.vue'
import VersionsDialog from './VersionsDialog.vue'
import LifecycleDialog from './LifecycleDialog.vue'
import DestDialog from './DestDialog.vue'
import HeadersDialog from './HeadersDialog.vue'
import AclDialog from './AclDialog.vue'
import TagsDialog from './TagsDialog.vue'
import StorageClassDialog from './StorageClassDialog.vue'
import ObjectDetailDialog from './ObjectDetailDialog.vue'
import CreateBucketDialog from './CreateBucketDialog.vue'
import PreviewOverlay from './PreviewOverlay.vue'
import ObjectList from './ObjectList.vue'
import ObjectToolbar from './ObjectToolbar.vue'
import UploadQueue from './UploadQueue.vue'
import BucketList from './BucketList.vue'
import ObjectContextMenu from './ObjectContextMenu.vue'
import { useObjectBrowser, type KeyBindings } from '../composables/useObjectBrowser'
import { useObjectActions } from '../composables/useObjectActions'
import { usePreview } from '../composables/usePreview'

/* ---- 组合式编排：对象浏览（state / 导航 / 选择 / 分页）+ 动作（右键 / 弹窗 / 上传） + 预览 ---- */
const bindings = {} as KeyBindings
const browser = useObjectBrowser(bindings)
const actions = useObjectActions({
  account: browser.account,
  currentBucket: browser.currentBucket,
  selected: browser.selected,
  fileObjects: browser.fileObjects,
  prefix: browser.prefix,
  opsBusy: browser.opsBusy,
  error: browser.error,
  ctxMenu: browser.ctxMenu,
  load: browser.load,
  enterPrefix: browser.enterPrefix,
  closeCtx: browser.closeCtx,
})
const previewComposable = usePreview({
  account: browser.account,
  currentBucket: browser.currentBucket,
  getCtxEntry: () => browser.ctxMenu.value?.entry,
  closeCtx: browser.closeCtx,
  download: actions.download,
})

// 回填键盘/双击依赖（onGlobalKey / onRowDblClick 在用户输入时才触发，此时均已就绪）
bindings.previewOrDownload = previewComposable.previewOrDownload
bindings.ctxRenameKey = actions.ctxRenameKey
bindings.removeSelected = actions.removeSelected

/* ---- 视图绑定的 state / action（模板直接引用） ---- */
const {
  accSel,
  account,
  currentBucket,
  ctxMenu,
  buckets,
  loadingBuckets,
  loading,
  prefix,
  crumbs,
  pathEditing,
  filterActive,
  visibleEntries,
  entries,
  allSelected,
  selected,
  selectedSize,
  fileObjects,
  loadedSize,
  bucketView,
  opsBusy,
  filter,
  pathDraft,
  hintsHidden,
  error,
  sortKey,
  sortDir,
  nextToken,
  isTruncated,
  loadingAll,
  creatingBucket,
  onBucketSelect,
  goRoot,
  enterPrefix,
  goUp,
  startPathEdit,
  commitPath,
  cancelPathEdit,
  togglePathEdit,
  refreshAll,
  backToBuckets,
  selectAll,
  toggleView,
  loadMore,
  loadAll,
  onRowClick,
  onRowDblClick,
  openCtx,
  openCtxFromButton,
  toggleWithShift,
  toggleSort,
  openCreateBucket,
  enterBucket,
  removeBucket,
  onCreateBucket,
  hideHints,
} = browser

const {
  uploading,
  zipLoading,
  uploadQueue,
  uploadInput,
  detail,
  headersOpen,
  headersKey,
  tagsOpen,
  tagsKey,
  lifecycleOpen,
  lifecycleBucket,
  destOpen,
  destCtx,
  aclOpen,
  aclKey,
  bucketInfoOpen,
  versionsOpen,
  versionsKey,
  openBucketInfo,
  pickUploadFiles,
  mkdir,
  openDestMulti,
  copySelectedLinks,
  downloadSelectedZip,
  removeSelected,
  onPickUpload,
  download,
  openLifecycle,
  onHeadersSaved,
  openHeadersDialog,
  openAcl,
  openTagsDialog,
  openVersions,
  storageClassOpen,
  storageClassKey,
  openStorageClass,
  onStorageClassSaved,
  onDestSubmit,
  ctxOpen,
  ctxCopyLink,
  ctxCopyKey,
  ctxCopyFolder,
  ctxMoveFolder,
  ctxDeleteFolder,
  ctxCopyFile,
  ctxMoveFile,
  ctxRename,
  ctxDetail,
  ctxAcl,
  ctxTags,
  ctxVersions,
  ctxDelete,
} = actions

const { preview, showPreview, ctxPreview } = previewComposable
</script>

<template>
  <div class="panel">
    <div class="toolbar">
      <h3 style="margin:0">{{ t('objects.title') }}</h3>
      <span class="spacer" />
      <span class="badge" style="margin-right:2px">{{ t('objects.currentAccount') }}</span>
      <select v-model="accSel" class="acc-select" :title="tf('objects.switchAccount', { n: state.accounts.length })">
        <option v-if="!state.accounts.length" value="">{{ t('objects.noAccountOption') }}</option>
        <option v-for="a in state.accounts" :key="a.id" :value="a.id">{{ a.name }}</option>
      </select>
    </div>

    <div v-if="!account" class="empty">
      <span class="empty-icon" aria-hidden="true">🗂️</span>
      {{ t('objects.noAccountHint') }}
      <div class="row" style="justify-content:center; margin-top:14px">
        <button class="btn sm" @click="requestAccountForm">{{ t('objects.createFirst') }}</button>
      </div>
    </div>

    <template v-else>
      <!-- ===== Bucket 列表页（控制台习惯：先选桶再管理对象） ===== -->
      <div v-if="!currentBucket">
        <BucketList :buckets="buckets" @create="openCreateBucket" @enter="enterBucket" @remove="removeBucket" @lifecycle="openLifecycle" />
      </div>

      <!-- ===== 对象浏览页 ===== -->
      <template v-else>
      <ObjectToolbar
        :bucket="currentBucket"
        :buckets="buckets"
        :loading-buckets="loadingBuckets"
        :loading="loading"
        :prefix="prefix"
        :crumbs="crumbs"
        :path-editing="pathEditing"
        :filter-active="filterActive"
        :visible-count="visibleEntries.length"
        :total-count="entries.length"
        :all-selected="allSelected"
        :selected-count="selected.size"
        :selected-size="selectedSize"
        :file-count="fileObjects.length"
        :loaded-size="loadedSize"
        :bucket-view="bucketView"
        :uploading="uploading"
        :ops-busy="opsBusy"
        :zip-loading="zipLoading"
        v-model:filter="filter"
        v-model:path-draft="pathDraft"
        @bucket-change="onBucketSelect"
        @open-bucket-info="openBucketInfo"
        @go-root="goRoot"
        @enter-prefix="enterPrefix"
        @go-up="goUp"
        @start-path-edit="startPathEdit"
        @commit-path="commitPath"
        @cancel-path-edit="cancelPathEdit"
        @toggle-path-edit="togglePathEdit"
        @refresh="refreshAll"
        @back-to-buckets="backToBuckets"
        @upload="pickUploadFiles"
        @mkdir="mkdir"
        @toggle-select-all="selectAll"
        @open-dest-multi="openDestMulti"
        @copy-links="copySelectedLinks"
        @download-zip="downloadSelectedZip"
        @remove-selected="removeSelected"
        @toggle-view="toggleView"
      />

      <!-- 上传队列（进行中 / 失败） -->
      <UploadQueue :items="uploadQueue" />

      <!-- 上传文件隐藏输入 -->
      <input ref="uploadInput" type="file" multiple style="display:none" @change="onPickUpload" />

      <!-- 快捷键提示条（可关闭） -->
      <div v-if="!hintsHidden" class="hints-bar">
        <span>⌨️ {{ t('objects.hintsLabel') }}</span>
        <kbd>Enter</kbd> {{ t('objects.hintEnter') }}
        <kbd>F2</kbd> {{ t('objects.hintRename') }}
        <kbd>Delete</kbd> {{ t('objects.hintDelete') }}
        <kbd>Ctrl</kbd>+<kbd>A</kbd> {{ t('objects.hintSelectAll') }}
        <span class="spacer" />
        <button class="link" @click="hideHints">{{ t('objects.hintsDismiss') }}</button>
      </div>

      <div v-if="error" class="msg err" style="margin-bottom:10px">
        <span style="flex:1">{{ error }}</span>
        <button class="link" style="flex:none" @click="refreshAll">{{ t('common.retry') }}</button>
      </div>

      <!-- 对象列表（网格 / 表格 / 分页加载） -->
      <ObjectList
        :entries="visibleEntries"
        :bucket-view="bucketView"
        :selected="selected"
        :sort-key="sortKey"
        :sort-dir="sortDir"
        :filter="filter"
        :filter-active="filterActive"
        :loading="loading"
        :total-count="entries.length"
        :visible-count="visibleEntries.length"
        :next-token="nextToken"
        :is-truncated="isTruncated"
        :loading-all="loadingAll"
        @row-click="onRowClick"
        @row-dbl="onRowDblClick"
        @ctx="openCtx"
        @ctx-button="openCtxFromButton"
        @check-toggle="toggleWithShift"
        @enter-dir="enterPrefix"
        @download="download"
        @preview="showPreview"
        @toggle-sort="toggleSort"
        @load-more="loadMore"
        @load-all="loadAll"
      />

      <!-- 对象详情（弹窗） -->
      <ObjectDetailDialog
        :open="!!detail"
        :detail="detail"
        :account-id="account?.id ?? ''"
        :bucket="currentBucket"
        @close="detail = null"
        @edit-headers="openHeadersDialog"
        @open-acl="openAcl"
        @open-tags="openTagsDialog"
        @open-versions="openVersions"
        @open-storage-class="openStorageClass"
        @error="error = $event"
      />
      </template>
    </template>

    <!-- 创建 Bucket 弹窗 -->
    <CreateBucketDialog
      :open="creatingBucket"
      :account-id="account?.id ?? ''"
      @close="creatingBucket = false"
      @created="onCreateBucket"
      @error="error = $event"
    />

    <!-- 编辑 HTTP 头弹窗 -->
    <HeadersDialog
      :open="headersOpen"
      :account-id="account?.id ?? ''"
      :bucket="currentBucket"
      :object-key="headersKey"
      :detail="detail"
      @close="headersOpen = false"
      @saved="onHeadersSaved"
      @error="error = $event"
    />

    <!-- 编辑标签弹窗 -->
    <TagsDialog
      :open="tagsOpen"
      :account-id="account?.id ?? ''"
      :bucket="currentBucket"
      :object-key="tagsKey"
      @close="tagsOpen = false"
      @error="error = $event"
    />

    <!-- 生命周期规则弹窗 -->
    <LifecycleDialog
      :open="lifecycleOpen"
      :account-id="account?.id ?? ''"
      :bucket="lifecycleBucket"
      @close="lifecycleOpen = false"
      @error="error = $event"
    />

    <!-- 复制到 / 移动到（目标桶 + 目标路径，支持跨桶） -->
    <DestDialog
      :open="destOpen"
      :account-id="account?.id ?? ''"
      :source-bucket="currentBucket"
      :kind="destCtx?.kind ?? 'file'"
      :mode="destCtx?.mode ?? 'copy'"
      :object-key="destCtx?.key ?? ''"
      :keys="destCtx?.keys"
      @close="destOpen = false"
      @submit="onDestSubmit"
      @error="error = $event"
    />

    <!-- 对象权限（ACL） -->
    <AclDialog
      :open="aclOpen"
      :account-id="account?.id ?? ''"
      :bucket="currentBucket"
      :object-key="aclKey"
      @close="aclOpen = false"
      @error="error = $event"
    />

    <!-- 对象存储类型（StorageClass）切换 -->
    <StorageClassDialog
      :open="storageClassOpen"
      :account-id="account?.id ?? ''"
      :bucket="currentBucket"
      :object-key="storageClassKey"
      :current-class="detail?.key === storageClassKey ? (detail?.storageClass ?? '') : ''"
      @close="storageClassOpen = false"
      @saved="onStorageClassSaved"
      @error="error = $event"
    />

    <!-- 桶属性（区域 / 创建时间 / 版本控制） -->
    <BucketInfoDialog
      :open="bucketInfoOpen"
      :account-id="account?.id ?? ''"
      :bucket="currentBucket"
      @close="bucketInfoOpen = false"
      @error="error = $event"
    />

    <!-- 对象版本列表（ListObjectVersions） -->
    <VersionsDialog
      :open="versionsOpen"
      :account-id="account?.id ?? ''"
      :bucket="currentBucket"
      :object-key="versionsKey"
      @close="versionsOpen = false"
      @error="error = $event"
    />

    <!-- 安全预览（服务端代理 + 类型分发；文本转义渲染，PDF 沙箱 iframe） -->
    <PreviewOverlay
      :preview="preview"
      :account-id="account?.id ?? ''"
      :bucket="currentBucket"
      @close="preview = null"
      @error="error = $event"
    />

    <!-- 右键菜单 -->
    <ObjectContextMenu
      :menu="ctxMenu"
      @open="ctxOpen"
      @preview="ctxPreview"
      @copy-link="ctxCopyLink"
      @copy-key="ctxCopyKey"
      @copy-folder="ctxCopyFolder"
      @move-folder="ctxMoveFolder"
      @delete-folder="ctxDeleteFolder"
      @copy-file="ctxCopyFile"
      @move-file="ctxMoveFile"
      @rename="ctxRename"
      @detail="ctxDetail"
      @acl="ctxAcl"
      @tags="ctxTags"
      @versions="ctxVersions"
      @delete="ctxDelete"
    />
  </div>
</template>

<style scoped>
.acc-select { max-width: 220px; padding: 5px 10px; font-size: 13px; }

/* 快捷键提示条 */
.hints-bar {
  display: flex; align-items: center; flex-wrap: wrap; gap: 6px;
  padding: 7px 12px; margin-bottom: 10px;
  border: 1px dashed var(--border-strong);
  border-radius: var(--radius);
  background: var(--panel-2);
  font-size: 12px; color: var(--muted);
}
.hints-bar kbd {
  padding: 1px 6px;
  border: 1px solid var(--border-strong);
  border-bottom-width: 2px;
  border-radius: 5px;
  background: var(--panel);
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--text);
}
</style>
