<script setup lang="ts">
import { onMounted } from 'vue'
import { storeToRefs } from 'pinia'
import { useTasksStore } from '@/stores/tasks'
import { Activity } from '@/components/common/icons'
import type { Session } from '@/types/api'
import { t } from '@/i18n'
import { formatDateTime } from '@/utils/time'

/**
 * 任务视图。
 *
 * <p>本视图只是 store 的「薄壳」——状态/方法都在 {@link useTasksStore} 中。
 * <p>el-radio-group 过滤 + el-table 会话列表，统一 wb 玻璃态。
 */
const tasks = useTasksStore()
const { sessions, error, loading, filter } = storeToRefs(tasks)

onMounted(tasks.load)

const filtered = (): Session[] => {
  if (filter.value === 'active') return sessions.value.filter((s) => (s.message_count ?? 0) > 0)
  if (filter.value === 'empty') return sessions.value.filter((s) => (s.message_count ?? 0) === 0)
  return sessions.value
}

function fmt(t: string | number | null): string {
  return formatDateTime(t)
}
</script>

<template>
  <div class="flex h-full flex-col overflow-y-auto text-wb-ink">
    <div class="mx-auto w-full max-w-4xl space-y-5 px-6 py-8">
      <!-- Hero header -->
      <header class="flex items-center gap-3">
        <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-wb-primary/10 text-wb-primary">
          <Activity class="h-5 w-5" />
        </div>
        <div>
          <h1 class="font-display text-lg font-semibold text-wb-ink">{{ t('task.title') }}</h1>
          <p class="text-xs text-wb-muted">{{ t('task.subtitle') }}</p>
        </div>
        <el-radio-group v-model="filter" size="small" class="ml-auto">
          <el-radio-button value="all">{{ t('task.all') }}</el-radio-button>
          <el-radio-button value="active">{{ t('task.active') }}</el-radio-button>
          <el-radio-button value="empty">{{ t('task.emptyFilter') }}</el-radio-button>
        </el-radio-group>
      </header>

      <div v-if="error" class="rounded-md bg-wb-danger/15 px-3 py-2 text-sm text-wb-danger">{{ error }}</div>

      <div v-if="loading" class="text-sm text-wb-muted">{{ t('ui.status.loading') }}</div>
      <el-table v-else-if="filtered().length > 0" :data="filtered()" stripe class="wb-el-table">
        <el-table-column :label="t('task.name')" min-width="220">
          <template #default="{ row }">
            <span class="font-medium text-wb-ink">{{ (row as Session).name || t('task.unnamed') }}</span>
          </template>
        </el-table-column>
        <el-table-column :label="t('task.meta')" min-width="300">
          <template #default="{ row }">
            <span class="text-xs text-wb-muted">
              {{ t('task.meta', (row as Session).message_count ?? 0, fmt((row as Session).created_at), fmt((row as Session).last_message_at)) }}
            </span>
          </template>
        </el-table-column>
        <el-table-column :label="t('task.status')" width="100">
          <template #default="{ row }">
            <el-tag size="small" :type="(row as Session).active ? 'success' : 'info'" effect="plain">
              {{ (row as Session).active ? 'active' : 'closed' }}
            </el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else :description="t('task.empty')" :image-size="80" class="py-8" />
    </div>
  </div>
</template>

