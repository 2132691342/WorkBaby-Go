<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import {
  FileText,
  FolderOpen,
  Image,
  Layers,
  RefreshCw,
  Download,
  X as Close
} from '@/components/common/icons'
import { apiGet } from '@/api/client'
import { openExternal } from '@/api/shellBridge'
import type { FolderTreeNode, WorkspaceFile } from '@/types/api'
import { t } from '@/i18n'
import Skeleton from '@/components/common/Skeleton.vue'
import FolderTree from '@/components/chat/FolderTree.vue'

/**
 * 工作区侧栏：三个 tab 展示当前会话的工作区内容。
 *
 * <ul>
 *   <li><b>files</b>：工作区全部文件（`/api/v1/chat/workspace/files/{id}`，含大小与下载）</li>
 *   <li><b>artifacts</b>：可内嵌预览的产物（html / url / text / pdf / image）</li>
 *   <li><b>folders</b>：绑定到该会话工作区的逻辑文件夹树</li>
 * </ul>
 */
const props = defineProps<{
  session_id: string | null
  /** 会话绑定的外部工作目录（空 = 默认工作区）；仅用于头部展示。 */
  workspace_path?: string | null
  /** 打开目录选择器（头部「更换」按钮）。 */
  pick?: () => void
}>()

type TabID = 'files' | 'artifacts' | 'folders'

interface TabDef {
  id: TabID
  labelKey: string
  icon: Component
}

const tabs: TabDef[] = [
  { id: 'files', labelKey: 'chat.tabFiles', icon: FolderOpen },
  { id: 'artifacts', labelKey: 'chat.tabArtifacts', icon: Image },
  { id: 'folders', labelKey: 'chat.tabFolders', icon: Layers }
]

const activeTab = ref<TabID>('files')
const files = ref<WorkspaceFile[]>([])
const folders = ref<FolderTreeNode[]>([])
const loading = ref(false)
const selected = ref<WorkspaceFile | null>(null)

const totalFolderCount = computed(() => countNodes(folders.value))

/** 头部目录展示：外部目录取末两级，默认工作区给固定文案。 */
const rootDisplay = computed(() => {
  const wp = props.workspace_path
  if (!wp) return t('chat.workspace.default')
  const parts = wp.replace(/\\/g, '/').split('/').filter(Boolean)
  return parts.length <= 2 ? wp : `…/${parts.slice(-2).join('/')}`
})

/** 可预览产物：html / url / text / pdf / image。 */
const previewableFiles = computed(() =>
  files.value.filter((f) => ['html', 'url', 'text', 'pdf', 'image'].includes(f.kind))
)

function countNodes(nodes: FolderTreeNode[]): number {
  let n = 0
  for (const x of nodes) {
    n += 1 + countNodes(x.children ?? [])
  }
  return n
}

watch(
  () => [props.session_id, props.workspace_path] as [string | null, string | null | undefined],
  ([id, wp], old) => {
    const [prevId, prevWp] = old ?? [null, undefined]
    selected.value = null
    // 绑定路径变化（含切换会话）：文件夹树与文件列表都失效，全部重置重拉
    if (id !== prevId || wp !== prevWp) folders.value = []
    if (id) {
      void load(id)
    } else {
      files.value = []
    }
  },
  { immediate: true }
)

async function load(id: string): Promise<void> {
  loading.value = true
  try {
    files.value = await apiGet<WorkspaceFile[]>(`/api/v1/chat/workspace/files/${id}`)
  } catch {
    files.value = []
  } finally {
    loading.value = false
  }
}

/** 拉取绑定到当前 session workspace 的逻辑文件夹树。 */
async function loadFolders(id: string): Promise<void> {
  loading.value = true
  try {
    folders.value = await apiGet<FolderTreeNode[]>(
      `/api/v1/folders/tree?workspaceID=${encodeURIComponent(id)}`
    )
  } catch {
    folders.value = []
  } finally {
    loading.value = false
  }
}

function selectFile(f: WorkspaceFile): void {
  selected.value = f
}

function fmtSize(size?: number): string {
  if (!size) return ''
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function switchTab(id: TabID): void {
  activeTab.value = id
  if (id === 'folders' && props.session_id && folders.value.length === 0) {
    void loadFolders(props.session_id)
  }
}

function onTabChange(name: string | number): void {
  switchTab(name as TabID)
}

function refresh(): void {
  if (!props.session_id) return
  if (activeTab.value === 'folders') void loadFolders(props.session_id)
  else void load(props.session_id)
}
</script>

<template>
  <aside class="flex h-full w-full flex-col bg-wb-surface/50">
    <!-- 根目录条：让「当前工作区是哪个目录」一眼可见 -->
    <div class="flex shrink-0 items-center gap-1.5 border-b border-wb-border px-3 py-2">
      <FolderOpen class="h-3.5 w-3.5 shrink-0 text-wb-primary" />
      <span
        class="min-w-0 flex-1 truncate text-xs font-medium text-wb-ink"
        :title="workspace_path || t('chat.workspace.defaultFull')"
      >{{ rootDisplay }}</span>
      <button
        v-if="pick"
        type="button"
        class="shrink-0 rounded px-1.5 py-0.5 text-[11px] text-wb-muted transition-colors hover:bg-wb-surface-hover hover:text-wb-primary-strong"
        :title="t('chat.workspacePicker.title')"
        @click="pick"
      >
        {{ t('chat.workspace.change') }}
      </button>
      <button
        type="button"
        class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-wb-muted transition-colors hover:bg-wb-surface-hover hover:text-wb-ink"
        :disabled="!session_id || loading"
        :title="t('chat.refresh')"
        @click="refresh"
      >
        <RefreshCw class="h-3.5 w-3.5" :class="loading ? 'animate-spin' : ''" />
      </button>
    </div>

    <!-- tab 栏（独立一行，不再与刷新按钮绝对定位互相覆盖） -->
    <el-tabs v-model="activeTab" class="wb-panel-tabs shrink-0" @tab-change="onTabChange">
      <el-tab-pane v-for="tab in tabs" :key="tab.id" :name="tab.id">
        <template #label>
          <span class="flex items-center gap-1.5 text-xs">
            <el-icon :size="14"><component :is="tab.icon" /></el-icon>
            {{ t(tab.labelKey) }}
          </span>
        </template>
      </el-tab-pane>
    </el-tabs>

    <!-- 内容区按当前 tab 渲染 -->
    <div class="min-h-0 flex-1 overflow-y-auto p-2">
      <Skeleton v-if="loading" :lines="4" />

      <!-- files：工作区全部文件（含下载） -->
      <template v-else-if="activeTab === 'files'">
        <el-empty
          v-if="files.length === 0"
          :description="t('chat.workspaceEmpty')"
          :image-size="48"
        />
        <ul v-else class="space-y-0.5">
          <li
            v-for="f in files"
            :key="f.path"
            class="group flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition-colors"
            :class="selected?.path === f.path ? 'bg-wb-primary/10 text-wb-primary-strong' : 'text-wb-ink hover:bg-wb-primary/5'"
            @click="selectFile(f)"
          >
            <component
              :is="['image'].includes(f.kind) ? Image : FileText"
              class="h-3.5 w-3.5 shrink-0"
            />
            <span class="min-w-0 flex-1 truncate" :title="f.path">{{ f.path }}</span>
            <span class="shrink-0 text-[10px] text-wb-muted">{{ fmtSize(f.size) }}</span>
            <a
              :href="f.url"
              :download="f.name"
              :title="t('chat.download')"
              class="hidden shrink-0 text-wb-muted hover:text-wb-primary-strong group-hover:block"
              @click.prevent.stop="openExternal(f.url)"
            >
              <Download class="h-3 w-3" />
            </a>
          </li>
        </ul>
      </template>

      <!-- artifacts：可内嵌预览的产物 -->
      <template v-else-if="activeTab === 'artifacts'">
        <el-empty
          v-if="previewableFiles.length === 0"
          :description="t('chat.noArtifacts')"
          :image-size="48"
        />
        <ul v-else class="space-y-0.5">
          <li
            v-for="f in previewableFiles"
            :key="f.path"
            class="group flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition-colors"
            :class="selected?.path === f.path ? 'bg-wb-primary/10 text-wb-primary-strong' : 'text-wb-ink hover:bg-wb-primary/5'"
            @click="selectFile(f)"
          >
            <component :is="f.kind === 'image' ? Image : FileText" class="h-3.5 w-3.5 shrink-0" />
            <span class="min-w-0 flex-1 truncate">{{ f.name || f.path }}</span>
          </li>
        </ul>
      </template>

      <!-- folders：绑定到本会话工作区的逻辑文件夹树 -->
      <template v-else-if="activeTab === 'folders'">
        <div class="mb-2 flex items-center justify-between px-1 text-[10px] text-wb-muted">
          <span>共 {{ totalFolderCount }} 个文件夹</span>
        </div>
        <el-empty
          v-if="folders.length === 0"
          :description="t('chat.noFolders')"
          :image-size="48"
        />
        <FolderTree v-else :nodes="folders" />
      </template>
    </div>

    <!-- 选中文件预览 -->
    <div v-if="selected" class="shrink-0 border-t border-wb-border p-2">
      <div class="mb-1 flex items-center justify-between">
        <span class="truncate text-xs text-wb-ink">{{ selected.name || selected.path }}</span>
        <button
          type="button"
          class="flex h-5 w-5 items-center justify-center rounded text-wb-muted transition-colors hover:bg-wb-surface-hover hover:text-wb-ink"
          @click="selected = null"
        >
          <Close class="h-3 w-3" />
        </button>
      </div>
      <iframe
        v-if="['html', 'url', 'text', 'pdf'].includes(selected.kind)"
        :src="selected.url"
        :title="selected.name"
        class="h-40 w-full rounded-md border border-wb-border bg-wb-surface"
        sandbox="allow-scripts allow-popups allow-forms"
      />
      <img
        v-else-if="selected.kind === 'image'"
        :src="selected.url"
        :alt="selected.name"
        class="max-h-40 w-full rounded-md border border-wb-border bg-wb-surface object-contain"
      />
      <div v-else class="flex items-center justify-between rounded-md border border-wb-border bg-wb-surface px-2 py-1.5 text-xs text-wb-muted">
        <span class="truncate">{{ selected.name }}</span>
        <a :href="selected.url" :download="selected.name" class="cursor-pointer text-wb-primary-strong hover:underline" @click.prevent="openExternal(selected.url)">{{ t('chat.download') }}</a>
      </div>
    </div>
  </aside>
</template>

<style scoped>
/* 只取 el-tabs 的 header 作为 tab 栏，内容区由上方自定义区域承载 */
.wb-panel-tabs :deep(.el-tabs__header) {
  margin: 0;
  padding: 0 4px;
  border-bottom: 1px solid var(--wb-border);
}
.wb-panel-tabs :deep(.el-tabs__content) {
  display: none;
}
.wb-panel-tabs :deep(.el-tabs__item) {
  height: 36px;
  line-height: 36px;
  font-size: 13px;
  padding: 0 10px;
}
.wb-panel-tabs :deep(.el-tabs__nav-wrap)::after {
  display: none;
}
</style>
