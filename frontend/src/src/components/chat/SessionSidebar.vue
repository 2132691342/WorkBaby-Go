<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Search, CircleCheck, Circle, Delete, GitBranch } from '@/components/common/icons'
import type { Session } from '@/types/api'
import { t } from '@/i18n'
import { useDialog } from '@/composables/useDialog'
import { useChatStore } from '@/stores/chat'

/**
 * 会话侧边栏：树形分组 + 搜索 + 多选批量删除。
 *
 * 树形血缘：parent_id 相同的会话挂在同一个父节点下；根会话按时段分组。
 * 缩进深度 = 父级缩进 + 1；子节点不可换行或换组，仅作为父的「分支」展示。
 */
const props = defineProps<{
  sessions: Session[]
  currentID: string | null
  loading: boolean
  creating?: boolean
}>()

const emit = defineEmits<{
  select: [id: string]
  create: []
  remove: [id: string]
  'delete-batch': [ids: string[]]
}>()

const dialog = useDialog()
const chat = useChatStore()
const multiSelect = ref(false)
const selectedIds = ref<Set<string>>(new Set())
const search = ref('')

// 后端内容搜索：输入去抖后跨会话检索（标题 + 正文），
// 命中的会话合并进列表——本地标题过滤抓不到「内容里提过」的会话。
let searchTimer: number | undefined
watch(search, (q) => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = window.setTimeout(() => void chat.searchSessions(q), 250)
})

/** 后端内容搜索命中且尚未在加载列表里的会话（搜索态下并入展示）。 */
const remoteHits = computed<Session[]>(() => {
  if (!search.value.trim()) return []
  const known = new Set(props.sessions.map((s) => s.id))
  return chat.searchResults.filter((s) => !known.has(s.id))
})

function toggleSelect(id: string): void {
  const next = new Set(selectedIds.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedIds.value = next
}

function enterMultiSelect(): void {
  multiSelect.value = true
  selectedIds.value = new Set()
}

function exitMultiSelect(): void {
  multiSelect.value = false
  selectedIds.value = new Set()
}

async function confirmDeleteBatch(): Promise<void> {
  if (selectedIds.value.size === 0) return
  const ok = await dialog.confirm({
    title: t('chat.deleteBatchTitle'),
    content: t('chat.deleteBatchConfirm', selectedIds.value.size),
    danger: true
  })
  if (!ok) return
  emit('delete-batch', Array.from(selectedIds.value))
  exitMultiSelect()
}

/** 单个渲染节点（根或分支），扁平化以便分组遍历。 */
interface SessionNode {
  session: Session
  depth: number
  isRoot: boolean
  branchLabel?: string
}

interface Group {
  label: string
  items: SessionNode[]
}

const searchLower = computed(() => search.value.trim().toLowerCase())

/**
 * 构建树状节点序列：
 *   - 根会话按时段分组（今天 / 昨天 / 一周内 / 更早）；
 *   - 每个根会话下挂其直接子分支（按 last_message_at 倒序）；
 *   - 多层分叉暂不展开（分叉可递归，但平铺更易读）。
 */
function buildTree(): { roots: Session[]; children: Map<string, Session[]> } {
  const roots: Session[] = []
  const children = new Map<string, Session[]>()
  for (const s of props.sessions) {
    if (!s.parent_id) {
      roots.push(s)
    } else {
      const arr = children.get(s.parent_id) ?? []
      arr.push(s)
      children.set(s.parent_id, arr)
    }
  }
  // 后端内容搜索命中、未在加载列表里的会话并入根列表
  roots.push(...remoteHits.value)
  // 子分支按时段倒序
  for (const arr of children.values()) {
    arr.sort((a, b) => (b.last_message_at ?? 0) - (a.last_message_at ?? 0))
  }
  return { roots, children }
}

const tree = computed(() => buildTree())

/** 根会话的分支数。 */
function branchCount(id: string): number {
  return (tree.value.children.get(id) ?? []).length
}

/** 单个节点是否命中搜索（标题或子分支标题任一命中即视为命中）。 */
function nodeMatches(node: SessionNode): boolean {
  if (!searchLower.value) return true
  if ((node.session.name ?? '').toLowerCase().includes(searchLower.value)) return true
  // 子分支标题命中也要把根带出来
  const kids = tree.value.children.get(node.session.id) ?? []
  return kids.some((c) => (c.name ?? '').toLowerCase().includes(searchLower.value))
}

const grouped = computed<Group[]>(() => {
  const { roots, children } = tree.value
  const filteredRoots = searchLower.value
    ? roots.filter((r) => nodeMatches({ session: r, depth: 0, isRoot: true }))
    : roots
  if (filteredRoots.length === 0) return []

  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
  const yesterday = today - 86400000
  const weekAgo = today - 7 * 86400000

  const todayList: SessionNode[] = []
  const yesterdayList: SessionNode[] = []
  const weekList: SessionNode[] = []
  const olderList: SessionNode[] = []
  const bucket = (ts: number): SessionNode[] => {
    if (ts >= today) return todayList
    if (ts >= yesterday) return yesterdayList
    if (ts >= weekAgo) return weekList
    return olderList
  }

  for (const root of filteredRoots) {
    const rootNode: SessionNode = { session: root, depth: 0, isRoot: true }
    bucket(new Date(root.last_message_at ?? 0).getTime()).push(rootNode)
    const kids = children.get(root.id) ?? []
    for (const child of kids) {
      bucket(new Date(child.last_message_at ?? 0).getTime()).push({
        session: child,
        depth: 1,
        isRoot: false
      })
    }
  }

  const groups: Group[] = []
  const push = (label: string, list: SessionNode[]) => {
    if (list.length) groups.push({ label: t(label) + ` (${list.length})`, items: list })
  }
  push('chat.today', todayList)
  push('chat.yesterday', yesterdayList)
  push('chat.week', weekList)
  push('chat.earlier', olderList)
  return groups
})

function fmtTime(iso: string | number | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const hm = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  return sameDay ? hm : `${d.getMonth() + 1}/${d.getDate()} ${hm}`
}

// 命中搜索时自动清空多选（避免搜索态的 checkbox 残留）
watch(searchLower, () => {
  if (searchLower.value) exitMultiSelect()
})
</script>

<template>
  <aside class="flex h-full w-full flex-col">
    <!-- 顶部「会话」+ 「+」由 App.vue 的 ses-head 渲染，此处不重复；
         SessionSidebar 只负责搜索框、多选入口、会话列表 -->
    <div class="space-y-2 border-b border-wb-border p-3">
      <!-- 多选模式：取消 + 批量删除 -->
      <div v-if="multiSelect" class="flex items-center gap-1.5">
        <el-button class="flex-1" size="small" @click="exitMultiSelect">
          {{ t('chat.cancelMultiSelect') }}
        </el-button>
        <el-button
          class="flex-1"
          type="danger"
          size="small"
          :disabled="selectedIds.size === 0"
          @click="confirmDeleteBatch"
        >
          <el-icon class="mr-1"><Delete /></el-icon>
          {{ t('chat.deleteBatch', selectedIds.size) }}
        </el-button>
      </div>

      <el-input v-model="search" size="small" :placeholder="t('chat.searchSession')" clearable>
        <template #prefix><el-icon><Search /></el-icon></template>
      </el-input>

      <!-- 多选入口（≥2 个会话才显示，避免噪声） -->
      <el-button
        v-if="!multiSelect && sessions.length > 1"
        class="w-full"
        size="small"
        text
        @click="enterMultiSelect"
      >
        {{ t('chat.enterMultiSelect') }}
      </el-button>
    </div>

    <el-scrollbar class="flex-1">
      <div class="px-2 pb-2">
        <div v-if="loading" class="p-3 text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
        <el-empty
          v-else-if="sessions.length === 0"
          :description="t('chat.noSessions')"
          :image-size="56"
        />
        <el-empty
          v-else-if="grouped.length === 0"
          :description="t('chat.noMatch')"
          :image-size="56"
        />
        <template v-else>
          <div v-for="g in grouped" :key="g.label" class="mt-2">
            <div class="px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-wb-muted">
              {{ g.label }}
            </div>
            <ul class="space-y-0.5">
              <li
                v-for="node in g.items"
                :key="node.session.id"
                class="group relative flex cursor-pointer items-center justify-between rounded-lg px-2 py-1.5 text-sm transition-colors"
                :class="[
                  node.session.id === currentID && !multiSelect
                    ? 'bg-wb-primary/10 text-wb-primary-strong'
                    : 'text-wb-ink hover:bg-wb-primary/5',
                  selectedIds.has(node.session.id) ? 'ring-1 ring-wb-primary/40' : '',
                  node.isRoot ? '' : 'ml-3 border-l border-wb-border pl-2 bg-wb-primary/[0.03]'
                ]"
                @click="multiSelect ? toggleSelect(node.session.id) : emit('select', node.session.id)"
              >
                <button
                  v-if="multiSelect"
                  type="button"
                  class="mr-1.5 shrink-0 text-wb-primary"
                  @click.stop="toggleSelect(node.session.id)"
                >
                  <el-icon><component :is="selectedIds.has(node.session.id) ? CircleCheck : Circle" /></el-icon>
                </button>
                <div class="min-w-0 flex-1">
                  <div class="flex items-center gap-1">
                    <span v-if="!node.isRoot" class="text-wb-muted shrink-0" :title="t('chat.branch')">
                      <el-icon><GitBranch /></el-icon>
                    </span>
                    <span class="truncate text-sm font-medium">
                      {{ node.session.name || t('chat.unnamed') }}
                    </span>
                    <!-- 分支数徽标：根会话有支线时展示 -->
                    <span
                      v-if="node.isRoot && branchCount(node.session.id) > 0"
                      class="shrink-0 rounded-full bg-wb-primary/10 px-1.5 text-[10px] font-medium text-wb-primary-strong"
                      :title="t('chat.branchCount', branchCount(node.session.id))"
                    >
                      <el-icon :size="10" class="align-[-1px]"><GitBranch /></el-icon>
                      {{ branchCount(node.session.id) }}
                    </span>
                  </div>
                  <div class="truncate text-xs text-wb-muted">
                    {{ fmtTime(node.session.last_message_at) || t('chat.message_count', node.session.message_count ?? 0) }}
                  </div>
                </div>
                <el-button
                  v-if="!multiSelect"
                  type="danger"
                  size="small"
                  text
                  circle
                  class="ml-1 shrink-0 opacity-0 group-hover:opacity-100 focus:opacity-100"
                  :title="t('chat.deleteSession')"
                  @click.stop="emit('remove', node.session.id)"
                >
                  <el-icon :size="14"><Delete /></el-icon>
                </el-button>
              </li>
            </ul>
          </div>
        </template>
      </div>
    </el-scrollbar>
  </aside>
</template>
