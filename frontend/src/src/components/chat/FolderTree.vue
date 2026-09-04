<script setup lang="ts">
/**
 * 文件夹树。
 *
 * <p>绑定到 {@code chat_sessions.workspace_id} 或 sessionID 的 {@code folders} 表条目；
 * {@code FolderTreeNode} 数据在 computed 中转换为 el-tree 结构（label/children 平铺），
 * 节点内容用 slot 渲染（folder 图标 + 名称 + 子节点计数）。
 */
import { computed } from 'vue'
import { FolderOpen, Folder } from '@/components/common/icons'
import type { FolderTreeNode } from '@/types/api'

defineOptions({ name: 'FolderTree' })

/** el-tree 平铺节点结构（从 FolderTreeNode 嵌套 folder 对象转换而来）。 */
interface TreeItem {
  key: string
  label: string
  path: string
  child_count: number
  children: TreeItem[]
}

const props = defineProps<{
  nodes: FolderTreeNode[]
  /** 兼容旧调用签名；el-tree 自带缩进，此参数不再参与渲染。 */
  depth?: number
}>()

const treeData = computed<TreeItem[]>(() => toTree(props.nodes))

function toTree(nodes: FolderTreeNode[]): TreeItem[] {
  return nodes.map((n) => ({
    key: n.folder.id,
    label: n.folder.name,
    path: n.folder.path,
    child_count: n.folder.child_count ?? 0,
    children: toTree(n.children ?? [])
  }))
}
</script>

<template>
  <el-tree
    :data="treeData"
    node-key="key"
    default-expand-all
    :expand-on-click-node="false"
    :props="{ label: 'label', children: 'children' }"
    class="folder-tree"
  >
    <template #default="{ data }">
      <span class="flex items-center gap-1.5 text-xs" :title="data.path">
        <FolderOpen v-if="data.children?.length" class="h-3.5 w-3.5 shrink-0 text-wb-muted" />
        <Folder v-else class="h-3.5 w-3.5 shrink-0 text-wb-muted" />
        <span class="min-w-0 flex-1 truncate text-wb-ink">{{ data.label }}</span>
        <span
          v-if="data.child_count > 0"
          class="shrink-0 rounded bg-wb-primary/10 px-1 text-[10px] text-wb-primary-strong"
        >{{ data.child_count }}</span>
      </span>
    </template>
  </el-tree>
</template>

<style scoped>
/* el-tree 融入 wb 主题：透明背景 + wb 色节点 */
.folder-tree {
  --el-tree-node-hover-bg-color: color-mix(in srgb, var(--wb-primary) 8%, transparent);
  --el-tree-text-color: var(--wb-ink);
  background: transparent;
}
.folder-tree :deep(.el-tree-node__content) {
  border-radius: 8px;
  height: 28px;
}
</style>
