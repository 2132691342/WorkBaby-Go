<script setup lang="ts">
import { ref, type Component } from 'vue'
import FilesView from '@/components/files/FilesView.vue'
import FoldersView from '@/components/folders/FoldersView.vue'
import PetSpaceView from '@/components/pet/PetSpaceView.vue'
import ToolsView from '@/components/tools/ToolsView.vue'
import ChannelsView from '@/components/channel/ChannelsView.vue'
import { t } from '@/i18n'

/**
 * 更多资源聚合页：低频管理页的统一入口。
 *
 * <p>tab 承载文件库 / 文件夹 / 桌宠 / 工具 / 通道五块，复用各独立视图组件
 * （各自带数据加载与操作）。lazy 挂载：tab 首次激活才加载对应视图。
 */
interface MoreTab {
  name: string
  labelKey: string
  component: Component
}

const TABS: MoreTab[] = [
  { name: 'files', labelKey: 'nav.files', component: FilesView },
  { name: 'folders', labelKey: 'nav.folders', component: FoldersView },
  { name: 'pet', labelKey: 'nav.pet', component: PetSpaceView },
  { name: 'tools', labelKey: 'nav.tools', component: ToolsView },
  { name: 'channels', labelKey: 'nav.channels', component: ChannelsView }
]

const active = ref('files')
</script>

<template>
  <div class="flex h-full min-h-0 flex-col overflow-hidden text-wb-ink">
    <div class="mx-auto flex w-full max-w-5xl min-h-0 flex-1 flex-col px-6 py-6">
      <header class="flex shrink-0 items-center gap-3 pb-4">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="h-5 w-5">
            <rect x="3" y="3" width="7" height="7" rx="1" />
            <rect x="14" y="3" width="7" height="7" rx="1" />
            <rect x="3" y="14" width="7" height="7" rx="1" />
            <rect x="14" y="14" width="7" height="7" rx="1" />
          </svg>
        </div>
        <div>
          <h1 class="text-base font-semibold">{{ t('nav.moreResources') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('nav.moreResourcesHint') }}</p>
        </div>
      </header>

      <el-tabs v-model="active" class="wb-more-tabs flex min-h-0 flex-1 flex-col">
        <el-tab-pane v-for="tab in TABS" :key="tab.name" :name="tab.name" :label="t(tab.labelKey)" lazy>
          <div class="min-h-0 flex-1 overflow-y-auto">
            <component :is="tab.component" />
          </div>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>
</template>

<style scoped>
/* tabs 融入 Studio：细分割线 + 激活项品牌色 */
.wb-more-tabs :deep(.el-tabs__header) {
  margin-bottom: 0;
}
.wb-more-tabs :deep(.el-tabs__nav-wrap::after) {
  height: 1px;
  background: var(--wb-border);
}
.wb-more-tabs :deep(.el-tabs__item) {
  font-size: 13px;
  color: var(--wb-muted);
}
.wb-more-tabs :deep(.el-tabs__item.is-active),
.wb-more-tabs :deep(.el-tabs__item:hover) {
  color: var(--wb-primary);
}
.wb-more-tabs :deep(.el-tabs__content) {
  overflow: hidden;
  flex: 1;
  min-height: 0;
}
.wb-more-tabs :deep(.el-tab-pane) {
  height: 100%;
  display: flex;
  flex-direction: column;
  min-height: 0;
}
</style>
