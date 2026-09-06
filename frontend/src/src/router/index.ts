import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

/**
 * 路由表（WorkBaby Go 版 · Wails v2 嵌入式前端）。
 *
 * <p>Wails v2 用 wails:// scheme 与 WebView2 通信，createWebHistory 在 wails
 * 单 HTML 资源下无法 fallback（刷新会 404），统一改用 createWebHashHistory。
 * 路由表与 Java 版完全一致（23 条），便于视觉/验收对照。
 *
 * <p>鉴权：Wails 绑定本身即鉴权，没有 /api/v1/auth/session；路由层不做守卫。
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
  { path: '/more', name: 'more', component: () => import('@/components/more/MoreView.vue') },

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
