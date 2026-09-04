<script setup lang="ts">
import { computed, ref, watch, type Component } from 'vue'
import {
  Code,
  FileText,
  FolderOpen,
  Image,
  Layers,
  RefreshCw,
  Download,
  X as Close
} from '@/components/common/icons'
import { apiGet } from '@/api/client'
import type { FolderTreeNode, WorkspaceFile } from '@/types/api'
import { t } from '@/i18n'
import Skeleton from '@/components/common/Skeleton.vue'
import FolderTree from '@/components/chat/FolderTree.vue'

/**
 * 工作区侧栏：五个 tab 展示当前会话的工作区内容。
 *
 * <ul>
 *   <li><b>files</b>：物理产出文件（`/api/v1/chat/workspace/files/{id}`）</li>
 *   <li><b>artifacts</b>：可预览产物（html / url / text / pdf / image）</li>
 *   <li><b>folders</b>：绑定到该会话工作区的逻辑文件夹树</li>
 *   <li><b>context</b>：上下文记忆（待后端接口）</li>
 *   <li><b>code</b>：其余可当作代码预览的文件</li>
 * </ul>
 */
const props = defineProps<{
  session_id: string | null
}>()

type TabID = 'files' | 'artifacts' | 'folders' | 'context' | 'code'

interface TabDef {
  id: TabID
  labelKey: string
  icon: Component
}

const tabs: TabDef[] = [
  { id: 'files', labelKey: 'chat.tabFiles', icon: FolderOpen },
  { id: 'artifacts', labelKey: 'chat.tabArtifacts', icon: Image },
  { id: 'folders', labelKey: 'chat.tabFolders', icon: Layers },
  { id: 'context', labelKey: 'chat.tabContext', icon: FileText },
  { id: 'code', labelKey: 'chat.tabCode', icon: Code }
]

const activeTab = ref<TabID>('files')
const files = ref<WorkspaceFile[]>([])
const folders = ref<FolderTreeNode[]>([])
const loading = ref(false)
const selected = ref<WorkspaceFile | null>(null)

// 上下文记忆占位数据
const contextItems = ref<{ id: string; summary: string; created_at: number }[]>([])
const contextLoading = ref(false)

const totalFolderCount = computed(() => countNodes(folders.value))

/** 可预览产物：html / url / text / pdf / image。 */
const previewableFiles = computed(() =>
  files.value.filter((f) => ['html', 'url', 'text', 'pdf', 'image'].includes(f.kind))
)

/** 其余文件按代码预览。 */
const codeFiles = computed(
  () => files.value.filter((f) => !['html', 'url', 'text', 'pdf', 'image'].includes(f.kind))
)

function countNodes(nodes: FolderTreeNode[]): number {
  let n = 0
  for (const x of nodes) {
    n += 1 + countNodes(x.children ?? [])
  }
  return n
}

watch(
  () => props.session_id,
  (id) => {
    selected.value = null
    contextItems.value = []
    folders.value = []
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
  // 根据文件类型自动切换 tab
  if (['html', 'url', 'text', 'pdf', 'image'].includes(f.kind)) {
    activeTab.value = 'artifacts'
  } else {
    activeTab.value = 'code'
  }
}

function fmtSize(size?: number): string {
  if (!size) return ''
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

function switchTab(id: TabID): void {
  activeTab.value = id
  if (id === 'context' && contextItems.value.length === 0) {
    loadContext()
  }
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

async function loadContext(): Promise<void> {
  if (!props.session_id) return
  contextLoading.value = true
  try {
  // 上下文记忆面板：后端暂无独立端点，保持空列表（会话记忆经 MEMORY.md 在 agent 侧生效）
  contextItems.value = []
  } catch {
    contextItems.value = []
  } finally {
    contextLoading.value = false
  }
}
</script>

<template>
  <aside class="flex h-full w-full flex-col border-l border-wb-border bg-wb-surface/50">
    <div class="relative min-h-0 flex-1">
      <el-tabs v-model="activeTab" class="wb-panel-tabs h-full" @tab-change="onTabChange">
        <el-tab-pane v-for="tab in tabs" :key="tab.id" :name="tab.id">
          <template #label>
            <span class="flex items-center gap-1.5 text-xs">
              <el-icon :size="14"><component :is="tab.icon" /></el-icon>
              {{ t(tab.labelKey) }}
            </span>
          </template>
        </el-tab-pane>
      </el-tabs>

      <el-button
        class="absolute right-2 top-1.5 z-10"
        text
        circle
        size="small"
        :disabled="!props.session_id || loading"
        :title="t('chat.refresh')"
        @click="refresh"
      >
        <el-icon :size="14" :class="loading ? 'is-loading' : ''"><RefreshCw /></el-icon>
      </el-button>

      <!-- 内容区按当前 tab 渲染 -->
      <div class="absolute inset-x-0 bottom-0 top-[40px] overflow-y-auto p-2">
        <Skeleton v-if="loading || (activeTab === 'context' && contextLoading)" :lines="4" />

        <!-- files：物理产出文件 -->
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
                @click.stop
              >
                <Download class="h-3 w-3" />
              </a>
            </li>
          </ul>
        </template>

        <!-- artifacts：可预览产物 -->
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

        <!-- context：上下文记忆 -->
        <template v-else-if="activeTab === 'context'">
          <el-empty
            v-if="contextItems.length === 0"
            :description="t('chat.noContext')"
            :image-size="48"
          />
          <ul v-else class="space-y-2">
            <li
              v-for="item in contextItems"
              :key="item.id"
              class="rounded-lg border border-wb-border p-2 text-xs"
            >
              <p class="text-wb-ink">{{ item.summary }}</p>
              <p class="mt-1 text-[10px] text-wb-muted">{{ item.created_at }}</p>
            </li>
          </ul>
        </template>

        <!-- code：其余文件按代码预览 -->
        <template v-else-if="activeTab === 'code'">
          <el-empty
            v-if="codeFiles.length === 0"
            :description="t('chat.workspaceEmpty')"
            :image-size="48"
          />
          <ul v-else class="space-y-0.5">
            <li
              v-for="f in codeFiles"
              :key="f.path"
              class="group flex cursor-pointer items-center gap-2 rounded-lg px-2 py-1.5 text-xs transition-colors"
              :class="selected?.path === f.path ? 'bg-wb-primary/10 text-wb-primary-strong' : 'text-wb-ink hover:bg-wb-primary/5'"
              @click="selectFile(f)"
            >
              <Code class="h-3.5 w-3.5 shrink-0" />
              <span class="min-w-0 flex-1 truncate" :title="f.path">{{ f.path }}</span>
            </li>
          </ul>
        </template>
      </div>
    </div>

    <!-- 选中文件预览 -->
    <div v-if="selected" class="shrink-0 border-t border-wb-border p-2">
      <div class="mb-1 flex items-center justify-between">
        <span class="truncate text-xs text-wb-ink">{{ selected.name || selected.path }}</span>
        <el-button text circle size="small" @click="selected = null">
          <el-icon :size="14"><Close /></el-icon>
        </el-button>
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
        <a :href="selected.url" :download="selected.name" class="text-wb-primary-strong hover:underline">{{ t('chat.download') }}</a>
      </div>
    </div>
  </aside>
</template>

<style scoped>
/* 只取 el-tabs 的 header 作为 tab 栏，内容区由下方自定义区域承载 */
.wb-panel-tabs :deep(.el-tabs__header) {
  margin: 0;
  padding: 0 4px;
  border-bottom: 1px solid var(--wb-border);
}
.wb-panel-tabs :deep(.el-tabs__content) {
  display: none;
}
.wb-panel-tabs :deep(.el-tabs__item) {
  height: 40px;
  line-height: 40px;
  font-size: 13px;
  padding: 0 10px;
}
.wb-panel-tabs :deep(.el-tabs__nav-wrap)::after {
  display: none;
}
</style>
