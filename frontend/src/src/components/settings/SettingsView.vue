<script setup lang="ts">
import { computed, onMounted, ref, watch, defineAsyncComponent, type Component } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSettingsStore } from '@/stores/settings'
import { useAdminStore } from '@/stores/admin'
import { t } from '@/i18n'
import { ArrowRight } from '@/components/common/icons'

/**
 * 设置中心：左侧三组导航 + 右侧内容区，路由 query 同步直达。
 *
 * <p>功能页全部收进设置：原侧栏的 记忆 / 知识库 / 技能 / MCP / 工具 / 工作流 /
 * 仪表盘 / 运行 / 任务 / 文件 / 文件夹 / 文档 / 桌宠 都成为这里的一个 section，
 * 旧路由（/memory 等）重定向到 /settings?tab=x，外壳左栏只保留任务主链路。
 * section 组件按需异步加载（首次点击才拉取 chunk）。
 */

interface SectionItem {
  id: SettingsTab
  labelKey: string
}

type SettingsTab =
  | 'models'
  | 'appearance'
  | 'advanced'
  | 'channels'
  | 'search'
  | 'skills'
  | 'subagents'
  | 'commands'
  | 'hooks'
  | 'mcp'
  | 'tools'
  | 'workflows'
  | 'memory'
  | 'kdocs'
  | 'wiki'
  | 'dashboard'
  | 'runs'
  | 'tasks'
  | 'files'
  | 'folders'
  | 'docs'
  | 'pet'
  | 'about'

const groups: { labelKey: string; items: SectionItem[] }[] = [
  {
    labelKey: 'settings.group.basic',
    items: [
      { id: 'models', labelKey: 'settings.tab.models' },
      { id: 'appearance', labelKey: 'settings.tab.appearance' },
      { id: 'advanced', labelKey: 'settings.tab.advanced' },
      { id: 'channels', labelKey: 'settings.tab.channels' },
      { id: 'search', labelKey: 'settings.tab.search' }
    ]
  },
  {
    labelKey: 'settings.group.agent',
    items: [
      { id: 'skills', labelKey: 'nav.skills' },
      { id: 'subagents', labelKey: 'nav.subagents' },
      { id: 'commands', labelKey: 'nav.commands' },
      { id: 'hooks', labelKey: 'nav.hooks' },
      { id: 'mcp', labelKey: 'nav.mcp' },
      { id: 'tools', labelKey: 'nav.tools' },
      { id: 'workflows', labelKey: 'nav.workflows' },
      { id: 'memory', labelKey: 'nav.memory' },
      { id: 'kdocs', labelKey: 'nav.knowledge' },
      { id: 'wiki', labelKey: 'nav.wiki' }
    ]
  },
  {
    labelKey: 'settings.group.data',
    items: [
      { id: 'dashboard', labelKey: 'nav.dashboard' },
      { id: 'runs', labelKey: 'nav.runs' },
      { id: 'tasks', labelKey: 'nav.tasks' },
      { id: 'files', labelKey: 'nav.files' },
      { id: 'folders', labelKey: 'nav.folders' },
      { id: 'docs', labelKey: 'nav.docs' },
      { id: 'pet', labelKey: 'nav.pet' },
      { id: 'about', labelKey: 'settings.tab.about' }
    ]
  }
]

/** section 组件登记表：id → 异步组件（首次激活才加载 chunk）。 */
const sectionComponents: Record<SettingsTab, Component> = {
  models: defineAsyncComponent(() => import('@/components/settings/tabs/ProviderSettings.vue')),
  appearance: defineAsyncComponent(() => import('@/components/settings/tabs/AppearanceSettings.vue')),
  advanced: defineAsyncComponent(() => import('@/components/settings/tabs/AdvancedSettings.vue')),
  channels: defineAsyncComponent(() => import('@/components/settings/tabs/ChannelSettings.vue')),
  search: defineAsyncComponent(() => import('@/components/settings/tabs/SearchSettings.vue')),
  about: defineAsyncComponent(() => import('@/components/settings/tabs/AboutSettings.vue')),
  skills: defineAsyncComponent(() => import('@/components/skills/SkillsView.vue')),
  subagents: defineAsyncComponent(() => import('@/components/agents/SubagentsView.vue')),
  commands: defineAsyncComponent(() => import('@/components/commands/CommandsView.vue')),
  hooks: defineAsyncComponent(() => import('@/components/hooks/HooksView.vue')),
  wiki: defineAsyncComponent(() => import('@/components/wiki/WikiView.vue')),
  mcp: defineAsyncComponent(() => import('@/components/mcp/McpServersView.vue')),
  tools: defineAsyncComponent(() => import('@/components/tools/ToolsView.vue')),
  workflows: defineAsyncComponent(() => import('@/components/workflows/WorkflowsView.vue')),
  memory: defineAsyncComponent(() => import('@/components/memory/MemoryCenterView.vue')),
  kdocs: defineAsyncComponent(() => import('@/components/knowledge/KnowledgeDocsView.vue')),
  dashboard: defineAsyncComponent(() => import('@/components/dashboard/DashboardView.vue')),
  runs: defineAsyncComponent(() => import('@/components/runs/RunsView.vue')),
  tasks: defineAsyncComponent(() => import('@/components/tasks/TasksView.vue')),
  files: defineAsyncComponent(() => import('@/components/files/FilesView.vue')),
  folders: defineAsyncComponent(() => import('@/components/folders/FoldersView.vue')),
  docs: defineAsyncComponent(() => import('@/components/docs/DocsView.vue')),
  pet: defineAsyncComponent(() => import('@/components/pet/PetSpaceView.vue'))
}

const settings = useSettingsStore()
const adminStore = useAdminStore()
const route = useRoute()
const router = useRouter()

/** 当前激活 section：路由 query（settings?tab=memory）为真相源。 */
const activeTab = ref<SettingsTab>('models')
const activeComponent = computed(() => sectionComponents[activeTab.value])

function onTabChange(id: SettingsTab): void {
  activeTab.value = id
  void router.replace({ query: { ...route.query, tab: id } })
}

function backToWorkspace(): void {
  void router.push('/chat')
}

onMounted(async () => {
  const q = route.query.tab
  if (typeof q === 'string' && q in sectionComponents) activeTab.value = q as SettingsTab
  void settings.load()
  void adminStore.load()
})

// 深链 / 命令面板在设置页已挂载时改 query（settings?tab=x）也要跟随切换
watch(
  () => route.query.tab,
  (q) => {
    if (typeof q === 'string' && q in sectionComponents && q !== activeTab.value) {
      activeTab.value = q as SettingsTab
    }
  }
)
</script>

<template>
  <div class="wb-ui flex h-full min-h-0">
    <!-- 左导航（基础设置 / Agent 能力 / 数据与统计 三组） -->
    <aside class="set-nav">
      <button class="set-back" @click="backToWorkspace">
        <component :is="ArrowRight" class="h-3.5 w-3.5 rotate-180" />
        <span>{{ t('settings.backToWorkspace') }}</span>
      </button>

      <nav class="set-nav-scroll">
        <div v-for="g in groups" :key="g.labelKey" class="set-group">
          <h4>{{ t(g.labelKey) }}</h4>
          <button
            v-for="item in g.items"
            :key="item.id"
            class="set-item"
            :class="{ 'is-active': activeTab === item.id }"
            @click="onTabChange(item.id)"
          >
            {{ t(item.labelKey) }}
          </button>
        </div>
      </nav>
    </aside>

    <!-- 内容区 -->
    <div class="flex min-w-0 flex-1 flex-col">
      <div v-if="settings.error" class="alert a-info mx-6 mt-4">
        {{ settings.error }}
      </div>
      <component :is="activeComponent" :key="activeTab" class="min-h-0 flex-1" />
    </div>
  </div>
</template>

<style scoped>
.set-nav {
  width: 216px;
  flex: none;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--wb-border);
  background: var(--wb-side, var(--wb-surface));
}
.set-back {
  display: flex;
  align-items: center;
  gap: 6px;
  margin: 12px 12px 4px;
  padding: 0 8px;
  height: 28px;
  border-radius: var(--wb-radius, 8px);
  color: var(--wb-muted);
  font-size: 12px;
}
.set-back:hover {
  background: var(--wb-surface-hover);
  color: var(--wb-ink);
}
.set-nav-scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 6px 8px 12px;
}
.set-group {
  margin-bottom: 10px;
}
.set-group h4 {
  padding: 0 8px;
  margin-bottom: 2px;
  font-size: 9.5px;
  font-weight: 600;
  letter-spacing: 0.9px;
  text-transform: uppercase;
  color: var(--wb-muted);
  opacity: 0.85;
}
.set-item {
  width: 100%;
  display: flex;
  align-items: center;
  padding: 0 8px;
  height: 28px;
  border-radius: var(--wb-radius, 8px);
  color: var(--wb-ink);
  font-size: 12px;
  text-align: left;
  transition: background 0.12s;
}
.set-item:hover {
  background: var(--wb-surface-hover);
}
.set-item.is-active {
  background: var(--wb-primary-soft);
  color: var(--wb-primary-strong);
  font-weight: 600;
}
</style>
