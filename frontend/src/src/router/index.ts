import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

/**
 * 路由表：Wails 单 HTML 资源下 history 模式无法 fallback，统一用 hash 路由。
 * 鉴权由 Wails 绑定承载，路由层不做守卫。
 */

const routes: RouteRecordRaw[] = [
  // 启动即进工作区：AI 助手的主场景是聊天，概览/仪表盘经命令面板进入
  { path: '/', redirect: '/chat' },

  // 顶层
  { path: '/home', name: 'home', component: () => import('@/components/home/HomeView.vue') },
  { path: '/dashboard', name: 'dashboard', component: () => import('@/components/dashboard/DashboardView.vue') },
  { path: '/chat', name: 'chat', component: () => import('@/components/chat/ChatView.vue') },
  { path: '/chat/:id', name: 'chat-session', component: () => import('@/components/chat/ChatView.vue'), props: true },
  { path: '/files', name: 'files', component: () => import('@/components/files/FilesView.vue') },
  { path: '/tasks', name: 'tasks', component: () => import('@/components/tasks/TasksView.vue') },
  { path: '/tools', name: 'tools', component: () => import('@/components/tools/ToolsView.vue') },
  { path: '/skills', name: 'skills', component: () => import('@/components/skills/SkillsView.vue') },
  { path: '/mcp', name: 'mcp', component: () => import('@/components/mcp/McpServersView.vue') },
  { path: '/workflows', name: 'workflows', component: () => import('@/components/workflows/WorkflowsView.vue') },
  { path: '/workflows/:id', name: 'workflow-graph', component: () => import('@/components/workflows/WorkflowGraphView.vue'), props: true },
  { path: '/folders', name: 'folders', component: () => import('@/components/folders/FoldersView.vue') },
  { path: '/kdocs', name: 'kdocs', component: () => import('@/components/knowledge/KnowledgeDocsView.vue') },
  { path: '/memory', name: 'memory', component: () => import('@/components/memory/MemoryCenterView.vue') },
  { path: '/runs', name: 'runs', component: () => import('@/components/runs/RunsView.vue') },

  // 设置组
  { path: '/cron', name: 'cron', component: () => import('@/components/cron/CronView.vue') },
  { path: '/pet', name: 'pet', component: () => import('@/components/pet/PetSpaceView.vue') },
  { path: '/pet/desktop', name: 'pet-desktop', component: () => import('@/components/pet/PetDesktop.vue') },

  // 系统组
  { path: '/channels', name: 'channels', component: () => import('@/components/channel/ChannelsView.vue') },
  { path: '/admin', redirect: '/settings?tab=about' },
  { path: '/docs', name: 'docs', component: () => import('@/components/docs/DocsView.vue') },
  { path: '/settings', name: 'settings', component: () => import('@/components/settings/SettingsView.vue') },

  // 404 → 回聊天工作区
  { path: '/:pathMatch(.*)*', redirect: '/chat' }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes
})

export default router
