<script setup lang="ts">
import { computed, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  BookOpen,
  Brain,
  Clock,
  GitBranch,
  LayoutDashboard,
  LayoutGrid,
  Moon,
  Plus,
  Search,
  Settings,
  Sunny
} from '@/components/common/icons'
import { useTheme } from '@/composables/useTheme'
import { t } from '@/i18n'

/**
 * 顶栏横向菜单：工作台 / 自动化 / 工作流 / 记忆 / 知识库 / 仪表盘 / 设置，
 * 右侧为命令面板、主题切换与新建任务。
 *
 * <p>激活态由路由判定：顶层路由按前缀，设置中心内页面按 `?tab=` 深链，
 * 未列入菜单的设置 tab 统一归到「设置」，与 SettingsView 左导航语义一致。
 */
const emit = defineEmits<{ create: []; search: [] }>()

const route = useRoute()
const router = useRouter()
const theme = useTheme()

/** 菜单内直挂的设置中心 section（其余 tab 归「设置」）。 */
const SETTINGS_TABS = ['workflows', 'memory', 'kdocs', 'dashboard'] as const

interface NavItem {
  labelKey: string
  icon: Component
  to: string
  match: () => boolean
}

function onSettingsTab(tab: string): boolean {
  return route.path.startsWith('/settings') && route.query.tab === tab
}

function inSettingsTail(): boolean {
  if (!route.path.startsWith('/settings')) return false
  const q = typeof route.query.tab === 'string' ? route.query.tab : ''
  return !(SETTINGS_TABS as readonly string[]).includes(q)
}

const items = computed<NavItem[]>(() => [
  { labelKey: 'nav.workbench', icon: LayoutGrid, to: '/chat', match: () => route.path.startsWith('/chat') },
  { labelKey: 'nav.automation', icon: Clock, to: '/cron', match: () => route.path.startsWith('/cron') },
  { labelKey: 'nav.workflows', icon: GitBranch, to: '/settings?tab=workflows', match: () => onSettingsTab('workflows') },
  { labelKey: 'nav.memory', icon: Brain, to: '/settings?tab=memory', match: () => onSettingsTab('memory') },
  { labelKey: 'nav.knowledge', icon: BookOpen, to: '/settings?tab=kdocs', match: () => onSettingsTab('kdocs') },
  { labelKey: 'nav.dashboard', icon: LayoutDashboard, to: '/settings?tab=dashboard', match: () => onSettingsTab('dashboard') },
  { labelKey: 'nav.settings', icon: Settings, to: '/settings', match: inSettingsTail }
])

const isDark = computed(() => theme.currentTheme.value === 'dark')

function go(to: string): void {
  void router.push(to)
}

function toggleTheme(): void {
  theme.setTheme(isDark.value ? 'light' : 'dark')
}
</script>

<template>
  <nav class="appnav" :aria-label="t('nav.navMain')">
    <div class="appnav-menu">
      <button
        v-for="it in items"
        :key="it.labelKey"
        class="appnav-item"
        :class="{ 'is-active': it.match() }"
        @click="go(it.to)"
      >
        <component :is="it.icon" class="ic-sm" />
        <span>{{ t(it.labelKey) }}</span>
      </button>
    </div>

    <span class="appnav-sp" />

    <div class="appnav-actions">
      <button class="btn-icon" :title="t('nav.search')" @click="emit('search')">
        <Search class="ic-sm" />
      </button>
      <button class="btn-icon" :title="t('chat.toggleTheme')" @click="toggleTheme">
        <Moon v-if="!isDark" class="ic-sm" />
        <Sunny v-else class="ic-sm" />
      </button>
      <button class="btn btn-primary btn-sm" @click="emit('create')">
        <Plus class="ic-sm" />
        <span>{{ t('nav.newTask') }}</span>
      </button>
    </div>
  </nav>
</template>
