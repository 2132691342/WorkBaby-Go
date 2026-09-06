<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch, type Component } from 'vue'
import { useRoute, useRouter, RouterView } from 'vue-router'
import { EventsOn } from '@/wailsjs/runtime/runtime'
import { init as initI18n, setLocale, t, currentLocale } from '@/i18n'
import {
  MessageSquare,
  GitBranch,
  ListTree,
  Clock,
  BookOpen,
  ScrollText,
  Settings,
  ArrowLeft,
  ArrowRight,
  Bot,
  Brain,
  LayoutDashboard,
  LayoutGrid,
  Server,
  Zap,
  Layers
} from '@/components/common/icons'
import { storeToRefs } from 'pinia'
import { UploadFile } from '@/wailsjs/go/main/App'
import AppBackground from '@/components/common/AppBackground.vue'
import CommandPalette from '@/components/common/CommandPalette.vue'
import SessionSidebar from '@/components/chat/SessionSidebar.vue'
import { useSettingsStore } from '@/stores/settings'
import { useChatStore } from '@/stores/chat'
import { useAsyncAction } from '@/composables/useAsyncAction'
import { useToast } from '@/composables/useToast'
import { useDialog } from '@/composables/useDialog'
import { useShortcuts } from '@/composables/useShortcuts'
import { useFocusMode } from '@/composables/useFocusMode'
import { openPalette } from '@/composables/useCommandPalette'
import { bootstrapServer } from '@/api/bootstrap'

/**
 * 应用外壳：左侧导航 + 右侧工作区。
 *
 * <p>左栏分上下两区——上为 `el-menu`（主导航 / 资源 / 系统三组，支持折叠），
 * 下为常驻 {@link SessionSidebar}（跨页面切换会话）。右栏为 `<RouterView/>`。
 * 会话列表提升到外壳层，任何页面都能直接切会话。
 * `/pet/desktop` 为桌宠独立窗口路由，跳过外壳。
 *
 * <p><b>启动即进</b>（去账号化）：没有登录页。本机会话令牌由
 * {@code api/session.ts} 在第一次请求前自动换取，因此外壳启动只需等一次
 * {@code ensureSessionToken()} 成功，失败才显示错误态。
 */
const route = useRoute()
const router = useRouter()
const toast = useToast()
const dialog = useDialog()
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

// 会话（全局常驻左栏下半）
const chat = useChatStore()
const { sessions, currentID, loadingSessions } = storeToRefs(chat)

// T25：包一层让「新建会话」按钮能感知创建中；
// 新建时沿用当前选中的模型（选中模型会同步到新会话），避免新会话回落到 Auto/默认模型让人以为模型丢了
const creating = useAsyncAction(() => chat.createSession(chat.selectedModelID))

interface NavItem {
  id: string
  to: string
  labelKey: string
  icon: Component
}

const mainNav: NavItem[] = [
  { id: 'chat', to: '/chat', labelKey: 'nav.chat', icon: MessageSquare },
  { id: 'workflows', to: '/workflows', labelKey: 'nav.workflows', icon: GitBranch },
  { id: 'tasks', to: '/tasks', labelKey: 'nav.tasks', icon: ListTree },
  { id: 'cron', to: '/cron', labelKey: 'nav.cron', icon: Clock },
  { id: 'dashboard', to: '/dashboard', labelKey: 'nav.dashboard', icon: LayoutDashboard },
  { id: 'overview', to: '/home', labelKey: 'nav.overview', icon: LayoutGrid }
]

// 资源组：高频资源直出；低频管理页（文件/文件夹/桌宠/工具/通道）收进
// 「更多资源」聚合页（/more，tab 切换）——入口永远可达，侧栏不再有二级菜单
const resourceNav: NavItem[] = [
  { id: 'memory', to: '/memory', labelKey: 'nav.memory', icon: Brain },
  { id: 'knowledge', to: '/kdocs', labelKey: 'nav.knowledge', icon: BookOpen },
  { id: 'mcp', to: '/mcp', labelKey: 'nav.mcp', icon: Server },
  { id: 'skills', to: '/skills', labelKey: 'nav.skills', icon: Zap },
  { id: 'more', to: '/more', labelKey: 'nav.moreResources', icon: Layers }
]

/** 管理后台已并入设置中心「关于」tab，导航不再单独暴露 admin。 */
const systemNav: NavItem[] = [
  { id: 'docs', to: '/docs', labelKey: 'nav.docs', icon: ScrollText },
  { id: 'settings', to: '/settings', labelKey: 'nav.settings', icon: Settings }
]



/** 全局快捷键（Ctrl+N 新建会话 / Ctrl+K 命令面板 / Ctrl+/ 聚焦输入框 / Ctrl+Shift+P 命令面板
 *  / Ctrl+B 折叠侧栏 / Ctrl+Shift+F 焦点模式 — M3-4 快捷键体系）。 */
const { registerShortcut, clearAll: clearAllShortcuts } = useShortcuts()
const focusMode = useFocusMode()

/** 当前高亮菜单项：/home 精确匹配，其余支持子路由（chat/:id），取最长前缀命中。 */
const activeIndex = computed(() => {
  const all = [...mainNav, ...resourceNav, ...systemNav]
  const hits = all.filter((i) =>
    i.to === '/home' ? route.path === '/home' : route.path === i.to || route.path.startsWith(i.to + '/')
  )
  return hits.sort((a, b) => b.to.length - a.to.length)[0]?.to ?? ''
})

function onReady(): void {
  ready.value = true
  booting.value = false
  settings.loadBackground()
  settings.loadBehavior()
  chat.loadSessions()
}

/** 选择会话：切到该会话消息；若当前不在聊天页则跳转 /chat/:id。 */
async function onSelectSession(id: string): Promise<void> {
  await chat.selectSession(id)
  if (route.name !== 'chat' && route.name !== 'chat-session') {
    void router.push(`/chat/${id}`)
  }
}

function onCreateSession(): void {
  void creating.run()
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

/** 批量删除会话（SessionSidebar 进入多选模式后触发）。 */
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
  <div v-else-if="booting" class="relative flex h-screen items-center justify-center" aria-live="polite">
    <AppBackground />
    <div class="relative z-10 w-full max-w-md space-y-5 rounded-2xl border border-wb-border bg-wb-surface/90 p-8 text-center shadow-[var(--wb-shadow-lg)] backdrop-blur">
      <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-wb-primary text-white">
        <Bot class="h-6 w-6" />
      </div>
      <h1 class="text-lg font-bold text-wb-ink">{{ t('boot.startingTitle') }}</h1>
      <p class="text-xs text-wb-muted">{{ t('boot.startingHint') }}</p>
      <div class="mx-auto h-1 w-40 overflow-hidden rounded-full bg-wb-surface-hover">
        <div class="h-full w-1/2 animate-pulse rounded-full bg-wb-primary" />
      </div>
      <p class="text-[11px] text-wb-muted">{{ t('boot.startingDetail') }}</p>
    </div>
  </div>

  <!-- 后端仍未就绪时给出可见错误态与重试入口 -->
  <div v-else-if="bootError" class="relative flex h-screen items-center justify-center">
    <AppBackground />
    <div class="relative z-10 w-full max-w-md space-y-4 rounded-2xl border border-wb-border bg-wb-surface/90 p-8 text-center shadow-[var(--wb-shadow-lg)] backdrop-blur">
      <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-wb-primary text-white">
        <Bot class="h-6 w-6" />
      </div>
      <h1 class="text-lg font-bold text-wb-ink">{{ t('boot.unreachableTitle') }}</h1>
      <p class="text-xs text-wb-muted">{{ t('boot.unreachableHint') }}</p>
      <p class="break-all rounded-lg bg-wb-bg/60 p-2 text-[11px] text-wb-muted">{{ bootError }}</p>
      <el-button type="primary" @click="recoverBoot">{{ t('boot.retry') }}</el-button>
    </div>
  </div>

  <div v-else-if="ready" class="flex h-screen overflow-hidden bg-wb-bg text-wb-ink">
    <!-- 全局背景层（仅用户自定义背景；无背景时纯色，克制） -->
    <AppBackground :background-url="backgroundUrl" />

    <!-- 左侧侧栏（Studio：暖 surface 底 + 右侧细边框） -->
    <aside
      class="relative z-10 flex shrink-0 flex-col border-r border-wb-border bg-wb-surface transition-[width] duration-200"
      :class="collapsed ? 'w-14' : 'w-60'"
    >
      <!-- Logo（纯色实底 Mark + 品牌名；去渐变，克制桌面质感） -->
      <div class="flex h-14 shrink-0 items-center gap-2.5 border-b border-wb-border px-3" :class="collapsed ? 'justify-center' : ''">
        <div class="relative flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-[10px] bg-wb-primary text-white shadow-[inset_0_1px_0_rgba(255,255,255,0.18),0_1px_2px_rgba(15,17,22,0.12)]">
          <Bot class="h-4 w-4" />
          <span class="pointer-events-none absolute inset-x-1 top-0.5 h-px rounded-full bg-white/40" />
        </div>
        <span v-if="!collapsed" class="truncate text-[15px] font-semibold tracking-tight text-wb-ink">WorkBaby</span>
      </div>

      <!-- 上半：菜单（主导航 + 资源组 + 系统组；底部渐隐提示可滚动） -->
      <el-scrollbar class="wb-nav-fade min-h-0 flex-[5] px-1.5 py-2">
        <el-menu
          :default-active="activeIndex"
          :collapse="collapsed"
          :collapse-transition="false"
          router
          class="wb-nav border-r-0!"
        >
          <el-menu-item v-for="item in mainNav" :key="item.id" :index="item.to">
            <el-icon><component :is="item.icon" class="h-[18px] w-[18px]" /></el-icon>
            <template #title>{{ t(item.labelKey) }}</template>
          </el-menu-item>

          <el-menu-item-group v-if="!collapsed">
            <template #title>{{ t('nav.resources') }}</template>
            <el-menu-item v-for="item in resourceNav" :key="item.id" :index="item.to">
              <el-icon><component :is="item.icon" class="h-[18px] w-[18px]" /></el-icon>
              <template #title>{{ t(item.labelKey) }}</template>
            </el-menu-item>
          </el-menu-item-group>
          <template v-else>
            <el-menu-item v-for="item in resourceNav" :key="item.id" :index="item.to">
              <el-icon><component :is="item.icon" class="h-[18px] w-[18px]" /></el-icon>
              <template #title>{{ t(item.labelKey) }}</template>
            </el-menu-item>
          </template>

          <el-menu-item-group v-if="!collapsed">
            <template #title>{{ t('nav.system') }}</template>
            <el-menu-item v-for="item in systemNav" :key="item.id" :index="item.to">
              <el-icon><component :is="item.icon" class="h-[18px] w-[18px]" /></el-icon>
              <template #title>{{ t(item.labelKey) }}</template>
            </el-menu-item>
          </el-menu-item-group>
          <template v-else>
            <el-menu-item v-for="item in systemNav" :key="item.id" :index="item.to">
              <el-icon><component :is="item.icon" class="h-[18px] w-[18px]" /></el-icon>
              <template #title>{{ t(item.labelKey) }}</template>
            </el-menu-item>
          </template>
        </el-menu>
      </el-scrollbar>

      <!-- 下半：会话记录（全局常驻） -->
      <template v-if="!collapsed">
        <div class="shrink-0 border-t border-wb-border px-3 pt-2 pb-1">
          <p class="text-[11px] font-semibold text-wb-muted">
            {{ t('nav.sessions') }}
          </p>
        </div>
        <div class="min-h-0 flex-[5]">
          <SessionSidebar
            :sessions="sessions"
            :currentID="currentID"
            :loading="loadingSessions"
            :creating="creating.pending.value"
            @select="onSelectSession"
            @create="onCreateSession"
            @remove="onRemoveSession"
            @delete-batch="onDeleteBatch"
          />
        </div>
      </template>

      <!-- 底部：语言切换 + 用户信息 -->
      <div class="shrink-0 border-t border-wb-border p-2">
        <el-radio-group
          v-if="!collapsed"
          :model-value="currentLocale"
          size="small"
          class="w-full"
          @change="switchLang"
        >
          <el-radio-button value="zh-CN">中文</el-radio-button>
          <el-radio-button value="en-US">EN</el-radio-button>
        </el-radio-group>

        <!-- 本机身份卡（去账号化） -->
        <div class="mt-2 flex items-center gap-2 rounded-lg px-2 py-1.5" :class="collapsed ? 'justify-center' : 'hover:bg-wb-surface-hover'">
          <div class="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-wb-surface-hover text-wb-muted">
            <Bot class="h-3.5 w-3.5" />
          </div>
          <div v-if="!collapsed" class="min-w-0 flex-1 text-left">
            <p class="truncate text-[13px] font-medium text-wb-ink">{{ t('nav.localIdentity') }}</p>
            <p class="truncate text-[11px] text-wb-muted">{{ t('nav.localIdentityHint') }}</p>
          </div>
        </div>
      </div>

      <!-- 收起/展开按钮 -->
      <button
        type="button"
        class="absolute -right-3 top-6 flex h-6 w-6 items-center justify-center rounded-full border border-wb-border bg-wb-surface text-wb-muted shadow-sm hover:text-wb-ink"
        :title="collapsed ? t('nav.expandRail') : t('nav.collapseRail')"
        @click="collapsed = !collapsed"
      >
        <el-icon :size="12"><component :is="collapsed ? ArrowRight : ArrowLeft" /></el-icon>
      </button>
    </aside>

    <!-- 右工作区 -->
    <main class="relative z-10 min-w-0 flex-1 overflow-hidden">
      <RouterView />
    </main>

    <!-- 全局命令面板（Ctrl+K / Ctrl+Shift+P） -->
    <CommandPalette />
  </div>
</template>

<style scoped>
/* el-menu 融入 Studio 侧栏：透明底 + 圆角 item + 激活态左侧指示条 */
.wb-nav {
  --el-menu-bg-color: transparent;
  --el-menu-text-color: var(--wb-muted);
  --el-menu-active-color: var(--wb-primary);
  --el-menu-hover-bg-color: var(--wb-surface-hover);
  --el-menu-hover-text-color: var(--wb-ink);
  --el-menu-item-height: 34px;
  --el-menu-base-level-padding: 10px;
  --el-menu-level-padding: 10px;
  background: transparent;
}
.wb-nav :deep(.el-menu-item) {
  position: relative;
  border-radius: 8px;
  margin: 2px 4px;
  font-size: 13px;
  font-weight: 500;
  transition: background-color 0.15s ease, color 0.15s ease;
}
.wb-nav :deep(.el-menu-item.is-active) {
  background: var(--wb-primary-soft);
  color: var(--wb-primary);
  font-weight: 600;
}
.wb-nav :deep(.el-menu-item.is-active)::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 16px;
  border-radius: 0 2px 2px 0;
  background: var(--wb-primary);
}
.wb-nav :deep(.el-menu-item-group__title) {
  padding-left: 14px;
  padding-top: 14px;
  padding-bottom: 4px;
  color: var(--wb-muted);
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
.wb-nav :deep(.el-menu--collapse) {
  width: auto;
}
.wb-nav :deep(.el-menu--collapse .el-menu-item) {
  margin: 2px 6px;
  border-radius: 8px;
}
/* 菜单区底部渐隐：提示「下面还有，可滚动」，替代生硬截断 */
.wb-nav-fade {
  mask-image: linear-gradient(to bottom, #000 0%, #000 calc(100% - 18px), transparent 100%);
}
</style>
