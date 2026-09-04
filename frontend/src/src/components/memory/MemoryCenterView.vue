<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { Brain, Search, RefreshCw, Trash2 } from '@/components/common/icons'
import { useMemoryStore } from '@/stores/memory'
import { t } from '@/i18n'
import type { MemoryEpisode } from '@/types/api'
import { formatDateTime } from '@/utils/time'

/**
 * 记忆中心三类记忆统一管理视图。
 *
 * <p>三 tab 结构（ElementPlus el-tabs）：
 * <ul>
 *   <li><b>情景记忆</b> — 每轮对话摘要（episodic），支持搜索 / 删除</li>
 *   <li><b>语义记忆</b> — 稳定偏好与事实（semantic facts）</li>
 *   <li><b>程序记忆</b> — 工具链成功率统计（procedural）</li>
 * </ul>
 *
 * <p>顶部统一召回框：输入关键词走 RRF 融合召回（跨三类），结果以卡片展示。
 *
 * <p>ElementPlus 组件按需引入（unplugin-vue-components 自动 import）：
 * el-tabs / el-tab-pane / el-table / el-table-column / el-input / el-button / el-tag / el-empty。
 */
type MemoryTab = 'episodes' | 'facts' | 'procedures'

const memory = useMemoryStore()
const {
  episodes, total, query, loading, error,
  facts, procedures,
  recallResults, recallLoading
} = storeToRefs(memory)
const { load, loadFacts, loadProcedures, recall, doDelete } = memory

const activeTab = ref<MemoryTab>('episodes')
const recallQuery = ref('')

function switchTab(tab: string | number): void {
  const t = String(tab) as MemoryTab
  activeTab.value = t
  if (t === 'episodes' && episodes.value.length === 0) void load()
  if (t === 'facts' && facts.value.length === 0) void loadFacts()
  if (t === 'procedures' && procedures.value.length === 0) void loadProcedures()
}

function doRecall(): void {
  if (!recallQuery.value.trim()) return
  void recall(recallQuery.value)
}

function refreshCurrent(): void {
  if (activeTab.value === 'episodes') void load()
  else if (activeTab.value === 'facts') void loadFacts()
  else void loadProcedures()
}

function fmtTime(iso: string | number | null): string {
  return formatDateTime(iso)
}

onMounted(() => {
  void load()
})
</script>

<template>
  <div class="flex h-full overflow-y-auto bg-wb-bg text-wb-ink">
    <div class="mx-auto w-full max-w-6xl space-y-5 px-6 py-8">
      <!-- Hero header -->
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <Brain class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('memory.center.title') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('memory.center.subtitle') }}</p>
        </div>
        <span class="ml-auto badge-neutral">{{ t('memory.center.total', total) }}</span>
      </header>

      <div v-if="error" class="rounded-lg bg-wb-danger/15 px-3 py-2 text-sm text-wb-danger">{{ error }}</div>

      <!-- 统一召回（RRF 跨三类） -->
      <section class="card p-4">
        <h2 class="mb-3 flex items-center gap-2 font-display text-sm font-semibold text-wb-ink">
          <Search class="h-4 w-4 text-wb-primary" />
          {{ t('memory.center.recall') }}
        </h2>
        <div class="flex gap-2">
          <el-input
            v-model="recallQuery"
            :placeholder="t('memory.center.recallPlaceholder')"
            clearable
            class="flex-1"
            @keyup.enter="doRecall"
          >
            <template #prefix>
              <Search class="h-4 w-4 text-wb-muted" />
            </template>
          </el-input>
          <el-button type="primary" :loading="recallLoading" @click="doRecall">
            {{ t('memory.center.recallBtn') }}
          </el-button>
        </div>
        <!-- 召回结果 -->
        <div v-if="recallResults.length > 0" class="mt-3 space-y-2">
          <div
            v-for="(r, idx) in recallResults"
            :key="`${r.kind}-${idx}`"
            class="rounded-xl border border-wb-border bg-wb-surface/60 p-3 text-sm"
          >
            <div class="mb-1 flex items-center gap-2">
              <el-tag size="small" :type="r.kind === 'episode' ? 'primary' : r.kind === 'fact' ? 'success' : 'warning'">
                {{ t(`memory.center.kind.${r.kind}`) }}
              </el-tag>
              <span class="text-xs text-wb-muted">score {{ r.score.toFixed(3) }}</span>
              <span class="ml-auto truncate text-xs text-wb-muted" :title="r.source">{{ r.source }}</span>
            </div>
            <p class="line-clamp-3 font-medium text-wb-ink">{{ r.title }}</p>
            <p v-if="r.snippet" class="line-clamp-3 text-wb-muted">{{ r.snippet }}</p>
          </div>
        </div>
        <el-empty v-else-if="recallQuery && !recallLoading" :description="t('memory.center.noRecall')" :image-size="60" />
      </section>

      <!-- 三类记忆 tabs（包进卡片，与全站 section.card 模式统一） -->
      <section class="card p-5">
        <el-tabs v-model="activeTab" class="memory-tabs" @tab-change="switchTab">
        <!-- 情景记忆 -->
        <el-tab-pane :label="t('memory.center.tab.episodes')" name="episodes">
          <div class="mb-3 flex items-center gap-2">
            <el-input
              v-model="query"
              :placeholder="t('memory.center.searchEpisodes')"
              clearable
              class="max-w-xs"
              @keyup.enter="load"
            />
            <el-button @click="load">
              <Search class="h-3.5 w-3.5" />
            </el-button>
            <el-button circle @click="refreshCurrent">
              <RefreshCw class="h-3.5 w-3.5" />
            </el-button>
          </div>
          <div v-if="loading" class="py-8 text-center text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
          <el-table v-else-if="episodes.length > 0" :data="episodes" stripe class="wb-el-table">
            <el-table-column prop="summary" :label="t('memory.center.col.summary')" min-width="260" show-overflow-tooltip />
            <el-table-column prop="source" :label="t('memory.center.col.source')" width="90">
              <template #default="{ row }">
                <el-tag size="small" :type="row.source === 'chat' ? 'info' : 'success'">
                  {{ row.source || 'manual' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="importance" :label="t('memory.center.col.importance')" width="100">
              <template #default="{ row }">
                <span class="text-xs text-wb-muted">{{ (row.importance ?? 0).toFixed(2) }}</span>
              </template>
            </el-table-column>
            <el-table-column prop="createdAt" :label="t('memory.center.col.time')" width="160">
              <template #default="{ row }">
                <span class="text-xs text-wb-muted">{{ fmtTime(row.created_at) }}</span>
              </template>
            </el-table-column>
            <el-table-column :label="t('memory.center.col.actions')" width="70" fixed="right">
              <template #default="{ row }">
                <el-button link type="danger" size="small" @click="doDelete(row as MemoryEpisode)">
                  <Trash2 class="h-3.5 w-3.5" />
                </el-button>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-else :description="t('memory.center.emptyEpisodes')" />
        </el-tab-pane>

        <!-- 语义记忆 -->
        <el-tab-pane :label="t('memory.center.tab.facts')" name="facts">
          <div class="mb-3 flex justify-end">
            <el-button circle @click="refreshCurrent">
              <RefreshCw class="h-3.5 w-3.5" />
            </el-button>
          </div>
          <el-table v-if="facts.length > 0" :data="facts" stripe class="wb-el-table">
            <el-table-column prop="key" :label="t('memory.center.col.key')" min-width="180" show-overflow-tooltip />
            <el-table-column prop="value" :label="t('memory.center.col.value')" min-width="300" show-overflow-tooltip />
            <el-table-column prop="updatedAt" :label="t('memory.center.col.time')" width="160">
              <template #default="{ row }">
                <span class="text-xs text-wb-muted">{{ fmtTime(row.updated_at) }}</span>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-else :description="t('memory.center.emptyFacts')" />
        </el-tab-pane>

        <!-- 程序记忆 -->
        <el-tab-pane :label="t('memory.center.tab.procedures')" name="procedures">
          <div class="mb-3 flex justify-end">
            <el-button circle @click="refreshCurrent">
              <RefreshCw class="h-3.5 w-3.5" />
            </el-button>
          </div>
          <el-table v-if="procedures.length > 0" :data="procedures" stripe class="wb-el-table">
            <el-table-column prop="name" :label="t('memory.center.col.name')" min-width="180" show-overflow-tooltip />
            <el-table-column prop="signature" :label="t('memory.center.col.signature')" min-width="240" show-overflow-tooltip />
            <el-table-column prop="count" :label="t('memory.center.col.count')" width="90">
              <template #default="{ row }">
                <el-tag size="small">{{ row.count }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="lastUsedAt" :label="t('memory.center.col.lastUsed')" width="160">
              <template #default="{ row }">
                <span class="text-xs text-wb-muted">{{ fmtTime(row.last_used_at) }}</span>
              </template>
            </el-table-column>
          </el-table>
          <el-empty v-else :description="t('memory.center.emptyProcedures')" />
        </el-tab-pane>
        </el-tabs>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* ElementPlus tabs 与 wb 主题融合：去掉默认底部边框，用 wb-border */
.memory-tabs :deep(.el-tabs__header) {
  margin-bottom: 12px;
}
.memory-tabs :deep(.el-tabs__nav-wrap::after) {
  background-color: var(--wb-border);
}
.memory-tabs :deep(.el-tabs__item) {
  color: var(--wb-muted);
}
.memory-tabs :deep(.el-tabs__item.is-active) {
  color: var(--wb-primary);
}
.memory-tabs :deep(.el-tabs__active-bar) {
  background-color: var(--wb-primary);
}
</style>