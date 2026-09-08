<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type Component } from 'vue'
import { useRoute, useRouter, RouterView } from 'vue-router'
import { EventsOn, WindowMinimise, WindowToggleMaximise, Quit } from '@/wailsjs/runtime/runtime'
import { init as initI18n, setLocale, t, currentLocale } from '@/i18n'
import {
  MessageSquare,
  GitBranch,
  ListTree,
  Clock,
  BookOpen,
  ScrollText,
  Settings,
  Bot,
  Brain,
  LayoutDashboard,
  LayoutGrid,
  Server,
  Zap,
  File,
  Folder,
  Wrench,
  Radio,
  PawPrint,
  ArrowRight,
  Plus
} from '@/components/common/icons'
import { storeToRefs } from 'pinia'
import { UploadFile } from '@/wailsjs/go/main/App'
import AppBackground from '@/components/common/AppBackground.vue'
import CommandPalette from '@/components/common/CommandPalette.vue'
import SessionSidebar from '@/components/chat/SessionSidebar.vue'
import { useSettingsStore } from '@/stores/settings'
import { useChatStore } from '@/stores/chat'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useShortcuts } from '@/composables/useShortcuts'
import { useFocusMode } from '@/composables/useFocusMode'
import { openPalette } from '@/composables/useCommandPalette'
import { bootstrapServer } from '@/api/bootstrap'

/**
 * 应用外壳（照 prd/WorkBaby-UI-Prototype.html 还原）：
 * 自绘标题栏（frameless）+ 侧栏（主导航 / 资源 / 工作区 / 系统四组 + 折叠）+ RouterView。
 *
 * <p>样式全部来自 wb-ui.css 设计系统层（.win / .titlebar / .side / .nav-item …）。
 * 会话列表归聊天页自持（原型 .ses 列），外壳不再常驻；`/pet/desktop` 独立窗口跳过外壳。
 * 启动即进（去账号化）：bootstrapServer 成功才挂载业务视图，失败给可见错误态。
 */
const route = useRoute()
const router = useRouter()
const toast = useToast()
const ready = ref(false)
/** 启动引导状态：后端完成端口注入前显示可见进度，而不是白屏。 */
const booting = ref(true)
/** 桌宠独立窗口路由：跳过 AppShell。 */
const isPetDesktop = computed(() => route.path === '/pet/desktop')
const collapsed = ref(false)
/** 本机会话令牌换取失败（后端未就绪 / 端口不对）时的错误提示。 */
const bootError = ref<string | null>(null)

// 全局外观：读取 settings.general.appearance 背景 → 换取签名预览 URL 传给背景层
const settings = useSettingsStore()
const { backgroundUrl } = storeToRefs(settings)

// 会话（侧栏下半区常驻：新建 + 历史）
const dialog = useDialog()
const chat = useChatStore()
const { sessions, currentID, loadingSessions } = storeToRefs(chat)

interface NavItem {
  id: string
  to: string
  labelKey: string
  icon: Component
  /** 角标计数（可选；当前仅聊天接线会话数）。 */
  count?: () => number
}

const mainNav: NavItem[] = [
  { id: 'home', to: '/home', labelKey: 'nav.overview', icon: LayoutGrid },
  { id: 'chat', to: '/chat', labelKey: 'nav.chat', icon: MessageSquare, count: () => sessions.value.length },
  { id: 'workflows', to: '/workflows', labelKey: 'nav.workflows', icon: GitBranch },
  { id: 'tasks', to: '/tasks', labelKey: 'nav.tasks', icon: ListTree },
  { id: 'cron', to: '/cron', labelKey: 'nav.cron', icon: Clock },
  { id: 'dashboard', to: '/dashboard', labelKey: 'nav.dashboard', icon: LayoutDashboard }
]

const resourceNav: NavItem[] = [
  { id: 'memory', to: '/memory', labelKey: 'nav.memory', icon: Brain },
  { id: 'kdocs', to: '/kdocs', labelKey: 'nav.knowledge', icon: BookOpen },
  { id: 'skills', to: '/skills', labelKey: 'nav.skills', icon: Zap },
  { id: 'mcp', to: '/mcp', labelKey: 'nav.mcp', icon: Server }
]

const workspaceNav: NavItem[] = [
  { id: 'files', to: '/files', labelKey: 'nav.files', icon: File },
  { id: 'folders', to: '/folders', labelKey: 'nav.folders', icon: Folder },
  { id: 'tools', to: '/tools', labelKey: 'nav.tools', icon: Wrench },
  { id: 'channels', to: '/channels', labelKey: 'nav.channels', icon: Radio },
  { id: 'pet', to: '/pet', labelKey: 'nav.pet', icon: PawPrint }
]

const systemNav: NavItem[] = [
  { id: 'docs', to: '/docs', labelKey: 'nav.docs', icon: ScrollText },
  { id: 'settings', to: '/settings', labelKey: 'nav.settings', icon: Settings }
]

/** 全局快捷键（Ctrl+N 新建会话 / Ctrl+K 命令面板 / Ctrl+/ 聚焦输入框 / Ctrl+Shift+P 命令面板
 *  / Ctrl+B 折叠侧栏 / Ctrl+Shift+F 焦点模式 — M3-4 快捷键体系）。 */
const { registerShortcut, clearAll: clearAllShortcuts } = useShortcuts()
const focusMode = useFocusMode()

/** 当前高亮导航项：/home 精确匹配，其余支持子路由（chat/:id），取最长前缀命中。 */
const activePath = computed(() => {
  const all = [...mainNav, ...resourceNav, ...workspaceNav, ...systemNav]
  const hits = all.filter((i) =>
    i.to === '/home' ? route.path === '/home' : route.path === i.to || route.path.startsWith(i.to + '/')
  )
  return hits.sort((a, b) => b.to.length - a.to.length)[0]?.to ?? ''
})

function navTo(to: string): void {
  void router.push(to)
}

// ===== 自绘窗口控制（frameless；浏览器预览无 runtime 时静默降级） =====
function minWindow(): void {
  try {
    WindowMinimise()
  } catch {
    /* 非 Wails 环境 */
  }
}
function toggleMaximise(): void {
  try {
    WindowToggleMaximise()
  } catch {
    /* 非 Wails 环境 */
  }
}
function closeWindow(): void {
  try {
    Quit()
  } catch {
    /* 非 Wails 环境 */
  }
}

function onReady(): void {
  ready.value = true
  booting.value = false
  settings.loadBackground()
  settings.loadBehavior()
  chat.loadSessions()
}

// ===== 会话区操作（侧栏常驻） =====
const creating = ref(false)
async function onCreateSession(): Promise<void> {
  creating.value = true
  try {
    await chat.createSession(chat.selectedModelID)
  } finally {
    creating.value = false
  }
}
async function onSelectSession(id: string): Promise<void> {
  await chat.selectSession(id)
  if (route.name !== 'chat' && route.name !== 'chat-session') {
    void router.push(`/chat/${id}`)
  }
}
async function onRemoveSession(id: string): Promise<void> {
  const ok = await dialog.confirm({
    title: t('chat.deleteTitle'),
    content: t('chat.deleteConfirm'),
    danger: true
  })
  if (!ok) return
  try {
    await chat.deleteSession(id)
    toast.success(t('chat.deletedSuccess'))
  } catch (e) {
    toast.error(t('chat.operationFailed'), e instanceof Error ? e.message : String(e))
  }
}
async function onDeleteBatch(ids: string[]): Promise<void> {
  if (ids.length === 0) return
  const result = await chat.deleteSessions(ids)
  if (result.failed.length > 0) {
    toast.warning(t('chat.deleteBatchPartial', result.ok, result.failed.length))
  } else {
    toast.success(t('chat.deleteBatchDone', result.ok))
  }
}

async function switchLang(lang: string | number | boolean): Promise<void> {
  await setLocale(String(lang))
}

/** 侧栏底部语言快切：中 / EN 一键互换。 */
async function toggleLang(): Promise<void> {
  await switchLang(currentLocale.value === 'zh-CN' ? 'en-US' : 'zh-CN')
}

/** 桌宠模式：进入独立页前的路由记忆（pet:hide 后回到这里）。 */
const savedMainRoute = ref('/home')
watch(
  () => route.fullPath,
  (p) => {
    if (p && p !== '/pet/desktop') savedMainRoute.value = p
  },
  { immediate: true }
)

/** 桌宠模式事件监听（Go 侧 PetToggleMode emit pet:show/pet:hide）。
 *  浏览器预览态无 window.runtime，静默降级。 */
let offPetShow: (() => void) | undefined
let offPetHide: (() => void) | undefined
function registerPetEvents(): void {
  try {
    offPetShow = EventsOn('pet:show', () => {
      if (route.path !== '/pet/desktop') void router.push('/pet/desktop')
    })
    offPetHide = EventsOn('pet:hide', () => {
      if (route.path === '/pet/desktop') void router.replace(savedMainRoute.value)
    })
  } catch {
    /* 非 Wails 环境无 window.runtime */
  }
}

onMounted(async () => {
  await initI18n()
  // 主题已移到 main.ts 在 mount 前同步应用（见该文件说明），此处不再重复初始化
  // 双主机：先等 app:ready 拿到 serverPort 并初始化 HTTP 层，成功才挂载业务视图
  // （后端未就绪给可见错误态，而不是静默白屏）
  try {
    await bootstrapServer()
  } catch (e) {
    bootError.value = e instanceof Error ? e.message : String(e)
    booting.value = false
    return
  }
  registerPetEvents()
  registerFileOpenEvents()
  onReady()
  registerShortcut('ctrl+n', () => void onNewSessionShortcut())
  registerShortcut('ctrl+k', () => openPalette())
  registerShortcut('ctrl+/', () => window.dispatchEvent(new CustomEvent('wb:focus-input')))
  registerShortcut('ctrl+shift+p', () => openPalette())
  registerShortcut('ctrl+b', () => (collapsed.value = !collapsed.value))
  registerShortcut('ctrl+shift+f', () => focusMode.toggle())
})

onBeforeUnmount(() => {
  clearAllShortcuts()
  offPetShow?.()
  offPetHide?.()
  offFileOpen?.()
})

/**
 * 文件关联打开：主实例收到二次启动转交的路径后 emit app:open-file。
 * 处理：导入文件 → 新建会话 → 直接以附件发起一次「阅读总结」，用户双击文件即得到回答。
 */
let offFileOpen: (() => void) | undefined
function registerFileOpenEvents(): void {
  try {
    offFileOpen = EventsOn('app:open-file', (path: string) => {
      void handleOpenFile(path)
    })
  } catch {
    /* 非 Wails 环境（浏览器预览）无事件桥 */
  }
}

async function handleOpenFile(path: string): Promise<void> {
  try {
    const info = await UploadFile('', path, '', '')
    const session = await chat.createSession()
    await router.push(`/chat/${session.id}`)
    await chat.sendMessage(t('chat.fileOpenedPrompt', info.original_name || info.name), [info.id])
    toast.success(t('chat.fileOpened'), info.original_name || info.name)
  } catch (e) {
    toast.error(t('chat.fileOpenFailed'), e instanceof Error ? e.message : String(e))
  }
}

/** 后端就绪事件丢失时允许用户重新建立本地 HTTP 连接。 */
async function recoverBoot(): Promise<void> {
  bootError.value = null
  booting.value = true
  try {
    await bootstrapServer()
  } catch (e) {
    bootError.value = e instanceof Error ? e.message : String(e)
    booting.value = false
  }
}

/** Ctrl+N：新建会话并跳转聊天页。 */
async function onNewSessionShortcut(): Promise<void> {
  try {
    const s = await chat.createSession(chat.selectedModelID)
    await router.push(`/chat/${s.id}`)
    window.dispatchEvent(new CustomEvent('wb:focus-input'))
  } catch {
    /* 快捷键失败静默：不打扰用户 */
  }
}
</script>

<template>
  <!-- 桌宠独立窗口：跳过主壳 -->
  <RouterView v-if="isPetDesktop" />

  <!-- 后端初始化期间显示可见启动态，避免窗口打开后出现长时间白屏 -->
  <div v-else-if="booting" class="wb-ui relative flex h-screen items-center justify-center" aria-live="polite">
    <AppBackground />
    <div class="card relative z-10 w-full max-w-md space-y-5 text-center">
      <div class="tile tile-xl mx-auto flex items-center justify-center" style="margin: 0 auto 14px">
        <Bot class="ic" />
      </div>
      <h1 class="fs13 font-semibold text-wb-ink">{{ t('boot.startingTitle') }}</h1>
      <p class="fs11 muted">{{ t('boot.startingHint') }}</p>
      <div class="bar mx-auto w-40">
        <div class="h-full w-1/2 animate-pulse rounded-full bg-wb-primary" />
      </div>
      <p class="fs11 muted">{{ t('boot.startingDetail') }}</p>
    </div>
  </div>

  <!-- 后端仍未就绪时给出可见错误态与重试入口 -->
  <div v-else-if="bootError" class="wb-ui relative flex h-screen items-center justify-center">
    <AppBackground />
    <div class="card relative z-10 w-full max-w-md space-y-4 text-center">
      <div class="tile tile-xl mx-auto flex items-center justify-center" style="margin: 0 auto">
        <Bot class="ic" />
      </div>
      <h1 class="fs13 font-semibold text-wb-ink">{{ t('boot.unreachableTitle') }}</h1>
      <p class="fs11 muted">{{ t('boot.unreachableHint') }}</p>
      <p class="mono break-all rounded-lg bg-wb-bg/60 p-2 fs11 muted">{{ bootError }}</p>
      <button class="btn btn-primary" @click="recoverBoot">{{ t('boot.retry') }}</button>
    </div>
  </div>

  <!-- 应用外壳：titlebar + side + main -->
  <div v-else-if="ready" class="wb-ui win">
    <AppBackground :background-url="backgroundUrl" />

    <!-- 自绘标题栏（frameless 拖拽区） -->
    <div class="titlebar" style="--wails-draggable: drag">
      <div class="tb-mark">
        <Bot class="h-2.5 w-2.5" />
      </div>
      <span class="tb-name">WorkBaby</span>
      <span class="tb-path">{{ route.path }}</span>
      <span class="tb-sp" />
      <div class="winctl" style="--wails-draggable: no-drag">
        <span title="最小化" @click="minWindow">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="M5 12h14" /></svg>
        </span>
        <span title="最大化" @click="toggleMaximise">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="4" y="5" width="16" height="14" rx="1" /></svg>
        </span>
        <span class="close" title="关闭" @click="closeWindow">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"><path d="M18 6 6 18" /><path d="m6 6 12 12" /></svg>
        </span>
      </div>
    </div>

    <div class="win-body">
      <!-- 侧栏：四组导航 + 底部折叠 / 语言 / 设置 -->
      <aside class="side" :class="{ collapsed }">
        <div class="side-brand">
          <div class="logo"><Bot class="h-4 w-4" /></div>
          <div>
            <b>WorkBaby</b>
            <small>{{ t('nav.localIdentityHint') }}</small>
          </div>
        </div>

        <nav class="side-nav">
          <div class="nav-group">
            <h4>{{ t('nav.navMain') }}</h4>
            <button
              v-for="item in mainNav"
              :key="item.id"
              class="nav-item"
              :class="{ 'is-active': activePath === item.to }"
              @click="navTo(item.to)"
            >
              <component :is="item.icon" class="ic" />
              <span>{{ t(item.labelKey) }}</span>
              <span v-if="item.count && item.count() > 0" class="cnt">{{ item.count() }}</span>
            </button>
          </div>

          <div class="nav-group">
            <h4>{{ t('nav.resources') }}</h4>
            <button
              v-for="item in resourceNav"
              :key="item.id"
              class="nav-item"
              :class="{ 'is-active': activePath === item.to }"
              @click="navTo(item.to)"
            >
              <component :is="item.icon" class="ic" />
              <span>{{ t(item.labelKey) }}</span>
            </button>
          </div>

          <div class="nav-group">
            <h4>{{ t('nav.workspace') }}</h4>
            <button
              v-for="item in workspaceNav"
              :key="item.id"
              class="nav-item"
              :class="{ 'is-active': activePath === item.to }"
              @click="navTo(item.to)"
            >
              <component :is="item.icon" class="ic" />
              <span>{{ t(item.labelKey) }}</span>
            </button>
          </div>

          <div class="nav-group">
            <h4>{{ t('nav.system') }}</h4>
            <button
              v-for="item in systemNav"
              :key="item.id"
              class="nav-item"
              :class="{ 'is-active': activePath === item.to }"
              @click="navTo(item.to)"
            >
              <component :is="item.icon" class="ic" />
              <span>{{ t(item.labelKey) }}</span>
            </button>
          </div>
        </nav>

        <!-- 会话区（侧栏 40%：新建 + 历史，全页面常驻） -->
        <div class="side-sessions">
          <div class="ses-head">
            <h3>{{ t('nav.sessions') }}</h3>
            <button class="btn-icon" :title="t('chat.newSession')" @click="onCreateSession">
              <Plus class="ic ic-sm" />
            </button>
          </div>
          <SessionSidebar
            class="ses-list"
            :sessions="sessions"
            :currentID="currentID"
            :loading="loadingSessions"
            :creating="creating"
            @select="onSelectSession"
            @create="onCreateSession"
            @remove="onRemoveSession"
            @delete-batch="onDeleteBatch"
          />
        </div>

        <div class="side-foot">
          <button class="fi" :title="t('nav.collapseRail')" @click="collapsed = !collapsed">
            <component :is="ArrowRight" class="ic ic-sm" :class="collapsed ? '' : 'rotate-180'" />
            <span>{{ t('nav.collapseRail') }}</span>
          </button>
          <button class="fi" :title="currentLocale === 'zh-CN' ? 'English' : '中文'" @click="toggleLang">
            <span class="ic ic-sm font-semibold">{{ currentLocale === 'zh-CN' ? 'EN' : '中' }}</span>
            <span>{{ currentLocale === 'zh-CN' ? 'EN' : '中文' }}</span>
          </button>
          <button class="fi" @click="navTo('/settings')">
            <Settings class="ic ic-sm" />
            <span>{{ t('nav.settings') }}</span>
          </button>
        </div>
      </aside>

      <!-- 主内容区 -->
      <main class="main relative z-10">
        <RouterView />
      </main>
    </div>

    <!-- 全局命令面板（Ctrl+K / Ctrl+Shift+P） -->
    <CommandPalette />
  </div>
</template>
