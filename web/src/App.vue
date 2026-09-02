<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { toErrorMessage } from './errors'

import { s3api, api } from './api'
import logoUrl from './assets/logo.svg'
import { state, rememberedAccountId, selectAccount, tabRequest, accountFormRequest } from './store'
import { readTheme, resolvedTheme, cycleTheme, systemThemeTick, type Theme } from './theme'
import { tabFromHash, setTabHash, onTabHashChange, type TabKey } from './router'
import { t, cycleLocale, i18nState } from './i18n'
import AccountsPanel from './components/AccountsPanel.vue'
import ObjectsPanel from './components/ObjectsPanel.vue'
import UploadPanel from './components/UploadPanel.vue'
import MigratePanel from './components/MigratePanel.vue'
import BucketsPanel from './components/BucketsPanel.vue'
import RecycleBinPanel from './components/RecycleBinPanel.vue'
import ServerPanel from './components/ServerPanel.vue'
import ConfirmDialog from './components/ConfirmDialog.vue'
import PromptDialog from './components/PromptDialog.vue'
import Toasts from './components/Toasts.vue'

const tab = ref<TabKey>(tabFromHash() ?? 'accounts')
const serverError = ref('')
const theme = ref<Theme>(readTheme())

const connected = computed(() => !serverError.value)

const themeIcon = computed(() => {
  // 依赖 systemThemeTick 使系统主题变化时图标响应式刷新
  void systemThemeTick.value
  return resolvedTheme() === 'dark' ? 'moon' : 'sun'
})

const themeLabel = computed(() => {
  void i18nState.locale
  const map: Record<Theme, string> = {
    auto: t('theme.auto'),
    light: t('theme.light'),
    dark: t('theme.dark'),
  }
  return `${map[theme.value]}（${t('theme.cycleHint')}）`
})

const localeLabel = computed(() => (i18nState.locale === 'zh-CN' ? t('locale.zh') : t('locale.en')))

/* 左侧主导航：按依赖关系分组 —— 数据操作（依赖账号）在前、配置（账号/服务器）在后。 */
type MenuIcon = 'key' | 'folder' | 'upload' | 'swap' | 'bucket' | 'trash' | 'server'
interface MenuItem { key: TabKey; label: string; icon: MenuIcon }
interface MenuGroup { label: string; items: MenuItem[] }

const menuGroups = computed<MenuGroup[]>(() => {
  void i18nState.locale
  return [
    {
      label: t('nav.data'),
      items: [
        { key: 'objects', label: t('nav.objects'), icon: 'folder' },
        { key: 'buckets', label: t('nav.buckets'), icon: 'bucket' },
        { key: 'trash', label: t('nav.trash'), icon: 'trash' },
        { key: 'upload', label: t('nav.upload'), icon: 'upload' },
        { key: 'migrate', label: t('nav.migrate'), icon: 'swap' },
      ],
    },
    {
      label: t('nav.config'),
      items: [
        { key: 'accounts', label: t('nav.accounts'), icon: 'key' },
        { key: 'server', label: t('nav.server'), icon: 'server' },
      ],
    },
  ]
})

function onCycleLocale() {
  cycleLocale()
}

function onCycleTheme() {
  theme.value = cycleTheme()
}

// 仅首次加载账号时按依赖路由一次：有账号则直接进入「对象管理」，否则停留在「账号管理」。
let initialRouted = false

async function loadAccounts() {
  const first = !initialRouted
  initialRouted = true
  try {
    const res = await s3api.listAccounts()
    state.accounts = res.accounts
    // 优先恢复上次选中的账号；其次沿用当前；否则取第一个
    const remembered = rememberedAccountId()
    if (state.accounts.some((a) => a.id === remembered)) {
      selectAccount(remembered)
    } else if (state.currentAccountId && state.accounts.some((a) => a.id === state.currentAccountId)) {
      // 保持当前
    } else {
      selectAccount(state.accounts[0]?.id ?? '')
    }
    serverError.value = ''
    if (first) {
      const hashTab = tabFromHash()
      tab.value = hashTab ?? (state.accounts.length ? 'objects' : 'accounts')
      if (!hashTab) setTabHash(tab.value)
    }
  } catch (e) {
    serverError.value = toErrorMessage(e)
  }
}

onMounted(loadAccounts)

let stopHashListen: (() => void) | undefined
onMounted(() => {
  stopHashListen = onTabHashChange((t) => {
    if (t) tab.value = t
  })
})
onUnmounted(() => stopHashListen?.())

function switchTab(t: TabKey) {
  tab.value = t
  setTabHash(t)
  // 面板用 KeepAlive 保持状态（目录/筛选/选中），账号信息通过 @changed 与 watcher 刷新，无需切页重拉。
}

// 跨面板跳转：任何组件 requestTab() 都会切换到对应 tab
watch(
  () => tabRequest.seq,
  () => {
    if (tabRequest.tab) switchTab(tabRequest.tab as TabKey)
  },
)

// 账号表单自动打开信号：空状态引导「创建第一个账号」
watch(
  () => accountFormRequest.seq,
  () => switchTab('accounts'),
)
</script>

<template>
  <div>
    <header class="top">
      <div class="brand">
        <img class="logo" :src="logoUrl" width="30" height="30" alt="S3 Client" />
        <span class="name">S3 Client</span>
        <span class="tag" v-if="api.isTauri">Desktop</span>
      </div>
      <div class="settings">
        <button class="theme-btn" :title="localeLabel" :aria-label="localeLabel" @click="onCycleLocale">{{ localeLabel }}</button>
        <button class="theme-btn" :title="themeLabel" :aria-label="themeLabel" @click="onCycleTheme">
          <svg v-if="themeIcon === 'moon'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M21.752 15.002A9.718 9.718 0 0118 15.75c-5.385 0-9.75-4.365-9.75-9.75 0-1.33.266-2.597.748-3.752A9.753 9.753 0 003 11.25C3 16.635 7.365 21 12.75 21a9.753 9.753 0 009.002-5.998z" />
          </svg>
          <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M12 3v2.25m6.364.386l-1.591 1.591M21 12h-2.25m-.386 6.364l-1.591-1.591M12 18.75V21m-4.773-4.227l-1.591 1.591M5.25 12H3m4.227-4.773L5.636 5.636M15.75 12a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0z" />
          </svg>
        </button>
        <span class="conn" :class="connected ? 'ok' : 'bad'" :title="connected ? t('conn.ok') : serverError">
          <span class="dot" />{{ connected ? (api.getActiveServer()?.name || t('conn.ok')) : t('conn.bad') }}
        </span>
      </div>
    </header>

    <div v-if="serverError" class="msg err" style="margin:14px 22px 0">
      {{ t('conn.fail') }}：{{ serverError }}
      <button class="btn secondary sm" style="margin-left:10px" @click="switchTab('server')">{{ t('conn.gotoServer') }}</button>
    </div>

    <div class="layout">
      <nav class="tabs" :aria-label="t('nav.aria')">
        <div v-for="g in menuGroups" :key="g.label" class="nav-group">
          <div class="nav-group-label">{{ g.label }}</div>
          <button
            v-for="t in g.items"
            :key="t.key"
            :class="{ active: tab === t.key }"
            :aria-current="tab === t.key ? 'page' : undefined"
            @click="switchTab(t.key)"
          >
            <svg v-if="t.icon === 'key'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M15.75 5.25a3 3 0 013 3m3 0a6 6 0 01-7.029 5.912c-.563-.097-1.159.026-1.563.43L10.5 17.25H8.25v2.25H6v2.25H2.25v-2.818c0-.597.237-1.17.659-1.591l6.499-6.499c.404-.404.527-1 .43-1.563A6 6 0 1121.75 8.25z" />
            </svg>
            <svg v-else-if="t.icon === 'folder'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M2.25 12.75V12A2.25 2.25 0 014.5 9.75h15A2.25 2.25 0 0121.75 12v.75m-8.69-6.44l-2.12-2.12a1.5 1.5 0 00-1.061-.44H4.5A2.25 2.25 0 002.25 6v12a2.25 2.25 0 002.25 2.25h15A2.25 2.25 0 0021.75 18V9a2.25 2.25 0 00-2.25-2.25h-5.379a1.5 1.5 0 01-1.06-.44z" />
            </svg>
            <svg v-else-if="t.icon === 'upload'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M3 16.5v2.25A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75V16.5m-13.5-9L12 3m0 0l4.5 4.5M12 3v13.5" />
            </svg>
            <svg v-else-if="t.icon === 'swap'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M7.5 21L3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
            </svg>
            <svg v-else-if="t.icon === 'bucket'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M4 5h16l-1.5 3H5.5L4 5zM5.5 8v10.5A1.5 1.5 0 007 20h10a1.5 1.5 0 001.5-1.5V8M9.5 11.5l5 5m0-5l-5 5" />
            </svg>
            <svg v-else-if="t.icon === 'trash'" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" />
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M21.75 17.25v-.228a4.5 4.5 0 00-.12-1.03l-2.268-9.64a3.375 3.375 0 00-3.285-2.602H7.923a3.375 3.375 0 00-3.285 2.602l-2.268 9.64a4.5 4.5 0 00-.12 1.03v.228m19.5 0a3 3 0 01-3 3H5.25a3 3 0 01-3-3m19.5 0a3 3 0 00-3-3H5.25a3 3 0 00-3 3m16.5 0h.008v.008h-.008v-.008zm-3 0h.008v.008h-.008v-.008z" />
            </svg>
            {{ t.label }}
          </button>
        </div>
      </nav>

      <main class="content">
        <Transition name="fade-slide" mode="out-in">
          <KeepAlive :max="5" :exclude="['UploadPanel', 'MigratePanel']">
            <AccountsPanel v-if="tab === 'accounts'" @changed="loadAccounts" />
            <ObjectsPanel v-else-if="tab === 'objects'" />
            <BucketsPanel v-else-if="tab === 'buckets'" />
            <RecycleBinPanel v-else-if="tab === 'trash'" />
            <UploadPanel v-else-if="tab === 'upload'" />
            <MigratePanel v-else-if="tab === 'migrate'" />
            <ServerPanel v-else-if="tab === 'server'" />
          </KeepAlive>
        </Transition>
      </main>
    </div>

    <Toasts />
    <ConfirmDialog />
    <PromptDialog />
  </div>
</template>
